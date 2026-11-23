package editor

import (
	"fmt"
	"html"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/model"
	appUi "block/internal/ui"
)

type autoConvertRecord struct {
	blockID       model.BlockID // block ID to match on Backspace (for divider, the trailing empty paragraph ID)
	targetIdx     int           // starting block index in s.doc.Blocks to replace when restoring
	replaceCount  int           // number of blocks to replace on restore (1 normally, 2 for divider + paragraph)
	isCell        bool
	cellKey       tableCellKey
	docRev        uint64
	beforeBlk     model.Block
	beforeCell    []model.Span
	beforePos     int
	afterPos      int
	afterText     string
	isMathConvert bool // true when `$...$` just converted to MarkMath (rolls back if immediately followed by ASCII digit)
}

type nextInsertStyleGuard struct {
	blockID   model.BlockID
	isCell    bool
	cellKey   tableCellKey
	docRev    uint64
	caretPos  int
	clearMask model.Mark
	clearBG   bool
}

type rightStyleBias struct {
	blockID model.BlockID
	isCell  bool
	cellKey tableCellKey
	pos     int
}

func cloneSpansSlice(src []model.Span) []model.Span {
	if len(src) == 0 {
		return nil
	}
	out := make([]model.Span, len(src))
	copy(out, src)
	return out
}

func (s *Session) clearAutoConvertState() {
	s.lastAutoConvert = nil
	s.nextInsertGuard = nil
	s.rightStyleBias = nil
}

func (s *Session) recordBlockAutoConvert(idx int, blkBefore model.Block, beforePos int, blkAfter *model.Block, afterPos int, isMath bool) {
	if blkAfter == nil {
		return
	}
	s.lastAutoConvert = &autoConvertRecord{
		blockID:       blkAfter.ID,
		targetIdx:     idx,
		replaceCount:  1,
		docRev:        s.docRev,
		beforeBlk:     blkBefore.Clone(),
		beforePos:     beforePos,
		afterPos:      afterPos,
		afterText:     blkAfter.PlainText(),
		isMathConvert: isMath,
	}
}

func (s *Session) recordMultiBlockAutoConvert(idx, replaceCount int, blkBefore model.Block, beforePos int, afterFocusBlk *model.Block, afterPos int) {
	if afterFocusBlk == nil {
		return
	}
	s.lastAutoConvert = &autoConvertRecord{
		blockID:      afterFocusBlk.ID,
		targetIdx:    idx,
		replaceCount: max(replaceCount, 1),
		docRev:       s.docRev,
		beforeBlk:    blkBefore.Clone(),
		beforePos:    beforePos,
		afterPos:     afterPos,
		afterText:    afterFocusBlk.PlainText(),
	}
}

func (s *Session) recordCellAutoConvert(key tableCellKey, beforeSpans []model.Span, beforePos int, afterSpans []model.Span, afterPos int, isMath bool) {
	s.lastAutoConvert = &autoConvertRecord{
		blockID:       key.BlockID,
		isCell:        true,
		cellKey:       key,
		docRev:        s.docRev,
		beforeCell:    cloneSpansSlice(beforeSpans),
		beforePos:     beforePos,
		afterPos:      afterPos,
		afterText:     model.PlainTextOfSpans(afterSpans),
		isMathConvert: isMath,
	}
}

func (s *Session) restoreBlockAutoConvert(rec *autoConvertRecord) bool {
	if rec == nil || rec.isCell || s.doc == nil {
		return false
	}
	startIdx := rec.targetIdx
	repCount := max(rec.replaceCount, 1)
	if startIdx < 0 || startIdx+repCount > len(s.doc.Blocks) {
		return false
	}
	s.pushUndo()
	restored := rec.beforeBlk.Clone()
	restored.Dirty = true
	restored.CleanSource = ""
	for i := startIdx; i < startIdx+repCount; i++ {
		oldID := s.doc.Blocks[i].ID
		delete(s.richInputs, oldID)
		for k := range s.tableInputs {
			if k.BlockID == oldID {
				delete(s.tableInputs, k)
			}
		}
	}
	s.spliceBlocks(startIdx, startIdx+repCount, []model.Block{restored})
	s.clearAutoConvertState()
	s.focusedBlock = startIdx
	s.selAnchor = Position{Block: startIdx, Offset: rec.beforePos}
	s.selHead = s.selAnchor
	s.multiSelFocusPending = false
	s.selSyncPending = true
	s.focusPending = true
	if st, ok := s.richInputs[restored.ID]; ok && st != nil {
		st.Sync(restored.PlainText(), rec.beforePos, rec.beforePos)
	}
	s.markDirty()
	return true
}

func (s *Session) tryUndoAutoConvertBackspace(idx int, curText string, caret int) bool {
	rec := s.lastAutoConvert
	if rec == nil || rec.isCell || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[idx]
	if b.ID != rec.blockID || rec.docRev != s.docRev || caret != rec.afterPos || curText != rec.afterText {
		return false
	}
	return s.restoreBlockAutoConvert(rec)
}

func (s *Session) tryUndoAtomicAutoConvertBackspace() bool {
	rec := s.lastAutoConvert
	if rec == nil || rec.isCell || s.doc == nil || s.focusedBlock < 0 || s.focusedBlock >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[s.focusedBlock]
	if b.ID != rec.blockID || rec.docRev != s.docRev {
		return false
	}
	return s.restoreBlockAutoConvert(rec)
}

func (s *Session) tryUndoCodeAutoConvertBackspace(idx int, curText string) bool {
	rec := s.lastAutoConvert
	if rec == nil || rec.isCell || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[idx]
	if b.Kind != model.BlockCode || b.ID != rec.blockID || rec.docRev != s.docRev || curText != rec.afterText {
		return false
	}
	return s.restoreBlockAutoConvert(rec)
}

func (s *Session) tryUndoCellAutoConvertBackspace(blockIdx, rIdx, cIdx int, curText string, caret int) bool {
	rec := s.lastAutoConvert
	if rec == nil || s.doc == nil || blockIdx < 0 || blockIdx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[blockIdx]
	if b.Kind != model.BlockTable || b.Table == nil || rIdx < 0 || rIdx >= len(b.Table.Rows) || cIdx < 0 || cIdx >= len(b.Table.Rows[rIdx]) {
		return false
	}
	// Block-level table auto-convert (`| A | B |` + Enter -> BlockTable): immediate Backspace
	// from the initial empty body cell (1, 0) restores the original paragraph syntax.
	if !rec.isCell {
		if b.ID == rec.blockID && rec.docRev == s.docRev && rIdx == 1 && cIdx == 0 && caret == 0 && curText == "" {
			s.tableActiveBlock = 0
			s.tableActiveRow = 0
			s.tableActiveCol = 0
			return s.restoreBlockAutoConvert(rec)
		}
		return false
	}
	key := tableCellKey{BlockID: b.ID, Row: rIdx, Col: cIdx}
	if rec.cellKey != key || rec.docRev != s.docRev || caret != rec.afterPos || curText != rec.afterText {
		return false
	}
	s.pushUndo()
	cell := &b.Table.Rows[rIdx][cIdx]
	cell.Spans = cloneSpansSlice(rec.beforeCell)
	b.Dirty = true
	b.CleanSource = ""
	s.clearAutoConvertState()
	base := tableCellStartOffset(b.Table, rIdx, cIdx)
	s.selAnchor = Position{Block: blockIdx, Offset: base + rec.beforePos}
	s.selHead = s.selAnchor
	s.selSyncPending = true
	if st, ok := s.tableInputs[key]; ok && st != nil {
		st.Sync(cell.PlainText(), rec.beforePos, rec.beforePos)
	}
	s.markDirty()
	return true
}

// tryRollbackCurrencyMath rolls back an immediately preceding `$...$` -> MarkMath conversion
// when the very next keystroke is an ASCII digit (e.g. typing `$5&$10` character by character),
// keeping live typing 100% consistent with Pandoc/Markdown currency boundaries.
func (s *Session) tryRollbackCurrencyMathOnBlock(idx int, oldText, newText string, caretHint int) (int, bool) {
	rec := s.lastAutoConvert
	if rec == nil || rec.isCell || !rec.isMathConvert || rec.docRev != s.docRev || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return 0, false
	}
	b := &s.doc.Blocks[idx]
	if b.ID != rec.blockID || oldText != rec.afterText {
		return 0, false
	}
	insStart, insLen := computeEditInsertedRange(oldText, newText, caretHint)
	if insLen <= 0 || insStart != rec.afterPos {
		return 0, false
	}
	newR := []rune(newText)
	if insStart >= len(newR) || newR[insStart] < '0' || newR[insStart] > '9' {
		return 0, false
	}
	inserted := string(newR[insStart : insStart+insLen])
	beforeR := []rune(rec.beforeBlk.PlainText())
	pos := clampInt(rec.beforePos, 0, len(beforeR))
	restoredText := string(beforeR[:pos]) + inserted + string(beforeR[pos:])
	b.Spans = spliceSpansByTextEdit(cloneSpansSlice(rec.beforeBlk.Spans), rec.beforeBlk.PlainText(), restoredText)
	b.Dirty = true
	b.CleanSource = ""
	s.clearAutoConvertState()
	return pos + insLen, true
}

func (s *Session) tryRollbackCurrencyMathOnCell(blockIdx, rIdx, cIdx int, key tableCellKey, oldText, newText string, caretHint int) (int, bool) {
	rec := s.lastAutoConvert
	if rec == nil || !rec.isCell || !rec.isMathConvert || rec.docRev != s.docRev || rec.cellKey != key || oldText != rec.afterText {
		return 0, false
	}
	b := &s.doc.Blocks[blockIdx]
	cell := &b.Table.Rows[rIdx][cIdx]
	insStart, insLen := computeEditInsertedRange(oldText, newText, caretHint)
	if insLen <= 0 || insStart != rec.afterPos {
		return 0, false
	}
	newR := []rune(newText)
	if insStart >= len(newR) || newR[insStart] < '0' || newR[insStart] > '9' {
		return 0, false
	}
	inserted := string(newR[insStart : insStart+insLen])
	beforePlain := model.PlainTextOfSpans(rec.beforeCell)
	beforeR := []rune(beforePlain)
	pos := clampInt(rec.beforePos, 0, len(beforeR))
	restoredText := string(beforeR[:pos]) + inserted + string(beforeR[pos:])
	cell.Spans = spliceSpansByTextEdit(cloneSpansSlice(rec.beforeCell), beforePlain, restoredText)
	b.Dirty = true
	b.CleanSource = ""
	s.clearAutoConvertState()
	return pos + insLen, true
}

// computeEditInsertedRange computes the actual inserted rune range [insStart, insStart+insLen)
// from oldText -> newText, even when replacing a longer selection with a shorter string.
func computeEditInsertedRange(oldText, newText string, caretHint int) (insStart, insLen int) {
	oldR := []rune(oldText)
	newR := []rune(newText)
	maxPref := min(len(oldR), len(newR))
	if caretHint >= 0 && caretHint <= len(newR) && caretHint < maxPref {
		maxPref = caretHint
	}
	pref := 0
	for pref < maxPref && oldR[pref] == newR[pref] {
		pref++
	}
	suf := 0
	for suf < len(oldR)-pref && suf < len(newR)-pref && oldR[len(oldR)-1-suf] == newR[len(newR)-1-suf] {
		suf++
	}
	insStart = pref
	insLen = len(newR) - suf - pref
	if insLen < 0 {
		insLen = 0
	}
	return insStart, insLen
}

func (s *Session) recordRightStyleBias(blockID model.BlockID, isCell bool, cellKey tableCellKey, spans []model.Span, prevOff, newAnchor, newCaret int) {
	s.rightStyleBias = nil
	if newAnchor != newCaret || prevOff <= newCaret || newCaret <= 0 {
		return
	}
	total := utf8.RuneCountInString(model.PlainTextOfSpans(spans))
	if newCaret >= total {
		return
	}
	left := sliceSpans(spans, newCaret-1, newCaret)
	right := sliceSpans(spans, newCaret, newCaret+1)
	if len(left) == 0 || len(right) == 0 {
		return
	}
	if left[0].ImageSrc != "" || right[0].ImageSrc != "" || left[0].Marks != right[0].Marks || left[0].BGColor != right[0].BGColor || left[0].FGColor != right[0].FGColor || left[0].Link != right[0].Link {
		s.rightStyleBias = &rightStyleBias{
			blockID: blockID,
			isCell:  isCell,
			cellKey: cellKey,
			pos:     newCaret,
		}
	}
}

