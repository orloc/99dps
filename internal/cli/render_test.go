package cli

import (
	"99dps/internal/common"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
	"strings"
	"testing"
	"time"
)

func TestHumanizeInt(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1500: "1.5k", 1_500_000: "1.5m"}
	for in, want := range cases {
		if got := humanizeInt(in); got != want {
			t.Errorf("humanizeInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	cases := map[int]string{0: "0:00", 65: "1:05", 600: "10:00"}
	for secs, want := range cases {
		// fmtDuration takes a time.Duration via seconds
		if got := fmtDuration(time.Duration(secs) * time.Second); got != want {
			t.Errorf("fmtDuration(%ds) = %q, want %q", secs, got, want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	cases := map[int]string{100: "100", 12345: "12,345", 1000000: "1,000,000", -5: "-5"}
	for in, want := range cases {
		if got := formatInt(in); got != want {
			t.Errorf("formatInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateAndPad_Multibyte(t *testing.T) {
	if got := truncate("ünïcode", 3); got != "ünï" {
		t.Errorf("truncate multibyte = %q, want %q", got, "ünï")
	}
	if got := padTo("ab", 5); got != "ab   " {
		t.Errorf("padTo widen = %q", got)
	}
	if got := padTo("abcdef", 3); got != "abc" {
		t.Errorf("padTo clip = %q", got)
	}
	// the multibyte selection marker should count as one cell, not three
	if got := padTo("▸ x", 5); len([]rune(got)) != 5 {
		t.Errorf("padTo rune width wrong: %q has %d runes", got, len([]rune(got)))
	}
}

func TestEnsureVisible(t *testing.T) {
	// linesPerCard == 4. Viewport of 8 lines shows 2 cards.
	const h = 8
	// card 0 already at top: no scroll
	if got := ensureVisible(0, 0, h); got != 0 {
		t.Errorf("card0 from top = %d, want 0", got)
	}
	// card 3 (lines 12-15) below an 8-line viewport at top: scroll so its
	// bottom (15) is the last visible line -> origin 8
	if got := ensureVisible(0, 3, h); got != 8 {
		t.Errorf("card3 scroll-down = %d, want 8", got)
	}
	// selecting an earlier card while scrolled down jumps the origin up to it
	if got := ensureVisible(8, 1, h); got != 4 {
		t.Errorf("card1 scroll-up = %d, want 4", got)
	}
	// a card already inside the viewport doesn't move it
	if got := ensureVisible(8, 2, h); got != 8 {
		t.Errorf("card2 in view = %d, want 8 (unchanged)", got)
	}
}

func TestClampScroll(t *testing.T) {
	// total 40 lines, height 16 -> max origin 24
	if got := clampScroll(100, 40, 16); got != 24 {
		t.Errorf("over-max = %d, want 24", got)
	}
	if got := clampScroll(-5, 40, 16); got != 0 {
		t.Errorf("under-zero = %d, want 0", got)
	}
	// content shorter than the viewport pins to top
	if got := clampScroll(5, 8, 16); got != 0 {
		t.Errorf("short content = %d, want 0", got)
	}
}

func TestTimerStyle(t *testing.T) {
	// background tint escalates with urgency; final seconds flash red/white
	cases := []struct {
		detrimental bool
		rem, now    int64
		want        string
	}{
		{false, 120, 101, "42;30"}, // buff, lots of time → green bg
		{true, 120, 101, "44;37"},  // debuff → blue bg
		{false, 25, 101, "43;30"},  // ≤30s → yellow bg
		{true, 25, 101, "43;30"},   // urgency overrides type
		{false, 8, 101, "41;37"},   // ≤10s → red bg
		{false, 4, 100, "41;37"},   // ≤5s, even second → red fill
		{false, 4, 101, "47;31"},   // ≤5s, odd second → inverted (flash)
		{true, 3, 101, "47;31"},    // debuff flashing
	}
	for _, c := range cases {
		if got := timerStyle(c.detrimental, c.rem, c.now); got != c.want {
			t.Errorf("timerStyle(detr=%v, rem=%d, now=%d) = %q, want %q",
				c.detrimental, c.rem, c.now, got, c.want)
		}
	}
}

func TestGroupByTarget(t *testing.T) {
	timers := []gamestate.Timer{
		{Spell: "Spirit of Wolf", Target: "You", Expiry: 200},
		{Spell: "Snare", Target: "a rat", Expiry: 50},
		{Spell: "Bedlam", Target: "You", Expiry: 100},
		{Spell: "Tashani", Target: "a giant", Expiry: 40},
	}
	groups, order := groupByTarget(timers)

	// ordered by soonest-expiring timer: a giant(40) < a rat(50) < You(100)
	want := []string{"a giant", "a rat", "You"}
	for i, w := range want {
		if i >= len(order) || order[i] != w {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if len(groups["You"]) != 2 {
		t.Errorf("You group has %d timers, want 2", len(groups["You"]))
	}
}

func TestPctOfAndDisplayName(t *testing.T) {
	if pctOf(1, 4) != 25 {
		t.Errorf("pctOf(1,4) = %d, want 25", pctOf(1, 4))
	}
	if displayName("YOU") != "You" || displayName("Naku") != "Naku" {
		t.Errorf("displayName mapping wrong")
	}
}

// renderDamage smoke test: build a real session via the manager, render it, and
// assert the key facts surface. Exercises the whole pure render path.
func TestRenderDamage_Smoke(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&common.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 50, Target: "a rat", Verb: "slash"})
	sm.Apply(&common.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 70, Target: "a rat", Verb: "slash"})

	out := renderDamage(sm.Current(), true, 60)

	for _, want := range []string{"You", "HIT%", "CRIT%", "live"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDamage output missing %q:\n%s", want, out)
		}
	}

	// at a narrow width the optional columns must drop, not clip
	narrow := renderDamage(sm.Current(), true, 40)
	if strings.Contains(narrow, "HIT%") {
		t.Errorf("narrow render should omit Hit%% column:\n%s", narrow)
	}
}

func TestRenderBars(t *testing.T) {
	agg := []common.DamageStat{
		{Dealer: "You", Total: 1000, Hits: 10, FirstTime: 100, LastTime: 110},
		{Dealer: "Bob", Total: 500, Hits: 5, FirstTime: 100, LastTime: 110},
	}

	out := renderBars(agg, 60, 5)
	for _, want := range []string{"You", "Bob", "█"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderBars missing %q:\n%s", want, out)
		}
	}

	// height caps the number of bars (no overflow past the view)
	if capped := renderBars(agg, 60, 1); strings.Contains(capped, "Bob") {
		t.Errorf("height=1 should drop the 2nd bar:\n%s", capped)
	}

	// degenerate inputs all yield the placeholder rather than panicking
	for _, c := range []struct {
		name          string
		agg           []common.DamageStat
		width, height int
	}{
		{"empty", nil, 60, 5},
		{"too narrow", agg, 5, 5},
		{"zero height", agg, 60, 0},
		{"zero total", []common.DamageStat{{Dealer: "x", Total: 0}}, 60, 5},
	} {
		if got := renderBars(c.agg, c.width, c.height); got != "Fight something!" {
			t.Errorf("renderBars(%s) = %q, want placeholder", c.name, got)
		}
	}
}

func TestRenderSkillsAndSummary(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&common.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 200, Target: "a rat", Verb: "backstabs"})
	sm.Apply(&common.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 50, Target: "a rat", Verb: "slash"})
	cur := sm.Current()

	out := renderSkills(cur, eqclass.ClassRogue, 50, 40)
	for _, want := range []string{"SKILLS", "Backstab", "Hit rate"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderSkills missing %q:\n%s", want, out)
		}
	}
	if renderSkills(nil, eqclass.ClassRogue, 50, 40) == "" {
		t.Error("renderSkills(nil) should return placeholder text, not empty")
	}

	// the hybrid one-liner leads with the top skill and includes accuracy
	if sum := skillsSummary(cur, eqclass.ClassRogue, 50); !strings.Contains(sum, "Backstab") || !strings.Contains(sum, "Hit") {
		t.Errorf("skillsSummary = %q, want Backstab + Hit", sum)
	}
	if skillsSummary(nil, eqclass.ClassRogue, 50) != "" {
		t.Error("skillsSummary(nil) should be empty")
	}
}

// A monk's kick is labelled Flying Kick at 30+, and the monk-only "strike"
// bucket surfaces for monks but is hidden for other classes.
func TestSkillLabellingByClass(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&common.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 80, Target: "a rat", Verb: "kick"})
	sm.Apply(&common.DamageSet{ActionTime: 101, Dealer: "You", Dmg: 60, Target: "a rat", Verb: "strike"})
	cur := sm.Current()

	monk := renderSkills(cur, eqclass.ClassMonk, 35, 40)
	if !strings.Contains(monk, "Flying Kick") {
		t.Errorf("level-35 monk kick should label Flying Kick:\n%s", monk)
	}
	if !strings.Contains(monk, "Strike") {
		t.Errorf("monk strike bucket should show:\n%s", monk)
	}

	// a low-level monk's kick stays generic
	low := renderSkills(cur, eqclass.ClassMonk, 20, 40)
	if strings.Contains(low, "Flying Kick") {
		t.Errorf("level-20 monk should not show Flying Kick:\n%s", low)
	}

	// for a non-monk, the "strike" bucket is not a skill and must be hidden
	war := renderSkills(cur, eqclass.ClassWarrior, 35, 40)
	if strings.Contains(war, "Strike") {
		t.Errorf("non-monk should not surface a Strike skill:\n%s", war)
	}
}

