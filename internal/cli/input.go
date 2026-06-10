package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jroimartin/gocui"
)

// This file holds the gocui input layer: the keybinding/mouse handler methods
// (bound in keys.go) and the selection/scroll bookkeeping they drive. The
// repaint pipeline and gui lifecycle live in app.go; the keybinding table in
// keys.go.

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

func (a *App) timerWheelUp(gui *gocui.Gui, view *gocui.View) error {
	a.scrollTimers(-scrollStep)
	return nil
}

func (a *App) timerWheelDown(gui *gocui.Gui, view *gocui.View) error {
	a.scrollTimers(scrollStep)
	return nil
}

// scrollTimers nudges the spell-timer panel; the clamp to content happens in
// updatePanel.
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
