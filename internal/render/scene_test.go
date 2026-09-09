package render

import (
	"math"
	"testing"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// diagonalGeometry stands in for a real wall arrangement: nine panels in a
// diagonal band, like the shape this was built for.
func diagonalGeometry() Geometry {
	const n = 9
	ids := make([]int, n)
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i := range n {
		ids[i] = i + 1
		xs[i] = float64(i) * 100
		ys[i] = float64(i) * 100
	}
	return NewGeometry(ids, xs, ys)
}

// light is the total emitted light across channels. Deliberately not
// perceptual: these tests compare how much a panel is lit, not how bright it
// looks, and each band has its own hue.
func light(c nanoleaf.RGB) float64 {
	return float64(c.R) + float64(c.G) + float64(c.B)
}

// byOrder indexes a frame by each panel's rank from the bottom.
func byOrder(geo Geometry, frame nanoleaf.Frame) []nanoleaf.RGB {
	out := make([]nanoleaf.RGB, len(geo.Points))
	for _, p := range geo.Points {
		out[p.Order] = frame[p.PanelID]
	}
	return out
}

// TestBandsLightInOrder is the gauge property: the budget fills the shape one
// band at a time from the bottom, so the display can be read by counting.
func TestBandsLightInOrder(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)
	n := len(geo.Points)

	for lit := range n + 1 {
		// Land just inside band `lit`, so exactly that many bands are
		// fully covered.
		level := float64(lit)/float64(n) + 0.5/float64(n)
		if lit == n {
			level = 1
		}
		frame := byOrder(geo, scene.Frame(Input{Budget: level, Phase: PhaseIdle}, 0))

		for order, c := range frame {
			full := light(budgetColour(bandOf(order, n).centre).RGB())
			got := light(c)

			switch {
			case order < lit && got < full*0.98:
				t.Errorf("level %.3f: band %d should be fully lit but is at %.0f of %.0f",
					level, order, got, full)
			case order > lit && got != 0:
				t.Errorf("level %.3f: band %d is unspent and should be off, but is at %.0f",
					level, order, got)
			}
		}
	}
}

// TestUnspentBandsAreOff is the invariant that replaced a faint floor under
// them.
//
// Keeping unspent bands dimly lit so the whole scale stayed visible sounded
// right and did not survive contact with the wall: a few percent into a
// session, eight of nine panels glowing at a quarter brightness read as a
// uniform dim field with no level in it, and on a diffused panel at half
// device brightness it was barely visible at all. Brightness now means spend
// and nothing else.
func TestUnspentBandsAreOff(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	// Two percent spent: the first band barely lit, the rest dark.
	frame := byOrder(geo, scene.Frame(Input{Budget: 0.02, Phase: PhaseIdle}, 0))

	if light(frame[0]) == 0 {
		t.Error("the first band is off at 2% spent; nothing would be visible at all")
	}
	for order, c := range frame[1:] {
		if light(c) != 0 {
			t.Errorf("band %d is unspent but emitting %.0f", order+1, light(c))
		}
	}
}

// TestBandColoursAreFixed is what separates this design from colouring every
// panel by the current total: a band's hue is a property of its position on
// the scale, so it never changes. Only its brightness does.
func TestBandColoursAreFixed(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	frame := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: PhaseIdle}, 0))

	first, last := frame[0], frame[len(frame)-1]
	if first == last {
		t.Fatal("the gradient is flat: every band has the same colour")
	}
	if first.G <= first.R {
		t.Errorf("the bottom band is not green: %+v", first)
	}
	if last.R <= last.G {
		t.Errorf("the top band is not red: %+v", last)
	}

	// Every band must match the ramp at its own position, whatever the
	// level happens to be.
	for order, c := range frame {
		want := budgetColour(bandOf(order, len(frame)).centre).RGB()
		if c != want {
			t.Errorf("band %d is %+v, want its own ramp colour %+v", order, c, want)
		}
	}
}

