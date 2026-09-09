package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// LogPath returns the file a hook-started daemon writes its log to.
//
// Started by systemd, the daemon logs to stderr and journald collects it.
// Started by a hook there is no collector, and stderr would go to whatever
// Claude Code happened to be holding -- so it goes to a file instead.
func LogPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.log"), nil
}

// EnsureRunning starts the daemon in the background unless one is already
// running, reporting whether it started one.
//
// Called from every hook, so the check has to be cheap: it is a file open and
// a non-blocking flock, and it only spawns anything when the lock is free.
// That is what makes the display self-healing -- once the daemon has stood
// down for idleness, the next prompt or tool call brings it straight back.
func EnsureRunning() (bool, error) {
	pid, err := RunningPID()
	if err != nil {
		return false, err
	}
	if pid != 0 {
		return false, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("daemon: locate own binary: %w", err)
	}

	logPath, err := LogPath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return false, fmt.Errorf("daemon: create state directory: %w", err)
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // fixed path under the user's state directory
	if err != nil {
		return false, fmt.Errorf("daemon: open log: %w", err)
	}
	defer func() { _ = log.Close() }()

	cmd := exec.Command(exe, "run") //nolint:gosec,noctx // our own binary; deliberately outlives this process
	cmd.Stdout = log
	cmd.Stderr = log
	// Setsid detaches the child into its own session, so it survives the
	// hook process exiting and never receives signals meant for Claude
	// Code's terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("daemon: start: %w", err)
	}
	// Deliberately not waited on: the child is meant to outlive this
	// process, and init reaps it once this one exits.
	return true, nil
}

// Stop asks a running daemon to shut down, reporting whether one was there to
// ask. Shutdown is a request, not a kill: the daemon has panels to hand back.
func Stop() (bool, error) {
	pid, err := RunningPID()
	if err != nil {
		return false, err
	}
	switch {
	case pid == 0:
		return false, nil
	case pid < 0:
		return false, errors.New("daemon: running, but its pid could not be read")
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			// Gone between the probe and the signal.
			return false, nil
		}
		return false, fmt.Errorf("daemon: signal %d: %w", pid, err)
	}
	return true, nil
}
