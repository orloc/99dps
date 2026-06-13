package gamestate

import (
	"99dps/internal/eqclass"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// fallbackLevel is used to compute durations before we've seen the player's
// real level in the log. It's the max classic level, so level-capped formulas
// resolve to the spell's cap (its full duration).
const fallbackLevel = 60

// landWindowSec is how long after a cast completes we'll still accept a landing
// emote as belonging to that cast.
const landWindowSec = 12

// clickyCastCeilSec floors the stale-pending window. Clicky items cast far
// longer (up to ~25s) than the underlying spell's own listed cast time, so a
// pending whose listed cast is short must not be dropped before a slow clicky's
// landing can arrive (e.g. White Lotus Pants → Spirit of Ox, ~20s).
const clickyCastCeilSec = 25

// Timer is one active spell the player has on a target.
type Timer struct {
	Spell       string
	Target      string
	Start       int64 // unix seconds (log time)
	Expiry      int64
	Detrimental bool
	Charm       bool // a charm — display elapsed (counts up), not remaining
	Mez         bool // a mez/enthrall — crowd control, breaks on damage
	Pacify      bool // a pacify/lull/calm — crowd control (aggro lowered)
	Estimated   bool // duration is a ceiling guess (an incoming debuff — caster level unknown)
}

// Tracker holds the player's active spell timers. It's fed parsed log signals
// and is safe for concurrent use (the parser writes, the UI reads).
type Tracker struct {
	book *Book

	mu     sync.Mutex
	level  int
	class  eqclass.Class
	timers map[string]Timer // key: spell\x00target

	cool cooldownTracker // activated-ability reuse, feign, bind (see cooldown.go)
	zone zoneTracker     // zone-awareness: current zone, repops, kills (see zone.go)

	canni canniMeter // shaman "canni dance" gamification (see canni.go)

	// pending cast awaiting its landing emote. Kept alive across landings (not
	// cleared on the first) so an AoE/PBAoE that lands on several mobs starts a
	// timer on each; the cast window (landWindowSec) expires it.
	pending   *Spell
	pendingAt int64

	// the most recent "Your target resisted the X spell." — surfaced briefly so
	// the UI can flag a cast that didn't land.
	resistSpell string
	resistAt    int64

	// character is the tracked player (set by the host), used to flag the player's
	// own pet. petName is the player's current pet; petOwners maps every seen pet
	// (lowercased name) to its owner, from "My leader is <Owner>." lines (see
	// pet.go) — for the whole group, so a pet's damage can be attributed correctly.
	character string
	petName   string
	petOwners map[string]string
}

// resistGraceSec is how long a resist notice stays shown after it lands.
const resistGraceSec = 5

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
func (t *Tracker) inferClassLocked(c eqclass.Class) {
	if c != eqclass.ClassUnknown && t.class == eqclass.ClassUnknown {
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
func (t *Tracker) SetClass(c eqclass.Class) {
	if t == nil || c == eqclass.ClassUnknown {
		return
	}
	t.mu.Lock()
	t.class = c
	t.mu.Unlock()
}

// Class returns the detected class, or ClassUnknown until a /who is seen.
func (t *Tracker) Class() eqclass.Class {
	if t == nil {
		return eqclass.ClassUnknown
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
func (t *Tracker) Category() eqclass.Category {
	return eqclass.CategoryOf(t.Class())
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

// BreakCCOnTarget clears a target's mez and pacify timers — both break the
// instant the mob takes damage (it re-aggros), with no wear-off message, so the
// parser calls this on any damage to that target to keep the CC list honest.
func (t *Tracker) BreakCCOnTarget(target string) {
	if t == nil || target == "" {
		return
	}
	n := normalizeMobName(target)
	t.mu.Lock()
	for k, tm := range t.timers {
		// the landing emote capitalizes/strips the article ("Greater kobold") while
		// a damage line keeps it ("a greater kobold"), so compare normalized.
		if (tm.Mez || tm.Pacify) && normalizeMobName(tm.Target) == n {
			delete(t.timers, k)
		}
	}
	t.mu.Unlock()
}

// Resisted returns the spell name of the most recent target-resisted cast and
// whether it's recent enough to still surface (within resistGraceSec).
func (t *Tracker) Resisted(now int64) (string, bool) {
	if t == nil {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.resistSpell != "" && now-t.resistAt >= 0 && now-t.resistAt <= resistGraceSec {
		return t.resistSpell, true
	}
	return "", false
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

	// a fizzled or interrupted cast never happened — drop the pending cast.
	if t.pending != nil && strings.HasPrefix(body, "Your ") &&
		(strings.Contains(body, "fizzle") || strings.Contains(body, "interrupt")) {
		t.pending = nil
	}
	// "Your target resisted the X spell." — the cast didn't land on that mob.
	// Recorded (not cleared) so the rest of an AoE can still land on others.
	if s, ok := strings.CutPrefix(body, "Your target resisted the "); ok {
		if name, ok := strings.CutSuffix(s, " spell."); ok {
			t.resistSpell, t.resistAt = name, at
		}
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
		// registered insta-clickies whose effect emote isn't in spells_us (or that
		// emit no spell message at all) — matched by their item-specific line.
		t.matchClickyLocked(body, at)
	}
	// a hostile debuff landing on you (cast_on_you emote) — caster/level unknown,
	// so it's an estimated ceiling cleared early by its wear-off (fade) line. The
	// emote can't collide with a self-cast (these are detrimental phrasings), so
	// it's matched regardless of any pending cast.
	t.matchIncomingDebuffLocked(body, at)
	t.expireIncomingByFadeLocked(body)
	t.observePetLocked(body)
	t.expireByMessageLocked(body)
	t.expireOnSlainLocked(body)
	t.inferClassLocked(t.cool.matchLocked(body, at, t.class))
	t.zone.observeLocked(body, at, t.petName)
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
	if at*1000 < t.pendingAt*1000+int64(t.pending.CastTimeMs)-600 {
		return // a landing can't arrive before the cast bar finishes
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
		// Not this spell's landing. Drop the pending only once we're past the latest
		// a landing could plausibly arrive, so a much-later unrelated emote can't
		// match it — but a MATCHING landing above is honored regardless of how late.
		// Clicky items cast far longer (up to ~25s) than the underlying spell's
		// listed cast time, so floor the window: otherwise a slow clicky's landing
		// (White Lotus Pants → Spirit of Ox lands ~20s after the cast) is dropped as
		// stale before it ever arrives.
		castSec := max(int64(t.pending.CastTimeMs)/1000, clickyCastCeilSec)
		if at > t.pendingAt+castSec+landWindowSec {
			t.pending = nil
		}
		return
	}
	if target == "" {
		return
	}

	dur := t.pending.DurationSeconds(t.levelOrDefault())
	if t.pending.Pacify {
		dur = t.pending.PacifyDurationSeconds(t.levelOrDefault()) // P99 calm/pacify/wake times
	}
	if dur <= 0 {
		t.pending = nil // instant spell — nothing to time
		return
	}
	t.addOrRefreshTimerLocked(Timer{
		Spell:       t.pending.Name,
		Target:      target,
		Start:       at,
		Expiry:      at + int64(dur),
		Detrimental: t.pending.Detrimental,
		Mez:         t.pending.Mez,
		Pacify:      t.pending.Pacify,
	}, at)
	// pending is left set on purpose: an AoE lands on several mobs, each with its
	// own emote line, so we keep matching until the cast window (landWindowSec)
	// expires the pending cast or a new cast supersedes it. (Same-named mobs in
	// that AoE each get their own instance via addOrRefreshTimerLocked.)
}

// addOrRefreshTimerLocked stores a freshly-landed timer, deciding whether it
// refreshes an existing same-named copy or is a distinct same-named mob. A
// beneficial buff lands on a unique target (you or a named ally), so a re-cast
// is ALWAYS a refresh. Only a detrimental spell on a same-named mob can be a
// *second* mob — and only when the existing copy isn't already expiring (the red
// zone, via the shared TimerUrgency): see the panel, refresh what's red, treat a
// still-healthy copy as another mob. The new instance gets a unique key (the
// Target stays the plain name, so grouping/break/slay/dismiss are unaffected).
// This mirrors the Mob Tracker, which keeps one repop entry per kill. Caller
// holds the lock.
func (t *Tracker) addOrRefreshTimerLocked(tm Timer, at int64) {
	soonestKey, soonestExp := "", int64(0)
	for k, ex := range t.timers {
		if ex.Spell == tm.Spell && ex.Target == tm.Target {
			if soonestKey == "" || ex.Expiry < soonestExp {
				soonestKey, soonestExp = k, ex.Expiry
			}
		}
	}
	if soonestKey != "" {
		ex := t.timers[soonestKey]
		if !tm.Detrimental || TimerUrgency(ex.Expiry-at, ex.Expiry-ex.Start) == Expiring {
			t.timers[soonestKey] = tm // a buff (unique target) or a stale (red) debuff → refresh
			return
		}
	}
	t.timers[t.uniqueKeyLocked(tm.Spell, tm.Target)] = tm // fresh copy still up → a different same-named mob
}

// uniqueKeyLocked returns key(spell,target), suffixed with an instance number
// when that base key is already taken, so several same-named mobs can each hold
// a copy of the same spell. Caller holds the lock.
func (t *Tracker) uniqueKeyLocked(spell, target string) string {
	base := key(spell, target)
	if _, taken := t.timers[base]; !taken {
		return base
	}
	for i := 2; ; i++ {
		k := base + "\x00" + strconv.Itoa(i)
		if _, taken := t.timers[k]; !taken {
			return k
		}
	}
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

// expireSoonestLocked removes the soonest-to-expire active timer for spell (a
// single instance), if any. Caller holds the lock.
func (t *Tracker) expireSoonestLocked(spell string) {
	bestK := ""
	var bestE int64
	for k, tm := range t.timers {
		if tm.Spell == spell && (bestK == "" || tm.Expiry < bestE) {
			bestK, bestE = k, tm.Expiry
		}
	}
	if bestK != "" {
		delete(t.timers, bestK)
	}
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

	// "Your <Spell> spell has worn off." — the caster-side end message. It fires on
	// an EARLY break (a root the mob shakes off, a debuff that drops) as well as a
	// full-duration expiry, and is the most reliable signal that one of your spells
	// ended. It names no target, so clear the soonest-expiring instance of that
	// spell (one wear-off line = one instance ended).
	if rest, ok := strings.CutPrefix(body, "Your "); ok {
		if i := strings.Index(rest, " spell has worn off"); i > 0 {
			t.expireSoonestLocked(rest[:i])
		}
	}

	// collect every timer whose spell's fade message ends this line
	var matched []string
	for k, tm := range t.timers {
		if s, ok := t.book.ByName(tm.Spell); ok && s.Fades != "" && strings.HasSuffix(body, s.Fades) {
			matched = append(matched, k)
		}
	}
	// One fade line = ONE mob's debuff ending, so clear a single instance — never
	// every same-named copy (two "a sand giant" each with Malosini: one fading must
	// leave the other's timer running). EQ writes "<target> <fades>", so prefer the
	// copies whose target prefixes the line (case-insensitively — cast emotes
	// capitalize the leading name, fade lines may not); among those drop only the
	// soonest-to-expire. Fall back to all matched when none prefix (a target-less
	// self-buff fade, where there are no same-named duplicates to confuse anyway).
	lower := strings.ToLower(body)
	var prefixed []string
	for _, k := range matched {
		if strings.HasPrefix(lower, strings.ToLower(t.timers[k].Target)) {
			prefixed = append(prefixed, k)
		}
	}
	cands := prefixed
	if len(cands) == 0 {
		cands = matched
	}
	soonestK, soonestE := "", int64(0)
	for _, k := range cands {
		if tm := t.timers[k]; soonestK == "" || tm.Expiry < soonestE {
			soonestK, soonestE = k, tm.Expiry
		}
	}
	if soonestK != "" {
		delete(t.timers, soonestK)
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
		// "<victim> has been slain by <killer>!" — require a non-empty victim
		// before the phrase (matching the kill path), so a relayed chat line that
		// merely contains the words can't yield an empty/garbage victim.
		if i := strings.Index(body, " has been slain by "); i > 0 {
			victim = body[:i]
		} else {
			return
		}
	default:
		return
	}
	// one death clears one mob's worth of debuffs. With several same-named mobs
	// each holding a copy of a spell, drop the soonest instance of each spell on
	// the victim — not every copy — so the surviving same-named mobs keep theirs.
	// (For "You", each buff is unique, so this clears all your buffs as before.)
	soonestKey := map[string]string{}
	soonestExp := map[string]int64{}
	for k, tm := range t.timers {
		// case-insensitive: EQ capitalizes the leading name in landing emotes
		// (timer target) but not in death lines (victim).
		if !strings.EqualFold(tm.Target, victim) {
			continue
		}
		if _, seen := soonestKey[tm.Spell]; !seen || tm.Expiry < soonestExp[tm.Spell] {
			soonestKey[tm.Spell] = k
			soonestExp[tm.Spell] = tm.Expiry
		}
	}
	for _, k := range soonestKey {
		delete(t.timers, k)
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
	t.class = eqclass.ClassUnknown
	t.petName = ""
	t.petOwners = nil
	t.mu.Unlock()
}

func (t *Tracker) levelOrDefault() int {
	if t.level > 0 {
		return t.level
	}
	return fallbackLevel
}

func key(spell, target string) string { return spell + "\x00" + target }
