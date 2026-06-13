//go:build !windows

package tts

import "os/exec"

// hideWindow is a no-op off Windows (no console window to hide).
func hideWindow(*exec.Cmd) {}

// playWav plays a WAV file through the first available CLI player, detached so it
// never blocks the UI. No player found → silent (same graceful-degrade contract
// as the legacy engine).
func playWav(path string) {
	for _, p := range [][]string{
		{"paplay"},
		{"aplay", "-q"},
		{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet"},
		{"play", "-q"}, // sox
	} {
		bin, err := exec.LookPath(p[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, append(p[1:], path)...)
		if cmd.Start() == nil {
			go func() { _ = cmd.Wait() }() // reap
		}
		return
	}
}
