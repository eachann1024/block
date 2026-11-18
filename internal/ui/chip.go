package ui

import (
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
	btn := ui.ButtonBase(c).Height(theme.HeightCompact).AlignItems(ui.Center).
		Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(opts.Label)
	if opts.Style == ChipPrimary {
		btn.PaddingX(theme.Space12).Gap(theme.Space6).Background(pal.Selected).Border(1, pal.Accent)
	} else {
		btn.PaddingX(theme.Space8).Gap(theme.Space4)
	}
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
	if opts.Tooltip != "" {
		Tooltip(c, btn, opts.Tooltip)
	}
	btn.Children(func() {
		fg := pal.Text
		w := theme.WeightRegular
		switch opts.Style {
		case ChipPrimary:
			w = theme.WeightControl
		case ChipMuted:
			fg = pal.TextSecondary
		}
		ui.Text(c, opts.Label).FontSize(theme.FontBody).FontWeight(w).TextColor(fg).SingleLine()
		if opts.Hint != "" {
			ui.Text(c, opts.Hint).Font("monospace").FontSize(theme.FontMeta).FontWeight(theme.WeightMono).TextColor(pal.Accent).SingleLine()
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
