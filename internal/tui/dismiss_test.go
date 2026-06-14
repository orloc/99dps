package tui

import (
	"strings"
	"testing"
	"time"

	"99dps/internal/gamestate"
)

// TestOnYouDebuffNotDismissible: an incoming "ON YOU" debuff carries no caster, so
// its line must not map to a dismiss target — otherwise clicking it would run
// DismissTarget("You") and wipe your buffs.
func TestOnYouDebuffNotDismissible(t *testing.T) {
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	now := time.Now().Unix()
	tr.Observe("You feel drowsy.", now) // incoming Slow on You → ON YOU section

	body, targets := timerColumn(themes[0], tr, 40, "", false, false, true)
	if !strings.Contains(body, "ON YOU") || !strings.Contains(body, "Slowed") {
		t.Fatalf("expected the ON YOU debuff to render:\n%s", body)
	}
	for ln, tgt := range targets {
		if tgt == "You" {
			t.Errorf("an ON-YOU debuff line must not be dismissible (line %d → %q)", ln, tgt)
		}
	}
}
