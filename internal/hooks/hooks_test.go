package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// A settings file shaped like a real one: another tool's hooks already
// present, and hand-maintained top-level keys in a deliberate order.
const existing = `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "/other/tool hook"}]}
    ],
    "SubagentStop": [
      {"hooks": [{"type": "command", "command": "/other/tool hook"}]}
    ]
  },
  "statusLine": {"type": "command", "command": "/usr/bin/python3 status.py"},
  "_model_COMENTADO": "sonnet",
  "includeCoAuthoredBy": false
}`

func writeSettings(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

func TestInstallAddsEveryEvent(t *testing.T) {
	path := writeSettings(t, existing)

	res, err := Install(path, "/home/me/.local/bin/nanoclaude")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(res.Changed) != len(Events()) {
		t.Errorf("changed %d events, want %d", len(res.Changed), len(Events()))
	}
	if res.Backup == "" {
		t.Error("no backup was taken")
	}

	ours, others, err := Status(path)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, event := range Events() {
		if ours[event] != 1 {
			t.Errorf("event %s carries %d of our entries, want 1", event, ours[event])
		}
	}
	// The other tool's PreToolUse entry has to survive.
	if others["PreToolUse"] != 1 {
		t.Errorf("another tool's PreToolUse entry was lost (found %d)", others["PreToolUse"])
	}
}

// TestInstallIsIdempotent matters because a user runs this again after an
// upgrade, and because the first version keyed on the command text: a binary
// installed at a second path would have produced a duplicate of every hook.
func TestInstallIsIdempotent(t *testing.T) {
	path := writeSettings(t, existing)

	if _, err := Install(path, "/bin/nanoclaude"); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	res, err := Install(path, "/somewhere/else/nanoclaude")
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if len(res.Changed) != 0 {
		t.Errorf("second install changed %v, want nothing", res.Changed)
	}

	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(again) {
		t.Error("the second install rewrote the file")
	}
}

// TestInstallPreservesTheRestOfTheFile is why this package edits JSON in
// place instead of decoding and re-encoding it.
//
// Go marshals a map with its keys sorted, so a decode-and-re-encode would
// silently reorder every key in a hand-maintained configuration file to add
// nine entries. Here everything outside the edited paths must come back
// unchanged, order included.
func TestInstallPreservesTheRestOfTheFile(t *testing.T) {
	path := writeSettings(t, existing)

	if _, err := Install(path, "/bin/nanoclaude"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Same top-level keys, in the same order.
	var before, now []string
	gjson.Parse(existing).ForEach(func(k, _ gjson.Result) bool {
		before = append(before, k.String())
		return true
	})
	gjson.ParseBytes(after).ForEach(func(k, _ gjson.Result) bool {
		now = append(now, k.String())
		return true
	})
	if strings.Join(before, ",") != strings.Join(now, ",") {
		t.Errorf("top-level keys changed:\n before %v\n now    %v", before, now)
	}

	// Untouched values must be identical, byte for byte.
	for _, path := range []string{"model", "statusLine", "_model_COMENTADO", "includeCoAuthoredBy", "hooks.SubagentStop"} {
		want := gjson.Parse(existing).Get(path).Raw
		got := gjson.ParseBytes(after).Get(path).Raw
		if want != got {
			t.Errorf("%s changed:\n want %s\n got  %s", path, want, got)
		}
	}
}

func TestRemoveTakesOnlyOurs(t *testing.T) {
	path := writeSettings(t, existing)

	if _, err := Install(path, "/bin/nanoclaude"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	res, err := Remove(path)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(res.Changed) != len(Events()) {
		t.Errorf("removed from %d events, want %d", len(res.Changed), len(Events()))
	}

	ours, others, err := Status(path)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(ours) != 0 {
		t.Errorf("entries left behind: %v", ours)
	}
	if others["PreToolUse"] != 1 {
		t.Error("another tool's entry was removed too")
	}

	// And removing again is a no-op.
	if res, err := Remove(path); err != nil || len(res.Changed) != 0 {
		t.Errorf("second Remove changed %v (err %v)", res.Changed, err)
	}
}

// TestStartupEventStartsTheDaemon pins the one entry that differs: without
// it there is nothing to launch the display at all.
func TestStartupEventStartsTheDaemon(t *testing.T) {
	path := writeSettings(t, existing)
	if _, err := Install(path, "/bin/nanoclaude"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var commands []string
	for _, e := range gjson.GetBytes(data, "hooks."+startupEvent).Array() {
		if !e.Get(MarkerKey).Bool() {
			continue
		}
		for _, h := range e.Get("hooks").Array() {
			commands = append(commands, h.Get("command").String())
		}
	}

	joined := strings.Join(commands, " ")
	for _, want := range []string{"nanoclaude hook", "nanoclaude up"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s is missing from %s: %v", want, startupEvent, commands)
		}
	}
}

// TestToolEventsCarryAMatcher checks the per-tool events are registered for
// every tool. Without the matcher Claude Code would not fire them.
func TestToolEventsCarryAMatcher(t *testing.T) {
	path := writeSettings(t, existing)
	if _, err := Install(path, "/bin/nanoclaude"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for _, event := range toolEvents {
		for _, e := range gjson.GetBytes(data, "hooks."+event).Array() {
			if !e.Get(MarkerKey).Bool() {
				continue
			}
			if e.Get("matcher").String() != "*" {
				t.Errorf("%s entry has matcher %q, want *", event, e.Get("matcher").String())
			}
		}
	}
	// And the session-level ones must not carry one.
	for _, event := range sessionEvents {
		for _, e := range gjson.GetBytes(data, "hooks."+event).Array() {
			if e.Get(MarkerKey).Bool() && e.Get("matcher").Exists() {
				t.Errorf("%s entry has a matcher and should not", event)
			}
		}
	}
}

func TestRefusesBadInput(t *testing.T) {
	if _, err := Install(filepath.Join(t.TempDir(), "nope.json"), "/bin/x"); !errors.Is(err, ErrNoSettings) {
		t.Errorf("missing file: err = %v, want ErrNoSettings", err)
	}

	// Invalid JSON must be refused rather than overwritten: it is the
	// user's live configuration, and a partial parse would destroy it.
	path := writeSettings(t, `{"hooks": {`)
	if _, err := Install(path, "/bin/x"); err == nil {
		t.Error("invalid JSON was accepted")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != `{"hooks": {` {
		t.Error("the invalid file was modified")
	}
}

func TestSettingsPathHonoursTheConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/claude")
	got, err := SettingsPath()
	if err != nil {
		t.Fatalf("SettingsPath: %v", err)
	}
	if want := "/custom/claude/settings.json"; got != want {
		t.Errorf("SettingsPath() = %q, want %q", got, want)
	}
}
