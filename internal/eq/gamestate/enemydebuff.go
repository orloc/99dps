package gamestate

import (
	"strings"

	"99dps/internal/eq/combat"
)

// Enemy debuffs: detrimental effects landing on a MOB that we learn about from a
// caster-less signal — either the spell's cast_on_other emote ("<mob> yawns." for
// a slow) or an explicit guild/group macro call ("SLOWED - a fiery guardian").
// Neither names the caster or the caster level, so we can't know the exact spell
// or its real duration. We show the effect CATEGORY on the mob, timed to the
// family's max-level CEILING (an over-estimate, marked Estimated → a leading "~"),
// refreshed by any repeat signal and cleared when the mob dies. Useful for a
// melee: "is my target slowed / tashed / maloed?"
//
// Both signals route through setEnemyDebuffLocked, which keys on the NORMALIZED
// mob name — so a slow learned from the emote ("Lava beetle") and from a macro
// ("a lava beetle") collapse to a single timer instead of doubling up.
//
// A player casting their OWN version lands via the normal pending-cast path
// (matchLandingLocked, with the true spell + duration), so the emote path is only
// consulted when no real cast landed the line (the !landed guard in observeLocked)
// AND there's no matching pending cast.

// setEnemyDebuffLocked records (or refreshes) an estimated detrimental category
// debuff on a mob. Keyed by the normalized mob name (article-stripped, lowercased)
// so different spellings of the same mob — and the same effect learned from two
// signals — don't stack. One instance per (label, mob).
func (t *Tracker) setEnemyDebuffLocked(label, mob string, dur int, at int64) {
	if combat.IgnoredEntity(mob) { // a summoned eye etc. — never tracked
		return
	}
	name := normalizeMobName(mob)
	if name == "" || name == "you" {
		return
	}
	t.timers[key(label, name)] = Timer{
		Spell:       label,
		Target:      name,
		Start:       at,
		Expiry:      at + int64(dur),
		Detrimental: true,
		Estimated:   true,
	}
}

// --- emote signal: the spell's cast_on_other line on a mob ---

type enemyDebuff struct {
	label string   // category shown on the mob (e.g. "Slowed")
	land  []string // cast_on_other emote suffixes (the mob name is the prefix)
	dur   int      // ceiling duration in seconds (estimated)
}

// enemyDebuffs is the curated set of mob-side emotes worth surfacing to a melee.
// Suffixes are checked in order, so a more-specific possessive form ("'s body
// slows down.") must precede the generic one (" slows down.") it would otherwise
// shadow — else the target would be extracted as "<mob>'s body".
var enemyDebuffs = []enemyDebuff{
	{
		label: "Slowed",
		// attack-speed slow across casting classes: shaman insects/Drowsy/Walking
		// Sleep emote " yawns."; enchanter Deeds line + Languid Pace emote " slows
		// down."; a few others use "'s body slows down."
		land: []string{"'s body slows down.", " slows down.", " yawns."},
		dur:  366, // Turgur's Insects, cap 60 ticks — the "big slow" ceiling
	},
	{
		label: "Crippled",
		// STR/AC/ATK debuff: Cripple " has been crippled."; Disempower/Listless
		// Power/Incapacitate " looks frail."; Insipid Weakness " is weakened.";
		// Weaken " weakens."
		land: []string{" has been crippled.", " looks frail.", " is weakened.", " weakens."},
		dur:  426, // Cripple, cap 70 ticks
	},
}

// maloEmote is the cast_on_other shared by the whole Malo line (Malo/Malosi/
// Malosini/Malosinia/Malaise III+) — one sighting can't tell them apart.
const maloEmote = " looks very uncomfortable."

// matchEnemyMaloLocked times a mob the malo-line emote landed on. Malo (cap 140)
// and Malosini (cap 205) share this emote but differ a lot in duration, so we
// assume the shorter Malo on the FIRST emote sighting per mob and upgrade to
// Malosini on the SECOND — counted by emote only, so a macro call or your own cast
// doesn't trip the upgrade. An explicit Malosini already up (e.g. from a macro) is
// just refreshed, never downgraded. Caller holds the lock; only consulted when no
// real cast landed this line.
func (t *Tracker) matchEnemyMaloLocked(body string, at int64) {
	if !strings.HasSuffix(body, maloEmote) {
		return
	}
	if t.pending != nil && t.pending.CastOnOther == maloEmote {
		return // your own malo — the real pending-cast path owns it
	}
	target := strings.TrimSpace(strings.TrimSuffix(body, maloEmote))
	if target == "" || target == "You" || combat.IgnoredEntity(target) {
		return
	}
	name := normalizeMobName(target)
	if name == "" || name == "you" {
		return
	}
	switch {
	case t.timerExistsLocked("Malosini", name): // already the strong one → refresh
		t.setEnemyDebuffLocked("Malosini", target, 1236, at)
	case t.maloSeen[name]: // a second emote sighting → it's really Malosini
		delete(t.timers, key("Malo'd", name))
		t.setEnemyDebuffLocked("Malosini", target, 1236, at)
	default: // first sighting → assume the shorter Malo
		if t.maloSeen == nil {
			t.maloSeen = map[string]bool{}
		}
		t.maloSeen[name] = true
		t.setEnemyDebuffLocked("Malo'd", target, 846, at)
	}
}

// timerExistsLocked reports whether a timer for (label, normalized-name) is active.
func (t *Tracker) timerExistsLocked(label, normName string) bool {
	_, ok := t.timers[key(label, normName)]
	return ok
}

