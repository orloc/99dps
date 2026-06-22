// Package gamestate derives and holds the live, non-DPS game state read from the
// log: the spell Book (parsed from spells_us.txt) and the central Tracker — spell
// timers, zone/repop tracking, cooldowns, pet/group attribution, incoming
// debuffs, and the detected class/level. The Tracker is fed one log line at a
// time via Observe and read lock-free by the UI.
//
// This file (book.go) is the spell Book: it parses EverQuest's spells_us.txt into
// a lookup table and computes durations for spells the player casts (debuffs on
// others, self/other buffs). The data is the canonical client SPDat_Spell_Struct
// (217 ^-delimited fields); only the handful we need are decoded.
package gamestate

import (
	"io"
	"os"
	"strconv"
	"strings"

	"99dps/internal/eq/eqclass"
)

// field indices in spells_us.txt (0-based)
const (
	fName        = 1
	fCastOnYou   = 6  // emote when the spell lands on you
	fCastOnOther = 7  // emote when it lands on someone else (prefix = target)
	fFades       = 8  // wear-off message
	fCastTime    = 13 // cast time, ms
	fRecastTime  = 15 // recast time, ms (the spell's own reuse gate)
	fDurFormula  = 16 // buffdurationformula
	fDurCap      = 17 // buffduration (tick cap)
	fGoodEffect  = 83 // 0 = detrimental, 1/2 = beneficial
	minFields    = 84 // we read up to index 83
)

// numSpellClasses is the count of player classes in spells_us.txt's per-class
// min-cast-level block (a 16-wide run: each entry is the level that class can
// first cast the spell, or 254/255 = never). The block's OFFSET differs by client
// version (Titanium vs later clients add fields), so it is auto-calibrated at load
// rather than hardcoded — see calibrateClassOffset. The class ORDER within the
// block is the canonical EQ one and is stable across clients.
const numSpellClasses = 16

// spellClassIndex maps an eqclass to its column in the per-class block, in the
// canonical EQ order. Berserker (index 15) has no eqclass (outside P99's era).
var spellClassIndex = map[eqclass.Class]int{
	eqclass.ClassWarrior: 0, eqclass.ClassCleric: 1, eqclass.ClassPaladin: 2, eqclass.ClassRanger: 3,
	eqclass.ClassShadowKnight: 4, eqclass.ClassDruid: 5, eqclass.ClassMonk: 6, eqclass.ClassBard: 7,
	eqclass.ClassRogue: 8, eqclass.ClassShaman: 9, eqclass.ClassNecromancer: 10, eqclass.ClassWizard: 11,
	eqclass.ClassMagician: 12, eqclass.ClassEnchanter: 13, eqclass.ClassBeastlord: 14,
}

// genericBuffEmotes are cast-on-you lines for beneficial effects other players
// commonly put on you (e.g. group regen). They are NOT click-items, many real
// spells share each line, and we can't time an unknown caster's buff honestly —
// so they must not be indexed as self-clickies, where a stray mislabeled instant
// entry would otherwise claim them (the real spells_us "Aura of Battle" #1499 is
// an instant buff whose emote is "You begin to regenerate.", which made any group
// regen show up as Aura of Battle).
var genericBuffEmotes = map[string]bool{
	"You begin to regenerate.": true,
}

// nonPlayerSpell reports a spell players can't cast — the NPC/GM variants, by the
// spells_us naming convention (e.g. "NPCAegolism", "GM SoW"). Their often-instant
// copies must not be indexed as self-clickies, where they'd hijack a real buff's
// shared landing emote.
func nonPlayerSpell(name string) bool {
	return strings.HasPrefix(name, "NPC") || strings.HasPrefix(name, "GM ")
}

