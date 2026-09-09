package webui

import (
	"errors"
	"fmt"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/shapes"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// BuildShape is the name of the wall the page builds itself.
const BuildShape = "your-own"

// buildLabel is how it is offered in the list of arrangements.
const buildLabel = "build your own wall"

// noPlacing means no panel is being dragged.
//
// Not zero: zero is the shape number of the original Light Panels, so a
// missing value and a real panel would be the same thing.
const noPlacing = -1

// Kind is a panel a page may drop onto a wall.
type Kind struct {
	// Shape is the device's shape number.
	Shape int `json:"shape"`
	// Label is what the palette calls it.
	Label string `json:"label"`
}

// editor is the wall being built, with its history.
//
// Every edit is applied to a copy of the wall and kept only if it worked, so
// a placement the geometry refuses leaves nothing behind, and the copies that
// were kept are the undo history. That is also why undo needs no work: it is
// the previous copy.
//
// It is not safe for concurrent use. The server keeps it behind its own lock,
// and hands the render loop the finished drawing rather than the editor.
type editor struct {
	kinds []Kind
	side  int

	// steps is never empty. The last is the wall as it stands.
	steps []*shapes.Builder

	// pic is the drawing of that wall, or nil while it has no panels. It
	// is replaced whole after every edit, never edited, so the render loop
	// only ever holds a drawing that nothing can change underneath it.
	pic *picture
}

// newEditor starts an empty wall.
func newEditor(side int, kinds []Kind) *editor {
	return &editor{
		kinds: kinds,
		side:  side,
		steps: []*shapes.Builder{shapes.Empty(side, false)},
	}
}

// wall is the wall as it stands.
func (e *editor) wall() *shapes.Builder { return e.steps[len(e.steps)-1] }

// apply tries a change, and keeps it only if it worked.
func (e *editor) apply(change func(*shapes.Builder)) error {
	next := e.wall().Clone()
	change(next)

	layout, err := next.Layout()
	if err != nil {
		return err
	}

	// A wall with nothing on it has no drawing: there is no shape to fit a
	// frame around, and every panel of it would be a panel that is not
	// there.
	var pic *picture
	if len(layout.Panels) > 0 {
		pic, err = newPicture(layout)
		if err != nil {
			return err
		}
	}

	e.steps = append(e.steps, next)
	e.pic = pic
	return nil
}

// undo goes back to the wall as it was before the last change.
func (e *editor) undo() error {
	if len(e.steps) < 2 {
		return errors.New("there is nothing to undo")
	}
	e.steps = e.steps[:len(e.steps)-1]
	return e.redraw()
}

// clear takes everything off the wall.
func (e *editor) clear() error {
	e.steps = []*shapes.Builder{shapes.Empty(e.side, false)}
	return e.redraw()
}

// redraw rebuilds the drawing for the wall as it stands.
func (e *editor) redraw() error {
	layout, err := e.wall().Layout()
	if err != nil {
		return err
	}
	if len(layout.Panels) == 0 {
		e.pic = nil
		return nil
	}
	pic, err := newPicture(layout)
	if err != nil {
		return err
	}
	e.pic = pic
	return nil
}

// knownKind reports whether the palette offers this panel.
func (e *editor) knownKind(shapeType int) bool {
	for _, kind := range e.kinds {
		if kind.Shape == shapeType {
			return true
		}
	}
	return false
}

// SpotView is a place a panel could be dropped, ready to be drawn.
type SpotView struct {
	Panel int `json:"panel"`
	Edge  int `json:"edge"`
	Half  int `json:"half"`

	// X and Y are where the panel's centre would land, in the same screen
	// coordinates as the wall, so the page can tell which spot the mouse
	// is nearest.
	X float64 `json:"x"`
	Y float64 `json:"y"`

	// Points is the outline the panel would have there, as an SVG polygon
	// attribute. The page draws it rather than working out what would
	// happen, so what is shown under the cursor is what lands.
	Points string `json:"points"`
}

// frame is how a wall is placed on the screen: the rotation applied to it and
// the point the drawing is centred on.
//
// A spot is a place on the same wall as the panels, so it has to be turned
// and centred the same way. Passing this in rather than reading it back off a
// drawing keeps the two from drifting apart.
type frame struct {
	turn     render.Turn
	rotation float64
	center   point
}

// emptyFrame is the frame of a wall with nothing on it. There are no panels
// to turn about, so the one spot it offers sits at the origin either way.
func emptyFrame() frame { return frame{turn: render.TurnOf(nil, 0)} }

// frameOf is the frame a drawing is in.
func frameOf(pic *picture) frame {
	if pic == nil {
		return emptyFrame()
	}
	return frame{turn: pic.wall.Turn, rotation: pic.wall.Rotation, center: pic.center}
}

// spotViews turns the places a panel of this kind could go into outlines the
// page can draw, in the same frame as the wall.
func (e *editor) spotViews(shapeType int, in frame) []SpotView {
	if !e.knownKind(shapeType) {
		return nil
	}

	spots := e.wall().Spots(shapeType)
	poly := e.wall().PolygonOf(shapeType)

	views := make([]SpotView, 0, len(spots))
	for _, spot := range spots {
		x, y := in.turn.Apply(spot.X, spot.Y)
		views = append(views, SpotView{
			Panel:  spot.Panel,
			Edge:   spot.Edge,
			Half:   spot.Half,
			X:      x - in.center.X,
			Y:      in.center.Y - y,
			Points: svgPoints(poly.Outline(x, y, spot.Orientation+in.rotation), in.center),
		})
	}
	return views
}

// editRequest is a change to the wall being built.
type editRequest struct {
	// Action is place, attach, remove, undo or clear.
	Action string `json:"action"`

	// Shape is the panel to put down, for place and attach.
	Shape *int `json:"shape"`

	// Panel and Edge say where, for attach; Panel alone for remove. Half
	// picks which half of an edge a smaller panel covers.
	Panel *int `json:"panel"`
	Edge  *int `json:"edge"`
	Half  *int `json:"half"`
}

// edit applies one change from the page.
func (e *editor) edit(req editRequest) error {
	switch req.Action {
	case "place":
		if req.Shape == nil {
			return errors.New("no panel to place")
		}
		if !e.knownKind(*req.Shape) {
			return fmt.Errorf("%s is not on the palette", nanoleaf.ShapeName(*req.Shape))
		}
		shapeType := *req.Shape
		return e.apply(func(b *shapes.Builder) { b.Place(shapeType) })

	case "attach":
		if req.Shape == nil || req.Panel == nil || req.Edge == nil {
			return errors.New("an attachment needs a panel, an edge and a shape")
		}
		if !e.knownKind(*req.Shape) {
			return fmt.Errorf("%s is not on the palette", nanoleaf.ShapeName(*req.Shape))
		}
		shapeType, panel, edge := *req.Shape, *req.Panel, *req.Edge
		half := shapes.NoHalf
		if req.Half != nil {
			half = *req.Half
		}
		return e.apply(func(b *shapes.Builder) {
			if half == shapes.NoHalf {
				b.Attach(panel, edge, shapeType)
				return
			}
			b.AttachHalf(panel, edge, shapeType, half)
		})

	case "remove":
		if req.Panel == nil {
			return errors.New("no panel to take off")
		}
		panel := *req.Panel
		if err := e.apply(func(b *shapes.Builder) { _ = b.Remove(panel) }); err != nil {
			return err
		}
		// Remove reports its own failure rather than making the wall
		// sticky, so a panel that was not there has to be checked for
		// here.
		return nil

	case "undo":
		return e.undo()

	case "clear":
		return e.clear()

	default:
		return fmt.Errorf("unknown action %q", req.Action)
	}
}
