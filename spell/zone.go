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

// zoneTracker is the zone-awareness subsystem: the current zone, its default
// respawn, the pending mob-repop list, per-(zone,mob) overrides, and the
// zone-wide xp-kill/death tallies. Its methods are caller-locked (the owning
// Tracker holds the mutex), matching the rest of the package.
type zoneTracker struct {
	name       string         // current zone (from a "You have entered" line)
	respawnSec int            // current zone's default respawn, 0 if unknown
	respawns   []respawnEntry // pending mob repops (one entry per death)
	overrides  *Overrides     // persisted per-(zone,mob) respawn overrides

	// reset on a zone change: xp-credited kills, player deaths, and the log time
	// of the first xp kill (the kills/hr denominator).
	xpKills     int
	deaths      int
	firstKillAt int64
}

func (z *zoneTracker) clear() {
	z.name, z.respawnSec, z.respawns = "", 0, nil
	z.xpKills, z.deaths, z.firstKillAt = 0, 0, 0
}

// observeLocked tracks zone-in lines and mob deaths to drive the zone-aware
// respawn list. Caller holds the lock.
func (z *zoneTracker) observeLocked(body string, at int64) {
	if zn, ok := strings.CutPrefix(body, "You have entered "); ok {
		zn = strings.TrimSuffix(zn, ".")
		if zn != z.name {
			z.name = zn
			z.respawnSec, _ = common.ZoneRespawn(zn)
			z.respawns = nil // left the zone — old repops are moot
			z.xpKills, z.deaths, z.firstKillAt = 0, 0, 0
		}
		return
	}

	// xp-credited kills drive the zone kills/hr (solo "You gain experience" and
	// grouped "You gain party experience" both count one credited kill).
	if strings.HasPrefix(body, "You gain experience") || strings.HasPrefix(body, "You gain party experience") {
		z.xpKills++
		if z.firstKillAt == 0 {
			z.firstKillAt = at
		}
		return
	}
	if strings.HasPrefix(body, "You have been slain by") {
		z.deaths++
		return
	}

	// the player's own killing blow
	if mob, ok := strings.CutPrefix(body, "You have slain "); ok {
		z.recordKillLocked(strings.TrimSuffix(mob, "!"), at, true, "You")
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
			z.recordKillLocked(victim, at, false, killer)
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
func (z *zoneTracker) recordKillLocked(mob string, at int64, mine bool, killer string) {
	if mob == "" {
		return
	}
	sec := z.respawnSec
	if o, ok := z.overrides.Get(z.name, mob); ok {
		sec = o
	}
	if sec <= 0 {
		return
	}
	exp := at + int64(sec)
	for i := range z.respawns {
		if z.respawns[i].mob == mob && z.respawns[i].expiry <= at {
			// same spawn, re-killed: reset time and attribute to this kill
			z.respawns[i].at, z.respawns[i].expiry = at, exp
			z.respawns[i].mine, z.respawns[i].killer = mine, killer
			return
		}
	}
	z.respawns = append(z.respawns, respawnEntry{mob: mob, at: at, expiry: exp, mine: mine, killer: killer})
}

// setOverrideLocked updates the live timers for a mob to a new respawn (or the
// zone default when sec <= 0) and returns the (zone, store) the caller should
// persist to after releasing the lock. Caller holds the lock.
func (z *zoneTracker) setOverrideLocked(mob string, sec int) (string, *Overrides) {
	eff := sec
	if eff <= 0 {
		eff = z.respawnSec
	}
	if eff > 0 {
		for i := range z.respawns {
			if z.respawns[i].mob == mob {
				z.respawns[i].expiry = z.respawns[i].at + int64(eff)
			}
		}
	}
	return z.name, z.overrides
}

// killStatsLocked returns the zone-wide tallies at wall-clock `now`. Caller holds the lock.
func (z *zoneTracker) killStatsLocked(now int64) (kills, perHour, deaths int) {
	kills, deaths = z.xpKills, z.deaths
	if kills > 0 && z.firstKillAt > 0 {
		span := now - z.firstKillAt
		if span < 1 {
			span = 1
		}
		perHour = kills * 3600 / int(span)
	}
	return kills, perHour, deaths
}

// respawnsLocked returns pending repops, soonest first, purging long-dead
// entries. Caller holds the lock.
func (z *zoneTracker) respawnsLocked(now int64) []Respawn {
	kept := z.respawns[:0]
	out := make([]Respawn, 0, len(z.respawns))
	for _, e := range z.respawns {
		rem := e.expiry - now
		if rem < -respawnKeepUpSec {
			continue // long past up — drop it
		}
		kept = append(kept, e)
		out = append(out, Respawn{Mob: e.mob, Remaining: rem, Mine: e.mine, Killer: e.killer})
	}
	z.respawns = kept
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

// --- Tracker facade: lock the shared mutex, delegate to the zone subsystem ---

// UseOverrides attaches the persisted respawn-override store (called at wiring).
func (t *Tracker) UseOverrides(o *Overrides) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.zone.overrides = o
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
	zone, o := t.zone.setOverrideLocked(mob, sec)
	t.mu.Unlock()

	if o != nil && zone != "" {
		_ = o.Set(zone, mob, sec)
	}
}

// ZoneKillStats returns the zone-wide tallies at wall-clock `now`: xp-credited
// kills, the per-hour rate (over the time since the first kill), and deaths.
func (t *Tracker) ZoneKillStats(now int64) (kills, perHour, deaths int) {
	if t == nil {
		return 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.zone.killStatsLocked(now)
}

// Zone returns the player's current zone as logged, or "" until a zone-in is
// seen.
func (t *Tracker) Zone() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.zone.name
}

// ZoneKnown reports whether the current zone has a known respawn timer.
func (t *Tracker) ZoneKnown() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.zone.respawnSec > 0
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
	return t.zone.respawnsLocked(now)
}
