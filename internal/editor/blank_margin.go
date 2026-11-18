package editor

import (
	"maps"
	"strings"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// blankClickSlopPx matches TRAILING_BLANK_CLICK_SLOP_PX from the original Thinkly editor.
const blankClickSlopPx float32 = 2

func isPlainEmptyParagraph(b *model.Block) bool {
	if b == nil || b.Kind != model.BlockParagraph {
		return false
	}
	if b.Indent != 0 || b.QuoteDepth != 0 || b.Callout != "" || b.Table != nil {
		return false
	}
	if b.PlainText() != "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(b.CleanSource)), "<p") {
		return false
	}
	return true
}

// insertParagraphAt splices a new plain empty paragraph at idx, keeps any folded
// section above a bottom-appended tail shut via foldTails, and focuses the new block.
func (s *Session) insertParagraphAt(idx int) (int, bool) {
	if s.readOnly || s.doc == nil || idx < 0 || idx > len(s.doc.Blocks) {
		return -1, false
	}
	s.pushUndo()
	nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
	s.spliceBlocks(idx, idx, []model.Block{nb})
	newIdx := s.blockIndexByID(nb.ID)
	if newIdx < 0 {
		newIdx = clampInt(idx, 0, len(s.doc.Blocks)-1)
	}
	s.tableActiveBlock = 0
	s.closeTransientOverlays()
	s.trailingPlus = false
	if s.findOpen {
		s.recomputeFind(false)
	}
	s.markDirty()

	// Appending at the end of a folded section reveals the new block as a visible tail
	// without opening the folded heading or focusing hidden blocks inside it.
	if !s.showHiddenTail(nb.ID) {
		return newIdx, true
	}
	newIdx = s.blockIndexByID(nb.ID)
	if newIdx < 0 {
		newIdx = clampInt(idx, 0, len(s.doc.Blocks)-1)
	}
	s.FocusBlock(newIdx)
	return newIdx, true
}

// insertTopParagraph focuses block 0 if it is already a plain empty paragraph,
// otherwise inserts a new plain empty paragraph at index 0 and reveals the top.
func (s *Session) insertTopParagraph() bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	if isPlainEmptyParagraph(&s.doc.Blocks[0]) {
		s.tableActiveBlock = 0
		s.closeTransientOverlays()
		s.trailingPlus = false
		s.FocusBlock(0)
		s.bodyScroll.Y = 0
		return true
	}
	_, ok := s.insertParagraphAt(0)
	if ok {
		s.bodyScroll.Y = 0
	}
	return ok
}

// insertBottomParagraph appends a new plain empty paragraph at the end of the document
// and focuses it (as a visible tail if the final section is folded).
func (s *Session) insertBottomParagraph() (int, bool) {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return -1, false
	}
	return s.insertParagraphAt(len(s.doc.Blocks))
}

// insertOrFocusBottomParagraph focuses the last visible block when it is already a plain
// empty paragraph, otherwise appends a new plain empty paragraph at the end of the document.
func (s *Session) insertOrFocusBottomParagraph() bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	_, lastVis := s.firstAndLastVisibleBlocks()
	if lastVis < 0 {
		return false
	}
	if isPlainEmptyParagraph(&s.doc.Blocks[lastVis]) {
		s.tableActiveBlock = 0
		s.closeTransientOverlays()
		s.trailingPlus = false
		s.FocusBlock(lastVis)
		return true
	}
	_, ok := s.insertBottomParagraph()
	return ok
}

func (s *Session) inEditorPaper(wx, wy float32) bool {
	r := s.scrollRect
	if r.W <= 0 || r.H <= 0 {
		r = s.editorRect
	}
	if r.W <= 0 || r.H <= 0 {
		return true
	}
	gutter := float32(0)
	if s.bodyScroll.MaxY > 0 {
		gutter = 10
	}
	right := max(r.X+r.W-gutter, r.X)
	return wx >= r.X && wx <= right && wy >= r.Y && wy <= r.Y+r.H
}

