package desktop

import (
	"fmt"
	"os"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/layout"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"
)

const sidebarWidth = float32(212)

// View builds the whole window.
func (a *App) View(c *ui.Context) {
	theme.Apply(c)
	tk := theme.TokensOf(c)

	if a.dir == "" {
		if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil && dir != "" {
			a.dir = dir
		} else {
			a.dir = "."
		}
	}
	if a.connections <= 0 {
		a.connections = 16
	}
	if a.dir == "" {
		a.dir = "."
	}

	if a.toastSeq != a.toastShown {
		a.toastShown = a.toastSeq
		feedback.ShowToast(c, feedback.ToastProps{
			Type:        a.toastType,
			Title:       a.toastTitle,
			Description: a.toastDesc,
		})
	}

	width, _ := c.Size()
	root := ui.Column(c).Fill()
	if !c.Vibrancy() {
		root.Background(tk.BgLayout)
	}
	root.Children(func() {
		a.titleBar(c, tk)
		ui.Row(c).Grow(1).Basis(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c, tk)
			switch a.page {
			case pageSettings:
				layout.ScrollArea(c, layout.ScrollAreaProps{Fade: true}, func() {
					a.settingsPage(c, tk)
				}).Grow(1).Basis(0).MinHeight(0)
			case pageAbout:
				layout.ScrollArea(c, layout.ScrollAreaProps{Fade: true}, func() {
					a.aboutPage(c, tk)
				}).Grow(1).Basis(0).MinHeight(0)
			default:
				ui.Column(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
					a.workbench(c, tk, width-sidebarWidth)
				})
			}
			a.addDrawer(c, tk)
			a.updateDrawer(c, tk)
			feedback.ToastHost(c, feedback.ToastHostProps{Placement: feedback.ToastBottomLeft})
		})
		if !a.addOpen && !a.updateOpen {
			a.statusBar(c, tk)
		}
	})
}

func (a *App) titleBar(c *ui.Context, tk *theme.Tokens) {
	bar := c.TitleBar()
	height := float32(40)
	if bar.Height > height {
		height = bar.Height
	}
	ui.Row(c).Height(height).Shrink(0).Gap(8).AlignItems(ui.Center).
		Padding(0, bar.Right+16, 0, bar.Left+16).
		Background(tk.BgLayout).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.BorderSecondary).
		DragWindow().Children(func() {
		if a.logo != nil {
			ui.Image(c, a.logo).Size(22, 22)
		} else {
			ui.Box(c).Size(22, 22).Radius(6).Background(tk.Primary).Center().Children(func() {
				ui.Icon(c, lucide.ArrowDownToLine()).FontSize(14).TextColor(tk.PrimaryForeground)
			})
		}
		ui.Text(c, "DownloadX").FontSize(13).FontWeight(600).TextColor(tk.Text)
		ui.Spacer(c)
		if !a.addOpen && !a.updateOpen {
			ui.Text(c, a.versionText()).FontSize(11).TextColor(tk.TextQuaternary)
		}
	})
}

func (a *App) sidebar(c *ui.Context, tk *theme.Tokens) {
	counts := a.manager.Counts()
	jobs := a.manager.Snapshot()
	active := ""
	if a.page == pageWorkbench {
		active = a.filter
	}
	ui.Column(c).Width(sidebarWidth).Shrink(0).Padding(10, 8).Gap(4).
		Background(tk.BgContainer).
		BorderWidth(0, 1, 0, 0).BorderColor(tk.BorderSecondary).Children(func() {
		ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				a.sidebarLabel(c, tk, a.t("nav.downloads"))
				clicked := display.List(c, display.ListProps{
					Items: []display.ListItem{
						{Key: "all", Label: a.t("category.all"), Icon: lucide.Inbox(), Extra: countText(len(jobs))},
						{Key: "downloading", Label: a.t("category.downloading"), Icon: lucide.LoaderCircle(), Extra: countText(counts.Active)},
						{Key: "queued", Label: a.t("category.queued"), Icon: lucide.Clock(), Extra: countText(counts.Queued)},
						{Key: "paused", Label: a.t("category.paused"), Icon: lucide.CirclePause(), Extra: countText(counts.Paused)},
						{Key: "finished", Label: a.t("category.finished"), Icon: lucide.CircleCheck(), Extra: countText(counts.Done)},
						{Key: "failed", Label: a.t("category.failed"), Icon: lucide.CircleAlert(), Extra: countText(counts.Failed)},
					},
					Active:  active,
					Compact: true,
				})
				if clicked != "" {
					a.filter = clicked
					a.page = pageWorkbench
				}

				a.sidebarLabel(c, tk, a.t("nav.categories"))
				clicked = display.List(c, display.ListProps{
					Items: []display.ListItem{
						{Key: "compressed", Label: a.t("category.compressed"), Icon: lucide.FileArchive(), Extra: countText(categoryCount(jobs, "compressed"))},
						{Key: "documents", Label: a.t("category.documents"), Icon: lucide.FileText(), Extra: countText(categoryCount(jobs, "documents"))},
						{Key: "music", Label: a.t("category.music"), Icon: lucide.Music(), Extra: countText(categoryCount(jobs, "music"))},
						{Key: "programs", Label: a.t("category.programs"), Icon: lucide.AppWindow(), Extra: countText(categoryCount(jobs, "programs"))},
						{Key: "video", Label: a.t("category.video"), Icon: lucide.FileVideo(), Extra: countText(categoryCount(jobs, "video"))},
						{Key: "other", Label: a.t("category.other"), Icon: lucide.FileDown(), Extra: countText(categoryCount(jobs, "other"))},
					},
					Active:  active,
					Compact: true,
				})
				if clicked != "" {
					a.filter = clicked
					a.page = pageWorkbench
				}
			})
		})

		ui.Box(c).Height(1).Background(tk.BorderSecondary).Margin(4, 0)
		clicked := display.List(c, display.ListProps{
			Items: []display.ListItem{
				{Key: "settings", Label: a.t("nav.settings"), Icon: lucide.Settings2()},
				{Key: "about", Label: a.t("nav.about"), Icon: lucide.Info()},
			},
			Active:  pageKey(a.page),
			Compact: true,
		})
		switch clicked {
		case "settings":
			a.page = pageSettings
		case "about":
			a.page = pageAbout
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 6).Children(func() {
			ui.Icon(c, lucide.Globe()).FontSize(11).TextColor(tk.TextQuaternary)
			ui.Text(c, a.t("sidebar.extension")).FontSize(11).TextColor(tk.TextQuaternary).SingleLine()
		})
	})
}

