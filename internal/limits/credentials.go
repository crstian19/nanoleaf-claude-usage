// Package limits reads the account's real usage against its rate limits.
//
// This is the number Claude Code's own /usage screen shows, which is what a
// person actually cares about: how much of the current session's allowance is
// gone. It is deliberately kept apart from the usage package, which counts
// tokens out of local transcripts and measures something quite different.
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

// maxCredentials caps the credentials file read. It holds a handful of tokens.
const maxCredentials = 1 << 20

// ErrNoCredentials means Claude Code has not stored an OAuth token, so there
// is nothing to authenticate with.
var ErrNoCredentials = errors.New("limits: no Claude Code OAuth credentials found")

// credentials is the subset of Claude Code's credential store we need.
type credentials struct {
	AccessToken string
	ExpiresAt   time.Time
}

// CredentialsPath returns the default location of Claude Code's credential
// store, honouring CLAUDE_CONFIG_DIR.
func CredentialsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, ".credentials.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("limits: locate home: %w", err)
	}
	return filepath.Join(home, ".claude", ".credentials.json"), nil
}

// readCredentials loads the current OAuth access token.
//
// The file is re-read on every request rather than cached, and this is not an
// oversight: the access token is short-lived (hours), and Claude Code
// refreshes it in place. Caching it would mean the daemon starts failing a few
// hours after launch and stays broken until restarted, while a fresh token sat
// on disk the whole time. Reading a small local file is cheap next to the
// request it authenticates.
func readCredentials(path string) (credentials, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the environment or the user's home, not from input
	if err != nil {
		if os.IsNotExist(err) {
			return credentials{}, ErrNoCredentials
		}
		return credentials{}, fmt.Errorf("limits: open credentials: %w", err)
	}
	defer func() { _ = f.Close() }()

	var raw struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.NewDecoder(io.LimitReader(f, maxCredentials)).Decode(&raw); err != nil {
		return credentials{}, fmt.Errorf("limits: decode credentials: %w", err)
	}
	if raw.ClaudeAiOauth.AccessToken == "" {
		return credentials{}, ErrNoCredentials
	}

	c := credentials{AccessToken: raw.ClaudeAiOauth.AccessToken}
	if ms := raw.ClaudeAiOauth.ExpiresAt; ms > 0 {
		c.ExpiresAt = time.UnixMilli(ms)
	}
	return c, nil
}

// expired reports whether the token is past its stated expiry. Checked before
// spending a request on it, because this endpoint's rate limit is far too
// tight to waste an attempt on a token we already know is stale.
func (c credentials) expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}
