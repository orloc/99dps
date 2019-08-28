package session

import (
	"99dps/common"
	"fmt"
	"sort"
	"sync"
	"time"
)

const CS_THRESHOLD = 8

type SessionManager struct {
	Sessions      []CombatSession
	activeSession int
	Mutex         *sync.RWMutex
}

/*
 * 	An active session is defined as follows:
 *  	- T seconds has not passed since last damage
 */
func (sm *SessionManager) GetActiveSession(set *common.DamageSet) *CombatSession {
	// if there isn't a session create one
	if len(sm.Sessions) == 0 {
		sm.addSession()
		sm.activeSession = 0
	}

	s := &sm.Sessions[sm.activeSession]

	// combat has ended - make a new session
	if set.ActionTime-s.LastTime >= CS_THRESHOLD {
		s.end = time.Unix(s.LastTime, 0)
		sm.addSession()
		sm.activeSession = len(sm.Sessions) - 1
		s = &sm.Sessions[sm.activeSession]
	}

	return s
}

func (sm *SessionManager) Current() *CombatSession {
	if len(sm.Sessions) == 0 {
		return nil
	}
	sm.Mutex.RLock()
	defer sm.Mutex.RUnlock()
	s := sm.Sessions[sm.activeSession]
	return &s
}

func (sm *SessionManager) Clear() {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()
	sm.Sessions = []CombatSession{}
}

func (sm *SessionManager) All() []CombatSession {
	if len(sm.Sessions) == 0 {
		return nil
	}

	sm.Mutex.RLock()
	defer sm.Mutex.RUnlock()

	var cs []CombatSession
	for _, as := range sm.Sessions {
		cs = append(cs, as)
	}

	return cs
}

func (sm *SessionManager) PrintDps(s *CombatSession) string {
	if s == nil {
		return "Session disappeared!"
	}
	summary := fmt.Sprintf("Started : %s \nEnded: %s\nDuration: %.2fm\n\n", s.start.String(), s.end.String(), time.Unix(s.LastTime, 0).Sub(s.start).Minutes())

	summary = fmt.Sprintf("%s|%-4s|%-20s|%-5s|%-10s|%-5s|%-5s|\n", summary, "#", "Dealer", "Dps", "Total", "High", "Low")

	var stats []common.DamageStat

	for _, v := range s.aggressors {
		stats = append(stats, v)
	}

	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })

	for i, v := range stats {
		dps := s.computeDPS(v.CombatRecords, v.Total)
		k := v.CombatRecords[0].Dealer
		summary = fmt.Sprintf("%s\n|%-4d|%-20s|%-5v|%-10v|%-5v|%-5v|", summary, i+1, k, dps, v.Total, v.High, v.Low)
	}

	return summary
}

func (sm *SessionManager) addSession() {
	cs := CombatSession{}
	sm.Sessions = append(sm.Sessions, cs)
}
