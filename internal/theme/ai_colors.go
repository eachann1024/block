package theme

import "github.com/egoist/mygo/ui"

// AISurfaceColors holds surface tokens for the AI chat panel, composer,
// model/agent switcher popovers, cards, and settings controls with strict
// monotonic luminance in both Ocean Light and Ocean Dark.
type AISurfaceColors struct {
	// RailBG is the recessed icon-rail surface inside floating model popovers.
	RailBG ui.Color
	// ComposerBG and ComposerBorder style the chat composer input shell.
	ComposerBG     ui.Color
	ComposerBorder ui.Color
	// CapsuleBG and CapsuleBorder style interactive capsules/chips inside
	// the composer or settings cards (resting elevated above ComposerBG/Card).
	CapsuleBG     ui.Color
	CapsuleBorder ui.Color
	// WellBG styles secondary cards/wells sitting on Paper (process, sources, plan bars).
	WellBG ui.Color
	// UserBubbleBG styles the right-aligned user prompt bubble in the chat stream.
	UserBubbleBG ui.Color
	// HoverBG and SelectedBG provide monotonic hover/active elevation on menus,
	// rails, capsules, and wells without inverting dark-mode brightness.
	HoverBG    ui.Color
	SelectedBG ui.Color
	// TintedSelectedBG pairs with a 1px Accent border on active capsules and provider cards.
	TintedSelectedBG ui.Color
}

// AISurface returns the AI surface colour set for p.
func (p Palette) AISurface() AISurfaceColors {
	if p.Dark {
		return AISurfaceColors{
			RailBG:           ui.Hex("#1b1b1b"),
			ComposerBG:       ui.Hex("#222222"),
			ComposerBorder:   ui.Hex("#363636"),
			CapsuleBG:        ui.Hex("#2a2a2a"),
			CapsuleBorder:    ui.Hex("#3d3d3d"),
			WellBG:           ui.Hex("#222222"),
			UserBubbleBG:     ui.Hex("#282c34"),
			HoverBG:          ui.Hex("#313131"),
			SelectedBG:       ui.Hex("#363940"),
			TintedSelectedBG: ui.Hex("#233148"),
		}
	}
	return AISurfaceColors{
		RailBG:           ui.Hex("#f6f6f7"),
		ComposerBG:       p.Paper,
		ComposerBorder:   ui.Hex("#e8e8e6"),
		CapsuleBG:        p.Paper,
		CapsuleBorder:    p.BtnBorder,
		WellBG:           ui.Hex("#f6f6f6"),
		UserBubbleBG:     p.Hover,
		HoverBG:          p.Hover,
		SelectedBG:       p.Selected,
		TintedSelectedBG: p.Selected,
	}
}
