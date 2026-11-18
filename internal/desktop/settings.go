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
	Language    string `json:"language,omitempty"`
	Dir         string `json:"dir,omitempty"`
	Connections int    `json:"connections,omitempty"`
	Type        string `json:"type,omitempty"`
}

func settingsPath() (string, error) {
	base, err := inbox.BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "settings.json"), nil
}

// loadSettings applies saved preferences. It reports whether a language was
// restored, so New can fall back to the system locale.
func (a *App) loadSettings() bool {
	path, err := settingsPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var s appSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return false
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
	if s.Language != "" && a.lang.Set(s.Language) {
		a.language = a.lang.Code()
		return true
	}
	return false
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
	s := appSettings{
		Language:    a.lang.Code(),
		Dir:         a.dir,
		Connections: int(a.connections),
		Type:        a.typeValue,
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

func (a *App) setLanguage(code string) {
	if a.lang.Set(code) {
		a.language = a.lang.Code()
		_ = a.SaveSettings()
	}
}

func detectLanguage(bundle *i18n.Bundle) string {
	locale := strings.ToLower(mygo.App.Locale())
	if strings.HasPrefix(locale, "zh") {
		if bundle.Set("zh-Hans") {
			return bundle.Code()
		}
	}
	if bundle.Set("en") {
		return "en"
	}
	return bundle.Code()
}

func localesDir() string {
	dir, err := i18n.UserDir()
	if err != nil {
		return filepath.Join("%LOCALAPPDATA%", "DownloadX", "locales")
	}
	return dir
}
