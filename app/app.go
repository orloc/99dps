package app

import (
	"github.com/jroimartin/gocui"
	"99dps/common"
	"99dps/session"
	"sync"
	"time"
	"fmt"
	"github.com/buger/goterm"
	"sort"
)

type App struct {
	gui *gocui.Gui
	manager *session.SessionManager
	lock *sync.RWMutex
}

func New(m *session.SessionManager, lock *sync.RWMutex) *App {
	a := new(App)

	a.manager = m
	a.lock = lock

	var err error
	a.gui, err = gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		common.CheckErr(err)
	}

	a.initGui()

	return a
}

func (a *App) Loop() {
	if err := a.gui.MainLoop(); err != nil && err != gocui.ErrQuit {
		common.CheckErr(err)
	}
}

func (a *App) Sync() {
	// every seconds update
	for {
		select {
		case <- time.After(1 * time.Second):
			a.updateSessions()
			a.updateDamage()
			a.updateGraph()
		}
	}
}

func (a *App) quit(gui *gocui.Gui, view *gocui.View) error{
	a.gui.Close()
	return gocui.ErrQuit
}

func (a *App) clear(gui *gocui.Gui, view *gocui.View) error {
	a.manager.Clear()
	return nil
}

func (a *App) initGui() {
	// default config
	a.gui.Cursor = true
	a.gui.InputEsc = true
	a.gui.Mouse = true
	a.gui.BgColor = gocui.ColorDefault
	a.gui.FgColor = gocui.ColorDefault

	// set layout
	a.gui.SetManagerFunc(a.Layout)

	// set keybindings
	a.setKeybindings()

}

func (a *App) updateDamage() {
	dat := a.manager.Current()
	str := a.manager.PrintDps(dat)

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewDamage, str)
		return nil
	})
}

func (a *App) updateSessions() {
	dat := a.manager.All()

	if len(dat) == 0 {
		a.writeView(viewSessions, "No Sessions found!\n\nFight something!")
		return
	}

	str := dat[0].GetSessionIdentifier()
	for _, d := range dat[1:] {
		str = fmt.Sprintf("%s\n%s", str, d.GetSessionIdentifier())
	}

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewSessions, str)
		return nil
	})
}

func (a *App) updateGraph () {
	v := vp[viewGraph]
	maxX, maxY := a.gui.Size()

	x1, y1, x2, y2 := common.GetScreenDims(v, maxX, maxY)

	x := x2-x1
	y := y2-y1


	dat := a.manager.Current()
	agg := dat.GetAggressors()

	// filter top 2
	sort.SliceStable(agg, func(i, j int) bool {
		return agg[i].Total > agg[j].Total
	})

	var top2 []common.DamageStat
	if len(agg) < 2 {
		top2 = agg
	} else {
		top2 = agg[:2]
	}

	columns, damageRows, tList := a.preprocess(top2)

	c, d := a.prepareGraph(x, y, columns, damageRows, tList)

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewGraph, c.Draw(d))
		goterm.Flush()
		return nil
	})
}

func (a *App) prepareGraph(x, y int, columns []string, damageRows [][]int, tList [][]int64) (*goterm.LineChart, *goterm.DataTable){
	chart := goterm.NewLineChart(x, y)
	data := new(goterm.DataTable)

	// add all the columns
	for _, c := range columns {
		data.AddColumn(c)
	}

	tHash := make(map[int64]bool)
	for _, times := range tList {
		for _, t := range times {
			tHash[t] = true
		}
	}

	for _, times := range tList {
		for j, t := range times {
			// add this time
			input := []float64{float64(t)}
			// add all the other dmg markers for the rows
			for _, dmgRow := range damageRows {
				if j > len(dmgRow) -1 {
					input = append(input, float64(0))
				} else {
					input = append(input, float64(dmgRow[j]))
				}
			}
			data.AddRow(input...)
		}
	}

	return chart, data

}

func (a *App) preprocess(agg []common.DamageStat) ([]string, [][]int, [][]int64) {
	var (
		damageRows [][]int
		times [][]int64
	)

	columns := []string{"time"}

	for _, d := range agg {
		columns = append(columns, d.CombatRecords[0].Dealer)
		var (
			t []int64
			r []int
		)

		for _, d := range d.CombatRecords {
			t = append(t, d.ActionTime)
			r = append(r, d.Dmg)
		}

		damageRows = append(damageRows, r)
		times = append(times, t)
	}

	return columns, damageRows, times
}


