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
	hex := nanoleaf.ShapeHexagon

	// A honeycomb, clockwise from the right.
	clockwise := Start(hex)
	for edge := range 6 {
		clockwise.Attach(clockwise.First(), edge, hex)
	}

	// The same honeycomb, built outwards and backwards: two opposite
	// petals first, then the rest in the other direction.
	scattered := Start(hex)
	scattered.Attach(scattered.First(), 3, hex)
	scattered.Attach(scattered.First(), 0, hex)
	for _, edge := range []int{5, 4, 2, 1} {
		scattered.Attach(scattered.First(), edge, hex)
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
	b := Start(nanoleaf.ShapeTriangle)
	b.Attach(b.First(), 0, nanoleaf.ShapeTriangle)
	b.Attach(b.First(), 0, nanoleaf.ShapeTriangle)

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
				b := Start(nanoleaf.ShapeTriangle)
				b.Attach(b.First(), 0, nanoleaf.ShapeMiniTriangle)
				return b
			},
			expect: "edge to edge",
		},
		{
			name: "an edge a triangle does not have",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeTriangle)
				b.Attach(b.First(), 3, nanoleaf.ShapeTriangle)
				return b
			},
			expect: "does not exist",
		},
		{
			name: "a panel that was never placed",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeTriangle)
				b.Attach(7, 0, nanoleaf.ShapeTriangle)
				return b
			},
			expect: "no panel 7",
		},
		{
			name: "a direction no edge faces",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeTriangle)
				b.AttachToward(b.First(), up, nanoleaf.ShapeTriangle)
				return b
			},
			expect: "no edge",
		},
		{
			// Both are 134 across the edge, which is a
			// coincidence: they are different product lines and do
			// not clip together.
			name: "an Elements hexagon on a Shapes triangle",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeTriangle)
				b.Attach(b.First(), 0, nanoleaf.ShapeElementsHexagon)
				return b
			},
			expect: "do not clip",
		},
		{
			name: "a Lines bar, which joins at a connector",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeTriangle)
				b.Attach(b.First(), 0, nanoleaf.ShapeLines)
				return b
			},
			expect: "edge to edge",
		},
		{
			name: "a wall started from a bar",
			build: func() *Builder {
				return Start(nanoleaf.ShapeLightstrip4D)
			},
			expect: "edge to edge",
		},
		{
			name: "a piece with no LEDs",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeTriangle)
				b.Attach(b.First(), 0, nanoleaf.ShapeLinesConnector)
				return b
			},
			expect: "no LEDs",
		},
		{
			name: "the Elements hexagon that lights its corners",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeElementsHexagon)
				b.Attach(b.First(), 0, nanoleaf.ShapeElementsCorner)
				return b
			},
			expect: "edge to edge",
		},
		{
			name: "a shape no version of this knows",
			build: func() *Builder {
				b := Start(nanoleaf.ShapeTriangle)
				b.Attach(b.First(), 0, 200)
				return b
			},
			expect: "no published edge length",
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
	b := Start(nanoleaf.ShapeTriangle)
	bad := b.Attach(b.First(), 9, nanoleaf.ShapeTriangle)
	if bad != invalidPanel {
		t.Errorf("a refused placement returned index %d", bad)
	}

	// A placement that would be fine on its own.
	if got := b.Attach(b.First(), 0, nanoleaf.ShapeTriangle); got != invalidPanel {
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
	b := Start(nanoleaf.ShapeTriangle)
	// The downward edge of an upward triangle.
	b.AttachHalfToward(b.First(), 270, nanoleaf.ShapeMiniTriangle, 0)
	b.AttachHalfToward(b.First(), 270, nanoleaf.ShapeMiniTriangle, 1)

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
	b := Start(nanoleaf.ShapeMiniTriangle)
	last := b.First()
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

// TestSpotsAreEveryPlaceAPanelFits is what dragging a panel onto a wall
// depends on: the places it can land are worked out here, so the page only
// has to draw them.
func TestSpotsAreEveryPlaceAPanelFits(t *testing.T) {
	hex := nanoleaf.ShapeHexagon

	// An empty wall has one place to put anything: the middle.
	empty := Empty()
	if spots := empty.Spots(hex); len(spots) != 1 || spots[0].Panel != invalidPanel {
		t.Fatalf("an empty wall offers %+v, want one spot attached to nothing", spots)
	}

	b := Start(hex)
	if got := len(b.Spots(hex)); got != 6 {
		t.Errorf("a lone hexagon offers %d spots, want 6", got)
	}

	// Filling one of them takes it off the list, and offers the new
	// panel's own five free edges instead.
	b.Attach(b.First(), 0, hex)
	if got := len(b.Spots(hex)); got != 10 {
		t.Errorf("two hexagons offer %d spots, want 10", got)
	}

	// Every spot has to be somewhere a panel really can go.
	for _, spot := range b.Spots(hex) {
		trial := Start(hex)
		trial.Attach(trial.First(), 0, hex)
		if id := trial.Attach(spot.Panel, spot.Edge, hex); id == invalidPanel {
			_, err := trial.Layout()
			t.Errorf("spot %+v was offered but refused: %v", spot, err)
		}
	}
}

// TestAHalfSizePanelGetsTwoSpotsPerEdge covers the mixed set: two mini
// triangles fit along one edge of a full one, so a mini being dragged has two
// places to land on every edge.
func TestAHalfSizePanelGetsTwoSpotsPerEdge(t *testing.T) {
	b := Start(nanoleaf.ShapeTriangle)

	full := b.Spots(nanoleaf.ShapeTriangle)
	if len(full) != 3 {
		t.Errorf("a triangle offers %d spots to another triangle, want 3", len(full))
	}
	for _, spot := range full {
		if spot.Half != NoHalf {
			t.Errorf("a full-size spot reports half %d", spot.Half)
		}
	}

	minis := b.Spots(nanoleaf.ShapeMiniTriangle)
	if len(minis) != 6 {
		t.Fatalf("a triangle offers %d spots to a mini, want 6", len(minis))
	}
	halves := map[int]int{}
	for _, spot := range minis {
		halves[spot.Half]++
	}
	if halves[0] != 3 || halves[1] != 3 {
		t.Errorf("the halves are %v, want three of each", halves)
	}
}

// TestRemovingAPanelLeavesTheOthersAlone matters because a page holds panel
// identities from a moment ago: if removing one renumbered the rest, the next
// click would land on a different panel.
func TestRemovingAPanelLeavesTheOthersAlone(t *testing.T) {
	b := Start(nanoleaf.ShapeHexagon)
	first := b.First()
	second := b.Attach(first, 0, nanoleaf.ShapeHexagon)
	third := b.Attach(first, 2, nanoleaf.ShapeHexagon)

	if err := b.Remove(second); err != nil {
		t.Fatal(err)
	}
	layout, err := b.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Panels) != 2 {
		t.Fatalf("%d panels are left, want 2", len(layout.Panels))
	}

	left := map[int]bool{}
	for _, p := range layout.Panels {
		left[p.ID] = true
	}
	if !left[first] || !left[third] {
		t.Errorf("the panels left are %v, want %d and %d", left, first, third)
	}

	// The edge the removed panel was on is free again.
	free := false
	for _, spot := range b.Spots(nanoleaf.ShapeHexagon) {
		if spot.Panel == first && spot.Edge == 0 {
			free = true
		}
	}
	if !free {
		t.Error("the edge the removed panel was on is still taken")
	}

	if err := b.Remove(second); err == nil {
		t.Error("removing the same panel twice was accepted")
	}
}

// TestATriedChangeLeavesNothingBehind is how the editor keeps a refused
// placement from wedging a wall: every edit is applied to a copy, and the
// copy is kept only if it worked.
func TestATriedChangeLeavesNothingBehind(t *testing.T) {
	b := Start(nanoleaf.ShapeHexagon)
	b.Attach(b.First(), 0, nanoleaf.ShapeHexagon)

	// A refused placement, on a copy.
	trial := b.Clone()
	trial.Attach(trial.First(), 0, nanoleaf.ShapeHexagon)
	if _, err := trial.Layout(); err == nil {
		t.Fatal("a placement on an occupied edge was accepted")
	}

	// The wall it was copied from is untouched and still usable.
	layout, err := b.Layout()
	if err != nil {
		t.Fatalf("the original wall is broken: %v", err)
	}
	if len(layout.Panels) != 2 {
		t.Errorf("the original wall has %d panels, want 2", len(layout.Panels))
	}
	if id := b.Attach(b.First(), 1, nanoleaf.ShapeHexagon); id == invalidPanel {
		t.Error("the original wall refuses new panels after a failed trial elsewhere")
	}

	// And a kept copy is independent of the original.
	kept := b.Clone()
	kept.Attach(kept.First(), 2, nanoleaf.ShapeHexagon)
	if b.Panels() == kept.Panels() {
		t.Errorf("both walls have %d panels: the copy is not independent", b.Panels())
	}
}

// TestTheFirstPanelGoesOnAnEmptyWallOnly keeps an editor from dropping a
// panel in the middle of a wall that already has one, where it would overlap
// whatever is there.
func TestTheFirstPanelGoesOnAnEmptyWallOnly(t *testing.T) {
	b := Empty()
	if id := b.Place(nanoleaf.ShapeTriangle); id == invalidPanel {
		t.Fatalf("the first panel was refused: %v", b.err)
	}
	if b.Place(nanoleaf.ShapeTriangle) != invalidPanel {
		t.Error("a second panel was placed in the middle of the wall")
	}

	// A wall of minis declares their own size, which the first one sets.
	minis := Empty()
	minis.Place(nanoleaf.ShapeMiniTriangle)
	minis.AttachToward(minis.First(), right, nanoleaf.ShapeMiniTriangle)
	layout, err := minis.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if got := layout.Panels[1].X - layout.Panels[0].X; got != 34 {
		t.Errorf("mini triangles on their own wall are %d apart, want 34 (half of 67)", got)
	}
}

// TestHexagonsAndMiniTrianglesMeet is the pair the published edge lengths
// make possible, and the reason those lengths cannot come from the layout: a
// Shapes hexagon and a mini triangle are both 67, while a full triangle is
// 134.
func TestHexagonsAndMiniTrianglesMeet(t *testing.T) {
	b := Start(nanoleaf.ShapeHexagon)
	for edge := range 6 {
		if id := b.Attach(b.First(), edge, nanoleaf.ShapeMiniTriangle); id == invalidPanel {
			_, err := b.Layout()
			t.Fatalf("a mini triangle would not go on a hexagon: %v", err)
		}
	}

	layout, err := b.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Panels) != 7 {
		t.Errorf("the wall has %d panels, want 7", len(layout.Panels))
	}
	// The layout reports its first panel's length, which is the hexagon's.
	if layout.SideLength != 67 {
		t.Errorf("the wall reports a side length of %d, want 67", layout.SideLength)
	}

	// And two minis fit along one edge of a full triangle, which is the
	// other half of the same fact.
	full := Start(nanoleaf.ShapeTriangle)
	for half := range 2 {
		if id := full.AttachHalfToward(full.First(), 270, nanoleaf.ShapeMiniTriangle, half); id == invalidPanel {
			_, err := full.Layout()
			t.Fatalf("a mini triangle would not go on half a triangle edge: %v", err)
		}
	}
	if got := full.Panels(); got != 3 {
		t.Errorf("the wall has %d panels, want 3", got)
	}
}
