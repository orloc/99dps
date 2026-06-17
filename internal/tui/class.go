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

// classBoxTitle labels the small class box under the Mob Tracker — the detected
// class, or "Class" until a /who reveals it.
func (m Model) classBoxTitle() string {
	if m.tracker != nil {
		if c := m.tracker.Class(); c != eqclass.ClassUnknown {
			return string(c)
		}
	}
	return "Class"
}

// classBoxView renders the small class box body. Prototype: a rune-health
// estimate. EQ never reports absorbed amounts, so we estimate from the blows the
// runes have soaked × the mobs' average melee hit (mobstats), draining the active
// shields in their stacking order. Everything's a guess, so it reads with a "~".
func (m Model) classBoxView(w, h int) string {
	th := themes[m.theme]
	iw := max(w-4, 1)
	now := time.Now().Unix()

	shields, blows, attacker := m.tracker.RuneShields(now)
	if len(shields) == 0 {
		return th.fg(th.dim).Render(truncate("no rune up", iw))
	}

	// size the estimate by the attacker's own recent (last 30 min) average hit, so
	// it's specific to what's beating on the rune; fall back to all mobs.
	avg, haveAvg := 0, false
	if m.sm != nil {
		mi := m.sm.MobStats()
		if attacker != "" {
			avg, haveAvg = mi.AvgHit(attacker, now)
		}
		if !haveAvg {
			avg, haveAvg = mi.Overall(now)
		}
	}
	rem := runeRemaining(shields, blows, avg)

	// header: the average hit the estimate is built from (so the guess is legible).
	var lines []string
	if haveAvg && avg > 0 {
		lines = append(lines, th.fg(th.dim).Render(truncate(fmt.Sprintf("avg hit %d · %d soaked", avg, blows), iw)))
	} else {
		lines = append(lines, th.fg(th.dim).Render(truncate("no hit data yet", iw)))
	}
	for i, s := range shields {
		var detail string
		if !haveAvg || avg <= 0 {
			detail = th.fg(th.dim).Render(fmt.Sprintf("%d soaked", blows)) // no hit data → raw count
		} else {
			col := urgencyColor(th, int64(rem[i]), int64(s.Pool))
			detail = th.fg(col).Render(fmt.Sprintf("~%d (%dh)", rem[i], rem[i]/avg))
		}
		lines = append(lines, truncate(th.fg(th.text).Render(runeShort(s.Name)+" ")+detail, iw))
	}
	if ih := max(h-3, 1); len(lines) > ih { // clip to the card's inner height
		lines = lines[:ih]
	}
	return strings.Join(lines, "\n")
}

// runeRemaining estimates each shield's leftover absorb, draining them in order:
// total damage soaked (blows × average hit) eats the first shield, then overflows
// into the next.
func runeRemaining(shields []gamestate.RuneShield, blows, avgHit int) []int {
	absorbed := blows * avgHit
	out := make([]int, len(shields))
	for i, s := range shields {
		r := s.Pool - absorbed
		if r < 0 {
			r = 0
		}
		out[i] = r
		if absorbed -= s.Pool; absorbed < 0 {
			absorbed = 0
		}
	}
	return out
}

// runeShort compacts a rune name for the narrow box ("Rune V" → "RV").
func runeShort(name string) string {
	if s, ok := strings.CutPrefix(name, "Rune "); ok {
		return "R" + s
	}
	return name
}

// classPanelTitle labels the class-aware bottom panel by the player's category.
// When the Enemy column is split out, the panel holds only buffs (+ skills for a
// hybrid), so the title reflects that.
func classPanelTitle(tr *gamestate.Tracker, enemySplit bool) string {
	if tr == nil {
		return "Spell Timers"
	}
	switch tr.Category() {
	case eqclass.CatMelee:
		return "Skills"
	case eqclass.CatHybrid:
		if enemySplit {
			return "Buffs + Skills"
		}
		return "Spells + Skills"
	default: // caster
		if enemySplit {
			return "Buffs"
		}
		return "Spell Timers"
	}
}

