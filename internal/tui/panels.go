package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"99dps/internal/combat"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// card wraps body in a themed rounded panel of total size w×h, with a gold
// title. Lipgloss handles the border/padding/fill; content is clipped to fit.
// An empty title omits the title line entirely (the body gets the extra row) —
// used when the body supplies its own section headers.
func card(th theme, w, h int, title, body string) string {
	cw, ch := w-2, h-2 // border adds 2 in each axis
	if cw < 6 {
		cw = 6
	}
	if ch < 1 {
		ch = 1
	}
	content := body
	if title != "" {
		titleLine := th.fg(th.accent).Bold(true).Render(truncate(title, cw-2))
		content = lipgloss.JoinVertical(lipgloss.Left, titleLine, body)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(th.accent)).
		Background(lipgloss.Color(th.panel)).
		Width(cw).Height(ch).Padding(0, 1).MaxHeight(h).MaxWidth(w).
		Render(content)
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

// splitCCtimers partitions timers into crowd control (mez/charm/pacify) and rest.
func splitCCtimers(timers []gamestate.Timer) (cc, rest []gamestate.Timer) {
	for _, tm := range timers {
		if tm.Mez || tm.Charm || tm.Pacify {
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
	// Your own buffs ("You") pin to the top no matter what fades first; the rest go
	// soonest-to-expire first, breaking ties by name so the order is stable across
	// repaints (map iteration isn't) — otherwise a hovered row could resolve to a
	// different target on the next frame and a click would dismiss the wrong one.
	sort.SliceStable(order, func(i, j int) bool {
		if iYou, jYou := order[i] == "You", order[j] == "You"; iYou != jYou {
			return iYou
		}
		si, sj := soonest(groups[order[i]]), soonest(groups[order[j]])
		if si != sj {
			return si < sj
		}
		return order[i] < order[j]
	})
	return groups, order
}

// urgencyColor tints a countdown by how close it is to fading, using the shared
// gamestate.TimerUrgency classifier (fractional, but absolute-capped so a long
// buff isn't orange with an hour left). Red here is exactly the tracker's
// refresh-vs-new-mob "stale".
func urgencyColor(th theme, remaining, total int64) string {
	switch gamestate.TimerUrgency(remaining, total) {
	case gamestate.Expiring:
		return "#e0564e" // red — about to fade
	case gamestate.Low:
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
	col := urgencyColor(th, rem, total)

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
	label, col := "M", "#e0564e" // mez
	if tm.Charm {
		label, col = "⊗", "#c98ad6" // charm
	}
	if tm.Pacify {
		label, col = "z", "#7fd4e8" // pacify / lull / calm
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

// timerColumn renders spell timers in sections, including only the ones the
// caller asks for: CROWD CONTROL (mez/charm/pacify, pinned), DEBUFFS
// (detrimental, on mobs), BUFFS (beneficial, on you/allies). The class panel and
// the Enemy column request different subsets (buffs vs cc+debuffs), so a debuff
// on a mob never mixes in with a group-mate's buffs. Each section groups by
// target with a thin rule between groups; returns a line→target map for
// hover/click-to-dismiss (hover highlights that group with an ✕).
func timerColumn(th theme, tr *gamestate.Tracker, w int, hover string, wantCC, wantDebuffs, wantBuffs bool) (string, map[int]string) {
	if tr == nil {
		return th.fg(th.dim).Render("spell timers off\n(no spells_us.txt)"), nil
	}
	now := time.Now().Unix()
	cc, rest := splitCCtimers(tr.Active(now))
	var buffs, debuffs []gamestate.Timer
	for _, tm := range rest {
		if tm.Detrimental {
			debuffs = append(debuffs, tm)
		} else {
			buffs = append(buffs, tm)
		}
	}
	// size the time column to the panel's longest remaining time (covers h:mm:ss)
	tw := timeColW(now, 4, cc, rest)

	var lines []string
	targets := map[int]string{}
	// section appends a labelled, target-grouped block (a thin rule between
	// groups), preceded by a blank line when it isn't the first thing shown.
	section := func(label string, ts []gamestate.Timer) {
		if len(ts) == 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		if label != "" { // a "" label means the panel title already says it (Buffs)
			lines = append(lines, th.fg(th.accent).Bold(true).Render(label))
		}
		groups, order := groupByTargetTimers(ts)
		for gi, tgt := range order {
			if gi > 0 {
				lines = append(lines, th.fg(th.accentLo).Render(strings.Repeat("─", max(w, 0))))
			}
			targets[len(lines)] = tgt
			lines = append(lines, targetHeader(th, tgt, w, tgt == hover))
			g := groups[tgt]
			bySoonest(g)
			for _, tm := range g {
				targets[len(lines)] = tgt
				lines = append(lines, "  "+timerLine(th, tm, now, w-2, tw))
			}
		}
	}

	if wantCC && len(cc) > 0 { // crowd control pinned at the top (its own ccLine layout)
		bySoonest(cc)
		lines = append(lines, th.fg(th.accent).Bold(true).Render("CROWD CONTROL"))
		for _, tm := range cc {
			targets[len(lines)] = tm.Target
			lines = append(lines, ccLine(th, tm, now, w, tm.Target == hover, tw))
		}
	}
	if wantDebuffs {
		section("DEBUFFS", debuffs)
	}
	if wantBuffs {
		// when buffs are the whole panel (the dedicated "Buffs" column), the card
		// title already labels it — drop the redundant inner header.
		label := "BUFFS"
		if !wantCC && !wantDebuffs {
			label = ""
		}
		section(label, buffs)
	}

	if len(lines) == 0 {
		return th.fg(th.dim).Render("No active spells."), nil
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
		// full table fits a wide column; a narrow side column drops Share+Hits and
		// keeps the essentials (kind · dmg · hit-rate). Lines are clipped to w too.
		full := w >= 34
		if b.Len() == 0 {
			b.WriteString(sectionHead(th, "Specials · backstab/bash/kick", w) + "\n")
			var hdr string
			if full { // Dmg = kind dmg; Share = % of dealer total; Hits = landed; Hit% = landed/(hit+miss)
				hdr = fmt.Sprintf("  %-8s %6s %5s %4s %5s", "Skill", "Dmg", "Share", "Hits", "Hit%")
			} else {
				hdr = fmt.Sprintf("  %-8s %6s %5s", "Skill", "Dmg", "Hit%")
			}
			b.WriteString(th.fg(th.dim).Render(truncate(hdr, w)) + "\n")
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
			var line string
			if full {
				line = fmt.Sprintf("  %-8s %6s %4d%% %4d %5s", kind, humanize(s.Total), pct(s.Total, v.Total), s.Hits, hr)
			} else {
				line = fmt.Sprintf("  %-8s %6s %5s", kind, humanize(s.Total), hr)
			}
			b.WriteString(th.fg(th.text).Render(truncate(line, w)) + "\n")
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

// damageAvoidance lists each combatant's defensive breakdown. The Specials /
// Avoidance column is too narrow for a 7-column table, so it's laid out
// vertically per defender — a "name · N faced" header, then every outcome with
// its full name (Avoided total, Miss, Dodge, Parry, Block, Riposte). The
// player's header is bolded. "" when nobody took a swing.
func damageAvoidance(th theme, cur *session.CombatSession, w int) string {
	defs := cur.Defense()
	if len(defs) == 0 {
		return ""
	}
	const maxDefenders = 4

	var b strings.Builder
	b.WriteString(sectionHead(th, "Avoidance", w) + "\n")
	shown := 0
	for _, d := range defs {
		if shown >= maxDefenders {
			break
		}
		s := d.Stats
		faced := s.Swings()
		if faced == 0 {
			continue
		}
		// match the Specials treatment: a bright/bold defender name (accent for You),
		// the stat lines in normal text rather than faded dim.
		head := th.fg(th.text).Bold(true)
		if strings.EqualFold(d.Name, "you") {
			head = th.fg(th.accent).Bold(true)
		}
		b.WriteString(head.Render(truncate(fmt.Sprintf("%s · %d faced", displayName(d.Name), faced), w)) + "\n")
		for _, it := range []struct {
			name string
			n    int
		}{
			{"Avoided", s.Avoided()}, {"Miss", s.Misses}, {"Dodge", s.Dodges},
			{"Parry", s.Parries}, {"Block", s.Blocks}, {"Riposte", s.Ripostes},
		} {
			b.WriteString(th.fg(th.text).Render(truncate(fmt.Sprintf("  %-8s %3d%%", it.name, pct(it.n, faced)), w)) + "\n")
		}
		shown++
	}
	return strings.TrimRight(b.String(), "\n")
}

// mobTracker is the zone-aware repop list: the player's kills first, then a
// "killed by others" separator and others' kills (with the killer's name). A
// clicked mob (editMob) is marked. Returns a content-line→mob map for click
// resolution (separator lines have no entry).
func mobTracker(th theme, tr *gamestate.Tracker, w int, editMob string) (string, map[int]string) {
	if tr == nil {
		return th.fg(th.dim).Render("—"), nil
	}
	rs := tr.Respawns(time.Now().Unix())
	if len(rs) == 0 {
		return th.fg(th.dim).Render("No kills tracked yet."), nil
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
	nameW := max(w-timeW-1, 1)

	var lines []string
	targets := map[int]string{}
	for i, r := range rs {
		// separate the group's kills (yours + xp-credited) from everyone else's.
		if i > 0 && rs[i-1].Group && !r.Group {
			sep := "── killed by others "
			sep += strings.Repeat("─", max(w-lipgloss.Width(sep), 0))
			lines = append(lines, th.fg(th.dim).Render(truncate(sep, w)))
		}
		when := mmss(r.Remaining)
		if r.Remaining <= 0 {
			when = "UP"
		}
		marker := "  "
		if editMob != "" && r.Mob == editMob {
			marker = "▸ "
		}
		// your own kills bolded; a group-mate's (still a group kill) and others'
		// name the killer; non-group kills are dimmed.
		label := marker + r.Mob
		nameStyle := th.fg(th.text)
		switch {
		case r.Mine:
			nameStyle = nameStyle.Bold(true)
		case !r.Group:
			nameStyle = th.fg(th.dim)
		}
		if !r.Mine && r.Killer != "" {
			label += " «" + r.Killer
		}
		targets[len(lines)] = r.Mob
		lines = append(lines, nameStyle.Width(nameW).Render(truncate(label, nameW))+" "+
			rightCell(when, timeW, mobUrgencyColor(th, r.Remaining)))
	}
	return strings.Join(lines, "\n"), targets
}
