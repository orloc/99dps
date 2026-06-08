package cli

import "os/exec"

// speaker shells out to a Linux TTS engine for audio cues. Output is discarded
// so the engine can never print over the gocui display.
type speaker struct {
	bin  string
	args []string
}

// newSpeaker picks the first available TTS engine. spd-say (speech-dispatcher)
// is preferred because it's asynchronous and desktop-integrated; espeak is a
// blocking fallback (we run it detached either way).
func newSpeaker() *speaker {
	for _, c := range []struct {
		bin  string
		args []string
	}{
		{"spd-say", nil},
		{"espeak-ng", nil},
		{"espeak", nil},
	} {
		if p, err := exec.LookPath(c.bin); err == nil {
			return &speaker{bin: p, args: c.args}
		}
	}
	return &speaker{}
}

// available reports whether a TTS engine was found.
func (s *speaker) available() bool { return s.bin != "" }

// say speaks text without blocking. stdout/stderr go to /dev/null so nothing
// leaks onto the terminal.
func (s *speaker) say(text string) {
	if s.bin == "" {
		return
	}
	cmd := exec.Command(s.bin, append(append([]string{}, s.args...), text)...)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }() // reap, don't leave zombies
	}
}
