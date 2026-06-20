// Package spellwiki validates the dynamically-selected spell against the
// authoritative P99 wiki. The client's spells_us.txt is a SUPERSET — the server
// hides spells, and the wiki is the source of truth for what actually exists and
// which classes cast it. This package fetches a single spell's wiki page on
// demand (lazy, per cast), parses the {{Spellpagesmart}} template, and caches the
// result so each spell is fetched once.
//
// It has no internal dependencies (like prices/eqmap) and never touches the
// render loop; the TUI drives it through an interface and a tea.Cmd, and uses the
// verdict to flag a timer whose class/existence the wiki doesn't confirm
// ("trust the wiki, flag the mismatch").
package spellwiki

import (
	"regexp"
	"strconv"
	"strings"
)

// Spell is the subset of a P99 wiki spell page we use to validate a cast.
type Spell struct {
	Name        string         // the page's spellname
	Classes     map[string]int // normalized class name (see NormClass) -> P99 min level
	Duration    string         // raw duration text, e.g. "6.3 minutes @L53 to 7.0 minutes @L60"
	Detrimental bool           // spell_type == Detrimental
	Resist      string         // e.g. "Magic (0)", "Unresistable"
	CastOnOther string         // landing emote with the "Someone" target placeholder stripped
}

// CastableBy reports the P99 level the given class can cast this spell, and
// whether it can at all. The class is matched normalized, so "Shadow Knight" and
// the wiki's "Shadowknight" compare equal.
func (s Spell) CastableBy(class string) (int, bool) {
	lv, ok := s.Classes[NormClass(class)]
	return lv, ok
}

// NormClass canonicalizes a class name for comparison: lowercased, spaces removed,
// so eqclass names and the wiki's link text match regardless of spacing.
func NormClass(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
}

// NormName canonicalizes a spell name to its wiki spelling: spells_us stores many
// possessives with a BACKTICK (e.g. "Solon`s Song") and some text uses a
// typographic apostrophe, but the wiki titles all use a plain ASCII apostrophe.
// Folding them lets a logged cast match the dataset key (which is generated from
// wiki titles). Used by both the runtime lookup and the generator.
func NormName(name string) string {
	return strings.NewReplacer("`", "'", "’", "'").Replace(strings.TrimSpace(name))
}

var (
	// a "| key = inlinevalue" line that opens a template parameter.
	paramRe = regexp.MustCompile(`^\|\s*([A-Za-z_]+)\s*=\s*(.*)$`)
	// a class entry within the classes parameter: "* [[Ranger]] - Level 30 (...)".
	// Only lines with an explicit "- Level N" count, so the "see also" list links
	// ("* [[Shaman Spells (Good)]]") are excluded. The first number is the current
	// (P99/Velious) level; a trailing "(39 Pre Velious)" annotation is ignored.
	classRe = regexp.MustCompile(`^\*\s*\[\[([^\]|]+?)\]\]\s*-\s*Level\s+(\d+)`)
)

// Parse decodes a {{Spellpagesmart}} wiki page. The second return is false when
// the text isn't a spell page (a redirect, a missing page, or a non-spell
// article — neither {{Spellpage}} nor {{Spellpagesmart}}), so the caller can
// treat "no spell page" as "not on the server".
func Parse(raw string) (Spell, bool) {
	// the wiki uses two spell templates with identical parameters: {{Spellpage}}
	// (older) and {{Spellpagesmart}}. The "{{Spellpage" prefix matches both.
	if !strings.Contains(raw, "{{Spellpage") {
		return Spell{}, false
	}
	sp := Spell{Classes: map[string]int{}}
	lines := strings.Split(raw, "\n")
	current := "" // the parameter whose continuation lines we're collecting
	for _, ln := range lines {
		if m := paramRe.FindStringSubmatch(ln); m != nil {
			current = m[1]
			val := strings.TrimSpace(m[2])
			switch current {
			case "spellname":
				sp.Name = val
			case "duration":
				sp.Duration = val
			case "spell_type":
				sp.Detrimental = strings.EqualFold(val, "Detrimental")
			case "resist":
				sp.Resist = val
			case "msg_cast_on_other":
				sp.CastOnOther = stripTargetPlaceholder(val)
			}
			continue
		}
		// continuation lines of the current parameter — only the classes list is
		// multi-line and interesting.
		if current == "classes" {
			if m := classRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
				if lv, err := strconv.Atoi(m[2]); err == nil {
					sp.Classes[NormClass(m[1])] = lv
				}
			}
		}
	}
	if sp.Name == "" && len(sp.Classes) == 0 {
		return Spell{}, false // a Spellpagesmart stub with nothing usable
	}
	return sp, true
}

// stripTargetPlaceholder removes the wiki's "Someone" stand-in for the target name
// from a cast-on-other emote ("Someone  has been crippled." → " has been
// crippled."), leaving the suffix that matches spells_us's stored emote.
func stripTargetPlaceholder(s string) string {
	if rest, ok := strings.CutPrefix(s, "Someone"); ok {
		// the wiki inserts one separator space; the emote keeps its own leading space.
		return strings.TrimPrefix(rest, " ")
	}
	return s
}
