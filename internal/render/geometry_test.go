package render

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// TestDiagonalArrangementGetsDiagonalAxis is the property the whole design
// rests on: for panels mounted along a diagonal, the pulse axis must follow
// the diagonal rather than defaulting to horizontal or vertical. If this
// breaks, the wave stops travelling through the shape.
func TestDiagonalArrangementGetsDiagonalAxis(t *testing.T) {
	ids := []int{1, 2, 3, 4}
	xs := []float64{0, 100, 200, 300}
	ys := []float64{0, 100, 200, 300}

	geo := NewGeometry(ids, xs, ys)

	// Along a perfect diagonal, S must increase monotonically and span
	// the full range.
	for i := 1; i < len(geo.Points); i++ {
		if geo.Points[i].S <= geo.Points[i-1].S {
			t.Fatalf("S not increasing along diagonal: %v", pointsS(geo))
		}
	}
	if got := geo.Points[0].S; math.Abs(got) > 1e-9 {
		t.Errorf("first S = %v, want 0", got)
	}
	if got := geo.Points[3].S; math.Abs(got-1) > 1e-9 {
		t.Errorf("last S = %v, want 1", got)
	}
}

// TestVerticalOrientation pins the vertical axis: V must be 0 at the bottom
// of the wall and 1 at the top, or the budget fill drains downwards.
func TestVerticalOrientation(t *testing.T) {
	geo := NewGeometry([]int{1, 2}, []float64{0, 0}, []float64{0, 500})

	if geo.Points[0].V != 0 {
		t.Errorf("bottom panel V = %v, want 0", geo.Points[0].V)
	}
	if geo.Points[1].V != 1 {
		t.Errorf("top panel V = %v, want 1", geo.Points[1].V)
	}
}

// TestAxisAlignedArrangements covers the case where the covariance's
// off-diagonal term vanishes and the eigenvector formula degenerates.
func TestAxisAlignedArrangements(t *testing.T) {
	tests := []struct {
		name   string
		xs, ys []float64
	}{
		{"vertical column", []float64{0, 0, 0}, []float64{0, 100, 200}},
		{"horizontal row", []float64{0, 100, 200}, []float64{0, 0, 0}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			geo := NewGeometry([]int{1, 2, 3}, tc.xs, tc.ys)
			for i := 1; i < len(geo.Points); i++ {
				if geo.Points[i].S <= geo.Points[i-1].S {
					t.Fatalf("S not increasing: %v", pointsS(geo))
				}
			}
		})
	}
}

// TestDegenerateArrangements checks the renderer survives inputs that have no
// meaningful axis, instead of dividing by zero or emitting NaN colours.
func TestDegenerateArrangements(t *testing.T) {
	tests := []struct {
		name   string
		ids    []int
		xs, ys []float64
	}{
		{"empty", nil, nil, nil},
		{"single panel", []int{1}, []float64{50}, []float64{50}},
		{"all stacked", []int{1, 2}, []float64{7, 7}, []float64{7, 7}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			geo := NewGeometry(tc.ids, tc.xs, tc.ys)
			if len(geo.Points) != len(tc.ids) {
				t.Fatalf("got %d points, want %d", len(geo.Points), len(tc.ids))
			}
			for _, p := range geo.Points {
				for _, v := range []float64{p.U, p.V, p.S} {
					if math.IsNaN(v) || math.IsInf(v, 0) {
						t.Fatalf("non-finite coordinate in %+v", p)
					}
				}
			}
		})
	}
}

func pointsS(g Geometry) []float64 {
	out := make([]float64, len(g.Points))
	for i, p := range g.Points {
		out[i] = p.S
	}
	return out
}

// TestFromLayoutUndoesGlobalOrientation pins the sign of the global
// orientation, which is the one thing in this package that cannot be checked
// by reading the code.
//
// The numbers come from a real NL42: nine triangles mounted in a diagonal
// zigzag, reported at globalOrientation 302. On that wall the two panels the
// device places lowest are physically the highest, so the orientation has to
// be undone. Adding it instead left the vertical axis 116 degrees out.
func TestFromLayoutUndoesGlobalOrientation(t *testing.T) {
	layout := nanoleaf.Layout{
		NumPanels:         10,
		SideLength:        134,
		GlobalOrientation: 302,
		Panels: []nanoleaf.Panel{
			{ID: 53940, X: 34, Y: 89, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 38513, X: 101, Y: 127, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 22926, X: 34, Y: 205, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 38260, X: 101, Y: 243, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 51757, X: 168, Y: 205, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 65173, X: 235, Y: 243, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 51695, X: 167, Y: 89, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 58908, X: 234, Y: 127, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 17522, X: 301, Y: 89, ShapeType: nanoleaf.ShapeTriangle},
			// The controller reports itself as a panel but has no LEDs.
			{ID: 1, X: 0, Y: 0, ShapeType: nanoleaf.ShapeController},
		},
	}

	geo, skipped := FromLayout(layout, 0)
	if len(skipped) != 0 {
		t.Errorf("skipped %d panels, want none", len(skipped))
	}
	if len(geo.Points) != 9 {
		t.Fatalf("rendered %d panels, want 9 (the controller must be dropped)", len(geo.Points))
	}

	v := map[int]float64{}
	for _, p := range geo.Points {
		v[p.PanelID] = p.V
	}

	// Wall-confirmed groups, from a calibration run photographed in place:
	// these two panels are physically high on the wall, those two are low.
	// Deliberately not every panel -- the ones near the middle prove
	// nothing, and pinning them would just encode measurement noise.
	high := []int{17522, 58908}
	low := []int{53940, 22926}

	for _, id := range high {
		for _, other := range low {
			if v[id] <= v[other] {
				t.Errorf("panel %d (physically high, V=%.2f) is not above panel %d (physically low, V=%.2f)",
					id, v[id], other, v[other])
			}
		}
	}

	// The separation should be decisive, not marginal: with the sign
	// inverted these two groups swap ends entirely.
	if v[17522] < 0.6 || v[53940] > 0.4 {
		t.Errorf("vertical axis is not aligned with the wall: high panel V=%.2f, low panel V=%.2f",
			v[17522], v[53940])
	}
}

// TestFromLayoutExtraRotation checks the override composes with the device's
// own orientation, for a wall where the two disagree.
func TestFromLayoutExtraRotation(t *testing.T) {
	layout := nanoleaf.Layout{
		GlobalOrientation: 0,
		Panels: []nanoleaf.Panel{
			{ID: 1, X: 0, Y: 0, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 2, X: 0, Y: 100, ShapeType: nanoleaf.ShapeTriangle},
		},
	}

	// Unrotated, panel 2 is above panel 1.
	geo, _ := FromLayout(layout, 0)
	if geo.Points[0].V >= geo.Points[1].V {
		t.Fatalf("unrotated: V = %.2f, %.2f; want the second higher",
			geo.Points[0].V, geo.Points[1].V)
	}

	// Turned upside down, it must be below.
	geo, _ = FromLayout(layout, 180)
	if geo.Points[0].V <= geo.Points[1].V {
		t.Errorf("rotated 180: V = %.2f, %.2f; want the second lower",
			geo.Points[0].V, geo.Points[1].V)
	}
}