func (s *Session) isAboveFirstVisibleBlock(wy float32) bool {
	firstVis, _ := s.firstAndLastVisibleBlocks()
	if firstVis < 0 {
		return false
	}
	firstR := s.blockRects[s.doc.Blocks[firstVis].ID]
	if firstR.H > 0 {
		return wy < firstR.Y
	}
	if s.scrollRect.H > 0 {
		topPad := editorTopPad
		if s.minimal {
			topPad = minimalTopPad
		}
		return wy < s.scrollRect.Y+topPad-s.bodyScroll.Y
	}
	return false
}

func (s *Session) isBelowLastVisibleBlock(wy float32) bool {
	_, lastVis := s.firstAndLastVisibleBlocks()
	if lastVis < 0 {
		return false
	}
	lastR := s.blockRects[s.doc.Blocks[lastVis].ID]
	if lastR.H > 0 {
		return wy > lastR.Y+lastR.H+blankClickSlopPx
	}
	return false
}

// tryBlankMarginInsert handles clicks in the top (64px) or bottom (>=160px) blank paper
// margins (including left/right margins above the first block or below the last block),
// matching thinkly-fast's Editor::try_blank_margin_insert.
func (s *Session) tryBlankMarginInsert(wx, wy float32) bool {
	if s.readOnly || !s.isMarkdownNote() || s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	if s.inlineAI != nil || s.activePlan != nil {
		return false
	}
	if !s.inEditorPaper(wx, wy) {
		return false
	}
	if s.isAboveFirstVisibleBlock(wy) {
		return s.insertTopParagraph()
	}
	if s.isBelowLastVisibleBlock(wy) {
		return s.insertOrFocusBottomParagraph()
	}
	return false
}

// --- Folded-section visible tail support (matching thinkly-fast fold.rs) ---

func (s *Session) foldSectionEnd(headingIdx int) int {
	if s.doc == nil || headingIdx < 0 || headingIdx >= len(s.doc.Blocks) {
		return 0
	}
	head := &s.doc.Blocks[headingIdx]
	if !head.Kind.IsHeading() {
		return min(headingIdx+1, len(s.doc.Blocks))
	}
	lvl := head.Kind.HeadingLevel()
	for i := headingIdx + 1; i < len(s.doc.Blocks); i++ {
		if s.doc.Blocks[i].Kind.IsHeading() && s.doc.Blocks[i].Kind.HeadingLevel() <= lvl {
			return i
		}
	}
	return len(s.doc.Blocks)
}

func (s *Session) tailReveals(headingIdx, idx int) bool {
	if len(s.foldTails) == 0 || s.doc == nil || headingIdx < 0 || headingIdx >= len(s.doc.Blocks) {
		return false
	}
	tailID, ok := s.foldTails[s.doc.Blocks[headingIdx].ID]
	if !ok || tailID == 0 {
		return false
	}
	startIdx := s.blockIndexByID(tailID)
	if startIdx <= headingIdx {
		return false
	}
	end := s.foldSectionEnd(headingIdx)
	return startIdx < end && idx >= startIdx && idx < end
}

func (s *Session) noteFoldTail(headingID, blockID model.BlockID) {
	if headingID == 0 || blockID == 0 {
		return
	}
	if s.foldTails == nil {
		s.foldTails = make(map[model.BlockID]model.BlockID)
	}
	if curID, ok := s.foldTails[headingID]; ok {
		curIdx := s.blockIndexByID(curID)
		blkIdx := s.blockIndexByID(blockID)
		if curIdx >= 0 && blkIdx >= curIdx {
			return
		}
	}
	s.foldTails[headingID] = blockID
}

