package ui

import (
	"math"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

const (
	maxTabColumn           = 256
	selectionFullLineLimit = 200
	visibleWindowPad       = 32
	firstFrameMinLastLine  = 80
)

type lineSpan struct {
	from, to int
}

type byteRange struct {
	start, end int
}

type deferredTabArrow struct {
	line  int
	xOff  float32
	width float32
}

// EditorTabWhitespace replaces visible tab characters in codeed.View with
// full-width arrow rendering so tab arrows span the tab-stop width with
// symmetric 2 DIP insets instead of stopping at 2*charWidth.
//
// Under SoftWrap=true, leading tabs within half of the wrap width keep plain
// ASCII space replacements in mygo (preserving wrapIndent, line-break points,
// and selection hit-testing) and paint the arrow in Observe via DrawOver.
type EditorTabWhitespace struct {
	initialized       bool
	lastState         *codeed.State
	lastBuf           *codeed.Buffer
	lastBufVer        uint64
	lastMode          codeed.Whitespace
	lastTabSize       int
	lastFont          ui.Font
	lastFontEpoch     uint64
	lastSoftWrap      bool
	lastWrapAvailW    float32
	lastWinFrom       int
	lastWinTo         int
	lastSels          []codeed.Selection
	hasReplacements   bool
	windowClipped     bool
	hasLeadingTabs    bool
	lastEditorW       float32
	lastScale         float32
	toggledNumbers    bool

	widthsValid       bool
	measuredFont      ui.Font
	measuredTabSize   int
	measuredFontEpoch uint64
	charW             float32
	tabWidths         []float32

	repls          []codeed.Replacement
	deferredArrows []deferredTabArrow
	spansBuf       []lineSpan
	rangesBuf      []byteRange
}

// Prepare runs before codeed.View each frame. It installs tab replacements for
// visible tabs under st.Whitespace and keeps subsequent unselected tabs on the
// same line aligned to their original tab stops.
func (h *EditorTabWhitespace) Prepare(c *ui.Context, st *codeed.State) {
	if h == nil || c == nil || st == nil || st.Buffer == nil {
		return
	}
	if h.lastState != st {
		h.hasReplacements = false
		h.toggledNumbers = false
		h.lastEditorW = 0
	}

	font := normalizeEditorFont(st.Font)
	tabSize := resolveEditorTabSize(st)
	fontEpoch := theme.FontEpoch()
	h.ensureTabWidths(c, font, tabSize, fontEpoch)

	mode := st.Whitespace
	buf := st.Buffer
	snap := buf.Snapshot()
	bufVer := snap.Version()
	lineCount := snap.LineCount()
	availWrapW := h.computeAvailWrapW(c, st, font, lineCount)

	var sels []codeed.Selection
	if mode == codeed.WhitespaceSelection {
		sels = st.Selections()
	}

	winFrom, winTo := -1, -1
	var lineSpans []lineSpan
	hasNonEmptySel := false
	clipped := false

	switch mode {
	case codeed.WhitespaceNone:
	case codeed.WhitespaceSelection:
		lineSpans, hasNonEmptySel = h.selectionLineSpans(snap, sels)
		if hasNonEmptySel && countSpanLines(lineSpans) > selectionFullLineLimit {
			clipped = true
			winFrom, winTo = liveVisibleLineWindow(st, lineCount)
			lineSpans = intersectLineSpans(lineSpans, winFrom, winTo)
		}
	default:
		if lineCount > 0 {
			clipped = true
			winFrom, winTo = liveVisibleLineWindow(st, lineCount)
			if winFrom <= winTo {
				h.spansBuf = append(h.spansBuf[:0], lineSpan{from: winFrom, to: winTo})
				lineSpans = h.spansBuf
			}
		}
	}

	sameSels := mode != codeed.WhitespaceSelection || slices.Equal(h.lastSels, sels)
	if h.initialized &&
		h.lastState == st &&
		h.lastBuf == buf &&
		h.lastBufVer == bufVer &&
		h.lastMode == mode &&
		h.lastTabSize == tabSize &&
		h.lastFont == font &&
		h.lastFontEpoch == fontEpoch &&
		h.lastSoftWrap == st.SoftWrap &&
		h.lastWrapAvailW == availWrapW &&
		h.lastWinFrom == winFrom &&
		h.lastWinTo == winTo &&
		h.windowClipped == clipped &&
		sameSels {
		return
	}

	h.storeCache(st, buf, bufVer, mode, tabSize, font, fontEpoch, st.SoftWrap, availWrapW, winFrom, winTo, clipped, sels)

	if mode == codeed.WhitespaceNone || (mode == codeed.WhitespaceSelection && !hasNonEmptySel) || len(lineSpans) == 0 {
		h.deferredArrows = h.deferredArrows[:0]
		h.hasLeadingTabs = false
		if h.hasReplacements {
			st.SetReplacements(nil)
			h.hasReplacements = false
		}
		return
	}

	var selRanges []byteRange
	if mode == codeed.WhitespaceSelection {
		selRanges = h.selectionByteRanges(sels)
	}

	h.repls, h.deferredArrows, h.hasLeadingTabs = h.buildReplacements(snap, lineSpans, mode, tabSize, selRanges, st, availWrapW)
	if len(h.repls) == 0 {
		if h.hasReplacements {
			st.SetReplacements(nil)
			h.hasReplacements = false
		}
		return
	}
	st.SetReplacements(h.repls)
	h.hasReplacements = true
}

// Observe runs right after codeed.View each frame. It tracks editor width and
// line-number toggles, invalidates if doReveal moved past the prepared window,
// and paints deferred leading-tab arrows via DrawOver.
func (h *EditorTabWhitespace) Observe(c *ui.Context, ed ui.Element, st *codeed.State, findBarBounds ui.Rect) {
	if h == nil || c == nil || st == nil || st.Buffer == nil || !ed.Valid() {
		return
	}

	if w := ed.Bounds().W; w > 0 && w != h.lastEditorW {
		h.lastEditorW = w
		if st.SoftWrap && h.hasLeadingTabs {
			c.Invalidate()
		}
	}

	if runtime.GOOS == "darwin" {
		ed.ObserveInput(func(ev ui.InputEvent) {
			if ed.Valid() && ed.Focused() && ev.Kind == ui.InputKeyDown && ev.Mods == ui.Cmd && ev.Key == ui.KeySemicolon {
				h.toggledNumbers = !h.toggledNumbers
			}
		})
	}

	if h.windowClipped && h.lastState == st {
		lineCount := st.Buffer.Snapshot().LineCount()
		if curFirst, curLast, ok := currentVisibleLines(st, lineCount); ok {
			if curFirst < h.lastWinFrom || curLast > h.lastWinTo {
				c.Invalidate()
			}
		}
	}

	if len(h.deferredArrows) == 0 {
		return
	}

	font := normalizeEditorFont(st.Font)
	tabSize := resolveEditorTabSize(st)
	h.ensureTabWidths(c, font, tabSize, theme.FontEpoch())
	col := resolveWhitespaceColor(c, st)
	lineCount := st.Buffer.Snapshot().LineCount()
	charW := h.charW
	toggledNumbers := h.toggledNumbers
	arrows := slices.Clone(h.deferredArrows)

	ed.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 || len(arrows) == 0 {
			return
		}
		h.lastEditorW = r.W
		scale := p.Scale()
		if scale <= 0 {
			scale = 1
		}
		h.lastScale = scale

		rowH := editorRowHeight(st, font, scale)
		gutterW := editorGutterWidth(st, lineCount, charW, rowH, toggledNumbers)
		textX := editorTextX(gutterW)
		textClip := ui.Rect{X: r.X + gutterW, Y: r.Y, W: max(0, r.W-gutterW), H: r.H}
		if textClip.W <= 0 || textClip.H <= 0 {
			return
		}

		scrollY := st.ScrollY()
		vis := p.Visible()
		yMin := max(textClip.Y, vis.Y)
		yMax := min(textClip.Y+textClip.H, vis.Y+vis.H)

		p.Clip(textClip, 0, func() {
			for _, da := range arrows {
				top := st.LineTop(da.line)
				nextTop := st.LineTop(da.line + 1)
				if nextTop <= top+0.5 {
					continue
				}
				y := r.Y + float32(top-scrollY)
				if y+rowH < yMin || y > yMax {
					continue
				}
				arrowRect := ui.Rect{
					X: r.X + textX + da.xOff,
					Y: y,
					W: da.width,
					H: rowH,
				}
				paintDeferredTabArrow(p, arrowRect, col, findBarBounds)
			}
		})
	})
}

