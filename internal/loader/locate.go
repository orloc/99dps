package loader

import (
	"os"
	"path/filepath"
	"strings"
)

// This file is the cross-platform core of EQ-directory discovery: detecting an
// install from a set of candidate folders and remembering the chosen log dir
// across runs. The interactive bits (auto-detect roots + native dialogs) are
// platform-specific — see locate_windows.go / locate_other.go.

// DirHasLogs reports whether dir directly contains EQ log files (eqlog_*.txt).
func DirHasLogs(dir string) bool {
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "eqlog_") && strings.HasSuffix(e.Name(), ".txt") {
			return true
		}
	}
	return false
}

// isEQRoot reports whether dir looks like an EverQuest install folder by the
// presence of a signature client file.
func isEQRoot(dir string) bool {
	for _, marker := range []string{"eqgame.exe", "eqclient.ini", "spells_us.txt"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// eqLogDirFrom resolves the log directory for an EQ-install candidate: the dir
// itself if it holds logs, else its Logs subfolder if that does, else — when dir
// is clearly an EQ root but logging isn't on yet — the (possibly empty) Logs
// subfolder so the meter is ready once logging starts. "" when dir isn't EQ.
func eqLogDirFrom(dir string) string {
	if DirHasLogs(dir) {
		return dir
	}
	logs := filepath.Join(dir, "Logs")
	if DirHasLogs(logs) {
		return logs
	}
	if isEQRoot(dir) {
		return logs
	}
	return ""
}

// scanForEQ checks each candidate directory and its immediate subdirectories for
// an EverQuest install, returning the unique log directories found, in order.
// It only descends one level, so it stays fast even over a big folder like the
// Desktop or C:\Games.
func scanForEQ(candidates []string) []string {
	var found []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		found = append(found, d)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		add(eqLogDirFrom(c))
		entries, err := os.ReadDir(c)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				add(eqLogDirFrom(filepath.Join(c, e.Name())))
			}
		}
	}
	return found
}

// logDirFromChoice resolves a user-picked path — an EQ folder, its Logs
// subfolder, or a raw logs path — to a usable log directory. It falls back to
// the path as-is so a mistaken pick surfaces as "no logs" rather than silently
// doing nothing. "" in, "" out.
func logDirFromChoice(path string) string {
	if path == "" {
		return ""
	}
	if d := eqLogDirFrom(path); d != "" {
		return d
	}
	if logs := filepath.Join(path, "Logs"); DirHasLogs(logs) {
		return logs
	}
	return path
}

// configPath is where the chosen log dir is remembered between runs
// (%APPDATA%\99dps\logdir.txt on Windows, ~/.config/99dps/logdir.txt on Unix).
func configPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "99dps", "logdir.txt")
}

// SavedLogDir returns the previously chosen log dir, or "" if none is saved.
func SavedLogDir() string {
	p := configPath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SaveLogDir persists the chosen log dir for next time (best-effort; a failure
// to write just means we'll re-detect next run).
func SaveLogDir(dir string) {
	p := configPath()
	if p == "" || dir == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
		_ = os.WriteFile(p, []byte(dir), 0o644)
	}
}
