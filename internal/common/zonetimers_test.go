package common

import "testing"

func TestZoneRespawn(t *testing.T) {
	cases := []struct {
		zone string
		want int
		ok   bool
	}{
		{"Greater Faydark", 425, true},
		{"East Commonlands", 400, true},
		{"The Feerrott", 400, true},    // leading "the" stripped
		{"the feerrott.", 400, true},   // case + trailing period normalized
		{"Plane of Fear", 28800, true}, // 8 hours
		{"Sleeper's Tomb", 28800, true},
		{"Nonexistent Zone", 0, false},
	}
	for _, c := range cases {
		got, ok := ZoneRespawn(c.zone)
		if got != c.want || ok != c.ok {
			t.Errorf("ZoneRespawn(%q) = (%d, %v), want (%d, %v)", c.zone, got, ok, c.want, c.ok)
		}
	}
}
