package desktop

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/choice"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/layout"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"
	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/version"
)

func (a *App) settingsPage(c *ui.Context, tk *theme.Tokens) {
	ui.Row(c).FillWidth().Justify(ui.Center).Padding(24, 18, 32, 24).Children(func() {
		ui.Column(c).Width(760).MaxWidthPercent(100).Gap(16).Children(func() {
			a.pageHeader(c, tk, a.t("settings.title"), a.t("settings.subtitle"))

			layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(14).Children(func() {
				a.sectionTitle(c, tk, a.t("settings.general"))
				a.languageField(c, tk)
				a.toggleField(c, tk, &a.startWithWindows, a.t("settings.startWithWindows"), a.t("settings.startWithWindowsHelp"), func() {
					_ = mygo.App.SetOpenAtLogin(a.startWithWindows)
					_ = a.SaveSettings()
				})
				a.toggleField(c, tk, &a.closeToTray, a.t("settings.closeToTray"), a.t("settings.closeToTrayHelp"), func() {
					_ = a.SaveSettings()
				})
				a.toggleField(c, tk, &a.autoCheckUpdates, a.t("settings.autoUpdate"), a.t("settings.autoUpdateHelp"), func() {
					_ = a.SaveSettings()
					if a.autoCheckUpdates {
						a.CheckUpdates(false)
					}
				})
				a.toggleField(c, tk, &a.monitorClipboard, a.t("settings.clipboard"), a.t("settings.clipboardHelp"), func() {
					a.applyClipboardSetting()
					_ = a.SaveSettings()
				})
			})

			layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(14).Children(func() {
				a.sectionTitle(c, tk, a.t("settings.downloads"))
				ui.Column(c).Gap(5).Children(func() {
					ui.Text(c, a.t("settings.folder")).FontSize(12).TextColor(tk.TextTertiary)
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						folder, _ := textField(c, tk, &a.dir, a.t("composer.folderPlaceholder"), lucide.Folder(), 34, 13, false)
						folder.Grow(1).Basis(0)
						if action.ActionIcon(c, lucide.FolderOpen(), action.ActionIconProps{
							Variant: action.ActionIconBorderless,
							Size:    theme.Middle,
							Title:   a.t("composer.browse"),
						}).Clicked() {
							a.browse()
						}
					})
				})
				ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
					ui.Column(c).Width(150).Gap(5).Children(func() {
						ui.Text(c, a.t("settings.connections")).FontSize(12).TextColor(tk.TextTertiary)
						field.InputNumber(c, &a.connections, field.InputNumberProps{
							Min:       1,
							Max:       float64(engine.MaxConnections),
							Step:      1,
							Precision: 0,
							Size:      theme.Middle,
						})
					})
					ui.Column(c).Width(190).Gap(5).Children(func() {
						ui.Text(c, a.t("settings.protocol")).FontSize(12).TextColor(tk.TextTertiary)
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
							a.showToast(feedback.ToastError, a.t("toast.settingsFailed"), err.Error())
						} else {
							a.showToast(feedback.ToastSuccess, a.t("toast.settingsSaved"), "")
						}
					}
				})
			})

			a.updateSettingsBlock(c, tk)

			layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(10).Children(func() {
				a.sectionTitle(c, tk, a.t("settings.browser"))
				ui.Text(c, a.t("settings.browserDescription")).FontSize(13).LineHeight(1.6).TextColor(tk.TextSecondary)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.t("settings.browserId")).FontSize(11).TextColor(tk.TextTertiary)
					ui.Text(c, extensionID).FontSize(11).Font("monospace").Selectable().TextColor(tk.TextSecondary)
				})
				ui.Text(c, a.t("settings.browserUpdate")).FontSize(12).TextColor(tk.TextTertiary)
			})
		})
	})
}

func (a *App) languageField(c *ui.Context, tk *theme.Tokens) {
	ui.Column(c).Gap(5).Children(func() {
		ui.Text(c, a.t("settings.language")).FontSize(12).TextColor(tk.TextTertiary)
		previous := a.languageChoice
		field.Select(c, &a.languageChoice, field.SelectProps{
			Options: a.languageOptions(),
			Size:    theme.Middle,
		})
		if a.languageChoice != previous {
			a.setLanguage(a.languageChoice)
			a.rebuildTrayMenu()
			a.showToast(feedback.ToastSuccess, a.t("toast.settingsSaved"), "")
		}
		active := a.lang.Code()
		if a.languageAuto {
			active = a.t("settings.systemDefault") + " · " + a.languageName(a.lang.Code())
		}
		ui.Text(c, a.t("settings.languageHelp", localesDir())).FontSize(11).LineHeight(1.5).TextColor(tk.TextQuaternary).MaxLines(2)
		ui.Text(c, active).FontSize(11).TextColor(tk.TextTertiary)
	})
}

func (a *App) toggleField(c *ui.Context, tk *theme.Tokens, value *bool, title, help string, changed func()) {
	ui.Column(c).Gap(2).Children(func() {
		cb := choice.Checkbox(c, value, title, choice.CheckboxProps{})
		if cb.Changed() {
			changed()
		}
		ui.Text(c, help).FontSize(11).LineHeight(1.5).TextColor(tk.TextQuaternary).Margin(24, 0, 0, 0)
	})
}

