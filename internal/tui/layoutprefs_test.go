package tui

import "testing"

func TestPanelModeCycle(t *testing.T) {
	if panelFull.next() != panelCompact || panelCompact.next() != panelOff || panelOff.next() != panelFull {
		t.Error("panelMode.next should cycle Full → Compact → Off → Full")
	}
}
