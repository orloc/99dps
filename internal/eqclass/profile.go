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
	Mez, Charm, Pacify bool                   // crowd control the class can cast
	Feign              bool                   // has Feign Death
	RecastWatch        []string               // spells whose reuse we surface as a cooldown
	Skills             map[string][]SkillRank // generic combat bucket → level-gated display names
}

// SkillRank names an activated-skill bucket at or above a level. EQ logs special
// attacks with a generic verb (every kick variant is "kick", every monk hand
// strike is "strike"), so the level-right ability name is a best-guess overlay.
// Ranks for a bucket are listed highest MinLevel first; the first one the level
// clears wins.
type SkillRank struct {
	MinLevel int
	Name     string
}

// profiles is the canonical class table — the single source of truth for both a
// class's panel category and its capabilities. An unmapped class (incl.
// ClassUnknown) yields the zero Profile: CatCaster with no capabilities, the
// safe default (an empty spell-timer panel rather than a wrong guess).
var profiles = map[Class]Profile{
	// melee
	ClassWarrior: {Category: CatMelee},
	ClassMonk: {Category: CatMelee, Feign: true, Skills: map[string][]SkillRank{
		// the kick and hand-strike specials log generically; name them by level
		"Kick":   {{30, "Flying Kick"}, {5, "Round Kick"}},
		"Strike": {{25, "Dragon Punch"}, {10, "Eagle Strike"}, {0, "Tiger Claw"}},
	}},
	ClassRogue: {Category: CatMelee},

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

// restrictedSkills marks generic skill buckets that are meaningful for only one
// class — the monk hand-strike bucket logs as "Strike"; for anyone else it's
// noise. A bucket not listed here is universal melee, shown for every class.
var restrictedSkills = map[string]Class{"Strike": ClassMonk}

// SkillName resolves a generic combat bucket to its level-appropriate display
// name for a class (Monk "Kick" → "Flying Kick" at 30). Falls back to the bucket
// name when the class has no override at that level.
func SkillName(class Class, bucket string, level int) string {
	for _, r := range profiles[class].Skills[bucket] {
		if level >= r.MinLevel {
			return r.Name
		}
	}
	return bucket
}

// SkillRelevant reports whether a generic skill bucket should be shown for a
// class (a class-restricted bucket only for its owning class; all others always).
func SkillRelevant(class Class, bucket string) bool {
	if owner, ok := restrictedSkills[bucket]; ok {
		return class == owner
	}
	return true
}

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
