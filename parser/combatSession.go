package parser

import (
	"fmt"
	"strings"
	"time"
	"sort"
)

type DamageStat struct {
	low   int
	high  int
	total int
	sets []*DamageSet
}

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

func (cs *CombatSession) AdjustDamage(set *DamageSet) {
	indxRef := strings.Replace(set.dealer, " ", "_", -1)
	if val, exists := cs.aggressors[indxRef]; exists {
		val.total = val.total + set.dmg
		dmg := set.dmg

		if val.low > dmg {
			val.low = dmg
		}

		if val.high < dmg {
			val.high = dmg
		}

		val.sets = append(val.sets, set)

		cs.aggressors[indxRef] = val
	} else {
		var collection []*DamageSet
		collection = append(collection, set)
		cs.aggressors[indxRef] = DamageStat{set.dmg, set.dmg, set.dmg, collection}
	}
}

func (cs *CombatSession) Display() {

	fmt.Println("=== Damage ===\n")

	if v, ok := cs.aggressors["You"]; ok {
		dps := cs.computeDPS(v.sets, v.total)
		fmt.Printf("DPS: %v\n", dps)
		fmt.Printf("Total: %v\n", v.total)
		fmt.Printf("High: %v\n", v.high)
		fmt.Printf("Low: %v\n", v.low)
	}

	fmt.Println("=== End ===\n")
}

func (cs *CombatSession) computeDPS(sets []*DamageSet, total int) int64 {
	var times []int64
	for _, set := range sets {
		times = append(times, set.actionTime)
	}

	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })

	tDiff := times[len(times)-1] - times[0]

	return int64(total) / tDiff
}

func (cs *CombatSession) startSession(set *DamageSet) {
}
