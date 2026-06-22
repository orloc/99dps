package gamestate

import (
	"fmt"
	"testing"
)

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

// A player's OWN slow cast (Forlorn/Shiftless Deeds) shares its cast_on_you emote
// with the incoming Slowed family, so the line echoes while they cast it. It must
// time the MOB it lands on — never spawn a false ~Slowed on the caster, and never
// land the detrimental spell on "You".
func TestSelfSlowNotIncomingDebuff(t *testing.T) {
	b := loadBook(t, row(map[int]string{
		fName: "Forlorn Deeds", fCastOnOther: "'s movements slow.",
		fCastOnYou: "You feel drowsy.", fCastTime: "0",
		fDurFormula: "3", fDurCap: "50", fGoodEffect: "0", // detrimental
	}))
	tr := NewTracker(b)
	tr.SetLevel(60)
	now := int64(1000)
	tr.BeginCast("Forlorn Deeds", now)
	tr.Observe("a gnoll's movements slow.", now) // lands on the mob → correct timer
	tr.Observe("You feel drowsy.", now)          // own cast echo — NOT a debuff on you

	if _, ok := findTimer(tr, "Slowed", now+1); ok {
		t.Error("casting your own slow must not create a self ~Slowed timer")
	}
	mob := false
	for _, tm := range tr.Active(now + 1) {
		if tm.Target == "You" {
			t.Errorf("a detrimental self-cast must not land on the caster: %+v", tm)
		}
		if tm.Spell == "Forlorn Deeds" && tm.Target == "a gnoll" {
			mob = true
		}
	}
	if !mob {
		t.Error("Forlorn Deeds should still time the mob it landed on")
	}

	// a genuine incoming slow (no matching pending cast) is unaffected — still shows.
	tr2 := NewTracker(loadBook(t))
	tr2.Observe("You feel drowsy.", 2000)
	if _, ok := findTimer(tr2, "Slowed", 2001); !ok {
		t.Error("a real incoming slow (no own cast) must still register")
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

// TestSelfClickyExcludesDebuffEmote: an instant beneficial buff whose cast_on_you
// is a hostile-debuff line (a slow's "You slow down.") must NOT be self-clicky
// indexed — otherwise a self-cast slow's echo is misread as that buff (the
// Forlorn Deeds → "Rejuvenation" bug).
func TestSelfClickyExcludesDebuffEmote(t *testing.T) {
	b := loadBook(t,
		row(map[int]string{
			fName: "Forlorn Deeds", fCastOnOther: "'s movements slow.",
			fCastOnYou: "You slow down.", fCastTime: "3000",
			fDurFormula: "3", fDurCap: "50", fGoodEffect: "0", // detrimental slow
		}),
		row(map[int]string{
			fName: "Rejuvenation", fCastOnYou: "You slow down.", fCastTime: "0",
			fDurFormula: "3", fDurCap: "100", fGoodEffect: "1", // instant beneficial buff
		}),
	)
	if s, ok := b.byEmote[NormEmote("You slow down.")]; ok {
		t.Fatalf("a debuff emote must not index a self-buff, got %q", s.Name)
	}

	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.BeginCast("Forlorn Deeds", 1000)
	tr.Observe("You slow down.", 1003) // Forlorn Deeds' own slow echo
	for _, tm := range tr.Active(1004) {
		if tm.Spell == "Rejuvenation" {
			t.Errorf("a self-cast slow must not be misread as Rejuvenation: %+v", tm)
		}
	}
}

// TestSelfClickyNeverIndexesDebuffEmotes is the structural lock: NO incoming
// debuff "on you" landing emote — for ANY category (slow, root, cripple, tash, …)
// — may be indexed as a self-buff clicky, even if a beneficial instant spell
// collides with it. This generalizes the Forlorn Deeds → "Rejuvenation" bug to the
// whole incoming registry, so a new category or emote can't reopen it.
func TestSelfClickyNeverIndexesDebuffEmotes(t *testing.T) {
	var rows, emotes []string
	for _, d := range incomingDebuffs {
		for i, land := range d.land {
			rows = append(rows, row(map[int]string{
				fName: fmt.Sprintf("Phantom %s %d", d.label, i), fCastOnYou: land,
				fCastTime: "0", fDurFormula: "3", fDurCap: "100", fGoodEffect: "1", // instant beneficial buff
			}))
			emotes = append(emotes, land)
		}
	}
	b := loadBook(t, rows...)
	for _, e := range emotes {
		if s, ok := b.byEmote[NormEmote(e)]; ok {
			t.Errorf("debuff emote %q must not index a self-buff, got %q", e, s.Name)
		}
	}
}

// TestSelfCastDebuffNoPhantom drives the full Observe pipeline: casting a
// DETRIMENTAL spell on a mob whose cast_on_you collides with an incoming family
// must produce NO self timer — not the incoming "on you" category (pending-family
// skip), and not a phantom self-buff (debuff emote excluded from the index).
func TestSelfCastDebuffNoPhantom(t *testing.T) {
	cases := []struct{ spell, emote string }{
		{"Forlorn Deeds", "You slow down."},
		{"Enstill", "Your feet adhere to the ground."},
		{"Cripple", "You feel weak."},
		{"Tashan", "You hear the barking of Tashan."},
		{"Ensnare", "You are ensnared."},
	}
	for _, c := range cases {
		b := loadBook(t,
			row(map[int]string{ // the player's own detrimental cast
				fName: c.spell, fCastOnOther: " staggers.", fCastOnYou: c.emote,
				fCastTime: "3000", fDurFormula: "3", fDurCap: "50", fGoodEffect: "0",
			}),
			row(map[int]string{ // a beneficial buff that collides on the emote
				fName: "Decoy Buff", fCastOnYou: c.emote, fCastTime: "0",
				fDurFormula: "3", fDurCap: "100", fGoodEffect: "1",
			}),
		)
		tr := NewTracker(b)
		tr.SetLevel(60)
		tr.BeginCast(c.spell, 1000)
		tr.Observe(c.emote, 1003) // the player's own debuff cast echoing
		for _, tm := range tr.Active(1004) {
			if tm.Target == "You" {
				t.Errorf("%s: own cast must not create any self timer, got %+v", c.spell, tm)
			}
		}
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
