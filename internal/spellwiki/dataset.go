package spellwiki

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:generate go run ./gen -out spelldata.json

// rawSpelldata is the committed P99 spell set, scraped from the wiki's
// Category:Spells by ./gen (run via `go generate ./internal/spellwiki`). It's
// embedded so validation is fully offline — no per-cast network, no false
// positives from title/encoding quirks (those are reconciled at generation time).
//
//go:embed spelldata.json
var rawSpelldata []byte

// dataset is the bundled spell set keyed by foldKey(name).
type dataset struct {
	Generated string           `json:"generated"` // ISO date the file was scraped
	Source    string           `json:"source"`    // provenance (the wiki category)
	Spells    map[string]Spell `json:"spells"`
}

var (
	loaded   *dataset
	loadOnce sync.Once
)

// load parses the embedded dataset once. A malformed/empty file degrades to an
// empty set (validation then confirms nothing, flagging everything) rather than
// panicking — but the generator validates before writing, so that shouldn't happen.
func loadDataset() *dataset {
	loadOnce.Do(func() {
		var d dataset
		if json.Unmarshal(rawSpelldata, &d) == nil && d.Spells != nil {
			loaded = &d
			return
		}
		loaded = &dataset{Spells: map[string]Spell{}}
	})
	return loaded
}

// Key is the dataset lookup key: apostrophe-normalized (backtick/typographic →
// ASCII) and lowercased, so a logged cast matches regardless of those differences.
// Exported so the generator keys entries identically to the runtime lookup.
func Key(name string) string { return strings.ToLower(NormName(name)) }

// Validate reports whether the named spell exists in the bundled P99 set and
// whether the given class can cast it. A spell not in the set → (false, false)
// ("not on the server"). When class is "", castability is unknown so classOK is
// false and the caller should not flag (you can't judge without the class).
func Validate(name, class string) (found, classOK bool) {
	sp, ok := loadDataset().Spells[Key(name)]
	if !ok {
		return false, false
	}
	if class == "" {
		return true, false
	}
	_, classOK = sp.CastableBy(class)
	return true, classOK
}

// Count is the number of bundled spells (diagnostics / footer summary).
func Count() int { return len(loadDataset().Spells) }

// Generated is the ISO date the bundled dataset was scraped (for the footer).
func Generated() string { return loadDataset().Generated }