// matchEnemyDebuffLocked starts an estimated category debuff on the struck mob
// when body is a registered cast_on_other emote. Caller holds the lock; only
// called when no real cast landed this line.
func (t *Tracker) matchEnemyDebuffLocked(body string, at int64) {
	for _, d := range enemyDebuffs {
		for _, suffix := range d.land {
			if !strings.HasSuffix(body, suffix) {
				continue
			}
			// the player casting their OWN version of this effect: the real
			// pending-cast path owns it (with the true spell + duration), so don't
			// also lay down an estimate — that's the double-timer to avoid.
			if t.pending != nil && t.pending.CastOnOther == suffix {
				return
			}
			target := strings.TrimSpace(strings.TrimSuffix(body, suffix))
			if target == "" || target == "You" {
				return
			}
			t.setEnemyDebuffLocked(d.label, target, d.dur, at)
			return
		}
	}
}

// --- macro signal: an explicit guild/group debuff call ---

// macroDebuff maps a raid/group call keyword to its category label and the
// max-level ceiling for that spell type.
type macroDebuff struct {
	label string
	dur   int
}

// macroDebuffs is the keyword vocabulary players announce in guild/group chat as
// "<KEYWORD> - <mob>". Each maps to a category + the family's MAX-LEVEL ceiling:
// (DurCap+1)*6 seconds, the same grant-tick convention Spell.DurationSeconds uses,
// with caps/formulas verified against the client spells_us.txt and the P99 wiki.
// Heal/summon calls that share the format (KCH/CH/COTH/REMEDY, which target
// players) are deliberately absent, so only mob debuffs are recognized. Malo and
// Malosini are SEPARATE families — different spells, very different durations — so
// they get distinct labels and aren't clumped.
var macroDebuffs = map[string]macroDebuff{
	"SLOWED":    {"Slowed", 366},    // Turgur's Insects — F7, cap 60 ticks (wiki 6.0m @60)
	"TASHED":    {"Tashed", 786},    // Tashania — F9, cap 130 ticks (wiki 13m @60)
	"SIEVED":    {"Sieved", 786},    // enchanter "sieve" = Tashan line (Tashania)
	"SIEVE":     {"Sieved", 786},    //
	"MALO":      {"Malo'd", 846},    // Malo/Mala/Malos/Malosi — F9, cap 140 ticks
	"MALOED":    {"Malo'd", 846},    //
	"MALOSINI":  {"Malosini", 1236}, // Malosini — F10, cap 205 ticks (wiki 20.5m) — NOT the same as Malo
	"MALOSINIA": {"Malosini", 1236}, // Malosinia (Velious) — same F10/205 cap
	"CRIPPLE":   {"Crippled", 426},  // Cripple — F8, cap 70 ticks (wiki 7.0m @60)
	"CRIPPLED":  {"Crippled", 426},  //
	"OOS":       {"OOS", 24},        // Occlusion of Sound — bard song, F5 cap 3 ticks (wiki 3 ticks); short & re-sung
}

// matchMacroDebuffLocked recognizes an explicit guild/group debuff call
// ("<KEYWORD> - <mob>") and times the mob accordingly. Only the guild and group
// channels are honored (not say/ooc/tell), so random chat can't trip it. Caller
// holds the lock.
func (t *Tracker) matchMacroDebuffLocked(body string, at int64) {
	msg, ok := guildOrGroupMessage(body)
	if !ok {
		return
	}
	kw, mob, ok := splitMacroCall(msg)
	if !ok {
		return
	}
	if d, ok := macroDebuffs[kw]; ok {
		t.setEnemyDebuffLocked(d.label, mob, d.dur, at)
		t.raid.recordLocked(d.label, mob, speakerOf(body), d.dur, at) // raid-target board
	}
}

// speakerOf returns the player who sent a guild/group chat line, or "".
func speakerOf(body string) string {
	for _, sep := range []string{" tells the guild, '", " tells the group, '"} {
		if i := strings.Index(body, sep); i > 0 {
			return body[:i]
		}
	}
	return ""
}

// guildOrGroupMessage returns the quoted message body of a guild- or group-chat
// line ("<Player> tells the guild, '<msg>'"), or false for any other line.
func guildOrGroupMessage(body string) (string, bool) {
	for _, sep := range []string{" tells the guild, '", " tells the group, '"} {
		if i := strings.Index(body, sep); i >= 0 {
			rest := body[i+len(sep):]
			if j := strings.LastIndex(rest, "'"); j >= 0 {
				return rest[:j], true
			}
		}
	}
	return "", false
}

// splitMacroCall parses "<KEYWORD> - <mob>" into its keyword (upper-cased) and a
// cleaned mob name (trailing " -" and a parenthetical annotation stripped). It
// reports false when the message isn't in that shape, so plain chat is ignored.
func splitMacroCall(msg string) (kw, mob string, ok bool) {
	i := strings.Index(msg, " - ")
	if i <= 0 {
		return "", "", false
	}
	kw = strings.ToUpper(strings.TrimSpace(msg[:i]))
	mob = strings.TrimSpace(msg[i+3:])
	if p := strings.Index(mob, " ("); p >= 0 { // drop "(Occlusion of Sound; -10 CR…)"
		mob = strings.TrimSpace(mob[:p])
	}
	mob = strings.TrimRight(mob, " -") // drop a trailing " -"
	return kw, mob, kw != "" && mob != ""
}
