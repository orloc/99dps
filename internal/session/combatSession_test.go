package session

import (
	"testing"
	"time"

	"99dps/internal/eq/combat"
)

// Name picks the heaviest enemy *target* (the thing being fought), not the
// heaviest dealer; falls back to the heaviest non-player dealer (key spaces
// restored) when no enemy was struck; a pure non-melee fight still names its
// target; and an empty session is "Solo".
func TestName(t *testing.T) {
	cases := []struct {
		name string
		seed func(*SessionManager)
		want string
	}{
		{
			// even when the player deals all the damage, the fight is named
			// after what's being hit.
			name: "heaviest target not dealer",
			seed: func(sm *SessionManager) {
				hit(sm, 100, "You", 500, "a frost giant")
				hit(sm, 101, "You", 10, "a bat")
			},
			want: "a frost giant",
		},
		{
			// no enemy struck (the player only took damage): fall back to the
			// heaviest non-player dealer, with the underscore key→spaces.
			name: "falls back to top dealer with spaces",
			seed: func(sm *SessionManager) {
				hit(sm, 100, "a frost giant", 50, "YOU")
			},
			want: "a frost giant",
		},
		{
			// a pure non-melee kill (wizard nuke / DoT): only ApplyMagic, no
			// melee — must not fall through to Solo.
			name: "magic-only fight names the target",
			seed: func(sm *SessionManager) {
				sm.ApplyMagic(&combat.Magic{ActionTime: 100, Target: "a sand giant", Dmg: 800})
				sm.ApplyMagic(&combat.Magic{ActionTime: 101, Target: "a sand giant", Dmg: 600})
			},
			want: "a sand giant",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sm := &SessionManager{}
			tc.seed(sm)
			if got := sm.Current().Name(); got != tc.want {
				t.Errorf("Name = %q, want %q", got, tc.want)
			}
		})
	}

	if got := (&CombatSession{}).Name(); got != "Solo" {
		t.Errorf("empty Name = %q, want Solo", got)
	}
}

// TopDealer ranks every dealer (player included) and restores the spaced name;
// an empty session yields ("", 0).
func TestTopDealer(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "a frost giant", 800, "YOU")
	hit(sm, 100, "You", 200, "a frost giant")

	name, pct := sm.Current().TopDealer()
	if name != "a frost giant" || pct != 80 { // 800 / 1000
		t.Errorf("TopDealer = (%q, %d), want (a frost giant, 80)", name, pct)
	}

	if name, pct := (&CombatSession{}).TopDealer(); name != "" || pct != 0 {
		t.Errorf("empty TopDealer = (%q, %d), want (\"\", 0)", name, pct)
	}
}

// Duration spans first to last hit; a zero/negative span clamps to 0.
func TestDuration(t *testing.T) {
	cases := []struct {
		name string
		cs   *CombatSession
		want time.Duration
	}{
		{"spans first to last", &CombatSession{start: time.Unix(100, 0), lastTime: 130}, 30 * time.Second},
		{"unstarted", &CombatSession{}, 0},
		// out-of-order timestamps must not yield a negative duration
		{"negative span clamps", &CombatSession{start: time.Unix(200, 0), lastTime: 100}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if d := tc.cs.Duration(); d != tc.want {
				t.Errorf("Duration = %v, want %v", d, tc.want)
			}
		})
	}
}

// Total sums every dealer; the per-dealer aggregates (Hits/FirstTime/LastTime)
// must reflect every hit folded in. Empty Total is 0.
func TestTotalAndAggregates(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "You", 10, "a rat")
	hit(sm, 105, "You", 30, "a rat")
	hit(sm, 106, "a rat", 5, "YOU")

	cur := sm.Current()
	if cur.Total() != 45 {
		t.Errorf("Total = %d, want 45", cur.Total())
	}

	you := dealerStat(cur, "You")
	if you.Hits != 2 || you.Total != 40 || you.FirstTime != 100 || you.LastTime != 105 {
		t.Errorf("You aggregate = %+v, want hits2 total40 first100 last105", you)
	}

	if (&CombatSession{}).Total() != 0 {
		t.Error("empty Total != 0")
	}
}

