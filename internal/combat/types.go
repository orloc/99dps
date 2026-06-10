package combat

// DamageStat is a per-dealer running tally within a session. It keeps only the
// aggregates the UI consumes — never the individual hits — so a snapshot is a
// flat value copy and memory stays bounded over a long fight.
type DamageStat struct {
	Dealer    string // display name (spaces preserved), from the damage line
	Total     int
	Hits      int   // number of landed melee damage lines from this dealer
	FirstTime int64 // ActionTime of this dealer's first hit
	LastTime  int64 // ActionTime of this dealer's most recent hit
	// SpecialTotal/SpecialHits track the subset of damage dealt by activated
	// skills (backstab/bash/kick) rather than auto-attack swings.
	SpecialTotal int
	SpecialHits  int
}

type DamageSet struct {
	ActionTime int64
	Dealer     string
	Dmg        int
	Target     string
	Verb       string
}

// Crit is a melee critical-hit line: who landed it and for how much. The crit's
// damage is already in the matching "points of damage" line, so a Crit is used
// only to tally crit frequency and magnitude — never added to damage totals.
type Crit struct {
	ActionTime int64
	Attacker   string
	Damage     int
}

// CritStat tallies a combatant's critical hits.
type CritStat struct {
	Count  int
	Damage int
}

// SkillStat tallies one activated melee skill (backstab/bash/kick): how much
// damage it dealt and how many times it landed.
type SkillStat struct {
	Total int
	Hits  int
}

// EventKind classifies a non-damage combat event used for session bookkeeping.
type EventKind int

const (
	EventKill    EventKind = iota // your killing blow on a mob
	EventXP                       // you were credited solo experience for a kill
	EventDeath                    // you were slain
	EventPartyXP                  // group/party experience — implies you are grouped
	EventZone                     // you zoned or camped — a hard session boundary
)

// Magic is a non-melee (spell/proc/DoT) damage line. EQ logs these in passive
// voice — target and amount only, never the caster — so Magic carries no
// attacker and feeds only the unattributed encounter magic total.
type Magic struct {
	ActionTime int64
	Target     string
	Dmg        int
}

// Event is a parsed bookkeeping line (kill / xp / death) attributed to the
// active session.
type Event struct {
	ActionTime int64
	Kind       EventKind
	Name       string
}

// SwingOutcome is how a single melee attempt resolved.
type SwingOutcome int

const (
	OutcomeHit SwingOutcome = iota
	OutcomeMiss
	OutcomeDodge
	OutcomeParry
	OutcomeBlock
	OutcomeRiposte // defender deflected and counter-attacks (a separate line)
	OutcomeAbsorb  // rune-absorbed: the blow connected but was eaten
)

// Swing is one melee attempt parsed from the log: who swung, at whom, and how
// it resolved. A landed hit (a "points of damage" line) is OutcomeHit; the
// "tries to … but …" lines supply the avoided outcomes.
type Swing struct {
	ActionTime int64
	Attacker   string
	Defender   string
	Outcome    SwingOutcome
}

// SwingStats tallies swing outcomes for one combatant, viewed either as the
// attacker (offense / accuracy) or the defender (defense / avoidance).
type SwingStats struct {
	Hits     int
	Misses   int
	Dodges   int
	Parries  int
	Blocks   int
	Ripostes int
	Absorbs  int
}

// Swings is the total number of attempts recorded.
func (s SwingStats) Swings() int {
	return s.Hits + s.Misses + s.Dodges + s.Parries + s.Blocks + s.Ripostes + s.Absorbs
}

// Avoided counts attempts the defender actively turned away (miss, dodge,
// parry, block, riposte). Rune absorbs are excluded — the blow connected.
func (s SwingStats) Avoided() int {
	return s.Misses + s.Dodges + s.Parries + s.Blocks + s.Ripostes
}

// Attempts is hits + misses: the swings that count toward an attacker's
// accuracy. Defensive outcomes (dodge/parry/block/absorb) belong to the
// defender and are deliberately excluded.
func (s SwingStats) Attempts() int {
	return s.Hits + s.Misses
}

// HitRate is landed hits as a percentage of Attempts (hits + misses), or -1
// when there were no qualifying swings.
func (s SwingStats) HitRate() int {
	a := s.Attempts()
	if a == 0 {
		return -1
	}
	return s.Hits * 100 / a
}

// Add folds one outcome into the tally.
func (s SwingStats) Add(o SwingOutcome) SwingStats {
	switch o {
	case OutcomeHit:
		s.Hits++
	case OutcomeMiss:
		s.Misses++
	case OutcomeDodge:
		s.Dodges++
	case OutcomeParry:
		s.Parries++
	case OutcomeBlock:
		s.Blocks++
	case OutcomeRiposte:
		s.Ripostes++
	case OutcomeAbsorb:
		s.Absorbs++
	}
	return s
}
