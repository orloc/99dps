package gamestate

import (
	"testing"

	"99dps/internal/eq/eqclass"
)

// A group-mate's slow on the mob you're fighting (no caster in the log) shows an
// estimated "Slowed" timer on that mob, at the big-slow ceiling.
func TestEnemySlowEstimated(t *testing.T) {
	tr := NewTracker(loadBook(t)) // monk: no own slow to cast
	tr.Observe("a lava beetle yawns.", 1000)

	// the timer is keyed by the normalized mob name (article-stripped, lowercased)
	tm, ok := activeMobTimer(tr, "Slowed", "lava beetle", 1001)
	if !ok {
		t.Fatal("a mob slow should create an estimated Slowed timer")
	}
	if !tm.Detrimental || !tm.Estimated {
		t.Errorf("enemy slow should be a detrimental, estimated timer: %+v", tm)
	}
	if got := tm.Expiry - tm.Start; got != 366 {
		t.Errorf("slow ceiling = %d, want 366 (big slow)", got)
	}
	// clears when the mob dies
	tr.Observe("a lava beetle has been slain by Grok!", 1100)
	if _, ok := activeMobTimer(tr, "Slowed", "lava beetle", 1101); ok {
		t.Error("the slow should clear when the mob dies")
	}
}

// Enchanter slows (Shiftless/Tepid/Forlorn Deeds, Languid Pace) emote " slows
// down.", not " yawns." — both must register as Slowed on the mob.
func TestEnemySlowEmoteVariants(t *testing.T) {
	cases := []struct{ line, mob string }{
		{"Imp protector slows down.", "imp protector"},      // enchanter Deeds line
		{"a lava beetle yawns.", "lava beetle"},             // shaman insect line
		{"Guard Rolkin's body slows down.", "guard rolkin"}, // possessive variant → correct mob
	}
	for _, c := range cases {
		tr := NewTracker(loadBook(t))
		tr.Observe(c.line, 1000)
		if _, ok := activeMobTimer(tr, "Slowed", c.mob, 1001); !ok {
			t.Errorf("%q should slow %q", c.line, c.mob)
		}
	}
}

// When YOU are the slower, the real cast path owns the timer (real spell name +
// duration) and the estimate must NOT also fire — exactly one Slowed-ish timer.
// Covers the enchanter " slows down." emote (Iznoa's case).
func TestOwnSlowNoDoubleTimer(t *testing.T) {
	b := loadBook(t, row(map[int]string{
		fName:        "Shiftless Deeds",
		fCastOnOther: " slows down.",
		fCastTime:    "0",
		fDurFormula:  "7",
		fDurCap:      "60",
		fGoodEffect:  "0", // detrimental
	}))
	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassEnchanter)
	now := int64(1000)
	tr.BeginCast("Shiftless Deeds", now)
	tr.Observe("a lava beetle slows down.", now) // your own slow landing

	var slows int
	for _, tm := range tr.Active(now + 1) {
		if tm.Target == "a lava beetle" {
			slows++
		}
	}
	if slows != 1 {
		t.Fatalf("your own slow must produce exactly ONE timer, got %d", slows)
	}
	// and it's the REAL spell (with its real duration), not the estimate
	if _, ok := activeMobTimer(tr, "Slowed", "a lava beetle", now+1); ok {
		t.Error("no phantom estimated Slowed when you cast your own slow")
	}
	if _, ok := activeMobTimer(tr, "Shiftless Deeds", "a lava beetle", now+1); !ok {
		t.Error("your own slow should show under its real spell name")
	}
}

// A landing emote can strip/capitalize the article ("Greater kobold yawns.")
// while the death line keeps it ("a greater kobold has been slain ..."). The slow
// timer — whose ONLY clear path is the slay — must still clear across that
// article mismatch, or a stale ~Slowed lingers on a dead mob for its full ceiling.
func TestEnemySlowClearsAcrossArticleMismatch(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("Greater kobold yawns.", 1000) // emote: article stripped, capitalized
	if _, ok := activeMobTimer(tr, "Slowed", "greater kobold", 1001); !ok {
		t.Fatal("setup: the slow timer should exist")
	}
	tr.Observe("a greater kobold has been slain by Xyzzy!", 1100) // death: article kept
	for _, tm := range tr.Active(1101) {
		if tm.Spell == "Slowed" {
			t.Errorf("slow must clear on kill despite the article mismatch, got %+v", tm)
		}
	}
}

