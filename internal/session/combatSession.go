package session

import (
	"99dps/internal/combat"
	"maps"
	"sort"
	"strings"
	"time"
)

type CombatSession struct {
	start      time.Time
	end        time.Time
	lastTime   int64
	aggressors map[string]combat.DamageStat
	// offense is keyed by attacker (raw name, matching DamageSet.Dealer) and
	// only counts hits and misses — an attacker's accuracy. Defensive outcomes
	// (dodge/parry/block/absorb) are the defender's doing and must not affect
	// the attacker's hit ratio. defense is keyed by defender (normalized,
	// matching DamageSet.Target) and faces every outcome.
	offense map[string]combat.SwingStats
	defense map[string]combat.SwingStats
	// crits is keyed by attacker (raw name); counts/sums critical hits.
	crits map[string]combat.CritStat

	// targets sums melee damage dealt *to* each target (raw target name), so
	// Name() can pick the heaviest enemy without retaining every hit.
	targets map[string]int

	// skills tallies the *player's* activated melee skills (Backstab/Bash/Kick),
	// keyed by canonical skill name, for the class-aware skills panel. Only the
	// player's skills are tracked (it's their own breakdown).
	skills map[string]combat.SkillStat

	// specials breaks every combatant's activated skills out per kind:
	// specials[dealer][kind] (dealer = raw name; kind = Backstab/Bash/Kick). Landed
	// hits + damage come from damage lines, misses from swing lines, so the Damage
	// panel can show a per-dealer, per-kind damage / share / hit-rate breakdown.
	specials map[string]map[string]combat.SpecialStat

	// session bookkeeping from kill / xp / death lines.
	kills   int // your killing blows
	xpGains int // mobs you were credited xp for
	deaths  int // times you were slain

	// magicTotal sums unattributed non-melee (spell/proc/DoT) damage on enemies.
	// EQ logs carry no caster, so spell damage stays a lump rather than a
	// per-spell split (the spell timer panel handles per-spell tracking).
	magicTotal int
}

// Defender pairs a combatant with its defensive swing tally, for display.
type Defender struct {
	Name  string
	Stats combat.SwingStats
}

// liveMobs estimates how many engaged enemies this fight has NOT yet killed:
// distinct damage-targets (mobs — "YOU" excluded) minus credited deaths (your
// killing blows, or xp / party-xp). While it's > 0 the pull isn't over — a mob
// is still up — so a lull shouldn't end the session (you're "still killing them,"
// no xp yet). Same-named mobs collapse to one target, the irreducible EQ ambiguity.
// Caller holds the lock.
func (cs *CombatSession) liveMobs() int {
	enemies := 0
	for name := range cs.targets { // damage targets are mobs (or the player, "YOU")
		if !strings.EqualFold(name, "You") {
			enemies++
		}
	}
	dead := cs.kills
	if cs.xpGains > dead {
		dead = cs.xpGains
	}
	if n := enemies - dead; n > 0 {
		return n
	}
	return 0
}

// reengages reports whether this exchange returns to a MOB the fight already
// damaged — so a lull is the same pull continuing, not a new one. It matches only
// damage-targets (mobs), never dealers, so a pet or group-mate (which appears as a
// dealer, never a target) can't falsely bridge unrelated fights. Player skipped.
// Caller holds the lock.
func (cs *CombatSession) reengages(combatants []string) bool {
	for _, name := range combatants {
		if name == "" || strings.EqualFold(name, "You") {
			continue
		}
		// case-insensitive: EQ writes "a drolvarg snarler" when you hit it (a
		// target) but "A drolvarg snarler" when it hits you (a dealer).
		for tgt := range cs.targets {
			if strings.EqualFold(tgt, name) {
				return true
			}
		}
	}
	return false
}

