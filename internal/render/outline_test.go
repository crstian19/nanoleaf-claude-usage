package render

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// TestPolygonOfKnowsTheProducts pins the outlines every drawing and every
// built wall depends on.
func TestPolygonOfKnowsTheProducts(t *testing.T) {
	const side = 134.0
	for _, tc := range []struct {
		name      string
		shapeType int
		sides     int
		base      float64
	}{
		{"triangle", nanoleaf.ShapeTriangle, 3, 90},
		{"mini triangle", nanoleaf.ShapeMiniTriangle, 3, 90},
		{"light panel", nanoleaf.ShapeLightPanel, 3, 90},
		{"square", nanoleaf.ShapeSquare, 4, 45},
		{"square with the controller", nanoleaf.ShapeSquareMaster, 4, 45},
		{"hexagon", nanoleaf.ShapeHexagon, 6, 30},
		{"something this version does not know", 200, unknownSides, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := PolygonOf(tc.shapeType, side)
			if p.Sides != tc.sides {
				t.Errorf("has %d sides, want %d", p.Sides, tc.sides)
			}
			if p.Base != tc.base {
				t.Errorf("first corner at %v degrees, want %v", p.Base, tc.base)
			}

			// A shape this program can name is drawn at the side
			// length it was asked for. An unknown one is drawn as a
			// circle, where the side length means nothing.
			if tc.sides != unknownSides {
				if got := p.Side(); math.Abs(got-side) > 1e-9 {
					t.Errorf("edge is %v long, want %v", got, side)
				}
			}

			// Every corner sits at the radius, and every edge
			// midpoint at the apothem: the outline and the edges
			// have to agree, because one is drawn and the other is
			// what panels are stuck to.
			for i, c := range p.Outline(0, 0, 0) {
				if r := math.Hypot(c.X, c.Y); math.Abs(r-p.Radius) > 1e-9 {
					t.Errorf("corner %d is %v from the centre, want %v", i, r, p.Radius)
				}
			}
			for edge := range p.Sides {
				m := p.EdgeMidpoint(0, 0, 0, edge)
				if a := math.Hypot(m.X, m.Y); math.Abs(a-p.Apothem()) > 1e-9 {
					t.Errorf("edge %d is %v from the centre, want %v", edge, a, p.Apothem())
				}
			}
		})
	}
}

// TestEdgeMidpointsSitBetweenCorners keeps the edge numbering honest: edge i
// runs from corner i to corner i+1. The shape builder attaches panels by edge
// number, so a numbering that disagreed with the drawing would stick them on
// in the wrong place.
func TestEdgeMidpointsSitBetweenCorners(t *testing.T) {
	for _, shapeType := range []int{nanoleaf.ShapeTriangle, nanoleaf.ShapeSquare, nanoleaf.ShapeHexagon} {
		p := PolygonOf(shapeType, 100)
		corners := p.Outline(0, 0, 17)
		for edge := range p.Sides {
			a := corners[edge]
			b := corners[(edge+1)%p.Sides]
			want := Corner{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
			got := p.EdgeMidpoint(0, 0, 17, edge)
			if math.Hypot(got.X-want.X, got.Y-want.Y) > 1e-9 {
				t.Errorf("shape %d edge %d: midpoint %+v, want %+v", shapeType, edge, got, want)
			}
		}
	}
}

// TestOrientationDecidesWhichWayATrianglePoints is the other half of the
// convention the device reports: o=0 points up and o=180 points down.
func TestOrientationDecidesWhichWayATrianglePoints(t *testing.T) {
	p := PolygonOf(nanoleaf.ShapeTriangle, 134)
	for _, tc := range []struct {
		orientation float64
		wantAbove   int
	}{
		{0, 1},
		{180, 2},
		// A third of a turn leaves an equilateral triangle where it
		// was, which is why a device only ever reports six values.
		{120, 1},
		{300, 2},
	} {
		above := 0
		for _, c := range p.Outline(0, 0, tc.orientation) {
			if c.Y > 0 {
				above++
			}
		}
		if above != tc.wantAbove {
			t.Errorf("at %v degrees, %d of 3 corners are above the centre, want %d",
				tc.orientation, above, tc.wantAbove)
		}
	}
}

// TestHexBaseIsInferredFromTheNeighbour covers the one outline this program
// cannot check against a real device.
//
// A honeycomb can be mounted with a corner at the top or an edge at the top,
// and the device does not say which. The direction of the nearest neighbour
// does: hexagons meet edge to edge, so it points at the middle of a shared
// edge and a corner is 30 degrees away.
func TestHexBaseIsInferredFromTheNeighbour(t *testing.T) {
	const side = 100.0
	gap := side * math.Sqrt(3)

	for _, tc := range []struct {
		name     string
		toward   float64 // direction of the neighbour, degrees
		wantBase float64
	}{
		{"neighbour to the right means a corner at the top", 0, 30},
		{"neighbour up and to the right means a corner to the right", 30, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rad := radians(tc.toward)
			wall := Project(nanoleaf.Layout{
				SideLength: int(side),
				Panels: []nanoleaf.Panel{
					{ID: 1, X: 0, Y: 0, ShapeType: nanoleaf.ShapeHexagon},
					{
						ID:        2,
						X:         int(math.Round(gap * math.Cos(rad))),
						Y:         int(math.Round(gap * math.Sin(rad))),
						ShapeType: nanoleaf.ShapeHexagon,
					},
				},
			}, 0)

			if got := NewOutliner(wall).Polygon(nanoleaf.ShapeHexagon).Base; math.Abs(got-tc.wantBase) > 0.5 {
				t.Errorf("first corner at %v degrees, want %v", got, tc.wantBase)
			}
		})
	}
}

