package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrAlreadyRunning means another daemon holds the lock.
var ErrAlreadyRunning = errors.New("daemon: already running")

// Lock is an exclusive single-instance lock, held for as long as the process
// runs.
//
// It exists because the daemon is started by a Claude Code hook, which fires
// once per session: opening three worktrees would otherwise start three
// daemons, all streaming different frames to the same panels at 20fps.
//
// An advisory flock is used rather than a bare PID file because the kernel
// releases it when the process dies however it dies -- crash, SIGKILL, power
// cut. A PID file left behind by a crash would block every future start until
// someone deleted it by hand.
type Lock struct {
	f *os.File
}

// LockPath returns the lock's location.
func LockPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.lock"), nil
}

// stateDir returns the daemon's own state directory, honouring
// XDG_STATE_HOME.
func stateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("daemon: locate home: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "nanoclaude"), nil
}

// AcquireLock takes the single-instance lock, returning ErrAlreadyRunning
// when another daemon already holds it.
func AcquireLock() (*Lock, error) {
	path, err := LockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create state directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // fixed path under the user's state directory
	if err != nil {
		return nil, fmt.Errorf("daemon: open lock: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("daemon: lock: %w", err)
	}

	// The PID is written for the benefit of `status` and `down`; the lock
	// itself does not depend on it, so a stale value can never wedge
	// anything.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("daemon: truncate lock: %w", err)
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("daemon: write pid: %w", err)
	}

	return &Lock{f: f}, nil
}

// Release drops the lock. The kernel would do it at exit anyway; this makes
// the handover prompt when the daemon shuts down cleanly.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// RunningPID reports the PID of the running daemon, or zero if none holds the
// lock.
//
// The lock, not the file's contents, is the source of truth: if it can be
// taken then nobody is running, whatever PID the file happens to contain.
func RunningPID() (int, error) {
	path, err := LockPath()
	if err != nil {
		return 0, err
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0o600) //nolint:gosec // fixed path under the user's state directory
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("daemon: open lock: %w", err)
	}
	defer func() { _ = f.Close() }()

	// A successful lock means nothing is running; drop it straight away so
	// this stays a read-only probe.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0, nil
	}

	raw := make([]byte, 32)
	n, err := f.ReadAt(raw, 0)
	if n == 0 && err != nil {
		// Locked but unreadable: something is running, we just cannot
		// say what.
		return -1, nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw[:n])))
	if err != nil {
		return -1, nil
	}
	return pid, nil
}
