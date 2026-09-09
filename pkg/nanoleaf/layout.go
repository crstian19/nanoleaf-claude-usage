package nanoleaf

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// Shape identifiers the device reports for each panel.
//
// The list is Nanoleaf's own, from the layout section of their Open API
// documentation, and it covers the range: Light Panels, Rhythm, Canvas,
// Shapes, Elements, Lines, the 4D lightstrip and Skylight. Numbers 5, 6, 10,
// 11 and 13 are not in it, and 21 to 28 are not either.
//
// Getting one wrong is worse than not knowing it: a real panel treated as a
// blank leaves a dead spot in the middle of the display, and a blank treated
// as a panel does the same. Anything unnamed is still rendered and is
// reported by the layout command, so a person with a model newer than this
// list can see the number and skip it themselves.
const (
	ShapeLightPanel    = 0  // the original triangles, Light Panels and Aurora
	ShapeRhythm        = 1  // the Rhythm music module: no LEDs
	ShapeSquare        = 2  // Canvas
	ShapeSquareMaster  = 3  // the Canvas square with the controller in it
	ShapeSquarePassive = 4  // a Canvas square in passive mode
	ShapeHexagon       = 7  // Shapes
	ShapeTriangle      = 8  // Shapes
	ShapeMiniTriangle  = 9  // Shapes
	ShapeController    = 12 // the Shapes controller brick: no LEDs

	ShapeElementsHexagon = 14 // Elements, the wood-look hexagons
	ShapeElementsCorner  = 15 // one Elements hexagon reports six of these

	ShapeLinesConnector  = 16 // the hinge between two Lines bars: no LEDs
	ShapeLines           = 17 // a Lines bar
	ShapeLinesSingleZone = 18 // a Lines bar that reports its two halves
	ShapeControllerCap   = 19 // the cap on a Lines controller: no LEDs
	ShapePowerConnector  = 20 // where the power goes in: no LEDs

	ShapeLightstrip4D = 29 // a segment of the 4D lightstrip

	ShapeSkylight        = 30 // a Skylight ceiling panel
	ShapeSkylightPrimary = 31 // the Skylight panel with the controller in it
	ShapeSkylightPassive = 32 // a Skylight panel in passive mode
)

// blankShapes are the shapes known to have no LEDs. They appear in the layout
// like any other panel, and lighting them would put a hole in every frame.
//
// Deliberately short. Only the two this package is sure about are listed:
// a wrong entry here is as damaging as a missing one, and SkipShapes exists
// for the models it does not know.
var blankShapes = map[int]bool{
	ShapeRhythm:         true,
	ShapeController:     true,
	ShapeLinesConnector: true,
	ShapeControllerCap:  true,
	ShapePowerConnector: true,
}

// SkipShapes adds shape identifiers to treat as blanks, for a device whose
// model this package does not know. Set it before reading a layout.
//
// `nanoclaude layout` prints the identifier of every panel, so the number to
// put here is visible without guessing.
var SkipShapes = map[int]bool{}

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

// IsLight reports whether the panel actually emits light.
//
// The controller brick and the Rhythm module show up in a layout like any
// other panel and have no LEDs, so lighting them would put a dead spot in the
// middle of every frame.
func (p Panel) IsLight() bool {
	return !blankShapes[p.ShapeType] && !SkipShapes[p.ShapeType]
}

// ShapeName names a shape identifier, or reports it as unknown.
func ShapeName(t int) string {
	switch t {
	case ShapeLightPanel:
		return "light panel"
	case ShapeRhythm:
		return "rhythm module"
	case ShapeSquare:
		return "square"
	case ShapeSquareMaster:
		return "square (controller)"
	case ShapeSquarePassive:
		return "square (passive)"
	case ShapeHexagon:
		return "hexagon"
	case ShapeTriangle:
		return "triangle"
	case ShapeMiniTriangle:
		return "mini triangle"
	case ShapeController:
		return "controller"
	case ShapeElementsHexagon:
		return "elements hexagon"
	case ShapeElementsCorner:
		return "elements hexagon corner"
	case ShapeLinesConnector:
		return "lines connector"
	case ShapeLines:
		return "lines bar"
	case ShapeLinesSingleZone:
		return "lines bar (half)"
	case ShapeControllerCap:
		return "controller cap"
	case ShapePowerConnector:
		return "power connector"
	case ShapeLightstrip4D:
		return "4d lightstrip"
	case ShapeSkylight:
		return "skylight panel"
	case ShapeSkylightPrimary:
		return "skylight panel (controller)"
	case ShapeSkylightPassive:
		return "skylight panel (passive)"
	default:
		return fmt.Sprintf("unknown (type %d)", t)
	}
}

// Family is the product line a shape belongs to.
//
// It matters because panels only clip to panels of their own line. Two shapes
// can have the same edge length by coincidence -- an Elements hexagon and a
// Shapes triangle are both 134 -- and a wall that mixed them would look
// possible while being impossible to build.
type Family string

// The product lines, in the order Nanoleaf released them.
const (
	FamilyUnknown    Family = "unknown"
	FamilyLightPanel Family = "light panels"
	FamilyCanvas     Family = "canvas"
	FamilyShapes     Family = "shapes"
	FamilyElements   Family = "elements"
	FamilyLines      Family = "lines"
	Family4D         Family = "4d"
	FamilySkylight   Family = "skylight"
)

// FamilyOf reports which product line a shape belongs to.
func FamilyOf(t int) Family {
	switch t {
	case ShapeLightPanel, ShapeRhythm:
		return FamilyLightPanel
	case ShapeSquare, ShapeSquareMaster, ShapeSquarePassive:
		return FamilyCanvas
	case ShapeHexagon, ShapeTriangle, ShapeMiniTriangle, ShapeController:
		return FamilyShapes
	case ShapeElementsHexagon, ShapeElementsCorner:
		return FamilyElements
	case ShapeLinesConnector, ShapeLines, ShapeLinesSingleZone, ShapeControllerCap, ShapePowerConnector:
		return FamilyLines
	case ShapeLightstrip4D:
		return Family4D
	case ShapeSkylight, ShapeSkylightPrimary, ShapeSkylightPassive:
		return FamilySkylight
	default:
		return FamilyUnknown
	}
}

// IsKnownShape reports whether the identifier is one this package names.
func IsKnownShape(t int) bool {
	return !strings.HasPrefix(ShapeName(t), "unknown")
}

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
