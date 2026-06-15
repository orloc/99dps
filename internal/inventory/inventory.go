// Package inventory reads the EverQuest Titanium "/outputfile inventory" dump —
// a tab-separated file (Location, Name, ID, Count, Slots) the client writes into
// the EQ folder as "<Character>-Inventory.txt", covering worn gear, bags, and
// bank. The dump carries no NO-DROP/tradeable flag, so that's left to callers
// (e.g. pricing data).
package inventory

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Item is one non-empty inventory line.
type Item struct {
	Location string // where it sits: "Ear", "General1", "General1-Slot3", "Bank2", …
	Name     string
	ID       int
	Count    int
	Slots    int // bag capacity for containers, else 0
}

// Locate returns the path to a character's inventory dump in the EQ folder.
func Locate(eqDir, character string) string {
	return filepath.Join(eqDir, character+"-Inventory.txt")
}

// Load reads and parses a character's inventory dump from the EQ folder.
func Load(eqDir, character string) ([]Item, error) {
	f, err := os.Open(Locate(eqDir, character))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a tab-separated inventory dump, skipping the header row and empty
// slots. Lines whose ID column isn't numeric (the header, or anything malformed)
// are ignored, so it's tolerant of stray content.
func Parse(r io.Reader) ([]Item, error) {
	var items []Item
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		f := strings.Split(strings.TrimRight(sc.Text(), "\r\n"), "\t")
		if len(f) < 5 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(f[2]))
		if err != nil {
			continue // the "ID" header or a malformed line
		}
		name := strings.TrimSpace(f[1])
		if name == "" || name == "Empty" {
			continue // empty slot
		}
		count, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		slots, _ := strconv.Atoi(strings.TrimSpace(f[4]))
		items = append(items, Item{
			Location: strings.TrimSpace(f[0]),
			Name:     name,
			ID:       id,
			Count:    count,
			Slots:    slots,
		})
	}
	return items, sc.Err()
}

// Names returns the unique item names (preserving first-seen order) — the set to
// price-check.
func Names(items []Item) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if !seen[it.Name] {
			seen[it.Name] = true
			out = append(out, it.Name)
		}
	}
	return out
}
