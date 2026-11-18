package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/neko233-com/DownloadX/internal/engine"
)

func (a *App) jobTitle(j Job) string {
	if j.Path != "" {
		return filepath.Base(j.Path)
	}
	if j.Filename != "" {
		return SafeFilename(j.Filename)
	}
	if name := engine.FilenameFromURL(j.URL); name != "" && name != "download" {
		return name
	}
	if strings.HasPrefix(strings.ToLower(j.URL), "magnet:") {
		return a.t("job.magnetTitle")
	}
	if info, err := os.Stat(j.URL); err == nil && !info.IsDir() {
		return filepath.Base(j.URL)
	}
	return j.URL
}

func (a *App) jobSubtitle(j Job) string {
	if j.Err != "" {
		return j.Err
	}
	if j.Path != "" {
		return j.Path
	}
	return j.URL
}

func (a *App) statusLabel(s Status) string {
	switch s {
	case StatusQueued:
		return a.t("status.queued")
	case StatusRunning:
		return a.t("status.running")
	case StatusPaused:
		return a.t("status.paused")
	case StatusDone:
		return a.t("status.done")
	case StatusError:
		return a.t("status.error")
	case StatusCanceled:
		return a.t("status.canceled")
	default:
		return string(s)
	}
}

func statusTone(tk *theme.Tokens, s Status) (*ui.SVG, ui.Color, display.TagColor) {
	switch s {
	case StatusQueued:
		return lucide.Clock(), tk.TextTertiary, display.TagGray
	case StatusRunning:
		return lucide.LoaderCircle(), tk.Primary, display.TagProcessing
	case StatusPaused:
		return lucide.CirclePause(), tk.Warning, display.TagWarning
	case StatusDone:
		return lucide.CircleCheck(), tk.Success, display.TagSuccess
	case StatusError:
		return lucide.CircleAlert(), tk.Error, display.TagError
	case StatusCanceled:
		return lucide.X(), tk.TextQuaternary, display.TagGray
	default:
		return lucide.Download(), tk.TextTertiary, display.TagGray
	}
}

func categoryIcon(category string) *ui.SVG {
	switch category {
	case "compressed":
		return lucide.FileArchive()
	case "documents":
		return lucide.FileText()
	case "music":
		return lucide.Music()
	case "programs":
		return lucide.AppWindow()
	case "video":
		return lucide.FileVideo()
	default:
		return lucide.FileDown()
	}
}

func (a *App) categoryLabel(category string) string {
	switch category {
	case "compressed":
		return a.t("category.compressed")
	case "documents":
		return a.t("category.documents")
	case "music":
		return a.t("category.music")
	case "programs":
		return a.t("category.programs")
	case "video":
		return a.t("category.video")
	default:
		return a.t("category.other")
	}
}

func humanBytes(n int64) string {
	if n < 0 {
		return "—"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	v := float64(n) / 1024
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func humanSpeed(v float64) string {
	if v <= 0 {
		return "—"
	}
	return humanBytes(int64(v)) + "/s"
}

func humanETA(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < time.Second {
		return "<1s"
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

func dateText(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("01-02 15:04")
}
