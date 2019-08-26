package app

import "github.com/jroimartin/gocui"

type keyConfig struct {
	views *[]string
	key interface{}
	mod gocui.Modifier
	handler func(*gocui.Gui, *gocui.View) error
}

func (a *App) setKeybindings() error {
	qView := []string{""}
	
	var kc = []keyConfig{
		{
			&qView,
			gocui.KeyCtrlC,
			gocui.ModNone,
			a.quit,
		},
	}

	for _, shortcut := range kc {
		for _, view := range *shortcut.views {
			if err := a.gui.SetKeybinding(
				view,
				shortcut.key,
				shortcut.mod,
				shortcut.handler,
			); err != nil {
				return err
			}
		}
	}

	return nil
}