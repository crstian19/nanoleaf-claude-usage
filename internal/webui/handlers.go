package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// assets is the page itself. It is embedded rather than built, so
// `go install` remains the whole installation: a page this size does not
// justify putting a JavaScript toolchain in front of a Go binary.
//
//go:embed assets
var assets embed.FS

// heartbeat is how often a stream that has nothing new to say writes anyway,
// so a connection that died without a close is noticed.
const heartbeat = 10 * time.Second

// handler routes the session's requests.
//
// Every route carries the session token as its first path segment, so a
// request that does not have it never reaches a handler.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /t/{ticket}/{$}", http.HandlerFunc(s.handleTicket))
	mux.Handle("GET /{token}/{$}", s.guard(s.handlePage))
	mux.Handle("GET /{token}/app.css", s.guard(s.handleAsset("assets/app.css", "text/css; charset=utf-8")))
	mux.Handle("GET /{token}/app.js", s.guard(s.handleAsset("assets/app.js", "text/javascript; charset=utf-8")))
	mux.Handle("GET /{token}/info", s.guard(s.handleInfo))
	mux.Handle("GET /{token}/events", s.guard(s.handleEvents))
	mux.Handle("POST /{token}/state", s.guard(s.handleState))
	mux.Handle("POST /{token}/edit", s.guard(s.handleEdit))
	mux.Handle("POST /{token}/save", s.guard(s.handleSave))
	mux.Handle("POST /{token}/done", s.guard(s.handleDone))
	mux.HandleFunc("/", handleStray)
	return s.checkCaller(mux)
}

// handleTicket exchanges the one-time code in the printed address for the
// session itself, by redirecting to it.
//
// The redirect is the whole point: the printed address is handed to a browser
// as a command-line argument, where another user on the machine can read it,
// so what it carries has to be worth nothing the second time. See newSecrets.
func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	switch s.redeem(r.PathValue("ticket")) {
	case ticketFresh:
		http.Redirect(w, r, s.pageURL(), http.StatusSeeOther)
	case ticketSpent:
		// Said out loud rather than answered with a bare 404: if the
		// user did not open this address twice themselves, something
		// else on the machine did, and that is worth knowing.
		http.Error(w, "This address has already been opened. Run `nanoclaude calibrate` again for a new one.\n",
			http.StatusGone)
	case ticketWrong:
		handleStray(w, r)
	}
}

// handleStray answers anything else. A person who lands here typed the
// address without the part that makes it work.
func handleStray(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Not found. Open the address nanoclaude printed, including the code at the end of it.\n",
		http.StatusNotFound)
}

// checkCaller rejects requests that did not come from the page.
//
// Two things are checked, and neither is a formality:
//
// The Host header must be this server's own loopback address. Without that a
// name in someone else's DNS can be pointed at 127.0.0.1 and a page they
// serve becomes same-origin with this one, which is the whole DNS rebinding
// attack.
//
// Sec-Fetch-Site, when the browser sends it, must say the request came from
// this page or from the address bar. That is what stops another site the user
// happens to have open from posting here.
func (s *Server) checkCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.knownHost(r.Host) {
			http.Error(w, "Wrong host.\n", http.StatusForbidden)
			return
		}
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
		default:
			http.Error(w, "Cross-site requests are not accepted.\n", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "Cross-origin requests are not accepted.\n", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// knownHost reports whether the request was addressed to this server by a
// loopback name and its own port.
func (s *Server) knownHost(host string) bool {
	_, port, err := net.SplitHostPort(s.host)
	if err != nil {
		return false
	}
	reqHost, reqPort, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	if reqPort != port {
		return false
	}
	switch strings.ToLower(reqHost) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// guard checks the session token, in constant time, and sets the headers that
// keep the page from reaching anything but this server.
func (s *Server) guard(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !codeMatches(r.PathValue("token"), s.token) {
			handleStray(w, r)
			return
		}
		head := w.Header()
		head.Set("Content-Security-Policy",
			"default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; "+
				"img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		head.Set("Referrer-Policy", "no-referrer")
		head.Set("X-Content-Type-Options", "nosniff")
		head.Set("Cache-Control", "no-store")
		h(w, r)
	})
}

// handlePage serves the page.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	s.handleAsset("assets/page.html", "text/html; charset=utf-8")(w, r)
}

