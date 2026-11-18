package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"github.com/neko233-com/DownloadX/internal/desktop/i18n"
	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/inbox"
)

type appSettings struct {
	Version     int    `json:"version,omitempty"`
	Language    string `json:"language,omitempty"` // "" or "system" follows Windows
	Dir         string `json:"dir,omitempty"`
	Connections int    `json:"connections,omitempty"`
	Type        string `json:"type,omitempty"`

	StartWithWindows bool  `json:"startWithWindows"`
	CloseToTray      bool  `json:"closeToTray"`
	AutoCheckUpdates bool  `json:"autoCheckUpdates"`
	MonitorClipboard bool  `json:"monitorClipboard"`
	LastUpdateCheck  int64 `json:"lastUpdateCheck,omitempty"`
}

func defaultSettings() appSettings {
	return appSettings{
		Language:         "system",
		Connections:      engine.DefaultConnections,
		Type:             "auto",
		StartWithWindows: true,
		CloseToTray:      true,
		AutoCheckUpdates: true,
		MonitorClipboard: true,
		LastUpdateCheck:  0,
	}
}

func settingsPath() (string, error) {
	base, err := inbox.BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "settings.json"), nil
}

// loadSettings applies saved preferences. Missing booleans keep their
// defaults, so a settings file written by an older release turns the new
// background and update options on rather than off.
func (a *App) loadSettings() {
	path, err := settingsPath()
	if err != nil {
		a.languageAuto = true
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.languageAuto = true
		return
	}
	s := defaultSettings()
	s.Version = 0
	if err := json.Unmarshal(data, &s); err != nil {
		a.languageAuto = true
		return
	}
	if s.Dir != "" {
		a.dir = s.Dir
	}
	if s.Connections > 0 {
		a.connections = float64(s.Connections)
		if a.connections > float64(engine.MaxConnections) {
			a.connections = float64(engine.MaxConnections)
		}
	}
	if t, err := engine.ParseType(s.Type); err == nil && s.Type != "" {
		a.typeValue = string(t)
	}
	a.startWithWindows = s.StartWithWindows
	a.closeToTray = s.CloseToTray
	a.autoCheckUpdates = s.AutoCheckUpdates
	a.monitorClipboard = s.MonitorClipboard
	a.lastUpdateCheck = s.LastUpdateCheck

	// Version 1 always saved the detected language, which pinned English (or
	// whichever language was detected) forever. Follow Windows again after
	// upgrading; the user can pick a fixed language in Settings.
	if s.Version < 2 {
		a.languageAuto = true
		return
	}
	if s.Language == "" || strings.EqualFold(s.Language, "system") {
		a.languageAuto = true
		return
	}
	if a.lang.Set(s.Language) {
		a.language = a.lang.Code()
		a.languageAuto = false
		return
	}
	// The selected catalog disappeared: follow Windows again instead of
	// freezing the missing language.
	a.languageAuto = true
}

// SaveSettings writes the current preferences.
func (a *App) SaveSettings() error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	language := "system"
	if !a.languageAuto {
		language = a.lang.Code()
	}
	s := appSettings{
		Version:          2,
		Language:         language,
		Dir:              a.dir,
		Connections:      int(a.connections),
		Type:             a.typeValue,
		StartWithWindows: a.startWithWindows,
		CloseToTray:      a.closeToTray,
		AutoCheckUpdates: a.autoCheckUpdates,
		MonitorClipboard: a.monitorClipboard,
		LastUpdateCheck:  a.lastUpdateCheck,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// setLanguage switches the UI language. "system" follows Windows.
func (a *App) setLanguage(code string) {
	if code == "" || strings.EqualFold(code, "system") {
		a.languageAuto = true
		a.languageChoice = "system"
		a.language = detectLanguage(a.lang)
		_ = a.SaveSettings()
		return
	}
	if a.lang.Set(code) {
		a.languageAuto = false
		a.language = a.lang.Code()
		a.languageChoice = a.lang.Code()
		_ = a.SaveSettings()
	}
}

// RefreshLanguage re-resolves the system language when the user follows the
// system. It is called when Windows reports a display-language change.
func (a *App) RefreshLanguage() bool {
	if !a.languageAuto {
		return false
	}
	next := detectLanguage(a.lang)
	if next == a.language {
		return false
	}
	a.language = next
	return true
}

// detectLanguage maps Windows' preferred languages to an installed catalog.
func detectLanguage(bundle *i18n.Bundle) string {
	if bundle == nil {
		return "en"
	}
	preferred := mygo.App.PreferredLanguages()
	if len(preferred) == 0 {
		return bundle.Match(nil)
	}
	return bundle.Match(preferred)
}

func localesDir() string {
	dir, err := i18n.UserDir()
	if err != nil {
		return filepath.Join("%LOCALAPPDATA%", "DownloadX", "locales")
	}
	return dir
}
