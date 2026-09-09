package nanoleaf

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"
)

// StreamPort is the UDP port that accepts extControl v2 frames.
const StreamPort = 60222

// frameHeader is the 2-byte panel count that precedes the per-panel records.
const frameHeader = 2

// panelRecord is the wire size of one panel in a v2 frame:
// panelID(2) + R + G + B + W + transitionTime(2).
const panelRecord = 8

// Frame is one rendered image: a colour per panel, addressed by panel ID.
type Frame map[int]RGB

// RGB is a straight 8-bit-per-channel colour. The white channel is always
// sent as zero: Shapes panels have no dedicated white LED, and setting it
// only washes the colour out.
type RGB struct {
	R, G, B uint8
}

// Streamer sends extControl frames to a device over UDP.
//
// UDP is fire-and-forget by design: the device never acknowledges a frame, so
// a dropped packet just means one skipped image. That is the right trade for
// a display refreshing many times a second.
//
// Unlike Client, a Streamer is NOT safe for concurrent use: Send reuses one
// encoding buffer across calls. It is meant to be owned by a single render
// loop.
type Streamer struct {
	conn       *net.UDPConn
	transition time.Duration
	buf        []byte
}

// OpenStream puts the device into extControl v2 mode and returns a Streamer
// bound to it. transition is how long the panels take to blend into each new
// frame; matching it to the frame interval gives continuous motion instead of
// visible steps.
//
// The caller must Close the returned Streamer. Note that streaming mode stays
// active on the device until some other effect is selected, so a caller that
// wants to hand control back should also call SelectEffect.
//
// On error the device is left as it was found.
func (c *Client) OpenStream(ctx context.Context, transition time.Duration) (*Streamer, error) {
	// The local socket is opened BEFORE streaming mode is enabled, which
	// looks like the wrong order but is the whole point. Enabling
	// extControl is the only step that changes the device, and it cannot
	// be undone without knowing which effect was selected before. Were the
	// dial to fail after the PUT, the panels would be stranded in
	// streaming mode -- and since the device then reports its effect as
	// "*ExtControl*", the effect the user actually had would be
	// unrecoverable. A UDP dial is local and makes no round trip, so doing
	// it first makes this function effectively atomic.
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(c.host, strconv.Itoa(StreamPort)))
	if err != nil {
		return nil, fmt.Errorf("resolve stream address: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("dial stream: %w", err)
	}

	body := map[string]any{
		"write": map[string]any{
			"command":           "display",
			"animType":          "extControl",
			"extControlVersion": "v2",
		},
	}
	if err := c.do(ctx, http.MethodPut, c.url("/effects"), body, nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("enable extControl: %w", err)
	}

	return &Streamer{conn: conn, transition: transition}, nil
}

// maxTransitionTenths caps the blend time, which is also a 16-bit field.
const maxTransitionTenths = math.MaxUint16

// maxPanels is the largest panel count the wire format can express, since
// the frame header is a 16-bit field.
const maxPanels = math.MaxUint16

// Send writes one frame. Panels absent from the frame are left untouched by
// the device, so a caller that wants a panel dark must say so explicitly.
//
// Panel counts and IDs are range-checked rather than truncated: silently
// wrapping an out-of-range ID would address a different panel, which is far
// worse than refusing the frame.
func (s *Streamer) Send(f Frame) error {
	if len(f) > maxPanels {
		return fmt.Errorf("send frame: %d panels exceeds the %d the protocol allows", len(f), maxPanels)
	}
	for id := range f {
		if id < 0 || id > math.MaxUint16 {
			return fmt.Errorf("send frame: panel id %d is out of range", id)
		}
	}

	// transitionTime is expressed in tenths of a second, and 0 makes the
	// device jump instantly, which reads as flicker at speed. Keep at
	// least one tick.
	tenths := s.transition.Milliseconds() / 100
	if tenths < 1 {
		tenths = 1
	}
	if tenths > maxTransitionTenths {
		tenths = maxTransitionTenths
	}

	need := frameHeader + len(f)*panelRecord
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	buf := s.buf[:need]

	binary.BigEndian.PutUint16(buf[0:], uint16(len(f))) //nolint:gosec // bounded above
	off := frameHeader
	for id, c := range f {
		binary.BigEndian.PutUint16(buf[off:], uint16(id)) //nolint:gosec // bounded above
		buf[off+2] = c.R
		buf[off+3] = c.G
		buf[off+4] = c.B
		buf[off+5] = 0                                          // white channel: unused on Shapes
		binary.BigEndian.PutUint16(buf[off+6:], uint16(tenths)) //nolint:gosec // clamped to [1, maxTransitionTenths]
		off += panelRecord
	}

	if _, err := s.conn.Write(buf); err != nil {
		return fmt.Errorf("send frame: %w", err)
	}
	return nil
}

// Close releases the UDP socket. It does not take the device out of
// streaming mode; see OpenStream.
func (s *Streamer) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}
