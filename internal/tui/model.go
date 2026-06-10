// Package tui is the experimental Bubble Tea + Lipgloss UI for 99dps (see
// docs/tui-migration.md). Phase 1: the full multi-panel layout — Now + Sessions
// sidebar, a scrollable Damage panel, Spell Timers and Mob Tracker — themed and
// reading live snapshots. Selected with `99dps -ui tui`.
package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/gamestate"
	"99dps/internal/session"
	"99dps/internal/tts"
)

type tickMsg time.Time

// switchMsg tells the UI a different character's log is now active (sent by the
// log watcher when you switch characters in-game). The session manager and
// tracker are shared pointers already cleared/rebuilt by the watcher, so the
// model only needs the new name and to reset its selection.
type switchMsg struct{ character string }

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
	vpExtras   viewport.Model // Specials / Avoidance, beside the damage meter
	vpClass    viewport.Model // spell timers / skills (class-aware bottom panel)
	vpMob      viewport.Model
	vpCC       viewport.Model // enchanter-only Crowd Control column

	// hover/click-to-dismiss: classTargets/ccTargets map a panel content line to
	// the buff target on it; hover is the target currently under the cursor (its
	// group is highlighted with an ✕). A left-click dismisses that target's timers.
	classTargets map[int]string
	ccTargets    map[int]string
	hover        string

	// TTS audio cues for low buffs; announced re-arms per spell\x00target so each
	// fires once until refreshed/expired.
	speaker   *tts.Speaker
	ttsOn     bool
	announced map[string]bool

	// transient status banner (character switch, action feedback, edit prompt),
	// shown in the footer for a few seconds.
	status   string
	statusAt int64

	// repop respawn editing: click a Mob Tracker row (mobTargets maps a content
	// line → mob) to open an inline editor that writes a per-(zone,mob) override.
	mobTargets map[int]string
	editing    bool
	editBuf    string
	editMob    string

	spellInfo string // data-source summary for the footer

	w, h  int
	ready bool
}

// New builds the model over a live session manager + (optional) tracker.
func New(sm *session.SessionManager, tracker *gamestate.Tracker, character string) Model {
	return Model{
		sm: sm, tracker: tracker, character: character, follow: true,
		speaker: tts.New(), announced: map[string]bool{},
	}
}

func (m Model) Init() tea.Cmd { return tick() }

// flash shows a transient status message in the footer for a few seconds.
func (m *Model) flash(msg string) { m.status, m.statusAt = msg, time.Now().Unix() }

// statusGraceSec is how long a flashed status stays visible.
const statusGraceSec = 5

// parseRespawn reads "h:mm:ss", "m:ss", or plain seconds into total seconds.
func parseRespawn(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	total := 0
	for _, p := range strings.Split(s, ":") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, false
		}
		total = total*60 + n
	}
	return total, total > 0
}

