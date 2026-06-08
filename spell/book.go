// Package spell parses EverQuest's spells_us.txt into a lookup table and tracks
// live durations for spells the player casts (debuffs on others, self/other
// buffs). The data is the canonical client SPDat_Spell_Struct (217 ^-delimited
// fields); only the handful we need are decoded.
package spell

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// field indices in spells_us.txt (0-based)
const (
	fName        = 1
	fCastOnYou   = 6  // emote when the spell lands on you
	fCastOnOther = 7  // emote when it lands on someone else (prefix = target)
	fFades       = 8  // wear-off message
	fCastTime    = 13 // cast time, ms
	fDurFormula  = 16 // buffdurationformula
	fDurCap      = 17 // buffduration (tick cap)
	fGoodEffect  = 83 // 0 = detrimental, 1/2 = beneficial
	minFields    = 84 // we read up to index 83
)

// Spell is the decoded subset of one spells_us.txt row.
type Spell struct {
	Name        string
	CastTimeMs  int
	DurFormula  int
	DurCap      int
	CastOnYou   string
	CastOnOther string
	Fades       string
	Detrimental bool
	Charm       bool // a charm spell — no landing emote, breaks unpredictably
}

// DurationSeconds returns the spell's duration at the given caster level, using
// EQ's classic buffdurationformula (ticks are 6s). 0 means instant (no timer).
func (s *Spell) DurationSeconds(level int) int {
	dc := s.DurCap
	var ticks int
	switch s.DurFormula {
	case 0:
		ticks = 0
	case 1, 6:
		ticks = min(ceilDiv(level, 2), dc) // ceil(level/2)
	case 2:
		ticks = min(ceilDiv(level*3, 5), dc) // ceil(level/5*3)
	case 3:
		ticks = min(level*30, dc)
	case 4:
		if dc == 0 {
			ticks = 50
		} else {
			ticks = dc
		}
	case 5:
		ticks = dc
		if ticks == 0 {
			ticks = 3
		}
	case 7:
		ticks = min(level, dc)
	case 8:
		ticks = min(level+10, dc)
	case 9:
		ticks = min(level*2+10, dc)
	case 10:
		ticks = min(level*3+10, dc)
	case 11, 12, 15:
		ticks = dc
	case 50:
		ticks = 72000
	case 3600:
		if dc == 0 {
			ticks = 3600
		} else {
			ticks = dc
		}
	default:
		ticks = dc
	}
	return ticks * 6
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// Book is a spell lookup keyed by name.
type Book struct {
	byName map[string]*Spell
	// byEmote maps an instant self-buff's landing emote (cast_on_you) to its
	// spell, so clickies that emit no "You begin casting" line (Journeyman Boots
	// etc.) are still trackable.
	byEmote map[string]*Spell
}

// ByName returns the spell with the given name, if known.
func (b *Book) ByName(name string) (*Spell, bool) {
	s, ok := b.byName[name]
	return s, ok
}

// SelfClicky returns the instant self-buff whose cast-on-you emote is line, if
// any — used to time clickies that produce no cast line.
func (b *Book) SelfClicky(line string) (*Spell, bool) {
	s, ok := b.byEmote[line]
	return s, ok
}

// Len is the number of spells loaded.
func (b *Book) Len() int { return len(b.byName) }

// Load parses spells_us.txt at path.
func Load(path string) (*Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadReader(f)
}

// LoadReader parses spells_us.txt content from r.
func LoadReader(r io.Reader) (*Book, error) {
	b := &Book{byName: make(map[string]*Spell, 9000), byEmote: make(map[string]*Spell)}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		s := decode(sc.Text())
		if s == nil {
			continue
		}
		b.byName[s.Name] = s
		// index instant (cast-time 0) self-buffs with a real duration by their
		// landing emote — these clickies emit no "You begin casting" line. First
		// spell per emote wins (duplicates like the Boots line share a duration).
		if s.CastTimeMs == 0 && s.DurFormula != 0 && s.CastOnYou != "" {
			if _, dup := b.byEmote[s.CastOnYou]; !dup {
				b.byEmote[s.CastOnYou] = s
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

func decode(line string) *Spell {
	f := strings.Split(line, "^")
	if len(f) < minFields {
		return nil
	}
	name := f[fName]
	if name == "" {
		return nil
	}
	fades := NormEmote(strings.TrimSpace(f[fFades]))
	return &Spell{
		Name:        name,
		CastTimeMs:  atoi(f[fCastTime]),
		DurFormula:  atoi(f[fDurFormula]),
		DurCap:      atoi(f[fDurCap]),
		CastOnYou:   NormEmote(strings.TrimSpace(f[fCastOnYou])),
		CastOnOther: NormEmote(strings.TrimRight(f[fCastOnOther], " ")),
		Fades:       fades,
		Detrimental: atoi(f[fGoodEffect]) == 0,
		// every charm spell carries this wear-off marker and has no landing emote
		Charm: fades == "You are no longer charmed.",
	}
}

// NormEmote collapses a run of trailing periods to a single one. spells_us.txt
// has data with doubled trailing periods ("..") that the game log renders as a
// single "."; normalizing both the stored emote and the incoming log line lets
// them match regardless of which side is doubled.
func NormEmote(s string) string {
	trimmed := strings.TrimRight(s, ".")
	if trimmed == s {
		return s // no trailing period to collapse
	}
	return trimmed + "."
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
