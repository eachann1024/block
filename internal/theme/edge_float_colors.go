package theme

import "github.com/egoist/mygo/ui"

// EdgeFloatColors holds Ocean Paper tokens for the left/right colored edge
// hot-zones and rounded floating Sidebar / AI drawer cards (Scheme A).
type EdgeFloatColors struct {
	StripIdle  ui.Color
	StripHot   ui.Color
	StripGlow  ui.Color
	CardBorder ui.Color
}

// EdgeFloat returns the edge hot-zone and floating card colour set for p.
func (p Palette) EdgeFloat() EdgeFloatColors {
	if p.Dark {
		return EdgeFloatColors{
			StripIdle:  ui.RGBA(0x6d, 0x95, 0xe8, 0.36),
			StripHot:   p.Accent,
			StripGlow:  ui.RGBA(0x6d, 0x95, 0xe8, 0.16),
			CardBorder: ui.Hex("#343944"),
		}
	}
	return EdgeFloatColors{
		StripIdle:  ui.RGBA(0x54, 0x7d, 0xd0, 0.28),
		StripHot:   p.Accent,
		StripGlow:  ui.RGBA(0x54, 0x7d, 0xd0, 0.12),
		CardBorder: ui.Hex("#d8dee9"),
	}
}
