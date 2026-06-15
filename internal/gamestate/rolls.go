package gamestate

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Roll is one EQ /random result — who rolled, the range, and the value. EQ logs
// /random across two consecutive lines (the die line names the roller; the
// result line carries the range + value), which rollTracker pairs.
type Roll struct {
	Roller           string
	Low, High, Value int
	At               int64
}

// rollDiePrefix is the first /random line: "**A Magic Die is rolled by <Name>.".
const rollDiePrefix = "**A Magic Die is rolled by "

// rollResultRe matches the second /random line: the range and the rolled value
// ("**It could have been any number from 0 to 1000, but this time it turned up a 842.").
var rollResultRe = regexp.MustCompile(`^\*\*It could have been any number from (\d+) to (\d+), but this time it turned up a (\d+)\.`)

// rollTracker pairs the two-line /random output into a list of rolls for the
// Tools-tab winner widget. The die line stashes the roller; the next result line
// records the roll. Methods are caller-locked (the owning Tracker holds the mutex).
type rollTracker struct {
	pendingRoller string
	rolls         []Roll
}

func (r *rollTracker) clear() { r.pendingRoller, r.rolls = "", nil }

// observeLocked folds a /random line into the roll list. Caller holds the lock.
func (r *rollTracker) observeLocked(body string, at int64) {
	if name, ok := strings.CutPrefix(body, rollDiePrefix); ok {
		r.pendingRoller = strings.TrimSuffix(strings.TrimSpace(name), ".")
		return
	}
	if m := rollResultRe.FindStringSubmatch(body); m != nil {
		if r.pendingRoller == "" {
			return // a result with no preceding die line — can't attribute it
		}
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		v, _ := strconv.Atoi(m[3])
		r.rolls = append(r.rolls, Roll{Roller: r.pendingRoller, Low: lo, High: hi, Value: v, At: at})
		r.pendingRoller = ""
	}
}

// rollsLocked returns the rolls sorted highest value first (the winner on top),
// ties broken by who rolled first. Caller holds the lock.
func (r *rollTracker) rollsLocked() []Roll {
	out := slices.Clone(r.rolls)
	slices.SortStableFunc(out, func(a, b Roll) int {
		if a.Value != b.Value {
			return b.Value - a.Value // descending
		}
		return int(a.At - b.At) // earlier roll wins a tie
	})
	return out
}

// --- Tracker facade ---

// Rolls returns the tracked /random rolls, highest value first (winner on top).
func (t *Tracker) Rolls() []Roll {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rolls.rollsLocked()
}

// ClearRolls drops the tracked rolls — for the Tools widget to reset between
// loot contests without clearing the rest of the tracker.
func (t *Tracker) ClearRolls() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.rolls.clear()
	t.mu.Unlock()
}
