package webui

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// reopenEvery is how often a lost stream is reopened. Slow enough that a
// device which has gone away is not asked twenty times a second, fast enough
// that the picture comes back while the user is still looking at the wall.
const reopenEvery = 500 * time.Millisecond

// Stream is somewhere to send frames: the device, as a session uses it.
type Stream interface {
	Send(nanoleaf.Frame) error
	Close() error
}

// Open opens a fresh stream to the device.
type Open func(context.Context) (Stream, error)

// ErrStreamClosed is returned by a Stream that has been given up.
var ErrStreamClosed = errors.New("webui: the panels have been handed back")

// reopening is a Stream that gets itself back when the device drops it.
//
// It exists because the device leaves streaming mode whenever anything else
// selects an effect on it, and the next datagram then comes back refused.
// That happens for ordinary reasons -- the Nanoleaf app, a home automation,
// or the display itself starting up in the middle of a calibration -- so a
// session recovers from it rather than ending on it. The daemon does the same
// thing for the same reason, with a different policy: it retries at frame
// rate and logs, while this backs off, reports the failure to the page, and
// eventually gives up.
//
// Unlike the daemon, it does not re-read the device's state when it reopens.
// The daemon has to, because it may have been disarmed for hours and the user
// may have chosen a new effect in the meantime. A calibration session saved
// the state the user wants back before it started, seconds ago, and re-reading
// it mid-session would capture the pseudo-effect the device reports while
// streaming -- which is how the real effect gets lost for good.
type reopening struct {
	// ctx is the session's own, not a request's: reopening is session
	// work, and it has to stop when the session does.
	ctx  context.Context
	open Open

	// mu serialises everything, which is what makes Close safe to call
	// from another goroutine than Send. Without it, closing the stream
	// while a send was in flight would race on the encoding buffer, and a
	// send that started before the close could put the panels back into
	// streaming mode after they had been handed back -- leaving them
	// stuck there with the effect they were restored to already spent.
	mu      sync.Mutex
	stream  Stream
	closed  bool
	lastErr error
	nextTry time.Time
}

// Reopening takes the panels over, and keeps them for as long as the session
// needs them.
func Reopening(ctx context.Context, open Open) (Stream, error) {
	stream, err := open(ctx)
	if err != nil {
		return nil, err
	}
	return &reopening{ctx: ctx, open: open, stream: stream}, nil
}

// Send paints one frame, reopening the stream if the device has dropped it.
func (r *reopening) Send(frame nanoleaf.Frame) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return ErrStreamClosed
	}
	if r.stream == nil {
		if time.Now().Before(r.nextTry) {
			return r.lastErr
		}
		r.nextTry = time.Now().Add(reopenEvery)
		stream, err := r.open(r.ctx)
		if err != nil {
			r.lastErr = err
			return err
		}
		r.stream = stream
	}

	if err := r.stream.Send(frame); err != nil {
		// A dropped datagram reports nothing on UDP, so an error here
		// means the socket itself is broken.
		_ = r.stream.Close()
		r.stream, r.lastErr = nil, err
		r.nextTry = time.Now().Add(reopenEvery)
		return err
	}
	return nil
}

// Close gives the panels up for good. Sending afterwards is refused rather
// than quietly taking them back.
func (r *reopening) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
	stream := r.stream
	r.stream = nil
	if stream == nil {
		return nil
	}
	return stream.Close()
}
