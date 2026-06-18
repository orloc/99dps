package gamestate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAndDuration(t *testing.T) {
	b := loadBook(t, envenomedBolt())
	s, ok := b.ByName("Envenomed Bolt")
	if !ok {
		t.Fatal("spell not loaded")
	}
	if s.CastTimeMs != 6100 || s.DurFormula != 1 || s.DurCap != 7 || !s.Detrimental {
		t.Fatalf("decoded wrong: %+v", s)
	}
	// formula 1: (min(ceil(level/2), cap=7) + 1 cast-tick) * 6s. At level 43:
	// ceil(21.5)=22 -> capped 7 -> +1 -> 8 ticks -> 48s.
	if got := s.DurationSeconds(43); got != 48 {
		t.Errorf("duration@43 = %d, want 48", got)
	}
	// at a low level the formula (not the cap) governs: level 8 -> ceil(4)=4 -> +1 -> 5 ticks -> 30s.
	if got := s.DurationSeconds(8); got != 30 {
		t.Errorf("duration@8 = %d, want 30", got)
	}
}

func TestNormEmote(t *testing.T) {
	cases := map[string]string{
		" has been poisoned..":               " has been poisoned.",
		"Your muscles erupt with strength..": "Your muscles erupt with strength.",
		" has been ensnared.":                " has been ensnared.", // already single
		" feels much faster":                 " feels much faster",  // no period
		"":                                   "",
	}
	for in, want := range cases {
		if got := NormEmote(in); got != want {
			t.Errorf("NormEmote(%q) = %q, want %q", in, got, want)
		}
	}
}

// Duration formula spot-checks against known EQ values (Bedlam-style formula 8,
// Snare-style formula 2, Spirit-of-Wolf-style formula 3).
func TestDurationFormulas(t *testing.T) {
	// each = (min(formula, cap) + 1 cast-tick) * 6s
	f8 := &Spell{DurFormula: 8, DurCap: 75} // Bedlam
	if got := f8.DurationSeconds(60); got != 426 {
		t.Errorf("formula 8 @60 = %d, want 426 ((min(70,75)+1)*6)", got)
	}
	if got := f8.DurationSeconds(30); got != 246 {
		t.Errorf("formula 8 @30 = %d, want 246 ((min(40,75)+1)*6)", got)
	}
	f2 := &Spell{DurFormula: 2, DurCap: 39} // Snare
	if got := f2.DurationSeconds(50); got != 186 {
		t.Errorf("formula 2 @50 = %d, want 186 ((min(ceil(30),39)+1)*6)", got)
	}
	f3 := &Spell{DurFormula: 3, DurCap: 360} // Spirit of Wolf
	if got := f3.DurationSeconds(50); got != 2166 {
		t.Errorf("formula 3 @50 = %d, want 2166 ((min(1500,360)+1)*6)", got)
	}
	f0 := &Spell{DurFormula: 0} // nuke — no timer (no cast-tick added)
	if got := f0.DurationSeconds(60); got != 0 {
		t.Errorf("formula 0 = %d, want 0", got)
	}
	// Curse of the Spirits (formula 1, cap 14): observed in game as 1:30 = 90s =
	// 15 ticks at any level >= 28 — the cast-tick is what makes it 15, not 14.
	curse := &Spell{DurFormula: 1, DurCap: 14}
	if got := curse.DurationSeconds(60); got != 90 {
		t.Errorf("Curse of the Spirits @60 = %d, want 90 (1:30)", got)
	}
}

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
	soothe := Spell{DurCap: 25} // Soothe
	calm := Spell{DurCap: 30}   // Calm
	pacify := Spell{DurCap: 35} // Pacify
	wake := Spell{DurCap: 0}    // Wake of Tranquility (uncapped)

	// Soothe (cap 25): level+10 ticks up to the cap — matches the wiki's
	// "1.8 min @L8 to 2.5 min @L15".
	if got := soothe.PacifyDurationSeconds(8); got != 108 {
		t.Errorf("Soothe @8 = %d, want 108 (18 ticks)", got)
	}
	if got := soothe.PacifyDurationSeconds(15); got != 150 {
		t.Errorf("Soothe @15 = %d, want 150 (25 ticks, capped)", got)
	}
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

