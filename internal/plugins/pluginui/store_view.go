package pluginui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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

	body := ui.Column(c.Key("app-store-body")).FillWidth().MinWidth(0).Gap(theme.Space20)
	bodyW := body.Bounds().W
	if bodyW <= 0 {
		vpW, _ := c.Size()
		if vpW > 0 {
			bodyW = vpW - theme.SidebarMinWidth - 2*theme.Space24
		}
	}

	body.Children(func() {
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

		colCount := storeColumnCount(bodyW)
		renderStoreMasonry(c, ctrl, entries, installedMap, locale, installingID, onSelectTab, colCount)
	})
}

func storeColumnCount(bodyW float32) int {
	switch {
	case bodyW <= 0:
		return 2
	case bodyW >= theme.StoreThreeColMinWidth:
		return 3
	case bodyW >= theme.StoreTwoColMinWidth:
		return 2
	default:
		return 1
	}
}

// renderStoreMasonry distributes cards into shortest-first vertical columns (Xiaohongshu feed style)
// so each card wraps its natural content height without stretched empty gaps or orphan row spacers.
func renderStoreMasonry(
	c *ui.Context,
	ctrl *Controller,
	entries []plugins.StoreAppEntry,
	installedMap map[string]plugins.InstalledApp,
	locale string,
	installingID string,
	onSelectTab func(nextTab string),
	colCount int,
) {
	if colCount <= 1 {
		ui.Column(c.Key("app-store-list")).FillWidth().MinWidth(0).Gap(theme.Space16).Children(func() {
			for _, item := range entries {
				entry := item
				_, isInstalled := installedMap[entry.ID]
				renderStoreAppCard(c, ctrl, entry, locale, isInstalled, installingID, onSelectTab)
			}
		})
		return
	}

	buckets := distributeMasonryEntries(entries, locale, colCount)
	ui.Row(c.Key("app-store-list")).FillWidth().MinWidth(0).Gap(theme.Space16).AlignItems(ui.Start).Children(func() {
		for colIdx := 0; colIdx < colCount; colIdx++ {
			colItems := buckets[colIdx]
			ui.Column(c.Key(fmt.Sprintf("app-store-col-%d", colIdx))).
				Grow(1).Basis(0).MinWidth(0).Gap(theme.Space16).Children(func() {
				for _, item := range colItems {
					entry := item
					_, isInstalled := installedMap[entry.ID]
					renderStoreAppCard(c, ctrl, entry, locale, isInstalled, installingID, onSelectTab)
				}
			})
		}
	})
}

func distributeMasonryEntries(entries []plugins.StoreAppEntry, locale string, colCount int) [][]plugins.StoreAppEntry {
	buckets := make([][]plugins.StoreAppEntry, colCount)
	heights := make([]int, colCount)
	for _, entry := range entries {
		bestCol := 0
		for i := 1; i < colCount; i++ {
			if heights[i] < heights[bestCol] {
				bestCol = i
			}
		}
		buckets[bestCol] = append(buckets[bestCol], entry)
		heights[bestCol] += estimateStoreCardWeight(entry, locale)
	}
	return buckets
}

