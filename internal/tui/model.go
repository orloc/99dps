// Package tui is the Bubble Tea + Lipgloss UI for 99dps — the only UI (it
// replaced the legacy gocui one). A single Model renders the banner, tab bar, Damage meter + Specials/Avoidance, the class-aware panel, and the Mob
// Tracker from lock-free session/tracker snapshots, themed and truecolor.
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

	"99dps/internal/combat"
	"99dps/internal/eqclass"
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
// the parser goroutine feeds the manager exactly as before.
type Model struct {
	sm        *session.SessionManager
	tracker   *gamestate.Tracker
	character string
	theme     int

	sessions []*session.CombatSession // last snapshot (meter + Sessions tab)
	selected int                      // pinned session index
	follow   bool                     // glue selection to the live fight

	// every overflowing panel is independently scrollable (mirrors the previous
	// per-panel scroll). The mouse wheel scrolls whichever panel it's over.
	vpDamage viewport.Model
	vpExtras viewport.Model // Specials / Avoidance, beside the damage meter
	vpClass  viewport.Model // spell timers / skills (class-aware bottom panel)
	vpMob    viewport.Model
	vpEnemy  viewport.Model // Enemy column: CC + debuffs (caster/hybrid)

	// Sessions tab: a wide scrollable stats table + a DPS breakdown of the
	// highlighted session. sessHeader is the sticky column header (rendered above
	// the scroll viewport); sessRows maps a body line → session index.
	vpSessTable viewport.Model
	vpSessBreak viewport.Model
	sessHeader  string
	sessRows    map[int]int

	// hover/click-to-dismiss: classTargets/enemyTargets map a panel content line to
	// the buff target on it; hover is the target currently under the cursor (its
	// group is highlighted with an ✕). A left-click dismisses that target's timers.
	classTargets map[int]string
	enemyTargets map[int]string
	hover        string

	// TTS audio cues for low buffs; announced re-arms per spell\x00target so each
	// fires once until refreshed/expired.
	speaker   tts.Engine
	ttsOn     bool
	announced map[string]bool

	// urgent combat-cue dedup (fire once per event, re-arm when it clears) and a
	// rotating sequence for natural phrasing variety.
	charmAnnounced     bool
	resistAnnounced    string
	feignFailAnnounced bool
	cdReady            map[string]bool // long cooldown name → was-ready last tick
	cueSeq             int

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

	spellInfo  string // data-source summary for the footer
	canniBlock string // pre-rendered canni dance meter, pinned under the Damage panel

	// screen selects the active view (meter / settings tab / first-run setup).
	screen      screen
	setup       setupState
	settingsSel int // selected row on the Settings tab (0 = audio toggle, 1..n = voices)

	w, h  int
	ready bool
}

