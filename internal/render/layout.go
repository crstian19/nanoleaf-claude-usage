package render

import (
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// FromLayout projects a device layout into scene coordinates.
//
// This is the only conversion production code should use. It returns the
// panels it had to leave out, so a caller can say so once rather than
// discovering it a frame at a time:
//
//   - the controller brick, which reports itself as a panel but has no LEDs
//   - any panel whose ID does not fit the protocol's 16-bit field, since one
//     such ID would otherwise cause every frame to be rejected whole
//
// extraRotation is added to the layout's own global orientation, in degrees,
// for installations where the two do not agree.
func FromLayout(l nanoleaf.Layout, extraRotation int) (Geometry, []nanoleaf.Panel) {
	return FromWall(Project(l, extraRotation))
}

// FromWall is FromLayout for a layout already placed on the wall.
//
// It exists so a caller that needs both the scene coordinates and the
// mounted shape -- the calibration page needs the outlines as well as the
// bands -- projects the layout once instead of twice.
func FromWall(wall Wall) (Geometry, []nanoleaf.Panel) {
	usable, unaddressable := wall.Lights()

	ids := make([]int, len(usable))
	xs := make([]float64, len(usable))
	ys := make([]float64, len(usable))
	for i, wp := range usable {
		ids[i] = wp.Panel.ID
		xs[i], ys[i] = wp.X, wp.Y
	}

	skipped := make([]nanoleaf.Panel, 0, len(unaddressable))
	for _, wp := range unaddressable {
		skipped = append(skipped, wp.Panel)
	}
	if len(skipped) == 0 {
		// Nil rather than an empty slice, so a caller may compare
		// against nil as well as check the length.
		skipped = nil
	}
	return NewGeometry(ids, xs, ys), skipped
}
