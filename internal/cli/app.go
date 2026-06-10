package cli

import (
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jroimartin/gocui"
)

// linesPerCard is how many terminal rows each session occupies in the side
// panel (name, meta, top-dealer, blank). Click-to-select divides by this to map
// a clicked row back to a session.
const linesPerCard = 4

type App struct {
	gui     *gocui.Gui
	manager *session.SessionManager
	tracker *gamestate.Tracker

	// set on shutdown so a late repaint tick (Sync goroutine) doesn't enqueue a
	// gui.Update once the main loop has stopped draining its event channel.
	shuttingDown atomic.Bool

	// selection + scroll state for the session side panel, guarded by mu.
	// selected is the pinned session index; follow keeps it glued to the newest
	// (live) session. scrollY is the panel's viewport top line (origin); lastSel
	// records the selection at the previous render so we only auto-scroll the
	// selection into view when it actually changes — leaving wheel scrolling be.
	// character is the tracked log owner (updates on an auto-detected switch);
	// status/statusTicks drive a transient banner in the shortcuts bar.
	mu          sync.Mutex
	selected    int
	follow      bool
	scrollY     int
	lastSel     int
	character   string
	status      string
	statusTicks int

	// source info for the bottom-bar stats line; set once at startup.
	logDir    string
	spellInfo string

	// mouse-wheel scroll offset for the (potentially long) spell-timer panel.
	timerScrollY int

	// text-to-speech cues for low buffs. announced tracks which timers have
	// already been spoken (keyed spell\x00target) so each fires once, re-arming
	// when the buff is refreshed or expires.
	speaker   *speaker
	ttsOn     bool
	announced map[string]bool

	// repop editing: clicking a Repops row selects that mob (repopSel) and opens
	// an inline editor (editing/editBuf) to type a corrected respawn, which is
	// saved as a per-(zone,mob) override. repopLineMobs maps a panel line to the
	// mob on it, for click resolution.
	repopSel      string
	editing       bool
	editBuf       string
	repopLineMobs map[int]string
	repopScrollY  int

	// timerLineTargets / ccLineTargets map a panel line to its target, so a click
	// dismisses that person's timers (Spell Timers panel and the enchanter CC
	// column respectively).
	timerLineTargets map[int]string
	ccLineTargets    map[int]string
}

// lowBuffSec is the remaining-time threshold below which a buff triggers an
// audio cue.
const lowBuffSec = 15

// SetSources records the log directory and a spell-data summary for the
// bottom-bar stats line. Call once before the Sync goroutine starts.
func (a *App) SetSources(logDir, spellInfo string) {
	a.logDir = logDir
	a.spellInfo = spellInfo
}

// scrollStep is how many lines one mouse-wheel notch moves the session list.
const scrollStep = 3

func New(m *session.SessionManager, character string, tracker *gamestate.Tracker) *App {
	a := &App{
		manager:   m,
		tracker:   tracker,
		character: character,
		follow:    true,
		speaker:   newSpeaker(),
		announced: map[string]bool{},
	}

	var err error
	a.gui, err = gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		log.Fatal(err) // can't create the terminal UI — fatal at startup
	}

	a.initGui()

	return a
}

func (a *App) Loop() {
	if err := a.gui.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Fatal(err)
	}
}

// Sync repaints every panel once per second until stop is closed.
func (a *App) Sync(stop <-chan struct{}) {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.refresh()
		}
	}
}

// BeginShutdown marks the app as shutting down so any in-flight repaint tick
// returns without enqueuing a gui.Update. Call it before signalling the Sync
// goroutine to stop.
func (a *App) BeginShutdown() {
	a.shuttingDown.Store(true)
}

// Close tears down the gui, restoring the terminal. Call it after Sync has
// stopped so no repaint races the teardown.
func (a *App) Close() {
	a.gui.Close()
}

