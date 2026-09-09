package nanoleaf

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// Shape identifiers reported by the device for each panel. Only the ones we
// care about telling apart are named.
const (
	ShapeTriangle     = 8
	ShapeMiniTriangle = 9
	ShapeController   = 12
)

// Panel is one physical light panel and where it sits on the wall.
//
// X and Y are centroid coordinates in the device's own layout units, with Y
// increasing upwards. The origin is arbitrary, so only relative positions
// carry meaning.
type Panel struct {
	ID          int `json:"panelId"`
	X           int `json:"x"`
	Y           int `json:"y"`
	Orientation int `json:"o"`
	ShapeType   int `json:"shapeType"`
}

// IsLight reports whether the panel actually emits light. The controller
// brick shows up in the layout as a panel but has no LEDs, and including it
// would put a dead spot in the middle of any rendered scene.
func (p Panel) IsLight() bool { return p.ShapeType != ShapeController }

// Addressable reports whether the panel's ID fits the wire format's 16-bit
// panel field.
//
// Every panel a real device reports does. This exists because a frame
// containing one bad ID is rejected whole, so a single out-of-range ID from a
// firmware quirk would blank every panel -- better to drop that one panel up
// front than to lose the display.
func (p Panel) Addressable() bool { return p.ID >= 0 && p.ID <= math.MaxUint16 }

// Layout is the physical arrangement of a device's panels.
type Layout struct {
	NumPanels  int     `json:"numPanels"`
	SideLength int     `json:"sideLength"`
	Panels     []Panel `json:"positionData"`

	// GlobalOrientation is how the whole arrangement is rotated, in
	// degrees.
	//
	// Panel coordinates are in the device's own frame, which has no idea
	// which way is up on the wall: it comes from however the panels were
	// arranged in the Nanoleaf app. Without applying this, a layout that
	// is wider than tall in device coordinates can be taller than wide in
	// reality -- so a vertical fill would climb sideways.
	GlobalOrientation int `json:"-"`
}

// Lights returns only the light-emitting panels, in device order.
func (l Layout) Lights() []Panel {
	out := make([]Panel, 0, len(l.Panels))
	for _, p := range l.Panels {
		if p.IsLight() {
			out = append(out, p)
		}
	}
	return out
}

// Layout fetches the device's panel arrangement.
//
// It reads /panelLayout rather than /panelLayout/layout because the global
// orientation is a sibling of the positions, not part of them, and a layout
// without it cannot be placed on a wall.
func (c *Client) Layout(ctx context.Context) (Layout, error) {
	var out struct {
		GlobalOrientation struct {
			Value int `json:"value"`
		} `json:"globalOrientation"`
		Layout Layout `json:"layout"`
	}
	if err := c.do(ctx, http.MethodGet, c.url("/panelLayout"), nil, &out); err != nil {
		return Layout{}, fmt.Errorf("fetch layout: %w", err)
	}
	if len(out.Layout.Panels) == 0 {
		return Layout{}, errors.New("fetch layout: device reported no panels")
	}

	layout := out.Layout
	layout.GlobalOrientation = out.GlobalOrientation.Value
	return layout, nil
}

// ModelLightPanels is the original Light Panels (Aurora). It is triangular
// like the Shapes Triangles and easy to mistake for them, but it only speaks
// extControl v1 on a different port -- so this package cannot drive it, and
// the failure would otherwise be silent: UDP never answers.
const ModelLightPanels = "NL22"

// Info identifies the device.
type Info struct {
	Name     string
	Model    string
	Firmware string
	Serial   string
}

// StreamsV2 reports whether the model is expected to accept extControl v2
// frames, which is what this package sends.
func (i Info) StreamsV2() bool { return i.Model != ModelLightPanels }

// Info reads the device's identity.
func (c *Client) Info(ctx context.Context) (Info, error) {
	var out struct {
		Name            string `json:"name"`
		Model           string `json:"model"`
		FirmwareVersion string `json:"firmwareVersion"`
		SerialNo        string `json:"serialNo"`
	}
	if err := c.do(ctx, http.MethodGet, c.url(""), nil, &out); err != nil {
		return Info{}, fmt.Errorf("fetch device info: %w", err)
	}
	return Info{
		Name:     out.Name,
		Model:    out.Model,
		Firmware: out.FirmwareVersion,
		Serial:   out.SerialNo,
	}, nil
}

// State is the subset of device state we save before taking over, so the
// device can be handed back looking the way the user left it.
type State struct {
	// On is whether the panels were powered.
	On bool
	// Brightness is the device's global brightness, 0-100.
	//
	// Read but never written by the display. Every colour sent over the
	// wire is scaled by it, so it is tempting to take it over and get a
	// predictable canvas. That was tried and it was wrong: the value
	// belongs to whoever set it, in the Nanoleaf app or in a home
	// automation, and a daemon that restarts whenever a session starts
	// would overwrite that choice several times an hour. See the
	// brightness command for setting it on purpose.
	Brightness int
	// Effect is the selected effect's name, which may be one of the
	// device's pseudo-effects -- see Restorable.
	Effect string
}

// Restorable reports whether the saved effect can be handed back to.
//
// Names beginning with "*" are the device's own pseudo-effects, extControl
// among them. Restoring "*ExtControl*" would be meaningless, and treating it
// as a real effect is actively harmful: it would overwrite the last known good
// effect with a placeholder and lose it permanently.
func (s State) Restorable() bool {
	return s.Effect != "" && !strings.HasPrefix(s.Effect, "*")
}

// State reads the device's current power, brightness and selected effect.
func (c *Client) State(ctx context.Context) (State, error) {
	var out struct {
		State struct {
			On struct {
				Value bool `json:"value"`
			} `json:"on"`
			Brightness struct {
				Value int `json:"value"`
			} `json:"brightness"`
		} `json:"state"`
		Effects struct {
			Select string `json:"select"`
		} `json:"effects"`
	}
	if err := c.do(ctx, http.MethodGet, c.url(""), nil, &out); err != nil {
		return State{}, fmt.Errorf("fetch state: %w", err)
	}
	return State{
		On:         out.State.On.Value,
		Brightness: out.State.Brightness.Value,
		Effect:     out.Effects.Select,
	}, nil
}

// SetOn powers the panels on or off.
func (c *Client) SetOn(ctx context.Context, on bool) error {
	body := map[string]any{"on": map[string]any{"value": on}}
	if err := c.do(ctx, http.MethodPut, c.url("/state"), body, nil); err != nil {
		return fmt.Errorf("set power: %w", err)
	}
	return nil
}

// SetBrightness sets the global brightness, 0-100. Every colour sent over the
// wire is scaled by it.
func (c *Client) SetBrightness(ctx context.Context, pct int) error {
	pct = min(max(pct, 0), 100)
	body := map[string]any{"brightness": map[string]any{"value": pct}}
	if err := c.do(ctx, http.MethodPut, c.url("/state"), body, nil); err != nil {
		return fmt.Errorf("set brightness: %w", err)
	}
	return nil
}

// SelectEffect switches the device to one of its stored effects, which is how
// control is handed back after streaming.
func (c *Client) SelectEffect(ctx context.Context, name string) error {
	body := map[string]any{"select": name}
	if err := c.do(ctx, http.MethodPut, c.url("/effects"), body, nil); err != nil {
		return fmt.Errorf("select effect %q: %w", name, err)
	}
	return nil
}
