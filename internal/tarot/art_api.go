package tarot

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// IconKind identifies small vector icons painted on Tarot chrome and badges.
type IconKind uint8

const (
	IconMoon IconKind = iota
	IconStar4
	IconChevronLeft
	IconQuill
	IconSave
	IconCheck
	IconClose
	IconStop
)

// ArtRenderer defines the Midnight Copperplate ("午夜铜版") vector drawing backend.
// Implemented in art.go and registered via init().
type ArtRenderer interface {
	PaintCardBack(p *ui.Painter, r ui.Rect, radius float32, strokeScale float32)
	PaintCardFrame(p *ui.Painter, r ui.Rect, radius float32)
	PaintArcanaGlyph(p *ui.Painter, arcanaKey string, r ui.Rect, strokeScale float32, trim float32)
	PaintTableCloth(p *ui.Painter, r ui.Rect, dailyGlow bool)
	PaintCrosshatchCloth(p *ui.Painter, r ui.Rect)
	PaintStarChart(p *ui.Painter, r ui.Rect)
	PaintMoonPhaseRing(p *ui.Painter, r ui.Rect, angleDeg float32)
	PaintCornerFlourish(p *ui.Painter, r ui.Rect, rotDeg float32, col ui.Color, strokeW float32)
	PaintFleuron(p *ui.Painter, r ui.Rect, col ui.Color, strokeW float32)
	PaintCompass(p *ui.Painter, r ui.Rect)
	PaintSlotPlaceholder(p *ui.Painter, r ui.Rect)
	PaintMysticEye(p *ui.Painter, r ui.Rect, alpha float32)
	PaintGlintSweep(p *ui.Painter, r ui.Rect, radius float32, prog float32)
	PaintPaperSheetOverlay(p *ui.Painter, r ui.Rect, radius float32, flashProg float32, showFleurons bool)
	PaintDevelopingCard(p *ui.Painter, r ui.Rect, arcanaKey string, generating bool, prog float32, glintProg float32, reduced bool)
	PaintIcon(p *ui.Painter, r ui.Rect, kind IconKind, col ui.Color)
}

var activeArt ArtRenderer = fallbackArt{}

// RegisterArt installs the full copperplate engraving renderer (called by art.go's init).
func RegisterArt(a ArtRenderer) {
	if a != nil {
		activeArt = a
	}
}

func PaintCardBack(p *ui.Painter, r ui.Rect, radius float32, strokeScale float32) {
	activeArt.PaintCardBack(p, r, radius, strokeScale)
}

func PaintCardFrame(p *ui.Painter, r ui.Rect, radius float32) {
	activeArt.PaintCardFrame(p, r, radius)
}

func PaintArcanaGlyph(p *ui.Painter, arcanaKey string, r ui.Rect, strokeScale float32, trim float32) {
	activeArt.PaintArcanaGlyph(p, arcanaKey, r, strokeScale, trim)
}

func PaintTableCloth(p *ui.Painter, r ui.Rect, dailyGlow bool) {
	activeArt.PaintTableCloth(p, r, dailyGlow)
}

func PaintCrosshatchCloth(p *ui.Painter, r ui.Rect) {
	activeArt.PaintCrosshatchCloth(p, r)
}

func PaintStarChart(p *ui.Painter, r ui.Rect) {
	activeArt.PaintStarChart(p, r)
}

func PaintMoonPhaseRing(p *ui.Painter, r ui.Rect, angleDeg float32) {
	activeArt.PaintMoonPhaseRing(p, r, angleDeg)
}

func PaintCornerFlourish(p *ui.Painter, r ui.Rect, rotDeg float32, col ui.Color, strokeW float32) {
	activeArt.PaintCornerFlourish(p, r, rotDeg, col, strokeW)
}

func PaintFleuron(p *ui.Painter, r ui.Rect, col ui.Color, strokeW float32) {
	activeArt.PaintFleuron(p, r, col, strokeW)
}

func PaintCompass(p *ui.Painter, r ui.Rect) {
	activeArt.PaintCompass(p, r)
}

