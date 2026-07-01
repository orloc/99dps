package gamestate

import "testing"

func TestParseKCH(t *testing.T) {
	cases := []struct {
		body                 string
		caster, target, slot string
		ok                   bool
	}{
		{"Caffelatte tells the guild, 'KCH - Kingkazon - 003'", "Caffelatte", "Kingkazon", "003", true},
		{"Ayrie tells the guild, 'KCH - Balor - 001'", "Ayrie", "Balor", "001", true},
		{"Mcgriddle tells the guild, 'KCH - Relddo - 016'", "Mcgriddle", "Relddo", "016", true},
		// triple-dash separator variant
		{"Imaj tells the guild, 'KCH --- Balor --- 009'", "Imaj", "Balor", "009", true},
		// labeled slots are real call-outs, not dropped
		{"Halloe tells the guild, 'KCH - Flopcakes - GAP'", "Halloe", "Flopcakes", "GAP", true},
		{"Gaps tells the guild, 'KCH - Arbacus - R11'", "Gaps", "Arbacus", "R11", true},
		// trailing whitespace tolerated
		{"Wuntew tells the guild, 'KCH - Nasseribn - 002 '", "Wuntew", "Nasseribn", "002", true},
		// not the KCH chain format
		{"Rychack tells the guild, 'still no zlandi huh?'", "", "", "", false},
		{"Takingaaa tells the guild, 'FFF - CH - Balor'", "", "", "", false}, // letter-coded variant: not matched
		{"You tell your party, 'KCH - Balor - 001'", "", "", "", false},      // not a guild tell
		{"random chatter", "", "", "", false},
	}
	for _, c := range cases {
		caster, target, slot, ok := parseKCH(c.body)
		if ok != c.ok || caster != c.caster || target != c.target || slot != c.slot {
			t.Errorf("parseKCH(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				c.body, caster, target, slot, ok, c.caster, c.target, c.slot, c.ok)
		}
	}
}

type kchEv struct {
	dt   int64
	cast string
	tgt  string
	slot string
}

func feedKCH(t *testing.T, base int64, evs []kchEv) *Tracker {
	tr := NewTracker(loadBook(t))
	for _, e := range evs {
		tr.Observe(e.cast+" tells the guild, 'KCH - "+e.tgt+" - "+e.slot+"'", base+e.dt)
	}
	return tr
}

// chainFor returns the active-chain stats for target, or a zero value.
func chainFor(tr *Tracker, now int64, target string) CHChainStats {
	for _, c := range tr.CHActiveChains(now) {
		if c.Target == target {
			return c
		}
	}
	return CHChainStats{}
}

func TestCHChainCadenceAndGap(t *testing.T) {
	base := int64(1_700_000_000)
	// steady 3s-cadence chain of 3 clerics, one 15s gap, then resumes.
	evs := []kchEv{
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Cy", "Balor", "3"},
		{9, "Al", "Balor", "1"}, {12, "Bo", "Balor", "2"}, {15, "Cy", "Balor", "3"},
		{18, "Al", "Balor", "1"}, {21, "Bo", "Balor", "2"},
		{36, "Cy", "Balor", "3"}, // 15s gap after the 21s call-out
		{39, "Al", "Balor", "1"}, {42, "Bo", "Balor", "2"}, {45, "Cy", "Balor", "3"},
	}
	tr := feedKCH(t, base, evs)
	st := chainFor(tr, base+45, "Balor")

	if st.Target != "Balor" {
		t.Fatalf("target = %q, want Balor", st.Target)
	}
	if st.Clerics != 3 {
		t.Errorf("clerics = %d, want 3", st.Clerics)
	}
	if st.Casts != len(evs) {
		t.Errorf("casts = %d, want %d", st.Casts, len(evs))
	}
	if st.Cadence < 2.5 || st.Cadence > 3.5 {
		t.Errorf("cadence = %.1f, want ~3", st.Cadence)
	}
	if st.Gaps != 1 {
		t.Errorf("gaps = %d, want 1", st.Gaps)
	}
	if st.WorstGap != 15 {
		t.Errorf("worst gap = %d, want 15", st.WorstGap)
	}
	if !st.Active {
		t.Errorf("chain should be active at the last call-out")
	}
}

// the crucial correctness case: two tanks chained at once, call-outs interleaved.
// Each must be tracked as its own coherent chain — not shattered by the other.
func TestCHChainConcurrentTargets(t *testing.T) {
	base := int64(1_700_000_000)
	evs := []kchEv{
		{0, "Al", "TankA", "1"}, {1, "Mo", "TankB", "1"},
		{3, "Bo", "TankA", "2"}, {4, "No", "TankB", "2"},
		{6, "Cy", "TankA", "3"}, {7, "Pe", "TankB", "3"},
		{9, "Al", "TankA", "1"}, {10, "Mo", "TankB", "1"},
		{12, "Bo", "TankA", "2"}, {13, "No", "TankB", "2"},
		{15, "Cy", "TankA", "3"}, {16, "Pe", "TankB", "3"},
	}
	tr := feedKCH(t, base, evs)
	chains := tr.CHActiveChains(base + 16)
	if len(chains) != 2 {
		t.Fatalf("expected 2 concurrent chains, got %d: %+v", len(chains), chains)
	}
	a := chainFor(tr, base+16, "TankA")
	b := chainFor(tr, base+16, "TankB")
	if a.Casts != 6 || b.Casts != 6 {
		t.Errorf("casts A=%d B=%d, want 6 each", a.Casts, b.Casts)
	}
	if a.Clerics != 3 || b.Clerics != 3 {
		t.Errorf("clerics A=%d B=%d, want 3 each", a.Clerics, b.Clerics)
	}
	// each tank's calls are a clean 3s apart; the interleaved other-tank calls must
	// NOT corrupt that — no spurious gaps, cadence stays ~3s (not ~1.5s).
	if a.Gaps != 0 || b.Gaps != 0 {
		t.Errorf("expected no gaps from interleaving; A=%d B=%d", a.Gaps, b.Gaps)
	}
	if a.Cadence < 2.5 || a.Cadence > 3.5 {
		t.Errorf("TankA cadence = %.1f, want ~3 (interleaving must not corrupt it)", a.Cadence)
	}
}

func TestCHChainNextCaster(t *testing.T) {
	base := int64(1_700_000_000)
	evs := []kchEv{
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Cy", "Balor", "3"},
		{9, "Al", "Balor", "1"}, {12, "Bo", "Balor", "2"}, // last numbered slot = 2
	}
	tr := feedKCH(t, base, evs)
	if st := chainFor(tr, base+12, "Balor"); st.NextCaster != "Cy" { // slot 3 is next after 2
		t.Errorf("NextCaster = %q, want Cy (slot 3)", st.NextCaster)
	}
	// one more call from slot 3 → rotation wraps, slot 1 (Al) is next
	tr.Observe("Cy tells the guild, 'KCH - Balor - 3'", base+15)
	if st := chainFor(tr, base+15, "Balor"); st.NextCaster != "Al" {
		t.Errorf("after slot 3, NextCaster = %q, want Al (wrap to slot 1)", st.NextCaster)
	}
}

func TestCHChainBeatAndLag(t *testing.T) {
	base := int64(1_700_000_000)
	// steady 3-cleric chain, 3s cadence → each cleric casts every ~9s (their lap).
	var evs []kchEv
	for i := range 12 {
		evs = append(evs, kchEv{int64(i * 3), []string{"Al", "Bo", "Cy"}[i%3], "Balor", itoa(i%3 + 1)})
	}
	tr := feedKCH(t, base, evs)
	st := chainFor(tr, base+33, "Balor")
	for _, c := range st.Roster {
		if c.BeatPct < 90 {
			t.Errorf("%s BeatPct = %d, want ≥90 on a steady chain", c.Name, c.BeatPct)
		}
		if c.LagSec != 0 {
			t.Errorf("%s LagSec = %d, want 0 (on time)", c.Name, c.LagSec)
		}
	}
}

func TestCHChainGapFormingAndNext(t *testing.T) {
	base := int64(1_700_000_000)
	// 3-cleric, 3s cadence → cadence 3, forming@5s, broken@8s, rotation lap 9s.
	evs := []kchEv{
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Cy", "Balor", "3"},
		{9, "Al", "Balor", "1"}, {12, "Bo", "Balor", "2"}, {15, "Cy", "Balor", "3"},
	}
	tr := feedKCH(t, base, evs)

	// right on the last call: Cy (slot 3) just cast → next due ~1 rotation (9s) out.
	st := chainFor(tr, base+15, "Balor")
	for _, c := range st.Roster {
		if c.Slot == "3" && (c.NextSec < 7 || c.NextSec > 11) {
			t.Errorf("Cy NextSec = %d, want ~9 (rotation lap)", c.NextSec)
		}
	}
	// 6s of silence: the early "gap forming" cue trigger fires, but it's not yet the
	// visibly-broken InGap state (threshold 8s).
	st = chainFor(tr, base+21, "Balor")
	if !st.GapForming {
		t.Errorf("GapForming should be true at 6s silent (cadence 3)")
	}
	if st.InGap {
		t.Errorf("InGap should still be false at 6s (broken threshold is 8s)")
	}
	// 10s: fully broken — both true.
	st = chainFor(tr, base+25, "Balor")
	if !st.GapForming || !st.InGap {
		t.Errorf("at 10s silent both GapForming and InGap should be true")
	}
	// long silence (chain has clearly STOPPED — mob dead / pull over): the audio
	// GapForming window closes so it doesn't keep re-alerting, but InGap (visual) stays.
	st = chainFor(tr, base+15+60, "Balor")
	if st.GapForming {
		t.Errorf("GapForming should close past the cue ceil (60s silent) — no perpetual re-alert")
	}
	if !st.InGap {
		t.Errorf("InGap (visual) should still show the chain is broken at 60s")
	}
}

func TestCHChainGapCalls(t *testing.T) {
	base := int64(1_700_000_000)
	evs := []kchEv{
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Ha", "Balor", "GAP"},
		{9, "Al", "Balor", "1"}, {12, "Ha", "Balor", "GAP"}, {15, "Bo", "Balor", "2"},
	}
	tr := feedKCH(t, base, evs)
	st := chainFor(tr, base+15, "Balor")
	if st.GapCalls != 2 {
		t.Errorf("GapCalls = %d, want 2", st.GapCalls)
	}
	if st.Clerics != 3 { // Al, Bo, Ha — keyed by caster, GAP is a real participant
		t.Errorf("clerics = %d, want 3", st.Clerics)
	}
}

func TestCHChainSlotReplacement(t *testing.T) {
	base := int64(1_700_000_000)
	// slot 2 is held by Bo, who then goes silent. Co keeps the chain alive on slot 1
	// so it doesn't end; >60s later Ce calls slot 2 → Bo is dropped, Ce holds it.
	evs := []kchEv{
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Co", "Balor", "3"},
		{9, "Al", "Balor", "1"}, {12, "Bo", "Balor", "2"}, {15, "Co", "Balor", "3"},
		// Bo stops at slot 2; Al/Co keep chaining for >60s
		{30, "Al", "Balor", "1"}, {45, "Co", "Balor", "3"}, {60, "Al", "Balor", "1"},
		{75, "Co", "Balor", "3"},
		{78, "Ce", "Balor", "2"}, // Ce takes slot 2 — Bo silent 66s → replaced
		{81, "Al", "Balor", "1"}, {84, "Ce", "Balor", "2"},
	}
	tr := feedKCH(t, base, evs)
	st := chainFor(tr, base+84, "Balor")

	// Bo must be gone; slot 2 is now Ce.
	for _, c := range st.Roster {
		if c.Name == "Bo" {
			t.Errorf("Bo should have been dropped from the roster, still present: %+v", c)
		}
		if c.Slot == "2" && c.Name != "Ce" {
			t.Errorf("slot 2 holder = %q, want Ce (replacement)", c.Name)
		}
	}
	if st.Replacements != 1 {
		t.Errorf("replacements = %d, want 1", st.Replacements)
	}
}

func TestCHChainStaleHolderDropped(t *testing.T) {
	base := int64(1_700_000_000)
	// slot 3's holder (Cy) stops; the rest keep going. Cy should drop off the live
	// roster after chDropSec even with no replacement.
	evs := []kchEv{
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Cy", "Balor", "3"},
		{9, "Al", "Balor", "1"}, {12, "Bo", "Balor", "2"}, // Cy silent from t=6
		{30, "Al", "Balor", "1"}, {33, "Bo", "Balor", "2"},
		{60, "Al", "Balor", "1"}, {63, "Bo", "Balor", "2"},
	}
	tr := feedKCH(t, base, evs)
	st := chainFor(tr, base+63, "Balor") // Cy silent 57s here... still in
	for _, c := range st.Roster {
		if c.Name == "Cy" && st.Clerics != 3 {
			t.Errorf("Cy should still count at 57s silent")
		}
	}
	st = chainFor(tr, base+70, "Balor") // now Cy silent 64s > chDropSec → dropped
	for _, c := range st.Roster {
		if c.Name == "Cy" {
			t.Errorf("Cy should be dropped after %ds silence", chDropSec)
		}
	}
	if st.Clerics != 2 {
		t.Errorf("active clerics = %d, want 2 (Cy dropped)", st.Clerics)
	}
}

func TestCHChainLiveGapAndEnd(t *testing.T) {
	base := int64(1_700_000_000)
	evs := []kchEv{
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Cy", "Balor", "3"},
		{9, "Al", "Balor", "1"}, {12, "Bo", "Balor", "2"},
	}
	tr := feedKCH(t, base, evs)

	st := chainFor(tr, base+12+20, "Balor") // 20s silent, cadence ~3 → live gap
	if !st.InGap {
		t.Errorf("expected InGap at 20s since last (cadence %.1f, thresh %d)", st.Cadence, st.GapThresh)
	}
	if st.SinceLast != 20 {
		t.Errorf("sinceLast = %d, want 20", st.SinceLast)
	}

	// past chChainEndSec with no new call-out → no longer an active chain.
	if got := len(feedKCH(t, base, evs).CHActiveChains(base + 12 + chChainEndSec + 5)); got != 0 {
		t.Errorf("active chains after %ds silence = %d, want 0", chChainEndSec, got)
	}
}

func TestCHChainHistorySeparatesPulls(t *testing.T) {
	base := int64(1_700_000_000)
	evs := []kchEv{ // two separate Balor pulls, > chChainEndSec apart
		{0, "Al", "Balor", "1"}, {3, "Bo", "Balor", "2"}, {6, "Cy", "Balor", "3"}, {9, "Al", "Balor", "1"}, {12, "Bo", "Balor", "2"},
		{200, "Al", "Balor", "1"}, {203, "Bo", "Balor", "2"}, {206, "Cy", "Balor", "3"}, {209, "Al", "Balor", "1"}, {212, "Bo", "Balor", "2"},
	}
	tr := feedKCH(t, base, evs)
	hist := tr.CHChainHistory()
	if len(hist) != 2 {
		t.Fatalf("history = %d pulls, want 2 (separated by the %ds gap)", len(hist), chChainEndSec)
	}
	for _, h := range hist {
		if h.Target != "Balor" || h.Casts != 5 {
			t.Errorf("pull = %+v, want Balor with 5 casts", h)
		}
	}
}
