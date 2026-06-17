package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// TestCritPct: a safe percentage that floors and caps at 100.
func TestCritPct(t *testing.T) {
	cases := []struct{ crits, hits, want int }{
		{0, 0, 0},   // no hits → 0, no divide-by-zero
		{1, 0, 0},   // guard
		{1, 4, 25},  // floor
		{1, 3, 33},  // floor
		{9, 5, 100}, // capped (a crit can also be a hit; never >100%)
	}
	for _, c := range cases {
		if got := critPct(c.crits, c.hits); got != c.want {
			t.Errorf("critPct(%d,%d) = %d, want %d", c.crits, c.hits, got, c.want)
		}
	}
}

// TestTopSkill picks the highest-damage relevant skill (and filters Strike to
// monks).
func TestTopSkill(t *testing.T) {
	skills := map[string]combat.SkillStat{
		"Kick":   {Total: 100, Hits: 3},
		"Crush":  {Total: 500, Hits: 9},
		"Strike": {Total: 9000, Hits: 1}, // irrelevant to a warrior
	}
	if n, s := topSkill(skills, eqclass.ClassWarrior); n != "Crush" || s.Total != 500 {
		t.Errorf("warrior topSkill = %q/%d, want Crush/500", n, s.Total)
	}
	// for a monk Strike is relevant, so it wins on damage.
	if n, _ := topSkill(skills, eqclass.ClassMonk); n != "Strike" {
		t.Errorf("monk topSkill = %q, want Strike", n)
	}
	// no skills → empty.
	if n, _ := topSkill(map[string]combat.SkillStat{}, eqclass.ClassMonk); n != "" {
		t.Errorf("empty skills should yield no top skill, got %q", n)
	}
}

// TestSkillsSummaryLine renders a hybrid one-liner with the top skill + crit/hit,
// and is empty for a nil session.
func TestSkillsSummaryLine(t *testing.T) {
	if got := skillsSummaryLine(nil, eqclass.ClassRanger, 60); got != "" {
		t.Errorf("nil session should give an empty summary, got %q", got)
	}
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 800, Target: "a rat", Verb: "kick"})
	sm.Apply(&combat.DamageSet{ActionTime: 1001, Dealer: "You", Dmg: 200, Target: "a rat"})
	cur := sm.Current()
	got := skillsSummaryLine(cur, eqclass.ClassRanger, 60)
	if !strings.Contains(got, "Kick") {
		t.Errorf("hybrid summary should name the top skill, got %q", got)
	}
}

// TestClassPanelTitle switches on the player's category, splitting out a Buffs
// title when the Enemy column is separate.
func TestClassPanelTitle(t *testing.T) {
	mk := func(cat eqclass.Class) *gamestate.Tracker {
		book, _ := gamestate.LoadReader(strings.NewReader(""))
		tr := gamestate.NewTracker(book)
		tr.SetClass(cat)
		return tr
	}
	cases := []struct {
		tr    *gamestate.Tracker
		split bool
		want  string
	}{
		{nil, false, "Spell Timers"},
		{mk(eqclass.ClassWarrior), false, "Skills"},
		{mk(eqclass.ClassRanger), true, "Buffs + Skills"},
		{mk(eqclass.ClassRanger), false, "Spells + Skills"},
		{mk(eqclass.ClassEnchanter), true, "Buffs"},
		{mk(eqclass.ClassEnchanter), false, "Spell Timers"},
	}
	for _, c := range cases {
		if got := classPanelTitle(c.tr, c.split); got != c.want {
			t.Errorf("classPanelTitle(split=%v) = %q, want %q", c.split, got, c.want)
		}
	}
}

// TestCanniGrade maps the dance percentage to a letter grade.
func TestCanniGrade(t *testing.T) {
	cases := []struct {
		pct   int
		grade string
	}{
		{100, "S"}, {95, "S"},
		{90, "A"}, {85, "A"},
		{75, "B"}, {70, "B"},
		{60, "C"}, {50, "C"},
		{40, "D"}, {0, "D"},
	}
	for _, c := range cases {
		if g, _ := canniGrade(c.pct); g != c.grade {
			t.Errorf("canniGrade(%d) = %q, want %q", c.pct, g, c.grade)
		}
	}
}

// ---- moved from rune_test.go ----

