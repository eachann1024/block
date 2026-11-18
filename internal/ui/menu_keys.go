package ui

import "github.com/egoist/mygo/ui"

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
