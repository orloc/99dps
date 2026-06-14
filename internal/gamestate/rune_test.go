package gamestate

import "testing"

// runeRow crafts an instant beneficial self-buff (a stand-in for a rune) so the
// test can land it without cast-completion timing.
func runeRow(name, castOnYou string) string {
	return row(map[int]string{
		fName: name, fCastOnYou: castOnYou, fCastTime: "0",
		fDurFormula: "3", fDurCap: "100", fGoodEffect: "1",
	})
}

// TestRuneShields: casting recognized runes lists them in depletion order with
// their pools; absorbed blows are counted and reset on a (re)cast.
func TestRuneShields(t *testing.T) {
	b := loadBook(t,
		runeRow("Bedlam", "Your eyes gleam with bedlam."),
		runeRow("Rune V", "A coat of shimmering runes surround you."),
	)
	tr := NewTracker(b)
	tr.SetLevel(60)
	now := int64(1000)
	tr.BeginCast("Bedlam", now)
	tr.Observe("Your eyes gleam with bedlam.", now)
	tr.BeginCast("Rune V", now)
	tr.Observe("A coat of shimmering runes surround you.", now)

	shields, blows, _ := tr.RuneShields(now + 1)
	if len(shields) != 2 || shields[0].Name != "Bedlam" || shields[1].Name != "Rune V" {
		t.Fatalf("want [Bedlam, Rune V] in depletion order, got %+v", shields)
	}
	if shields[0].Pool != 350 || shields[1].Pool != 700 {
		t.Errorf("pools = %d/%d, want 350/700", shields[0].Pool, shields[1].Pool)
	}
	if blows != 0 {
		t.Errorf("fresh runes → 0 blows, got %d", blows)
	}

	// each "magical skin absorbs the blow" is one soaked blow; the attacker is captured
	for i := 0; i < 3; i++ {
		tr.Observe("A spectre tries to hit YOU, but YOUR magical skin absorbs the blow!", now+2)
	}
	if _, blows, attacker := tr.RuneShields(now + 3); blows != 3 || attacker != "A spectre" {
		t.Errorf("want 3 blows from 'A spectre', got %d from %q", blows, attacker)
	}

	// recasting a rune resets the estimate (count + attacker)
	tr.BeginCast("Rune V", now+4)
	tr.Observe("A coat of shimmering runes surround you.", now+4)
	if _, blows, attacker := tr.RuneShields(now + 5); blows != 0 || attacker != "" {
		t.Errorf("a recast should reset the estimate, got %d blows / %q", blows, attacker)
	}
}