// layout holds the computed panel rectangles for the current window size. The
// bottom row splits differently for enchanters (a dedicated Crowd Control
// column), so its widths are computed here once and shared by sizing + render.
type layout struct {
	leftW, rightW     int
	sessH             int
	dmgH, botH        int
	dmgW, extrasW     int // top-right split: dealer meter | Specials/Avoidance
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
	dmgH := areaH * 52 / 100
	if dmgH < 5 {
		dmgH = 5
	}
	botH := areaH - dmgH - 1
	sessH := dmgH + botH // Sessions spans the full left column (no "Now" box)

	ld := layout{
		leftW: leftW, rightW: rightW, sessH: sessH,
		dmgH: dmgH, botH: botH, areaH: areaH, ench: m.isEnchanter(),
	}
	// top-right splits into the dealer meter (left, the bulk) and a Specials /
	// Avoidance side column, so the meter uses its full height for dealers. Below
	// a threshold there's no room to split, so the meter takes the whole width.
	if rightW < 46 {
		ld.dmgW, ld.extrasW = rightW, 0
	} else {
		ld.extrasW = min(rightW*38/100, 46)
		ld.dmgW = rightW - ld.extrasW - 1
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
	m.vpExtras.SetContent(m.extrasContent(cur, m.vpExtras.Width))
	m.vpSessions.SetContent(sessionsList(th, m.sessions, sel, m.vpSessions.Width))
	m.rebuildInteractive(cur)
	mobStr, mobT := mobTracker(th, m.tracker, m.vpMob.Width, m.editMob)
	m.vpMob.SetContent(mobStr)
	m.mobTargets = mobT
	m.announceLowBuffs()
	m.ensureSelVisible(sel)
}

// announceLowBuffs speaks a cue when a (non-charm) timer first drops below the
// low threshold, once per timer, re-arming when refreshed or expired. Mirrors
// the gocui App.dueAnnouncements.
func (m *Model) announceLowBuffs() {
	if !m.ttsOn || m.tracker == nil || m.speaker == nil {
		return
	}
	const lowBuffSec = 15
	now := time.Now().Unix()
	live := map[string]bool{}
	for _, tm := range m.tracker.Active(now) {
		if tm.Charm {
			continue // charm breaks before its cap — a countdown "low" would cry wolf
		}
		k := tm.Spell + "\x00" + tm.Target
		live[k] = true
		if tm.Expiry-now <= lowBuffSec {
			if !m.announced[k] {
				m.announced[k] = true
				phrase := tm.Spell + " low"
				if tm.Target != "You" {
					phrase = tm.Target + ", " + tm.Spell + " low"
				}
				m.speaker.Say(phrase)
			}
		} else {
			delete(m.announced, k) // refreshed / still healthy → re-arm
		}
	}
	for k := range m.announced {
		if !live[k] {
			delete(m.announced, k) // timer gone → re-arm for next cast
		}
	}
}

// rebuildInteractive re-renders the hover-aware panels (class + enchanter CC)
// and refreshes their line→target maps. Cheap enough to call on every mouse
// move so the highlight tracks the cursor without a full snapshot pass.
func (m *Model) rebuildInteractive(cur *session.CombatSession) {
	th := themes[m.theme]
	classStr, classT := m.classPanel(cur, m.vpClass.Width, m.hover)
	m.vpClass.SetContent(classStr)
	m.classTargets = classT
	if m.isEnchanter() {
		ccStr, ccT := ccBody(th, m.tracker, m.vpCC.Width, m.hover)
		m.vpCC.SetContent(ccStr)
		m.ccTargets = ccT
	}
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
		// the repop editor captures keys while open (digits/colon, Enter, Esc).
		if m.editing {
			m.editKey(msg)
			return m, tea.Batch(cmds...)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "t", "tab":
			m.theme = (m.theme + 1) % len(themes)
		case "a":
			m.toggleTTS()
		case "backspace":
			m.sm.Clear()
			m.selected, m.follow, m.hover = 0, true, ""
			m.flash("cleared sessions")
			m.refresh()
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
		if msg.Width <= 0 || msg.Height <= 0 {
			return m, tea.Batch(cmds...) // ignore a degenerate early size — stay "starting…"
		}
		m.w, m.h = msg.Width, msg.Height
		m.resizeViewports()
		m.refresh()
	case tea.MouseMsg:
		// wheel → scroll whichever panel the cursor is over (gocui parity).
		if isWheel(msg.Button) {
			if vp := m.panelAt(msg.X, msg.Y); vp != nil {
				var cmd tea.Cmd
				*vp, cmd = vp.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			// click a buff target → dismiss; a session → select; a repop → edit it.
			if tgt := m.hoverTargetAt(msg.X, msg.Y); tgt != "" {
				m.tracker.DismissTarget(tgt)
				m.hover = ""
				m.refresh()
				return m, tea.Batch(cmds...)
			}
			if i := m.sessionAt(msg.X, msg.Y); i >= 0 {
				m.selected, m.follow = i, i == len(m.sessions)-1
				m.refresh()
				return m, tea.Batch(cmds...)
			}
			if mob := m.mobAt(msg.X, msg.Y); mob != "" {
				m.editing, m.editMob, m.editBuf = true, mob, ""
				m.refresh()
				return m, tea.Batch(cmds...)
			}
			return m, tea.Batch(cmds...)
		}
		tgt := m.hoverTargetAt(msg.X, msg.Y)
		// motion → update the hover highlight only when the target changes.
		if tgt != m.hover {
			m.hover = tgt
			var cur *session.CombatSession
			if sel := m.effectiveSel(); sel >= 0 {
				cur = m.sessions[sel]
			}
			m.rebuildInteractive(cur)
		}
		return m, tea.Batch(cmds...)
	case switchMsg:
		m.character = msg.character
		m.selected, m.follow, m.hover = 0, true, ""
		m.editing, m.editMob = false, ""
		m.flash("▶ now tracking " + msg.character)
		m.refresh()
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
	set(&m.vpDamage, ld.dmgW, ld.dmgH)
	set(&m.vpClass, ld.classW, ld.botH)
	set(&m.vpMob, ld.mobW, ld.botH)
	set(&m.vpCC, ld.ccW, ld.botH)
	// the extras card is title-less, so its body gets one more row (h-2, not h-3);
	// clamp to 0 when there's no side column (narrow terminal).
	if ew, eh := max(ld.extrasW-4, 0), max(ld.dmgH-2, 0); !m.ready {
		m.vpExtras = viewport.New(ew, eh)
	} else {
		m.vpExtras.Width, m.vpExtras.Height = ew, eh
	}
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
	case in(1, ld.leftW, gridY, ld.sessH):
		return &m.vpSessions
	case in(rightX, ld.dmgW, gridY, ld.dmgH):
		return &m.vpDamage
	case in(rightX+ld.dmgW+1, ld.extrasW, gridY, ld.dmgH):
		return &m.vpExtras
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

// isWheel reports whether a mouse button is a scroll-wheel direction.
func isWheel(b tea.MouseButton) bool {
	return b == tea.MouseButtonWheelUp || b == tea.MouseButtonWheelDown ||
		b == tea.MouseButtonWheelLeft || b == tea.MouseButtonWheelRight
}

// hoverTargetAt resolves the buff target under screen cell (x,y) in the
// class-aware or CC panel, or "" if the cursor isn't over a dismissable name.
// Content begins 2 rows below the card top (border + title); the panel's scroll
// offset is added back so the lookup matches the rendered line.
func (m *Model) hoverTargetAt(x, y int) string {
	ld := m.layout()
	gridY := 2
	rightX := ld.leftW + 2
	botY := gridY + ld.dmgH
	contentTop := botY + 2           // border (1) + title (1)
	contentBot := botY + ld.botH - 1 // inside the bottom border
	if y < contentTop || y >= contentBot {
		return ""
	}
	// class panel body (inside its border + padding)
	if x > rightX && x < rightX+ld.classW-1 {
		return m.classTargets[(y-contentTop)+m.vpClass.YOffset]
	}
	if ld.ench {
		ccX := rightX + ld.classW + 1
		if x > ccX && x < ccX+ld.ccW-1 {
			return m.ccTargets[(y-contentTop)+m.vpCC.YOffset]
		}
	}
	return ""
}

// sessionAt returns the session index under screen cell (x,y) in the Sessions
// panel (2 lines per fight), or -1.
func (m *Model) sessionAt(x, y int) int {
	ld := m.layout()
	if x < 1 || x >= 1+ld.leftW {
		return -1
	}
	contentTop := 2 + 2 // gridY + border + title
	if y < contentTop || y >= 2+ld.sessH-1 {
		return -1
	}
	idx := ((y - contentTop) + m.vpSessions.YOffset) / 2
	if idx < 0 || idx >= len(m.sessions) {
		return -1
	}
	return idx
}

// mobAt returns the repop mob under screen cell (x,y) in the Mob Tracker, or "".
func (m *Model) mobAt(x, y int) string {
	ld := m.layout()
	rightX := ld.leftW + 2
	botY := 2 + ld.dmgH
	contentTop := botY + 2
	if y < contentTop || y >= botY+ld.botH-1 {
		return ""
	}
	mobX := rightX + ld.classW + 1
	if ld.ench {
		mobX += ld.ccW + 1
	}
	if x < mobX || x >= mobX+ld.mobW {
		return ""
	}
	return m.mobTargets[(y-contentTop)+m.vpMob.YOffset]
}

// editKey feeds a keypress to the open repop editor: digits/colon build the time,
// Enter saves a per-(zone,mob) override, Esc cancels, Backspace deletes.
func (m *Model) editKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "enter":
		if sec, ok := parseRespawn(m.editBuf); ok && m.tracker != nil {
			m.tracker.SetOverride(m.editMob, sec)
			m.flash(fmt.Sprintf("%s → %s repop", m.editMob, mmss(int64(sec))))
		}
		m.editing, m.editBuf, m.editMob = false, "", ""
		m.refresh()
	case "esc":
		m.editing, m.editBuf, m.editMob = false, "", ""
		m.refresh()
	case "backspace":
		if len(m.editBuf) > 0 {
			m.editBuf = m.editBuf[:len(m.editBuf)-1]
		}
	default:
		if s := msg.String(); len(s) == 1 && len(m.editBuf) < 8 {
			if (s[0] >= '0' && s[0] <= '9') || s[0] == ':' {
				m.editBuf += s
			}
		}
	}
}

// toggleTTS flips audio cues at runtime, flashing feedback (no-op without an
// engine).
func (m *Model) toggleTTS() {
	if m.speaker == nil || !m.speaker.Available() {
		m.flash("no TTS engine (install spd-say or espeak)")
		return
	}
	m.ttsOn = !m.ttsOn
	if m.ttsOn {
		m.speaker.Say("audio cues on")
		m.flash("♪ audio cues on")
	} else {
		m.flash("audio cues off")
	}
}

// banner is the full-width header plaque (dark text on muted gold, like the EQ
// zone plaques) so it clearly reads as the header: app · character · class-level
// · zone on the left, with the zone kills/hr pushed to the right.
func (m Model) banner(th theme, w int) string {
	bar := lipgloss.NewStyle().Background(lipgloss.Color(th.accentLo)).Foreground(lipgloss.Color(th.bg))
	sep := bar.Render("  ·  ")

	bits := []string{bar.Bold(true).Render("✦ 99dps"), bar.Bold(true).Render(m.character)}
	var right string
	if m.tracker != nil {
		if lv := m.tracker.Level(); lv > 0 {
			bits = append(bits, bar.Render(fmt.Sprintf("L%d %s", lv, m.tracker.Class())))
		}
		if z := m.tracker.Zone(); z != "" {
			bits = append(bits, bar.Bold(true).Render("◆ "+z))
		}
		if k, ph, d := m.tracker.ZoneKillStats(time.Now().Unix()); k > 0 || d > 0 {
			r := ""
			if k > 0 {
				r = fmt.Sprintf("%d kills · %d/hr", k, ph)
			}
			if d > 0 {
				if r != "" {
					r += " · "
				}
				r += fmt.Sprintf("%d deaths", d)
			}
			right = bar.Bold(true).Render(r)
		}
	}
	left := " " + strings.Join(bits, sep)

	line := left
	if right != "" {
		if gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 1; gap >= 2 {
			line = left + strings.Repeat(" ", gap) + right + " "
		}
	}
	return bar.Width(w).MaxWidth(w).Render(line)
}

func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	th := themes[m.theme]
	ld := m.layout()

	innerW := m.w - 2 // content width inside the outer Padding(1,1)
	banner := m.banner(th, innerW)

	left := card(th, ld.leftW, ld.sessH, "Sessions", m.vpSessions.View())

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
	// top-right: the dealer meter beside its Specials / Avoidance side column.
	// The side card has no title — its SPECIALS / AVOIDANCE section headers label
	// it. On a narrow terminal there's no side column; the meter takes the width.
	topRight := card(th, ld.dmgW, ld.dmgH, dmgTitle, m.vpDamage.View())
	if ld.extrasW > 0 {
		topRight = lipgloss.JoinHorizontal(lipgloss.Top,
			topRight, " ", card(th, ld.extrasW, ld.dmgH, "", m.vpExtras.View()))
	}
	right := lipgloss.JoinVertical(lipgloss.Left, topRight, bottom)

	grid := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	full := lipgloss.JoinVertical(lipgloss.Left, banner, grid, m.footer(th, innerW))
	return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
		Width(m.w).Height(m.h).Padding(1, 1).Render(full)
}

// footer is the bottom line: the open repop editor's prompt, else a transient
// status flash, else the keybinding help + audio/data-source state.
func (m Model) footer(th theme, w int) string {
	switch {
	case m.editing:
		return th.fg(th.accent).Bold(true).Render(truncate(
			fmt.Sprintf("set repop for %s (m:ss): %s_   [enter] save   [esc] cancel", m.editMob, m.editBuf), w))
	case m.status != "" && time.Now().Unix()-m.statusAt <= statusGraceSec:
		return th.fg(th.accent).Bold(true).Render(truncate(m.status, w))
	}
	audio := "audio off"
	if m.ttsOn {
		audio = "♪ audio on"
	} else if m.speaker == nil || !m.speaker.Available() {
		audio = "audio n/a"
	}
	keys := "[t] theme  [a] " + audio + "  [↑↓/click] fight  [end] live  [wheel] scroll  [bksp] clear  [click repop] set timer  [q] quit"
	if m.spellInfo != "" {
		keys = m.spellInfo + "  ·  " + keys
	}
	return th.fg(th.dim).Render(truncate(keys, w))
}

// damageContent is the scrollable damage breakdown for the selected fight: an
// encounter summary, a ranked per-dealer table (share bar + DPS/Total/% and
// width-gated Hit%/Crit%), the unattributed spell line, then the Specials and
// Avoidance sub-tables — matching the old gocui Damage panel.
func (m Model) damageContent(cur *session.CombatSession, live bool, width int) string {
	th := themes[m.theme]
	if cur == nil {
		return th.fg(th.dim).Render("No fight selected.\nFight something!")
	}
	stats := cur.GetAggressors()
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	magic := cur.MagicTotal()
	encTotal := cur.Total() + magic // melee + unattributed spell damage
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

	// encounter summary: duration · total · dps · live/ended. Colored when it
	// fits; clipped (plain) on a very narrow panel so it never overflows.
	lead := fmt.Sprintf("%s · %s · %s/s", fmtDuration(cur.Duration()), humanize(encTotal), humanize(int(int64(encTotal)/span)))
	statusText := "● live"
	statusColor := "#5fd37a"
	if !live {
		statusText, statusColor = "○ ended "+cur.EndTime().Format("15:04:05"), th.dim
	}
	summary := th.fg(th.dim).Render(lead+"   ") + th.fg(statusColor).Render(statusText)
	if lipgloss.Width(lead)+3+lipgloss.Width(statusText) > width {
		summary = th.fg(th.dim).Render(truncate(lead+"  "+statusText, width))
	}

	// Columns flex with width: Total+DPS are always shown; %, Hit%, Crit% are
	// added as room allows; the name takes the rest, and a share bar fills any
	// slack beyond the name — so a row is always exactly the panel width.
	const rankW, nameMax, pctW, totW, dpsW = 2, 14, 4, 7, 7
	showPct := width >= 34
	showHit := width >= 60
	showCrit := width >= 70

	num := totW + 1 + dpsW // the right-aligned numeric block width
	if showPct {
		num += pctW + 1
	}
	if showHit {
		num += 1 + 5
	}
	if showCrit {
		num += 1 + 5
	}
	nameW := width - num - 1 - rankW - 1 // rank + gaps + name fill the left
	barCells, showBar := 0, false
	if nameW > nameMax {
		if barCells = nameW - nameMax - 1; barCells >= 4 {
			nameW, showBar = nameMax, true
		} else {
			barCells = 0
		}
	}
	nameW = max(nameW, 1)

	// numBlock right-aligns the numeric columns into exactly `num` cells.
	numBlock := func(pctv, total, dps int, hit, crit, col string) string {
		s := ""
		if showPct {
			s += rightCell(fmt.Sprintf("%d%%", pctv), pctW, col) + " "
		}
		s += rightCell(humanize(total), totW, col) + " " + rightCell(humanize(dps), dpsW, th.dim)
		if showHit {
			s += " " + rightCell(hit, 5, th.dim)
		}
		if showCrit {
			s += " " + rightCell(crit, 5, th.dim)
		}
		return s
	}

	var b strings.Builder
	b.WriteString(summary + "\n")
	b.WriteString(strings.Repeat(" ", max(width-num, 0)) +
		numBlockLabels(th, showPct, showHit, showCrit, pctW, totW, dpsW) + "\n")

	row := func(rankStr string, nameCell, hit, crit string, frac float64, from, to, col string, total, dps, pctv int) string {
		mid := ""
		if showBar {
			mid = gradientBar(frac, barCells, from, to, th.track) + " "
		}
		return rightCell(rankStr, rankW, th.dim) + " " + nameCell + " " + mid +
			numBlock(pctv, total, dps, hit, crit, col)
	}

	for i, d := range stats {
		from, to := th.barFrom, th.barTo
		if i > 0 {
			from, to = th.accent, th.accentLo
		}
		nameStyle := th.fg(th.text)
		col := th.text
		if strings.EqualFold(d.Dealer, "you") {
			nameStyle = nameStyle.Bold(true).Foreground(lipgloss.Color(th.accent))
		}
		hit := "-"
		if hr := cur.OffenseFor(d.Dealer).HitRate(); hr >= 0 {
			hit = fmt.Sprintf("%d%%", hr)
		}
		crit := "-"
		if c := cur.CritFor(d.Dealer); c.Count > 0 && d.Hits > 0 {
			crit = fmt.Sprintf("%d%%", critPct(c.Count, d.Hits))
		}
		b.WriteString(row(fmt.Sprintf("%d", i+1), nameStyle.Width(nameW).Render(truncate(d.Dealer, nameW)),
			hit, crit, float64(d.Total)/float64(maxTotal), from, to, col, d.Total, d.Total/int(span), pct(d.Total, encTotal)) + "\n")
	}

	// unattributed spell/proc/DoT damage — EQ names no caster, so it's its own
	// (n/a) line folded into the encounter total.
	if magic > 0 {
		b.WriteString(row(strings.Repeat(" ", rankW), th.fg(th.dim).Width(nameW).Render(truncate("spells n/a", nameW)),
			"-", "-", float64(magic)/float64(maxTotal), th.dim, th.dim, th.dim, magic, magic/int(span), pct(magic, encTotal)) + "\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// numBlockLabels renders the damage table's right-aligned column header to match
// numBlock's column set and width.
func numBlockLabels(th theme, showPct, showHit, showCrit bool, pctW, totW, dpsW int) string {
	s := ""
	if showPct {
		s += rightCell("%", pctW, th.dim) + " "
	}
	s += rightCell("Total", totW, th.dim) + " " + rightCell("DPS", dpsW, th.dim)
	if showHit {
		s += " " + rightCell("Hit", 5, th.dim)
	}
	if showCrit {
		s += " " + rightCell("Crit", 5, th.dim)
	}
	return s
}

// extrasContent is the side column beside the damage meter: the per-dealer,
// per-kind Specials breakdown and the Avoidance table, stacked and scrollable.
func (m Model) extrasContent(cur *session.CombatSession, width int) string {
	th := themes[m.theme]
	if cur == nil {
		return th.fg(th.dim).Render("—")
	}
	stats := cur.GetAggressors()
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })

	var parts []string
	if sp := damageSpecials(th, cur, stats, width); sp != "" {
		parts = append(parts, sp)
	}
	if av := damageAvoidance(th, cur, width); av != "" {
		parts = append(parts, av)
	}
	if len(parts) == 0 {
		return th.fg(th.dim).Render("No specials or\navoidance yet.")
	}
	return strings.Join(parts, "\n\n")
}

// Program wraps the Bubble Tea program so the host can push in a character
// switch (detected by the log watcher) while the UI is running.
type Program struct{ p *tea.Program }

// NewProgram builds the program over the shared manager + tracker. spellInfo is
// the data-source summary for the footer; tts enables audio cues at startup.
func NewProgram(sm *session.SessionManager, tracker *gamestate.Tracker, character, spellInfo string, ttsOn bool) *Program {
	m := New(sm, tracker, character)
	m.spellInfo = spellInfo
	m.ttsOn = ttsOn && m.speaker.Available()
	return &Program{p: tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())}
}

// Run launches the UI; blocks until the user quits.
func (pr *Program) Run() error {
	_, err := pr.p.Run()
	return err
}

// SwitchCharacter notifies the running UI that a different character's log is now
// active. Safe to call from another goroutine. No-op once the program has quit.
func (pr *Program) SwitchCharacter(name string) {
	pr.p.Send(switchMsg{character: name})
}
