package render

import "math"

// The rainbow is drawn at the most chroma each hue will physically take,
// which is not the same amount for every hue.
//
// A single chroma for the whole circle has to be the smallest of those
// maxima, since anything larger clips somewhere -- and a clipped colour is
// not the colour it claims to be. At the lightness used here that smallest
// maximum is about 0.13, while the greens and yellows would take 0.34 or
// more. Holding every hue down to 0.13 threw away most of the available
// saturation to accommodate one part of the circle.
//
// So the chroma is looked up per hue instead. Lightness stays constant, which
// is what keeps the rainbow even and the ambient highlight landing equally on
// all of it; only the saturation follows the gamut.
const (
	// chromaSamples is how finely the gamut boundary is sampled. Ample:
	// the boundary is smooth in hue, and the safety margin below covers
	// the interpolation error between samples.
	chromaSamples = 256

	// chromaSafety keeps the result just inside the boundary, so neither
	// the bisection's last step nor the interpolation between samples can
	// tip a channel over.
	chromaSafety = 0.96

	// chromaCeiling bounds the search. No hue in sRGB reaches this.
	chromaCeiling = 0.5
)

// maxChromaTable holds the gamut boundary at rainbowL, sampled around the hue
// circle. Built once: 256 bisections at startup is nothing, and it keeps the
// per-frame cost to a lookup.
var maxChromaTable = buildMaxChromaTable(rainbowL)

func buildMaxChromaTable(l float64) [chromaSamples]float64 {
	var table [chromaSamples]float64
	for i := range table {
		hue := 2 * math.Pi * float64(i) / chromaSamples
		table[i] = solveMaxChroma(l, hue)
	}
	return table
}

// solveMaxChroma bisects for the largest chroma at which a hue is still
// inside the sRGB gamut.
func solveMaxChroma(l, hue float64) float64 {
	lo, hi := 0.0, chromaCeiling
	for range 24 {
		mid := (lo + hi) / 2
		if inGamut(oklabPolar(l, mid, hue).color()) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// inGamut reports whether every channel of a linear-light colour is
// representable.
//
// Note that oklab.color already clamps negatives away, so a colour that was
// out of gamut on the low side arrives here looking fine; the bisection is
// therefore driven by the upper bound, which is the one that actually binds
// for a bright, saturated hue.
func inGamut(c Color) bool {
	return c.R <= 1 && c.G <= 1 && c.B <= 1
}

// maxChromaFor returns the usable chroma for a hue in radians, interpolating
// between samples.
func maxChromaFor(hue float64) float64 {
	// Normalise into [0, 2pi) so any phase can be handed in.
	turns := math.Mod(hue/(2*math.Pi), 1)
	if turns < 0 {
		turns++
	}

	pos := turns * chromaSamples
	i := int(pos)
	frac := pos - float64(i)

	a := maxChromaTable[i%chromaSamples]
	b := maxChromaTable[(i+1)%chromaSamples]
	return (a + (b-a)*frac) * chromaSafety
}
