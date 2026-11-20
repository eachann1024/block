package pluginui

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"block/internal/icons"
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

func renderStoreHeaderExtras(c *ui.Context, ctrl *Controller) {
	pal := theme.Ocean()
	query := ctrl.storeQuery
	input := ui.TextInput(c.Key("app-store-search"), &query).
		Width(theme.StoreSearchWidth).MaxWidthPercent(100).MinWidth(0).Height(theme.HeightControl).
		PaddingX(theme.Space12).Radius(theme.RadiusMD).FontSize(theme.FontBody).
		FixedLineHeight(theme.LineBody).TextColor(pal.Text).PlaceholderColor(pal.Placeholder).
		Label(theme.Tr("搜索插件或作者", "Search plugins or authors")).
		Placeholder(theme.Tr("搜索插件或作者", "Search plugins or authors"))
	border := pal.BtnBorder
	if input.Focused() {
		border = pal.Accent
	}
	input.Background(pal.Shell).Border(1, border)
	bui.Probe(input, "app-store-search")
	if input.Changed() {
		ctrl.storeQuery = query
	}
	ctrl.storeSourceMenu.Compact = true
	ctrl.storeSortMenu.Compact = true
	bui.DropdownSelect(c, "app-store-source", &ctrl.storeSource, []bui.SelectOption{
		{Label: theme.Tr("全部来源", "All sources")},
		{Value: storeSourceOfficial, Label: theme.Tr("Block 官方", "Official by Block")},
		{Value: storeSourceReviewed, Label: theme.Tr("已审核", "Reviewed")},
		{Value: storeSourceUnreviewed, Label: theme.Tr("未审核", "Unreviewed")},
	}, &ctrl.storeSourceMenu, "")
	bui.DropdownSelect(c, "app-store-sort", &ctrl.storeSort, []bui.SelectOption{
		{Label: theme.Tr("最多下载", "Most downloaded")},
		{Value: storeSortName, Label: theme.Tr("按名称", "By name")},
	}, &ctrl.storeSortMenu, "")
}

type storeCard struct {
	entry       plugins.StoreAppEntry
	installed   plugins.InstalledApp
	downloads   int
	isInstalled bool
	source      string
}

func renderStoreBody(c *ui.Context, ctrl *Controller, installedOnly bool) {
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

		if refreshing && !installedOnly {
			renderStoreSkeleton(c)
			return
		}

		cards := visibleStoreCards(ctrl, locale, installedOnly)
		renderStoreCategoryTabs(c, ctrl, cards, selectedCat, locale)

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
			msg := theme.Tr("没有符合条件的插件", "No matching plugins")
			if installedOnly && selectedCat == "" && ctrl.storeQuery == "" && ctrl.storeSource == "" {
				msg = theme.Tr("暂无已安装插件", "No installed plugins")
			}
			ui.Row(c.Key("app-store-empty")).FillWidth().Padding(theme.Space24, 0).Justify(ui.Center).Children(func() {
				ui.Text(c, msg).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
			})
			return
		}

		renderStoreGrid(c, len(list), func() {
			for _, card := range list {
				renderStoreCard(c, ctrl, card, locale, installingID)
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

func visibleStoreCards(ctrl *Controller, locale string, installedOnly bool) []storeCard {
	installed := ctrl.sup.Installed()
	installedMap := make(map[string]plugins.InstalledApp, len(installed))
	for _, app := range installed {
		installedMap[app.Manifest.ID] = app
	}
	entries := buildStoreEntries(ctrl.sup.Catalog(), installed)
	query := strings.ToLower(strings.TrimSpace(ctrl.storeQuery))
	cards := make([]storeCard, 0, len(entries))
	for _, entry := range entries {
		app, isInstalled := installedMap[entry.ID]
		if installedOnly && !isInstalled {
			continue
		}
		source := storeAppSource(ctrl, entry, app, isInstalled)
		if ctrl.storeSource != "" && ctrl.storeSource != source {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Name.Resolve(locale)), query) &&
			!strings.Contains(strings.ToLower(entry.Author), query) &&
			!strings.Contains(strings.ToLower(entry.Summary.Resolve(locale)), query) &&
			!strings.Contains(strings.ToLower(strings.Join(entry.FileExtensions, " ")), query) {
			continue
		}
		cards = append(cards, storeCard{
			entry: entry, installed: app, isInstalled: isInstalled, source: source,
			downloads: ctrl.sup.Store().EffectiveDownloads(entry),
		})
	}
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if ctrl.storeSort != storeSortName && a.downloads != b.downloads {
			return a.downloads > b.downloads
		}
		return strings.ToLower(a.entry.Name.Resolve(locale)) < strings.ToLower(b.entry.Name.Resolve(locale))
	})
	return cards
}

