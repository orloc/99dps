package eqclass

import "testing"

// TestHasDisciplines: every melee/hybrid class has disciplines; casters and
// Beastlord have none (so the widget hides for them).
func TestHasDisciplines(t *testing.T) {
	for _, c := range classList {
		want := false
		switch ProfileOf(c).Category {
		case CatMelee, CatHybrid:
			want = c != ClassBeastlord // Beastlord is a Luclin class — no P99 discs
		}
		if got := HasDisciplines(c); got != want {
			t.Errorf("HasDisciplines(%s) = %v, want %v", c, got, want)
		}
	}
}

// TestDisciplineData: entries are well-formed (named, leveled, sane reuse) and
// ordered lowest level first, so the widget can render them top-down.
func TestDisciplineData(t *testing.T) {
	for c, list := range disciplines {
		prev := 0
		for _, d := range list {
			if d.Name == "" || d.Level <= 0 || d.ReuseSec <= 0 {
				t.Errorf("%s: malformed discipline %+v", c, d)
			}
			if d.Level < prev {
				t.Errorf("%s: %s (L%d) out of level order after L%d", c, d.Name, d.Level, prev)
			}
			if d.DurSec > d.ReuseSec {
				t.Errorf("%s: %s duration %ds exceeds reuse %ds", c, d.Name, d.DurSec, d.ReuseSec)
			}
			prev = d.Level
		}
	}
}
