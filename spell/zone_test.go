package spell

import "testing"

func hasMob(rs []Respawn, mob string) bool {
	for _, r := range rs {
		if r.Mob == mob {
			return true
		}
	}
	return false
}

func TestZoneRespawnTracking(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	if tr.Zone() != "" {
		t.Fatal("no zone initially")
	}

	tr.Observe("You have entered Greater Faydark.", 1000)
	if tr.Zone() != "Greater Faydark" || !tr.ZoneKnown() {
		t.Fatalf("zone = %q known=%v, want Greater Faydark/true", tr.Zone(), tr.ZoneKnown())
	}

	// a kill starts a repop timer at the zone default (425s for Greater Faydark)
	tr.Observe("You have slain a large orc!", 1000)
	rs := tr.Respawns(1000)
	if len(rs) != 1 || rs[0].Mob != "a large orc" || rs[0].Remaining != 425 {
		t.Fatalf("respawns = %+v, want a large orc / 425", rs)
	}
	if rem := tr.Respawns(1300)[0].Remaining; rem != 125 {
		t.Errorf("remaining at +300s = %d, want 125", rem)
	}

	// a SECOND same-named mob dying within the window is a distinct spawn — two
	// separate timers, not a reset
	tr.Observe("You have slain a large orc!", 1050)
	if rs := tr.Respawns(1050); len(rs) != 2 {
		t.Fatalf("two nearby same-name deaths should be 2 timers, got %d", len(rs))
	}

	// a group kill (someone else's killing blow) is also tracked
	tr.Observe("a young kodiak has been slain by Gnadad!", 1100)
	if !hasMob(tr.Respawns(1100), "a young kodiak") {
		t.Error("group kill (slain by a player) should be tracked")
	}

	// a player death (killed by a mob) must NOT be tracked
	tr.Observe("Gnadad has been slain by a large orc!", 1100)
	if hasMob(tr.Respawns(1100), "Gnadad") {
		t.Error("a player's death must not be tracked as a repop")
	}

	// zoning clears the list (different zone, different mobs)
	tr.Observe("You have entered Lesser Faydark.", 2000)
	if len(tr.Respawns(2000)) != 0 {
		t.Error("zone change should clear repops")
	}

	// an unknown zone has no timer, so kills don't start one
	tr.Observe("You have entered Some Unknown Place.", 3000)
	if tr.ZoneKnown() {
		t.Error("unknown zone should not be marked known")
	}
	tr.Observe("You have slain a mob!", 3000)
	if len(tr.Respawns(3000)) != 0 {
		t.Error("no repop timer should start in an unknown zone")
	}

	tr.Clear()
	if tr.Zone() != "" || len(tr.Respawns(3000)) != 0 {
		t.Error("Clear should reset zone + repops")
	}
}
