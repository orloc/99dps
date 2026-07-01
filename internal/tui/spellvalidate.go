package tui

import (
	"fmt"
	"strings"
	"time"

	"99dps/internal/spellwiki"
)

// Spell-cast validation against the bundled, OFFLINE P99 spell set
// (internal/spellwiki, generated from the wiki's Category:Spells via `go
// generate`). The client's spells_us.txt is a superset — the server hides spells —
// so a cast can resolve to a wrong/non-existent spell. We flag (never silently
// change) any active spell the bundled set doesn't confirm for the player's class.
// The check is a synchronous map lookup: no per-cast network call, no tea.Cmd.

// suspectActiveSpells lists the distinct active-timer spells the bundled P99 set
// flags as suspect — not on the server, or not castable by the detected class — in
// the tracker's active order (by soonest expiry). Empty when the class is unknown
// (castability can't be judged) or the tracker is nil.
func (m *Model) suspectActiveSpells() []string {
	if m.tracker == nil {
		return nil
	}
	class := string(m.tracker.Class())
	if class == "" {
		return nil // need the class to judge castability — don't flag blind
	}
	seen := map[string]bool{}
	var out []string
	for _, tm := range m.tracker.Active(time.Now().Unix()) {
		if tm.Spell == "" || tm.Estimated || tm.Proc || seen[tm.Spell] {
			// Estimated = an incoming debuff (a category); Proc = a weapon proc —
			// neither is a spell the player cast, so castability can't be judged.
			continue
		}
		seen[tm.Spell] = true
		if found, classOK := spellwiki.Validate(tm.Spell, class); !found || !classOK {
			out = append(out, tm.Spell)
		}
	}
	return out
}

// spellWarning renders the suspect-spell flag as a dim red header + one bullet per
// spell (a single comma-joined line ran off the panel), each clipped to w, or ""
// when nothing is flagged. Appended under the class panel; the viewport scrolls if
// the list is long.
func (m *Model) spellWarning(w int) string {
	sus := m.suspectActiveSpells()
	if len(sus) == 0 || w < 6 {
		return ""
	}
	red := themes[m.theme].fg("#e0564e")
	// "unconfirmed" covers both suspect cases — no wiki page, OR a page that doesn't
	// list this class (so "not on the wiki" would be inaccurate for the latter).
	lines := []string{red.Render(truncate("⚠ unconfirmed on P99 wiki:", w))}
	for _, s := range sus {
		lines = append(lines, red.Render(truncate("  • "+s, w)))
	}
	// provenance: which bundled set judged these, so the user can gauge how current
	// the authority is (the offline dataset is regenerated occasionally, not live).
	dim := themes[m.theme].fg("#8a8a8a")
	prov := fmt.Sprintf("P99 set: %d spells · %s", spellwiki.Count(), spellwiki.Generated())
	lines = append(lines, dim.Render(truncate(prov, w)))
	return strings.Join(lines, "\n")
}
