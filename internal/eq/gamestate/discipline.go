package gamestate

import "99dps/internal/eq/eqclass"

// DiscPhase is where a discipline is in its use cycle.
type DiscPhase int

const (
	DiscReady   DiscPhase = iota // available now
	DiscActive                   // the effect is running (within its duration)
	DiscCooling                  // used; counting down its reuse before it's ready again
)

// DisciplineTimer is one discipline's live state for the widget: the static
// facts (name/level/durations) plus its current phase and the seconds left in
// that phase (0 for Ready).
type DisciplineTimer struct {
	eqclass.Discipline
	Phase     DiscPhase
	Remaining int64
}

// disciplinePhaseLocked computes a discipline's phase at `now` from when it was
// last activated (0 = never). A discipline runs Active for its DurSec, then
// Cooling until ReuseSec from activation, then Ready. Caller holds the lock.
func disciplinePhaseLocked(d eqclass.Discipline, activatedAt, now int64) (DiscPhase, int64) {
	if activatedAt == 0 || now >= activatedAt+d.ReuseSec {
		return DiscReady, 0
	}
	if now < activatedAt+d.DurSec {
		return DiscActive, activatedAt + d.DurSec - now
	}
	return DiscCooling, activatedAt + d.ReuseSec - now
}

// disciplineTimersLocked returns the player's disciplines (those their level has
// earned), each with its live phase. Caller holds the lock.
func (t *Tracker) disciplineTimersLocked(now int64) []DisciplineTimer {
	list := eqclass.DisciplinesFor(t.class)
	if len(list) == 0 {
		return nil
	}
	out := make([]DisciplineTimer, 0, len(list))
	for _, d := range list {
		// hide disciplines above the player's level (show all until a /who/level
		// is known, so a fresh launch isn't blank).
		if t.level > 0 && d.Level > t.level {
			continue
		}
		phase, rem := disciplinePhaseLocked(d, t.cool.discAt[d.Name], now)
		out = append(out, DisciplineTimer{Discipline: d, Phase: phase, Remaining: rem})
	}
	return out
}

// DisciplineUsed records that the player activated a discipline at log time `at`
// — starting its active window and reuse cooldown. Called by the parser when it
// matches a discipline's activation line. No-op for a name the class doesn't have.
func (t *Tracker) DisciplineUsed(name string, at int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, d := range eqclass.DisciplinesFor(t.class) {
		if d.Name == name {
			t.cool.discAt[name] = at
			return
		}
	}
}

// Disciplines returns the player's discipline timers at wall-clock `now`
// (level-earned only), or nil for a class with no disciplines — the widget's
// gate. Like the skill cooldowns, discipline state persists across zoning.
func (t *Tracker) Disciplines(now int64) []DisciplineTimer {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.disciplineTimersLocked(now)
}
