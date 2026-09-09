// Package shapes holds arrangements someone might actually have on a wall.
//
// It exists because a display that only looks right on one wall is not
// finished. The panels this was written against are nine triangles in a
// diagonal zigzag, and the drawing, the bands and the long axis all had to be
// checked against hexagons in a honeycomb, squares in a grid, a straight row,
// and a set that mixes two sizes of triangle. None of that needs a device:
// a layout is a list of centroids, and the samples below are built from the
// same tilings the real products click together in.
//
// They are also what a person with no panels yet can look at, and what the
// tests measure the outlines against.
package shapes

import (
	"fmt"
	"math"
	"strings"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Sample is one arrangement, named.
type Sample struct {
	// Name is what a flag or a page asks for.
	Name string
	// Label says what it is, in a few words.
	Label string
	// Layout is the arrangement, exactly as a device would report it.
	Layout nanoleaf.Layout
}

// Panels is how many panels the sample has, including any with no LEDs.
func (s Sample) Panels() int { return len(s.Layout.Panels) }

// Directions used to describe the walls below, in degrees counter-clockwise
// from the right. Named because "AttachToward(last, right, ...)" says what a
// person building a wall would say, and 0 does not.
const (
	right = 0.0
	up    = 90.0
	left  = 180.0
)

// All returns every sample, in the order they are offered.
//
// The first is the arrangement this program was written against, so it is
// also the one a page opens on.
func All() []Sample {
	return []Sample{
		{
			Name:   "triangles-zigzag",
			Label:  "9 Shapes triangles in a diagonal zigzag",
			Layout: zigzag(),
		},
		{
			Name:   "triangles-row",
			Label:  "7 Shapes triangles in a straight row",
			Layout: row(nanoleaf.ShapeTriangle, 7),
		},
		{
			Name:   "triangles-wall",
			Label:  "16 Shapes triangles in a block, four rows deep",
			Layout: block(nanoleaf.ShapeTriangle, 4, 4),
		},
		{
			Name:   "mini-triangles",
			Label:  "12 Shapes mini triangles in two rows",
			Layout: block(nanoleaf.ShapeMiniTriangle, 6, 2),
		},
		{
			Name:   "mixed-triangles",
			Label:  "Shapes triangles with mini triangles under them",
			Layout: mixed(),
		},
		{
			Name:   "hexagons-honeycomb",
			Label:  "7 Shapes hexagons in a honeycomb",
			Layout: honeycomb(nanoleaf.ShapeHexagon),
		},
		{
			Name:   "hexagons-column",
			Label:  "5 Shapes hexagons in a column",
			Layout: hexColumn(),
		},
		{
			Name:   "hexagons-and-minis",
			Label:  "Shapes hexagons ringed with mini triangles",
			Layout: hexFlower(),
		},
		{
			Name:   "elements-hexagons",
			Label:  "7 Elements hexagons, which are twice the size",
			Layout: honeycomb(nanoleaf.ShapeElementsHexagon),
		},
		{
			Name:   "elements-corners",
			Label:  "3 Elements hexagons that light six corners each",
			Layout: elementsCorners(),
		},
		{
			Name:   "squares-grid",
			Label:  "9 Canvas squares in a 3 by 3 grid",
			Layout: canvas(),
		},
		{
			Name:   "light-panels",
			Label:  "9 original Light Panels with a Rhythm module",
			Layout: aurora(),
		},
		{
			Name:   "lines",
			Label:  "5 Lines bars in a zigzag, with their connectors",
			Layout: lines(),
		},
		{
			Name:   "lightstrip-4d",
			Label:  "the 4D lightstrip around a screen",
			Layout: lightstrip(),
		},
		{
			Name:   "skylight",
			Label:  "6 Skylight panels on a ceiling",
			Layout: skylight(),
		},
	}
}

// Named finds a sample by name.
func Named(name string) (Sample, error) {
	for _, s := range All() {
		if s.Name == name {
			return s, nil
		}
	}
	return Sample{}, fmt.Errorf("no sample shape called %q (%s)", name, strings.Join(Names(), ", "))
}

// Names lists the sample names, for a flag's help and for an error.
func Names() []string {
	out := make([]string, 0, len(All()))
	for _, s := range All() {
		out = append(out, s.Name)
	}
	return out
}

// Default is the sample a page or a preview opens on.
func Default() Sample { return All()[0] }

// -- the walls that tile -----------------------------------------------------

// zigzag is the arrangement this program was written against, reported the
// way its own device reports it: mounted at a global orientation of 302, with
// the controller brick as a tenth panel that has no LEDs.
//
// Written out rather than built, because it is a measurement. It is the
// fixture the sign of the global orientation and the triangle corner angles
// were both settled against, and a version of it that came out of the builder
// would only agree with the builder.
func zigzag() nanoleaf.Layout {
	layout := nanoleaf.Layout{
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
			// The controller reports itself as a panel and has no LEDs.
			{ID: 0, X: 0, Y: 40, Orientation: 180, ShapeType: nanoleaf.ShapeController},
		},
	}
	layout.NumPanels = len(layout.Panels)
	return layout
}

