package gamestate

import (
	"strings"
	"testing"
)

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

// TestPet_CommandRepliesIgnored: with NO active charm, the "...Master." command
// replies must NOT set your pet — they leak into nearby players' logs, so a
// group-mate's pet saying "Following you, Master." would be mis-attributed to you.
func TestPet_CommandRepliesIgnored(t *testing.T) {
	for _, line := range []string{
		"Zarekab says 'Following you, Master.'",
		"Zarekab says 'At your service Master.'",
		"Zarekab says 'Sorry, Master..calming down.'",
	} {
		tr := newPetTracker(t, "Kelkix")
		tr.Observe(line, 1000)
		if got := tr.PetName(); got != "" {
			t.Errorf("%q should not set your pet (no active charm), got %q", line, got)
		}
	}
}

// TestPet_CharmedPetFromCommandReply: while a charm is active, a "...Master."
// command-reply adopts the charmed mob as your pet (the /pet-leader fallback for
// charm-killers — see Iznoa's logs). A leader line stays authoritative.
func TestPet_CharmedPetFromCommandReply(t *testing.T) {
	tr := newPetTracker(t, "Iznoa")
	tr.book = loadBook(t, row(map[int]string{
		fName: "Allure", fFades: "You are no longer charmed.",
		fCastTime: "4000", fDurFormula: "10", fDurCap: "70", fGoodEffect: "0",
	}))
	tr.SetLevel(60)

	// no charm yet → the reply is ignored, as before.
	tr.Observe("greater war boned skeleton says 'Following you, Master.'", 1000)
	if tr.PetName() != "" {
		t.Fatal("a command-reply with no active charm must not set your pet")
	}

	// cast a charm → it lands on cast completion; now the same reply names your pet.
	tr.BeginCast("Allure", 1010)
	tr.Observe("a bile golem hits a kobold for 10 points of damage.", 1015) // any line completes the cast
	tr.Observe("greater war boned skeleton tells you, 'Attacking a bile golem Master.'", 1016)
	if got := tr.PetName(); got != "greater war boned skeleton" {
		t.Fatalf("an active-charm command-reply should adopt the pet, got %q", got)
	}
	if got := tr.PetOwner("greater war boned skeleton"); got != "Iznoa" {
		t.Errorf("the charmed pet should be owned by you, got %q", got)
	}

	// a /pet leader line stays authoritative and is never overridden by a reply.
	tr.Observe("greater war boned skeleton says 'My leader is Sensive.'", 1020)
	tr.Observe("greater war boned skeleton says 'Following you, Master.'", 1021)
	if got := tr.PetOwner("greater war boned skeleton"); got != "Sensive" {
		t.Errorf("a leader line must win over the command-reply fallback, got %q", got)
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

func TestParsePetReply(t *testing.T) {
	sp, msg, ok := parsePetReply("Xenab says 'My leader is Yatiri.'")
	if !ok || sp != "Xenab" || msg != "My leader is Yatiri." {
		t.Errorf("says parse = %q,%q,%v", sp, msg, ok)
	}
	// the /pet attack reply uses the "tells you," form
	sp, msg, ok = parsePetReply("greater war boned skeleton tells you, 'Attacking a bile golem Master.'")
	if !ok || sp != "greater war boned skeleton" || msg != "Attacking a bile golem Master." {
		t.Errorf("tells-you parse = %q,%q,%v", sp, msg, ok)
	}
	if _, _, ok := parsePetReply("a sand giant hits YOU for 50 points of damage."); ok {
		t.Error("non-reply line should not parse")
	}
	if !strings.HasSuffix("x'", "'") { // sanity for the suffix guard
		t.Fatal("unreachable")
	}
}
