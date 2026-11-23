package editor

import (
	"math"
	"runtime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"block/internal/codepreview"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type mouseSelMode uint8

const (
	mouseSelNone mouseSelMode = iota
	mouseSelText
	mouseSelBox
)

type tableCellKey struct {
	BlockID model.BlockID
	Row     int
	Col     int
}

type cachedBlockLayout struct {
	text       string
	spans      []model.Span
	rich       bool
	fontFamily string
	fontSize   float32
	weight     int
	lineHeight float32
	width      float32
	fontEpoch  uint64
	layout     *ui.TextLayout
}

func (s *Session) isMouseDragging() bool {
	return s.mouseMode != mouseSelNone
}

func (s *Session) hasMultiBlockSelection() bool {
	if !s.hasNonEmptySelection() {
		return false
	}
	a, b := s.orderedSelection()
	return a.Block != b.Block
}

func (s *Session) isPreviewOnlyCodeBlock(idx int) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[idx]
	if b.Kind != model.BlockCode {
		return false
	}
	return codepreview.IsPreviewLanguage(codepreview.CanonicalLanguage(b.Language)) && !b.ShowSource && strings.TrimSpace(b.Code) != ""
}

func (s *Session) hasWholeAtomicSelection() bool {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return false
	}
	a, b := s.orderedSelection()
	if a.Block != b.Block || a.Block < 0 || a.Block >= len(s.doc.Blocks) {
		return false
	}
	rlen := s.blockSelectionLen(a.Block)
	if a.Offset != 0 || b.Offset < rlen {
		return false
	}
	blk := &s.doc.Blocks[a.Block]
	k := blk.Kind
	if k == model.BlockTable && rlen > 0 {
		// Selecting text inside a single active cell (e.g. Shift+Right across a 1x1 table cell)
		// is an inline cell selection, not a whole-table atomic selection.
		if s.tableActiveBlock == blk.ID && blk.Table != nil {
			if _, _, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
				return false
			}
		}
		return true
	}
	return k == model.BlockImage || k == model.BlockDivider || s.isPreviewOnlyCodeBlock(a.Block)
}

func (s *Session) hasMultiCellTableSelection() bool {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return false
	}
	a, b := s.orderedSelection()
	if a.Block != b.Block || a.Block < 0 || a.Block >= len(s.doc.Blocks) {
		return false
	}
	blk := &s.doc.Blocks[a.Block]
	if blk.Kind != model.BlockTable || blk.Table == nil {
		return false
	}
	_, _, _, _, singleCell := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset)
	return !singleCell
}

func (s *Session) hasFocusedAtomicBlock() bool {
	if s.doc == nil || s.focusedBlock < 0 || s.focusedBlock >= len(s.doc.Blocks) {
		return false
	}
	if s.imagePopKind == "alt" {
		return false
	}
	k := s.doc.Blocks[s.focusedBlock].Kind
	return k == model.BlockImage || k == model.BlockDivider || s.isPreviewOnlyCodeBlock(s.focusedBlock)
}

func (s *Session) needsDetachedSelectionFocus() bool {
	if s.isMouseDragging() {
		return false
	}
	if s.toolbarPop == "link" || s.toolbarPop == "comment" || s.findOpen || s.inlineAI != nil || s.editingAnnotID != "" {
		return false
	}
	if s.readOnly && s.hasNonEmptySelection() {
		return true
	}
	return s.hasMultiBlockSelection() || s.hasMultiCellTableSelection() || s.hasWholeAtomicSelection() || s.hasFocusedAtomicBlock()
}

func (s *Session) blockSelectionLen(idx int) int {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return 0
	}
	b := &s.doc.Blocks[idx]
	if b.Kind == model.BlockImage {
		return max(utf8.RuneCountInString(b.ImageAlt), 1)
	}
	return utf8.RuneCountInString(b.PlainText())
}

// blockSelectionSpanRange returns the selected [lo, hi) rune range inside block idx,
// whether the entire block is covered, and whether the block participates in the selection.
func (s *Session) blockSelectionSpanRange(idx int) (lo, hi int, fullBlock, selected bool) {
	if !s.hasNonEmptySelection() || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return 0, 0, false, false
	}
	a, b := s.orderedSelection()
	if idx < a.Block || idx > b.Block {
		return 0, 0, false, false
	}
	rlen := s.blockSelectionLen(idx)
	if a.Block != b.Block && idx == b.Block && b.Offset == 0 {
		if rlen == 0 {
			return 0, 0, true, true
		}
		return 0, 0, false, false
	}
	if a.Block != b.Block && idx == a.Block && a.Offset >= rlen && rlen > 0 {
		return 0, 0, false, false
	}
	if rlen == 0 {
		return 0, 0, true, true
	}
	lo = 0
	if idx == a.Block {
		lo = clampInt(a.Offset, 0, rlen)
	}
	hi = rlen
	if idx == b.Block {
		hi = clampInt(b.Offset, lo, rlen)
	}
	if hi <= lo {
		return 0, 0, false, false
	}
	return lo, hi, lo == 0 && hi == rlen, true
}

func (s *Session) hasMultipleVisibleBlocks() bool {
	first, last := s.firstAndLastVisibleBlocks()
	return first >= 0 && first != last
}

// SelectCurrentBlockOrAll selects the focused block first, or selects every visible block
// when the focused block is already selected in full (or has no selectable text).
func (s *Session) SelectCurrentBlockOrAll() {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	if s.hasMultiBlockSelection() {
		s.SelectAllBlocks()
		return
	}
	idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	if s.isBlockHiddenByFold(idx) {
		if first, _ := s.firstAndLastVisibleBlocks(); first >= 0 {
			idx = first
		}
	}
	rlen := s.blockSelectionLen(idx)
	a, b := s.orderedSelection()
	curFull := s.hasNonEmptySelection() && a.Block == idx && b.Block == idx && a.Offset == 0 && b.Offset >= rlen
	if !curFull && rlen > 0 {
		s.SelectRange(idx, 0, idx, rlen)
		if !s.needsDetachedSelectionFocus() && !s.readOnly {
			s.focusPending = true
		}
		return
	}
	if s.hasMultipleVisibleBlocks() {
		s.SelectAllBlocks()
		return
	}
	if rlen > 0 {
		s.SelectRange(idx, 0, idx, rlen)
	}
	if !s.needsDetachedSelectionFocus() && !s.readOnly {
		s.focusPending = true
	}
}

// SelectAllBlocks selects every visible block in the document.
func (s *Session) SelectAllBlocks() {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	first, last := -1, -1
	for i := range s.doc.Blocks {
		if s.isBlockHiddenByFold(i) {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		return
	}
	endOff := s.blockSelectionLen(last)
	s.tableActiveBlock = 0
	s.SelectRange(first, 0, last, endOff)
}

// DeleteSelection deletes the active single-block or multi-block selection in one undo step.
func (s *Session) DeleteSelection() bool {
	return s.ReplaceSelectionWithText("")
}

// copyFocusedAtomicBlock copies the currently focused atomic block (image, divider, or preview-only code)
// as multi-format Markdown/HTML/(PNG for local images) to the clipboard via persistent ui.Services.
func (s *Session) copyFocusedAtomicBlock(svc ui.Services) bool {
	if !s.hasFocusedAtomicBlock() {
		return false
	}
	blk := &s.doc.Blocks[s.focusedBlock]
	cp := blk.Clone()
	cp.ID = model.NewBlockID()
	cp.Dirty = true
	cp.CleanSource = ""
	single := []model.Block{cp}
	normalizeCopiedBlocks(single)
	if md := strings.TrimSuffix(s.serializeBlocks(single), "\n"); md != "" {
		s.writeRichClipboard(svc, md, &model.Doc{Blocks: single, TrailingLine: "\n"}, true)
		return true
	}
	return false
}

// CopySelection copies the current selection (or focused atomic block) as Markdown/HTML to clipboard.
func (s *Session) CopySelection(svc ui.Services) bool {
	if s.hasNonEmptySelection() {
		return s.writeSelectionToClipboard(svc)
	}
	return s.copyFocusedAtomicBlock(svc)
}

// CutSelection copies the selected Markdown/HTML to clipboard and deletes the selection when editable.
func (s *Session) CutSelection(svc ui.Services) bool {
	if !s.CopySelection(svc) {
		return false
	}
	if s.readOnly {
		return true
	}
	if !s.hasNonEmptySelection() {
		s.deleteBlock(s.focusedBlock)
		return true
	}
	return s.DeleteSelection()
}

// detachedCaretRect returns the IME caret rectangle in editor-root local coordinates
// for multi-block, multi-cell table, and atomic block selections.
func (s *Session) detachedCaretRect() ui.Rect {
	fallback := ui.Rect{X: 0, Y: 0, W: 1, H: 16}
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return fallback
	}
	idx := -1
	if s.hasNonEmptySelection() {
		a, _ := s.orderedSelection()
		idx = a.Block
	} else if s.hasFocusedAtomicBlock() {
		idx = s.focusedBlock
	}
	if idx < 0 || idx >= len(s.doc.Blocks) {
		return fallback
	}
	blk := &s.doc.Blocks[idx]
	var r ui.Rect
	var ok bool
	if blk.Kind == model.BlockImage {
		r, ok = s.imageRect(blk.ID)
	}
	if !ok || r.H <= 0 {
		r, ok = s.blockTextRect(blk.ID)
	}
	if !ok || r.H <= 0 {
		r, ok = s.blockRect(blk.ID)
	}
	if !ok || r.H <= 0 {
		return fallback
	}
	lx := r.X - s.editorRect.X
	ly := r.Y - s.editorRect.Y
	h := clampFloat32(r.H, 16, 28)
	if s.editorRect.W > 0 {
		lx = clampFloat32(lx, 0, max(s.editorRect.W-2, 0))
	}
	if s.editorRect.H > 0 {
		ly = clampFloat32(ly, 0, max(s.editorRect.H-h, 0))
	}
	return ui.Rect{X: lx, Y: ly, W: 1, H: h}
}

