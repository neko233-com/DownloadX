// Package desktop is the native MyGo + Lobe UI front end of DownloadX.
package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/layout"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/neko233-com/DownloadX/internal/desktop/i18n"
	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/version"
)

type page int

const (
	pageDownloads page = iota
	pageSettings
	pageAbout
)

const extensionID = "pdlpjmbjgipkbjekncinhchgbahpgbng"

// App holds the window state.
type App struct {
	manager      *Manager
	win          *mygo.Window
	lang         *i18n.Bundle
	language     string
	detectLocale bool
	page         page
	url          string
	dir          string
	connections  float64
	typeValue    string
}

// New creates the app and its download manager.
func New() *App {
	bundle := i18n.Load()
	_ = bundle.Set("en")
	app := &App{
		manager:     NewManager(2),
		lang:        bundle,
		language:    bundle.Code(),
		page:        pageDownloads,
		connections: float64(engine.DefaultConnections),
		typeValue:   "auto",
	}
	if !app.loadSettings() {
		app.detectLocale = true
	}
	if app.language == "" {
		app.language = app.lang.Code()
	}
	return app
}

// Attach binds the created window so downloads can repaint it.
func (a *App) Attach(win *mygo.Window) {
	a.win = win
	a.manager.SetOnUpdate(func() { win.Invalidate() })
	dir := a.dir
	if dir == "" {
		if downloads, err := mygo.App.Path(mygo.PathDownloads); err == nil {
			dir = downloads
		}
	}
	a.manager.StartInbox(dir)
	if a.detectLocale {
		a.detectLocale = false
		a.language = detectLanguage(a.lang)
		win.Invalidate()
	}
}

// Window returns the app window, if it is created.
func (a *App) Window() *mygo.Window { return a.win }

// Stop cancels running downloads.
func (a *App) Stop() { a.manager.Stop() }

func (a *App) t(key string, args ...any) string {
	return a.lang.Tf(key, args...)
}

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
		a.connections = float64(engine.DefaultConnections)
	}

	root := ui.Column(c).Fill()
	if !c.Vibrancy() {
		root.Background(tk.BgLayout)
	}
	root.Children(func() {
		a.titleBar(c, tk)
		ui.Row(c).Grow(1).Basis(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c, tk)
			a.pageContent(c, tk)
		})
		feedback.ToastHost(c, feedback.ToastHostProps{})
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
		ui.Box(c).Size(22, 22).Radius(6).Background(tk.Primary).Center().Children(func() {
			ui.Icon(c, lucide.ArrowDownToLine()).FontSize(14).TextColor(tk.PrimaryForeground)
		})
		ui.Text(c, a.t("app.name")).FontSize(13).FontWeight(600).TextColor(tk.Text)
		display.Tag(c, a.t("app.badge"), display.TagProps{
			Variant: display.TagBorderless,
			Color:   display.TagGray,
			Size:    theme.Small,
		})
		ui.Spacer(c)
		ui.Text(c, a.t("app.version", version.Version)).FontSize(11).TextColor(tk.TextQuaternary)
	})
}

func (a *App) sidebar(c *ui.Context, tk *theme.Tokens) {
	ui.Column(c).Width(220).Shrink(0).Padding(12).Gap(4).
		Background(tk.BgContainer).
		BorderWidth(0, 1, 0, 0).BorderColor(tk.BorderSecondary).Children(func() {
		a.navItem(c, tk, pageDownloads, lucide.Download(), a.t("nav.downloads"))
		a.navItem(c, tk, pageSettings, lucide.Settings2(), a.t("nav.settings"))
		a.navItem(c, tk, pageAbout, lucide.Info(), a.t("nav.about"))
		ui.Spacer(c)
		display.Tag(c, a.t("sidebar.extension"), display.TagProps{
			Variant: display.TagFilled,
			Color:   display.TagGray,
			Size:    theme.Small,
			Round:   true,
		})
		ui.Text(c, a.t("app.version", version.Version)).FontSize(11).TextColor(tk.TextQuaternary).PaddingX(2)
	})
}

