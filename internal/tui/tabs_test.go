package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/session"
	"99dps/internal/tts"
)

func TestTabAtMatchesBar(t *testing.T) {
	m := Model{screen: screenMeter}
	for _, h := range tabHits() {
		mid := (h.x0 + h.x1) / 2
		if scr, ok := m.tabAt(mid, tabRow); !ok || scr != h.scr {
			t.Errorf("tabAt(%d, %d) = (%v,%v), want %v", mid, tabRow, scr, ok, h.scr)
		}
	}
	// off the tab row → no hit
	if _, ok := m.tabAt(tabHits()[0].x0, tabRow+1); ok {
		t.Error("tabAt off the tab row should miss")
	}
	// far right → no hit
	if _, ok := m.tabAt(9999, tabRow); ok {
		t.Error("tabAt past the last tab should miss")
	}
}

func TestNextTabToggles(t *testing.T) {
	m := Model{screen: screenMeter}
	if m.nextTab() != screenSettings {
		t.Error("from meter, next tab should be settings")
	}
	m.screen = screenSettings
	if m.nextTab() != screenMeter {
		t.Error("from settings, next tab should be meter")
	}
}

func TestTabKeyNavigation(t *testing.T) {
	var m tea.Model = New(&session.SessionManager{}, nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.(Model).screen != screenSettings {
		t.Error("key 2 should open Settings")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.(Model).screen != screenMeter {
		t.Error("tab should toggle back to Meter")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.(Model).screen != screenMeter {
		t.Error("key 1 should stay on Meter")
	}
}

func TestTabClickSwitchesScreen(t *testing.T) {
	var m tea.Model = New(&session.SessionManager{}, nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	settingsTab := tabHits()[1]
	mid := (settingsTab.x0 + settingsTab.x1) / 2
	m, _ = m.Update(tea.MouseMsg{X: mid, Y: tabRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.(Model).screen != screenSettings {
		t.Errorf("clicking the Settings tab should switch screens, got %v", m.(Model).screen)
	}
}

func TestSettingsViewFitsWindow(t *testing.T) {
	for _, w := range []int{60, 80, 120} {
		m := Model{screen: screenSettings, ready: true, w: w, h: 30, speaker: tts.New()}
		for _, ln := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(ln) > w {
				t.Errorf("w=%d: settings line exceeds width (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}
