package desktop

import (
	"testing"

	"github.com/neko233-com/DownloadX/internal/engine"
)

func TestSettingsRoundTrip(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	app := New()
	defer app.Stop()
	app.dir = t.TempDir()
	app.connections = 32
	app.typeValue = "ftp"
	if !app.lang.Set("zh-Hans") {
		t.Fatal("zh-Hans catalog missing")
	}
	app.language = "zh-Hans"
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
	if restored.language != "zh-Hans" {
		t.Errorf("language = %q, want zh-Hans", restored.language)
	}
}
