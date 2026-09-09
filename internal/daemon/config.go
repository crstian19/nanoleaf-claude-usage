// Package daemon wires the pieces together and runs the render loop.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/limits"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Environment variables the daemon is configured through. Secrets come from
// the environment rather than flags so they never show up in ps output.
const (
	EnvNanoleafHost  = "NANOCLAUDE_NANOLEAF_HOST"
	EnvNanoleafToken = "NANOCLAUDE_NANOLEAF_TOKEN"
	EnvHassServer    = "HASS_SERVER"
	EnvHassToken     = "HASS_TOKEN"
	EnvToggleEntity  = "NANOCLAUDE_TOGGLE_ENTITY"
	EnvCeilingCost   = "NANOCLAUDE_CEILING_COST"
	EnvLimits        = "NANOCLAUDE_LIMITS"
	EnvLimitsEvery   = "NANOCLAUDE_LIMITS_EVERY"
	EnvLimitsCache   = "NANOCLAUDE_LIMITS_CACHE"
	EnvIdleExit      = "NANOCLAUDE_IDLE_EXIT"
	EnvFPS           = "NANOCLAUDE_FPS"
	EnvRotation      = "NANOCLAUDE_ROTATION"
)

// Defaults chosen for a wall display watched out of the corner of an eye.
const (
	// DefaultFPS is a compromise: fast enough that a travelling pulse
	// looks continuous, slow enough that the panels' own transition
	// smoothing keeps up and the network stays quiet.
	DefaultFPS = 20

	// DefaultToggleEntity is the Home Assistant switch that arms the
	// display.
	DefaultToggleEntity = "input_boolean.claude_display"

	// DefaultIdleExit is how long the daemon stays up with no session.
	//
	// The daemon is started by a Claude Code hook, so standing down when
	// there is nothing to show keeps it from being a permanent background
	// process. Generous enough that a pause for coffee does not make the
	// wall go dark and take the panels back.
	DefaultIdleExit = 20 * time.Minute

	// DefaultLimitsEvery is how often the real usage endpoint is read.
	//
	// Generous on purpose. That endpoint is severely rate limited -- two
	// probes in quick succession earned a 26-minute Retry-After -- and the
	// number it returns moves over hours, so there is nothing to gain by
	// asking often and an outage to lose by asking too much.
	DefaultLimitsEvery = 15 * time.Minute

	// MinLimitsEvery is the floor on that interval, so a misconfiguration
	// cannot turn the daemon into something that hammers the endpoint.
	MinLimitsEvery = 5 * time.Minute

	// cacheInterval is how often the status line's cache is re-read. It is
	// a small local file, so this only has to beat the rate at which it is
	// rewritten.
	cacheInterval = 30 * time.Second

	// cacheStaleAfter bounds how old a cached reading may be.
	//
	// Generous, because a stale cache mostly means Claude Code is not
	// running -- which is also when usage is not growing, so there is
	// nothing being missed. While it is running, the file is never more
	// than a minute old.
	cacheStaleAfter = 15 * time.Minute

	// usageInterval is how often ccusage is consulted.
	//
	// Once a minute, not once every fifteen seconds. ccusage is a Node
	// process that parses tens of megabytes of transcripts, and spawning
	// it four times a minute cost around 2% of a core continuously and
	// pushed the service's cgroup peak past 200MB. Since the real usage
	// endpoint became the anchor, all this has to do is supply the cost
	// delta between anchors -- and the level moves a fraction of a percent
	// per minute, well under the width of one panel's band.
	usageInterval = time.Minute

	// toggleInterval is how often the Home Assistant switch is polled.
	toggleInterval = 5 * time.Second

	// activityInterval is how often hook state files are read. Fast enough
	// that the pulse starts with the tool call, slow enough to keep
	// filesystem I/O off the render loop.
	activityInterval = 200 * time.Millisecond

	// limitsThrottleMargin is added to a Retry-After before trying again,
	// so the daemon does not return the instant the window opens.
	limitsThrottleMargin = 30 * time.Second

	// limitsMaxBackoff caps the geometric backoff for a persistently
	// broken endpoint.
	limitsMaxBackoff = 2 * time.Hour

	// sweepInterval is how often stale session files are removed.
	sweepInterval = 2 * time.Minute

	// levelTau controls how quickly the rendered fill chases a new
	// reading. Usage arrives in 15s steps; easing turns each step into a
	// visible rise instead of a jump.
	levelTau = 2500 * time.Millisecond
)