// refresh snapshots all sessions once, resolves which one is selected, and
// repaints every panel from that single consistent view of the data.
func (a *App) refresh() {
	if a.shuttingDown.Load() {
		return // main loop has stopped; don't enqueue a doomed gui.Update
	}
	all := a.manager.All()
	sel := a.resolveSelection(len(all))

	var cur *session.CombatSession
	if sel >= 0 {
		cur = all[sel]
	}

	// the live session is the newest one that hasn't been closed (a death/zone
	// can close the last session before a new fight has started)
	live := sel >= 0 && sel == len(all)-1 && all[sel].EndTime().IsZero()

	// Repaint on the gocui main loop. The update* methods read view geometry
	// (a.gui.Size()/View().Size()) to size the renderers, and the main loop
	// concurrently rewrites that geometry on a terminal resize — doing the reads
	// here, inside g.Update, keeps them on the main goroutine and race-free.
	a.gui.Update(func(g *gocui.Gui) error {
		a.updateStatus()
		a.updateSessions(all, sel)
		a.updateDamage(cur, live)
		a.updateGraph(cur)
		a.updatePanel(cur)
		a.updateRepops()
		a.updateCC()
		a.updateShortcuts()
		return nil
	})
}

// updatePanel repaints the bottom-right panel: a stack of independently-gated
// indicator sections (canni / feign / bind / cooldowns) above a category-driven
// body — casters get spell timers, pure melee the skills breakdown, hybrids
// spell timers plus a one-line skills digest. Until a /who reveals the class the
// body defaults to spell timers (CatCaster).
func (a *App) updatePanel(cur *session.CombatSession) {
	width := a.viewInnerWidth(viewTimers)
	now := time.Now().Unix()

	cat := eqclass.CatCaster
	var class eqclass.Class
	var level int
	if a.tracker != nil {
		cat = a.tracker.Category()
		class = a.tracker.Class()
		level = a.tracker.Level()
	}

	body, timerMap := a.panelBody(cur, cat, class, level, width)
	str, timerMap := stackPanel(a.panelSections(width, now), body, timerMap)

	a.mu.Lock()
	a.timerLineTargets = timerMap
	a.mu.Unlock()

	if str == "" {
		return // nothing to show (no spell data, no class, not dancing)
	}

	a.mu.Lock()
	a.timerScrollY = clampScroll(a.timerScrollY, strings.Count(str, "\n"), a.viewInnerHeight(viewTimers))
	sy := a.timerScrollY
	a.mu.Unlock()

	a.writeView(viewTimers, str)
	if v, err := a.gui.View(viewTimers); err == nil {
		v.SetOrigin(0, sy)
	}
}

// panelBody is the category-driven main content of the bottom-right panel, plus
// the spell-timer click-to-dismiss line map (nil for the skills view).
func (a *App) panelBody(cur *session.CombatSession, cat eqclass.Category, class eqclass.Class, level, width int) (string, map[int]string) {
	switch cat {
	case eqclass.CatMelee:
		return renderSkills(cur, class, level, width), nil
	case eqclass.CatHybrid:
		body, timerMap := a.timersStr(width)
		if sum := skillsSummary(cur, class, level); sum != "" {
			body += "\n" + sectionHeader("skills", width) + "  " + sum
		}
		return body, timerMap
	default: // CatCaster
		if a.tracker == nil {
			return "", nil
		}
		return a.timersStr(width)
	}
}

// panelSections are the indicator blocks stacked above the panel body, top-down.
// Each appears only when its own live state warrants it — independent of class
// or category — so e.g. a bind-wound bar shows for any class, not just melee.
func (a *App) panelSections(width int, now int64) []string {
	if a.tracker == nil {
		return nil
	}
	sections := []string{renderCanni(a.tracker.CanniStats(now), width)}

	switch a.tracker.FeignStatus(now) {
	case gamestate.FeignFailed:
		sections = append(sections, headerBar("⚠ FEIGN FAILED — mobs still on you", "41;1;37", width))
	case gamestate.FeignOK:
		sections = append(sections, headerBar("✓ feigned", "42;1;30", width))
	}

	if rem, ok := a.tracker.BindRemaining(now); ok {
		sections = append(sections, headerBar(fmt.Sprintf("⏳ bandaging… %s", fmtDuration(time.Duration(rem)*time.Second)), "43;30", width))
	}

	return append(sections, renderCooldowns(a.tracker.Cooldowns(now), width))
}

