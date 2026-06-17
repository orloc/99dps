// Package tui is the Bubble Tea + Lipgloss UI for 99dps — the only UI (it
// replaced the legacy gocui one). A single Model renders the banner, tab bar, Damage meter + Specials/Avoidance, the class-aware panel, and the Mob
// Tracker from lock-free session/tracker snapshots, themed and truecolor.
package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/cues"
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
	vpInv    viewport.Model // Tools tab: scrollable inventory price list

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

	// TTS audio cues for buff/debuff timers. The cue engine (internal/cues) holds
	// the across-tick dedup state + phrasing; the model owns the speaker, the
	// on/off flag, and the persisted prefs the engine reads each tick.
	speaker   tts.Engine
	ttsOn     bool
	cueEngine *cues.Engine // decides + speaks this tick's cues (see internal/cues)
	cuePrefs  cues.Prefs   // which cues fire (per-skill / per-category toggles)
	store     store        // consolidated per-character settings (settings.json)

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

	// Tools-tab inventory widget: the EQ folder (where /outputfile writes), a
	// price-lookup source (injected; nil-safe), and the last indexed result.
	eqDir     string
	pricer    PriceSource
	invRows   []invRow
	invStatus string

	// Map tab: a map source (fetch+cache zone maps), the currently-shown map, and
	// the zoom level (centered on the player when a /loc is known).
	mapSrc    MapSource
	curMap    *MapData
	mapShort  string // short name of the loaded/requested zone (dedup fetches)
	mapStatus string
	mapZoom   float64
	mapFloors bool // floor-scope (hide other levels) — off by default; on for stacked zones

	// screen selects the active view (meter / settings tab / first-run setup).
	screen      screen
	setup       setupState
	settingsSel int         // selected row in the Settings left column
	settingsCol int         // focused Settings column: 0 = left, 1 = cue matrix
	cueSel      int         // selected toggle in the Settings cue matrix (right column)
	layoutPrefs layoutPrefs // per-box meter visibility (Damage / Offense·Defense)

	w, h  int
	ready bool
}

// NewModel builds the model over a live session manager + (optional) tracker.
// The network-bearing map/price sources are injected later (via NewProgram); a
// nil source just means that widget shows no data, so tests can skip them.
func NewModel(sm *session.SessionManager, tracker *gamestate.Tracker, character string) Model {
	return Model{
		sm: sm, tracker: tracker, character: character, follow: true,
		speaker: tts.NewEngine(), cueEngine: cues.NewEngine(),
		mapZoom: 4,
	}
}

// announceCues speaks this tick's audio cues via the cue engine, reading live
// state from the tracker and gating on the player's cue prefs. No-op when audio
// is off.
func (m *Model) announceCues() {
	if !m.ttsOn {
		return
	}
	m.cueEngine.Announce(m.speaker, m.tracker, m.cuePrefs, time.Now().Unix())
}

func (m Model) Init() tea.Cmd { return tick() }

// flash shows a transient status message in the footer for a few seconds.
func (m *Model) flash(msg string) { m.status, m.statusAt = msg, time.Now().Unix() }

// statusGraceSec is how long a flashed status stays visible.
const statusGraceSec = 5

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
	if m.tracker != nil { // close a roll contest that's gone quiet (winner → log)
		m.tracker.FinalizeStaleRolls(time.Now().Unix())
	}
	m.announceCues()
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
