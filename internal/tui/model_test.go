package tui

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"99dps/internal/combat"
	"99dps/internal/eqclass"
	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// sampleManager builds a session manager with a few dealers, spread over a span.
func sampleManager() *session.SessionManager {
	sm := &session.SessionManager{}
	rows := []struct {
		dealer string
		dmg    int
		at     int64
	}{
		{"You", 520_000, 1000}, {"Gabnador", 349_000, 1008}, {"Borric", 190_000, 1016},
		{"Mourngul", 168_000, 1024}, {"Faelyn", 133_000, 1032}, {"a pet", 77_000, 1042},
	}
	for _, r := range rows {
		sm.Apply(&combat.DamageSet{ActionTime: r.at, Dealer: r.dealer, Dmg: r.dmg, Target: "a sand giant"})
	}
	// specials so the Specials breakdown has content (per dealer, per kind, with a
	// hit rate) — also exercised by the overflow guard.
	sm.Apply(&combat.DamageSet{ActionTime: 1040, Dealer: "Borric", Dmg: 5_000, Target: "a sand giant", Verb: "backstab"})
	sm.Apply(&combat.DamageSet{ActionTime: 1041, Dealer: "You", Dmg: 1_400, Target: "a sand giant", Verb: "kick"})
	sm.Apply(&combat.DamageSet{ActionTime: 1042, Dealer: "You", Dmg: 1_200, Target: "a sand giant", Verb: "kick"})
	sm.ApplySwing(&combat.Swing{ActionTime: 1043, Attacker: "You", Defender: "a sand giant", Verb: "kick", Outcome: combat.OutcomeMiss})
	return sm
}

// renderAt drives the model through a window-size + tick so View() has content.
func renderAt(sm *session.SessionManager, themeIdx, w, h int) string {
	return renderAtTr(sm, nil, themeIdx, w, h)
}

func renderAtTr(sm *session.SessionManager, tr *gamestate.Tracker, themeIdx, w, h int) string {
	var m tea.Model = New(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	mm := m.(Model)
	mm.theme = themeIdx
	mm.refresh()
	return mm.View()
}

// monkScene is a melee (Monk) scenario: skill attacks + a Mend cooldown, so the
// class-aware panel shows SKILLS + COOLDOWNS instead of spell timers.
func monkScene() (*session.SessionManager, *gamestate.Tracker) {
	sm := sampleManager()
	sm.Apply(&combat.DamageSet{ActionTime: 1043, Dealer: "You", Dmg: 90, Target: "a sand giant", Verb: "kick"})
	sm.Apply(&combat.DamageSet{ActionTime: 1044, Dealer: "You", Dmg: 70, Target: "a sand giant", Verb: "strike"})
	book, _ := gamestate.LoadReader(strings.NewReader(""))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassMonk)
	tr.Observe("You mend your wounds and heal some damage.", 1044) // starts the Mend cooldown
	return sm, tr
}

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
	body, targets := timersBody(themes[0], tr, 40, true, "")
	for _, want := range []string{"Aragorn", "Legolas", "Aegolism", "Haste"} {
		if !strings.Contains(body, want) {
			t.Errorf("grouped timers missing %q", want)
		}
	}
	if !containsValue(targets, "Aragorn") || !containsValue(targets, "Legolas") {
		t.Errorf("line→target map missing a target: %v", targets)
	}
	// hovering Aragorn surfaces the ✕ affordance; the others stay plain.
	if hovered, _ := timersBody(themes[0], tr, 40, true, "Aragorn"); !strings.Contains(hovered, "✕") {
		t.Error("hovered target should render an ✕ dismiss affordance")
	}
}

