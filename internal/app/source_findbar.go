package app

import (
	"fmt"

	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

const sourceFindInputW float32 = 180

func shortcutLabel(mac, other string) string {
	if theme.IsMacOS() {
		return mac
	}
	return other
}

// renderSourceFindBar replaces the code editor's stock find bar with a slim
// one matching the note find bar: one row, replace folded behind a chevron,
// only the case and regex options kept.
func renderSourceFindBar(c *ui.Context, bar ui.Element, f *codeed.FindBar) {
	pal := theme.Ocean()
	canReplace := f.CanReplace()
	padLeft := theme.Space10
	if canReplace {
		padLeft = theme.Space4
	}
	bar.Top(theme.Space8).Margin(0, theme.Space20, 0, theme.Space8).
		Padding(theme.Space4, theme.Space4, theme.Space4, padLeft).
		Radius(theme.RadiusXL).Background(pal.Menu).Border(theme.FindBarBorderWidth, pal.MenuBorder)
	blockui.Elevate(bar, theme.ElevationPopover)
	blockui.Probe(bar, "source-find-bar")

	bar.Children(func() {
		ui.Column(c).Gap(theme.Space4).Children(func() {
			ui.Row(c.Key("source-find-row")).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
				if canReplace {
					glyph, label := icons.ChevronRight, theme.Tr("展开替换", "Show replace")
					if f.Replacing() {
						glyph, label = icons.ChevronDown, theme.Tr("收起替换", "Hide replace")
					}
					if blockui.IconButton(c, glyph, blockui.IconButtonOptions{
						ID: "source-find-replace-toggle", Label: label, Tooltip: label,
						Shortcut: shortcutLabel("⌥⌘F", "Ctrl+H"),
						Size:     theme.HeightControl, IconSize: theme.IconInline,
					}).Clicked() {
						f.SetReplacing(!f.Replacing())
					}
				}
				icons.DrawAnimated(c, icons.FindSearch, theme.IconRow, pal.DuoIconColors(pal.TextSecondary), *f.Query() != "")

				in := ui.TextInputBase(c.Key("source-find-input"), f.Query()).Width(sourceFindInputW).Height(theme.HeightCompact).
					Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
					Placeholder(theme.Tr("查找…", "Find…")).PlaceholderColor(pal.Placeholder).Label("source-find-input").FocusRing(false)
				f.QueryInput(in)
				blockui.Probe(in, "source-find-input")

				sourceFindOption(c, f.CaseSensitive(), "Aa", theme.Tr("区分大小写", "Match case"))
				sourceFindOption(c, f.Regexp(), ".*", theme.Tr("正则表达式", "Regular expression"))

				count, cur := f.Matches()
				countText, countColor := "", pal.TextSecondary
				switch {
				case f.Err() != nil:
					countText, countColor = theme.Tr("无效", "Invalid"), pal.Danger
				case *f.Query() == "":
				case count == 0:
					countText = theme.Tr("无结果", "No results")
				default:
					countText = fmt.Sprintf("%d / %d", cur+1, count)
				}
				ui.Row(c).PaddingX(theme.Space6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, countText).FontSize(theme.FontMeta).TextColor(countColor).FontFeatures("tnum").SingleLine()
				})

				ui.Box(c).Width(1).Height(theme.IconChrome).Margin(0, theme.Space2).Background(pal.SidebarBorder)

				if blockui.IconButton(c, icons.ChevronUp, blockui.IconButtonOptions{
					ID: "source-find-prev", Label: theme.Tr("上一处", "Previous match"), Tooltip: theme.Tr("上一处", "Previous match"),
					Shortcut: shortcutLabel("⇧↩", "Shift+Enter"),
					Size:     theme.HeightControl, IconSize: theme.IconInline, Disabled: count == 0,
				}).Clicked() {
					f.Previous()
				}
				if blockui.IconButton(c, icons.ChevronDown, blockui.IconButtonOptions{
					ID: "source-find-next", Label: theme.Tr("下一处", "Next match"), Tooltip: theme.Tr("下一处", "Next match"),
					Shortcut: shortcutLabel("↩", "Enter"),
					Size:     theme.HeightControl, IconSize: theme.IconInline, Disabled: count == 0,
				}).Clicked() {
					f.Next()
				}
				if blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
					ID: "source-find-close", Label: theme.Tr("关闭查找", "Close find"), Tooltip: theme.Tr("关闭查找", "Close find"),
					Shortcut: "Esc",
					Size:     theme.HeightControl, IconSize: theme.IconInline,
				}).Clicked() {
					f.Close()
				}
			})
			if canReplace && f.Replacing() {
				renderSourceReplaceRow(c, f)
			}
		})
	})
}

func renderSourceReplaceRow(c *ui.Context, f *codeed.FindBar) {
	pal := theme.Ocean()
	ui.Row(c.Key("source-find-replace-row")).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(theme.HeightControl + theme.Space4 + theme.IconRow).Height(theme.IconRow)

		in := ui.TextInputBase(c.Key("source-find-replace-input"), f.Replacement()).Width(sourceFindInputW).Height(theme.HeightCompact).
			Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
			Placeholder(theme.Tr("替换为…", "Replace…")).PlaceholderColor(pal.Placeholder).Label("source-find-replace-input").FocusRing(false)
		f.ReplacementInput(in)
		blockui.Probe(in, "source-find-replace-input")

		ui.Box(c).Grow(1)
		count, _ := f.Matches()
		none := count == 0
		if blockui.ActionButtonWithOptions(c, blockui.ActionButtonOptions{
			ID: "source-find-replace-one", Label: theme.Tr("替换", "Replace"), Style: blockui.ButtonGhost,
			Height: theme.HeightControl, PaddingX: theme.Space8, RegularWeight: true, Disabled: none,
		}).Clicked() {
			f.Replace()
		}
		if blockui.ActionButtonWithOptions(c, blockui.ActionButtonOptions{
			ID: "source-find-replace-all", Label: theme.Tr("全部替换", "Replace all"), Style: blockui.ButtonGhost,
			Height: theme.HeightControl, PaddingX: theme.Space8, RegularWeight: true, Disabled: none,
		}).Clicked() {
			f.ReplaceAll()
		}
	})
}

// sourceFindOption is a small text toggle for a search option; it keeps the
// keyboard in the query field.
func sourceFindOption(c *ui.Context, on *bool, label, tip string) {
	pal := theme.Ocean()
	b := ui.ToggleBase(c, on).Size(theme.HeightMicro, theme.HeightMicro).Center().Radius(theme.RadiusSM).
		Label(tip).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	color := pal.TextSecondary
	switch {
	case *on:
		b.Background(pal.Selected)
		color = pal.Accent
	case b.Hovered():
		b.Background(pal.Hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(theme.FontMeta).TextColor(color).SingleLine()
	})
}
