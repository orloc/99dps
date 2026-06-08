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
