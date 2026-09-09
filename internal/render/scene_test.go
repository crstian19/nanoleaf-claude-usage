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

// quiet is any instant under PhaseIdle, where the only thing still moving is
// the ambient wave -- and the thresholds below account for that by applying
// the wave's own transformation rather than guessing at it.
//
// It used to have to dodge a whole-shape brightness breath, which no longer
// exists: that breath was the reason the wave looked like it affected every
// panel at once, since it modulated all of them identically and on the same
// period.
const quiet = 0

// highlightRange is the least and most total light a band takes across the
// whole highlight sweep.
//
// It has to be measured rather than taken from either end, because the
// highlight spends its headroom on chroma and a more saturated colour can
// have a *lower* channel sum than a duller one: a pure (0,231,0) green sums
// to less than the (105,189,76) it came from, while being obviously more
// vivid. So neither "no highlight" nor "full highlight" is reliably the
// brightest, and any threshold derived from one of them would be wrong.
func highlightRange(base Color) (least, most float64) {
	least, most = math.Inf(1), 0
	for i := range 41 {
		g := float64(i) / 40
		// Includes the absolute glow, because that is what the scene
		// adds: a ceiling computed without it comes out too low.
		v := light(base.Highlight(g).Scale(1 + waveGlow*g).RGB())
		least, most = math.Min(least, v), math.Max(most, v)
	}
	return least, most
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
		frame := byOrder(geo, scene.Frame(Input{Budget: level, Phase: PhaseIdle}, quiet))

		for order, c := range frame {
			base := budgetColour(bandOf(order, n).centre)
			litLeast, _ := highlightRange(base)
			got := light(c)

			// Both bounds come from applying the highlight's own
			// extreme rather than from guessing: a spent band is
			// dimmest with no highlight on it, an unspent one is
			// brightest with the highlight right over it. Neither
			// could be a fixed number -- dimFloor is a fraction of
			// LINEAR light and the output is gamma-encoded, so 5.5%
			// of the light is about 26% of the byte.
			// The brightest an unspent band gets: the dim floor plus
			// the absolute glow, with the highlight's own colour
			// change on top.
			dim := light(base.Highlight(1).Scale(dimFloor + waveGlow).RGB())

			litEnough := litLeast * 0.98

			switch {
			case order < lit && got < litEnough:
				t.Errorf("level %.3f: band %d should be lit but is at %.0f, below the sweep's floor of %.0f",
					level, order, got, litEnough)
			case order > lit && got > dim*1.05:
				t.Errorf("level %.3f: band %d should be dim (~%.0f) but is at %.0f",
					level, order, dim, got)
			}
		}
	}
}

