package main

import (
	"99dps/internal/loader"
	"flag"
	"os"
	"path/filepath"
)

func main() {
	logDir := flag.String("logdir", "", "directory containing eqlog_*.txt files (default: saved choice, EQ_LOG_DIR, or auto-detect)")
	logFile := flag.String("logfile", "", "replay/follow one specific eqlog file (debug; bypasses log-dir detection and character hot-swap)")
	spells := flag.String("spells", "", "path to spells_us.txt (default: next to the logs, else <logdir>/../spells_us.txt)")
	tts := flag.Bool("tts", false, "speak audio cues when your buffs get low (toggle in-app with 'a')")
	flag.Parse()

	// -logfile points the meter at a single file (a captured test log) without
	// touching the saved log-dir choice or the most-recently-modified heuristic.
	if *logFile != "" {
		dir := filepath.Dir(*logFile)
		spellsPath := *spells
		if spellsPath == "" {
			spellsPath = defaultSpellsPath(dir)
		}
		launchFile(*logFile, dir, spellsPath, *tts)
		return
	}

	dir := resolveLogDir(*logDir)

	spellsPath := *spells
	if spellsPath == "" {
		spellsPath = defaultSpellsPath(dir)
	}

	launchTUI(dir, spellsPath, *tts)
}

// defaultSpellsPath locates spells_us.txt for a given log directory. A real EQ
// install keeps it one level above the Logs folder (<logdir>/../spells_us.txt);
// a flat capture (e.g. a test log dropped in Downloads next to spells_us.txt)
// keeps it in the same directory. Prefer the sibling, fall back to the parent.
func defaultSpellsPath(logDir string) string {
	sibling := filepath.Join(logDir, "spells_us.txt")
	if _, err := os.Stat(sibling); err == nil {
		return sibling
	}
	return filepath.Join(filepath.Dir(logDir), "spells_us.txt")
}

// resolveLogDir decides which EverQuest log directory to use, in priority order:
//  1. an explicit -logdir flag (and remember it for next time),
//  2. the EQ_LOG_DIR environment variable,
//  3. a previously chosen+saved directory that still exists,
//  4. the platform default if it already holds logs (e.g. a relative "Logs" when
//     the exe sits in the EQ folder),
//  5. auto-detect installs and prompt the user (Windows: confirm/picker; no-op
//     elsewhere) — remembering whatever they choose.
//
// If nothing resolves it falls back to the platform default, and the meter shows
// "no logs" until pointed at a real directory.
func resolveLogDir(flagVal string) string {
	if flagVal != "" {
		loader.SaveLogDir(flagVal)
		return flagVal
	}
	if env := os.Getenv("EQ_LOG_DIR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		// a stale/typo'd EQ_LOG_DIR falls through to detection rather than
		// hard-exiting later when the directory can't be opened.
	}
	if saved := loader.SavedLogDir(); saved != "" {
		if _, err := os.Stat(saved); err == nil {
			return saved
		}
	}
	if loader.DirHasLogs(loader.DefaultLogDir) {
		return loader.DefaultLogDir
	}
	if chosen := loader.PromptForLogDir(loader.DetectLogDirs()); chosen != "" {
		loader.SaveLogDir(chosen)
		return chosen
	}
	return loader.DefaultLogDir
}
