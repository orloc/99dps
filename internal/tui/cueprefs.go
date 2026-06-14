package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"99dps/internal/gamestate"
)

// cuePrefs is the persisted "which audio cues fire" choice. It stores only
// DEVIATIONS from each cue's built-in default (an override map), not the full
// set, for two reasons:
//   - Pluggable: a newly-registered skill or cue category isn't in the file, so
//     it simply takes its code-supplied default until the user touches it. Old
//     config files keep working as the cue catalog grows.
//   - Small + forward-compatible: the file lists only what the user changed.
//
// Cue IDs are stable strings, e.g. "cd.ready.Mend", "cd.half.Mend", "cc.mez",
// "buff.fade", "alert.resist".
type cuePrefs struct {
	Overrides map[string]bool `json:"overrides"`
}

// enabled reports whether a cue should fire: the user's override if set,
// otherwise the caller's built-in default (def).
func (c cuePrefs) enabled(id string, def bool) bool {
	if v, ok := c.Overrides[id]; ok {
		return v
	}
	return def
}

// toggle flips a cue relative to its current effective state. To keep the file
// minimal, an override that matches the default is dropped (back to "unset")
// rather than stored.
func (c *cuePrefs) toggle(id string, def bool) {
	next := !c.enabled(id, def)
	if next == def {
		delete(c.Overrides, id)
		return
	}
	if c.Overrides == nil {
		c.Overrides = map[string]bool{}
	}
	c.Overrides[id] = next
}

// Cue IDs. Per-skill cooldown cues are built from the skill name so the set
// grows with the cooldown catalog; the category cues are fixed constants. These
// strings are the contract between the settings UI (which toggles them) and
// announceCuesAt (which gates on them) — keep them in sync via these helpers.
const (
	cueCharm      = "cc.charm"    // charm broke (urgent)
	cueMez        = "cc.mez"      // a mez/enthrall fading
	cuePacify     = "cc.pacify"   // a pacify/lull fading
	cueBuffFade   = "buff.fade"   // a self/ally buff fading
	cueDebuffFade = "debuff.fade" // a debuff on a mob fading
	cueFeignFail  = "alert.feignfail"
	cueResist     = "alert.resist"
)

func cueCDReady(skill string) string { return "cd.ready." + skill }
func cueCDHalf(skill string) string  { return "cd.half." + skill }

// cueRow is one line in the Settings cue matrix: a non-selectable group header,
// or a toggle (a cue ID + its built-in default).
type cueRow struct {
	header bool
	label  string
	id     string // toggle rows only
	def    bool   // toggle rows only
}

// cueRows builds the cue matrix, data-driven off the cooldown catalog so new
// skills appear automatically. A cooldown's cues default ON only when it's long
// enough to be worth announcing (short reuses like Kick/Feign would be chatter);
// the user can still flip them on per skill.
func cueRows() []cueRow {
	rows := []cueRow{{header: true, label: "Cooldowns"}}
	for _, cd := range gamestate.CooldownCatalog() {
		def := cd.ReuseSec >= longCooldownSec
		rows = append(rows,
			cueRow{label: cd.Name + " ready", id: cueCDReady(cd.Name), def: def},
			cueRow{label: cd.Name + " halfway", id: cueCDHalf(cd.Name), def: def},
		)
	}
	rows = append(rows,
		cueRow{header: true, label: "Crowd control"},
		cueRow{label: "Charm break", id: cueCharm, def: true},
		cueRow{label: "Mez fading", id: cueMez, def: true},
		cueRow{label: "Pacify fading", id: cuePacify, def: true},
		cueRow{header: true, label: "Fades"},
		cueRow{label: "Buff fading", id: cueBuffFade, def: true},
		cueRow{label: "Debuff fading", id: cueDebuffFade, def: true},
		cueRow{header: true, label: "Alerts"},
		cueRow{label: "Feign failed", id: cueFeignFail, def: true},
		cueRow{label: "Resisted", id: cueResist, def: true},
	)
	return rows
}

// cueToggles returns just the selectable rows, in display order — the list the
// right-column cursor (cueSel) indexes into.
func cueToggles(rows []cueRow) []cueRow {
	out := make([]cueRow, 0, len(rows))
	for _, r := range rows {
		if !r.header {
			out = append(out, r)
		}
	}
	return out
}

func cuePrefsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "99dps", "cues.json"), nil
}

// loadCuePrefs reads the saved cue choices; a missing/invalid file yields the
// zero value (every cue at its built-in default).
func loadCuePrefs() cuePrefs {
	var c cuePrefs
	path, err := cuePrefsPath()
	if err != nil {
		return c
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

func saveCuePrefs(c cuePrefs) error {
	path, err := cuePrefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
