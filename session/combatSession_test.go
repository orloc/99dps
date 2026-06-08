package session

import (
	"testing"
	"time"

	"99dps/common"
)

// hit is a tiny helper to push a melee damage line through the manager.
func hit(sm *SessionManager, at int64, dealer string, dmg int, target string) {
	sm.Apply(&common.DamageSet{ActionTime: at, Dealer: dealer, Dmg: dmg, Target: target})
}

// Name is the heaviest enemy *target*, not the heaviest dealer — so even when
// the player deals all the damage, the fight is named after what's being hit.
func TestName_HeaviestTargetNotDealer(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "You", 500, "a frost giant")
	hit(sm, 101, "You", 10, "a bat")

	if got := sm.Current().Name(); got != "a frost giant" {
		t.Errorf("Name = %q, want %q", got, "a frost giant")
	}
}

// When no enemy was struck (the player only took damage), Name falls back to the
// heaviest non-player dealer — with the underscore key restored to spaces.
func TestName_FallsBackToTopDealerWithSpaces(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "a frost giant", 50, "YOU")

	if got := sm.Current().Name(); got != "a frost giant" {
		t.Errorf("Name = %q, want %q", got, "a frost giant")
	}
}

func TestName_EmptyIsSolo(t *testing.T) {
	if got := (&CombatSession{}).Name(); got != "Solo" {
		t.Errorf("Name = %q, want Solo", got)
	}
}

// TopDealer ranks every dealer (player included) and restores the spaced name.
func TestTopDealer(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "a frost giant", 800, "YOU")
	hit(sm, 100, "You", 200, "a frost giant")

	name, pct := sm.Current().TopDealer()
	if name != "a frost giant" || pct != 80 { // 800 / 1000
		t.Errorf("TopDealer = (%q, %d), want (a frost giant, 80)", name, pct)
	}

	if name, pct := (&CombatSession{}).TopDealer(); name != "" || pct != 0 {
		t.Errorf("empty TopDealer = (%q, %d), want (\"\", 0)", name, pct)
	}
}

// Duration spans first to last hit; a zero/negative span clamps to 0.
func TestDuration(t *testing.T) {
	cs := &CombatSession{start: time.Unix(100, 0), LastTime: 130}
	if d := cs.Duration(); d != 30*time.Second {
		t.Errorf("Duration = %v, want 30s", d)
	}

	if d := (&CombatSession{}).Duration(); d != 0 {
		t.Errorf("unstarted Duration = %v, want 0", d)
	}
	// out-of-order timestamps must not yield a negative duration
	neg := &CombatSession{start: time.Unix(200, 0), LastTime: 100}
	if d := neg.Duration(); d != 0 {
		t.Errorf("negative-span Duration = %v, want 0", d)
	}
}

// Total sums every dealer; the per-dealer aggregates (Hits/FirstTime/LastTime)
// must reflect every hit folded in.
func TestTotalAndAggregates(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "You", 10, "a rat")
	hit(sm, 105, "You", 30, "a rat")
	hit(sm, 106, "a rat", 5, "YOU")

	cur := sm.Current()
	if cur.Total() != 45 {
		t.Errorf("Total = %d, want 45", cur.Total())
	}

	var you common.DamageStat
	for _, s := range cur.GetAggressors() {
		if s.Dealer == "You" {
			you = s
		}
	}
	if you.Hits != 2 || you.Total != 40 || you.FirstTime != 100 || you.LastTime != 105 {
		t.Errorf("You aggregate = %+v, want hits2 total40 first100 last105", you)
	}

	if (&CombatSession{}).Total() != 0 {
		t.Error("empty Total != 0")
	}
}

// Player skill tracking buckets only the player's activated attacks (Backstab/
// Bash/Kick) by canonical name; auto-attacks and other dealers' skills are
// excluded.
func TestPlayerSkillsTracking(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&common.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 150, Target: "a rat", Verb: "backstabs"})
	sm.Apply(&common.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 40, Target: "a rat", Verb: "kick"})
	sm.Apply(&common.DamageSet{ActionTime: 102, Dealer: "You", Dmg: 20, Target: "a rat", Verb: "slash"}) // auto-attack
	sm.Apply(&common.DamageSet{ActionTime: 103, Dealer: "Bob", Dmg: 99, Target: "a rat", Verb: "kick"})  // groupmate

	skills := sm.Current().Skills()
	if len(skills) != 2 {
		t.Fatalf("skills = %v, want Backstab + Kick only", skills)
	}
	if skills["Backstab"].Total != 150 || skills["Backstab"].Hits != 1 {
		t.Errorf("Backstab = %+v, want 150/1", skills["Backstab"])
	}
	if skills["Kick"].Total != 40 || skills["Kick"].Hits != 1 {
		t.Errorf("Kick = %+v, want 40/1 (groupmate's kick must be excluded)", skills["Kick"])
	}
}

// A snapshot must be independent of the live session: mutating the original
// after snapshotting must not change the copy (regression guard for the
// CombatRecords→aggregates refactor, where maps are now cloned).
func TestSnapshotIsIndependent(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "You", 10, "a rat")

	snap := sm.Current()
	hit(sm, 101, "You", 90, "a rat") // mutate live state after the snapshot

	if snap.Total() != 10 {
		t.Errorf("snapshot Total = %d, want 10 (must not see later hits)", snap.Total())
	}
}
