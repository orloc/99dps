package main

import (
	"99dps/internal/loader"
	"flag"
	"os"
	"path/filepath"
)

func main() {
	logDir := flag.String("logdir", "", "directory containing eqlog_*.txt files (default: saved choice, EQ_LOG_DIR, or auto-detect)")
	spells := flag.String("spells", "", "path to spells_us.txt (default: <logdir>/../spells_us.txt)")
	tts := flag.Bool("tts", false, "speak audio cues when your buffs get low (toggle in-app with 'a')")
	flag.Parse()

	dir := resolveLogDir(*logDir)

	spellsPath := *spells
	if spellsPath == "" {
		spellsPath = filepath.Join(filepath.Dir(dir), "spells_us.txt")
	}

	launchTUI(dir, spellsPath, *tts)
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
