package gamestate

import (
	"testing"

	"99dps/internal/eqclass"
)

// TestCooldown_MonkSpecialSharedTimer: monk specials share ONE reuse timer, so a
// kick and a hand strike drive a single "Kick" cooldown — never two — and a
// strike (re)starts the same timer.
func TestCooldown_MonkSpecialSharedTimer(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.SetClass(eqclass.ClassMonk)

	tr.Observe("You kick a sand giant for 24 points of damage.", 1000)
	cds := tr.Cooldowns(1001)
	if len(cds) != 1 || cds[0].Name != "Kick" || cds[0].Total != monkSpecialReuseSec {
		t.Fatalf("a kick should start one shared special cooldown; got %+v", cds)
	}

	// a hand strike later restarts the SAME timer, not a second one
	tr.Observe("You strike a sand giant for 18 points of damage.", 1003)
	cds = tr.Cooldowns(1004)
	if len(cds) != 1 {
		t.Fatalf("a strike must reuse the shared timer, not add a second cooldown; got %+v", cds)
	}
	if cds[0].Remaining != monkSpecialReuseSec-1 { // restarted at 1003, read at 1004
		t.Errorf("strike should restart the shared timer; remaining = %d, want %d", cds[0].Remaining, monkSpecialReuseSec-1)
	}
}

// TestCooldown_KickNotForNonMonk: kick isn't monk-exclusive, so a warrior's kick
// must neither start the cooldown nor flip the detected class to monk.
func TestCooldown_KickNotForNonMonk(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.SetClass(eqclass.ClassWarrior)
	tr.Observe("You kick a sand giant for 24 points of damage.", 1000)
	if cds := tr.Cooldowns(1001); len(cds) != 0 {
		t.Errorf("a warrior's kick must not create a cooldown; got %v", cds)
	}
	if tr.Class() != eqclass.ClassWarrior {
		t.Errorf("a kick must not change the detected class; got %v", tr.Class())
	}
}
