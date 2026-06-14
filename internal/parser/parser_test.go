package parser

import (
	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regression: log timestamps carry no zone and are the player's LOCAL time, so
// they must parse in time.Local — not UTC — or wall-clock comparisons (spell
// timers vs time.Now) land an offset away and every timer reads as expired.
func TestParseTimestamp_Local(t *testing.T) {
	got, err := parseTimestamp("[Sat Jun 06 22:55:24 2026] anything")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.June, 6, 22, 55, 24, 0, time.Local).Unix()
	if got != want {
		t.Errorf("parseTimestamp = %d, want %d (local time)", got, want)
	}
}

// spellRow builds a 217-field spells_us.txt line with the given fields set.
func spellRow(set map[int]string) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	for i, v := range set {
		f[i] = v
	}
	return strings.Join(f, "^")
}

// Regression: EQ logs are CRLF. The trailing \r must not break the exact/suffix
// match used for spell landing emotes — otherwise no timer ever starts.
func TestObserveSpells_CRLFLandingStartsTimer(t *testing.T) {
	book, err := gamestate.LoadReader(strings.NewReader(spellRow(map[int]string{
		1:  "Bedlam",
		6:  "Your eyes gleam with bedlam.", // cast_on_you
		7:  "'s eyes gleam with bedlam.",   // cast_on_other
		13: "3000",                         // cast time
		16: "8",                            // duration formula
		17: "75",                           // duration cap
		83: "1",                            // beneficial
	})))
	if err != nil {
		t.Fatal(err)
	}

	tr := gamestate.NewTracker(book)
	tr.SetLevel(50)
	p := &DmgParser{character: "Iznoa", tracker: tr}

	// note the trailing \r on every line, as in real EQ logs
	p.observeSpells("[Sat Jun 06 21:13:27 2026] You begin casting Bedlam.\r")
	p.observeSpells("[Sat Jun 06 21:13:30 2026] Your eyes gleam with bedlam.\r")

	// "now" parsed the same way as the log line, so the test is TZ-independent
	landing, _ := parseTimestamp("[Sat Jun 06 21:13:30 2026] x")
	act := tr.Active(landing + 10)
	if len(act) != 1 {
		t.Fatalf("expected 1 active timer, got %d", len(act))
	}
	if act[0].Spell != "Bedlam" || act[0].Target != "You" {
		t.Errorf("timer = %+v, want Bedlam on You", act[0])
	}
}

// parseSwingLine is a test helper around parseSwing.
func parseSwingLine(t *testing.T, subject string) *combat.Swing {
	t.Helper()
	p := &DmgParser{}
	full := fakeTS + subject
	if !p.hasSwing(full) {
		t.Fatalf("hasSwing(%q) = false, want true", subject)
	}
	sw, err := p.parseSwing(full)
	if err != nil {
		t.Fatalf("parseSwing(%q) failed: %v", subject, err)
	}
	return sw
}

func TestParseSwing_Outcomes(t *testing.T) {
	cases := []struct {
		subject  string
		attacker string
		defender string
		outcome  combat.SwingOutcome
	}{
		{"A saltwater croc tries to bite YOU, but misses!", "A saltwater croc", "YOU", combat.OutcomeMiss},
		{"You try to pierce a saltwater croc, but miss!", "You", "a saltwater croc", combat.OutcomeMiss},
		{"A saltwater croc tries to bite YOU, but YOU block!", "A saltwater croc", "YOU", combat.OutcomeBlock},
		{"A saltwater croc tries to kick YOU, but YOU dodge!", "A saltwater croc", "YOU", combat.OutcomeDodge},
		{"Argoni tries to slash imp protector, but imp protector parries!", "Argoni", "imp protector", combat.OutcomeParry},
		{"A saltwater croc tries to bite YOU, but YOU riposte!", "A saltwater croc", "YOU", combat.OutcomeRiposte},
		{"A sand giant tries to crush YOU, but YOUR magical skin absorbs the blow!", "A sand giant", "YOU", combat.OutcomeAbsorb},
	}

	for _, c := range cases {
		sw := parseSwingLine(t, c.subject)
		if sw.Attacker != c.attacker {
			t.Errorf("%q: attacker = %q, want %q", c.subject, sw.Attacker, c.attacker)
		}
		if sw.Defender != c.defender {
			t.Errorf("%q: defender = %q, want %q", c.subject, sw.Defender, c.defender)
		}
		if sw.Outcome != c.outcome {
			t.Errorf("%q: outcome = %d, want %d", c.subject, sw.Outcome, c.outcome)
		}
	}
}