func (a *App) navItem(c *ui.Context, tk *theme.Tokens, target page, icon *ui.SVG, label string) {
	active := a.page == target
	b := ui.ButtonBase(c).Label(label).Row().Gap(10).AlignItems(ui.Center).
		Padding(9, 10).Radius(tk.BorderRadius).Cursor(ui.CursorPointer)
	color := tk.TextSecondary
	weight := 500
	if active {
		b.Background(tk.FillTertiary)
		color = tk.Text
		weight = 600
	} else if b.Hovered() {
		b.Background(tk.FillQuaternary)
		color = tk.Text
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(color)
		ui.Text(c, label).FontSize(13).FontWeight(weight).TextColor(color)
	})
	if b.Clicked() {
		a.page = target
	}
}

func (a *App) pageContent(c *ui.Context, tk *theme.Tokens) {
	layout.ScrollArea(c, layout.ScrollAreaProps{Fade: true}, func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(32, 24, 40, 24).Children(func() {
			ui.Column(c).Width(900).MaxWidthPercent(100).Gap(24).Children(func() {
				switch a.page {
				case pageSettings:
					a.settingsPage(c, tk)
				case pageAbout:
					a.aboutPage(c, tk)
				default:
					a.downloadsPage(c, tk)
				}
			})
		})
	}).Grow(1).Basis(0).MinHeight(0)
}

func (a *App) downloadsPage(c *ui.Context, tk *theme.Tokens) {
	ui.Column(c).Gap(26).Children(func() {
		a.hero(c, tk)
		a.composer(c, tk)
		a.queue(c, tk)
	})
}

func (a *App) hero(c *ui.Context, tk *theme.Tokens) {
	ui.Column(c).Gap(10).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			display.Tag(c, a.t("hero.connections"), display.TagProps{
				Variant: display.TagFilled,
				Color:   display.TagBlue,
				Size:    theme.Small,
				Round:   true,
			})
			display.Tag(c, a.t("hero.pureGo"), display.TagProps{
				Variant: display.TagFilled,
				Color:   display.TagPurple,
				Size:    theme.Small,
				Round:   true,
			})
		})
		ui.Text(c, a.t("hero.title")).FontSize(34).FontWeight(600).
			LetterSpacing(-0.8).TextColor(tk.TextHeading).Balance()
		ui.Text(c, a.t("hero.subtitle")).
			FontSize(14).LineHeight(1.6).TextColor(tk.TextSecondary)
	})
}

func (a *App) composer(c *ui.Context, tk *theme.Tokens) {
	layout.Block(c, layout.BlockProps{Variant: layout.BlockOutlined, Shadow: true}).
		Padding(16).Gap(12).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			urlField, urlInput := textField(c, tk, &a.url,
				a.t("composer.placeholder"),
				lucide.Link2(), 40, 14, true)
			urlField.Grow(1).Basis(0)
			if urlInput.Submitted() {
				a.add(c)
			}
			if action.Button(c, a.t("composer.download"), action.ButtonProps{
				Variant: action.Primary,
				Size:    theme.Large,
			}).Clicked() {
				a.add(c)
			}
		})

		ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, a.t("composer.saveTo")).FontSize(11).TextColor(tk.TextTertiary)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					dirField, _ := textField(c, tk, &a.dir,
						a.t("composer.folderPlaceholder"), lucide.Folder(), 32, 13, false)
					dirField.Grow(1).Basis(0)
					if action.ActionIcon(c, lucide.FolderOpen(), action.ActionIconProps{
						Variant: action.ActionIconBorderless,
						Size:    theme.Middle,
						Title:   a.t("composer.browse"),
					}).Clicked() {
						a.browse()
					}
				})
			})
			ui.Column(c).Width(112).Gap(4).Children(func() {
				ui.Text(c, a.t("composer.connections")).FontSize(11).TextColor(tk.TextTertiary)
				field.InputNumber(c, &a.connections, field.InputNumberProps{
					Min:        1,
					Max:        float64(engine.MaxConnections),
					Step:       1,
					Precision:  0,
					Size:       theme.Middle,
					NoControls: false,
				})
			})
			ui.Column(c).Width(148).Gap(4).Children(func() {
				ui.Text(c, a.t("composer.protocol")).FontSize(11).TextColor(tk.TextTertiary)
				field.Select(c, &a.typeValue, field.SelectProps{
					Options: a.typeOptions(),
					Size:    theme.Middle,
				})
			})
		})
	})
}

