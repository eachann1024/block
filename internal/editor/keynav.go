package editor

import (
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"

	"github.com/egoist/mygo/ui"
)

// Arrow-key navigation across blocks.
//
// Every block exposes the same geometric model: a caret has a window X (its column),
// and Up/Down leave a block on its first/last visual line, landing in the neighbour at
// the same column. Left/Right leave at offset 0/len and land at the neighbour's end/start.
// Shift turns each move into a selection-head move. Text blocks, code blocks, table cells
// and atomic blocks (divider, image, preview-only code) all route through these helpers.

// arrowNav decodes a plain or Shift arrow key.
func arrowNav(key ui.Key, mods ui.Modifiers) (dir int, vertical, extend, ok bool) {
	if mods != 0 && mods != ui.Shift {
		return 0, false, false, false
	}
	extend = mods == ui.Shift
	switch key {
	case ui.KeyUp:
		return -1, true, extend, true
	case ui.KeyDown:
		return 1, true, extend, true
	case ui.KeyLeft:
		return -1, false, extend, true
	case ui.KeyRight:
		return 1, false, extend, true
	}
	return 0, false, false, false
}

// rememberColumn keeps the sticky Up/Down column across blocks until the caret moves otherwise.
func (s *Session) rememberColumn(x float32) {
	s.navCarryX, s.navCarry = x, true
}

// takeCarryColumn hands the sticky column to the RichInput that receives the caret.
func (s *Session) takeCarryColumn(syncSel bool) (float32, bool) {
	if !syncSel || !s.navCarry {
		return 0, false
	}
	s.navCarry = false
	return s.navCarryX, true
}

// navigatePastBlock continues an arrow move that ran off block idx into its visible neighbour.
func (s *Session) navigatePastBlock(idx int, e appUi.RichEdge) bool {
	if s.doc == nil {
		return false
	}
	target := s.stepVisibleBlock(idx, e.Dir)
	if target < 0 || target == idx {
		return false
	}
	var pos Position
	switch {
	case e.Vertical:
		pos = s.verticalEntry(target, e.Dir, e.ColumnX)
	case e.Dir < 0:
		pos = Position{Block: target, Offset: s.blockSelectionLen(target)}
	default:
		pos = Position{Block: target}
	}
	s.placeCaret(pos, e.Extend)
	if e.Vertical {
		s.rememberColumn(e.ColumnX)
	}
	return true
}

// placeCaret collapses the caret at pos, or moves only the selection head there when extend.
func (s *Session) placeCaret(pos Position, extend bool) {
	pos = s.clampPos(pos)
	if !extend {
		s.FocusBlock(pos.Block)
		if !s.hasFocusedAtomicBlock() {
			s.selAnchor, s.selHead = pos, pos
			s.syncCollapsedTableCellFocus(pos)
		}
		return
	}
	s.selHead = pos
	s.focusedBlock = pos.Block
	s.caretHidden = false
	s.detachedCompose = ""
	if s.needsDetachedSelectionFocus() {
		s.multiSelFocusPending = true
		s.focusPending = false
		s.selSyncPending = false
	} else {
		s.syncCollapsedTableCellFocus(pos)
		s.multiSelFocusPending = false
		s.focusPending = !s.readOnly
		s.selSyncPending = !s.readOnly
	}
}

// verticalEntry is where Up (dir<0) or Down (dir>0) lands in block target at window column x:
// its last or first visual line, table row, or edge.
func (s *Session) verticalEntry(target, dir int, x float32) Position {
	b := &s.doc.Blocks[target]
	edge := Position{Block: target}
	if dir < 0 {
		edge.Offset = s.blockSelectionLen(target)
	}
	switch b.Kind {
	case model.BlockDivider, model.BlockImage:
		return edge
	case model.BlockTable:
		if b.Table == nil || len(b.Table.Rows) == 0 {
			return edge
		}
		row := 0
		if dir < 0 {
			row = len(b.Table.Rows) - 1
		}
		return s.tableRowPositionAtX(target, row, x, dir > 0, edge)
	}
	if s.isPreviewOnlyCodeBlock(target) {
		return edge
	}
	lay, tr, text, ok := s.blockTextLayout(target)
	if !ok {
		return edge
	}
	lines := lay.Lines()
	if len(lines) == 0 {
		return edge
	}
	ln := lines[0]
	if dir < 0 {
		ln = lines[len(lines)-1]
	}
	cp := lay.PositionAt(ui.Point{X: x - tr.X, Y: ln.Rect.Y + ln.Rect.H*0.5})
	return Position{Block: target, Offset: clampInt(utf16OffsetToRuneOffset(text, cp.Index), 0, utf8.RuneCountInString(text))}
}

