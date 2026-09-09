package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

func newLayoutCmd() *cobra.Command {
	var rotation int

	cmd := &cobra.Command{
		Use:   "layout",
		Short: "Show the panel arrangement and its scene coordinates",
		Long: "Prints each panel's wall position alongside the scene coordinates\n" +
			"rendering uses: U and V across the shape's bounding box, and S along\n" +
			"the long axis a pulse travels.\n\n" +
			"Use it to confirm the detected shape matches how the panels are\n" +
			"actually mounted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := daemon.LeafFromEnv()
			if err != nil {
				return err
			}

			layout, err := client.Layout(cmd.Context())
			if err != nil {
				return err
			}
			geo, skipped := render.FromLayout(layout, rotation)

			o := newOut(cmd.OutOrStdout())
			o.printf("%d panels, %d rendered, side length %d\n",
				layout.NumPanels, len(geo.Points), layout.SideLength)
			o.printf("global orientation %d deg, extra rotation %d deg\n",
				layout.GlobalOrientation, rotation)
			for _, p := range skipped {
				o.printf("skipped panel %d: id out of protocol range\n", p.ID)
			}
			o.print("\n")

			// The tabwriter buffers, so its Flush has to happen before
			// anything is written to o again -- otherwise the table
			// lands after the shape below it.
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			table := newOut(w)
			table.print("PANEL\tX\tY\tORIENT\tSHAPE\tU\tV\tS\n")
			for _, pt := range geo.Points {
				p := renderedPanel(layout, pt.PanelID)
				table.printf("%d\t%d\t%d\t%d\t%s\t%.2f\t%.2f\t%.2f\n",
					p.ID, p.X, p.Y, p.Orientation, shapeName(p.ShapeType),
					pt.U, pt.V, pt.S)
			}
			if err := table.Err(); err != nil {
				return fmt.Errorf("layout: write table: %w", err)
			}
			if err := w.Flush(); err != nil {
				return fmt.Errorf("layout: flush table: %w", err)
			}

			// A still frame at a plausible mid-window state makes a
			// wrong axis or a flipped vertical obvious at a glance.
			scene := render.NewScene(geo)
			o.print("\nShape as rendered (fill at 60%):\n\n")
			o.print(drawShape(geo, scene.Frame(render.Input{Budget: 0.6}, 0)))
			return o.Err()
		},
	}

	cmd.Flags().IntVar(&rotation, "rotation", 0,
		"extra rotation in degrees, added to the layout's global orientation")
	return cmd
}

// renderedPanel finds a panel's device record by id.
func renderedPanel(l nanoleaf.Layout, id int) nanoleaf.Panel {
	for _, p := range l.Panels {
		if p.ID == id {
			return p
		}
	}
	return nanoleaf.Panel{ID: id}
}

func shapeName(t int) string {
	switch t {
	case nanoleaf.ShapeTriangle:
		return "triangle"
	case nanoleaf.ShapeMiniTriangle:
		return "mini-triangle"
	case nanoleaf.ShapeController:
		return "controller"
	default:
		return fmt.Sprintf("type-%d", t)
	}
}
