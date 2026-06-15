package gamestate

import (
	"99dps/internal/eqclass"
	"testing"
)

func TestMendCooldown(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	// no cooldowns until an ability fires
	if len(tr.Cooldowns(1000)) != 0 {
		t.Fatal("expected no cooldowns initially")
	}

	// a Mend attempt starts the 6-minute reuse and reveals the class as Monk
	tr.Observe("You mend your wounds and heal some damage.", 1000)
	if tr.Class() != eqclass.ClassMonk {
		t.Errorf("Mend should infer class Monk, got %q", tr.Class())
	}
	cds := tr.Cooldowns(1000)
	if len(cds) != 1 || cds[0].Name != "Mend" || cds[0].Remaining != 360 {
		t.Fatalf("cooldowns = %+v, want Mend 360", cds)
	}

	// partway through it counts down
	if rem := tr.Cooldowns(1300)[0].Remaining; rem != 60 {
		t.Errorf("remaining at +300s = %d, want 60", rem)
	}
	// past expiry it reads ready (0), not negative
	if rem := tr.Cooldowns(2000)[0].Remaining; rem != 0 {
		t.Errorf("remaining past expiry = %d, want 0", rem)
	}

	// a failed/worsened attempt still restarts the reuse
	tr.Observe("You worsen your wounds.", 2000)
	if rem := tr.Cooldowns(2000)[0].Remaining; rem != 360 {
		t.Errorf("failed Mend should restart reuse, got %d", rem)
	}

	// a character switch clears it
	tr.Clear()
	if len(tr.Cooldowns(2000)) != 0 {
		t.Error("Clear should drop cooldowns")
	}
}

// TestCooldownCatalog is the pluggable-cue contract: every registry ability plus
// the macro-driven Feign Death is enumerable (with its reuse) before any timer
// fires, so the settings UI can list per-skill cues.
func TestCooldownCatalog(t *testing.T) {
	cat := CooldownCatalog()
	want := map[string]int64{"Mend": 360, "Kick": monkSpecialReuseSec, "Feign Death": feignReuseSec}
	got := map[string]int64{}
	for _, c := range cat {
		got[c.Name] = c.ReuseSec
		if c.Class != eqclass.ClassMonk {
			t.Errorf("%s: class = %v, want Monk (all current abilities are Monk)", c.Name, c.Class)
		}
	}
	for name, reuse := range want {
		if got[name] != reuse {
			t.Errorf("catalog[%q] reuse = %d, want %d (have %v)", name, got[name], reuse, got)
		}
	}
}

// TestMendMatcherRejectsSpoof guards the anchored matcher: another player can
// quote "mend your wounds" in a tell/say, which logs verbatim in our file. That
// must not start the Mend cooldown or mis-infer us as a Monk — only a first-
// person line ("You ...") is the player's own ability.
func TestMendMatcherRejectsSpoof(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	tr.Observe("Spoofer tells you, 'just mend your wounds bro'", 1000)
	tr.Observe("Spoofer says, 'I will mend your wounds and heal some damage.'", 1000)
	if len(tr.Cooldowns(1000)) != 0 {
		t.Fatal("a tell/say quoting Mend must not start the cooldown")
	}
	if tr.Class() == eqclass.ClassMonk {
		t.Error("a spoofing tell must not infer Monk")
	}

	// the real first-person line still works
	tr.Observe("You mend your wounds and heal some damage.", 2000)
	if cds := tr.Cooldowns(2000); len(cds) != 1 || cds[0].Name != "Mend" {
		t.Fatalf("real Mend should start the cooldown, got %+v", cds)
	}
}

func TestFeignStatus(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	if tr.FeignStatus(1000) != FeignNone {
		t.Fatal("no feign status expected initially")
	}

	// attempt via macro: pending during the grace window, then OK with no fail
	tr.FeignAttempt(1000)
	// feign is not class-exclusive (Monk skill / SK-Necro spell), so it must NOT
	// infer a class — Monks are identified by Mend/Kick/who instead.
	if tr.Class() != eqclass.ClassUnknown {
		t.Errorf("feign attempt should infer no class, got %q", tr.Class())
	}
	// the attempt also starts the 11s FD reuse cooldown
	if cds := tr.Cooldowns(1000); len(cds) != 1 || cds[0].Name != "Feign Death" || cds[0].Remaining != feignReuseSec {
		t.Errorf("feign attempt should start an 11s cooldown, got %+v", cds)
	}
	if s := tr.FeignStatus(1001); s != FeignPending {
		t.Errorf("within grace = %v, want FeignPending", s)
	}
	if s := tr.FeignStatus(1003); s != FeignOK {
		t.Errorf("after grace, no fail = %v, want FeignOK", s)
	}
	if s := tr.FeignStatus(1100); s != FeignNone {
		t.Errorf("past the show window = %v, want FeignNone", s)
	}

	// a fail right after an attempt classifies as failed
	tr.FeignAttempt(2000)
	tr.FeignFailed(2001)
	if s := tr.FeignStatus(2002); s != FeignFailed {
		t.Errorf("fail after attempt = %v, want FeignFailed", s)
	}

	// a bare fail with no macro still alerts
	tr2 := NewTracker(&Book{byName: map[string]*Spell{}})
	tr2.FeignFailed(5000)
	if s := tr2.FeignStatus(5001); s != FeignFailed {
		t.Errorf("bare fail = %v, want FeignFailed", s)
	}

	tr.Clear()
	if tr.FeignStatus(2002) != FeignNone {
		t.Error("Clear should reset feign state")
	}
}

