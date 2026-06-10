package cli

import (
	"99dps/internal/common"
	"99dps/internal/gamestate"
	"99dps/internal/session"
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file holds the pure rendering layer: functions that turn a session
// snapshot into the text drawn in each panel. They take no gui state (only the
// data and a target width), so they're unit-testable without a terminal.

// barColors cycles fg colors for the bars, ranked highest-damage first.
// gocui's OutputNormal only honours the 8 base colors (30-37), so we stay in
// that range rather than the bright 90-97 codes (which it ignores).
var barColors = []int{36, 32, 33, 35, 34, 31, 37}

// avoidance column widths for the full (wide) layout.
const (
	avNameW = 14
	avNumW  = 5 // each numeric column, e.g. "Faced", "Dodge" (fits 5-char headers + "100%")
)

// fullAvoidanceWidth is the column count the labelled layout needs (7 numeric
// columns: Faced/Avoid/Miss/Dodge/Parry/Block/Ripo). Kept tight so the full
// table still shows in the half-width Damage panel.
const fullAvoidanceWidth = avNameW + 7*(avNumW+1)

// renderDamage builds the damage breakdown table. Dealers are ranked by total
// and colored with the same palette (by rank) as the graph bars, so a dealer's
// table row and its bar share a color. The player's row is bolded.
func renderDamage(cur *session.CombatSession, live bool, width int) string {
	if cur == nil {
		return "No fight selected.\n\nFight something!"
	}

	stats := cur.GetAggressors()
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })

	groupTotal := cur.Total()
	magic := cur.MagicTotal()
	encounterTotal := groupTotal + magic // melee + unattributed spell damage

	// session-span seconds, shared by every dealer's DPS so the rows are
	// comparable contributions to the same fight
	span := cur.LastUnix() - cur.StartTime().Unix()
	if span < 1 {
		span = 1
	}

	status, titleSGR := "● live", dpsTitleLiveSGR
	if !live {
		status, titleSGR = "○ ended "+cur.EndTime().Format("15:04:05"), dpsTitleDoneSGR
	}

	// the optional accuracy/crit columns only appear when the panel is wide
	// enough to hold them without clipping the core stats off the right edge.
	showHit := width >= 45
	showCrit := width >= 51

	var b strings.Builder
	// encounter title bar (green = live, blue = ended)
	b.WriteString(headerBar(fmt.Sprintf("⚔ %s   %s", cur.Name(), status), titleSGR, width))
	fmt.Fprintf(&b, "%s · group %s · %s dps\n",
		fmtDuration(cur.Duration()),
		humanizeInt(encounterTotal),
		humanizeInt(int(int64(encounterTotal)/span)),
	)
	b.WriteString("\n")

	hdr := fmt.Sprintf("%-2s %-14s %6s %8s %5s", "#", "Dealer", "DPS", "Total", "%")
	if showHit {
		hdr += fmt.Sprintf(" %5s", "Hit%")
	}
	if showCrit {
		hdr += fmt.Sprintf(" %5s", "Crit%")
	}
	b.WriteString(sectionHeader(hdr, width))

	for i, v := range stats {
		name := v.Dealer

		pct := 0
		if encounterTotal > 0 {
			pct = v.Total * 100 / encounterTotal
		}

		row := fmt.Sprintf("%-2d %-14s %6s %8s %4d%%",
			i+1,
			truncate(name, 14),
			humanizeInt(v.Total/int(span)),
			humanizeInt(v.Total),
			pct,
		)
		if showHit {
			// accuracy = landed hits / (hits + misses); "-" when this dealer
			// never produced a parseable swing (e.g. spell-only activity)
			hit := "-"
			if hr := cur.OffenseFor(name).HitRate(); hr >= 0 {
				hit = fmt.Sprintf("%d%%", hr)
			}
			row += fmt.Sprintf(" %5s", hit)
		}
		if showCrit {
			crit := "-"
			if c := cur.CritFor(name); c.Count > 0 {
				// distant group-members' crit messages arrive even when their
				// swing damage is out of range, so crits can exceed locally
				// visible hits — cap the ratio at 100%.
				cp := c.Count * 100 / v.Hits
				if cp > 100 {
					cp = 100
				}
				crit = fmt.Sprintf("%d%%", cp)
			}
			row += fmt.Sprintf(" %5s", crit)
		}

		color := barColors[i%len(barColors)]
		style := fmt.Sprintf("\x1b[%dm", color)
		if strings.EqualFold(name, "you") {
			style = fmt.Sprintf("\x1b[1;%dm", color) // bold the player's row
		}
		b.WriteString(style + row + "\x1b[0m\n")
	}

	// unattributed spell/proc/DoT damage — EQ logs name no caster, so it can't
	// join a dealer row; shown as its own line (n/a) and folded into the group
	// total. Per-spell tracking lives in the spell-timer panel.
	if magic > 0 {
		pct := 0
		if encounterTotal > 0 {
			pct = magic * 100 / encounterTotal
		}
		fmt.Fprintf(&b, "%-2s %-14s %6s %8s %4d%%\n",
			"", "spells (n/a)",
			humanizeInt(magic/int(span)),
			humanizeInt(magic),
			pct)
	}

	b.WriteString(renderSpecials(stats, width))
	b.WriteString(renderAvoidance(cur, width))

	return b.String()
}