// blockTextLayout returns the shaped text of a rich or code block with its window text rect.
func (s *Session) blockTextLayout(idx int) (*ui.TextLayout, ui.Rect, string, bool) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return nil, ui.Rect{}, "", false
	}
	b := &s.doc.Blocks[idx]
	tr, ok := s.blockTextRect(b.ID)
	if !ok || tr.W <= 0 {
		return nil, ui.Rect{}, "", false
	}
	var lay *ui.TextLayout
	var text string
	switch b.Kind {
	case model.BlockDivider, model.BlockImage, model.BlockTable:
		return nil, ui.Rect{}, "", false
	case model.BlockCode, model.BlockRawHTML:
		if s.isPreviewOnlyCodeBlock(idx) {
			return nil, ui.Rect{}, "", false
		}
		text = b.Code
		lay = s.cachedShapeBlock(b.ID, text, nil, 0, resolvedCodeFont(), s.codeFontSize(), theme.WeightRegular, 1.5, tr.W, false)
	default:
		text = b.PlainText()
		fontSize, weight, lh := s.textMetrics(b.Kind)
		lay = s.cachedShapeBlock(b.ID, text, b.Spans, idx, resolvedEditorFont(), fontSize, weight, lh, tr.W, true)
	}
	if lay == nil {
		return nil, ui.Rect{}, "", false
	}
	return lay, tr, text, true
}

// caretLine reports which visual line of lay holds rune offset off, out of how many, and its X.
func caretLine(lay *ui.TextLayout, text string, off int) (line, count int, x float32) {
	lines := lay.Lines()
	cr := lay.CaretAt(ui.TextCaretPosition{Index: utf8RuneToUTF16(text, off), Affinity: ui.TextDownstream})
	mid := cr.Y + cr.H*0.5
	for i, ln := range lines {
		if mid >= ln.Rect.Y && mid <= ln.Rect.Y+ln.Rect.H {
			return i, len(lines), cr.X
		}
	}
	if len(lines) > 0 && mid > lines[len(lines)-1].Rect.Y {
		return len(lines) - 1, len(lines), cr.X
	}
	return 0, len(lines), cr.X
}

// caretWindowX is the window column of pos, used when no RichInput tracks it.
func (s *Session) caretWindowX(pos Position) float32 {
	if s.doc == nil || pos.Block < 0 || pos.Block >= len(s.doc.Blocks) {
		return 0
	}
	b := &s.doc.Blocks[pos.Block]
	if b.Kind == model.BlockTable && b.Table != nil {
		if row, col, _, _, ok := tableCellAtRuneRange(b.Table, pos.Offset, pos.Offset); ok {
			if cr, ok := s.tableCellRect(b.ID, row, col); ok {
				return cr.X + cr.W*0.5
			}
		}
	}
	if lay, tr, text, ok := s.blockTextLayout(pos.Block); ok {
		_, _, x := caretLine(lay, text, clampInt(pos.Offset, 0, utf8.RuneCountInString(text)))
		return tr.X + x
	}
	return s.blockRectValue(b.ID).X
}

// columnFor returns the sticky column if one is live, otherwise pos's own column.
func (s *Session) columnFor(pos Position) float32 {
	if s.navCarry {
		return s.navCarryX
	}
	return s.caretWindowX(pos)
}

// verticalStep moves pos one visual line up or down at column x, crossing into the
// neighbouring block at its edge line. ok is false at the document's first/last line.
func (s *Session) verticalStep(pos Position, dir int, x float32) (Position, bool) {
	b := &s.doc.Blocks[pos.Block]
	if b.Kind == model.BlockTable && b.Table != nil {
		if row, _, _, _, ok := tableCellAtRuneRange(b.Table, pos.Offset, pos.Offset); ok {
			if nr := row + dir; nr >= 0 && nr < len(b.Table.Rows) {
				return s.tableRowPositionAtX(pos.Block, nr, x, dir > 0, pos), true
			}
		}
	} else if lay, tr, text, ok := s.blockTextLayout(pos.Block); ok {
		n := utf8.RuneCountInString(text)
		line, count, _ := caretLine(lay, text, clampInt(pos.Offset, 0, n))
		if nl := line + dir; nl >= 0 && nl < count {
			ln := lay.Lines()[nl]
			cp := lay.PositionAt(ui.Point{X: x - tr.X, Y: ln.Rect.Y + ln.Rect.H*0.5})
			return Position{Block: pos.Block, Offset: clampInt(utf16OffsetToRuneOffset(text, cp.Index), 0, n)}, true
		}
	}
	target := s.stepVisibleBlock(pos.Block, dir)
	if target < 0 || target == pos.Block {
		return pos, false
	}
	return s.verticalEntry(target, dir, x), true
}

