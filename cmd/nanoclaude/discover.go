package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/discover"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
)

func newDiscoverCmd() *cobra.Command {
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Find Nanoleaf controllers on this network",
		Long: "Sweeps the machine's own IPv4 networks for the Nanoleaf API port and\n" +
			"confirms each answer, then prints the addresses it found.\n\n" +
			"This scans instead of using mDNS. A panel does advertise itself, but\n" +
			"reaching the advert needs a resolver running locally, and that is not a\n" +
			"safe assumption: on the machine this was written on, avahi-daemon was\n" +
			"stopped and the browse returned nothing while the panels answered fine.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			panels, err := findPanels(ctx, cmd)
			if err != nil {
				return err
			}

			o := newOut(cmd.OutOrStdout())
			if len(panels) == 0 {
				o.print(ui.Warn.Render("No panels found.") + "\n")
				o.print(ui.Muted.Render("Make sure that the panels are powered and on this network.") + "\n")
				return o.Err()
			}

			o.printf("%s\n", ui.Title.Render(fmt.Sprintf("Found %d:", len(panels))))
			for _, p := range panels {
				name := p.Name
				if name == "" {
					name = "no hostname"
				}
				o.printf("  %s  %s\n", ui.Value.Render(p.Host), ui.Muted.Render(name))
			}
			o.print("\n" + ui.Muted.Render("Next: nanoclaude pair --host "+panels[0].Host) + "\n")
			return o.Err()
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "give up after this long")
	return cmd
}

// findPanels runs the sweep, with a live progress bar on a terminal and a
// single line anywhere else.
func findPanels(ctx context.Context, cmd *cobra.Command) ([]discover.Panel, error) {
	if !ui.Interactive(cmd.OutOrStdout()) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Scanning for panels...")
		return discover.Find(ctx, nil)
	}

	m := &scanModel{
		bar:      progress.New(progress.WithDefaultBlend(), progress.WithWidth(40)),
		progress: make(chan scanProgress, 64),
		result:   make(chan scanResult, 1),
	}

	go func() {
		panels, err := discover.Find(ctx, func(done, total int) {
			// Dropped when the view is behind: a progress update is
			// worthless once a newer one exists.
			select {
			case m.progress <- scanProgress{done: done, total: total}:
			default:
			}
		})
		m.result <- scanResult{panels: panels, err: err}
	}()

	final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithOutput(cmd.OutOrStdout())).Run()
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}

	done, ok := final.(*scanModel)
	if !ok {
		return nil, errors.New("discover: unexpected final model")
	}
	return done.panels, done.err
}

type (
	scanProgress struct{ done, total int }
	scanResult   struct {
		panels []discover.Panel
		err    error
	}
)

// scanModel draws the sweep's progress.
type scanModel struct {
	bar      progress.Model
	progress chan scanProgress
	result   chan scanResult

	done, total int
	panels      []discover.Panel
	err         error
	finished    bool
}

func (m *scanModel) Init() tea.Cmd {
	return tea.Batch(waitForProgress(m.progress), waitForResult(m.result))
}

func waitForProgress(ch chan scanProgress) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func waitForResult(ch chan scanResult) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m *scanModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case scanProgress:
		m.done, m.total = msg.done, msg.total
		return m, waitForProgress(m.progress)

	case scanResult:
		m.panels, m.err = msg.panels, msg.err
		m.finished = true
		return m, tea.Quit

	case tea.KeyPressMsg:
		// Ctrl-C or q stops the sweep, like any other long command.
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case progress.FrameMsg:
		bar, cmd := m.bar.Update(msg)
		m.bar = bar
		return m, cmd
	}
	return m, nil
}

func (m *scanModel) View() tea.View {
	if m.finished {
		// Nothing left to show: the command prints the result itself,
		// so leaving a stale bar behind would only be noise.
		return tea.NewView("")
	}

	var pct float64
	if m.total > 0 {
		pct = float64(m.done) / float64(m.total)
	}

	label := ui.Muted.Render(fmt.Sprintf("scanning %d of %d addresses", m.done, m.total))
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		ui.Title.Render("Looking for Nanoleaf panels"),
		m.bar.ViewAs(pct),
		label,
	))
}
