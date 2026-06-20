package cues

import (
	"testing"

	"99dps/internal/eq/eqclass"
)

func TestCuePrefsDefaultAndOverride(t *testing.T) {
	var c Prefs // zero value: everything at its default

	if !c.Enabled("cc.mez", true) {
		t.Error("unset cue should take its default (true)")
	}
	if c.Enabled("cd.ready.Kick", false) {
		t.Error("unset cue should take its default (false)")
	}

	// toggling a default-on cue stores an explicit off
	c.Toggle("cc.mez", true)
	if c.Enabled("cc.mez", true) {
		t.Error("toggled cue should now be off")
	}
	if v, ok := c.Overrides["cc.mez"]; !ok || v {
		t.Errorf("expected an explicit false override, got %v ok=%v", v, ok)
	}

	// toggling it back to its default drops the override entirely (minimal file)
	c.Toggle("cc.mez", true)
	if !c.Enabled("cc.mez", true) {
		t.Error("toggled back should restore the default (on)")
	}
	if _, ok := c.Overrides["cc.mez"]; ok {
		t.Error("an override matching the default should be removed, not stored")
	}
}

// TestCueRowsCoverCatalog ensures the matrix is data-driven off the catalog: each
// catalog skill yields a ready + halfway toggle row, and the fixed categories are
// present. Adding a skill to the registry should surface here automatically.
func TestCueRowsCoverCatalog(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range Toggles(Rows(eqclass.ClassUnknown)) {
		ids[r.ID] = true
	}
	for _, want := range []string{
		cueCDReady("Mend"), cueCDHalf("Mend"),
		cueCDReady("Feign Death"), cueCDHalf("Feign Death"),
		cueCharm, cueMez, cuePacify, cueBuffFade, cueDebuffFade, cueFeignFail, cueResist,
	} {
		if !ids[want] {
			t.Errorf("cue matrix missing toggle %q", want)
		}
	}
	// class-agnostic action skills (CooldownInfo.AnyClass) are tracked in the panel
	// but deliberately kept OUT of the per-class cue matrix — their reuse is too
	// short to cue. Guard the exclusion so it can't silently regress.
	for _, skip := range []string{"Taunt", "Evade", "Pick Pocket"} {
		if ids[cueCDReady(skip)] || ids[cueCDHalf(skip)] {
			t.Errorf("anyClass cooldown %q must not appear in the cue matrix", skip)
		}
	}
}

// TestCueRowsByClass: the matrix shows only what a class can produce.
func TestCueRowsByClass(t *testing.T) {
	has := func(class eqclass.Class, id string) bool {
		for _, r := range Toggles(Rows(class)) {
			if r.ID == id {
				return true
			}
		}
		return false
	}

	// Monk: cooldowns + feign alert; no CC, no buff fades, no resist.
	if !has(eqclass.ClassMonk, cueCDReady("Mend")) || !has(eqclass.ClassMonk, cueFeignFail) {
		t.Error("Monk should have cooldown + feign cues")
	}
	if has(eqclass.ClassMonk, cueMez) || has(eqclass.ClassMonk, cueBuffFade) || has(eqclass.ClassMonk, cueResist) {
		t.Error("Monk should not have CC / buff / resist cues")
	}
	// Enchanter: all three CC types.
	for _, id := range []string{cueMez, cueCharm, cuePacify} {
		if !has(eqclass.ClassEnchanter, id) {
			t.Errorf("Enchanter should have %q", id)
		}
	}
	// Necromancer: mez + charm + pacify (undead) and a feign alert.
	if !has(eqclass.ClassNecromancer, cueMez) || !has(eqclass.ClassNecromancer, cueFeignFail) {
		t.Error("Necromancer should have undead mez + feign")
	}
	// Cleric: pacify only.
	if !has(eqclass.ClassCleric, cuePacify) || has(eqclass.ClassCleric, cueMez) || has(eqclass.ClassCleric, cueCharm) {
		t.Error("Cleric should have pacify only among CC")
	}
	// Druid: charm + pacify, no mez.
	if !has(eqclass.ClassDruid, cueCharm) || !has(eqclass.ClassDruid, cuePacify) || has(eqclass.ClassDruid, cueMez) {
		t.Error("Druid should have charm + pacify, not mez")
	}
	// Bard: mez + charm, no pacify.
	if !has(eqclass.ClassBard, cueMez) || !has(eqclass.ClassBard, cueCharm) || has(eqclass.ClassBard, cuePacify) {
		t.Error("Bard should have mez + charm, not pacify")
	}
	// Warrior: no spells → only the universal spawn cue.
	wt := Toggles(Rows(eqclass.ClassWarrior))
	if len(wt) != 1 || wt[0].ID != cueSpawnSoon {
		t.Errorf("Warrior should have only the spawn cue, got %+v", wt)
	}
	// the spawn cue is universal.
	if !has(eqclass.ClassWizard, cueSpawnSoon) {
		t.Error("every class should get the spawn cue")
	}
}
