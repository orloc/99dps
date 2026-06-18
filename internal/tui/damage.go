package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"99dps/internal/eq/combat"
	"99dps/internal/eq/gamestate"
	"99dps/internal/session"
)

// damageContent forwards to damageMeter using this Model's theme/tracker/character
// and the Meter tab's compact preference. The Sessions tab calls damageMeter
// directly with compact=false, so its breakdown is always full regardless of the
// Meter's layout setting.
func (m Model) damageContent(cur *session.CombatSession, live bool, width int) string {
	return damageMeter(themes[m.theme], m.tracker, m.character, cur, live, m.layoutPrefs.Damage == panelCompact, width)
}

// damageMeter is the scrollable damage breakdown for the selected fight: an
// encounter summary, a ranked per-dealer table (share bar + DPS/Total/% and
// width-gated Hit%/Crit%), the unattributed spell line, then the Specials and
// Avoidance sub-tables — matching the old Damage panel. Pure (no Model state), so
// it's unit-testable directly; compact slims the columns to name + bar + Total + DPS.
func damageMeter(th theme, tracker *gamestate.Tracker, character string, cur *session.CombatSession, live, compact bool, width int) string {
	if cur == nil {
		return th.fg(th.dim).Render("No fight selected.\nFight something!")
	}
	stats := cur.GetAggressors() // ranked below, after spell/pet damage is rolled in
	magic := cur.MagicTotal()
	encTotal := cur.Total() + magic // melee + unattributed spell damage
	span := cur.LastUnix() - cur.StartTime().Unix()
	if span < 1 {
		span = 1
	}
	// encounter summary: duration · total · dps · live/ended. Colored when it
	// fits; clipped (plain) on a very narrow panel so it never overflows.
	lead := fmt.Sprintf("%s · %s · %s/s", fmtDuration(cur.Duration()), humanize(encTotal), humanize(int(int64(encTotal)/span)))
	statusText := "● live"
	statusColor := "#5fd37a"
	if !live {
		statusText, statusColor = "○ ended "+cur.EndTime().Format("15:04:05"), th.dim
	}
	summary := th.fg(th.dim).Render(lead+"   ") + th.fg(statusColor).Render(statusText)
	if lipgloss.Width(lead)+3+lipgloss.Width(statusText) > width {
		summary = th.fg(th.dim).Render(truncate(lead+"  "+statusText, width))
	}

	// Columns flex with width: Total+DPS are always shown; %, Hit%, Crit% are
	// added as room allows; the name takes the rest, and a share bar fills any
	// slack beyond the name — so a row is always exactly the panel width.
	const rankW, nameMax, pctW, totW, dpsW = 2, 14, 4, 7, 7
	showPct := width >= 34
	showHit := width >= 60
	showCrit := width >= 70
	if compact {
		showPct, showHit, showCrit = false, false, false // slim: name + bar + Total + DPS
	}

	num := totW + 1 + dpsW // the right-aligned numeric block width
	if showPct {
		num += pctW + 1
	}
	if showHit {
		num += 1 + 5
	}
	if showCrit {
		num += 1 + 5
	}
	nameW := width - num - 1 - rankW - 1 // rank + gaps + name fill the left
	barCells, showBar := 0, false
	if nameW > nameMax {
		if barCells = nameW - nameMax - 1; barCells >= 4 {
			nameW, showBar = nameMax, true
		} else {
			barCells = 0
		}
	}
	nameW = max(nameW, 1)

	// numBlock right-aligns the numeric columns into exactly `num` cells.
	numBlock := func(pctv, total, dps int, hit, crit, col string) string {
		s := ""
		if showPct {
			s += rightCell(fmt.Sprintf("%d%%", pctv), pctW, col) + " "
		}
		// all numeric columns in the row's normal color (children pass col=dim).
		s += rightCell(humanize(total), totW, col) + " " + rightCell(humanize(dps), dpsW, col)
		if showHit {
			s += " " + rightCell(hit, 5, col)
		}
		if showCrit {
			s += " " + rightCell(crit, 5, col)
		}
		return s
	}

	var b strings.Builder
	b.WriteString(summary + "\n")
	b.WriteString(strings.Repeat(" ", max(width-num, 0)) +
		numBlockLabels(th, showPct, showHit, showCrit, pctW, totW, dpsW) + "\n")

	// from=="rainbow" → a horizontal red→violet rainbow across the bar (every
	// ranked meter shares the spectrum); otherwise a solid from→to gradient (the
	// dim breakdown children).
	row := func(rankStr string, nameCell, hit, crit string, frac float64, from, to, col string, total, dps, pctv int) string {
		mid := ""
		if showBar {
			if from == "rainbow" {
				mid = rainbowBarH(frac, barCells, th.track) + " "
			} else {
				mid = gradientBar(frac, barCells, from, to, th.track) + " "
			}
		}
		return rightCell(rankStr, rankW, th.dim) + " " + nameCell + " " + mid +
			numBlock(pctv, total, dps, hit, crit, col)
	}

	// attribute each pet under its OWNER (from the pet→owner map): a pet whose
	// owner is also a dealer here is pulled out of the ranking and nested as a
	// "↳ <pet>" child of that owner — your own pet under You, a group-mate's pet
	// under them. A pet whose owner dealt no damage stays ranked, tagged "(<owner>'s
	// pet)" so it's never silently lumped under you.
	present := map[string]bool{}
	for _, d := range stats {
		present[strings.ToLower(d.Dealer)] = true
	}
	hasYou := present["you"]

	// EQ shows a client only its OWN non-melee, so all spell/proc/DoT damage is the
	// player's. A spell caster may never melee a mob, but once they cast on it they
	// should appear on the chart: fold the spell damage into the "You" row —
	// synthesizing one if the player never meleed — so the damage is attributed to
	// them, their pet links under them, and (a damage-shield-only or pet-only
	// caster) still shows up. Owning a fighting pet alone also puts them on the board.
	playerOwnsPet := false
	if tracker != nil {
		for _, d := range stats {
			if o := tracker.PetOwner(d.Dealer); o != "" && strings.EqualFold(o, character) {
				playerOwnsPet = true
				break
			}
		}
	}
	if !hasYou && (magic > 0 || playerOwnsPet) {
		stats = append(stats, combat.DamageStat{Dealer: "You"})
		present["you"] = true // rollup nests the player's pet under the synthesized You row
	}
	if magic > 0 { // the player's row includes their own spell/proc/DoT damage
		for i := range stats {
			if strings.EqualFold(stats[i].Dealer, "You") {
				stats[i].Total += magic
				break
			}
		}
	}

	ownerRow := func(dealer string) string { // the dealer a pet nests under, or ""
		o := tracker.PetOwner(dealer)
		if o == "" {
			return ""
		}
		if strings.EqualFold(o, character) {
			return "You"
		}
		return o
	}
	children := map[string][]combat.DamageStat{} // owner (lower) → its pets
	kept := stats[:0]
	for _, d := range stats {
		if owner := ownerRow(d.Dealer); owner != "" && present[strings.ToLower(owner)] {
			children[strings.ToLower(owner)] = append(children[strings.ToLower(owner)], d)
			continue
		}
		kept = append(kept, d)
	}
	stats = kept

	// a pet's damage rolls UP into its owner's row, so that row is inclusive of
	// everything the owner put out (the player's: melee + spells + pet). The pet
	// stays visible as a dim "↳ <pet>" breakdown child below; without the rollup
	// the owner's Total/DPS/share silently dropped the pet's contribution.
	for ownerLower, pets := range children {
		sum := 0
		for _, p := range pets {
			sum += p.Total
		}
		for i := range stats {
			if strings.ToLower(stats[i].Dealer) == ownerLower {
				stats[i].Total += sum
				break
			}
		}
	}

	// rank and scale the bars by the now-inclusive row totals
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	maxTotal := 1
	for _, d := range stats {
		if d.Total > maxTotal {
			maxTotal = d.Total
		}
	}

	// acc returns the hit%/crit% cells for a dealer (its own DamageStat for crits).
	acc := func(d combat.DamageStat) (hit, crit string) {
		hit, crit = "-", "-"
		if hr := cur.OffenseFor(d.Dealer).HitRate(); hr >= 0 {
			hit = fmt.Sprintf("%d%%", hr)
		}
		if c := cur.CritFor(d.Dealer); c.Count > 0 && d.Hits > 0 {
			crit = fmt.Sprintf("%d%%", critPct(c.Count, d.Hits))
		}
		return hit, crit
	}
	// child renders a dim, indented "↳ <label>" breakdown row.
	childRow := func(label string, total int, hit, crit string) string {
		return row("", th.fg(th.dim).Width(nameW).Render(truncate(label, nameW)),
			hit, crit, float64(total)/float64(maxTotal), th.dim, th.dim, th.dim,
			total, total/int(span), pct(total, encTotal))
	}

	for i, d := range stats {
		nameStyle, col := th.fg(th.text), th.text
		you := strings.EqualFold(d.Dealer, "you")
		if you {
			nameStyle = nameStyle.Bold(true).Foreground(lipgloss.Color(th.accent))
		}
		name := d.Dealer
		if o := tracker.PetOwner(d.Dealer); o != "" { // orphan pet — credit its owner
			if strings.EqualFold(o, character) {
				name = "your pet"
			} else {
				name = o + "'s pet"
			}
		}
		hit, crit := acc(d)
		b.WriteString(row(fmt.Sprintf("%d", i+1), nameStyle.Width(nameW).Render(truncate(name, nameW)),
			hit, crit, float64(d.Total)/float64(maxTotal), "rainbow", "", col, d.Total, d.Total/int(span), pct(d.Total, encTotal)) + "\n")

		// your own unattributed spell/proc/DoT damage, under You (EQ shows you only
		// your own non-melee, so it's almost always yours)
		if you && magic > 0 {
			b.WriteString(childRow("↳ spells", magic, "-", "-") + "\n")
		}
		// this dealer's pets, nested as dim children
		for _, p := range children[strings.ToLower(d.Dealer)] {
			phit, pcrit := acc(p)
			b.WriteString(childRow("↳ "+p.Dealer, p.Total, phit, pcrit) + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// numBlockLabels renders the damage table's right-aligned column header to match
// numBlock's column set and width.
func numBlockLabels(th theme, showPct, showHit, showCrit bool, pctW, totW, dpsW int) string {
	s := ""
	if showPct {
		s += rightCell("%", pctW, th.dim) + " "
	}
	s += rightCell("Total", totW, th.dim) + " " + rightCell("DPS", dpsW, th.dim)
	if showHit {
		s += " " + rightCell("Hit", 5, th.dim)
	}
	if showCrit {
		s += " " + rightCell("Crit", 5, th.dim)
	}
	return s
}

// extrasContent is the side column beside the damage meter: the per-dealer,
// per-kind Specials breakdown and the Avoidance table, stacked and scrollable.
func (m Model) extrasContent(cur *session.CombatSession, width int) string {
	th := themes[m.theme]
	if cur == nil {
		return th.fg(th.dim).Render("—")
	}
	stats := cur.GetAggressors()
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })

	var parts []string
	if sp := damageSpecials(th, cur, stats, width); sp != "" {
		parts = append(parts, sp)
	}
	// compact mode shows only the Specials block, dropping the Avoidance table.
	if m.layoutPrefs.OffDef != panelCompact {
		if av := damageAvoidance(th, cur, width); av != "" {
			parts = append(parts, av)
		}
	}
	if len(parts) == 0 {
		return th.fg(th.dim).Render("No specials or\navoidance yet.")
	}
	return strings.Join(parts, "\n\n")
}
