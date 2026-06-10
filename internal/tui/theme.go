package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// theme is a true-color palette. The whole point of the Bubble Tea UI is that
// these are real 24-bit colors, switchable at runtime — not the 8 ANSI colors
// gocui was stuck with.
type theme struct {
	name             string
	bg, panel        string // screen bg, slightly-lighter card bg
	accent, accentLo string // frame/title; dimmer accent
	text, dim        string
	track            string // unfilled bar track (the bar fill is a DPS rainbow, see rainbowBar)
}

var themes = []theme{
	{
		name: "Kunark Gold",
		bg:   "#0c0b09", panel: "#16130d",
		accent: "#c9a227", accentLo: "#7a6220",
		text: "#e7e2d4", dim: "#8d837a",
		track: "#2a2620",
	},
	{
		name: "Velious Ice",
		bg:   "#070b0e", panel: "#0d141a",
		accent: "#7fd4e8", accentLo: "#3a7d8c",
		text: "#dce8ee", dim: "#6b818c",
		track: "#16242c",
	},
	{
		name: "Spirit Crimson",
		bg:   "#0d0808", panel: "#180c0c",
		accent: "#d6534b", accentLo: "#7e2f2b",
		text: "#ecdcdc", dim: "#8c6f6f",
		track: "#2a1818",
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

// hsvHex converts HSV (h in degrees, s & v in 0..1) to a "#rrggbb" string.
func hsvHex(h, s, v float64) string {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return fmt.Sprintf("#%02x%02x%02x", int((r+m)*255), int((g+m)*255), int((b+m)*255))
}

// rainbowBar maps a 0..1 DPS magnitude (share of the top dealer) to a heat-map
// bar gradient: hot red for the highest, cooling through orange/yellow/green to
// violet for the lowest — so a bar's hue encodes its DPS.
func rainbowBar(frac float64) (light, dark string) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	h := (1 - frac) * 280 // frac=1 → 0° (red), frac=0 → 280° (violet)
	return hsvHex(h, 0.52, 0.96), hsvHex(h, 0.66, 0.60)
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
