package ui

import "github.com/egoist/mygo/ui"

// NavigateMenu returns the highlighted row and invokes the same callback used by
// a pointer click. Disabled entries and dividers are skipped in both directions.
func NavigateMenu(panel ui.Element, items []MenuItemSpec, cursor *int) {
	if len(items) == 0 {
		return
	}
	active := func(i int) bool { return !items[i].Separator && !items[i].Disabled }
	if *cursor < 0 || *cursor >= len(items) {
		*cursor = 0
	}
	step := 0
	switch {
	case panel.OverlayShortcut(0, ui.KeyDown):
		step = 1
	case panel.OverlayShortcut(0, ui.KeyUp):
		step = -1
	}
	if step != 0 || !active(*cursor) {
		if step == 0 {
			step = 1
		}
		for range items {
			*cursor = (*cursor + step + len(items)) % len(items)
			if active(*cursor) {
				break
			}
		}
	}
	item := items[*cursor]
	if !active(*cursor) || item.OnClick == nil {
		return
	}
	if panel.OverlayShortcut(0, ui.KeyEnter) || item.HasSubmenu && panel.OverlayShortcut(0, ui.KeyRight) {
		item.OnClick()
	}
}
