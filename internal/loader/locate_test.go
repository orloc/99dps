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

// DirHasLogs is false for an empty path and for a non-existent directory (the
// ReadDir error path), and true only when an eqlog_*.txt sits directly inside.
func TestDirHasLogs_Edges(t *testing.T) {
	if DirHasLogs("") {
		t.Error("empty path should have no logs")
	}
	if DirHasLogs(filepath.Join(t.TempDir(), "nope")) {
		t.Error("missing dir should have no logs")
	}
	empty := t.TempDir()
	if DirHasLogs(empty) {
		t.Error("an empty dir holds no logs")
	}
	writeFile(t, filepath.Join(empty, "eqlog_Kelkix_Server.txt"), "x")
	if !DirHasLogs(empty) {
		t.Error("a dir with an eqlog_*.txt should report logs")
	}
}

// eqLogDirFrom prefers the dir itself when it directly holds logs, then a Logs
// subfolder that holds logs — exercising both positive branches.
func TestEQLogDirFrom_DirectAndSubfolder(t *testing.T) {
	direct := t.TempDir()
	writeFile(t, filepath.Join(direct, "eqlog_A_Srv.txt"), "x")
	if got := eqLogDirFrom(direct); got != direct {
		t.Errorf("eqLogDirFrom(direct logs) = %q, want %q", got, direct)
	}

	root := t.TempDir()
	logs := filepath.Join(root, "Logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(logs, "eqlog_B_Srv.txt"), "x")
	if got := eqLogDirFrom(root); got != logs {
		t.Errorf("eqLogDirFrom(Logs subfolder) = %q, want %q", got, logs)
	}
}

// scanForEQ de-duplicates: the same install reachable both directly and via the
// parent's directory walk yields one entry, and a non-EQ candidate is skipped.
func TestScanForEQ_Dedup(t *testing.T) {
	root := t.TempDir()
	eq := filepath.Join(root, "EverQuest")
	logs := filepath.Join(eq, "Logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(eq, "eqclient.ini"), "x")
	writeFile(t, filepath.Join(logs, "eqlog_C_Srv.txt"), "x")

	// pass both the EQ dir directly and its parent — must collapse to one result
	found := scanForEQ([]string{eq, root, "", filepath.Join(root, "missing")})
	if len(found) != 1 || found[0] != logs {
		t.Errorf("scanForEQ = %v, want exactly [%q]", found, logs)
	}
}

// logDirFromChoice resolves a raw Logs path (no EQ markers above it) to itself
// via the Logs-has-logs fallback, distinct from the as-is bogus path branch.
func TestLogDirFromChoice_RawLogsPath(t *testing.T) {
	parent := t.TempDir()
	logs := filepath.Join(parent, "Logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(logs, "eqlog_D_Srv.txt"), "x")
	// the parent isn't an EQ root, but its Logs subfolder holds logs
	if got := logDirFromChoice(parent); got != logs {
		t.Errorf("logDirFromChoice(parent of Logs) = %q, want %q", got, logs)
	}
}

// configPath/SavedLogDir return empty when no config dir can be resolved
// (UserConfigDir errors with HOME and XDG_CONFIG_HOME both unset on Unix).
func TestConfigPath_NoConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if p := configPath(); p != "" {
		t.Skipf("UserConfigDir still resolved (%q) on this host; skip", p)
	}
	if SavedLogDir() != "" {
		t.Error("SavedLogDir should be empty with no config dir")
	}
	SaveLogDir("/whatever") // must be a no-op, not panic
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
