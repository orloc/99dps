package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = "Location\tName\tID\tCount\tSlots\n" +
	"Charm\tEmpty\t0\t0\t0\n" +
	"Ear\tBlack Sapphire Electrum Earring\t17213\t1\t5\n" +
	"General1\tBackpack\t17\t1\t10\n" +
	"General1-Slot1\tBat Wing\t13007\t20\t0\n" +
	"General1-Slot2\tBlack Sapphire Electrum Earring\t17213\t1\t0\n" + // a dup name
	"\n" + // blank line
	"junk line with no tabs\n"

func TestParse(t *testing.T) {
	items, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 { // header, Empty, blank, and the no-tab junk are all skipped
		t.Fatalf("got %d items, want 4: %+v", len(items), items)
	}
	first := items[0]
	if first.Name != "Black Sapphire Electrum Earring" || first.ID != 17213 || first.Location != "Ear" {
		t.Errorf("first item = %+v", first)
	}
	if items[2].Name != "Bat Wing" || items[2].Count != 20 {
		t.Errorf("bat wing row = %+v, want count 20", items[2])
	}

	// Names dedupes (the earring appears twice) and preserves order.
	names := Names(items)
	want := []string{"Black Sapphire Electrum Earring", "Backpack", "Bat Wing"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("Names = %v, want %v", names, want)
	}
}

func TestLocateAndLoad(t *testing.T) {
	dir := t.TempDir()
	if got := Locate(dir, "Iznoa"); got != filepath.Join(dir, "Iznoa-Inventory.txt") {
		t.Errorf("Locate = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "Iznoa-Inventory.txt"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := Load(dir, "Iznoa")
	if err != nil || len(items) != 4 {
		t.Fatalf("Load = %d items, err %v", len(items), err)
	}
	if _, err := Load(dir, "Nobody"); err == nil {
		t.Error("Load of a missing dump should error")
	}
}
