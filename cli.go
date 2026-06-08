package main

import (
	app "99dps/cli"
	"99dps/loader"
	"99dps/parser"
	"99dps/session"
	"99dps/spell"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// switchPollInterval is how often we re-check which eqlog file is the most
// recently written (i.e. which character is being actively played).
const switchPollInterval = 4 * time.Second

func launchCLI(logDir, spellsPath string, tts bool) {
	src := loader.LoadFile(logDir)
	sm := &session.SessionManager{}

	// optional spell-timer tracker — disabled gracefully if spells_us.txt is
	// missing or unreadable.
	var tracker *spell.Tracker
	spellInfo := "spell timers off (no spells_us.txt)"
	if book, err := spell.Load(spellsPath); err == nil {
		tracker = spell.NewTracker(book)
		spellInfo = fmt.Sprintf("%d spells (%s)", tracker.SpellCount(), filepath.Base(spellsPath))
	}

	a := app.New(sm, src.Character, tracker)
	a.SetSources(logDir, spellInfo)
	a.SetTTS(tts)

	// stop signals the repaint loop and the switch poller to exit. We wait for
	// BOTH (they're the only callers of gui.Update) before closing the gui, so
	// nothing repaints a closed terminal.
	stop := make(chan struct{})
	var bg sync.WaitGroup

	bg.Add(1)
	go func() {
		defer bg.Done()
		a.Sync(stop)
	}()

	ctrl := &logController{dir: logDir, sm: sm, app: a, cur: src, tracker: tracker}
	ctrl.startParse(src)
	bg.Add(1)
	go func() {
		defer bg.Done()
		ctrl.watch(stop)
	}()

	a.Loop() // blocks on the gocui main loop until the user quits

	close(stop)
	bg.Wait()
	a.Close()
	ctrl.shutdown()
}

// logController owns the currently-followed log source and hot-swaps it when a
// different character's log becomes the most recently active.
type logController struct {
	dir     string
	sm      *session.SessionManager
	app     *app.App
	tracker *spell.Tracker

	mu      sync.Mutex
	cur     *loader.LogSource
	parseWG sync.WaitGroup
}

// startParse launches a parser goroutine for src; it exits when src's tail is
// stopped (which closes the line channel).
func (c *logController) startParse(src *loader.LogSource) {
	c.parseWG.Add(1)
	go func() {
		defer c.parseWG.Done()
		parser.DoParse(src.Tail, c.sm, src.Character, c.tracker)
	}()
}

// watch polls for a different most-active log and switches to it.
func (c *logController) watch(stop <-chan struct{}) {
	t := time.NewTicker(switchPollInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			latest, err := loader.Latest(c.dir)
			if err != nil {
				continue
			}
			c.mu.Lock()
			changed := latest != c.cur.Path
			c.mu.Unlock()
			if changed {
				c.switchTo(latest)
			}
		}
	}
}

// switchTo swaps the followed source to path: it opens the new tail (from the
// end, so only new combat is read), stops the old one, clears the sessions for
// the new character, and notifies the UI.
func (c *logController) switchTo(path string) {
	next, err := loader.Follow(path, true)
	if err != nil {
		return
	}

	c.mu.Lock()
	old := c.cur
	c.cur = next
	c.mu.Unlock()

	old.Tail.Stop()  // ends the old parser goroutine
	c.parseWG.Wait() // ...wait for it before reusing the manager
	c.sm.Clear()     // fresh slate for the new character
	if c.tracker != nil {
		c.tracker.Clear()
	}
	c.app.SetCharacter(next.Character)
	c.startParse(next)
}

// shutdown stops the active tail and waits for its parser goroutine.
func (c *logController) shutdown() {
	c.mu.Lock()
	cur := c.cur
	c.mu.Unlock()
	cur.Tail.Stop()
	c.parseWG.Wait()
}
