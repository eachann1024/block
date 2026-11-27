package pluginui

import (
	"fmt"
	"strings"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	pluginDevGuideURLZh = "https://docs.block.cooool.top/"
	pluginDevGuideURLEn = "https://docs.block.cooool.top/en/"
)

func pluginDevGuideURL() string {
	return theme.Tr(pluginDevGuideURLZh, pluginDevGuideURLEn)
}

// storeCategoryIcon returns the Duo icon representing an App Store category
// in empty states. Unknown categories fall back to the generic package glyph.
func storeCategoryIcon(cat plugins.Category) icons.Icon {
	switch cat {
	case plugins.CategoryEdit:
		return icons.Pencil
	case plugins.CategoryOrganize:
		return icons.Tag
	case plugins.CategoryAI:
		return icons.Sparkles
	case plugins.CategoryAppearance:
		return icons.TbPalette
	case plugins.CategoryTools:
		return icons.Wrench
	case plugins.CategoryLanguage:
		return icons.Code
	default:
		return icons.Package
	}
}

type storeEmptySpec struct {
	glyph        icons.Icon
	title        string
	body         string
	primaryID    string
	primaryLabel string
	primaryIcon  icons.Icon
	onPrimary    func()
	showGuide    bool
}

func resolveStoreEmptySpec(
	c *ui.Context,
	ctrl *Controller,
	selectedCat plugins.Category,
	locale string,
) storeEmptySpec {
	query := strings.TrimSpace(ctrl.storeQuery)
	if query != "" {
		body := theme.Tr("换个关键词，或提交你想要的插件想法。", "Try a different keyword, or suggest a plugin idea.")
		if selectedCat != "" || ctrl.storeSource != "" {
			body = theme.Tr("换个关键词，或切换到「全部」分类再试。", "Try a different keyword, or switch back to All.")
		}
		return storeEmptySpec{
			glyph:        icons.FindSearch,
			title:        fmt.Sprintf(theme.Tr("没有找到「%s」相关插件", "No plugins found for %q"), query),
			body:         body,
			primaryID:    "app-store-empty-clear-search",
			primaryLabel: theme.Tr("清除搜索", "Clear search"),
			primaryIcon:  icons.Close,
			onPrimary: func() {
				ctrl.storeQuery = ""
				c.Invalidate()
			},
		}
	}

	if ctrl.storeSource != "" {
		return storeEmptySpec{
			glyph:        icons.FindSearch,
			title:        theme.Tr("当前筛选条件下暂无插件", "No plugins match the current filter"),
			body:         theme.Tr("切换来源筛选，或查看全部插件。", "Try switching the source filter or viewing all plugins."),
			primaryID:    "app-store-empty-reset-filter",
			primaryLabel: theme.Tr("重置筛选", "Reset filters"),
			primaryIcon:  icons.Refresh,
			onPrimary: func() {
				ctrl.storeSource = ""
				ctrl.setStoreCategory("")
				c.Invalidate()
			},
		}
	}

	if selectedCat != "" {
		title, body := plugins.CategoryEmptyCopy(selectedCat, locale)
		return storeEmptySpec{
			glyph:        storeCategoryIcon(selectedCat),
			title:        title,
			body:         body,
			primaryID:    "app-store-empty-idea",
			primaryLabel: theme.Tr("提交插件想法", "Suggest a plugin"),
			primaryIcon:  icons.Plus,
			onPrimary: func() {
				ctrl.suggestPlugin(selectedCat)
			},
			showGuide: true,
		}
	}

	return storeEmptySpec{
		glyph:        icons.Package,
		title:        theme.Tr("暂无可用插件", "No plugins available"),
		body:         theme.Tr("插件上架后会出现在这里。", "Plugins will appear here once published."),
		primaryID:    "app-store-empty-idea",
		primaryLabel: theme.Tr("提交插件想法", "Suggest a plugin"),
		primaryIcon:  icons.Plus,
		onPrimary: func() {
			ctrl.suggestPlugin("")
		},
		showGuide: true,
	}
}

func renderStoreEmptyState(
	c *ui.Context,
	ctrl *Controller,
	selectedCat plugins.Category,
	locale string,
) {
	pal := theme.Ocean()
	spec := resolveStoreEmptySpec(c, ctrl, selectedCat, locale)

	root := ui.Column(c.Key("app-store-empty")).
		FillWidth().MinWidth(0).
		Padding(theme.StoreEmptyPadY, theme.Space24).
		Gap(theme.Space16).
		AlignItems(ui.Center)
	bui.Probe(root, "app-store-empty")

	root.Children(func() {
		ui.Box(c).
			Size(theme.StoreEmptyIconBox, theme.StoreEmptyIconBox).
			Shrink(0).
			Radius(theme.RadiusXL).
			Background(pal.Shell).
			Center().
			Children(func() {
				icons.Draw(c, spec.glyph, theme.StoreEmptyIconGlyph, pal.DuoIconColors(pal.Text))
			})

		ui.Column(c).
			FillWidth().
			MaxWidth(theme.StoreEmptyMaxWidth).
			Gap(theme.Space6).
			AlignItems(ui.Center).
			Children(func() {
				ui.Text(c, spec.title).
					FontSize(theme.FontHeading).
					FixedLineHeight(theme.LineHeading).
					FontWeight(theme.WeightTitle).
					TextColor(pal.Text).
					TextAlign(ui.Center)
				ui.Text(c, spec.body).
					FontSize(theme.FontBody).
					FixedLineHeight(theme.LineBody).
					TextColor(pal.TextSecondary).
					TextAlign(ui.Center)
			})

		ui.Row(c).
			Gap(theme.Space16).
			AlignItems(ui.Center).
			Justify(ui.Center).
			Wrap().
			Children(func() {
				primaryIc := spec.primaryIcon
				primary := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
					ID:       spec.primaryID,
					Label:    spec.primaryLabel,
					Leading:  &primaryIc,
					Style:    bui.ButtonAccentOutline,
					Height:   theme.HeightControl,
					PaddingX: theme.Space16,
				})
				if primary.Clicked() && spec.onPrimary != nil {
					spec.onPrimary()
				}

				if spec.showGuide {
					guideLabel := theme.Tr("插件开发指南", "Plugin developer guide")
					link := ui.ButtonBase(c.Key("app-store-empty-guide")).
						Height(theme.HeightControl).
						PaddingX(theme.Space4).
						AlignItems(ui.Center).
						Radius(theme.RadiusSM).
						Cursor(ui.CursorPointer).
						Label(guideLabel)
					linkFG := pal.Accent
					if link.Pressed() {
						linkFG = pal.AccentPressed
					} else if link.Hovered() {
						linkFG = pal.AccentHover
					}
					link.Children(func() {
						ui.Text(c, guideLabel).
							FontSize(theme.FontBody).
							FixedLineHeight(theme.LineBody).
							FontWeight(theme.WeightControl).
							TextColor(linkFG).
							SingleLine()
					})
					bui.Probe(link, "app-store-empty-guide")
					if link.Clicked() {
						c.OpenURL(pluginDevGuideURL())
					}
				}
			})
	})
}
