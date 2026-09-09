// Package usage reads Claude Code token usage by shelling out to ccusage.
//
// It lives in internal/ rather than pkg/ on purpose: launching a process and
// finding a binary on PATH is specific to this daemon's environment, not
// reusable library behaviour.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// binaryName is the executable consulted for usage data.
const binaryName = "ccusage"

// maxOutput caps how much JSON we accept.
//
// This is enforced while reading, not after: cmd.Output would buffer the
// whole stream into memory first and leave the check with nothing left to
// prevent. Ceiling asks for every historical window, so the output grows with
// usage history and the bound is not theoretical.
const maxOutput = 8 << 20

// maxStderr caps the diagnostic tail kept from a failed run. Enough for a
// node stack trace's first lines, not enough to matter if ccusage goes mad.
const maxStderr = 4 << 10

// ErrNoActiveBlock means no five-hour window is currently open: nothing has
// been sent to Claude recently.
var ErrNoActiveBlock = errors.New("usage: no active block")

// Window is the state of the current five-hour billing window.
type Window struct {
	StartTime time.Time
	EndTime   time.Time
	Tokens    int64
	CostUSD   float64
	Models    []string

	// TokensPerMinute is the current burn rate.
	TokensPerMinute float64
	// CostPerHour is the same rate expressed in money, which is the
	// better proxy for rate-limit consumption: it weights each token type
	// by price instead of counting a cache read the same as an output
	// token.
	CostPerHour float64
	// ProjectedTokens is where that rate lands by EndTime.
	ProjectedTokens int64
}

// block mirrors the fields of a ccusage block that we use.
type block struct {
	ID        string   `json:"id"`
	StartTime string   `json:"startTime"`
	EndTime   string   `json:"endTime"`
	IsActive  bool     `json:"isActive"`
	IsGap     bool     `json:"isGap"`
	Tokens    int64    `json:"totalTokens"`
	CostUSD   float64  `json:"costUSD"`
	Models    []string `json:"models"`
	BurnRate  struct {
		TokensPerMinute float64 `json:"tokensPerMinute"`
		CostPerHour     float64 `json:"costPerHour"`
	} `json:"burnRate"`
	Projection struct {
		TotalTokens int64 `json:"totalTokens"`
	} `json:"projection"`
}

type blocksResponse struct {
	Blocks []block `json:"blocks"`
}

// Reader queries ccusage. It holds no state beyond the resolved binary path,
// so it is safe to share.
type Reader struct {
	path string
}

// NewReader locates ccusage on PATH.
func NewReader() (*Reader, error) {
	path, err := exec.LookPath(binaryName)
	if err != nil {
		return nil, fmt.Errorf("usage: %s not found on PATH: %w", binaryName, err)
	}
	return &Reader{path: path}, nil
}

// cappedBuffer collects at most limit bytes and silently drops the rest.
type cappedBuffer struct {
	limit int
	buf   []byte
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - len(c.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		c.buf = append(c.buf, p[:room]...)
	}
	// Report a full write: this is a diagnostic sink, and short writes
	// would make the subprocess fail for the wrong reason.
	return len(p), nil
}

func (c *cappedBuffer) String() string { return string(c.buf) }

// run executes ccusage with the given arguments and decodes its JSON output.
func (r *Reader) run(ctx context.Context, args ...string) (blocksResponse, error) {
	// Resolved path and fixed arguments only: nothing here is built from
	// user input, so there is no shell and no injection surface.
	cmd := exec.CommandContext(ctx, r.path, args...) //nolint:gosec // absolute resolved path, fixed literal args, no shell

	// ccusage's own message is the only clue when it is misconfigured --
	// wrong node version, missing data directory -- so keep a tail of it
	// instead of reporting a bare exit code.
	stderr := &cappedBuffer{limit: maxStderr}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return blocksResponse{}, fmt.Errorf("usage: pipe %s stdout: %w", binaryName, err)
	}
	if err := cmd.Start(); err != nil {
		return blocksResponse{}, fmt.Errorf("usage: start %s: %w", binaryName, err)
	}

	// One byte past the cap, so an oversized output is detected without
	// ever being held in full.
	out, readErr := io.ReadAll(io.LimitReader(stdout, maxOutput+1))
	oversized := len(out) > maxOutput
	if oversized {
		// Stop it rather than politely draining gigabytes; without this
		// the subprocess would block writing to a pipe nobody reads.
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()

	switch {
	case oversized:
		return blocksResponse{}, fmt.Errorf("usage: %s produced more than %d bytes, refusing", binaryName, maxOutput)
	case readErr != nil:
		return blocksResponse{}, fmt.Errorf("usage: read %s output: %w", binaryName, readErr)
	case waitErr != nil:
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			return blocksResponse{}, fmt.Errorf("usage: %s exited %d: %s",
				binaryName, exit.ExitCode(), strings.TrimSpace(stderr.String()))
		}
		return blocksResponse{}, fmt.Errorf("usage: run %s: %w", binaryName, waitErr)
	}

	return parseBlocks(out)
}