// TestPartialBandGivesFineResolution checks the boundary panel reads
// fractionally, which is what gives the gauge resolution better than one
// ninth.
func TestPartialBandGivesFineResolution(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)
	n := len(geo.Points)

	var prev float64
	for i, frac := range []float64{0.1, 0.5, 0.9} {
		level := 3.0/float64(n) + frac/float64(n)
		frame := byOrder(geo, scene.Frame(Input{Budget: level, Phase: PhaseIdle}, 0))
		got := light(frame[3])

		if i > 0 && got <= prev {
			t.Errorf("band 3 did not brighten as the level crossed it: %.0f then %.0f", prev, got)
		}
		prev = got
	}
}

// TestRainbowOnlyWhileWorking checks the mode switch: the gradient means the
// budget, and it must not be replaced when there is nothing to signal.
func TestRainbowOnlyWhileWorking(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	idle := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: PhaseIdle}, 0))
	if idle[0].G <= idle[0].R {
		t.Errorf("idle: the bottom band is not green (%+v); the ramp was replaced", idle[0])
	}

	busy := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: PhaseTool}, 0))
	same := 0
	for i := range idle {
		if idle[i] == busy[i] {
			same++
		}
	}
	if same == len(idle) {
		t.Error("the display is identical whether Claude is working or not")
	}
}

// TestRainbowSpansTheShape checks it is a rainbow and not one colour that
// happens to change: several distinct hues have to be visible at once.
func TestRainbowSpansTheShape(t *testing.T) {
	geo := diagonalGeometry()
	frame := byOrder(geo, NewScene(geo).Frame(Input{Budget: 1, Phase: PhaseTool}, 0))

	dominants := map[string]bool{}
	for _, c := range frame {
		switch {
		case c.R >= c.G && c.R >= c.B:
			dominants["r"] = true
		case c.G >= c.R && c.G >= c.B:
			dominants["g"] = true
		default:
			dominants["b"] = true
		}
	}
	if len(dominants) < 3 {
		t.Errorf("only %d dominant channels across the shape (%v); want a full spectrum",
			len(dominants), dominants)
	}
}

// TestRainbowRotates checks the hue moves over time, and faster for a tool
// call than for thinking.
func TestRainbowRotates(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	if colourDistance(scene, geo, PhaseTool, 300*time.Millisecond) == 0 {
		t.Error("the rainbow does not rotate")
	}

	tool := colourDistance(scene, geo, PhaseTool, 200*time.Millisecond)
	think := colourDistance(scene, geo, PhaseThinking, 200*time.Millisecond)
	if tool <= think {
		t.Errorf("a tool call (%0.f) does not look busier than thinking (%0.f)", tool, think)
	}
}

// colourDistance is how far the whole frame shifts over dt.
func colourDistance(scene *Scene, geo Geometry, ph Phase, dt time.Duration) float64 {
	a := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: ph}, 0))
	b := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: ph}, dt))

	var total float64
	for i := range a {
		total += math.Abs(float64(a[i].R)-float64(b[i].R)) +
			math.Abs(float64(a[i].G)-float64(b[i].G)) +
			math.Abs(float64(a[i].B)-float64(b[i].B))
	}
	return total
}

// TestRainbowStaysInGamut protects the reason for using OKLCH at all.
// Pushing chroma too far sends some hues outside sRGB, where they get
// clipped -- and a clipped hue is not the colour it claims to be, so the
// rainbow would develop flat patches at some angles and not others.
func TestRainbowStaysInGamut(t *testing.T) {
	for deg := range 360 {
		col := rainbowAt(float64(deg)/360, 0, time.Second)

		for name, v := range map[string]float64{"R": col.R, "G": col.G, "B": col.B} {
			if v < -1e-9 || v > 1.02 {
				t.Fatalf("hue %d: channel %s is %v: chroma is outside the gamut", deg, name, v)
			}
		}
	}
}

