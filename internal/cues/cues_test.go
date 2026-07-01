package cues

import (
	"strings"
	"testing"
	"time"

	"99dps/internal/eq/eqclass"
	"99dps/internal/eq/gamestate"
)

// fakeSpeaker records what the engine spoke, for assertions.
type fakeSpeaker struct {
	normal, urgent []string
	flushed        int
}

func (f *fakeSpeaker) Say(s string)       { f.normal = append(f.normal, s) }
func (f *fakeSpeaker) SayUrgent(s string) { f.urgent = append(f.urgent, s) }
func (f *fakeSpeaker) Flush()             { f.flushed++; f.normal = nil }

// debuffRow builds a minimal 217-field spells_us.txt line for a detrimental spell
// with the given cast-on-other landing emote. Field indices mirror gamestate/book.go.
func debuffRow(name, emote string) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name    // fName
	f[7] = emote   // fCastOnOther
	f[13] = "3000" // fCastTime (ms)
	f[16] = "1"    // fDurFormula
	f[17] = "30"   // fDurCap (ticks)
	f[83] = "0"    // detrimental
	return strings.Join(f, "^")
}

func TestDueAnnouncements(t *testing.T) {
	e := NewEngine()
	now := int64(1000)
	healthy := gamestate.Timer{Spell: "Clarity", Target: "Tankguy", Expiry: now + 600}
	low := gamestate.Timer{Spell: "Clarity", Target: "Healer", Expiry: now + 8}
	charm := gamestate.Timer{Spell: "Charm", Target: "Charm", Expiry: now + 5, Charm: true}

	got := e.dueAnnouncements([]gamestate.Timer{healthy, low, charm}, Prefs{}, now)
	if len(got) != 1 || got[0].Spell != "Clarity" || got[0].Target != "Healer" {
		t.Fatalf("first pass = %v, want one Clarity/Healer", got)
	}
	if got := e.dueAnnouncements([]gamestate.Timer{healthy, low, charm}, Prefs{}, now); got != nil {
		t.Errorf("repeat pass spoke again: %v", got)
	}
	// refreshed (healthy) re-arms; dropping low again speaks
	e.dueAnnouncements([]gamestate.Timer{{Spell: "Clarity", Target: "Healer", Expiry: now + 600}}, Prefs{}, now)
	if got := e.dueAnnouncements([]gamestate.Timer{{Spell: "Clarity", Target: "Healer", Expiry: now + 5}}, Prefs{}, now); len(got) != 1 {
		t.Errorf("refresh should re-arm; got %v", got)
	}
}

func TestDueAnnouncementsTwoLevels(t *testing.T) {
	e := NewEngine()
	now := int64(1_000_000)
	// a 100-min buff: low (gold) caps at 300s left, fading (red) at 60s left.
	buff := func(remaining int64) gamestate.Timer {
		return gamestate.Timer{Spell: "Clarity", Target: "You", Start: now - (6000 - remaining), Expiry: now + remaining}
	}

	if got := e.dueAnnouncements([]gamestate.Timer{buff(400)}, Prefs{}, now); len(got) != 0 {
		t.Fatalf("fresh buff (400s left) should be silent, got %v", got)
	}
	got := e.dueAnnouncements([]gamestate.Timer{buff(250)}, Prefs{}, now)
	if len(got) != 1 || got[0].fading {
		t.Fatalf("entering the low zone should give one non-fading cue, got %+v", got)
	}
	if got := e.dueAnnouncements([]gamestate.Timer{buff(250)}, Prefs{}, now); len(got) != 0 {
		t.Errorf("still low should not re-announce, got %v", got)
	}
	got = e.dueAnnouncements([]gamestate.Timer{buff(50)}, Prefs{}, now)
	if len(got) != 1 || !got[0].fading {
		t.Fatalf("entering the red zone should give one fading cue, got %+v", got)
	}
	// refreshed back to fresh → re-arm; decaying to low warns "low" again
	e.dueAnnouncements([]gamestate.Timer{buff(400)}, Prefs{}, now)
	if got := e.dueAnnouncements([]gamestate.Timer{buff(250)}, Prefs{}, now); len(got) != 1 || got[0].fading {
		t.Errorf("a refreshed buff should warn low again, got %+v", got)
	}
}

