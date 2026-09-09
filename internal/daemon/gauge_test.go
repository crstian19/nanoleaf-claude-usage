package daemon

import (
	"math"
	"testing"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/limits"
	"github.com/crstian19/nanoleaf-claude-usage/internal/usage"
)

func TestGaugeFallsBackToCost(t *testing.T) {
	var g gauge
	g.setFallbackCeiling(200)

	w := usage.Window{CostUSD: 50}
	got := g.level(w, true, time.Now())

	if math.Abs(got.Used-0.25) > 1e-9 {
		t.Errorf("used = %v, want 0.25", got.Used)
	}
	// The display must not claim a historical guess is the real number.
	if got.Exact {
		t.Error("a historical estimate was reported as exact")
	}
}

func TestGaugeWithoutCeilingOrWindow(t *testing.T) {
	var g gauge
	// No ceiling yet: nothing can be said, and dividing by zero is not an
	// acceptable way to say it.
	if got := g.level(usage.Window{CostUSD: 10}, true, time.Now()); got.Used != 0 {
		t.Errorf("used = %v with no ceiling, want 0", got.Used)
	}

	g.setFallbackCeiling(100)
	if got := g.level(usage.Window{CostUSD: 10}, false, time.Now()); got.Used != 0 {
		t.Errorf("used = %v with no active window, want 0", got.Used)
	}
}

// TestGaugeAnchorDerivesCeiling is the point of anchoring: a real reading
// teaches the daemon what a full session costs on this account, including a
// temporary limit boost that history could never account for.
func TestGaugeAnchorDerivesCeiling(t *testing.T) {
	var g gauge
	g.setFallbackCeiling(200) // historical guess, deliberately wrong

	now := time.Now()
	g.anchor(limits.Snapshot{
		SessionUtilization: 0.30,
		SessionResetsAt:    now.Add(2 * time.Hour),
		At:                 now,
	}, 75)

	// 75 dollars was 30% of the allowance, so a full session is 250.
	if math.Abs(g.costCeiling-250) > 1e-9 {
		t.Errorf("derived ceiling = %v, want 250", g.costCeiling)
	}
	if !g.exact {
		t.Error("ceiling from a real reading is not marked exact")
	}

	// Immediately after anchoring, the level is the real number.
	got := g.level(usage.Window{CostUSD: 75}, true, now)
	if math.Abs(got.Used-0.30) > 1e-9 {
		t.Errorf("used = %v right after anchoring, want 0.30", got.Used)
	}
	if !got.Exact {
		t.Error("an anchored reading is not reported as exact")
	}
}

// TestGaugeInterpolatesBetweenAnchors covers what carries the display between
// polls: the endpoint can only be read every several minutes, so local cost
// has to move the level in the meantime.
func TestGaugeInterpolatesBetweenAnchors(t *testing.T) {
	var g gauge
	now := time.Now()
	g.anchor(limits.Snapshot{
		SessionUtilization: 0.30,
		SessionResetsAt:    now.Add(2 * time.Hour),
	}, 75)

	// 25 dollars later, on a 250-dollar allowance, is another 10%.
	got := g.level(usage.Window{CostUSD: 100}, true, now.Add(time.Minute))
	if math.Abs(got.Used-0.40) > 1e-9 {
		t.Errorf("used = %v after 25 more dollars, want 0.40", got.Used)
	}
}

// TestGaugeIgnoresBackwardsCost covers the local five-hour block rolling over
// while an anchor is live: cost restarts from zero, which is not the session
// giving budget back.
func TestGaugeIgnoresBackwardsCost(t *testing.T) {
	var g gauge
	now := time.Now()
	g.anchor(limits.Snapshot{
		SessionUtilization: 0.50,
		SessionResetsAt:    now.Add(time.Hour),
	}, 80)

	got := g.level(usage.Window{CostUSD: 3}, true, now.Add(time.Minute))
	if got.Used < 0.50 {
		t.Errorf("used = %v after the local block rolled over, want at least 0.50", got.Used)
	}
}

