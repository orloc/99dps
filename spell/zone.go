package spell

import (
	"sort"
	"strings"

	"99dps/common"
)

// respawnKeepUpSec keeps a repopped mob listed (shown as "up") this long after
// its timer elapses, then drops it so the list doesn't grow without bound.
const respawnKeepUpSec = 120

// Respawn is one killed mob's pending repop, for the panel.
type Respawn struct {
	Mob       string
	Remaining int64 // seconds until repop; <= 0 means it should be up now
}

// observeZoneLocked tracks zone-in lines and the player's kills to drive the
// zone-aware respawn list. Caller holds the lock.
func (t *Tracker) observeZoneLocked(body string, at int64) {
	if z, ok := strings.CutPrefix(body, "You have entered "); ok {
		z = strings.TrimSuffix(z, ".")
		if z != t.zone {
			t.zone = z
			t.zoneRespawnSec, _ = common.ZoneRespawn(z)
			t.respawns = make(map[string]int64) // left the zone — old repops are moot
		}
		return
	}
	// "You have slain <mob>!" — the player's own kill. Start a repop timer at the
	// zone's default respawn (re-killing the same name resets it).
	if mob, ok := strings.CutPrefix(body, "You have slain "); ok && t.zoneRespawnSec > 0 {
		mob = strings.TrimSuffix(mob, "!")
		t.respawns[mob] = at + int64(t.zoneRespawnSec)
	}
}

// Zone returns the player's current zone as logged, or "" until a zone-in is
// seen. ZoneKnown reports whether that zone has a respawn timer in the table.
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

	out := make([]Respawn, 0, len(t.respawns))
	for mob, exp := range t.respawns {
		rem := exp - now
		if rem < -respawnKeepUpSec {
			delete(t.respawns, mob)
			continue
		}
		out = append(out, Respawn{Mob: mob, Remaining: rem})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Remaining != out[j].Remaining {
			return out[i].Remaining < out[j].Remaining
		}
		return out[i].Mob < out[j].Mob
	})
	return out
}
