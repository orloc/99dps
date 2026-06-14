package gamestate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDurationSeconds_AllFormulas covers every branch of EQ's classic
// buffdurationformula at representative caster levels. Each non-zero result is
// (ticks + 1 cast-tick) * 6s; formula 0 alone returns 0 (no timer, no tick).
func TestDurationSeconds_AllFormulas(t *testing.T) {
	tick := func(n int) int { return (n + 1) * 6 } // n formula ticks → seconds
	cases := []struct {
		name    string
		formula int
		cap     int
		level   int
		want    int
	}{
		// 0: instant nuke — no timer at all (the +1 tick is NOT added).
		{"f0 nuke", 0, 0, 60, 0},

		// 1 & 6: ceil(level/2), capped. Uncapped low level vs capped high level.
		{"f1 uncapped low", 1, 100, 8, tick(4)}, // ceil(8/2)=4
		{"f1 capped high", 1, 7, 60, tick(7)},   // ceil(30)=30 → cap 7
		{"f6 same as f1", 6, 100, 9, tick(5)},   // ceil(9/2)=5

		// 2: ceil(level/5*3), capped.
		{"f2 uncapped", 2, 100, 50, tick(30)}, // ceil(150/5)=30
		{"f2 capped", 2, 10, 60, tick(10)},    // ceil(180/5)=36 → cap 10

		// 3: level*30, capped (SoW-style long buffs).
		{"f3 uncapped", 3, 9999, 5, tick(150)}, // 5*30
		{"f3 capped", 3, 360, 50, tick(360)},   // 1500 → cap 360

		// 4: dc==0 → 50 ticks; else the cap.
		{"f4 zero-cap default", 4, 0, 60, tick(50)},
		{"f4 capped", 4, 33, 60, tick(33)},

		// 5: the cap, but a zero cap becomes 1 tick (never instant).
		{"f5 cap", 5, 6, 60, tick(6)},
		{"f5 zero-cap → 1", 5, 0, 60, tick(1)},

		// 7: level, capped.
		{"f7 uncapped", 7, 100, 40, tick(40)},
		{"f7 capped", 7, 4, 60, tick(4)},

		// 9: level*2+10, capped.
		{"f9 uncapped", 9, 999, 20, tick(50)}, // 20*2+10
		{"f9 capped", 9, 30, 60, tick(30)},    // 130 → cap 30

		// 10: level*3+10, capped.
		{"f10 uncapped", 10, 999, 10, tick(40)}, // 10*3+10
		{"f10 capped", 10, 50, 60, tick(50)},    // 190 → cap 50

		// 11, 12, 15: the cap verbatim.
		{"f11 cap", 11, 20, 60, tick(20)},
		{"f12 cap", 12, 17, 60, tick(17)},
		{"f15 cap", 15, 8, 60, tick(8)},

		// 50: a permanent illusion/buff — a fixed 72000 ticks.
		{"f50 permanent", 50, 0, 60, tick(72000)},

		// 3600: dc==0 → 3600 ticks; else the cap.
		{"f3600 zero-cap default", 3600, 0, 60, tick(3600)},
		{"f3600 capped", 3600, 100, 60, tick(100)},

		// default (unknown formula): falls back to the cap.
		{"default uses cap", 99, 12, 60, tick(12)},
		// a zero/negative tick result is no timer (default with cap 0).
		{"default zero-cap → none", 99, 0, 60, 0},
	}
	for _, c := range cases {
		s := &Spell{DurFormula: c.formula, DurCap: c.cap}
		if got := s.DurationSeconds(c.level); got != c.want {
			t.Errorf("%s: DurationSeconds(formula=%d cap=%d level=%d) = %d, want %d",
				c.name, c.formula, c.cap, c.level, got, c.want)
		}
	}
}

// TestPacifyDurationSeconds_AllCases covers the P99 lull/calm/pacify durations:
// capped Calm 180s, capped Pacify 210s honoring a low-level cap, and uncapped
// Wake of Tranquility (the formula-8 level+10 base). No +1 cast-tick.
func TestPacifyDurationSeconds_AllCases(t *testing.T) {
	calm := Spell{DurCap: 30}   // Calm
	pacify := Spell{DurCap: 35} // Pacify
	wake := Spell{DurCap: 0}    // Wake of Tranquility (uncapped)

	if got := calm.PacifyDurationSeconds(60); got != 180 {
		t.Errorf("Calm @60 = %d, want 180 (30 ticks)", got)
	}
	if got := pacify.PacifyDurationSeconds(60); got != 210 {
		t.Errorf("Pacify @60 = %d, want 210 (35 ticks)", got)
	}
	// a low-level caster is governed by level+10, not the cap
	if got := pacify.PacifyDurationSeconds(10); got != 120 {
		t.Errorf("Pacify @10 = %d, want 120 (min(20,35) ticks)", got)
	}
	// uncapped → level+10 ticks (≈7 min at 60)
	if got := wake.PacifyDurationSeconds(60); got != 420 {
		t.Errorf("Wake @60 = %d, want 420 (70 ticks)", got)
	}
	// a degenerate non-positive tick count yields no timer
	if got := (Spell{DurCap: 0}).PacifyDurationSeconds(-20); got != 0 {
		t.Errorf("non-positive ticks should be 0, got %d", got)
	}
}

