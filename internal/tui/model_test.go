package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
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
	return renderAtTr(sm, nil, themeIdx, w, h)
}

func renderAtTr(sm *session.SessionManager, tr *gamestate.Tracker, themeIdx, w, h int) string {
	var m tea.Model = New(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	mm := m.(Model)
	mm.theme = themeIdx
	mm.refresh()
	return mm.View()
}

// monkScene is a melee (Monk) scenario: skill attacks + a Mend cooldown, so the
// class-aware panel shows SKILLS + COOLDOWNS instead of spell timers.
func monkScene() (*session.SessionManager, *gamestate.Tracker) {
	sm := sampleManager()
	sm.Apply(&combat.DamageSet{ActionTime: 1043, Dealer: "You", Dmg: 90, Target: "a sand giant", Verb: "kick"})
	sm.Apply(&combat.DamageSet{ActionTime: 1044, Dealer: "You", Dmg: 70, Target: "a sand giant", Verb: "strike"})
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassMonk)
	tr.Observe("You mend your wounds and heal some damage.", 1044) // starts the Mend cooldown
	return sm, tr
}

func TestPanelsRenderLiveData(t *testing.T) {
	out := renderAt(sampleManager(), 0, 100, 32)
	for _, want := range []string{
		"99dps",           // banner
		"a sand giant",    // sessions list + damage title
		"You", "Gabnador", // damage rows
		"Now", "Sessions", "Spell Timers", "Mob Tracker", // panel titles
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered layout missing %q", want)
		}
	}
	// an empty manager must not panic and should show a placeholder
	if out := renderAt(&session.SessionManager{}, 0, 90, 28); !strings.Contains(out, "No fight") {
		t.Errorf("empty manager should render a placeholder, got:\n%s", out)
	}
}

// TestClassAwarePanels verifies the bottom panel switches on class: a melee
// class shows SKILLS + COOLDOWNS, and an enchanter gets a Crowd Control column.
func TestClassAwarePanels(t *testing.T) {
	sm, tr := monkScene()
	out := renderAtTr(sm, tr, 0, 110, 34)
	for _, want := range []string{"Skills", "SKILLS", "COOLDOWNS", "Mend"} {
		if !strings.Contains(out, want) {
			t.Errorf("monk panel missing %q", want)
		}
	}

	book, _ := gamestate.LoadReader(strings.NewReader(""))
	ench := gamestate.NewTracker(book)
	ench.SetClass(eqclass.ClassEnchanter)
	if out := renderAtTr(sampleManager(), ench, 0, 110, 34); !strings.Contains(out, "Crowd Control") {
		t.Errorf("enchanter layout should include a Crowd Control column")
	}
}

// TestWriteShots dumps truecolor frames for screenshotting with `freeze`. Gated
// behind TUI_SHOT so it doesn't run (or write) in normal CI.
func TestWriteShots(t *testing.T) {
	if os.Getenv("TUI_SHOT") == "" {
		t.Skip("set TUI_SHOT=1 to write /tmp/tui<theme>.ansi previews")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	_ = time.Now()
	// theme 0/1: caster scenario; theme 2: a Monk (melee) scenario showing the
	// class-aware Skills + Cooldowns panel.
	if err := os.WriteFile("/tmp/tui0.ansi", []byte(renderAt(sampleManager(), 0, 108, 34)), 0o644); err != nil {
		t.Fatal(err)
	}
	smMonk, trMonk := monkScene()
	if err := os.WriteFile("/tmp/tui-monk.ansi", []byte(renderAtTr(smMonk, trMonk, 0, 108, 34)), 0o644); err != nil {
		t.Fatal(err)
	}
}
