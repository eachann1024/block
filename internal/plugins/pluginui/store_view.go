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
	storedata "block/store"
	"github.com/egoist/mygo/ui"
)

const (
	storeSourceOfficial   = "official"
	storeSourceReviewed   = "reviewed"
	storeSourceUnreviewed = "unreviewed"
	storeSortName         = "name"
)

// renderStoreHeaderExtras draws an Update All button (when updates exist) and
// one search field with an inline filter button; source and sort live together
// in the button's menu so the header stays light.
func renderStoreHeaderExtras(c *ui.Context, ctrl *Controller) {
	pal := theme.Ocean()
	refreshing, installingID, _, _ := ctrl.sup.StoreStatus()
	batchUpdating := ctrl.sup.IsBatchUpdating()
	updatable := updatableStoreEntries(ctrl)
	if !refreshing && (batchUpdating || len(updatable) > 0) {
		glyph := icons.Refresh
		label := theme.Tr(fmt.Sprintf("一键更新 (%d)", len(updatable)), fmt.Sprintf("Update All (%d)", len(updatable)))
		if batchUpdating {
			label = theme.Tr("更新中…", "Updating…")
		}
		allBusy := len(updatable) > 0
		for _, entry := range updatable {
			if !ctrl.sup.IsPluginBusyAnyNotebook(entry.ID) {
				allBusy = false
				break
			}
		}
		disabled := (installingID != "" && !batchUpdating) || (!batchUpdating && allBusy)
		upAllBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID:       "app-store-update-all",
			Label:    label,
			Leading:  &glyph,
			Style:    bui.ButtonAccentOutline,
			Disabled: disabled,
			Loading:  batchUpdating,
			Height:   theme.HeightControl,
			PaddingX: theme.Space12,
		})
		if upAllBtn.Clicked() && !batchUpdating && !disabled && len(updatable) > 0 {
			ctrl.closeStoreMenus()
			ctrl.RequestUpdateAll(updatable, ctrl.clearPluginState)
		}
	}
	query := ctrl.storeQuery
	border := ui.Transparent
	if ctrl.storeSearchFocused {
		border = pal.Accent
	}
	field := ui.Row(c.Key("app-store-search-field")).
		Width(theme.StoreSearchFieldWidth).MaxWidthPercent(100).MinWidth(0).Height(theme.HeightControl).
		Padding(0, theme.Space4, 0, 0).AlignItems(ui.Center).Gap(theme.Space4).
		Radius(theme.RadiusMD).Background(pal.Shell).Border(1, border)
	field.Children(func() {
		input := ui.TextInput(c.Key("app-store-search"), &query).
			Grow(1).MinWidth(0).Height(theme.HeightControl).
			Padding(0, 0, 0, theme.Space12).FontSize(theme.FontBody).
			FixedLineHeight(theme.LineBody).TextColor(pal.Text).PlaceholderColor(pal.Placeholder).
			Background(ui.Transparent).Border(0, ui.Transparent).
			Label(theme.Tr("搜索插件或作者", "Search plugins or authors")).
			Placeholder(theme.Tr("搜索插件或作者", "Search plugins or authors"))
		ctrl.storeSearchFocused = input.Focused()
		bui.Probe(input, "app-store-search")
		if input.Changed() {
			ctrl.storeQuery = query
		}

		filtered := ctrl.storeSource != "" || ctrl.storeSort != ""
		filterBtn := bui.IconButton(c, icons.SortFilter, bui.IconButtonOptions{
			ID:          "app-store-filter",
			Size:        theme.StoreFilterButtonSize,
			IconSize:    theme.IconRow,
			Radius:      theme.RadiusSM,
			Tooltip:     theme.Tr("来源与排序", "Source & Sort"),
			Active:      ctrl.storeFilterOpen,
			ActiveStyle: bui.ActiveAccent,
			AlwaysDuo:   filtered,
		})
		if b := filterBtn.Bounds(); b.W > 0 {
			ctrl.storeFilterAnchor = b
		}
		if filterBtn.Clicked() {
			ctrl.storeFilterOpen = !ctrl.storeFilterOpen
			ctrl.storeFilterCursor = -1
		}
	})
	renderStoreFilterMenu(c, ctrl)
}

