package ui

import (
	"time"

	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// IconButtonVariant picks one of the thinkly-fast icon button families (ui/icon_button.rs).
type IconButtonVariant uint8

const (
	// IconChrome is a standalone titlebar/panel/footer button: 26 box, 18 glyph.
	IconChrome IconButtonVariant = iota
	// IconPlain is an inline/compact button: 24 box, 14 glyph, radius 6.
	IconPlain
	// IconBordered is a sidebar-header style button: 32 box, paper fill, 1px btn border, 18 glyph.
	IconBordered
)

// IconActiveStyle picks how an Active icon button reads (thinkly-fast titlebar toggles).
type IconActiveStyle uint8

const (
	// ActiveOutlined: selected fill + accent ring, glyph in text color.
	ActiveOutlined IconActiveStyle = iota
	// ActiveAccent: selected fill + accent ring, glyph in accent (AI panel, page more menu).
	ActiveAccent
	// ActiveAccentFlat: selected fill only, glyph in accent (feedback dialog).
	ActiveAccentFlat
)

type IconButtonOptions struct {
	ID       string
	Label    string
	Tooltip  string
	Variant  IconButtonVariant
	Size     float32 // Outer square size override (default from Variant)
	IconSize float32 // Inner glyph size override (default from Variant)
	Radius   float32 // Corner radius override (default from Variant)
	Active   bool
	// ActiveStyle refines how Active is drawn.
	ActiveStyle IconActiveStyle
	Disabled    bool
	Danger      bool
}

func (o IconButtonOptions) metrics() (box, glyph, radius float32) {
	switch o.Variant {
	case IconPlain:
		box, glyph, radius = theme.IconButtonBox, theme.IconButtonGlyph, 6
	case IconBordered:
		box, glyph, radius = theme.ControlHeight, theme.IconRow, theme.RadiusControl
	default:
		box, glyph, radius = theme.ChromeBoxSize, theme.IconChrome, theme.RadiusControl
	}
	if o.Size > 0 {
		box = o.Size
	}
	if o.IconSize > 0 {
		glyph = o.IconSize
	}
	if o.Radius > 0 {
		radius = o.Radius
	}
	return
}

// IconButton renders an Ocean Paper icon button with Scheme A Duo hover/active states.
func IconButton(c *ui.Context, glyph icons.Icon, opts IconButtonOptions) ui.Element {
	pal := theme.Ocean()
	boxSize, glyphSize, radius := opts.metrics()
	if opts.ID != "" {
		c.Key(opts.ID)
	}
	btn := ui.ButtonBase(c).Size(boxSize, boxSize).Shrink(0).Center().
		Radius(radius).Cursor(ui.CursorPointer).Disabled(opts.Disabled).DimDisabled(false)
	if opts.Variant == IconBordered {
		btn.Background(pal.Paper).Border(1, pal.BtnBorder)
	} else {
		btn.Border(1, ui.Transparent)
	}
	if opts.Label != "" {
		btn.Label(opts.Label)
	} else if opts.Tooltip != "" {
		btn.Label(opts.Tooltip)
	} else if opts.ID != "" {
		btn.Label(opts.ID)
	}
	if opts.Tooltip != "" {
		Tooltip(c, btn, opts.Tooltip)
	}
	hovered := btn.Hovered() && !opts.Disabled
	pressed := btn.Pressed() && !opts.Disabled

	switch {
	case opts.Disabled:
		btn.Opacity(theme.DisabledOpacity)
	case opts.Danger && (hovered || pressed):
		btn.Background(pal.DangerSurface)
	case opts.Active && opts.ActiveStyle == ActiveAccentFlat:
		btn.Background(pal.Selected)
	case opts.Active && opts.ActiveStyle == ActiveAccent:
		btn.Background(pal.Selected).Border(1, pal.Accent)
	case opts.Active:
		btn.Background(pal.Selected)
	case pressed:
		btn.Background(pal.Selected)
	case hovered && opts.Variant == IconBordered:
		btn.Background(pal.BtnHover)
	case hovered:
		btn.Background(pal.Hover)
	}

	btn.Children(func() {
		var ic theme.IconColors
		switch {
		case opts.Danger && (hovered || opts.Active):
			ic = pal.MonoIconColors(pal.Danger)
		case opts.Active && opts.ActiveStyle != ActiveOutlined:
			ic = pal.DuoIconColors(pal.Accent)
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
	ID         string
	Label      string
	Shortcut   string
	Icon       *icons.Icon
	Checked    bool
	Danger     bool
	Separator  bool
	Disabled   bool
	HasSubmenu bool
	OnClick    func()
}

// MenuRow renders one dropdown/context-menu row (thinkly-fast ui/dropdown.rs):
// 28px tall, radius 8, 18px leading icon, neutral hover, solid red danger hover,
// tinted checked row with a 16px text-coloured check.
func MenuRow(c *ui.Context, item MenuItemSpec, highlighted bool) ui.Element {
	pal := theme.Ocean()
	if item.Separator {
		return ui.Box(c).FillWidth().Height(1).MarginY(3).Background(pal.MenuSeparator)
	}
	if item.ID != "" {
		c.Key(item.ID)
	}
	row := ui.ButtonBase(c).FillWidth().Height(theme.MenuItemHeight).PaddingX(10).Gap(8).AlignItems(ui.Center).
		Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Disabled(item.Disabled).Label(item.Label)
	hovered := (row.Hovered() || highlighted) && !item.Disabled
	fg, iconLine := pal.Text, pal.MenuIcon
	switch {
	case item.Danger && hovered:
		row.Background(pal.MenuDangerHoverBG)
		fg, iconLine = pal.MenuDangerHoverFG, pal.MenuDangerHoverFG
	case item.Danger:
		fg, iconLine = pal.MenuDangerFG, pal.MenuDangerFG
	case hovered:
		row.Background(pal.MenuHover)
	case item.Checked:
		row.Background(pal.MenuChecked)
	}
	if item.Disabled {
		row.Opacity(theme.DisabledOpacity)
	}
	row.Children(func() {
		if item.Icon != nil {
			var ic theme.IconColors
			switch {
			case item.Danger:
				ic = pal.MonoIconColors(iconLine)
			case hovered || item.Checked:
				ic = pal.DuoIconColors(iconLine)
			default:
				ic = pal.MonoIconColors(iconLine)
			}
			icons.Draw(c, *item.Icon, theme.IconRow, ic)
		}
		ui.Text(c, item.Label).FontSize(theme.FontBody).TextColor(fg).Grow(1).SingleLine()
		switch {
		case item.Checked:
			icons.Draw(c, icons.Check, 16, pal.MonoIconColors(pal.Text))
		case item.Shortcut != "":
			ui.Text(c, item.Shortcut).FontSize(theme.FontSmall).TextColor(pal.MenuMuted).SingleLine()
		case item.HasSubmenu:
			icons.Draw(c, icons.TbChevronRight, theme.IconInline, pal.MonoIconColors(pal.MenuMuted))
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

// PopupTransition returns the shared 160ms enter/exit transition for menus and popovers.
func PopupTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: time.Duration(theme.MotionMs) * time.Millisecond,
		Ease:     ui.EaseOut,
		Enter:    &ui.Motion{Y: -4, Opacity: 0.01},
		Exit:     &ui.Motion{Y: -4},
	}
}

// MenuSurface styles el as a dropdown/context-menu surface: menu fill, radius 14,
// 4px padding, 2px row gap, a hairline ring plus layered soft shadows, and 160ms popup motion.
func MenuSurface(el ui.Element) ui.Element {
	pal := theme.Ocean()
	el.Background(pal.Menu).Radius(theme.RadiusSurface).Padding(theme.MenuPadding).Gap(theme.MenuGap).
		Border(1, pal.MenuBorder.Alpha(0.6)).
		Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.06)).
		Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08)).
		Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.12)).
		Transition(PopupTransition())
	return el
}

