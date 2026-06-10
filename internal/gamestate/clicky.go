package gamestate

import "99dps/internal/eqclass"

// Clicky is an instant-cast clicky item whose buff lands with NO "You begin
// casting X" line — so the normal pending-cast → landing-emote path never fires.
// It also covers clickies whose effect emote isn't in spells_us.txt (so the
// byEmote self-clicky path in book.go misses them too). When Message appears in
// the log (timestamp stripped), a self-timer for Effect starts.
//
// Duration is resolved in this order: the spell book's duration for Effect at
// your level (preferred — keeps it level-correct), else the explicit Seconds
// fallback (for item effects that aren't a real spell in spells_us). Item is
// documentation only; the match is on Message.
type Clicky struct {
	Item    string // the clicky item (reference / docs)
	Effect  string // the granted buff — looked up in spells_us by name for duration
	Message string // the exact log line (no leading timestamp) when it lands on you
	Seconds int    // duration fallback when Effect isn't a timed spell in spells_us
}

// clickyRegistry maps each class to the insta-clicky items its players use whose
// landing isn't otherwise tracked. ADD YOUR ITEMS HERE (or at runtime with
// RegisterClicky).
//
// The match is GATED ON THE DETECTED CLASS: only entries under the player's
// current class are considered, so an ambiguous emote can resolve to a
// different item per class. Entries under eqclass.ClassUnknown are the
// "universal" bucket — class-agnostic items, checked for every class. (A
// class-specific entry therefore only resolves once a /who reveals the class;
// universal entries always work.)
//
// Example (verify the exact Message against your own log before trusting it):
//
//	eqclass.ClassRanger: {
//	    {Item: "Journeyman Boots", Effect: "Spirit of Wolf", Message: "Your feet move faster."},
//	},
//	eqclass.ClassShadowKnight: {
//	    {Item: "Reaper of the Dead", Effect: "Lifedraw", Message: "...", Seconds: 90},
//	},
//	eqclass.ClassUnknown: { // usable by anyone, regardless of detected class
//	    {Item: "Some Universal Clicky", Effect: "Some Buff", Message: "..."},
//	},
var clickyRegistry = map[eqclass.Class][]Clicky{}

// RegisterClicky adds (or appends) a clicky for a class at runtime — handy for
// tests and for wiring a user config later without editing the literal above.
func RegisterClicky(class eqclass.Class, c Clicky) {
	clickyRegistry[class] = append(clickyRegistry[class], c)
}

// matchClickyLocked starts a self-buff timer when a registered clicky's message
// appears, gated on the detected class (class-specific entries first, then the
// universal eqclass.ClassUnknown bucket). Caller holds the lock; only called
// when no cast was pending (a clicky produces no "begin casting" line).
func (t *Tracker) matchClickyLocked(body string, at int64) {
	if t.tryClickiesLocked(clickyRegistry[t.class], body, at) {
		return
	}
	if t.class != eqclass.ClassUnknown {
		t.tryClickiesLocked(clickyRegistry[eqclass.ClassUnknown], body, at)
	}
}

// tryClickiesLocked starts the first clicky in list whose Message matches body;
// reports whether one did.
func (t *Tracker) tryClickiesLocked(list []Clicky, body string, at int64) bool {
	for _, c := range list {
		if c.Message == "" || body != c.Message {
			continue
		}
		dur := c.Seconds
		detrimental := false
		if s, ok := t.book.ByName(c.Effect); ok {
			if d := s.DurationSeconds(t.levelOrDefault()); d > 0 {
				dur = d
			}
			detrimental = s.Detrimental
		}
		if dur <= 0 {
			return false // nothing to time (unknown effect, no fallback)
		}
		t.timers[key(c.Effect, "You")] = Timer{
			Spell:       c.Effect,
			Target:      "You",
			Start:       at,
			Expiry:      at + int64(dur),
			Detrimental: detrimental,
		}
		return true
	}
	return false
}
