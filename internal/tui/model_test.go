package tui

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// buffRow builds a minimal 217-field spells_us.txt line for a beneficial buff
// with the given cast-on-other landing emote (the prefix of which is the
// target). Field indices mirror internal/gamestate/book.go.
func buffRow(name, emote string) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name    // fName
	f[7] = emote   // fCastOnOther
	f[13] = "3000" // fCastTime (ms)
	f[16] = "1"    // fDurFormula
	f[17] = "30"   // fDurCap (ticks) → ~3 min at L60, outlasts the test
	f[83] = "1"    // fGoodEffect (beneficial)
	return strings.Join(f, "^")
}

// debuffRow is buffRow's detrimental sibling (fGoodEffect 0).
func debuffRow(name, emote string) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name
	f[7] = emote
	f[13] = "3000"
	f[16] = "1"
	f[17] = "30"
	f[83] = "0" // detrimental
	return strings.Join(f, "^")
}

// casterScene buffs two group members so the spell-timer panel groups by target
// (Aragorn carries two buffs, Legolas one). Timers are anchored at wall-clock so
// they're still active when the renderer reads time.Now().
func casterScene() (*session.SessionManager, *gamestate.Tracker) {
	sm := sampleManager()
	book, _ := gamestate.LoadReader(strings.NewReader(strings.Join([]string{
		buffRow("Aegolism", "'s skin turns to stone."),
		buffRow("Haste", "'s feet move faster."),
	}, "\n")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	// cast 5s ago so the 3s cast has completed by the landing emote (the tracker
	// gates a landing on cast-completion); timers then run ~3 min from now.
	now := time.Now().Unix()
	tr.BeginCast("Aegolism", now-5)
	tr.Observe("Aragorn's skin turns to stone.", now)
	tr.BeginCast("Haste", now-5)
	tr.Observe("Aragorn's feet move faster.", now)
	tr.BeginCast("Aegolism", now-5)
	tr.Observe("Legolas's skin turns to stone.", now)
	return sm, tr
}

// TestTimersGroupedByTarget verifies buffs are bucketed under a per-target header
// and that hovering a target adds the ✕ dismiss affordance.
func TestTimersGroupedByTarget(t *testing.T) {
	_, tr := casterScene()
	body, targets := timerColumn(themes[0], tr, 40, "", true, true, true)
	for _, want := range []string{"Aragorn", "Legolas", "Aegolism", "Haste"} {
		if !strings.Contains(body, want) {
			t.Errorf("grouped timers missing %q", want)
		}
	}
	if !containsValue(targets, "Aragorn") || !containsValue(targets, "Legolas") {
		t.Errorf("line→target map missing a target: %v", targets)
	}
	// hovering Aragorn surfaces the ✕ affordance; the others stay plain.
	if hovered, _ := timerColumn(themes[0], tr, 40, "Aragorn", true, true, true); !strings.Contains(hovered, "✕") {
		t.Error("hovered target should render an ✕ dismiss affordance")
	}
}

// TestHoverDismiss drives the model: a mouse-move over a target's row sets the
// hover highlight, and a left-click there dismisses that target's timers.
func TestHoverDismiss(t *testing.T) {
	sm, tr := casterScene()
	var m tea.Model = NewModel(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.refresh()

	// screen cell over Aragorn's group in the class (bottom-left) panel
	ld := mm.layout()
	contentTop := gridTop + ld.dmgH + 2 // outer pad + banner + dmg card + border + title
	rightX := gridX
	line := -1
	for l, tgt := range mm.classTargets {
		if tgt == "Aragorn" && (line < 0 || l < line) {
			line = l
		}
	}
	if line < 0 {
		t.Fatal("Aragorn not present in classTargets")
	}
	x, y := rightX+3, contentTop+(line-mm.vpClass.YOffset)
	if got := mm.hoverTargetAt(x, y); got != "Aragorn" {
		t.Fatalf("hoverTargetAt(%d,%d) = %q, want Aragorn", x, y, got)
	}

	// motion → hover highlight + ✕
	m2, _ := mm.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion})
	mm2 := m2.(Model)
	if mm2.hover != "Aragorn" {
		t.Fatalf("hover = %q, want Aragorn", mm2.hover)
	}
	if !strings.Contains(mm2.View(), "✕") {
		t.Error("hovered view should show the ✕ affordance")
	}

	// click → Aragorn's timers dismissed, Legolas untouched
	m3, _ := mm2.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	mm3 := m3.(Model)
	if containsValue(mm3.classTargets, "Aragorn") {
		t.Error("Aragorn should be dismissed after the click")
	}
	if !containsValue(mm3.classTargets, "Legolas") {
		t.Error("Legolas should remain after dismissing Aragorn")
	}
}

func containsValue(m map[int]string, v string) bool {
	for _, x := range m {
		if x == v {
			return true
		}
	}
	return false
}

// TestTimerTimeNotClipped guards the fix for hour-plus timers: the countdown
// must render in full (h:mm:ss) at a normal width and still survive a narrow
// panel, where the bar and name yield but the time stays.
func TestTimerTimeNotClipped(t *testing.T) {
	now := int64(1000)
	tm := gamestate.Timer{Spell: "Resist Magic", Target: "You", Start: now, Expiry: now + 3*3600 + 5} // 3:00:05
	for _, w := range []int{40, 18, 10} {
		line := timerLine(themes[0], tm, now, w, 7)
		if !strings.Contains(line, "3:00:05") {
			t.Errorf("w=%d: full time clipped: %q", w, line)
		}
	}
}

// TestDamageNoOverflow guards the enriched Damage panel's column math: no
// rendered line may exceed the panel width at any size (the previous UI clipped silently;
// lipgloss would push past the card border).
func TestDamageNoOverflow(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	cur := mm.sessions[mm.effectiveSel()]
	for _, w := range []int{20, 28, 34, 40, 56, 64, 90, 120} {
		for i, line := range strings.Split(mm.damageContent(cur, true, w), "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("damage w=%d line %d overflows (%d): %q", w, i, lw, line)
			}
		}
		for i, line := range strings.Split(mm.extrasContent(cur, w), "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("extras w=%d line %d overflows (%d): %q", w, i, lw, line)
			}
		}
	}
}

// TestViewFitsWindow guards against the "scrunched on first open" bug: the
// rendered frame must fit the window exactly — no line wider than the width
// (which would wrap), and no more lines than the height. Also checks a
// degenerate early size is ignored.
func TestViewFitsWindow(t *testing.T) {
	sizes := [][2]int{{60, 20}, {70, 24}, {73, 24}, {80, 24}, {100, 30}, {120, 40}, {160, 50}}
	smv, trv := monkScene() // a populated scene (class + zone in the banner)
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		var m tea.Model = NewModel(smv, trv, "Kelkix")
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		mm := m.(Model)
		mm.refresh()
		out := mm.View()
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Errorf("%dx%d: %d lines > height %d (content wrapped)", w, h, len(lines), h)
		}
		for i, line := range lines {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("%dx%d: line %d width %d > %d: %q", w, h, i, lw, w, line)
			}
		}

		// the Settings tab's two-column layout must fit the width at every size too.
		mm.screen = screenSettings
		for i, line := range strings.Split(mm.View(), "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("settings %dx%d: line %d width %d > %d: %q", w, h, i, lw, w, line)
			}
		}

		// the optional class box (under the Mob Tracker) must also fit.
		mm.screen = screenMeter
		mm.layoutPrefs.ClassBox = true
		mm.resizeViewports()
		mm.refresh()
		boxOut := strings.Split(mm.View(), "\n")
		if len(boxOut) > h {
			t.Errorf("class box %dx%d: %d lines > height %d", w, h, len(boxOut), h)
		}
		for i, line := range boxOut {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("class box %dx%d: line %d width %d > %d: %q", w, h, i, lw, w, line)
			}
		}
	}

	// a degenerate early size must not flip the model into rendering (stay "starting…")
	var m tea.Model = NewModel(sampleManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	if mm := m.(Model); mm.ready {
		t.Error("a 0x0 size should be ignored, not mark the model ready")
	}
}

