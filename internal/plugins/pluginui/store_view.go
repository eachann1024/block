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
	"golang.org/x/mod/semver"
)

func renderStoreHeaderExtras(c *ui.Context, ctrl *Controller) {
	refreshing, _, _, _ := ctrl.sup.StoreStatus()

	ui.Row(c.Key("app-store-header-extras")).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
		filterIdx := ctrl.storeFilterValue()
		if bui.SegmentedControlWithOptions(c, "app-store-filter", &filterIdx, []bui.SegmentOption{
			{ID: "app-store-filter-all", Label: theme.Tr("全部", "All")},
			{ID: "app-store-filter-installed", Label: theme.Tr("已安装", "Installed")},
		}, bui.SegmentedControlOptions{
			TrackHeight:   theme.SegmentedControlHeight,
			TrackPadding:  theme.Space2,
			SegmentHeight: theme.SegmentedInnerHeight,
			SegmentRadius: theme.RadiusSM,
			CardTrack:     true,
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

	body := ui.Column(c.Key("app-store-body")).FillWidth().MinWidth(0).Gap(theme.Space20).
		OnUnmount(ctrl.closeStoreReadme)
	bodyW := body.Bounds().W
	if bodyW <= 0 {
		vpW, _ := c.Size()
		if vpW > 0 {
			bodyW = vpW - theme.SidebarMinWidth - 2*theme.Space24
			c.Invalidate()
		}
	}

	body.Children(func() {
		if storeErr != "" {
			ui.Row(c.Key("app-store-error")).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Gap(theme.Space8).Radius(theme.RadiusMD).
				Background(pal.DangerSurface).Border(1, pal.Danger).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, storeErr).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Danger).Grow(1).MinWidth(0)
				dismissBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
					ID:      "app-store-error-dismiss",
					Label:   theme.Tr("关闭提示", "Dismiss"),
					Variant: bui.IconPlain,
					Danger:  true,
				})
				if dismissBtn.Clicked() {
					ctrl.sup.ClearStoreMessage()
				}
			})
		} else if storeNotice != "" {
			ui.Row(c.Key("app-store-notice")).FillWidth().MinWidth(0).
				Padding(theme.Space10, theme.Space12).Gap(theme.Space8).Radius(theme.RadiusMD).
				Background(pal.Selected).Border(1, pal.Accent).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, storeNotice).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).Grow(1).MinWidth(0)
				dismissBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
					ID:      "app-store-notice-dismiss",
					Label:   theme.Tr("关闭提示", "Dismiss"),
					Variant: bui.IconPlain,
				})
				if dismissBtn.Clicked() {
					ctrl.sup.ClearStoreMessage()
				}
			})
		}

		renderManualInstallCard(c, ctrl, installingID)

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

		ui.Grid(c.Key("app-store-list")).FillWidth().MinWidth(0).
			Columns(storeColumnCount(bodyW)).Gap(theme.Space16).Children(func() {
			for _, entry := range entries {
				app, isInstalled := installedMap[entry.ID]
				renderStoreAppCard(c, ctrl, entry, locale, app, isInstalled, installingID, onSelectTab)
			}
		})
	})
	ctrl.renderStoreReadme(c)
}

func storeColumnCount(bodyW float32) int {
	if bodyW <= 0 {
		return 2
	}
	cols := int((bodyW + theme.Space16) / (theme.StoreMinColWidth + theme.Space16))
	return min(max(cols, 1), 4)
}

