package limits

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestParseUsageUtilizationScale covers the one thing about this undocumented
// endpoint that cannot be pinned down: whether utilization arrives as a
// percentage or a fraction. Guessing wrong would put the display out by 100x.
func TestParseUsageUtilizationScale(t *testing.T) {
	tests := []struct {
		name string
		json string
		want float64
	}{
		{"percentage", `{"five_hour":{"utilization":30}}`, 0.30},
		{"fraction", `{"five_hour":{"utilization":0.30}}`, 0.30},
		{"zero", `{"five_hour":{"utilization":0}}`, 0},
		{"full as percentage", `{"five_hour":{"utilization":100}}`, 1.0},
		{"full as fraction", `{"five_hour":{"utilization":1}}`, 1.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUsage(strings.NewReader(tc.json), time.Now())
			if err != nil {
				t.Fatalf("parseUsage: %v", err)
			}
			if got.SessionUtilization != tc.want {
				t.Errorf("utilization = %v, want %v", got.SessionUtilization, tc.want)
			}
		})
	}
}

func TestParseUsageRejectsBadResponses(t *testing.T) {
	tests := []struct{ name, json string }{
		{"not json", `nope`},
		{"no five_hour bucket", `{"seven_day":{"utilization":7}}`},
		{"implausible utilization", `{"five_hour":{"utilization":9000}}`},
		{"negative utilization", `{"five_hour":{"utilization":-5}}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseUsage(strings.NewReader(tc.json), time.Now())
			if err == nil {
				t.Fatal("bad response was accepted")
			}
			// Every failure has to be a fallback signal, never a fatal
			// one: a wall light must not stop working because an
			// undocumented endpoint changed shape.
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("err = %v, want it to match ErrUnavailable", err)
			}
		})
	}
}

func TestParseResetsAt(t *testing.T) {
	want := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		in   any
		want time.Time
	}{
		{"rfc3339", "2026-09-09T12:00:00Z", want},
		{"rfc3339 with nanos", "2026-09-09T12:00:00.000Z", want},
		{"unix seconds as number", float64(want.Unix()), want},
		{"unix millis as number", float64(want.UnixMilli()), want},
		// Derived, not hardcoded: an arithmetic slip in the test would
		// otherwise look like a bug in the parser.
		{"unix seconds as string", strconv.FormatInt(want.Unix(), 10), want},
		{"missing", nil, time.Time{}},
		{"garbage", "tomorrow", time.Time{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseResetsAt(tc.in)
			if !got.Equal(tc.want) {
				t.Errorf("parseResetsAt(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestRetryAfter matters more than it looks: this endpoint answered two
// probes with a 26-minute Retry-After, so a wrong reading here either locks
// the daemon out or gets it locked out.
func TestRetryAfter(t *testing.T) {
	mk := func(v string) http.Header {
		h := http.Header{}
		if v != "" {
			h.Set("Retry-After", v)
		}
		return h
	}

	if got := retryAfter(mk("1579")); got != 1579*time.Second {
		t.Errorf("seconds: got %v, want 1579s", got)
	}
	if got := retryAfter(mk("")); got != 30*time.Minute {
		t.Errorf("missing header: got %v, want the 30m fallback", got)
	}
	if got := retryAfter(mk("nonsense")); got != 30*time.Minute {
		t.Errorf("unparseable: got %v, want the 30m fallback", got)
	}
	if got := retryAfter(mk("0")); got != 30*time.Minute {
		t.Errorf("zero: got %v, want the fallback rather than no wait", got)
	}
	// An HTTP-date in the past must not produce a negative wait.
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := retryAfter(mk(past)); got <= 0 {
		t.Errorf("past date: got %v, want a positive wait", got)
	}
}

func TestThrottledMatchesUnavailable(t *testing.T) {
	err := error(&Throttled{RetryAfter: time.Minute})
	if !errors.Is(err, ErrUnavailable) {
		t.Error("a throttling error does not match ErrUnavailable")
	}

	var throttled *Throttled
	if !errors.As(err, &throttled) || throttled.RetryAfter != time.Minute {
		t.Error("the retry delay is not recoverable from the error")
	}
}

// writeCreds builds a credential store like Claude Code's.
func writeCreds(t *testing.T, token string, expires time.Time) string {
	t.Helper()

	body := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken": token,
			"expiresAt":   expires.UnixMilli(),
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestReadCredentials(t *testing.T) {
	exp := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	got, err := readCredentials(writeCreds(t, "tok-123", exp))
	if err != nil {
		t.Fatalf("readCredentials: %v", err)
	}
	if got.AccessToken != "tok-123" {
		t.Errorf("token = %q, want tok-123", got.AccessToken)
	}
	if !got.ExpiresAt.Equal(exp) {
		t.Errorf("expiry = %v, want %v", got.ExpiresAt, exp)
	}
	if got.expired(time.Now()) {
		t.Error("a token valid for another hour was reported expired")
	}
	if !got.expired(exp.Add(time.Minute)) {
		t.Error("an expired token was reported valid")
	}
}

func TestReadCredentialsMissing(t *testing.T) {
	_, err := readCredentials(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, ErrNoCredentials) {
		t.Errorf("err = %v, want ErrNoCredentials", err)
	}

	empty := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(empty, []byte(`{"claudeAiOauth":{}}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := readCredentials(empty); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("err = %v for a store with no token, want ErrNoCredentials", err)
	}
}

// TestFetchSendsExpectedRequest checks the request shape, including that the
// token travels in a header and never in the URL -- the mistake that put a
// Nanoleaf token in the journal earlier in this project.
func TestFetchSendsExpectedRequest(t *testing.T) {
	const token = "secret-token-value"
	var gotAuth, gotBeta, gotAgent, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAgent = r.Header.Get("User-Agent")
		gotPath = r.URL.String()
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2026-09-09T12:00:00Z"}}`))
	}))
	defer srv.Close()

	c := New(writeCreds(t, token, time.Now().Add(time.Hour)), "2.1.263")
	c.baseURL = srv.URL

	got, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if got.SessionUtilization != 0.42 {
		t.Errorf("utilization = %v, want 0.42", got.SessionUtilization)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBeta != oauthBeta {
		t.Errorf("anthropic-beta = %q, want %q", gotBeta, oauthBeta)
	}
	if !strings.Contains(gotAgent, "2.1.263") {
		t.Errorf("User-Agent = %q, want it to carry the version", gotAgent)
	}
	if strings.Contains(gotPath, token) {
		t.Errorf("the token appeared in the URL: %q", gotPath)
	}
}

