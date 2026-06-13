package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/session"
)

// sessionsLayout splits the Sessions tab: ~3/4 width for the stats table, the
// rest for the DPS breakdown. h is the shared full content height.
func (m Model) sessionsLayout() (tableW, breakW, h int) {
	innerW := m.w - 2
	if innerW < 8 {
		innerW = 8
	}
	h = m.h - 5 // outer pad + banner + tab bar + footer
	if h < 6 {
		h = 6
	}
	tableW = innerW * 3 / 4
	breakW = innerW - tableW - 1
	if breakW < 0 {
		breakW = 0
	}
	return tableW, breakW, h
}

// sessionsTable renders the per-session stats table as a fixed header line and a
// separately-scrolling body, plus a map from BODY line (0-based, no header) →
// session index. Columns drop as width shrinks (Top dealer, then Kills) so the
// row always fits w. The header is rendered outside the scroll viewport so it
// stays sticky.
func sessionsTable(th theme, sessions []*session.CombatSession, sel, w int) (header, body string, rows map[int]int) {
	rows = map[int]int{}
	showKills := w >= 56
	showTop := w >= 78
	idxW, durW, totW, dpsW, kW, topW := 3, 7, 8, 8, 5, 18

	used := idxW + 1 + durW + 1 + totW + 1 + dpsW
	if showKills {
		used += 1 + kW
	}
	if showTop {
		used += 1 + topW
	}
	nameW := w - used - 1
	if nameW < 6 {
		nameW = 6
	}

	line := func(idx, name, dur, tot, dps, kills, top string) string {
		s := fmt.Sprintf("%*s %-*s %*s %*s %*s", idxW, idx, nameW, truncate(name, nameW), durW, dur, totW, tot, dpsW, dps)
		if showKills {
			s += fmt.Sprintf(" %*s", kW, kills)
		}
		if showTop {
			s += fmt.Sprintf(" %-*s", topW, truncate(top, topW))
		}
		return s
	}

	header = th.fg(th.accentLo).Render(line("#", "Fight", "Dur", "Total", "DPS", "Kills", "Top dealer"))
	if len(sessions) == 0 {
		return header, th.fg(th.dim).Render(truncate("no sessions yet", max(w, 1))), rows
	}

	var b strings.Builder
	for i, cs := range sessions {
		dps := 0
		if sec := cs.Duration().Seconds(); sec > 0 {
			dps = int(float64(cs.Total()) / sec)
		}
		top, _ := cs.TopDealer()
		row := line(strconv.Itoa(i+1), cs.Name(), fmtDuration(cs.Duration()),
			humanize(cs.Total()), humanize(dps), strconv.Itoa(cs.Kills()), displayName(top))
		if i == sel {
			row = th.fg(th.accent).Bold(true).Render(row)
		} else {
			row = th.fg(th.text).Render(row)
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(row)
		rows[i] = i // body line i (0-based, no header) → session i
	}
	return header, b.String(), rows
}

// refreshSessions repopulates the Sessions tab (table + breakdown of the
// highlighted session). Audio cues stay live here too.
func (m *Model) refreshSessions() {
	m.sessions = m.sm.All()
	sel := m.effectiveSel()
	th := themes[m.theme]

	header, body, rowMap := sessionsTable(th, m.sessions, sel, m.vpSessTable.Width)
	m.sessHeader = header
	m.vpSessTable.SetContent(body)
	m.sessRows = rowMap

	var cur *session.CombatSession
	if sel >= 0 && sel < len(m.sessions) {
		cur = m.sessions[sel]
	}
	live := cur != nil && cur.EndTime().IsZero()
	m.vpSessBreak.SetContent(m.damageContent(cur, live, m.vpSessBreak.Width))

	m.announceCues()
	m.ensureSessRowVisible(sel)
}

// ensureSessRowVisible scrolls the table so the selected row stays in view (one
// line per session; the header is line 0).
func (m *Model) ensureSessRowVisible(sel int) {
	if sel < 0 {
		return
	}
	line := sel // body lines are 0-based (header is outside the viewport)
	top, h := m.vpSessTable.YOffset, m.vpSessTable.Height
	switch {
	case line < top:
		m.vpSessTable.SetYOffset(line)
	case line >= top+h:
		m.vpSessTable.SetYOffset(line - h + 1)
	}
}

// updateSessions handles keys while the Sessions tab is focused (tab-navigation
// keys are consumed earlier, in Update).
func (m Model) updateSessions(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "up", "k":
		if cur := m.effectiveSel(); cur > 0 {
			m.selected, m.follow = cur-1, false
		}
		m.refreshSessions()
	case "down", "j":
		cur := m.effectiveSel()
		if cur >= 0 && cur < len(m.sessions)-1 {
			m.selected = cur + 1
		}
		if m.selected >= len(m.sessions)-1 {
			m.follow = true
		}
		m.refreshSessions()
	case "end":
		m.follow = true
		m.refreshSessions()
	case "backspace":
		m.sm.Clear()
		m.selected, m.follow = 0, true
		m.flash("cleared sessions")
		m.refreshSessions()
	}
	return m, nil
}

// mouseSessions handles wheel scrolling (table vs breakdown by cursor) and a
// left-click on a table row to select that session.
func (m Model) mouseSessions(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	tableW, _, _ := m.sessionsLayout()
	if isWheel(msg.Button) {
		vp := &m.vpSessBreak
		if msg.X < 1+tableW {
			vp = &m.vpSessTable
		}
		var cmd tea.Cmd
		*vp, cmd = vp.Update(msg)
		return m, cmd
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if i, ok := m.sessTableAt(msg.X, msg.Y); ok {
			m.selected, m.follow = i, i == len(m.sessions)-1
			m.refreshSessions()
		}
	}
	return m, nil
}

// sessTableAt maps a click in the table card to a session index.
func (m Model) sessTableAt(x, y int) (int, bool) {
	tableW, _, h := m.sessionsLayout()
	if x < 1 || x >= 1+tableW {
		return 0, false
	}
	contentTop := gridTop + 3 // card border (1) + title (1) + sticky header (1)
	if y < contentTop || y >= gridTop+h-1 {
		return 0, false
	}
	ln := (y - contentTop) + m.vpSessTable.YOffset
	if i, ok := m.sessRows[ln]; ok {
		return i, true
	}
	return 0, false
}
