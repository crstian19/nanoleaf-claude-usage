// Package hooks registers the daemon's Claude Code hooks.
//
// The hooks do two jobs: they report what Claude is doing, and they start the
// display. Between them the daemon needs no service manager and no autostart
// entry, so this is the only installation step that is not just copying a
// binary.
//
// Edits are made in place with a JSON path library rather than by decoding
// and re-encoding the whole file. That is not a style preference. A user's
// settings.json is hand-maintained -- the author's carries keys like
// _model_COMENTADO, kept in a deliberate order -- and Go marshals a map with
// its keys sorted, so a decode-and-re-encode would silently reshuffle a 26 KB
// configuration file to add nine lines. Everything outside the paths touched
// here is left byte for byte.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MarkerKey is stamped into every entry this package writes.
//
// A marker rather than a match on the command text, because the command
// contains the binary's path: a user who installs it elsewhere would get a
// second copy of every hook on the next install, and removal would miss them.
const MarkerKey = "_nanoclaude"

// maxSettings caps the read. A settings file is tens of kilobytes.
const maxSettings = 8 << 20

// The events the display reacts to. The tool events are matched against every
// tool, so they carry a matcher; the session-level ones take none.
var (
	toolEvents = []string{
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"PermissionRequest",
	}
	sessionEvents = []string{
		"UserPromptSubmit",
		"Stop",
		"StopFailure",
		"SessionEnd",
	}
)

// startupEvent both records the session and brings the display up.
//
// Every other event revives the daemon too, which is what lets it shut down
// when idle without becoming unrecoverable: a session left open long enough
// for its state file to expire never fires SessionStart again.
const startupEvent = "SessionStart"

// Events lists every event this package writes to.
func Events() []string {
	out := make([]string, 0, len(toolEvents)+len(sessionEvents)+1)
	out = append(out, toolEvents...)
	out = append(out, sessionEvents...)
	return append(out, startupEvent)
}

// ErrNoSettings means Claude Code has no settings file to write to.
var ErrNoSettings = errors.New("hooks: no Claude Code settings file")

// SettingsPath returns the default location of that file.
func SettingsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("hooks: locate home: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// Result reports what an install or a remove did.
type Result struct {
	// Changed lists the events whose entries were added or removed.
	Changed []string
	// Backup is the copy taken before writing, empty when nothing changed.
	Backup string
}

// Install adds the hooks, leaving every entry that is already there alone.
//
// Other tools register hooks on the same events, so an install that replaced
// them would break those tools. Running it twice changes nothing.
func Install(path, binary string) (Result, error) {
	return edit(path, func(event string, entries []gjson.Result) ([]string, bool) {
		for _, e := range entries {
			if e.Get(MarkerKey).Bool() {
				return nil, false
			}
		}
		kept := raw(entries)
		return append(kept, entry(event, binary)), true
	})
}

// Remove takes the hooks out again.
func Remove(path string) (Result, error) {
	return edit(path, func(_ string, entries []gjson.Result) ([]string, bool) {
		var kept []string
		var dropped bool
		for _, e := range entries {
			if e.Get(MarkerKey).Bool() {
				dropped = true
				continue
			}
			kept = append(kept, e.Raw)
		}
		return kept, dropped
	})
}

// Status reports how many of this package's entries each event carries, and
// how many belong to something else.
func Status(path string) (ours, others map[string]int, err error) {
	data, err := read(path)
	if err != nil {
		return nil, nil, err
	}

	ours, others = map[string]int{}, map[string]int{}
	for _, event := range Events() {
		for _, e := range gjson.GetBytes(data, "hooks."+event).Array() {
			if e.Get(MarkerKey).Bool() {
				ours[event]++
			} else {
				others[event]++
			}
		}
	}
	return ours, others, nil
}

// edit applies fn to every event's entry list and writes the file if any
// event changed.
func edit(path string, fn func(event string, entries []gjson.Result) ([]string, bool)) (Result, error) {
	data, err := read(path)
	if err != nil {
		return Result{}, err
	}

	var res Result
	updated := data
	for _, event := range Events() {
		entries := gjson.GetBytes(updated, "hooks."+event).Array()

		next, changed := fn(event, entries)
		if !changed {
			continue
		}
		res.Changed = append(res.Changed, event)

		updated, err = sjson.SetRawBytes(updated, "hooks."+event, []byte("["+strings.Join(next, ",")+"]"))
		if err != nil {
			return Result{}, fmt.Errorf("hooks: update %s: %w", event, err)
		}
	}

	if len(res.Changed) == 0 {
		return res, nil
	}

	res.Backup, err = write(path, data, updated)
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// entry builds one hook entry.
func entry(event, binary string) string {
	// A short timeout: this runs on the critical path of every tool call,
	// and the binary writes one small file and exits.
	commands := []map[string]any{
		{"type": "command", "command": binary + " hook", "timeout": 5},
	}
	if event == startupEvent {
		// `up` starts the daemon detached and returns without waiting,
		// so a session is never held up by the display.
		commands = append(commands,
			map[string]any{"type": "command", "command": binary + " up", "timeout": 10})
	}

	e := map[string]any{MarkerKey: true, "hooks": commands}
	for _, tool := range toolEvents {
		if tool == event {
			e["matcher"] = "*"
			break
		}
	}

	// The map is small and fixed, so marshalling cannot fail.
	out, _ := json.Marshal(e)
	return string(out)
}

// raw returns the entries unchanged, as JSON text.
func raw(entries []gjson.Result) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Raw)
	}
	return out
}

func read(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the user's own settings file
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoSettings
		}
		return nil, fmt.Errorf("hooks: read settings: %w", err)
	}
	if len(data) > maxSettings {
		return nil, fmt.Errorf("hooks: settings file is %d bytes, refusing", len(data))
	}
	if !gjson.ValidBytes(data) {
		return nil, errors.New("hooks: settings file is not valid JSON")
	}
	return data, nil
}

// write backs the file up and replaces it atomically.
//
// Atomically because the file being rewritten is Claude Code's own
// configuration: a crash halfway through a plain write would truncate it, and
// a backup is a recovery, not a substitute.
func write(path string, before, after []byte) (string, error) {
	backup := path + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(backup, before, 0o600); err != nil {
		return "", fmt.Errorf("hooks: write backup: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return "", fmt.Errorf("hooks: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("hooks: chmod temp file: %w", err)
	}
	// A trailing newline, so the file stays diffable and plays nicely with
	// text tools.
	if _, err := tmp.Write(append(after, '\n')); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("hooks: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("hooks: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("hooks: install settings: %w", err)
	}
	return backup, nil
}
