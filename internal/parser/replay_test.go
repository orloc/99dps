package parser

import (
	"99dps/internal/common"
	"99dps/internal/session"
	"bufio"
	"os"
	"testing"
)

// TestDispatch_GoldenReplay feeds a fixture log through the full classify →
// parse → sink pipeline and asserts the aggregated session stats. It locks in
// the end-to-end behaviour (damage, accuracy, specials, crits, kills) against
// regressions, replacing the ad-hoc manual replays.
func TestDispatch_GoldenReplay(t *testing.T) {
	f, err := os.Open("testdata/sample.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	sm := &session.SessionManager{}
	p := &DmgParser{character: "Kelkix"}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p.dispatch(sc.Text(), sm)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	cur := sm.Current()
	if cur == nil {
		t.Fatal("no session was created")
	}

	// You 88 + 150, croc 8
	if got := cur.Total(); got != 246 {
		t.Errorf("group total = %d, want 246", got)
	}

	// two landed hits, one miss → 66% accuracy (defensive dodge excluded)
	if off := cur.OffenseFor("You"); off.Hits != 2 || off.Misses != 1 || off.HitRate() != 66 {
		t.Errorf("You offense = %+v (hitrate %d)", off, off.HitRate())
	}

	// YOU faced 2 swings (1 landed bite, 1 dodge), avoided 1
	var youDef common.SwingStats
	for _, d := range cur.Defense() {
		if d.Name == "YOU" {
			youDef = d.Stats
		}
	}
	if youDef.Swings() != 2 || youDef.Avoided() != 1 || youDef.Dodges != 1 {
		t.Errorf("YOU defense = %+v, want faced2 avoided1 dodge1", youDef)
	}

	if cur.Kills() != 1 || cur.XpGains() != 1 {
		t.Errorf("kills/xp = %d/%d, want 1/1", cur.Kills(), cur.XpGains())
	}

	if c := cur.CritFor("Naku"); c.Count != 1 || c.Damage != 34 {
		t.Errorf("Naku crit = %+v, want count1 dmg34", c)
	}

	// non-melee damage feeds the unattributed magic total, not the melee Total()
	if cur.MagicTotal() != 120 {
		t.Errorf("magic total = %d, want 120", cur.MagicTotal())
	}

	// the backstab is an activated special
	var you common.DamageStat
	for _, s := range cur.GetAggressors() {
		if s.Dealer == "You" {
			you = s
		}
	}
	if you.SpecialTotal != 150 || you.SpecialHits != 1 {
		t.Errorf("You special = %d/%d, want 150/1", you.SpecialTotal, you.SpecialHits)
	}
}
