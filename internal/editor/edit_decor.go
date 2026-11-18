package editor

import (
	"math"
	"time"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// editLayout mirrors the text layout of a focused block's TextAreaBase so the
// editor can paint what MyGo's text input cannot: highlight bands behind runs,
// link underlines and its own caret. It is shaped during build from the same
// weight runs that feed TextRanges, so glyph advances match the input's.
type editLayout struct {
	key    editLayoutKey
	layout *ui.TextLayout
	text   string
	units  []int // rune offset -> UTF-16 offset
}

type editLayoutKey struct {
	text   string
	runs   string
	width  float32
	family string
	size   float32
	weight int
	lh     float32
}

// weightRuns is the single source for bold/code weights in the focused input:
// TextRanges and the mirrored layout are both built from it.
func weightRuns(ranges []ui.TextRange) []ui.TextRange {
	var out []ui.TextRange
	for _, r := range ranges {
		if r.Weight != 0 && r.End > r.Start {
			out = append(out, ui.TextRange{Start: r.Start, End: r.End, Weight: r.Weight})
		}
	}
	return out
}

func runsSignature(runs []ui.TextRange) string {
	b := make([]byte, 0, len(runs)*12)
	for _, r := range runs {
		b = utf8.AppendRune(b, rune(r.Start))
		b = utf8.AppendRune(b, rune(r.End))
		b = utf8.AppendRune(b, rune(r.Weight))
	}
	return string(b)
}

// mirrorLayout returns the cached layout for text, reshaping only when the
// text, weight runs, width or font change.
func (s *Session) mirrorLayout(text string, runs []ui.TextRange, width float32, family string, size float32, weight int, lh float32) *editLayout {
	key := editLayoutKey{text: text, runs: runsSignature(runs), width: width, family: family, size: size, weight: weight, lh: lh}
	if s.editMirror != nil && s.editMirror.key == key {
		return s.editMirror
	}
	rs := []rune(text)
	wAt := make([]int, len(rs))
	for i := range wAt {
		wAt[i] = weight
	}
	for _, r := range runs {
		for i := max(r.Start, 0); i < min(r.End, len(rs)); i++ {
			wAt[i] = max(wAt[i], r.Weight)
		}
	}
	var spans []ui.Span
	for i := 0; i < len(rs); {
		j := i
		for j < len(rs) && wAt[j] == wAt[i] {
			j++
		}
		spans = append(spans, ui.Span{Text: string(rs[i:j]), Font: family, Size: size, Weight: wAt[i]})
		i = j
	}
	if len(spans) == 0 {
		spans = []ui.Span{{Text: "", Font: family, Size: size, Weight: weight}}
	}
	font := ui.Font{Family: family, Size: size, Weight: weight, LineHeight: lh}
	units := make([]int, len(rs)+1)
	u := 0
	for i, r := range rs {
		units[i] = u
		if r >= 0x10000 {
			u += 2
		} else {
			u++
		}
	}
	units[len(rs)] = u
	s.editMirror = &editLayout{key: key, layout: ui.ShapeRichText(spans, font, width), text: text, units: units}
	return s.editMirror
}

func (m *editLayout) unit(runeOff int) int {
	return m.units[max(min(runeOff, len(m.units)-1), 0)]
}

// rects returns the line rectangles covering runes [start, end), relative to
// the text origin.
func (m *editLayout) rects(start, end int) []ui.Rect {
	if m == nil || end <= start {
		return nil
	}
	return m.layout.SelectionRects(ui.TextInputRange{Start: m.unit(start), End: m.unit(end)})
}

// caret returns the caret rectangle at a rune offset, relative to the text origin.
func (m *editLayout) caret(runeOff int) ui.Rect {
	return m.layout.Caret(m.unit(runeOff))
}

// Caret blink (thinkly-fast editor/caret_blink.rs): solid while the caret
// moves, then hold 480ms / fade 120ms, restarting on every move.
const (
	caretHoldMs  = 480
	caretFadeMs  = 120
	caretFrameMs = 33
)

func caretBlinkPhase(tMs float64) (alpha float32, waitMs int) {
	period := float64(2 * (caretHoldMs + caretFadeMs))
	t := mathMod(tMs, period)
	ease := func(x float64) float32 { return float32(x * x * (3 - 2*x)) }
	switch {
	case t < caretHoldMs:
		return 1, int(caretHoldMs-t) + 1
	case t < caretHoldMs+caretFadeMs:
		return 1 - ease((t-caretHoldMs)/caretFadeMs), caretFrameMs
	case t < 2*caretHoldMs+caretFadeMs:
		return 0, int(2*caretHoldMs+caretFadeMs-t) + 1
	default:
		return ease((t - 2*caretHoldMs - caretFadeMs) / caretFadeMs), caretFrameMs
	}
}

func mathMod(a, b float64) float64 {
	m := a - b*float64(int64(a/b))
	if m < 0 {
		m += b
	}
	return m
}

type caretBlinkState struct {
	block model.BlockID
	pos   int
	epoch time.Time
}

type decorBand struct {
	start, end int
	fill       ui.Color
	ring       ui.Color // zero alpha = none
	radius     float32
	padX       float32
}

// editBands lists the highlight bands of block idx: annotations, find matches
// and inline-code chips, painted behind the focused input's glyphs.
func (s *Session) editBands(idx int, b *model.Block, pal theme.Palette) []decorBand {
	var out []decorBand
	pos := 0
	for _, sp := range b.Spans {
		n := utf8.RuneCountInString(sp.Text)
		if sp.Marks&model.MarkCode != 0 && n > 0 {
			out = append(out, decorBand{start: pos, end: pos + n, fill: pal.InlineCodeBG, radius: theme.RadiusMicroSM, padX: 2})
		}
		pos += n
	}
	for _, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		lo, hi, ok := s.annotationBlockRange(ann, idx)
		if !ok {
			continue
		}
		fill := pal.AnnotationBG
		if ann.ID == s.activeAnnotID || ann.ID == s.hoveredAnnotID {
			fill = pal.AnnotationActive
		}
		out = append(out, decorBand{start: lo, end: hi, fill: fill, radius: 2})
	}
	if s.findOpen {
		for i, m := range s.findMatches {
			if m.BlockIdx != idx {
				continue
			}
			band := decorBand{start: m.StartRune, end: m.EndRune, fill: pal.FindMatch, radius: 2}
			if i == s.findCurrent {
				band.fill, band.ring = pal.FindActive, pal.FindRing
			}
			out = append(out, band)
		}
	}
	if s.hasMultiBlockSelection() {
		if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && selHi > selLo {
			out = append(out, decorBand{start: selLo, end: selHi, fill: pal.SelectionBG, radius: 2})
		}
	}
	return out
}