// TestHoverDismiss drives the model: a mouse-move over a target's row sets the
// hover highlight, and a left-click there dismisses that target's timers.
func TestHoverDismiss(t *testing.T) {
	sm, tr := casterScene()
	var m tea.Model = New(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.refresh()

	// screen cell over Aragorn's group in the class (bottom-left) panel
	ld := mm.layout()
	contentTop := 2 + ld.dmgH + 2 // outer pad + banner + dmg card + border + title
	rightX := ld.leftW + 2
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
// rendered line may exceed the panel width at any size (gocui clipped silently;
// lipgloss would push past the card border).
func TestDamageNoOverflow(t *testing.T) {
	var m tea.Model = New(sampleManager(), nil, "Kelkix")
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
		var m tea.Model = New(smv, trv, "Kelkix")
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
	}

	// a degenerate early size must not flip the model into rendering (stay "starting…")
	var m tea.Model = New(sampleManager(), nil, "Kelkix")
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
	var m tea.Model = New(sampleManager(), nil, "Kelkix")
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
func twoSessionManager() *session.SessionManager {
	sm := &session.SessionManager{}
	sm.Apply(&combat.DamageSet{ActionTime: 1000, Dealer: "You", Dmg: 1000, Target: "a rat"})
	sm.Apply(&combat.DamageSet{ActionTime: 9000, Dealer: "You", Dmg: 2000, Target: "a bat"})
	return sm
}

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
	var m tea.Model = New(sampleManager(), nil, "Kelkix")
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

func TestSessionClickSelects(t *testing.T) {
	var m tea.Model = New(twoSessionManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	if len(mm.sessions) < 2 {
		t.Fatalf("want >=2 sessions, got %d", len(mm.sessions))
	}
	// row 0 of the Sessions panel: content starts at gridY(2)+border+title = 4
	m2, _ := mm.Update(tea.MouseMsg{X: 3, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if mm2 := m2.(Model); mm2.selected != 0 || mm2.follow {
		t.Errorf("clicking the first fight should pin it (selected=%d follow=%v)", mm2.selected, mm2.follow)
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
	var m tea.Model = New(sampleManager(), mobSceneTracker(), "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)

	ld := mm.layout()
	contentTop := 2 + ld.dmgH + 2
	line, mob := -1, ""
	for l, mb := range mm.mobTargets {
		if line < 0 || l < line {
			line, mob = l, mb
		}
	}
	if line < 0 {
		t.Fatal("no mob targets to click")
	}
	mobX := ld.leftW + 2 + ld.classW + 1
	if ld.ench {
		mobX += ld.ccW + 1
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
	m := New(&session.SessionManager{}, nil, "Kelkix")
	now := int64(1000)
	healthy := gamestate.Timer{Spell: "Clarity", Target: "Tankguy", Expiry: now + 600}
	low := gamestate.Timer{Spell: "Clarity", Target: "Healer", Expiry: now + 8}
	charm := gamestate.Timer{Spell: "Charm", Target: "Charm", Expiry: now + 5, Charm: true}

	got := m.dueAnnouncements([]gamestate.Timer{healthy, low, charm}, now)
	if want := []string{"Healer, Clarity low"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first pass = %v, want %v", got, want)
	}
	if got := m.dueAnnouncements([]gamestate.Timer{healthy, low, charm}, now); got != nil {
		t.Errorf("repeat pass spoke again: %v", got)
	}
	// refreshed (healthy) re-arms; dropping low again speaks
	m.dueAnnouncements([]gamestate.Timer{{Spell: "Clarity", Target: "Healer", Expiry: now + 600}}, now)
	if got := m.dueAnnouncements([]gamestate.Timer{{Spell: "Clarity", Target: "Healer", Expiry: now + 5}}, now); len(got) != 1 {
		t.Errorf("refresh should re-arm; got %v", got)
	}
	m2 := New(&session.SessionManager{}, nil, "X")
	self := gamestate.Timer{Spell: "Bedlam", Target: "You", Expiry: now + 5}
	if got := m2.dueAnnouncements([]gamestate.Timer{self}, now); len(got) != 1 || got[0] != "Bedlam low" {
		t.Errorf("self phrase = %v, want [\"Bedlam low\"]", got)
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

	var m tea.Model = New(sm, tr, "Kelkix")
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

// TestSpellsAttributedToYou: non-melee damage is credited to You (a "↳ spells"
// child), not shown as an unattributed lump, when a You row is present.
func TestSpellsAttributedToYou(t *testing.T) {
	sm := sampleManager()
	sm.ApplyMagic(&combat.Magic{ActionTime: 1042, Dmg: 240_000, Target: "a sand giant"})
	var m tea.Model = New(sm, nil, "Kelkix")
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

	book, _ := gamestate.LoadReader(strings.NewReader(""))
	ench := gamestate.NewTracker(book)
	ench.SetClass(eqclass.ClassEnchanter)
	if out := renderAtTr(sampleManager(), ench, 0, 110, 34); !strings.Contains(out, "Crowd Control") {
		t.Errorf("enchanter layout should include a Crowd Control column")
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
	var m tea.Model = New(smC, trC, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 108, Height: 34})
	mc := m.(Model)
	mc.refresh()
	ld := mc.layout()
	for l, tgt := range mc.classTargets {
		if tgt == "Aragorn" {
			mc2, _ := mc.Update(tea.MouseMsg{X: ld.leftW + 5, Y: 2 + ld.dmgH + 2 + l, Action: tea.MouseActionMotion})
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
	var dm tea.Model = New(sampleManager(), nil, "Kelkix")
	dm, _ = dm.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	dmm := dm.(Model)
	if err := os.WriteFile("/tmp/tui-damage.txt", []byte(dmm.damageContent(dmm.sessions[dmm.effectiveSel()], true, 80)), 0o644); err != nil {
		t.Fatal(err)
	}
}
