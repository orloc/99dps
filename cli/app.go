package cli

import (
	"99dps/common"
	"99dps/session"
	"99dps/spell"
	"fmt"
	"github.com/jroimartin/gocui"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// linesPerCard is how many terminal rows each session occupies in the side
// panel (name, meta, top-dealer, blank). Click-to-select divides by this to map
// a clicked row back to a session.
const linesPerCard = 4

type App struct {
	gui     *gocui.Gui
	manager *session.SessionManager
	tracker *spell.Tracker

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

func New(m *session.SessionManager, character string, tracker *spell.Tracker) *App {
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
		common.CheckErr(err)
	}

	a.initGui()

	return a
}

func (a *App) Loop() {
	if err := a.gui.MainLoop(); err != nil && err != gocui.ErrQuit {
		common.CheckErr(err)
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

// Close tears down the gui, restoring the terminal. Call it after Sync has
// stopped so no repaint races the teardown.
func (a *App) Close() {
	a.gui.Close()
}

// refresh snapshots all sessions once, resolves which one is selected, and
// repaints every panel from that single consistent view of the data.
func (a *App) refresh() {
	all := a.manager.All()
	sel := a.resolveSelection(len(all))

	var cur *session.CombatSession
	if sel >= 0 {
		cur = all[sel]
	}

	// the live session is the newest one that hasn't been closed (a death/zone
	// can close the last session before a new fight has started)
	live := sel >= 0 && sel == len(all)-1 && all[sel].EndTime().IsZero()

	a.updateStatus()
	a.updateSessions(all, sel)
	a.updateDamage(cur, live)
	a.updateGraph(cur)
	a.updatePanel(cur)
	a.updateRepops()
	a.updateCC()
	a.updateShortcuts()
}

// updatePanel repaints the bottom-right panel according to the player's class
// category: casters get spell timers, pure melee get the skills breakdown, and
// hybrids get spell timers with a one-line skills digest underneath. Until a
// /who reveals the class it defaults to spell timers (CatCaster).
func (a *App) updatePanel(cur *session.CombatSession) {
	width := a.viewInnerWidth(viewTimers)
	now := time.Now().Unix()

	cat := common.CatCaster
	var class common.Class
	var level int
	if a.tracker != nil {
		cat = a.tracker.Category()
		class = a.tracker.Class()
		level = a.tracker.Level()
	}

	var str string
	var timerMap map[int]string // panel line -> buff target, for click-to-dismiss
	switch cat {
	case common.CatMelee:
		var cds []spell.CooldownTimer
		if a.tracker != nil {
			cds = a.tracker.Cooldowns(now)
		}
		str = renderSkills(cur, cds, class, level, width)
		if a.tracker != nil {
			if a.tracker.Binding(now) {
				str = headerBar("⏳ bandaging…", "43;30", width) + str
			}
			switch a.tracker.FeignStatus(now) {
			case spell.FeignFailed:
				str = headerBar("⚠ FEIGN FAILED — mobs still on you", "41;1;37", width) + str
			case spell.FeignOK:
				str = headerBar("✓ feigned", "42;1;30", width) + str
			}
		}
	case common.CatHybrid:
		str, timerMap = a.timersStr(width)
		if sum := skillsSummary(cur, class, level); sum != "" {
			str += "\n" + headerBar("skills", dpsHeaderSGR, width) + "  " + sum
		}
	default: // CatCaster
		if a.tracker != nil {
			str, timerMap = a.timersStr(width)
		}
	}

	// the canni-dance meter rides above the panel while you're actively dancing —
	// it shifts the timer lines down, so re-key the click map by the same offset.
	if a.tracker != nil {
		if cm := renderCanni(a.tracker.CanniStats(now), width); cm != "" {
			off := strings.Count(cm, "\n") + 1
			if timerMap != nil {
				shifted := make(map[int]string, len(timerMap))
				for k, v := range timerMap {
					shifted[k+off] = v
				}
				timerMap = shifted
			}
			if str != "" {
				str = cm + "\n" + str
			} else {
				str = cm
			}
		}
	}

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

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewTimers, str)
		if v, err := g.View(viewTimers); err == nil {
			v.SetOrigin(0, sy)
		}
		return nil
	})
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

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewRepops, str)
		if v, err := g.View(viewRepops); err == nil {
			v.SetOrigin(0, sy)
		}
		return nil
	})
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

	a.gui.Update(func(g *gocui.Gui) error {
		if _, err := g.View(viewCC); err != nil {
			return nil // not laid out yet this frame
		}
		a.writeView(viewCC, str)
		return nil
	})
}