// adjustDamageLocked applies one event. Caller must hold the SessionManager
// write lock.
func (cs *CombatSession) adjustDamageLocked(set *combat.DamageSet) {
	cs.lastTime = set.ActionTime

	// a landed damage line is also a connecting swing
	cs.recordOutcome(set.Dealer, set.Target, combat.OutcomeHit)

	// sum damage per target for Name()
	if set.Target != "" {
		if cs.targets == nil {
			cs.targets = make(map[string]int)
		}
		cs.targets[set.Target] += set.Dmg
	}

	indxRef := strings.ReplaceAll(set.Dealer, " ", "_")
	val, exists := cs.aggressors[indxRef]
	if !exists {
		val.Dealer = set.Dealer
		val.FirstTime = set.ActionTime
	}
	val.Total += set.Dmg
	val.LastTime = set.ActionTime
	val.Hits++
	if kind := specialKind(set.Verb); kind != "" {
		val.SpecialTotal += set.Dmg
		val.SpecialHits++
		cs.addSpecialLocked(set.Dealer, kind, func(s *combat.SpecialStat) {
			s.Total += set.Dmg
			s.Hits++
		})
	}
	// the player's own skill breakdown feeds the skills panel
	if strings.EqualFold(set.Dealer, "You") {
		if sk := playerSkill(set.Verb); sk != "" {
			if cs.skills == nil {
				cs.skills = make(map[string]combat.SkillStat)
			}
			s := cs.skills[sk]
			s.Total += set.Dmg
			s.Hits++
			cs.skills[sk] = s
		}
	}
	cs.aggressors[indxRef] = val
}

// specialKind maps a verb to its canonical activated-skill kind
// (Backstab/Bash/Kick) for any combatant, or "" for an auto-attack. These feed
// the per-dealer Specials tally. (Unlike playerSkill, "strike"/"punch" aren't
// universal specials, so they're not bucketed here.)
func specialKind(verb string) string {
	switch v := strings.ToLower(verb); {
	case strings.HasPrefix(v, "backstab"):
		return "Backstab"
	case strings.HasPrefix(v, "bash"):
		return "Bash"
	case strings.HasPrefix(v, "kick"):
		return "Kick"
	}
	return ""
}

// addSpecialLocked mutates the per-dealer, per-kind special tally in place,
// lazily allocating the nested maps. Caller holds the write lock.
func (cs *CombatSession) addSpecialLocked(dealer, kind string, fn func(*combat.SpecialStat)) {
	if cs.specials == nil {
		cs.specials = make(map[string]map[string]combat.SpecialStat)
	}
	if cs.specials[dealer] == nil {
		cs.specials[dealer] = make(map[string]combat.SpecialStat)
	}
	s := cs.specials[dealer][kind]
	fn(&s)
	cs.specials[dealer][kind] = s
}

// playerSkill returns the skill bucket for one of the *player's* damage lines,
// or "" for an auto-attack. EQ logs special attacks with a generic verb, so the
// specific skill isn't recoverable here — only the bucket. Notably every monk
// special strike (Eagle Strike / Tiger Claw / Dragon Punch / Tail Rake) logs as
// "strike", and every kick variant as "kick"; hand-to-hand ("punch") and weapon
// ("crush"/"slash"/…) auto-attacks are not skills. "strike" is monk-only for a
// player, so bucketing it here is safe (the panel filters by class anyway).
func playerSkill(verb string) string {
	switch v := strings.ToLower(verb); {
	case strings.HasPrefix(v, "backstab"):
		return "Backstab"
	case strings.HasPrefix(v, "bash"):
		return "Bash"
	case strings.HasPrefix(v, "kick"):
		return "Kick"
	case strings.HasPrefix(v, "strike"):
		return "Strike"
	}
	return ""
}

// applyCritLocked records a critical hit against its attacker. Caller holds the
// write lock; crits only annotate an in-progress fight.
func (cs *CombatSession) applyCritLocked(cr *combat.Crit) {
	if cs.crits == nil {
		cs.crits = make(map[string]combat.CritStat)
	}
	s := cs.crits[cr.Attacker]
	s.Count++
	s.Damage += cr.Damage
	cs.crits[cr.Attacker] = s
}

// applyEventLocked folds a kill / xp / death line into the session counters.
func (cs *CombatSession) applyEventLocked(e *combat.Event) {
	switch e.Kind {
	case combat.EventKill:
		cs.kills++
	case combat.EventXP:
		cs.xpGains++
	case combat.EventPartyXP:
		cs.xpGains++
	case combat.EventDeath:
		cs.deaths++
	}
}

// applyMagicLocked adds a non-melee (spell/proc/DoT) damage line to the
// unattributed magic total. EQ logs name no caster, so it can't be split per
// spell. Caller holds the write lock; magic only annotates an in-progress fight.
func (cs *CombatSession) applyMagicLocked(m *combat.Magic) {
	if strings.ToUpper(m.Target) == "YOU" {
		return // incoming spell damage on the player isn't enemy magic
	}
	cs.magicTotal += m.Dmg
	cs.lastTime = m.ActionTime // spell damage counts toward the DPS span

	// fold the target in so Name() can title a pure-spell fight (a wizard nuke or
	// DoT-only kill) by the mob taking the damage, not fall through to "Solo".
	if m.Target != "" {
		if cs.targets == nil {
			cs.targets = make(map[string]int)
		}
		cs.targets[m.Target] += m.Dmg
	}
}

