package main

import (
	"context"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Calibration colours, picked to be unmistakable from across a room and
// nameable without hedging: nobody has to judge a shade.
var (
	calibrateBottom = nanoleaf.RGB{R: 0, G: 255, B: 0}
	calibrateTop    = nanoleaf.RGB{R: 255, G: 0, B: 0}
	calibrateMiddle = nanoleaf.RGB{R: 0, G: 0, B: 90}
)

const (
	// calibrateFrame is how often the pattern is resent. Slow, because the
	// picture only changes when a key is pressed.
	calibrateFrame = 200 * time.Millisecond

	// coarseStep and fineStep are how far the arrow keys turn the shape.
	// Coarse is a sixth of a turn, which is the symmetry of a triangular
	// or hexagonal tiling, so it lands on the orientations a wall is
	// likely to use.
	coarseStep = 15
	fineStep   = 1
)

func newCalibrateCmd() *cobra.Command {
	var (
		rotation int
		hold     time.Duration
		static   bool
	)

	cmd := &cobra.Command{
		Use:   "calibrate",
		Short: "Turn the shape until it matches your wall",
		Long: "Lights the bottom of the shape green and the top red, and lets you turn\n" +
			"it with the arrow keys until it matches the wall. Enter saves the\n" +
			"rotation to the configuration file.\n\n" +
			"This exists because nobody can look at a wall and name an angle. The\n" +
			"panels report where they are, and the device reports how the whole\n" +
			"arrangement is rotated, but nothing tells it which way is up in your\n" +
			"room. The earlier advice was to work out the difference in degrees and\n" +
			"set it by hand, which is not something a person can do.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := daemon.LeafFromEnv()
			if err != nil {
				return err
			}
			if static || !ui.Interactive(cmd.OutOrStdout()) {
				return holdPattern(cmd.Context(), cmd.OutOrStdout(), client, rotation, hold)
			}
			return turnShape(cmd, client, rotation)
		},
	}

	cmd.Flags().IntVar(&rotation, "rotation", 0, "rotation to start from, in degrees")
	cmd.Flags().DurationVar(&hold, "hold", 30*time.Second, "how long to hold the pattern in static mode")
	cmd.Flags().BoolVar(&static, "static", false, "just hold the pattern, without the arrow keys")
	return cmd
}

// pattern is the calibration picture for a geometry: green at the bottom, red
// at the top, and a dim blue between them.
func pattern(geo render.Geometry) nanoleaf.Frame {
	frame := make(nanoleaf.Frame, len(geo.Points))
	for _, p := range geo.Points {
		switch {
		case p.V < 0.34:
			frame[p.PanelID] = calibrateBottom
		case p.V > 0.66:
			frame[p.PanelID] = calibrateTop
		default:
			frame[p.PanelID] = calibrateMiddle
		}
	}
	return frame
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
	// Streaming to powered-off panels shows nothing, so the pattern would
	// be invisible and look like a fault.
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

	frame := pattern(geo)
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

// restore hands the panels back, on a context of its own because the
// command's may already be cancelled by the key that ended the session.
func restore(client *nanoleaf.Client, stream *nanoleaf.Streamer, saved nanoleaf.State) {
	_ = stream.Close()

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

// turnShape runs the interactive dial.
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
	if !ok || !done.accepted {
		o := newOut(cmd.OutOrStdout())
		o.print(ui.Muted.Render("Cancelled. Nothing was saved.") + "\n")
		return o.Err()
	}
	return saveRotation(cmd, done.rotation)
}

// saveRotation writes the accepted value to the configuration file.
func saveRotation(cmd *cobra.Command, rotation int) error {
	path, err := daemon.ConfigFilePath()
	if err != nil {
		return err
	}
	if err := daemon.SetConfigValue(path, daemon.EnvRotation, fmt.Sprint(rotation)); err != nil {
		return err
	}

	o := newOut(cmd.OutOrStdout())
	o.printf("%s %s=%s\n", ui.Good.Render("Saved"),
		daemon.EnvRotation, ui.Value.Render(fmt.Sprint(rotation)))
	o.printf("  %s\n", ui.Muted.Render(path))
	o.print("\n" + ui.Muted.Render("Restart the display to pick it up: nanoclaude down && nanoclaude up") + "\n")
	return o.Err()
}

// turnModel is the dial.
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
		if err := m.stream.Send(pattern(geo)); err != nil {
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
		switch msg.String() {
		case "left":
			m.rotation = wrap(m.rotation - coarseStep)
		case "right":
			m.rotation = wrap(m.rotation + coarseStep)
		case "shift+left", ",":
			m.rotation = wrap(m.rotation - fineStep)
		case "shift+right", ".":
			m.rotation = wrap(m.rotation + fineStep)
		case "enter":
			m.accepted = true
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// wrap keeps the rotation in [0, 360).
func wrap(deg int) int {
	deg %= 360
	if deg < 0 {
		deg += 360
	}
	return deg
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
		drawShape(geo, pattern(geo)),
		help,
	))
}