// New builds the model over a live session manager + (optional) tracker.
func New(sm *session.SessionManager, tracker *gamestate.Tracker, character string) Model {
	return Model{
		sm: sm, tracker: tracker, character: character, follow: true,
		speaker: tts.New(), announced: map[string]bool{}, cdReady: map[string]bool{},
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
// bottom row gets a dedicated "Enemy" column (CC + debuffs) for caster/hybrid
// classes when there's room; widths are computed here and shared by sizing +
// render.
type layout struct {
	rightW               int // full content width (the sidebar was removed)
	dmgH, botH           int
	dmgW, extrasW        int // top-right split: dealer meter | Specials/Avoidance
	classW, enemyW, mobW int // bottom row: class panel | [Enemy] | mob tracker
	enemy                bool
	areaH                int
}

// gridX is the screen X of the meter's first content column (just inside the
// outer padding). The Sessions sidebar used to sit left of it; now the meter
// spans the full width from here.
const gridX = 1

// enemyColMinW is the right-column width below which the bottom row drops the
// Enemy column (falls back to class | mob, with CC+debuffs back in the class panel).
const enemyColMinW = 60

// gridTop is the Y of the first grid row: outer padding (1) + banner (1) + the
// tab bar (1). Every mouse hit-tester derives from it so the tab row can't drift
// the click math.
const gridTop = 3

func (m Model) layout() layout {
	innerW := m.w - 2
	areaH := m.h - 5 // outer padding + banner + tab bar + footer
	if areaH < 6 {
		areaH = 6
	}
	rightW := innerW // the meter spans the full width now (no Sessions sidebar)
	dmgH := areaH * 52 / 100
	if dmgH < 5 {
		dmgH = 5
	}
	botH := areaH - dmgH - 1

	// every class gets a middle column when it fits: casters/hybrids → Enemy
	// (CC + debuffs on mobs), melee → Buffs (self-buff / clicky timers). The same
	// timerColumn code feeds both, just different sections. A too-narrow window
	// drops it and the content folds back into the class panel.
	enemy := m.tracker != nil && rightW >= enemyColMinW

	ld := layout{
		rightW: rightW,
		dmgH:   dmgH, botH: botH, areaH: areaH, enemy: enemy,
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
	if enemy {
		ld.classW = rightW * 38 / 100
		ld.enemyW = rightW * 30 / 100
		ld.mobW = rightW - ld.classW - ld.enemyW - 2
	} else {
		ld.classW = rightW / 2
		ld.mobW = rightW - ld.classW - 1
	}
	return ld
}

// middleIsBuffs reports whether the bottom-row middle column should hold self
// BUFFS (pure melee) rather than the Enemy CC+debuffs (caster/hybrid).
func (m Model) middleIsBuffs() bool {
	return m.tracker != nil && m.tracker.Category() == eqclass.CatMelee
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

	// the canni dance meter is pinned to the bottom of the Damage panel; reserve
	// its lines from the dealer viewport so it's always visible (it never scrolls
	// away with the dealer list). Recomputed each tick since the dance state moves.
	var canniLines int
	if m.tracker != nil {
		m.canniBlock, canniLines = canniFooter(th, m.tracker.CanniStats(time.Now().Unix()), m.vpDamage.Width)
	} else {
		m.canniBlock = ""
	}
	ld := m.layout()
	_, dmgInnerH := cardInner(ld.dmgW, ld.dmgH)
	m.vpDamage.Height = max(dmgInnerH-canniLines, 1)

	m.vpDamage.SetContent(m.damageContent(cur, live, m.vpDamage.Width))
	m.vpExtras.SetContent(m.extrasContent(cur, m.vpExtras.Width))
	m.rebuildInteractive(cur)
	mobStr, mobT := mobTracker(th, m.tracker, m.vpMob.Width, m.editMob)
	m.vpMob.SetContent(mobStr)
	m.mobTargets = mobT
	m.announceCues()
}

// announceCues speaks this tick's audio cues. Urgent combat alerts (charm break,
// resist) use SayUrgent — a snappier voice that jumps the playback queue — and
// each fires once per event. Buff/debuff fades are gentler, combined into one
// varied sentence so simultaneous fades don't overlap. No-op when audio is off.
func (m *Model) announceCues() { m.announceCuesAt(time.Now().Unix()) }

// announceCuesAt is the testable core (now is injected). Urgent alerts use
// SayUrgent (snappier, jump the queue); informational ones use Say.
func (m *Model) announceCuesAt(now int64) {
	if !m.ttsOn || m.tracker == nil || m.speaker == nil {
		return
	}

	// charm break — scary and time-critical.
	if m.tracker.CharmBroke(now) {
		if !m.charmAnnounced {
			m.charmAnnounced = true
			m.speaker.SayUrgent(charmBreakPhrase(m.cueSeq))
			m.cueSeq++
		}
	} else {
		m.charmAnnounced = false
	}

	// a failed feign death — you're still being attacked. urgent.
	if m.tracker.FeignStatus(now) == gamestate.FeignFailed {
		if !m.feignFailAnnounced {
			m.feignFailAnnounced = true
			m.speaker.SayUrgent(feignFailPhrase(m.cueSeq))
			m.cueSeq++
		}
	} else {
		m.feignFailAnnounced = false
	}

	// a cast that didn't land — alert so you re-cast.
	if spell, ok := m.tracker.Resisted(now); ok {
		if spell != m.resistAnnounced {
			m.resistAnnounced = spell
			m.speaker.SayUrgent(resistPhrase(spell, m.cueSeq))
			m.cueSeq++
		}
	} else {
		m.resistAnnounced = ""
	}

	// a long cooldown coming back up (Mend, Lay Hands, Harm Touch, disciplines).
	// Only the long ones — short skill reuses (kick, feign) would be chatter.
	for _, cd := range m.tracker.Cooldowns(now) {
		if cd.Total < longCooldownSec {
			continue
		}
		ready := cd.Remaining <= 0
		if was, seen := m.cdReady[cd.Name]; seen && !was && ready {
			m.speaker.Say(cd.Name + " ready.")
			m.cueSeq++
		}
		m.cdReady[cd.Name] = ready // first sight just records; only false→true speaks
	}

	// gentle buff/debuff fades, combined into one varied utterance.
	if due := m.dueAnnouncements(m.tracker.Active(now), now); len(due) > 0 {
		m.speaker.Say(composeCue(due, m.cueSeq))
		m.cueSeq++
	}
}

// longCooldownSec gates which cooldowns get a "ready" cue — Mend (360s) and up,
// excluding short skill reuses like feign (11s) or monk specials (5s).
const longCooldownSec = 60

// dueAnnouncements returns the low-buff phrases to speak this tick and updates
// the announced set: each (non-charm) timer fires once when it first drops below
// its warning lead, re-arming when it's refreshed or gone. Speaking is left to
// the caller so this stays testable. (Charm breaks before its cap, so a countdown
// "low" would cry wolf — it's skipped.)
func (m *Model) dueAnnouncements(active []gamestate.Timer, now int64) []gamestate.Timer {
	var due []gamestate.Timer
	live := make(map[string]bool, len(active))
	for _, tm := range active {
		if tm.Charm {
			continue
		}
		k := tm.Spell + "\x00" + tm.Target
		live[k] = true
		if tm.Expiry-now <= warnLeadSec(tm.Expiry-tm.Start) {
			if !m.announced[k] {
				m.announced[k] = true
				due = append(due, tm)
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
	return due
}

// warnLeadSec is how many seconds before expiry the "low" cue fires, scaled to a
// timer's total length: ~10% of the duration, clamped to [15s, 180s]. So a short
// debuff still warns ~15s out, while a ~100-min Enchanter buff warns a full 3 min
// early — enough time to recast — instead of a useless 15s.
func warnLeadSec(total int64) int64 {
	lead := total / 10
	if lead < 15 {
		lead = 15
	}
	if lead > 180 {
		lead = 180
	}
	return lead
}

// composeCue turns the timers that just went low into one terse, natural
// utterance, so simultaneous fades are spoken as a single sentence rather than
// several cues overlapping. seq rotates the phrasing for variety. Self-buffs read
// by name; effects on a mob read as "<spell> on <mob>".
func composeCue(due []gamestate.Timer, seq int) string {
	subjects := make([]string, 0, len(due))
	for _, tm := range due {
		if tm.Target == "" || tm.Target == "You" {
			subjects = append(subjects, tm.Spell)
		} else {
			subjects = append(subjects, tm.Spell+" on "+tm.Target)
		}
	}
	if len(subjects) == 0 {
		return ""
	}
	style := fadeStyles[seq%len(fadeStyles)]
	if len(subjects) == 1 {
		return fmt.Sprintf(style.one, joinList(subjects))
	}
	return fmt.Sprintf(style.many, joinList(subjects))
}

// fadeStyles are the rotating phrasings for a buff/debuff fading (singular vs
// plural form so verb agreement stays correct however many are listed).
var fadeStyles = []struct{ one, many string }{
	{"%s is fading.", "%s are fading."},
	{"%s is wearing off.", "%s are wearing off."},
	{"Heads up, %s is about to drop.", "Heads up, %s are about to drop."},
}

// joinList renders names with natural list grammar: "X" / "X and Y" /
// "X, Y, and Z".
func joinList(s []string) string {
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s[0]
	case 2:
		return s[0] + " and " + s[1]
	default:
		return strings.Join(s[:len(s)-1], ", ") + ", and " + s[len(s)-1]
	}
}

// charmBreakPhrase is an urgent, varied alert that the player's charm broke.
func charmBreakPhrase(seq int) string {
	phrases := []string{"Charm broke!", "Your charm broke — careful!", "Charm just broke!"}
	return phrases[seq%len(phrases)]
}

// feignFailPhrase is an urgent, varied alert that a feign death failed.
func feignFailPhrase(seq int) string {
	phrases := []string{"Feign failed!", "Your feign failed — get up!", "Feign Death failed!"}
	return phrases[seq%len(phrases)]
}

// resistPhrase is an urgent, varied alert that a cast was resisted.
func resistPhrase(spell string, seq int) string {
	switch seq % 3 {
	case 0:
		return spell + " resisted!"
	case 1:
		return spell + " was resisted."
	default:
		return "Resisted: " + spell + "."
	}
}

// rebuildInteractive re-renders the hover-aware panels (class + enchanter CC)
// and refreshes their line→target maps. Cheap enough to call on every mouse
// move so the highlight tracks the cursor without a full snapshot pass.
func (m *Model) rebuildInteractive(cur *session.CombatSession) {
	th := themes[m.theme]
	enemy := m.layout().enemy
	classStr, classT := m.classPanel(cur, m.vpClass.Width, m.hover, enemy)
	m.vpClass.SetContent(classStr)
	m.classTargets = classT
	if enemy {
		// the middle column holds self-BUFFS for melee, or CC + DEBUFFS (what you
		// cast on mobs) for casters/hybrids — same timerColumn, different sections.
		var midStr string
		var midT map[int]string
		if m.middleIsBuffs() {
			midStr, midT = timerColumn(th, m.tracker, m.vpEnemy.Width, m.hover, false, false, true)
		} else {
			midStr, midT = timerColumn(th, m.tracker, m.vpEnemy.Width, m.hover, true, true, false)
		}
		m.vpEnemy.SetContent(midStr)
		m.enemyTargets = midT
	}
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
		case "1":
			return m.gotoScreen(screenMeter), nil
		case "2":
			return m.gotoScreen(screenSessions), nil
		case "3":
			return m.gotoScreen(screenSettings), nil
		}
		// each tab owns the rest of its keys.
		if m.screen == screenSettings {
			return m.updateSettings(msg)
		}
		if m.screen == screenSessions {
			return m.updateSessions(msg)
		}
		switch msg.String() {
		case "t":
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
		// a left-click on the tab bar switches screens (on any screen).
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if scr, ok := m.tabAt(msg.X, msg.Y); ok {
				return m.gotoScreen(scr), nil
			}
		}
		if m.screen == screenSessions {
			return m.mouseSessions(msg)
		}
		if m.screen != screenMeter {
			return m, tea.Batch(cmds...) // the Settings tab has no mouse targets
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
	case switchMsg:
		m.character = msg.character
		m.selected, m.follow, m.hover = 0, true, ""
		m.editing, m.editMob = false, ""
		m.flash("▶ now tracking " + msg.character)
		m.refresh()
	case tickMsg:
		switch m.screen {
		case screenMeter:
			m.refresh()
		case screenSessions:
			m.refreshSessions()
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
	set(&m.vpMob, ld.mobW, ld.botH)
	set(&m.vpEnemy, ld.enemyW, ld.botH)
	// the extras card has a title ("Offense · Defense"), so reserve its row (h-3,
	// like cardInner); clamp to 0 when there's no side column (narrow terminal).
	if ew, eh := max(ld.extrasW-4, 0), max(ld.dmgH-3, 0); !m.ready {
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
	gridY := gridTop        // outer pad (1) + banner (1)
	rightX := gridX         // meter spans full width from the content left edge
	botY := gridY + ld.dmgH // bottom row starts after the Damage card
	in := func(cx, cw, cy, ch int) bool { return x >= cx && x < cx+cw && y >= cy && y < cy+ch }

	switch {
	case in(rightX, ld.dmgW, gridY, ld.dmgH):
		return &m.vpDamage
	case in(rightX+ld.dmgW+1, ld.extrasW, gridY, ld.dmgH):
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
	if y < contentTop || y >= botY+ld.botH-1 {
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

// toggleTTS flips audio cues at runtime, flashing feedback (no-op without an
// engine).
func (m *Model) toggleTTS() {
	if m.speaker == nil || !m.speaker.Available() {
		m.flash("no voice yet — run: 99dps -tts-setup")
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

// scrollHint returns a tiny arrow for a card title when its viewport has content
// off-screen: ▾ (more below), ▴ (more above), ↕ (both), "" (it all fits).
func scrollHint(vp viewport.Model) string {
	up, down := !vp.AtTop(), !vp.AtBottom()
	switch {
	case up && down:
		return " ↕"
	case down:
		return " ▾"
	case up:
		return " ▴"
	}
	return ""
}

func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	th := themes[m.theme]

	innerW := m.w - 2 // content width inside the outer Padding(1,1)

	// first-run audio-cue setup takes over the whole window.
	if m.screen == screenSetup {
		body := lipgloss.JoinVertical(lipgloss.Left, m.banner(th, innerW), "", m.setupView(th, innerW))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(body)
	}

	banner := m.banner(th, innerW)
	tabbar := m.tabBar(th)

	// the Sessions tab: a wide scrollable stats table beside a DPS breakdown of
	// the highlighted session.
	if m.screen == screenSessions {
		tableW, breakW, fullH := m.sessionsLayout()
		breakTitle := "Damage"
		if sel := m.effectiveSel(); sel >= 0 && sel < len(m.sessions) {
			breakTitle = "Damage — " + truncate(m.sessions[sel].Name(), breakW-12)
		}
		// sticky header sits above the scrolling body inside the table card.
		tableBody := lipgloss.JoinVertical(lipgloss.Left, m.sessHeader, m.vpSessTable.View())
		grid := lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, tableW, fullH, "Sessions"+scrollHint(m.vpSessTable), tableBody), " ",
			card(th, breakW, fullH, breakTitle+scrollHint(m.vpSessBreak), m.vpSessBreak.View()))
		full := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, grid, m.footer(th, innerW))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(full)
	}

	// the Settings tab renders its own body under the banner + tab bar.
	if m.screen == screenSettings {
		body := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, m.settingsView(th, innerW))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(body)
	}

	ld := m.layout()

	dmgTitle := "Damage"
	if sel := m.effectiveSel(); sel >= 0 {
		dmgTitle = "Damage — " + truncate(m.sessions[sel].Name(), ld.rightW-12)
	}

	// bottom row: the class-aware panel + Mob Tracker — caster/hybrid classes get a
	// third, dedicated Enemy column (CC + debuffs on mobs). Every panel renders from
	// its own viewport, so each scrolls independently — a scroll hint (▾/▴/↕) in the
	// title signals when there's more off-screen.
	classTitle := classPanelTitle(m.tracker, ld.enemy) + scrollHint(m.vpClass)
	midTitle := "Enemy" // CC + debuffs on mobs (caster/hybrid)
	if m.middleIsBuffs() {
		midTitle = "Buffs" // self-buff / clicky timers (melee)
	}
	var bottom string
	if ld.enemy {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, ld.classW, ld.botH, classTitle, m.vpClass.View()), " ",
			card(th, ld.enemyW, ld.botH, midTitle+scrollHint(m.vpEnemy), m.vpEnemy.View()), " ",
			card(th, ld.mobW, ld.botH, "Mob Tracker"+scrollHint(m.vpMob), m.vpMob.View()))
	} else {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, ld.classW, ld.botH, classTitle, m.vpClass.View()), " ",
			card(th, ld.mobW, ld.botH, "Mob Tracker"+scrollHint(m.vpMob), m.vpMob.View()))
	}
	// top-right: the dealer meter beside its Specials / Avoidance side column.
	// The side card has no title — its SPECIALS / AVOIDANCE section headers label
	// it. On a narrow terminal there's no side column; the meter takes the width.
	// the canni dance meter is pinned beneath the scrollable dealer list (its
	// height was reserved out of the viewport in refresh), so it's always visible.
	dmgBody := m.vpDamage.View()
	if m.canniBlock != "" {
		dmgBody = lipgloss.JoinVertical(lipgloss.Left, dmgBody, m.canniBlock)
	}
	topRight := card(th, ld.dmgW, ld.dmgH, dmgTitle+scrollHint(m.vpDamage), dmgBody)
	if ld.extrasW > 0 {
		topRight = lipgloss.JoinHorizontal(lipgloss.Top,
			topRight, " ", card(th, ld.extrasW, ld.dmgH, "Offense · Defense"+scrollHint(m.vpExtras), m.vpExtras.View()))
	}
	grid := lipgloss.JoinVertical(lipgloss.Left, topRight, bottom)
	full := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, grid, m.footer(th, innerW))
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
	keys := "[tab] screen  [t] theme  [a] " + audio + "  [↑↓/click] fight  [end] live  [wheel] scroll  [bksp] clear  [q] quit"
	if m.spellInfo != "" {
		keys = m.spellInfo + "  ·  " + keys
	}
	return th.fg(th.dim).Render(truncate(keys, w))
}

// damageContent is the scrollable damage breakdown for the selected fight: an
// encounter summary, a ranked per-dealer table (share bar + DPS/Total/% and
// width-gated Hit%/Crit%), the unattributed spell line, then the Specials and
// Avoidance sub-tables — matching the old Damage panel.
func (m Model) damageContent(cur *session.CombatSession, live bool, width int) string {
	th := themes[m.theme]
	if cur == nil {
		return th.fg(th.dim).Render("No fight selected.\nFight something!")
	}
	stats := cur.GetAggressors() // ranked below, after spell/pet damage is rolled in
	magic := cur.MagicTotal()
	encTotal := cur.Total() + magic // melee + unattributed spell damage
	span := cur.LastUnix() - cur.StartTime().Unix()
	if span < 1 {
		span = 1
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
		// all numeric columns in the row's normal color (children pass col=dim).
		s += rightCell(humanize(total), totW, col) + " " + rightCell(humanize(dps), dpsW, col)
		if showHit {
			s += " " + rightCell(hit, 5, col)
		}
		if showCrit {
			s += " " + rightCell(crit, 5, col)
		}
		return s
	}

	var b strings.Builder
	b.WriteString(summary + "\n")
	b.WriteString(strings.Repeat(" ", max(width-num, 0)) +
		numBlockLabels(th, showPct, showHit, showCrit, pctW, totW, dpsW) + "\n")

	// from=="rainbow" → a horizontal red→violet rainbow across the bar (every
	// ranked meter shares the spectrum); otherwise a solid from→to gradient (the
	// dim breakdown children).
	row := func(rankStr string, nameCell, hit, crit string, frac float64, from, to, col string, total, dps, pctv int) string {
		mid := ""
		if showBar {
			if from == "rainbow" {
				mid = rainbowBarH(frac, barCells, th.track) + " "
			} else {
				mid = gradientBar(frac, barCells, from, to, th.track) + " "
			}
		}
		return rightCell(rankStr, rankW, th.dim) + " " + nameCell + " " + mid +
			numBlock(pctv, total, dps, hit, crit, col)
	}

	// attribute each pet under its OWNER (from the pet→owner map): a pet whose
	// owner is also a dealer here is pulled out of the ranking and nested as a
	// "↳ <pet>" child of that owner — your own pet under You, a group-mate's pet
	// under them. A pet whose owner dealt no damage stays ranked, tagged "(<owner>'s
	// pet)" so it's never silently lumped under you.
	present := map[string]bool{}
	for _, d := range stats {
		present[strings.ToLower(d.Dealer)] = true
	}
	hasYou := present["you"]

	// EQ shows a client only its OWN non-melee, so all spell/proc/DoT damage is the
	// player's. A spell caster may never melee a mob, but once they cast on it they
	// should appear on the chart: fold the spell damage into the "You" row —
	// synthesizing one if the player never meleed — so the damage is attributed to
	// them, their pet links under them, and (a damage-shield-only or pet-only
	// caster) still shows up. Owning a fighting pet alone also puts them on the board.
	playerOwnsPet := false
	if m.tracker != nil {
		for _, d := range stats {
			if o := m.tracker.PetOwner(d.Dealer); o != "" && strings.EqualFold(o, m.character) {
				playerOwnsPet = true
				break
			}
		}
	}
	if !hasYou && (magic > 0 || playerOwnsPet) {
		stats = append(stats, combat.DamageStat{Dealer: "You"})
		present["you"] = true // rollup nests the player's pet under the synthesized You row
	}
	if magic > 0 { // the player's row includes their own spell/proc/DoT damage
		for i := range stats {
			if strings.EqualFold(stats[i].Dealer, "You") {
				stats[i].Total += magic
				break
			}
		}
	}

	ownerRow := func(dealer string) string { // the dealer a pet nests under, or ""
		o := m.tracker.PetOwner(dealer)
		if o == "" {
			return ""
		}
		if strings.EqualFold(o, m.character) {
			return "You"
		}
		return o
	}
	children := map[string][]combat.DamageStat{} // owner (lower) → its pets
	kept := stats[:0]
	for _, d := range stats {
		if owner := ownerRow(d.Dealer); owner != "" && present[strings.ToLower(owner)] {
			children[strings.ToLower(owner)] = append(children[strings.ToLower(owner)], d)
			continue
		}
		kept = append(kept, d)
	}
	stats = kept

	// a pet's damage rolls UP into its owner's row, so that row is inclusive of
	// everything the owner put out (the player's: melee + spells + pet). The pet
	// stays visible as a dim "↳ <pet>" breakdown child below; without the rollup
	// the owner's Total/DPS/share silently dropped the pet's contribution.
	for ownerLower, pets := range children {
		sum := 0
		for _, p := range pets {
			sum += p.Total
		}
		for i := range stats {
			if strings.ToLower(stats[i].Dealer) == ownerLower {
				stats[i].Total += sum
				break
			}
		}
	}

	// rank and scale the bars by the now-inclusive row totals
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	maxTotal := 1
	for _, d := range stats {
		if d.Total > maxTotal {
			maxTotal = d.Total
		}
	}

	// acc returns the hit%/crit% cells for a dealer (its own DamageStat for crits).
	acc := func(d combat.DamageStat) (hit, crit string) {
		hit, crit = "-", "-"
		if hr := cur.OffenseFor(d.Dealer).HitRate(); hr >= 0 {
			hit = fmt.Sprintf("%d%%", hr)
		}
		if c := cur.CritFor(d.Dealer); c.Count > 0 && d.Hits > 0 {
			crit = fmt.Sprintf("%d%%", critPct(c.Count, d.Hits))
		}
		return hit, crit
	}
	// child renders a dim, indented "↳ <label>" breakdown row.
	childRow := func(label string, total int, hit, crit string) string {
		return row("", th.fg(th.dim).Width(nameW).Render(truncate(label, nameW)),
			hit, crit, float64(total)/float64(maxTotal), th.dim, th.dim, th.dim,
			total, total/int(span), pct(total, encTotal))
	}

	for i, d := range stats {
		nameStyle, col := th.fg(th.text), th.text
		you := strings.EqualFold(d.Dealer, "you")
		if you {
			nameStyle = nameStyle.Bold(true).Foreground(lipgloss.Color(th.accent))
		}
		name := d.Dealer
		if o := m.tracker.PetOwner(d.Dealer); o != "" { // orphan pet — credit its owner
			if strings.EqualFold(o, m.character) {
				name = "your pet"
			} else {
				name = o + "'s pet"
			}
		}
		hit, crit := acc(d)
		b.WriteString(row(fmt.Sprintf("%d", i+1), nameStyle.Width(nameW).Render(truncate(name, nameW)),
			hit, crit, float64(d.Total)/float64(maxTotal), "rainbow", "", col, d.Total, d.Total/int(span), pct(d.Total, encTotal)) + "\n")

		// your own unattributed spell/proc/DoT damage, under You (EQ shows you only
		// your own non-melee, so it's almost always yours)
		if you && magic > 0 {
			b.WriteString(childRow("↳ spells", magic, "-", "-") + "\n")
		}
		// this dealer's pets, nested as dim children
		for _, p := range children[strings.ToLower(d.Dealer)] {
			phit, pcrit := acc(p)
			b.WriteString(childRow("↳ "+p.Dealer, p.Total, phit, pcrit) + "\n")
		}
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

	prefs := tts.LoadPrefs()
	m.screen = initialScreen(prefs)
	if m.screen == screenSetup {
		m.setup = newSetupState()
	} else if prefs.Voice != "" {
		m.speaker.SetVoice(prefs.Voice)
	}
	// prefs drive cues once configured; the -tts flag is a manual force-on.
	m.ttsOn = (prefs.Enabled || ttsOn) && m.speaker.Available()

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