func renderStoreFilterMenu(c *ui.Context, ctrl *Controller) {
	if !ctrl.storeFilterOpen || ctrl.storeFilterAnchor.W <= 0 {
		return
	}
	pick := func(target *string, value string) func() {
		return func() {
			*target = value
			ctrl.storeFilterOpen = false
		}
	}
	source, sortBy := ctrl.storeSource, ctrl.storeSort
	items := []bui.MenuItemSpec{
		{ID: "app-store-source-all", Label: theme.Tr("全部来源", "All sources"), Checked: source == "", OnClick: pick(&ctrl.storeSource, "")},
		{ID: "app-store-source-official", Label: theme.Tr("Block 官方", "Official by Block"), Checked: source == storeSourceOfficial, OnClick: pick(&ctrl.storeSource, storeSourceOfficial)},
		{ID: "app-store-source-reviewed", Label: theme.Tr("已审核", "Reviewed"), Checked: source == storeSourceReviewed, OnClick: pick(&ctrl.storeSource, storeSourceReviewed)},
		{ID: "app-store-source-unreviewed", Label: theme.Tr("未审核", "Unreviewed"), Checked: source == storeSourceUnreviewed, OnClick: pick(&ctrl.storeSource, storeSourceUnreviewed)},
		{Separator: true},
		{ID: "app-store-sort-downloads", Label: theme.Tr("最多下载", "Most downloaded"), Checked: sortBy == "", OnClick: pick(&ctrl.storeSort, "")},
		{ID: "app-store-sort-name", Label: theme.Tr("按名称", "By name"), Checked: sortBy == storeSortName, OnClick: pick(&ctrl.storeSort, storeSortName)},
	}
	vpW, vpH := c.Size()
	menuW := min(theme.StoreFilterMenuWidth, max(vpW-theme.Space16, 1))
	rows := float32(len(items) - 1)
	estH := theme.MenuPadding*2 + rows*theme.MenuItemHeight + rows*theme.MenuGap + 7
	spec := bui.PlaceBelow(ctrl.storeFilterAnchor).
		WithAlignX(bui.AlignX{Kind: bui.AlignRight}).
		WithGap(theme.Space6)
	maxH := bui.MaxHeightForSpec(spec, vpW, vpH, 320)
	popH := min(estH, maxH)
	ox, oy := bui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

	ui.Overlay(c, func() {
		scrim := ui.Box(c.Key("app-store-filter-scrim")).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		c.Key("app-store-filter-menu")
		card := bui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
		bui.NavigateMenu(c, card, items, &ctrl.storeFilterCursor)
		bui.Probe(card, "app-store-filter-menu")
		card.Children(func() {
			for i, it := range items {
				bui.MenuRow(c, it, i == ctrl.storeFilterCursor)
			}
		})
		if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
			ctrl.storeFilterOpen = false
		}
	})
}

type storeCard struct {
	entry       plugins.StoreAppEntry
	installed   plugins.InstalledApp
	downloads   int
	isInstalled bool
	source      string
}

func renderStoreBody(c *ui.Context, ctrl *Controller, onSelectTab func(nextTab string)) {
	pal := theme.Ocean()
	locale := ctrl.sup.Locale()
	refreshing, installingID, storeErr, _ := ctrl.sup.StoreStatus()
	selectedCat := ctrl.storeCategoryValue()

	body := ui.Column(c.Key("app-store-body")).FillWidth().MinWidth(0).Gap(theme.Space24)

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
		}

		if refreshing {
			renderStoreSkeleton(c)
			return
		}

		view := ctrl.storeView(locale)
		if strings.TrimSpace(ctrl.storeQuery) == "" && ctrl.storeSource == "" && selectedCat == "" {
			renderStoreHero(c, ctrl, locale, installingID, onSelectTab)
		}
		renderStoreCategoryTabs(c, ctrl, selectedCat, locale, view)

		list := view.cards
		if selectedCat != "" {
			list = make([]storeCard, 0, view.catCounts[selectedCat])
			for _, card := range view.cards {
				if card.entry.EffectiveCategory() == selectedCat {
					list = append(list, card)
				}
			}
		}

		if len(list) == 0 {
			renderStoreEmptyState(c, ctrl, selectedCat, locale)
			return
		}

		// Rank numbers only make sense while the list is ordered by downloads.
		ranked := ctrl.storeSort != storeSortName && strings.TrimSpace(ctrl.storeQuery) == ""
		renderStoreList(c, ctrl, list, ranked, locale, installingID, onSelectTab)
	})
}

