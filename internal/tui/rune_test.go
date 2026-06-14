package tui

import (
	"testing"

	"99dps/internal/gamestate"
)

// TestRuneRemaining: damage drains the shields in order — the first absorbs until
// empty, then the overflow eats the next.
func TestRuneRemaining(t *testing.T) {
	sh := []gamestate.RuneShield{{Name: "Bedlam", Pool: 350}, {Name: "Rune V", Pool: 700}}

	if got := runeRemaining(sh, 8, 60); got[0] != 0 || got[1] != 570 { // 480 absorbed
		t.Errorf("8×60: got %v, want [0 570]", got)
	}
	if got := runeRemaining(sh, 3, 60); got[0] != 170 || got[1] != 700 { // 180, all on Bedlam
		t.Errorf("3×60: got %v, want [170 700]", got)
	}
	if got := runeRemaining(sh, 0, 60); got[0] != 350 || got[1] != 700 {
		t.Errorf("fresh: got %v, want [350 700]", got)
	}
}