// ReplaceSelectionWithText replaces the current selection (single-block or multi-block)
// with plain text in one undo step and places the caret right after the inserted text.
func (s *Session) ReplaceSelectionWithText(insert string) bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 || !s.hasNonEmptySelection() {
		return false
	}
	a, b := s.orderedSelection()
	if a.Block < 0 || b.Block >= len(s.doc.Blocks) {
		return false
	}

	s.pushUndo()
	s.firstLineDeleteArmed = false
	s.clearAutoConvertState()
	s.closeTransientOverlays()

	// Single-block selection
	if a.Block == b.Block {
		idx := a.Block
		blk := &s.doc.Blocks[idx]
		rlen := s.blockSelectionLen(idx)
		whole := a.Offset == 0 && b.Offset >= rlen

		if blk.Kind == model.BlockImage || blk.Kind == model.BlockDivider || (blk.Kind == model.BlockTable && whole && s.hasWholeAtomicSelection()) || (whole && s.isPreviewOnlyCodeBlock(idx)) {
			if insert != "" || len(s.doc.Blocks) == 1 {
				nb := model.Block{ID: blk.ID, Kind: model.BlockParagraph, Dirty: true}
				nb.SetText(insert)
				s.doc.Blocks[idx] = nb
				s.focusedBlock = idx
				off := utf8.RuneCountInString(insert)
				s.selAnchor = Position{Block: idx, Offset: off}
				s.selHead = s.selAnchor
			} else {
				s.spliceBlocks(idx, idx+1, nil)
				nextIdx := min(idx, len(s.doc.Blocks)-1)
				s.focusedBlock = nextIdx
				s.selAnchor = Position{Block: nextIdx, Offset: 0}
				s.selHead = s.selAnchor
			}
			s.multiSelFocusPending = s.needsDetachedSelectionFocus()
			s.focusPending = !s.multiSelFocusPending
			s.selSyncPending = !s.multiSelFocusPending
			s.markDirty()
			return true
		}

		if blk.Kind == model.BlockTable && blk.Table != nil {
			if row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
				cell := &blk.Table.Rows[row][col]
				var ins []model.Span
				if insert != "" {
					ins = []model.Span{{Text: insert}}
				}
				nextSpans, nextRel := replaceSpanRange(cell.Spans, relLo, relHi, ins)
				cell.Spans = nextSpans
				blk.Dirty = true
				blk.CleanSource = ""
				s.focusedBlock = idx
				s.tableActiveBlock = blk.ID
				s.tableActiveRow = row
				s.tableActiveCol = col
				caret := tableCellStartOffset(blk.Table, row, col) + nextRel
				s.selAnchor = Position{Block: idx, Offset: caret}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.multiSelFocusPending = false
				s.markDirty()
				return true
			}
			// Multi-cell selection inside table: clear touched text ranges inside each cell.
			clearTableRange(blk.Table, a.Offset, b.Offset, insert)
			blk.Dirty = true
			blk.CleanSource = ""
			caret := a.Offset + utf8.RuneCountInString(insert)
			s.focusedBlock = idx
			if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, caret, caret); ok {
				s.tableActiveBlock = blk.ID
				s.tableActiveRow = row
				s.tableActiveCol = col
			}
			s.selAnchor = Position{Block: idx, Offset: caret}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}

		s.replaceCurrentSelectionInBlock(idx, insert)
		s.focusedBlock = idx
		s.focusPending = true
		s.multiSelFocusPending = false
		return true
	}

	// Multi-block selection (a.Block < b.Block)
	firstIdx, lastIdx := a.Block, b.Block
	first := &s.doc.Blocks[firstIdx]
	last := &s.doc.Blocks[lastIdx]
	firstLen := s.blockSelectionLen(firstIdx)
	lastLen := s.blockSelectionLen(lastIdx)

	isRichBlock := func(blk *model.Block) bool {
		switch blk.Kind {
		case model.BlockCode, model.BlockRawHTML, model.BlockTable, model.BlockImage, model.BlockDivider:
			return false
		default:
			return true
		}
	}

	// Whole-block deletion across [firstIdx..lastIdx] with no replacement text
	if a.Offset == 0 && b.Offset >= lastLen && insert == "" {
		if firstIdx == 0 && lastIdx == len(s.doc.Blocks)-1 {
			nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
			s.doc.Blocks = []model.Block{nb}
			s.focusedBlock = 0
			s.selAnchor = Position{Block: 0, Offset: 0}
			s.selHead = s.selAnchor
		} else {
			s.spliceBlocks(firstIdx, lastIdx+1, nil)
			targetIdx := min(firstIdx, len(s.doc.Blocks)-1)
			off := 0
			if firstIdx > 0 && targetIdx == firstIdx-1 {
				off = s.blockSelectionLen(targetIdx)
			}
			s.focusedBlock = targetIdx
			s.selAnchor = Position{Block: targetIdx, Offset: off}
			s.selHead = s.selAnchor
		}
		s.focusPending = true
		s.selSyncPending = true
		s.multiSelFocusPending = false
		s.markDirty()
		return true
	}

	// Both endpoints are inline rich-text blocks: merge prefix + insert + suffix cleanly.
	if isRichBlock(first) && isRichBlock(last) {
		prefix := sliceSpans(first.Spans, 0, clampInt(a.Offset, 0, firstLen))
		suffix := sliceSpans(last.Spans, clampInt(b.Offset, 0, lastLen), lastLen)
		var merged []model.Span
		merged = append(merged, prefix...)
		if insert != "" {
			merged = append(merged, model.Span{Text: insert})
		}
		merged = append(merged, suffix...)
		updated := first.Clone()
		if a.Offset == 0 && b.Offset < lastLen {
			updated.Kind = last.Kind
			updated.Indent = last.Indent
			updated.Callout = last.Callout
			updated.Checked = last.Checked
		}
		updated.Spans = normalizeSpans(merged)
		updated.Dirty = true
		updated.CleanSource = ""
		s.spliceBlocks(firstIdx, lastIdx+1, []model.Block{updated})
		caret := clampInt(a.Offset, 0, firstLen) + utf8.RuneCountInString(insert)
		s.focusedBlock = firstIdx
		s.selAnchor = Position{Block: firstIdx, Offset: caret}
		s.selHead = s.selAnchor
		s.focusPending = true
		s.selSyncPending = true
		s.multiSelFocusPending = false
		s.markDirty()
		return true
	}

	// Mixed block kinds (code / table / image / divider + rich text)
	var replacement []model.Block
	caretBlockOffset := 0
	caretRuneOffset := 0
	placedCaret := false

	if a.Offset > 0 {
		switch {
		case first.Kind == model.BlockCode || first.Kind == model.BlockRawHTML:
			runes := []rune(first.Code)
			keep := clampInt(a.Offset, 0, len(runes))
			cp := first.Clone()
			cp.SetText(string(runes[:keep]) + insert)
			replacement = append(replacement, cp)
			caretBlockOffset = 0
			caretRuneOffset = keep + utf8.RuneCountInString(insert)
			placedCaret = true
			insert = ""
		case isRichBlock(first):
			cp := first.Clone()
			prefix := sliceSpans(first.Spans, 0, clampInt(a.Offset, 0, firstLen))
			if insert != "" {
				prefix = append(prefix, model.Span{Text: insert})
			}
			cp.Spans = normalizeSpans(prefix)
			cp.Dirty = true
			cp.CleanSource = ""
			replacement = append(replacement, cp)
			caretBlockOffset = 0
			caretRuneOffset = clampInt(a.Offset, 0, firstLen) + utf8.RuneCountInString(insert)
			placedCaret = true
			insert = ""
		case a.Offset >= firstLen:
			replacement = append(replacement, first.Clone())
		}
	}

	if insert != "" {
		nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
		nb.SetText(insert)
		caretBlockOffset = len(replacement)
		caretRuneOffset = utf8.RuneCountInString(insert)
		placedCaret = true
		replacement = append(replacement, nb)
	}

	if b.Offset < lastLen {
		switch {
		case b.Offset == 0:
			if !placedCaret {
				caretBlockOffset = len(replacement)
				caretRuneOffset = 0
				placedCaret = true
			}
			replacement = append(replacement, last.Clone())
		case last.Kind == model.BlockCode || last.Kind == model.BlockRawHTML:
			runes := []rune(last.Code)
			cut := clampInt(b.Offset, 0, len(runes))
			cp := last.Clone()
			cp.SetText(string(runes[cut:]))
			if !placedCaret {
				caretBlockOffset = len(replacement)
				caretRuneOffset = 0
				placedCaret = true
			}
			replacement = append(replacement, cp)
		case isRichBlock(last):
			suffix := sliceSpans(last.Spans, clampInt(b.Offset, 0, lastLen), lastLen)
			if len(replacement) > 0 && isRichBlock(&replacement[len(replacement)-1]) {
				prevIdx := len(replacement) - 1
				prevLen := utf8.RuneCountInString(replacement[prevIdx].PlainText())
				replacement[prevIdx].Spans = normalizeSpans(append(replacement[prevIdx].Spans, suffix...))
				if !placedCaret {
					caretBlockOffset = prevIdx
					caretRuneOffset = prevLen
					placedCaret = true
				}
			} else {
				cp := last.Clone()
				cp.Spans = normalizeSpans(suffix)
				cp.Dirty = true
				cp.CleanSource = ""
				if !placedCaret {
					caretBlockOffset = len(replacement)
					caretRuneOffset = 0
					placedCaret = true
				}
				replacement = append(replacement, cp)
			}
		}
	}

	if len(replacement) == 0 && len(s.doc.Blocks) == (lastIdx-firstIdx+1) {
		replacement = append(replacement, model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true})
	}

	s.spliceBlocks(firstIdx, lastIdx+1, replacement)
	targetIdx := clampInt(firstIdx+caretBlockOffset, 0, len(s.doc.Blocks)-1)
	if !placedCaret && len(replacement) == 0 {
		targetIdx = clampInt(firstIdx, 0, len(s.doc.Blocks)-1)
		caretRuneOffset = 0
	}
	caretRuneOffset = clampInt(caretRuneOffset, 0, s.blockSelectionLen(targetIdx))
	s.focusedBlock = targetIdx
	s.selAnchor = Position{Block: targetIdx, Offset: caretRuneOffset}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.multiSelFocusPending = false
	s.markDirty()
	return true
}

func clearTableRange(tbl *model.TableData, startRune, endRune int, insertFirst string) {
	if tbl == nil || startRune >= endRune {
		return
	}
	pos := 0
	inserted := false
	for rIdx := range tbl.Rows {
		for cIdx := range tbl.Rows[rIdx] {
			cell := &tbl.Rows[rIdx][cIdx]
			clen := utf8.RuneCountInString(cell.PlainText())
			cStart, cEnd := pos, pos+clen
			ovLo := max(startRune, cStart) - cStart
			ovHi := min(endRune, cEnd) - cStart
			if ovLo < ovHi {
				var merged []model.Span
				merged = append(merged, sliceSpans(cell.Spans, 0, ovLo)...)
				if !inserted && insertFirst != "" {
					merged = append(merged, model.Span{Text: insertFirst})
					inserted = true
				}
				merged = append(merged, sliceSpans(cell.Spans, ovHi, clen)...)
				cell.Spans = normalizeSpans(merged)
			}
			pos = cEnd
			if cIdx+1 < len(tbl.Rows[rIdx]) {
				pos++
			}
		}
		if rIdx+1 < len(tbl.Rows) {
			pos++
		}
	}
}

