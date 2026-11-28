package editor

import (
	"strings"
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

// JumpToSourcePos jumps to a 1-based Markdown source line and optional 1-based rune column,
// mapping multi-line code blocks, raw HTML, tables, and rich text blocks to an intra-block range.
func (s *Session) JumpToSourcePos(line, col int) {
	if s == nil || s.doc == nil || line <= 0 {
		return
	}
	source := markdown.MapSource(s.doc)
	srcLines := strings.Split(source.Text, "\n")
	if len(srcLines) == 0 {
		return
	}
	clampedLine := min(line, len(srcLines))
	byteCol := -1
	if col > 0 {
		byteCol = runeColToByteOffset(srcLines[clampedLine-1], col)
	}
	target, ok := source.Locate(clampedLine, byteCol, "")
	if !ok {
		s.JumpToSearchHit(model.SearchHit{Line: line, Column: -1})
		return
	}
	if target.Block < 0 || target.Block >= len(s.doc.Blocks) {
		return
	}
	block := &s.doc.Blocks[target.Block]
	if block.Table != nil {
		if col > 0 && target.Row >= 0 && target.Column >= 0 &&
			target.Row < len(block.Table.Rows) && target.Column < len(block.Table.Rows[target.Row]) {
			if cellVisCol, okCol := source.TableCellVisibleColumn(target.Block, clampedLine, col); okCol {
				cellPlain := block.Table.Rows[target.Row][target.Column].PlainText()
				if startRune, endRune, okRange := textLineColRuneRange(cellPlain, 0, cellVisCol); okRange {
					s.jumpToRange(target.Block, target.Row, target.Column, startRune, endRune)
					return
				}
			}
		}
		s.jumpTo(target.Block, target.Row, target.Column)
		return
	}
	relLine, contentCol, hasPos := source.BlockContentPosition(target.Block, clampedLine, col)
	if block.Kind == model.BlockCode || block.Kind == model.BlockRawHTML {
		if !hasPos {
			relLine, contentCol = 0, col
		}
		if startRune, endRune, okRange := textLineColRuneRange(block.Code, relLine, contentCol); okRange {
			s.jumpToRange(target.Block, -1, -1, startRune, endRune)
			return
		}
		s.jumpTo(target.Block, -1, -1)
		return
	}
	plain := block.PlainText()
	if !hasPos {
		relLine, contentCol = 0, col
	}
	if (relLine > 0 || contentCol > 0) && plain != "" {
		if startRune, endRune, okRange := textLineColRuneRange(plain, relLine, contentCol); okRange {
			s.jumpToRange(target.Block, -1, -1, startRune, endRune)
			return
		}
	}
	s.jumpTo(target.Block, -1, -1)
}

func runeColToByteOffset(lineText string, col1 int) int {
	if col1 <= 1 {
		return 0
	}
	runeIdx := 0
	for off := range lineText {
		if runeIdx == col1-1 {
			return off
		}
		runeIdx++
	}
	return len(lineText)
}

// textLineColRuneRange maps a 0-based line index and optional 1-based rune column inside text
// to a non-empty [startRune, endRune) range for matchLineOffsetInBlock.
func textLineColRuneRange(text string, line0, col1 int) (startRune, endRune int, ok bool) {
	total := utf8.RuneCountInString(text)
	if total == 0 {
		return 0, 0, false
	}
	if line0 < 0 {
		line0 = 0
	}
	lines := strings.Split(text, "\n")
	if line0 >= len(lines) {
		line0 = len(lines) - 1
	}
	lineStart := 0
	for i := 0; i < line0; i++ {
		lineStart += utf8.RuneCountInString(lines[i]) + 1
	}
	lineLen := utf8.RuneCountInString(lines[line0])
	lineEnd := lineStart + lineLen
	if col1 > 0 {
		pos := min(lineStart+col1-1, lineEnd)
		startRune = pos
		endRune = min(pos+1, total)
		if endRune <= startRune && startRune > 0 {
			startRune--
		}
	} else {
		startRune = lineStart
		endRune = lineEnd
		if endRune <= startRune {
			if startRune < total {
				endRune = startRune + 1
			} else if startRune > 0 {
				startRune--
			}
		}
	}
	if endRune <= startRune {
		return 0, 0, false
	}
	return startRune, endRune, true
}

// JumpToBlock is used by the outline. Jumping preserves the editing caret.
func (s *Session) JumpToBlock(idx int) {
	s.jumpToOutlineBlock(idx, true)
}

func (s *Session) jumpToOutlineBlock(idx int, closeOverlay bool) bool {
	if !s.jumpTo(idx, -1, -1) {
		return false
	}
	blockID := s.doc.Blocks[idx].ID
	s.outlineSelectedBlockID = blockID
	s.outlineJumpLockID = blockID
	s.outlineJumpLockUntil = time.Time{}
	s.outlineLastClickedID = blockID
	s.outlinePreferScroll = false
	s.outlineWakeUntil = time.Time{}
	s.outlineIdleFadeAt = time.Time{}
	if closeOverlay {
		s.closeOutline(blockID)
	}
	return true
}

func (s *Session) previewOutlineBlock(idx int) bool {
	return s.jumpToOutlineBlock(idx, false)
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
