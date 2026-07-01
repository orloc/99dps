package gamestate

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CH-chain analyzer. Clerics running a Complete Heal chain call their slot in
// guild chat — "<Caster> tells the guild, 'KCH - <Target> - <Slot>'" — once per
// cast, so a single guild member's log carries every cleric's call-out and thus
// the whole chain. Real raid logs show three things this must get right:
//
//   - CONCURRENT chains. Two (up to four observed) tanks get chained at once, so
//     their call-outs interleave in one log. Chains are therefore keyed by
//     TARGET and tracked simultaneously — never collapsed into a single "current"
//     chain (which would shatter each real chain at every interleaved call).
//   - SEPARATOR + SLOT variants. Most call "KCH - <tgt> - 003"; some use
//     "KCH --- <tgt> --- 009"; the slot may be a label ("GAP", "R11", "SPOT",
//     "RAMP") — a designated gap-filler/reserve, a real CH on the target — not
//     just a number. All count as chain events; "GAP" calls are also surfaced as
//     the clerics' own gap signal.
//   - PARTICIPANTS are clerics, not slot numbers. Keyed by caster name so a slot
//     reused by two clerics isn't miscounted.
//
// Cadence is derived per chain (it scales with stack size — a 16-cleric chain
// lands a CH every ~3s, a 7-cleric one every ~6s); a gap is an interval far past
// the cadence. Methods are caller-locked (the owning Tracker holds the mutex).
const (
	chChainEndSec   = 90 // no call-out for this long ends a chain (mob died / break)
	chChainMinCasts = 4  // a run shorter than this is incidental, not a chain
	chGapMultNum    = 5  // an interval past cadence × (5/2) = 2.5× ...
	chGapMultDen    = 2
	chGapPadSec     = 3 //   ... and at least cadence+3s, is a gap
	chCadenceMinN   = 3 // need this many intervals before cadence (and gaps) are trusted
	chHistoryCap    = 64
	chRecentGapsCap = 8
	chMaxConcurrent = 8 // safety bound on simultaneously-tracked targets
	chRosterCap     = 64
	chLapWindow     = 8 // per-cleric laps kept for Beat% — recent only, so a stack
	//                     size change (clerics dropping) can't retro-misjudge old laps
)

// kchRE matches "KCH - <target> - <slot>" tolerating one-or-more dashes as the
// separator, surrounding whitespace, and trailing spaces. The slot is any
// alphanumeric token (a number, or a label like GAP/R11/SPOT/RAMP).
var kchRE = regexp.MustCompile(`^KCH\s+-+\s+(.+?)\s+-+\s+([A-Za-z0-9]+)\s*$`)

// CHCleric is one cleric's standing in a chain.
type CHCleric struct {
	Name    string
	Slot    string // last-seen slot label ("003", "GAP", "R11", …)
	Casts   int
	AgoSec  int // seconds since this cleric's last call-out (at snapshot time)
	NextSec int // seconds until this cleric's next CH is due (negative = overdue); only when cadence known
	BeatPct int // % of this cleric's turns hit on-rhythm (0..100, -1 if not enough data)
	LagSec  int // seconds this cleric is overdue right now (0 = on time)
}

// CHGapInfo is a single detected gap in the chain.
type CHGapInfo struct {
	At        int64 // unix time the gap began (the call-out before the gap)
	Seconds   int
	AfterSlot string
}

// CHChainStats is the live readout for one chain.
type CHChainStats struct {
	Active       bool
	Target       string
	Clerics      int     // active chain positions right now (stale holders dropped)
	Casts        int     // total call-outs this chain
	Replacements int     // times a silent holder was subbed out
	GapCalls     int     // explicit "GAP" call-outs by clerics
	Cadence      float64 // derived seconds between CHs (0 until enough data)
	SinceLast    int     // seconds since the last call-out (the live gap)
	InGap        bool    // SinceLast past the gap threshold — chain visibly BROKEN
	GapForming   bool    // SinceLast past a lower "a CH is late" threshold — for the early audio cue
	GapThresh    int     // live gap threshold in seconds (0 until cadence trusted)
	Gaps         int     // interval gaps detected this chain
	WorstGap     int
	Start        int64
	LastAt       int64
	NextCaster   string // the cleric whose slot is next in the rotation ("" if unknown)
	Roster       []CHCleric
	RecentGaps   []CHGapInfo
}

