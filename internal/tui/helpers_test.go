package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
	"99dps/internal/tts"
)

// Shared test helpers used by more than one <source>_test.go in this package.
// Helpers exercised by a single source file live next to that file's tests.

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
	// specials so the Specials breakdown has content (per dealer, per kind, with a
	// hit rate) — also exercised by the overflow guard.
	sm.Apply(&combat.DamageSet{ActionTime: 1040, Dealer: "Borric", Dmg: 5_000, Target: "a sand giant", Verb: "backstab"})
	sm.Apply(&combat.DamageSet{ActionTime: 1041, Dealer: "You", Dmg: 1_400, Target: "a sand giant", Verb: "kick"})
	sm.Apply(&combat.DamageSet{ActionTime: 1042, Dealer: "You", Dmg: 1_200, Target: "a sand giant", Verb: "kick"})
	sm.ApplySwing(&combat.Swing{ActionTime: 1043, Attacker: "You", Defender: "a sand giant", Verb: "kick", Outcome: combat.OutcomeMiss})
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
	mm.layoutPrefs.DiscHidden = true // the discipline widget has its own tests; keep layout tests focused
	mm.refresh()
	return mm.View()
}

func twoSessionManager() *session.SessionManager {
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 1000, Target: "a rat"})
	sm.Apply(&combat.DamageSet{ActionTime: 9000, Dealer: "You", Dmg: 2000, Target: "a bat"})
	return sm
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

// fakeEngine records which cues went to the normal vs urgent voice, and how many
// times the queue was flushed.
type fakeEngine struct {
	normal, urgent []string
	flushed        int
}

func (f *fakeEngine) Say(s string)         { f.normal = append(f.normal, s) }
func (f *fakeEngine) SayUrgent(s string)   { f.urgent = append(f.urgent, s) }
func (f *fakeEngine) Available() bool      { return true }
func (f *fakeEngine) Voices() []tts.Voice  { return nil }
func (f *fakeEngine) Voice() string        { return "" }
func (f *fakeEngine) SetVoice(string) bool { return false }
func (f *fakeEngine) Flush()               { f.flushed++; f.normal = nil }
