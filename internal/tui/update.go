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
		// the inventory quick-filter captures all typing while focused, so letters
		// and digits build the filter instead of firing tab/command shortcuts. Tab
		// (and shift+tab) is the exception: it cancels the search and falls through
		// to normal tab navigation, so you're never stuck typing into the filter.
		if m.screen == screenTools && m.invSearching {
			if s := msg.String(); s == "tab" || s == "shift+tab" {
				m.invSearching, m.invFilter = false, ""
				m.refreshInventory()
			} else {
				m.invSearchKey(msg)
				return m, tea.Batch(cmds...)
			}
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
		if m.screen == screenLog {
			return m.updateLog(msg)
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
		if m.screen == screenLog {
			if isWheel(msg.Button) { // scroll the log; an upward scroll pauses the tail
				if msg.Button == tea.MouseButtonWheelUp {
					m.logFollow = false
				}
				var cmd tea.Cmd
				m.vpLog, cmd = m.vpLog.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		if m.screen == screenCHChain {
			if isWheel(msg.Button) { // route to whichever column the cursor is over
				vp := &m.vpChain
				if raidSplit(m.w-2) && msg.X > gridX+chainCardW(m.w-2) {
					vp = &m.vpRaid
				}
				var cmd tea.Cmd
				*vp, cmd = vp.Update(msg)
				cmds = append(cmds, cmd)
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
			dbl := m.isDoubleClick(msg.X, msg.Y)
			// click a buff target → dismiss; a session → select; a repop → edit it.
			if tgt := m.hoverTargetAt(msg.X, msg.Y); tgt != "" {
				m.tracker.DismissTarget(tgt)
				m.hover = ""
				m.refresh()
				return m, tea.Batch(cmds...)
			}
			if mob := m.mobAt(msg.X, msg.Y); mob != "" {
				m.editing, m.editAdd, m.editMob, m.editBuf = true, false, mob, ""
				m.refresh()
				return m, tea.Batch(cmds...)
			}
			// double-click empty space in the Mob Tracker → add a manual timer.
			if dbl && m.panelAt(msg.X, msg.Y) == &m.vpMob {
				m.editing, m.editAdd, m.editMob, m.editBuf = true, true, "", ""
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
		m.editing, m.editAdd, m.editMob = false, false, ""
		m.invRows, m.invStatus, m.invGen = nil, "", time.Time{} // a new toon has its own inventory
		m.applyCharSettings(m.store.forChar(msg.character))     // load the new toon's saved settings
		if m.ready {
			m.resizeViewports() // a layout change (Off/Compact/Full) needs re-sizing
		}
		m.flash("▶ now tracking " + msg.character)
		m.logTail.AddMark("▶ now tracking "+msg.character, time.Now().Unix()) // divider in the Live Log
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
		case screenLog:
			m.announceCues() // record fired cues first, then redraw the merged sheet
			m.refreshLog()
		case screenCHChain:
			m.announceCues()
			m.refreshRaid() // live cadence/countdowns update every tick
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
	// Live Log tab: one full-width card under banner/tabs, above the footer.
	set(&m.vpLog, m.w-2, max(m.h-5, 4))
	// Raid tab: CH-chain column + raid-debuff column, each its own scroll viewport.
	rh := max(m.h-5, 4)
	set(&m.vpChain, chainCardW(m.w-2), rh)
	set(&m.vpRaid, raidCardW(m.w-2), rh)
	m.ready = true
	m.refreshInventory() // re-render at the new width
	m.refreshLog()
	m.refreshRaid()
}

// refreshInventory re-renders the inventory list into its viewport (after an
// index, a resize, or a theme change).
func (m *Model) refreshInventory() {
	m.vpInv.SetContent(inventoryBoard(themes[m.theme], m.invRows, m.invStatus, m.invFilter, m.invGen, time.Now(), max(m.vpInv.Width, 1)))
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

// doubleClickMaxMillis is how close (in time) two left-presses on roughly the
// same cell must be to count as a double-click.
const doubleClickMaxMillis = 450

// isDoubleClick reports whether the current left-press follows a recent one on
// roughly the same cell, and records this press for the next test.
func (m *Model) isDoubleClick(x, y int) bool {
	now := time.Now()
	dbl := !m.lastClick.IsZero() &&
		now.Sub(m.lastClick) <= doubleClickMaxMillis*time.Millisecond &&
		abs(x-m.lastClickX) <= 1 && abs(y-m.lastClickY) <= 1
	m.lastClick, m.lastClickX, m.lastClickY = now, x, y
	if dbl {
		m.lastClick = time.Time{} // consume it, so a triple-click isn't two doubles
	}
	return dbl
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// parseManual splits a free-text "name [m:ss]" add-buffer into a mob name and an
// optional respawn time (0 = use the zone default). A trailing token that parses
// as a time is taken as the time; otherwise the whole string is the name.
func parseManual(buf string) (string, int) {
	buf = strings.TrimSpace(buf)
	if i := strings.LastIndexByte(buf, ' '); i > 0 {
		if sec, ok := parseRespawn(buf[i+1:]); ok {
			return strings.TrimSpace(buf[:i]), sec
		}
	}
	return buf, 0
}

// editKey feeds a keypress to the open repop editor. In edit mode digits/colon
// build a time and Enter saves a per-(zone,mob) override; in add mode (editAdd)
// the buffer is a free-text "name [m:ss]" and Enter adds a live-only manual
// timer. Esc cancels, Backspace deletes.
func (m *Model) editKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "enter":
		if m.editAdd {
			if name, sec := parseManual(m.editBuf); name != "" && m.tracker != nil {
				if m.tracker.AddManualRespawn(name, sec, time.Now().Unix()) {
					m.flash(fmt.Sprintf("added timer for %s", name))
				} else {
					m.flash("no zone respawn known — give a time (e.g. " + name + " 6:40)")
				}
			}
		} else if sec, ok := parseRespawn(m.editBuf); ok && m.tracker != nil {
			m.tracker.SetOverride(m.editMob, sec)
			m.flash(fmt.Sprintf("%s → %s repop", m.editMob, mmss(int64(sec))))
		}
		m.editing, m.editAdd, m.editBuf, m.editMob = false, false, "", ""
		m.refresh()
	case "esc":
		m.editing, m.editAdd, m.editBuf, m.editMob = false, false, "", ""
		m.refresh()
	case "backspace":
		if len(m.editBuf) > 0 {
			m.editBuf = m.editBuf[:len(m.editBuf)-1]
		}
	default:
		s := msg.String()
		if len(s) != 1 {
			return
		}
		if m.editAdd {
			// a name plus an optional time: letters, digits, spaces, colon.
			if len(m.editBuf) < 28 && (s[0] == ' ' || s[0] == ':' ||
				(s[0] >= '0' && s[0] <= '9') || (s[0] >= 'A' && s[0] <= 'Z') ||
				(s[0] >= 'a' && s[0] <= 'z')) {
				m.editBuf += s
			}
			return
		}
		if len(m.editBuf) < 8 && ((s[0] >= '0' && s[0] <= '9') || s[0] == ':') {
			m.editBuf += s
		}
	}
}
