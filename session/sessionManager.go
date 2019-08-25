package session

import "sync"

type SessionManager struct {
	Sessions [10]CombatSession
	ActiveSession int
}

func (sm *SessionManager) Display(mutex *sync.RWMutex){

	// find the active session
	// print it
}