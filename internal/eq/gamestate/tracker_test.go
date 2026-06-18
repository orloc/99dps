package gamestate

import (
	"testing"

	"99dps/internal/eq/eqclass"
)

func TestTracker_LandThenExpireOnSlain(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)

	tr.BeginCast("Envenomed Bolt", 1000)
	// landing emote 8s later (cast completes at ~6.1s); prefix is the target
	tr.Observe("a sand giant's body convulses with the poison.", 1008)

	act := tr.Active(1010)
	if len(act) != 1 {
		t.Fatalf("active timers = %d, want 1", len(act))
	}
	if act[0].Spell != "Envenomed Bolt" || act[0].Target != "a sand giant" {
		t.Errorf("timer = %+v", act[0])
	}
	if act[0].Expiry != 1008+48 {
		t.Errorf("expiry = %d, want %d", act[0].Expiry, 1008+48)
	}

	// the mob dies -> debuff timer drops
	tr.Observe("a sand giant has been slain by Aragorn!", 1020)
	if act := tr.Active(1021); len(act) != 0 {
		t.Errorf("slain should clear timers, got %d", len(act))
	}
}

// A spell whose data has a doubled trailing period must still match the
// single-period log line (and vice versa).
func TestTracker_DoubledPeriodEmoteMatches(t *testing.T) {
	// data stores " has been poisoned.." (doubled)
	rowDoubled := row(map[int]string{
		fName:        "Poison Bolt",
		fCastOnOther: " has been poisoned..",
		fCastTime:    "3000",
		fDurFormula:  "1",
		fDurCap:      "10",
		fGoodEffect:  "0",
	})
	tr := NewTracker(loadBook(t, rowDoubled))
	tr.SetLevel(50)

	tr.BeginCast("Poison Bolt", 1000)
	// the log renders a single period
	tr.Observe("a kobold has been poisoned.", 1004)

	act := tr.Active(1010)
	if len(act) != 1 || act[0].Target != "a kobold" {
		t.Fatalf("doubled-period emote failed to match: %+v", act)
	}
}

// A /who that reveals the level after a timer was started at the fallback level
// recomputes the running timer's expiry.
func TestTracker_SetLevelRecomputesTimers(t *testing.T) {
	tr := NewTracker(loadBook(t, row(map[int]string{
		fName:       "Mind Buff",
		fCastOnYou:  "You feel sharper.",
		fCastTime:   "3000",
		fDurFormula: "8", // min(level+10, cap)
		fDurCap:     "75",
		fGoodEffect: "1",
	})))

	// no SetLevel yet → fallback level 60 → min(70,75)=70 +1 cast-tick = 71 ticks = 426s
	tr.BeginCast("Mind Buff", 1000)
	tr.Observe("You feel sharper.", 1004)
	if act := tr.Active(1010); len(act) != 1 || act[0].Expiry != 1004+426 {
		t.Fatalf("fallback expiry = %+v, want %d", act, 1004+426)
	}

	// /who reveals level 30 → recompute: min(40,75)=40 +1 = 41 ticks = 246s
	tr.SetLevel(30)
	if act := tr.Active(1010); len(act) != 1 || act[0].Expiry != 1004+246 {
		t.Errorf("after SetLevel(30) expiry = %+v, want %d", act, 1004+246)
	}
}

// Real bug: EQ capitalizes the leading mob name in landing emotes ("Skeletal
// duke's feet...") but not in death lines ("skeletal duke has been slain by..."),
// so the slain match must be case-insensitive or the debuff timer never clears.
func TestTracker_SlainClearsAcrossCaseMismatch(t *testing.T) {
	tr := NewTracker(loadBook(t, row(map[int]string{
		fName:        "Enstill",
		fCastOnOther: "'s feet adhere to the ground.",
		fCastTime:    "2000",
		fDurFormula:  "2",
		fDurCap:      "39",
		fGoodEffect:  "0",
	})))
	tr.SetLevel(50)

	tr.BeginCast("Enstill", 1000)
	// emote capitalizes the leading name → timer target "Skeletal duke"
	tr.Observe("Skeletal duke's feet adhere to the ground.", 1003)
	if act := tr.Active(1010); len(act) != 1 || act[0].Target != "Skeletal duke" {
		t.Fatalf("timer not created: %+v", act)
	}

	// death line is lowercase — must still clear it
	tr.Observe("skeletal duke has been slain by skeletal champion!", 1015)
	if act := tr.Active(1016); len(act) != 0 {
		t.Errorf("death (case-mismatched) should clear timer, got %+v", act)
	}
}

