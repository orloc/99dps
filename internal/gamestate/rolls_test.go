package gamestate

import (
	"fmt"
	"testing"
)

func die(name string) string { return rollDiePrefix + name + "." }
func result(lo, hi, v int) string {
	return fmt.Sprintf("**It could have been any number from %d to %d, but this time it turned up a %d.", lo, hi, v)
}

// roll feeds the two /random lines for one roller at log time `at`.
func roll(tr *Tracker, name string, v int, at int64) {
	tr.Observe(die(name), at)
	tr.Observe(result(0, 1000, v), at)
}

// TestRolls_LiveContest: rolls within a contest accumulate into the live board,
// high value first (the leader on top), ties broken by who rolled first.
func TestRolls_LiveContest(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	if len(tr.Rolls()) != 0 {
		t.Fatal("no rolls initially")
	}
	roll(tr, "Trashromancer", 409, 100)
	roll(tr, "Emandar", 952, 101)
	roll(tr, "Daffadill", 112, 102)
	roll(tr, "Latecomer", 952, 103) // ties the 952 but rolled later

	live := tr.Rolls()
	if len(live) != 4 {
		t.Fatalf("want 4 live rolls, got %d", len(live))
	}
	if live[0].Roller != "Emandar" || live[0].Value != 952 {
		t.Errorf("leader = %+v, want Emandar 952", live[0])
	}
	if live[1].Roller != "Latecomer" {
		t.Errorf("tie should order by roll time, got %q", live[1].Roller)
	}
	if len(tr.RollWinners()) != 0 {
		t.Error("no contest has closed yet — winners log should be empty")
	}
}

// TestRolls_ContestClosesOnGap: a roll after a lull closes the prior contest —
// its high roller lands in the winners log (with the zone) and the board resets.
func TestRolls_ContestClosesOnGap(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.Observe("You have entered Sebilis.", 1000)

	roll(tr, "Airor", 700, 1000)
	roll(tr, "Belvinn", 903, 1001) // wins this contest
	// next roll arrives well after the gap → previous contest finalizes
	roll(tr, "Newguy", 50, 1001+contestGapSec+5)

	wins := tr.RollWinners()
	if len(wins) != 1 {
		t.Fatalf("a gap should close one contest, got %d winners", len(wins))
	}
	if wins[0].Roller != "Belvinn" || wins[0].Value != 903 {
		t.Errorf("winner = %+v, want Belvinn 903", wins[0])
	}
	if wins[0].Zone != "Sebilis" {
		t.Errorf("winner zone = %q, want Sebilis", wins[0].Zone)
	}
	// the new roll is the only one in the fresh live board
	if live := tr.Rolls(); len(live) != 1 || live[0].Roller != "Newguy" {
		t.Errorf("new contest should hold only Newguy, got %+v", live)
	}
}

// TestRolls_FinalizeStaleAndManual: a quiet contest closes on the tick-driven
// stale check; FinalizeRolls closes one immediately; ClearRollWinners empties the
// log; Clear (character switch) drops everything.
func TestRolls_FinalizeStaleAndManual(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	// stale finalize: rolls go quiet, the tick check closes the contest
	roll(tr, "A", 10, 100)
	roll(tr, "B", 20, 101)
	tr.FinalizeStaleRolls(101 + contestGapSec) // not yet past the gap
	if len(tr.RollWinners()) != 0 {
		t.Error("within the gap nothing should finalize")
	}
	tr.FinalizeStaleRolls(101 + contestGapSec + 1) // now past it
	if w := tr.RollWinners(); len(w) != 1 || w[0].Roller != "B" {
		t.Fatalf("stale finalize should record B, got %+v", w)
	}

	// manual finalize closes the current contest immediately
	roll(tr, "C", 5, 500)
	tr.FinalizeRolls()
	if w := tr.RollWinners(); len(w) != 2 || w[0].Roller != "C" {
		t.Errorf("manual finalize should record C first, got %+v", w)
	}
	if len(tr.Rolls()) != 0 {
		t.Error("finalize should reset the live board")
	}

	// clear the log; then a character switch drops all roll state
	tr.ClearRollWinners()
	if len(tr.RollWinners()) != 0 {
		t.Error("ClearRollWinners should empty the log")
	}
	roll(tr, "D", 1, 600)
	tr.Clear()
	if len(tr.Rolls()) != 0 || len(tr.RollWinners()) != 0 {
		t.Error("Clear should drop all roll state")
	}
}

// TestRolls_OrphanResult: a result line with no preceding die line is ignored.
func TestRolls_OrphanResult(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.Observe(result(0, 100, 50), 1)
	if len(tr.Rolls()) != 0 {
		t.Error("a result with no die line must not record a roll")
	}
}
