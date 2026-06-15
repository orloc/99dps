package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// TestToolsTabRendersAndFits: the Tools tab is reachable, renders the roll
// widget, and never overflows the window width.
func TestToolsTabRendersAndFits(t *testing.T) {
	var m tea.Model = New(&session.SessionManager{}, nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	mm := m.(Model)
	if mm.screen != screenTools {
		t.Fatalf("key 3 should open Tools, got %v", mm.screen)
	}

	for _, w := range []int{60, 80, 120} {
		mm.w, mm.h, mm.ready = w, 30, true
		out := mm.View()
		if !strings.Contains(out, "Random Rolls") {
			t.Errorf("w=%d: Tools tab should render the roll widget", w)
		}
		for _, ln := range strings.Split(out, "\n") {
			if lipgloss.Width(ln) > w {
				t.Errorf("w=%d: tools line exceeds width (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}

// TestRollBoard: empty shows a hint; with rolls the high roll leads, marked ★,
// and every roller/value/range renders.
func TestRollBoard(t *testing.T) {
	th := themes[0]

	if out := rollBoard(th, nil, 60); !strings.Contains(out, "No rolls yet") {
		t.Errorf("empty board should show the hint, got %q", out)
	}

	rolls := []gamestate.Roll{
		{Roller: "Emandar", Low: 0, High: 1000, Value: 952},
		{Roller: "Trashromancer", Low: 0, High: 1000, Value: 409},
	}
	out := rollBoard(th, rolls, 60)
	if !strings.Contains(out, "★") {
		t.Error("the high roll should be marked ★")
	}
	for _, want := range []string{"Emandar", "952", "Trashromancer", "409", "(0–1000)", "2 rolls"} {
		if !strings.Contains(out, want) {
			t.Errorf("roll board missing %q in:\n%s", want, out)
		}
	}
	// the ★ marker precedes the winner (Emandar), not the runner-up
	if star := strings.Index(out, "★"); star < 0 || strings.Index(out, "Emandar") < star {
		t.Error("★ should mark the winning (first) row")
	}
}

// TestUpdateToolsClear: pressing c clears the roll board and flashes.
func TestUpdateToolsClear(t *testing.T) {
	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("**A Magic Die is rolled by Solo.", 1)
	tr.Observe("**It could have been any number from 0 to 100, but this time it turned up a 77.", 1)
	if len(tr.Rolls()) != 1 {
		t.Fatal("setup: expected one roll")
	}

	m := New(&session.SessionManager{}, tr, "X")
	m.screen = screenTools
	out, _ := m.updateTools(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(tr.Rolls()) != 0 {
		t.Error("pressing c should clear the rolls")
	}
	if mm := out.(Model); mm.status != "rolls cleared" {
		t.Errorf("expected a 'rolls cleared' flash, got %q", mm.status)
	}
}
