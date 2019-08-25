package session

import (
	"sync"
	"99dps/common"
	"time"
	"fmt"
)

const CS_THRESHOLD = 30

type SessionManager struct {
	Sessions []CombatSession
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

	s :=  &sm.Sessions[sm.activeSession]

	// combat has ended - make a new session
	if set.ActionTime - s.LastTime >= CS_THRESHOLD {
		s.end = time.Unix(s.LastTime, 0)
		sm.addSession()
		sm.activeSession = len(sm.Sessions) - 1
		s =  &sm.Sessions[sm.activeSession]
	}

	return s
}

func (sm *SessionManager) Display(mutex *sync.RWMutex){
	mutex.RLock()
	defer mutex.RUnlock()
	s := sm.Sessions[sm.activeSession]
	sm.printDps(&s)
}

func (sm *SessionManager) Clear(mutex *sync.RWMutex) {
	mutex.Lock()
	defer mutex.Unlock()
	sm.Sessions = []CombatSession{}
}

func (sm *SessionManager) All(mutex *sync.RWMutex) {
	mutex.RLock()
	defer mutex.RUnlock()
	for _, as := range sm.Sessions {
		sm.printDps(&as)
		fmt.Println("\r")
	}
}

func (sm *SessionManager) printDps(s *CombatSession) {
	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>")
	fmt.Printf("Started : %s \nEnded: %s\n\n", s.start.String(), s.end.String())
	for k, v := range s.aggressors {
		dps := s.computeDPS(v.CombatRecords, v.Total)
		fmt.Printf("Dealer: %s\n", k)
		fmt.Printf("DPS: %v\n", dps)
		fmt.Printf("Total: %v\n", v.Total)
		fmt.Printf("High: %v\n", v.High)
		fmt.Printf("Low: %v\n", v.Low)
		fmt.Println("")
	}
	fmt.Println("<<<<<<<<<<<<<<<<<<<<<<<<")
}


func (sm *SessionManager) addSession() {
	cs := CombatSession{}
	sm.Sessions = append(sm.Sessions, cs)
}