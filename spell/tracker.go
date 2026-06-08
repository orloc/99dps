package spell

import (
	"sort"
	"strings"
	"sync"

	"99dps/common"
)

// fallbackLevel is used to compute durations before we've seen the player's
// real level in the log. It's the max classic level, so level-capped formulas
// resolve to the spell's cap (its full duration).
const fallbackLevel = 60

// landWindowSec is how long after a cast completes we'll still accept a landing
// emote as belonging to that cast.
const landWindowSec = 12

// Timer is one active spell the player has on a target.
type Timer struct {
	Spell       string
	Target      string
	Start       int64 // unix seconds (log time)
	Expiry      int64
	Detrimental bool
	Charm       bool // a charm — display elapsed (counts up), not remaining
}

// Tracker holds the player's active spell timers. It's fed parsed log signals
// and is safe for concurrent use (the parser writes, the UI reads).
type Tracker struct {
	book *Book

	mu             sync.Mutex
	level          int
	class          common.Class
	timers         map[string]Timer // key: spell\x00target
	cooldowns      map[string]int64 // ability name -> reuse-expiry unix seconds
	feignAttemptAt int64            // log time of the last feign attempt (macro)
	feignFailAt    int64            // log time of the last failed feign (0 = none)
	bindStartAt    int64            // log time bandaging began
	bindDoneAt     int64            // log time bandaging last completed

	zone           string           // current zone (from a "You have entered" line)
	zoneRespawnSec int              // current zone's default respawn, 0 if unknown
	respawns       map[string]int64 // killed mob -> repop-expiry unix seconds

	// pending cast awaiting its landing emote
	pending   *Spell
	pendingAt int64
}

// NewTracker builds a tracker over a loaded spell book.
func NewTracker(book *Book) *Tracker {
	return &Tracker{
		book:      book,
		timers:    make(map[string]Timer),
		cooldowns: make(map[string]int64),
		respawns:  make(map[string]int64),
	}
}

// SpellCount is the number of spells in the loaded book.
func (t *Tracker) SpellCount() int {
	if t == nil || t.book == nil {
		return 0
	}
	return t.book.Len()
}

// SetLevel records the player's level (drives duration formulas). When the
// level changes — e.g. learned from a /who after timers were started at the
// fallback level — already-running timers are recomputed from their original
// cast time so their expiry reflects the correct duration.
func (t *Tracker) SetLevel(level int) {
	if level <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if level == t.level {
		return
	}
	t.level = level

	for k, tm := range t.timers {
		s, ok := t.book.ByName(tm.Spell)
		if !ok {
			continue
		}
		if dur := s.DurationSeconds(level); dur > 0 {
			tm.Expiry = tm.Start + int64(dur)
			t.timers[k] = tm
		}
	}
}

// SetClass records the player's class (derived from a /who level-title). A nil
// tracker or an unknown class is ignored, so callers needn't pre-check.
func (t *Tracker) SetClass(c common.Class) {
	if t == nil || c == common.ClassUnknown {
		return
	}
	t.mu.Lock()
	t.class = c
	t.mu.Unlock()
}

// Class returns the detected class, or ClassUnknown until a /who is seen.
func (t *Tracker) Class() common.Class {
	if t == nil {
		return common.ClassUnknown
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.class
}

// Level returns the player's known level, or 0 until a /who or level-up is seen.
func (t *Tracker) Level() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.level
}

// Category returns the player's panel category, defaulting to CatCaster (spell
// timers) until the class is known.
func (t *Tracker) Category() common.Category {
	return common.CategoryOf(t.Class())
}

// BeginCast remembers that the player started casting a known, timed spell, so a
// later landing emote can be attributed to it.
func (t *Tracker) BeginCast(spellName string, at int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// a fresh cast supersedes any prior pending one — even when this spell is
	// unknown/untimed — so a later landing emote can't mis-attribute to the
	// stale cast.
	t.pending = nil
	if s, ok := t.book.ByName(spellName); ok {
		t.pending = s
		t.pendingAt = at
	}
}

// Observe feeds one log line (timestamp body, no leading "[...]"). It matches a
// pending cast's landing emote, clears the pending cast on a resist, expires
// timers on a wear-off message, and drops debuffs when their target is slain.
func (t *Tracker) Observe(body string, at int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// collapse a doubled trailing period so log lines match stored emotes
	body = NormEmote(body)

	// a resisted, fizzled, or interrupted cast never lands
	if t.pending != nil && (strings.Contains(body, "resisted") ||
		(strings.HasPrefix(body, "Your ") &&
			(strings.Contains(body, "fizzle") || strings.Contains(body, "interrupt")))) {
		t.pending = nil
	}

	t.matchLandingLocked(body, at)
	t.expireByMessageLocked(body)
	t.expireOnSlainLocked(body)
	t.matchCooldownLocked(body, at)
	t.observeZoneLocked(body, at)

	// bind wound: "You begin to bandage <target>" … "the bandaging is complete"
	// — both are the player's own self-messages, so no name gating is needed.
	if strings.HasPrefix(body, "You begin to bandage") {
		t.bindStartAt = at
	}
	if strings.Contains(body, "bandaging is complete") {
		t.bindDoneAt = at
	}
}

// bindTimeoutSec clears a stuck "bandaging" indicator if the completion line is
// never seen (movement/damage interrupts bind wound with no message).
const bindTimeoutSec = 20

// Binding reports whether the player is mid-bandage at wall-clock `now`.
func (t *Tracker) Binding(now int64) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bindStartAt > t.bindDoneAt && now-t.bindStartAt <= bindTimeoutSec
}

