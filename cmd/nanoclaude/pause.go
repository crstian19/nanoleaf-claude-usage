package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
)

func newPauseCmd() *cobra.Command {
	var until time.Duration

	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Keep the panels dark without stopping the display",
		Long: "Hands the panels back and leaves them alone until you resume.\n\n" +
			"`down` is not this. The display is started by Claude Code hooks, so\n" +
			"stopping it lasts until the next tool call brings it back. A pause is\n" +
			"kept in a file, which outlives the process that reads it.\n\n" +
			"Nothing has to be running for this to work. A pause set now is still in\n" +
			"force when the next session starts the display.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var deadline time.Time
			if until > 0 {
				deadline = time.Now().Add(until)
			}
			if err := daemon.Pause(deadline); err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			if deadline.IsZero() {
				o.printf("%s until you run `nanoclaude resume`\n", ui.Good.Render("Paused"))
			} else {
				o.printf("%s until %s\n", ui.Good.Render("Paused"),
					ui.Value.Render(deadline.Format("15:04")))
			}
			o.print(ui.Muted.Render("The panels go back to what they were showing within a second or two.") + "\n")
			return o.Err()
		},
	}

	cmd.Flags().DurationVar(&until, "for", 0, "pause for this long, instead of until you resume")
	return cmd
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Let the display have the panels again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := daemon.Resume(); err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			o.printf("%s\n", ui.Good.Render("Resumed"))

			// Resuming with nothing running is not a mistake, but
			// saying nothing would look like one.
			pid, err := daemon.RunningPID()
			if err != nil {
				return err
			}
			if pid == 0 {
				o.print(ui.Muted.Render("The display is not running. Claude Code starts it, or `nanoclaude up` does.") + "\n")
			}
			return o.Err()
		},
	}
}

// pauseLine is how `status` says the display is paused, or nothing at all
// when it is not.
func pauseLine() (string, error) {
	paused, until, err := daemon.Paused()
	if err != nil {
		return "", err
	}
	if !paused {
		return "", nil
	}
	if until.IsZero() {
		return fmt.Sprintf("%s until you run `nanoclaude resume`\n", ui.Warn.Render("paused")), nil
	}
	return fmt.Sprintf("%s until %s\n", ui.Warn.Render("paused"), until.Format("15:04")), nil
}
