package tts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrefs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate os.UserConfigDir

	// a fresh install reads the zero value (→ setup screen will run)
	if p := LoadPrefs(); p.Configured {
		t.Fatalf("fresh install should be unconfigured, got %+v", p)
	}

	// a previously-written choice (the legacy on-disk format) reads back intact
	path, err := prefsPath()
	if err != nil {
		t.Fatalf("prefsPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"configured":true,"enabled":true,"voice":"7"}`), 0o644); err != nil {
		t.Fatalf("seed prefs: %v", err)
	}
	want := Prefs{Configured: true, Enabled: true, Voice: "7"}
	if got := LoadPrefs(); got != want {
		t.Errorf("LoadPrefs = %+v, want %+v", got, want)
	}
}
