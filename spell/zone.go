package spell

import (
	"sort"
	"strings"

	"99dps/common"
)

// respawnKeepUpSec keeps a repopped mob listed (shown as "up") this long after
// its timer elapses, then drops it so the list doesn't grow without bound.
const respawnKeepUpSec = 120

// respawnEntry is one killed mob's pending repop. Kills are stored as a list,
// not keyed by name: two same-named mobs dying close together are distinct
// spawns and each get their own timer.
type respawnEntry struct {
	mob    string
	at     int64 // kill time, kept so an override can recompute expiry
	expiry int64
}

// Respawn is one pending repop, for the panel.
type Respawn struct {
	Mob       string
	Remaining int64 // seconds until repop; <= 0 means it should be up now
}

// observeZoneLocked tracks zone-in lines and mob deaths to drive the zone-aware
// respawn list. Caller holds the lock.
func (t *Tracker) observeZoneLocked(body string, at int64) {
	if z, ok := strings.CutPrefix(body, "You have entered "); ok {
		z = strings.TrimSuffix(z, ".")
		if z != t.zone {
			t.zone = z
			t.zoneRespawnSec, _ = common.ZoneRespawn(z)
			t.respawns = nil // left the zone — old repops are moot
		}
		return
	}

	// the player's own killing blow
	if mob, ok := strings.CutPrefix(body, "You have slain "); ok {
		t.recordKillLocked(strings.TrimSuffix(mob, "!"), at)
		return
	}

	// anyone's kill: "<victim> has been slain by <killer>!" — covers group kills
	// where a groupmate lands the blow. Player deaths read as "<you> HAVE been
	// slain" (not "has"), so they don't match here; and when the killer is a mob
	// the victim is a player who died, so skip those.
	if i := strings.Index(body, " has been slain by "); i > 0 {
		victim := body[:i]
		killer := body[i+len(" has been slain by "):]
		if !killerIsMob(killer) {
			t.recordKillLocked(victim, at)
		}
	}
}

// recordKillLocked appends a repop timer for a mob death (one entry per death —
// see respawnEntry). A per-(zone, mob) override wins over the zone default;
// no-op when neither yields a time.
func (t *Tracker) recordKillLocked(mob string, at int64) {
	if mob == "" {
		return
	}
	sec := t.zoneRespawnSec
	if o, ok := t.overrides.Get(t.zone, mob); ok {
		sec = o
	}
	if sec <= 0 {
		return
	}
	t.respawns = append(t.respawns, respawnEntry{mob: mob, at: at, expiry: at + int64(sec)})
}

// UseOverrides attaches the persisted respawn-override store (called at wiring).
func (t *Tracker) UseOverrides(o *Overrides) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.overrides = o
	t.mu.Unlock()
}

// SetOverride sets a respawn override (seconds) for a mob in the current zone,
// persists it, and retroactively updates that mob's live timers. sec <= 0 clears
// the override and reverts live timers to the zone default.
func (t *Tracker) SetOverride(mob string, sec int) {
	if t == nil || mob == "" {
		return
	}
	t.mu.Lock()
	zone := t.zone
	eff := sec
	if eff <= 0 {
		eff = t.zoneRespawnSec
	}
	if eff > 0 {
		for i := range t.respawns {
			if t.respawns[i].mob == mob {
				t.respawns[i].expiry = t.respawns[i].at + int64(eff)
			}
		}
	}
	o := t.overrides
	t.mu.Unlock()

	if o != nil && zone != "" {
		_ = o.Set(zone, mob, sec)
	}
}

// killerIsMob reports whether the "slain by X" killer is a mob (lowercase / an
// "a"/"an"/"the" article) rather than a player. Used to skip player deaths,
// whose lines read "<player> has been slain by <a mob>".
func killerIsMob(killer string) bool {
	killer = strings.TrimRight(killer, " !.")
	if killer == "" {
		return false
	}
	lower := strings.ToLower(killer)
	if strings.HasPrefix(lower, "a ") || strings.HasPrefix(lower, "an ") || strings.HasPrefix(lower, "the ") {
		return true
	}
	c := killer[0]
	return c >= 'a' && c <= 'z' // a leading lowercase letter ⇒ a mob (players are capitalized)
}

// Zone returns the player's current zone as logged, or "" until a zone-in is
// seen.
func (t *Tracker) Zone() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.zone
}

// ZoneKnown reports whether the current zone has a known respawn timer.
func (t *Tracker) ZoneKnown() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.zoneRespawnSec > 0
}

// Respawns returns pending repops for mobs killed in the current zone, soonest
// first. Entries past their repop read as up (Remaining <= 0) and are purged
// once they've been up longer than respawnKeepUpSec.
func (t *Tracker) Respawns(now int64) []Respawn {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	kept := t.respawns[:0]
	out := make([]Respawn, 0, len(t.respawns))
	for _, e := range t.respawns {
		rem := e.expiry - now
		if rem < -respawnKeepUpSec {
			continue // long past up — drop it
		}
		kept = append(kept, e)
		out = append(out, Respawn{Mob: e.mob, Remaining: rem})
	}
	t.respawns = kept
	sort.Slice(out, func(i, j int) bool {
		if out[i].Remaining != out[j].Remaining {
			return out[i].Remaining < out[j].Remaining
		}
		return out[i].Mob < out[j].Mob
	})
	return out
}
