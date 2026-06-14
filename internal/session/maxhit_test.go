package session

import (
	"testing"

	"99dps/internal/combat"
)

// TestMaxHitTracked: the session records the biggest single melee hit per dealer
// and per skill (a bounded running max, not the individual hits).
func TestMaxHitTracked(t *testing.T) {
	sm := &SessionManager{}
	apply := func(dmg int, verb string) {
		sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: dmg, Target: "a rat", Verb: verb})
	}
	apply(50, "crush") // auto-attack
	apply(120, "kick") // skill
	apply(80, "kick")
	apply(200, "crush") // biggest melee overall

	cur := sm.Current()
	var you combat.DamageStat
	for _, s := range cur.GetAggressors() {
		if s.Dealer == "You" {
			you = s
		}
	}
	if you.Max != 200 {
		t.Errorf("overall melee max = %d, want 200", you.Max)
	}
	if k := cur.Skills()["Kick"]; k.Max != 120 {
		t.Errorf("Kick skill max = %d, want 120", k.Max)
	}
}