func TestParseDamage_CapturesSpecialVerb(t *testing.T) {
	p := &DmgParser{}
	set, err := p.parseDamage(fakeTS + "Varobn backstabs Nallar for 174 points of damage.")
	if err != nil {
		t.Fatalf("parseDamage failed: %v", err)
	}
	if set.Verb != "backstabs" {
		t.Errorf("verb = %q, want %q", set.Verb, "backstabs")
	}
}

func TestParseCrit(t *testing.T) {
	p := &DmgParser{character: "Kelkix"}

	cr, err := p.parseCrit(fakeTS + "Naku Scores a critical hit!(34)")
	if err != nil {
		t.Fatalf("parseCrit failed: %v", err)
	}
	if cr.Attacker != "Naku" || cr.Damage != 34 {
		t.Errorf("got attacker=%q dmg=%d, want Naku/34", cr.Attacker, cr.Damage)
	}

	// the log owner's own crits log under their name; they must map to "You"
	own, err := p.parseCrit(fakeTS + "Kelkix Scores a critical hit!(99)")
	if err != nil {
		t.Fatalf("parseCrit(owner) failed: %v", err)
	}
	if own.Attacker != "You" {
		t.Errorf("owner crit attacker = %q, want %q", own.Attacker, "You")
	}
}

func TestParseEvent(t *testing.T) {
	p := &DmgParser{}
	cases := []struct {
		subject string
		kind    combat.EventKind
	}{
		{"You have slain a saltwater croc!", combat.EventKill},
		{"You gain party experience!!", combat.EventPartyXP},
		{"You gain experience!!", combat.EventXP},
		{"You have been slain by The Avatar of War!", combat.EventDeath},
		{"You have entered the Greater Faydark.", combat.EventZone},
		{"It will take you about 30 seconds to prepare your camp.", combat.EventZone},
	}
	for _, c := range cases {
		ev, err := p.parseEvent(fakeTS + c.subject)
		if err != nil {
			t.Fatalf("parseEvent(%q) failed: %v", c.subject, err)
		}
		if ev.Kind != c.kind {
			t.Errorf("%q: kind = %d, want %d", c.subject, ev.Kind, c.kind)
		}
	}
}

// Regression: a non-melee (spell) line contains "points of damage" and must NOT
// be classified as melee damage — otherwise the melee regex invents a bogus
// "<X> was" dealer. It must route to the magic parser instead.
func TestNonMeleeRoutesToMagicNotDamage(t *testing.T) {
	p := &DmgParser{}
	line := fakeTS + "a sand giant was hit by non-melee for 4 points of damage."

	if p.hasDamage(line) {
		t.Errorf("non-melee line wrongly classified as melee damage")
	}
	if !p.hasMagic(line) {
		t.Fatalf("non-melee line not classified as magic")
	}
	m, err := p.parseMagic(line)
	if err != nil {
		t.Fatal(err)
	}
	if m.Target != "a sand giant" || m.Dmg != 4 {
		t.Errorf("parseMagic = %+v, want target='a sand giant' dmg=4", m)
	}
}

func TestRebuildTrackerFromFile(t *testing.T) {
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)

	path := filepath.Join(t.TempDir(), "eqlog_Kelkix_test.txt")
	lines := fakeTS + "[60 Warlord] Kelkix (Troll)\n" +
		fakeTS + "You have entered Greater Faydark.\n"
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	RebuildTrackerFromFile(path, "Kelkix", tr)

	if tr.Class() != eqclass.ClassWarrior || tr.Level() != 60 {
		t.Errorf("class/level not recovered from file: %v L%d", tr.Class(), tr.Level())
	}
	if tr.Zone() != "Greater Faydark" {
		t.Errorf("zone not recovered from file: %q", tr.Zone())
	}
}

