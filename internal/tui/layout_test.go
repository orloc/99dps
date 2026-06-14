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
	full.layoutPrefs = layoutPrefs{panelFull, panelFull}
	if ld := full.layout(); ld.dmgW <= 0 || ld.extrasW <= 0 || ld.dmgH <= 0 {
		t.Errorf("both full: want a meter + side column + top height, got %+v", ld)
	}

	noDmg := base
	noDmg.layoutPrefs = layoutPrefs{panelOff, panelFull}
	if ld := noDmg.layout(); ld.dmgW != 0 || ld.extrasW != ld.rightW {
		t.Errorf("damage off: want dmgW=0 and Offense·Defense full width, got %+v", ld)
	}

	noOD := base
	noOD.layoutPrefs = layoutPrefs{panelFull, panelOff}
	if ld := noOD.layout(); ld.extrasW != 0 || ld.dmgW != ld.rightW {
		t.Errorf("offdef off: want extrasW=0 and meter full width, got %+v", ld)
	}

	both := base
	both.layoutPrefs = layoutPrefs{panelOff, panelOff}
	if ld := both.layout(); ld.dmgH != 0 || ld.botH != ld.areaH {
		t.Errorf("both off: top row should collapse (dmgH=0, botH=areaH), got %+v", ld)
	}

	// Compact shrinks the top row so the bottom panels grow.
	compact := base
	compact.layoutPrefs = layoutPrefs{panelCompact, panelCompact}
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
