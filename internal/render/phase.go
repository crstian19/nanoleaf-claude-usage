package render

import "fmt"

// String names a phase the way the commands and the calibration page spell
// it, so the name a user types and the name a page sends are the same list in
// one place.
func (p Phase) String() string {
	switch p {
	case PhaseIdle:
		return "idle"
	case PhaseThinking:
		return "thinking"
	case PhaseTool:
		return "tool"
	case PhaseError:
		return "error"
	default:
		return fmt.Sprintf("phase(%d)", int(p))
	}
}

// ParsePhase reads a phase name, listing the valid ones when it cannot.
func ParsePhase(name string) (Phase, error) {
	for _, p := range []Phase{PhaseIdle, PhaseThinking, PhaseTool, PhaseError} {
		if p.String() == name {
			return p, nil
		}
	}
	return PhaseIdle, fmt.Errorf("unknown phase %q (idle, thinking, tool, error)", name)
}

// WrapDegrees brings an angle into [0, 360), which is where every rotation
// this program stores or shows belongs.
func WrapDegrees(deg int) int {
	deg %= 360
	if deg < 0 {
		deg += 360
	}
	return deg
}