func (h *EditorTabWhitespace) storeCache(
	st *codeed.State,
	buf *codeed.Buffer,
	bufVer uint64,
	mode codeed.Whitespace,
	tabSize int,
	font ui.Font,
	fontEpoch uint64,
	softWrap bool,
	wrapAvailW float32,
	winFrom, winTo int,
	clipped bool,
	sels []codeed.Selection,
) {
	h.initialized = true
	h.lastState = st
	h.lastBuf = buf
	h.lastBufVer = bufVer
	h.lastMode = mode
	h.lastTabSize = tabSize
	h.lastFont = font
	h.lastFontEpoch = fontEpoch
	h.lastSoftWrap = softWrap
	h.lastWrapAvailW = wrapAvailW
	h.lastWinFrom = winFrom
	h.lastWinTo = winTo
	h.windowClipped = clipped
	if mode == codeed.WhitespaceSelection {
		h.lastSels = append(h.lastSels[:0], sels...)
	} else {
		h.lastSels = h.lastSels[:0]
	}
}

func (h *EditorTabWhitespace) ensureTabWidths(c *ui.Context, font ui.Font, tabSize int, fontEpoch uint64) {
	if h.widthsValid &&
		h.measuredFont == font &&
		h.measuredTabSize == tabSize &&
		h.measuredFontEpoch == fontEpoch &&
		len(h.tabWidths) == tabSize+1 &&
		h.charW > 0 {
		return
	}

	shapeFont := ui.Font{
		Family:   font.Family,
		Size:     font.Size,
		Weight:   font.Weight,
		Italic:   font.Italic,
		Features: font.Features,
	}

	charW := shapeWidth(ui.Shape("0", shapeFont))
	if charW <= 0 && c != nil {
		charW, _ = c.MeasureText(0, ui.Span{
			Text:     "0",
			Font:     font.Family,
			Size:     font.Size,
			Weight:   font.Weight,
			Italic:   font.Italic,
			Features: font.Features,
			Lang:     font.Lang,
		})
	}
	if charW <= 0 {
		charW = font.Size * 0.6
	}
	h.charW = charW

	span := ui.Span{
		Text:     "00",
		Font:     font.Family,
		Size:     font.Size,
		Weight:   font.Weight,
		Italic:   font.Italic,
		Features: font.Features,
		Lang:     font.Lang,
	}
	var w00, shapeW00 float32
	measuredBase := false
	shapedBase := false

	widths := slices.Grow(h.tabWidths[:0], tabSize+1)[:tabSize+1]
	widths[0] = 0
	for k := 1; k <= tabSize; k++ {
		w := shapeWidth(ui.Shape(spaces(k), shapeFont))
		if w <= 0 {
			if !shapedBase {
				shapeW00 = shapeWidth(ui.Shape("00", shapeFont))
				shapedBase = true
			}
			if shapeW00 > 0 {
				w = shapeWidth(ui.Shape("0"+spaces(k)+"0", shapeFont)) - shapeW00
			}
		}
		if w <= 0 {
			if c != nil {
				if !measuredBase {
					w00, _ = c.MeasureText(0, span)
					measuredBase = true
				}
				wk, _ := c.MeasureText(0, ui.Span{
					Text:     "0" + spaces(k) + "0",
					Font:     font.Family,
					Size:     font.Size,
					Weight:   font.Weight,
					Italic:   font.Italic,
					Features: font.Features,
					Lang:     font.Lang,
				})
				w = wk - w00
			}
			if w <= 0 {
				w = float32(k) * font.Size * 0.6
			}
		}
		widths[k] = w
	}

	h.tabWidths = widths
	h.measuredFont = font
	h.measuredTabSize = tabSize
	h.measuredFontEpoch = fontEpoch
	h.widthsValid = true
}

