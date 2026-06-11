package gamestate

import (
	"testing"
)

// malo is an instant-cast debuff with a clear landing emote and fade line, so
// tests can drive same-named mobs without cast-completion timing.
func malo() string {
	return row(map[int]string{
		fName:        "Malosini",
		fCastOnOther: "'s magic resistances have been lowered.",
		fFades:       " feels less resistant.",
		fCastTime:    "0",
		fDurFormula:  "1",
		fDurCap:      "50",
		fGoodEffect:  "0", // detrimental
	})
}

const maloLand = "a sand giant's magic resistances have been lowered."

func castMalo(tr *Tracker, at int64) {
	tr.BeginCast("Malosini", at)
	tr.Observe(maloLand, at)
}

// TestTimer_SecondSameNamedMobIsNewInstance: debuffing a same-named mob while the
// first copy is still fresh (not red) yields two timers, not one overwrite.
func TestTimer_SecondSameNamedMobIsNewInstance(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	castMalo(tr, 1001) // first copy still fresh → a different mob
	if act := tr.Active(1002); len(act) != 2 {
		t.Fatalf("two same-named mobs should hold two timers, got %d", len(act))
	}
}

// TestTimer_RefreshWhenStale: re-casting while the existing copy is stale (red,
// ≤ StaleFrac left) refreshes it in place — one timer, later expiry.
func TestTimer_RefreshWhenStale(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	first := tr.Active(1000)[0]
	dur := first.Expiry - first.Start

	late := first.Start + dur - 1 // 1s left → well inside the red zone
	castMalo(tr, late)
	act := tr.Active(late)
	if len(act) != 1 {
		t.Fatalf("a re-cast on a stale (red) copy should refresh, not add; got %d", len(act))
	}
	if act[0].Expiry <= first.Expiry {
		t.Errorf("refresh should extend the expiry: was %d, now %d", first.Expiry, act[0].Expiry)
	}
}

// TestTimer_FadeClearsOneInstance is the fall-off-then-re-apply case: with two
// same-named mobs each debuffed, one fading clears only its own timer (not both),
// and a fresh cast re-adds the instance.
func TestTimer_FadeClearsOneInstance(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	castMalo(tr, 1001)
	if act := tr.Active(1002); len(act) != 2 {
		t.Fatalf("setup: expected 2 timers, got %d", len(act))
	}

	tr.Observe("a sand giant feels less resistant.", 1003) // one mob's debuff fades
	if act := tr.Active(1004); len(act) != 1 {
		t.Fatalf("a fade should clear ONE instance, leaving the other; got %d", len(act))
	}

	castMalo(tr, 1005) // re-apply to that mob
	if act := tr.Active(1006); len(act) != 2 {
		t.Fatalf("re-casting after a fade should re-add the instance; got %d", len(act))
	}
}

// TestTimer_SlainClearsOneInstance: killing one of two same-named mobs drops one
// debuff timer, not every same-named copy.
func TestTimer_SlainClearsOneInstance(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	castMalo(tr, 1001)
	tr.Observe("You have slain a sand giant!", 1003)
	if act := tr.Active(1004); len(act) != 1 {
		t.Fatalf("slaying one same-named mob should clear one instance, got %d", len(act))
	}
}
