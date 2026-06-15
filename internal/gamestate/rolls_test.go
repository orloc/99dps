package gamestate

import (
	"fmt"
	"testing"
)

func die(name string) string { return rollDiePrefix + name + "." }
func result(lo, hi, v int) string {
	return fmt.Sprintf("**It could have been any number from %d to %d, but this time it turned up a %d.", lo, hi, v)
}

// TestRolls: the two-line /random output pairs into rolls, sorted high-first
// (the winner on top), with ties broken by who rolled first.
func TestRolls(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	if len(tr.Rolls()) != 0 {
		t.Fatal("no rolls initially")
	}

	feed := func(name string, v, at int64) {
		tr.Observe(die(name), at)
		tr.Observe(result(0, 1000, int(v)), at)
	}
	feed("Trashromancer", 409, 100)
	feed("Emandar", 952, 101)
	feed("Daffadill", 112, 102)
	feed("Latecomer", 952, 103) // ties Emandar's 952 but rolled later

	rolls := tr.Rolls()
	if len(rolls) != 4 {
		t.Fatalf("want 4 rolls, got %d", len(rolls))
	}
	// winner first; the earlier of the tied 952s wins the tiebreak
	if rolls[0].Roller != "Emandar" || rolls[0].Value != 952 {
		t.Errorf("winner = %+v, want Emandar 952", rolls[0])
	}
	if rolls[1].Roller != "Latecomer" {
		t.Errorf("tie should order by roll time, got %q second", rolls[1].Roller)
	}
	if rolls[3].Roller != "Daffadill" || rolls[3].Value != 112 {
		t.Errorf("last = %+v, want Daffadill 112", rolls[3])
	}
	if rolls[0].Low != 0 || rolls[0].High != 1000 {
		t.Errorf("range not captured: %+v", rolls[0])
	}
}

// TestRolls_Edges: a result with no preceding die line is ignored; ClearRolls and
// Clear both reset the board.
func TestRolls_Edges(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	tr.Observe(result(0, 100, 50), 1) // orphan result — no die line
	if len(tr.Rolls()) != 0 {
		t.Error("a result with no die line must not record a roll")
	}

	tr.Observe(die("Solo"), 2)
	tr.Observe(result(0, 100, 77), 2)
	if len(tr.Rolls()) != 1 {
		t.Fatal("a paired roll should record")
	}
	tr.ClearRolls()
	if len(tr.Rolls()) != 0 {
		t.Error("ClearRolls should empty the board")
	}

	tr.Observe(die("Again"), 3)
	tr.Observe(result(0, 100, 5), 3)
	tr.Clear() // a character switch
	if len(tr.Rolls()) != 0 {
		t.Error("Clear should also drop rolls")
	}
}