// Player skill tracking buckets only the player's activated attacks (Backstab/
// Bash/Kick) by canonical name; auto-attacks and other dealers' skills are
// excluded.
func TestPlayerSkillsTracking(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 150, Target: "a rat", Verb: "backstabs"})
	sm.Apply(&combat.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 40, Target: "a rat", Verb: "kick"})
	sm.Apply(&combat.DamageSet{ActionTime: 102, Dealer: "You", Dmg: 20, Target: "a rat", Verb: "slash"}) // auto-attack
	sm.Apply(&combat.DamageSet{ActionTime: 103, Dealer: "Bob", Dmg: 99, Target: "a rat", Verb: "kick"})  // groupmate

	skills := sm.Current().Skills()
	if len(skills) != 2 {
		t.Fatalf("skills = %v, want Backstab + Kick only", skills)
	}
	if skills["Backstab"].Total != 150 || skills["Backstab"].Hits != 1 {
		t.Errorf("Backstab = %+v, want 150/1", skills["Backstab"])
	}
	if skills["Kick"].Total != 40 || skills["Kick"].Hits != 1 {
		t.Errorf("Kick = %+v, want 40/1 (groupmate's kick must be excluded)", skills["Kick"])
	}
}

// TestMaxHitTracked: the session records the biggest single melee hit per dealer
// and per skill (a bounded running max, not the individual hits).
func TestMaxHitTracked(t *testing.T) {
	sm := &SessionManager{}
	apply := func(dmg int, verb string) {
		sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: dmg, Target: "a rat", Verb: verb})
	}
	apply(50, "crush") // auto-attack
	apply(120, "kick") // skill
	apply(80, "kick")
	apply(200, "crush") // biggest melee overall

	cur := sm.Current()
	you := dealerStat(cur, "You")
	if you.Max != 200 {
		t.Errorf("overall melee max = %d, want 200", you.Max)
	}
	if k := cur.Skills()["Kick"]; k.Max != 120 {
		t.Errorf("Kick skill max = %d, want 120", k.Max)
	}
}

// A snapshot must be independent of the live session: mutating the original
// after snapshotting must not change the copy (regression guard for the
// CombatRecords→aggregates refactor, where maps are now cloned).
func TestSnapshotIsIndependent(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 100, "You", 10, "a rat")

	snap := sm.Current()
	hit(sm, 101, "You", 90, "a rat") // mutate live state after the snapshot

	if snap.Total() != 10 {
		t.Errorf("snapshot Total = %d, want 10 (must not see later hits)", snap.Total())
	}
}

// StartTime is the first hit's wall-clock time; LastUnix is the most recent
// exchange's ActionTime. Both read 0/zero on an unstarted session.
func TestStartTimeAndLastUnix(t *testing.T) {
	sm := &SessionManager{}
	hit(sm, 1_700_000_000, "You", 10, "a rat")
	hit(sm, 1_700_000_004, "You", 10, "a rat")

	cur := sm.Current()
	if got := cur.StartTime().Unix(); got != 1_700_000_000 {
		t.Errorf("StartTime = %d, want 1700000000", got)
	}
	if got := cur.LastUnix(); got != 1_700_000_004 {
		t.Errorf("LastUnix = %d, want 1700000004", got)
	}

	if !(&CombatSession{}).StartTime().IsZero() {
		t.Error("unstarted StartTime should be the zero time")
	}
	if got := (&CombatSession{}).LastUnix(); got != 0 {
		t.Errorf("unstarted LastUnix = %d, want 0", got)
	}
}

