package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestLogTailRing: appends keep arrival order, evict the oldest past capacity,
// and the Tap closure records raw lines; a nil ring is safe.
func TestLogTailRing(t *testing.T) {
	lt := NewLogTail()
	tap := lt.Tap()
	for i := 0; i < logTailCap+10; i++ {
		tap("line", int64(i)) // raw lines via the parser-shaped tap
	}
	lt.AddCue("a gnoll spawns soon", 999, false)

	entries, seq := lt.Snapshot(logTailCap)
	if len(entries) > logTailCap {
		t.Fatalf("ring kept %d entries, want ≤ %d", len(entries), logTailCap)
	}
	if seq != uint64(logTailCap+11) {
		t.Errorf("seq = %d, want %d (total ever appended)", seq, logTailCap+11)
	}
	// newest is the cue we just added; the oldest raw lines were evicted.
	last := entries[len(entries)-1]
	if last.Kind != logCue || last.Text != "a gnoll spawns soon" {
		t.Errorf("newest entry = %+v, want the cue", last)
	}
	if entries[0].At >= entries[len(entries)-2].At {
		t.Error("entries should be in arrival (ascending) order")
	}

	// a snapshot window returns only the last n.
	if win, _ := lt.Snapshot(5); len(win) != 5 {
		t.Errorf("Snapshot(5) = %d entries, want 5", len(win))
	}

	// nil-safety (a zero-value Model before NewModel runs).
	var nilLT *LogTail
	if nilLT.Tap() != nil {
		t.Error("nil ring Tap() should be nil")
	}
	if got, _ := nilLT.Snapshot(10); got != nil {
		t.Error("nil ring Snapshot should be nil")
	}
}

// TestLogSheetColumns: the merged sheet shows raw lines and fired cues, fits the
// width (never wraps), and degrades to a placeholder when empty.
func TestLogSheetColumns(t *testing.T) {
	th := themes[0]
	if got := logSheet(th, nil, 60); !strings.Contains(got, "waiting") {
		t.Errorf("empty sheet should show a placeholder, got %q", got)
	}

	entries := []LogEntry{
		{At: 1_700_000_000, Kind: logRaw, Text: "a gnoll hits YOU for 12 points of damage."},
		{At: 1_700_000_001, Kind: logCue, Text: "a gnoll spawns soon"},
		{At: 1_700_000_002, Kind: logCue, Text: "Charm broke!", Urgent: true},
	}
	const w = 64
	sheet := logSheet(th, entries, w)
	if !strings.Contains(sheet, "a gnoll hits YOU") {
		t.Error("raw log line should appear")
	}
	if !strings.Contains(sheet, "spawns soon") || !strings.Contains(sheet, "Charm broke") {
		t.Error("fired cues should appear in the sheet")
	}
	for _, ln := range strings.Split(sheet, "\n") {
		if lipgloss.Width(ln) > w {
			t.Errorf("a row overflowed width %d: %d cells", w, lipgloss.Width(ln))
		}
	}
}

// TestLogSheetMark: a character-switch mark renders as a divider that shows the
// message and rule dashes, without overflowing the width.
func TestLogSheetMark(t *testing.T) {
	th := themes[0]
	const w = 64
	lt := NewLogTail()
	lt.AddMark("▶ now tracking Sensive", 1_700_000_000)
	entries, _ := lt.Snapshot(logTailCap)
	if len(entries) != 1 || entries[0].Kind != logMark {
		t.Fatalf("AddMark should record one logMark entry, got %+v", entries)
	}
	sheet := logSheet(th, entries, w)
	if !strings.Contains(sheet, "now tracking Sensive") {
		t.Errorf("divider should show the switch message, got %q", sheet)
	}
	if !strings.Contains(sheet, "─") {
		t.Error("divider should render rule dashes")
	}
	for _, ln := range strings.Split(sheet, "\n") {
		if lipgloss.Width(ln) > w {
			t.Errorf("divider overflowed width %d: %d", w, lipgloss.Width(ln))
		}
	}
}