// row sticks panels side by side, going right.
func row(shapeType, count int) nanoleaf.Layout {
	b := Start(shapeType)
	last := b.First()
	for range count - 1 {
		last = b.AttachToward(last, right, shapeType)
	}
	return mustLayout(b)
}

// block fills a wall row by row, each row running back the way the last one
// came.
//
// Alternating the direction is not decoration: a row of triangles ends on an
// inverted one, and an inverted triangle is the only one with an edge facing
// up, so the row above starts from there.
func block(shapeType, across, rows int) nanoleaf.Layout {
	b := Start(shapeType)

	last := b.First()
	toward := right
	for r := range rows {
		if r > 0 {
			last = b.AttachToward(last, up, shapeType)
			toward = left + (right - toward)
		}
		for range across - 1 {
			last = b.AttachToward(last, toward, shapeType)
		}
	}
	return mustLayout(b)
}

// honeycomb is a hexagon with the six that fit around it, which is how a
// starter kit is usually mounted. Both sizes of hexagon go together the same
// way; only the scale differs.
func honeycomb(shapeType int) nanoleaf.Layout {
	b := Start(shapeType)
	for edge := range 6 {
		b.Attach(b.First(), edge, shapeType)
	}
	return mustLayout(b)
}

// hexColumn climbs a wall in hexagons. A hexagon with a flat top has an edge
// facing straight up, so a column of them is straight.
func hexColumn() nanoleaf.Layout {
	b := Start(nanoleaf.ShapeHexagon)
	last := b.First()
	for range 4 {
		last = b.AttachToward(last, up, nanoleaf.ShapeHexagon)
	}
	return mustLayout(b)
}

// hexFlower mixes the two Shapes that share an edge length.
//
// A hexagon's edge is 67 and a mini triangle's is 67, so those two go
// together; a full triangle's is 134, which is why two minis fit along one of
// its edges and a hexagon does not sit against it at all. Getting that wrong
// is easy: an Elements hexagon happens to be 134, so a wall of hexagons and
// full triangles looks plausible and cannot be built.
func hexFlower() nanoleaf.Layout {
	b := Start(nanoleaf.ShapeHexagon)
	for edge := range 6 {
		shapeType := nanoleaf.ShapeMiniTriangle
		if edge%2 == 0 {
			shapeType = nanoleaf.ShapeHexagon
		}
		b.Attach(b.First(), edge, shapeType)
	}
	return mustLayout(b)
}

// mixed puts mini triangles under a row of full ones. Two minis fit along one
// full edge, which is how the two sizes are sold to be combined.
func mixed() nanoleaf.Layout {
	b := Start(nanoleaf.ShapeTriangle)

	full := make([]int, 1, 4)
	full[0] = b.First()
	for range 3 {
		full = append(full, b.AttachToward(full[len(full)-1], right, nanoleaf.ShapeTriangle))
	}

	// The upward triangles are the ones with a downward edge, and they are
	// every other panel in a row.
	const down = 270.0
	for i := 0; i < len(full); i += 2 {
		first := b.AttachHalfToward(full[i], down, nanoleaf.ShapeMiniTriangle, 0)
		b.AttachHalfToward(full[i], down, nanoleaf.ShapeMiniTriangle, 1)

		// Two minis on the halves of an edge leave an inverted mini's
		// worth of gap between them, and a third fills it. Without it
		// the two would meet at a corner and nowhere else, which is a
		// legal wall but a strange one to hold up as an example.
		//
		// It hangs off the first mini's free edge, a sixth of a turn
		// round from the edge they share.
		const roundFromShared = 330.0
		b.AttachToward(first, roundFromShared, nanoleaf.ShapeMiniTriangle)
	}
	return mustLayout(b)
}

// canvas is a grid of Canvas squares, one of which holds the controller and
// one of which is in passive mode. A real Canvas reports both.
func canvas() nanoleaf.Layout {
	layout := block(nanoleaf.ShapeSquare, 3, 3)
	layout.Panels[0].ShapeType = nanoleaf.ShapeSquareMaster
	layout.Panels[len(layout.Panels)-1].ShapeType = nanoleaf.ShapeSquarePassive
	return layout
}

// aurora is the original Light Panels, whose triangles are larger than the
// Shapes ones, with the Rhythm module that clips onto them and has no LEDs.
func aurora() nanoleaf.Layout {
	b := Start(nanoleaf.ShapeLightPanel)

	// Five along the bottom, then back along the top. The second row
	// hangs off the fourth panel rather than the fifth, because only an
	// inverted triangle has an edge facing up, and they are every other
	// one.
	bottom := make([]int, 1, 5)
	bottom[0] = b.First()
	for range 4 {
		bottom = append(bottom, b.AttachToward(bottom[len(bottom)-1], right, nanoleaf.ShapeLightPanel))
	}

	last := b.AttachToward(bottom[3], up, nanoleaf.ShapeLightPanel)
	for range 3 {
		last = b.AttachToward(last, left, nanoleaf.ShapeLightPanel)
	}

	// The Rhythm module hangs off the side of the arrangement.
	b.Blank(bottom[0], nanoleaf.ShapeRhythm, -180, -60)
	return mustLayout(b)
}