func estimateStoreCardWeight(entry plugins.StoreAppEntry, locale string) int {
	summaryRunes := utf8.RuneCountInString(strings.TrimSpace(entry.Summary.Resolve(locale)))
	lines := max((summaryRunes+21)/22, 1)
	permWeight := 0
	if len(entry.Permissions) > 0 {
		permWeight = 28
	}
	return 210 + lines*20 + permWeight
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
) {
	pal := theme.Ocean()
	tone := pal.StoreCard(entry.ID)
	downloads := entry.Downloads
	if downloads < 0 {
		downloads = 0
	}
	if store := ctrl.sup.Store(); store != nil {
		downloads = store.EffectiveDownloads(entry)
	}
	dlStr := formatDownloadCount(downloads)

	card := ui.Column(c.Key("app-store-card-" + entry.ID)).
		FillWidth().
		MinWidth(0).
		Radius(theme.RadiusXL).
		Clip()
	hovered := card.Hovered()
	bdCol := pal.Hairline
	if hovered {
		bdCol = pal.BtnBorder
	}
	bui.AnimateSurfaceColors(card, pal.Card, bdCol, true)
	if hovered {
		bui.Elevate(card, theme.ElevationRaised)
	}

	card.Children(func() {
		// Top Xiaohongshu-style visual cover stage with floating version & badge tags.
		ui.Box(c).FillWidth().Height(theme.StoreCoverHeight).
			Background(tone.CoverBG).
			BorderWidth(0, 0, 1, 0).
			BorderColor(pal.Hairline).
			Center().
			Children(func() {
				if ver := strings.TrimSpace(entry.Version); ver != "" {
					ui.Row(c).Absolute().Top(theme.Space10).Left(theme.Space12).
						Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusPill).
						Background(tone.EmblemBG).Border(1, tone.EmblemBorder).AlignItems(ui.Center).
						Children(func() {
							ui.Text(c, "v"+strings.TrimPrefix(ver, "v")).
								Font("monospace").
								FontSize(theme.FontMeta).
								FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightMono).
								TextColor(pal.TextSecondary).
								SingleLine()
						})
				}

				ui.Box(c).Absolute().Top(theme.Space10).Right(theme.Space12).Children(func() {
					renderVerifiedBadge(c, entry.Verified)
				})

				emblem := ui.Box(c).Size(theme.StoreCoverEmblemBox, theme.StoreCoverEmblemBox).
					Radius(theme.RadiusXL).
					Background(tone.EmblemBG).
					Border(1, tone.EmblemBorder).
					Center()
				bui.Elevate(emblem, theme.ElevationRaised)
				emblem.Children(func() {
					DrawDuoSpec(c, entry.Icon, theme.StoreCoverEmblemGlyph, theme.IconColors{
						Line:   tone.IconLine,
						Fill:   tone.IconFill,
						Accent: tone.IconAccent,
					})
				})
			})

		// Card content body: title, full natural-height summary, compact permission strip, and author/CTA footer.
		ui.Column(c).FillWidth().MinWidth(0).
			Padding(theme.Space16, theme.Space16, theme.Space12, theme.Space16).
			Gap(theme.Space10).
			Children(func() {
				ui.Text(c, entry.Name.Resolve(locale)).
					FontSize(theme.FontTitle).
					FixedLineHeight(theme.LineTitle).
					FontWeight(theme.WeightTitle).
					TextColor(pal.Text).
					SingleLine().
					Ellipsis("…")

				if summary := strings.TrimSpace(entry.Summary.Resolve(locale)); summary != "" {
					ui.Text(c, summary).
						FontSize(theme.FontSmall).
						FixedLineHeight(theme.LineBody).
						TextColor(pal.TextSecondary)
				}

				if len(entry.Permissions) > 0 {
					renderPermissionSummaryRow(c, entry.Permissions, locale)
				}

				// Xiaohongshu-style creator + metric + compact action footer (single-line, never wraps or pushes card uneven).
				ui.Row(c).FillWidth().MinWidth(0).
					Padding(theme.Space10, 0, 0, 0).
					BorderWidth(1, 0, 0, 0).
					BorderColor(pal.Hairline).
					Gap(theme.Space8).
					AlignItems(ui.Center).
					Justify(ui.SpaceBetween).
					Children(func() {
						author := strings.TrimSpace(entry.Author)
						if author == "" {
							author = "Community"
						}
						ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(theme.StoreAuthorDotSize, theme.StoreAuthorDotSize).Shrink(0).
								Radius(theme.RadiusPill).Background(tone.AuthorDotBG).Center().Children(func() {
								ui.Text(c, authorInitial(author)).
									FontSize(theme.FontMeta).
									FontWeight(theme.WeightTitle).
									TextColor(tone.AuthorDotFG).
									SingleLine()
							})
							ui.Text(c, author).
								FontSize(theme.FontMeta).
								FixedLineHeight(theme.LineMeta).
								TextColor(pal.TextSecondary).
								Grow(1).
								MinWidth(0).
								SingleLine().
								Ellipsis("…")
						})

						ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0).Children(func() {
							dlRow := ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0)
							bui.Tooltip(c, dlRow, theme.Tr(dlStr+" 次下载", dlStr+" downloads"))
							dlRow.Children(func() {
								icons.Draw(c, icons.Download, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
								ui.Text(c, dlStr).
									Font("monospace").
									FontSize(theme.FontMeta).
									FixedLineHeight(theme.LineMeta).
									TextColor(pal.TextSecondary).
									SingleLine()
							})

							renderStoreCardActions(c, ctrl, entry, isInstalled, installingID, onSelectTab)
						})
					})
			})
	})
}

