package spellwiki

import (
	"sync"
	"testing"
)

// withDataset swaps in a controlled dataset for the duration of a test, so Validate
// can be exercised without depending on the committed spelldata.json. Restored after.
func withDataset(t *testing.T, spells map[string]Spell) {
	t.Helper()
	loaded = &dataset{Spells: spells}
	loadOnce.Do(func() {}) // mark the embedded load skipped so it can't overwrite
	t.Cleanup(func() { loaded = nil; loadOnce = sync.Once{} })
}

// TestValidate covers the four verdicts plus the empty-class guard, against a
// controlled set.
func TestValidate(t *testing.T) {
	withDataset(t, map[string]Spell{
		"cripple": {Name: "Cripple", Classes: map[string]int{"shaman": 53, "enchanter": 53}},
	})
	cases := []struct {
		spell, class      string
		wantFound, wantOK bool
	}{
		{"Cripple", "Shaman", true, true},         // in set, class casts it
		{"cripple", "Enchanter", true, true},      // case-folded lookup
		{"Cripple", "Warrior", true, false},       // in set, class can't cast
		{"Made Up Spell", "Shaman", false, false}, // not on the server
		{"Cripple", "", true, false},              // class unknown → can't judge castability
	}
	for _, c := range cases {
		f, ok := Validate(c.spell, c.class)
		if f != c.wantFound || ok != c.wantOK {
			t.Errorf("Validate(%q,%q) = (%v,%v), want (%v,%v)", c.spell, c.class, f, ok, c.wantFound, c.wantOK)
		}
	}
}

// TestKeyFolds: backtick/typographic apostrophes and case fold to one key, so a
// logged cast matches the wiki-derived key.
func TestKeyFolds(t *testing.T) {
	if Key("Solon`s Song") != Key("Solon's Song") {
		t.Error("backtick and apostrophe must fold to the same key")
	}
	if got := Key("Spirit Of Wolf"); got != "spirit of wolf" {
		t.Errorf("Key = %q, want lowercased", got)
	}
}

// TestEmbeddedDatasetLoads guards the committed spelldata.json: it must be present,
// non-trivial, and resolve a known anchor — catching an empty/clobbered file.
func TestEmbeddedDatasetLoads(t *testing.T) {
	if n := Count(); n < 1000 {
		t.Fatalf("embedded dataset has %d spells, expected ~1500 (regenerate with `go generate ./internal/spellwiki`)", n)
	}
	if f, ok := Validate("Spirit of Wolf", "Shaman"); !f || !ok {
		t.Error("embedded Spirit of Wolf should be castable by Shaman")
	}
	if Generated() == "" {
		t.Error("embedded dataset should carry a generated date")
	}
}