// stackPanel joins the non-empty sections above body (top-to-bottom) and shifts
// body's line-keyed click map down by the number of lines the sections occupy,
// so click resolution stays aligned. Trailing newlines are normalized away so
// each block contributes an exact line count. Returns ("", nil) when empty.
func stackPanel(sections []string, body string, bodyMap map[int]string) (string, map[int]string) {
	blocks := make([]string, 0, len(sections)+1)
	prefixLines := 0
	for _, s := range sections {
		if s = strings.TrimRight(s, "\n"); s == "" {
			continue
		}
		blocks = append(blocks, s)
		prefixLines += strings.Count(s, "\n") + 1
	}

	if body = strings.TrimRight(body, "\n"); body != "" {
		if prefixLines > 0 && len(bodyMap) > 0 {
			shifted := make(map[int]string, len(bodyMap))
			for k, v := range bodyMap {
				shifted[k+prefixLines] = v
			}
			bodyMap = shifted
		}
		blocks = append(blocks, body)
	} else {
		bodyMap = nil
	}

	return strings.Join(blocks, "\n"), bodyMap
}

// updateRepops repaints the dedicated Mob Tracker panel: the zone-aware repop
// list with the clicked mob marked. It records which line each mob sits on so a
// click can select it for editing.
func (a *App) updateRepops() {
	if a.tracker == nil {
		return
	}
	now := time.Now().Unix()
	width := a.viewInnerWidth(viewRepops)
	respawns := a.tracker.Respawns(now)

	a.mu.Lock()
	sel := a.repopSel
	a.mu.Unlock()

	str := renderRespawns(respawns, sel, width)
	if str == "" {
		str = "No kills tracked yet."
	}

	// map each rendered line to its mob for click selection. renderRespawns puts
	// the player's kills first, then (if there are also others' kills) one
	// separator line, then the rest — so non-mine rows shift down by one.
	mineCount := 0
	for _, r := range respawns {
		if r.Mine {
			mineCount++
		}
	}
	hasSep := mineCount > 0 && mineCount < len(respawns)
	lineMobs := make(map[int]string, len(respawns))
	for j, r := range respawns {
		line := j
		if hasSep && j >= mineCount {
			line++ // skip the separator row
		}
		lineMobs[line] = r.Mob
	}

	total := len(respawns)
	if hasSep {
		total++
	}
	a.mu.Lock()
	a.repopLineMobs = lineMobs
	a.repopScrollY = clampScroll(a.repopScrollY, total, a.viewInnerHeight(viewRepops))
	sy := a.repopScrollY
	a.mu.Unlock()

	a.writeView(viewRepops, str)
	if v, err := a.gui.View(viewRepops); err == nil {
		v.SetOrigin(0, sy)
	}
}

// timersStr renders the active spell timers and fires any due audio cues. "now"
// is wall-clock — log timestamps track real time during live play, and timers
// replayed from old log history are already expired and filtered out. Only
// called when the tracker is non-nil.
func (a *App) timersStr(width int) (string, map[int]string) {
	now := time.Now().Unix()
	active := a.tracker.Active(now)
	a.announceLowBuffs(active, now)
	// when the enchanter CC column is shown, mez/charm live there, not inline
	return renderTimers(active, now, width, !a.enchanterLayout())
}

// updateCC repaints the enchanter's dedicated Crowd Control column (mez + charm).
// No-op when that column isn't shown (non-enchanter / view not laid out).
func (a *App) updateCC() {
	if a.tracker == nil || !a.enchanterLayout() {
		return
	}
	now := time.Now().Unix()
	width := a.viewInnerWidth(viewCC)
	cc, _ := splitCC(a.tracker.Active(now))
	str, lineTargets := renderCC(cc, now, width)
	if str == "" {
		str = "No crowd control."
	}
	a.mu.Lock()
	a.ccLineTargets = lineTargets
	a.mu.Unlock()

	if _, err := a.gui.View(viewCC); err != nil {
		return // not laid out yet this frame
	}
	a.writeView(viewCC, str)
}

// announceLowBuffs speaks a cue when a (non-charm) timer first drops below the
// low threshold, once per timer, re-arming when it's refreshed or expires.
func (a *App) announceLowBuffs(active []gamestate.Timer, now int64) {
	for _, p := range a.dueAnnouncements(active, now) {
		a.speaker.say(p)
	}
}

