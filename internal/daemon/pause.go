package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pauseFile is where a pause is remembered.
//
// On disk rather than in the running process, because the process is not the
// thing being paused. A Claude Code hook starts the daemon and every other
// hook brings it back, so a pause held in memory would last until the next
// tool call. This outlives the daemon, which is the whole point.
const pauseFile = "paused"

// PausePath is where the pause is kept.
func PausePath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pauseFile), nil
}

// Pause stops the display until it is resumed, or until the given time.
//
// A zero time pauses it for good, which is what somebody who wants the wall
// dark this evening means.
func Pause(until time.Time) error {
	path, err := PausePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("daemon: create state directory: %w", err)
	}

	body := ""
	if !until.IsZero() {
		body = until.Format(time.RFC3339)
	}
	return writeFileAtomic(path, []byte(body+"\n"))
}

// Resume lets the display take the panels again. Resuming a display that is
// not paused is not an error: it is the state the caller asked for.
func Resume() error {
	path, err := PausePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("daemon: resume: %w", err)
	}
	return nil
}

// Paused reports whether the display is paused, and until when. A zero time
// with paused true means until somebody resumes it.
func Paused() (paused bool, until time.Time, err error) {
	path, err := PausePath()
	if err != nil {
		return false, time.Time{}, err
	}

	raw, err := os.ReadFile(path) //nolint:gosec // fixed path under the user's state directory
	if errors.Is(err, os.ErrNotExist) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, fmt.Errorf("daemon: read pause: %w", err)
	}

	text := strings.TrimSpace(string(raw))
	if text == "" {
		return true, time.Time{}, nil
	}

	deadline, ok := pauseDeadline(text)
	if !ok {
		// Somebody wrote the file by hand and got it wrong. It is a
		// pause without an end rather than no pause at all, because
		// the file exists for one reason: somebody wanted the wall
		// dark.
		return true, time.Time{}, nil
	}
	if time.Now().After(deadline) {
		return false, deadline, nil
	}
	return true, deadline, nil
}

// pauseDeadline reads the time a pause runs out.
func pauseDeadline(text string) (time.Time, bool) {
	deadline, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, false
	}
	return deadline, true
}