// classPanel is the class-aware bottom panel: independently-gated indicator
// sections (canni / feign / bind / cooldowns) stacked above a category-driven
// body — caster→timers, melee→skills, hybrid→both. Mirrors the previous class-aware panel dispatch.
// It returns the panel text plus a line→target map (shifted past the stacked
// sections) so the model can resolve hover/click-to-dismiss; hover is the
// highlighted target.
// enemySplit is true when the Enemy column is shown separately — then the class
// panel's timer body holds only BUFFS (CC + debuffs move to the Enemy column).
// When false (melee has no timers; or a too-narrow window collapsed the column),
// the body holds the full CC + DEBUFFS + BUFFS stack.
func (m Model) classPanel(cur *session.CombatSession, w int, hover string, enemySplit bool) (string, map[int]string) {
	th := themes[m.theme]
	tr := m.tracker
	if tr == nil {
		return timerColumn(th, nil, w, hover, true, true, true)
	}
	now := time.Now().Unix()
	class, level := tr.Class(), tr.Level()

	// gated indicator sections stack above the body; count their lines so the
	// body's line→target map can be shifted down to match (cf. the previous stackPanel).
	// (The canni dance meter is no longer here — it's pinned to the bottom of the
	// Damage panel, see canniFooter.)
	var sections []string
	switch tr.FeignStatus(now) {
	case gamestate.FeignFailed:
		sections = append(sections, badge(th, "#e0564e", "⚠ FEIGN FAILED — mobs still on you", w))
	case gamestate.FeignOK:
		sections = append(sections, badge(th, "#5fd37a", "✓ feigned", w))
	}
	if rem, ok := tr.BindRemaining(now); ok {
		sections = append(sections, badge(th, th.accent, fmt.Sprintf("⏳ bandaging… %s", mmss(int64(rem))), w))
	}
	if sp, ok := tr.Resisted(now); ok {
		sections = append(sections, badge(th, "#e0564e", truncate("✦ "+sp+" resisted", w), w))
	}
	if cds := tr.Cooldowns(now); len(cds) > 0 {
		for i := range cds { // name the generic kick/strike by level (Flying Kick, etc.)
			cds[i].Name = eqclass.SkillName(class, cds[i].Name, level)
		}
		sections = append(sections, cooldownRows(th, cds, w))
	}
	// the discipline widget — only melee/hybrid classes have any (tr.Disciplines
	// returns nil otherwise), and it's toggleable (default on).
	if !m.layoutPrefs.DiscHidden {
		if ds := tr.Disciplines(now); len(ds) > 0 {
			sections = append(sections, disciplineRows(th, ds, w))
		}
	}

	// when the Enemy column is split out, the class panel shows only BUFFS;
	// otherwise it carries the full CC + DEBUFFS + BUFFS stack.
	wantCC, wantDebuffs := !enemySplit, !enemySplit
	var body string
	var bodyMap map[int]string
	switch tr.Category() {
	case eqclass.CatMelee:
		body = skillsBody(th, cur, class, level, w)
		if !enemySplit { // no separate Buffs column (narrow) — fold self-buffs below skills
			if bf, bft := timerColumn(th, tr, w, hover, false, false, true); len(bft) > 0 {
				body += "\n" + th.fg(th.accentLo).Render(strings.Repeat("─", w)) + "\n" + bf
			}
		}
	case eqclass.CatHybrid:
		body, bodyMap = timerColumn(th, tr, w, hover, wantCC, wantDebuffs, true)
		if sum := skillsSummaryLine(cur, class, level); sum != "" {
			body += "\n" + th.fg(th.accentLo).Render(strings.Repeat("─", w)) + "\n" + th.fg(th.dim).Render(truncate(sum, w))
		}
	default: // caster
		body, bodyMap = timerColumn(th, tr, w, hover, wantCC, wantDebuffs, true)
	}

	prefix := 0
	for _, s := range sections {
		prefix += strings.Count(s, "\n") + 1
	}
	shifted := bodyMap
	if prefix > 0 && len(bodyMap) > 0 {
		shifted = make(map[int]string, len(bodyMap))
		for k, v := range bodyMap {
			shifted[k+prefix] = v
		}
	}
	return strings.Join(append(sections, body), "\n"), shifted
}

// badge is a full-width filled pill bar (dark text on an accent fill).
func badge(th theme, bgHex, text string, w int) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(th.bg)).Background(lipgloss.Color(bgHex)).
		Bold(true).Width(w).Render(truncate(text, w))
}

