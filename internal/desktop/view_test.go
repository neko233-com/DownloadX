package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	app := New()
	app.lang.Set("en")
	app.language = "en"
	return app
}

func TestViewBuilds(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()

	tester := ui.NewTester(app.View, 1180, 820)
	tester.Frame()

	if !tester.HasText("Download anything, fast.") {
		t.Fatal("hero title missing")
	}
	if !tester.HasText("No downloads yet") {
		t.Fatal("empty state missing")
	}
	if !tester.HasText("Settings") {
		t.Fatal("sidebar missing")
	}
	if !tester.HasText("16 CONNECTIONS") {
		t.Fatal("feature tag missing")
	}

	app.page = pageSettings
	tester.Frame()
	if !tester.HasText("Save settings") || !tester.HasText("Browser extension") {
		t.Fatal("settings page missing")
	}

	app.page = pageAbout
	tester.Frame()
	if !tester.HasText("About DownloadX") || !tester.HasText("Protocols") {
		t.Fatal("about page missing")
	}
}

func TestViewBuildsChinese(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()
	if !app.lang.Set("zh-Hans") {
		t.Fatal("zh-Hans catalog missing")
	}
	app.language = "zh-Hans"

	tester := ui.NewTester(app.View, 1180, 820)
	tester.Frame()

	if !tester.HasText("极速下载，随心所欲。") {
		t.Fatal("Chinese hero title missing")
	}
	if !tester.HasText("下载任务") {
		t.Fatal("Chinese queue title missing")
	}
	if !tester.HasText("设置") {
		t.Fatal("Chinese settings nav missing")
	}
}
