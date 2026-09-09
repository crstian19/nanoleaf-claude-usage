package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/hooks"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// pairWait is how long setup keeps asking the panels for a token.
const pairWait = 3 * time.Minute

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Set everything up, start to finish",
		Long: "Finds the panels, pairs with them, writes the configuration, checks the\n" +
			"shape is the right way up, and installs the Claude Code hooks.\n\n" +
			"Every step is also a command of its own, so anything this does can be\n" +
			"done or redone by hand.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !ui.Interactive(cmd.OutOrStdout()) {
				return errors.New("setup: needs a terminal; see `nanoclaude discover`, `pair` and `hooks install`")
			}
			return runSetup(cmd)
		},
	}
}

func runSetup(cmd *cobra.Command) error {
	ctx := cmd.Context()
	o := newOut(cmd.OutOrStdout())

	host, err := chooseHost(ctx, cmd)
	if err != nil {
		return err
	}

	token, err := pairWithPanels(ctx, cmd, host)
	if err != nil {
		return err
	}
	o.printf("%s %s\n\n", ui.Good.Render("Paired with"), ui.Value.Render(host))

	hass, err := askHomeAssistant(ctx)
	if err != nil {
		return err
	}

	settings := make([]daemon.Setting, 0, 2+len(hass))
	settings = append(settings,
		daemon.Setting{Key: daemon.EnvNanoleafHost, Value: host, Comment: "The panel controller on your network."},
		daemon.Setting{Key: daemon.EnvNanoleafToken, Value: token, Comment: "From `nanoclaude pair`. Panels hold several tokens at once."},
	)
	settings = append(settings, hass...)

	path, err := daemon.ConfigFilePath()
	if err != nil {
		return err
	}
	if err := daemon.WriteConfigFile(path, settings); err != nil {
		return err
	}
	o.printf("%s %s\n\n", ui.Good.Render("Wrote"), ui.Value.Render(path))

	if err := offerCalibrate(ctx, cmd, host, token); err != nil {
		return err
	}
	if err := offerHooks(ctx, o); err != nil {
		return err
	}

	o.print("\n" + ui.Title.Render("Done.") + "\n")
	o.print(ui.Muted.Render("The display starts with your next Claude Code session.") + "\n")
	o.print(ui.Muted.Render("`nanoclaude status` says whether it runs, `nanoclaude down` stops it.") + "\n")
	return o.Err()
}

// chooseHost finds the panels and asks which one to use.
func chooseHost(ctx context.Context, cmd *cobra.Command) (string, error) {
	panels, err := findPanels(ctx, cmd)
	if err != nil {
		return "", err
	}

	const manual = ""
	options := make([]huh.Option[string], 0, len(panels)+1)
	for _, p := range panels {
		label := p.Host
		if p.Name != "" {
			label += "  (" + p.Name + ")"
		}
		options = append(options, huh.NewOption(label, p.Host))
	}
	options = append(options, huh.NewOption("Enter an address by hand", manual))

	choice := manual
	if len(panels) > 0 {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which panels?").
				Description(fmt.Sprintf("Found %d on your network.", len(panels))).
				Options(options...).
				Value(&choice),
		)).RunWithContext(ctx); err != nil {
			return "", err
		}
	}
	if choice != manual {
		return choice, nil
	}

	var typed string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Address of the panel controller").
			Placeholder("192.168.1.50").
			Value(&typed).
			Validate(notBlank("an address")),
	)).RunWithContext(ctx); err != nil {
		return "", err
	}
	return strings.TrimSpace(typed), nil
}