func renderStoreCategoryTabs(
	c *ui.Context,
	ctrl *Controller,
	selected plugins.Category,
	locale string,
	view *storeViewSnapshot,
) {
	pal := theme.Ocean()

	type catTabSpec struct {
		cat   plugins.Category
		key   string
		label string
	}
	countOf := func(cat plugins.Category) int {
		if cat == "" {
			return len(view.cards)
		}
		return view.catCounts[cat]
	}
	tabs := make([]catTabSpec, 0, 1+len(plugins.StoreCategories()))
	tabs = append(tabs, catTabSpec{
		cat:   "",
		key:   "all",
		label: theme.Tr("全部", "All"),
	})
	for _, cat := range plugins.StoreCategories() {
		tabs = append(tabs, catTabSpec{
			cat:   cat,
			key:   string(cat),
			label: plugins.CategoryLabel(cat, locale),
		})
	}

	ui.Column(c.Key("app-store-cat-bar")).FillWidth().MinWidth(0).Gap(theme.Space12).Children(func() {
		ui.Row(c.Key("app-store-cat-tabs")).FillWidth().MinWidth(0).Gap(theme.Space4).Wrap().AlignItems(ui.Center).Children(func() {
			for _, t := range tabs {
				tab := t
				active := selected == tab.cat
				btnID := "app-store-cat-" + tab.key
				btn := ui.ButtonBase(c.Key(btnID)).
					Height(theme.HeightControl).PaddingX(theme.Space12).Gap(theme.Space6).
					AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
					Label(tab.label).Shrink(0)
				hovered := btn.Hovered()
				pressed := btn.Pressed()
				if active {
					btn.Background(pal.Selected).Border(1, pal.SelectedBorder)
				} else if pressed {
					btn.Background(pal.Selected).Border(1, ui.Transparent)
				} else if hovered {
					btn.Background(pal.Hover).Border(1, ui.Transparent)
				} else {
					btn.Border(1, ui.Transparent)
				}
				bui.TactileCard(btn, hovered, pressed, active, false)
				btn.Children(func() {
					lblColor := pal.TextSecondary
					if active || hovered || pressed {
						lblColor = pal.Text
					}
					ui.Text(c, tab.label).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightControl).TextColor(lblColor).SingleLine()
					if n := countOf(tab.cat); n > 0 {
						ui.Text(c, strconv.Itoa(n)).Font("monospace").
							FontSize(theme.FontMeta).FixedLineHeight(theme.LineBody).
							TextColor(pal.Placeholder).SingleLine()
					}
				})
				bui.Probe(btn, btnID)
				if btn.Clicked() {
					ctrl.setStoreCategory(tab.cat)
				}
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
	})
}

func renderManualInstallRow(c *ui.Context, ctrl *Controller, installingID string) {
	pal := theme.Ocean()
	spec := ctrl.manualSpecValue()
	ui.Row(c.Key("app-store-manual-row")).FillWidth().MinWidth(0).Gap(theme.Space10).AlignItems(ui.Center).Wrap().Children(func() {
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
			Basis(theme.StoreSearchWidth).
			MaxWidthPercent(100).
			MinWidth(0)
		if in.Focused() {
			in.Background(pal.Card).Border(1, pal.Accent)
		} else if pal.Dark || theme.IsSkinActive() {
			in.Background(pal.Shell).Border(1, pal.Hairline)
		} else {
			in.Background(pal.Shell).Border(1, ui.Transparent)
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
			ctrl.RequestManualInstall(source, func(pluginID string) {
				ctrl.clearManualSpec()
				ctrl.clearPluginState(pluginID)
			})
		}
	})
}

func storeCardActionButton(
	c *ui.Context,
	id, label string,
	style bui.ActionButtonStyle,
	leadingDot, disabled, busy bool,
) ui.Element {
	return bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
		ID:          id,
		Label:       label,
		Style:       style,
		LeadingDot:  leadingDot,
		CompactText: true,
		Disabled:    disabled,
		Loading:     busy,
		Height:      theme.HeightCompact,
		MinWidth:    theme.StoreCardActionWidth,
		PaddingX:    theme.Space6,
	})
}

