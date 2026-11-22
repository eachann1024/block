package pluginui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"block/internal/fuzzy"
	"block/internal/icons"
	"block/internal/modals"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	storeSourceOfficial   = "official"
	storeSourceReviewed   = "reviewed"
	storeSourceUnreviewed = "unreviewed"
	storeSortName         = "name"
)

func updatableStoreEntries(ctrl *Controller) []plugins.StoreAppEntry {
	if ctrl == nil || ctrl.sup == nil {
		return nil
	}
	installed := ctrl.sup.Installed()
	installedMap := make(map[string]plugins.InstalledApp, len(installed))
	for _, app := range installed {
		installedMap[app.Manifest.ID] = app
	}
	entries := buildStoreEntries(ctrl.sup.Catalog(), installed)
	var out []plugins.StoreAppEntry
	for _, entry := range entries {
		app, isInstalled := installedMap[entry.ID]
		if isInstalled && entry.ID != plugins.DiagnosticsPluginID && storeEntryHasUpdate(entry, app) {
			out = append(out, entry)
		}
	}
	return out
}

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
			ctrl.sup.UpdateEntriesAsync(updatable, ctrl.clearPluginState)
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
	refreshing, installingID, storeErr, storeNotice := ctrl.sup.StoreStatus()
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

		if refreshing {
			renderStoreSkeleton(c)
			return
		}

		cards := visibleStoreCards(ctrl, locale)
		renderStoreCategoryTabs(c, ctrl, selectedCat, locale)

		list := cards
		if selectedCat != "" {
			list = nil
			for _, card := range cards {
				if card.entry.EffectiveCategory() == selectedCat {
					list = append(list, card)
				}
			}
		}

		if len(list) == 0 {
			renderStoreEmptyState(c, ctrl, selectedCat, locale)
			return
		}

		renderStoreGrid(c, len(list), func() {
			for _, card := range list {
				renderStoreCard(c, ctrl, card, locale, installingID, onSelectTab)
			}
		})
	})
}

func renderStoreGrid(c *ui.Context, count int, children func()) {
	grid := ui.Grid(c.Key("app-store-grid")).FillWidth().MinWidth(0).Gap(theme.StoreCardGap).AlignItems(ui.Stretch)
	columnsFor := func(width float32) int {
		return min(count, max(1, int((width+theme.StoreCardGap)/(theme.StoreMinColWidth+theme.StoreCardGap))))
	}
	columns := columnsFor(grid.Bounds().W)
	grid.Columns(columns).DrawOver(func(_ *ui.Painter, bounds ui.Rect) {
		if columns != columnsFor(bounds.W) {
			c.Invalidate()
		}
	}).Children(children)
}

func visibleStoreCards(ctrl *Controller, locale string) []storeCard {
	installed := ctrl.sup.Installed()
	installedMap := make(map[string]plugins.InstalledApp, len(installed))
	for _, app := range installed {
		installedMap[app.Manifest.ID] = app
	}
	entries := buildStoreEntries(ctrl.sup.Catalog(), installed)
	query := strings.TrimSpace(ctrl.storeQuery)
	type scoredStoreCard struct {
		card  storeCard
		score int
	}
	scoredCards := make([]scoredStoreCard, 0, len(entries))
	for _, entry := range entries {
		app, isInstalled := installedMap[entry.ID]
		source := storeAppSource(ctrl, entry, app, isInstalled)
		if ctrl.storeSource != "" && ctrl.storeSource != source {
			continue
		}
		// Code highlighting packs stay out of the browsing list; they only
		// surface when searched for or already installed.
		if query == "" && !isInstalled && entry.EffectiveCategory() == plugins.CategoryLanguage {
			continue
		}
		matchScore := 0
		if query != "" {
			primary := []string{
				entry.Name.Resolve(locale),
				entry.Name.Zh,
				entry.Name.En,
				entry.ID,
				entry.Author,
				strings.Join(entry.FileExtensions, " "),
			}
			secondary := []string{
				entry.Summary.Resolve(locale),
				entry.Summary.Zh,
				entry.Summary.En,
			}
			sc, ok := fuzzy.Score(query, primary, secondary, false)
			if !ok {
				continue
			}
			matchScore = sc
		}
		scoredCards = append(scoredCards, scoredStoreCard{
			card: storeCard{
				entry: entry, installed: app, isInstalled: isInstalled, source: source,
				downloads: ctrl.sup.Store().EffectiveDownloads(entry),
			},
			score: matchScore,
		})
	}
	sort.SliceStable(scoredCards, func(i, j int) bool {
		if query != "" && scoredCards[i].score != scoredCards[j].score {
			return scoredCards[i].score > scoredCards[j].score
		}
		a, b := scoredCards[i].card, scoredCards[j].card
		if ctrl.storeSort != storeSortName {
			// Sort by catalog downloads only so local installs never shift card positions.
			da, db := max(0, a.entry.Downloads), max(0, b.entry.Downloads)
			if da != db {
				return da > db
			}
		}
		na, nb := strings.ToLower(a.entry.Name.Resolve(locale)), strings.ToLower(b.entry.Name.Resolve(locale))
		if na != nb {
			return na < nb
		}
		return a.entry.ID < b.entry.ID
	})
	cards := make([]storeCard, len(scoredCards))
	for i, sc := range scoredCards {
		cards[i] = sc.card
	}
	return cards
}

