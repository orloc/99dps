package gamestate

import (
	"sort"
	"strings"
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
	mine   bool   // the player landed the killing blow
	group  bool   // a group kill — you got xp for it (your own blow, or a group-mate's)
	killer string // who landed the blow ("You" for the player's own)
}

// Respawn is one pending repop, for the panel.
type Respawn struct {
	Mob       string
	Remaining int64  // seconds until repop; <= 0 means it should be up now
	Mine      bool   // the player landed the killing blow (bold)
	Group     bool   // a group kill (you got xp) — sorted above others'
	Killer    string // who killed it ("You" for the player's own)
}

// groupXPWindowSec is how far back a "You gain (party) experience" line will look
// to credit a just-killed mob as a group kill (the xp line trails the blow).
const groupXPWindowSec = 8

// zoneTracker is the zone-awareness subsystem: the current zone, its default
// respawn, the pending mob-repop list, per-(zone,mob) overrides, and the
// zone-wide xp-kill/death tallies. Its methods are caller-locked (the owning
// Tracker holds the mutex), matching the rest of the package.
type zoneTracker struct {
	name       string         // current zone (from a "You have entered" line)
	respawnSec int            // current zone's default respawn, 0 if unknown
	respawns   []respawnEntry // pending mob repops (one entry per death)
	overrides  *Overrides     // persisted per-(zone,mob) respawn overrides

	// reset on a zone change: xp-credited kills (running total, for the count),
	// player deaths, the log time of the first xp kill, and the times of the last
	// hour's kills (the rolling kills/hr window — see killRateWindowSec).
	xpKills     int
	deaths      int
	firstKillAt int64
	xpKillTimes []int64

	// lastOwnKillAt is when you (or your pet) last landed a killing blow. A solo
	// "You gain experience" only counts as a kill when one landed just before it —
	// otherwise the xp is a quest turn-in / other non-kill source (see the xp
	// branch in observeLocked).
	lastOwnKillAt int64
}

// killRateWindowSec is the rolling window for kills/hr: the rate reflects your
// pace over the last hour (or the whole session if shorter), not a flat average
// since the first kill — so old downtime doesn't drag the number down.
const killRateWindowSec = 3600

// killXPWindowSec is how recently one of your own kills must have landed for a
// solo "You gain experience" to count as a kill rather than a quest turn-in.
// The death line and its xp land in the same combat resolution (~1s apart);
// turn-ins have no preceding own-kill, so a few seconds cleanly separates them.
const killXPWindowSec = 6

func (z *zoneTracker) clear() {
	z.name, z.respawnSec, z.respawns = "", 0, nil
	z.xpKills, z.deaths, z.firstKillAt, z.xpKillTimes = 0, 0, 0, nil
	z.lastOwnKillAt = 0
}

