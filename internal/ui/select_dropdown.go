package ui

import (
	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// SelectOption describes one selectable entry in DropdownSelect.
type SelectOption struct {
	ID    string
	Value string
	Label string
	Hint  string
}

// DropdownSelectState holds transient popover state for a single DropdownSelect control.
type DropdownSelectState struct {
	Open        bool
	TriggerRect ui.Rect
	Scroll      ui.ScrollState
	Cursor      int
}

// DropdownSelect renders an Ocean Paper dropdown trigger button and viewport-aware floating menu.
func DropdownSelect(
	c *ui.Context,
	id string,
	value *string,
	options []SelectOption,
	state *DropdownSelectState,
	placeholder string,
) bool {
	if state == nil || value == nil {
		return false
	}
	pal := theme.Ocean()
	curVal := *value
	activeLabel := placeholder
	for _, opt := range options {
		if opt.Value == curVal {
			activeLabel = opt.Label
			break
		}
	}
	if activeLabel == "" && len(options) > 0 {
		activeLabel = options[0].Label
	}

	open := state.Open
	borderCol := pal.BtnBorder
	if open {
		borderCol = pal.Accent
	}

	if id != "" {
		c.Key(id)
	}
	btn := ui.ButtonBase(c).
		MinWidth(180).
		MaxWidth(theme.AIPanelDefaultWidth).
		Shrink(1).
		Height(theme.HeightControl).
		Padding(0, theme.Space10, 0, theme.Space12).
		AlignItems(ui.Center).
		Justify(ui.SpaceBetween).
		Gap(theme.Space8).
		Radius(theme.RadiusMD).
		Cursor(ui.CursorPointer).
		Label(activeLabel)

	hovered := btn.Hovered()
	pressed := btn.Pressed()
	bg := pal.Shell
	if pressed {
		bg = pal.Selected
	} else if hovered {
		bg = pal.Hover
	}
	AnimateSurfaceColors(btn, bg, borderCol, true)
	TactileCard(btn, hovered, pressed, open, false)

	btn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		state.TriggerRect = r
		if id != "" {
			RecordBounds(id, r)
		}
	})

	btn.Children(func() {
		ui.Text(c, activeLabel).
			FontSize(theme.FontBody).
			FixedLineHeight(theme.LineBody).
			TextColor(pal.Text).
			Grow(1).
			MinWidth(0).
			SingleLine().
			Ellipsis("…")
		glyph := icons.ChevronDown
		if open {
			glyph = icons.ChevronUp
		}
		ic := pal.MonoIconColors(pal.TextSecondary)
		if open || hovered || pressed {
			ic = pal.DuoIconColors(pal.Text)
		}
		icons.DrawAnimated(c, glyph, theme.IconInline, ic, open || hovered || pressed)
	})
	if id != "" {
		Probe(btn, id)
	}

	if btn.Clicked() {
		state.Open = !state.Open
		state.Cursor = -1
	}

	changed := false
	if state.Open && state.TriggerRect.W > 0 && state.TriggerRect.H > 0 && len(options) > 0 {
		winW, winH := c.Size()
		if winW <= 0 {
			winW = 1100
		}
		if winH <= 0 {
			winH = 760
		}
		popW := max(state.TriggerRect.W, float32(220))
		popW = min(popW, min(theme.MenuMaxWidth, max(winW-theme.Space16, 160)))

		capH := float32(280)
		spec := PlaceBelow(state.TriggerRect).
			WithAlignX(AlignX{Kind: AlignRight}).
			WithGap(theme.Space6).
			WithMargin(theme.Space8).
			WithReserveHeight(min(float32(len(options))*30+theme.Space12, capH))
		maxH := MaxHeightForSpec(spec, winW, winH, capH)
		estH := min(float32(len(options))*(theme.MenuItemHeight+theme.MenuGap)+theme.MenuPadding*2, maxH)
		x, y := ResolveOrigin(spec, popW, estH, winW, winH)

		ui.Overlay(c, func() {
			backdrop := ui.Box(c.Key(id + "-select-backdrop")).Absolute().Left(0).Top(0).Right(0).Bottom(0)
			c.Key(id + "-select-menu")
			panel := MenuSurface(
				Scroll(c, &state.Scroll).
					Absolute().
					Left(x).
					Top(y).
					Width(popW).
					MaxHeight(maxH),
			)
			ObserveMenuPointer(c, panel, &state.Cursor)
			if id != "" {
				Probe(panel, id+"-menu")
			}

			pickedIdx := -1
			panel.Children(func() {
				for idx, opt := range options {
					i := idx
					o := opt
					rowID := o.ID
					if rowID == "" && id != "" {
						rowID = id + "-opt-" + o.Value
					}
					MenuRow(c, MenuItemSpec{
						ID:      rowID,
						Label:   o.Label,
						Hint:    o.Hint,
						Checked: o.Value == curVal,
						OnClick: func() {
							pickedIdx = i
						},
					}, state.Cursor == i)
				}
			})

			if pickedIdx >= 0 && pickedIdx < len(options) {
				nextVal := options[pickedIdx].Value
				state.Open = false
				if nextVal != *value {
					*value = nextVal
					changed = true
				}
			} else if backdrop.Clicked() {
				state.Open = false
			}
		})
	}
	return changed
}
