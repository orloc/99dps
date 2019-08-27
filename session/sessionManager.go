package session

import (
	"99dps/common"
	"fmt"
	"sync"
	"time"
	"sort"
)

const CS_THRESHOLD = 30

type SessionManager struct {
	Sessions      []CombatSession
	activeSession int
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

func (sm *SessionManager) Current(mutex *sync.RWMutex) *CombatSession {
	if len(sm.Sessions) == 0 {
		fmt.Println("No sessions found")
		return nil
	}
	mutex.RLock()
	defer mutex.RUnlock()
	s := sm.Sessions[sm.activeSession]
	return &s
}

func (sm *SessionManager) Clear(mutex *sync.RWMutex) {
	mutex.Lock()
	defer mutex.Unlock()
	sm.Sessions = []CombatSession{}
}

func (sm *SessionManager) All(mutex *sync.RWMutex) []CombatSession {
	if len(sm.Sessions) == 0 {
		return nil
	}

	mutex.RLock()
	defer mutex.RUnlock()
	var cs []CombatSession
	for _, as := range sm.Sessions {
		cs = append(cs, as)
	}

	return cs
}

func (sm *SessionManager) PrintDps(s *CombatSession) string {
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
		summary = fmt.Sprintf("%s\n|%-4d|%-20s|%-5v|%-10v|%-5v|%-5v|", summary, i + 1, k, dps, v.Total, v.High, v.Low)
	}

	return summary
}

func (sm *SessionManager) addSession() {
	cs := CombatSession{}
	sm.Sessions = append(sm.Sessions, cs)
}
