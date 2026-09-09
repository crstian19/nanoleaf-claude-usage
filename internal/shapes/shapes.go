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
	"strings"

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

// Side lengths the real products use, in the device's own units.
const (
	// shapesSide is a Shapes triangle or hexagon edge.
	shapesSide = 134
	// miniSide is a Shapes mini triangle edge, half the full one.
	miniSide = shapesSide / 2
	// canvasSide is a Canvas square edge.
	canvasSide = 100
	// auroraSide is an original Light Panels triangle edge.
	auroraSide = 150
)

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
			Label:  "9 triangles in a diagonal zigzag",
			Layout: zigzag(),
		},
		{
			Name:   "triangles-row",
			Label:  "7 triangles in a straight row",
			Layout: row(shapesSide, nanoleaf.ShapeTriangle, 7),
		},
		{
			Name:   "triangles-wall",
			Label:  "16 triangles in a block, four rows deep",
			Layout: block(shapesSide, nanoleaf.ShapeTriangle, 4, 4),
		},
		{
			Name:   "hexagons-honeycomb",
			Label:  "7 hexagons in a honeycomb",
			Layout: honeycomb(),
		},
		{
			Name:   "hexagons-column",
			Label:  "5 hexagons in a zigzagging column",
			Layout: hexColumn(),
		},
		{
			Name:   "hexagons-and-triangles",
			Label:  "a hexagon with hexagons and triangles around it",
			Layout: hexFlower(),
		},
		{
			Name:   "squares-grid",
			Label:  "9 Canvas squares in a 3 by 3 grid",
			Layout: block(canvasSide, nanoleaf.ShapeSquare, 3, 3),
		},
		{
			Name:   "mini-triangles",
			Label:  "12 mini triangles in two rows",
			Layout: block(miniSide, nanoleaf.ShapeMiniTriangle, 6, 2),
		},
		{
			Name:   "mixed-triangles",
			Label:  "triangles with a band of mini triangles under them",
			Layout: mixed(),
		},
		{
			Name:   "light-panels",
			Label:  "9 original Light Panels, the triangles from the Aurora",
			Layout: aurora(),
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

// -- the walls ---------------------------------------------------------------

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
		SideLength:        shapesSide,
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
func row(side, shapeType, count int) nanoleaf.Layout {
	b := Start(side, shapeType)
	last := 0
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
func block(side, shapeType, across, rows int) nanoleaf.Layout {
	b := Start(side, shapeType)

	last := 0
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

// honeycomb is a hexagon with the six that fit around it, which is how the
// starter kit is usually mounted.
func honeycomb() nanoleaf.Layout {
	b := Start(shapesSide, nanoleaf.ShapeHexagon)
	for edge := range 6 {
		b.Attach(0, edge, nanoleaf.ShapeHexagon)
	}
	return mustLayout(b)
}

// hexColumn climbs a wall in hexagons. Pointy-topped hexagons have no upward
// edge, so a column of them zigzags: that is what the tiling allows, and
// pretending otherwise would draw a stack that does not touch.
func hexColumn() nanoleaf.Layout {
	b := Start(shapesSide, nanoleaf.ShapeHexagon)
	last := 0
	for i := range 4 {
		toward := 60.0
		if i%2 == 1 {
			toward = 120
		}
		last = b.AttachToward(last, toward, nanoleaf.ShapeHexagon)
	}
	return mustLayout(b)
}

// hexFlower mixes the two Shapes that share an edge length. Nanoleaf sells
// them to be combined, and a wall that mixes them is the case where a panel
// meets a neighbour of a different kind.
func hexFlower() nanoleaf.Layout {
	b := Start(shapesSide, nanoleaf.ShapeHexagon)
	for edge := range 6 {
		shapeType := nanoleaf.ShapeTriangle
		if edge%2 == 0 {
			shapeType = nanoleaf.ShapeHexagon
		}
		b.Attach(0, edge, shapeType)
	}
	return mustLayout(b)
}

// mixed puts mini triangles under a row of full ones. Two minis fit along one
// full edge, which is how the two sizes are sold to be combined.
func mixed() nanoleaf.Layout {
	b := Start(shapesSide, nanoleaf.ShapeTriangle)

	full := make([]int, 1, 4)
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

// aurora is the original Light Panels, whose triangles are larger than the
// Shapes ones. Included because they are the model this package cannot drive
// -- they speak an older streaming protocol -- and the shape still has to be
// drawn for anyone who has them.
func aurora() nanoleaf.Layout {
	b := Start(auroraSide, nanoleaf.ShapeLightPanel)

	// Five along the bottom, then back along the top. The second row
	// hangs off the fourth panel rather than the fifth, because only an
	// inverted triangle has an edge facing up, and they are every other
	// one.
	bottom := make([]int, 1, 5)
	for range 4 {
		bottom = append(bottom, b.AttachToward(bottom[len(bottom)-1], right, nanoleaf.ShapeLightPanel))
	}

	last := b.AttachToward(bottom[3], up, nanoleaf.ShapeLightPanel)
	for range 3 {
		last = b.AttachToward(last, left, nanoleaf.ShapeLightPanel)
	}
	return mustLayout(b)
}
