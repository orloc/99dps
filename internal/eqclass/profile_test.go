package eqclass

import "testing"

// allClasses is every real class constant — used to assert the profile table is
// complete (a missing melee/hybrid entry would silently default to CatCaster).
var allClasses = []Class{
	ClassWarrior, ClassMonk, ClassRogue,
	ClassPaladin, ClassShadowKnight, ClassRanger, ClassBard, ClassBeastlord,
	ClassCleric, ClassDruid, ClassShaman, ClassNecromancer,
	ClassWizard, ClassMagician, ClassEnchanter,
}

func TestProfileCompleteness(t *testing.T) {
	for _, c := range allClasses {
		if _, ok := profiles[c]; !ok {
			t.Errorf("class %q has no profile entry", c)
		}
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

	// Feign Death is the two feign classes only
	for _, c := range allClasses {
		want := c == ClassMonk || c == ClassNecromancer
		if got := ProfileOf(c).Feign; got != want {
			t.Errorf("%s feign = %v, want %v", c, got, want)
		}
	}
}

func TestRecastWatched(t *testing.T) {
	if !RecastWatched(ClassEnchanter, "Dictate") || !RecastWatched(ClassEnchanter, "Rapture") {
		t.Error("Enchanter should watch Dictate and Rapture")
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