// horizontalStep moves pos one rune left or right, crossing to the neighbour's end/start.
func (s *Session) horizontalStep(pos Position, dir int) (Position, bool) {
	n := s.blockSelectionLen(pos.Block)
	switch {
	case dir < 0 && pos.Offset > 0:
		return Position{Block: pos.Block, Offset: min(pos.Offset, n) - 1}, true
	case dir > 0 && pos.Offset < n:
		return Position{Block: pos.Block, Offset: pos.Offset + 1}, true
	}
	target := s.stepVisibleBlock(pos.Block, dir)
	if target < 0 || target == pos.Block {
		return pos, false
	}
	if dir < 0 {
		return Position{Block: target, Offset: s.blockSelectionLen(target)}, true
	}
	return Position{Block: target}, true
}

// tableRowPositionAtX lands in table block idx, row row, in the column under window x,
// on the cell's first line (top) or last line. fallback is used without geometry.
func (s *Session) tableRowPositionAtX(idx, row int, x float32, top bool, fallback Position) Position {
	b := &s.doc.Blocks[idx]
	tbl := b.Table
	if tbl == nil || row < 0 || row >= len(tbl.Rows) || len(tbl.Rows[row]) == 0 {
		return fallback
	}
	col := 0
	if rr, ok := s.tableCellRect(b.ID, row, 0); ok {
		if _, c, ok := s.hitTableCellCoords(b.ID, x, rr.Y+rr.H*0.5); ok {
			col = c
		}
	} else if fr, fc, _, _, ok := tableCellAtRuneRange(tbl, fallback.Offset, fallback.Offset); ok && fr >= 0 {
		col = fc
	}
	col = clampInt(col, 0, len(tbl.Rows[row])-1)
	return Position{Block: idx, Offset: s.tableCellOffsetAtX(idx, row, col, x, top)}
}

// tableCellOffsetAtX is the block offset in cell (row, col) under window column x,
// on its first line when top, otherwise its last line.
func (s *Session) tableCellOffsetAtX(idx, row, col int, x float32, top bool) int {
	b := &s.doc.Blocks[idx]
	base := tableCellStartOffset(b.Table, row, col)
	cell := &b.Table.Rows[row][col]
	cellPlain := cell.PlainText()
	cellLen := utf8.RuneCountInString(cellPlain)
	if cellLen == 0 {
		return base
	}
	cr, ok := s.tableCellRect(b.ID, row, col)
	if !ok {
		if top {
			return base
		}
		return base + cellLen
	}
	tFontSz, _, _, _, _, _, tPadX := s.tableMetrics()
	weight := tableCellTextWeight(b.Table, row, col)
	cellBlock := model.Block{ID: b.ID, Kind: model.BlockParagraph, Spans: cell.Spans}
	layout := ui.ShapeRichTextWith(s.buildPureDisplaySpans(&cellBlock, tFontSz, weight), ui.Font{
		Family:     resolvedEditorFont(),
		Size:       tFontSz,
		Weight:     weight,
		LineHeight: tableCellLineHeight / tableCellFontSize,
	}, ui.TextShape{Width: max(cr.W-2*tPadX, 1), Align: tableCellTextAlign(b.Table, row, col)})
	lines := layout.Lines()
	if len(lines) == 0 {
		return base
	}
	ln := lines[0]
	if !top {
		ln = lines[len(lines)-1]
	}
	cp := layout.PositionAt(ui.Point{X: x - cr.X - tPadX, Y: ln.Rect.Y + ln.Rect.H*0.5})
	return base + clampInt(utf16OffsetToRuneOffset(cellPlain, cp.Index), 0, cellLen)
}

