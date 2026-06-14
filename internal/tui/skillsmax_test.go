package tui

import (
	"strings"
	"testing"

	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/session"
)

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