func TestRenderStatus(t *testing.T) {
	out := renderStatus("Kelkix", eqclass.ClassMonk, 60, "Greater Faydark", 42, 38, 2, 24)
	for _, want := range []string{"Kelkix", "Monk", "Greater Faydark", "42 kills", "38/hr", "2 deaths"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderStatus missing %q:\n%s", want, out)
		}
	}
	// pre-/who, pre-kill: still shows the character (no kills/zone line)
	if out := renderStatus("Kelkix", eqclass.ClassUnknown, 0, "", 0, 0, 0, 24); !strings.Contains(out, "Kelkix") {
		t.Errorf("renderStatus should always show the character:\n%s", out)
	}
}

func TestParseTimer(t *testing.T) {
	ok := map[string]int{"28:00": 1680, "6:40": 400, "1:10:10": 4210, "400": 400}
	for in, want := range ok {
		if got, valid := parseTimer(in); !valid || got != want {
			t.Errorf("parseTimer(%q) = (%d,%v), want (%d,true)", in, got, valid, want)
		}
	}
	for _, bad := range []string{"", "abc", "5:xx", "0", "-3"} {
		if _, valid := parseTimer(bad); valid {
			t.Errorf("parseTimer(%q) should be invalid", bad)
		}
	}
}

func TestGetScreenDims(t *testing.T) {
	// full-screen view on an 80x24 terminal
	x1, y1, x2, y2 := GetScreenDims(ViewProperties{X1: 0, X2: 1, Y1: 0, Y2: 1}, 80, 24)
	if x1 != 0 || y1 != 0 || x2 != 79 || y2 != 23 {
		t.Errorf("got (%d,%d,%d,%d), want (0,0,79,23)", x1, y1, x2, y2)
	}

	// right 80% panel
	x1, _, x2, _ = GetScreenDims(ViewProperties{X1: 0.2, X2: 1}, 100, 24)
	if x1 != 20 || x2 != 99 {
		t.Errorf("got x1=%d x2=%d, want 20/99", x1, x2)
	}
}

