// Package nanoleaf is a client for the Nanoleaf local OpenAPI, including the
// UDP external-control streaming mode used to drive individual panels.
//
// It talks to a device on the LAN; it does not depend on any cloud service.
package nanoleaf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// DefaultAPIPort is the TCP port the Nanoleaf local OpenAPI listens on.
const DefaultAPIPort = 16021

// maxBody caps how much of a device response we are willing to read, so a
// misbehaving or spoofed device cannot make us allocate without bound.
const maxBody = 1 << 20

// ErrNotPairing is returned by Pair when the device is not in pairing mode.
// The physical remedy is holding the controller's power button for 5-7s.
var ErrNotPairing = errors.New("nanoleaf: device not in pairing mode (hold power 5-7s)")

// Client is a Nanoleaf local API client. It is safe for concurrent use.
type Client struct {
	host  string
	port  int
	token string
	http  *http.Client
}

// New returns a client for the device at host, authenticating with token.
// An empty token is only good for Pair.
func New(host, token string) *Client {
	return &Client{
		host:  host,
		port:  DefaultAPIPort,
		token: token,
		http: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				// A panel controller is a single LAN host: one idle
				// connection is plenty and keeps it reused.
				MaxIdleConnsPerHost: 1,
				DialContext: (&net.Dialer{
					Timeout: 2 * time.Second,
				}).DialContext,
			},
		},
	}
}

func (c *Client) url(path string) string {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))
	return "http://" + addr + "/api/v1/" + c.token + path
}

// do performs a request and decodes a JSON response into out (which may be nil
// when the endpoint returns no body, as most writes do).
func (c *Client) do(ctx context.Context, method, rawURL string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", stripURL(err))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, redactPath(rawURL), stripURL(err))
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusForbidden {
		return ErrNotPairing
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: unexpected status %s", method, redactPath(rawURL), resp.Status)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// tokenInPath matches the auth token segment of a local API path. The
// Nanoleaf API carries the token in the URL, so every error and log line has
// to be scrubbed before it escapes this package.
var tokenInPath = regexp.MustCompile(`/api/v1/[^/]+`)

func redactPath(rawURL string) string {
	return tokenInPath.ReplaceAllString(rawURL, "/api/v1/REDACTED")
}

// stripURL removes the request URL from a transport error.
//
// This is not belt-and-braces: http.Client returns a *url.Error whose Error()
// prints the full URL, and Go only strips userinfo from it, never the path.
// Since the auth token IS a path segment, wrapping such an error would print
// the token in clear right next to the redacted copy -- defeating the
// redaction with the very error being wrapped. Only the cause is kept;
// url.Error carries nothing else we want in a log line.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		// Unwrapping preserves errors.Is against context.DeadlineExceeded
		// and friends, since that is what url.Error wrapped anyway.
		return ue.Err
	}
	return err
}

// Pair asks a device in pairing mode for a fresh auth token. Devices hold
// several tokens at once, so pairing again does not revoke an existing
// integration's access.
func Pair(ctx context.Context, host string) (string, error) {
	c := New(host, "")
	addr := net.JoinHostPort(host, strconv.Itoa(DefaultAPIPort))

	var out struct {
		AuthToken string `json:"auth_token"`
	}
	if err := c.do(ctx, http.MethodPost, "http://"+addr+"/api/v1/new", nil, &out); err != nil {
		return "", err
	}
	if out.AuthToken == "" {
		return "", errors.New("nanoleaf: device returned an empty auth token")
	}
	return out.AuthToken, nil
}
