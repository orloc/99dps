package session

import (
	"testing"

	"99dps/internal/combat"
)

// Swing attribution: an attacker's accuracy (offense) counts only hits and
// misses; the defender's dodge/parry/block must not bleed into the
// attacker's hit ratio. The defender, conversely, faces every outcome.
func TestApplySwing_OffenseExcludesDefensiveOutcomes(t *testing.T) {
	sm := &SessionManager{}

	// start the fight with a landed hit (also a connecting swing)
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 10, Target: "a rat"})

	at := func(o combat.SwingOutcome) {
		sm.ApplySwing(&combat.Swing{ActionTime: 100, Attacker: "You", Defender: "a rat", Outcome: o})
	}
	at(combat.OutcomeMiss)
	at(combat.OutcomeDodge)
	at(combat.OutcomeParry)
	at(combat.OutcomeBlock)
	at(combat.OutcomeRiposte)

	cur := sm.Current()

	off := cur.OffenseFor("You")
	if off.Hits != 1 || off.Misses != 1 {
		t.Fatalf("offense hits/misses = %d/%d, want 1/1", off.Hits, off.Misses)
	}
	if off.Dodges != 0 || off.Parries != 0 || off.Blocks != 0 || off.Ripostes != 0 {
		t.Errorf("offense leaked defensive outcomes: %+v", off)
	}
	if off.Attempts() != 2 || off.HitRate() != 50 {
		t.Errorf("attempts/hitrate = %d/%d, want 2/50", off.Attempts(), off.HitRate())
	}

	var rat combat.SwingStats
	for _, d := range cur.Defense() {
		if d.Name == "a rat" {
			rat = d.Stats
		}
	}
	if rat.Hits != 1 || rat.Misses != 1 || rat.Dodges != 1 || rat.Parries != 1 || rat.Blocks != 1 || rat.Ripostes != 1 {
		t.Errorf("rat defense = %+v, want each of hit/miss/dodge/parry/block/riposte == 1", rat)
	}
	if rat.Swings() != 6 || rat.Avoided() != 5 {
		t.Errorf("rat faced/avoided = %d/%d, want 6/5", rat.Swings(), rat.Avoided())
	}
}

// Adaptive cadence segmentation: a tight pull (with a mid-fight kill) stays one
// session; swing activity keeps a fight alive through a melee-damage gap that
// the old fixed 8s timeout would have split; and a long silence rolls a new
// session.
func TestSegmentation_AdaptiveCadence(t *testing.T) {
	sm := &SessionManager{}

	// fast pull: damage every 1s, plus a kill mid-way → one encounter
	for ts := int64(1000); ts < 1010; ts++ {
		sm.Apply(&combat.DamageSet{ActionTime: ts, Dealer: "You", Dmg: 10, Target: "a rat"})
	}
	sm.ApplyEvent(&combat.Event{ActionTime: 1005, Kind: combat.EventKill}) // punctuation, not a boundary
	if sm.Len() != 1 {
		t.Fatalf("fast pull split into %d sessions, want 1", sm.Len())
	}

	// a 15s stretch with no melee damage but steady misses (every 4s) — the
	// swing activity keeps it one fight, though the damage-to-damage gap alone
	// would exceed the floor and split it.
	for ts := int64(1013); ts <= 1024; ts += 4 { // 1013,1017,1021 — gaps of 4s
		sm.ApplySwing(&combat.Swing{ActionTime: ts, Attacker: "a rat", Defender: "YOU", Outcome: combat.OutcomeMiss})
	}
	sm.Apply(&combat.DamageSet{ActionTime: 1024, Dealer: "You", Dmg: 10, Target: "a rat"})
	if sm.Len() != 1 {
		t.Fatalf("swing activity should hold one session, got %d", sm.Len())
	}

	// the rat was killed (EventKill above) → the pull is resolved, so a silence past
	// the ceiling rolls a new encounter
	sm.Apply(&combat.DamageSet{ActionTime: 1024 + segGapCeil + 5, Dealer: "You", Dmg: 10, Target: "a bat"})
	if sm.Len() != 2 {
		t.Fatalf("silence after the mob's dead should roll a session, got %d", sm.Len())
	}
}

