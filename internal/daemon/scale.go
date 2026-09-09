package daemon

import (
	"errors"
	"math"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/activity"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/usage"
)

// The pure functions that turn measurements into render inputs. Kept apart
// from the loop so they can be tested without a device or a subprocess.

// phase decides the motion shown.
//
// With no active usage window and no live session there is nothing running,
// whatever a session file might still claim: a window closing is the more
// reliable signal, so it wins over a stale hook record.
func phase(snap activity.Snapshot, haveWindow bool) render.Phase {
	if !haveWindow && snap.Sessions == 0 {
		return render.PhaseIdle
	}
	switch snap.Phase {
	case activity.PhaseTool:
		return render.PhaseTool
	case activity.PhaseThinking:
		return render.PhaseThinking
	case activity.PhaseError:
		return render.PhaseError
	case activity.PhaseIdle:
		return render.PhaseIdle
	}
	return render.PhaseIdle
}

// ease moves current towards target on an exponential curve, so the fill
// glides between samples instead of stepping when a new one arrives.
func ease(current, target float64, dt time.Duration) float64 {
	if dt <= 0 {
		return current
	}
	k := 1 - math.Exp(-dt.Seconds()/levelTau.Seconds())
	return current + (target-current)*k
}

// isNoActiveBlock reports whether an error just means no five-hour window is
// open, which is an ordinary state and not a failure.
func isNoActiveBlock(err error) bool {
	return errors.Is(err, usage.ErrNoActiveBlock)
}
