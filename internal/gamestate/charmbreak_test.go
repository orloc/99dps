package gamestate

import "testing"

func TestCharmBroke(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	// inject an active charm timer (charm has no landing emote to script here)
	tr.timers = map[string]Timer{
		key("Allure", "Charm"): {Spell: "Allure", Target: "Charm", Charm: true, Start: 1000, Expiry: 9000},
	}

	if tr.CharmBroke(1100) {
		t.Fatal("no break yet")
	}
	tr.Observe("Your charm spell has worn off.", 1100)

	if !tr.CharmBroke(1100) {
		t.Error("charm break should be surfaced right after it happens")
	}
	if len(tr.Active(1100)) != 0 {
		t.Error("the charm timer should be cleared on break")
	}
	if tr.CharmBroke(1100 + charmBreakGraceSec + 1) {
		t.Error("charm break should stop surfacing after the grace window")
	}
}
