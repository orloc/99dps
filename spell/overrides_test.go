package spell

import (
	"path/filepath"
	"testing"
)

func TestOverridesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ov.json")
	o := LoadOverrides(path)
	if _, ok := o.Get("Z", "m"); ok {
		t.Fatal("expected empty overrides")
	}

	if err := o.Set("Greater Faydark", "a named orc", 1680); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if s, ok := o.Get("Greater Faydark", "a named orc"); !ok || s != 1680 {
		t.Fatalf("get = (%d,%v), want 1680/true", s, ok)
	}

	// persisted to disk and reloads
	if s, ok := LoadOverrides(path).Get("Greater Faydark", "a named orc"); !ok || s != 1680 {
		t.Errorf("reload = (%d,%v), want 1680/true", s, ok)
	}

	// sec <= 0 clears it
	if err := o.Set("Greater Faydark", "a named orc", 0); err != nil {
		t.Fatalf("Set clear: %v", err)
	}
	if _, ok := o.Get("Greater Faydark", "a named orc"); ok {
		t.Error("override should be cleared")
	}
}

func TestTrackerOverrideOnKill(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})
	tr.UseOverrides(LoadOverrides(filepath.Join(t.TempDir(), "ov.json")))
	tr.Observe("You have entered Greater Faydark.", 1000) // default 425s

	tr.Observe("You have slain a named orc!", 1000)
	if rem := tr.Respawns(1000)[0].Remaining; rem != 425 {
		t.Fatalf("default remaining = %d, want 425", rem)
	}

	// setting an override updates the live timer (recomputed from kill time)...
	tr.SetOverride("a named orc", 1680)
	if rem := tr.Respawns(1000)[0].Remaining; rem != 1680 {
		t.Errorf("override should update live timer, got %d", rem)
	}

	// ...and future kills use it
	tr.Observe("You have slain a named orc!", 2000)
	found := false
	for _, r := range tr.Respawns(2000) {
		if r.Mob == "a named orc" && r.Remaining == 1680 {
			found = true
		}
	}
	if !found {
		t.Error("a later kill should use the override (1680s)")
	}
}