func PaintSlotPlaceholder(p *ui.Painter, r ui.Rect) {
	activeArt.PaintSlotPlaceholder(p, r)
}

func PaintMysticEye(p *ui.Painter, r ui.Rect, alpha float32) {
	activeArt.PaintMysticEye(p, r, alpha)
}

func PaintGlintSweep(p *ui.Painter, r ui.Rect, radius float32, prog float32) {
	activeArt.PaintGlintSweep(p, r, radius, prog)
}

func PaintPaperSheetOverlay(p *ui.Painter, r ui.Rect, radius float32, flashProg float32, showFleurons bool) {
	activeArt.PaintPaperSheetOverlay(p, r, radius, flashProg, showFleurons)
}

func PaintDevelopingCard(p *ui.Painter, r ui.Rect, arcanaKey string, generating bool, prog float32, glintProg float32, reduced bool) {
	activeArt.PaintDevelopingCard(p, r, arcanaKey, generating, prog, glintProg, reduced)
}

func PaintIcon(p *ui.Painter, r ui.Rect, kind IconKind, col ui.Color) {
	activeArt.PaintIcon(p, r, kind, col)
}

// IconBox returns a fixed-size element painted with a Tarot vector icon.
func IconBox(c *ui.Context, kind IconKind, size float32, col ui.Color) ui.Element {
	return ui.Box(c).Size(size, size).Shrink(0).IgnoreInput(true).Draw(func(p *ui.Painter, r ui.Rect) {
		PaintIcon(p, r, kind, col)
	})
}

// fallbackArt keeps package compilation independent while art.go is developed in parallel.
type fallbackArt struct{}

func (fallbackArt) PaintCardBack(p *ui.Painter, r ui.Rect, radius float32, _ float32) {
	tc := theme.Tarot()
	p.FillRadialGradient(r, tc.CardBackGrad, radius)
	p.Stroke(r, tc.GoldBorderMid, radius, 1)
}
func (fallbackArt) PaintCardFrame(p *ui.Painter, r ui.Rect, radius float32) {
	tc := theme.Tarot()
	p.FillRadialGradient(r, tc.ParchmentGrad, radius)
	p.Stroke(r, tc.ParchmentBorder, radius, 1)
}
func (fallbackArt) PaintArcanaGlyph(*ui.Painter, string, ui.Rect, float32, float32) {}
func (fallbackArt) PaintTableCloth(p *ui.Painter, r ui.Rect, dailyGlow bool) {
	tc := theme.Tarot()
	if dailyGlow {
		p.FillRadialGradient(r, tc.DailyGlowGrad, 0)
	} else {
		p.FillRadialGradient(r, tc.TableGlowGrad, 0)
	}
}
func (fallbackArt) PaintCrosshatchCloth(p *ui.Painter, r ui.Rect) {
	p.Fill(r, theme.Tarot().TableCanvas, 0)
}
func (fallbackArt) PaintStarChart(*ui.Painter, ui.Rect)                                  {}
func (fallbackArt) PaintMoonPhaseRing(*ui.Painter, ui.Rect, float32)                     {}
func (fallbackArt) PaintCornerFlourish(*ui.Painter, ui.Rect, float32, ui.Color, float32) {}
func (fallbackArt) PaintFleuron(*ui.Painter, ui.Rect, ui.Color, float32)                 {}
func (fallbackArt) PaintCompass(*ui.Painter, ui.Rect)                                    {}
func (fallbackArt) PaintSlotPlaceholder(*ui.Painter, ui.Rect)                            {}
func (fallbackArt) PaintMysticEye(*ui.Painter, ui.Rect, float32)                         {}
func (fallbackArt) PaintGlintSweep(*ui.Painter, ui.Rect, float32, float32)               {}
func (fallbackArt) PaintPaperSheetOverlay(*ui.Painter, ui.Rect, float32, float32, bool)  {}
func (fallbackArt) PaintDevelopingCard(*ui.Painter, ui.Rect, string, bool, float32, float32, bool) {
}
func (fallbackArt) PaintIcon(*ui.Painter, ui.Rect, IconKind, ui.Color) {}