// Crits, special-attack damage, and kill/xp/death counters attach to the
// active session without rolling it.
func TestApply_CritsSpecialsAndEvents(t *testing.T) {
	sm := &SessionManager{}

	// auto-attack hit + a backstab special, both by You
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 50, Target: "a rat", Verb: "slash"})
	sm.Apply(&combat.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 150, Target: "a rat", Verb: "backstab"})

	sm.ApplyCrit(&combat.Crit{ActionTime: 101, Attacker: "You", Damage: 150})
	sm.ApplyEvent(&combat.Event{ActionTime: 102, Kind: combat.EventKill})
	sm.ApplyEvent(&combat.Event{ActionTime: 102, Kind: combat.EventXP})
	sm.ApplyEvent(&combat.Event{ActionTime: 102, Kind: combat.EventDeath})

	if n := sm.Len(); n != 1 {
		t.Fatalf("non-damage events rolled a session: Len=%d, want 1", n)
	}

	cur := sm.Current()

	if c := cur.CritFor("You"); c.Count != 1 || c.Damage != 150 {
		t.Errorf("crit tally = %+v, want count1 dmg150", c)
	}
	if cur.Kills() != 1 || cur.XpGains() != 1 || cur.Deaths() != 1 {
		t.Errorf("counters kills/xp/deaths = %d/%d/%d, want 1/1/1", cur.Kills(), cur.XpGains(), cur.Deaths())
	}

	you := dealerStat(cur, "You")
	if you.Total != 200 {
		t.Errorf("total = %d, want 200", you.Total)
	}
	if you.SpecialTotal != 150 || you.SpecialHits != 1 {
		t.Errorf("special total/hits = %d/%d, want 150/1", you.SpecialTotal, you.SpecialHits)
	}
}

// MagicTotal sums enemy non-melee damage (folded into the encounter total);
// incoming spell damage on the player is excluded.
func TestApplyMagic_UnattributedTotal(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "a rat", Dmg: 5, Target: "YOU", Verb: "bite"})

	sm.ApplyMagic(&combat.Magic{ActionTime: 103, Target: "a rat", Dmg: 200})
	sm.ApplyMagic(&combat.Magic{ActionTime: 104, Target: "a rat", Dmg: 50})
	sm.ApplyMagic(&combat.Magic{ActionTime: 105, Target: "YOU", Dmg: 999}) // incoming — excluded

	if n := sm.Len(); n != 1 {
		t.Fatalf("magic events rolled a session: Len=%d, want 1", n)
	}
	if got := sm.Current().MagicTotal(); got != 250 {
		t.Errorf("magic total = %d, want 250 (incoming-on-you excluded)", got)
	}
}

// Swing attribution: an attacker's accuracy (offense) counts only hits and
// misses; the defender's dodge/parry/block must not bleed into the
// attacker's hit ratio. The defender, conversely, faces every outcome.
func TestApplySwing_OffenseExcludesDefensiveOutcomes(t *testing.T) {
	sm := &SessionManager{}

	// start the fight with a landed hit (also a connecting swing)
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 10, Target: "a rat"})

	at := func(o combat.SwingOutcome) {
		sm.ApplySwing(&combat.Swing{ActionTime: 100, Attacker: "You", Defender: "a rat", Outcome: o})
	}
	at(combat.OutcomeMiss)
	at(combat.OutcomeDodge)
	at(combat.OutcomeParry)
	at(combat.OutcomeBlock)
	at(combat.OutcomeRiposte)

	cur := sm.Current()

	off := cur.OffenseFor("You")
	if off.Hits != 1 || off.Misses != 1 {
		t.Fatalf("offense hits/misses = %d/%d, want 1/1", off.Hits, off.Misses)
	}
	if off.Dodges != 0 || off.Parries != 0 || off.Blocks != 0 || off.Ripostes != 0 {
		t.Errorf("offense leaked defensive outcomes: %+v", off)
	}
	if off.Attempts() != 2 || off.HitRate() != 50 {
		t.Errorf("attempts/hitrate = %d/%d, want 2/50", off.Attempts(), off.HitRate())
	}

	var rat combat.SwingStats
	for _, d := range cur.Defense() {
		if d.Name == "a rat" {
			rat = d.Stats
		}
	}
	if rat.Hits != 1 || rat.Misses != 1 || rat.Dodges != 1 || rat.Parries != 1 || rat.Blocks != 1 || rat.Ripostes != 1 {
		t.Errorf("rat defense = %+v, want each of hit/miss/dodge/parry/block/riposte == 1", rat)
	}
	if rat.Swings() != 6 || rat.Avoided() != 5 {
		t.Errorf("rat faced/avoided = %d/%d, want 6/5", rat.Swings(), rat.Avoided())
	}
}

