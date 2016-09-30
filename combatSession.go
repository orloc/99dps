package main

import (
	"time"
)

type DamageStat struct {
	high  int
	low   int
	total int
}

type CombatSession struct {
	start      time.Time
	end        time.Time
	targets    []string
	aggressors map[string]DamageStat
}

func (cs *CombatSession) isStarted() bool {
	return !cs.start.Equal(time.Time{})
}

func (cs *CombatSession) startSession(set *DamageSet) {
}
