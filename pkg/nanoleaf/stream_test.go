package nanoleaf

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// TestSendFrameEncoding checks the wire format against the extControl v2
// layout by reading back a real UDP packet. A mistake here is invisible in
// code review and shows up only as panels that never light.
func TestSendFrameEncoding(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6loopback, Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = server.Close() }()

	conn, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s := &Streamer{conn: conn, transition: 100 * time.Millisecond}
	defer func() { _ = s.Close() }()

	if err := s.Send(Frame{7: {R: 1, G: 2, B: 3}}); err != nil {
		t.Fatalf("send: %v", err)
	}

	buf := make([]byte, 64)
	if err := server.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	n, _, err := server.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if want := frameHeader + panelRecord; n != want {
		t.Fatalf("frame length = %d, want %d", n, want)
	}
	if got := binary.BigEndian.Uint16(buf[0:]); got != 1 {
		t.Errorf("panel count = %d, want 1", got)
	}
	if got := binary.BigEndian.Uint16(buf[2:]); got != 7 {
		t.Errorf("panel id = %d, want 7", got)
	}
	if buf[4] != 1 || buf[5] != 2 || buf[6] != 3 {
		t.Errorf("rgb = %d,%d,%d, want 1,2,3", buf[4], buf[5], buf[6])
	}
	if buf[7] != 0 {
		t.Errorf("white channel = %d, want 0", buf[7])
	}
	if got := binary.BigEndian.Uint16(buf[8:]); got != 1 {
		t.Errorf("transition = %d tenths, want 1", got)
	}
}

// TestSendTransitionFloor guards the rule that a zero transition time makes
// the panels jump, which reads as flicker at frame rate.
func TestSendTransitionFloor(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6loopback, Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = server.Close() }()

	conn, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// 20fps is 50ms, which rounds down to zero tenths of a second.
	s := &Streamer{conn: conn, transition: 50 * time.Millisecond}
	defer func() { _ = s.Close() }()

	if err := s.Send(Frame{1: {}}); err != nil {
		t.Fatalf("send: %v", err)
	}

	buf := make([]byte, 64)
	if err := server.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, _, err := server.ReadFromUDP(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := binary.BigEndian.Uint16(buf[8:]); got < 1 {
		t.Errorf("transition = %d, want at least 1", got)
	}
}

// TestLightsExcludesController checks the controller brick is dropped: it
// appears in the layout but has no LEDs, and including it would leave a dead
// spot in every rendered scene.
func TestLightsExcludesController(t *testing.T) {
	l := Layout{Panels: []Panel{
		{ID: 1, ShapeType: ShapeTriangle},
		{ID: 2, ShapeType: ShapeController},
		{ID: 3, ShapeType: ShapeTriangle},
	}}

	got := l.Lights()
	if len(got) != 2 {
		t.Fatalf("got %d light panels, want 2", len(got))
	}
	for _, p := range got {
		if p.ID == 2 {
			t.Error("controller was included among light panels")
		}
	}
}

// TestRedactPathHidesToken makes sure the auth token, which the Nanoleaf API
// carries in the URL path, never reaches an error message or a log line.
func TestRedactPathHidesToken(t *testing.T) {
	const token = "sUpErSeCrEtToKeN"
	got := redactPath("http://10.0.0.5:16021/api/v1/" + token + "/panelLayout/layout")

	if want := "http://10.0.0.5:16021/api/v1/REDACTED/panelLayout/layout"; got != want {
		t.Errorf("redactPath() = %q, want %q", got, want)
	}
}

// newTestStreamer returns a Streamer writing to a local UDP socket, plus that
// socket for reading frames back.
func newTestStreamer(t *testing.T, transition time.Duration) (*Streamer, *net.UDPConn) {
	t.Helper()

	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6loopback, Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	conn, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s := &Streamer{conn: conn, transition: transition}
	t.Cleanup(func() { _ = s.Close() })
	return s, server
}

// TestSendRejectsUnaddressablePanels covers the safety property Send's doc
// claims: an out-of-range id is refused rather than truncated, because
// wrapping it would silently address a different panel.
func TestSendRejectsUnaddressablePanels(t *testing.T) {
	s, _ := newTestStreamer(t, 100*time.Millisecond)

	for _, id := range []int{-1, 65536, 70000} {
		if err := s.Send(Frame{id: {R: 1}}); err == nil {
			t.Errorf("Send accepted panel id %d", id)
		}
	}

	// The boundary itself is representable and must be accepted.
	if err := s.Send(Frame{65535: {R: 1}}); err != nil {
		t.Errorf("Send rejected the maximum valid id: %v", err)
	}
}

// TestSendMultiPanelFrame checks a realistic frame: every panel gets its own
// record, and each carries its own id -- so the map's random iteration order
// is harmless.
func TestSendMultiPanelFrame(t *testing.T) {
	s, server := newTestStreamer(t, 100*time.Millisecond)

	want := Frame{1: {R: 10}, 2: {G: 20}, 3: {B: 30}, 4: {R: 40, G: 40, B: 40}}
	if err := s.Send(want); err != nil {
		t.Fatalf("send: %v", err)
	}

	buf := make([]byte, 1024)
	if err := server.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	n, _, err := server.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if wantLen := frameHeader + len(want)*panelRecord; n != wantLen {
		t.Fatalf("frame length = %d, want %d", n, wantLen)
	}
	if got := binary.BigEndian.Uint16(buf[0:]); int(got) != len(want) {
		t.Fatalf("panel count = %d, want %d", got, len(want))
	}

	// Decode independently of order and compare against the frame sent.
	got := Frame{}
	for off := frameHeader; off < n; off += panelRecord {
		id := int(binary.BigEndian.Uint16(buf[off:]))
		got[id] = RGB{R: buf[off+2], G: buf[off+3], B: buf[off+4]}
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d panels, want %d", len(got), len(want))
	}
	for id, c := range want {
		if got[id] != c {
			t.Errorf("panel %d = %+v, want %+v", id, got[id], c)
		}
	}
}

// TestSendBufferReuse checks consecutive frames of different sizes do not
// bleed stale bytes through the reused encoding buffer.
func TestSendBufferReuse(t *testing.T) {
	s, server := newTestStreamer(t, 100*time.Millisecond)

	if err := s.Send(Frame{1: {R: 9}, 2: {R: 9}, 3: {R: 9}}); err != nil {
		t.Fatalf("send large: %v", err)
	}
	if err := s.Send(Frame{7: {R: 1, G: 2, B: 3}}); err != nil {
		t.Fatalf("send small: %v", err)
	}

	buf := make([]byte, 1024)
	if err := server.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	// Discard the first frame, then inspect the second.
	if _, _, err := server.ReadFromUDP(buf); err != nil {
		t.Fatalf("read first: %v", err)
	}
	n, _, err := server.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}

	if wantLen := frameHeader + panelRecord; n != wantLen {
		t.Errorf("second frame length = %d, want %d", n, wantLen)
	}
	if got := binary.BigEndian.Uint16(buf[0:]); got != 1 {
		t.Errorf("second frame panel count = %d, want 1", got)
	}
	if got := binary.BigEndian.Uint16(buf[2:]); got != 7 {
		t.Errorf("second frame panel id = %d, want 7", got)
	}
}