func (a *App) queue(c *ui.Context, tk *theme.Tokens) {
	jobs := a.manager.Snapshot()
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.t("queue.title")).FontSize(18).FontWeight(600).TextColor(tk.TextHeading)
			display.Tag(c, fmt.Sprint(len(jobs)), display.TagProps{
				Variant: display.TagFilled,
				Color:   display.TagGray,
				Size:    theme.Small,
				Round:   true,
			})
			ui.Spacer(c)
			if action.Button(c, a.t("queue.clear"), action.ButtonProps{
				Variant: action.Text,
				Size:    theme.Small,
			}).Disabled(!a.manager.HasFinished()).Clicked() {
				a.manager.ClearFinished()
			}
		})

		if len(jobs) == 0 {
			display.Empty(c, display.EmptyProps{
				Icon:        lucide.Download(),
				Title:       a.t("queue.emptyTitle"),
				Description: a.t("queue.emptyDescription"),
				Action: func() {
					if action.Button(c, a.t("queue.openFolder"), action.ButtonProps{
						Variant: action.Default,
						Size:    theme.Middle,
					}).Clicked() {
						reveal(a.dir)
					}
				},
			})
			return
		}

		for i := len(jobs) - 1; i >= 0; i-- {
			a.jobCard(c, tk, jobs[i])
		}
		ui.Text(c, a.t("composer.footer")).
			FontSize(11).TextColor(tk.TextQuaternary).Margin(4, 0, 0, 0)
	})
}

func (a *App) jobCard(c *ui.Context, tk *theme.Tokens, j Job) {
	icon, tone, tagColor := jobTone(tk, j)
	card := layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(14).Gap(10)
	card.Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Size(32, 32).Radius(16).Background(tk.BgContainer).Center().Children(func() {
				ui.Icon(c, icon).FontSize(16).TextColor(tone)
			})

			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, jobTitle(j)).FontSize(14).FontWeight(500).TextColor(tk.Text).SingleLine()
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					display.Tag(c, a.statusLabel(j.Status), display.TagProps{
						Variant: display.TagFilled,
						Color:   tagColor,
						Size:    theme.Small,
						Round:   true,
					})
					if meta := a.jobMeta(j); meta != "" {
						ui.Text(c, meta).FontSize(12).TextColor(tk.TextTertiary).SingleLine()
					}
				})
			})

			ui.Row(c).Shrink(0).Gap(2).AlignItems(ui.Center).Children(func() {
				switch j.Status {
				case StatusQueued, StatusRunning:
					if action.ActionIcon(c, lucide.X(), action.ActionIconProps{
						Size:  theme.Middle,
						Title: a.t("action.cancel"),
					}).Clicked() {
						a.manager.Cancel(j.ID)
					}
				case StatusError, StatusCanceled:
					if action.ActionIcon(c, lucide.RotateCw(), action.ActionIconProps{
						Size:  theme.Middle,
						Title: a.t("action.retry"),
					}).Clicked() {
						if err := a.manager.Retry(j.ID); err != nil {
							feedback.ShowToast(c, feedback.ToastProps{
								Type:        feedback.ToastError,
								Title:       a.t("toast.retryFailed"),
								Description: err.Error(),
							})
						}
					}
				}
				if j.Status == StatusDone && j.Path != "" {
					if action.ActionIcon(c, lucide.FolderOpen(), action.ActionIconProps{
						Size:  theme.Middle,
						Title: a.t("action.reveal"),
					}).Clicked() {
						reveal(j.Path)
					}
					if action.ActionIcon(c, lucide.Copy(), action.ActionIconProps{
						Size:  theme.Middle,
						Title: a.t("action.copyPath"),
					}).Clicked() {
						mygo.Clipboard.WriteText(j.Path)
						feedback.ShowToast(c, feedback.ToastProps{Type: feedback.ToastSuccess, Title: a.t("toast.pathCopied")})
					}
				}
				if j.Status != StatusRunning {
					if action.ActionIcon(c, lucide.Trash2(), action.ActionIconProps{
						Size:   theme.Middle,
						Title:  a.t("action.remove"),
						Danger: true,
					}).Clicked() {
						a.manager.Remove(j.ID)
					}
				}
			})
		})

		switch j.Status {
		case StatusQueued:
		case StatusDone:
			feedback.Progress(c, 100, feedback.ProgressProps{
				Size:   theme.Small,
				Status: feedback.ProgressSuccess,
			})
		case StatusError:
			feedback.Progress(c, float32(j.Fraction()*100), feedback.ProgressProps{
				Size:   theme.Small,
				Status: feedback.ProgressException,
			})
		case StatusCanceled:
			feedback.Progress(c, float32(j.Fraction()*100), feedback.ProgressProps{
				Size:  theme.Small,
				Color: tk.TextQuaternary,
			})
		default:
			feedback.Progress(c, float32(j.Fraction()*100), feedback.ProgressProps{
				Size:   theme.Small,
				Status: feedback.ProgressActive,
				Color:  tk.Primary,
			})
		}

		if j.Err != "" {
			ui.Text(c, j.Err).FontSize(12).LineHeight(1.5).TextColor(tk.ErrorText).MaxLines(2)
		}
		if j.Status == StatusDone && j.Path != "" {
			ui.Text(c, j.Path).FontSize(12).TextColor(tk.TextQuaternary).SingleLine()
		}
	})
}