func (t *Tracker) matchLandingLocked(body string, at int64) {
	if t.pending == nil {
		return
	}
	// drop a stale pending cast that never landed
	complete := t.pendingAt + int64(t.pending.CastTimeMs)/1000
	if at > complete+landWindowSec {
		t.pending = nil
		return
	}
	if at*1000 < t.pendingAt*1000+int64(t.pending.CastTimeMs)-600 {
		return // cast hasn't finished yet
	}

	// charm has no landing emote — start it on cast completion (counts up,
	// cleared by the "worn off" break message rather than a fixed expiry)
	if t.pending.Charm {
		t.startCharmLocked(at)
		return
	}

	var target string
	switch {
	case t.pending.CastOnOther != "" && strings.HasSuffix(body, t.pending.CastOnOther):
		target = strings.TrimSpace(strings.TrimSuffix(body, t.pending.CastOnOther))
	case t.pending.CastOnYou != "" && body == t.pending.CastOnYou:
		target = "You"
	default:
		return
	}
	if target == "" {
		return
	}

	dur := t.pending.DurationSeconds(t.levelOrDefault())
	if dur <= 0 {
		t.pending = nil // instant spell — nothing to time
		return
	}
	t.timers[key(t.pending.Name, target)] = Timer{
		Spell:       t.pending.Name,
		Target:      target,
		Start:       at,
		Expiry:      at + int64(dur),
		Detrimental: t.pending.Detrimental,
	}
	t.pending = nil
}

// startCharmLocked begins (or replaces) the single charm timer. Its expiry is
// the formula maximum — a safety ceiling — but in practice the "Your charm spell
// has worn off." message clears it first, since charm breaks unpredictably.
func (t *Tracker) startCharmLocked(at int64) {
	for k, tm := range t.timers {
		if tm.Charm {
			delete(t.timers, k) // only one charm at a time
		}
	}
	t.timers[key(t.pending.Name, "Charm")] = Timer{
		Spell:       t.pending.Name,
		Target:      "Charm",
		Start:       at,
		Expiry:      at + int64(t.pending.DurationSeconds(t.levelOrDefault())),
		Detrimental: true,
		Charm:       true,
	}
	t.pending = nil
}

func (t *Tracker) expireByMessageLocked(body string) {
	// a charm broke
	if strings.HasPrefix(body, "Your charm spell has worn off") {
		for k, tm := range t.timers {
			if tm.Charm {
				delete(t.timers, k)
			}
		}
	}

	// collect every timer whose spell's fade message ends this line
	var matched []string
	for k, tm := range t.timers {
		if s, ok := t.book.ByName(tm.Spell); ok && s.Fades != "" && strings.HasSuffix(body, s.Fades) {
			matched = append(matched, k)
		}
	}
	if len(matched) <= 1 {
		for _, k := range matched {
			delete(t.timers, k) // the common case: one timer owns this fade text
		}
		return
	}

	// the same fade text matches several timers — the same debuff up on multiple
	// mobs. EQ writes the line as "<target> <fades>", so clear only the timer
	// whose target prefixes the line (case-insensitively, since cast emotes
	// capitalize the leading name and fade lines may not). If none match — e.g. a
	// target-less self-buff fade — fall back to clearing all of them.
	lower := strings.ToLower(body)
	var cleared bool
	for _, k := range matched {
		if tm, ok := t.timers[k]; ok && strings.HasPrefix(lower, strings.ToLower(tm.Target)) {
			delete(t.timers, k)
			cleared = true
		}
	}
	if !cleared {
		for _, k := range matched {
			delete(t.timers, k)
		}
	}
}

func (t *Tracker) expireOnSlainLocked(body string) {
	var victim string
	switch {
	case strings.HasPrefix(body, "You have been slain by"):
		victim = "You" // you lose your buffs when you die
	case strings.HasPrefix(body, "You have slain "):
		victim = strings.TrimSuffix(strings.TrimPrefix(body, "You have slain "), "!")
	case strings.Contains(body, " has been slain by "):
		victim = body[:strings.Index(body, " has been slain by ")]
	default:
		return
	}
	for k, tm := range t.timers {
		// case-insensitive: EQ capitalizes the leading name in landing emotes
		// (timer target) but not in death lines (victim).
		if strings.EqualFold(tm.Target, victim) {
			delete(t.timers, k)
		}
	}
}

// Active returns the timers still running at now (unix seconds), soonest-to-
// expire first, and purges any that have lapsed.
func (t *Tracker) Active(now int64) []Timer {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Timer, 0, len(t.timers))
	for k, tm := range t.timers {
		if tm.Expiry <= now {
			delete(t.timers, k)
			continue
		}
		out = append(out, tm)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Expiry < out[j].Expiry })
	return out
}

// Clear drops all timers and pending state (used on a character switch).
func (t *Tracker) Clear() {
	t.mu.Lock()
	t.timers = make(map[string]Timer)
	t.cooldowns = make(map[string]int64)
	t.feignAttemptAt = 0
	t.feignFailAt = 0
	t.bindStartAt = 0
	t.bindDoneAt = 0
	t.zone = ""
	t.zoneRespawnSec = 0
	t.respawns = make(map[string]int64)
	t.pending = nil
	t.level = 0
	t.class = common.ClassUnknown
	t.mu.Unlock()
}

func (t *Tracker) levelOrDefault() int {
	if t.level > 0 {
		return t.level
	}
	return fallbackLevel
}

func key(spell, target string) string { return spell + "\x00" + target }
