// Package tui is the experimental Bubble Tea + Lipgloss UI for 99dps (see
// docs/tui-migration.md). Phase 1: the full multi-panel layout — Now + Sessions
// sidebar, a scrollable Damage panel, Spell Timers and Mob Tracker — themed and
// reading live snapshots. Selected with `99dps -ui tui`.
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

// Model is the root Bubble Tea model. It reads session snapshots once per tick —
// the parser goroutine feeds the manager exactly as under gocui.
type Model struct {
	sm        *session.SessionManager
	tracker   *gamestate.Tracker
	character string
	theme     int

	sessions []*session.CombatSession // last snapshot (for the sidebar)
	selected int                      // pinned session index
	follow   bool                     // glue selection to the live fight

	vp    viewport.Model // the scrollable Damage panel
	w, h  int
	ready bool
}

// New builds the model over a live session manager + (optional) tracker.
func New(sm *session.SessionManager, tracker *gamestate.Tracker, character string) Model {
	return Model{sm: sm, tracker: tracker, character: character, follow: true}
}

func (m Model) Init() tea.Cmd { return tick() }

// layout holds the computed panel rectangles for the current window size.
type layout struct {
	leftW, rightW        int
	nowH, sessH          int
	dmgH, botH           int
	timersW, mobW        int
	dmgInnerW, dmgInnerH int // the Damage card's body area (the viewport)
	areaH                int
}

func (m Model) layout() layout {
	innerW := m.w - 2
	areaH := m.h - 4 // banner + footer + outer padding
	if areaH < 6 {
		areaH = 6
	}
	leftW := 26
	if leftW > innerW/3 {
		leftW = innerW / 3
	}
	rightW := innerW - leftW - 1
	nowH := 6
	sessH := areaH - nowH - 1
	dmgH := areaH * 52 / 100
	if dmgH < 5 {
		dmgH = 5
	}
	botH := areaH - dmgH - 1
	timersW := rightW / 2
	mobW := rightW - timersW - 1
	return layout{
		leftW: leftW, rightW: rightW, nowH: nowH, sessH: sessH,
		dmgH: dmgH, botH: botH, timersW: timersW, mobW: mobW, areaH: areaH,
		dmgInnerW: rightW - 4, dmgInnerH: dmgH - 3, // card body inside border+title
	}
}

// effectiveSel resolves the selection against the current session count.
func (m Model) effectiveSel() int {
	n := len(m.sessions)
	if n == 0 {
		return -1
	}
	if m.follow || m.selected >= n {
		return n - 1
	}
	if m.selected < 0 {
		return 0
	}
	return m.selected
}

func (m *Model) refresh() {
	m.sessions = m.sm.All()
	sel := m.effectiveSel()
	var cur *session.CombatSession
	if sel >= 0 {
		cur = m.sessions[sel]
	}
	live := cur != nil && cur.EndTime().IsZero()
	m.vp.SetContent(m.damageContent(cur, live, m.vp.Width))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "t", "tab":
			m.theme = (m.theme + 1) % len(themes)
		case "up", "k":
			cur := m.effectiveSel()
			if cur > 0 {
				m.selected, m.follow = cur-1, false
			}
			m.refresh()
		case "down", "j":
			cur := m.effectiveSel()
			if cur >= 0 && cur < len(m.sessions)-1 {
				m.selected = cur + 1
			}
			if m.selected >= len(m.sessions)-1 {
				m.follow = true
			}
			m.refresh()
		case "end":
			m.follow = true
			m.refresh()
		}
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		ld := m.layout()
		if !m.ready {
			m.vp = viewport.New(ld.dmgInnerW, ld.dmgInnerH)
			m.ready = true
		} else {
			m.vp.Width, m.vp.Height = ld.dmgInnerW, ld.dmgInnerH
		}
		m.refresh()
	case tickMsg:
		m.refresh()
		cmds = append(cmds, tick())
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // viewport handles wheel + arrows over the Damage panel
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	th := themes[m.theme]
	ld := m.layout()

	// header banner: app · character · class/level · zone
	bannerBits := []string{th.fg(th.accent).Bold(true).Render("✦ 99dps"), th.fg(th.dim).Render(m.character)}
	if m.tracker != nil {
		if lv := m.tracker.Level(); lv > 0 {
			bannerBits = append(bannerBits, th.fg(th.dim).Render(fmt.Sprintf("L%d %s", lv, m.tracker.Class())))
		}
		if z := m.tracker.Zone(); z != "" {
			bannerBits = append(bannerBits, th.fg(th.accent).Render("◆ "+z))
		}
	}
	banner := strings.Join(bannerBits, th.fg(th.dim).Render("  ·  "))

	left := lipgloss.JoinVertical(lipgloss.Left,
		card(th, ld.leftW, ld.nowH, "Now", nowBox(th, m.character, m.tracker, ld.leftW-4)),
		card(th, ld.leftW, ld.sessH, "Sessions", sessionsList(th, m.sessions, m.effectiveSel(), ld.leftW-4, ld.sessH-3)))

	var cur *session.CombatSession
	dmgTitle := "Damage"
	if sel := m.effectiveSel(); sel >= 0 {
		cur = m.sessions[sel]
		dmgTitle = "Damage — " + truncate(cur.Name(), ld.rightW-12)
	}

	// bottom row: the class-aware panel + Mob Tracker — enchanters get a third,
	// dedicated Crowd Control column (matching the gocui layout).
	var bottom string
	if m.isEnchanter() {
		classW := ld.rightW * 38 / 100
		ccW := ld.rightW * 30 / 100
		mobW := ld.rightW - classW - ccW - 2
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, classW, ld.botH, classPanelTitle(m.tracker), m.classPanel(cur, classW-4)), " ",
			card(th, ccW, ld.botH, "Crowd Control", ccBody(th, m.tracker, ccW-4)), " ",
			card(th, mobW, ld.botH, "Mob Tracker", mobTracker(th, m.tracker, mobW-4)))
	} else {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, ld.timersW, ld.botH, classPanelTitle(m.tracker), m.classPanel(cur, ld.timersW-4)), " ",
			card(th, ld.mobW, ld.botH, "Mob Tracker", mobTracker(th, m.tracker, ld.mobW-4)))
	}
	right := lipgloss.JoinVertical(lipgloss.Left,
		card(th, ld.rightW, ld.dmgH, dmgTitle, m.vp.View()),
		bottom)

	grid := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	footer := th.fg(th.dim).Render("[t] theme   [↑↓] select fight   [wheel] scroll dmg   [end] live   [q] quit")

	full := lipgloss.JoinVertical(lipgloss.Left, banner, grid, footer)
	return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
		Width(m.w).Height(m.h).Padding(1, 1).Render(full)
}

// damageContent is the scrollable dealer bar chart for the selected fight.
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
	barCells := width - nameW - valW - dpsW - 3 - 4
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
