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
	VersionPillBorder ui.Color
	VersionPillFG     ui.Color
	SourcePillBG      ui.Color
	SourcePillBorder  ui.Color
	SourcePillFG      ui.Color
	ProgressTrack     ui.Color
	ProgressFill      ui.Color
	ReadyBadgeBG      ui.Color
	ReadyBadgeFG      ui.Color
	CategoryNewFG     ui.Color
	CategoryNewBG     ui.Color
	CategoryImpFG     ui.Color
	CategoryImpBG     ui.Color
	CategoryFixFG     ui.Color
	CategoryFixBG     ui.Color
	Mark              CatMarkColors
}

// UpdaterColors returns the Light or Dark semantic colors for the updater UI.
func (p Palette) UpdaterColors() UpdaterPalette {
	if p.Dark {
		return UpdaterPalette{
			BadgeDot:          p.Accent,
			VersionPillBG:     p.SelectionBG,
			VersionPillBorder: p.IconFill,
			VersionPillFG:     p.SelectionFG,
			SourcePillBG:      p.CodeBG,
			SourcePillBorder:  p.BtnBorder,
			SourcePillFG:      p.TextSecondary,
			ProgressTrack:     p.CodeBG,
			ProgressFill:      p.Accent,
			ReadyBadgeBG:      p.SuccessSurface,
			ReadyBadgeFG:      p.Success,
			CategoryNewFG:     p.SelectionFG,
			CategoryNewBG:     p.SelectionBG,
			CategoryImpFG:     p.Success,
			CategoryImpBG:     p.SuccessSurface,
			CategoryFixFG:     p.Warning,
			CategoryFixBG:     p.WarningSurface,
			Mark:              catMark,
		}
	}
	return UpdaterPalette{
		BadgeDot:          p.Accent,
		VersionPillBG:     p.Selected,
		VersionPillBorder: p.IconFill,
		VersionPillFG:     p.SelectionFG,
		SourcePillBG:      p.CodeBG,
		SourcePillBorder:  p.Hairline,
		SourcePillFG:      p.TextSecondary,
		ProgressTrack:     p.Shell,
		ProgressFill:      p.Accent,
		ReadyBadgeBG:      p.SuccessSurface,
		ReadyBadgeFG:      p.Success,
		CategoryNewFG:     p.SelectionFG,
		CategoryNewBG:     p.Selected,
		CategoryImpFG:     p.Success,
		CategoryImpBG:     p.SuccessSurface,
		CategoryFixFG:     p.Warning,
		CategoryFixBG:     p.WarningSurface,
		Mark:              catMark,
	}
}
