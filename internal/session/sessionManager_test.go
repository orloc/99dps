package session

import (
	"sync"
	"testing"
	"time"

	"99dps/internal/eq/combat"
)

// --- Segmentation (sequential state-machine tests) ---------------------------
//
// These drive an ordered series of Apply/ApplyMagic/ApplySwing/ApplyEvent calls
// and assert session boundaries after each step. They are deliberately kept as
// sequential tests (not table-ized) because each assertion depends on the
// accumulated state of the calls before it.

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

// Hard boundaries: player death and zoning close the current fight so the next
// combat exchange starts a fresh session — while a mob kill does not.
func TestSegmentation_HardBoundaries(t *testing.T) {
	cases := []struct {
		name string
		ev   *combat.Event
	}{
		{"death", &combat.Event{ActionTime: 1002, Kind: combat.EventDeath}},
		{"zone", &combat.Event{ActionTime: 1002, Kind: combat.EventZone}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sm := &SessionManager{}
			sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 10, Target: "a rat"})
			sm.Apply(&combat.DamageSet{ActionTime: 1001, Dealer: "You", Dmg: 10, Target: "a rat"})
			sm.ApplyEvent(tc.ev) // boundary
			// next exchange immediately (well within the idle threshold) → new session
			sm.Apply(&combat.DamageSet{ActionTime: 1003, Dealer: "You", Dmg: 10, Target: "a rat"})
			if n := sm.Len(); n != 2 {
				t.Errorf("Len=%d, want 2 (boundary should split)", n)
			}
			if all := sm.All(); len(all) == 2 && all[0].EndTime().IsZero() {
				t.Error("first session should be marked ended")
			}
		})
	}

	// a kill must NOT split a multi-mob pull
	t.Run("kill does not split", func(t *testing.T) {
		sm := &SessionManager{}
		sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 10, Target: "a rat"})
		sm.ApplyEvent(&combat.Event{ActionTime: 1001, Kind: combat.EventKill})
		sm.Apply(&combat.DamageSet{ActionTime: 1002, Dealer: "You", Dmg: 10, Target: "another rat"})
		if n := sm.Len(); n != 1 {
			t.Errorf("kill split the pull: Len=%d, want 1", n)
		}
	})
}

// Bug #1: previously the first parsed line opened a fresh session and
// immediately rolled into a second one, because the threshold check compared
// set.ActionTime against an uninitialized LastTime == 0 — the unix-epoch gap
// trivially exceeded the threshold.
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

// A second hit within the threshold stays in the same session; a hit past it on
// a DIFFERENT enemy opens a new one (the re-engage rule only bridges a lull when
// you return to an enemy the fight already involved).
func TestApply_ThresholdRollsSessions(t *testing.T) {
	sm := &SessionManager{}

	sm.Apply(&combat.DamageSet{ActionTime: 1_700_000_000, Dealer: "You", Dmg: 1, Target: "a rat"})
	sm.Apply(&combat.DamageSet{ActionTime: 1_700_000_005, Dealer: "You", Dmg: 1, Target: "a rat"}) // +5s, same session
	if got := len(sm.sessions); got != 1 {
		t.Fatalf("within-threshold hit opened a new session: got %d sessions", got)
	}

	sm.Apply(&combat.DamageSet{ActionTime: 1_700_000_100, Dealer: "You", Dmg: 1, Target: "a bat"})
	if got := len(sm.sessions); got != 2 {
		t.Fatalf("past-threshold hit on a new enemy didn't roll: got %d sessions", got)
	}
}

// ApplyMagic: enemy non-melee opens a session, but an incoming non-melee line on
// the player (a DoT ticking on YOU) must not, on its own, open a fight.
// Previously ApplyMagic opened a session before excluding the on-you damage,
// leaving an empty "Solo" session whenever the first thing seen was an enemy
// spell landing on the player. Matching is case-insensitive.
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

// TestMobStatsFromApply: the mob-hit index captures every mob's melee on anyone
// (pet, groupmate), excludes the player's own hits, and resets on Clear.
func TestMobStatsFromApply(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "a sand giant", Dmg: 40, Target: "Pet"})
	sm.Apply(&combat.DamageSet{ActionTime: 101, Dealer: "a sand giant", Dmg: 60, Target: "Groupmate"})
	sm.Apply(&combat.DamageSet{ActionTime: 102, Dealer: "You", Dmg: 200, Target: "a sand giant"})

	mi := sm.MobStats()
	if avg, ok := mi.AvgHit("a sand giant", 102); !ok || avg != 50 {
		t.Errorf("mob avg = %d ok=%v, want 50 (hits on pet+groupmate)", avg, ok)
	}
	if _, ok := mi.AvgHit("You", 102); ok {
		t.Error("the player's own hits must not be in the mob index")
	}

	sm.Clear()
	if _, ok := sm.MobStats().AvgHit("a sand giant", 102); ok {
		t.Error("Clear should reset the mob index")
	}
}

// Clear drops every session and resets the selection so the next exchange starts
// fresh from a single session.
func TestClear(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 1_700_000_000, "You", 10, "a rat")
	hit(sm, 1_700_001_000, "You", 10, "a bat") // far apart → a second session
	if sm.Len() < 2 {
		t.Fatalf("setup: want >=2 sessions, got %d", sm.Len())
	}

	sm.Clear()
	if sm.Len() != 0 {
		t.Errorf("Len after Clear = %d, want 0", sm.Len())
	}
	if sm.Current() != nil {
		t.Error("Current after Clear should be nil")
	}
	if sm.activeSession != 0 {
		t.Errorf("activeSession after Clear = %d, want 0", sm.activeSession)
	}

	// the next hit opens exactly one session at index 0
	hit(sm, 1_700_002_000, "You", 10, "a rat")
	if sm.Len() != 1 {
		t.Errorf("Len after post-Clear hit = %d, want 1", sm.Len())
	}
}

// --- Concurrency (race) tests ------------------------------------------------
//
// Kept as sequential goroutine-driven tests (not table-ized): they assert the
// snapshot/locking contract and must run under -race.

// Regression for bug #3: Apply takes the write lock for the duration of the
// session mutation, so concurrent Apply / All / Current must be race-free
// under -race.
func TestApply_RaceFreeWithReaders(t *testing.T) {
	sm := &SessionManager{}

	// Seed a session so readers have something to look at.
	sm.Apply(&combat.DamageSet{ActionTime: 1, Dealer: "Foo", Dmg: 1})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: simulates the parser goroutine. Periodically crosses
	// the threshold so a new session is appended.
	go func() {
		defer wg.Done()
		tick := int64(2)
		for {
			select {
			case <-stop:
				return
			default:
				sm.Apply(&combat.DamageSet{
					ActionTime: tick,
					Dealer:     "Foo",
					Dmg:        1,
				})
				tick += int64(segGapCeil * 2) // guarantees a roll each tick
			}
		}
	}()

	// Reader: simulates the UI Sync goroutine.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = sm.All()
				_ = sm.Current()
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// Regression for bug #3 variant: Current() now returns a deep snapshot, so a
// reader can iterate its aggressors map while the writer keeps mutating the
// live session. Previously this panicked with "concurrent map iteration and
// map write" even without -race.
func TestCurrent_SnapshotIndependentOfLiveSession(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1, Dealer: "Foo", Dmg: 1})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				sm.Apply(&combat.DamageSet{
					ActionTime: int64(i + 2),
					Dealer:     "d" + string(rune('a'+(i%26))),
					Dmg:        1,
				})
				i++
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				snap := sm.Current()
				if snap == nil {
					continue
				}
				for k := range snap.aggressors {
					_ = k
				}
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