func renderStoreCardAction(
	c *ui.Context,
	ctrl *Controller,
	card storeCard,
	busy, hasSettings bool,
	installingID string,
	onSelectTab func(nextTab string),
) (actionHandled, actionHovered bool) {
	pal := theme.Ocean()
	entry := card.entry
	if card.isInstalled && entry.ID != plugins.DiagnosticsPluginID && storeEntryHasUpdate(entry, card.installed) {
		label := theme.Tr("更新", "Update")
		if busy {
			label = theme.Tr("更新中", "Updating")
		}
		pluginBusy := ctrl.sup.IsPluginBusyAnyNotebook(entry.ID)
		disabled := (installingID != "" && !busy) || (pluginBusy && !busy)
		upBtn := storeCardActionButton(c, "app-store-update-"+entry.ID, label, bui.ButtonAccentSoft, true, disabled, busy)
		actionHovered = upBtn.Hovered()
		if upBtn.Clicked() && !busy && !disabled {
			actionHandled = true
			ctrl.closeStoreMenus()
			ctrl.RequestInstallEntry(entry, ctrl.clearPluginState)
		}
		return actionHandled, actionHovered
	}
	if card.isInstalled && hasSettings {
		openBtn := storeCardActionButton(c, "app-store-open-"+entry.ID, theme.Tr("打开", "Open"), bui.ButtonSecondary, false, busy, busy)
		actionHovered = openBtn.Hovered()
		if openBtn.Clicked() && !busy {
			actionHandled = true
			ctrl.closeStoreMenus()
			if onSelectTab != nil {
				onSelectTab(modals.TabPluginPrefix + entry.ID)
			} else {
				ctrl.openStoreReadme(entry, card.installed)
				c.Invalidate()
			}
		}
		return actionHandled, actionHovered
	}
	if card.isInstalled {
		ui.Row(c.Key("app-store-installed-" + entry.ID)).
			MinWidth(theme.StoreCardActionWidth).Height(theme.HeightCompact).
			PaddingX(theme.Space6).Gap(theme.Space4).Radius(theme.RadiusMD).
			Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Children(func() {
			icons.Draw(c, icons.Check, theme.IconMicro, pal.MinimalIconColors(pal.TextSecondary))
			ui.Text(c, theme.Tr("已安装", "Installed")).FontSize(theme.FontSmall).
				FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightControl).
				TextColor(pal.TextSecondary).SingleLine()
		})
		return false, false
	}

	label := theme.Tr("安装", "Install")
	if busy {
		label = theme.Tr("安装中", "Installing")
	}
	disabled := installingID != "" && !busy
	btn := storeCardActionButton(c, "app-store-install-"+entry.ID, label, bui.ButtonAccentOutline, false, disabled, busy)
	actionHovered = btn.Hovered()
	if btn.Clicked() && !busy && !disabled {
		actionHandled = true
		ctrl.closeStoreMenus()
		ctrl.RequestInstallEntry(entry, ctrl.clearPluginState)
	}
	return actionHandled, actionHovered
}

func renderStoreSource(c *ui.Context, author, source string, fadeBG ui.Color) {
	pal := theme.Ocean()
	ui.Row(c).MinWidth(0).Shrink(1).Height(theme.LineSmall).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
		if source == storeSourceOfficial {
			ui.Row(c).MinWidth(0).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
				DrawDuoSpec(c, plugins.AppStoreIconSpec(), theme.IconMicro, pal.DuoIconColors(pal.Accent))
				ui.Text(c, theme.Tr("Block 官方", "Official by Block")).FontSize(theme.FontMeta).
					FixedLineHeight(theme.LineSmall).TextColor(pal.Accent).MinWidth(0).SingleLine().Ellipsis("…")
			})
			return
		}
		if strings.TrimSpace(author) != "" {
			renderStoreAuthor(c, author, fadeBG)
		}
		if source == storeSourceUnreviewed {
			renderStoreUnreviewed(c)
		}
	})
}

func renderStoreUnreviewed(c *ui.Context) {
	color := theme.Ocean().Warning
	ui.Row(c).MinWidth(0).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(theme.IconMicro, theme.IconMicro).Shrink(0).Radius(theme.RadiusPill).
			Border(1, color).Center().Children(func() {
			ui.Text(c, "!").FontSize(theme.FontMicro).FixedLineHeight(theme.IconMicro).TextColor(color)
		})
		ui.Text(c, theme.Tr("未审核", "Unreviewed")).FontSize(theme.FontMeta).
			FixedLineHeight(theme.LineSmall).TextColor(color).MinWidth(0).SingleLine().Ellipsis("…")
	})
}

