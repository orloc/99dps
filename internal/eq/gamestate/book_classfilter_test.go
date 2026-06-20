package gamestate

import (
	"strconv"
	"testing"

	"99dps/internal/eq/eqclass"
)

// classBlockOffset is where the test rows place the 16-wide per-class block. It's
// arbitrary (calibration auto-finds it); 104 mirrors the user's real 237-field
// file. row() builds 217-field lines, so 104+15=119 fits.
const classBlockOffset = 104

// withClasses sets the per-class min-level block (classIdx → level) on a field map,
// defaulting every other class column to 255 (can't cast). Pass nil levels for an
// NPC-only spell (no class can cast it).
func withClasses(fields map[int]string, levels map[int]int) map[int]string {
	for i := range numSpellClasses {
		fields[classBlockOffset+i] = "255"
	}
	for idx, lv := range levels {
		fields[classBlockOffset+idx] = strconv.Itoa(lv)
	}
	return fields
}

// gateAnchor is the calibration keystone: Gate, castable by exactly the 7 pure
// casters (indices 1,5,9,10,11,12,13), nothing else.
func gateAnchor() string {
	return row(withClasses(map[int]string{fName: "Gate"}, map[int]int{
		1: 5, 5: 5, 9: 5, 10: 4, 11: 4, 12: 4, 13: 4,
	}))
}

// idxCleric etc. — class indices in the spells_us block, for readable tests.
const (
	idxCleric  = 1
	idxDruid   = 5
	idxShaman  = 9
	idxEnch    = 13
	idxWarrior = 0
)

// TestClassCalibrationAndFilter: with a Gate keystone present, the book calibrates
// the class block, prefers the player-castable row over an NPC-only duplicate
// (regardless of file order), and reports the class as known.
func TestClassCalibrationAndFilter(t *testing.T) {
	npcCripple := row(withClasses(map[int]string{
		fName: "Cripple", fCastOnOther: "x", fDurFormula: "3", fDurCap: "99",
	}, nil)) // all 255 → NPC-only
	playerCripple := row(withClasses(map[int]string{
		fName: "Cripple", fCastOnOther: "y", fDurFormula: "3", fDurCap: "70",
	}, map[int]int{idxShaman: 53, idxEnch: 53}))

	// NPC row LAST — last-wins would have picked it; the dedup must not.
	b := loadBook(t, gateAnchor(), playerCripple, npcCripple)
	if !b.classKnown {
		t.Fatal("class block should have calibrated from the Gate anchor")
	}
	s, ok := b.ByName("Cripple")
	if !ok || s.DurCap != 70 {
		t.Fatalf("canonical Cripple = %+v, want the player-castable DurCap 70", s)
	}
	if !s.playerCastable() {
		t.Error("canonical Cripple should be player-castable")
	}
}

// TestByNameForClass: a name castable by two classes resolves to the variant the
// player's class can cast (distinguished here by DurCap), and falls back to the
// canonical pick for a class that can't cast it.
func TestByNameForClass(t *testing.T) {
	shaCripple := row(withClasses(map[int]string{
		fName: "Cripple", fDurFormula: "3", fDurCap: "70",
	}, map[int]int{idxShaman: 53}))
	encCripple := row(withClasses(map[int]string{
		fName: "Cripple", fDurFormula: "3", fDurCap: "55",
	}, map[int]int{idxEnch: 53}))
	b := loadBook(t, gateAnchor(), shaCripple, encCripple)

	if s, ok := b.ByNameForClass("Cripple", eqclass.ClassShaman); !ok || s.DurCap != 70 {
		t.Errorf("Shaman Cripple = %+v, want DurCap 70", s)
	}
	if s, ok := b.ByNameForClass("Cripple", eqclass.ClassEnchanter); !ok || s.DurCap != 55 {
		t.Errorf("Enchanter Cripple = %+v, want DurCap 55", s)
	}
	// a class that can't cast either → canonical fallback (still resolves, never panics)
	if _, ok := b.ByNameForClass("Cripple", eqclass.ClassWarrior); !ok {
		t.Error("Warrior should fall back to the canonical Cripple, not miss")
	}
}

// TestNPCSelfClickyFiltered: an NPC-only instant self-buff must not hijack the
// byEmote (self-clicky) index when class data is known — structurally excluded,
// not by name-prefix heuristics. A player-castable buff with the same emote wins.
func TestNPCSelfClickyFiltered(t *testing.T) {
	const emote = "You feel resolute."
	npcBuff := row(withClasses(map[int]string{
		fName: "NPC Resolve", fCastOnYou: emote, fCastTime: "0", fDurFormula: "5", fDurCap: "100", fGoodEffect: "1",
	}, nil)) // NPC-only
	playerBuff := row(withClasses(map[int]string{
		fName: "Resolve", fCastOnYou: emote, fCastTime: "0", fDurFormula: "5", fDurCap: "20", fGoodEffect: "1",
	}, map[int]int{idxCleric: 10}))
	b := loadBook(t, gateAnchor(), npcBuff, playerBuff)

	s, ok := b.SelfClicky(emote)
	if !ok {
		t.Fatal("the player-castable buff should be indexed by its emote")
	}
	if s.Name != "Resolve" {
		t.Errorf("self-clicky for %q = %q, want the player-castable Resolve (NPC copy must be excluded)", emote, s.Name)
	}
}

// TestNoCalibrationFallback: without a Gate keystone (a custom/foreign file), the
// book can't calibrate, reports classKnown=false, and falls back to the original
// last-row-wins behavior — no regression, no false NPC filtering.
func TestNoCalibrationFallback(t *testing.T) {
	first := row(map[int]string{fName: "Dup", fDurCap: "10"})
	last := row(map[int]string{fName: "Dup", fDurCap: "20"})
	b := loadBook(t, first, last)
	if b.classKnown {
		t.Fatal("no Gate anchor → must not claim class data")
	}
	if s, _ := b.ByName("Dup"); s.DurCap != 20 {
		t.Errorf("without class data, last row wins (DurCap 20), got %+v", s)
	}
	// castability is unknown → treated as castable so nothing is filtered out
	if s, _ := b.ByName("Dup"); !s.playerCastable() {
		t.Error("unknown castability should read as castable (no filtering)")
	}
}