func renderStoreCategoryTabs(
	c *ui.Context,
	ctrl *Controller,
	selected plugins.Category,
	locale string,
) {
	pal := theme.Ocean()

	type catTabSpec struct {
		cat   plugins.Category
		key   string
		label string
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
			ctrl.sup.InstallFromSourceAsync(source, func(pluginID string) {
				ctrl.clearManualSpec()
				ctrl.clearPluginState(pluginID)
			})
		}
	})
}

func storeCardFrame(c *ui.Context, id string) ui.Element {
	sc := theme.Ocean().StoreCard()
	return ui.Column(c.Key(id)).FillWidth().MinWidth(0).
		Padding(theme.StoreCardPadTop, theme.StoreCardPadX, theme.StoreCardPadBottom, theme.StoreCardPadX).
		Gap(theme.StoreCardInnerGap).Radius(theme.RadiusXL).Border(1, sc.Border).Background(sc.BG)
}

func renderStoreCard(c *ui.Context, ctrl *Controller, card storeCard, locale, installingID string, onSelectTab func(nextTab string)) {
	pal := theme.Ocean()
	sc := pal.StoreCard()
	entry := card.entry
	name := entry.Name.Resolve(locale)
	busy := installingID == entry.ID
	hasSettings := card.isInstalled && card.installed.Manifest.HasSettingsPage() && entry.ID != plugins.DiagnosticsPluginID

	cardBtn := ui.ButtonBase(c.Key("app-store-card-" + entry.ID)).FillWidth().MinWidth(0).
		Padding(theme.StoreCardPadTop, theme.StoreCardPadX, theme.StoreCardPadBottom, theme.StoreCardPadX).
		Radius(theme.RadiusXL).Cursor(ui.CursorPointer).Label(name)
	hovered := cardBtn.Hovered()
	pressed := cardBtn.Pressed()
	cardBG := sc.BG
	cardBorder := sc.Border
	if hovered || pressed {
		cardBG = sc.HoverBG
		cardBorder = sc.HoverBorder
	}
	bui.AnimateSurfaceColors(cardBtn, cardBG, cardBorder, true)
	if hovered && !pressed {
		bui.Elevate(cardBtn, theme.ElevationCardHover)
	}
	bui.TactileCard(cardBtn, hovered, pressed, false, false)

	iconBG := sc.IconBG
	iconBorder := sc.IconBorder
	iconColors := pal.DuoIconColors(pal.Text)
	if card.source == storeSourceOfficial {
		iconBG = sc.OfficialIconBG
		iconBorder = sc.OfficialIconBorder
		iconColors = pal.DuoIconColors(pal.Accent)
	}

	actionHandled := false
	actionHovered := false
	cardBtn.Children(func() {
		ui.Column(c.Key("app-store-tile-" + entry.ID)).FillWidth().Grow(1).MinWidth(0).Gap(theme.StoreCardInnerGap).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(theme.StoreIconBoxSize, theme.StoreIconBoxSize).Shrink(0).Center().
						Radius(theme.RadiusLG).Background(iconBG).Border(1, iconBorder).Children(func() {
						DrawDuoSpec(c, entry.Icon, theme.StoreIconGlyphSize, iconColors)
					})
					ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
						ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
							var nameMarks [][2]int
							if q := strings.TrimSpace(ctrl.storeQuery); q != "" {
								nameMarks = fuzzy.HighlightRanges(name, q)
							}
							bui.HighlightedText(c, name, nameMarks, theme.FontBody, theme.WeightControl, pal.Text, pal.SearchHighlight).
								FixedLineHeight(theme.LineBody).MinWidth(0).Shrink(1)
							if storeAppIsBeta(entry, card.installed, card.isInstalled) {
								bui.BetaBadge(c, true)
							}
						})
						renderStoreSource(c, entry.Author, card.source, cardBG)
					})
				})
				if card.isInstalled && entry.ID != plugins.DiagnosticsPluginID && storeEntryHasUpdate(entry, card.installed) {
					label, glyph := theme.Tr("更新", "Update"), icons.Refresh
					if busy {
						label = theme.Tr("更新中…", "Updating…")
					}
					pluginBusy := ctrl.sup.IsPluginBusyAnyNotebook(entry.ID)
					disabled := (installingID != "" && !busy) || (pluginBusy && !busy)
					upBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
						ID: "app-store-update-" + entry.ID, Label: label, Leading: &glyph,
						Style: bui.ButtonAccentOutline, Disabled: disabled,
						Loading: busy, Height: theme.HeightCompact, PaddingX: theme.Space8,
					})
					if upBtn.Hovered() {
						actionHovered = true
					}
					if upBtn.Clicked() && !busy && !disabled {
						actionHandled = true
						ctrl.closeStoreMenus()
						ctrl.sup.InstallEntryAsync(entry, ctrl.clearPluginState)
					}
					return
				}
				if card.isInstalled {
					if hasSettings {
						openBtn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
							ID:       "app-store-open-" + entry.ID,
							Label:    theme.Tr("打开", "Open"),
							Style:    bui.ButtonSecondary,
							Disabled: busy,
							Loading:  busy,
							Height:   theme.HeightCompact,
							PaddingX: theme.Space10,
						})
						if openBtn.Hovered() {
							actionHovered = true
						}
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
					} else {
						ui.Row(c.Key("app-store-installed-"+entry.ID)).Height(theme.HeightCompact).
							PaddingX(theme.Space8).Gap(theme.Space4).Radius(theme.RadiusMD).
							Background(sc.InstalledPillBG).Border(1, pal.Hairline).AlignItems(ui.Center).Shrink(0).Children(func() {
							icons.Draw(c, icons.Check, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
							ui.Text(c, theme.Tr("已安装", "Installed")).FontSize(theme.FontSmall).
								FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
						})
					}
					return
				}

				label, glyph := theme.Tr("安装", "Install"), icons.Download
				if busy {
					label = theme.Tr("安装中…", "Installing…")
				}
				disabled := installingID != "" && !busy
				button := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
					ID: "app-store-install-" + entry.ID, Label: label, Leading: &glyph,
					Style: bui.ButtonAccentOutline, Disabled: disabled,
					Loading: busy, Height: theme.HeightCompact, PaddingX: theme.Space8,
				})
				if button.Hovered() {
					actionHovered = true
				}
				if button.Clicked() && !busy && !disabled {
					actionHandled = true
					ctrl.closeStoreMenus()
					ctrl.sup.InstallEntryAsync(entry, ctrl.clearPluginState)
				}
			})
			bui.FadedTextPreview(c, strings.TrimSpace(entry.Summary.Resolve(locale)), 2,
				theme.FontBody, theme.LineTitle, pal.TextSecondary, cardBG)
			ui.Box(c).Grow(1)
			renderStoreCardMeta(c, card, locale, cardBG)
		})
	})
	bui.Probe(cardBtn, "app-store-card-"+entry.ID)
	if !actionHandled && !actionHovered && cardBtn.Clicked() {
		ctrl.closeStoreMenus()
		ctrl.openStoreReadme(entry, card.installed)
		c.Invalidate()
	}
}