func renderStoreCategoryTabs(
	c *ui.Context,
	ctrl *Controller,
	cards []storeCard,
	selected plugins.Category,
	locale string,
) {
	pal := theme.Ocean()
	counts := make(map[plugins.Category]int)
	for _, card := range cards {
		counts[card.entry.EffectiveCategory()]++
	}

	type catTabSpec struct {
		cat   plugins.Category
		key   string
		label string
		count int
	}
	tabs := make([]catTabSpec, 0, 1+len(plugins.AllCategories()))
	tabs = append(tabs, catTabSpec{
		cat:   "",
		key:   "all",
		label: theme.Tr("全部", "All"),
		count: len(cards),
	})
	for _, cat := range plugins.AllCategories() {
		tabs = append(tabs, catTabSpec{
			cat:   cat,
			key:   string(cat),
			label: plugins.CategoryLabel(cat, locale),
			count: counts[cat],
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
					ui.Text(c, strconv.Itoa(tab.count)).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightRegular).TextColor(pal.TextSecondary).SingleLine()
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

func storeCardFrame(c *ui.Context, id string) ui.Element {
	pal := theme.Ocean()
	return ui.Column(c.Key(id)).FillWidth().MinWidth(0).Padding(theme.Space20).Gap(theme.StoreCardInnerGap).
		Radius(theme.RadiusXL).Border(1, pal.Hairline).Background(pal.ShellChrome().Card)
}

func renderStoreCard(c *ui.Context, ctrl *Controller, card storeCard, locale, installingID string) {
	pal := theme.Ocean()
	entry := card.entry
	name := entry.Name.Resolve(locale)
	busy := installingID == entry.ID
	storeCardFrame(c, "app-store-tile-"+entry.ID).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
			open := ui.ButtonBase(c.Key("app-store-card-" + entry.ID)).Grow(1).MinWidth(0).
				Gap(theme.StoreCardInnerGap).AlignItems(ui.Center).Label(name).Cursor(ui.CursorPointer)
			bui.Probe(open, "app-store-card-"+entry.ID)
			open.Children(func() {
				ui.Box(c).Size(theme.StoreIconBoxSize, theme.StoreIconBoxSize).Shrink(0).Center().
					Radius(theme.RadiusLG).Background(pal.Shell).Children(func() {
					DrawDuoSpec(c, entry.Icon, theme.StoreIconGlyphSize, pal.DuoIconColors(pal.Text))
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
					ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, name).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
							FontWeight(theme.WeightTitle).TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
						if storeAppIsBeta(entry, card.installed, card.isInstalled) {
							bui.BetaBadge(c, true)
						}
					})
					renderStoreSource(c, entry.Author, card.source)
				})
			})
			if open.Clicked() {
				ctrl.closeStoreMenus()
				ctrl.openStoreReadme(entry, card.installed)
				c.Invalidate()
			}
			if card.isInstalled {
				enabled := card.installed.Enabled
				if bui.TogglePill(c, "app-store-toggle-"+entry.ID,
					theme.Tr("启用 ", "Enable ")+name, &enabled, busy) {
					ctrl.sup.SetPluginEnabled(entry.ID, enabled)
				}
				return
			}

			label, glyph := theme.Tr("安装", "Install"), icons.Download
			if busy {
				label = theme.Tr("处理中…", "Working…")
			}
			button := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
				ID: "app-store-install-" + entry.ID, Label: label, Leading: &glyph,
				Style: bui.ButtonAccentOutline, Disabled: installingID != "" && !busy,
				Loading: busy, Height: theme.HeightCompact, PaddingX: theme.Space8,
			})
			if button.Clicked() && !busy {
				ctrl.closeStoreMenus()
				ctrl.openStoreReadme(entry, card.installed)
				c.Invalidate()
			}
		})
		bui.FadedTextPreview(c, strings.TrimSpace(entry.Summary.Resolve(locale)), 3,
			theme.FontBody, theme.LineTitle, pal.TextSecondary, pal.ShellChrome().Card)
		renderStoreCardMeta(c, card, locale)
	})
}

