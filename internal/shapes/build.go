package shapes

import (
	"fmt"
	"math"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Builder composes an arrangement by sticking panels onto each other's edges.
//
// It exists so that a wall can be described the way it is actually built --
// another one to the right of that, two small ones under this -- instead of
// by working out centroids. Nanoleaf panels click together edge to edge, and
// so does this: a panel is placed against a whole edge of one already there,
// turned to lie flat against it, and refused if something is in the way.
//
// Panels may be added in any order, and the arrangement that comes out is the
// same either way, because every position is derived from the edge it was
// stuck to rather than from a lattice the caller has to keep in their head.
//
// What it checks is geometry, not whether two panels physically clip
// together. Every shape is built at the wall's own side length, so it will
// stick a square to a triangle of that size: a real tiling, and not a product
// Nanoleaf sells. The samples in this package stay within what does exist.
//
// The first failure is remembered and reported by Layout, so a wall reads as
// a list of instructions rather than a list of error checks. Every index
// returned after a failure is invalid, and using one is itself an error, so
// nothing is placed on a guess.
type Builder struct {
	side   float64
	panels []placed
	err    error

	// miniScale says the declared side length is already a mini
	// triangle's, because that is what the wall is made of.
	miniScale bool
}

// placed is a panel as the builder holds it, before it is reported.
//
// The coordinates are exact. A device reports whole units and so does the
// finished layout, but rounding as each panel goes down would compound: every
// panel is placed against the one before it, so half a unit of rounding per
// step drifted four units across twelve panels, which is enough to pull a
// tiling apart at the far end of a wall.
type placed struct {
	x, y        float64
	orientation float64
	shapeType   int
}

// Start begins an arrangement with one panel, at the origin, unturned.
func Start(side int, shapeType int) *Builder {
	b := &Builder{side: float64(side)}
	if side <= 0 {
		b.err = fmt.Errorf("shapes: side length %d is not a length", side)
		return b
	}
	b.panels = []placed{{shapeType: shapeType}}
	b.miniScale = shapeType == nanoleaf.ShapeMiniTriangle
	return b
}

// Attach places a panel against one edge of another, and returns its index.
//
// The two panels must have the same edge length, which is what "edge to edge"
// means. A mini triangle against a full one covers half an edge; that is
// AttachHalf.
func (b *Builder) Attach(panel, edge, shapeType int) int {
	return b.attach(panel, edge, shapeType, 0, false)
}

// AttachToward places a panel against whichever edge faces nearest to the
// given direction, in degrees counter-clockwise from the right.
//
// It refuses a direction no edge really faces, rather than picking the
// nearest and putting the panel somewhere else: an upward triangle has no
// upward edge, and a wall built on that assumption would come out wrong in a
// way only a person looking at it would notice.
func (b *Builder) AttachToward(panel int, degrees float64, shapeType int) int {
	edge, err := b.edgeToward(panel, degrees)
	if err != nil {
		return b.fail(err)
	}
	return b.Attach(panel, edge, shapeType)
}

// AttachHalf places a half-length panel against one half of an edge.
//
// Two mini triangles fit along the edge of a full one, which is how the two
// sizes are sold to be combined. half selects which of the two, 0 being the
// clockwise side of the edge's middle.
func (b *Builder) AttachHalf(panel, edge, shapeType, half int) int {
	if half != 0 && half != 1 {
		return b.fail(fmt.Errorf("shapes: half must be 0 or 1, got %d", half))
	}
	return b.attach(panel, edge, shapeType, half, true)
}

// AttachHalfToward is AttachHalf, by direction rather than by edge number.
func (b *Builder) AttachHalfToward(panel int, degrees float64, shapeType, half int) int {
	edge, err := b.edgeToward(panel, degrees)
	if err != nil {
		return b.fail(err)
	}
	return b.AttachHalf(panel, edge, shapeType, half)
}

// Blank adds a panel with no LEDs, such as the controller brick, at an offset
// from a panel already placed.
//
// Real devices report one, and a display that lit it would leave a hole in
// every frame, so a sample that has one is a sample that exercises the code
// which drops it.
func (b *Builder) Blank(panel int, shapeType int, dx, dy int) int {
	host, err := b.panel(panel)
	if err != nil {
		return b.fail(err)
	}
	return b.place(placed{
		x:         host.x + float64(dx),
		y:         host.y + float64(dy),
		shapeType: shapeType,
	}, false)
}

// Panels is how many panels have been placed.
func (b *Builder) Panels() int { return len(b.panels) }

// Layout returns the arrangement, as a device would report it.
func (b *Builder) Layout() (nanoleaf.Layout, error) {
	if b.err != nil {
		return nanoleaf.Layout{}, b.err
	}
	panels := make([]nanoleaf.Panel, 0, len(b.panels))
	for i, p := range b.panels {
		panels = append(panels, nanoleaf.Panel{
			ID:          firstID + i,
			X:           int(math.Round(p.x)),
			Y:           int(math.Round(p.y)),
			Orientation: render.WrapDegrees(int(math.Round(p.orientation))),
			ShapeType:   p.shapeType,
		})
	}
	return nanoleaf.Layout{
		NumPanels:  len(panels),
		SideLength: int(math.Round(b.side)),
		Panels:     panels,
	}, nil
}

// attach is the one piece of geometry in the builder.
//
// The new panel's centre sits its own apothem beyond the shared edge, and it
// is turned so that its own first edge faces back the way it came. Both come
// from render, which is also where the drawing gets them, so a wall built
// edge to edge is drawn edge to edge.
func (b *Builder) attach(panel, edge, shapeType, half int, halfEdge bool) int {
	host, err := b.panel(panel)
	if err != nil {
		return b.fail(err)
	}

	hostPoly := b.polygon(host.shapeType)
	if edge < 0 || edge >= hostPoly.Sides {
		return b.fail(fmt.Errorf("shapes: panel %d has %d edges, so edge %d does not exist",
			panel, hostPoly.Sides, edge))
	}

	// A mini triangle is only drawn at half size when the layout also
	// holds a full one, because that is all a device's single side length
	// can say. Building minis into a wall that has none would be built at
	// one scale and drawn at another.
	if shapeType == nanoleaf.ShapeMiniTriangle && !b.miniScale && !b.hasFullTriangle() {
		return b.fail(fmt.Errorf(
			"shapes: mini triangles here would be drawn at %v, not %v: start the wall from a triangle or from a mini",
			b.side, b.side/2))
	}

	newPoly := b.polygon(shapeType)
	want := hostPoly.Side()
	if halfEdge {
		want /= 2
	}
	if math.Abs(newPoly.Side()-want) > sideTolerance {
		return b.fail(fmt.Errorf(
			"shapes: a %s has an edge of %.0f and a %s of %.0f: they do not meet edge to edge",
			nanoleaf.ShapeName(host.shapeType), hostPoly.Side(),
			nanoleaf.ShapeName(shapeType), newPoly.Side()))
	}

	angle := hostPoly.EdgeAngle(host.orientation, edge)
	mid := hostPoly.EdgeMidpoint(host.x, host.y, host.orientation, edge)

	x, y := mid.X, mid.Y
	if halfEdge {
		// Along the edge, a quarter of the host's edge either side of
		// its middle, which is the centre of each half.
		along := radians(angle + 90)
		offset := hostPoly.Side() / 4
		if half == 0 {
			offset = -offset
		}
		x += offset * math.Cos(along)
		y += offset * math.Sin(along)
	}

	outward := radians(angle)
	x += newPoly.Apothem() * math.Cos(outward)
	y += newPoly.Apothem() * math.Sin(outward)

	return b.place(placed{
		x:           x,
		y:           y,
		orientation: newPoly.FacingOrientation(0, angle+180),
		shapeType:   shapeType,
	}, true)
}

// firstID is where panel IDs start. Nothing depends on the value; it only
// has to look like the five-digit IDs a device reports rather than like an
// index, so nobody reads a built wall as a list of array positions.
const firstID = 10100

// sideTolerance is how far two edge lengths may differ and still be called
// the same. Centroids are whole units on a real device, so a rounded position
// can shift an edge by a fraction of a unit.
const sideTolerance = 0.5

// place adds a panel, refusing a spot something is already in.
//
// checkGap is off for panels with no LEDs: a controller brick is not part of
// the tiling, and where it hangs is the installer's business.
func (b *Builder) place(panel placed, checkGap bool) int {
	if b.err != nil {
		return invalidPanel
	}
	if checkGap {
		if err := b.checkClear(panel); err != nil {
			return b.fail(err)
		}
	}
	b.panels = append(b.panels, panel)
	return len(b.panels) - 1
}

// checkClear reports whether a panel would land on top of one already
// placed.
//
// Two panels that share an edge are exactly the sum of their apothems apart,
// so anything closer than that overlaps. This is what makes the order the
// panels are added in not matter: attaching to an edge something is already
// on is refused rather than drawn twice.
func (b *Builder) checkClear(panel placed) error {
	apothem := b.polygon(panel.shapeType).Apothem()
	for i, other := range b.panels {
		if blankShape(other.shapeType) {
			continue
		}
		gap := math.Hypot(panel.x-other.x, panel.y-other.y)
		// A hundredth of slack, which is floating point noise rather
		// than a real gap: positions are exact until the layout is
		// reported.
		if want := apothem + b.polygon(other.shapeType).Apothem(); gap < want*0.99 {
			return fmt.Errorf("shapes: a %s at (%.0f, %.0f) would land on panel %d, %.0f away",
				nanoleaf.ShapeName(panel.shapeType), panel.x, panel.y, i, gap)
		}
	}
	return nil
}

// blankShape reports whether a shape has no LEDs, and so is not part of the
// tiling. It asks nanoleaf.Panel rather than keeping its own list.
func blankShape(shapeType int) bool {
	return !nanoleaf.Panel{ShapeType: shapeType}.IsLight()
}

// edgeToward finds the edge of a panel that faces a direction, and refuses
// one that is not really there.
func (b *Builder) edgeToward(panel int, degrees float64) (int, error) {
	host, err := b.panel(panel)
	if err != nil {
		return 0, err
	}

	poly := b.polygon(host.shapeType)
	edge, off := poly.EdgeToward(host.orientation, degrees)

	// Half the way to the next edge, so the nearest edge wins whenever
	// there is a nearest edge, and a direction sitting exactly between two
	// of them is refused. That second case is not a nicety: an upward
	// triangle has no upward edge, and picking one of the two beside it
	// would put the panel 60 degrees from where it was asked for.
	if limit := 180/float64(poly.Sides) - 1e-6; off > limit {
		return 0, fmt.Errorf(
			"shapes: no edge of panel %d (a %s) faces %.0f degrees: the two nearest are %.0f off each",
			panel, nanoleaf.ShapeName(host.shapeType), degrees, off)
	}
	return edge, nil
}

// polygon is the outline of a panel of this shape in this arrangement.
//
// Mini triangles are half the declared side length, unless the wall is made
// of them, in which case the declared length is already theirs. That is the
// same rule the drawing applies to a layout read from a device, and it has to
// be: a wall built at one scale and drawn at another would not tile on screen.
func (b *Builder) polygon(shapeType int) render.Polygon {
	side := b.side
	if shapeType == nanoleaf.ShapeMiniTriangle && !b.miniScale {
		side /= 2
	}
	return render.PolygonOf(shapeType, side)
}

// hasFullTriangle reports whether a full-size triangle has been placed, which
// is what tells the drawing that this wall's mini triangles are half size.
func (b *Builder) hasFullTriangle() bool {
	for _, p := range b.panels {
		if p.shapeType == nanoleaf.ShapeTriangle || p.shapeType == nanoleaf.ShapeLightPanel {
			return true
		}
	}
	return false
}

// panel returns a placed panel by index.
func (b *Builder) panel(i int) (placed, error) {
	if b.err != nil {
		return placed{}, b.err
	}
	if i < 0 || i >= len(b.panels) {
		return placed{}, fmt.Errorf("shapes: no panel %d has been placed", i)
	}
	return b.panels[i], nil
}

// invalidPanel is the index returned by a failed placement. Every later call
// that uses it fails too, which is what keeps a broken wall from being half
// built.
const invalidPanel = -1

// fail remembers the first error.
func (b *Builder) fail(err error) int {
	if b.err == nil {
		b.err = err
	}
	return invalidPanel
}

// mustLayout is for the samples in this package, which are constants of the
// program: a builder error in one is a bug in this file, not something a user
// can cause, and every sample is built by the tests.
func mustLayout(b *Builder) nanoleaf.Layout {
	layout, err := b.Layout()
	if err != nil {
		panic(err)
	}
	return layout
}

func radians(degrees float64) float64 { return degrees * math.Pi / 180 }
