package pluginui

import (
	"strings"

	"block/internal/icons"
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
			{ID: "app-store-filter-available", Label: theme.Tr("未安装", "Not Installed")},
		}, bui.SegmentedControlOptions{
			TrackHeight:   theme.SegmentedControlHeight,
			TrackPadding:  theme.Space2,
			SegmentHeight: theme.SegmentedInnerHeight,
			SegmentRadius: theme.RadiusSM,
			CardTrack:     true,
		}) {
			ctrl.setStoreFilter(filterIdx)
		}

		manualOpen := ctrl.manualOpenValue()
		manualBtn := bui.IconButton(c, icons.Plus, bui.IconButtonOptions{
			ID:      "app-store-manual-toggle",
			Label:   theme.Tr("手动安装", "Manual Install"),
			Tooltip: theme.Tr("手动安装", "Manual Install"),
			Active:  manualOpen,
		})
		if manualBtn.Clicked() {
			ctrl.toggleManualOpen()
		}

		refTip := theme.Tr("刷新", "Refresh")
		if refreshing {
			refTip = theme.Tr("刷新中…", "Refreshing…")
		}
		refBtn := bui.IconButton(c, icons.Refresh, bui.IconButtonOptions{
			ID:      "app-store-refresh",
			Label:   refTip,
			Tooltip: refTip,
			Loading: refreshing,
		})
		if refBtn.Clicked() && !refreshing {
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

	body := ui.Column(c.Key("app-store-body")).FillWidth().MinWidth(0).Gap(theme.Space20)
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
				Background(pal.DangerSurface).Border(1, pal.Danger).AlignItems(ui.Center).Justify(ui.SpaceBetween).
				Transition(bui.CascadeRowTransition(0)).Children(func() {
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
				Background(pal.Selected).Border(1, ui.Transparent).AlignItems(ui.Center).Justify(ui.SpaceBetween).
				Transition(bui.CascadeRowTransition(0)).Children(func() {
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

		if ctrl.manualOpenValue() {
			renderManualInstallRow(c, ctrl, installingID)
		}

		if len(entries) == 0 {
			c.Key("app-store-empty-card")
			bui.Card(c, func() {
				msg := theme.Tr("暂无可用应用", "No apps available")
				if filter == 1 {
					msg = theme.Tr("暂无已安装应用", "No installed apps")
				} else if filter == 2 && !refreshing {
					msg = theme.Tr("已安装全部应用", "All apps are installed")
				} else if refreshing {
					msg = theme.Tr("正在加载应用目录…", "Loading catalog…")
				}
				ui.Text(c, msg).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			})
			return
		}

		ui.Grid(c.Key("app-store-list")).FillWidth().MinWidth(0).
			Columns(storeColumnCount(bodyW)).Gap(theme.Space4).Children(func() {
			for _, entry := range entries {
				app, isInstalled := installedMap[entry.ID]
				renderStoreAppRow(c, ctrl, entry, locale, app, isInstalled, isInstalled && filter == 0, installingID, onSelectTab)
			}
		})
	})
}

func storeColumnCount(bodyW float32) int {
	if bodyW <= 0 {
		return 2
	}
	cols := int((bodyW + theme.Space4) / (theme.StoreMinColWidth + theme.Space4))
	return min(max(cols, 1), 2)
}

func renderManualInstallRow(c *ui.Context, ctrl *Controller, installingID string) {
	pal := theme.Ocean()
	spec := ctrl.manualSpecValue()
	ui.Row(c.Key("app-store-manual-row")).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).
		Transition(bui.CascadeRowTransition(0)).Children(func() {
		in := ui.TextInput(c.Key("app-store-manual-input"), &spec).
			Placeholder(theme.Tr("输入 owner/repo、压缩包 URL 或本地目录路径", "Enter owner/repo, archive URL, or local directory path")).
			Label(theme.Tr("手动安装插件源", "Plugin Source")).
			FontSize(theme.FontBody).
			FixedLineHeight(theme.LineBody).
			Height(theme.HeightControl).
			Padding(0, theme.Space12).
			Radius(theme.RadiusMD).
			TextColor(pal.Text).
			PlaceholderColor(pal.Placeholder).
			Grow(1).
			MinWidth(0)
		if in.Focused() {
			in.Background(pal.Card).Border(1, pal.Accent)
		} else if in.Hovered() {
			in.Background(pal.Shell).Border(1, pal.BtnBorder.Mix(pal.Text, 0.20))
		} else {
			in.Background(pal.Shell).Border(1, pal.BtnBorder)
		}
		bui.Probe(in, "app-store-manual-input")
		if in.Changed() {
			ctrl.setManualSpec(spec)
		}
		submitted := in.Submitted()

		if ctrl.sup != nil && ctrl.sup.CanPickDirectory() {
			folderIc := icons.Folder
			pickBtn := bui.ActionButton(c, "app-store-manual-browse", theme.Tr("本地目录", "Browse"), &folderIc, bui.ButtonGhost, installingID != "")
			if pickBtn.Clicked() {
				ctrl.sup.PickDirectory(theme.Tr("选择本地插件目录", "Choose Local Plugin Directory"), func(chosen string) {
					ctrl.setManualSpec(chosen)
				})
			}
		}
		manualBusy := installingID == "__manual__"
		disabled := (installingID != "" && !manualBusy) || strings.TrimSpace(spec) == ""
		btnLbl := theme.Tr("安装", "Install")
		if manualBusy {
			btnLbl = theme.Tr("安装中…", "Installing…")
		}
		installIc := icons.Download
		btn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID:       "app-store-manual-install",
			Label:    btnLbl,
			Leading:  &installIc,
			Style:    bui.ButtonAccentOutline,
			Disabled: disabled,
			Loading:  manualBusy,
		})
		if (btn.Clicked() || submitted) && !disabled && !manualBusy {
			source := spec
			ctrl.sup.InstallFromSourceAsync(source, func(pluginID string) {
				ctrl.clearManualSpec()
				ctrl.clearPluginState(pluginID)
			})
		}
	})
}

