package common

type DamageSet struct {
	ActionTime int64
	Dealer     string
	Dmg        int
	Target     string
}

type DamageStat struct {
	Low   int
	High int
	Total int
	LastTime int64
	CombatRecords []*DamageSet
}