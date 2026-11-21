package editor

import (
	"time"
	"unicode/utf8"

	"block/internal/files"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	appUI "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const jumpHighlightHold = 1100 * time.Millisecond

type editorJump struct {
	block              model.BlockID
	row, col           int
	startRune, endRune int
	revision           uint64
	pending            bool
	shownAt            time.Time
}

func (s *Session) JumpToSearchHit(hit model.SearchHit) {
	if hit.Line <= 0 {
		return
	}
	source := markdown.MapSource(s.doc)
	line, column := files.ResolveSearchMatch(source.Text, hit)
	if target, ok := source.Locate(line, column, hit.Query); ok {
		s.jumpTo(target.Block, target.Row, target.Column)
	}
}

// JumpToBlock is used by the outline. Jumping preserves the editing caret.
func (s *Session) JumpToBlock(idx int) {
	if s.jumpTo(idx, -1, -1) {
		s.outlineHovered = false
		s.outlinePinnedClosed = true
		s.outlineHoverEnterAt = time.Time{}
		s.outlineHoverLeaveAt = time.Time{}
	}
}

func (s *Session) jumpTo(idx, row, col int) bool {
	return s.jumpToRange(idx, row, col, -1, -1)
}

func (s *Session) jumpToRange(idx, row, col, startRune, endRune int) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	s.ensureBlockUnfolded(idx)
	block := &s.doc.Blocks[idx]
	table := block.Table
	if table == nil || row < 0 || row >= len(table.Rows) || col < 0 || col >= len(table.Rows[row]) {
		row, col = -1, -1
	}
	s.jump = editorJump{
		block:     block.ID,
		row:       row,
		col:       col,
		startRune: startRune,
		endRune:   endRune,
		revision:  s.docRev,
		pending:   true,
	}
	return true
}

func (s *Session) jumpToFindMatch(m findMatch) {
	if table := s.doc.Blocks[m.BlockIdx].Table; table != nil {
		offset := 0
		for r, cells := range table.Rows {
			for c, cell := range cells {
				end := offset + utf8.RuneCountInString(cell.PlainText())
				if m.StartRune >= offset && m.EndRune <= end {
					s.jumpToRange(m.BlockIdx, r, c, -1, -1)
					return
				}
				offset = end + 1 // Tab between cells, newline between rows.
			}
		}
	}
	s.jumpToRange(m.BlockIdx, -1, -1, m.StartRune, m.EndRune)
}

func (s *Session) findJumpTopClearance() float32 {
	clearance := theme.FindBarTopOffset + theme.FindBarHeight + theme.FindBarJumpGap
	if r, ok := s.overlayPanelRects["find-bar"]; ok && r.H > 0 && s.scrollRect.H > 0 && r.Y >= s.scrollRect.Y {
		measured := (r.Y - s.scrollRect.Y) + r.H + theme.FindBarJumpGap
		clearance = max(clearance, measured)
	}
	if s.scrollRect.H > 0 {
		clearance = min(clearance, max(s.scrollRect.H*0.45, theme.Space24))
	}
	return clearance
}

func (s *Session) jumpTopClearance() float32 {
	if s.findOpen {
		return s.findJumpTopClearance()
	}
	return s.scale(theme.Space24)
}

func (s *Session) baseCanvasTopPad() float32 {
	if s.embedded {
		return 0
	}
	if s.minimal {
		return minimalTopPad
	}
	return s.scale(editorTopPad)
}

// canvasTopPad stays fixed while the find bar is open: the bar floats over the
// page, so opening it must not push the document down.
func (s *Session) canvasTopPad() float32 {
	return s.baseCanvasTopPad()
}

func (s *Session) shiftScrollY(delta float32) {
	if delta == 0 || s.bodyScroll.Y <= 0 {
		return
	}
	nextY := max(s.bodyScroll.Y+delta, 0)
	s.bodyScroll.Y = nextY
	if s.bodyScroll.Scrolling() {
		s.bodyScroll.ScrollTo(s.bodyScroll.X, nextY, ui.Animation{})
	}
}

func (s *Session) matchLineOffsetInBlock(idx int, b *model.Block, topGap float32) float32 {
	if b == nil || s.jump.row >= 0 || s.jump.endRune <= s.jump.startRune || s.jump.startRune < 0 {
		return 0
	}
	var textW, padTop float32
	if tr, ok := s.blockTextRect(b.ID); ok && tr.W > 0 {
		textW = tr.W
		if r, okR := s.blockRect(b.ID); okR && r.H > 0 {
			padTop = min(max(tr.Y-(r.Y+topGap), 0), r.H)
		}
	} else {
		flowW := min(s.viewportWidth, s.outerFlowWidth())
		indentW := float32(b.Indent) * s.scale(editorIndentStep)
		textW = max(flowW-s.leftContentGutter()-s.rightContentInset()-indentW, 80)
	}
	var layout *ui.TextLayout
	var plain string
	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		plain = b.Code
		layout = s.cachedShapeBlock(b.ID, plain, nil, idx, resolvedCodeFont(), s.codeFontSize(), theme.WeightRegular, 1.5, textW, false)
	} else if b.Kind != model.BlockTable && b.Kind != model.BlockImage && b.Kind != model.BlockDivider {
		plain = b.PlainText()
		fontSize, weight, lh := s.textMetrics(b.Kind)
		layout = s.cachedShapeBlock(b.ID, plain, b.Spans, idx, resolvedEditorFont(), fontSize, weight, lh, textW, true)
	}
	if layout == nil {
		return 0
	}
	u0 := runeOffsetToUTF16(plain, s.jump.startRune)
	u1 := runeOffsetToUTF16(plain, s.jump.endRune)
	if rects := layout.SelectionRects(ui.TextInputRange{Start: u0, End: u1}); len(rects) > 0 {
		return padTop + max(rects[0].Y, 0)
	}
	return 0
}

// revealJump uses the freshly laid-out row or cell. The scroll containers
// clamp to their actual extents and reveal horizontally clipped cells too.
func (s *Session) revealJump(c *ui.Context, contentTop float32) {
	// Input can rebuild the view before layout. Keep the anchor in every
	// pass so layout can apply the one-shot scroll request.
	anchor := ui.Box(c.Key("jump-anchor")).Absolute().Top(contentTop - s.jumpTopClearance()).Left(0).Right(0).
		Height(theme.Space2).IgnoreInput(true).AccessHidden(true)
	if !s.jump.pending {
		return
	}
	s.jump.pending = false
	anchor.ScrollIntoView(ui.ScrollIntoViewOptions{
		Block:     ui.ScrollStart,
		Inline:    ui.ScrollNearest,
		Animation: appUI.JumpScrollAnimation(s.scrollRect.H > 0),
	})
}

func (s *Session) jumpHighlight(c *ui.Context) float32 {
	if s.jump.block == 0 {
		return 0
	}
	if s.jump.revision != s.docRev {
		s.jump = editorJump{}
		return 0
	}
	if s.jump.shownAt.IsZero() {
		s.jump.shownAt = c.Now()
	}
	fade := appUI.FadeColor.Duration
	elapsed := c.Now().Sub(s.jump.shownAt)
	if elapsed < jumpHighlightHold {
		c.After(jumpHighlightHold - elapsed)
		return 1
	}
	if elapsed >= jumpHighlightHold+fade {
		s.jump = editorJump{}
		return 0
	}
	c.AnimationFrame()
	return 1 - float32(elapsed-jumpHighlightHold)/float32(fade)
}
