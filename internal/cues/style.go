package cues

// This file holds the two user-facing "cue style" knobs that shape what the
// engine announces, kept apart from the per-cue on/off toggles in prefs.go:
//
//   - Timing  — how far ahead a spawn cue fires (relaxed → fewer/later warnings,
//     early → earlier heads-up). It tunes only the spawn windows; the buff/debuff
//     fade zones stay driven by gamestate.TimerUrgency so audio matches the visual
//     countdown color.
//   - Chatter — how talkative the engine is: whether the gentle, non-critical cue
//     tier fires at all (Quiet drops it) and how wordy each phrase is (terse vs
//     full sentences). Urgent combat alerts always fire regardless.
//
// Both default to their zero value (Normal = today's behavior), so an old
// settings file with neither field set keeps the current experience.

// Timing selects how early spawn cues fire.
type Timing int

const (
	TimingNormal  Timing = iota // current behavior (zero value)
	TimingRelaxed               // later heads-up — less chatter about distant spawns
	TimingEarly                 // earlier heads-up — more lead time
)

// Next cycles to the following preset (for the Settings row), wrapping.
func (t Timing) Next() Timing { return (t + 1) % 3 }

// String is the Settings label.
func (t Timing) String() string {
	switch t {
	case TimingRelaxed:
		return "Relaxed"
	case TimingEarly:
		return "Early"
	default:
		return "Normal"
	}
}

// spawnWindows returns the spawn-cue lead times for this preset: lead is how far
// out the calm heads-up fires, imminent sizes the tight window around the actual
// pop for the urgent "spawning now" alert. Normal mirrors the spawnLeadSec /
// spawnImminentSec constants (the single source of truth lives there).
func (t Timing) spawnWindows() (lead, imminent int64) {
	switch t {
	case TimingRelaxed:
		return 30, spawnImminentSec
	case TimingEarly:
		return 120, 5
	default:
		return spawnLeadSec, spawnImminentSec
	}
}

// Chatter selects how talkative the engine is.
type Chatter int

const (
	ChatterNormal Chatter = iota // current behavior (zero value)
	ChatterQuiet                 // only urgent + fading cues, terse wording
	ChatterChatty                // every tier, fuller wording
)

// Next cycles to the following preset (for the Settings row), wrapping.
func (c Chatter) Next() Chatter { return (c + 1) % 3 }

// String is the Settings label.
func (c Chatter) String() string {
	switch c {
	case ChatterQuiet:
		return "Quiet"
	case ChatterChatty:
		return "Chatty"
	default:
		return "Normal"
	}
}

// speaksGentle reports whether the non-critical "gentle" cue tier should fire —
// the gold "running low" fade, cooldown ready/halfway, and the calm spawn-soon
// heads-up. Quiet suppresses them (urgent alerts and the red "fading" cue still
// fire); Normal and Chatty keep them.
func (c Chatter) speaksGentle() bool { return c != ChatterQuiet }