func shapeWidth(glyphs []ui.Glyph) float32 {
	var w float32
	for _, g := range glyphs {
		if r := g.X + g.Advance; r > w {
			w = r
		}
	}
	return w
}

func (h *EditorTabWhitespace) spacesWidth(cols, tabSize int) float32 {
	if cols <= 0 || tabSize <= 0 || len(h.tabWidths) <= tabSize {
		return 0
	}
	if cols <= tabSize {
		return h.tabWidths[cols]
	}
	full := cols / tabSize
	rem := cols % tabSize
	return float32(full)*h.tabWidths[tabSize] + h.tabWidths[rem]
}

func (h *EditorTabWhitespace) computeAvailWrapW(c *ui.Context, st *codeed.State, font ui.Font, lineCount int) float32 {
	if !st.SoftWrap || h.lastEditorW <= 0 {
		return 0
	}
	scale := h.lastScale
	if scale <= 0 {
		scale = 1
	}
	rowH := editorRowHeight(st, font, scale)
	gutterW := editorGutterWidth(st, lineCount, h.charW, rowH, h.toggledNumbers)
	textX := editorTextX(gutterW)
	trackW := editorTrackWidth(c, st)
	ww := h.lastEditorW - textX - trackW - 4
	if st.WrapColumn > 0 {
		ww = min(ww, float32(st.WrapColumn)*h.charW)
	}
	return float32(math.Floor(float64(max(ww, h.charW*8))))
}