func (a *App) sidebarLabel(c *ui.Context, tk *theme.Tokens, text string) {
	ui.Text(c, strings.ToUpper(text)).FontSize(10).FontWeight(600).LetterSpacing(0.4).
		TextColor(tk.TextQuaternary).Padding(6, 10, 2, 10)
}

func pageKey(p page) string {
	switch p {
	case pageSettings:
		return "settings"
	case pageAbout:
		return "about"
	}
	return ""
}

func countText(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprint(n)
}

func categoryCount(jobs []Job, category string) int {
	n := 0
	for _, j := range jobs {
		if j.Category() == category {
			n++
		}
	}
	return n
}

func (a *App) statusBar(c *ui.Context, tk *theme.Tokens) {
	counts := a.manager.Counts()
	ui.Row(c).Height(32).Shrink(0).Gap(12).AlignItems(ui.Center).Padding(0, 12, 6, 12).
		Background(tk.BgContainer).BorderWidth(1, 0, 0, 0).BorderColor(tk.BorderSecondary).Children(func() {
		a.statusItem(c, tk, a.t("status.total"), fmt.Sprint(counts.Total))
		a.statusItem(c, tk, a.t("status.active"), fmt.Sprint(counts.Active))
		a.statusItem(c, tk, a.t("status.queued"), fmt.Sprint(counts.Queued))
		if counts.Paused > 0 {
			a.statusItem(c, tk, a.t("status.paused"), fmt.Sprint(counts.Paused))
		}
		if counts.Failed > 0 {
			a.statusItem(c, tk, a.t("status.failed"), fmt.Sprint(counts.Failed))
		}
		ui.Spacer(c)
		if counts.Speed > 0 {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, lucide.Activity()).FontSize(11).TextColor(tk.Success)
				ui.Text(c, humanSpeed(counts.Speed)).FontSize(11).TextColor(tk.TextSecondary).FontFeatures("tnum")
			})
		}
		ui.Text(c, a.t("status.connections", int(a.connections))).FontSize(11).TextColor(tk.TextQuaternary)
		if text := a.statusUpdateText(); text != "" {
			b := ui.ButtonBase(c).Label(text).Row().Gap(4).Height(22).PaddingX(6).Radius(4)
			color := tk.TextTertiary
			if a.updateState == updateAvailable || a.updateState == updateReady {
				color = tk.Primary
			} else if a.updateState == updateError {
				color = tk.Error
			} else if a.updateState == updateUptodate {
				color = tk.Success
			}
			b.Children(func() {
				if a.updateState == updateDownloading || a.updateState == updateChecking {
					ui.Icon(c, lucide.LoaderCircle()).FontSize(11).TextColor(color)
				}
				ui.Text(c, text).FontSize(11).LineHeight(1).TextColor(color).SingleLine().MaxWidth(150)
			})
			if b.Clicked() {
				a.updateOpen = true
			}
		}
	})
}

func (a *App) statusItem(c *ui.Context, tk *theme.Tokens, label, value string) {
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).LineHeight(1).TextColor(tk.TextQuaternary)
		ui.Text(c, value).FontSize(11).LineHeight(1).FontWeight(500).TextColor(tk.TextSecondary).FontFeatures("tnum")
	})
}

func reveal(path string) {
	if path == "" {
		return
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		_ = mygo.Shell.OpenPath(path)
		return
	}
	mygo.Shell.ShowItemInFolder(path)
}
