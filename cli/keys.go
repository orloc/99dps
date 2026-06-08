package cli

import "github.com/jroimartin/gocui"

type keyConfig struct {
	views   *[]string
	key     interface{}
	mod     gocui.Modifier
	handler func(*gocui.Gui, *gocui.View) error
}

func (a *App) setKeybindings() error {
	qView := []string{""}
	sessionsView := []string{viewSessions}
	timersView := []string{viewTimers}
	repopsView := []string{viewRepops}
	ccView := []string{viewCC}

	var kc = []keyConfig{
		{
			&qView,
			gocui.KeyCtrlC,
			gocui.ModNone,
			a.quit,
		},
		{
			&qView,
			gocui.KeyBackspace,
			gocui.ModNone,
			a.clear,
		},
		{
			&qView,
			gocui.KeyArrowUp,
			gocui.ModNone,
			a.selectUp,
		},
		{
			&qView,
			gocui.KeyArrowDown,
			gocui.ModNone,
			a.selectDown,
		},
		{
			&qView,
			gocui.KeyEnd,
			gocui.ModNone,
			a.selectLive,
		},
		{
			&qView,
			'a',
			gocui.ModNone,
			a.toggleTTS,
		},
		{
			&sessionsView,
			gocui.MouseLeft,
			gocui.ModNone,
			a.selectClick,
		},
		{
			&sessionsView,
			gocui.MouseWheelUp,
			gocui.ModNone,
			a.wheelUp,
		},
		{
			&sessionsView,
			gocui.MouseWheelDown,
			gocui.ModNone,
			a.wheelDown,
		},
		{
			&timersView,
			gocui.MouseLeft,
			gocui.ModNone,
			a.dismissTimerClick,
		},
		{
			&timersView,
			gocui.MouseWheelUp,
			gocui.ModNone,
			a.timerWheelUp,
		},
		{
			&timersView,
			gocui.MouseWheelDown,
			gocui.ModNone,
			a.timerWheelDown,
		},
		{
			&ccView,
			gocui.MouseLeft,
			gocui.ModNone,
			a.dismissTimerClick,
		},
		{
			&repopsView,
			gocui.MouseLeft,
			gocui.ModNone,
			a.selectRepopClick,
		},
		{
			&repopsView,
			gocui.MouseWheelUp,
			gocui.ModNone,
			a.repopWheelUp,
		},
		{
			&repopsView,
			gocui.MouseWheelDown,
			gocui.ModNone,
			a.repopWheelDown,
		},
		{
			&qView,
			gocui.KeyEnter,
			gocui.ModNone,
			a.editCommit,
		},
		{
			&qView,
			gocui.KeyEsc,
			gocui.ModNone,
			a.editCancel,
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

	// digits and ':' feed the repop-timer editor (no-op when not editing)
	for _, ch := range []byte("0123456789:") {
		if err := a.gui.SetKeybinding("", rune(ch), gocui.ModNone, a.editType(ch)); err != nil {
			return err
		}
	}

	return nil
}
