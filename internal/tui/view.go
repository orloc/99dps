package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// banner is the full-width header plaque (dark text on muted gold, like the EQ
// zone plaques) so it clearly reads as the header: app · character · class-level
// · zone on the left, with the zone kills/hr pushed to the right.
func (m Model) banner(th theme, w int) string {
	bar := lipgloss.NewStyle().Background(lipgloss.Color(th.accentLo)).Foreground(lipgloss.Color(th.bg))
	sep := bar.Render("  ·  ")

	bits := []string{bar.Bold(true).Render("✦ 99dps"), bar.Bold(true).Render(m.character)}
	var right string
	if m.tracker != nil {
		if lv := m.tracker.Level(); lv > 0 {
			bits = append(bits, bar.Render(fmt.Sprintf("L%d %s", lv, m.tracker.Class())))
		}
		if z := m.tracker.Zone(); z != "" {
			bits = append(bits, bar.Bold(true).Render("◆ "+z))
		}
		now := time.Now().Unix()
		var rparts []string
		if secs, ok := m.tracker.TimeInGroup(now); ok {
			rparts = append(rparts, compactDur(secs)+" grouped")
		}
		if secs, ok := m.tracker.TimeInZone(now); ok {
			rparts = append(rparts, compactDur(secs)+" in zone")
		}
		if k, ph, d := m.tracker.ZoneKillStats(now); k > 0 || d > 0 {
			if k > 0 {
				rparts = append(rparts, fmt.Sprintf("%d kills · %d/hr", k, ph))
			}
			if d > 0 {
				rparts = append(rparts, fmt.Sprintf("%d deaths", d))
			}
		}
		if len(rparts) > 0 {
			right = bar.Bold(true).Render(strings.Join(rparts, " · "))
		}
	}
	left := " " + strings.Join(bits, sep)

	line := left
	if right != "" {
		if gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 1; gap >= 2 {
			line = left + strings.Repeat(" ", gap) + right + " "
		}
	}
	return bar.Width(w).MaxWidth(w).Render(line)
}

// scrollHint returns a tiny arrow for a card title when its viewport has content
// off-screen: ▾ (more below), ▴ (more above), ↕ (both), "" (it all fits).
func scrollHint(vp viewport.Model) string {
	up, down := !vp.AtTop(), !vp.AtBottom()
	switch {
	case up && down:
		return " ↕"
	case down:
		return " ▾"
	case up:
		return " ▴"
	}
	return ""
}