// navigateFromTableCell continues an arrow move that ran off cell (row, col) of table block idx:
// to the cell above/below at the same column, the previous/next cell, or out of the table.
func (s *Session) navigateFromTableCell(idx, row, col int, e appUi.RichEdge) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	tbl := s.doc.Blocks[idx].Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return false
	}
	if e.Vertical {
		nr := row + e.Dir
		if nr < 0 || nr >= len(tbl.Rows) {
			return s.navigatePastBlock(idx, e)
		}
		fallback := Position{Block: idx, Offset: tableCellStartOffset(tbl, nr, min(col, len(tbl.Rows[nr])-1))}
		s.placeCaret(s.tableRowPositionAtX(idx, nr, e.ColumnX, e.Dir > 0, fallback), e.Extend)
		s.rememberColumn(e.ColumnX)
		return true
	}
	cols := len(tbl.Rows[row])
	flat := row*cols + col + e.Dir
	if flat < 0 || flat >= len(tbl.Rows)*cols {
		return s.navigatePastBlock(idx, e)
	}
	nr, nc := flat/cols, flat%cols
	off := tableCellStartOffset(tbl, nr, nc)
	if e.Dir < 0 {
		off += utf8.RuneCountInString(tbl.Rows[nr][nc].PlainText())
	}
	s.placeCaret(Position{Block: idx, Offset: off}, e.Extend)
	return true
}

// navigateFromCodeKey lets arrow keys leave an editable code block at its edges;
// inside the block the native text area keeps handling them.
func (s *Session) navigateFromCodeKey(idx int, key ui.Key, mods ui.Modifiers, text string) bool {
	dir, vertical, extend, ok := arrowNav(key, mods)
	if !ok || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	if s.selHead.Block != idx || s.selAnchor.Block != idx {
		return false
	}
	n := utf8.RuneCountInString(text)
	caret := clampInt(s.selHead.Offset, 0, n)
	if !extend && s.selAnchor.Offset != s.selHead.Offset {
		return false
	}
	boundary := 0
	if dir > 0 {
		boundary = n
	}
	atEdge := caret == boundary
	x := s.blockRectValue(s.doc.Blocks[idx].ID).X
	if lay, tr, laidText, ok := s.blockTextLayout(idx); ok && laidText == text {
		line, count, cx := caretLine(lay, text, caret)
		x = tr.X + cx
		if vertical && !extend {
			atEdge = (dir < 0 && line == 0) || (dir > 0 && line >= count-1)
		}
	} else if vertical && !extend {
		atEdge = !containsNewline(text, caret, dir)
	}
	if !atEdge {
		return false
	}
	if s.navCarry && vertical {
		x = s.navCarryX
	}
	return s.navigatePastBlock(idx, appUi.RichEdge{Dir: dir, Vertical: vertical, Extend: extend, ColumnX: x})
}

// containsNewline reports a newline before (dir<0) or after (dir>0) rune offset off.
func containsNewline(text string, off, dir int) bool {
	i := 0
	for _, r := range text {
		if r == '\n' && ((dir < 0 && i < off) || (dir > 0 && i >= off)) {
			return true
		}
		i++
	}
	return false
}

// navigateFromAtomic moves off a focused divider, image or preview-only code block.
func (s *Session) navigateFromAtomic(idx int, key ui.Key, mods ui.Modifiers) bool {
	dir, vertical, extend, ok := arrowNav(key, mods)
	if !ok {
		return false
	}
	// Without a live column, Up/Down behave like Left/Right: end of the block above, start below.
	if vertical && !s.navCarry {
		vertical = false
	}
	// At the document's edge the key is still consumed so the block stays focused.
	s.navigatePastBlock(idx, appUi.RichEdge{Dir: dir, Vertical: vertical, Extend: extend, ColumnX: s.navCarryX})
	return true
}

// extendSelectionByArrow moves the head of an existing selection with Shift+arrows.
func (s *Session) extendSelectionByArrow(key ui.Key) bool {
	dir, vertical, _, ok := arrowNav(key, ui.Shift)
	if !ok {
		return false
	}
	head := s.clampPos(s.selHead)
	if vertical {
		x := s.columnFor(head)
		if next, ok := s.verticalStep(head, dir, x); ok {
			s.placeCaret(next, true)
		}
		s.rememberColumn(x)
		return true
	}
	s.navCarry = false
	if next, ok := s.horizontalStep(head, dir); ok {
		s.placeCaret(next, true)
	}
	return true
}
