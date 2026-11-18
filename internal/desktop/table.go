package desktop

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/layout"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/overlay"
	"github.com/lobehub/lobe-mygo/pkg/theme"
)

const (
	colSize     = float32(82)
	colProgress = float32(126)
	colSpeed    = float32(88)
	colETA      = float32(72)
	colStatus   = float32(92)
	colAdded    = float32(92)
)

// visibleJobs filters and sorts the queue for the table.
func (a *App) visibleJobs() []Job {
	jobs := a.manager.Snapshot()
	search := strings.ToLower(strings.TrimSpace(a.search))
	out := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		if !matchesFilter(j, a.filter) {
			continue
		}
		if search != "" {
			hay := strings.ToLower(j.URL + " " + j.Path + " " + a.jobTitle(j))
			if !strings.Contains(hay, search) {
				continue
			}
		}
		out = append(out, j)
	}
	sort.SliceStable(out, func(i, k int) bool {
		x, y := out[i], out[k]
		cmp := 0
		switch a.sortBy {
		case "name":
			cmp = strings.Compare(strings.ToLower(a.jobTitle(x)), strings.ToLower(a.jobTitle(y)))
		case "size":
			cmp = compareInt64(jobSize(x), jobSize(y))
		case "progress":
			cmp = compareFloat(x.Fraction(), y.Fraction())
		case "speed":
			cmp = compareFloat(x.Speed, y.Speed)
		case "status":
			cmp = strings.Compare(string(x.Status), string(y.Status))
		default:
			cmp = x.Created.Compare(y.Created)
		}
		if a.sortAsc {
			return cmp < 0
		}
		return cmp > 0
	})
	return out
}

func matchesFilter(j Job, filter string) bool {
	switch filter {
	case "", "all":
		return true
	case "downloading":
		return j.Status == StatusRunning
	case "queued":
		return j.Status == StatusQueued
	case "paused":
		return j.Status == StatusPaused
	case "finished":
		return j.Status == StatusDone
	case "failed":
		return j.Status == StatusError
	default:
		return j.Category() == filter
	}
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func jobSize(j Job) int64 {
	if j.Total > 0 {
		return j.Total
	}
	if j.Bytes > 0 {
		return j.Bytes
	}
	return j.Done
}

func showETA(width float32) bool      { return width >= 760 }
func showAdded(width float32) bool    { return width >= 900 }
func showProgress(width float32) bool { return width >= 560 }

// workbench builds the toolbar, table and selected-job strip.
func (a *App) workbench(c *ui.Context, tk *theme.Tokens, width float32) {
	ui.Column(c).Fill().Children(func() {
		a.toolbar(c, tk)
		a.table(c, tk, width)
		a.detailsStrip(c, tk)
	})
}

func (a *App) toolbar(c *ui.Context, tk *theme.Tokens) {
	ui.Row(c).Height(52).Shrink(0).Gap(8).AlignItems(ui.Center).PaddingX(14).
		Background(tk.BgLayout).BorderWidth(0, 0, 1, 0).BorderColor(tk.BorderSecondary).Children(func() {
		a.toolbarButton(c, tk, lucide.Plus(), a.t("toolbar.add"), action.Primary, true, func() {
			a.addOpen = true
		})
		ui.Box(c).Width(1).Height(20).Background(tk.BorderSecondary).Margin(2, 0)
		a.iconButton(c, lucide.Pause(), a.t("toolbar.pause"), func() {
			if n := a.manager.PauseAll(); n == 0 {
				a.showToast(feedback.ToastInfo, a.t("toast.noSelection"), "")
			}
		})
		a.iconButton(c, lucide.Play(), a.t("toolbar.resume"), func() {
			if n := a.manager.ResumeAll(); n == 0 {
				a.showToast(feedback.ToastInfo, a.t("toast.noSelection"), "")
			}
		})
		a.iconButton(c, lucide.FolderOpen(), a.t("toolbar.openFolder"), func() {
			reveal(a.dir)
		})
		a.iconButton(c, lucide.Trash2(), a.t("toolbar.clear"), func() {
			a.manager.ClearFinished()
		})
		ui.Spacer(c)
		field, _ := searchField(c, &a.search, a.t("toolbar.search"))
		field.Width(240)
		a.updateIndicator(c, tk)
		if action.ActionIcon(c, lucide.Settings2(), action.ActionIconProps{
			Size:  theme.Middle,
			Title: a.t("nav.settings"),
		}).Clicked() {
			a.page = pageSettings
		}
	})
}

func (a *App) updateIndicator(c *ui.Context, tk *theme.Tokens) {
	if a.updateState == updateAvailable || a.updateState == updateReady || a.updateState == updateDownloading {
		label := a.t("toolbar.update")
		if a.updateRelease != nil {
			label = "v" + a.updateRelease.Version
		}
		if action.Button(c, label, action.ButtonProps{
			Variant: action.Filled,
			Size:    theme.Small,
		}).Clicked() {
			a.updateOpen = true
		}
		return
	}
	if a.updateState == updateChecking {
		ui.Icon(c, lucide.LoaderCircle()).FontSize(14).TextColor(tk.TextTertiary)
	}
}

func (a *App) toolbarButton(c *ui.Context, tk *theme.Tokens, icon *ui.SVG, label string, variant action.ButtonVariant, primary bool, click func()) {
	b := ui.ButtonBase(c).Row().Gap(6)
	action.StyleButton(c, b, "", action.ButtonProps{Variant: variant, Size: theme.Middle})
	b.Children(func() {
		ui.Icon(c, icon).FontSize(14)
		ui.Text(c, label).FontSize(12).FontWeight(500)
	})
	if b.Clicked() {
		click()
	}
}

func (a *App) iconButton(c *ui.Context, icon *ui.SVG, title string, click func()) {
	if action.ActionIcon(c, icon, action.ActionIconProps{
		Size:  theme.Middle,
		Title: title,
	}).Clicked() {
		click()
	}
}

func (a *App) table(c *ui.Context, tk *theme.Tokens, width float32) {
	jobs := a.visibleJobs()
	ui.Column(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
		a.tableHeader(c, tk, width)
		if len(jobs) == 0 {
			ui.Box(c).Grow(1).Basis(0).Center().Children(func() {
				a.emptyState(c, tk)
			})
			return
		}
		layout.ScrollArea(c, layout.ScrollAreaProps{Fade: true}, func() {
			ui.Column(c).Children(func() {
				for i := range jobs {
					a.jobRow(c, tk, width, jobs[i], i)
				}
			})
		}).Grow(1).Basis(0).MinHeight(0)
	})
}

