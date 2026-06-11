package parser

import (
	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hpcloud/tail"
)

// Sink receives the events parsed from the log. *session.SessionManager
// satisfies it; depending on this interface rather than the concrete manager
// keeps the parser free of the storage layer and unit-testable with a fake.
type Sink interface {
	Apply(*combat.DamageSet)
	ApplySwing(*combat.Swing)
	ApplyCrit(*combat.Crit)
	ApplyEvent(*combat.Event)
	ApplyMagic(*combat.Magic)
}

// SpellObserver is the parser's view of the live game-state tracker: the spell,
// cast, level/class, feign, and mez signals it feeds. Like Sink, depending on
// this interface (rather than *gamestate.Tracker) keeps the parser off the
// gamestate package and testable with a fake. *gamestate.Tracker satisfies it.
type SpellObserver interface {
	Observe(body string, at int64)
	BeginCast(spellName string, at int64)
	BreakCCOnTarget(target string)
	SetLevel(level int)
	SetClass(c eqclass.Class)
	FeignAttempt(at int64)
	FeignFailed(at int64)
}

type DmgParser struct {
	// character is the log owner's name. Their own crits are logged under that
	// name rather than "You", so we remap it for attribution.
	character string
	// tracker, when non-nil, receives cast/level/landing signals for the spell
	// timer overlay.
	tracker SpellObserver
}

const COMBAT_VERB_STRING = "gores|gore|claws|claw|punches|punch|kicks|kick|bites|bite|mauls|maul|slashes|slash|slices|slice|strikes|strike|stings|sting|pierces|pierce|bashes|bash|hits|hit|backstabs|backstab|crushes|crush"

const LOG_TS_INDEX_END = 25
const LOG_SUBJECT_INDEX_START = 27

// DoParse tails the log, classifying each line and forwarding the parsed event
// to sink. tracker (optional) receives spell-timer signals. It blocks until the
// tail's line channel closes.
func DoParse(t *tail.Tail, sink Sink, character string, tracker SpellObserver) {
	p := DmgParser{character: character, tracker: tracker}
	for line := range t.Lines {
		// EQ writes CRLF; strip the trailing \r so exact/suffix matches (spell
		// landing emotes, wear-offs) aren't thrown off by it.
		text := strings.TrimRight(line.Text, "\r\n")
		p.dispatch(text, sink)
		if p.tracker != nil {
			p.observeSpells(text)
		}
	}
}

// RebuildTrackerFromFile replays a log file's spell/zone/class signals into the
// tracker only (no Sink, so no session side-effects). Used on a character switch
// to recover the new character's active spell timers, class/level, and zone
// instead of starting blank — the live tail still follows from end-of-file.
// Already-expired timers fall out via tracker.Active. Cheap because logs are
// rotated small.
func RebuildTrackerFromFile(path, character string, tracker SpellObserver) {
	if tracker == nil {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	p := DmgParser{character: character, tracker: tracker}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		p.observeSpells(strings.TrimRight(sc.Text(), "\r\n"))
	}
}

var (
	// whoSelfRe captures level (1), level-title (2), and name (3) from a /who
	// self line: "[60 Warlord] Stelzar (Troll)". The title maps to a eqclass.
	whoSelfRe  = regexp.MustCompile(`^\[(\d+) ([^\]]+)\] (\S+)`)
	welcomeRe  = regexp.MustCompile(`Welcome to level (\d+)`)
	castPrefix = "You begin casting "
)

// observeSpells feeds one log line to the spell-timer tracker: caster level
// (from /who self or a level-up), cast starts, and everything else (for landing
// emotes, wear-offs, resists, and target deaths).
func (p *DmgParser) observeSpells(line string) {
	if len(line) < LOG_SUBJECT_INDEX_START {
		return
	}
	ts, err := parseTimestamp(line)
	if err != nil {
		return
	}
	body := strings.TrimRight(line[LOG_SUBJECT_INDEX_START:], "\r\n")

	if lvl, cls, ok := p.parseLevel(body); ok {
		p.tracker.SetLevel(lvl)
		p.tracker.SetClass(cls) // no-op for ClassUnknown (e.g. a level-up line)
		return
	}
	if p.isFeignMacro(body) {
		p.tracker.FeignAttempt(ts) // the FD attempt; a fail line (if any) follows
		return
	}
	if p.isOwnFeignFail(body) {
		p.tracker.FeignFailed(ts)
		return
	}
	if strings.HasPrefix(body, castPrefix) {
		p.tracker.BeginCast(strings.TrimSuffix(strings.TrimSpace(body[len(castPrefix):]), "."), ts)
		return
	}
	p.tracker.Observe(body, ts)
}

