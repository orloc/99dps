package tui

import (
	"os"
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
	for _, w := range []int{40, 56, 64, 90, 120} {
		for i, line := range strings.Split(mm.damageContent(cur, true, w), "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("w=%d line %d overflows (%d): %q", w, i, lw, line)
			}
		}
	}
}

func TestPanelsRenderLiveData(t *testing.T) {
	out := renderAt(sampleManager(), 0, 100, 32)
	for _, want := range []string{
		"99dps",           // banner
		"a sand giant",    // sessions list + damage title
		"You", "Gabnador", // damage rows
		"Now", "Sessions", "Spell Timers", "Mob Tracker", // panel titles
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
