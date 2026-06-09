package loader

import "strings"

// PowerShell-snippet builders for the Windows native dialogs (used by
// locate_windows.go). They're kept here, untagged and pure, so the quoting/
// escaping — the easy thing to get subtly wrong — is unit-testable on any host.

// psQuote escapes a string for embedding inside a single-quoted PowerShell
// literal: single quotes are doubled. Single-quoted literals are used
// throughout so a path can't trigger "$" interpolation or backtick escapes.
func psQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

// confirmFolderScript builds the Yes/No message box asking whether to use dir.
// Line breaks come from [Environment]::NewLine (a single-quoted "`n" would print
// literally), and the title is ASCII (non-ASCII can mangle through -Command).
func confirmFolderScript(dir string) string {
	return "Add-Type -AssemblyName System.Windows.Forms; " +
		"[System.Windows.Forms.MessageBox]::Show(" +
		"'Use this EverQuest folder?' + [Environment]::NewLine + [Environment]::NewLine + '" + psQuote(dir) + "', " +
		"'99dps - EverQuest folder', 'YesNo', 'Question')"
}

// pickFolderScript builds the folder-browse dialog; the chosen path is written
// to stdout (no trailing newline) for the caller to read.
func pickFolderScript() string {
	return "Add-Type -AssemblyName System.Windows.Forms; " +
		"$f = New-Object System.Windows.Forms.FolderBrowserDialog; " +
		"$f.Description = 'Select your EverQuest folder'; " +
		"if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($f.SelectedPath) }"
}
