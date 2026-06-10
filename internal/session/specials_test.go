package session

import (
	"testing"

	"99dps/internal/combat"
)

// TestSpecialsBreakdown checks the per-dealer, per-kind special tally: landed
// damage/hits from damage lines, misses from swings, hit rate, and that a
// snapshot doesn't alias the live inner maps.
func TestSpecialsBreakdown(t *testing.T) {
	sm := &SessionManager{}
	// You: two backstabs land, one misses; one kick lands.
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 300, Target: "a rat", Verb: "backstab"})
	sm.Apply(&combat.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 500, Target: "a rat", Verb: "backstabs"})
	sm.ApplySwing(&combat.Swing{ActionTime: 102, Attacker: "You", Defender: "a rat", Verb: "backstab", Outcome: combat.OutcomeMiss})
	sm.Apply(&combat.DamageSet{ActionTime: 103, Dealer: "You", Dmg: 90, Target: "a rat", Verb: "kicks"})
	// a dodge of a kick must NOT count against the kick's hit rate
	sm.ApplySwing(&combat.Swing{ActionTime: 104, Attacker: "You", Defender: "a rat", Verb: "kick", Outcome: combat.OutcomeDodge})

	snap := sm.Current()
	sp := snap.SpecialsFor("You")
	bs := sp["Backstab"]
	if bs.Total != 800 || bs.Hits != 2 || bs.Misses != 1 {
		t.Errorf("backstab = %+v, want {Total:800 Hits:2 Misses:1}", bs)
	}
	if bs.HitRate() != 66 { // 2/3
		t.Errorf("backstab hit rate = %d, want 66", bs.HitRate())
	}
	if k := sp["Kick"]; k.Total != 90 || k.Hits != 1 || k.Misses != 0 || k.HitRate() != 100 {
		t.Errorf("kick = %+v (hr %d), want {90 1 0} 100%%", k, k.HitRate())
	}

	// snapshot independence: mutating the live session must not touch the snapshot
	sm.Apply(&combat.DamageSet{ActionTime: 110, Dealer: "You", Dmg: 1000, Target: "a rat", Verb: "backstab"})
	if sp["Backstab"].Total != 800 {
		t.Error("snapshot aliased the live specials map")
	}
}
