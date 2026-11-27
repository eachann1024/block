package theme

import "github.com/egoist/mygo/ui"

// Elevation is the shared shadow scale. Surfaces pick a level instead of
// writing their own Shadow(...) stacks.
type Elevation uint8

const (
	ElevationRaised         Elevation = iota // switch knobs, floating pills, small chips
	ElevationCard                            // main floating paper card resting over the window ground
	ElevationCardHover                       // recessed store card lifting onto crisp paper on hover
	ElevationPopover                         // menus, tooltips, toolbars, popovers
	ElevationPopoverFocused                  // focused floating input popovers with a soft accent halo
	ElevationDialog                          // modal dialogs, search, export, command panels
	ElevationDrawer                          // side drawers sliding over content
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
			if skinActive() {
				return []Shadow{{0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.42)}}
			}
			return []Shadow{{0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.30)}}
		}
		return []Shadow{{0, 1, 3, 0, ui.RGBA(0x20, 0x24, 0x2c, 0.05)}}
	case ElevationCardHover:
		if p.Dark {
			return []Shadow{{0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.32)}}
		}
		return []Shadow{{0, 4, 14, 0, ui.RGBA(0x14, 0x18, 0x20, 0.06)}}
	case ElevationDialog:
		if p.Dark {
			return []Shadow{{0, 3, 10, 0, ui.RGBA(0, 0, 0, 0.24)}, {0, 14, 36, 0, ui.RGBA(0, 0, 0, 0.40)}}
		}
		return []Shadow{{0, 2, 6, 0, black(0.08)}, {0, 12, 32, 0, black(0.14)}}
	case ElevationDrawer:
		return []Shadow{{8, 0, 28, 0, black(0.18)}}
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

// LightboxStageBG is the opaque theater backdrop behind full-window media viewers.
func (p Palette) LightboxStageBG() ui.Color {
	return ui.Hex("#0c0e12")
}

// LightboxChromeBG is the translucent dark floating surface for lightbox toolbars and video controls.
func (p Palette) LightboxChromeBG() ui.Color {
	return ui.RGBA(0x14, 0x17, 0x1e, 0.80)
}

// LightboxChromeBorder is the subtle hairline border around dark floating lightbox chrome.
func (p Palette) LightboxChromeBorder() ui.Color {
	return ui.RGBA(0xff, 0xff, 0xff, 0.14)
}

// LightboxChromeHover is the hover fill for buttons inside dark floating lightbox chrome.
func (p Palette) LightboxChromeHover() ui.Color {
	return ui.RGBA(0xff, 0xff, 0xff, 0.14)
}

// LightboxChromeActive is the pressed/selected fill for buttons inside dark floating lightbox chrome.
func (p Palette) LightboxChromeActive() ui.Color {
	return ui.RGBA(0xff, 0xff, 0xff, 0.22)
}

// LightboxChromeFG is the primary light text/icon color inside dark floating lightbox chrome.
func (p Palette) LightboxChromeFG() ui.Color {
	return ui.Hex("#f4f5f7")
}

// LightboxChromeMutedFG is the secondary light text/icon color inside dark floating lightbox chrome.
func (p Palette) LightboxChromeMutedFG() ui.Color {
	return ui.RGBA(0xf4, 0xf5, 0xf7, 0.68)
}

// LightboxChromeTrack is the slider track background inside dark floating video controls.
func (p Palette) LightboxChromeTrack() ui.Color {
	return ui.RGBA(0xff, 0xff, 0xff, 0.24)
}
