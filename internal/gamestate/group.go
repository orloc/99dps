package gamestate

import "slices"

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