// dueAnnouncements returns the phrases to speak this tick and updates the
// announced set (each timer fires once; re-arms when refreshed or gone). The
// speaking itself is left to the caller so this stays unit-testable.
func (a *App) dueAnnouncements(active []gamestate.Timer, now int64) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.ttsOn {
		return nil
	}

	var phrases []string
	live := make(map[string]bool, len(active))
	for _, tm := range active {
		if tm.Charm {
			continue // charm breaks before its cap — a countdown "low" would cry wolf
		}
		k := tm.Spell + "\x00" + tm.Target
		live[k] = true
		if tm.Expiry-now <= lowBuffSec {
			if !a.announced[k] {
				a.announced[k] = true
				phrases = append(phrases, lowBuffPhrase(tm))
			}
		} else {
			delete(a.announced, k) // refreshed / still healthy → re-arm
		}
	}
	for k := range a.announced {
		if !live[k] {
			delete(a.announced, k) // timer gone → re-arm for next cast
		}
	}
	return phrases
}

func lowBuffPhrase(tm gamestate.Timer) string {
	if tm.Target == "You" {
		return tm.Spell + " low"
	}
	return tm.Target + ", " + tm.Spell + " low"
}

// SetTTS sets the initial audio-cue state (no-op if no TTS engine is present).
func (a *App) SetTTS(on bool) {
	a.mu.Lock()
	a.ttsOn = on && a.speaker.available()
	a.mu.Unlock()
}

// flashStatus shows a transient banner in the bottom bar.
func (a *App) flashStatus(msg string) {
	a.mu.Lock()
	a.status = msg
	a.statusTicks = 6
	a.mu.Unlock()
}

// SetCharacter switches the tracked character after an auto-detected log swap:
// it resets the panel selection/scroll, updates the title (via the next
// Layout), and flashes a transient banner. The caller clears the session
// manager separately.
func (a *App) SetCharacter(name string) {
	a.mu.Lock()
	a.character = name
	a.selected = 0
	a.follow = true
	a.scrollY = 0
	a.lastSel = 0
	a.status = "▶ now tracking " + name + " (auto-detected)"
	a.statusTicks = 6
	a.mu.Unlock()
	a.refresh()
}

// characterLabel reads the tracked character under the lock.
func (a *App) characterLabel() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.character
}

// updateShortcuts repaints the bottom bar — the keybinding help, with a
// transient status banner on top that counts down over a few refreshes.
func (a *App) updateShortcuts() {
	a.mu.Lock()
	status := a.status
	if a.statusTicks > 0 {
		a.statusTicks--
		if a.statusTicks == 0 {
			a.status = ""
		}
	}
	// the repop editor takes over the banner while active
	if a.editing {
		status = fmt.Sprintf("set timer for '%s' (m:ss): %s_    [Enter] save  [Esc] cancel", a.repopSel, a.editBuf)
	}
	audio := "audio off"
	if a.ttsOn {
		audio = "♪ audio on"
	} else if !a.speaker.available() {
		audio = "audio n/a"
	}
	a.mu.Unlock()

	// character/zone/kills now live in the top-left "Now" box; the bar just keeps
	// the data-source note, audio state, and key help.
	stats := fmt.Sprintf("%s  ·  %s", a.spellInfo, audio)
	// stats first so it survives if the (thin) bar clips; keybindings below it
	text := stats + "\n" + keyBindingsText
	if status != "" {
		text = "\x1b[1m" + status + "\x1b[0m\n" + text
	}
	a.writeView(viewShortcuts, text)
}

// resolveSelection clamps the pinned selection to the available sessions and,
// while following, snaps it to the newest one. Returns -1 when there are none.
func (a *App) initGui() {
	// default config
	a.gui.Cursor = false
	a.gui.InputEsc = true
	a.gui.Mouse = true
	a.gui.BgColor = gocui.ColorDefault
	// gold window frames (EQ-flavored); per-view text is forced back to white in
	// createView so only the borders are gilded. SelFgColor keeps the focused
	// panel's frame gold too.
	a.gui.FgColor = gocui.ColorYellow
	a.gui.SelFgColor = gocui.ColorYellow

	// set layout
	a.gui.SetManagerFunc(a.Layout)

	// set keybindings — a failure here (bad key, duplicate binding) is a
	// programming error, fatal at startup. The gui already exists by now, so close
	// it first to restore the terminal before exiting.
	if err := a.setKeybindings(); err != nil {
		a.gui.Close()
		log.Fatal(err)
	}
}