// TestSegmentation_LiveMobBridgesLull: while the engaged mob is still ALIVE (no
// kill credited), a long root/med lull keeps it one session as long as you return
// to that mob — but a different mob, or a lull past the ceiling, rolls.
func TestSegmentation_LiveMobBridgesLull(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 10, Target: "a drolvarg snarler"})
	sm.ApplyMagic(&combat.Magic{ActionTime: 1005, Dmg: 50, Target: "a drolvarg growler"})

	// ~2-min lull (root + med), no kill yet → the snarler swings at you → one fight
	sm.Apply(&combat.DamageSet{ActionTime: 1005 + 120, Dealer: "a drolvarg snarler", Dmg: 50, Target: "YOU"})
	if got := sm.Len(); got != 1 {
		t.Fatalf("a still-alive mob after a lull should stay one session; got %d", got)
	}

	// a different (un-engaged) mob after a lull → new session
	sm.Apply(&combat.DamageSet{ActionTime: 1005 + 240, Dealer: "You", Dmg: 10, Target: "a bat"})
	if got := sm.Len(); got != 2 {
		t.Fatalf("a different mob after a lull should roll; got %d", got)
	}

	// past the live ceiling, even the same alive mob rolls (it leashed / you left)
	sm2 := &SessionManager{}
	sm2.Apply(&combat.DamageSet{ActionTime: 2000, Dealer: "You", Dmg: 10, Target: "a drolvarg snarler"})
	sm2.Apply(&combat.DamageSet{ActionTime: 2000 + segLiveCeil + 5, Dealer: "You", Dmg: 10, Target: "a drolvarg snarler"})
	if got := sm2.Len(); got != 2 {
		t.Fatalf("past the live ceiling the same mob still rolls; got %d", got)
	}
}

// TestSegmentation_KillResolvesPull: once the mob is dead (xp credited), the next
// pull — even the same-named mob after a lull — is a NEW session. A pull is
// contiguous only while its mob is up.
func TestSegmentation_KillResolvesPull(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 10, Target: "a rat"})
	sm.ApplyEvent(&combat.Event{ActionTime: 1001, Kind: combat.EventKill, Name: "a rat"})
	sm.ApplyEvent(&combat.Event{ActionTime: 1001, Kind: combat.EventXP}) // dead + credited

	// a lull, then a fresh same-named rat → the prior pull is over → new session
	sm.Apply(&combat.DamageSet{ActionTime: 1001 + 90, Dealer: "You", Dmg: 10, Target: "a rat"})
	if got := sm.Len(); got != 2 {
		t.Fatalf("a new pull after the mob died should roll, even same-named; got %d", got)
	}
}

// Hard boundaries: player death, zoning, and camping close the current fight so
// the next combat exchange starts a fresh session — while a mob kill does not.
func TestSegmentation_HardBoundaries(t *testing.T) {
	boundary := func(name string, ev *combat.Event) {
		sm := &SessionManager{}
		sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 10, Target: "a rat"})
		sm.Apply(&combat.DamageSet{ActionTime: 1001, Dealer: "You", Dmg: 10, Target: "a rat"})
		sm.ApplyEvent(ev) // boundary
		// next exchange immediately (well within the idle threshold) → new session
		sm.Apply(&combat.DamageSet{ActionTime: 1003, Dealer: "You", Dmg: 10, Target: "a rat"})
		if n := sm.Len(); n != 2 {
			t.Errorf("%s: Len=%d, want 2 (boundary should split)", name, n)
		}
		if all := sm.All(); len(all) == 2 && all[0].EndTime().IsZero() {
			t.Errorf("%s: first session should be marked ended", name)
		}
	}
	boundary("death", &combat.Event{ActionTime: 1002, Kind: combat.EventDeath})
	boundary("zone", &combat.Event{ActionTime: 1002, Kind: combat.EventZone})

	// a kill must NOT split a multi-mob pull
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 10, Target: "a rat"})
	sm.ApplyEvent(&combat.Event{ActionTime: 1001, Kind: combat.EventKill})
	sm.Apply(&combat.DamageSet{ActionTime: 1002, Dealer: "You", Dmg: 10, Target: "another rat"})
	if n := sm.Len(); n != 1 {
		t.Errorf("kill split the pull: Len=%d, want 1", n)
	}
}

// Crits, special-attack damage, and kill/xp/death counters attach to the
// active session without rolling it.
func TestApply_CritsSpecialsAndEvents(t *testing.T) {
	sm := &SessionManager{}

	// auto-attack hit + a backstab special, both by You
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 50, Target: "a rat", Verb: "slash"})
	sm.Apply(&combat.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 150, Target: "a rat", Verb: "backstab"})

	sm.ApplyCrit(&combat.Crit{ActionTime: 101, Attacker: "You", Damage: 150})
	sm.ApplyEvent(&combat.Event{ActionTime: 102, Kind: combat.EventKill})
	sm.ApplyEvent(&combat.Event{ActionTime: 102, Kind: combat.EventXP})
	sm.ApplyEvent(&combat.Event{ActionTime: 102, Kind: combat.EventDeath})

	if n := sm.Len(); n != 1 {
		t.Fatalf("non-damage events rolled a session: Len=%d, want 1", n)
	}

	cur := sm.Current()

	if c := cur.CritFor("You"); c.Count != 1 || c.Damage != 150 {
		t.Errorf("crit tally = %+v, want count1 dmg150", c)
	}
	if cur.Kills() != 1 || cur.XpGains() != 1 || cur.Deaths() != 1 {
		t.Errorf("counters kills/xp/deaths = %d/%d/%d, want 1/1/1", cur.Kills(), cur.XpGains(), cur.Deaths())
	}

	var you combat.DamageStat
	for _, s := range cur.GetAggressors() {
		if s.Dealer == "You" {
			you = s
		}
	}
	if you.Total != 200 {
		t.Errorf("total = %d, want 200", you.Total)
	}
	if you.SpecialTotal != 150 || you.SpecialHits != 1 {
		t.Errorf("special total/hits = %d/%d, want 150/1", you.SpecialTotal, you.SpecialHits)
	}
}

