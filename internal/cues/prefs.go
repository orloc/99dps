package cues

import (
	"encoding/json"
	"os"
	"path/filepath"

	"99dps/internal/eq/eqclass"
	"99dps/internal/eq/gamestate"
)

// Prefs is the persisted "which audio cues fire" choice. It stores only
// DEVIATIONS from each cue's built-in default (an override map), not the full
// set, for two reasons:
//   - Pluggable: a newly-registered skill or cue category isn't in the file, so
//     it simply takes its code-supplied default until the user touches it. Old
//     config files keep working as the cue catalog grows.
//   - Small + forward-compatible: the file lists only what the user changed.
//
// Cue IDs are stable strings, e.g. "cd.ready.Mend", "cd.half.Mend", "cc.mez",
// "buff.fade", "alert.resist".
type Prefs struct {
	Overrides map[string]bool `json:"overrides"`
	// Style knobs (see style.go). Their persisted source of truth is the per-
	// character store (tui.charSettings); they ride along on Prefs so Announce can
	// read everything from one value. Zero values = today's behavior.
	Timing  Timing  `json:"timing,omitempty"`
	Chatter Chatter `json:"chatter,omitempty"`
}

// Enabled reports whether a cue should fire: the user's override if set,
// otherwise the caller's built-in default (def).
func (c Prefs) Enabled(id string, def bool) bool {
	if v, ok := c.Overrides[id]; ok {
		return v
	}
	return def
}

// Toggle flips a cue relative to its current effective state. To keep the file
// minimal, an override that matches the default is dropped (back to "unset")
// rather than stored.
func (c *Prefs) Toggle(id string, def bool) {
	next := !c.Enabled(id, def)
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
// Announce (which gates on them) — keep them in sync via these helpers.
const (
	cueCharm      = "cc.charm"    // charm broke (urgent)
	cueMez        = "cc.mez"      // a mez/enthrall fading
	cuePacify     = "cc.pacify"   // a pacify/lull fading
	cueBuffFade   = "buff.fade"   // a self/ally buff fading
	cueDebuffFade = "debuff.fade" // a debuff on a mob fading
	cueFeignFail  = "alert.feignfail"
	cueResist     = "alert.resist"
	cueSpawnSoon  = "spawn.soon" // a tracked mob about to repop (~30s out)
	cueCHGap      = "raid.chgap" // a Complete Heal chain broke (urgent)
)

func cueCDReady(skill string) string { return "cd.ready." + skill }
func cueCDHalf(skill string) string  { return "cd.half." + skill }

// Row is one line in the Settings cue matrix: a non-selectable group header,
// or a toggle (a cue ID + its built-in default).
type Row struct {
	Header bool
	Label  string
	ID     string // toggle rows only
	Def    bool   // toggle rows only
}

// caps is which cue categories a class can actually produce — drives which rows
// the Settings matrix shows. Cooldowns come from the (class-tagged) catalog; the
// rest are derived here.
type caps struct {
	mez, charm, pacify bool // crowd control the class can cast
	buffs, debuffs     bool // casts beneficial buffs / mob debuffs worth a fade cue
	feign, resist      bool // has feign death / casts resistable spells
	all                bool // class unknown (pre-/who) → show everything
}

// capsFor maps a class to its cue capabilities, read from its eqclass Profile.
// Crowd control / feign come straight off the profile; buff/debuff/resist follow
// the class category (casters + hybrids cast spells; pure melee don't). An
// unknown class (pre-/who) shows everything.
func capsFor(class eqclass.Class) caps {
	if class == eqclass.ClassUnknown {
		return caps{mez: true, charm: true, pacify: true, buffs: true, debuffs: true, feign: true, resist: true, all: true}
	}
	p := eqclass.ProfileOf(class)
	c := caps{mez: p.Mez, charm: p.Charm, pacify: p.Pacify, feign: p.Feign}
	if p.Category == eqclass.CatCaster || p.Category == eqclass.CatHybrid {
		c.buffs, c.debuffs, c.resist = true, true, true
	}
	return c
}

// Rows builds the cue matrix for a class: only the categories that class can
// produce. Cooldown cues come from the catalog filtered to the class (all of them
// while the class is unknown). A cooldown's cues default ON only when it's long
// enough to be worth announcing (short reuses like Kick/Feign would be chatter).
func Rows(class eqclass.Class) []Row {
	caps := capsFor(class)
	var rows []Row
	add := func(rs ...Row) { rows = append(rows, rs...) }
	section := func(label string, rs []Row) {
		if len(rs) > 0 {
			add(Row{Header: true, Label: label})
			add(rs...)
		}
	}

	var cds []Row
	for _, cd := range gamestate.CooldownCatalog() {
		if cd.AnyClass {
			continue // class-agnostic action skills (Taunt/Evade/Pick Pocket): tracked
			// in the panel for whoever uses them, but not a per-class cue toggle, and
			// their reuse is too short to cue anyway (well under longCooldownSec).
		}
		if !caps.all && cd.Class != class {
			continue
		}
		def := cd.ReuseSec >= longCooldownSec
		cds = append(cds,
			Row{Label: cd.Name + " ready", ID: cueCDReady(cd.Name), Def: def},
			Row{Label: cd.Name + " halfway", ID: cueCDHalf(cd.Name), Def: def},
		)
	}
	section("Cooldowns", cds)

	var cc []Row
	if caps.charm {
		cc = append(cc, Row{Label: "Charm break", ID: cueCharm, Def: true})
	}
	if caps.mez {
		cc = append(cc, Row{Label: "Mez fading", ID: cueMez, Def: true})
	}
	if caps.pacify {
		cc = append(cc, Row{Label: "Pacify fading", ID: cuePacify, Def: true})
	}
	section("Crowd control", cc)

	var fades []Row
	if caps.buffs {
		fades = append(fades, Row{Label: "Buff fading", ID: cueBuffFade, Def: true})
	}
	if caps.debuffs {
		fades = append(fades, Row{Label: "Debuff fading", ID: cueDebuffFade, Def: true})
	}
	section("Fades", fades)

	var alerts []Row
	if caps.feign {
		alerts = append(alerts, Row{Label: "Feign failed", ID: cueFeignFail, Def: true})
	}
	if caps.resist {
		alerts = append(alerts, Row{Label: "Resisted", ID: cueResist, Def: true})
	}
	section("Alerts", alerts)

	// the mob tracker is class-agnostic — everyone kills things.
	section("Mob Tracker", []Row{{Label: "Spawn soon (~30s)", ID: cueSpawnSoon, Def: true}})
	// raid cues are class-agnostic too — anyone watching the CH chain wants the alert.
	section("Raid", []Row{{Label: "CH chain gap", ID: cueCHGap, Def: true}})
	return rows
}

// Toggles returns just the selectable rows, in display order — the list the
// right-column cursor (cueSel) indexes into.
func Toggles(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if !r.Header {
			out = append(out, r)
		}
	}
	return out
}

func prefsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "99dps", "cues.json"), nil
}

// LoadPrefs reads the legacy cue-choice file; a missing/invalid file yields the
// zero value (every cue at its default). Retained as a migration source —
// current writes go through the consolidated per-character store (see tui/store.go).
func LoadPrefs() Prefs {
	var c Prefs
	path, err := prefsPath()
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