// canniFooter is the shaman "canni dance" meter, pinned at the bottom of the
// Damage panel so it stays visible while the dealer list scrolls. It renders a
// fixed 4 lines — a divider rule, a graded headline, a throughput bar, and a
// detail line — whenever the player is dancing OR has danced this session, so
// the height it reserves stays stable across the dance. With the full Damage
// width it shows more than the old narrow readout: rank, the recast beat, combo,
// score, session best, and early ("buzzer") presses. Returns the block and its
// line count; ("", 0) when there's nothing to show yet.
func canniFooter(th theme, c gamestate.CanniStats, w int) (string, int) {
	if !c.Active && c.Best == 0 && c.Score == 0 {
		return "", 0 // never danced this session — nothing to pin
	}
	rule := th.fg(th.accentLo).Render(strings.Repeat("─", max(w, 0)))
	detailLine := func(parts ...string) string {
		var nz []string
		for _, p := range parts {
			if p != "" {
				nz = append(nz, p)
			}
		}
		return th.fg(th.dim).Render(truncate(strings.Join(nz, " · "), w))
	}
	if !c.Active { // idle between dances — keep it present but muted
		head := th.fg(th.dim).Bold(true).Render(truncate("⟳ CANNI DANCE — idle", w))
		bar := gradientBar(0, w, th.dim, th.dim, th.track)
		detail := detailLine(c.Rank, fmt.Sprintf("best %d%%", c.Best), humanize(c.Score)+" pts")
		return strings.Join([]string{rule, head, bar, detail}, "\n"), 4
	}
	grade, col := canniGrade(c.Pct)
	head := badge(th, col, fmt.Sprintf("⟳ CANNI DANCE   %d%%  grade %s   ×%d combo", c.Pct, grade, c.Combo), w)
	bar := gradientBar(float64(c.Pct)/100, w, col, col, th.track)
	early := ""
	if c.Buzzers > 0 {
		early = fmt.Sprintf("%d early", c.Buzzers)
	}
	detail := detailLine(c.Rank, fmt.Sprintf("beat %.2fs", float64(c.EdgeMs)/1000),
		humanize(c.Score)+" pts", fmt.Sprintf("best %d%%", c.Best), early)
	return strings.Join([]string{rule, head, bar, detail}, "\n"), 4
}

func canniGrade(pct int) (grade, colorHex string) {
	switch {
	case pct >= 95:
		return "S", "#5fd37a"
	case pct >= 85:
		return "A", "#7bc86a"
	case pct >= 70, pct >= 50:
		return map[bool]string{true: "B", false: "C"}[pct >= 70], "#d4af37"
	default:
		return "D", "#e0564e"
	}
}

// cooldownRows lists activated-ability reuse (Mend, Feign Death) as a small
// charge box that fills left→right as the ability nears ready — blue while
// counting, full green when ready — with the time alongside. On a panel too
// narrow for a box it falls back to a plain name + time row.
func cooldownRows(th theme, cds []gamestate.CooldownTimer, w int) string {
	lines := []string{th.fg(th.accent).Bold(true).Render("COOLDOWNS")}
	const (
		nameW  = 12 // fits "Eagle Strike" / "Dragon Punch"
		timeW  = 5  // "m:ss" or "ready"
		charge = "#5aa9e6"
		ready  = "#5fd37a"
	)
	boxW := min(w-2-nameW-1-1-timeW, 12) // indent + name + gaps + time
	for _, cd := range cds {
		if boxW < 4 { // too narrow for a box — plain text
			if cd.Remaining <= 0 {
				lines = append(lines, badge(th, ready, "  "+truncate(cd.Name, 13)+" ready", w))
			} else {
				lines = append(lines, th.fg(th.text).Render(fmt.Sprintf("  %-13s %s", truncate(cd.Name, 13), mmss(cd.Remaining))))
			}
			continue
		}
		name := fmt.Sprintf("%-*s", nameW, truncate(cd.Name, nameW))
		if cd.Remaining <= 0 { // ready: a full green box
			lines = append(lines, "  "+th.fg(ready).Render(name)+" "+
				gradientBar(1, boxW, ready, ready, th.track)+" "+th.fg(ready).Render("ready"))
			continue
		}
		frac := 0.0
		if cd.Total > 0 { // fills toward ready as the remaining time shrinks
			frac = float64(cd.Total-cd.Remaining) / float64(cd.Total)
		}
		lines = append(lines, "  "+th.fg(th.text).Render(name)+" "+
			gradientBar(frac, boxW, charge, charge, th.track)+" "+
			th.fg(th.dim).Render(fmt.Sprintf("%*s", timeW, mmss(cd.Remaining))))
	}
	return strings.Join(lines, "\n")
}

