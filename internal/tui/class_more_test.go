package tui

import (
	"strings"
	"testing"

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
