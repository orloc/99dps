package spell

import (
	"strings"
	"testing"
)

// row builds a 217-field spells_us.txt line with the given fields set.
func row(fields map[int]string) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	for i, v := range fields {
		f[i] = v
	}
	return strings.Join(f, "^")
}

func envenomedBolt() string {
	return row(map[int]string{
		fName:        "Envenomed Bolt",
		fCastOnOther: "'s body convulses with the poison.",
		fFades:       " has been cured of the poison.",
		fCastTime:    "6100",
		fDurFormula:  "1",
		fDurCap:      "7",
		fGoodEffect:  "0", // detrimental
	})
}

func loadBook(t *testing.T, rows ...string) *Book {
	t.Helper()
	b, err := LoadReader(strings.NewReader(strings.Join(rows, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeAndDuration(t *testing.T) {
	b := loadBook(t, envenomedBolt())
	s, ok := b.ByName("Envenomed Bolt")
	if !ok {
		t.Fatal("spell not loaded")
	}
	if s.CastTimeMs != 6100 || s.DurFormula != 1 || s.DurCap != 7 || !s.Detrimental {
		t.Fatalf("decoded wrong: %+v", s)
	}
	// formula 1: min(ceil(level/2), cap=7) ticks * 6s. At level 43: ceil(21.5)=22 -> capped 7 -> 42s.
	if got := s.DurationSeconds(43); got != 42 {
		t.Errorf("duration@43 = %d, want 42", got)
	}
	// at a low level the formula (not the cap) governs: level 8 -> ceil(4)=4 -> 24s.
	if got := s.DurationSeconds(8); got != 24 {
		t.Errorf("duration@8 = %d, want 24", got)
	}
}

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
	if act[0].Expiry != 1008+42 {
		t.Errorf("expiry = %d, want %d", act[0].Expiry, 1008+42)
	}

	// the mob dies -> debuff timer drops
	tr.Observe("a sand giant has been slain by Aragorn!", 1020)
	if act := tr.Active(1021); len(act) != 0 {
		t.Errorf("slain should clear timers, got %d", len(act))
	}
}

func TestNormEmote(t *testing.T) {
	cases := map[string]string{
		" has been poisoned..":               " has been poisoned.",
		"Your muscles erupt with strength..": "Your muscles erupt with strength.",
		" has been ensnared.":                " has been ensnared.", // already single
		" feels much faster":                 " feels much faster",  // no period
		"":                                   "",
	}
	for in, want := range cases {
		if got := NormEmote(in); got != want {
			t.Errorf("NormEmote(%q) = %q, want %q", in, got, want)
		}
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

// Duration formula spot-checks against known EQ values (Bedlam-style formula 8,
// Snare-style formula 2, Spirit-of-Wolf-style formula 3).
func TestDurationFormulas(t *testing.T) {
	f8 := &Spell{DurFormula: 8, DurCap: 75} // Bedlam
	if got := f8.DurationSeconds(60); got != 420 {
		t.Errorf("formula 8 @60 = %d, want 420 (min(70,75)*6)", got)
	}
	if got := f8.DurationSeconds(30); got != 240 {
		t.Errorf("formula 8 @30 = %d, want 240 (min(40,75)*6)", got)
	}
	f2 := &Spell{DurFormula: 2, DurCap: 39} // Snare
	if got := f2.DurationSeconds(50); got != 180 {
		t.Errorf("formula 2 @50 = %d, want 180 (min(ceil(30),39)*6)", got)
	}
	f3 := &Spell{DurFormula: 3, DurCap: 360} // Spirit of Wolf
	if got := f3.DurationSeconds(50); got != 2160 {
		t.Errorf("formula 3 @50 = %d, want 2160 (min(1500,360)*6)", got)
	}
	f0 := &Spell{DurFormula: 0} // nuke — no timer
	if got := f0.DurationSeconds(60); got != 0 {
		t.Errorf("formula 0 = %d, want 0", got)
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

	// no SetLevel yet → fallback level 60 → 70 ticks = 420s
	tr.BeginCast("Mind Buff", 1000)
	tr.Observe("You feel sharper.", 1004)
	if act := tr.Active(1010); len(act) != 1 || act[0].Expiry != 1004+420 {
		t.Fatalf("fallback expiry = %+v, want %d", act, 1004+420)
	}

	// /who reveals level 30 → recompute: min(40,75)=40 ticks = 240s
	tr.SetLevel(30)
	if act := tr.Active(1010); len(act) != 1 || act[0].Expiry != 1004+240 {
		t.Errorf("after SetLevel(30) expiry = %+v, want %d", act, 1004+240)
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

func TestTracker_ResistClearsPending(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.BeginCast("Envenomed Bolt", 1000)
	tr.Observe("Your target resisted the Envenomed Bolt spell.", 1007)
	// the landing emote arrives but the cast was already resisted away
	tr.Observe("a sand giant's body convulses with the poison.", 1008)
	if act := tr.Active(1010); len(act) != 0 {
		t.Errorf("resisted spell should not start a timer, got %d", len(act))
	}
}

func TestTracker_ExpiresOnTimeout(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.BeginCast("Envenomed Bolt", 1000)
	tr.Observe("a sand giant's body convulses with the poison.", 1008) // expiry 1050
	if act := tr.Active(1049); len(act) != 1 {
		t.Errorf("should still be active at 1049, got %d", len(act))
	}
	if act := tr.Active(1051); len(act) != 0 {
		t.Errorf("should be expired at 1051, got %d", len(act))
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
	if rem := act[0].Expiry - 1000; rem != 1080 { // formula 3 capped at 180 ticks = 18 min
		t.Errorf("duration = %d, want 1080", rem)
	}

	tr.Observe("Your feet slow down.", 1100)
	if len(tr.Active(1100)) != 0 {
		t.Error("the fade emote should clear the clicky timer")
	}
}

// When many spells share a clicky emote, indexing skips dev "Test" stubs and
// keeps the longest-duration (real) buff — so "Your feet leave the ground."
// resolves to a normal Levitation, not the 6-tick "Levitate Test".
func TestSelfClickyDisambiguation(t *testing.T) {
	book := loadBook(t,
		row(map[int]string{fName: "Levitate Test", fCastOnYou: "Your feet leave the ground.", fCastTime: "0", fDurFormula: "1", fDurCap: "6"}),
		row(map[int]string{fName: "Levitation", fCastOnYou: "Your feet leave the ground.", fCastTime: "0", fDurFormula: "3", fDurCap: "120"}),
	)
	s, ok := book.SelfClicky("Your feet leave the ground.")
	got := ""
	if s != nil {
		got = s.Name
	}
	if !ok || got != "Levitation" {
		t.Errorf("clicky should resolve to Levitation, got ok=%v name=%q", ok, got)
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

	tr.BreakMezOnTarget("a greater kobold") // damage line: article + lowercase
	if len(tr.Active(1001)) != 0 {
		t.Error("damage should break the mez despite the name-form mismatch")
	}
}
