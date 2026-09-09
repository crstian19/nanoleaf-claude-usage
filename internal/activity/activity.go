// Package activity tracks what Claude Code is doing, by way of hook events.
//
// Claude Code hooks are short-lived processes, so they cannot hold state or
// talk to the daemon over a socket without slowing every tool call down.
// Instead each hook drops one small file naming the latest event for its
// session, and the daemon reads the directory. One file per session means
// concurrent sessions (worktrees, subagents) never overwrite each other.
package activity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Event is a hook event name, as Claude Code spells it.
type Event string

// The hook events the display reacts to.
const (
	EventPrompt      Event = "UserPromptSubmit"
	EventPreTool     Event = "PreToolUse"
	EventPostTool    Event = "PostToolUse"
	EventToolFailure Event = "PostToolUseFailure"
	EventPermission  Event = "PermissionRequest"
	EventStop        Event = "Stop"
	EventStopFailure Event = "StopFailure"
	EventSessionEnd  Event = "SessionEnd"
)

// Phase is the aggregate state across every live session.
type Phase int

const (
	// PhaseIdle means nothing is running, or Claude is waiting on the user.
	PhaseIdle Phase = iota
	// PhaseThinking means a model response is being generated.
	PhaseThinking
	// PhaseTool means at least one tool call is in flight.
	PhaseTool
	// PhaseError means something failed, or a permission prompt is blocking.
	PhaseError
)

// String makes Phase readable in log lines and test failures.
func (p Phase) String() string {
	switch p {
	case PhaseThinking:
		return "thinking"
	case PhaseTool:
		return "tool"
	case PhaseError:
		return "error"
	case PhaseIdle:
		return "idle"
	default:
		return "unknown"
	}
}

// Record is one session's latest event, as written by a hook.
type Record struct {
	SessionID string    `json:"session_id"`
	Event     Event     `json:"event"`
	Tool      string    `json:"tool,omitempty"`
	At        time.Time `json:"at"`
}

// Snapshot is the aggregated view the renderer consumes.
type Snapshot struct {
	Phase Phase
	// Sessions is how many sessions reported activity recently.
	Sessions int
	// Tool names the tool running, when exactly one is.
	Tool string
}

const (
	// staleAfter is how long a session's file stays meaningful. A crashed
	// or force-quit session leaves its last file behind forever, and
	// without this the display would show a phantom session for good.
	staleAfter = 3 * time.Minute

	// errorHold keeps an error visible long enough to notice, since the
	// next event often arrives within milliseconds.
	errorHold = 8 * time.Second
)

// safeSessionID validates a session identifier before it is used as a
// filename. Hook payloads come from outside this process, so a session ID
// containing a path separator or "..", could otherwise be steered into
// writing anywhere on disk.
func safeSessionID(id string) (string, error) {
	if id == "" {
		return "", errors.New("activity: empty session id")
	}
	if len(id) > 128 {
		return "", errors.New("activity: session id too long")
	}
	for _, r := range id {
		ok := r == '-' || r == '_' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
		if !ok {
			return "", fmt.Errorf("activity: session id contains %q", r)
		}
	}
	return id, nil
}

// StateDir returns the directory session files live in, honouring
// XDG_STATE_HOME.
func StateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("activity: locate home: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "nanoclaude", "sessions"), nil
}

// phaseOf maps a single event to the phase it implies.
func phaseOf(e Event) Phase {
	switch e {
	case EventPreTool:
		return PhaseTool
	case EventPrompt, EventPostTool:
		return PhaseThinking
	case EventToolFailure, EventPermission, EventStopFailure:
		return PhaseError
	case EventStop, EventSessionEnd:
		return PhaseIdle
	default:
		return PhaseIdle
	}
}

// Read aggregates every live session's latest event into one snapshot.
//
// A missing directory is not an error: it just means no hook has fired yet.
//
// Age comes from the timestamp inside each record, which is authoritative
// here; Sweep uses the file's mtime instead, because a file it cannot decode
// still has to be collectable. The two agree in practice since both are set
// in the same instant, and only diverge if the directory is copied around.
func Read(dir string, now time.Time) (Snapshot, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{Phase: PhaseIdle}, nil
		}
		return Snapshot{}, fmt.Errorf("activity: read %s: %w", dir, err)
	}

	snap := Snapshot{Phase: PhaseIdle}
	var tools []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, err := readRecord(filepath.Join(dir, e.Name()))
		if err != nil {
			// One unreadable or half-written file must not blank the
			// whole display; skip it and let the next frame retry.
			continue
		}
		age := now.Sub(rec.At)
		if age > staleAfter || age < -time.Minute {
			continue
		}

		snap.Sessions++
		ph := phaseOf(rec.Event)
		// An error is held briefly, then stops masking real activity.
		if ph == PhaseError && age > errorHold {
			ph = PhaseThinking
		}
		if ph > snap.Phase {
			snap.Phase = ph
		}
		if ph == PhaseTool && rec.Tool != "" {
			tools = append(tools, rec.Tool)
		}
	}

	if len(tools) == 1 {
		snap.Tool = tools[0]
	}
	return snap, nil
}
