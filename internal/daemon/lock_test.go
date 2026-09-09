package daemon

import (
	"errors"
	"os"
	"testing"
)

// TestLockIsExclusive is the property that keeps three Claude Code sessions
// from starting three daemons that fight over the same panels at 20fps.
func TestLockIsExclusive(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	first, err := AcquireLock()
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}

	if _, err := AcquireLock(); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second AcquireLock err = %v, want ErrAlreadyRunning", err)
	}

	// Released, the lock is available again -- which is what lets the
	// daemon stand down when idle and be restarted by the next hook.
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	again, err := AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock after release: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

// TestRunningPID reports the holder, and reports nothing when the lock is
// free -- including when a stale PID is still written in the file, since the
// lock and not its contents is the source of truth.
func TestRunningPID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if pid, err := RunningPID(); err != nil || pid != 0 {
		t.Fatalf("RunningPID with no lock file = %d, %v; want 0", pid, err)
	}

	lock, err := AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	pid, err := RunningPID()
	if err != nil {
		t.Fatalf("RunningPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("RunningPID() = %d, want %d", pid, os.Getpid())
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	// The file still holds the old PID, but nothing is running.
	if pid, err := RunningPID(); err != nil || pid != 0 {
		t.Errorf("RunningPID after release = %d, %v; want 0 despite the stale pid in the file", pid, err)
	}
}

// TestReleaseIsSafeOnZeroValue guards the shutdown path, which releases
// whatever it has.
func TestReleaseIsSafeOnZeroValue(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Errorf("Release on a nil lock: %v", err)
	}
	if err := (&Lock{}).Release(); err != nil {
		t.Errorf("Release on an empty lock: %v", err)
	}
}