// VerticalResizeHandle renders a vertical splitter adjusting width within [minW, maxW].
func VerticalResizeHandle(c *ui.Context, id string, width *float32, minW, maxW float32, invert bool) bool {
	return ResizeHandle(c, id, width, minW, maxW, invert, 0)
}

// ResizeHandle is VerticalResizeHandle with a double-click reset width (0 = no reset).
// Matches thinkly-fast ui/resize_handle.rs: 8px hit centred on the edge, 2px line,
// menu-border on hover, accent while dragging.
func ResizeHandle(c *ui.Context, id string, width *float32, minW, maxW float32, invert bool, reset float32) bool {
	pal := theme.Ocean()
	if id != "" {
		c.Key(id)
	}
	h := ui.Box(c).Width(theme.ResizeHitWidth).FillHeight().Shrink(0).Cursor(ui.CursorResizeColumn)
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
	if reset > 0 && h.DoubleClicked() && *width != reset {
		*width = max(min(reset, maxW), minW)
		changed = true
	}
	hovered := h.Hovered()
	h.Draw(func(p *ui.Painter, r ui.Rect) {
		cx := r.X + r.W*0.5
		switch {
		case held:
			p.Fill(ui.Rect{X: cx - theme.ResizeLineWidth/2, Y: r.Y, W: theme.ResizeLineWidth, H: r.H}, pal.Accent, 0)
		case hovered:
			p.Fill(ui.Rect{X: cx - theme.ResizeLineWidth/2, Y: r.Y, W: theme.ResizeLineWidth, H: r.H}, pal.MenuBorder, 0)
		default:
			p.Fill(ui.Rect{X: cx - 0.5, Y: r.Y, W: 1, H: r.H}, pal.SidebarBorder, 0)
		}
	})
	if id != "" {
		Probe(h, id)
	}
	return changed
}
