package tts

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Prefs is the user's persisted audio-cue choice, written once by the first-run
// setup screen so the app doesn't ask again. Configured distinguishes "the user
// has made a choice" (skip the setup screen) from a fresh install.
type Prefs struct {
	Configured bool   `json:"configured"`
	Enabled    bool   `json:"enabled"`
	Voice      string `json:"voice"` // engine voice ID, "" = default
}

func prefsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "99dps", "tts.json"), nil
}

// LoadPrefs reads the saved choice; a missing/invalid file yields the zero value
// (Configured=false → the setup screen will run).
func LoadPrefs() Prefs {
	var p Prefs
	path, err := prefsPath()
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
