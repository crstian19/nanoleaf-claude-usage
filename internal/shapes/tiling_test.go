package shapes

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
)

// sharedCorners counts corners two outlines have in common.
//
// A device reports centroids as whole units, and so does a built wall, so two
// corners that are the same point can differ by a fraction of a unit here.
// The tolerance is a unit and a half, against side lengths of 67 and up.
func sharedCorners(a, b []render.Corner) int {
	const tolerance = 1.5
	shared := 0
	for _, p := range a {
		for _, q := range b {
			if math.Hypot(p.X-q.X, p.Y-q.Y) < tolerance {
				shared++
				break
			}
		}
	}
	return shared
}

// TestEverySampleTiles is the check that the drawing is right for other
// people's walls, not just the one this was written against.
//
// The property is the one a real wall has: two panels of the same kind that
// are as close as neighbours can be are edge to edge, so their outlines share
// exactly two corners. Panels further apart are not neighbours and are not
// asserted about, which is what lets a sample hold three triangles around a
// hexagon without them being expected to touch each other.
//
// It catches a corner angle out by a sixth of a turn on triangles, a twelfth
// on hexagons, or an eighth on squares. Each of those draws a heap of
// overlapping shapes rather than a wall, and none of them is something the
// code can tell you about on its own. The zigzag is the sample that makes
// this more than self-consistency: its coordinates are a measurement from a
// real device rather than something this package computed.
func TestEverySampleTiles(t *testing.T) {
	for _, sample := range All() {
		if !sample.Tiles() {
			// Lines, lightstrips and the corner-lit Elements
			// hexagon are not walls of panels meeting edge to
			// edge. What holds for them is checked below.
			continue
		}
		t.Run(sample.Name, func(t *testing.T) {
			wall := render.Project(sample.Layout, 0)
			outliner := render.NewOutliner(wall)
			usable, _ := wall.Lights()

			for i, panel := range usable {
				neighbours := 0
				for j, other := range usable {
					if i == j {
						continue
					}
					gap := math.Hypot(panel.X-other.X, panel.Y-other.Y)
					reach := outliner.Polygon(panel.Panel.ShapeType).Apothem() +
						outliner.Polygon(other.Panel.ShapeType).Apothem()
					// A twentieth of slack, for centroids
					// rounded to whole units.
					if gap > reach*1.05 {
						continue
					}
					neighbours++

					if !sameKind(outliner, panel, other) {
						// Two sizes meet along an edge
						// without their corners lining
						// up: one edge of a full
						// triangle is two edges of a
						// mini. AttachHalf is tested
						// where it is implemented.
						continue
					}
					shared := sharedCorners(outliner.Outline(panel), outliner.Outline(other))
					if shared != 2 {
						t.Errorf("panels %d and %d are %.0f apart, as close as neighbours get, but share %d corners",
							panel.Panel.ID, other.Panel.ID, gap, shared)
					}
				}

				if neighbours == 0 {
					t.Errorf("panel %d touches nothing: a wall is one piece",
						panel.Panel.ID)
				}
			}
		})
	}
}

// sameKind reports whether two panels are drawn as the same polygon at the
// same size, which is the condition for their edges to be able to coincide.
func sameKind(o render.Outliner, a, b render.WallPanel) bool {
	return o.Polygon(a.Panel.ShapeType).Sides == o.Polygon(b.Panel.ShapeType).Sides &&
		o.Side(a.Panel.ShapeType) == o.Side(b.Panel.ShapeType)
}

// TestTheFiguresThatDoNotTileAreStillWhole covers the samples the rule above
// cannot judge: a Lines zigzag, a lightstrip round a screen, and the Elements
// hexagon that lights six corners.
//
// What they have to be is drawable and not on top of each other, which is all
// "a wall" means for something joined at connectors.
func TestTheFiguresThatDoNotTileAreStillWhole(t *testing.T) {
	figures := 0
	for _, sample := range All() {
		if sample.Tiles() {
			continue
		}
		figures++

		t.Run(sample.Name, func(t *testing.T) {
			wall := render.Project(sample.Layout, 0)
			outliner := render.NewOutliner(wall)
			usable, _ := wall.Lights()
			if len(usable) < 3 {
				t.Fatalf("the figure has %d panels that light", len(usable))
			}

			for i, panel := range usable {
				corners := outliner.Outline(panel)
				if len(corners) < 3 {
					t.Errorf("panel %d is drawn with %d corners", panel.Panel.ID, len(corners))
				}

				// Every panel is near something: a figure is
				// still one object.
				near := false
				for j, other := range usable {
					if i == j {
						continue
					}
					gap := math.Hypot(panel.X-other.X, panel.Y-other.Y)
					reach := outliner.Polygon(panel.Panel.ShapeType).Radius +
						outliner.Polygon(other.Panel.ShapeType).Radius
					if gap < 1 {
						t.Errorf("panels %d and %d are in the same place",
							panel.Panel.ID, other.Panel.ID)
					}
					if gap <= reach*1.2 {
						near = true
					}
				}
				if !near {
					t.Errorf("panel %d is off on its own", panel.Panel.ID)
				}
			}
		})
	}

	if figures < 3 {
		t.Errorf("only %d samples do not tile; the Lines, lightstrip and Elements corner figures should", figures)
	}
}
