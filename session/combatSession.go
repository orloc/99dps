package session

import (
	"99dps/common"
	"fmt"
	"strings"
	"sync"
	"time"
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

func (cs *CombatSession) GetAggressors(mutex *sync.RWMutex) []common.DamageStat {
	mutex.Lock()
	defer mutex.Unlock()

	var stats []common.DamageStat

	if cs == nil {
		return stats
	}

	for _, v := range cs.aggressors {
		stats = append(stats, v)
	}
	return stats
}

/**
Who ever did the most damage - that wasn't you
lead with the time so its sortable
*/
func (cs *CombatSession) GetSessionIdentifier() string {
	if cs == nil {
		return ""
	}

	var total = 0
	var mname = ""
	for name, combat := range cs.aggressors {
		if combat.Total > total && strings.ToUpper(name) != "YOU" {
			total = combat.Total
			mname = name
		}
	}

	if mname == "" {
		return fmt.Sprintf("%d::Solo", cs.LastTime)
	}

	return fmt.Sprintf("%d::%s", cs.LastTime, mname)
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