// Charm spells have no landing emote, so the timer starts on cast completion,
// counts up (Charm=true), and is cleared by "Your charm spell has worn off."
func TestTracker_Charm(t *testing.T) {
	tr := NewTracker(loadBook(t, row(map[int]string{
		fName:       "Boltran`s Agacerie",
		fFades:      "You are no longer charmed.", // the charm marker (no cast_on_other)
		fCastTime:   "4000",
		fDurFormula: "10",
		fDurCap:     "70",
		fGoodEffect: "0",
	})))
	tr.SetLevel(60)

	// confirm the book flagged it as a charm
	if s, _ := tr.book.ByName("Boltran`s Agacerie"); !s.Charm {
		t.Fatal("spell not detected as a charm")
	}

	tr.BeginCast("Boltran`s Agacerie", 1000)
	// some line arrives after the 4s cast completes → charm timer starts (no emote)
	tr.Observe("a sand giant hits a kobold for 30 points of damage.", 1005)

	act := tr.Active(1015)
	if len(act) != 1 || !act[0].Charm || act[0].Target != "Charm" {
		t.Fatalf("charm not started: %+v", act)
	}

	// the break message clears it (well before the formula max)
	tr.Observe("Your charm spell has worn off.", 1050)
	if act := tr.Active(1051); len(act) != 0 {
		t.Errorf("worn-off should clear charm, got %+v", act)
	}
}

// CharmBroke surfaces a charm break for a grace window, then stops.
func TestCharmBroke(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	// inject an active charm timer (charm has no landing emote to script here)
	tr.timers = map[string]Timer{
		key("Allure", "Charm"): {Spell: "Allure", Target: "Charm", Charm: true, Start: 1000, Expiry: 9000},
	}

	if tr.CharmBroke(1100) {
		t.Fatal("no break yet")
	}
	tr.Observe("Your charm spell has worn off.", 1100)

	if !tr.CharmBroke(1100) {
		t.Error("charm break should be surfaced right after it happens")
	}
	if len(tr.Active(1100)) != 0 {
		t.Error("the charm timer should be cleared on break")
	}
	if tr.CharmBroke(1100 + charmBreakGraceSec + 1) {
		t.Error("charm break should stop surfacing after the grace window")
	}
}

func TestTracker_ResistRecorded(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.BeginCast("Envenomed Bolt", 1000)
	tr.Observe("Your target resisted the Envenomed Bolt spell.", 1007)
	// a resist surfaces briefly and, with no landing emote, starts no timer
	if sp, ok := tr.Resisted(1008); !ok || sp != "Envenomed Bolt" {
		t.Errorf("Resisted = %q,%v; want \"Envenomed Bolt\",true", sp, ok)
	}
	if act := tr.Active(1010); len(act) != 0 {
		t.Errorf("a resisted cast with no landing emote should have no timer, got %d", len(act))
	}
	if _, ok := tr.Resisted(1007 + resistGraceSec + 1); ok {
		t.Error("resist notice should expire after the grace window")
	}
}

// TestTracker_EarlyWearOff: "Your <Spell> spell has worn off." clears the timer
// before its max duration — an early break (root/mez shaken off, debuff dropped).
func TestTracker_EarlyWearOff(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.BeginCast("Envenomed Bolt", 1000)
	tr.Observe("a sand giant's body convulses with the poison.", 1008) // expiry 1056
	if len(tr.Active(1010)) != 1 {
		t.Fatal("timer should be active")
	}
	tr.Observe("Your Envenomed Bolt spell has worn off.", 1020) // breaks at 1020 << 1056
	if act := tr.Active(1021); len(act) != 0 {
		t.Errorf("early wear-off should clear the timer, got %d", len(act))
	}
}

// TestTracker_EarlyWearOff_OneInstance: with the same spell up on two mobs, one
// wear-off line clears exactly one instance (the line names no target).
func TestTracker_EarlyWearOff_OneInstance(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.BeginCast("Envenomed Bolt", 1000)
	tr.Observe("a sand giant's body convulses with the poison.", 1008)
	tr.Observe("a cliff golem's body convulses with the poison.", 1008)
	if len(tr.Active(1010)) != 2 {
		t.Fatal("two instances expected")
	}
	tr.Observe("Your Envenomed Bolt spell has worn off.", 1020)
	if act := tr.Active(1021); len(act) != 1 {
		t.Errorf("one wear-off should clear exactly one instance, got %d", len(act))
	}
}

