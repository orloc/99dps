package tui

import (
	"strings"
	"testing"
	"time"

	"99dps/internal/eq/eqclass"
	"99dps/internal/eq/gamestate"
	"99dps/internal/session"
)

// fakeSpellTracker builds an Enchanter tracker holding one active timer for a spell
// name guaranteed absent from the bundled P99 set (so it's always "suspect"),
// independent of the committed dataset's contents.
func fakeSpellTracker(t *testing.T) *gamestate.Tracker {
	t.Helper()
	const fake = "Xyzzy Bogus Incantation"
	book, _ := gamestate.LoadReader(strings.NewReader(mezRow(fake, " is bamboozled.")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassEnchanter)
	now := time.Now().Unix()
	tr.BeginCast(fake, now-5)
	tr.Observe("a sand giant is bamboozled.", now)
	return tr
}

// TestSuspect_ConfirmedRealSpell: a real P99 spell cast by a class that can cast it
// (Enchanter / Mesmerize, from the bundled dataset) is NOT flagged.
func TestSuspect_ConfirmedRealSpell(t *testing.T) {
	m := NewModel(&session.SessionManager{}, ccTracker(t), "Tester") // Enchanter casting Mesmerize
	if got := m.suspectActiveSpells(); len(got) != 0 {
		t.Errorf("a real Enchanter spell must be confirmed, flagged %v", got)
	}
	if w := m.spellWarning(60); w != "" {
		t.Errorf("no warning expected for a confirmed spell, got %q", w)
	}
}

// TestSuspect_FlagsUnknownSpell: a spell absent from the bundled set is flagged,
// and the warning renders as a header plus one bullet per spell (clipped to width).
func TestSuspect_FlagsUnknownSpell(t *testing.T) {
	m := NewModel(&session.SessionManager{}, fakeSpellTracker(t), "Tester")
	sus := m.suspectActiveSpells()
	if len(sus) != 1 || !strings.Contains(sus[0], "Xyzzy") {
		t.Fatalf("an unknown spell must be flagged, got %v", sus)
	}
	w := m.spellWarning(60)
	if !strings.Contains(w, "Xyzzy") || !strings.Contains(w, "unconfirmed") {
		t.Errorf("warning = %q, want it to name the spell + 'unconfirmed'", w)
	}
	// header + one bullet per suspect + a trailing dataset-provenance line
	lines := strings.Split(w, "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "•") || !strings.Contains(lines[2], "P99 set:") {
		t.Errorf("warning should be a header + bullet list + provenance, got %q", w)
	}
	if m.spellWarning(4) != "" {
		t.Error("warning should be suppressed at tiny widths")
	}
}

// TestSuspect_SkipsWhenClassUnknown: with no detected class, castability can't be
// judged, so nothing is flagged (don't flag blind).
func TestSuspect_SkipsWhenClassUnknown(t *testing.T) {
	book, _ := gamestate.LoadReader(strings.NewReader(mezRow("Xyzzy Bogus Incantation", " is bamboozled.")))
	tr := gamestate.NewTracker(book)
	tr.SetLevel(60) // class deliberately left unknown
	now := time.Now().Unix()
	tr.BeginCast("Xyzzy Bogus Incantation", now-5)
	tr.Observe("a sand giant is bamboozled.", now)
	m := NewModel(&session.SessionManager{}, tr, "Tester")
	if got := m.suspectActiveSpells(); got != nil {
		t.Errorf("an unknown class must not flag anything, got %v", got)
	}
}

// TestSuspect_SkipsIncomingDebuffs: incoming "ON YOU" debuffs are category
// estimates (Estimated), not spells we cast — they must never be flagged.
func TestSuspect_SkipsIncomingDebuffs(t *testing.T) {
	tr := onYouTracker(t) // Estimated "Slowed" on You
	tr.SetClass(eqclass.ClassEnchanter)
	m := NewModel(&session.SessionManager{}, tr, "Tester")
	if got := m.suspectActiveSpells(); len(got) != 0 {
		t.Errorf("incoming (Estimated) debuffs must not be flagged, got %v", got)
	}
}

// TestSuspect_NilTracker: no tracker → no flags, no panic.
func TestSuspect_NilTracker(t *testing.T) {
	m := NewModel(&session.SessionManager{}, nil, "Tester")
	if got := m.suspectActiveSpells(); got != nil {
		t.Errorf("a nil tracker must yield no suspects, got %v", got)
	}
}
