package desktop

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
)

func (a *App) startTray() {
	menu := a.buildTrayMenu()
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "downloadx",
		Icon:    trayPNG,
		ToolTip: a.t("tray.tooltip", "DownloadX"),
		Menu:    menu,
	})
	if err != nil {
		// Without a tray icon, hiding the window would strand the app in the
		// background. Fall back to a normal close.
		a.closeToTray = false
		return
	}
	a.tray = tray
	a.trayStop = make(chan struct{})
	go a.trayStatusLoop(a.trayStop)
}

func (a *App) buildTrayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: a.t("tray.show"), Click: func(*mygo.MenuItem, *mygo.Window) {
			if a.win != nil {
				a.win.Show()
				a.win.Focus()
			}
		}},
		mygo.Separator(),
		{Label: a.t("tray.pauseAll"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.manager.PauseAll()
			a.invalidate()
		}},
		{Label: a.t("tray.resumeAll"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.manager.ResumeAll()
			a.invalidate()
		}},
		mygo.Separator(),
		{Label: a.t("tray.startWithWindows"), Type: mygo.MenuItemCheckbox, Checked: a.startWithWindows, Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.startWithWindows = item.Checked
			_ = mygo.App.SetOpenAtLogin(a.startWithWindows)
			_ = a.SaveSettings()
			a.invalidate()
		}},
		{Label: a.t("tray.checkUpdates"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.CheckUpdates(true)
		}},
		mygo.Separator(),
		{Label: a.t("tray.exit"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.Quit()
		}},
	})
}

func (a *App) rebuildTrayMenu() {
	if a.tray == nil {
		return
	}
	a.tray.SetMenu(a.buildTrayMenu())
	a.tray.SetToolTip(a.trayTooltip())
}

func (a *App) trayStatusLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if a.quitting || a.tray == nil {
				return
			}
			a.tray.SetToolTip(a.trayTooltip())
		}
	}
}

func (a *App) trayTooltip() string {
	c := a.manager.Counts()
	switch {
	case c.Active > 0:
		return fmt.Sprintf("DownloadX · %d · %s/s", c.Active, humanBytes(int64(c.Speed)))
	case c.Paused > 0:
		return fmt.Sprintf("DownloadX · %d %s", c.Paused, a.t("status.paused"))
	case c.Queued > 0:
		return fmt.Sprintf("DownloadX · %d %s", c.Queued, a.t("status.queued"))
	default:
		return "DownloadX · " + a.t("tray.idle")
	}
}

func (a *App) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}