func (a *App) settingsPage(c *ui.Context, tk *theme.Tokens) {
	a.pageHeader(c, tk, a.t("settings.title"), a.t("settings.subtitle"))

	layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(14).Children(func() {
		a.sectionTitle(c, tk, a.t("settings.general"))

		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, a.t("settings.language")).FontSize(11).TextColor(tk.TextTertiary)
			previous := a.language
			field.Select(c, &a.language, field.SelectProps{
				Options: a.languageOptions(),
				Size:    theme.Middle,
			})
			if a.language != previous {
				a.setLanguage(a.language)
				feedback.ShowToast(c, feedback.ToastProps{Type: feedback.ToastSuccess, Title: a.t("toast.settingsSaved")})
			}
			ui.Text(c, a.lang.Tf("settings.languageHelp", localesDir())).
				FontSize(11).TextColor(tk.TextQuaternary).SingleLine()
		})

		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, a.t("settings.folder")).FontSize(11).TextColor(tk.TextTertiary)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				dirField, _ := textField(c, tk, &a.dir, a.t("composer.folderPlaceholder"), lucide.Folder(), 32, 13, false)
				dirField.Grow(1).Basis(0)
				if action.ActionIcon(c, lucide.FolderOpen(), action.ActionIconProps{
					Variant: action.ActionIconBorderless,
					Size:    theme.Middle,
					Title:   a.t("composer.browse"),
				}).Clicked() {
					a.browse()
				}
			})
		})

		ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
			ui.Column(c).Width(140).Gap(4).Children(func() {
				ui.Text(c, a.t("settings.connections")).FontSize(11).TextColor(tk.TextTertiary)
				field.InputNumber(c, &a.connections, field.InputNumberProps{
					Min:       1,
					Max:       float64(engine.MaxConnections),
					Step:      1,
					Precision: 0,
					Size:      theme.Middle,
				})
			})
			ui.Column(c).Width(180).Gap(4).Children(func() {
				ui.Text(c, a.t("settings.protocol")).FontSize(11).TextColor(tk.TextTertiary)
				field.Select(c, &a.typeValue, field.SelectProps{
					Options: a.typeOptions(),
					Size:    theme.Middle,
				})
			})
			ui.Spacer(c)
			if action.Button(c, a.t("settings.save"), action.ButtonProps{
				Variant: action.Primary,
				Size:    theme.Middle,
			}).Clicked() {
				if err := a.SaveSettings(); err != nil {
					feedback.ShowToast(c, feedback.ToastProps{Type: feedback.ToastError, Title: a.t("toast.addFailed"), Description: err.Error()})
				} else {
					feedback.ShowToast(c, feedback.ToastProps{Type: feedback.ToastSuccess, Title: a.t("toast.settingsSaved")})
				}
			}
		})
	})

	layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(10).Children(func() {
		a.sectionTitle(c, tk, a.t("settings.browser"))
		ui.Text(c, a.t("settings.browserDescription")).
			FontSize(13).LineHeight(1.6).TextColor(tk.TextSecondary)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.t("settings.browserId")).FontSize(11).TextColor(tk.TextTertiary)
			ui.Text(c, extensionID).FontSize(11).Font("monospace").Selectable().TextColor(tk.TextSecondary)
		})
		ui.Text(c, a.t("settings.browserUpdate")).FontSize(12).TextColor(tk.TextTertiary)
	})
}

