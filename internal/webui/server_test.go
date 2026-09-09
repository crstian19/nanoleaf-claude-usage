package webui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// panels records what a session did, standing in for a device and a
// configuration file.
type panels struct {
	mu     sync.Mutex
	frames int
	saved  []int
	opens  int
	closes int

	// fail is returned by the next failUntil sends, standing in for a
	// device that has left streaming mode.
	fail      error
	failUntil int
}

// open, Send and Close make it a Stream the session can take over.
func (p *panels) open(context.Context) (Stream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opens++
	return p, nil
}

func (p *panels) Send(nanoleaf.Frame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.frames++
	if p.fail != nil && (p.failUntil < 0 || p.frames <= p.failUntil) {
		return p.fail
	}
	return nil
}

func (p *panels) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	return nil
}

func (p *panels) opened() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opens
}

// failWith makes every send fail from now on.
func (p *panels) failWith(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail, p.failUntil = err, -1
}

// failFor makes the next n sends fail, as a device does while something else
// is selecting effects on it.
func (p *panels) failFor(n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail, p.failUntil = err, n
}

func (p *panels) save(rotation int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saved = append(p.saved, rotation)
	return nil
}

func (p *panels) sent() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frames
}

func (p *panels) written() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.saved...)
}

// origin is the http:// prefix the handler answers on.
func (s *Server) origin() string { return "http://" + s.host }

func newTestServer(t *testing.T) (*Server, *panels) {
	t.Helper()
	rec := &panels{}
	s, err := New(t.Context(), Options{
		Shapes:     []Shape{{Layout: realLayout(t)}},
		Rotation:   0,
		Open:       rec.open,
		Save:       rec.save,
		ConfigPath: "/tmp/nanoclaude-test/env",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, rec
}

// call sends a request to the handler as the page would: right host, right
// token unless the test says otherwise.
func call(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, "http://"+s.host+path, reader)
	req.Host = s.host
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}

// TestTokenGuardsThePage is the first half of keeping the panels to their
// owner: the page can change what the wall shows and can write to the
// configuration file, so knowing the port must not be enough.
func TestTokenGuardsThePage(t *testing.T) {
	s, _ := newTestServer(t)

	// "/info/" is in the list because a path that has the shape of a
	// token is still just a wrong token: the first segment of every route
	// is the session's own.
	for _, path := range []string{"/", "/info/", "/wrong/", "/wrong/info", "/wrong/events"} {
		if got := call(t, s, http.MethodGet, path, "").Code; got != http.StatusNotFound {
			t.Errorf("GET %s returned %d, want 404", path, got)
		}
	}
	if got := call(t, s, http.MethodPost, "/wrong/save", `{"rotation":10}`).Code; got != http.StatusNotFound {
		t.Errorf("a save without the token returned %d, want 404", got)
	}
	if got := call(t, s, http.MethodGet, "/"+s.token+"/", "").Code; got != http.StatusOK {
		t.Errorf("the page itself returned %d, want 200", got)
	}
}

// TestForeignHostRejected closes the DNS rebinding path: a name in someone
// else's DNS pointed at 127.0.0.1 would otherwise make their page
// same-origin with this one.
func TestForeignHostRejected(t *testing.T) {
	s, _ := newTestServer(t)

	for _, host := range []string{"panels.example.com:1234", "127.0.0.1:1", "evil.test"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"http://"+s.host+"/"+s.token+"/info", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Host %q returned %d, want 403", host, rec.Code)
		}
	}
}

// TestCrossSiteRequestRejected covers the other browser that happens to be
// open: a page on another site must not be able to post here, even if it
// somehow learned the token.
func TestCrossSiteRequestRejected(t *testing.T) {
	s, _ := newTestServer(t)

	for _, header := range []struct{ name, value string }{
		{"Sec-Fetch-Site", "cross-site"},
		{"Sec-Fetch-Site", "same-site"},
		{"Origin", "https://elsewhere.example"},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"http://"+s.host+"/"+s.token+"/state", strings.NewReader(`{"rotation":10}`))
		req.Host = s.host
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(header.name, header.value)
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: %s returned %d, want 403", header.name, header.value, rec.Code)
		}
	}
}

