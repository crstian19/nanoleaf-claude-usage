package render

import (
	"math"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Color is a linear-light RGB colour with components nominally in [0,1].
//
// Blending happens in linear light rather than directly on 8-bit sRGB values:
// mixing sRGB numbers makes a half-brightness green come out muddy, and a
// fill edge crossing a panel look like a hard step instead of a fade.
type Color struct {
	R, G, B float64
}

// sRGB builds a Color from the 8-bit values a colour picker would give,
// converting them into linear light.
func sRGB(r, g, b uint8) Color {
	return Color{
		R: srgbToLinear(float64(r) / 255),
		G: srgbToLinear(float64(g) / 255),
		B: srgbToLinear(float64(b) / 255),
	}
}

// Scale multiplies every channel, i.e. changes brightness without changing hue.
func (c Color) Scale(k float64) Color {
	return Color{R: c.R * k, G: c.G * k, B: c.B * k}
}

// Add sums two colours, as overlapping light sources do.
func (c Color) Add(o Color) Color {
	return Color{R: c.R + o.R, G: c.G + o.G, B: c.B + o.B}
}

// Lerp blends towards o by t in [0,1], perceptually.
//
// The blend goes through OKLab, not linear light: see the oklab type for why.
// This is interpolation between two colours, which is a perceptual question.
// Scale and Add stay in linear light because brightness and the sum of two
// light sources are physical ones.
func (c Color) Lerp(o Color, t float64) Color {
	t = clamp01(t)
	return c.oklab().lerp(o.oklab(), t).color()
}

// RGB converts back to the 8-bit sRGB triple the panels expect.
func (c Color) RGB() nanoleaf.RGB {
	return nanoleaf.RGB{
		R: quantise(c.R),
		G: quantise(c.G),
		B: quantise(c.B),
	}
}

func quantise(v float64) uint8 {
	return uint8(math.Round(clamp01(linearToSRGB(clamp01(v))) * 255))
}

func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// clamp01 confines a value to [0,1]. Every clamp in the renderer is to that
// range: colours, fills and fractions all live there.
//
// NaN is mapped to 0 rather than propagated. Neither math.Min/math.Max nor
// the min/max builtins reject it, and a NaN that reaches the wire becomes an
// implementation-defined byte -- Go does not specify the result of an
// out-of-range float-to-int conversion. Since a degenerate layout can produce
// NaN coordinates, and Input is exported and reachable from the CLI, this is
// the one place that has to stop it.
func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Min(math.Max(v, 0), 1)
}
