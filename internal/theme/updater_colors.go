package theme

import "github.com/egoist/mygo/ui"

// CatMarkColors are the fixed brand colors of the animated Block cat mark.
type CatMarkColors struct {
	Tile  ui.Color
	Cat   ui.Color
	Eye   ui.Color
	Pupil ui.Color
	Bolt  ui.Color
}

// catMark matches the app icon, so it stays the same in Light and Dark.
var catMark = CatMarkColors{
	Tile:  ui.Hex("#eef0f4"),
	Cat:   ui.Hex("#2b2f38"),
	Eye:   ui.Hex("#2f6fe4"),
	Pupil: ui.Hex("#0f1830"),
	Bolt:  ui.Hex("#ffffff"),
}

// UpdaterPalette groups semantic surface and badge tokens for the application updater.
type UpdaterPalette struct {
	BadgeDot          ui.Color
	VersionPillBG     ui.Color
	VersionPillFG     ui.Color
	ProgressTrack     ui.Color
	ProgressFill      ui.Color
	CategoryNewFG     ui.Color
	CategoryNewBG     ui.Color
	CategoryImpFG     ui.Color
	CategoryImpBG     ui.Color
	CategoryFixFG     ui.Color
	CategoryFixBG     ui.Color
	BadgeGoodFG       ui.Color
	BadgeGoodBG       ui.Color
	BadgeGoodBorder   ui.Color
	BadgeWarnFG       ui.Color
	BadgeWarnBG       ui.Color
	BadgeDangerFG     ui.Color
	BadgeDangerBG     ui.Color
	BadgeDangerBorder ui.Color
	BadgeAccentFG     ui.Color
	BadgeAccentBG     ui.Color
	SignalBarOff      ui.Color
	Mark              CatMarkColors
}

// UpdaterColors returns the Light or Dark semantic colors for the updater UI.
func (p Palette) UpdaterColors() UpdaterPalette {
	if p.Dark {
		return UpdaterPalette{
			BadgeDot:          p.Accent,
			VersionPillBG:     p.SelectionBG,
			VersionPillFG:     p.SelectionFG,
			ProgressTrack:     p.Accent.Alpha(0.18),
			ProgressFill:      p.Accent,
			CategoryNewFG:     p.SelectionFG,
			CategoryNewBG:     p.SelectionBG,
			CategoryImpFG:     p.Success,
			CategoryImpBG:     p.SuccessSurface,
			CategoryFixFG:     p.Warning,
			CategoryFixBG:     p.WarningSurface,
			BadgeGoodFG:       ui.Hex("#6fd39b"),
			BadgeGoodBG:       p.SuccessSurface,
			BadgeGoodBorder:   ui.Hex("#284d38"),
			BadgeWarnFG:       ui.Hex("#f0b866"),
			BadgeWarnBG:       p.WarningSurface,
			BadgeDangerFG:     ui.Hex("#f48989"),
			BadgeDangerBG:     p.DangerSurface,
			BadgeDangerBorder: ui.Hex("#572d2d"),
			BadgeAccentFG:     p.Accent,
			BadgeAccentBG:     p.Accent.Alpha(0.16),
			SignalBarOff:      ui.Hex("#3d3d3d"),
			Mark:              catMark,
		}
	}
	return UpdaterPalette{
		BadgeDot:          p.Accent,
		VersionPillBG:     p.Selected,
		VersionPillFG:     p.SelectionFG,
		ProgressTrack:     p.Accent.Alpha(0.14),
		ProgressFill:      p.Accent,
		CategoryNewFG:     p.SelectionFG,
		CategoryNewBG:     p.Selected,
		CategoryImpFG:     p.Success,
		CategoryImpBG:     p.SuccessSurface,
		CategoryFixFG:     p.Warning,
		CategoryFixBG:     p.WarningSurface,
		BadgeGoodFG:       ui.Hex("#1a6139"),
		BadgeGoodBG:       p.SuccessSurface,
		BadgeGoodBorder:   ui.Hex("#c5e4d2"),
		BadgeWarnFG:       ui.Hex("#9a5b13"),
		BadgeWarnBG:       p.WarningSurface,
		BadgeDangerFG:     ui.Hex("#b82e2e"),
		BadgeDangerBG:     p.DangerSurface,
		BadgeDangerBorder: ui.Hex("#f2c9c9"),
		BadgeAccentFG:     p.Accent,
		BadgeAccentBG:     p.Accent.Alpha(0.12),
		SignalBarOff:      ui.Hex("#dde0e5"),
		Mark:              catMark,
	}
}
