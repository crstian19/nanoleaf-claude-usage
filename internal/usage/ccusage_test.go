package usage

import (
	"errors"
	"strings"
	"testing"
)

// TestSelectActiveSkipsGaps pins a rule that is easy to get wrong: ccusage
// reports gap blocks for periods with no usage, and one can be flagged
// active. Treating a gap as a real window would light the display for a
// session nobody is running.
func TestSelectActiveSkipsGaps(t *testing.T) {
	resp, err := parseBlocks([]byte(`{"blocks":[
		{"id":"gap","isActive":true,"isGap":true,"totalTokens":999},
		{"id":"real","isActive":true,"isGap":false,"totalTokens":42,
		 "projection":{"totalTokens":100},"burnRate":{"tokensPerMinute":7}}
	]}`))
	if err != nil {
		t.Fatalf("parseBlocks: %v", err)
	}

	w, err := selectActive(resp)
	if err != nil {
		t.Fatalf("selectActive: %v", err)
	}
	if w.Tokens != 42 {
		t.Errorf("tokens = %d, want 42 (the gap block was picked)", w.Tokens)
	}
	if w.ProjectedTokens != 100 {
		t.Errorf("projected = %d, want 100", w.ProjectedTokens)
	}
}

// TestSelectActiveNoWindow covers the ordinary idle case, which must be a
// recognisable sentinel rather than a generic failure.
func TestSelectActiveNoWindow(t *testing.T) {
	resp, err := parseBlocks([]byte(`{"blocks":[{"id":"old","isActive":false,"totalTokens":5}]}`))
	if err != nil {
		t.Fatalf("parseBlocks: %v", err)
	}
	if _, err := selectActive(resp); !errors.Is(err, ErrNoActiveBlock) {
		t.Errorf("err = %v, want ErrNoActiveBlock", err)
	}
}

// TestSelectCeiling covers the calibration rule and its edges. The
// second-heaviest choice is a product decision -- one freak session must not
// flatten the scale for good -- so it is pinned here.
func TestSelectCeiling(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    int64
		wantErr bool
	}{
		{
			name: "second heaviest wins",
			json: `{"blocks":[{"totalTokens":50},{"totalTokens":200},{"totalTokens":130}]}`,
			want: 130,
		},
		{
			name: "a single window is used as is",
			json: `{"blocks":[{"totalTokens":77}]}`,
			want: 77,
		},
		{
			name: "gaps and empty windows do not count",
			json: `{"blocks":[{"totalTokens":900,"isGap":true},{"totalTokens":0},{"totalTokens":60},{"totalTokens":80}]}`,
			want: 60,
		},
		{
			name:    "nothing to calibrate against",
			json:    `{"blocks":[{"totalTokens":0},{"totalTokens":500,"isGap":true}]}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := parseBlocks([]byte(tc.json))
			if err != nil {
				t.Fatalf("parseBlocks: %v", err)
			}
			got, err := selectCeiling(resp)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got %d, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectCeiling: %v", err)
			}
			if got != tc.want {
				t.Errorf("ceiling = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestParseBlocksRejectsGarbage checks a decode failure names the binary, so
// a broken ccusage is identifiable from the daemon's log.
func TestParseBlocksRejectsGarbage(t *testing.T) {
	_, err := parseBlocks([]byte("not json"))
	if err == nil {
		t.Fatal("garbage was accepted")
	}
	if !strings.Contains(err.Error(), binaryName) {
		t.Errorf("err = %v, want it to name %s", err, binaryName)
	}
}

// TestParseTimeTolerantOfJunk pins the deliberate choice to degrade rather
// than fail: a malformed timestamp yields a zero time.
func TestParseTimeTolerantOfJunk(t *testing.T) {
	if got := parseTime("not a time"); !got.IsZero() {
		t.Errorf("parseTime(junk) = %v, want the zero time", got)
	}
	if got := parseTime("2026-09-09T05:00:00.000Z"); got.IsZero() {
		t.Error("a valid RFC3339 timestamp parsed as zero")
	}
}

// TestCappedBufferStopsAtLimit checks the stderr sink keeps a head and still
// reports full writes -- a short write would fail the subprocess for the
// wrong reason.
func TestCappedBufferStopsAtLimit(t *testing.T) {
	c := &cappedBuffer{limit: 4}

	n, err := c.Write([]byte("abcdefgh"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 8 {
		t.Errorf("n = %d, want 8 (a short write would break the subprocess)", n)
	}
	if got := c.String(); got != "abcd" {
		t.Errorf("buffered %q, want %q", got, "abcd")
	}

	// Writes after the limit are accepted and discarded.
	if _, err := c.Write([]byte("ijkl")); err != nil {
		t.Fatalf("Write past limit: %v", err)
	}
	if got := c.String(); got != "abcd" {
		t.Errorf("buffered %q after overflow, want %q", got, "abcd")
	}
}