func activeMobTimer(tr *Tracker, spell, target string, now int64) (Timer, bool) {
	for _, tm := range tr.Active(now) {
		if tm.Spell == spell && tm.Target == target {
			return tm, true
		}
	}
	return Timer{}, false
}

// Guild/group macro calls ("<KEYWORD> - <mob>") create authoritative estimated
// debuffs on the named mob, at each kind's max-level ceiling.
func TestMacroDebuffCalls(t *testing.T) {
	cases := []struct {
		line, label, mob string
		dur              int64
	}{
		{"Orland tells the guild, 'SLOWED - a fiery guardian'", "Slowed", "fiery guardian", 366},
		{"Ck tells the guild, 'TASHED - an ancient frost guardian'", "Tashed", "ancient frost guardian", 786},
		{"Mk tells the group, 'MALO - a vehement wyvern'", "Malo'd", "vehement wyvern", 846},
		{"Mk tells the guild, 'MALOSINI - a vehement wyvern'", "Malosini", "vehement wyvern", 1236}, // longer than MALO, distinct label
		{"En tells the guild, 'SIEVED - a shimmer drake'", "Sieved", "shimmer drake", 786},
		{"Or tells the guild, 'CRIPPLE - Ayillish'", "Crippled", "ayillish", 426},
		{"Ba tells the guild, 'OOS - a burning guardian (Occlusion of Sound; -10 CR, -10 FR, -10 MR)'", "OOS", "burning guardian", 24},
	}
	for _, c := range cases {
		tr := NewTracker(loadBook(t))
		tr.Observe(c.line, 1000)
		tm, ok := activeMobTimer(tr, c.label, c.mob, 1001)
		if !ok {
			t.Errorf("%q should create a %s timer on %q", c.line, c.label, c.mob)
			continue
		}
		if !tm.Detrimental || !tm.Estimated {
			t.Errorf("%s should be detrimental+estimated: %+v", c.label, tm)
		}
		if got := tm.Expiry - tm.Start; got != c.dur {
			t.Errorf("%s ceiling = %d, want %d", c.label, got, c.dur)
		}
	}
}

// MALO and MALOSINI are different spells with different durations — they must be
// tracked as distinct timers, not clumped into one.
func TestMacroMaloVsMalosiniDistinct(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("A tells the guild, 'MALO - a vehement wyvern'", 1000)
	tr.Observe("B tells the guild, 'MALOSINI - a vehement wyvern'", 1000)

	malo, ok1 := activeMobTimer(tr, "Malo'd", "vehement wyvern", 1001)
	malosini, ok2 := activeMobTimer(tr, "Malosini", "vehement wyvern", 1001)
	if !ok1 || !ok2 {
		t.Fatalf("both Malo'd and Malosini should exist on the mob (malo=%v malosini=%v)", ok1, ok2)
	}
	if (malo.Expiry - malo.Start) != 846 {
		t.Errorf("Malo'd ceiling = %d, want 846", malo.Expiry-malo.Start)
	}
	if (malosini.Expiry - malosini.Start) != 1236 {
		t.Errorf("Malosini ceiling = %d, want 1236", malosini.Expiry-malosini.Start)
	}
}

// Heal/summon calls share the "<KEYWORD> - <target>" format but target players,
// not mobs — they must NOT be recognized as debuffs.
func TestMacroIgnoresHealAndSummonCalls(t *testing.T) {
	tr := NewTracker(loadBook(t))
	for _, line := range []string{
		"Cleric tells the guild, 'KCH - Kelkix'",
		"Cleric tells the guild, 'CH - Beorngar'",
		"Mage tells the guild, 'COTH - Bigmurgh'",
		"Cleric tells the group, 'REMEDY - Chugster'",
		"Cleric tells the guild, 'BBB - CH - Flopcakes'", // nested heal-chain macro (real Iznoa log) — first token BBB, not a debuff
		"Joe tells the group, 'all slowed'",              // free-form, no "KEYWORD - mob"
	} {
		tr.Observe(line, 1000)
	}
	if n := len(tr.Active(1001)); n != 0 {
		t.Errorf("heal/summon/free-form calls must not create timers, got %d", n)
	}
}

