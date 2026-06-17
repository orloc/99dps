package mobstats

import "testing"

func TestAvgAndOverall(t *testing.T) {
	ix := NewIndex()
	now := int64(1_000_000)
	for _, d := range []int{40, 60, 50} {
		ix.Record("a sand giant", d, now)
	}
	ix.Record("a gnoll", 10, now)

	if avg, ok := ix.AvgHit("a sand giant", now); !ok || avg != 50 {
		t.Errorf("sand giant avg = %d ok=%v, want 50", avg, ok)
	}
	if avg, _ := ix.AvgHit("A Sand Giant", now); avg != 50 {
		t.Errorf("lookup should be case-insensitive, got %d", avg)
	}
	if o, ok := ix.Overall(now); !ok || o != 40 { // (40+60+50+10)/4
		t.Errorf("overall = %d ok=%v, want 40", o, ok)
	}
	if seen := ix.Seen(now); len(seen) != 2 {
		t.Errorf("seen = %v, want 2 mobs", seen)
	}
}

// TestWindow: hits older than the window drop out of the average.
func TestWindow(t *testing.T) {
	ix := NewIndex()
	base := int64(1_000_000)
	ix.Record("a rat", 100, base)    // big
	ix.Record("a rat", 10, base+100) // small, both within the window

	// while both are inside the window they average together
	if avg, ok := ix.AvgHit("a rat", base+200); !ok || avg != 55 {
		t.Errorf("both recent hits should count: avg=%d ok=%v, want 55", avg, ok)
	}

	// a fresh hit long after; querying past the window leaves only it (the two old
	// big/small hits have aged out)
	ix.Record("a rat", 10, base+WindowSec+100)
	now := base + WindowSec + 150
	if avg, ok := ix.AvgHit("a rat", now); !ok || avg != 10 {
		t.Errorf("only the in-window hit should count: avg=%d ok=%v, want 10", avg, ok)
	}
}

func TestGuardsAndClear(t *testing.T) {
	var nilIx *Index
	if _, ok := nilIx.AvgHit("x", 0); ok {
		t.Error("nil index should report no data")
	}
	nilIx.Record("x", 5, 0) // must not panic

	ix := NewIndex()
	ix.Record("a rat", 0, 1) // non-positive ignored
	ix.Record("", 5, 1)      // empty name ignored
	if len(ix.Seen(1)) != 0 {
		t.Errorf("bad inputs should record nothing, seen=%v", ix.Seen(1))
	}

	ix.Record("a rat", 5, 1)
	ix.Clear()
	if _, ok := ix.AvgHit("a rat", 1); ok {
		t.Error("Clear should drop all tallies")
	}
}
