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

// BackdropSpec configures an optional custom vector motif or paper-grain backdrop
// painted behind the editor canvas or full window (contributed by ui:appearance plugins).
type BackdropSpec struct {
	FillPaths   []string
	StrokePaths []string
	AccentPaths []string
	Pattern     string  // "scatter" | "grid" | "watermark" | "corners" | "paper_dots" | "paper_grid"
	Density     string  // "sparse" | "medium" | "dense"
	Contrast    string  // "standard" | "high" (for paper_dots / paper_grid)
	Scope       string  // "editor" (default) | "window"
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

// paperBackdropStep returns the unified paper dot/grid spacing shared across
// the window shell, empty stage, and editor canvas so full-window patterns align
// seamlessly across every region boundary.
func paperBackdropStep(z float32) float32 {
	if z <= 0 {
		z = 1
	}
	_, _, lhMode := theme.Typography()
	step := float32(math.Round(float64(theme.FontEditor * z * lhMode.Factor())))
	if step < 16 {
		step = 16
	}
	return step
}

// paintBorderAlignedPaperTiles renders paper dots or grid lines anchored to the
// canvas border: anchor.X and anchor.Y act as logical index 0 (the border itself),
// so the first drawn dot or grid line starts at (anchor.X+step, anchor.Y+step),
// and no dot or line is ever drawn on or clipped across the outer border.
func paintBorderAlignedPaperTiles(p *ui.Painter, anchor, clipRect ui.Rect, pal theme.Palette, step, zoom float32, mode string, high, chromeSurface bool, exclude ui.Rect) {
	if p == nil || anchor.W <= 0 || anchor.H <= 0 || clipRect.W <= 0 || clipRect.H <= 0 || step <= 0 {
		return
	}
	edCols := pal.Editor()

	if mode == "dots" {
		col := edCols.BgDotStandard
		dotR := float32(1.15)
		if high {
			col = edCols.BgDotHigh
			dotR = 1.6
		}
		dotR = max(float32(1.0), min(dotR*zoom, float32(2.6)))
		if chromeSurface {
			alphaScale := float32(0.78)
			if high {
				alphaScale = 0.62
			}
			col = col.Alpha(alphaScale)
			dotR = min(dotR, 1.3)
		}
		margin := dotR + 2
		maxI := int(math.Floor(float64((anchor.W - margin) / step)))
		maxJ := int(math.Floor(float64((anchor.H - margin) / step)))
		if maxI < 1 || maxJ < 1 {
			return
		}
		iStart := max(1, int(math.Ceil(float64((clipRect.X+dotR-anchor.X)/step))))
		iEnd := min(maxI, int(math.Floor(float64((clipRect.X+clipRect.W-dotR-anchor.X)/step))))
		jStart := max(1, int(math.Ceil(float64((clipRect.Y+dotR-anchor.Y)/step))))
		jEnd := min(maxJ, int(math.Floor(float64((clipRect.Y+clipRect.H-dotR-anchor.Y)/step))))
		if iStart > iEnd || jStart > jEnd {
			return
		}
		hasEx := exclude.W > 0 && exclude.H > 0
		exL, exT := exclude.X-dotR, exclude.Y-dotR
		exR, exB := exclude.X+exclude.W+dotR, exclude.Y+exclude.H+dotR
		d := dotR * 2
		p.Clip(clipRect, 0, func() {
			for j := jStart; j <= jEnd; j++ {
				y := anchor.Y + float32(j)*step
				for i := iStart; i <= iEnd; i++ {
					x := anchor.X + float32(i)*step
					if hasEx && x >= exL && x <= exR && y >= exT && y <= exB {
						continue
					}
					p.Fill(ui.Rect{X: x - dotR, Y: y - dotR, W: d, H: d}, col, dotR)
				}
			}
		})
		return
	}

	col := edCols.BgGridStandard
	if high {
		col = edCols.BgGridHigh
	}
	if chromeSurface {
		col = col.Alpha(0.72)
	}
	const (
		lineW      float32 = 1
		edgeMargin float32 = 2
	)
	maxI := int(math.Floor(float64((anchor.W - edgeMargin) / step)))
	maxJ := int(math.Floor(float64((anchor.H - edgeMargin) / step)))
	if maxI < 1 && maxJ < 1 {
		return
	}
	iStart := max(1, int(math.Ceil(float64((clipRect.X-lineW-anchor.X)/step))))
	iEnd := min(maxI, int(math.Floor(float64((clipRect.X+clipRect.W-anchor.X)/step))))
	jStart := max(1, int(math.Ceil(float64((clipRect.Y-lineW-anchor.Y)/step))))
	jEnd := min(maxJ, int(math.Floor(float64((clipRect.Y+clipRect.H-anchor.Y)/step))))
	if iStart > iEnd && jStart > jEnd {
		return
	}
	p.Clip(clipRect, 0, func() {
		for j := jStart; j <= jEnd; j++ {
			y := anchor.Y + float32(j)*step
			p.Fill(ui.Rect{X: clipRect.X, Y: y, W: clipRect.W, H: lineW}, col, 0)
		}
		for i := iStart; i <= iEnd; i++ {
			x := anchor.X + float32(i)*step
			p.Fill(ui.Rect{X: x, Y: clipRect.Y, W: lineW, H: clipRect.H}, col, 0)
		}
	})
}

// subtractBackdropRects splits area into non-overlapping visible sub-rectangles
// with every intersecting exclusion region removed.
func subtractBackdropRects(area ui.Rect, excludes ...ui.Rect) []ui.Rect {
	if area.W <= 0 || area.H <= 0 {
		return nil
	}
	rects := []ui.Rect{area}
	for _, ex := range excludes {
		if ex.W <= 0 || ex.H <= 0 {
			continue
		}
		exR := ex.X + ex.W
		exB := ex.Y + ex.H
		next := make([]ui.Rect, 0, len(rects)+3)
		for _, r := range rects {
			rR := r.X + r.W
			rB := r.Y + r.H
			ix0 := max(r.X, ex.X)
			iy0 := max(r.Y, ex.Y)
			ix1 := min(rR, exR)
			iy1 := min(rB, exB)
			if ix1 <= ix0 || iy1 <= iy0 {
				next = append(next, r)
				continue
			}
			// Top strip
			if iy0 > r.Y {
				next = append(next, ui.Rect{X: r.X, Y: r.Y, W: r.W, H: iy0 - r.Y})
			}
			// Bottom strip
			if iy1 < rB {
				next = append(next, ui.Rect{X: r.X, Y: iy1, W: r.W, H: rB - iy1})
			}
			// Left strip between iy0 and iy1
			if ix0 > r.X {
				next = append(next, ui.Rect{X: r.X, Y: iy0, W: ix0 - r.X, H: iy1 - iy0})
			}
			// Right strip between iy0 and iy1
			if ix1 < rR {
				next = append(next, ui.Rect{X: ix1, Y: iy0, W: rR - ix1, H: iy1 - iy0})
			}
		}
		rects = next
		if len(rects) == 0 {
			return nil
		}
	}
	return rects
}

// PaintBackdrop draws a plugin-contributed vector or paper-grain backdrop onto
// non-session editor-area surfaces such as the blank/empty page stage.
func PaintBackdrop(p *ui.Painter, r ui.Rect, pal theme.Palette, bd *BackdropSpec, chromeSurface bool) {
	PaintBackdropClipped(p, r, pal, bd, chromeSurface)
}

// PaintBackdropClipped draws a plugin-contributed backdrop across r anchored to
// r's border (treating r.X/r.Y as index 0) while excluding any specified regions.
func PaintBackdropClipped(p *ui.Painter, r ui.Rect, pal theme.Palette, bd *BackdropSpec, chromeSurface bool, excludes ...ui.Rect) {
	if p == nil || bd == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	visible := subtractBackdropRects(r, excludes...)
	if len(visible) == 0 {
		return
	}
	switch bd.Pattern {
	case "paper_dots", "paper_grid":
		mode := "dots"
		if bd.Pattern == "paper_grid" {
			mode = "grid"
		}
		z := theme.Zoom()
		if z <= 0 {
			z = 1
		}
		step := paperBackdropStep(z)
		for _, sub := range visible {
			paintBorderAlignedPaperTiles(p, r, sub, pal, step, z, mode, bd.Contrast == "high", chromeSurface, ui.Rect{})
		}
	default:
		if len(bd.StrokePaths) > 0 {
			for _, sub := range visible {
				paintVectorBackdropRect(p, sub, r, pal, 0, bd, chromeSurface, bd.Scope == "window")
			}
		}
	}
}

func (s *Session) paintBackground(p *ui.Painter, r ui.Rect, pal theme.Palette, edgeL, edgeR float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	if bd := s.bgStyle.Backdrop; bd != nil {
		fullWindow := bd.Scope == "window"
		switch bd.Pattern {
		case "paper_dots":
			s.paintGridOrDotsBackground(p, r, pal, edgeL, edgeR, "dots", bd.Contrast == "high", fullWindow)
			return
		case "paper_grid":
			s.paintGridOrDotsBackground(p, r, pal, edgeL, edgeR, "grid", bd.Contrast == "high", fullWindow)
			return
		default:
			if len(bd.StrokePaths) > 0 {
				s.paintVectorBackdrop(p, r, pal, edgeL, edgeR, bd)
				return
			}
		}
	}
	mode := s.bgStyle.Mode
	if mode == "dots" || mode == "grid" {
		s.paintGridOrDotsBackground(p, r, pal, edgeL, edgeR, mode, s.bgStyle.Contrast == "high", false)
	}
}

func backdropColors(pal theme.Palette, bd *BackdropSpec, chromeSurface bool) (fillCol, strokeCol, accentCol ui.Color) {
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
	if chromeSurface {
		op = max(op*0.72, 0.025)
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

func (s *Session) outlineBackdropExcludeRect(r ui.Rect) ui.Rect {
	if !s.outlineVisible() || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}
	}
	items := s.Outline()
	if len(items) == 0 {
		return ui.Rect{}
	}
	maxTicks := min(theme.OutlineMaxTicks, max(1, int((r.H-theme.Space24)/theme.OutlineTickHitHeight)))
	tickCount := min(len(items), maxTicks)
	railH := float32(tickCount) * theme.OutlineTickHitHeight
	const pad float32 = 6
	exX := max(r.X, 0) + theme.OutlineRailLeftOffset - pad
	exW := theme.OutlineRailWidth + pad*2
	exH := min(r.H, railH+pad*2)
	exY := r.Y + (r.H-exH)*0.5
	return ui.Rect{X: exX, Y: exY, W: exW, H: exH}
}

func (s *Session) paintVectorBackdrop(p *ui.Painter, r ui.Rect, pal theme.Palette, edgeL, edgeR float32, bd *BackdropSpec) {
	if bd.Scope == "window" {
		visible := subtractBackdropRects(r, s.outlineBackdropExcludeRect(r))
		for _, sub := range visible {
			paintVectorBackdropRect(p, sub, r, pal, 0, bd, false, true)
		}
		return
	}
	canvasLeft := r.X + edgeL
	canvasWidth := max(r.W-edgeL-edgeR, 0)
	if canvasWidth <= 0 {
		return
	}
	vis := ui.Rect{X: canvasLeft, Y: r.Y, W: canvasWidth, H: r.H}
	paintVectorBackdropRect(p, vis, vis, pal, s.bodyScroll.Y, bd, false, false)
}

func paintVectorBackdropRect(p *ui.Painter, vis, anchor ui.Rect, pal theme.Palette, scrollY float32, bd *BackdropSpec, chromeSurface, windowAligned bool) {
	if vis.W <= 0 || vis.H <= 0 || bd == nil {
		return
	}
	strokeSVG := cachedBackdropSVG("stroke", bd.StrokePaths)
	if strokeSVG == nil {
		return
	}
	fillSVG := cachedBackdropSVG("fill", bd.FillPaths)
	accentSVG := cachedBackdropSVG("accent", bd.AccentPaths)
	fillCol, strokeCol, accentCol := backdropColors(pal, bd, chromeSurface)

	p.Clip(vis, 0, func() {
		switch bd.Pattern {
		case "watermark":
			size := min(min(anchor.W, anchor.H)*0.34, float32(220))
			if size < 96 {
				size = 96
			}
			rect := ui.Rect{
				X: anchor.X + (anchor.W-size)*0.5,
				Y: anchor.Y + (anchor.H-size)*0.5,
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
				{anchor.X + pad, anchor.Y + pad, -10},
				{anchor.X + anchor.W - pad - size, anchor.Y + pad, 12},
				{anchor.X + pad, anchor.Y + anchor.H - pad - size, 8},
				{anchor.X + anchor.W - pad - size, anchor.Y + anchor.H - pad - size, -12},
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
			if windowAligned {
				startRow := int(math.Floor(float64((vis.Y - glyphSize) / stepY)))
				endRow := int(math.Ceil(float64((vis.Y + vis.H) / stepY)))
				startCol := int(math.Floor(float64((vis.X - stepX - glyphSize) / stepX)))
				endCol := int(math.Ceil(float64((vis.X + vis.W + stepX) / stepX)))
				drawn := 0
				for absRow := startRow; absRow <= endRow && drawn < maxBackdropTiles; absRow++ {
					y := float32(absRow) * stepY
					if y+glyphSize <= vis.Y || y >= vis.Y+vis.H {
						continue
					}
					rowOffset := float32(0)
					if scatter && (absRow&1) != 0 {
						rowOffset = stepX * 0.5
					}
					for col := startCol; col <= endCol && drawn < maxBackdropTiles; col++ {
						x := float32(col)*stepX + rowOffset
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
				return
			}
			// Gentle vertical parallax using absolute row indices so stagger and
			// per-tile rotation never jump when scrolling across tile boundaries.
			parallaxY := float64(scrollY) * 0.35
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

func (s *Session) paintGridOrDotsBackground(p *ui.Painter, r ui.Rect, pal theme.Palette, edgeL, edgeR float32, mode string, high, fullWindow bool) {
	if fullWindow {
		// In full-editor scope, treat the editor canvas border (r.X, r.Y) as the
		// logical 0-th grid line/dot so the first drawn cell is a full square and
		// no dot or line lands on the border, while carving out the outline rail.
		z := s.zoom()
		step := paperBackdropStep(z)
		outlineEx := s.outlineBackdropExcludeRect(r)
		if mode == "dots" {
			paintBorderAlignedPaperTiles(p, r, r, pal, step, z, mode, high, false, outlineEx)
			return
		}
		visible := subtractBackdropRects(r, outlineEx)
		for _, sub := range visible {
			paintBorderAlignedPaperTiles(p, r, sub, pal, step, z, mode, high, false, ui.Rect{})
		}
		return
	}

	fontSize, _, lh := s.textMetrics(model.BlockParagraph)
	step := float32(math.Round(float64(fontSize * lh)))
	if step < 16 {
		step = 16
	}

	// Confine editor-scoped background strictly to the centered text column so
	// it never intrudes on the top-left outline rail or outer shell margins.
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

	paintPaperTiles(p, r, pal, step, boxLeft, firstY, cols, rows, s.zoom(), mode, high, false, false)
}

func paintPaperTiles(p *ui.Painter, r ui.Rect, pal theme.Palette, step, firstX, firstY float32, cols, rows int, zoom float32, mode string, high, chromeSurface, fullSpan bool) {
	if cols < 0 || rows < 0 || step <= 0 {
		return
	}
	edCols := pal.Editor()

	p.Clip(r, 0, func() {
		if mode == "dots" {
			col := edCols.BgDotStandard
			dotR := float32(1.15)
			if high {
				col = edCols.BgDotHigh
				dotR = 1.6
			}
			dotR = max(float32(1.0), min(dotR*zoom, float32(2.6)))
			if chromeSurface {
				alphaScale := float32(0.78)
				if high {
					alphaScale = 0.62
				}
				col = col.Alpha(alphaScale)
				dotR = min(dotR, 1.3)
			}
			d := dotR * 2
			for j := 0; j <= rows; j++ {
				y := firstY + float32(j)*step
				if y+dotR < r.Y || y-dotR > r.Y+r.H {
					continue
				}
				for i := 0; i <= cols; i++ {
					x := firstX + float32(i)*step
					if x+dotR < r.X || x-dotR > r.X+r.W {
						continue
					}
					p.Fill(ui.Rect{X: x - dotR, Y: y - dotR, W: d, H: d}, col, dotR)
				}
			}
			return
		}

		col := edCols.BgGridStandard
		if high {
			col = edCols.BgGridHigh
		}
		if chromeSurface {
			col = col.Alpha(0.72)
		}
		const lineW float32 = 1
		lineX, lineWSpan := firstX, float32(cols)*step+lineW
		lineY, lineHSpan := firstY, float32(rows)*step+lineW
		if fullSpan {
			lineX, lineWSpan = r.X, r.W
			lineY, lineHSpan = r.Y, r.H
		}
		for j := 0; j <= rows; j++ {
			y := firstY + float32(j)*step
			p.Fill(ui.Rect{X: lineX, Y: y, W: lineWSpan, H: lineW}, col, 0)
		}
		for i := 0; i <= cols; i++ {
			x := firstX + float32(i)*step
			p.Fill(ui.Rect{X: x, Y: lineY, W: lineW, H: lineHSpan}, col, 0)
		}
	})
}
