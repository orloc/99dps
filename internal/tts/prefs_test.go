package tts

import "testing"

func TestPrefsRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate os.UserConfigDir

	// a fresh install reads the zero value (→ setup screen will run)
	if p := LoadPrefs(); p.Configured {
		t.Fatalf("fresh install should be unconfigured, got %+v", p)
	}

	want := Prefs{Configured: true, Enabled: true, Voice: "7"}
	if err := SavePrefs(want); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}
	if got := LoadPrefs(); got != want {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}