func (s *Session) handleFocusedAtomicInput(svc ui.Services, ev ui.InputEvent) bool {
	idx := s.focusedBlock
	if idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	copyAtomic := func() {
		s.copyFocusedAtomicBlock(svc)
	}
	switch ev.Kind {
	case ui.InputCommand:
		switch ev.Text {
		case "copy":
			copyAtomic()
			return true
		case "cut":
			return s.CutSelection(svc)
		case "paste":
			if !s.readOnly {
				s.PasteFromClipboard(svc, svc.ReadClipboard(), false)
			}
			return true
		case "pasteAndMatchStyle":
			if !s.readOnly {
				s.PastePlainFromClipboard(svc, svc.ReadClipboard(), false)
			}
			return true
		case "selectAll":
			s.SelectCurrentBlockOrAll()
			return true
		case "delete":
			if !s.readOnly {
				s.deleteBlock(idx)
			}
			return true
		case "undo":
			if !s.readOnly {
				s.Undo()
			}
			return true
		case "redo":
			if !s.readOnly {
				s.Redo()
			}
			return true
		}
	case ui.InputCompose:
		if s.readOnly || s.lightbox.Open {
			return false
		}
		s.detachedCompose = ev.Text
		return true
	case ui.InputText:
		s.detachedCompose = ""
		if s.lightbox.Open {
			return true
		}
		if s.readOnly || ev.Text == "" {
			return false
		}
		s.pushUndo()
		nb := model.Block{ID: s.doc.Blocks[idx].ID, Kind: model.BlockParagraph, Dirty: true}
		nb.SetText(ev.Text)
		s.doc.Blocks[idx] = nb
		off := utf8.RuneCountInString(ev.Text)
		s.selAnchor = Position{Block: idx, Offset: off}
		s.selHead = s.selAnchor
		s.focusPending = true
		s.selSyncPending = true
		s.multiSelFocusPending = false
		s.markDirty()
		return true
	case ui.InputKeyDown:
		switch {
		case ev.Mods == ui.Cmd && ev.Key == ui.KeyA:
			s.SelectCurrentBlockOrAll()
			return true
		case (ev.Mods == ui.Cmd && ev.Key == ui.KeyC) || (ev.Mods == ui.Ctrl && ev.Key == ui.KeyInsert):
			copyAtomic()
			return true
		case (ev.Mods == ui.Cmd && ev.Key == ui.KeyX) || (ev.Mods == ui.Shift && ev.Key == ui.KeyDelete):
			return s.CutSelection(svc)
		case (ev.Mods == ui.Cmd && ev.Key == ui.KeyV) || (ev.Mods == ui.Shift && ev.Key == ui.KeyInsert):
			if !s.readOnly {
				s.PasteFromClipboard(svc, svc.ReadClipboard(), false)
			}
			return true
		case (ev.Mods == (ui.Cmd|ui.Shift) || ev.Mods == (ui.Cmd|ui.Shift|ui.Alt)) && ev.Key == ui.KeyV:
			if !s.readOnly {
				s.PastePlainFromClipboard(svc, svc.ReadClipboard(), false)
			}
			return true
		case !s.readOnly && ev.Mods == 0 && (ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete):
			if ev.Key == ui.KeyBackspace && s.tryUndoAtomicAutoConvertBackspace() {
				return true
			}
			s.deleteBlock(idx)
			return true
		case s.hasMenuPopupOpen() && ev.Mods == 0 && (ev.Key == ui.KeyUp || ev.Key == ui.KeyDown || ev.Key == ui.KeyLeft || ev.Key == ui.KeyRight || ev.Key == ui.KeyEnter || ev.Key == ui.KeyEscape):
			return false
		case !s.hasMenuPopupOpen() && !s.lightbox.Open && ev.Mods == 0 &&
			(ev.Key == ui.KeyF || (theme.IsMacOS() && ev.Key == ui.KeySpace)) &&
			s.doc.Blocks[idx].Kind == model.BlockImage && strings.TrimSpace(s.doc.Blocks[idx].ImageSrc) != "":
			s.OpenMediaLightbox(s.doc.Blocks[idx].ID)
			return true
		case !s.readOnly && ev.Mods == 0 && ev.Key == ui.KeyEnter:
			s.pushUndo()
			nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
			s.spliceBlocks(idx+1, idx+1, []model.Block{nb})
			s.FocusBlock(idx + 1)
			s.markDirty()
			return true
		default:
			if s.navigateFromAtomic(idx, ev.Key, ev.Mods) {
				return true
			}
		}
	}
	return false
}

func (s *Session) handleMultiSelectionInput(svc ui.Services, ev ui.InputEvent) bool {
	if !s.hasNonEmptySelection() {
		if s.hasFocusedAtomicBlock() {
			return s.handleFocusedAtomicInput(svc, ev)
		}
		return false
	}
	switch ev.Kind {
	case ui.InputCommand:
		switch ev.Text {
		case "copy":
			s.CopySelection(svc)
			return true
		case "cut":
			return s.CutSelection(svc)
		case "paste":
			if s.readOnly {
				return true
			}
			s.PasteFromClipboard(svc, svc.ReadClipboard(), false)
			return true
		case "pasteAndMatchStyle":
			if s.readOnly {
				return true
			}
			s.PastePlainFromClipboard(svc, svc.ReadClipboard(), false)
			return true
		case "selectAll":
			s.SelectAllBlocks()
			return true
		case "delete":
			if s.readOnly {
				return true
			}
			return s.DeleteSelection()
		case "undo":
			if !s.readOnly {
				s.Undo()
			}
			return true
		case "redo":
			if !s.readOnly {
				s.Redo()
			}
			return true
		case "bold":
			if !s.readOnly {
				s.ToggleMarkOnSelection(model.MarkBold)
			}
			return true
		case "italic":
			if !s.readOnly {
				s.ToggleMarkOnSelection(model.MarkItalic)
			}
			return true
		case "underline":
			if !s.readOnly {
				s.ToggleMarkOnSelection(model.MarkUnderline)
			}
			return true
		case "strikethrough":
			if !s.readOnly {
				s.ToggleMarkOnSelection(model.MarkStrike)
			}
			return true
		}

	case ui.InputCompose:
		if s.readOnly {
			return false
		}
		s.detachedCompose = ev.Text
		return true

	case ui.InputText:
		s.detachedCompose = ""
		if s.readOnly || ev.Text == "" {
			return false
		}
		return s.ReplaceSelectionWithText(ev.Text)

	case ui.InputKeyDown:
		switch {
		case ev.Mods == ui.Cmd && ev.Key == ui.KeyA:
			s.SelectAllBlocks()
			return true
		case (ev.Mods == ui.Cmd && ev.Key == ui.KeyC) || (ev.Mods == ui.Ctrl && ev.Key == ui.KeyInsert):
			s.CopySelection(svc)
			return true
		case (ev.Mods == ui.Cmd && ev.Key == ui.KeyX) || (ev.Mods == ui.Shift && ev.Key == ui.KeyDelete):
			return s.CutSelection(svc)
		case (ev.Mods == ui.Cmd && ev.Key == ui.KeyV) || (ev.Mods == ui.Shift && ev.Key == ui.KeyInsert):
			if !s.readOnly {
				s.PasteFromClipboard(svc, svc.ReadClipboard(), false)
			}
			return true
		case (ev.Mods == (ui.Cmd|ui.Shift) || ev.Mods == (ui.Cmd|ui.Shift|ui.Alt)) && ev.Key == ui.KeyV:
			if !s.readOnly {
				s.PastePlainFromClipboard(svc, svc.ReadClipboard(), false)
			}
			return true
		case !s.readOnly && ev.Mods == 0 && (ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete):
			if ev.Key == ui.KeyBackspace && s.tryUndoAtomicAutoConvertBackspace() {
				return true
			}
			return s.DeleteSelection()
		case s.hasMenuPopupOpen() && ev.Mods == 0 && (ev.Key == ui.KeyUp || ev.Key == ui.KeyDown || ev.Key == ui.KeyLeft || ev.Key == ui.KeyRight || ev.Key == ui.KeyEnter || ev.Key == ui.KeyEscape):
			return false
		case !s.readOnly && ev.Mods == 0 && ev.Key == ui.KeyEnter:
			if !s.DeleteSelection() {
				return false
			}
			idx := s.focusedBlock
			if idx >= 0 && idx < len(s.doc.Blocks) {
				s.handleEnterOnBlock(idx, s.doc.Blocks[idx].PlainText(), s.selHead.Offset)
			}
			return true
		case ev.Mods == 0 && (ev.Key == ui.KeyLeft || ev.Key == ui.KeyUp):
			a, _ := s.orderedSelection()
			s.focusedBlock = a.Block
			s.selAnchor = a
			s.selHead = a
			s.syncCollapsedTableCellFocus(a)
			s.focusPending = !s.readOnly
			s.selSyncPending = !s.readOnly
			s.multiSelFocusPending = false
			return true
		case ev.Mods == 0 && (ev.Key == ui.KeyRight || ev.Key == ui.KeyDown):
			_, b := s.orderedSelection()
			s.focusedBlock = b.Block
			s.selAnchor = b
			s.selHead = b
			s.syncCollapsedTableCellFocus(b)
			s.focusPending = !s.readOnly
			s.selSyncPending = !s.readOnly
			s.multiSelFocusPending = false
			return true
		case s.navigateDocEdge(ev.Key, ev.Mods):
			return true
		case ev.Mods == ui.Shift && (ev.Key == ui.KeyUp || ev.Key == ui.KeyDown || ev.Key == ui.KeyLeft || ev.Key == ui.KeyRight):
			return s.extendSelectionByArrow(ev.Key)
		case ev.Mods == 0 && ev.Key == ui.KeyEscape:
			_, b := s.orderedSelection()
			s.selAnchor = b
			s.selHead = b
			s.focusedBlock = b.Block
			s.syncCollapsedTableCellFocus(b)
			s.focusPending = !s.readOnly
			s.selSyncPending = !s.readOnly
			s.multiSelFocusPending = false
			s.closeTransientOverlays()
			return true
		}
	}
	return false
}

func (s *Session) syncCollapsedTableCellFocus(pos Position) {
	if s.doc == nil || pos.Block < 0 || pos.Block >= len(s.doc.Blocks) {
		return
	}
	blk := &s.doc.Blocks[pos.Block]
	if blk.Kind != model.BlockTable || blk.Table == nil {
		return
	}
	if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, pos.Offset, pos.Offset); ok {
		s.tableActiveBlock = blk.ID
		s.tableActiveRow = row
		s.tableActiveCol = col
	}
}

func (s *Session) stepVisibleBlock(from, dir int) int {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return -1
	}
	for i := from + dir; i >= 0 && i < len(s.doc.Blocks); i += dir {
		if !s.isBlockHiddenByFold(i) {
			return i
		}
	}
	return from
}

