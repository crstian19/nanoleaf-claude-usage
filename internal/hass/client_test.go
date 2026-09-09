package hass

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewValidatesBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		token   string
		wantErr bool
	}{
		{"plain http", "http://10.0.0.12:32100", "tok", false},
		{"https", "https://ha.example.com", "tok", false},
		{"trailing slash is trimmed", "http://ha/", "tok", false},
		{"file scheme", "file:///etc/passwd", "tok", true},
		{"no scheme", "10.0.0.12:32100", "tok", true},
		{"scheme without host", "http://", "tok", true},
		{"empty url", "", "tok", true},
		{"empty token", "http://ha", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.url, tc.token)
			if (err != nil) != tc.wantErr {
				t.Errorf("New(%q) error = %v, wantErr %v", tc.url, err, tc.wantErr)
			}
		})
	}
}

// TestIsOn covers the states the daemon actually branches on.
func TestIsOn(t *testing.T) {
	tests := []struct {
		name   string
		state  string
		wantOn bool
	}{
		{"on", `{"entity_id":"input_boolean.x","state":"on"}`, true},
		{"off", `{"entity_id":"input_boolean.x","state":"off"}`, false},
		// An entity that exists but has no value yet must not read as on.
		{"unavailable", `{"entity_id":"input_boolean.x","state":"unavailable"}`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.state))
			}))
			defer srv.Close()

			c, err := New(srv.URL, "tok")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			on, err := c.IsOn(context.Background(), "input_boolean.x")
			if err != nil {
				t.Fatalf("IsOn: %v", err)
			}
			if on != tc.wantOn {
				t.Errorf("state %q -> on = %v, want %v", tc.state, on, tc.wantOn)
			}
		})
	}
}

// TestErrorStatusesAreDistinguishable checks a missing entity and a rejected
// token produce different messages: they need different fixes.
func TestErrorStatusesAreDistinguishable(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{http.StatusNotFound, "does not exist"},
		{http.StatusUnauthorized, "token rejected"},
		{http.StatusInternalServerError, "unexpected status"},
	}

	for _, tc := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		c, err := New(srv.URL, "tok")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, err = c.IsOn(context.Background(), "input_boolean.x")
		srv.Close()

		if err == nil {
			t.Errorf("status %d produced no error", tc.status)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d: err = %v, want it to mention %q", tc.status, err, tc.want)
		}
	}
}

// TestTokenNeverAppearsInErrors is the counterpart to the Nanoleaf redaction
// test. Here the token is header-only, so it should never reach an error --
// this pins that it stays that way.
func TestTokenNeverAppearsInErrors(t *testing.T) {
	const token = "hassSuPeRsEcReT"

	// A closed server, to provoke a transport error.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c, err := New(url, token)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.IsOn(context.Background(), "input_boolean.x"); err == nil {
		t.Fatal("expected a transport error")
	} else if strings.Contains(err.Error(), token) {
		t.Errorf("the token leaked into the error: %v", err)
	}
}