// announceLowBuffs speaks a cue when a (non-charm) timer first drops below the
// low threshold, once per timer, re-arming when it's refreshed or expires.
func (a *App) announceLowBuffs(active []spell.Timer, now int64) {
	for _, p := range a.dueAnnouncements(active, now) {
		a.speaker.say(p)
	}
}

// dueAnnouncements returns the phrases to speak this tick and updates the
// announced set (each timer fires once; re-arms when refreshed or gone). The
// speaking itself is left to the caller so this stays unit-testable.
func (a *App) dueAnnouncements(active []spell.Timer, now int64) []string {
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

func lowBuffPhrase(tm spell.Timer) string {
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

// toggleTTS flips audio cues on/off at runtime (bound to a key).
func (a *App) toggleTTS(gui *gocui.Gui, view *gocui.View) error {
	a.mu.Lock()
	if a.speaker.available() {
		a.ttsOn = !a.ttsOn
	}
	on, has := a.ttsOn, a.speaker.available()
	a.mu.Unlock()

	switch {
	case !has:
		a.flashStatus("no TTS engine found (install spd-say or espeak)")
	case on:
		a.speaker.say("audio cues on")
		a.flashStatus("♪ audio cues ON")
	default:
		a.flashStatus("♪ audio cues off")
	}
	a.refresh()
	return nil
}

// flashStatus shows a transient banner in the bottom bar.
func (a *App) flashStatus(msg string) {
	a.mu.Lock()
	a.status = msg
	a.statusTicks = 6
	a.mu.Unlock()
}

func (a *App) timerWheelUp(gui *gocui.Gui, view *gocui.View) error {
	a.scrollTimers(-scrollStep)
	return nil
}

func (a *App) timerWheelDown(gui *gocui.Gui, view *gocui.View) error {
	a.scrollTimers(scrollStep)
	return nil
}

// scrollTimers nudges the spell-timer panel; the clamp to content happens in
// updateTimers.
func (a *App) scrollTimers(delta int) {
	a.mu.Lock()
	a.timerScrollY += delta
	if a.timerScrollY < 0 {
		a.timerScrollY = 0
	}
	a.mu.Unlock()
	a.refresh()
}

func (a *App) repopWheelUp(gui *gocui.Gui, view *gocui.View) error {
	a.scrollRepops(-scrollStep)
	return nil
}

func (a *App) repopWheelDown(gui *gocui.Gui, view *gocui.View) error {
	a.scrollRepops(scrollStep)
	return nil
}

// scrollRepops nudges the Mob Tracker panel; the clamp to content happens in
// updateRepops.
func (a *App) scrollRepops(delta int) {
	a.mu.Lock()
	a.repopScrollY += delta
	if a.repopScrollY < 0 {
		a.repopScrollY = 0
	}
	a.mu.Unlock()
	a.refresh()
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
	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewShortcuts, text)
		return nil
	})
}

// resolveSelection clamps the pinned selection to the available sessions and,
// while following, snaps it to the newest one. Returns -1 when there are none.
func (a *App) resolveSelection(n int) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n == 0 {
		return -1
	}
	if a.follow || a.selected >= n {
		a.selected = n - 1
	} else if a.selected < 0 {
		a.selected = 0
	}
	return a.selected
}

// quit unwinds the gocui main loop; the gui itself is closed by launchCLI after
// the repaint loop has stopped (see App.Close).
func (a *App) quit(gui *gocui.Gui, view *gocui.View) error {
	return gocui.ErrQuit
}

func (a *App) clear(gui *gocui.Gui, view *gocui.View) error {
	// while editing a repop timer, Backspace deletes a character instead
	a.mu.Lock()
	if a.editing {
		if len(a.editBuf) > 0 {
			a.editBuf = a.editBuf[:len(a.editBuf)-1]
		}
		a.mu.Unlock()
		a.refresh()
		return nil
	}
	a.mu.Unlock()

	a.manager.Clear()
	a.mu.Lock()
	a.selected = 0
	a.follow = true
	a.scrollY = 0
	a.lastSel = 0
	a.mu.Unlock()
	a.refresh()
	return nil
}