// DPS-panel header bar styles (SGR params: bg;fg;attrs). gocui OutputNormal
// supports the 8 base colors plus bold(1)/underline(4)/reverse(7).
const (
	dpsTitleLiveSGR = "43;1;30" // gold bg, bold black — a live encounter (EQ "gold plaque")
	dpsTitleDoneSGR = "44;1;37" // blue bg, bold white — an ended encounter (cool/past)
	dpsHeaderSGR    = "1;33"    // bold gold text      — column/section headers (gilded lettering)
)

// headerBar renders label as a full-width bar (sgr = SGR params), padded to
// width so the tint fills the whole row.
func headerBar(label, sgr string, width int) string {
	return fmt.Sprintf("\x1b[%sm%s\x1b[0m\n", sgr, padTo(label, width))
}

// sectionHeader renders a panel section label as a compact UPPERCASE gilded
// header — the in-app "header font" treatment (terminals pick the typeface; this
// is the styling we control). Centralized so the look is tweakable in one place.
func sectionHeader(label string, width int) string {
	return headerBar(strings.ToUpper(label), dpsHeaderSGR, width)
}

// renderStatus is the top-left "Now" box: character, class/level, the current
// zone (tinted bar), and the zone-wide xp-kill rate — the at-a-glance summary.
func renderStatus(char string, class common.Class, level int, zone string, kills, perHour, deaths, width int) string {
	var b strings.Builder
	b.WriteString("\x1b[1m" + truncate(char, width) + "\x1b[0m\n")

	cl := ""
	if level > 0 {
		cl = fmt.Sprintf("L%d ", level)
	}
	if class != common.ClassUnknown {
		cl += string(class)
	}
	if cl != "" {
		b.WriteString(truncate(cl, width) + "\n")
	}

	z := zone
	if z == "" {
		z = "—"
	}
	b.WriteString(headerBar(z, "43;1;30", width)) // gold zone plaque (EQ-flavored)

	if kills > 0 || deaths > 0 {
		b.WriteString("\x1b[1m" + truncate(fmt.Sprintf("%d kills · %d/hr", kills, perHour), width) + "\x1b[0m\n")
		if deaths > 0 {
			fmt.Fprintf(&b, "%d deaths\n", deaths)
		}
	}
	return b.String()
}

// renderSpecials lists dealers who landed activated skills (backstab/bash/kick),
// showing that damage and its share of the dealer's total. Empty when nobody
// used a special.
func renderSpecials(stats []common.DamageStat, width int) string {
	var b strings.Builder
	for _, v := range stats {
		if v.SpecialHits == 0 {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\n" + sectionHeader("Specials · backstab/bash/kick", width))
		}
		pct := 0
		if v.Total > 0 {
			pct = v.SpecialTotal * 100 / v.Total
		}
		fmt.Fprintf(&b, "%-14s %8s %4d%%  %d hits\n",
			truncate(displayName(v.Dealer), 14),
			humanizeInt(v.SpecialTotal),
			pct,
			v.SpecialHits,
		)
	}
	return b.String()
}

