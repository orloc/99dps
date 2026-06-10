package gamestate

import "strings"

// Pet detection: EQ gives summoned/charmed pets a generated name (e.g. "Xenab")
// and logs their damage under it, with no link back to the owner. But a pet
// announces itself in two ways we can read:
//
//   - `/pet leader` →  "<Pet> says 'My leader is <Owner>.'"
//   - any pet command reply →  "<Pet> says 'Following you, Master.'" /
//     "...At your service Master." / "Changing position, Master." /
//     "Sorry, Master..calming down." (and friends — all addressed to "Master")
//
// In your own log you only see *your* pet's command replies, so the speaker of
// either line is your pet. The "My leader is" form is the strongest: we accept
// it only when the named owner is the tracked character. A re-summon/re-charm
// renames the pet (Yatiri's log shows Xaber → Xenab), so the latest wins.

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

// observePetLocked learns the player's pet name from a /pet leader reply or a pet
// command reply. Caller holds the lock.
func (t *Tracker) observePetLocked(body string) {
	speaker, msg, ok := parsePetSays(body)
	if !ok {
		return
	}
	// "My leader is <owner>." — accept only when the owner is us (or we don't yet
	// know our own name), so a nearby pet's reply can't claim us.
	if owner, isLeader := strings.CutPrefix(msg, "My leader is "); isLeader {
		owner = strings.TrimSuffix(owner, ".")
		if t.character == "" || strings.EqualFold(owner, t.character) {
			t.petName = speaker
		}
		return
	}
	// a command reply addressed to "Master" — only your own pet's are shown to you.
	if strings.Contains(msg, "Master") {
		t.petName = speaker
	}
}