// TestTracker_AoEMultipleTargets: one cast whose landing emote appears for
// several mobs (an AoE/PBAoE) starts a timer on each.
func TestTracker_AoEMultipleTargets(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.BeginCast("Envenomed Bolt", 1000)
	tr.Observe("a sand giant's body convulses with the poison.", 1008)
	tr.Observe("a cliff golem's body convulses with the poison.", 1008)
	act := tr.Active(1010)
	if len(act) != 2 {
		t.Fatalf("AoE should time each affected mob, got %d", len(act))
	}
	got := map[string]bool{}
	for _, a := range act {
		got[a.Target] = true
	}
	if !got["a sand giant"] || !got["a cliff golem"] {
		t.Errorf("missing an AoE target: %+v", act)
	}
}

func TestTracker_ExpiresOnTimeout(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.BeginCast("Envenomed Bolt", 1000)
	tr.Observe("a sand giant's body convulses with the poison.", 1008) // 8 ticks → expiry 1056
	if act := tr.Active(1055); len(act) != 1 {
		t.Errorf("should still be active at 1055, got %d", len(act))
	}
	if act := tr.Active(1057); len(act) != 0 {
		t.Errorf("should be expired at 1057, got %d", len(act))
	}
}

// An instant clicky self-buff (Journeyman Boots) emits no cast line, so its
// landing emote is matched directly to start the timer.
func TestSelfClickyTimer(t *testing.T) {
	jboots := &Spell{
		Name: "JourneymanBoots", CastTimeMs: 0, DurFormula: 3, DurCap: 180,
		CastOnYou: "Your feet feel quick.", Fades: "Your feet slow down.",
	}
	book := &Book{
		byName:  map[string]*Spell{jboots.Name: jboots},
		byEmote: map[string]*Spell{jboots.CastOnYou: jboots},
	}
	tr := NewTracker(book)

	tr.Observe("Your feet feel quick.", 1000)
	act := tr.Active(1000)
	if len(act) != 1 || act[0].Spell != "JourneymanBoots" || act[0].Target != "You" {
		t.Fatalf("clicky should start a self timer, got %+v", act)
	}
	if rem := act[0].Expiry - 1000; rem != 1086 { // formula 3 capped at 180 +1 cast-tick = 181 ticks
		t.Errorf("duration = %d, want 1086", rem)
	}

	tr.Observe("Your feet slow down.", 1100)
	if len(tr.Active(1100)) != 0 {
		t.Error("the fade emote should clear the clicky timer")
	}
}

// Canni dance: replays a real spam sequence (casts at 36/39/41/43 with a buzzer
// at 42) and checks the efficiency/buzzer/active readout.
func TestCanniDance(t *testing.T) {
	book := loadBook(t, row(map[int]string{
		fName: "Cannibalize III", fCastTime: "1250", fRecastTime: "2250",
	}))
	tr := NewTracker(book)

	tr.BeginCast("Cannibalize III", 36)
	tr.BeginCast("Cannibalize III", 39)
	tr.BeginCast("Cannibalize III", 41)
	tr.Observe("Spell recast time not yet met.", 42) // too early — the buzzer
	tr.BeginCast("Cannibalize III", 43)

	c := tr.CanniStats(43)
	if !c.Active || c.Rank != "Cannibalize III" {
		t.Fatalf("expected active Cannibalize III dance, got %+v", c)
	}
	if c.Buzzers != 1 {
		t.Errorf("buzzers = %d, want 1", c.Buzzers)
	}
	// throughput ~96% (3 intervals × 2.25s over 7s) × accuracy 80% (4 casts of
	// 5 presses; 1 buzzer) ≈ 76% — the buzzer drags it down
	if c.Pct != 76 {
		t.Errorf("efficiency = %d%%, want 76%% (buzzer must hurt it)", c.Pct)
	}

	// stops dancing → inactive after the timeout
	if tr.CanniStats(43 + canniDanceTimeoutSec + 1).Active {
		t.Error("dance should go inactive after the timeout")
	}
}

