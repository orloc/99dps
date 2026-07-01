package tui

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/eq/gamestate"
	"99dps/internal/inventory"
)

// The Tools tab is a 3-column home for little helper widgets — quick utilities
// that aren't part of the live meter, each in its own box:
//   - 🎲 Rolls: a /random loot-roll tracker (live board + winners log).
//   - 🎒 Inventory: indexes your "/outputfile inventory" dump and shows each item
//     as a clickable wiki link with its tunnel (pigparse) price and location.

// invRow is one priced inventory line for the widget.
type invRow struct {
	name, location string
	count          int
	price, priceN  int // WTS average + sample count (0 = no tunnel data)
}

// invLoadedMsg carries the result of indexing the inventory dump back to Update.
type invLoadedMsg struct {
	rows   []invRow
	status string    // non-empty = a message to show instead of rows (no dump, error…)
	gen    time.Time // when the dump file was written (zero = unknown)
}

// updateTools handles input while the Tools tab is focused. (Tab/number nav is
// handled earlier, in Update.) i indexes the inventory dump; c closes the
// current roll; x clears the winners log.
func (m Model) updateTools(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "/":
		m.invSearching = true // focus the quick filter; typing now builds it
		m.refreshInventory()
		return m, nil
	case "esc":
		if m.invFilter != "" || m.invSearching {
			m.invSearching, m.invFilter = false, ""
			m.refreshInventory()
		}
		return m, nil
	case "i":
		m.invStatus = "indexing…"
		return m, m.loadInventoryCmd()
	case "c":
		if m.tracker != nil {
			m.tracker.FinalizeRolls()
			m.flash("roll closed")
		}
	case "x":
		if m.tracker != nil {
			m.tracker.ClearRollWinners()
			m.flash("winners cleared")
		}
	case "up", "down", "pgup", "pgdown", "j", "k", "home", "end":
		var cmd tea.Cmd // scroll the inventory list
		m.vpInv, cmd = m.vpInv.Update(msg)
		return m, cmd
	}
	return m, nil
}

// invSearchKey feeds keystrokes into the inventory quick filter while it's
// focused: Enter accepts (keeps the filter, unfocuses), Esc cancels (clears it),
// Backspace deletes a rune, and any printable rune extends the filter.
func (m *Model) invSearchKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEnter:
		m.invSearching = false
	case tea.KeyEsc:
		m.invSearching, m.invFilter = false, ""
	case tea.KeyBackspace:
		if r := []rune(m.invFilter); len(r) > 0 {
			m.invFilter = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.invFilter += " "
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if unicode.IsPrint(r) {
				m.invFilter += string(r)
			}
		}
	}
	m.refreshInventory()
}

// loadInventoryCmd reads the character's inventory dump and looks up tunnel
// prices — off the UI goroutine (it touches disk and the network), delivering an
// invLoadedMsg. Price lookup is best-effort: a network failure still returns the
// items (all as "no tunnel data").
func (m Model) loadInventoryCmd() tea.Cmd {
	eqDir, character, pricer := m.eqDir, m.character, m.pricer
	return func() tea.Msg {
		if eqDir == "" {
			return invLoadedMsg{status: "inventory folder unknown"}
		}
		items, err := inventory.Load(eqDir, character)
		if err != nil {
			return invLoadedMsg{status: "no inventory dump found"}
		}
		if len(items) == 0 {
			return invLoadedMsg{status: "the inventory dump is empty"}
		}
		var gen time.Time // the dump file's write time, for the staleness hint
		if fi, err := os.Stat(inventory.Locate(eqDir, character)); err == nil {
			gen = fi.ModTime()
		}
		priced := map[string]ItemPrice{}
		if pricer != nil {
			priced, _ = pricer.Lookup(inventory.Names(items)) // best-effort
		}
		rows := make([]invRow, 0, len(items))
		for _, it := range items {
			p := priced[strings.ToLower(it.Name)]
			rows = append(rows, invRow{name: it.Name, location: it.Location, count: it.Count, price: p.Avg, priceN: p.Count})
		}
		return invLoadedMsg{rows: rows, gen: gen}
	}
}

