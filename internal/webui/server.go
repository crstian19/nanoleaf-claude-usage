package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/internal/render"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Timings of the session.
const (
	// FramePeriod is how often the panels are repainted. The same rate the
	// daemon uses, so the rainbow the page previews moves at the speed it
	// will move for real. Exported so the caller can match the panels'
	// blend time to it.
	FramePeriod = 50 * time.Millisecond

	// eventPeriod is how often the page is sent a new picture. Slower than
	// the panels because a browser only has to look continuous, and every
	// event carries the whole shape.
	eventPeriod = 100 * time.Millisecond

	// openGrace is how long to wait for a browser that has not connected
	// yet. Long enough for a cold browser start on a loaded machine.
	openGrace = 3 * time.Minute

	// closeGrace is how long to keep the panels after the last page goes
	// away. Short: the wall is showing a calibration picture, and the user
	// has closed the tab, so they are done.
	closeGrace = 20 * time.Second

	// writeTimeout bounds one write to a page. Generous next to
	// eventPeriod, so a busy machine is not mistaken for a dead browser.
	writeTimeout = 5 * time.Second

	// pumpStopTimeout is how long Run waits for the loop that paints the
	// panels to finish its last frame. Long enough to cover a device
	// request that is already in flight.
	pumpStopTimeout = 15 * time.Second

	// requestTimeout bounds reading a request. Nothing this server accepts
	// is large or slow.
	requestTimeout = 10 * time.Second

	// idleConnTimeout drops a connection nothing is being asked over. It
	// does not touch the event stream, which is one long response rather
	// than an idle connection.
	idleConnTimeout = 2 * time.Minute

	// maxBody is the largest request body accepted. The biggest one is a
	// handful of numbers.
	maxBody = 4 << 10

	// maxClients bounds how many pages may stream at once. A person uses
	// one; the cap is there so a reloading page cannot pile up streams.
	maxClients = 8

	// sendGrace is how long the panels may refuse frames before the
	// session gives up on them.
	//
	// Not the first failure: the device leaves streaming mode whenever
	// anything else selects an effect on it -- the Nanoleaf app, a home
	// automation, or the display itself starting up in the middle of a
	// calibration -- and the next datagram comes back refused. That is a
	// state to recover from and report on the page, not one to exit on.
	sendGrace = 10 * time.Second
)

// Shape is one arrangement the page can draw.
type Shape struct {
	// Name identifies it in a request. It may be empty when a session has
	// only one shape.
	Name string
	// Label says what it is, for the page's list.
	Label string
	// Layout is the arrangement, as a device reports it.
	Layout nanoleaf.Layout
}

// Options configures a Server.
type Options struct {
	// Shapes are the arrangements the page may draw, the first being the
	// one it opens on.
	//
	// A session driving a real device has exactly one: the panels on the
	// wall. A session showing samples has a list, and the page offers a
	// picker -- which is what makes it possible to see what the display
	// looks like on a honeycomb of hexagons or a grid of squares without
	// owning either.
	Shapes []Shape

	// Rotation is the angle the session starts at.
	Rotation int

	// Open takes the panels over. It is called once, when Run starts, and
	// again whenever the device drops the stream.
	//
	// Nil means there is no device: the page is live and the pictures are
	// computed, but nothing is sent anywhere. That is the mode the sample
	// shapes run in.
	//
	// The session opens the stream rather than being handed one because
	// enabling streaming mode is the only step that changes the device
	// and cannot be undone without knowing which effect was selected
	// before. Everything that can fail -- the port, the layout, the
	// configuration file -- has to fail in New, which happens first.
	Open Open

	// Save persists an accepted rotation. It is called from a request
	// handler, so it must be safe to call at any time.
	//
	// Nil means a rotation cannot be saved, which is the honest answer for
	// a shape that is not on the user's wall. The page then offers no
	// button for it.
	Save func(rotation int) error

	// ConfigPath is the file Save writes, shown on the page so a user can
	// see where the value went.
	ConfigPath string

	// DisplayRunning says whether the daemon is streaming to the same
	// device, which the page warns about: two senders fight over the
	// panels and the picture flickers between them.
	DisplayRunning bool

	// Port is the loopback port to listen on. Zero picks a free one.
	Port int
}

