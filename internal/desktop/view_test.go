package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestViewBuilds(t *testing.T) {
	app := New()
	defer app.Stop()

	tester := ui.NewTester(app.View, 1100, 800)
	tester.Frame()

	if !tester.HasText("Download anything, fast.") {
		t.Fatal("hero title missing")
	}
	if !tester.HasText("No downloads yet") {
		t.Fatal("empty state missing")
	}
	if !tester.HasText("16 CONNECTIONS") {
		t.Fatal("feature tag missing")
	}
}
