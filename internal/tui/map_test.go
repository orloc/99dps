package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/eq/gamestate"
	"99dps/internal/session"
)

// TestCompass8: a screen-space delta (dx right, dy down) maps to the matching arrow.
func TestCompass8(t *testing.T) {
	cases := []struct {
		dx, dy float64
		want   string
	}{
		{10, 0, "→"}, {0, 10, "↓"}, {-10, 0, "←"}, {0, -10, "↑"},
		{10, 10, "↘"}, {-10, -10, "↖"}, {-10, 10, "↙"}, {10, -10, "↗"},
	}
	for _, c := range cases {
		if got := compass8(c.dx, c.dy); got != c.want {
			t.Errorf("compass8(%v,%v) = %q, want %q", c.dx, c.dy, got, c.want)
		}
	}
}

// TestClassifyPOI: labels bucket into zone lines / NPCs / points.
func TestClassifyPOI(t *testing.T) {
	cases := map[string]poiKind{
		"to Chardok":       poiZone,
		"Zone In":          poiZone,
		"Acet (Merchant)":  poiNPC,
		"Beek (GM_Cleric)": poiNPC,
		"Ancient Totem":    poiPoint,
		"brew barrel":      poiPoint,
	}
	for label, want := range cases {
		if got := classifyPOI(label); got != want {
			t.Errorf("classifyPOI(%q) = %d, want %d", label, got, want)
		}
	}
}

// TestOrderedPOIsAndPanel: POIs sort by kind then distance; the panel groups them
// under headers, marks the selection, and fits the width.
func TestOrderedPOIsAndPanel(t *testing.T) {
	pts := []MapPoint{
		{X: 0, Y: -80, Label: "Ancient Totem"}, // point, near
		{X: -90, Y: -90, Label: "to Chardok"},  // zone line, far
		{X: 5, Y: 5, Label: "Acet (Merchant)"}, // npc, nearest
	}
	es := orderedPOIs(pts, 10, 10) // player at map (-10,-10)
	// kind order: zone, npc, point — regardless of distance
	if es[0].kind != poiZone || es[1].kind != poiNPC || es[2].kind != poiPoint {
		t.Fatalf("order = %v, want zone,npc,point", []poiKind{es[0].kind, es[1].kind, es[2].kind})
	}
	panel := poiPanel(themes[0], es, 1, 28, 20) // select the NPC
	for _, want := range []string{"POIs (3)", "ZONE LINES", "NPCS", "POINTS", "to Chardok", "Acet (Merchant)", "▸"} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel missing %q:\n%s", want, panel)
		}
	}
	for _, ln := range strings.Split(panel, "\n") {
		if lipgloss.Width(ln) > 28 {
			t.Errorf("panel line exceeds width 28 (%d): %q", lipgloss.Width(ln), ln)
		}
	}
}

// TestRenderMapCanvasPOI: a POI is drawn as a colored marker on the canvas.
func TestRenderMapCanvasPOI(t *testing.T) {
	md := &MapData{
		MinX: -100, MinY: -100, MaxX: 100, MaxY: 100,
		Segments: []MapSegment{{X1: -100, Y1: 0, X2: 100, Y2: 0, R: 80, G: 80, B: 80}},
		Points:   []MapPoint{{X: 0, Y: 0, Label: "Zone In"}},
	}
	out := renderMapCanvas(md, 0, 0, 0, true, 1, false, 30, 14, nil)
	r, g, b := poiRGB("Zone In")
	if !strings.Contains(out, fmt.Sprintf("38;2;%d;%d;%d", r, g, b)) {
		t.Errorf("canvas should contain the zone POI marker color (%d,%d,%d)", r, g, b)
	}
}

// fakeMapSource is a MapSource that serves a canned map without any network.
type fakeMapSource struct {
	short string
	data  *MapData
	err   error
}

func (f fakeMapSource) ShortName(string) string       { return f.short }
func (f fakeMapSource) Load(string) (*MapData, error) { return f.data, f.err }

