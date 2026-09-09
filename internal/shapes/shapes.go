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
	"sort"
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
			Layout: triangleRow(),
		},
		{
			Name:   "triangles-wall",
			Label:  "16 triangles in a block, four rows deep",
			Layout: triangleWall(),
		},
		{
			Name:   "hexagons-honeycomb",
			Label:  "7 hexagons in a honeycomb",
			Layout: honeycomb(),
		},
		{
			Name:   "hexagons-column",
			Label:  "5 hexagons in a vertical column",
			Layout: hexColumn(),
		},
		{
			Name:   "squares-grid",
			Label:  "9 Canvas squares in a 3 by 3 grid",
			Layout: squareGrid(3, 3),
		},
		{
			Name:   "mini-triangles",
			Label:  "12 mini triangles in two rows",
			Layout: miniRows(),
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

// -- the tilings -------------------------------------------------------------

// triangleCell is a cell of a triangular tiling: a column, a row, and which
// way the triangle points.
//
// Columns are half a side apart, because that is how a triangular row works:
// an upward triangle and the inverted one beside it share an edge, and their
// centroids differ by half a side across and a third of the height up.
type triangleCell struct {
	col, row int
	up       bool
}

// triangles lays cells out on a triangular tiling of the given side length.
func triangles(side int, shape int, cells []triangleCell) []nanoleaf.Panel {
	height := float64(side) * math.Sqrt(3) / 2
	panels := make([]nanoleaf.Panel, 0, len(cells))
	for i, c := range cells {
		// A third of the way up for an upward triangle, two thirds for
		// an inverted one.
		frac := 1.0 / 3
		orientation := 0
		if !c.up {
			frac = 2.0 / 3
			orientation = 180
		}
		panels = append(panels, nanoleaf.Panel{
			ID:          firstID + i,
			X:           int(math.Round(float64(c.col+1) * float64(side) / 2)),
			Y:           int(math.Round(float64(c.row)*height + frac*height)),
			Orientation: orientation,
			ShapeType:   shape,
		})
	}
	return panels
}

// firstID is where sample panel IDs start. Nothing depends on the value; it
// only has to look like the device's own five-digit IDs rather than like an
// index, so nobody reads a sample as a list of cells.
const firstID = 10100

// hexes lays cells out on a hexagonal tiling, in axial coordinates.
//
// The hexagons are pointy-topped, which is how the Shapes hexagons come apart
// and back together: neighbours sit beside each other in a row, and the next
// row is offset by half a step.
func hexes(side int, cells [][2]int) []nanoleaf.Panel {
	width := float64(side) * math.Sqrt(3)
	panels := make([]nanoleaf.Panel, 0, len(cells))
	for i, c := range cells {
		q, r := float64(c[0]), float64(c[1])
		panels = append(panels, nanoleaf.Panel{
			ID:        firstID + i,
			X:         int(math.Round(width * (q + r/2))),
			Y:         int(math.Round(1.5 * float64(side) * r)),
			ShapeType: nanoleaf.ShapeHexagon,
		})
	}
	return panels
}

func zigzag() nanoleaf.Layout {
	// The arrangement this program was written against, reported the way
	// its device reports it: mounted at a global orientation of 302, with
	// the controller brick as a tenth panel that has no LEDs.
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
			{ID: 0, X: 0, Y: 40, Orientation: 180, ShapeType: nanoleaf.ShapeController},
		},
	}
	layout.NumPanels = len(layout.Panels)
	return layout
}

func triangleRow() nanoleaf.Layout {
	cells := make([]triangleCell, 0, 7)
	for col := range 7 {
		cells = append(cells, triangleCell{col: col, up: col%2 == 0})
	}
	return sized(shapesSide, triangles(shapesSide, nanoleaf.ShapeTriangle, cells))
}

func triangleWall() nanoleaf.Layout {
	cells := make([]triangleCell, 0, 16)
	for row := range 4 {
		for col := range 4 {
			cells = append(cells, triangleCell{col: col, row: row, up: col%2 == 0})
		}
	}
	return sized(shapesSide, triangles(shapesSide, nanoleaf.ShapeTriangle, cells))
}

func honeycomb() nanoleaf.Layout {
	// A centre hexagon and the six around it, which is how the starter kit
	// is usually mounted.
	return sized(shapesSide, hexes(shapesSide, [][2]int{
		{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {-1, 1}, {0, -1}, {1, -1},
	}))
}

func hexColumn() nanoleaf.Layout {
	cells := make([][2]int, 0, 5)
	for r := range 5 {
		// Each row of a pointy-topped tiling is offset half a step, so a
		// column that looks straight steps back one every two rows.
		cells = append(cells, [2]int{-r / 2, r})
	}
	return sized(shapesSide, hexes(shapesSide, cells))
}

func squareGrid(cols, rows int) nanoleaf.Layout {
	panels := make([]nanoleaf.Panel, 0, cols*rows)
	for row := range rows {
		for col := range cols {
			shape := nanoleaf.ShapeSquare
			// One square in a Canvas holds the controller.
			if row == 0 && col == 0 {
				shape = nanoleaf.ShapeSquareMaster
			}
			panels = append(panels, nanoleaf.Panel{
				ID:        firstID + len(panels),
				X:         col * canvasSide,
				Y:         row * canvasSide,
				ShapeType: shape,
			})
		}
	}
	return sized(canvasSide, panels)
}

func miniRows() nanoleaf.Layout {
	cells := make([]triangleCell, 0, 12)
	for row := range 2 {
		for col := range 6 {
			cells = append(cells, triangleCell{col: col, row: row, up: col%2 == 0})
		}
	}
	return sized(miniSide, triangles(miniSide, nanoleaf.ShapeMiniTriangle, cells))
}

func mixed() nanoleaf.Layout {
	full := triangles(shapesSide, nanoleaf.ShapeTriangle, []triangleCell{
		{col: 0, row: 1, up: true},
		{col: 1, row: 1, up: false},
		{col: 2, row: 1, up: true},
		{col: 3, row: 1, up: false},
	})

	// The minis sit on their own tiling, half the size, in the row below.
	// Four of them fill the footprint of one full triangle, which is how
	// the two sizes are sold to be combined, so eight of them run the
	// width of the four above.
	miniCells := make([]triangleCell, 0, 8)
	for col := range 8 {
		miniCells = append(miniCells, triangleCell{col: col, row: 1, up: col%2 == 0})
	}
	mini := triangles(miniSide, nanoleaf.ShapeMiniTriangle, miniCells)
	for i := range mini {
		mini[i].ID = firstID + 100 + i
	}

	// The reported side length is the full triangle's, as a real device
	// with both sizes reports it.
	return sized(shapesSide, append(full, mini...))
}

func aurora() nanoleaf.Layout {
	cells := make([]triangleCell, 0, 9)
	for row := range 2 {
		for col := range 5 {
			if row == 1 && col == 4 {
				continue
			}
			cells = append(cells, triangleCell{col: col, row: row, up: col%2 == 0})
		}
	}
	return sized(auroraSide, triangles(auroraSide, nanoleaf.ShapeLightPanel, cells))
}

// sized finishes a layout: the panel count the device would report, and the
// panels sorted by ID so a sample is stable to read.
func sized(side int, panels []nanoleaf.Panel) nanoleaf.Layout {
	sort.Slice(panels, func(i, j int) bool { return panels[i].ID < panels[j].ID })
	return nanoleaf.Layout{
		NumPanels:  len(panels),
		SideLength: side,
		Panels:     panels,
	}
}
