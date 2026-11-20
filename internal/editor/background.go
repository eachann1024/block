package editor

import (
	"math"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// BackgroundStyle configures the optional decorative dot or grid background
// rendered behind the editor canvas.
type BackgroundStyle struct {
	Mode     string // "off" | "dots" | "grid"
	Contrast string // "standard" | "high"
}

// SetBackground updates the editor's decorative background style for this frame.
func (s *Session) SetBackground(bg BackgroundStyle) {
	s.bgStyle = bg
}

func (s *Session) paintBackground(p *ui.Painter, r ui.Rect, pal theme.Palette) {
	mode := s.bgStyle.Mode
	if mode != "dots" && mode != "grid" {
		return
	}
	if r.W <= 0 || r.H <= 0 {
		return
	}

	fontSize, _, lh := s.textMetrics(model.BlockParagraph)
	step := float32(math.Round(float64(fontSize * lh)))
	if step < 16 {
		step = 16
	}

	edCols := pal.Editor()
	high := s.bgStyle.Contrast == "high"

	// Keep a clean breathing margin so patterns stay clear of frame edges and
	// the right-hand vertical scrollbar.
	const (
		insetL float32 = 12
		insetR float32 = 18
		insetT float32 = 8
		insetB float32 = 8
	)
	minX := r.X + insetL
	maxX := r.X + r.W - insetR
	minY := r.Y + insetT
	maxY := r.Y + r.H - insetB
	if maxX <= minX || maxY <= minY {
		return
	}

	// Horizontally centered with the centered document column; vertically
	// phase-locked to top content padding and scroll offset so rows track text.
	centerX := r.X + r.W*0.5
	originY := r.Y + s.canvasTopPad() - s.bodyScroll.Y

	firstX := centerX + float32(math.Ceil(float64((minX-centerX)/step)))*step
	firstY := originY + float32(math.Ceil(float64((minY-originY)/step)))*step

	p.Clip(r, 0, func() {
		if mode == "dots" {
			col := edCols.BgDotStandard
			dotR := float32(1.15)
			if high {
				col = edCols.BgDotHigh
				dotR = 1.6
			}
			dotR = max(float32(1.0), min(dotR*s.zoom(), float32(2.6)))
			d := dotR * 2
			for y := firstY; y <= maxY; y += step {
				for x := firstX; x <= maxX; x += step {
					p.Fill(ui.Rect{X: x - dotR, Y: y - dotR, W: d, H: d}, col, dotR)
				}
			}
			return
		}

		col := edCols.BgGridStandard
		if high {
			col = edCols.BgGridHigh
		}
		const lineW float32 = 1
		for y := firstY; y <= maxY; y += step {
			p.Fill(ui.Rect{X: minX, Y: y, W: maxX - minX, H: lineW}, col, 0)
		}
		for x := firstX; x <= maxX; x += step {
			p.Fill(ui.Rect{X: x, Y: minY, W: lineW, H: maxY - minY}, col, 0)
		}
	})
}
