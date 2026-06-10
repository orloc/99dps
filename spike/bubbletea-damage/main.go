// Spike: the 99dps Damage panel rebuilt in Bubble Tea + Lipgloss, to compare
// against the gocui version — true 24-bit "Kunark gold" on near-black, a
// declarative gold-bordered/padded panel, and a Bubbles viewport that scrolls
// for free (no manual SetOrigin/clamp). Run:  go run .   (q or ctrl+c to quit;
// mouse wheel or ↑/↓/pgup/pgdn to scroll).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// real 24-bit colors — the whole point of the spike (gocui can only do 8).
var (
	gold    = lipgloss.Color("#c9a227") // metallic EQ/Kunark gold
	goldLit = lipgloss.Color("#e8c45a")
	dim     = lipgloss.Color("#8a8f98")
	white   = lipgloss.Color("#e6e6e6")
	cyan    = lipgloss.Color("#38c9c9")
	green   = lipgloss.Color("#54c267")
	red     = lipgloss.Color("#e0564e")
)

var (
	panel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(gold).Padding(0, 1)
	titleBG = lipgloss.NewStyle().Foreground(lipgloss.Color("#0c0c0e")).Background(gold).Bold(true).Padding(0, 1)
	header  = lipgloss.NewStyle().Foreground(goldLit).Bold(true)
	youRow  = lipgloss.NewStyle().Foreground(white).Bold(true)
	dimText = lipgloss.NewStyle().Foreground(dim)
)

// body builds the scrollable damage content (table + specials + avoidance), with
// enough rows to force scrolling so the viewport is the visible win.
func body() string {
	var b strings.Builder
	b.WriteString(header.Render("#  DEALER          DPS    TOTAL     %  HIT% CRIT%") + "\n")

	rows := []struct {
		s  string
		st lipgloss.Style
	}{
		{"1  You            12k    520k   31%  100%   12%", youRow},
		{"2  Gabnador      8.1k    349k   21%   94%    8%", lipgloss.NewStyle().Foreground(cyan)},
		{"3  Borric        4.4k    190k   11%   88%    5%", lipgloss.NewStyle().Foreground(green)},
		{"4  Mourngul      3.9k    168k   10%   91%    6%", lipgloss.NewStyle().Foreground(lipgloss.Color("#c98ad6"))},
		{"5  Faelyn        3.1k    133k    8%   85%    4%", lipgloss.NewStyle().Foreground(lipgloss.Color("#6a9ee8"))},
		{"6  Tunare        2.2k     95k    6%   90%    7%", lipgloss.NewStyle().Foreground(red)},
		{"7  a pet         1.8k     77k    5%   97%    9%", lipgloss.NewStyle().Foreground(white)},
		{"   spells (n/a)  1.5k     64k    4%    -      -", dimText},
	}
	for _, r := range rows {
		b.WriteString(r.st.Render(r.s) + "\n")
	}

	b.WriteString("\n" + header.Render("SPECIALS · backstab/bash/kick") + "\n")
	b.WriteString(dimText.Render("You            84k   16%  42 hits") + "\n")
	b.WriteString(dimText.Render("Borric         31k   16%  18 hits") + "\n")

	b.WriteString("\n" + header.Render("AVOIDANCE") + "\n")
	b.WriteString(dimText.Render("Defender       Faced Avoid  Miss Dodge Parry Block  Ripo") + "\n")
	for _, d := range []string{
		"a sand giant     142   18%   12%    3%    2%    1%    0%",
		"a sand golem      88   24%   14%    6%    3%    1%    0%",
		"a dust devil      40   31%   20%    7%    3%    1%    0%",
	} {
		b.WriteString(d + "\n")
	}
	return b.String()
}

type model struct {
	vp    viewport.Model
	ready bool
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		// leave room for the border (2), padding (2), and the title bar (1) + a gap
		w := msg.Width - 4
		h := msg.Height - 5
		if h < 1 {
			h = 1
		}
		if !m.ready {
			m.vp = viewport.New(w, h)
			m.ready = true
		} else {
			m.vp.Width, m.vp.Height = w, h
		}
		m.vp.SetContent(body())
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // viewport handles wheel + arrows for free
	return m, cmd
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	title := titleBG.Width(m.vp.Width).Render("⚔ a sand giant   ● live")
	meta := dimText.Render(fmt.Sprintf("0:42 · group 1.2M · 28k dps    (%3.0f%% — wheel/↑↓ to scroll, q to quit)",
		m.vp.ScrollPercent()*100))
	inner := lipgloss.JoinVertical(lipgloss.Left, title, meta, m.vp.View())
	return panel.Render(inner)
}

func main() {
	p := tea.NewProgram(model{}, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