// Non-melee damage on enemies feeds the unattributed magic total (folded into
// the encounter total); incoming spell damage on the player does not.
func TestApplyMagic_UnattributedTotal(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "a rat", Dmg: 5, Target: "YOU", Verb: "bite"})

	sm.ApplyMagic(&combat.Magic{ActionTime: 103, Target: "a rat", Dmg: 200})
	sm.ApplyMagic(&combat.Magic{ActionTime: 104, Target: "a rat", Dmg: 50})
	sm.ApplyMagic(&combat.Magic{ActionTime: 105, Target: "YOU", Dmg: 999}) // incoming — excluded

	if n := sm.Len(); n != 1 {
		t.Fatalf("magic events rolled a session: Len=%d, want 1", n)
	}
	if got := sm.Current().MagicTotal(); got != 250 {
		t.Errorf("magic total = %d, want 250 (incoming-on-you excluded)", got)
	}
}

// Bug #3: an incoming non-melee line on the player (e.g. a DoT ticking on YOU)
// must not, on its own, open a fight. Previously ApplyMagic opened a session
// before excluding the on-you damage, leaving an empty "Solo" session whenever
// the first thing seen was an enemy spell landing on the player.
func TestApplyMagic_OnYouOpensNoSession(t *testing.T) {
	sm := &SessionManager{}
	sm.ApplyMagic(&combat.Magic{ActionTime: 100, Target: "YOU", Dmg: 200})
	sm.ApplyMagic(&combat.Magic{ActionTime: 101, Target: "you", Dmg: 50}) // case-insensitive
	if n := sm.Len(); n != 0 {
		t.Fatalf("incoming-on-you magic opened a session: Len=%d, want 0", n)
	}

	// a real enemy target still opens one
	sm.ApplyMagic(&combat.Magic{ActionTime: 102, Target: "a rat", Dmg: 30})
	if n := sm.Len(); n != 1 {
		t.Fatalf("enemy magic should open a session: Len=%d, want 1", n)
	}
}

// Bug #1: previously the first parsed line opened a fresh session and
// immediately rolled into a second one, because the threshold check compared
// set.ActionTime against an uninitialized LastTime == 0 — the unix-epoch gap
// trivially exceeded CS_THRESHOLD.
//
// Expected behavior: after a single Apply, there is exactly one session and
// it contains the dealer.
func TestApply_FirstHitDoesNotRollSession(t *testing.T) {
	sm := &SessionManager{}

	set := &combat.DamageSet{
		ActionTime: 1_700_000_000,
		Dealer:     "Foo",
		Dmg:        42,
		Target:     "a_giant_rat",
	}
	sm.Apply(set)

	if got, want := len(sm.sessions), 1; got != want {
		t.Fatalf("expected %d session after first hit, got %d", want, got)
	}

	all := sm.All()
	if len(all) != 1 {
		t.Fatalf("All() returned %d sessions, want 1", len(all))
	}
	if _, ok := all[0].aggressors["Foo"]; !ok {
		t.Errorf("session is missing dealer Foo; aggressors=%v", all[0].aggressors)
	}
}

// A second hit within CS_THRESHOLD stays in the same session; a hit past it
// opens a new one.
func TestApply_ThresholdRollsSessions(t *testing.T) {
	sm := &SessionManager{}

	sm.Apply(&combat.DamageSet{ActionTime: 1_700_000_000, Dealer: "You", Dmg: 1, Target: "a rat"})
	sm.Apply(&combat.DamageSet{ActionTime: 1_700_000_005, Dealer: "You", Dmg: 1, Target: "a rat"}) // +5s, same session
	if got := len(sm.sessions); got != 1 {
		t.Fatalf("within-threshold hit opened a new session: got %d sessions", got)
	}

	// +95s on a DIFFERENT enemy → a new session (the re-engage rule only bridges a
	// lull when you return to an enemy the fight already involved).
	sm.Apply(&combat.DamageSet{ActionTime: 1_700_000_100, Dealer: "You", Dmg: 1, Target: "a bat"})
	if got := len(sm.sessions); got != 2 {
		t.Fatalf("past-threshold hit on a new enemy didn't roll: got %d sessions", got)
	}
}