func (h *EditorTabWhitespace) selectionLineSpans(snap codeed.Snapshot, sels []codeed.Selection) ([]lineSpan, bool) {
	spans := h.spansBuf[:0]
	n := snap.Len()
	for _, s := range sels {
		if s.Empty() {
			continue
		}
		a := max(0, min(s.Start(), n))
		b := max(0, min(s.End(), n))
		if b <= a {
			continue
		}
		l0 := snap.LineOf(a)
		l1 := snap.LineOf(b)
		if l1 > l0 && b == snap.LineStart(l1) {
			l1--
		}
		spans = append(spans, lineSpan{from: l0, to: l1})
	}
	if len(spans) == 0 {
		h.spansBuf = spans
		return nil, false
	}
	slices.SortFunc(spans, func(x, y lineSpan) int {
		if x.from != y.from {
			return x.from - y.from
		}
		return x.to - y.to
	})
	merged := spans[:1]
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.from <= last.to+1 {
			last.to = max(last.to, sp.to)
			continue
		}
		merged = append(merged, sp)
	}
	h.spansBuf = merged
	return merged, true
}

func (h *EditorTabWhitespace) selectionByteRanges(sels []codeed.Selection) []byteRange {
	ranges := h.rangesBuf[:0]
	for _, s := range sels {
		if s.Empty() {
			continue
		}
		ranges = append(ranges, byteRange{start: s.Start(), end: s.End()})
	}
	if len(ranges) <= 1 {
		h.rangesBuf = ranges
		return ranges
	}
	slices.SortFunc(ranges, func(x, y byteRange) int {
		if x.start != y.start {
			return x.start - y.start
		}
		return x.end - y.end
	})
	merged := ranges[:1]
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.start <= last.end {
			last.end = max(last.end, r.end)
			continue
		}
		merged = append(merged, r)
	}
	h.rangesBuf = merged
	return merged
}

func (h *EditorTabWhitespace) buildReplacements(
	snap codeed.Snapshot,
	lineSpans []lineSpan,
	mode codeed.Whitespace,
	tabSize int,
	selRanges []byteRange,
	st *codeed.State,
	availWrapW float32,
) ([]codeed.Replacement, []deferredTabArrow, bool) {
	repls := h.repls[:0]
	arrows := h.deferredArrows[:0]
	hasLeadingTabs := false

	contentFn := func(ctx *ui.Context, e ui.Element, _ codeed.Replacement) {
		col := resolveWhitespaceColor(ctx, st)
		e.PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
			paintTabArrow(p, r, col)
		})
	}

	canDeferLeading := func(colStart, cols int) bool {
		if !st.SoftWrap {
			return false
		}
		if availWrapW > 0 {
			return h.spacesWidth(colStart+cols, tabSize) <= availWrapW*0.5
		}
		return colStart+cols <= 16
	}

	si := 0
	for _, sp := range lineSpans {
		for line := sp.from; line <= sp.to; line++ {
			lineText := snap.Line(line)
			if strings.IndexByte(lineText, '\t') < 0 {
				continue
			}
			lineStart := snap.LineStart(line)
			trailFrom := 0
			if mode == codeed.WhitespaceTrailing {
				trailFrom = len(strings.TrimRight(lineText, " \t"))
			}

			col := 0
			inLeadingWS := true
			// Once a line contains a Content replacement (rendered as a 1-rune
			// object char), subsequent unselected tabs on the same line need
			// explicit spaces(cols) so mygo's tab-stop column count stays aligned.
			lineHasContentRepl := false
			for i := 0; i < len(lineText); {
				r, size := utf8.DecodeRuneInString(lineText[i:])
				if r != '\t' {
					if r != ' ' {
						inLeadingWS = false
					}
					col++
					i += size
					continue
				}
				if inLeadingWS {
					hasLeadingTabs = true
				}
				colStart := col
				cols := tabSize - col%tabSize
				if col >= maxTabColumn {
					cols = 1
				}
				col += cols

				show := false
				switch mode {
				case codeed.WhitespaceAll, codeed.WhitespaceBoundary:
					show = true
				case codeed.WhitespaceTrailing:
					show = i >= trailFrom
				case codeed.WhitespaceSelection:
					off := lineStart + i
					for si < len(selRanges) && selRanges[si].end <= off {
						si++
					}
					show = si < len(selRanges) && off >= selRanges[si].start
				}

				if show {
					if inLeadingWS && canDeferLeading(colStart, cols) {
						xOff := h.spacesWidth(colStart, tabSize)
						width := h.spacesWidth(colStart+cols, tabSize) - xOff
						repls = append(repls, codeed.Replacement{
							Start: lineStart + i,
							End:   lineStart + i + 1,
							Text:  spaces(cols),
						})
						arrows = append(arrows, deferredTabArrow{
							line:  line,
							xOff:  xOff,
							width: width,
						})
					} else {
						lineHasContentRepl = true
						repls = append(repls, codeed.Replacement{
							Start:   lineStart + i,
							End:     lineStart + i + 1,
							Width:   h.tabWidths[cols],
							Content: contentFn,
						})
					}
				} else if lineHasContentRepl {
					repls = append(repls, codeed.Replacement{
						Start: lineStart + i,
						End:   lineStart + i + 1,
						Text:  spaces(cols),
					})
				}
				i += size
			}
		}
	}
	return repls, arrows, hasLeadingTabs
}

