package tui

import (
	"strings"
	"testing"
	"time"

	"99dps/internal/gamestate"
	"99dps/internal/tts"
)

// TestDueAnnouncementsTwoLevels: a buff cues once on entering the gold "low" zone
// and again on the red "fading" zone (the same TimerUrgency the flash uses), and a
// refresh re-arms both levels.
func TestDueAnnouncementsTwoLevels(t *testing.T) {
	m := &Model{announced: map[string]gamestate.Urgency{}}
	now := int64(1_000_000)
	// a 100-min buff: low (gold) caps at 300s left, fading (red) at 60s left.
	buff := func(remaining int64) gamestate.Timer {
		return gamestate.Timer{Spell: "Clarity", Target: "You", Start: now - (6000 - remaining), Expiry: now + remaining}
	}

	if got := m.dueAnnouncements([]gamestate.Timer{buff(400)}, now); len(got) != 0 {
		t.Fatalf("fresh buff (400s left) should be silent, got %v", got)
	}
	got := m.dueAnnouncements([]gamestate.Timer{buff(250)}, now)
	if len(got) != 1 || got[0].fading {
		t.Fatalf("entering the low zone should give one non-fading cue, got %+v", got)
	}
	if got := m.dueAnnouncements([]gamestate.Timer{buff(250)}, now); len(got) != 0 {
		t.Errorf("still low should not re-announce, got %v", got)
	}
	got = m.dueAnnouncements([]gamestate.Timer{buff(50)}, now)
	if len(got) != 1 || !got[0].fading {
		t.Fatalf("entering the red zone should give one fading cue, got %+v", got)
	}
	// refreshed back to fresh → re-arm; decaying to low warns "low" again
	m.dueAnnouncements([]gamestate.Timer{buff(400)}, now)
	if got := m.dueAnnouncements([]gamestate.Timer{buff(250)}, now); len(got) != 1 || got[0].fading {
		t.Errorf("a refreshed buff should warn low again, got %+v", got)
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
		if got := composeCue(c.due, true, 0); got != c.want {
			t.Errorf("composeCue = %q, want %q", got, c.want)
		}
	}
}

// TestComposeCueLow: the gold-zone verbiage reads "running low", distinct from the
// red-zone "fading".
func TestComposeCueLow(t *testing.T) {
	due := []gamestate.Timer{{Spell: "Clarity", Target: "You"}}
	if got := composeCue(due, false, 0); got != "Clarity is running low." {
		t.Errorf("low cue = %q, want \"Clarity is running low.\"", got)
	}
}

func TestFadeVariety(t *testing.T) {
	due := []gamestate.Timer{{Spell: "Clarity", Target: "You"}}
	seen := map[string]bool{}
	for seq := 0; seq < 3; seq++ {
		seen[composeCue(due, true, seq)] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct phrasings across seqs, got %v", seen)
	}
	// plural agreement holds in every style
	two := []gamestate.Timer{{Spell: "Clarity", Target: "You"}, {Spell: "Umbra", Target: "You"}}
	for seq := 0; seq < 3; seq++ {
		if got := composeCue(two, true, seq); !strings.Contains(got, "Clarity and Umbra") || !strings.Contains(got, "are") {
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

// fakeEngine records which cues went to the normal vs urgent voice, and how many
// times the queue was flushed.
type fakeEngine struct {
	normal, urgent []string
	flushed        int
}

func (f *fakeEngine) Say(s string)         { f.normal = append(f.normal, s) }
func (f *fakeEngine) SayUrgent(s string)   { f.urgent = append(f.urgent, s) }
func (f *fakeEngine) Available() bool      { return true }
func (f *fakeEngine) Voices() []tts.Voice  { return nil }
func (f *fakeEngine) Voice() string        { return "" }
func (f *fakeEngine) SetVoice(string) bool { return false }
func (f *fakeEngine) Flush()               { f.flushed++; f.normal = nil }

func TestAnnounceCuesResistIsUrgent(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	now := time.Now().Unix()
	tr.Observe("Your target resisted the Bedlam spell.", now) // sets the resist signal

	m := &Model{ttsOn: true, tracker: tr, speaker: fe, announced: map[string]gamestate.Urgency{}}
	m.announceCues()
	if len(fe.urgent) != 1 || !strings.Contains(fe.urgent[0], "Bedlam") {
		t.Fatalf("resist should use the urgent voice with the spell name, got urgent=%v normal=%v", fe.urgent, fe.normal)
	}
	if len(fe.normal) != 0 {
		t.Errorf("resist must not use the normal voice, got %v", fe.normal)
	}
	m.announceCues() // same resist still within grace → must not repeat
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
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.FeignFailed(1000)
	m := &Model{ttsOn: true, tracker: tr, speaker: fe, announced: map[string]gamestate.Urgency{}, cdReady: map[string]bool{}}

	m.announceCuesAt(1000)
	if len(fe.urgent) != 1 || !strings.Contains(strings.ToLower(fe.urgent[0]), "feign") {
		t.Fatalf("failed feign should be urgent, got urgent=%v normal=%v", fe.urgent, fe.normal)
	}
	m.announceCuesAt(1000) // still failed → no repeat
	if len(fe.urgent) != 1 {
		t.Errorf("feign fail should fire once, got %v", fe.urgent)
	}
}

func TestLongCooldownReadyCue(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000) // Mend → 360s cooldown
	m := &Model{ttsOn: true, tracker: tr, speaker: fe, announced: map[string]gamestate.Urgency{}, cdReady: map[string]bool{}}

	m.announceCuesAt(1000) // just used → records not-ready, no cue
	if len(fe.normal) != 0 {
		t.Fatalf("no cue while on cooldown, got %v", fe.normal)
	}
	m.announceCuesAt(1000 + 361) // ready → gentle cue
	if len(fe.normal) != 1 || !strings.Contains(fe.normal[0], "Mend") {
		t.Fatalf("Mend ready should be a gentle cue, got normal=%v urgent=%v", fe.normal, fe.urgent)
	}
	m.announceCuesAt(1000 + 362) // still ready → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("cooldown-ready should fire once, got %v", fe.normal)
	}
}

func TestShortCooldownNoCue(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.FeignAttempt(2000) // Feign Death → 11s reuse (short)
	m := &Model{ttsOn: true, tracker: tr, speaker: fe, announced: map[string]gamestate.Urgency{}, cdReady: map[string]bool{}}

	m.announceCuesAt(2000)
	m.announceCuesAt(2000 + 12) // ready, but too short to announce
	if len(fe.normal) != 0 {
		t.Errorf("a short cooldown must not announce, got %v", fe.normal)
	}
}

func TestDueAnnouncementsSkipsEstimated(t *testing.T) {
	m := &Model{announced: map[string]gamestate.Urgency{}}
	now := int64(1_000)
	// an incoming debuff on you, near (estimated) expiry — must NOT announce
	est := gamestate.Timer{Spell: "Slowed", Target: "You", Start: now - 360, Expiry: now + 5, Estimated: true}
	if got := m.dueAnnouncements([]gamestate.Timer{est}, now); len(got) != 0 {
		t.Errorf("estimated incoming debuff should not produce a fade cue, got %v", got)
	}
	// a real buff at the same remaining still announces (sanity)
	buff := gamestate.Timer{Spell: "Clarity", Target: "You", Start: now - 360, Expiry: now + 5}
	if got := m.dueAnnouncements([]gamestate.Timer{buff}, now); len(got) != 1 {
		t.Errorf("a real buff should still announce, got %v", got)
	}
}
