package daemon

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/activity"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/usage"
)

func TestEase(t *testing.T) {
	// Converges towards the target without overshooting. 200 steps of 50ms
	// is 10s, i.e. four time constants, so exactly exp(-4) ~= 1.8% of the
	// distance is left -- that is the curve, not slack in the test.
	level := 0.0
	for range 200 {
		level = ease(level, 1, 50*time.Millisecond)
		if level > 1 {
			t.Fatalf("overshot the target: %v", level)
		}
	}
	want := 1 - math.Exp(-4)
	if math.Abs(level-want) > 1e-6 {
		t.Errorf("after four time constants level = %v, want %v", level, want)
	}

	// A single step must move, but not arrive.
	if got := ease(0, 1, 50*time.Millisecond); got <= 0 || got >= 1 {
		t.Errorf("one step = %v, want strictly between 0 and 1", got)
	}

	// A non-positive interval must not move or divide by zero.
	if got := ease(0.4, 1, 0); got != 0.4 {
		t.Errorf("zero dt moved the level to %v", got)
	}
	if got := ease(0.4, 1, -time.Second); got != 0.4 {
		t.Errorf("negative dt moved the level to %v", got)
	}
}

// TestPhase pins the rule that a closed usage window beats a session file.
// A killed session leaves its last event behind, and without this the display
// would keep pulsing for work that stopped.
func TestPhase(t *testing.T) {
	tests := []struct {
		name       string
		snap       activity.Snapshot
		haveWindow bool
		want       render.Phase
	}{
		{"tool running", activity.Snapshot{Phase: activity.PhaseTool, Sessions: 1}, true, render.PhaseTool},
		{"thinking", activity.Snapshot{Phase: activity.PhaseThinking, Sessions: 1}, true, render.PhaseThinking},
		{"error", activity.Snapshot{Phase: activity.PhaseError, Sessions: 1}, true, render.PhaseError},
		{"idle session", activity.Snapshot{Phase: activity.PhaseIdle, Sessions: 1}, true, render.PhaseIdle},
		{
			name:       "stale session file with no window loses",
			snap:       activity.Snapshot{Phase: activity.PhaseTool, Sessions: 0},
			haveWindow: false,
			want:       render.PhaseIdle,
		},
		{
			name:       "live session with no window still counts",
			snap:       activity.Snapshot{Phase: activity.PhaseTool, Sessions: 1},
			haveWindow: false,
			want:       render.PhaseTool,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := phase(tc.snap, tc.haveWindow); got != tc.want {
				t.Errorf("phase() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsNoActiveBlock(t *testing.T) {
	if !isNoActiveBlock(usage.ErrNoActiveBlock) {
		t.Error("the sentinel was not recognised")
	}
	if isNoActiveBlock(errors.New("something else")) {
		t.Error("an unrelated error was treated as an idle window")
	}
}

// TestIdleTimedOut covers the idle shutdown, which is what keeps a
// hook-started daemon from becoming a permanent background process.
func TestIdleTimedOut(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		sessions int
		last     time.Time
		idle     time.Duration
		want     bool
	}{
		{"a live session keeps it up", 1, now.Add(-time.Hour), 20 * time.Minute, false},
		{"no sessions but recently seen", 0, now.Add(-time.Minute), 20 * time.Minute, false},
		{"no sessions for long enough", 0, now.Add(-21 * time.Minute), 20 * time.Minute, true},
		// Zero means stay up for good, for a daemon run as a service.
		{"disabled", 0, now.Add(-24 * time.Hour), 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := &loop{
				activity:    activity.Snapshot{Sessions: tc.sessions},
				lastSession: tc.last,
				idleExit:    tc.idle,
			}
			if got := l.idleTimedOut(now); got != tc.want {
				t.Errorf("idleTimedOut() = %v, want %v", got, tc.want)
			}
		})
	}
}