// TestFeignSpell: the SK/Necro Feign Death is a spell — its cast is the attempt
// (reuse = the spell's recast), success is the absence of a fizzle/interrupt, and
// a fizzle marks it failed. It infers no class (SK and Necro both cast it).
func TestFeignSpell(t *testing.T) {
	b := loadBook(t, row(map[int]string{
		fName: "Feign Death", fCastTime: "1500", fRecastTime: "12000", fGoodEffect: "1",
	}))
	tr := NewTracker(b)

	// casting it is the attempt → 12s reuse (the spell's recast), no class inferred
	tr.BeginCast("Feign Death", 1000)
	if tr.Class() != eqclass.ClassUnknown {
		t.Errorf("casting Feign Death must infer no class, got %q", tr.Class())
	}
	if cds := tr.Cooldowns(1000); len(cds) != 1 || cds[0].Name != "Feign Death" || cds[0].Remaining != 12 {
		t.Fatalf("spell feign should start a 12s cooldown, got %+v", cds)
	}
	// no fizzle/interrupt → success once the grace window passes
	if s := tr.FeignStatus(1003); s != FeignOK {
		t.Errorf("a clean spell cast should read FeignOK, got %v", s)
	}

	// a fizzle on the cast marks it failed
	tr.BeginCast("Feign Death", 2000)
	tr.Observe("Your spell fizzles!", 2000)
	if s := tr.FeignStatus(2001); s != FeignFailed {
		t.Errorf("a fizzled feign cast should read FeignFailed, got %v", s)
	}
}

func TestBinding(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	if _, ok := tr.BindRemaining(1000); ok {
		t.Fatal("not binding initially")
	}

	// begins → counts down toward the 10s finish
	tr.Observe("You begin to bandage yourself.", 1000)
	if rem, ok := tr.BindRemaining(1003); !ok || rem != 7 {
		t.Errorf("at +3s = (%d,%v), want (7,true)", rem, ok)
	}
	// the "complete" line ends it
	tr.Observe("The bandaging is complete.", 1010)
	if _, ok := tr.BindRemaining(1010); ok {
		t.Error("complete line should end binding")
	}

	// a move interrupts it
	tr.Observe("You begin to bandage yourself.", 2000)
	tr.Observe("You have moved and your attempt to bandage has failed.", 2003)
	if _, ok := tr.BindRemaining(2004); ok {
		t.Error("a failed/interrupted bind should clear")
	}

	// and a stuck bind (no complete/fail line) clears after the grace window
	tr.Observe("You begin to bandage yourself.", 3000)
	if _, ok := tr.BindRemaining(3005); !ok {
		t.Error("should still be binding within the window")
	}
	if _, ok := tr.BindRemaining(3020); ok {
		t.Error("a stuck bind should clear past the duration+grace")
	}
}

// TestBehavioralTablesReferenceKnownClasses guards the link between the
// declarative class facts (eqclass.Profile) and this layer's behavioral tables:
// every class a cooldown or clicky is filed under must be a real class. A typo or
// a stale class constant would otherwise silently never match.
func TestBehavioralTablesReferenceKnownClasses(t *testing.T) {
	for _, cd := range cooldownRegistry {
		if !eqclass.IsKnown(cd.Class) {
			t.Errorf("cooldownRegistry %q references unknown class %q", cd.Name, cd.Class)
		}
	}
	for class, list := range clickyRegistry {
		if class == eqclass.ClassUnknown {
			continue // the deliberate "usable by anyone" universal bucket
		}
		if !eqclass.IsKnown(class) {
			t.Errorf("clickyRegistry references unknown class %q (%d entries)", class, len(list))
		}
	}
}

// TestCooldown_MonkSpecialSharedTimer: monk specials share ONE reuse timer, so a
// kick and a hand strike drive a single "Kick" cooldown — never two — and a
// strike (re)starts the same timer.
func TestCooldown_MonkSpecialSharedTimer(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.SetClass(eqclass.ClassMonk)

	tr.Observe("You kick a sand giant for 24 points of damage.", 1000)
	cds := tr.Cooldowns(1001)
	if len(cds) != 1 || cds[0].Name != "Kick" || cds[0].Total != monkSpecialReuseSec {
		t.Fatalf("a kick should start one shared special cooldown; got %+v", cds)
	}

	// a hand strike later restarts the SAME timer, not a second one
	tr.Observe("You strike a sand giant for 18 points of damage.", 1003)
	cds = tr.Cooldowns(1004)
	if len(cds) != 1 {
		t.Fatalf("a strike must reuse the shared timer, not add a second cooldown; got %+v", cds)
	}
	if cds[0].Remaining != monkSpecialReuseSec-1 { // restarted at 1003, read at 1004
		t.Errorf("strike should restart the shared timer; remaining = %d, want %d", cds[0].Remaining, monkSpecialReuseSec-1)
	}
}

