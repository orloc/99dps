package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/gamestate"
	"99dps/internal/session"
)

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
	if got := composeCue(nil, 0); got != "" {
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
