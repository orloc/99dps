package gamestate

import (
	"sort"
	"strings"
	"sync"

	"99dps/internal/common"
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
	Mez         bool // a mez/enthrall — crowd control, breaks on damage
}

// Tracker holds the player's active spell timers. It's fed parsed log signals
// and is safe for concurrent use (the parser writes, the UI reads).
type Tracker struct {
	book *Book

	mu     sync.Mutex
	level  int
	class  common.Class
	timers map[string]Timer // key: spell\x00target

	cool cooldownTracker // activated-ability reuse, feign, bind (see cooldown.go)
	zone zoneTracker     // zone-awareness: current zone, repops, kills (see zone.go)

	canni canniMeter // shaman "canni dance" gamification (see canni.go)

	// pending cast awaiting its landing emote
	pending   *Spell
	pendingAt int64
}

// NewTracker builds a tracker over a loaded spell book.
func NewTracker(book *Book) *Tracker {
	return &Tracker{
		book:   book,
		timers: make(map[string]Timer),
		cool:   cooldownTracker{cooldowns: make(map[string]int64)},
	}
}

// inferClassLocked adopts a class detected by a subsystem, but only when one
// isn't already known (a /who title always wins). Caller holds the lock.
func (t *Tracker) inferClassLocked(c common.Class) {
	if c != common.ClassUnknown && t.class == common.ClassUnknown {
		t.class = c
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
	if strings.HasPrefix(spellName, "Cannibalize") {
		t.canni.recordCastLocked(t.book, spellName, at)
	}
}

// BreakMezOnTarget clears a target's mez timers — a mezzed mob that takes
// damage breaks early with no wear-off message, so the parser calls this on any
// damage to that target to keep the CC list honest.
func (t *Tracker) BreakMezOnTarget(target string) {
	if t == nil || target == "" {
		return
	}
	n := normalizeMobName(target)
	t.mu.Lock()
	for k, tm := range t.timers {
		// the mez emote capitalizes/strips the article ("Greater kobold") while a
		// damage line keeps it ("a greater kobold"), so compare normalized.
		if tm.Mez && normalizeMobName(tm.Target) == n {
			delete(t.timers, k)
		}
	}
	t.mu.Unlock()
}

// normalizeMobName lowercases and strips a leading article so a mez-landing name
// and a damage-line name for the same mob compare equal.
func normalizeMobName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, a := range []string{"a ", "an ", "the "} {
		if rest, ok := strings.CutPrefix(s, a); ok {
			return rest
		}
	}
	return s
}

// DismissTarget removes all of a target's active timers (manual cleanup of a
// raid-buff list — the timer reappears if the buff is re-cast).
func (t *Tracker) DismissTarget(target string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	for k, tm := range t.timers {
		if tm.Target == target {
			delete(t.timers, k)
		}
	}
	t.mu.Unlock()
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

	// One line can matter to several subsystems at once (a kill both expires the
	// victim's debuffs *and* records a repop), so this is a fan-out to every
	// matcher, not a mutually-exclusive dispatch. Each matcher is itself cheap:
	// it either early-returns on a prefix screen or is a no-op when its state is
	// empty (e.g. the timer scans below short-circuit when no timers are active).
	hadPending := t.pending != nil
	t.matchLandingLocked(body, at)
	// an instant clicky self-buff (Journeyman Boots etc.) emits no cast line, so
	// it never sets a pending cast — match its landing emote directly. Only when
	// nothing was pending, so a normal cast's landing isn't double-counted.
	if !hadPending {
		t.matchSelfClickyLocked(body, at)
	}
	t.expireByMessageLocked(body)
	t.expireOnSlainLocked(body)
	t.inferClassLocked(t.cool.matchLocked(body, at))
	t.zone.observeLocked(body, at)
	if body == "Spell recast time not yet met." {
		t.canni.recordBuzzerLocked()
	}
	t.cool.observeBindLocked(body, at)
}

// matchSelfClickyLocked starts a self-buff timer when a line is the landing
// emote of an instant clicky (no cast line preceded it). Caller holds the lock.
func (t *Tracker) matchSelfClickyLocked(body string, at int64) {
	s, ok := t.book.SelfClicky(body)
	if !ok {
		return
	}
	dur := s.DurationSeconds(t.levelOrDefault())
	if dur <= 0 {
		return
	}
	t.timers[key(s.Name, "You")] = Timer{
		Spell:       s.Name,
		Target:      "You",
		Start:       at,
		Expiry:      at + int64(dur),
		Detrimental: s.Detrimental,
	}
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
		Mez:         t.pending.Mez,
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
	if len(t.timers) == 0 {
		return // nothing to expire — skip the charm check and the per-timer scan
	}
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
	if len(t.timers) == 0 {
		return // only ever drops timers — nothing to do with none active
	}
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
	t.cool.clear()
	t.zone.clear()
	t.canni.clear()
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