// Server is one calibration session: a page on the loopback interface, and
// the panels following whatever that page is showing.
type Server struct {
	opt Options

	// pics is one drawing per shape, by name, built up front so a shape
	// that cannot be drawn is refused before a page opens rather than
	// when someone picks it.
	pics map[string]*picture

	ln   net.Listener
	host string

	// token authorises every route. It is never printed and never passed
	// to another program: it reaches the browser as a redirect, and lives
	// after that in the address bar and in same-origin request URLs.
	token string

	// ticket is the one-time code in the address the terminal prints.
	// Redeeming it hands out the token; see newSecrets.
	ticket      string
	ticketSpent bool

	mu    sync.Mutex
	state view
	snap  Snapshot
	saved *int

	// clients is how many pages are streaming, and idleSince is when the
	// last one went away. Together they end the session when the browser
	// is closed, so the panels are not left showing a calibration pattern
	// because someone forgot the terminal.
	clients   int
	idleSince time.Time
	connected bool

	reason string

	// trouble is the last thing the panels said when they refused a
	// frame, and failingSince is when they started refusing.
	trouble      string
	failingSince time.Time

	// How long the session may sit with no page attached, before one has
	// ever connected and after the last one has gone. Fields rather than
	// constants so a test does not have to wait out a real grace period.
	openGrace  time.Duration
	closeGrace time.Duration

	// sendGrace is how long the panels may keep refusing frames before
	// the session gives up on them.
	sendGrace time.Duration

	done     chan struct{}
	stopOnce sync.Once
}

// New prepares a session. The listener is opened here, so a port already in
// use is reported before the panels are touched.
//
// ctx bounds opening the listener only; the session's own lifetime is the
// context passed to Run.
func New(ctx context.Context, o Options) (*Server, error) {
	if len(o.Shapes) == 0 {
		return nil, errors.New("webui: at least one shape is required")
	}
	if o.Port < 0 || o.Port > 65535 {
		return nil, fmt.Errorf("webui: port %d is not a port number", o.Port)
	}

	pics := make(map[string]*picture, len(o.Shapes))
	for _, shape := range o.Shapes {
		pic, err := newPicture(shape.Layout)
		if err != nil {
			return nil, fmt.Errorf("webui: shape %q: %w", shape.Name, err)
		}
		pics[shape.Name] = pic
	}

	// The page can change what the wall is showing and can write to the
	// configuration file, so it is never offered beyond this machine. A
	// flag to bind elsewhere is deliberately absent.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Port)))
	if err != nil {
		return nil, fmt.Errorf("webui: listen on loopback: %w", err)
	}

	token, ticket, err := newSecrets()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}

	s := &Server{
		opt:        o,
		pics:       pics,
		token:      token,
		ticket:     ticket,
		ln:         ln,
		host:       ln.Addr().String(),
		idleSince:  time.Now(),
		openGrace:  openGrace,
		closeGrace: closeGrace,
		sendGrace:  sendGrace,
		done:       make(chan struct{}),
		state: view{
			Shape:    o.Shapes[0].Name,
			Rotation: render.WrapDegrees(o.Rotation),
			Mode:     ModePattern,
			Level:    0.6,
			Phase:    render.PhaseIdle,
		},
	}
	first := pics[o.Shapes[0].Name]
	s.snap = first.snapshot(s.state, first.frame(s.state, 0))
	return s, nil
}

// newSecrets makes the two codes a session needs.
//
// Two rather than one, because the address has to be handed to a browser as a
// command-line argument, and on Linux /proc/<pid>/cmdline is world readable:
// any other user on the machine can read the arguments of the browser process
// for as long as it lives. A single code in that address would therefore be
// readable by anyone on the machine, and this page can repaint a wall of
// lights and write to a configuration file.
//
// So the printed address carries a ticket that works exactly once. Redeeming
// it redirects to the real token, which never appears in anyone's process
// arguments. Someone reading the browser's arguments finds a code that has
// already been spent, and spending it themselves first is a race they can
// only win by being noticed: the user's browser then says so instead of
// opening the page.
func newSecrets() (token, ticket string, err error) {
	both := make([]string, 2)
	for i := range both {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return "", "", fmt.Errorf("webui: generate session codes: %w", err)
		}
		both[i] = base64.RawURLEncoding.EncodeToString(raw)
	}
	return both[0], both[1], nil
}

// ticketState is what came of an attempt to redeem a ticket.
type ticketState int

const (
	// ticketWrong means the code is not this session's.
	ticketWrong ticketState = iota
	// ticketFresh means it was this session's and had not been used.
	ticketFresh
	// ticketSpent means it was this session's and has been used already.
	ticketSpent
)

// redeem exchanges the printed ticket for the session, once.
func (s *Server) redeem(code string) ticketState {
	if !codeMatches(code, s.ticket) {
		return ticketWrong
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ticketSpent {
		return ticketSpent
	}
	s.ticketSpent = true
	return ticketFresh
}

// codeMatches compares a code from a request with the session's, in constant
// time. The comparison is short and local, but a timing-safe compare costs
// nothing and removes the question.
func codeMatches(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// URL is the address to open. It carries the one-time ticket, so it is the
// only address fit to print or to hand to another program.
func (s *Server) URL() string { return "http://" + s.host + "/t/" + s.ticket + "/" }

// pageURL is where the ticket leads: the address the page itself runs at.
func (s *Server) pageURL() string { return "/" + s.token + "/" }

// Close releases the listener without serving, for a caller that gives up
// between New and Run. Run closes it on its own.
func (s *Server) Close() error {
	if err := s.ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("webui: close listener: %w", err)
	}
	return nil
}

// Result says how a finished session went.
type Result struct {
	// Rotation is the angle the shape was left at.
	Rotation int
	// Saved is the angle written to the configuration file, if any.
	Saved *int
	// Reason is why the session ended, in a form fit to print.
	Reason string
}

// Result reports the outcome. Safe to call once Run has returned.
func (s *Server) Result() Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Result{Rotation: s.state.Rotation, Saved: s.saved, Reason: s.reason}
}

