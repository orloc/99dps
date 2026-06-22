package gamestate

import "99dps/internal/eq/eqclass"

// DiscPhase is where a discipline is in its use cycle.
type DiscPhase int

const (
	DiscReady   DiscPhase = iota // available now
	DiscActive                   // the effect is running (within its duration)
	DiscCooling                  // unavailable: the shared discipline timer is running
)

// DisciplineTimer is one discipline's live state for the widget: the static
// facts (name/level/durations) plus its current phase and the seconds left in
// that phase (0 for Ready). While the shared timer runs, LockedBy names the
// discipline that holds it and LockTotal is that timer's full length (for the
// countdown bar) — in classic EQ activating one discipline locks out ALL of them
// until its reuse expires.
type DisciplineTimer struct {
	eqclass.Discipline
	Phase     DiscPhase
	Remaining int64
	LockedBy  string // discipline holding the shared lockout (Cooling phase; == Name for the one you used)
	LockTotal int64  // the shared lockout's full length, seconds (for the bar)
}

// sharedDiscLockLocked finds the discipline governing the shared reuse: the
// player's most-recently-activated one (stale cross-class names in discAt are
// ignored — only the current class's disciplines are considered). locked is true
// while that discipline's reuse has not yet elapsed. Caller holds the lock.
func (t *Tracker) sharedDiscLockLocked(now int64) (used eqclass.Discipline, usedAt int64, locked bool) {
	for _, d := range eqclass.DisciplinesFor(t.class) {
		if at := t.cool.discAt[d.Name]; at > usedAt {
			used, usedAt = d, at
		}
	}
	return used, usedAt, usedAt != 0 && now < usedAt+used.ReuseSec
}

// disciplineTimersLocked returns the player's level-earned disciplines, each with
// its live phase under the SHARED discipline reuse: all combat disciplines share
// one timer, so activating any discipline locks out every other until that
// discipline's reuse expires. The discipline that holds the lock shows Active for
// its duration then counts its reuse down; every other discipline shows the same
// countdown (Cooling, LockedBy the held one). Caller holds the lock.
func (t *Tracker) disciplineTimersLocked(now int64) []DisciplineTimer {
	list := eqclass.DisciplinesFor(t.class)
	if len(list) == 0 {
		return nil
	}
	used, usedAt, locked := t.sharedDiscLockLocked(now)
	readyAt := usedAt + used.ReuseSec

	out := make([]DisciplineTimer, 0, len(list))
	for _, d := range list {
		// hide disciplines above the player's level (show all until a /who/level
		// is known, so a fresh launch isn't blank).
		if t.level > 0 && d.Level > t.level {
			continue
		}
		dt := DisciplineTimer{Discipline: d}
		switch {
		case !locked:
			dt.Phase = DiscReady
		case d.Name == used.Name && now < usedAt+used.DurSec:
			dt.Phase, dt.Remaining = DiscActive, usedAt+used.DurSec-now
		default:
			// locked out by the shared timer — this disc's own reuse (the one you
			// used) or another's. Every locked discipline shares the same ready time.
			dt.Phase, dt.Remaining = DiscCooling, readyAt-now
			dt.LockedBy, dt.LockTotal = used.Name, used.ReuseSec
		}
		out = append(out, dt)
	}
	return out
}

// DisciplineLock reports the shared-lockout state at `now`: the discipline that
// holds the timer, how long until ANY discipline can be used again, and whether a
// lockout is active. (All disciplines share one reuse — using one blocks the rest.)
func (t *Tracker) DisciplineLock(now int64) (name string, remaining int64, locked bool) {
	if t == nil {
		return "", 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	used, usedAt, on := t.sharedDiscLockLocked(now)
	if !on {
		return "", 0, false
	}
	return used.Name, usedAt + used.ReuseSec - now, true
}

// matchDisciplineLocked records a discipline activation when body is its exact
// self-emote, and infers the owning class when that emote is class-exclusive (all
// currently-known emotes are). Recording is unconditional: discAt is keyed by
// name and only ever read for the detected class's own disciplines
// (disciplineTimersLocked), so a name the class doesn't have is harmless. Caller
// holds the lock. body is already NormEmote-normalized by Observe.
func (t *Tracker) matchDisciplineLocked(body string, at int64) {
	name, class, inferClass, ok := eqclass.DisciplineActivation(body)
	if !ok {
		return
	}
	t.cool.discAt[name] = at
	if inferClass {
		t.inferClassLocked(class)
	}
}

// DisciplineUsed records that the player activated a discipline at log time `at`
// — starting its active window and reuse cooldown. No-op for a name the class
// doesn't have. (The live path is matchDisciplineLocked, driven by the activation
// emote; this stays as a direct API for tests and explicit callers.)
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
