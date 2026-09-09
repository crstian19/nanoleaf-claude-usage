package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
)

func newRunCmd() *cobra.Command {
	var debug bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the display daemon",
		Long: "Runs until interrupted, then restores the panels to how it found them.\n\n" +
			"Configuration comes from the environment: NANOCLAUDE_NANOLEAF_HOST,\n" +
			"NANOCLAUDE_NANOLEAF_TOKEN, and optionally HASS_SERVER / HASS_TOKEN to\n" +
			"gate the display on a Home Assistant toggle.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			level := slog.LevelInfo
			if debug {
				level = slog.LevelDebug
			}
			// Logs go to stderr so stdout stays clean, and in text
			// form because this is read via journalctl.
			log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

			cfg, err := daemon.ConfigFromEnv()
			if err != nil {
				return err
			}
			d, err := daemon.New(cfg, log)
			if err != nil {
				return err
			}
			if err := d.Run(cmd.Context()); err != nil {
				// Losing the race to another session's hook is
				// the normal outcome, not a failure.
				if errors.Is(err, daemon.ErrAlreadyRunning) {
					log.Info("another display is already running; nothing to do")
					return nil
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "log at debug level")
	return cmd
}