// TestSelfClickyDisambiguation: when many spells share a clicky emote, indexing
// skips dev "Test" stubs and keeps the longest-duration (real) buff — so "Your
// feet leave the ground." resolves to a normal Levitation, not "Levitate Test".
func TestSelfClickyDisambiguation(t *testing.T) {
	book := loadBook(t,
		row(map[int]string{fName: "Levitate Test", fCastOnYou: "Your feet leave the ground.", fCastTime: "0", fDurFormula: "1", fDurCap: "6", fGoodEffect: "1"}),
		row(map[int]string{fName: "Levitation", fCastOnYou: "Your feet leave the ground.", fCastTime: "0", fDurFormula: "3", fDurCap: "120", fGoodEffect: "1"}),
	)
	s, ok := book.SelfClicky("Your feet leave the ground.")
	got := ""
	if s != nil {
		got = s.Name
	}
	if !ok || got != "Levitation" {
		t.Errorf("clicky should resolve to Levitation, got ok=%v name=%q", ok, got)
	}
}

// TestSelfClickyRegenExcludedBootsKept guards two things at once:
//   - a generic "buff from another caster" emote (group regen) is NOT treated as
//     a self-clicky, so a stray instant spell carrying it (the real spells_us
//     "Aura of Battle" #1499) can't mislabel every regen as Aura of Battle;
//   - Journeyman's Boots ("Your feet feel quick.") IS a self-clicky for any class
//     (the index is class-agnostic), so everyone's J-Boots are tracked.
func TestSelfClickyRegenExcludedBootsKept(t *testing.T) {
	b := loadBook(t,
		row(map[int]string{ // mislabeled instant buff whose emote is group regen
			fName: "Aura of Battle", fCastOnYou: "You begin to regenerate.",
			fCastOnOther: " begins to regenerate.", fFades: "You have stopped regenerating.",
			fCastTime: "0", fDurFormula: "10", fDurCap: "205", fGoodEffect: "1",
		}),
		row(map[int]string{ // Journeyman's Boots: instant self-clicky, usable by all
			fName: "JourneymanBoots", fCastOnYou: "Your feet feel quick.",
			fCastOnOther: " begins to walk faster.", fFades: "Your feet slow down.",
			fCastTime: "0", fDurFormula: "3", fDurCap: "180", fGoodEffect: "1",
		}),
	)

	if _, ok := b.SelfClicky("You begin to regenerate."); ok {
		t.Error("a group regen emote must not be a self-clicky")
	}
	if s, ok := b.SelfClicky("Your feet feel quick."); !ok || s.Name != "JourneymanBoots" {
		t.Errorf("J-Boots should be a self-clicky for any class, got ok=%v name=%v", ok, s)
	}

	// end-to-end on an unknown class: J-Boots tracked, regen ignored.
	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.Observe("You begin to regenerate.", 1000)
	tr.Observe("Your feet feel quick.", 1000)
	act := tr.Active(1001)
	if len(act) != 1 || act[0].Spell != "JourneymanBoots" {
		t.Errorf("expected only J-Boots tracked, got %+v", act)
	}
}

// TestSelfClickyExcludesNPCVariant: an instant NPC/GM copy of a real buff must not
// be indexed as a self-clicky — otherwise a cleric's Aegolism on you (the castable
// one has a cast time, so it's not a clicky) resolves to the instant "NPCAegolism".
func TestSelfClickyExcludesNPCVariant(t *testing.T) {
	const emote = "You are filled with the power of Aegolism."
	b := loadBook(t,
		// the castable buff: has a cast time, so it's never a self-clicky
		row(map[int]string{
			fName: "Aegolism", fCastOnYou: emote, fCastTime: "14000",
			fDurFormula: "3", fDurCap: "1500", fGoodEffect: "1",
		}),
		// the NPC variant: instant, would otherwise win the shared emote
		row(map[int]string{
			fName: "NPCAegolism", fCastOnYou: emote, fCastTime: "0",
			fDurFormula: "3", fDurCap: "1500", fGoodEffect: "1",
		}),
	)
	if s, ok := b.SelfClicky(emote); ok {
		t.Errorf("Aegolism cast on you must not resolve to a clicky (got %q)", s.Name)
	}

	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.Observe(emote, 1000) // a cleric's Aegolism lands on you
	if got := tr.Active(1001); len(got) != 0 {
		t.Errorf("an NPC buff variant must not be tracked as a self-buff, got %+v", got)
	}
}