// Only the guild and group channels are honored — say/ooc/tell can't trip it.
func TestMacroChannelRestricted(t *testing.T) {
	tr := NewTracker(loadBook(t))
	for _, line := range []string{
		"Bob says, 'SLOWED - a fiery guardian'",
		"Bob says out of character, 'SLOWED - a fiery guardian'",
		"Bob tells you, 'SLOWED - a fiery guardian'",
		"Bob auctions, 'SLOWED - a fiery guardian'",
	} {
		tr.Observe(line, 1000)
	}
	if n := len(tr.Active(1001)); n != 0 {
		t.Errorf("only guild/group macros should be recognized, got %d timers", n)
	}
}

// The emote signal and a macro call for the SAME mob collapse to one timer
// (normalized key), rather than doubling up.
func TestMacroAndEmoteDedup(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("Lava beetle yawns.", 1000)                           // emote → "lava beetle"
	tr.Observe("Sh tells the guild, 'SLOWED - a lava beetle'", 1001) // macro → "a lava beetle"

	n := 0
	for _, tm := range tr.Active(1002) {
		if tm.Spell == "Slowed" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("emote + macro for one mob should be a single Slowed timer, got %d", n)
	}
}

// The cripple-line emote times a Crippled debuff on the mob.
func TestEnemyCrippleEmote(t *testing.T) {
	for _, line := range []string{
		"a sand giant has been crippled.",
		"a sand giant looks frail.",
		"a sand giant is weakened.",
		"a sand giant weakens.",
	} {
		tr := NewTracker(loadBook(t))
		tr.Observe(line, 1000)
		tm, ok := activeMobTimer(tr, "Crippled", "sand giant", 1001)
		if !ok {
			t.Errorf("%q should cripple the mob", line)
			continue
		}
		if got := tm.Expiry - tm.Start; got != 426 {
			t.Errorf("cripple ceiling = %d, want 426", got)
		}
	}
}

// The malo-line emote is ambiguous: first sighting = Malo (846), second sighting
// on the same mob = Malosini (1236), replacing the Malo timer.
func TestEnemyMaloUpgradeOnSecondSighting(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("Glonk looks very uncomfortable.", 1000) // 1st → Malo
	if _, ok := activeMobTimer(tr, "Malo'd", "glonk", 1001); !ok {
		t.Fatal("first malo emote should create a Malo'd timer")
	}
	if _, ok := activeMobTimer(tr, "Malosini", "glonk", 1001); ok {
		t.Fatal("first malo emote should NOT be Malosini")
	}
	tr.Observe("Glonk looks very uncomfortable.", 1010) // 2nd → upgrade to Malosini
	if _, ok := activeMobTimer(tr, "Malo'd", "glonk", 1011); ok {
		t.Error("second sighting should replace the Malo'd timer")
	}
	ms, ok := activeMobTimer(tr, "Malosini", "glonk", 1011)
	if !ok {
		t.Fatal("second malo emote should upgrade to Malosini")
	}
	if got := ms.Expiry - ms.Start; got != 1236 {
		t.Errorf("Malosini ceiling = %d, want 1236", got)
	}
	// a fresh mob (same name) after a kill starts over at Malo
	tr.Observe("Glonk has been slain by Bob!", 1020)
	tr.Observe("Glonk looks very uncomfortable.", 1030)
	if _, ok := activeMobTimer(tr, "Malo'd", "glonk", 1031); !ok {
		t.Error("after the mob dies, the next malo should reset to Malo'd")
	}
}

// DismissTarget clears a mob's timers regardless of the capitalization used.
func TestDismissTargetCaseInsensitive(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.Observe("Glonk looks very uncomfortable.", 1000) // estimate keyed "glonk"
	if len(tr.Active(1001)) == 0 {
		t.Fatal("setup: a timer should exist")
	}
	tr.DismissTarget("Glonk") // click header shows "Glonk"
	if n := len(tr.Active(1001)); n != 0 {
		t.Errorf("dismiss should clear the mob's timers regardless of case, got %d", n)
	}
}
