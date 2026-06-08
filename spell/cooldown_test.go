package spell

import (
	"testing"

	"99dps/common"
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

func TestFeignFailDetection(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	if tr.FeignFailedAt() != 0 {
		t.Fatal("no feign fail expected initially")
	}

	tr.Observe("You have fallen to the ground.", 5000)
	if tr.FeignFailedAt() != 5000 {
		t.Errorf("feign fail time = %d, want 5000", tr.FeignFailedAt())
	}

	tr.Clear()
	if tr.FeignFailedAt() != 0 {
		t.Error("Clear should reset feign fail time")
	}
}