func (a *App) aboutPage(c *ui.Context, tk *theme.Tokens) {
	a.pageHeader(c, tk, a.t("about.title"), a.t("about.subtitle"))

	layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(10).Children(func() {
		ui.Text(c, a.t("about.description")).FontSize(14).LineHeight(1.7).TextColor(tk.TextSecondary)
		ui.Text(c, a.t("about.license")).FontSize(12).TextColor(tk.TextTertiary)
	})

	layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(10).Children(func() {
		a.sectionTitle(c, tk, a.t("about.protocols"))
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, label := range []string{"HTTP(S)", "FTP / FTPS", "HLS", "DASH", "BitTorrent"} {
				display.Tag(c, label, display.TagProps{
					Variant: display.TagFilled,
					Color:   display.TagGray,
					Size:    theme.Small,
					Round:   true,
				})
			}
		})
	})

	layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.t("settings.version")).FontSize(12).TextColor(tk.TextTertiary)
			ui.Text(c, version.String()).FontSize(12).TextColor(tk.TextSecondary)
		})
		if action.Button(c, a.t("about.github"), action.ButtonProps{
			Variant: action.Default,
			Size:    theme.Middle,
		}).Clicked() {
			_ = mygo.Shell.OpenExternal("https://github.com/neko233-com/DownloadX")
		}
	})
}

func (a *App) pageHeader(c *ui.Context, tk *theme.Tokens, title, subtitle string) {
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(26).FontWeight(600).LetterSpacing(-0.5).TextColor(tk.TextHeading)
		ui.Text(c, subtitle).FontSize(13).LineHeight(1.6).TextColor(tk.TextSecondary)
	})
}

func (a *App) sectionTitle(c *ui.Context, tk *theme.Tokens, title string) {
	ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(tk.Text)
}

