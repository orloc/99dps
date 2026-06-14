package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// mezRow builds a 217-field mez spell line: a detrimental spell whose
// cast-on-other emote carries "mesmerized" (so the tracker flags it as Mez/CC).
func mezRow(name, emote string) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name
	f[7] = emote // cast_on_other → "<target> has been mesmerized."
	f[13] = "3000"
	f[16] = "1"
	f[17] = "30"
	f[83] = "0" // detrimental
	return strings.Join(f, "^")
}

// ccTracker is an enchanter holding a live mez on a mob (a CROWD CONTROL entry).
func ccTracker(t *testing.T) *gamestate.Tracker {
	t.Helper()
	book, _ := gamestate.LoadReader(strings.NewReader(mezRow("Mesmerize", " has been mesmerized.")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassEnchanter)
	now := time.Now().Unix()
	tr.BeginCast("Mesmerize", now-5)
	tr.Observe("a sand giant has been mesmerized.", now)
	return tr
}

// onYouTracker holds a live incoming "ON YOU" debuff (a slow landing on the
// player) — the registry-driven incoming path, not a self-cast.
func onYouTracker(t *testing.T) *gamestate.Tracker {
	t.Helper()
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.Observe("You feel drowsy.", time.Now().Unix()) // slow landed on you
	return tr
}

// TestCCLineRenders: the CROWD CONTROL line marks mez with M and shows the target
// and time; hovering adds the ✕ dismiss affordance.
func TestCCLineRenders(t *testing.T) {
	now := int64(1000)
	mez := gamestate.Timer{Spell: "Mesmerize", Target: "a sand giant", Start: now, Expiry: now + 30, Mez: true}
	plain := ccLine(themes[0], mez, now, 40, false, 5)
	if !strings.Contains(plain, "M") || !strings.Contains(plain, "sand giant") {
		t.Errorf("mez ccLine should mark M and name the target, got %q", plain)
	}
	if strings.Contains(plain, "✕") {
		t.Error("an unhovered ccLine should not show the ✕")
	}
	if hov := ccLine(themes[0], mez, now, 40, true, 5); !strings.Contains(hov, "✕") {
		t.Errorf("a hovered ccLine should show the ✕, got %q", hov)
	}
	// charm and pacify use their own markers.
	charm := gamestate.Timer{Spell: "Charm", Target: "a pet", Start: now, Expiry: now + 30, Charm: true}
	if !strings.Contains(ccLine(themes[0], charm, now, 40, false, 5), "⊗") {
		t.Error("charm ccLine should use the ⊗ marker")
	}
	pac := gamestate.Timer{Spell: "Lull", Target: "a guard", Start: now, Expiry: now + 30, Pacify: true}
	if !strings.Contains(ccLine(themes[0], pac, now, 40, false, 5), "z") {
		t.Error("pacify ccLine should use the z marker")
	}
}

// TestCCLineNarrow: at a width too small for a name, ccLine still renders the time.
func TestCCLineNarrow(t *testing.T) {
	now := int64(1000)
	mez := gamestate.Timer{Spell: "Mez", Target: "a very long mob name here", Start: now, Expiry: now + 65, Mez: true}
	out := ccLine(themes[0], mez, now, 6, false, 4)
	if !strings.Contains(out, "1:05") {
		t.Errorf("narrow ccLine should still show the time, got %q", out)
	}
}

// TestOnYouLine: an incoming debuff renders its category + time; an estimated
// duration gets a leading "~".
func TestOnYouLine(t *testing.T) {
	now := int64(1000)
	real := gamestate.Timer{Spell: "Rooted", Target: "You", Start: now, Expiry: now + 40}
	if out := onYouLine(themes[0], real, now, 40, 5); !strings.Contains(out, "Rooted") || strings.Contains(out, "~") {
		t.Errorf("a real onYouLine should show the category without ~, got %q", out)
	}
	est := gamestate.Timer{Spell: "Slowed", Target: "You", Start: now, Expiry: now + 40, Estimated: true}
	if out := onYouLine(themes[0], est, now, 40, 5); !strings.Contains(out, "~Slowed") {
		t.Errorf("an estimated onYouLine should lead with ~, got %q", out)
	}
	// narrow: still shows the time.
	if out := onYouLine(themes[0], est, now, 5, 4); !strings.Contains(out, "0:40") {
		t.Errorf("narrow onYouLine should still show the time, got %q", out)
	}
}

// TestCCRenderedInEnemyColumn: a live mez shows in the enemy/CC column with its
// CROWD CONTROL header and M marker.
func TestCCRenderedInEnemyColumn(t *testing.T) {
	body, _ := timerColumn(themes[0], ccTracker(t), 40, "", true, true, false)
	if !strings.Contains(body, "CROWD CONTROL") {
		t.Fatalf("a mez should render under a CROWD CONTROL header, got:\n%s", body)
	}
	if !strings.Contains(body, "a sand giant") {
		t.Errorf("the mezzed mob should be named, got:\n%s", body)
	}
}

// TestOnYouRenderedInClassPanel: an incoming debuff on the player renders in an
// "ON YOU" section in the player-self (class/buffs) column.
func TestOnYouRenderedInClassPanel(t *testing.T) {
	// the buffs column (wantBuffs only) carries the player-self ON YOU section.
	body, _ := timerColumn(themes[0], onYouTracker(t), 40, "", false, false, true)
	if !strings.Contains(body, "ON YOU") {
		t.Fatalf("an incoming debuff should render under an ON YOU header, got:\n%s", body)
	}
	if !strings.Contains(body, "Slowed") {
		t.Errorf("the incoming category should show, got:\n%s", body)
	}
}

// TestUrgencyColor maps the three thresholds (green healthy → gold low → red
// expiring) off the shared classifier.
func TestUrgencyColor(t *testing.T) {
	th := themes[0]
	total := int64(100)
	if got := urgencyColor(th, 80, total); got != "#5fd37a" {
		t.Errorf("plenty of time should be green, got %q", got)
	}
	if got := urgencyColor(th, 40, total); got != th.accent {
		t.Errorf("low (≤50%%) should be gold, got %q", got)
	}
	if got := urgencyColor(th, 5, total); got != "#e0564e" {
		t.Errorf("near expiry (≤20%%) should be red, got %q", got)
	}
}

// TestMobUrgencyColor maps the repop thresholds (up/imminent/soon/far).
func TestMobUrgencyColor(t *testing.T) {
	th := themes[0]
	cases := []struct {
		rem  int64
		want string
	}{
		{0, "#5fd37a"}, {-5, "#5fd37a"}, // up now
		{20, "#e0564e"}, // imminent
		{60, th.accent}, // soon
		{300, th.dim},   // far off
	}
	for _, c := range cases {
		if got := mobUrgencyColor(th, c.rem); got != c.want {
			t.Errorf("mobUrgencyColor(rem=%d) = %q, want %q", c.rem, got, c.want)
		}
	}
}

// TestSplitCCtimers partitions mez/charm/pacify into CC and the rest into rest.
func TestSplitCCtimers(t *testing.T) {
	ts := []gamestate.Timer{
		{Spell: "Mez", Mez: true},
		{Spell: "Charm", Charm: true},
		{Spell: "Lull", Pacify: true},
		{Spell: "Slow"},  // a plain debuff
		{Spell: "Haste"}, // a plain buff
	}
	cc, rest := splitCCtimers(ts)
	if len(cc) != 3 {
		t.Errorf("CC should hold mez+charm+pacify, got %d", len(cc))
	}
	if len(rest) != 2 {
		t.Errorf("rest should hold the non-CC timers, got %d", len(rest))
	}
}

// TestPct: zero/negative totals are safe (0%); otherwise it's the floor percent.
func TestPct(t *testing.T) {
	if got := pct(0, 0); got != 0 {
		t.Errorf("pct with zero total should be 0, got %d", got)
	}
	if got := pct(5, -1); got != 0 {
		t.Errorf("pct with negative total should be 0, got %d", got)
	}
	if got := pct(1, 3); got != 33 {
		t.Errorf("pct(1,3) = %d, want 33", got)
	}
	if got := pct(50, 50); got != 100 {
		t.Errorf("pct(50,50) = %d, want 100", got)
	}
}

// TestSpecialKindsByDamage orders a dealer's special kinds by damage descending,
// name as the tiebreak.
func TestSpecialKindsByDamage(t *testing.T) {
	sp := map[string]combat.SpecialStat{
		"kick":     {Total: 100},
		"backstab": {Total: 500},
		"bash":     {Total: 100}, // ties kick → name order (bash < kick)
	}
	got := specialKindsByDamage(sp)
	want := []string{"backstab", "bash", "kick"}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("order[%d] = %q, want %q (full %v)", i, got[i], k, got)
		}
	}
}

