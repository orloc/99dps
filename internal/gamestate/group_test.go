package gamestate

import "testing"

// TestTimeInGroup: the clock starts on join/form, counts up, spans zones, and
// clears when you leave or the group disbands.
func TestTimeInGroup(t *testing.T) {
	tr := NewTracker(&Book{byName: map[string]*Spell{}})

	if _, ok := tr.TimeInGroup(1000); ok {
		t.Error("solo: time-in-group should be unknown")
	}

	// forming a group starts the clock; a later member-join doesn't reset it
	tr.Observe("You have formed the group.", 1000)
	tr.Observe("Sensive has joined the group.", 1050) // someone else — must not reset
	if d, ok := tr.TimeInGroup(1075); !ok || d != 75 {
		t.Errorf("after 75s = (%d,%v), want (75,true)", d, ok)
	}
	// group time spans a zone change (a group survives zoning)
	tr.Observe("You have entered Sebilis.", 1100)
	if d, ok := tr.TimeInGroup(1200); !ok || d != 200 {
		t.Errorf("group time should span zoning = (%d,%v), want (200,true)", d, ok)
	}

	// disband clears it
	tr.Observe("Your group has been disbanded.", 1300)
	if _, ok := tr.TimeInGroup(1400); ok {
		t.Error("disband should clear group time")
	}

	// joining a fresh group restarts the clock; being removed clears it again
	tr.Observe("You have joined the group.", 2000)
	if d, ok := tr.TimeInGroup(2020); !ok || d != 20 {
		t.Errorf("rejoin should restart = (%d,%v), want (20,true)", d, ok)
	}
	tr.Observe("You have been removed from the group.", 2100)
	if _, ok := tr.TimeInGroup(2200); ok {
		t.Error("being removed should clear group time")
	}

	// a character switch also clears it
	tr.Observe("You have formed the group.", 3000)
	tr.Clear()
	if _, ok := tr.TimeInGroup(3100); ok {
		t.Error("Clear should drop group time")
	}
}
