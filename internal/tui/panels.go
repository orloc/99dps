package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"99dps/internal/combat"
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

// timeColW returns the width a right-aligned countdown column needs: the widest
// remaining time among ts (floored at min), so an h:mm:ss entry over an hour
// isn't clipped and all rows in the panel align.
func timeColW(now int64, min int, tss ...[]gamestate.Timer) int {
	w := min
	for _, ts := range tss {
		for _, tm := range ts {
			rem := tm.Expiry - now
			if rem < 0 {
				rem = 0
			}
			if v := lipgloss.Width(mmss(rem)); v > w {
				w = v
			}
		}
	}
	return w
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
	// soonest-to-expire first, breaking ties by name so the order is stable across
	// repaints (map iteration isn't) — otherwise a hovered row could resolve to a
	// different target on the next frame and a click would dismiss the wrong one.
	sort.SliceStable(order, func(i, j int) bool {
		si, sj := soonest(groups[order[i]]), soonest(groups[order[j]])
		if si != sj {
			return si < sj
		}
		return order[i] < order[j]
	})
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

// mobUrgencyColor tints a repop countdown by how imminent the pop is (absolute,
// since a respawn carries no total): up now → green, escalating gold→red as it
// approaches so an imminent spawn alerts; dim while still far off.
func mobUrgencyColor(th theme, rem int64) string {
	switch {
	case rem <= 0:
		return "#5fd37a" // up now
	case rem <= 30:
		return "#e0564e" // imminent — get to the spawn
	case rem <= 90:
		return th.accent // soon
	default:
		return th.dim // counting down
	}
}

// timerLine is one buff/debuff: spell name on the left, the remaining time on
// the right tinted by urgency (green healthy → gold → red near expiry). The time
// column (width tw, sized by the caller to the panel's longest time) always
// renders in full; the name takes the rest and truncates if the panel is narrow.
func timerLine(th theme, tm gamestate.Timer, now int64, w, tw int) string {
	total, rem := tm.Expiry-tm.Start, tm.Expiry-now
	if rem < 0 {
		rem = 0
	}
	timeStr := mmss(rem)
	if v := lipgloss.Width(timeStr); v > tw {
		tw = v
	}
	frac := 1.0
	if total > 0 {
		frac = float64(rem) / float64(total)
	}
	col := urgencyColor(th, frac)

	nameW := w - tw - 1 // the rest, minus the gap before the time
	if nameW < 1 {
		return rightCell(timeStr, w, col) // too narrow for anything but the time
	}
	return th.fg(th.text).Width(nameW).Render(truncate(tm.Spell, nameW)) + " " +
		rightCell(timeStr, tw, col)
}

// ccLine is one crowd-control entry: mez (M, breaks on damage) or charm (⊗).
// When hovered it reserves room for a trailing ✕ and tints the name, signalling
// it's clickable to dismiss.
func ccLine(th theme, tm gamestate.Timer, now int64, w int, hovered bool, tw int) string {
	rem := tm.Expiry - now
	if rem < 0 {
		rem = 0
	}
	timeStr := mmss(rem)
	if v := lipgloss.Width(timeStr); v > tw {
		tw = v
	}
	label, col := "M", "#e0564e"
	if tm.Charm {
		label, col = "⊗", "#c98ad6"
	}
	nameStyle := th.fg(th.text)
	if hovered {
		nameStyle = th.fg(th.accent).Bold(true)
	}
	xtra := 0
	if hovered {
		xtra = 2 // trailing " ✕"
	}
	prefix := th.fg(col).Bold(true).Render(label) + " "
	suffix := ""
	if hovered {
		suffix = th.fg("#e0564e").Bold(true).Render(" ✕")
	}

	// label + space + name + space + time (+ ✕). The time keeps its mez/charm
	// tint; the name takes whatever's left.
	nameW := w - 2 - 1 - tw - xtra
	if nameW < 1 {
		return prefix + rightCell(timeStr, max(w-2, 1), col) + suffix
	}
	name := nameStyle.Width(nameW).Render(truncate(displayName(tm.Target), nameW))
	return prefix + name + " " + rightCell(timeStr, tw, col) + suffix
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
	// size the time column to the panel's longest remaining time (covers h:mm:ss)
	tw := timeColW(now, 4, cc, rest)
	var lines []string
	targets := map[int]string{}
	if len(cc) > 0 {
		bySoonest(cc)
		lines = append(lines, th.fg(th.accent).Bold(true).Render("CROWD CONTROL"))
		for _, tm := range cc {
			targets[len(lines)] = tm.Target
			lines = append(lines, ccLine(th, tm, now, w, tm.Target == hover, tw))
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
			lines = append(lines, "  "+timerLine(th, tm, now, w-2, tw))
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
	tw := timeColW(now, 4, cc)
	var lines []string
	targets := map[int]string{}
	for _, tm := range cc {
		targets[len(lines)] = tm.Target
		lines = append(lines, ccLine(th, tm, now, w, tm.Target == hover, tw))
	}
	return strings.Join(lines, "\n"), targets
}

// sectionHead renders a compact uppercase gilded section label (the in-app
// "header" treatment), clipped to width.
func sectionHead(th theme, label string, w int) string {
	return th.fg(th.accent).Bold(true).Render(truncate(strings.ToUpper(label), w))
}

func pct(n, total int) int {
	if total <= 0 {
		return 0
	}
	return n * 100 / total
}

// damageSpecials breaks activated skills (backstab/bash/kick) out per dealer and
// then per kind: each kind's damage, its share of that dealer's total, the hit
// count, and the hit rate. A labelled header explains the columns. "" when
// nobody used a special.
func damageSpecials(th theme, cur *session.CombatSession, stats []combat.DamageStat, w int) string {
	var b strings.Builder
	for _, v := range stats {
		sp := cur.SpecialsFor(v.Dealer)
		if len(sp) == 0 {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(sectionHead(th, "Specials · backstab/bash/kick", w) + "\n")
			// Dmg = the kind's damage; Share = % of the dealer's total; Hits = landed;
			// Hit% = landed / (landed + missed).
			b.WriteString(th.fg(th.dim).Render(fmt.Sprintf("  %-10s %7s %5s %5s %5s",
				"Skill", "Dmg", "Share", "Hits", "Hit%")) + "\n")
		}
		nameStyle := th.fg(th.text).Bold(true)
		if strings.EqualFold(v.Dealer, "you") {
			nameStyle = nameStyle.Foreground(lipgloss.Color(th.accent))
		}
		b.WriteString(nameStyle.Render(truncate(displayName(v.Dealer), w)) + "\n")
		for _, kind := range specialKindsByDamage(sp) {
			s := sp[kind]
			hr := "-"
			if r := s.HitRate(); r >= 0 {
				hr = fmt.Sprintf("%d%%", r)
			}
			b.WriteString(th.fg(th.text).Render(fmt.Sprintf("  %-10s %7s %4d%% %5d %5s",
				kind, humanize(s.Total), pct(s.Total, v.Total), s.Hits, hr)) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// specialKindsByDamage orders a dealer's special kinds by damage, descending
// (name as a stable tiebreak).
func specialKindsByDamage(sp map[string]combat.SpecialStat) []string {
	kinds := make([]string, 0, len(sp))
	for k := range sp {
		kinds = append(kinds, k)
	}
	sort.SliceStable(kinds, func(i, j int) bool {
		if sp[kinds[i]].Total != sp[kinds[j]].Total {
			return sp[kinds[i]].Total > sp[kinds[j]].Total
		}
		return kinds[i] < kinds[j]
	})
	return kinds
}

// damageAvoidance is the per-combatant defensive table: a fully-labelled form
// when the panel is wide, a compact name + avoid% fallback otherwise. The
// player's row is bolded. "" when nobody took a swing.
func damageAvoidance(th theme, cur *session.CombatSession, w int) string {
	defs := cur.Defense()
	if len(defs) == 0 {
		return ""
	}
	const maxRows = 6
	full := w >= 56

	var b strings.Builder
	b.WriteString(sectionHead(th, "Avoidance", w) + "\n")
	if full {
		b.WriteString(th.fg(th.dim).Render(fmt.Sprintf("  %-12s %5s %5s %5s %5s %5s %5s",
			"Defender", "Faced", "Avoid", "Miss", "Dodge", "Parry", "Block")) + "\n")
	}
	rows := 0
	for _, d := range defs {
		if rows >= maxRows {
			break
		}
		s := d.Stats
		faced := s.Swings()
		if faced == 0 {
			continue
		}
		var line string
		if full {
			line = fmt.Sprintf("  %-12s %5d %4d%% %4d%% %4d%% %4d%% %4d%%",
				truncate(displayName(d.Name), 12), faced,
				pct(s.Avoided(), faced), pct(s.Misses, faced), pct(s.Dodges, faced),
				pct(s.Parries, faced), pct(s.Blocks, faced))
		} else {
			line = fmt.Sprintf("  %-12s %3d%% avoid · %d faced",
				truncate(displayName(d.Name), 12), pct(s.Avoided(), faced), faced)
		}
		st := th.fg(th.text)
		if strings.EqualFold(d.Name, "you") {
			st = st.Bold(true)
		}
		b.WriteString(st.Render(line) + "\n")
		rows++
	}
	return strings.TrimRight(b.String(), "\n")
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
	// size the time column to the widest entry ("UP" or m:ss / h:mm:ss) so long
	// repop timers aren't clipped; the mob name yields, the time always shows.
	timeW := 2 // "UP"
	for _, r := range rs {
		if r.Remaining > 0 {
			if v := lipgloss.Width(mmss(r.Remaining)); v > timeW {
				timeW = v
			}
		}
	}
	mobW := max(w-timeW-1, 1)
	var lines []string
	for _, r := range rs {
		when := mmss(r.Remaining)
		if r.Remaining <= 0 {
			when = "UP"
		}
		// names as bright as the spell-timer buff names; the player's own kills
		// bolded for a subtle ownership cue.
		nameStyle := th.fg(th.text)
		if r.Mine {
			nameStyle = nameStyle.Bold(true)
		}
		lines = append(lines, nameStyle.Width(mobW).Render(truncate(r.Mob, mobW))+" "+
			rightCell(when, timeW, mobUrgencyColor(th, r.Remaining)))
	}
	return strings.Join(lines, "\n")
}
