package daemon

import (
	"errors"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(*testing.T, Config)
	}{
		{
			name: "minimum viable config",
			env:  map[string]string{EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok"},
			check: func(t *testing.T, c Config) {
				if c.FPS != DefaultFPS {
					t.Errorf("FPS = %d, want the default %d", c.FPS, DefaultFPS)
				}
				if c.ToggleEntity != DefaultToggleEntity {
					t.Errorf("toggle = %q, want the default", c.ToggleEntity)
				}
				if c.UsesHomeAssistant() {
					t.Error("Home Assistant reported as configured when it is not")
				}
			},
		},
		{
			name:    "host is required",
			env:     map[string]string{EnvNanoleafToken: "tok"},
			wantErr: true,
		},
		{
			name:    "token is required",
			env:     map[string]string{EnvNanoleafHost: "10.0.0.5"},
			wantErr: true,
		},
		{
			// Half-configured Home Assistant is a typo, not a
			// request to ignore the toggle and light up anyway.
			name: "hass server without token is rejected",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok",
				EnvHassServer: "http://ha",
			},
			wantErr: true,
		},
		{
			name: "hass token without server is rejected",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok",
				EnvHassToken: "t",
			},
			wantErr: true,
		},
		{
			name: "both hass variables are accepted",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok",
				EnvHassServer: "https://ha", EnvHassToken: "t",
			},
			check: func(t *testing.T, c Config) {
				if !c.UsesHomeAssistant() {
					t.Error("Home Assistant not reported as configured")
				}
			},
		},
		{
			name: "fps out of range is rejected",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok", EnvFPS: "0",
			},
			wantErr: true,
		},
		{
			name: "fps above the cap is rejected",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok", EnvFPS: "61",
			},
			wantErr: true,
		},
		{
			name: "non-numeric ceiling is rejected",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok", EnvCeilingCost: "lots",
			},
			wantErr: true,
		},
		{
			name: "zero ceiling is rejected rather than meaning auto",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok", EnvCeilingCost: "0",
			},
			wantErr: true,
		},
		{
			// The endpoint that gives real numbers is severely rate
			// limited, so an interval below the floor must be
			// refused rather than quietly clamped.
			name: "limits interval below the floor is rejected",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok",
				EnvLimitsEvery: "10s",
			},
			wantErr: true,
		},
		{
			name: "limits can be switched off",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok",
				EnvLimits: "off",
			},
			check: func(t *testing.T, c Config) {
				if c.LimitsSource != SourceOff {
					t.Errorf("source = %q, want %q", c.LimitsSource, SourceOff)
				}
			},
		},
		{
			name: "the api source is opt-in",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok",
				EnvLimits: "api",
			},
			check: func(t *testing.T, c Config) {
				if c.LimitsSource != SourceAPI {
					t.Errorf("source = %q, want %q", c.LimitsSource, SourceAPI)
				}
			},
		},
		{
			name: "an unknown source is rejected",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok",
				EnvLimits: "on",
			},
			wantErr: true,
		},
		{
			// The default has to stay the status line cache: it is
			// the source that costs nothing and cannot get the
			// account rate limited.
			name: "the cache is the default source",
			env:  map[string]string{EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok"},
			check: func(t *testing.T, c Config) {
				if c.LimitsSource != SourceCache {
					t.Errorf("source = %q, want %q", c.LimitsSource, SourceCache)
				}
				if c.LimitsCache == "" {
					t.Error("no default cache path was resolved")
				}
				if c.LimitsEvery != DefaultLimitsEvery {
					t.Errorf("interval = %v, want %v", c.LimitsEvery, DefaultLimitsEvery)
				}
			},
		},
		{
			name: "explicit ceiling is kept",
			env: map[string]string{
				EnvNanoleafHost: "10.0.0.5", EnvNanoleafToken: "tok", EnvCeilingCost: "1000",
			},
			check: func(t *testing.T, c Config) {
				if c.CeilingCost != 1000 {
					t.Errorf("ceiling = %v, want 1000", c.CeilingCost)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear every variable so the host's real environment
			// cannot make a case pass or fail by accident.
			for _, k := range []string{
				EnvNanoleafHost, EnvNanoleafToken, EnvHassServer,
				EnvHassToken, EnvToggleEntity, EnvCeilingCost, EnvFPS,
				EnvRotation, EnvLimits, EnvLimitsEvery, EnvLimitsCache,
				EnvIdleExit,
			} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := configFromEnvOnly()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got %+v, want an error", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConfigFromEnv: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

// TestFrameInterval checks the frame rate maps to a sane interval.
func TestFrameInterval(t *testing.T) {
	if got := (Config{FPS: 20}).FrameInterval(); got.Milliseconds() != 50 {
		t.Errorf("20fps = %v, want 50ms", got)
	}
}

// TestLeafFromEnv covers the distinction the preview command depends on:
// nothing configured is a normal state, half configured is a mistake.
//
// XDG_CONFIG_HOME points at an empty directory because LeafFromEnv loads the
// configuration file, and the machine running the tests has a real one.
func TestLeafFromEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("nothing configured", func(t *testing.T) {
		t.Setenv(EnvNanoleafHost, "")
		t.Setenv(EnvNanoleafToken, "")
		if _, err := LeafFromEnv(); !errors.Is(err, ErrNoDevice) {
			t.Errorf("err = %v, want ErrNoDevice", err)
		}
	})

	t.Run("host without token", func(t *testing.T) {
		t.Setenv(EnvNanoleafHost, "10.0.0.5")
		t.Setenv(EnvNanoleafToken, "")
		if _, err := LeafFromEnv(); !errors.Is(err, ErrNoToken) {
			t.Errorf("err = %v, want ErrNoToken", err)
		}
	})

	t.Run("fully configured", func(t *testing.T) {
		t.Setenv(EnvNanoleafHost, "10.0.0.5")
		t.Setenv(EnvNanoleafToken, "tok")
		c, err := LeafFromEnv()
		if err != nil || c == nil {
			t.Errorf("LeafFromEnv() = %v, %v", c, err)
		}
	})
}