func TestDueAnnouncementsSkipsEstimated(t *testing.T) {
	e := NewEngine()
	now := int64(1_000)
	// an incoming debuff on you, near (estimated) expiry — must NOT announce
	est := gamestate.Timer{Spell: "Slowed", Target: "You", Start: now - 360, Expiry: now + 5, Estimated: true}
	if got := e.dueAnnouncements([]gamestate.Timer{est}, Prefs{}, now); len(got) != 0 {
		t.Errorf("estimated incoming debuff should not produce a fade cue, got %v", got)
	}
	// a real buff at the same remaining still announces (sanity)
	buff := gamestate.Timer{Spell: "Clarity", Target: "You", Start: now - 360, Expiry: now + 5}
	if got := e.dueAnnouncements([]gamestate.Timer{buff}, Prefs{}, now); len(got) != 1 {
		t.Errorf("a real buff should still announce, got %v", got)
	}
}

// TestDueAnnouncementsGatesByType: a disabled cue type stays silent while other
// types still fire from the same tick.
func TestDueAnnouncementsGatesByType(t *testing.T) {
	e := NewEngine()
	prefs := Prefs{Overrides: map[string]bool{cueMez: false}} // mez cues off
	now := int64(1000)
	mez := gamestate.Timer{Spell: "Lull", Target: "a gnoll", Start: now - 100, Expiry: now + 5, Mez: true}
	deb := gamestate.Timer{Spell: "Cripple", Target: "a gnoll", Start: now - 100, Expiry: now + 5, Detrimental: true}

	due := e.dueAnnouncements([]gamestate.Timer{mez, deb}, prefs, now)
	if len(due) != 1 || due[0].Spell != "Cripple" {
		t.Fatalf("mez disabled → only the debuff should be due, got %+v", due)
	}
}

// TestDebuffCueFiresOncePerMob: a debuff/DoT on a mob warns at most once, even
// when re-applied (refreshed) — a necro re-dotting shouldn't re-announce.
func TestDebuffCueFiresOncePerMob(t *testing.T) {
	e := NewEngine()
	now := int64(1000)
	dot := func(exp int64) gamestate.Timer {
		return gamestate.Timer{Spell: "Heat Blood", Target: "a spectre", Start: now - 100, Expiry: exp, Detrimental: true}
	}
	if got := e.dueAnnouncements([]gamestate.Timer{dot(now + 5)}, Prefs{}, now); len(got) != 1 {
		t.Fatalf("first low should warn, got %v", got)
	}
	if got := e.dueAnnouncements([]gamestate.Timer{dot(now + 600)}, Prefs{}, now); len(got) != 0 {
		t.Errorf("a refresh shouldn't re-announce, got %v", got)
	}
	if got := e.dueAnnouncements([]gamestate.Timer{dot(now + 5)}, Prefs{}, now); len(got) != 0 {
		t.Errorf("a re-dotted DoT should warn at most once per mob, got %v", got)
	}
}

// TestSameNamedInstancesNoRepeat reproduces the audio-spam bug: several
// same-named mobs under the same CC (pacify on multiple "a sand giant") share the
// (spell, target) cue key. The most-urgent instance must announce its fade ONCE,
// not re-fire every tick — the old per-instance loop reset the announced urgency
// to the least-urgent instance each tick, re-spamming the fading cue.
func TestSameNamedInstancesNoRepeat(t *testing.T) {
	e := NewEngine()
	now := int64(1000)
	// pacify is beneficial-flagged (not Detrimental), the case that flip-flopped.
	pac := func(exp int64) gamestate.Timer {
		return gamestate.Timer{Spell: "Lull", Target: "a sand giant", Start: now - 100, Expiry: exp, Pacify: true}
	}
	// one giant fading (5s), one fresh (300s), one mid (120s) — all the same key.
	active := []gamestate.Timer{pac(now + 5), pac(now + 300), pac(now + 120)}

	got := e.dueAnnouncements(active, Prefs{}, now)
	if len(got) != 1 || !got[0].fading {
		t.Fatalf("first tick: want one fading cue, got %+v", got)
	}
	// subsequent ticks with the SAME set must stay silent — no per-tick repeat.
	for i := 0; i < 5; i++ {
		if got := e.dueAnnouncements(active, Prefs{}, now); len(got) != 0 {
			t.Fatalf("tick %d: same-key instances must not re-announce, got %+v", i, got)
		}
	}
}