// feignMacroPhrase is the distinctive text in the player's custom feign-death
// macro emote ("<character> looks dead..."). Change it here if the macro text
// changes; it's how we detect a feign *attempt* (success has no message).
const feignMacroPhrase = "looks dead"

// isFeignMacro reports whether a line is the player's own feign-death macro —
// their character name followed by the macro phrase. Gating on the name avoids
// tripping on another monk's emote.
func (p *DmgParser) isFeignMacro(body string) bool {
	return p.character != "" &&
		strings.HasPrefix(body, p.character) &&
		strings.Contains(body, feignMacroPhrase)
}

// isOwnFeignFail reports whether a line is the *player's* failed feign
// ("<you> have/has fallen to the ground"). EQ logs this with the player's own
// name, and other monks in the zone log the same line, so it's gated to the
// player to avoid false alerts.
func (p *DmgParser) isOwnFeignFail(body string) bool {
	if !strings.Contains(body, "fallen to the ground") {
		return false
	}
	return strings.HasPrefix(body, "You ") ||
		(p.character != "" && strings.HasPrefix(body, p.character+" "))
}

// parseLevel reads the player's level (and class, when available) from a /who
// line naming the log owner (e.g. "[34 Wizard] Kelkix (Gnome)" → 34, Wizard) or
// a level-up ("Welcome to level 43!" → 43, ClassUnknown — a level-up names no
// class). The class is derived from the /who level-title.
func (p *DmgParser) parseLevel(body string) (int, eqclass.Class, bool) {
	if strings.HasPrefix(body, "[") && p.character != "" {
		if m := whoSelfRe.FindStringSubmatch(body); m != nil && m[3] == p.character {
			n, _ := strconv.Atoi(m[1])
			return n, eqclass.ClassFromTitle(m[2]), true
		}
	}
	if strings.Contains(body, "Welcome to level") {
		if m := welcomeRe.FindStringSubmatch(body); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n, eqclass.ClassUnknown, true
		}
	}
	return 0, eqclass.ClassUnknown, false
}

// dispatch routes a single log line to the matching parser and sink method.
// Unparseable lines for a matched category are silently dropped.
func (p *DmgParser) dispatch(line string, sink Sink) {
	switch {
	case p.hasDamage(line):
		if set, err := p.parseDamage(line); err == nil {
			sink.Apply(set)
			if p.tracker != nil {
				p.tracker.BreakCCOnTarget(set.Target) // damage breaks a mob's mez
			}
		}
	case p.hasSwing(line):
		if sw, err := p.parseSwing(line); err == nil {
			sink.ApplySwing(sw)
		}
	case p.hasCrit(line):
		if cr, err := p.parseCrit(line); err == nil {
			sink.ApplyCrit(cr)
		}
	case p.hasMagic(line):
		if m, err := p.parseMagic(line); err == nil {
			sink.ApplyMagic(m)
			if p.tracker != nil {
				p.tracker.BreakCCOnTarget(m.Target) // non-melee damage breaks mez too
			}
		}
	case p.hasEvent(line):
		if ev, err := p.parseEvent(line); err == nil {
			sink.ApplyEvent(ev)
		}
	}
}

// swingPattern captures an attempted melee swing that did not land:
//
//	<attacker> tries to <verb> <defender>, but <outcome>
//
// "You" uses "try to" instead of "tries to", hence the optional branch. The
// verb itself is irrelevant here — the defender is whatever sits between the
// verb and the comma, and the outcome is the tail.
var swingPattern = regexp.MustCompile(`^(.+?) tr(?:y|ies) to (\S+) (.+?), but (.+)$`)

// hasSwing is a cheap screen for swing-attempt lines before the regex runs.
func (p *DmgParser) hasSwing(input string) bool {
	return strings.Contains(input, ", but ") &&
		(strings.Contains(input, " tries to ") || strings.Contains(input, " try to "))
}

