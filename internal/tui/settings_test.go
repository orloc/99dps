package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/eqclass"
	"99dps/internal/session"
	"99dps/internal/tts"
)

// settingsEngine is a fakeEngine-style speaker exposing a voice catalog and
// recording SetVoice / Say so the voice-select + preview paths can be asserted.
type settingsEngine struct {
	voices   []tts.Voice
	cur      string
	said     []string
	setCalls []string
}

func (e *settingsEngine) Say(s string)       { e.said = append(e.said, s) }
func (e *settingsEngine) SayUrgent(s string) { e.said = append(e.said, s) }
func (e *settingsEngine) Available() bool    { return true }
func (e *settingsEngine) Voices() []tts.Voice {
	return e.voices
}
func (e *settingsEngine) Voice() string { return e.cur }
func (e *settingsEngine) SetVoice(id string) bool {
	e.cur, e.setCalls = id, append(e.setCalls, id)
	return true
}
func (e *settingsEngine) Flush() {}

// TestVoiceIndex: in-range returns the voice, out-of-range (both ends) misses.
func TestVoiceIndex(t *testing.T) {
	vs := []tts.Voice{{ID: "a"}, {ID: "b"}}
	if v, ok := voiceIndex(vs, 1); !ok || v.ID != "b" {
		t.Errorf("in-range = (%v,%v), want b/true", v, ok)
	}
	if _, ok := voiceIndex(vs, -1); ok {
		t.Error("negative index should miss")
	}
	if _, ok := voiceIndex(vs, 2); ok {
		t.Error("past-end index should miss")
	}
	if _, ok := voiceIndex(nil, 0); ok {
		t.Error("nil catalog should miss")
	}
}

// TestSettingsColWidthsNarrowClamp: at a tiny inner width the left column floors
// at 24 and the right column floors at 1.
func TestSettingsColWidthsNarrowClamp(t *testing.T) {
	// innerW 27: the even split (12) is below the 24 floor → 24; the right column
	// (27-3-24 = 0) is below 1 → floors at 1.
	leftW, rightW := settingsColWidths(27)
	if leftW != 24 {
		t.Errorf("narrow leftW should floor at 24, got %d", leftW)
	}
	if rightW != 1 {
		t.Errorf("narrow rightW should floor at 1, got %d", rightW)
	}
	// a roomy width splits ~evenly with neither floor engaged
	l, r := settingsColWidths(100)
	if l < 24 || r < 1 || l+settingsGapW+r != 100 {
		t.Errorf("roomy split should sum to inner width, got l=%d r=%d", l, r)
	}
}

// TestToggleCueOutOfRange: an out-of-range index is a no-op (no panic, no flash).
func TestToggleCueOutOfRange(t *testing.T) {
	m := &Model{cues: cuePrefs{Overrides: map[string]bool{}}}
	m.toggleCue(-1)
	m.toggleCue(9999)
	if len(m.cues.Overrides) != 0 || m.status != "" {
		t.Errorf("out-of-range toggle should not change anything (overrides=%v status=%q)", m.cues.Overrides, m.status)
	}
}

// TestSettingsNavClampsBothColumns: up/down clamp within each column, and
// left/right (and h/l) switch the focused column.
func TestSettingsNavClampsBothColumns(t *testing.T) {
	m := Model{screen: screenSettings}
	send := func(s string) {
		var msg tea.KeyMsg
		switch s {
		case "left", "right", "up", "down":
			msg = tea.KeyMsg{Type: map[string]tea.KeyType{
				"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
			}[s]}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		tm, _ := m.updateSettings(msg)
		m = tm.(Model)
	}

	// left column: up at the top clamps to 0
	send("up")
	if m.settingsSel != 0 {
		t.Errorf("up at top should clamp left sel to 0, got %d", m.settingsSel)
	}
	send("down")
	if m.settingsSel != settingsDamageRow {
		t.Errorf("down should advance left sel to %d, got %d", settingsDamageRow, m.settingsSel)
	}
	// 'h' / 'l' switch columns just like left/right
	send("l")
	if m.settingsCol != 1 {
		t.Errorf("'l' should focus the cue column, got %d", m.settingsCol)
	}
	send("h")
	if m.settingsCol != 0 {
		t.Errorf("'h' should focus the left column, got %d", m.settingsCol)
	}

	// right column: drive past the end, it clamps at the last cue toggle
	send("right")
	last := len(cueToggles(cueRows(eqclass.ClassUnknown))) - 1
	for i := 0; i < last+5; i++ {
		send("down")
	}
	if m.cueSel != last {
		t.Errorf("down past end should clamp cueSel to %d, got %d", last, m.cueSel)
	}
	for i := 0; i < last+5; i++ {
		send("up")
	}
	if m.cueSel != 0 {
		t.Errorf("up past top should clamp cueSel to 0, got %d", m.cueSel)
	}
}

// TestApplyLeftSettingCycles: enter on the Damage and Offense·Defense rows cycles
// the box mode and flashes a saved confirmation.
func TestApplyLeftSettingCycles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := &Model{}
	before := m.layoutPrefs.Damage
	m.applyLeftSetting(settingsDamageRow)
	if m.layoutPrefs.Damage == before {
		t.Errorf("Damage row should cycle the box mode (was %v)", before)
	}
	if m.status == "" {
		t.Error("cycling Damage should flash a saved confirmation")
	}

	odBefore := m.layoutPrefs.OffDef
	m.applyLeftSetting(settingsOffDefRow)
	if m.layoutPrefs.OffDef == odBefore {
		t.Errorf("OffDef row should cycle the box mode (was %v)", odBefore)
	}
}

