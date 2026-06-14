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

// TestTimer_SlainClearsAllOnMob: a mob's death ends ALL debuffs on it. The death
// line names only the mob (not which same-named instance), and a single-target
// caster stacks several instances on one mob — all die with it. (A *fade* line,
// by contrast, clears just one instance: see TestTimer_FadeClearsOneInstance.)
func TestTimer_SlainClearsAllOnMob(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	castMalo(tr, 1000)
	castMalo(tr, 1001) // two instances on "a sand giant"
	tr.Observe("You have slain a sand giant!", 1003)
	if act := tr.Active(1004); len(act) != 0 {
		t.Fatalf("slaying the mob should clear all its debuffs, got %d", len(act))
	}
}

// TestTimer_BuffRecastAlwaysRefreshes: a buff lands on a unique target (you),
// so re-casting it — even while it's still fresh (green) — refreshes the one
// timer rather than spawning a duplicate. Only detrimental spells on same-named
// mobs split into instances.
func TestTimer_BuffRecastAlwaysRefreshes(t *testing.T) {
	book := loadBook(t, row(map[int]string{
		fName: "Aegolism", fCastOnYou: "You feel the strength of the gods.",
		fCastTime: "0", fDurFormula: "5", fDurCap: "600", fGoodEffect: "1", // beneficial
	}))
	tr := NewTracker(book)
	tr.SetLevel(60)
	tr.BeginCast("Aegolism", 1000)
	tr.Observe("You feel the strength of the gods.", 1000)
	tr.BeginCast("Aegolism", 1005) // re-cast while still fresh
	tr.Observe("You feel the strength of the gods.", 1005)

	if act := tr.Active(1006); len(act) != 1 {
		t.Fatalf("re-casting a buff on yourself should refresh one timer, not duplicate; got %d", len(act))
	}
}
