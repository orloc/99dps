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
	mine   bool   // the player got the killing blow (vs a group/other kill)
	killer string // who landed the blow ("You" for the player's own)
}

// Respawn is one pending repop, for the panel.
type Respawn struct {
	Mob       string
	Remaining int64  // seconds until repop; <= 0 means it should be up now
	Mine      bool   // the player's own kill (sorted above others')
	Killer    string // who killed it ("You" for the player's own)
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
			t.zoneXpKills, t.zoneDeaths, t.zoneFirstKillAt = 0, 0, 0
		}
		return
	}

	// xp-credited kills drive the zone kills/hr (solo "You gain experience" and
	// grouped "You gain party experience" both count one credited kill).
	if strings.HasPrefix(body, "You gain experience") || strings.HasPrefix(body, "You gain party experience") {
		t.zoneXpKills++
		if t.zoneFirstKillAt == 0 {
			t.zoneFirstKillAt = at
		}
		return
	}
	if strings.HasPrefix(body, "You have been slain by") {
		t.zoneDeaths++
		return
	}

	// the player's own killing blow
	if mob, ok := strings.CutPrefix(body, "You have slain "); ok {
		t.recordKillLocked(strings.TrimSuffix(mob, "!"), at, true, "You")
		return
	}

	// anyone's kill: "<victim> has been slain by <killer>!" — covers group kills
	// where a groupmate lands the blow. Player deaths read as "<you> HAVE been
	// slain" (not "has"), so they don't match here; and when the killer is a mob
	// the victim is a player who died, so skip those.
	if i := strings.Index(body, " has been slain by "); i > 0 {
		victim := body[:i]
		killer := strings.TrimRight(body[i+len(" has been slain by "):], " !.")
		if !killerIsMob(killer) {
			t.recordKillLocked(victim, at, false, killer)
		}
	}
}

// recordKillLocked records a mob death. A per-(zone, mob) override wins over the
// zone default; no-op when neither yields a time.
//
// Same-name handling: if a prior entry for this mob has already repopped by now
// (its timer elapsed), this kill is that same spawn killed again — reuse the
// slot rather than leaving a stale entry behind. A still-pending same-name entry
// is a *distinct* live spawn, so we leave it alone and add a new one.
func (t *Tracker) recordKillLocked(mob string, at int64, mine bool, killer string) {
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
	exp := at + int64(sec)
	for i := range t.respawns {
		if t.respawns[i].mob == mob && t.respawns[i].expiry <= at {
			// same spawn, re-killed: reset time and attribute to this kill
			t.respawns[i].at, t.respawns[i].expiry = at, exp
			t.respawns[i].mine, t.respawns[i].killer = mine, killer
			return
		}
	}
	t.respawns = append(t.respawns, respawnEntry{mob: mob, at: at, expiry: exp, mine: mine, killer: killer})
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

// ZoneKillStats returns the zone-wide tallies at wall-clock `now`: xp-credited
// kills, the per-hour rate (over the time since the first kill), and deaths.
func (t *Tracker) ZoneKillStats(now int64) (kills, perHour, deaths int) {
	if t == nil {
		return 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	kills, deaths = t.zoneXpKills, t.zoneDeaths
	if kills > 0 && t.zoneFirstKillAt > 0 {
		span := now - t.zoneFirstKillAt
		if span < 1 {
			span = 1
		}
		perHour = kills * 3600 / int(span)
	}
	return kills, perHour, deaths
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
		out = append(out, Respawn{Mob: e.mob, Remaining: rem, Mine: e.mine, Killer: e.killer})
	}
	t.respawns = kept
	// my kills first, then others'; within each, soonest-to-repop first.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mine != out[j].Mine {
			return out[i].Mine
		}
		if out[i].Remaining != out[j].Remaining {
			return out[i].Remaining < out[j].Remaining
		}
		return out[i].Mob < out[j].Mob
	})
	return out
}
