package activity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// maxHookPayload caps the hook's stdin read.
//
// Generous on purpose. Hook payloads embed tool inputs -- a PreToolUse for a
// Write carries the whole file -- and JSON has to be complete to decode at
// all, so a cap that truncates does not degrade the event, it discards it: no
// pulse for exactly the big operations most worth seeing. io.ReadAll grows on
// demand, so an ordinary few-kilobyte payload still costs kilobytes, and the
// hook process exits immediately either way.
const maxHookPayload = 32 << 20

// hookPayload is the subset of Claude Code's hook JSON that matters here.
type hookPayload struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name"`
}

// WriteFromHook reads a hook payload from r and records it for the daemon.
//
// It returns the record written so a caller can log it. Errors are the
// caller's to swallow: a hook that fails loudly would interrupt the user's
// session over a wall light.
func WriteFromHook(dir string, r io.Reader, now time.Time) (Record, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxHookPayload))
	if err != nil {
		return Record{}, fmt.Errorf("activity: read hook payload: %w", err)
	}

	var p hookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Record{}, fmt.Errorf("activity: decode hook payload: %w", err)
	}

	id, err := safeSessionID(p.SessionID)
	if err != nil {
		return Record{}, err
	}
	if p.HookEventName == "" {
		return Record{}, errors.New("activity: hook payload has no event name")
	}

	rec := Record{
		SessionID: id,
		Event:     Event(p.HookEventName),
		Tool:      p.ToolName,
		At:        now,
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Record{}, fmt.Errorf("activity: create %s: %w", dir, err)
	}
	if err := writeRecord(filepath.Join(dir, id+".json"), rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// writeRecord writes a record atomically, so the daemon never reads a
// half-written file.
func writeRecord(path string, rec Record) error {
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("activity: encode record: %w", err)
	}

	// The temp file is created in the destination directory so the rename
	// stays on one filesystem, and with the final permissions already set
	// rather than being widened and narrowed again.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("activity: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Harmless once the rename has succeeded; the cleanup that
		// matters is on the failure paths below.
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("activity: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("activity: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("activity: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("activity: install record: %w", err)
	}
	return nil
}

// readRecord loads one session file.
func readRecord(path string) (Record, error) {
	// The path is built from our own 0700 state directory plus a name that
	// came from reading it, never from a payload; session ids are validated
	// long before they reach a filename.
	f, err := os.Open(path) //nolint:gosec // path is derived from our own state directory
	if err != nil {
		return Record{}, fmt.Errorf("activity: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var rec Record
	if err := json.NewDecoder(io.LimitReader(f, maxHookPayload)).Decode(&rec); err != nil {
		return Record{}, fmt.Errorf("activity: decode %s: %w", path, err)
	}
	return rec, nil
}

// Sweep deletes session files that went stale, including leftovers from
// sessions that were killed rather than ended.
func Sweep(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("activity: read %s: %w", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > staleAfter {
			// Ignoring the error is deliberate: a file that cannot be
			// removed is retried on the next sweep, and Read already
			// ignores it on age, so a failure here costs nothing.
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}
