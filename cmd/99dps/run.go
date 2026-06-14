package main

import (
	"99dps/internal/gamestate"
	"99dps/internal/loader"
	"99dps/internal/parser"
	"99dps/internal/session"
	"99dps/internal/tui"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"
)

// switchPollInterval is how often we re-check which eqlog file is the most
// recently written (i.e. which character is being actively played).
const switchPollInterval = 4 * time.Second

// loadTracker builds the optional spell-timer tracker, disabled gracefully when
// spells_us.txt is missing. Returns the tracker (or nil) and a one-line summary.
func loadTracker(spellsPath, logDir string) (*gamestate.Tracker, string) {
	book, err := gamestate.Load(spellsPath)
	if err != nil {
		return nil, "spell timers off (no spells_us.txt)"
	}
	tracker := gamestate.NewTracker(book)
	// user respawn overrides live next to the logs so they're easy to find
	tracker.UseOverrides(gamestate.LoadOverrides(filepath.Join(logDir, "99dps-overrides.json")))
	return tracker, fmt.Sprintf("%d spells (%s)", tracker.SpellCount(), filepath.Base(spellsPath))
}

// launchTUI is the entry point: it wires the loader → parser → session/tracker
// pipeline, runs the Bubble Tea UI (internal/tui), and watches for a character
// switch in-game to hot-swap. The only UI.
func launchTUI(logDir, spellsPath string, tts bool) {
	src, err := loader.LoadFile(logDir)
	if err != nil {
		log.Fatal(err) // startup discovery failure — nothing to fall back to
	}
	runTUI(src, logDir, spellsPath, tts, true)
}

// launchFile runs the same pipeline against one explicit log file (the -logfile
// debug flag): it follows from the start so a captured log replays in full, and
// skips the character hot-swap watcher (there's nothing to swap to).
func launchFile(path, logDir, spellsPath string, tts bool) {
	src, err := loader.Follow(path, false)
	if err != nil {
		log.Fatal(err)
	}
	runTUI(src, logDir, spellsPath, tts, false)
}

// runTUI wires the pipeline for an already-opened source and blocks on the UI.
// watchSwitch enables the character hot-swap poller (off for a fixed -logfile).
func runTUI(src *loader.LogSource, logDir, spellsPath string, tts, watchSwitch bool) {
	sm := &session.SessionManager{}
	tracker, spellInfo := loadTracker(spellsPath, logDir)
	tracker.SetCharacter(src.Character) // for the pet's "My leader is <you>" check

	prog := tui.NewProgram(sm, tracker, src.Character, spellInfo, tts)
	ctrl := &logController{dir: logDir, sm: sm, tui: prog, cur: src, tracker: tracker}
	ctrl.startParse(src)

	stop := make(chan struct{})
	var bg sync.WaitGroup
	if watchSwitch {
		// watch for the active eqlog changing (a character switch in-game) and hot-swap.
		bg.Add(1)
		go func() {
			defer bg.Done()
			ctrl.watch(stop)
		}()
	}

	if err := prog.Run(); err != nil { // blocks until the user quits
		log.Print(err)
	}
	close(stop)
	bg.Wait()
	ctrl.shutdown()
}

// logController owns the currently-followed log source and hot-swaps it when a
// different character's log becomes the most recently active.
type logController struct {
	dir     string
	sm      *session.SessionManager
	tui     *tui.Program
	tracker *gamestate.Tracker

	mu      sync.Mutex
	cur     *loader.LogSource
	parseWG sync.WaitGroup
}

// observer adapts the optional tracker to the parser's SpellObserver interface,
// returning a true-nil interface (not a typed-nil) when there's no tracker so
// the parser's nil check works.
func (c *logController) observer() parser.SpellObserver {
	if c.tracker == nil {
		return nil
	}
	return c.tracker
}

// startParse launches a parser goroutine for src; it exits when src's tail is
// stopped (which closes the line channel).
func (c *logController) startParse(src *loader.LogSource) {
	c.parseWG.Add(1)
	go func() {
		defer c.parseWG.Done()
		parser.DoParse(src.Tail, c.sm, src.Character, c.observer())
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
		c.tracker.SetCharacter(next.Character)
		// recover the new character's active spell timers / class / zone / pet from
		// the log (the live tail below only sees new lines from end-of-file).
		parser.RebuildTrackerFromFile(next.Path, next.Character, c.tracker)
	}
	c.tui.SwitchCharacter(next.Character)
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
