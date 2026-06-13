package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/tts"
)

// screen selects which top-level view is active. The meter is the normal view;
// the setup screen runs once on first launch (or until the user makes a choice).
type screen int

const (
	screenMeter    screen = iota // the live DPS meter (default)
	screenSettings               // audio / voice configuration (a tab)
	screenSetup                  // first-run onboarding (full-screen, not a tab)
)

// setupPhase is the step within the first-run audio-cue setup.
type setupPhase int

const (
	phaseMenu        setupPhase = iota // enable (download) or skip
	phaseDownloading                   // fetching the ~120 MB voice
	phaseVoice                         // pick a voice profile
	phaseError                         // download failed; retry or skip
)

// setupRed colors the error state (the theme has no dedicated red field).
const setupRed = "#d6534b"

// setupState is the first-run audio-cue setup screen's state. It lives by value
// on the Model; ch is the one reference shared across Model copies.
type setupState struct {
	phase setupPhase
	sel   int // selected row in the current phase's list

	voices  []tts.Voice
	dlLabel string
	dlBytes int64
	err     error

	ch chan tea.Msg // download progress/done events
}

func newSetupState() setupState { return setupState{phase: phaseMenu} }

// initialScreen decides the launch view: the setup screen until the user has
// made an audio choice, otherwise straight to the meter.
func initialScreen(p tts.Prefs) screen {
	if p.Configured {
		return screenMeter
	}
	return screenSetup
}

// --- download plumbing (Bubble Tea channel pattern) ---

type dlProgressMsg struct {
	label string
	done  int64
}
type dlDoneMsg struct{ err error }

// startDownload kicks off the asset fetch in a goroutine that streams progress
// onto ch (throttled to whole-MB changes, non-blocking so it never stalls the
// download), then a final dlDoneMsg. It returns the first event.
func startDownload(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			lastMB := int64(-1)
			err := tts.EnsureAssets(func(label string, done int64) {
				if mb := done / (1024 * 1024); mb != lastMB {
					lastMB = mb
					select {
					case ch <- dlProgressMsg{label, done}:
					default: // UI busy; drop this tick, the next MB will report
					}
				}
			})
			ch <- dlDoneMsg{err} // blocking — the done event is never dropped
		}()
		return <-ch
	}
}

// waitFor reads the next download event; re-issued after each progress message.
func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// updateSetup owns all input while the setup screen is active.
func (m Model) updateSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 && msg.Height > 0 {
			m.w, m.h, m.ready = msg.Width, msg.Height, true
		}
		return m, nil
	case tickMsg:
		return m, tick() // keep the heartbeat alive for when we reach the meter
	case dlProgressMsg:
		m.setup.dlLabel, m.setup.dlBytes = msg.label, msg.done
		return m, waitFor(m.setup.ch)
	case dlDoneMsg:
		if msg.err != nil {
			m.setup.phase, m.setup.err = phaseError, msg.err
			return m, nil
		}
		m.speaker = tts.New() // assets now present → engine becomes available
		m.setup.voices = m.speaker.Voices()
		m.setup.phase, m.setup.sel = phaseVoice, 0
		return m, nil
	case tea.KeyMsg:
		return m.setupKey(msg)
	}
	return m, nil
}

func (m Model) setupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if s := msg.String(); s == "ctrl+c" || s == "q" {
		return m, tea.Quit
	}
	switch m.setup.phase {
	case phaseMenu:
		switch msg.String() {
		case "up", "k":
			m.setup.sel = 0
		case "down", "j":
			m.setup.sel = 1
		case "enter", " ":
			if m.setup.sel == 1 { // skip
				_ = tts.SavePrefs(tts.Prefs{Configured: true, Enabled: false})
				return m.enterMeter(), nil
			}
			if m.speaker.Available() { // already downloaded → straight to voice pick
				m.setup.voices = m.speaker.Voices()
				m.setup.phase, m.setup.sel = phaseVoice, 0
				return m, nil
			}
			m.setup.ch = make(chan tea.Msg, 4)
			m.setup.phase = phaseDownloading
			return m, startDownload(m.setup.ch)
		}
	case phaseVoice:
		switch msg.String() {
		case "up", "k":
			if m.setup.sel > 0 {
				m.setup.sel--
			}
		case "down", "j":
			if m.setup.sel < len(m.setup.voices)-1 {
				m.setup.sel++
			}
		case "p": // preview the highlighted voice
			if v, ok := m.selectedVoice(); ok {
				m.speaker.SetVoice(v.ID)
				m.speaker.Say("Audio cue test.")
			}
		case "enter":
			v, ok := m.selectedVoice()
			if ok {
				m.speaker.SetVoice(v.ID)
			}
			_ = tts.SavePrefs(tts.Prefs{Configured: true, Enabled: true, Voice: v.ID})
			m.ttsOn = m.speaker.Available()
			return m.enterMeter(), nil
		}
	case phaseError:
		switch msg.String() {
		case "enter", "r": // retry
			m.setup.ch = make(chan tea.Msg, 4)
			m.setup.phase, m.setup.err = phaseDownloading, nil
			return m, startDownload(m.setup.ch)
		case "s": // give up on audio
			_ = tts.SavePrefs(tts.Prefs{Configured: true, Enabled: false})
			return m.enterMeter(), nil
		}
	}
	return m, nil
}