// TestSameNamedPacifyNoTickSpam simulates the real scenario over time: several
// same-named pacified mobs ("a sand giant") with staggered expiries, ticked once
// per second as they count down and expire. They all share the (spell, target)
// cue key, so the group must announce a bounded number of times (≤ one per mob) —
// NOT once per tick. Before the fix this fired the fading cue every tick while any
// instance sat in the red zone, producing dozens of repeats.
func TestSameNamedPacifyNoTickSpam(t *testing.T) {
	e := NewEngine()
	start := int64(1000)
	exps := []int64{start + 10, start + 25, start + 40} // three giants, staggered
	pac := func(exp int64) gamestate.Timer {
		// pacify: beneficial-flagged (not Detrimental) — the case that flip-flopped.
		return gamestate.Timer{Spell: "Lull", Target: "a sand giant", Start: start - 100, Expiry: exp, Pacify: true}
	}

	var spoken int
	for now := start; now <= start+45; now++ {
		var active []gamestate.Timer
		for _, exp := range exps {
			if exp > now { // still up
				active = append(active, pac(exp))
			}
		}
		spoken += len(e.dueAnnouncements(active, Prefs{}, now))
	}

	// the whole same-named group is one speech subject, so over its entire lifetime
	// it should speak at most a couple of times — never the per-tick storm (~30+).
	if spoken == 0 {
		t.Error("expected at least one cue over the lifetime")
	}
	if spoken > len(exps) {
		t.Errorf("group spoke %d times over its lifetime, want ≤ %d (no per-tick spam)", spoken, len(exps))
	}
}

func TestComposeCue(t *testing.T) {
	you := func(s string) gamestate.Timer { return gamestate.Timer{Spell: s, Target: "You"} }
	cases := []struct {
		due  []gamestate.Timer
		want string
	}{
		{[]gamestate.Timer{you("Clarity")}, "Clarity is fading."},
		{[]gamestate.Timer{you("Clarity"), you("Umbra")}, "Clarity and Umbra are fading."},
		{[]gamestate.Timer{you("Clarity"), you("Brilliance"), you("Umbra")}, "Clarity, Brilliance, and Umbra are fading."},
		{[]gamestate.Timer{{Spell: "Snare", Target: "a gnoll"}}, "Snare on a gnoll is fading."},
	}
	for _, c := range cases {
		if got := composeCue(ChatterNormal, c.due, true, 0, 0); got != c.want {
			t.Errorf("composeCue = %q, want %q", got, c.want)
		}
	}
}

// TestComposeCueLow: the gold-zone cue names the buff, reads "low" (not "fading"),
// and speaks the time remaining.
func TestComposeCueLow(t *testing.T) {
	now := int64(1000)
	due := []gamestate.Timer{{Spell: "Clarity", Target: "You", Expiry: now + 300}} // 5 min left
	got := composeCue(ChatterNormal, due, false, now, 0)
	if !strings.Contains(got, "Clarity") || !strings.Contains(got, "low") {
		t.Errorf("low cue should name the buff and say it's low: %q", got)
	}
	if !strings.Contains(got, "5 minutes") {
		t.Errorf("low cue should speak the time left (5 minutes): %q", got)
	}
	if strings.Contains(got, "fading") {
		t.Errorf("low cue should not say 'fading': %q", got)
	}
}

// TestComposeCueCombines: several subjects in a zone combine into one sentence,
// and more than three collapse to a count.
func TestComposeCueCombines(t *testing.T) {
	now := int64(1000)
	tm := func(name string) gamestate.Timer {
		return gamestate.Timer{Spell: name, Target: "You", Expiry: now + 300}
	}
	two := composeCue(ChatterNormal, []gamestate.Timer{tm("Clarity"), tm("Aegolism")}, false, now, 0)
	if !strings.Contains(two, "Clarity") || !strings.Contains(two, "Aegolism") {
		t.Errorf("two low buffs should combine into one cue: %q", two)
	}
	many := composeCue(ChatterNormal, []gamestate.Timer{tm("A"), tm("B"), tm("C"), tm("D")}, true, now, 0)
	if !strings.Contains(many, "4") || !strings.Contains(many, "fading") {
		t.Errorf("more than three fading should collapse to a count: %q", many)
	}
}

func TestSpokenDuration(t *testing.T) {
	cases := []struct {
		sec  int64
		want string
	}{
		{300, "5 minutes"},
		{100, "1 minute 40 seconds"}, // must NOT round up to "2 minutes"
		{90, "1 minute 30 seconds"},
		{61, "1 minute 1 second"},
		{60, "1 minute"},
		{50, "50 seconds"},
		{1, "1 second"},
		{-5, "0 seconds"},
	}
	for _, c := range cases {
		if got := spokenDuration(c.sec); got != c.want {
			t.Errorf("spokenDuration(%d) = %q, want %q", c.sec, got, c.want)
		}
	}
}

