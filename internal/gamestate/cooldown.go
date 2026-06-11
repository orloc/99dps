package gamestate

import (
	"99dps/internal/eqclass"
	"sort"
	"strings"
)

// Cooldown is a class skill with a fixed reuse timer, detected from a self log
// message (not a damage or cast line). Unlike spell buffs the reuse is a fixed
// duration, and any attempt — success or failure — starts it.
type Cooldown struct {
	Name     string
	Class    eqclass.Class
	ReuseSec int64
	matches  func(line string) bool
	// requireClass: the activation isn't class-exclusive (a kick — warriors kick
	// too), so only count it for an already-known matching class, and never infer
	// the class from it.
	requireClass bool
}

// monkSpecialReuseSec is the reuse on monk special attacks (kick / hand strike).
// Measured from Kelkix's logs: kicks chain no faster than ~5s apart.
const monkSpecialReuseSec = 5

// cooldownRegistry lists the tracked skill cooldowns. Activation strings are
// matched leniently to cover the success / partial / fail message variants, since
// every variant still consumes the reuse timer. Add abilities here as their
// exact messages are confirmed from real logs.
var cooldownRegistry = []Cooldown{
	{
		Name:     "Mend",
		Class:    eqclass.ClassMonk,
		ReuseSec: 360, // 6-minute reuse (confirmed)
		matches: func(s string) bool {
			// success: "You mend your wounds and heal some damage."
			// partial: "You mended some of your wounds."
			// fail:    "...attempt to mend failed." / "You worsen your wounds."
			return strings.Contains(s, "mend your wounds") ||
				strings.Contains(s, "mended some of your wounds") ||
				strings.Contains(s, "to mend failed") ||
				(strings.Contains(s, "worsen") && strings.Contains(s, "wounds"))
		},
	},
	{
		// the level-appropriate kick (Round Kick → Flying Kick); the panel names it
		// by level. Logged as a generic "kick", which isn't monk-only, so it's
		// class-gated and non-inferring.
		Name:         "Kick",
		Class:        eqclass.ClassMonk,
		ReuseSec:     monkSpecialReuseSec,
		requireClass: true,
		matches: func(s string) bool {
			return strings.HasPrefix(s, "You kick ") || strings.HasPrefix(s, "You try to kick ")
		},
	},
	{
		// the hand special (Tiger Claw / Eagle Strike / Dragon Punch), logged as a
		// generic "strike"; named by level in the panel.
		Name:         "Strike",
		Class:        eqclass.ClassMonk,
		ReuseSec:     monkSpecialReuseSec,
		requireClass: true,
		matches: func(s string) bool {
			return strings.HasPrefix(s, "You strike ") || strings.HasPrefix(s, "You try to strike ")
		},
	},
}

// CooldownTimer is one ability's live reuse state for the panel.
type CooldownTimer struct {
	Name      string
	Remaining int64 // seconds until ready; <= 0 means ready now
	Total     int64 // full reuse duration, for the panel's charge bar
}

// reuseSecFor returns an ability's full reuse duration (registry, or the
// macro-driven Feign Death), so the panel can draw how charged it is.
func reuseSecFor(name string) int64 {
	if name == "Feign Death" {
		return feignReuseSec
	}
	for i := range cooldownRegistry {
		if cooldownRegistry[i].Name == name {
			return cooldownRegistry[i].ReuseSec
		}
	}
	return 0
}

const (
	// bindDurationSec is how long bind wound channels — measured from Kelkix's
	// logs (begin → "complete" is a consistent 10s).
	bindDurationSec = 10
	// bindGraceSec tolerates a slightly-late "complete" line before the bar clears.
	bindGraceSec = 4
)

// cooldownTracker is the activated-ability subsystem: skill reuse timers (Mend,
// Feign Death), the feign success/fail banner state, and the bind-wound channel.
// Several of its matchers can reveal the player's class; they return the
// inferred class (ClassUnknown when none) for the owning Tracker to apply. Its
// methods are caller-locked.
type cooldownTracker struct {
	cooldowns      map[string]int64 // ability name -> reuse-expiry unix seconds
	feignAttemptAt int64            // log time of the last feign attempt (macro)
	feignFailAt    int64            // log time of the last failed feign (0 = none)
	bindStartAt    int64            // log time bandaging began
	bindDoneAt     int64            // log time bandaging last completed
}

func (c *cooldownTracker) clear() {
	c.cooldowns = make(map[string]int64)
	c.feignAttemptAt, c.feignFailAt = 0, 0
	c.bindStartAt, c.bindDoneAt = 0, 0
}

// FeignState is the outcome of the most recent feign, for the panel banner.
type FeignState int

const (
	FeignNone    FeignState = iota
	FeignPending            // attempt seen, too soon to know if a fail follows
	FeignOK                 // attempt with no failure message — feigned
	FeignFailed             // "fallen to the ground" — mobs still attacking
)

const (
	feignFailGraceSec = 2 // a fail message lands within ~this long of the attempt
	feignOKShowSec    = 5 // how long a success banner stays up
	feignFailShowSec  = 8 // how long a failure alert stays up
)

