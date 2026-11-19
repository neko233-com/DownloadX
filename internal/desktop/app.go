package desktop

import (
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/neko233-com/DownloadX/internal/desktop/i18n"
	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/update"
	"github.com/neko233-com/DownloadX/internal/version"
)

//go:embed assets/logo.png
var logoPNG []byte

//go:embed assets/tray.png
var trayPNG []byte

const extensionID = "pdlpjmbjgipkbjekncinhchgbahpgbng"

type page int

const (
	pageWorkbench page = iota
	pageSettings
	pageAbout
)

type updateState string

const (
	updateIdle        updateState = ""
	updateChecking    updateState = "checking"
	updateAvailable   updateState = "available"
	updateDownloading updateState = "downloading"
	updateReady       updateState = "ready"
	updateUptodate    updateState = "uptodate"
	updateError       updateState = "error"
)

// App holds the window, the queue and the background features.
type App struct {
	manager *Manager
	win     *mygo.Window
	lang    *i18n.Bundle

	logo     *ui.Bitmap
	tray     *mygo.Tray
	trayStop chan struct{}

	language       string
	languageAuto   bool
	languageChoice string

	page     page
	filter   string
	search   string
	sortBy   string
	sortAsc  bool
	selected string

	// Add download drawer.
	addOpen     bool
	url         string
	dir         string
	filename    string
	connections float64
	typeValue   string

	// Settings.
	startWithWindows bool
	closeToTray      bool
	autoCheckUpdates bool
	monitorClipboard bool
	lastUpdateCheck  int64

	// Updates.
	updateOpen     bool
	updateChecking bool
	updateRelease  *update.Release
	updateState    updateState
	updateProgress float64
	updateErr      string
	updatePath     string
	notifiedUpdate string

	quitting      bool
	clipboardOff  func()
	lastClipboard string

	toastSeq   int
	toastShown int
	toastType  feedback.ToastType
	toastTitle string
	toastDesc  string
}

// New creates the app and its download manager.
func New() *App {
	bundle := i18n.Load()
	_ = bundle.Set("en")
	app := &App{
		manager:          NewManager(2),
		lang:             bundle,
		language:         bundle.Code(),
		page:             pageWorkbench,
		filter:           "all",
		sortBy:           "added",
		sortAsc:          false,
		connections:      float64(engine.DefaultConnections),
		typeValue:        "auto",
		startWithWindows: true,
		closeToTray:      true,
		autoCheckUpdates: true,
		monitorClipboard: true,
	}
	if bmp, err := ui.DecodeBitmap(logoPNG); err == nil {
		app.logo = bmp
	}
	app.loadSettings()
	if app.language == "" {
		app.language = app.lang.Code()
	}
	if app.languageAuto {
		app.languageChoice = "system"
	} else {
		app.languageChoice = app.language
	}
	return app
}

// Attach binds the window, language, tray and background services. hidden
// reports that the window was created for a login launch.
func (a *App) Attach(win *mygo.Window, hidden bool) {
	a.win = win
	a.manager.SetOnUpdate(func() { win.Invalidate() })
	if a.dir == "" {
		if downloads, err := mygo.App.Path(mygo.PathDownloads); err == nil && downloads != "" {
			a.dir = downloads
		} else {
			a.dir = "."
		}
	}
	a.manager.StartInbox(a.dir)
	if a.languageAuto {
		a.language = detectLanguage(a.lang)
	}

	a.applyStartupSetting()
	a.startTray()

	win.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting || !a.closeToTray {
			return
		}
		e.PreventDefault()
		win.Hide()
	})
	if hidden && a.tray == nil {
		// A login launch with no tray icon has no way back to the window.
		win.Show()
	}

	a.startClipboard()
	a.startUpdateSchedule()
	mygo.App.OnLocaleChanged(func(string) {
		win.Update(func() {
			if a.RefreshLanguage() {
				a.rebuildTrayMenu()
			}
		})
	})
	win.Invalidate()
}

// Window returns the app window, if it exists.
func (a *App) Window() *mygo.Window { return a.win }

// Stop cancels running downloads and stops the workers.
func (a *App) Stop() { a.manager.Stop() }

// StartHidden reports whether this launch should begin in the tray.
func (a *App) StartHidden() bool {
	return a.closeToTray && mygo.App.WasOpenedAtLogin()
}

// Quit exits through MyGo so OnBeforeQuit can persist settings.
func (a *App) Quit() {
	a.quitting = true
	mygo.App.Quit()
}