func (p *DmgParser) parseSwing(input string) (*combat.Swing, error) {
	if len(input) < LOG_SUBJECT_INDEX_START {
		return nil, fmt.Errorf("line too short for a swing")
	}

	ts, err := parseTimestamp(input)
	if err != nil {
		return nil, err
	}

	m := swingPattern.FindStringSubmatch(input[LOG_SUBJECT_INDEX_START:])
	if m == nil {
		return nil, fmt.Errorf("not a swing line: %q", input)
	}

	outcome, ok := classifyOutcome(m[4])
	if !ok {
		return nil, fmt.Errorf("unrecognised swing outcome: %q", m[4])
	}

	return &combat.Swing{
		ActionTime: ts,
		Attacker:   strings.TrimSpace(m[1]),
		Verb:       m[2],
		Defender:   normalizeName(strings.TrimSpace(m[3])),
		Outcome:    outcome,
	}, nil
}

// classifyOutcome maps the "but …" tail to a SwingOutcome. The second return is
// false when the tail isn't a known avoidance phrase (so non-combat "tries to"
// lines are ignored).
func classifyOutcome(tail string) (combat.SwingOutcome, bool) {
	switch {
	case strings.Contains(tail, "miss"):
		return combat.OutcomeMiss, true
	case strings.Contains(tail, "dodge"):
		return combat.OutcomeDodge, true
	case strings.Contains(tail, "parr"): // parry / parries
		return combat.OutcomeParry, true
	case strings.Contains(tail, "block"):
		return combat.OutcomeBlock, true
	case strings.Contains(tail, "ripost"): // riposte / ripostes
		return combat.OutcomeRiposte, true
	case strings.Contains(tail, "magical skin absorbs"):
		return combat.OutcomeAbsorb, true
	}
	return 0, false
}

// normalizeName collapses the player's defender token to "YOU", matching the
// normalization normalizeTarget applies to damage lines so a combatant keys the
// same whether they were hit or they avoided.
func normalizeName(s string) string {
	if strings.Contains(s, "YOU") {
		return "YOU"
	}
	return s
}

func (p *DmgParser) hasDamage(inputString string) bool {
	// A melee line carries "points of damage". We must exclude two look-alikes:
	// incoming spell damage on the player ("You have taken N points of damage")
	// and non-melee/spell damage on a target ("X was hit by non-melee for N
	// points of damage") — the latter would otherwise be mis-parsed by the melee
	// regex into a bogus "<X> was" dealer. Non-melee is routed to hasMagic.
	return strings.Contains(inputString, "points of damage") &&
		!strings.Contains(inputString, "You have taken") &&
		!strings.Contains(inputString, "non-melee")
}

// damageLineRe captures a melee damage line in one shot:
//
//	<dealer> <verb> <target> for <N> points of damage
//
// dealer is non-greedy so it stops at the first space-delimited combat verb;
// because the verb must be flanked by spaces, a dealer name containing a verb
// substring (e.g. "a slashing terror") isn't mistaken for the verb.
var damageLineRe = regexp.MustCompile(`^(.+?) (` + COMBAT_VERB_STRING + `) (.+?) for (\d+) points of damage`)

func (p *DmgParser) parseDamage(input string) (*combat.DamageSet, error) {
	if len(input) < LOG_SUBJECT_INDEX_START {
		return nil, fmt.Errorf("line too short for a damage event")
	}

	ts, err := parseTimestamp(input)
	if err != nil {
		return nil, err
	}

	m := damageLineRe.FindStringSubmatch(input[LOG_SUBJECT_INDEX_START:])
	if m == nil {
		return nil, fmt.Errorf("not a damage line: %q", input)
	}

	dmg, err := strconv.Atoi(m[4])
	if err != nil {
		return nil, err
	}

	return &combat.DamageSet{
		ActionTime: ts,
		Dealer:     strings.TrimSpace(m[1]),
		Verb:       m[2],
		Target:     normalizeTarget(m[3]),
		Dmg:        dmg,
	}, nil
}

// normalizeTarget collapses the player and spell-damage tokens so a combatant
// keys consistently regardless of phrasing.
func normalizeTarget(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.Contains(s, "YOU"):
		return "YOU"
	case strings.Contains(s, "non-melee"):
		return "non-melee"
	}
	return s
}