// renderAvoidance appends a per-combatant defensive table. When the Damage
// panel is wide enough it uses a fully-labelled table (Faced / Avoid / Miss /
// Dodge / Parry / Block); on narrower panels it falls back to a compact form,
// and on very narrow ones to just name + avoid% — so the key numbers are never
// clipped off the right edge. The player's row is bolded.
func renderAvoidance(cur *session.CombatSession, width int) string {
	defenders := cur.Defense()
	if len(defenders) == 0 {
		return ""
	}

	const maxRows = 6
	full := width >= fullAvoidanceWidth

	var b strings.Builder
	b.WriteString("\n" + sectionHeader("Avoidance", width))
	if full {
		fmt.Fprintf(&b, "%-*s %*s %*s %*s %*s %*s %*s %*s\n",
			avNameW, "Defender",
			avNumW, "Faced", avNumW, "Avoid",
			avNumW, "Miss", avNumW, "Dodge", avNumW, "Parry", avNumW, "Block", avNumW, "Ripo")
	}

	for i, d := range defenders {
		if i >= maxRows {
			break
		}
		s := d.Stats
		faced := s.Swings()
		if faced == 0 {
			continue
		}

		row := compactAvoidanceRow(d.Name, s, faced, width)
		if full {
			row = fmt.Sprintf("%-*s %*d %*s %*s %*s %*s %*s %*s",
				avNameW, truncate(displayName(d.Name), avNameW),
				avNumW, faced,
				avNumW, pctStr(s.Avoided(), faced),
				avNumW, pctStr(s.Misses, faced),
				avNumW, pctStr(s.Dodges, faced),
				avNumW, pctStr(s.Parries, faced),
				avNumW, pctStr(s.Blocks, faced),
				avNumW, pctStr(s.Ripostes, faced))
		}

		if strings.EqualFold(d.Name, "you") {
			b.WriteString("\x1b[1m" + row + "\x1b[0m\n")
		} else {
			b.WriteString(row + "\n")
		}
	}

	return b.String()
}

// compactAvoidanceRow is the narrow fallback: name + avoid% (faced), with the
// miss/dodge/parry/block split appended only if it still fits the width.
func compactAvoidanceRow(name string, s common.SwingStats, faced, width int) string {
	base := fmt.Sprintf("%-12s %3d%% %5d",
		truncate(displayName(name), 12),
		s.Avoided()*100/faced,
		faced,
	)
	breakdown := fmt.Sprintf("  m%2d d%2d p%2d b%2d r%2d",
		pctOf(s.Misses, faced),
		pctOf(s.Dodges, faced),
		pctOf(s.Parries, faced),
		pctOf(s.Blocks, faced),
		pctOf(s.Ripostes, faced),
	)
	if width >= len([]rune(base))+len([]rune(breakdown)) { // rune-count: names may be multibyte
		return base + breakdown
	}
	return base
}

// renderSessions builds the side-panel session list: one card per fight, the
// selected one drawn as a reverse-video bar.
func renderSessions(dat []*session.CombatSession, selected, width int) string {
	if len(dat) == 0 {
		return "No sessions yet.\n\nFight something!"
	}

	// Sessions are append-only, so the live (active) session is the last entry.
	active := len(dat) - 1

	var b strings.Builder
	for i, d := range dat {
		dealer, pct := d.TopDealer()

		name := d.Name()
		if i == active {
			name += "  ● LIVE"
		}

		meta := fmt.Sprintf("%s · %s · %s",
			d.StartTime().Format("15:04:05"),
			fmtDuration(d.Duration()),
			humanizeInt(d.Total()),
		)

		top := ""
		if dealer != "" {
			top = fmt.Sprintf("top: %s %d%%", dealer, pct)
		}

		marker := "  "
		if i == selected {
			marker = "▸ "
		}

		// a thin gilded rule closes each card — separates the list cleanly without
		// a new row (keeps linesPerCard at 4 so click-to-select math is unchanged).
		rule := "\x1b[33m" + strings.Repeat("─", max(width, 0)) + "\x1b[0m"

		// Each card is exactly linesPerCard rows so clicks map back cleanly.
		card := []string{marker + name, "  " + meta, "  " + top, rule}
		for j, line := range card {
			if i == selected && j < len(card)-1 {
				// selected card → a gold "plaque" (black on gold), matching the
				// encounter-title and zone plaques elsewhere
				b.WriteString("\x1b[43;30m" + padTo(line, width) + "\x1b[0m\n")
			} else {
				b.WriteString(line + "\n")
			}
		}
	}

	return b.String()
}

