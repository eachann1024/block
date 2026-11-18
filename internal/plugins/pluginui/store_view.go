package pluginui

import (
	"fmt"
	"strconv"
	"strings"

	"block/internal/icons"
	"block/internal/modals"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func renderStoreHeaderExtras(c *ui.Context, ctrl *Controller) {
	refreshing, _, _, _ := ctrl.sup.StoreStatus()

	ui.Row(c.Key("app-store-header-extras")).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
		filterIdx := ctrl.storeFilterValue()
		if bui.SegmentedControl(c, "app-store-filter", &filterIdx, []bui.SegmentOption{
			{ID: "app-store-filter-all", Label: theme.Tr("全部", "All")},
			{ID: "app-store-filter-installed", Label: theme.Tr("已安装", "Installed")},
		}) {
			ctrl.setStoreFilter(filterIdx)
		}

		refreshLbl := theme.Tr("刷新", "Refresh")
		if refreshing {
			refreshLbl = theme.Tr("刷新中…", "Refreshing…")
		}
		refIcon := icons.Refresh
		refBtn := bui.ActionButton(c, "app-store-refresh", refreshLbl, &refIcon, bui.ButtonSecondary, refreshing)
		if refBtn.Clicked() {
			ctrl.sup.RefreshCatalogAsync()
		}
	})
}

func renderStoreBody(c *ui.Context, ctrl *Controller, onSelectTab func(nextTab string)) {
	pal := theme.Ocean()
	locale := ctrl.sup.Locale()
	refreshing, installingID, storeErr, storeNotice := ctrl.sup.StoreStatus()
	catalog := ctrl.sup.Catalog()
	installed := ctrl.sup.Installed()
	filter := ctrl.storeFilterValue()

	installedMap := make(map[string]plugins.InstalledApp, len(installed))
	for _, app := range installed {
		installedMap[app.Manifest.ID] = app
	}

	entries := buildVisibleStoreEntries(catalog, installed, installedMap, filter)

	ui.Column(c.Key("app-store-body")).FillWidth().MinWidth(0).Gap(theme.Space24).Children(func() {
		if storeErr != "" {
			ui.Row(c.Key("app-store-error")).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Radius(theme.RadiusMD).
				Background(pal.DangerSurface).Border(1, pal.Danger).AlignItems(ui.Center).Children(func() {
				ui.Text(c, storeErr).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Danger)
			})
		} else if storeNotice != "" {
			ui.Row(c.Key("app-store-notice")).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Radius(theme.RadiusMD).
				Background(pal.Selected).Border(1, pal.Accent).AlignItems(ui.Center).Children(func() {
				ui.Text(c, storeNotice).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text)
			})
		}

		renderManualInstallCard(c, ctrl, installingID, onSelectTab)

		if len(entries) == 0 {
			c.Key("app-store-empty-card")
			bui.Card(c, func() {
				msg := theme.Tr("暂无可用应用", "No apps available")
				if filter == 1 {
					msg = theme.Tr("暂无已安装应用", "No installed apps")
				} else if refreshing {
					msg = theme.Tr("正在加载应用目录…", "Loading catalog…")
				}
				ui.Text(c, msg).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			})
			return
		}

		vpW, _ := c.Size()
		twoCol := vpW == 0 || vpW >= 920

		ui.Column(c.Key("app-store-list")).FillWidth().MinWidth(0).Gap(theme.Space16).Children(func() {
			if !twoCol {
				for _, item := range entries {
					entry := item
					_, isInstalled := installedMap[entry.ID]
					renderStoreAppCard(c, ctrl, entry, locale, isInstalled, installingID, onSelectTab, false)
				}
				return
			}
			for i := 0; i < len(entries); i += 2 {
				rowIdx := i / 2
				ui.Row(c.Key(fmt.Sprintf("app-store-row-%d", rowIdx))).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Stretch).Children(func() {
					left := entries[i]
					_, leftInstalled := installedMap[left.ID]
					renderStoreAppCard(c, ctrl, left, locale, leftInstalled, installingID, onSelectTab, true)
					if i+1 < len(entries) {
						right := entries[i+1]
						_, rightInstalled := installedMap[right.ID]
						renderStoreAppCard(c, ctrl, right, locale, rightInstalled, installingID, onSelectTab, true)
					} else {
						ui.Box(c.Key("app-store-grid-spacer")).Grow(1).Basis(0).MinWidth(0)
					}
				})
			}
		})
	})
}

