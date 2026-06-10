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

	// every overflowing panel is independently scrollable (mirrors the gocui
	// per-panel scroll). The mouse wheel scrolls whichever panel it's over.
	vpSessions viewport.Model
	vpDamage   viewport.Model
	vpClass    viewport.Model // spell timers / skills (class-aware bottom panel)
	vpMob      viewport.Model
	vpCC       viewport.Model // enchanter-only Crowd Control column

	w, h  int
	ready bool
}

// New builds the model over a live session manager + (optional) tracker.
func New(sm *session.SessionManager, tracker *gamestate.Tracker, character string) Model {
	return Model{sm: sm, tracker: tracker, character: character, follow: true}
}

func (m Model) Init() tea.Cmd { return tick() }

// layout holds the computed panel rectangles for the current window size. The
// bottom row splits differently for enchanters (a dedicated Crowd Control
// column), so its widths are computed here once and shared by sizing + render.
type layout struct {
	leftW, rightW     int
	nowH, sessH       int
	dmgH, botH        int
	classW, ccW, mobW int // bottom row: class panel | [CC] | mob tracker
	ench              bool
	areaH             int
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

	ld := layout{
		leftW: leftW, rightW: rightW, nowH: nowH, sessH: sessH,
		dmgH: dmgH, botH: botH, areaH: areaH, ench: m.isEnchanter(),
	}
	if ld.ench {
		ld.classW = rightW * 38 / 100
		ld.ccW = rightW * 30 / 100
		ld.mobW = rightW - ld.classW - ld.ccW - 2
	} else {
		ld.classW = rightW / 2
		ld.mobW = rightW - ld.classW - 1
	}
	return ld
}

// cardInner returns the body width/height inside a card of total size w×h
// (border 2 + padding 2 horizontally; border 2 + title line 1 vertically).
func cardInner(w, h int) (int, int) { return w - 4, h - 3 }

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
	th := themes[m.theme]

	m.vpDamage.SetContent(m.damageContent(cur, live, m.vpDamage.Width))
	m.vpSessions.SetContent(sessionsList(th, m.sessions, sel, m.vpSessions.Width))
	m.vpClass.SetContent(m.classPanel(cur, m.vpClass.Width))
	m.vpMob.SetContent(mobTracker(th, m.tracker, m.vpMob.Width))
	if m.isEnchanter() {
		m.vpCC.SetContent(ccBody(th, m.tracker, m.vpCC.Width))
	}
	m.ensureSelVisible(sel)
}

// ensureSelVisible scrolls the Sessions viewport so the selected fight (2 lines
// per fight) stays in view — only nudging when it's off-screen, like the gocui
// ensureVisible (wheel scrolling is otherwise left alone).
func (m *Model) ensureSelVisible(sel int) {
	if sel < 0 {
		return
	}
	line := sel * 2
	top, h := m.vpSessions.YOffset, m.vpSessions.Height
	switch {
	case line < top:
		m.vpSessions.SetYOffset(line)
	case line >= top+h:
		m.vpSessions.SetYOffset(line - h + 2)
	}
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
		m.resizeViewports()
		m.refresh()
	case tea.MouseMsg:
		// route the wheel to whichever panel the cursor is over (gocui parity).
		if vp := m.panelAt(msg.X, msg.Y); vp != nil {
			var cmd tea.Cmd
			*vp, cmd = vp.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	case tickMsg:
		m.refresh()
		cmds = append(cmds, tick())
	}
	return m, tea.Batch(cmds...)
}

// resizeViewports (re)sizes every panel's viewport to its card's inner area for
// the current window. Creates them on the first WindowSizeMsg.
func (m *Model) resizeViewports() {
	ld := m.layout()
	set := func(vp *viewport.Model, w, h int) {
		iw, ih := cardInner(w, h)
		if !m.ready {
			*vp = viewport.New(iw, ih)
		} else {
			vp.Width, vp.Height = iw, ih
		}
	}
	set(&m.vpSessions, ld.leftW, ld.sessH)
	set(&m.vpDamage, ld.rightW, ld.dmgH)
	set(&m.vpClass, ld.classW, ld.botH)
	set(&m.vpMob, ld.mobW, ld.botH)
	set(&m.vpCC, ld.ccW, ld.botH)
	m.ready = true
}

// panelAt returns the viewport whose card contains screen cell (x,y), or nil.
// The geometry mirrors View()'s composition: outer Padding(1,1), a 1-line
// banner, then the grid (left column | right column) — see the layout comments.
func (m *Model) panelAt(x, y int) *viewport.Model {
	ld := m.layout()
	gridY := 2              // outer pad (1) + banner (1)
	rightX := ld.leftW + 2  // outer pad (1) + left column + 1-col gap
	botY := gridY + ld.dmgH // bottom row starts after the Damage card
	in := func(cx, cw, cy, ch int) bool { return x >= cx && x < cx+cw && y >= cy && y < cy+ch }

	switch {
	case in(1, ld.leftW, gridY+ld.nowH, ld.sessH):
		return &m.vpSessions
	case in(rightX, ld.rightW, gridY, ld.dmgH):
		return &m.vpDamage
	case in(rightX, ld.classW, botY, ld.botH):
		return &m.vpClass
	}
	if ld.ench {
		ccX := rightX + ld.classW + 1
		mobX := ccX + ld.ccW + 1
		switch {
		case in(ccX, ld.ccW, botY, ld.botH):
			return &m.vpCC
		case in(mobX, ld.mobW, botY, ld.botH):
			return &m.vpMob
		}
	} else if in(rightX+ld.classW+1, ld.mobW, botY, ld.botH) {
		return &m.vpMob
	}
	return nil
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
		card(th, ld.leftW, ld.sessH, "Sessions", m.vpSessions.View()))

	dmgTitle := "Damage"
	if sel := m.effectiveSel(); sel >= 0 {
		dmgTitle = "Damage — " + truncate(m.sessions[sel].Name(), ld.rightW-12)
	}

	// bottom row: the class-aware panel + Mob Tracker — enchanters get a third,
	// dedicated Crowd Control column (matching the gocui layout). Every panel
	// renders from its own viewport, so each scrolls independently.
	var bottom string
	if ld.ench {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, ld.classW, ld.botH, classPanelTitle(m.tracker), m.vpClass.View()), " ",
			card(th, ld.ccW, ld.botH, "Crowd Control", m.vpCC.View()), " ",
			card(th, ld.mobW, ld.botH, "Mob Tracker", m.vpMob.View()))
	} else {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, ld.classW, ld.botH, classPanelTitle(m.tracker), m.vpClass.View()), " ",
			card(th, ld.mobW, ld.botH, "Mob Tracker", m.vpMob.View()))
	}
	right := lipgloss.JoinVertical(lipgloss.Left,
		card(th, ld.rightW, ld.dmgH, dmgTitle, m.vpDamage.View()),
		bottom)

	grid := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	footer := th.fg(th.dim).Render("[t] theme   [↑↓] select fight   [wheel] scroll panel   [end] live   [q] quit")

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