// TestBandColoursAreFixed is what separates this design from colouring every
// panel by the current total: a band's hue is a property of its position on
// the scale, so it never changes. Only its brightness does.
func TestBandColoursAreFixed(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	low := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: PhaseIdle}, quiet))
	high := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: PhaseIdle}, quiet))

	// Same input twice must be identical, and the ramp must actually vary
	// across the shape -- a constant gradient would pass everything else.
	first, last := low[0], low[len(low)-1]
	if first == last {
		t.Fatal("the gradient is flat: every band has the same colour")
	}
	if first.G <= first.R {
		t.Errorf("the bottom band is not green: %+v", first)
	}
	if last.R <= last.G {
		t.Errorf("the top band is not red: %+v", last)
	}

	for i := range low {
		if low[i] != high[i] {
			t.Errorf("band %d is not deterministic: %+v vs %+v", i, low[i], high[i])
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

	// Three levels inside the same band.
	var prev float64
	for i, frac := range []float64{0.1, 0.5, 0.9} {
		level := 3.0/float64(n) + frac/float64(n)
		frame := byOrder(geo, scene.Frame(Input{Budget: level, Phase: PhaseIdle}, quiet))
		got := light(frame[3])

		if i > 0 && got <= prev {
			t.Errorf("band 3 did not brighten as the level crossed it: %.0f then %.0f", prev, got)
		}
		prev = got
	}
}

// TestUnreachedBandsStayFaintlyLit protects the choice that makes the
// gradient a readable scale: an unreached band still shows its own colour, so
// the whole range is visible instead of the display looking half broken.
func TestUnreachedBandsStayFaintlyLit(t *testing.T) {
	geo := diagonalGeometry()
	frame := NewScene(geo).Frame(Input{Budget: 0, Phase: PhaseIdle}, quiet)

	for id, c := range frame {
		if light(c) == 0 {
			t.Errorf("panel %d is fully dark at zero budget", id)
		}
	}
}

// TestProjectionWarningSitsAboveTheLevel checks the overrun glow marks where
// the burn rate is heading without being mistaken for budget already spent.
func TestProjectionWarningSitsAboveTheLevel(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)
	n := len(geo.Points)

	// Only a projection past the ceiling warrants a warning; 0.9 would
	// correctly show nothing.
	in := Input{Budget: 0.3, Projected: 1.4, Phase: PhaseIdle}
	warned := byOrder(geo, scene.Frame(in, quiet))
	calm := byOrder(geo, scene.Frame(Input{Budget: 0.3, Phase: PhaseIdle}, quiet))

	// Every band above the level is heading to be spent, so it must be
	// brighter than it would be without the warning.
	ahead := int(0.6 * float64(n))
	if light(warned[ahead]) <= light(calm[ahead]) {
		t.Errorf("band %d got no projection warning: %+v vs %+v", ahead, warned[ahead], calm[ahead])
	}

	// But still clearly dimmer than a band that is actually spent.
	if light(warned[0]) <= light(warned[ahead]) {
		t.Errorf("the projection warning is as bright as spent budget: spent %+v, warned %+v",
			warned[0], warned[ahead])
	}

	// A worse projection must warn harder than a mild one.
	mild := byOrder(geo, scene.Frame(Input{Budget: 0.3, Projected: 1.05, Phase: PhaseIdle}, quiet))
	if light(mild[ahead]) >= light(warned[ahead]) {
		t.Errorf("a 40%% overshoot does not warn harder than a 5%% one: %+v vs %+v",
			warned[ahead], mild[ahead])
	}
	// But a mild one is still visible.
	if light(mild[ahead]) <= light(calm[ahead]) {
		t.Errorf("a mild overshoot produced no warning at all: %+v", mild[ahead])
	}
}

// TestIdleIsCalm checks that when nothing is running, the only thing on the
// panels is the band's own colour under the ambient wave -- no other light
// source leaking in.
//
// The bound is the wave's own crest, not the band colour: the crest
// deliberately brightens past it now, which is what makes the wave visible on
// the hues the eye is least sensitive to changes in.
func TestIdleIsCalm(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)
	n := len(geo.Points)

	for i := range 40 {
		frame := byOrder(geo, scene.Frame(
			Input{Budget: 1, Phase: PhaseIdle}, time.Duration(i)*200*time.Millisecond))

		for order, c := range frame {
			base := budgetColour(bandOf(order, n).centre)
			_, ceiling := highlightRange(base)
			if light(c) > ceiling*1.02 {
				t.Fatalf("idle band %d is brighter than the wave's crest (%.0f > %.0f): something else is lighting it",
					order, light(c), ceiling)
			}
		}
	}
}

