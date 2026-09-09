package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/activity"
	"github.com/crstian19/nanoleaf-claude-usage/internal/limits"
)

// The pollers below all run on their own goroutines and publish onto buffered
// channels with a non-blocking send. Polling has to stay off the render loop:
// ccusage takes around 200ms, which at 20fps would stall four frames and show
// up as a visible hitch in the travelling pulse. Dropping a sample when the
// loop is behind is harmless, since the next one is seconds away.

func send[T any](ch chan<- T, v T) {
	select {
	case ch <- v:
	default:
	}
}

// pollUsage samples the active window, publishing immediately on start so the
// display does not sit empty for the first interval.
func (d *Daemon) pollUsage(ctx context.Context, out chan<- reading) {
	tick := time.NewTicker(usageInterval)
	defer tick.Stop()

	read := func() {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		w, err := d.usage.Active(callCtx)
		send(out, reading{window: w, err: err})
	}

	read()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			read()
		}
	}
}

// pollToggle watches the Home Assistant switch that arms the display. Without
// Home Assistant configured the daemon is armed for its whole lifetime, so
// there is nothing to poll.
func (d *Daemon) pollToggle(ctx context.Context, out chan<- bool) {
	if d.hass == nil {
		return
	}

	tick := time.NewTicker(toggleInterval)
	defer tick.Stop()

	// Tracks the last successfully read value so an unreachable Home
	// Assistant leaves the display as it is rather than flapping.
	read := func() {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		on, err := d.hass.IsOn(callCtx, d.cfg.ToggleEntity)
		if err != nil {
			d.log.Warn("toggle read failed", "entity", d.cfg.ToggleEntity, "err", err)
			return
		}
		send(out, on)
	}

	read()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			read()
		}
	}
}

// pollActivity reads the hook state files.
//
// This is a poller rather than a read inside the frame case for the same
// reason as the others: os.ReadDir plus a JSON decode per live session, at
// frame rate, puts blocking filesystem I/O on the render loop. The phase
// cannot meaningfully change faster than this anyway.
func (d *Daemon) pollActivity(ctx context.Context, out chan<- activity.Snapshot) {
	tick := time.NewTicker(activityInterval)
	defer tick.Stop()

	read := func(now time.Time) {
		snap, err := activity.Read(d.stateDir, now)
		if err != nil {
			d.log.Warn("activity read failed", "err", err)
			return
		}
		if snap.Tool != "" {
			d.log.Debug("activity", "phase", snap.Phase, "tool", snap.Tool, "sessions", snap.Sessions)
		}
		send(out, snap)
	}

	read(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			read(now)
		}
	}
}

// pollLimits reads the account's real usage.
//
// Which source, and therefore which pace, depends on configuration. The
// default reads a local file a status line already keeps up to date, so it
// can be read as often as is useful. Querying the endpoint directly is the
// opt-in path, and there the endpoint dictates the pace: it answers a 429
// with a Retry-After measured in tens of minutes.
func (d *Daemon) pollLimits(ctx context.Context, out chan<- limits.Snapshot) {
	switch d.cfg.LimitsSource {
	case SourceCache:
		d.pollLimitsCache(ctx, out)
	case SourceAPI:
		d.pollLimitsAPI(ctx, out)
	case SourceOff:
	}
}

// pollLimitsCache re-reads the status line's usage cache.
func (d *Daemon) pollLimitsCache(ctx context.Context, out chan<- limits.Snapshot) {
	tick := time.NewTicker(cacheInterval)
	defer tick.Stop()

	// Failures are logged once rather than every tick: a machine without a
	// status line installed would otherwise fill the journal with a
	// message about a file that is never going to appear.
	var reported bool

	read := func(now time.Time) {
		snap, err := limits.ReadCache(d.cfg.LimitsCache, cacheStaleAfter, now)
		if err != nil {
			if !reported {
				reported = true
				d.log.Warn("no usable status line usage cache; using the local estimate",
					"path", d.cfg.LimitsCache, "err", err,
					"hint", EnvLimits+"="+string(SourceAPI)+" queries the endpoint directly instead")
			}
			return
		}
		reported = false
		send(out, snap)
	}

	read(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			read(now)
		}
	}
}

// pollLimitsAPI queries the usage endpoint directly.
//
// Unlike the other pollers this one does not run on a ticker, because the
// endpoint dictates the pace: ignoring its Retry-After would lock the daemon
// out for good. So the wait is recomputed after every attempt, and the
// server's own figure always wins over the configured interval.
func (d *Daemon) pollLimitsAPI(ctx context.Context, out chan<- limits.Snapshot) {
	if d.limits == nil {
		return
	}

	// The first read happens immediately: until it lands, the display is
	// running on a historical guess.
	wait := time.Duration(0)
	backoff := d.cfg.LimitsEvery

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		snap, err := d.limits.Fetch(callCtx)
		cancel()

		var throttled *limits.Throttled
		switch {
		case err == nil:
			send(out, snap)
			wait = d.cfg.LimitsEvery
			backoff = d.cfg.LimitsEvery

		case errors.As(err, &throttled):
			// Plus a margin: coming back the very second the window
			// opens is how the next refusal is earned.
			wait = throttled.RetryAfter + limitsThrottleMargin
			backoff = d.cfg.LimitsEvery
			d.log.Warn("real usage endpoint throttled; backing off",
				"retry_after", throttled.RetryAfter, "next_attempt_in", wait)

		default:
			// Anything else -- offline, expired token, changed
			// response shape -- backs off geometrically so a
			// permanent breakage costs one request an hour rather
			// than one every quarter of an hour.
			wait = backoff
			if backoff < limitsMaxBackoff {
				backoff *= 2
			}
			d.log.Warn("could not read real usage; using the local estimate",
				"err", err, "next_attempt_in", wait)
		}
	}
}

// sweepSessions removes state files left behind by sessions that were killed
// instead of exiting.
func (d *Daemon) sweepSessions(ctx context.Context) {
	tick := time.NewTicker(sweepInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if err := activity.Sweep(d.stateDir, now); err != nil {
				d.log.Warn("session sweep failed", "err", err)
			}
		}
	}
}