func TestStateChecksWhatItIsSent(t *testing.T) {
	s, _ := newTestServer(t)

	for _, body := range []string{
		`{"mode":"rainbow"}`,
		`{"phase":"busy"}`,
		`{"rotation":"90"}`,
		`{"turns":3}`,
		`{"rotation":1}{"rotation":2}`,
		``,
	} {
		if got := call(t, s, http.MethodPost, "/"+s.token+"/state", body).Code; got != http.StatusBadRequest {
			t.Errorf("state %s returned %d, want 400", body, got)
		}
	}

	rec := call(t, s, http.MethodPost, "/"+s.token+"/state", `{"rotation":725,"level":9,"mode":"gauge"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid change returned %d: %s", rec.Code, rec.Body)
	}
	var got stateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Rotation != 5 {
		t.Errorf("rotation 725 became %d, want 5", got.Rotation)
	}
	if got.Level != maxLevel {
		t.Errorf("level 9 became %v, want %v", got.Level, maxLevel)
	}
	if got.Mode != ModeGauge {
		t.Errorf("mode is %q, want %q", got.Mode, ModeGauge)
	}
}

// TestSaveWritesTheAngleThePageNamed keeps the saved value to the one the
// user was looking at, rather than whatever the server happened to hold.
func TestSaveWritesTheAngleThePageNamed(t *testing.T) {
	s, rec := newTestServer(t)

	if got := call(t, s, http.MethodPost, "/"+s.token+"/save", `{}`).Code; got != http.StatusBadRequest {
		t.Errorf("a save with no angle returned %d, want 400", got)
	}
	if got := call(t, s, http.MethodPost, "/"+s.token+"/save", `{"rotation":370}`).Code; got != http.StatusOK {
		t.Fatalf("save returned %d", got)
	}

	if written := rec.written(); len(written) != 1 || written[0] != 10 {
		t.Errorf("wrote %v, want [10]", written)
	}
	if saved := s.Result().Saved; saved == nil || *saved != 10 {
		t.Errorf("the result reports %v, want 10", saved)
	}
}

// TestSessionPaintsAndStops runs the real thing: a page loads, the panels
// start being painted, the picture reaches the page, and closing it from the
// page ends the session.
func TestSessionPaintsAndStops(t *testing.T) {
	s, rec := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// The printed address carries the one-time ticket, and following it
	// lands on the page.
	if code := get(t, s.URL()); code != http.StatusOK {
		t.Fatalf("the page returned %d", code)
	}

	// The panels are painted whether or not a page is watching.
	waitFor(t, func() bool { return rec.sent() > 0 })

	// One picture off the stream, to prove the page is fed the same frame.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.origin()+s.pageURL()+"events", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
	if !ok {
		t.Fatalf("the stream opened with %q, want a data line", line)
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Panels) != 9 {
		t.Errorf("the page was sent %d panels, want 9", len(snap.Panels))
	}

	if code := post(t, s.origin()+s.pageURL()+"done", "{}"); code != http.StatusNoContent {
		t.Fatalf("done returned %d", code)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the session ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end after the page said it was done")
	}
	if reason := s.Result().Reason; reason != "closed from the page" {
		t.Errorf("the session reports %q", reason)
	}
}

// TestTheAddressOpensOnce covers the two codes a session has. The address
// printed in the terminal is handed to a browser as a command-line argument,
// where any other user on the machine can read it, so it has to be worth
// nothing the second time.
func TestTheAddressOpensOnce(t *testing.T) {
	s, _ := newTestServer(t)

	first := call(t, s, http.MethodGet, "/t/"+s.ticket+"/", "")
	if first.Code != http.StatusSeeOther {
		t.Fatalf("the printed address returned %d, want 303", first.Code)
	}
	if got := first.Header().Get("Location"); got != s.pageURL() {
		t.Errorf("it led to %q, wanted the page at %q", got, s.pageURL())
	}

	if again := call(t, s, http.MethodGet, "/t/"+s.ticket+"/", ""); again.Code != http.StatusGone {
		t.Errorf("opening it a second time returned %d, want 410", again.Code)
	}
	if wrong := call(t, s, http.MethodGet, "/t/notthisticket/", ""); wrong.Code != http.StatusNotFound {
		t.Errorf("a wrong ticket returned %d, want 404", wrong.Code)
	}

	// And the page itself keeps working, which is what makes a reload
	// safe once the ticket is spent.
	if page := call(t, s, http.MethodGet, s.pageURL(), ""); page.Code != http.StatusOK {
		t.Errorf("the page returned %d after the ticket was spent", page.Code)
	}
}

// TestTheSameOriginPageIsAdmitted is the positive case the rejections do not
// cover: a guard that refused everything would pass every other test here.
func TestTheSameOriginPageIsAdmitted(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+s.host+s.pageURL()+"state", strings.NewReader(`{"rotation":90}`))
	req.Host = s.host
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://"+s.host)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the page's own request returned %d: %s", rec.Code, rec.Body)
	}
}

// TestAControlChangeTouchesOnlyWhatItNames pins the contract handleState
// keeps: a request that says nothing about the rotation must leave it alone.
//
// That is what makes a save safe. The page sends a control change on every
// mouse move, and a save arriving in the middle of that stream has to
// survive it. Reading the whole state, changing a copy and writing it back
// would put the pre-save rotation back a moment later, and the configuration
// file would then disagree with the wall. Whether it does so is a matter of
// timing that no test can force, so the fields are applied in one critical
// section and this is the visible half of it.
func TestAControlChangeTouchesOnlyWhatItNames(t *testing.T) {
	s, rec := newTestServer(t)

	const saveAt = 137
	for _, body := range []string{
		`{"rotation":90}`,
		`{"level":0.2}`,
		`{"mode":"gauge"}`,
		`{"phase":"tool"}`,
	} {
		if code := call(t, s, http.MethodPost, s.pageURL()+"state", body).Code; code != http.StatusOK {
			t.Fatalf("state %s returned %d", body, code)
		}
	}
	if got := s.Result().Rotation; got != 90 {
		t.Errorf("the rotation is %d after three changes that never mentioned it, want 90", got)
	}

	if code := call(t, s, http.MethodPost, s.pageURL()+"save", fmt.Sprintf(`{"rotation":%d}`, saveAt)).Code; code != http.StatusOK {
		t.Fatalf("save returned %d", code)
	}
	if code := call(t, s, http.MethodPost, s.pageURL()+"state", `{"level":0.9}`).Code; code != http.StatusOK {
		t.Fatal("a level change after a save was refused")
	}

	res := s.Result()
	if res.Saved == nil || *res.Saved != saveAt {
		t.Errorf("the result reports %v saved, want %d", res.Saved, saveAt)
	}
	if res.Rotation != saveAt {
		t.Errorf("the session is at %d after saving %d", res.Rotation, saveAt)
	}
	if written := rec.written(); len(written) != 1 || written[0] != saveAt {
		t.Errorf("wrote %v, want [%d]", written, saveAt)
	}
}

// TestThePanelsAreNotTakenBackAfterTheSession is the invariant the whole
// project turns on: enabling streaming mode is the only irreversible step, so
// nothing may enable it once the panels have been handed back.
func TestThePanelsAreNotTakenBackAfterTheSession(t *testing.T) {
	s, rec := newTestServer(t)
	s.openGrace = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("the session ended with %v", err)
	}

	opened := rec.opened()
	// Give a pump that outlived Run every chance to show itself.
	time.Sleep(10 * framePeriodForTest)
	if now := rec.opened(); now != opened {
		t.Errorf("the panels were taken over %d more times after the session ended", now-opened)
	}
	if rec.closes == 0 {
		t.Error("the session never gave the stream up")
	}
}

// framePeriodForTest is the pump's tick, named here so the wait above is
// obviously more than one frame.
const framePeriodForTest = FramePeriod

// TestASessionWithNoDeviceDrawsAnyway is the mode the sample shapes run in:
// a page with no panels behind it. It has to be live in every other respect,
// because it is what somebody looks at to decide whether this is worth
// mounting anything for.
func TestASessionWithNoDeviceDrawsAnyway(t *testing.T) {
	rec := &panels{}
	s, err := New(t.Context(), Options{
		Shapes: []Shape{
			{Name: "one", Label: "the wall this was written on", Layout: realLayout(t)},
			{Name: "two", Label: "a square", Layout: squareLayout()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.openGrace = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// A picture is produced without a device to send it to.
	waitFor(t, func() bool { return len(s.snapshot().Panels) > 0 })
	if rec.sent() != 0 {
		t.Errorf("%d frames were sent by a session with no device", rec.sent())
	}

	// The shape can be changed, which is the whole point of the mode.
	if code := call(t, s, http.MethodPost, s.pageURL()+"state", `{"shape":"two"}`).Code; code != http.StatusOK {
		t.Fatalf("picking a shape returned %d", code)
	}
	waitFor(t, func() bool { return s.snapshot().Shape == "two" })
	if got := len(s.snapshot().Panels); got != 4 {
		t.Errorf("the square shape drew %d panels, want 4", got)
	}
	if code := call(t, s, http.MethodPost, s.pageURL()+"state", `{"shape":"nope"}`).Code; code != http.StatusBadRequest {
		t.Errorf("an unknown shape returned %d, want 400", code)
	}

	// And nothing can be saved, because none of it is on a wall.
	if code := call(t, s, http.MethodPost, s.pageURL()+"save", `{"rotation":30}`).Code; code != http.StatusBadRequest {
		t.Errorf("saving a sample shape returned %d, want 400", code)
	}

	var info Info
	if err := json.Unmarshal(call(t, s, http.MethodGet, s.pageURL()+"info", "").Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Live || info.CanSave {
		t.Errorf("the page is told live=%v canSave=%v", info.Live, info.CanSave)
	}
	if len(info.Shapes) != 2 || info.Shapes[1].Label != "a square" {
		t.Errorf("the page was offered %+v", info.Shapes)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("the session ended with %v", err)
	}
}

// squareLayout is a four-panel Canvas, small enough to be counted in a test.
func squareLayout() nanoleaf.Layout {
	return nanoleaf.Layout{
		NumPanels:  4,
		SideLength: 100,
		Panels: []nanoleaf.Panel{
			{ID: 1, X: 0, Y: 0, ShapeType: nanoleaf.ShapeSquare},
			{ID: 2, X: 100, Y: 0, ShapeType: nanoleaf.ShapeSquare},
			{ID: 3, X: 0, Y: 100, ShapeType: nanoleaf.ShapeSquare},
			{ID: 4, X: 100, Y: 100, ShapeType: nanoleaf.ShapeSquare},
		},
	}
}

// TestSessionEndsWithNoBrowser hands the panels back on its own. Without it a
// browser that never opened, or a terminal left behind, would leave a
// calibration pattern on the wall for good.
func TestSessionEndsWithNoBrowser(t *testing.T) {
	s, _ := newTestServer(t)
	s.openGrace = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the session ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session waited for a browser for ever")
	}
	if reason := s.Result().Reason; reason != "no browser opened the page" {
		t.Errorf("the session reports %q", reason)
	}
}

// TestUnpaintablePanelsStopTheSession reports a device that stopped
// answering, rather than sitting there with a page that looks alive.
func TestUnpaintablePanelsStopTheSession(t *testing.T) {
	s, rec := newTestServer(t)
	s.sendGrace = 100 * time.Millisecond
	rec.failWith(errNoDevice)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "gone") {
			t.Fatalf("the session ended with %v, want the device error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a device that cannot be painted did not end the session")
	}
}

// TestTroubleWithThePanelsIsReportedNotFatal is the state a calibration
// really runs into: the device leaves streaming mode whenever anything else
// selects an effect on it, and the frames after that come back refused. The
// session has to recover and say so, not exit.
func TestTroubleWithThePanelsIsReportedNotFatal(t *testing.T) {
	s, rec := newTestServer(t)
	rec.failFor(4, errNoDevice)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// The page is told what the panels said.
	waitFor(t, func() bool { return strings.Contains(s.snapshot().Trouble, "gone") })
	// And told when they come back.
	waitFor(t, func() bool { return s.snapshot().Trouble == "" })

	select {
	case err := <-done:
		t.Fatalf("the session ended over a passing failure: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if code := post(t, s.origin()+s.pageURL()+"done", "{}"); code != http.StatusNoContent {
		t.Fatalf("done returned %d", code)
	}
	if err := <-done; err != nil {
		t.Fatalf("the session ended with %v", err)
	}
}

var errNoDevice = errStr("the device is gone")

type errStr string

func (e errStr) Error() string { return string(e) }

func get(t *testing.T, url string) int {
	ctx := t.Context()
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func post(t *testing.T, url, body string) int {
	ctx := t.Context()
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting")
}
