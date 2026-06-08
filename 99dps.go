package main

import (
	"99dps/loader"
	"flag"
	"os"
	"path/filepath"
)

func main() {
	defaultDir := loader.DefaultLogDir
	if env := os.Getenv("EQ_LOG_DIR"); env != "" {
		defaultDir = env
	}
	logDir := flag.String("logdir", defaultDir, "directory containing eqlog_*.txt files")
	spells := flag.String("spells", "", "path to spells_us.txt (default: <logdir>/../spells_us.txt)")
	tts := flag.Bool("tts", false, "speak audio cues when your buffs get low (toggle in-app with 'a')")
	flag.Parse()

	spellsPath := *spells
	if spellsPath == "" {
		spellsPath = filepath.Join(filepath.Dir(*logDir), "spells_us.txt")
	}

	launchCLI(*logDir, spellsPath, *tts)
}