// TestApplyLeftSettingVoiceSelect: enter on a voice row selects it via the engine
// and flashes the voice name.
func TestApplyLeftSettingVoiceSelect(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	eng := &settingsEngine{voices: []tts.Voice{{ID: "v0", Name: "Alpha"}, {ID: "v1", Name: "Beta"}}}
	m := &Model{speaker: eng}
	m.applyLeftSetting(settingsFixedRows + 1) // the second voice
	if eng.cur != "v1" {
		t.Errorf("voice row should SetVoice v1, got %q", eng.cur)
	}
	if m.status == "" {
		t.Error("voice select should flash the saved voice")
	}
}

// TestUpdateSettingsEnterTogglesCue: enter while the cue column is focused toggles
// the selected cue (default-on → off).
func TestUpdateSettingsEnterTogglesCue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSettings, settingsCol: 1, cueSel: 0}
	tm, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	first := cueToggles(cueRows(eqclass.ClassUnknown))[0]
	if m.cues.enabled(first.id, first.def) {
		t.Errorf("enter on the cue column should toggle %s off", first.id)
	}
}

// TestUpdateSettingsPreviewVoice: 'p' on a voice row previews it (SetVoice + Say).
func TestUpdateSettingsPreviewVoice(t *testing.T) {
	eng := &settingsEngine{voices: []tts.Voice{{ID: "v0", Name: "Alpha"}}}
	m := Model{screen: screenSettings, speaker: eng, settingsCol: 0, settingsSel: settingsFixedRows}
	tm, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	_ = tm
	if eng.cur != "v0" || len(eng.said) == 0 {
		t.Errorf("'p' should preview the voice (SetVoice + Say), got cur=%q said=%v", eng.cur, eng.said)
	}
}

