package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/eqmap"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// TestRenderMapCanvas: a map renders to exactly hCells lines, none wider than
// wCells, and the player marker is drawn.
func TestRenderMapCanvas(t *testing.T) {
	mp := &eqmap.Map{
		Segments: []eqmap.Segment{{X1: -100, Y1: -100, X2: 100, Y2: 100, R: 64, G: 64, B: 64}},
		MinX:     -100, MinY: -100, MaxX: 100, MaxY: 100,
	}
	out := renderMapCanvas(mp, 0, 0, 0, true, 1, false, 20, 10)
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Fatalf("want 10 cell rows, got %d", len(lines))
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) > 20 {
			t.Errorf("row %d wider than 20: %d", i, lipgloss.Width(ln))
		}
	}
	if !strings.Contains(out, playerMarker) { // the emoji "you are here" pin
		t.Error("player marker emoji should be drawn")
	}
}

// TestOnFloor: a segment within the band is on the reference floor; one a floor
// away (~50) is not, so it isn't drawn.
func TestOnFloor(t *testing.T) {
	if !onFloor(10, 0) {
		t.Error("a segment near the reference Z should be on this floor")
	}
	if onFloor(50, 0) {
		t.Error("a segment a floor away (~50) should not be on this floor")
	}
}

// TestFloorScoping: with two floors 50 apart, only the player's floor renders —
// the other is hidden entirely, so a multi-level zone doesn't overlay everything.
func TestFloorScoping(t *testing.T) {
	mp := &eqmap.Map{
		Segments: []eqmap.Segment{
			{X1: -50, Y1: -50, X2: 50, Y2: 50, Z: 0, R: 200, G: 200, B: 200},  // ground
			{X1: -50, Y1: 50, X2: 50, Y2: -50, Z: 50, R: 200, G: 200, B: 200}, // upper floor
		},
		MinX: -50, MinY: -50, MaxX: 50, MaxY: 50, MinZ: 0,
	}
	ground := renderMapCanvas(mp, 0, 0, 0, true, 1, true, 20, 10) // floor-scope on, on the ground floor
	if !strings.Contains(ground, "110;110;110") {                 // 200 darkened by inkDarken (0.55)
		t.Error("the player's own floor should render (as darkened ink)")
	}
	// the upper floor (Z=50) is a floor away → hidden when scoped to the ground;
	// standing on the upper floor renders different geometry.
	upper := renderMapCanvas(mp, 0, 0, 50, true, 1, true, 20, 10) // floor-scope on, on the upper floor
	if ground == upper {
		t.Error("standing on different floors should render different geometry")
	}

	// with floor-scoping OFF (the default), both floors render — more than scoped.
	all := renderMapCanvas(mp, 0, 0, 0, true, 1, false, 20, 10)
	if all == ground {
		t.Error("floor-scoping off should show more geometry than scoped")
	}

	// f toggles floor-scoping on the model (default off)
	m := NewModel(&session.SessionManager{}, nil, "X")
	if m.mapFloors {
		t.Error("floor-scoping should default off")
	}
	out, _ := m.updateMap(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if !out.(Model).mapFloors {
		t.Error("f should toggle floor-scoping on")
	}
}

// TestMapView_States: no-loc + no-map shows guidance; with a map it shows the
// zone, the /loc, and never overflows the width.
func TestMapView_States(t *testing.T) {
	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("You have entered Sebilis.", 1)
	m := NewModel(&session.SessionManager{}, tr, "X")

	// before a map loads: header + a hint, no overflow
	out := m.mapView(themes[0], 50, 20)
	if !strings.Contains(out, "Sebilis") || !strings.Contains(out, "no /loc yet") {
		t.Errorf("empty map view should show zone + no-loc hint:\n%s", out)
	}

	// with a /loc and a loaded map
	tr.Observe("Your Location is 50.0, -25.0, 3.0", 2) // EQ order Y,X,Z → worldX=-25, worldY=50
	m.curMap = &eqmap.Map{
		Segments: []eqmap.Segment{{X1: -100, Y1: -100, X2: 100, Y2: 100, R: 80, G: 80, B: 80}},
		MinX:     -100, MinY: -100, MaxX: 100, MaxY: 100,
	}
	out = m.mapView(themes[0], 50, 20)
	if !strings.Contains(out, "/loc -25, 50") {
		t.Errorf("loaded map view should show the /loc (worldX,worldY):\n%s", out)
	}
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 50 {
			t.Errorf("map view line exceeds width 50: %d", lipgloss.Width(ln))
		}
	}
}