// MagicTotal is the unattributed non-melee damage dealt to enemies this fight.
func (cs *CombatSession) MagicTotal() int {
	if cs == nil {
		return 0
	}
	return cs.magicTotal
}

// Skills returns the player's per-skill activated-attack tallies (keyed by
// canonical name: Backstab/Bash/Kick). Safe to call on a snapshot.
func (cs *CombatSession) Skills() map[string]combat.SkillStat {
	if cs == nil {
		return nil
	}
	return cs.skills
}

// CritFor returns the critical-hit tally for an attacker, keyed by raw name.
func (cs *CombatSession) CritFor(name string) combat.CritStat {
	if cs == nil {
		return combat.CritStat{}
	}
	return cs.crits[name]
}

// Kills, XpGains, and Deaths report the session's bookkeeping counters.
func (cs *CombatSession) Kills() int {
	if cs == nil {
		return 0
	}
	return cs.kills
}

func (cs *CombatSession) XpGains() int {
	if cs == nil {
		return 0
	}
	return cs.xpGains
}

func (cs *CombatSession) Deaths() int {
	if cs == nil {
		return 0
	}
	return cs.deaths
}

// applySwingLocked folds a swing attempt into the session. Swings are combat
// activity: the SessionManager routes them through activeForLocked, so a swing
// can open or sustain a fight (a stretch of pure misses no longer splits it).
// Caller holds the write lock.
func (cs *CombatSession) applySwingLocked(sw *combat.Swing) {
	cs.recordOutcome(sw.Attacker, sw.Defender, sw.Outcome)
	// a missed special counts against that kind's hit rate (only outright misses,
	// matching offense accuracy — dodge/parry/block are the defender's doing).
	if sw.Outcome == combat.OutcomeMiss && sw.Attacker != "" {
		if kind := specialKind(sw.Verb); kind != "" {
			cs.addSpecialLocked(sw.Attacker, kind, func(s *combat.SpecialStat) { s.Misses++ })
		}
	}
}

// recordOutcome tallies one swing. The defender faces every outcome; the
// attacker only owns whether it connected or missed — dodge/parry/block/absorb
// are defensive acts and stay out of the attacker's accuracy.
func (cs *CombatSession) recordOutcome(attacker, defender string, o combat.SwingOutcome) {
	if cs.offense == nil {
		cs.offense = make(map[string]combat.SwingStats)
	}
	if cs.defense == nil {
		cs.defense = make(map[string]combat.SwingStats)
	}
	if defender != "" {
		cs.defense[defender] = cs.defense[defender].Add(o)
	}
	if (o == combat.OutcomeHit || o == combat.OutcomeMiss) && attacker != "" {
		cs.offense[attacker] = cs.offense[attacker].Add(o)
	}
}

// OffenseFor returns the accuracy tally for an attacker, keyed by raw name
// (i.e. DamageStat dealer names). Zero value if unseen.
func (cs *CombatSession) OffenseFor(name string) combat.SwingStats {
	if cs == nil {
		return combat.SwingStats{}
	}
	return cs.offense[name]
}

// Defense returns each combatant's defensive tally, sorted by attempts faced
// (descending).
func (cs *CombatSession) Defense() []Defender {
	if cs == nil {
		return nil
	}
	out := make([]Defender, 0, len(cs.defense))
	for n, s := range cs.defense {
		out = append(out, Defender{Name: n, Stats: s})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Stats.Swings() > out[j].Stats.Swings()
	})
	return out
}

// GetAggressors is safe to call on a snapshot returned by SessionManager.
// Calling it on a live session is unsafe — go through SessionManager.Current().
func (cs *CombatSession) GetAggressors() []combat.DamageStat {
	if cs == nil {
		return nil
	}
	stats := make([]combat.DamageStat, 0, len(cs.aggressors))
	for _, v := range cs.aggressors {
		stats = append(stats, v)
	}
	return stats
}

