package main

import (
	"context"
	"fmt"
	"io"
	"math"
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

// Steps the keys move the level by.
const (
	coarseBudget = 0.05
	fineBudget   = 0.01

	// keySweep is how long the s key takes to fill the wall, which is
	// about as long as a clip of it filling wants to be.
	keySweep = 20 * time.Second
)

// noSweep means the level was not asked to move.
const noSweep = -1

func newDebugCmd() *cobra.Command {
	var (
		budget    float64
		from      float64
		phaseName string
		hold      time.Duration
		rotation  int
	)

	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Put the panels in any state, for filming or for looking at",
		Long: "Paints the real panels at a level and an activity you choose, instead of\n" +
			"at the one you have actually spent.\n\n" +
			"It is how the display gets filmed: a wall at 90 percent with an error on\n" +
			"it is a few seconds of work here and an afternoon of work otherwise.\n\n" +
			"On a terminal it takes the arrow keys, so the wall can be driven while\n" +
			"the camera runs. With --for it holds one state and exits, which is what\n" +
			"a script or a pipe gets.\n\n" +
			"Stop the display first with `nanoclaude down`, or the two fight over the\n" +
			"panels. Note that a Claude Code hook in another window starts it again.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			phase, err := render.ParsePhase(phaseName)
			if err != nil {
				return fmt.Errorf("debug: %w", err)
			}
			if budget < 0 || budget > 1.5 {
				return fmt.Errorf("debug: --budget is a fraction from 0 to 1, got %v", budget)
			}
			if from != noSweep && hold <= 0 {
				return fmt.Errorf("debug: --from sweeps the level over time, so it needs --for")
			}

			client, err := daemon.LeafFromEnv()
			if err != nil {
				return err
			}
			return runDebug(cmd, client, debugState{
				budget:   budget,
				from:     from,
				phase:    phase,
				hold:     hold,
				rotation: rotation,
			})
		},
	}

	cmd.Flags().Float64VarP(&budget, "budget", "b", 0.6, "fraction of the allowance spent, 0 to 1")
	cmd.Flags().Float64Var(&from, "from", noSweep, "sweep the level from here up to --budget, over --for")
	cmd.Flags().StringVarP(&phaseName, "phase", "p", "idle", "activity: idle, thinking, tool or error")
	cmd.Flags().DurationVar(&hold, "for", 0, "hold it this long and exit, instead of taking the keys")
	cmd.Flags().IntVar(&rotation, "rotation", 0,
		"extra rotation in degrees, added to the layout's global orientation")
	return cmd
}

// debugState is what the wall is being asked to show.
type debugState struct {
	budget   float64
	from     float64
	phase    render.Phase
	hold     time.Duration
	rotation int
}

// sweeping reports whether the level moves on its own.
func (d debugState) sweeping() bool { return d.from != noSweep }

// levelAt is the level to show after this long.
func (d debugState) levelAt(elapsed time.Duration) float64 {
	if !d.sweeping() || d.hold <= 0 {
		return d.budget
	}
	through := float64(elapsed) / float64(d.hold)
	return d.from + (d.budget-d.from)*min(max(through, 0), 1)
}

