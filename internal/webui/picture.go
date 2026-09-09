package webui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Mode is which picture the panels are showing.
type Mode string

const (
	// ModePattern is the calibration picture: green along the bottom of
	// the shape, red along the top.
	ModePattern Mode = "pattern"

	// ModeGauge is what the display normally shows, at a level the page
	// chooses, so the look can be checked without waiting to spend a real
	// allowance.
	ModeGauge Mode = "gauge"
)

// maxLevel is the highest gauge level the page may ask for.
//
// One, not more, because the renderer clamps the budget to the same value:
// asking for 150% would paint exactly the same picture as 100%, so the top
// third of a slider that allowed it would do nothing. An account with overage
// enabled really does go past its allowance, but nothing shows that yet, and a
// control that pretends to is worse than one that stops.
const maxLevel = 1.0

// view is everything the page controls.
type view struct {
	// Shape is which arrangement is being drawn, by name. Empty when the
	// session has only one.
	Shape string

	// Placing is the kind of panel being dragged onto the wall, or
	// noPlacing. While it is set, every place that panel could go is sent
	// with the picture.
	Placing  int
	Rotation int
	Mode     Mode
	Level    float64
	Phase    render.Phase
}

// Snapshot is one picture, as the page draws it.
//
// It carries the colours that went to the panels rather than colours computed
// for the screen. The point of the page is to agree with the wall, so both
// come from the same frame.
type Snapshot struct {
	Shape    string  `json:"shape"`
	Rotation int     `json:"rotation"`
	Mode     Mode    `json:"mode"`
	Level    float64 `json:"level"`
	Phase    string  `json:"phase"`

	// Extent is half the width of the square the shape is drawn in, in
	// the device's own units. It never changes as the shape turns, so the
	// drawing does not resize while it is being dragged.
	Extent float64 `json:"extent"`

	Panels []PanelView `json:"panels"`

	// Editing says the wall on screen is one the page is building, so it
	// can offer the palette and take panels off again.
	Editing bool `json:"editing"`

	// Spots are the places the panel being dragged could go, empty when
	// nothing is being dragged.
	Spots []SpotView `json:"spots,omitempty"`

	// Trouble is what the panels last said when they refused a frame, so
	// a wall that has gone dark says why on the page rather than only in
	// the terminal behind it.
	Trouble string `json:"trouble,omitempty"`
}

// PanelView is one panel of a Snapshot, ready to be drawn.
type PanelView struct {
	ID    int    `json:"id"`
	Shape string `json:"shape"`

	// Points are the panel's corners as an SVG polygon attribute, in
	// screen coordinates: the shape is centred on the origin and Y grows
	// downwards, as SVG expects.
	Points string `json:"points"`

	// Color is the colour this panel is showing right now, as #rrggbb.
	Color string `json:"color"`

	// Order is the panel's rank from the bottom of the shape upwards. The
	// page shows it so a user can see which panel the gauge fills first.
	Order int `json:"order"`

	// Label is where that number goes, in the same screen coordinates as
	// Points. Computed here because the page does no geometry.
	Label [2]float64 `json:"label"`
}

// point is a place in the drawing.
type point struct{ X, Y float64 }

// picture renders both halves of the tool: the frame that goes to the panels
// and the drawing that goes to the page.
//
// One object owns both so they cannot fall out of step. It is not safe for
// concurrent use; the server keeps it inside its own loop.
type picture struct {
	layout   nanoleaf.Layout
	outliner render.Outliner

	// extent is computed once, from a bound that rotation cannot change.
	extent float64

	// The projection for the rotation currently loaded.
	rotation int
	loaded   bool
	wall     render.Wall
	geo      render.Geometry
	scene    *render.Scene
	center   point
}

// newPicture prepares a layout for drawing.
func newPicture(l nanoleaf.Layout) (*picture, error) {
	wall := render.Project(l, 0)
	usable, _ := wall.Lights()
	if len(usable) == 0 {
		return nil, fmt.Errorf("webui: the device reported %d panels and none of them can be lit", len(l.Panels))
	}

	outliner := render.NewOutliner(wall)
	p := &picture{layout: l, outliner: outliner, extent: viewExtent(wall, outliner)}
	p.load(0)
	return p, nil
}

// load projects the layout at a rotation, reusing the last projection when
// the rotation has not changed. Turning the shape is the one thing this tool
// does, so the common case is a value that only changes when a key is pressed
// or the mouse moves.
func (p *picture) load(rotation int) {
	if p.loaded && p.rotation == rotation {
		return
	}
	p.wall = render.Project(p.layout, rotation)
	p.geo, _ = render.FromWall(p.wall)
	p.scene = render.NewScene(p.geo)
	p.center = drawnCenter(p.wall)
	p.rotation = rotation
	p.loaded = true
}

