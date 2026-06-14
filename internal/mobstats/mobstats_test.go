package mobstats

import "testing"

func TestIndexAvgMaxOverall(t *testing.T) {
	ix := New()
	for _, d := range []int{40, 60, 50} {
		ix.Record("a sand giant", d)
	}
	ix.Record("a gnoll", 10)

	if avg, ok := ix.AvgHit("a sand giant"); !ok || avg != 50 {
		t.Errorf("sand giant avg = %d ok=%v, want 50", avg, ok)
	}
	if mx, ok := ix.MaxHit("a sand giant"); !ok || mx != 60 {
		t.Errorf("sand giant max = %d ok=%v, want 60", mx, ok)
	}
	// case-insensitive lookup
	if avg, _ := ix.AvgHit("A Sand Giant"); avg != 50 {
		t.Errorf("lookup should be case-insensitive, got %d", avg)
	}
	// overall across all mobs: (40+60+50+10)/4 = 40
	if o, ok := ix.Overall(); !ok || o != 40 {
		t.Errorf("overall = %d ok=%v, want 40", o, ok)
	}
	if ix.Len() != 2 {
		t.Errorf("len = %d, want 2", ix.Len())
	}
}

func TestIndexGuardsAndClear(t *testing.T) {
	var nilIx *Index
	if _, ok := nilIx.AvgHit("x"); ok { // nil-safe
		t.Error("nil index should report no data")
	}
	nilIx.Record("x", 5) // must not panic

	ix := New()
	ix.Record("a rat", 0) // non-positive damage ignored
	ix.Record("", 5)      // empty name ignored
	if ix.Len() != 0 {
		t.Errorf("bad inputs should record nothing, len=%d", ix.Len())
	}

	ix.Record("a rat", 5)
	ix.Clear()
	if _, ok := ix.AvgHit("a rat"); ok {
		t.Error("Clear should drop all tallies")
	}
}
