package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
)

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start the display in the background if it is not already running",
		Long: "Starts the daemon detached and returns immediately.\n\n" +
			"Safe to call repeatedly and from several places at once: the daemon\n" +
			"holds a single-instance lock, so extra attempts do nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			started, err := daemon.EnsureRunning()
			if err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			if !started {
				o.print("already running\n")
				return o.Err()
			}
			logPath, err := daemon.LogPath()
			if err != nil {
				return err
			}
			o.printf("started; logging to %s\n", logPath)
			return o.Err()
		},
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the display and hand the panels back",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stopped, err := daemon.Stop()
			if err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			if !stopped {
				o.print("not running\n")
				return o.Err()
			}
			// The daemon restores the panels on the way out, which is
			// why this is a request rather than a kill.
			o.print("asked to stop; the panels will be restored\n")
			return o.Err()
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether the display is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pid, err := daemon.RunningPID()
			if err != nil {
				return err
			}
			logPath, logErr := daemon.LogPath()
			if logErr != nil {
				return logErr
			}
			cfgPath, cfgErr := daemon.ConfigFilePath()
			if cfgErr != nil {
				return cfgErr
			}

			o := newOut(cmd.OutOrStdout())
			switch {
			case pid == 0:
				o.print("not running\n")
			case pid < 0:
				o.print("running (pid unknown)\n")
			default:
				o.printf("running, pid %d\n", pid)
			}
			o.printf("config: %s\n", cfgPath)
			o.printf("log:    %s\n", logPath)
			if err := o.Err(); err != nil {
				return err
			}
			if pid == 0 {
				// A non-zero exit so `nanoclaude status` can be
				// used in a shell test without parsing output.
				return fmt.Errorf("nanoclaude: not running")
			}
			return nil
		},
	}
}