// -- the walls that do not tile ----------------------------------------------

// elementsCorners is the Elements hexagon that lights its corners.
//
// One physical piece reports six panels, one per corner, each with its own
// position and its own colour. Nothing here can be built with edges, since
// the six are parts of one hexagon rather than six hexagons, so they are
// placed where the piece puts them: around its centre, a side length out.
func elementsCorners() nanoleaf.Layout {
	side, _ := render.SideOf(nanoleaf.ShapeElementsCorner)

	// Three pieces in a row, far enough apart to sit edge to edge as
	// hexagons of that side length.
	const pieces, corners = 3, 6
	panels := make([]nanoleaf.Panel, 0, pieces*corners)
	for piece := range pieces {
		cx := float64(piece) * side * math.Sqrt(3)
		for corner := range corners {
			angle := radians(float64(corner) * 60)
			panels = append(panels, nanoleaf.Panel{
				X:         int(math.Round(cx + side*math.Cos(angle))),
				Y:         int(math.Round(side * math.Sin(angle))),
				ShapeType: nanoleaf.ShapeElementsCorner,
			})
		}
	}
	return handmade(int(side), panels)
}

// lines walks a path of Lines bars, turning at each connector.
//
// Lines do not tile. Each bar is a light joined to the next at a connector
// that has no LEDs of its own, and the angles the connectors allow are what
// gives a Lines figure its look. The power goes in at one end and the
// controller caps the other, and a device reports both of those as panels
// with no LEDs.
func lines() nanoleaf.Layout {
	side, _ := render.SideOf(nanoleaf.ShapeLines)
	turns := []float64{0, 60, -120, 60, -60}

	// A bar and a joint for each turn, plus the joint the power goes into.
	panels := make([]nanoleaf.Panel, 0, 1+2*len(turns))
	x, y, heading := 0.0, 0.0, 0.0

	panels = append(panels, nanoleaf.Panel{X: 0, Y: 0, ShapeType: nanoleaf.ShapePowerConnector})
	for i, turn := range turns {
		heading += turn
		rad := radians(heading)

		// A bar's position is its middle, half a length along.
		panels = append(panels, nanoleaf.Panel{
			X:           int(math.Round(x + side/2*math.Cos(rad))),
			Y:           int(math.Round(y + side/2*math.Sin(rad))),
			Orientation: int(math.Round(heading)),
			ShapeType:   nanoleaf.ShapeLines,
		})

		x += side * math.Cos(rad)
		y += side * math.Sin(rad)

		joint := nanoleaf.ShapeLinesConnector
		if i == len(turns)-1 {
			joint = nanoleaf.ShapeControllerCap
		}
		panels = append(panels, nanoleaf.Panel{
			X:         int(math.Round(x)),
			Y:         int(math.Round(y)),
			ShapeType: joint,
		})
	}
	return handmade(int(side), panels)
}

// lightstrip is the 4D strip, which reports a panel per segment.
//
// It goes round the back of a screen, so the sample is a rectangle: the
// segments run along each side and turn the corner.
func lightstrip() nanoleaf.Layout {
	side, _ := render.SideOf(nanoleaf.ShapeLightstrip4D)
	across, down := 8, 4

	panels := make([]nanoleaf.Panel, 0, 2*(across+down))
	x, y, heading := 0.0, 0.0, 0.0
	for _, run := range []int{across, down, across, down} {
		rad := radians(heading)
		for range run {
			panels = append(panels, nanoleaf.Panel{
				X:           int(math.Round(x + side/2*math.Cos(rad))),
				Y:           int(math.Round(y + side/2*math.Sin(rad))),
				Orientation: int(math.Round(heading)),
				ShapeType:   nanoleaf.ShapeLightstrip4D,
			})
			x += side * math.Cos(rad)
			y += side * math.Sin(rad)
		}
		heading += 90
	}
	return handmade(int(side), panels)
}

// skylight is a ceiling of Skylight panels, which are squares twice the size
// of a Canvas one. One holds the controller and one is in passive mode.
func skylight() nanoleaf.Layout {
	layout := block(nanoleaf.ShapeSkylight, 3, 2)
	layout.Panels[0].ShapeType = nanoleaf.ShapeSkylightPrimary
	layout.Panels[len(layout.Panels)-1].ShapeType = nanoleaf.ShapeSkylightPassive
	return layout
}

// handmade finishes a layout that was walked out rather than built, giving
// every panel an identity.
func handmade(side int, panels []nanoleaf.Panel) nanoleaf.Layout {
	for i := range panels {
		panels[i].ID = firstID + i
	}
	return nanoleaf.Layout{NumPanels: len(panels), SideLength: side, Panels: panels}
}