// observeLocked tracks zone-in lines and mob deaths to drive the zone-aware
// respawn list. petName is the player's own pet (or "" if unknown), so a kill its
// pet lands is credited to the player rather than read as a stranger's. Caller
// holds the lock.
// observeLocked folds one log line into the zone state. inferredVictim is the
// single mob we're currently debuffing (or ""), used to attribute an xp-only kill
// (no slain line) to it; the function returns that victim when it inferred such a
// kill, so the caller can clear the mob's timers. All other lines return "".
func (z *zoneTracker) observeLocked(body string, at int64, petName, inferredVictim string) string {
	if zn, ok := strings.CutPrefix(body, "You have entered "); ok {
		zn = strings.TrimSuffix(zn, ".")
		if zn != z.name {
			z.name = zn
			z.respawnSec, _ = ZoneRespawn(zn)
			z.respawns = nil // left the zone — old repops are moot
			z.xpKills, z.deaths, z.firstKillAt, z.xpKillTimes = 0, 0, 0, nil
			z.lastOwnKillAt = 0
		}
		return ""
	}

	// xp-credited kills drive the zone kills/hr. A solo "You gain experience" only
	// counts when one of your (or your pet's) kills landed just before it —
	// otherwise it's a quest turn-in or other non-kill xp (e.g. Chardok goblin-skin
	// turn-ins, which would otherwise inflate the count massively). Party xp always
	// reflects a group kill (turn-ins don't grant party experience), so it counts
	// regardless.
	party := strings.HasPrefix(body, "You gain party experience")
	solo := strings.HasPrefix(body, "You gain experience")
	if party || solo {
		inferred := ""
		if solo && !party && (z.lastOwnKillAt == 0 || at-z.lastOwnKillAt > killXPWindowSec) {
			// solo xp with no own/pet slain line just before it. If we were debuffing
			// a single mob, that mob died (a DoT or charmed-pet kill EQ logged only as
			// xp) — record its repop and report it so its timers clear. With no such
			// mob it's non-kill xp (a quest turn-in) — skip it as before.
			if inferredVictim == "" {
				return ""
			}
			z.recordKillLocked(inferredVictim, at, true, "You")
			inferred = inferredVictim
		}
		z.xpKills++
		if z.firstKillAt == 0 {
			z.firstKillAt = at
		}
		// keep the last hour of kill times for the rolling rate (log order is
		// monotonic, so old entries are a leading prefix to drop)
		z.xpKillTimes = append(z.xpKillTimes, at)
		cutoff := at - killRateWindowSec
		drop := 0
		for drop < len(z.xpKillTimes) && z.xpKillTimes[drop] < cutoff {
			drop++
		}
		z.xpKillTimes = z.xpKillTimes[drop:]
		z.creditGroupKillLocked(at) // the mob we just got xp for is a group kill
		return inferred
	}
	if strings.HasPrefix(body, "You have been slain by") {
		z.deaths++
		return ""
	}

	// the player's own killing blow
	if mob, ok := strings.CutPrefix(body, "You have slain "); ok {
		z.recordKillLocked(strings.TrimSuffix(mob, "!"), at, true, "You")
		return ""
	}

	// anyone's kill: "<victim> has been slain by <killer>!" — covers group kills
	// where a groupmate lands the blow. Player deaths read as "<you> HAVE been
	// slain" (not "has"), so they don't match here; and when the killer is a mob
	// the victim is a player who died, so skip those.
	if i := strings.Index(body, " has been slain by "); i > 0 {
		victim := body[:i]
		killer := strings.TrimRight(body[i+len(" has been slain by "):], " !.")
		// your own pet's killing blow is YOUR kill: credit it to you (mine, "You"),
		// not the pet. Check this BEFORE killerIsMob — a charmed pet keeps its
		// mob-style name ("a gnoll pup"), which killerIsMob would otherwise treat
		// as a player death and drop, costing the kill its repop timer.
		if petName != "" && strings.EqualFold(killer, petName) {
			z.recordKillLocked(victim, at, true, "You")
			return ""
		}
		if killerIsMob(killer) {
			return "" // a player died to a mob — not a kill we track
		}
		z.recordKillLocked(victim, at, false, killer)
	}
	return ""
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
	if mine {
		z.lastOwnKillAt = at // arms the solo-xp kill credit (see the xp branch)
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
			z.respawns[i].group = mine // a group-mate's kill is credited later by xp
			return
		}
	}
	// your own blow is a group kill immediately; a group-mate's becomes one when
	// the following "You gain party experience" credits it (creditGroupKillLocked).
	z.respawns = append(z.respawns, respawnEntry{mob: mob, at: at, expiry: exp, mine: mine, group: mine, killer: killer})
}

// creditGroupKillLocked marks the most-recent recently-killed mob as a group
// kill — called when a "You gain (party) experience" line lands, since the mob
// that gave the xp belongs in the group's repops even if a group-mate (not you)
// landed the blow. Caller holds the lock.
func (z *zoneTracker) creditGroupKillLocked(at int64) {
	best := -1
	for i := range z.respawns {
		if z.respawns[i].group || at-z.respawns[i].at > groupXPWindowSec {
			continue
		}
		if best < 0 || z.respawns[i].at > z.respawns[best].at {
			best = i
		}
	}
	if best >= 0 {
		z.respawns[best].group = true
	}
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
		// rate over the last hour (or the whole session if shorter): count the kills
		// inside the window and divide by the window's actual length.
		windowStart := now - killRateWindowSec
		if z.firstKillAt > windowStart {
			windowStart = z.firstKillAt
		}
		recent := 0
		for _, ts := range z.xpKillTimes {
			if ts >= windowStart {
				recent++
			}
		}
		dur := now - windowStart
		if dur < 1 {
			dur = 1
		}
		perHour = recent * 3600 / int(dur)
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
		out = append(out, Respawn{Mob: e.mob, Remaining: rem, Mine: e.mine, Group: e.group, Killer: e.killer})
	}
	z.respawns = kept
	// group kills (yours + xp-credited) first, then others'; within each, soonest first.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group
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