// showHiddenTail reveals blockID as the visible tail of every folded heading that hides it,
// provided no hidden blocks follow it in that section. Never opens the fold itself.
func (s *Session) showHiddenTail(blockID model.BlockID) bool {
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return false
	}
	if !s.isBlockHiddenByFold(idx) {
		return true
	}
	for hIdx := 0; hIdx < idx; hIdx++ {
		head := &s.doc.Blocks[hIdx]
		if !head.Kind.IsHeading() || !s.folded[head.ID] {
			continue
		}
		end := s.foldSectionEnd(hIdx)
		if idx >= end || s.tailReveals(hIdx, idx) {
			continue
		}
		hasHiddenAfter := false
		for i := idx + 1; i < end; i++ {
			if s.isBlockHiddenByFold(i) {
				hasHiddenAfter = true
				break
			}
		}
		if hasHiddenAfter {
			continue
		}
		s.noteFoldTail(head.ID, blockID)
	}
	idx = s.blockIndexByID(blockID)
	return idx >= 0 && !s.isBlockHiddenByFold(idx)
}

// enterOnFoldedHeading appends a visible paragraph after a folded heading's hidden section
// while keeping the fold shut (matching thinkly-fast Editor::enter_on_folded_heading).
func (s *Session) enterOnFoldedHeading(headingIdx int) bool {
	if s.minimal || s.readOnly || !s.isMarkdownNote() || s.doc == nil || headingIdx < 0 || headingIdx >= len(s.doc.Blocks) {
		return false
	}
	head := &s.doc.Blocks[headingIdx]
	if !head.Kind.IsHeading() || !s.folded[head.ID] {
		return false
	}
	headID := head.ID
	end := s.foldSectionEnd(headingIdx)
	s.pushUndo()
	nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
	s.spliceBlocks(end, end, []model.Block{nb})
	s.noteFoldTail(headID, nb.ID)
	s.showHiddenTail(nb.ID)
	newIdx := s.blockIndexByID(nb.ID)
	if newIdx < 0 {
		newIdx = min(end, len(s.doc.Blocks)-1)
	}
	s.FocusBlock(newIdx)
	if s.findOpen {
		s.recomputeFind(false)
	}
	s.markDirty()
	return true
}