// TestRuneRemaining: damage drains the shields in order — the first absorbs until
// empty, then the overflow eats the next.
func TestRuneRemaining(t *testing.T) {
	sh := []gamestate.RuneShield{{Name: "Bedlam", Pool: 350}, {Name: "Rune V", Pool: 700}}

	if got := runeRemaining(sh, 8, 60); got[0] != 0 || got[1] != 570 { // 480 absorbed
		t.Errorf("8×60: got %v, want [0 570]", got)
	}
	if got := runeRemaining(sh, 3, 60); got[0] != 170 || got[1] != 700 { // 180, all on Bedlam
		t.Errorf("3×60: got %v, want [170 700]", got)
	}
	if got := runeRemaining(sh, 0, 60); got[0] != 350 || got[1] != 700 {
		t.Errorf("fresh: got %v, want [350 700]", got)
	}
}

// ---- moved from skillsmax_test.go ----

// TestSkillsBodyShowsMaxHit: the SKILLS panel shows the overall melee max plus a
// per-skill max column (when the panel is wide enough).
func TestSkillsBodyShowsMaxHit(t *testing.T) {
	sm := &session.SessionManager{}
	for _, h := range []struct {
		dmg  int
		verb string
	}{{50, "crush"}, {120, "kick"}, {200, "crush"}} {
		sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: h.dmg, Target: "a rat", Verb: h.verb})
	}

	out := skillsBody(themes[0], sm.Current(), eqclass.ClassMonk, 60, 60)
	if !strings.Contains(out, "Max hit") || !strings.Contains(out, "200") {
		t.Errorf("skills body should show the overall max hit (200):\n%s", out)
	}
	if !strings.Contains(out, "max 120") {
		t.Errorf("skills body should show the per-skill max (Kick 120):\n%s", out)
	}

	// a narrow panel drops the per-skill max column but keeps the overall line.
	narrow := skillsBody(themes[0], sm.Current(), eqclass.ClassMonk, 60, 30)
	if strings.Contains(narrow, "max 120") {
		t.Errorf("narrow panel should drop the per-skill max column:\n%s", narrow)
	}
	if !strings.Contains(narrow, "Max hit") {
		t.Errorf("narrow panel should still show the overall max hit line:\n%s", narrow)
	}
}

// ---- TestPlayerStatAndAvoidance moved from panels_more_test.go (playerStat/playerAvoidance in class.go) ----

// TestPlayerStatAndAvoidance pull the player's aggregate + defense out of a
// session (case-insensitive "you" match).
func TestPlayerStatAndAvoidance(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 500, Target: "a rat"})
	sm.ApplySwing(&combat.Swing{ActionTime: 1001, Attacker: "a rat", Defender: "You", Outcome: combat.OutcomeDodge})
	cur := sm.Current()
	if st := playerStat(cur); st.Total != 500 {
		t.Errorf("playerStat total = %d, want 500", st.Total)
	}
	av, faced := playerAvoidance(cur)
	if faced == 0 || av == 0 {
		t.Errorf("player should have an avoidance record (avoided=%d faced=%d)", av, faced)
	}
	// a session with no You data returns zero values, not a panic.
	other := &session.SessionManager{}
	other.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "Bob", Dmg: 1, Target: "a rat"})
	if st := playerStat(other.Current()); st.Total != 0 {
		t.Errorf("playerStat with no You row should be zero, got %d", st.Total)
	}
	if a, f := playerAvoidance(other.Current()); a != 0 || f != 0 {
		t.Errorf("playerAvoidance with no You row should be zero, got (%d,%d)", a, f)
	}
}

// TestDisciplineWidget: a warrior's discipline widget renders (default on), an
// activated discipline shows its live state, and the toggle hides it.
func TestDisciplineWidget(t *testing.T) {
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetClass(eqclass.ClassWarrior)
	tr.SetLevel(60)

	var m tea.Model = New(sampleManager(), tr, "Grok")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 40})
	mm := m.(Model)
	mm.refresh()
	if v := mm.vpClass.View(); !strings.Contains(v, "DISCIPLINES") || !strings.Contains(v, "Defensive") {
		t.Errorf("discipline widget should render for a warrior;\n%s", v)
	}

	// an activated discipline shows as active/cooling, not ready.
	tr.DisciplineUsed("Defensive", time.Now().Unix())
	mm.refresh()
	body, _ := mm.classPanel(nil, 40, "", true)
	if !strings.Contains(body, "Defensive") {
		t.Fatal("Defensive missing after activation")
	}

	// toggling the widget off hides it.
	mm.layoutPrefs.DiscHidden = true
	body2, _ := mm.classPanel(nil, 40, "", true)
	if strings.Contains(body2, "DISCIPLINES") {
		t.Error("DiscHidden should hide the discipline widget")
	}
}
