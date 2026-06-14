package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/gamestate"
)

// TestDueAnnouncementsGatesByType: a disabled cue type stays silent while other
// types still fire from the same tick.
func TestDueAnnouncementsGatesByType(t *testing.T) {
	m := &Model{
		announced: map[string]bool{},
		cues:      cuePrefs{Overrides: map[string]bool{cueMez: false}}, // mez cues off
	}
	now := int64(1000)
	mez := gamestate.Timer{Spell: "Lull", Target: "a gnoll", Start: now - 100, Expiry: now + 5, Mez: true}
	deb := gamestate.Timer{Spell: "Cripple", Target: "a gnoll", Start: now - 100, Expiry: now + 5, Detrimental: true}

	due := m.dueAnnouncements([]gamestate.Timer{mez, deb}, now)
	if len(due) != 1 || due[0].Spell != "Cripple" {
		t.Fatalf("mez disabled → only the debuff should be due, got %+v", due)
	}
}

// TestHalfCooldownCue: the 50% cue fires once at the halfway mark, then the ready
// cue fires once at expiry.
func TestHalfCooldownCue(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000) // Mend, 360s reuse
	m := &Model{ttsOn: true, tracker: tr, speaker: fe}

	m.announceCuesAt(1000)       // baseline: on cooldown, full
	m.announceCuesAt(1000 + 181) // crossed halfway (179s left ≤ 180)
	if len(fe.normal) != 1 || !strings.Contains(strings.ToLower(fe.normal[0]), "halfway") {
		t.Fatalf("halfway cue expected, got %v", fe.normal)
	}
	m.announceCuesAt(1000 + 182) // still past halfway → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("halfway cue should fire once, got %v", fe.normal)
	}
	m.announceCuesAt(1000 + 361) // ready
	if len(fe.normal) != 2 || !strings.Contains(fe.normal[1], "ready") {
		t.Fatalf("ready cue expected after halfway, got %v", fe.normal)
	}
}

// TestCooldownReadyCueDisabled: turning a skill's ready cue off silences it.
func TestCooldownReadyCueDisabled(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000)
	m := &Model{
		ttsOn: true, tracker: tr, speaker: fe,
		cues: cuePrefs{Overrides: map[string]bool{
			cueCDReady("Mend"): false,
			cueCDHalf("Mend"):  false,
		}},
	}
	m.announceCuesAt(1000)
	m.announceCuesAt(1000 + 181) // halfway, disabled
	m.announceCuesAt(1000 + 361) // ready, disabled
	if len(fe.normal) != 0 {
		t.Errorf("disabled Mend cues should be silent, got %v", fe.normal)
	}
}

// TestSettingsRightColumnToggle drives the keyboard path: focus the cue column,
// toggle the first cue, and confirm it persisted as an override.
func TestSettingsRightColumnToggle(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSettings}

	send := func(k tea.KeyMsg) {
		tm, _ := m.updateSettings(k)
		m = tm.(Model)
	}
	send(tea.KeyMsg{Type: tea.KeyRight}) // focus the cue column
	if m.settingsCol != 1 {
		t.Fatalf("→ should focus the cue column, got col %d", m.settingsCol)
	}
	send(tea.KeyMsg{Type: tea.KeyEnter}) // toggle the first cue (Mend ready, default on → off)

	first := cueToggles(cueRows())[0]
	if m.cues.enabled(first.id, first.def) {
		t.Errorf("first cue (%s) should now be off", first.id)
	}
	if loadCuePrefs().enabled(first.id, first.def) {
		t.Errorf("the cue toggle should persist to disk")
	}
}