// --- Mouse Pointer Hit-Testing & Multi-Block Drag Selection ---

func (s *Session) isAnyInputComposing() bool {
	if s == nil {
		return false
	}
	if s.detachedCompose != "" {
		return true
	}
	for _, st := range s.richInputs {
		if st != nil && st.Composing() {
			return true
		}
	}
	for _, st := range s.tableInputs {
		if st != nil && st.Composing() {
			return true
		}
	}
	return false
}

func (s *Session) clearInlinePathContextMenuLock() {
	s.ctxMenuPtrValid = false
	s.ctxMenuLocked = false
	s.ctxMenuLockedFromPtr = false
	s.ctxMenuLockedValid = false
	s.ctxMenuLockedRaw = ""
	s.ctxMenuLockedTarget = resolvedPathTarget{}
	s.ctxMenuOwnerKey = ""
}

func (s *Session) onScrollObserveInput(ev ui.InputEvent) {
	isContextClick := ev.Kind == ui.InputPointerDown && (ev.Button == 1 || (runtime.GOOS == "darwin" && ev.Button == 0 && ev.Mods&ui.Ctrl != 0))
	if isContextClick {
		var pt ui.Point
		switch {
		case !s.embedded && s.scrollRect.W > 0:
			pt = ui.Point{X: s.scrollRect.X + ev.X, Y: s.scrollRect.Y + ev.Y}
		case s.editorRect.W > 0:
			pt = ui.Point{X: s.editorRect.X + ev.X, Y: s.editorRect.Y + ev.Y}
		default:
			if wp, ok := s.windowPointerStatus(); ok {
				pt = wp
			}
		}
		s.clearInlinePathContextMenuLock()
		s.lastPressCmdIntact = false
		s.ctxMenuPtrValid = true
		s.ctxMenuPtrWin = pt
		return
	}
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
		s.clearInlinePathContextMenuLock()
		s.lastPressMods = ev.Mods
		s.lastPressCmdIntact = ev.Mods == ui.Cmd
		s.lastPressClicks = max(ev.Clicks, 1)
		s.lastPressLocal = ui.Point{X: ev.X, Y: ev.Y}
		s.lastPressScrollY = s.bodyScroll.Y
		s.lastPressCanceled = false
		s.lastPressComposing = s.isAnyInputComposing()
		s.lastReleaseMods = 0
		s.lastReleaseValid = false
		s.pressJustBegan = true
		s.navCarry = false
		return
	}
	if ev.Kind == ui.InputPointerDown {
		s.clearInlinePathContextMenuLock()
		s.lastPressCmdIntact = false
		return
	}
	if ev.Kind == ui.InputPointerUp && ev.Button == 0 {
		s.lastReleaseMods = ev.Mods
		s.lastReleaseValid = true
		if ev.Canceled {
			s.lastPressCanceled = true
			s.lastPressCmdIntact = false
		} else if ev.Mods != ui.Cmd {
			s.lastPressCmdIntact = false
		}
		return
	}
	if s.lastPressCmdIntact {
		switch ev.Kind {
		case ui.InputKeyDown, ui.InputKeyUp, ui.InputPointerMove, ui.InputScroll, ui.InputZoom:
			if ev.Mods != ui.Cmd {
				s.lastPressCmdIntact = false
			}
		case ui.InputText, ui.InputCompose, ui.InputCommand:
			s.lastPressCmdIntact = false
		}
	}
	if ev.Kind == ui.InputKeyDown {
		// Any key press reaching the editor means any previously shown native context menu
		// has closed, and Shift+F10 / KeyContextMenu begins a new keyboard menu instance.
		s.clearInlinePathContextMenuLock()
	}
}

func (s *Session) windowPointer(root ui.Element) (wx, wy float32) {
	rx, ry, _ := root.PointerPosition()
	return s.editorRect.X + rx, s.editorRect.Y + ry
}

func (s *Session) windowPointerStatus() (pt ui.Point, ok bool) {
	rx, ry, over := s.frameRoot.PointerPosition()
	if !over {
		return ui.Point{}, false
	}
	return ui.Point{X: s.editorRect.X + rx, Y: s.editorRect.Y + ry}, true
}

// blockHitBoundsY returns the vertical hit-test territory of visible block idx,
// splitting inter-block topGaps at their midpoints so top and bottom edges behave symmetrically.
func (s *Session) blockHitBoundsY(idx int) (top, bottom float32, ok bool) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return 0, 0, false
	}
	r, hasRect := s.blockRect(s.doc.Blocks[idx].ID)
	if !hasRect || r.H <= 0 {
		return 0, 0, false
	}
	top = r.Y
	if prev := s.stepVisibleBlock(idx, -1); prev >= 0 && prev < idx {
		topGap := min(s.blockGap(&s.doc.Blocks[prev], &s.doc.Blocks[idx]), max(r.H-1, 0))
		top = r.Y + topGap*0.5
	}
	bottom = r.Y + r.H
	if next := s.stepVisibleBlock(idx, 1); next > idx && next < len(s.doc.Blocks) {
		nextGap := s.blockGap(&s.doc.Blocks[idx], &s.doc.Blocks[next])
		if nextR, hasNext := s.blockRect(s.doc.Blocks[next].ID); hasNext && nextR.H > 0 {
			nextGap = min(nextGap, max(nextR.H-1, 0))
		}
		bottom = r.Y + r.H + nextGap*0.5
	}
	return top, bottom, true
}

// preUpdateFocusedDrag updates cross-block selection ahead of building rows when an
// already-focused block input is being dragged across other blocks, ensuring 0-frame lag
// even when dragging upwards to earlier blocks.
func (s *Session) preUpdateFocusedDrag(c *ui.Context, root ui.Element) {
	if s.mouseMode != mouseSelText || s.mouseOriginBlock < 0 || s.doc == nil {
		return
	}
	if s.mouseOriginBlock >= len(s.doc.Blocks) {
		s.mouseMode = mouseSelNone
		s.mouseOriginBlock = -1
		return
	}
	wx, wy := s.windowPointer(root)
	s.mouseCurPt = ui.Point{X: wx, Y: wy}
	if math.Hypot(float64(wx-s.mouseStartPt.X), float64(wy-s.mouseStartPt.Y)) > 3 {
		s.mouseDragged = true
	}
	if topY, botY, ok := s.blockHitBoundsY(s.mouseOriginBlock); ok && (wy < topY || wy > botY) {
		s.selAnchor = s.mouseTextAnchor
		s.selHead = s.hitPositionAtPoint(wx, wy)
		s.autoScrollOnDrag(c, wy)
	}
}

// updateCanvasMouseSelection handles pointer press, drag, and release initiated on
// unfocused blocks or margins/gutters of editor-canvas.
func (s *Session) updateCanvasMouseSelection(c *ui.Context, root, canvas ui.Element, readOnly bool) {
	_, _, canvasHeld := canvas.Dragged()
	canvasClicked := canvas.Clicked()
	if s.lastPressCmdIntact && (!c.WindowActive() || c.Modifiers() != ui.Cmd) {
		s.lastPressCmdIntact = false
	}
	if canvasHeld {
		wx, wy := s.windowPointer(root)
		curPt := ui.Point{X: wx, Y: wy}

		if s.mouseMode == mouseSelNone || s.mouseOriginBlock >= 0 || s.pressJustBegan {
			startPt := curPt
			docPt := ui.Point{
				X: startPt.X - s.scrollRect.X,
				Y: startPt.Y - s.scrollRect.Y + s.bodyScroll.Y,
			}
			if s.pressJustBegan && s.scrollRect.W > 0 {
				startPt = ui.Point{
					X: s.scrollRect.X + s.lastPressLocal.X,
					Y: s.scrollRect.Y + s.lastPressLocal.Y - (s.bodyScroll.Y - s.lastPressScrollY),
				}
				docPt = ui.Point{
					X: s.lastPressLocal.X,
					Y: s.lastPressLocal.Y + s.lastPressScrollY,
				}
			}
			s.pressJustBegan = false
			s.mouseOriginBlock = -1
			s.mouseStartPt = startPt
			s.mouseCurPt = curPt
			s.mouseStartDocPt = docPt
			s.mouseDragged = math.Hypot(float64(curPt.X-startPt.X), float64(curPt.Y-startPt.Y)) > 3
			s.firstLineDeleteArmed = false
			s.closeTransientOverlays()

			if s.lastPressMods&ui.Shift != 0 {
				s.mouseMode = mouseSelText
				s.selAnchor = s.clampPos(s.selAnchor)
				s.selHead = s.hitPositionAtPoint(curPt.X, curPt.Y)
			} else if s.isMarginBoxSelectStart(startPt.X, startPt.Y) {
				s.mouseMode = mouseSelBox
			} else {
				s.mouseMode = mouseSelText
				pos := s.hitPositionAtPoint(startPt.X, startPt.Y)
				switch {
				case s.lastPressClicks >= 3:
					a := Position{Block: pos.Block, Offset: 0}
					b := Position{Block: pos.Block, Offset: s.blockSelectionLen(pos.Block)}
					s.mouseUnitRange = [2]Position{a, b}
					s.selAnchor, s.selHead = a, b
				case s.lastPressClicks == 2:
					w0, w1 := s.wordRangeAt(pos.Block, pos.Offset)
					a := Position{Block: pos.Block, Offset: w0}
					b := Position{Block: pos.Block, Offset: w1}
					s.mouseUnitRange = [2]Position{a, b}
					s.selAnchor, s.selHead = a, b
				default:
					s.mouseTextAnchor = pos
					s.selAnchor = pos
					s.selHead = pos
				}
			}
		}

		s.mouseCurPt = curPt
		startWinX := s.scrollRect.X + s.mouseStartDocPt.X
		startWinY := s.scrollRect.Y + s.mouseStartDocPt.Y - s.bodyScroll.Y
		if math.Hypot(float64(curPt.X-startWinX), float64(curPt.Y-startWinY)) > 3 {
			s.mouseDragged = true
		}

		switch s.mouseMode {
		case mouseSelText:
			head := s.hitPositionAtPoint(curPt.X, curPt.Y)
			switch {
			case s.lastPressMods&ui.Shift != 0:
				s.selHead = head
			case s.lastPressClicks >= 3:
				s.extendBlockUnitSelection(head.Block)
			case s.lastPressClicks == 2:
				s.extendWordUnitSelection(head)
			default:
				s.selAnchor = s.mouseTextAnchor
				s.selHead = head
			}
			s.autoScrollOnDrag(c, curPt.Y)

		case mouseSelBox:
			if s.mouseDragged {
				s.applyBoxSelection(curPt.X, curPt.Y)
				s.autoScrollOnDrag(c, curPt.Y)
			}
		}
		return
	}

	// Released after a canvas-owned gesture
	if s.mouseMode != mouseSelNone && s.mouseOriginBlock < 0 {
		mode := s.mouseMode
		dragged := s.mouseDragged
		canceled := s.lastPressCanceled || canvas.PressCanceled() || !c.WindowActive()
		endPt := s.mouseCurPt
		if !dragged && (s.mouseStartPt.X != 0 || s.mouseStartPt.Y != 0) {
			endPt = s.mouseStartPt
		}
		s.mouseMode = mouseSelNone
		s.mouseDragged = false
		s.pressJustBegan = false
		s.lastPressCanceled = false
		if canceled {
			s.lastPressCmdIntact = false
			return
		}
		if mode == mouseSelBox && dragged {
			s.applyBoxSelection(endPt.X, endPt.Y)
		}
		s.finishCanvasPointerSelection(c, mode, dragged, endPt, readOnly)
		return
	}

	// Same-frame tap-to-click (PointerDown + PointerUp delivered before next frame build)
	if canvasClicked {
		pt := s.windowPointerPoint(root)
		if s.pressJustBegan && s.scrollRect.W > 0 {
			pt = ui.Point{
				X: s.scrollRect.X + s.lastPressLocal.X,
				Y: s.scrollRect.Y + s.lastPressLocal.Y,
			}
		}
		s.pressJustBegan = false
		s.firstLineDeleteArmed = false
		s.closeTransientOverlays()
		if s.lastPressMods&ui.Shift != 0 {
			s.selAnchor = s.clampPos(s.selAnchor)
			s.selHead = s.hitPositionAtPoint(pt.X, pt.Y)
			s.finishCanvasPointerSelection(c, mouseSelText, false, pt, readOnly)
			return
		}
		if s.isMarginBoxSelectStart(pt.X, pt.Y) {
			s.finishCanvasPointerSelection(c, mouseSelBox, false, pt, readOnly)
			return
		}
		pos := s.hitPositionAtPoint(pt.X, pt.Y)
		switch {
		case s.lastPressClicks >= 3:
			s.selAnchor = Position{Block: pos.Block, Offset: 0}
			s.selHead = Position{Block: pos.Block, Offset: s.blockSelectionLen(pos.Block)}
		case s.lastPressClicks == 2:
			w0, w1 := s.wordRangeAt(pos.Block, pos.Offset)
			s.selAnchor = Position{Block: pos.Block, Offset: w0}
			s.selHead = Position{Block: pos.Block, Offset: w1}
		default:
			s.selAnchor = pos
			s.selHead = pos
		}
		s.finishCanvasPointerSelection(c, mouseSelText, false, pt, readOnly)
	}
}