// LimitsSource selects where real usage readings come from.
type LimitsSource string

const (
	// SourceCache reads the usage a status line has already fetched.
	//
	// The default, and it should stay the default. claude-pulse polls the
	// usage endpoint every 60s because it is a status line; reading its
	// cache costs nothing, needs no credentials, cannot get the account
	// rate limited, and is fresher than polite direct polling could be.
	SourceCache LimitsSource = "cache"

	// SourceAPI queries the usage endpoint directly. Opt-in: it needs the
	// OAuth token and shares a tight rate limit with whatever else is
	// already asking.
	SourceAPI LimitsSource = "api"

	// SourceOff estimates from local cost alone and never leaves the
	// machine except to reach the panels.
	SourceOff LimitsSource = "off"
)

var (
	// ErrNoToken is returned when a host is configured but no token is.
	ErrNoToken = errors.New("daemon: no Nanoleaf token configured (run `nanoclaude pair`)")

	// ErrNoDevice is returned when nothing at all is configured, which is
	// a normal state for commands that can work without a device.
	ErrNoDevice = errors.New("daemon: no Nanoleaf device configured")
)

// LeafFromEnv builds a Nanoleaf client from the environment.
//
// It exists so the subcommands that talk to the device without running the
// loop do not each re-implement a slice of ConfigFromEnv's validation. A
// completely unconfigured environment yields ErrNoDevice, which a caller may
// treat as "no device available" rather than a failure; a half-configured one
// is a real error, so a mistyped variable is never mistaken for absence.
func LeafFromEnv() (*nanoleaf.Client, error) {
	host := os.Getenv(EnvNanoleafHost)
	token := os.Getenv(EnvNanoleafToken)

	switch {
	case host == "" && token == "":
		return nil, ErrNoDevice
	case host == "":
		return nil, fmt.Errorf("daemon: %s is required", EnvNanoleafHost)
	case token == "":
		return nil, ErrNoToken
	}
	return nanoleaf.New(host, token), nil
}

// Config is a validated daemon configuration.
type Config struct {
	NanoleafHost  string
	NanoleafToken string

	HassServer   string
	HassToken    string
	ToggleEntity string

	// CeilingCost is what a full session costs, in dollars. Zero means
	// work it out: from the real usage endpoint if it can be read, from
	// this machine's history otherwise.
	CeilingCost float64

	// LimitsSource says where the account's real usage comes from.
	LimitsSource LimitsSource
	// LimitsCache is the status line cache to read in SourceCache mode.
	LimitsCache string
	// LimitsEvery is how often the real usage is read. Only meaningful in
	// SourceAPI mode; the cache is local and read far more often.
	LimitsEvery time.Duration

	FPS int

	// IdleExit is how long to keep running with no live Claude Code
	// session before standing down. Zero keeps the daemon up for good.
	IdleExit time.Duration

	// Rotation is added to the layout's own global orientation, in degrees,
	// for installations where the device's idea of "up" does not match the
	// wall.
	Rotation int
}

// FrameInterval is how long one frame lasts.
func (c Config) FrameInterval() time.Duration {
	return time.Second / time.Duration(c.FPS)
}

// UsesHomeAssistant reports whether the display is gated on a Home Assistant
// switch. Without one the daemon simply runs whenever it is started.
func (c Config) UsesHomeAssistant() bool {
	return c.HassServer != "" && c.HassToken != ""
}

