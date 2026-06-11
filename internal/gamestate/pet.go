package gamestate

import "strings"

// Pet detection: EQ gives summoned/charmed pets a generated name (e.g. "Xenab")
// and logs their damage under it, with no link back to the owner. A pet names
// its owner only via `/pet leader`:
//
//	"<Pet> says 'My leader is <Owner>.'"
//
// This is the one reliable ownership signal, so we build a pet→owner map from
// every such line — for the whole group, not just you (a group-mate's
// `/pet leader` reveals their pet too). Yatiri's log shows a re-summon renames
// the pet (Xaber → Xenab), so the latest leader line wins.
//
// We do NOT use the "...Master." command replies: those leak into nearby
// players' logs (you see a group-mate's pet say "Following you, Master."), so
// they can't tell whose pet it is — relying on them mis-attributes others' pets
// to you. PetName (your own pet) is just the pet whose owner is the tracked
// character.

// SetCharacter records the tracked player (for the "My leader is <you>" check).
func (t *Tracker) SetCharacter(name string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.character = name
	t.mu.Unlock()
}

// PetName returns the player's current pet name, or "" if none has been seen.
func (t *Tracker) PetName() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.petName
}

// PetOwner returns the owner of a pet (by its dealer name), or "" if it isn't a
// known pet. Case-insensitive.
func (t *Tracker) PetOwner(pet string) string {
	if t == nil || pet == "" {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.petOwners[strings.ToLower(pet)]
}

// parsePetSays splits "<Speaker> says '<message>'" (note: no comma after "says"
// on P99). Returns ("","",false) for any other line.
func parsePetSays(body string) (speaker, msg string, ok bool) {
	const sep = " says '"
	i := strings.Index(body, sep)
	if i <= 0 || !strings.HasSuffix(body, "'") {
		return "", "", false
	}
	return body[:i], body[i+len(sep) : len(body)-1], true
}

// observePetLocked records a "<Pet> says 'My leader is <Owner>.'" line into the
// pet→owner map (for the whole group), and flags the player's own pet when the
// owner is the tracked character. Caller holds the lock.
func (t *Tracker) observePetLocked(body string) {
	speaker, msg, ok := parsePetSays(body)
	if !ok {
		return
	}
	owner, isLeader := strings.CutPrefix(msg, "My leader is ")
	if !isLeader {
		return
	}
	owner = strings.TrimSuffix(owner, ".")
	if owner == "" || speaker == "" {
		return
	}
	if t.petOwners == nil {
		t.petOwners = map[string]string{}
	}
	t.petOwners[strings.ToLower(speaker)] = owner
	if t.character != "" && strings.EqualFold(owner, t.character) {
		t.petName = speaker // the player's own pet
	}
}