// feignReuseSec is the Feign Death reuse. Measured from real spam logs: the
// macro fires on each FD activation, consistently 11s apart, and a failed feign
// consumes the timer too.
const feignReuseSec = 11

// feignAttemptLocked records that the player initiated a feign and starts the FD
// reuse countdown. Returns ClassMonk (the inferred class). Caller holds lock.
func (c *cooldownTracker) feignAttemptLocked(at int64) eqclass.Class {
	c.feignAttemptAt = at
	c.cooldowns["Feign Death"] = at + feignReuseSec
	return eqclass.ClassMonk
}

// feignFailedLocked records the player's failed feign. Returns ClassMonk.
// Caller holds lock.
func (c *cooldownTracker) feignFailedLocked(at int64) eqclass.Class {
	c.feignFailAt = at
	return eqclass.ClassMonk
}

// feignStatusLocked reports the current feign banner state at `now`. Caller
// holds lock.
func (c *cooldownTracker) feignStatusLocked(now int64) FeignState {
	a, f := c.feignAttemptAt, c.feignFailAt
	if f > 0 && f >= a && now-f <= feignFailShowSec {
		return FeignFailed
	}
	if a > 0 && now-a <= feignOKShowSec {
		if now-a >= feignFailGraceSec {
			return FeignOK
		}
		return FeignPending
	}
	return FeignNone
}

// matchLocked starts (or restarts) a reuse timer when a line is an ability
// activation, and returns the class that ability reveals (ClassUnknown if the
// line matched nothing). Caller holds the lock.
func (c *cooldownTracker) matchLocked(body string, at int64, class eqclass.Class) eqclass.Class {
	for _, cd := range cooldownRegistry {
		if !cd.matches(body) {
			continue
		}
		if cd.requireClass && class != cd.Class {
			continue // not class-exclusive (a kick) — only count it for a known matching class
		}
		c.cooldowns[cd.Name] = at + cd.ReuseSec
		if cd.requireClass {
			return eqclass.ClassUnknown // never infer the class from a non-exclusive activation
		}
		return cd.Class
	}
	return eqclass.ClassUnknown
}

// observeBindLocked tracks the bind-wound channel: "You begin to bandage" starts
// it, "complete"/"failed" ends it. Caller holds the lock.
func (c *cooldownTracker) observeBindLocked(body string, at int64) {
	if strings.HasPrefix(body, "You begin to bandage") {
		c.bindStartAt = at
	}
	if strings.Contains(body, "bandaging is complete") ||
		strings.Contains(body, "attempt to bandage has failed") {
		c.bindDoneAt = at
	}
}

// bindRemainingLocked reports seconds left on an in-progress bind and whether one
// is active. Caller holds the lock.
func (c *cooldownTracker) bindRemainingLocked(now int64) (int, bool) {
	if c.bindStartAt <= c.bindDoneAt || now-c.bindStartAt > bindDurationSec+bindGraceSec {
		return 0, false
	}
	rem := c.bindStartAt + bindDurationSec - now
	if rem < 0 {
		rem = 0
	}
	return int(rem), true
}

// timersLocked returns the reuse timers, soonest-ready first. Caller holds lock.
func (c *cooldownTracker) timersLocked(now int64) []CooldownTimer {
	out := make([]CooldownTimer, 0, len(c.cooldowns))
	for name, exp := range c.cooldowns {
		rem := exp - now
		if rem < 0 {
			rem = 0
		}
		out = append(out, CooldownTimer{Name: name, Remaining: rem, Total: reuseSecFor(name)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Remaining != out[j].Remaining {
			return out[i].Remaining < out[j].Remaining
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// --- Tracker facade: lock the shared mutex, delegate, apply any inferred class ---

// FeignAttempt records that the player initiated a feign (detected via their
// custom macro line) and starts the FD reuse countdown. Seeing it also infers
// the class as Monk.
func (t *Tracker) FeignAttempt(at int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.inferClassLocked(t.cool.feignAttemptLocked(at))
	t.mu.Unlock()
}

// FeignFailed records the player's failed feign (mobs still attacking). The
// parser gates this to the player's own line so another monk's fail in the zone
// doesn't trip it.
func (t *Tracker) FeignFailed(at int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.inferClassLocked(t.cool.feignFailedLocked(at))
	t.mu.Unlock()
}

// FeignStatus reports the current feign banner state at wall-clock `now`. A
// recent failure always alerts (even with no macro attempt); otherwise a recent
// attempt with no following failure reads as success once the grace window has
// passed.
func (t *Tracker) FeignStatus(now int64) FeignState {
	if t == nil {
		return FeignNone
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cool.feignStatusLocked(now)
}

// BindRemaining reports the seconds left on an in-progress bind wound and whether
// one is active. Active ends on the "complete"/"failed" line or after the
// duration (plus a little grace) elapses.
func (t *Tracker) BindRemaining(now int64) (int, bool) {
	if t == nil {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cool.bindRemainingLocked(now)
}

// Cooldowns returns the player's reuse timers (soonest-ready first), each as
// remaining seconds (0 = ready). Entries persist after readiness so the panel
// can show "ready"; they're dropped on a character switch.
func (t *Tracker) Cooldowns(now int64) []CooldownTimer {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cool.timersLocked(now)
}
