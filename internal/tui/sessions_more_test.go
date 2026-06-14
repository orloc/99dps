package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// readySessions builds a ready, sized model on the Sessions tab.
func readySessions(t *testing.T) Model {
	t.Helper()
	var m tea.Model = New(twoSessionManager(), nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.screen = screenSessions
	mm.refreshSessions()
	return mm
}

// TestUpdateSessionsNav drives keyboard nav on the Sessions tab: up/down move the
// selection (down at the end re-enters follow), end jumps to live.
func TestUpdateSessionsNav(t *testing.T) {
	mm := readySessions(t) // two sessions, follow → sel 1 (last)
	if mm.effectiveSel() != 1 {
		t.Fatalf("fresh model should follow the live (last) session, got %d", mm.effectiveSel())
	}

	// up → pins session 0, leaves follow
	r, _ := mm.updateSessions(tea.KeyMsg{Type: tea.KeyUp})
	mm = r.(Model)
	if mm.effectiveSel() != 0 || mm.follow {
		t.Errorf("up should pin session 0 and drop follow (sel=%d follow=%v)", mm.effectiveSel(), mm.follow)
	}

	// down → back to the last, re-entering follow
	r, _ = mm.updateSessions(tea.KeyMsg{Type: tea.KeyDown})
	mm = r.(Model)
	if mm.effectiveSel() != 1 || !mm.follow {
		t.Errorf("down to the last should re-enter follow (sel=%d follow=%v)", mm.effectiveSel(), mm.follow)
	}

	// pin 0, then End jumps back to live
	r, _ = mm.updateSessions(tea.KeyMsg{Type: tea.KeyUp})
	mm = r.(Model)
	r, _ = mm.updateSessions(tea.KeyMsg{Type: tea.KeyEnd})
	mm = r.(Model)
	if !mm.follow || mm.effectiveSel() != 1 {
		t.Errorf("End should jump to live (follow=%v sel=%d)", mm.follow, mm.effectiveSel())
	}
}

// TestUpdateSessionsBackspaceClears: backspace on the Sessions tab clears all
// sessions and flashes.
func TestUpdateSessionsBackspaceClears(t *testing.T) {
	mm := readySessions(t)
	r, _ := mm.updateSessions(tea.KeyMsg{Type: tea.KeyBackspace})
	mm = r.(Model)
	if len(mm.sessions) != 0 {
		t.Errorf("backspace should clear sessions, got %d", len(mm.sessions))
	}
	if mm.status == "" {
		t.Error("clearing should flash a status")
	}
}

// TestUpdateSessionsIgnoresNonKey: a non-key message is a no-op.
func TestUpdateSessionsIgnoresNonKey(t *testing.T) {
	mm := readySessions(t)
	r, _ := mm.updateSessions(tea.WindowSizeMsg{Width: 10, Height: 10})
	if r.(Model).effectiveSel() != mm.effectiveSel() {
		t.Error("a non-key message should not change the selection")
	}
}

// TestMouseSessionsSelectsRow: a left-click on a table row selects that session.
func TestMouseSessionsSelectsRow(t *testing.T) {
	mm := readySessions(t)
	if len(mm.sessRows) == 0 {
		t.Fatal("no session rows")
	}
	// find the screen row of session 0 and click it.
	contentTop := gridTop + 3
	line := -1
	for ln, idx := range mm.sessRows {
		if idx == 0 {
			line = ln
			break
		}
	}
	if line < 0 {
		t.Fatal("session 0 not in the row map")
	}
	y := contentTop + (line - mm.vpSessTable.YOffset)
	r, _ := mm.mouseSessions(tea.MouseMsg{X: 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	mm = r.(Model)
	if mm.selected != 0 || mm.follow {
		t.Errorf("clicking session 0 should pin it (selected=%d follow=%v)", mm.selected, mm.follow)
	}
}

// TestMouseSessionsWheelScrolls: a wheel event over the table column routes to the
// table viewport (no panic, returns the model).
func TestMouseSessionsWheelScrolls(t *testing.T) {
	mm := readySessions(t)
	r, _ := mm.mouseSessions(tea.MouseMsg{X: 2, Y: gridTop + 4, Button: tea.MouseButtonWheelDown})
	if r == nil {
		t.Error("wheel over the table should return a model")
	}
	// over the breakdown column too
	tableW, _, _ := mm.sessionsLayout()
	r, _ = mm.mouseSessions(tea.MouseMsg{X: 1 + tableW + 1, Y: gridTop + 4, Button: tea.MouseButtonWheelDown})
	if r == nil {
		t.Error("wheel over the breakdown should return a model")
	}
}

// TestEnsureSessRowVisibleScrolls: a selected row below the viewport scrolls the
// table down so the row is in view; a negative selection is a no-op.
func TestEnsureSessRowVisibleScrolls(t *testing.T) {
	mm := readySessions(t)
	mm.vpSessTable.Height = 1 // force scrolling for a >1 selection
	mm.vpSessTable.SetYOffset(0)
	mm.ensureSessRowVisible(1)
	if mm.vpSessTable.YOffset != 1 {
		t.Errorf("a row below the 1-line viewport should scroll to it, got offset %d", mm.vpSessTable.YOffset)
	}
	// a row above the viewport scrolls up to it.
	mm.vpSessTable.SetYOffset(5)
	mm.ensureSessRowVisible(0)
	if mm.vpSessTable.YOffset != 0 {
		t.Errorf("a row above the viewport should scroll up, got offset %d", mm.vpSessTable.YOffset)
	}
	// a negative selection is a no-op (offset unchanged).
	mm.vpSessTable.SetYOffset(1)
	before := mm.vpSessTable.YOffset
	mm.ensureSessRowVisible(-1)
	if mm.vpSessTable.YOffset != before {
		t.Errorf("a negative sel should not scroll, got offset %d (was %d)", mm.vpSessTable.YOffset, before)
	}
}

// TestSessionsLayoutTinyWindow: degenerate small sizes clamp to the floors
// rather than producing negative widths.
func TestSessionsLayoutTinyWindow(t *testing.T) {
	m := Model{w: 2, h: 2}
	tableW, breakW, h := m.sessionsLayout()
	if h < 6 {
		t.Errorf("height should floor at 6, got %d", h)
	}
	if tableW < 0 || breakW < 0 {
		t.Errorf("widths must not go negative (table=%d break=%d)", tableW, breakW)
	}
}
