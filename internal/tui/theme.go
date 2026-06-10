package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// theme is a true-color palette. The whole point of the Bubble Tea UI is that
// these are real 24-bit colors, switchable at runtime — not the 8 ANSI colors
// gocui was stuck with.
type theme struct {
	name                  string
	bg, panel             string // screen bg, slightly-lighter card bg
	accent, accentLo      string // frame/title; dimmer accent
	text, dim             string
	barFrom, barTo, track string // leader-bar gradient + unfilled track
}

var themes = []theme{
	{
		name: "Kunark Gold",
		bg:   "#0c0b09", panel: "#16130d",
		accent: "#c9a227", accentLo: "#7a6220",
		text: "#e7e2d4", dim: "#8d837a",
		barFrom: "#f2d36b", barTo: "#9c6f1d", track: "#2a2620",
	},
	{
		name: "Velious Ice",
		bg:   "#070b0e", panel: "#0d141a",
		accent: "#7fd4e8", accentLo: "#3a7d8c",
		text: "#dce8ee", dim: "#6b818c",
		barFrom: "#bdf0ff", barTo: "#2b7fa6", track: "#16242c",
	},
	{
		name: "Spirit Crimson",
		bg:   "#0d0808", panel: "#180c0c",
		accent: "#d6534b", accentLo: "#7e2f2b",
		text: "#ecdcdc", dim: "#8c6f6f",
		barFrom: "#ff9b8a", barTo: "#a3243a", track: "#2a1818",
	},
}

func (t theme) fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

// ---- color + bar helpers ----------------------------------------------------

func hexRGB(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	var r, g, b int
	_, _ = fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b) // theme literals are well-formed
	return r, g, b
}

func blend(a, b string, t float64) lipgloss.Color {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	lerp := func(x, y int) int { return x + int(float64(y-x)*t) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", lerp(ar, br), lerp(ag, bg), lerp(ab, bb)))
}

// gradientBar draws a width-cell bar filled to frac (0..1) with a left→right
// gradient, using 1/8th sub-blocks for a smooth end instead of a blocky █/░.
func gradientBar(frac float64, cells int, from, to, track string) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	eighths := []string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	fill := frac * float64(cells)
	whole := int(fill)
	var sb strings.Builder
	for i := 0; i < cells; i++ {
		t := 0.0
		if cells > 1 {
			t = float64(i) / float64(cells-1)
		}
		col := blend(from, to, t)
		switch {
		case i < whole:
			sb.WriteString(lipgloss.NewStyle().Foreground(col).Render("█"))
		case i == whole:
			if idx := int((fill - float64(whole)) * 8); idx > 0 {
				sb.WriteString(lipgloss.NewStyle().Foreground(col).Render(eighths[idx]))
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(track)).Render("░"))
			}
		default:
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(track)).Render("░"))
		}
	}
	return sb.String()
}
