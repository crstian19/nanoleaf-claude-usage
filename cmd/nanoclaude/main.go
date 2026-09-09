// Command nanoclaude drives Nanoleaf panels from Claude Code usage.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	// os.Exit skips deferred calls, so the signal handler is released in
	// run() and the exit code is decided out here.
	os.Exit(run())
}

func run() int {
	// Signals are handled here rather than in the daemon so every
	// subcommand is interruptible, and so the daemon's own shutdown path
	// (handing the panels back) is driven by plain context cancellation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := fang.Execute(ctx, newRootCmd(), fang.WithVersion(version)); err != nil {
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "nanoclaude",
		Short: "Show Claude Code usage on Nanoleaf panels",
		Long: "nanoclaude renders Claude Code token usage onto Nanoleaf panels.\n\n" +
			"The five-hour usage window fills the mounted shape from the bottom up,\n" +
			"coloured green through red as it fills, and a pulse travels along the\n" +
			"shape while Claude is working.",
		SilenceUsage: true,
	}

	root.AddCommand(
		newRunCmd(),
		newPairCmd(),
		newLayoutCmd(),
		newCalibrateCmd(),
		newPreviewCmd(),
		newHookCmd(),
		newUpCmd(),
		newDownCmd(),
		newStatusCmd(),
	)
	return root
}
