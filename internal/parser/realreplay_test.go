package parser

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// TestObserveSpells_MonkClickyBuffTracked reproduces the reported case verbatim
// from Kelkix's log: a level-53 monk (whose /who title is "Disciple") right-clicks
// White Lotus Pants, which logs a real "You begin casting Spirit of Ox." — but the
// CLICKY casts ~20s (far longer than the spell's listed 5s), so the landing emote
// arrives 20s later, with an unrelated line in between. The class must resolve to
// Monk (melee) and the buff must be a live self timer.
//
// Two bugs this guards: (1) "Disciple" was unmapped → 51-54 monk read as caster;
// (2) the landing arrived past the stale-pending window (5s cast + 12s) and was
// dropped before the emote was matched — so no buff ever showed.
func TestObserveSpells_MonkClickyBuffTracked(t *testing.T) {
	book, err := gamestate.LoadReader(strings.NewReader(spellRow(map[int]string{
		1:  "Spirit of Ox",
		6:  "You feel the spirit of ox enter you.", // cast_on_you
		13: "5000",                                 // the SPELL lists a 5s cast…
		16: "3",                                    // duration formula
		17: "450",                                  // duration cap
		83: "1",                                    // beneficial
	})))
	if err != nil {
		t.Fatal(err)
	}
	tr := gamestate.NewTracker(book)
	p := DmgParser{character: "Kelkix", tracker: tr}

	// …but the White Lotus Pants clicky takes ~20s, so the land is 20s after cast.
	p.observeSpells("[Thu Jun 11 03:26:43 2026] [53 Disciple] Kelkix (Iksar) <Kingdom>")
	p.observeSpells("[Thu Jun 11 03:26:47 2026] You begin casting Spirit of Ox.")
	p.observeSpells("[Thu Jun 11 03:26:47 2026] Your White Lotus Pants begins to glow.")
	p.observeSpells("[Thu Jun 11 03:26:52 2026] a pickclaw seer begins to cast a spell.")
	p.observeSpells("[Thu Jun 11 03:27:07 2026] You feel the spirit of ox enter you.")

	if tr.Class() != eqclass.ClassMonk {
		t.Fatalf("a 'Disciple' /who should detect Monk; got %v", tr.Class())
	}
	if tr.Category() != eqclass.CatMelee {
		t.Errorf("a monk should be the melee category (so it gets the Skills + Buffs layout); got %v", tr.Category())
	}
	landTs, _ := parseTimestamp("[Thu Jun 11 03:27:07 2026] x")
	act := tr.Active(landTs + 1)
	if len(act) != 1 || act[0].Spell != "Spirit of Ox" || act[0].Target != "You" {
		t.Fatalf("the clicked Spirit of Ox should be a live self-buff timer despite the ~20s clicky cast; got %+v", act)
	}
}

// These tests replay VERBATIM real P99 log excerpts (internal/parser/testdata)
// through the full pipeline — DmgParser.dispatch + observeSpells, exactly as
// DoParse does — into a real SessionManager. They guard end-to-end behavior on
// real data: that parsing/attribution didn't break, and that session start/stop
// (segmentation) lands where a player would expect. The synthetic single-fight
// golden lives in TestDispatch_GoldenReplay; these cover multi-encounter
// segmentation and boundaries on actual logs.

// replayLog drives raw log lines through the parser into a fresh SessionManager,
// mirroring DoParse line-for-line (CRLF already stripped here).
func replayLog(t *testing.T, lines []string, character string, tr *gamestate.Tracker) *session.SessionManager {
	t.Helper()
	sm := &session.SessionManager{}
	p := DmgParser{character: character, tracker: tr}
	for _, ln := range lines {
		text := strings.TrimRight(ln, "\r\n")
		p.dispatch(text, sm)
		if tr != nil {
			p.observeSpells(text)
		}
	}
	return sm
}

