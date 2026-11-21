package ui

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// KeycapOptions configures an extended Keycap chip (such as shortcut recording chips).
type KeycapOptions struct {
	Recording bool
	Plain     bool
	Height    float32
	MinWidth  float32
	PaddingX  float32
	Radius    float32
	Mono      bool
}

// KeycapWithOptions renders a shortcut keycap chip with optional recording-state styling.
func KeycapWithOptions(c *ui.Context, label string, opts KeycapOptions) ui.Element {
	pal := theme.Ocean()
	h := opts.Height
	if h <= 0 {
		h = theme.KeycapHeight
	}
	if opts.Plain && !opts.Recording {
		chip := ui.Row(c).Height(h).Shrink(0).AlignItems(ui.Center).Justify(ui.Center)
		if opts.MinWidth > 0 {
			chip.MinWidth(opts.MinWidth)
		}
		return chip.Children(func() {
			ShortcutLabel(c, label, ShortcutLabelStyle{
				Font:       "monospace",
				FontSize:   theme.KeycapFontSize,
				LineHeight: theme.LineMeta,
				FontWeight: theme.WeightMono,
				Color:      pal.TextSecondary,
				IsShortcut: true,
			})
		})
	}
	padX := opts.PaddingX
	if padX <= 0 {
		padX = theme.KeycapPadX
	}
	rad := opts.Radius
	if rad <= 0 {
		rad = theme.RadiusXS
	}
	bg := pal.Shell
	bd := pal.BtnBorder
	fg := pal.TextSecondary
	if opts.Mono {
		bg = pal.Keycap().BG
		fg = pal.Text
	}
	if opts.Recording {
		bd = pal.Accent
		fg = pal.Accent
	}
	weight := theme.WeightRegular
	if opts.Mono {
		weight = theme.WeightMono
	}
	chip := ui.Row(c).Height(h).Padding(0, padX, 0, padX).
		Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
		Radius(rad).Background(bg).Border(1, bd)
	if opts.MinWidth > 0 {
		chip.MinWidth(opts.MinWidth)
	}
	return chip.Children(func() {
		ShortcutLabel(c, label, ShortcutLabelStyle{
			FontSize:   theme.KeycapFontSize,
			LineHeight: theme.LineMeta,
			FontWeight: weight,
			Color:      fg,
			IsShortcut: true,
		})
	})
}
