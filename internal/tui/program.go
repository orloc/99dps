package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/cues"
	"99dps/internal/eq/gamestate"
	"99dps/internal/session"
)

// Program wraps the Bubble Tea program so the host can push in a character
// switch (detected by the log watcher) while the UI is running.
type Program struct {
	p  *tea.Program
	lt *LogTail // the live-log ring, shared with the host's parser tap
}

// LogTail returns the program's live-log ring so the host can feed the parser's
// raw lines into it (the model already renders from the same ring).
func (pr *Program) LogTail() *LogTail { return pr.lt }

// NewProgram builds the program over the shared manager + tracker. spellInfo is
// the data-source summary for the footer; tts enables audio cues at startup.
// mapSrc/priceSrc are the network-bearing map/price capabilities, constructed and
// adapted by the caller (the composition root) so the UI never imports them.
func NewProgram(sm *session.SessionManager, tracker *gamestate.Tracker, character, spellInfo, eqDir string, ttsOn bool, mapSrc MapSource, priceSrc PriceSource) *Program {
	m := NewModel(sm, tracker, character)
	m.spellInfo = spellInfo
	m.eqDir = eqDir
	m.mapSrc = mapSrc
	m.pricer = priceSrc

	m.store = loadStore()
	cs := m.store.forChar(character) // this character's saved profile (or the Default)
	m.layoutPrefs = layoutPrefs{Damage: cs.Damage, OffDef: cs.OffDef, ClassBox: cs.ClassBox, DiscHidden: cs.DiscHidden}
	m.cuePrefs = cues.Prefs{Overrides: cs.Cues}
	m.screen = initialScreen(m.store.Configured)
	if m.screen == screenSetup {
		m.setup = newSetupState()
	} else if cs.Voice != "" {
		m.speaker.SetVoice(cs.Voice)
	}
	// saved choice drives cues once configured; the -tts flag is a manual force-on.
	m.ttsOn = (cs.AudioOn || ttsOn) && m.speaker.Available()

	return &Program{p: tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()), lt: m.logTail}
}

// Run launches the UI; blocks until the user quits.
func (pr *Program) Run() error {
	_, err := pr.p.Run()
	return err
}

// Kill stops the program immediately, restoring the terminal (leaves raw mode,
// the alt screen, and mouse reporting). Used as a safety net so a background
// goroutine panic can't leave the terminal wedged. Run returns after this.
func (pr *Program) Kill() { pr.p.Kill() }

// SwitchCharacter notifies the running UI that a different character's log is now
// active. Safe to call from another goroutine. No-op once the program has quit.
func (pr *Program) SwitchCharacter(name string) {
	pr.p.Send(switchMsg{character: name})
}
