package gamestate

import (
	"testing"

	"99dps/internal/eq/eqclass"
)

// TestDisciplinesGatedByClass: a class with no disciplines (caster) returns nil;
// a melee class returns its list.
func TestDisciplinesGatedByClass(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetClass(eqclass.ClassWizard)
	if got := tr.Disciplines(100); got != nil {
		t.Errorf("a Wizard should have no disciplines, got %d", len(got))
	}
	tr.SetClass(eqclass.ClassWarrior)
	if got := tr.Disciplines(100); len(got) == 0 {
		t.Error("a Warrior should have disciplines")
	}
}

// TestDisciplineLevelGate: disciplines above the player's level are hidden once
// a level is known; with no level, all show.
func TestDisciplineLevelGate(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetClass(eqclass.ClassWarrior)
	all := len(tr.Disciplines(100)) // level unknown → everything

	tr.SetLevel(52)
	got := tr.Disciplines(100)
	if len(got) >= all {
		t.Errorf("L52 should hide high-level discs (%d) vs all (%d)", len(got), all)
	}
	for _, d := range got {
		if d.Level > 52 {
			t.Errorf("%s (L%d) shouldn't show at L52", d.Name, d.Level)
		}
	}
}

// TestDisciplinePhases: an activated discipline moves Active → Cooling → Ready as
// time passes, and the state persists (it isn't cleared on a zone change).
func TestDisciplinePhases(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetClass(eqclass.ClassWarrior)
	tr.SetLevel(60)

	// Defensive: 3 min duration, 10 min reuse.
	const dur, reuse = 180, 600
	tr.DisciplineUsed("Defensive", 1000)

	find := func(now int64) DisciplineTimer {
		for _, d := range tr.Disciplines(now) {
			if d.Name == "Defensive" {
				return d
			}
		}
		t.Fatal("Defensive not found")
		return DisciplineTimer{}
	}

	if d := find(1000 + 10); d.Phase != DiscActive || d.Remaining != dur-10 {
		t.Errorf("just-used should be Active with %ds left, got phase %d rem %d", dur-10, d.Phase, d.Remaining)
	}
	if d := find(1000 + dur + 5); d.Phase != DiscCooling {
		t.Errorf("past duration should be Cooling, got %d", d.Phase)
	}
	if d := find(1000 + reuse + 1); d.Phase != DiscReady {
		t.Errorf("past reuse should be Ready, got %d", d.Phase)
	}

	// zoning doesn't reset it — only a character switch (Clear) does.
	tr.Observe("You have entered Befallen.", 1100)
	if d := find(1100); d.Phase != DiscActive {
		t.Error("discipline state should survive a zone change")
	}
	// Clear (character switch) wipes both the class and the activation state.
	tr.Clear()
	if got := tr.Disciplines(1100); got != nil {
		t.Error("Clear should drop the class, leaving no disciplines")
	}
	tr.SetClass(eqclass.ClassWarrior)
	tr.SetLevel(60)
	if d := find(1100); d.Phase != DiscReady {
		t.Error("Clear should reset the activation, so Defensive is Ready again")
	}
}

// TestDisciplineUsedUnknown: activating a discipline the class doesn't have is a
// no-op (no panic, no phantom timer).
func TestDisciplineUsedUnknown(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetClass(eqclass.ClassWarrior)
	tr.DisciplineUsed("Hundred Fists", 1000) // a Monk disc
	for _, d := range tr.Disciplines(1001) {
		if d.Phase != DiscReady {
			t.Errorf("%s should be Ready (no cross-class activation)", d.Name)
		}
	}
}