func TestIsFeignMacro(t *testing.T) {
	p := &DmgParser{character: "Kelkix"}
	if !p.isFeignMacro("Kelkix looks dead...") {
		t.Error("should detect the player's own feign macro")
	}
	if p.isFeignMacro("Someguy looks dead...") {
		t.Error("another player's emote must not trip the macro")
	}
	if p.isFeignMacro("Kelkix says, 'hello'") {
		t.Error("an unrelated line from the player should not match")
	}
}

func TestIsOwnFeignFail(t *testing.T) {
	p := &DmgParser{character: "Kelkix"}
	if !p.isOwnFeignFail("Kelkix has fallen to the ground.") {
		t.Error("should detect the player's own failed feign")
	}
	if !p.isOwnFeignFail("You have fallen to the ground.") {
		t.Error("should detect the 'You' self form")
	}
	if p.isOwnFeignFail("Dancogar has fallen to the ground.") {
		t.Error("another monk's fail must not trip the alert")
	}
}

func TestParseLevel(t *testing.T) {
	p := &DmgParser{character: "Kelkix"}

	// /who self: level + class from the title (here the base name "Wizard")
	if lvl, cls, ok := p.parseLevel("[34 Wizard] Kelkix (Gnome) <Kingdom> ZONE: unrest"); !ok || lvl != 34 || cls != eqclass.ClassWizard {
		t.Errorf("/who self = %d,%v,%v, want 34,Wizard,true", lvl, cls, ok)
	}
	// a level-title that isn't the base name still resolves the class
	if lvl, cls, ok := p.parseLevel("[60 Warlord] Kelkix (Troll)"); !ok || lvl != 60 || cls != eqclass.ClassWarrior {
		t.Errorf("/who title = %d,%v,%v, want 60,Warrior,true", lvl, cls, ok)
	}
	// multi-word title
	if lvl, cls, ok := p.parseLevel("[51 Grave Lord] Kelkix (Troll)"); !ok || lvl != 51 || cls != eqclass.ClassShadowKnight {
		t.Errorf("/who multi-word title = %d,%v,%v, want 51,Shadow Knight,true", lvl, cls, ok)
	}
	// level-up names no class
	if lvl, cls, ok := p.parseLevel("You have gained a level! Welcome to level 43!"); !ok || lvl != 43 || cls != eqclass.ClassUnknown {
		t.Errorf("level-up = %d,%v,%v, want 43,Unknown,true", lvl, cls, ok)
	}
	// another player's /who line must not set our level
	if _, _, ok := p.parseLevel("[50 Cleric] Someoneelse (Human)"); ok {
		t.Errorf("another player's /who line should be ignored")
	}
	// anonymous (no level) is ignored
	if _, _, ok := p.parseLevel("[ANONYMOUS] Kelkix"); ok {
		t.Errorf("anonymous /who line should be ignored")
	}
}

// Malformed lines for each category must return an error (and never panic) so
// dispatch silently drops them instead of forwarding bogus events. This covers
// the no-regex-match and too-short branches of every parse* function.
func TestParse_MalformedLinesError(t *testing.T) {
	p := &DmgParser{}

	// no combat verb → not a damage line
	if _, err := p.parseDamage(fakeTS + "a rat sniffs YOU for 5 points of damage."); err == nil {
		t.Error("parseDamage accepted a verbless line")
	}
	// "for X points" non-numeric can't reach parseDamage (regex requires \d+), so
	// the no-match path is the malformed case; a too-short line is the other.
	if _, err := p.parseDamage("[short]"); err == nil {
		t.Error("parseDamage accepted a too-short line")
	}
	if _, err := p.parseCrit(fakeTS + "Naku did something unrelated"); err == nil {
		t.Error("parseCrit accepted a non-crit line")
	}
	if _, err := p.parseCrit("[short]"); err == nil {
		t.Error("parseCrit accepted a too-short line")
	}
	if _, err := p.parseMagic(fakeTS + "a rat sneezes for 5 points of damage."); err == nil {
		t.Error("parseMagic accepted a non-magic line")
	}
	if _, err := p.parseMagic("[short]"); err == nil {
		t.Error("parseMagic accepted a too-short line")
	}
	if _, err := p.parseEvent(fakeTS + "You say, 'hello'"); err == nil {
		t.Error("parseEvent accepted a non-event line")
	}
	if _, err := p.parseEvent("[short]"); err == nil {
		t.Error("parseEvent accepted a too-short line")
	}
	if _, err := p.parseSwing("[short]"); err == nil {
		t.Error("parseSwing accepted a too-short line")
	}
}

