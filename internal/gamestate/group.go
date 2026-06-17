package gamestate

import (
	"slices"
	"strconv"
	"strings"
)

// Group-membership timing. EQ logs first-person group join/leave lines, so the
// clock starts when you join or form a group and clears when you leave it or it
// disbands. It deliberately spans zones — a group survives zoning — so this is
// independent of the zone tracker.

// groupStartLines put you in a group; groupEndLines take you out of one. All are
// unambiguous first-person ("You"/"Your") lines.
var (
	groupStartLines = []string{"You have joined the group.", "You have formed the group."}
	groupEndLines   = []string{"You have been removed from the group.", "Your group has been disbanded.", "You have left the group."}
)

// observeGroupLocked updates group timing from a self group line. Caller holds
// the lock. Joining is recorded only on the transition into a group (so a later
// "X has joined" doesn't reset your own clock — those name someone else and never
// match these self lines anyway).
func (t *Tracker) observeGroupLocked(body string, at int64) {
	if slices.Contains(groupStartLines, body) {
		if t.groupJoinedAt == 0 {
			t.groupJoinedAt = at
		}
		return
	}
	if slices.Contains(groupEndLines, body) {
		t.groupJoinedAt = 0
	}
}

// observeLocLocked captures the player's position from a "/loc" line. EQ prints
// "Your Location is <Y>, <X>, <Z>", so the world coords are worldX=second,
// worldY=first. Caller holds the lock.
func (t *Tracker) observeLocLocked(body string, at int64) {
	rest, ok := strings.CutPrefix(body, "Your Location is ")
	if !ok {
		return
	}
	f := strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' })
	if len(f) < 3 {
		return
	}
	y, e1 := strconv.ParseFloat(f[0], 64)
	x, e2 := strconv.ParseFloat(f[1], 64)
	z, e3 := strconv.ParseFloat(f[2], 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return
	}
	t.locX, t.locY, t.locZ, t.haveLoc, t.locAt = x, y, z, true, at
}

// LogoutPause accounts for a camp/logout: EQ buffs pause while you're logged
// out, but the app's timers are real-time, so when the log resumes after a long
// silence we shift active SELF-BUFFS forward by the gap (freezing them over the
// logout) and drop effects that don't survive a relog — mob debuffs, charm/mez,
// and incoming debuffs on you. A later wear-off line self-corrects any overshoot.
//
// The same gap is pushed onto the zone and group "since" clocks, so time-in-zone
// and time-in-group count only time actually logged in — an overnight camp
// doesn't read as "18h in zone". (An explicit relog re-emits "You have entered
// X", which resets the zone clock to the login moment anyway; this also covers a
// long idle/AFK with no zone-in line.)
func (t *Tracker) LogoutPause(seconds int64) {
	if t == nil || seconds <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, tm := range t.timers {
		if tm.Target == "You" && !tm.Detrimental {
			tm.Start += seconds
			tm.Expiry += seconds
			t.timers[k] = tm
		} else {
			delete(t.timers, k) // a mob debuff / charm / mez / incoming — gone on relog
		}
	}
	if t.zone.enteredAt != 0 {
		t.zone.enteredAt += seconds
	}
	if t.groupJoinedAt != 0 {
		t.groupJoinedAt += seconds
	}
}

// Loc returns the player's last-known world position (X, Y, Z) and whether a
// /loc has been seen. EQ only logs position on /loc, so it's a snapshot. Z drives
// the map's floor scoping in multi-level zones.
func (t *Tracker) Loc() (x, y, z float64, ok bool) {
	if t == nil {
		return 0, 0, 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.locX, t.locY, t.locZ, t.haveLoc
}

// TimeInGroup returns how many seconds you've been in the current group and
// whether you're grouped at all (false when solo). Never negative.
func (t *Tracker) TimeInGroup(now int64) (int64, bool) {
	if t == nil {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.groupJoinedAt == 0 {
		return 0, false
	}
	if d := now - t.groupJoinedAt; d > 0 {
		return d, true
	}
	return 0, true
}
