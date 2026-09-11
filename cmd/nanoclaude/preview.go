package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/shapes"
	"github.com/crstian19/nanoleaf-claude-usage/internal/ui"
	"github.com/crstian19/nanoleaf-claude-usage/internal/webui"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

func newPreviewCmd() *cobra.Command {
	var (
		budget    float64
		phaseName string
		animate   time.Duration
		fps       int
		rotation  int
		web       bool
		shapeName string
		port      int
		noOpen    bool
	)

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Render a scene without touching the panels",
		Long: "Draws what the panels would show, for tuning the look without a\n" +
			"device and without disturbing whatever is on the wall.\n\n" +
			"Uses the real layout when NANOCLAUDE_NANOLEAF_HOST and _TOKEN are set,\n" +
			"and a sample arrangement otherwise. --shape picks a sample even when a\n" +
			"device is configured.\n\n" +
			"--web opens a page that draws every sample arrangement, so the display\n" +
			"can be seen on a honeycomb of hexagons or a grid of squares without\n" +
			"owning either. Nothing is sent to any device in that mode, and nothing\n" +
			"can be saved.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if web {
				return previewInBrowser(cmd, shapeName, rotation, port, !noOpen)
			}

			ph, err := parsePhase(phaseName)
			if err != nil {
				return err
			}
			// The daemon validates its own frame rate; this flag has
			// to as well, or --fps 0 divides by zero below.
			if fps < 1 || fps > 60 {
				return fmt.Errorf("preview: --fps must be between 1 and 60, got %d", fps)
			}

			geo, source, err := previewGeometry(cmd, shapeName, rotation)
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
	cmd.Flags().BoolVar(&web, "web", false, "draw the shapes on a page in your browser")
	cmd.Flags().StringVar(&shapeName, "shape", "",
		"sample arrangement to draw instead of your own panels: "+strings.Join(shapes.Names(), ", "))
	cmd.Flags().IntVar(&port, "port", 0, "port for the page on 127.0.0.1 (0 picks a free one)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the address instead of opening a browser")
	return cmd
}

// previewInBrowser draws the sample arrangements on a page.
//
// It never touches a device, which is the difference between this and
// `calibrate`: the shapes on offer are not the user's, so there is nothing to
// paint and no rotation worth saving. What it is for is seeing the display on
// an arrangement you do not own -- every wall is different, and a gauge that
// only reads well on nine triangles in a zigzag is not finished.
func previewInBrowser(cmd *cobra.Command, shapeName string, rotation, port int, open bool) error {
	ctx := cmd.Context()

	offered, err := offeredShapes(shapeName)
	if err != nil {
		return err
	}

	server, err := webui.New(ctx, webui.Options{
		Shapes:   offered,
		OpenOn:   shapeName,
		Rotation: rotation,
		Build:    buildableKinds(),
		Port:     port,
	})
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()

	o := newOut(cmd.OutOrStdout())
	o.printf("%s\n\n", ui.Title.Render("Sample shapes in your browser"))
	o.printf("  %s\n\n", ui.Value.Render(server.URL()))
	for _, line := range []string{
		"Pick an arrangement, or build your own by dragging panels onto it.",
		"Nothing is sent to any device, so this is safe with the display running.",
		"The address opens once. Run the command again for a new one.",
	} {
		o.printf("%s\n", ui.Muted.Render(line))
	}
	if err := o.Err(); err != nil {
		return err
	}

	if open {
		if openErr := openBrowser(server.URL()); openErr != nil {
			o.printf("\n%s %s\n", ui.Muted.Render("Could not open a browser:"), openErr)
		}
	}

	if err := server.Run(ctx); err != nil {
		return err
	}
	o.printf("\n%s\n", ui.Muted.Render("Stopped: "+server.Result().Reason))
	return o.Err()
}

// buildableKinds are the panels a page may drop onto a wall it is building.
//
// Every panel in the range that is put together edge to edge, which leaves
// out the Lines bars and the lightstrip: those join end to end at connectors,
// at angles the connector decides.
//
// Mixing lines is not policed here and does not need to be. Each panel is
// built at its own published edge length and only clips to its own product
// line, so a Canvas square will not go on a Shapes triangle, and the page is
// told why by the same words this list is written in.
func buildableKinds() []webui.Kind {
	offered := []int{
		nanoleaf.ShapeTriangle,
		nanoleaf.ShapeHexagon,
		nanoleaf.ShapeMiniTriangle,
		nanoleaf.ShapeElementsHexagon,
		nanoleaf.ShapeSquare,
		nanoleaf.ShapeLightPanel,
		nanoleaf.ShapeSkylight,
	}

	kinds := make([]webui.Kind, 0, len(offered))
	for _, shapeType := range offered {
		kinds = append(kinds, webui.Kind{Shape: shapeType, Label: kindLabel(shapeType)})
	}
	return kinds
}

// kindLabel is a shape's name as a button says it.
//
// Taken from the protocol package rather than written out again, so a panel
// is called the same thing on a button, in a layout listing and in the
// sentence that refuses to put it somewhere. Four of these are triangles and
// the names are what say which.
func kindLabel(shapeType int) string {
	name := nanoleaf.ShapeName(shapeType)
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// offeredShapes is every sample, in the order this package lists them.
//
// A shape asked for by name is opened on rather than moved to the front: the
// list is the same for everybody, and the page starts wherever it was told.
func offeredShapes(wanted string) ([]webui.Shape, error) {
	if wanted != "" {
		if _, err := shapes.Named(wanted); err != nil {
			return nil, fmt.Errorf("preview: %w", err)
		}
	}

	all := shapes.All()
	offered := make([]webui.Shape, 0, len(all))
	for _, sample := range all {
		offered = append(offered, webui.Shape{Name: sample.Name, Label: sample.Label, Layout: sample.Layout})
	}
	return offered, nil
}

func parsePhase(name string) (render.Phase, error) {
	ph, err := render.ParsePhase(name)
	if err != nil {
		return 0, fmt.Errorf("preview: %w", err)
	}
	return ph, nil
}

// previewGeometry uses the real device layout when one is configured, and a
// sample arrangement otherwise so the command is useful before pairing.
//
// Only a completely unconfigured environment falls back to a sample. A
// half-configured one is an error, so a mistyped variable does not silently
// preview the wrong arrangement.
func previewGeometry(cmd *cobra.Command, shapeName string, rotation int) (render.Geometry, string, error) {
	// An explicitly named shape wins over the device: it is the only way
	// to look at somebody else's arrangement while owning one.
	if shapeName != "" {
		sample, err := shapes.Named(shapeName)
		if err != nil {
			return render.Geometry{}, "", fmt.Errorf("preview: %w", err)
		}
		geo, _ := render.FromLayout(sample.Layout, rotation)
		return geo, sample.Label, nil
	}

	client, err := daemon.LeafFromEnv()
	if errors.Is(err, daemon.ErrNoDevice) {
		sample := shapes.Default()
		geo, _ := render.FromLayout(sample.Layout, rotation)
		return geo, sample.Label, nil
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
