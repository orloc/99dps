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

// sessionsList: the fight list, newest last, with the selected one a gold
// plaque. The full list is returned (no clip) — the Sessions viewport scrolls it.
func sessionsList(th theme, sessions []*session.CombatSession, selected, w int) string {
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
	return strings.Join(lines, "\n")
}

// splitCCtimers partitions timers into crowd control (mez/charm) and the rest.
func splitCCtimers(timers []gamestate.Timer) (cc, rest []gamestate.Timer) {
	for _, tm := range timers {
		if tm.Mez || tm.Charm {
			cc = append(cc, tm)
		} else {
			rest = append(rest, tm)
		}
	}
	return cc, rest
}

func bySoonest(ts []gamestate.Timer) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Expiry < ts[j].Expiry })
}

// groupByTargetTimers buckets timers by who they're on, ordering the targets by
// their soonest-to-expire timer (so the group about to drop floats up).
func groupByTargetTimers(ts []gamestate.Timer) (map[string][]gamestate.Timer, []string) {
	groups := map[string][]gamestate.Timer{}
	for _, tm := range ts {
		groups[tm.Target] = append(groups[tm.Target], tm)
	}
	soonest := func(g []gamestate.Timer) int64 {
		m := g[0].Expiry
		for _, t := range g {
			if t.Expiry < m {
				m = t.Expiry
			}
		}
		return m
	}
	order := make([]string, 0, len(groups))
	for t := range groups {
		order = append(order, t)
	}
	sort.SliceStable(order, func(i, j int) bool { return soonest(groups[order[i]]) < soonest(groups[order[j]]) })
	return groups, order
}

func urgencyColor(th theme, frac float64) string {
	switch {
	case frac <= 0.2:
		return "#e0564e" // red — about to fade
	case frac <= 0.5:
		return th.accent // gold
	default:
		return "#5fd37a" // green
	}
}

// timerLine is one buff/debuff: name + urgency-tinted countdown bar + remaining.
func timerLine(th theme, tm gamestate.Timer, now int64, w int) string {
	const nameW, timeW = 13, 6
	barCells := w - nameW - timeW - 4
	if barCells < 4 {
		barCells = 4
	}
	total, rem := tm.Expiry-tm.Start, tm.Expiry-now
	if rem < 0 {
		rem = 0
	}
	frac := 1.0
	if total > 0 {
		frac = float64(rem) / float64(total)
	}
	col := urgencyColor(th, frac)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		th.fg(th.text).Width(nameW).Render(truncate(tm.Spell, nameW)), " ",
		gradientBar(frac, barCells, col, col, th.track), " ",
		rightCell(mmss(rem), timeW, th.dim))
}

// ccLine is one crowd-control entry: mez (M, breaks on damage) or charm (⊗).
// When hovered it reserves room for a trailing ✕ and tints the name, signalling
// it's clickable to dismiss.
func ccLine(th theme, tm gamestate.Timer, now int64, w int, hovered bool) string {
	const nameW, timeW = 12, 6
	reserve := 0
	if hovered {
		reserve = 2 // " ✕"
	}
	barCells := w - nameW - timeW - 6 - reserve
	if barCells < 4 {
		barCells = 4
	}
	rem := tm.Expiry - now
	if rem < 0 {
		rem = 0
	}
	label, col := "M", "#e0564e"
	if tm.Charm {
		label, col = "⊗", "#c98ad6"
	}
	total := tm.Expiry - tm.Start
	frac := 1.0
	if total > 0 {
		frac = float64(rem) / float64(total)
	}
	nameStyle := th.fg(th.text)
	if hovered {
		nameStyle = th.fg(th.accent).Bold(true)
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top,
		th.fg(col).Bold(true).Render(label), " ",
		nameStyle.Width(nameW).Render(truncate(displayName(tm.Target), nameW)), " ",
		gradientBar(frac, barCells, col, col, th.track), " ",
		rightCell(mmss(rem), timeW, th.dim))
	if hovered {
		row += th.fg("#e0564e").Bold(true).Render(" ✕")
	}
	return row
}

// targetHeader renders a buff/debuff group's target name. When hovered it
// becomes a full-width accent plaque with a trailing ✕ — the click-to-dismiss
// affordance (terminals can't switch the OS cursor to a pointer, so this is how
// we telegraph that the name is interactive).
func targetHeader(th theme, target string, w int, hovered bool) string {
	name := displayName(target)
	if !hovered {
		return th.fg(th.text).Bold(true).Render(truncate(name, w))
	}
	name = truncate(name, w-2)
	pad := w - lipgloss.Width(name) - 1
	if pad < 1 {
		pad = 1
	}
	line := name + strings.Repeat(" ", pad) + "✕"
	return lipgloss.NewStyle().Foreground(lipgloss.Color(th.bg)).
		Background(lipgloss.Color(th.accent)).Bold(true).Render(truncate(line, w))
}

// timersBody renders the spell-timer panel: crowd control pinned at the top when
// ccInline, then buffs/debuffs grouped by target. (Enchanters move CC to their
// own column.) It returns a line→target map for hover/click-to-dismiss; hover is
// the target whose group is highlighted (with an ✕ affordance).
func timersBody(th theme, tr *gamestate.Tracker, w int, ccInline bool, hover string) (string, map[int]string) {
	if tr == nil {
		return th.fg(th.dim).Render("spell timers off\n(no spells_us.txt)"), nil
	}
	now := time.Now().Unix()
	cc, rest := splitCCtimers(tr.Active(now))
	if !ccInline {
		cc = nil
	}
	var lines []string
	targets := map[int]string{}
	if len(cc) > 0 {
		bySoonest(cc)
		lines = append(lines, th.fg(th.accent).Bold(true).Render("CROWD CONTROL"))
		for _, tm := range cc {
			targets[len(lines)] = tm.Target
			lines = append(lines, ccLine(th, tm, now, w, tm.Target == hover))
		}
		if len(rest) > 0 {
			lines = append(lines, "")
		}
	}
	if len(rest) == 0 {
		if len(cc) == 0 {
			return th.fg(th.dim).Render("No active spells."), nil
		}
		return strings.Join(lines, "\n"), targets
	}
	// buffs/debuffs grouped by who they're on — a bold target header, then that
	// target's spells indented beneath it (matches the gocui renderTimers layout).
	// Hovering any row of a group highlights its header so a click dismisses it.
	groups, order := groupByTargetTimers(rest)
	for _, tgt := range order {
		targets[len(lines)] = tgt
		lines = append(lines, targetHeader(th, tgt, w, tgt == hover))
		g := groups[tgt]
		bySoonest(g)
		for _, tm := range g {
			targets[len(lines)] = tgt
			lines = append(lines, "  "+timerLine(th, tm, now, w-2))
		}
	}
	return strings.Join(lines, "\n"), targets
}

// ccBody renders the enchanter's dedicated Crowd Control column (mez + charm),
// returning a line→target map for hover/click-to-dismiss.
func ccBody(th theme, tr *gamestate.Tracker, w int, hover string) (string, map[int]string) {
	if tr == nil {
		return th.fg(th.dim).Render("—"), nil
	}
	now := time.Now().Unix()
	cc, _ := splitCCtimers(tr.Active(now))
	if len(cc) == 0 {
		return th.fg(th.dim).Render("No crowd control."), nil
	}
	bySoonest(cc)
	var lines []string
	targets := map[int]string{}
	for _, tm := range cc {
		targets[len(lines)] = tm.Target
		lines = append(lines, ccLine(th, tm, now, w, tm.Target == hover))
	}
	return strings.Join(lines, "\n"), targets
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
