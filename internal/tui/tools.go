package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/gamestate"
)

// The Tools tab is a home for little helper widgets — quick utilities that aren't
// part of the live meter. The first is a /random winner board for loot rolls:
// the tracker pairs EQ's two-line /random output, and this lists the rolls with
// the high roll on top.

// updateTools handles input while the Tools tab is focused. (Tab/number nav is
// handled earlier, in Update.) `c` clears the roll board between contests.
func (m Model) updateTools(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "c" && m.tracker != nil {
		m.tracker.ClearRolls()
		m.flash("rolls cleared")
	}
	return m, nil
}

// toolsView renders the Tools tab body: the /random winner board.
func (m Model) toolsView(th theme, w int) string {
	var rolls []gamestate.Roll
	if m.tracker != nil {
		rolls = m.tracker.Rolls()
	}
	return rollBoard(th, rolls, w)
}

// rollBoard renders the /random winner list (high roll first, marked ★). Pure
// data→string so it's unit-tested directly.
func rollBoard(th theme, rolls []gamestate.Roll, w int) string {
	ls := []string{paint(th, th.accent, "🎲 Random Rolls", w)}
	if len(rolls) == 0 {
		ls = append(ls, "", paint(th, th.dim, "No rolls yet — /random in game to populate.", w))
		return lines(ls...)
	}
	ls = append(ls, paint(th, th.dim, fmt.Sprintf("%d rolls · press c to clear", len(rolls)), w), "")
	for i, r := range rolls {
		marker, style := "  ", th.fg(th.text)
		if i == 0 { // the high roll wins
			marker, style = "★ ", th.fg(th.accent).Bold(true)
		}
		line := fmt.Sprintf("%s%-16s %5d  (%d–%d)", marker, truncate(r.Roller, 16), r.Value, r.Low, r.High)
		ls = append(ls, style.Render(truncate(line, w)))
	}
	return lines(ls...)
}