func renderManualInstallCard(c *ui.Context, ctrl *Controller, installingID string) {
	pal := theme.Ocean()
	card := ui.Column(c.Key("app-store-manual-card")).FillWidth().MinWidth(0).
		Padding(theme.Space16, theme.Space20, theme.Space16, theme.Space20).
		Gap(theme.Space12).
		Radius(theme.RadiusXL).
		Background(pal.Card).
		Border(1, pal.Hairline)

	card.Children(func() {
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
				Font("monospace").
				FontSize(theme.FontBody).
				Height(theme.HeightControl).
				PaddingX(theme.Space10).
				Radius(theme.RadiusMD).
				Background(pal.Shell).
				Border(1, pal.Hairline).
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
			btn := bui.ActionButton(c, "app-store-manual-install", btnLbl, nil, bui.ButtonAccentOutline, disabled)
			if (btn.Clicked() || submitted) && !disabled {
				source := spec
				ctrl.sup.InstallFromSourceAsync(source, func(pluginID string) {
					ctrl.clearManualSpec()
					ctrl.clearPluginState(pluginID)
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
	installedApp plugins.InstalledApp,
	isInstalled bool,
	installingID string,
	onSelectTab func(nextTab string),
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

	card := ui.Column(c.Key("app-store-card-"+entry.ID)).
		FillWidth().
		MinWidth(0).
		Padding(theme.CardPadY, theme.CardPadX, theme.CardPadY, theme.CardPadX).
		Gap(theme.Space12).
		Radius(theme.RadiusXL)
	hovered := card.Hovered()
	bdCol := pal.Hairline
	if hovered {
		bdCol = pal.BtnBorder
	}
	bui.AnimateSurfaceColors(card, pal.Card, bdCol, true)
	if hovered {
		bui.Elevate(card, theme.ElevationRaised)
	}

	name := entry.Name.Resolve(locale)
	card.Cursor(ui.CursorPointer)
	openReadme := card.Clicked()

	card.Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space4).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Height(theme.StoreIconBoxSize).
				Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(theme.StoreIconBoxSize, theme.StoreIconBoxSize).Shrink(0).Center().
					Radius(theme.RadiusLG).Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
					DrawDuoSpec(c, entry.Icon, theme.StoreIconGlyphSize, pal.DuoIconColors(pal.Text))
				})

				title := ui.Text(c.Key("app-store-readme-"+entry.ID), name).
					FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).FontWeight(theme.WeightTitle).
					TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				title.Role(ui.RoleButton).Focusable().Cursor(ui.CursorPointer)
				bui.Tooltip(c, title, theme.Tr("查看详情", "View Details"))
				if hovered {
					title.TextColor(pal.Accent)
				}
				openReadme = title.Clicked() || openReadme

				ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
					hasUpdate := isInstalled && entry.DownloadURL != "" && strings.TrimSpace(entry.Version) != "" &&
						semver.Compare("v"+strings.TrimPrefix(entry.Version, "v"), "v"+installedApp.Manifest.Version) > 0
					renderStoreCardActions(c, ctrl, entry, isInstalled, hasUpdate, installingID, onSelectTab)
				})
			})

			ui.Row(c).FillWidth().MinWidth(0).Height(theme.HeightMicro).
				Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
				renderVerifiedBadge(c, entry.Verified)
				if ver := strings.TrimSpace(entry.Version); ver != "" {
					ui.Text(c, "v"+strings.TrimPrefix(ver, "v")).
						Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).
						MaxWidthPercent(35).MinWidth(0).SingleLine().Ellipsis("…")
				}
				if author := strings.TrimSpace(entry.Author); author != "" {
					ui.Text(c, author).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				}
			})
		})

		bui.FadedTextPreview(c, strings.TrimSpace(entry.Summary.Resolve(locale)), 3,
			theme.FontSmall, theme.LineBody, pal.TextSecondary, pal.Card)

		ui.Row(c).FillWidth().MinWidth(0).Height(theme.Space12+theme.HeightMicro+1).
			Padding(theme.Space12, 0, 0, 0).
			BorderWidth(1, 0, 0, 0).
			BorderColor(pal.Hairline).
			Gap(theme.Space10).
			AlignItems(ui.Center).
			Justify(ui.SpaceBetween).
			Children(func() {
				ui.Text(c, theme.Tr(dlStr+" 次下载", dlStr+" downloads")).
					Font("monospace").
					FontSize(theme.FontMeta).
					FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightMono).
					TextColor(pal.Text).
					SingleLine().
					Shrink(0)

				if len(entry.Permissions) > 0 {
					renderPermissionSummaryPill(c, entry.Permissions, locale)
				}
			})
	})

	if openReadme {
		ctrl.openStoreReadme(c, entry, installedApp)
	}
}

func renderStoreCardActions(
	c *ui.Context,
	ctrl *Controller,
	entry plugins.StoreAppEntry,
	isInstalled bool,
	hasUpdate bool,
	installingID string,
	onSelectTab func(nextTab string),
) {
	if isInstalled {
		if hasUpdate {
			isUpdating := installingID == entry.ID
			upLbl := theme.Tr("更新至 ", "Update to ") + entry.Version
			if isUpdating {
				upLbl = theme.Tr("更新中…", "Updating…")
			}
			upBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
				ID:       "app-store-update-" + entry.ID,
				Label:    upLbl,
				Style:    bui.ButtonPrimary,
				Disabled: installingID != "",
				Height:   theme.HeightCompact,
				PaddingX: theme.Space12,
			})
			if upBtn.Clicked() {
				appEntry := entry
				ctrl.sup.InstallEntryAsync(appEntry, ctrl.clearPluginState)
			}
		}
		openLabel := theme.Tr("打开", "Open")
		if entry.ID == "block-sync" {
			openLabel = theme.Tr("配置同步", "Configure Sync")
		}
		openBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID:       "app-store-open-" + entry.ID,
			Label:    openLabel,
			Style:    bui.ButtonAccentOutline,
			Height:   theme.HeightCompact,
			PaddingX: theme.Space12,
		})
		if openBtn.Clicked() && onSelectTab != nil {
			onSelectTab(modals.TabPluginPrefix + entry.ID)
		}
		uninstBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID:       "app-store-uninstall-" + entry.ID,
			Label:    theme.Tr("卸载", "Uninstall"),
			Style:    bui.ButtonSecondary,
			Height:   theme.HeightCompact,
			PaddingX: theme.Space10,
		})
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
	instBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
		ID:       "app-store-install-" + entry.ID,
		Label:    lbl,
		Style:    bui.ButtonAccentOutline,
		Disabled: installingID != "",
		Height:   theme.HeightCompact,
		PaddingX: theme.Space12,
	})
	if instBtn.Clicked() {
		appEntry := entry
		ctrl.sup.InstallEntryAsync(appEntry, nil)
	}
}

func renderPermissionSummaryPill(c *ui.Context, perms []plugins.Permission, locale string) {
	pal := theme.Ocean()
	allLabels := make([]string, 0, len(perms))
	for _, p := range perms {
		allLabels = append(allLabels, plugins.PermissionLabel(p, locale))
	}
	limit := theme.StorePermissionSummary
	display := strings.Join(allLabels, " · ")
	hasExtra := false
	if len(allLabels) > limit {
		display = fmt.Sprintf("%s +%d", strings.Join(allLabels[:limit], " · "), len(allLabels)-limit)
		hasExtra = true
	}

	pill := ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).MinWidth(0).Shrink(1).
		Radius(theme.RadiusSM).Background(pal.Shell).Border(1, pal.Hairline).AlignItems(ui.Center)
	if hasExtra {
		bui.Tooltip(c, pill, theme.Tr("所需权限：", "Permissions: ")+strings.Join(allLabels, " · "))
	}
	pill.Children(func() {
		ui.Text(c, display).
			FontSize(theme.FontMeta).
			FixedLineHeight(theme.LineMeta).
			TextColor(pal.TextSecondary).
			SingleLine().
			Ellipsis("…")
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
		fg = pal.Text
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
