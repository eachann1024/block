package ui

import (
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
	// IconBordered is a sidebar-header style button: 32 box, transparent at rest, hover fill, 18 glyph.
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
	ID           string
	Label        string
	Tooltip      string
	Shortcut     string
	TooltipPill  bool
	TooltipAbove bool
	TooltipSpec  *TooltipSpec
	Variant      IconButtonVariant
	Size         float32 // Outer square size override (default from Variant)
	IconSize     float32 // Inner glyph size override (default from Variant)
	Radius       float32 // Corner radius override (default from Variant)
	Active       bool
	// ActiveStyle refines how Active is drawn.
	ActiveStyle IconActiveStyle
	// AlwaysDuo renders Scheme A two-tone semantic fills & accents at rest too.
	AlwaysDuo   bool
	ChromeHover bool
	CardBorder  bool
	RestColor   ui.Color // optional resting icon color override (defaults to TextSecondary)
	Disabled    bool
	Loading     bool
	Danger      bool
	// ForceHover draws the hover state (and plays the glyph's hover animation)
	// without the pointer, e.g. for an intro when a control first appears.
	ForceHover bool
}

func (o IconButtonOptions) metrics() (box, glyph, radius float32) {
	switch o.Variant {
	case IconPlain:
		box, glyph, radius = theme.IconButtonBox, theme.IconButtonGlyph, 6
	case IconBordered:
		box, glyph, radius = theme.HeightControl, theme.IconRow, theme.RadiusMD
	default:
		box, glyph, radius = theme.ChromeBoxSize, theme.IconChrome, theme.RadiusMD
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
	isDisabled := opts.Disabled || opts.Loading
	cursor := ui.CursorPointer
	if isDisabled {
		cursor = ui.CursorDefault
	}
	btn := ui.ButtonBase(c).Size(boxSize, boxSize).Shrink(0).Center().
		Radius(radius).Cursor(cursor).Disabled(isDisabled).DimDisabled(false)
	a11yLabel := opts.Label
	if a11yLabel == "" {
		a11yLabel = opts.Tooltip
	}
	if a11yLabel == "" {
		a11yLabel = opts.ID
	}
	if a11yLabel != "" {
		btn.Label(a11yLabel)
	}
	if opts.TooltipSpec != nil {
		RichTooltip(c, btn, *opts.TooltipSpec)
	} else if opts.Tooltip != "" || opts.Shortcut != "" {
		placement := TooltipBelow
		if opts.TooltipAbove {
			placement = TooltipAbove
		}
		RichTooltip(c, btn, TooltipSpec{
			Pill:      opts.TooltipPill,
			Placement: placement,
			Rows: []TooltipRow{{
				Trigger:  opts.Tooltip,
				Shortcut: opts.Shortcut,
			}},
		})
	}
	hovered := (btn.Hovered() || opts.ForceHover) && !isDisabled
	pressed := btn.Pressed() && !isDisabled

	bgCol := ui.Transparent
	bdCol := ui.Transparent
	hasBorder := opts.Active && opts.ActiveStyle != ActiveAccentFlat
	switch {
	case opts.Danger && pressed:
		bgCol = pal.Danger.Alpha(0.22)
	case opts.Danger && hovered:
		bgCol = pal.DangerSurface
	case opts.Active && opts.ActiveStyle == ActiveAccentFlat:
		bgCol = pal.Selected
	case opts.Active:
		bgCol = pal.Selected
		bdCol = pal.SelectedBorder
		hasBorder = true
	case pressed:
		if opts.ChromeHover {
			bgCol = pal.ShellChrome().Hover
		} else {
			bgCol = pal.Selected
		}
	case hovered:
		if opts.ChromeHover {
			bgCol = pal.ShellChrome().Hover
		} else {
			bgCol = pal.Hover
		}
	}
	AnimateSurfaceColors(btn, bgCol, bdCol, hasBorder)
	TactileButton(btn, hovered, pressed, isDisabled)
	if opts.Disabled && !opts.Loading {
		btn.Opacity(theme.DisabledOpacity)
	}

	btn.Children(func() {
		if opts.Loading {
			sp := ui.Spinner(c).FontSize(glyphSize).TextColor(pal.Accent)
			if a11yLabel != "" {
				sp.Label(a11yLabel)
			}
			return
		}
		var ic theme.IconColors
		switch {
		case opts.Danger && (hovered || pressed || opts.Active):
			ic = pal.MonoIconColors(pal.Danger)
		case opts.Active && opts.ActiveStyle != ActiveOutlined:
			ic = pal.DuoIconColors(pal.Text)
		case opts.Active || hovered || pressed:
			ic = pal.DuoIconColors(pal.Text)
		case opts.AlwaysDuo:
			ic = pal.DuoIconColors(pal.TextSecondary)
		case opts.Variant == IconBordered:
			ic = pal.MonoIconColors(pal.Text)
		case opts.RestColor.A > 0:
			ic = pal.MonoIconColors(opts.RestColor)
		default:
			ic = pal.MonoIconColors(pal.TextSecondary)
		}
		icons.DrawAnimated(c, glyph, glyphSize, ic, hovered || pressed)
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
	ButtonAccentOutline
	ButtonGhost
	ButtonDanger
	ButtonSuccess
)

// ActionButtonOptions configures optional sizing and layout overrides for ActionButtonWithOptions.
type ActionButtonOptions struct {
	ID                 string
	Label              string
	AccessibilityLabel string
	Leading            *icons.Icon
	AlwaysDuo          bool
	Style              ActionButtonStyle
	Disabled           bool
	Loading            bool
	Height             float32
	Width              float32
	MinWidth           float32
	PaddingX           float32
	Grow               float32
	Shrink             *float32
	Truncate           bool
	SecondaryShellBG   bool
	RegularWeight      bool
}

// ActionButton renders a clean Ocean Paper text (+ optional leading icon) button.
func ActionButton(c *ui.Context, id, label string, leading *icons.Icon, style ActionButtonStyle, disabled bool) ui.Element {
	return ActionButtonWithOptions(c, ActionButtonOptions{
		ID:       id,
		Label:    label,
		Leading:  leading,
		Style:    style,
		Disabled: disabled,
	})
}

// ActionButtonWithOptions renders an ActionButton with optional sizing/layout overrides.
func ActionButtonWithOptions(c *ui.Context, opts ActionButtonOptions) ui.Element {
	pal := theme.Ocean()
	if opts.ID != "" {
		c.Key(opts.ID)
	}
	isDisabled := opts.Disabled || opts.Loading
	cursor := ui.CursorPointer
	if isDisabled {
		cursor = ui.CursorDefault
	}
	h := opts.Height
	if h <= 0 {
		h = theme.HeightControl
	}
	padX := opts.PaddingX
	if padX <= 0 && opts.Width <= 0 {
		padX = theme.Space12
	}
	a11yLabel := opts.AccessibilityLabel
	if a11yLabel == "" {
		a11yLabel = opts.Label
	}
	btn := ui.ButtonBase(c).Height(h).Gap(theme.Space6).AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusMD).Cursor(cursor).Disabled(isDisabled).DimDisabled(false).Label(a11yLabel)
	if opts.Width > 0 {
		btn.Width(opts.Width)
	} else if padX > 0 {
		btn.PaddingX(padX)
	}
	if opts.MinWidth > 0 {
		btn.MinWidth(opts.MinWidth)
	}
	if opts.Grow > 0 {
		btn.Grow(opts.Grow)
	}
	if opts.Shrink != nil {
		btn.Shrink(*opts.Shrink)
	}
	if opts.Truncate {
		btn.MaxWidthPercent(100).MinWidth(0)
	}
	hovered := btn.Hovered() && !isDisabled
	pressed := btn.Pressed() && !isDisabled

	var fg, bg, bd ui.Color
	hasBorder := false
	switch opts.Style {
	case ButtonPrimary:
		fg = pal.AccentText
		bg = pal.Accent
		if pressed {
			bg = pal.AccentPressed
		} else if hovered {
			bg = pal.AccentHover
		}
	case ButtonAccentOutline:
		fg = pal.Text
		bg = pal.Selected
		if pressed {
			bg = pal.Selected.Mix(pal.Text, 0.08)
		} else if hovered {
			bg = pal.Selected.Mix(pal.Text, 0.04)
		}
		bd = pal.SelectedBorder
		hasBorder = true
	case ButtonDanger:
		fg = pal.Danger
		bg = pal.DangerSurface
		if pressed {
			bg = pal.Danger.Alpha(0.26)
		} else if hovered {
			bg = pal.Danger.Alpha(0.18)
		}
		bd = pal.Danger.Alpha(0.35)
		hasBorder = true
	case ButtonSuccess:
		fg = pal.Success
		bg = pal.SuccessSurface
		if pressed {
			bg = pal.SuccessSurface.Mix(pal.Success, 0.14)
		} else if hovered {
			bg = pal.SuccessSurface.Mix(pal.Success, 0.08)
		}
		bd = pal.Success.Alpha(0.38)
		hasBorder = true
	case ButtonGhost:
		fg = pal.TextSecondary
		if pressed {
			fg = pal.Text
			bg = pal.Selected
		} else if hovered {
			fg = pal.Text
			bg = pal.Hover
		}
	default:
		fg = pal.Text
		bg = pal.Card
		if opts.SecondaryShellBG {
			bg = pal.Shell
		}
		if pressed {
			bg = pal.Selected
		} else if hovered {
			bg = pal.Hover
		}
		bd = pal.BtnBorder
		hasBorder = true
	}
	AnimateSurfaceColors(btn, bg, bd, hasBorder)
	TactileButton(btn, hovered, pressed, isDisabled)
	if opts.Disabled && !opts.Loading {
		btn.Opacity(theme.DisabledOpacity)
	} else if opts.Loading {
		btn.Opacity(0.92)
	}

	weight := theme.WeightControl
	if opts.RegularWeight {
		weight = theme.WeightRegular
	}
	btn.Children(func() {
		if opts.Loading {
			spCol := fg
			if opts.Style == ButtonSecondary || opts.Style == ButtonGhost {
				spCol = pal.Accent
			}
			sp := ui.Spinner(c).FontSize(theme.IconInline).TextColor(spCol)
			if a11yLabel != "" {
				sp.Label(a11yLabel)
			}
		} else if opts.Leading != nil {
			ic := pal.MonoIconColors(fg)
			if opts.Style == ButtonSecondary || opts.Style == ButtonGhost || opts.Style == ButtonAccentOutline {
				if hovered || pressed {
					ic = pal.DuoIconColors(fg)
				} else if opts.AlwaysDuo {
					ic = pal.DuoIconColors(pal.TextSecondary)
				}
			}
			icons.DrawAnimated(c, *opts.Leading, theme.IconAction, ic, hovered || pressed)
		}
		txt := ui.Text(c, opts.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(weight).TextColor(fg).SingleLine()
		if opts.Truncate {
			txt.MinWidth(0).Shrink(1).Ellipsis("…")
		}
	})
	if opts.ID != "" {
		Probe(btn, opts.ID)
	}
	return btn
}

type SegmentOption struct {
	ID       string
	Label    string
	Disabled bool
}

// SegmentedControlOptions configures optional layout overrides for SegmentedControlWithOptions.
type SegmentedControlOptions struct {
	FillWidth     bool
	TrackHeight   float32
	TrackPadding  float32
	SegmentHeight float32
	SegmentRadius float32
	ShellTrack    bool
	CardTrack     bool
	// Quiet: transparent track with a hairline ring and FontSmall labels (settings pages).
	Quiet bool
}

// SegmentedControl renders an Ocean Paper segmented radio track.
func SegmentedControl(c *ui.Context, id string, active *int, options []SegmentOption) bool {
	return SegmentedControlWithOptions(c, id, active, options, SegmentedControlOptions{})
}

// SegmentedControlWithOptions renders a SegmentedControl with optional layout overrides.
func SegmentedControlWithOptions(c *ui.Context, id string, active *int, options []SegmentOption, opts SegmentedControlOptions) bool {
	if len(options) == 0 {
		return false
	}
	pal := theme.Ocean()
	prev := *active
	if id != "" {
		c.Key(id)
	}
	trackH := opts.TrackHeight
	if trackH <= 0 {
		trackH = 30
	}
	trackPad := opts.TrackPadding
	if trackPad <= 0 {
		trackPad = theme.Space2
	}
	segH := opts.SegmentHeight
	if segH <= 0 {
		segH = 24
	}
	segRad := opts.SegmentRadius
	if segRad <= 0 {
		segRad = theme.RadiusSM
	}
	trackBG := pal.Paper
	if opts.CardTrack {
		trackBG = pal.Card
	} else if opts.ShellTrack {
		trackBG = pal.Shell
	}
	labelSize, labelLine := theme.FontBody, theme.LineBody
	if opts.Quiet {
		trackBG = ui.Transparent
		labelSize, labelLine = theme.FontSmall, theme.LineSmall
	}
	seg := ui.SegmentedBase(c, active, len(options))
	track := seg.Track.Height(trackH).Padding(trackPad).Gap(theme.Space2).AlignItems(ui.Center).Radius(theme.RadiusMD).
		Background(trackBG)
	if opts.FillWidth {
		track.FillWidth()
	}
	if id != "" {
		Probe(track, id)
	}
	track.Children(func() {
		for i, opt := range options {
			item := seg.Segment(i).Height(segH).Center().Radius(segRad).
				Disabled(opt.Disabled).Label(opt.Label)
			if opts.FillWidth {
				item.Grow(1).MinWidth(0)
			} else {
				item.PaddingX(theme.Space10)
			}
			disabled := item.IsDisabled() || opt.Disabled
			if disabled {
				item.Cursor(ui.CursorDefault).Opacity(0.4)
			} else {
				item.Cursor(ui.CursorPointer)
			}
			hovered := item.Hovered() && !disabled
			pressed := item.Pressed() && !disabled
			isAct := i == *active
			bg := ui.Transparent
			if isAct {
				bg = pal.Selected
			} else if pressed {
				bg = pal.Selected
			} else if hovered {
				bg = pal.Hover
			}
			AnimateSurfaceColors(item, bg, ui.Transparent, false)
			TactileCard(item, hovered, pressed, isAct, disabled)
			if opt.ID != "" {
				Probe(item, opt.ID)
			}
			item.Children(func() {
				col := pal.TextSecondary
				if isAct || hovered || pressed {
					col = pal.Text
				}
				ui.Text(c, opt.Label).FontSize(labelSize).FixedLineHeight(labelLine).FontWeight(theme.WeightControl).TextColor(col).SingleLine()
			})
		}
	})
	return *active != prev
}

// TogglePill renders the global Block Pluck Switch at compact size.
// Kept as a compatibility wrapper so all existing callers automatically use
// the unified SwitchWithOptions implementation.
func TogglePill(c *ui.Context, id, label string, value *bool, disabled bool) bool {
	return SwitchWithOptions(c, SwitchOptions{
		ID:       id,
		Label:    label,
		Value:    value,
		Size:     SwitchCompact,
		Disabled: disabled,
	})
}

// MenuItemSpec describes a row in an Ocean Paper floating menu.
type MenuItemSpec struct {
	ID            string
	Label         string
	Shortcut      string
	Hint          string
	Icon          *icons.Icon
	CustomIcon    func(c *ui.Context, size float32, colors theme.IconColors)
	Checked       bool
	Danger        bool
	Separator     bool
	Disabled      bool
	HasSubmenu    bool
	SuppressHover bool
	OnClick       func()
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
		Radius(theme.RadiusMD).Cursor(cursor).Disabled(item.Disabled).DimDisabled(false).Label(item.Label)
	RevealOnHighlight(row, highlighted && !item.Disabled)
	rawHover := row.Hovered() && !item.SuppressHover
	hovered := (rawHover || highlighted) && !item.Disabled
	pressed := row.Pressed() && !item.Disabled
	active := hovered || pressed
	fg, iconLine := pal.Text, pal.MenuIcon
	bg := ui.Transparent
	switch {
	case item.Danger && active:
		bg = pal.MenuDangerHoverBG
		fg, iconLine = pal.MenuDangerHoverFG, pal.MenuDangerHoverFG
	case item.Danger:
		fg, iconLine = pal.MenuDangerFG, pal.MenuDangerFG
	case pressed:
		bg = pal.Selected
	case hovered:
		bg = pal.MenuHover
	}
	AnimateSurfaceColors(row, bg, ui.Transparent, false)
	if item.Disabled {
		row.Opacity(theme.DisabledOpacity)
	}
	row.Children(func() {
		if item.Icon != nil || item.CustomIcon != nil {
			var ic theme.IconColors
			switch {
			case item.Danger && active:
				ic = theme.IconColors{Line: pal.MenuDangerHoverFG, Fill: pal.MenuDangerHoverFG.Alpha(0.20), Accent: pal.MenuDangerHoverFG}
			case item.Danger:
				ic = pal.MonoIconColors(iconLine)
			case active || item.Checked:
				ic = pal.DuoIconColors(iconLine)
			default:
				ic = pal.MonoIconColors(iconLine)
			}
			if item.Icon != nil {
				icons.Draw(c, *item.Icon, theme.IconRow, ic)
			} else {
				item.CustomIcon(c, theme.IconRow, ic)
			}
		}
		ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if item.Hint != "" {
			hintCol := pal.MenuMuted
			if item.Danger && active {
				hintCol = pal.MenuDangerHoverFG
			}
			ui.Text(c, item.Hint).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
				TextColor(hintCol).SingleLine().Shrink(0)
		} else if item.Shortcut != "" {
			hintCol := pal.MenuMuted
			if item.Danger && active {
				hintCol = pal.MenuDangerHoverFG
			}
			ShortcutLabel(c, item.Shortcut, ShortcutLabelStyle{
				FontSize:   theme.FontMeta,
				LineHeight: theme.LineMeta,
				Color:      hintCol,
				IsShortcut: true,
			})
		}
		if item.HasSubmenu {
			chevCol := pal.MenuMuted
			if item.Danger && active {
				chevCol = pal.MenuDangerHoverFG
			}
			icons.DrawAnimated(c, icons.TbChevronRight, theme.IconInline, pal.MonoIconColors(chevCol), active)
		}
		if item.Checked {
			icons.Draw(c, icons.Check, theme.IconMenuCheck, pal.MonoIconColors(pal.Accent))
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

// InlineConfirmOptions configures an in-place two-step confirmation bar inside a row.
type InlineConfirmOptions struct {
	ID           string
	ConfirmLabel string
	CancelLabel  string
	Danger       bool
}

// InlineConfirm renders compact [Cancel] [Confirm] actions in-place at the trailing
// end of a list/menu row (confirm sits at the trailing edge under the pointer).
func InlineConfirm(c *ui.Context, opts InlineConfirmOptions) (confirmed, cancelled bool) {
	pal := theme.Ocean()
	cancelText := opts.CancelLabel
	if cancelText == "" {
		cancelText = theme.Tr("取消", "Cancel")
	}
	confirmText := opts.ConfirmLabel
	if confirmText == "" {
		confirmText = theme.Tr("确认", "Confirm")
	}
	if opts.ID != "" {
		c.Key(opts.ID + "-confirm-group")
	}
	ui.Row(c).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
		cancelID := ""
		if opts.ID != "" {
			cancelID = opts.ID + "-cancel"
			c.Key(cancelID)
		}
		cancelBtn := ui.ButtonBase(c).Height(theme.TreeMoreBtnSize).PaddingX(7).Shrink(0).Center().
			Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(cancelText)
		cancelHover := cancelBtn.Hovered()
		cancelPress := cancelBtn.Pressed()
		cancelFG := pal.TextSecondary
		switch {
		case cancelPress:
			cancelBtn.Background(pal.Selected)
			cancelFG = pal.Text
		case cancelHover:
			cancelBtn.Background(pal.MenuHover)
			cancelFG = pal.Text
		}
		cancelBtn.Children(func() {
			ui.Text(c, cancelText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineMeta).
				FontWeight(theme.WeightControl).TextColor(cancelFG).SingleLine()
		})
		if cancelID != "" {
			Probe(cancelBtn, cancelID)
		}
		cancelled = cancelBtn.Clicked()

		confirmID := ""
		if opts.ID != "" {
			confirmID = opts.ID + "-confirm"
			c.Key(confirmID)
		}
		confirmBtn := ui.ButtonBase(c).Height(theme.TreeMoreBtnSize).PaddingX(8).Shrink(0).Center().
			Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(confirmText)
		confirmHover := confirmBtn.Hovered()
		confirmPress := confirmBtn.Pressed()
		var confirmFG ui.Color
		if opts.Danger {
			switch {
			case confirmPress:
				confirmBtn.Background(pal.Danger)
				confirmFG = pal.MenuDangerHoverFG
			case confirmHover:
				confirmBtn.Background(pal.MenuDangerHoverBG)
				confirmFG = pal.MenuDangerHoverFG
			default:
				confirmBtn.Background(pal.DangerSurface)
				confirmFG = pal.Danger
			}
		} else {
			confirmFG = pal.AccentText
			bg := pal.Accent
			if confirmPress {
				bg = pal.AccentPressed
			} else if confirmHover {
				bg = pal.AccentHover
			}
			confirmBtn.Background(bg)
		}
		confirmBtn.Children(func() {
			ui.Text(c, confirmText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineMeta).
				FontWeight(theme.WeightControl).TextColor(confirmFG).SingleLine()
		})
		if confirmID != "" {
			Probe(confirmBtn, confirmID)
		}
		confirmed = confirmBtn.Clicked()
	})
	return confirmed, cancelled
}

// PopupTransition returns the short enter/exit transition for menus and popovers.
func PopupTransition() ui.ElementTransition {
	return PopoverSpringTransition()
}

// MenuSurface styles el as a dropdown/context-menu surface: menu fill, RadiusXL,
// 4px padding, 2px row gap, a hairline ring, popover elevation and short popup motion.
func MenuSurface(el ui.Element) ui.Element {
	pal := theme.Ocean()
	el.Background(pal.Menu).Radius(theme.RadiusXL).Padding(theme.MenuPadding).Gap(theme.MenuGap).
		Border(1, pal.MenuBorder.Alpha(0.6)).Cursor(ui.CursorDefault).
		Transition(PopupTransition())
	return Elevate(el, theme.ElevationPopover)
}

// VerticalResizeHandle renders a vertical splitter adjusting width within [minW, maxW].
func VerticalResizeHandle(c *ui.Context, id string, width *float32, minW, maxW float32, invert bool) bool {
	return ResizeHandle(c, id, width, minW, maxW, invert, 0)
}

// ResizeHandle is VerticalResizeHandle with a double-click reset width (0 = no reset).
// Matches thinkly-fast ui/resize_handle.rs: 8px hit centred on the edge, 2px line,
// menu-border on hover, accent while dragging.
func ResizeHandle(c *ui.Context, id string, width *float32, minW, maxW float32, invert bool, reset float32) bool {
	return resizeHandle(c, id, width, minW, maxW, invert, reset, true)
}

// SeamlessResizeHandle is ResizeHandle without an idle 1px divider line, used beside
// floating cards that already carry their own 1px rounded contour.
func SeamlessResizeHandle(c *ui.Context, id string, width *float32, minW, maxW, reset float32) bool {
	return resizeHandle(c, id, width, minW, maxW, false, reset, false)
}

func resizeHandle(c *ui.Context, id string, width *float32, minW, maxW float32, invert bool, reset float32, idleLine bool) bool {
	pal := theme.Ocean()
	if id != "" {
		c.Key(id + "-slot")
	}
	slot := ui.Box(c).Width(0).FillHeight().Shrink(0)
	if invert {
		slot.ZIndex(1) // Keep the inward stroke above the right panel background.
	}
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
			var lineW float32
			var color ui.Color
			if idleLine {
				lineW, color = 1, pal.SidebarBorder
			}
			switch {
			case held:
				lineW, color = theme.ResizeLineWidth, pal.Accent
			case hovered:
				lineW, color = theme.ResizeLineWidth, pal.MenuBorder
			}
			if lineW <= 0 {
				return
			}
			edge := r.X + r.W*0.5
			lineX := edge - lineW/2
			if invert {
				// The right panel clips to its left edge while opening.
				lineX = edge
			}
			y, lh := r.Y, r.H
			var rad float32
			if !idleLine {
				y = r.Y + 8
				lh = max(r.H-16, 16)
				rad = lineW * 0.5
			}
			p.Fill(ui.Rect{X: lineX, Y: y, W: lineW, H: lh}, color, rad)
		})
		if id != "" {
			Probe(h, id)
		}
	})
	return changed
}