func renderManualInstallCard(c *ui.Context, ctrl *Controller, installingID string, onSelectTab func(nextTab string)) {
	pal := theme.Ocean()
	c.Key("app-store-manual-card")
	bui.Card(c, func() {
		ui.Text(c, theme.Tr("手动安装", "Manual Install")).
			FontSize(theme.FontTitle).
			FixedLineHeight(theme.LineTitle).
			FontWeight(theme.WeightTitle).
			TextColor(pal.Text).
			SingleLine()

		spec := ctrl.manualSpecValue()
		ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("app-store-manual-input"), &spec).
				Placeholder(theme.Tr("输入 owner/repo、压缩包 URL 或本地目录路径", "Enter owner/repo, archive URL, or local directory path")).
				Label(theme.Tr("手动安装插件源", "Plugin Source")).
				FontSize(theme.FontBody).
				Height(theme.HeightControl).
				PaddingX(theme.Space10).
				Radius(theme.RadiusMD).
				Background(pal.Shell).
				Border(1, pal.BtnBorder).
				TextColor(pal.Text).
				PlaceholderColor(pal.Placeholder).
				Grow(1).
				MinWidth(0)
			bui.Probe(in, "app-store-manual-input")
			if in.Changed() {
				ctrl.setManualSpec(spec)
			}
			submitted := in.Submitted()

			manualBusy := installingID == "__manual__"
			if ctrl.sup != nil && ctrl.sup.CanPickDirectory() {
				pickBtn := bui.ActionButton(c, "app-store-manual-browse", theme.Tr("本地目录…", "Browse…"), nil, bui.ButtonSecondary, installingID != "")
				if pickBtn.Clicked() {
					ctrl.sup.PickDirectory(theme.Tr("选择本地插件目录", "Choose Local Plugin Directory"), func(chosen string) {
						ctrl.setManualSpec(chosen)
					})
				}
			}
			disabled := installingID != "" || strings.TrimSpace(spec) == ""
			btnLbl := theme.Tr("安装", "Install")
			if manualBusy {
				btnLbl = theme.Tr("安装中…", "Installing…")
			}
			btn := bui.ActionButton(c, "app-store-manual-install", btnLbl, nil, bui.ButtonPrimary, disabled)
			if (btn.Clicked() || submitted) && !disabled {
				source := spec
				ctrl.sup.InstallFromSourceAsync(source, func(pluginID string) {
					ctrl.clearManualSpec()
					if onSelectTab != nil {
						onSelectTab(modals.TabPluginPrefix + pluginID)
					}
				})
			}
		})
	})
}

func renderStoreAppCard(
	c *ui.Context,
	ctrl *Controller,
	entry plugins.StoreAppEntry,
	locale string,
	isInstalled bool,
	installingID string,
	onSelectTab func(nextTab string),
	inGrid bool,
) {
	pal := theme.Ocean()
	downloads := entry.Downloads
	if downloads < 0 {
		downloads = 0
	}
	if store := ctrl.sup.Store(); store != nil {
		downloads = store.EffectiveDownloads(entry)
	}
	dlStr := formatDownloadCount(downloads)

	card := ui.Column(c.Key("app-store-card-" + entry.ID)).
		Padding(theme.CardPadY, theme.CardPadX, theme.CardPadY, theme.CardPadX).
		Gap(theme.CardGap).
		Radius(theme.RadiusXL).
		Background(pal.Card).
		Border(1, pal.Hairline).
		Justify(ui.SpaceBetween)
	if inGrid {
		card.Grow(1).Basis(0).MinWidth(0)
	} else {
		card.FillWidth().MinWidth(0)
	}

	card.Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space12).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Start).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space12).AlignItems(ui.Start).Children(func() {
					ui.Box(c).Size(theme.HeightLarge, theme.HeightLarge).Shrink(0).Center().
						Radius(theme.RadiusLG).Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
						DrawDuoSpec(c, entry.Icon, theme.IconChrome, pal.DuoIconColors(pal.Text))
					})

					ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space4).Children(func() {
						ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, entry.Name.Resolve(locale)).
								FontSize(theme.FontTitle).
								FixedLineHeight(theme.LineTitle).
								FontWeight(theme.WeightTitle).
								TextColor(pal.Text).
								SingleLine().
								Ellipsis("…")
							if ver := strings.TrimSpace(entry.Version); ver != "" {
								ui.Text(c, "v"+strings.TrimPrefix(ver, "v")).
									Font("monospace").
									FontSize(theme.FontSmall).
									FixedLineHeight(theme.LineSmall).
									TextColor(pal.TextSecondary).
									SingleLine()
							}
						})

						ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
							renderVerifiedBadge(c, entry.Verified)
							if author := strings.TrimSpace(entry.Author); author != "" {
								ui.Text(c, author).
									FontSize(theme.FontSmall).
									FixedLineHeight(theme.LineSmall).
									TextColor(pal.TextSecondary).
									SingleLine().
									Ellipsis("…")
							}
						})
					})
				})

				ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
					if isInstalled {
						openBtn := bui.ActionButton(c, "app-store-open-"+entry.ID, theme.Tr("打开", "Open"), nil, bui.ButtonSecondary, false)
						if openBtn.Clicked() && onSelectTab != nil {
							onSelectTab(modals.TabPluginPrefix + entry.ID)
						}
						uninstBtn := bui.ActionButton(c, "app-store-uninstall-"+entry.ID, theme.Tr("卸载", "Uninstall"), nil, bui.ButtonDanger, false)
						if uninstBtn.Clicked() {
							ctrl.uninstallPlugin(entry.ID, nil)
						}
						return
					}

					isThisInstalling := installingID == entry.ID
					lbl := theme.Tr("安装", "Install")
					if isThisInstalling {
						lbl = theme.Tr("安装中…", "Installing…")
					}
					instBtn := bui.ActionButton(c, "app-store-install-"+entry.ID, lbl, nil, bui.ButtonPrimary, installingID != "")
					if instBtn.Clicked() {
						appEntry := entry
						ctrl.sup.InstallEntryAsync(appEntry, func(pluginID string) {
							if onSelectTab != nil {
								onSelectTab(modals.TabPluginPrefix + pluginID)
							}
						})
					}
				})
			})

			if summary := strings.TrimSpace(entry.Summary.Resolve(locale)); summary != "" {
				ui.Text(c, summary).
					FontSize(theme.FontBody).
					FixedLineHeight(theme.LineBody).
					TextColor(pal.TextSecondary).
					MaxLines(2).
					Ellipsis("…")
			}
		})

		ui.Row(c).FillWidth().MinWidth(0).
			Padding(theme.Space10, 0, 0, 0).
			BorderWidth(1, 0, 0, 0).
			BorderColor(pal.Hairline).
			Gap(theme.Space12).
			AlignItems(ui.Center).
			Justify(ui.SpaceBetween).
			Children(func() {
				ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
					icons.Draw(c, icons.Download, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
					ui.Text(c, theme.Tr(dlStr+" 次下载", dlStr+" downloads")).
						FontSize(theme.FontMeta).
						FixedLineHeight(theme.LineMeta).
						TextColor(pal.TextSecondary).
						SingleLine()
				})

				if len(entry.Permissions) > 0 {
					ui.Row(c).Grow(1).MinWidth(0).Wrap().Gap(theme.Space4).Justify(ui.End).AlignItems(ui.Center).Children(func() {
						for _, perm := range entry.Permissions {
							renderPermissionCapsule(c, plugins.PermissionLabel(perm, locale))
						}
					})
				}
			})
	})
}

