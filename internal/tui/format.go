package tui

import (
	"fmt"
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
