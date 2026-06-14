package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/tts"
)

// TestSwitchReloadsCharacterSettings: a character hot-swap loads that toon's own
// saved layout/cues/audio, so settings really are per-character at runtime.
func TestSwitchReloadsCharacterSettings(t *testing.T) {
	var m tea.Model = New(sampleManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	mm := m.(Model)
	mm.store.setChar("Kelkix", charSettings{Damage: panelFull})
	mm.store.setChar("Iznoa", charSettings{Damage: panelOff, OffDef: panelCompact, Cues: map[string]bool{"cc.mez": false}})
	mm.layoutPrefs = layoutPrefs{Damage: panelFull}

	out, _ := mm.Update(switchMsg{character: "Iznoa"})
	got := out.(Model)
	if got.layoutPrefs.Damage != panelOff || got.layoutPrefs.OffDef != panelCompact {
		t.Errorf("switch should load Iznoa's layout, got %+v", got.layoutPrefs)
	}
	if got.cues.enabled("cc.mez", true) {
		t.Error("switch should load Iznoa's cue overrides (mez off)")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// fresh install: nothing saved → zero store (drives onboarding)
	if s := loadStore(); s.Configured {
		t.Fatalf("fresh install should be unconfigured, got %+v", s)
	}

	s := store{Configured: true}
	s.setChar("Kelkix", charSettings{
		AudioOn: true, Voice: "5", Damage: panelCompact, OffDef: panelOff,
		Cues: map[string]bool{"cc.mez": false},
	})
	if err := saveStore(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := loadStore().forChar("Kelkix")
	if !got.AudioOn || got.Voice != "5" || got.Damage != panelCompact || got.OffDef != panelOff {
		t.Errorf("round-trip lost data: %+v", got)
	}
	if got.Cues["cc.mez"] {
		t.Error("cue override should round-trip as false")
	}
}

func TestStorePerCharacterIsolation(t *testing.T) {
	var s store
	s.Default = charSettings{AudioOn: true, Voice: "1"} // onboarding baseline
	s.setChar("Kelkix", charSettings{AudioOn: false, Voice: "8", Damage: panelOff})

	if k := s.forChar("Kelkix"); k.AudioOn || k.Voice != "8" || k.Damage != panelOff {
		t.Errorf("Kelkix should use its own settings, got %+v", k)
	}
	// an unseen alt inherits the Default baseline, untouched by Kelkix's overrides
	if alt := s.forChar("Iznoa"); !alt.AudioOn || alt.Voice != "1" || alt.Damage != panelFull {
		t.Errorf("a new character should inherit Default, got %+v", alt)
	}
}

// TestStoreMigratesLegacyTTS: an existing user's legacy tts.json is folded into
// the Default profile on first load, so they keep their voice/audio choice and
// don't get re-onboarded after upgrading.
func TestStoreMigratesLegacyTTS(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := tts.SavePrefs(tts.Prefs{Configured: true, Enabled: true, Voice: "7"}); err != nil {
		t.Fatalf("seed legacy prefs: %v", err)
	}

	s := loadStore() // no settings.json yet → migrates from tts.json
	if !s.Configured {
		t.Error("migration should carry the configured flag (no re-onboarding)")
	}
	if d := s.Default; !d.AudioOn || d.Voice != "7" {
		t.Errorf("migration should carry audio/voice into Default, got %+v", d)
	}
}