func formatDownloadCount(n int) string {
	if n < 0 {
		n = 0
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	rem := len(s) % 3
	if rem > 0 {
		out = append(out, s[:rem]...)
	}
	for i := rem; i < len(s); i += 3 {
		if len(out) > 0 {
			out = append(out, ',')
		}
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}

func renderVerifiedBadge(c *ui.Context, verified bool) {
	pal := theme.Ocean()
	label := theme.Tr("社区收录", "Community")
	bg := pal.Shell
	bd := pal.Hairline
	fg := pal.TextSecondary
	if verified {
		label = theme.Tr("官方认证", "Official")
		bg = pal.Selected
		bd = pal.Accent
		fg = pal.Accent
	}
	ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusPill).
		Background(bg).Border(1, bd).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).
			FontSize(theme.FontMeta).
			FixedLineHeight(theme.LineMeta).
			FontWeight(theme.WeightControl).
			TextColor(fg).
			SingleLine()
	})
}

func renderPermissionCapsule(c *ui.Context, label string) {
	pal := theme.Ocean()
	ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusSM).
		Background(pal.CodeBG).Border(1, pal.Hairline).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).
			FontSize(theme.FontMeta).
			FixedLineHeight(theme.LineMeta).
			TextColor(pal.TextSecondary).
			SingleLine()
	})
}

func buildVisibleStoreEntries(
	catalog *plugins.StoreIndex,
	installed []plugins.InstalledApp,
	installedMap map[string]plugins.InstalledApp,
	filter int,
) []plugins.StoreAppEntry {
	var all []plugins.StoreAppEntry
	seen := make(map[string]bool)
	if catalog != nil {
		for _, app := range catalog.Apps {
			if app.ID == "" || seen[app.ID] {
				continue
			}
			seen[app.ID] = true
			all = append(all, app)
		}
	}
	for _, inst := range installed {
		id := inst.Manifest.ID
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		all = append(all, plugins.StoreAppEntry{
			App:         inst.Manifest.App,
			ID:          id,
			Name:        inst.Manifest.Name,
			Version:     inst.Manifest.Version,
			Author:      inst.Manifest.Author,
			Repo:        inst.Manifest.Repo,
			Summary:     inst.Manifest.Summary,
			Permissions: inst.Manifest.Permissions,
			Surfaces:    inst.Manifest.Surfaces,
			Icon:        inst.Manifest.Icon,
		})
	}
	if filter != 1 {
		return all
	}
	filtered := make([]plugins.StoreAppEntry, 0, len(installed))
	for _, entry := range all {
		if _, ok := installedMap[entry.ID]; ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