func renderStoreAuthor(c *ui.Context, author string, bg ui.Color) {
	pal := theme.Ocean()
	fullWidth, _ := c.MeasureText(0, ui.Span{Text: author, Size: theme.FontMeta})
	wrapper := ui.Box(c).MinWidth(0).MaxWidth(fullWidth).Shrink(1).Clip()
	wrapper.DrawOver(func(p *ui.Painter, bounds ui.Rect) {
		if bounds.W >= fullWidth {
			return
		}
		width := min(theme.StoreAuthorFadeWidth, bounds.W)
		fade := ui.Rect{X: bounds.X + bounds.W - width, Y: bounds.Y, W: width, H: bounds.H}
		p.FillGradient(fade, ui.LinearGradient{From: bg.Alpha(0), To: bg, Angle: 90}, 0)
	}).Children(func() {
		ui.Text(c, author).Width(fullWidth).Shrink(0).FontSize(theme.FontMeta).
			FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
	})
	bui.Tooltip(c, wrapper, author)
}

func storeAppIsBeta(entry plugins.StoreAppEntry, installed plugins.InstalledApp, isInstalled bool) bool {
	if isInstalled {
		return installed.Manifest.Beta
	}
	return entry.Beta
}

func storeAppSource(ctrl *Controller, entry plugins.StoreAppEntry, installed plugins.InstalledApp, isInstalled bool) string {
	if storeAppIsOfficial(ctrl, entry, installed, isInstalled) {
		return storeSourceOfficial
	}
	if storeAppIsReviewed(ctrl, entry, installed, isInstalled) {
		return storeSourceReviewed
	}
	return storeSourceUnreviewed
}

func storeAppIsOfficial(ctrl *Controller, entry plugins.StoreAppEntry, installed plugins.InstalledApp, isInstalled bool) bool {
	if !isInstalled {
		return entry.Verified && plugins.IsOfficialRepo(entry.Repo)
	}
	if ctrl == nil || ctrl.sup == nil || ctrl.sup.Store() == nil {
		return false
	}
	return ctrl.sup.Store().MatchesOfficialSource(installed, entry)
}

func storeAppIsReviewed(ctrl *Controller, entry plugins.StoreAppEntry, installed plugins.InstalledApp, isInstalled bool) bool {
	if !plugins.IsReviewedAuthorRepo(entry.Repo) {
		return false
	}
	if !isInstalled {
		return true
	}
	if ctrl == nil || ctrl.sup == nil || ctrl.sup.Store() == nil {
		return false
	}
	return ctrl.sup.Store().MatchesReviewedSource(installed, entry)
}

func buildStoreEntries(catalog *plugins.StoreIndex, installed []plugins.InstalledApp) []plugins.StoreAppEntry {
	var all []plugins.StoreAppEntry
	seen := make(map[string]bool)
	if catalog != nil {
		for _, app := range catalog.Apps {
			if app.ID == "" || seen[app.ID] || storedata.Blocklisted(app.ID, app.Repo) {
				continue
			}
			seen[app.ID] = true
			all = append(all, app)
		}
	}
	for _, inst := range installed {
		id := inst.Manifest.ID
		if id == "" || seen[id] || storedata.Blocklisted(id, inst.Manifest.Repo) {
			continue
		}
		seen[id] = true
		all = append(all, plugins.StoreAppEntry{
			App:               inst.Manifest.App,
			ID:                id,
			Category:          inst.Manifest.EffectiveCategory(),
			Name:              inst.Manifest.Name,
			Version:           inst.Manifest.Version,
			Beta:              inst.Manifest.Beta,
			Author:            inst.Manifest.Author,
			Repo:              inst.Manifest.Repo,
			Summary:           inst.Manifest.Summary,
			Permissions:       inst.Manifest.Permissions,
			PermissionReasons: plugins.ClonePermissionReasons(inst.Manifest.PermissionReasons),
			Surfaces:          inst.Manifest.Surfaces,
			FileExtensions:    inst.Manifest.DisplayFileExtensions(),
			Icon:              inst.Manifest.Icon,
		})
	}
	return all
}
