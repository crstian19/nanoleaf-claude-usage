package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
)

func newBrightnessCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "brightness [0-100]",
		Short: "Report or set the panels' global brightness",
		Long: "With no argument, reports the brightness the panels are set to. With a\n" +
			"number, sets it.\n\n" +
			"The display never changes this by itself. Every colour it sends is\n" +
			"scaled by this value, so taking it over would give a predictable\n" +
			"canvas, and that was tried and reverted: the value belongs to whoever\n" +
			"set it, in the Nanoleaf app or in a home automation. A daemon that\n" +
			"restarts with every Claude Code session would overwrite that choice\n" +
			"several times an hour.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := daemon.LeafFromEnv()
			if err != nil {
				return err
			}
			o := newOut(cmd.OutOrStdout())

			if len(args) == 0 {
				state, err := client.State(cmd.Context())
				if err != nil {
					return err
				}
				o.printf("%s\n", ui.Value.Render(fmt.Sprintf("%d", state.Brightness)))
				return o.Err()
			}

			pct, err := strconv.Atoi(args[0])
			if err != nil || pct < 0 || pct > 100 {
				return fmt.Errorf("brightness: want a number from 0 to 100, got %q", args[0])
			}
			if err := client.SetBrightness(cmd.Context(), pct); err != nil {
				return err
			}
			o.printf("%s %s\n", ui.Good.Render("Brightness"), ui.Value.Render(fmt.Sprintf("%d", pct)))
			return o.Err()
		},
	}
}
