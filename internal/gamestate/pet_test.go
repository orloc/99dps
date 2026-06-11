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

// TestPet_CommandRepliesIgnored: the "...Master." command replies must NOT set
// your pet — they leak into nearby players' logs, so a group-mate's pet saying
// "Following you, Master." would otherwise be mis-attributed to you.
func TestPet_CommandRepliesIgnored(t *testing.T) {
	for _, line := range []string{
		"Zarekab says 'Following you, Master.'",
		"Zarekab says 'At your service Master.'",
		"Zarekab says 'Sorry, Master..calming down.'",
	} {
		tr := newPetTracker(t, "Kelkix")
		tr.Observe(line, 1000)
		if got := tr.PetName(); got != "" {
			t.Errorf("%q should not set your pet, got %q", line, got)
		}
	}
}

// TestPet_OwnerMap: "My leader is <Owner>." links a pet to its owner for the
// whole group — your own (owner == character) sets PetName; a group-mate's only
// fills the owner map (so its damage can be attributed to them, not you).
func TestPet_OwnerMap(t *testing.T) {
	tr := newPetTracker(t, "Kelkix")
	tr.Observe("Zarekab says 'My leader is Sensive.'", 1000) // a group-mate's pet
	tr.Observe("Xenab says 'My leader is Kelkix.'", 1001)    // your own pet

	if got := tr.PetOwner("Zarekab"); got != "Sensive" {
		t.Errorf("PetOwner(Zarekab) = %q, want Sensive", got)
	}
	if got := tr.PetOwner("zarekab"); got != "Sensive" { // case-insensitive
		t.Errorf("PetOwner is case-sensitive: %q", got)
	}
	if got := tr.PetName(); got != "Xenab" {
		t.Errorf("PetName = %q, want Xenab (your own)", got)
	}
	if tr.PetOwner("Zarekab") == "Kelkix" {
		t.Error("a group-mate's pet must not be attributed to you")
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
