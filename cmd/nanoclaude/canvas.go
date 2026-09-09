package main

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Canvas size in character cells. Terminal cells are about twice as tall as
// they are wide, so the width is doubled to keep the mounted shape from
// looking squashed.
const (
	canvasW = 62
	canvasH = 22
	blockW  = 5
	blockH  = 2
)

// drawShape renders a frame as coloured blocks laid out at the panels' real
// positions, so the terminal preview has the same silhouette as the wall.
func drawShape(geo render.Geometry, frame nanoleaf.Frame) string {
	type cell struct {
		set     bool
		r, g, b uint8
	}
	grid := make([][]cell, canvasH)
	for i := range grid {
		grid[i] = make([]cell, canvasW)
	}

	for _, p := range geo.Points {
		col := int(p.U * float64(canvasW-blockW-1))
		// V points up on the wall but rows count down the screen.
		row := int((1 - p.V) * float64(canvasH-blockH-1))
		c := frame[p.PanelID]

		for dy := range blockH {
			for dx := range blockW {
				y, x := row+dy, col+dx
				if y < 0 || y >= canvasH || x < 0 || x >= canvasW {
					continue
				}
				grid[y][x] = cell{set: true, r: c.R, g: c.G, b: c.B}
			}
		}
	}

	var b strings.Builder
	for _, row := range grid {
		for _, c := range row {
			if !c.set {
				b.WriteByte(' ')
				continue
			}
			hex := fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
			b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render(" "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
