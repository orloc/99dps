package tui

import "testing"

func TestCuePrefsDefaultAndOverride(t *testing.T) {
	var c cuePrefs // zero value: everything at its default

	if !c.enabled("cc.mez", true) {
		t.Error("unset cue should take its default (true)")
	}
	if c.enabled("cd.ready.Kick", false) {
		t.Error("unset cue should take its default (false)")
	}

	// toggling a default-on cue stores an explicit off
	c.toggle("cc.mez", true)
	if c.enabled("cc.mez", true) {
		t.Error("toggled cue should now be off")
	}
	if v, ok := c.Overrides["cc.mez"]; !ok || v {
		t.Errorf("expected an explicit false override, got %v ok=%v", v, ok)
	}

	// toggling it back to its default drops the override entirely (minimal file)
	c.toggle("cc.mez", true)
	if !c.enabled("cc.mez", true) {
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
	for _, r := range cueToggles(cueRows()) {
		ids[r.id] = true
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
}
