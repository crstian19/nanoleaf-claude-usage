package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os/exec"
	"strings"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/activity"
	"github.com/crstian19/nanoleaf-claude-usage/internal/hass"
	"github.com/crstian19/nanoleaf-claude-usage/internal/limits"
	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/internal/usage"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Daemon drives the panels from Claude Code usage.
type Daemon struct {
	cfg      Config
	leaf     *nanoleaf.Client
	hass     *hass.Client
	usage    *usage.Reader
	limits   *limits.Client
	stateDir string
	log      *slog.Logger
}

// New builds a daemon from a validated configuration.
func New(cfg Config, log *slog.Logger) (*Daemon, error) {
	reader, err := usage.NewReader()
	if err != nil {
		return nil, err
	}
	stateDir, err := activity.StateDir()
	if err != nil {
		return nil, err
	}

	d := &Daemon{
		cfg:      cfg,
		leaf:     nanoleaf.New(cfg.NanoleafHost, cfg.NanoleafToken),
		usage:    reader,
		stateDir: stateDir,
		log:      log,
	}

	if cfg.LimitsSource == SourceAPI {
		credPath, err := limits.CredentialsPath()
		if err != nil {
			return nil, err
		}
		d.limits = limits.New(credPath, claudeCodeVersion())
		log.Warn("reading the usage endpoint directly; it shares a tight rate limit with the status line, which reads it already",
			"consider", EnvLimits+"="+string(SourceCache))
	}

	if cfg.UsesHomeAssistant() {
		// A Home Assistant long-lived token is unscoped and does not
		// expire: it is full control of the instance, not read access
		// to one switch. Over plaintext it is polled out onto the
		// network every few seconds, so say so loudly rather than
		// letting a wall light quietly become the weakest link.
		if strings.HasPrefix(cfg.HassServer, "http://") {
			log.Warn("HASS_SERVER is plaintext http; the Home Assistant token is sent unencrypted on every poll and is unscoped and non-expiring. Use https.",
				"server", cfg.HassServer)
		}
		d.hass, err = hass.New(cfg.HassServer, cfg.HassToken)
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

// reading is one usage sample, or the error that prevented taking it.
type reading struct {
	window usage.Window
	err    error
}

// loop holds the render loop's mutable state.
//
// It is a type rather than a pile of local variables in Run for two reasons:
// the frame step and the arm/disarm step both own stream and saved, which is
// impossible to follow when they are closure variables; and pulling them out
// is what makes those steps testable without a device.
type loop struct {
	d     *Daemon
	scene *render.Scene

	// start anchors animation time. Elapsed time is measured from it
	// rather than from the wall clock, so animations stay continuous if
	// the clock is adjusted.
	start     time.Time
	lastFrame time.Time

	stream *nanoleaf.Streamer
	saved  nanoleaf.State
	armed  bool

	window     usage.Window
	haveWindow bool
	gauge      gauge

	// level is the eased fill, chasing the target implied by window.
	level    float64
	activity activity.Snapshot
	// exact tracks the last reported provenance, so a change of source is
	// logged once rather than every frame.
	exact bool
	// reportedSource forces that log line once at startup, so the source
	// in use never has to be inferred from the absence of a message.
	reportedSource bool

	// lastSession is when a live Claude Code session was last seen, which
	// drives the idle shutdown.
	lastSession time.Time
	idleExit    time.Duration
}

// Run renders until ctx is cancelled, then hands the panels back.
//
// It returns ErrAlreadyRunning if another daemon holds the single-instance
// lock, which is the normal outcome when a second Claude Code session starts
// and its hook tries to bring the display up again.
func (d *Daemon) Run(ctx context.Context) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	if info, err := d.leaf.Info(ctx); err != nil {
		// Not fatal: identity is diagnostic, and the layout call right
		// after this will fail properly if the device is unreachable.
		d.log.Warn("could not read device info", "err", err)
	} else {
		d.log.Info("device",
			"name", info.Name, "model", info.Model,
			"firmware", info.Firmware, "serial", info.Serial)
		if !info.StreamsV2() {
			d.log.Error("this model does not speak extControl v2; panels will stay dark and UDP will report no error",
				"model", info.Model)
		}
	}

	layout, err := d.leaf.Layout(ctx)
	if err != nil {
		return err
	}
	geo, skipped := render.FromLayout(layout, d.cfg.Rotation)
	if len(geo.Points) == 0 {
		return fmt.Errorf("daemon: device reported %d panels but none can be rendered", layout.NumPanels)
	}
	for _, p := range skipped {
		d.log.Warn("skipping panel with an out-of-range id", "panel", p.ID)
	}
	d.log.Info("layout loaded",
		"panels", len(geo.Points),
		"side_length", layout.SideLength,
		"global_orientation", layout.GlobalOrientation,
		"extra_rotation", d.cfg.Rotation,
		"host", d.cfg.NanoleafHost)

	var g gauge
	switch {
	case d.cfg.CeilingCost > 0:
		g.setFallbackCeiling(d.cfg.CeilingCost)
		d.log.Info("ceiling pinned by configuration", "ceiling_usd", d.cfg.CeilingCost)
	default:
		cost, err := d.usage.CeilingCost(ctx)
		if err != nil {
			return fmt.Errorf("calibrate ceiling: %w (set %s to override)", err, EnvCeilingCost)
		}
		g.setFallbackCeiling(cost)
		d.log.Info("provisional ceiling from history", "ceiling_usd", cost)
	}

	// Saved before anything is streamed, so the panels can be put back the
	// way they were found.
	saved, err := d.leaf.State(ctx)
	if err != nil {
		d.log.Warn("could not save device state; will power off when disarming", "err", err)
	}

	readings := make(chan reading, 1)
	toggles := make(chan bool, 1)
	activities := make(chan activity.Snapshot, 1)
	snapshots := make(chan limits.Snapshot, 1)

	go d.pollUsage(ctx, readings)
	go d.pollToggle(ctx, toggles)
	go d.pollActivity(ctx, activities)
	go d.pollLimits(ctx, snapshots)
	go d.sweepSessions(ctx)

	now := time.Now()
	l := &loop{
		d:           d,
		scene:       render.NewScene(geo),
		start:       now,
		lastFrame:   now,
		saved:       saved,
		gauge:       g,
		lastSession: now,
		idleExit:    d.cfg.IdleExit,
		// With no Home Assistant toggle configured there is nothing to
		// wait for: the daemon displays for its whole lifetime.
		armed: !d.cfg.UsesHomeAssistant(),
	}

	frames := time.NewTicker(d.cfg.FrameInterval())
	defer frames.Stop()

	// Always release the panels, whether Run returns cleanly or not.
	defer l.release()

	for {
		select {
		case <-ctx.Done():
			d.log.Info("shutting down")
			return nil
		case r := <-readings:
			l.onReading(r)
		case s := <-snapshots:
			l.onLimits(s)
		case s := <-activities:
			l.activity = s
			if s.Sessions > 0 {
				l.lastSession = time.Now()
			}
		case on := <-toggles:
			l.onToggle(on)
		case now := <-frames.C:
			if l.idleTimedOut(now) {
				d.log.Info("no Claude Code session for a while; shutting down",
					"idle_for", l.idleExit)
				return nil
			}
			l.onFrame(ctx, now)
		}
	}
}

// idleTimedOut reports whether the daemon should stand down.
//
// Exiting when nothing is running is safe because any hook event starts it
// again: the display comes back with the next prompt or tool call. Without
// that revival this would be a trap, since a session left open long enough
// for its state file to go stale would never fire SessionStart again.
func (l *loop) idleTimedOut(now time.Time) bool {
	if l.idleExit <= 0 {
		return false
	}
	return l.activity.Sessions == 0 && now.Sub(l.lastSession) > l.idleExit
}

// onReading takes a usage sample.
func (l *loop) onReading(r reading) {
	switch {
	case r.err == nil:
		l.window, l.haveWindow = r.window, true
	case isNoActiveBlock(r.err):
		l.haveWindow = false
	default:
		// Keep showing the last good reading rather than dropping to
		// zero on a transient failure.
		l.d.log.Warn("usage read failed", "err", r.err)
	}
}

// onLimits anchors the display to a real usage reading.
func (l *loop) onLimits(s limits.Snapshot) {
	// Only log an anchor that actually moved the display, or a wall light
	// writes a line to the journal every thirty seconds forever.
	changed := math.Abs(s.SessionUtilization-l.gauge.anchorUtil) > 0.005 || !l.gauge.anchored
	l.gauge.anchor(s, l.window.CostUSD)
	if !changed {
		return
	}
	l.d.log.Info("anchored to real usage",
		"session_used_pct", math.Round(s.SessionUtilization*1000)/10,
		"resets_at", s.SessionResetsAt.Format(time.RFC3339),
		"plan", s.Plan,
		"implied_ceiling_usd", math.Round(l.gauge.costCeiling*100)/100)
}

// onToggle arms or disarms the display, releasing the panels on the way down.
func (l *loop) onToggle(on bool) {
	if on == l.armed {
		return
	}
	l.armed = on
	if !l.armed {
		l.release()
		l.d.log.Info("disarmed; panels handed back")
		return
	}
	l.d.log.Info("armed")
}

// onFrame renders and sends one frame.
func (l *loop) onFrame(ctx context.Context, now time.Time) {
	dt := now.Sub(l.lastFrame)
	l.lastFrame = now

	if !l.armed {
		return
	}
	if l.stream == nil && !l.takeOver(ctx) {
		return
	}

	r := l.gauge.level(l.window, l.haveWindow, now)
	if r.Exact != l.exact || !l.reportedSource {
		l.reportedSource = true
		l.exact = r.Exact
		if r.Exact {
			l.d.log.Info("showing real usage")
		} else {
			l.d.log.Warn("real usage unavailable; showing the local cost estimate")
		}
	}
	l.level = ease(l.level, r.Used, dt)

	in := render.Input{
		Budget: l.level,
		Phase:  phase(l.activity, l.haveWindow),
	}
	if err := l.stream.Send(l.scene.Frame(in, now.Sub(l.start))); err != nil {
		// A dropped packet is normal on UDP and reports no error, so an
		// error here means a broken socket. Frame-shaped errors cannot
		// happen: unaddressable panels were filtered out of the
		// geometry at startup.
		l.d.log.Warn("frame send failed; reopening stream", "err", err)
		_ = l.stream.Close()
		l.stream = nil
	}
}

// takeOver opens the stream, reporting whether the display can render.
func (l *loop) takeOver(ctx context.Context) bool {
	// Re-read state at take-over time so a change made by hand while
	// disarmed is preserved. A device already in extControl reports a
	// pseudo-effect, which must not be allowed to overwrite the last known
	// good effect -- that is how the user's effect gets lost for good.
	if s, err := l.d.leaf.State(ctx); err == nil && s.Restorable() {
		l.saved = s
	}

	stream, err := l.d.leaf.OpenStream(ctx, l.d.cfg.FrameInterval())
	if err != nil {
		l.d.log.Error("could not open stream; retrying", "err", err)
		return false
	}
	l.stream = stream

	// Take the global brightness too. Not doing so was a real fault: every
	// colour is scaled by it, so a device left at half brightness halved a
	// display whose contrast had been calibrated byte by byte.
	if err := l.d.leaf.SetBrightness(ctx, l.d.cfg.Brightness); err != nil {
		l.d.log.Warn("could not set brightness", "err", err)
	}
	l.d.log.Info("streaming", "fps", l.d.cfg.FPS)
	return true
}

// release stops streaming and restores the device. It is safe to call when
// nothing is streaming.
func (l *loop) release() {
	if l.stream == nil {
		return
	}
	_ = l.stream.Close()
	l.stream = nil

	// A fresh context: Run's is already cancelled during shutdown, and
	// handing the panels back is exactly the work that must still happen.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Brightness first, so the restored effect appears at the level the
	// user had it rather than at whatever the display was using.
	if l.saved.Brightness > 0 {
		if err := l.d.leaf.SetBrightness(ctx, l.saved.Brightness); err != nil {
			l.d.log.Warn("could not restore brightness", "brightness", l.saved.Brightness, "err", err)
		}
	}

	if l.saved.Restorable() {
		if err := l.d.leaf.SelectEffect(ctx, l.saved.Effect); err != nil {
			l.d.log.Warn("could not restore effect", "effect", l.saved.Effect, "err", err)
		}
		if l.saved.On {
			return
		}
	}

	// Either there is nothing meaningful to restore -- leaving the panels
	// streaming a frozen frame would look broken -- or they were off to
	// begin with.
	if err := l.d.leaf.SetOn(ctx, false); err != nil {
		l.d.log.Warn("could not power panels off", "err", err)
	}
}

// claudeCodeVersion reports the installed Claude Code version, for the
// User-Agent the usage endpoint expects. An unknown version is not worth
// failing over: the request is attempted either way.
func claudeCodeVersion() string {
	// Its own short timeout: this runs during startup, and a wedged
	// `claude` process must not hold the daemon there.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--version") //nolint:gosec // fixed command, no input
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Output looks like "2.1.263 (Claude Code)".
	if fields := strings.Fields(string(out)); len(fields) > 0 {
		return fields[0]
	}
	return ""
}
