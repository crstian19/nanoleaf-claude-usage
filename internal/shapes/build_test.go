package shapes

import (
	"math"
	"strings"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// centres is every panel's position, for comparing two ways of building the
// same wall.
func centres(t *testing.T, b *Builder) map[[2]int]int {
	t.Helper()
	layout, err := b.Layout()
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[[2]int]int, len(layout.Panels))
	for _, p := range layout.Panels {
		out[[2]int{p.X, p.Y}] = p.ShapeType
	}
	return out
}

// TestTheOrderPanelsGoOnDoesNotMatter is the promise the builder makes.
//
// A wall is described by sticking panels to edges, so the same wall can be
// described in any order and has to come out the same. Building it from a
// lattice the caller keeps in their head would not have this property, which
// is the reason the builder exists.
func TestTheOrderPanelsGoOnDoesNotMatter(t *testing.T) {
	const side = 134
	hex := nanoleaf.ShapeHexagon

	// A honeycomb, clockwise from the right.
	clockwise := Start(side, hex)
	for edge := range 6 {
		clockwise.Attach(0, edge, hex)
	}

	// The same honeycomb, built outwards and backwards: two opposite
	// petals first, then the rest in the other direction.
	scattered := Start(side, hex)
	scattered.Attach(0, 3, hex)
	scattered.Attach(0, 0, hex)
	for _, edge := range []int{5, 4, 2, 1} {
		scattered.Attach(0, edge, hex)
	}

	first, second := centres(t, clockwise), centres(t, scattered)
	if len(first) != 7 || len(second) != 7 {
		t.Fatalf("built %d and %d panels, want 7 each", len(first), len(second))
	}
	for at, shapeType := range first {
		if got, ok := second[at]; !ok || got != shapeType {
			t.Errorf("the second wall has %v at (%d, %d), the first has shape %d",
				ok, at[0], at[1], shapeType)
		}
	}
}

// TestAnOccupiedEdgeIsRefused is what makes any order safe: attaching to an
// edge something is already on has to fail rather than stack two panels in
// one place.
func TestAnOccupiedEdgeIsRefused(t *testing.T) {
	b := Start(134, nanoleaf.ShapeTriangle)
	b.Attach(0, 0, nanoleaf.ShapeTriangle)
	b.Attach(0, 0, nanoleaf.ShapeTriangle)

	_, err := b.Layout()
	if err == nil {
		t.Fatal("two panels were placed on one edge")
	}
	if !strings.Contains(err.Error(), "would land on") {
		t.Errorf("the error does not say what happened: %v", err)
	}
}

// TestShapesThatDoNotMeetAreRefused covers the pairs that cannot be stuck
// together, rather than placing them where their edges do not line up.
func TestShapesThatDoNotMeetAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		build  func() *Builder
		expect string
	}{
		{
			name: "a mini triangle on a full one is half an edge",
			build: func() *Builder {
				b := Start(134, nanoleaf.ShapeTriangle)
				b.Attach(0, 0, nanoleaf.ShapeMiniTriangle)
				return b
			},
			expect: "edge to edge",
		},
		{
			name: "an edge a triangle does not have",
			build: func() *Builder {
				b := Start(134, nanoleaf.ShapeTriangle)
				b.Attach(0, 3, nanoleaf.ShapeTriangle)
				return b
			},
			expect: "does not exist",
		},
		{
			name: "a panel that was never placed",
			build: func() *Builder {
				b := Start(134, nanoleaf.ShapeTriangle)
				b.Attach(7, 0, nanoleaf.ShapeTriangle)
				return b
			},
			expect: "no panel 7",
		},
		{
			name: "a direction no edge faces",
			build: func() *Builder {
				b := Start(134, nanoleaf.ShapeTriangle)
				b.AttachToward(0, up, nanoleaf.ShapeTriangle)
				return b
			},
			expect: "no edge",
		},
		{
			name: "mini triangles with nothing to give them a scale",
			build: func() *Builder {
				b := Start(134, nanoleaf.ShapeHexagon)
				b.Attach(0, 0, nanoleaf.ShapeMiniTriangle)
				return b
			},
			expect: "drawn at",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build().Layout()
			if err == nil {
				t.Fatal("it was accepted")
			}
			if !strings.Contains(err.Error(), tc.expect) {
				t.Errorf("the error is %q, which does not mention %q", err, tc.expect)
			}
		})
	}
}

