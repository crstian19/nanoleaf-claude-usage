// Package render turns usage numbers into a colour per panel.
//
// Rendering is deliberately geometric rather than index-based. Panels are not
// treated as a list of N cells to light up in order; each one is a point on
// the wall, and a scene is a field evaluated at those points. That way the
// picture follows whatever shape the panels are actually mounted in — a
// vertical fill really does rise through the arrangement, and a travelling
// pulse really does run along its long axis.
package render

import (
	"cmp"
	"math"
	"slices"
)

// Point is one panel reduced to what a scene needs: its identity and its
// place within the mounted shape.
type Point struct {
	PanelID int

	// U and V are the panel's position inside the shape's bounding box,
	// each in [0,1]. V is 0 at the bottom of the wall and 1 at the top.
	U, V float64

	// S is the panel's position along the shape's long axis, in [0,1].
	// For a diagonal arrangement this is the axis a pulse should travel,
	// which is neither U nor V.
	S float64

	// Order is the panel's rank from the bottom of the shape upwards,
	// counting from zero.
	//
	// This is what divides the display into equal bands -- one panel per
	// band -- rather than V, which measures distance and would give panels
	// unequal shares according to how they happen to be mounted. A gauge
	// has to be countable: every panel means the same percentage.
	Order int
}

// Geometry is a mounted arrangement of panels in scene coordinates.
type Geometry struct {
	Points []Point
}

// panelPos is the minimum a caller has to provide per panel: an ID and wall
// coordinates with Y increasing upwards.
type panelPos struct {
	ID   int
	X, Y float64
}

// NewGeometry projects panel positions into scene coordinates.
//
// The long axis is found by principal component analysis over the panel
// centroids, so it is derived from the arrangement itself instead of assumed
// to be horizontal or vertical.
//
// ids, xs and ys must be the same length; production code should call
// FromLayout, which guarantees that. Points are returned in input order, so a
// caller may pair Points[i] with its own slice's element i.
func NewGeometry(ids []int, xs, ys []float64) Geometry {
	n := len(ids)
	if n == 0 {
		return Geometry{}
	}
	pos := make([]panelPos, n)
	for i := range ids {
		pos[i] = panelPos{ID: ids[i], X: xs[i], Y: ys[i]}
	}

	minX, maxX := pos[0].X, pos[0].X
	minY, maxY := pos[0].Y, pos[0].Y
	var sumX, sumY float64
	for _, p := range pos {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
		sumX, sumY = sumX+p.X, sumY+p.Y
	}
	meanX, meanY := sumX/float64(n), sumY/float64(n)

	ax, ay := principalAxis(pos, meanX, meanY)

	// Project onto the axis first, so S can be normalised over the real
	// extent of the projection rather than a guess.
	proj := make([]float64, n)
	minS, maxS := math.Inf(1), math.Inf(-1)
	for i, p := range pos {
		proj[i] = (p.X-meanX)*ax + (p.Y-meanY)*ay
		minS, maxS = math.Min(minS, proj[i]), math.Max(maxS, proj[i])
	}

	pts := make([]Point, n)
	for i, p := range pos {
		pts[i] = Point{
			PanelID: p.ID,
			U:       norm(p.X, minX, maxX),
			V:       norm(p.Y, minY, maxY),
			S:       norm(proj[i], minS, maxS),
		}
	}
	assignOrder(pts)
	return Geometry{Points: pts}
}

// assignOrder ranks the points from the bottom of the shape upwards.
//
// Ties are broken by U so the ordering is deterministic: two panels at the
// same height would otherwise swap between runs and make the band a given
// panel belongs to unstable.
func assignOrder(pts []Point) {
	idx := make([]int, len(pts))
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int {
		if c := cmp.Compare(pts[a].V, pts[b].V); c != 0 {
			return c
		}
		return cmp.Compare(pts[a].U, pts[b].U)
	})
	for rank, i := range idx {
		pts[i].Order = rank
	}
}

// principalAxis returns the unit vector along which the panel centroids vary
// most, from the closed-form eigenvector of their 2x2 covariance matrix.
func principalAxis(pos []panelPos, meanX, meanY float64) (x, y float64) {
	var cxx, cyy, cxy float64
	for _, p := range pos {
		dx, dy := p.X-meanX, p.Y-meanY
		cxx += dx * dx
		cyy += dy * dy
		cxy += dx * dy
	}

	// A single panel, or panels stacked at one point, has no meaningful
	// axis; anything unit-length will do.
	if cxx == 0 && cyy == 0 {
		return 0, 1
	}

	// Axis-aligned arrangements make the off-diagonal term vanish, and the
	// eigenvector formula below degenerates. Pick the wider axis directly.
	if math.Abs(cxy) < 1e-9 {
		if cyy >= cxx {
			return 0, 1
		}
		return 1, 0
	}

	trace, det := cxx+cyy, cxx*cyy-cxy*cxy
	// The discriminant of a real symmetric 2x2 matrix is never negative;
	// clamp only to absorb floating-point noise near zero.
	disc := math.Max(trace*trace/4-det, 0)
	lambda := trace/2 + math.Sqrt(disc)

	x, y = cxy, lambda-cxx
	if norm := math.Hypot(x, y); norm > 0 {
		x, y = x/norm, y/norm
	}

	// Orient the axis upwards (or rightwards when it is nearly flat) so a
	// pulse always travels the same way between runs.
	if y < 0 || (y == 0 && x < 0) {
		x, y = -x, -y
	}
	return x, y
}

// norm maps v from [lo,hi] onto [0,1], returning the midpoint when the range
// is degenerate — a single row of panels has no vertical extent to speak of.
func norm(v, lo, hi float64) float64 {
	if hi-lo < 1e-9 {
		return 0.5
	}
	return (v - lo) / (hi - lo)
}
