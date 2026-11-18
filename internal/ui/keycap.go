package ui

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// Keycap renders a shortcut chip (thinkly-fast ui/shortcut_label.rs `kbd`):
// 18px tall, 5px side padding, 4px radius, shell fill with a btn-border ring
// and 11px secondary text. Use it for every inline shortcut hint.
func Keycap(c *ui.Context, label string) ui.Element {
	pal := theme.Ocean()
	return ui.Row(c).Height(theme.KeycapHeight).Padding(0, theme.KeycapPadX, 0, theme.KeycapPadX).
		Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusMicroSM).Background(pal.Shell).Border(1, pal.BtnBorder).
		Children(func() {
			ui.Text(c, label).FontSize(theme.KeycapFontSize).TextColor(pal.TextSecondary).SingleLine()
		})
}

// ShortcutKeycap formats a shortcut spec (e.g. "cmd-k") and renders it as a Keycap.
func ShortcutKeycap(c *ui.Context, spec string) ui.Element {
	return Keycap(c, FormatShortcutDisplay(spec))
}
