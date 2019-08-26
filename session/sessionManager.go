package session

import (
	"99dps/common"
	"fmt"
	"sync"
	"time"
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

func (sm *SessionManager) Display(mutex *sync.RWMutex) {
	if len(sm.Sessions) == 0 {
		fmt.Println("No sessions found")
		return
	}
	mutex.RLock()
	defer mutex.RUnlock()
	s := sm.Sessions[sm.activeSession]
	s.PrintDps()
}

func (sm *SessionManager) Clear(mutex *sync.RWMutex) {
	mutex.Lock()
	defer mutex.Unlock()
	sm.Sessions = []CombatSession{}
}

func (sm *SessionManager) All(mutex *sync.RWMutex) {
	if len(sm.Sessions) == 0 {
		fmt.Println("No sessions found")
		return
	}

	mutex.RLock()
	defer mutex.RUnlock()
	for _, cs := range sm.Sessions {
		cs.PrintDps()
		fmt.Println("\r")
	}
}


func (sm *SessionManager) addSession() {
	cs := CombatSession{}
	sm.Sessions = append(sm.Sessions, cs)
}
