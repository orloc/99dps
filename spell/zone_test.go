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
	if rs := tr.Respawns(1000); len(rs) != 1 || rs[0].Mob != "a large orc" || rs[0].Remaining != 425 {
		t.Fatalf("respawns = %+v, want a large orc / 425", rs)
	}
	if rem := tr.Respawns(1300)[0].Remaining; rem != 125 {
		t.Errorf("remaining at +300s = %d, want 125", rem)
	}

	// re-killing the SAME spawn after it has repopped (timer elapsed at 1425)
	// reuses its slot — still one entry, just reset
	tr.Observe("You have slain a large orc!", 1500)
	if rs := tr.Respawns(1500); len(rs) != 1 || rs[0].Remaining != 425 {
		t.Fatalf("re-kill of a repopped mob should reuse the slot, got %+v", rs)
	}

	// but a SECOND orc dying while the first is still pending is a distinct spawn
	tr.Observe("You have slain a large orc!", 1600)
	if rs := tr.Respawns(1600); len(rs) != 2 {
		t.Fatalf("a concurrent same-name death is a distinct spawn, got %d", len(rs))
	}

	// a group kill (someone else's killing blow) is also tracked
	tr.Observe("a young kodiak has been slain by Gnadad!", 1700)
	if !hasMob(tr.Respawns(1700), "a young kodiak") {
		t.Error("group kill (slain by a player) should be tracked")
	}

	// a player death (killed by a mob) must NOT be tracked
	tr.Observe("Gnadad has been slain by a large orc!", 1700)
	if hasMob(tr.Respawns(1700), "Gnadad") {
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

func TestRespawnMineFirstAndKiller(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.Observe("You have entered Greater Faydark.", 1000)

	tr.Observe("a young kodiak has been slain by Gnadad!", 1000) // a groupmate's kill
	tr.Observe("You have slain a large orc!", 1001)              // my kill, later

	rs := tr.Respawns(1001)
	if len(rs) != 2 {
		t.Fatalf("want 2 repops, got %d", len(rs))
	}
	// my kill sorts first even though it happened later
	if !rs[0].Mine || rs[0].Mob != "a large orc" || rs[0].Killer != "You" {
		t.Errorf("first row should be my orc kill, got %+v", rs[0])
	}
	if rs[1].Mine || rs[1].Mob != "a young kodiak" || rs[1].Killer != "Gnadad" {
		t.Errorf("second row should be Gnadad's kodiak, got %+v", rs[1])
	}
}
