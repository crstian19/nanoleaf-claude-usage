package render

import "math"

// Turn is the rotation that puts a device layout on the wall.
//
// It exists so that a point which is not a panel can be put in the same frame
// as the panels. The calibration page needs that: while a panel is being
// dragged onto a wall, it draws every place the panel could go, and those
// places have to land exactly where the panels themselves land.
type Turn struct {
	cx, cy   float64
	sin, cos float64
}

// TurnOf is the rotation Project applies to a layout.
//
// extraRotation is added to the layout's own global orientation, in degrees.
// The global orientation is SUBTRACTED, not added: it describes how the
// arrangement is rotated in the device's own frame, so undoing it is what
// brings the coordinates back to the wall. Getting the sign wrong is not a
// small error -- verified against a real NL42 mounted at globalOrientation
// 302, adding it put the vertical axis 116 degrees out, and the fill climbed
// diagonally, which on a wall reads as a design choice rather than a bug.
func TurnOf(panels []panelPos, degrees float64) Turn {
	var sumX, sumY float64
	for _, p := range panels {
		sumX += p.X
		sumY += p.Y
	}

	t := Turn{sin: 0, cos: 1}
	if n := float64(len(panels)); n > 0 {
		t.cx, t.cy = sumX/n, sumY/n
	}

	// Exact no-op for the common case, so an unrotated layout is not
	// perturbed by floating-point error.
	if math.Mod(degrees, 360) == 0 {
		return t
	}
	rad := degrees * math.Pi / 180
	t.sin, t.cos = math.Sin(rad), math.Cos(rad)
	return t
}

// Apply turns one point.
//
// The centre of rotation is the centroid of the panels the Turn was built
// from. Which point that is cannot affect a rendered scene, since scene
// coordinates are normalised over the bounding box, but it does decide where
// a drawing sits, so everything that draws has to turn about the same place.
func (t Turn) Apply(x, y float64) (float64, float64) {
	dx, dy := x-t.cx, y-t.cy
	return t.cx + dx*t.cos - dy*t.sin, t.cy + dx*t.sin + dy*t.cos
}
