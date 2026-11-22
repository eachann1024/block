package theme

import "github.com/egoist/mygo/ui"

// ShellChromeColors holds the T3-inspired floating-card shell tokens:
// quiet window ground, elevated paper card + contour, translucent chrome hover
// over the brand landscape, sidebar search field, and notebook monogram badge.
type ShellChromeColors struct {
	Ground           ui.Color
	Card             ui.Color
	CardBorder       ui.Color
	PathBarBorder    ui.Color // subtle bottom hairline of the external-file breadcrumb bar
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
	if p.Tarot && p.Dark {
		return ShellChromeColors{
			Ground:           ui.Hex("#09111e"),
			Card:             ui.Hex("#0a1322"),
			CardBorder:       ui.RGBA(201, 162, 90, 0.24),
			PathBarBorder:    ui.RGBA(201, 162, 90, 0.18),
			TabBorder:        ui.RGBA(201, 162, 90, 0.28),
			Hover:            ui.RGBA(201, 162, 90, 0.10),
			FieldBG:          ui.Hex("#0c182b"),
			FieldBorder:      ui.RGBA(201, 162, 90, 0.32),
			FieldBorderHover: ui.Hex("#e2c07e"),
			KbdBG:            ui.Hex("#0f1b2e"),
			BadgeFG:          ui.Hex("#e2c07e"),
		}
	}
	if p.Tarot && !p.Dark {
		return ShellChromeColors{
			Ground:           ui.Hex("#0e1a2c"),
			Card:             ui.Hex("#f3ecdc"),
			CardBorder:       ui.RGBA(167, 132, 63, 0.45),
			PathBarBorder:    ui.RGBA(122, 94, 44, 0.16),
			TabBorder:        ui.RGBA(122, 94, 44, 0.22),
			Hover:            ui.RGBA(122, 94, 44, 0.08),
			FieldBG:          ui.Hex("#f4ecd9"),
			FieldBorder:      ui.RGBA(167, 132, 63, 0.38),
			FieldBorderHover: ui.Hex("#a7843f"),
			KbdBG:            ui.Hex("#ebe0c6"),
			BadgeFG:          ui.Hex("#7a5718"),
		}
	}
	if p.Dark {
		return ShellChromeColors{
			Ground:           ui.Hex("#222222"),
			Card:             ui.Hex("#181818"),
			CardBorder:       ui.Hex("#2e2e2e"),
			PathBarBorder:    ui.RGBA(0xff, 0xff, 0xff, 0.07),
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
		PathBarBorder:    ui.RGBA(0, 0, 0, 0.05),
		TabBorder:        ui.RGBA(0, 0, 0, 0.06),
		Hover:            ui.RGBA(0x20, 0x24, 0x2c, 0.07),
		FieldBG:          ui.Hex("#ffffff"),
		FieldBorder:      ui.Hex("#e1e4e9"),
		FieldBorderHover: ui.Hex("#d0d0d0"),
		KbdBG:            ui.Hex("#f3f4f6"),
		BadgeFG:          ui.Hex("#2f4f8f"),
	}
}

// StoreCardColors holds surface, icon badge, footer, and chip colours for App Store
// tinted paper cards (Scheme A: recessed tile lifting to crisp paper on hover).
type StoreCardColors struct {
	BG                 ui.Color
	Border             ui.Color
	HoverBG            ui.Color
	HoverBorder        ui.Color
	IconBG             ui.Color
	IconBorder         ui.Color
	OfficialIconBG     ui.Color
	OfficialIconBorder ui.Color
	Divider            ui.Color
	CategoryBG         ui.Color
	ExtBG              ui.Color
	ExtBorder          ui.Color
}

// StoreCard returns the tinted App Store card token set for p.
func (p Palette) StoreCard() StoreCardColors {
	if p.Tarot && p.Dark {
		return StoreCardColors{
			BG:                 ui.Hex("#0e192b"),
			Border:             ui.RGBA(201, 162, 90, 0.22),
			HoverBG:            ui.Hex("#13223a"),
			HoverBorder:        ui.RGBA(201, 162, 90, 0.42),
			IconBG:             ui.Hex("#0a1322"),
			IconBorder:         ui.RGBA(201, 162, 90, 0.26),
			OfficialIconBG:     ui.Hex("#182844"),
			OfficialIconBorder: ui.RGBA(201, 162, 90, 0.45),
			Divider:            ui.RGBA(201, 162, 90, 0.16),
			CategoryBG:         ui.Hex("#15243c"),
			ExtBG:              ui.Hex("#0a1322"),
			ExtBorder:          ui.RGBA(201, 162, 90, 0.24),
		}
	}
	if p.Tarot && !p.Dark {
		return StoreCardColors{
			BG:                 ui.Hex("#ede4d0"),
			Border:             ui.RGBA(167, 132, 63, 0.34),
			HoverBG:            ui.Hex("#f6f0e2"),
			HoverBorder:        ui.RGBA(167, 132, 63, 0.56),
			IconBG:             ui.Hex("#f6f0e2"),
			IconBorder:         ui.RGBA(167, 132, 63, 0.30),
			OfficialIconBG:     ui.Hex("#e7dec7"),
			OfficialIconBorder: ui.RGBA(167, 132, 63, 0.48),
			Divider:            ui.RGBA(167, 132, 63, 0.20),
			CategoryBG:         ui.Hex("#e5dac1"),
			ExtBG:              ui.Hex("#f6f0e2"),
			ExtBorder:          ui.RGBA(167, 132, 63, 0.28),
		}
	}
	if p.Dark {
		return StoreCardColors{
			BG:                 ui.Hex("#1f1f1f"),
			Border:             ui.Hex("#2d2d2d"),
			HoverBG:            ui.Hex("#252525"),
			HoverBorder:        ui.Hex("#434343"),
			IconBG:             ui.Hex("#181818"),
			IconBorder:         ui.Hex("#303030"),
			OfficialIconBG:     ui.Hex("#1e2a3f"),
			OfficialIconBorder: ui.Hex("#2f4569"),
			Divider:            ui.Hex("#2a2a2a"),
			CategoryBG:         ui.Hex("#292929"),
			ExtBG:              ui.Hex("#282828"),
			ExtBorder:          ui.Hex("#353535"),
		}
	}
	return StoreCardColors{
		BG:                 ui.Hex("#f8f8f7"),
		Border:             ui.Hex("#e6e6e3"),
		HoverBG:            ui.Hex("#ffffff"),
		HoverBorder:        ui.Hex("#cfd3da"),
		IconBG:             ui.Hex("#ffffff"),
		IconBorder:         ui.Hex("#e4e5e2"),
		OfficialIconBG:     ui.Hex("#eef3fc"),
		OfficialIconBorder: ui.Hex("#d3e0f6"),
		Divider:            ui.Hex("#eaeae7"),
		CategoryBG:         ui.Hex("#ecece9"),
		ExtBG:              ui.Hex("#ffffff"),
		ExtBorder:          ui.Hex("#e2e4e8"),
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
	return p.PerfTileColors(id).Dot
}

// PerfTileColors defines the low-saturation pastel palette for one storage Bento tile
// and its synchronized breakdown strip (matching D1-PastelMist).
type PerfTileColors struct {
	BG        ui.Color
	HotBG     ui.Color
	Border    ui.Color
	HotBorder ui.Color
	BarBG     ui.Color
	Dot       ui.Color
	TitleText ui.Color
	SubText   ui.Color
}

// PerfTileColors returns the category-specific low-saturation pastel colour set.
func (p Palette) PerfTileColors(id string) PerfTileColors {
	if p.Dark {
		switch id {
		case "assets":
			return PerfTileColors{
				BG: ui.Hex("#1e2b26"), HotBG: ui.Hex("#24362f"),
				Border: ui.Hex("#2d433a"), HotBorder: ui.Hex("#5da685"),
				BarBG: ui.Hex("#23342d"), Dot: ui.Hex("#5da685"),
				TitleText: ui.Hex("#d9ece3"), SubText: ui.Hex("#99bba9"),
			}
		case "config":
			return PerfTileColors{
				BG: ui.Hex("#1e2636"), HotBG: ui.Hex("#253147"),
				Border: ui.Hex("#2e3e5c"), HotBorder: ui.Hex("#779de8"),
				BarBG: ui.Hex("#232f44"), Dot: ui.Hex("#779de8"),
				TitleText: ui.Hex("#dce6f9"), SubText: ui.Hex("#9bb0d4"),
			}
		case "notes":
			return PerfTileColors{
				BG: ui.Hex("#252436"), HotBG: ui.Hex("#2e2d45"),
				Border: ui.Hex("#3b3959"), HotBorder: ui.Hex("#8e87d4"),
				BarBG: ui.Hex("#2b2a40"), Dot: ui.Hex("#8e87d4"),
				TitleText: ui.Hex("#e3e1f7"), SubText: ui.Hex("#aba7cf"),
			}
		case "history":
			return PerfTileColors{
				BG: ui.Hex("#2c2233"), HotBG: ui.Hex("#382a42"),
				Border: ui.Hex("#4a3757"), HotBorder: ui.Hex("#b585cf"),
				BarBG: ui.Hex("#33273d"), Dot: ui.Hex("#b585cf"),
				TitleText: ui.Hex("#eedef5"), SubText: ui.Hex("#be9fcc"),
			}
		case "recovery":
			return PerfTileColors{
				BG: ui.Hex("#1d2b2e"), HotBG: ui.Hex("#24373b"),
				Border: ui.Hex("#2f474d"), HotBorder: ui.Hex("#6db0bd"),
				BarBG: ui.Hex("#223337"), Dot: ui.Hex("#6db0bd"),
				TitleText: ui.Hex("#d9eef2"), SubText: ui.Hex("#99bec4"),
			}
		case "logs":
			return PerfTileColors{
				BG: ui.Hex("#322222"), HotBG: ui.Hex("#402a2a"),
				Border: ui.Hex("#543838"), HotBorder: ui.Hex("#d98480"),
				BarBG: ui.Hex("#3b2727"), Dot: ui.Hex("#d98480"),
				TitleText: ui.Hex("#f5dedd"), SubText: ui.Hex("#cca09e"),
			}
		case "ai_sessions":
			return PerfTileColors{
				BG: ui.Hex("#31281d"), HotBG: ui.Hex("#3e3223"),
				Border: ui.Hex("#52422d"), HotBorder: ui.Hex("#d9a45b"),
				BarBG: ui.Hex("#392e21"), Dot: ui.Hex("#d9a45b"),
				TitleText: ui.Hex("#f5e7d3"), SubText: ui.Hex("#ccb391"),
			}
		default:
			return PerfTileColors{
				BG: p.Sidebar, HotBG: p.Selected,
				Border: p.Hairline, HotBorder: p.Accent,
				BarBG: p.Hover, Dot: p.Accent,
				TitleText: p.Text, SubText: p.TextSecondary,
			}
		}
	}

	switch id {
	case "assets":
		return PerfTileColors{
			BG: ui.Hex("#eef6f2"), HotBG: ui.Hex("#e1f0e8"),
			Border: ui.Hex("#cfe2d7"), HotBorder: ui.Hex("#5da685"),
			BarBG: ui.Hex("#e2f1e9"), Dot: ui.Hex("#5da685"),
			TitleText: ui.Hex("#1f332a"), SubText: ui.Hex("#436154"),
		}
	case "config":
		return PerfTileColors{
			BG: ui.Hex("#eef3fb"), HotBG: ui.Hex("#e0eaf9"),
			Border: ui.Hex("#cddcf2"), HotBorder: ui.Hex("#547dd0"),
			BarBG: ui.Hex("#e3ecfa"), Dot: ui.Hex("#547dd0"),
			TitleText: ui.Hex("#202d45"), SubText: ui.Hex("#45597d"),
		}
	case "notes":
		return PerfTileColors{
			BG: ui.Hex("#f2f1fb"), HotBG: ui.Hex("#e6e4f8"),
			Border: ui.Hex("#d8d5f0"), HotBorder: ui.Hex("#7068ba"),
			BarBG: ui.Hex("#e9e7f8"), Dot: ui.Hex("#7068ba"),
			TitleText: ui.Hex("#2b2847"), SubText: ui.Hex("#54507a"),
		}
	case "history":
		return PerfTileColors{
			BG: ui.Hex("#f7f0fa"), HotBG: ui.Hex("#eee1f5"),
			Border: ui.Hex("#e0cce9"), HotBorder: ui.Hex("#9c6ab5"),
			BarBG: ui.Hex("#efe4f5"), Dot: ui.Hex("#9c6ab5"),
			TitleText: ui.Hex("#3a2444"), SubText: ui.Hex("#684b76"),
		}
	case "recovery":
		return PerfTileColors{
			BG: ui.Hex("#eef6f7"), HotBG: ui.Hex("#e0eff1"),
			Border: ui.Hex("#cce1e5"), HotBorder: ui.Hex("#5596a3"),
			BarBG: ui.Hex("#e2f0f2"), Dot: ui.Hex("#5596a3"),
			TitleText: ui.Hex("#1f3539"), SubText: ui.Hex("#466368"),
		}
	case "logs":
		return PerfTileColors{
			BG: ui.Hex("#fcf1f0"), HotBG: ui.Hex("#f8e3e1"),
			Border: ui.Hex("#ecd0cd"), HotBorder: ui.Hex("#c8706c"),
			BarBG: ui.Hex("#f7e5e3"), Dot: ui.Hex("#c8706c"),
			TitleText: ui.Hex("#442424"), SubText: ui.Hex("#744949"),
		}
	case "ai_sessions":
		return PerfTileColors{
			BG: ui.Hex("#fcf5ec"), HotBG: ui.Hex("#f6e9d6"),
			Border: ui.Hex("#ebd7ba"), HotBorder: ui.Hex("#c68e47"),
			BarBG: ui.Hex("#f6ebd9"), Dot: ui.Hex("#c68e47"),
			TitleText: ui.Hex("#42301b"), SubText: ui.Hex("#72593b"),
		}
	default:
		return PerfTileColors{
			BG: p.Sidebar, HotBG: p.Selected,
			Border: p.Hairline, HotBorder: p.Accent,
			BarBG: p.Hover, Dot: p.Accent,
			TitleText: p.Text, SubText: p.TextSecondary,
		}
	}
}