func renderStoreSource(c *ui.Context, author, source string) {
	pal := theme.Ocean()
	color, label := pal.Warning, theme.Tr("未审核", "Unreviewed")
	switch source {
	case storeSourceOfficial:
		color, label = pal.Accent, theme.Tr("Block 官方", "Official by Block")
	case storeSourceReviewed:
		color, label = pal.Success, theme.Tr("已审核", "Reviewed")
	}
	ui.Row(c).FillWidth().MinWidth(0).Height(theme.LineSmall).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
		if source != storeSourceOfficial && strings.TrimSpace(author) != "" {
			renderStoreAuthor(c, author)
		}
		ui.Row(c).MinWidth(0).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
			switch source {
			case storeSourceOfficial:
				DrawDuoSpec(c, plugins.AppStoreIconSpec(), theme.IconMicro, pal.DuoIconColors(color))
			case storeSourceReviewed:
				icons.Draw(c, icons.Check, theme.IconMicro, pal.MonoIconColors(color))
			default:
				ui.Box(c).Size(theme.IconMicro, theme.IconMicro).Shrink(0).Radius(theme.RadiusPill).
					Border(1, color).Center().Children(func() {
					ui.Text(c, "!").FontSize(theme.FontMicro).FixedLineHeight(theme.IconMicro).TextColor(color)
				})
			}
			ui.Text(c, label).FontSize(theme.FontMeta).FixedLineHeight(theme.LineSmall).
				TextColor(color).MinWidth(0).SingleLine().Ellipsis("…")
		})
	})
}

func renderStoreAuthor(c *ui.Context, author string) {
	pal := theme.Ocean()
	bg := pal.ShellChrome().Card
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

func renderStoreCardMeta(c *ui.Context, card storeCard, locale string) {
	pal := theme.Ocean()
	entry := card.entry
	version := entry.Version
	if card.isInstalled {
		version = card.installed.Manifest.Version
	}
	ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.HeightMicro).Gap(theme.Space10).Wrap().AlignItems(ui.Center).Children(func() {
		ui.Text(c, plugins.CategoryLabel(entry.EffectiveCategory(), locale)).FontSize(theme.FontSmall).
			FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
		ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0).Children(func() {
			icons.Draw(c, icons.Download, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
			ui.Text(c, formatDownloadCount(card.downloads)).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
				TextColor(pal.TextSecondary).SingleLine()
		})
		if version != "" {
			ui.Text(c, "v"+strings.TrimPrefix(version, "v")).Font("monospace").FontSize(theme.FontMeta).
				FixedLineHeight(theme.LineMeta).TextColor(pal.TextSecondary).MinWidth(0).SingleLine().Ellipsis("…")
		}
		for _, ext := range entry.FileExtensions {
			ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusSM).
				Background(pal.CodeBG).AlignItems(ui.Center).Children(func() {
				ui.Text(c, ext).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).MinWidth(0).SingleLine().Ellipsis("…")
			})
		}
	})
}

func renderStoreSkeleton(c *ui.Context) {
	pal := theme.Ocean()
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
				for range 1 + len(plugins.AllCategories()) {
					bar(theme.StoreIconBoxSize+theme.Space24, theme.HeightControl)
				}
			})
			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		})
		renderStoreGrid(c, 6, func() {
			for i := range 6 {
				storeCardFrame(c, "app-store-skeleton-"+strconv.Itoa(i)).Children(func() {
					ui.Row(c).FillWidth().MinWidth(0).Height(theme.LineTitle + theme.Space2 + theme.LineSmall).
						Gap(theme.StoreCardInnerGap).AlignItems(ui.Center).Children(func() {
						bar(theme.StoreIconBoxSize, theme.StoreIconBoxSize).Radius(theme.RadiusLG)
						ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space6).Children(func() {
							bar(0, theme.LineMeta).WidthPercent(60)
							bar(0, theme.StoreSkeletonLineH).WidthPercent(45)
						})
						bar(theme.StoreIconBoxSize+theme.Space16, theme.HeightCompact)
					})
					ui.Column(c).FillWidth().Height(3 * theme.LineTitle).Gap(theme.Space12).Justify(ui.Center).Children(func() {
						for _, pct := range []float32{94, 80, 50} {
							bar(0, theme.StoreSkeletonLineH).WidthPercent(pct)
						}
					})
					ui.Row(c).FillWidth().Height(theme.HeightMicro).Gap(theme.Space10).AlignItems(ui.Center).Children(func() {
						bar(theme.StoreIconBoxSize+theme.Space16, theme.StoreSkeletonLineH)
						bar(theme.StoreIconBoxSize, theme.StoreSkeletonLineH)
						bar(theme.StoreIconBoxSize, theme.StoreSkeletonLineH)
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
	if !entry.Verified || !plugins.IsOfficialRepo(entry.Repo) {
		return false
	}
	if !isInstalled {
		return true
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
			Summary:        inst.Manifest.Summary,
			Permissions:    inst.Manifest.Permissions,
			Surfaces:       inst.Manifest.Surfaces,
			FileExtensions: inst.Manifest.DisplayFileExtensions(),
			Icon:           inst.Manifest.Icon,
		})
	}
	return all
}
