// Package hass reads Home Assistant entity state over its REST API.
//
// The daemon only ever reads: the toggle that arms the display is an
// input_boolean the user owns, created once out of band. Keeping this
// read-only means a bug here can never flip switches around the house.
package hass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxBody = 1 << 20

// Client talks to a Home Assistant instance.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client for baseURL (e.g. http://10.0.0.12:32100) using a
// long-lived access token.
func New(baseURL, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("hass: parse base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("hass: base URL must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("hass: base URL has no host")
	}
	if token == "" {
		return nil, errors.New("hass: empty access token")
	}

	return &Client{
		baseURL: u.String(),
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// State is an entity's current state string.
type State struct {
	EntityID string `json:"entity_id"`
	State    string `json:"state"`
}

// EntityState fetches one entity's state.
//
// The entity ID is percent-escaped into the path: it comes from
// configuration, and a stray slash would otherwise silently address a
// different endpoint.
func (c *Client) EntityState(ctx context.Context, entityID string) (State, error) {
	endpoint := c.baseURL + "/api/states/" + url.PathEscape(entityID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return State{}, fmt.Errorf("hass: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// http.Client errors quote the URL but never headers, so the
		// token cannot leak through here.
		return State{}, fmt.Errorf("hass: get %s: %w", entityID, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return State{}, fmt.Errorf("hass: entity %s does not exist", entityID)
	case resp.StatusCode == http.StatusUnauthorized:
		return State{}, errors.New("hass: access token rejected")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return State{}, fmt.Errorf("hass: get %s: unexpected status %s", entityID, resp.Status)
	}

	var out State
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
		return State{}, fmt.Errorf("hass: decode %s: %w", entityID, err)
	}
	return out, nil
}

// IsOn reports whether an entity is in the "on" state.
func (c *Client) IsOn(ctx context.Context, entityID string) (bool, error) {
	s, err := c.EntityState(ctx, entityID)
	if err != nil {
		return false, err
	}
	return s.State == "on", nil
}
