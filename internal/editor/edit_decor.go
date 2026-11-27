package editor

import (
	"strings"
	"time"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	annotFlashHold = 240 * time.Millisecond
	annotFlashFade = 360 * time.Millisecond
)

func (s *Session) annotationFlashAlpha(c *ui.Context) float32 {
	if s.annotFlashID == "" {
		return 0
	}
	if s.annotFlashAt.IsZero() {
		s.annotFlashAt = c.Now()
	}
	elapsed := c.Now().Sub(s.annotFlashAt)
	if elapsed < annotFlashHold {
		c.After(annotFlashHold - elapsed)
		return 1
	}
	if elapsed >= annotFlashHold+annotFlashFade {
		s.annotFlashID = ""
		s.annotFlashAt = time.Time{}
		return 0
	}
	c.AnimationFrame()
	return 1 - float32(elapsed-annotFlashHold)/float32(annotFlashFade)
}

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
		if ann.ID == s.activeAnnotID || ann.ID == s.hoveredAnnotID || ann.ID == s.editingAnnotID {
			fill = pal.AnnotationActive
		} else if ann.ID == s.annotFlashID && s.annotFlashAlpha > 0 {
			fill = pal.AnnotationBG.Mix(pal.AnnotationActive, s.annotFlashAlpha)
		}
		out = append(out, appUi.RichBand{
			Start:    lo,
			End:      hi,
			Fill:     fill,
			Radius:   theme.RadiusXS,
			TintOver: true,
		})
	}

	matches, first := s.findMatchesForBlock(idx)
	for i, m := range matches {
		band := appUi.RichBand{
			Start:    m.StartRune,
			End:      m.EndRune,
			Fill:     pal.FindMatch,
			Radius:   theme.RadiusXS,
			TintOver: true,
		}
		if first+i == s.findCurrent {
			band.Fill = pal.FindActive
			band.Ring = pal.FindRing
		}
		out = append(out, band)
	}

	if includeSelection {
		if selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx); selOk && selHi > selLo {
			if s.toolbarPop == "comment" {
				out = append(out, appUi.RichBand{
					Start:     selLo,
					End:       selHi,
					Fill:      pal.AnnotationBG,
					Underline: pal.AnnotationActive,
					Radius:    theme.RadiusXS,
					TintOver:  true,
				})
			} else {
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
		if sp.Link != "" && !strings.HasPrefix(sp.Link, inlinePathLinkPrefix) && n > 0 {
			out = append(out, appUi.RichLinkRange{Start: pos, End: pos + n, URL: sp.Link})
		}
		pos += n
	}
	return out
}

func blockCodeRanges(b *model.Block, baseSize float32) []appUi.RichCodeRange {
	if b == nil || len(b.Spans) == 0 {
		return nil
	}
	_, _, padX, padY, radius := inlineCodeStyle(baseSize)
	pal := theme.Ocean()
	dark := pal.Dark
	var out []appUi.RichCodeRange
	pos := 0
	for _, sp := range b.Spans {
		n := utf8.RuneCountInString(sp.Text)
		_, hasNamedBG := theme.ResolveNamedBG(sp.BGColor, dark)
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 && !hasNamedBG && n > 0 {
			if m := len(out); m > 0 && out[m-1].End == pos {
				out[m-1].End = pos + n
			} else {
				out = append(out, appUi.RichCodeRange{
					Start:  pos,
					End:    pos + n,
					PadX:   padX,
					PadY:   padY,
					Radius: radius,
				})
			}
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
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 || sp.BGColor != "" {
			return true
		}
	}
	return false
}