func renderStoreCardActions(
	c *ui.Context,
	ctrl *Controller,
	entry plugins.StoreAppEntry,
	isInstalled bool,
	installingID string,
	onSelectTab func(nextTab string),
) {
	if isInstalled {
		openBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID:       "app-store-open-" + entry.ID,
			Label:    theme.Tr("打开", "Open"),
			Style:    bui.ButtonSecondary,
			Height:   theme.HeightCompact,
			PaddingX: theme.Space10,
		})
		if openBtn.Clicked() && onSelectTab != nil {
			onSelectTab(modals.TabPluginPrefix + entry.ID)
		}
		uninstBtn := bui.IconButton(c, icons.Trash, bui.IconButtonOptions{
			ID:      "app-store-uninstall-" + entry.ID,
			Label:   theme.Tr("卸载", "Uninstall"),
			Tooltip: theme.Tr("卸载", "Uninstall"),
			Variant: bui.IconPlain,
			Size:    theme.HeightCompact,
			Danger:  true,
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
		Style:    bui.ButtonPrimary,
		Disabled: installingID != "",
		Height:   theme.HeightCompact,
		PaddingX: theme.Space10,
	})
	if instBtn.Clicked() {
		appEntry := entry
		ctrl.sup.InstallEntryAsync(appEntry, func(pluginID string) {
			if onSelectTab != nil {
				onSelectTab(modals.TabPluginPrefix + pluginID)
			}
		})
	}
}

func renderPermissionSummaryRow(c *ui.Context, perms []plugins.Permission, locale string) {
	limit := theme.StorePermissionSummary
	shown := perms
	var hidden []string
	if len(perms) > limit {
		shown = perms[:limit]
		for _, p := range perms[limit:] {
			hidden = append(hidden, plugins.PermissionLabel(p, locale))
		}
	}

	ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
		for _, perm := range shown {
			renderPermissionCapsule(c, plugins.PermissionLabel(perm, locale))
		}
		if len(hidden) > 0 {
			morePill := renderPermissionCapsule(c, fmt.Sprintf("+%d", len(hidden)))
			allLabels := make([]string, 0, len(perms))
			for _, p := range perms {
				allLabels = append(allLabels, plugins.PermissionLabel(p, locale))
			}
			bui.Tooltip(c, morePill, theme.Tr("所需权限：", "Permissions: ")+strings.Join(allLabels, " · "))
		}
	})
}

func authorInitial(author string) string {
	for _, r := range strings.TrimSpace(author) {
		if unicode.IsSpace(r) {
			continue
		}
		return strings.ToUpper(string(r))
	}
	return "B"
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

func renderPermissionCapsule(c *ui.Context, label string) ui.Element {
	pal := theme.Ocean()
	pill := ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusSM).
		Background(pal.CodeBG).Border(1, pal.Hairline).AlignItems(ui.Center)
	pill.Children(func() {
		ui.Text(c, label).
			FontSize(theme.FontMeta).
			FixedLineHeight(theme.LineMeta).
			TextColor(pal.TextSecondary).
			SingleLine()
	})
	return pill
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
