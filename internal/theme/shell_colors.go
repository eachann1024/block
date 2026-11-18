package theme

import "github.com/egoist/mygo/ui"

// DotPagerColors holds recessed-track tactile bead colours for DotPager.
type DotPagerColors struct {
	TrackBG     ui.Color
	TrackBorder ui.Color
	Idle        ui.Color
	Hover       ui.Color
	Core        ui.Color
}

// DotPager returns the DotPager colour set for p.
func (p Palette) DotPager() DotPagerColors {
	if p.Dark {
		return DotPagerColors{
			TrackBG:     ui.Hex("#24272f"),
			TrackBorder: ui.Hex("#333844"),
			Idle:        ui.Hex("#4d5565"),
			Hover:       ui.Hex("#8d97ab"),
			Core:        ui.Hex("#eef4ff"),
		}
	}
	return DotPagerColors{
		TrackBG:     ui.Hex("#f1f3f7"),
		TrackBorder: ui.Hex("#e2e6ee"),
		Idle:        ui.Hex("#bcc4d2"),
		Hover:       ui.Hex("#7b869a"),
		Core:        ui.Hex("#eef4ff"),
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
