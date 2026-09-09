package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
	"github.com/crstian19/nanoleaf-claude-usage/internal/webui"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

const (
	// calibrateFrame is how often the terminal dial resends the pattern.
	// Slow, because the picture only changes when a key is pressed.
	calibrateFrame = 200 * time.Millisecond

	// coarseStep and fineStep are how far the arrow keys turn the shape.
	// Fifteen degrees is a quarter of the sixth of a turn that a
	// triangular or hexagonal tiling repeats over, so every orientation a
	// wall is likely to use is a few presses away, and the ones the
	// tiling actually allows are all multiples of it.
	coarseStep = 15
	fineStep   = 1
)

func newCalibrateCmd() *cobra.Command {
	var (
		rotation int
		hold     time.Duration
		static   bool
		terminal bool
		port     int
		noOpen   bool
	)

	cmd := &cobra.Command{
		Use:   "calibrate",
		Short: "Turn the shape until it matches your wall",
		Long: "Lights the bottom of the shape green and the top red, and opens a page\n" +
			"where you drag the shape until it matches the wall. Saving writes the\n" +
			"rotation to the configuration file.\n\n" +
			"This exists because nobody can look at a wall and name an angle. The\n" +
			"panels report where they are, and the device reports how the whole\n" +
			"arrangement is rotated, but nothing tells it which way is up in your\n" +
			"room.\n\n" +
			"The page runs on this machine only. Use --tui for the same job with the\n" +
			"arrow keys in the terminal, which draws the shape as coloured blocks\n" +
			"rather than as your actual panels.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := daemon.LeafFromEnv()
			if err != nil {
				return err
			}
			switch {
			case static:
				return holdPattern(cmd.Context(), cmd.OutOrStdout(), client, rotation, hold)
			case terminal && ui.Interactive(cmd.OutOrStdout()):
				return turnShape(cmd, client, rotation)
			case terminal:
				return holdPattern(cmd.Context(), cmd.OutOrStdout(), client, rotation, hold)
			default:
				return calibrateInBrowser(cmd, client, rotation, port, !noOpen)
			}
		},
	}

	cmd.Flags().IntVar(&rotation, "rotation", 0, "rotation to start from, in degrees")
	cmd.Flags().DurationVar(&hold, "hold", 30*time.Second, "how long to hold the pattern in static mode")
	cmd.Flags().BoolVar(&static, "static", false, "just hold the pattern, without turning it")
	cmd.Flags().BoolVar(&terminal, "tui", false, "turn the shape in the terminal instead of a browser")
	cmd.Flags().IntVar(&port, "port", 0, "port for the page on 127.0.0.1 (0 picks a free one)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the address instead of opening a browser")
	return cmd
}

// calibrateInBrowser runs the session on a local page.
//
// The browser is the default because this is a visual job on a real wall. A
// terminal can only draw the shape as coloured blocks, so the thing being
// compared against the wall is already a translation; a page draws the actual
// triangles at their actual angles, and a mouse turns them.
func calibrateInBrowser(cmd *cobra.Command, client *nanoleaf.Client, rotation, port int, open bool) error {
	ctx := cmd.Context()

	layout, err := client.Layout(ctx)
	if err != nil {
		return err
	}

	configPath, save, err := rotationSaver()
	if err != nil {
		return err
	}

	// Another daemon streaming to the same panels would fight this session
	// frame for frame. Reported rather than refused: the display is
	// started by a hook, so it can come up in the middle of a calibration
	// through no fault of the user, and the fix is theirs to choose.
	pid, err := daemon.RunningPID()
	if err != nil {
		return err
	}

	saved, err := client.State(ctx)
	if err != nil {
		return err
	}

	// Everything that can fail happens before the session opens the
	// stream: the port, the layout and the configuration path are all
	// settled here, because enabling streaming mode is the only step that
	// changes the device and cannot be undone without knowing which
	// effect was selected before.
	server, err := webui.New(ctx, webui.Options{
		Shapes:   []webui.Shape{{Label: "your panels", Layout: layout}},
		Rotation: rotation,
		Open: func(streamCtx context.Context) (webui.Stream, error) {
			return client.OpenStream(streamCtx, webui.FramePeriod)
		},
		Save:           save,
		ConfigPath:     configPath,
		DisplayRunning: pid != 0,
		Port:           port,
	})
	if err != nil {
		return err
	}
	// Run gives the listener up on its way out; this covers the paths
	// that never reach it.
	defer func() { _ = server.Close() }()

	// Streaming to powered-off panels shows nothing, so the pattern would
	// be invisible and look like a fault. Reversible, unlike the stream,
	// which is why it can happen here.
	if !saved.On {
		if err := client.SetOn(ctx, true); err != nil {
			return err
		}
	}
	defer handBack(client, saved)

	o := newOut(cmd.OutOrStdout())
	printAddress(o, server.URL(), pid)
	if err := o.Err(); err != nil {
		return err
	}

	if open {
		if openErr := openBrowser(server.URL()); openErr != nil {
			o.printf("\n%s %s\n", ui.Muted.Render("Could not open a browser:"), openErr)
		}
	}

	// The result is reported whatever happened. A device that stops
	// answering ends the session, and a user who saved a rotation half a
	// minute earlier still has to be told that it was written.
	runErr := server.Run(ctx)
	if err := reportSession(cmd, server.Result()); err != nil {
		return err
	}
	return runErr
}

