package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/tts"
)

func key(m Model, t tea.KeyType) Model {
	tm, _ := m.updateSetup(tea.KeyMsg{Type: t})
	return tm.(Model)
}

func TestInitialScreen(t *testing.T) {
	if initialScreen(true) != screenMeter {
		t.Error("a configured install should open straight to the meter")
	}
	if initialScreen(false) != screenSetup {
		t.Error("a fresh install should open the setup screen")
	}
}

func TestSetupMenuNavigation(t *testing.T) {
	m := Model{screen: screenSetup, setup: newSetupState()}
	if m = key(m, tea.KeyDown); m.setup.sel != 1 {
		t.Errorf("down should select Skip (1), got %d", m.setup.sel)
	}
	if m = key(m, tea.KeyUp); m.setup.sel != 0 {
		t.Errorf("up should select Enable (0), got %d", m.setup.sel)
	}
}

func TestSetupSkipPersistsAndEntersMeter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSetup, setup: setupState{phase: phaseMenu, sel: 1}} // Skip
	m = key(m, tea.KeyEnter)

	if m.screen != screenMeter {
		t.Error("skipping should enter the meter")
	}
	if s := loadStore(); !s.Configured || s.Default.AudioOn {
		t.Errorf("skip should persist configured+disabled, got %+v", s)
	}
}

func TestSetupVoiceNavigationClamps(t *testing.T) {
	m := Model{screen: screenSetup, setup: setupState{
		phase:  phaseVoice,
		voices: []tts.Voice{{ID: "0", Name: "Voice 0"}, {ID: "1", Name: "Voice 1"}},
	}}
	m = key(m, tea.KeyUp) // already at top → stays 0
	if m.setup.sel != 0 {
		t.Errorf("up at top should clamp to 0, got %d", m.setup.sel)
	}
	m = key(m, tea.KeyDown)
	m = key(m, tea.KeyDown) // past the end → clamps to last
	if m.setup.sel != 1 {
		t.Errorf("down past end should clamp to 1, got %d", m.setup.sel)
	}
}

func TestSetupViewFitsWindow(t *testing.T) {
	states := []setupState{
		newSetupState(),
		{phase: phaseDownloading, dlLabel: "voice", dlBytes: 42 * 1024 * 1024},
		{phase: phaseVoice, voices: []tts.Voice{{ID: "0", Name: "Voice 0"}, {ID: "1", Name: "Voice 1"}}},
		{phase: phaseError, err: errString("network unreachable while fetching a long url")},
	}
	for _, w := range []int{40, 60, 80, 120} {
		for _, st := range states {
			m := Model{screen: screenSetup, setup: st, w: w, h: 24, ready: true}
			for _, ln := range strings.Split(m.View(), "\n") {
				if lipgloss.Width(ln) > w {
					t.Errorf("phase %d w=%d: line exceeds width (%d): %q", st.phase, w, lipgloss.Width(ln), ln)
				}
			}
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