// Spell is the decoded subset of one spells_us.txt row.
type Spell struct {
	Name        string
	CastTimeMs  int
	RecastMs    int
	DurFormula  int
	DurCap      int
	CastOnYou   string
	CastOnOther string
	Fades       string
	Detrimental bool
	Charm       bool // a charm spell — no landing emote, breaks unpredictably
	Mez         bool // a mez (mesmerize/enthrall) — crowd control, breaks on damage
	Pacify      bool // a pacify/lull/calm — lowers aggro ("looks less aggressive")

	// classLevels is the per-class min cast level (canonical EQ class order; 254/255
	// = the class can't cast it). nil when the class block couldn't be calibrated
	// for this file — in which case castability is treated as UNKNOWN (assume yes),
	// preserving the pre-calibration behavior.
	classLevels []int
}

// playerCastable reports whether any player class can cast the spell. With no
// class data (calibration failed) it returns true — unknown, so don't filter.
func (s *Spell) playerCastable() bool {
	if s.classLevels == nil {
		return true
	}
	for _, lv := range s.classLevels {
		if lv >= 1 && lv <= 253 {
			return true
		}
	}
	return false
}

// minCastLevel is the lowest level any class can cast the spell (the base/classic
// version), or a large sentinel when none/unknown — used to pick the canonical
// row among duplicate names (the real player spell over a high-level reprint).
func (s *Spell) minCastLevel() int {
	m := 999
	for _, lv := range s.classLevels {
		if lv >= 1 && lv <= 253 && lv < m {
			m = lv
		}
	}
	return m
}

// castableBy returns the level a given class can first cast the spell, and whether
// it can at all. Always false when class data is absent.
func (s *Spell) castableBy(classIdx int) (int, bool) {
	if classIdx < 0 || classIdx >= len(s.classLevels) {
		return 0, false
	}
	if lv := s.classLevels[classIdx]; lv >= 1 && lv <= 253 {
		return lv, true
	}
	return 0, false
}