// DismissTarget removes only the clicked person's timers.
func TestDismissTarget(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.timers[key("Clarity", "Tank")] = Timer{Spell: "Clarity", Target: "Tank", Expiry: 9999}
	tr.timers[key("Brell", "Tank")] = Timer{Spell: "Brell", Target: "Tank", Expiry: 9999}
	tr.timers[key("SoW", "You")] = Timer{Spell: "SoW", Target: "You", Expiry: 9999}

	tr.DismissTarget("Tank")
	act := tr.Active(0)
	if len(act) != 1 || act[0].Target != "You" {
		t.Errorf("dismiss should remove only Tank's buffs, left %+v", act)
	}
}

// Mez lands as a debuff timer flagged Mez, and damage to the mob breaks it even
// though the mez emote name ("Greater kobold") differs from the damage-line name
// ("a greater kobold").
func TestMezTrackingAndBreak(t *testing.T) {
	tr := NewTracker(loadBook(t, row(map[int]string{
		fName: "Mesmerize", fCastOnOther: " has been mesmerized.",
		fCastTime: "1000", fDurFormula: "7", fDurCap: "4",
	})))
	tr.SetLevel(60)

	tr.BeginCast("Mesmerize", 1000)
	tr.Observe("Greater kobold has been mesmerized.", 1001) // emote: no article, capitalized
	act := tr.Active(1001)
	if len(act) != 1 || !act[0].Mez || act[0].Target != "Greater kobold" {
		t.Fatalf("mez timer not created: %+v", act)
	}

	tr.BreakCCOnTarget("a greater kobold") // damage line: article + lowercase
	if len(tr.Active(1001)) != 0 {
		t.Error("damage should break the mez despite the name-form mismatch")
	}
}

// --- same-named-mob multi-instance behavior ---

// TestTimer_SecondSameNamedMobIsNewInstance: debuffing a same-named mob while the
// first copy is still fresh (not red) yields two timers, not one overwrite.
func TestTimer_SecondSameNamedMobIsNewInstance(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	castMalo(tr, 1001) // first copy still fresh → a different mob
	if act := tr.Active(1002); len(act) != 2 {
		t.Fatalf("two same-named mobs should hold two timers, got %d", len(act))
	}
}

// TestTimer_RefreshWhenStale: re-casting while the existing copy is stale (red,
// ≤ StaleFrac left) refreshes it in place — one timer, later expiry.
func TestTimer_RefreshWhenStale(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	first := tr.Active(1000)[0]
	dur := first.Expiry - first.Start

	late := first.Start + dur - 1 // 1s left → well inside the red zone
	castMalo(tr, late)
	act := tr.Active(late)
	if len(act) != 1 {
		t.Fatalf("a re-cast on a stale (red) copy should refresh, not add; got %d", len(act))
	}
	if act[0].Expiry <= first.Expiry {
		t.Errorf("refresh should extend the expiry: was %d, now %d", first.Expiry, act[0].Expiry)
	}
}

// TestTimer_FadeClearsOneInstance is the fall-off-then-re-apply case: with two
// same-named mobs each debuffed, one fading clears only its own timer (not both),
// and a fresh cast re-adds the instance.
func TestTimer_FadeClearsOneInstance(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	castMalo(tr, 1001)
	if act := tr.Active(1002); len(act) != 2 {
		t.Fatalf("setup: expected 2 timers, got %d", len(act))
	}

	tr.Observe("a sand giant feels less resistant.", 1003) // one mob's debuff fades
	if act := tr.Active(1004); len(act) != 1 {
		t.Fatalf("a fade should clear ONE instance, leaving the other; got %d", len(act))
	}

	castMalo(tr, 1005) // re-apply to that mob
	if act := tr.Active(1006); len(act) != 2 {
		t.Fatalf("re-casting after a fade should re-add the instance; got %d", len(act))
	}
}

// TestTimer_SlainClearsAllOnMob: a mob's death ends ALL debuffs on it. The death
// line names only the mob (not which same-named instance), and a single-target
// caster stacks several instances on one mob — all die with it. (A *fade* line,
// by contrast, clears just one instance: see TestTimer_FadeClearsOneInstance.)
func TestTimer_SlainClearsAllOnMob(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	castMalo(tr, 1001) // two instances on "a sand giant"
	tr.Observe("You have slain a sand giant!", 1003)
	if act := tr.Active(1004); len(act) != 0 {
		t.Fatalf("slaying the mob should clear all its debuffs, got %d", len(act))
	}
}

