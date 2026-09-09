// Package webui serves the local page that turns the panel shape until it
// matches the wall.
//
// The page exists because orienting a wall of panels is a visual job. The
// device knows where its panels are relative to each other, but nothing tells
// it which way is up in the room, and the first two attempts at asking the
// user both failed: naming an angle in degrees is not something a person can
// do, and a terminal dial can only draw the shape as coloured blocks. A
// browser can draw the real triangles, and a mouse can turn them.
//
// Everything the page draws is computed here and sent to it. The browser does
// no geometry of its own beyond reading the mouse, so the picture on the
// screen and the picture on the wall come from one calculation and cannot
// disagree.
package webui

import (
	"math"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// point is a corner of a drawn panel, in wall coordinates with Y up.
type point struct{ X, Y float64 }

// shape is how to draw one kind of panel: how many corners it has, how far
// they sit from the centroid as a multiple of the side length, and the angle
// of the first corner before the panel's own rotation is added.
type shape struct {
	corners int
	radius  float64
	base    float64
}

// unknownCorners is how many corners an unrecognised shape is drawn with.
// Enough to read as a circle, which is the honest picture: the panel is
// there, and this version does not know its outline.
const unknownCorners = 16

// Corner angles are measured counter-clockwise from the positive X axis, and
// the panel's own reported orientation is added to the base angle.
//
// The triangle base of 90 degrees was verified against a real NL42 rather
// than assumed. On that device the panels reporting o=0 point up and the ones
// reporting o=180 point down, and their centroids are offset by half a side
// horizontally and one inradius vertically -- exactly a triangular tiling of
// the reported side length. Because a triangle is unchanged by a third of a
// turn, o values of 0, 120 and 240 all draw the same up-pointing triangle,
// which is why the whole device only ever reports those six values.
func shapeOf(t int) shape {
	switch t {
	case nanoleaf.ShapeLightPanel, nanoleaf.ShapeTriangle, nanoleaf.ShapeMiniTriangle:
		return shape{corners: 3, radius: 1 / math.Sqrt(3), base: 90}
	case nanoleaf.ShapeSquare, nanoleaf.ShapeSquareMaster, nanoleaf.ShapeSquarePassive:
		return shape{corners: 4, radius: 1 / math.Sqrt(2), base: 45}
	case nanoleaf.ShapeHexagon:
		// Replaced by the inferred angle when the layout has more than
		// one hexagon; a corner at the top is the fallback.
		return shape{corners: 6, radius: 1, base: 30}
	default:
		return shape{corners: unknownCorners, radius: 0.5, base: 0}
	}
}

// drawing is the part of the picture that does not change when the shape is
// turned: how big each panel is and which way its corners face.
type drawing struct {
	side float64

	// miniHalved says whether mini triangles are drawn at half the
	// reported side length.
	miniHalved bool

	// hexBase is the corner angle for hexagons, relative to a panel's own
	// orientation.
	hexBase float64
}

// newDrawing works out the fixed part of the picture from a layout.
func newDrawing(w render.Wall) drawing {
	var hasTriangle, hasMini bool
	for _, wp := range w.Panels {
		switch wp.Panel.ShapeType {
		case nanoleaf.ShapeTriangle:
			hasTriangle = true
		case nanoleaf.ShapeMiniTriangle:
			hasMini = true
		}
	}

	side := float64(w.SideLength)
	if side <= 0 {
		// A device that reports no side length still has to be drawn.
		// The value only sets the scale of the picture, and the page
		// scales to fit whatever comes out.
		side = 100
	}
	return drawing{
		side:       side,
		miniHalved: hasTriangle && hasMini,
		hexBase:    hexBase(w.Panels),
	}
}

// sideOf is the side length to draw a panel with.
//
// The device reports one side length for a whole layout, but a set can mix
// full triangles with mini ones, whose side is half. The halving is applied
// only when both are present: in a mini-only set the reported length already
// is the mini one, and halving it would draw a tiling full of gaps.
func (d drawing) sideOf(shapeType int) float64 {
	if shapeType == nanoleaf.ShapeMiniTriangle && d.miniHalved {
		return d.side / 2
	}
	return d.side
}

// outline returns a panel's corners in wall coordinates, Y up.
func (d drawing) outline(wp render.WallPanel) []point {
	s := shapeOf(wp.Panel.ShapeType)
	if wp.Panel.ShapeType == nanoleaf.ShapeHexagon {
		s.base = d.hexBase
	}
	radius := s.radius * d.sideOf(wp.Panel.ShapeType)

	pts := make([]point, s.corners)
	step := 360.0 / float64(s.corners)
	for i := range pts {
		deg := s.base + wp.Orientation + float64(i)*step
		rad := deg * math.Pi / 180
		pts[i] = point{
			X: wp.X + radius*math.Cos(rad),
			Y: wp.Y + radius*math.Sin(rad),
		}
	}
	return pts
}

// hexBase infers which way a hexagon's corners face, as an angle relative to
// the panel's own reported orientation.
//
// Hexagons meet edge to edge, so the direction from one to its nearest
// neighbour points at the middle of the edge they share, and a corner sits 30
// degrees away from that. Inferring it beats picking a convention: this
// project has no hexagons to check a guess against, and drawing them a
// sixteenth of a turn out would make a honeycomb look like a pile of
// overlapping bricks.
//
// A layout with fewer than two hexagons has nothing to infer from and keeps
// the fallback angle.
func hexBase(panels []render.WallPanel) float64 {
	hexes := make([]render.WallPanel, 0, len(panels))
	for _, wp := range panels {
		if wp.Panel.ShapeType == nanoleaf.ShapeHexagon {
			hexes = append(hexes, wp)
		}
	}
	fallback := shapeOf(nanoleaf.ShapeHexagon).base
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
