package session

import (
	"strings"
	"time"
	"sort"
	"sync"
	"99dps/common"
)
type CombatSession struct {
	start      time.Time
	end        time.Time
	LastTime 	int64
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
		Low:set.Dmg,
		High: set.Dmg,
		Total: set.Dmg,
		LastTime: set.ActionTime,
		CombatRecords: collection,
	}
}

func (cs *CombatSession) init(set *common.DamageSet) {
	cs.aggressors = make(map[string]common.DamageStat)
	cs.start = time.Unix(set.ActionTime, 0)
}

func (cs *CombatSession) isStarted() bool {
	return !cs.start.Equal(time.Time{})
}

func (cs *CombatSession) computeDPS(sets []*common.DamageSet, total int) int {
	var times []int64
	for _, set := range sets {
		times = append(times, set.ActionTime)
	}

	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })

	tDiff := times[len(times)-1] - times[0]

	if tDiff == 0 {
		return total
	}

	return total / int(tDiff)
}