func (m Model) selectedVoice() (tts.Voice, bool) {
	if m.setup.sel < 0 || m.setup.sel >= len(m.setup.voices) {
		return tts.Voice{}, false
	}
	return m.setup.voices[m.setup.sel], true
}

// enterMeter leaves the setup screen for the live meter, sizing its panels.
func (m Model) enterMeter() Model {
	m.screen = screenMeter
	if m.ready {
		m.resizeViewports()
		m.refresh()
	}
	return m
}

// setupView renders the first-run screen body for the current phase. Each line
// is truncated to w (plain) before coloring, so colored output still fits.
func (m Model) setupView(th theme, w int) string {
	switch m.setup.phase {
	case phaseDownloading:
		mb := m.setup.dlBytes / (1024 * 1024)
		return lines(
			paint(th, th.accent, "Downloading voice…", w),
			"",
			paint(th, th.text, fmt.Sprintf("  %s: %d MB", orDefault(m.setup.dlLabel, "voice"), mb), w),
			"",
			paint(th, th.dim, "One-time ~120 MB download. Please wait…", w),
		)
	case phaseVoice:
		return lines(m.voicePickLines(th, w)...)
	case phaseError:
		return lines(
			paint(th, setupRed, "Download failed", w),
			"",
			paint(th, th.text, "  "+fmt.Sprint(m.setup.err), w),
			"",
			paint(th, th.dim, "enter retry · s skip (no audio cues)", w),
		)
	default: // phaseMenu
		return lines(
			paint(th, th.accent, "Audio cues", w),
			"",
			paint(th, th.text, "99dps can speak a heads-up when a buff is about to drop", w),
			paint(th, th.text, `(e.g. "Clarity low"). The voice is a one-time ~120 MB download.`, w),
			"",
			menuRow(th, m.setup.sel == 0, "Enable audio cues  (download the voice now)", w),
			menuRow(th, m.setup.sel == 1, "Skip — no audio cues", w),
			"",
			paint(th, th.dim, "↑/↓ choose · enter select · q quit", w),
		)
	}
}

// voicePickLines renders a windowed view of the voice list around the selection.
func (m Model) voicePickLines(th theme, w int) []string {
	const visN = 8
	out := []string{paint(th, th.accent, "Choose a voice", w), ""}
	n := len(m.setup.voices)
	start := m.setup.sel - visN/2
	if start < 0 {
		start = 0
	}
	if start+visN > n {
		start = max(0, n-visN)
	}
	for i := start; i < n && i < start+visN; i++ {
		out = append(out, menuRow(th, i == m.setup.sel, voiceLabel(m.setup.voices[i]), w))
	}
	out = append(out, "", paint(th, th.dim, "p preview · ↑/↓ choose · enter confirm", w))
	return out
}

// voiceLabel renders a voice as "name — description" for the picker.
func voiceLabel(v tts.Voice) string {
	if v.Desc == "" {
		return v.Name
	}
	return v.Name + " — " + v.Desc
}

func menuRow(th theme, selected bool, label string, w int) string {
	if selected {
		return paint(th, th.accent, "  ▸ "+label, w)
	}
	return paint(th, th.text, "    "+label, w)
}

// paint truncates plain text to w, then colors it.
func paint(th theme, color, s string, w int) string {
	return th.fg(color).Render(truncate(s, w))
}

func lines(ls ...string) string { return lipgloss.JoinVertical(lipgloss.Left, ls...) }

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
