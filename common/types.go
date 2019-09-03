package common

import "github.com/jroimartin/gocui"

type DamageStat struct {
	Low           int
	High          int
	Total         int
	LastTime      int64
	CombatRecords []*DamageSet
}

type DamageSet struct {
	ActionTime int64
	Dealer     string
	Dmg        int
	Target     string
}

type ViewProperties struct {
	Title      string
	Text       string
	X1         float64
	Y1         float64
	X2         float64
	Y2         float64
	Editor     gocui.Editor
	Editable   bool
	Autoscroll bool
	Modal      bool
}
