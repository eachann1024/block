package theme

import "github.com/egoist/mygo/ui"

// Elevation is the shared shadow scale. Surfaces pick a level instead of
// writing their own Shadow(...) stacks.
type Elevation uint8

const (
	ElevationRaised  Elevation = iota // switch knobs, floating pills, small chips
	ElevationCard                     // main floating paper card resting over the window ground
	ElevationPopover                  // menus, tooltips, toolbars, popovers
	ElevationDialog                   // modal dialogs, search, export, command panels
	ElevationDrawer                   // side drawers sliding over content
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
	case ElevationDialog:
		return []Shadow{{0, 2, 6, 0, black(0.08)}, {0, 12, 32, 0, black(0.14)}}
	case ElevationDrawer:
		return []Shadow{{8, 0, 28, 0, black(0.18)}}
	default:
		return []Shadow{{0, 1, 2, 0, black(0.06)}, {0, 4, 12, 0, black(0.08)}, {0, 12, 32, 0, black(0.12)}}
	}
}

// Scrim is the dimming layer behind modal dialogs and drawers.
func (p Palette) Scrim() ui.Color {
	if p.Dark {
		return ui.RGBA(0, 0, 0, 0.45)
	}
	return ui.RGBA(0x20, 0x24, 0x2c, 0.18)
}