func (a *App) tableHeader(c *ui.Context, tk *theme.Tokens, width float32) {
	ui.Row(c).Height(32).Shrink(0).Gap(8).AlignItems(ui.Center).PaddingX(12).
		Background(tk.FillQuaternary).BorderWidth(0, 0, 1, 0).BorderColor(tk.BorderSecondary).Children(func() {
		ui.Box(c).Width(28).Shrink(0)
		a.sortHeader(c, tk, "name", a.t("table.file"), true)
		a.fixedHeader(c, tk, a.t("table.size"), colSize)
		if showProgress(width) {
			a.fixedHeader(c, tk, a.t("table.progress"), colProgress)
		}
		a.fixedHeader(c, tk, a.t("table.speed"), colSpeed)
		if showETA(width) {
			a.fixedHeader(c, tk, a.t("table.eta"), colETA)
		}
		a.fixedHeader(c, tk, a.t("table.status"), colStatus)
		if showAdded(width) {
			a.fixedHeader(c, tk, a.t("table.added"), colAdded)
		}
	})
}

func (a *App) sortHeader(c *ui.Context, tk *theme.Tokens, key, label string, grow bool) {
	b := ui.ButtonBase(c).Label(label).Row().Gap(4).AlignItems(ui.Center).Justify(ui.Start).Height(24).PaddingX(4)
	if grow {
		b.Grow(1).Basis(0).MinWidth(0)
	}
	color := tk.TextTertiary
	if a.sortBy == key {
		color = tk.Text
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(color).SingleLine()
		if a.sortBy == key {
			if a.sortAsc {
				ui.Icon(c, lucide.ChevronUp()).FontSize(11).TextColor(tk.TextTertiary)
			} else {
				ui.Icon(c, lucide.ChevronDown()).FontSize(11).TextColor(tk.TextTertiary)
			}
		}
	})
	if b.Clicked() {
		if a.sortBy == key {
			a.sortAsc = !a.sortAsc
		} else {
			a.sortBy = key
			a.sortAsc = key == "name"
		}
	}
}

