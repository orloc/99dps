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

// TestDisciplineSharedLockout: in classic EQ all disciplines share one reuse —
// using one locks out EVERY other until the used one's reuse expires.
func TestDisciplineSharedLockout(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetClass(eqclass.ClassWarrior)
	tr.SetLevel(60)

	// Defensive: 3 min duration, 10 min reuse. Evasive's own reuse is only 7 min.
	tr.DisciplineUsed("Defensive", 1000)
	find := func(name string, now int64) DisciplineTimer {
		for _, d := range tr.Disciplines(now) {
			if d.Name == name {
				return d
			}
		}
		t.Fatalf("%s not found", name)
		return DisciplineTimer{}
	}

	// while Defensive is active, EVERY other discipline is locked out — not Ready,
	// and counting down to Defensive's reuse (600s), not its own.
	ev := find("Evasive", 1100)
	if ev.Phase != DiscCooling {
		t.Fatalf("Evasive should be locked out while Defensive holds the timer, got phase %d", ev.Phase)
	}
	if ev.Remaining != 600-100 || ev.LockedBy != "Defensive" || ev.LockTotal != 600 {
		t.Errorf("Evasive lock = rem %d by %q total %d, want 500/Defensive/600", ev.Remaining, ev.LockedBy, ev.LockTotal)
	}
	if d := find("Defensive", 1100); d.Phase != DiscActive {
		t.Errorf("Defensive should be Active, got %d", d.Phase)
	}

	// the shared-lockout summary mirrors it.
	if name, rem, on := tr.DisciplineLock(1100); !on || name != "Defensive" || rem != 500 {
		t.Errorf("DisciplineLock = (%q,%d,%v), want (Defensive,500,true)", name, rem, on)
	}

	// once Defensive's reuse elapses, the whole bar frees up.
	if ev := find("Evasive", 1601); ev.Phase != DiscReady {
		t.Errorf("past the shared reuse Evasive should be Ready, got %d", ev.Phase)
	}
	if _, _, on := tr.DisciplineLock(1601); on {
		t.Error("no lockout should remain past the reuse")
	}

	// the most-recent activation governs: now Evasive holds the (shorter) lock.
	tr.DisciplineUsed("Evasive", 2000) // 7 min reuse
	if name, _, on := tr.DisciplineLock(2100); !on || name != "Evasive" {
		t.Errorf("the latest activation should hold the lock, got %q (%v)", name, on)
	}
	if d := find("Defensive", 2100); d.Phase != DiscCooling || d.LockedBy != "Evasive" {
		t.Errorf("Defensive should now be locked by Evasive, got phase %d by %q", d.Phase, d.LockedBy)
	}
}

// TestDisciplineActivationEmote: a discipline's self-emote in the log starts its
// timer AND reveals the class (the emote is class-exclusive), with no /who needed.
func TestDisciplineActivationEmote(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetLevel(60)

	// a Warrior Defensive emote: infers Warrior and arms the discipline
	tr.Observe("You assume a defensive fighting style.", 1000)
	if tr.Class() != eqclass.ClassWarrior {
		t.Fatalf("Defensive emote should infer Warrior, got %q", tr.Class())
	}
	find := func(now int64) DisciplineTimer {
		for _, d := range tr.Disciplines(now) {
			if d.Name == "Defensive" {
				return d
			}
		}
		t.Fatal("Defensive not found")
		return DisciplineTimer{}
	}
	if d := find(1010); d.Phase != DiscActive || d.Remaining != 180-10 {
		t.Errorf("just-activated Defensive should be Active ~170s, got phase %d rem %d", d.Phase, d.Remaining)
	}
}

// TestDisciplineActivationDoublePeriod: EQ writes some discipline emotes with a
// doubled trailing period ("...fighting style.."); NormEmote collapses it, so the
// stored single-period emote still matches.
func TestDisciplineActivationDoublePeriod(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetLevel(60)
	tr.Observe("You assume a defensive fighting style..", 1000) // doubled period, as logged
	if tr.Class() != eqclass.ClassWarrior {
		t.Fatalf("a doubled-period emote should still match, got class %q", tr.Class())
	}
}

// TestDisciplineActivationSpoof: a quoted tell/say containing a discipline emote
// must not arm a discipline or infer a class — only the bare first-person line
// (matched whole) is the player's own activation.
func TestDisciplineActivationSpoof(t *testing.T) {
	tr := NewTracker(&Book{})
	tr.SetLevel(60)
	tr.Observe("Bob tells you, 'You assume a defensive fighting style.'", 1000)
	tr.Observe("You say, 'You become untouchable.'", 1000)
	if tr.Class() != eqclass.ClassUnknown {
		t.Errorf("a quoted emote must not infer a class, got %q", tr.Class())
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
