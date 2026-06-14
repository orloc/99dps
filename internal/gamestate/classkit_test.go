package gamestate

import (
	"testing"

	"99dps/internal/eqclass"
)

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