// printAddress says where the page is and what to expect of it.
func printAddress(o *out, url string, displayPID int) {
	o.printf("%s\n\n", ui.Title.Render("Calibrate the shape in your browser"))
	o.printf("  %s\n\n", ui.Value.Render(url))
	// One Render per line: a style applied to text with newlines in it
	// pads every line to the width of the longest.
	for _, line := range []string{
		"Drag the shape until green is at the bottom of your wall, then save.",
		"The page is on this machine only, and the address opens once: it is",
		"passed to your browser, where other programs can read it. Run the",
		"command again for a new one. Closing the tab stops the session, and",
		"so does Ctrl-C.",
	} {
		o.printf("%s\n", ui.Muted.Render(line))
	}
	if displayPID != 0 {
		o.printf("\n%s the display is running and painting the same panels, so the\n",
			ui.Warn.Render("Note:"))
		o.print("      picture will flicker between the two. Stop it with `nanoclaude down`.\n")
	}
}

// reportSession says what the session did, since the page it was said on is
// gone by the time this runs.
func reportSession(cmd *cobra.Command, res webui.Result) error {
	o := newOut(cmd.OutOrStdout())
	o.printf("\n%s\n", ui.Muted.Render("Stopped: "+res.Reason))
	if res.Saved == nil {
		o.printf("%s\n", ui.Muted.Render("Nothing was saved."))
		return o.Err()
	}
	printSaved(o, *res.Saved)
	return o.Err()
}

// holdPattern paints the pattern once and leaves it up, for a pipe or a
// script.
func holdPattern(ctx context.Context, w io.Writer, client *nanoleaf.Client, rotation int, hold time.Duration) error {
	layout, err := client.Layout(ctx)
	if err != nil {
		return err
	}
	geo, _ := render.FromLayout(layout, rotation)
	if len(geo.Points) == 0 {
		return fmt.Errorf("calibrate: no renderable panels")
	}

	saved, err := client.State(ctx)
	if err != nil {
		return err
	}
	if !saved.On {
		if err := client.SetOn(ctx, true); err != nil {
			return err
		}
	}

	stream, err := client.OpenStream(ctx, calibrateFrame)
	if err != nil {
		return err
	}
	defer restore(client, stream, saved)

	o := newOut(w)
	o.printf("global orientation %d deg + extra rotation %d deg\n", layout.GlobalOrientation, rotation)
	o.printf("holding for %s -- look at the wall, green must be at the bottom\n", hold)
	if err := o.Err(); err != nil {
		return err
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	deadline := time.After(hold)

	frame := render.Calibration(geo)
	for {
		if err := stream.Send(frame); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			return nil
		case <-tick.C:
		}
	}
}

// restore closes the stream and hands the panels back.
func restore(client *nanoleaf.Client, stream io.Closer, saved nanoleaf.State) {
	_ = stream.Close()
	handBack(client, saved)
}

// handBack puts the device back the way it was found, on a context of its own
// because the command's may already be cancelled by the key that ended the
// session.
//
// It must run after the stream is closed, never beside it: restoring the
// effect while something is still streaming would spend the saved effect and
// leave the panels in streaming mode, which cannot be recovered from.
func handBack(client *nanoleaf.Client, saved nanoleaf.State) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if saved.Restorable() {
		_ = client.SelectEffect(ctx, saved.Effect)
		if saved.On {
			return
		}
	}
	_ = client.SetOn(ctx, false)
}

func (m *turnModel) Init() tea.Cmd { return m.send() }

