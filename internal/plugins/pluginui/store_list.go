package pluginui

import (
	"math"
	"strconv"
	"strings"
	"time"

	"block/internal/fuzzy"
	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// App Store listing (designs: 方案 A 紧凑列表). One grouped card holds a row per
// plugin; while ordered by downloads each row carries its rank, so the most
// downloaded plugin always sits on top.

const (
	storeRankWidth   float32 = 18
	storeRankTopN            = 3
	storeRowPadY             = theme.Space10
	storeRowPadX             = theme.Space12
	storeListPad             = theme.Space4
	storeSkeletonRow         = 6
)

func storeListFrame(c *ui.Context, id string) ui.Element {
	sc := theme.Ocean().StoreCard()
	return ui.Column(c.Key(id)).FillWidth().MinWidth(0).Padding(storeListPad).
		Radius(theme.RadiusXL).Border(1, sc.Border).Background(sc.BG)
}

// storeRowDivider draws a hairline that starts at the text column so it never
// cuts through the rank or icon gutter.
func storeRowDivider(c *ui.Context, ranked bool) {
	inset := storeRowPadX + theme.StoreIconBoxSize + theme.Space12
	if ranked {
		inset += storeRankWidth + theme.Space12
	}
	ui.Row(c).FillWidth().MinWidth(0).Padding(0, storeRowPadX, 0, inset).Shrink(0).Children(func() {
		ui.Box(c).Grow(1).Height(1).Background(theme.Ocean().StoreCard().Divider)
	})
}

func renderStoreList(
	c *ui.Context,
	ctrl *Controller,
	list []storeCard,
	ranked bool,
	locale, installingID string,
	onSelectTab func(nextTab string),
) {
	query := strings.TrimSpace(ctrl.storeQuery)
	storeListFrame(c, "app-store-list").Children(func() {
		for i, card := range list {
			if i > 0 {
				storeRowDivider(c, ranked)
			}
			rank := 0
			if ranked {
				rank = i + 1
			}
			renderStoreRow(c, ctrl, card, rank, query, locale, installingID, onSelectTab)
		}
	})
}

func renderStoreRow(
	c *ui.Context,
	ctrl *Controller,
	card storeCard,
	rank int,
	query, locale, installingID string,
	onSelectTab func(nextTab string),
) {
	pal := theme.Ocean()
	sc := pal.StoreCard()
	entry := card.entry
	name := entry.Name.Resolve(locale)
	busy := installingID == entry.ID
	hasSettings := card.isInstalled && card.installed.Manifest.HasSettingsPage() && entry.ID != plugins.DiagnosticsPluginID

	id := "app-store-card-" + entry.ID
	row := ui.ButtonBase(c.Key(id)).FillWidth().MinWidth(0).
		Padding(storeRowPadY, storeRowPadX).Gap(theme.Space12).AlignItems(ui.Center).
		Radius(theme.RadiusLG).Cursor(ui.CursorPointer).Label(name)
	hovered, pressed := row.Hovered(), row.Pressed()
	rowBG := sc.BG
	if hovered || pressed {
		rowBG = sc.HoverBG
	}
	bui.AnimateSurfaceColors(row, rowBG, ui.Transparent, false)
	bui.TactileCard(row, hovered, pressed, false, false)

	iconBG, iconBorder := sc.IconBG, sc.IconBorder
	iconColors := pal.DuoIconColors(pal.Text)
	if card.source == storeSourceOfficial {
		iconBG, iconBorder = sc.OfficialIconBG, sc.OfficialIconBorder
		iconColors = pal.DuoIconColors(pal.Accent)
	}

	actionHandled, actionHovered := false, false
	row.Children(func() {
		if rank > 0 {
			rankColor := pal.Placeholder
			if rank <= storeRankTopN {
				rankColor = pal.Text
			}
			ui.Text(c, strconv.Itoa(rank)).Width(storeRankWidth).Shrink(0).TextAlign(ui.End).
				FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				FontWeight(theme.WeightTitle).TextColor(rankColor).SingleLine()
		}
		renderStorePluginIcon(
			c, entry, card.source == storeSourceOfficial,
			theme.StoreIconBoxSize, theme.StoreIconGlyphSize, theme.RadiusLG,
			iconBG, iconBorder, iconColors, hovered || pressed,
		)
		ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
				var marks [][2]int
				if query != "" {
					marks = fuzzy.HighlightRanges(name, query)
				}
				bui.HighlightedText(c, name, marks, theme.FontBody, theme.WeightTitle, pal.Text, pal.SearchHighlight).
					FixedLineHeight(theme.LineBody).MinWidth(0).Shrink(1)
				if storeAppIsBeta(entry, card.installed, card.isInstalled) {
					bui.BetaBadge(c, true)
				}
			})
			if summary := strings.TrimSpace(entry.Summary.Resolve(locale)); summary != "" {
				ui.Text(c, summary).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(pal.TextSecondary).SingleLine().Ellipsis("…").MinWidth(0)
			}
			renderStoreRowMeta(c, card, locale, rowBG)
		})
		actionHandled, actionHovered = renderStoreCardAction(c, ctrl, card, busy, hasSettings, installingID, onSelectTab)
	})
	bui.Probe(row, id)
	if !actionHandled && !actionHovered && row.Clicked() {
		ctrl.closeStoreMenus()
		ctrl.openStoreReadme(entry, card.installed)
		c.Invalidate()
	}
}

