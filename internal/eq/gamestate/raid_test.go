package gamestate

import "testing"

func TestRaidDebuffBoard(t *testing.T) {
	base := int64(1_700_000_000)
	tr := NewTracker(loadBook(t))
	// macro debuff calls in guild chat name the mob and imply the debuff family.
	tr.Observe("Gruhh tells the guild, 'SLOWED - Vulak`Aerr'", base)
	tr.Observe("Dgchanter tells the guild, 'TASHED - Vulak`Aerr'", base+5)
	tr.Observe("Gruhh tells the guild, 'MALO - Vulak`Aerr'", base+10)
	tr.Observe("Powerade tells the guild, 'OOS - Lord Feshlak'", base+12)

	board := tr.RaidDebuffTargets(base + 12)
	if len(board) != 2 {
		t.Fatalf("targets = %d, want 2", len(board))
	}
	// most-recently-active first → Lord Feshlak (t+12) ahead of Vulak (t+10)
	if board[0].Name != "Lord Feshlak" {
		t.Errorf("first target = %q, want Lord Feshlak", board[0].Name)
	}
	var vulak *RaidTarget
	for i := range board {
		if board[i].Name == "Vulak`Aerr" {
			vulak = &board[i]
		}
	}
	if vulak == nil || len(vulak.Debuffs) != 3 {
		t.Fatalf("Vulak debuffs = %+v, want 3 (Slowed/Tashed/Malo'd)", vulak)
	}
	// labels sorted; verify the slow call recorded its caller and a positive remain
	var slowed *RaidDebuffInfo
	for i := range vulak.Debuffs {
		if vulak.Debuffs[i].Label == "Slowed" {
			slowed = &vulak.Debuffs[i]
		}
	}
	if slowed == nil || slowed.Caller != "Gruhh" || slowed.RemainSec <= 0 {
		t.Errorf("Slowed = %+v, want caller Gruhh with positive remain", slowed)
	}

	// re-calling refreshes the remaining and the caller
	tr.Observe("Narukan tells the guild, 'SLOWED - Vulak`Aerr'", base+20)
	for _, d := range targetDebuffs(tr.RaidDebuffTargets(base+20), "Vulak`Aerr") {
		if d.Label == "Slowed" && d.Caller != "Narukan" {
			t.Errorf("after refresh, Slowed caller = %q, want Narukan", d.Caller)
		}
	}

	// past the TTL with no calls → the board clears
	if got := len(tr.RaidDebuffTargets(base + 20 + raidTargetTTLSec + 1)); got != 0 {
		t.Errorf("board after TTL = %d, want 0", got)
	}
}

func TestRaidSieveCounter(t *testing.T) {
	base := int64(1_700_000_000)
	tr := NewTracker(loadBook(t))
	// sieves stack — three SIEVE calls on one mob = a count of 3, not a timer.
	tr.Observe("Enchy tells the guild, 'SIEVED - Gozzrem'", base)
	tr.Observe("Enchy tells the guild, 'SIEVE - Gozzrem'", base+5)
	tr.Observe("Miya tells the guild, 'SIEVED - Gozzrem'", base+10)
	// a normal timer debuff alongside, to prove they're handled differently
	tr.Observe("Slower tells the guild, 'SLOWED - Gozzrem'", base+11)

	ds := targetDebuffs(tr.RaidDebuffTargets(base+11), "Gozzrem")
	var sieve, slow *RaidDebuffInfo
	for i := range ds {
		switch ds[i].Label {
		case "Sieved":
			sieve = &ds[i]
		case "Slowed":
			slow = &ds[i]
		}
	}
	if sieve == nil || sieve.Count != 3 {
		t.Fatalf("Sieved = %+v, want Count 3 (a stacking counter)", sieve)
	}
	if sieve.RemainSec != 0 {
		t.Errorf("Sieved.RemainSec = %d, want 0 (it's a counter, not a timer)", sieve.RemainSec)
	}
	if slow == nil || slow.Count != 0 || slow.RemainSec <= 0 {
		t.Errorf("Slowed = %+v, want a timer (Count 0, positive RemainSec)", slow)
	}
}

func TestRaidBoardEviction(t *testing.T) {
	base := int64(1_700_000_000)
	tr := NewTracker(loadBook(t))
	// call a debuff on more than raidTargetCap distinct mobs, one second apart.
	for i := range raidTargetCap + 5 {
		tr.Observe("Al tells the guild, 'SLOWED - Mob"+itoa(i)+"'", base+int64(i))
	}
	now := base + int64(raidTargetCap+5)
	board := tr.RaidDebuffTargets(now)
	if len(board) > raidTargetCap {
		t.Errorf("board size = %d, want ≤ %d (eviction)", len(board), raidTargetCap)
	}
	// the earliest mobs should have been evicted; the most recent must remain.
	if targetDebuffs(board, "Mob"+itoa(raidTargetCap+4)) == nil {
		t.Errorf("the most-recent target was evicted")
	}
	if targetDebuffs(board, "Mob0") != nil {
		t.Errorf("the stalest target (Mob0) should have been evicted")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func targetDebuffs(board []RaidTarget, name string) []RaidDebuffInfo {
	for _, rt := range board {
		if rt.Name == name {
			return rt.Debuffs
		}
	}
	return nil
}
