package theme

import "github.com/egoist/mygo/ui"

// ShellChromeColors holds the T3-inspired floating-card shell tokens:
// quiet window ground, elevated paper card + contour, translucent chrome hover
// over the brand landscape, sidebar search field, and notebook monogram badge.
type ShellChromeColors struct {
	Ground           ui.Color
	Card             ui.Color
	CardBorder       ui.Color
	TabBorder        ui.Color // hairline ring of the active titlebar tab
	Hover            ui.Color
	FieldBG          ui.Color
	FieldBorder      ui.Color
	FieldBorderHover ui.Color
	KbdBG            ui.Color
	BadgeFG          ui.Color
}

// ShellChrome returns the window shell and floating-card token set for p.
func (p Palette) ShellChrome() ShellChromeColors {
	if p.Dark {
		return ShellChromeColors{
			Ground:           ui.Hex("#222222"),
			Card:             ui.Hex("#181818"),
			CardBorder:       ui.Hex("#2e2e2e"),
			TabBorder:        ui.RGBA(0xff, 0xff, 0xff, 0.08),
			Hover:            ui.RGBA(0xff, 0xff, 0xff, 0.07),
			FieldBG:          ui.Hex("#2a2a2a"),
			FieldBorder:      ui.Hex("#3a3a3a"),
			FieldBorderHover: ui.Hex("#4c4c4c"),
			KbdBG:            ui.Hex("#303030"),
			BadgeFG:          ui.Hex("#b9cdf5"),
		}
	}
	return ShellChromeColors{
		Ground:           ui.Hex("#f1f1f1"),
		Card:             ui.Hex("#ffffff"),
		CardBorder:       ui.Hex("#e4e6ea"),
		TabBorder:        ui.RGBA(0, 0, 0, 0.06),
		Hover:            ui.RGBA(0x20, 0x24, 0x2c, 0.07),
		FieldBG:          ui.Hex("#ffffff"),
		FieldBorder:      ui.Hex("#e1e4e9"),
		FieldBorderHover: ui.Hex("#d0d0d0"),
		KbdBG:            ui.Hex("#f3f4f6"),
		BadgeFG:          ui.Hex("#2f4f8f"),
	}
}

// DotPagerColors holds recessed-track tactile bead and companion header dot colours.
type DotPagerColors struct {
	TrackBG          ui.Color
	TrackBorder      ui.Color
	Idle             ui.Color
	Hover            ui.Color
	Core             ui.Color
	CloseHoverBorder ui.Color
}

// DotPager returns the DotPager colour set for p.
func (p Palette) DotPager() DotPagerColors {
	if p.Dark {
		return DotPagerColors{
			TrackBG:          ui.Hex("#24272f"),
			TrackBorder:      ui.Hex("#333844"),
			Idle:             ui.Hex("#4d5565"),
			Hover:            ui.Hex("#8d97ab"),
			Core:             ui.Hex("#eef4ff"),
			CloseHoverBorder: ui.Hex("#6e3232"),
		}
	}
	return DotPagerColors{
		TrackBG:          ui.Hex("#f1f3f7"),
		TrackBorder:      ui.Hex("#e2e6ee"),
		Idle:             ui.Hex("#bcc4d2"),
		Hover:            ui.Hex("#7b869a"),
		Core:             ui.Hex("#eef4ff"),
		CloseHoverBorder: ui.Hex("#f3c7c7"),
	}
}

// KeycapColors holds surface colours for Keycap chips.
type KeycapColors struct {
	BG ui.Color
}

// Keycap returns the Keycap colour set for p.
func (p Palette) Keycap() KeycapColors {
	if p.Dark {
		return KeycapColors{BG: ui.Hex("#292b2f")}
	}
	return KeycapColors{BG: ui.Hex("#f4f4f2")}
}

// ExportColors holds export preview stage and swatch chip colours.
type ExportColors struct {
	StageBG    ui.Color
	ChipBorder ui.Color
}

// Export returns the Export modal colour set for p.
func (p Palette) Export() ExportColors {
	if p.Dark {
		return ExportColors{
			StageBG:    ui.Hex("#121316"),
			ChipBorder: ui.Hex("#3a3a3a"),
		}
	}
	return ExportColors{
		StageBG:    ui.Hex("#e8eaee"),
		ChipBorder: ui.Hex("#d8d2c5"),
	}
}

// AIColors holds AI panel informational banner colours.
type AIColors struct {
	InfoBannerBG     ui.Color
	InfoBannerBorder ui.Color
	InfoBannerFG     ui.Color
}

// AI returns the AI panel colour set for p.
func (p Palette) AI() AIColors {
	if p.Dark {
		return AIColors{
			InfoBannerBG:     ui.Hex("#1f2b40"),
			InfoBannerBorder: ui.Hex("#31456a"),
			InfoBannerFG:     ui.Hex("#9dbcf5"),
		}
	}
	return AIColors{
		InfoBannerBG:     ui.Hex("#eef4ff"),
		InfoBannerBorder: ui.Hex("#c9d8f3"),
		InfoBannerFG:     ui.Hex("#345498"),
	}
}

// PerfCategoryColor returns the chart series colour for a disk-usage category ID.
func (p Palette) PerfCategoryColor(id string) ui.Color {
	switch id {
	case "notes":
		return ui.Hex("#547dd0")
	case "assets":
		return ui.Hex("#3ba57b")
	case "history":
		return ui.Hex("#a870bc")
	case "recovery":
		return ui.Hex("#8c8d96")
	case "ai_sessions":
		return ui.Hex("#d19348")
	case "logs":
		return ui.Hex("#d16e6e")
	case "config":
		return ui.Hex("#6a9bab")
	default:
		return p.Accent
	}
}
