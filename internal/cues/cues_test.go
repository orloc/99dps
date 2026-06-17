package cues

import (
	"strings"
	"testing"
	"time"

	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
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
		if got := composeCue(c.due, true, 0, 0); got != c.want {
			t.Errorf("composeCue = %q, want %q", got, c.want)
		}
	}
}

// TestComposeCueLow: the gold-zone cue names the buff, reads "low" (not "fading"),
// and speaks the time remaining.
func TestComposeCueLow(t *testing.T) {
	now := int64(1000)
	due := []gamestate.Timer{{Spell: "Clarity", Target: "You", Expiry: now + 300}} // 5 min left
	got := composeCue(due, false, now, 0)
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
	two := composeCue([]gamestate.Timer{tm("Clarity"), tm("Aegolism")}, false, now, 0)
	if !strings.Contains(two, "Clarity") || !strings.Contains(two, "Aegolism") {
		t.Errorf("two low buffs should combine into one cue: %q", two)
	}
	many := composeCue([]gamestate.Timer{tm("A"), tm("B"), tm("C"), tm("D")}, true, now, 0)
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
		seen[composeCue(due, true, 0, seq)] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct phrasings across seqs, got %v", seen)
	}
	// plural agreement holds in every style
	two := []gamestate.Timer{{Spell: "Clarity", Target: "You"}, {Spell: "Umbra", Target: "You"}}
	for seq := 0; seq < 3; seq++ {
		if got := composeCue(two, true, 0, seq); !strings.Contains(got, "Clarity and Umbra") || !strings.Contains(got, "are") {
			t.Errorf("seq %d plural phrasing wrong: %q", seq, got)
		}
	}
}

func TestCharmAndResistPhrases(t *testing.T) {
	charm := map[string]bool{}
	for seq := 0; seq < 3; seq++ {
		charm[charmBreakPhrase(seq)] = true
	}
	if len(charm) != 3 {
		t.Errorf("charm-break phrasing should vary, got %v", charm)
	}
	for seq := 0; seq < 3; seq++ {
		if got := resistPhrase("Bedlam", seq); !strings.Contains(got, "Bedlam") {
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

func TestFeignFailVariety(t *testing.T) {
	seen := map[string]bool{}
	for seq := 0; seq < 3; seq++ {
		seen[feignFailPhrase(seq)] = true
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

// TestSpawnSoonCue: a tracked repop cues once when it crosses ~30s out, and the
// toggle silences it.
func TestSpawnSoonCue(t *testing.T) {
	fe := &fakeSpeaker{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	now := time.Now().Unix()
	tr.Observe("You have entered east commonlands.", now)
	tr.UseOverrides(gamestate.LoadOverrides(t.TempDir() + "/ov.json"))
	tr.SetOverride("a griffon", 60)              // 60s repop
	tr.Observe("You have slain a griffon!", now) // repop at now+60
	e := NewEngine()

	e.Announce(fe, tr, Prefs{}, now+20) // 40s out → not yet in the 30s window
	if len(fe.normal) != 0 {
		t.Fatalf("no spawn cue at 40s out, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, now+35) // 25s out → fires
	if len(fe.normal) != 1 || !strings.Contains(fe.normal[0], "griffon") {
		t.Fatalf("spawn cue expected ~30s out, got %v", fe.normal)
	}
	e.Announce(fe, tr, Prefs{}, now+36) // still in window → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("spawn cue should fire once per spawn, got %v", fe.normal)
	}

	// the toggle silences it: a fresh kill with the cue disabled stays quiet.
	fe2 := &fakeSpeaker{}
	e2 := NewEngine()
	prefs := Prefs{Overrides: map[string]bool{cueSpawnSoon: false}}
	tr.SetOverride("a griffon", 60)
	tr.Observe("You have slain a griffon!", now+200) // a second spawn, repop at now+260
	e2.Announce(fe2, tr, prefs, now+235)             // 25s out, but disabled
	if len(fe2.normal) != 0 {
		t.Errorf("disabled spawn cue should stay silent, got %v", fe2.normal)
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
	if got := composeCue(nil, false, 0, 0); got != "" {
		t.Errorf("composeCue(nil) = %q, want empty", got)
	}
}
