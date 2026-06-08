package cli

import (
	"reflect"
	"testing"

	"99dps/spell"
)

func TestDueAnnouncements(t *testing.T) {
	a := &App{ttsOn: true, announced: map[string]bool{}}
	now := int64(1000)

	healthy := spell.Timer{Spell: "Clarity", Target: "Tankguy", Expiry: now + 600}
	low := spell.Timer{Spell: "Clarity", Target: "Healer", Expiry: now + 8} // ≤15s
	charm := spell.Timer{Spell: "Charm", Target: "Charm", Expiry: now + 5, Charm: true}

	// first pass: only the low non-charm timer is announced
	got := a.dueAnnouncements([]spell.Timer{healthy, low, charm}, now)
	if want := []string{"Healer, Clarity low"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first pass = %v, want %v", got, want)
	}
	// second pass, same state: nothing new (already announced)
	if got := a.dueAnnouncements([]spell.Timer{healthy, low, charm}, now); got != nil {
		t.Errorf("repeat pass spoke again: %v", got)
	}

	// the buff is refreshed (now healthy) → re-arm; then drops low again → speaks
	refreshed := spell.Timer{Spell: "Clarity", Target: "Healer", Expiry: now + 600}
	a.dueAnnouncements([]spell.Timer{refreshed}, now)
	lowAgain := spell.Timer{Spell: "Clarity", Target: "Healer", Expiry: now + 5}
	if got := a.dueAnnouncements([]spell.Timer{lowAgain}, now); len(got) != 1 {
		t.Errorf("refresh should re-arm; got %v", got)
	}

	// self buff uses the short phrasing
	a2 := &App{ttsOn: true, announced: map[string]bool{}}
	self := spell.Timer{Spell: "Bedlam", Target: "You", Expiry: now + 5}
	if got := a2.dueAnnouncements([]spell.Timer{self}, now); len(got) != 1 || got[0] != "Bedlam low" {
		t.Errorf("self phrase = %v, want [\"Bedlam low\"]", got)
	}

	// disabled → silent
	a3 := &App{ttsOn: false, announced: map[string]bool{}}
	if got := a3.dueAnnouncements([]spell.Timer{low}, now); got != nil {
		t.Errorf("disabled should be silent, got %v", got)
	}
}
