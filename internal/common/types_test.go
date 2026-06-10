package common

import "testing"

func TestSwingStats_TallyAndRatios(t *testing.T) {
	var s SwingStats
	for _, o := range []SwingOutcome{
		OutcomeHit, OutcomeHit, OutcomeMiss, OutcomeDodge, OutcomeParry, OutcomeBlock, OutcomeRiposte, OutcomeAbsorb,
	} {
		s = s.Add(o)
	}

	if s.Hits != 2 || s.Misses != 1 || s.Dodges != 1 || s.Parries != 1 || s.Blocks != 1 || s.Ripostes != 1 || s.Absorbs != 1 {
		t.Fatalf("Add mis-tallied: %+v", s)
	}
	if s.Swings() != 8 {
		t.Errorf("Swings = %d, want 8", s.Swings())
	}
	// avoided includes riposte but excludes the rune absorb (the blow connected)
	if s.Avoided() != 5 {
		t.Errorf("Avoided = %d, want 5", s.Avoided())
	}
	// accuracy denominator is hits+misses only
	if s.Attempts() != 3 {
		t.Errorf("Attempts = %d, want 3", s.Attempts())
	}
	if s.HitRate() != 66 { // 2/3
		t.Errorf("HitRate = %d, want 66", s.HitRate())
	}
}

func TestSwingStats_HitRateEmptyIsNegative(t *testing.T) {
	if hr := (SwingStats{}).HitRate(); hr != -1 {
		t.Errorf("empty HitRate = %d, want -1", hr)
	}
}
