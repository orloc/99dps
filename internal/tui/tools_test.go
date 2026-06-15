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
		if !strings.Contains(out, "Rolls") {
			t.Errorf("w=%d: Tools tab should render the roll widget", w)
		}
		for _, ln := range strings.Split(out, "\n") {
			if lipgloss.Width(ln) > w {
				t.Errorf("w=%d: tools line exceeds width (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}

// TestRollBoard: the live leader is marked ★ with the range header; the winners
// log shows value · age · zone; an idle board shows the hint.
func TestRollBoard(t *testing.T) {
	th := themes[0]
	now := int64(10_000)

	if out := rollBoard(th, nil, nil, now, 60); !strings.Contains(out, "No active roll") {
		t.Errorf("an idle board should show the no-roll hint, got %q", out)
	}

	current := []gamestate.Roll{
		{Roller: "Emandar", Low: 0, High: 1000, Value: 952},
		{Roller: "Trashromancer", Low: 0, High: 1000, Value: 409},
	}
	winners := []gamestate.RollWin{
		{Roller: "Belvinn", Value: 871, At: now - 120, Zone: "Sebilis"}, // 2m ago
	}
	out := rollBoard(th, current, winners, now, 60)
	if !strings.Contains(out, "★") || strings.Index(out, "Emandar") < strings.Index(out, "★") {
		t.Error("★ should mark the leading (first) live row")
	}
	for _, want := range []string{"rolling · 0–1000", "Emandar", "952", "Trashromancer", "409",
		"recent winners", "Belvinn", "871", "2m", "Sebilis"} {
		if !strings.Contains(out, want) {
			t.Errorf("roll board missing %q in:\n%s", want, out)
		}
	}
}

// TestUpdateToolsKeys: c closes the current contest (winner → log, board resets);
// x clears the winners log.
func TestUpdateToolsKeys(t *testing.T) {
	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("**A Magic Die is rolled by Solo.", 1)
	tr.Observe("**It could have been any number from 0 to 100, but this time it turned up a 77.", 1)
	if len(tr.Rolls()) != 1 {
		t.Fatal("setup: expected one live roll")
	}

	m := New(&session.SessionManager{}, tr, "X")
	m.screen = screenTools

	out, _ := m.updateTools(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(tr.Rolls()) != 0 || len(tr.RollWinners()) != 1 {
		t.Errorf("c should close the contest: live=%d winners=%d", len(tr.Rolls()), len(tr.RollWinners()))
	}
	if mm := out.(Model); mm.status != "roll closed" {
		t.Errorf("expected a 'roll closed' flash, got %q", mm.status)
	}

	out, _ = out.(Model).updateTools(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if len(tr.RollWinners()) != 0 {
		t.Error("x should clear the winners log")
	}
	if mm := out.(Model); mm.status != "winners cleared" {
		t.Errorf("expected a 'winners cleared' flash, got %q", mm.status)
	}
}