// splitCC partitions timers into crowd control (mez/charm) and everything else.
func splitCC(timers []gamestate.Timer) (cc, rest []gamestate.Timer) {
	for _, tm := range timers {
		if tm.Mez || tm.Charm {
			cc = append(cc, tm)
		} else {
			rest = append(rest, tm)
		}
	}
	return cc, rest
}

// ccRow renders one crowd-control timer: mez ("M", debuff-urgency tint that
// escalates red as it nears a break) or charm ("⊗", magenta), target + remaining.
func ccRow(tm gamestate.Timer, now int64, width int) string {
	inner := width - 2
	nameW := inner - 7
	if nameW < 8 {
		nameW = 8
	}
	rem := tm.Expiry - now
	if rem < 0 {
		rem = 0
	}
	label, sgr := "M", timerStyle(true, rem, now)
	if tm.Charm {
		label, sgr = "⊗", charmStyle(rem, now)
	}
	content := fmt.Sprintf("%-*s %s%5s", nameW, truncate(displayName(tm.Target), nameW),
		label, fmtDuration(time.Duration(rem)*time.Second))
	return "  " + fmt.Sprintf("\x1b[%sm%s\x1b[0m", sgr, padTo(content, inner))
}

// renderCC renders the crowd-control list (mez + charm) for the enchanter's
// dedicated column, soonest-to-break first. No header (the panel title supplies
// it). Returns the line→target map (line 0 = first row) for click-to-dismiss.
func renderCC(cc []gamestate.Timer, now int64, width int) (string, map[int]string) {
	if len(cc) == 0 {
		return "", nil
	}
	sort.SliceStable(cc, func(i, j int) bool { return cc[i].Expiry < cc[j].Expiry })
	var b strings.Builder
	lt := map[int]string{}
	for i, tm := range cc {
		b.WriteString(ccRow(tm, now, width) + "\n")
		lt[i] = tm.Target
	}
	return b.String(), lt
}

// renderTimers lists the player's active spell timers, soonest-to-expire first:
// detrimental spells in blue, buffs in green, escalating to red near expiry. The
// second return maps each output line to its target so a click can dismiss that
// person's buffs. When ccInline is true, mez/charm are pinned in a CROWD CONTROL
// section at the top; when false (enchanter, CC has its own column) they're
// omitted here.
func renderTimers(timers []gamestate.Timer, now int64, width int, ccInline bool) (string, map[int]string) {
	if len(timers) == 0 {
		return "No active spells.", nil
	}

	inner := width - 2
	// spell name gets the room left after the 2-char indent and count-down (5) —
	// long names ("Speed of the Shissar") fit; the column flexes with the panel.
	nameW := inner - 6
	if nameW < 8 {
		nameW = 8
	}

	cc, rest := splitCC(timers)
	if !ccInline {
		cc = nil // CC lives in its own column; keep only buffs/debuffs here
	}

	var b strings.Builder
	lineTargets := map[int]string{}
	line := 0

	if len(cc) > 0 {
		ccStr, ccMap := renderCC(cc, now, width)
		b.WriteString("\x1b[1mCROWD CONTROL\x1b[0m\n")
		line++ // header → no dismiss target
		b.WriteString(ccStr)
		for k, v := range ccMap {
			lineTargets[line+k] = v
		}
		line += len(ccMap)
		if len(rest) > 0 {
			b.WriteString("\n") // gap before buffs/debuffs
			line++
		}
	}

	// buffs/debuffs grouped by target (clicking any row dismisses that target)
	groups, order := groupByTarget(rest)
	for _, tgt := range order {
		b.WriteString("\x1b[1m" + truncate(displayName(tgt), width) + "\x1b[0m\n")
		lineTargets[line] = tgt
		line++

		g := groups[tgt]
		sort.SliceStable(g, func(i, j int) bool { return g[i].Expiry < g[j].Expiry })
		for _, tm := range g {
			rem := tm.Expiry - now
			if rem < 0 {
				rem = 0
			}
			sgr := timerStyle(tm.Detrimental, rem, now)
			content := fmt.Sprintf("%-*s %5s", nameW, truncate(tm.Spell, nameW),
				fmtDuration(time.Duration(rem)*time.Second))
			b.WriteString("  " + fmt.Sprintf("\x1b[%sm%s\x1b[0m", sgr, padTo(content, inner)) + "\n")
			lineTargets[line] = tgt
			line++
		}
	}
	if b.Len() == 0 {
		return "No active spells.", nil
	}
	return b.String(), lineTargets
}