// CHChainSummary is a finished chain, for the post-raid report.
type CHChainSummary struct {
	Target       string
	Start        int64
	End          int64
	Casts        int
	Clerics      int
	Replacements int
	GapCalls     int
	Cadence      float64
	Gaps         int
	WorstGap     int
}

// chSlot is the holder of one chain position (slot label). Clerics get subbed,
// so a slot's holder is whoever is currently calling that number; the prior
// holder is dropped once they've been silent at it for chDropSec.
type chSlot struct {
	caster string
	casts  int
	lastAt int64
	laps   []int // seconds between this holder's own consecutive casts (their rhythm)
}

type chGap struct {
	at        int64
	seconds   int
	afterSlot string
}

type chChain struct {
	target       string
	start        int64
	lastAt       int64
	casts        int
	gapCalls     int
	lastSlot     string
	lastNum      int // numeric value of the most recent numbered slot (for "who's next")
	replacements int // times a slot's silent holder was subbed out
	intervals    []int
	gaps         []chGap
	slots        map[string]*chSlot // keyed by slot label (position), holder = current cleric
}

// chDropSec: a cleric silent at their slot this long is dropped from the live
// roster and replaced by whoever next calls that number.
const chDropSec = 60

type chchainTracker struct {
	active map[string]*chChain // keyed by target — concurrent chains
	done   []CHChainSummary
}

func (c *chchainTracker) clear() { *c = chchainTracker{} }

// parseKCH pulls (caster, target, slot) from a
// "<Caster> tells the guild, 'KCH - <Target> - <Slot>'" line. The letter-coded
// "FFF - CH - x" variant is a different format and deliberately not matched.
func parseKCH(body string) (caster, target, slot string, ok bool) {
	const sep = " tells the guild, '"
	i := strings.Index(body, sep)
	if i <= 0 {
		return "", "", "", false
	}
	caster = body[:i]
	msg, ok := strings.CutSuffix(body[i+len(sep):], "'")
	if !ok {
		return "", "", "", false
	}
	m := kchRE.FindStringSubmatch(msg)
	if m == nil {
		return "", "", "", false
	}
	target, slot = strings.TrimSpace(m[1]), m[2]
	if target == "" {
		return "", "", "", false
	}
	return caster, target, slot, true
}

// observeLocked feeds one log line to the chain analyzer. Caller holds the lock.
func (c *chchainTracker) observeLocked(body string, at int64) {
	caster, target, slot, ok := parseKCH(body)
	if !ok {
		return
	}
	if c.active == nil {
		c.active = map[string]*chChain{}
	}
	c.sweepLocked(at) // finalize any chain that went quiet > chChainEndSec ago

	ch := c.active[target]
	if ch == nil {
		if len(c.active) >= chMaxConcurrent {
			return // pathological; ignore further concurrent targets
		}
		ch = &chChain{target: target, start: at, lastAt: at, slots: map[string]*chSlot{}}
		c.active[target] = ch
	} else {
		gap := int(at - ch.lastAt)
		// judge this interval against cadence from the intervals SO FAR (before
		// this one), so the gap being measured can't inflate its own baseline.
		if cad := chCadence(ch.intervals); cad > 0 {
			if gap > chGapThreshold(cad) {
				ch.gaps = append(ch.gaps, chGap{at: ch.lastAt, seconds: gap, afterSlot: ch.lastSlot})
			}
		}
		if gap > 0 {
			ch.intervals = append(ch.intervals, gap)
		}
	}
	ch.recordLocked(caster, slot, at)
}

func (ch *chChain) recordLocked(caster, slot string, at int64) {
	h := ch.slots[slot]
	switch {
	case h == nil:
		if len(ch.slots) < chRosterCap {
			ch.slots[slot] = &chSlot{caster: caster, casts: 1, lastAt: at}
		}
	case h.caster == caster:
		if at > h.lastAt {
			h.laps = append(h.laps, int(at-h.lastAt)) // this holder's own rhythm
			if len(h.laps) > chLapWindow {
				h.laps = h.laps[len(h.laps)-chLapWindow:] // keep only recent laps (current stack)
			}
		}
		h.casts++
		h.lastAt = at
	default: // a DIFFERENT cleric is calling this number
		if at-h.lastAt > chDropSec {
			// the holder went silent past a minute → subbed out; the new cleric takes
			// the slot with fresh stats. A holder who returns within the minute keeps it.
			ch.replacements++
			*h = chSlot{caster: caster, casts: 1, lastAt: at}
		}
		// else: holder still active (<chDropSec) — they keep the slot; this stray
		// same-number call still counts toward chain totals below (a CH did land).
	}
	ch.casts++
	ch.lastAt = at
	ch.lastSlot = slot
	if n, err := strconv.Atoi(slot); err == nil {
		ch.lastNum = n // advance the rotation position only on a numbered call
	}
	if slot == "GAP" {
		ch.gapCalls++
	}
}

