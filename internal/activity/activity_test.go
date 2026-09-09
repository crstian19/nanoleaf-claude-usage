package activity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSafeSessionIDRejectsPathEscapes is a security test. Session IDs come
// from hook payloads, i.e. from outside this process, and are used as
// filenames; anything path-shaped must be refused rather than sanitised, so a
// crafted payload cannot steer a write out of the state directory.
func TestSafeSessionIDRejectsPathEscapes(t *testing.T) {
	bad := []string{
		"",
		"..",
		"../../etc/passwd",
		"a/b",
		`a\b`,
		"a\x00b",
		"foo.json",
		".hidden",
		"with space",
		strings.Repeat("a", 129),
	}

	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			if _, err := safeSessionID(id); err == nil {
				t.Errorf("safeSessionID(%q) was accepted", id)
			}
		})
	}
}

// TestSafeSessionIDAcceptsRealIDs makes sure the validation is not so strict
// that it rejects the UUIDs Claude Code actually sends.
func TestSafeSessionIDAcceptsRealIDs(t *testing.T) {
	good := []string{
		"f2869566-511c-4bde-9c29-b1e76b563c72",
		"abc123",
		"a_b-C9",
	}

	for _, id := range good {
		if _, err := safeSessionID(id); err != nil {
			t.Errorf("safeSessionID(%q) rejected: %v", id, err)
		}
	}
}

// TestWriteFromHookStaysInDirectory confirms the validation is actually wired
// into the write path, not just available to call.
func TestWriteFromHookStaysInDirectory(t *testing.T) {
	dir := t.TempDir()
	payload := `{"session_id":"../escaped","hook_event_name":"PreToolUse"}`

	if _, err := WriteFromHook(dir, strings.NewReader(payload), time.Now()); err == nil {
		t.Fatal("hook payload with a path-escaping session id was accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped.json")); err == nil {
		t.Fatal("a file was written outside the state directory")
	}
}

// TestReadAggregatesPhases checks the busiest phase across concurrent
// sessions wins, so one idle worktree cannot mask another that is working.
func TestReadAggregatesPhases(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	write(t, dir, "sessionA", `{"session_id":"sessionA","hook_event_name":"Stop"}`, now)
	write(t, dir, "sessionB", `{"session_id":"sessionB","hook_event_name":"PreToolUse","tool_name":"Bash"}`, now)

	snap, err := Read(dir, now)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if snap.Phase != PhaseTool {
		t.Errorf("phase = %v, want tool", snap.Phase)
	}
	if snap.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", snap.Sessions)
	}
	if snap.Tool != "Bash" {
		t.Errorf("tool = %q, want Bash", snap.Tool)
	}
}

// TestReadIgnoresStaleSessions covers the killed-session case: without an age
// cut-off, a crashed session's last event would light the display forever.
func TestReadIgnoresStaleSessions(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	write(t, dir, "ghost", `{"session_id":"ghost","hook_event_name":"PreToolUse"}`, now.Add(-staleAfter-time.Minute))

	snap, err := Read(dir, now)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if snap.Sessions != 0 {
		t.Errorf("sessions = %d, want 0", snap.Sessions)
	}
	if snap.Phase != PhaseIdle {
		t.Errorf("phase = %v, want idle", snap.Phase)
	}
}

// TestReadHoldsThenReleasesErrors checks an error is visible briefly and then
// stops masking real activity.
func TestReadHoldsThenReleasesErrors(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	write(t, dir, "s1", `{"session_id":"s1","hook_event_name":"PostToolUseFailure"}`, now)
	if snap, _ := Read(dir, now); snap.Phase != PhaseError {
		t.Errorf("fresh failure: phase = %v, want error", snap.Phase)
	}

	write(t, dir, "s1", `{"session_id":"s1","hook_event_name":"PostToolUseFailure"}`, now.Add(-errorHold-time.Second))
	if snap, _ := Read(dir, now); snap.Phase == PhaseError {
		t.Error("stale failure still shown as error")
	}
}

// TestReadSurvivesGarbage checks one unreadable file does not blank the whole
// display: hooks write concurrently and a half-written file is normal.
func TestReadSurvivesGarbage(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	write(t, dir, "good", `{"session_id":"good","hook_event_name":"PreToolUse"}`, now)

	snap, err := Read(dir, now)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if snap.Phase != PhaseTool {
		t.Errorf("phase = %v, want tool despite a corrupt neighbour", snap.Phase)
	}
}

// TestReadMissingDirectory covers a first run, before any hook has fired.
func TestReadMissingDirectory(t *testing.T) {
	snap, err := Read(filepath.Join(t.TempDir(), "nope"), time.Now())
	if err != nil {
		t.Fatalf("Read on missing dir: %v", err)
	}
	if snap.Phase != PhaseIdle || snap.Sessions != 0 {
		t.Errorf("got %+v, want an idle empty snapshot", snap)
	}
}

// TestSweepRemovesOnlyStaleFiles guards against the sweeper deleting live
// sessions' state.
func TestSweepRemovesOnlyStaleFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	write(t, dir, "live", `{"session_id":"live","hook_event_name":"PreToolUse"}`, now)
	write(t, dir, "dead", `{"session_id":"dead","hook_event_name":"PreToolUse"}`, now.Add(-staleAfter-time.Hour))

	if err := Sweep(dir, now); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "live.json")); err != nil {
		t.Errorf("live session file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dead.json")); !os.IsNotExist(err) {
		t.Error("stale session file was kept")
	}
}

// write records a hook payload and backdates the file so age-based logic can
// be exercised.
func write(t *testing.T, dir, id, payload string, at time.Time) {
	t.Helper()

	if _, err := WriteFromHook(dir, strings.NewReader(payload), at); err != nil {
		t.Fatalf("WriteFromHook(%s): %v", id, err)
	}
	path := filepath.Join(dir, id+".json")
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}
