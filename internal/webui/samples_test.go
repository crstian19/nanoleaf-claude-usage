package webui

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/internal/shapes"
)

// TestEverySampleCanBeDrawn keeps a sample from reaching a page it would
// break: a picture needs at least one panel it can light, and a frame to put
// it in.
//
// Whether the panels actually tile is checked where the samples are built.
func TestEverySampleCanBeDrawn(t *testing.T) {
	for _, sample := range shapes.All() {
		t.Run(sample.Name, func(t *testing.T) {
			pic, err := newPicture(sample.Layout)
			if err != nil {
				t.Fatal(err)
			}
			if pic.extent <= 0 {
				t.Fatalf("the drawing has an extent of %v", pic.extent)
			}

			snap := snapshotAt(t, pic, 0)
			if len(snap.Panels) != len(sample.Layout.Lights()) {
				t.Errorf("drew %d panels of %d that can be lit",
					len(snap.Panels), len(sample.Layout.Lights()))
			}
			for _, p := range snap.Panels {
				if p.Points == "" {
					t.Errorf("panel %d has no outline", p.ID)
				}
				for _, c := range corners(t, p.Points) {
					if math.Abs(c.X) > pic.extent || math.Abs(c.Y) > pic.extent {
						t.Errorf("a corner of panel %d falls outside the drawing: %+v", p.ID, c)
					}
				}
			}
		})
	}
}
