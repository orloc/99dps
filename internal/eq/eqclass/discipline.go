package eqclass

// Discipline is one activated combat discipline: its name, the level it's
// learned, how long the effect lasts (DurSec, 0 for "arms the next attack"), and
// the reuse timer before it can fire again (ReuseSec — the cooldown the UI
// counts down). Data transcribed from the P99 wiki (wiki.project1999.com/Disciplines).
//
// A few warrior discs (Evasive/Defensive/Precision) shorten their reuse as you
// level; we store the level-60 value, the one that matters at the cap.
type Discipline struct {
	Name     string
	Level    int
	DurSec   int64 // active duration; 0 = arms the next strike (no fixed window)
	ReuseSec int64 // reuse/refresh timer — the cooldown we surface
}

// mn converts minutes to seconds, for readable durations below.
const mn = 60

// disciplines lists every class's disciplines, lowest level first. Only melee
// and hybrid classes appear; casters (and Beastlord, a Luclin class outside
// P99's era) have none — DisciplinesFor returns nil for them, hiding the widget.
var disciplines = map[Class][]Discipline{
	ClassWarrior: {
		{"Resistant", 30, 1 * mn, 60 * mn},
		{"Fearless", 40, 12, 60 * mn},
		{"Evasive", 52, 3 * mn, 7 * mn}, // 15m→7m by L60
		{"Charge", 53, 14, 30 * mn},
		{"Mighty Strike", 54, 10, 60 * mn},
		{"Defensive", 55, 3 * mn, 10 * mn}, // 15m→10m by L60
		{"Furious", 56, 9, 60 * mn},
		{"Precision", 57, 3 * mn, 27 * mn}, // 30m→27m by L60
		{"Felstrike", 58, 12, 30 * mn},
		{"Fortitude", 59, 8, 60 * mn},
		{"Aggressive", 60, 3 * mn, 27 * mn},
	},
	ClassMonk: {
		{"Resistant", 30, 1 * mn, 60 * mn},
		{"Fearless", 40, 11, 60 * mn},
		{"Stonestance", 51, 12, 6 * mn},
		{"Thunderkick", 52, 0, 4 * mn}, // arms next Flying Kick
		{"Whirlwind", 53, 9, 60 * mn},
		{"Voiddance", 54, 8, 55 * mn},
		{"Inner Flame", 56, 12, 26 * mn},
		{"Hundred Fists", 57, 15, 30 * mn},
		{"Silentfist", 59, 0, 9 * mn}, // arms next Dragon Punch
		{"Ashenhand", 60, 0, 72 * mn}, // arms next Eagle Strike
	},
	ClassRogue: {
		{"Resistant", 30, 1 * mn, 60 * mn},
		{"Fearless", 40, 11, 60 * mn},
		{"Counterattack", 53, 9, 60 * mn},
		{"Deadeye", 54, 18, 30 * mn},
		{"Nimble", 55, 12, 30 * mn},
		{"Kinesthetics", 57, 18, 30 * mn},
		{"Blinding Speed", 58, 18, 30 * mn},
		{"Duelist", 59, 12, 30 * mn},
	},
	ClassPaladin: {
		{"Resistant", 51, 1 * mn, 60 * mn},
		{"Fearless", 54, 11, 60 * mn},
		{"Holyforge", 55, 2 * mn, 72 * mn},
		{"Sanctification", 60, 10, 72 * mn},
	},
	ClassShadowKnight: {
		{"Resistant", 51, 1 * mn, 60 * mn},
		{"Fearless", 54, 11, 60 * mn},
		{"Unholy Aura", 55, 0, 72 * mn}, // arms next Harm Touch
		{"Leechcurse", 60, 15, 72 * mn},
	},
	ClassRanger: {
		{"Resistant", 51, 1 * mn, 60 * mn},
		{"Fearless", 54, 11, 60 * mn},
		{"Trueshot", 55, 2 * mn, 72 * mn},
		{"Weapon Shield", 60, 15, 72 * mn},
	},
	ClassBard: {
		{"Resistant", 51, 1 * mn, 60 * mn},
		{"Fearless", 54, 11, 60 * mn},
		{"Deftdance", 55, 10, 72 * mn},
		{"Puretone", 60, 2 * mn, 72 * mn},
	},
}

// DisciplinesFor returns a class's disciplines lowest-level first (nil for a
// class with none — the widget's gate). The slice aliases the canonical table —
// READ-ONLY.
func DisciplinesFor(c Class) []Discipline { return disciplines[c] }

// disciplineActivations maps a discipline's self-activation emote (the exact line
// EQ logs when the player uses it) to the discipline name. These are first-person
// self-emotes, so they only ever appear in the owner's own log — matching the
// whole line (not a substring) keeps a quoted tell/say from tripping them.
//
// Transcribed from the GINA "Kingdom" community trigger pack; verify against real
// logs before trusting any one line. Emotes are stored in NormEmote form (a single
// trailing period) — EQ sometimes writes a doubled "..", which gamestate.NormEmote
// collapses before lookup. Only disciplines with a confirmed unique emote are
// listed; the rest simply aren't auto-detected yet (their timers still work if
// started another way). Every name here MUST exist in `disciplines` above and be
// unique to one class — TestDisciplineActivationsValid guards both.
var disciplineActivations = map[string]string{
	// Warrior
	"You assume a defensive fighting style.":    "Defensive",
	"You assume an evasive fighting style.":     "Evasive",
	"You assume a precise fighting style.":      "Precision",
	"A consuming rage takes over your weapons.": "Furious",
	// Monk
	"Your body becomes one with the earth.":                    "Stonestance",
	"You become untouchable.":                                  "Voiddance",
	"Your instincts take over as you turn aside every attack.": "Whirlwind",
	"Your muscles bulge with the force of will.":               "Inner Flame",
	// Rogue
	"Your weapons move with uncanny grace.": "Counterattack",
	"Your muscles quiver with power.":       "Duelist",
	"You bounce about nimbly.":              "Nimble",
	// Paladin
	"Your body is surrounded in an aura of sanctification.": "Sanctification",
	// Ranger
	"Your bow crackles with natural energy.": "Trueshot",
	"Your weapons begin to spin.":            "Weapon Shield",
	// Bard
	"You dance about nimbly.":                 "Deftdance",
	"Your voice becomes perfectly melodious.": "Puretone",
}

// DisciplineActivation resolves a self-emote line to the discipline it signals.
// It returns the discipline name, the owning class, and whether that class can be
// safely inferred from the emote (true when exactly one class has the discipline,
// which is the case for every entry today). ok is false when no emote matches. The
// class is derived from the `disciplines` table, so the activation map is the
// single source of truth for emotes and can't drift from the class data.
func DisciplineActivation(line string) (name string, class Class, inferClass bool, ok bool) {
	name, ok = disciplineActivations[line]
	if !ok {
		return "", ClassUnknown, false, false
	}
	var owners []Class
	for c, list := range disciplines {
		for _, d := range list {
			if d.Name == name {
				owners = append(owners, c)
				break
			}
		}
	}
	if len(owners) == 1 {
		return name, owners[0], true, true
	}
	return name, ClassUnknown, false, true // shared name → record it, but don't guess the class
}