// renderStoreAppRow draws one quiet store entry: icon, name and a one-line
// summary. Actions stay hidden until the row is hovered (or busy installing);
// an installed app's row opens its settings page, where enable/uninstall live.
func renderStoreAppRow(
	c *ui.Context,
	ctrl *Controller,
	entry plugins.StoreAppEntry,
	locale string,
	installedApp plugins.InstalledApp,
	isInstalled bool,
	showInstalledMark bool,
	installingID string,
	onSelectTab func(nextTab string),
) {
	pal := theme.Ocean()
	name := entry.Name.Resolve(locale)
	rowH := 2*theme.Space12 + theme.LineTitle + theme.Space2 + theme.LineBody

	row := ui.Row(c.Key("app-store-card-"+entry.ID)).
		FillWidth().MinWidth(0).Height(rowH).
		Padding(theme.Space12, theme.Space12, theme.Space12, theme.Space12).
		Gap(theme.Space12).AlignItems(ui.Center).
		Radius(theme.RadiusLG).Cursor(ui.CursorPointer)
	row.Role(ui.RoleButton).Focusable()
	bui.Probe(row, "app-store-card-"+entry.ID)
	hovered := row.Hovered()
	pressed := row.Pressed()
	if pressed {
		row.Background(pal.Selected)
	} else if hovered {
		row.Background(pal.Hover)
	}
	bui.TactileCard(row, hovered, pressed, false, false)
	activate := row.Clicked()

	busy := installingID == entry.ID
	hasUpdate := isInstalled && entry.DownloadURL != "" && strings.TrimSpace(entry.Version) != "" &&
		semver.Compare("v"+strings.TrimPrefix(entry.Version, "v"), "v"+installedApp.Manifest.Version) > 0

	row.Children(func() {
		ui.Box(c).Size(theme.StoreIconBoxSize, theme.StoreIconBoxSize).Shrink(0).Center().
			Radius(theme.RadiusLG).Background(pal.Shell).Children(func() {
			DrawDuoSpec(c, entry.Icon, theme.StoreIconGlyphSize, pal.DuoIconColors(pal.Text))
		})

		ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).
					FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).FontWeight(theme.WeightTitle).
					TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
				if storeAppIsOfficial(entry, installedApp, isInstalled) {
					bui.OfficialBadge(c, true)
				}
				if storeAppIsBeta(entry, installedApp, isInstalled) {
					bui.BetaBadge(c, true)
				}
				if showInstalledMark {
					// A quiet check after the name is the only resting cue that an app is installed.
					mark := ui.Box(c.Key("app-store-installed-" + entry.ID)).Shrink(0).Center().Children(func() {
						icons.Draw(c, icons.Check, theme.IconInline, pal.DuoIconColors(pal.TextSecondary))
					})
					bui.Tooltip(c, mark, theme.Tr("已安装", "Installed"))
				}
			})
			if summary := strings.TrimSpace(entry.Summary.Resolve(locale)); summary != "" {
				ui.Text(c, summary).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
			}
		})

		if !hovered && !busy {
			return
		}
		ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0).Children(func() {
			switch {
			case !isInstalled || hasUpdate:
				lbl := theme.Tr("安装", "Install")
				if hasUpdate {
					lbl = theme.Tr("更新", "Update")
				}
				if busy {
					lbl = theme.Tr("安装中…", "Installing…")
					if hasUpdate {
						lbl = theme.Tr("更新中…", "Updating…")
					}
				}
				actIc := icons.Download
				if hasUpdate {
					actIc = icons.Refresh
				}
				btn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
					ID:       "app-store-install-" + entry.ID,
					Label:    lbl,
					Leading:  &actIc,
					Style:    bui.ButtonAccentOutline,
					Disabled: installingID != "" && !busy,
					Loading:  busy,
					Height:   theme.HeightCompact,
					PaddingX: theme.Space12,
				})
				if btn.Clicked() && !busy {
					ctrl.openStoreReadme(entry, installedApp)
				}
			}
			if isInstalled {
				icons.Draw(c, icons.ChevronRight, theme.IconAction, pal.DuoIconColors(pal.TextSecondary))
			}
		})
	})

	if activate {
		ctrl.openStoreReadme(entry, installedApp)
	}
}

