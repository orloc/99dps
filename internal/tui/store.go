package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"99dps/internal/tts"
)

// charSettings is everything a single character can configure: audio on/off and
// voice, the meter-box layout, and the audio-cue toggle overrides.
type charSettings struct {
	AudioOn bool            `json:"audioOn"`
	Voice   string          `json:"voice,omitempty"`
	Damage  panelMode       `json:"damage"`
	OffDef  panelMode       `json:"offDef"`
	Cues    map[string]bool `json:"cues,omitempty"`
}

// store is the consolidated settings file (settings.json): one install-level flag
// (Configured — first-run onboarding done), a Default profile new characters
// inherit, and a per-character override map. Keeping everything in one file means
// a single load/save path; keying by character means each toon remembers its own
// audio cues, voice, and layout.
type store struct {
	Configured bool                    `json:"configured"`
	Default    charSettings            `json:"default"`
	Chars      map[string]charSettings `json:"characters,omitempty"`
}

// forChar returns a character's settings, falling back to Default for one not yet
// seen — so a fresh alt starts from the onboarding baseline rather than blank.
func (s store) forChar(name string) charSettings {
	if cs, ok := s.Chars[name]; ok {
		return cs
	}
	return s.Default
}

// setChar records a character's settings (creating the map on first use).
func (s *store) setChar(name string, cs charSettings) {
	if s.Chars == nil {
		s.Chars = map[string]charSettings{}
	}
	s.Chars[name] = cs
}

func storePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "99dps", "settings.json"), nil
}

// loadStore reads settings.json. On a fresh install (no file yet) it migrates the
// legacy per-install tts.json / layout.json / cues.json into the Default profile,
// so an existing user keeps their voice / layout / cue choices on first upgrade.
func loadStore() store {
	if path, err := storePath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			var s store
			if json.Unmarshal(b, &s) == nil {
				return s
			}
		}
	}
	return migrateLegacy()
}

// migrateLegacy folds the pre-consolidation config files into one Default profile.
// All-zero when nothing exists (a true fresh install → onboarding runs).
func migrateLegacy() store {
	p := tts.LoadPrefs()
	lp := loadLayoutPrefs()
	cp := loadCuePrefs()
	return store{
		Configured: p.Configured,
		Default: charSettings{
			AudioOn: p.Enabled,
			Voice:   p.Voice,
			Damage:  lp.Damage,
			OffDef:  lp.OffDef,
			Cues:    cp.Overrides,
		},
	}
}

func saveStore(s store) error {
	path, err := storePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