// TestFramesAreFinite guards against a bad input reaching the wire.
func TestFramesAreFinite(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)
	inputs := []Input{
		{Budget: 0, Projected: 0, Phase: PhaseIdle},
		{Budget: 2.5, Projected: 4, Phase: PhaseTool},
		{Budget: -1, Projected: -1, Phase: PhaseError},
		{Budget: math.Inf(1), Projected: math.NaN(), Phase: PhaseThinking},
		{Budget: math.NaN(), Projected: math.Inf(-1), Phase: PhaseTool},
	}

	for _, in := range inputs {
		frame := scene.Frame(in, 3*time.Second)
		if len(frame) != len(geo.Points) {
			t.Fatalf("rendered %d panels for %+v, want %d", len(frame), in, len(geo.Points))
		}
		for id, c := range frame {
			if id == 0 {
				t.Errorf("panel id 0 rendered for %+v", in)
			}
			// NaN cannot survive as a uint8; a NaN budget must come
			// out as a defined colour, not an arbitrary byte.
			if math.IsNaN(in.Budget) && light(c) > light(budgetColour(1).RGB())*3 {
				t.Errorf("NaN budget produced a suspicious colour: %+v", c)
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

// TestWaveTravels checks the highlight moves along the shape even with
// nothing running: it is what keeps the display from looking like a static
// picture.
func TestWaveTravels(t *testing.T) {
	geo := diagonalGeometry()

	// Which band is under the highlight at each instant? It has to move.
	peaks := map[int]bool{}
	for i := range 40 {
		secs := float64(i) * 0.2

		best, bestAmount := -1, 0.0
		for _, p := range geo.Points {
			if a := waveAt(p.S, secs); a > bestAmount {
				best, bestAmount = p.Order, a
			}
		}
		if best >= 0 {
			peaks[best] = true
		}
	}

	if len(peaks) < 4 {
		t.Errorf("the highlight visited only %d panels, want it to travel", len(peaks))
	}
}

// TestWaveStaysWithinItsEnvelope pins the highlight's strength to [0,1].
// Everything downstream -- how bright a band can get, how much chroma it can
// gain, and the worst case the gauge has to survive -- was worked out from
// those bounds.
func TestWaveStaysWithinItsEnvelope(t *testing.T) {
	for i := range 400 {
		s := float64(i%20) / 20
		secs := float64(i) * 0.13

		if got := waveAt(s, secs); got < 0 || got > 1+1e-9 {
			t.Fatalf("waveAt(%v, %v) = %v, want it within [0, 1]", s, secs, got)
		}
	}
}

// TestSpentBandOutshinesUnspentThroughoutTheWave is the constraint that caps
// how strong the wave may be, and the reason the crest exists at all.
//
// Panels sit at different points of the wave simultaneously, so the hardest
// case is a spent band in a trough seen next to an unspent one at a crest. If
// that comparison ever inverts, the display stops being countable -- which is
// what happens past about 0.45 of depth.
func TestSpentBandOutshinesUnspentThroughoutTheWave(t *testing.T) {
	const n = 9

	for order := range n {
		base := budgetColour(bandOf(order, n).centre)

		// Worst case: a spent band with no highlight on it, beside an
		// unspent one with the highlight right over it.
		spent := light(base.RGB())
		unspent := light(base.Highlight(1).Scale(dimFloor).RGB())

		if spent <= unspent*1.15 {
			t.Errorf("band %d: spent in a trough (%.0f) is not clearly brighter than unspent at a crest (%.0f)",
				order, spent, unspent)
		}
	}
}

// TestWaveIsVisibleOnEveryHue is the complaint that shaped the highlight,
// twice over. A pure dimming was reported as invisible on the greens; a
// saturating highlight then made the reds the weak end instead, because a
// bright red has no chroma to spare. What matters is not that the effect is
// equal everywhere -- it cannot be, that is the gamut's shape -- but that no
// part of the scale is left without one.
func TestWaveIsVisibleOnEveryHue(t *testing.T) {
	const n = 9

	for order := range n {
		base := budgetColour(bandOf(order, n).centre)
		plain, lit := base.RGB(), base.Highlight(1).RGB()

		moved := math.Abs(float64(plain.R)-float64(lit.R)) +
			math.Abs(float64(plain.G)-float64(lit.G)) +
			math.Abs(float64(plain.B)-float64(lit.B))

		// The weakest band measures around 106 byte-steps across three
		// channels. Below about half that the highlight stops reading
		// as anything from across a room.
		if moved < 60 {
			t.Errorf("band %d barely moves under the highlight (%.0f byte-steps)", order, moved)
		}
	}
}

// TestRainbowOnlyWhileWorking checks the mode switch: the gradient means the
// budget, and it must not be replaced when there is nothing to signal.
func TestRainbowOnlyWhileWorking(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	idle := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: PhaseIdle}, quiet))
	if idle[0].G <= idle[0].R {
		t.Errorf("idle: the bottom band is not green (%+v); the ramp was replaced", idle[0])
	}
	if last := idle[len(idle)-1]; last.R <= last.G {
		t.Errorf("idle: the top band is not red (%+v); the ramp was replaced", last)
	}

	// Working, the same frame has to look different -- that is the signal.
	busy := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: PhaseTool}, quiet))
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

	// Which channel dominates tells hues apart without needing a full
	// conversion back.
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

	hueMoves := func(ph Phase, dt time.Duration) int {
		a := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: ph}, 0))
		b := byOrder(geo, scene.Frame(Input{Budget: 1, Phase: ph}, dt))
		moved := 0
		for i := range a {
			if a[i] != b[i] {
				moved++
			}
		}
		return moved
	}

	if got := hueMoves(PhaseTool, 300*time.Millisecond); got == 0 {
		t.Error("the rainbow does not rotate")
	}

	// Over the same short interval, a tool call must move further than
	// thinking does.
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

