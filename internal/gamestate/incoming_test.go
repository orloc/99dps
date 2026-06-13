package gamestate

import "testing"

// findTimer returns the active "You" timer for the given category label, if any.
func findTimer(tr *Tracker, label string, now int64) (Timer, bool) {
	for _, tm := range tr.Active(now) {
		if tm.Spell == label && tm.Target == "You" {
			return tm, true
		}
	}
	return Timer{}, false
}

func TestIncomingDebuffLandsOnYou(t *testing.T) {
	tr := NewTracker(loadBook(t)) // incoming detection is registry-driven, not book-driven
	tr.Observe("You feel drowsy.", 1000)

	tm, ok := findTimer(tr, "Slowed", 1001)
	if !ok {
		t.Fatal("slow not detected on player")
	}
	if !tm.Detrimental || !tm.Estimated || tm.Target != "You" {
		t.Fatalf("wrong timer flags: %+v", tm)
	}
	if got := tm.Expiry - tm.Start; got != 366 {
		t.Errorf("ceiling duration = %d, want 366", got)
	}
}

func TestIncomingDebuffClearedByFade(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("Your feet adhere to the ground.", 1000) // rooted
	if _, ok := findTimer(tr, "Rooted", 1001); !ok {
		t.Fatal("root not detected")
	}
	tr.Observe("Your feet come free.", 1100) // wear-off — the authoritative end
	if _, ok := findTimer(tr, "Rooted", 1101); ok {
		t.Error("root should clear on its fade line")
	}
}

// A re-application refreshes the single per-category timer (no duplicate "You"
// instances), unlike same-named mobs.
func TestIncomingDebuffRefreshes(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("You are ensnared.", 1000)
	tr.Observe("You are ensnared.", 1200) // re-snared later
	n := 0
	for _, tm := range tr.Active(1201) {
		if tm.Spell == "Snared" {
			n++
			if tm.Start != 1200 {
				t.Errorf("timer not refreshed: start = %d, want 1200", tm.Start)
			}
		}
	}
	if n != 1 {
		t.Errorf("got %d Snared timers, want 1 (refresh, not duplicate)", n)
	}
}

// Every curated landing emote maps to exactly one category, and every fade emote
// resolves — so detection is unambiguous and the tables stay consistent.
func TestIncomingTablesConsistent(t *testing.T) {
	seenLand := map[string]string{}
	for _, d := range incomingDebuffs {
		if d.dur <= 0 {
			t.Errorf("%s has no ceiling duration", d.label)
		}
		for _, l := range d.land {
			if prev, dup := seenLand[NormEmote(l)]; dup {
				t.Errorf("landing emote %q maps to both %q and %q", l, prev, d.label)
			}
			seenLand[NormEmote(l)] = d.label
		}
		if len(d.fade) == 0 {
			t.Errorf("%s has no fade emote (can't be cleared early)", d.label)
		}
	}
}

// A wear-off emote a mob's debuff produces must not be confused with anything;
// feeding only a fade line (no prior landing) is a harmless no-op.
func TestIncomingFadeWithoutLandingNoop(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("Your feet come free.", 1000)
	if len(tr.Active(1001)) != 0 {
		t.Error("a stray fade line should create nothing")
	}
}

// Dismissing your buff list (clicking the "You" group header) must NOT clear an
// incoming debuff on you — it shares the "You" target but carries no caster to
// dismiss, and only its fade line / your death should end it.
func TestDismissTargetKeepsIncomingDebuff(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("You feel drowsy.", 1000) // Slowed (incoming, Estimated)
	if _, ok := findTimer(tr, "Slowed", 1001); !ok {
		t.Fatal("slow not detected")
	}
	tr.DismissTarget("You")
	if _, ok := findTimer(tr, "Slowed", 1001); !ok {
		t.Error("DismissTarget(\"You\") must not clear an incoming ON-YOU debuff")
	}
}

// A hostile proc's cast_on_you emote belongs to the incoming-debuff path, not the
// self-clicky index — so no detrimental spell may be indexed as a self-clicky, or
// a proc would spawn a duplicate ON-YOU timer with a bogus spell-exact duration.
func TestSelfClickyIndexExcludesDetrimental(t *testing.T) {
	b := loadBook(t)
	for emote, s := range b.byEmote {
		if s.Detrimental {
			t.Errorf("byEmote[%q] = %q is detrimental; self-clickies must be beneficial", emote, s.Name)
		}
	}
}
