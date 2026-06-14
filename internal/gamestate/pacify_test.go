package gamestate

import "testing"

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
