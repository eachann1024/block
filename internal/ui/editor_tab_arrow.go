package ui

import (
	"slices"
	"strings"
	"unicode/utf8"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

const (
	maxTabColumn           = 256
	selectionFullLineLimit = 200
	visibleWindowPad       = 24
	firstFrameMinLastLine  = 80
)

type lineSpan struct {
	from, to int
}

type byteRange struct {
	start, end int
}

// EditorTabWhitespace replaces visible tab characters in codeed.View with
// full-width PassThrough arrow elements so tab arrows span the tab-stop width
// with symmetric 2 DIP insets instead of stopping at 2*charWidth.
type EditorTabWhitespace struct {
	initialized     bool
	lastState       *codeed.State
	lastBuf         *codeed.Buffer
	lastBufVer      uint64
	lastMode        codeed.Whitespace
	lastTabSize     int
	lastFont        ui.Font
	lastWinFrom     int
	lastWinTo       int
	lastSels        []codeed.Selection
	hasReplacements bool

	widthsValid     bool
	measuredFont    ui.Font
	measuredTabSize int
	tabWidths       []float32

	repls     []codeed.Replacement
	spansBuf  []lineSpan
	rangesBuf []byteRange
}

// Prepare runs before codeed.View each frame. It installs full-width tab arrow
// replacements for tabs visible under st.Whitespace and keeps subsequent
// unselected tabs on the same line aligned to their original tab stops.
func (h *EditorTabWhitespace) Prepare(c *ui.Context, st *codeed.State) {
	if h == nil || c == nil || st == nil || st.Buffer == nil {
		return
	}
	if h.lastState != st {
		h.hasReplacements = false
	}

	font := normalizeEditorFont(st.Font)
	tabSize := resolveEditorTabSize(st)
	mode := st.Whitespace
	buf := st.Buffer
	snap := buf.Snapshot()
	bufVer := snap.Version()
	lineCount := snap.LineCount()

	var sels []codeed.Selection
	if mode == codeed.WhitespaceSelection {
		sels = st.Selections()
	}

	winFrom, winTo := -1, -1
	var lineSpans []lineSpan
	hasNonEmptySel := false

	switch mode {
	case codeed.WhitespaceNone:
	case codeed.WhitespaceSelection:
		lineSpans, hasNonEmptySel = h.selectionLineSpans(snap, sels)
		if hasNonEmptySel && countSpanLines(lineSpans) > selectionFullLineLimit {
			winFrom, winTo = visibleLineWindow(st, lineCount)
			lineSpans = intersectLineSpans(lineSpans, winFrom, winTo)
		}
	default:
		winFrom, winTo = visibleLineWindow(st, lineCount)
		if lineCount > 0 && winFrom <= winTo {
			h.spansBuf = append(h.spansBuf[:0], lineSpan{from: winFrom, to: winTo})
			lineSpans = h.spansBuf
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
		h.lastWinFrom == winFrom &&
		h.lastWinTo == winTo &&
		sameSels {
		return
	}

	h.storeCache(st, buf, bufVer, mode, tabSize, font, winFrom, winTo, sels)

	if mode == codeed.WhitespaceNone || (mode == codeed.WhitespaceSelection && !hasNonEmptySel) || len(lineSpans) == 0 {
		if h.hasReplacements {
			st.SetReplacements(nil)
			h.hasReplacements = false
		}
		return
	}

	h.ensureTabWidths(c, font, tabSize)

	var selRanges []byteRange
	if mode == codeed.WhitespaceSelection {
		selRanges = h.selectionByteRanges(sels)
	}

	h.repls = h.buildReplacements(snap, lineSpans, mode, tabSize, selRanges, st)
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

func (h *EditorTabWhitespace) storeCache(
	st *codeed.State,
	buf *codeed.Buffer,
	bufVer uint64,
	mode codeed.Whitespace,
	tabSize int,
	font ui.Font,
	winFrom, winTo int,
	sels []codeed.Selection,
) {
	h.initialized = true
	h.lastState = st
	h.lastBuf = buf
	h.lastBufVer = bufVer
	h.lastMode = mode
	h.lastTabSize = tabSize
	h.lastFont = font
	h.lastWinFrom = winFrom
	h.lastWinTo = winTo
	if mode == codeed.WhitespaceSelection {
		h.lastSels = append(h.lastSels[:0], sels...)
	} else {
		h.lastSels = h.lastSels[:0]
	}
}

func (h *EditorTabWhitespace) ensureTabWidths(c *ui.Context, font ui.Font, tabSize int) {
	if h.widthsValid && h.measuredFont == font && h.measuredTabSize == tabSize && len(h.tabWidths) == tabSize+1 {
		return
	}
	span := ui.Span{
		Text:     "00",
		Font:     font.Family,
		Size:     font.Size,
		Weight:   font.Weight,
		Italic:   font.Italic,
		Features: font.Features,
		Lang:     font.Lang,
	}
	w00, _ := c.MeasureText(0, span)
	widths := slices.Grow(h.tabWidths[:0], tabSize+1)[:tabSize+1]
	widths[0] = 0
	for k := 1; k <= tabSize; k++ {
		span.Text = "0" + spaces(k) + "0"
		wk, _ := c.MeasureText(0, span)
		w := wk - w00
		if w <= 0 {
			w = float32(k) * font.Size * 0.6
		}
		widths[k] = w
	}
	h.tabWidths = widths
	h.measuredFont = font
	h.measuredTabSize = tabSize
	h.widthsValid = true
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
) []codeed.Replacement {
	repls := h.repls[:0]
	contentFn := func(ctx *ui.Context, e ui.Element, _ codeed.Replacement) {
		col := resolveWhitespaceColor(ctx, st)
		e.PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
			paintTabArrow(p, r, col)
		})
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
			// Once a line contains a Content replacement (rendered as a 1-rune
			// object char), subsequent unselected tabs on the same line need
			// explicit spaces(cols) so mygo's tab-stop column count stays aligned.
			lineHasContentRepl := false
			for i := 0; i < len(lineText); {
				r, size := utf8.DecodeRuneInString(lineText[i:])
				if r != '\t' {
					col++
					i += size
					continue
				}
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
					lineHasContentRepl = true
					repls = append(repls, codeed.Replacement{
						Start:   lineStart + i,
						End:     lineStart + i + 1,
						Width:   h.tabWidths[cols],
						Content: contentFn,
					})
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
	return repls
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

func visibleLineWindow(st *codeed.State, lineCount int) (int, int) {
	if lineCount <= 0 {
		return 0, -1
	}
	first, last := st.VisibleLines()
	if first == 0 && last == 0 {
		last = firstFrameMinLastLine
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
