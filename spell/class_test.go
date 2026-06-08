package spell

import (
	"testing"

	"99dps/common"
)

func TestTrackerClass(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	// defaults to caster (spell timers) until a /who is seen
	if tr.Category() != common.CatCaster {
		t.Errorf("default category = %d, want CatCaster", tr.Category())
	}

	tr.SetClass(common.ClassWarrior)
	if tr.Class() != common.ClassWarrior || tr.Category() != common.CatMelee {
		t.Errorf("after SetClass(Warrior): class=%q cat=%d", tr.Class(), tr.Category())
	}

	// an unknown class is ignored, not overwriting a known one
	tr.SetClass(common.ClassUnknown)
	if tr.Class() != common.ClassWarrior {
		t.Errorf("SetClass(Unknown) overwrote class: %q", tr.Class())
	}

	// a character switch clears the class back to unknown
	tr.Clear()
	if tr.Class() != common.ClassUnknown {
		t.Errorf("after Clear: class=%q, want unknown", tr.Class())
	}

	// nil tracker is safe and defaults to caster
	var nilTr *Tracker
	if nilTr.Category() != common.CatCaster {
		t.Error("nil tracker Category should be CatCaster")
	}
}