// TestMaybeLoadMapCmd: fetches the zone's map (via an injected loader) and the
// resulting message lands it on the model; an unknown zone reports a status.
func TestMaybeLoadMapCmd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "sebilis.txt") {
			w.Write([]byte("L -10,-10,0, 10,10,0, 1,2,3\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("You have entered Sebilis.", 1)
	m := NewModel(&session.SessionManager{}, tr, "X")
	m.mapLoader = &eqmap.Loader{BaseURL: srv.URL, HTTP: srv.Client()}

	cmd := m.maybeLoadMapCmd()
	if cmd == nil || m.mapShort != "sebilis" {
		t.Fatalf("should request sebilis; short=%q cmd=%v", m.mapShort, cmd)
	}
	msg, ok := cmd().(mapLoadedMsg)
	if !ok || msg.m == nil || len(msg.m.Segments) != 1 {
		t.Fatalf("load should return one segment, got %+v", msg)
	}
	// feeding it back stores the map
	updated, _ := m.Update(msg)
	if updated.(Model).curMap == nil {
		t.Error("Update should store the loaded map")
	}

	// a second call for the same zone is a no-op (cached request)
	if cmd := m.maybeLoadMapCmd(); cmd != nil {
		t.Error("same-zone reload should be a no-op")
	}
}

// TestMapZoom: +/- adjust the zoom (clamped ≥1), and a higher zoom centered on
// the player renders differently (zoomed in) than the whole-zone fit.
func TestMapZoom(t *testing.T) {
	m := NewModel(&session.SessionManager{}, nil, "X")
	if m.mapZoom != 4 {
		t.Fatalf("default zoom = %v, want 4", m.mapZoom)
	}
	out, _ := m.updateMap(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	if z := out.(Model).mapZoom; z <= 4 {
		t.Errorf("+ should zoom in, got %v", z)
	}
	m.mapZoom = 1
	out, _ = m.updateMap(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")})
	if z := out.(Model).mapZoom; z < 1 {
		t.Errorf("zoom must clamp at 1, got %v", z)
	}

	// the mouse wheel zooms on the Map tab
	mw := NewModel(&session.SessionManager{}, nil, "X")
	mw.screen = screenMap
	mw.mapZoom = 4
	var tm tea.Model = mw
	tm, _ = tm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if z := tm.(Model).mapZoom; z <= 4 {
		t.Errorf("wheel up should zoom in, got %v", z)
	}
	tm, _ = tm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	tm, _ = tm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if z := tm.(Model).mapZoom; z >= 6 {
		t.Errorf("wheel down should zoom out, got %v", z)
	}

	mp := &eqmap.Map{
		Segments: []eqmap.Segment{{X1: -100, Y1: -100, X2: 100, Y2: 100, R: 80, G: 80, B: 80}},
		MinX:     -100, MinY: -100, MaxX: 100, MaxY: 100,
	}
	near := renderMapCanvas(mp, 20, 20, 0, true, 8, false, 30, 15) // zoomed in on the player
	far := renderMapCanvas(mp, 20, 20, 0, true, 1, false, 30, 15)  // whole-zone fit
	if near == far {
		t.Error("zooming in should change the rendered map")
	}
}

// TestMapTabReachable: the Map tab is selectable and renders.
func TestMapTabReachable(t *testing.T) {
	var m tea.Model = NewModel(&session.SessionManager{}, gamestate.NewTracker(&gamestate.Book{}), "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")}) // Meter,Sessions,Tools,Map
	if mm := m.(Model); mm.screen != screenMap {
		t.Fatalf("key 4 should open the Map tab, got %v", mm.screen)
	}
}