// TestCooldown_KickNotForNonMonk: kick isn't monk-exclusive, so a warrior's kick
// must neither start the cooldown nor flip the detected class to monk.
func TestCooldown_KickNotForNonMonk(t *testing.T) {
	tr := NewTracker(loadBook(t))
	tr.SetClass(eqclass.ClassWarrior)
	tr.Observe("You kick a sand giant for 24 points of damage.", 1000)
	if cds := tr.Cooldowns(1001); len(cds) != 0 {
		t.Errorf("a warrior's kick must not create a cooldown; got %v", cds)
	}
	if tr.Class() != eqclass.ClassWarrior {
		t.Errorf("a kick must not change the detected class; got %v", tr.Class())
	}
}

// TestSpellRecastCooldown: casting a watched recast spell (Dictate charm /
// Rapture mez) starts a reuse cooldown read from the spell's own RecastMs, and
// the timer persists across a zone change (recasts are real-time in EQ).
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

	// zoning must NOT reset the recasts — they're real-time in EQ. Both still
	// counting at now+4: Rapture landed at now+2 (9s → 7 left), Dictate completed
	// at now+1 (12s → 9 left).
	tr.Observe("You have entered Skyfire Mountains.", now+4)
	cds = tr.Cooldowns(now + 4)
	if len(cds) != 2 {
		t.Fatalf("a zone change must not drop spell recasts, got %+v", cds)
	}
	if cds[0].Name != "Rapture" || cds[0].Remaining != 7 {
		t.Errorf("Rapture should keep counting across zoning, got %+v", cds[0])
	}
	if cds[1].Name != "Dictate" || cds[1].Remaining != 9 {
		t.Errorf("Dictate should keep counting across zoning, got %+v", cds[1])
	}
}

// TestSpellRecastOnMemorize: memorizing a watched long-recast spell arms its
// cooldown (it must clear the first gem refresh before it can be cast), without
// any cast at all. The real log line is "You have finished memorizing Dictate."
func TestSpellRecastOnMemorize(t *testing.T) {
	b := loadBook(t, row(map[int]string{
		fName: "Dictate", fFades: "You are no longer charmed.",
		fCastTime: "5000", fRecastTime: "300000", // P99 Dictate: 5-min recast
		fDurFormula: "10", fDurCap: "70", fGoodEffect: "0",
	}))
	tr := NewTracker(b)
	tr.SetLevel(60)
	now := int64(1000)
	tr.Observe("You have entered Lake of Ill Omen.", now)

	tr.Observe("You have finished memorizing Dictate.", now)
	cds := tr.Cooldowns(now)
	if len(cds) != 1 || cds[0].Name != "Dictate" || cds[0].Remaining != 300 || cds[0].Total != 300 {
		t.Fatalf("memorizing Dictate should arm a 300s cooldown, got %+v", cds)
	}

	// an unwatched spell's memorize line arms nothing
	tr.Observe("You have finished memorizing Gate.", now)
	if got := len(tr.Cooldowns(now)); got != 1 {
		t.Errorf("memorizing an unwatched spell must not add a cooldown, have %d", got)
	}

	// unmemorizing it ("You forget Dictate.") drops the timer — no longer applicable
	tr.Observe("You forget Dictate.", now+5)
	if got := len(tr.Cooldowns(now + 5)); got != 0 {
		t.Errorf("forgetting Dictate should drop its cooldown, have %d", got)
	}
}

// TestSpellRecastClassGated: once the class is known, a recast spell that isn't
// on that class's watch list is ignored (the class param flows from the tracker
// through to eqclass.RecastWatched).
func TestSpellRecastClassGated(t *testing.T) {
	b := loadBook(t, row(map[int]string{
		fName: "Dictate", fFades: "You are no longer charmed.",
		fCastTime: "5000", fRecastTime: "300000",
		fDurFormula: "10", fDurCap: "70", fGoodEffect: "0",
	}))
	tr := NewTracker(b)
	tr.SetLevel(60)
	tr.SetClass(eqclass.ClassWarrior) // not the Enchanter that watches Dictate
	tr.Observe("You have entered Lake of Ill Omen.", 1000)
	tr.Observe("You have finished memorizing Dictate.", 1000)
	if cds := tr.Cooldowns(1000); len(cds) != 0 {
		t.Errorf("a Warrior must not track Dictate's recast, got %+v", cds)
	}
}

// TestSpellRecastNoRecast: a watched spell whose book entry has no recast
// (RecastMs 0) produces no cooldown rather than a bogus zero-length one.
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
