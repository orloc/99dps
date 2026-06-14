package tts

// Engine speaks short audio-cue phrases via the neural Kokoro backend (sherpa-
// onnx). There is no robotic OS-voice fallback: until the voice is downloaded
// (EnsureAssets / the -tts-setup flow) the engine is simply unavailable and cues
// no-op. Every method is safe to call on an unavailable engine.
type Engine interface {
	// Say speaks text without blocking (normal, gentle delivery).
	Say(text string)
	// SayUrgent speaks text with a snappier delivery and jumps ahead of any
	// queued normal cues — for time-critical combat alerts (charm break, resist).
	SayUrgent(text string)
	// Available reports whether a working speech engine was found.
	Available() bool
	// Voices lists the selectable voices, or nil when the engine offers no
	// in-app voice picker (the legacy OS engine).
	Voices() []Voice
	// Voice is the currently selected voice ID, or "" when not applicable.
	Voice() string
	// SetVoice switches to the voice with the given ID, reporting whether it was
	// accepted (always false for an engine with no selectable voices).
	SetVoice(id string) bool
	// Flush drops any queued-but-not-yet-spoken normal cues (urgent alerts and the
	// currently-playing clip are left alone). Called when a mob dies so stale
	// "fading" cues about its debuffs don't keep playing after it's gone.
	Flush()
}

// Voice is a selectable speech voice, surfaced to the setup/settings screens.
type Voice struct {
	ID   string // stable identifier passed to SetVoice (the sherpa --sid index)
	Name string // voice name (e.g. af_bella)
	Desc string // human description (accent · gender · character)
}

// New builds the neural speech engine. It always returns a usable value; when
// the voice assets aren't downloaded yet the engine reports Available()==false
// and cues no-op until EnsureAssets (the -tts-setup flow) has run.
func New() Engine {
	return newKokoro()
}
