package webui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// fakeDevice is a stream that can be told to refuse frames, the way a device
// does once something else has selected an effect on it.
type fakeDevice struct {
	opens  int
	sends  int
	closes int
	fail   error
	refuse error // returned by open
}

func (f *fakeDevice) open(context.Context) (Stream, error) {
	if f.refuse != nil {
		return nil, f.refuse
	}
	f.opens++
	return f, nil
}

func (f *fakeDevice) Send(nanoleaf.Frame) error {
	f.sends++
	return f.fail
}

func (f *fakeDevice) Close() error {
	f.closes++
	return nil
}

var errDropped = errors.New("write: connection refused")

func openStream(t *testing.T, device *fakeDevice) *reopening {
	t.Helper()
	stream, err := Reopening(t.Context(), device.open)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := stream.(*reopening)
	if !ok {
		t.Fatalf("Reopening returned a %T", stream)
	}
	return r
}

// TestStreamGetsThePanelsBack is the ordinary case: the device leaves
// streaming mode, the frame after that is refused, and the session recovers
// instead of ending.
func TestStreamGetsThePanelsBack(t *testing.T) {
	device := &fakeDevice{}
	stream := openStream(t, device)

	device.fail = errDropped
	if err := stream.Send(nanoleaf.Frame{1: {}}); !errors.Is(err, errDropped) {
		t.Fatalf("a refused frame reported %v", err)
	}
	if device.closes != 1 {
		t.Errorf("the broken stream was closed %d times, want 1", device.closes)
	}

	// Still inside the backoff: the device is not asked again yet, and the
	// caller keeps being told what went wrong.
	if err := stream.Send(nanoleaf.Frame{1: {}}); !errors.Is(err, errDropped) {
		t.Errorf("during the backoff the error became %v", err)
	}
	if device.opens != 1 {
		t.Errorf("the device was reopened %d times during the backoff, want 0 extra", device.opens-1)
	}

	device.fail = nil
	stream.nextTry = time.Now().Add(-time.Second)
	if err := stream.Send(nanoleaf.Frame{1: {}}); err != nil {
		t.Fatalf("after the backoff the frame still failed: %v", err)
	}
	if device.opens != 2 {
		t.Errorf("the device was opened %d times, want 2", device.opens)
	}
}

// TestStreamWillNotTakeThePanelsBackAfterClose is the invariant that matters
// most in this file.
//
// Enabling streaming mode is the only step that changes the device and cannot
// be undone: the effect it replaces is only known to whoever saved it. So a
// stream that has been given up must refuse to reopen, whatever calls it and
// whenever -- including a render loop one tick behind the shutdown.
func TestStreamWillNotTakeThePanelsBackAfterClose(t *testing.T) {
	device := &fakeDevice{}
	stream := openStream(t, device)

	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if device.closes != 1 {
		t.Errorf("Close closed the device %d times, want 1", device.closes)
	}

	for range 3 {
		if err := stream.Send(nanoleaf.Frame{1: {}}); !errors.Is(err, ErrStreamClosed) {
			t.Fatalf("a send after Close reported %v, want ErrStreamClosed", err)
		}
	}
	if device.opens != 1 {
		t.Errorf("the panels were taken over again after being handed back (%d opens)", device.opens)
	}
	if err := stream.Close(); err != nil {
		t.Errorf("closing twice reported %v", err)
	}
}

// TestStreamReportsADeviceThatWillNotOpen keeps the failure at the start,
// where nothing has been changed yet.
func TestStreamReportsADeviceThatWillNotOpen(t *testing.T) {
	device := &fakeDevice{refuse: errDropped}
	if _, err := Reopening(t.Context(), device.open); !errors.Is(err, errDropped) {
		t.Fatalf("opening reported %v, want the device error", err)
	}
}