func (s *Session) windowPointerPoint(root ui.Element) ui.Point {
	wx, wy := s.windowPointer(root)
	return ui.Point{X: wx, Y: wy}
}

func (s *Session) scheduleRebuild(c *ui.Context) {
	c.AnimationFrame()
	c.Invalidate()
}

func (s *Session) finishCanvasPointerSelection(c *ui.Context, mode mouseSelMode, dragged bool, pt ui.Point, readOnly bool) {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	releaseMods := c.Modifiers()
	if s.lastReleaseValid {
		releaseMods = s.lastReleaseMods
	}
	s.lastReleaseValid = false
	cmdIntact := s.lastPressCmdIntact && c.WindowActive() && c.Modifiers() == ui.Cmd
	s.lastPressCmdIntact = false
	exactCmdClick := cmdIntact && s.lastPressMods == ui.Cmd && releaseMods == ui.Cmd && s.lastPressClicks == 1 && !s.lastPressComposing && !s.isAnyInputComposing()
	if mode == mouseSelText && !dragged && c.WindowActive() && (readOnly || exactCmdClick) {
		pos := s.hitPositionAtPoint(pt.X, pt.Y)
		if pos.Block >= 0 && pos.Block < len(s.doc.Blocks) {
			blk := &s.doc.Blocks[pos.Block]
			link := s.hitExplicitLinkAtPoint(pos.Block, blk, pt)
			if link == "" && blk.Kind != model.BlockTable {
				link = spanLinkAtOffset(blk, pos.Offset)
			}
			if link != "" {
				s.selAnchor = pos
				s.selHead = pos
				s.openBlockLink(c.Services(), link)
				s.scheduleRebuild(c)
				return
			}
			if exactCmdClick && s.previewMedia == nil {
				if cand, ok := s.hitInlinePathAtPoint(pos.Block, blk, pt); ok {
					res, ready := s.probeInlinePath(cand.raw)
					if !ready || res.exists || res.permErr {
						s.openInlinePathLink(c.Services(), cand.raw)
						s.scheduleRebuild(c)
						return
					}
				}
			}
		}
	}
	if mode == mouseSelBox && !dragged {
		firstVis, lastVis := s.firstAndLastVisibleBlocks()
		if firstVis < 0 {
			return
		}
		if !readOnly && s.lastPressMods&(ui.Shift|ui.Alt) == 0 && s.tryBlankMarginInsert(pt.X, pt.Y) {
			s.scheduleRebuild(c)
			return
		}
		if !readOnly && s.isAboveFirstVisibleBlock(pt.Y) {
			s.FocusBlock(firstVis)
			s.scheduleRebuild(c)
			return
		}
		if !readOnly && s.isBelowLastVisibleBlock(pt.Y) {
			lastBlk := &s.doc.Blocks[lastVis]
			switch lastBlk.Kind {
			case model.BlockDivider, model.BlockImage, model.BlockTable, model.BlockCode, model.BlockRawHTML:
				s.insertOrFocusBottomParagraph()
			default:
				endOff := s.blockSelectionLen(lastVis)
				s.focusedBlock = lastVis
				s.selAnchor = Position{Block: lastVis, Offset: endOff}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.multiSelFocusPending = false
			}
			s.scheduleRebuild(c)
			return
		}
		idx := s.nearestVisibleBlockAtY(pt.Y)
		if idx < 0 {
			return
		}
		if k := s.doc.Blocks[idx].Kind; k == model.BlockDivider || k == model.BlockImage || s.isPreviewOnlyCodeBlock(idx) {
			s.FocusBlock(idx)
			s.focusPreventScroll = true
			if readOnly {
				s.focusPending = false
				s.selSyncPending = false
			}
			s.scheduleRebuild(c)
			return
		}
		pos := s.hitPositionAtPoint(pt.X, pt.Y)
		s.selAnchor = pos
		s.selHead = pos
	}

	idx := clampInt(s.selHead.Block, 0, len(s.doc.Blocks)-1)
	s.focusedBlock = idx
	if s.needsDetachedSelectionFocus() {
		s.multiSelFocusPending = true
		s.focusPending = false
		s.selSyncPending = false
		s.scheduleRebuild(c)
		return
	}

	a, b := s.orderedSelection()
	s.multiSelFocusPending = false

	blk := &s.doc.Blocks[idx]
	if blk.Kind == model.BlockTable && blk.Table != nil && !readOnly {
		if rIdx, cIdx, ok := s.hitTableCellCoords(blk.ID, pt.X, pt.Y); ok && a == b {
			s.tableActiveBlock = blk.ID
			s.tableActiveRow = rIdx
			s.tableActiveCol = cIdx
			s.focusPending = true
			s.focusPreventScroll = true
			s.selSyncPending = true
			s.scheduleRebuild(c)
			return
		}
		if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
			s.tableActiveBlock = blk.ID
			s.tableActiveRow = row
			s.tableActiveCol = col
			s.focusPending = true
			s.focusPreventScroll = true
			s.selSyncPending = true
			s.scheduleRebuild(c)
			return
		}
	}

	if !readOnly {
		s.focusPending = true
		s.focusPreventScroll = true
		s.selSyncPending = true
	}
	s.scheduleRebuild(c)
}

// updateFocusedInputDrag bridges an active drag inside a focused TextAreaBase (or code block)
// into a live multi-block selection when the pointer leaves the block's vertical bounds.
func (s *Session) updateFocusedInputDrag(c *ui.Context, root, in ui.Element, idx int) bool {
	_, _, held := in.Dragged()
	if held {
		wx, wy := s.windowPointer(root)
		anchorOff, caretOff := in.TextAnchorCaret()
		topY, botY, hasBounds := s.blockHitBoundsY(idx)
		outsideBlock := hasBounds && (wy < topY || wy > botY)

		if s.mouseMode == mouseSelNone || s.mouseOriginBlock != idx || s.pressJustBegan {
			s.pressJustBegan = false
			s.mouseMode = mouseSelText
			s.mouseOriginBlock = idx
			s.mouseStartPt = ui.Point{X: wx, Y: wy}
			s.mouseCurPt = s.mouseStartPt
			s.mouseDragged = false
			s.closeTransientOverlays()
			if s.lastPressMods&ui.Shift != 0 && s.selAnchor.Block != idx {
				s.mouseTextAnchor = s.clampPos(s.selAnchor)
			} else {
				s.mouseTextAnchor = Position{Block: idx, Offset: anchorOff}
			}
		} else if !outsideBlock && s.lastPressMods&ui.Shift == 0 && s.lastPressClicks >= 2 {
			s.mouseTextAnchor = Position{Block: idx, Offset: anchorOff}
		}

		s.mouseCurPt = ui.Point{X: wx, Y: wy}
		if math.Hypot(float64(wx-s.mouseStartPt.X), float64(wy-s.mouseStartPt.Y)) > 3 {
			s.mouseDragged = true
		}

		if outsideBlock || s.mouseTextAnchor.Block != idx {
			prevHead := s.selHead
			s.selAnchor = s.mouseTextAnchor
			s.selHead = s.hitPositionAtPoint(wx, wy)
			s.autoScrollOnDrag(c, wy)
			if s.selHead != prevHead && s.selHead.Block < idx {
				s.scheduleRebuild(c)
			}
		} else {
			s.selAnchor = Position{Block: idx, Offset: anchorOff}
			s.selHead = Position{Block: idx, Offset: caretOff}
		}
		return true
	}

	if s.mouseMode == mouseSelText && s.mouseOriginBlock == idx {
		s.mouseMode = mouseSelNone
		s.mouseOriginBlock = -1
		s.mouseDragged = false
		s.pressJustBegan = false
		if s.needsDetachedSelectionFocus() {
			if s.doc != nil && len(s.doc.Blocks) > 0 {
				s.focusedBlock = clampInt(s.selHead.Block, 0, len(s.doc.Blocks)-1)
			}
			s.multiSelFocusPending = true
			s.focusPending = false
			s.scheduleRebuild(c)
		}
	}
	return false
}

