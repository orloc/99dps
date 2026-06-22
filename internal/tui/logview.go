package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// logSheet renders the merged live-tail sheet: time │ raw log line │ fired cue,
// newest at the bottom. Raw lines read in the body text color; fired cues are
// colored (urgent in red, bold) in the right column, so they pop against the log
// stream. Width-aware — every column is clipped to fit, so it never wraps.
func logSheet(th theme, entries []LogEntry, w int) string {
	if len(entries) == 0 {
		return th.fg(th.dim).Render("waiting for log activity…")
	}
	const timeW = 8 // HH:MM:SS
	bar := th.fg(th.dim).Render("│")
	sep := " " + bar + " "
	sepW := lipgloss.Width(sep)
	// split the content area between the log and cue columns (~58 / 42).
	rest := max(w-timeW-2*sepW, 8)
	logW := max(rest*58/100, 4)
	cueW := max(rest-logW, 6)
	blankLog := strings.Repeat(" ", logW)

	lines := make([]string, len(entries))
	for i, e := range entries {
		ts := "        "
		if e.At > 0 {
			ts = time.Unix(e.At, 0).Format("15:04:05")
		}
		stamp := th.fg(th.dim).Render(ts)
		if e.Kind == logMark {
			// a system divider (character switch): a centered label in a rule of
			// dashes spanning the rest of the row, so it clearly breaks the stream.
			label := " " + e.Text + " "
			avail := max(w-timeW-sepW, 4)
			label = truncate(label, avail)
			dashes := avail - lipgloss.Width(label)
			left := dashes / 2
			rule := strings.Repeat("─", left) + label + strings.Repeat("─", dashes-left)
			lines[i] = stamp + sep + th.fg(th.accent).Bold(true).Render(rule)
			continue
		}
		if e.Kind == logCue {
			marker, col := "♪ ", th.accent
			if e.Urgent {
				marker, col = "⚠ ", "#e0564e"
			}
			cue := th.fg(col).Bold(true).Render(truncate(marker+e.Text, cueW))
			lines[i] = stamp + sep + blankLog + sep + cue
			continue
		}
		text := truncate(stripLogStamp(e.Text), logW)
		pad := strings.Repeat(" ", max(logW-lipgloss.Width(text), 0))
		lines[i] = stamp + sep + th.fg(th.text).Render(text) + pad + sep
	}
	return strings.Join(lines, "\n")
}

// stripLogStamp drops the fixed-width "[Www Mmm DD HH:MM:SS YYYY] " prefix from a
// raw EQ log line — the time already has its own column, so showing it again in
// the body is redundant. (LOG_SUBJECT_INDEX_START in the parser is the same 27.)
// Returns the line unchanged if it's too short or doesn't start with a stamp.
func stripLogStamp(s string) string {
	s = strings.TrimRight(s, "\r\n") // defensive; the parser already trims CRLF
	if len(s) >= 27 && s[0] == '[' {
		return s[27:]
	}
	return s
}

// refreshLog re-renders the live-log ring into its viewport, auto-tailing to the
// newest row while following.
func (m *Model) refreshLog() {
	entries, _ := m.logTail.Snapshot(logTailCap)
	m.vpLog.SetContent(logSheet(themes[m.theme], entries, max(m.vpLog.Width, 1)))
	if m.logFollow {
		m.vpLog.GotoBottom()
	}
}

// logTitle labels the Live Log card, noting when the tail is paused (scrolled up).
func (m Model) logTitle() string {
	t := "Live Log"
	if !m.logFollow {
		t += " — paused (End to follow)"
	}
	return t
}

// updateLog handles keys on the Live Log tab: scroll the sheet, End re-follows.
func (m Model) updateLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "end", "g":
		m.logFollow = true
		m.vpLog.GotoBottom()
	case "home":
		m.logFollow = false
		m.vpLog.GotoTop()
	case "up", "k", "down", "j", "pgup", "pgdown", "ctrl+u", "ctrl+d":
		m.logFollow = false // any manual scroll pauses the tail
		var cmd tea.Cmd
		m.vpLog, cmd = m.vpLog.Update(msg)
		return m, cmd
	case "t":
		m.theme = (m.theme + 1) % len(themes)
		m.refreshLog()
	case "a":
		return m, m.toggleTTS()
	}
	return m, nil
}
