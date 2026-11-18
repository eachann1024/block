package ui

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// Tooltip attaches the Block tooltip pill to anchor (thinkly-fast ui/tooltip.rs):
// 30px tall, 12px side padding, 13px medium text, radius 8, dark pill with a
// medium shadow, 6px below the anchor (or above where there is no room).
func Tooltip(c *ui.Context, anchor ui.Element, text string) ui.Element {
	if text == "" {
		return anchor
	}
	anchor.Description(text)
	pal := theme.Ocean()
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		tip.Height(theme.TooltipHeight).Padding(0, theme.TooltipPadX, 0, theme.TooltipPadX).
			AlignItems(ui.Center).Justify(ui.Center).Radius(theme.RadiusControl).
			Background(pal.TooltipBG).Border(1, pal.TooltipBorder).
			Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.18))
		tip.Children(func() {
			ShortcutLabel(c, text, ShortcutLabelStyle{
				FontSize:   theme.TooltipFontSize,
				LineHeight: 18,
				FontWeight: 500,
				Color:      pal.TooltipFG,
			})
		})
		tip.AttachTo(anchor, ui.AnchorBottom, ui.AnchorTop).Margin(theme.TooltipOffset, 0, 0, 0)
	})
	return anchor
}