func (a *App) fixedHeader(c *ui.Context, tk *theme.Tokens, label string, width float32) {
	ui.Row(c).Width(width).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(tk.TextTertiary).SingleLine()
	})
}

func (a *App) jobRow(c *ui.Context, tk *theme.Tokens, width float32, j Job, index int) {
	selected := a.selected == j.ID
	row := ui.ButtonBase(c).Label(a.jobTitle(j)).Row().Gap(8).AlignItems(ui.Center).
		Height(50).PaddingX(12).Shrink(0).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.BorderSecondary).Cursor(ui.CursorPointer)
	switch {
	case selected:
		row.Background(tk.FillTertiary)
	case row.Hovered():
		row.Background(tk.FillQuaternary)
	}
	_, tone, tagColor := statusTone(tk, j.Status)
	row.Children(func() {
		ui.Box(c).Size(28, 28).Radius(8).Background(tk.FillTertiary).Center().Shrink(0).Children(func() {
			ui.Icon(c, categoryIcon(j.Category())).FontSize(14).TextColor(tone)
		})
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, a.jobTitle(j)).FontSize(13).FontWeight(500).TextColor(tk.Text).SingleLine()
			ui.Text(c, a.jobSubtitle(j)).FontSize(11).TextColor(tk.TextTertiary).SingleLine()
		})
		ui.Text(c, humanBytes(jobSize(j))).Width(colSize).Shrink(0).FontSize(12).TextColor(tk.TextSecondary).FontFeatures("tnum")
		if showProgress(width) {
			progressCell(c, tk, j)
		}
		ui.Text(c, humanSpeed(j.Speed)).Width(colSpeed).Shrink(0).FontSize(12).TextColor(tk.TextSecondary).FontFeatures("tnum")
		if showETA(width) {
			ui.Text(c, humanETA(j.ETA())).Width(colETA).Shrink(0).FontSize(12).TextColor(tk.TextTertiary).FontFeatures("tnum")
		}
		ui.Row(c).Width(colStatus).Shrink(0).Children(func() {
			display.Tag(c, a.statusLabel(j.Status), display.TagProps{
				Variant: display.TagFilled,
				Color:   tagColor,
				Size:    theme.Small,
				Round:   true,
			})
		})
		if showAdded(width) {
			ui.Text(c, dateText(j.Created)).Width(colAdded).Shrink(0).FontSize(11).TextColor(tk.TextQuaternary).FontFeatures("tnum")
		}
	})

	if row.Clicked() {
		a.selected = j.ID
	}
	if row.DoubleClicked() {
		a.openJob(j)
	}
	chosen := overlay.ContextMenu(c, row, overlay.DropdownMenuProps{Items: a.jobMenuItems(j)})
	if chosen != "" {
		a.runJobAction(j, chosen)
	}
}

func progressCell(c *ui.Context, tk *theme.Tokens, j Job) {
	pct := float32(j.Fraction() * 100)
	color := tk.Primary
	switch j.Status {
	case StatusPaused:
		color = tk.Warning
	case StatusError, StatusCanceled:
		color = tk.Error
	case StatusDone:
		color = tk.Success
	}
	ui.Column(c).Width(colProgress).Shrink(0).Gap(3).Children(func() {
		ui.Text(c, fmt.Sprintf("%.0f%%", pct)).FontSize(10).TextColor(tk.TextTertiary).FontFeatures("tnum")
		track := ui.Box(c).Width(colProgress).Height(4).Radius(2).Background(tk.FillTertiary)
		track.Children(func() {
			if pct > 0 {
				ui.Box(c).WidthPercent(pct).Height(4).Radius(2).Background(color)
			}
		})
	})
}

func (a *App) jobMenuItems(j Job) []overlay.MenuItem {
	items := []overlay.MenuItem{}
	switch j.Status {
	case StatusRunning, StatusQueued:
		items = append(items, overlay.MenuItem{Key: "pause", Label: a.t("action.pause"), Icon: lucide.Pause()})
	case StatusPaused:
		items = append(items, overlay.MenuItem{Key: "resume", Label: a.t("action.resume"), Icon: lucide.Play()})
	case StatusError, StatusCanceled:
		items = append(items, overlay.MenuItem{Key: "retry", Label: a.t("action.retry"), Icon: lucide.RotateCw()})
	}
	if j.Path != "" {
		items = append(items, overlay.MenuItem{Key: "open", Label: a.t("action.open"), Icon: lucide.FolderOpen()})
	}
	items = append(items, overlay.MenuItem{Key: "copyUrl", Label: a.t("action.copyURL"), Icon: lucide.Copy()})
	items = append(items, overlay.MenuItem{Divider: true})
	if j.Status != StatusRunning {
		items = append(items, overlay.MenuItem{Key: "remove", Label: a.t("action.remove"), Icon: lucide.Trash2(), Danger: true})
	}
	return items
}

