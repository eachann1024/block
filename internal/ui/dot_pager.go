package ui

import (
	"fmt"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// DotPagerOptions configures a minimalist dot/pill slot indicator (thinkly-fast ui/dot_pager.rs).
type DotPagerOptions struct {
	PagerID     string
	IDPrefix    string
	Count       int
	Active      int
	Tooltip     string
	SlotTooltip func(index int) *TooltipSpec
	OnSelect    func(index int)
}

// DotPager renders a centered horizontal row of clickable slot indicators where the
// active slot expands to an 18x6 accent pill and inactive slots render as 6x6 dots.
func DotPager(c *ui.Context, opts DotPagerOptions) ui.Element {
	pal := theme.Ocean()
	idleCol := ui.Hex("#c6cfd9")
	if theme.IsDark() {
		idleCol = ui.Hex("#384152")
	}
	hoverCol := pal.TextSecondary

	pagerID := opts.PagerID
	if pagerID == "" {
		pagerID = opts.IDPrefix + "-pager"
	}
	pager := ui.Row(c.Key(pagerID)).Gap(2).AlignItems(ui.Center)
	Probe(pager, pagerID)

	pager.Children(func() {
		for i := 0; i < opts.Count; i++ {
			slot := i
			isActive := slot == opts.Active
			dotID := fmt.Sprintf("%s-%d", opts.IDPrefix, slot)
			btn := ui.ButtonBase(c.Key(dotID)).Size(22, 24).Center().
				Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
				Label(fmt.Sprintf("Slot %d", slot+1))
			if opts.SlotTooltip != nil {
				if spec := opts.SlotTooltip(slot); spec != nil {
					RichTooltip(c, btn, *spec)
				}
			} else if opts.Tooltip != "" {
				Tooltip(c, btn, opts.Tooltip)
			}
			hovered := btn.Hovered()
			btn.Children(func() {
				w := float32(6)
				col := idleCol
				if isActive {
					w = 18
					col = pal.Accent
				} else if hovered {
					col = hoverCol
				}
				ui.Box(c).Size(w, 6).Radius(theme.RadiusPill).Background(col)
			})
			Probe(btn, dotID)
			if btn.Clicked() && opts.OnSelect != nil {
				opts.OnSelect(slot)
			}
		}
	})
	return pager
}
