package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/tts"
)

// setupModel builds a first-run Model in the given phase with a ready fake voice
// engine, so the onboarding keyboard flow can be driven without the real engine.
func setupModel(phase setupPhase, voices []tts.Voice) Model {
	return Model{
		screen:  screenSetup,
		speaker: &fakeEngine{},
		setup:   setupState{phase: phase, voices: voices},
	}
}

// TestSetupEnableGoesToVoicePick: choosing "enable" when the engine is already
// downloaded jumps straight to the voice picker (no download).
func TestSetupEnableGoesToVoicePick(t *testing.T) {
	m := setupModel(phaseMenu, nil)
	m.setup.sel = 0 // enable
	out := key(m, tea.KeyEnter)
	if out.setup.phase != phaseVoice {
		t.Errorf("enable with engine available should go to voice pick, got phase %d", out.setup.phase)
	}
}

// TestSetupVoicePickPersistsAndEnters: picking a voice saves it as the Default
// profile (configured + audio on + that voice) and enters the meter.
func TestSetupVoicePickPersistsAndEnters(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := setupModel(phaseVoice, []tts.Voice{{ID: "0", Name: "A"}, {ID: "5", Name: "B"}})
	m.setup.sel = 1 // pick voice "5"
	out := key(m, tea.KeyEnter)

	if out.screen != screenMeter {
		t.Error("picking a voice should enter the meter")
	}
	if !out.ttsOn {
		t.Error("audio should be on after picking a voice (engine available)")
	}
	s := loadStore()
	if !s.Configured || !s.Default.AudioOn || s.Default.Voice != "5" {
		t.Errorf("voice pick should persist as the Default profile, got %+v", s)
	}
}

// TestSetupErrorSkipPersists: giving up at the error screen saves configured +
// audio-off and enters the meter (no retry/download).
func TestSetupErrorSkipPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := setupModel(phaseError, nil)
	tm, _ := m.updateSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	out := tm.(Model)

	if out.screen != screenMeter {
		t.Error("skipping at the error screen should enter the meter")
	}
	if s := loadStore(); !s.Configured || s.Default.AudioOn {
		t.Errorf("error-skip should persist configured+disabled, got %+v", s)
	}
}

// TestSetupPreviewVoice: 'p' on the voice picker previews the highlighted voice.
func TestSetupPreviewVoice(t *testing.T) {
	m := setupModel(phaseVoice, []tts.Voice{{ID: "0", Name: "A"}, {ID: "1", Name: "B"}})
	m.setup.sel = 1
	fe := m.speaker.(*fakeEngine)
	tm, _ := m.updateSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	_ = tm
	if len(fe.normal) != 1 || !strings.Contains(strings.ToLower(fe.normal[0]), "audio") {
		t.Errorf("preview should speak a test phrase, got %v", fe.normal)
	}
}

func TestSelectedVoice(t *testing.T) {
	m := setupModel(phaseVoice, []tts.Voice{{ID: "0", Name: "A"}, {ID: "1", Name: "B"}})
	m.setup.sel = 1
	if v, ok := m.selectedVoice(); !ok || v.ID != "1" {
		t.Errorf("selectedVoice(1) = %+v ok=%v, want B/1", v, ok)
	}
	m.setup.sel = 5 // out of range
	if _, ok := m.selectedVoice(); ok {
		t.Error("an out-of-range selection should report not-ok")
	}
}

// TestToggleTTSNoVoice: with no available engine, toggling audio flashes guidance
// and leaves audio off (the only branch the fake engine can't exercise).
func TestToggleTTSNoVoice(t *testing.T) {
	m := Model{} // nil speaker
	m.toggleTTS()
	if m.ttsOn {
		t.Error("audio should stay off with no engine")
	}
	if !strings.Contains(m.status, "no voice") {
		t.Errorf("expected a no-voice hint, got %q", m.status)
	}
}
