package session

import (
	"99dps/common"
	"strings"
	"sync"
	"time"
	"fmt"
)

type CombatSession struct {
	start      time.Time
	end        time.Time
	LastTime   int64
	aggressors map[string]common.DamageStat
}

func (cs *CombatSession) AdjustDamage(set *common.DamageSet, mutex *sync.RWMutex) {
	mutex.Lock()
	defer mutex.Unlock()

	if !cs.isStarted() {
		cs.init(set)
	}

	indxRef := strings.Replace(set.Dealer, " ", "_", -1)
	cs.LastTime = set.ActionTime

	if val, exists := cs.aggressors[indxRef]; exists {
		val.Total = val.Total + set.Dmg
		val.LastTime = set.ActionTime

		dmg := set.Dmg

		if val.Low > dmg {
			val.Low = dmg
		}

		if val.High < dmg {
			val.High = dmg
		}

		val.CombatRecords = append(val.CombatRecords, set)

		cs.aggressors[indxRef] = val
		return
	}

	var collection []*common.DamageSet
	collection = append(collection, set)
	cs.aggressors[indxRef] = common.DamageStat{
		Low:           set.Dmg,
		High:          set.Dmg,
		Total:         set.Dmg,
		LastTime:      set.ActionTime,
		CombatRecords: collection,
	}
}

func (s *CombatSession) PrintDps() {
	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>")
	fmt.Printf("Started : %s \nEnded: %s\nDuration: %.2fm\n\n", s.start.String(), s.end.String(), s.end.Sub(s.start).Minutes())
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

func (cs *CombatSession) init(set *common.DamageSet) {
	cs.aggressors = make(map[string]common.DamageStat)
	cs.start = time.Unix(set.ActionTime, 0)
}

func (cs *CombatSession) isStarted() bool {
	return !cs.start.Equal(time.Time{})
}

func (cs *CombatSession) computeDPS(sets []*common.DamageSet, total int) int {
	tDiff := cs.LastTime - cs.start.Unix()
	if tDiff == 0 {
		return total
	}
	return total / int(tDiff)
}
