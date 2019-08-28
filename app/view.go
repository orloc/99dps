package app

import (
	"github.com/jroimartin/gocui"
	"fmt"
	"99dps/common"
)


const (
	viewSessions = "sessions"
	viewDamage = "dmg"
	viewGraph = "graph"
	viewShortcuts = "shortcuts"
)

const keyBindingsText = `CTL + C: quit
BACKSPACE: clear all data 

If you change characters - please restart the program`


var vp = map[string]common.ViewProperties{
	viewSessions: {
		Title: "Sessions",
		Text: "",
		X1: 0.0,
		X2: 0.2,
		Y1: 0.0,
		Y2: 0.8,
		Editor: nil,
		Editable: true,
		Autoscroll: true,
		Modal: false,
	},
	viewDamage: {
		Title: "DaMage",
		Text: "",
		X1: 0.2,
		X2: 1,
		Y1: 0.0,
		Y2: 0.4,
		Editor: nil,
		Editable: false,
		Modal: false,
	},
	viewGraph: {
		Title: "Graph",
		Text: "",
		X1: 0.2,
		X2: 1,
		Y1: 0.4,
		Y2: 0.8,
		Editor: nil,
		Editable: false,
		Modal: false,
	},
	viewShortcuts: {
		Title: "Key Bindings",
		Text: keyBindingsText,
		X1: 0.0,
		X2: 1,
		Y1: 0.8,
		Y2: 1,
		Editor: nil,
		Editable: false,
		Modal: false,
	},
}

var views = []string{
	viewSessions,
	viewDamage,
	viewGraph,
	viewShortcuts,
}

func (a *App) Layout(g *gocui.Gui) error {

	for _, v := range views {

		if err := a.initView(v); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) initView(viewName string) error {
	maxX, maxY := a.gui.Size()
	v := vp[viewName]

	x1, y1, x2, y2 := common.GetScreenDims(v, maxX, maxY)

	return a.createView(viewName, x1, x2, y1, y2)

}

func (a *App) createView(name string, x1, x2, y1, y2 int) error {

	if v, err := a.gui.SetView(name, x1, y1, x2, y2); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}

		p := vp[name]
		v.Title = p.Title
		v.Editor = p.Editor
		v.Editable = p.Editable
		v.Autoscroll = p.Autoscroll
		a.writeView(name, p.Text)
	}

	return nil
}

func (a *App) writeView(name, text string) {
	v, _ := a.gui.View(name)
	v.Clear()
	fmt.Fprint(v, text)
	v.SetCursor(len(text), 0)
}
