package limits

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// DefaultCachePath is where claude-pulse leaves the usage it reads.
//
// Reading that file, rather than asking the API directly, is the better
// source whenever it exists, and not by a small margin:
//
//   - It costs nothing. Pulse is a status line, so it is already polling on
//     its own account; one more reader of a local file adds no traffic.
//   - It cannot get anyone rate limited. Probing the endpoint directly on top
//     of pulse's own traffic is what earned a 26-minute lockout while this
//     was being built.
//   - It needs no credentials. No token to read, expire, or refresh, which
//     removes that whole surface from this daemon.
//   - It is fresher. Pulse caches for 60 seconds; polite direct polling could
//     not go near that.
const DefaultCachePath = "cache.json"

// CachePath returns the default location of that cache, honouring
// XDG_CACHE_HOME the same way pulse does.
func CachePath() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("limits: locate home: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claude-status", DefaultCachePath), nil
}

// ErrNoCache means the status line's cache is not there to read.
var ErrNoCache = errors.New("limits: no status line usage cache")

// maxCache caps the read. The file holds a rendered line and a handful of
// numbers.
const maxCache = 1 << 20

// ReadCache loads the usage a status line has already fetched.
//
// staleAfter bounds how old a reading may be. Note that a stale cache is not
// only a failure mode: the file stops being written when Claude Code is not
// running, which is also when usage stops growing -- so the caller's fallback
// takes over exactly when there is nothing new to miss.
func ReadCache(path string, staleAfter time.Duration, now time.Time) (Snapshot, error) {
	f, err := os.Open(path) //nolint:gosec // path is a known cache location, not input
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, ErrNoCache
		}
		return Snapshot{}, fmt.Errorf("limits: open usage cache: %w", err)
	}
	defer func() { _ = f.Close() }()

	var raw struct {
		Timestamp float64 `json:"timestamp"`
		Plan      string  `json:"plan"`
		Usage     struct {
			FiveHour *limit `json:"five_hour"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(f, maxCache)).Decode(&raw); err != nil {
		return Snapshot{}, fmt.Errorf("%w: decode usage cache: %w", ErrUnavailable, err)
	}

	// The cache is also written for entries that only hold a rendered line,
	// with no usage in them at all.
	if raw.Usage.FiveHour == nil {
		return Snapshot{}, fmt.Errorf("%w: cache holds no five_hour bucket", ErrUnavailable)
	}

	written := time.Unix(int64(raw.Timestamp), 0)
	if age := now.Sub(written); age > staleAfter {
		return Snapshot{}, fmt.Errorf("%w: cache is %s old", ErrUnavailable, age.Round(time.Second))
	}

	snap, err := fromLimit(*raw.Usage.FiveHour, written)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Plan = raw.Plan
	return snap, nil
}
