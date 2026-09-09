package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/internal/daemon"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Calibration colours, picked to be unmistakable from across a room and
// nameable without hedging: nobody has to judge a shade.
var (
	calibrateBottom = nanoleaf.RGB{R: 0, G: 255, B: 0}
	calibrateTop    = nanoleaf.RGB{R: 255, G: 0, B: 0}
	calibrateMiddle = nanoleaf.RGB{R: 0, G: 0, B: 90}
)

func newCalibrateCmd() *cobra.Command {
	var (
		rotation int
		hold     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "calibrate",
		Short: "Light the panels to confirm which way the shape is mounted",
		Long: "Paints the bottom of the shape green and the top red, according to the\n" +
			"orientation the daemon believes the panels are in.\n\n" +
			"Panel coordinates come from the Nanoleaf app's arrangement, which does\n" +
			"not know which way is up on the wall; the device's global orientation\n" +
			"is what reconciles the two, and it is undone rather than applied.\n\n" +
			"Run this and look at the wall. If green is not at the bottom, the\n" +
			"device's orientation disagrees with how the panels actually hang: set\n" +
			"NANOCLAUDE_ROTATION to the difference in degrees.\n\n" +
			"The panels are restored on exit.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			client, err := daemon.LeafFromEnv()
			if err != nil {
				return err
			}

			layout, err := client.Layout(ctx)
			if err != nil {
				return err
			}
			geo, _ := render.FromLayout(layout, rotation)
			if len(geo.Points) == 0 {
				return fmt.Errorf("calibrate: no renderable panels")
			}

			// Saved before the stream is opened, so the panels can be
			// put back the way they were found.
			saved, err := client.State(ctx)
			if err != nil {
				return err
			}

			frame := make(nanoleaf.Frame, len(geo.Points))
			var bottom, middle, top int
			for _, p := range geo.Points {
				switch {
				case p.V < 0.34:
					frame[p.PanelID] = calibrateBottom
					bottom++
				case p.V > 0.66:
					frame[p.PanelID] = calibrateTop
					top++
				default:
					frame[p.PanelID] = calibrateMiddle
					middle++
				}
			}

			o := newOut(cmd.OutOrStdout())
			o.printf("global orientation %d deg + extra rotation %d deg\n",
				layout.GlobalOrientation, rotation)
			o.printf("painting %d panels GREEN (bottom), %d BLUE (middle), %d RED (top)\n",
				bottom, middle, top)
			o.printf("holding for %s -- look at the wall\n", hold)
			if err := o.Err(); err != nil {
				return err
			}

			// Streaming to powered-off panels shows nothing, so the
			// pattern would be invisible and look like a bug. restore
			// puts the power back as it was.
			if !saved.On {
				if err := client.SetOn(ctx, true); err != nil {
					return err
				}
			}

			stream, err := client.OpenStream(ctx, 200*time.Millisecond)
			if err != nil {
				return err
			}
			defer restore(client, stream, saved)

			// Resent periodically: a single frame is enough for the
			// panels, but repeating keeps the picture up if a packet
			// is lost, and lets the hold be interrupted promptly.
			tick := time.NewTicker(time.Second)
			defer tick.Stop()

			deadline := time.After(hold)
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
		},
	}

	cmd.Flags().IntVar(&rotation, "rotation", 0,
		"extra rotation in degrees, added to the layout's global orientation")
	cmd.Flags().DurationVar(&hold, "hold", 30*time.Second, "how long to hold the pattern")
	return cmd
}

// restore hands the panels back, on a context of its own because the
// command's may already be cancelled by the interrupt that ended the hold.
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
