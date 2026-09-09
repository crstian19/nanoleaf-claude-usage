package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultBaseURL is the API host that serves the usage endpoint.
const DefaultBaseURL = "https://api.anthropic.com"

// usagePath is the endpoint Claude Code's own /usage screen reads.
//
// It is not a documented public API. That is a deliberate, known trade: it is
// the only source of the real numbers, and everything here is written to
// degrade quietly when it changes or goes away rather than to depend on it.
const usagePath = "/api/oauth/usage"

// oauthBeta is the beta header the CLI sends with OAuth-authenticated calls.
const oauthBeta = "oauth-2025-04-20"

const maxBody = 1 << 20

// ErrUnavailable means the endpoint could not be used this time -- throttled,
// unauthorised, changed shape. Callers fall back rather than fail.
var ErrUnavailable = errors.New("limits: usage endpoint unavailable")

// Throttled reports that the endpoint refused the request and when it may be
// tried again.
//
// This endpoint's limit is severe: two probes bought a 26-minute Retry-After.
// So the wait is surfaced as data rather than swallowed -- a caller that
// ignored it would stay locked out permanently.
type Throttled struct {
	RetryAfter time.Duration
}

func (t *Throttled) Error() string {
	return fmt.Sprintf("limits: throttled, retry after %s", t.RetryAfter)
}

// Is lets errors.Is(err, ErrUnavailable) match a throttling error, since to
// every caller it means the same thing: use the fallback this time.
func (t *Throttled) Is(target error) bool { return target == ErrUnavailable }

// Snapshot is the account's real usage against its limits.
type Snapshot struct {
	// SessionUtilization is the fraction of the current session's
	// allowance already used, in [0,1].
	SessionUtilization float64
	// SessionResetsAt is when that allowance refills.
	SessionResetsAt time.Time
	// At is when the snapshot was taken.
	At time.Time
	// Plan names the subscription, when the source reports it. Logged at
	// startup so the scale on the wall can be traced to an account.
	Plan string
}

// Client reads the usage endpoint.
type Client struct {
	credPath  string
	baseURL   string
	userAgent string
	http      *http.Client
}

// New returns a client reading credentials from credPath and identifying
// itself with the given Claude Code version.
func New(credPath, clientVersion string) *Client {
	if clientVersion == "" {
		clientVersion = "unknown"
	}
	return &Client{
		credPath:  credPath,
		baseURL:   DefaultBaseURL,
		userAgent: "claude-cli/" + clientVersion + " (external, cli)",
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Fetch reads the current usage.
func (c *Client) Fetch(ctx context.Context) (Snapshot, error) {
	creds, err := readCredentials(c.credPath)
	if err != nil {
		return Snapshot{}, err
	}
	now := time.Now()
	if creds.expired(now) {
		// Not worth a request: the rate limit is too tight to spend an
		// attempt on a token that is already stale. Claude Code will
		// refresh it on its next call.
		return Snapshot{}, fmt.Errorf("%w: stored token expired at %s", ErrUnavailable, creds.ExpiresAt.Format(time.RFC3339))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+usagePath, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("limits: build request: %w", err)
	}
	// The token goes in a header, never the URL, so transport errors
	// cannot carry it the way the Nanoleaf client's could.
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return Snapshot{}, &Throttled{RetryAfter: retryAfter(resp.Header)}
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return Snapshot{}, fmt.Errorf("%w: credentials rejected (%s)", ErrUnavailable, resp.Status)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return Snapshot{}, fmt.Errorf("%w: unexpected status %s", ErrUnavailable, resp.Status)
	}

	return parseUsage(io.LimitReader(resp.Body, maxBody), now)
}

// retryAfter reads the Retry-After header, falling back to a conservative
// wait when it is missing or unparseable -- guessing short would just earn
// another refusal.
//
// Takes the header rather than the response because that is all it needs.
func retryAfter(h http.Header) time.Duration {
	const fallback = 30 * time.Minute

	raw := h.Get("Retry-After")
	if raw == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return fallback
}

// limit is one of the rate-limit buckets the endpoint reports.
type limit struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    any     `json:"resets_at"`
}

// parseUsage decodes the response.
//
// The parser is deliberately loose about two things, because the endpoint is
// undocumented and its exact conventions cannot be pinned: utilization is
// accepted as either a percentage or a fraction, and resets_at as either an
// RFC3339 string or a unix timestamp.
func parseUsage(r io.Reader, now time.Time) (Snapshot, error) {
	var raw struct {
		FiveHour *limit `json:"five_hour"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return Snapshot{}, fmt.Errorf("%w: decode response: %w", ErrUnavailable, err)
	}
	if raw.FiveHour == nil {
		return Snapshot{}, fmt.Errorf("%w: response has no five_hour bucket", ErrUnavailable)
	}

	return fromLimit(*raw.FiveHour, now)
}

// fromLimit normalises one bucket into a Snapshot.
//
// Utilization is accepted as either a percentage or a fraction because the
// two sources disagree: the status line cache stores 32.0 for a third of the
// allowance, and there is no guarantee the API is consistent about it either.
func fromLimit(l limit, at time.Time) (Snapshot, error) {
	u := l.Utilization
	// A value above 1 can only be a percentage: a fraction of an
	// allowance cannot exceed 1 by more than rounding.
	if u > 1 {
		u /= 100
	}
	if u < 0 || u > 1.5 {
		return Snapshot{}, fmt.Errorf("%w: implausible utilization %v", ErrUnavailable, l.Utilization)
	}

	return Snapshot{
		SessionUtilization: u,
		SessionResetsAt:    parseResetsAt(l.ResetsAt),
		At:                 at,
	}, nil
}

// parseResetsAt accepts the shapes a timestamp might arrive in. A zero time
// means unknown, which only costs the caller a projection.
func parseResetsAt(v any) time.Time {
	switch t := v.(type) {
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return parsed
			}
		}
		if secs, err := strconv.ParseInt(t, 10, 64); err == nil {
			return unixGuess(secs)
		}
	case float64:
		return unixGuess(int64(t))
	}
	return time.Time{}
}

// unixGuess reads a unix timestamp that may be in seconds or milliseconds.
func unixGuess(v int64) time.Time {
	// Seconds-since-epoch stays below this well past the next century;
	// anything larger is milliseconds.
	const secondsCeiling = 1e11
	if v > secondsCeiling {
		return time.UnixMilli(v)
	}
	return time.Unix(v, 0)
}
