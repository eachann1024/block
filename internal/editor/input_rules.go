package editor

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/model"
	appUi "block/internal/ui"
)

type autoConvertRecord struct {
	blockID    model.BlockID
	isCell     bool
	cellKey    tableCellKey
	docRev     uint64
	beforeBlk  model.Block
	beforeCell []model.Span
	beforePos  int
	afterPos   int
	afterText  string
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
}

func (s *Session) recordBlockAutoConvert(blkBefore model.Block, beforePos int, blkAfter *model.Block, afterPos int) {
	if blkAfter == nil {
		return
	}
	s.lastAutoConvert = &autoConvertRecord{
		blockID:   blkAfter.ID,
		docRev:    s.docRev,
		beforeBlk: blkBefore.Clone(),
		beforePos: beforePos,
		afterPos:  afterPos,
		afterText: blkAfter.PlainText(),
	}
}

func (s *Session) recordCellAutoConvert(key tableCellKey, beforeSpans []model.Span, beforePos int, afterSpans []model.Span, afterPos int) {
	s.lastAutoConvert = &autoConvertRecord{
		blockID:    key.BlockID,
		isCell:     true,
		cellKey:    key,
		docRev:     s.docRev,
		beforeCell: cloneSpansSlice(beforeSpans),
		beforePos:  beforePos,
		afterPos:   afterPos,
		afterText:  model.PlainTextOfSpans(afterSpans),
	}
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
	s.pushUndo()
	restored := rec.beforeBlk.Clone()
	restored.Dirty = true
	restored.CleanSource = ""
	s.doc.Blocks[idx] = restored
	s.lastAutoConvert = nil
	s.nextInsertGuard = nil
	s.focusedBlock = idx
	s.selAnchor = Position{Block: idx, Offset: rec.beforePos}
	s.selHead = s.selAnchor
	s.selSyncPending = true
	s.focusPending = true
	if st, ok := s.richInputs[b.ID]; ok && st != nil {
		st.Sync(restored.PlainText(), rec.beforePos, rec.beforePos)
	}
	s.markDirty()
	return true
}