// TestCardEmptyTitle: an empty title omits the title line, so the body gets the
// extra row; a titled card includes the title text.
func TestCardEmptyTitle(t *testing.T) {
	th := themes[0]
	titled := card(th, 30, 6, "My Title", "body")
	if !strings.Contains(titled, "My Title") {
		t.Error("a titled card should render the title")
	}
	untitled := card(th, 30, 6, "", "body line")
	if strings.Contains(untitled, "My Title") {
		t.Error("an empty-title card should not invent a title")
	}
	// a degenerate tiny card clamps rather than panicking.
	tiny := card(th, 1, 1, "t", "b")
	for _, ln := range strings.Split(tiny, "\n") {
		if lipgloss.Width(ln) > 6+2 { // clamped to the cw floor (6) + border
			t.Errorf("tiny card line exceeds the clamped width: %q", ln)
		}
	}
}

// TestPlayerStatAndAvoidance pull the player's aggregate + defense out of a
// session (case-insensitive "you" match).
func TestPlayerStatAndAvoidance(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 500, Target: "a rat"})
	sm.ApplySwing(&combat.Swing{ActionTime: 1001, Attacker: "a rat", Defender: "You", Outcome: combat.OutcomeDodge})
	cur := sm.Current()
	if st := playerStat(cur); st.Total != 500 {
		t.Errorf("playerStat total = %d, want 500", st.Total)
	}
	av, faced := playerAvoidance(cur)
	if faced == 0 || av == 0 {
		t.Errorf("player should have an avoidance record (avoided=%d faced=%d)", av, faced)
	}
	// a session with no You data returns zero values, not a panic.
	other := &session.SessionManager{}
	other.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "Bob", Dmg: 1, Target: "a rat"})
	if st := playerStat(other.Current()); st.Total != 0 {
		t.Errorf("playerStat with no You row should be zero, got %d", st.Total)
	}
	if a, f := playerAvoidance(other.Current()); a != 0 || f != 0 {
		t.Errorf("playerAvoidance with no You row should be zero, got (%d,%d)", a, f)
	}
}
