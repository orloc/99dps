package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// panelMode is how much of a toggleable meter box to show.
type panelMode int

const (
	panelFull    panelMode = iota // all columns/sections (default — zero value)
	panelCompact                  // a slimmed version
	panelOff                      // hidden
)

func (p panelMode) String() string {
	switch p {
	case panelCompact:
		return "Compact"
	case panelOff:
		return "Off"
	default:
		return "Full"
	}
}

// next cycles Full → Compact → Off → Full.
func (p panelMode) next() panelMode { return (p + 1) % 3 }

// layoutPrefs is the persisted per-box visibility choice for the meter screen.
// The zero value (both Full) is the default for a fresh install.
type layoutPrefs struct {
	Damage panelMode `json:"damage"`
	OffDef panelMode `json:"offDef"`
}

func layoutPrefsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "99dps", "layout.json"), nil
}

// loadLayoutPrefs reads the legacy meter-layout file; a missing/invalid file
// yields the default (both panels Full). Retained as a migration source — current
// writes go through the consolidated per-character store (see store.go).
func loadLayoutPrefs() layoutPrefs {
	var p layoutPrefs
	path, err := layoutPrefsPath()
	if err != nil {
		return p
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	_ = json.Unmarshal(b, &p)
	return p
}
