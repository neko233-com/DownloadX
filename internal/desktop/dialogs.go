package desktop

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/neko233-com/DownloadX/internal/engine"
)

func (a *App) addDrawer(c *ui.Context, tk *theme.Tokens) {
	feedback.Drawer(c, &a.addOpen, feedback.DrawerProps{
		Placement:     feedback.Right,
		Size:          440,
		Title:         a.t("add.title"),
		NoCloseButton: true,
		Footer: func() {
			if action.Button(c, a.t("dialog.cancel"), action.ButtonProps{
				Variant: action.Text,
				Size:    theme.Middle,
			}).Clicked() {
				a.addOpen = false
			}
			if action.Button(c, a.t("add.start"), action.ButtonProps{
				Variant: action.Primary,
				Size:    theme.Middle,
			}).Clicked() {
				a.startDownload()
			}
		},
	}, func() {
		ui.Column(c).Gap(16).Children(func() {
			ui.Text(c, a.t("add.subtitle")).FontSize(12).LineHeight(1.6).TextColor(tk.TextSecondary)

			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, a.t("add.url")).FontSize(12).TextColor(tk.TextTertiary)
				row := ui.Row(c).FillWidth().Children(func() {
					f, input := textField(c, tk, &a.url, a.t("add.urlPlaceholder"), lucide.Link2(), 40, 13, true)
					f.Grow(1).Basis(0)
					input.OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyEnter && k.Mods == 0 {
							a.startDownload()
							return true
						}
						return false
					})
				})
				_ = row
			})

			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, a.t("add.saveTo")).FontSize(12).TextColor(tk.TextTertiary)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					f, _ := textField(c, tk, &a.dir, a.t("composer.folderPlaceholder"), lucide.Folder(), 34, 13, false)
					f.Grow(1).Basis(0)
					if action.ActionIcon(c, lucide.FolderOpen(), action.ActionIconProps{
						Variant: action.ActionIconBorderless,
						Size:    theme.Middle,
						Title:   a.t("composer.browse"),
					}).Clicked() {
						a.browse()
					}
				})
			})

			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, a.t("add.filename")).FontSize(12).TextColor(tk.TextTertiary)
				row := ui.Row(c).FillWidth().Children(func() {
					f, _ := textField(c, tk, &a.filename, a.t("add.filenamePlaceholder"), lucide.FileDown(), 34, 13, false)
					f.Grow(1).Basis(0)
				})
				_ = row
			})

			ui.Row(c).Gap(12).Children(func() {
				ui.Column(c).Width(132).Gap(6).Children(func() {
					ui.Text(c, a.t("add.connections")).FontSize(12).TextColor(tk.TextTertiary)
					field.InputNumber(c, &a.connections, field.InputNumberProps{
						Min:       1,
						Max:       float64(engine.MaxConnections),
						Step:      1,
						Precision: 0,
						Size:      theme.Middle,
					})
				})
				ui.Column(c).Grow(1).Basis(0).Gap(6).Children(func() {
					ui.Text(c, a.t("add.protocol")).FontSize(12).TextColor(tk.TextTertiary)
					field.Select(c, &a.typeValue, field.SelectProps{
						Options: a.typeOptions(),
						Size:    theme.Middle,
					})
				})
			})

			ui.Text(c, a.t("add.hint")).FontSize(11).LineHeight(1.6).TextColor(tk.TextQuaternary)
		})
	})
}

func (a *App) startDownload() {
	url := strings.TrimSpace(a.url)
	if url == "" {
		a.showToast(feedback.ToastWarning, a.t("toast.pasteFirst"), "")
		return
	}
	conn := int(a.connections)
	if conn < 1 {
		conn = engine.DefaultConnections
	}
	if conn > engine.MaxConnections {
		conn = engine.MaxConnections
	}
	typ := engine.TypeAuto
	switch a.typeValue {
	case "http":
		typ = engine.TypeHTTP
	case "ftp":
		typ = engine.TypeFTP
	case "hls":
		typ = engine.TypeHLS
	case "dash":
		typ = engine.TypeDASH
	case "bt":
		typ = engine.TypeBT
	}
	dir := strings.TrimSpace(a.dir)
	if dir == "" {
		dir = "."
	}
	job, err := a.manager.Add(Request{URL: url, Dir: dir, Filename: a.filename, Connections: conn, Type: typ})
	if err != nil {
		a.showToast(feedback.ToastError, a.t("toast.addFailed"), err.Error())
		return
	}
	a.selected = job.ID
	a.url = ""
	a.filename = ""
	a.addOpen = false
	_ = a.SaveSettings()
	a.showToast(feedback.ToastSuccess, a.t("toast.added"), "")
}