// pairWithPanels waits for the panels to be put into pairing mode.
func pairWithPanels(ctx context.Context, cmd *cobra.Command, host string) (string, error) {
	ready := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("Put the panels into pairing mode").
			Description("Hold the power button on the controller for 5 to 7 seconds,\n"+
				"until the LEDs flash.\n\n"+
				"This does not revoke any other token. The panels hold several\n"+
				"at once, so Home Assistant and the Nanoleaf app keep working."),
		huh.NewConfirm().
			Title("Are the LEDs flashing?").
			Affirmative("Yes, pair now").
			Negative("Cancel").
			Value(&ready),
	)).RunWithContext(ctx); err != nil {
		return "", err
	}
	if !ready {
		return "", errors.New("setup: cancelled")
	}

	return spin(ctx, cmd, "Waiting for the panels...", func(ctx context.Context) (string, error) {
		deadline := time.Now().Add(pairWait)
		for {
			token, err := nanoleaf.Pair(ctx, host)
			switch {
			case err == nil:
				return token, nil
			case !errors.Is(err, nanoleaf.ErrNotPairing):
				return "", err
			}

			if time.Now().After(deadline) {
				return "", fmt.Errorf("setup: %s did not enter pairing mode within %s", host, pairWait)
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	})
}

// askHomeAssistant collects the optional Home Assistant settings.
func askHomeAssistant(ctx context.Context) ([]daemon.Setting, error) {
	use := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Gate the display on a Home Assistant switch?").
			Description("Optional. With it, an input_boolean turns the display on and off.\n" +
				"Without it, the display runs whenever Claude Code does.").
			Value(&use),
	)).RunWithContext(ctx); err != nil {
		return nil, err
	}
	if !use {
		return nil, nil
	}

	var server, token, entity string
	entity = daemon.DefaultToggleEntity

	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Home Assistant address").
			Placeholder("https://homeassistant.example.com").
			Description("Use https. A long-lived token has full access and never expires,\n"+
				"and the daemon sends it on every poll.").
			Value(&server).
			Validate(notBlank("an address")),
		huh.NewInput().
			Title("Long-lived access token").
			EchoMode(huh.EchoModePassword).
			Value(&token).
			Validate(notBlank("a token")),
		huh.NewInput().
			Title("Switch entity").
			Description("Create an input_boolean helper in Home Assistant with this id.").
			Value(&entity).
			Validate(notBlank("an entity id")),
	)).RunWithContext(ctx); err != nil {
		return nil, err
	}

	return []daemon.Setting{
		{Key: daemon.EnvHassServer, Value: strings.TrimSpace(server), Comment: "Use https: the token below has full access and never expires."},
		{Key: daemon.EnvHassToken, Value: strings.TrimSpace(token)},
		{Key: daemon.EnvToggleEntity, Value: strings.TrimSpace(entity), Comment: "The switch that arms the display."},
	}, nil
}

// offerCalibrate runs the orientation check, which cannot be verified without
// the user looking at the wall.
func offerCalibrate(ctx context.Context, cmd *cobra.Command, host, token string) error {
	run := true
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Line the shape up with your wall?").
			Description("Opens a page on this machine that draws your panels, lights the\n" +
				"bottom of the shape green and the top red, and lets you drag it\n" +
				"until it matches. The panels report where they are, but nothing\n" +
				"tells them which way is up in your room, so this is the only way\n" +
				"to be sure.").
			Value(&run),
	)).RunWithContext(ctx); err != nil {
		return err
	}
	if !run {
		return nil
	}

	// The page, not a static pattern: nobody can look at a wall and name
	// an angle, and a terminal can only draw the shape as coloured
	// blocks. The browser draws the panels themselves.
	return calibrateInBrowser(cmd, nanoleaf.New(host, token), 0, 0, true)
}

// offerHooks installs the hooks, which is what makes the display start on its
// own.
func offerHooks(ctx context.Context, o *out) error {
	install := true
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Let Claude Code start the display?").
			Description("Adds hooks to Claude Code's settings, keeping every hook already\n" +
				"there and writing a backup first. Without them, start the display\n" +
				"with `nanoclaude up`.").
			Value(&install),
	)).RunWithContext(ctx); err != nil {
		return err
	}
	if !install {
		return nil
	}

	path, err := hooks.SettingsPath()
	if err != nil {
		return err
	}
	bin, err := binaryPath("")
	if err != nil {
		return err
	}

	res, err := hooks.Install(path, bin)
	if err != nil {
		return err
	}
	if len(res.Changed) == 0 {
		o.print(ui.Good.Render("Hooks already installed.") + "\n")
		return o.Err()
	}
	o.printf("%s %s\n", ui.Good.Render("Installed hooks"), ui.Muted.Render("("+res.Backup+")"))
	return o.Err()
}

// notBlank rejects an empty answer, naming what was expected.
func notBlank(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("enter %s", what)
		}
		return nil
	}
}