// TestTimer_BuffRecastAlwaysRefreshes: a buff lands on a unique target (you),
// so re-casting it — even while it's still fresh (green) — refreshes the one
// timer rather than spawning a duplicate. Only detrimental spells on same-named
// mobs split into instances.
func TestTimer_BuffRecastAlwaysRefreshes(t *testing.T) {
	book := loadBook(t, row(map[int]string{
		fName: "Aegolism", fCastOnYou: "You feel the strength of the gods.",
		fCastTime: "0", fDurFormula: "5", fDurCap: "600", fGoodEffect: "1", // beneficial
	}))
	tr := NewTracker(book)
	tr.SetLevel(60)
	tr.BeginCast("Aegolism", 1000)
	tr.Observe("You feel the strength of the gods.", 1000)
	tr.BeginCast("Aegolism", 1005) // re-cast while still fresh
	tr.Observe("You feel the strength of the gods.", 1005)

	if act := tr.Active(1006); len(act) != 1 {
		t.Fatalf("re-casting a buff on yourself should refresh one timer, not duplicate; got %d", len(act))
	}
}

// --- class detection / category ---

func TestTrackerClass(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	// defaults to caster (spell timers) until a /who is seen
	if tr.Category() != eqclass.CatCaster {
		t.Errorf("default category = %d, want CatCaster", tr.Category())
	}

	tr.SetClass(eqclass.ClassWarrior)
	if tr.Class() != eqclass.ClassWarrior || tr.Category() != eqclass.CatMelee {
		t.Errorf("after SetClass(Warrior): class=%q cat=%d", tr.Class(), tr.Category())
	}

	// an unknown class is ignored, not overwriting a known one
	tr.SetClass(eqclass.ClassUnknown)
	if tr.Class() != eqclass.ClassWarrior {
		t.Errorf("SetClass(Unknown) overwrote class: %q", tr.Class())
	}

	// a character switch clears the class back to unknown
	tr.Clear()
	if tr.Class() != eqclass.ClassUnknown {
		t.Errorf("after Clear: class=%q, want unknown", tr.Class())
	}

	// nil tracker is safe and defaults to caster
	var nilTr *Tracker
	if nilTr.Category() != eqclass.CatCaster {
		t.Error("nil tracker Category should be CatCaster")
	}
}

// TestTracker_SpellCountAndLevel: the trivial getters reflect the loaded book
// size and the level set via a /who, and degrade to zero on a nil tracker / book.
func TestTracker_SpellCountAndLevel(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	if got := tr.SpellCount(); got != 1 {
		t.Errorf("SpellCount = %d, want 1", got)
	}
	if got := tr.Level(); got != 0 {
		t.Errorf("Level before /who = %d, want 0", got)
	}
	tr.SetLevel(57)
	if got := tr.Level(); got != 57 {
		t.Errorf("Level = %d, want 57", got)
	}

	var nilTr *Tracker
	if nilTr.SpellCount() != 0 || nilTr.Level() != 0 {
		t.Error("nil tracker getters should be zero, not panic")
	}
	if NewTracker(nil).SpellCount() != 0 {
		t.Error("SpellCount with a nil book should be 0")
	}
}

// --- pacify (CC) ---

// pacify spells all land with "<mob> looks less aggressive." and use formula 8.
func calmSpell() string { // P99 Calm: cap 30 → 180s
	return row(map[int]string{fName: "Calm", fCastOnOther: " looks less aggressive.", fCastTime: "2500", fDurFormula: "8", fDurCap: "30", fGoodEffect: "0"})
}
func wakeSpell() string { // Wake of Tranquility: uncapped (cap 0) → ~7 min at 60
	return row(map[int]string{fName: "Wake of Tranquility", fCastOnOther: " looks less aggressive.", fCastTime: "4500", fDurFormula: "8", fDurCap: "0", fGoodEffect: "0"})
}

// TestDismissTargetKeepsSelfCC: an AoE pacify/mez (Wake of Tranquility) can catch
// the player, landing on "You". Dismissing your buffs must not clear it — it's a
// detrimental effect on you, not one of your buffs.
func TestDismissTargetKeepsSelfCC(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.timers[key("Wake of Tranquility", "You")] = Timer{Spell: "Wake of Tranquility", Target: "You", Pacify: true, Detrimental: true, Expiry: 9999}
	tr.timers[key("Clarity", "You")] = Timer{Spell: "Clarity", Target: "You", Expiry: 9999}

	tr.DismissTarget("You")

	wake, clar := false, false
	for _, tm := range tr.Active(0) {
		switch tm.Spell {
		case "Wake of Tranquility":
			wake = true
		case "Clarity":
			clar = true
		}
	}
	if !wake {
		t.Error("an AoE pacify that caught you must survive a buff dismiss")
	}
	if clar {
		t.Error("dismissing your buffs should still clear the buff")
	}
}