func (a *App) updateDrawer(c *ui.Context, tk *theme.Tokens) {
	feedback.Drawer(c, &a.updateOpen, feedback.DrawerProps{
		Placement:     feedback.Right,
		Size:          500,
		Title:         a.t("update.title"),
		NoCloseButton: true,
		Footer: func() {
			if action.Button(c, a.t("dialog.later"), action.ButtonProps{
				Variant: action.Text,
				Size:    theme.Middle,
			}).Clicked() {
				a.updateOpen = false
			}
			switch a.updateState {
			case updateAvailable:
				if action.Button(c, a.t("settings.downloadUpdate"), action.ButtonProps{
					Variant: action.Primary,
					Size:    theme.Middle,
				}).Clicked() {
					a.DownloadUpdate()
				}
			case updateReady:
				if action.Button(c, a.t("settings.installUpdate"), action.ButtonProps{
					Variant: action.Primary,
					Size:    theme.Middle,
				}).Clicked() {
					a.InstallUpdate()
				}
			case updateDownloading:
				action.Button(c, a.t("settings.downloading"), action.ButtonProps{
					Variant: action.Default,
					Size:    theme.Middle,
				}).Disabled(true)
			default:
				if action.Button(c, a.t("settings.checkNow"), action.ButtonProps{
					Variant: action.Primary,
					Size:    theme.Middle,
				}).Clicked() {
					a.CheckUpdates(true)
				}
			}
		},
	}, func() {
		ui.Column(c).Gap(14).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.t("settings.currentVersion")+": "+a.versionText()).FontSize(12).TextColor(tk.TextSecondary)
				if a.updateRelease != nil {
					ui.Text(c, "→ v"+a.updateRelease.Version).FontSize(12).FontWeight(600).TextColor(tk.Primary)
				}
			})
			if a.updateState == updateDownloading {
				feedback.Progress(c, float32(a.updateProgress), feedback.ProgressProps{
					Size:     theme.Small,
					Status:   feedback.ProgressActive,
					ShowInfo: true,
				})
			}
			if a.updateErr != "" {
				ui.Text(c, a.updateErr).FontSize(12).TextColor(tk.Error).MaxLines(4)
			}
			if a.updateRelease != nil {
				ui.Text(c, a.updateRelease.Notes).FontSize(12).LineHeight(1.7).TextColor(tk.TextSecondary)
			} else if a.updateState == updateUptodate {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, lucide.CircleCheck()).FontSize(18).TextColor(tk.Success)
					ui.Text(c, a.t("settings.upToDate")).FontSize(13).TextColor(tk.TextSecondary)
				})
			} else if a.updateState == updateError {
				ui.Text(c, a.t("settings.updateFailed")).FontSize(13).TextColor(tk.Error)
			} else {
				ui.Text(c, a.t("update.checkHint")).FontSize(12).LineHeight(1.6).TextColor(tk.TextTertiary)
			}
		})
	})
}

func (a *App) browse() {
	win := a.win
	dir := a.dir
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            win,
			Title:             a.t("composer.browse"),
			DefaultPath:       dir,
			Directory:         true,
			CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 || win == nil {
			return
		}
		win.Update(func() { a.dir = paths[0] })
	}()
}

func searchField(c *ui.Context, value *string, placeholder string) (ui.Element, ui.Element) {
	root := ui.Row(c).Height(32).AlignItems(ui.Center)
	var input ui.Element
	root.Children(func() {
		f, in := textField(c, theme.TokensOf(c), value, placeholder, lucide.Search(), 32, 12, false)
		f.Grow(1).Basis(0)
		input = in
	})
	return root, input
}

// textField builds a Lobe-token text input that can grow inside a row.
func textField(c *ui.Context, tk *theme.Tokens, value *string, placeholder string, icon *ui.SVG, height, fontSize float32, autoFocus bool) (ui.Element, ui.Element) {
	root := ui.Row(c).Gap(8).AlignItems(ui.Center).Height(height).Padding(0, 12).
		Radius(tk.BorderRadius).Border(1, tk.BorderSecondary).Background(tk.BgContainer)
	var input ui.Element
	root.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(fontSize).TextColor(tk.TextTertiary).Shrink(0)
		}
		input = ui.TextInputBase(c, value).Grow(1).FocusRing(false).
			Placeholder(placeholder).PlaceholderColor(tk.TextPlaceholder).FontSize(fontSize).TextColor(tk.Text)
		if autoFocus {
			input.AutoFocus()
		}
	})
	if input.Valid() && input.Focused() {
		root.Border(1, tk.Primary)
	} else if root.Hovered() {
		root.Border(1, tk.Border)
	}
	return root, input
}