func (a *App) runJobAction(j Job, actionKey string) {
	switch actionKey {
	case "pause":
		a.manager.Pause(j.ID)
	case "resume":
		_ = a.manager.Resume(j.ID)
	case "retry":
		_ = a.manager.Retry(j.ID)
	case "open":
		a.openJob(j)
	case "copyUrl":
		a.lastClipboard = j.URL
		mygo.Clipboard.WriteText(j.URL)
		a.showToast(feedback.ToastSuccess, a.t("toast.pathCopied"), "")
	case "remove":
		a.manager.Remove(j.ID)
		if a.selected == j.ID {
			a.selected = ""
		}
	}
}

func (a *App) openJob(j Job) {
	switch {
	case j.Path != "":
		reveal(j.Path)
	case strings.HasPrefix(j.URL, "http://"), strings.HasPrefix(j.URL, "https://"):
		_ = mygo.Shell.OpenExternal(j.URL)
	}
}

func (a *App) emptyState(c *ui.Context, tk *theme.Tokens) {
	display.Empty(c, display.EmptyProps{
		Icon:        lucide.Inbox(),
		Title:       a.t("empty.title"),
		Description: a.t("empty.description"),
		Action: func() {
			if action.Button(c, a.t("toolbar.add"), action.ButtonProps{
				Variant: action.Primary,
				Size:    theme.Middle,
			}).Clicked() {
				a.addOpen = true
			}
		},
	})
}

func (a *App) detailsStrip(c *ui.Context, tk *theme.Tokens) {
	if a.selected == "" {
		return
	}
	var job Job
	found := false
	for _, j := range a.manager.Snapshot() {
		if j.ID == a.selected {
			job = j
			found = true
			break
		}
	}
	if !found {
		a.selected = ""
		return
	}
	ui.Row(c).Height(72).Shrink(0).Gap(12).AlignItems(ui.Center).PaddingX(16).
		Background(tk.BgContainer).BorderWidth(1, 0, 0, 0).BorderColor(tk.BorderSecondary).Children(func() {
		ui.Box(c).Size(32, 32).Radius(8).Background(tk.FillTertiary).Center().Children(func() {
			ui.Icon(c, categoryIcon(job.Category())).FontSize(15).TextColor(tk.TextSecondary)
		})
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, a.jobTitle(job)).FontSize(13).FontWeight(500).TextColor(tk.Text).SingleLine()
			meta := fmt.Sprintf("%s · %s · %s", a.statusLabel(job.Status), humanBytes(jobSize(job)), a.categoryLabel(job.Category()))
			if job.Speed > 0 {
				meta += " · " + humanSpeed(job.Speed)
			}
			ui.Text(c, meta).FontSize(11).TextColor(tk.TextTertiary).SingleLine()
		})
		switch job.Status {
		case StatusRunning, StatusQueued:
			if action.Button(c, a.t("action.pause"), action.ButtonProps{Variant: action.Default, Size: theme.Small}).Clicked() {
				a.manager.Pause(job.ID)
			}
		case StatusPaused:
			if action.Button(c, a.t("action.resume"), action.ButtonProps{Variant: action.Primary, Size: theme.Small}).Clicked() {
				_ = a.manager.Resume(job.ID)
			}
		case StatusError, StatusCanceled:
			if action.Button(c, a.t("action.retry"), action.ButtonProps{Variant: action.Default, Size: theme.Small}).Clicked() {
				_ = a.manager.Retry(job.ID)
			}
		}
		if job.Path != "" {
			if action.Button(c, a.t("action.open"), action.ButtonProps{Variant: action.Default, Size: theme.Small}).Clicked() {
				reveal(job.Path)
			}
		}
		if action.ActionIcon(c, lucide.X(), action.ActionIconProps{Size: theme.Small, Title: a.t("dialog.close")}).Clicked() {
			a.selected = ""
		}
	})
}