// handleFoldGapBackspace prevents Backspace at offset 0 of a visible tail block from merging
// into hidden blocks tucked inside the folded section above it.
func (s *Session) handleFoldGapBackspace(idx int, b *model.Block) bool {
	if s.minimal || len(s.folded) == 0 || idx <= 0 {
		return false
	}
	prevVis := s.stepVisibleBlock(idx, -1)
	if prevVis < 0 || prevVis == idx || prevVis+1 >= idx {
		return false
	}
	hasHiddenBetween := false
	for i := prevVis + 1; i < idx; i++ {
		if s.isBlockHiddenByFold(i) {
			hasHiddenBetween = true
			break
		}
	}
	if !hasHiddenBetween || b.Kind.IsHeading() {
		return false
	}
	removable := b.PlainText() == "" &&
		b.Kind != model.BlockTable &&
		b.Kind != model.BlockImage &&
		b.Kind != model.BlockDivider &&
		b.Kind != model.BlockCode &&
		b.Kind != model.BlockRawHTML &&
		!strings.HasPrefix(strings.ToLower(strings.TrimSpace(b.CleanSource)), "<p")
	endOff := len([]rune(s.doc.Blocks[prevVis].PlainText()))
	if s.doc.Blocks[prevVis].Kind == model.BlockDivider || s.doc.Blocks[prevVis].Kind == model.BlockImage {
		endOff = 0
	}
	if removable {
		s.pushUndo()
		s.spliceBlocks(idx, idx+1, nil)
		s.markDirty()
	}
	s.focusedBlock = prevVis
	s.selAnchor = Position{Block: prevVis, Offset: endOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	return true
}

func (s *Session) syncFoldTailsBeforeSplice(start, end int, insert []model.Block) func() {
	if len(s.foldTails) == 0 || s.doc == nil {
		return nil
	}
	type pendingTail struct {
		headID  model.BlockID
		tailID  model.BlockID
		tailIdx int
	}
	var items []pendingTail
	for hID, tID := range s.foldTails {
		items = append(items, pendingTail{
			headID:  hID,
			tailID:  tID,
			tailIdx: s.blockIndexByID(tID),
		})
	}
	return func() {
		for _, it := range items {
			hIdx := s.blockIndexByID(it.headID)
			if hIdx < 0 || !s.doc.Blocks[hIdx].Kind.IsHeading() || !s.folded[it.headID] || it.tailIdx < 0 {
				delete(s.foldTails, it.headID)
				continue
			}
			switch {
			case start == it.tailIdx && end == it.tailIdx && len(insert) > 0:
				s.foldTails[it.headID] = insert[0].ID
			case start <= it.tailIdx && end > it.tailIdx:
				if len(insert) > 0 {
					s.foldTails[it.headID] = insert[0].ID
				} else {
					secEnd := s.foldSectionEnd(hIdx)
					if start > hIdx && start < secEnd && start < len(s.doc.Blocks) {
						s.foldTails[it.headID] = s.doc.Blocks[start].ID
					} else {
						delete(s.foldTails, it.headID)
					}
				}
			}
		}
	}
}

func cloneFoldTails(src map[model.BlockID]model.BlockID) map[model.BlockID]model.BlockID {
	if len(src) == 0 {
		return nil
	}
	return maps.Clone(src)
}

// --- Trailing "+" button below the final block (matching thinkly-fast trailing_add.rs) ---

type trailingAddSlot struct {
	btnRect ui.Rect
	lineTop float32
	lineH   float32
	rowLeft float32
}

func (s *Session) computeTrailingAddSlot() (trailingAddSlot, bool) {
	if s.minimal || s.readOnly || !s.isMarkdownNote() || s.doc == nil || len(s.doc.Blocks) == 0 {
		return trailingAddSlot{}, false
	}
	if s.hasAnyOverlayOpen() || s.hoveredAnnotID != "" || s.imgResizeBlock != 0 || s.dragBlockID != 0 || s.isTableExtending() || s.isMouseDragging() {
		return trailingAddSlot{}, false
	}
	_, lastVis := s.firstAndLastVisibleBlocks()
	if lastVis < 0 {
		return trailingAddSlot{}, false
	}
	lastBlock := &s.doc.Blocks[lastVis]
	if isPlainEmptyParagraph(lastBlock) {
		return trailingAddSlot{}, false
	}
	lastRow, ok := s.blockRects[lastBlock.ID]
	if !ok || lastRow.H <= 0 || s.scrollRect.H <= 0 {
		return trailingAddSlot{}, false
	}
	gap := s.blockGap(lastBlock, &model.Block{Kind: model.BlockParagraph})
	lineTop := lastRow.Y + lastRow.H + gap
	fontSize, _, lh := s.textMetrics(model.BlockParagraph)
	lineH := fontSize * lh
	btnSize := s.scale(24)
	top := lineTop + max(lineH-btnSize, 0)*0.5
	if top < s.scrollRect.Y || top+btnSize > s.scrollRect.Y+s.scrollRect.H {
		return trailingAddSlot{}, false
	}
	left := lastRow.X + s.handleWidth() - btnSize
	return trailingAddSlot{
		btnRect: ui.Rect{X: left, Y: top, W: btnSize, H: btnSize},
		lineTop: lineTop,
		lineH:   lineH,
		rowLeft: lastRow.X,
	}, true
}

func (s *Session) updateTrailingAddHover(c *ui.Context, root ui.Element, nextHover int, readOnly bool) int {
	clearTrailing := func() {
		if s.trailingPlus {
			s.trailingPlus = false
			c.Invalidate()
		}
	}
	if readOnly || s.minimal || !s.isMarkdownNote() || s.doc == nil || len(s.doc.Blocks) == 0 {
		clearTrailing()
		return nextHover
	}
	if s.hasAnyOverlayOpen() || s.hoveredAnnotID != "" || s.imgResizeBlock != 0 || s.dragBlockID != 0 || s.isTableExtending() || s.isMouseDragging() {
		clearTrailing()
		return nextHover
	}
	rx, ry, inside := root.PointerPosition()
	if !inside || s.scrollRect.H <= 0 {
		clearTrailing()
		return nextHover
	}
	wx, wy := s.editorRect.X+rx, s.editorRect.Y+ry
	_, lastVis := s.firstAndLastVisibleBlocks()
	if lastVis < 0 {
		clearTrailing()
		return nextHover
	}
	lastBlock := &s.doc.Blocks[lastVis]
	lastRow, ok := s.blockRects[lastBlock.ID]
	if !ok || lastRow.H <= 0 {
		clearTrailing()
		return nextHover
	}
	lastBottom := lastRow.Y + lastRow.H
	inBlankBelow := wy >= lastBottom &&
		wy <= s.scrollRect.Y+s.scrollRect.H &&
		wx >= lastRow.X-16 &&
		wx <= lastRow.X+lastRow.W

	handleW := s.handleWidth()
	btnSz := s.scale(24)
	if isPlainEmptyParagraph(lastBlock) {
		clearTrailing()
		btnLeft := lastRow.X + handleW - btnSz
		inFinalCorridor := s.hoveredBlock == lastVis &&
			wx >= btnLeft-8 && wx <= lastRow.X+handleW+32 &&
			wy >= lastRow.Y-6 && wy <= lastBottom+8
		if inBlankBelow || inFinalCorridor {
			return lastVis
		}
		return nextHover
	}

	slot, hasSlot := s.computeTrailingAddSlot()
	if !hasSlot {
		clearTrailing()
		return nextHover
	}
	inCorridor := wx >= slot.btnRect.X-8 &&
		wx <= slot.rowLeft+handleW+32 &&
		wy >= min(slot.btnRect.Y, lastBottom)-6 &&
		wy <= max(slot.btnRect.Y+slot.btnRect.H, lastBottom)+8
	if inBlankBelow || (s.trailingPlus && inCorridor) {
		if !s.trailingPlus {
			s.trailingPlus = true
			c.Invalidate()
		}
		return -1
	}
	clearTrailing()
	return nextHover
}

func (s *Session) renderTrailingAddOverlay(c *ui.Context, readOnly bool) {
	if readOnly || !s.trailingPlus {
		return
	}
	slot, ok := s.computeTrailingAddSlot()
	if !ok {
		return
	}
	left := slot.btnRect.X - s.editorRect.X
	top := slot.btnRect.Y - s.editorRect.Y
	wrap := ui.Box(c.Key("trailing-add-wrap")).Absolute().Left(left).Top(top).
		Transition(appUi.GutterHandleTransition())
	wrap.Children(func() {
		tip := appUi.TooltipSpec{
			Placement: appUi.TooltipAbove,
			Rows: []appUi.TooltipRow{{
				Detail: theme.Tr("添加块", "Add block"),
			}},
		}
		addBtn := appUi.IconButton(c, icons.Plus, appUi.IconButtonOptions{
			ID:          "trailing-add",
			Label:       theme.Tr("添加块", "Add block"),
			TooltipSpec: &tip,
			Size:        s.scale(24),
			IconSize:    s.scale(theme.IconChrome),
		})
		addBtn.KeepFocus()
		s.recordOverlayBtn("trailing-add", addBtn)
		if addBtn.Clicked() {
			s.pressJustBegan = false
			s.trailingPlus = false
			if newIdx, inserted := s.insertBottomParagraph(); inserted {
				trigger := ui.Rect{
					X: slot.rowLeft + s.leftContentGutter(),
					Y: slot.lineTop,
					W: 224,
					H: slot.lineH,
				}
				s.openSlashMenu(newIdx, trigger)
				s.scheduleRebuild(c)
			}
		}
	})
}
