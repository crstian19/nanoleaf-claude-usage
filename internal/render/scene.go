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
	// Budget is the fraction of the session's allowance already spent. It
	// can exceed 1 on an account with overage enabled.
	Budget float64
	// Phase is the current activity.
	Phase Phase
}

// Visual constants. Grouped here because tuning the look means touching
// these and nothing else.
const (
	// activeFloor is the least brightness a band shows once any of it is
	// spent.
	//
	// Without it the lowest band fades in from nothing, so the first few
	// percent of a session are invisible: at 2% spent the only lit panel
	// would sit at 18% of its colour, which on a diffused panel is nothing
	// at all. With a floor, anything spent is unmistakably lit and the
	// fill still modulates on top -- the resolution is compressed into the
	// remaining range rather than lost.
	activeFloor = 0.35
)

// The budget ramp, as an arc of constant perceived lightness in OKLCH.
//
// Only the hue changes along it, sweeping green to red. That corrects a real
// fault in the hand-picked sRGB ramp it replaces, which varied in measured
// lightness from 0.674 at the red end to 0.843 in the middle -- and was not
// even monotonic, so the ambers were the brightest thing on the wall. The
// gradient did not read as an even scale, which was noticed from across the
// room before it was measured.
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

	rainbow := rainbowSpeed(in.Phase)

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

		// While the rainbow turns, every panel is lit. The signal is
		// the whole shape, and a rainbow drawn on two of nine panels
		// is not one. The level is unreadable for as long as Claude
		// works, which is the trade: the gauge is what the display
		// shows when there is nothing else to say.
		brightness := 1.0
		if rainbow == 0 {
			// Brightness is the spend and nothing else: an unspent
			// band is off. A floor under the unspent ones, to keep
			// the whole scale faintly visible, sounded reasonable
			// and did not survive the wall. At a few percent spent,
			// eight of nine panels glowing at a quarter brightness
			// read as a uniform dim field with no level in it, and
			// competed with the one band that mattered.
			//
			// A band with anything in it starts at activeFloor, so
			// the start of a session is visible rather than fading
			// in from black.
			brightness = 0
			if fill := band.coveredBy(level); fill > 0 {
				brightness = activeFloor + (1-activeFloor)*fill
			}
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
