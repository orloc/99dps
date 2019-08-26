package app

import (
	"github.com/jroimartin/gocui"
	"99dps/common"
)

type App struct {
	gui *gocui.Gui
}


func New() *App {
	a := new(App)

	var err error
	a.gui, err = gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		common.CheckErr(err)
	}

	// initialize view state?

	a.initGui()

	return a
}

func (a *App) Loop() {
	if err := a.gui.MainLoop(); err != nil && err != gocui.ErrQuit {
		common.CheckErr(err)
	}
}

func (a *App) Close() {
	a.gui.Close()
}

func (a *App) quit(gui *gocui.Gui, view *gocui.View) error{
	return gocui.ErrQuit
}

func (a *App) initGui() {
	// default config
	a.gui.Cursor = true
	a.gui.InputEsc = true
	a.gui.BgColor = gocui.ColorDefault
	a.gui.FgColor = gocui.ColorDefault

	// set layout
	a.gui.SetManagerFunc(a.Layout)

	// set keybindings
	a.setKeybindings()

}

