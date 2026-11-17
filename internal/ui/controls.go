package ui

import (
	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

type IconButtonOptions struct {
	ID       string
	Label    string
	Tooltip  string
	Size     float32 // Outer square size (default 28)
	IconSize float32 // Inner glyph size (default 18)
	Active   bool
	Disabled bool
	Danger   bool
}

// IconButton renders an Ocean Paper icon button with Scheme A Duo hover/active states.
func IconButton(c *ui.Context, glyph icons.Icon, opts IconButtonOptions) ui.Element {
	pal := theme.Ocean()
	boxSize := opts.Size
	if boxSize <= 0 {
		boxSize = 28
	}
	glyphSize := opts.IconSize
	if glyphSize <= 0 {
		glyphSize = 18
	}
	if opts.ID != "" {
		c.Key(opts.ID)
	}
	btn := ui.ButtonBase(c).Size(boxSize, boxSize).Shrink(0).Center().
		Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Disabled(opts.Disabled).DimDisabled(false)
	if opts.Label != "" {
		btn.Label(opts.Label)
	} else if opts.Tooltip != "" {
		btn.Label(opts.Tooltip)
	} else if opts.ID != "" {
		btn.Label(opts.ID)
	}
	if opts.Tooltip != "" {
		btn.Tooltip(opts.Tooltip)
	}
	hovered := btn.Hovered() && !opts.Disabled
	pressed := btn.Pressed() && !opts.Disabled

	switch {
	case opts.Disabled:
		btn.Opacity(0.45)
	case opts.Danger && (hovered || pressed):
		btn.Background(pal.DangerSurface)
	case opts.Active:
		btn.Background(pal.Selected)
	case pressed:
		btn.Background(pal.Selected)
	case hovered:
		btn.Background(pal.Hover)
	}

	btn.Children(func() {
		var ic theme.IconColors
		switch {
		case opts.Danger && (hovered || opts.Active):
			ic = pal.MonoIconColors(pal.Danger)
		case opts.Active || hovered:
			ic = pal.DuoIconColors(pal.Text)
		default:
			ic = pal.MonoIconColors(pal.TextSecondary)
		}
		icons.Draw(c, glyph, glyphSize, ic)
	})
	if opts.ID != "" {
		Probe(btn, opts.ID)
	}
	return btn
}

type ActionButtonStyle uint8

const (
	ButtonSecondary ActionButtonStyle = iota
	ButtonPrimary
	ButtonGhost
	ButtonDanger
)

// ActionButton renders a clean Ocean Paper text (+ optional leading icon) button.
func ActionButton(c *ui.Context, id, label string, leading *icons.Icon, style ActionButtonStyle, disabled bool) ui.Element {
	pal := theme.Ocean()
	if id != "" {
		c.Key(id)
	}
	btn := ui.ButtonBase(c).Height(32).PaddingX(12).Gap(6).AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Disabled(disabled).Label(label)
	hovered := btn.Hovered() && !disabled
	pressed := btn.Pressed() && !disabled

	var fg ui.Color
	switch style {
	case ButtonPrimary:
		fg = pal.AccentText
		bg := pal.Accent
		if pressed {
			bg = pal.AccentPressed
		} else if hovered {
			bg = pal.AccentHover
		}
		btn.Background(bg)
	case ButtonDanger:
		fg = pal.Danger
		bg := pal.DangerSurface
		if hovered || pressed {
			bg = pal.Danger.Alpha(0.18)
		}
		btn.Background(bg).Border(1, pal.Danger.Alpha(0.35))
	case ButtonGhost:
		fg = pal.TextSecondary
		if hovered || pressed {
			fg = pal.Text
			btn.Background(pal.Hover)
		}
	default:
		fg = pal.Text
		bg := pal.Card
		if pressed {
			bg = pal.Selected
		} else if hovered {
			bg = pal.Hover
		}
		btn.Background(bg).Border(1, pal.BtnBorder)
	}

	btn.Children(func() {
		if leading != nil {
			icons.Draw(c, *leading, 16, pal.MonoIconColors(fg))
		}
		ui.Text(c, label).FontSize(14).FontWeight(500).TextColor(fg).SingleLine()
	})
	if id != "" {
		Probe(btn, id)
	}
	return btn
}

type SegmentOption struct {
	ID    string
	Label string
}

// SegmentedControl renders an Ocean Paper segmented radio track.
func SegmentedControl(c *ui.Context, id string, active *int, options []SegmentOption) bool {
	if len(options) == 0 {
		return false
	}
	pal := theme.Ocean()
	prev := *active
	if id != "" {
		c.Key(id)
	}
	seg := ui.SegmentedBase(c, active, len(options))
	track := seg.Track.Height(32).Padding(3).Gap(2).Radius(theme.RadiusControl).
		Background(pal.Shell).Border(1, pal.Hairline)
	if id != "" {
		Probe(track, id)
	}
	track.Children(func() {
		for i, opt := range options {
			item := seg.Segment(i).Height(24).PaddingX(12).Center().Radius(6).Cursor(ui.CursorPointer).Label(opt.Label)
			if i == *active {
				item.Background(pal.Card).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
			} else if item.Hovered() {
				item.Background(pal.Hover.Alpha(0.6))
			}
			if opt.ID != "" {
				Probe(item, opt.ID)
			}
			item.Children(func() {
				col := pal.TextSecondary
				if i == *active {
					col = pal.Text
				}
				ui.Text(c, opt.Label).FontSize(14).FontWeight(500).TextColor(col).SingleLine()
			})
		}
	})
	return *active != prev
}

// TogglePill renders an iOS/macOS-style Ocean Paper boolean switch pill.
func TogglePill(c *ui.Context, id, label string, value *bool, disabled bool) bool {
	pal := theme.Ocean()
	prev := *value
	if id != "" {
		c.Key(id)
	}
	sw := ui.SwitchBase(c, value).Size(38, 22).Shrink(0).Padding(2).Radius(theme.RadiusPill).
		Cursor(ui.CursorPointer).Disabled(disabled).Label(label)
	bg := pal.BtnBorder
	if *value {
		bg = pal.Accent
	}
	sw.Background(bg)
	sw.Children(func() {
		knobX := float32(0)
		if *value {
			knobX = 16
		}
		ui.Box(c).Size(18, 18).Radius(theme.RadiusPill).Background(ui.Hex("#ffffff")).
			Left(knobX).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.2))
	})
	if id != "" {
		Probe(sw, id)
	}
	return *value != prev
}

