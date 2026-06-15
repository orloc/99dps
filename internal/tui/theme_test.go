package tui

import "testing"

// TestHSVHex pins the rainbow primaries.
func TestHSVHex(t *testing.T) {
	for _, c := range []struct {
		h, s, v float64
		want    string
	}{
		{0, 1, 1, "#ff0000"}, {120, 1, 1, "#00ff00"}, {240, 1, 1, "#0000ff"},
	} {
		if got := hsvHex(c.h, c.s, c.v); got != c.want {
			t.Errorf("hsvHex(%g,%g,%g) = %s, want %s", c.h, c.s, c.v, got, c.want)
		}
	}
}