// handleAsset serves one embedded file.
func (s *Server) handleAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		body, err := assets.ReadFile(name)
		if err != nil {
			http.Error(w, "missing asset\n", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}
}

// ShapeOption is one entry of the page's shape picker.
type ShapeOption struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// Info is what the page needs once: the things that do not change while the
// shape is being turned.
type Info struct {
	// Shapes are the arrangements that can be drawn, and Shape is the one
	// being drawn now. A single unnamed shape means there is nothing to
	// pick from.
	Shapes []ShapeOption `json:"shapes"`
	Shape  string        `json:"shape"`

	// Live says whether the panels on a wall are being painted. False for
	// the sample shapes, where the page is the only display.
	Live bool `json:"live"`

	// CanSave says whether a rotation can be written to the
	// configuration, which a sample shape cannot.
	CanSave bool `json:"canSave"`

	// BuildShape names the arrangement the page builds itself, and Kinds
	// are the panels it may drop onto it. Empty when this session offers
	// no such thing.
	BuildShape string `json:"buildShape,omitempty"`
	Kinds      []Kind `json:"kinds,omitempty"`

	// Placeable are the kinds that have somewhere to go on the wall as it
	// stands. A page greys out the rest: a Canvas square cannot join a
	// wall of Shapes triangles, and someone dragging one deserves to be
	// told before they try.
	Placeable []int `json:"placeable,omitempty"`

	// Panels is how many panels the device reports and Lit how many of
	// them the display can light.
	Panels int `json:"panels"`
	Lit    int `json:"lit"`

	SideLength        int `json:"sideLength"`
	GlobalOrientation int `json:"globalOrientation"`

	// UnknownShapes are shape numbers this version cannot name. They are
	// drawn as circles and lit like any other panel; a panel with no LEDs
	// among them would leave a dark spot, which is worth saying.
	UnknownShapes []int `json:"unknownShapes"`

	ConfigPath     string `json:"configPath"`
	DisplayRunning bool   `json:"displayRunning"`

	// The controls' starting positions.
	Rotation int     `json:"rotation"`
	Mode     Mode    `json:"mode"`
	Level    float64 `json:"level"`
	Phase    string  `json:"phase"`
}

func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	// One critical section: the state and the wall being built have to be
	// described as they were at the same moment.
	s.mu.Lock()
	state := s.state
	layout := s.shapeNamed(state.Shape).Layout
	if state.Shape == BuildShape && s.build != nil {
		// The wall the page is building has no layout of its own until
		// something is on it.
		if built, err := s.build.wall().Layout(); err == nil {
			layout = built
		}
	}
	// The palette is fixed when the session starts, so it can be read
	// here and used after the lock is dropped.
	var buildShape string
	var kinds []Kind
	var placeable []int
	if s.build != nil {
		buildShape, kinds = BuildShape, s.build.kinds
		placeable = s.build.placeable()
	}
	s.mu.Unlock()

	usable, _ := render.Project(layout, state.Rotation).Lights()

	options := make([]ShapeOption, 0, len(s.opt.Shapes))
	for _, shape := range s.opt.Shapes {
		options = append(options, ShapeOption{Name: shape.Name, Label: shape.Label})
	}

	seen := map[int]bool{}
	unknown := []int{}
	for _, p := range layout.Panels {
		if !nanoleaf.IsKnownShape(p.ShapeType) && !seen[p.ShapeType] {
			seen[p.ShapeType] = true
			unknown = append(unknown, p.ShapeType)
		}
	}

	writeJSON(w, Info{
		BuildShape:        buildShape,
		Kinds:             kinds,
		Placeable:         placeable,
		Shapes:            options,
		Shape:             state.Shape,
		Live:              s.opt.Open != nil,
		CanSave:           s.opt.Save != nil,
		Panels:            len(layout.Panels),
		Lit:               len(usable),
		SideLength:        layout.SideLength,
		GlobalOrientation: layout.GlobalOrientation,
		UnknownShapes:     unknown,
		ConfigPath:        s.opt.ConfigPath,
		DisplayRunning:    s.opt.DisplayRunning,
		Rotation:          state.Rotation,
		Mode:              state.Mode,
		Level:             state.Level,
		Phase:             state.Phase.String(),
	})
}