// TestSettingsClickAt resolves clicks to (col, sel): a left selectable row, a
// right cue toggle, a header/blank (miss), and a click above the body (miss).
func TestSettingsClickAt(t *testing.T) {
	var m tea.Model = NewModel(sampleManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.screen = screenSettings

	// y above the body returns ok=false.
	if _, _, ok := mm.settingsClickAt(gridX, gridTop-1); ok {
		t.Error("a click above the settings body should miss")
	}

	th := themes[mm.theme]
	leftW, rightW := settingsColWidths(mm.w - 2)

	// find the audio row line in the left column and click it.
	_, lsel := mm.leftSettingsColumn(th, leftW)
	leftLine := -1
	for i, s := range lsel {
		if s == settingsAudioRow {
			leftLine = i
			break
		}
	}
	if leftLine < 0 {
		t.Fatal("no audio row in the left column")
	}
	if col, sel, ok := mm.settingsClickAt(gridX, gridTop+leftLine); !ok || col != 0 || sel != settingsAudioRow {
		t.Errorf("left click = (%d,%d,%v), want col 0 / audio row", col, sel, ok)
	}

	// a header/blank line in the left column (sel == -1) misses.
	headerLine := -1
	for i, s := range lsel {
		if s == -1 {
			headerLine = i
			break
		}
	}
	if headerLine >= 0 {
		if _, _, ok := mm.settingsClickAt(gridX, gridTop+headerLine); ok {
			t.Error("a click on a left-column header/blank should miss")
		}
	}

	// find a cue-toggle line in the right column and click it.
	_, rsel := mm.rightSettingsColumn(th, rightW)
	rightLine, wantSel := -1, -1
	for i, s := range rsel {
		if s >= 0 {
			rightLine, wantSel = i, s
			break
		}
	}
	if rightLine < 0 {
		t.Fatal("no cue toggle in the right column")
	}
	rx := gridX + leftW + settingsGapW
	if col, sel, ok := mm.settingsClickAt(rx, gridTop+rightLine); !ok || col != 1 || sel != wantSel {
		t.Errorf("right click = (%d,%d,%v), want col 1 / sel %d", col, sel, ok, wantSel)
	}
}

// TestSettingsMouseTogglesCue drives the full Update mouse path: a left-click on a
// cue row toggles it.
func TestSettingsMouseTogglesCue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var m tea.Model = NewModel(sampleManager(), nil, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.screen = screenSettings

	th := themes[mm.theme]
	leftW, rightW := settingsColWidths(mm.w - 2)
	_, rsel := mm.rightSettingsColumn(th, rightW)
	line, wantSel := -1, -1
	for i, s := range rsel {
		if s >= 0 {
			line, wantSel = i, s
			break
		}
	}
	if line < 0 {
		t.Fatal("no cue toggle row")
	}
	rx := gridX + leftW + settingsGapW
	m2, _ := mm.Update(tea.MouseMsg{X: rx, Y: gridTop + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	mm2 := m2.(Model)
	row := cueToggles(cueRows(eqclass.ClassUnknown))[wantSel]
	if mm2.cues.enabled(row.id, row.def) {
		t.Errorf("a left-click on cue row %d should toggle %s off", wantSel, row.id)
	}
}

// TestGotoScreenSizesDestination: going to the Sessions tab on a ready model
// populates its table (resize + refresh ran).
func TestGotoScreenSizesDestination(t *testing.T) {
	var m tea.Model = NewModel(twoSessionManager(), nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	dst := mm.gotoScreen(screenSessions)
	if dst.screen != screenSessions {
		t.Fatalf("screen = %v, want Sessions", dst.screen)
	}
	if len(dst.sessRows) == 0 {
		t.Error("gotoScreen(Sessions) should refresh the session table")
	}
	// not-ready: gotoScreen just sets the screen, no refresh.
	notReady := Model{}.gotoScreen(screenMeter)
	if notReady.screen != screenMeter {
		t.Errorf("gotoScreen on a not-ready model should still set the screen")
	}
}

// TestSettingsVoicesNilSpeaker: with no engine the voice catalog is nil and the
// left column shows the "download a voice" hint.
func TestSettingsVoicesNilSpeaker(t *testing.T) {
	m := Model{}
	if m.settingsVoices() != nil {
		t.Error("a nil speaker should yield a nil voice catalog")
	}
	out, _ := m.leftSettingsColumn(themes[0], 40)
	joined := ""
	for _, l := range out {
		joined += l
	}
	if !strings.Contains(joined, "download a voice") {
		t.Errorf("no-voices left column should show the download hint, got:\n%s", joined)
	}
}

// ---- moved from tabs_test.go (tab bar logic in settings.go) ----

func TestTabAtMatchesBar(t *testing.T) {
	m := Model{screen: screenMeter, w: 122} // innerW = 120
	for _, h := range tabHits(m.w - 2) {
		mid := (h.x0 + h.x1) / 2
		if scr, ok := m.tabAt(mid, tabRow); !ok || scr != h.scr {
			t.Errorf("tabAt(%d, %d) = (%v,%v), want %v", mid, tabRow, scr, ok, h.scr)
		}
	}
	// off the tab row → no hit
	if _, ok := m.tabAt(tabHits(m.w - 2)[0].x0, tabRow+1); ok {
		t.Error("tabAt off the tab row should miss")
	}
	// in the gap between the left tabs and right-aligned Settings → no hit
	if _, ok := m.tabAt(9999, tabRow); ok {
		t.Error("tabAt past the right edge should miss")
	}
}

func TestCycleTab(t *testing.T) {
	m := Model{screen: screenMeter}
	if m.cycleTab(+1) != screenSessions {
		t.Error("next from Meter should be Sessions")
	}
	m.screen = screenSettings // the last tab in the slice
	if m.cycleTab(+1) != screenMeter {
		t.Error("next from the last tab should wrap to Meter")
	}
	if m.cycleTab(-1) != screenMap {
		t.Error("prev from Settings should be Map")
	}
	m.screen = screenMeter
	if m.cycleTab(-1) != screenSettings {
		t.Error("prev from Meter should wrap to the last tab (Settings)")
	}
}

func TestTabKeyNavigation(t *testing.T) {
	var m tea.Model = NewModel(&session.SessionManager{}, nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.(Model).screen != screenSessions {
		t.Error("key 2 should open Sessions")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.(Model).screen != screenTools {
		t.Error("key 3 should open Tools")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.(Model).screen != screenMap {
		t.Error("key 4 should open Map")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	if m.(Model).screen != screenSettings {
		t.Error("key 5 should open Settings")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // wraps the last tab (Settings) -> Meter
	if m.(Model).screen != screenMeter {
		t.Error("tab from the last tab should wrap to Meter")
	}
}

func TestTabClickSwitchesScreen(t *testing.T) {
	var m tea.Model = NewModel(&session.SessionManager{}, nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	sessionsTab := tabHits(100 - 2)[1] // Meter | Sessions | Tools … Settings
	mid := (sessionsTab.x0 + sessionsTab.x1) / 2
	m, _ = m.Update(tea.MouseMsg{X: mid, Y: tabRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.(Model).screen != screenSessions {
		t.Errorf("clicking the Sessions tab should switch screens, got %v", m.(Model).screen)
	}
}

func TestSettingsViewFitsWindow(t *testing.T) {
	for _, w := range []int{60, 80, 120} {
		m := Model{screen: screenSettings, ready: true, w: w, h: 30, speaker: tts.NewEngine()}
		for _, ln := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(ln) > w {
				t.Errorf("w=%d: settings line exceeds width (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}

// ---- moved from cuegating_test.go (settings-driven cue toggles) ----

// TestSettingsRightColumnToggle drives the keyboard path: focus the cue column,
// toggle the first cue, and confirm it persisted as an override.
func TestSettingsRightColumnToggle(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSettings}

	send := func(k tea.KeyMsg) {
		tm, _ := m.updateSettings(k)
		m = tm.(Model)
	}
	send(tea.KeyMsg{Type: tea.KeyRight}) // focus the cue column
	if m.settingsCol != 1 {
		t.Fatalf("→ should focus the cue column, got col %d", m.settingsCol)
	}
	send(tea.KeyMsg{Type: tea.KeyEnter}) // toggle the first cue (Mend ready, default on → off)

	first := cueToggles(cueRows(eqclass.ClassUnknown))[0]
	if m.cues.enabled(first.id, first.def) {
		t.Errorf("first cue (%s) should now be off", first.id)
	}
	saved := cuePrefs{Overrides: loadStore().forChar(m.character).Cues}
	if saved.enabled(first.id, first.def) {
		t.Errorf("the cue toggle should persist to the settings store")
	}
}

// TestSettingsFlashConfirmsSave: toggling a cue surfaces a saved-confirmation
// flash on the Settings page, so the user can see the (silent) toggle stuck.
func TestSettingsFlashConfirmsSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	sm, tr := monkScene()
	var m tea.Model = NewModel(sm, tr, "Kelkix")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	mm := m.(Model)
	mm.screen = screenSettings

	r, _ := mm.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	mm = r.(Model)
	r, _ = mm.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	mm = r.(Model)

	if !strings.Contains(mm.View(), "saved") {
		t.Error("toggling a cue should show a saved-confirmation flash on the settings page")
	}
}

// ---- moved from layout_test.go (Settings rows in settings.go) ----

func TestSettingsTogglesClassBox(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSettings, settingsSel: settingsClassBoxRow}
	if m.layoutPrefs.ClassBox {
		t.Fatal("class box should default off")
	}
	tm, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if !m.layoutPrefs.ClassBox {
		t.Error("enter should turn the class box on")
	}
	if !loadStore().forChar(m.character).ClassBox {
		t.Error("the class box choice should persist to the store")
	}
}

func TestSettingsCyclesMeterMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSettings, settingsSel: settingsDamageRow}
	for _, want := range []panelMode{panelCompact, panelOff, panelFull} {
		tm, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
		m = tm.(Model)
		if m.layoutPrefs.Damage != want {
			t.Errorf("Damage mode = %v, want %v", m.layoutPrefs.Damage, want)
		}
	}
	if loadStore().forChar(m.character).Damage != panelFull {
		t.Error("the meter mode should persist to the settings store")
	}
}
