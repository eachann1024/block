package ui

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// Elevate applies the shared shadow stack for level to el.
func Elevate(el ui.Element, level theme.Elevation) ui.Element {
	for _, s := range theme.Ocean().Shadows(level) {
		el.Shadow(s.X, s.Y, s.Blur, s.Spread, s.Color)
	}
	return el
}

// CardSurface styles el as an Ocean Paper card: card fill, hairline ring,
// RadiusLG corners and the standard card padding and row gap.
func CardSurface(el ui.Element) ui.Element {
	pal := theme.Ocean()
	el.Padding(theme.CardPadY, theme.CardPadX, theme.CardPadY, theme.CardPadX).Gap(theme.CardGap).
		Radius(theme.RadiusLG).Background(pal.Card).Border(1, pal.Hairline)
	return el
}

// Card renders a full-width card column holding children.
func Card(c *ui.Context, children func()) ui.Element {
	return CardSurface(ui.Column(c).FillWidth().MinWidth(0)).Children(children)
}

// PopoverSurface styles el as a floating popover (link card, image menu,
// comment composer): menu fill and border, RadiusLG, popover elevation and motion.
func PopoverSurface(el ui.Element) ui.Element {
	pal := theme.Ocean()
	el.Background(pal.Menu).Radius(theme.RadiusLG).Border(1, pal.MenuBorder).Transition(PopupTransition())
	return Elevate(el, theme.ElevationPopover)
}

// FocusedPopoverSurface styles el as a focused floating input popover (such as
// the annotation composer): menu fill, accent border, RadiusLG, popover elevation and motion.
func FocusedPopoverSurface(el ui.Element) ui.Element {
	pal := theme.Ocean()
	el.Background(pal.Menu).Radius(theme.RadiusLG).Border(1, pal.Accent).Transition(PopupTransition())
	return Elevate(el, theme.ElevationPopover)
}
