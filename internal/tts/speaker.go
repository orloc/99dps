package tts

import "os/exec"

// speaker shells out to a platform TTS engine for audio cues. Output is
// discarded so the engine can never print over the TUI display. The engine is
// chosen per-OS in speech_unix.go / speech_windows.go via newSpeaker.
type Speaker struct {
	// build constructs the command that speaks text, or is nil when no engine is
	// available (cues then silently no-op).
	build func(text string) *exec.Cmd
}

// available reports whether a TTS engine was found.
func (s *Speaker) Available() bool { return s != nil && s.build != nil }

// say speaks text without blocking — the process is started detached and reaped
// in the background so a slow/blocking engine never stalls the UI.
func (s *Speaker) Say(text string) {
	if !s.Available() {
		return
	}
	cmd := s.build(text)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }() // reap, don't leave zombies
	}
}