// TestTheFirstFailureSticks keeps a broken wall from being half built: every
// placement after a failure is refused too, so nothing lands on a guess.
func TestTheFirstFailureSticks(t *testing.T) {
	b := Start(134, nanoleaf.ShapeTriangle)
	bad := b.Attach(0, 9, nanoleaf.ShapeTriangle)
	if bad != invalidPanel {
		t.Errorf("a refused placement returned index %d", bad)
	}

	// A placement that would be fine on its own.
	if got := b.Attach(0, 0, nanoleaf.ShapeTriangle); got != invalidPanel {
		t.Errorf("a placement after a failure returned index %d", got)
	}
	if _, err := b.Layout(); err == nil || !strings.Contains(err.Error(), "edge 9") {
		t.Errorf("the layout reports %v, want the first failure", err)
	}
}

// TestTwoMinisCoverOneEdge is the geometry AttachHalf exists for: two mini
// triangles fit along one edge of a full triangle, each covering half of it.
func TestTwoMinisCoverOneEdge(t *testing.T) {
	const side = 134.0
	b := Start(int(side), nanoleaf.ShapeTriangle)
	// The downward edge of an upward triangle.
	b.AttachHalfToward(0, 270, nanoleaf.ShapeMiniTriangle, 0)
	b.AttachHalfToward(0, 270, nanoleaf.ShapeMiniTriangle, 1)

	layout, err := b.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Panels) != 3 {
		t.Fatalf("built %d panels, want 3", len(layout.Panels))
	}

	wall := render.Project(layout, 0)
	outliner := render.NewOutliner(wall)
	host := wall.Panels[0]
	hostPoly := outliner.Polygon(host.Panel.ShapeType)

	// The shared edge of the full triangle, as a segment.
	hostEdge, _ := hostPoly.EdgeToward(host.Orientation, 270)
	corners := hostPoly.Outline(host.X, host.Y, host.Orientation)
	from := corners[hostEdge]
	to := corners[(hostEdge+1)%hostPoly.Sides]

	for _, mini := range wall.Panels[1:] {
		// Each mini has two corners on that edge: one of the full
		// triangle's own corners, and the middle of the edge. That is
		// what covering half an edge means, and why their corners
		// cannot simply coincide.
		on := 0
		for _, c := range outliner.Outline(mini) {
			if pointToSegment(c, from, to) < 0.75 {
				on++
			}
		}
		if on != 2 {
			t.Errorf("mini triangle %d has %d corners on the edge it was attached to, want 2",
				mini.Panel.ID, on)
		}

		// And half an edge each, not the same half.
		if math.Abs(mini.X-host.X) < 1 && math.Abs(mini.Y-host.Y) < 1 {
			t.Errorf("mini triangle %d landed on the host", mini.Panel.ID)
		}
	}

	first, second := wall.Panels[1], wall.Panels[2]
	if gap := math.Hypot(first.X-second.X, first.Y-second.Y); math.Abs(gap-side/2) > 1 {
		t.Errorf("the two minis are %.1f apart, want half an edge (%.1f)", gap, side/2)
	}
}

// pointToSegment is the distance from a point to a line segment.
func pointToSegment(p, from, to render.Corner) float64 {
	dx, dy := to.X-from.X, to.Y-from.Y
	length := dx*dx + dy*dy
	if length == 0 {
		return math.Hypot(p.X-from.X, p.Y-from.Y)
	}
	t := ((p.X-from.X)*dx + (p.Y-from.Y)*dy) / length
	t = math.Min(1, math.Max(0, t))
	return math.Hypot(p.X-(from.X+t*dx), p.Y-(from.Y+t*dy))
}

// TestRoundingDoesNotDriftAcrossAWall is why the builder holds exact
// positions and rounds once.
//
// Every panel is placed against the one before it. Rounding each step to the
// whole units a device reports compounded, and twelve panels along a wall the
// tiling was four units out -- enough that the far end no longer met.
func TestRoundingDoesNotDriftAcrossAWall(t *testing.T) {
	const side = 67
	b := Start(side, nanoleaf.ShapeMiniTriangle)
	last := 0
	for range 19 {
		last = b.AttachToward(last, right, nanoleaf.ShapeMiniTriangle)
	}

	layout, err := b.Layout()
	if err != nil {
		t.Fatal(err)
	}

	// Every second panel points the same way and sits one side length on
	// from the last, all the way along.
	for i := 2; i < len(layout.Panels); i += 2 {
		step := layout.Panels[i].X - layout.Panels[i-2].X
		if step != side {
			t.Errorf("panels %d and %d are %d apart, want %d", i-2, i, step, side)
		}
		if y := layout.Panels[i].Y; y != 0 {
			t.Errorf("panel %d has drifted to y=%d, want 0", i, y)
		}
	}
}
