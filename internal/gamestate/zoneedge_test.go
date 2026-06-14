package gamestate

import (
	"path/filepath"
	"testing"
)

// TestSetOverride_RevertsToDefault: clearing an override (sec<=0) reverts the
// mob's live timers and future kills back to the zone default — the complement of
// the existing set-then-kill coverage.
func TestSetOverride_RevertsToDefault(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.UseOverrides(LoadOverrides(filepath.Join(t.TempDir(), "ov.json")))
	tr.Observe("You have entered Greater Faydark.", 1000)

	// first kill at the zone default (425s)
	tr.Observe("You have slain a large orc!", 1000)
	if rs := tr.Respawns(1000); len(rs) != 1 || rs[0].Remaining != 425 {
		t.Fatalf("default repop = %+v, want 425", rs)
	}

	// override that mob to 60s, then clear it back to the zone default
	tr.SetOverride("a large orc", 60)
	if rs := tr.Respawns(1000); rs[0].Remaining != 60 {
		t.Fatalf("override should retro-fix the live timer, got %d", rs[0].Remaining)
	}

	// clearing the override (sec<=0) reverts live timers to the zone default...
	tr.SetOverride("a large orc", 0)
	if rs := tr.Respawns(1000); rs[0].Remaining != 425 {
		t.Errorf("clearing the override should revert the live timer, got %d", rs[0].Remaining)
	}
	// ...and a fresh kill of the same spawn is back on the zone default too
	tr.Observe("You have slain a large orc!", 1500)
	if rs := tr.Respawns(1500); rs[0].Remaining != 425 {
		t.Errorf("a future kill after clearing should use the default, got %d", rs[0].Remaining)
	}
}

// TestKillerIsMob_Heuristic: a capitalized killer is a player, a lowercase one or
// an article-led name is a mob.
func TestKillerIsMob_Heuristic(t *testing.T) {
	cases := map[string]bool{
		"a large orc": true,  // article + lowercase
		"an ancient":  true,  // "an "
		"the Sarnak":  true,  // "the "
		"sand giant":  true,  // bare lowercase
		"Gnadad":      false, // a player
		"Borric!":     false, // trailing punctuation stripped, still a player
		"":            false, // empty
	}
	for killer, want := range cases {
		if got := killerIsMob(killer); got != want {
			t.Errorf("killerIsMob(%q) = %v, want %v", killer, got, want)
		}
	}
}

// TestRespawn_PurgesLongDead: an entry that's been "up" longer than
// respawnKeepUpSec is dropped from the list.
func TestRespawn_PurgesLongDead(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.Observe("You have entered Greater Faydark.", 1000)
	tr.Observe("You have slain a large orc!", 1000) // expiry 1425

	// still listed just after it pops (within the keep-up grace)
	if rs := tr.Respawns(1425 + respawnKeepUpSec - 1); len(rs) != 1 {
		t.Fatalf("a freshly-popped mob should still be listed, got %d", len(rs))
	}
	// dropped once it's been up past the grace window
	if rs := tr.Respawns(1425 + respawnKeepUpSec + 1); len(rs) != 0 {
		t.Errorf("a long-dead entry should be purged, got %d", len(rs))
	}
}

// TestZone_UnknownAtStartup: with no zone-in seen, Zone is "" and ZoneKnown is
// false; the nil-tracker facades return the same zero values.
func TestZone_UnknownAtStartup(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	if tr.Zone() != "" || tr.ZoneKnown() {
		t.Errorf("startup zone = %q known=%v, want empty/false", tr.Zone(), tr.ZoneKnown())
	}

	var nilTr *Tracker
	if nilTr.Zone() != "" || nilTr.ZoneKnown() || nilTr.Respawns(0) != nil {
		t.Error("nil tracker zone facades should be zero values")
	}
	k, ph, d := nilTr.ZoneKillStats(0)
	if k != 0 || ph != 0 || d != 0 {
		t.Errorf("nil ZoneKillStats = %d/%d/%d, want 0/0/0", k, ph, d)
	}
	nilTr.SetOverride("x", 1) // must not panic
	nilTr.UseOverrides(nil)   // must not panic
}

// TestCanniMeter_EdgeBranches covers the empty/single-cast efficiency paths and
// the buzzer-before-dance no-op.
func TestCanniMeter_EdgeBranches(t *testing.T) {
	var c canniMeter

	// a buzzer with no dance in progress is ignored (lastCast == 0)
	c.recordBuzzerLocked()
	if c.buzzers != 0 {
		t.Errorf("a buzzer before any cast should be a no-op, buzzers = %d", c.buzzers)
	}

	// efficiency of a fresh meter (no casts) is 0
	if got := c.pctLocked(); got != 0 {
		t.Errorf("pct with no casts = %d, want 0", got)
	}

	// a single cast: no intervals yet, so throughput defaults to 100% (perfect so far)
	book := canniBook(t, "Cannibalize", 4500)
	c.recordCastLocked(book, "Cannibalize", 1000)
	if got := c.pctLocked(); got != 100 {
		t.Errorf("pct after a single cast = %d, want 100 (no interval to penalize yet)", got)
	}

	// a too-early second press buzzes: accuracy drops, breaking the perfect score
	c.recordBuzzerLocked()
	if c.buzzers != 1 || c.combo != 0 {
		t.Errorf("buzzer mid-dance should count and break the combo, got %+v", c)
	}
	if got := c.pctLocked(); got >= 100 {
		t.Errorf("a buzzer should drag efficiency below 100, got %d", got)
	}
}

// TestCanniStats_NilTracker: the facade tolerates a nil tracker.
func TestCanniStats_NilTracker(t *testing.T) {
	var nilTr *Tracker
	if c := nilTr.CanniStats(0); c.Active || c.Pct != 0 {
		t.Errorf("nil CanniStats = %+v, want empty/inactive", c)
	}
}

// TestPet_NilTracker: the pet facades are nil-safe and PetOwner returns "" for an
// empty name or an unknown pet.
func TestPet_NilTracker(t *testing.T) {
	var nilTr *Tracker
	nilTr.SetCharacter("Yatiri") // must not panic
	if nilTr.PetName() != "" || nilTr.PetOwner("x") != "" {
		t.Error("nil tracker pet getters should be empty")
	}

	tr := newPetTracker(t, "Yatiri")
	if got := tr.PetOwner(""); got != "" {
		t.Errorf("PetOwner(\"\") = %q, want empty", got)
	}
	if got := tr.PetOwner("Stranger"); got != "" {
		t.Errorf("PetOwner of an unknown pet = %q, want empty", got)
	}
}
