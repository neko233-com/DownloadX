package desktop

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/egoist/mygo/ui"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	app := New()
	app.lang.Set("en")
	app.language = "en"
	app.languageAuto = false
	app.languageChoice = "en"
	return app
}

func TestViewBuilds(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()

	tester := ui.NewTester(app.View, 1180, 820)
	tester.Frame()

	for _, want := range []string{"DownloadX", "All downloads", "No downloads here", "Add URL", "Settings"} {
		if !tester.HasText(want) {
			t.Fatalf("%q missing", want)
		}
	}

	app.page = pageSettings
	tester.Frame()
	for _, want := range []string{"Save settings", "Browser extension", "Start with Windows", "Check for updates automatically"} {
		if !tester.HasText(want) {
			t.Fatalf("settings %q missing", want)
		}
	}

	app.page = pageAbout
	tester.Frame()
	for _, want := range []string{"About DownloadX", "Protocols", "Native MyGo"} {
		if !tester.HasText(want) {
			t.Fatalf("about %q missing", want)
		}
	}
}

func TestViewBuildsChinese(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()
	if !app.lang.Set("zh-Hans") {
		t.Fatal("zh-Hans catalog missing")
	}
	app.language = "zh-Hans"
	app.languageAuto = false
	app.languageChoice = "zh-Hans"

	tester := ui.NewTester(app.View, 1180, 820)
	tester.Frame()

	for _, want := range []string{"全部下载", "这里还没有下载", "添加链接", "设置"} {
		if !tester.HasText(want) {
			t.Fatalf("Chinese %q missing", want)
		}
	}
}

func TestViewBuildsGermanAndJapanese(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()

	app.lang.Set("de")
	app.language = "de"
	app.languageAuto = false
	app.languageChoice = "de"
	tester := ui.NewTester(app.View, 1180, 820)
	tester.Frame()
	for _, want := range []string{"Alle Downloads", "Einstellungen", "URL hinzufügen"} {
		if !tester.HasText(want) {
			t.Fatalf("German %q missing", want)
		}
	}

	app.lang.Set("ja")
	app.language = "ja"
	app.languageChoice = "ja"
	tester.Frame()
	for _, want := range []string{"すべてのダウンロード", "設定", "URL を追加"} {
		if !tester.HasText(want) {
			t.Fatalf("Japanese %q missing", want)
		}
	}
}

func TestAddDrawerEnterStartsDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	app := newTestApp(t)
	defer app.Stop()
	app.dir = t.TempDir()
	app.addOpen = true
	tester := ui.NewTester(app.View, 1180, 820)
	tester.Frame()
	tester.Type(srv.URL + "/file.bin")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	jobs := app.manager.Snapshot()
	if len(jobs) != 1 {
		t.Fatalf("jobs after Enter = %d, want 1", len(jobs))
	}
	if jobs[0].URL != srv.URL+"/file.bin" {
		t.Fatalf("job URL = %q", jobs[0].URL)
	}
	if app.addOpen {
		t.Fatal("Add drawer stayed open after Enter")
	}
}

func TestTableColumnsAtMinimumWidth(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()
	tester := ui.NewTester(app.View, 900, 620)
	tester.Frame()
	for _, want := range []string{"File", "Size", "Speed", "Status"} {
		if !tester.HasText(want) {
			t.Fatalf("column %q missing at 900px", want)
		}
	}
}