func TestFadeVariety(t *testing.T) {
	due := []gamestate.Timer{{Spell: "Clarity", Target: "You"}}
	seen := map[string]bool{}
	for seq := 0; seq < 3; seq++ {
		seen[composeCue(ChatterNormal, due, true, 0, seq)] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct phrasings across seqs, got %v", seen)
	}
	// plural agreement holds in every style
	two := []gamestate.Timer{{Spell: "Clarity", Target: "You"}, {Spell: "Umbra", Target: "You"}}
	for seq := 0; seq < 3; seq++ {
		if got := composeCue(ChatterNormal, two, true, 0, seq); !strings.Contains(got, "Clarity and Umbra") || !strings.Contains(got, "are") {
			t.Errorf("seq %d plural phrasing wrong: %q", seq, got)
		}
	}
}

func TestCharmAndResistPhrases(t *testing.T) {
	charm := map[string]bool{}
	for seq := 0; seq < 3; seq++ {
		charm[charmBreakPhrase(ChatterNormal, seq)] = true
	}
	if len(charm) != 3 {
		t.Errorf("charm-break phrasing should vary, got %v", charm)
	}
	for seq := 0; seq < 3; seq++ {
		if got := resistPhrase(ChatterNormal, "Bedlam", seq); !strings.Contains(got, "Bedlam") {
			t.Errorf("resist phrase %d should name the spell: %q", seq, got)
		}
	}
}

func TestAnnounceResistIsUrgent(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	now := time.Now().Unix()
	tr.Observe("Your target resisted the Bedlam spell.", now) // sets the resist signal

	e := NewEngine()
	e.Announce(fe, tr, Prefs{}, now)
	if len(fe.urgent) != 1 || !strings.Contains(fe.urgent[0], "Bedlam") {
		t.Fatalf("resist should use the urgent voice with the spell name, got urgent=%v normal=%v", fe.urgent, fe.normal)
	}
	if len(fe.normal) != 0 {
		t.Errorf("resist must not use the normal voice, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, now) // same resist still within grace → must not repeat
	if len(fe.urgent) != 1 {
		t.Errorf("resist should fire once, got %v", fe.urgent)
	}
}

// TestResistAlternatingNoSpam reproduces the resist-macro spam: a 2-spell macro
// on a high-resist mob alternates resisted spells, so the resist signal never
// goes quiet. The old single-slot dedup keyed on spell name and re-fired on every
// alternation; the per-spell time gate must cap it to once per spell per window.
func TestResistAlternatingNoSpam(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	e := NewEngine()
	now := time.Now().Unix()

	// Snare/Tashan/Snare/Tashan… resisted every 2s for 12s (within the grace window
	// throughout, so the signal stays "active" — the alternation is the only change).
	spells := []string{"Snare", "Tashan"}
	for i := 0; i <= 6; i++ {
		at := now + int64(i*2)
		tr.Observe("Your target resisted the "+spells[i%2]+" spell.", at)
		e.Announce(fe, tr, Prefs{}, at)
	}
	// each spell announces at most once inside one resistRepeatSec window — so two
	// spells over 12s give a small bounded count, not one urgent cue per cast (~7).
	if len(fe.urgent) > len(spells) {
		t.Fatalf("alternating resists should not spam: got %d urgent cues %v, want ≤ %d", len(fe.urgent), fe.urgent, len(spells))
	}
	if len(fe.urgent) == 0 {
		t.Error("a resist should still announce at least once")
	}

	// after the signal clears (quiet > grace) a fresh resist re-announces.
	later := now + 100
	tr.Observe("Your target resisted the Snare spell.", later)
	e.Announce(fe, tr, Prefs{}, later)
	if len(fe.urgent) == 0 || !strings.Contains(fe.urgent[len(fe.urgent)-1], "Snare") {
		t.Errorf("a resist after a quiet gap should re-announce, got %v", fe.urgent)
	}
}

func TestFeignFailVariety(t *testing.T) {
	seen := map[string]bool{}
	for seq := 0; seq < 3; seq++ {
		seen[feignFailPhrase(ChatterNormal, seq)] = true
	}
	if len(seen) != 3 {
		t.Errorf("feign-fail phrasing should vary, got %v", seen)
	}
}

func TestFeignFailIsUrgent(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.FeignFailed(1000)
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, 1000)
	if len(fe.urgent) != 1 || !strings.Contains(strings.ToLower(fe.urgent[0]), "feign") {
		t.Fatalf("failed feign should be urgent, got urgent=%v normal=%v", fe.urgent, fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, 1000) // still failed → no repeat
	if len(fe.urgent) != 1 {
		t.Errorf("feign fail should fire once, got %v", fe.urgent)
	}
}

func TestLongCooldownReadyCue(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000) // Mend → 360s cooldown
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, 1000) // just used → records not-ready, no cue
	if len(fe.normal) != 0 {
		t.Fatalf("no cue while on cooldown, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, 1000+361) // ready → gentle cue
	if len(fe.normal) != 1 || !strings.Contains(fe.normal[0], "Mend") {
		t.Fatalf("Mend ready should be a gentle cue, got normal=%v urgent=%v", fe.normal, fe.urgent)
	}
	e.Announce(fe, tr, Prefs{}, 1000+362) // still ready → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("cooldown-ready should fire once, got %v", fe.normal)
	}
}

func TestShortCooldownNoCue(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.FeignAttempt(2000) // Feign Death → 11s reuse (short)
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, 2000)
	e.Announce(fe, tr, Prefs{}, 2000+12) // ready, but too short to announce
	if len(fe.normal) != 0 {
		t.Errorf("a short cooldown must not announce, got %v", fe.normal)
	}
}

