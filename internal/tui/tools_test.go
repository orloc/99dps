package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/gamestate"
	"99dps/internal/session"
)

// fakePricer returns canned tunnel prices without touching the network.
type fakePricer struct{ m map[string]ItemPrice }

func (f fakePricer) Lookup(names []string) (map[string]ItemPrice, error) {
	return f.m, nil
}

const invSample = "Location\tName\tID\tCount\tSlots\n" +
	"Ear\tBlack Sapphire Electrum Earring\t17213\t1\t0\n" +
	"General1-Slot1\tBat Wing\t13007\t20\t0\n" +
	"General1-Slot2\tInsidious Halo\t1246\t1\t0\n" // no tunnel data

// TestInventoryBoard: items split into WORN / INVENTORY / BANK sections, each
// high-value-first (unpriced last); names are clickable wiki links; header totals.
func TestInventoryBoard(t *testing.T) {
	th := themes[0]
	rows := []invRow{
		{name: "Bat Wing", location: "General1-Slot1", count: 20, price: 2, priceN: 500},               // INV, 40
		{name: "Black Sapphire Electrum Earring", location: "Ear", count: 1, price: 615, priceN: 1367}, // WORN, 615
		{name: "Insidious Halo", location: "General1-Slot2", count: 1, price: 0, priceN: 0},            // INV, no data
		{name: "Cloudy Stone", location: "Bank3", count: 1, price: 50, priceN: 10},                     // BANK, 50
	}
	out := inventoryBoard(th, rows, "", 60)

	for _, want := range []string{"4 items", "WORN", "INVENTORY", "BANK", "Black Sapphire", "Cloudy Stone"} {
		if !strings.Contains(out, want) {
			t.Errorf("board missing %q in:\n%s", want, out)
		}
	}
	// the worn earring is in its own section, before the inventory bat wing
	if !(strings.Index(out, "WORN") < strings.Index(out, "Black Sapphire") &&
		strings.Index(out, "Black Sapphire") < strings.Index(out, "INVENTORY")) {
		t.Error("the earring should sit under WORN, above the INVENTORY section")
	}
	// within INVENTORY, the priced bat wing (40) outranks the unpriced halo (0)
	if strings.Index(out, "Bat Wing") > strings.Index(out, "Insidious Halo") {
		t.Error("priced items should sort above no-data items within a section")
	}
	// names are OSC 8 hyperlinks to the P99 wiki
	if !strings.Contains(out, "\x1b]8;;https://wiki.project1999.com/Black_Sapphire_Electrum_Earring") {
		t.Error("item names should be clickable wiki hyperlinks")
	}
	// the OSC 8 escapes must not count toward display width (else the column wraps)
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 60 {
			t.Errorf("inventory line exceeds width %d: %q", lipgloss.Width(ln), ln)
		}
	}
	// the empty board prompts for indexing
	if got := inventoryBoard(th, nil, "", 60); !strings.Contains(got, "press i to index") {
		t.Errorf("empty board hint = %q", got)
	}
}

// TestInvCategory maps Location prefixes to the worn/inventory/bank sections.
func TestInvCategory(t *testing.T) {
	cases := map[string]int{
		"Ear": catWorn, "Primary": catWorn, "Fingers": catWorn,
		"General1": catInv, "General4-Slot2": catInv, "Cursor": catInv,
		"Bank3": catBank, "Bank8-Slot1": catBank, "SharedBank1": catBank,
	}
	for loc, want := range cases {
		if got := invCategory(loc); got != want {
			t.Errorf("invCategory(%q) = %d, want %d", loc, got, want)
		}
	}
}

// TestLoadInventory: pressing i indexes the dump and prices it via the injected
// pricer; the resulting message lands the rows on the model.
func TestLoadInventory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Iznoa-Inventory.txt"), []byte(invSample), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewModel(&session.SessionManager{}, nil, "Iznoa")
	m.eqDir = dir
	m.pricer = fakePricer{m: map[string]ItemPrice{
		"black sapphire electrum earring": {Avg: 615, Count: 1367},
	}}
	m.screen = screenTools

	out, cmd := m.updateTools(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if mm := out.(Model); mm.invStatus != "indexing…" {
		t.Errorf("i should set an indexing status, got %q", mm.invStatus)
	}
	if cmd == nil {
		t.Fatal("i should return a load command")
	}
	msg, ok := cmd().(invLoadedMsg)
	if !ok {
		t.Fatalf("command should produce invLoadedMsg, got %T", cmd())
	}
	if len(msg.rows) != 3 {
		t.Fatalf("want 3 rows, got %d (%+v)", len(msg.rows), msg.rows)
	}
	// the priced earring carries the fake price; the halo stays unpriced
	var earring, halo invRow
	for _, r := range msg.rows {
		switch r.name {
		case "Black Sapphire Electrum Earring":
			earring = r
		case "Insidious Halo":
			halo = r
		}
	}
	if earring.price != 615 || earring.priceN != 1367 {
		t.Errorf("earring priced = %+v, want 615/1367", earring)
	}
	if halo.priceN != 0 {
		t.Errorf("halo should have no tunnel data, got %+v", halo)
	}

	// feeding the message back through Update stores the rows
	updated, _ := m.Update(msg)
	if got := len(updated.(Model).invRows); got != 3 {
		t.Errorf("Update should store %d rows, got %d", len(msg.rows), got)
	}
}

