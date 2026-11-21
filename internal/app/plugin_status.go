package app

import (
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func renderPluginStatusStrip(c *ui.Context, sup *plugins.Supervisor, getSelection, getFile func() string) {
	if sup == nil {
		return
	}
	items := sup.StatusItems()
	if len(items) == 0 {
		return
	}
	pal := theme.Ocean()
	c.Key("plugin-status-strip")
	strip := ui.Column(c).FillWidth().Shrink(0).Background(pal.Paper)
	strip.Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		row := ui.Row(c).FillWidth().Height(theme.HeightChrome).Padding(0, theme.Space16).
			Gap(theme.Space12).AlignItems(ui.Center).Justify(ui.End)
		blockui.Probe(row, "plugin-status-strip")
		row.Children(func() {
			for _, it := range items {
				item := it
				key := "plugin-status-" + item.PluginID + "-" + item.ID
				if item.Command != "" {
					btn := ui.ButtonBase(c.Key(key)).Height(theme.HeightMicro).Padding(0, theme.Space6).
						Gap(theme.Space4).Radius(theme.RadiusXS).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					hovered := btn.Hovered()
					pressed := btn.Pressed()
					bg := ui.Transparent
					if pressed {
						bg = pal.Selected
					} else if hovered {
						bg = pal.Hover
					}
					blockui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
					if item.Tooltip != "" {
						blockui.RichTooltip(c, btn, blockui.TooltipSpec{
							Placement: blockui.TooltipAbove,
							Rows:      []blockui.TooltipRow{{Trigger: item.Tooltip}},
						})
					}
					btn.Children(func() {
						txtCol := pal.TextSecondary
						if hovered {
							txtCol = pal.Text
						}
						if item.Icon != nil && len(item.Icon.StrokePaths) > 0 {
							pluginui.DrawDuoSpec(c, *item.Icon, theme.IconInline, pal.DuoIconColors(txtCol))
						}
						ui.Text(c, item.Text).
							FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).
							TextColor(txtCol).
							SingleLine().
							Ellipsis("…")
					})
					blockui.Probe(btn, key)
					if btn.Clicked() {
						sel := ""
						if getSelection != nil {
							sel = getSelection()
						}
						sup.RunCommandAsync(item.PluginID, item.Command, plugins.CommandContext{
							Note:      sup.ActiveNoteRel(),
							File:      getFile(),
							Selection: sel,
							Source:    "status",
						})
					}
				} else {
					wrap := ui.Row(c.Key(key)).Height(theme.HeightMicro).Padding(0, theme.Space4).
						Gap(theme.Space4).AlignItems(ui.Center)
					if item.Tooltip != "" {
						blockui.RichTooltip(c, wrap, blockui.TooltipSpec{
							Placement: blockui.TooltipAbove,
							Rows:      []blockui.TooltipRow{{Trigger: item.Tooltip}},
						})
					}
					wrap.Children(func() {
						if item.Icon != nil && len(item.Icon.StrokePaths) > 0 {
							pluginui.DrawDuoSpec(c, *item.Icon, theme.IconInline, pal.DuoIconColors(pal.TextSecondary))
						}
						ui.Text(c, item.Text).
							FontSize(theme.FontMeta).
							FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).
							SingleLine().
							Ellipsis("…")
					})
					blockui.Probe(wrap, key)
				}
			}
		})
	})
}
