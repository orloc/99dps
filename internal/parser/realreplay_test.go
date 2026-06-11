package parser

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"99dps/internal/gamestate"
	"99dps/internal/session"
)

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
