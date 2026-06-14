package gamestate

import "testing"

// TestSelfClickyRegenExcludedBootsKept guards two things at once:
//   - a generic "buff from another caster" emote (group regen) is NOT treated as
//     a self-clicky, so a stray instant spell carrying it (the real spells_us
//     "Aura of Battle" #1499) can't mislabel every regen as Aura of Battle;
//   - Journeyman's Boots ("Your feet feel quick.") IS a self-clicky for any class
//     (the index is class-agnostic), so everyone's J-Boots are tracked.
func TestSelfClickyRegenExcludedBootsKept(t *testing.T) {
	b := loadBook(t,
		row(map[int]string{ // mislabeled instant buff whose emote is group regen
			fName: "Aura of Battle", fCastOnYou: "You begin to regenerate.",
			fCastOnOther: " begins to regenerate.", fFades: "You have stopped regenerating.",
			fCastTime: "0", fDurFormula: "10", fDurCap: "205", fGoodEffect: "1",
		}),
		row(map[int]string{ // Journeyman's Boots: instant self-clicky, usable by all
			fName: "JourneymanBoots", fCastOnYou: "Your feet feel quick.",
			fCastOnOther: " begins to walk faster.", fFades: "Your feet slow down.",
			fCastTime: "0", fDurFormula: "3", fDurCap: "180", fGoodEffect: "1",
		}),
	)

	if _, ok := b.SelfClicky("You begin to regenerate."); ok {
		t.Error("a group regen emote must not be a self-clicky")
	}
	if s, ok := b.SelfClicky("Your feet feel quick."); !ok || s.Name != "JourneymanBoots" {
		t.Errorf("J-Boots should be a self-clicky for any class, got ok=%v name=%v", ok, s)
	}

	// end-to-end on an unknown class: J-Boots tracked, regen ignored.
	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.Observe("You begin to regenerate.", 1000)
	tr.Observe("Your feet feel quick.", 1000)
	act := tr.Active(1001)
	if len(act) != 1 || act[0].Spell != "JourneymanBoots" {
		t.Errorf("expected only J-Boots tracked, got %+v", act)
	}
}

// TestSelfClickyExcludesNPCVariant: an instant NPC/GM copy of a real buff must not
// be indexed as a self-clicky — otherwise a cleric's Aegolism on you (the castable
// one has a cast time, so it's not a clicky) resolves to the instant "NPCAegolism".
func TestSelfClickyExcludesNPCVariant(t *testing.T) {
	const emote = "You are filled with the power of Aegolism."
	b := loadBook(t,
		// the castable buff: has a cast time, so it's never a self-clicky
		row(map[int]string{
			fName: "Aegolism", fCastOnYou: emote, fCastTime: "14000",
			fDurFormula: "3", fDurCap: "1500", fGoodEffect: "1",
		}),
		// the NPC variant: instant, would otherwise win the shared emote
		row(map[int]string{
			fName: "NPCAegolism", fCastOnYou: emote, fCastTime: "0",
			fDurFormula: "3", fDurCap: "1500", fGoodEffect: "1",
		}),
	)
	if s, ok := b.SelfClicky(emote); ok {
		t.Errorf("Aegolism cast on you must not resolve to a clicky (got %q)", s.Name)
	}

	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.Observe(emote, 1000) // a cleric's Aegolism lands on you
	if got := tr.Active(1001); len(got) != 0 {
		t.Errorf("an NPC buff variant must not be tracked as a self-buff, got %+v", got)
	}
}

// TestSelfClickyWhileCastPending: an insta-clicky must still be tracked when an
// unrelated cast is still pending (it just hasn't matched a landing). An
// enchanter is almost always mid-cast, so the old "skip clickies whenever a cast
// is pending" rule hid J-Boots entirely.
func TestSelfClickyWhileCastPending(t *testing.T) {
	b := loadBook(t,
		malo(), // a debuff to cast — leaves a lingering pending (its landing never comes)
		row(map[int]string{
			fName: "JourneymanBoots", fCastOnYou: "Your feet feel quick.",
			fCastTime: "0", fDurFormula: "3", fDurCap: "180", fGoodEffect: "1",
		}),
	)
	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.BeginCast("Malosini", 1000) // a cast that never lands (resisted) → pending lingers
	tr.Observe("Your feet feel quick.", 1002)

	found := false
	for _, tm := range tr.Active(1003) {
		if tm.Spell == "JourneymanBoots" {
			found = true
		}
	}
	if !found {
		t.Error("an insta-clicky should track even with a lingering pending cast")
	}
}
