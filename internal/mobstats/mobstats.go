// Package mobstats keeps a lightweight, app-wide index of how hard each NPC hits
// in melee — running average (and max) damage per mob name, accumulated from the
// live combat stream. It's a shared read-only-ish data structure: the parse
// goroutine feeds it via Record as the log streams, and any reader (the UI, the
// rune-absorb estimate, …) can query it concurrently.
//
// EQ logs never name the caster on spell damage and never report a rune's
// absorbed amount, so a mob's *typical melee hit* is the best proxy we have for
// "how fast is that rune burning down" — and it's keyed by mob, not by who got
// hit, so it works even when the player isn't the one being hit (an enchanter
// whose charmed pet is tanking).
package mobstats

import (
	"strings"
	"sync"
)

// hitAgg is one mob's running melee tally.
type hitAgg struct {
	n     int
	total int
	max   int
}

// Index maps a mob name to its melee-hit tally. Safe for concurrent use.
type Index struct {
	mu sync.RWMutex
	m  map[string]hitAgg
}

// New builds an empty index.
func New() *Index { return &Index{m: make(map[string]hitAgg)} }

// normalize keys a mob by a case-insensitive, trimmed name so the article-bearing
// damage-line name ("a sand giant") matches a query consistently.
func normalize(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Record folds one melee hit (dealer + damage) into the index. No-op on a nil
// index, empty name, or non-positive damage. Cheap enough to call per damage line
// from the parse goroutine.
func (ix *Index) Record(name string, dmg int) {
	if ix == nil || dmg <= 0 {
		return
	}
	k := normalize(name)
	if k == "" {
		return
	}
	ix.mu.Lock()
	a := ix.m[k]
	a.n++
	a.total += dmg
	if dmg > a.max {
		a.max = dmg
	}
	ix.m[k] = a
	ix.mu.Unlock()
}

// AvgHit returns a mob's mean melee hit and whether any hits were seen.
func (ix *Index) AvgHit(name string) (int, bool) {
	if ix == nil {
		return 0, false
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	a, ok := ix.m[normalize(name)]
	if !ok || a.n == 0 {
		return 0, false
	}
	return a.total / a.n, true
}

// MaxHit returns a mob's biggest seen melee hit.
func (ix *Index) MaxHit(name string) (int, bool) {
	if ix == nil {
		return 0, false
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	a, ok := ix.m[normalize(name)]
	return a.max, ok && a.n > 0
}

// Overall returns the mean melee hit across every mob seen — a fallback when a
// specific mob hasn't been observed yet.
func (ix *Index) Overall() (int, bool) {
	if ix == nil {
		return 0, false
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	n, total := 0, 0
	for _, a := range ix.m {
		n += a.n
		total += a.total
	}
	if n == 0 {
		return 0, false
	}
	return total / n, true
}

// Len is the number of distinct mobs seen.
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.m)
}

// Clear drops all tallies (a character switch).
func (ix *Index) Clear() {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	ix.m = make(map[string]hitAgg)
	ix.mu.Unlock()
}