// frame is the picture to send to the panels.
func (p *picture) frame(v view, elapsed time.Duration) nanoleaf.Frame {
	p.load(v.Rotation)
	if v.Mode == ModeGauge {
		return p.scene.Frame(render.Input{Budget: v.Level, Phase: v.Phase}, elapsed)
	}
	return render.Calibration(p.geo)
}

// snapshot is the same picture, as the page draws it.
func (p *picture) snapshot(v view, frame nanoleaf.Frame) Snapshot {
	p.load(v.Rotation)

	usable, _ := p.wall.Lights()
	order := make(map[int]int, len(p.geo.Points))
	for _, pt := range p.geo.Points {
		order[pt.PanelID] = pt.Order
	}

	panels := make([]PanelView, 0, len(usable))
	for _, wp := range usable {
		panels = append(panels, PanelView{
			ID:     wp.Panel.ID,
			Shape:  nanoleaf.ShapeName(wp.Panel.ShapeType),
			Points: svgPoints(p.outliner.Outline(wp), p.center),
			Color:  hexColor(frame[wp.Panel.ID]),
			Order:  order[wp.Panel.ID],
			Label:  [2]float64{wp.X - p.center.X, p.center.Y - wp.Y},
		})
	}

	return Snapshot{
		Shape:    v.Shape,
		Rotation: v.Rotation,
		Mode:     v.Mode,
		Level:    v.Level,
		Phase:    v.Phase.String(),
		Extent:   p.extent,
		Panels:   panels,
	}
}

// emptySnapshot is the picture of a wall with nothing on it.
//
// It still needs an extent, because the page has to have a frame to draw the
// first panel's landing spot in.
func emptySnapshot(v view, side int) Snapshot {
	extent := float64(side) * 2.2
	if extent <= 0 {
		extent = 300
	}
	return Snapshot{
		Shape:    v.Shape,
		Rotation: v.Rotation,
		Mode:     v.Mode,
		Level:    v.Level,
		Phase:    v.Phase.String(),
		Extent:   extent,
		// Empty rather than absent: a JSON null here would be a
		// missing list to the page, and it draws by walking the list.
		Panels: []PanelView{},
	}
}

// svgPoints writes corners as an SVG polygon attribute, moving the shape to
// the origin and flipping Y, which is the only difference between wall
// coordinates and screen ones.
func svgPoints(pts []render.Corner, center point) string {
	var b strings.Builder
	for i, pt := range pts {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.FormatFloat(pt.X-center.X, 'f', 1, 64))
		b.WriteByte(',')
		b.WriteString(strconv.FormatFloat(center.Y-pt.Y, 'f', 1, 64))
	}
	return b.String()
}

// hexColor formats a panel colour the way CSS wants it.
func hexColor(c nanoleaf.RGB) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// drawnCenter is the centre of the panels that get drawn, which is what the
// picture is centred on. The controller brick is left out: it is often
// mounted off to one side, and letting it pull the frame across would push
// the shape itself off centre.
func drawnCenter(w render.Wall) point {
	usable, _ := w.Lights()
	var sum point
	for _, wp := range usable {
		sum.X += wp.X
		sum.Y += wp.Y
	}
	n := float64(len(usable))
	if n == 0 {
		return point{}
	}
	return point{X: sum.X / n, Y: sum.Y / n}
}

// viewExtent is half the width of a square that holds the shape at every
// rotation.
//
// It has to be rotation invariant, or the drawing would breathe in and out as
// the shape is dragged -- a shape wider than tall covers more of a square
// frame at 90 degrees than at 0. Turning happens about a fixed centre, so
// every corner keeps its distance from that centre; the bound below is built
// from those distances only, plus the offset between the centre of rotation
// and the centre the picture is drawn about. Both terms survive any rotation,
// so the frame never moves.
func viewExtent(w render.Wall, outliner render.Outliner) float64 {
	pivot := rotationCenter(w)
	usable, _ := w.Lights()

	var reach float64
	for _, wp := range usable {
		for _, c := range outliner.Outline(wp) {
			reach = math.Max(reach, math.Hypot(c.X-pivot.X, c.Y-pivot.Y))
		}
	}

	center := drawnCenter(w)
	offset := math.Hypot(center.X-pivot.X, center.Y-pivot.Y)

	// A margin so the outermost panel is not flush with the edge of the
	// drawing.
	const margin = 1.06
	return (reach + offset) * margin
}

// rotationCenter is the point the projection turns the layout about, which is
// the centroid of every panel the device reports.
func rotationCenter(w render.Wall) point {
	var sum point
	for _, wp := range w.Panels {
		sum.X += wp.X
		sum.Y += wp.Y
	}
	n := float64(len(w.Panels))
	if n == 0 {
		return point{}
	}
	return point{X: sum.X / n, Y: sum.Y / n}
}
