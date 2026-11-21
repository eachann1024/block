package editor

import (
	"html"
	"math"
	"strings"
	"sync"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// BackdropSpec configures an optional custom vector motif backdrop painted
// behind the editor canvas (contributed by ui:appearance plugins).
type BackdropSpec struct {
	FillPaths   []string
	StrokePaths []string
	AccentPaths []string
	Pattern     string  // "scatter" | "grid" | "watermark" | "corners"
	Density     string  // "sparse" | "medium" | "dense"
	Opacity     float64 // clamped to [0.03, 0.25]
	Tint        string  // "accent" | "muted" | "warm" | "glow"
	DarkGlow    bool
}

// BackgroundStyle configures the optional decorative dot, grid, or custom
// vector backdrop rendered behind the editor canvas.
type BackgroundStyle struct {
	Mode     string // "off" | "dots" | "grid"
	Contrast string // "standard" | "high"
	Backdrop *BackdropSpec
}

var (
	backdropSVGMu    sync.RWMutex
	backdropSVGCache = make(map[string]*ui.SVG)
)

const (
	maxBackdropSVGCache = 128
	maxBackdropTiles    = 640
)

func cachedBackdropSVG(layer string, paths []string) *ui.SVG {
	if len(paths) == 0 {
		return nil
	}
	var b strings.Builder
	hasPath := false
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`)
	if layer == "fill" {
		b.WriteString(`<g fill="black" stroke="none">`)
	} else {
		b.WriteString(`<g fill="none" stroke="black" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round">`)
	}
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		hasPath = true
		b.WriteString(`<path d="`)
		b.WriteString(html.EscapeString(trimmed))
		b.WriteString(`"/>`)
	}
	if !hasPath {
		return nil
	}
	b.WriteString(`</g></svg>`)
	raw := b.String()

	backdropSVGMu.RLock()
	cached, ok := backdropSVGCache[raw]
	backdropSVGMu.RUnlock()
	if ok {
		return cached
	}
	parsed, err := ui.ParseSVG([]byte(raw))
	if err != nil {
		parsed = nil
	}
	backdropSVGMu.Lock()
	if len(backdropSVGCache) >= maxBackdropSVGCache {
		clear(backdropSVGCache)
	}
	backdropSVGCache[raw] = parsed
	backdropSVGMu.Unlock()
	return parsed
}

// SetBackground updates the editor's decorative background style for this frame.
func (s *Session) SetBackground(bg BackgroundStyle) {
	s.bgStyle = bg
}

func (s *Session) paintBackground(p *ui.Painter, r ui.Rect, pal theme.Palette, edgeL, edgeR float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	if s.bgStyle.Backdrop != nil && len(s.bgStyle.Backdrop.StrokePaths) > 0 {
		s.paintVectorBackdrop(p, r, pal, edgeL, edgeR, s.bgStyle.Backdrop)
		return
	}
	mode := s.bgStyle.Mode
	if mode == "dots" || mode == "grid" {
		s.paintGridOrDotsBackground(p, r, pal, edgeL, edgeR, mode)
	}
}

func backdropColors(pal theme.Palette, bd *BackdropSpec) (fillCol, strokeCol, accentCol ui.Color) {
	op := float32(bd.Opacity)
	if op < 0.03 {
		op = 0.03
	}
	if op > 0.25 {
		op = 0.25
	}
	glow := pal.Dark && (bd.DarkGlow || bd.Tint == "glow")
	var base ui.Color
	switch bd.Tint {
	case "muted":
		base = pal.TextSecondary
	case "warm":
		base = pal.Warning
	case "glow":
		if pal.Dark {
			base = pal.Success
		} else {
			base = pal.Accent
		}
	default:
		base = pal.Accent
	}
	if glow {
		base = pal.Success
		op = min(op*1.35, 0.25)
	}
	fillCol = base.Alpha(op * 0.45)
	strokeCol = base.Alpha(op)
	accentCol = base.Alpha(min(op*1.15, 0.25))
	return fillCol, strokeCol, accentCol
}

func drawBackdropGlyph(p *ui.Painter, fillSVG, strokeSVG, accentSVG *ui.SVG, rect ui.Rect, deg float32, fillCol, strokeCol, accentCol ui.Color) {
	draw := func() {
		if fillSVG != nil && fillCol.A > 0 {
			p.Icon(fillSVG, rect, fillCol)
		}
		if strokeSVG != nil && strokeCol.A > 0 {
			p.Icon(strokeSVG, rect, strokeCol)
		}
		if accentSVG != nil && accentCol.A > 0 {
			p.Icon(accentSVG, rect, accentCol)
		}
	}
	if deg != 0 {
		cx := rect.X + rect.W*0.5
		cy := rect.Y + rect.H*0.5
		p.Transform(ui.Rotation(deg), cx, cy, draw)
		return
	}
	draw()
}

