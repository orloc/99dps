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

// RollWin is the winner of a finished roll contest (one item): the high roller,
// their value/range, when they won, and the zone it happened in.
type RollWin struct {
	Roller           string
	Low, High, Value int
	At               int64
	Zone             string
}

// rollDiePrefix is the first /random line: "**A Magic Die is rolled by <Name>.".
const rollDiePrefix = "**A Magic Die is rolled by "

// rollResultRe matches the second /random line: the range and the rolled value
// ("**It could have been any number from 0 to 1000, but this time it turned up a 842.").
var rollResultRe = regexp.MustCompile(`^\*\*It could have been any number from (\d+) to (\d+), but this time it turned up a (\d+)\.`)

const (
	// contestGapSec is how long a lull (no new roll) ends a contest: rolls for one
	// dropped item come within seconds of each other, so a gap this long means the
	// item's rolls are done and the next roll belongs to a new contest.
	contestGapSec = 20
	// maxWinners bounds the winners log so a long raid stays memory-bounded.
	maxWinners = 30
)

// rollTracker turns EQ's two-line /random output into roll *contests*: rolls
// accumulate into the current contest, and a lull (or a manual finalize) closes
// it — the high roller is recorded to the winners log and the board resets, ready
// for the next item. Methods are caller-locked (the owning Tracker holds the mutex).
type rollTracker struct {
	pendingRoller string
	current       []Roll    // the in-progress contest's rolls
	lastAt        int64     // time of the most recent roll in current
	winners       []RollWin // finished contests, most recent first
}

func (r *rollTracker) clear() { *r = rollTracker{} }

// observeLocked folds a /random line into the current contest. A result arriving
// after a contestGapSec lull first closes the previous contest. Caller holds the lock.
func (r *rollTracker) observeLocked(body string, at int64, zone string) {
	if name, ok := strings.CutPrefix(body, rollDiePrefix); ok {
		r.pendingRoller = strings.TrimSuffix(strings.TrimSpace(name), ".")
		return
	}
	m := rollResultRe.FindStringSubmatch(body)
	if m == nil || r.pendingRoller == "" {
		return // not a result line, or a result with no preceding die line
	}
	lo, _ := strconv.Atoi(m[1])
	hi, _ := strconv.Atoi(m[2])
	v, _ := strconv.Atoi(m[3])
	if len(r.current) > 0 && at-r.lastAt > contestGapSec {
		r.finalizeLocked(zone) // a new roll after a lull → the prior contest is over
	}
	r.current = append(r.current, Roll{Roller: r.pendingRoller, Low: lo, High: hi, Value: v, At: at})
	r.lastAt = at
	r.pendingRoller = ""
}

// finalizeLocked records the current contest's high roller to the winners log and
// resets the board. No-op when no contest is in progress. Caller holds the lock.
func (r *rollTracker) finalizeLocked(zone string) {
	if len(r.current) == 0 {
		return
	}
	win := r.current[0]
	for _, c := range r.current[1:] {
		if c.Value > win.Value { // ties keep the earliest roll (first appended)
			win = c
		}
	}
	r.winners = append([]RollWin{{
		Roller: win.Roller, Low: win.Low, High: win.High, Value: win.Value, At: win.At, Zone: zone,
	}}, r.winners...)
	if len(r.winners) > maxWinners {
		r.winners = r.winners[:maxWinners]
	}
	r.current, r.lastAt = nil, 0
}

// finalizeStaleLocked closes a contest that's gone quiet (no roll within the gap),
// so the winner lands in the log without waiting for the next contest's first
// roll. Caller holds the lock.
func (r *rollTracker) finalizeStaleLocked(now int64, zone string) {
	if len(r.current) > 0 && now-r.lastAt > contestGapSec {
		r.finalizeLocked(zone)
	}
}

// currentLocked returns the in-progress contest's rolls, high value first (ties by
// roll time). Caller holds the lock.
func (r *rollTracker) currentLocked() []Roll {
	out := slices.Clone(r.current)
	slices.SortStableFunc(out, func(a, b Roll) int {
		if a.Value != b.Value {
			return b.Value - a.Value
		}
		return int(a.At - b.At)
	})
	return out
}

// --- Tracker facade ---

// Rolls returns the in-progress contest's rolls, high value first (the leader on
// top). Empty between contests.
func (t *Tracker) Rolls() []Roll {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rolls.currentLocked()
}

// RollWinners returns the log of finished contests, most recent first.
func (t *Tracker) RollWinners() []RollWin {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.rolls.winners)
}

// FinalizeStaleRolls closes a contest that's gone quiet — call it each tick so a
// winner lands in the log shortly after the rolls stop.
func (t *Tracker) FinalizeStaleRolls(now int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.rolls.finalizeStaleLocked(now, t.zone.name)
	t.mu.Unlock()
}

// FinalizeRolls closes the current contest now (manual "the roll's over") —
// records its winner and resets the board.
func (t *Tracker) FinalizeRolls() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.rolls.finalizeLocked(t.zone.name)
	t.mu.Unlock()
}

// ClearRollWinners empties the winners log.
func (t *Tracker) ClearRollWinners() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.rolls.winners = nil
	t.mu.Unlock()
}
