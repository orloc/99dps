package eqclass

import "testing"

func TestClassFromTitleAndCategory(t *testing.T) {
	cases := []struct {
		title string
		class Class
		cat   Category
	}{
		{"Warlord", ClassWarrior, CatMelee},
		{"Grandmaster", ClassMonk, CatMelee},
		{"Disciple", ClassMonk, CatMelee}, // the 51-rank monk title (regression: was unmapped → caster)
		{"Assassin", ClassRogue, CatMelee},
		{"Wanderer", ClassDruid, CatCaster}, // Druid, NOT Ranger — the easy trap
		{"Phantasmist", ClassEnchanter, CatCaster},
		{"Beguiler", ClassEnchanter, CatCaster},
		{"Channeler", ClassWizard, CatCaster},
		{"Sorcerer", ClassWizard, CatCaster},
		{"Defiler", ClassShaman, CatCaster},
		{"Reaver", ClassShadowKnight, CatHybrid},
		{"Minstrel", ClassBard, CatHybrid},
		{"Wizard", ClassWizard, CatCaster},              // base name (low levels)
		{"Shadow Knight", ClassShadowKnight, CatHybrid}, // multi-word base name
	}
	for _, c := range cases {
		got := ClassFromTitle(c.title)
		if got != c.class {
			t.Errorf("ClassFromTitle(%q) = %q, want %q", c.title, got, c.class)
		}
		if cat := CategoryOf(got); cat != c.cat {
			t.Errorf("CategoryOf(%q) = %d, want %d", got, cat, c.cat)
		}
	}

	// an unrecognised title resolves to ClassUnknown → CatCaster (safe default)
	if got := ClassFromTitle("Notatitle"); got != ClassUnknown {
		t.Errorf("unknown title = %q, want ClassUnknown", got)
	}
	if CategoryOf(ClassUnknown) != CatCaster {
		t.Error("ClassUnknown should default to CatCaster")
	}
}
