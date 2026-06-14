package gamestate

import "testing"

// TestSpellRecastCooldown: casting a watched recast spell (Dictate charm /
// Rapture mez) starts a zone-scoped reuse cooldown read from the spell's own
// RecastMs, and a zone change clears it (you can recast freely after zoning).
func TestSpellRecastCooldown(t *testing.T) {
	b := loadBook(t,
		// Dictate: a charm (no landing emote), 12s recast
		row(map[int]string{
			fName: "Dictate", fFades: "You are no longer charmed.",
			fCastTime: "1000", fRecastTime: "12000",
			fDurFormula: "10", fDurCap: "70", fGoodEffect: "0",
		}),
		// Rapture: a single-target mez, 9s recast
		row(map[int]string{
			fName: "Rapture", fCastOnOther: " has been mesmerized.",
			fCastTime: "1000", fRecastTime: "9000",
			fDurFormula: "3", fDurCap: "120", fGoodEffect: "0",
		}),
		// an unwatched detrimental with a recast — must NOT spawn a cooldown
		row(map[int]string{
			fName: "Tashani", fCastOnOther: "'s magical defenses fall.",
			fCastTime: "1000", fRecastTime: "9000",
			fDurFormula: "3", fDurCap: "120", fGoodEffect: "0",
		}),
	)
	tr := NewTracker(b)
	tr.SetLevel(60)
	now := int64(1000)

	// enter a zone so the recast is "cast in this zone"
	tr.Observe("You have entered Lake of Ill Omen.", now)

	// cast Dictate → charm completes on the next line, starting its 12s recast
	tr.BeginCast("Dictate", now)
	tr.Observe("a sand giant glares at you.", now+1)
	cds := tr.Cooldowns(now + 1)
	if len(cds) != 1 || cds[0].Name != "Dictate" || cds[0].Remaining != 12 || cds[0].Total != 12 {
		t.Fatalf("after Dictate: cooldowns = %+v, want Dictate 12/12", cds)
	}

	// cast Rapture → its landing emote starts the 9s recast (now two cooldowns)
	tr.BeginCast("Rapture", now+1)
	tr.Observe("a sand giant has been mesmerized.", now+2)
	cds = tr.Cooldowns(now + 2)
	if len(cds) != 2 {
		t.Fatalf("after Rapture: want 2 cooldowns, got %+v", cds)
	}
	// soonest-ready first: Rapture (9 - 0) before Dictate (12 - 1)
	if cds[0].Name != "Rapture" || cds[0].Remaining != 9 {
		t.Errorf("Rapture cooldown = %+v, want Rapture 9", cds[0])
	}

	// an unwatched recast spell adds no cooldown
	tr.BeginCast("Tashani", now+2)
	tr.Observe("a sand giant's magical defenses fall.", now+3)
	if got := len(tr.Cooldowns(now + 3)); got != 2 {
		t.Errorf("Tashani must not add a cooldown, have %d", got)
	}

	// zoning clears the zone-scoped recasts
	tr.Observe("You have entered Skyfire Mountains.", now+4)
	if cds := tr.Cooldowns(now + 4); len(cds) != 0 {
		t.Errorf("a zone change should clear spell recasts, got %+v", cds)
	}
}

// TestSpellRecastSurvivesZeroRecast: a watched spell whose book entry has no
// recast (RecastMs 0) produces no cooldown rather than a bogus zero-length one.
func TestSpellRecastNoRecast(t *testing.T) {
	b := loadBook(t, row(map[int]string{
		fName: "Rapture", fCastOnOther: " has been mesmerized.",
		fCastTime: "1000", fRecastTime: "0",
		fDurFormula: "3", fDurCap: "120", fGoodEffect: "0",
	}))
	tr := NewTracker(b)
	tr.SetLevel(60)
	now := int64(1000)
	tr.Observe("You have entered Lake of Ill Omen.", now)
	tr.BeginCast("Rapture", now)
	tr.Observe("a sand giant has been mesmerized.", now+1)
	if cds := tr.Cooldowns(now + 1); len(cds) != 0 {
		t.Errorf("a 0-recast spell should make no cooldown, got %+v", cds)
	}
}