// BeforeQuit persists state and stops background work.
func (a *App) BeforeQuit() {
	a.quitting = true
	_ = a.SaveSettings()
	a.manager.Stop()
	if a.clipboardOff != nil {
		a.clipboardOff()
		a.clipboardOff = nil
	}
	if a.trayStop != nil {
		close(a.trayStop)
		a.trayStop = nil
	}
	if a.tray != nil {
		a.tray.Destroy()
		a.tray = nil
	}
}

func (a *App) t(key string, args ...any) string {
	return a.lang.Tf(key, args...)
}

func (a *App) applyStartupSetting() {
	// Development builds must never register their throwaway path as a
	// Windows startup item.
	if strings.Contains(version.Version, "dev") {
		return
	}
	want := a.startWithWindows
	if mygo.App.OpenAtLogin() != want {
		_ = mygo.App.SetOpenAtLogin(want)
	}
}

func (a *App) startClipboard() {
	if !a.monitorClipboard {
		return
	}
	a.clipboardOff = mygo.Clipboard.OnChanged(func() {
		data, err := mygo.Clipboard.Read()
		if err != nil {
			return
		}
		raw, err := data.Read("text/plain")
		if err != nil {
			return
		}
		text := strings.TrimSpace(string(raw))
		if text == "" || text == a.lastClipboard {
			return
		}
		a.lastClipboard = text
		if !looksDownloadable(text) {
			return
		}
		a.win.Update(func() {
			a.url = text
			a.addOpen = true
			a.win.Show()
			a.win.Focus()
		})
	})
}

func looksDownloadable(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 8 || strings.ContainsAny(s, "\r\n\t ") {
		return false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "magnet:") {
		return true
	}
	if !strings.HasPrefix(lower, "http://") &&
		!strings.HasPrefix(lower, "https://") &&
		!strings.HasPrefix(lower, "ftp://") &&
		!strings.HasPrefix(lower, "ftps://") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if ext == "" {
		return false
	}
	_, ok := downloadExtensions[ext]
	return ok
}

// downloadExtensions are the suffixes that are safe to offer automatically
// when a link is copied. A normal page URL has no suffix and stays silent;
// the user can still paste it into Add Download explicitly.
var downloadExtensions = map[string]struct{}{
	".7z": {}, ".apk": {}, ".appx": {}, ".avi": {}, ".bin": {},
	".bz2": {}, ".cab": {}, ".csv": {}, ".deb": {}, ".dmg": {},
	".doc": {}, ".docx": {}, ".epub": {}, ".exe": {}, ".flac": {},
	".flv": {}, ".gz": {}, ".img": {}, ".iso": {}, ".jar": {},
	".m3u8": {}, ".m4a": {}, ".m4v": {}, ".mkv": {}, ".mov": {},
	".mp3": {}, ".mp4": {}, ".mpd": {}, ".mpeg": {}, ".mpg": {},
	".msi": {}, ".msix": {}, ".ogg": {}, ".opus": {}, ".pdf": {},
	".pkg": {}, ".ppt": {}, ".pptx": {}, ".rar": {}, ".rpm": {},
	".rtf": {}, ".tar": {}, ".tgz": {}, ".torrent": {}, ".ts": {},
	".txt": {}, ".vhd": {}, ".vhdx": {}, ".wav": {}, ".webm": {},
	".webp": {}, ".wim": {}, ".wma": {}, ".wmv": {}, ".xz": {},
	".zip": {}, ".zst": {},
}

func (a *App) startUpdateSchedule() {
	if !a.autoCheckUpdates {
		return
	}
	due := a.lastUpdateCheck == 0 || time.Since(time.Unix(a.lastUpdateCheck, 0)) > 24*time.Hour
	if !due {
		return
	}
	time.AfterFunc(3*time.Second, func() { a.CheckUpdates(false) })
}

// CheckUpdates contacts GitHub Releases. Results are published on the UI
// thread through win.Update.
func (a *App) CheckUpdates(manual bool) {
	if a.win == nil {
		return
	}
	start := false
	mygo.RunOnMain(func() {
		if a.updateChecking {
			return
		}
		a.updateChecking = true
		a.updateState = updateChecking
		a.updateErr = ""
		start = true
	})
	if !start {
		return
	}
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		rel, err := update.Check(ctx, version.Version)
		mygo.RunOnMain(func() {
			a.updateChecking = false
			switch {
			case err != nil:
				a.updateState = updateError
				a.updateErr = err.Error()
				if manual {
					a.showToast(feedback.ToastError, a.t("toast.checkFailed"), err.Error())
				}
			case rel == nil:
				a.lastUpdateCheck = time.Now().Unix()
				_ = a.SaveSettings()
				a.updateState = updateUptodate
				a.updateRelease = nil
				if manual {
					a.showToast(feedback.ToastSuccess, a.t("toast.upToDate"), "")
				}
			default:
				a.lastUpdateCheck = time.Now().Unix()
				_ = a.SaveSettings()
				a.updateState = updateAvailable
				a.updateRelease = rel
				a.updateErr = ""
				if a.notifiedUpdate != rel.Version {
					a.notifiedUpdate = rel.Version
					a.notifyUpdate(rel)
				}
			}
			a.invalidate()
		})
	}()
}

