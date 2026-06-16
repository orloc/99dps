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
