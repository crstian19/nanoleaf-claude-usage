package daemon

import (
	"os"
	"testing"
	"time"
)

// atState points the state directory at a temporary one, so a test never
// touches the pause the person running it set.
func atState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// TestAPauseOutlivesTheProcess is the whole reason it is a file. A Claude
// Code hook starts the display and every other hook brings it back, so a
// pause held in memory would last until the next tool call.
func TestAPauseOutlivesTheProcess(t *testing.T) {
	atState(t)

	if paused, _, err := Paused(); err != nil || paused {
		t.Fatalf("a fresh machine reports paused=%v, %v", paused, err)
	}

	if err := Pause(time.Time{}); err != nil {
		t.Fatal(err)
	}
	paused, until, err := Paused()
	if err != nil {
		t.Fatal(err)
	}
	if !paused {
		t.Error("the display is not paused after pausing it")
	}
	if !until.IsZero() {
		t.Errorf("a pause with no end reports an end of %v", until)
	}

	// The file is what holds it, so it is still there for the next
	// process to read.
	path, err := PausePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the pause left nothing on disk: %v", err)
	}

	if err := Resume(); err != nil {
		t.Fatal(err)
	}
	if paused, _, err := Paused(); err != nil || paused {
		t.Errorf("still paused after resuming: %v, %v", paused, err)
	}

	// Resuming twice is what somebody does when they are not sure, and it
	// is the state they asked for either way.
	if err := Resume(); err != nil {
		t.Errorf("resuming an already running display failed: %v", err)
	}
}

// TestAPauseWithAnEndRunsOut covers `pause --for`, which is what somebody
// means by keeping the wall dark through a film.
func TestAPauseWithAnEndRunsOut(t *testing.T) {
	atState(t)

	if err := Pause(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	paused, until, err := Paused()
	if err != nil {
		t.Fatal(err)
	}
	if !paused {
		t.Error("a pause with an hour to run is not in force")
	}
	if time.Until(until) < 59*time.Minute {
		t.Errorf("the pause runs out at %v, want about an hour away", until)
	}

	// One that has run out is no pause, and does not need resuming.
	if err := Pause(time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if paused, _, err := Paused(); err != nil || paused {
		t.Errorf("a pause that ran out a minute ago is still in force: %v, %v", paused, err)
	}
}

// TestAPauseFileNobodyCanReadIsStillAPause keeps the failure on the safe
// side. The file exists for one reason, so a mangled one means the wall
// should be dark, not that the pause never happened.
func TestAPauseFileNobodyCanReadIsStillAPause(t *testing.T) {
	atState(t)

	path, err := PausePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := Pause(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tomorrow morning\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	paused, until, err := Paused()
	if err != nil {
		t.Fatalf("an unreadable pause is an error: %v", err)
	}
	if !paused {
		t.Error("an unreadable pause was taken as no pause at all")
	}
	if !until.IsZero() {
		t.Errorf("it reports an end of %v", until)
	}
}
