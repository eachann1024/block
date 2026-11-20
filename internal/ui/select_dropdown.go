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
	// MenuWidth, when set, is the menu's minimum width (default 220 or the trigger width).
	MenuWidth float32
	// Compact sizes the trigger to its label instead of the 180px form-field minimum.
	Compact bool
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
		borderCol = pal.Placeholder
	}

	if id != "" {
		c.Key(id)
	}
	minW, shrink := float32(180), float32(1)
	if state.Compact {
		minW, shrink = 0, 0
	}
	btn := ui.ButtonBase(c).
		MinWidth(minW).
		MaxWidth(theme.AIPanelDefaultWidth).
		Shrink(shrink).
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
		bg = pal.Shell.Mix(pal.Text, 0.09)
	} else if hovered {
		bg = pal.Shell.Mix(pal.Text, 0.05)
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
	// The menu matches the trigger width so both edges line up instead of overhanging it.
	if picked := selectMenu(c, id, options, state, curVal, 0); picked >= 0 {
		if nextVal := options[picked].Value; nextVal != *value {
			*value = nextVal
			changed = true
		}
	}
	return changed
}

// SelectMenu renders the floating option menu below state.TriggerRect while state.Open is set,
// so custom triggers can reuse DropdownSelect's popover. It returns the picked index, or -1.
func SelectMenu(c *ui.Context, id string, options []SelectOption, state *DropdownSelectState, curVal string) int {
	return selectMenu(c, id, options, state, curVal, 220)
}

func selectMenu(c *ui.Context, id string, options []SelectOption, state *DropdownSelectState, curVal string, minW float32) int {
	pickedIdx := -1
	if !state.Open || state.TriggerRect.W <= 0 || state.TriggerRect.H <= 0 || len(options) == 0 {
		return pickedIdx
	}
	winW, winH := c.Size()
	if winW <= 0 {
		winW = 1100
	}
	if winH <= 0 {
		winH = 760
	}
	popW := max(state.TriggerRect.W, minW, state.MenuWidth)
	popW = min(popW, max(winW-theme.Space16, 160))
	if state.MenuWidth <= 0 {
		popW = min(popW, theme.MenuMaxWidth)
	}

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
			state.Open = false
		} else {
			pickedIdx = -1
			if backdrop.Clicked() {
				state.Open = false
			}
		}
	})
	return pickedIdx
}
