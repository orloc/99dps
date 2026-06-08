package spell

import (
	"sort"
	"strings"

	"99dps/common"
)

// Cooldown is a class skill with a fixed reuse timer, detected from a self log
// message (not a damage or cast line). Unlike spell buffs the reuse is a fixed
// duration, and any attempt — success or failure — starts it.
type Cooldown struct {
	Name     string
	Class    common.Class
	ReuseSec int64
	matches  func(line string) bool
}

// cooldownRegistry lists the tracked skill cooldowns. Activation strings are
// matched leniently to cover the success / partial / fail message variants, since
// every variant still consumes the reuse timer. Add abilities here as their
// exact messages are confirmed from real logs.
var cooldownRegistry = []Cooldown{
	{
		Name:     "Mend",
		Class:    common.ClassMonk,
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
}

// CooldownTimer is one ability's live reuse state for the panel.
type CooldownTimer struct {
	Name      string
	Remaining int64 // seconds until ready; <= 0 means ready now
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

// FeignAttempt records that the player initiated a feign (detected via their
// custom macro line) and starts the FD reuse countdown. Seeing it also infers
// the class as Monk.
func (t *Tracker) FeignAttempt(at int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.feignAttemptAt = at
	t.cooldowns["Feign Death"] = at + feignReuseSec
	if t.class == common.ClassUnknown {
		t.class = common.ClassMonk
	}
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
	t.feignFailAt = at
	if t.class == common.ClassUnknown {
		t.class = common.ClassMonk
	}
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

	a, f := t.feignAttemptAt, t.feignFailAt
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

// matchCooldownLocked starts (or restarts) a reuse timer when a line is an
// ability activation. Detecting a class-specific ability also reveals the class
// if a /who hasn't yet. Caller holds the lock.
func (t *Tracker) matchCooldownLocked(body string, at int64) {
	for _, cd := range cooldownRegistry {
		if cd.matches(body) {
			if t.class == common.ClassUnknown {
				t.class = cd.Class
			}
			t.cooldowns[cd.Name] = at + cd.ReuseSec
			return
		}
	}
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

	out := make([]CooldownTimer, 0, len(t.cooldowns))
	for name, exp := range t.cooldowns {
		rem := exp - now
		if rem < 0 {
			rem = 0
		}
		out = append(out, CooldownTimer{Name: name, Remaining: rem})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Remaining != out[j].Remaining {
			return out[i].Remaining < out[j].Remaining
		}
		return out[i].Name < out[j].Name
	})
	return out
}