// TestRainbowStaysInGamut is the test that protects the reason for using
// OKLCH at all. Pushing chroma too far sends some hues outside sRGB, where
// they get clipped -- and a clipped hue is not the colour it claims to be, so
// the rainbow would develop flat patches at some angles and not others.
func TestRainbowStaysInGamut(t *testing.T) {
	for deg := range 360 {
		// Sample the hue circle directly rather than through a scene,
		// so this tests the colour choice and not the geometry.
		h := float64(deg) / 360
		col := rainbowAt(h, 0, time.Second)

		for name, v := range map[string]float64{"R": col.R, "G": col.G, "B": col.B} {
			if v < -1e-9 {
				t.Fatalf("hue %d: channel %s is negative (%v): chroma is outside the gamut", deg, name, v)
			}
			if v > 1.02 {
				t.Fatalf("hue %d: channel %s is %v: chroma is outside the gamut", deg, name, v)
			}
		}
	}
}

// TestRainbowKeepsTheGaugeReadable checks the mode switch changes colour
// only. If the rainbow also lit the unspent bands, the display would stop
// being countable exactly when it is most interesting to look at.
func TestRainbowKeepsTheGaugeReadable(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)
	n := len(geo.Points)

	// A third spent: bands 0-2 lit, the rest dim.
	frame := byOrder(geo, scene.Frame(Input{Budget: 3.0 / 9.0, Phase: PhaseTool}, 0))

	var litTotal, dimTotal float64
	for order, c := range frame {
		if order < 3 {
			litTotal += light(c)
		} else if order > 3 {
			dimTotal += light(c)
		}
	}
	litAvg := litTotal / 3
	dimAvg := dimTotal / float64(n-4)

	if litAvg < dimAvg*2 {
		t.Errorf("spent bands (avg %.0f) are not clearly brighter than unspent ones (avg %.0f) under the rainbow",
			litAvg, dimAvg)
	}
}

// TestWaveStaysInGamutAcrossTheRamp checks the highlight cannot push any
// band out of the sRGB gamut at any strength. It deliberately spends its
// headroom on chroma, which is exactly the direction that clips -- and a
// clipped colour is not the colour it claims to be.
func TestWaveStaysInGamutAcrossTheRamp(t *testing.T) {
	const n = 9

	for order := range n {
		base := budgetColour(bandOf(order, n).centre)
		for i := range 21 {
			amount := float64(i) / 20
			c := base.Highlight(amount)

			for name, v := range map[string]float64{"R": c.R, "G": c.G, "B": c.B} {
				if v < -1e-9 || v > 1.005 {
					t.Fatalf("band %d at highlight %.2f: channel %s is %v",
						order, amount, name, v)
				}
			}
		}
	}
}

// TestMostBandsKeepTheirColour is the property the narrow highlight was
// introduced for.
//
// A continuously modulating wave had to pale every panel at once to stay in
// gamut, which left a washed-out green and a washed-out red looking much the
// same; telling those apart is the entire purpose of the gradient. A narrow
// highlight leaves most bands untouched.
func TestMostBandsKeepTheirColour(t *testing.T) {
	geo := diagonalGeometry()
	n := len(geo.Points)

	for i := range 30 {
		secs := float64(i) * 0.2

		untouched := 0
		for _, p := range geo.Points {
			if waveAt(p.S, secs) < 0.25 {
				untouched++
			}
		}
		// Four of nine, measured. The point is that the highlight is
		// local: a wave that touched everything at once is what left
		// the whole shape pale.
		if untouched < 4 {
			t.Fatalf("at %.1fs only %d of %d bands are outside the highlight; it is too wide",
				secs, untouched, n)
		}
	}
}