func (a *App) add(c *ui.Context) {
	url := strings.TrimSpace(a.url)
	if url == "" {
		feedback.ShowToast(c, feedback.ToastProps{Type: feedback.ToastWarning, Title: a.t("toast.pasteFirst")})
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
	if _, err := a.manager.Add(Request{URL: url, Dir: dir, Connections: conn, Type: typ}); err != nil {
		feedback.ShowToast(c, feedback.ToastProps{
			Type:        feedback.ToastError,
			Title:       a.t("toast.addFailed"),
			Description: err.Error(),
		})
		return
	}
	a.url = ""
	_ = a.SaveSettings()
	feedback.ShowToast(c, feedback.ToastProps{Type: feedback.ToastSuccess, Title: a.t("toast.added")})
}

func (a *App) browse() {
	win := a.win
	dir := a.dir
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            win,
			Title:             "Choose a download folder",
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

func (a *App) typeOptions() []field.SelectOption {
	return []field.SelectOption{
		{Value: "auto", Label: a.t("type.auto")},
		{Value: "http", Label: a.t("type.http")},
		{Value: "ftp", Label: a.t("type.ftp")},
		{Value: "hls", Label: a.t("type.hls")},
		{Value: "dash", Label: a.t("type.dash")},
		{Value: "bt", Label: a.t("type.bt")},
	}
}

func (a *App) languageOptions() []field.SelectOption {
	languages := a.lang.Languages()
	options := make([]field.SelectOption, 0, len(languages))
	for _, language := range languages {
		options = append(options, field.SelectOption{Value: language.Code, Label: language.Name})
	}
	return options
}

func (a *App) statusLabel(s Status) string {
	switch s {
	case StatusQueued:
		return a.t("status.queued")
	case StatusRunning:
		return a.t("status.running")
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

func jobTone(tk *theme.Tokens, j Job) (*ui.SVG, ui.Color, display.TagColor) {
	switch j.Status {
	case StatusQueued:
		return lucide.Clock(), tk.TextTertiary, display.TagGray
	case StatusRunning:
		return lucide.LoaderCircle(), tk.Primary, display.TagProcessing
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

func jobTitle(j Job) string {
	if j.Path != "" {
		return filepath.Base(j.Path)
	}
	if name := engine.FilenameFromURL(j.URL); name != "" && name != "download" {
		return name
	}
	if strings.HasPrefix(strings.ToLower(j.URL), "magnet:") {
		return "Magnet download"
	}
	if info, err := os.Stat(j.URL); err == nil && !info.IsDir() {
		return filepath.Base(j.URL)
	}
	return j.URL
}

func (a *App) jobMeta(j Job) string {
	switch j.Status {
	case StatusQueued:
		return a.t("meta.waiting")
	case StatusRunning:
		parts := make([]string, 0, 3)
		if j.ItemsTotal > 0 {
			parts = append(parts, a.t("meta.segments", j.ItemsDone, j.ItemsTotal))
		} else if j.Total > 0 {
			parts = append(parts, humanBytes(j.Done)+" / "+humanBytes(j.Total))
		} else if j.Done > 0 {
			parts = append(parts, humanBytes(j.Done))
		}
		if j.Speed > 0 {
			parts = append(parts, humanBytes(int64(j.Speed))+"/s")
		}
		if j.Connections > 0 {
			if j.Type == engine.TypeBT || strings.HasPrefix(strings.ToLower(j.URL), "magnet:") {
				parts = append(parts, a.t("meta.peers", j.Connections))
			} else {
				parts = append(parts, a.t("meta.connections", j.Connections))
			}
		}
		if msg := j.Message; msg != "" && msg != "segments" && msg != "peers" && msg != "seeding" {
			parts = append(parts, msg)
		}
		if len(parts) == 0 {
			parts = append(parts, j.Phase)
		}
		return strings.Join(parts, " · ")
	case StatusDone:
		size := humanBytes(j.Bytes)
		if j.Bytes == 0 {
			size = humanBytes(j.Total)
		}
		if !j.Started.IsZero() && !j.Finished.IsZero() {
			return size + " · " + humanDuration(j.Finished.Sub(j.Started))
		}
		return size
	case StatusCanceled:
		return ""
	case StatusError:
		return ""
	}
	return ""
}

func humanBytes(n int64) string {
	if n < 0 {
		return "?"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func humanDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
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

// textField builds a Lobe-styled text input that can stretch in a row.
// field.Input returns its inner text input, so it cannot grow with the
// surrounding surface; this keeps the styled box and the input together.
func textField(c *ui.Context, tk *theme.Tokens, value *string, placeholder string, icon *ui.SVG, height, fontSize float32, autoFocus bool) (ui.Element, ui.Element) {
	root := ui.Row(c).Gap(8).AlignItems(ui.Center).Height(height).Padding(0, 12).
		Radius(tk.BorderRadius).Border(1, tk.BorderSecondary).Background(tk.BgContainer)
	var input ui.Element
	root.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(fontSize).TextColor(tk.TextTertiary).Shrink(0)
		}
		input = ui.TextInput(c, value).Grow(1).FocusRing(false).
			Placeholder(placeholder).PlaceholderColor(tk.TextPlaceholder).FontSize(fontSize)
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