func TestFetchHandlesThrottling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1579")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(writeCreds(t, "tok", time.Now().Add(time.Hour)), "1.0.0")
	c.baseURL = srv.URL

	_, err := c.Fetch(context.Background())
	var throttled *Throttled
	if !errors.As(err, &throttled) {
		t.Fatalf("err = %v, want a Throttled error", err)
	}
	if throttled.RetryAfter != 1579*time.Second {
		t.Errorf("retry after = %v, want 1579s", throttled.RetryAfter)
	}
}

func TestFetchHandlesRejectionAndErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"unauthorised", http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden},
		{"server error", http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			c := New(writeCreds(t, "tok", time.Now().Add(time.Hour)), "1.0.0")
			c.baseURL = srv.URL

			if _, err := c.Fetch(context.Background()); !errors.Is(err, ErrUnavailable) {
				t.Errorf("err = %v, want it to match ErrUnavailable", err)
			}
		})
	}
}

// TestFetchSkipsExpiredToken checks a token already known to be stale does
// not cost a request. With a 26-minute penalty for asking too often, a wasted
// attempt is expensive.
func TestFetchSkipsExpiredToken(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := New(writeCreds(t, "tok", time.Now().Add(-time.Hour)), "1.0.0")
	c.baseURL = srv.URL

	if _, err := c.Fetch(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if called {
		t.Error("a request was spent on a token already known to be expired")
	}
}

// TestFetchErrorsNeverCarryTheToken is the counterpart to the Nanoleaf
// redaction test.
func TestFetchErrorsNeverCarryTheToken(t *testing.T) {
	const token = "hunter2-but-longer"

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // provoke a transport error

	c := New(writeCreds(t, token, time.Now().Add(time.Hour)), "1.0.0")
	c.baseURL = url

	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the token leaked into the error: %v", err)
	}
}
