package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"99dps/internal/eq/gamestate"
)

// CH-chain monitor colors (fixed, theme-independent — a gap is a gap).
const (
	chOK    = "#7ee787" // healthy / chaining
	chWarn  = "#e3b341" // cadence / soft caution
	chAlert = "#f85149" // active gap — the chain is breaking
)

// chchainView renders the Complete Heal chain monitor. Multiple tanks can be
// chained at once (the real raid case), so every active chain gets a status
// line; the busiest also gets a full cleric roster + recent gaps. Finished pulls
// land in a post-raid summary.
func (m Model) chchainBody(th theme, w int) (title, body string) {
	now := time.Now().Unix()
	var active []gamestate.CHChainStats
	var hist []gamestate.CHChainSummary
	if m.tracker != nil {
		active = m.tracker.CHActiveChains(now)
		hist = m.tracker.CHChainHistory()
	}

	var b strings.Builder

	if len(active) == 0 && len(hist) == 0 {
		b.WriteString(th.fg(th.dim).Render(truncate(
			"Waiting for CH-chain call-outs — clerics announcing \"KCH - <target> - <slot>\" in guild chat.", w)))
		b.WriteString("\n\n")
		b.WriteString(th.fg(th.dim).Render(truncate(
			"Tracks every tank's chain at once; cadence + gaps are derived live per chain.", w)))
		return "CH Chain", b.String()
	}

	title = "CH Chain"
	if len(active) > 0 {
		title = fmt.Sprintf("CH Chain — %d active", len(active))
	}

	for i, st := range active {
		// status line
		cad := "—"
		if st.Cadence > 0 {
			cad = trimZero(st.Cadence) + "s"
		}
		switch {
		case st.InGap:
			line := fmt.Sprintf("⚠ %s CHAIN GAP — %ds since last CH (cadence %s)", st.Target, st.SinceLast, cad)
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(chAlert)).Render(truncate(line, w)))
		default:
			line := fmt.Sprintf("● %s — %ds since CH · cadence %s", st.Target, st.SinceLast, cad)
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(chOK)).Render(truncate(line, w)))
		}
		b.WriteString("\n")
		stats := fmt.Sprintf("   %d clerics · %d CHs · gaps %d (worst %ds) · GAP-calls %d · subs %d",
			st.Clerics, st.Casts, st.Gaps, st.WorstGap, st.GapCalls, st.Replacements)
		b.WriteString(th.fg(th.dim).Render(truncate(stats, w)))
		b.WriteString("\n")

		if i == 0 {
			// the busiest chain gets the full roster + recent gaps.
			b.WriteString(rosterBlock(th, st, w))
			if len(st.RecentGaps) > 0 {
				b.WriteString(th.fg(th.accent).Render(truncate("  recent gaps", w)))
				b.WriteString("\n")
				for _, g := range st.RecentGaps {
					clk := time.Unix(g.At, 0).Format("15:04:05")
					b.WriteString(th.fg(chWarn).Render(truncate(
						fmt.Sprintf("    %s  %2ds gap (after slot %s)", clk, g.Seconds, g.AfterSlot), w)))
					b.WriteString("\n")
				}
			}
		}
		b.WriteString("\n")
	}

	// completed pulls (post-raid summary)
	if done := completedOnly(hist, active); len(done) > 0 {
		b.WriteString(th.fg(th.accent).Render(truncate("COMPLETED PULLS", w)))
		b.WriteString("\n")
		for i := len(done) - 1; i >= 0 && i >= len(done)-8; i-- {
			s := done[i]
			cad := "—"
			if s.Cadence > 0 {
				cad = trimZero(s.Cadence) + "s"
			}
			line := fmt.Sprintf("%-14s %2dc · %3d CHs · %s · cad %s · %d gaps (worst %ds) · %d GAP · %d subs",
				truncate(s.Target, 14), s.Clerics, s.Casts, mmss(s.End-s.Start), cad, s.Gaps, s.WorstGap, s.GapCalls, s.Replacements)
			col := th.fg(th.dim)
			if s.Gaps > 0 || s.GapCalls > 0 {
				col = th.fg(chWarn)
			}
			b.WriteString(col.Render(truncate(line, w)))
			b.WriteString("\n")
		}
	}

	return title, b.String()
}