// updateDamage / updateSessions / updateGraph are the gui-coupled wrappers: each
// gathers the panel width, calls the pure renderer in render.go, and writes the
// view. They run inside refresh()'s single g.Update, i.e. on the main loop, so
// reading view geometry and writing views here is race-free.

func (a *App) updateDamage(cur *session.CombatSession, live bool) {
	str := renderDamage(cur, live, a.viewInnerWidth(viewDamage))
	a.writeView(viewDamage, str)
}

// updateStatus repaints the top-left "Now" box: character, class/level, zone,
// and the zone-wide xp-kill rate.
func (a *App) updateStatus() {
	a.mu.Lock()
	char := a.character
	a.mu.Unlock()

	var class eqclass.Class
	var level, kills, perHour, deaths int
	var zone string
	if a.tracker != nil {
		class, level = a.tracker.Class(), a.tracker.Level()
		zone = a.tracker.Zone()
		kills, perHour, deaths = a.tracker.ZoneKillStats(time.Now().Unix())
	}

	str := renderStatus(char, class, level, zone, kills, perHour, deaths, a.viewInnerWidth(viewStatus))
	a.writeView(viewStatus, str)
}

func (a *App) updateSessions(dat []*session.CombatSession, selected int) {
	width := a.viewInnerWidth(viewSessions)
	height := a.viewInnerHeight(viewSessions)
	str := renderSessions(dat, selected, width)
	total := len(dat) * linesPerCard

	a.mu.Lock()
	// only chase the selection when it changed (keyboard/click, or follow
	// snapping to a new live fight) — wheel scrolling leaves it untouched.
	if selected != a.lastSel {
		a.scrollY = ensureVisible(a.scrollY, selected, height)
		a.lastSel = selected
	}
	a.scrollY = clampScroll(a.scrollY, total, height)
	sy := a.scrollY
	a.mu.Unlock()

	a.writeView(viewSessions, str)
	if v, err := a.gui.View(viewSessions); err == nil {
		v.SetOrigin(0, sy)
	}
}

func (a *App) updateGraph(cur *session.CombatSession) {
	v := vp[viewGraph]
	maxX, maxY := a.gui.Size()
	x1, y1, x2, y2 := GetScreenDims(v, maxX, maxY)

	// inner drawable area, minus the view borders
	width := x2 - x1 - 1
	height := y2 - y1 - 1

	agg := cur.GetAggressors()

	// rank dealers by total damage, highest first
	sort.SliceStable(agg, func(i, j int) bool {
		return agg[i].Total > agg[j].Total
	})

	str := renderBars(agg, width, height)
	a.writeView(viewGraph, str)
}

// viewInnerWidth returns the drawable column count inside a view, borders
// excluded. It reads the view's *actual* placed size (the bottom-row columns are
// positioned dynamically), falling back to the static vp coords before the first
// Layout has run.
func (a *App) viewInnerWidth(name string) int {
	if v, err := a.gui.View(name); err == nil {
		if w, _ := v.Size(); w > 0 {
			return w
		}
	}
	maxX, maxY := a.gui.Size()
	x1, _, x2, _ := GetScreenDims(vp[name], maxX, maxY)
	if w := x2 - x1 - 1; w > 0 {
		return w
	}
	return 0
}

// viewInnerHeight returns the drawable row count inside a view, borders excluded.
// Like viewInnerWidth, it prefers the actual placed size.
func (a *App) viewInnerHeight(name string) int {
	if v, err := a.gui.View(name); err == nil {
		if _, h := v.Size(); h > 0 {
			return h
		}
	}
	maxX, maxY := a.gui.Size()
	_, y1, _, y2 := GetScreenDims(vp[name], maxX, maxY)
	if h := y2 - y1 - 1; h > 0 {
		return h
	}
	return 0
}
