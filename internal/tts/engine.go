package tts

// Engine speaks short audio-cue phrases. Two implementations exist: the legacy
// OS engine (spd-say/espeak on Unix, SAPI via PowerShell on Windows) and, later,
// a neural backend (Kokoro via sherpa-onnx). Every method is safe to call on an
// unavailable engine — cues then silently no-op.
type Engine interface {
	// Say speaks text without blocking.
	Say(text string)
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
}

// Voice is a selectable speech voice, surfaced to the (future) settings screen.
type Voice struct {
	ID   string // stable identifier passed to SetVoice
	Name string // human-friendly label
}

// New builds the speech engine. For now it always returns the legacy OS engine;
// the neural backend will be selected here once its assets are present.
func New() Engine {
	return newLegacy()
}
