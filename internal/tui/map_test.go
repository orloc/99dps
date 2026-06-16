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
	out := renderMapCanvas(mp, 0, 0, true, 20, 10)
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Fatalf("want 10 cell rows, got %d", len(lines))
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) > 20 {
			t.Errorf("row %d wider than 20: %d", i, lipgloss.Width(ln))
		}
	}
	if !strings.Contains(out, "255;255;0") { // the yellow player marker
		t.Error("player marker should be drawn")
	}
}

// TestMapView_States: no-loc + no-map shows guidance; with a map it shows the
// zone, the /loc, and never overflows the width.
func TestMapView_States(t *testing.T) {
	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("You have entered Sebilis.", 1)
	m := New(&session.SessionManager{}, tr, "X")

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
	m := New(&session.SessionManager{}, tr, "X")
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

// TestMapTabReachable: the Map tab is selectable and renders.
func TestMapTabReachable(t *testing.T) {
	var m tea.Model = New(&session.SessionManager{}, gamestate.NewTracker(&gamestate.Book{}), "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")}) // Meter,Sessions,Tools,Map
	if mm := m.(Model); mm.screen != screenMap {
		t.Fatalf("key 4 should open the Map tab, got %v", mm.screen)
	}
}