// sweepLocked finalizes (into done) every active chain quiet for > chChainEndSec.
func (c *chchainTracker) sweepLocked(now int64) {
	for tgt, ch := range c.active {
		if now-ch.lastAt > chChainEndSec {
			if ch.casts >= chChainMinCasts {
				c.appendDoneLocked(ch.summary())
			}
			delete(c.active, tgt)
		}
	}
}

func (c *chchainTracker) appendDoneLocked(s CHChainSummary) {
	c.done = append(c.done, s)
	if len(c.done) > chHistoryCap {
		c.done = c.done[len(c.done)-chHistoryCap:]
	}
}

func (ch *chChain) summary() CHChainSummary {
	worst := 0
	for _, g := range ch.gaps {
		if g.seconds > worst {
			worst = g.seconds
		}
	}
	return CHChainSummary{
		Target:       ch.target,
		Start:        ch.start,
		End:          ch.lastAt,
		Casts:        ch.casts,
		Clerics:      len(ch.slots), // distinct chain positions used
		Replacements: ch.replacements,
		GapCalls:     ch.gapCalls,
		Cadence:      chCadence(ch.intervals),
		Gaps:         len(ch.gaps),
		WorstGap:     worst,
	}
}

// chCadence is the chain's steady-state seconds-between-CHs: the median of the
// "tight" intervals (within 1.5× the overall median), excluding big gaps and the
// initial spin-up. 0 until there's enough data.
func chCadence(intervals []int) float64 {
	if len(intervals) < chCadenceMinN {
		return 0
	}
	s := append([]int(nil), intervals...)
	sort.Ints(s)
	med := s[len(s)/2]
	tight := make([]int, 0, len(s))
	for _, v := range s {
		if float64(v) <= float64(med)*1.5 {
			tight = append(tight, v)
		}
	}
	if len(tight) == 0 {
		return float64(med)
	}
	return float64(tight[len(tight)/2])
}

// chGapForming is the earlier "a CH is late" threshold used for the audio cue —
// ~1.5× cadence (floor 4s) so the alert fires as soon as the chain slips, not
// only once it's fully broken (chGapThreshold).
func chGapForming(cadence float64) int {
	return max(int(cadence*2+0.5), 5)
}

// chGapCueCeil bounds the audio gap window: past this much silence the chain has
// effectively STOPPED (the mob died / the pull ended), not "slipped", so the cue
// shouldn't keep re-firing as it winds down. A recoverable gap is a few cadences
// (min 20s) — beyond that, further alerts are noise, not signal.
func chGapCueCeil(cadence float64) int {
	return max(int(cadence*4+0.5), 20)
}

// chGapThreshold is the seconds-since-last beyond which the chain is broken:
// max(cadence × 2.5, cadence + pad).
func chGapThreshold(cadence float64) int {
	a := cadence * chGapMultNum / chGapMultDen
	if b := cadence + chGapPadSec; b > a {
		a = b
	}
	return int(a + 0.5)
}

// slotKey parses a slot label for ordering: numeric slots by value (ahead of
// labels), labels after, alpha.
func slotKey(slot string) (int, string) {
	if n, err := strconv.Atoi(slot); err == nil {
		return n, ""
	}
	return 1 << 30, slot
}