func renderStoreSource(c *ui.Context, author, source string, fadeBG ui.Color) {
	pal := theme.Ocean()
	ui.Row(c).FillWidth().MinWidth(0).Height(theme.LineSmall).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
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

func renderStoreCardMeta(c *ui.Context, card storeCard, locale string, fadeBG ui.Color) {
	pal := theme.Ocean()
	sc := pal.StoreCard()
	entry := card.entry
	version := entry.Version
	if card.isInstalled {
		version = card.installed.Manifest.Version
	}
	ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space10).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(sc.Divider)
		ui.Row(c).FillWidth().MinWidth(0).Height(theme.HeightMicro).Gap(theme.Space10).
			Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			var contentRight float32
			tagsWrap := ui.Row(c).Grow(1).MinWidth(0).Height(theme.HeightMicro).AlignItems(ui.Center).Clip()
			tagsWrap.DrawOver(func(p *ui.Painter, bounds ui.Rect) {
				if contentRight <= bounds.X+bounds.W+0.5 {
					return
				}
				width := min(theme.Space24, bounds.W)
				fade := ui.Rect{X: bounds.X + bounds.W - width, Y: bounds.Y, W: width, H: bounds.H}
				p.FillGradient(fade, ui.LinearGradient{From: fadeBG.Alpha(0), To: fadeBG, Angle: 90}, 0)
			}).Children(func() {
				ui.Row(c).Height(theme.HeightMicro).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).
					DrawOver(func(_ *ui.Painter, inner ui.Rect) {
						contentRight = inner.X + inner.W
					}).Children(func() {
					ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusSM).
						Background(sc.CategoryBG).AlignItems(ui.Center).Shrink(0).Children(func() {
						ui.Text(c, plugins.CategoryLabel(entry.EffectiveCategory(), locale)).FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary).SingleLine()
					})
					for _, ext := range entry.FileExtensions {
						ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusSM).
							Background(sc.ExtBG).Border(1, sc.ExtBorder).AlignItems(ui.Center).Shrink(0).Children(func() {
							ui.Text(c, ext).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).SingleLine()
						})
					}
				})
			})
			ui.Row(c).Gap(theme.Space10).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0).Children(func() {
					icons.Draw(c, icons.Download, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
					ui.Text(c, formatDownloadCount(card.downloads)).Font("monospace").FontSize(theme.FontMeta).
						FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary).SingleLine()
				})
				if version != "" {
					ui.Text(c, "v"+strings.TrimPrefix(version, "v")).Font("monospace").FontSize(theme.FontMeta).
						FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary).SingleLine()
				}
			})
		})
	})
}