// disciplineRows renders the discipline widget: every discipline the player's
// level has earned, the live ones (active / cooling down) first, then the ready
// ones. Active shows the effect's time left (gold), cooling shows the reuse
// countdown as a charge box that fills toward ready (blue), ready is a green box.
// State only becomes live once the parser detects a discipline activation; until
// then it's a class cheat-sheet (everything Ready).
func disciplineRows(th theme, ds []gamestate.DisciplineTimer, w int) string {
	sort.SliceStable(ds, func(i, j int) bool {
		oi, oj := discOrder(ds[i].Phase), discOrder(ds[j].Phase)
		if oi != oj {
			return oi < oj // active, then cooling, then ready
		}
		if ds[i].Phase == gamestate.DiscCooling && ds[i].Remaining != ds[j].Remaining {
			return ds[i].Remaining < ds[j].Remaining // soonest-ready cooling first
		}
		return ds[i].Level < ds[j].Level
	})

	lines := []string{th.fg(th.accent).Bold(true).Render("DISCIPLINES")}
	const (
		nameW  = 14 // fits "Mighty Strike" / "Sanctification"
		timeW  = 5
		active = "#d4af37"
		charge = "#5aa9e6"
		ready  = "#5fd37a"
	)
	boxW := min(w-2-nameW-1-1-timeW, 12)
	for _, d := range ds {
		name := fmt.Sprintf("%-*s", nameW, truncate(d.Name, nameW))
		if boxW < 4 { // too narrow for a box — plain text
			switch d.Phase {
			case gamestate.DiscActive:
				lines = append(lines, "  "+th.fg(active).Render(name)+" "+th.fg(active).Render(mmss(d.Remaining)))
			case gamestate.DiscCooling:
				lines = append(lines, "  "+th.fg(th.text).Render(name)+" "+th.fg(th.dim).Render(mmss(d.Remaining)))
			default:
				lines = append(lines, "  "+th.fg(ready).Render(name)+" "+th.fg(ready).Render("ready"))
			}
			continue
		}
		switch d.Phase {
		case gamestate.DiscActive: // effect running — a gold bar draining with the duration
			frac := 0.0
			if d.DurSec > 0 {
				frac = float64(d.Remaining) / float64(d.DurSec)
			}
			lines = append(lines, "  "+th.fg(active).Render(name)+" "+
				gradientBar(frac, boxW, active, active, th.track)+" "+
				th.fg(active).Render(fmt.Sprintf("%*s", timeW, mmss(d.Remaining))))
		case gamestate.DiscCooling: // reuse counting down — fills toward ready
			frac := 0.0
			if d.ReuseSec > 0 {
				frac = float64(d.ReuseSec-d.Remaining) / float64(d.ReuseSec)
			}
			lines = append(lines, "  "+th.fg(th.text).Render(name)+" "+
				gradientBar(frac, boxW, charge, charge, th.track)+" "+
				th.fg(th.dim).Render(fmt.Sprintf("%*s", timeW, mmss(d.Remaining))))
		default: // ready
			lines = append(lines, "  "+th.fg(ready).Render(name)+" "+
				gradientBar(1, boxW, ready, ready, th.track)+" "+th.fg(ready).Render("ready"))
		}
	}
	return strings.Join(lines, "\n")
}

// discOrder sorts discipline phases: active first (most urgent), then cooling,
// then ready.
func discOrder(p gamestate.DiscPhase) int {
	switch p {
	case gamestate.DiscActive:
		return 0
	case gamestate.DiscCooling:
		return 1
	default:
		return 2
	}
}