// TestPacify_Durations matches the P99 wiki: Calm 180s (30 ticks), Wake of
// Tranquility ~7 min (uncapped). No +1 cast-tick, unlike a buff.
func TestPacify_Durations(t *testing.T) {
	b := loadBook(t, calmSpell(), wakeSpell())
	calm, _ := b.ByName("Calm")
	if !calm.Pacify {
		t.Error("Calm should be flagged Pacify")
	}
	if got := calm.PacifyDurationSeconds(60); got != 180 {
		t.Errorf("Calm @60 = %d, want 180", got)
	}
	wake, _ := b.ByName("Wake of Tranquility")
	if got := wake.PacifyDurationSeconds(60); got != 420 {
		t.Errorf("Wake of Tranquility @60 = %d, want 420 (7 min)", got)
	}
}

// TestPacify_TracksAndBreaks: a pacify cast lands as a CC timer with the P99
// duration, and breaks when the mob takes damage (it re-aggros).
func TestPacify_TracksAndBreaks(t *testing.T) {
	tr := NewTracker(loadBook(t, calmSpell()))
	tr.SetLevel(60)
	tr.BeginCast("Calm", 1000)
	tr.Observe("a sand giant looks less aggressive.", 1003)

	act := tr.Active(1005)
	if len(act) != 1 || !act[0].Pacify || act[0].Target != "a sand giant" {
		t.Fatalf("pacify should land as a CC timer: %+v", act)
	}
	if act[0].Expiry != 1003+180 {
		t.Errorf("expiry = %d, want %d (P99 180s, not the +1-tick 186)", act[0].Expiry, 1003+180)
	}

	tr.BreakCCOnTarget("a sand giant") // damage wakes it
	if act := tr.Active(1006); len(act) != 0 {
		t.Errorf("damage should break pacify, got %d", len(act))
	}
}

// TestSetLevel_KeepsPacifyDuration: a level update (e.g. a /who revealing the
// real level) must recompute a live pacify timer with the cap-based
// PacifyDurationSeconds, NOT the buff formula — which adds a cast-tick and would
// stretch Calm from 180s to 186s. Regression for the SetLevel clobber.
func TestSetLevel_KeepsPacifyDuration(t *testing.T) {
	tr := NewTracker(loadBook(t, calmSpell()))
	tr.SetLevel(60)
	tr.BeginCast("Calm", 1000)
	tr.Observe("a sand giant looks less aggressive.", 1003)

	calmDur := func() int64 {
		t.Helper()
		for _, tm := range tr.Active(1005) {
			if tm.Spell == "Calm" {
				return tm.Expiry - tm.Start
			}
		}
		t.Fatal("Calm timer not found")
		return 0
	}
	if d := calmDur(); d != 180 {
		t.Fatalf("Calm on landing = %ds, want 180", d)
	}
	tr.SetLevel(59) // a later /who reveals a different level
	if d := calmDur(); d != 180 {
		t.Errorf("Calm after level update = %ds, want 180 (must not be clobbered to 186)", d)
	}
}

// TestPacify_SameNamedMobsInstance: pacify lands on mobs (which can be same-named),
// so a second cast while the first is still up gets its OWN timer — even though
// P99 flags pacify "beneficial" (the old code therefore overwrote it). A re-cast
// within reapplyWindowSec of expiry refreshes instead of adding a third.
func TestPacify_SameNamedMobsInstance(t *testing.T) {
	tr := NewTracker(loadBook(t, calmSpell()))
	tr.SetLevel(60)
	land := func(at int64) { // calm lands ~3s after the cast begins, 180s duration
		tr.BeginCast("Calm", at)
		tr.Observe("a sand giant looks less aggressive.", at+3)
	}
	land(1000) // lands 1003, expiry 1183
	land(1010) // second same-named mob; first still has ~170s left → a new instance
	if act := tr.Active(1020); len(act) != 2 {
		t.Fatalf("two pacified same-named mobs should hold two timers, got %d", len(act))
	}
	// re-applying near the soonest expiry (1183) refreshes that copy, not a third.
	land(1167) // lands 1170; soonest expiry 1183 → 13s left (< 30) → refresh
	if act := tr.Active(1171); len(act) != 2 {
		t.Errorf("re-applying within 30s of expiry should refresh, not add a third; got %d", len(act))
	}
}
