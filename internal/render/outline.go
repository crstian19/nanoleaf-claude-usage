package render

import (
	"math"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Corner is a point of a panel's outline, in the same coordinates as the
// panel centroid it was computed from.
type Corner struct{ X, Y float64 }

// Polygon is the shape of one panel: how many sides it has, how far its
// corners sit from the centroid, and which way they face.
//
// Angles are degrees counter-clockwise from the positive X axis, and a
// panel's own reported orientation is added to Base.
//
// This is the one description of a panel's outline in the program. The page
// draws from it and the shape builder sticks panels together with it, so a
// wall that is built edge to edge is drawn edge to edge for the same reason.
type Polygon struct {
	// Sides is the number of edges, which is also the number of corners.
	Sides int
	// Radius is the distance from the centroid to a corner.
	Radius float64
	// Base is the angle of the first corner, before the panel's own
	// orientation is added.
	Base float64

	// Thickness, when set, makes the shape a bar: twice Radius long and
	// this far across, turned by Base plus the panel's orientation. That
	// is what a Lines bar and a lightstrip segment are, and no regular
	// polygon can describe one.
	Thickness float64
}

// Regular reports whether the shape is a regular polygon, which is what
// having edges of one length and a meaningful apothem depends on. A bar is
// not.
func (p Polygon) Regular() bool { return p.Thickness == 0 }

// unknownSides is how many corners an unrecognised shape is drawn with.
// Enough to read as a circle, which is the honest picture: the panel is
// there, and this version does not know its outline.
const unknownSides = 16

// SideLengths are the edge lengths Nanoleaf publishes for each shape, in the
// units panel positions are given in.
//
// They are in the layout section of Nanoleaf's own API documentation, which
// also says that the sideLength field a device reports is deprecated as of
// firmware 5.0.0, "since it cannot represent the side lengths of multiple
// shapes". A wall can hold two sizes at once -- a Shapes triangle is 134 and
// a hexagon 67 -- so the length has to come from the shape, not from the
// layout.
//
// The pieces with no LEDs are here too, at the length the documentation gives
// them, so that a caller which draws them can.
var sideLengths = map[int]float64{
	nanoleaf.ShapeLightPanel:    150,
	nanoleaf.ShapeSquare:        100,
	nanoleaf.ShapeSquareMaster:  100,
	nanoleaf.ShapeSquarePassive: 100,
	nanoleaf.ShapeHexagon:       67,
	nanoleaf.ShapeTriangle:      134,
	nanoleaf.ShapeMiniTriangle:  67,

	nanoleaf.ShapeElementsHexagon: 134,
	// The documentation gives this one as "33.5 / 58". 58 is the piece,
	// and each of its six lights is drawn at half that; see PolygonOf.
	nanoleaf.ShapeElementsCorner: 58,

	nanoleaf.ShapeLinesConnector:  11,
	nanoleaf.ShapeLines:           154,
	nanoleaf.ShapeLinesSingleZone: 77,
	nanoleaf.ShapeControllerCap:   11,
	nanoleaf.ShapePowerConnector:  11,

	nanoleaf.ShapeLightstrip4D: 50,

	nanoleaf.ShapeSkylight:        180,
	nanoleaf.ShapeSkylightPrimary: 180,
	nanoleaf.ShapeSkylightPassive: 180,
}

// SideOf is the published edge length of a shape, and whether there is one.
func SideOf(shapeType int) (float64, bool) {
	side, ok := sideLengths[shapeType]
	return side, ok
}

// barThickness is how thick a bar is drawn, as a fraction of its length.
//
// Wider than life: a Lines bar is about a twentieth of its length across, and
// at the size a wall is drawn on a page that is a hairline. It is a light bar
// either way, and being able to see it matters more than the proportion.
const barThickness = 1.0 / 12

// PolygonOf is the outline of a panel of the given shape, at the given side
// length.
//
// The triangle base of 90 degrees was verified against a real NL42 rather
// than assumed. On that device the panels reporting o=0 point up and the ones
// reporting o=180 point down, and their centroids are offset by half a side
// horizontally and one inradius vertically -- exactly a triangular tiling of
// the reported side length. Because a triangle is unchanged by a third of a
// turn, o values of 0, 120 and 240 all draw the same up-pointing triangle,
// which is why the device only ever reports those six values.
//
// Hexagons get a corner to the right, which is a flat top and bottom. That is
// what the openHAB binding draws them as, and a real layout overrides it
// anyway: see Outliner.
func PolygonOf(shapeType int, side float64) Polygon {
	switch shapeType {
	case nanoleaf.ShapeLightPanel, nanoleaf.ShapeTriangle, nanoleaf.ShapeMiniTriangle:
		return Polygon{Sides: 3, Radius: side / math.Sqrt(3), Base: 90}

	case nanoleaf.ShapeSquare, nanoleaf.ShapeSquareMaster, nanoleaf.ShapeSquarePassive,
		nanoleaf.ShapeSkylight, nanoleaf.ShapeSkylightPrimary, nanoleaf.ShapeSkylightPassive:
		return Polygon{Sides: 4, Radius: side / math.Sqrt(2), Base: 45}

	case nanoleaf.ShapeHexagon, nanoleaf.ShapeElementsHexagon:
		return Polygon{Sides: 6, Radius: side, Base: 0}

	case nanoleaf.ShapeElementsCorner:
		// One Elements hexagon reports six of these, one per corner,
		// and the device gives each its own position. Drawing each as
		// a hexagon of half the piece's side puts six of them around
		// the piece touching each other, which is the closest this can
		// get to the real thing without a device to look at.
		return Polygon{Sides: 6, Radius: side / 2, Base: 0}

	case nanoleaf.ShapeLines, nanoleaf.ShapeLinesSingleZone, nanoleaf.ShapeLightstrip4D:
		return Polygon{Sides: 4, Radius: side / 2, Base: 0, Thickness: side * barThickness}

	default:
		return Polygon{Sides: unknownSides, Radius: side / 2, Base: 0}
	}
}

// WithBase returns the polygon turned so its first corner sits at the given
// angle.
func (p Polygon) WithBase(base float64) Polygon {
	p.Base = base
	return p
}

// step is the angle from one corner to the next.
func (p Polygon) step() float64 { return 360 / float64(p.Sides) }

// Side is the length of one edge.
func (p Polygon) Side() float64 {
	return 2 * p.Radius * math.Sin(math.Pi/float64(p.Sides))
}

// Apothem is the distance from the centroid to the middle of an edge.
func (p Polygon) Apothem() float64 {
	return p.Radius * math.Cos(math.Pi/float64(p.Sides))
}

// Outline returns the panel's corners, counter-clockwise from Base.
func (p Polygon) Outline(cx, cy, orientation float64) []Corner {
	if !p.Regular() {
		return p.bar(cx, cy, orientation)
	}
	out := make([]Corner, p.Sides)
	for i := range out {
		rad := radians(p.Base + orientation + float64(i)*p.step())
		out[i] = Corner{X: cx + p.Radius*math.Cos(rad), Y: cy + p.Radius*math.Sin(rad)}
	}
	return out
}

// bar returns the four corners of a bar, lying along its own orientation.
func (p Polygon) bar(cx, cy, orientation float64) []Corner {
	rad := radians(p.Base + orientation)
	sin, cos := math.Sin(rad), math.Cos(rad)
	half := p.Thickness / 2

	out := make([]Corner, 0, 4)
	for _, along := range [][2]float64{{-p.Radius, -half}, {p.Radius, -half}, {p.Radius, half}, {-p.Radius, half}} {
		out = append(out, Corner{
			X: cx + along[0]*cos - along[1]*sin,
			Y: cy + along[0]*sin + along[1]*cos,
		})
	}
	return out
}

// EdgeAngle is the direction from the centroid to the middle of an edge, in
// degrees. Edge i runs from corner i to corner i+1, so it faces halfway
// between them.
//
// It is also the outward normal of that edge, because a regular polygon's
// centroid is equidistant from every edge.
func (p Polygon) EdgeAngle(orientation float64, edge int) float64 {
	return p.Base + orientation + (float64(edge)+0.5)*p.step()
}

// EdgeMidpoint is the middle of an edge.
func (p Polygon) EdgeMidpoint(cx, cy, orientation float64, edge int) Corner {
	rad := radians(p.EdgeAngle(orientation, edge))
	a := p.Apothem()
	return Corner{X: cx + a*math.Cos(rad), Y: cy + a*math.Sin(rad)}
}

func radians(degrees float64) float64 { return degrees * math.Pi / 180 }

// Outliner draws the panels of one mounted wall.
//
// It holds what a single panel cannot say for itself: how big it is, since a
// device reports one side length for a layout that may hold two sizes, and
// which way its corners face, since hexagons can be mounted either way up.
type Outliner struct {
	// reported is the layout's own side length, used only for a shape
	// with no published one.
	reported float64

	// hexBase is the corner angle for hexagons, inferred from the layout.
	hexBase float64
}

// NewOutliner works out the fixed part of a wall's drawing.
func NewOutliner(w Wall) Outliner {
	side := float64(w.SideLength)
	if side <= 0 {
		// A device that reports no side length still has to be drawn,
		// and since firmware 5.0.0 that field is deprecated, so this
		// is the normal case for a new device rather than a fault. The
		// value only sets the scale of a shape nothing else describes,
		// and a page scales to fit whatever comes out.
		side = 100
	}
	return Outliner{reported: side, hexBase: hexBase(w.Panels)}
}

// Side is the side length a panel of this shape is drawn at.
//
// The published length for the shape, when there is one. A device reports a
// single length for a whole layout and that cannot describe a wall holding
// two sizes at once -- a Shapes triangle is 134 and a hexagon 67 -- which is
// why Nanoleaf deprecated the field. It is still the fallback for a shape
// this version has never heard of.
func (o Outliner) Side(shapeType int) float64 {
	if side, ok := SideOf(shapeType); ok {
		return side
	}
	return o.reported
}

// Polygon is the outline of a panel of this shape on this wall.
func (o Outliner) Polygon(shapeType int) Polygon {
	p := PolygonOf(shapeType, o.Side(shapeType))
	if shapeType == nanoleaf.ShapeHexagon || shapeType == nanoleaf.ShapeElementsHexagon {
		p = p.WithBase(o.hexBase)
	}
	return p
}

// Outline returns a panel's corners in wall coordinates, Y up.
func (o Outliner) Outline(wp WallPanel) []Corner {
	return o.Polygon(wp.Panel.ShapeType).Outline(wp.X, wp.Y, wp.Orientation)
}

// hexBase infers which way a hexagon's corners face, as an angle relative to
// the panel's own reported orientation.
//
// Hexagons meet edge to edge, so the direction from one to its nearest
// neighbour points at the middle of the edge they share, and a corner sits 30
// degrees away from that. Inferring it beats picking a convention: this
// project has no hexagons to check a guess against, and drawing them a
// twelfth of a turn out would make a honeycomb look like a pile of
// overlapping bricks.
//
// A layout with fewer than two hexagons has nothing to infer from and keeps
// the fallback angle.
func hexBase(panels []WallPanel) float64 {
	hexes := make([]WallPanel, 0, len(panels))
	for _, wp := range panels {
		if wp.Panel.ShapeType == nanoleaf.ShapeHexagon || wp.Panel.ShapeType == nanoleaf.ShapeElementsHexagon {
			hexes = append(hexes, wp)
		}
	}
	fallback := PolygonOf(nanoleaf.ShapeHexagon, 1).Base
	if len(hexes) < 2 {
		return fallback
	}

	// Device coordinates, not wall ones: the angle is returned relative to
	// the panel's own orientation, and the caller adds the rotation of the
	// whole arrangement back on. Measuring it after rotation would count
	// that rotation twice.
	first := hexes[0].Panel
	best := math.Inf(1)
	var bx, by float64
	for _, other := range hexes[1:] {
		dx := float64(other.Panel.X - first.X)
		dy := float64(other.Panel.Y - first.Y)
		if d := math.Hypot(dx, dy); d > 0 && d < best {
			best, bx, by = d, dx, dy
		}
	}
	if math.IsInf(best, 1) {
		return fallback
	}

	const cornerFromEdge = 30
	toNeighbour := math.Atan2(by, bx) * 180 / math.Pi
	return toNeighbour - float64(first.Orientation) + cornerFromEdge
}
