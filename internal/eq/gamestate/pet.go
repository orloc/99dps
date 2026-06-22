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
// The "...Master." command replies are a leak-prone FALLBACK, used only for charm
// pets: they appear in nearby players' logs too (you see a group-mate's pet say
// "Following you, Master."), so on their own they mis-attribute others' pets to
// you. But a charmed mob keeps a generic mob name and rarely gets a /pet leader
// line, so its kills/damage would go uncredited. We bridge that by adopting a
// "...Master." speaker as YOUR pet ONLY while you have an active charm running
// (you do have a charmed pet then) — and never over an owner a /pet leader line
// already settled. PetName (your own pet) is the pet whose owner is the tracked
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

// parsePetReply splits a pet line in either form EQ uses on P99: "<Speaker> says
// '<msg>'" or "<Speaker> tells you, '<msg>'" (the pet command-replies use the
// latter for an /pet attack). Returns ("","",false) for any other line.
func parsePetReply(body string) (speaker, msg string, ok bool) {
	if !strings.HasSuffix(body, "'") {
		return "", "", false
	}
	for _, sep := range []string{" says '", " tells you, '"} {
		if i := strings.Index(body, sep); i > 0 {
			return body[:i], body[i+len(sep) : len(body)-1], true
		}
	}
	return "", "", false
}

// hasActiveCharmLocked reports whether the player currently has a charm running (a
// charm timer not yet cleared by the "worn off" break). Caller holds the lock.
func (t *Tracker) hasActiveCharmLocked() bool {
	for _, tm := range t.timers {
		if tm.Charm {
			return true
		}
	}
	return false
}

// observePetLocked links a pet to its owner from a pet line. The authoritative
// signal is "<Pet> says 'My leader is <Owner>.'" (/pet leader), recorded for the
// whole group. As a fallback for charm pets — which keep a generic mob name and
// rarely get a leader line — a "...Master." command-reply adopts the speaker as
// YOUR pet, but only while you have an active charm (so a nearby enchanter's pet
// reply isn't grabbed) and never over an owner a leader line already set. Caller
// holds the lock.
func (t *Tracker) observePetLocked(body string) {
	speaker, msg, ok := parsePetReply(body)
	if !ok || speaker == "" {
		return
	}
	if owner, isLeader := strings.CutPrefix(msg, "My leader is "); isLeader {
		owner = strings.TrimSuffix(owner, ".")
		if owner == "" {
			return
		}
		if t.petOwners == nil {
			t.petOwners = map[string]string{}
		}
		t.petOwners[strings.ToLower(speaker)] = owner
		if t.character != "" && strings.EqualFold(owner, t.character) {
			t.petName = speaker // the player's own pet
		}
		return
	}
	// charm-gated fallback: a command-reply while you're charming names your pet.
	if t.character != "" && strings.Contains(msg, "Master") && t.hasActiveCharmLocked() {
		if t.petOwners == nil {
			t.petOwners = map[string]string{}
		}
		if k := strings.ToLower(speaker); t.petOwners[k] == "" {
			t.petOwners[k] = t.character
			t.petName = speaker
		}
	}
}