func (s *Session) consumeRightStyleBias(blockID model.BlockID, isCell bool, cellKey tableCellKey, insStart, insLen int) bool {
	b := s.rightStyleBias
	s.rightStyleBias = nil
	if b == nil || insLen <= 0 {
		return false
	}
	return b.blockID == blockID && b.isCell == isCell && (!isCell || b.cellKey == cellKey) && b.pos == insStart
}

func inheritRightSpanStyleOnInsert(spans []model.Span, insStart, insLen int) []model.Span {
	if insLen <= 0 {
		return spans
	}
	total := utf8.RuneCountInString(model.PlainTextOfSpans(spans))
	if insStart+insLen >= total {
		return spans
	}
	right := sliceSpans(spans, insStart+insLen, insStart+insLen+1)
	if len(right) == 0 {
		return spans
	}
	tpl := right[0]
	return applySpanTransform(spans, insStart, insStart+insLen, func(sp model.Span) model.Span {
		sp.Marks = tpl.Marks
		sp.FGColor = tpl.FGColor
		sp.BGColor = tpl.BGColor
		sp.Link = tpl.Link
		return sp
	})
}

func (s *Session) consumeNextInsertGuard(blockID model.BlockID, isCell bool, cellKey tableCellKey, insStart, insLen int) (model.Mark, bool) {
	g := s.nextInsertGuard
	s.nextInsertGuard = nil
	if g == nil || g.docRev != s.docRev || g.blockID != blockID || g.isCell != isCell || (isCell && g.cellKey != cellKey) {
		return 0, false
	}
	if insLen <= 0 || insStart != g.caretPos {
		return 0, false
	}
	return g.clearMask, g.clearBG
}

func (s *Session) applyNextInsertStyleClear(spans []model.Span, insStart, insLen int, clearMask model.Mark, clearBG bool) []model.Span {
	if insLen <= 0 || (clearMask == 0 && !clearBG) {
		return spans
	}
	return applySpanTransform(spans, insStart, insStart+insLen, func(sp model.Span) model.Span {
		sp.Marks &^= clearMask
		if clearBG {
			sp.BGColor = ""
		}
		return sp
	})
}

// --- Span Protection Helpers (O(N) monotonic scan) ---

func spanProtectionAt(spans []model.Span, startRune, endRune int) bool {
	if endRune <= startRune {
		return false
	}
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if s1 <= startRune {
			continue
		}
		if s0 >= endRune {
			break
		}
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 || sp.Link != "" || sp.ImageSrc != "" {
			return true
		}
	}
	return false
}

func spanCodeOrMathAt(spans []model.Span, startRune, endRune int) bool {
	if endRune <= startRune {
		return false
	}
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if s1 <= startRune {
			continue
		}
		if s0 >= endRune {
			break
		}
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 {
			return true
		}
	}
	return false
}

func isEscapedAt(r []rune, pos int) bool {
	if pos <= 0 || pos > len(r) {
		return false
	}
	bs := 0
	for i := pos - 1; i >= 0 && r[i] == '\\'; i-- {
		bs++
	}
	return bs%2 == 1
}

func hasURLSchemePrefixRunes(r []rune, pos, limit int) bool {
	rem := limit - pos
	if rem >= 8 && strings.EqualFold(string(r[pos:pos+8]), "https://") {
		return true
	}
	if rem >= 7 && strings.EqualFold(string(r[pos:pos+7]), "http://") {
		return true
	}
	if rem >= 4 && strings.EqualFold(string(r[pos:pos+4]), "www.") {
		return true
	}
	return false
}

func isURLStartBoundary(r []rune, i, lineStart int) bool {
	if i <= lineStart {
		return true
	}
	prev := r[i-1]
	if unicode.IsSpace(prev) {
		return true
	}
	switch prev {
	case '(', '[', '{', '<', '"', '\'', '（', '【', '「', '『', '《', '“', '‘':
		return true
	}
	return false
}

func scanURLTokenEnd(r []rune, start, limit int, inParen bool, closeParen rune) int {
	i := start
	depth := 0
	for i < limit && !unicode.IsSpace(r[i]) {
		if inParen {
			openParen := '('
			if closeParen == '）' {
				openParen = '（'
			}
			if r[i] == openParen {
				depth++
			} else if r[i] == closeParen {
				if depth == 0 {
					break
				}
				depth--
			}
		}
		i++
	}
	return i
}

// inUnclosedMarkdownLinkDest reports whether rune position `pos` on the current line
// is inside an unclosed `[...](...` / `![...](...` destination or title.
func inUnclosedMarkdownLinkDest(r []rune, lineStart, pos int) bool {
	if pos <= lineStart+3 {
		return false
	}
	inDest := false
	inQuote := rune(0)
	parenDepth := 0
	for i := lineStart; i < pos; i++ {
		ch := r[i]
		if !inDest {
			if ch == '(' && i > lineStart && r[i-1] == ']' && !isEscapedAt(r, i-1) {
				// Verify there is a matching unescaped `[` earlier on the line.
				for j := i - 2; j >= lineStart; j-- {
					if r[j] == ']' && !isEscapedAt(r, j) {
						break
					}
					if r[j] == '[' && !isEscapedAt(r, j) {
						inDest = true
						inQuote = 0
						parenDepth = 0
						break
					}
				}
			}
			continue
		}
		if inQuote != 0 {
			if ch == inQuote && !isEscapedAt(r, i) {
				inQuote = 0
			}
			continue
		}
		if (ch == '"' || ch == '\'') && !isEscapedAt(r, i) {
			inQuote = ch
			continue
		}
		if ch == '(' && !isEscapedAt(r, i) {
			parenDepth++
		} else if ch == ')' && !isEscapedAt(r, i) {
			if parenDepth > 0 {
				parenDepth--
			} else {
				inDest = false
			}
		}
	}
	return inDest
}

// inUnclosedLiteralContext runs in O(N) over the current line using a monotonic span cursor.
// It skips completed URL tokens and Markdown link destinations so unpaired `$` or “ ` “
// inside an earlier URL never taint subsequent prose, while still protecting active URLs,
// unclosed inline code backticks (` / ｀), and unclosed inline math ($ / ＄).
func inUnclosedLiteralContext(spans []model.Span, r []rune, pos int) (inBacktick, inMath, inURL bool) {
	if pos <= 0 || pos > len(r) {
		return false, false, false
	}
	lineStart := pos
	for lineStart > 0 && r[lineStart-1] != '\n' {
		lineStart--
	}

	// Advance monotonic span cursor to lineStart once: O(N) total across the whole line.
	spIdx := 0
	spEnd := 0
	if len(spans) > 0 {
		spEnd = utf8.RuneCountInString(spans[0].Text)
	}
	advanceSpanTo := func(runePos int) bool {
		for spIdx < len(spans) && spEnd <= runePos {
			spIdx++
			if spIdx < len(spans) {
				spEnd += utf8.RuneCountInString(spans[spIdx].Text)
			}
		}
		if spIdx >= len(spans) {
			return false
		}
		sp := spans[spIdx]
		return sp.Marks&(model.MarkCode|model.MarkMath) != 0 || sp.Link != ""
	}

	var openBT rune
	var openMath rune
	for i := lineStart; i < pos; i++ {
		if advanceSpanTo(i) {
			continue
		}
		ch := r[i]
		// Outside unclosed code/math, detect URL tokens (bare, wrapped, or link destination)
		// and skip their contents so unpaired `$` or `` ` `` inside a URL never taint later text.
		if openBT == 0 && openMath == 0 && isURLStartBoundary(r, i, lineStart) && hasURLSchemePrefixRunes(r, i, pos) {
			inParen := i > lineStart && (r[i-1] == '(' || r[i-1] == '（')
			closeParen := ')'
			if i > lineStart && r[i-1] == '（' {
				closeParen = '）'
			}
			urlEnd := scanURLTokenEnd(r, i, pos, inParen, closeParen)
			if urlEnd == pos {
				return false, false, true
			}
			i = urlEnd - 1
			continue
		}
		if openMath == 0 && (ch == '`' || ch == '｀') && !isEscapedAt(r, i) {
			if (i > lineStart && r[i-1] == ch) || (i+1 < pos && r[i+1] == ch) {
				continue
			}
			if openBT == 0 {
				openBT = ch
			} else if openBT == ch {
				openBT = 0
			}
			continue
		}
		if openBT != 0 {
			continue
		}
		if (ch == '$' || ch == '＄') && !isEscapedAt(r, i) {
			if (i > lineStart && r[i-1] == ch) || (i+1 < pos && r[i+1] == ch) {
				continue
			}
			if openMath == 0 {
				if i+1 < pos && !unicode.IsSpace(r[i+1]) {
					openMath = ch
				}
			} else if openMath == ch && i > lineStart && !unicode.IsSpace(r[i-1]) {
				openMath = 0
			}
		}
	}
	return openBT != 0, openMath != 0, false
}

// --- Inline Markdown Real-Time Conversion ---

type inlineRuleResult struct {
	spans      []model.Span
	caret      int
	changed    bool
	codeEdge   appUi.RichCodeEdge
	clearMask  model.Mark
	clearBG    bool
	isMath     bool
	toImage    bool
	imageAlt   string
	imageSrc   string
	imageTitle string
}

func applyInlineInputRules(spans []model.Span, caretRune, insLen int, allowImageBlock bool) inlineRuleResult {
	plain := model.PlainTextOfSpans(spans)
	r := []rune(plain)
	n := len(r)
	if n == 0 {
		return inlineRuleResult{spans: spans, caret: 0}
	}
	if caretRune <= 0 || caretRune > n {
		caretRune = n
	}

	// 1. Direct match right at caret (e.g. closing `, *, **, ***, ~~, ==, ^, ~, $, )).
	res := tryInlineConstructEndingAt(spans, r, caretRune, allowImageBlock)
	if res.toImage {
		return res
	}
	if res.changed {
		if insLen > 1 {
			curR := []rune(model.PlainTextOfSpans(res.spans))
			scanLo := max(caretRune-insLen, 0)
			if updated, ok := autolinkBareURLsInWindow(res.spans, curR, scanLo, res.caret); ok {
				res.spans = updated
			}
		}
		return res
	}

	// 2. Trailing space / fullwidth space triggers:
	//    - Multi-rune commit fallback (e.g. IME committing "`code` " at once, insLen > 1)
	//    - Bare URL auto-link (`https://... ` / `www.... `) when not inside unclosed code/math
	if caretRune > 0 && (r[caretRune-1] == ' ' || r[caretRune-1] == '\u3000') {
		tokenEnd := caretRune - 1
		if insLen > 1 {
			if res := tryInlineConstructEndingAt(spans, r, tokenEnd, allowImageBlock); res.changed || res.toImage {
				if res.toImage {
					return res
				}
				spacePos := res.caret
				res.spans = applySpanTransform(res.spans, spacePos, spacePos+1, func(sp model.Span) model.Span {
					sp.Marks &^= model.MarkCode | model.MarkMath | model.MarkBold | model.MarkItalic | model.MarkStrike | model.MarkSuperscript | model.MarkSubscript
					sp.BGColor = ""
					sp.Link = ""
					return sp
				})
				res.caret = spacePos + 1
				res.codeEdge = appUi.RichCodeEdgeNone
				res.clearMask = 0
				res.clearBG = false
				curR := []rune(model.PlainTextOfSpans(res.spans))
				scanLo := max(caretRune-insLen, 0)
				if updated, ok := autolinkBareURLsInWindow(res.spans, curR, scanLo, res.caret); ok {
					res.spans = updated
				}
				return res
			}
		}
		if res := tryBareURLAutolinkBefore(spans, r, tokenEnd); res.changed {
			if insLen > 1 {
				scanLo := max(caretRune-insLen, 0)
				if updated, ok := autolinkBareURLsInWindow(res.spans, r, scanLo, tokenEnd); ok {
					res.spans = updated
				}
			}
			res.caret = caretRune
			return res
		}
	}

	// 3. Multi-rune commit whose construct closes before trailing text (e.g. IME committing "“`code`”" at once).
	if insLen > 1 && insLen <= 256 {
		scanLo := max(caretRune-insLen, 0)
		for e := caretRune - 1; e > scanLo; e-- {
			res := tryInlineConstructEndingAt(spans, r, e, false)
			if !res.changed {
				continue
			}
			res.caret = caretRune - (n - utf8.RuneCountInString(model.PlainTextOfSpans(res.spans)))
			res.codeEdge = appUi.RichCodeEdgeNone
			res.clearMask = 0
			res.clearBG = false
			return res
		}
	}

	// 4. Multi-rune commit interior bare URLs followed by whitespace (e.g. `https://example.com/$x foo`).
	if insLen > 1 {
		scanLo := max(caretRune-insLen, 0)
		if updated, ok := autolinkBareURLsInWindow(spans, r, scanLo, caretRune); ok {
			return inlineRuleResult{spans: updated, caret: caretRune, changed: true}
		}
	}

	return inlineRuleResult{spans: spans, caret: caretRune}
}

