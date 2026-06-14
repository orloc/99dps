package eqclass

import "slices"

// Profile is the per-class descriptor: the single place a class's declarative
// facts live, one literal per class (a class's "module"). Consumers in other
// layers read it through ProfileOf rather than each keeping their own class
// switch. Behavior that needs log-line matching — cooldown/clicky detection —
// stays in the gamestate layer (it needs the spell book), so this holds only the
// data both the gamestate and tui layers can share.
type Profile struct {
	Category           Category
	Mez, Charm, Pacify bool     // crowd control the class can cast
	Feign              bool     // has Feign Death
	RecastWatch        []string // spells whose reuse we surface as a cooldown
}

// profiles is the canonical class table — the single source of truth for both a
// class's panel category and its capabilities. An unmapped class (incl.
// ClassUnknown) yields the zero Profile: CatCaster with no capabilities, the
// safe default (an empty spell-timer panel rather than a wrong guess).
var profiles = map[Class]Profile{
	// melee
	ClassWarrior: {Category: CatMelee},
	ClassMonk:    {Category: CatMelee, Feign: true},
	ClassRogue:   {Category: CatMelee},

	// hybrid
	ClassPaladin:      {Category: CatHybrid},
	ClassShadowKnight: {Category: CatHybrid},
	ClassRanger:       {Category: CatHybrid, Pacify: true}, // Harmony
	ClassBard:         {Category: CatHybrid, Mez: true, Charm: true},
	ClassBeastlord:    {Category: CatHybrid},

	// caster
	ClassCleric:      {Category: CatCaster, Pacify: true},
	ClassDruid:       {Category: CatCaster, Charm: true, Pacify: true}, // charm animals + calm/harmony
	ClassShaman:      {Category: CatCaster},
	ClassNecromancer: {Category: CatCaster, Feign: true, Mez: true, Charm: true, Pacify: true}, // undead CC
	ClassWizard:      {Category: CatCaster},
	ClassMagician:    {Category: CatCaster},
	ClassEnchanter: {Category: CatCaster, Mez: true, Charm: true, Pacify: true,
		RecastWatch: []string{"Dictate", "Rapture"}},
}

// ProfileOf returns a class's descriptor (the zero Profile for an unknown class).
func ProfileOf(c Class) Profile { return profiles[c] }

// RecastWatched reports whether a spell's reuse should be surfaced as a cooldown
// for the given class. When the class isn't known yet (pre-/who), it matches any
// class's watch list — so an Enchanter's Dictate is tracked even before a /who
// reveals the class.
func RecastWatched(c Class, spell string) bool {
	if c == ClassUnknown {
		for _, p := range profiles {
			if slices.Contains(p.RecastWatch, spell) {
				return true
			}
		}
		return false
	}
	return slices.Contains(profiles[c].RecastWatch, spell)
}
