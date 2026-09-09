package render

import (
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Wall is a device layout placed on the wall: the same panels, with the
// device's own global orientation undone, so Y really does point at the
// ceiling.
//
// It exists so that everything which has to agree about where a panel is --
// the renderer and the calibration page that draws the shape -- shares one
// rotation instead of each applying its own. Two implementations of the same
// transform is exactly the bug this project already paid for once: the sign
// of the global orientation was wrong for a while, and a second copy would
// have hidden the fix.
type Wall struct {
	// Panels are every panel the device reported, including the ones with
	// no LEDs. Filtering is the caller's business: a drawing may want to
	// show them, a frame must not.
	Panels []WallPanel

	// SideLength is the panel edge length in the device's own units.
	SideLength int

	// Turn is the rotation that was applied, so a caller can put a point
	// which is not a panel into the same frame as the panels.
	Turn Turn

	// Rotation is the angle Turn applies, in degrees. A panel that is not
	// in the layout -- one being dragged onto it -- has to be turned by it
	// as well as moved.
	Rotation float64
}

// WallPanel is one panel of a Wall.
type WallPanel struct {
	// Panel is the record the device reported, untouched.
	Panel nanoleaf.Panel

	// X and Y are the panel's centroid in wall coordinates, Y up. The
	// origin is arbitrary, as it is on the device.
	X, Y float64

	// Orientation is how the panel itself is turned on the wall, in
	// degrees. It is the panel's own reported orientation plus the
	// rotation applied to the whole arrangement, which is what a drawing
	// needs to place its corners.
	Orientation float64
}

// Project places a device layout on the wall.
//
// extraRotation is added to the layout's own global orientation, in degrees,
// for installations where the two do not agree.
func Project(l nanoleaf.Layout, extraRotation int) Wall {
	turn, degrees := turnFor(l, extraRotation)

	panels := make([]WallPanel, len(l.Panels))
	for i, p := range l.Panels {
		x, y := turn.Apply(float64(p.X), float64(p.Y))
		panels[i] = WallPanel{
			Panel:       p,
			X:           x,
			Y:           y,
			Orientation: float64(p.Orientation) + degrees,
		}
	}
	return Wall{Panels: panels, SideLength: l.SideLength, Turn: turn, Rotation: degrees}
}

// turnFor is the rotation that puts this layout on the wall, and the angle it
// applies.
func turnFor(l nanoleaf.Layout, extraRotation int) (Turn, float64) {
	pos := make([]panelPos, len(l.Panels))
	for i, p := range l.Panels {
		pos[i] = panelPos{ID: p.ID, X: float64(p.X), Y: float64(p.Y)}
	}
	degrees := float64(extraRotation - l.GlobalOrientation)
	return TurnOf(pos, degrees), degrees
}

// Lights returns only the panels that emit light and can be addressed, along
// with the ones that emit light but cannot.
//
// Both halves matter to a caller: the first is what can be drawn or lit, and
// the second is worth saying out loud once rather than discovering a frame at
// a time.
func (w Wall) Lights() (usable, unaddressable []WallPanel) {
	usable = make([]WallPanel, 0, len(w.Panels))
	for _, wp := range w.Panels {
		if !wp.Panel.IsLight() {
			continue
		}
		if !wp.Panel.Addressable() {
			unaddressable = append(unaddressable, wp)
			continue
		}
		usable = append(usable, wp)
	}
	return usable, unaddressable
}
