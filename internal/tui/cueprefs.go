package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"99dps/internal/eqclass"
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
	cueSpawnSoon  = "spawn.soon" // a tracked mob about to repop (~30s out)
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

// cueCaps is which cue categories a class can actually produce — drives which
// rows the Settings matrix shows. Cooldowns come from the (class-tagged) catalog;
// the rest are derived here.
type cueCaps struct {
	mez, charm, pacify bool // crowd control the class can cast
	buffs, debuffs     bool // casts beneficial buffs / mob debuffs worth a fade cue
	feign, resist      bool // has feign death / casts resistable spells
	all                bool // class unknown (pre-/who) → show everything
}

// cueCapsFor maps a class to its cue capabilities, read from its eqclass
// Profile. Crowd control / feign come straight off the profile; buff/debuff/
// resist follow the class category (casters + hybrids cast spells; pure melee
// don't). An unknown class (pre-/who) shows everything.
func cueCapsFor(class eqclass.Class) cueCaps {
	if class == eqclass.ClassUnknown {
		return cueCaps{mez: true, charm: true, pacify: true, buffs: true, debuffs: true, feign: true, resist: true, all: true}
	}
	p := eqclass.ProfileOf(class)
	c := cueCaps{mez: p.Mez, charm: p.Charm, pacify: p.Pacify, feign: p.Feign}
	if p.Category == eqclass.CatCaster || p.Category == eqclass.CatHybrid {
		c.buffs, c.debuffs, c.resist = true, true, true
	}
	return c
}

// cueRows builds the cue matrix for a class: only the categories that class can
// produce. Cooldown cues come from the catalog filtered to the class (all of them
// while the class is unknown). A cooldown's cues default ON only when it's long
// enough to be worth announcing (short reuses like Kick/Feign would be chatter).
func cueRows(class eqclass.Class) []cueRow {
	caps := cueCapsFor(class)
	var rows []cueRow
	add := func(rs ...cueRow) { rows = append(rows, rs...) }
	section := func(label string, rs []cueRow) {
		if len(rs) > 0 {
			add(cueRow{header: true, label: label})
			add(rs...)
		}
	}

	var cds []cueRow
	for _, cd := range gamestate.CooldownCatalog() {
		if !caps.all && cd.Class != class {
			continue
		}
		def := cd.ReuseSec >= longCooldownSec
		cds = append(cds,
			cueRow{label: cd.Name + " ready", id: cueCDReady(cd.Name), def: def},
			cueRow{label: cd.Name + " halfway", id: cueCDHalf(cd.Name), def: def},
		)
	}
	section("Cooldowns", cds)

	var cc []cueRow
	if caps.charm {
		cc = append(cc, cueRow{label: "Charm break", id: cueCharm, def: true})
	}
	if caps.mez {
		cc = append(cc, cueRow{label: "Mez fading", id: cueMez, def: true})
	}
	if caps.pacify {
		cc = append(cc, cueRow{label: "Pacify fading", id: cuePacify, def: true})
	}
	section("Crowd control", cc)

	var fades []cueRow
	if caps.buffs {
		fades = append(fades, cueRow{label: "Buff fading", id: cueBuffFade, def: true})
	}
	if caps.debuffs {
		fades = append(fades, cueRow{label: "Debuff fading", id: cueDebuffFade, def: true})
	}
	section("Fades", fades)

	var alerts []cueRow
	if caps.feign {
		alerts = append(alerts, cueRow{label: "Feign failed", id: cueFeignFail, def: true})
	}
	if caps.resist {
		alerts = append(alerts, cueRow{label: "Resisted", id: cueResist, def: true})
	}
	section("Alerts", alerts)

	// the mob tracker is class-agnostic — everyone kills things.
	section("Mob Tracker", []cueRow{{label: "Spawn soon (~30s)", id: cueSpawnSoon, def: true}})
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

// loadCuePrefs reads the legacy cue-choice file; a missing/invalid file yields
// the zero value (every cue at its default). Retained as a migration source —
// current writes go through the consolidated per-character store (see store.go).
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
