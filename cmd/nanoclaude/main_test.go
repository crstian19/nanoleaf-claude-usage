package main

import (
	"math"
	"testing"
	"time"
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
	state := debugState{from: 0.1, budget: 0.9, hold: 10 * time.Second}

	if got := state.levelAt(0); got != 0.1 {
		t.Errorf("the sweep starts at %v, want 0.1", got)
	}
	if got := state.levelAt(5 * time.Second); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("halfway through the sweep is at %v, want 0.5", got)
	}
	if got := state.levelAt(10 * time.Second); got != 0.9 {
		t.Errorf("the sweep ends at %v, want 0.9", got)
	}
	// Past the end it holds, rather than running off the scale.
	if got := state.levelAt(time.Minute); got != 0.9 {
		t.Errorf("a minute in, the sweep is at %v, want 0.9", got)
	}

	// Without --from there is nothing to sweep, and the level is the one
	// that was asked for.
	held := debugState{from: noSweep, budget: 0.42, hold: 10 * time.Second}
	if got := held.levelAt(5 * time.Second); got != 0.42 {
		t.Errorf("a held level moved to %v", got)
	}
}
