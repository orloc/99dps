package gamestate

import (
	"os"
	"testing"
)

// TestLoad_RealFile validates parsing against an actual spells_us.txt when
// SPELLS_FILE points at one; otherwise it skips.
func TestLoad_RealFile(t *testing.T) {
	path := os.Getenv("SPELLS_FILE")
	if path == "" {
		t.Skip("set SPELLS_FILE to validate against a real spells_us.txt")
	}
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded %d spells", b.Len())
	for _, name := range []string{"Lightning Bolt", "Envenomed Bolt", "Clarity", "Snare", "Tashani"} {
		s, ok := b.ByName(name)
		if !ok {
			t.Errorf("missing %q", name)
			continue
		}
		t.Logf("%-16s cast=%5dms formula=%2d cap=%3d dur@50=%4ds detr=%-5v onOther=%q",
			name, s.CastTimeMs, s.DurFormula, s.DurCap, s.DurationSeconds(50), s.Detrimental, s.CastOnOther)
	}
}