// toolsLayout sizes the Tools tab's three equal boxed columns and their height.
func (m Model) toolsLayout() (colW, lastW, h int) {
	innerW := max(m.w-2, 8)
	colW = max((innerW-2)/3, 10) // two 1-space gaps between three columns
	lastW = innerW - 2 - 2*colW  // remainder to the last column
	if lastW < colW {
		lastW = colW
	}
	h = max(m.h-4, 6) // outer pad (2) + banner (1) + tab bar (1)
	return colW, lastW, h
}

// toolsView lays the Tools tab out as three boxed columns: the /random roll
// board, the scrollable inventory list (its own viewport), and a reserved slot.
func (m Model) toolsView(th theme) string {
	colW, lastW, h := m.toolsLayout()

	var rolls []gamestate.Roll
	var winners []gamestate.RollWin
	if m.tracker != nil {
		rolls, winners = m.tracker.Rolls(), m.tracker.RollWinners()
	}
	rollCard := card(th, colW, h, "🎲 Rolls", rollBoard(th, rolls, winners, time.Now().Unix(), colW-4))
	invTitle := "🎒 Inventory"
	if m.invSearching || m.invFilter != "" {
		cursor := ""
		if m.invSearching {
			cursor = "▎" // a caret hint while the filter is focused
		}
		invTitle += " /" + m.invFilter + cursor
	}
	invCard := card(th, colW, h, invTitle+scrollHint(m.vpInv), m.vpInv.View())
	reserved := card(th, lastW, h, "", th.fg(th.dim).Render(truncate("more widgets soon", lastW-4)))
	return lipgloss.JoinHorizontal(lipgloss.Top, rollCard, " ", invCard, " ", reserved)
}

// inventory categories, by Location prefix in the dump.
const (
	catWorn = iota // equipped slots (Ear, Primary, Chest, …)
	catInv         // carried bags (General1, General4-Slot2, Cursor)
	catBank        // bank (Bank1, Bank8-Slot3, SharedBank…)
)

func invCategory(loc string) int {
	switch {
	case strings.HasPrefix(loc, "Bank") || strings.HasPrefix(loc, "SharedBank"):
		return catBank
	case strings.HasPrefix(loc, "General") || strings.HasPrefix(loc, "Cursor"):
		return catInv
	default:
		return catWorn
	}
}

// invValue is an item's total tunnel value (unit price × stack), 0 when unpriced.
func invValue(r invRow) int { return r.price * stack(r.count) }

// inventoryBoard renders the indexed inventory to width w, split into WORN /
// INVENTORY / BANK sections (each high-value-first, unpriced items dim at the
// bottom), under a grand-total + generated-time header. Empty until you press i.
// filter (case-insensitive substring) quick-narrows to matching item names.
// gen is the dump's write time (zero = unknown); now is used to age it.
func inventoryBoard(th theme, rows []invRow, status, filter string, gen, now time.Time, w int) string {
	dim := func(ls []string) string {
		for i, l := range ls {
			ls[i] = th.fg(th.dim).Render(l)
		}
		return lines(ls...)
	}

	if len(rows) == 0 {
		hint := status
		if hint == "" {
			hint = "press i to index your inventory"
		}
		// The dump is generated in-game, so always show the command + how to load
		// it, word-wrapped so a narrow column never clips it.
		usage := wrap("in EQ type  /outputfile inventory  — then press i to index", w)
		help := append(wrap(hint, w), "")
		return dim(append(help, usage...))
	}

	total := len(rows)
	if f := strings.TrimSpace(strings.ToLower(filter)); f != "" {
		kept := rows[:0:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.name), f) {
				kept = append(kept, r)
			}
		}
		rows = kept
		if len(rows) == 0 {
			return dim(append(wrap(fmt.Sprintf("no items match %q", filter), w),
				"", "press / to edit · esc to clear"))
		}
	}

	var groups [3][]invRow
	for _, r := range rows {
		c := invCategory(r.location)
		groups[c] = append(groups[c], r)
	}
	titles := [3]string{"WORN", "INVENTORY", "BANK"}

	grand := 0
	var body []string
	for gi := range groups {
		g := groups[gi]
		if len(g) == 0 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool { return invValue(g[i]) > invValue(g[j]) })
		sec := 0
		for _, r := range g {
			sec += invValue(r)
		}
		grand += sec
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, th.fg(th.accent).Bold(true).Render(
			truncate(fmt.Sprintf("%s (%d) · %sp", titles[gi], len(g), humanize(sec)), w)))
		for _, r := range g {
			body = append(body, invLine(th, r, w))
		}
	}

	count := fmt.Sprintf("%d items · ~%sp total", len(rows), humanize(grand))
	if len(rows) != total { // a filter is narrowing the list
		count = fmt.Sprintf("%d of %d items · ~%sp", len(rows), total, humanize(grand))
	}
	header := append([]string{count}, wrap(genLine(gen, now), w)...)
	return lines(append([]string{dim(header), ""}, body...)...)
}

