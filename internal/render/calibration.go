package render

import (
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// Calibration colours, picked to be unmistakable from across a room and
// nameable without hedging: nobody has to judge a shade.
var (
	calibrateBottom = nanoleaf.RGB{R: 0, G: 255, B: 0}
	calibrateTop    = nanoleaf.RGB{R: 255, G: 0, B: 0}
	calibrateMiddle = nanoleaf.RGB{R: 0, G: 0, B: 90}
)

// Bands of the shape the calibration picture divides the wall into. The
// middle third is deliberately wide, so a shape that is nearly right does not
// look right.
const (
	calibrateLow  = 0.34
	calibrateHigh = 0.66
)

// Calibration is the picture used to check which way the shape is mounted:
// green along the bottom, red along the top, and a dim blue between them.
//
// It lives here, next to the renderer, because both the terminal dial and the
// calibration page paint it. Two copies of the picture would let the wall and
// the screen disagree, which is the one thing a calibration tool cannot do.
func Calibration(geo Geometry) nanoleaf.Frame {
	frame := make(nanoleaf.Frame, len(geo.Points))
	for _, p := range geo.Points {
		switch {
		case p.V < calibrateLow:
			frame[p.PanelID] = calibrateBottom
		case p.V > calibrateHigh:
			frame[p.PanelID] = calibrateTop
		default:
			frame[p.PanelID] = calibrateMiddle
		}
	}
	return frame
}
