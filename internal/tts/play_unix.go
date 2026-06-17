//go:build !windows

package tts

import (
	"context"
	"os/exec"
)

// hideWindow is a no-op off Windows (no console window to hide).
func hideWindow(*exec.Cmd) {}

// playWav plays a WAV file through the first available CLI player and BLOCKS
// until playback finishes — the speaker's worker goroutine calls this, so
// blocking is what serializes cues (no overlap). A wedged player is killed after
// playTimeout so it can't stall every later cue. No player found → silent (same
// graceful-degrade contract as before).
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
		ctx, cancel := context.WithTimeout(context.Background(), playTimeout)
		_ = exec.CommandContext(ctx, bin, append(p[1:], path)...).Run() // block until done
		cancel()
		return
	}
}