// Run serves the page until the browser goes away, the page says it is done,
// or ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	// The one destructive step, and the first thing that happens after
	// every other failure has already been ruled out by New. A session
	// with no device skips it and paints nothing.
	var stream Stream
	if s.opt.Open != nil {
		opened, err := Reopening(ctx, s.opt.Open)
		if err != nil {
			return err
		}
		stream = opened
		// Runs after the wait for the pump below, so the panels are
		// never given up while a frame is still being painted.
		defer func() { _ = opened.Close() }()
	}

	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: requestTimeout,
		IdleTimeout:       idleConnTimeout,
	}

	// Buffered, so a goroutine that finishes after Run has stopped
	// selecting does not leak waiting to report.
	errc := make(chan error, 2)
	pumpStopped := make(chan struct{})
	go func() {
		defer close(pumpStopped)
		errc <- s.pump(ctx, stream)
	}()
	go func() {
		err := srv.Serve(s.ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errc <- err
	}()

	var runErr error
	select {
	case <-ctx.Done():
		s.stop("interrupted")
	case <-s.done:
	case runErr = <-errc:
	}

	// Closing done first releases the streaming handlers. Shutdown waits
	// for handlers to return and would otherwise sit there for as long as
	// a page is open.
	s.stop("stopped")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("webui: shut down: %w", err)
	}

	// The pump owns the device, so Run must not return while it is still
	// running. The caller hands the panels back the moment this returns,
	// and a pump one tick behind would take them straight back -- leaving
	// them in streaming mode with the effect they were restored to
	// already spent, which is the one state this program cannot recover
	// from. The Stream refuses a send after it is closed as well; this is
	// the half that keeps the two from overlapping at all.
	select {
	case <-pumpStopped:
	case <-time.After(pumpStopTimeout):
		if runErr == nil {
			runErr = errors.New("webui: the panel loop did not stop; the panels may still be streaming")
		}
	}
	return runErr
}

// stop ends the session, keeping the first reason given.
func (s *Server) stop(reason string) {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.reason = reason
		s.mu.Unlock()
		close(s.done)
	})
}

// pump paints the panels and keeps the latest picture ready for the page.
//
// Everything that touches the device happens here, in one goroutine: a
// Streamer reuses its encoding buffer, so it must not be shared with a
// request handler.
func (s *Server) pump(ctx context.Context, stream Stream) error {
	ticker := time.NewTicker(FramePeriod)
	defer ticker.Stop()

	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		case <-ticker.C:
		}

		s.mu.Lock()
		state := s.state
		s.mu.Unlock()

		pic := s.pics[state.Shape]
		frame := pic.frame(state, time.Since(start))

		// A session with no device paints nothing: the page is the
		// whole display.
		if stream != nil {
			if err := stream.Send(frame); err != nil {
				if gaveUp := s.noteTrouble(err); gaveUp {
					return err
				}
			} else {
				s.clearTrouble()
			}
		}

		snap := pic.snapshot(state, frame)

		// Read after the send, not before: a send can take a moment,
		// and deciding on who was watching a moment ago could end the
		// session just as the browser arrives.
		s.mu.Lock()
		snap.Trouble = s.trouble
		s.snap = snap
		alone := s.clients == 0 && time.Since(s.idleSince) > s.graceLocked()
		s.mu.Unlock()

		if alone {
			s.stop(s.idleReason())
			return nil
		}
	}
}

// noteTrouble records panels that refused a frame, reporting whether they
// have been refusing for long enough to give up on.
func (s *Server) noteTrouble(err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failingSince.IsZero() {
		s.failingSince = time.Now()
	}
	s.trouble = err.Error()
	return time.Since(s.failingSince) > s.sendGrace
}

// clearTrouble forgets a failure the panels have recovered from.
func (s *Server) clearTrouble() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trouble = ""
	s.failingSince = time.Time{}
}

// graceLocked is how long the session may sit with no page attached. The
// caller must hold the lock.
func (s *Server) graceLocked() time.Duration {
	if s.connected {
		return s.closeGrace
	}
	return s.openGrace
}

// idleReason says which kind of silence ended the session: a browser that
// never arrived, or one that has gone.
func (s *Server) idleReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connected {
		return "the page was closed"
	}
	return "no browser opened the page"
}

// snapshot is the latest picture.
func (s *Server) snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}
