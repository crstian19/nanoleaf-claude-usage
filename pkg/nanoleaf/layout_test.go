package nanoleaf

import "testing"

// theRange is every shape in Nanoleaf's own layout documentation, with the
// product line it belongs to and whether it has LEDs.
//
// Written out here rather than derived, so that this test disagrees with the
// code when the code changes. The numbers are theirs; the two judgements are
// this program's.
var theRange = []struct {
	shapeType int
	family    Family
	lights    bool
}{
	{ShapeLightPanel, FamilyLightPanel, true},
	{ShapeRhythm, FamilyLightPanel, false},
	{ShapeSquare, FamilyCanvas, true},
	{ShapeSquareMaster, FamilyCanvas, true},
	{ShapeSquarePassive, FamilyCanvas, true},
	{ShapeHexagon, FamilyShapes, true},
	{ShapeTriangle, FamilyShapes, true},
	{ShapeMiniTriangle, FamilyShapes, true},
	{ShapeController, FamilyShapes, false},
	{ShapeElementsHexagon, FamilyElements, true},
	{ShapeElementsCorner, FamilyElements, true},
	{ShapeLinesConnector, FamilyLines, false},
	{ShapeLines, FamilyLines, true},
	{ShapeLinesSingleZone, FamilyLines, true},
	{ShapeControllerCap, FamilyLines, false},
	{ShapePowerConnector, FamilyLines, false},
	{ShapeLightstrip4D, Family4D, true},
	{ShapeSkylight, FamilySkylight, true},
	{ShapeSkylightPrimary, FamilySkylight, true},
	{ShapeSkylightPassive, FamilySkylight, true},
}

// TestTheWholeRangeIsNamed keeps the list complete and the numbers where
// Nanoleaf put them.
func TestTheWholeRangeIsNamed(t *testing.T) {
	if len(theRange) != 20 {
		t.Fatalf("the range has %d shapes in it, and the documentation lists 20", len(theRange))
	}

	seen := map[int]bool{}
	for _, want := range theRange {
		name := ShapeName(want.shapeType)
		if !IsKnownShape(want.shapeType) {
			t.Errorf("shape %d is reported as %q", want.shapeType, name)
		}
		if seen[want.shapeType] {
			t.Errorf("shape %d appears twice in the range", want.shapeType)
		}
		seen[want.shapeType] = true

		if got := FamilyOf(want.shapeType); got != want.family {
			t.Errorf("%s belongs to %q, want %q", name, got, want.family)
		}
		if got := (Panel{ShapeType: want.shapeType}).IsLight(); got != want.lights {
			t.Errorf("%s reports lights=%v, want %v", name, got, want.lights)
		}
	}
}

// TestTheNumbersNanoleafDoesNotUse pins the gaps. A shape that turns up in
// one of them is a model newer than this list, and it has to be reported as
// unknown rather than mistaken for something else.
func TestTheNumbersNanoleafDoesNotUse(t *testing.T) {
	for _, shapeType := range []int{5, 6, 10, 11, 13, 21, 25, 28, 33, 99} {
		if IsKnownShape(shapeType) {
			t.Errorf("shape %d is named %q, and the documentation does not list it",
				shapeType, ShapeName(shapeType))
		}
		if got := FamilyOf(shapeType); got != FamilyUnknown {
			t.Errorf("shape %d is put in %q", shapeType, got)
		}
		// Unknown means rendered, not skipped: a panel nobody knows
		// about is still a panel, and leaving it dark would be a hole
		// in the middle of the display.
		if !(Panel{ShapeType: shapeType}).IsLight() {
			t.Errorf("shape %d is treated as having no LEDs", shapeType)
		}
	}
}

// TestSkipShapesOverridesTheList is the escape hatch for a model this version
// does not know that turns out to have no LEDs.
func TestSkipShapesOverridesTheList(t *testing.T) {
	t.Cleanup(func() { SkipShapes = map[int]bool{} })

	SkipShapes = map[int]bool{99: true}
	if (Panel{ShapeType: 99}).IsLight() {
		t.Error("a shape named in SkipShapes is still lit")
	}
	if !(Panel{ShapeType: ShapeTriangle}).IsLight() {
		t.Error("skipping one shape stopped another from lighting")
	}
}
