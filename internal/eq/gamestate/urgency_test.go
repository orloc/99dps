package gamestate

import "testing"

// TestTimerUrgency_AbsoluteCaps: short timers stay fractional, but long buffs are
// capped so they don't read orange/red with many minutes left.
func TestTimerUrgency_AbsoluteCaps(t *testing.T) {
	cases := []struct {
		name             string
		remaining, total int64
		want             Urgency
	}{
		// a 2-hour self-buff: green until 5 min, orange under 5 min, red under 1 min
		{"2h buff, 1h left", 3600, 7200, Fresh},
		{"2h buff, 10m left", 600, 7200, Fresh},
		{"2h buff, 4m left", 240, 7200, Low},
		{"2h buff, 30s left", 30, 7200, Expiring},
		// a 3-minute debuff stays fractional (caps don't bite): gold ≤90s, red ≤36s
		{"3m debuff, 2m left", 120, 180, Fresh},
		{"3m debuff, 80s left", 80, 180, Low},
		{"3m debuff, 30s left", 30, 180, Expiring},
		// degenerate
		{"no duration", 5, 0, Expiring},
		{"already expired", 0, 600, Expiring},
	}
	for _, c := range cases {
		if got := TimerUrgency(c.remaining, c.total); got != c.want {
			t.Errorf("%s: TimerUrgency(%d,%d) = %d, want %d", c.name, c.remaining, c.total, got, c.want)
		}
	}
}
