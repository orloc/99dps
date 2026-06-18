package gamestate

// Incoming debuffs: hostile spell effects landing on the PLAYER.
//
// EQ logs the spell's cast_on_you emote when an effect lands on you ("You feel
// drowsy.", "Your feet adhere to the ground.") but — like every spell line —
// names no caster and carries no caster level. Two things follow:
//
//   - We can't know the exact spell. Within an effect family the cast_on_you
//     emote is shared (every classic slow → "You feel drowsy."; every root →
//     "Your feet adhere to the ground."), so the emote tells us the CATEGORY,
//     not the spell. We show the category ("Slowed", "Rooted").
//   - We can't compute the real duration (it scales with the mob's level). So we
//     show the family's CEILING duration — an over-estimate — and rely on the
//     wear-off (fade) emote, which the afflicted player DOES see and which IS
//     authoritative, to clear it early. Over-estimate + reliable clear means a
//     timer can linger a beat past the real end but never vanish while you're
//     still debuffed. The estimate is marked (a "~" in the UI) so the seconds
//     aren't trusted to the wire.
//
// Scope is the common melee-relevant set (the effects a melee reacts to), not
// every detrimental spell — fewer false positives, less noise. Each landing
// emote belongs to exactly one category, so detection is unambiguous even when
// the underlying spell isn't.

// incomingDebuff is one recognizable category of hostile effect on the player.
type incomingDebuff struct {
	label string   // shown in the ON YOU section
	land  []string // cast_on_you emotes that start it
	fade  []string // wear-off emotes that clear it early (the authoritative end)
	dur   int      // ceiling duration, seconds = (maxDurCapTicks+1)*6 for the family
}

// incomingDebuffs is the curated melee-relevant set. Durations are the family's
// ceiling (the longest member's cap); the fade line corrects the real end.
var incomingDebuffs = []incomingDebuff{
	{
		label: "Slowed", // attack-speed slow (Drowsy, Walking Sleep, Tagar's/Togor's/Turgur's, Shiftless Deeds, Languid Pace, Earthcall)
		land: []string{"You feel drowsy.", "You slow down.",
			"The earth's call dulls your mind and slows your muscles."},
		fade: []string{"You feel less drowsy.", "Your speed returns.",
			"You speed back up.", "The call of earth recedes."},
		dur: 366, // Turgur's Insects, cap 60 ticks
	},
	{
		label: "Snared", // movement snare (Snare, Ensnare, Tangling Weeds, Clinging Darkness)
		land: []string{"You are ensnared.",
			"You slow down as your feet are covered in tangling weeds.",
			"You are in the grip of darkness."},
		fade: []string{"You are no longer ensnared.",
			"The tangling weeds wither away.", "The darkness fades."},
		dur: 786, // Ensnare, ~130 ticks at level 60
	},
	{
		label: "Rooted", // root (Root, Fetter, Paralyzing Earth, Enstill, Engulfing Roots, Bonds of Force, Atol's, Vengeance of the Glades)
		land: []string{"Your feet adhere to the ground.",
			"Your feet become entwined.",
			"Bonds of force stick your feet to the ground.",
			"Spectral shackles bind your feet to the ground."},
		fade: []string{"Your feet come free.", "The roots fall from your feet."},
		dur:  186, // Fetter, cap 30 ticks
	},
	{
		label: "Crippled", // STR/AC/ATK debuff (Cripple, Disempower, Listless Power, Incapacitate, Weaken, Insipid Weakness)
		land: []string{"You have been crippled.", "You feel frail.",
			"You feel weak.", "You feel weaker."},
		fade: []string{"You feel your strength return.",
			"Your strength returns.", "Your weakness fades."},
		dur: 426, // Cripple, cap 70 ticks
	},
	{
		label: "Malo'd", // magic-resist debuff (Malo, Mala)
		land:  []string{"You feel very vulnerable."},
		fade:  []string{"Your vulnerability fades."},
		dur:   846, // cap 140 ticks
	},
	{
		label: "Tashed", // magic-resist debuff (Tashan line, Scent of Dusk/Darkness)
		land: []string{"You hear the barking of Tashan.",
			"You hear the barking of the Tashani.",
			"You hear the barking of Tashania.",
			"You smell the faint scent of dusk.",
			"You smell the faint scent of darkness."},
		fade: []string{"The barking fades.",
			"The scent of dusk fades.", "The scent of darkness fades."},
		dur: 786, // ~130 ticks
	},
	{
		label: "Feared", // fear (Invoke/Inspire/Cast Fear, Cloud of Fear)
		land:  []string{"Your mind fills with fear.", "Your mind is wracked by fear."},
		fade:  []string{"You are no longer afraid."},
		dur:   48, // cap 7 ticks
	},
	{
		label: "Blinded", // blind (Blinding Luminance, Flash of Light)
		land:  []string{"You are blinded by a flash of light."},
		fade:  []string{"Your sight returns."},
		dur:   30, // cap 4 ticks
	},
	{
		label: "Diseased", // disease DoT/debuff (Insidious Malady)
		land:  []string{"You feel a fever settle upon you."},
		fade:  []string{"Your fever has broken."},
		dur:   846, // cap 140 ticks
	},
	{
		label: "Poisoned", // poison DoT (Envenomed Bolt, Feeble Mind, …)
		land: []string{"You have been poisoned.",
			"You feel your mind fuzz as poison spreads through your body."},
		fade: []string{"The poison has run its course."},
		dur:  48, // cap 7 ticks
	},
}

// incoming lookup tables, built once from the curated set. A landing emote maps
// to exactly one category; a fade emote may clear more than one (e.g. "Your feet
// come free." ends any root).
var (
	incomingByLand = map[string]incomingDebuff{}
	incomingByFade = map[string][]string{}
)

func init() {
	for _, d := range incomingDebuffs {
		for _, l := range d.land {
			incomingByLand[NormEmote(l)] = d
		}
		for _, f := range d.fade {
			fe := NormEmote(f)
			incomingByFade[fe] = append(incomingByFade[fe], d.label)
		}
	}
}

// matchIncomingDebuffLocked starts (or refreshes) a timer when body is the
// cast_on_you emote of a melee-relevant hostile effect. The timer is on "You",
// detrimental, and flagged Estimated (its duration is a family ceiling, not an
// exact value). Re-application refreshes the single per-category timer — the
// target is always "You", so there are no same-named instances to disambiguate.
// Caller holds the lock; body is already NormEmote-normalized.
func (t *Tracker) matchIncomingDebuffLocked(body string, at int64) {
	d, ok := incomingByLand[body]
	if !ok {
		return
	}
	t.timers[key(d.label, "You")] = Timer{
		Spell:       d.label,
		Target:      "You",
		Start:       at,
		Expiry:      at + int64(d.dur),
		Detrimental: true,
		Estimated:   true,
	}
}

// expireIncomingByFadeLocked clears the player's incoming-debuff timer(s) when
// body is a wear-off emote — the authoritative end that corrects the ceiling
// estimate. Caller holds the lock; body is already NormEmote-normalized.
func (t *Tracker) expireIncomingByFadeLocked(body string) {
	labels, ok := incomingByFade[body]
	if !ok {
		return
	}
	for _, lbl := range labels {
		delete(t.timers, key(lbl, "You"))
	}
}
