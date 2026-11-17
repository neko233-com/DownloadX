package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/neko233-com/DownloadX/internal/engine"
)

// TestPreviewImage writes a PNG of the window when DOWNLOADX_PREVIEW names a
// path, for reviewing the design without opening a window.
func TestPreviewImage(t *testing.T) {
	out := os.Getenv("DOWNLOADX_PREVIEW")
	if out == "" {
		t.Skip("set DOWNLOADX_PREVIEW=path.png to render the window")
	}
	app := New()
	defer app.Stop()

	now := time.Now()
	app.manager.mu.Lock()
	app.manager.jobs = []*Job{
		{
			ID:          "preview-1",
			URL:         "https://releases.ubuntu.com/24.04/ubuntu-24.04-desktop-amd64.iso",
			Dir:         filepath.Join("Downloads"),
			Connections: 16,
			Type:        engine.TypeAuto,
			Status:      StatusRunning,
			Done:        1240000000,
			Total:       2600000000,
			Speed:       23500000,
			Created:     now,
			Started:     now.Add(-40 * time.Second),
		},
		{
			ID:          "preview-2",
			URL:         "https://example.com/assets/design-system-kit.zip",
			Dir:         filepath.Join("Downloads"),
			Connections: 16,
			Type:        engine.TypeAuto,
			Status:      StatusDone,
			Done:        18400000,
			Total:       18400000,
			Bytes:       18400000,
			Path:        filepath.Join("Downloads", "design-system-kit.zip"),
			Created:     now.Add(-2 * time.Minute),
			Started:     now.Add(-110 * time.Second),
			Finished:    now.Add(-95 * time.Second),
		},
		{
			ID:          "preview-3",
			URL:         "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
			Dir:         filepath.Join("Downloads"),
			Connections: 16,
			Type:        engine.TypeBT,
			Status:      StatusError,
			Err:         "bittorrent: timed out waiting for metadata",
			Created:     now.Add(-3 * time.Minute),
			Started:     now.Add(-3 * time.Minute),
			Finished:    now.Add(-time.Minute),
		},
	}
	app.manager.mu.Unlock()

	for _, spec := range []struct {
		name string
		dark bool
	}{
		{"dark", true},
		{"light", false},
	} {
		tester := ui.NewTester(app.View, 1180, 820)
		tester.SetDark(spec.dark)
		tester.Frame()
		tester.Advance(400 * time.Millisecond)
		tester.Frame()
		img := tester.Image()
		if img == nil {
			t.Fatal("no image")
		}
		path := out
		if len(spec.name) > 0 {
			ext := filepath.Ext(out)
			path = out[:len(out)-len(ext)] + "-" + spec.name + ext
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		fmt.Println("wrote", path)
	}
}