type batchURLLink struct {
	start int
	end   int
	url   string
}

func autolinkBareURLsInWindow(spans []model.Span, r []rune, scanLo, limit int) ([]model.Span, bool) {
	if limit <= 0 || limit > len(r) {
		limit = len(r)
	}
	if scanLo < 0 {
		scanLo = 0
	}
	var prot []bool
	var links []batchURLLink
	for i := scanLo; i < limit; i++ {
		if !unicode.IsSpace(r[i]) {
			continue
		}
		start, end, url, ok := bareURLCandidateBefore(r, i)
		if !ok {
			continue
		}
		if len(links) > 0 && start < links[len(links)-1].end {
			continue
		}
		if prot == nil {
			prot = buildLiteralProtectedMask(spans, r, limit, false)
		}
		blocked := false
		for k := start; k < end; k++ {
			if prot[k] {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		links = append(links, batchURLLink{start: start, end: end, url: url})
	}
	if len(links) == 0 {
		return spans, false
	}
	return applyBatchURLLinksToSpans(spans, links), true
}

func applyBatchURLLinksToSpans(spans []model.Span, links []batchURLLink) []model.Span {
	out := make([]model.Span, 0, len(spans)+len(links)*2)
	li := 0
	spStart := 0
	for _, sp := range spans {
		rs := []rune(sp.Text)
		spLen := len(rs)
		spEnd := spStart + spLen
		if spLen == 0 {
			continue
		}
		for li < len(links) && links[li].end <= spStart {
			li++
		}
		if li >= len(links) || links[li].start >= spEnd {
			out = append(out, sp)
			spStart = spEnd
			continue
		}
		cursor := spStart
		for li < len(links) && links[li].start < spEnd {
			lk := links[li]
			if lk.start > cursor {
				seg := sp
				seg.Text = string(rs[cursor-spStart : lk.start-spStart])
				out = append(out, seg)
			}
			hi := min(lk.end, spEnd)
			lo := max(lk.start, cursor)
			if hi > lo {
				seg := sp
				seg.Text = string(rs[lo-spStart : hi-spStart])
				seg.Link = lk.url
				out = append(out, seg)
			}
			cursor = hi
			if lk.end <= spEnd {
				li++
			} else {
				break
			}
		}
		if cursor < spEnd {
			seg := sp
			seg.Text = string(rs[cursor-spStart:])
			out = append(out, seg)
		}
		spStart = spEnd
	}
	return normalizeSpans(out)
}

func tryInlineConstructEndingAt(spans []model.Span, r []rune, end int, allowImageBlock bool) inlineRuleResult {
	if end < 2 {
		return inlineRuleResult{spans: spans, caret: end}
	}
	last := r[end-1]

	// Standalone image `![alt](url)` or inline link `[label](url)` / `[label]（https://...）` / `［label］（https://...）`.
	if last == ')' || last == '）' {
		if allowImageBlock && last == ')' {
			trimmed := strings.TrimSpace(string(r[:end]))
			rest := strings.TrimSpace(string(r[end:]))
			if rest == "" {
				if alt, src, title, ok := matchStandaloneImageLine(trimmed); ok && !spanHasInlineImageAt(spans, 0, len(r)) && !spanCodeOrMathAt(spans, 0, len(r)) {
					return inlineRuleResult{toImage: true, imageAlt: alt, imageSrc: src, imageTitle: title}
				}
			}
		}
		if res := tryInlineLinkEndingAt(spans, r, end); res.changed {
			return res
		}
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Inline code `` `code` `` or fullwidth `` ｀code｀ `` (guard against ```, unclosed math, and URLs).
	if last == '`' || last == '｀' {
		_, inMath, inURL := inUnclosedLiteralContext(spans, r, end-1)
		if inMath || inURL || isEscapedAt(r, end-1) || (end >= 2 && r[end-2] == last) {
			return inlineRuleResult{spans: spans, caret: end}
		}
		open := -1
		for i := end - 2; i >= 0; i-- {
			if r[i] == '\n' {
				break
			}
			if r[i] == last && !isEscapedAt(r, i) {
				if i > 0 && r[i-1] == last {
					return inlineRuleResult{spans: spans, caret: end}
				}
				open = i
				break
			}
		}
		if open >= 0 && end-1 > open+1 && !spanCodeOrMathAt(spans, open, end) {
			return replaceInlineDelimited(spans, len(r), open, end, 1, func(sp model.Span) model.Span {
				sp.Marks = (sp.Marks &^ model.MarkMath) | model.MarkCode
				return sp
			}, appUi.RichCodeEdgeAfter, model.MarkCode|model.MarkMath, false)
		}
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Inline math `$E=mc^2$` or fullwidth `＄E=mc^2＄` (guard against `$$`, unclosed code, URLs, and currency).
	if last == '$' || last == '＄' {
		inBT, _, inURL := inUnclosedLiteralContext(spans, r, end-1)
		if inBT || inURL || isEscapedAt(r, end-1) || (end >= 2 && (r[end-2] == last || unicode.IsSpace(r[end-2]))) {
			return inlineRuleResult{spans: spans, caret: end}
		}
		// Never close math if immediately followed by another `$` or an ASCII digit (`$5&$10`).
		if end < len(r) && (r[end] == last || (r[end] >= '0' && r[end] <= '9')) {
			return inlineRuleResult{spans: spans, caret: end}
		}
		open := -1
		for i := end - 2; i >= 0; i-- {
			if r[i] == '\n' {
				break
			}
			if r[i] == last && !isEscapedAt(r, i) {
				if i > 0 && r[i-1] == last {
					return inlineRuleResult{spans: spans, caret: end}
				}
				open = i
				break
			}
		}
		if open >= 0 && end-1 > open+1 && !unicode.IsSpace(r[open+1]) && !spanCodeOrMathAt(spans, open, end) {
			res := replaceInlineDelimited(spans, len(r), open, end, 1, func(sp model.Span) model.Span {
				sp.Marks = (sp.Marks &^ model.MarkCode) | model.MarkMath
				return sp
			}, appUi.RichCodeEdgeAfter, model.MarkCode|model.MarkMath, false)
			res.isMath = true
			return res
		}
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Do not trigger emphasis / highlight / strike / sup / sub inside unclosed inline code, math, or bare URL.
	if inBT, inMath, inURL := inUnclosedLiteralContext(spans, r, end-1); inBT || inMath || inURL {
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Highlight `==text==` or fullwidth `＝＝text＝＝`.
	if last == '=' || last == '＝' {
		if end >= 5 && r[end-2] == last && (end < 3 || r[end-3] != last) && !isEscapedAt(r, end-2) {
			if open, ok := findPairedDoubleDelim(r, end, last); ok && !spanCodeOrMathAt(spans, open, end) {
				return replaceInlineDelimited(spans, len(r), open, end, 2, func(sp model.Span) model.Span {
					sp.BGColor = "yellow"
					return sp
				}, appUi.RichCodeEdgeNone, 0, true)
			}
		}
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Strikethrough `~~text~~` / `～～text～～` or Subscript `~text~`.
	if last == '~' || last == '～' {
		if end >= 5 && r[end-2] == last && (end < 3 || r[end-3] != last) && !isEscapedAt(r, end-2) {
			if open, ok := findPairedDoubleDelim(r, end, last); ok && !spanCodeOrMathAt(spans, open, end) {
				return replaceInlineDelimited(spans, len(r), open, end, 2, func(sp model.Span) model.Span {
					sp.Marks |= model.MarkStrike
					return sp
				}, appUi.RichCodeEdgeNone, model.MarkStrike, false)
			}
			return inlineRuleResult{spans: spans, caret: end}
		}
		if last == '~' && end >= 3 && r[end-2] != '~' && !isEscapedAt(r, end-1) {
			if open, ok := findPairedSingleNoSpaceDelim(r, end, '~'); ok && !spanCodeOrMathAt(spans, open, end) {
				return replaceInlineDelimited(spans, len(r), open, end, 1, func(sp model.Span) model.Span {
					sp.Marks = (sp.Marks &^ model.MarkSuperscript) | model.MarkSubscript
					return sp
				}, appUi.RichCodeEdgeNone, model.MarkSubscript, false)
			}
		}
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Superscript `^text^`.
	if last == '^' {
		if end >= 3 && r[end-2] != '^' && !isEscapedAt(r, end-1) {
			if open, ok := findPairedSingleNoSpaceDelim(r, end, '^'); ok && !spanCodeOrMathAt(spans, open, end) {
				return replaceInlineDelimited(spans, len(r), open, end, 1, func(sp model.Span) model.Span {
					sp.Marks = (sp.Marks &^ model.MarkSubscript) | model.MarkSuperscript
					return sp
				}, appUi.RichCodeEdgeNone, model.MarkSuperscript, false)
			}
		}
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Emphasis: `***text***` / `**text**` / `*text*`, `___text___` / `__text__` / `_text_`, and fullwidth `＊＊text＊＊`.
	if last == '*' || last == '_' || last == '＊' {
		closeRun := 0
		for end-1-closeRun >= 0 && r[end-1-closeRun] == last {
			closeRun++
		}
		if isEscapedAt(r, end-closeRun) {
			return inlineRuleResult{spans: spans, caret: end}
		}
		if last != '＊' && closeRun == 3 && end >= 7 {
			if open, ok := findPairedTripleDelim(r, end, last); ok && !spanCodeOrMathAt(spans, open, end) {
				return replaceInlineDelimited(spans, len(r), open, end, 3, func(sp model.Span) model.Span {
					sp.Marks |= model.MarkBold | model.MarkItalic
					return sp
				}, appUi.RichCodeEdgeNone, model.MarkBold|model.MarkItalic, false)
			}
		} else if closeRun == 2 && end >= 5 {
			if open, ok := findPairedDoubleDelim(r, end, last); ok && !spanCodeOrMathAt(spans, open, end) {
				return replaceInlineDelimited(spans, len(r), open, end, 2, func(sp model.Span) model.Span {
					sp.Marks |= model.MarkBold
					return sp
				}, appUi.RichCodeEdgeNone, model.MarkBold, false)
			}
		} else if last != '＊' && closeRun == 1 && end >= 3 {
			if open, ok := findPairedSingleEmphasisDelim(r, end, last); ok && !spanCodeOrMathAt(spans, open, end) {
				return replaceInlineDelimited(spans, len(r), open, end, 1, func(sp model.Span) model.Span {
					sp.Marks |= model.MarkItalic
					return sp
				}, appUi.RichCodeEdgeNone, model.MarkItalic, false)
			}
		}
	}

	return inlineRuleResult{spans: spans, caret: end}
}

func isWordBoundaryBeforeUnderscore(r []rune, open int) bool {
	if open <= 0 {
		return true
	}
	prev := r[open-1]
	return unicode.IsSpace(prev) || unicode.IsPunct(prev) || unicode.IsSymbol(prev)
}

func findPairedTripleDelim(r []rune, end int, ch rune) (int, bool) {
	innerEnd := end - 3
	if innerEnd <= 3 || unicode.IsSpace(r[innerEnd-1]) {
		return -1, false
	}
	for i := innerEnd - 1; i >= 2; i-- {
		if r[i] == '\n' {
			return -1, false
		}
		if r[i] == ch && r[i-1] == ch && r[i-2] == ch {
			open := i - 2
			if open > 0 && r[open-1] == ch {
				return -1, false
			}
			if isEscapedAt(r, open) {
				return -1, false
			}
			if ch == '_' && !isWordBoundaryBeforeUnderscore(r, open) {
				return -1, false
			}
			if open+3 < innerEnd && !unicode.IsSpace(r[open+3]) {
				return open, true
			}
			return -1, false
		}
	}
	return -1, false
}

func findPairedDoubleDelim(r []rune, end int, ch rune) (int, bool) {
	innerEnd := end - 2
	if innerEnd <= 2 || unicode.IsSpace(r[innerEnd-1]) {
		return -1, false
	}
	for i := innerEnd - 1; i >= 1; i-- {
		if r[i] == '\n' {
			return -1, false
		}
		if r[i] == ch && r[i-1] == ch {
			open := i - 1
			if open > 0 && r[open-1] == ch {
				return -1, false
			}
			if isEscapedAt(r, open) {
				return -1, false
			}
			if ch == '_' && !isWordBoundaryBeforeUnderscore(r, open) {
				return -1, false
			}
			if open+2 < innerEnd && !unicode.IsSpace(r[open+2]) {
				return open, true
			}
			return -1, false
		}
	}
	return -1, false
}

func findPairedSingleEmphasisDelim(r []rune, end int, ch rune) (int, bool) {
	innerEnd := end - 1
	if innerEnd <= 1 || unicode.IsSpace(r[innerEnd-1]) {
		return -1, false
	}
	for i := innerEnd - 1; i >= 0; i-- {
		if r[i] == '\n' {
			return -1, false
		}
		if r[i] == ch {
			if (i > 0 && r[i-1] == ch) || (i+1 < innerEnd && r[i+1] == ch) {
				return -1, false
			}
			if isEscapedAt(r, i) {
				return -1, false
			}
			if ch == '_' && !isWordBoundaryBeforeUnderscore(r, i) {
				return -1, false
			}
			if i+1 < innerEnd && !unicode.IsSpace(r[i+1]) {
				return i, true
			}
			return -1, false
		}
	}
	return -1, false
}

func findPairedSingleNoSpaceDelim(r []rune, end int, ch rune) (int, bool) {
	innerEnd := end - 1
	if innerEnd <= 1 {
		return -1, false
	}
	for i := innerEnd - 1; i >= 0; i-- {
		if unicode.IsSpace(r[i]) {
			return -1, false
		}
		if r[i] == ch {
			if (i > 0 && r[i-1] == ch) || isEscapedAt(r, i) {
				return -1, false
			}
			if i+1 < innerEnd {
				return i, true
			}
			return -1, false
		}
	}
	return -1, false
}

func spanSplitsInlineImage(spans []model.Span, open, end, delimLen int) bool {
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if sp.ImageSrc == "" {
			continue
		}
		if (open+delimLen > s0 && open < s1) || (end-delimLen < s1 && end > s0) {
			return true
		}
	}
	return false
}

func replaceInlineDelimited(spans []model.Span, totalRunes, open, end, delimLen int, fn func(model.Span) model.Span, codeEdge appUi.RichCodeEdge, clearMask model.Mark, clearBG bool) inlineRuleResult {
	if spanSplitsInlineImage(spans, open, end, delimLen) {
		return inlineRuleResult{spans: spans, caret: end}
	}
	left := sliceSpans(spans, 0, open)
	mid := sliceSpans(spans, open+delimLen, end-delimLen)
	right := sliceSpans(spans, end, totalRunes)
	for i := range mid {
		mid[i] = fn(mid[i])
	}
	var merged []model.Span
	merged = append(merged, left...)
	merged = append(merged, mid...)
	merged = append(merged, right...)
	newCaret := end - 2*delimLen
	return inlineRuleResult{
		spans:     normalizeSpans(merged),
		caret:     newCaret,
		changed:   true,
		codeEdge:  codeEdge,
		clearMask: clearMask,
		clearBG:   clearBG,
	}
}

func normalizeInlineLinkURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	lower := strings.ToLower(u)
	if strings.HasPrefix(lower, "www.") {
		return "https://" + u
	}
	return u
}

func isPlausibleInlineLinkTarget(raw string, requireExplicitScheme bool) bool {
	u := strings.TrimSpace(raw)
	if u == "" || strings.ContainsAny(u, " \t\r\n<>") {
		return false
	}
	lower := strings.ToLower(u)
	if strings.HasPrefix(lower, "http://") && len(u) > len("http://") {
		return true
	}
	if strings.HasPrefix(lower, "https://") && len(u) > len("https://") {
		return true
	}
	if strings.HasPrefix(lower, "www.") && len(u) > len("www.") && strings.ContainsRune(u[4:], '.') {
		return true
	}
	if requireExplicitScheme {
		return false
	}
	if strings.HasPrefix(lower, "mailto:") && len(u) > len("mailto:") {
		return true
	}
	if strings.HasPrefix(lower, "tel:") && len(u) > len("tel:") {
		return true
	}
	if strings.HasPrefix(lower, "ftp://") && len(u) > len("ftp://") {
		return true
	}
	if strings.HasPrefix(u, "#") && len(u) > 1 {
		return true
	}
	if strings.HasPrefix(u, "/") || strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../") {
		return len(u) > 1
	}
	// Accept relative paths/domains/ASCII link targets (e.g. `docs/guide`, `page.html`, `url`),
	// while rejecting CJK prose notes like `[重要](请先备份)` that contain no path separator.
	hasNonASCII := false
	for _, ch := range u {
		if ch > 0x7F {
			hasNonASCII = true
			break
		}
	}
	if hasNonASCII {
		return strings.ContainsRune(u, '/')
	}
	if strings.HasPrefix(u, ".") || strings.HasSuffix(u, ".") {
		return false
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("._~:/?#[]@!$&'()*+,;=%-", c) >= 0) {
			return false
		}
	}
	return true
}

func isASCIIPunctByteLocal(b byte) bool {
	return (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~')
}

func unescapeMarkdownEntitiesLocal(s string) string {
	if !strings.ContainsRune(s, '&') || !strings.ContainsRune(s, ';') {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	changed := false
	for i := 0; i < len(s); {
		if s[i] == '&' {
			limit := len(s)
			if limit > i+34 {
				limit = i + 34
			}
			if semi := strings.IndexByte(s[i+1:limit], ';'); semi > 0 {
				raw := s[i : i+semi+2]
				if dec := html.UnescapeString(raw); dec != raw {
					out.WriteString(dec)
					i += semi + 2
					changed = true
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	if !changed {
		return s
	}
	return out.String()
}

func unescapeMarkdownPunct(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunctByteLocal(s[i+1]) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func tryInlineLinkEndingAt(spans []model.Span, r []rune, end int) inlineRuleResult {
	// Match `[label](url)`, `![alt](url "title")`, `[label]（https://...）`, and `［label］（https://...）` ending at `end`,
	// allowing balanced `(...)` inside URL and quoted titles containing `(`.
	closeParen := r[end-1]
	openChar := '('
	fullwidthParen := closeParen == '）'
	if fullwidthParen {
		openChar = '（'
	}
	openParen := -1
	depth := 0
	inQuote := rune(0)
	for i := end - 2; i >= 1; i-- {
		if r[i] == '\n' {
			return inlineRuleResult{spans: spans, caret: end}
		}
		if !fullwidthParen && (r[i] == '"' || r[i] == '\'') && !isEscapedAt(r, i) {
			if inQuote == 0 {
				// Only enter quoted title mode when closing quote is followed solely by spaces before `)`.
				onlySpaceAfter := true
				for k := i + 1; k < end-1; k++ {
					if r[k] != ' ' && r[k] != '\t' {
						onlySpaceAfter = false
						break
					}
				}
				if onlySpaceAfter {
					inQuote = r[i]
					continue
				}
			} else if inQuote == r[i] {
				inQuote = 0
				continue
			}
		}
		if inQuote != 0 {
			continue
		}
		if r[i] == closeParen && !isEscapedAt(r, i) {
			depth++
			continue
		}
		if r[i] == openChar && !isEscapedAt(r, i) {
			if depth > 0 {
				depth--
				continue
			}
			if (r[i-1] == ']' || (fullwidthParen && r[i-1] == '］')) && !isEscapedAt(r, i-1) {
				openParen = i
				break
			}
			return inlineRuleResult{spans: spans, caret: end}
		}
	}
	if openParen < 2 {
		return inlineRuleResult{spans: spans, caret: end}
	}
	closeBracket := openParen - 1
	wantOpenBracket := '['
	if r[closeBracket] == '］' {
		wantOpenBracket = '［'
	}
	openBracket := -1
	for i := closeBracket - 1; i >= 0; i-- {
		if r[i] == '\n' || (r[i] == r[closeBracket] && !isEscapedAt(r, i)) {
			return inlineRuleResult{spans: spans, caret: end}
		}
		if r[i] == wantOpenBracket {
			if isEscapedAt(r, i) {
				continue
			}
			openBracket = i
			break
		}
	}
	if openBracket < 0 {
		return inlineRuleResult{spans: spans, caret: end}
	}
	isImage := openBracket > 0 && (r[openBracket-1] == '!' || r[openBracket-1] == '！') && !isEscapedAt(r, openBracket-1)
	startPos := openBracket
	if isImage {
		startPos = openBracket - 1
	}
	if inBT, inMath, _ := inUnclosedLiteralContext(spans, r, startPos); inBT || inMath {
		return inlineRuleResult{spans: spans, caret: end}
	}
	if isImage {
		imgStart := openBracket - 1
		rawInner := strings.TrimSpace(string(r[openParen+1 : end-1]))
		url, title, ok := splitInlineTargetAndTitle(rawInner, fullwidthParen)
		if !ok || spanCodeOrMathAt(spans, imgStart, end) || spanHasInlineImageAt(spans, imgStart, end) {
			return inlineRuleResult{spans: spans, caret: end}
		}
		alt := unescapeMarkdownEntitiesLocal(unescapeMarkdownPunct(string(r[openBracket+1 : closeBracket])))
		label := alt
		altEmpty := label == ""
		if altEmpty {
			label = fallbackInlineImageAlt(url)
		}
		imgSpan := model.Span{
			Text:          label,
			ImageSrc:      url,
			ImageTitle:    title,
			ImageAltEmpty: altEmpty,
		}
		if outer := sliceSpans(spans, imgStart, imgStart+1); len(outer) > 0 {
			imgSpan.Marks = outer[0].Marks &^ (model.MarkCode | model.MarkMath)
			imgSpan.Link = outer[0].Link
			imgSpan.LinkTitle = outer[0].LinkTitle
			imgSpan.FGColor = outer[0].FGColor
			imgSpan.BGColor = outer[0].BGColor
		}
		left := sliceSpans(spans, 0, imgStart)
		right := sliceSpans(spans, end, len(r))
		var merged []model.Span
		merged = append(merged, left...)
		merged = append(merged, imgSpan)
		merged = append(merged, right...)
		return inlineRuleResult{
			spans:   normalizeSpans(merged),
			caret:   imgStart + utf8.RuneCountInString(label),
			changed: true,
		}
	}
	labelLen := closeBracket - (openBracket + 1)
	rawInner := strings.TrimSpace(string(r[openParen+1 : end-1]))
	url, linkTitle, ok := splitInlineTargetAndTitle(rawInner, fullwidthParen)
	if labelLen <= 0 || !ok {
		return inlineRuleResult{spans: spans, caret: end}
	}
	if spanCodeOrMathAt(spans, openBracket, end) ||
		spanHasInlineImageAt(spans, openBracket, openBracket+1) ||
		spanHasInlineImageAt(spans, closeBracket, end) ||
		spanBoundarySplitsInlineImage(spans, openBracket+1, closeBracket) {
		return inlineRuleResult{spans: spans, caret: end}
	}
	left := sliceSpans(spans, 0, openBracket)
	mid := sliceSpans(spans, openBracket+1, closeBracket)
	right := sliceSpans(spans, end, len(r))
	for i := range mid {
		mid[i].Link = url
		mid[i].LinkTitle = linkTitle
	}
	var merged []model.Span
	merged = append(merged, left...)
	merged = append(merged, mid...)
	merged = append(merged, right...)
	return inlineRuleResult{
		spans:   normalizeSpans(merged),
		caret:   openBracket + labelLen,
		changed: true,
	}
}

func fallbackInlineImageAlt(dest string) string {
	d := strings.TrimSpace(dest)
	if idx := strings.LastIndexByte(d, '/'); idx >= 0 && idx+1 < len(d) {
		return d[idx+1:]
	}
	if d != "" {
		return d
	}
	return "image"
}

func spanHasInlineImageAt(spans []model.Span, start, end int) bool {
	if end <= start {
		return false
	}
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if s1 <= start {
			continue
		}
		if s0 >= end {
			break
		}
		if sp.ImageSrc != "" {
			return true
		}
	}
	return false
}

func splitInlineTargetAndTitle(raw string, fullwidthParen bool) (url, title string, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", false
	}
	var dest, rest string
	if s[0] == '<' {
		gt := -1
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if s[i] == '>' {
				gt = i
				break
			}
			if s[i] == '<' || s[i] == '\n' || s[i] == '\r' {
				return "", "", false
			}
		}
		if gt <= 1 {
			return "", "", false
		}
		dest = s[1:gt]
		rest = strings.TrimSpace(s[gt+1:])
	} else {
		depth := 0
		split := len(s)
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if s[i] == '(' {
				depth++
			} else if s[i] == ')' {
				if depth == 0 {
					return "", "", false
				}
				depth--
			} else if (s[i] == ' ' || s[i] == '\t') && depth == 0 {
				split = i
				break
			}
		}
		if depth != 0 {
			return "", "", false
		}
		dest = s[:split]
		rest = strings.TrimSpace(s[split:])
		if !isPlausibleInlineLinkTarget(dest, fullwidthParen) {
			return "", "", false
		}
	}
	if rest != "" {
		if len(rest) < 2 {
			return "", "", false
		}
		openQ := rest[0]
		var closeQ byte
		switch openQ {
		case '"':
			closeQ = '"'
		case '\'':
			closeQ = '\''
		case '(':
			closeQ = ')'
		default:
			return "", "", false
		}
		var tb strings.Builder
		closed := false
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\\' && i+1 < len(rest) {
				if isASCIIPunctByteLocal(rest[i+1]) {
					tb.WriteByte(rest[i+1])
				} else {
					tb.WriteByte('\\')
					tb.WriteByte(rest[i+1])
				}
				i++
				continue
			}
			if rest[i] == closeQ {
				if i != len(rest)-1 {
					return "", "", false
				}
				closed = true
				break
			}
			tb.WriteByte(rest[i])
		}
		if !closed {
			return "", "", false
		}
		title = unescapeMarkdownEntitiesLocal(tb.String())
	}
	return normalizeInlineLinkURL(unescapeMarkdownEntitiesLocal(unescapeMarkdownPunct(dest))), title, true
}

func matchStandaloneImageLine(s string) (alt, src, title string, ok bool) {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "![") || !strings.HasSuffix(t, ")") {
		return "", "", "", false
	}
	closeBracket := -1
	depth := 0
	for i := 2; i < len(t)-1; i++ {
		if t[i] == '\\' && i+1 < len(t)-1 {
			i++
			continue
		}
		if t[i] == '[' {
			depth++
			continue
		}
		if t[i] == ']' {
			if depth > 0 {
				depth--
				continue
			}
			if i+1 < len(t)-1 && t[i+1] == '(' {
				closeBracket = i
				break
			}
			return "", "", "", false
		}
	}
	if closeBracket < 2 {
		return "", "", "", false
	}
	alt = unescapeMarkdownPunct(t[2:closeBracket])
	src, title, ok = splitInlineTargetAndTitle(t[closeBracket+2:len(t)-1], false)
	if !ok {
		return "", "", "", false
	}
	return alt, src, title, true
}

func bareURLCandidateBefore(r []rune, tokenEnd int) (start, end int, url string, ok bool) {
	if tokenEnd <= 0 || tokenEnd > len(r) {
		return 0, 0, "", false
	}
	start = tokenEnd
	for start > 0 && !unicode.IsSpace(r[start-1]) {
		start--
	}
	for start < tokenEnd {
		switch r[start] {
		case '(', '[', '{', '<', '"', '\'', '（', '【', '「', '『', '《', '“', '‘':
			start++
			continue
		}
		break
	}
	if start >= tokenEnd {
		return 0, 0, "", false
	}
	end = tokenEnd
	parenCounted := false
	opens, closes := 0, 0
	for end > start {
		ch := r[end-1]
		if ch == ')' {
			if !parenCounted {
				for i := start; i < end; i++ {
					if r[i] == '(' {
						opens++
					} else if r[i] == ')' {
						closes++
					}
				}
				parenCounted = true
			}
			if closes > opens {
				closes--
				end--
				continue
			}
			break
		}
		switch ch {
		case '.', ',', ';', ':', '!', '?', '\'', '"', ']', '}', '>', '。', '，', '；', '：', '！', '？', '）', '】', '》', '」', '』', '”', '’':
			end--
			continue
		}
		break
	}
	if end <= start {
		return 0, 0, "", false
	}
	cand := string(r[start:end])
	lower := strings.ToLower(cand)
	switch {
	case strings.HasPrefix(lower, "https://") && len(cand) > len("https://"):
		return start, end, cand, true
	case strings.HasPrefix(lower, "http://") && len(cand) > len("http://"):
		return start, end, cand, true
	case strings.HasPrefix(lower, "www.") && len(cand) > len("www.") && strings.ContainsRune(cand[4:], '.'):
		return start, end, "https://" + cand, true
	default:
		return 0, 0, "", false
	}
}

func tryBareURLAutolinkBefore(spans []model.Span, r []rune, tokenEnd int) inlineRuleResult {
	start, end, url, ok := bareURLCandidateBefore(r, tokenEnd)
	if !ok {
		return inlineRuleResult{spans: spans, caret: tokenEnd}
	}
	if inBT, inMath, _ := inUnclosedLiteralContext(spans, r, tokenEnd); inBT || inMath {
		return inlineRuleResult{spans: spans, caret: tokenEnd}
	}
	if spanProtectionAt(spans, start, end) {
		return inlineRuleResult{spans: spans, caret: tokenEnd}
	}
	next := applySpanTransform(spans, start, end, func(sp model.Span) model.Span {
		sp.Link = url
		return sp
	})
	return inlineRuleResult{
		spans:   next,
		caret:   tokenEnd,
		changed: true,
	}
}

// --- Smart Typography Replacements ---

type smartTypoRule struct {
	from  []rune
	to    []rune
	toStr string
}

var smartTypoRules = []smartTypoRule{
	{[]rune("<->"), []rune("↔"), "↔"},
	{[]rune("<=>"), []rune("⇔"), "⇔"},
	{[]rune("←>"), []rune("↔"), "↔"},
	{[]rune("⇐>"), []rune("⇔"), "⇔"},
	{[]rune("≤>"), []rune("⇔"), "⇔"},
	{[]rune("->"), []rune("→"), "→"},
	{[]rune("-》"), []rune("→"), "→"},
	{[]rune("—>"), []rune("→"), "→"},
	{[]rune("—》"), []rune("→"), "→"},
	{[]rune("=>"), []rune("⇒"), "⇒"},
	{[]rune("=》"), []rune("⇒"), "⇒"},
	{[]rune("<-"), []rune("←"), "←"},
	{[]rune("《-"), []rune("←"), "←"},
	{[]rune("!="), []rune("≠"), "≠"},
	{[]rune("<="), []rune("≤"), "≤"},
	{[]rune(">="), []rune("≥"), "≥"},
	{[]rune("+-"), []rune("±"), "±"},
	{[]rune("..."), []rune("…"), "…"},
	{[]rune("(c)"), []rune("©"), "©"},
	{[]rune("(C)"), []rune("©"), "©"},
	{[]rune("(tm)"), []rune("™"), "™"},
	{[]rune("(TM)"), []rune("™"), "™"},
	{[]rune("(r)"), []rune("®"), "®"},
	{[]rune("(R)"), []rune("®"), "®"},
}

func replaceSpanRuneRange(spans []model.Span, total, start, end int, replacement string) []model.Span {
	left := sliceSpans(spans, 0, start)
	right := sliceSpans(spans, end, total)
	tpl := model.Span{Text: replacement}
	if mid := sliceSpans(spans, start, start+1); len(mid) > 0 {
		tpl = mid[0]
		tpl.Text = replacement
	}
	var merged []model.Span
	merged = append(merged, left...)
	if replacement != "" {
		merged = append(merged, tpl)
	}
	merged = append(merged, right...)
	return normalizeSpans(merged)
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func applySmartTypographyOnSpans(spans []model.Span, caretRune, insStart, insLen int) ([]model.Span, int, bool) {
	if insLen <= 0 {
		return spans, caretRune, false
	}
	plain := model.PlainTextOfSpans(spans)
	r := []rune(plain)
	n := len(r)
	if n == 0 {
		return spans, caretRune, false
	}
	if caretRune <= 0 || caretRune > n {
		caretRune = n
	}

	// Em-dash `-- ` / `--　` only when preceded by non-dash, non-empty content (never breaks `---` divider).
	if caretRune >= 4 && (r[caretRune-1] == ' ' || r[caretRune-1] == '\u3000') &&
		r[caretRune-2] == '-' && r[caretRune-3] == '-' && r[caretRune-4] != '-' && !isEscapedAt(r, caretRune-3) {
		emLineStart := caretRune - 1
		for emLineStart > 0 && r[emLineStart-1] != '\n' {
			emLineStart--
		}
		if inBT, inMath, inURL := inUnclosedLiteralContext(spans, r, caretRune-1); !inBT && !inMath && !inURL && !inUnclosedMarkdownLinkDest(r, emLineStart, caretRune-1) {
			hasNonDash := false
			for i := 0; i < caretRune-3; i++ {
				if r[i] != '-' && !unicode.IsSpace(r[i]) {
					hasNonDash = true
					break
				}
			}
			if hasNonDash && !spanProtectionAt(spans, caretRune-3, caretRune-1) {
				next := replaceSpanRuneRange(spans, n, caretRune-3, caretRune-1, "—")
				return next, caretRune - 1, true
			}
		}
	}

	curSpans := spans
	curCaret := caretRune

	// Single-character typing: ONLY match rules ending right at caret.
	if insLen == 1 {
		lineStart := curCaret
		for lineStart > 0 && r[lineStart-1] != '\n' {
			lineStart--
		}
		if inBT, inMath, inURL := inUnclosedLiteralContext(spans, r, curCaret); inBT || inMath || inURL || inUnclosedMarkdownLinkDest(r, lineStart, curCaret) {
			return spans, caretRune, false
		}
		for _, rule := range smartTypoRules {
			fl := len(rule.from)
			if curCaret >= fl && runesEqual(r[curCaret-fl:curCaret], rule.from) {
				start := curCaret - fl
				if !isEscapedAt(r, start) && !spanProtectionAt(curSpans, start, curCaret) {
					curSpans = replaceSpanRuneRange(curSpans, n, start, curCaret, rule.toStr)
					return curSpans, start + len(rule.to), true
				}
			}
		}
		return spans, caretRune, false
	}

	// Multi-character commit (IME / whole-string Type): O(N) single-pass scan over [scanStart, curCaret).
	scanStart := max(insStart-3, 0)
	prot := buildLiteralProtectedMask(curSpans, r, curCaret, true)

	// Zero-overhead streaming fast path when the entire block/cell is a single span (no cross-span edits possible).
	if len(curSpans) == 1 {
		var sb strings.Builder
		sb.Grow(len(plain))
		matchedAny := false
		delta := 0
		if scanStart > 0 {
			for k := 0; k < scanStart; k++ {
				sb.WriteRune(r[k])
			}
		}
		for pos := scanStart; pos < curCaret; {
			matchedRule := false
			for _, rule := range smartTypoRules {
				fl := len(rule.from)
				if pos+fl <= curCaret && runesEqual(r[pos:pos+fl], rule.from) && !isEscapedAt(r, pos) {
					blocked := false
					for k := pos; k < pos+fl; k++ {
						if prot[k] {
							blocked = true
							break
						}
					}
					if !blocked {
						sb.WriteString(rule.toStr)
						delta += fl - len(rule.to)
						pos += fl
						matchedRule = true
						matchedAny = true
						break
					}
				}
			}
			if !matchedRule {
				sb.WriteRune(r[pos])
				pos++
			}
		}
		if !matchedAny {
			return curSpans, curCaret, false
		}
		for k := curCaret; k < len(r); k++ {
			sb.WriteRune(r[k])
		}
		sp := curSpans[0]
		sp.Text = sb.String()
		return []model.Span{sp}, curCaret - delta, true
	}

	var edits []batchRuneEdit
	for pos := scanStart; pos < curCaret; {
		matchedRule := false
		for _, rule := range smartTypoRules {
			fl := len(rule.from)
			if pos+fl <= curCaret && runesEqual(r[pos:pos+fl], rule.from) && !isEscapedAt(r, pos) {
				blocked := false
				for k := pos; k < pos+fl; k++ {
					if prot[k] {
						blocked = true
						break
					}
				}
				if !blocked {
					edits = append(edits, batchRuneEdit{start: pos, end: pos + fl, repl: rule.toStr})
					pos += fl
					matchedRule = true
					break
				}
			}
		}
		if !matchedRule {
			pos++
		}
	}
	if len(edits) == 0 {
		return curSpans, curCaret, false
	}
	delta := 0
	for _, e := range edits {
		delta += (e.end - e.start) - utf8.RuneCountInString(e.repl)
	}
	return applyBatchRuneEditsToSpans(curSpans, r, edits), curCaret - delta, true
}

type batchRuneEdit struct {
	start int
	end   int
	repl  string
}

// buildLiteralProtectedMask marks every rune index in [0, limit) that lies inside a protected
// span (MarkCode/MarkMath/Link), unclosed inline code/math delimiters, or (when protectURLs is
// true) URL tokens, in a single O(N) left-to-right pass.
func buildLiteralProtectedMask(spans []model.Span, r []rune, limit int, protectURLs bool) []bool {
	if limit > len(r) {
		limit = len(r)
	}
	prot := make([]bool, limit)
	pos := 0
	for _, sp := range spans {
		spLen := utf8.RuneCountInString(sp.Text)
		if pos >= limit {
			break
		}
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 || sp.Link != "" || sp.ImageSrc != "" {
			end := min(pos+spLen, limit)
			for i := pos; i < end; i++ {
				prot[i] = true
			}
		}
		pos += spLen
	}
	lineStart := 0
	var openBT rune
	var openMath rune
	for i := 0; i < limit; i++ {
		if r[i] == '\n' {
			openBT = 0
			openMath = 0
			lineStart = i + 1
			continue
		}
		if prot[i] {
			continue
		}
		ch := r[i]
		if openBT == 0 && openMath == 0 && isURLStartBoundary(r, i, lineStart) && hasURLSchemePrefixRunes(r, i, limit) {
			inParen := i > lineStart && (r[i-1] == '(' || r[i-1] == '（')
			closeParen := ')'
			if i > lineStart && r[i-1] == '（' {
				closeParen = '）'
			}
			urlEnd := scanURLTokenEnd(r, i, limit, inParen, closeParen)
			if protectURLs {
				for k := i; k < urlEnd; k++ {
					prot[k] = true
				}
			}
			i = urlEnd - 1
			continue
		}
		if openMath == 0 && (ch == '`' || ch == '｀') && !isEscapedAt(r, i) {
			if (i > lineStart && r[i-1] == ch) || (i+1 < limit && r[i+1] == ch) {
				if openBT != 0 {
					prot[i] = true
				}
				continue
			}
			if openBT == 0 {
				openBT = ch
				prot[i] = true
			} else if openBT == ch {
				prot[i] = true
				openBT = 0
			} else {
				prot[i] = true
			}
			continue
		}
		if openBT != 0 {
			prot[i] = true
			continue
		}
		if (ch == '$' || ch == '＄') && !isEscapedAt(r, i) {
			if (i > lineStart && r[i-1] == ch) || (i+1 < limit && r[i+1] == ch) {
				if openMath != 0 {
					prot[i] = true
				}
				continue
			}
			if openMath == 0 {
				if i+1 < limit && !unicode.IsSpace(r[i+1]) {
					openMath = ch
					prot[i] = true
				}
			} else if openMath == ch && i > lineStart && !unicode.IsSpace(r[i-1]) {
				prot[i] = true
				openMath = 0
			} else {
				prot[i] = true
			}
			continue
		}
		if openMath != 0 {
			prot[i] = true
		}
	}
	return prot
}

func applyBatchRuneEditsToSpans(spans []model.Span, allRunes []rune, edits []batchRuneEdit) []model.Span {
	if len(edits) == 0 {
		return spans
	}
	out := make([]model.Span, 0, len(spans))
	ei := 0
	spStart := 0
	for _, sp := range spans {
		spLen := utf8.RuneCountInString(sp.Text)
		spEnd := spStart + spLen
		if spLen == 0 {
			continue
		}
		for ei < len(edits) && edits[ei].end <= spStart {
			ei++
		}
		if ei >= len(edits) || edits[ei].start >= spEnd {
			out = append(out, sp)
			spStart = spEnd
			continue
		}
		var sb strings.Builder
		sb.Grow(len(sp.Text))
		cursor := spStart
		for ei < len(edits) && edits[ei].start < spEnd {
			e := edits[ei]
			for k := cursor; k < e.start; k++ {
				sb.WriteRune(allRunes[k])
			}
			if e.start >= spStart && e.start < spEnd && e.repl != "" {
				sb.WriteString(e.repl)
			}
			if e.end <= spEnd {
				cursor = max(cursor, e.end)
				ei++
			} else {
				cursor = spEnd
				break
			}
		}
		for k := cursor; k < spEnd; k++ {
			sb.WriteRune(allRunes[k])
		}
		if sb.Len() > 0 {
			seg := sp
			seg.Text = sb.String()
			out = append(out, seg)
		}
		spStart = spEnd
	}
	return normalizeSpans(out)
}

// --- :emoji: Shortcode Auto-Replacement ---

var emojiShortcodes = map[string]string{
	"+1": "👍", "thumbsup": "👍", "-1": "👎", "thumbsdown": "👎",
	"ok": "👌", "ok_hand": "👌", "clap": "👏", "pray": "🙏", "wave": "👋", "muscle": "💪",
	"smile": "😄", "grin": "😁", "joy": "😂", "laugh": "😂", "rofl": "🤣",
	"wink": "😉", "blush": "😊", "heart_eyes": "😍", "sunglasses": "😎", "cool": "😎",
	"thinking": "🤔", "neutral": "😐", "sweat_smile": "😅", "cry": "😢", "sob": "😭",
	"angry": "😠", "rage": "😡", "mindblown": "🤯", "party": "🥳", "shrug": "🤷",
	"heart": "❤️", "red_heart": "❤️", "broken_heart": "💔", "sparkling_heart": "💖",
	"fire": "🔥", "rocket": "🚀", "star": "⭐", "sparkles": "✨", "zap": "⚡", "boom": "💥",
	"100": "💯", "tada": "🎉", "confetti": "🎊", "trophy": "🏆", "medal": "🏅", "crown": "👑",
	"gem": "💎", "gift": "🎁", "balloon": "🎈",
	"check": "✅", "white_check_mark": "✅", "heavy_check_mark": "✔️",
	"x": "❌", "cross": "❌", "warning": "⚠️", "alert": "🚨", "stop": "🛑", "no_entry": "⛔",
	"question": "❓", "exclamation": "❗", "info": "ℹ️",
	"bulb": "💡", "idea": "💡", "memo": "📝", "note": "📝", "book": "📖", "books": "📚",
	"bookmark": "🔖", "pin": "📌", "pushpin": "📌", "paperclip": "📎", "link": "🔗",
	"calendar": "📅", "clock": "🕒", "hourglass": "⏳", "stopwatch": "⏱️", "alarm": "⏰",
	"bell": "🔔", "megaphone": "📣", "loudspeaker": "📢",
	"lock": "🔒", "unlock": "🔓", "key": "🔑", "shield": "🛡️",
	"bug": "🐛", "wrench": "🔧", "hammer": "🔨", "gear": "⚙️", "tools": "🛠️",
	"package": "📦", "folder": "📁", "file": "📄", "chart": "📊", "graph": "📈",
	"mag": "🔍", "search": "🔍", "computer": "💻", "laptop": "💻", "phone": "📱", "email": "📧",
	"globe": "🌐", "cloud": "☁️", "sun": "☀️", "moon": "🌙", "rainbow": "🌈", "umbrella": "☂️",
	"coffee": "☕", "tea": "🍵", "beer": "🍺", "pizza": "🍕", "cake": "🎂", "apple": "🍎",
	"cat": "🐱", "dog": "🐶", "fox": "🦊", "unicorn": "🦄", "bee": "🐝", "butterfly": "🦋",
	"tree": "🌳", "flower": "🌸", "rose": "🌹", "seedling": "🌱",
	"eye": "👁️", "eyes": "👀", "brain": "🧠", "ghost": "👻", "robot": "🤖", "alien": "👽",
	"point_right": "👉", "point_left": "👈", "point_up": "👆", "point_down": "👇",
	"arrow_right": "➡️", "arrow_left": "⬅️", "arrow_up": "⬆️", "arrow_down": "⬇️",
	"red_circle": "🔴", "orange_circle": "🟠", "yellow_circle": "🟡", "green_circle": "🟢", "blue_circle": "🔵", "purple_circle": "🟣",
}

func applyEmojiShortcodeOnSpans(spans []model.Span, caretRune int) ([]model.Span, int, bool) {
	plain := model.PlainTextOfSpans(spans)
	r := []rune(plain)
	n := len(r)
	if caretRune <= 0 || caretRune > n {
		caretRune = n
	}
	if caretRune < 3 || r[caretRune-1] != ':' || isEscapedAt(r, caretRune-1) {
		return spans, caretRune, false
	}
	if inBT, inMath, inURL := inUnclosedLiteralContext(spans, r, caretRune); inBT || inMath || inURL {
		return spans, caretRune, false
	}
	open := -1
	minStart := max(caretRune-26, 0)
	for i := caretRune - 2; i >= minStart; i-- {
		ch := r[i]
		if ch == ':' {
			if isEscapedAt(r, i) {
				return spans, caretRune, false
			}
			open = i
			break
		}
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '+' || ch == '-') {
			return spans, caretRune, false
		}
	}
	if open < 0 || caretRune-1 <= open+1 {
		return spans, caretRune, false
	}
	if open > 0 {
		prev := r[open-1]
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) {
			return spans, caretRune, false
		}
	}
	if spanProtectionAt(spans, open, caretRune) {
		return spans, caretRune, false
	}
	code := strings.ToLower(string(r[open+1 : caretRune-1]))
	emoji, ok := emojiShortcodes[code]
	if !ok {
		return spans, caretRune, false
	}
	next := replaceSpanRuneRange(spans, n, open, caretRune, emoji)
	return next, open + utf8.RuneCountInString(emoji), true
}

// --- Block-Level Shortcut Matchers ---

func leadingListIndent(rawPrefix string) int {
	spaces := 0
	levels := 0
	for _, ch := range rawPrefix {
		switch ch {
		case ' ':
			spaces++
		case '\t', '\u3000':
			levels++
		default:
			return min(levels+spaces/2, 8)
		}
	}
	return min(levels+spaces/2, 8)
}

func matchQuotePrefixDepth(rawPrefix string) (int, bool) {
	r := []rune(rawPrefix)
	if len(r) == 0 {
		return 0, false
	}
	if len(r) == 1 {
		switch r[0] {
		case '>', '＞', '》', '"', '“', '”':
			return 1, true
		default:
			return 0, false
		}
	}
	if len(r) <= 6 {
		for _, ch := range r {
			if ch != '>' && ch != '＞' && ch != '》' {
				return 0, false
			}
		}
		return len(r), true
	}
	return 0, false
}

func isTablePipeRune(ch rune) bool {
	return ch == '|' || ch == '｜'
}

func unescapeTableCellSpans(spans []model.Span) []model.Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]model.Span, 0, len(spans))
	for _, sp := range spans {
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 {
			out = append(out, sp)
			continue
		}
		if strings.Contains(sp.Text, `\|`) || strings.Contains(sp.Text, `\｜`) {
			txt := strings.ReplaceAll(sp.Text, `\|`, "|")
			txt = strings.ReplaceAll(txt, `\｜`, "｜")
			sp.Text = txt
		}
		out = append(out, sp)
	}
	return normalizeSpans(out)
}

func trimCellSpansWhitespace(spans []model.Span) []model.Span {
	plain := model.PlainTextOfSpans(spans)
	r := []rune(plain)
	lo := 0
	for lo < len(r) && unicode.IsSpace(r[lo]) {
		lo++
	}
	hi := len(r)
	for hi > lo && unicode.IsSpace(r[hi-1]) {
		hi--
	}
	if lo >= hi {
		return nil
	}
	return sliceSpans(spans, lo, hi)
}

func buildTablePipeProtectedMask(spans []model.Span, r []rune) []bool {
	n := len(r)
	prot := make([]bool, n)
	// 1. Protect already-formatted inline code, math, and link spans.
	pos := 0
	for _, sp := range spans {
		spLen := utf8.RuneCountInString(sp.Text)
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 || sp.Link != "" || sp.ImageSrc != "" {
			end := min(pos+spLen, n)
			for i := pos; i < end; i++ {
				prot[i] = true
			}
		}
		pos += spLen
	}
	// 2. Protect raw unformatted inline code (`...`), inline math ($...$), and links ([...](...))
	// in strict O(N) using lazy O(N) backtick/paren match tables and monotonic closer cursors.
	var btRuns map[int][]int
	var btHead map[int]int
	var parenMatch []int
	nextDollarPos, nextDollarLineEnd := -1, -1
	nextFWDollarPos, nextFWDollarLineEnd := -1, -1
	nextCloseBPos, nextCloseBLineEnd := -1, -1

	ensureBTRuns := func() {
		if btRuns != nil {
			return
		}
		btRuns = make(map[int][]int)
		btHead = make(map[int]int)
		for k := 0; k < n; {
			if r[k] == '`' || r[k] == '｀' {
				c := r[k]
				run := 0
				for k+run < n && r[k+run] == c {
					run++
				}
				key := run
				if c == '｀' {
					key = -run
				}
				btRuns[key] = append(btRuns[key], k)
				k += run
				continue
			}
			k++
		}
	}

	ensureParenMatch := func() {
		if parenMatch != nil {
			return
		}
		parenMatch = make([]int, n)
		for k := range parenMatch {
			parenMatch[k] = -1
		}
		var stack []int
		for k := 0; k < n; k++ {
			switch r[k] {
			case '\n':
				stack = stack[:0]
			case '(':
				stack = append(stack, k)
			case ')':
				if len(stack) > 0 {
					openIdx := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					parenMatch[openIdx] = k
				}
			}
		}
	}

	for i := 0; i < n; {
		if prot[i] {
			i++
			continue
		}
		ch := r[i]
		if (ch == '`' || ch == '｀') && !isEscapedAt(r, i) {
			run := 0
			for i+run < n && r[i+run] == ch {
				run++
			}
			ensureBTRuns()
			btKey := run
			if ch == '｀' {
				btKey = -run
			}
			positions := btRuns[btKey]
			h := btHead[btKey]
			for h < len(positions) && positions[h] <= i {
				h++
			}
			btHead[btKey] = h
			closeIdx := -1
			if h < len(positions) {
				closeIdx = positions[h]
			}
			if closeIdx > i+run {
				for k := i; k < closeIdx+run; k++ {
					prot[k] = true
				}
				i = closeIdx + run
				continue
			}
			i += run
			continue
		}
		if (ch == '$' || ch == '＄') && !isEscapedAt(r, i) {
			run := 0
			for i+run < n && r[i+run] == ch {
				run++
			}
			if run == 1 && i+1 < n && !unicode.IsSpace(r[i+1]) {
				cachedPos, cachedEnd := &nextDollarPos, &nextDollarLineEnd
				if ch == '＄' {
					cachedPos, cachedEnd = &nextFWDollarPos, &nextFWDollarLineEnd
				}
				if i >= *cachedEnd || (*cachedPos >= 0 && *cachedPos <= i) {
					found := -1
					j := i + 1
					for ; j < n; j++ {
						if r[j] == '\n' {
							break
						}
						if r[j] == ch && !isEscapedAt(r, j) {
							found = j
							break
						}
					}
					*cachedPos = found
					if found >= 0 {
						*cachedEnd = found + 1
					} else {
						*cachedEnd = j
					}
				}
				closeIdx := -1
				if j := *cachedPos; j > i+1 && !unicode.IsSpace(r[j-1]) && (j+1 >= n || (r[j+1] != ch && !(r[j+1] >= '0' && r[j+1] <= '9'))) {
					closeIdx = j
				}
				if closeIdx > i+1 {
					for k := i; k <= closeIdx; k++ {
						prot[k] = true
					}
					i = closeIdx + 1
					continue
				}
			}
			i += run
			continue
		}
		if ch == '[' && !isEscapedAt(r, i) {
			if i >= nextCloseBLineEnd || (nextCloseBPos >= 0 && nextCloseBPos <= i) {
				found := -1
				j := i + 1
				for ; j < n; j++ {
					if r[j] == '\n' {
						break
					}
					if r[j] == ']' && !isEscapedAt(r, j) {
						found = j
						break
					}
				}
				nextCloseBPos = found
				if found >= 0 {
					nextCloseBLineEnd = found + 1
				} else {
					nextCloseBLineEnd = j
				}
			}
			closeB := nextCloseBPos
			if closeB > i+1 && closeB+1 < n && r[closeB+1] == '(' {
				ensureParenMatch()
				closeP := parenMatch[closeB+1]
				if closeP > closeB+2 {
					for k := i; k <= closeP; k++ {
						prot[k] = true
					}
					i = closeP + 1
					continue
				}
			}
		}
		i++
	}
	return prot
}

// matchTableHeaderCells splits a pipe header line (e.g. `| **Col1** | a\|b |`) into rich TableCells,
// preserving existing inline spans, respecting escaped pipes `\|`, and never splitting inside code, math, or links.
func (s *Session) matchTableHeaderCells(spans []model.Span) ([]model.TableCell, bool) {
	plain := model.PlainTextOfSpans(spans)
	r := []rune(plain)
	lo := 0
	for lo < len(r) && unicode.IsSpace(r[lo]) {
		lo++
	}
	hi := len(r)
	for hi > lo && unicode.IsSpace(r[hi-1]) {
		hi--
	}
	if hi-lo < 3 {
		return nil, false
	}
	prot := buildTablePipeProtectedMask(spans, r)
	if !isTablePipeRune(r[lo]) || prot[lo] || isEscapedAt(r, lo) ||
		!isTablePipeRune(r[hi-1]) || prot[hi-1] || isEscapedAt(r, hi-1) {
		return nil, false
	}

	var pipePositions []int
	for i := lo; i < hi; i++ {
		if isTablePipeRune(r[i]) && !isEscapedAt(r, i) && !prot[i] {
			pipePositions = append(pipePositions, i)
		}
	}
	// Need at least 3 pipes for >= 2 columns (`| c1 | c2 |`).
	if len(pipePositions) < 3 || len(pipePositions)-1 > 16 {
		return nil, false
	}

	nCols := len(pipePositions) - 1
	cells := make([]model.TableCell, nCols)
	allSep := true
	for c := 0; c < nCols; c++ {
		segLo := pipePositions[c] + 1
		segHi := pipePositions[c+1]
		segSpans := sliceSpans(spans, segLo, segHi)
		segSpans = trimCellSpansWhitespace(unescapeTableCellSpans(segSpans))
		// If the cell is still plain unformatted text that contains Markdown syntax (e.g. from IME paste), parse inline spans.
		if len(segSpans) == 1 && segSpans[0].ImageSrc == "" && segSpans[0].Marks == 0 && segSpans[0].Link == "" && segSpans[0].FGColor == "" && segSpans[0].BGColor == "" {
			if looksLikeInlineMarkdown(segSpans[0].Text) {
				segSpans = s.parseInlineSpans(segSpans[0].Text)
			}
		}
		cells[c] = model.TableCell{Spans: segSpans}
		cellTxt := strings.TrimSpace(model.PlainTextOfSpans(segSpans))
		if cellTxt == "" {
			continue
		}
		for _, ch := range cellTxt {
			if ch != '-' && ch != ':' && ch != ' ' {
				allSep = false
				break
			}
		}
	}
	if allSep {
		return nil, false
	}
	return cells, true
}

func (s *Session) convertParagraphToImageBlock(idx int, alt, src, title string) {
	b := &s.doc.Blocks[idx]
	b.Kind = model.BlockImage
	b.ImageAlt = alt
	b.ImageSrc = src
	b.ImageTitle = title
	b.Spans = nil
	b.Dirty = true
	b.CleanSource = ""
	s.blockMenu = nil
	s.detachRichInput(b.ID)
	s.focusedBlock = idx
	s.selAnchor = Position{Block: idx, Offset: 0}
	s.selHead = s.selAnchor
	s.multiSelFocusPending = true
	s.focusPending = false
	s.selSyncPending = false
	s.markDirty()
}

func (s *Session) convertParagraphToTableBlock(idx int, headerRow []model.TableCell) {
	b := &s.doc.Blocks[idx]
	nCols := len(headerRow)
	bodyRow := make([]model.TableCell, nCols)
	aligns := make([]model.ColAlign, nCols)
	for i := range aligns {
		aligns[i] = model.ColAlignLeft
	}
	b.Kind = model.BlockTable
	b.Spans = nil
	b.Table = &model.TableData{
		Aligns: aligns,
		Rows:   [][]model.TableCell{headerRow, bodyRow},
	}
	b.Dirty = true
	b.CleanSource = ""
	s.blockMenu = nil
	s.detachRichInput(b.ID)
	bodyOff := tableCellStartOffset(b.Table, 1, 0)
	s.focusedBlock = idx
	s.tableActiveBlock = b.ID
	s.tableActiveRow = 1
	s.tableActiveCol = 0
	s.selAnchor = Position{Block: idx, Offset: bodyOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

// --- Auto-Wrap Selected Text on Punctuation / Format Key ---

var autoWrapPairs = map[string][2]string{
	"(": {"(", ")"},
	"[": {"[", "]"},
	"{": {"{", "}"},
	`"`: {`"`, `"`},
	"'": {"'", "'"},
	"（": {"（", "）"},
	"【": {"【", "】"},
	"「": {"「", "」"},
	"『": {"『", "』"},
	"《": {"《", "》"},
	"“": {"“", "”"},
	"‘": {"‘", "’"},
}

type autoWrapResult struct {
	spans   []model.Span
	newLo   int
	newHi   int
	handled bool
}

func tryAutoWrapSpans(spans []model.Span, lo, hi int, text string) autoWrapResult {
	if hi <= lo || utf8.RuneCountInString(text) != 1 {
		return autoWrapResult{}
	}
	plain := model.PlainTextOfSpans(spans)
	r := []rune(plain)
	n := len(r)
	if lo < 0 || hi > n {
		return autoWrapResult{}
	}

	switch text {
	case "`":
		hasAll := spansAllMatch(spans, lo, hi, func(sp model.Span) bool { return sp.Marks&model.MarkCode != 0 })
		next := applySpanTransform(spans, lo, hi, func(sp model.Span) model.Span {
			if hasAll {
				sp.Marks &^= model.MarkCode
			} else {
				sp.Marks = (sp.Marks &^ model.MarkMath) | model.MarkCode
			}
			return sp
		})
		return autoWrapResult{spans: next, newLo: lo, newHi: hi, handled: true}
	case "^":
		hasAll := spansAllMatch(spans, lo, hi, func(sp model.Span) bool { return sp.Marks&model.MarkSuperscript != 0 })
		next := applySpanTransform(spans, lo, hi, func(sp model.Span) model.Span {
			if hasAll {
				sp.Marks &^= model.MarkSuperscript
			} else {
				sp.Marks = (sp.Marks &^ model.MarkSubscript) | model.MarkSuperscript
			}
			return sp
		})
		return autoWrapResult{spans: next, newLo: lo, newHi: hi, handled: true}
	case "$":
		hasAll := spansAllMatch(spans, lo, hi, func(sp model.Span) bool { return sp.Marks&model.MarkMath != 0 })
		next := applySpanTransform(spans, lo, hi, func(sp model.Span) model.Span {
			if hasAll {
				sp.Marks &^= model.MarkMath
			} else {
				sp.Marks = (sp.Marks &^ model.MarkCode) | model.MarkMath
			}
			return sp
		})
		return autoWrapResult{spans: next, newLo: lo, newHi: hi, handled: true}
	case "*", "~", "=":
		if res, ok := wrapInsideSingleInlineImage(spans, lo, hi, text, text); ok {
			return res
		}
		if spanBoundarySplitsInlineImage(spans, lo, hi) {
			return autoWrapResult{}
		}
		ch := []rune(text)[0]
		if lo > 0 && hi < n && r[lo-1] == ch && r[hi] == ch {
			left := sliceSpans(spans, 0, lo-1)
			mid := sliceSpans(spans, lo, hi)
			right := sliceSpans(spans, hi+1, n)
			for i := range mid {
				switch ch {
				case '*':
					mid[i].Marks |= model.MarkBold
				case '~':
					mid[i].Marks |= model.MarkStrike
				case '=':
					mid[i].BGColor = "yellow"
				}
			}
			var merged []model.Span
			merged = append(merged, left...)
			merged = append(merged, mid...)
			merged = append(merged, right...)
			return autoWrapResult{spans: normalizeSpans(merged), newLo: lo - 1, newHi: hi - 1, handled: true}
		}
		return wrapSpansWithPair(spans, n, lo, hi, text, text)
	}

	if pair, ok := autoWrapPairs[text]; ok {
		return wrapSpansWithPair(spans, n, lo, hi, pair[0], pair[1])
	}
	return autoWrapResult{}
}

func wrapInsideSingleInlineImage(spans []model.Span, lo, hi int, openStr, closeStr string) (autoWrapResult, bool) {
	pos := 0
	for si, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if sp.ImageSrc != "" && lo >= s0 && hi <= s1 && !(lo == s0 && hi == s1) {
			spR := []rune(sp.Text)
			nextText := string(spR[:lo-s0]) + openStr + string(spR[lo-s0:hi-s0]) + closeStr + string(spR[hi-s0:])
			cp := sp
			cp.Text = nextText
			cp.ImageAltEmpty = false
			out := make([]model.Span, 0, len(spans))
			out = append(out, spans[:si]...)
			out = append(out, cp)
			out = append(out, spans[si+1:]...)
			openLen := utf8.RuneCountInString(openStr)
			return autoWrapResult{
				spans:   normalizeSpans(out),
				newLo:   lo + openLen,
				newHi:   hi + openLen,
				handled: true,
			}, true
		}
	}
	return autoWrapResult{}, false
}

func spanBoundarySplitsInlineImage(spans []model.Span, lo, hi int) bool {
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if sp.ImageSrc == "" {
			continue
		}
		if (lo > s0 && lo < s1) || (hi > s0 && hi < s1) {
			return true
		}
	}
	return false
}

// sliceSpansForClipboard slices spans over [startRune, endRune) for copy/cut.
// Any inline image span that is only partially covered by the selection is downgraded
// to plain alt text so cutting/copying a substring of alt never duplicates the image.
func sliceSpansForClipboard(spans []model.Span, startRune, endRune int) []model.Span {
	if startRune >= endRune {
		return nil
	}
	var out []model.Span
	pos := 0
	for _, sp := range spans {
		r := []rune(sp.Text)
		n := len(r)
		s0, s1 := pos, pos+n
		pos = s1
		if s1 <= startRune || s0 >= endRune {
			continue
		}
		lo := max(startRune-s0, 0)
		hi := min(endRune-s0, n)
		if hi > lo {
			cp := sp
			cp.Text = string(r[lo:hi])
			if cp.ImageSrc != "" && (lo > 0 || hi < n) {
				cp = model.Span{Text: cp.Text}
			}
			out = append(out, cp)
		}
	}
	return normalizeSpans(out)
}

// replaceSpanRange replaces rune range [lo, hi) in spans with insertSpans while
// keeping any single inline-image span indivisible (editing its alt in place
// instead of duplicating the image across both halves).
func replaceSpanRange(spans []model.Span, lo, hi int, insertSpans []model.Span) ([]model.Span, int) {
	total := len([]rune(model.PlainTextOfSpans(spans)))
	lo = clampInt(lo, 0, total)
	hi = clampInt(hi, lo, total)
	insLen := len([]rune(model.PlainTextOfSpans(insertSpans)))

	pos := 0
	for si, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if sp.ImageSrc != "" && lo >= s0 && hi <= s1 {
			isBoundaryInsert := len(insertSpans) > 0 && lo == hi && (lo == s0 || lo == s1)
			isFullReplace := lo == s0 && hi == s1
			if !isBoundaryInsert && !isFullReplace {
				hasInsertedImage := false
				for _, isp := range insertSpans {
					if isp.ImageSrc != "" {
						hasInsertedImage = true
						break
					}
				}
				spR := []rune(sp.Text)
				out := make([]model.Span, 0, len(spans)+len(insertSpans))
				out = append(out, spans[:si]...)
				if !hasInsertedImage {
					plainIns := model.PlainTextOfSpans(insertSpans)
					nextText := string(spR[:lo-s0]) + plainIns + string(spR[hi-s0:])
					if nextText != "" {
						cp := sp
						cp.Text = nextText
						cp.ImageAltEmpty = false
						out = append(out, cp)
					}
					out = append(out, spans[si+1:]...)
					return normalizeSpans(out), lo + insLen
				}
				nextText := string(spR[:lo-s0]) + string(spR[hi-s0:])
				afterImg := s0
				if nextText != "" {
					cp := sp
					cp.Text = nextText
					cp.ImageAltEmpty = false
					out = append(out, cp)
					afterImg = s0 + utf8.RuneCountInString(nextText)
				}
				out = append(out, insertSpans...)
				out = append(out, spans[si+1:]...)
				return normalizeSpans(out), afterImg + insLen
			}
			break
		}
	}

	var merged []model.Span
	merged = append(merged, sliceSpans(spans, 0, lo)...)
	merged = append(merged, insertSpans...)
	merged = append(merged, sliceSpans(spans, hi, total)...)
	return normalizeSpans(merged), lo + insLen
}

