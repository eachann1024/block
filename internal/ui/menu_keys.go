package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// MenuFollowSpring is a critically-damped zero-overshoot spring (~200ms) used when stepping
// through menus, pickers, or tab strips by keyboard so scrolling tracks smoothly without bounce.
var MenuFollowSpring = ui.Animation{
	Spring: ui.SpringOptions{
		Duration: 200 * time.Millisecond,
	},
}

// MenuFollowScrollOptions returns ScrollIntoViewOptions that smoothly center-align the active
// item on both vertical and horizontal axes once selection crosses the container midpoint,
// clamping cleanly at 0 and maxScroll.
func MenuFollowScrollOptions() ui.ScrollIntoViewOptions {
	return ui.ScrollIntoViewOptions{
		Block:     ui.ScrollCenter,
		Inline:    ui.ScrollCenter,
		Animation: &MenuFollowSpring,
	}
}

// RevealActiveItem smoothly glides el toward the center of its enclosing scroll container.
func RevealActiveItem(el ui.Element) {
	if el.Valid() {
		el.ScrollIntoView(MenuFollowScrollOptions())
	}
}

type revealHighlightKey struct{}

type revealHighlightState struct {
	initialized bool
	highlighted bool
}

// RevealOnHighlight center-scrolls el into view on the exact frame highlighted transitions
// from false to true (or centers it immediately if already highlighted on first mount),
// without re-firing on subsequent frames so wheel/trackpad scrolling never fights the animation.
func RevealOnHighlight(el ui.Element, highlighted bool) {
	if !el.Valid() {
		return
	}
	st := ui.Local(el, revealHighlightKey{}, func() revealHighlightState {
		return revealHighlightState{}
	})
	if !st.initialized {
		st.initialized = true
		st.highlighted = highlighted
		if highlighted {
			el.ScrollIntoView(ui.ScrollIntoViewOptions{
				Block:  ui.ScrollCenter,
				Inline: ui.ScrollCenter,
			})
		}
		return
	}
	if highlighted != st.highlighted {
		st.highlighted = highlighted
		if highlighted {
			RevealActiveItem(el)
		}
	}
}

// NavigateMenu handles keyboard navigation (Up/Down/Enter/Right) for a floating menu.
// A negative *cursor means no item is keyboard-selected yet (the default when a menu opens);
// pointer movement inside the panel clears *cursor so hover and keyboard never highlight two rows at once.
// Disabled entries and dividers are skipped in both directions.
func NavigateMenu(panel ui.Element, items []MenuItemSpec, cursor *int) {
	if len(items) == 0 {
		*cursor = -1
		return
	}
	panel.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind == ui.InputPointerMove && *cursor >= 0 {
			*cursor = -1
		}
	})
	active := func(i int) bool { return i >= 0 && i < len(items) && !items[i].Separator && !items[i].Disabled }
	if *cursor < 0 || *cursor >= len(items) || !active(*cursor) {
		*cursor = -1
	}
	step := 0
	switch {
	case panel.OverlayShortcut(0, ui.KeyDown):
		step = 1
	case panel.OverlayShortcut(0, ui.KeyUp):
		step = -1
	}
	if step != 0 {
		n := len(items)
		cur := *cursor
		if cur < 0 {
			if step > 0 {
				cur = -1
			} else {
				cur = n
			}
		}
		*cursor = -1
		for range items {
			cur = (cur + step + n) % n
			if active(cur) {
				*cursor = cur
				break
			}
		}
	}
	suppressHover := *cursor >= 0
	for i := range items {
		items[i].SuppressHover = suppressHover
	}
	if !active(*cursor) {
		return
	}
	item := items[*cursor]
	if item.OnClick == nil {
		return
	}
	if panel.OverlayShortcut(0, ui.KeyEnter) || item.HasSubmenu && panel.OverlayShortcut(0, ui.KeyRight) {
		item.OnClick()
	}
}
