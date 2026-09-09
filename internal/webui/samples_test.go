package webui

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/shapes"
)

// TestEverySampleShapeTiles is the check that the drawing is right for other
// people's walls, not just the one this was written against.
//
// It holds every sample to the property a real wall has: panels of the same
// kind that sit next to each other are edge to edge, so their outlines share
// exactly two corners. That is what catches a corner angle that is out by a
// sixth of a turn on triangles, a twelfth on hexagons, or an eighth on
// squares -- each of which draws a heap of overlapping shapes rather than a
// wall, and none of which the code can tell you about on its own.
func TestEverySampleShapeTiles(t *testing.T) {
	for _, sample := range shapes.All() {
		t.Run(sample.Name, func(t *testing.T) {
			wall := render.Project(sample.Layout, 0)
			d := newDrawing(wall)
			usable, _ := wall.Lights()

			for i, panel := range usable {
				// Panels are only expected to meet panels drawn
				// the same size and with the same number of
				// corners. A full triangle really does sit
				// against a mini one in a mixed set, but their
				// corners cannot coincide: one edge of the big
				// one is two edges of the small one.
				nearest, gap := -1, math.Inf(1)
				for j, other := range usable {
					if i == j || !sameKind(d, panel, other) {
						continue
					}
					if dist := math.Hypot(panel.X-other.X, panel.Y-other.Y); dist < gap {
						nearest, gap = j, dist
					}
				}
				if nearest < 0 {
					continue
				}

				shared := sharedCorners(d.outline(panel), d.outline(usable[nearest]))
				if shared != 2 {
					t.Errorf("panels %d and %d are %0.f apart, the closest of their kind, but share %d corners",
						panel.Panel.ID, usable[nearest].Panel.ID, gap, shared)
				}
			}
		})
	}
}

// sameKind reports whether two panels are drawn as the same polygon at the
// same size, which is the condition for their edges to be able to coincide.
func sameKind(d drawing, a, b render.WallPanel) bool {
	shapeA, shapeB := shapeOf(a.Panel.ShapeType), shapeOf(b.Panel.ShapeType)
	return shapeA.corners == shapeB.corners &&
		d.sideOf(a.Panel.ShapeType) == d.sideOf(b.Panel.ShapeType)
}

// TestEverySampleCanBeDrawn keeps a sample from reaching a page it would
// break: a picture needs at least one panel it can light, and a frame to put
// it in.
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