// TestSlainFlushesQueuedCues: when a mob death clears its debuffs, queued fade
// cues about it are flushed so they don't keep playing after the kill.
func TestSlainFlushesQueuedCues(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(debuffRow("Malosini", "'s magic resistances are lowered.")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassShaman)
	now := time.Now().Unix()
	tr.BeginCast("Malosini", now-5)
	tr.Observe("a sand giant's magic resistances are lowered.", now) // debuff on a mob
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, now) // baseline: record the current slain-clear count
	base := fe.flushed

	tr.Observe("You have slain a sand giant!", now+1) // clears the debuff → bumps the counter
	e.Announce(fe, tr, Prefs{}, now+2)
	if fe.flushed != base+1 {
		t.Errorf("a mob death should flush queued cues once, got %d (base %d)", fe.flushed, base)
	}
}

// TestSpawnSoonCue: a tracked repop gets a calm heads-up ~60s out (normal voice,
// once) then an urgent "spawning now" alert ~3s out (urgent voice, once); the
// toggle silences both.
func TestSpawnSoonCue(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	now := time.Now().Unix()
	tr.Observe("You have entered east commonlands.", now)
	tr.UseOverrides(gamestate.LoadOverrides(t.TempDir() + "/ov.json"))
	tr.SetOverride("a griffon", 120)             // 120s repop
	tr.Observe("You have slain a griffon!", now) // repop at now+120
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, now+30) // 90s out → not yet in the 60s heads-up window
	if len(fe.normal) != 0 {
		t.Fatalf("no spawn cue at 90s out, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, now+65) // 55s out → heads-up fires (normal voice)
	if len(fe.normal) != 1 || !strings.Contains(fe.normal[0], "griffon") {
		t.Fatalf("heads-up expected ~60s out, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, now+66) // still in window → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("heads-up should fire once per spawn, got %v", fe.normal)
	}

	// the urgent at-the-pop alert fires once as the timer crosses ~3s out.
	e.Announce(fe, tr, Prefs{}, now+118) // 2s out → "spawning now" (urgent voice)
	if len(fe.urgent) != 1 || !strings.Contains(fe.urgent[0], "griffon") {
		t.Fatalf("imminent alert expected ~3s out, got urgent=%v", fe.urgent)
	}
	e.Announce(fe, tr, Prefs{}, now+119) // still imminent → no repeat
	if len(fe.urgent) != 1 {
		t.Errorf("imminent alert should fire once per spawn, got %v", fe.urgent)
	}

	// the toggle silences both stages: a fresh kill with the cue disabled stays quiet.
	fe2 := &fakeSpeaker{}
	e2 := NewEngine()
	prefs := Prefs{Overrides: map[string]bool{cueSpawnSoon: false}}
	tr.SetOverride("a griffon", 60)
	tr.Observe("You have slain a griffon!", now+200) // a second spawn, repop at now+260
	e2.Announce(fe2, tr, prefs, now+235)             // 25s out, but disabled
	e2.Announce(fe2, tr, prefs, now+258)             // 2s out, but disabled
	if len(fe2.normal) != 0 || len(fe2.urgent) != 0 {
		t.Errorf("disabled spawn cue should stay silent, got normal=%v urgent=%v", fe2.normal, fe2.urgent)
	}
}

// TestSpawnOverrideNoReannounce: re-timing a mob's repop (an override edit) moves
// its expiry but it's the same physical spawn — it must not re-announce. The cue
// keys on the stable kill time, not the mutable expiry.
func TestSpawnOverrideNoReannounce(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.UseOverrides(gamestate.LoadOverrides(t.TempDir() + "/ov.json"))
	now := time.Now().Unix()
	tr.Observe("You have entered east commonlands.", now)
	tr.SetOverride("a griffon", 60)
	tr.Observe("You have slain a griffon!", now) // repop at now+60
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, now+35) // 25s out → fires once
	if len(fe.normal) != 1 || !strings.Contains(fe.normal[0], "griffon") {
		t.Fatalf("spawn should announce once, got %v", fe.normal)
	}
	// the player corrects the camp timer while the cue is already spoken: expiry
	// moves now+60 → now+65 (still ~30s out), same spawn → must stay silent.
	tr.SetOverride("a griffon", 65)
	e.Announce(fe, tr, Prefs{}, now+35)
	if len(fe.normal) != 1 {
		t.Errorf("an override re-time must not re-announce the same spawn, got %v", fe.normal)
	}
}

// TestSpawnSoonOnlyCurrentZone: a repop in a zone you've LEFT must not cue (you
// can't reach it) — the visual mob list still shows it, but the audio alert is
// scoped to your current zone, and re-fires if you zone back while it's pending.
func TestSpawnSoonOnlyCurrentZone(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.UseOverrides(gamestate.LoadOverrides(t.TempDir() + "/ov.json"))
	now := time.Now().Unix()
	tr.Observe("You have entered east commonlands.", now)
	tr.SetOverride("a griffon", 60)
	tr.Observe("You have slain a griffon!", now) // repop in ECommons at now+60
	tr.Observe("You have entered west commonlands.", now+10)
	e := NewEngine()

	// 25s before the griffon pops, but we're in a different zone now → silent.
	e.Announce(fe, tr, Prefs{}, now+35)
	if len(fe.normal) != 0 {
		t.Fatalf("a repop in a left zone must not cue, got %v", fe.normal)
	}
	// zone back in while it's still pending → the cue fires.
	tr.Observe("You have entered east commonlands.", now+40)
	e.Announce(fe, tr, Prefs{}, now+40)
	if len(fe.normal) != 1 || !strings.Contains(fe.normal[0], "griffon") {
		t.Fatalf("returning to the zone should cue the still-pending spawn, got %v", fe.normal)
	}
}

// TestHalfCooldownCue: the 50% cue fires once at the halfway mark, then the ready
// cue fires once at expiry.
func TestHalfCooldownCue(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000) // Mend, 360s reuse
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, 1000)     // baseline: on cooldown, full
	e.Announce(fe, tr, Prefs{}, 1000+181) // crossed halfway (179s left ≤ 180)
	if len(fe.normal) != 1 || !strings.Contains(strings.ToLower(fe.normal[0]), "halfway") {
		t.Fatalf("halfway cue expected, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, 1000+182) // still past halfway → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("halfway cue should fire once, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, 1000+361) // ready
	if len(fe.normal) != 2 || !strings.Contains(fe.normal[1], "ready") {
		t.Fatalf("ready cue expected after halfway, got %v", fe.normal)
	}
}