// turnShape runs the interactive dial in the terminal.
func turnShape(cmd *cobra.Command, client *nanoleaf.Client, rotation int) error {
	ctx := cmd.Context()

	layout, err := client.Layout(ctx)
	if err != nil {
		return err
	}
	if geo, _ := render.FromLayout(layout, rotation); len(geo.Points) == 0 {
		return fmt.Errorf("calibrate: no renderable panels")
	}

	saved, err := client.State(ctx)
	if err != nil {
		return err
	}
	if !saved.On {
		if err := client.SetOn(ctx, true); err != nil {
			return err
		}
	}

	stream, err := client.OpenStream(ctx, calibrateFrame)
	if err != nil {
		return err
	}
	defer restore(client, stream, saved)

	m := &turnModel{
		layout:   layout,
		stream:   stream,
		rotation: rotation,
	}
	final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithOutput(cmd.OutOrStdout())).Run()
	if err != nil {
		return err
	}

	done, ok := final.(*turnModel)
	if ok && done.err != nil {
		// Without this the dial exits 0 and tells the user they
		// cancelled, when in fact the panels stopped answering.
		return done.err
	}
	if !ok || !done.accepted {
		o := newOut(cmd.OutOrStdout())
		o.print(ui.Muted.Render("Cancelled. Nothing was saved.") + "\n")
		return o.Err()
	}
	return saveRotation(cmd, done.rotation)
}

// rotationSaver returns the configuration file's path and a function that
// writes a rotation to it.
//
// Shared by the page and the terminal dial: two ways of choosing an angle,
// one way of storing it.
func rotationSaver() (path string, save func(int) error, err error) {
	path, err = daemon.ConfigFilePath()
	if err != nil {
		return "", nil, err
	}
	return path, func(rotation int) error {
		return daemon.SetConfigValue(path, daemon.EnvRotation, strconv.Itoa(rotation))
	}, nil
}

// saveRotation writes the accepted value to the configuration file and says
// so.
func saveRotation(cmd *cobra.Command, rotation int) error {
	_, save, err := rotationSaver()
	if err != nil {
		return err
	}
	if err := save(rotation); err != nil {
		return err
	}

	o := newOut(cmd.OutOrStdout())
	printSaved(o, rotation)
	return o.Err()
}

// printSaved reports a written rotation and what to do about it.
func printSaved(o *out, rotation int) {
	path, err := daemon.ConfigFilePath()
	if err != nil {
		path = "the configuration file"
	}
	o.printf("%s %s=%s\n", ui.Good.Render("Saved"),
		daemon.EnvRotation, ui.Value.Render(strconv.Itoa(rotation)))
	o.printf("  %s\n", ui.Muted.Render(path))
	o.print("\n" + ui.Muted.Render("Restart the display to pick it up: nanoclaude down && nanoclaude up") + "\n")
}

// turnModel is the terminal dial.
type turnModel struct {
	layout   nanoleaf.Layout
	stream   *nanoleaf.Streamer
	rotation int

	accepted bool
	err      error
}

type turnTick struct{}

func (m *turnModel) send() tea.Cmd {
	return func() tea.Msg {
		geo, _ := render.FromLayout(m.layout, m.rotation)
		if err := m.stream.Send(render.Calibration(geo)); err != nil {
			return err
		}
		time.Sleep(calibrateFrame)
		return turnTick{}
	}
}

func (m *turnModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case turnTick:
		return m, m.send()

	case error:
		m.err = msg
		return m, tea.Quit

	case tea.KeyPressMsg:
		// An arrow turns the shape the way it points, as drawn. The
		// rotation is counter-clockwise, so left adds and right takes
		// away; the number is not what anybody is looking at.
		switch msg.String() {
		case "left":
			m.rotation = render.WrapDegrees(m.rotation + coarseStep)
		case "right":
			m.rotation = render.WrapDegrees(m.rotation - coarseStep)
		case "shift+left", ",":
			m.rotation = render.WrapDegrees(m.rotation + fineStep)
		case "shift+right", ".":
			m.rotation = render.WrapDegrees(m.rotation - fineStep)
		case "enter":
			m.accepted = true
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *turnModel) View() tea.View {
	geo, _ := render.FromLayout(m.layout, m.rotation)

	help := ui.Muted.Render(
		"←/→ turn 15°   ,/. turn 1°   enter save   esc cancel")
	status := fmt.Sprintf("rotation %s   %s",
		ui.Value.Render(fmt.Sprintf("%3d°", m.rotation)),
		ui.Muted.Render(fmt.Sprintf("(device reports %d°)", m.layout.GlobalOrientation)))

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		ui.Title.Render("Turn the shape until green is at the bottom of your wall"),
		"",
		status,
		"",
		drawShape(geo, render.Calibration(geo)),
		help,
	))
}