func (s *Session) tryUndoCellAutoConvertBackspace(blockIdx, rIdx, cIdx int, curText string, caret int) bool {
	rec := s.lastAutoConvert
	if rec == nil || !rec.isCell || s.doc == nil || blockIdx < 0 || blockIdx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[blockIdx]
	if b.Kind != model.BlockTable || b.Table == nil || rIdx < 0 || rIdx >= len(b.Table.Rows) || cIdx < 0 || cIdx >= len(b.Table.Rows[rIdx]) {
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
	s.lastAutoConvert = nil
	s.nextInsertGuard = nil
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

func (s *Session) consumeNextInsertGuard(blockID model.BlockID, isCell bool, cellKey tableCellKey, oldText, newText string, caretHint int) (model.Mark, bool) {
	g := s.nextInsertGuard
	s.nextInsertGuard = nil
	if g == nil || g.docRev != s.docRev || g.blockID != blockID || g.isCell != isCell || (isCell && g.cellKey != cellKey) {
		return 0, false
	}
	oldR := []rune(oldText)
	newR := []rune(newText)
	inserted := len(newR) - len(oldR)
	if inserted <= 0 {
		return 0, false
	}
	insStart := caretHint - inserted
	if caretHint < 0 {
		insStart = g.caretPos
	}
	if insStart != g.caretPos {
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

// --- Span Protection Helpers ---

func spanProtectionAt(spans []model.Span, startRune, endRune int) bool {
	if endRune <= startRune {
		return false
	}
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if s1 <= startRune || s0 >= endRune {
			continue
		}
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 || sp.Link != "" {
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
		if s1 <= startRune || s0 >= endRune {
			continue
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

// inUnclosedLiteralContext detects whether caret `pos` lies inside an unclosed inline
// code backtick (` / ｀), unclosed inline math ($ / ＄), or an active bare URL token,
// so smart typography / emoji / other inline formatting rules never corrupt code, LaTeX, or URLs while typing.
func inUnclosedLiteralContext(spans []model.Span, r []rune, pos int) (inBacktick, inMath, inURL bool) {
	if pos <= 0 || pos > len(r) {
		return false, false, false
	}
	// Find current line start.
	lineStart := pos
	for lineStart > 0 && r[lineStart-1] != '\n' {
		lineStart--
	}
	// Check active non-whitespace token for bare URL prefix.
	tokStart := pos
	for tokStart > lineStart && !unicode.IsSpace(r[tokStart-1]) {
		tokStart--
	}
	if tokStart < pos {
		tokLower := strings.ToLower(string(r[tokStart:pos]))
		if strings.HasPrefix(tokLower, "http://") || strings.HasPrefix(tokLower, "https://") || strings.HasPrefix(tokLower, "www.") {
			inURL = true
		}
	}
	// Scan line from lineStart to pos skipping already-formatted code/math/link spans.
	var openBT rune
	var openMath rune
	for i := lineStart; i < pos; i++ {
		if spanProtectionAt(spans, i, i+1) {
			continue
		}
		ch := r[i]
		if (ch == '`' || ch == '｀') && !isEscapedAt(r, i) {
			// Skip multi-backtick fences (``` / ``).
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
	return openBT != 0, openMath != 0, inURL
}

// --- Inline Markdown Real-Time Conversion ---

type inlineRuleResult struct {
	spans     []model.Span
	caret     int
	changed   bool
	codeEdge  appUi.RichCodeEdge
	clearMask model.Mark
	clearBG   bool
	toImage   bool
	imageAlt  string
	imageSrc  string
}

func applyInlineInputRules(spans []model.Span, caretRune int, allowImageBlock bool) inlineRuleResult {
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
	if res := tryInlineConstructEndingAt(spans, r, caretRune, allowImageBlock); res.changed || res.toImage {
		return res
	}

	// 2. Trailing space / fullwidth space triggers:
	//    - Bare URL auto-link (`https://... ` / `www.... `)
	//    - Fallback for inline constructs committed with a trailing space (e.g. IME commit "`code` ")
	if caretRune > 0 && (r[caretRune-1] == ' ' || r[caretRune-1] == '\u3000') {
		tokenEnd := caretRune - 1
		if res := tryInlineConstructEndingAt(spans, r, tokenEnd, allowImageBlock); res.changed || res.toImage {
			if res.toImage {
				return res
			}
			// Keep the trailing space unstyled outside the newly formatted span.
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
			return res
		}
		if res := tryBareURLAutolinkBefore(spans, r, tokenEnd); res.changed {
			res.caret = caretRune
			return res
		}
	}

	return inlineRuleResult{spans: spans, caret: caretRune}
}

func tryInlineConstructEndingAt(spans []model.Span, r []rune, end int, allowImageBlock bool) inlineRuleResult {
	if end < 2 {
		return inlineRuleResult{spans: spans, caret: end}
	}
	last := r[end-1]

	// Standalone image `![alt](url)` or inline link `[label](url)` / `[label]（https://...）` ending with `)` / `）`.
	if last == ')' || last == '）' {
		inBT, inMath, _ := inUnclosedLiteralContext(spans, r, end-1)
		if inBT || inMath {
			return inlineRuleResult{spans: spans, caret: end}
		}
		if allowImageBlock && last == ')' {
			trimmed := strings.TrimSpace(string(r[:end]))
			rest := strings.TrimSpace(string(r[end:]))
			if rest == "" {
				if alt, src, ok := matchStandaloneImageLine(trimmed); ok {
					return inlineRuleResult{toImage: true, imageAlt: alt, imageSrc: src}
				}
			}
		}
		if res := tryInlineLinkEndingAt(spans, r, end); res.changed {
			return res
		}
		return inlineRuleResult{spans: spans, caret: end}
	}

	// Inline code `` `code` `` or fullwidth `` ｀code｀ `` (guard against ``` fenced code block and escaped backticks).
	if last == '`' || last == '｀' {
		if isEscapedAt(r, end-1) || (end >= 2 && r[end-2] == last) {
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

	// Inline math `$E=mc^2$` or fullwidth `＄E=mc^2＄` (guard against `$$`, `$5 and $10`, and inside unclosed backtick).
	if last == '$' || last == '＄' {
		inBT, _, _ := inUnclosedLiteralContext(spans, r, end-1)
		if inBT || isEscapedAt(r, end-1) || (end >= 2 && (r[end-2] == last || unicode.IsSpace(r[end-2]))) {
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
			return replaceInlineDelimited(spans, len(r), open, end, 1, func(sp model.Span) model.Span {
				sp.Marks = (sp.Marks &^ model.MarkCode) | model.MarkMath
				return sp
			}, appUi.RichCodeEdgeAfter, model.MarkCode|model.MarkMath, false)
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
			// If opener actually has 3+ chars (e.g. `***bold**`), wait for the 3rd closing char.
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
			// If opener is part of `**` or `***` (e.g. user is typing `**bold**` and just typed `**bold*`), do not hijack!
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

func replaceInlineDelimited(spans []model.Span, totalRunes, open, end, delimLen int, fn func(model.Span) model.Span, codeEdge appUi.RichCodeEdge, clearMask model.Mark, clearBG bool) inlineRuleResult {
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
	// Accept bare domain or relative file path (must contain `.` or `/`, and no CJK prose like `[重要](请先备份)`).
	if strings.ContainsAny(u, "./") {
		for _, ch := range u {
			if ch > 0x7F && unicode.Is(unicode.Han, ch) {
				return strings.ContainsRune(u, '/')
			}
		}
		return !strings.HasPrefix(u, ".") && !strings.HasSuffix(u, ".")
	}
	return false
}

func tryInlineLinkEndingAt(spans []model.Span, r []rune, end int) inlineRuleResult {
	// Match `[label](url)` or `[label]（https://...）` ending at `end`, allowing balanced `(...)` inside URL.
	closeParen := r[end-1]
	openChar := '('
	fullwidthParen := closeParen == '）'
	if fullwidthParen {
		openChar = '（'
	}
	openParen := -1
	depth := 0
	for i := end - 2; i >= 1; i-- {
		if r[i] == '\n' {
			return inlineRuleResult{spans: spans, caret: end}
		}
		if r[i] == closeParen {
			depth++
			continue
		}
		if r[i] == openChar {
			if depth > 0 {
				depth--
				continue
			}
			if r[i-1] == ']' {
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
	openBracket := -1
	for i := closeBracket - 1; i >= 0; i-- {
		if r[i] == '\n' || r[i] == ']' {
			return inlineRuleResult{spans: spans, caret: end}
		}
		if r[i] == '[' {
			if isEscapedAt(r, i) {
				return inlineRuleResult{spans: spans, caret: end}
			}
			openBracket = i
			break
		}
	}
	if openBracket < 0 || (openBracket > 0 && r[openBracket-1] == '!') {
		return inlineRuleResult{spans: spans, caret: end}
	}
	labelLen := closeBracket - (openBracket + 1)
	rawURL := strings.TrimSpace(string(r[openParen+1 : end-1]))
	if labelLen <= 0 || !isPlausibleInlineLinkTarget(rawURL, fullwidthParen) {
		return inlineRuleResult{spans: spans, caret: end}
	}
	if spanCodeOrMathAt(spans, openBracket, end) {
		return inlineRuleResult{spans: spans, caret: end}
	}
	url := normalizeInlineLinkURL(rawURL)
	left := sliceSpans(spans, 0, openBracket)
	mid := sliceSpans(spans, openBracket+1, closeBracket)
	right := sliceSpans(spans, end, len(r))
	for i := range mid {
		mid[i].Link = url
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

func matchStandaloneImageLine(s string) (alt, src string, ok bool) {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "![") || !strings.HasSuffix(t, ")") {
		return "", "", false
	}
	mid := strings.Index(t, "](")
	if mid < 2 {
		return "", "", false
	}
	alt = strings.TrimSpace(t[2:mid])
	src = strings.TrimSpace(t[mid+2 : len(t)-1])
	if !isPlausibleInlineLinkTarget(src, false) {
		return "", "", false
	}
	return alt, src, true
}

func tryBareURLAutolinkBefore(spans []model.Span, r []rune, tokenEnd int) inlineRuleResult {
	if tokenEnd <= 0 {
		return inlineRuleResult{spans: spans, caret: tokenEnd}
	}
	start := tokenEnd
	for start > 0 && !unicode.IsSpace(r[start-1]) {
		start--
	}
	// Strip leading wrapper punctuation like `(https://...` or `「https://...`.
	for start < tokenEnd {
		switch r[start] {
		case '(', '[', '{', '<', '"', '\'', '（', '【', '「', '《', '“':
			start++
			continue
		}
		break
	}
	if start >= tokenEnd {
		return inlineRuleResult{spans: spans, caret: tokenEnd}
	}
	end := tokenEnd
	// Strip trailing punctuation while keeping balanced `)` inside URLs like Wikipedia links.
	for end > start {
		ch := r[end-1]
		if ch == ')' {
			opens, closes := 0, 0
			for i := start; i < end; i++ {
				if r[i] == '(' {
					opens++
				} else if r[i] == ')' {
					closes++
				}
			}
			if closes > opens {
				end--
				continue
			}
			break
		}
		switch ch {
		case '.', ',', ';', ':', '!', '?', '\'', '"', ']', '}', '>', '。', '，', '；', '：', '！', '？', '）', '】', '》', '」', '』', '”':
			end--
			continue
		}
		break
	}
	if end <= start {
		return inlineRuleResult{spans: spans, caret: tokenEnd}
	}
	cand := string(r[start:end])
	lower := strings.ToLower(cand)
	var url string
	switch {
	case strings.HasPrefix(lower, "https://") && len(cand) > len("https://"):
		url = cand
	case strings.HasPrefix(lower, "http://") && len(cand) > len("http://"):
		url = cand
	case strings.HasPrefix(lower, "www.") && len(cand) > len("www.") && strings.ContainsRune(cand[4:], '.'):
		url = "https://" + cand
	default:
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
	from []rune
	to   []rune
}

var smartTypoRules = []smartTypoRule{
	{[]rune("<->"), []rune("↔")},
	{[]rune("<=>"), []rune("⇔")},
	{[]rune("←>"), []rune("↔")},
	{[]rune("⇐>"), []rune("⇔")},
	{[]rune("≤>"), []rune("⇔")},
	{[]rune("->"), []rune("→")},
	{[]rune("-》"), []rune("→")},
	{[]rune("—>"), []rune("→")},
	{[]rune("—》"), []rune("→")},
	{[]rune("=>"), []rune("⇒")},
	{[]rune("=》"), []rune("⇒")},
	{[]rune("<-"), []rune("←")},
	{[]rune("《-"), []rune("←")},
	{[]rune("!="), []rune("≠")},
	{[]rune("<="), []rune("≤")},
	{[]rune(">="), []rune("≥")},
	{[]rune("+-"), []rune("±")},
	{[]rune("..."), []rune("…")},
	{[]rune("(c)"), []rune("©")},
	{[]rune("(C)"), []rune("©")},
	{[]rune("(tm)"), []rune("™")},
	{[]rune("(TM)"), []rune("™")},
	{[]rune("(r)"), []rune("®")},
	{[]rune("(R)"), []rune("®")},
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
	if inBT, inMath, inURL := inUnclosedLiteralContext(spans, r, caretRune); inBT || inMath || inURL {
		return spans, caretRune, false
	}

	// Em-dash `-- ` / `--　` only when preceded by non-dash, non-empty content (never breaks `---` divider).
	if caretRune >= 4 && (r[caretRune-1] == ' ' || r[caretRune-1] == '\u3000') &&
		r[caretRune-2] == '-' && r[caretRune-3] == '-' && r[caretRune-4] != '-' && !isEscapedAt(r, caretRune-3) {
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

	curSpans := spans
	curCaret := caretRune
	changed := false

	// Single-character typing: ONLY match rules ending right at caret so previously undone raw tokens (via Backspace) stay intact.
	if insLen == 1 {
		for _, rule := range smartTypoRules {
			fl := len(rule.from)
			if curCaret >= fl && runesEqual(r[curCaret-fl:curCaret], rule.from) {
				start := curCaret - fl
				if !isEscapedAt(r, start) && !spanProtectionAt(curSpans, start, curCaret) {
					curSpans = replaceSpanRuneRange(curSpans, n, start, curCaret, string(rule.to))
					return curSpans, start + len(rule.to), true
				}
			}
		}
		return spans, caretRune, false
	}

	// Multi-character commit (IME / whole-string Type): scan only the newly inserted window [scanStart, curCaret).
	scanStart := max(insStart-3, 0)
	for {
		curR := []rune(model.PlainTextOfSpans(curSpans))
		curN := len(curR)
		if curCaret > curN {
			curCaret = curN
		}
		matched := false
		for pos := scanStart; pos < curCaret && !matched; pos++ {
			for _, rule := range smartTypoRules {
				fl := len(rule.from)
				if pos+fl <= curCaret && runesEqual(curR[pos:pos+fl], rule.from) {
					if !isEscapedAt(curR, pos) && !spanProtectionAt(curSpans, pos, pos+fl) {
						if inBT, inMath, inURL := inUnclosedLiteralContext(curSpans, curR, pos+fl); !inBT && !inMath && !inURL {
							curSpans = replaceSpanRuneRange(curSpans, curN, pos, pos+fl, string(rule.to))
							curCaret -= fl - len(rule.to)
							changed = true
							matched = true
							break
						}
					}
				}
			}
		}
		if !matched {
			break
		}
	}
	return curSpans, curCaret, changed
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
	if caretRune < 3 || r[caretRune-1] != ':' {
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
	// Require boundary before opening `:` so URLs or timestamps (e.g. `12:30:00`) never match.
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
		case '>', '＞', '》', '"', '“', '”', '|', '｜':
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

func matchTableHeaderLine(text string) ([]string, bool) {
	t := strings.TrimSpace(text)
	t = strings.ReplaceAll(t, "｜", "|")
	if len(t) < 3 || !strings.HasPrefix(t, "|") || !strings.HasSuffix(t, "|") {
		return nil, false
	}
	inner := t[1 : len(t)-1]
	parts := strings.Split(inner, "|")
	if len(parts) < 2 || len(parts) > 16 {
		return nil, false
	}
	allSep := true
	cols := make([]string, len(parts))
	for i, p := range parts {
		cell := strings.TrimSpace(p)
		cols[i] = cell
		if cell == "" {
			continue
		}
		for _, ch := range cell {
			if ch != '-' && ch != ':' && ch != ' ' {
				allSep = false
				break
			}
		}
	}
	if allSep {
		return nil, false
	}
	return cols, true
}

func (s *Session) convertParagraphToImageBlock(idx int, alt, src string) {
	b := &s.doc.Blocks[idx]
	b.Kind = model.BlockImage
	b.ImageAlt = alt
	b.ImageSrc = src
	b.Spans = nil
	b.Dirty = true
	b.CleanSource = ""
	s.blockMenu = nil
	delete(s.richInputs, b.ID)
	s.focusedBlock = idx
	s.selAnchor = Position{Block: idx, Offset: 0}
	s.selHead = s.selAnchor
	s.multiSelFocusPending = true
	s.focusPending = false
	s.selSyncPending = false
	s.markDirty()
}

func (s *Session) convertParagraphToTableBlock(idx int, cols []string) {
	b := &s.doc.Blocks[idx]
	nCols := len(cols)
	headerRow := make([]model.TableCell, nCols)
	bodyRow := make([]model.TableCell, nCols)
	aligns := make([]model.ColAlign, nCols)
	for i, text := range cols {
		aligns[i] = model.ColAlignLeft
		if text != "" {
			headerRow[i] = model.TableCell{Spans: s.parseInlineSpans(text)}
		}
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
	delete(s.richInputs, b.ID)
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

	// Direct single-key inline mark toggles on selection.
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
		ch := []rune(text)[0]
		// Second consecutive press on already wrapped selection (e.g. `*sel*` + `*` -> bold `sel`).
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

func wrapSpansWithPair(spans []model.Span, total, lo, hi int, openStr, closeStr string) autoWrapResult {
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
		return
	}
	key := tableCellKey{BlockID: b.ID, Row: rIdx, Col: cIdx}
	clearMask, clearBG := s.consumeNextInsertGuard(b.ID, true, key, oldDraft, newText, caret)
	s.lastAutoConvert = nil
	s.postEditCodeEdge = appUi.RichCodeEdgeNone

	rDelta := len([]rune(newText)) - len([]rune(oldDraft))
	if rDelta < 0 {
		rDelta = -rDelta
	}
	if rDelta <= 4 && !strings.Contains(newText, "\n") && !strings.Contains(oldDraft, "\n") {
		s.pushCoalescedTextUndo(fmt.Sprintf("cell:%d:%d:%d", b.ID, rIdx, cIdx))
	} else {
		s.pushUndo()
	}

	cell.Spans = spliceSpansByTextEditWithEdge(cell.Spans, oldDraft, newText, caret, codeEdge)
	if ins := len([]rune(newText)) - len([]rune(oldDraft)); ins > 0 && (clearMask != 0 || clearBG) {
		insStart := caret - ins
		if insStart >= 0 {
			cell.Spans = s.applyNextInsertStyleClear(cell.Spans, insStart, ins, clearMask, clearBG)
		}
	}

	beforeConvertSpans := cloneSpansSlice(cell.Spans)
	beforeConvertCaret := clampInt(caret, 0, len([]rune(newText)))
	insLen := len([]rune(newText)) - len([]rune(oldDraft))
	insStart := beforeConvertCaret - max(insLen, 0)
	if insStart < 0 {
		insStart = 0
	}
	converted := false
	finalCaret := beforeConvertCaret
	finalEdge := appUi.RichCodeEdgeNone
	var nextClearMask model.Mark
	var nextClearBG bool

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
		if res := applyInlineInputRules(cell.Spans, finalCaret, false); res.changed {
			cell.Spans = res.spans
			finalCaret = res.caret
			finalEdge = res.codeEdge
			nextClearMask = res.clearMask
			nextClearBG = res.clearBG
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
		s.recordCellAutoConvert(key, beforeConvertSpans, beforeConvertCaret, cell.Spans, finalCaret)
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
