package app

import (
	"github.com/jroimartin/gocui"
	"fmt"
)


const (
	viewSessions = "sessions"
	viewDamage = "dmg"
	viewGraph = "graph"
	viewShortcuts = "shortcuts"
)

const keyBindingsText = `CTL + C: quit `

type viewProperties struct {
	title    string
	text     string
	x1       float64
	y1       float64
	x2       float64
	y2       float64
	editor   gocui.Editor
	editable bool
	autoscroll bool
	modal    bool
}

var vp = map[string]viewProperties{
	viewSessions: {
		title: "Sessions",
		text: "",
		x1: 0.0,
		x2: 0.2,
		y1: 0.0,
		y2: 0.8,
		editor: nil,
		editable: true,
		autoscroll: true,
		modal: false,
	},
	viewDamage: {
		title: "Damage",
		text: "",
		x1: 0.2,
		x2: 1,
		y1: 0.0,
		y2: 0.4,
		editor: nil,
		editable: false,
		modal: false,
	},
	viewGraph: {
		title: "Graph",
		text: "",
		x1: 0.2,
		x2: 1,
		y1: 0.4,
		y2: 0.8,
		editor: nil,
		editable: false,
		modal: false,
	},
	viewShortcuts: {
		title: "Key Bindings",
		text: keyBindingsText,
		x1: 0.0,
		x2: 1,
		y1: 0.8,
		y2: 1,
		editor: nil,
		editable: false,
		modal: false,
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

	x1 := int(v.x1 * float64(maxX))
	x2 := int(v.x2 * float64(maxX)) - 1
	y1 := int(v.y1 * float64(maxY))
	y2 := int(v.y2 * float64(maxY)) - 1


	return a.createView(viewName, x1, x2, y1, y2)

}

func (a *App) createView(name string, x1, x2, y1, y2 int) error {

	if v, err := a.gui.SetView(name, x1, y1, x2, y2); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}

		p := vp[name]
		v.Title = p.title
		v.Editor = p.editor
		v.Editable = p.editable
		v.Autoscroll = p.autoscroll
		a.writeView(name, p.text)
	}

	return nil
}

func (a *App) writeView(name, text string) {
	v, _ := a.gui.View(name)
	v.Clear()
	fmt.Fprint(v, text)
	v.SetCursor(len(text), 0)
}
