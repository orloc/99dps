package tui

import (
	"testing"

	"99dps/internal/gamestate"
)

func TestWarnLeadSec(t *testing.T) {
	cases := []struct {
		total, want int64
	}{
		{60, 15},    // 1m buff: 10% = 6s → floored to 15s
		{300, 30},   // 5m: 30s
		{900, 90},   // 15m: 90s
		{6006, 180}, // ~100m (Gift of Brilliance): capped at 180s, not 15s
		{5946, 180}, // ~99m (Umbra): capped at 180s
		{10, 15},    // degenerate short: floor
	}
	for _, c := range cases {
		if got := warnLeadSec(c.total); got != c.want {
			t.Errorf("warnLeadSec(%d) = %d, want %d", c.total, got, c.want)
		}
	}
}

func TestDueAnnouncementsScalesByDuration(t *testing.T) {
	m := &Model{announced: map[string]bool{}}
	now := int64(1_000_000)

	// a 100-min buff with 2 minutes (120s) left: that's inside the 180s lead → fires
	longBuff := gamestate.Timer{Spell: "Gift of Brilliance", Target: "You", Start: now - 5886, Expiry: now + 120}
	if got := m.dueAnnouncements([]gamestate.Timer{longBuff}, now); len(got) != 1 {
		t.Fatalf("long buff at 120s left should warn (180s lead), got %v", got)
	}
	// fires once only
	if got := m.dueAnnouncements([]gamestate.Timer{longBuff}, now); len(got) != 0 {
		t.Errorf("should not re-announce the same timer, got %v", got)
	}

	// a short 60s debuff with 30s left is NOT yet in its 15s lead → silent
	m2 := &Model{announced: map[string]bool{}}
	shortDot := gamestate.Timer{Spell: "Snare", Target: "a gnoll", Start: now - 30, Expiry: now + 30}
	if got := m2.dueAnnouncements([]gamestate.Timer{shortDot}, now); len(got) != 0 {
		t.Errorf("short debuff at 30s left should stay silent (15s lead), got %v", got)
	}
	// ...but at 15s left it fires
	shortDot.Expiry = now + 15
	if got := m2.dueAnnouncements([]gamestate.Timer{shortDot}, now); len(got) != 1 {
		t.Errorf("short debuff at 15s left should warn, got %v", got)
	}
}