// TestCooldownReadyCueDisabled: turning a skill's ready cue off silences it.
func TestCooldownReadyCueDisabled(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000)
	e := NewEngine()
	prefs := Prefs{Overrides: map[string]bool{
		cueCDReady("Mend"): false,
		cueCDHalf("Mend"):  false,
	}}
	e.Announce(fe, tr, prefs, 1000)
	e.Announce(fe, tr, prefs, 1000+181) // halfway, disabled
	e.Announce(fe, tr, prefs, 1000+361) // ready, disabled
	if len(fe.normal) != 0 {
		t.Errorf("disabled Mend cues should be silent, got %v", fe.normal)
	}
}

func TestCueIDForTimer(t *testing.T) {
	cases := []struct {
		tm   gamestate.Timer
		want string
	}{
		{gamestate.Timer{Mez: true}, cueMez},
		{gamestate.Timer{Pacify: true}, cuePacify},
		{gamestate.Timer{Detrimental: true}, cueDebuffFade},
		{gamestate.Timer{}, cueBuffFade},
		// mez takes precedence over the detrimental flag.
		{gamestate.Timer{Mez: true, Detrimental: true}, cueMez},
	}
	for _, c := range cases {
		if got := cueIDForTimer(c.tm); got != c.want {
			t.Errorf("cueIDForTimer(%+v) = %q, want %q", c.tm, got, c.want)
		}
	}
}

