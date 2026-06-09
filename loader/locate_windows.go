//go:build windows

package loader

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// candidateRoots lists the likely places an EverQuest / Project 1999 install
// lives on Windows: well-known fixed paths, the standard Sony/Daybreak install
// dirs, the user's Desktop/Downloads/Games/My Games (where players often drop a
// P99 folder), and the directory the exe itself sits in (drop-in case).
func candidateRoots() []string {
	var roots []string
	add := func(p string) {
		if p != "" {
			roots = append(roots, p)
		}
	}

	for _, p := range []string{
		`C:\P99`, `C:\Project1999`, `C:\EverQuest`, `C:\EQ`, `C:\Titanium`, `C:\Games`,
		`C:\Program Files\Sony\EverQuest`,
		`C:\Program Files (x86)\Sony\EverQuest`,
		`C:\Users\Public\Daybreak Game Company\Installed Games\EverQuest`,
	} {
		add(p)
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		add(filepath.Join(pf, "Sony", "EverQuest"))
	}
	if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
		add(filepath.Join(pf, "Sony", "EverQuest"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, sub := range []string{"Desktop", "Downloads", "Games", filepath.Join("Documents", "My Games")} {
			add(filepath.Join(home, sub))
		}
	}
	if exe, err := os.Executable(); err == nil {
		add(filepath.Dir(exe))
	}
	return roots
}

// DetectLogDirs scans the likely Windows locations for an EQ install and returns
// the log directories found.
func DetectLogDirs() []string { return scanForEQ(candidateRoots()) }

// PromptForLogDir runs the standard Windows flow: for each auto-detected
// install, ask the user to confirm it (Yes/No message box); if none is detected
// or all are declined, open a native folder picker. Returns the chosen log dir,
// or "" if the user cancels.
func PromptForLogDir(found []string) string {
	for _, d := range found {
		if confirmDir(d) {
			return d
		}
	}
	picked := pickDir()
	if picked == "" {
		return ""
	}
	if d := eqLogDirFrom(picked); d != "" {
		return d // recognized an EQ folder (or its Logs subdir)
	}
	if logs := filepath.Join(picked, "Logs"); DirHasLogs(logs) {
		return logs
	}
	return picked // take it as-is; the meter will report if it holds no logs
}

// powershell runs a PowerShell snippet and returns its trimmed stdout, or "" on
// any failure (so a missing/blocked PowerShell degrades to no prompt).
func powershell(script string) string {
	ps, err := exec.LookPath("powershell")
	if err != nil {
		return ""
	}
	out, err := exec.Command(ps, "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// confirmDir shows a native Yes/No box asking whether to use dir.
func confirmDir(dir string) bool {
	safe := strings.ReplaceAll(dir, "'", "''") // escape for the PS single-quoted string
	script := "Add-Type -AssemblyName System.Windows.Forms; " +
		"[System.Windows.Forms.MessageBox]::Show(" +
		"'Use this EverQuest folder?`n`n" + safe + "'," +
		"'99dps — EverQuest folder','YesNo','Question')"
	return powershell(script) == "Yes"
}

// pickDir opens the native folder-browse dialog and returns the selected path,
// or "" if cancelled.
func pickDir() string {
	script := "Add-Type -AssemblyName System.Windows.Forms; " +
		"$f = New-Object System.Windows.Forms.FolderBrowserDialog; " +
		"$f.Description = 'Select your EverQuest folder'; " +
		"if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($f.SelectedPath) }"
	return powershell(script)
}