func renderStoreSkeleton(c *ui.Context) {
	pal := theme.Ocean()
	sc := pal.StoreCard()
	alpha := float32(1)
	if !c.Preferences().ReduceMotion {
		phase := float64(time.Now().UnixNano()%int64(storeReadmeSkeletonPeriod)) / float64(storeReadmeSkeletonPeriod)
		alpha = float32(0.75 + 0.25*math.Cos(2*math.Pi*phase))
		c.Invalidate()
	}
	bar := func(width, height float32) ui.Element {
		return ui.Box(c).Size(width, height).Radius(theme.RadiusXS).Background(pal.Selected).Opacity(alpha)
	}
	ui.Column(c.Key("app-store-skeleton")).FillWidth().MinWidth(0).Gap(theme.Space24).Role(ui.RoleStatus).
		Label(theme.Tr("正在加载插件目录", "Loading plugin catalog")).Children(func() {
		ui.Column(c).FillWidth().Gap(theme.Space12).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
				for range 1 + len(plugins.StoreCategories()) {
					bar(theme.StoreIconBoxSize+theme.Space24, theme.HeightControl)
				}
			})
			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		})
		renderStoreGrid(c, 6, func() {
			for i := range 6 {
				storeCardFrame(c, "app-store-skeleton-"+strconv.Itoa(i)).Children(func() {
					ui.Row(c).FillWidth().MinWidth(0).Height(theme.LineTitle + theme.Space2 + theme.LineSmall).
						Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
						bar(theme.StoreIconBoxSize, theme.StoreIconBoxSize).Radius(theme.RadiusLG)
						ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space6).Children(func() {
							bar(0, theme.LineMeta).WidthPercent(60)
							bar(0, theme.StoreSkeletonLineH).WidthPercent(45)
						})
						bar(theme.StoreIconBoxSize+theme.Space16, theme.HeightCompact)
					})
					ui.Column(c).FillWidth().Height(2 * theme.LineTitle).Gap(theme.Space10).Justify(ui.Center).Children(func() {
						for _, pct := range []float32{94, 72} {
							bar(0, theme.StoreSkeletonLineH).WidthPercent(pct)
						}
					})
					ui.Column(c).FillWidth().Gap(theme.Space10).Children(func() {
						ui.Box(c).FillWidth().Height(1).Background(sc.Divider)
						ui.Row(c).FillWidth().Height(theme.HeightMicro).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
							bar(theme.StoreIconBoxSize+theme.Space16, theme.StoreSkeletonLineH)
							ui.Row(c).Gap(theme.Space10).AlignItems(ui.Center).Children(func() {
								bar(theme.StoreIconBoxSize, theme.StoreSkeletonLineH)
								bar(theme.StoreIconBoxSize, theme.StoreSkeletonLineH)
							})
						})
					})
				})
			}
		})
	})
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
			App:            inst.Manifest.App,
			ID:             id,
			Category:       inst.Manifest.EffectiveCategory(),
			Name:           inst.Manifest.Name,
			Version:        inst.Manifest.Version,
			Beta:           inst.Manifest.Beta,
			Author:         inst.Manifest.Author,
			Repo:           inst.Manifest.Repo,
			Summary:           inst.Manifest.Summary,
			Permissions:       inst.Manifest.Permissions,
			PermissionReasons: plugins.ClonePermissionReasons(inst.Manifest.PermissionReasons),
			Surfaces:          inst.Manifest.Surfaces,
			FileExtensions: inst.Manifest.DisplayFileExtensions(),
			Icon:           inst.Manifest.Icon,
		})
	}
	return all
}
