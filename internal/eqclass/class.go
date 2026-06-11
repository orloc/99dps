package eqclass

import "strings"

// Category groups EQ classes by what the bottom-right panel should show:
// casters get spell timers, pure melee get skills/disciplines, hybrids get both.
type Category int

const (
	CatCaster Category = iota // spells only — spell-timer panel
	CatMelee                  // skills/disciplines only — skills panel
	CatHybrid                 // both — spell timers plus a skills line
)

// Class is a canonical EQ class name (the base class, not a level title).
type Class string

const (
	ClassUnknown Class = ""

	ClassWarrior      Class = "Warrior"
	ClassMonk         Class = "Monk"
	ClassRogue        Class = "Rogue"
	ClassPaladin      Class = "Paladin"
	ClassShadowKnight Class = "Shadow Knight"
	ClassRanger       Class = "Ranger"
	ClassBard         Class = "Bard"
	ClassBeastlord    Class = "Beastlord"
	ClassCleric       Class = "Cleric"
	ClassDruid        Class = "Druid"
	ClassShaman       Class = "Shaman"
	ClassNecromancer  Class = "Necromancer"
	ClassWizard       Class = "Wizard"
	ClassMagician     Class = "Magician"
	ClassEnchanter    Class = "Enchanter"
)

// category maps each class to its panel category.
var category = map[Class]Category{
	ClassWarrior: CatMelee, ClassMonk: CatMelee, ClassRogue: CatMelee,

	ClassPaladin: CatHybrid, ClassShadowKnight: CatHybrid, ClassRanger: CatHybrid,
	ClassBard: CatHybrid, ClassBeastlord: CatHybrid,

	ClassCleric: CatCaster, ClassDruid: CatCaster, ClassShaman: CatCaster,
	ClassNecromancer: CatCaster, ClassWizard: CatCaster, ClassMagician: CatCaster,
	ClassEnchanter: CatCaster,
}

// CategoryOf returns a class's panel category. An unknown class defaults to
// CatCaster — the safe default, since the spell-timer panel is simply empty for
// a melee player rather than wrong.
func CategoryOf(c Class) Category {
	return category[c] // ClassUnknown and any unmapped class → CatCaster (zero value)
}

// titleToClass maps an EQ /who level-title to its base eqclass. /who prints a
// level-based title ("[60 Warlord]"), not the class name, so detection needs
// this lookup. Lower levels often print the base class name itself, so those
// are included too. The mapping below is the high-confidence subset — base
// names, the well-known 51/55/60 rank titles, and titles validated against real
// P99 /who lines in this project's sample logs. It is intentionally
// conservative: an unrecognised title yields ClassUnknown (→ CatCaster) rather
// than a wrong guess, and new titles are a one-line addition.
var titleToClass = map[string]Class{
	// Warrior (melee)
	"Warrior": ClassWarrior, "Warlord": ClassWarrior,
	// Monk (melee) — Disciple is the 51 rank (51-54), Master 55, Grandmaster 60.
	"Monk": ClassMonk, "Disciple": ClassMonk, "Master": ClassMonk, "Grandmaster": ClassMonk, "Transcendent": ClassMonk,
	// Rogue (melee)
	"Rogue": ClassRogue, "Rake": ClassRogue, "Blackguard": ClassRogue, "Assassin": ClassRogue,

	// Paladin (hybrid)
	"Paladin": ClassPaladin, "Cavalier": ClassPaladin, "Crusader": ClassPaladin,
	// Shadow Knight (hybrid)
	"Shadow Knight": ClassShadowKnight, "Shadowknight": ClassShadowKnight,
	"Reaver": ClassShadowKnight, "Revenant": ClassShadowKnight,
	"Grave Lord": ClassShadowKnight, "Dread Lord": ClassShadowKnight,
	// Ranger (hybrid)
	"Ranger": ClassRanger, "Pathfinder": ClassRanger, "Outrider": ClassRanger, "Warder": ClassRanger,
	// Bard (hybrid)
	"Bard": ClassBard, "Minstrel": ClassBard, "Troubadour": ClassBard,
	"Virtuoso": ClassBard, "Maestro": ClassBard,
	// Beastlord (hybrid)
	"Beastlord": ClassBeastlord, "Primalist": ClassBeastlord, "Animist": ClassBeastlord,
	"Savage Lord": ClassBeastlord,

	// Cleric (caster)
	"Cleric": ClassCleric, "Vicar": ClassCleric, "Templar": ClassCleric, "High Priest": ClassCleric,
	// Druid (caster)
	"Druid": ClassDruid, "Wanderer": ClassDruid, "Preserver": ClassDruid, "Hierophant": ClassDruid,
	// Shaman (caster)
	"Shaman": ClassShaman, "Mystic": ClassShaman, "Luminary": ClassShaman, "Defiler": ClassShaman,
	// Necromancer (caster)
	"Necromancer": ClassNecromancer, "Heretic": ClassNecromancer,
	"Warlock": ClassNecromancer, "Arch Lich": ClassNecromancer,
	// Wizard (caster)
	"Wizard": ClassWizard, "Channeler": ClassWizard, "Evoker": ClassWizard,
	"Sorcerer": ClassWizard, "Arcanist": ClassWizard,
	// Magician (caster)
	"Magician": ClassMagician, "Elementalist": ClassMagician,
	"Conjurer": ClassMagician, "Arch Convoker": ClassMagician,
	// Enchanter (caster)
	"Enchanter": ClassEnchanter, "Illusionist": ClassEnchanter, "Beguiler": ClassEnchanter,
	"Phantasmist": ClassEnchanter, "Coercer": ClassEnchanter,
}

// ClassFromTitle resolves an EQ /who level-title (e.g. "Warlord", "Phantasmist")
// to its eqclass. Returns ClassUnknown for an unrecognised title.
func ClassFromTitle(title string) Class {
	return titleToClass[strings.TrimSpace(title)]
}