// shapeNamed finds a shape the session offers, falling back to the first so
// a caller always has a layout to work from.
func (s *Server) shapeNamed(name string) Shape {
	for _, shape := range s.opt.Shapes {
		if shape.Name == name {
			return shape
		}
	}
	return s.opt.Shapes[0]
}

// knownShape reports whether the session offers a shape by that name.
func (s *Server) knownShape(name string) bool {
	for _, shape := range s.opt.Shapes {
		if shape.Name == name {
			return true
		}
	}
	return false
}

// handleEvents streams the picture to the page.
//
// The page is sent the frame that went to the panels, not a colour it works
// out for itself. A calibration tool whose screen and wall could disagree
// would be worse than no tool at all.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.attach() {
		http.Error(w, "too many pages are open\n", http.StatusServiceUnavailable)
		return
	}
	defer s.detach()

	w.Header().Set("Content-Type", "text/event-stream")
	stream := http.NewResponseController(w)

	ticker := time.NewTicker(eventPeriod)
	defer ticker.Stop()

	var last []byte
	lastWrite := time.Now()
	for {
		payload, err := json.Marshal(s.snapshot())
		if err != nil {
			return
		}

		var wrote bool
		switch {
		case !bytes.Equal(payload, last):
			wrote = true
			last = payload
		case time.Since(lastWrite) > heartbeat:
			// A comment, which the browser ignores. Its only job is
			// to fail if the connection has gone away without
			// saying so. It matters because the calibration picture
			// does not change on its own: with the shape left
			// alone, every snapshot is byte for byte the last one,
			// and nothing else would ever be written.
			payload = nil
			wrote = true
		}

		if wrote {
			// A deadline per write, because a client that stops
			// reading would otherwise block this goroutine for
			// good once the socket's buffer filled -- and a blocked
			// stream never gets to notice the page has gone, so the
			// session would hold the panels for ever.
			if err := stream.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				return
			}
			if payload == nil {
				_, err = io.WriteString(w, ":\n\n")
			} else {
				_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
			}
			if err != nil {
				return
			}
			if err := stream.Flush(); err != nil {
				return
			}
			lastWrite = time.Now()
		}

		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
		}
	}
}

// attach registers a streaming page.
func (s *Server) attach() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clients >= maxClients {
		return false
	}
	s.clients++
	s.connected = true
	return true
}

// detach forgets one, starting the clock that ends the session when the last
// page has gone.
func (s *Server) detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients--
	if s.clients < 0 {
		s.clients = 0
	}
	if s.clients == 0 {
		s.idleSince = time.Now()
	}
}