// TestResistBadgeRendered verifies a target-resisted cast surfaces as a badge in
// the class panel (the user-facing "show when the spell doesn't land" cue).
func TestResistBadgeRendered(t *testing.T) {
	sm, tr := casterScene()
	tr.Observe("Your target resisted the Aegolism spell.", time.Now().Unix())
	out := renderAtTr(sm, tr, 0, 120, 40)
	if !strings.Contains(out, "Aegolism") || !strings.Contains(out, "resisted") {
		t.Errorf("expected an \"Aegolism resisted\" badge in the rendered view")
	}
}

// TestSwitchCharacter verifies a hot-swap message updates the banner name and
// resets the selection (the watcher clears the shared manager/tracker).
func TestSwitchCharacter(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	mm := m.(Model)
	mm.selected, mm.follow = 1, false // pretend the user pinned an old fight

	m2, _ := mm.Update(switchMsg{character: "Kelkas"})
	mm2 := m2.(Model)
	if mm2.character != "Kelkas" {
		t.Errorf("character = %q, want Kelkas", mm2.character)
	}
	if !mm2.follow || mm2.selected != 0 {
		t.Errorf("switch should reset selection (follow=%v selected=%d)", mm2.follow, mm2.selected)
	}
	if !strings.Contains(mm2.View(), "Kelkas") {
		t.Error("banner should show the new character after a switch")
	}
}

// twoSessionManager forces two distinct fights (a big gap between them).
// mobSceneTracker has one of the player's kills and one of someone else's, in a
// zone with a known respawn — so the Mob Tracker shows the separator + killer.
func mobSceneTracker() *gamestate.Tracker {
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(50)
	now := time.Now().Unix()
	tr.Observe("You have entered east commonlands.", now-600)
	tr.Observe("You have slain a young kodiak!", now-100)
	tr.Observe("a fippy darkpaw has been slain by Sue!", now-100)
	return tr
}

func TestClearSessions(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	mm := m.(Model)
	if len(mm.sessions) == 0 {
		t.Fatal("expected sessions before clear")
	}
	m2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if mm2 := m2.(Model); len(mm2.sessions) != 0 {
		t.Errorf("backspace should clear sessions, got %d", len(mm2.sessions))
	}
}

func TestMobTrackerSeparatorAndKiller(t *testing.T) {
	out, targets := mobTracker(themes[0], mobSceneTracker(), 44, "")
	if !strings.Contains(out, "killed by others") {
		t.Error("expected a \"killed by others\" separator")
	}
	if !strings.Contains(out, "Sue") {
		t.Error("expected the killer's name on others' kills")
	}
	if !containsValue(targets, "a young kodiak") {
		t.Errorf("line→mob map missing the player's kill: %v", targets)
	}
}

func TestRepopEditFlow(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), mobSceneTracker(), "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)

	ld := mm.layout()
	contentTop := gridTop + ld.dmgH + 2
	line, mob := -1, ""
	for l, mb := range mm.mobTargets {
		if line < 0 || l < line {
			line, mob = l, mb
		}
	}
	if line < 0 {
		t.Fatal("no mob targets to click")
	}
	mobX := gridX + ld.classW + 1
	if ld.enemy {
		mobX += ld.enemyW + 1
	}
	x, y := mobX+2, contentTop+(line-mm.vpMob.YOffset)
	if got := mm.mobAt(x, y); got != mob {
		t.Fatalf("mobAt(%d,%d) = %q, want %q", x, y, got, mob)
	}

	// click opens the editor for that mob
	m2, _ := mm.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	mm = m2.(Model)
	if !mm.editing || mm.editMob != mob {
		t.Fatalf("click should open the editor for %q (editing=%v mob=%q)", mob, mm.editing, mm.editMob)
	}
	// type "5:00", then Enter saves and closes
	for _, k := range []string{"5", ":", "0", "0"} {
		m3, _ := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		mm = m3.(Model)
	}
	if mm.editBuf != "5:00" {
		t.Fatalf("editBuf = %q, want 5:00", mm.editBuf)
	}
	m4, _ := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mm4 := m4.(Model); mm4.editing {
		t.Error("Enter should close the repop editor")
	}
}

// TestAvoidanceShowsRiposte: the vertical avoidance breakdown includes Riposte
// (and full names) for a defender that riposted.
func TestAvoidanceShowsRiposte(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 50, Target: "a rat"})
	sm.ApplySwing(&combat.Swing{ActionTime: 1001, Attacker: "You", Defender: "a rat", Outcome: combat.OutcomeRiposte})
	out := damageAvoidance(themes[0], sm.Current(), 40)
	for _, want := range []string{"Avoided", "Riposte", "faced"} {
		if !strings.Contains(out, want) {
			t.Errorf("avoidance missing full label %q in:\n%s", want, out)
		}
	}
}

// TestDueAnnouncements covers the low-buff cue logic (each fires once, re-arms
// on refresh/expiry, charm is skipped, self-buffs use the short phrase).
func TestDueAnnouncements(t *testing.T) {
	m := NewModel(&session.SessionManager{}, nil, "Kelkix")
	now := int64(1000)
	healthy := gamestate.Timer{Spell: "Clarity", Target: "Tankguy", Expiry: now + 600}
	low := gamestate.Timer{Spell: "Clarity", Target: "Healer", Expiry: now + 8}
	charm := gamestate.Timer{Spell: "Charm", Target: "Charm", Expiry: now + 5, Charm: true}

	got := m.dueAnnouncements([]gamestate.Timer{healthy, low, charm}, now)
	if len(got) != 1 || got[0].Spell != "Clarity" || got[0].Target != "Healer" {
		t.Fatalf("first pass = %v, want one Clarity/Healer", got)
	}
	if got := m.dueAnnouncements([]gamestate.Timer{healthy, low, charm}, now); got != nil {
		t.Errorf("repeat pass spoke again: %v", got)
	}
	// refreshed (healthy) re-arms; dropping low again speaks
	m.dueAnnouncements([]gamestate.Timer{{Spell: "Clarity", Target: "Healer", Expiry: now + 600}}, now)
	if got := m.dueAnnouncements([]gamestate.Timer{{Spell: "Clarity", Target: "Healer", Expiry: now + 5}}, now); len(got) != 1 {
		t.Errorf("refresh should re-arm; got %v", got)
	}
}