// dismissTimerClick removes all of a target's spell timers when its row (or
// header) in the Spell Timers panel is clicked — for pruning a long raid-buff
// list as people leave.
func (a *App) dismissTimerClick(gui *gocui.Gui, view *gocui.View) error {
	_, cy := view.Cursor()
	_, oy := view.Origin()
	a.mu.Lock()
	m := a.timerLineTargets
	if view.Name() == viewCC {
		m = a.ccLineTargets
	}
	tgt := m[oy+cy]
	a.mu.Unlock()
	if tgt != "" && a.tracker != nil {
		a.tracker.DismissTarget(tgt)
		a.refresh()
	}
	return nil
}

// selectRepopClick selects the Repops row under the click and opens the inline
// editor for that mob's respawn override.
func (a *App) selectRepopClick(gui *gocui.Gui, view *gocui.View) error {
	_, cy := view.Cursor()
	_, oy := view.Origin()
	a.mu.Lock()
	mob := a.repopLineMobs[oy+cy]
	if mob != "" {
		a.repopSel = mob
		a.editing = true
		a.editBuf = ""
	}
	a.mu.Unlock()
	if mob != "" {
		a.refresh()
	}
	return nil
}

// editType appends a typed character to the repop-timer editor (digits/colon).
func (a *App) editType(ch byte) func(*gocui.Gui, *gocui.View) error {
	return func(*gocui.Gui, *gocui.View) error {
		a.mu.Lock()
		ed := a.editing
		if ed && len(a.editBuf) < 8 {
			a.editBuf += string(ch)
		}
		a.mu.Unlock()
		if ed {
			a.refresh()
		}
		return nil
	}
}

// editCommit parses the typed time and saves it as the mob's respawn override.
func (a *App) editCommit(gui *gocui.Gui, view *gocui.View) error {
	a.mu.Lock()
	if !a.editing {
		a.mu.Unlock()
		return nil
	}
	buf, mob := a.editBuf, a.repopSel
	a.editing, a.editBuf = false, ""
	a.mu.Unlock()

	if sec, ok := parseTimer(buf); ok && a.tracker != nil {
		a.tracker.SetOverride(mob, sec)
		a.flashStatus(fmt.Sprintf("%s → %s repop", mob, fmtDuration(time.Duration(sec)*time.Second)))
	}
	a.refresh()
	return nil
}

// editCancel closes the editor without saving.
func (a *App) editCancel(gui *gocui.Gui, view *gocui.View) error {
	a.mu.Lock()
	editing := a.editing
	a.editing, a.editBuf, a.repopSel = false, "", ""
	a.mu.Unlock()
	if editing {
		a.refresh()
	}
	return nil
}

// parseTimer reads a respawn time as "h:mm:ss", "m:ss", or plain seconds into
// total seconds. Returns false on malformed input or a non-positive total.
func parseTimer(s string) (int, bool) {
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

// selectUp pins the selection one session older (toward the top), leaving
// follow mode so the panels stop tracking the live fight.
func (a *App) selectUp(gui *gocui.Gui, view *gocui.View) error {
	n := a.manager.Len()
	a.mu.Lock()
	cur := a.effectiveLocked(n)
	if cur > 0 {
		a.selected = cur - 1
		a.follow = false
	}
	a.mu.Unlock()
	a.refresh()
	return nil
}

// selectDown moves the selection one session newer; reaching the live session
// re-enables follow mode.
func (a *App) selectDown(gui *gocui.Gui, view *gocui.View) error {
	n := a.manager.Len()
	a.mu.Lock()
	cur := a.effectiveLocked(n)
	if cur >= 0 && cur < n-1 {
		a.selected = cur + 1
	}
	if a.selected >= n-1 {
		a.follow = true
	}
	a.mu.Unlock()
	a.refresh()
	return nil
}

// selectLive jumps back to the live session and resumes follow mode.
func (a *App) selectLive(gui *gocui.Gui, view *gocui.View) error {
	a.mu.Lock()
	a.follow = true
	a.mu.Unlock()
	a.refresh()
	return nil
}

// selectClick pins the session under the clicked row. gocui has already moved
// the view cursor to the click position by the time this fires.
func (a *App) selectClick(gui *gocui.Gui, view *gocui.View) error {
	_, cy := view.Cursor()
	_, oy := view.Origin()
	idx := (oy + cy) / linesPerCard

	n := a.manager.Len()
	a.mu.Lock()
	if idx >= 0 && idx < n {
		a.selected = idx
		a.follow = idx == n-1
	}
	a.mu.Unlock()
	a.refresh()
	return nil
}

// effectiveLocked returns the currently resolved selection index. Caller holds mu.
func (a *App) effectiveLocked(n int) int {
	if n == 0 {
		return -1
	}
	if a.follow || a.selected >= n {
		return n - 1
	}
	if a.selected < 0 {
		return 0
	}
	return a.selected
}

func (a *App) initGui() {
	// default config
	a.gui.Cursor = false
	a.gui.InputEsc = true
	a.gui.Mouse = true
	a.gui.BgColor = gocui.ColorDefault
	a.gui.FgColor = gocui.ColorDefault

	// set layout
	a.gui.SetManagerFunc(a.Layout)

	// set keybindings — a failure here (bad key, duplicate binding) would leave
	// a silently dead control, so treat it as fatal at startup like NewGui.
	common.CheckErr(a.setKeybindings())
}

// updateDamage / updateSessions / updateGraph are the gui-coupled wrappers:
// each gathers the panel width, calls the pure renderer in render.go, and pushes
// the result onto the gocui event loop.

func (a *App) updateDamage(cur *session.CombatSession, live bool) {
	str := renderDamage(cur, live, a.viewInnerWidth(viewDamage))
	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewDamage, str)
		return nil
	})
}

