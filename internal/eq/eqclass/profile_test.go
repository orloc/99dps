package eqclass

import "testing"

func TestIsKnown(t *testing.T) {
	for _, c := range classList {
		if !IsKnown(c) {
			t.Errorf("IsKnown(%q) = false, want true", c)
		}
	}
	if IsKnown(ClassUnknown) {
		t.Error("ClassUnknown should not be a known class")
	}
	if IsKnown("Bard the Bold") {
		t.Error("a bogus class string should not be known")
	}
}

// TestCategoryOfPerClass pins each class's panel category directly (the category
// map was folded into the profile, so a mis-set Category in the table should be
// caught here, not only indirectly via the tui tests).
func TestCategoryOfPerClass(t *testing.T) {
	want := map[Class]Category{
		ClassWarrior: CatMelee, ClassMonk: CatMelee, ClassRogue: CatMelee,
		ClassPaladin: CatHybrid, ClassShadowKnight: CatHybrid, ClassRanger: CatHybrid,
		ClassBard: CatHybrid, ClassBeastlord: CatHybrid,
		ClassCleric: CatCaster, ClassDruid: CatCaster, ClassShaman: CatCaster,
		ClassNecromancer: CatCaster, ClassWizard: CatCaster, ClassMagician: CatCaster,
		ClassEnchanter: CatCaster,
	}
	for _, c := range classList {
		if got := CategoryOf(c); got != want[c] {
			t.Errorf("CategoryOf(%q) = %d, want %d", c, got, want[c])
		}
	}
	if CategoryOf(ClassUnknown) != CatCaster {
		t.Error("ClassUnknown should default to CatCaster")
	}
}

func TestProfileCompleteness(t *testing.T) {
	for _, c := range classList {
		if _, ok := profiles[c]; !ok {
			t.Errorf("class %q has no profile entry", c)
		}
	}
	// classList and profiles must stay in lockstep — a class added to one but not
	// the other (e.g. a new profile entry never added to classList) is a bug.
	if len(profiles) != len(classList) {
		t.Errorf("profiles has %d entries, classList has %d — they must match", len(profiles), len(classList))
	}
}

// TestProfileCapabilities pins the crowd-control / feign facts the cue settings
// derive from, so a future table edit can't silently drop a class's CC.
func TestProfileCapabilities(t *testing.T) {
	cc := func(c Class) [3]bool { p := ProfileOf(c); return [3]bool{p.Mez, p.Charm, p.Pacify} }
	cases := []struct {
		class Class
		want  [3]bool // mez, charm, pacify
	}{
		{ClassEnchanter, [3]bool{true, true, true}},
		{ClassNecromancer, [3]bool{true, true, true}},
		{ClassBard, [3]bool{true, true, false}},
		{ClassDruid, [3]bool{false, true, true}},
		{ClassCleric, [3]bool{false, false, true}},
		{ClassRanger, [3]bool{false, false, true}},
		{ClassWarrior, [3]bool{false, false, false}},
	}
	for _, c := range cases {
		if got := cc(c.class); got != c.want {
			t.Errorf("%s CC = %v, want %v", c.class, got, c.want)
		}
	}

	// Feign Death belongs to the three feign classes only (Monk skill; SK/Necro spell)
	for _, c := range classList {
		want := c == ClassMonk || c == ClassNecromancer || c == ClassShadowKnight
		if got := ProfileOf(c).Feign; got != want {
			t.Errorf("%s feign = %v, want %v", c, got, want)
		}
	}
}

// TestSkillName: the generic kick/strike buckets are named by the monk's level,
// and a non-monk's bucket stays generic.
func TestSkillName(t *testing.T) {
	cases := []struct {
		class Class
		gen   string
		lvl   int
		want  string
	}{
		{ClassMonk, "Kick", 60, "Flying Kick"}, {ClassMonk, "Kick", 12, "Round Kick"},
		{ClassMonk, "Kick", 3, "Kick"}, // below the lowest rank → generic
		{ClassMonk, "Strike", 60, "Dragon Punch"}, {ClassMonk, "Strike", 15, "Eagle Strike"},
		{ClassMonk, "Strike", 4, "Tiger Claw"},
		{ClassWarrior, "Kick", 60, "Kick"}, // non-monk → generic
		{ClassMonk, "Crush", 60, "Crush"},  // bucket with no override → generic
	}
	for _, c := range cases {
		if got := SkillName(c.class, c.gen, c.lvl); got != c.want {
			t.Errorf("SkillName(%s, %q, %d) = %q, want %q", c.class, c.gen, c.lvl, got, c.want)
		}
	}
}

// TestSkillRelevant: the monk hand-strike bucket shows only for monks; universal
// buckets show for everyone.
func TestSkillRelevant(t *testing.T) {
	if !SkillRelevant(ClassMonk, "Strike") {
		t.Error("Strike should be relevant for a Monk")
	}
	if SkillRelevant(ClassWarrior, "Strike") {
		t.Error("Strike should be hidden for a non-Monk")
	}
	for _, c := range []Class{ClassWarrior, ClassMonk, ClassRogue} {
		if !SkillRelevant(c, "Kick") {
			t.Errorf("Kick should be relevant for %s", c)
		}
	}
}

func TestRecastWatched(t *testing.T) {
	if !RecastWatched(ClassEnchanter, "Dictate") || !RecastWatched(ClassEnchanter, "Rapture") ||
		!RecastWatched(ClassEnchanter, "Theft of Thought") {
		t.Error("Enchanter should watch Dictate, Rapture and Theft of Thought")
	}
	if RecastWatched(ClassWarrior, "Dictate") {
		t.Error("a Warrior has no Dictate to watch")
	}
	if RecastWatched(ClassEnchanter, "Clarity") {
		t.Error("an unlisted spell must not be watched")
	}
	// pre-/who (class unknown) matches any class's watch, so detection isn't blocked
	if !RecastWatched(ClassUnknown, "Dictate") {
		t.Error("an unknown class should still watch a recognised recast spell")
	}
	if RecastWatched(ClassUnknown, "Gate") {
		t.Error("an unknown class must not watch an unlisted spell")
	}
}