func TestDealerDPS(t *testing.T) {
	// 1000 damage over a 10s span = 100/s
	if got := dealerDPS(common.DamageStat{Total: 1000, Hits: 5, FirstTime: 100, LastTime: 110}); got != 100 {
		t.Errorf("dealerDPS = %d, want 100", got)
	}
	// a zero span (single hit) falls back to the raw total
	if got := dealerDPS(common.DamageStat{Total: 250, Hits: 1, FirstTime: 100, LastTime: 100}); got != 250 {
		t.Errorf("zero-span dealerDPS = %d, want 250", got)
	}
	// no hits → 0, no division
	if got := dealerDPS(common.DamageStat{}); got != 0 {
		t.Errorf("empty dealerDPS = %d, want 0", got)
	}
}

// Mez/charm are pinned in a CROWD CONTROL section above buffs/debuffs; the
// line→target map (for click-to-dismiss) accounts for the header + separator.
func TestRenderTimersCrowdControl(t *testing.T) {
	timers := []gamestate.Timer{
		{Spell: "Mesmerize", Target: "a kobold", Expiry: 30, Mez: true},
		{Spell: "Clarity II", Target: "You", Expiry: 600},
	}
	out, lm := renderTimers(timers, 0, 40, true) // ccInline: CC pinned at top

	if !strings.Contains(out, "CROWD CONTROL") {
		t.Errorf("missing CROWD CONTROL header:\n%s", out)
	}
	// header is line 0; the mez row is line 1 → its target is "a kobold"
	if lm[1] != "a kobold" {
		t.Errorf("line 1 target = %q, want a kobold", lm[1])
	}
	// the buff still shows below, under its own target group
	for _, want := range []string{"Clarity II", "You"} {
		if !strings.Contains(out, want) {
			t.Errorf("buff section missing %q:\n%s", want, out)
		}
	}
}

