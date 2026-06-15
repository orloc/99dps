package tui

import "testing"

func TestCompactDur(t *testing.T) {
	cases := []struct {
		sec  int64
		want string
	}{
		{0, "<1m"},
		{59, "<1m"},
		{60, "1m"},
		{75, "1m"},
		{600, "10m"},
		{3599, "59m"},
		{3600, "1h00m"},
		{3725, "1h02m"}, // 1h 2m 5s → minutes floored, seconds dropped
		{7800, "2h10m"},
	}
	for _, c := range cases {
		if got := compactDur(c.sec); got != c.want {
			t.Errorf("compactDur(%d) = %q, want %q", c.sec, got, c.want)
		}
	}
}