// runDebug takes the panels over and shows the state until it is done with.
func runDebug(cmd *cobra.Command, client *nanoleaf.Client, state debugState) error {
	ctx := cmd.Context()

	layout, err := client.Layout(ctx)
	if err != nil {
		return err
	}
	geo, _ := render.FromLayout(layout, state.rotation)
	if len(geo.Points) == 0 {
		return fmt.Errorf("debug: no renderable panels")
	}

	pid, err := daemon.RunningPID()
	if err != nil {
		return err
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

	stream, err := webui.Reopening(ctx, func(streamCtx context.Context) (webui.Stream, error) {
		return client.OpenStream(streamCtx, webui.FramePeriod)
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = stream.Close()
		handBack(client, saved)
	}()

	o := newOut(cmd.OutOrStdout())
	if pid != 0 {
		o.printf("%s the display is running and painting the same panels. Stop it with\n",
			ui.Warn.Render("Note:"))
		o.print("      `nanoclaude down` first.\n\n")
	}
	if err := o.Err(); err != nil {
		return err
	}

	scene := render.NewScene(geo)
	if state.hold > 0 || !ui.Interactive(cmd.OutOrStdout()) {
		return holdState(ctx, cmd.OutOrStdout(), stream, scene, state)
	}
	return driveState(cmd, stream, scene, geo, state)
}

// holdState paints one state, or sweeps it, and returns when the time is up.
func holdState(ctx context.Context, w io.Writer, stream webui.Stream,
	scene *render.Scene, state debugState,
) error {
	o := newOut(w)
	switch {
	case state.sweeping():
		o.printf("sweeping %.0f%% to %.0f%% over %s, %s\n",
			state.from*100, state.budget*100, state.hold, state.phase)
	case state.hold > 0:
		o.printf("holding %.0f%% %s for %s\n", state.budget*100, state.phase, state.hold)
	default:
		o.printf("holding %.0f%% %s until you stop it\n", state.budget*100, state.phase)
	}
	if err := o.Err(); err != nil {
		return err
	}

	ticker := time.NewTicker(webui.FramePeriod)
	defer ticker.Stop()

	start := time.Now()
	var deadline <-chan time.Time
	if state.hold > 0 {
		deadline = time.After(state.hold)
	}

	for {
		elapsed := time.Since(start)
		in := render.Input{Budget: state.levelAt(elapsed), Phase: state.phase}
		if err := stream.Send(scene.Frame(in, elapsed)); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			return nil
		case <-ticker.C:
		}
	}
}

// driveState hands the wall to the arrow keys.
func driveState(cmd *cobra.Command, stream webui.Stream,
	scene *render.Scene, geo render.Geometry, state debugState,
) error {
	m := &debugModel{
		geo:    geo,
		scene:  scene,
		stream: stream,
		start:  time.Now(),
		budget: state.budget,
		phase:  state.phase,
	}

	final, err := tea.NewProgram(m, tea.WithContext(cmd.Context()),
		tea.WithOutput(cmd.OutOrStdout())).Run()
	if err != nil {
		return err
	}
	if done, ok := final.(*debugModel); ok && done.err != nil {
		return done.err
	}
	return nil
}

// debugModel drives the wall from the keyboard.
type debugModel struct {
	geo    render.Geometry
	scene  *render.Scene
	stream webui.Stream
	start  time.Time

	budget float64
	phase  render.Phase

	// sweepFrom and sweepEnd run the level up on its own when the s key
	// asks for it, which is the shot of the wall filling.
	sweepFrom float64
	sweepEnd  time.Time

	err error
}

type debugTick struct{}

func (m *debugModel) Init() tea.Cmd { return m.send() }

// send paints one frame and asks for the next.
func (m *debugModel) send() tea.Cmd {
	return func() tea.Msg {
		in := render.Input{Budget: m.level(), Phase: m.phase}
		if err := m.stream.Send(m.scene.Frame(in, time.Since(m.start))); err != nil {
			return err
		}
		time.Sleep(webui.FramePeriod)
		return debugTick{}
	}
}

// level is what to show now, which is the sweep while one is running.
func (m *debugModel) level() float64 {
	if m.sweepEnd.IsZero() {
		return m.budget
	}
	left := time.Until(m.sweepEnd)
	if left <= 0 {
		return m.budget
	}
	through := 1 - float64(left)/float64(keySweep)
	return m.sweepFrom + (m.budget-m.sweepFrom)*through
}

func (m *debugModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case debugTick:
		if !m.sweepEnd.IsZero() && time.Now().After(m.sweepEnd) {
			m.sweepEnd = time.Time{}
		}
		return m, m.send()

	case error:
		m.err = msg
		return m, tea.Quit

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.setBudget(m.budget + coarseBudget)
		case "down":
			m.setBudget(m.budget - coarseBudget)
		case ".", "right":
			m.setBudget(m.budget + fineBudget)
		case ",", "left":
			m.setBudget(m.budget - fineBudget)
		case "1":
			m.phase = render.PhaseIdle
		case "2":
			m.phase = render.PhaseThinking
		case "3":
			m.phase = render.PhaseTool
		case "4":
			m.phase = render.PhaseError
		case "s":
			m.sweepFrom, m.sweepEnd = 0, time.Now().Add(keySweep)
			m.budget = 1
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// setBudget keeps the level on the scale.
func (m *debugModel) setBudget(level float64) {
	m.sweepEnd = time.Time{}
	m.budget = math.Round(min(max(level, 0), 1)*100) / 100
}

func (m *debugModel) View() tea.View {
	in := render.Input{Budget: m.level(), Phase: m.phase}

	status := fmt.Sprintf("used %s   activity %s",
		ui.Value.Render(fmt.Sprintf("%3.0f%%", m.level()*100)),
		ui.Value.Render(m.phase.String()))
	if !m.sweepEnd.IsZero() {
		status += ui.Muted.Render(fmt.Sprintf("   filling, %s left", time.Until(m.sweepEnd).Round(time.Second)))
	}

	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		ui.Title.Render("Driving the panels"),
		"",
		status,
		"",
		drawShape(m.geo, m.scene.Frame(in, time.Since(m.start))),
		ui.Muted.Render("↑/↓ 5%   ,/. 1%   1 idle  2 thinking  3 tool  4 error   s fill   q quit"),
	))
}
