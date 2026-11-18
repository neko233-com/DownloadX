// Command downloadx-gui is the native DownloadX desktop app, drawn by MyGo
// with the Lobe UI design language.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/neko233-com/DownloadX/internal/desktop"
	"github.com/neko233-com/DownloadX/internal/version"
)

func main() {
	mygo.App.SetName("DownloadX")
	mygo.App.SetVersion(version.Version)

	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	app := desktop.New()
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		if win := app.Window(); win != nil {
			win.Show()
			win.Focus()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		_ = app.SaveSettings()
		app.Stop()
	})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "DownloadX",
			Width:           1180,
			Height:          820,
			MinWidth:        900,
			MinHeight:       620,
			TitleBarStyle:   mygo.TitleBarHidden,
			BackgroundColor: "light-dark(#ffffff, #000000)",
			Content:         ui.View(app.View),
		})
		app.Attach(win)
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
