package render

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// TestProjectTurnsPanelsWithTheArrangement covers the field the renderer
// never reads and the drawing depends on entirely: a panel's own rotation has
// to follow the rotation applied to the whole shape, or the outlines would be
// drawn at the angles the panels had before the wall was taken into account.
func TestProjectTurnsPanelsWithTheArrangement(t *testing.T) {
	layout := nanoleaf.Layout{
		SideLength:        134,
		GlobalOrientation: 302,
		Panels: []nanoleaf.Panel{
			{ID: 1, X: 100, Y: 0, Orientation: 0, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 2, X: -100, Y: 0, Orientation: 180, ShapeType: nanoleaf.ShapeTriangle},
		},
	}

	wall := Project(layout, 30)
	// 30 asked for, 302 undone.
	const applied = float64(30 - 302)
	for i, wp := range wall.Panels {
		if want := float64(layout.Panels[i].Orientation) + applied; wp.Orientation != want {
			t.Errorf("panel %d faces %v, want %v", wp.Panel.ID, wp.Orientation, want)
		}
	}

	// Turning the shape cannot change how far apart its panels are.
	gap := math.Hypot(wall.Panels[0].X-wall.Panels[1].X, wall.Panels[0].Y-wall.Panels[1].Y)
	if math.Abs(gap-200) > 1e-9 {
		t.Errorf("the panels are %v apart after turning, want 200", gap)
	}
}

// TestLightsSplitsThePanelsTheDeviceCannotShow keeps the two reasons a panel
// is dropped apart: one is normal and one is worth reporting.
func TestLightsSplitsThePanelsTheDeviceCannotShow(t *testing.T) {
	wall := Project(nanoleaf.Layout{
		Panels: []nanoleaf.Panel{
			{ID: 1, ShapeType: nanoleaf.ShapeTriangle},
			{ID: 2, ShapeType: nanoleaf.ShapeController},
			{ID: math.MaxUint16 + 1, ShapeType: nanoleaf.ShapeTriangle},
		},
	}, 0)

	usable, unaddressable := wall.Lights()
	if len(usable) != 1 || usable[0].Panel.ID != 1 {
		t.Errorf("usable panels are %v, want just panel 1", usable)
	}
	if len(unaddressable) != 1 || unaddressable[0].Panel.ID != math.MaxUint16+1 {
		t.Errorf("unaddressable panels are %v, want the out-of-range one", unaddressable)
	}
}

// TestCalibrationDividesTheShape checks the picture actually splits top from
// bottom, since a calibration pattern that looked the same either way up
// would be worse than none.
func TestCalibrationDividesTheShape(t *testing.T) {
	geo := NewGeometry(
		[]int{1, 2, 3},
		[]float64{0, 0, 0},
		[]float64{0, 50, 100},
	)

	frame := Calibration(geo)
	if got := frame[1]; got != calibrateBottom {
		t.Errorf("the lowest panel is %+v, want green", got)
	}
	if got := frame[3]; got != calibrateTop {
		t.Errorf("the highest panel is %+v, want red", got)
	}
	if got := frame[2]; got != calibrateMiddle {
		t.Errorf("the middle panel is %+v, want the dim blue", got)
	}
}

func TestWrapDegrees(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, 0}, {15, 15}, {360, 0}, {725, 5}, {-15, 345}, {-360, 0}, {-725, 355},
	} {
		if got := WrapDegrees(tc.in); got != tc.want {
			t.Errorf("WrapDegrees(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestPhaseNamesRoundTrip(t *testing.T) {
	for _, p := range []Phase{PhaseIdle, PhaseThinking, PhaseTool, PhaseError} {
		got, err := ParsePhase(p.String())
		if err != nil {
			t.Fatalf("%v: %v", p, err)
		}
		if got != p {
			t.Errorf("%q parsed back as %v", p.String(), got)
		}
	}
	if _, err := ParsePhase("busy"); err == nil {
		t.Error("an unknown phase name was accepted")
	}
}
