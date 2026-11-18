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
	cursor := ui.CursorPointer
	if opts.Disabled {
		cursor = ui.CursorDefault
	}
	btn := ui.ButtonBase(c).Size(boxSize, boxSize).Shrink(0).Center().
		Radius(radius).Cursor(cursor).Disabled(opts.Disabled).DimDisabled(false)
	if opts.Variant == IconBordered {
		btn.Background(pal.Paper).Border(1, pal.BtnBorder)
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
	case opts.Danger && pressed:
		btn.Background(pal.Danger.Alpha(0.22))
	case opts.Danger && hovered:
		btn.Background(pal.DangerSurface)
	case opts.Active && opts.ActiveStyle == ActiveAccentFlat:
		btn.Background(pal.Selected)
	case opts.Active:
		btn.Background(pal.Selected).Border(1, pal.Accent)
	case pressed:
		btn.Background(pal.Selected)
	case hovered:
		btn.Background(pal.Hover)
	}
	if opts.Disabled {
		btn.Opacity(theme.DisabledOpacity)
	}

	btn.Children(func() {
		var ic theme.IconColors
		switch {
		case opts.Danger && (hovered || pressed || opts.Active):
			ic = pal.MonoIconColors(pal.Danger)
		case opts.Active && opts.ActiveStyle != ActiveOutlined:
			ic = pal.DuoIconColors(pal.Accent)
		case opts.Active || hovered || pressed:
			ic = pal.DuoIconColors(pal.Text)
		case opts.Variant == IconBordered:
			ic = pal.MonoIconColors(pal.Text)
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
	cursor := ui.CursorPointer
	if disabled {
		cursor = ui.CursorDefault
	}
	btn := ui.ButtonBase(c).Height(32).PaddingX(12).Gap(6).AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusControl).Cursor(cursor).Disabled(disabled).DimDisabled(false).Label(label)
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
		if pressed {
			bg = pal.Danger.Alpha(0.26)
		} else if hovered {
			bg = pal.Danger.Alpha(0.18)
		}
		btn.Background(bg).Border(1, pal.Danger.Alpha(0.35))
	case ButtonGhost:
		fg = pal.TextSecondary
		if pressed {
			fg = pal.Text
			btn.Background(pal.Selected)
		} else if hovered {
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
	if disabled {
		btn.Opacity(theme.DisabledOpacity)
	}

	btn.Children(func() {
		if leading != nil {
			ic := pal.MonoIconColors(fg)
			if (style == ButtonSecondary || style == ButtonGhost) && (hovered || pressed) {
				ic = pal.DuoIconColors(fg)
			}
			icons.Draw(c, *leading, 16, ic)
		}
		ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(20).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
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
	track := seg.Track.Height(30).Padding(2).Gap(2).Radius(theme.RadiusControl).
		Background(pal.Paper).Border(1, pal.BtnBorder)
	if id != "" {
		Probe(track, id)
	}
	track.Children(func() {
		for i, opt := range options {
			item := seg.Segment(i).Height(24).PaddingX(10).Center().Radius(6).Border(1, ui.Transparent).Label(opt.Label)
			disabled := item.IsDisabled()
			if disabled {
				item.Cursor(ui.CursorDefault)
			} else {
				item.Cursor(ui.CursorPointer)
			}
			hovered := item.Hovered() && !disabled
			pressed := item.Pressed() && !disabled
			if i == *active {
				item.Background(pal.Selected).Border(1, pal.Accent)
			} else if pressed {
				item.Background(pal.Selected)
			} else if hovered {
				item.Background(pal.Hover)
			}
			if opt.ID != "" {
				Probe(item, opt.ID)
			}
			item.Children(func() {
				col := pal.TextSecondary
				if i == *active || hovered || pressed {
					col = pal.Text
				}
				ui.Text(c, opt.Label).FontSize(theme.FontBody).FixedLineHeight(20).FontWeight(theme.WeightControl).TextColor(col).SingleLine()
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
	cursor := ui.CursorPointer
	if disabled {
		cursor = ui.CursorDefault
	}
	sw := ui.SwitchBase(c, value).Size(38, 22).Shrink(0).Padding(2).AlignItems(ui.Center).Radius(theme.RadiusPill).
		Cursor(cursor).Disabled(disabled).DimDisabled(false).Label(label)
	hovered := sw.Hovered() && !disabled
	pressed := sw.Pressed() && !disabled
	bg := pal.BtnBorder
	if *value {
		switch {
		case pressed:
			bg = pal.AccentPressed
		case hovered:
			bg = pal.AccentHover
		default:
			bg = pal.Accent
		}
	} else {
		switch {
		case pressed:
			bg = pal.BtnBorder.Mix(pal.Text, 0.22)
		case hovered:
			bg = pal.BtnBorder.Mix(pal.Text, 0.12)
		}
	}
	sw.Background(bg)
	if disabled {
		sw.Opacity(theme.DisabledOpacity)
	}
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
		return ui.Box(c).FillWidth().Height(1).Shrink(0).MarginY(3).Background(pal.MenuSeparator)
	}
	if item.ID != "" {
		c.Key(item.ID)
	}
	cursor := ui.CursorPointer
	if item.Disabled {
		cursor = ui.CursorDefault
	}
	row := ui.ButtonBase(c).FillWidth().Height(theme.MenuItemHeight).PaddingX(10).Gap(8).
		AlignItems(ui.Center).Justify(ui.Start).
		Radius(theme.RadiusControl).Cursor(cursor).Disabled(item.Disabled).DimDisabled(false).Label(item.Label)
	hovered := (row.Hovered() || highlighted) && !item.Disabled
	pressed := row.Pressed() && !item.Disabled
	active := hovered || pressed
	fg, iconLine := pal.Text, pal.MenuIcon
	switch {
	case item.Danger && active:
		row.Background(pal.MenuDangerHoverBG)
		fg, iconLine = pal.MenuDangerHoverFG, pal.MenuDangerHoverFG
	case item.Danger:
		fg, iconLine = pal.MenuDangerFG, pal.MenuDangerFG
	case item.Checked:
		row.Background(pal.MenuChecked)
	case pressed:
		row.Background(pal.Selected)
	case hovered:
		row.Background(pal.MenuHover)
	}
	if item.Disabled {
		row.Opacity(theme.DisabledOpacity)
	}
	row.Children(func() {
		if item.Icon != nil {
			var ic theme.IconColors
			switch {
			case item.Danger && active:
				ic = theme.IconColors{Line: pal.MenuDangerHoverFG, Fill: ui.RGBA(255, 255, 255, 0.20), Accent: pal.MenuDangerHoverFG}
			case item.Danger:
				ic = pal.MonoIconColors(iconLine)
			case active || item.Checked:
				ic = pal.DuoIconColors(iconLine)
			default:
				ic = pal.MonoIconColors(iconLine)
			}
			icons.Draw(c, *item.Icon, theme.IconRow, ic)
		}
		ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(20).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if item.Shortcut != "" {
			hintCol := pal.MenuMuted
			if item.Danger && active {
				hintCol = pal.MenuDangerHoverFG
			}
			ShortcutLabel(c, item.Shortcut, ShortcutLabelStyle{
				FontSize:   theme.FontMeta,
				LineHeight: 16,
				Color:      hintCol,
			})
		}
		if item.HasSubmenu {
			chevCol := pal.MenuMuted
			if item.Danger && active {
				chevCol = pal.MenuDangerHoverFG
			}
			icons.Draw(c, icons.TbChevronRight, theme.IconInline, pal.MonoIconColors(chevCol))
		}
		if item.Checked {
			icons.Draw(c, icons.Check, 16, pal.MonoIconColors(pal.Text))
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
		Ease:     ui.CubicBezier(0.16, 1, 0.3, 1),
		Colors:   true,
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
		c.Key(id + "-slot")
	}
	slot := ui.Box(c).Width(0).FillHeight().Shrink(0)
	changed := false
	slot.Children(func() {
		if id != "" {
			c.Key(id)
		}
		h := ui.Box(c).Absolute().Left(-theme.ResizeHitWidth / 2).Top(0).Bottom(0).Width(theme.ResizeHitWidth).Cursor(ui.CursorResizeColumn)
		dx, _, held := h.Dragged()
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
	})
	return changed
}
