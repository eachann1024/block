package editor

import (
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// editLayout is retained on Session for compatibility and lightweight geometry helpers.
type editLayout struct {
	layout *ui.TextLayout
}

type caretBlinkState struct {
	block model.BlockID
	pos   int
}

// blockHighlightBands computes non-layout-splitting highlight bands (annotations, find matches,
// and block selections) so neither unfocused RichText nor focused RichInput ever splits Span
// padding or shifts glyph positions.
func (s *Session) blockHighlightBands(idx int, b *model.Block, pal theme.Palette, includeSelection bool) []appUi.RichBand {
	var out []appUi.RichBand

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
		out = append(out, appUi.RichBand{
			Start:    lo,
			End:      hi,
			Fill:     fill,
			Radius:   4,
			TintOver: true,
		})
	}

	if s.findOpen {
		for i, m := range s.findMatches {
			if m.BlockIdx != idx {
				continue
			}
			band := appUi.RichBand{
				Start:    m.StartRune,
				End:      m.EndRune,
				Fill:     pal.FindMatch,
				Radius:   2,
				TintOver: true,
			}
			if i == s.findCurrent {
				band.Fill = pal.FindActive
				band.Ring = pal.FindRing
			}
			out = append(out, band)
		}
	}

	if includeSelection {
		if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && selHi > selLo {
			out = append(out, appUi.RichBand{
				Start:        selLo,
				End:          selHi,
				Fill:         pal.EditorSelectionBG(),
				Radius:       theme.EditorSelectionRadius,
				PadX:         theme.EditorSelectionPadX,
				FullLineBand: true,
				TintOver:     true,
			})
		}
	}
	return out
}

func blockOpaqueBGRanges(b *model.Block) [][2]int {
	if b == nil || len(b.Spans) == 0 {
		return nil
	}
	var out [][2]int
	pos := 0
	for _, sp := range b.Spans {
		n := utf8.RuneCountInString(sp.Text)
		if sp.BGColor != "" && sp.BGColor != "default" && n > 0 {
			out = append(out, [2]int{pos, pos + n})
		}
		pos += n
	}
	return out
}

func blockLinkRanges(b *model.Block) []appUi.RichLinkRange {
	if b == nil || len(b.Spans) == 0 {
		return nil
	}
	var out []appUi.RichLinkRange
	pos := 0
	for _, sp := range b.Spans {
		n := utf8.RuneCountInString(sp.Text)
		if sp.Link != "" && n > 0 {
			out = append(out, appUi.RichLinkRange{Start: pos, End: pos + n, URL: sp.Link})
		}
		pos += n
	}
	return out
}

func blockHasChipBG(b *model.Block) bool {
	if b == nil {
		return false
	}
	for _, sp := range b.Spans {
		if sp.Marks&model.MarkCode != 0 || sp.BGColor != "" {
			return true
		}
	}
	return false
}
