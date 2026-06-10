package session

import (
	"99dps/internal/common"
	"sync"
	"time"
)

// Segmentation tuning. A fight closes when combat goes quiet for longer than
// segPulseK × the fight's rolling activity "pulse", clamped to [floor, ceil]
// seconds. The pulse is an EWMA of the gaps between combat exchanges, so a
// frantic AoE pull and a slow tank-and-spank are each judged by their own
// rhythm rather than one fixed timeout. Only actual exchanges (melee, magic,
// swings) drive this — kills/xp/casts/crits don't — so a multi-mob pull with
// several kills stays a single encounter.
// Defaults tuned against real P99 logs (see the parameter sweep that informed
// these): dense melee collapses the pulse to ~1-2s, so the floor is the
// effective timeout for normal fights, while the adaptive widening (up to the
// ceiling) keeps slow/sparse caster fights from splitting. floor=10 brought
// session counts just under the kill count (the right side of over- vs
// under-splitting); 6 over-split, 15 began merging distinct pulls.
const (
	segGapFloor  = 10  // never split on a gap shorter than this (seconds)
	segGapCeil   = 20  // always split after this much silence
	segPulseK    = 3   // close at k × pulse
	segPulseSeed = 4.0 // seed pulse before a fight reveals its cadence
	segEWMAAlpha = 0.3 // EWMA weight on the newest gap
)

type SessionManager struct {
	sessions      []*CombatSession
	activeSession int
	mu            sync.RWMutex

	// live segmentation state for the active fight (not snapshotted).
	lastActivity int64   // timestamp of the last combat exchange
	pulse        float64 // EWMA of inter-activity gaps, in seconds
	forceRoll    bool    // a hard boundary closed the fight; next event opens a new one
}

// Apply routes one melee damage event, opening or rolling a session per the
// adaptive cadence rule.
func (sm *SessionManager) Apply(set *common.DamageSet) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.activeForLocked(set.ActionTime).adjustDamageLocked(set)
}

// ApplySwing folds an avoided melee attempt into the fight. Swings are combat
// activity, so they keep a session alive (and can open one) — a stretch of
// nothing but misses no longer splits a fight.
func (sm *SessionManager) ApplySwing(sw *common.Swing) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.activeForLocked(sw.ActionTime).applySwingLocked(sw)
}

// activeForLocked returns the session an event at actionTime belongs to,
// rolling to a fresh one when combat has been quiet past the adaptive
// threshold. Caller holds the write lock.
func (sm *SessionManager) activeForLocked(actionTime int64) *CombatSession {
	if len(sm.sessions) == 0 {
		return sm.openSessionLocked(actionTime)
	}

	// a hard boundary (death/zone/camp) closed the previous fight
	if sm.forceRoll {
		sm.forceRoll = false
		return sm.openSessionLocked(actionTime)
	}

	active := sm.sessions[sm.activeSession]
	gap := actionTime - sm.lastActivity
	if gap < 0 {
		gap = 0 // out-of-order or duplicate log timestamps
	}

	if gap > sm.closeThresholdLocked() {
		active.end = time.Unix(sm.lastActivity, 0)
		return sm.openSessionLocked(actionTime)
	}

	// same fight — fold the gap into the rolling pulse
	sm.pulse = segEWMAAlpha*float64(gap) + (1-segEWMAAlpha)*sm.pulse
	sm.lastActivity = actionTime
	return active
}

// closeThresholdLocked is the current idle-out threshold, in seconds.
func (sm *SessionManager) closeThresholdLocked() int64 {
	t := int64(segPulseK * sm.pulse)
	if t < segGapFloor {
		return segGapFloor
	}
	if t > segGapCeil {
		return segGapCeil
	}
	return t
}

// openSessionLocked appends a fresh, initialized session and resets the
// segmentation state to this fight's start.
func (sm *SessionManager) openSessionLocked(actionTime int64) *CombatSession {
	cs := &CombatSession{
		start:      time.Unix(actionTime, 0),
		lastTime:   actionTime,
		aggressors: make(map[string]common.DamageStat),
	}
	sm.sessions = append(sm.sessions, cs)
	sm.activeSession = len(sm.sessions) - 1
	sm.lastActivity = actionTime
	sm.pulse = segPulseSeed
	return cs
}

// ApplyCrit records a critical hit on the active session (never rolls).
func (sm *SessionManager) ApplyCrit(cr *common.Crit) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.sessions) == 0 {
		return
	}
	sm.sessions[sm.activeSession].applyCritLocked(cr)
}

// ApplyEvent folds a kill / xp line into the active session. Death, zoning, and
// camping are hard boundaries: they close the current fight so the next combat
// exchange starts a fresh session (kills do not — a multi-mob pull stays one).
func (sm *SessionManager) ApplyEvent(e *common.Event) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.sessions) == 0 {
		return
	}
	sm.sessions[sm.activeSession].applyEventLocked(e)
	if e.Kind == common.EventDeath || e.Kind == common.EventZone {
		sm.endSessionLocked(e.ActionTime)
	}
}

// endSessionLocked marks the active fight ended (at the last activity) and
// arms forceRoll so the next exchange opens a new session.
func (sm *SessionManager) endSessionLocked(at int64) {
	s := sm.sessions[sm.activeSession]
	if s.end.IsZero() {
		end := sm.lastActivity
		if end == 0 {
			end = at
		}
		s.end = time.Unix(end, 0)
	}
	sm.forceRoll = true
}

// ApplyMagic folds a non-melee damage line into the fight. Spell damage is a
// combat exchange, so it drives segmentation (and can open a fight for a pure
// caster the mob never melees back).
func (sm *SessionManager) ApplyMagic(m *common.Magic) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.activeForLocked(m.ActionTime).applyMagicLocked(m)
}

// Current returns a deep snapshot of the active session, or nil. The returned
// value is owned by the caller — iterating it requires no lock.
func (sm *SessionManager) Current() *CombatSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if len(sm.sessions) == 0 {
		return nil
	}
	return sm.sessions[sm.activeSession].snapshot()
}

// All returns deep snapshots of every session.
func (sm *SessionManager) All() []*CombatSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if len(sm.sessions) == 0 {
		return nil
	}
	out := make([]*CombatSession, len(sm.sessions))
	for i, s := range sm.sessions {
		out[i] = s.snapshot()
	}
	return out
}

// Len is the number of recorded sessions.
func (sm *SessionManager) Len() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

func (sm *SessionManager) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions = nil
	sm.activeSession = 0
	sm.lastActivity = 0
	sm.pulse = 0
	sm.forceRoll = false
}
