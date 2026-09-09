package render

import "math"

// FacingOrientation returns the orientation that turns this polygon so the
// given edge's outward normal points at the given angle.
//
// It is the inverse of EdgeAngle, and it is what lets one panel be laid
// against another's edge: the new panel has to face back the way it came
// from. Solving it here rather than at the call site keeps the edge numbering
// in one place, so a builder that sticks panels together and a page that
// draws them cannot disagree about which edge is which.
func (p Polygon) FacingOrientation(edge int, angle float64) float64 {
	return angle - p.Base - (float64(edge)+0.5)*p.step()
}

// EdgeToward returns the edge whose outward normal points nearest to the
// given angle, and how far off it is in degrees.
//
// A wall is described in words -- another one to the right, one above this --
// and this turns that into an edge number. The distance is returned because
// some shapes have no edge facing a given way: an upward triangle has no
// upward edge, and a caller that ignored that would stick a panel on
// somewhere unintended.
func (p Polygon) EdgeToward(orientation, angle float64) (edge int, off float64) {
	best, bestOff := 0, math.Inf(1)
	for i := range p.Sides {
		diff := math.Abs(wrapDegrees180(p.EdgeAngle(orientation, i) - angle))
		if diff < bestOff-1e-9 {
			best, bestOff = i, diff
		}
	}
	return best, bestOff
}

// wrapDegrees180 brings an angle into (-180, 180].
func wrapDegrees180(degrees float64) float64 {
	for degrees <= -180 {
		degrees += 360
	}
	for degrees > 180 {
		degrees -= 360
	}
	return degrees
}