// TestALoneHexagonKeepsTheFallbackAngle covers the layout with nothing to
// infer from.
func TestALoneHexagonKeepsTheFallbackAngle(t *testing.T) {
	wall := Project(nanoleaf.Layout{
		SideLength: 100,
		Panels:     []nanoleaf.Panel{{ID: 1, ShapeType: nanoleaf.ShapeHexagon}},
	}, 0)
	want := PolygonOf(nanoleaf.ShapeHexagon, 1).Base
	if got := NewOutliner(wall).Polygon(nanoleaf.ShapeHexagon).Base; got != want {
		t.Errorf("first corner at %v degrees, want the fallback %v", got, want)
	}
}

// TestMiniTrianglesAreHalvedOnlyInAMixedSet pins the one guess in the
// drawing: a device reports a single side length, and a set holding both
// sizes of triangle has two.
func TestMiniTrianglesAreHalvedOnlyInAMixedSet(t *testing.T) {
	mini := nanoleaf.Panel{ID: 2, X: 200, Y: 0, ShapeType: nanoleaf.ShapeMiniTriangle}

	mixed := NewOutliner(Project(nanoleaf.Layout{
		SideLength: 134,
		Panels:     []nanoleaf.Panel{{ID: 1, ShapeType: nanoleaf.ShapeTriangle}, mini},
	}, 0))
	if got := mixed.Side(nanoleaf.ShapeMiniTriangle); got != 67 {
		t.Errorf("mixed set draws a mini triangle with side %v, want 67", got)
	}
	if got := mixed.Side(nanoleaf.ShapeTriangle); got != 134 {
		t.Errorf("mixed set draws a full triangle with side %v, want 134", got)
	}

	only := NewOutliner(Project(nanoleaf.Layout{
		SideLength: 67,
		Panels:     []nanoleaf.Panel{mini},
	}, 0))
	if got := only.Side(nanoleaf.ShapeMiniTriangle); got != 67 {
		t.Errorf("mini-only set draws a mini triangle with side %v, want the reported 67", got)
	}
}

// TestADeviceWithNoSideLengthIsStillDrawn keeps a firmware quirk from
// producing a wall of zero-sized panels.
func TestADeviceWithNoSideLengthIsStillDrawn(t *testing.T) {
	wall := Project(nanoleaf.Layout{
		Panels: []nanoleaf.Panel{{ID: 1, ShapeType: nanoleaf.ShapeTriangle}},
	}, 0)
	if got := NewOutliner(wall).Side(nanoleaf.ShapeTriangle); got <= 0 {
		t.Errorf("a layout with no side length draws panels at %v", got)
	}
}

// TestFacingOrientationIsTheInverseOfEdgeAngle is what the shape builder
// stands on: a panel turned by FacingOrientation really does face the way it
// was asked to.
func TestFacingOrientationIsTheInverseOfEdgeAngle(t *testing.T) {
	for _, shapeType := range []int{nanoleaf.ShapeTriangle, nanoleaf.ShapeSquare, nanoleaf.ShapeHexagon} {
		p := PolygonOf(shapeType, 134)
		for edge := range p.Sides {
			for _, angle := range []float64{0, 30, 90, 137, 210, 359} {
				o := p.FacingOrientation(edge, angle)
				if got := wrapDegrees180(p.EdgeAngle(o, edge) - angle); math.Abs(got) > 1e-9 {
					t.Errorf("shape %d edge %d asked to face %v faces %v off",
						shapeType, edge, angle, got)
				}
			}
		}
	}
}

// TestEdgeTowardReportsWhenNothingFacesThatWay covers the case a builder has
// to refuse: an upward triangle has no upward edge, and picking the nearest
// one anyway would stick a panel on at 60 degrees from where it was asked
// for.
func TestEdgeTowardReportsWhenNothingFacesThatWay(t *testing.T) {
	triangle := PolygonOf(nanoleaf.ShapeTriangle, 134)

	// Pointing up: an upward triangle's edges face 150, 270 and 30.
	if _, off := triangle.EdgeToward(0, 90); math.Abs(off-60) > 1e-9 {
		t.Errorf("an upward triangle reports an upward edge %v degrees off, want 60", off)
	}
	// Its bottom edge, which it does have.
	if edge, off := triangle.EdgeToward(0, 270); edge != 1 || off > 1e-9 {
		t.Errorf("the downward edge is %d, %v off; want edge 1 exactly", edge, off)
	}

	hexagon := PolygonOf(nanoleaf.ShapeHexagon, 134)
	if edge, off := hexagon.EdgeToward(0, 180); edge != 2 || off > 1e-9 {
		t.Errorf("a hexagon's left edge is %d, %v off; want edge 2 exactly", edge, off)
	}
}
