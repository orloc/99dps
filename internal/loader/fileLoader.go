package loader

import (
	"99dps/internal/common"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hpcloud/tail"
)

// DefaultLogDir is the fallback EverQuest log directory, used when neither the
// -logdir flag nor the EQ_LOG_DIR environment variable is set. Its value is
// platform-specific (see defaultdir_*.go): the EQ folder layout is identical on
// every OS, so once the log dir is known, sibling files like spells_us.txt are
// located relative to it (<logdir>/../spells_us.txt) regardless of platform.

type LogSource struct {
	Tail      *tail.Tail
	Path      string
	Character string
}

// LoadFile picks the most-recently-active eqlog_*.txt under dir and follows it
// from the start of the file.
func LoadFile(dir string) *LogSource {
	path, err := Latest(dir)
	common.CheckErr(err)
	src, err := Follow(path, false)
	common.CheckErr(err)
	return src
}

var eqlogName = regexp.MustCompile(`^eqlog_.*\.txt$`)

// Latest returns the path of the most-recently-modified eqlog_*.txt under dir.
// Symlinks are resolved (so a symlinked Logs directory works) and the mtime is
// read via Stat (following the link). Safe to call repeatedly — it returns an
// error rather than panicking, so the character-switch poller can use it.
func Latest(dir string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}

	var newest os.FileInfo
	var newestName string
	for _, entry := range entries {
		if !eqlogName.MatchString(entry.Name()) {
			continue
		}
		info, err := os.Stat(filepath.Join(root, entry.Name()))
		if err != nil || info.IsDir() {
			continue
		}
		if newest == nil || info.ModTime().After(newest.ModTime()) {
			newest, newestName = info, entry.Name()
		}
	}

	if newest == nil {
		return "", fmt.Errorf("no eqlog_*.txt files in %s", dir)
	}
	return filepath.Join(root, newestName), nil
}

// Follow opens a tail on a specific eqlog file. fromEnd starts at the end of the
// file — used on a character switch so only new combat is read, not the new
// character's entire history — otherwise it follows from the beginning.
func Follow(path string, fromEnd bool) (*LogSource, error) {
	// DiscardingLogger: the tail library otherwise writes "Seeked …"/"Stopping
	// tail …" lines to os.Stderr, which corrupt the gocui TUI (most visibly on a
	// character switch, which opens a new tail with a SeekEnd location).
	cfg := tail.Config{Follow: true, Logger: tail.DiscardingLogger}
	if fromEnd {
		cfg.Location = &tail.SeekInfo{Whence: io.SeekEnd}
	}
	t, err := tail.TailFile(path, cfg)
	if err != nil {
		return nil, err
	}
	return &LogSource{Tail: t, Path: path, Character: parseCharacter(path)}, nil
}

// parseCharacter pulls the character name out of an eqlog_<Char>_<server>.txt
// filename. Returns "" if the name doesn't match the expected shape.
func parseCharacter(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".txt")
	parts := strings.SplitN(base, "_", 3)
	if len(parts) < 2 || parts[0] != "eqlog" {
		return ""
	}
	return parts[1]
}
