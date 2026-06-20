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

func castRune(tr *Tracker, name, emote string, at int64) {
	tr.BeginCast(name, at)
	tr.Observe(emote, at)
}

const (
	bedlamEmote = "Your eyes gleam with bedlam."
	runeVEmote  = "A coat of shimmering runes surround you."
)

func soak(tr *Tracker, mob string, at int64) {
	tr.Observe(mob+" tries to hit YOU, but YOUR magical skin absorbs the blow!", at)
}

// TestRuneShields: casting recognized runes lists them in depletion order with
// their pools; absorbed blows are counted and the attacker captured.
func TestRuneShields(t *testing.T) {
	b := loadBook(t, runeRow("Bedlam", bedlamEmote), runeRow("Rune V", runeVEmote))
	tr := NewTracker(b)
	tr.SetLevel(60)
	now := int64(1000)
	castRune(tr, "Bedlam", bedlamEmote, now)
	castRune(tr, "Rune V", runeVEmote, now)

	shields, blows := tr.RuneShields(now+1, 0)
	if len(shields) != 2 || shields[0].Name != "Bedlam" || shields[1].Name != "Rune V" {
		t.Fatalf("want [Bedlam, Rune V] in depletion order, got %+v", shields)
	}
	if shields[0].Pool != 350 || shields[1].Pool != 700 {
		t.Errorf("pools = %d/%d, want 350/700", shields[0].Pool, shields[1].Pool)
	}
	if blows != 0 {
		t.Errorf("fresh runes → 0 blows, got %d", blows)
	}

	for i := 0; i < 3; i++ {
		soak(tr, "A spectre", now+2)
	}
	if _, blows := tr.RuneShields(now+3, 0); blows != 3 {
		t.Errorf("want 3 soaked blows, got %d", blows)
	}
	if a := tr.RuneAttacker(); a != "A spectre" {
		t.Errorf("attacker = %q, want 'A spectre'", a)
	}
}

// TestRunePerShieldRefresh is the separate-pools bug: refreshing the TOP rune
// (Bedlam) must restore only its own pool and leave the underlying Rune's
// depletion intact — not wipe the whole stack as the old shared counter did.
func TestRunePerShieldRefresh(t *testing.T) {
	b := loadBook(t, runeRow("Bedlam", bedlamEmote), runeRow("Rune V", runeVEmote))
	tr := NewTracker(b)
	tr.SetLevel(60)
	now := int64(1000)
	castRune(tr, "Bedlam", bedlamEmote, now)
	castRune(tr, "Rune V", runeVEmote, now)
	const avg = 50 // Bedlam 350 = 7 blows; Rune V 700

	// 10 blows = 500 dmg: Bedlam (350) depletes, 150 overflows into Rune V.
	for i := 0; i < 10; i++ {
		soak(tr, "a golem", now+1)
	}
	sh, _ := tr.RuneShields(now+2, avg)
	if sh[0].Remaining != 0 || sh[1].Remaining != 550 {
		t.Fatalf("after 500 absorbed: got Bedlam=%d RuneV=%d, want 0/550", sh[0].Remaining, sh[1].Remaining)
	}

	// refresh Bedlam → only Bedlam restores; Rune V keeps its 150 absorbed.
	castRune(tr, "Bedlam", bedlamEmote, now+3)
	sh, _ = tr.RuneShields(now+4, avg)
	if sh[0].Remaining != 350 || sh[1].Remaining != 550 {
		t.Fatalf("refreshing Bedlam must not refill Rune V: got Bedlam=%d RuneV=%d, want 350/550", sh[0].Remaining, sh[1].Remaining)
	}

	// 5 more blows (250 dmg) hit the refreshed Bedlam; Rune V untouched (no overflow).
	for i := 0; i < 5; i++ {
		soak(tr, "a golem", now+5)
	}
	sh, _ = tr.RuneShields(now+6, avg)
	if sh[0].Remaining != 100 || sh[1].Remaining != 550 {
		t.Errorf("post-refresh drain: got Bedlam=%d RuneV=%d, want 100/550", sh[0].Remaining, sh[1].Remaining)
	}
}