// skillsBody is the pure-melee panel: the player's activated-skill breakdown
// (class/level-labelled) plus accuracy.
func skillsBody(th theme, cur *session.CombatSession, class eqclass.Class, level, w int) string {
	if cur == nil {
		return th.fg(th.dim).Render("No fight selected.")
	}
	lines := []string{th.fg(th.accent).Bold(true).Render("SKILLS · THIS FIGHT")}
	type row struct {
		name string
		s    combat.SkillStat
	}
	var rows []row
	for n, s := range cur.Skills() {
		if eqclass.SkillRelevant(class, n) {
			rows = append(rows, row{eqclass.SkillName(class, n, level), s})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].s.Total > rows[j].s.Total })
	if len(rows) == 0 {
		lines = append(lines, th.fg(th.dim).Render("  no skill attacks yet"))
	}
	showMax := w >= 44 // the per-skill "max" column only fits a roomy panel
	for _, r := range rows {
		line := fmt.Sprintf("  %-12s %6s  %d hits", truncate(r.name, 12), humanize(r.s.Total), r.s.Hits)
		if showMax && r.s.Max > 0 {
			line += fmt.Sprintf("  max %s", humanize(r.s.Max))
		}
		lines = append(lines, th.fg(th.text).Render(truncate(line, w)))
	}
	// the biggest single melee hit overall (auto-attack + specials) — a short line,
	// shown even before any skill has landed.
	if you := playerStat(cur); you.Max > 0 {
		lines = append(lines, th.fg(th.dim).Render(truncate(fmt.Sprintf("  %-12s %6s", "Max hit", humanize(you.Max)), w)))
	}
	lines = append(lines, "", th.fg(th.accent).Bold(true).Render("ACCURACY"))
	if hr := cur.OffenseFor("You").HitRate(); hr >= 0 {
		lines = append(lines, th.fg(th.dim).Render(fmt.Sprintf("  Hit rate   %3d%%", hr)))
	}
	if you := playerStat(cur); you.Hits > 0 {
		if c := cur.CritFor("You"); c.Count > 0 {
			lines = append(lines, th.fg(th.dim).Render(fmt.Sprintf("  Crit rate  %3d%%", critPct(c.Count, you.Hits))))
		}
	}
	if av, faced := playerAvoidance(cur); faced > 0 {
		lines = append(lines, th.fg(th.dim).Render(fmt.Sprintf("  Avoided    %3d%%", av*100/faced)))
	}
	return strings.Join(lines, "\n")
}

// skillsSummaryLine is the one-line skill digest for the hybrid panel.
func skillsSummaryLine(cur *session.CombatSession, class eqclass.Class, level int) string {
	if cur == nil {
		return ""
	}
	var parts []string
	if name, s := topSkill(cur.Skills(), class); name != "" {
		parts = append(parts, fmt.Sprintf("%s %s", eqclass.SkillName(class, name, level), humanize(s.Total)))
	}
	if you := playerStat(cur); you.Hits > 0 {
		if c := cur.CritFor("You"); c.Count > 0 {
			parts = append(parts, fmt.Sprintf("Crit %d%%", critPct(c.Count, you.Hits)))
		}
	}
	if hr := cur.OffenseFor("You").HitRate(); hr >= 0 {
		parts = append(parts, fmt.Sprintf("Hit %d%%", hr))
	}
	return strings.Join(parts, " · ")
}

// ---- ported skill/stat helpers (from the old render layer) -----------------------

func critPct(crits, hits int) int {
	if hits <= 0 {
		return 0
	}
	if p := crits * 100 / hits; p <= 100 {
		return p
	}
	return 100
}

func topSkill(skills map[string]combat.SkillStat, class eqclass.Class) (string, combat.SkillStat) {
	var name string
	var best combat.SkillStat
	for n, s := range skills {
		if eqclass.SkillRelevant(class, n) && s.Total > best.Total {
			name, best = n, s
		}
	}
	return name, best
}

func playerStat(cur *session.CombatSession) combat.DamageStat {
	for _, v := range cur.GetAggressors() {
		if strings.EqualFold(v.Dealer, "you") {
			return v
		}
	}
	return combat.DamageStat{}
}

func playerAvoidance(cur *session.CombatSession) (avoided, faced int) {
	for _, d := range cur.Defense() {
		if strings.EqualFold(d.Name, "you") {
			return d.Stats.Avoided(), d.Stats.Swings()
		}
	}
	return 0, 0
}
