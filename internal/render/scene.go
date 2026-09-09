package render

import (
	"math"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Phase is what Claude Code is doing right now, which drives the motion laid
// over the budget fill.
type Phase int

const (
	// PhaseIdle means nothing is running: no session, or Claude is waiting
	// on the user.
	PhaseIdle Phase = iota
	// PhaseThinking means a model response is being generated.
	PhaseThinking
	// PhaseTool means at least one tool call is in flight.
	PhaseTool
	// PhaseError means the last thing that happened was a failure or a
	// blocked permission request.
	PhaseError
)

// Input is everything a frame is computed from.
type Input struct {
	// Budget is the fraction of the five-hour window's ceiling already
	// spent. It can exceed 1 on a heavy window.
	Budget float64
	// Projected is where the current burn rate lands by the time the
	// window closes, on the same scale as Budget.
	Projected float64
	// Phase is the current activity.
	Phase Phase
}

// Visual constants. Grouped here because tuning the look means touching
// these and nothing else.
const (
	// dimFloor is the brightness of a band the budget has not reached yet.
	//
	// Not zero, because the whole point of the gradient is that it IS the
	// scale: an unreached band still shows its own colour faintly, so the
	// shape always spells out the full green-to-red range and the bright
	// part reads as progress along a visible ruler rather than as a lit
	// blob in the dark.
	dimFloor = 0.055

	// overrunGlow is how brightly a band the burn rate is heading for is
	// lit. Above dimFloor so it is clearly a warning, well below full so
	// it cannot be mistaken for budget already spent.
	overrunGlow = 0.32

	// overrunBlink is how fast the projection warning pulses, in Hz.
	overrunBlink = 0.5

	// overrunThreshold is how far past the ceiling the projection has to
	// point before it is worth showing. Slightly above 1 so a rate hovering
	// exactly at the limit does not flicker the warning on and off.
	overrunThreshold = 1.02

	// overrunFloor is the warning's strength at the moment it appears, so
	// a mild overshoot is still visible rather than fading in from nothing.
	overrunFloor = 0.45

	// overrunFullAt is how much overshoot reaches full warning strength:
	// heading for 160% of the ceiling glows as hard as it gets.
	overrunFullAt = 0.6

	// waveDepth is how much the ambient wave dims the light it passes
	// over, as a fraction.
	//
	// The wave only ever subtracts, never adds, so a fully spent band
	// still reads as fully lit at the wave's crest -- the gauge stays
	// exact and the motion is decoration on top of it, not a distortion
	// of the number.
	//
	// waveSigma is the highlight's width along the long axis.
	//
	// Narrow on purpose, and this is the whole design. A wave shaped like
	// a sine modulates every panel at every instant, and since a bright
	// highlight has to lose most of its chroma to stay in gamut, that left
	// the entire shape permanently washed out -- a pale green and a pale
	// red are hard to tell apart, which destroyed the one thing the
	// gradient is for. A narrow travelling highlight leaves most bands at
	// their exact colour and only touches two or three at a time.
	waveSigma = 0.15

	// waveLift is how much brighter the highlight makes a band, and
	// waveSaturate how far it pushes the band's chroma towards the most
	// the gamut allows at that brightness. See Color.Highlight.
	//
	// The lift is deliberately small. Light and chroma trade against each
	// other, so a strong lift forces a colour towards white -- which is
	// exactly the fault this replaces: at 0.20 with a fixed 60% chroma
	// drain, a green band went to (188,222,179) and a red one to
	// (251,195,180), near enough alike to defeat the gradient. Spending
	// the headroom on chroma instead more than doubles the green's chroma
	// and leaves the red at two thirds of its own -- and both ends of the
	// scale move visibly, which neither did before.
	//
	// Brightening cannot make an unspent band look spent: the highlight is
	// applied to the band's colour and the gauge's brightness afterwards,
	// so both are lifted by the same factor and their ratio is untouched.
	waveLift     = 0.12
	waveSaturate = 0.70

	// wavePeriod is how long the highlight takes to cross the whole shape.
	wavePeriod = 4 * time.Second
)

// The budget ramp, as an arc of constant perceived lightness in OKLCH.
//
// Only the hue changes along it, sweeping green to red. That is a correction
// of a real fault: the hand-picked sRGB ramp this replaces varied in measured
// lightness from 0.674 at the red end to 0.843 in the middle, and worse, it
// was not even monotonic -- the ambers were the brightest thing on the wall.
// Two consequences, both reported from across the room before being measured:
// the gradient did not read as an even scale, and the ambient wave appeared
// to vanish on the brighter bands, because taking 35% of the light off
// something already glaring is far less noticeable.
//
// With lightness fixed, the wave lands identically on every band and the
// gradient reads as pure hue.
//
// The price is that the top of the scale is a warm coral rather than a deep
// red: a true deep red simply has a low lightness, so it cannot appear on a
// constant-lightness arc. Evenness was worth more here than the last bit of
// menace.
const (
	// rampL is the lightness every band shares.
	rampL = 0.72
	// rampC is the chroma. The most this hue arc will take at rampL
	// without leaving the sRGB gamut, so no band gets quietly clipped.
	rampC = 0.17

	// The hue arc, in degrees. OKLab puts green near 145 and red near 29.
	rampHueStart = 145.0
	rampHueEnd   = 29.0
)

// budgetColour is the colour of a point on the scale, from 0 (nothing spent)
// to 1 (all of it).
func budgetColour(t float64) Color {
	t = clamp01(t)
	hue := (rampHueStart - t*(rampHueStart-rampHueEnd)) * math.Pi / 180
	return oklab{
		L: rampL,
		A: rampC * math.Cos(hue),
		B: rampC * math.Sin(hue),
	}.color()
}

// errorColor flashes over everything when something broke.
var errorColor = sRGB(255, 16, 32)

// Scene renders frames for one mounted arrangement.
type Scene struct {
	geo Geometry
}

// NewScene builds a renderer for the given geometry.
func NewScene(geo Geometry) *Scene { return &Scene{geo: geo} }

// Frame computes the colour of every panel at elapsed time t.
//
// The shape is a gauge of equal bands, one per panel from the bottom up. Each
// band owns a fixed slice of the budget and a fixed colour from the ramp, so
// the panels together always spell out the whole scale; the budget controls
// how brightly each band is lit, not what colour it is.
//
// That is the opposite of colouring every panel by the current total. Both
// show the same number, but this one can be read without knowing the code:
// count the bright panels. It also makes the boundary panel meaningful -- it
// is lit in proportion to how far into its own band the budget has got, which
// is what gives the display resolution finer than one ninth.
//
// t is time since the daemon started rather than wall-clock, so animations
// are continuous and do not jump when the clock is adjusted.
func (s *Scene) Frame(in Input, t time.Duration) nanoleaf.Frame {
	secs := t.Seconds()
	n := len(s.geo.Points)
	if n == 0 {
		return nanoleaf.Frame{}
	}

	level := clamp01(in.Budget)

	// The projection warning only earns space once the burn rate actually
	// points past the ceiling: heading for 90% is not news.
	//
	// Past that point every remaining band is going to be spent, so the
	// warning covers all of them and the projection's magnitude sets how
	// hard it glows instead of how far it reaches. A mild overshoot is
	// still visible; a violent one is unmistakable.
	showOverrun := in.Projected > overrunThreshold
	severity := overrunFloor + (1-overrunFloor)*clamp01((in.Projected-1)/overrunFullAt)

	rainbow := rainbowSpeed(in.Phase)
	blink := 0.5 + 0.5*math.Sin(2*math.Pi*overrunBlink*secs)

	frame := make(nanoleaf.Frame, n)
	for _, p := range s.geo.Points {
		band := bandOf(p.Order, n)

		// Each band keeps its own colour for the life of the display --
		// except while Claude is working, when the whole shape turns
		// rainbow. Only the colour changes: brightness still encodes
		// the level, so the gauge stays countable throughout.
		col := budgetColour(band.centre)
		if rainbow > 0 {
			col = rainbowAt(p.S, secs, rainbow)
		}

		brightness := dimFloor + (1-dimFloor)*band.coveredBy(level)

		// A band the current rate is heading for glows without being
		// mistaken for budget already spent.
		if showOverrun {
			ahead := 1 - band.coveredBy(level)
			brightness += overrunGlow * blink * severity * ahead
		}

		// The ambient wave says one thing only: the display is alive.
		// It is therefore pointless while the rainbow is turning, which
		// says something louder -- and stacking the two would push the
		// rainbow's saturated hues out of the gamut, clipping them into
		// colours they are not.
		//
		// Applied to the band's colour with the gauge's brightness
		// after it, so a spent band and an unspent one under the same
		// part of the highlight keep their ratio exactly.
		if rainbow == 0 {
			col = col.Highlight(waveAt(p.S, secs))
		}
		col = col.Scale(brightness)

		if in.Phase == PhaseError {
			flash := 0.5 + 0.5*math.Sin(2*math.Pi*2*secs)
			col = col.Lerp(errorColor, 0.55*flash)
		}

		frame[p.PanelID] = col.RGB()
	}
	return frame
}

// band is one panel's slice of the budget.
type band struct {
	lo, hi, centre float64
}

// bandOf returns the slice of the budget belonging to the panel at the given
// rank, out of n panels.
func bandOf(order, n int) band {
	w := 1 / float64(n)
	lo := float64(order) * w
	return band{lo: lo, hi: lo + w, centre: lo + w/2}
}

// coveredBy reports how much of the band a level reaches, in [0,1]. A partly
// covered band is what makes the boundary panel a fractional reading rather
// than a step.
func coveredBy(b band, level float64) float64 {
	return clamp01((level - b.lo) / (b.hi - b.lo))
}

func (b band) coveredBy(level float64) float64 { return coveredBy(b, level) }

// Rainbow constants. L and C are OKLCH coordinates held constant while the
// hue rotates, which is the whole point: a naive HSV rainbow varies wildly in
// perceived brightness -- yellow glares, blue sinks -- whereas this one sweeps
// hue at a steady lightness. These particular values were picked because they
// stay inside the sRGB gamut across all 360 hues, so no hue gets quietly
// clipped into a different colour.
const (
	// rainbowL is the lightness every hue shares. Lower than it could be:
	// the gamut allows far more chroma as lightness comes down, and a
	// vivid rainbow was worth more here than a bright pale one.
	rainbowL = 0.70

	// rainbowTurns is how many full hue cycles span the shape. One, so the
	// whole spectrum is visible across the arrangement at once.
	rainbowTurns = 1.0

	// The revolution time per phase. A tool call should look busier than
	// a model thinking, which is the only thing distinguishing them now
	// that the rainbow has replaced the old travelling pulse.
	rainbowThinking = 3 * time.Second
	rainbowTool     = 1400 * time.Millisecond
)

// rainbowSpeed returns the hue revolution time for a phase, or zero when the
// phase should not be rainbow at all.
func rainbowSpeed(ph Phase) time.Duration {
	switch ph {
	case PhaseThinking:
		return rainbowThinking
	case PhaseTool:
		return rainbowTool
	case PhaseIdle, PhaseError:
		return 0
	}
	return 0
}

// rainbowAt is the rainbow's colour at a point along the shape's long axis.
//
// Chroma comes from the gamut boundary for that hue rather than from a
// constant, so every hue is as saturated as it can physically be. See
// gamut.go for why a single constant had to be so much duller.
func rainbowAt(s, secs float64, period time.Duration) Color {
	h := 2 * math.Pi * (s*rainbowTurns - secs/period.Seconds())
	return oklabPolar(rainbowL, maxChromaFor(h), h).color()
}

// waveAt is how strongly the travelling highlight falls on a point of the
// shape's long axis, in [0,1].
//
// Zero away from the highlight, so the band shows its true colour -- which is
// what keeps green and red tellable apart. An earlier version modulated every
// panel continuously and left the whole shape pale.
func waveAt(s, secs float64) float64 {
	// The highlight travels the axis and wraps around.
	pos := math.Mod(secs/wavePeriod.Seconds(), 1)
	d := s - pos
	// Shortest way round, so the highlight re-enters one end as it leaves
	// the other instead of jumping back.
	switch {
	case d > 0.5:
		d--
	case d < -0.5:
		d++
	}

	return math.Exp(-(d * d) / (2 * waveSigma * waveSigma))
}
