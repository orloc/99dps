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

// classPanelTitle labels the class-aware bottom panel by the player's category.
func classPanelTitle(tr *gamestate.Tracker) string {
	if tr == nil {
		return "Spell Timers"
	}
	switch tr.Category() {
	case eqclass.CatMelee:
		return "Skills"
	case eqclass.CatHybrid:
		return "Spells + Skills"
	default:
		return "Spell Timers"
	}
}

func (m Model) isEnchanter() bool {
	return m.tracker != nil && m.tracker.Class() == eqclass.ClassEnchanter
}

// classPanel is the class-aware bottom panel: independently-gated indicator
// sections (canni / feign / bind / cooldowns) stacked above a category-driven
// body — caster→timers, melee→skills, hybrid→both. Mirrors the previous class-aware panel dispatch.
// It returns the panel text plus a line→target map (shifted past the stacked
// sections) so the model can resolve hover/click-to-dismiss; hover is the
// highlighted target.
func (m Model) classPanel(cur *session.CombatSession, w int, hover string) (string, map[int]string) {
	th := themes[m.theme]
	tr := m.tracker
	if tr == nil {
		return timersBody(th, nil, w, true, hover)
	}
	now := time.Now().Unix()

	// gated indicator sections stack above the body; count their lines so the
	// body's line→target map can be shifted down to match (cf. the previous stackPanel).
	var sections []string
	if cm := canniMeter(th, tr.CanniStats(now), w); cm != "" {
		sections = append(sections, cm)
	}
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
		sections = append(sections, cooldownRows(th, cds, w))
	}

	var body string
	var bodyMap map[int]string
	class, level := tr.Class(), tr.Level()
	switch tr.Category() {
	case eqclass.CatMelee:
		body = skillsBody(th, cur, class, level, w)
	case eqclass.CatHybrid:
		body, bodyMap = timersBody(th, tr, w, true, hover)
		if sum := skillsSummaryLine(cur, class, level); sum != "" {
			body += "\n" + th.fg(th.accentLo).Render(strings.Repeat("─", w)) + "\n" + th.fg(th.dim).Render(truncate(sum, w))
		}
	default: // caster — enchanters keep CC in their own column
		body, bodyMap = timersBody(th, tr, w, !m.isEnchanter(), hover)
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

// canniMeter is the shaman "canni dance" efficiency readout (grade + bar). "".
func canniMeter(th theme, c gamestate.CanniStats, w int) string {
	if !c.Active {
		return ""
	}
	grade, col := canniGrade(c.Pct)
	head := badge(th, col, fmt.Sprintf("⟳ CANNI  %d%%  %s  ×%d", c.Pct, grade, c.Combo), w)
	bar := gradientBar(float64(c.Pct)/100, w, col, col, th.track)
	detail := th.fg(th.dim).Render(truncate(
		fmt.Sprintf("%s · %.2fs · %s pts · best %d%%", c.Rank, float64(c.EdgeMs)/1000, humanize(c.Score), c.Best), w))
	return head + "\n" + bar + "\n" + detail
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

// cooldownRows lists activated-ability reuse (Mend, Feign Death): green "ready"
// or a counting-down timer.
func cooldownRows(th theme, cds []gamestate.CooldownTimer, w int) string {
	lines := []string{th.fg(th.accent).Bold(true).Render("COOLDOWNS")}
	for _, cd := range cds {
		if cd.Remaining <= 0 {
			lines = append(lines, badge(th, "#5fd37a", "  "+truncate(cd.Name, 13)+" ready", w))
		} else {
			lines = append(lines, th.fg(th.text).Render(fmt.Sprintf("  %-13s %s", truncate(cd.Name, 13), mmss(cd.Remaining))))
		}
	}
	return strings.Join(lines, "\n")
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
		if skillRelevant(n, class) {
			rows = append(rows, row{displaySkillName(n, class, level), s})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].s.Total > rows[j].s.Total })
	if len(rows) == 0 {
		lines = append(lines, th.fg(th.dim).Render("  no skill attacks yet"))
	}
	for _, r := range rows {
		lines = append(lines, th.fg(th.text).Render(fmt.Sprintf("  %-12s %6s  %d hits", truncate(r.name, 12), humanize(r.s.Total), r.s.Hits)))
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
		parts = append(parts, fmt.Sprintf("%s %s", displaySkillName(name, class, level), humanize(s.Total)))
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

func displaySkillName(generic string, class eqclass.Class, level int) string {
	if class == eqclass.ClassMonk && generic == "Kick" && level >= 30 {
		return "Flying Kick"
	}
	return generic
}

func skillRelevant(generic string, class eqclass.Class) bool {
	if generic == "Strike" {
		return class == eqclass.ClassMonk
	}
	return true
}

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
		if skillRelevant(n, class) && s.Total > best.Total {
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
