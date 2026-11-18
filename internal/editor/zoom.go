package editor

import (
	"block/internal/model"
	"block/internal/theme"
)

func (s *Session) zoom() float32 {
	if s.minimal {
		return 1.0
	}
	z := theme.Zoom()
	if z <= 0 {
		return 1.0
	}
	return z
}

func (s *Session) scale(px float32) float32 {
	return px * s.zoom()
}

func (s *Session) standardWidth() float32 {
	return theme.EditorStandardWidth * s.zoom()
}

func (s *Session) handleWidth() float32 {
	return editorHandleWidth * s.zoom()
}

func (s *Session) rowGap() float32 {
	return editorRowGap * s.zoom()
}

func (s *Session) markerWidth() float32 {
	return editorMarkerWidth * s.zoom()
}

func (s *Session) indentStep() float32 {
	return editorIndentStep * s.zoom()
}

func (s *Session) codeFontSize() float32 {
	return 14 * s.zoom()
}

func (s *Session) tableMetrics() (fontSize, lineHeight, minRowH, minColW, maxColW, padY, padX float32) {
	z := s.zoom()
	return tableCellFontSize * z, tableCellLineHeight * z, tableMinRowHeight * z,
		tableMinColWidth * z, tableMaxColWidth * z, 6 * z, 10 * z
}

func (s *Session) outerFlowWidth() float32 {
	return s.standardWidth() + s.leftContentGutter() + s.rightContentInset()
}

func (s *Session) blockHandleTopScaled(b *model.Block, topGap float32) float32 {
	z := s.zoom()
	stripH := 24 * z
	if b == nil {
		return 0
	}
	switch {
	case b.Kind == model.BlockTable:
		return (tableMinRowHeight*z - stripH) * 0.5
	case b.Kind == model.BlockImage:
		return 4 * z
	case b.Kind == model.BlockCallout:
		return max(8*z, -topGap)
	case b.Kind == model.BlockDivider:
		return max((18*z-stripH)*0.5, -topGap)
	case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
		return max(5*z, -topGap)
	default:
		fontSize, _, lh := s.textMetrics(b.Kind)
		top := (fontSize*lh - stripH) * 0.5
		if b.Kind == model.BlockQuote || b.QuoteDepth > 0 {
			top += 2 * z
		}
		return max(top, -topGap)
	}
}
