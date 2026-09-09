package main

import (
	"context"
	"errors"
	"fmt"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
)

// spin runs fn while showing a spinner, and returns whatever fn returns.
//
// Without a terminal it prints one line and runs fn directly. A live view
// written to a pipe is a stream of escape codes and redraws, which is worse
// than no view at all.
func spin[T any](ctx context.Context, cmd *cobra.Command, title string, fn func(context.Context) (T, error)) (T, error) {
	if !ui.Interactive(cmd.OutOrStdout()) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), title)
		return fn(ctx)
	}

	m := &spinModel[T]{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		title:   title,
		result:  make(chan spinResult[T], 1),
	}

	go func() {
		v, err := fn(ctx)
		m.result <- spinResult[T]{value: v, err: err}
	}()

	final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithOutput(cmd.OutOrStdout())).Run()
	if err != nil {
		var zero T
		return zero, err
	}

	done, ok := final.(*spinModel[T])
	if !ok {
		var zero T
		return zero, errors.New("spin: unexpected final model")
	}
	if !done.finished {
		var zero T
		return zero, context.Canceled
	}
	return done.value, done.err
}

type spinResult[T any] struct {
	value T
	err   error
}

type spinModel[T any] struct {
	spinner spinner.Model
	title   string
	result  chan spinResult[T]

	value    T
	err      error
	finished bool
}

func (m *spinModel[T]) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, func() tea.Msg { return <-m.result })
}

func (m *spinModel[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinResult[T]:
		m.value, m.err, m.finished = msg.value, msg.err, true
		return m, tea.Quit

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case spinner.TickMsg:
		s, cmd := m.spinner.Update(msg)
		m.spinner = s
		return m, cmd
	}
	return m, nil
}

func (m *spinModel[T]) View() tea.View {
	if m.finished {
		// The caller reports the outcome, so a leftover spinner line
		// would only be noise.
		return tea.NewView("")
	}
	return tea.NewView(lipgloss.JoinHorizontal(lipgloss.Left,
		m.spinner.View(), " ", ui.Muted.Render(m.title)))
}
