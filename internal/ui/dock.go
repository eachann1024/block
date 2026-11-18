package ui

import "github.com/egoist/mygo/ui"

// DockPanel renders a docked side panel whose layout width springs open and
// shut, so the neighbouring columns glide with it instead of jumping. While it
// moves, content keeps its full width and slides in with the panel's inner
// edge; at rest the panel takes its content's natural width. Call it every
// frame, open or not, so a closing panel can finish leaving.
func DockPanel(c *ui.Context, key string, open bool, width float32, fromRight bool, content func()) {
	target := float32(0)
	if open {
		target = 1
	}
	slot := ui.Row(c.Key(key)).Shrink(0).FillHeight()
	p := RevealTo(slot, "dockOpen", target)
	if almostZero(p-target, 0.002) {
		p = target
	}
	if p <= 0 {
		slot.Width(0)
		return
	}
	// The content keeps one element path at rest and in motion, so its state
	// (scroll, focus, hover) survives the panel opening and closing.
	moving := p < 1
	shown := width * p
	if moving {
		slot.Width(shown).Clip()
	}
	slot.Children(func() {
		inner := ui.Row(c.Key(key + "-content")).Shrink(0).FillHeight()
		if moving {
			inner.Width(width)
			if !fromRight {
				inner.Translate(shown-width, 0)
			}
		}
		inner.Children(content)
	})
}