// updateFocusedTableCellDrag promotes a drag starting inside a focused table cell input
// into a multi-cell or multi-block selection when dragged outside that cell.
func (s *Session) updateFocusedTableCellDrag(c *ui.Context, root, in ui.Element, blockIdx int, blockID model.BlockID, rIdx, cIdx int) bool {
	_, _, held := in.Dragged()
	if held {
		wx, wy := s.windowPointer(root)
		anchorOff, caretOff := in.TextAnchorCaret()
		cellBase := tableCellStartOffset(s.doc.Blocks[blockIdx].Table, rIdx, cIdx)

		if s.mouseMode == mouseSelNone || s.mouseOriginBlock != blockIdx || s.pressJustBegan {
			s.pressJustBegan = false
			s.mouseMode = mouseSelText
			s.mouseOriginBlock = blockIdx
			s.mouseStartPt = ui.Point{X: wx, Y: wy}
			s.mouseCurPt = s.mouseStartPt
			s.mouseDragged = false
			s.closeTransientOverlays()
			if s.lastPressMods&ui.Shift != 0 && s.selAnchor.Block != blockIdx {
				s.mouseTextAnchor = s.clampPos(s.selAnchor)
			} else {
				s.mouseTextAnchor = Position{Block: blockIdx, Offset: cellBase + anchorOff}
			}
		}

		cellRect, hasCell := s.tableCellRect(blockID, rIdx, cIdx)
		inSameCell := hasCell && wx >= cellRect.X && wx <= cellRect.X+cellRect.W && wy >= cellRect.Y && wy <= cellRect.Y+cellRect.H
		if inSameCell && s.mouseTextAnchor.Block == blockIdx {
			s.selAnchor = Position{Block: blockIdx, Offset: cellBase + anchorOff}
			s.selHead = Position{Block: blockIdx, Offset: cellBase + caretOff}
		} else {
			prevHead := s.selHead
			s.selAnchor = s.mouseTextAnchor
			s.selHead = s.hitPositionAtPoint(wx, wy)
			s.autoScrollOnDrag(c, wy)
			if s.selHead != prevHead {
				s.scheduleRebuild(c)
			}
		}
		return true
	}

	if s.mouseMode == mouseSelText && s.mouseOriginBlock == blockIdx {
		s.mouseMode = mouseSelNone
		s.mouseOriginBlock = -1
		s.mouseDragged = false
		s.pressJustBegan = false
		if s.needsDetachedSelectionFocus() {
			if s.doc != nil && len(s.doc.Blocks) > 0 {
				s.focusedBlock = clampInt(s.selHead.Block, 0, len(s.doc.Blocks)-1)
			}
			s.multiSelFocusPending = true
			s.focusPending = false
			s.scheduleRebuild(c)
		}
	}
	return false
}

// updateFocusedRichInputDrag bridges an active pointer drag inside a RichInput into live
// multi-block selection when the pointer crosses out of the block's vertical bounds.
func (s *Session) updateFocusedRichInputDrag(c *ui.Context, root ui.Element, st *appUi.RichInputState, idx int) bool {
	if st != nil && st.Dragging() {
		wx, wy := s.windowPointer(root)
		anchorOff, caretOff := st.SelectionRunes()
		topY, botY, hasBounds := s.blockHitBoundsY(idx)
		outsideBlock := hasBounds && (wy < topY || wy > botY)

		if s.mouseMode == mouseSelNone || s.mouseOriginBlock != idx || s.pressJustBegan {
			s.pressJustBegan = false
			s.mouseMode = mouseSelText
			s.mouseOriginBlock = idx
			s.mouseStartPt = ui.Point{X: wx, Y: wy}
			s.mouseCurPt = s.mouseStartPt
			s.mouseDragged = false
			s.closeTransientOverlays()
			if s.lastPressMods&ui.Shift != 0 && s.selAnchor.Block != idx {
				s.mouseTextAnchor = s.clampPos(s.selAnchor)
			} else {
				s.mouseTextAnchor = Position{Block: idx, Offset: anchorOff}
			}
		} else if !outsideBlock && s.lastPressMods&ui.Shift == 0 && s.lastPressClicks >= 2 {
			s.mouseTextAnchor = Position{Block: idx, Offset: anchorOff}
		}

		s.mouseCurPt = ui.Point{X: wx, Y: wy}
		if math.Hypot(float64(wx-s.mouseStartPt.X), float64(wy-s.mouseStartPt.Y)) > 3 {
			s.mouseDragged = true
		}

		if outsideBlock || s.mouseTextAnchor.Block != idx {
			prevHead := s.selHead
			s.selAnchor = s.mouseTextAnchor
			s.selHead = s.hitPositionAtPoint(wx, wy)
			s.autoScrollOnDrag(c, wy)
			if s.selHead != prevHead && s.selHead.Block < idx {
				s.scheduleRebuild(c)
			}
		} else {
			s.selAnchor = Position{Block: idx, Offset: anchorOff}
			s.selHead = Position{Block: idx, Offset: caretOff}
		}
		return true
	}

	if s.mouseMode == mouseSelText && s.mouseOriginBlock == idx {
		s.mouseMode = mouseSelNone
		s.mouseOriginBlock = -1
		s.mouseDragged = false
		s.pressJustBegan = false
		if s.needsDetachedSelectionFocus() {
			if s.doc != nil && len(s.doc.Blocks) > 0 {
				s.focusedBlock = clampInt(s.selHead.Block, 0, len(s.doc.Blocks)-1)
			}
			s.multiSelFocusPending = true
			s.focusPending = false
			s.scheduleRebuild(c)
		}
	}
	return false
}

// updateFocusedRichTableCellDrag promotes a drag inside a RichInput table cell into
// multi-cell or multi-block selection when dragged outside that cell.
func (s *Session) updateFocusedRichTableCellDrag(c *ui.Context, root ui.Element, st *appUi.RichInputState, blockIdx int, blockID model.BlockID, rIdx, cIdx int) bool {
	if st != nil && st.Dragging() {
		wx, wy := s.windowPointer(root)
		anchorOff, caretOff := st.SelectionRunes()
		cellBase := tableCellStartOffset(s.doc.Blocks[blockIdx].Table, rIdx, cIdx)

		if s.mouseMode == mouseSelNone || s.mouseOriginBlock != blockIdx || s.pressJustBegan {
			s.pressJustBegan = false
			s.mouseMode = mouseSelText
			s.mouseOriginBlock = blockIdx
			s.mouseStartPt = ui.Point{X: wx, Y: wy}
			s.mouseCurPt = s.mouseStartPt
			s.mouseDragged = false
			s.closeTransientOverlays()
			if s.lastPressMods&ui.Shift != 0 && s.selAnchor.Block != blockIdx {
				s.mouseTextAnchor = s.clampPos(s.selAnchor)
			} else {
				s.mouseTextAnchor = Position{Block: blockIdx, Offset: cellBase + anchorOff}
			}
		}

		cellRect, hasCell := s.tableCellRect(blockID, rIdx, cIdx)
		inSameCell := hasCell && wx >= cellRect.X && wx <= cellRect.X+cellRect.W && wy >= cellRect.Y && wy <= cellRect.Y+cellRect.H
		if inSameCell && s.mouseTextAnchor.Block == blockIdx {
			s.selAnchor = Position{Block: blockIdx, Offset: cellBase + anchorOff}
			s.selHead = Position{Block: blockIdx, Offset: cellBase + caretOff}
		} else {
			prevHead := s.selHead
			s.selAnchor = s.mouseTextAnchor
			s.selHead = s.hitPositionAtPoint(wx, wy)
			s.autoScrollOnDrag(c, wy)
			if s.selHead != prevHead {
				s.scheduleRebuild(c)
			}
		}
		return true
	}

	if s.mouseMode == mouseSelText && s.mouseOriginBlock == blockIdx {
		s.mouseMode = mouseSelNone
		s.mouseOriginBlock = -1
		s.mouseDragged = false
		s.pressJustBegan = false
		if s.needsDetachedSelectionFocus() {
			if s.doc != nil && len(s.doc.Blocks) > 0 {
				s.focusedBlock = clampInt(s.selHead.Block, 0, len(s.doc.Blocks)-1)
			}
			s.multiSelFocusPending = true
			s.focusPending = false
			s.scheduleRebuild(c)
		}
	}
	return false
}

func (s *Session) isMarginBoxSelectStart(wx, wy float32) bool {
	firstVis, _ := s.firstAndLastVisibleBlocks()
	if firstVis < 0 {
		return true
	}
	if s.isAboveFirstVisibleBlock(wy) || s.isBelowLastVisibleBlock(wy) {
		return true
	}
	idx := s.nearestVisibleBlockAtY(wy)
	if idx < 0 {
		return true
	}
	r, ok := s.blockRect(s.doc.Blocks[idx].ID)
	if !ok || r.W <= 0 {
		return false
	}
	bleed := s.bandBleed()
	contentLeft := r.X + s.leftContentGutter() - bleed
	contentRight := r.X + r.W - s.rightContentInset() + bleed
	return wx < contentLeft-4 || wx > contentRight+4
}

func (s *Session) applyBoxSelection(curWX, curWY float32) {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	startWinY := s.scrollRect.Y + s.mouseStartDocPt.Y - s.bodyScroll.Y
	minY := min(startWinY, curWY)
	maxY := max(startWinY, curWY)

	firstHit, lastHit := -1, -1
	var prevVis *model.Block
	for i := range s.doc.Blocks {
		if s.isBlockHiddenByFold(i) {
			continue
		}
		cur := &s.doc.Blocks[i]
		r, ok := s.blockRect(cur.ID)
		if !ok || r.H <= 0 {
			prevVis = cur
			continue
		}
		topGap := float32(0)
		if tr, hasT := s.blockTextRect(cur.ID); hasT && tr.Y > r.Y {
			topGap = min(tr.Y-r.Y, r.H*0.5)
		} else if prevVis != nil {
			topGap = min(s.blockGap(prevVis, cur), r.H*0.5)
		}
		prevVis = cur
		bTop := r.Y + topGap*0.6
		bBot := r.Y + r.H
		if bBot >= minY && bTop <= maxY {
			if firstHit < 0 {
				firstHit = i
			}
			lastHit = i
		}
	}

	if firstHit < 0 {
		if s.focusedBlock >= 0 {
			s.selAnchor = Position{Block: s.focusedBlock, Offset: 0}
			s.selHead = s.selAnchor
		}
		return
	}

	if curWY >= startWinY {
		s.selAnchor = Position{Block: firstHit, Offset: 0}
		s.selHead = Position{Block: lastHit, Offset: s.blockSelectionLen(lastHit)}
	} else {
		s.selAnchor = Position{Block: lastHit, Offset: s.blockSelectionLen(lastHit)}
		s.selHead = Position{Block: firstHit, Offset: 0}
	}
}

