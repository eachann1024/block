package ui

import (
	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ChipStyle picks the visual emphasis of a Chip button.
type ChipStyle uint8

const (
	ChipDefault ChipStyle = iota
	ChipPrimary
	ChipMuted
)

// ChipOptions configures a reusable compact chip button.
type ChipOptions struct {
	ID      string
	Label   string
	Hint    string
	Tooltip string
	Style   ChipStyle
}

// Chip renders an Ocean Paper compact action chip with optional trailing key hint.
func Chip(c *ui.Context, opts ChipOptions) ui.Element {
	pal := theme.Ocean()
	if opts.ID != "" {
		c.Key(opts.ID)
	}
	btn := ui.ButtonBase(c).Height(theme.HeightMicro).AlignItems(ui.Center).
		Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(opts.Label).
		PaddingX(theme.Space8).Gap(theme.Space4)
	switch {
	case opts.Style == ChipPrimary && btn.Hovered():
		btn.Background(pal.Accent.Alpha(0.18))
	case opts.Style == ChipPrimary:
		btn.Background(pal.Accent.Alpha(0.12))
	case btn.Hovered():
		btn.Background(pal.Hover)
	}
	if opts.Tooltip != "" {
		Tooltip(c, btn, opts.Tooltip)
	}
	btn.Children(func() {
		fg := pal.Text
		hint := pal.Accent
		w := theme.WeightRegular
		switch opts.Style {
		case ChipPrimary:
			fg, hint, w = pal.Accent, pal.Accent, theme.WeightControl
		case ChipMuted:
			fg = pal.TextSecondary
		}
		ui.Text(c, opts.Label).FontSize(theme.FontMeta).FontWeight(w).TextColor(fg).SingleLine()
		if opts.Hint != "" {
			ui.Text(c, opts.Hint).Font("monospace").FontSize(theme.FontMeta).FontWeight(theme.WeightMono).TextColor(hint).SingleLine()
		}
	})
	if opts.ID != "" {
		Probe(btn, opts.ID)
	}
	return btn
}

// ChipSurface styles a Row as a compact tag/prompt chip container.
func ChipSurface(el ui.Element, active bool) ui.Element {
	pal := theme.Ocean()
	bg := pal.CodeBG
	brd := pal.Hairline
	if active {
		bg = pal.Hover
		brd = pal.IconFill
	}
	return el.Height(theme.HeightMicro).
		Padding(0, theme.Space4, 0, theme.Space8).Gap(theme.Space2).
		AlignItems(ui.Center).Radius(theme.RadiusSM).Background(bg).Border(1, brd)
}

// BetaBadge renders an Ocean Paper "Beta" status pill for preview plugins/features.
func BetaBadge(c *ui.Context, compact bool) {
	pal := theme.Ocean()
	h := theme.HeightMicro
	padX := theme.Space8
	if compact {
		h = theme.KeycapHeight
		padX = theme.Space6
	}
	// Light outline only: no fill, softened warm ring, regular weight.
	ring := pal.Warning
	ring.A = ring.A * 9 / 20
	ui.Row(c).Height(h).PaddingX(padX).Radius(theme.RadiusPill).Shrink(0).
		Border(1, ring).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Beta").
			FontSize(theme.FontMicro).
			FixedLineHeight(theme.LineMeta).
			FontWeight(theme.WeightRegular).
			TextColor(pal.Warning).
			SingleLine()
	})
}

// BetaBadgePlain renders a muted, borderless 11px "Beta" text label for dense lists such as sidebars.
func BetaBadgePlain(c *ui.Context) {
	pal := theme.Ocean()
	ui.Text(c, "Beta").
		FontSize(theme.FontMicro).
		FixedLineHeight(theme.LineMeta).
		FontWeight(theme.WeightRegular).
		TextColor(pal.TextSecondary).
		SingleLine().
		Shrink(0)
}

func monochromeStatusBadge(c *ui.Context, label string, compact bool) {
	pal := theme.Ocean()
	h := theme.HeightMicro
	if compact {
		h = theme.KeycapHeight
	}
	ui.Row(c).Height(h).Gap(theme.Space4).Shrink(0).AlignItems(ui.Center).Children(func() {
		icons.Draw(c, icons.Check, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
		ui.Text(c, label).
			FontSize(theme.FontMicro).
			FixedLineHeight(theme.LineMeta).
			FontWeight(theme.WeightRegular).
			TextColor(pal.TextSecondary).
			SingleLine()
	})
}

// OfficialBadge renders an Ocean Paper "Official" badge for verified first-party plugins.
func OfficialBadge(c *ui.Context, compact bool) {
	monochromeStatusBadge(c, theme.Tr("官方", "Official"), compact)
}

// ReviewedBadge renders an Ocean Paper "Reviewed" badge for plugins from trusted reviewed authors.
func ReviewedBadge(c *ui.Context, compact bool) {
	monochromeStatusBadge(c, theme.Tr("已审核", "Reviewed"), compact)
}

// FileExtBadge renders a subtle Ocean Paper extension pill (e.g. JSON, PNG, MDX)
// beside non-.md entries in the file tree so note rows stay uncluttered while
// non-note formats remain identifiable at a glance.
func FileExtBadge(c *ui.Context, ext string, active bool) {
	pal := theme.Ocean()
	fg := pal.MenuMuted
	if active {
		fg = pal.TextSecondary
	}
	ui.Row(c).Height(theme.KeycapHeight).PaddingX(theme.Space4).Radius(theme.RadiusXS).Shrink(0).
		Background(pal.MenuHover).Border(1, pal.Hairline).AlignItems(ui.Center).Children(func() {
		ui.Text(c, ext).
			Font("monospace").
			FontSize(theme.FontMicro).
			FixedLineHeight(theme.LineMeta).
			FontWeight(theme.WeightMono).
			TextColor(fg).
			SingleLine()
	})
}
