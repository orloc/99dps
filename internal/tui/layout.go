package tui

import "99dps/internal/eq/eqclass"

// layout holds the computed panel rectangles for the current window size. The
// bottom row gets a dedicated "Enemy" column (CC + debuffs) for caster/hybrid
// classes when there's room; widths are computed here and shared by sizing +
// render.
type layout struct {
	rightW               int // full content width (the sidebar was removed)
	dmgH, botH           int
	dmgW, extrasW        int // top-right split: dealer meter | Specials/Avoidance
	classW, enemyW, mobW int // bottom row: class panel | [Enemy] | mob tracker
	mobH, boxH           int // mob column split: Mob Tracker (mobH) over the class box (boxH)
	classBox             bool
	enemy                bool
	areaH                int
}

// gridX is the screen X of the meter's first content column (just inside the
// outer padding). The Sessions sidebar used to sit left of it; now the meter
// spans the full width from here.
const gridX = 1

// enemyColMinW is the right-column width below which the bottom row drops the
// Enemy column (falls back to class | mob, with CC+debuffs back in the class panel).
const enemyColMinW = 60

// gridTop is the Y of the first grid row: outer padding (1) + banner (1) + the
// tab bar (1). Every mouse hit-tester derives from it so the tab row can't drift
// the click math.
const gridTop = 3

func (m Model) layout() layout {
	innerW := m.w - 2
	areaH := m.h - 5 // outer padding + banner + tab bar + footer
	if areaH < 6 {
		areaH = 6
	}
	rightW := innerW // the meter spans the full width now (no Sessions sidebar)

	// the top row (Damage meter + Offense·Defense) is user-toggleable. Its height
	// scales with the visible boxes: Full gets the bulk (52%), Compact a smaller
	// slice (35%) so the bottom row (class/enemy/mob) grows, and both-Off collapses
	// the top entirely.
	showDmg := m.layoutPrefs.Damage != panelOff
	showOD := m.layoutPrefs.OffDef != panelOff
	topFrac := 0
	switch {
	case m.layoutPrefs.Damage == panelFull || m.layoutPrefs.OffDef == panelFull:
		topFrac = 52
	case showDmg || showOD: // at least one box, all visible ones Compact
		topFrac = 35
	}
	dmgH, botH := 0, areaH
	if topFrac > 0 {
		dmgH = areaH * topFrac / 100
		if dmgH < 5 {
			dmgH = 5
		}
		botH = areaH - dmgH - 1
	}

	// every class gets a middle column when it fits: casters/hybrids → Enemy
	// (CC + debuffs on mobs), melee → Buffs (self-buff / clicky timers). The same
	// timerColumn code feeds both, just different sections. A too-narrow window
	// drops it and the content folds back into the class panel.
	enemy := m.tracker != nil && rightW >= enemyColMinW

	ld := layout{
		rightW: rightW,
		dmgH:   dmgH, botH: botH, areaH: areaH, enemy: enemy,
	}
	// split the top row among the visible boxes: both → dealer meter + side
	// column (when wide enough to fit beside it); one → it takes the full width;
	// neither → no top row (dmgH was already zeroed above).
	switch {
	case showDmg && showOD && rightW >= 46:
		ld.extrasW = min(rightW*38/100, 46)
		ld.dmgW = rightW - ld.extrasW - 1
	case showDmg:
		ld.dmgW, ld.extrasW = rightW, 0 // narrow, or OffDef off → meter full width
	case showOD:
		ld.dmgW, ld.extrasW = 0, rightW // only Offense·Defense
	default:
		ld.dmgW, ld.extrasW = 0, 0
	}
	if enemy {
		ld.classW = rightW * 38 / 100
		ld.enemyW = rightW * 30 / 100
		ld.mobW = rightW - ld.classW - ld.enemyW - 2
	} else {
		ld.classW = rightW / 2
		ld.mobW = rightW - ld.classW - 1
	}
	// the small class box sits under the Mob Tracker, splitting that column's
	// height (it stays the taller of the two). Drops out if the column is too short.
	ld.mobH = ld.botH
	if m.layoutPrefs.ClassBox {
		bh := min(max(ld.botH/3, 6), 9) // ≥6 so the avg-hit line + two shields fit
		if ld.botH-bh-1 >= 6 {          // keep the Mob Tracker usefully tall
			ld.classBox = true
			ld.boxH = bh
			ld.mobH = ld.botH - bh - 1
		}
	}
	return ld
}

// middleIsBuffs reports whether the bottom-row middle column should hold self
// BUFFS (pure melee) rather than the Enemy CC+debuffs (caster/hybrid).
func (m Model) middleIsBuffs() bool {
	return m.tracker != nil && m.tracker.Category() == eqclass.CatMelee
}

// cardInner returns the body width/height inside a card of total size w×h
// (border 2 + padding 2 horizontally; border 2 + title line 1 vertically).
func cardInner(w, h int) (int, int) { return w - 4, h - 3 }

// sepJoin interleaves sep between cells (for lipgloss.JoinHorizontal).
func sepJoin(cells []string, sep string) []string {
	if len(cells) == 0 {
		return nil
	}
	out := make([]string, 0, len(cells)*2-1)
	for i, c := range cells {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, c)
	}
	return out
}
