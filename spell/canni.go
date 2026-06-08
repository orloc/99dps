package spell

// canniDanceTimeoutSec ends the current dance after this long with no cast (so
// stopping to fight/heal doesn't tank the score — only sloppy chaining does).
const canniDanceTimeoutSec = 12

// CanniStats is the live "canni dance" readout for the panel.
type CanniStats struct {
	Active  bool
	Rank    string // e.g. "Cannibalize III"
	EdgeMs  int    // the recast floor (the beat to hit)
	Pct     int    // throughput vs the recast cap, 0..100 (100 = perfect chaining)
	Combo   int    // consecutive clean, on-tempo casts
	Buzzers int    // too-early ("recast not yet met") presses this dance
	Score   int    // session score
	Best    int    // session best Pct
}

// recordCanniCastLocked logs a successful Cannibalize cast. Caller holds lock.
func (t *Tracker) recordCanniCastLocked(rank string, at int64) {
	s, ok := t.book.ByName(rank)
	if !ok || s.RecastMs <= 0 {
		return
	}
	// a fresh dance starts on the first cast or after a long gap
	if t.canniLastCast == 0 || at-t.canniLastCast > canniDanceTimeoutSec {
		t.canniDanceStart = at
		t.canniCasts, t.canniBuzzers, t.canniCombo, t.canniSinceCast = 0, 0, 0, 0
	}
	gap := at - t.canniLastCast
	t.canniRank, t.canniEdgeMs = rank, s.RecastMs
	t.canniCasts++

	// a "clean" cast: no buzzer since the last one, and not dawdling
	edgeSec := int64(s.RecastMs+999) / 1000
	if t.canniCasts > 1 && t.canniSinceCast == 0 && gap <= edgeSec+2 {
		t.canniCombo++
	} else {
		t.canniCombo = 1
	}
	t.canniSinceCast = 0
	t.canniScore += 10 * t.canniCombo
	t.canniLastCast = at

	if p := t.canniPctLocked(); p > t.canniBestPct {
		t.canniBestPct = p
	}
}

// recordCanniBuzzerLocked logs a too-early "Spell recast time not yet met."
func (t *Tracker) recordCanniBuzzerLocked() {
	if t.canniLastCast == 0 {
		return // not dancing
	}
	t.canniBuzzers++
	t.canniSinceCast++
	t.canniCombo = 0 // overshot the edge — broke the rhythm
}

// canniPctLocked is the dance efficiency: throughput (cast rate vs the recast
// cap) × accuracy (good casts vs total attempts). Each "recast not yet met"
// buzzer is a wasted attempt that drags accuracy — and thus efficiency — down,
// so the goal is fast *and* clean. Uses log times, robust to 1-second
// resolution over a run of casts.
func (t *Tracker) canniPctLocked() int {
	if t.canniCasts < 1 {
		return 0
	}

	// throughput: how close the cast rate is to the recast cap
	thru := 100
	if intervals := t.canniCasts - 1; intervals >= 1 && t.canniEdgeMs > 0 {
		elapsed := t.canniLastCast - t.canniDanceStart
		if elapsed < 1 {
			elapsed = 1
		}
		thru = int(int64(intervals) * int64(t.canniEdgeMs) * 100 / (elapsed * 1000))
		if thru > 100 {
			thru = 100
		}
	}

	// accuracy: successful casts out of total presses (buzzers = too-early misses)
	acc := t.canniCasts * 100 / (t.canniCasts + t.canniBuzzers)

	return thru * acc / 100
}

// CanniStats returns the live dance readout, or an empty (inactive) value once
// you stop dancing for canniDanceTimeoutSec.
func (t *Tracker) CanniStats(now int64) CanniStats {
	if t == nil {
		return CanniStats{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.canniLastCast == 0 || now-t.canniLastCast > canniDanceTimeoutSec {
		return CanniStats{}
	}
	return CanniStats{
		Active:  true,
		Rank:    t.canniRank,
		EdgeMs:  t.canniEdgeMs,
		Pct:     t.canniPctLocked(),
		Combo:   t.canniCombo,
		Buzzers: t.canniBuzzers,
		Score:   t.canniScore,
		Best:    t.canniBestPct,
	}
}