// parseBlocks decodes ccusage's JSON. Split from run so the selection rules
// below can be tested against fixtures instead of a subprocess.
func parseBlocks(raw []byte) (blocksResponse, error) {
	var resp blocksResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return blocksResponse{}, fmt.Errorf("usage: decode %s output: %w", binaryName, err)
	}
	return resp, nil
}

// Active returns the window currently in progress.
func (r *Reader) Active(ctx context.Context) (Window, error) {
	resp, err := r.run(ctx, "blocks", "--active", "--json", "--offline")
	if err != nil {
		return Window{}, err
	}
	return selectActive(resp)
}

// selectActive picks the live window out of a response.
//
// Gap blocks are periods with no usage that ccusage reports for continuity;
// one can be marked active, and treating it as a real window would show a
// full display for a session that is not running.
func selectActive(resp blocksResponse) (Window, error) {
	for _, b := range resp.Blocks {
		if !b.IsActive || b.IsGap {
			continue
		}
		return Window{
			StartTime:       parseTime(b.StartTime),
			EndTime:         parseTime(b.EndTime),
			Tokens:          b.Tokens,
			CostUSD:         b.CostUSD,
			Models:          b.Models,
			TokensPerMinute: b.BurnRate.TokensPerMinute,
			CostPerHour:     b.BurnRate.CostPerHour,
			ProjectedTokens: b.Projection.TotalTokens,
		}, nil
	}
	return Window{}, ErrNoActiveBlock
}

// Ceiling estimates what a "full" window looks like, from the heaviest
// windows this machine has actually recorded.
//
// The plan's real rate limit is not exposed anywhere readable, so the display
// is calibrated against observed history instead: the second-highest window
// rather than the outright maximum, so one freak session does not permanently
// flatten the scale.
func (r *Reader) Ceiling(ctx context.Context) (int64, error) {
	resp, err := r.run(ctx, "blocks", "--json", "--offline")
	if err != nil {
		return 0, err
	}
	return selectCeiling(resp)
}

// CeilingCost estimates what a full window costs, from history.
//
// Only used as a last resort: it is a fallback for the fallback, for a
// machine that has never once managed to read its real limits. Cost is a far
// better proxy than raw tokens -- it weights each token type by price,
// whereas totalTokens is about 98% cache reads, the cheapest thing there is,
// and so mostly measures how long the conversation has got.
func (r *Reader) CeilingCost(ctx context.Context) (float64, error) {
	resp, err := r.run(ctx, "blocks", "--json", "--offline")
	if err != nil {
		return 0, err
	}
	return selectCeilingCost(resp)
}

func selectCeilingCost(resp blocksResponse) (float64, error) {
	var costs []float64
	for _, b := range resp.Blocks {
		if b.IsGap || b.CostUSD <= 0 {
			continue
		}
		costs = append(costs, b.CostUSD)
	}
	if len(costs) == 0 {
		return 0, errors.New("usage: no historical windows to calibrate against")
	}

	slices.Sort(costs)
	slices.Reverse(costs)
	if len(costs) >= 2 {
		return costs[1], nil
	}
	return costs[0], nil
}

// selectCeiling implements the calibration rule: the second-heaviest window
// observed, so one freak session does not permanently flatten the scale. With
// only one window on record there is nothing to discard, so it is used as is.
func selectCeiling(resp blocksResponse) (int64, error) {
	var tokens []int64
	for _, b := range resp.Blocks {
		if b.IsGap || b.Tokens <= 0 {
			continue
		}
		tokens = append(tokens, b.Tokens)
	}
	if len(tokens) == 0 {
		return 0, errors.New("usage: no historical windows to calibrate against")
	}

	slices.Sort(tokens)
	slices.Reverse(tokens)
	if len(tokens) >= 2 {
		return tokens[1], nil
	}
	return tokens[0], nil
}

// parseTime tolerates a missing or malformed timestamp: a zero time makes the
// caller fall back to a time-independent view rather than fail the frame.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