// DownloadUpdate fetches and verifies the MSI for the pending release.
func (a *App) DownloadUpdate() {
	var rel *update.Release
	busy := false
	mygo.RunOnMain(func() {
		if a.updateRelease == nil {
			return
		}
		if a.updateState == updateDownloading || a.updateState == updateReady {
			busy = true
			return
		}
		rel = a.updateRelease
		a.updateState = updateDownloading
		a.updateProgress = 0
		a.updateErr = ""
	})
	if rel == nil {
		if !busy {
			a.CheckUpdates(true)
		}
		return
	}
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var last time.Time
		path, err := update.DownloadMSI(ctx, rel, updatesDir(), func(done, total int64) {
			if total <= 0 {
				return
			}
			now := time.Now()
			if done < total && now.Sub(last) < 120*time.Millisecond {
				return
			}
			last = now
			pct := float64(done) * 100 / float64(total)
			mygo.RunOnMain(func() {
				a.updateProgress = pct
				a.invalidate()
			})
		})
		mygo.RunOnMain(func() {
			if err != nil {
				a.updateState = updateError
				a.updateErr = err.Error()
				a.showToast(feedback.ToastError, a.t("toast.updateFailed"), err.Error())
				a.invalidate()
				return
			}
			a.updatePath = path
			a.updateState = updateReady
			a.updateProgress = 100
			a.showToast(feedback.ToastSuccess, a.t("toast.updateReady"), "")
			a.invalidate()
		})
	}()
}

// InstallUpdate starts Windows Installer and quits so the MSI can replace
// the running executable.
func (a *App) InstallUpdate() {
	var path string
	mygo.RunOnMain(func() {
		path = a.updatePath
		if path == "" && a.updateRelease != nil {
			a.updateState = updateAvailable
		}
	})
	if path == "" {
		a.DownloadUpdate()
		return
	}
	if err := update.Apply(path); err != nil {
		mygo.RunOnMain(func() {
			a.updateState = updateError
			a.updateErr = err.Error()
			a.invalidate()
		})
		return
	}
	a.Quit()
}

func (a *App) notifyUpdate(rel *update.Release) {
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:       "downloadx-update",
		Title:    a.t("update.availableTitle"),
		Body:     a.t("update.availableBody", rel.Version),
		Subtitle: "DownloadX",
	})
	n.OnClick(func() {
		if a.win != nil {
			a.win.Show()
			a.win.Focus()
			mygo.RunOnMain(func() {
				a.updateOpen = true
				a.invalidate()
			})
		}
	})
	go func() { _ = n.Show() }()
}

func updatesDir() string {
	base, err := mygo.App.Path(mygo.PathCache)
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "updates")
}

func (a *App) showToast(kind feedback.ToastType, title, description string) {
	a.toastType = kind
	a.toastTitle = title
	a.toastDesc = description
	a.toastSeq++
	a.invalidate()
}

func (a *App) statusUpdateText() string {
	switch a.updateState {
	case updateAvailable:
		return a.t("status.updateAvailable")
	case updateReady:
		return a.t("status.updateReady")
	case updateDownloading:
		return fmt.Sprintf("%s %d%%", a.t("toolbar.update"), int(a.updateProgress))
	case updateError:
		return a.t("status.updateFailed")
	case updateUptodate:
		return a.t("status.upToDate")
	}
	return ""
}

func (a *App) statusText() string {
	switch a.updateState {
	case updateChecking:
		return a.t("settings.checking")
	case updateAvailable:
		if a.updateRelease != nil {
			return a.t("settings.updateAvailable", a.updateRelease.Version)
		}
	case updateDownloading:
		return a.t("settings.downloadingUpdate", int(a.updateProgress))
	case updateReady:
		return a.t("settings.updateReady")
	case updateUptodate:
		return a.t("settings.upToDate")
	case updateError:
		return a.t("settings.updateFailed")
	}
	return ""
}

func (a *App) versionText() string { return version.String() }
