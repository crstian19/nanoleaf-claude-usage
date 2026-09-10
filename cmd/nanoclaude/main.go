// Command nanoclaude drives Nanoleaf panels from Claude Code usage.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// buildVersion is what `nanoclaude --version` reports.
//
// A release sets version through -ldflags and `just build` sets it from git,
// but neither happens for `go install`, which is how the README tells people
// to install this. So the module version the toolchain stamps into the binary
// is the fallback. Without it every installed copy calls itself "dev", and a
// bug report cannot say which one it came from.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	// "(devel)" is what a build from a working tree reports, which is no
	// more use than "dev" and less honest about it.
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		return version
	}
	return info.Main.Version
}

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

	if err := fang.Execute(ctx, newRootCmd(), fang.WithVersion(buildVersion())); err != nil {
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "nanoclaude",
		Short: "Show Claude Code usage on Nanoleaf panels",
		Long: "nanoclaude shows how much of your Claude Code session you have used,\n" +
			"on a wall of Nanoleaf panels.\n\n" +
			"Each panel owns an equal share of the allowance. A panel lights up when\n" +
			"you spend its share, and its colour says where it sits on the scale.\n" +
			"While Claude works, every panel turns into a rainbow.\n\n" +
			"Start with `nanoclaude setup`.",
		SilenceUsage: true,
	}

	root.AddCommand(
		newSetupCmd(),
		newRunCmd(),
		newPairCmd(),
		newDiscoverCmd(),
		newLayoutCmd(),
		newCalibrateCmd(),
		newBrightnessCmd(),
		newPreviewCmd(),
		newHookCmd(),
		newHooksCmd(),
		newUpCmd(),
		newDownCmd(),
		newStatusCmd(),
	)
	return root
}
