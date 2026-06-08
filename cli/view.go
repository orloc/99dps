package cli

import (
	"99dps/common"
	"fmt"
	"github.com/jroimartin/gocui"
)

const (
	viewSessions  = "sessions"
	viewDamage    = "dmg"
	viewGraph     = "graph"
	viewTimers    = "timers"
	viewShortcuts = "shortcuts"
)

// ViewProperties is a panel's static layout config: fractional screen bounds
// (0..1, translated to cells by GetScreenDims) plus gocui display flags.
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
}

// GetScreenDims translates a view's fractional bounds into absolute cell
// coordinates (x1,y1,x2,y2) for a maxX×maxY terminal.
func GetScreenDims(v ViewProperties, maxX, maxY int) (int, int, int, int) {
	x1 := int(v.X1 * float64(maxX))
	x2 := int(v.X2*float64(maxX)) - 1
	y1 := int(v.Y1 * float64(maxY))
	y2 := int(v.Y2*float64(maxY)) - 1
	return x1, y1, x2, y2
}

const keyBindingsText = `CTL + C: quit    BACKSPACE: clear    A: audio cues    (char switches auto-detected)
↑/↓: select session    CLICK: select    WHEEL: scroll    END: jump to live`

var vp = map[string]ViewProperties{
	viewSessions: {
		Title:      "Sessions",
		Text:       "",
		X1:         0.0,
		X2:         0.2,
		Y1:         0.0,
		Y2:         0.8,
		Editor:     nil,
		Editable:   false,
		Autoscroll: false, // scroll is driven manually (selection + mouse wheel)
	},
	viewDamage: {
		Title:    "Damage",
		Text:     "",
		X1:       0.2,
		X2:       1,
		Y1:       0.0,
		Y2:       0.4,
		Editor:   nil,
		Editable: false,
	},
	viewGraph: {
		Title:    "Graph",
		Text:     "",
		X1:       0.2,
		X2:       0.6,
		Y1:       0.4,
		Y2:       0.8,
		Editor:   nil,
		Editable: false,
	},
	viewTimers: {
		Title:    "Spell Timers",
		Text:     "",
		X1:       0.6,
		X2:       1,
		Y1:       0.4,
		Y2:       0.8,
		Editor:   nil,
		Editable: false,
	},
	viewShortcuts: {
		Title:    "Key Bindings",
		Text:     keyBindingsText,
		X1:       0.0,
		X2:       1,
		Y1:       0.8,
		Y2:       1,
		Editor:   nil,
		Editable: false,
	},
}

var views = []string{
	viewSessions,
	viewDamage,
	viewGraph,
	viewTimers,
	viewShortcuts,
}

func (a *App) Layout(g *gocui.Gui) error {

	for _, v := range views {

		if err := a.initView(v); err != nil {
			return err
		}
	}

	// keep keyboard focus on the session list so ↑/↓ navigate it
	if g.CurrentView() == nil {
		if _, err := g.SetCurrentView(viewSessions); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) initView(viewName string) error {
	maxX, maxY := a.gui.Size()
	v := vp[viewName]

	x1, y1, x2, y2 := GetScreenDims(v, maxX, maxY)

	return a.createView(viewName, x1, x2, y1, y2)

}

func (a *App) createView(name string, x1, x2, y1, y2 int) error {

	v, err := a.gui.SetView(name, x1, y1, x2, y2)
	if err != nil && err != gocui.ErrUnknownView {
		return err
	}
	if err == gocui.ErrUnknownView {
		// first creation
		p := vp[name]
		v.Editor = p.Editor
		v.Editable = p.Editable
		v.Autoscroll = p.Autoscroll
		a.writeView(name, p.Text)
	}

	// refresh the title every Layout so the sessions panel reflects the current
	// character even after an auto-detected switch.
	v.Title = a.viewTitle(name, vp[name].Title)
	return nil
}

func (a *App) viewTitle(name, base string) string {
	switch name {
	case viewSessions:
		if ch := a.characterLabel(); ch != "" {
			return base + " — " + ch
		}
	case viewTimers:
		return a.panelTitle()
	}
	return base
}

// panelTitle labels the bottom-right panel by the player's class category.
func (a *App) panelTitle() string {
	if a.tracker == nil {
		return "Spell Timers"
	}
	switch a.tracker.Category() {
	case common.CatMelee:
		return "Skills"
	case common.CatHybrid:
		return "Spells + Skills"
	default:
		return "Spell Timers"
	}
}

func (a *App) writeView(name, text string) {
	v, _ := a.gui.View(name)
	v.Clear()
	fmt.Fprint(v, text)
	v.SetCursor(len(text), 0)
}