// genLine is the loaded inventory's header hint: how stale the dump is plus the
// command to regenerate it. gen is the dump's write time (zero = unknown).
func genLine(gen, now time.Time) string {
	tail := "re-run /outputfile inventory if stale, then i · / to search"
	if gen.IsZero() {
		return tail
	}
	when := "generated " + compactDur(int64(now.Sub(gen).Seconds())) + " ago"
	if now.Sub(gen) < time.Minute {
		when = "generated just now"
	}
	return when + " · " + tail
}

// invLine renders one item: a clickable wiki link (the item name) and its price
// (or "—" for no tunnel data), sized to width w. The OSC 8 hyperlink is added
// after the layout is measured on plain text, so widths stay correct.
func invLine(th theme, r invRow, w int) string {
	color := th.dim
	price := "—"
	if r.priceN > 0 {
		color = th.text
		price = humanize(r.price) + "p"
		if r.count > 1 {
			price = fmt.Sprintf("%dx%sp", r.count, humanize(r.price))
		}
	}
	nameW := max(w-lipgloss.Width(price)-1, 1)
	plain := truncate(r.name, nameW)
	pad := strings.Repeat(" ", max(nameW-lipgloss.Width(plain), 0))
	return th.fg(color).Render(hyperlink(wikiURL(r.name), plain) + pad + " " + price)
}

// stack treats a 0/1 count as a single item for value math.
func stack(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// hyperlink wraps text in an OSC 8 terminal hyperlink. Terminals that support it
// (Windows Terminal, iTerm2, modern others) make the text clickable; the rest
// just show the text, so it degrades cleanly.
func hyperlink(uri, text string) string {
	return "\x1b]8;;" + uri + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// wikiURL is the P99 wiki page for an item (spaces → underscores).
func wikiURL(item string) string {
	return "https://wiki.project1999.com/" + url.PathEscape(strings.ReplaceAll(item, " ", "_"))
}

// rollBoard renders the roll widget body to width w: the live contest leaderboard
// (high roll first, leader marked ★) over a log of past winners (value · age ·
// zone). Width-aware so it fits a narrow column. Pure data→string for testing.
func rollBoard(th theme, current []gamestate.Roll, winners []gamestate.RollWin, now int64, w int) string {
	var ls []string

	// live contest
	if len(current) == 0 {
		ls = append(ls, th.fg(th.dim).Render(truncate("No active roll.", w)))
	} else {
		ls = append(ls, th.fg(th.dim).Render(truncate(fmt.Sprintf("rolling · %d–%d", current[0].Low, current[0].High), w)))
		for i, r := range current {
			marker, style := "  ", th.fg(th.text)
			if i == 0 { // current leader
				marker, style = "★ ", th.fg(th.accent).Bold(true)
			}
			ls = append(ls, style.Render(nameValue(marker, r.Roller, fmt.Sprintf("%d", r.Value), w)))
		}
	}

	// winners log
	if len(winners) > 0 {
		ls = append(ls, "", th.fg(th.accent).Render(truncate("recent winners", w)))
		for _, win := range winners {
			line := fmt.Sprintf("%s %d · %s", win.Roller, win.Value, compactDur(now-win.At))
			if win.Zone != "" {
				line += " · " + win.Zone
			}
			ls = append(ls, th.fg(th.dim).Render(truncate(line, w)))
		}
	}

	ls = append(ls, "", th.fg(th.dim).Render(truncate("c close · x clear log", w)))
	return lines(ls...)
}

// nameValue renders "<marker><name> <value>" with the value right-justified and
// the name truncated to fit width w.
func nameValue(marker, name, value string, w int) string {
	nameW := max(w-lipgloss.Width(marker)-lipgloss.Width(value)-1, 1)
	return truncate(fmt.Sprintf("%s%-*s %s", marker, nameW, truncate(name, nameW), value), w)
}
