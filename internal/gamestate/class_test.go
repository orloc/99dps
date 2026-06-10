package gamestate

import (
	"99dps/internal/eqclass"
	"testing"
)

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