// raidDebuffBody renders the raid-target debuff board: each mob and its called
// debuffs (Slowed/Tashed/Malo'd/OOS/…) with seconds remaining and who called it.
// A lapsed debuff (≤0 left) shows OUT in red — it needs a recast. w is the
// content (viewport-inner) width.
func (m Model) raidDebuffBody(th theme, w int) string {
	now := time.Now().Unix()
	var board []gamestate.RaidTarget
	if m.tracker != nil {
		board = m.tracker.RaidDebuffTargets(now)
	}
	iw := w
	var b strings.Builder
	if len(board) == 0 {
		b.WriteString(th.fg(th.dim).Render(truncate("No raid-target debuffs called yet.", iw)))
		b.WriteString("\n\n")
		b.WriteString(th.fg(th.dim).Render(truncate("Tracks macro calls like \"SLOWED - <mob>\", \"TASHED - …\", \"MALO - …\", \"OOS - …\".", iw)))
		return b.String()
	}
	for _, rt := range board {
		b.WriteString(th.fg(th.accent).Render(truncate("▸ "+rt.Name, iw)))
		b.WriteString("\n")
		for _, d := range rt.Debuffs {
			var rem string
			var remCol lipgloss.Style
			switch {
			case d.Count > 0: // stacking counter (sieves) — a tally, not a countdown
				rem, remCol = fmt.Sprintf("×%d", d.Count), th.fg(th.accent)
			case d.RemainSec <= 0:
				rem, remCol = "OUT", th.fg(chAlert)
			case d.RemainSec < 60:
				rem, remCol = mmss(int64(d.RemainSec)), th.fg(chWarn)
			default:
				rem, remCol = mmss(int64(d.RemainSec)), th.fg(chOK)
			}
			// fixed prefix "  %-9s " (12) + "%5s " (6) = 18 cols; caller gets the rest.
			b.WriteString(th.fg(th.text).Render(fmt.Sprintf("  %-9s ", truncate(d.Label, 9))))
			b.WriteString(remCol.Render(fmt.Sprintf("%5s ", rem)))
			b.WriteString(th.fg(th.dim).Render(truncate(d.Caller, max(iw-18, 1))))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// raidSplit reports whether the Raid tab shows both columns (wide enough) or
// just the CH-chain column (too narrow). raidCardW/chainCardW are the two card
// widths — the single source of truth shared by View, resizeViewports, and wheel
// routing so their geometry can't drift.
func raidSplit(innerW int) bool { return innerW >= 64 }
func raidCardW(innerW int) int {
	if !raidSplit(innerW) {
		return 0
	}
	return min(40, max(innerW/3, 24))
}
func chainCardW(innerW int) int {
	if !raidSplit(innerW) {
		return innerW
	}
	return innerW - raidCardW(innerW) - 1
}

// refreshRaid re-renders the Raid tab's two columns into their viewports.
func (m *Model) refreshRaid() {
	th := themes[m.theme]
	title, body := m.chchainBody(th, max(m.vpChain.Width, 1))
	m.raidChainTitle = title
	m.vpChain.SetContent(body)
	m.vpRaid.SetContent(m.raidDebuffBody(th, max(m.vpRaid.Width, 1)))
}

// rosterBlock renders the cleric roster for one chain: slot · name · CHs · last,
// with clerics silent past the gap threshold flagged.
func rosterBlock(th theme, st gamestate.CHChainStats, w int) string {
	var b strings.Builder
	b.WriteString(th.fg(th.accent).Render(truncate("  SLOT CLERIC          CHs  BEAT  NEXT", w)))
	b.WriteString("\n")
	for _, c := range st.Roster {
		beat := "  —" // BEAT% — how often this cleric hits their turn on-rhythm
		if c.BeatPct >= 0 {
			beat = fmt.Sprintf("%3d%%", c.BeatPct)
		}
		// NEXT — countdown to this cleric's next CH: "in Ns", "DUE" as it lands, or
		// "⚠Ns" overdue. "—" until cadence is known.
		next := "  —"
		switch {
		case st.Cadence <= 0:
		case c.NextSec > 1:
			next = fmt.Sprintf("%ds", c.NextSec)
		case c.NextSec >= -1:
			next = "DUE"
		default:
			next = fmt.Sprintf("⚠%ds", -c.NextSec)
		}
		line := fmt.Sprintf("  %-4s %-15s %3d  %4s  %s", c.Slot, truncate(c.Name, 15), c.Casts, beat, next)

		// the cleric whose turn is next gets a full-width green highlight bar — so
		// during a gap it's obvious who should cast right now.
		if st.Active && c.Name != "" && c.Name == st.NextCaster {
			hl := lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.Color(th.bg)).Background(lipgloss.Color(chOK)).Width(w)
			b.WriteString(hl.Render(truncate(line+"  ◀ NEXT", w)))
			b.WriteString("\n")
			continue
		}
		// otherwise color the whole row by status: red if overdue, amber if shaky.
		col := th.fg(th.text)
		switch {
		case st.Cadence > 0 && c.NextSec < -1:
			col = th.fg(chAlert)
		case c.BeatPct >= 0 && c.BeatPct < 70:
			col = th.fg(chWarn)
		}
		b.WriteString(col.Render(truncate(line, w)))
		b.WriteString("\n")
	}
	return b.String()
}

// completedOnly drops trailing in-progress chains that CHChainHistory appends
// (already shown live above), leaving only finished pulls.
func completedOnly(hist []gamestate.CHChainSummary, active []gamestate.CHChainStats) []gamestate.CHChainSummary {
	live := map[string]int64{}
	for _, a := range active {
		live[a.Target] = a.Start
	}
	out := hist[:0:0]
	for _, h := range hist {
		if s, ok := live[h.Target]; ok && s == h.Start {
			continue // this summary is the still-running chain shown above
		}
		out = append(out, h)
	}
	return out
}

// trimZero formats a float with at most one decimal, dropping a trailing ".0".
func trimZero(f float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0")
}
