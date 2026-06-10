//go:build !windows

package cli

import "os/exec"

// newSpeaker picks the first available Unix TTS engine. spd-say
// (speech-dispatcher) is preferred — asynchronous and desktop-integrated;
// espeak is a fallback (run detached either way). No engine found → silent.
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
			bin, args := p, c.args
			return &speaker{build: func(text string) *exec.Cmd {
				return exec.Command(bin, append(append([]string{}, args...), text)...)
			}}
		}
	}
	return &speaker{}
}