// TestSpecialsBreakdown checks the per-dealer, per-kind special tally: landed
// damage/hits from damage lines, misses from swings, hit rate, and that a
// snapshot doesn't alias the live inner maps.
func TestSpecialsBreakdown(t *testing.T) {
	sm := &SessionManager{}
	// You: two backstabs land, one misses; one kick lands.
	sm.Apply(&combat.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 300, Target: "a rat", Verb: "backstab"})
	sm.Apply(&combat.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 500, Target: "a rat", Verb: "backstabs"})
	sm.ApplySwing(&combat.Swing{ActionTime: 102, Attacker: "You", Defender: "a rat", Verb: "backstab", Outcome: combat.OutcomeMiss})
	sm.Apply(&combat.DamageSet{ActionTime: 103, Dealer: "You", Dmg: 90, Target: "a rat", Verb: "kicks"})
	// a dodge of a kick must NOT count against the kick's hit rate
	sm.ApplySwing(&combat.Swing{ActionTime: 104, Attacker: "You", Defender: "a rat", Verb: "kick", Outcome: combat.OutcomeDodge})

	snap := sm.Current()
	sp := snap.SpecialsFor("You")
	bs := sp["Backstab"]
	if bs.Total != 800 || bs.Hits != 2 || bs.Misses != 1 {
		t.Errorf("backstab = %+v, want {Total:800 Hits:2 Misses:1}", bs)
	}
	if bs.HitRate() != 66 { // 2/3
		t.Errorf("backstab hit rate = %d, want 66", bs.HitRate())
	}
	if k := sp["Kick"]; k.Total != 90 || k.Hits != 1 || k.Misses != 0 || k.HitRate() != 100 {
		t.Errorf("kick = %+v (hr %d), want {90 1 0} 100%%", k, k.HitRate())
	}

	// snapshot independence: mutating the live session must not touch the snapshot
	sm.Apply(&combat.DamageSet{ActionTime: 110, Dealer: "You", Dmg: 1000, Target: "a rat", Verb: "backstab"})
	if sp["Backstab"].Total != 800 {
		t.Error("snapshot aliased the live specials map")
	}
}

// TestSkillReuseMeasurement: the effective reuse is the fastest plausible gap
// between attempts — excluding double-attack same-round pairs (<4s) and lulls
// (>12s), and counting misses as attempts.
func TestSkillReuseMeasurement(t *testing.T) {
	sm := &SessionManager{}
	bs := func(at int64) {
		sm.Apply(&combat.DamageSet{ActionTime: at, Dealer: "You", Dmg: 200, Target: "a rat", Verb: "backstab"})
	}
	miss := func(at int64) {
		sm.ApplySwing(&combat.Swing{ActionTime: at, Attacker: "You", Defender: "a rat", Verb: "backstab", Outcome: combat.OutcomeMiss})
	}
	bs(100)
	bs(100)   // double-attack same second (gap 0) — ignored
	miss(106) // gap 6 → reuse candidate
	bs(111)   // gap 5 → faster candidate
	bs(160)   // gap 49 (med-break lull) — ignored
	bs(165)   // gap 5

	s := sm.Current().Skills()["Backstab"]
	if s.Reuse != 5 {
		t.Errorf("measured reuse = %ds, want 5 (min valid gap; DA 0s and 49s lull excluded)", s.Reuse)
	}
	if s.LastAttempt != 165 {
		t.Errorf("last attempt = %d, want 165", s.LastAttempt)
	}
	if s.Hits != 5 { // five landed (the DA pair both land); the miss is an attempt, not a hit
		t.Errorf("hits = %d, want 5", s.Hits)
	}
}

// dealerStat returns the aggregate row for the named dealer (zero value if
// absent).
func dealerStat(cs *CombatSession, dealer string) combat.DamageStat {
	for _, s := range cs.GetAggressors() {
		if s.Dealer == dealer {
			return s
		}
	}
	return combat.DamageStat{}
}