// TestSelfClickyWhileCastPending: an insta-clicky must still be tracked when an
// unrelated cast is still pending (it just hasn't matched a landing). An
// enchanter is almost always mid-cast, so the old "skip clickies whenever a cast
// is pending" rule hid J-Boots entirely.
func TestSelfClickyWhileCastPending(t *testing.T) {
	b := loadBook(t,
		malo(), // a debuff to cast — leaves a lingering pending (its landing never comes)
		row(map[int]string{
			fName: "JourneymanBoots", fCastOnYou: "Your feet feel quick.",
			fCastTime: "0", fDurFormula: "3", fDurCap: "180", fGoodEffect: "1",
		}),
	)
	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.BeginCast("Malosini", 1000) // a cast that never lands (resisted) → pending lingers
	tr.Observe("Your feet feel quick.", 1002)

	found := false
	for _, tm := range tr.Active(1003) {
		if tm.Spell == "JourneymanBoots" {
			found = true
		}
	}
	if !found {
		t.Error("an insta-clicky should track even with a lingering pending cast")
	}
}

// TestLoad_RealFile validates parsing against an actual spells_us.txt when
// SPELLS_FILE points at one; otherwise it skips. Kept sequential (integration).
func TestLoad_RealFile(t *testing.T) {
	path := os.Getenv("SPELLS_FILE")
	if path == "" {
		t.Skip("set SPELLS_FILE to validate against a real spells_us.txt")
	}
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded %d spells", b.Len())
	for _, name := range []string{"Lightning Bolt", "Envenomed Bolt", "Clarity", "Snare", "Tashani"} {
		s, ok := b.ByName(name)
		if !ok {
			t.Errorf("missing %q", name)
			continue
		}
		t.Logf("%-16s cast=%5dms formula=%2d cap=%3d dur@50=%4ds detr=%-5v onOther=%q",
			name, s.CastTimeMs, s.DurFormula, s.DurCap, s.DurationSeconds(50), s.Detrimental, s.CastOnOther)
	}
}

// TestRealFile_TriggerCoverage audits our extracted triggers against the real
// spells_us.txt: counts each message type and asserts no doubled-period emote
// survived normalization. Set SPELLS_FILE to run. Kept sequential (integration).
func TestRealFile_TriggerCoverage(t *testing.T) {
	path := os.Getenv("SPELLS_FILE")
	if path == "" {
		t.Skip("set SPELLS_FILE to run")
	}
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	var onYou, onOther, fades, timed, badDots int
	for _, s := range b.byName {
		if s.CastOnYou != "" {
			onYou++
		}
		if s.CastOnOther != "" {
			onOther++
		}
		if s.Fades != "" {
			fades++
		}
		if s.DurFormula != 0 {
			timed++
		}
		for _, e := range []string{s.CastOnYou, s.CastOnOther, s.Fades} {
			if strings.HasSuffix(e, "..") {
				badDots++
			}
		}
	}
	t.Logf("spells=%d  cast_on_you=%d  cast_on_other=%d  fades=%d  timed(formula!=0)=%d",
		b.Len(), onYou, onOther, fades, timed)
	if badDots != 0 {
		t.Errorf("%d emotes still end in '..' after normalization", badDots)
	}

	// spot-check a known doubled-period spell normalizes to single
	if s, ok := b.ByName("Bedlam"); ok && strings.HasSuffix(s.CastOnYou, "..") {
		t.Errorf("Bedlam cast_on_you not normalized: %q", s.CastOnYou)
	}
}
