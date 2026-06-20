package spellwiki

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestParse_SpiritOfWolf: a beneficial multi-class buff. The classes list (with a
// "(39 Pre Velious)" annotation on Ranger) parses to the current P99 levels, and
// the cast-on-other placeholder is stripped to the spells_us-style suffix.
func TestParse_SpiritOfWolf(t *testing.T) {
	s, ok := Parse(load(t, "spirit_of_wolf.wikitext"))
	if !ok {
		t.Fatal("should parse as a spell page")
	}
	if s.Name != "Spirit of Wolf" {
		t.Errorf("name = %q", s.Name)
	}
	if s.Detrimental {
		t.Error("SoW is Beneficial, not Detrimental")
	}
	want := map[string]int{"ranger": 30, "druid": 14, "shaman": 9}
	for cls, lv := range want {
		if got, ok := s.Classes[cls]; !ok || got != lv {
			t.Errorf("class %s = (%d,%v), want %d", cls, got, ok, lv)
		}
	}
	if len(s.Classes) != 3 {
		t.Errorf("classes = %v, want exactly 3", s.Classes)
	}
	// the "see also" / spell-list links must NOT be parsed as classes
	if _, ok := s.Classes[NormClass("Shaman Spells (Good)")]; ok {
		t.Error("a spell-list link leaked into Classes")
	}
	if lv, ok := s.CastableBy("Shaman"); !ok || lv != 9 {
		t.Errorf("CastableBy(Shaman) = (%d,%v), want (9,true)", lv, ok)
	}
	if _, ok := s.CastableBy("Warrior"); ok {
		t.Error("a Warrior cannot cast SoW")
	}
	if s.CastOnOther != " is surrounded by a brief lupine aura." {
		t.Errorf("cast-on-other = %q (placeholder not stripped cleanly)", s.CastOnOther)
	}
}

// TestParse_Cripple: a detrimental two-class debuff. Confirms the spell_type flag
// and that "Shadowknight"-style spacing wouldn't matter (Shaman/Enchanter here).
func TestParse_Cripple(t *testing.T) {
	s, ok := Parse(load(t, "cripple.wikitext"))
	if !ok {
		t.Fatal("should parse")
	}
	if !s.Detrimental {
		t.Error("Cripple is Detrimental")
	}
	if lv, ok := s.CastableBy("Shaman"); !ok || lv != 53 {
		t.Errorf("Shaman Cripple = (%d,%v), want 53", lv, ok)
	}
	if lv, ok := s.CastableBy("Enchanter"); !ok || lv != 53 {
		t.Errorf("Enchanter Cripple = (%d,%v), want 53", lv, ok)
	}
	if s.CastOnOther != " has been crippled." {
		t.Errorf("cast-on-other = %q", s.CastOnOther)
	}
}

// TestParse_Mesmerize: single-class mez; "4 ticks" duration parses as raw text.
func TestParse_Mesmerize(t *testing.T) {
	s, ok := Parse(load(t, "mesmerize.wikitext"))
	if !ok {
		t.Fatal("should parse")
	}
	if lv, ok := s.CastableBy("Enchanter"); !ok || lv != 4 {
		t.Errorf("Enchanter Mesmerize = (%d,%v), want 4", lv, ok)
	}
	if len(s.Classes) != 1 {
		t.Errorf("Mesmerize should be Enchanter-only, got %v", s.Classes)
	}
}

// TestParse_NotASpell: non-spell content (or a missing page) returns ok=false, so
// the caller treats it as "not on the server".
func TestParse_NotASpell(t *testing.T) {
	for _, raw := range []string{"", "#REDIRECT [[Something]]", "Just an article with no template."} {
		if _, ok := Parse(raw); ok {
			t.Errorf("Parse(%q) should not parse as a spell", raw)
		}
	}
}

// TestNormClass: spacing/case differences (eqclass "Shadow Knight" vs wiki
// "Shadowknight") normalize equal.
func TestNormClass(t *testing.T) {
	if NormClass("Shadow Knight") != NormClass("Shadowknight") {
		t.Error("Shadow Knight and Shadowknight should normalize equal")
	}
	if NormClass(" Shaman ") != "shaman" {
		t.Errorf("got %q", NormClass(" Shaman "))
	}
}
