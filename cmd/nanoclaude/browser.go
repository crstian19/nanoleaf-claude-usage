package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser asks the desktop to open a URL.
//
// The command is looked up on PATH and the URL is passed as one argument, so
// nothing here goes through a shell. The address always comes from the server
// that just started, never from a user, but keeping it out of a shell means a
// future caller cannot make it a problem either.
func openBrowser(url string) error {
	var argv []string
	switch runtime.GOOS {
	case "darwin":
		argv = []string{"open", url}
	case "windows":
		argv = []string{"rundll32", "url.dll,FileProtocolHandler", url}
	default:
		// xdg-open honours the BROWSER variable and the desktop's own
		// default, which is more likely to be right than any list of
		// browsers this program could carry.
		argv = []string{"xdg-open", url}
	}

	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("no %s on PATH: open the address yourself", argv[0])
	}

	// Background, deliberately: the browser must outlive this command. A
	// context tied to the session would close the user's tab the moment
	// calibration finished.
	cmd := exec.CommandContext(context.Background(), path, argv[1:]...) //nolint:gosec // fixed program, single URL argument
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	// The exit status would say nothing useful: a tab opened in an
	// already-running browser returns immediately. Reaped so the process
	// does not linger as a zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}
