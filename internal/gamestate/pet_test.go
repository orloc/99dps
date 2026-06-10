package gamestate

import (
	"strings"
	"testing"
)

func newPetTracker(t *testing.T, character string) *Tracker {
	t.Helper()
	tr := NewTracker(loadBook(t)) // empty book is fine; pet detection doesn't need spells
	tr.SetCharacter(character)
	return tr
}

// TestPet_LeaderReply: "<Pet> says 'My leader is <you>.'" names your pet.
func TestPet_LeaderReply(t *testing.T) {
	tr := newPetTracker(t, "Yatiri")
	tr.Observe("Xenab says 'My leader is Yatiri.'", 1000)
	if got := tr.PetName(); got != "Xenab" {
		t.Errorf("PetName = %q, want Xenab", got)
	}
	// a re-summon renames the pet — the latest wins
	tr.Observe("Xaber says 'My leader is Yatiri.'", 1010)
	if got := tr.PetName(); got != "Xaber" {
		t.Errorf("PetName after re-summon = %q, want Xaber", got)
	}
}

// TestPet_CommandReplies: the "Master" command replies also reveal the pet.
func TestPet_CommandReplies(t *testing.T) {
	for _, line := range []string{
		"Xenab says 'Following you, Master.'",
		"Xenab says 'At your service Master.'",
		"Xenab says 'Changing position, Master.'",
		"Xenab says 'Sorry, Master..calming down.'",
	} {
		tr := newPetTracker(t, "Yatiri")
		tr.Observe(line, 1000)
		if got := tr.PetName(); got != "Xenab" {
			t.Errorf("%q → PetName %q, want Xenab", line, got)
		}
	}
}

// TestPet_LeaderMismatch: "My leader is <someone else>." is not our pet.
func TestPet_LeaderMismatch(t *testing.T) {
	tr := newPetTracker(t, "Yatiri")
	tr.Observe("Spot says 'My leader is Gandalf.'", 1000)
	if got := tr.PetName(); got != "" {
		t.Errorf("another player's pet should be ignored, got %q", got)
	}
}

// TestPet_IgnoresChatter: ordinary speech doesn't get mistaken for a pet.
func TestPet_IgnoresChatter(t *testing.T) {
	tr := newPetTracker(t, "Yatiri")
	tr.Observe("Yatiri says 'hello there'", 1000)
	if got := tr.PetName(); got != "" {
		t.Errorf("plain chat should not set a pet, got %q", got)
	}
}

// TestPet_ClearedOnSwitch: a character switch drops the pet.
func TestPet_ClearedOnSwitch(t *testing.T) {
	tr := newPetTracker(t, "Yatiri")
	tr.Observe("Xenab says 'My leader is Yatiri.'", 1000)
	tr.Clear()
	if got := tr.PetName(); got != "" {
		t.Errorf("Clear should drop the pet, got %q", got)
	}
}

func TestParsePetSays(t *testing.T) {
	sp, msg, ok := parsePetSays("Xenab says 'My leader is Yatiri.'")
	if !ok || sp != "Xenab" || msg != "My leader is Yatiri." {
		t.Errorf("parse = %q,%q,%v", sp, msg, ok)
	}
	if _, _, ok := parsePetSays("a sand giant hits YOU for 50 points of damage."); ok {
		t.Error("non-says line should not parse")
	}
	if !strings.HasSuffix("x'", "'") { // sanity for the suffix guard
		t.Fatal("unreachable")
	}
}
