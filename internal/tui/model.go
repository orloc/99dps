// Package tui is the experimental Bubble Tea + Lipgloss UI for 99dps (see
// docs/tui-migration.md). Phase 0: a live, themed, scrollable Damage panel
// reading real session snapshots. Selected with `99dps -ui tui`.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/gamestate"
	"99dps/internal/session"
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Model is the root Bubble Tea model. It reads the session snapshot once per
// tick — the parser goroutine feeds the manager exactly as under gocui.
type Model struct {
	sm        *session.SessionManager
	tracker   *gamestate.Tracker
	character string
	theme     int

	vp    viewport.Model // the scrollable dealer list (free scrolling, no manual clamp)
	w, h  int
	ready bool
}

// New builds the model over a live session manager + (optional) tracker.
func New(sm *session.SessionManager, tracker *gamestate.Tracker, character string) Model {
	return Model{sm: sm, tracker: tracker, character: character}
}

func (m Model) Init() tea.Cmd { return tick() }

// dims returns the card's inner content width and the scrollable body height.
func (m Model) dims() (int, int) {
	cardW := m.w - 4
	if cardW > 84 {
		cardW = 84
	}
	if cardW < 32 {
		cardW = 32
	}
	innerW := cardW - 4 // border(2) + padding(2)
	bodyH := m.h - 9    // banner + gaps + title + meta + border + footer
	if bodyH < 3 {
		bodyH = 3
	}
	return innerW, bodyH
}

func (m *Model) refresh() {
	cur := m.sm.Current()
	live := cur != nil && cur.EndTime().IsZero()
	m.vp.SetContent(m.damageContent(cur, live, m.vp.Width))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "t", "tab":
			m.theme = (m.theme + 1) % len(themes)
			m.refresh()
		}
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		iw, bh := m.dims()
		if !m.ready {
			m.vp = viewport.New(iw, bh)
			m.ready = true
		} else {
			m.vp.Width, m.vp.Height = iw, bh
		}
		m.refresh()
	case tickMsg:
		m.refresh()
		cmds = append(cmds, tick())
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // viewport handles wheel + arrows
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	th := themes[m.theme]
	innerW := m.vp.Width

	cur := m.sm.Current()
	live := cur != nil && cur.EndTime().IsZero()

	name := "No fight yet"
	meta := "fight something!"
	if cur != nil {
		enc := cur.Total() + cur.MagicTotal()
		span := cur.LastUnix() - cur.StartTime().Unix()
		if span < 1 {
			span = 1
		}
		name = cur.Name()
		meta = fmt.Sprintf("%s  ·  group %s  ·  %s dps",
			fmtDuration(cur.Duration()), humanize(enc), humanize(enc/int(span)))
	}

	pillBG, pillTxt := "#5fd37a", "● LIVE"
	if !live {
		pillBG, pillTxt = th.accentLo, "○ ended"
	}
	pill := lipgloss.NewStyle().Foreground(lipgloss.Color(th.bg)).Background(lipgloss.Color(pillBG)).
		Bold(true).Padding(0, 1).Render(pillTxt)
	titleTxt := th.fg(th.accent).Bold(true).Render(truncate("⚔  "+name, innerW-lipgloss.Width(pill)-1))
	gap := innerW - lipgloss.Width(titleTxt) - lipgloss.Width(pill)
	if gap < 1 {
		gap = 1
	}
	titleRow := titleTxt + strings.Repeat(" ", gap) + pill

	inner := lipgloss.JoinVertical(lipgloss.Left, titleRow, th.fg(th.dim).Render(meta), "", m.vp.View())
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(th.accent)).
		Background(lipgloss.Color(th.panel)).Padding(0, 1).Render(inner)

	banner := th.fg(th.accent).Bold(true).Render("✦ 99dps") + th.fg(th.dim).Render("  ·  "+m.character)
	footer := th.fg(th.dim).Render(fmt.Sprintf("theme: %s   ·   [t] theme   [wheel/↑↓] scroll   [q] quit", th.name))

	body := lipgloss.JoinVertical(lipgloss.Left, banner, "", card, "", footer)
	return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
		Width(m.w).Height(m.h).Padding(1, 2).Render(body)
}

// damageContent is the scrollable dealer bar chart for the active fight, ported
// from render.go's logic into themed Lipgloss.
func (m Model) damageContent(cur *session.CombatSession, _ bool, width int) string {
	th := themes[m.theme]
	if cur == nil {
		return th.fg(th.dim).Render("No fight selected.\nFight something!")
	}
	stats := cur.GetAggressors()
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	magic := cur.MagicTotal()
	span := cur.LastUnix() - cur.StartTime().Unix()
	if span < 1 {
		span = 1
	}
	maxTotal := magic
	for _, d := range stats {
		if d.Total > maxTotal {
			maxTotal = d.Total
		}
	}
	if maxTotal < 1 {
		maxTotal = 1
	}

	const nameW, valW, dpsW = 12, 7, 8
	barCells := width - nameW - valW - dpsW - 3 - 8 // 3 gaps + headroom so values always fit
	if barCells < 6 {
		barCells = 6
	}

	var rows []string
	for i, d := range stats {
		from, to := th.barFrom, th.barTo
		if i > 0 {
			from, to = th.accent, th.accentLo
		}
		nameStyle := th.fg(th.text).Width(nameW)
		if strings.EqualFold(d.Dealer, "you") {
			nameStyle = nameStyle.Bold(true).Foreground(lipgloss.Color(th.accent))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
			nameStyle.Render(truncate(d.Dealer, nameW)), " ",
			gradientBar(float64(d.Total)/float64(maxTotal), barCells, from, to, th.track), " ",
			rightCell(humanize(d.Total), valW, th.text), " ",
			rightCell(humanize(d.Total/int(span))+"/s", dpsW, th.dim)))
	}
	if magic > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
			th.fg(th.dim).Width(nameW).Render("spells n/a"), " ",
			gradientBar(float64(magic)/float64(maxTotal), barCells, th.dim, th.dim, th.track), " ",
			rightCell(humanize(magic), valW, th.dim), " ",
			rightCell(humanize(magic/int(span))+"/s", dpsW, th.dim)))
	}
	if len(rows) == 0 {
		return th.fg(th.dim).Render("No damage yet.")
	}
	return strings.Join(rows, "\n")
}

// Run launches the Bubble Tea program; blocks until the user quits.
func Run(sm *session.SessionManager, tracker *gamestate.Tracker, character string) error {
	_, err := tea.NewProgram(New(sm, tracker, character),
		tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