// A line whose bracketed header isn't a valid time.ANSIC stamp must surface as a
// parse error from every parser that reads the timestamp first.
func TestParse_BadTimestampError(t *testing.T) {
	p := &DmgParser{}
	bad := "[not a real timestamp..] You have slain a rat!"
	if _, err := parseTimestamp(bad); err == nil {
		t.Error("parseTimestamp accepted a malformed stamp")
	}
	if _, err := p.parseEvent(bad); err == nil {
		t.Error("parseEvent accepted a malformed stamp")
	}
	if _, err := p.parseDamage("[not a real timestamp..] a rat hits YOU for 5 points of damage."); err == nil {
		t.Error("parseDamage accepted a malformed stamp")
	}
}

// The log owner's own crits log under their character name; parseCrit remaps it
// to "You" while leaving other attackers untouched.
func TestParseCrit_OwnerRemapAndOthers(t *testing.T) {
	p := &DmgParser{character: "Stelzar"}
	own, err := p.parseCrit(fakeTS + "Stelzar scores a critical hit!(212)")
	if err != nil {
		t.Fatal(err)
	}
	if own.Attacker != "You" || own.Damage != 212 {
		t.Errorf("owner crit = %+v, want You/212", own)
	}
	other, err := p.parseCrit(fakeTS + "Grobnik Scores a critical hit!(50)")
	if err != nil {
		t.Fatal(err)
	}
	if other.Attacker != "Grobnik" {
		t.Errorf("other crit attacker = %q, want Grobnik", other.Attacker)
	}
}

// parseEvent distinguishes party (grouped) xp from solo xp — the substrings
// overlap ("You gain experience" is a prefix of "...party..."), so the party
// case must be checked first.
func TestParseEvent_PartyVsSoloXP(t *testing.T) {
	p := &DmgParser{}
	party, err := p.parseEvent(fakeTS + "You gain party experience!!")
	if err != nil || party.Kind != combat.EventPartyXP {
		t.Errorf("party xp = %+v err=%v, want EventPartyXP", party, err)
	}
	solo, err := p.parseEvent(fakeTS + "You gain experience!!")
	if err != nil || solo.Kind != combat.EventXP {
		t.Errorf("solo xp = %+v err=%v, want EventXP", solo, err)
	}
}

// parseEvent extracts the slain mob's name and routes a player-death line to
// EventDeath (not a kill).
func TestParseEvent_KillNameAndDeath(t *testing.T) {
	p := &DmgParser{}
	kill, err := p.parseEvent(fakeTS + "You have slain a sand giant!")
	if err != nil || kill.Kind != combat.EventKill || kill.Name != "a sand giant" {
		t.Errorf("kill = %+v err=%v, want EventKill 'a sand giant'", kill, err)
	}
	death, err := p.parseEvent(fakeTS + "You have been slain by a sand giant!")
	if err != nil || death.Kind != combat.EventDeath {
		t.Errorf("death = %+v err=%v, want EventDeath", death, err)
	}
}

// parseMagic captures the non-melee target and amount, normalizing a player
// target to the "YOU" key.
func TestParseMagic_TargetNormalization(t *testing.T) {
	p := &DmgParser{}
	onMob, err := p.parseMagic(fakeTS + "a sand giant was hit by non-melee for 412 points of damage.")
	if err != nil {
		t.Fatal(err)
	}
	if onMob.Target != "a sand giant" || onMob.Dmg != 412 {
		t.Errorf("magic on mob = %+v, want 'a sand giant'/412", onMob)
	}
	onYou, err := p.parseMagic(fakeTS + "YOU was hit by non-melee for 90 points of damage.")
	if err != nil {
		t.Fatal(err)
	}
	if onYou.Target != "YOU" {
		t.Errorf("magic on you target = %q, want YOU", onYou.Target)
	}
}

