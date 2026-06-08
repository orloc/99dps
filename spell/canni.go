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

// canniPctLocked is throughput vs the recast cap over the current dance. Uses
// log times, so it's robust to the 1-second log resolution over a run of casts.
func (t *Tracker) canniPctLocked() int {
	intervals := t.canniCasts - 1
	if intervals < 1 || t.canniEdgeMs <= 0 {
		return 0
	}
	elapsed := t.canniLastCast - t.canniDanceStart
	if elapsed < 1 {
		elapsed = 1
	}
	pct := int(int64(intervals) * int64(t.canniEdgeMs) * 100 / (elapsed * 1000))
	if pct > 100 {
		pct = 100
	}
	return pct
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
