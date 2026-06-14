package tui

import (
	"time"

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
// keys are consumed earlier, in Update). The tab has two columns — left/right (or
// h/l) switch focus, up/down moves within the focused column, enter/space acts.
func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "left", "h":
		m.settingsCol = 0
	case "right", "l":
		m.settingsCol = 1
	case "up", "k":
		if m.settingsCol == 0 {
			if m.settingsSel > 0 {
				m.settingsSel--
			}
		} else if m.cueSel > 0 {
			m.cueSel--
		}
	case "down", "j":
		if m.settingsCol == 0 {
			if last := settingsFixedRows + len(m.settingsVoices()) - 1; m.settingsSel < last {
				m.settingsSel++
			}
		} else if last := len(cueToggles(cueRows())) - 1; m.cueSel < last {
			m.cueSel++
		}
	case "enter", " ":
		if m.settingsCol == 0 {
			m.applyLeftSetting(m.settingsSel)
		} else {
			m.toggleCue(m.cueSel)
		}
	case "p":
		if m.settingsCol == 0 {
			if v, ok := voiceIndex(m.settingsVoices(), m.settingsSel-settingsFixedRows); ok {
				m.speaker.SetVoice(v.ID)
				m.speaker.Say("Audio cue test.")
			}
		}
	}
	return m, nil
}

// applyLeftSetting performs the left-column action for selectable row sel
// (audio toggle, meter-box cycles, or voice select).
func (m *Model) applyLeftSetting(sel int) {
	switch sel {
	case settingsAudioRow:
		m.toggleTTS()
		m.persistAudioPrefs()
	case settingsDamageRow:
		m.layoutPrefs.Damage = m.layoutPrefs.Damage.next()
		_ = saveLayoutPrefs(m.layoutPrefs)
		m.flash("✓ saved · Damage meter " + m.layoutPrefs.Damage.String())
	case settingsOffDefRow:
		m.layoutPrefs.OffDef = m.layoutPrefs.OffDef.next()
		_ = saveLayoutPrefs(m.layoutPrefs)
		m.flash("✓ saved · Offense · Defense " + m.layoutPrefs.OffDef.String())
	default:
		if v, ok := voiceIndex(m.settingsVoices(), sel-settingsFixedRows); ok {
			m.speaker.SetVoice(v.ID)
			m.flash("✓ saved · voice " + v.Name)
			m.persistAudioPrefs()
		}
	}
}

// toggleCue flips the cue toggle at right-column index idx and persists it.
func (m *Model) toggleCue(idx int) {
	toggles := cueToggles(cueRows())
	if idx < 0 || idx >= len(toggles) {
		return
	}
	r := toggles[idx]
	m.cues.toggle(r.id, r.def)
	_ = saveCuePrefs(m.cues)
	state := "off"
	if m.cues.enabled(r.id, r.def) {
		state = "on"
	}
	m.flash("✓ saved · " + r.label + " " + state)
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

// settingsGapW is the blank gutter between the two settings columns; it's folded
// into the left column's padded width so the right column starts at a fixed X
// (gridX + leftW + settingsGapW), which the click hit-test relies on.
const settingsGapW = 3

// settingsColWidths splits the inner width into the left controls column and the
// right cue-matrix column.
func settingsColWidths(innerW int) (leftW, rightW int) {
	leftW = (innerW - settingsGapW) / 2
	if leftW < 24 {
		leftW = 24
	}
	if leftW > innerW {
		leftW = innerW
	}
	rightW = innerW - settingsGapW - leftW
	if rightW < 1 {
		rightW = 1
	}
	return
}

// settingsView renders the Settings tab body: the existing controls on the left,
// the audio-cue matrix on the right, with a key-hint line beneath.
func (m Model) settingsView(th theme, w int) string {
	leftW, rightW := settingsColWidths(w)
	left, _ := m.leftSettingsColumn(th, leftW)
	right, _ := m.rightSettingsColumn(th, rightW)
	body := lipgloss.JoinHorizontal(lipgloss.Top, settingsColBlock(left, leftW+settingsGapW), lines(right...))
	help := paint(th, th.dim, "↑/↓ move · ←/→ switch column · enter toggle/cycle · p preview voice · tab or click to switch tab", w)
	// a transient confirmation that the last toggle/cycle was saved — the line is
	// always present (blank when idle) so the layout doesn't jump when it appears.
	flash := ""
	if m.status != "" && time.Now().Unix()-m.statusAt <= statusGraceSec {
		flash = th.fg(th.accent).Bold(true).Render(truncate(m.status, w))
	}
	return lines(body, "", help, flash)
}

// settingsColBlock pads every line to exactly w (truncating already done at
// render) so the block has a fixed width and the next column lands predictably.
func settingsColBlock(ls []string, w int) string {
	style := lipgloss.NewStyle().Width(w)
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = style.Render(l)
	}
	return lines(out...)
}

