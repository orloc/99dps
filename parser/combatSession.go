package parser

import (
	"fmt"
	"strings"
	"time"
)

type DamageStat struct {
	low   int
	high  int
	total int
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

		cs.aggressors[indxRef] = val
	} else {
		cs.aggressors[indxRef] = DamageStat{set.dmg, set.dmg, set.dmg}
	}
}

func (cs *CombatSession) Display() {
	fmt.Println("=== Damage ===\n")
	for k, v := range cs.aggressors {
		fmt.Printf("Dealer Name: %v\n\n", k)
		fmt.Printf("Total: %v\n", v.total)
		fmt.Printf("High: %v\n", v.high)
		fmt.Printf("Low: %v\n", v.low)

		fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>")
	}
	fmt.Println("=== End ===\n")
}

func (cs *CombatSession) startSession(set *DamageSet) {
}
