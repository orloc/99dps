//go:build windows

package cli

import (
	"os/exec"
	"strings"
	"syscall"
)

// newSpeaker uses the built-in Windows SAPI voice via PowerShell's
// System.Speech — always present on Windows, so audio cues work out of the box
// with no install. Speak() blocks, but say() starts it detached. The console
// window is hidden so the spawned PowerShell never flashes over the TUI.
func newSpeaker() *speaker {
	ps, err := exec.LookPath("powershell")
	if err != nil {
		return &speaker{} // no PowerShell (unusual) → silent
	}
	return &speaker{build: func(text string) *exec.Cmd {
		safe := strings.ReplaceAll(text, "'", "''") // escape for the PS string literal
		script := "Add-Type -AssemblyName System.Speech; " +
			"(New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak('" + safe + "')"
		cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd
	}}
}