// TestJoinList renders natural list grammar.
func TestJoinList(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"A"}, "A"},
		{[]string{"A", "B"}, "A and B"},
		{[]string{"A", "B", "C"}, "A, B, and C"},
	}
	for _, c := range cases {
		if got := joinList(c.in); got != c.want {
			t.Errorf("joinList(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestComposeCueEmpty: with no due timers there's no phrase.
func TestComposeCueEmpty(t *testing.T) {
	if got := composeCue(ChatterNormal, nil, false, 0, 0); got != "" {
		t.Errorf("composeCue(nil) = %q, want empty", got)
	}
}

func TestAnnounceCHChainGapIsUrgent(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	base := time.Now().Unix() - 30
	// a steady 3s-cadence chain (≥3 intervals so cadence/gap are trusted)
	for i, c := range []struct{ cast, slot string }{
		{"Al", "1"}, {"Bo", "2"}, {"Cy", "3"}, {"Al", "1"}, {"Bo", "2"}, {"Cy", "3"},
		{"Al", "1"}, {"Bo", "2"}, {"Cy", "3"},
	} {
		tr.Observe(c.cast+" tells the guild, 'KCH - Rztank - "+c.slot+"'", base+int64(i*3))
	}
	e := NewEngine()

	// right after the last call — not in a gap yet → nothing spoken.
	e.Announce(fe, tr, Prefs{}, base+24)
	if len(fe.urgent) != 0 {
		t.Fatalf("no gap yet — unexpected urgent %v", fe.urgent)
	}
	// only 6s of silence on a ~3s-cadence chain → the early "gap forming" trigger
	// fires immediately (before the fully-broken threshold).
	now := base + 30
	e.Announce(fe, tr, Prefs{}, now)
	if len(fe.urgent) != 1 || !strings.Contains(strings.ToLower(fe.urgent[0]), "chain gap") {
		t.Fatalf("expected an urgent chain-gap cue, got urgent=%v normal=%v", fe.urgent, fe.normal)
	}
	// still gapping within the repeat window → must not spam.
	e.Announce(fe, tr, Prefs{}, now+5)
	if len(fe.urgent) != 1 {
		t.Errorf("chain gap must not repeat within %ds, got %v", chGapRepeatSec, fe.urgent)
	}
	// disabled via prefs → silent.
	fe2 := &fakeSpeaker{}
	e2 := NewEngine()
	e2.Announce(fe2, tr, Prefs{Overrides: map[string]bool{cueCHGap: false}}, now)
	if len(fe2.urgent) != 0 {
		t.Errorf("cue disabled in prefs should be silent, got %v", fe2.urgent)
	}
}

// --- CH-chain gap cue: business-logic edge cases ---

func chainTracker(t *testing.T) *gamestate.Tracker {
	t.Helper()
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	return gamestate.NewTracker(book)
}

// feedSteadyChain lays down a 3-cleric, 3s-cadence chain on target; last call at base+15.
func feedSteadyChain(tr *gamestate.Tracker, target string, base int64) {
	for i, c := range []struct{ cast, slot string }{
		{"Al", "1"}, {"Bo", "2"}, {"Cy", "3"}, {"Al", "1"}, {"Bo", "2"}, {"Cy", "3"},
		{"Al", "1"}, {"Bo", "2"}, {"Cy", "3"},
	} {
		tr.Observe(c.cast+" tells the guild, 'KCH - "+target+" - "+c.slot+"'", base+int64(i*3))
	}
}

// a chain that recovers must re-arm so a LATER gap alerts again (not swallowed by
// the per-target rate gate).
func TestCHChainGapCueRearms(t *testing.T) {
	fe := &fakeSpeaker{}
	tr := chainTracker(t)
	e := NewEngine()
	base := int64(1000)
	feedSteadyChain(tr, "Rztank", base) // last call base+15

	e.Announce(fe, tr, Prefs{}, base+30) // 6s silent → gap → alert #1
	if len(fe.urgent) != 1 {
		t.Fatalf("first gap should alert, got %v", fe.urgent)
	}
	tr.Observe("Al tells the guild, 'KCH - Rztank - 1'", base+31) // chain recovers
	e.Announce(fe, tr, Prefs{}, base+32)                          // healthy → no new alert
	if len(fe.urgent) != 1 {
		t.Fatalf("a recovered chain must not alert, got %v", fe.urgent)
	}
	// gaps again 8s after the recovery call — re-armed, so it alerts despite being
	// only ~9s after the first alert (inside the rate gate) — the re-arm, not the
	// clock, is what lets it fire.
	e.Announce(fe, tr, Prefs{}, base+38)
	if len(fe.urgent) != 2 {
		t.Errorf("a second gap after recovery should alert again, got %v", fe.urgent)
	}
}

// no cadence yet (a too-short chain) must NOT false-alarm on the trailing silence.
func TestCHChainGapCueNoCadenceNoAlarm(t *testing.T) {
	fe := &fakeSpeaker{}
	tr := chainTracker(t)
	e := NewEngine()
	base := int64(1000)
	tr.Observe("Al tells the guild, 'KCH - Rztank - 1'", base)
	tr.Observe("Bo tells the guild, 'KCH - Rztank - 2'", base+3) // only 1 interval → cadence untrusted
	e.Announce(fe, tr, Prefs{}, base+30)
	if len(fe.urgent) != 0 {
		t.Errorf("no alert without an established cadence, got %v", fe.urgent)
	}
}

// two tanks chained at once, both gapping → each alerts on its own (named).
func TestCHChainGapCueConcurrent(t *testing.T) {
	fe := &fakeSpeaker{}
	tr := chainTracker(t)
	e := NewEngine()
	base := int64(1000)
	seq := []struct{ cast, tgt, slot string }{
		{"Al", "TankA", "1"}, {"Mo", "TankB", "1"}, {"Bo", "TankA", "2"}, {"No", "TankB", "2"},
		{"Cy", "TankA", "3"}, {"Pe", "TankB", "3"}, {"Al", "TankA", "1"}, {"Mo", "TankB", "1"},
		{"Bo", "TankA", "2"}, {"No", "TankB", "2"}, {"Cy", "TankA", "3"}, {"Pe", "TankB", "3"},
		{"Al", "TankA", "1"}, {"Mo", "TankB", "1"}, {"Bo", "TankA", "2"}, {"No", "TankB", "2"},
		{"Cy", "TankA", "3"}, {"Pe", "TankB", "3"}, // 9 casts per tank (clears the sustained-chain bar)
	}
	for i, c := range seq {
		tr.Observe(c.cast+" tells the guild, 'KCH - "+c.tgt+" - "+c.slot+"'", base+int64(i))
	}
	e.Announce(fe, tr, Prefs{}, base+17+8) // both silent past forming → two alerts
	if len(fe.urgent) != 2 {
		t.Fatalf("two concurrent gapping chains should each alert, got %d: %v", len(fe.urgent), fe.urgent)
	}
	joined := strings.Join(fe.urgent, " | ")
	if !strings.Contains(joined, "TankA") || !strings.Contains(joined, "TankB") {
		t.Errorf("both tanks should be named across the alerts: %v", fe.urgent)
	}
}

func TestCHGapPhraseVariety(t *testing.T) {
	if got := chGapPhrase(ChatterQuiet, "Vulak", "Akayim", 0); got != "Chain gap!" {
		t.Errorf("quiet phrase = %q, want terse", got)
	}
	if got := chGapPhrase(ChatterNormal, "Vulak", "Akayim", 0); !strings.Contains(got, "Vulak") || !strings.Contains(got, "Akayim") {
		t.Errorf("normal phrase should name tank + next: %q", got)
	}
	if got := chGapPhrase(ChatterNormal, "Vulak", "", 0); !strings.Contains(got, "Vulak") {
		t.Errorf("normal phrase (no next) should still name the tank: %q", got)
	}
	seen := map[string]bool{}
	for i := range 6 {
		seen[chGapPhrase(ChatterChatty, "Vulak", "Akayim", i)] = true
	}
	if len(seen) < 2 {
		t.Errorf("chatty phrasing should vary across seq, got %d distinct", len(seen))
	}
}

// a chain with a trusted cadence but too few casts (a transient emergency burst)
// must NOT trigger the audio alert — only sustained chains do.
func TestCHChainGapCueNeedsSustainedChain(t *testing.T) {
	fe := &fakeSpeaker{}
	tr := chainTracker(t)
	e := NewEngine()
	base := int64(1000)
	// 5 calls → cadence trusted (4 intervals) but Casts=5 < chGapCueMinCasts
	for i, c := range []struct{ cast, slot string }{
		{"Al", "1"}, {"Bo", "2"}, {"Cy", "3"}, {"Al", "1"}, {"Bo", "2"},
	} {
		tr.Observe(c.cast+" tells the guild, 'KCH - Rztank - "+c.slot+"'", base+int64(i*3))
	}
	e.Announce(fe, tr, Prefs{}, base+12+8) // silent past forming, but chain too short
	if len(fe.urgent) != 0 {
		t.Errorf("a transient (short) chain's slip must not alert, got %v", fe.urgent)
	}
}