func (ch *chChain) statsLocked(now int64) CHChainStats {
	cad := chCadence(ch.intervals)
	worst := 0
	for _, g := range ch.gaps {
		if g.seconds > worst {
			worst = g.seconds
		}
	}
	st := CHChainStats{
		Active:       now-ch.lastAt <= chChainEndSec,
		Target:       ch.target,
		Casts:        ch.casts,
		GapCalls:     ch.gapCalls,
		Replacements: ch.replacements,
		Cadence:      cad,
		SinceLast:    int(now - ch.lastAt),
		Gaps:         len(ch.gaps),
		WorstGap:     worst,
		Start:        ch.start,
		LastAt:       ch.lastAt,
	}
	if cad > 0 {
		st.GapThresh = chGapThreshold(cad)
		st.InGap = st.Active && st.SinceLast > st.GapThresh
		// GapForming (audio): a recoverable slip — past the "a CH is late" mark but
		// not so long the chain has clearly stopped (mob dead / pull over).
		st.GapForming = st.Active && st.SinceLast >= chGapForming(cad) && st.SinceLast <= chGapCueCeil(cad)
	}

	// the live roster is the CURRENT composition: slots whose holder has called
	// within chDropSec (stale holders are dropped). Sorted by slot.
	slots := make([]string, 0, len(ch.slots))
	for s, h := range ch.slots {
		if now-h.lastAt <= chDropSec {
			slots = append(slots, s)
		}
	}
	sort.Slice(slots, func(i, j int) bool {
		ai, as := slotKey(slots[i])
		bi, bs := slotKey(slots[j])
		if ai != bi {
			return ai < bi
		}
		return as < bs
	})
	st.Clerics = len(slots)

	// a cleric's expected rotation period (their "lap") ≈ cadence × active clerics.
	// their next CH is due idealLap after their last; being late by >1.5× is a miss.
	idealLap := 0
	if cad > 0 && st.Clerics > 0 {
		idealLap = int(cad*float64(st.Clerics) + 0.5)
	}
	lateLap := float64(idealLap) * 1.5

	for _, s := range slots {
		h := ch.slots[s]
		c := CHCleric{Name: h.caster, Slot: s, Casts: h.casts, AgoSec: int(now - h.lastAt), BeatPct: -1}
		if idealLap > 0 {
			c.NextSec = idealLap - c.AgoSec // countdown to their next turn (negative = overdue)
		}
		if lateLap > 0 {
			if len(h.laps) > 0 {
				onBeat := 0
				for _, lap := range h.laps {
					if float64(lap) <= lateLap {
						onBeat++
					}
				}
				c.BeatPct = onBeat * 100 / len(h.laps)
			}
			if over := float64(c.AgoSec) - lateLap; over > 0 {
				c.LagSec = int(over + 0.5)
			}
		}
		st.Roster = append(st.Roster, c)
	}
	st.NextCaster = nextCaster(slots, ch.slots, ch.lastNum)

	for i := len(ch.gaps) - 1; i >= 0 && len(st.RecentGaps) < chRecentGapsCap; i-- {
		g := ch.gaps[i]
		st.RecentGaps = append(st.RecentGaps, CHGapInfo{At: g.at, Seconds: g.seconds, AfterSlot: g.afterSlot})
	}
	return st
}

// nextCaster predicts whose turn is next: the holder of the smallest numbered
// active slot strictly greater than the last numbered call, wrapping to the
// lowest. activeSlots is already sorted ascending. "" if none are numbered.
func nextCaster(activeSlots []string, slots map[string]*chSlot, lastNum int) string {
	var lowest, next string
	lowestN, nextN := 0, 0
	for _, s := range activeSlots {
		n, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		if lowest == "" || n < lowestN {
			lowest, lowestN = s, n
		}
		if n > lastNum && (next == "" || n < nextN) {
			next, nextN = s, n
		}
	}
	pick := next
	if pick == "" {
		pick = lowest
	}
	if pick == "" {
		return ""
	}
	return slots[pick].caster
}

// CHActiveChains returns the live readouts for every chain with a call-out
// within chChainEndSec, busiest first. Empty if no chain is running.
func (t *Tracker) CHActiveChains(now int64) []CHChainStats {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []CHChainStats
	for _, ch := range t.chain.active {
		if now-ch.lastAt <= chChainEndSec && ch.casts >= 1 {
			out = append(out, ch.statsLocked(now))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Casts != out[j].Casts {
			return out[i].Casts > out[j].Casts
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// CHChainHistory returns completed chains this session (oldest first), plus any
// still-tracked chains that are already substantial.
func (t *Tracker) CHChainHistory() []CHChainSummary {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]CHChainSummary(nil), t.chain.done...)
	// include currently-tracked chains (ordered by start) so a just-finished raid
	// still reports its last pulls before the next call-out sweeps them.
	var live []*chChain
	for _, ch := range t.chain.active {
		if ch.casts >= chChainMinCasts {
			live = append(live, ch)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].start < live[j].start })
	for _, ch := range live {
		out = append(out, ch.summary())
	}
	return out
}
