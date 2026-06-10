// Spike v2: a *themed*, app-like Damage card in Bubble Tea + Lipgloss.
// Shows what a real visual pass buys over plain gocui text: true-color themes
// you can cycle live, gradient bar charts with sub-cell precision, a filled
// "card" panel, and a LIVE pill badge. Run:  go run .
//
//	[t] cycle theme   [q]/ctrl+c quit
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ---- themes -----------------------------------------------------------------

type theme struct {
	name             string
	bg, panel        string // terminal-ish bg, slightly-lighter card bg
	accent, accentLo string // frame/title gold; dimmer accent
	text, dim        string
	barFrom, barTo   string // left→right gradient for the #1 bar
	track            string // unfilled bar
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

// ---- color helpers ----------------------------------------------------------

func hex(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	var r, g, b int
	fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

func blend(a, b string, t float64) lipgloss.Color {
	ar, ag, ab := hex(a)
	br, bg, bb := hex(b)
	lerp := func(x, y int) int { return x + int(float64(y-x)*t) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", lerp(ar, br), lerp(ag, bg), lerp(ab, bb)))
}

// gradientBar draws a width-cell bar filled to frac (0..1) with a left→right
// gradient, using 1/8th sub-blocks for a smooth end instead of a blocky █/░.
func gradientBar(frac float64, cells int, from, to, track string) string {
	frac = clamp01(frac)
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
			idx := int((fill - float64(whole)) * 8)
			if idx == 0 {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(track)).Render("░"))
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(col).Render(eighths[idx]))
			}
		default:
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(track)).Render("░"))
		}
	}
	return sb.String()
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// ---- model ------------------------------------------------------------------

type dealer struct {
	name  string
	total int
	dps   string
	you   bool
}

var dealers = []dealer{
	{"You", 520_000, "12k", true},
	{"Gabnador", 349_000, "8.1k", false},
	{"Borric", 190_000, "4.4k", false},
	{"Mourngul", 168_000, "3.9k", false},
	{"Faelyn", 133_000, "3.1k", false},
	{"a pet", 77_000, "1.8k", false},
}

type model struct {
	theme int
	w, h  int
	ready bool
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "t", "tab", " ":
			m.theme = (m.theme + 1) % len(themes)
		}
	case tea.WindowSizeMsg:
		m.w, m.h, m.ready = msg.Width, msg.Height, true
	}
	return m, nil
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	th := themes[m.theme]
	screen := lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text))

	cardW := min(m.w-6, 64)
	if cardW < 36 {
		cardW = 36
	}
	innerW := cardW - 4 // content width inside border(2) + padding(2)

	// title row: name on the left, a rounded LIVE pill on the right
	pill := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.bg)).Background(lipgloss.Color("#5fd37a")).
		Bold(true).Padding(0, 1).Render("● LIVE")
	titleTxt := lipgloss.NewStyle().Foreground(lipgloss.Color(th.accent)).Bold(true).Render("⚔  a sand giant")
	gap := innerW - lipgloss.Width(titleTxt) - lipgloss.Width(pill)
	if gap < 1 {
		gap = 1
	}
	titleRow := titleTxt + strings.Repeat(" ", gap) + pill

	sub := lipgloss.NewStyle().Foreground(lipgloss.Color(th.dim)).
		Render("0:42  ·  group 1.2M  ·  28k dps")

	// dealer bar chart (gradient bars, sub-cell ends). Compose each row from
	// fixed-width Lipgloss cells so ANSI styling never throws off the alignment.
	const nameW, valW, dpsW = 10, 6, 7
	barCells := innerW - nameW - valW - dpsW - 3 - 4 // 3 gaps + a little slack
	if barCells < 8 {
		barCells = 8
	}
	maxTotal := dealers[0].total
	var rows []string
	for i, d := range dealers {
		from, to := th.barFrom, th.barTo
		if i > 0 { // non-leader bars: dimmer, accent-tinted
			from, to = th.accent, th.accentLo
		}
		nameCol := lipgloss.Color(th.text)
		nameStyle := lipgloss.NewStyle().Width(nameW).Foreground(nameCol)
		if d.you {
			nameStyle = nameStyle.Bold(true).Foreground(lipgloss.Color(th.accent))
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			nameStyle.Render(d.name), " ",
			gradientBar(float64(d.total)/float64(maxTotal), barCells, from, to, th.track), " ",
			lipgloss.NewStyle().Width(valW).Align(lipgloss.Right).Foreground(nameCol).Render(humanize(d.total)), " ",
			lipgloss.NewStyle().Width(dpsW).Align(lipgloss.Right).Foreground(lipgloss.Color(th.dim)).Render(d.dps+"/s"),
		)
		rows = append(rows, row)
	}

	divider := lipgloss.NewStyle().Foreground(lipgloss.Color(th.accentLo)).Render(strings.Repeat("─", innerW))
	avoidHdr := lipgloss.NewStyle().Foreground(lipgloss.Color(th.accent)).Bold(true).Render("AVOIDANCE")
	avoid := lipgloss.NewStyle().Foreground(lipgloss.Color(th.dim)).Render(
		"a sand giant   142 faced   18% avoided   m12 d3 p2 b1")

	inner := lipgloss.JoinVertical(lipgloss.Left,
		titleRow, sub, "", strings.Join(rows, "\n"), "", divider, avoidHdr, avoid)

	// pad the shorter lines (sub, avoid) to innerW so the card bg fills evenly;
	// rows/divider/title are already innerW. No forced Width → no wrapping.
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(th.accent)).
		Background(lipgloss.Color(th.panel)).
		Padding(0, 1).
		Render(inner)

	banner := lipgloss.NewStyle().Foreground(lipgloss.Color(th.accent)).Bold(true).Render("✦ 99dps") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(th.dim)).Render("  ·  Kelkix")
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color(th.dim)).Render(
		fmt.Sprintf("theme: %s   ·   [t] next theme   [q] quit", th.name))

	content := lipgloss.JoinVertical(lipgloss.Left, banner, "", card, "", footer)
	// paint the whole screen with the theme bg
	return screen.Width(m.w).Height(m.h).Padding(1, 2).Render(content)
}

// ---- tiny formatters --------------------------------------------------------

func humanize(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	// non-interactive preview mode: `go run . print <themeIdx>` prints one frame
	// with truecolor forced, for screenshotting (e.g. via charmbracelet/freeze).
	if len(os.Args) > 1 && os.Args[1] == "print" {
		lipgloss.SetColorProfile(termenv.TrueColor)
		idx := 0
		if len(os.Args) > 2 {
			idx, _ = strconv.Atoi(os.Args[2])
		}
		fmt.Print(model{theme: idx, w: 74, h: 26, ready: true}.View())
		return
	}
	if _, err := tea.NewProgram(model{}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