func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	th := themes[m.theme]

	innerW := m.w - 2 // content width inside the outer Padding(1,1)

	// first-run audio-cue setup takes over the whole window.
	if m.screen == screenSetup {
		body := lipgloss.JoinVertical(lipgloss.Left, m.banner(th, innerW), "", m.setupView(th, innerW))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(body)
	}

	banner := m.banner(th, innerW)
	tabbar := m.tabBar(th, innerW)

	// the Sessions tab: a wide scrollable stats table beside a DPS breakdown of
	// the highlighted session.
	if m.screen == screenSessions {
		tableW, breakW, fullH := m.sessionsLayout()
		breakTitle := "Damage"
		if sel := m.effectiveSel(); sel >= 0 && sel < len(m.sessions) {
			breakTitle = "Damage — " + truncate(m.sessions[sel].Name(), breakW-12)
		}
		// sticky header sits above the scrolling body inside the table card.
		tableBody := lipgloss.JoinVertical(lipgloss.Left, m.sessHeader, m.vpSessTable.View())
		grid := lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, tableW, fullH, "Sessions"+scrollHint(m.vpSessTable), tableBody), " ",
			card(th, breakW, fullH, breakTitle+scrollHint(m.vpSessBreak), m.vpSessBreak.View()))
		full := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, grid, m.footer(th, innerW))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(full)
	}

	// the Settings tab renders its own body under the banner + tab bar.
	if m.screen == screenSettings {
		body := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, m.settingsView(th, innerW))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(body)
	}

	// the Tools tab: a 3-column home for little helper widgets.
	if m.screen == screenTools {
		body := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, m.toolsView(th))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(body)
	}

	// the Map tab: a braille render of the current zone, player marked from /loc.
	if m.screen == screenMap {
		body := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, m.mapView(th, innerW, max(m.h-4, 4)))
		return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
			Width(m.w).Height(m.h).Padding(1, 1).Render(body)
	}

	ld := m.layout()

	dmgTitle := "Damage"
	if sel := m.effectiveSel(); sel >= 0 {
		dmgTitle = "Damage — " + truncate(m.sessions[sel].Name(), ld.rightW-12)
	}

	// bottom row: the class-aware panel + Mob Tracker — caster/hybrid classes get a
	// third, dedicated Enemy column (CC + debuffs on mobs). Every panel renders from
	// its own viewport, so each scrolls independently — a scroll hint (▾/▴/↕) in the
	// title signals when there's more off-screen.
	classTitle := classPanelTitle(m.tracker, ld.enemy) + scrollHint(m.vpClass)
	midTitle := "Enemy" // CC + debuffs on mobs (caster/hybrid)
	if m.middleIsBuffs() {
		midTitle = "Buffs" // self-buff / clicky timers (melee)
	}
	// the mob column: Mob Tracker on top, optionally a small class box beneath it.
	// The count of tracked mobs sits in the title's right corner (one mobTargets
	// entry per repop row).
	mobTitle := "Mob Tracker" + scrollHint(m.vpMob)
	if n := len(m.mobTargets); n > 0 {
		mobTitle = titleWithCorner(mobTitle, fmt.Sprintf("%d mobs", n), ld.mobW)
	}
	mobCol := card(th, ld.mobW, ld.mobH, mobTitle, m.vpMob.View())
	if ld.classBox {
		box := card(th, ld.mobW, ld.boxH, m.classBoxTitle(), m.classBoxView(ld.mobW, ld.boxH))
		mobCol = lipgloss.JoinVertical(lipgloss.Left, mobCol, "", box)
	}
	var bottom string
	if ld.enemy {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, ld.classW, ld.botH, classTitle, m.vpClass.View()), " ",
			card(th, ld.enemyW, ld.botH, midTitle+scrollHint(m.vpEnemy), m.vpEnemy.View()), " ",
			mobCol)
	} else {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			card(th, ld.classW, ld.botH, classTitle, m.vpClass.View()), " ",
			mobCol)
	}
	// top-right: the dealer meter beside its Specials / Avoidance side column.
	// The side card has no title — its SPECIALS / AVOIDANCE section headers label
	// it. On a narrow terminal there's no side column; the meter takes the width.
	// the canni dance meter is pinned beneath the scrollable dealer list (its
	// height was reserved out of the viewport in refresh), so it's always visible.
	// top row: whichever of the Damage meter / Offense·Defense the user has on
	// (widths are 0 when a box is off; both off → no top row).
	var topCells []string
	if ld.dmgW > 0 {
		dmgBody := m.vpDamage.View()
		if m.canniBlock != "" {
			dmgBody = lipgloss.JoinVertical(lipgloss.Left, dmgBody, m.canniBlock)
		}
		topCells = append(topCells, card(th, ld.dmgW, ld.dmgH, dmgTitle+scrollHint(m.vpDamage), dmgBody))
	}
	if ld.extrasW > 0 {
		topCells = append(topCells, card(th, ld.extrasW, ld.dmgH, "Offense · Defense"+scrollHint(m.vpExtras), m.vpExtras.View()))
	}

	var grid string
	if len(topCells) > 0 {
		top := lipgloss.JoinHorizontal(lipgloss.Top, sepJoin(topCells, " ")...)
		grid = lipgloss.JoinVertical(lipgloss.Left, top, bottom)
	} else {
		grid = bottom // both top boxes off — the bottom row fills the height
	}
	full := lipgloss.JoinVertical(lipgloss.Left, banner, tabbar, grid, m.footer(th, innerW))
	return lipgloss.NewStyle().Background(lipgloss.Color(th.bg)).Foreground(lipgloss.Color(th.text)).
		Width(m.w).Height(m.h).Padding(1, 1).Render(full)
}

// footer is the bottom line: the open repop editor's prompt, else a transient
// status flash, else the keybinding help + audio/data-source state.
func (m Model) footer(th theme, w int) string {
	switch {
	case m.editing:
		return th.fg(th.accent).Bold(true).Render(truncate(
			fmt.Sprintf("set repop for %s (m:ss): %s_   [enter] save   [esc] cancel", m.editMob, m.editBuf), w))
	case m.status != "" && time.Now().Unix()-m.statusAt <= statusGraceSec:
		return th.fg(th.accent).Bold(true).Render(truncate(m.status, w))
	}
	audio := "audio off"
	if m.ttsOn {
		audio = "♪ audio on"
	} else if m.speaker == nil || !m.speaker.Available() {
		audio = "audio n/a"
	}
	keys := "[tab] screen  [t] theme  [a] " + audio + "  [↑↓/click] fight  [end] live  [wheel] scroll  [bksp] clear  [q] quit"
	if m.spellInfo != "" {
		keys = m.spellInfo + "  ·  " + keys
	}
	return th.fg(th.dim).Render(truncate(keys, w))
}
