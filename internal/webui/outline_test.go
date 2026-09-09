package webui

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// realLayout is a real NL42 as it reports itself: nine triangles in a
// diagonal zigzag plus the controller brick, mounted at globalOrientation
// 302. Orientations included, which is what the drawing needs and what the
// renderer never looks at.
func realLayout() nanoleaf.Layout {
	return nanoleaf.Layout{
		NumPanels:         10,
		SideLength:        134,
		GlobalOrientation: 302,
		Panels: []nanoleaf.Panel{
			{ID: 53940, X: 34, Y: 89, Orientation: 0, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 38513, X: 101, Y: 127, Orientation: 180, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 22926, X: 34, Y: 205, Orientation: 120, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 38260, X: 101, Y: 243, Orientation: 300, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 51757, X: 168, Y: 205, Orientation: 240, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 65173, X: 235, Y: 243, Orientation: 300, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 51695, X: 167, Y: 89, Orientation: 120, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 58908, X: 234, Y: 127, Orientation: 180, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 17522, X: 301, Y: 89, Orientation: 240, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 0, X: 0, Y: 40, Orientation: 180, ShapeType: nanoleaf.ShapeController},
		},
	}
}

// sharedCorners counts corners two outlines have in common.
//
// The device reports centroids as whole units, so two corners that are the
// same point on the wall can differ by a fraction of a unit here. The
// tolerance is a unit and a half, against a side length of 134.
func sharedCorners(a, b []point) int {
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

// TestTrianglesShareTheirEdges is what pins the corner angles.
//
// Panels that touch on the wall must touch in the drawing: two triangles
// whose centroids are one circumradius apart are neighbours in the tiling, so
// their outlines have to meet along a whole edge, which is two corners. Get
// the base angle wrong by 60 degrees -- point every triangle the other way --
// and neighbours share nothing at all, which on screen looks like a pile of
// overlapping shapes rather than a wall.
func TestTrianglesShareTheirEdges(t *testing.T) {
	layout := realLayout()
	wall := render.Project(layout, 0)
	d := newDrawing(wall)

	usable, _ := wall.Lights()
	outlines := make([][]point, len(usable))
	for i, wp := range usable {
		outlines[i] = d.outline(wp)
	}

	// Neighbouring centroids in a triangular tiling sit one circumradius
	// apart, which is the side length over the square root of three.
	neighbour := float64(layout.SideLength) / math.Sqrt(3)
	pairs := 0
	for i := range usable {
		for j := i + 1; j < len(usable); j++ {
			gap := math.Hypot(usable[i].X-usable[j].X, usable[i].Y-usable[j].Y)
			if math.Abs(gap-neighbour) > 2 {
				continue
			}
			pairs++
			if shared := sharedCorners(outlines[i], outlines[j]); shared != 2 {
				t.Errorf("panels %d and %d are neighbours but share %d corners, want 2",
					usable[i].Panel.ID, usable[j].Panel.ID, shared)
			}
		}
	}
	// Seven: four along the upper row of the zigzag and three along the
	// lower one. The two rows meet at a vertex, not an edge, so they
	// contribute none.
	if pairs != 7 {
		t.Fatalf("found %d neighbouring pairs in the zigzag, want 7", pairs)
	}
}

// TestOrientationDecidesWhichWayATrianglePoints checks the other half of the
// convention: the device reports o=0 for a triangle pointing up and o=180 for
// one pointing down.
func TestOrientationDecidesWhichWayATrianglePoints(t *testing.T) {
	layout := nanoleaf.Layout{
		SideLength: 134,
		Panels: []nanoleaf.Panel{
			{ID: 1, X: 67, Y: 39, Orientation: 0, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 2, X: 134, Y: 77, Orientation: 180, ShapeType: nanoleaf.ShapeTriangle},
		},
	}
	wall := render.Project(layout, 0)
	d := newDrawing(wall)

	for _, tc := range []struct {
		name  string
		panel int
		up    bool
	}{
		{"o=0 points up", 0, true},
		{"o=180 points down", 1, false},
	} {
		wp := wall.Panels[tc.panel]
		above := 0
		for _, c := range d.outline(wp) {
			if c.Y > wp.Y {
				above++
			}
		}
		// One corner above the centroid and two below means the
		// triangle points up; the other way round means it points
		// down.
		wantAbove := 2
		if tc.up {
			wantAbove = 1
		}
		if above != wantAbove {
			t.Errorf("%s: %d of 3 corners are above the centroid, want %d", tc.name, above, wantAbove)
		}
	}
}

// TestHexagonsShareTheirEdges checks that the corner angle is inferred from
// the layout rather than assumed.
//
// The fixture is a flat-top honeycomb, which a fixed pointy-top convention
// would draw a twelfth of a turn out, so its neighbours would share no
// corners at all.
func TestHexagonsShareTheirEdges(t *testing.T) {
	const side = 100.0
	gap := side * math.Sqrt(3)
	layout := nanoleaf.Layout{
		SideLength: int(side),
		Panels: []nanoleaf.Panel{
			{ID: 1, X: 0, Y: 0, ShapeType: nanoleaf.ShapeHexagon},
			{ID: 2, X: int(gap * math.Cos(math.Pi/6)), Y: int(gap * math.Sin(math.Pi/6)), ShapeType: nanoleaf.ShapeHexagon},
		},
	}

	wall := render.Project(layout, 0)
	d := newDrawing(wall)
	first := d.outline(wall.Panels[0])
	second := d.outline(wall.Panels[1])

	if shared := sharedCorners(first, second); shared != 2 {
		t.Errorf("neighbouring hexagons share %d corners, want 2", shared)
	}
	if len(first) != 6 {
		t.Errorf("drew a hexagon with %d corners", len(first))
	}
}

// TestLoneHexagonKeepsTheFallbackAngle covers the layout with nothing to
// infer from.
func TestLoneHexagonKeepsTheFallbackAngle(t *testing.T) {
	layout := nanoleaf.Layout{
		SideLength: 100,
		Panels:     []nanoleaf.Panel{{ID: 1, ShapeType: nanoleaf.ShapeHexagon}},
	}
	if got := newDrawing(render.Project(layout, 0)).hexBase; got != shapeOf(nanoleaf.ShapeHexagon).base {
		t.Errorf("hexBase = %v, want the fallback %v", got, shapeOf(nanoleaf.ShapeHexagon).base)
	}
}

// TestMiniTrianglesAreHalvedOnlyInAMixedSet pins the one guess in the
// drawing: a device reports a single side length, and a set holding both
// sizes of triangle has two.
func TestMiniTrianglesAreHalvedOnlyInAMixedSet(t *testing.T) {
	mini := nanoleaf.Panel{ID: 2, X: 200, Y: 0, ShapeType: nanoleaf.ShapeMiniTriangle}

	mixed := newDrawing(render.Project(nanoleaf.Layout{
		SideLength: 134,
		Panels:     []nanoleaf.Panel{{ID: 1, ShapeType: nanoleaf.ShapeTriangle}, mini},
	}, 0))
	if got := mixed.sideOf(nanoleaf.ShapeMiniTriangle); got != 67 {
		t.Errorf("mixed set draws a mini triangle with side %v, want 67", got)
	}
	if got := mixed.sideOf(nanoleaf.ShapeTriangle); got != 134 {
		t.Errorf("mixed set draws a full triangle with side %v, want 134", got)
	}

	only := newDrawing(render.Project(nanoleaf.Layout{
		SideLength: 67,
		Panels:     []nanoleaf.Panel{mini},
	}, 0))
	if got := only.sideOf(nanoleaf.ShapeMiniTriangle); got != 67 {
		t.Errorf("mini-only set draws a mini triangle with side %v, want the reported 67", got)
	}
}

// TestUnknownShapeIsDrawnAsACircle keeps an unrecognised panel visible. A
// model this version does not know still hangs on the wall.
func TestUnknownShapeIsDrawnAsACircle(t *testing.T) {
	layout := nanoleaf.Layout{
		SideLength: 100,
		Panels:     []nanoleaf.Panel{{ID: 1, ShapeType: 200}},
	}
	wall := render.Project(layout, 0)
	pts := newDrawing(wall).outline(wall.Panels[0])
	if len(pts) != unknownCorners {
		t.Fatalf("drew %d corners, want %d", len(pts), unknownCorners)
	}
	for _, p := range pts {
		if r := math.Hypot(p.X, p.Y); math.Abs(r-50) > 0.001 {
			t.Errorf("corner at radius %.3f, want 50", r)
		}
	}
}
