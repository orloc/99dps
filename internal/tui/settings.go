package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/tts"
)

// tabRow is the screen Y of the tab bar: outer pad (row 0) + banner (row 1) →
// the tabs sit on row 2 (and the grid starts at gridTop = 3).
const tabRow = 2

// tabs are the navigable top-level screens. The first-run setup is intentionally
// not a tab — it's one-time onboarding, not a place you switch back to.
var tabs = []struct {
	scr   screen
	label string
}{
	{screenMeter, "Meter"},
	{screenSessions, "Sessions"},
	{screenSettings, "Settings"},
}

// tabHit is a tab's clickable column range, [x0, x1).
type tabHit struct {
	scr    screen
	x0, x1 int
}

// tabHits returns each tab's clickable range, matching exactly how tabBar lays
// the labels out: " label ", single-space separated, starting at the content's
// left edge (x=1, just inside the outer padding).
func tabHits() []tabHit {
	hits := make([]tabHit, 0, len(tabs))
	x := 1
	for _, t := range tabs {
		w := len(t.label) + 2 // " label "
		hits = append(hits, tabHit{t.scr, x, x + w})
		x += w + 1 // one-space gap between tabs
	}
	return hits
}

// tabBar renders the clickable tab strip; the active tab is highlighted.
func (m Model) tabBar(th theme) string {
	active := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color(th.bg)).Background(lipgloss.Color(th.accent))
	inactive := th.fg(th.dim)

	parts := make([]string, 0, len(tabs)*2)
	for i, t := range tabs {
		if i > 0 {
			parts = append(parts, " ")
		}
		label := " " + t.label + " "
		if t.scr == m.screen {
			parts = append(parts, active.Render(label))
		} else {
			parts = append(parts, inactive.Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// tabAt reports which screen a click landed on, if it hit the tab bar.
func (m Model) tabAt(x, y int) (screen, bool) {
	if y != tabRow {
		return 0, false
	}
	for _, h := range tabHits() {
		if x >= h.x0 && x < h.x1 {
			return h.scr, true
		}
	}
	return 0, false
}

// cycleTab returns the tab dir steps from the current one (wrapping); +1 = next,
// -1 = previous.
func (m Model) cycleTab(dir int) screen {
	for i, t := range tabs {
		if t.scr == m.screen {
			return tabs[(i+dir+len(tabs))%len(tabs)].scr
		}
	}
	return screenMeter
}

// gotoScreen switches the active screen, (re)sizing + refreshing the destination
// tab so its first frame is current.
func (m Model) gotoScreen(scr screen) Model {
	m.screen = scr
	if !m.ready {
		return m
	}
	switch scr {
	case screenMeter:
		m.resizeViewports()
		m.refresh()
	case screenSessions:
		m.resizeViewports()
		m.refreshSessions()
	}
	return m
}

// --- Settings tab ---

// settingsVoices is the engine's voice catalog (nil when no engine).
func (m Model) settingsVoices() []tts.Voice {
	if m.speaker == nil {
		return nil
	}
	return m.speaker.Voices()
}

// The Settings rows: a few fixed controls, then the voice list.
const (
	settingsAudioRow  = 0
	settingsDamageRow = 1 // Damage meter: Full/Compact/Off
	settingsOffDefRow = 2 // Offense·Defense: Full/Compact/Off
	settingsFixedRows = 3 // rows before the voice list
)

// updateSettings handles input while the Settings tab is focused (tab-navigation
// keys are consumed earlier, in Update).
func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	voices := m.settingsVoices()
	last := settingsFixedRows + len(voices) - 1
	switch km.String() {
	case "up", "k":
		if m.settingsSel > 0 {
			m.settingsSel--
		}
	case "down", "j":
		if m.settingsSel < last {
			m.settingsSel++
		}
	case "enter", " ":
		switch m.settingsSel {
		case settingsAudioRow:
			m.toggleTTS()
			m.persistAudioPrefs()
		case settingsDamageRow:
			m.layoutPrefs.Damage = m.layoutPrefs.Damage.next()
			_ = saveLayoutPrefs(m.layoutPrefs)
			m.flash("Damage meter: " + m.layoutPrefs.Damage.String())
		case settingsOffDefRow:
			m.layoutPrefs.OffDef = m.layoutPrefs.OffDef.next()
			_ = saveLayoutPrefs(m.layoutPrefs)
			m.flash("Offense · Defense: " + m.layoutPrefs.OffDef.String())
		default:
			if v, ok := voiceIndex(voices, m.settingsSel-settingsFixedRows); ok {
				m.speaker.SetVoice(v.ID)
				m.flash("voice: " + v.Name)
				m.persistAudioPrefs()
			}
		}
	case "p":
		if v, ok := voiceIndex(voices, m.settingsSel-settingsFixedRows); ok {
			m.speaker.SetVoice(v.ID)
			m.speaker.Say("Audio cue test.")
		}
	}
	return m, nil
}

func voiceIndex(voices []tts.Voice, i int) (tts.Voice, bool) {
	if i < 0 || i >= len(voices) {
		return tts.Voice{}, false
	}
	return voices[i], true
}

// persistAudioPrefs saves the current audio choice so it survives a restart.
func (m *Model) persistAudioPrefs() {
	voice := ""
	if m.speaker != nil {
		voice = m.speaker.Voice()
	}
	_ = tts.SavePrefs(tts.Prefs{Configured: true, Enabled: m.ttsOn, Voice: voice})
}

// settingsView renders the Settings tab body. Each line is truncated to w before
// coloring so the colored output still fits.
func (m Model) settingsView(th theme, w int) string {
	audio := "off"
	if m.speaker == nil || !m.speaker.Available() {
		audio = "no voice — run -tts-setup"
	} else if m.ttsOn {
		audio = "on"
	}

	out := []string{
		paint(th, th.accent, "Audio cues", w),
		"",
		menuRow(th, m.settingsSel == settingsAudioRow, "Audio cues: "+audio, w),
		"",
		paint(th, th.accent, "Meter boxes", w),
		"",
		menuRow(th, m.settingsSel == settingsDamageRow, "Damage meter: "+m.layoutPrefs.Damage.String(), w),
		menuRow(th, m.settingsSel == settingsOffDefRow, "Offense · Defense: "+m.layoutPrefs.OffDef.String(), w),
		"",
		paint(th, th.accent, "Voice", w),
	}
	out = append(out, m.settingsVoiceLines(th, w)...)
	out = append(out, "", paint(th, th.dim, "↑/↓ move · enter toggle/cycle/select · p preview · tab or click to switch", w))
	return lines(out...)
}

// settingsVoiceLines renders a windowed voice list, marking the current voice (●)
// and the cursor (▸).
func (m Model) settingsVoiceLines(th theme, w int) []string {
	voices := m.settingsVoices()
	if len(voices) == 0 {
		return []string{paint(th, th.dim, "  (download a voice first)", w)}
	}
	cur := ""
	if m.speaker != nil {
		cur = m.speaker.Voice()
	}
	const visN = 8
	sel := m.settingsSel - settingsFixedRows // voice index (negative on a fixed row)
	start := sel - visN/2
	if start < 0 {
		start = 0
	}
	if start+visN > len(voices) {
		start = max(0, len(voices)-visN)
	}
	var out []string
	for i := start; i < len(voices) && i < start+visN; i++ {
		mark := "  "
		if voices[i].ID == cur {
			mark = "● "
		}
		out = append(out, menuRow(th, i == sel, mark+voiceLabel(voices[i]), w))
	}
	return out
}
