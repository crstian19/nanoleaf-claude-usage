package webui

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/shapes"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// corners parses an SVG polygon attribute back into points, in the screen
// coordinates the page draws in.
func corners(t *testing.T, points string) []point {
	t.Helper()
	fields := strings.Fields(points)
	out := make([]point, 0, len(fields))
	for _, field := range fields {
		x, y, ok := strings.Cut(field, ",")
		if !ok {
			t.Fatalf("corner %q is not x,y", field)
		}
		px, err := strconv.ParseFloat(x, 64)
		if err != nil {
			t.Fatalf("corner %q: %v", field, err)
		}
		py, err := strconv.ParseFloat(y, 64)
		if err != nil {
			t.Fatalf("corner %q: %v", field, err)
		}
		out = append(out, point{X: px, Y: py})
	}
	return out
}

// realLayout is the arrangement this program was written against, as its own
// device reports it. It lives with the samples so that the drawing tests and
// the tiling tests measure the same wall.
func realLayout(t *testing.T) nanoleaf.Layout {
	t.Helper()
	sample, err := shapes.Named("triangles-zigzag")
	if err != nil {
		t.Fatal(err)
	}
	return sample.Layout
}

// screenCentre averages a polygon's corners.
func screenCentre(t *testing.T, points string) point {
	t.Helper()
	pts := corners(t, points)
	var sum point
	for _, p := range pts {
		sum.X += p.X
		sum.Y += p.Y
	}
	return point{X: sum.X / float64(len(pts)), Y: sum.Y / float64(len(pts))}
}

func snapshotAt(t *testing.T, pic *picture, rotation int) Snapshot {
	t.Helper()
	v := view{Rotation: rotation, Mode: ModePattern, Level: 0.6, Phase: render.PhaseIdle}
	return pic.snapshot(v, pic.frame(v, 0))
}

func panelView(t *testing.T, snap Snapshot, id int) PanelView {
	t.Helper()
	for _, p := range snap.Panels {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("panel %d is not in the snapshot", id)
	return PanelView{}
}

// TestExtentSurvivesRotation keeps the drawing still while it is dragged.
//
// The frame is a square, and a shape wider than tall fills more of one at 90
// degrees than at 0. If the extent were measured from the shape's bounding
// box, the drawing would zoom in and out as the mouse moved, which reads as
// the page being broken rather than as the shape turning.
func TestExtentSurvivesRotation(t *testing.T) {
	pic, err := newPicture(realLayout(t))
	if err != nil {
		t.Fatal(err)
	}

	want := pic.extent
	for rotation := 0; rotation < 360; rotation += 10 {
		snap := snapshotAt(t, pic, rotation)
		if snap.Extent != want {
			t.Fatalf("extent at %d degrees is %v, want %v", rotation, snap.Extent, want)
		}
		// Corners, not centres: the bound is over corners, and a
		// panel whose centre is inside the frame can still have half
		// of itself hanging out of it.
		for _, p := range snap.Panels {
			for _, c := range corners(t, p.Points) {
				if math.Abs(c.X) > want || math.Abs(c.Y) > want {
					t.Fatalf("a corner of panel %d is outside the drawing at %d degrees: %+v",
						p.ID, rotation, c)
				}
			}
		}
	}
}

// TestScreenCoordinatesTurnCounterClockwise pins the direction, which is what
// the page's dragging depends on: a quarter turn has to send a panel at the
// top of the drawing to the left of it, not the right.
//
// In screen coordinates, where Y grows downwards, adding 90 degrees maps
// (x, y) to (y, -x).
func TestScreenCoordinatesTurnCounterClockwise(t *testing.T) {
	pic, err := newPicture(realLayout(t))
	if err != nil {
		t.Fatal(err)
	}

	const id = 17522
	before := screenCentre(t, panelView(t, snapshotAt(t, pic, 0), id).Points)
	after := screenCentre(t, panelView(t, snapshotAt(t, pic, 90), id).Points)

	const tolerance = 0.2
	if math.Abs(after.X-before.Y) > tolerance || math.Abs(after.Y+before.X) > tolerance {
		t.Errorf("a quarter turn moved panel %d from %+v to %+v, want (%.1f, %.1f)",
			id, before, after, before.Y, -before.X)
	}
}

// TestPageAndWallGetTheSamePicture is the whole reason a snapshot carries
// colours instead of the page working them out: the shape on screen has to be
// the shape on the wall.
func TestPageAndWallGetTheSamePicture(t *testing.T) {
	pic, err := newPicture(realLayout(t))
	if err != nil {
		t.Fatal(err)
	}

	v := view{Rotation: 0, Mode: ModePattern, Level: 0.6, Phase: render.PhaseIdle}
	frame := pic.frame(v, 0)
	snap := pic.snapshot(v, frame)

	if len(snap.Panels) != len(frame) {
		t.Fatalf("the page is sent %d panels and the wall %d", len(snap.Panels), len(frame))
	}
	for _, p := range snap.Panels {
		if want := hexColor(frame[p.ID]); p.Color != want {
			t.Errorf("panel %d is %s on the page and %s on the wall", p.ID, p.Color, want)
		}
	}

	// The picture is only useful if it actually divides the shape: the
	// lowest panel green, the highest red.
	lowest, highest := snap.Panels[0], snap.Panels[0]
	for _, p := range snap.Panels {
		if p.Order < lowest.Order {
			lowest = p
		}
		if p.Order > highest.Order {
			highest = p
		}
	}
	if lowest.Color != hexColor(nanoleaf.RGB{G: 255}) {
		t.Errorf("the bottom panel is %s, want green", lowest.Color)
	}
	if highest.Color != hexColor(nanoleaf.RGB{R: 255}) {
		t.Errorf("the top panel is %s, want red", highest.Color)
	}
}

// TestTheLabelSitsOnItsPanel covers the one number the page draws: it comes
// from Go, like every other coordinate, so that the browser does no geometry
// of its own.
func TestTheLabelSitsOnItsPanel(t *testing.T) {
	pic, err := newPicture(realLayout(t))
	if err != nil {
		t.Fatal(err)
	}

	snap := snapshotAt(t, pic, 30)
	for _, p := range snap.Panels {
		centre := screenCentre(t, p.Points)
		label := point{X: p.Label[0], Y: p.Label[1]}
		if math.Hypot(centre.X-label.X, centre.Y-label.Y) > 0.2 {
			t.Errorf("panel %d is drawn at %+v and its number at %+v", p.ID, centre, label)
		}
	}
}

// TestGaugeModeShowsTheRealDisplay checks the other picture: a full gauge
// lights every panel, which the calibration pattern never does.
func TestGaugeModeShowsTheRealDisplay(t *testing.T) {
	pic, err := newPicture(realLayout(t))
	if err != nil {
		t.Fatal(err)
	}

	v := view{Rotation: 0, Mode: ModeGauge, Level: 1, Phase: render.PhaseIdle}
	snap := pic.snapshot(v, pic.frame(v, 0))
	for _, p := range snap.Panels {
		if p.Color == "#000000" {
			t.Errorf("panel %d is dark at a full gauge", p.ID)
		}
	}
}

// TestNoLitPanelsIsRefused stops the page opening on a device it cannot
// paint, which would otherwise show an empty frame and no reason for it.
func TestNoLitPanelsIsRefused(t *testing.T) {
	_, err := newPicture(nanoleaf.Layout{
		SideLength: 134,
		Panels:     []nanoleaf.Panel{{ID: 1, ShapeType: nanoleaf.ShapeController}},
	})
	if err == nil {
		t.Fatal("a layout of nothing but a controller was accepted")
	}
}