// ModalCard renders a centered modal dialog over a translucent backdrop that closes on Escape or backdrop click.
func ModalCard(c *ui.Context, id, title string, open *bool, width float32, maxHeight float32, body func()) {
	if !*open {
		return
	}
	pal := theme.Ocean()
	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().
			Background(ui.RGBA(0, 0, 0, 0.36)).Modal()
		backdrop.Children(func() {
			if id != "" {
				c.Key(id)
			}
			card := ui.Column(c).Width(width).MaxWidthPercent(92).
				Radius(theme.RadiusSurface).Background(pal.Paper).Border(1, pal.Hairline).
				Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, 0.24)).
				Role(ui.RoleDialog).Label(title)
			if maxHeight > 0 {
				card.MaxHeight(maxHeight)
			}
			if id != "" {
				Probe(card, id)
			}
			card.Children(body)
			if card.PressedOutside() || backdrop.OverlayShortcut(0, ui.KeyEscape) {
				*open = false
			}
		})
	})
}

// MenuItemSpec describes a row in an Ocean Paper floating menu.
type MenuItemSpec struct {
	ID          string
	Label       string
	Shortcut    string
	Icon        *icons.Icon
	Checked     bool
	Danger      bool
	Separator   bool
	Disabled    bool
	HasSubmenu  bool
	OnClick     func()
}

// MenuRow renders a single standardized Ocean Paper menu item (height 32, 14px text, Duo icon).
func MenuRow(c *ui.Context, item MenuItemSpec, highlighted bool) ui.Element {
	pal := theme.Ocean()
	if item.Separator {
		return ui.Box(c).FillWidth().Height(1).MarginY(4).Background(pal.Hairline)
	}
	if item.ID != "" {
		c.Key(item.ID)
	}
	row := ui.ButtonBase(c).FillWidth().Height(32).PaddingX(10).Gap(8).AlignItems(ui.Center).
		Radius(6).Cursor(ui.CursorPointer).Disabled(item.Disabled).Label(item.Label)
	hovered := row.Hovered() || highlighted
	if hovered && !item.Disabled {
		if item.Danger {
			row.Background(pal.DangerSurface)
		} else {
			row.Background(pal.Hover)
		}
	}
	fg := pal.Text
	if item.Danger {
		fg = pal.Danger
	}
	row.Children(func() {
		if item.Icon != nil {
			var ic theme.IconColors
			if item.Danger {
				ic = pal.MonoIconColors(pal.Danger)
			} else if hovered || item.Checked {
				ic = pal.DuoIconColors(pal.Text)
			} else {
				ic = pal.MonoIconColors(pal.TextSecondary)
			}
			icons.Draw(c, *item.Icon, 16, ic)
		}
		ui.Text(c, item.Label).FontSize(14).TextColor(fg).Grow(1).SingleLine()
		if item.Checked {
			icons.Draw(c, icons.Check, 15, pal.MonoIconColors(pal.Accent))
		} else if item.Shortcut != "" {
			ui.Text(c, item.Shortcut).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
		} else if item.HasSubmenu {
			icons.Draw(c, icons.ChevronRight, 14, pal.MonoIconColors(pal.TextSecondary))
		}
	})
	if item.ID != "" {
		Probe(row, item.ID)
	}
	if row.Clicked() && item.OnClick != nil {
		item.OnClick()
	}
	return row
}

// VerticalResizeHandle renders a 6px hit-target vertical splitter that adjusts `width` within `[minW, maxW]`.
func VerticalResizeHandle(c *ui.Context, id string, width *float32, minW, maxW float32, invert bool) bool {
	pal := theme.Ocean()
	if id != "" {
		c.Key(id)
	}
	h := ui.Box(c).Width(6).FillHeight().Shrink(0).Cursor(ui.CursorResizeColumn)
	dx, _, held := h.Dragged()
	changed := false
	if held && dx != 0 {
		delta := dx
		if invert {
			delta = -dx
		}
		next := max(min(*width+delta, maxW), minW)
		if next != *width {
			*width = next
			changed = true
		}
	}
	hovered := h.Hovered() || held
	h.Draw(func(p *ui.Painter, r ui.Rect) {
		lineX := r.X + r.W*0.5 - 0.5
		col := pal.SidebarBorder
		w := float32(1)
		if hovered {
			col = pal.Accent
			w = 2
		}
		p.Fill(ui.Rect{X: lineX, Y: r.Y, W: w, H: r.H}, col, 0)
	})
	if id != "" {
		Probe(h, id)
	}
	return changed
}
