package main

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
)

func newPreviewCmd() *cobra.Command {
	var (
		budget    float64
		phaseName string
		animate   time.Duration
		fps       int
		rotation  int
	)

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Render a scene in the terminal without touching the panels",
		Long: "Draws what the panels would show, for tuning the look without a\n" +
			"device and without disturbing whatever is on the wall.\n\n" +
			"Uses the real layout when NANOCLAUDE_NANOLEAF_HOST and _TOKEN are set,\n" +
			"and a stand-in nine-panel shape otherwise.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ph, err := parsePhase(phaseName)
			if err != nil {
				return err
			}
			// The daemon validates its own frame rate; this flag has
			// to as well, or --fps 0 divides by zero below.
			if fps < 1 || fps > 60 {
				return fmt.Errorf("preview: --fps must be between 1 and 60, got %d", fps)
			}

			geo, source, err := previewGeometry(cmd, rotation)
			if err != nil {
				return err
			}
			scene := render.NewScene(geo)
			in := render.Input{Budget: budget, Phase: ph}

			o := newOut(cmd.OutOrStdout())
			if animate <= 0 {
				o.printf("%s | budget %.0f%% | %s\n\n", source, budget*100, phaseName)
				o.print(drawShape(geo, scene.Frame(in, 0)))
				return o.Err()
			}

			interval := time.Second / time.Duration(fps)
			deadline := time.Now().Add(animate)
			start := time.Now()

			// Clear once, then home the cursor each frame rather than
			// clearing again, which would flicker.
			o.print("\x1b[2J")
			for time.Now().Before(deadline) {
				select {
				case <-cmd.Context().Done():
					return o.Err()
				default:
				}
				elapsed := time.Since(start)
				o.print("\x1b[H")
				o.printf("%s | budget %.0f%% | %s | %.1fs\n\n",
					source, budget*100, phaseName, elapsed.Seconds())
				o.print(drawShape(geo, scene.Frame(in, elapsed)))
				if err := o.Err(); err != nil {
					return err
				}
				time.Sleep(interval)
			}
			return o.Err()
		},
	}

	cmd.Flags().Float64Var(&budget, "budget", 0.6, "fraction of the window's ceiling used")
	cmd.Flags().StringVar(&phaseName, "phase", "idle", "activity: idle, thinking, tool or error")
	cmd.Flags().DurationVar(&animate, "animate", 0, "animate for this long instead of drawing one frame")
	cmd.Flags().IntVar(&fps, "fps", 20, "animation frame rate")
	cmd.Flags().IntVar(&rotation, "rotation", 0,
		"extra rotation in degrees, added to the layout's global orientation")
	return cmd
}

func parsePhase(name string) (render.Phase, error) {
	ph, err := render.ParsePhase(name)
	if err != nil {
		return 0, fmt.Errorf("preview: %w", err)
	}
	return ph, nil
}

// previewGeometry uses the real device layout when one is configured, and a
// stand-in shape otherwise so the command is useful before pairing.
//
// Only a completely unconfigured environment falls back to the demo shape. A
// half-configured one is an error, so a mistyped variable does not silently
// preview the wrong arrangement.
func previewGeometry(cmd *cobra.Command, rotation int) (render.Geometry, string, error) {
	client, err := daemon.LeafFromEnv()
	if errors.Is(err, daemon.ErrNoDevice) {
		return demoGeometry(), "demo shape", nil
	}
	if err != nil {
		return render.Geometry{}, "", err
	}

	layout, err := client.Layout(cmd.Context())
	if err != nil {
		return render.Geometry{}, "", err
	}
	geo, _ := render.FromLayout(layout, rotation)
	return geo, fmt.Sprintf("device layout (%d panels)", len(geo.Points)), nil
}

// demoGeometry is a nine-panel triangular arrangement in a diagonal band,
// standing in for a real layout. It exists to exercise the renderer: the
// long axis is genuinely diagonal, so a wrong axis or a flipped vertical
// shows up immediately.
func demoGeometry() render.Geometry {
	const side = 150.0
	height := side * math.Sqrt(3) / 2

	// Each entry is a cell in a triangular tiling: column index, row
	// index, and whether the triangle points up.
	cells := []struct {
		col, row int
		up       bool
	}{
		{0, 1, true},
		{1, 1, false},
		{2, 1, true},
		{1, 0, true},
		{2, 0, false},
		{3, 0, true},
		{3, 1, false},
		{4, 1, true},
		{4, 0, false},
	}

	ids := make([]int, len(cells))
	xs := make([]float64, len(cells))
	ys := make([]float64, len(cells))
	for i, c := range cells {
		ids[i] = 1000 + i
		xs[i] = (float64(c.col) + 1) * side / 2
		// Centroid sits a third of the way up an upward triangle and
		// two thirds up an inverted one.
		frac := 1.0 / 3
		if !c.up {
			frac = 2.0 / 3
		}
		ys[i] = float64(c.row)*height + frac*height
	}
	return render.NewGeometry(ids, xs, ys)
}
