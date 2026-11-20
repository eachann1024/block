package ui

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// MenuInnerMaxHeight returns the inner scroll viewport cap for a MenuSurface
// of outer MaxHeight maxH (accounting for top/bottom MenuPadding and 1px border).
func MenuInnerMaxHeight(maxH float32) float32 {
	return max(maxH-theme.MenuPadding*2-2, 1)
}

// MenuFollowScrollOptions returns ScrollIntoViewOptions that smoothly center-align the active
// item on both vertical and horizontal axes once selection crosses the container midpoint,
// clamping cleanly at 0 and maxScroll.
func MenuFollowScrollOptions() ui.ScrollIntoViewOptions {
	return ui.ScrollIntoViewOptions{
		Block:     ui.ScrollCenter,
		Inline:    ui.ScrollCenter,
		Animation: &SpringTactile,
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

type revealExpandKey struct{}

type revealExpandState struct {
	initialized bool
	expanded    bool
}

// RevealOnExpand scrolls el into view using ScrollNearest on the exact frame
// expanded transitions from false to true, so newly expanded content below a
// disclosure toggle is not clipped below the bottom edge of its scroll viewport.
func RevealOnExpand(el ui.Element, expanded bool) {
	if !el.Valid() {
		return
	}
	st := ui.Local(el, revealExpandKey{}, func() revealExpandState {
		return revealExpandState{}
	})
	if !st.initialized {
		st.initialized = true
		st.expanded = expanded
		return
	}
	if expanded != st.expanded {
		st.expanded = expanded
		if expanded {
			el.ScrollIntoView(ui.ScrollIntoViewOptions{
				Block:     ui.ScrollNearest,
				Animation: JumpScrollAnimation(true),
			})
		}
	}
}

// ObserveMenuPointer clears *cursor to -1 whenever the pointer moves or scrolls
// over panel so hover and mouse-wheel scrolling cleanly take over from keyboard
// selection without consuming InputScroll events needed by child scroll containers.
func ObserveMenuPointer(c *ui.Context, panel ui.Element, cursor *int) {
	if !panel.Valid() || cursor == nil {
		return
	}
	svc := c.Services()
	panel.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerMove:
			if *cursor >= 0 {
				*cursor = -1
				return true
			}
		case ui.InputScroll:
			if *cursor >= 0 {
				*cursor = -1
				svc.Invalidate()
			}
		}
		return false
	})
}

// NavigateMenu handles keyboard navigation (Up/Down/Left/Right/Enter) for a floating menu.
// A negative *cursor means no item is keyboard-selected yet (the default when a menu opens);
// pointer movement or wheel scrolling inside the panel clears *cursor so hover and keyboard
// never highlight two rows at once while leaving scroll events free for the menu's Scroll box.
// Disabled entries and dividers are skipped in both directions.
func NavigateMenu(c *ui.Context, panel ui.Element, items []MenuItemSpec, cursor *int) {
	if len(items) == 0 {
		*cursor = -1
		return
	}
	ObserveMenuPointer(c, panel, cursor)
	active := func(i int) bool { return i >= 0 && i < len(items) && !items[i].Separator && !items[i].Disabled }
	if *cursor < 0 || *cursor >= len(items) || !active(*cursor) {
		*cursor = -1
	}
	down := panel.OverlayShortcut(0, ui.KeyDown)
	right := panel.OverlayShortcut(0, ui.KeyRight)
	up := panel.OverlayShortcut(0, ui.KeyUp)
	left := panel.OverlayShortcut(0, ui.KeyLeft)
	enter := panel.OverlayShortcut(0, ui.KeyEnter)

	curHasSubmenu := active(*cursor) && items[*cursor].HasSubmenu
	step := 0
	switch {
	case down || (right && !curHasSubmenu):
		step = 1
	case up || left:
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
	if enter || (item.HasSubmenu && right) {
		item.OnClick()
	}
}

// menuNavKeys are the keys an open overlay menu navigates with.
var menuNavKeys = []ui.Key{ui.KeyUp, ui.KeyDown, ui.KeyLeft, ui.KeyRight, ui.KeyEnter}

// ReserveMenuNavKeys claims the menu navigation keys for the window this frame.
// MyGo only counts focus and window shortcuts as claimed, so without it a key the
// focused text field leaves scrolls the page under the menu (browser-style arrow
// scrolling) instead of reaching the menu. Overlay shortcuts still route first.
// Call it after the overlays render, so window shortcuts they read are not taken.
func ReserveMenuNavKeys(c *ui.Context) {
	for _, k := range menuNavKeys {
		c.Shortcut(0, k)
	}
}