// DurationSeconds returns the spell's duration at the given caster level, using
// EQ's classic buffdurationformula (ticks are 6s). 0 means instant (no timer).
func (s *Spell) DurationSeconds(level int) int {
	dc := s.DurCap
	var ticks int
	switch s.DurFormula {
	case 0:
		ticks = 0
	case 1, 6:
		ticks = min(ceilDiv(level, 2), dc) // ceil(level/2)
	case 2:
		ticks = min(ceilDiv(level*3, 5), dc) // ceil(level/5*3)
	case 3:
		ticks = min(level*30, dc)
	case 4:
		if dc == 0 {
			ticks = 50
		} else {
			ticks = dc
		}
	case 5:
		ticks = dc
		if ticks == 0 {
			ticks = 1
		}
	case 7:
		ticks = min(level, dc)
	case 8:
		ticks = min(level+10, dc)
	case 9:
		ticks = min(level*2+10, dc)
	case 10:
		ticks = min(level*3+10, dc)
	case 11, 12, 15:
		ticks = dc
	case 50:
		ticks = 72000
	case 3600:
		if dc == 0 {
			ticks = 3600
		} else {
			ticks = dc
		}
	default:
		ticks = dc
	}
	if ticks <= 0 {
		return 0
	}
	// EQ grants a buff the remainder of the server tick it's cast in, so the
	// in-game duration runs one tick longer than the raw formula — e.g. a cap-14
	// debuff like Curse of the Spirits lasts 15 ticks (1:30), not 14 (1:24). Add
	// that tick so the countdown matches the in-game buff window.
	return (ticks + 1) * 6
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// PacifyDurationSeconds is the P99 lull/calm/pacify duration: DurCap ticks × 6,
// with NO extra cast-tick (unlike buffs) — so Calm (cap 30) = 180s and Pacify
// (cap 35) = 210s (3.5 min), matching the P99 wiki. Wake of Tranquility is
// uncapped (DurCap 0), running the formula-8 level+10 ticks (≈ 7 min at 60).
func (s Spell) PacifyDurationSeconds(level int) int {
	ticks := s.DurCap
	if ticks == 0 {
		ticks = level + 10 // uncapped (Wake of Tranquility) — the formula-8 base
	} else {
		ticks = min(level+10, ticks) // honor the cap if the caster is low level
	}
	if ticks <= 0 {
		return 0
	}
	return ticks * 6
}

// Book is a spell lookup keyed by name.
type Book struct {
	// byName is the CANONICAL spell per name: among duplicate-named rows (the file
	// has thousands), the player-castable one is preferred over NPC-only copies, and
	// the lowest-level (classic) variant over a high-level reprint. This fixes the
	// old last-row-wins bug where an NPC/later-era copy could shadow the real spell.
	byName map[string]*Spell
	// byNameAll keeps every same-named variant, so a cast can be resolved to the one
	// the player's CLASS can actually cast (ByNameForClass) when the canonical pick
	// isn't theirs.
	byNameAll map[string][]*Spell
	// byEmote maps an instant self-buff's landing emote (cast_on_you) to its
	// spell, so clickies that emit no "You begin casting" line (Journeyman Boots
	// etc.) are still trackable.
	byEmote map[string]*Spell
	// classKnown is true when the per-class block was calibrated for this file, so
	// castability filtering/selection is active. False → behaves as before (no
	// filtering), so a custom/unrecognized file never regresses.
	classKnown bool
}

// ByName returns the canonical spell with the given name, if known.
func (b *Book) ByName(name string) (*Spell, bool) {
	s, ok := b.byName[name]
	return s, ok
}

// ByNameForClass resolves a spell name to the variant the given class can cast,
// preferring the lowest-level one (the version they actually learned). It falls
// back to the canonical ByName pick when the class can't cast any same-named
// variant, when the class is unknown, or when class data is absent — so it's
// always at least as good as ByName, never worse.
func (b *Book) ByNameForClass(name string, c eqclass.Class) (*Spell, bool) {
	if b.classKnown {
		if idx, ok := spellClassIndex[c]; ok {
			var best *Spell
			bestLv := 999
			for _, s := range b.byNameAll[name] {
				if lv, ok := s.castableBy(idx); ok && lv < bestLv {
					best, bestLv = s, lv
				}
			}
			if best != nil {
				return best, true
			}
		}
	}
	return b.ByName(name)
}

// betterCanonical reports whether candidate should replace cur as the canonical
// entry for a name: a player-castable row beats an NPC-only one; among equals, the
// lower min cast level (the classic spell, not a high-level reprint) wins. With no
// class data both are "castable" and the later row wins (the original behavior).
func betterCanonical(candidate, cur *Spell) bool {
	cc, pc := candidate.playerCastable(), cur.playerCastable()
	if cc != pc {
		return cc
	}
	return candidate.minCastLevel() <= cur.minCastLevel()
}

// SelfClicky returns the instant self-buff whose cast-on-you emote is line, if
// any — used to time clickies that produce no cast line.
func (b *Book) SelfClicky(line string) (*Spell, bool) {
	s, ok := b.byEmote[line]
	return s, ok
}

// Len is the number of spells loaded.
func (b *Book) Len() int { return len(b.byName) }

// Load parses spells_us.txt at path.
func Load(path string) (*Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadReader(f)
}

// LoadReader parses spells_us.txt content from r. It first auto-calibrates the
// per-class min-level block offset (which moves between client versions), then
// decodes each row, keeping the canonical (player-castable, classic) spell per
// name and all variants for class-aware lookup.
func LoadReader(r io.Reader) (*Book, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	classOff := calibrateClassOffset(lines)
	b := &Book{
		byName:     make(map[string]*Spell, 9000),
		byNameAll:  make(map[string][]*Spell, 9000),
		byEmote:    make(map[string]*Spell),
		classKnown: classOff >= 0,
	}
	for _, line := range lines {
		s := decode(line, classOff)
		if s == nil {
			continue
		}
		// canonical pick per name (player-castable + lowest level wins; see
		// betterCanonical) and keep every variant for class-aware resolution.
		if cur, ok := b.byName[s.Name]; !ok || betterCanonical(s, cur) {
			b.byName[s.Name] = s
		}
		b.byNameAll[s.Name] = append(b.byNameAll[s.Name], s)
		// index instant (cast-time 0) self-buffs with a real duration by their
		// landing emote — these clickies emit no "You begin casting" line. Many
		// spells share an emote ("Your feet leave the ground." → Levitate, a dev
		// "Levitate Test" stub, …), so skip obvious test stubs and keep the
		// longest-duration candidate, which is the real buff rather than a stub.
		// Detrimental spells are excluded: a self-clicky is beneficial, and a
		// hostile proc's cast_on_you emote is owned by the incoming-debuff path
		// (incoming.go) — indexing it here would spawn a duplicate ON-YOU timer.
		// NPC/GM variants are excluded too: many are instant copies of a real buff
		// (NPCAegolism vs the castable Aegolism), and being instant they'd win the
		// shared landing emote — so a cleric's Aegolism on you showed as NPCAegolism.
		// When class data is available, also require the buff be player-castable —
		// the same NPC-copy problem, caught structurally rather than by name prefix.
		if s.CastTimeMs == 0 && s.DurFormula != 0 && s.CastOnYou != "" && !s.Detrimental &&
			!strings.Contains(strings.ToLower(s.Name), "test") && !genericBuffEmotes[s.CastOnYou] &&
			!isIncomingDebuffLand(s.CastOnYou) && // a slow/debuff "on you" line is never a self-buff landing
			!nonPlayerSpell(s.Name) && (!b.classKnown || s.playerCastable()) {
			if prev, ok := b.byEmote[s.CastOnYou]; !ok || s.DurCap > prev.DurCap {
				b.byEmote[s.CastOnYou] = s
			}
		}
	}
	return b, nil
}

func decode(line string, classOff int) *Spell {
	f := strings.Split(line, "^")
	if len(f) < minFields {
		return nil
	}
	name := f[fName]
	if name == "" {
		return nil
	}
	fades := NormEmote(strings.TrimSpace(f[fFades]))
	return &Spell{
		Name:        name,
		CastTimeMs:  atoi(f[fCastTime]),
		RecastMs:    atoi(f[fRecastTime]),
		DurFormula:  atoi(f[fDurFormula]),
		DurCap:      atoi(f[fDurCap]),
		CastOnYou:   NormEmote(strings.TrimSpace(f[fCastOnYou])),
		CastOnOther: NormEmote(strings.TrimRight(f[fCastOnOther], " ")),
		Fades:       fades,
		Detrimental: atoi(f[fGoodEffect]) == 0,
		// every charm spell carries this wear-off marker and has no landing emote
		Charm: fades == "You are no longer charmed.",
		// mez/enthrall — the landing emote on the mob carries "mesmeriz"/"enthrall"
		Mez: mezEmote(f[fCastOnOther]),
		// pacify/lull/calm — the whole line lands with "<mob> looks less aggressive."
		Pacify:      strings.Contains(strings.ToLower(f[fCastOnOther]), "looks less aggressive"),
		classLevels: decodeClassLevels(f, classOff),
	}
}

// decodeClassLevels reads the 16-wide per-class min-level block at the calibrated
// offset. Returns nil when uncalibrated (classOff < 0) or the row is too short, so
// castability reads as unknown rather than wrongly "NPC-only".
func decodeClassLevels(f []string, classOff int) []int {
	if classOff < 0 || classOff+numSpellClasses > len(f) {
		return nil
	}
	out := make([]int, numSpellClasses)
	for i := range out {
		out[i] = atoi(f[classOff+i])
	}
	return out
}

// classAnchors are spells whose exact player-class castability is well known, used
// to locate the per-class block without hardcoding a client-specific offset. Gate
// is the keystone: castable by exactly the seven pure casters and nothing else, a
// near-unique 16-wide signature. The single-class anchors confirm the offset.
var classAnchors = []struct {
	name    string
	casters []int // class indices that MUST be castable; all others MUST be 254/255
}{
	{"Gate", []int{1, 5, 9, 10, 11, 12, 13}}, // Cleric/Druid/Shaman/Necro/Wizard/Mage/Enchanter
	{"Complete Heal", []int{1}},              // Cleric only
	{"Mesmerize", []int{13}},                 // Enchanter only
}

// calibrateClassOffset finds the offset of the per-class min-level block by testing
// every plausible offset against the anchor spells. Gate must validate; any other
// anchor present must validate at the same offset. Returns -1 if it can't be found
// (a custom/foreign file) — callers then skip class features rather than misread.
func calibrateClassOffset(lines []string) int {
	// collect the split fields of each anchor spell's rows (names may repeat)
	rows := map[string][][]string{}
	wanted := map[string]bool{}
	for _, a := range classAnchors {
		wanted[a.name] = true
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		f := strings.Split(line, "^")
		if len(f) <= fName {
			continue
		}
		if wanted[f[fName]] {
			rows[f[fName]] = append(rows[f[fName]], f)
		}
	}
	gate := rows["Gate"]
	if len(gate) == 0 {
		return -1 // no keystone → can't calibrate
	}
	maxLen := 0
	for _, f := range gate {
		if len(f) > maxLen {
			maxLen = len(f)
		}
	}
	for off := fGoodEffect + 1; off+numSpellClasses <= maxLen; off++ {
		ok := true
		for _, a := range classAnchors {
			present := rows[a.name]
			if len(present) == 0 {
				continue // anchor not in this file — skip (Gate is guaranteed above)
			}
			if !anyRowMatchesSignature(present, off, a.casters) {
				ok = false
				break
			}
		}
		if ok {
			return off
		}
	}
	return -1
}

// anyRowMatchesSignature reports whether at least one of the anchor's rows has,
// at offset off, exactly the given caster columns castable (1..253) and every
// other column set to "never" (254/255). At least one row must match because a
// duplicate name can include NPC copies whose block is all-255.
func anyRowMatchesSignature(rows [][]string, off int, casters []int) bool {
	isCaster := make([]bool, numSpellClasses)
	for _, c := range casters {
		isCaster[c] = true
	}
	for _, f := range rows {
		if off+numSpellClasses > len(f) {
			continue
		}
		good := true
		for i := range numSpellClasses {
			v := atoi(f[off+i])
			castable := v >= 1 && v <= 253
			if castable != isCaster[i] {
				good = false
				break
			}
		}
		if good {
			return true
		}
	}
	return false
}

// mezEmoteMarkers are the distinctive cast-on-other phrases that mark a mez/charm-
// hold (a stand-still crowd-control that breaks on damage). The original heuristic
// only caught "mesmeriz"/"enthrall", which silently MISSED the Enchanter and Bard
// mez lines that phrase it differently (Rapture, Fascination, Entrance, Lullaby,
// Song of the Sirens, the undead Rest/Numb the Dead). Each phrase was confirmed
// present in spells_us cast_on_other and unique to mez spells — verified against
// the GINA "Kingdom" P99 trigger pack, which enumerates these CC landing emotes.
var mezEmoteMarkers = []string{
	"mesmeriz",                 // Mesmerize / Dazzle / Mesmerization (the broad family)
	"enthrall",                 // Enthrall / Dominate / Gaze
	"swoons in raptured bliss", // Rapture
	"has been fascinated",      // Fascination
	"has been entranced",       // Entrance / Entrancing Tune
	"head nods",                // Kelin's Lucid Lullaby (and Dreams of … line)
	"eyes glaze over",          // Solon's Song of the Sirens / Bewitching Bravura
	"looks ambivalent",         // Rest the Dead / Numb the Dead (undead mez)
}

// mezEmote reports whether a cast-on-other emote marks a mez (crowd control that
// breaks on damage).
func mezEmote(castOnOther string) bool {
	s := strings.ToLower(castOnOther)
	for _, m := range mezEmoteMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// NormEmote collapses a run of trailing periods to a single one. spells_us.txt
// has data with doubled trailing periods ("..") that the game log renders as a
// single "."; normalizing both the stored emote and the incoming log line lets
// them match regardless of which side is doubled.
func NormEmote(s string) string {
	trimmed := strings.TrimRight(s, ".")
	if trimmed == s {
		return s // no trailing period to collapse
	}
	return trimmed + "."
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
