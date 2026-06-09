package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEQLogDirDetection(t *testing.T) {
	root := t.TempDir()
	eq := filepath.Join(root, "P99")
	logs := filepath.Join(eq, "Logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(eq, "eqclient.ini"), "Log=TRUE\n")
	writeFile(t, filepath.Join(logs, "eqlog_Kelkix_P1999.txt"), "hi\n")

	if !DirHasLogs(logs) {
		t.Error("DirHasLogs should see the eqlog file in Logs")
	}
	if DirHasLogs(eq) {
		t.Error("the EQ root itself holds no eqlog_*.txt")
	}
	// the root resolves to its Logs subfolder
	if got := eqLogDirFrom(eq); got != logs {
		t.Errorf("eqLogDirFrom(root) = %q, want %q", got, logs)
	}
	// a non-EQ folder yields nothing
	if got := eqLogDirFrom(t.TempDir()); got != "" {
		t.Errorf("eqLogDirFrom(non-EQ) = %q, want empty", got)
	}
	// scanning the parent finds the install one level down
	if found := scanForEQ([]string{root}); len(found) != 1 || found[0] != logs {
		t.Errorf("scanForEQ = %v, want [%q]", found, logs)
	}
}

func TestEQRootWithoutLogsYet(t *testing.T) {
	eq := t.TempDir()
	writeFile(t, filepath.Join(eq, "eqgame.exe"), "x")
	// logging not enabled yet (no Logs) — still point at the future Logs folder
	want := filepath.Join(eq, "Logs")
	if got := eqLogDirFrom(eq); got != want {
		t.Errorf("eqLogDirFrom(root, no logs yet) = %q, want %q", got, want)
	}
}

func TestSaveAndLoadLogDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // redirect os.UserConfigDir on this host
	if SavedLogDir() != "" {
		t.Fatal("expected no saved dir initially")
	}
	SaveLogDir("/some/eq/Logs")
	if got := SavedLogDir(); got != "/some/eq/Logs" {
		t.Errorf("round-trip = %q, want /some/eq/Logs", got)
	}
}

func TestLogDirFromChoice(t *testing.T) {
	if logDirFromChoice("") != "" {
		t.Error("empty choice should stay empty")
	}
	// an EQ root resolves to its Logs subfolder
	eq := t.TempDir()
	writeFile(t, filepath.Join(eq, "eqgame.exe"), "x")
	if got, want := logDirFromChoice(eq), filepath.Join(eq, "Logs"); got != want {
		t.Errorf("EQ root choice = %q, want %q", got, want)
	}
	// an unrecognized path is taken as-is (surfaces as "no logs" later)
	bogus := t.TempDir()
	if got := logDirFromChoice(bogus); got != bogus {
		t.Errorf("bogus choice = %q, want it returned as-is %q", got, bogus)
	}
}
