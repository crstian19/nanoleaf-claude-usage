package webui

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/crstian19/nanoleaf-claude-usage/internal/shapes"
	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

// buildKinds is the palette the tests build with: the Shapes family, which is
// the set that genuinely clicks together.
func buildKinds() []Kind {
	return []Kind{
		{Shape: nanoleaf.ShapeTriangle, Label: "Triangle"},
		{Shape: nanoleaf.ShapeHexagon, Label: "Hexagon"},
		{Shape: nanoleaf.ShapeMiniTriangle, Label: "Mini triangle"},
	}
}

// newBuildServer is a session with a wall the page can build, which means a
// session with no device.
func newBuildServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(t.Context(), Options{
		Shapes: []Shape{{Name: "sample", Label: "a sample", Layout: realLayout(t)}},
		Build:  buildKinds(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// The page starts on the sample; every test here is about the other
	// one.
	if code := call(t, s, http.MethodPost, s.pageURL()+"state",
		`{"shape":"`+BuildShape+`"}`).Code; code != http.StatusOK {
		t.Fatalf("selecting the wall to build returned %d", code)
	}
	return s
}

// edit sends one change and returns the response, for a test to check.
func edit(t *testing.T, s *Server, body string) int {
	t.Helper()
	return call(t, s, http.MethodPost, s.pageURL()+"edit", body).Code
}

// spots asks for the places a panel of this kind could go, the way the page
// does: by saying what it is dragging and reading the next picture.
func spots(t *testing.T, s *Server, shapeType int) []SpotView {
	t.Helper()
	if code := call(t, s, http.MethodPost, s.pageURL()+"state",
		`{"placing":`+strconv.Itoa(shapeType)+`}`).Code; code != http.StatusOK {
		t.Fatalf("picking up a panel returned %d", code)
	}

	s.mu.Lock()
	state := s.state
	pic := s.build.pic
	views := s.build.spotViews(state.Placing, frameOf(pic))
	s.mu.Unlock()
	return views
}

func panelsOn(t *testing.T, s *Server) int {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.build.wall().Panels()
}

// TestAWallIsBuiltByDroppingPanelsOnEdges is the whole feature: a panel is
// picked up, the places it can go are worked out here, and dropping it on one
// of them puts it there.
func TestAWallIsBuiltByDroppingPanelsOnEdges(t *testing.T) {
	s := newBuildServer(t)

	// An empty wall offers one place: the middle.
	first := spots(t, s, nanoleaf.ShapeTriangle)
	if len(first) != 1 {
		t.Fatalf("an empty wall offers %d places, want 1", len(first))
	}
	if first[0].Panel >= 0 {
		t.Errorf("the first place is attached to panel %d, want nothing", first[0].Panel)
	}
	if first[0].Points == "" {
		t.Error("the first place has no outline to show")
	}

	if code := edit(t, s, `{"action":"place","shape":8}`); code != http.StatusOK {
		t.Fatalf("placing the first panel returned %d", code)
	}
	if got := panelsOn(t, s); got != 1 {
		t.Fatalf("the wall has %d panels, want 1", got)
	}

	// A triangle then offers its three edges, and each of them is a place
	// with an outline the page can draw.
	edges := spots(t, s, nanoleaf.ShapeTriangle)
	if len(edges) != 3 {
		t.Fatalf("a triangle offers %d edges, want 3", len(edges))
	}
	for _, spot := range edges {
		if spot.Points == "" {
			t.Errorf("the place on edge %d has no outline", spot.Edge)
		}
		if spot.Half != shapes.NoHalf {
			t.Errorf("a triangle on a triangle reports half %d", spot.Half)
		}
	}

	target := edges[0]
	if code := edit(t, s, `{"action":"attach","shape":8,"panel":`+strconv.Itoa(target.Panel)+
		`,"edge":`+strconv.Itoa(target.Edge)+`,"half":-1}`); code != http.StatusOK {
		t.Fatalf("attaching to a place that was offered returned %d", code)
	}
	if got := panelsOn(t, s); got != 2 {
		t.Errorf("the wall has %d panels, want 2", got)
	}

	// That edge is no longer on offer, and dropping on it again is
	// refused rather than stacking two panels in one place.
	again := call(t, s, http.MethodPost, s.pageURL()+"edit",
		`{"action":"attach","shape":8,"panel":`+strconv.Itoa(target.Panel)+
			`,"edge":`+strconv.Itoa(target.Edge)+`,"half":-1}`)
	if again.Code != http.StatusBadRequest {
		t.Errorf("dropping on a taken edge returned %d, want 400", again.Code)
	}
	if !strings.Contains(again.Body.String(), "would land on") {
		t.Errorf("the page is told %q, which does not say what happened", again.Body)
	}
	if got := panelsOn(t, s); got != 2 {
		t.Errorf("a refused drop left %d panels, want 2", got)
	}
}

// TestMiniTrianglesGetHalfAnEdgeEach is the case where the panel being
// dragged is smaller than the edge it lands on.
func TestMiniTrianglesGetHalfAnEdgeEach(t *testing.T) {
	s := newBuildServer(t)
	if code := edit(t, s, `{"action":"place","shape":8}`); code != http.StatusOK {
		t.Fatalf("placing a triangle returned %d", code)
	}

	minis := spots(t, s, nanoleaf.ShapeMiniTriangle)
	if len(minis) != 6 {
		t.Fatalf("a triangle offers %d places to a mini, want 6", len(minis))
	}

	placed := 0
	for _, spot := range minis {
		if spot.Half != 0 && spot.Half != 1 {
			t.Errorf("a mini's place reports half %d", spot.Half)
		}
		if spot.Edge != minis[0].Edge {
			continue
		}
		code := edit(t, s, `{"action":"attach","shape":9,"panel":`+strconv.Itoa(spot.Panel)+
			`,"edge":`+strconv.Itoa(spot.Edge)+`,"half":`+strconv.Itoa(spot.Half)+`}`)
		if code != http.StatusOK {
			t.Fatalf("attaching a mini to half an edge returned %d", code)
		}
		placed++
	}
	if placed != 2 {
		t.Fatalf("placed %d minis on one edge, want 2", placed)
	}
	if got := panelsOn(t, s); got != 3 {
		t.Errorf("the wall has %d panels, want 3", got)
	}
}

// TestUndoAndClearGoBack keeps a wall recoverable: a wrong panel is one
// button away from being gone.
func TestUndoAndClearGoBack(t *testing.T) {
	s := newBuildServer(t)
	edit(t, s, `{"action":"place","shape":7}`)

	hexes := spots(t, s, nanoleaf.ShapeHexagon)
	for _, spot := range hexes {
		edit(t, s, `{"action":"attach","shape":7,"panel":`+strconv.Itoa(spot.Panel)+
			`,"edge":`+strconv.Itoa(spot.Edge)+`,"half":-1}`)
	}
	if got := panelsOn(t, s); got != 7 {
		t.Fatalf("a honeycomb came out with %d panels, want 7", got)
	}

	// Taking one off, then putting it back with undo.
	s.mu.Lock()
	layout, _ := s.build.wall().Layout()
	s.mu.Unlock()
	last := layout.Panels[len(layout.Panels)-1].ID

	if code := edit(t, s, `{"action":"remove","panel":`+strconv.Itoa(last)+`}`); code != http.StatusOK {
		t.Fatalf("removing a panel returned %d", code)
	}
	if got := panelsOn(t, s); got != 6 {
		t.Errorf("the wall has %d panels after a removal, want 6", got)
	}
	if code := edit(t, s, `{"action":"undo"}`); code != http.StatusOK {
		t.Fatalf("undo returned %d", code)
	}
	if got := panelsOn(t, s); got != 7 {
		t.Errorf("the wall has %d panels after undoing the removal, want 7", got)
	}

	if code := edit(t, s, `{"action":"clear"}`); code != http.StatusOK {
		t.Fatalf("clear returned %d", code)
	}
	if got := panelsOn(t, s); got != 0 {
		t.Errorf("the wall has %d panels after being cleared, want none", got)
	}
	if code := edit(t, s, `{"action":"undo"}`); code != http.StatusBadRequest {
		t.Errorf("undoing an empty wall returned %d, want 400", code)
	}
}

// TestOnlyThePaletteCanBeDropped stops a page inventing a panel the wall
// cannot hold, whatever it sends.
func TestOnlyThePaletteCanBeDropped(t *testing.T) {
	s := newBuildServer(t)

	for _, body := range []string{
		`{"action":"place","shape":2}`,
		`{"action":"place"}`,
		`{"action":"attach","shape":8}`,
		`{"action":"remove"}`,
		`{"action":"levitate","shape":8}`,
		`{"action":"place","shape":8,"colour":"red"}`,
	} {
		if code := edit(t, s, body); code != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", body, code)
		}
	}

	// And a kind that is not on the palette cannot be picked up either.
	if code := call(t, s, http.MethodPost, s.pageURL()+"state", `{"placing":2}`).Code; code != http.StatusBadRequest {
		t.Errorf("picking up a square returned %d, want 400", code)
	}
}

// TestThePageIsToldItCanBuild covers what the palette is drawn from.
func TestThePageIsToldItCanBuild(t *testing.T) {
	s := newBuildServer(t)

	var info Info
	if err := json.Unmarshal(call(t, s, http.MethodGet, s.pageURL()+"info", "").Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.BuildShape != BuildShape {
		t.Errorf("the page is told the wall to build is %q, want %q", info.BuildShape, BuildShape)
	}
	if len(info.Kinds) != len(buildKinds()) {
		t.Errorf("the palette has %d kinds, want %d", len(info.Kinds), len(buildKinds()))
	}
	if info.Shape != BuildShape {
		t.Errorf("the page is on %q, want the wall it is building", info.Shape)
	}
	// The wall is empty, and the page has to be able to say so.
	if info.Lit != 0 {
		t.Errorf("an empty wall reports %d lit panels", info.Lit)
	}

	found := false
	for _, shape := range info.Shapes {
		if shape.Name == BuildShape && shape.Label == buildLabel {
			found = true
		}
	}
	if !found {
		t.Errorf("the wall to build is not in the list: %+v", info.Shapes)
	}
}

// TestAWallToBuildNeedsNoDevice pins the one combination that is refused: a
// wall someone invents has panel identities no device has, so it cannot be
// painted on one.
func TestAWallToBuildNeedsNoDevice(t *testing.T) {
	rec := &panels{}
	_, err := New(t.Context(), Options{
		Shapes: []Shape{{Layout: realLayout(t)}},
		Open:   rec.open,
		Save:   rec.save,
		Build:  buildKinds(),
	})
	if err == nil {
		t.Fatal("a session was allowed to build a wall and paint a device")
	}
	if !strings.Contains(err.Error(), "cannot be painted") {
		t.Errorf("the error is %q", err)
	}

	// A wall to build is enough on its own: it needs no arrangement
	// beside it, because it is one.
	if _, err := New(t.Context(), Options{Build: buildKinds()}); err != nil {
		t.Errorf("a session with only a wall to build was refused: %v", err)
	}
}

// TestAnEmptyWallIsStillAPicture keeps the page working before anything is on
// the wall: it draws by walking a list of panels, and a missing list is a
// broken page rather than an empty one.
func TestAnEmptyWallIsStillAPicture(t *testing.T) {
	s := newBuildServer(t)

	s.mu.Lock()
	snap := emptySnapshot(s.state, 134)
	s.mu.Unlock()

	if snap.Panels == nil {
		t.Error("an empty wall reports no list of panels at all")
	}
	if len(snap.Panels) != 0 {
		t.Errorf("an empty wall has %d panels", len(snap.Panels))
	}
	if snap.Extent <= 0 {
		t.Errorf("an empty wall has an extent of %v, so the page has no frame to draw in", snap.Extent)
	}

	// And the first panel can be dropped in the middle of it.
	if got := spots(t, s, nanoleaf.ShapeHexagon); len(got) != 1 {
		t.Errorf("an empty wall offers %d places, want 1", len(got))
	}
}

// TestOnlyTheKindsThatFitAreOffered is how the page says that two product
// lines do not clip together, without anyone having to read it: the panels
// with nowhere to go are greyed out.
func TestOnlyTheKindsThatFitAreOffered(t *testing.T) {
	rec := &panels{}
	s, err := New(t.Context(), Options{
		Build: []Kind{
			{Shape: nanoleaf.ShapeTriangle, Label: "Shapes triangle"},
			{Shape: nanoleaf.ShapeMiniTriangle, Label: "Mini triangle"},
			{Shape: nanoleaf.ShapeHexagon, Label: "Shapes hexagon"},
			{Shape: nanoleaf.ShapeSquare, Label: "Canvas square"},
			{Shape: nanoleaf.ShapeElementsHexagon, Label: "Elements hexagon"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	_ = rec

	read := func() []int {
		t.Helper()
		var info Info
		if err := json.Unmarshal(call(t, s, http.MethodGet, s.pageURL()+"info", "").Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		return info.Placeable
	}

	// An empty wall takes anything.
	if got := len(read()); got != 5 {
		t.Errorf("an empty wall offers %d of 5 kinds", got)
	}

	// A Shapes triangle on it, and now only the Shapes panels fit: a
	// hexagon and a mini triangle by their edge of 67, a triangle by its
	// own 134. An Elements hexagon is also 134 and still does not clip,
	// because it is another product line, and a Canvas square is neither.
	if code := edit(t, s, `{"action":"place","shape":8}`); code != http.StatusOK {
		t.Fatalf("placing a triangle returned %d", code)
	}

	want := map[int]bool{
		nanoleaf.ShapeTriangle:     true,
		nanoleaf.ShapeMiniTriangle: true,
		nanoleaf.ShapeHexagon:      true,
	}
	got := read()
	if len(got) != len(want) {
		t.Errorf("a wall of one triangle offers %v, want %v", got, want)
	}
	for _, shapeType := range got {
		if !want[shapeType] {
			t.Errorf("%s is offered on a wall of Shapes triangles", nanoleaf.ShapeName(shapeType))
		}
	}
}

// TestTheWallToBuildComesFirst is what the page opens on: an empty wall with
// the palette beside it, rather than somebody else's arrangement.
func TestTheWallToBuildComesFirst(t *testing.T) {
	s, err := New(t.Context(), Options{
		Shapes: []Shape{
			{Name: "sample", Label: "a sample", Layout: realLayout(t)},
			{Name: "other", Label: "another", Layout: realLayout(t)},
		},
		Build: buildKinds(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var info Info
	if err := json.Unmarshal(call(t, s, http.MethodGet, s.pageURL()+"info", "").Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Shapes) != 3 || info.Shapes[0].Name != BuildShape {
		t.Errorf("the list starts with %+v, want the wall to build", info.Shapes)
	}
	if info.Shape != BuildShape {
		t.Errorf("the page opens on %q, want the wall to build", info.Shape)
	}
	if s.snapshot().Shape != BuildShape {
		t.Errorf("the first picture is of %q", s.snapshot().Shape)
	}
}

// TestASessionCanOpenOnANamedShape covers `preview --shape`, which says where
// to start without changing the order everybody else sees.
func TestASessionCanOpenOnANamedShape(t *testing.T) {
	s, err := New(t.Context(), Options{
		Shapes: []Shape{
			{Name: "first", Label: "the first", Layout: realLayout(t)},
			{Name: "second", Label: "the second", Layout: realLayout(t)},
		},
		OpenOn: "second",
		Build:  buildKinds(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var info Info
	if err := json.Unmarshal(call(t, s, http.MethodGet, s.pageURL()+"info", "").Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Shape != "second" {
		t.Errorf("the page opens on %q, want the shape it was told", info.Shape)
	}
	if info.Shapes[0].Name != BuildShape {
		t.Errorf("the list starts with %q: opening on a shape moved it", info.Shapes[0].Name)
	}

	if _, err := New(t.Context(), Options{
		Shapes: []Shape{{Name: "first", Layout: realLayout(t)}},
		OpenOn: "nothing-like-it",
	}); err == nil {
		t.Error("a session was allowed to open on a shape it does not offer")
	}
}