// renderSkills is the melee-class panel: the player's activated-skill breakdown
// (Backstab/Bash/Kick) this fight, plus accuracy, crit rate, and avoidance.
// class/level drive a best-guess specific name for the generic verb (see
// displaySkillName). The discipline-cooldown section will sit above this once
// that data lands.
// renderCooldowns is the activated-ability reuse section (Mend, Feign Death, …),
// or "" when nothing is on cooldown. A standalone, class-independent section so
// it can stack above any panel body — not just the melee skills view.
func renderCooldowns(cooldowns []gamestate.CooldownTimer, width int) string {
	if len(cooldowns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(sectionHeader("Cooldowns", width))
	for _, cd := range cooldowns {
		b.WriteString("  " + renderCooldownRow(cd, width) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderSkills(cur *session.CombatSession, class common.Class, level, width int) string {
	if cur == nil {
		return "No fight selected.\n\nFight something!"
	}

	var b strings.Builder

	b.WriteString(sectionHeader("Skills · this fight", width))

	skills := cur.Skills()
	if len(skills) == 0 {
		b.WriteString("  no skill attacks yet\n")
	} else {
		type row struct {
			name string
			s    common.SkillStat
		}
		rows := make([]row, 0, len(skills))
		for n, s := range skills {
			if !skillRelevant(n, class) {
				continue
			}
			rows = append(rows, row{displaySkillName(n, class, level), s})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].s.Total > rows[j].s.Total })
		if len(rows) == 0 {
			b.WriteString("  no skill attacks yet\n")
		}
		for _, r := range rows {
			fmt.Fprintf(&b, "  %-12s %6s  %d hits\n", r.name, humanizeInt(r.s.Total), r.s.Hits)
		}
	}

	b.WriteString("\n" + sectionHeader("Accuracy", width))
	you := playerStat(cur)
	if hr := cur.OffenseFor("You").HitRate(); hr >= 0 {
		fmt.Fprintf(&b, "  %-12s %3d%%\n", "Hit rate", hr)
	}
	if you.Hits > 0 {
		if c := cur.CritFor("You"); c.Count > 0 {
			fmt.Fprintf(&b, "  %-12s %3d%%\n", "Crit rate", critPct(c.Count, you.Hits))
		}
	}
	if av, faced := playerAvoidance(cur); faced > 0 {
		fmt.Fprintf(&b, "  %-12s %3d%%\n", "Avoided", av*100/faced)
	}
	return b.String()
}

// skillsSummary is the one-line skill digest appended to the hybrid panel under
// the spell timers, e.g. "Bash 220 · Crit 8% · Hit 71%". "" when there's nothing.
func skillsSummary(cur *session.CombatSession, class common.Class, level int) string {
	if cur == nil {
		return ""
	}
	var parts []string
	if name, s := topSkill(cur.Skills(), class); name != "" {
		parts = append(parts, fmt.Sprintf("%s %s", displaySkillName(name, class, level), humanizeInt(s.Total)))
	}
	you := playerStat(cur)
	if you.Hits > 0 {
		if c := cur.CritFor("You"); c.Count > 0 {
			parts = append(parts, fmt.Sprintf("Crit %d%%", critPct(c.Count, you.Hits)))
		}
	}
	if hr := cur.OffenseFor("You").HitRate(); hr >= 0 {
		parts = append(parts, fmt.Sprintf("Hit %d%%", hr))
	}
	return strings.Join(parts, " · ")
}

// renderCanni is the gamified "canni dance" meter: ride the Cannibalize recast
// edge as fast as possible without the "recast not yet met" buzzer. "" when not
// dancing.
func renderCanni(c gamestate.CanniStats, width int) string {
	if !c.Active {
		return ""
	}
	grade, sgr := canniGrade(c.Pct)

	var b strings.Builder
	b.WriteString(headerBar(fmt.Sprintf("⟳ CANNI DANCE   %d%%   %s   ×%d", c.Pct, grade, c.Combo), sgr, width))

	// a progress bar of the efficiency, in the grade's colour
	barW := width - 2
	if barW < 4 {
		barW = 4
	}
	filled := c.Pct * barW / 100
	if filled > barW {
		filled = barW
	}
	b.WriteString("  " + fmt.Sprintf("\x1b[%sm%s\x1b[0m%s", sgr,
		strings.Repeat("█", filled), strings.Repeat("░", barW-filled)) + "\n")

	detail := fmt.Sprintf("%s · %.2fs · %s pts · best %d%%",
		c.Rank, float64(c.EdgeMs)/1000, formatInt(c.Score), c.Best)
	if c.Buzzers > 0 {
		detail += fmt.Sprintf(" · %d early", c.Buzzers)
	}
	b.WriteString("  " + truncate(detail, width-2) + "\n")
	return b.String()
}

// canniGrade maps an efficiency % to a letter grade + SGR colour (green→red).
func canniGrade(pct int) (string, string) {
	switch {
	case pct >= 95:
		return "S", "42;1;30" // bright green
	case pct >= 85:
		return "A", "42;30" // green
	case pct >= 70:
		return "B", "43;30" // yellow
	case pct >= 50:
		return "C", "43;30" // yellow
	default:
		return "D", "41;37" // red
	}
}

// renderRespawns lists pending mob repops: the player's own kills first, then a
// separator and the kills others got (with the killer's name). A mob past its
// timer shows "UP" (green); the rest count down (blue). Rows stay aligned with
// updateRepops's line→mob map, which inserts the same one separator line.
func renderRespawns(respawns []gamestate.Respawn, selected string, width int) string {
	if len(respawns) == 0 {
		return ""
	}
	inner := width - 2 // minus the 2-char marker

	var b strings.Builder
	for i, r := range respawns {
		// one rule between "my kills" and "others'" (entries are sorted mine-first)
		if i > 0 && respawns[i-1].Mine && !r.Mine {
			label := []rune("── killed by others ")
			sep := string(label)
			if fill := inner - len(label); fill > 0 {
				sep += strings.Repeat("─", fill)
			}
			b.WriteString("  \x1b[1m" + sep + "\x1b[0m\n")
		}

		timeStr := "UP"
		if r.Remaining > 0 {
			timeStr = fmtDuration(time.Duration(r.Remaining) * time.Second)
		}
		killer := ""
		if !r.Mine && r.Killer != "" {
			killer = " " + truncate(r.Killer, 12)
		}
		nameW := inner - 6 - len(killer) // 6 = space + 5-wide time field
		if nameW < 6 {
			nameW = 6
		}
		content := fmt.Sprintf("%-*s %5s%s", nameW, truncate(r.Mob, nameW), timeStr, killer)

		sgr := "44;37" // blue: counting down
		if r.Remaining <= 0 {
			sgr = "42;30" // green: should be up
		}
		marker := "  "
		if selected != "" && r.Mob == selected {
			marker = "▸ " // a clicked mob (its override is being edited)
		}
		b.WriteString(marker + fmt.Sprintf("\x1b[%sm%s\x1b[0m", sgr, padTo(content, inner)) + "\n")
	}
	return b.String()
}

// renderCooldownRow renders one ability reuse timer as a tinted bar: green when
// the ability is ready, blue with the remaining time while on cooldown.
func renderCooldownRow(cd gamestate.CooldownTimer, width int) string {
	var content, sgr string
	if cd.Remaining <= 0 {
		content = fmt.Sprintf("%-13s ready", truncate(cd.Name, 13))
		sgr = "42;30" // green: ready to use
	} else {
		content = fmt.Sprintf("%-13s %s", truncate(cd.Name, 13),
			fmtDuration(time.Duration(cd.Remaining)*time.Second))
		sgr = "44;37" // blue: on cooldown
	}
	return fmt.Sprintf("\x1b[%sm%s\x1b[0m", sgr, padTo(content, width-2))
}

// displaySkillName turns a generic skill bucket into the best class/level label.
// EQ collapses every kick variant to "kick" and every monk special strike
// (Eagle Strike / Tiger Claw / Dragon Punch) to "strike", so this is a
// display-only best guess: a 30+ monk's kick is almost always Flying Kick;
// "Strike" can't be disambiguated further.
func displaySkillName(generic string, class common.Class, level int) string {
	if class == common.ClassMonk && generic == "Kick" && level >= 30 {
		return "Flying Kick" // learned at level 30; a 30+ monk kicks with it
	}
	return generic
}

// skillRelevant reports whether a skill bucket should appear for the class. A
// player's "strike" is a monk special; for any other class it shouldn't surface
// as a skill (those classes don't produce it from auto-attacks either).
func skillRelevant(generic string, class common.Class) bool {
	if generic == "Strike" {
		return class == common.ClassMonk
	}
	return true
}

// critPct is crit count as a percentage of melee hits, capped at 100 (distant
// group crits can arrive without their matching swing — see renderDamage).
func critPct(crits, hits int) int {
	if hits <= 0 {
		return 0
	}
	if p := crits * 100 / hits; p <= 100 {
		return p
	}
	return 100
}

// topSkill returns the highest-damage class-relevant skill, or ("", zero).
func topSkill(skills map[string]common.SkillStat, class common.Class) (string, common.SkillStat) {
	var name string
	var best common.SkillStat
	for n, s := range skills {
		if !skillRelevant(n, class) {
			continue
		}
		if s.Total > best.Total {
			name, best = n, s
		}
	}
	return name, best
}

// playerStat returns the player's own DamageStat from a session snapshot.
func playerStat(cur *session.CombatSession) common.DamageStat {
	for _, v := range cur.GetAggressors() {
		if strings.EqualFold(v.Dealer, "you") {
			return v
		}
	}
	return common.DamageStat{}
}

// playerAvoidance returns the player's avoided count and swings faced as the
// defender, or (0, 0) if they took no swings.
func playerAvoidance(cur *session.CombatSession) (avoided, faced int) {
	for _, d := range cur.Defense() {
		if strings.EqualFold(d.Name, "you") {
			return d.Stats.Avoided(), d.Stats.Swings()
		}
	}
	return 0, 0
}

// groupByTarget buckets timers by their target and returns the targets ordered
// with charm always first, then the rest by their soonest-expiring timer
// (ascending) — so charm pets sit pinned at the top and, below them, whoever's
// buff is about to drop floats up, which is what you want when raid-buffing.
func groupByTarget(timers []gamestate.Timer) (map[string][]gamestate.Timer, []string) {
	groups := make(map[string][]gamestate.Timer)
	for _, tm := range timers {
		groups[tm.Target] = append(groups[tm.Target], tm)
	}

	soonest := func(g []gamestate.Timer) int64 {
		m := g[0].Expiry
		for _, x := range g[1:] {
			if x.Expiry < m {
				m = x.Expiry
			}
		}
		return m
	}
	isCharm := func(g []gamestate.Timer) bool {
		for _, x := range g {
			if x.Charm {
				return true
			}
		}
		return false
	}

	order := make([]string, 0, len(groups))
	for tgt := range groups {
		order = append(order, tgt)
	}
	sort.SliceStable(order, func(i, j int) bool {
		ci, cj := isCharm(groups[order[i]]), isCharm(groups[order[j]])
		if ci != cj {
			return ci // charm groups sort ahead of everything else
		}
		return soonest(groups[order[i]]) < soonest(groups[order[j]])
	})
	return groups, order
}

// charmSGR is the resting bar style for a charm timer (magenta bg, white),
// used while it's well short of its duration cap.
const charmSGR = "45;37"

// charmStyle tints a charm row: magenta while comfortably below the cap, then
// the same yellow→red urgency escalation as any other timer as the formula
// maximum (a hard ceiling the charm breaks before) approaches.
func charmStyle(rem, now int64) string {
	if rem > 30 {
		return charmSGR
	}
	return timerStyle(true, rem, now)
}

// timerStyle returns the ANSI SGR parameters (background;foreground) for a timer
// row. The whole row is tinted: green for a healthy buff, blue for a debuff,
// escalating to yellow then red as expiry nears, and a red/white flash that
// alternates each second in the final seconds (the panel repaints once a sec).
func timerStyle(detrimental bool, rem, now int64) string {
	switch {
	case rem <= 5:
		if now%2 == 0 {
			return "41;37" // red bg, white fg
		}
		return "47;31" // inverted: white bg, red fg (the flash)
	case rem <= 10:
		return "41;37" // red bg
	case rem <= 30:
		return "43;30" // yellow bg, black fg
	}
	if detrimental {
		return "44;37" // blue bg: active debuff
	}
	return "42;30" // green bg: healthy buff
}

// renderBars draws one horizontal bar per dealer, Recount-style: a colored
// fill proportional to that dealer's share of the top dealer's damage, with the
// name on the left and total/dps on the right.
func renderBars(agg []common.DamageStat, width, height int) string {
	if len(agg) == 0 || width < 12 || height < 1 {
		return "Fight something!"
	}

	maxTotal := agg[0].Total
	if maxTotal <= 0 {
		return "Fight something!"
	}

	// cap rows to what fits in the view
	if len(agg) > height {
		agg = agg[:height]
	}

	const nameW = 14
	var b strings.Builder

	for i, d := range agg {
		name := truncate(d.Dealer, nameW)

		value := fmt.Sprintf(" %s  %d/s", formatInt(d.Total), dealerDPS(d))

		// nameW + 1 (the space after the name) + barW + value must equal width;
		// the missing -1 for that space was clipping the last char ("/s" → "/").
		barW := width - nameW - 1 - len(value)
		if barW < 1 {
			barW = 1
		}

		filled := int(float64(barW) * float64(d.Total) / float64(maxTotal))
		if filled < 1 {
			filled = 1
		}
		if filled > barW {
			filled = barW
		}

		color := barColors[i%len(barColors)]
		bar := fmt.Sprintf("\x1b[%dm%s\x1b[0m%s",
			color,
			strings.Repeat("█", filled),
			strings.Repeat("░", barW-filled),
		)

		fmt.Fprintf(&b, "%-*s %s%s\n", nameW, name, bar, value)
	}

	return b.String()
}

// dealerDPS derives damage-per-second from a dealer's own first-to-last-hit
// span. A zero span (or a single hit) falls back to raw total.
func dealerDPS(d common.DamageStat) int {
	if d.Hits == 0 {
		return 0
	}
	span := d.LastTime - d.FirstTime
	if span <= 0 {
		return d.Total
	}
	return d.Total / int(span)
}

// --- small pure formatters -------------------------------------------------

// pctStr formats n as a whole percentage of total, e.g. "47%".
func pctStr(n, total int) string {
	return fmt.Sprintf("%d%%", pctOf(n, total))
}

// pctOf returns n as a whole percentage of total (total assumed > 0).
func pctOf(n, total int) int {
	return n * 100 / total
}

// displayName renders the player's normalized "YOU" token as "You" for the UI.
func displayName(name string) string {
	if name == "YOU" {
		return "You"
	}
	return name
}

// fmtDuration renders a fight length compactly as m:ss.
func fmtDuration(d time.Duration) string {
	total := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// humanizeInt abbreviates large counts for the narrow session panel:
// 1_234 -> "1.2k", 1_500_000 -> "1.5m".
func humanizeInt(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatInt renders an int with thousands separators (e.g. 12345 -> "12,345").
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var out strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(c)
	}
	return out.String()
}

// truncate clips s to at most n runes (not bytes), so multibyte names don't
// get cut mid-glyph.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// padTo right-pads s with spaces to width (truncating if longer) so a
// reverse-video highlight fills the whole row. Counts runes, not bytes, so the
// multibyte glyphs in the cards (▸, ●) line up with their on-screen cells.
func padTo(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		if width <= 0 {
			return ""
		}
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}