func snapSplitOutsideInlineImage(spans []model.Span, off int) int {
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if sp.ImageSrc != "" && off > s0 && off < s1 {
			return s1
		}
	}
	return off
}

func wrapSpansWithPair(spans []model.Span, total, lo, hi int, openStr, closeStr string) autoWrapResult {
	if res, ok := wrapInsideSingleInlineImage(spans, lo, hi, openStr, closeStr); ok {
		return res
	}
	if spanBoundarySplitsInlineImage(spans, lo, hi) {
		return autoWrapResult{}
	}
	left := sliceSpans(spans, 0, lo)
	mid := sliceSpans(spans, lo, hi)
	right := sliceSpans(spans, hi, total)
	openSpan := model.Span{Text: openStr}
	closeSpan := model.Span{Text: closeStr}
	if len(mid) > 0 {
		openSpan.Marks = mid[0].Marks &^ (model.MarkCode | model.MarkMath)
		closeSpan.Marks = mid[len(mid)-1].Marks &^ (model.MarkCode | model.MarkMath)
	}
	var merged []model.Span
	merged = append(merged, left...)
	merged = append(merged, openSpan)
	merged = append(merged, mid...)
	merged = append(merged, closeSpan)
	merged = append(merged, right...)
	openLen := utf8.RuneCountInString(openStr)
	return autoWrapResult{
		spans:   normalizeSpans(merged),
		newLo:   lo + openLen,
		newHi:   hi + openLen,
		handled: true,
	}
}