func (s *Session) renderBoxSelectMarquee(c *ui.Context) {
	if s.mouseMode != mouseSelBox || !s.mouseDragged {
		return
	}
	pal := theme.Ocean()
	startX := s.scrollRect.X + s.mouseStartDocPt.X
	startY := s.scrollRect.Y + s.mouseStartDocPt.Y - s.bodyScroll.Y
	curX := s.mouseCurPt.X
	curY := s.mouseCurPt.Y

	rx := min(startX, curX)
	ry := min(startY, curY)
	rw := float32(math.Abs(float64(curX - startX)))
	rh := float32(math.Abs(float64(curY - startY)))

	if s.scrollRect.W > 0 && s.scrollRect.H > 0 {
		x0 := max(rx, s.scrollRect.X)
		y0 := max(ry, s.scrollRect.Y)
		x1 := min(rx+rw, s.scrollRect.X+s.scrollRect.W)
		y1 := min(ry+rh, s.scrollRect.Y+s.scrollRect.H)
		if x1 <= x0 || y1 <= y0 {
			return
		}
		rx, ry, rw, rh = x0, y0, x1-x0, y1-y0
	}
	if rw < 2 && rh < 2 {
		return
	}

	ui.Overlay(c, func() {
		ui.Box(c.Key("editor-marquee")).Absolute().
			Left(rx).Top(ry).Size(rw, rh).Radius(theme.RadiusXS).
			Background(pal.Accent.Alpha(0.10)).
			Border(1, pal.Accent.Alpha(0.45)).
			IgnoreInput(true)
	})
}

func (s *Session) autoScrollOnDrag(c *ui.Context, wy float32) {
	if s.scrollRect.H <= 0 || s.bodyScroll.MaxY <= 0 {
		return
	}
	const edge = float32(36)
	var dy float32
	topDist := wy - s.scrollRect.Y
	botDist := (s.scrollRect.Y + s.scrollRect.H) - wy
	switch {
	case topDist < edge:
		dy = -min(edge-topDist, edge*2) * 0.45
	case botDist < edge:
		dy = min(edge-botDist, edge*2) * 0.45
	}
	if dy != 0 {
		nextY := clampFloat32(s.bodyScroll.Y+dy, 0, s.bodyScroll.MaxY)
		if nextY != s.bodyScroll.Y {
			s.bodyScroll.Y = nextY
			c.AnimationFrame()
		}
	}
}

func (s *Session) firstAndLastVisibleBlocks() (first, last int) {
	first, last = -1, -1
	if s.doc == nil {
		return
	}
	for i := range s.doc.Blocks {
		if s.isBlockHiddenByFold(i) {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	return
}

func (s *Session) nearestVisibleBlockAtY(wy float32) int {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return -1
	}
	firstVis, lastVis := -1, -1
	bestIdx := -1
	bestDist := float32(1e9)
	var prevVis *model.Block
	for i := range s.doc.Blocks {
		if s.isBlockHiddenByFold(i) {
			continue
		}
		cur := &s.doc.Blocks[i]
		r, ok := s.blockRect(cur.ID)
		if !ok || r.H <= 0 {
			prevVis = cur
			continue
		}
		if firstVis < 0 {
			firstVis = i
		}
		lastVis = i
		topGap := float32(0)
		if prevVis != nil {
			topGap = min(s.blockGap(prevVis, cur), max(r.H-1, 0))
		}
		prevVis = cur
		vTop := r.Y + topGap
		vBot := r.Y + r.H
		if wy >= vTop && wy <= vBot {
			return i
		}
		var d float32
		if wy < vTop {
			d = vTop - wy
		} else {
			d = wy - vBot
		}
		if d < bestDist {
			bestDist = d
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		if firstVis >= 0 {
			if firstR, ok := s.blockRect(s.doc.Blocks[firstVis].ID); ok && wy < firstR.Y {
				return firstVis
			}
		}
		if lastVis >= 0 {
			if lastR, ok := s.blockRect(s.doc.Blocks[lastVis].ID); ok && wy > lastR.Y+lastR.H {
				return lastVis
			}
		}
		return bestIdx
	}
	if firstVis >= 0 {
		return firstVis
	}
	return 0
}

func (s *Session) hitPositionAtPoint(wx, wy float32) Position {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return Position{}
	}
	firstVis, lastVis := s.firstAndLastVisibleBlocks()
	if firstVis < 0 {
		return Position{}
	}
	if r, ok := s.blockRect(s.doc.Blocks[firstVis].ID); ok && r.H > 0 && wy < r.Y {
		return Position{Block: firstVis, Offset: 0}
	}
	if r, ok := s.blockRect(s.doc.Blocks[lastVis].ID); ok && r.H > 0 && wy > r.Y+r.H {
		return Position{Block: lastVis, Offset: s.blockSelectionLen(lastVis)}
	}

	idx := s.nearestVisibleBlockAtY(wy)
	if idx < 0 {
		return Position{}
	}
	b := &s.doc.Blocks[idx]
	rowRect := s.blockRectValue(b.ID)

	switch b.Kind {
	case model.BlockDivider, model.BlockImage:
		rlen := s.blockSelectionLen(idx)
		midY := rowRect.Y + rowRect.H*0.5
		if ir, ok := s.imageRect(b.ID); ok && ir.H > 0 {
			midY = ir.Y + ir.H*0.5
		}
		if wy < midY {
			return Position{Block: idx, Offset: 0}
		}
		return Position{Block: idx, Offset: rlen}

	case model.BlockCode, model.BlockRawHTML:
		if s.isPreviewOnlyCodeBlock(idx) {
			rlen := s.blockSelectionLen(idx)
			if wy < rowRect.Y+rowRect.H*0.5 {
				return Position{Block: idx, Offset: 0}
			}
			return Position{Block: idx, Offset: rlen}
		}
		off := s.hitOffsetInCodeBlock(b, rowRect, wx, wy)
		return Position{Block: idx, Offset: off}

	case model.BlockTable:
		off := s.hitOffsetInTableBlock(idx, b, rowRect, wx, wy)
		return Position{Block: idx, Offset: off}

	default:
		off := s.hitOffsetInRichBlock(idx, b, rowRect, wx, wy)
		return Position{Block: idx, Offset: off}
	}
}

func (s *Session) hitOffsetInRichBlock(idx int, b *model.Block, rowRect ui.Rect, wx, wy float32) int {
	plain := b.PlainText()
	if plain == "" {
		return 0
	}
	rlen := utf8.RuneCountInString(plain)
	tr, ok := s.blockTextRect(b.ID)
	if !ok || tr.W <= 0 {
		if wy < rowRect.Y+rowRect.H*0.5 {
			return 0
		}
		return rlen
	}
	lx := wx - tr.X
	ly := wy - tr.Y
	if ly < -4 {
		return 0
	}
	if ly > tr.H+4 {
		return rlen
	}
	fontSize, weight, lh := s.textMetrics(b.Kind)
	layout := s.cachedShapeBlock(b.ID, plain, b.Spans, idx, resolvedEditorFont(), fontSize, weight, lh, tr.W, true)
	if layout == nil {
		return rlen
	}
	cp := layout.PositionAt(ui.Point{X: lx, Y: clampFloat32(ly, 0, max(tr.H-0.5, 0))})
	return clampInt(utf16OffsetToRuneOffset(plain, cp.Index), 0, rlen)
}

func (s *Session) hitOffsetInCodeBlock(b *model.Block, rowRect ui.Rect, wx, wy float32) int {
	rlen := utf8.RuneCountInString(b.Code)
	if rlen == 0 {
		return 0
	}
	tr, ok := s.blockTextRect(b.ID)
	if !ok || tr.W <= 0 {
		if wy < rowRect.Y+rowRect.H*0.5 {
			return 0
		}
		return rlen
	}
	lx := wx - tr.X
	ly := wy - tr.Y
	if ly < -4 {
		return 0
	}
	if ly > tr.H+4 {
		return rlen
	}
	layout := s.cachedShapeBlock(b.ID, b.Code, nil, 0, resolvedCodeFont(), s.codeFontSize(), theme.WeightRegular, 1.5, tr.W, false)
	if layout == nil {
		return rlen
	}
	cp := layout.PositionAt(ui.Point{X: lx, Y: clampFloat32(ly, 0, max(tr.H-0.5, 0))})
	return clampInt(utf16OffsetToRuneOffset(b.Code, cp.Index), 0, rlen)
}

func (s *Session) hitTableCellCoords(blockID model.BlockID, wx, wy float32) (bestR, bestC int, ok bool) {
	if len(s.tableCellDocRects) == 0 {
		return 0, 0, false
	}
	var tbl *model.TableData
	if bIdx := s.blockIndexByID(blockID); bIdx >= 0 && bIdx < len(s.doc.Blocks) {
		tbl = s.doc.Blocks[bIdx].Table
	}
	bestDist := float32(1e9)
	for k := range s.tableCellDocRects {
		if k.BlockID != blockID {
			continue
		}
		if tbl != nil && (k.Row < 0 || k.Row >= len(tbl.Rows) || k.Col < 0 || k.Col >= len(tbl.Rows[k.Row])) {
			continue
		}
		r, rOk := s.tableCellRect(k.BlockID, k.Row, k.Col)
		if !rOk || r.W <= 0 || r.H <= 0 {
			continue
		}
		if wx >= r.X && wx <= r.X+r.W && wy >= r.Y && wy <= r.Y+r.H {
			return k.Row, k.Col, true
		}
		dx := max(r.X-wx, wx-(r.X+r.W), 0)
		dy := max(r.Y-wy, wy-(r.Y+r.H), 0)
		if d := dx*dx + dy*dy; d < bestDist || (d == bestDist && (!ok || k.Row < bestR || (k.Row == bestR && k.Col < bestC))) {
			bestDist = d
			bestR, bestC = k.Row, k.Col
			ok = true
		}
	}
	return bestR, bestC, ok
}

func tableCellStartOffset(tbl *model.TableData, targetRow, targetCol int) int {
	if tbl == nil {
		return 0
	}
	pos := 0
	for rIdx, row := range tbl.Rows {
		for cIdx, cell := range row {
			if rIdx == targetRow && cIdx == targetCol {
				return pos
			}
			pos += utf8.RuneCountInString(cell.PlainText())
			if cIdx+1 < len(row) {
				pos++
			}
		}
		if rIdx+1 < len(tbl.Rows) {
			pos++
		}
	}
	return pos
}

func (s *Session) hitOffsetInTableBlock(idx int, b *model.Block, rowRect ui.Rect, wx, wy float32) int {
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return 0
	}
	totalLen := s.blockSelectionLen(idx)
	rIdx, cIdx, ok := s.hitTableCellCoords(b.ID, wx, wy)
	if !ok {
		if wy < rowRect.Y+rowRect.H*0.5 {
			return 0
		}
		return totalLen
	}
	if rIdx >= len(b.Table.Rows) || cIdx >= len(b.Table.Rows[rIdx]) {
		return totalLen
	}
	base := tableCellStartOffset(b.Table, rIdx, cIdx)
	cell := &b.Table.Rows[rIdx][cIdx]
	cellPlain := cell.PlainText()
	cellLen := utf8.RuneCountInString(cellPlain)
	if cellLen == 0 {
		return base
	}
	cr, _ := s.tableCellRect(b.ID, rIdx, cIdx)
	tFontSz, tLineH, _, _, _, tPadY, tPadX := s.tableMetrics()
	weight := tableCellTextWeight(b.Table, rIdx, cIdx)
	cellBlock := model.Block{ID: b.ID, Kind: model.BlockParagraph, Spans: cell.Spans}
	uiSpans := s.buildPureDisplaySpans(&cellBlock, tFontSz, weight)
	layout := ui.ShapeRichTextWith(uiSpans, ui.Font{
		Family:     resolvedEditorFont(),
		Size:       tFontSz,
		Weight:     weight,
		LineHeight: tableCellLineHeight / tableCellFontSize,
	}, ui.TextShape{Width: max(cr.W-2*tPadX, 1), Align: tableCellTextAlign(b.Table, rIdx, cIdx)})
	_, textH := layout.Size()
	textY := cr.Y + max((cr.H-max(tLineH, textH))*0.5, tPadY)
	cp := layout.PositionAt(ui.Point{X: wx - cr.X - tPadX, Y: wy - textY})
	return clampInt(base+utf16OffsetToRuneOffset(cellPlain, cp.Index), 0, totalLen)
}

