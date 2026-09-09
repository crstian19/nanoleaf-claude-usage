package render

import (
	"math"

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
	lights := l.Lights()

	ids := make([]int, 0, len(lights))
	xs := make([]float64, 0, len(lights))
	ys := make([]float64, 0, len(lights))
	var skipped []nanoleaf.Panel

	for _, p := range lights {
		if !p.Addressable() {
			skipped = append(skipped, p)
			continue
		}
		ids = append(ids, p.ID)
		xs = append(xs, float64(p.X))
		ys = append(ys, float64(p.Y))
	}

	// The global orientation is SUBTRACTED, not added. It describes how the
	// arrangement is rotated in the device's own frame, so undoing it is
	// what brings the coordinates back to the wall. Getting the sign wrong
	// is not a small error: verified against a real NL42 mounted at
	// globalOrientation 302, adding it put the vertical axis 116 degrees
	// out -- the fill climbed diagonally, which on a wall reads as a design
	// choice rather than a bug.
	rotate(xs, ys, float64(extraRotation-l.GlobalOrientation))
	return NewGeometry(ids, xs, ys), skipped
}

// rotate turns the point cloud in place, about its own centroid.
//
// The centroid is used rather than the origin only for tidiness: the scene
// coordinates that come out are normalised over the bounding box anyway, so
// the centre of rotation cannot affect the result -- but keeping the numbers
// small makes a printed layout readable.
func rotate(xs, ys []float64, degrees float64) {
	// Exact no-op for the common case, so an unrotated layout is not
	// perturbed by floating-point error.
	if math.Mod(degrees, 360) == 0 {
		return
	}

	var sumX, sumY float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
	}
	n := float64(len(xs))
	if n == 0 {
		return
	}
	cx, cy := sumX/n, sumY/n

	rad := degrees * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)

	for i := range xs {
		dx, dy := xs[i]-cx, ys[i]-cy
		xs[i] = dx*cos - dy*sin
		ys[i] = dx*sin + dy*cos
	}
}