// leftSettingsColumn builds the left controls column. It returns the rendered
// lines plus a parallel slice mapping each line to its selectable row index
// (settingsSel semantics: audio/damage/offdef/voice…), or -1 for a non-row line.
// Render and click hit-test share this so they can't drift.
func (m Model) leftSettingsColumn(th theme, w int) (out []string, sel []int) {
	focused := m.settingsCol == 0
	add := func(line string, s int) { out = append(out, line); sel = append(sel, s) }

	audio := "off"
	if m.speaker == nil || !m.speaker.Available() {
		audio = "no voice — run -tts-setup"
	} else if m.ttsOn {
		audio = "on"
	}

	add(paint(th, th.accent, "Audio cues", w), -1)
	add("", -1)
	add(menuRow(th, focused && m.settingsSel == settingsAudioRow, "Audio cues: "+audio, w), settingsAudioRow)
	add("", -1)
	add(paint(th, th.accent, "Meter boxes", w), -1)
	add("", -1)
	add(menuRow(th, focused && m.settingsSel == settingsDamageRow, "Damage meter: "+m.layoutPrefs.Damage.String(), w), settingsDamageRow)
	add(menuRow(th, focused && m.settingsSel == settingsOffDefRow, "Offense · Defense: "+m.layoutPrefs.OffDef.String(), w), settingsOffDefRow)
	add("", -1)
	add(paint(th, th.accent, "Voice", w), -1)

	voices := m.settingsVoices()
	if len(voices) == 0 {
		add(paint(th, th.dim, "  (download a voice first)", w), -1)
		return
	}
	cur := ""
	if m.speaker != nil {
		cur = m.speaker.Voice()
	}
	const visN = 8
	selIdx := m.settingsSel - settingsFixedRows // voice index (negative on a fixed row)
	start := selIdx - visN/2
	if start < 0 {
		start = 0
	}
	if start+visN > len(voices) {
		start = max(0, len(voices)-visN)
	}
	for i := start; i < len(voices) && i < start+visN; i++ {
		mark := "  "
		if voices[i].ID == cur {
			mark = "● "
		}
		add(menuRow(th, focused && i == selIdx, mark+voiceLabel(voices[i]), w), settingsFixedRows+i)
	}
	return
}

// rightSettingsColumn builds the cue-matrix column. It returns the rendered lines
// plus a parallel slice mapping each line to its cue-toggle index (cueSel), or -1
// for a header/blank line.
func (m Model) rightSettingsColumn(th theme, w int) (out []string, sel []int) {
	focused := m.settingsCol == 1
	add := func(line string, s int) { out = append(out, line); sel = append(sel, s) }

	add(paint(th, th.accent, "Cue settings", w), -1)
	add("", -1)
	idx := 0
	for _, r := range cueRows() {
		if r.header {
			add(paint(th, th.dim, r.label, w), -1)
			continue
		}
		box := "[ ] "
		if m.cues.enabled(r.id, r.def) {
			box = "[x] "
		}
		add(menuRow(th, focused && idx == m.cueSel, box+r.label, w), idx)
		idx++
	}
	return
}

// settingsClickAt resolves a click to a settings column + selectable index. The
// body starts at gridTop; the right column starts at gridX+leftW+settingsGapW.
func (m Model) settingsClickAt(x, y int) (col, selIdx int, ok bool) {
	line := y - gridTop
	if line < 0 {
		return 0, 0, false
	}
	th := themes[m.theme]
	leftW, rightW := settingsColWidths(m.w - 2)
	if x >= gridX+leftW+settingsGapW {
		if _, s := m.rightSettingsColumn(th, rightW); line < len(s) && s[line] >= 0 {
			return 1, s[line], true
		}
		return 0, 0, false
	}
	if _, s := m.leftSettingsColumn(th, leftW); line < len(s) && s[line] >= 0 {
		return 0, s[line], true
	}
	return 0, 0, false
}