// observeSpells ignores lines too short to carry a timestamp and ones with a
// malformed stamp, rather than panicking on the slice access. A nil tracker call
// is exercised via the cast-start path with a real tracker for the happy case.
func TestObserveSpells_GuardsBadInput(t *testing.T) {
	p := &DmgParser{character: "Kelkix", tracker: recordingObserver{}}
	// must not panic / must early-return: too short, and bad timestamp
	p.observeSpells("[short]")
	p.observeSpells("[not a real timestamp..] You begin casting Snare.")
}

// recordingObserver is a no-op SpellObserver used to drive observeSpells'
// guard paths without standing up a full gamestate.Tracker.
type recordingObserver struct{}

func (recordingObserver) Observe(string, int64)   {}
func (recordingObserver) BeginCast(string, int64) {}
func (recordingObserver) BreakCCOnTarget(string)  {}
func (recordingObserver) SetLevel(int)            {}
func (recordingObserver) SetClass(eqclass.Class)  {}
func (recordingObserver) FeignAttempt(int64)      {}
func (recordingObserver) FeignFailed(int64)       {}

// RebuildTrackerFromFile must no-op (not panic) on a nil tracker and on a
// missing file path.
func TestRebuildTrackerFromFile_NilAndMissing(t *testing.T) {
	RebuildTrackerFromFile("/no/such/file.txt", "Kelkix", nil) // nil tracker
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	RebuildTrackerFromFile(filepath.Join(t.TempDir(), "does_not_exist.txt"), "Kelkix", tr) // missing file
	// no assertion beyond "didn't panic and left the tracker untouched"
	if tr.Level() != 0 {
		t.Errorf("missing-file rebuild mutated the tracker: L%d", tr.Level())
	}
}

// A "tries to" line that isn't a combat avoidance must be rejected, not
// mis-tallied as a swing.
func TestParseSwing_RejectsNonCombat(t *testing.T) {
	p := &DmgParser{}
	full := fakeTS + "A gnome tries to sell you wares, but you decline!"
	if _, err := p.parseSwing(full); err == nil {
		t.Errorf("parseSwing accepted a non-combat line; want error")
	}
}

// EQ-style log lines look like:
//
//	[Mon Jan  2 15:04:05 2006] <subject>
//
// The timestamp is exactly 24 chars (time.ANSIC), bracketed, then a space — so
// the subject starts at index 27 (LOG_SUBJECT_INDEX_START).
const fakeTS = "[Mon Jan  2 15:04:05 2006] "

func parseLine(t *testing.T, subject string) (dealer, target string, dmg int) {
	t.Helper()
	p := &DmgParser{}
	set, err := p.parseDamage(fakeTS + subject)
	if err != nil {
		t.Fatalf("parseDamage(%q) failed: %v", subject, err)
	}
	return set.Dealer, set.Target, set.Dmg
}

// Sanity: a plain "you" line parses cleanly. This passes today.
func TestParseDamage_BasicYou(t *testing.T) {
	dealer, target, dmg := parseLine(t, "a giant rat slashes YOU for 5 points of damage.")

	if dealer != "a giant rat" {
		t.Errorf("dealer = %q, want %q", dealer, "a giant rat")
	}
	if target != "YOU" {
		t.Errorf("target = %q, want %q", target, "YOU")
	}
	if dmg != 5 {
		t.Errorf("dmg = %d, want 5", dmg)
	}
}

// A damage target containing the "YOU" token normalizes to the player ("YOU")
// so the player's own incoming-hit lines attribute correctly. (The check is a
// substring match, so a mob name literally containing "YOU" would also collapse
// — harmless in practice; no P99 mob name does.)
func TestParseDamage_TargetNormalizationToYou(t *testing.T) {
	_, target, _ := parseLine(t, "Foo slashes the GREAT YOU MONSTER for 99 points of damage.")
	if target != "YOU" {
		t.Errorf("target = %q, want %q (a target containing YOU normalizes to the player)", target, "YOU")
	}
}

// Likewise, a target containing "non-melee" normalizes to the "non-melee"
// sentinel — a guard for a non-melee line that slips into the melee parser.
func TestParseDamage_NonMeleeNormalization(t *testing.T) {
	_, target, _ := parseLine(t, "Foo crushes a non-melee aura for 12 points of damage.")
	if target != "non-melee" {
		t.Errorf("target = %q, want %q", target, "non-melee")
	}
}
