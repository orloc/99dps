package gamestate

import (
	"strconv"
	"strings"
	"testing"

	"99dps/internal/eqclass"
)

// canniBook builds a book with a single Cannibalize spell that has a recast time.
func canniBook(t *testing.T, name string, recastMs int) *Book {
	t.Helper()
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name
	f[15] = strconv.Itoa(recastMs)
	book, err := LoadReader(strings.NewReader(strings.Join(f, "^")))
	if err != nil {
		t.Fatalf("LoadReader: %v", err)
	}
	return book
}

// TestCanni_PersistsAfterLapse: once a dance lapses, CanniStats stays present as
// a muted summary (session best/score) rather than vanishing, so the Damage-panel
// footer can keep showing it between dances.
func TestCanni_PersistsAfterLapse(t *testing.T) {
	tr := NewTracker(canniBook(t, "Cannibalize III", 9000))
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassShaman)

	tr.BeginCast("Cannibalize III", 1000)
	tr.BeginCast("Cannibalize III", 1009) // a clean second cast → builds score/best

	if live := tr.CanniStats(1010); !live.Active {
		t.Fatalf("dance should be active mid-cast, got %+v", live)
	}
	// well past the dance timeout
	lapsed := tr.CanniStats(1009 + canniDanceTimeoutSec + 5)
	if lapsed.Active {
		t.Errorf("dance should be inactive after the timeout, got %+v", lapsed)
	}
	if lapsed.Score == 0 {
		t.Errorf("lapsed summary should retain the session score, got %+v", lapsed)
	}

	// a character/zone switch clears it entirely
	tr.Clear()
	if cleared := tr.CanniStats(2000); cleared.Active || cleared.Score != 0 {
		t.Errorf("Clear should reset the canni meter, got %+v", cleared)
	}
}
