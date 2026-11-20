package theme

import "github.com/egoist/mygo/ui"

// EdgeFloatColors holds Ocean Paper tokens for the rounded floating
// Sidebar / AI drawer cards (Scheme A).
type EdgeFloatColors struct {
	CardBorder ui.Color
}

// EdgeFloat returns the floating card colour set for p.
func (p Palette) EdgeFloat() EdgeFloatColors {
	if p.Dark {
		return EdgeFloatColors{
			CardBorder: ui.Hex("#343944"),
		}
	}
	return EdgeFloatColors{
		CardBorder: ui.Hex("#dedede"),
	}
}
