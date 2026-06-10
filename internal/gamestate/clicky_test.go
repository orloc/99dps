package gamestate

import (
	"testing"

	"99dps/internal/eqclass"
)

// withClickyRegistry swaps in an isolated registry for the test and restores it.
func withClickyRegistry(t *testing.T, entries map[eqclass.Class][]Clicky) {
	t.Helper()
	saved := clickyRegistry
	clickyRegistry = entries
	t.Cleanup(func() { clickyRegistry = saved })
}

// TestClicky_BookDuration: a registered clicky whose Effect is a real spell
// times for the book duration at the player's level.
func TestClicky_BookDuration(t *testing.T) {
	withClickyRegistry(t, map[eqclass.Class][]Clicky{})
	RegisterClicky(eqclass.ClassRanger, Clicky{
		Item: "Test Boots", Effect: "Envenomed Bolt", Message: "You feel a test buff.",
	})

	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43) // Envenomed Bolt @43 → 48s (see TestDecodeAndDuration)
	tr.Observe("You feel a test buff.", 1000)

	act := tr.Active(1001)
	if len(act) != 1 || act[0].Spell != "Envenomed Bolt" || act[0].Target != "You" {
		t.Fatalf("clicky should start a self timer, got %+v", act)
	}
	if act[0].Expiry != 1000+48 {
		t.Errorf("expiry = %d, want %d (book duration)", act[0].Expiry, 1048)
	}
}

// TestClicky_SecondsFallback: a clicky whose Effect isn't a timed spell uses the
// explicit Seconds fallback.
func TestClicky_SecondsFallback(t *testing.T) {
	withClickyRegistry(t, map[eqclass.Class][]Clicky{})
	RegisterClicky(eqclass.ClassShadowKnight, Clicky{
		Item: "Custom Item", Effect: "Custom Haste", Message: "The item surges.", Seconds: 90,
	})

	tr := NewTracker(loadBook(t, envenomedBolt())) // book lacks "Custom Haste"
	tr.SetLevel(50)
	tr.Observe("The item surges.", 2000)

	act := tr.Active(2001)
	if len(act) != 1 || act[0].Spell != "Custom Haste" {
		t.Fatalf("fallback clicky should time, got %+v", act)
	}
	if act[0].Expiry != 2000+90 {
		t.Errorf("expiry = %d, want %d (Seconds fallback)", act[0].Expiry, 2090)
	}
}

// TestClicky_NoMatch: an unrelated line starts nothing.
func TestClicky_NoMatch(t *testing.T) {
	withClickyRegistry(t, map[eqclass.Class][]Clicky{})
	RegisterClicky(eqclass.ClassRanger, Clicky{Effect: "Envenomed Bolt", Message: "You feel a test buff."})
	tr := NewTracker(loadBook(t, envenomedBolt()))
	tr.SetLevel(43)
	tr.Observe("a sand giant hits YOU for 50 points of damage.", 1000)
	if act := tr.Active(1001); len(act) != 0 {
		t.Errorf("unrelated line should not start a clicky timer, got %d", len(act))
	}
}
