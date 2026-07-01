package gamestate

import "sort"

// Raid-target debuff board. In a raid, players call mob debuffs in guild/group
// chat via macros — "SLOWED - Vulak`Aerr", "TASHED - Gozzrem", "OOS - Zlandicar".
// This records, per raid target, which debuffs are up, who last called each, and
// the remaining estimate (from the same family ceilings enemydebuff.go uses), so
// the Raid tab can show what's covered and what's about to fall off. Methods are
// caller-locked (the owning Tracker holds the mutex).
const (
	raidTargetTTLSec       = 900 // a target with no debuff call this long drops off the board
	raidDebuffCapPerTarget = 16
	raidTargetCap          = 24
)

// raidCounterDebuffs are stacking calls that are COUNTED, not timed — each call
// is another stack (sieves drain a chunk of the mob's mana per cast), so the
// tally matters, not a countdown.
var raidCounterDebuffs = map[string]bool{"Sieved": true}

// RaidDebuffInfo is one debuff on a raid target. A counter debuff (e.g. Sieved)
// reports Count instead of RemainSec.
type RaidDebuffInfo struct {
	Label     string // "Slowed", "Tashed", "Malo'd", "OOS", "Sieved", …
	Caller    string // who last called it
	RemainSec int    // estimated seconds left (≤0 = lapsed → needs a recast)
	Count     int    // stacks so far (counter debuffs only; 0 = it's a timer)
}

// RaidTarget is one mob and its currently-called debuffs.
type RaidTarget struct {
	Name    string
	AgoSec  int // seconds since any debuff was last called on this target
	Debuffs []RaidDebuffInfo
}

type raidDebuff struct {
	caller string
	expiry int64
	count  int // stacks (counter debuffs like Sieved)
}

type raidTarget struct {
	name    string
	lastAt  int64
	debuffs map[string]*raidDebuff
}

type raidTracker struct {
	targets map[string]*raidTarget
}

func (r *raidTracker) clear() { *r = raidTracker{} }

// recordLocked logs a macro debuff call on a mob. Caller holds the lock.
func (r *raidTracker) recordLocked(label, target, caller string, durSec int, at int64) {
	if r.targets == nil {
		r.targets = map[string]*raidTarget{}
	}
	tg := r.targets[target]
	if tg == nil {
		if len(r.targets) >= raidTargetCap {
			r.evictStalestLocked()
		}
		tg = &raidTarget{name: target, debuffs: map[string]*raidDebuff{}}
		r.targets[target] = tg
	}
	tg.lastAt = at
	d := tg.debuffs[label]
	if d == nil {
		if len(tg.debuffs) >= raidDebuffCapPerTarget {
			return
		}
		d = &raidDebuff{}
		tg.debuffs[label] = d
	}
	d.caller = caller
	if raidCounterDebuffs[label] {
		d.count++ // each call is another stack, not a refresh
	} else {
		d.expiry = at + int64(durSec)
	}
}

// evictStalestLocked drops the least-recently-active target to make room.
func (r *raidTracker) evictStalestLocked() {
	var oldest string
	var oldestAt int64 = 1 << 62
	for name, tg := range r.targets {
		if tg.lastAt < oldestAt {
			oldest, oldestAt = name, tg.lastAt
		}
	}
	if oldest != "" {
		delete(r.targets, oldest)
	}
}

// targetsLocked returns the live board: targets active within raidTargetTTLSec,
// most-recently-active first, each with its debuffs sorted by label.
func (r *raidTracker) targetsLocked(now int64) []RaidTarget {
	var out []RaidTarget
	for _, tg := range r.targets {
		if now-tg.lastAt > raidTargetTTLSec {
			continue
		}
		rt := RaidTarget{Name: tg.name, AgoSec: int(now - tg.lastAt)}
		labels := make([]string, 0, len(tg.debuffs))
		for l := range tg.debuffs {
			labels = append(labels, l)
		}
		sort.Strings(labels)
		for _, l := range labels {
			d := tg.debuffs[l]
			info := RaidDebuffInfo{Label: l, Caller: d.caller}
			if raidCounterDebuffs[l] {
				info.Count = d.count
			} else {
				info.RemainSec = int(d.expiry - now)
			}
			rt.Debuffs = append(rt.Debuffs, info)
		}
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgoSec != out[j].AgoSec {
			return out[i].AgoSec < out[j].AgoSec
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// RaidDebuffTargets returns the raid-target debuff board (empty if nothing called).
func (t *Tracker) RaidDebuffTargets(now int64) []RaidTarget {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.raid.targetsLocked(now)
}
