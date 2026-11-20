package theme

import "github.com/egoist/mygo/ui"

// Elevation is the shared shadow scale. Surfaces pick a level instead of
// writing their own Shadow(...) stacks.
type Elevation uint8

const (
	ElevationRaised  Elevation = iota // switch knobs, floating pills, small chips
	ElevationCard                     // main floating paper card resting over the window ground
	ElevationCardHover                // recessed store card lifting onto crisp paper on hover
	ElevationPopover        // menus, tooltips, toolbars, popovers
	ElevationPopoverFocused // focused floating input popovers with a soft accent halo
	ElevationDialog         // modal dialogs, search, export, command panels
	ElevationDrawer                   // side drawers sliding over content
	ElevationTarotCard                // tarot cards hovering over the midnight table
	ElevationTarotSheet               // tarot parchment sheet over the crosshatch desk
)

// Shadow is one layer of an elevation stack.
type Shadow struct {
	X, Y, Blur, Spread float32
	Color              ui.Color
}

// Shadows returns the layered shadow stack for an elevation level. Dark mode
// doubles the opacity so surfaces still separate from graphite backgrounds.
func (p Palette) Shadows(e Elevation) []Shadow {
	k := float32(1)
	if p.Dark {
		k = 2
	}
	black := func(a float32) ui.Color { return ui.RGBA(0, 0, 0, min(a*k, 0.6)) }
	switch e {
	case ElevationRaised:
		return []Shadow{{0, 1, 2, 0, black(0.12)}}
	case ElevationCard:
		if p.Dark {
			return []Shadow{{0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.22)}}
		}
		return []Shadow{{0, 1, 3, 0, ui.RGBA(0x20, 0x24, 0x2c, 0.05)}}
	case ElevationCardHover:
		if p.Dark {
			return []Shadow{{0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.32)}}
		}
		return []Shadow{{0, 4, 14, 0, ui.RGBA(0x14, 0x18, 0x20, 0.06)}}
	case ElevationDialog:
		return []Shadow{{0, 2, 6, 0, black(0.08)}, {0, 12, 32, 0, black(0.14)}}
	case ElevationDrawer:
		return []Shadow{{8, 0, 28, 0, black(0.18)}}
	case ElevationTarotCard:
		return []Shadow{{0, 18, 36, 0, ui.RGBA(2, 6, 14, 0.58)}}
	case ElevationTarotSheet:
		return []Shadow{
			{0, 1, 0, 0, ui.Hex("#d9cdaf")},
			{0, 2, 0, 0, ui.Hex("#8f7f5c")},
			{0, 24, 56, 0, ui.RGBA(2, 6, 14, 0.55)},
		}
	case ElevationPopoverFocused:
		ringAlpha := float32(0.14)
		if p.Dark {
			ringAlpha = 0.22
		}
		return []Shadow{
			{0, 0, 0, 3, p.Accent.Alpha(ringAlpha)},
			{0, 1, 2, 0, black(0.05)},
			{0, 6, 20, 0, black(0.09)},
		}
	default:
		return []Shadow{{0, 1, 2, 0, black(0.05)}, {0, 6, 20, 0, black(0.09)}}
	}
}

// Scrim is the dimming layer behind modal dialogs and drawers.
func (p Palette) Scrim() ui.Color {
	if p.Dark {
		return ui.RGBA(0, 0, 0, 0.45)
	}
	return ui.RGBA(0x20, 0x24, 0x2c, 0.18)
}

// LightboxScrim is the deeper dimming layer behind full-window media previews.
func (p Palette) LightboxScrim() ui.Color {
	if p.Dark {
		return ui.RGBA(0, 0, 0, 0.74)
	}
	return ui.RGBA(0x14, 0x17, 0x1e, 0.56)
}

// ImageOverlayBadgeBG returns a translucent neutral dark pill fill for badges
// and close controls rendered over arbitrary user images.
func (p Palette) ImageOverlayBadgeBG() ui.Color {
	if p.Dark {
		return ui.RGBA(0x12, 0x14, 0x18, 0.72)
	}
	return ui.RGBA(0x18, 0x1b, 0x22, 0.66)
}

// ImageOverlayBadgeHoverBG is the hovered fill for badges rendered over images.
func (p Palette) ImageOverlayBadgeHoverBG() ui.Color {
	return ui.RGBA(0x12, 0x14, 0x18, 0.88)
}

// ImageOverlayBadgeFG is the high-contrast foreground for icons on ImageOverlayBadgeBG.
func (p Palette) ImageOverlayBadgeFG() ui.Color {
	return ui.Hex("#f4f5f7")
}