// TestPetRollup: the detected pet renders as an indented "↳" child directly
// under the You row, not as its own ranked dealer.
func TestPetRollup(t *testing.T) {
	sm := sampleManager() // "You" is the top dealer
	sm.Apply(&combat.DamageSet{ActionTime: 1041, Dealer: "Xenab", Dmg: 120_000, Target: "a sand giant"})
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetCharacter("Kelkix")
	tr.Observe("Xenab says 'My leader is Kelkix.'", time.Now().Unix())

	var m tea.Model = NewModel(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	out := mm.damageContent(mm.sessions[mm.effectiveSel()], true, 80)

	if !strings.Contains(out, "↳ Xenab") {
		t.Fatalf("pet should render as an indented child; got:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	youIdx, petIdx := -1, -1
	for i, l := range lines {
		if youIdx < 0 && strings.Contains(l, "You") {
			youIdx = i
		}
		if strings.Contains(l, "Xenab") {
			petIdx = i
		}
	}
	if youIdx < 0 || petIdx != youIdx+1 {
		t.Errorf("pet should sit directly under You (you=%d pet=%d)", youIdx, petIdx)
	}
}

// TestYouRowInclusiveOfPetAndSpells: the You row's total rolls UP its pet and
// spell damage (melee + spells + pet), so its share/DPS/ranking reflect the whole
// of the player's output — while the pet and spells still show as breakdown
// children. Guards the bug where the pet was broken out but never added back.
func TestYouRowInclusiveOfPetAndSpells(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 100_000, Target: "a sand giant"})
	sm.Apply(&combat.DamageSet{ActionTime: 1001, Dealer: "Gabnador", Dmg: 120_000, Target: "a sand giant"})
	sm.Apply(&combat.DamageSet{ActionTime: 1002, Dealer: "Xenab", Dmg: 80_000, Target: "a sand giant"}) // your pet
	sm.ApplyMagic(&combat.Magic{ActionTime: 1003, Dmg: 40_000, Target: "a sand giant"})
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetCharacter("Kelkix")
	tr.Observe("Xenab says 'My leader is Kelkix.'", time.Now().Unix())

	var m tea.Model = NewModel(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	out := mm.damageContent(mm.sessions[mm.effectiveSel()], true, 80)

	lines := strings.Split(out, "\n")
	youIdx, gabIdx := -1, -1
	for i, l := range lines {
		if youIdx < 0 && strings.Contains(l, "You") && !strings.Contains(l, "↳") {
			youIdx = i
		}
		if gabIdx < 0 && strings.Contains(l, "Gabnador") {
			gabIdx = i
		}
	}
	if youIdx < 0 || gabIdx < 0 {
		t.Fatalf("missing rows (you=%d gab=%d):\n%s", youIdx, gabIdx, out)
	}
	// You = 100k melee + 80k pet + 40k spells = 220k inclusive, shown on the row.
	if want := humanize(220_000); !strings.Contains(lines[youIdx], want) {
		t.Errorf("You row should show inclusive total %s; got %q", want, lines[youIdx])
	}
	// 220k inclusive outranks Gabnador's 120k (without the rollup You's 100k wouldn't)
	if youIdx > gabIdx {
		t.Errorf("inclusive You should rank above Gabnador (you=%d gab=%d)", youIdx, gabIdx)
	}
	// the breakdown children are still present
	if !strings.Contains(out, "↳ Xenab") || !strings.Contains(out, "↳ spells") {
		t.Errorf("pet and spells should still show as breakdown children:\n%s", out)
	}
}

// TestSpellsAttributedToYou: non-melee damage is credited to You (a "↳ spells"
// child), not shown as an unattributed lump, when a You row is present.
func TestSpellsAttributedToYou(t *testing.T) {
	sm := sampleManager()
	sm.ApplyMagic(&combat.Magic{ActionTime: 1042, Dmg: 240_000, Target: "a sand giant"})
	var m tea.Model = NewModel(sm, nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	out := mm.damageContent(mm.sessions[mm.effectiveSel()], true, 80)

	if !strings.Contains(out, "↳ spells") {
		t.Fatalf("spell damage should be a \"↳ spells\" child of You; got:\n%s", out)
	}
	if strings.Contains(out, "spells n/a") {
		t.Error("should not show an unattributed spells line when You is present")
	}
	lines := strings.Split(out, "\n")
	youIdx, spIdx := -1, -1
	for i, l := range lines {
		if youIdx < 0 && strings.Contains(l, "You") {
			youIdx = i
		}
		if strings.Contains(l, "spells") {
			spIdx = i
		}
	}
	if youIdx < 0 || spIdx != youIdx+1 {
		t.Errorf("spells should sit directly under You (you=%d spells=%d)", youIdx, spIdx)
	}
}

// TestScrollHint: a viewport with off-screen content advertises it in the title.
func TestScrollHint(t *testing.T) {
	over := viewport.New(20, 3)
	over.SetContent("a\nb\nc\nd\ne\nf") // 6 lines in a height-3 viewport → more below
	if h := scrollHint(over); h != " ▾" {
		t.Errorf("overflow (at top) should hint ▾, got %q", h)
	}
	fits := viewport.New(20, 10)
	fits.SetContent("a\nb")
	if h := scrollHint(fits); h != "" {
		t.Errorf("content that fits should have no hint, got %q", h)
	}
}

// TestTimerGroupDivider: a rule separates one person's buffs from the next.
func TestTimerGroupDivider(t *testing.T) {
	_, tr := casterScene() // Aragorn (2 buffs) + Legolas (1) → 2 groups
	body, _ := timerColumn(themes[0], tr, 40, "", true, true, true)
	if !strings.Contains(body, "─") {
		t.Errorf("expected a divider between buff groups; got:\n%s", body)
	}
}

// TestPetAttributedToOwner: a group-mate's pet nests under its owner's row (not
// under You) when the owner also dealt damage.
func TestPetAttributedToOwner(t *testing.T) {
	sm := sampleManager()
	sm.Apply(&combat.DamageSet{ActionTime: 1041, Dealer: "Sensive", Dmg: 300_000, Target: "a sand giant"})
	sm.Apply(&combat.DamageSet{ActionTime: 1042, Dealer: "Zarekab", Dmg: 150_000, Target: "a sand giant"})
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetCharacter("Kelkix")
	tr.Observe("Zarekab says 'My leader is Sensive.'", time.Now().Unix())

	var m tea.Model = NewModel(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	out := mm.damageContent(mm.sessions[mm.effectiveSel()], true, 80)

	if !strings.Contains(out, "↳ Zarekab") {
		t.Fatalf("Zarekab should nest under its owner; got:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	si, zi, yi := -1, -1, -1
	for i, l := range lines {
		if si < 0 && strings.Contains(l, "Sensive") {
			si = i
		}
		if strings.Contains(l, "Zarekab") {
			zi = i
		}
		if yi < 0 && strings.Contains(l, "You") {
			yi = i
		}
	}
	if si < 0 || zi != si+1 {
		t.Errorf("Zarekab should be Sensive's child (sensive=%d zarekab=%d)", si, zi)
	}
	if zi == yi+1 {
		t.Error("Zarekab must NOT nest under You")
	}
}

// TestPetTaggedWhenOwnerAbsent: a pet whose owner dealt no damage stays a ranked
// row, tagged with the owner — never silently lumped under You.
func TestPetTaggedWhenOwnerAbsent(t *testing.T) {
	sm := sampleManager() // no "Sensive" dealer
	sm.Apply(&combat.DamageSet{ActionTime: 1042, Dealer: "Zarekab", Dmg: 150_000, Target: "a sand giant"})
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetCharacter("Kelkix")
	tr.Observe("Zarekab says 'My leader is Sensive.'", time.Now().Unix())

	var m tea.Model = NewModel(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	out := mm.damageContent(mm.sessions[mm.effectiveSel()], true, 80)

	if !strings.Contains(out, "Sensive's pet") {
		t.Errorf("an orphan pet should be credited to its owner; got:\n%s", out)
	}
	if strings.Contains(out, "↳ Zarekab") {
		t.Error("with no owner row, the pet should stay ranked, not nested")
	}
}

// TestTimersSplitBuffsDebuffs: a debuff on a mob and a buff on a player land in
// separate sections (DEBUFFS before BUFFS), never interleaved.
func TestTimersSplitBuffsDebuffs(t *testing.T) {
	book, _ := gamestate.LoadReader(strings.NewReader(strings.Join([]string{
		buffRow("Aegolism", "'s skin turns to stone."),
		debuffRow("Malosini", "'s magic resistances are lowered."),
	}, "\n")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	now := time.Now().Unix()
	tr.BeginCast("Aegolism", now-5)
	tr.Observe("Aragorn's skin turns to stone.", now) // buff on a player
	tr.BeginCast("Malosini", now-5)
	tr.Observe("a sand giant's magic resistances are lowered.", now) // debuff on a mob

	body, _ := timerColumn(themes[0], tr, 40, "", true, true, true)
	if !strings.Contains(body, "DEBUFFS") {
		t.Fatalf("expected a DEBUFFS section; got:\n%s", body)
	}
	mob := strings.Index(body, "a sand giant")
	ally := strings.Index(body, "Aragorn")
	if mob < 0 || ally < 0 || mob > ally {
		t.Errorf("debuff target (mob) should sit in its own section before the buffed ally (mob=%d ally=%d)", mob, ally)
	}
}

// mixedCaster: a shaman with a buff on an ally and a debuff on a mob, for the
// Enemy-column routing tests.
func mixedCaster(t *testing.T) *gamestate.Tracker {
	t.Helper()
	book, _ := gamestate.LoadReader(strings.NewReader(strings.Join([]string{
		buffRow("Aegolism", "'s skin turns to stone."),
		debuffRow("Malosini", "'s magic resistances are lowered."),
	}, "\n")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassShaman)
	now := time.Now().Unix()
	tr.BeginCast("Aegolism", now-5)
	tr.Observe("Aragorn's skin turns to stone.", now) // buff on an ally
	tr.BeginCast("Malosini", now-5)
	tr.Observe("a sand giant's magic resistances are lowered.", now) // debuff on a mob
	return tr
}

// TestEnemyColumnRouting: with the Enemy column shown, debuffs go to it and buffs
// stay in the class panel — never crossed.
func TestEnemyColumnRouting(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), mixedCaster(t), "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 34})
	mm := m.(Model)
	if !mm.layout().enemy {
		t.Fatal("expected the Enemy column at this width")
	}
	classBody, enemyBody := mm.vpClass.View(), mm.vpEnemy.View()
	if !strings.Contains(classBody, "Aragorn") || strings.Contains(classBody, "a sand giant") {
		t.Errorf("class panel should hold the buff (Aragorn), not the debuff target")
	}
	if !strings.Contains(enemyBody, "a sand giant") || strings.Contains(enemyBody, "Aragorn") {
		t.Errorf("Enemy column should hold the debuff (a sand giant), not the buff target")
	}
}

// TestEnemyColumnNarrowFallback: below the width threshold the Enemy column drops
// and both buffs and debuffs fold back into the class panel.
func TestEnemyColumnNarrowFallback(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), mixedCaster(t), "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 58, Height: 34})
	mm := m.(Model)
	if mm.layout().enemy {
		t.Fatal("expected no Enemy column on a narrow window")
	}
	body := mm.vpClass.View()
	if !strings.Contains(body, "Aragorn") || !strings.Contains(body, "a sand giant") {
		t.Errorf("narrow fallback: class panel should hold both buffs and debuffs;\n%s", body)
	}
}

func TestPanelsRenderLiveData(t *testing.T) {
	out := renderAt(sampleManager(), 0, 100, 32)
	for _, want := range []string{
		"99dps",           // banner
		"a sand giant",    // sessions list + damage title
		"You", "Gabnador", // damage rows
		"Sessions", "Spell Timers", "Mob Tracker", // panel titles
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered layout missing %q", want)
		}
	}
	// an empty manager must not panic and should show a placeholder
	if out := renderAt(&session.SessionManager{}, 0, 90, 28); !strings.Contains(out, "No fight") {
		t.Errorf("empty manager should render a placeholder, got:\n%s", out)
	}
}

// TestClassAwarePanels verifies the bottom panel switches on class: a melee
// class shows SKILLS + COOLDOWNS, and an enchanter gets a Crowd Control column.
func TestClassAwarePanels(t *testing.T) {
	sm, tr := monkScene()
	out := renderAtTr(sm, tr, 0, 110, 34)
	for _, want := range []string{"Skills", "SKILLS", "COOLDOWNS", "Mend"} {
		if !strings.Contains(out, want) {
			t.Errorf("monk panel missing %q", want)
		}
	}

	// a caster (enchanter here) gets the dedicated Enemy column (CC + debuffs);
	// the class panel becomes "Buffs".
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	ench := gamestate.NewTracker(book)
	ench.SetClass(eqclass.ClassEnchanter)
	if out := renderAtTr(sampleManager(), ench, 0, 110, 34); !strings.Contains(out, "Enemy") || !strings.Contains(out, "Buffs") {
		t.Errorf("caster layout should include an Enemy column and a Buffs panel")
	}
}

// TestWriteShots dumps truecolor frames for screenshotting with `freeze`. Gated
// behind TUI_SHOT so it doesn't run (or write) in normal CI.
func TestWriteShots(t *testing.T) {
	if os.Getenv("TUI_SHOT") == "" {
		t.Skip("set TUI_SHOT=1 to write /tmp/tui<theme>.ansi previews")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	_ = time.Now()
	// theme 0/1: caster scenario; theme 2: a Monk (melee) scenario showing the
	// class-aware Skills + Cooldowns panel.
	if err := os.WriteFile("/tmp/tui0.ansi", []byte(renderAt(sampleManager(), 0, 108, 34)), 0o644); err != nil {
		t.Fatal(err)
	}
	smMonk, trMonk := monkScene()
	if err := os.WriteFile("/tmp/tui-monk.ansi", []byte(renderAtTr(smMonk, trMonk, 0, 108, 34)), 0o644); err != nil {
		t.Fatal(err)
	}
	// caster scene with the cursor hovering a buff target, to show the ✕ affordance
	smC, trC := casterScene()
	var m tea.Model = NewModel(smC, trC, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 108, Height: 34})
	mc := m.(Model)
	mc.refresh()
	ld := mc.layout()
	for l, tgt := range mc.classTargets {
		if tgt == "Aragorn" {
			mc2, _ := mc.Update(tea.MouseMsg{X: gridX + 4, Y: gridTop + ld.dmgH + 2 + l, Action: tea.MouseActionMotion})
			mc = mc2.(Model)
			break
		}
	}
	if err := os.WriteFile("/tmp/tui-caster.ansi", []byte(mc.View()), 0o644); err != nil {
		t.Fatal(err)
	}

	// mob-tracker scene: kills staggered so repops span every urgency state
	// (UP / imminent / soon / counting), to eyeball the brighter names + tints.
	book2, _ := gamestate.LoadReader(strings.NewReader(""))
	trMob := gamestate.NewTracker(book2)
	trMob.SetLevel(50)
	now := time.Now().Unix()
	trMob.Observe("You have entered east commonlands.", now-600)         // default repop 400s
	trMob.Observe("a giant rat has been slain by Thunk!", now)           // ~400s → dim
	trMob.Observe("a decaying skeleton has been slain by Bob!", now-340) // ~60s → gold
	trMob.Observe("You have slain a young kodiak!", now-382)             // ~18s → red
	trMob.Observe("a fippy darkpaw has been slain by Sue!", now-410)     // UP → green
	if err := os.WriteFile("/tmp/tui-mob.ansi", []byte(renderAtTr(sampleManager(), trMob, 0, 108, 34)), 0o644); err != nil {
		t.Fatal(err)
	}

	// plain-text dump of just the damage panel content, to eyeball the columns
	var dm tea.Model = NewModel(sampleManager(), nil, "Kelkix")
	dm, _ = dm.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	dmm := dm.(Model)
	if err := os.WriteFile("/tmp/tui-damage.txt", []byte(dmm.damageContent(dmm.sessions[dmm.effectiveSel()], true, 80)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// canniRow builds a Cannibalize spell row with a recast time (drives the meter).
func canniRow(name string, recastMs int) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name                    // fName
	f[15] = strconv.Itoa(recastMs) // recast (ms) — the dance beat
	return strings.Join(f, "^")
}

// TestCanniPinnedInDamagePanel: a live canni dance shows pinned at the bottom of
// the Damage panel (not in the class/Buffs panel), and its height is reserved
// out of the dealer viewport so it can't scroll away.
func TestCanniPinnedInDamagePanel(t *testing.T) {
	book, _ := gamestate.LoadReader(strings.NewReader(canniRow("Cannibalize III", 9000)))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassShaman)
	now := time.Now().Unix()
	tr.BeginCast("Cannibalize III", now-9)
	tr.BeginCast("Cannibalize III", now) // second cast within the window → Active

	var m tea.Model = NewModel(sampleManager(), tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	mm := m.(Model)

	if !strings.Contains(mm.View(), "CANNI DANCE") {
		t.Fatal("the canni dance meter should be visible in the view")
	}
	if strings.Contains(mm.vpClass.View(), "CANNI") {
		t.Error("canni should no longer render in the class/Buffs panel")
	}
	ld := mm.layout()
	if _, dmgInnerH := cardInner(ld.dmgW, ld.dmgH); mm.vpDamage.Height >= dmgInnerH {
		t.Errorf("dealer viewport height %d should be reduced below %d to pin the canni footer", mm.vpDamage.Height, dmgInnerH)
	}
}

// TestCanniFooterIdleAndEmpty: before any dance the footer is absent; an idle
// (lapsed) dance keeps a muted summary line.
func TestCanniFooterIdleAndEmpty(t *testing.T) {
	th := themes[0]
	if s, n := canniFooter(th, gamestate.CanniStats{}, 50); s != "" || n != 0 {
		t.Errorf("no dance → empty footer, got %q / %d lines", s, n)
	}
	s, n := canniFooter(th, gamestate.CanniStats{Best: 88, Score: 1200, Rank: "Cannibalize III"}, 50)
	if n != 4 || !strings.Contains(s, "idle") || !strings.Contains(s, "best 88%") {
		t.Errorf("idle footer should be a 4-line muted summary with best%%, got %d lines:\n%s", n, s)
	}
}

// TestBuffsPanelNoRedundantHeader: the dedicated Buffs column (only wantBuffs)
// drops the inner "BUFFS" header — the card title already says it — but the
// buffs themselves and their target headers remain. The full stack keeps it.
func TestBuffsPanelNoRedundantHeader(t *testing.T) {
	_, tr := casterScene()
	solo, _ := timerColumn(themes[0], tr, 40, "", false, false, true)
	if strings.Contains(solo, "BUFFS") {
		t.Errorf("dedicated Buffs panel should omit the inner BUFFS header;\n%s", solo)
	}
	for _, want := range []string{"Aragorn", "Aegolism"} {
		if !strings.Contains(solo, want) {
			t.Errorf("buffs content should still render %q;\n%s", want, solo)
		}
	}
	if full, _ := timerColumn(themes[0], tr, 40, "", true, true, true); !strings.Contains(full, "BUFFS") {
		t.Error("the full CC+DEBUFFS+BUFFS stack should keep the BUFFS header to disambiguate")
	}
}

// TestCasterAppearsAndLinksPet: a player who never melees but casts (spell damage)
// and owns a pet should appear as "You" on the chart, with the spell damage
// attributed to them and the pet nested under them — never "spells n/a" or an
// orphaned pet.
func TestCasterAppearsAndLinksPet(t *testing.T) {
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "Borric", Dmg: 50_000, Target: "a sand giant"}) // a group-mate melees
	sm.Apply(&combat.DamageSet{ActionTime: 1001, Dealer: "Xenab", Dmg: 30_000, Target: "a sand giant"})  // the player's pet melees
	sm.ApplyMagic(&combat.Magic{ActionTime: 1002, Dmg: 90_000, Target: "a sand giant"})                  // the player's nuke/DoT — no caster in the log
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetCharacter("Kelkix")
	tr.Observe("Xenab says 'My leader is Kelkix.'", time.Now().Unix())

	var m tea.Model = NewModel(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	out := mm.damageContent(mm.sessions[mm.effectiveSel()], true, 80)

	if strings.Contains(out, "spells n/a") {
		t.Errorf("a casting player must be attributed, not 'spells n/a';\n%s", out)
	}
	lines := strings.Split(out, "\n")
	yi, si, xi := -1, -1, -1
	for i, l := range lines {
		if yi < 0 && strings.Contains(l, "You") {
			yi = i
		}
		if strings.Contains(l, "spells") {
			si = i
		}
		if strings.Contains(l, "Xenab") {
			xi = i
		}
	}
	if yi < 0 {
		t.Fatalf("the player should appear as You once they cast;\n%s", out)
	}
	if si != yi+1 {
		t.Errorf("the spell damage should nest right under You (you=%d spells=%d)", yi, si)
	}
	if xi != yi+2 {
		t.Errorf("the player's pet should nest under You, after the spells row (you=%d pet=%d)", yi, xi)
	}
}

// TestCooldownBoxes: cooldowns render as a charge box — a half-charged Mend shows
// a partial fill (█ and ░), a ready ability shows "ready", and rows fit the width.
func TestCooldownBoxes(t *testing.T) {
	th := themes[0]
	cds := []gamestate.CooldownTimer{
		{Name: "Mend", Remaining: 180, Total: 360},     // half charged → partial box
		{Name: "Feign Death", Remaining: 0, Total: 11}, // ready → full box
	}
	out := cooldownRows(th, cds, 40)
	if !strings.Contains(out, "Mend") || !strings.Contains(out, "ready") {
		t.Fatalf("expected Mend + a ready ability; got:\n%s", out)
	}
	if !strings.Contains(out, "█") || !strings.Contains(out, "░") {
		t.Errorf("a half-charged cooldown should show a partial box (█ and ░); got:\n%s", out)
	}
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 40 {
			t.Errorf("cooldown row overflows width 40 (%d): %q", lipgloss.Width(ln), ln)
		}
	}
}

// monkSelfBuff builds a monk tracker carrying one self-buff timer (an instant SoW
// clicky landing on You), for the melee Buffs-column tests.
func monkSelfBuff(t *testing.T) *gamestate.Tracker {
	t.Helper()
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = "Spirit of Wolf"
	f[6] = "You feel the spirit of wolf enter you." // cast_on_you
	f[13] = "0"                                     // instant
	f[16] = "3"
	f[17] = "360"
	f[83] = "1" // beneficial
	book, _ := gamestate.LoadReader(strings.NewReader(strings.Join(f, "^")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassMonk)
	now := time.Now().Unix()
	tr.BeginCast("Spirit of Wolf", now)
	tr.Observe("You feel the spirit of wolf enter you.", now)
	return tr
}

// TestMeleeBuffsColumn: a melee class gets a third "Buffs" column (not "Enemy"),
// and self-buff timers render there via the shared timerColumn code.
func TestMeleeBuffsColumn(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), monkSelfBuff(t), "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)

	if !mm.layout().enemy || !mm.middleIsBuffs() {
		t.Fatal("a melee class at this width should get a Buffs middle column")
	}
	if mid := mm.vpEnemy.View(); !strings.Contains(mid, "Spirit") || !strings.Contains(mid, "You") {
		t.Errorf("the self-buff should render in the Buffs column;\n%s", mid)
	}
	if strings.Contains(mm.vpClass.View(), "Spirit") {
		t.Error("the buff belongs in the Buffs column, not the Skills panel")
	}
	if v := mm.View(); !strings.Contains(v, "Buffs") || strings.Contains(v, "Enemy") {
		t.Error("melee bottom row should label the column Buffs, with no Enemy column")
	}
}

// TestMeleeBuffsFoldNarrow: too narrow for a third column, the self-buffs fold
// into the Skills panel rather than vanish.
func TestMeleeBuffsFoldNarrow(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), monkSelfBuff(t), "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 58, Height: 40})
	mm := m.(Model)
	mm.layoutPrefs.DiscHidden = true // testing buff-fold, not the discipline widget
	mm.refresh()
	if mm.layout().enemy {
		t.Fatal("expected no middle column at this narrow width")
	}
	if !strings.Contains(mm.vpClass.View(), "Spirit") {
		t.Errorf("narrow fallback: self-buffs should fold into the Skills panel;\n%s", mm.vpClass.View())
	}
}

// TestBuffsYouPinnedTop: your own buffs stay at the top of the list even when
// others' buffs fade sooner.
func TestBuffsYouPinnedTop(t *testing.T) {
	now := int64(1000)
	ts := []gamestate.Timer{
		{Spell: "Haste", Target: "Aragorn", Start: now, Expiry: now + 60},
		{Spell: "SoW", Target: "Legolas", Start: now, Expiry: now + 30}, // fades soonest
		{Spell: "Aegolism", Target: "You", Start: now, Expiry: now + 3000},
	}
	_, order := groupByTargetTimers(ts)
	if len(order) == 0 || order[0] != "You" {
		t.Errorf("your own buffs should pin to the top; got order %v", order)
	}
	// the rest stay soonest-first (Legolas 30s before Aragorn 60s)
	if order[1] != "Legolas" || order[2] != "Aragorn" {
		t.Errorf("non-You groups should stay soonest-first; got %v", order)
	}
}

// ---- moved from announce_test.go (cue/announcement helpers in model.go) ----

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

// ---- moved from cuegating_test.go (cue gating in model.go) ----

// TestDueAnnouncementsGatesByType: a disabled cue type stays silent while other
// types still fire from the same tick.
func TestDueAnnouncementsGatesByType(t *testing.T) {
	m := &Model{
		announced: map[string]gamestate.Urgency{},
		cues:      cuePrefs{Overrides: map[string]bool{cueMez: false}}, // mez cues off
	}
	now := int64(1000)
	mez := gamestate.Timer{Spell: "Lull", Target: "a gnoll", Start: now - 100, Expiry: now + 5, Mez: true}
	deb := gamestate.Timer{Spell: "Cripple", Target: "a gnoll", Start: now - 100, Expiry: now + 5, Detrimental: true}

	due := m.dueAnnouncements([]gamestate.Timer{mez, deb}, now)
	if len(due) != 1 || due[0].Spell != "Cripple" {
		t.Fatalf("mez disabled → only the debuff should be due, got %+v", due)
	}
}

// TestDebuffCueFiresOncePerMob: a debuff/DoT on a mob warns at most once, even
// when re-applied (refreshed) — a necro re-dotting shouldn't re-announce.
func TestDebuffCueFiresOncePerMob(t *testing.T) {
	m := &Model{announced: map[string]gamestate.Urgency{}}
	now := int64(1000)
	dot := func(exp int64) gamestate.Timer {
		return gamestate.Timer{Spell: "Heat Blood", Target: "a spectre", Start: now - 100, Expiry: exp, Detrimental: true}
	}
	// low → warns once
	if got := m.dueAnnouncements([]gamestate.Timer{dot(now + 5)}, now); len(got) != 1 {
		t.Fatalf("first low should warn, got %v", got)
	}
	// refreshed (healthy again) → no re-arm for a debuff
	if got := m.dueAnnouncements([]gamestate.Timer{dot(now + 600)}, now); len(got) != 0 {
		t.Errorf("a refresh shouldn't re-announce, got %v", got)
	}
	// low again after the refresh → still silent (already warned this mob)
	if got := m.dueAnnouncements([]gamestate.Timer{dot(now + 5)}, now); len(got) != 0 {
		t.Errorf("a re-dotted DoT should warn at most once per mob, got %v", got)
	}
}

// TestSlainFlushesQueuedCues: when a mob death clears its debuffs, queued fade
// cues about it are flushed so they don't keep playing after the kill.
func TestSlainFlushesQueuedCues(t *testing.T) {
	fe := &fakeEngine{}
	tr := mixedCaster(t) // holds a Malosini debuff on "a sand giant"
	m := &Model{ttsOn: true, tracker: tr, speaker: fe}
	now := time.Now().Unix()

	m.announceCuesAt(now) // baseline: record the current slain-clear count
	base := fe.flushed

	tr.Observe("You have slain a sand giant!", now+1) // clears the debuff → bumps the counter
	m.announceCuesAt(now + 2)
	if fe.flushed != base+1 {
		t.Errorf("a mob death should flush queued cues once, got %d (base %d)", fe.flushed, base)
	}
}

// TestSpawnSoonCue: a tracked repop cues once when it crosses ~30s out, and the
// toggle silences it.
func TestSpawnSoonCue(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	now := time.Now().Unix()
	tr.Observe("You have entered east commonlands.", now)
	tr.UseOverrides(gamestate.LoadOverrides(t.TempDir() + "/ov.json"))
	tr.SetOverride("a griffon", 60)              // 60s repop
	tr.Observe("You have slain a griffon!", now) // repop at now+60
	m := &Model{ttsOn: true, tracker: tr, speaker: fe}

	m.announceCuesAt(now + 20) // 40s out → not yet in the 30s window
	if len(fe.normal) != 0 {
		t.Fatalf("no spawn cue at 40s out, got %v", fe.normal)
	}
	m.announceCuesAt(now + 35) // 25s out → fires
	if len(fe.normal) != 1 || !strings.Contains(fe.normal[0], "griffon") {
		t.Fatalf("spawn cue expected ~30s out, got %v", fe.normal)
	}
	m.announceCuesAt(now + 36) // still in window → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("spawn cue should fire once per spawn, got %v", fe.normal)
	}

	// the toggle silences it: a fresh kill with the cue disabled stays quiet.
	fe2 := &fakeEngine{}
	m2 := &Model{ttsOn: true, tracker: tr, speaker: fe2, cues: cuePrefs{Overrides: map[string]bool{cueSpawnSoon: false}}}
	tr.SetOverride("a griffon", 60)
	tr.Observe("You have slain a griffon!", now+200) // a second spawn, repop at now+260
	m2.announceCuesAt(now + 235)                     // 25s out, but disabled
	if len(fe2.normal) != 0 {
		t.Errorf("disabled spawn cue should stay silent, got %v", fe2.normal)
	}
}

// TestHalfCooldownCue: the 50% cue fires once at the halfway mark, then the ready
// cue fires once at expiry.
func TestHalfCooldownCue(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000) // Mend, 360s reuse
	m := &Model{ttsOn: true, tracker: tr, speaker: fe}

	m.announceCuesAt(1000)       // baseline: on cooldown, full
	m.announceCuesAt(1000 + 181) // crossed halfway (179s left ≤ 180)
	if len(fe.normal) != 1 || !strings.Contains(strings.ToLower(fe.normal[0]), "halfway") {
		t.Fatalf("halfway cue expected, got %v", fe.normal)
	}
	m.announceCuesAt(1000 + 182) // still past halfway → no repeat
	if len(fe.normal) != 1 {
		t.Errorf("halfway cue should fire once, got %v", fe.normal)
	}
	m.announceCuesAt(1000 + 361) // ready
	if len(fe.normal) != 2 || !strings.Contains(fe.normal[1], "ready") {
		t.Fatalf("ready cue expected after halfway, got %v", fe.normal)
	}
}

// TestCooldownReadyCueDisabled: turning a skill's ready cue off silences it.
func TestCooldownReadyCueDisabled(t *testing.T) {
	fe := &fakeEngine{}
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.Observe("You mend your wounds and heal some damage.", 1000)
	m := &Model{
		ttsOn: true, tracker: tr, speaker: fe,
		cues: cuePrefs{Overrides: map[string]bool{
			cueCDReady("Mend"): false,
			cueCDHalf("Mend"):  false,
		}},
	}
	m.announceCuesAt(1000)
	m.announceCuesAt(1000 + 181) // halfway, disabled
	m.announceCuesAt(1000 + 361) // ready, disabled
	if len(fe.normal) != 0 {
		t.Errorf("disabled Mend cues should be silent, got %v", fe.normal)
	}
}

// ---- moved from model_more_test.go ----

// TestParseRespawn parses h:mm:ss / m:ss / plain seconds, rejecting junk.
func TestParseRespawn(t *testing.T) {
	cases := []struct {
		in  string
		sec int
		ok  bool
	}{
		{"300", 300, true},
		{"5:00", 300, true},
		{"1:02:03", 3723, true},
		{" 90 ", 90, true},
		{"", 0, false},
		{"0", 0, false}, // zero isn't a valid respawn
		{"0:00", 0, false},
		{"abc", 0, false},
		{"5:xx", 0, false},
		{"-5", 0, false},
	}
	for _, c := range cases {
		sec, ok := parseRespawn(c.in)
		if ok != c.ok || (ok && sec != c.sec) {
			t.Errorf("parseRespawn(%q) = (%d,%v), want (%d,%v)", c.in, sec, ok, c.sec, c.ok)
		}
	}
}

// TestEffectiveSel covers follow-mode, a pinned selection, an over-range pin, and
// an empty manager.
func TestEffectiveSel(t *testing.T) {
	empty := Model{}
	if got := empty.effectiveSel(); got != -1 {
		t.Errorf("empty sessions → -1, got %d", got)
	}
	sm := twoSessionManager()
	m := Model{sessions: sm.All()}
	m.follow = true
	if got := m.effectiveSel(); got != 1 {
		t.Errorf("follow should pick the last session, got %d", got)
	}
	m.follow, m.selected = false, 0
	if got := m.effectiveSel(); got != 0 {
		t.Errorf("a pinned selection should be honored, got %d", got)
	}
	m.selected = 99 // past the end → clamps to last
	if got := m.effectiveSel(); got != 1 {
		t.Errorf("an over-range pin should clamp to last, got %d", got)
	}
	m.follow, m.selected = false, -5 // negative → first
	if got := m.effectiveSel(); got != 0 {
		t.Errorf("a negative pin should clamp to first, got %d", got)
	}
}

// TestCueIDForTimer maps each timer flavor to its cue category.
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

// TestApplyCharSettingsVoiceAndAudio: a saved voice is applied to the engine and
// ttsOn reflects AudioOn && availability.
func TestApplyCharSettingsVoiceAndAudio(t *testing.T) {
	eng := &fakeEngine{}
	m := &Model{speaker: eng}
	m.applyCharSettings(charSettings{AudioOn: true, Voice: "v9", Damage: panelCompact, OffDef: panelOff})
	if !m.ttsOn {
		t.Error("AudioOn with an available engine should turn ttsOn on")
	}
	if m.layoutPrefs.Damage != panelCompact || m.layoutPrefs.OffDef != panelOff {
		t.Errorf("layout prefs should load from the saved settings, got %+v", m.layoutPrefs)
	}

	// AudioOn but no engine → ttsOn stays off (no crash on the nil speaker).
	noEng := &Model{}
	noEng.applyCharSettings(charSettings{AudioOn: true})
	if noEng.ttsOn {
		t.Error("AudioOn with no engine must not enable ttsOn")
	}
}

// editTracker is mobSceneTracker with an attached (temp-file) override store so a
// saved override can be read back.
func editTracker(t *testing.T) (*gamestate.Tracker, *gamestate.Overrides) {
	t.Helper()
	tr := mobSceneTracker()
	ov := gamestate.LoadOverrides(t.TempDir() + "/overrides.json")
	tr.UseOverrides(ov)
	return tr, ov
}

// TestEditKeyBuildsAndSaves drives the repop editor: digits/colon build the
// buffer, backspace deletes, enter saves a valid override.
func TestEditKeyBuildsAndSaves(t *testing.T) {
	tr, ov := editTracker(t)
	m := &Model{sm: &session.SessionManager{}, tracker: tr, editing: true, editMob: "a young kodiak"}

	for _, k := range []string{"7", ":", "3", "0"} {
		m.editKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	if m.editBuf != "7:30" {
		t.Fatalf("editBuf = %q, want 7:30", m.editBuf)
	}
	// a non-digit/colon key is ignored.
	m.editKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.editBuf != "7:30" {
		t.Errorf("a non-digit key should be ignored, got %q", m.editBuf)
	}
	// backspace deletes the last char.
	m.editKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.editBuf != "7:3" {
		t.Errorf("backspace should delete the last char, got %q", m.editBuf)
	}
	// enter saves and closes.
	m.editKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editing {
		t.Error("enter should close the editor")
	}
	if got, ok := ov.Get("east commonlands", "a young kodiak"); !ok || got != 7*60+3 {
		t.Errorf("enter should save the parsed override (got %d/%v)", got, ok)
	}
}

// TestEditKeyEscCancels: esc closes the editor without saving.
func TestEditKeyEscCancels(t *testing.T) {
	tr, ov := editTracker(t)
	m := &Model{sm: &session.SessionManager{}, tracker: tr, editing: true, editMob: "a young kodiak", editBuf: "9:99"}
	m.editKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.editing || m.editBuf != "" {
		t.Errorf("esc should cancel + clear (editing=%v buf=%q)", m.editing, m.editBuf)
	}
	if _, ok := ov.Get("east commonlands", "a young kodiak"); ok {
		t.Error("esc must not save an override")
	}
}

// TestEditKeyEnterInvalidNoSave: enter with an unparseable buffer closes the
// editor but saves nothing.
func TestEditKeyEnterInvalidNoSave(t *testing.T) {
	tr, ov := editTracker(t)
	m := &Model{sm: &session.SessionManager{}, tracker: tr, editing: true, editMob: "a young kodiak", editBuf: ""}
	m.editKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editing {
		t.Error("enter should close the editor even when nothing parses")
	}
	if _, ok := ov.Get("east commonlands", "a young kodiak"); ok {
		t.Error("an empty buffer must not save an override")
	}
}

// TestEditKeyMaxLength: the buffer caps at 8 chars.
func TestEditKeyMaxLength(t *testing.T) {
	m := &Model{sm: &session.SessionManager{}, editing: true, editMob: "x"}
	for i := 0; i < 12; i++ {
		m.editKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")})
	}
	if len(m.editBuf) > 8 {
		t.Errorf("editBuf should cap at 8 chars, got %d (%q)", len(m.editBuf), m.editBuf)
	}
}

// ---- TestToggleTTSNoVoice moved from setup_more_test.go (toggleTTS in model.go) ----

// TestToggleTTSNoVoice: with no available engine, toggling audio flashes guidance
// and leaves audio off (the only branch the fake engine can't exercise).
func TestToggleTTSNoVoice(t *testing.T) {
	m := Model{} // nil speaker
	m.toggleTTS()
	if m.ttsOn {
		t.Error("audio should stay off with no engine")
	}
	if !strings.Contains(m.status, "no voice") {
		t.Errorf("expected a no-voice hint, got %q", m.status)
	}
}

// ---- moved from layout_test.go (layout()/damageContent in model.go) ----

func TestLayoutHonorsBoxModes(t *testing.T) {
	base := Model{w: 120, h: 40}

	full := base
	full.layoutPrefs = layoutPrefs{Damage: panelFull, OffDef: panelFull}
	if ld := full.layout(); ld.dmgW <= 0 || ld.extrasW <= 0 || ld.dmgH <= 0 {
		t.Errorf("both full: want a meter + side column + top height, got %+v", ld)
	}

	noDmg := base
	noDmg.layoutPrefs = layoutPrefs{Damage: panelOff, OffDef: panelFull}
	if ld := noDmg.layout(); ld.dmgW != 0 || ld.extrasW != ld.rightW {
		t.Errorf("damage off: want dmgW=0 and Offense·Defense full width, got %+v", ld)
	}

	noOD := base
	noOD.layoutPrefs = layoutPrefs{Damage: panelFull, OffDef: panelOff}
	if ld := noOD.layout(); ld.extrasW != 0 || ld.dmgW != ld.rightW {
		t.Errorf("offdef off: want extrasW=0 and meter full width, got %+v", ld)
	}

	both := base
	both.layoutPrefs = layoutPrefs{Damage: panelOff, OffDef: panelOff}
	if ld := both.layout(); ld.dmgH != 0 || ld.botH != ld.areaH {
		t.Errorf("both off: top row should collapse (dmgH=0, botH=areaH), got %+v", ld)
	}

	// Compact shrinks the top row so the bottom panels grow.
	compact := base
	compact.layoutPrefs = layoutPrefs{Damage: panelCompact, OffDef: panelCompact}
	fl, cm := full.layout(), compact.layout()
	if cm.dmgH >= fl.dmgH {
		t.Errorf("compact top should be shorter than full (%d vs %d)", cm.dmgH, fl.dmgH)
	}
	if cm.botH <= fl.botH {
		t.Errorf("compact should give the bottom row more height (%d vs %d)", cm.botH, fl.botH)
	}
}

func TestDamageCompactDropsColumns(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.refresh()
	cur := mm.sessions[mm.effectiveSel()]

	mm.layoutPrefs.Damage = panelFull
	if !strings.Contains(mm.damageContent(cur, true, 80), "Crit") {
		t.Error("full meter at width 80 should show the Crit column")
	}
	mm.layoutPrefs.Damage = panelCompact
	if strings.Contains(mm.damageContent(cur, true, 80), "Crit") {
		t.Error("compact meter should drop the Crit column even at width 80")
	}
}

func TestLayoutClassBox(t *testing.T) {
	m := Model{w: 120, h: 40}
	if ld := m.layout(); ld.classBox || ld.mobH != ld.botH {
		t.Errorf("class box off: mob should fill the column (classBox=%v mobH=%d botH=%d)", ld.classBox, ld.mobH, ld.botH)
	}

	m.layoutPrefs.ClassBox = true
	ld := m.layout()
	if !ld.classBox || ld.boxH <= 0 {
		t.Fatalf("class box on: want a box, got %+v", ld)
	}
	if ld.mobH+ld.boxH+1 != ld.botH {
		t.Errorf("box should split the column with a gap (mobH=%d boxH=%d botH=%d)", ld.mobH, ld.boxH, ld.botH)
	}
	if ld.mobH <= ld.boxH {
		t.Errorf("Mob Tracker should be the taller part (mobH=%d boxH=%d)", ld.mobH, ld.boxH)
	}
}