func (s *Session) handleBlockTextInput(idx int, text string, anchor, caret int) bool {
	if s.readOnly || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) || anchor == caret {
		return false
	}
	b := &s.doc.Blocks[idx]
	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		return false
	}
	lo, hi := min(anchor, caret), max(anchor, caret)
	res := tryAutoWrapSpans(b.Spans, lo, hi, text)
	if !res.handled {
		return false
	}
	s.pushUndo()
	b.Spans = res.spans
	b.Dirty = true
	b.CleanSource = ""
	s.clearAutoConvertState()
	s.selAnchor = Position{Block: idx, Offset: res.newLo}
	s.selHead = Position{Block: idx, Offset: res.newHi}
	if st, ok := s.richInputs[b.ID]; ok && st != nil {
		st.Sync(b.PlainText(), res.newLo, res.newHi)
	}
	s.markDirty()
	return true
}

func (s *Session) handleTableCellTextInput(blockIdx, rIdx, cIdx int, text string, anchor, caret int) bool {
	if s.readOnly || s.doc == nil || blockIdx < 0 || blockIdx >= len(s.doc.Blocks) || anchor == caret {
		return false
	}
	b := &s.doc.Blocks[blockIdx]
	if b.Kind != model.BlockTable || b.Table == nil || rIdx < 0 || rIdx >= len(b.Table.Rows) || cIdx < 0 || cIdx >= len(b.Table.Rows[rIdx]) {
		return false
	}
	cell := &b.Table.Rows[rIdx][cIdx]
	lo, hi := min(anchor, caret), max(anchor, caret)
	res := tryAutoWrapSpans(cell.Spans, lo, hi, text)
	if !res.handled {
		return false
	}
	s.pushUndo()
	cell.Spans = res.spans
	b.Dirty = true
	b.CleanSource = ""
	s.clearAutoConvertState()
	base := tableCellStartOffset(b.Table, rIdx, cIdx)
	s.selAnchor = Position{Block: blockIdx, Offset: base + res.newLo}
	s.selHead = Position{Block: blockIdx, Offset: base + res.newHi}
	key := tableCellKey{BlockID: b.ID, Row: rIdx, Col: cIdx}
	if st, ok := s.tableInputs[key]; ok && st != nil {
		st.Sync(cell.PlainText(), res.newLo, res.newHi)
	}
	s.markDirty()
	return true
}

