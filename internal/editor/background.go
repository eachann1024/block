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

func (s *Session) paintBackground(p *ui.Painter, r ui.Rect, pal theme.Palette, edgeL, edgeR float32) {
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

	// Confine the background strictly to the centered text column so it never
	// intrudes on the top-left outline rail or outer shell margins.
	canvasLeft := r.X + edgeL
	canvasWidth := max(r.W-edgeL-edgeR, 0)
	if canvasWidth <= 0 {
		return
	}
	flowWidth := min(canvasWidth, s.outerFlowWidth())
	flowLeft := canvasLeft + (canvasWidth-flowWidth)*0.5
	textLeft := flowLeft + s.leftContentGutter()
	textRight := flowLeft + flowWidth - s.rightContentInset()
	textW := textRight - textLeft
	if textW <= 0 {
		return
	}

	// Symmetric breathing margin around the text column (1 grid step on left,
	// right, and top), snapped to an integer column count for clean edges.
	availL := canvasLeft + 12
	availR := canvasLeft + canvasWidth - 18
	maxAvailW := availR - availL
	if maxAvailW < step {
		return
	}

	wantW := min(textW+2*step, maxAvailW)
	cols := max(1, int(math.Round(float64(wantW/step))))
	for float32(cols)*step > maxAvailW && cols > 1 {
		cols--
	}
	gridW := float32(cols) * step
	centerX := (textLeft + textRight) * 0.5
	boxLeft := float32(math.Round(float64(centerX - gridW*0.5)))
	if boxLeft < availL {
		boxLeft = availL
	}
	if boxLeft+gridW > availR {
		boxLeft = availR - gridW
	}

	// Align the top starting whitespace above the first text line with the
	// exact horizontal padding around the text column (clamped for compact
	// minimal layouts so the first row never starts below the first text line).
	vpMargin := min(float32(12), max(s.canvasTopPad()*0.5, 2))
	sidePad := max((gridW-textW)*0.5, 0)
	topPadOffset := min(sidePad, max(s.canvasTopPad()-vpMargin, 0))
	docTop := float32(math.Floor(float64(r.Y + s.canvasTopPad() - topPadOffset - s.bodyScroll.Y)))

	minVisY := r.Y + vpMargin
	if s.bodyScroll.Y <= 0 && docTop < minVisY {
		minVisY = docTop
	}
	maxVisY := r.Y + r.H - vpMargin
	if maxVisY <= minVisY {
		return
	}

	firstY := docTop
	if firstY < minVisY {
		firstY = docTop + float32(math.Ceil(float64((minVisY-docTop)/step)))*step
	}
	if firstY > maxVisY {
		return
	}
	rows := int(math.Floor(float64((maxVisY - firstY) / step)))
	if rows < 0 {
		return
	}

	edCols := pal.Editor()
	high := s.bgStyle.Contrast == "high"

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
			for j := 0; j <= rows; j++ {
				y := firstY + float32(j)*step
				for i := 0; i <= cols; i++ {
					x := boxLeft + float32(i)*step
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
		gridH := float32(rows) * step
		for j := 0; j <= rows; j++ {
			y := firstY + float32(j)*step
			p.Fill(ui.Rect{X: boxLeft, Y: y, W: gridW + lineW, H: lineW}, col, 0)
		}
		for i := 0; i <= cols; i++ {
			x := boxLeft + float32(i)*step
			p.Fill(ui.Rect{X: x, Y: firstY, W: lineW, H: gridH + lineW}, col, 0)
		}
	})
}
