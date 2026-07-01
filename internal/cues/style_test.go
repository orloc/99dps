package cues

import (
	"strings"
	"testing"
	"time"

	"99dps/internal/eq/gamestate"
)

// TestTimingSpawnWindows: each preset reports the documented spawn lead times,
// with Normal mirroring the package constants.
func TestTimingSpawnWindows(t *testing.T) {
	cases := []struct {
		t              Timing
		lead, imminent int64
	}{
		{TimingNormal, spawnLeadSec, spawnImminentSec},
		{TimingRelaxed, 30, spawnImminentSec},
		{TimingEarly, 120, 5},
	}
	for _, c := range cases {
		lead, imm := c.t.spawnWindows()
		if lead != c.lead || imm != c.imminent {
			t.Errorf("%s.spawnWindows() = (%d, %d), want (%d, %d)", c.t, lead, imm, c.lead, c.imminent)
		}
	}
}

// TestTimingEarlyFiresHeadsUpSooner: with the Early preset the calm heads-up
// fires 90s out (inside the 120s window) where Normal would stay silent until 60s.
func TestTimingEarlyFiresHeadsUpSooner(t *testing.T) {
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	now := time.Now().Unix()
	tr.Observe("You have entered east commonlands.", now)
	tr.UseOverrides(gamestate.LoadOverrides(t.TempDir() + "/ov.json"))
	tr.SetOverride("a griffon", 200)
	tr.Observe("You have slain a griffon!", now) // repop at now+200

	// Normal: 90s out is still outside the 60s window → silent.
	feN := &fakeSpeaker{}
	NewEngine().Announce(feN, tr, Prefs{Timing: TimingNormal}, now+110)
	if len(feN.normal) != 0 {
		t.Fatalf("Normal timing should stay silent 90s out, got %v", feN.normal)
	}
	// Early: 90s out is inside the 120s window → heads-up fires.
	feE := &fakeSpeaker{}
	NewEngine().Announce(feE, tr, Prefs{Timing: TimingEarly}, now+110)
	if len(feE.normal) != 1 || !strings.Contains(feE.normal[0], "griffon") {
		t.Fatalf("Early timing should warn 90s out, got %v", feE.normal)
	}
}

// TestChatterQuietSuppressesGentleTiers: Quiet drops the non-critical cues — the
// calm spawn heads-up and the cooldown-ready cue — while urgent alerts (resist)
// and the at-the-pop spawn alert still fire.
func TestChatterQuietSuppressesGentleTiers(t *testing.T) {
	now := time.Now().Unix()

	// spawn heads-up (gentle) is suppressed, but the imminent alert (urgent) fires.
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You have entered east commonlands.", now)
	tr.UseOverrides(gamestate.LoadOverrides(t.TempDir() + "/ov.json"))
	tr.SetOverride("a griffon", 120)
	tr.Observe("You have slain a griffon!", now) // repop at now+120
	fe := &fakeSpeaker{}
	e := NewEngine()
	quiet := Prefs{Chatter: ChatterQuiet}
	e.Announce(fe, tr, quiet, now+65) // 55s out — heads-up window, but Quiet
	if len(fe.normal) != 0 {
		t.Errorf("Quiet should suppress the spawn heads-up, got %v", fe.normal)
	}
	e.Announce(fe, tr, quiet, now+118) // 2s out — urgent alert still fires
	if len(fe.urgent) != 1 || !strings.Contains(fe.urgent[0], "griffon") {
		t.Errorf("Quiet must keep the urgent at-the-pop alert, got %v", fe.urgent)
	}

	// a cooldown-ready cue (gentle) is suppressed under Quiet.
	tr2 := gamestate.NewTracker(book)
	tr2.Observe("You mend your wounds and heal some damage.", now) // Mend → 360s
	fe2 := &fakeSpeaker{}
	e2 := NewEngine()
	e2.Announce(fe2, tr2, quiet, now)     // on cooldown
	e2.Announce(fe2, tr2, quiet, now+361) // ready, but Quiet
	if len(fe2.normal) != 0 {
		t.Errorf("Quiet should suppress the cooldown-ready cue, got %v", fe2.normal)
	}

	// a resist (urgent) still fires under Quiet.
	tr3 := gamestate.NewTracker(book)
	tr3.Observe("Your target resisted the Bedlam spell.", now)
	fe3 := &fakeSpeaker{}
	NewEngine().Announce(fe3, tr3, quiet, now)
	if len(fe3.urgent) != 1 || !strings.Contains(fe3.urgent[0], "Bedlam") {
		t.Errorf("Quiet must keep the urgent resist alert, got %v", fe3.urgent)
	}
}

// TestChatterWordingLength: for the cues that actually fire under Quiet (the
// fading + urgent tier), the terse Quiet wording is shorter than the verbose
// Chatty wording — the audible "less/more chatty". (The gentle-tier builders are
// suppressed entirely under Quiet, so they carry no terse form to compare.)
func TestChatterWordingLength(t *testing.T) {
	due := []gamestate.Timer{{Spell: "Clarity", Target: "You", Expiry: 300}}
	mobs := []string{"a griffon"}
	checks := []struct {
		name          string
		quiet, chatty string
	}{
		{"fading", fadingPhrase(ChatterQuiet, due, 0), fadingPhrase(ChatterChatty, due, 0)},
		{"spawnNow", spawnNowPhrase(ChatterQuiet, mobs, 0), spawnNowPhrase(ChatterChatty, mobs, 0)},
		{"charm", charmBreakPhrase(ChatterQuiet, 0), charmBreakPhrase(ChatterChatty, 0)},
		{"feign", feignFailPhrase(ChatterQuiet, 0), feignFailPhrase(ChatterChatty, 0)},
		{"resist", resistPhrase(ChatterQuiet, "Bedlam", 0), resistPhrase(ChatterChatty, "Bedlam", 0)},
	}
	for _, c := range checks {
		if len(c.quiet) >= len(c.chatty) {
			t.Errorf("%s: quiet %q should be shorter than chatty %q", c.name, c.quiet, c.chatty)
		}
	}
}