func (s *Session) cachedShapeBlock(
	id model.BlockID,
	plain string,
	spans []model.Span,
	idx int,
	fontFamily string,
	fontSize float32,
	weight int,
	lineHeight float32,
	width float32,
	rich bool,
) *ui.TextLayout {
	if s.textLayoutCache == nil {
		s.textLayoutCache = make(map[model.BlockID]cachedBlockLayout)
	}
	width = max(width, 1)
	epoch := theme.FontEpoch()
	if entry, ok := s.textLayoutCache[id]; ok &&
		entry.text == plain &&
		entry.rich == rich &&
		slices.Equal(entry.spans, spans) &&
		entry.fontFamily == fontFamily &&
		entry.fontSize == fontSize &&
		entry.weight == weight &&
		entry.lineHeight == lineHeight &&
		entry.width == width &&
		entry.fontEpoch == epoch &&
		entry.layout != nil {
		return entry.layout
	}
	font := ui.Font{
		Family:     fontFamily,
		Size:       fontSize,
		Weight:     weight,
		LineHeight: lineHeight,
	}
	var layout *ui.TextLayout
	if rich && len(spans) > 0 {
		uiSpans := make([]ui.Span, 0, len(spans))
		codeFG, codeBG, codePadX, codePadY, codeRadius := inlineCodeStyle(fontSize)
		for _, sp := range spans {
			u, _ := buildPureSpan(sp, fontSize, weight, codeFG, codeBG, codePadX, codePadY, codeRadius)
			uiSpans = append(uiSpans, u)
		}
		layout = ui.ShapeRichText(uiSpans, font, width)
	} else {
		layout = ui.ShapeText(plain, font, width)
	}
	s.textLayoutCache[id] = cachedBlockLayout{
		text:       plain,
		spans:      slices.Clone(spans),
		rich:       rich,
		fontFamily: fontFamily,
		fontSize:   fontSize,
		weight:     weight,
		lineHeight: lineHeight,
		width:      width,
		fontEpoch:  epoch,
		layout:     layout,
	}
	return layout
}

type cachedCellLayout struct {
	text       string
	spans      []ui.Span
	fontFamily string
	fontSize   float32
	weight     int
	lineHeight float32
	align      ui.Align
	width      float32
	fontEpoch  uint64
	layout     *ui.TextLayout
}

func (s *Session) cachedShapeTableCell(
	key tableCellKey,
	plain string,
	spans []ui.Span,
	fontFamily string,
	fontSize float32,
	weight int,
	lineHeight float32,
	align ui.Align,
	width float32,
) *ui.TextLayout {
	if s.tableCellLayouts == nil {
		s.tableCellLayouts = make(map[tableCellKey]cachedCellLayout)
	}
	width = max(width, 1)
	epoch := theme.FontEpoch()
	if entry, ok := s.tableCellLayouts[key]; ok &&
		entry.text == plain &&
		entry.fontFamily == fontFamily &&
		entry.fontSize == fontSize &&
		entry.weight == weight &&
		entry.lineHeight == lineHeight &&
		entry.align == align &&
		entry.width == width &&
		entry.fontEpoch == epoch &&
		slices.Equal(entry.spans, spans) &&
		entry.layout != nil {
		return entry.layout
	}
	lay := ui.ShapeRichTextWith(spans, ui.Font{
		Family:     fontFamily,
		Size:       fontSize,
		Weight:     weight,
		LineHeight: lineHeight,
	}, ui.TextShape{Width: width, Align: align})
	if len(s.tableCellLayouts) > 4096 {
		clear(s.tableCellLayouts)
	}
	s.tableCellLayouts[key] = cachedCellLayout{
		text:       plain,
		spans:      slices.Clone(spans),
		fontFamily: fontFamily,
		fontSize:   fontSize,
		weight:     weight,
		lineHeight: lineHeight,
		align:      align,
		width:      width,
		fontEpoch:  epoch,
		layout:     lay,
	}
	return lay
}

func utf8RuneToUTF16(text string, runeOff int) int {
	if runeOff <= 0 || text == "" {
		return 0
	}
	units := 0
	idx := 0
	for _, r := range text {
		if idx >= runeOff {
			return units
		}
		units += utf16.RuneLen(r)
		idx++
	}
	return units
}

func utf16OffsetToRuneOffset(text string, utf16Off int) int {
	if utf16Off <= 0 || text == "" {
		return 0
	}
	units := 0
	runes := 0
	for _, r := range text {
		if units >= utf16Off {
			return runes
		}
		units += utf16.RuneLen(r)
		runes++
	}
	return runes
}

func (s *Session) wordRangeAt(idx, off int) (int, int) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return 0, 0
	}
	runes := []rune(s.doc.Blocks[idx].PlainText())
	if len(runes) == 0 {
		return 0, 0
	}
	at := clampInt(off, 0, len(runes)-1)
	cat := wordRuneCategory(runes[at])
	lo, hi := at, at+1
	for lo > 0 && wordRuneCategory(runes[lo-1]) == cat {
		lo--
	}
	for hi < len(runes) && wordRuneCategory(runes[hi]) == cat {
		hi++
	}
	return lo, hi
}

func wordRuneCategory(r rune) uint8 {
	switch {
	case unicode.IsSpace(r):
		return 0
	case r >= 0x4e00 && r <= 0x9fff:
		return 1
	case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
		return 2
	default:
		return 3
	}
}

func (s *Session) extendBlockUnitSelection(targetBlock int) {
	u0, u1 := s.mouseUnitRange[0], s.mouseUnitRange[1]
	targetBlock = clampInt(targetBlock, 0, len(s.doc.Blocks)-1)
	if targetBlock < u0.Block {
		s.selAnchor = u1
		s.selHead = Position{Block: targetBlock, Offset: 0}
	} else {
		s.selAnchor = u0
		s.selHead = Position{Block: targetBlock, Offset: s.blockSelectionLen(targetBlock)}
	}
}

func (s *Session) extendWordUnitSelection(head Position) {
	u0, u1 := s.mouseUnitRange[0], s.mouseUnitRange[1]
	w0, w1 := s.wordRangeAt(head.Block, head.Offset)
	if head.Block < u0.Block || (head.Block == u0.Block && head.Offset < u0.Offset) {
		s.selAnchor = u1
		s.selHead = Position{Block: head.Block, Offset: w0}
	} else {
		s.selAnchor = u0
		end := Position{Block: head.Block, Offset: w1}
		if end.Block == u1.Block && end.Offset < u1.Offset {
			end = u1
		}
		s.selHead = end
	}
}

func (s *Session) codeSyntaxSpansWithSelection(idx int, src, lang string) []ui.Span {
	spans := codeSyntaxSpans(src, lang)
	if len(spans) == 0 {
		return nil
	}
	selLo, selHi, _, selOk := s.blockSelectionSpanRange(idx)
	if !selOk || selHi <= selLo {
		return spans
	}
	pal := theme.Ocean()
	var out []ui.Span
	pos := 0
	for _, sp := range spans {
		runes := []rune(sp.Text)
		s0, s1 := pos, pos+len(runes)
		pos = s1
		if s1 <= selLo || s0 >= selHi {
			out = append(out, sp)
			continue
		}
		if s0 < selLo {
			pre := sp
			pre.Text = string(runes[:selLo-s0])
			out = append(out, pre)
		}
		midLo := max(selLo-s0, 0)
		midHi := min(selHi-s0, len(runes))
		if midHi > midLo {
			mid := sp
			mid.Text = string(runes[midLo:midHi])
			mid.Background = pal.EditorSelectionBG()
			out = append(out, mid)
		}
		if s1 > selHi {
			suf := sp
			suf.Text = string(runes[selHi-s0:])
			out = append(out, suf)
		}
	}
	return slices.Clip(out)
}

func (s *Session) pasteOverMultiSelection(norm string) bool {
	if s.hasMultiCellTableSelection() || (s.hasWholeAtomicSelection() && s.isTablePasteTarget()) {
		a, b := s.orderedSelection()
		idx := a.Block
		if idx < 0 || idx >= len(s.doc.Blocks) || s.doc.Blocks[idx].Table == nil {
			return false
		}
		if strings.ContainsAny(norm, "\t\n") {
			blk := &s.doc.Blocks[idx]
			startR, startC := 0, 0
			if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, a.Offset); ok {
				startR, startC = row, col
			}
			if !s.canFitTablePaste(blk.Table, startR, startC, norm, false, false) {
				return true
			}
			s.pushUndo()
			clearTableRange(blk.Table, a.Offset, b.Offset, "")
			s.focusedBlock = idx
			s.tableActiveBlock = blk.ID
			s.tableActiveRow = startR
			s.tableActiveCol = startC
			s.selAnchor = Position{Block: idx, Offset: a.Offset}
			s.selHead = s.selAnchor
			s.pasteIntoTableBlock(idx, norm)
			return true
		}
		if !s.hasWholeAtomicSelection() {
			return s.ReplaceSelectionWithText(norm)
		}
	}
	if !s.hasMultiBlockSelection() && !s.hasWholeAtomicSelection() {
		return false
	}
	if !s.isMarkdownNote() || (!strings.Contains(norm, "\n") && !looksLikeMarkdownBlock(norm) && !looksLikeInlineMarkdown(norm)) {
		return s.ReplaceSelectionWithText(norm)
	}
	if s.ReplaceAIMarkdown(norm) {
		s.multiSelFocusPending = false
		return true
	}
	return s.ReplaceSelectionWithText(norm)
}
