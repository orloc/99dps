// Package mobstats keeps a lightweight, app-wide index of how hard each NPC hits
// in melee — a rolling average (over a recent window) of damage per mob name,
// accumulated from the live combat stream. It's a shared data structure: the
// parse goroutine feeds it via Record as the log streams, and any reader (the
// rune-absorb estimate, …) can query it concurrently.
//
// EQ logs never report a rune's absorbed amount, so a mob's *typical recent melee
// hit* is the best proxy for how fast a rune is burning down — and it's keyed by
// mob, not by who got hit, so it works even when the player isn't the one being
// hit (an enchanter whose charmed pet is tanking). The rolling window keeps the
// figure specific: only the last WindowSec of that mob's hits count.
package mobstats

import (
	"sort"
	"strings"
	"sync"
)

// WindowSec is how far back hits count toward a mob's running average (30 min).
const WindowSec = 30 * 60

// sample is one recorded melee hit at a log time.
type sample struct {
	at  int64
	dmg int
}

// Index maps a mob name to its recent melee hits. Safe for concurrent use.
type Index struct {
	mu sync.RWMutex
	m  map[string][]sample
}

// NewIndex builds an empty index.
func NewIndex() *Index { return &Index{m: make(map[string][]sample)} }

// normalize keys a mob case-insensitively so the article-bearing damage-line name
// ("a sand giant") matches a query consistently.
func normalize(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Record folds one melee hit (dealer + damage at log time `at`) into the index,
// dropping that mob's samples older than the window. No-op on a nil index, empty
// name, or non-positive damage.
func (ix *Index) Record(name string, dmg int, at int64) {
	if ix == nil || dmg <= 0 {
		return
	}
	k := normalize(name)
	if k == "" {
		return
	}
	ix.mu.Lock()
	s := append(ix.m[k], sample{at: at, dmg: dmg})
	ix.m[k] = pruneOlder(s, at-WindowSec)
	ix.mu.Unlock()
}

// pruneOlder drops the leading samples older than cutoff (log time is monotonic,
// so old samples are a prefix).
func pruneOlder(s []sample, cutoff int64) []sample {
	i := 0
	for i < len(s) && s[i].at < cutoff {
		i++
	}
	return s[i:]
}

// AvgHit returns a mob's mean melee hit over the last window ending at `now`, and
// whether any qualifying hits were seen.
func (ix *Index) AvgHit(name string, now int64) (int, bool) {
	if ix == nil {
		return 0, false
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return avgIn(ix.m[normalize(name)], now-WindowSec)
}

// Overall returns the mean melee hit across every mob seen in the window — a
// fallback when the specific attacker hasn't been observed.
func (ix *Index) Overall(now int64) (int, bool) {
	if ix == nil {
		return 0, false
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	n, total := 0, 0
	for _, s := range ix.m {
		for _, smp := range s {
			if smp.at >= now-WindowSec {
				n++
				total += smp.dmg
			}
		}
	}
	if n == 0 {
		return 0, false
	}
	return total / n, true
}

// avgIn averages the samples at/after cutoff.
func avgIn(s []sample, cutoff int64) (int, bool) {
	n, total := 0, 0
	for _, smp := range s {
		if smp.at >= cutoff {
			n++
			total += smp.dmg
		}
	}
	if n == 0 {
		return 0, false
	}
	return total / n, true
}

// Seen reports the distinct mobs with a hit in the window (sorted) — handy for
// debugging / future UI.
func (ix *Index) Seen(now int64) []string {
	if ix == nil {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []string
	for k, s := range ix.m {
		if _, ok := avgIn(s, now-WindowSec); ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Clear drops all tallies (a character switch).
func (ix *Index) Clear() {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	ix.m = make(map[string][]sample)
	ix.mu.Unlock()
}
