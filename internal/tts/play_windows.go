//go:build windows

package tts

import (
	"os/exec"
	"strings"
	"syscall"
)

// hideWindow stops the spawned PowerShell from flashing a console over the TUI.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// playWav plays a WAV file via the built-in System.Media.SoundPlayer (always
// present on Windows, no install), detached so it never blocks the UI.
func playWav(path string) {
	ps, err := exec.LookPath("powershell")
	if err != nil {
		return
	}
	safe := strings.ReplaceAll(path, "'", "''") // escape for the PS string literal
	script := "(New-Object System.Media.SoundPlayer '" + safe + "').PlaySync()"
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script)
	hideWindow(cmd)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }() // reap
	}
}