// TestLoadInventoryNoDump: a missing dump yields a helpful status, no rows.
func TestLoadInventoryNoDump(t *testing.T) {
	m := NewModel(&session.SessionManager{}, nil, "Ghost")
	m.eqDir = t.TempDir() // exists, but no dump for "Ghost"
	_, cmd := m.updateTools(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	msg := cmd().(invLoadedMsg)
	if len(msg.rows) != 0 || !strings.Contains(msg.status, "/outputfile inventory") {
		t.Errorf("missing dump should yield a help status, got %+v", msg)
	}
}

// TestToolsTabRendersAndFits: the Tools tab is reachable, renders the roll
// widget, and never overflows the window width.
func TestToolsTabRendersAndFits(t *testing.T) {
	var m tea.Model = NewModel(&session.SessionManager{}, nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	mm := m.(Model)
	if mm.screen != screenTools {
		t.Fatalf("key 3 should open Tools, got %v", mm.screen)
	}

	for _, w := range []int{60, 80, 120} {
		// resize via the real message so the inventory viewport is sized for w
		var tm tea.Model = mm
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		out := tm.View()
		if !strings.Contains(out, "Rolls") || !strings.Contains(out, "Inventory") {
			t.Errorf("w=%d: Tools tab should render both widgets", w)
		}
		for _, ln := range strings.Split(out, "\n") {
			if lipgloss.Width(ln) > w {
				t.Errorf("w=%d: tools line exceeds width (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}

// TestRollBoard: the live leader is marked ★ with the range header; the winners
// log shows value · age · zone; an idle board shows the hint.
func TestRollBoard(t *testing.T) {
	th := themes[0]
	now := int64(10_000)

	if out := rollBoard(th, nil, nil, now, 60); !strings.Contains(out, "No active roll") {
		t.Errorf("an idle board should show the no-roll hint, got %q", out)
	}

	current := []gamestate.Roll{
		{Roller: "Emandar", Low: 0, High: 1000, Value: 952},
		{Roller: "Trashromancer", Low: 0, High: 1000, Value: 409},
	}
	winners := []gamestate.RollWin{
		{Roller: "Belvinn", Value: 871, At: now - 120, Zone: "Sebilis"}, // 2m ago
	}
	out := rollBoard(th, current, winners, now, 60)
	if !strings.Contains(out, "★") || strings.Index(out, "Emandar") < strings.Index(out, "★") {
		t.Error("★ should mark the leading (first) live row")
	}
	for _, want := range []string{"rolling · 0–1000", "Emandar", "952", "Trashromancer", "409",
		"recent winners", "Belvinn", "871", "2m", "Sebilis"} {
		if !strings.Contains(out, want) {
			t.Errorf("roll board missing %q in:\n%s", want, out)
		}
	}
}

// TestUpdateToolsKeys: c closes the current contest (winner → log, board resets);
// x clears the winners log.
func TestUpdateToolsKeys(t *testing.T) {
	tr := gamestate.NewTracker(&gamestate.Book{})
	tr.Observe("**A Magic Die is rolled by Solo.", 1)
	tr.Observe("**It could have been any number from 0 to 100, but this time it turned up a 77.", 1)
	if len(tr.Rolls()) != 1 {
		t.Fatal("setup: expected one live roll")
	}

	m := NewModel(&session.SessionManager{}, tr, "X")
	m.screen = screenTools

	out, _ := m.updateTools(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(tr.Rolls()) != 0 || len(tr.RollWinners()) != 1 {
		t.Errorf("c should close the contest: live=%d winners=%d", len(tr.Rolls()), len(tr.RollWinners()))
	}
	if mm := out.(Model); mm.status != "roll closed" {
		t.Errorf("expected a 'roll closed' flash, got %q", mm.status)
	}

	out, _ = out.(Model).updateTools(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if len(tr.RollWinners()) != 0 {
		t.Error("x should clear the winners log")
	}
	if mm := out.(Model); mm.status != "winners cleared" {
		t.Errorf("expected a 'winners cleared' flash, got %q", mm.status)
	}
}
