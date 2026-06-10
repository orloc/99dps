package parser

import (
	"99dps/internal/gamestate"
	"os"
	"testing"
)

// End-to-end with the REAL spells_us.txt and the REAL CRLF log lines for a
// Bedlam self-buff. Set SPELLS_FILE to run; otherwise skips.
func TestObserveSpells_RealBedlam(t *testing.T) {
	path := os.Getenv("SPELLS_FILE")
	if path == "" {
		t.Skip("set SPELLS_FILE to run")
	}
	book, err := gamestate.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	p := &DmgParser{character: "Iznoa", tracker: tr}

	// exactly as they appear in the log (CRLF)
	p.observeSpells("[Sat Jun 06 22:55:21 2026] You begin casting Bedlam.\r")
	p.observeSpells("[Sat Jun 06 22:55:24 2026] Your eyes gleam with bedlam.\r")

	land, _ := parseTimestamp("[Sat Jun 06 22:55:24 2026] x")
	act := tr.Active(land + 5)
	if len(act) != 1 {
		t.Fatalf("got %d timers, want 1 (Bedlam)", len(act))
	}
	rem := act[0].Expiry - (land + 5)
	t.Logf("TIMER: %s on %s, %ds remaining (detrimental=%v)", act[0].Spell, act[0].Target, rem, act[0].Detrimental)
	if act[0].Spell != "Bedlam" || act[0].Target != "You" {
		t.Errorf("timer = %+v, want Bedlam/You", act[0])
	}
}
