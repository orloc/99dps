package gamestate

import (
	"testing"

	"99dps/internal/eq/eqclass"
)

// tashaniBook builds a spell book containing a Tashani row whose cast_on_other
// emote matches the real client ("<mob> glances nervously about.").
func tashaniBook(t *testing.T) *Book {
	return loadBook(t, row(map[int]string{
		fName:        "Tashani",
		fCastOnOther: " glances nervously about.",
		fCastTime:    "0",
		fDurFormula:  "9",
		fDurCap:      "130",
		fGoodEffect:  "0", // detrimental
	}))
}

// activeTimerFor returns the active timer for (spell, target), if any.
func activeTimerFor(tr *Tracker, spell, target string, now int64) (Timer, bool) {
	for _, tm := range tr.Active(now) {
		if tm.Spell == spell && tm.Target == target {
			return tm, true
		}
	}
	return Timer{}, false
}

// A Journeyman's Walking Stick proc (Tashani) on a struck mob — no cast line —
// starts a detrimental debuff timer on that mob for a wielding class at level.
func TestWeaponProcLandsOnMob(t *testing.T) {
	tr := NewTracker(tashaniBook(t))
	tr.SetClass(eqclass.ClassMonk)
	tr.SetLevel(60)
	tr.Observe("a sand giant glances nervously about.", 1000)

	tm, ok := activeTimerFor(tr, "Tashani", "a sand giant", 1001)
	if !ok {
		t.Fatal("weapon proc should start a Tashani timer on the struck mob")
	}
	if tm.Target == "You" || !tm.Detrimental {
		t.Errorf("proc should be a detrimental debuff on the mob, got %+v", tm)
	}
	if tm.Expiry <= tm.Start {
		t.Errorf("proc timer should have a positive duration, got %+v", tm)
	}
}

// The proc is gated: a class that can't wield the stick must not get a phantom
// Tashani timer from the shared cast_on_other emote (e.g. a group-mate's own
// Tashani landing, which the log can't attribute to a caster).
func TestWeaponProcClassGated(t *testing.T) {
	tr := NewTracker(tashaniBook(t))
	tr.SetClass(eqclass.ClassWizard) // can't use the stick
	tr.SetLevel(60)
	tr.Observe("a sand giant glances nervously about.", 1000)
	if _, ok := activeTimerFor(tr, "Tashani", "a sand giant", 1001); ok {
		t.Error("a non-wielding class must not get a proc timer")
	}
}

// The proc only fires at/above its level (Tashani at 35); below that there's no
// proc, so the emote must not start a timer.
func TestWeaponProcLevelGated(t *testing.T) {
	tr := NewTracker(tashaniBook(t))
	tr.SetClass(eqclass.ClassMonk)
	tr.SetLevel(20) // below the level-35 proc
	tr.Observe("a sand giant glances nervously about.", 1000)
	if _, ok := activeTimerFor(tr, "Tashani", "a sand giant", 1001); ok {
		t.Error("the proc must not fire below its minimum level")
	}
}

// The proc timer clears on the caster-side worn-off line, like any debuff.
func TestWeaponProcClearsOnWornOff(t *testing.T) {
	tr := NewTracker(tashaniBook(t))
	tr.SetClass(eqclass.ClassMonk)
	tr.SetLevel(60)
	tr.Observe("a sand giant glances nervously about.", 1000)
	if _, ok := activeTimerFor(tr, "Tashani", "a sand giant", 1001); !ok {
		t.Fatal("setup: proc timer should exist")
	}
	tr.Observe("Your Tashani spell has worn off.", 1100)
	if _, ok := activeTimerFor(tr, "Tashani", "a sand giant", 1101); ok {
		t.Error("proc timer should clear on the worn-off line")
	}
}
