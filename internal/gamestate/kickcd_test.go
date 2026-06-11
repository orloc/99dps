package gamestate

import (
	"testing"

	"99dps/internal/eqclass"
)

// TestCooldown_MonkKickAndStrike: a known monk's kick/strike start their reuse
// cooldowns (with the full reuse exposed for the charge bar).
func TestCooldown_MonkKickAndStrike(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.SetClass(eqclass.ClassMonk)
	tr.Observe("You kick a sand giant for 24 points of damage.", 1000)
	tr.Observe("You strike a sand giant for 18 points of damage.", 1000)

	got := map[string]int64{}
	for _, cd := range tr.Cooldowns(1001) {
		got[cd.Name] = cd.Total
	}
	for _, name := range []string{"Kick", "Strike"} {
		if got[name] != monkSpecialReuseSec {
			t.Errorf("%s cooldown Total = %d, want %d", name, got[name], monkSpecialReuseSec)
		}
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