func (s *Session) paintVectorBackdrop(p *ui.Painter, r ui.Rect, pal theme.Palette, edgeL, edgeR float32, bd *BackdropSpec) {
	strokeSVG := cachedBackdropSVG("stroke", bd.StrokePaths)
	if strokeSVG == nil {
		return
	}
	fillSVG := cachedBackdropSVG("fill", bd.FillPaths)
	accentSVG := cachedBackdropSVG("accent", bd.AccentPaths)
	fillCol, strokeCol, accentCol := backdropColors(pal, bd)

	canvasLeft := r.X + edgeL
	canvasWidth := max(r.W-edgeL-edgeR, 0)
	if canvasWidth <= 0 {
		return
	}
	vis := ui.Rect{X: canvasLeft, Y: r.Y, W: canvasWidth, H: r.H}

	p.Clip(vis, 0, func() {
		switch bd.Pattern {
		case "watermark":
			size := min(min(vis.W, vis.H)*0.34, float32(220))
			if size < 96 {
				size = 96
			}
			rect := ui.Rect{
				X: vis.X + (vis.W-size)*0.5,
				Y: vis.Y + (vis.H-size)*0.5,
				W: size,
				H: size,
			}
			drawBackdropGlyph(p, fillSVG, strokeSVG, accentSVG, rect, -12, fillCol, strokeCol, accentCol)

		case "corners":
			size := float32(56)
			pad := float32(28)
			switch bd.Density {
			case "sparse":
				size = 44
			case "dense":
				size = 72
			}
			pts := [4]struct {
				x, y, deg float32
			}{
				{vis.X + pad, vis.Y + pad, -10},
				{vis.X + vis.W - pad - size, vis.Y + pad, 12},
				{vis.X + pad, vis.Y + vis.H - pad - size, 8},
				{vis.X + vis.W - pad - size, vis.Y + vis.H - pad - size, -12},
			}
			for _, pt := range pts {
				drawBackdropGlyph(p, fillSVG, strokeSVG, accentSVG, ui.Rect{X: pt.x, Y: pt.y, W: size, H: size}, pt.deg, fillCol, strokeCol, accentCol)
			}

		default: // "scatter" or "grid"
			stepX, stepY, glyphSize := float32(116), float32(104), float32(40)
			switch bd.Density {
			case "sparse":
				stepX, stepY, glyphSize = 156, 140, 36
			case "dense":
				stepX, stepY, glyphSize = 88, 80, 44
			}
			scatter := bd.Pattern != "grid"
			// Gentle vertical parallax using absolute row indices so stagger and
			// per-tile rotation never jump when scrolling across tile boundaries.
			parallaxY := float64(s.bodyScroll.Y) * 0.35
			baseRow := int(math.Floor(parallaxY / float64(stepY)))
			offsetY := float32(parallaxY - float64(baseRow)*float64(stepY))
			startRow := -1
			endRow := int(math.Ceil(float64((vis.H+offsetY)/stepY))) + 1
			cols := int(math.Ceil(float64(vis.W/stepX))) + 2
			drawn := 0
			for row := startRow; row <= endRow && drawn < maxBackdropTiles; row++ {
				absRow := baseRow + row
				y := vis.Y + float32(row)*stepY - offsetY
				if y+glyphSize <= vis.Y || y >= vis.Y+vis.H {
					continue
				}
				rowOffset := float32(0)
				if scatter && (absRow&1) != 0 {
					rowOffset = stepX * 0.5
				}
				for col := -1; col <= cols && drawn < maxBackdropTiles; col++ {
					x := vis.X + float32(col)*stepX + rowOffset
					if x+glyphSize <= vis.X || x >= vis.X+vis.W {
						continue
					}
					var deg float32
					if scatter {
						hash := ((absRow*7+col*13)%5 + 5) % 5
						deg = float32(hash-2) * 9
					}
					drawBackdropGlyph(p, fillSVG, strokeSVG, accentSVG, ui.Rect{X: x, Y: y, W: glyphSize, H: glyphSize}, deg, fillCol, strokeCol, accentCol)
					drawn++
				}
			}
		}
	})
}

func (s *Session) paintGridOrDotsBackground(p *ui.Painter, r ui.Rect, pal theme.Palette, edgeL, edgeR float32, mode string) {

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
