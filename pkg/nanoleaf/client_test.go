package nanoleaf

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const secret = "sUpErSeCrEtToKeN"

// closedPort returns an address nothing is listening on, for provoking a real
// transport error.
func closedPort(t *testing.T) string {
	t.Helper()

	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr.String()
}

// clientAt builds a client pointed at an arbitrary host:port.
func clientAt(t *testing.T, addr, token string) *Client {
	t.Helper()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	c := New(host, token)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	c.port = port
	return c
}

// TestTransportErrorsHideToken is a regression test, and the reason it is
// written end to end rather than against redactPath: the redaction function
// was always correct, but http.Client returns a *url.Error whose Error()
// reprints the full URL -- token and all -- so wrapping it defeated the
// redaction. A unit test on redactPath alone passed while the token was
// being written to the journal on every connection failure.
func TestTransportErrorsHideToken(t *testing.T) {
	addr := closedPort(t)

	calls := map[string]func(*Client) error{
		"Layout": func(c *Client) error {
			_, err := c.Layout(context.Background())
			return err
		},
		"State": func(c *Client) error {
			_, err := c.State(context.Background())
			return err
		},
		"Info": func(c *Client) error {
			_, err := c.Info(context.Background())
			return err
		},
		"SetOn":        func(c *Client) error { return c.SetOn(context.Background(), true) },
		"SelectEffect": func(c *Client) error { return c.SelectEffect(context.Background(), "Nightclub") },
		"OpenStream": func(c *Client) error {
			_, err := c.OpenStream(context.Background(), time.Second)
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call(clientAt(t, addr, secret))
			if err == nil {
				t.Fatal("expected a transport error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("the token leaked into the error: %v", err)
			}
			if !strings.Contains(err.Error(), "REDACTED") {
				t.Errorf("error does not show the redacted path: %v", err)
			}
		})
	}
}

// TestTimeoutErrorsHideToken covers the other transport failure shape, since
// a timeout produces a differently-wrapped *url.Error.
func TestTimeoutErrorsHideToken(t *testing.T) {
	// A server that never answers within the client's deadline.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	c := clientAt(t, strings.TrimPrefix(srv.URL, "http://"), secret)
	c.http.Timeout = 50 * time.Millisecond

	if _, err := c.Layout(context.Background()); err == nil {
		t.Fatal("expected a timeout")
	} else if strings.Contains(err.Error(), secret) {
		t.Errorf("the token leaked into a timeout error: %v", err)
	}
}

// TestNonPairingIsRecognisable checks a 403 maps to the sentinel, so `pair`
// can tell "not in pairing mode yet" from a real failure and keep waiting.
func TestNonPairingIsRecognisable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := clientAt(t, strings.TrimPrefix(srv.URL, "http://"), secret)
	if _, err := c.State(context.Background()); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "pairing mode") {
		t.Errorf("err = %v, want the pairing-mode sentinel", err)
	}
}

// TestRestorable pins which saved effects may be handed back. Getting this
// wrong is how a user's effect is lost for good: treating "*ExtControl*" as
// real overwrites the last known good effect with a placeholder.
func TestRestorable(t *testing.T) {
	tests := []struct {
		effect string
		want   bool
	}{
		{"Northern Lights", true},
		{"moonlight", true},
		{"", false},
		{"*ExtControl*", false},
		{"*Dynamic*", false},
		{"*Solid*", false},
	}

	for _, tc := range tests {
		if got := (State{Effect: tc.effect}).Restorable(); got != tc.want {
			t.Errorf("State{Effect: %q}.Restorable() = %v, want %v", tc.effect, got, tc.want)
		}
	}
}

// TestAddressable checks the panel-id bound that keeps one bad id from
// blanking every panel.
func TestAddressable(t *testing.T) {
	tests := []struct {
		id   int
		want bool
	}{
		{0, true},
		{1, true},
		{65535, true},
		{65536, false},
		{70000, false},
		{-1, false},
	}

	for _, tc := range tests {
		if got := (Panel{ID: tc.id}).Addressable(); got != tc.want {
			t.Errorf("Panel{ID: %d}.Addressable() = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// TestStreamsV2 pins the model check. NL22 is triangular like the Shapes
// Triangles and easy to mistake for them, but speaks a different protocol
// version -- and because UDP never answers, the failure would be silent.
func TestStreamsV2(t *testing.T) {
	if (Info{Model: ModelLightPanels}).StreamsV2() {
		t.Error("NL22 reported as extControl v2 capable")
	}
	for _, m := range []string{"NL42", "NL47", "NL48", "NL29", ""} {
		if !(Info{Model: m}).StreamsV2() {
			t.Errorf("model %q reported as not v2 capable", m)
		}
	}
}