// TestRenderMapCanvas: a map renders to exactly hCells lines, none wider than
// wCells, and the player marker is drawn.
func TestRenderMapCanvas(t *testing.T) {
	mp := &MapData{
		Segments: []MapSegment{{X1: -100, Y1: -100, X2: 100, Y2: 100, R: 64, G: 64, B: 64}},
		MinX:     -100, MinY: -100, MaxX: 100, MaxY: 100,
	}
	out := renderMapCanvas(mp, 0, 0, 0, true, 1, false, 20, 10, nil)
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
	mp := &MapData{
		Segments: []MapSegment{
			{X1: -50, Y1: -50, X2: 50, Y2: 50, Z: 0, R: 200, G: 200, B: 200},  // ground
			{X1: -50, Y1: 50, X2: 50, Y2: -50, Z: 50, R: 200, G: 200, B: 200}, // upper floor
		},
		MinX: -50, MinY: -50, MaxX: 50, MaxY: 50, MinZ: 0,
	}
	ground := renderMapCanvas(mp, 0, 0, 0, true, 1, true, 20, 10, nil) // floor-scope on, on the ground floor
	if !strings.Contains(ground, "110;110;110") {                      // 200 darkened by inkDarken (0.55)
		t.Error("the player's own floor should render (as darkened ink)")
	}
	// the upper floor (Z=50) is a floor away → hidden when scoped to the ground;
	// standing on the upper floor renders different geometry.
	upper := renderMapCanvas(mp, 0, 0, 50, true, 1, true, 20, 10, nil) // floor-scope on, on the upper floor
	if ground == upper {
		t.Error("standing on different floors should render different geometry")
	}

	// with floor-scoping OFF (the default), both floors render — more than scoped.
	all := renderMapCanvas(mp, 0, 0, 0, true, 1, false, 20, 10, nil)
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
	m.curMap = &MapData{
		Segments: []MapSegment{{X1: -100, Y1: -100, X2: 100, Y2: 100, R: 80, G: 80, B: 80}},
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

// TestMapView_SidePanel: a wide map with POIs + a /loc shows the selectable side
// panel and a selection pin (distinct from the player), and never overflows.
func TestMapView_SidePanel(t *testing.T) {
	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("You have entered Chardok.", 1)
	tr.Observe("Your Location is 10.0, 10.0, 0.0", 2)
	m := NewModel(&session.SessionManager{}, tr, "X")
	m.mapZoom = 1
	m.curMap = &MapData{
		MinX: -120, MinY: -120, MaxX: 120, MaxY: 120,
		Segments: []MapSegment{{X1: -120, Y1: -120, X2: 120, Y2: 120, R: 90, G: 90, B: 90}},
		Points: []MapPoint{
			{X: 100, Y: -100, Label: "to Burning Wood"},
			{X: 50, Y: 50, Label: "Acet (Merchant)"},
		},
	}
	out := m.mapView(themes[0], 90, 18)
	for _, want := range []string{"POIs (2)", "ZONE LINES", "to Burning Wood", poiPin, playerMarker} {
		if !strings.Contains(out, want) {
			t.Errorf("paneled map view missing %q", want)
		}
	}
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 90 {
			t.Errorf("paneled map line exceeds width 90 (%d)", lipgloss.Width(ln))
		}
	}
	// ↓ moves the selection (and thus the pin)
	moved, _ := m.updateMap(tea.KeyMsg{Type: tea.KeyDown})
	if moved.(Model).mapSel != 1 {
		t.Errorf("down should advance the POI selection, got %d", moved.(Model).mapSel)
	}
}

// TestMaybeLoadMapCmd: fetches the zone's map (via an injected source) and the
// resulting message lands it on the model; a same-zone reload is a no-op.
func TestMaybeLoadMapCmd(t *testing.T) {
	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("You have entered Sebilis.", 1)
	m := NewModel(&session.SessionManager{}, tr, "X")
	m.mapSrc = fakeMapSource{
		short: "sebilis",
		data:  &MapData{Segments: []MapSegment{{X1: -10, Y1: -10, X2: 10, Y2: 10, R: 1, G: 2, B: 3}}},
	}

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

	mp := &MapData{
		Segments: []MapSegment{{X1: -100, Y1: -100, X2: 100, Y2: 100, R: 80, G: 80, B: 80}},
		MinX:     -100, MinY: -100, MaxX: 100, MaxY: 100,
	}
	near := renderMapCanvas(mp, 20, 20, 0, true, 8, false, 30, 15, nil) // zoomed in on the player
	far := renderMapCanvas(mp, 20, 20, 0, true, 1, false, 30, 15, nil)  // whole-zone fit
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
