package render

import (
	"math"
	"testing"
)

// TestOklabRoundTrip guards the conversion matrices. A single transposed
// coefficient would shift every colour on the wall subtly enough to look like
// a design choice rather than a bug, so the round trip is pinned here.
func TestOklabRoundTrip(t *testing.T) {
	cases := []struct{ r, g, b uint8 }{
		{0, 0, 0},
		{255, 255, 255},
		{255, 0, 0},
		{0, 255, 0},
		{0, 0, 255},
		{0, 224, 132},
		{255, 196, 0},
		{255, 24, 24},
		{150, 240, 255},
		{17, 99, 48},
	}

	for _, c := range cases {
		in := sRGB(c.r, c.g, c.b)
		out := in.oklab().color()
		for _, d := range []struct {
			name string
			a, b float64
		}{{"R", in.R, out.R}, {"G", in.G, out.G}, {"B", in.B, out.B}} {
			// 1e-6, not tighter: the cube root and cubing lose about
			// seven digits. A transposed coefficient would be off by
			// orders of magnitude, so this still catches one.
			if math.Abs(d.a-d.b) > 1e-6 {
				t.Errorf("rgb(%d,%d,%d) channel %s: %v -> %v", c.r, c.g, c.b, d.name, d.a, d.b)
			}
		}
	}
}

// TestOklabNeutralsStayNeutral checks grey does not acquire a tint, which is
// the classic symptom of a broken matrix pair.
func TestOklabNeutralsStayNeutral(t *testing.T) {
	for _, v := range []uint8{32, 64, 128, 192} {
		got := sRGB(v, v, v).oklab().color().RGB()
		if got.R != got.G || got.G != got.B {
			t.Errorf("grey %d round-tripped to a tint: %+v", v, got)
		}
	}
}

// TestClamp01RejectsNaN pins the sanitisation that stops a NaN reaching the
// wire as an implementation-defined byte.
func TestClamp01RejectsNaN(t *testing.T) {
	if got := clamp01(math.NaN()); got != 0 {
		t.Errorf("clamp01(NaN) = %v, want 0", got)
	}
	c := Color{R: math.NaN(), G: math.NaN(), B: math.NaN()}.RGB()
	if c.R != 0 || c.G != 0 || c.B != 0 {
		t.Errorf("NaN colour = %+v, want black", c)
	}
}

// TestBudgetRampHasConstantLightness is the property the whole ramp was
// rebuilt for. Unequal lightness is what made the ambient wave look as
// though it skipped the brighter bands: taking a third of the light off
// something already glaring is far less visible than taking it off something
// dimmer.
func TestBudgetRampHasConstantLightness(t *testing.T) {
	var least, most float64 = math.Inf(1), 0
	for i := range 33 {
		o := budgetColour(float64(i) / 32).oklab()
		least, most = math.Min(least, o.L), math.Max(most, o.L)
	}

	// The hand-picked ramp this replaced spanned 0.674 to 0.843.
	if most-least > 0.005 {
		t.Errorf("lightness spans %.3f to %.3f across the ramp; want it constant", least, most)
	}
}

// TestBudgetRampSweepsGreenToRed checks the scale still reads as a scale: hue
// has to move steadily from green to red, with no detour.
func TestBudgetRampSweepsGreenToRed(t *testing.T) {
	first, last := budgetColour(0).RGB(), budgetColour(1).RGB()
	if first.G <= first.R || first.G <= first.B {
		t.Errorf("the bottom of the ramp is not green: %+v", first)
	}
	if last.R <= last.G || last.R <= last.B {
		t.Errorf("the top of the ramp is not red: %+v", last)
	}

	// Red rising relative to green, monotonically, all the way up.
	var prev float64
	for i := range 33 {
		c := budgetColour(float64(i) / 32).RGB()
		ratio := (float64(c.R) + 1) / (float64(c.G) + 1)
		if i > 0 && ratio < prev-1e-9 {
			t.Errorf("at t=%.2f the ramp turned back towards green (%.3f < %.3f)",
				float64(i)/32, ratio, prev)
		}
		prev = ratio
	}
}

// TestBudgetRampStaysInGamut protects the chroma choice: a clipped colour is
// not the colour it claims to be, so a clipped band would break both the
// constant lightness and the even wave.
func TestBudgetRampStaysInGamut(t *testing.T) {
	for i := range 65 {
		c := budgetColour(float64(i) / 64)
		for name, v := range map[string]float64{"R": c.R, "G": c.G, "B": c.B} {
			if v < -1e-9 || v > 1.005 {
				t.Fatalf("at t=%.3f channel %s is %v: chroma is outside the gamut",
					float64(i)/64, name, v)
			}
		}
	}
}
