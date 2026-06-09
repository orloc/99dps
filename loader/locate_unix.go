//go:build !windows

package loader

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// candidateRoots lists likely places an EverQuest install lives on Linux/macOS:
// the user's common folders and a Wine drive_c (P99 usually runs under Wine), the
// exe's own directory (drop-in case), plus shallow mount points people park a
// game install on.
func candidateRoots() []string {
	var roots []string
	add := func(p string) {
		if p != "" {
			roots = append(roots, p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, sub := range []string{
			"Desktop", "Downloads", "Games", "Documents",
			".wine/drive_c", ".wine/drive_c/Program Files",
		} {
			add(filepath.Join(home, sub))
		}
		add(home)
	}
	add("/mnt")
	add("/opt")
	if exe, err := os.Executable(); err == nil {
		add(filepath.Dir(exe))
	}
	return roots
}

// DetectLogDirs scans the likely Unix/Wine locations for an EQ install.
func DetectLogDirs() []string { return scanForEQ(candidateRoots()) }

// PromptForLogDir is the Unix equivalent of the Windows native-dialog flow: a
// console prompt (the app launches from a terminal, so stdin is available before
// the TUI takes over). It lists detected installs to confirm/choose, or asks the
// user to type a path. Returns the chosen log dir, or "" if skipped.
func PromptForLogDir(found []string) string {
	in := bufio.NewReader(os.Stdin)

	if len(found) > 0 {
		fmt.Println("Found EverQuest log folder(s):")
		for i, d := range found {
			fmt.Printf("  %d) %s\n", i+1, d)
		}
		fmt.Print("Use which? [number], or type a path (Enter to skip): ")
		line, _ := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return ""
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(found) {
			return found[n-1]
		}
		return logDirFromChoice(line) // they typed a path instead of a number
	}

	fmt.Print("Couldn't find your EverQuest folder. Enter its log directory (Enter to skip): ")
	line, _ := in.ReadString('\n')
	return logDirFromChoice(strings.TrimSpace(line))
}