// parseTimestamp reads the bracketed time.ANSIC stamp at the head of a log
// line. EQ writes these in the player's LOCAL time with no zone, so we parse in
// time.Local — otherwise the resulting unix time is off by the UTC offset, which
// makes wall-clock comparisons (spell timers) and displayed session times wrong.
func parseTimestamp(input string) (int64, error) {
	t, err := time.ParseInLocation(time.ANSIC, input[1:LOG_TS_INDEX_END], time.Local)
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

// critPattern captures "<attacker> Scores a critical hit!(<dmg>)". "score"
// (singular) covers the player's own crit phrasing.
var critPattern = regexp.MustCompile(`^(.+?) [Ss]cores? a critical hit!\((\d+)\)`)

func (p *DmgParser) hasCrit(input string) bool {
	return strings.Contains(input, "critical hit!(")
}

func (p *DmgParser) parseCrit(input string) (*combat.Crit, error) {
	if len(input) < LOG_SUBJECT_INDEX_START {
		return nil, fmt.Errorf("line too short for a crit")
	}

	ts, err := parseTimestamp(input)
	if err != nil {
		return nil, err
	}

	m := critPattern.FindStringSubmatch(input[LOG_SUBJECT_INDEX_START:])
	if m == nil {
		return nil, fmt.Errorf("not a crit line: %q", input)
	}

	dmg, err := strconv.Atoi(m[2])
	if err != nil {
		return nil, err
	}

	return &combat.Crit{
		ActionTime: ts,
		Attacker:   p.normalizeAttacker(strings.TrimSpace(m[1])),
		Damage:     dmg,
	}, nil
}

// normalizeAttacker maps the log owner's own name (and a literal "You") to the
// "You" key used for the player's damage, so their crits attribute correctly.
func (p *DmgParser) normalizeAttacker(name string) string {
	if name == "You" || (p.character != "" && name == p.character) {
		return "You"
	}
	return name
}

func (p *DmgParser) hasEvent(input string) bool {
	return strings.Contains(input, "have slain ") ||
		strings.Contains(input, "have been slain by ") ||
		strings.Contains(input, "gain experience") ||
		strings.Contains(input, "gain party experience") ||
		strings.Contains(input, "You have entered ") ||
		strings.Contains(input, "to prepare your camp")
}

var slainPattern = regexp.MustCompile(`^You have slain (.+)!`)

func (p *DmgParser) parseEvent(input string) (*combat.Event, error) {
	if len(input) < LOG_SUBJECT_INDEX_START {
		return nil, fmt.Errorf("line too short for an event")
	}

	ts, err := parseTimestamp(input)
	if err != nil {
		return nil, err
	}
	body := input[LOG_SUBJECT_INDEX_START:]

	switch {
	case strings.HasPrefix(body, "You have been slain by"):
		return &combat.Event{ActionTime: ts, Kind: combat.EventDeath}, nil
	case strings.HasPrefix(body, "You have entered ") || strings.Contains(body, "to prepare your camp"):
		return &combat.Event{ActionTime: ts, Kind: combat.EventZone}, nil
	case strings.HasPrefix(body, "You have slain "):
		name := ""
		if m := slainPattern.FindStringSubmatch(body); m != nil {
			name = m[1]
		}
		return &combat.Event{ActionTime: ts, Kind: combat.EventKill, Name: name}, nil
	case strings.HasPrefix(body, "You gain party experience"):
		return &combat.Event{ActionTime: ts, Kind: combat.EventPartyXP}, nil
	case strings.HasPrefix(body, "You gain experience"):
		return &combat.Event{ActionTime: ts, Kind: combat.EventXP}, nil
	}

	return nil, fmt.Errorf("not a bookkeeping event: %q", input)
}

// magicPattern captures non-melee (spell/proc/DoT) damage on a target. EQ logs
// these in passive voice, so there is no caster to capture.
var magicPattern = regexp.MustCompile(`^(.+?) was hit by non-melee for (\d+) points of damage`)

func (p *DmgParser) hasMagic(input string) bool {
	return strings.Contains(input, "was hit by non-melee for ") &&
		strings.Contains(input, "points of damage")
}

func (p *DmgParser) parseMagic(input string) (*combat.Magic, error) {
	if len(input) < LOG_SUBJECT_INDEX_START {
		return nil, fmt.Errorf("line too short for a magic event")
	}
	ts, err := parseTimestamp(input)
	if err != nil {
		return nil, err
	}
	m := magicPattern.FindStringSubmatch(input[LOG_SUBJECT_INDEX_START:])
	if m == nil {
		return nil, fmt.Errorf("not a magic line: %q", input)
	}
	dmg, err := strconv.Atoi(m[2])
	if err != nil {
		return nil, err
	}
	return &combat.Magic{
		ActionTime: ts,
		Target:     normalizeName(strings.TrimSpace(m[1])),
		Dmg:        dmg,
	}, nil
}
