package cli

import "testing"

// ascending reports whether the ints are strictly increasing — the bottom-row
// column boundaries must not overlap or invert.
func ascending(xs ...int) bool {
	for i := 1; i < len(xs); i++ {
		if xs[i-1] >= xs[i] {
			return false
		}
	}
	return true
}

func TestEnchanterColsTimerFloor(t *testing.T) {
	// wide landscape: the timer column is proportional and well above the floor
	x0, cc, repop := enchanterCols(200)
	if w := cc - x0; w < timerMinCols {
		t.Errorf("wide: timer width %d below floor %d", w, timerMinCols)
	}
	if w := cc - x0; w <= 30 {
		t.Errorf("wide: timer should stay proportional (got %d, want >30)", w)
	}
	if !ascending(x0, cc, repop, 200) {
		t.Errorf("wide: bad ordering x0=%d cc=%d repop=%d", x0, cc, repop)
	}

	// narrow/portrait: proportional would be tiny, so the floor must hold
	x0, cc, repop = enchanterCols(120)
	if w := cc - x0; w < timerMinCols {
		t.Errorf("portrait: timer width %d not floored to %d", w, timerMinCols)
	}
	if !ascending(x0, cc, repop, 120) {
		t.Errorf("portrait: bad ordering x0=%d cc=%d repop=%d", x0, cc, repop)
	}

	// pathologically small: the floor yields but boundaries stay ordered (no overlap)
	x0, cc, repop = enchanterCols(40)
	if !ascending(x0, cc, repop, 40) {
		t.Errorf("tiny: bad ordering x0=%d cc=%d repop=%d", x0, cc, repop)
	}
}