// TestRainbowLightsEveryPanel is a requirement about the signal, not about
// the gauge. A rainbow drawn on two of nine panels does not read as a
// rainbow, so while Claude works the whole shape lights up whatever the level
// happens to be.
//
// The level is unreadable for as long as that lasts. That is the accepted
// trade: the gauge is what the display shows when there is nothing else to
// say.
func TestRainbowLightsEveryPanel(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	// Almost nothing spent, so the gauge alone would leave eight panels
	// dark.
	for _, ph := range []Phase{PhaseTool, PhaseThinking} {
		frame := byOrder(geo, scene.Frame(Input{Budget: 0.02, Phase: ph}, 0))
		for order, c := range frame {
			if light(c) == 0 {
				t.Errorf("%v: panel %d is dark under the rainbow", ph, order)
			}
		}
	}

	// And the gauge is back the moment Claude stops.
	idle := byOrder(geo, scene.Frame(Input{Budget: 0.02, Phase: PhaseIdle}, 0))
	for order, c := range idle[1:] {
		if light(c) != 0 {
			t.Errorf("idle: unspent panel %d is still lit (%.0f)", order+1, light(c))
		}
	}
}

// TestRainbowUsesTheWholeGamut is what "more saturated" was asked for. A
// single chroma for the whole hue circle has to be the smallest hue's
// maximum; following the boundary per hue frees the rest.
func TestRainbowUsesTheWholeGamut(t *testing.T) {
	// The old fixed value, kept here as the thing to beat.
	const oldFixedChroma = 0.13

	var better int
	for deg := range 360 {
		if maxChromaFor(float64(deg)*math.Pi/180) > oldFixedChroma*1.05 {
			better++
		}
	}
	if better < 300 {
		t.Errorf("only %d of 360 hues are more saturated than the old fixed chroma", better)
	}
}

// TestFramesAreFinite guards against a bad input reaching the wire.
func TestFramesAreFinite(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)
	inputs := []Input{
		{Budget: 0, Phase: PhaseIdle},
		{Budget: 2.5, Phase: PhaseTool},
		{Budget: -1, Phase: PhaseError},
		{Budget: math.Inf(1), Phase: PhaseThinking},
		{Budget: math.NaN(), Phase: PhaseTool},
	}

	for _, in := range inputs {
		frame := scene.Frame(in, 3*time.Second)
		if len(frame) != len(geo.Points) {
			t.Fatalf("rendered %d panels for %+v, want %d", len(frame), in, len(geo.Points))
		}
		for id := range frame {
			if id == 0 {
				t.Errorf("panel id 0 rendered for %+v", in)
			}
		}
	}
}

// TestEmptyGeometry checks a device with nothing renderable does not panic.
func TestEmptyGeometry(t *testing.T) {
	if frame := NewScene(Geometry{}).Frame(Input{Budget: 0.5}, 0); len(frame) != 0 {
		t.Errorf("empty geometry rendered %d panels", len(frame))
	}
}

// TestNothingMovesWhenIdle is a requirement, not an implementation detail:
// the display must be a still picture unless Claude is actually working.
//
// It replaced a projection warning that blinked the unspent bands whenever
// the burn rate pointed past the ceiling. That was on almost permanently --
// a few minutes into a heavy session the projection always overshoots -- so
// eight of nine panels pulsed at half a hertz on a wall, all day.
func TestNothingMovesWhenIdle(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	first := byOrder(geo, scene.Frame(Input{Budget: 0.35, Phase: PhaseIdle}, 0))
	for i := 1; i < 60; i++ {
		got := byOrder(geo, scene.Frame(
			Input{Budget: 0.35, Phase: PhaseIdle}, time.Duration(i)*137*time.Millisecond))

		for order := range got {
			if got[order] != first[order] {
				t.Fatalf("band %d changed while idle: %+v then %+v", order, first[order], got[order])
			}
		}
	}
}
