package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/session"
)

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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	// the first-run setup screen owns all input until the user finishes/skips it.
	if m.screen == screenSetup {
		return m.updateSetup(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit // always quits, even mid-edit
		}
		// the repop editor captures keys while open (digits/colon, Enter, Esc).
		if m.editing {
			m.editKey(msg)
			return m, tea.Batch(cmds...)
		}
		// global: quit + tab-bar navigation (works on every screen).
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab":
			return m.gotoScreen(m.cycleTab(+1)), nil
		case "shift+tab":
			return m.gotoScreen(m.cycleTab(-1)), nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// a digit jumps to that tab (1-based), so adding tabs needs no new key wiring.
			if n := int(msg.String()[0] - '0'); n >= 1 && n <= len(tabs) {
				return m.gotoScreen(tabs[n-1].scr), nil
			}
		}
		// each tab owns the rest of its keys.
		if m.screen == screenSettings {
			return m.updateSettings(msg)
		}
		if m.screen == screenSessions {
			return m.updateSessions(msg)
		}
		if m.screen == screenTools {
			return m.updateTools(msg)
		}
		if m.screen == screenMap {
			return m.updateMap(msg)
		}
		switch msg.String() {
		case "t":
			m.theme = (m.theme + 1) % len(themes)
		case "a":
			cmds = append(cmds, m.toggleTTS())
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
		// a left-click on the tab bar switches screens (on any screen).
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if scr, ok := m.tabAt(msg.X, msg.Y); ok {
				return m.gotoScreen(scr), nil
			}
		}
		if m.screen == screenSessions {
			return m.mouseSessions(msg)
		}
		if m.screen == screenSettings {
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				if col, sel, ok := m.settingsClickAt(msg.X, msg.Y); ok {
					m.settingsCol = col
					if col == 0 {
						m.settingsSel = sel
						cmds = append(cmds, m.applyLeftSetting(sel))
					} else {
						m.cueSel = sel
						m.toggleCue(sel)
					}
				}
			}
			return m, tea.Batch(cmds...)
		}
		if m.screen == screenTools {
			if isWheel(msg.Button) { // scroll the inventory list
				var cmd tea.Cmd
				m.vpInv, cmd = m.vpInv.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		if m.screen == screenMap {
			switch msg.Button { // wheel zooms the map
			case tea.MouseButtonWheelUp:
				m.zoomIn()
			case tea.MouseButtonWheelDown:
				m.zoomOut()
			}
			return m, tea.Batch(cmds...)
		}
		if m.screen != screenMeter {
			return m, tea.Batch(cmds...)
		}
		// wheel → scroll whichever panel the cursor is over.
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
	case invLoadedMsg:
		m.invRows, m.invStatus, m.invGen = msg.rows, msg.status, msg.gen
		m.refreshInventory()
	case mapLoadedMsg:
		if msg.short == m.mapShort { // ignore a stale load if the zone changed
			m.curMap, m.mapStatus, m.mapSel = msg.m, msg.status, 0
		}
	case switchMsg:
		m.character = msg.character
		m.selected, m.follow, m.hover = 0, true, ""
		m.editing, m.editMob = false, ""
		m.invRows, m.invStatus, m.invGen = nil, "", time.Time{} // a new toon has its own inventory
		m.applyCharSettings(m.store.forChar(msg.character))     // load the new toon's saved settings
		if m.ready {
			m.resizeViewports() // a layout change (Off/Compact/Full) needs re-sizing
		}
		m.flash("▶ now tracking " + msg.character)
		m.refresh()
	case tickMsg:
		switch m.screen {
		case screenMeter:
			m.refresh()
		case screenSessions:
			m.refreshSessions()
		case screenTools:
			m.refreshInventory() // re-render for theme/status changes
			m.announceCues()
		case screenMap:
			if cmd := m.maybeLoadMapCmd(); cmd != nil { // fetch on entry / zone change
				cmds = append(cmds, cmd)
			}
			m.announceCues()
		default:
			m.announceCues() // keep audio alerts live on other screens
		}
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
		iw, ih = max(iw, 1), max(ih, 1) // a hidden box has w/h 0 → keep the vp valid
		if !m.ready {
			*vp = viewport.New(iw, ih)
		} else {
			vp.Width, vp.Height = iw, ih
		}
	}
	set(&m.vpDamage, ld.dmgW, ld.dmgH)
	// Sessions tab (own screen): table 3/4, DPS breakdown 1/4. The table reserves
	// one extra row for its sticky column header (rendered outside the viewport).
	tableW, breakW, fullH := m.sessionsLayout()
	set(&m.vpSessTable, tableW, fullH)
	m.vpSessTable.Height = max(m.vpSessTable.Height-1, 1)
	set(&m.vpSessBreak, breakW, fullH)
	set(&m.vpClass, ld.classW, ld.botH)
	set(&m.vpMob, ld.mobW, ld.mobH) // shorter when the class box splits the column
	set(&m.vpEnemy, ld.enemyW, ld.botH)
	// the extras card has a title ("Offense · Defense"), so reserve its row (h-3,
	// like cardInner); clamp to 0 when there's no side column (narrow terminal).
	if ew, eh := max(ld.extrasW-4, 0), max(ld.dmgH-3, 0); !m.ready {
		m.vpExtras = viewport.New(ew, eh)
	} else {
		m.vpExtras.Width, m.vpExtras.Height = ew, eh
	}
	// Tools tab: the inventory column is its own scrollable viewport.
	tColW, _, tH := m.toolsLayout()
	set(&m.vpInv, tColW, tH)
	m.ready = true
	m.refreshInventory() // re-render at the new width
}

