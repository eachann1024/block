package tarot

import (
	"time"

	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// RenderWritingStage wraps the live Block editor inside the Midnight Copperplate crosshatch
// desk and warm parchment sheet (#f3ecdc) with gold corner flourishes, inner frames, fleurons,
// save-flash animation, and subtle card-turn transition when switching notes.
func (ctrl *Controller) RenderWritingStage(c *ui.Context, availW float32, renderBody func(sheetW float32)) {
	tc := theme.Tarot()
	now := time.Now()

	ctrl.mu.RLock()
	flashAt := ctrl.SaveFlashAt
	switchAt := ctrl.PageSwitchAt
	ctrl.mu.RUnlock()

	flashProg := float32(0)
	if !flashAt.IsZero() {
		el := now.Sub(flashAt).Seconds()
		if el >= 0 && el < 1.1 {
			flashProg = float32(el / 1.1)
		}
	}

	stage := ui.Column(c.Key("tarot-writing-stage")).Grow(1).MinWidth(0).FillHeight().
		AlignItems(ui.Center).Padding(theme.Space20, theme.Space24, theme.Space24, theme.Space24).Clip()
	stage.Draw(func(p *ui.Painter, r ui.Rect) {
		PaintCrosshatchCloth(p, r)
	})
	blockui.Probe(stage, "tarot-writing-stage")

	sheetW := min(max(availW-theme.Space24*2, 320), theme.TarotSheetMaxWidth)

	stage.Children(func() {
		sheet := ui.Column(c.Key("tarot-writing-sheet")).Width(sheetW).FillHeight().
			Radius(theme.RadiusSM).Background(tc.ParchmentPaper).Clip()
		blockui.Elevate(sheet, theme.ElevationTarotSheet)

		// Subtle card-turn / settle entrance when switching notes in Tarot mode.
		if !switchAt.IsZero() {
			el := now.Sub(switchAt).Seconds()
			if el >= 0 && el < 0.34 {
				u := float32(el / 0.34)
				ease := 1 - (1-u)*(1-u)*(1-u)
				sheet.Translate(-14*(1-ease), 6*(1-ease)).Opacity(0.25 + 0.75*ease)
				c.Invalidate()
			}
		}

		// Perimeter parchment double-frame, top/bottom fleurons, and 4 animated gold corner flourishes.
		// Drawn over the margin perimeter only; the editing interior stays completely clean.
		sheet.DrawOver(func(p *ui.Painter, r ui.Rect) {
			blockui.RecordBounds("tarot-writing-sheet", r)
			PaintPaperSheetOverlay(p, r, theme.RadiusSM, flashProg, true)
			if flashProg > 0 && flashProg < 1 && !p.Preferences().ReduceMotion {
				p.AnimationFrameRate(60)
			}
		})

		sheet.Children(func() {
			// Inner safe inset keeps the editor text completely clear of the 11px inner gold frame.
			ui.Column(c.Key("tarot-sheet-body")).Fill().Padding(theme.Space16, theme.Space16, theme.Space16, theme.Space16).
				Children(func() {
					if renderBody != nil {
						theme.WithPalette(theme.TarotPaperPalette(), func() {
							renderBody(max(sheetW-theme.Space16*2, 260))
						})
					}
				})
		})
	})
}
