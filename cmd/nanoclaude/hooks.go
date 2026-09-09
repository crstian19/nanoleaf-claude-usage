package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/hooks"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
)

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Manage the Claude Code hooks that run the display",
		Long: "The hooks do two jobs: they report what Claude is doing, and they start\n" +
			"the display. Between them the daemon needs no service manager and no\n" +
			"autostart entry.\n\n" +
			"Entries this program wrote are marked, so installing is safe to repeat\n" +
			"and removing leaves other tools' hooks alone. The settings file is\n" +
			"edited in place, so keys it does not touch keep their order.",
	}
	cmd.AddCommand(newHooksInstallCmd(), newHooksRemoveCmd(), newHooksStatusCmd())
	return cmd
}

func newHooksInstallCmd() *cobra.Command {
	var (
		settings string
		binary   string
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Add the hooks to Claude Code's settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settingsPath(settings)
			if err != nil {
				return err
			}
			bin, err := binaryPath(binary)
			if err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			if dryRun {
				missing, err := pendingEvents(path)
				if err != nil {
					return err
				}
				if len(missing) == 0 {
					o.print(ui.Good.Render("Already installed.") + "\n")
					return o.Err()
				}
				o.printf("Would add %d events to %s:\n", len(missing), ui.Value.Render(path))
				for _, e := range missing {
					o.printf("  %s\n", e)
				}
				return o.Err()
			}

			res, err := hooks.Install(path, bin)
			if err != nil {
				return err
			}
			if len(res.Changed) == 0 {
				o.print(ui.Good.Render("Already installed.") + "\n")
				return o.Err()
			}

			o.printf("%s %s\n", ui.Good.Render("Installed"), ui.Muted.Render(fmt.Sprintf("(%d events)", len(res.Changed))))
			o.printf("  command  %s\n", ui.Value.Render(bin))
			o.printf("  settings %s\n", ui.Value.Render(path))
			o.printf("  backup   %s\n", ui.Muted.Render(res.Backup))
			o.print("\n" + ui.Muted.Render("The display starts with your next Claude Code session.") + "\n")
			return o.Err()
		},
	}

	cmd.Flags().StringVar(&settings, "settings", "", "path to settings.json (default: Claude Code's own)")
	cmd.Flags().StringVar(&binary, "binary", "", "path to write into the hooks (default: this binary)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change and exit")
	return cmd
}

func newHooksRemoveCmd() *cobra.Command {
	var settings string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Take the hooks out again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settingsPath(settings)
			if err != nil {
				return err
			}

			res, err := hooks.Remove(path)
			if err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			if len(res.Changed) == 0 {
				o.print("Not installed.\n")
				return o.Err()
			}
			o.printf("%s %s\n", ui.Good.Render("Removed"), ui.Muted.Render(fmt.Sprintf("(%d events)", len(res.Changed))))
			o.printf("  backup %s\n", ui.Muted.Render(res.Backup))
			o.print("\n" + ui.Muted.Render("Nothing starts the display now. Use `nanoclaude up` by hand.") + "\n")
			return o.Err()
		},
	}

	cmd.Flags().StringVar(&settings, "settings", "", "path to settings.json (default: Claude Code's own)")
	return cmd
}

func newHooksStatusCmd() *cobra.Command {
	var settings string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report which events carry the hooks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settingsPath(settings)
			if err != nil {
				return err
			}
			ours, others, err := hooks.Status(path)
			if err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			o.printf("%s\n\n", ui.Muted.Render(path))
			for _, event := range hooks.Events() {
				mark := ui.Bad.Render("missing")
				if ours[event] > 0 {
					mark = ui.Good.Render("installed")
				}
				line := fmt.Sprintf("  %-20s %s", event, mark)
				if others[event] > 0 {
					line += ui.Muted.Render(fmt.Sprintf("  (+%d from other tools)", others[event]))
				}
				o.print(line + "\n")
			}
			return o.Err()
		},
	}

	cmd.Flags().StringVar(&settings, "settings", "", "path to settings.json (default: Claude Code's own)")
	return cmd
}

// pendingEvents lists the events that do not carry the hooks yet.
func pendingEvents(path string) ([]string, error) {
	ours, _, err := hooks.Status(path)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, e := range hooks.Events() {
		if ours[e] == 0 {
			missing = append(missing, e)
		}
	}
	return missing, nil
}

func settingsPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return hooks.SettingsPath()
}

// binaryPath returns the absolute path to write into the hooks.
//
// Its own path by default, because a hook runs with whatever environment
// Claude Code has and cannot rely on the binary being on PATH.
func binaryPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("hooks: locate own binary: %w", err)
	}
	return exe, nil
}
