package main

import (
	"math"
	"testing"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
)

// TestBuildVersionPrefersTheStampedValue covers the version a released binary
// reports, which is the one set at build time.
func TestBuildVersionPrefersTheStampedValue(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "v1.2.3"
	if got := buildVersion(); got != "v1.2.3" {
		t.Errorf("version reported as %q, want the value built in", got)
	}
}

// TestBuildVersionFallsBackToTheModule covers `go install`, which sets no
// build flags at all.
//
// A test binary records its own module version as "(devel)", so what this
// pins is the shape of the fallback rather than a released number: an
// unstamped build reports something, and never the empty string.
func TestBuildVersionFallsBackToTheModule(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "dev"
	got := buildVersion()
	if got == "" {
		t.Fatal("an unstamped build reports no version at all")
	}
	if got == "(devel)" {
		t.Errorf("version reported as %q, which says less than %q", got, "dev")
	}
}

// TestTheSweepWalksFromOneLevelToTheOther covers the shot the debug command
// exists for: the wall filling up while a camera runs. The level has to start
// where it was asked to, end where it was asked to, and never leave the scale
// in between.
func TestTheSweepWalksFromOneLevelToTheOther(t *testing.T) {
	state := debugState{from: 0.1, budget: 0.9, sweep: 10 * time.Second}

	if got := state.levelAt(0); got != 0.1 {
		t.Errorf("the sweep starts at %v, want 0.1", got)
	}
	if got := state.levelAt(5 * time.Second); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("halfway through the sweep is at %v, want 0.5", got)
	}
	if got := state.levelAt(10 * time.Second); got != 0.9 {
		t.Errorf("the sweep ends at %v, want 0.9", got)
	}
	// And it stays there. A fill that ended by stopping showed the full
	// wall for a single frame, which is no use to a camera.
	if got := state.levelAt(time.Minute); got != 0.9 {
		t.Errorf("a minute in, the wall is at %v, want it still full at 0.9", got)
	}
	if got := state.levelAt(time.Hour); got != 0.9 {
		t.Errorf("an hour in, the wall is at %v", got)
	}

	// Without --from there is nothing to sweep, and the level is the one
	// that was asked for.
	held := debugState{from: noSweep, budget: 0.42, sweep: 10 * time.Second}
	if got := held.levelAt(5 * time.Second); got != 0.42 {
		t.Errorf("a held level moved to %v", got)
	}
}

// TestTheKeyboardModeRunsTheFillItWasAskedFor is the bug this had: a terminal
// takes the keyboard path, and that path ignored --sweep and opened on a full
// wall. Only a pipe ever saw the fill, which is why the tests missed it.
func TestTheKeyboardModeRunsTheFillItWasAskedFor(t *testing.T) {
	state := debugState{from: 0, budget: 1, sweep: 6 * time.Second}
	m := newDebugModel(nil, nil, render.Geometry{}, state)

	if m.sweepEnd.IsZero() {
		t.Fatal("the keyboard mode opened with no fill running")
	}
	if got := m.level(); got > 0.05 {
		t.Errorf("the fill opens at %v, want it to start from nothing", got)
	}

	// Halfway through.
	m.sweepEnd = time.Now().Add(3 * time.Second)
	if got := m.level(); got < 0.4 || got > 0.6 {
		t.Errorf("halfway through the fill the wall is at %v, want about 0.5", got)
	}

	// And it stays full once the fill is done.
	m.sweepEnd = time.Now().Add(-time.Second)
	if got := m.level(); got != 1 {
		t.Errorf("after the fill the wall is at %v, want 1", got)
	}

	// Asked to hold one level instead, it holds it from the first frame.
	held := newDebugModel(nil, nil, render.Geometry{}, debugState{from: noSweep, budget: 0.4})
	if !held.sweepEnd.IsZero() {
		t.Error("a held state started a fill")
	}
	if got := held.level(); got != 0.4 {
		t.Errorf("a held state opens at %v, want 0.4", got)
	}
}
