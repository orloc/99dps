package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPanelModeCycle(t *testing.T) {
	if panelFull.next() != panelCompact || panelCompact.next() != panelOff || panelOff.next() != panelFull {
		t.Error("panelMode.next should cycle Full → Compact → Off → Full")
	}
}

func TestLayoutHonorsBoxModes(t *testing.T) {
	base := Model{w: 120, h: 40}

	full := base
	full.layoutPrefs = layoutPrefs{Damage: panelFull, OffDef: panelFull}
	if ld := full.layout(); ld.dmgW <= 0 || ld.extrasW <= 0 || ld.dmgH <= 0 {
		t.Errorf("both full: want a meter + side column + top height, got %+v", ld)
	}

	noDmg := base
	noDmg.layoutPrefs = layoutPrefs{Damage: panelOff, OffDef: panelFull}
	if ld := noDmg.layout(); ld.dmgW != 0 || ld.extrasW != ld.rightW {
		t.Errorf("damage off: want dmgW=0 and Offense·Defense full width, got %+v", ld)
	}

	noOD := base
	noOD.layoutPrefs = layoutPrefs{Damage: panelFull, OffDef: panelOff}
	if ld := noOD.layout(); ld.extrasW != 0 || ld.dmgW != ld.rightW {
		t.Errorf("offdef off: want extrasW=0 and meter full width, got %+v", ld)
	}

	both := base
	both.layoutPrefs = layoutPrefs{Damage: panelOff, OffDef: panelOff}
	if ld := both.layout(); ld.dmgH != 0 || ld.botH != ld.areaH {
		t.Errorf("both off: top row should collapse (dmgH=0, botH=areaH), got %+v", ld)
	}

	// Compact shrinks the top row so the bottom panels grow.
	compact := base
	compact.layoutPrefs = layoutPrefs{Damage: panelCompact, OffDef: panelCompact}
	fl, cm := full.layout(), compact.layout()
	if cm.dmgH >= fl.dmgH {
		t.Errorf("compact top should be shorter than full (%d vs %d)", cm.dmgH, fl.dmgH)
	}
	if cm.botH <= fl.botH {
		t.Errorf("compact should give the bottom row more height (%d vs %d)", cm.botH, fl.botH)
	}
}

func TestDamageCompactDropsColumns(t *testing.T) {
	var m tea.Model = New(sampleManager(), nil, "X")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m.(Model)
	mm.refresh()
	cur := mm.sessions[mm.effectiveSel()]

	mm.layoutPrefs.Damage = panelFull
	if !strings.Contains(mm.damageContent(cur, true, 80), "Crit") {
		t.Error("full meter at width 80 should show the Crit column")
	}
	mm.layoutPrefs.Damage = panelCompact
	if strings.Contains(mm.damageContent(cur, true, 80), "Crit") {
		t.Error("compact meter should drop the Crit column even at width 80")
	}
}

// TestLayoutClassBox: the class box splits the Mob Tracker column when on (Mob
// Tracker stays the taller part), and the column is whole when off.
func TestLayoutClassBox(t *testing.T) {
	m := Model{w: 120, h: 40}
	if ld := m.layout(); ld.classBox || ld.mobH != ld.botH {
		t.Errorf("class box off: mob should fill the column (classBox=%v mobH=%d botH=%d)", ld.classBox, ld.mobH, ld.botH)
	}

	m.layoutPrefs.ClassBox = true
	ld := m.layout()
	if !ld.classBox || ld.boxH <= 0 {
		t.Fatalf("class box on: want a box, got %+v", ld)
	}
	if ld.mobH+ld.boxH+1 != ld.botH {
		t.Errorf("box should split the column with a gap (mobH=%d boxH=%d botH=%d)", ld.mobH, ld.boxH, ld.botH)
	}
	if ld.mobH <= ld.boxH {
		t.Errorf("Mob Tracker should be the taller part (mobH=%d boxH=%d)", ld.mobH, ld.boxH)
	}
}

// TestSettingsTogglesClassBox: the Settings row flips the class box on/off and
// persists it.
func TestSettingsTogglesClassBox(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSettings, settingsSel: settingsClassBoxRow}
	if m.layoutPrefs.ClassBox {
		t.Fatal("class box should default off")
	}
	tm, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if !m.layoutPrefs.ClassBox {
		t.Error("enter should turn the class box on")
	}
	if !loadStore().forChar(m.character).ClassBox {
		t.Error("the class box choice should persist to the store")
	}
}

func TestSettingsCyclesMeterMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := Model{screen: screenSettings, settingsSel: settingsDamageRow}
	for _, want := range []panelMode{panelCompact, panelOff, panelFull} {
		tm, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
		m = tm.(Model)
		if m.layoutPrefs.Damage != want {
			t.Errorf("Damage mode = %v, want %v", m.layoutPrefs.Damage, want)
		}
	}
	if loadStore().forChar(m.character).Damage != panelFull {
		t.Error("the meter mode should persist to the settings store")
	}
}