// linkRanges lists [start,end) rune ranges of linked spans.
func linkRanges(b *model.Block) [][2]int {
	var out [][2]int
	pos := 0
	for _, sp := range b.Spans {
		n := utf8.RuneCountInString(sp.Text)
		if sp.Link != "" && n > 0 {
			out = append(out, [2]int{pos, pos + n})
		}
		pos += n
	}
	return out
}

// decorateEditInput paints, on the focused block's TextAreaBase, what MyGo's
// text input cannot: highlight bands behind runs, the hover/caret link
// underline and Block's own blinking caret. Everything is computed here, at
// build time; the paint closures only paint.
func (s *Session) decorateEditInput(c *ui.Context, in ui.Element, idx int, b *model.Block, text string, ranges []ui.TextRange, size float32, weight int, lh float32, deleteArmed bool) {
	width := in.Bounds().W
	if width <= 0 || in.Composing() {
		// First frame (no bounds yet) or an input method composing: the
		// input's text differs from text, so leave it all native.
		return
	}
	family := resolvedEditorFont()
	if family == "" {
		family = c.Theme().Font
	}
	pal := theme.Ocean()
	m := s.mirrorLayout(text, weightRuns(ranges), width, family, size, weight, lh)
	selA, selB := in.TextSelection()
	lines := m.layout.Lines()

	type band struct {
		r          ui.Rect
		fill, ring ui.Color
		radius     float32
	}
	// Bands cover the glyph height, centred on the line, as the unfocused
	// rich text paints span backgrounds; not the full (loose) line box.
	bandH := float32(math.Round(float64(size * 1.28)))
	var bands []band
	for _, d := range s.editBands(idx, b, pal) {
		for _, r := range m.rects(d.start, d.end) {
			h := min(bandH, r.H)
			bands = append(bands, band{r: ui.Rect{X: r.X - d.padX, Y: r.Y + (r.H-h)*0.5, W: r.W + 2*d.padX, H: h}, fill: d.fill, ring: d.ring, radius: d.radius})
		}
	}

	// Link underline: 1px, 3px below the baseline, while the pointer is over
	// the link or the caret is inside it.
	var underlines []ui.Rect
	px, py, inside := in.PointerPosition()
	bounds := in.Bounds()
	for _, lr := range linkRanges(b) {
		rects := m.rects(lr[0], lr[1])
		show := selA == selB && selA >= lr[0] && selA <= lr[1]
		if !show && inside {
			for _, r := range rects {
				if px >= bounds.X+r.X && px <= bounds.X+r.X+r.W && py >= bounds.Y+r.Y && py <= bounds.Y+r.Y+r.H {
					show = true
					break
				}
			}
		}
		if !show {
			continue
		}
		for _, r := range rects {
			baseline := r.Y + r.H*0.8
			for _, ln := range lines {
				if r.Y+r.H*0.5 >= ln.Rect.Y && r.Y+r.H*0.5 < ln.Rect.Y+ln.Rect.H {
					baseline = ln.Baseline
					break
				}
			}
			underlines = append(underlines, ui.Rect{X: r.X, Y: baseline + 3, W: r.W, H: 1})
		}
	}

	// Own caret: 2px, round(size*1.125) tall, radius 1, accent, blinking.
	// Hide native caret whenever any selection is active so no stray bar shows at the drag edge.
	drawCaret := false
	var caretRect ui.Rect
	var caretCol ui.Color
	if selA != selB || s.hasNonEmptySelection() {
		in.CaretColor(ui.Transparent)
	} else if in.Focused() && c.WindowActive() {
		in.CaretColor(ui.Transparent)
		if !s.caretHidden {
			now := c.Now()
			if s.caretBlink.block != b.ID || s.caretBlink.pos != selA || s.caretBlink.epoch.IsZero() {
				s.caretBlink = caretBlinkState{block: b.ID, pos: selA, epoch: now}
			}
			alpha, wait := caretBlinkPhase(float64(now.Sub(s.caretBlink.epoch).Milliseconds()))
			c.After(time.Duration(wait) * time.Millisecond)
			if alpha > 0 {
				cr := m.caret(selA)
				h := float32(math.Round(float64(size * 1.125)))
				caretY := cr.Y + (cr.H-h)*0.5
				for _, ln := range lines {
					if cr.Y+cr.H*0.5 >= ln.Rect.Y && cr.Y+cr.H*0.5 <= ln.Rect.Y+ln.Rect.H {
						y := ln.Baseline - 0.95*size
						hiY := max(ln.Rect.Y+ln.Rect.H-h, ln.Rect.Y)
						caretY = min(max(y, ln.Rect.Y), hiY)
						break
					}
				}
				caretX := cr.X - 1
				if selA > 0 && cr.X > 1 {
					caretX = cr.X - 0.5
				}
				caretRect = ui.Rect{X: caretX, Y: caretY, W: 2, H: h}
				caretCol = pal.Accent
				if deleteArmed {
					caretCol = pal.Danger
				}
				caretCol = caretCol.Alpha(alpha)
				drawCaret = true
			}
		}
	}

	if len(bands) > 0 {
		in.Draw(func(p *ui.Painter, r ui.Rect) {
			for _, bd := range bands {
				rr := ui.Rect{X: r.X + bd.r.X, Y: r.Y + bd.r.Y, W: bd.r.W, H: bd.r.H}
				p.Fill(rr, bd.fill, bd.radius)
				if bd.ring.A > 0 {
					p.Stroke(rr, bd.ring, bd.radius, 1)
				}
			}
		})
	}
	if len(underlines) > 0 || drawCaret {
		link := pal.Link
		in.DrawOver(func(p *ui.Painter, r ui.Rect) {
			for _, u := range underlines {
				p.Fill(ui.Rect{X: r.X + u.X, Y: r.Y + u.Y, W: u.W, H: u.H}, link, 0)
			}
			if drawCaret {
				p.Fill(ui.Rect{X: r.X + caretRect.X, Y: r.Y + caretRect.Y, W: caretRect.W, H: caretRect.H}, caretCol, 1)
			}
		})
	}
}
