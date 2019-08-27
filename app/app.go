package app

import (
	"github.com/jroimartin/gocui"
	"99dps/common"
	"99dps/session"
	"sync"
	"time"
	"fmt"
)

type App struct {
	gui *gocui.Gui
	manager *session.SessionManager
}

func New(m *session.SessionManager) *App {
	a := new(App)

	a.manager = m

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

func (a *App) SyncSessions(rw *sync.RWMutex) {
	// every 2 seconds update
	for {
		select {
		case <- time.After(2 * time.Second):
			a.updateSessions(rw)
			a.updateDamage(rw)
			a.UpdateGraph(rw)
		}
	}
}

func (a *App) UpdateGraph (rw *sync.RWMutex) {

}

func (a *App) updateDamage(rw *sync.RWMutex) {
	dat := a.manager.Current(rw)
	str := a.manager.PrintDps(dat)

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewDamage, str)
		return nil
	})
}

func (a *App) updateSessions(rw *sync.RWMutex) {
	dat := a.manager.All(rw)
	str := dat[0].GetSessionIdentifier()
	for _, d := range dat[1:] {
		str = fmt.Sprintf("%s\n%s", str, d.GetSessionIdentifier())
	}

	a.gui.Update(func(g *gocui.Gui) error {
		a.writeView(viewSessions, str)
		return nil
	})
}

func (a *App) quit(gui *gocui.Gui, view *gocui.View) error{
	a.gui.Close()
	return gocui.ErrQuit
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