func (a *App) updateSettingsBlock(c *ui.Context, tk *theme.Tokens) {
	layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(18).Gap(10).Children(func() {
		a.sectionTitle(c, tk, a.t("settings.updates"))
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.t("settings.currentVersion")+": "+version.String()).FontSize(12).TextColor(tk.TextSecondary)
			if a.lastUpdateCheck > 0 {
				ui.Text(c, a.t("settings.lastChecked")+": "+dateText(time.Unix(a.lastUpdateCheck, 0))).FontSize(11).TextColor(tk.TextTertiary)
			}
		})
		status := a.statusText()
		if status != "" {
			color := tk.TextSecondary
			switch a.updateState {
			case updateAvailable, updateReady:
				color = tk.Primary
			case updateError:
				color = tk.Error
			case updateUptodate:
				color = tk.Success
			}
			ui.Text(c, status).FontSize(12).TextColor(color)
		}
		if a.updateState == updateDownloading {
			feedback.Progress(c, float32(a.updateProgress), feedback.ProgressProps{
				Size:     theme.Small,
				Status:   feedback.ProgressActive,
				ShowInfo: true,
			})
		}
		if a.updateErr != "" {
			ui.Text(c, a.updateErr).FontSize(11).TextColor(tk.Error).MaxLines(3)
		}
		ui.Row(c).Gap(8).Children(func() {
			if action.Button(c, a.t("settings.checkNow"), action.ButtonProps{
				Variant: action.Default,
				Size:    theme.Small,
			}).Disabled(a.updateChecking).Clicked() {
				a.CheckUpdates(true)
			}
			switch a.updateState {
			case updateAvailable:
				if action.Button(c, a.t("settings.downloadUpdate"), action.ButtonProps{
					Variant: action.Primary,
					Size:    theme.Small,
				}).Clicked() {
					a.DownloadUpdate()
				}
			case updateReady:
				if action.Button(c, a.t("settings.installUpdate"), action.ButtonProps{
					Variant: action.Primary,
					Size:    theme.Small,
				}).Clicked() {
					a.InstallUpdate()
				}
			}
			if a.updateRelease != nil {
				if action.Button(c, a.t("settings.releaseNotes"), action.ButtonProps{
					Variant: action.Text,
					Size:    theme.Small,
				}).Clicked() {
					a.updateOpen = true
				}
			}
		})
	})
}

func (a *App) aboutPage(c *ui.Context, tk *theme.Tokens) {
	ui.Row(c).FillWidth().Justify(ui.Center).Padding(28, 22, 32, 28).Children(func() {
		ui.Column(c).Width(700).MaxWidthPercent(100).Gap(16).Children(func() {
			a.pageHeader(c, tk, a.t("about.title"), a.t("about.subtitle"))
			layout.Block(c, layout.BlockProps{Variant: layout.BlockFilled}).Padding(22).Gap(14).Children(func() {
				ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
					if a.logo != nil {
						ui.Image(c, a.logo).Size(56, 56)
					}
					ui.Column(c).Gap(3).Children(func() {
						ui.Text(c, "DownloadX").FontSize(24).FontWeight(600).TextColor(tk.TextHeading)
						ui.Text(c, version.String()).FontSize(12).TextColor(tk.TextTertiary)
					})
				})
				ui.Text(c, a.t("about.description")).FontSize(13).LineHeight(1.7).TextColor(tk.TextSecondary)
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
				ui.Text(c, a.t("about.builtWith")).FontSize(12).TextColor(tk.TextTertiary)
				ui.Row(c).Gap(8).Children(func() {
					if action.Button(c, a.t("about.github"), action.ButtonProps{
						Variant: action.Default,
						Size:    theme.Middle,
					}).Clicked() {
						_ = mygo.Shell.OpenExternal("https://github.com/neko233-com/DownloadX")
					}
					if action.Button(c, a.t("about.openFolder"), action.ButtonProps{
						Variant: action.Text,
						Size:    theme.Middle,
					}).Clicked() {
						reveal(a.dir)
					}
				})
			})
		})
	})
}

func (a *App) pageHeader(c *ui.Context, tk *theme.Tokens, title, subtitle string) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, title).FontSize(24).FontWeight(600).LetterSpacing(-0.4).TextColor(tk.TextHeading)
		ui.Text(c, subtitle).FontSize(12).LineHeight(1.6).TextColor(tk.TextSecondary)
	})
}

func (a *App) sectionTitle(c *ui.Context, tk *theme.Tokens, title string) {
	ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(tk.Text)
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
	options := []field.SelectOption{{Value: "system", Label: a.t("settings.systemDefault")}}
	for _, language := range a.lang.Languages() {
		options = append(options, field.SelectOption{Value: language.Code, Label: language.Name})
	}
	return options
}

func (a *App) languageName(code string) string {
	for _, language := range a.lang.Languages() {
		if language.Code == code {
			return language.Name
		}
	}
	return code
}

func (a *App) applyClipboardSetting() {
	if a.monitorClipboard {
		if a.clipboardOff == nil {
			a.startClipboard()
		}
		return
	}
	if a.clipboardOff != nil {
		a.clipboardOff()
		a.clipboardOff = nil
	}
}
