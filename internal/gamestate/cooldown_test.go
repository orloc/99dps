package gamestate

import (
	"testing"

	"99dps/internal/common"
)

func TestMendCooldown(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	// no cooldowns until an ability fires
	if len(tr.Cooldowns(1000)) != 0 {
		t.Fatal("expected no cooldowns initially")
	}

	// a Mend attempt starts the 6-minute reuse and reveals the class as Monk
	tr.Observe("You mend your wounds and heal some damage.", 1000)
	if tr.Class() != common.ClassMonk {
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

func TestFeignStatus(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	if tr.FeignStatus(1000) != FeignNone {
		t.Fatal("no feign status expected initially")
	}

	// attempt via macro: pending during the grace window, then OK with no fail
	tr.FeignAttempt(1000)
	if tr.Class() != common.ClassMonk {
		t.Errorf("feign attempt should infer Monk, got %q", tr.Class())
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