// Name returns a display label for the session: the enemy taking the most
// damage. EQ melee lines read "<dealer> <verb> <target> for N", so the thing
// being fought lives in the Target field — we sum damage dealt *to* each
// target and name the session after the heaviest one that isn't the player.
//
// If nothing was hit (e.g. the player only took damage and never swung back),
// we fall back to the heaviest non-player dealer, then to "Solo".
func (cs *CombatSession) Name() string {
	if cs == nil {
		return ""
	}

	enemies := make(map[string]int, len(cs.targets))
	for t, dmg := range cs.targets {
		if t == "" || strings.ToUpper(t) == "YOU" || t == "non-melee" {
			continue
		}
		enemies[t] += dmg
	}

	if name := topByDamage(enemies); name != "" {
		return name
	}

	// no identifiable enemy was struck — fall back to whoever hit us hardest
	dealers := make(map[string]int)
	for _, combat := range cs.aggressors {
		if strings.ToUpper(combat.Dealer) == "YOU" {
			continue
		}
		dealers[combat.Dealer] = combat.Total
	}

	if name := topByDamage(dealers); name != "" {
		return name
	}

	return "Solo"
}

// topByDamage returns the key with the greatest value, or "" if the map is empty.
func topByDamage(m map[string]int) string {
	var name string
	var max int
	for k, v := range m {
		if v > max {
			max = v
			name = k
		}
	}
	return name
}

// StartTime is the timestamp of the session's first recorded hit.
func (cs *CombatSession) StartTime() time.Time {
	if cs == nil {
		return time.Time{}
	}
	return cs.start
}

// EndTime is when the session was closed by a combat lull. It is the zero
// value while the session is still live (the most recent fight).
func (cs *CombatSession) EndTime() time.Time {
	if cs == nil {
		return time.Time{}
	}
	return cs.end
}

// LastUnix is the ActionTime (unix seconds) of the most recent recorded
// exchange, or 0 if none. Replaces the formerly-exported LastTime field so
// readers go through a method like every other CombatSession accessor.
func (cs *CombatSession) LastUnix() int64 {
	if cs == nil {
		return 0
	}
	return cs.lastTime
}

// Duration is the elapsed time from the session's first to last recorded hit.
func (cs *CombatSession) Duration() time.Duration {
	if cs == nil || cs.lastTime == 0 {
		return 0
	}
	d := time.Unix(cs.lastTime, 0).Sub(cs.start)
	if d < 0 {
		return 0
	}
	return d
}

// TopDealer returns the dealer responsible for the most damage in the session
// (the player included) and that dealer's share of the session total, as a
// whole percentage. Returns ("", 0) for an empty session.
func (cs *CombatSession) TopDealer() (string, int) {
	if cs == nil {
		return "", 0
	}

	var name string
	var best, total int
	for n, combat := range cs.aggressors {
		total += combat.Total
		if combat.Total > best {
			best = combat.Total
			name = n
		}
	}

	if name == "" || total == 0 {
		return "", 0
	}

	return strings.ReplaceAll(name, "_", " "), best * 100 / total
}

// Total is the combined damage of every aggressor in the session.
func (cs *CombatSession) Total() int {
	if cs == nil {
		return 0
	}
	var total int
	for _, combat := range cs.aggressors {
		total += combat.Total
	}
	return total
}

// snapshot returns a deep copy of the session. Every map value (DamageStat,
// SwingStats, CritStat, int) is now a pure value type, so a shallow clone of
// each map is a full, independent copy — readers can iterate it lock-free.
func (cs *CombatSession) snapshot() *CombatSession {
	return &CombatSession{
		start:      cs.start,
		end:        cs.end,
		lastTime:   cs.lastTime,
		kills:      cs.kills,
		xpGains:    cs.xpGains,
		deaths:     cs.deaths,
		magicTotal: cs.magicTotal,
		aggressors: maps.Clone(cs.aggressors),
		targets:    maps.Clone(cs.targets),
		skills:     maps.Clone(cs.skills),
		offense:    maps.Clone(cs.offense),
		defense:    maps.Clone(cs.defense),
		crits:      maps.Clone(cs.crits),
		specials:   cloneSpecials(cs.specials),
	}
}

// cloneSpecials deep-clones the nested specials map so a snapshot doesn't alias
// the live inner maps (maps.Clone is shallow — the per-dealer maps would alias).
func cloneSpecials(m map[string]map[string]combat.SpecialStat) map[string]map[string]combat.SpecialStat {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]combat.SpecialStat, len(m))
	for dealer, kinds := range m {
		out[dealer] = maps.Clone(kinds)
	}
	return out
}

// SpecialsFor returns a dealer's per-kind special-attack tallies (keyed by
// Backstab/Bash/Kick), or nil. Safe to call on a snapshot (deep-cloned).
func (cs *CombatSession) SpecialsFor(dealer string) map[string]combat.SpecialStat {
	if cs == nil {
		return nil
	}
	return cs.specials[dealer]
}
