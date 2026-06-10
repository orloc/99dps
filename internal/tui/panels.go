package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// card wraps body in a themed rounded panel of total size w×h, with a gold
// title. Lipgloss handles the border/padding/fill; content is clipped to fit.
func card(th theme, w, h int, title, body string) string {
	cw, ch := w-2, h-2 // border adds 2 in each axis
	if cw < 6 {
		cw = 6
	}
	if ch < 1 {
		ch = 1
	}
	titleLine := th.fg(th.accent).Bold(true).Render(truncate(title, cw-2))
	content := lipgloss.JoinVertical(lipgloss.Left, titleLine, body)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(th.accent)).
		Background(lipgloss.Color(th.panel)).
		Width(cw).Height(ch).Padding(0, 1).MaxHeight(h).MaxWidth(w).
		Render(content)
}

// nowBox: character, class/level, current zone, and the zone-wide kill rate.
func nowBox(th theme, character string, tr *gamestate.Tracker, w int) string {
	lines := []string{th.fg(th.text).Bold(true).Render(truncate(character, w))}
	if tr != nil {
		cl := ""
		if lv := tr.Level(); lv > 0 {
			cl = fmt.Sprintf("L%d ", lv)
		}
		if c := tr.Class(); c != eqclass.ClassUnknown {
			cl += string(c)
		}
		if cl != "" {
			lines = append(lines, th.fg(th.dim).Render(truncate(cl, w)))
		}
		if z := tr.Zone(); z != "" {
			lines = append(lines, th.fg(th.accent).Render(truncate("◆ "+z, w)))
		}
		if k, ph, _ := tr.ZoneKillStats(time.Now().Unix()); k > 0 {
			lines = append(lines, th.fg(th.dim).Render(fmt.Sprintf("%d kills · %d/hr", k, ph)))
		}
	}
	return strings.Join(lines, "\n")
}

// sessionsList: the fight list, newest last, with the selected one a gold plaque.
func sessionsList(th theme, sessions []*session.CombatSession, selected, w, h int) string {
	if len(sessions) == 0 {
		return th.fg(th.dim).Render("No fights yet.\nFight something!")
	}
	var lines []string
	for i, s := range sessions {
		live := i == len(sessions)-1 && s.EndTime().IsZero()
		name := s.Name()
		if live {
			name += " ●"
		}
		marker, nameStyle := "  ", th.fg(th.text)
		if i == selected {
			marker = "▸ "
			nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(th.bg)).
				Background(lipgloss.Color(th.accent)).Bold(true).Width(w)
		}
		lines = append(lines, nameStyle.Render(truncate(marker+name, w)))
		meta := fmt.Sprintf("  %s · %s", fmtDuration(s.Duration()), humanize(s.Total()+s.MagicTotal()))
		lines = append(lines, th.fg(th.dim).Render(truncate(meta, w)))
	}
	if len(lines) > h {
		lines = lines[:h] // simple clip; per-panel scroll is a later phase
	}
	return strings.Join(lines, "\n")
}

// timersList: active spell timers as countdown bars, urgency-tinted.
func timersList(th theme, tr *gamestate.Tracker, w int) string {
	if tr == nil {
		return th.fg(th.dim).Render("spell timers off\n(no spells_us.txt)")
	}
	now := time.Now().Unix()
	active := tr.Active(now)
	if len(active) == 0 {
		return th.fg(th.dim).Render("No active spells.")
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].Expiry < active[j].Expiry })

	const nameW, timeW = 13, 6
	barCells := w - nameW - timeW - 2 - 2
	if barCells < 4 {
		barCells = 4
	}
	var lines []string
	for _, tm := range active {
		total := tm.Expiry - tm.Start
		rem := tm.Expiry - now
		if rem < 0 {
			rem = 0
		}
		frac := 1.0
		if total > 0 {
			frac = float64(rem) / float64(total)
		}
		col := "#5fd37a" // green
		switch {
		case frac <= 0.2:
			col = "#e0564e" // red — about to fade
		case frac <= 0.5:
			col = th.accent // gold
		}
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
			th.fg(th.text).Width(nameW).Render(truncate(tm.Spell, nameW)), " ",
			gradientBar(frac, barCells, col, col, th.track), " ",
			rightCell(mmss(rem), timeW, th.dim)))
	}
	return strings.Join(lines, "\n")
}

// mobTracker: the zone-aware repop list, the player's kills first.
func mobTracker(th theme, tr *gamestate.Tracker, w int) string {
	if tr == nil {
		return th.fg(th.dim).Render("—")
	}
	rs := tr.Respawns(time.Now().Unix())
	if len(rs) == 0 {
		return th.fg(th.dim).Render("No kills tracked yet.")
	}
	const timeW = 6
	mobW := w - timeW - 1
	var lines []string
	for _, r := range rs {
		when, whenCol := mmss(r.Remaining), th.dim
		if r.Remaining <= 0 {
			when, whenCol = "UP", "#5fd37a"
		}
		nameCol := th.text
		if !r.Mine {
			nameCol = th.dim
		}
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
			th.fg(nameCol).Width(mobW).Render(truncate(r.Mob, mobW)), " ",
			rightCell(when, timeW, whenCol)))
	}
	return strings.Join(lines, "\n")
}
