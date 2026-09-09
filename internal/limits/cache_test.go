package limits

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCache builds a cache file in the shape claude-pulse actually writes.
// The literal below is taken from a real file, percentages and all.
func writeCache(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	return path
}

func TestReadCache(t *testing.T) {
	now := time.Now()
	body := fmt.Sprintf(`{
		"timestamp": %d,
		"line": "some rendered status line",
		"plan": "Max 5x",
		"usage": {
			"five_hour": {"utilization": 32.0, "resets_at": "2026-09-09T10:50:00+00:00"},
			"seven_day": {"utilization": 7.000000000000001, "resets_at": "2026-09-10T04:00:00+00:00"}
		}
	}`, now.Unix())

	got, err := ReadCache(writeCache(t, body), 15*time.Minute, now)
	if err != nil {
		t.Fatalf("ReadCache: %v", err)
	}

	// 32.0 is a percentage in this file, so it has to come out as 0.32.
	if got.SessionUtilization != 0.32 {
		t.Errorf("utilization = %v, want 0.32", got.SessionUtilization)
	}
	if got.Plan != "Max 5x" {
		t.Errorf("plan = %q, want %q", got.Plan, "Max 5x")
	}
	want := time.Date(2026, 9, 9, 10, 50, 0, 0, time.UTC)
	if !got.SessionResetsAt.Equal(want) {
		t.Errorf("resets at %v, want %v", got.SessionResetsAt, want)
	}
}

func TestReadCacheMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	if _, err := ReadCache(path, time.Minute, time.Now()); !errors.Is(err, ErrNoCache) {
		t.Errorf("err = %v, want ErrNoCache", err)
	}
}

// TestReadCacheStale covers a status line that has stopped running. Note it
// must be a fallback signal, not a fatal error.
func TestReadCacheStale(t *testing.T) {
	now := time.Now()
	body := fmt.Sprintf(`{"timestamp": %d, "usage": {"five_hour": {"utilization": 50}}}`,
		now.Add(-time.Hour).Unix())

	_, err := ReadCache(writeCache(t, body), 15*time.Minute, now)
	if err == nil {
		t.Fatal("a one-hour-old cache was accepted with a 15 minute limit")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want it to match ErrUnavailable", err)
	}
}

// TestReadCacheWithoutUsage covers the entries pulse writes that hold only a
// rendered line -- which happens before any usage has been fetched, and after
// an error.
func TestReadCacheWithoutUsage(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		body string
	}{
		{"line only", fmt.Sprintf(`{"timestamp": %d, "line": "rendered"}`, now.Unix())},
		{"empty usage", fmt.Sprintf(`{"timestamp": %d, "usage": {}}`, now.Unix())},
		{"other buckets only", fmt.Sprintf(`{"timestamp": %d, "usage": {"seven_day": {"utilization": 7}}}`, now.Unix())},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadCache(writeCache(t, tc.body), time.Hour, now)
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("err = %v, want it to match ErrUnavailable", err)
			}
		})
	}
}

func TestReadCacheGarbage(t *testing.T) {
	if _, err := ReadCache(writeCache(t, "not json"), time.Hour, time.Now()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want it to match ErrUnavailable", err)
	}
}

// TestCachePathHonoursXDG checks the path is resolved the same way pulse
// resolves it, or the daemon would look in the wrong place.
func TestCachePathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")
	got, err := CachePath()
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	if want := "/custom/cache/claude-status/cache.json"; got != want {
		t.Errorf("CachePath() = %q, want %q", got, want)
	}

	// A relative value is ignored, as the spec requires.
	t.Setenv("XDG_CACHE_HOME", "relative/path")
	got, err = CachePath()
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	if filepath.IsAbs(got) == false {
		t.Errorf("CachePath() = %q, want an absolute path", got)
	}
}
