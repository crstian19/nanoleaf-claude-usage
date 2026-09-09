package shapes

import (
	"errors"
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
	// side is the length reported in the finished layout. Every shape has
	// its own published length, so this is only what a device would put in
	// the field it deprecated: the first panel's.
	side float64

	panels []placed
	nextID int
	err    error
}

// placed is a panel as the builder holds it, before it is reported.
//
// The coordinates are exact. A device reports whole units and so does the
// finished layout, but rounding as each panel goes down would compound: every
// panel is placed against the one before it, so half a unit of rounding per
// step drifted four units across twelve panels, which is enough to pull a
// tiling apart at the far end of a wall.
type placed struct {
	id          int
	x, y        float64
	orientation float64
	shapeType   int

	// gone marks a panel that was taken off the wall. The slot stays, so
	// that the identity of every other panel survives a removal: a page
	// holding panel IDs from a moment ago must not find them pointing at
	// something else.
	gone bool
}

// Empty begins an arrangement with nothing on it, for an editor to fill.
func Empty() *Builder {
	return &Builder{nextID: firstID}
}

// Start begins an arrangement with one panel, at the origin, unturned.
func Start(shapeType int) *Builder {
	b := Empty()
	b.Place(shapeType)
	return b
}

// sideOf is the published edge length of a shape.
//
// Every shape has one, and it is the shape's own rather than the wall's: a
// Shapes triangle is 134 and a hexagon 67, and both go on the same wall.
// Nanoleaf deprecated the single length a device reports for exactly that
// reason.
func sideOf(shapeType int) (float64, error) {
	side, ok := render.SideOf(shapeType)
	if !ok {
		return 0, fmt.Errorf("shapes: no published edge length for %s, so it cannot be built with",
			nanoleaf.ShapeName(shapeType))
	}
	if side <= 0 {
		return 0, fmt.Errorf("shapes: %s has no edge to attach to", nanoleaf.ShapeName(shapeType))
	}
	return side, nil
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

// First is the identity of the panel Start placed, which is where a wall
// described in code begins.
func (b *Builder) First() int { return firstID }

// Panels is how many panels are on the wall.
func (b *Builder) Panels() int {
	n := 0
	for _, p := range b.panels {
		if !p.gone {
			n++
		}
	}
	return n
}

// Place puts the first panel on an empty wall, at the origin.
//
// The first panel also decides what the finished layout reports as its side
// length, the way a device reports the length of whatever it is mostly made
// of.
func (b *Builder) Place(shapeType int) int {
	if b.err != nil {
		return invalidPanel
	}
	if b.Panels() > 0 {
		return b.fail(errors.New("shapes: the wall already has a panel; attach to one of its edges"))
	}
	side, err := b.buildable(shapeType)
	if err != nil {
		return b.fail(err)
	}
	b.side = side
	return b.place(placed{shapeType: shapeType}, false)
}

// buildable reports the edge length of a shape a wall can be built from, and
// refuses the ones it cannot.
func (b *Builder) buildable(shapeType int) (float64, error) {
	if blankShape(shapeType) {
		return 0, fmt.Errorf("shapes: a %s has no LEDs, so it is not part of a wall",
			nanoleaf.ShapeName(shapeType))
	}
	side, err := sideOf(shapeType)
	if err != nil {
		return 0, err
	}
	if !edgeTiled(shapeType) {
		return 0, fmt.Errorf("shapes: %s panels are not put together edge to edge",
			nanoleaf.ShapeName(shapeType))
	}
	return side, nil
}

// edgeTiled reports whether panels of this kind are put together edge to
// edge, which is the only way this package knows how to build a wall.
//
// Three kinds are not. A Lines bar and a lightstrip segment join end to end
// at a connector, and the angles allowed there are the connector's business.
// An Elements hexagon that lights its corners reports six panels for one
// physical piece, so those six are parts of a hexagon rather than six
// hexagons that meet.
func edgeTiled(shapeType int) bool {
	switch shapeType {
	case nanoleaf.ShapeElementsCorner,
		nanoleaf.ShapeLines, nanoleaf.ShapeLinesSingleZone, nanoleaf.ShapeLightstrip4D:
		return false
	default:
		if blankShape(shapeType) {
			return false
		}
		side, ok := render.SideOf(shapeType)
		return ok && side > 0 && render.PolygonOf(shapeType, side).Regular()
	}
}

// Tiles reports whether every panel of a sample meets its neighbours edge to
// edge, which is what makes it a wall rather than a figure.
func (s Sample) Tiles() bool {
	for _, p := range s.Layout.Panels {
		if p.IsLight() && !edgeTiled(p.ShapeType) {
			return false
		}
	}
	return true
}

// Remove takes a panel off the wall.
//
// What is left may be in two pieces, which is the editor's business rather
// than this package's: a person taking panels off a wall knows what they
// meant.
func (b *Builder) Remove(id int) error {
	if b.err != nil {
		return b.err
	}
	for i, p := range b.panels {
		if p.id == id && !p.gone {
			b.panels[i].gone = true
			return nil
		}
	}
	return fmt.Errorf("shapes: no panel %d is on the wall", id)
}

// Clone copies the wall, so a change can be tried without keeping it.
//
// That is how an editor works: every edit is applied to a copy, and the copy
// is kept only if it succeeded. A refused placement then leaves nothing
// behind, and the copies that were kept are the undo history.
func (b *Builder) Clone() *Builder {
	panels := make([]placed, len(b.panels))
	copy(panels, b.panels)
	return &Builder{
		side:   b.side,
		panels: panels,
		nextID: b.nextID,
		err:    b.err,
	}
}

// Layout returns the arrangement, as a device would report it.
func (b *Builder) Layout() (nanoleaf.Layout, error) {
	if b.err != nil {
		return nanoleaf.Layout{}, b.err
	}
	panels := make([]nanoleaf.Panel, 0, len(b.panels))
	for _, p := range b.panels {
		if p.gone {
			continue
		}
		panels = append(panels, nanoleaf.Panel{
			ID:          p.id,
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
	spot, err := b.candidate(panel, edge, shapeType, half, halfEdge)
	if err != nil {
		return b.fail(err)
	}
	return b.place(spot, true)
}

// candidate works out where a panel would go, without putting it there.
//
// Separate from attach because an editor has to show every place a panel
// could land before the user drops one, and the answer has to be the same
// geometry that placing it will use.
func (b *Builder) candidate(panel, edge, shapeType, half int, halfEdge bool) (placed, error) {
	host, err := b.panel(panel)
	if err != nil {
		return placed{}, err
	}

	hostPoly := b.polygon(host.shapeType)
	if edge < 0 || edge >= hostPoly.Sides {
		return placed{}, fmt.Errorf("shapes: panel %d has %d edges, so edge %d does not exist",
			panel, hostPoly.Sides, edge)
	}

	if _, err := b.buildable(shapeType); err != nil {
		return placed{}, err
	}

	// Panels only clip to panels of their own product line. Two shapes
	// can share an edge length by coincidence -- an Elements hexagon and
	// a Shapes triangle are both 134 -- and a wall mixing them would look
	// possible while being impossible to build.
	hostFamily, newFamily := nanoleaf.FamilyOf(host.shapeType), nanoleaf.FamilyOf(shapeType)
	if hostFamily != newFamily {
		return placed{}, fmt.Errorf("shapes: %s panels do not clip to %s panels", newFamily, hostFamily)
	}

	newPoly := b.polygon(shapeType)
	want := hostPoly.Side()
	if halfEdge {
		want /= 2
	}
	if math.Abs(newPoly.Side()-want) > sideTolerance {
		return placed{}, fmt.Errorf(
			"shapes: a %s has an edge of %.0f and a %s of %.0f: they do not meet edge to edge",
			nanoleaf.ShapeName(host.shapeType), hostPoly.Side(),
			nanoleaf.ShapeName(shapeType), newPoly.Side())
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

	return placed{
		x:           x,
		y:           y,
		orientation: newPoly.FacingOrientation(0, angle+180),
		shapeType:   shapeType,
	}, nil
}

// Spot is a place a panel of some kind can go: which edge of which panel it
// would stick to, and where its centre would land.
//
// Half is the half of the host's edge it would cover, or NoHalf when it
// covers the whole edge. Two mini triangles fit along the edge of a full one,
// so an edge offers two spots to a mini and one to anything its own size.
type Spot struct {
	Panel int
	Edge  int
	Half  int

	// X and Y are where the panel's centre would land, and Orientation
	// how it would be turned. Together they are enough to draw the panel
	// exactly as placing it would, which is what a page shows while a
	// panel is being dragged.
	X, Y        float64
	Orientation float64
}

// NoHalf means a panel covers a whole edge rather than half of one.
const NoHalf = -1

// Spots is every place a panel of this kind could go right now.
//
// It is what makes dragging a panel onto a wall possible: the places it can
// land are worked out here, from the same geometry that will place it, and
// the page only has to draw them and see which one the mouse is nearest. The
// browser is never asked to work out where a panel would fit.
//
// An empty wall offers one spot, at the origin, with no panel to attach to.
func (b *Builder) Spots(shapeType int) []Spot {
	if b.Panels() == 0 {
		return []Spot{{Panel: invalidPanel, Edge: 0, Half: NoHalf}}
	}

	if _, err := b.buildable(shapeType); err != nil {
		return nil
	}

	spots := make([]Spot, 0, len(b.panels)*3)
	for _, host := range b.panels {
		if host.gone || blankShape(host.shapeType) {
			continue
		}

		// A panel of the same size covers a whole edge; one whose edge
		// is half as long covers half of it, and there are two halves
		// to choose from.
		halves := []int{NoHalf}
		if b.polygon(shapeType).Side() < b.polygon(host.shapeType).Side()-sideTolerance {
			halves = []int{0, 1}
		}

		sides := b.polygon(host.shapeType).Sides
		for edge := range sides {
			for _, half := range halves {
				spot, err := b.candidate(host.id, edge, shapeType, max(half, 0), half != NoHalf)
				if err != nil {
					continue
				}
				if b.checkClear(spot) != nil {
					continue
				}
				spots = append(spots, Spot{
					Panel:       host.id,
					Edge:        edge,
					Half:        half,
					X:           spot.x,
					Y:           spot.y,
					Orientation: spot.orientation,
				})
			}
		}
	}
	return spots
}

// firstID is where panel identities start. Nothing depends on the value; it
// only has to look like the five-digit IDs a device reports rather than like
// an index, so nobody reads a built wall as a list of array positions.
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
	panel.id = b.nextID
	b.nextID++
	b.panels = append(b.panels, panel)
	return panel.id
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
	for _, other := range b.panels {
		if other.gone || blankShape(other.shapeType) {
			continue
		}
		gap := math.Hypot(panel.x-other.x, panel.y-other.y)
		// A hundredth of slack, which is floating point noise rather
		// than a real gap: positions are exact until the layout is
		// reported.
		if want := apothem + b.polygon(other.shapeType).Apothem(); gap < want*0.99 {
			return fmt.Errorf("shapes: a %s at (%.0f, %.0f) would land on panel %d, %.0f away",
				nanoleaf.ShapeName(panel.shapeType), panel.x, panel.y, other.id, gap)
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

// PolygonOf is the outline a panel of this kind would have on this wall,
// which is what a page draws while one is being dragged onto it.
func (b *Builder) PolygonOf(shapeType int) render.Polygon { return b.polygon(shapeType) }

// Side is the edge length this wall reports, which is its first panel's.
func (b *Builder) Side() int { return int(math.Round(b.side)) }

// polygon is the outline of a panel of this shape, at the shape's own
// published edge length. It is the same call the drawing makes, so a wall
// built edge to edge is drawn edge to edge.
func (b *Builder) polygon(shapeType int) render.Polygon {
	side, _ := render.SideOf(shapeType)
	return render.PolygonOf(shapeType, side)
}

// panel returns a panel on the wall by its identity.
func (b *Builder) panel(id int) (placed, error) {
	if b.err != nil {
		return placed{}, b.err
	}
	for _, p := range b.panels {
		if p.id != id {
			continue
		}
		if p.gone {
			return placed{}, fmt.Errorf("shapes: panel %d was taken off the wall", id)
		}
		return p, nil
	}
	return placed{}, fmt.Errorf("shapes: no panel %d has been placed", id)
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
