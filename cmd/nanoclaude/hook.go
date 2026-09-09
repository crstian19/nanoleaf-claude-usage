package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/activity"
	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
)

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hook",
		Short: "Record a Claude Code hook event (called from settings.json)",
		Long: "Reads a hook payload on stdin and records it for the daemon.\n\n" +
			"This runs on the critical path of every tool call, so it always\n" +
			"succeeds from Claude Code's point of view: a wall light must never be\n" +
			"able to interrupt a session.",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Claude Code reads stdout as the hook's response; an
			// empty object means "no opinion, carry on".
			defer func() { _, _ = cmd.OutOrStdout().Write([]byte("{}\n")) }()

			// Every failure below is reported on stderr and none
			// changes the exit code. Claude Code surfaces hook
			// stderr under `claude --debug`, so a broken display is
			// diagnosable, while a zero exit keeps the session
			// unaffected -- returning an error would surface as a
			// hook failure to the user.
			warn := func(err error) error {
				// If even stderr cannot be written there is nothing
				// left to do, and failing here would defeat the
				// whole point of never disturbing the session.
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "nanoclaude hook: %v\n", err)
				return nil
			}

			dir, err := activity.StateDir()
			if err != nil {
				return warn(err)
			}
			if _, err := activity.WriteFromHook(dir, cmd.InOrStdin(), time.Now()); err != nil {
				return warn(err)
			}

			// Every hook revives the display, which is what lets the
			// daemon stand down when idle without becoming
			// unrecoverable: a session left open long enough for its
			// state to go stale will never fire SessionStart again,
			// but it will fire this.
			//
			// Cheap enough for the critical path: a file open and a
			// non-blocking flock, spawning nothing unless the lock
			// is actually free.
			if _, err := daemon.EnsureRunning(); err != nil {
				return warn(err)
			}
			return nil
		},
	}
}
