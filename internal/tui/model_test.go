package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"99dps/internal/combat"
	"99dps/internal/session"
)

// sampleManager builds a session manager with a few dealers, spread over a span.
func sampleManager() *session.SessionManager {
	sm := &session.SessionManager{}
	rows := []struct {
		dealer string
		dmg    int
		at     int64
	}{
		{"You", 520_000, 1000}, {"Gabnador", 349_000, 1008}, {"Borric", 190_000, 1016},
		{"Mourngul", 168_000, 1024}, {"Faelyn", 133_000, 1032}, {"a pet", 77_000, 1042},
	}
	for _, r := range rows {
		sm.Apply(&combat.DamageSet{ActionTime: r.at, Dealer: r.dealer, Dmg: r.dmg, Target: "a sand giant"})
	}
	return sm
}

// renderAt drives the model through a window-size + tick so View() has content.
func renderAt(sm *session.SessionManager, themeIdx, w, h int) string {
	var m tea.Model = New(sm, nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	mm := m.(Model)
	mm.theme = themeIdx
	mm.refresh()
	return mm.View()
}

func TestDamagePanelRendersLiveData(t *testing.T) {
	out := renderAt(sampleManager(), 0, 92, 30)
	for _, want := range []string{"a sand giant", "You", "Gabnador", "dps", "99dps", "Kunark Gold"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered Damage panel missing %q", want)
		}
	}
	// a nil/empty manager must not panic and should show the placeholder
	if out := renderAt(&session.SessionManager{}, 0, 80, 24); !strings.Contains(out, "No fight") {
		t.Errorf("empty manager should render a placeholder, got:\n%s", out)
	}
}

// TestWriteShots dumps a truecolor frame per theme for screenshotting with
// `freeze`. Gated behind TUI_SHOT so it doesn't run (or write) in normal CI.
func TestWriteShots(t *testing.T) {
	if os.Getenv("TUI_SHOT") == "" {
		t.Skip("set TUI_SHOT=1 to write /tmp/tui<theme>.ansi previews")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	sm := sampleManager()
	for i := range themes {
		_ = time.Now()
		path := fmt.Sprintf("/tmp/tui%d.ansi", i)
		if err := os.WriteFile(path, []byte(renderAt(sm, i, 92, 30)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