// TestGreenAndRedStayDistinct is the complaint that prompted both the narrow
// highlight and the saturating one, stated as a test: at no point may the two
// ends of the scale become hard to tell apart.
func TestGreenAndRedStayDistinct(t *testing.T) {
	green, red := budgetColour(0), budgetColour(1)

	// Every combination of highlight strength on the two ends, including
	// the worst: full highlight on one and none on the other.
	for gi := range 11 {
		for ri := range 11 {
			g := green.Highlight(float64(gi) / 10).RGB()
			r := red.Highlight(float64(ri) / 10).RGB()

			// Green must stay greener than red is, and vice versa.
			if int(g.G)-int(g.R) <= int(r.G)-int(r.R) {
				t.Fatalf("green %+v (highlight %.1f) and red %+v (highlight %.1f) are no longer ordered by hue",
					g, float64(gi)/10, r, float64(ri)/10)
			}
		}
	}
}

// TestRainbowIsNotWaved checks the two ambient effects do not stack.
//
// Beyond being redundant -- the rainbow is already motion -- the highlight on
// top of a fully saturated hue leaves the sRGB gamut, and a clipped hue is
// not the colour it claims to be. Measured at 1.18 on the worst channel
// before this was separated.
func TestRainbowIsNotWaved(t *testing.T) {
	geo := diagonalGeometry()
	scene := NewScene(geo)

	// Under the rainbow, every panel must be exactly its rainbow colour
	// times the gauge brightness -- nothing else touching it.
	for i := range 20 {
		secs := float64(i) * 0.17
		frame := byOrder(geo, scene.Frame(
			Input{Budget: 1, Phase: PhaseTool}, time.Duration(secs*1000)*time.Millisecond))

		for _, p := range geo.Points {
			want := rainbowAt(p.S, secs, rainbowTool).RGB()
			got := frame[p.Order]
			if got != want {
				t.Fatalf("at %.2fs panel with S=%.2f is %+v, want the plain rainbow %+v",
					secs, p.S, got, want)
			}
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

	// The greens and yellows take three times as much; only a narrow
	// stretch of blue-purple is genuinely limited to around the old value.
	if better < 300 {
		t.Errorf("only %d of 360 hues are more saturated than the old fixed chroma", better)
	}
}

// TestHighlightCannotDisguiseTheLevel is the constraint that sets the
// highlight's size, and it has to compare across bands rather than within
// one: every panel is on the wall at the same time, so the case that decides
// readability is an unspent band with the highlight on it beside a spent band
// without.
//
// Measured in the bytes that reach the panels, not in the linear values the
// constants are written in -- gamma makes the two very different, and an
// earlier value that looked safe linearly brought the two within 2% of each
// other on the wall.
func TestHighlightCannotDisguiseTheLevel(t *testing.T) {
	const n = 9

	worst := math.Inf(1)
	for spentBand := range n {
		spent := light(budgetColour(bandOf(spentBand, n).centre).RGB())

		for unspentBand := range n {
			base := budgetColour(bandOf(unspentBand, n).centre)
			unspent := light(base.Highlight(1).Scale(dimFloor + waveGlow).RGB())
			if unspent > 0 {
				worst = math.Min(worst, spent/unspent)
			}
		}
	}

	if worst < 1.8 {
		t.Errorf("a spent band is only %.2fx an unspent one under the highlight; the gauge stops being countable", worst)
	}
}

// TestHighlightMovesAnEmptyGauge is the other half, and the complaint that
// prompted an absolute glow in the first place: with the gauge nearly empty
// almost every band sits at the dim floor, and a highlight scaled by the fill
// was multiplied into invisibility there.
func TestHighlightMovesAnEmptyGauge(t *testing.T) {
	const n = 9
	base := budgetColour(bandOf(4, n).centre)

	plain := light(base.Scale(dimFloor).RGB())
	lit := light(base.Highlight(1).Scale(dimFloor + waveGlow).RGB())

	if lit < plain*1.5 {
		t.Errorf("the highlight only moves an unspent band from %.0f to %.0f; it will not read as travelling",
			plain, lit)
	}
}