// updateStatus repaints the top-left "Now" box: character, class/level, zone,
// and the zone-wide xp-kill rate.
func (a *App) updateStatus() {
	a.mu.Lock()
	char := a.character
	a.mu.Unlock()

	var class common.Class
	var level, kills, perHour, deaths int
	var zone string
	if a.tracker != nil {
		class, level = a.tracker.Class(), a.tracker.Level()
		zone = a.tracker.Zone()
		kills, perHour, deaths = a.tracker.ZoneKillStats(time.Now().Unix())
	}

	str := renderStatus(char, class, level, zone, kills, perHour, deaths, a.viewInnerWidth(viewStatus))
	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewStatus, str)
		return nil
	})
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

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewSessions, str)
		if v, err := g.View(viewSessions); err == nil {
			v.SetOrigin(0, sy)
		}
		return nil
	})
}

// scrollSessions nudges the session-panel viewport by delta lines. The clamp to
// content happens in updateSessions, so here we only guard the floor.
func (a *App) scrollSessions(delta int) {
	a.mu.Lock()
	a.scrollY += delta
	if a.scrollY < 0 {
		a.scrollY = 0
	}
	a.mu.Unlock()
	a.refresh()
}

func (a *App) wheelUp(gui *gocui.Gui, view *gocui.View) error {
	a.scrollSessions(-scrollStep)
	return nil
}

func (a *App) wheelDown(gui *gocui.Gui, view *gocui.View) error {
	a.scrollSessions(scrollStep)
	return nil
}

// ensureVisible returns a scroll offset that brings the selected card fully into
// a viewport of the given height, scrolling the minimum needed.
func ensureVisible(scrollY, selected, height int) int {
	if height <= 0 || selected < 0 {
		return scrollY
	}
	top := selected * linesPerCard
	bot := top + linesPerCard - 1
	if top < scrollY {
		return top
	}
	if bot >= scrollY+height {
		return bot - height + 1
	}
	return scrollY
}

// clampScroll keeps the offset within [0, total-height].
func clampScroll(scrollY, total, height int) int {
	max := total - height
	if max < 0 {
		max = 0
	}
	if scrollY > max {
		return max
	}
	if scrollY < 0 {
		return 0
	}
	return scrollY
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
	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewGraph, str)
		return nil
	})
}

// viewInnerWidth returns the drawable column count inside a view, borders excluded.
func (a *App) viewInnerWidth(name string) int {
	maxX, maxY := a.gui.Size()
	x1, _, x2, _ := GetScreenDims(vp[name], maxX, maxY)
	w := x2 - x1 - 1
	if w < 0 {
		return 0
	}
	return w
}

// viewInnerHeight returns the drawable row count inside a view, borders excluded.
func (a *App) viewInnerHeight(name string) int {
	maxX, maxY := a.gui.Size()
	_, y1, _, y2 := GetScreenDims(vp[name], maxX, maxY)
	h := y2 - y1 - 1
	if h < 0 {
		return 0
	}
	return h
}
