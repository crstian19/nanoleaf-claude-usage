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
		{"hexagon", nanoleaf.ShapeHexagon, 6, 0},
		{"elements hexagon", nanoleaf.ShapeElementsHexagon, 6, 0},
		{"skylight panel", nanoleaf.ShapeSkylight, 4, 45},
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
		{"neighbour to the right means a corner 30 degrees round", 0, 30},
		{"neighbour up and to the right means a corner at 60", 30, 60},
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

// TestTheSideLengthComesFromTheShape is the rule Nanoleaf's own
// documentation states: the sideLength a device reports is deprecated as of
// firmware 5.0.0, because one number cannot describe a wall holding two
// sizes. A Shapes triangle is 134 and a hexagon 67, and both can be on the
// same wall.
func TestTheSideLengthComesFromTheShape(t *testing.T) {
	// A layout that reports a length for triangles, holding both sizes and
	// a hexagon.
	wall := Project(nanoleaf.Layout{
		SideLength: 134,
		Panels: []nanoleaf.Panel{
			{ID: 1, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 2, X: 300, ShapeType: nanoleaf.ShapeMiniTriangle},
			{ID: 3, X: 600, ShapeType: nanoleaf.ShapeHexagon},
			{ID: 4, X: 900, ShapeType: 200},
		},
	}, 0)
	outliner := NewOutliner(wall)

	for _, tc := range []struct {
		shapeType int
		want      float64
	}{
		{nanoleaf.ShapeTriangle, 134},
		{nanoleaf.ShapeMiniTriangle, 67},
		{nanoleaf.ShapeHexagon, 67},
		{nanoleaf.ShapeElementsHexagon, 134},
		{nanoleaf.ShapeSkylight, 180},
		{nanoleaf.ShapeLightPanel, 150},
		{nanoleaf.ShapeSquare, 100},
		{nanoleaf.ShapeLines, 154},
		// Nothing is published for a shape this version does not know,
		// so the layout's own number is all there is.
		{200, 134},
	} {
		if got := outliner.Side(tc.shapeType); got != tc.want {
			t.Errorf("%s is drawn with side %v, want %v",
				nanoleaf.ShapeName(tc.shapeType), got, tc.want)
		}
	}

	// A mini triangle's edge is exactly half a full one's, which is what
	// lets two of them sit along it.
	full, _ := SideOf(nanoleaf.ShapeTriangle)
	mini, _ := SideOf(nanoleaf.ShapeMiniTriangle)
	if full != mini*2 {
		t.Errorf("a triangle is %v and a mini %v: two minis no longer cover one edge", full, mini)
	}
	// And a hexagon's edge is the same as a mini's, which is what lets
	// them be mixed.
	hex, _ := SideOf(nanoleaf.ShapeHexagon)
	if hex != mini {
		t.Errorf("a hexagon is %v and a mini triangle %v: they no longer meet", hex, mini)
	}
}

// TestLinesAreDrawnAsBars covers the shapes no regular polygon describes: a
// Lines bar and a lightstrip segment are long and thin, and they are turned
// by the orientation the device reports.
func TestLinesAreDrawnAsBars(t *testing.T) {
	for _, shapeType := range []int{nanoleaf.ShapeLines, nanoleaf.ShapeLinesSingleZone, nanoleaf.ShapeLightstrip4D} {
		side, ok := SideOf(shapeType)
		if !ok {
			t.Fatalf("%s has no published length", nanoleaf.ShapeName(shapeType))
		}
		p := PolygonOf(shapeType, side)
		if p.Regular() {
			t.Errorf("%s is drawn as a regular polygon", nanoleaf.ShapeName(shapeType))
		}

		corners := p.Outline(0, 0, 0)
		if len(corners) != 4 {
			t.Fatalf("%s is drawn with %d corners", nanoleaf.ShapeName(shapeType), len(corners))
		}

		// It lies along its own orientation, so it is as long as the
		// published length and much thinner than that.
		var width, height float64
		for _, c := range corners {
			width = math.Max(width, math.Abs(c.X)*2)
			height = math.Max(height, math.Abs(c.Y)*2)
		}
		if math.Abs(width-side) > 1e-9 {
			t.Errorf("%s is %v long, want %v", nanoleaf.ShapeName(shapeType), width, side)
		}
		if height >= width/4 {
			t.Errorf("%s is %v across and %v long, which is not a bar",
				nanoleaf.ShapeName(shapeType), height, width)
		}

		// Turned a quarter of the way round, it stands up instead.
		turned := p.Outline(0, 0, 90)
		var upright float64
		for _, c := range turned {
			upright = math.Max(upright, math.Abs(c.Y)*2)
		}
		if math.Abs(upright-side) > 1e-9 {
			t.Errorf("%s turned upright is %v tall, want %v", nanoleaf.ShapeName(shapeType), upright, side)
		}
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

	// A hexagon with a corner to the right has a flat top, so an edge
	// faces straight up and none faces sideways.
	hexagon := PolygonOf(nanoleaf.ShapeHexagon, 134)
	if edge, off := hexagon.EdgeToward(0, 90); edge != 1 || off > 1e-9 {
		t.Errorf("a hexagon's top edge is %d, %v off; want edge 1 exactly", edge, off)
	}
	if _, off := hexagon.EdgeToward(0, 180); math.Abs(off-30) > 1e-9 {
		t.Errorf("a flat-topped hexagon reports a sideways edge %v off, want 30", off)
	}
}

// TestEveryPanelThatLightsHasALength is the cross-check between the two
// halves of the same table: a panel this program will draw needs a size to
// draw it at.
//
// The pieces with no LEDs are exempt, and Nanoleaf's own table is
// inconsistent about them: a Lines connector is given as 11 while the Rhythm
// module and the Shapes controller are given as N/A. Nothing draws them, so
// it does not matter.
func TestEveryPanelThatLightsHasALength(t *testing.T) {
	for shapeType := range 40 {
		if !nanoleaf.IsKnownShape(shapeType) {
			continue
		}
		if !(nanoleaf.Panel{ShapeType: shapeType}).IsLight() {
			continue
		}
		side, ok := SideOf(shapeType)
		if !ok {
			t.Errorf("%s has a name but no published edge length", nanoleaf.ShapeName(shapeType))
			continue
		}
		if side <= 0 {
			t.Errorf("%s has an edge length of %v", nanoleaf.ShapeName(shapeType), side)
		}

		// And it draws as something with an area.
		corners := PolygonOf(shapeType, side).Outline(0, 0, 0)
		if len(corners) < 3 {
			t.Errorf("%s is drawn with %d corners", nanoleaf.ShapeName(shapeType), len(corners))
		}
	}
}