// stateRequest is a change to the controls. Every field is optional, so the
// page can send just the one the user moved.
type stateRequest struct {
	Shape    *string  `json:"shape"`
	Placing  *int     `json:"placing"`
	Rotation *int     `json:"rotation"`
	Mode     *Mode    `json:"mode"`
	Level    *float64 `json:"level"`
	Phase    *string  `json:"phase"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	var req stateRequest
	if err := decode(w, r, &req); err != nil {
		http.Error(w, err.Error()+"\n", http.StatusBadRequest)
		return
	}

	// Everything is checked before the lock is taken, so a bad value
	// cannot leave the state half changed and a slow check cannot hold up
	// the loop painting the panels.
	var phase render.Phase
	if req.Phase != nil {
		ph, err := render.ParsePhase(*req.Phase)
		if err != nil {
			http.Error(w, err.Error()+"\n", http.StatusBadRequest)
			return
		}
		phase = ph
	}
	if req.Mode != nil && *req.Mode != ModePattern && *req.Mode != ModeGauge {
		http.Error(w, fmt.Sprintf("unknown mode %q\n", *req.Mode), http.StatusBadRequest)
		return
	}
	if req.Placing != nil && *req.Placing != noPlacing {
		s.mu.Lock()
		known := s.build != nil && s.build.knownKind(*req.Placing)
		s.mu.Unlock()
		if !known {
			http.Error(w, fmt.Sprintf("no panel of kind %d can be placed here\n", *req.Placing),
				http.StatusBadRequest)
			return
		}
	}
	if req.Shape != nil && !s.knownShape(*req.Shape) {
		http.Error(w, fmt.Sprintf("no shape called %q\n", *req.Shape), http.StatusBadRequest)
		return
	}

	// One critical section, and only the fields the request named. Reading
	// the whole state, changing a copy and writing it back would lose a
	// change made in between -- a save landing mid-drag would have its
	// rotation overwritten by the drag's next frame.
	s.mu.Lock()
	if req.Shape != nil {
		s.state.Shape = *req.Shape
	}
	if req.Placing != nil {
		s.state.Placing = *req.Placing
	}
	if req.Rotation != nil {
		s.state.Rotation = render.WrapDegrees(*req.Rotation)
	}
	if req.Mode != nil {
		s.state.Mode = *req.Mode
	}
	if req.Level != nil {
		s.state.Level = min(max(*req.Level, 0), maxLevel)
	}
	if req.Phase != nil {
		s.state.Phase = phase
	}
	next := s.state
	s.mu.Unlock()

	writeJSON(w, stateResponse{
		Shape:    next.Shape,
		Rotation: next.Rotation,
		Mode:     next.Mode,
		Level:    next.Level,
		Phase:    next.Phase.String(),
	})
}

// stateResponse echoes the controls after a change. The page adopts what
// comes back rather than assuming its own value was taken, which is how a
// rotation that got wrapped or a level that got clamped reaches the display
// the user is looking at.
type stateResponse struct {
	Shape    string  `json:"shape"`
	Rotation int     `json:"rotation"`
	Mode     Mode    `json:"mode"`
	Level    float64 `json:"level"`
	Phase    string  `json:"phase"`
}

// saveRequest carries the rotation to write. The page names it rather than
// letting the server use whatever it has, so a save can never land on an
// angle the user did not see.
type saveRequest struct {
	Rotation *int `json:"rotation"`
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if s.opt.Save == nil {
		// A sample shape is not on anybody's wall, so an angle for it
		// means nothing. The page offers no button, and this is the
		// answer for anything else that asks.
		http.Error(w, "this session has no configuration to write to\n", http.StatusBadRequest)
		return
	}

	var req saveRequest
	if err := decode(w, r, &req); err != nil {
		http.Error(w, err.Error()+"\n", http.StatusBadRequest)
		return
	}
	if req.Rotation == nil {
		http.Error(w, "no rotation to save\n", http.StatusBadRequest)
		return
	}

	rotation := render.WrapDegrees(*req.Rotation)
	if err := s.opt.Save(rotation); err != nil {
		http.Error(w, err.Error()+"\n", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.saved = &rotation
	s.state.Rotation = rotation
	s.mu.Unlock()

	writeJSON(w, map[string]any{"rotation": rotation, "configPath": s.opt.ConfigPath})
}

// handleEdit changes the wall the page is building.
//
// Every change goes through the same geometry that draws it: a panel is stuck
// to an edge, and an edge something is already on is refused. The page never
// sends a position, only which edge of which panel it dropped on, so it
// cannot put a panel somewhere the tiling does not allow.
func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	var req editRequest
	if err := decode(w, r, &req); err != nil {
		http.Error(w, err.Error()+"\n", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.build == nil {
		http.Error(w, "this session has no wall to build\n", http.StatusBadRequest)
		return
	}
	if err := s.build.edit(req); err != nil {
		// The builder's own words: they say which panel is in the way,
		// or which direction has no edge, and the page shows them.
		http.Error(w, err.Error()+"\n", http.StatusBadRequest)
		return
	}

	// Whatever was being dragged has landed.
	s.state.Placing = noPlacing
	s.state.Shape = BuildShape

	writeJSON(w, map[string]any{"panels": s.build.wall().Panels()})
}

// handleDone ends the session, which hands the panels back.
func (s *Server) handleDone(w http.ResponseWriter, _ *http.Request) {
	s.stop("closed from the page")
	w.WriteHeader(http.StatusNoContent)
}

// decode reads a small JSON body strictly: an unknown field is a mistake
// worth reporting, not something to ignore.
func decode(w http.ResponseWriter, r *http.Request, into any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	if err := dec.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return errors.New("bad request body: more than one JSON value")
	}
	return nil
}

// writeJSON sends a response body, ignoring a write failure: the client has
// gone, and there is nowhere left to report it.
func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
