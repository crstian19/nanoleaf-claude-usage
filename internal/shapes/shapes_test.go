package shapes

import (
	"math"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// TestEverySampleLooksLikeADevice checks the samples against what a real
// layout guarantees, since every one of them is fed to code that trusts a
// device: unique IDs, a panel count that matches, a side length, and shapes
// this program can name.
func TestEverySampleLooksLikeADevice(t *testing.T) {
	for _, s := range All() {
		t.Run(s.Name, func(t *testing.T) {
			if s.Label == "" {
				t.Error("no label")
			}
			if s.Layout.NumPanels != len(s.Layout.Panels) {
				t.Errorf("reports %d panels and has %d", s.Layout.NumPanels, len(s.Layout.Panels))
			}
			if s.Layout.SideLength <= 0 {
				t.Errorf("side length is %d", s.Layout.SideLength)
			}

			seen := map[int]bool{}
			var lights int
			for _, p := range s.Layout.Panels {
				if seen[p.ID] {
					t.Errorf("panel ID %d appears twice", p.ID)
				}
				seen[p.ID] = true

				if !p.Addressable() {
					t.Errorf("panel ID %d does not fit the wire format", p.ID)
				}
				if !nanoleaf.IsKnownShape(p.ShapeType) {
					t.Errorf("panel %d has shape %d, which this program cannot name",
						p.ID, p.ShapeType)
				}
				if p.IsLight() {
					lights++
				}
			}
			if lights < 4 {
				t.Errorf("only %d panels can be lit; a sample has to be worth looking at", lights)
			}
		})
	}
}

// TestNoTwoPanelsShareAPlace catches a tiling built with the wrong step,
// which would otherwise draw panels on top of each other.
func TestNoTwoPanelsShareAPlace(t *testing.T) {
	for _, s := range All() {
		t.Run(s.Name, func(t *testing.T) {
			panels := s.Layout.Lights()
			for i := range panels {
				for j := i + 1; j < len(panels); j++ {
					dx := float64(panels[i].X - panels[j].X)
					dy := float64(panels[i].Y - panels[j].Y)
					// A third of the smallest panel any sample
					// uses. The threshold is not taken from
					// the layout's own side length because a
					// mixed set holds two sizes, and this test
					// must not re-implement which is which.
					if gap := math.Hypot(dx, dy); gap < float64(miniSide)/3 {
						t.Errorf("panels %d and %d are %.1f apart", panels[i].ID, panels[j].ID, gap)
					}
				}
			}
		})
	}
}

func TestNamedReportsWhatItHas(t *testing.T) {
	for _, want := range Names() {
		if got, err := Named(want); err != nil || got.Name != want {
			t.Errorf("Named(%q) = %q, %v", want, got.Name, err)
		}
	}

	_, err := Named("dodecahedrons")
	if err == nil {
		t.Fatal("an unknown shape was accepted")
	}
	// The error is the only place a user finds the list, so it has to
	// carry it.
	for _, name := range Names() {
		if !contains(err.Error(), name) {
			t.Errorf("the error does not mention %q: %v", name, err)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestDefaultIsTheFirstOffered(t *testing.T) {
	if Default().Name != All()[0].Name {
		t.Errorf("Default is %q and the first sample is %q", Default().Name, All()[0].Name)
	}
}
