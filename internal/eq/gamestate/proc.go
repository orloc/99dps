package gamestate

import (
	"slices"
	"strings"

	"99dps/internal/eq/eqclass"
)

// WeaponProc is a melee/combat proc that lands on the MOB you hit — not a
// self-buff like Clicky. A proc emits NO "You begin casting X" line, so the
// normal pending-cast → landing path never fires; instead we match the effect
// spell's cast_on_other emote, whose prefix is the struck mob's name (e.g.
// "a sand giant glances nervously about." → Tashani on "a sand giant").
//
// The effect is looked up in spells_us by name for its emote, duration, and
// detrimental/mez/pacify flags, so the resulting timer behaves exactly like a
// cast version (same refresh/instance rules, worn-off + slay expiry). Gated to
// the classes that can wield the item and to MinLevel (the proc's "at Level N"),
// which keeps a group-mate's own Tashani — same emote, no caster in the log —
// from being mis-credited as your proc for classes that can't even hold it.
type WeaponProc struct {
	Item     string          // the weapon (reference / docs)
	Effect   string          // proc spell — looked up in spells_us for emote + duration
	MinLevel int             // the proc only fires at/above this level
	Classes  []eqclass.Class // classes that can wield it (empty = any class)
}

// weaponProcs lists tracked combat procs. Unlike clickyRegistry (keyed by class,
// since each class has its own clicky items) a weapon is often usable by many
// classes, so this is a flat slice carrying its own Classes allow-list. ADD
// WEAPONS HERE — verify the proc spell and its level against the P99 wiki.
var weaponProcs = []WeaponProc{
	{
		// Journeyman's Walking Stick — procs Tashani (magic-resist debuff) at 35.
		// Widely usable; pure casters (Enc/Mag/Wiz/Nec) are excluded because they
		// can't wield it (and would cast Tashani themselves, handled by the normal
		// pending-cast path). P99 wiki: Journeyman's_Walking_Stick.
		Item: "Journeyman's Walking Stick", Effect: "Tashani", MinLevel: 35,
		Classes: []eqclass.Class{
			eqclass.ClassMonk, eqclass.ClassWarrior, eqclass.ClassCleric,
			eqclass.ClassPaladin, eqclass.ClassRanger, eqclass.ClassShadowKnight,
			eqclass.ClassDruid, eqclass.ClassBard, eqclass.ClassRogue, eqclass.ClassShaman,
		},
	},
}

// matchWeaponProcLocked starts a debuff timer on the struck mob when body is a
// registered proc's cast_on_other landing emote and the player can produce it
// (right class, high enough level). Caller holds the lock; only called when no
// real cast landed this line. Reports whether a proc matched.
func (t *Tracker) matchWeaponProcLocked(body string, at int64) bool {
	for _, p := range weaponProcs {
		if !classAllowed(t.class, p.Classes) || t.levelOrDefault() < p.MinLevel {
			continue
		}
		sp, ok := t.book.ByName(p.Effect)
		if !ok || sp.CastOnOther == "" || !strings.HasSuffix(body, sp.CastOnOther) {
			continue
		}
		target := strings.TrimSpace(strings.TrimSuffix(body, sp.CastOnOther))
		if target == "" || target == "You" {
			continue
		}
		dur := sp.DurationSeconds(t.levelOrDefault())
		if dur <= 0 {
			continue // instant/untimed effect — nothing to show
		}
		t.addOrRefreshTimerLocked(Timer{
			Spell:       sp.Name,
			Target:      target,
			Start:       at,
			Expiry:      at + int64(dur),
			Detrimental: sp.Detrimental,
			Mez:         sp.Mez,
			Pacify:      sp.Pacify,
			Proc:        true, // a weapon proc, not a cast — exempt from spell-validation
		}, at)
		return true
	}
	return false
}

// classAllowed reports whether class is in the allow-list (an empty list means
// any class). An unknown class never matches a non-empty list — we wait for the
// /who that reveals it, rather than risk crediting a proc to a class that can't
// wield the item.
func classAllowed(class eqclass.Class, allow []eqclass.Class) bool {
	return len(allow) == 0 || slices.Contains(allow, class)
}