// refreshInventory re-renders the inventory list into its viewport (after an
// index, a resize, or a theme change).
func (m *Model) refreshInventory() {
	m.vpInv.SetContent(inventoryBoard(themes[m.theme], m.invRows, m.invStatus, m.invGen, time.Now(), max(m.vpInv.Width, 1)))
}

// panelAt returns the viewport whose card contains screen cell (x,y), or nil.
// The geometry mirrors View()'s composition: outer Padding(1,1), a 1-line
// banner, then the grid (left column | right column) — see the layout comments.
func (m *Model) panelAt(x, y int) *viewport.Model {
	ld := m.layout()
	gridY := gridTop        // outer pad (1) + banner (1)
	rightX := gridX         // meter spans full width from the content left edge
	botY := gridY + ld.dmgH // bottom row starts after the Damage card
	in := func(cx, cw, cy, ch int) bool { return x >= cx && x < cx+cw && y >= cy && y < cy+ch }

	extrasX := rightX // Offense·Defense sits beside the meter, or at the edge if it's off
	if ld.dmgW > 0 {
		extrasX = rightX + ld.dmgW + 1
	}
	switch {
	case ld.dmgW > 0 && in(rightX, ld.dmgW, gridY, ld.dmgH):
		return &m.vpDamage
	case ld.extrasW > 0 && in(extrasX, ld.extrasW, gridY, ld.dmgH):
		return &m.vpExtras
	case in(rightX, ld.classW, botY, ld.botH):
		return &m.vpClass
	}
	if ld.enemy {
		ccX := rightX + ld.classW + 1
		mobX := ccX + ld.enemyW + 1
		switch {
		case in(ccX, ld.enemyW, botY, ld.botH):
			return &m.vpEnemy
		case in(mobX, ld.mobW, botY, ld.mobH):
			return &m.vpMob
		}
	} else if in(rightX+ld.classW+1, ld.mobW, botY, ld.mobH) {
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
	gridY := gridTop
	rightX := gridX // meter spans full width from the content left edge
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
	if ld.enemy {
		ccX := rightX + ld.classW + 1
		if x > ccX && x < ccX+ld.enemyW-1 {
			return m.enemyTargets[(y-contentTop)+m.vpEnemy.YOffset]
		}
	}
	return ""
}

// mobAt returns the repop mob under screen cell (x,y) in the Mob Tracker, or "".
func (m *Model) mobAt(x, y int) string {
	ld := m.layout()
	rightX := gridX // meter spans full width from the content left edge
	botY := gridTop + ld.dmgH
	contentTop := botY + 2
	if y < contentTop || y >= botY+ld.mobH-1 { // mob column is shorter when the class box splits it
		return ""
	}
	mobX := rightX + ld.classW + 1
	if ld.enemy {
		mobX += ld.enemyW + 1
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
