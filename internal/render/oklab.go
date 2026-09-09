package render

import "math"

// OKLab is a perceptual colour space: equal numeric distances in it
// correspond to roughly equal perceived differences.
//
// It exists here for one reason. Interpolating green to red through linear
// light passes through a bright yellow-green that reads as a separate colour
// rather than a midpoint, which forced an extra hand-placed stop into the
// budget ramp to hide it. In OKLab the same two endpoints interpolate evenly
// and the extra stop is unnecessary.
//
// Interpolation only. Adding two light sources is physics and belongs in
// linear light -- see Color.Add.
type oklab struct {
	L, A, B float64
}

// Matrix coefficients are Björn Ottosson's, for linear sRGB. Color is already
// linear light, so no transfer function is applied here.
func (c Color) oklab() oklab {
	l := 0.4122214708*c.R + 0.5363325363*c.G + 0.0514459929*c.B
	m := 0.2119034982*c.R + 0.6806995451*c.G + 0.1073969566*c.B
	s := 0.0883024619*c.R + 0.2817188376*c.G + 0.6299787005*c.B

	// Cbrt rather than Pow: it is defined for negative inputs, which
	// out-of-gamut intermediates can produce.
	l, m, s = math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)

	return oklab{
		L: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		A: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		B: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// color converts back to linear light, clamping away negative channels.
//
// A straight line between two in-gamut OKLab colours can leave the sRGB
// gamut, which comes back as a small negative channel. Left alone it would
// survive into Add and brighten a panel it should not.
func (o oklab) color() Color {
	l := o.L + 0.3963377774*o.A + 0.2158037573*o.B
	m := o.L - 0.1055613458*o.A - 0.0638541728*o.B
	s := o.L - 0.0894841775*o.A - 1.2914855480*o.B

	l, m, s = l*l*l, m*m*m, s*s*s

	return Color{
		R: math.Max(4.0767416621*l-3.3077115913*m+0.2309699292*s, 0),
		G: math.Max(-1.2684380046*l+2.6097574011*m-0.3413193965*s, 0),
		B: math.Max(-0.0041960863*l-0.7034186147*m+1.7076147010*s, 0),
	}
}

// lerp blends two perceptual colours componentwise.
func (o oklab) lerp(t oklab, k float64) oklab {
	return oklab{
		L: o.L + (t.L-o.L)*k,
		A: o.A + (t.A-o.A)*k,
		B: o.B + (t.B-o.B)*k,
	}
}

// oklabPolar builds a colour from OKLCH coordinates: lightness, chroma and a
// hue angle in radians.
func oklabPolar(l, chroma, hue float64) oklab {
	return oklab{L: l, A: chroma * math.Cos(hue), B: chroma * math.Sin(hue)}
}