// ConfigFromEnv builds a configuration from the environment, after loading
// the daemon's config file into it.
func ConfigFromEnv() (Config, error) {
	path, err := ConfigFilePath()
	if err != nil {
		return Config{}, err
	}
	if err := LoadConfigFile(path); err != nil {
		return Config{}, err
	}
	return configFromEnvOnly()
}

// configFromEnvOnly reads the environment as it stands, without touching the
// config file. Split out so tests can exercise the parsing without a file on
// disk interfering.
func configFromEnvOnly() (Config, error) {
	cfg := Config{
		NanoleafHost:  os.Getenv(EnvNanoleafHost),
		NanoleafToken: os.Getenv(EnvNanoleafToken),
		HassServer:    os.Getenv(EnvHassServer),
		HassToken:     os.Getenv(EnvHassToken),
		ToggleEntity:  os.Getenv(EnvToggleEntity),
		FPS:           DefaultFPS,
	}

	if cfg.NanoleafHost == "" {
		return Config{}, fmt.Errorf("daemon: %s is required", EnvNanoleafHost)
	}
	if cfg.NanoleafToken == "" {
		return Config{}, ErrNoToken
	}
	if cfg.ToggleEntity == "" {
		cfg.ToggleEntity = DefaultToggleEntity
	}

	// A half-configured Home Assistant is a misconfiguration, not a
	// reason to silently ignore the toggle and light up anyway.
	if (cfg.HassServer == "") != (cfg.HassToken == "") {
		return Config{}, fmt.Errorf("daemon: set both %s and %s, or neither", EnvHassServer, EnvHassToken)
	}

	if raw := os.Getenv(EnvCeilingCost); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v <= 0 {
			return Config{}, fmt.Errorf("daemon: %s must be a positive number of dollars, got %q", EnvCeilingCost, raw)
		}
		cfg.CeilingCost = v
	}

	cfg.LimitsSource = SourceCache
	if raw := os.Getenv(EnvLimits); raw != "" {
		switch LimitsSource(raw) {
		case SourceCache, SourceAPI, SourceOff:
			cfg.LimitsSource = LimitsSource(raw)
		default:
			return Config{}, fmt.Errorf("daemon: %s must be %s, %s or %s, got %q",
				EnvLimits, SourceCache, SourceAPI, SourceOff, raw)
		}
	}

	cfg.LimitsCache = os.Getenv(EnvLimitsCache)
	if cfg.LimitsCache == "" {
		path, err := limits.CachePath()
		if err != nil {
			return Config{}, err
		}
		cfg.LimitsCache = path
	}

	cfg.IdleExit = DefaultIdleExit
	if raw := os.Getenv(EnvIdleExit); raw != "" {
		// "0" disables it, for a daemon meant to stay up.
		v, err := time.ParseDuration(raw)
		if err != nil || v < 0 {
			return Config{}, fmt.Errorf("daemon: %s must be a duration or 0, got %q", EnvIdleExit, raw)
		}
		cfg.IdleExit = v
	}

	cfg.LimitsEvery = DefaultLimitsEvery
	if raw := os.Getenv(EnvLimitsEvery); raw != "" {
		v, err := time.ParseDuration(raw)
		if err != nil || v < MinLimitsEvery {
			return Config{}, fmt.Errorf("daemon: %s must be a duration of at least %s, got %q",
				EnvLimitsEvery, MinLimitsEvery, raw)
		}
		cfg.LimitsEvery = v
	}

	if raw := os.Getenv(EnvRotation); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("daemon: %s must be a whole number of degrees, got %q", EnvRotation, raw)
		}
		cfg.Rotation = v
	}

	if raw := os.Getenv(EnvFPS); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 60 {
			return Config{}, fmt.Errorf("daemon: %s must be between 1 and 60, got %q", EnvFPS, raw)
		}
		cfg.FPS = v
	}

	return cfg, nil
}
