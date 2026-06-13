package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSessionsTableContent(t *testing.T) {
	sm := sampleManager()
	sessions := sm.All()
	header, body, rows := sessionsTable(themes[0], sessions, 0, 90)
	for _, want := range []string{"Fight", "Total", "DPS"} {
		if !strings.Contains(header, want) {
			t.Errorf("header missing %q\n%s", want, header)
		}
	}
	if !strings.Contains(body, "a sand giant") {
		t.Errorf("body missing the fight name\n%s", body)
	}
	if len(rows) != len(sessions) {
		t.Errorf("row map = %d entries, want %d", len(rows), len(sessions))
	}
	if rows[0] != 0 { // body line 0 → session 0 (the header is separate now)
		t.Errorf("body line 0 should map to session 0, got %d", rows[0])
	}
}

func TestSessionsTableEmpty(t *testing.T) {
	_, body, rows := sessionsTable(themes[0], nil, -1, 60)
	if len(rows) != 0 || !strings.Contains(body, "no sessions") {
		t.Errorf("empty table should say so with no rows, got %q / %v", body, rows)
	}
}

func TestSessionsTabFitsWindow(t *testing.T) {
	for _, w := range []int{60, 90, 140} {
		var m tea.Model = New(sampleManager(), nil, "X")
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		mm := m.(Model)
		mm.screen = screenSessions
		mm.refreshSessions()
		for _, ln := range strings.Split(mm.View(), "\n") {
			if lipgloss.Width(ln) > w {
				t.Errorf("w=%d: sessions line exceeds width (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}

func TestSessTableAt(t *testing.T) {
	var m tea.Model = New(twoSessionManager(), nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.screen = screenSessions
	mm.refreshSessions()
	if len(mm.sessRows) == 0 {
		t.Fatal("no session rows")
	}
	contentTop := gridTop + 3 // border + title + sticky header
	for ln, idx := range mm.sessRows {
		y := contentTop + (ln - mm.vpSessTable.YOffset)
		if got, ok := mm.sessTableAt(2, y); !ok || got != idx {
			t.Errorf("sessTableAt(2,%d) = (%d,%v), want %d", y, got, ok, idx)
		}
	}
	// a click outside the table column misses
	if _, ok := mm.sessTableAt(9999, contentTop); ok {
		t.Error("a click past the table should miss")
	}
}
