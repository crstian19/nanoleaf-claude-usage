// Package ui holds the styles and the terminal checks the commands share.
//
// Every styled view in this program has a plain counterpart. A CLI ends up in
// a pipe, a file, or a cron job, and a live Bubble Tea view written to a pipe
// is a mess of escape codes and redraw frames. Interactive returns whether
// the fancy path is safe to take.
package ui

import (
	"io"
	"os"

	"charm.land/lipgloss/v2"
)

// The palette. Kept small on purpose: a status tool that uses six colors
// teaches the reader nothing about which one matters.
var (
	// Title styles a heading.
	Title = lipgloss.NewStyle().Bold(true)
	// Muted styles text that supports the answer without being it.
	Muted = lipgloss.NewStyle().Faint(true)
	// Good styles a confirmed result.
	Good = lipgloss.NewStyle().Foreground(lipgloss.Color("#00d68f"))
	// Warn styles something the user has to act on.
	Warn = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffb020"))
	// Bad styles a failure.
	Bad = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5c5c"))
	// Value styles a piece of data, such as an address or a path.
	Value = lipgloss.NewStyle().Foreground(lipgloss.Color("#7cc5ff"))
)

// Interactive reports whether w is a terminal a live view can be drawn on.
//
// Checked by asking the file descriptor, not by reading an environment
// variable: TERM says what the terminal can do, never whether output still
// goes there.
func Interactive(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
