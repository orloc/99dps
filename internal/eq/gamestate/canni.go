package gamestate

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

// canniMeter is the shaman "canni dance" gamification subsystem: ride the
// Cannibalize recast edge as fast as possible without the "Spell recast time
// not yet met" buzzer. Its methods are caller-locked (the owning Tracker holds
// the mutex).
type canniMeter struct {
	rank       string
	edgeMs     int
	danceStart int64
	lastCast   int64
	casts      int
	buzzers    int
	sinceCast  int
	combo      int
	score      int
	bestPct    int
}

func (c *canniMeter) clear() { *c = canniMeter{} }

// recordCastLocked logs a successful Cannibalize cast. Caller holds lock.
func (c *canniMeter) recordCastLocked(book *Book, rank string, at int64) {
	s, ok := book.ByName(rank)
	if !ok || s.RecastMs <= 0 {
		return
	}
	// a fresh dance starts on the first cast or after a long gap
	if c.lastCast == 0 || at-c.lastCast > canniDanceTimeoutSec {
		c.danceStart = at
		c.casts, c.buzzers, c.combo, c.sinceCast = 0, 0, 0, 0
	}
	gap := at - c.lastCast
	c.rank, c.edgeMs = rank, s.RecastMs
	c.casts++

	// a "clean" cast: no buzzer since the last one, and not dawdling
	edgeSec := int64(s.RecastMs+999) / 1000
	if c.casts > 1 && c.sinceCast == 0 && gap <= edgeSec+2 {
		c.combo++
	} else {
		c.combo = 1
	}
	c.sinceCast = 0
	c.score += 10 * c.combo
	c.lastCast = at

	if p := c.pctLocked(); p > c.bestPct {
		c.bestPct = p
	}
}

// recordBuzzerLocked logs a too-early "Spell recast time not yet met."
func (c *canniMeter) recordBuzzerLocked() {
	if c.lastCast == 0 {
		return // not dancing
	}
	c.buzzers++
	c.sinceCast++
	c.combo = 0 // overshot the edge — broke the rhythm
}

// pctLocked is the dance efficiency: throughput (cast rate vs the recast cap) ×
// accuracy (good casts vs total attempts). Each "recast not yet met" buzzer is a
// wasted attempt that drags accuracy — and thus efficiency — down, so the goal
// is fast *and* clean. Uses log times, robust to 1-second resolution over a run
// of casts.
func (c *canniMeter) pctLocked() int {
	if c.casts < 1 {
		return 0
	}

	// throughput: how close the cast rate is to the recast cap
	thru := 100
	if intervals := c.casts - 1; intervals >= 1 && c.edgeMs > 0 {
		elapsed := c.lastCast - c.danceStart
		if elapsed < 1 {
			elapsed = 1
		}
		thru = int(int64(intervals) * int64(c.edgeMs) * 100 / (elapsed * 1000))
		if thru > 100 {
			thru = 100
		}
	}

	// accuracy: successful casts out of total presses (buzzers = too-early misses)
	acc := c.casts * 100 / (c.casts + c.buzzers)

	return thru * acc / 100
}

// statsLocked returns the live dance readout, or an empty (inactive) value once
// the dance has lapsed. Caller holds the lock.
func (c *canniMeter) statsLocked(now int64) CanniStats {
	if c.lastCast == 0 {
		return CanniStats{} // never danced this session
	}
	if now-c.lastCast > canniDanceTimeoutSec {
		// the dance has lapsed — keep a muted summary (session best/score) so the
		// meter stays visible between dances. Reset on zone/character switch (clear).
		return CanniStats{Rank: c.rank, Score: c.score, Best: c.bestPct}
	}
	return CanniStats{
		Active:  true,
		Rank:    c.rank,
		EdgeMs:  c.edgeMs,
		Pct:     c.pctLocked(),
		Combo:   c.combo,
		Buzzers: c.buzzers,
		Score:   c.score,
		Best:    c.bestPct,
	}
}

// CanniStats returns the live dance readout, or an empty (inactive) value once
// you stop dancing for canniDanceTimeoutSec.
func (t *Tracker) CanniStats(now int64) CanniStats {
	if t == nil {
		return CanniStats{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.canni.statsLocked(now)
}