// --- Unified Table Cell Edit Pipeline ---

func (s *Session) applyTableCellTextEdit(blockIdx, rIdx, cIdx int, newText string, anchor, caret int, codeEdge appUi.RichCodeEdge) {
	if s.doc == nil || blockIdx < 0 || blockIdx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[blockIdx]
	if b.Kind != model.BlockTable || b.Table == nil || rIdx < 0 || rIdx >= len(b.Table.Rows) || cIdx < 0 || cIdx >= len(b.Table.Rows[rIdx]) {
		return
	}
	cell := &b.Table.Rows[rIdx][cIdx]
	oldDraft := cell.PlainText()
	if newText == oldDraft {
		// Still sync collapsed caret (e.g. IME committing identical text over a selection via UnmarkText).
		newBase := tableCellStartOffset(b.Table, rIdx, cIdx)
		s.selAnchor = Position{Block: blockIdx, Offset: newBase + anchor}
		s.selHead = Position{Block: blockIdx, Offset: newBase + caret}
		return
	}
	key := tableCellKey{BlockID: b.ID, Row: rIdx, Col: cIdx}
	insStart, insLen := computeEditInsertedRange(oldDraft, newText, caret)

	// Currency rollback (e.g. `$5&$10` typed char-by-char: digit immediately after `$5&$`).
	if rolledCaret, rolled := s.tryRollbackCurrencyMathOnCell(blockIdx, rIdx, cIdx, key, oldDraft, newText, caret); rolled {
		newBase := tableCellStartOffset(b.Table, rIdx, cIdx)
		s.selAnchor = Position{Block: blockIdx, Offset: newBase + rolledCaret}
		s.selHead = s.selAnchor
		s.markDirty()
		if st, ok := s.tableInputs[key]; ok && st != nil {
			st.Sync(cell.PlainText(), rolledCaret, rolledCaret)
		}
		return
	}

	clearMask, clearBG := s.consumeNextInsertGuard(b.ID, true, key, insStart, insLen)
	useRightBias := s.consumeRightStyleBias(b.ID, true, key, insStart, insLen)
	s.lastAutoConvert = nil
	s.postEditCodeEdge = appUi.RichCodeEdgeNone

	rDelta := len([]rune(newText)) - len([]rune(oldDraft))
	if rDelta < 0 {
		rDelta = -rDelta
	}
	batchCont := s.suppressUndo
	if st, ok := s.tableInputs[key]; ok && st != nil && st.InBatchContinuation() {
		batchCont = true
	}
	if !batchCont {
		if rDelta <= 4 && !strings.Contains(newText, "\n") && !strings.Contains(oldDraft, "\n") {
			s.pushCoalescedTextUndo(fmt.Sprintf("cell:%d:%d:%d", b.ID, rIdx, cIdx))
		} else {
			s.pushUndo()
		}
	}

	cell.Spans = spliceSpansByTextEditWithEdge(cell.Spans, oldDraft, newText, caret, codeEdge)
	if useRightBias {
		cell.Spans = inheritRightSpanStyleOnInsert(cell.Spans, insStart, insLen)
	}
	if insLen > 0 && (clearMask != 0 || clearBG) {
		cell.Spans = s.applyNextInsertStyleClear(cell.Spans, insStart, insLen, clearMask, clearBG)
	}

	beforeConvertSpans := cloneSpansSlice(cell.Spans)
	beforeConvertCaret := clampInt(caret, 0, len([]rune(newText)))
	converted := false
	finalCaret := beforeConvertCaret
	finalEdge := appUi.RichCodeEdgeNone
	var nextClearMask model.Mark
	var nextClearBG bool
	isMathConvert := false

	if insLen > 0 {
		if nextSpans, nextCaret, ok := applyEmojiShortcodeOnSpans(cell.Spans, finalCaret); ok {
			cell.Spans = nextSpans
			finalCaret = nextCaret
			converted = true
		}
		if nextSpans, nextCaret, ok := applySmartTypographyOnSpans(cell.Spans, finalCaret, insStart, insLen); ok {
			cell.Spans = nextSpans
			finalCaret = nextCaret
			converted = true
		}
		if res := applyInlineInputRules(cell.Spans, finalCaret, insLen, false); res.changed {
			cell.Spans = res.spans
			finalCaret = res.caret
			finalEdge = res.codeEdge
			nextClearMask = res.clearMask
			nextClearBG = res.clearBG
			isMathConvert = res.isMath
			converted = true
		}
	}

	b.Dirty = true
	b.CleanSource = ""
	newBase := tableCellStartOffset(b.Table, rIdx, cIdx)
	if converted {
		s.selAnchor = Position{Block: blockIdx, Offset: newBase + finalCaret}
		s.selHead = s.selAnchor
		s.postEditCodeEdge = finalEdge
	} else {
		s.selAnchor = Position{Block: blockIdx, Offset: newBase + anchor}
		s.selHead = Position{Block: blockIdx, Offset: newBase + caret}
	}
	s.markDirty()

	if converted {
		s.recordCellAutoConvert(key, beforeConvertSpans, beforeConvertCaret, cell.Spans, finalCaret, isMathConvert)
		if nextClearMask != 0 || nextClearBG {
			s.nextInsertGuard = &nextInsertStyleGuard{
				blockID:   b.ID,
				isCell:    true,
				cellKey:   key,
				docRev:    s.docRev,
				caretPos:  finalCaret,
				clearMask: nextClearMask,
				clearBG:   nextClearBG,
			}
		}
		if st, ok := s.tableInputs[key]; ok && st != nil {
			st.SyncWithCodeEdge(cell.PlainText(), finalCaret, finalCaret, finalEdge)
		}
	}
}
