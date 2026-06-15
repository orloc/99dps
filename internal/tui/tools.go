package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/gamestate"
)

// The Tools tab is a 3-column home for little helper widgets — quick utilities
// that aren't part of the live meter, each in its own box. The first is a
// /random roll widget for loot: as people roll on a dropped item the live board
// shows who's leading; when the rolls stop (or you press c) the high roller is
// recorded to a winners log with the time and zone, and the board resets for the
// next item.

// updateTools handles input while the Tools tab is focused. (Tab/number nav is
// handled earlier, in Update.) c closes the current roll now; x clears the log.
func (m Model) updateTools(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok || m.tracker == nil {
		return m, nil
	}
	switch k.String() {
	case "c":
		m.tracker.FinalizeRolls()
		m.flash("roll closed")
	case "x":
		m.tracker.ClearRollWinners()
		m.flash("winners cleared")
	}
	return m, nil
}

// toolsView lays the Tools tab out as three equal boxed columns: the /random
// roll widget, then two reserved slots for future widgets.
func (m Model) toolsView(th theme, w, h int) string {
	colW := (w - 2) / 3 // two 1-space gaps between three columns
	if colW < 10 {
		colW = 10
	}
	lastW := w - 2 - 2*colW // give the remainder to the last column
	if lastW < colW {
		lastW = colW
	}

	var rolls []gamestate.Roll
	var winners []gamestate.RollWin
	if m.tracker != nil {
		rolls, winners = m.tracker.Rolls(), m.tracker.RollWinners()
	}
	roll := card(th, colW, h, "🎲 Rolls", rollBoard(th, rolls, winners, time.Now().Unix(), colW-4))
	soon := card(th, colW, h, "", th.fg(th.dim).Render(truncate("more widgets soon", colW-4)))
	soon2 := card(th, lastW, h, "", th.fg(th.dim).Render(truncate("more widgets soon", lastW-4)))
	return lipgloss.JoinHorizontal(lipgloss.Top, roll, " ", soon, " ", soon2)
}

// rollBoard renders the roll widget body to width w: the live contest leaderboard
// (high roll first, leader marked ★) over a log of past winners (value · age ·
// zone). Width-aware so it fits a narrow column. Pure data→string for testing.
func rollBoard(th theme, current []gamestate.Roll, winners []gamestate.RollWin, now int64, w int) string {
	var ls []string

	// live contest
	if len(current) == 0 {
		ls = append(ls, th.fg(th.dim).Render(truncate("No active roll.", w)))
	} else {
		ls = append(ls, th.fg(th.dim).Render(truncate(fmt.Sprintf("rolling · %d–%d", current[0].Low, current[0].High), w)))
		for i, r := range current {
			marker, style := "  ", th.fg(th.text)
			if i == 0 { // current leader
				marker, style = "★ ", th.fg(th.accent).Bold(true)
			}
			ls = append(ls, style.Render(nameValue(marker, r.Roller, fmt.Sprintf("%d", r.Value), w)))
		}
	}

	// winners log
	if len(winners) > 0 {
		ls = append(ls, "", th.fg(th.accent).Render(truncate("recent winners", w)))
		for _, win := range winners {
			line := fmt.Sprintf("%s %d · %s", win.Roller, win.Value, compactDur(now-win.At))
			if win.Zone != "" {
				line += " · " + win.Zone
			}
			ls = append(ls, th.fg(th.dim).Render(truncate(line, w)))
		}
	}

	ls = append(ls, "", th.fg(th.dim).Render(truncate("c close · x clear log", w)))
	return lines(ls...)
}

// nameValue renders "<marker><name> <value>" with the value right-justified and
// the name truncated to fit width w.
func nameValue(marker, name, value string, w int) string {
	nameW := max(w-lipgloss.Width(marker)-lipgloss.Width(value)-1, 1)
	return truncate(fmt.Sprintf("%s%-*s %s", marker, nameW, truncate(name, nameW), value), w)
}
