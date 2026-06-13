package tui

import (
	"strings"
	"testing"
	"time"

	"99dps/internal/gamestate"
	"99dps/internal/tts"
)

func TestWarnLeadSec(t *testing.T) {
	cases := []struct {
		total, want int64
	}{
		{60, 15},    // 1m buff: 10% = 6s → floored to 15s
		{300, 30},   // 5m: 30s
		{900, 90},   // 15m: 90s
		{6006, 180}, // ~100m (Gift of Brilliance): capped at 180s, not 15s
		{5946, 180}, // ~99m (Umbra): capped at 180s
		{10, 15},    // degenerate short: floor
	}
	for _, c := range cases {
		if got := warnLeadSec(c.total); got != c.want {
			t.Errorf("warnLeadSec(%d) = %d, want %d", c.total, got, c.want)
		}
	}
}

func TestDueAnnouncementsScalesByDuration(t *testing.T) {
	m := &Model{announced: map[string]bool{}}
	now := int64(1_000_000)

	// a 100-min buff with 2 minutes (120s) left: that's inside the 180s lead → fires
	longBuff := gamestate.Timer{Spell: "Gift of Brilliance", Target: "You", Start: now - 5886, Expiry: now + 120}
	if got := m.dueAnnouncements([]gamestate.Timer{longBuff}, now); len(got) != 1 {
		t.Fatalf("long buff at 120s left should warn (180s lead), got %v", got)
	}
	// fires once only
	if got := m.dueAnnouncements([]gamestate.Timer{longBuff}, now); len(got) != 0 {
		t.Errorf("should not re-announce the same timer, got %v", got)
	}

	// a short 60s debuff with 30s left is NOT yet in its 15s lead → silent
	m2 := &Model{announced: map[string]bool{}}
	shortDot := gamestate.Timer{Spell: "Snare", Target: "a gnoll", Start: now - 30, Expiry: now + 30}
	if got := m2.dueAnnouncements([]gamestate.Timer{shortDot}, now); len(got) != 0 {
		t.Errorf("short debuff at 30s left should stay silent (15s lead), got %v", got)
	}
	// ...but at 15s left it fires
	shortDot.Expiry = now + 15
	if got := m2.dueAnnouncements([]gamestate.Timer{shortDot}, now); len(got) != 1 {
		t.Errorf("short debuff at 15s left should warn, got %v", got)
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
		if got := composeCue(c.due, 0); got != c.want {
			t.Errorf("composeCue = %q, want %q", got, c.want)
		}
	}
}

func TestFadeVariety(t *testing.T) {
	due := []gamestate.Timer{{Spell: "Clarity", Target: "You"}}
	seen := map[string]bool{}
	for seq := 0; seq < 3; seq++ {
		seen[composeCue(due, seq)] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct phrasings across seqs, got %v", seen)
	}
	// plural agreement holds in every style
	two := []gamestate.Timer{{Spell: "Clarity", Target: "You"}, {Spell: "Umbra", Target: "You"}}
	for seq := 0; seq < 3; seq++ {
		if got := composeCue(two, seq); !strings.Contains(got, "Clarity and Umbra") || !strings.Contains(got, "are") {
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

// fakeEngine records which cues went to the normal vs urgent voice.
type fakeEngine struct{ normal, urgent []string }

func (f *fakeEngine) Say(s string)         { f.normal = append(f.normal, s) }
func (f *fakeEngine) SayUrgent(s string)   { f.urgent = append(f.urgent, s) }
func (f *fakeEngine) Available() bool      { return true }
func (f *fakeEngine) Voices() []tts.Voice  { return nil }
func (f *fakeEngine) Voice() string        { return "" }
func (f *fakeEngine) SetVoice(string) bool { return false }

func TestAnnounceCuesResistIsUrgent(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	now := time.Now().Unix()
	tr.Observe("Your target resisted the Bedlam spell.", now) // sets the resist signal

	m := &Model{ttsOn: true, tracker: tr, speaker: fe, announced: map[string]bool{}}
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