// renderStoreRowMeta: source/author · category · downloads.
func renderStoreRowMeta(c *ui.Context, card storeCard, locale string, fadeBG ui.Color) {
	pal := theme.Ocean()
	dot := func() {
		ui.Text(c, "·").FontSize(theme.FontMeta).FixedLineHeight(theme.LineSmall).
			TextColor(pal.Placeholder).Shrink(0)
	}
	ui.Row(c).FillWidth().MinWidth(0).Height(theme.LineSmall).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
		renderStoreSource(c, card.entry.Author, card.source, fadeBG)
		dot()
		ui.Text(c, plugins.CategoryLabel(card.entry.EffectiveCategory(), locale)).FontSize(theme.FontMeta).
			FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
		dot()
		ui.Row(c).Gap(theme.Space4).AlignItems(ui.Center).Shrink(0).Children(func() {
			icons.Draw(c, icons.Download, theme.IconMicro, pal.DuoIconColors(pal.TextSecondary))
			ui.Text(c, formatDownloadCount(card.downloads)).Font("monospace").FontSize(theme.FontMeta).
				FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
		})
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
		ui.Box(c).FillWidth().Height(theme.StoreHeroHeight).Radius(theme.RadiusXL).
			Background(pal.Selected).Opacity(alpha)
		ui.Column(c).FillWidth().Gap(theme.Space12).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
				for range 1 + len(plugins.StoreCategories()) {
					bar(theme.StoreIconBoxSize+theme.Space24, theme.HeightControl)
				}
			})
			ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		})
		storeListFrame(c, "app-store-skeleton-list").Children(func() {
			for i := range storeSkeletonRow {
				if i > 0 {
					storeRowDivider(c, true)
				}
				ui.Row(c.Key("app-store-skeleton-"+strconv.Itoa(i))).FillWidth().MinWidth(0).
					Padding(storeRowPadY, storeRowPadX).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
					bar(storeRankWidth, theme.StoreSkeletonLineH)
					bar(theme.StoreIconBoxSize, theme.StoreIconBoxSize).Radius(theme.RadiusLG)
					ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space8).Children(func() {
						bar(0, theme.StoreSkeletonLineH).WidthPercent(30)
						bar(0, theme.StoreSkeletonLineH).WidthPercent(70)
						bar(0, theme.StoreSkeletonLineH).WidthPercent(40)
					})
					bar(theme.StoreCardActionWidth, theme.HeightCompact).Radius(theme.RadiusMD)
				})
			}
		})
	})
}
