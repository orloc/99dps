package parser

import (
	"fmt"
	"strings"
	"time"
	"sort"
	"sync"
)

/*
A combat session is defined as follows:
	- the pc has not zoned
	- the npc has not died
	- damage is being done to an npc or received by the pc
	- 3 minutes has not passed since last damage to specific npc
 */

type CombatSession struct {
	start      time.Time
	end        time.Time
	targets    []string
	aggressors map[string]DamageStat
}

func (cs *CombatSession) Init() {
	cs.aggressors = make(map[string]DamageStat)
}

func (cs *CombatSession) IsStarted() bool {
	return !cs.start.Equal(time.Time{})
}

func (cs *CombatSession) AdjustDamage(set *DamageSet, mutex *sync.RWMutex) {
	mutex.Lock()
	defer mutex.Unlock()
	indxRef := strings.Replace(set.Dealer, " ", "_", -1)

	fmt.Println(set.ActionTime)
	if val, exists := cs.aggressors[indxRef]; exists {
		val.Total = val.Total + set.Dmg
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

	var collection []*DamageSet
	collection = append(collection, set)
	cs.aggressors[indxRef] = DamageStat{set.Dmg, set.Dmg, set.Dmg, collection}
}

func (cs *CombatSession) Display(mutex *sync.RWMutex) {
	mutex.RLock()
	defer mutex.RUnlock()
	fmt.Println("=== Damage ===\n")

	fmt.Printf("%+v",cs.start, cs.end, cs.targets)
	/*
	for k, v := range cs.aggressors {
		dps := cs.computeDPS(v.CombatRecords, v.Total)
		fmt.Printf("Dealer: %s\n", k)
		fmt.Printf("DPS: %v\n", dps)
		fmt.Printf("Total: %v\n", v.Total)
		fmt.Printf("High: %v\n", v.High)
		fmt.Printf("Low: %v\n", v.Low)

		fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>.")
	}

	if v, ok := cs.aggressors["You"]; ok {
		dps := cs.computeDPS(v.sets, v.total)
		fmt.Printf("DPS: %v\n", dps)
		fmt.Printf("Total: %v\n", v.total)
		fmt.Printf("High: %v\n", v.high)
		fmt.Printf("Low: %v\n", v.low)
	}
	*/

	fmt.Println("=== End ===\n")
}

func (cs *CombatSession) computeDPS(sets []*DamageSet, total int) int {
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

func (cs *CombatSession) startSession(set *DamageSet) {
}