func loadFixture(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// playerOutgoing matches a line where the log owner ("You") deals melee damage —
// "You crush a saltwater croc for 88 points of damage." The verb is lower-case
// and the line starts (after the timestamp) with "You ", so incoming hits
// ("A saltwater croc bites YOU for 38…") never match.
var playerOutgoing = regexp.MustCompile(`\] You [a-z]+ .* for (\d+) points of damage`)

func sumPlayerOutgoing(lines []string) int {
	total := 0
	for _, ln := range lines {
		if m := playerOutgoing.FindStringSubmatch(ln); m != nil {
			n, _ := strconv.Atoi(m[1])
			total += n
		}
	}
	return total
}

// dealerTotal returns a named dealer's melee total in a session (GetAggressors
// is a flat slice of value copies).
func dealerTotal(s *session.CombatSession, dealer string) int {
	for _, ds := range s.GetAggressors() {
		if ds.Dealer == dealer {
			return ds.Total
		}
	}
	return 0
}

// TestReplay_TwoFights_Segmentation is the flagship: a real "saltwater croc"
// fight, a multi-minute gap full of non-combat noise (guild chat, a mob casting,
// a skill-up, camp prep), then a real "CWG Model CA" fight. The pipeline must see
// exactly two encounters, named after the mobs fought, with the gap's noise
// neither splitting a fight nor inventing a session.
func TestReplay_TwoFights_Segmentation(t *testing.T) {
	lines := loadFixture(t, "two_fights.log")
	sm := replayLog(t, lines, "Kelkix", nil)

	all := sm.All()
	if len(all) != 2 {
		names := make([]string, len(all))
		for i, s := range all {
			names[i] = s.Name()
		}
		t.Fatalf("want 2 encounters (croc + CWG), got %d: %v", len(all), names)
	}
	if got := all[0].Name(); !strings.Contains(got, "saltwater croc") {
		t.Errorf("first encounter name = %q, want it to mention the saltwater croc", got)
	}
	if got := all[1].Name(); got != "CWG Model CA" {
		t.Errorf("second encounter name = %q, want \"CWG Model CA\"", got)
	}
	// the first fight is closed (gap + a camp-prep zone boundary); the second is
	// the live tail.
	if all[0].EndTime().IsZero() {
		t.Error("the first fight should be marked ended once the second begins")
	}
}

// TestReplay_DamageAttribution cross-checks the whole pipeline against the raw
// fixture: the player's melee total summed across all encounters must equal the
// sum of every "You … for N points of damage" line in the log. A regression in
// parsing, dealer keying, or segmentation would drift these apart.
func TestReplay_DamageAttribution(t *testing.T) {
	lines := loadFixture(t, "two_fights.log")
	sm := replayLog(t, lines, "Kelkix", nil)

	want := sumPlayerOutgoing(lines)
	if want == 0 {
		t.Fatal("fixture sanity: found no player damage lines")
	}
	got := 0
	for _, s := range sm.All() {
		got += dealerTotal(s, "You")
	}
	if got != want {
		t.Errorf("player melee total across encounters = %d, want %d (sum of the raw log lines)", got, want)
	}
}

// TestReplay_Invariants is the broad "didn't break anything" guard: replaying
// real combat must leave every encounter internally consistent — a non-empty
// name, a positive total, and per-dealer melee totals that don't exceed the
// encounter total (which also folds in enemy non-melee).
func TestReplay_Invariants(t *testing.T) {
	for _, fx := range []string{"two_fights.log", "player_death.log"} {
		sm := replayLog(t, loadFixture(t, fx), "Kelkix", nil)
		all := sm.All()
		if len(all) == 0 {
			t.Errorf("%s: produced no sessions", fx)
			continue
		}
		for i, s := range all {
			if s.Name() == "" {
				t.Errorf("%s: session %d has an empty name", fx, i)
			}
			if s.Total() <= 0 {
				t.Errorf("%s: session %d total = %d, want > 0", fx, i, s.Total())
			}
			sum := 0
			for _, ds := range s.GetAggressors() {
				if ds.Total < 0 {
					t.Errorf("%s: session %d dealer %q has negative total %d", fx, i, ds.Dealer, ds.Total)
				}
				sum += ds.Total
			}
			if sum > s.Total() {
				t.Errorf("%s: session %d per-dealer sum %d exceeds total %d", fx, i, sum, s.Total())
			}
		}
	}
}

// TestReplay_DeathClosesSession: a real fight where the mob kills the player must
// close the encounter at the death (a hard boundary), not leave it open.
func TestReplay_DeathClosesSession(t *testing.T) {
	sm := replayLog(t, loadFixture(t, "player_death.log"), "Kelkix", nil)
	all := sm.All()
	if len(all) == 0 {
		t.Fatal("the imp-protector fight should have opened a session")
	}
	last := all[len(all)-1]
	if last.EndTime().IsZero() {
		t.Errorf("the player's death should close the encounter, but it's still open (%q)", last.Name())
	}
}

// TestReplay_DrolvargCampOneSession: a real camp hunt from Kelkas's log — the
// same drolvargs over ~6 minutes with medding/CC lulls of 29s, 118s and 161s
// between bursts. Each lull exceeds the idle threshold, but re-engaging the same
// camp keeps it ONE session instead of fragmenting into ~5.
func TestReplay_DrolvargCampOneSession(t *testing.T) {
	sm := replayLog(t, loadFixture(t, "drolvarg_camp.log"), "Kelkas", nil)
	all := sm.All()
	if len(all) != 1 {
		names := make([]string, len(all))
		for i, s := range all {
			names[i] = s.Name()
		}
		t.Fatalf("the drolvarg camp should be one session; got %d: %v", len(all), names)
	}
	if !strings.Contains(all[0].Name(), "drolvarg") {
		t.Errorf("the session should be named after the camp; got %q", all[0].Name())
	}
}
