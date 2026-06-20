package eqclass

import (
	"strings"
	"testing"
)

// TestDisciplineCoverage: every melee/hybrid class has disciplines; casters and
// Beastlord have none (so DisciplinesFor returns nil and the widget hides).
func TestDisciplineCoverage(t *testing.T) {
	for _, c := range classList {
		want := false
		switch ProfileOf(c).Category {
		case CatMelee, CatHybrid:
			want = c != ClassBeastlord // Beastlord is a Luclin class — no P99 discs
		}
		if got := len(DisciplinesFor(c)) > 0; got != want {
			t.Errorf("DisciplinesFor(%s) present=%v, want %v", c, got, want)
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

// TestDisciplineActivationsValid guards the activation table against drift: every
// emote's name must exist in `disciplines`, be unique to one class (so the derived
// class inference is sound), carry no stray leading/trailing whitespace, and be in
// NormEmote form (no doubled trailing period). DisciplineActivation must then
// resolve it with inferClass=true.
func TestDisciplineActivationsValid(t *testing.T) {
	for emote, name := range disciplineActivations {
		if emote != strings.TrimSpace(emote) {
			t.Errorf("activation %q has stray whitespace", emote)
		}
		if strings.HasSuffix(emote, "..") {
			t.Errorf("activation %q should be NormEmote form (single trailing period)", emote)
		}
		var owners []Class
		for c, list := range disciplines {
			for _, d := range list {
				if d.Name == name {
					owners = append(owners, c)
				}
			}
		}
		if len(owners) == 0 {
			t.Errorf("activation %q → %q: no such discipline in the table", emote, name)
		}
		if len(owners) > 1 {
			t.Errorf("activation %q → %q: not class-unique (owners %v)", emote, name, owners)
		}
		gotName, gotClass, infer, ok := DisciplineActivation(emote)
		if !ok || gotName != name || !infer || len(owners) != 1 || gotClass != owners[0] {
			t.Errorf("DisciplineActivation(%q) = (%q,%v,%v,%v), want (%q,%v,true,true)",
				emote, gotName, gotClass, infer, ok, name, owners)
		}
	}
	// a non-emote resolves to nothing
	if _, _, _, ok := DisciplineActivation("You begin to regenerate."); ok {
		t.Error("a non-discipline line must not resolve")
	}
}
