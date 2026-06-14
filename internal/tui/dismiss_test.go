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

// ccRow builds a minimal spells_us row for a pacify (lands with "looks less
// aggressive"), with a cast-on-you emote so an AoE cast can land on the player.
func ccRow(name, castOnYou string) string {
	f := make([]string, 84)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name                      // fName
	f[6] = castOnYou                 // fCastOnYou
	f[7] = " looks less aggressive." // fCastOnOther → Pacify flag
	f[8] = name + " fades."          // fFades
	f[13] = "0"                      // fCastTime (instant for the test)
	f[16] = "8"                      // fDurFormula (pacify)
	f[17] = "0"                      // fDurCap (uncapped → ~7 min)
	f[83] = "0"                      // fGoodEffect 0 = detrimental
	return strings.Join(f, "^")
}

// TestSelfCCNotDismissible: an AoE pacify that lands on YOU shows in CROWD CONTROL
// but its line carries no dismiss target — clicking it must not run
// DismissTarget("You") and wipe your buffs.
func TestSelfCCNotDismissible(t *testing.T) {
	book, err := gamestate.LoadReader(strings.NewReader(ccRow("Wake of Tranquility", "You feel tranquil.")))
	if err != nil {
		t.Fatal(err)
	}
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	now := time.Now().Unix()
	tr.BeginCast("Wake of Tranquility", now)
	tr.Observe("You feel tranquil.", now) // AoE pacify lands on the player → CC on "You"

	body, targets := timerColumn(themes[0], tr, 40, "", true, false, false)
	// CC lines render by target ("z You …"); the self-pacify shows under CROWD CONTROL.
	if !strings.Contains(body, "CROWD CONTROL") || !strings.Contains(body, "You") {
		t.Fatalf("expected the self-pacify to render in CROWD CONTROL:\n%s", body)
	}
	for ln, tgt := range targets {
		if tgt == "You" {
			t.Errorf("a CC-on-you line must not be dismissible (line %d → %q)", ln, tgt)
		}
	}
}
