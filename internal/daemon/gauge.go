package daemon

import (
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/limits"
	"github.com/crstian19/nanoleaf-claude-usage/internal/usage"
)

// minUtilForCeiling is the smallest real reading worth deriving a cost
// ceiling from. Below it the division amplifies noise absurdly: 2% of an
// allowance divided into a few dollars implies almost any ceiling at all.
const minUtilForCeiling = 0.10

// gauge turns the available measurements into the level shown on the wall.
//
// It exists because the two sources it has are each insufficient alone. The
// real usage endpoint gives the true number but can only be asked every
// several minutes -- two probes bought a 26-minute lockout. The local cost
// meter can be read constantly but has no idea what the account's actual
// allowance is. So a real reading becomes an anchor, and local cost carries
// the level forward between anchors.
type gauge struct {
	anchored   bool
	anchorUtil float64
	anchorCost float64
	// haveBaseline records whether anchorCost is a real measurement.
	//
	// A real reading can land before the first local cost reading does --
	// the two are polled independently, and on startup the usage endpoint
	// cache is read immediately while ccusage takes a moment. Anchoring
	// with a zero baseline would make the next cost reading look like the
	// entire window's spend had happened since the anchor, which on a
	// 9-band gauge pushes the display straight to full.
	haveBaseline bool
	resetsAt     time.Time

	// costCeiling is what a full session costs. Derived from a real
	// reading when there has been one, from history otherwise.
	costCeiling float64
	// exact records whether costCeiling came from a real reading.
	exact bool
}

// setFallbackCeiling installs the historical estimate used until a real
// reading arrives.
func (g *gauge) setFallbackCeiling(cost float64) {
	if g.exact || cost <= 0 {
		return
	}
	g.costCeiling = cost
}

// anchor records a real reading, together with the local cost meter at that
// moment so later movement can be measured against it.
func (g *gauge) anchor(s limits.Snapshot, windowCost float64) {
	g.anchored = true
	g.anchorUtil = s.SessionUtilization
	g.resetsAt = s.SessionResetsAt

	g.haveBaseline = windowCost > 0
	if g.haveBaseline {
		g.anchorCost = windowCost
	}

	// The real reading also calibrates the cost ceiling, which is what
	// makes the fallback trustworthy: instead of guessing from history,
	// the daemon learns what a full session actually costs on this
	// account and plan -- including a temporary limit boost, which
	// history could never account for.
	if s.SessionUtilization >= minUtilForCeiling && windowCost > 0 {
		g.costCeiling = windowCost / s.SessionUtilization
		g.exact = true
	}
}

// stale reports whether the anchor has aged out, which happens when the
// session window it described has rolled over.
func (g *gauge) stale(now time.Time) bool {
	return !g.resetsAt.IsZero() && now.After(g.resetsAt)
}

// gaugeReading is a level plus where it came from.
type gaugeReading struct {
	Used float64
	// Exact is true when the level rests on a real reading rather than on
	// a historical guess. Only used for logging: the display looks the
	// same either way, and claiming otherwise in a log is how a fallback
	// gets mistaken for the real thing.
	Exact bool
}

// level computes what to show.
func (g *gauge) level(w usage.Window, haveWindow bool, now time.Time) gaugeReading {
	if !haveWindow || g.costCeiling <= 0 {
		return gaugeReading{}
	}

	if g.anchored && !g.stale(now) {
		// Without a cost baseline there is nothing to measure movement
		// against, so show the real reading unmoved and wait for the
		// next anchor to establish one.
		if !g.haveBaseline {
			return gaugeReading{Used: g.anchorUtil, Exact: true}
		}

		// Only the movement since the anchor is taken from local cost,
		// so a mismatch between the two windows' boundaries cannot
		// accumulate. A negative delta means the local block rolled
		// over, which is movement the anchor already accounts for.
		delta := w.CostUSD - g.anchorCost
		if delta < 0 {
			delta = 0
		}
		return gaugeReading{Used: g.anchorUtil + delta/g.costCeiling, Exact: true}
	}

	return gaugeReading{Used: w.CostUSD / g.costCeiling, Exact: false}
}