// renderCC is the enchanter Crowd Control column: mez+charm, soonest-first, no
// header (the panel title supplies it).
func TestRenderCC(t *testing.T) {
	cc := []gamestate.Timer{
		{Spell: "Charm", Target: "Charm", Expiry: 300, Charm: true},
		{Spell: "Mesmerize", Target: "a kobold", Expiry: 30, Mez: true},
	}
	out, lm := renderCC(cc, 0, 30)
	for _, want := range []string{"a kobold", "Charm"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCC missing %q:\n%s", want, out)
		}
	}
	if lm[0] != "a kobold" { // soonest-first: mez(30) before charm(300)
		t.Errorf("line 0 target = %q, want a kobold", lm[0])
	}
	if s, _ := renderCC(nil, 0, 30); s != "" {
		t.Error("empty CC should render empty")
	}
}

func TestRenderAvoidance(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&common.DamageSet{ActionTime: 100, Dealer: "You", Dmg: 10, Target: "a rat"})
	for _, o := range []common.SwingOutcome{common.OutcomeMiss, common.OutcomeDodge, common.OutcomeRiposte} {
		sm.ApplySwing(&common.Swing{ActionTime: 100, Attacker: "You", Defender: "a rat", Outcome: o})
	}
	cur := sm.Current()

	full := renderAvoidance(cur, 80) // wide → labelled table
	for _, want := range []string{"Defender", "Avoid", "Dodge", "Ripo", "a rat"} {
		if !strings.Contains(full, want) {
			t.Errorf("full avoidance missing %q:\n%s", want, full)
		}
	}
	if narrow := renderAvoidance(cur, 30); strings.Contains(narrow, "Defender") {
		t.Errorf("narrow avoidance should drop the labelled header:\n%s", narrow)
	}
}

func TestRenderRespawnsGroupsAndKiller(t *testing.T) {
	rs := []gamestate.Respawn{
		{Mob: "a fippy darkpaw", Remaining: 0, Mine: true}, // up
		{Mob: "a noble", Remaining: 300, Mine: true},       // mine, counting
		{Mob: "a guard", Remaining: 120, Killer: "Gnadad"}, // other's kill
	}
	out := renderRespawns(rs, "", 40)
	for _, want := range []string{"killed by others", "Gnadad", "UP", "a noble"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderRespawns missing %q:\n%s", want, out)
		}
	}
}

func TestCanniGrade(t *testing.T) {
	cases := map[int]string{100: "S", 95: "S", 94: "A", 85: "A", 84: "B", 70: "B", 69: "C", 50: "C", 49: "D", 0: "D"}
	for pct, want := range cases {
		if g, _ := canniGrade(pct); g != want {
			t.Errorf("canniGrade(%d) = %q, want %q", pct, g, want)
		}
	}
}

func TestStackPanel(t *testing.T) {
	// no sections: body and its click map pass through untouched
	str, m := stackPanel(nil, "timers\nrow", map[int]string{1: "Tank"})
	if str != "timers\nrow" || m[1] != "Tank" {
		t.Fatalf("no-section passthrough: str=%q map=%v", str, m)
	}

	// empties are skipped; a 1-line banner (trailing newline normalized) shifts
	// the body map down by exactly one line
	str, m = stackPanel([]string{"", "BANNER\n", ""}, "row0\nrow1", map[int]string{0: "A", 1: "B"})
	if str != "BANNER\nrow0\nrow1" {
		t.Errorf("stack str = %q", str)
	}
	if m[1] != "A" || m[2] != "B" || m[0] != "" {
		t.Errorf("map should shift by 1: %v", m)
	}

	// a multi-line section shifts by its full line count
	_, m = stackPanel([]string{"a\nb\nc"}, "body", map[int]string{0: "X"})
	if m[3] != "X" {
		t.Errorf("3-line section should shift map by 3: %v", m)
	}

	// sections but an empty body: sections still render, map drops to nil
	str, m = stackPanel([]string{"only"}, "", map[int]string{0: "Z"})
	if str != "only" || m != nil {
		t.Errorf("empty body: str=%q map=%v (want \"only\", nil)", str, m)
	}
}