func resolveWhitespaceColor(c *ui.Context, st *codeed.State) ui.Color {
	if st != nil && st.Theme != nil && st.Theme.Whitespace != (ui.Color{}) {
		return st.Theme.Whitespace
	}
	if c != nil {
		if t := c.Theme(); t != nil {
			if col := codeed.DefaultTheme(t).Whitespace; col != (ui.Color{}) {
				return col
			}
		}
	}
	return theme.Ocean().GutterText
}

func paintTabArrow(p *ui.Painter, r ui.Rect, col ui.Color) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	inset := min(float32(2), r.W*0.2)
	x0 := r.X + inset
	x1 := r.X + r.W - inset
	w := x1 - x0
	if w <= 0 {
		return
	}
	cy := r.Y + r.H/2
	head := min(float32(3), w*0.5)
	var path ui.Path
	path.MoveTo(x0, cy).LineTo(x1, cy)
	path.MoveTo(x1-head, cy-head).LineTo(x1, cy).LineTo(x1-head, cy+head)
	p.StrokePath(&path, 1, col)
}

func paintDeferredTabArrow(p *ui.Painter, arrowRect ui.Rect, col ui.Color, findBarBounds ui.Rect) {
	if findBarBounds.W <= 0 || findBarBounds.H <= 0 || !rectsIntersect(arrowRect, findBarBounds) {
		paintTabArrow(p, arrowRect, col)
		return
	}
	inset := min(float32(2), arrowRect.W*0.2)
	w := arrowRect.W - 2*inset
	if w <= 0 {
		return
	}
	cy := arrowRect.Y + arrowRect.H/2
	head := min(float32(3), w*0.5)
	strokeTop := cy - head - 0.5
	strokeBottom := cy + head + 0.5
	if strokeBottom <= findBarBounds.Y {
		clip := ui.Rect{
			X: arrowRect.X,
			Y: arrowRect.Y,
			W: arrowRect.W,
			H: max(0, findBarBounds.Y-arrowRect.Y),
		}
		p.Clip(clip, 0, func() { paintTabArrow(p, arrowRect, col) })
		return
	}
	if strokeTop >= findBarBounds.Y+findBarBounds.H {
		clipY := findBarBounds.Y + findBarBounds.H
		clip := ui.Rect{
			X: arrowRect.X,
			Y: clipY,
			W: arrowRect.W,
			H: max(0, arrowRect.Y+arrowRect.H-clipY),
		}
		p.Clip(clip, 0, func() { paintTabArrow(p, arrowRect, col) })
	}
}

