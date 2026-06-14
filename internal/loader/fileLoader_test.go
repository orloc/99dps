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

// Latest resolves through a symlinked log dir (so a symlinked Logs folder
// works) and errors on a non-existent directory.
func TestLatest_SymlinkAndMissingDir(t *testing.T) {
	real := t.TempDir()
	p := filepath.Join(real, "eqlog_Kelkix_Server.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "Logs")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported on this host: %v", err)
	}
	got, err := Latest(link)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "eqlog_Kelkix_Server.txt" {
		t.Errorf("Latest via symlink = %q, want the eqlog file", got)
	}

	if _, err := Latest(filepath.Join(real, "nope")); err == nil {
		t.Error("Latest on a missing dir should error")
	}
}

// stopTail closes a tail and drains any buffered lines concurrently so Stop()
// (which waits for the tail goroutine to finish sending) can't deadlock on a
// full Lines channel. The hpcloud/tail goroutine blocks on a channel send until
// either consumed or the file is truncated.
func stopTail(t *testing.T, ls *LogSource) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for range ls.Tail.Lines { // drain until Stop closes the channel
		}
		close(done)
	}()
	_ = ls.Tail.Stop()
	ls.Tail.Cleanup()
	<-done
}

// Follow opens a tail on a specific file, parsing the character from its name.
// LoadFile picks the newest eqlog in a dir and follows it. Both must be Stop()ed.
// Files are left empty so the tail goroutine never blocks mid-send during Stop.
func TestFollowAndLoadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "eqlog_Yatiri_P1999Green.txt")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	src, err := Follow(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stopTail(t, src)
	if src.Character != "Yatiri" {
		t.Errorf("Follow character = %q, want Yatiri", src.Character)
	}
	if src.Path != p {
		t.Errorf("Follow path = %q, want %q", src.Path, p)
	}
	if src.Tail == nil {
		t.Error("Follow returned a nil tail")
	}

	ls, err := LoadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stopTail(t, ls)
	if ls.Character != "Yatiri" {
		t.Errorf("LoadFile character = %q, want Yatiri", ls.Character)
	}

	// LoadFile on a dir with no eqlog files surfaces Latest's error.
	if _, err := LoadFile(t.TempDir()); err == nil {
		t.Error("LoadFile on an empty dir should error")
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