// TestGaugeAnchorExpires checks the anchor is dropped once the session it
// described has reset, instead of carrying a stale offset forward forever.
func TestGaugeAnchorExpires(t *testing.T) {
	var g gauge
	g.setFallbackCeiling(200)

	now := time.Now()
	g.anchor(limits.Snapshot{
		SessionUtilization: 0.80,
		SessionResetsAt:    now.Add(time.Minute),
	}, 100)

	// Before the reset the anchor rules.
	if got := g.level(usage.Window{CostUSD: 100}, true, now); !got.Exact {
		t.Error("anchor not used before its reset time")
	}
	// After it, the anchor is meaningless.
	got := g.level(usage.Window{CostUSD: 100}, true, now.Add(2*time.Minute))
	if got.Exact {
		t.Error("a stale anchor was still reported as exact")
	}
}

// TestGaugeRejectsNoisyAnchor guards the division that derives the ceiling: a
// tiny utilization makes almost any ceiling consistent with the data.
func TestGaugeRejectsNoisyAnchor(t *testing.T) {
	var g gauge
	g.setFallbackCeiling(200)

	g.anchor(limits.Snapshot{SessionUtilization: 0.02}, 5)

	if g.exact {
		t.Error("a 2% reading was used to derive a ceiling")
	}
	if g.costCeiling != 200 {
		t.Errorf("ceiling = %v, want the historical 200 to be kept", g.costCeiling)
	}
}

// TestGaugeProjection checks the burn rate is extrapolated to the window's
// end, since that is what triggers the overrun warning on the wall.
func TestGaugeProjection(t *testing.T) {
	var g gauge
	now := time.Now()
	g.anchor(limits.Snapshot{
		SessionUtilization: 0.50,
		SessionResetsAt:    now.Add(2 * time.Hour),
	}, 100) // implies a 200-dollar ceiling

	// 50 dollars an hour for two more hours is another 100 dollars, i.e.
	// the whole remaining allowance: heading for exactly 100%.
	w := usage.Window{CostUSD: 100, CostPerHour: 50}
	got := g.level(w, true, now)

	if math.Abs(got.Projected-1.0) > 1e-6 {
		t.Errorf("projected = %v, want 1.0", got.Projected)
	}
}

// TestGaugeProjectionWithoutData checks a missing burn rate or reset time
// yields no projection rather than a fabricated one.
func TestGaugeProjectionWithoutData(t *testing.T) {
	var g gauge
	g.setFallbackCeiling(100)

	if got := g.level(usage.Window{CostUSD: 50}, true, time.Now()); got.Projected != 0 {
		t.Errorf("projected = %v with no burn rate or window end, want 0", got.Projected)
	}
}

// TestGaugeAnchorWithoutCostBaseline covers a startup race that is easy to
// miss and very visible: the usage cache is read immediately while ccusage
// takes a moment, so the first anchor can arrive with no local cost known.
// Treating that zero as a baseline would make the next cost reading look like
// the whole window had been spent since the anchor.
func TestGaugeAnchorWithoutCostBaseline(t *testing.T) {
	var g gauge
	g.setFallbackCeiling(84)

	now := time.Now()
	g.anchor(limits.Snapshot{
		SessionUtilization: 0.33,
		SessionResetsAt:    now.Add(time.Hour),
	}, 0) // no cost reading yet

	if g.haveBaseline {
		t.Error("a zero cost reading was accepted as a baseline")
	}

	// The real reading still shows, unmoved.
	got := g.level(usage.Window{CostUSD: 86}, true, now)
	if math.Abs(got.Used-0.33) > 1e-9 {
		t.Errorf("used = %v, want the real 0.33 rather than an inflated value", got.Used)
	}
	if !got.Exact {
		t.Error("a real reading without a baseline should still count as exact")
	}

	// Once a cost reading exists, the next anchor establishes the baseline
	// and interpolation resumes.
	g.anchor(limits.Snapshot{
		SessionUtilization: 0.34,
		SessionResetsAt:    now.Add(time.Hour),
	}, 86)
	if !g.haveBaseline {
		t.Fatal("a real cost reading was not adopted as a baseline")
	}
	if got := g.level(usage.Window{CostUSD: 86}, true, now); math.Abs(got.Used-0.34) > 1e-9 {
		t.Errorf("used = %v right after re-anchoring, want 0.34", got.Used)
	}
}
