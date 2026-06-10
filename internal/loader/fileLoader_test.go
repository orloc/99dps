package loader

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLatest_PicksMostRecentEqlog(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}

	write("eqlog_Old_Server.txt", 10*time.Minute)
	newest := write("eqlog_New_Server.txt", 1*time.Minute)
	write("notalog.txt", 0)             // ignored: wrong prefix
	write("eqlog_Recent_Server.log", 0) // ignored: wrong extension

	got, err := Latest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != newest {
		t.Errorf("Latest = %q, want %q", got, newest)
	}
}

func TestLatest_NoFilesErrors(t *testing.T) {
	if _, err := Latest(t.TempDir()); err == nil {
		t.Error("Latest on an empty dir should error, got nil")
	}
}

func TestParseCharacter(t *testing.T) {
	cases := map[string]string{
		"/mnt/logs/eqlog_Kelkix_P1999Green.txt": "Kelkix",
		"eqlog_Yatiri_P1999Green.txt":           "Yatiri",
		"eqlog_Bob_Server.txt":                  "Bob",
		"notalog.txt":                           "",
		"prefix_eqlog_Bob_Server.txt":           "", // must start with eqlog_
	}
	for path, want := range cases {
		if got := parseCharacter(path); got != want {
			t.Errorf("parseCharacter(%q) = %q, want %q", path, got, want)
		}
	}
}