func storeAppIsBeta(entry plugins.StoreAppEntry, installed plugins.InstalledApp, isInstalled bool) bool {
	if isInstalled {
		return installed.Manifest.Beta
	}
	return entry.Beta
}

func storeAppIsOfficial(entry plugins.StoreAppEntry, installed plugins.InstalledApp, isInstalled bool) bool {
	if !entry.Verified {
		return false
	}
	if !isInstalled {
		return true
	}
	wantRepo := strings.TrimSpace(entry.Repo)
	gotRepo := strings.TrimSpace(installed.Manifest.Repo)
	return wantRepo != "" && strings.EqualFold(gotRepo, wantRepo)
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
			Beta:        inst.Manifest.Beta,
			Author:      inst.Manifest.Author,
			Repo:        inst.Manifest.Repo,
			Summary:     inst.Manifest.Summary,
			Permissions: inst.Manifest.Permissions,
			Surfaces:    inst.Manifest.Surfaces,
			Icon:        inst.Manifest.Icon,
		})
	}
	if filter != 1 && filter != 2 {
		return all
	}
	wantInstalled := filter == 1
	filtered := make([]plugins.StoreAppEntry, 0, len(all))
	for _, entry := range all {
		if _, ok := installedMap[entry.ID]; ok == wantInstalled {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
