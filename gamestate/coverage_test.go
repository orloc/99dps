package gamestate

import (
	"os"
	"strings"
	"testing"
)

// TestRealFile_TriggerCoverage audits our extracted triggers against the real
// spells_us.txt: counts each message type and asserts no doubled-period emote
// survived normalization. Set SPELLS_FILE to run.
func TestRealFile_TriggerCoverage(t *testing.T) {
	path := os.Getenv("SPELLS_FILE")
	if path == "" {
		t.Skip("set SPELLS_FILE to run")
	}
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	var onYou, onOther, fades, timed, badDots int
	for _, s := range b.byName {
		if s.CastOnYou != "" {
			onYou++
		}
		if s.CastOnOther != "" {
			onOther++
		}
		if s.Fades != "" {
			fades++
		}
		if s.DurFormula != 0 {
			timed++
		}
		for _, e := range []string{s.CastOnYou, s.CastOnOther, s.Fades} {
			if strings.HasSuffix(e, "..") {
				badDots++
			}
		}
	}
	t.Logf("spells=%d  cast_on_you=%d  cast_on_other=%d  fades=%d  timed(formula!=0)=%d",
		b.Len(), onYou, onOther, fades, timed)
	if badDots != 0 {
		t.Errorf("%d emotes still end in '..' after normalization", badDots)
	}

	// spot-check a known doubled-period spell normalizes to single
	if s, ok := b.ByName("Bedlam"); ok && strings.HasSuffix(s.CastOnYou, "..") {
		t.Errorf("Bedlam cast_on_you not normalized: %q", s.CastOnYou)
	}
}