// TestDecode_MalformedRows: a short row (fewer than minFields) and a row with an
// empty name both decode to nil and are dropped, so the book stays clean.
func TestDecode_MalformedRows(t *testing.T) {
	short := "0^Too Short^0" // far fewer than 217 fields
	noName := row(map[int]string{fName: ""})
	good := row(map[int]string{fName: "Real Spell", fDurFormula: "1", fDurCap: "10"})

	b := loadBook(t, short, noName, good)
	if b.Len() != 1 {
		t.Fatalf("malformed rows should be dropped, book Len = %d, want 1", b.Len())
	}
	if _, ok := b.ByName("Real Spell"); !ok {
		t.Error("the well-formed spell should still load")
	}
}

// TestDecode_GoodEffectFlag: GoodEffect 0 marks a detrimental spell, 1/2 a
// beneficial one — and only beneficial instant self-buffs are indexed by emote
// (a detrimental proc's cast_on_you belongs to the incoming-debuff path).
func TestDecode_GoodEffectFlag(t *testing.T) {
	beneficial := row(map[int]string{
		fName: "Inner Fire", fCastOnYou: "You feel protected.",
		fCastTime: "0", fDurFormula: "3", fDurCap: "100", fGoodEffect: "1",
	})
	detrimental := row(map[int]string{
		fName: "Hostile Proc", fCastOnYou: "You feel the poison.",
		fCastTime: "0", fDurFormula: "3", fDurCap: "100", fGoodEffect: "0",
	})
	b := loadBook(t, beneficial, detrimental)

	if s, _ := b.ByName("Inner Fire"); s.Detrimental {
		t.Error("GoodEffect 1 should be beneficial")
	}
	if s, _ := b.ByName("Hostile Proc"); !s.Detrimental {
		t.Error("GoodEffect 0 should be detrimental")
	}
	// the beneficial self-buff is reachable as a clicky; the hostile one is not
	if _, ok := b.SelfClicky("You feel protected."); !ok {
		t.Error("beneficial instant self-buff should be indexed by its emote")
	}
	if _, ok := b.SelfClicky("You feel the poison."); ok {
		t.Error("a detrimental cast_on_you must NOT be indexed as a clicky")
	}
}

// TestDecode_CCFlags: the mez/charm/pacify flags are detected from the spell's
// emotes/fade message during decode.
func TestDecode_CCFlags(t *testing.T) {
	b := loadBook(t,
		row(map[int]string{fName: "Enthrall", fCastOnOther: " has been enthralled.", fDurFormula: "7", fDurCap: "4"}),
		row(map[int]string{fName: "Beguile", fFades: "You are no longer charmed.", fDurFormula: "10", fDurCap: "70"}),
		row(map[int]string{fName: "Lull", fCastOnOther: " looks less aggressive.", fDurFormula: "8", fDurCap: "30"}),
	)
	if s, _ := b.ByName("Enthrall"); !s.Mez {
		t.Error("an 'enthrall' emote should flag Mez")
	}
	if s, _ := b.ByName("Beguile"); !s.Charm {
		t.Error("the 'no longer charmed' fade should flag Charm")
	}
	if s, _ := b.ByName("Lull"); !s.Pacify {
		t.Error("a 'looks less aggressive' emote should flag Pacify")
	}
}

// TestLoad_FromFile exercises the file-reading Load path with a temp spells file.
func TestLoad_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spells_us.txt")
	content := envenomedBolt() + "\n" + row(map[int]string{fName: "Clarity", fDurFormula: "5", fDurCap: "100"})
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Len() != 2 {
		t.Errorf("loaded %d spells, want 2", b.Len())
	}
	if _, ok := b.ByName("Envenomed Bolt"); !ok {
		t.Error("Envenomed Bolt should load from file")
	}

	// a missing file is an error, not a panic
	if _, err := Load(filepath.Join(dir, "nope.txt")); err == nil {
		t.Error("Load of a missing file should error")
	}
}

// TestBook_Len: Len reports the number of named spells, including the empty book.
func TestBook_Len(t *testing.T) {
	if got := loadBook(t).Len(); got != 0 {
		t.Errorf("empty book Len = %d, want 0", got)
	}
	if got := loadBook(t, envenomedBolt()).Len(); got != 1 {
		t.Errorf("one-spell book Len = %d, want 1", got)
	}
}

// TestTracker_SpellCountAndLevel: the trivial getters reflect the loaded book
// size and the level set via a /who, and degrade to zero on a nil tracker / book.
func TestTracker_SpellCountAndLevel(t *testing.T) {
	tr := NewTracker(loadBook(t, envenomedBolt()))
	if got := tr.SpellCount(); got != 1 {
		t.Errorf("SpellCount = %d, want 1", got)
	}
	if got := tr.Level(); got != 0 {
		t.Errorf("Level before /who = %d, want 0", got)
	}
	tr.SetLevel(57)
	if got := tr.Level(); got != 57 {
		t.Errorf("Level = %d, want 57", got)
	}

	var nilTr *Tracker
	if nilTr.SpellCount() != 0 || nilTr.Level() != 0 {
		t.Error("nil tracker getters should be zero, not panic")
	}
	if NewTracker(nil).SpellCount() != 0 {
		t.Error("SpellCount with a nil book should be 0")
	}
}
