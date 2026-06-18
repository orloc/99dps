package gamestate

import (
	"strconv"
	"strings"
	"testing"
)

// row builds a 217-field spells_us.txt line with the given fields set.
func row(fields map[int]string) string {
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	for i, v := range fields {
		f[i] = v
	}
	return strings.Join(f, "^")
}

// loadBook parses the given spells_us.txt rows into a Book.
func loadBook(t *testing.T, rows ...string) *Book {
	t.Helper()
	b, err := LoadReader(strings.NewReader(strings.Join(rows, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// envenomedBolt is an instant-ish detrimental DoT used across decode/timer tests.
func envenomedBolt() string {
	return row(map[int]string{
		fName:        "Envenomed Bolt",
		fCastOnOther: "'s body convulses with the poison.",
		fFades:       " has been cured of the poison.",
		fCastTime:    "6100",
		fDurFormula:  "1",
		fDurCap:      "7",
		fGoodEffect:  "0", // detrimental
	})
}

// malo is an instant-cast debuff with a clear landing emote and fade line, so
// tests can drive same-named mobs without cast-completion timing.
func malo() string {
	return row(map[int]string{
		fName:        "Malosini",
		fCastOnOther: "'s magic resistances have been lowered.",
		fFades:       " feels less resistant.",
		fCastTime:    "0",
		fDurFormula:  "1",
		fDurCap:      "50",
		fGoodEffect:  "0", // detrimental
	})
}

const maloLand = "a sand giant's magic resistances have been lowered."

func castMalo(tr *Tracker, at int64) {
	tr.BeginCast("Malosini", at)
	tr.Observe(maloLand, at)
}

// canniBook builds a book with a single Cannibalize spell that has a recast time.
func canniBook(t *testing.T, name string, recastMs int) *Book {
	t.Helper()
	f := make([]string, 217)
	for i := range f {
		f[i] = "0"
	}
	f[1] = name
	f[15] = strconv.Itoa(recastMs)
	book, err := LoadReader(strings.NewReader(strings.Join(f, "^")))
	if err != nil {
		t.Fatalf("LoadReader: %v", err)
	}
	return book
}

// newPetTracker builds a tracker with an empty book and the given character set.
func newPetTracker(t *testing.T, character string) *Tracker {
	t.Helper()
	tr := NewTracker(loadBook(t)) // empty book is fine; pet detection doesn't need spells
	tr.SetCharacter(character)
	return tr
}
