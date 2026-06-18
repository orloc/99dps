package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// humanize compacts a number for display: 1.2M, 12k, 8.1k, 940.
func humanize(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// fmtDuration formats a duration as m:ss (or h:mm:ss past an hour).
func fmtDuration(d time.Duration) string {
	s := int(d.Seconds())
	if s < 0 {
		s = 0
	}
	h, m := s/3600, (s%3600)/60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s%60)
	}
	return fmt.Sprintf("%d:%02d", m, s%60)
}

// wrap word-wraps s into lines no wider than w display cells, breaking on spaces
// (a word longer than w is hard-split so it still fits). Used for help/hint text
// that must not be clipped in a narrow panel. Always returns at least one line.
func wrap(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		for lipgloss.Width(word) > w { // hard-split an over-long word
			head := truncate(word, w)
			if head == "" {
				head = string([]rune(word)[:1]) // guarantee progress
			}
			if line != "" {
				out = append(out, line)
				line = ""
			}
			out = append(out, head)
			word = word[len(head):]
		}
		switch {
		case word == "":
			// fully consumed by the hard-split above
		case line == "":
			line = word
		case lipgloss.Width(line)+1+lipgloss.Width(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// truncate clips s to w display cells (rune-aware via lipgloss width).
func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
}

// rightCell renders s right-aligned in a fixed-width colored cell.
func rightCell(s string, w int, color string) string {
	return lipgloss.NewStyle().Width(w).Align(lipgloss.Right).Foreground(lipgloss.Color(color)).Render(s)
}

// mmss formats a seconds count as m:ss (or h:mm:ss past an hour).
func mmss(sec int64) string { return fmtDuration(time.Duration(sec) * time.Second) }

// compactDur formats a long elapsed span for the banner: "1h23m", "45m", "<1m".
func compactDur(sec int64) string {
	if sec < 60 {
		return "<1m"
	}
	if h, m := sec/3600, (sec%3600)/60; h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", sec/60)
}

// displayName renders the player's normalized "YOU" token as "You" for the UI.
func displayName(name string) string {
	if name == "YOU" {
		return "You"
	}
	return name
}
