package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/session"
)

// TestToolsTabRendersAndFits: the Tools tab is reachable (key 4), renders its
// placeholder, and never overflows the window width.
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
		if !strings.Contains(out, "Tools") {
			t.Errorf("w=%d: Tools tab should render its title", w)
		}
		for _, ln := range strings.Split(out, "\n") {
			if lipgloss.Width(ln) > w {
				t.Errorf("w=%d: tools line exceeds width (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}
