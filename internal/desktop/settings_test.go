package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neko233-com/DownloadX/internal/engine"
)

func TestSettingsDefaults(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	app := New()
	defer app.Stop()
	if !app.languageAuto || app.languageChoice != "system" {
		t.Fatalf("language auto = %v, choice = %q", app.languageAuto, app.languageChoice)
	}
	if !app.startWithWindows || !app.closeToTray || !app.autoCheckUpdates || !app.monitorClipboard {
		t.Fatalf("background defaults = %+v", app)
	}
}

func TestLegacySettingsKeepNewDefaults(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	path, err := settingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"language":"en","dir":"C:\\Downloads","connections":8}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app := New()
	defer app.Stop()
	if !app.languageAuto || app.languageChoice != "system" {
		t.Fatalf("legacy pinned language was not migrated to system: auto=%v choice=%q", app.languageAuto, app.languageChoice)
	}
	if !app.startWithWindows || !app.closeToTray || !app.autoCheckUpdates || !app.monitorClipboard {
		t.Fatalf("legacy settings turned new features off: %+v", app)
	}
	if int(app.connections) != 8 {
		t.Fatalf("connections = %v", app.connections)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	app := New()
	defer app.Stop()
	app.dir = t.TempDir()
	app.connections = 32
	app.typeValue = "ftp"
	app.startWithWindows = false
	app.closeToTray = false
	app.autoCheckUpdates = false
	app.monitorClipboard = false
	if !app.lang.Set("zh-Hans") {
		t.Fatal("zh-Hans catalog missing")
	}
	app.language = "zh-Hans"
	app.languageAuto = false
	app.languageChoice = "zh-Hans"
	if err := app.SaveSettings(); err != nil {
		t.Fatal(err)
	}

	restored := New()
	defer restored.Stop()
	if restored.dir != app.dir {
		t.Errorf("dir = %q, want %q", restored.dir, app.dir)
	}
	if int(restored.connections) != 32 {
		t.Errorf("connections = %v, want 32", restored.connections)
	}
	if restored.typeValue != string(engine.TypeFTP) {
		t.Errorf("type = %q, want ftp", restored.typeValue)
	}
	if restored.language != "zh-Hans" || restored.languageAuto {
		t.Errorf("language = %q auto=%v, want zh-Hans manual", restored.language, restored.languageAuto)
	}
	if restored.startWithWindows || restored.closeToTray || restored.autoCheckUpdates || restored.monitorClipboard {
		t.Errorf("background settings did not survive: %+v", restored)
	}
}