func rectsIntersect(a, b ui.Rect) bool {
	return a.W > 0 && a.H > 0 && b.W > 0 && b.H > 0 &&
		a.X < b.X+b.W && a.X+a.W > b.X &&
		a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

func editorRowHeight(st *codeed.State, font ui.Font, scale float32) float32 {
	lhMult := st.LineHeight
	if lhMult <= 0 {
		lhMult = 1.5
	}
	return float32(math.Round(float64(font.Size*lhMult*scale))) / scale
}

func editorGutterWidth(st *codeed.State, lineCount int, charW, rowH float32, toggledNumbers bool) float32 {
	numbersOn := (st.LineNumbers != codeed.LineNumbersOff) != toggledNumbers
	w := float32(0)
	if numbersOn {
		digits := max(3, len(strconv.Itoa(max(1, lineCount))))
		w += float32(digits)*charW + charW*1.5
	}
	if !st.NoFolding {
		w += rowH * 0.8
	}
	if st.DiffBase != nil {
		w += 6
	}
	if len(st.GutterMarks) > 0 && !numbersOn {
		w += rowH * 0.7
	}
	if w > 0 {
		w += 4
	}
	return float32(math.Ceil(float64(w)))
}

func editorTextX(gutterW float32) float32 {
	pad := float32(4)
	if gutterW == 0 {
		pad = 8
	}
	return gutterW + pad
}

func editorTrackWidth(c *ui.Context, st *codeed.State) float32 {
	if st.FitContent || st.NoScrollbars {
		return 0
	}
	if st.Scrollbar.TrackWidth > 0 {
		return st.Scrollbar.TrackWidth
	}
	if c != nil {
		if t := c.Theme(); t != nil && t.ScrollbarStyle.TrackWidth > 0 {
			return t.ScrollbarStyle.TrackWidth
		}
	}
	return 10
}

func normalizeEditorFont(f ui.Font) ui.Font {
	if strings.TrimSpace(f.Family) == "" {
		f.Family = "monospace"
	}
	if f.Size <= 0 {
		f.Size = 13
	}
	return f
}

func resolveEditorTabSize(st *codeed.State) int {
	tabSize := st.TabSize
	if tabSize <= 0 && st.Buffer != nil {
		if lang := st.Buffer.Language(); lang != nil && lang.TabSize > 0 {
			tabSize = lang.TabSize
		}
	}
	if tabSize <= 0 {
		tabSize = 4
	}
	return max(1, min(tabSize, 16))
}

func currentVisibleLines(st *codeed.State, lineCount int) (int, int, bool) {
	if lineCount <= 0 {
		return 0, -1, false
	}
	if viewH := st.ViewportHeight(); viewH > 0 {
		y0 := st.ScrollY()
		y1 := y0 + viewH
		first := sort.Search(lineCount, func(i int) bool {
			return st.LineTop(i+1) > y0
		})
		if first >= lineCount {
			first = lineCount - 1
		}
		last := sort.Search(lineCount, func(i int) bool {
			return st.LineTop(i+1) >= y1
		})
		if last >= lineCount {
			last = lineCount - 1
		}
		return first, max(first, last), true
	}
	first, last := st.VisibleLines()
	if first == 0 && last == 0 {
		return 0, 0, false
	}
	return first, max(first, last), true
}

func liveVisibleLineWindow(st *codeed.State, lineCount int) (int, int) {
	if lineCount <= 0 {
		return 0, -1
	}
	if viewH := st.ViewportHeight(); viewH > 0 {
		first, last, _ := currentVisibleLines(st, lineCount)
		from := max(0, first-visibleWindowPad)
		to := min(lineCount-1, last+visibleWindowPad)
		if from > to {
			return 0, -1
		}
		return from, to
	}
	first, last := st.VisibleLines()
	if first == 0 && last == 0 {
		return 0, min(lineCount-1, firstFrameMinLastLine)
	}
	from := max(0, first-visibleWindowPad)
	to := min(lineCount-1, max(0, last)+visibleWindowPad)
	if from > to {
		return 0, -1
	}
	return from, to
}

func countSpanLines(spans []lineSpan) int {
	total := 0
	for _, sp := range spans {
		total += sp.to - sp.from + 1
	}
	return total
}

func intersectLineSpans(spans []lineSpan, winFrom, winTo int) []lineSpan {
	if winFrom > winTo {
		return nil
	}
	out := spans[:0]
	for _, sp := range spans {
		from := max(sp.from, winFrom)
		to := min(sp.to, winTo)
		if from <= to {
			out = append(out, lineSpan{from: from, to: to})
		}
	}
	return out
}

func spaces(n int) string {
	const s = "                "
	if n <= len(s) {
		return s[:n]
	}
	return strings.Repeat(" ", n)
}
