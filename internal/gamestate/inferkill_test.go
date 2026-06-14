package gamestate

import "testing"

// cripple is a second instant debuff with a distinct landing emote, so a test can
// have two differently-named debuffed mobs at once.
func cripple() string {
	return row(map[int]string{
		fName:        "Cripple",
		fCastOnOther: "'s body weakens.",
		fFades:       " looks stronger.",
		fCastTime:    "0",
		fDurFormula:  "1",
		fDurCap:      "50",
		fGoodEffect:  "0", // detrimental
	})
}

// TestInferredKillFromXP: a mob killed by a DoT (or charmed pet) logs only "You
// gain experience" with no slain line. With a single debuffed mob, we infer THAT
// mob died — spawning its repop and clearing its (lingering) debuff timers.
func TestInferredKillFromXP(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	tr.Observe("You have entered Greater Faydark.", 1000) // gives the zone a respawn time
	castMalo(tr, 1001)                                    // debuff "a sand giant"
	if len(tr.Active(1002)) != 1 {
		t.Fatal("setup: expected one debuff timer")
	}

	tr.Observe("You gain experience!!", 1003) // xp, no slain line

	if act := tr.Active(1004); len(act) != 0 {
		t.Fatalf("an xp-only kill should clear the dead mob's debuffs, got %d", len(act))
	}
	if !hasMob(tr.Respawns(1004), "a sand giant") {
		t.Errorf("an xp-only kill should spawn a repop for the debuffed mob")
	}
}

// TestInferredKillAmbiguous: with two differently-named debuffed mobs we can't
// tell which died, so an xp-only line infers nothing (timers stay, no repop).
func TestInferredKillAmbiguous(t *testing.T) {
	tr := NewTracker(loadBook(t, malo(), cripple()))
	tr.SetLevel(60)
	tr.Observe("You have entered Greater Faydark.", 1000)
	castMalo(tr, 1001) // "a sand giant"
	tr.BeginCast("Cripple", 1001)
	tr.Observe("an orc's body weakens.", 1001) // "an orc"
	if len(tr.Active(1002)) != 2 {
		t.Fatalf("setup: expected two debuff timers, got %d", len(tr.Active(1002)))
	}

	tr.Observe("You gain experience!!", 1003)

	if len(tr.Active(1004)) != 2 {
		t.Error("ambiguous (two debuffed mobs) → infer nothing, both timers stay")
	}
	if len(tr.Respawns(1004)) != 0 {
		t.Error("ambiguous xp should not spawn a repop")
	}
}

// TestXPNoDebuffNoInference: xp with no active debuff is non-kill xp (a quest
// turn-in) — it must not invent a repop.
func TestXPNoDebuffNoInference(t *testing.T) {
	tr := NewTracker(loadBook(t, malo()))
	tr.SetLevel(60)
	tr.Observe("You have entered Greater Faydark.", 1000)
	tr.Observe("You gain experience!!", 1003)
	if len(tr.Respawns(1004)) != 0 {
		t.Error("xp with nothing debuffed should not spawn a repop (turn-in)")
	}
}
