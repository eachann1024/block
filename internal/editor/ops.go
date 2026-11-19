package editor

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"block/internal/codepreview"
	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// --- Selection Snapshots & Markdown Copy ---

type toolbarCaps struct {
	RichText bool
	Bold     bool
	Link     bool
}

func (s *Session) selectionSupportsToolbar() bool {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return false
	}
	a, b := s.orderedSelection()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		if a.Block != b.Block && idx == b.Block && b.Offset == 0 && s.blockSelectionLen(idx) > 0 {
			break
		}
		k := s.doc.Blocks[idx].Kind
		if k != model.BlockImage && k != model.BlockDivider && k != model.BlockTable {
			return true
		}
	}
	return false
}

func (s *Session) toolbarCapabilities() toolbarCaps {
	if !s.isMarkdownNote() || !s.hasNonEmptySelection() || s.doc == nil {
		return toolbarCaps{}
	}
	a, b := s.orderedSelection()
	caps := toolbarCaps{RichText: true, Bold: true}
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		if a.Block != b.Block && idx == b.Block && b.Offset == 0 {
			break
		}
		blk := &s.doc.Blocks[idx]
		rich := blk.Kind != model.BlockCode && blk.Kind != model.BlockRawHTML && blk.Kind != model.BlockImage && blk.Kind != model.BlockDivider && blk.Kind != model.BlockTable
		caps.RichText = caps.RichText && rich
		caps.Bold = caps.Bold && rich && !blk.Kind.IsHeading()
	}
	caps.Link = caps.RichText
	return caps
}

func (s *Session) SelectedDoc() *model.Doc {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return nil
	}
	a, b := s.orderedSelection()
	if a.Block < 0 || b.Block >= len(s.doc.Blocks) {
		return nil
	}

	if a.Block == b.Block {
		blk := &s.doc.Blocks[a.Block]
		rlen := s.blockSelectionLen(a.Block)
		whole := a.Offset == 0 && b.Offset >= rlen

		if blk.Kind == model.BlockTable && blk.Table != nil && !whole {
			if row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
				spans := sliceSpans(blk.Table.Rows[row][col].Spans, relLo, relHi)
				return &model.Doc{
					Blocks:       []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Spans: spans, Dirty: true}},
					TrailingLine: "\n",
				}
			}
			if sliced, ok := sliceTableSelection(blk, a.Offset, b.Offset); ok {
				return &model.Doc{
					Blocks:       []model.Block{sliced},
					TrailingLine: "\n",
				}
			}
		}

		if whole && (blk.Kind == model.BlockTable || blk.Kind == model.BlockImage || blk.Kind == model.BlockDivider || s.isPreviewOnlyCodeBlock(a.Block)) {
			cp := blk.Clone()
			cp.Dirty = true
			cp.CleanSource = ""
			return &model.Doc{Blocks: []model.Block{cp}, TrailingLine: "\n"}
		}

		part := s.sliceBlockForSelection(a.Block, a.Offset, b.Offset, false)
		return &model.Doc{Blocks: []model.Block{part}, TrailingLine: "\n"}
	}

	// Single trailing newline boundary between adjacent blocks.
	firstLen := s.blockSelectionLen(a.Block)
	if a.Block+1 == b.Block && a.Offset == firstLen && b.Offset == 0 {
		return &model.Doc{
			Blocks:       []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}},
			TrailingLine: "\n",
		}
	}

	var blocks []model.Block
	for idx := a.Block; idx <= b.Block; idx++ {
		cur := &s.doc.Blocks[idx]
		curLen := s.blockSelectionLen(idx)
		if idx == b.Block && a.Block != b.Block && b.Offset == 0 && curLen > 0 {
			continue
		}
		if idx == a.Block && a.Block != b.Block && a.Offset >= curLen && curLen > 0 {
			continue
		}
		if cur.Kind == model.BlockImage {
			cp := cur.Clone()
			cp.ID = model.NewBlockID()
			cp.Dirty = true
			cp.CleanSource = ""
			blocks = append(blocks, cp)
			continue
		}
		lo, hi := s.selectionSpanRange(idx, a, b)
		if cur.Kind == model.BlockTable && cur.Table != nil && !(lo == 0 && hi == curLen) {
			if lo < hi {
				cellMD := serializeTableMultiCellMarkdown(cur.Table, lo, hi, s.codec)
				if strings.Trim(cellMD, "\t\n ") != "" {
					for _, rowLine := range strings.Split(cellMD, "\n") {
						lineText := strings.ReplaceAll(rowLine, "\t", "  ")
						if strings.TrimSpace(lineText) == "" {
							continue
						}
						blocks = append(blocks, model.Block{
							ID:    model.NewBlockID(),
							Kind:  model.BlockParagraph,
							Spans: s.parseInlineSpans(lineText),
							Dirty: true,
						})
					}
				}
			}
			continue
		}
		if lo < hi || curLen == 0 {
			blocks = append(blocks, s.sliceBlockForSelection(idx, lo, hi, true))
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	normalizeCopiedBlocks(blocks)
	return &model.Doc{Blocks: blocks, TrailingLine: "\n"}
}

func (s *Session) SelectedMarkdown() string {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return ""
	}
	a, b := s.orderedSelection()
	if a.Block == b.Block {
		blk := &s.doc.Blocks[a.Block]
		rlen := s.blockSelectionLen(a.Block)
		whole := a.Offset == 0 && b.Offset >= rlen

		if blk.Kind == model.BlockTable && blk.Table != nil && !whole {
			if row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
				spans := sliceSpans(blk.Table.Rows[row][col].Spans, relLo, relHi)
				return serializeInlineBlockMarkdown(spans, s.codec)
			}
			// Partial multi-cell table slice preserves cell inline formatting across tabs/newlines.
			return serializeTableMultiCellMarkdown(blk.Table, a.Offset, b.Offset, s.codec)
		}

		if whole && (blk.Kind == model.BlockTable || blk.Kind == model.BlockImage || blk.Kind == model.BlockDivider || s.isPreviewOnlyCodeBlock(a.Block)) {
			cp := blk.Clone()
			cp.ID = model.NewBlockID()
			cp.Dirty = true
			cp.CleanSource = ""
			single := []model.Block{cp}
			normalizeCopiedBlocks(single)
			return strings.TrimSuffix(s.serializeBlocks(single), "\n")
		}

		// Single-block text selections carry only inline formatting (not heading/list container).
		if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML {
			runes := []rune(blk.Code)
			lo := clampInt(a.Offset, 0, len(runes))
			hi := clampInt(b.Offset, lo, len(runes))
			return string(runes[lo:hi])
		}
		spans := sliceSpans(blk.Spans, a.Offset, b.Offset)
		return serializeInlineBlockMarkdown(spans, s.codec)
	}

	firstLen := s.blockSelectionLen(a.Block)
	if a.Block+1 == b.Block && a.Offset == firstLen && b.Offset == 0 {
		return "\n"
	}

	doc := s.SelectedDoc()
	if doc == nil {
		return ""
	}
	return strings.TrimSuffix(s.serializeBlocks(doc.Blocks), "\n")
}

func serializeInlineBlockMarkdown(spans []model.Span, codec model.MarkdownCodec) string {
	if len(spans) == 0 {
		return ""
	}
	// Split on explicit '\n' so inline lines don't collapse.
	rawText := model.PlainTextOfSpans(spans)
	if rawText == "" {
		return ""
	}
	if strings.TrimSpace(rawText) == "" {
		return rawText
	}
	if !strings.Contains(rawText, "\n") {
		blk := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Spans: spans, Dirty: true}
		if codec != nil {
			md := codec.Serialize(&model.Doc{Blocks: []model.Block{blk}, TrailingLine: "\n"})
			return strings.TrimSuffix(md, "\n")
		}
		return serializeSpansFallback(spans)
	}
	lines := strings.Split(rawText, "\n")
	parts := make([]string, 0, len(lines))
	pos := 0
	for _, ln := range lines {
		lnLen := len([]rune(ln))
		sub := sliceSpans(spans, pos, pos+lnLen)
		pos += lnLen + 1
		parts = append(parts, serializeInlineBlockMarkdown(sub, codec))
	}
	return strings.Join(parts, "\n")
}

func sanitizeTableCellTSV(s string) string {
	if !strings.ContainsAny(s, "\t\r\n") {
		return s
	}
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ").Replace(s)
}

func serializeTableMultiCellMarkdown(tbl *model.TableData, startRune, endRune int, codec model.MarkdownCodec) string {
	if tbl == nil || startRune >= endRune {
		return ""
	}
	var rowLines []string
	pos := 0
	for rIdx, r := range tbl.Rows {
		var cellParts []string
		rowTouched := false
		for cIdx, cell := range r {
			clen := len([]rune(cell.PlainText()))
			cStart, cEnd := pos, pos+clen
			if clen > 0 {
				ovLo := max(startRune, cStart) - cStart
				ovHi := min(endRune, cEnd) - cStart
				if ovLo < ovHi {
					rowTouched = true
					sub := sliceSpans(cell.Spans, ovLo, ovHi)
					cellParts = append(cellParts, sanitizeTableCellTSV(serializeInlineBlockMarkdown(sub, codec)))
				}
			} else if startRune <= cStart && cStart <= endRune {
				rowTouched = true
				cellParts = append(cellParts, "")
			}
			pos = cEnd
			if cIdx+1 < len(r) {
				pos++ // '\t'
			}
		}
		if rowTouched {
			rowLines = append(rowLines, strings.Join(cellParts, "\t"))
		}
		if rIdx+1 < len(tbl.Rows) {
			pos++ // '\n'
		}
	}
	return strings.Join(rowLines, "\n")
}

func sliceTableSelection(blk *model.Block, startRune, endRune int) (model.Block, bool) {
	if blk == nil || blk.Table == nil || startRune >= endRune {
		return model.Block{}, false
	}
	tbl := blk.Table
	rMin, rMax := len(tbl.Rows), -1
	cMin, cMax := 1<<30, -1
	pos := 0
	type overlap struct {
		r, c, lo, hi int
	}
	var hits []overlap
	for rIdx, r := range tbl.Rows {
		for cIdx, cell := range r {
			clen := len([]rune(cell.PlainText()))
			cStart, cEnd := pos, pos+clen
			if clen > 0 {
				ovLo := max(startRune, cStart) - cStart
				ovHi := min(endRune, cEnd) - cStart
				if ovLo < ovHi {
					rMin = min(rMin, rIdx)
					rMax = max(rMax, rIdx)
					cMin = min(cMin, cIdx)
					cMax = max(cMax, cIdx)
					hits = append(hits, overlap{r: rIdx, c: cIdx, lo: ovLo, hi: ovHi})
				}
			} else if startRune <= cStart && cStart <= endRune {
				rMin = min(rMin, rIdx)
				rMax = max(rMax, rIdx)
				cMin = min(cMin, cIdx)
				cMax = max(cMax, cIdx)
				hits = append(hits, overlap{r: rIdx, c: cIdx, lo: 0, hi: 0})
			}
			pos = cEnd
			if cIdx+1 < len(r) {
				pos++
			}
		}
		if rIdx+1 < len(tbl.Rows) {
			pos++
		}
	}
	if len(hits) == 0 || rMax < rMin || cMax < cMin {
		return model.Block{}, false
	}
	rows := make([][]model.TableCell, rMax-rMin+1)
	for r := range rows {
		rows[r] = make([]model.TableCell, cMax-cMin+1)
	}
	for _, h := range hits {
		cell := tbl.Rows[h.r][h.c]
		cell.Spans = sliceSpans(cell.Spans, h.lo, h.hi)
		rows[h.r-rMin][h.c-cMin] = cell
	}
	var aligns []model.ColAlign
	for c := cMin; c <= cMax; c++ {
		if c < len(tbl.Aligns) {
			aligns = append(aligns, tbl.Aligns[c])
		} else {
			aligns = append(aligns, model.ColAlignNone)
		}
	}
	return model.Block{
		ID:    model.NewBlockID(),
		Kind:  model.BlockTable,
		Table: &model.TableData{Aligns: aligns, Rows: rows, NoHeaderRow: tbl.NoHeaderRow || rMin > 0, HeaderCol: tbl.HeaderCol && cMin == 0},
		Dirty: true,
	}, true
}

func (s *Session) sliceBlockForSelection(idx, lo, hi int, preserveContainer bool) model.Block {
	blk := &s.doc.Blocks[idx]
	rlen := len([]rune(blk.PlainText()))
	if lo == 0 && hi == rlen && preserveContainer {
		cp := blk.Clone()
		cp.ID = model.NewBlockID()
		cp.Dirty = true
		cp.CleanSource = ""
		if cp.Kind == model.BlockNumbered {
			cp.ListIndex = s.numberedListIndex(idx)
		}
		return cp
	}
	if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML {
		runes := []rune(blk.Code)
		s0 := clampInt(lo, 0, len(runes))
		s1 := clampInt(hi, s0, len(runes))
		if preserveContainer {
			cp := blk.Clone()
			cp.ID = model.NewBlockID()
			cp.Code = string(runes[s0:s1])
			cp.Dirty = true
			cp.CleanSource = ""
			return cp
		}
		return model.Block{
			ID:    model.NewBlockID(),
			Kind:  model.BlockParagraph,
			Spans: []model.Span{{Text: string(runes[s0:s1])}},
			Dirty: true,
		}
	}
	if blk.Kind == model.BlockTable || blk.Kind == model.BlockImage || blk.Kind == model.BlockDivider {
		runes := []rune(blk.PlainText())
		s0 := clampInt(lo, 0, len(runes))
		s1 := clampInt(hi, s0, len(runes))
		return model.Block{
			ID:    model.NewBlockID(),
			Kind:  model.BlockParagraph,
			Spans: []model.Span{{Text: string(runes[s0:s1])}},
			Dirty: true,
		}
	}
	spans := sliceSpans(blk.Spans, lo, hi)
	if !preserveContainer {
		return model.Block{
			ID:    model.NewBlockID(),
			Kind:  model.BlockParagraph,
			Spans: spans,
			Dirty: true,
		}
	}
	cp := blk.Clone()
	cp.ID = model.NewBlockID()
	cp.Spans = spans
	cp.Dirty = true
	cp.CleanSource = ""
	if cp.Kind == model.BlockNumbered {
		cp.ListIndex = s.numberedListIndex(idx)
	}
	return cp
}

func normalizeCopiedBlocks(blocks []model.Block) {
	type ancestor struct {
		orig int
		next int
	}
	var ancestors []ancestor
	for i := range blocks {
		b := &blocks[i]
		isList := b.Kind.IsList()
		orig := b.Indent
		for len(ancestors) > 0 && ancestors[len(ancestors)-1].orig >= orig {
			ancestors = ancestors[:len(ancestors)-1]
		}
		if len(ancestors) > 0 {
			top := ancestors[len(ancestors)-1]
			b.Indent = max(orig-(top.orig-top.next), 0)
			if b.Kind == model.BlockDivider || b.Kind == model.BlockImage {
				b.Indent = min(b.Indent, top.next+3)
			}
		} else if orig > 0 {
			b.Indent = 0
		}
		if isList {
			ancestors = append(ancestors, ancestor{orig: orig, next: b.Indent})
		}
	}
}

// --- Markdown Serialization & Paste ---

func (s *Session) Markdown() string {
	if s.doc == nil {
		return ""
	}
	if s.codec != nil {
		return s.codec.Serialize(s.doc)
	}
	return fallbackSerialize(s.doc)
}

func fallbackSerialize(doc *model.Doc) string {
	if doc == nil || len(doc.Blocks) == 0 {
		return ""
	}
	var lines []string
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		if !b.Dirty && b.CleanSource != "" {
			lines = append(lines, b.CleanSource)
			continue
		}
		lines = append(lines, serializeBlockFallback(b))
	}
	out := strings.Join(lines, "\n\n")
	if doc.TrailingLine != "" && !strings.HasSuffix(out, doc.TrailingLine) {
		out += doc.TrailingLine
	}
	return out
}

func serializeSpansFallback(spans []model.Span) string {
	if len(spans) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, sp := range spans {
		t := sp.Text
		if t == "" {
			continue
		}
		if sp.Marks&model.MarkCode != 0 {
			t = "`" + t + "`"
		}
		if sp.Marks&model.MarkBold != 0 {
			t = "**" + t + "**"
		}
		if sp.Marks&model.MarkItalic != 0 {
			t = "*" + t + "*"
		}
		if sp.Marks&model.MarkStrike != 0 {
			t = "~~" + t + "~~"
		}
		if sp.Marks&model.MarkUnderline != 0 {
			t = "<u>" + t + "</u>"
		}
		if sp.Link != "" {
			t = fmt.Sprintf("[%s](%s)", t, sp.Link)
		}
		if sp.FGColor != "" || sp.BGColor != "" {
			t = fmt.Sprintf(`<span data-thinkly-text-color="%s" data-thinkly-background-color="%s">%s</span>`, sp.FGColor, sp.BGColor, t)
		}
		sb.WriteString(t)
	}
	return sb.String()
}

func serializeBlockFallback(b *model.Block) string {
	indent := strings.Repeat(" ", max(b.Indent, 0))
	text := serializeSpansFallback(b.Spans)
	switch b.Kind {
	case model.BlockHeading1:
		return "# " + text
	case model.BlockHeading2:
		return "## " + text
	case model.BlockHeading3:
		return "### " + text
	case model.BlockHeading4:
		return "#### " + text
	case model.BlockHeading5:
		return "##### " + text
	case model.BlockHeading6:
		return "###### " + text
	case model.BlockBullet:
		return indent + "- " + text
	case model.BlockNumbered:
		n := b.ListIndex
		if n <= 0 {
			n = 1
		}
		return fmt.Sprintf("%s%d. %s", indent, n, text)
	case model.BlockTodo:
		mark := " "
		if b.Checked {
			mark = "x"
		}
		return fmt.Sprintf("%s- [%s] %s", indent, mark, text)
	case model.BlockQuote:
		return "> " + strings.ReplaceAll(text, "\n", "\n> ")
	case model.BlockCallout:
		kind := b.Callout
		if kind == "" {
			kind = model.CalloutNote
		}
		return fmt.Sprintf("> [!%s]\n> %s", string(kind), strings.ReplaceAll(text, "\n", "\n> "))
	case model.BlockCode:
		return fmt.Sprintf("```%s\n%s\n```", b.Language, b.Code)
	case model.BlockDivider:
		return "---"
	case model.BlockImage:
		return fmt.Sprintf("![%s](%s)", b.ImageAlt, b.ImageSrc)
	case model.BlockTable:
		if b.Table == nil || len(b.Table.Rows) == 0 {
			return ""
		}
		var rows []string
		for r, row := range b.Table.Rows {
			var cells []string
			for _, c := range row {
				cells = append(cells, serializeSpansFallback(c.Spans))
			}
			rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
			if r == 0 {
				var sep []string
				for col := range row {
					align := model.ColAlignNone
					if col < len(b.Table.Aligns) {
						align = b.Table.Aligns[col]
					}
					switch align {
					case model.ColAlignCenter:
						sep = append(sep, ":---:")
					case model.ColAlignRight:
						sep = append(sep, "---:")
					case model.ColAlignLeft:
						sep = append(sep, ":---")
					default:
						sep = append(sep, "---")
					}
				}
				rows = append(rows, "| "+strings.Join(sep, " | ")+" |")
			}
		}
		return strings.Join(rows, "\n")
	case model.BlockRawHTML:
		return b.Code
	default:
		return text
	}
}

func normalizeTablePasteRaw(raw string) string {
	if raw == "" {
		return ""
	}
	cleaned := strings.TrimPrefix(raw, "\uFEFF")
	return strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"\u200B", "",
		"\u200C", "",
		"\u200D", "",
		"\uFEFF", "",
		"\u00A0", " ",
	).Replace(cleaned)
}

func (s *Session) isTablePasteTarget() bool {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	if s.hasMultiCellTableSelection() && !s.hasWholeAtomicSelection() {
		return true
	}
	if s.hasMultiBlockSelection() || s.hasWholeAtomicSelection() {
		return false
	}
	idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	return s.doc.Blocks[idx].Kind == model.BlockTable && s.doc.Blocks[idx].Table != nil
}

func htmlTableRowCountMatches(htmlRaw, mdStr string) bool {
	norm := normalizeTablePasteRaw(mdStr)
	if !strings.HasSuffix(norm, "\n") {
		return false
	}
	lower := strings.ToLower(htmlRaw)
	if !strings.Contains(lower, "<table") {
		return false
	}
	trCount := strings.Count(lower, "<tr")
	expectedRows := strings.Count(norm, "\n") + 1
	return trCount > 1 && trCount == expectedRows
}

func (s *Session) pasteTableWithTrailingEmptyRow(raw string) {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	norm := normalizeTablePasteRaw(raw)
	if norm == "" {
		return
	}
	if s.hasMultiCellTableSelection() && !s.hasWholeAtomicSelection() {
		a, b := s.orderedSelection()
		idx := a.Block
		if idx < 0 || idx >= len(s.doc.Blocks) || s.doc.Blocks[idx].Table == nil {
			return
		}
		s.pushUndo()
		blk := &s.doc.Blocks[idx]
		clearTableRange(blk.Table, a.Offset, b.Offset, "")
		s.focusedBlock = idx
		s.tableActiveBlock = blk.ID
		s.tableActiveRow = 0
		s.tableActiveCol = 0
		if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, a.Offset); ok {
			s.tableActiveRow = row
			s.tableActiveCol = col
		}
		s.selAnchor = Position{Block: idx, Offset: a.Offset}
		s.selHead = s.selAnchor
		s.pasteIntoTableBlockOpts(idx, norm, false, true)
		return
	}
	idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	if s.doc.Blocks[idx].Kind == model.BlockTable && s.doc.Blocks[idx].Table != nil {
		s.pushUndo()
		s.firstLineDeleteArmed = false
		s.pasteIntoTableBlockOpts(idx, norm, false, true)
	}
}

func (s *Session) PasteText(raw string) {
	if len(raw) > media.MaxPasteBytes || s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	idx := s.focusedBlock
	if idx < 0 || idx >= len(s.doc.Blocks) {
		idx = 0
	}

	// Code / RawHTML block pastes raw text literally at caret (dedenting common leading indent on multi-line code).
	if !s.hasMultiBlockSelection() && !s.hasWholeAtomicSelection() {
		curBlk := &s.doc.Blocks[idx]
		if curBlk.Kind == model.BlockCode || curBlk.Kind == model.BlockRawHTML {
			codeNorm := strings.TrimPrefix(raw, "\uFEFF")
			codeNorm = strings.ReplaceAll(strings.ReplaceAll(codeNorm, "\r\n", "\n"), "\r", "\n")
			if codeNorm == "" {
				return
			}
			if curBlk.Kind == model.BlockCode && curBlk.Language != "math" && strings.Contains(codeNorm, "\n") && s.isCaretAtLineStartInBlock(idx) {
				codeNorm = dedentCodeSnippet(codeNorm)
			}
			s.pushUndo()
			s.firstLineDeleteArmed = false
			s.replaceCurrentSelectionInBlock(idx, codeNorm)
			return
		}
	}

	if s.tryPasteURLOverSelection(raw) {
		return
	}

	cleanRaw := normalizeTablePasteRaw(raw)
	norm := cleanRaw
	if s.isTablePasteTarget() {
		norm = cleanRaw
	} else if cleanRaw != "" && strings.Trim(cleanRaw, " \t") == "" && !strings.Contains(cleanRaw, "\n") {
		norm = cleanRaw
	} else if s.codec != nil {
		norm = s.codec.NormalizePaste(raw)
	}
	if norm == "" {
		return
	}
	if s.pasteOverMultiSelection(norm) {
		return
	}

	b := &s.doc.Blocks[idx]
	s.pushUndo()
	s.firstLineDeleteArmed = false

	// Table block pastes TSV grid across rows/cols or inline spans into active cell.
	if b.Kind == model.BlockTable && b.Table != nil {
		s.pasteIntoTableBlock(idx, norm)
		return
	}

	// Empty paragraph receiving TSV text converts directly into a table block.
	if b.Kind == model.BlockParagraph && b.PlainText() == "" && strings.Contains(norm, "\t") {
		if tbl := s.buildTableFromTSV(norm); tbl != nil {
			b.Kind = model.BlockTable
			b.Table = tbl
			b.Spans = nil
			b.Dirty = true
			b.CleanSource = ""
			s.tableActiveBlock = b.ID
			s.tableActiveRow = 0
			s.tableActiveCol = 0
			s.focusPending = true
			s.markDirty()
			return
		}
	}

	// Single-line plain text without markdown block/inline markers inserts inline at selection.
	if !strings.Contains(norm, "\n") && !looksLikeMarkdownBlock(norm) && !looksLikeInlineMarkdown(norm) && b.Kind != model.BlockDivider && b.Kind != model.BlockImage {
		s.replaceCurrentSelectionInBlock(idx, norm)
		return
	}

	var parsed *model.Doc
	if s.codec != nil {
		parsed = s.codec.Parse(norm)
	} else {
		parsed = fallbackParsePaste(norm)
	}
	if parsed == nil || len(parsed.Blocks) == 0 {
		if b.Kind != model.BlockDivider && b.Kind != model.BlockImage {
			s.replaceCurrentSelectionInBlock(idx, norm)
		}
		return
	}

	// Collapse blank separator blocks so pasting never spawns empty paragraph blocks (Notion / Feishu rule).
	filtered := make([]model.Block, 0, len(parsed.Blocks))
	for _, pb := range parsed.Blocks {
		if pb.Kind == model.BlockParagraph && pb.Indent == 0 && len(pb.Spans) == 0 {
			continue
		}
		if pb.ID == 0 {
			pb.ID = model.NewBlockID()
		}
		pb.Dirty = true
		pb.CleanSource = ""
		filtered = append(filtered, pb)
	}
	if len(filtered) == 0 {
		return
	}

	if len(filtered) == 1 && filtered[0].Kind == model.BlockParagraph && b.Kind != model.BlockDivider && b.Kind != model.BlockImage {
		s.replaceSelectionWithSpansInBlock(idx, filtered[0].Spans)
		return
	}

	s.splicePastedBlocksAtSelection(idx, filtered)
}

// PastePlainText inserts unformatted literal text (Cmd/Ctrl+Shift+V) without parsing Markdown, HTML, or URLs.
func (s *Session) PastePlainText(raw string) {
	if len(raw) > media.MaxPasteBytes || s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 || raw == "" {
		return
	}
	clean := strings.TrimPrefix(raw, "\uFEFF")
	clean = strings.ReplaceAll(strings.ReplaceAll(clean, "\r\n", "\n"), "\r", "\n")
	if clean == "" {
		return
	}
	if s.hasMultiBlockSelection() || s.hasWholeAtomicSelection() || s.hasMultiCellTableSelection() {
		s.ReplaceSelectionWithText(clean)
		return
	}
	idx := s.focusedBlock
	if idx < 0 || idx >= len(s.doc.Blocks) {
		idx = 0
	}
	b := &s.doc.Blocks[idx]
	s.pushUndo()
	s.firstLineDeleteArmed = false

	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		s.replaceCurrentSelectionInBlock(idx, clean)
		return
	}
	if b.Kind == model.BlockTable && b.Table != nil {
		if !strings.ContainsAny(clean, "\t\n") {
			s.spliceSpansIntoActiveTableCell(idx, []model.Span{{Text: clean}})
			return
		}
		s.pasteIntoTableBlockOpts(idx, clean, true, false)
		return
	}
	if !strings.Contains(clean, "\n") && b.Kind != model.BlockDivider && b.Kind != model.BlockImage {
		s.replaceSelectionWithSpansInBlock(idx, []model.Span{{Text: clean}})
		return
	}

	var plainBlocks []model.Block
	for _, line := range strings.Split(clean, "\n") {
		plainBlocks = append(plainBlocks, model.Block{
			ID:    model.NewBlockID(),
			Kind:  model.BlockParagraph,
			Spans: []model.Span{{Text: line}},
			Dirty: true,
		})
	}
	s.splicePastedBlocksAtSelection(idx, plainBlocks)
}

func (s *Session) splicePastedBlocksAtSelection(idx int, pasted []model.Block) {
	b := &s.doc.Blocks[idx]
	if b.Kind == model.BlockDivider || b.Kind == model.BlockImage {
		s.spliceBlocks(idx+1, idx+1, pasted)
		s.focusedBlock = idx + len(pasted)
		endLen := len([]rune(s.doc.Blocks[s.focusedBlock].PlainText()))
		s.selAnchor = Position{Block: s.focusedBlock, Offset: endLen}
		s.selHead = s.selAnchor
		s.focusPending = true
		s.selSyncPending = true
		s.markDirty()
		return
	}

	a, endPos := s.orderedSelection()
	curText := []rune(b.PlainText())
	lo, hi := len(curText), len(curText)
	if a.Block == idx && endPos.Block == idx {
		lo = clampInt(a.Offset, 0, len(curText))
		hi = clampInt(endPos.Offset, lo, len(curText))
	}

	inheritContainer := func(blocks []model.Block) []model.Block {
		if b.Indent == 0 && b.QuoteDepth == 0 {
			return blocks
		}
		out := make([]model.Block, len(blocks))
		copy(out, blocks)
		for i := range out {
			out[i].Indent += b.Indent
			out[i].QuoteDepth += b.QuoteDepth
		}
		return out
	}

	var inserted []model.Block
	focusRel := 0
	caretOff := 0

	switch {
	// 1. Empty block or entire block's text is selected.
	case len(curText) == 0 || (lo == 0 && hi == len(curText)):
		if b.Kind != model.BlockParagraph && pasted[0].Kind == model.BlockParagraph {
			first := b.Clone()
			first.Spans = normalizeSpans(pasted[0].Spans)
			first.Dirty = true
			first.CleanSource = ""
			inserted = append(inserted, first)
			if len(pasted) > 1 {
				inserted = append(inserted, inheritContainer(pasted[1:])...)
			}
		} else {
			inserted = inheritContainer(pasted)
		}
		focusRel = len(inserted) - 1
		caretOff = len([]rune(inserted[focusRel].PlainText()))

	// 2. Caret at column 0 of a non-Paragraph block (Heading / List / Quote / Callout): keep original block intact below.
	case lo == 0 && hi == 0 && b.Kind != model.BlockParagraph:
		inserted = append(inserted, inheritContainer(pasted)...)
		focusRel = len(inserted) - 1
		caretOff = len([]rune(inserted[focusRel].PlainText()))
		orig := b.Clone()
		orig.ID = model.NewBlockID()
		orig.Dirty = true
		orig.CleanSource = ""
		inserted = append(inserted, orig)

	// 3. Inline merge when first pasted block is a plain/rich Paragraph (Notion / Feishu rule).
	case pasted[0].Kind == model.BlockParagraph:
		prefix := sliceSpans(b.Spans, 0, lo)
		suffix := sliceSpans(b.Spans, hi, len(curText))
		first := b.Clone()
		first.Spans = normalizeSpans(append(append([]model.Span(nil), prefix...), pasted[0].Spans...))
		first.Dirty = true
		first.CleanSource = ""
		inserted = append(inserted, first)
		if len(pasted) > 1 {
			inserted = append(inserted, pasted[1:]...)
		}
		focusRel = len(inserted) - 1
		caretOff = len([]rune(inserted[focusRel].PlainText()))
		if len(suffix) > 0 {
			last := &inserted[focusRel]
			if blockAcceptsInlineSuffix(last.Kind) {
				last.Spans = normalizeSpans(append(append([]model.Span(nil), last.Spans...), suffix...))
			} else {
				inserted = append(inserted, model.Block{
					ID:     model.NewBlockID(),
					Kind:   model.BlockParagraph,
					Indent: b.Indent,
					Spans:  normalizeSpans(suffix),
					Dirty:  true,
				})
			}
		}

	// 4. First pasted block is structural (List / Heading / Code / Table / Quote / Divider): start on new block.
	default:
		prefix := sliceSpans(b.Spans, 0, lo)
		suffix := sliceSpans(b.Spans, hi, len(curText))
		if len(prefix) > 0 {
			pb := b.Clone()
			pb.Spans = normalizeSpans(prefix)
			pb.Dirty = true
			pb.CleanSource = ""
			inserted = append(inserted, pb)
		}
		inserted = append(inserted, pasted...)
		focusRel = len(inserted) - 1
		caretOff = len([]rune(inserted[focusRel].PlainText()))
		if len(suffix) > 0 {
			tailKind := model.BlockParagraph
			if len(prefix) == 0 {
				tailKind = b.Kind
			}
			tail := b.Clone()
			tail.ID = model.NewBlockID()
			tail.Kind = tailKind
			tail.Spans = normalizeSpans(suffix)
			tail.Dirty = true
			tail.CleanSource = ""
			inserted = append(inserted, tail)
		}
	}

	s.spliceBlocks(idx, idx+1, inserted)
	s.focusedBlock = clampInt(idx+focusRel, 0, len(s.doc.Blocks)-1)
	maxOff := len([]rune(s.doc.Blocks[s.focusedBlock].PlainText()))
	caretOff = clampInt(caretOff, 0, maxOff)
	s.selAnchor = Position{Block: s.focusedBlock, Offset: caretOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

func blockAcceptsInlineSuffix(k model.BlockKind) bool {
	return k == model.BlockParagraph || k.IsHeading() || k.IsList() || k == model.BlockQuote || k == model.BlockCallout
}

func (s *Session) isCaretAtLineStartInBlock(idx int) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	a, _ := s.orderedSelection()
	if a.Block != idx || a.Offset <= 0 {
		return true
	}
	rs := []rune(s.doc.Blocks[idx].PlainText())
	if a.Offset > len(rs) {
		return false
	}
	return rs[a.Offset-1] == '\n'
}

func dedentCodeSnippet(code string) string {
	lines := strings.Split(code, "\n")
	minCols := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := 0
		for i := 0; i < len(line); i++ {
			if line[i] == ' ' {
				cols++
			} else if line[i] == '\t' {
				cols += 4
			} else {
				break
			}
		}
		if minCols < 0 || cols < minCols {
			minCols = cols
		}
	}
	if minCols <= 0 {
		return code
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			out[i] = ""
			continue
		}
		rem := minCols
		pos := 0
		for pos < len(line) && rem > 0 {
			if line[pos] == ' ' {
				rem--
				pos++
			} else if line[pos] == '\t' {
				if rem >= 4 {
					rem -= 4
					pos++
				} else {
					out[i] = strings.Repeat(" ", 4-rem) + line[pos+1:]
					pos = -1
					break
				}
			} else {
				break
			}
		}
		if pos >= 0 {
			out[i] = line[pos:]
		}
	}
	return strings.Join(out, "\n")
}

func parseSingleClipboardURL(raw string) (string, bool) {
	s := strings.TrimSpace(strings.TrimPrefix(raw, "\uFEFF"))
	if media.WebLink(s) == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	schemeLen := 0
	switch {
	case strings.HasPrefix(lower, "https://"):
		schemeLen = 8
	case strings.HasPrefix(lower, "http://"):
		schemeLen = 7
	case strings.HasPrefix(lower, "mailto:"):
		addr := s[7:]
		at := strings.IndexByte(addr, '@')
		if at > 0 && at+1 < len(addr) && strings.ContainsRune(addr[at+1:], '.') && !strings.ContainsRune(addr, '/') {
			return "mailto:" + addr, true
		}
		return "", false
	default:
		return "", false
	}
	rest := s[schemeLen:]
	host := rest
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		host = rest[:cut]
	}
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", false
	}
	hasAlphaNum := false
	for i := 0; i < len(host); i++ {
		c := host[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			hasAlphaNum = true
			break
		}
	}
	if !hasAlphaNum || (!strings.ContainsRune(host, '.') && !strings.ContainsRune(host, ':') && !strings.EqualFold(host, "localhost")) {
		return "", false
	}
	return lower[:schemeLen] + s[schemeLen:], true
}

func (s *Session) tryPasteURLOverSelection(raw string) bool {
	if s.doc == nil || !s.isMarkdownNote() || !s.hasNonEmptySelection() || s.hasWholeAtomicSelection() {
		return false
	}
	urlStr, ok := parseSingleClipboardURL(raw)
	if !ok {
		return false
	}
	a, b := s.orderedSelection()
	var selectedText strings.Builder
	canLink := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		if blk.Kind == model.BlockTable {
			cellSpans, cLo, cHi, cellOK := singleCellSpansInSelection(blk, a, b, lo, hi)
			if !cellOK {
				return false
			}
			rs := []rune(model.PlainTextOfSpans(*cellSpans))
			cLo = clampInt(cLo, 0, len(rs))
			cHi = clampInt(cHi, cLo, len(rs))
			if cHi > cLo {
				selectedText.WriteString(string(rs[cLo:cHi]))
				canLink = true
			}
			continue
		}
		if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
			return false
		}
		rs := []rune(blk.PlainText())
		lo = clampInt(lo, 0, len(rs))
		hi = clampInt(hi, lo, len(rs))
		if hi > lo {
			selectedText.WriteString(string(rs[lo:hi]))
			canLink = true
		}
	}
	if !canLink {
		return false
	}
	selTrim := strings.TrimSpace(selectedText.String())
	if selTrim == "" {
		return false
	}
	// If the selected text itself is already a standalone URL, replace it rather than creating [oldURL](newURL).
	if _, selIsURL := parseSingleClipboardURL(selTrim); selIsURL {
		return false
	}
	s.ApplyLinkOnSelection(urlStr)
	s.focusPending = true
	s.selSyncPending = true
	return true
}

func looksLikeMarkdownBlock(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	// ATX headings # through ######.
	if t[0] == '#' {
		hashes := 0
		for hashes < len(t) && t[hashes] == '#' {
			hashes++
		}
		if hashes >= 1 && hashes <= 6 && (hashes == len(t) || t[hashes] == ' ' || t[hashes] == '\t') {
			return true
		}
	}
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") ||
		strings.HasPrefix(t, "> ") || t == ">" ||
		strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") || strings.HasPrefix(t, "$$") ||
		t == "---" || t == "***" || t == "___" || strings.HasPrefix(t, "|") ||
		(strings.HasPrefix(t, "![") && strings.Contains(t, "](") && strings.HasSuffix(t, ")")) {
		return true
	}
	// Ordered list marker `1. ` or `1) `.
	digits := 0
	for digits < len(t) && digits < 9 && t[digits] >= '0' && t[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits+1 < len(t) && (t[digits] == '.' || t[digits] == ')') && (t[digits+1] == ' ' || t[digits+1] == '\t') {
		return true
	}
	return false
}

func looksLikeInlineMarkdown(line string) bool {
	lower := strings.ToLower(line)
	return strings.ContainsAny(line, "*_`~[") ||
		strings.Contains(line, "<span") || strings.Contains(line, "<u>") || strings.Contains(line, "<a ") ||
		strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
}

func (s *Session) parseInlineSpans(text string) []model.Span {
	if text == "" {
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return []model.Span{{Text: text}}
	}
	if s.codec == nil {
		return []model.Span{{Text: text}}
	}
	if strings.Contains(text, "\n") {
		lines := strings.Split(text, "\n")
		var out []model.Span
		for i, ln := range lines {
			if i > 0 {
				out = append(out, model.Span{Text: "\n"})
			}
			out = append(out, s.parseInlineSpans(ln)...)
		}
		return normalizeSpans(out)
	}
	// Wrap with "x ... x" so leading/trailing spaces and block prefixes parse strictly as inline Paragraph spans.
	wrapped := "x " + text + " x"
	if doc := s.codec.Parse(wrapped); doc != nil && len(doc.Blocks) == 1 && doc.Blocks[0].Kind == model.BlockParagraph {
		spans := doc.Blocks[0].Spans
		total := len([]rune(model.PlainTextOfSpans(spans)))
		if total >= 4 {
			inner := sliceSpans(spans, 2, total-2)
			if len(inner) > 0 {
				return inner
			}
		}
	}
	if doc := s.codec.Parse(text); doc != nil && len(doc.Blocks) == 1 && doc.Blocks[0].Kind == model.BlockParagraph {
		if len(doc.Blocks[0].Spans) > 0 {
			return doc.Blocks[0].Spans
		}
	}
	return []model.Span{{Text: text}}
}

func (s *Session) replaceSelectionWithSpansInBlock(idx int, insertSpans []model.Span) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	curLen := len([]rune(b.PlainText()))
	a, endPos := s.orderedSelection()
	lo, hi := curLen, curLen
	if a.Block == idx && endPos.Block == idx {
		lo = clampInt(a.Offset, 0, curLen)
		hi = clampInt(endPos.Offset, lo, curLen)
	}
	var merged []model.Span
	merged = append(merged, sliceSpans(b.Spans, 0, lo)...)
	merged = append(merged, insertSpans...)
	merged = append(merged, sliceSpans(b.Spans, hi, curLen)...)
	b.Spans = normalizeSpans(merged)
	b.Dirty = true
	b.CleanSource = ""
	insLen := len([]rune(model.PlainTextOfSpans(insertSpans)))
	s.selAnchor = Position{Block: idx, Offset: lo + insLen}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

const maxPastedTableCells = 100_000

func (s *Session) buildTableFromTSV(tsv string) *model.TableData {
	rawRows := strings.Split(tsv, "\n")
	maxCols := 0
	var grid [][]string
	for _, rLine := range rawRows {
		if rLine == "" && len(grid) > 0 && rLine == rawRows[len(rawRows)-1] {
			continue
		}
		cols := strings.Split(rLine, "\t")
		if len(cols) > maxCols {
			maxCols = len(cols)
		}
		grid = append(grid, cols)
	}
	if len(grid) == 0 || maxCols < 2 || max(len(grid), 2) > maxPastedTableCells/maxCols {
		return nil
	}
	rows := make([][]model.TableCell, max(len(grid), 2))
	for r := range rows {
		rows[r] = make([]model.TableCell, maxCols)
		if r < len(grid) {
			for c := 0; c < len(grid[r]); c++ {
				rows[r][c] = model.TableCell{Spans: s.parseInlineSpans(grid[r][c])}
			}
		}
	}
	aligns := make([]model.ColAlign, maxCols)
	return &model.TableData{Aligns: aligns, Rows: rows}
}

func (s *Session) syncTableActiveBounds(idx int) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	rows := len(b.Table.Rows)
	cols := max(tableMaxCols(b.Table), 1)
	if s.tableActiveBlock == b.ID {
		s.tableActiveRow = clampInt(s.tableActiveRow, 0, rows-1)
		s.tableActiveCol = clampInt(s.tableActiveCol, 0, cols-1)
	}
	if s.focusedBlock == idx && s.selAnchor.Block == idx && s.selHead.Block == idx {
		maxOff := s.blockSelectionLen(idx)
		s.selAnchor.Offset = clampInt(s.selAnchor.Offset, 0, maxOff)
		s.selHead.Offset = clampInt(s.selHead.Offset, 0, maxOff)
		s.selSyncPending = true
	}
}

func (s *Session) pasteIntoTableBlock(idx int, norm string) {
	s.pasteIntoTableBlockOpts(idx, norm, false, false)
}

func (s *Session) pasteIntoTableBlockOpts(idx int, norm string, plain bool, keepTrailingEmptyRow bool) {
	b := &s.doc.Blocks[idx]
	tbl := b.Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return
	}
	startR := clampInt(s.tableActiveRow, 0, len(tbl.Rows)-1)
	startC := clampInt(s.tableActiveCol, 0, max(len(tbl.Rows[0])-1, 0))
	if !strings.Contains(norm, "\t") && !strings.Contains(norm, "\n") {
		if plain {
			s.spliceSpansIntoActiveTableCell(idx, []model.Span{{Text: norm}})
		} else {
			s.spliceSpansIntoActiveTableCell(idx, s.parseInlineSpans(norm))
		}
		return
	}
	if !plain && s.codec != nil && strings.HasPrefix(strings.TrimLeft(norm, " \t"), "|") {
		if parsed := s.codec.Parse(norm); parsed != nil && len(parsed.Blocks) == 1 &&
			parsed.Blocks[0].Kind == model.BlockTable && parsed.Blocks[0].Table != nil && len(parsed.Blocks[0].Table.Rows) > 0 {
			srcTbl := parsed.Blocks[0].Table
			neededCols := max(len(tbl.Rows[0]), 1)
			for _, r := range srcTbl.Rows {
				if startC+len(r) > neededCols {
					neededCols = startC + len(r)
				}
			}
			neededRows := max(len(tbl.Rows), startR+len(srcTbl.Rows))
			if neededRows > maxPastedTableCells/neededCols {
				return
			}
			for len(tbl.Aligns) < neededCols {
				tbl.Aligns = append(tbl.Aligns, model.ColAlignNone)
			}
			for len(tbl.Rows) < neededRows {
				tbl.Rows = append(tbl.Rows, make([]model.TableCell, neededCols))
			}
			for r := range tbl.Rows {
				for len(tbl.Rows[r]) < neededCols {
					tbl.Rows[r] = append(tbl.Rows[r], model.TableCell{})
				}
			}
			for dr, srcRow := range srcTbl.Rows {
				for dc, srcCell := range srcRow {
					tbl.Rows[startR+dr][startC+dc].Spans = append([]model.Span(nil), srcCell.Spans...)
				}
			}
			b.Dirty = true
			b.CleanSource = ""
			s.focusedBlock = idx
			s.tableActiveBlock = b.ID
			s.tableActiveRow = startR + len(srcTbl.Rows) - 1
			s.tableActiveCol = startC + max(len(srcTbl.Rows[len(srcTbl.Rows)-1])-1, 0)
			endOff := tableCellStartOffset(tbl, s.tableActiveRow, s.tableActiveCol) +
				len([]rune(tbl.Rows[s.tableActiveRow][s.tableActiveCol].PlainText()))
			s.selAnchor = Position{Block: idx, Offset: endOff}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return
		}
	}
	rawRows := strings.Split(norm, "\n")
	if !keepTrailingEmptyRow && len(rawRows) > 1 && rawRows[len(rawRows)-1] == "" {
		rawRows = rawRows[:len(rawRows)-1]
	}
	neededCols := max(len(tbl.Rows[0]), 1)
	for _, rLine := range rawRows {
		neededCols = max(neededCols, startC+1+strings.Count(rLine, "\t"))
	}
	neededRows := max(len(tbl.Rows), startR+len(rawRows))
	if neededRows > maxPastedTableCells/neededCols {
		return
	}
	for len(tbl.Aligns) < neededCols {
		tbl.Aligns = append(tbl.Aligns, model.ColAlignNone)
	}
	for len(tbl.Rows) < neededRows {
		tbl.Rows = append(tbl.Rows, make([]model.TableCell, neededCols))
	}
	for r := range tbl.Rows {
		for len(tbl.Rows[r]) < neededCols {
			tbl.Rows[r] = append(tbl.Rows[r], model.TableCell{})
		}
	}
	for dr, rLine := range rawRows {
		cols := strings.Split(rLine, "\t")
		for dc, cellText := range cols {
			if plain {
				if cellText == "" {
					tbl.Rows[startR+dr][startC+dc].Spans = nil
				} else {
					tbl.Rows[startR+dr][startC+dc].Spans = []model.Span{{Text: cellText}}
				}
			} else {
				tbl.Rows[startR+dr][startC+dc].Spans = s.parseInlineSpans(cellText)
			}
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.focusedBlock = idx
	s.tableActiveBlock = b.ID
	s.tableActiveRow = startR + len(rawRows) - 1
	lastCols := strings.Split(rawRows[len(rawRows)-1], "\t")
	s.tableActiveCol = startC + len(lastCols) - 1
	endOff := tableCellStartOffset(tbl, s.tableActiveRow, s.tableActiveCol) +
		len([]rune(tbl.Rows[s.tableActiveRow][s.tableActiveCol].PlainText()))
	s.selAnchor = Position{Block: idx, Offset: endOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.multiSelFocusPending = false
	s.markDirty()
}

func (s *Session) spliceSpansIntoActiveTableCell(idx int, insertSpans []model.Span) {
	b := &s.doc.Blocks[idx]
	tbl := b.Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return
	}
	startR := clampInt(s.tableActiveRow, 0, len(tbl.Rows)-1)
	startC := clampInt(s.tableActiveCol, 0, max(len(tbl.Rows[0])-1, 0))
	cell := &tbl.Rows[startR][startC]
	cellLen := len([]rune(model.PlainTextOfSpans(cell.Spans)))
	cLo, cHi := cellLen, cellLen
	a, endPos := s.orderedSelection()
	if a.Block == idx && endPos.Block == idx {
		base := tableCellStartOffset(tbl, startR, startC)
		cLo = clampInt(a.Offset-base, 0, cellLen)
		cHi = clampInt(endPos.Offset-base, cLo, cellLen)
	}
	var merged []model.Span
	merged = append(merged, sliceSpans(cell.Spans, 0, cLo)...)
	merged = append(merged, insertSpans...)
	merged = append(merged, sliceSpans(cell.Spans, cHi, cellLen)...)
	cell.Spans = normalizeSpans(merged)
	b.Dirty = true
	b.CleanSource = ""
	insLen := len([]rune(model.PlainTextOfSpans(insertSpans)))
	newBase := tableCellStartOffset(tbl, startR, startC)
	s.selAnchor = Position{Block: idx, Offset: newBase + cLo + insLen}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

func fallbackParsePaste(src string) *model.Doc {
	lines := strings.Split(src, "\n")
	var blocks []model.Block
	var codeBuf []string
	inCode := false
	codeLang := ""
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if inCode {
			if strings.HasPrefix(trimmed, "```") {
				blocks = append(blocks, model.Block{
					ID: model.NewBlockID(), Kind: model.BlockCode, Language: codeLang, Code: strings.Join(codeBuf, "\n"), Dirty: true,
				})
				inCode = false
				codeBuf = nil
				codeLang = ""
				continue
			}
			codeBuf = append(codeBuf, raw)
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			inCode = true
			codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			codeBuf = nil
			continue
		}
		if trimmed == "" {
			continue
		}
		if trimmed == "---" {
			blocks = append(blocks, model.Block{ID: model.NewBlockID(), Kind: model.BlockDivider, Dirty: true})
			continue
		}
		b := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
		switch {
		case strings.HasPrefix(trimmed, "### "):
			b.Kind = model.BlockHeading3
			b.SetText(strings.TrimPrefix(trimmed, "### "))
		case strings.HasPrefix(trimmed, "## "):
			b.Kind = model.BlockHeading2
			b.SetText(strings.TrimPrefix(trimmed, "## "))
		case strings.HasPrefix(trimmed, "# "):
			b.Kind = model.BlockHeading1
			b.SetText(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(trimmed, "- [x] ") || strings.HasPrefix(trimmed, "- [X] "):
			b.Kind = model.BlockTodo
			b.Checked = true
			b.SetText(trimmed[6:])
		case strings.HasPrefix(trimmed, "- [ ] "):
			b.Kind = model.BlockTodo
			b.SetText(trimmed[6:])
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			b.Kind = model.BlockBullet
			b.SetText(trimmed[2:])
		case strings.HasPrefix(trimmed, "> "):
			b.Kind = model.BlockQuote
			b.SetText(strings.TrimPrefix(trimmed, "> "))
		default:
			b.SetText(raw)
		}
		blocks = append(blocks, b)
	}
	if inCode {
		blocks = append(blocks, model.Block{
			ID: model.NewBlockID(), Kind: model.BlockCode, Language: codeLang, Code: strings.Join(codeBuf, "\n"), Dirty: true,
		})
	}
	return &model.Doc{Blocks: blocks, TrailingLine: "\n"}
}

func (s *Session) replaceCurrentSelectionInBlock(idx int, insert string) {
	b := &s.doc.Blocks[idx]
	a, endPos := s.orderedSelection()
	startOff, endOff := 0, len([]rune(b.PlainText()))
	if a.Block == idx && endPos.Block == idx {
		startOff, endOff = a.Offset, endPos.Offset
	} else {
		startOff = len([]rune(b.PlainText()))
		endOff = startOff
	}
	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		runes := []rune(b.Code)
		startOff = clampInt(startOff, 0, len(runes))
		endOff = clampInt(endOff, startOff, len(runes))
		insRunes := []rune(insert)
		next := make([]rune, 0, len(runes)-(endOff-startOff)+len(insRunes))
		next = append(next, runes[:startOff]...)
		next = append(next, insRunes...)
		next = append(next, runes[endOff:]...)
		b.SetText(string(next))
		caret := startOff + len(insRunes)
		s.selAnchor = Position{Block: idx, Offset: caret}
		s.selHead = s.selAnchor
	} else {
		left := sliceSpans(b.Spans, 0, startOff)
		right := sliceSpans(b.Spans, endOff, len([]rune(b.PlainText())))
		mid := []model.Span{{Text: insert}}
		var merged []model.Span
		merged = append(merged, left...)
		merged = append(merged, mid...)
		merged = append(merged, right...)
		b.Spans = normalizeSpans(merged)
		b.Dirty = true
		b.CleanSource = ""
		caret := startOff + len([]rune(insert))
		s.selAnchor = Position{Block: idx, Offset: caret}
		s.selHead = s.selAnchor
	}
	s.selSyncPending = true
	s.markDirty()
}

// --- Span & Mark Algebra (Rune Offsets) ---

func normalizeSpans(spans []model.Span) []model.Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]model.Span, 0, len(spans))
	for _, sp := range spans {
		if sp.Text == "" {
			continue
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Marks == sp.Marks && last.Link == sp.Link && last.FGColor == sp.FGColor && last.BGColor == sp.BGColor {
				last.Text += sp.Text
				continue
			}
		}
		out = append(out, sp)
	}
	return out
}

func sliceSpans(spans []model.Span, startRune, endRune int) []model.Span {
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
		if lo < hi {
			cp := sp
			cp.Text = string(r[lo:hi])
			out = append(out, cp)
		}
	}
	return normalizeSpans(out)
}

func applySpanTransform(spans []model.Span, startRune, endRune int, fn func(model.Span) model.Span) []model.Span {
	if startRune >= endRune {
		return spans
	}
	var out []model.Span
	pos := 0
	for _, sp := range spans {
		r := []rune(sp.Text)
		n := len(r)
		s0, s1 := pos, pos+n
		pos = s1
		if s1 <= startRune || s0 >= endRune {
			out = append(out, sp)
			continue
		}
		lo := max(startRune-s0, 0)
		hi := min(endRune-s0, n)
		if lo > 0 {
			pre := sp
			pre.Text = string(r[:lo])
			out = append(out, pre)
		}
		mid := sp
		mid.Text = string(r[lo:hi])
		out = append(out, fn(mid))
		if hi < n {
			suf := sp
			suf.Text = string(r[hi:])
			out = append(out, suf)
		}
	}
	return normalizeSpans(out)
}

func spansAllMatch(spans []model.Span, startRune, endRune int, pred func(model.Span) bool) bool {
	if startRune >= endRune {
		return false
	}
	pos := 0
	saw := false
	for _, sp := range spans {
		r := []rune(sp.Text)
		n := len(r)
		s0, s1 := pos, pos+n
		pos = s1
		if s1 <= startRune || s0 >= endRune {
			continue
		}
		saw = true
		if !pred(sp) {
			return false
		}
	}
	return saw
}

func (s *Session) selectionSpanRange(idx int, a, b Position) (int, int) {
	if s.doc == nil || idx < a.Block || idx > b.Block {
		return 0, 0
	}
	rlen := len([]rune(s.doc.Blocks[idx].PlainText()))
	start := 0
	end := rlen
	if idx == a.Block {
		start = clampInt(a.Offset, 0, rlen)
	}
	if idx == b.Block {
		end = clampInt(b.Offset, 0, rlen)
	}
	return start, end
}

func singleCellSpansInSelection(blk *model.Block, a, b Position, lo, hi int) (*[]model.Span, int, int, bool) {
	if a.Block != b.Block || blk.Kind != model.BlockTable || blk.Table == nil {
		return nil, 0, 0, false
	}
	row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, lo, hi)
	if !ok || relLo >= relHi {
		return nil, 0, 0, false
	}
	return &blk.Table.Rows[row][col].Spans, relLo, relHi, true
}

func (s *Session) selectionHasMark(m model.Mark) bool {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return false
	}
	a, b := s.orderedSelection()
	saw := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		spans := blk.Spans
		if blk.Kind == model.BlockTable {
			cellSpans, cLo, cHi, ok := singleCellSpansInSelection(blk, a, b, lo, hi)
			if !ok {
				continue
			}
			spans, lo, hi = *cellSpans, cLo, cHi
		} else if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
			continue
		}
		saw = true
		if !spansAllMatch(spans, lo, hi, func(sp model.Span) bool { return sp.Marks&m != 0 }) {
			return false
		}
	}
	return saw
}

func (s *Session) ToggleMarkOnSelection(m model.Mark) {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return
	}
	a, b := s.orderedSelection()
	clearMark := s.selectionHasMark(m)
	pushedUndo := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		targetSpans := &blk.Spans
		if blk.Kind == model.BlockTable {
			cellSpans, cLo, cHi, ok := singleCellSpansInSelection(blk, a, b, lo, hi)
			if !ok {
				continue
			}
			targetSpans, lo, hi = cellSpans, cLo, cHi
		} else if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
			continue
		}
		if !pushedUndo {
			s.pushUndo()
			pushedUndo = true
		}
		*targetSpans = applySpanTransform(*targetSpans, lo, hi, func(sp model.Span) model.Span {
			if clearMark {
				sp.Marks &^= m
			} else {
				sp.Marks |= m
			}
			return sp
		})
		blk.Dirty = true
		blk.CleanSource = ""
	}
	if pushedUndo {
		s.markDirty()
	}
}

func (s *Session) selectionLinkURL() string {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return ""
	}
	a, b := s.orderedSelection()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		spans := blk.Spans
		if blk.Kind == model.BlockTable {
			cellSpans, cLo, cHi, ok := singleCellSpansInSelection(blk, a, b, lo, hi)
			if !ok {
				continue
			}
			spans, lo, hi = *cellSpans, cLo, cHi
		}
		pos := 0
		for _, sp := range spans {
			n := len([]rune(sp.Text))
			s0, s1 := pos, pos+n
			pos = s1
			if s1 > lo && s0 < hi && sp.Link != "" {
				return sp.Link
			}
		}
	}
	return ""
}

func (s *Session) ApplyLinkOnSelection(url string) {
	url = strings.TrimSpace(url)
	if url != "" && media.WebLink(url) == "" {
		if _, err := media.RelativePath(s.mediaDir(), url); err != nil {
			return
		}
	}
	if !s.hasNonEmptySelection() || s.doc == nil {
		return
	}
	a, b := s.orderedSelection()
	pushedUndo := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		targetSpans := &blk.Spans
		if blk.Kind == model.BlockTable {
			cellSpans, cLo, cHi, ok := singleCellSpansInSelection(blk, a, b, lo, hi)
			if !ok {
				continue
			}
			targetSpans, lo, hi = cellSpans, cLo, cHi
		} else if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
			continue
		}
		if !pushedUndo {
			s.pushUndo()
			pushedUndo = true
		}
		*targetSpans = applySpanTransform(*targetSpans, lo, hi, func(sp model.Span) model.Span {
			sp.Link = strings.TrimSpace(url)
			return sp
		})
		blk.Dirty = true
		blk.CleanSource = ""
	}
	if pushedUndo {
		s.markDirty()
	}
}

func (s *Session) ApplyColorOnSelection(fgColor, bgColor *string) {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return
	}
	a, b := s.orderedSelection()
	pushedUndo := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		targetSpans := &blk.Spans
		if blk.Kind == model.BlockTable {
			cellSpans, cLo, cHi, ok := singleCellSpansInSelection(blk, a, b, lo, hi)
			if !ok {
				continue
			}
			targetSpans, lo, hi = cellSpans, cLo, cHi
		} else if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
			continue
		}
		if !pushedUndo {
			s.pushUndo()
			pushedUndo = true
		}
		*targetSpans = applySpanTransform(*targetSpans, lo, hi, func(sp model.Span) model.Span {
			if fgColor != nil {
				if *fgColor == "default" {
					sp.FGColor = ""
				} else {
					sp.FGColor = *fgColor
				}
			}
			if bgColor != nil {
				if *bgColor == "default" {
					sp.BGColor = ""
				} else {
					sp.BGColor = *bgColor
				}
			}
			return sp
		})
		blk.Dirty = true
		blk.CleanSource = ""
	}
	if pushedUndo {
		s.markDirty()
	}
}

func (s *Session) selectionColors() (fgName, bgName string) {
	fgName, bgName = "default", "default"
	if !s.hasNonEmptySelection() || s.doc == nil {
		return
	}
	a, b := s.orderedSelection()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		spans := blk.Spans
		if blk.Kind == model.BlockTable {
			cellSpans, cLo, cHi, ok := singleCellSpansInSelection(blk, a, b, lo, hi)
			if !ok {
				continue
			}
			spans, lo, hi = *cellSpans, cLo, cHi
		}
		pos := 0
		for _, sp := range spans {
			n := len([]rune(sp.Text))
			s0, s1 := pos, pos+n
			pos = s1
			if s1 > lo && s0 < hi {
				if sp.FGColor != "" {
					fgName = sp.FGColor
				}
				if sp.BGColor != "" {
					bgName = sp.BGColor
				}
			}
		}
	}
	return fgName, bgName
}

func isFrontmatterBlock(idx int, b *model.Block) bool {
	return idx == 0 && b != nil && b.Kind == model.BlockCode && b.Language == "yaml-frontmatter"
}

func (s *Session) setCalloutKind(blockID model.BlockID, kind *model.CalloutKind) {
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	s.calloutMenuBlock = 0
	if kind == nil {
		if b.Kind == model.BlockParagraph && b.Callout == "" {
			return
		}
		s.pushUndo()
		b.Kind = model.BlockParagraph
		b.Callout = ""
	} else {
		if b.Kind == model.BlockCallout && b.Callout == *kind {
			return
		}
		s.pushUndo()
		b.Kind = model.BlockCallout
		b.Callout = *kind
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) SetSelectionBlockKind(kind model.BlockKind, toggleIfSame bool) {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	lo, hi := s.focusedBlock, s.focusedBlock
	if s.hasNonEmptySelection() {
		a, b := s.orderedSelection()
		lo, hi = a.Block, b.Block
	}
	lo = clampInt(lo, 0, len(s.doc.Blocks)-1)
	hi = clampInt(hi, lo, len(s.doc.Blocks)-1)
	if toggleIfSame {
		allSame := true
		for i := lo; i <= hi; i++ {
			if s.doc.Blocks[i].Kind != kind {
				allSame = false
				break
			}
		}
		if allSame {
			kind = model.BlockParagraph
		}
	}
	willChange := false
	for i := lo; i <= hi; i++ {
		b := &s.doc.Blocks[i]
		if b.Kind == model.BlockTable || b.Kind == model.BlockImage || b.Kind == model.BlockDivider || isFrontmatterBlock(i, b) {
			continue
		}
		if b.Kind != kind || b.Callout != "" {
			willChange = true
			break
		}
	}
	if !willChange {
		return
	}
	s.pushUndo()
	for i := lo; i <= hi; i++ {
		b := &s.doc.Blocks[i]
		if b.Kind == model.BlockTable || b.Kind == model.BlockImage || b.Kind == model.BlockDivider || isFrontmatterBlock(i, b) {
			continue
		}
		convertTextBlock(b, kind)
	}
	s.markDirty()
}

// --- Text Sync & Input Rules (Chinese & English Markdown) ---

func (s *Session) applyBlockTextEdit(idx int, newText string, caretHint int) {
	s.applyBlockTextEditWithEdge(idx, newText, caretHint, appUi.RichCodeEdgeNone)
}

func (s *Session) applyBlockTextEditWithEdge(idx int, newText string, caretHint int, codeEdge appUi.RichCodeEdge) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	oldText := b.PlainText()
	if newText == oldText {
		return
	}
	s.firstLineDeleteArmed = false
	rDelta := len([]rune(newText)) - len([]rune(oldText))
	if rDelta < 0 {
		rDelta = -rDelta
	}
	if rDelta <= 4 && !strings.Contains(newText, "\n") && !strings.Contains(oldText, "\n") {
		s.pushCoalescedTextUndo(fmt.Sprintf("blk:%d:%d", b.ID, b.Kind))
	} else {
		s.pushUndo()
	}

	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		b.SetText(newText)
		s.markDirty()
		s.selAnchor = Position{Block: idx, Offset: clampInt(caretHint, 0, len([]rune(newText)))}
		s.selHead = s.selAnchor
		return
	}

	// Preserve inline spans when editing text via rune diff.
	b.Spans = spliceSpansByTextEditWithEdge(b.Spans, oldText, newText, caretHint, codeEdge)
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()

	// Check slash menu trigger / filter on plain paragraphs.
	if b.Kind == model.BlockParagraph {
		if (strings.HasPrefix(newText, "/") || strings.HasPrefix(newText, "、")) && !strings.ContainsAny(newText, " \t\n") && oldText == "" {
			s.openSlashMenu(idx, s.blockRectValue(b.ID))
			if newText != "/" && newText != "、" {
				s.syncSlashMenuText(idx, newText)
			}
		} else if s.blockMenu != nil && s.blockMenu.Mode == menuSlash && s.blockMenu.BlockIdx == idx {
			s.syncSlashMenuText(idx, newText)
		}
	}

	// Check arrow replacements (->, -》, =>, =》, <-, 《-).
	if nextText, nextCaret, ok := applyArrowRule(newText, caretHint); ok {
		b.Spans = spliceSpansByTextEdit(b.Spans, newText, nextText)
		newText = nextText
		caretHint = nextCaret
	}

	// Check immediate divider rule (---, ***, ___, －－－, ＊＊＊, ＿＿＿, ——).
	if b.Kind == model.BlockParagraph && isImmediateDivider(newText) {
		s.insertDividerWithParagraph(idx, true)
		return
	}

	// Check trailing space/fullwidth-space Markdown input rules.
	if nextIdx, nextCaret, ok := s.trySpaceInputRule(idx, newText, caretHint); ok {
		s.focusedBlock = clampInt(nextIdx, 0, len(s.doc.Blocks)-1)
		s.selAnchor = Position{Block: s.focusedBlock, Offset: nextCaret}
		s.selHead = s.selAnchor
		if s.needsDetachedSelectionFocus() {
			s.multiSelFocusPending = true
			s.focusPending = false
			s.selSyncPending = false
		} else {
			s.multiSelFocusPending = false
			s.focusPending = true
			s.selSyncPending = true
		}
		return
	}

	rlen := len([]rune(newText))
	pos := clampInt(caretHint, 0, rlen)
	s.selAnchor = Position{Block: idx, Offset: pos}
	s.selHead = s.selAnchor
}

func spliceSpansByTextEdit(spans []model.Span, oldText, newText string) []model.Span {
	return spliceSpansByTextEditWithEdge(spans, oldText, newText, -1, appUi.RichCodeEdgeNone)
}

func spliceSpansByTextEditWithEdge(spans []model.Span, oldText, newText string, caretHint int, codeEdge appUi.RichCodeEdge) []model.Span {
	if newText == "" {
		return nil
	}
	if len(spans) == 0 {
		return []model.Span{{Text: newText}}
	}
	oldR := []rune(oldText)
	newR := []rune(newText)
	maxPref := min(len(oldR), len(newR))
	if caretHint >= 0 && len(newR) > len(oldR) {
		if insStart := caretHint - (len(newR) - len(oldR)); insStart >= 0 && insStart < maxPref {
			maxPref = insStart
		}
	}
	pref := 0
	for pref < maxPref && oldR[pref] == newR[pref] {
		pref++
	}
	suf := 0
	for suf < len(oldR)-pref && suf < len(newR)-pref && oldR[len(oldR)-1-suf] == newR[len(newR)-1-suf] {
		suf++
	}
	left := sliceSpans(spans, 0, pref)
	right := sliceSpans(spans, len(oldR)-suf, len(oldR))
	inserted := string(newR[pref : len(newR)-suf])
	var mid []model.Span
	if inserted != "" {
		// Inherit style of the span at insertion boundary if any.
		template := model.Span{Text: inserted}
		if codeEdge == appUi.RichCodeEdgeInsideStart && len(right) > 0 && right[0].Marks&model.MarkCode != 0 {
			template = right[0]
			template.Text = inserted
		} else if pref > 0 {
			if prevSlice := sliceSpans(spans, pref-1, pref); len(prevSlice) > 0 {
				template = prevSlice[0]
				template.Text = inserted
			}
		}
		if codeEdge == appUi.RichCodeEdgeAfter {
			template.Marks &^= model.MarkCode
		}
		// Typing at the end of a link keeps the surrounding style and exits the link.
		if template.Link != "" && pref+suf == len(oldR) && (len(right) == 0 || right[0].Link != template.Link) {
			template.Link = ""
		}
		mid = []model.Span{template}
	}
	var merged []model.Span
	merged = append(merged, left...)
	merged = append(merged, mid...)
	merged = append(merged, right...)
	return normalizeSpans(merged)
}

func estimateCaretAfterEdit(oldText, newText string) int {
	oldR := []rune(oldText)
	newR := []rune(newText)
	pref := 0
	for pref < len(oldR) && pref < len(newR) && oldR[pref] == newR[pref] {
		pref++
	}
	suf := 0
	for suf < len(oldR)-pref && suf < len(newR)-pref && oldR[len(oldR)-1-suf] == newR[len(newR)-1-suf] {
		suf++
	}
	return len(newR) - suf
}

func applyArrowRule(text string, caretRune int) (string, int, bool) {
	rules := [][2]string{
		{"->", "→"}, {"-》", "→"}, {"—>", "→"}, {"—》", "→"},
		{"=>", "⇒"}, {"=》", "⇒"}, {"<-", "←"}, {"《-", "←"},
	}
	next := text
	changed := false
	for _, pair := range rules {
		if strings.Contains(next, pair[0]) {
			next = strings.ReplaceAll(next, pair[0], pair[1])
			changed = true
		}
	}
	if !changed {
		return "", 0, false
	}
	nextLen := len([]rune(next))
	if caretRune <= 0 || caretRune > nextLen {
		caretRune = nextLen
	}
	return next, caretRune, true
}

func isImmediateDivider(text string) bool {
	switch text {
	case "---", "***", "___", "－－－", "＊＊＊", "＿＿＿", "——":
		return true
	}
	return false
}

func isDividerLine(text string) bool {
	r := []rune(text)
	if len(r) == 0 {
		return false
	}
	first := r[0]
	for _, ch := range r {
		if ch != first {
			return false
		}
	}
	switch first {
	case '-', '*', '_', '－', '＊', '＿':
		return len(r) >= 3
	case '—':
		return len(r) >= 2
	default:
		return false
	}
}

func matchTodoPrefix(s string) (checked bool, ok bool) {
	pairs := [][2]rune{{'[', ']'}, {'【', '】'}, {'［', '］'}}
	for _, p := range pairs {
		r := []rune(s)
		if len(r) >= 2 && r[0] == p[0] && r[len(r)-1] == p[1] {
			mid := string(r[1 : len(r)-1])
			switch mid {
			case "", " ", "\u3000":
				return false, true
			case "x", "X", "ｘ", "Ｘ":
				return true, true
			}
		}
	}
	return false, false
}

func matchCalloutPrefix(s string) (model.CalloutKind, bool) {
	trimmed := strings.TrimSpace(s)
	trimmed = strings.TrimPrefix(trimmed, ">")
	trimmed = strings.TrimPrefix(trimmed, "＞")
	trimmed = strings.TrimSpace(trimmed)
	switch strings.ToLower(trimmed) {
	case "[!note]", "!note", "!!! note", ":::note", ":::info":
		return model.CalloutNote, true
	case "[!tip]", "!tip", "!!! tip", ":::tip":
		return model.CalloutTip, true
	case "[!important]", "!important":
		return model.CalloutImportant, true
	case "[!warning]", "!warning", "!!! warning", ":::warning":
		return model.CalloutWarning, true
	case "[!caution]", "!caution":
		return model.CalloutCaution, true
	default:
		return "", false
	}
}

func matchCodeFencePrefix(s string) (lang string, ok bool) {
	for _, fence := range []string{"```", "~~~", "｀｀｀", "～～～", "···"} {
		if strings.HasPrefix(s, fence) {
			return strings.TrimSpace(strings.TrimPrefix(s, fence)), true
		}
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "$$" || trimmed == "＄＄" {
		return "math", true
	}
	return "", false
}

func parseFullOrHalfDigits(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, ch := range s {
		var d int
		switch {
		case ch >= '0' && ch <= '9':
			d = int(ch - '0')
		case ch >= '０' && ch <= '９':
			d = int(ch - '０')
		default:
			return 0, false
		}
		n = n*10 + d
	}
	return n, true
}

func (s *Session) trySpaceInputRule(idx int, text string, caretRune int) (int, int, bool) {
	b := &s.doc.Blocks[idx]
	r := []rune(text)
	if len(r) == 0 {
		return 0, 0, false // emptied block (e.g. Backspace on the last char): no rule can match
	}
	if caretRune <= 0 || caretRune > len(r) {
		caretRune = len(r)
	}
	// Support both character-by-character typing (trailing space at caret) and
	// whole-string input such as IME commit or tester Type("# Title").
	ruleEnd := caretRune
	last := r[ruleEnd-1]
	if last != ' ' && last != '\u3000' {
		firstSpace := -1
		for i := 0; i < caretRune; i++ {
			if r[i] == ' ' || r[i] == '\u3000' {
				firstSpace = i
				break
			}
		}
		if firstSpace <= 0 {
			return 0, 0, false
		}
		ruleEnd = firstSpace + 1
	}
	rawPrefix := string(r[:ruleEnd-1])
	rest := string(r[ruleEnd:])
	newCaret := len([]rune(rest))

	if b.Kind == model.BlockBullet {
		trimmed := strings.TrimLeftFunc(rawPrefix, unicode.IsSpace)
		if checked, ok := matchTodoPrefix(trimmed); ok {
			b.Kind = model.BlockTodo
			b.Checked = checked
			b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
			b.Dirty = true
			b.CleanSource = ""
			s.blockMenu = nil
			return idx, newCaret, true
		}
		return 0, 0, false
	}

	if b.Kind == model.BlockQuote {
		if kind, ok := matchCalloutPrefix(rawPrefix); ok {
			b.Kind = model.BlockCallout
			b.Callout = kind
			b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
			b.Dirty = true
			b.CleanSource = ""
			return idx, newCaret, true
		}
		return 0, 0, false
	}

	if b.Kind != model.BlockParagraph && !b.Kind.IsHeading() {
		return 0, 0, false
	}

	if b.Kind == model.BlockParagraph && isDividerLine(rawPrefix) && rest == "" {
		s.insertDividerWithParagraph(idx, true)
		return s.focusedBlock, 0, true
	}

	// Headings: 1..6 '#' or '＃'
	prefixRunes := []rune(rawPrefix)
	if len(prefixRunes) >= 1 && len(prefixRunes) <= 6 {
		allHash := true
		for _, ch := range prefixRunes {
			if ch != '#' && ch != '＃' {
				allHash = false
				break
			}
		}
		if allHash {
			b.Kind = model.HeadingKind(len(prefixRunes))
			b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
			b.Dirty = true
			b.CleanSource = ""
			s.blockMenu = nil
			return idx, newCaret, true
		}
	}

	if b.Kind.IsHeading() {
		return 0, 0, false
	}

	if lang, ok := matchCodeFencePrefix(rawPrefix); ok {
		b.Kind = model.BlockCode
		b.Language = codepreview.CanonicalLanguage(lang)
		b.ShowSource = codepreview.IsPreviewLanguage(b.Language)
		b.Code = rest
		b.Spans = nil
		b.Dirty = true
		b.CleanSource = ""
		s.blockMenu = nil
		return idx, newCaret, true
	}

	if kind, ok := matchCalloutPrefix(rawPrefix); ok {
		b.Kind = model.BlockCallout
		b.Callout = kind
		b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
		b.Dirty = true
		b.CleanSource = ""
		return idx, newCaret, true
	}

	stripped := strings.TrimLeftFunc(rawPrefix, unicode.IsSpace)
	// Ordered list: 1. / 1。 / 1、 / 1． / 1) / 1） / a. / i.
	sr := []rune(stripped)
	if len(sr) >= 2 {
		suf := sr[len(sr)-1]
		if suf == '.' || suf == '。' || suf == '、' || suf == '．' || suf == ')' || suf == '）' {
			body := string(sr[:len(sr)-1])
			if n, ok := parseFullOrHalfDigits(body); ok {
				b.Kind = model.BlockNumbered
				if n > 1 {
					b.ListIndex = n
				} else {
					b.ListIndex = 0
				}
				b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
				b.Dirty = true
				b.CleanSource = ""
				return idx, newCaret, true
			}
			if body == "a" || body == "i" || body == "A" || body == "I" {
				b.Kind = model.BlockNumbered
				b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
				b.Dirty = true
				b.CleanSource = ""
				return idx, newCaret, true
			}
		}
	}

	switch stripped {
	case "-", "+", "*", "•", "·", "・", "－", "＋", "＊":
		b.Kind = model.BlockBullet
		b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
		b.Dirty = true
		b.CleanSource = ""
		return idx, newCaret, true
	}

	if checked, ok := matchTodoPrefix(stripped); ok {
		b.Kind = model.BlockTodo
		b.Checked = checked
		b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
		b.Dirty = true
		b.CleanSource = ""
		return idx, newCaret, true
	}

	switch rawPrefix {
	case ">", "＞", "》", `"`, "“", "”", "|", "｜":
		b.Kind = model.BlockQuote
		b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
		b.Dirty = true
		b.CleanSource = ""
		return idx, newCaret, true
	}

	return 0, 0, false
}

func (s *Session) insertDividerWithParagraph(idx int, replace bool) {
	s.blockMenu = nil
	divider := model.Block{Kind: model.BlockDivider, Dirty: true}
	end := idx
	if replace {
		divider.ID = s.doc.Blocks[idx].ID
		delete(s.richInputs, divider.ID)
		end++
	} else {
		divider.ID = model.NewBlockID()
	}
	paragraph := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
	s.spliceBlocks(idx, end, []model.Block{divider, paragraph})
	if s.isBlockHiddenByFold(idx + 1) {
		// Reveal the inserted pair as a tail without opening the folded section.
		s.showHiddenTail(paragraph.ID)
		s.showHiddenTail(divider.ID)
	}
	s.FocusBlock(idx + 1)
	s.markDirty()
}

// --- Enter / Split / Backspace / Merge / First-Line Delete Arming ---

func (s *Session) handleEnterOnBlock(idx int, text string, caretRune int) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	// If slash menu is open, Enter activates the selected item.
	if s.blockMenu != nil && s.blockMenu.Mode == menuSlash && s.blockMenu.BlockIdx == idx {
		if s.activateSlashMenu() {
			return true
		}
	}

	b := &s.doc.Blocks[idx]
	// Sync latest text before checking Enter rules.
	if b.PlainText() != text && b.Kind != model.BlockCode && b.Kind != model.BlockRawHTML {
		b.Spans = spliceSpansByTextEdit(b.Spans, b.PlainText(), text)
		b.Dirty = true
		b.CleanSource = ""
	}

	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		return false // Let TextArea insert newline normally inside code blocks.
	}

	r := []rune(b.PlainText())
	caretRune = clampInt(caretRune, 0, len(r))

	// Enter on fence / divider / callout at end of paragraph.
	if caretRune == len(r) {
		if (b.Kind == model.BlockParagraph || b.Kind == model.BlockQuote) && b.Callout == "" {
			if kind, ok := matchCalloutPrefix(b.PlainText()); ok {
				s.pushUndo()
				b.Kind = model.BlockCallout
				b.Callout = kind
				b.Spans = nil
				b.Dirty = true
				b.CleanSource = ""
				s.selAnchor = Position{Block: idx, Offset: 0}
				s.selHead = s.selAnchor
				s.selSyncPending = true
				s.markDirty()
				return true
			}
		}
		if b.Kind == model.BlockParagraph {
			if lang, ok := matchCodeFencePrefix(b.PlainText()); ok {
				s.pushUndo()
				b.Kind = model.BlockCode
				b.Language = codepreview.CanonicalLanguage(lang)
				b.ShowSource = codepreview.IsPreviewLanguage(b.Language)
				b.Code = ""
				b.Spans = nil
				b.Dirty = true
				b.CleanSource = ""
				s.selAnchor = Position{Block: idx, Offset: 0}
				s.selHead = s.selAnchor
				s.selSyncPending = true
				s.markDirty()
				return true
			}
			if isDividerLine(b.PlainText()) {
				s.pushUndo()
				s.insertDividerWithParagraph(idx, true)
				return true
			}
		}
	}

	// Empty list or quote item: outdent or demote to paragraph instead of splitting.
	if len(r) == 0 && (b.Kind.IsList() || b.Kind == model.BlockQuote || b.Kind == model.BlockCallout) {
		s.pushUndo()
		if b.Kind.IsList() && b.Indent > 0 {
			b.Indent--
		} else {
			b.Kind = model.BlockParagraph
			b.Callout = ""
		}
		b.Dirty = true
		b.CleanSource = ""
		s.markDirty()
		return true
	}

	// Enter on a folded heading appends a visible paragraph after its hidden section while keeping the fold shut.
	if !s.minimal && s.isMarkdownNote() && !s.hasNonEmptySelection() && caretRune > 0 && b.Kind.IsHeading() && s.folded[b.ID] {
		return s.enterOnFoldedHeading(idx)
	}

	// Keep the existing block's ID, annotations and metadata with its content.
	if !s.hasNonEmptySelection() && caretRune == 0 && len(r) > 0 && b.Kind != model.BlockQuote {
		s.pushUndo()
		blank := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
		if b.Kind.IsList() {
			blank.Kind = b.Kind
			blank.ListIndex, b.ListIndex = b.ListIndex, 0
			b.CleanSource = ""
			b.Dirty = true
		}
		if b.Kind.IsList() || b.Kind == model.BlockParagraph {
			blank.Indent = b.Indent
		}
		s.spliceBlocks(idx, idx, []model.Block{blank})
		s.FocusBlock(idx + 1)
		s.markDirty()
		return true
	}
	if b.Kind == model.BlockQuote && len(r) > 0 && !s.hasNonEmptySelection() {
		s.pushUndo()
		if caretRune == len(r) && r[caretRune-1] == '\n' {
			b.Spans = sliceSpans(b.Spans, 0, caretRune-1)
			b.CleanSource = ""
			b.Dirty = true
			s.spliceBlocks(idx+1, idx+1, []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}})
			s.FocusBlock(idx + 1)
		} else {
			b.Spans = spliceSpansByTextEdit(b.Spans, string(r), string(r[:caretRune])+"\n"+string(r[caretRune:]))
			b.Dirty = true
			b.CleanSource = ""
			s.FocusBlock(idx)
			s.selHead.Offset = caretRune + 1
			s.selAnchor = s.selHead
		}
		s.markDirty()
		return true
	}

	// Split block into [idx] and [idx+1].
	s.pushUndo()
	leftSpans := sliceSpans(b.Spans, 0, caretRune)
	rightSpans := sliceSpans(b.Spans, caretRune, len(r))

	nextKind := model.BlockParagraph
	nextIndent := 0
	if b.Kind.IsList() {
		nextKind = b.Kind
		nextIndent = b.Indent
	} else if caretRune == 0 {
		// Enter at start of block keeps the block's kind on the lower block.
		nextKind = b.Kind
		nextIndent = b.Indent
		b.Kind = model.BlockParagraph
		b.Indent = 0
	}

	b.Spans = leftSpans
	b.Dirty = true
	b.CleanSource = ""

	newBlk := model.Block{
		ID:     model.NewBlockID(),
		Kind:   nextKind,
		Indent: nextIndent,
		Spans:  rightSpans,
		Dirty:  true,
	}
	s.spliceBlocks(idx+1, idx+1, []model.Block{newBlk})
	s.focusedBlock = idx + 1
	s.selAnchor = Position{Block: idx + 1, Offset: 0}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
	return true
}

func (s *Session) canArmFirstLineDelete() bool {
	if s.minimal || s.readOnly || !s.isMarkdownNote() || s.doc == nil || len(s.doc.Blocks) <= 1 {
		return false
	}
	if s.hasNonEmptySelection() {
		return false
	}
	return isPlainEmptyParagraph(&s.doc.Blocks[0])
}

func (s *Session) handleBackspaceOnBlock(idx int, text string, caretRune int) bool {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[idx]
	// Only intercept when caret is collapsed at offset 0 (or empty block).
	if text != "" && caretRune > 0 {
		s.firstLineDeleteArmed = false
		return false
	}
	// If there is a non-empty selection inside the block, let normal delete happen.
	if s.selAnchor.Block == idx && s.selHead.Block == idx && s.selAnchor.Offset != s.selHead.Offset {
		s.firstLineDeleteArmed = false
		return false
	}

	// Code block at offset 0: demote if empty, else move to previous block.
	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		if text == "" {
			s.pushUndo()
			b.Kind = model.BlockParagraph
			b.Language = ""
			b.Code = ""
			b.Dirty = true
			b.CleanSource = ""
			s.markDirty()
			return true
		}
		if idx > 0 {
			s.FocusBlock(idx - 1)
			endOff := len([]rune(s.doc.Blocks[idx-1].PlainText()))
			s.selAnchor = Position{Block: idx - 1, Offset: endOff}
			s.selHead = s.selAnchor
			s.selSyncPending = true
			return true
		}
		return false
	}

	// Demote styled blocks / outdent lists first.
	if b.Kind.IsHeading() || b.Kind == model.BlockQuote || b.Kind == model.BlockCallout {
		s.pushUndo()
		b.Kind = model.BlockParagraph
		b.Callout = ""
		b.Dirty = true
		b.CleanSource = ""
		s.markDirty()
		return true
	}
	if b.Kind.IsList() {
		s.pushUndo()
		if b.Indent > 0 {
			b.Indent--
		} else {
			b.Kind = model.BlockParagraph
		}
		b.Dirty = true
		b.CleanSource = ""
		s.markDirty()
		return true
	}
	if b.Indent > 0 {
		s.pushUndo()
		b.Indent--
		b.Dirty = true
		b.CleanSource = ""
		s.markDirty()
		return true
	}

	// First block empty paragraph: two-step delete confirmation in full editor; direct delete in minimal mode.
	if idx == 0 {
		if s.minimal && !s.readOnly && s.isMarkdownNote() && len(s.doc.Blocks) > 1 && !s.hasNonEmptySelection() && isPlainEmptyParagraph(&s.doc.Blocks[0]) {
			s.pushUndo()
			s.spliceBlocks(0, 1, nil)
			s.focusedBlock = 0
			s.selAnchor = Position{Block: 0, Offset: 0}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.markDirty()
			return true
		}
		if s.canArmFirstLineDelete() {
			if s.firstLineDeleteArmed {
				return s.ConfirmFirstLineDelete()
			}
			s.firstLineDeleteArmed = true
			return true
		}
		return false
	}

	// Sync text if changed.
	if b.PlainText() != text {
		b.Spans = spliceSpansByTextEdit(b.Spans, b.PlainText(), text)
	}
	if s.handleFoldGapBackspace(idx, b) {
		return true
	}
	prev := &s.doc.Blocks[idx-1]
	s.pushUndo()

	// If prev is divider or image or table, remove empty current block or focus prev.
	if prev.Kind == model.BlockDivider || prev.Kind == model.BlockImage || prev.Kind == model.BlockTable || prev.Kind == model.BlockCode {
		if b.PlainText() == "" {
			s.spliceBlocks(idx, idx+1, nil)
			s.FocusBlock(idx - 1)
			if prev.Kind == model.BlockTable || prev.Kind == model.BlockCode {
				s.selAnchor.Offset = len([]rune(s.doc.Blocks[idx-1].PlainText()))
				s.selHead = s.selAnchor
			}
			s.markDirty()
			return true
		}
		return false
	}

	// Merge current block into prev.
	prevLen := len([]rune(prev.PlainText()))
	var merged []model.Span
	merged = append(merged, prev.Spans...)
	merged = append(merged, b.Spans...)
	prev.Spans = normalizeSpans(merged)
	prev.Dirty = true
	prev.CleanSource = ""
	s.spliceBlocks(idx, idx+1, nil)
	s.focusedBlock = idx - 1
	s.selAnchor = Position{Block: idx - 1, Offset: prevLen}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
	return true
}

func (s *Session) ConfirmFirstLineDelete() bool {
	s.firstLineDeleteArmed = false
	if !s.canArmFirstLineDelete() {
		return false
	}
	s.pushUndo()
	s.spliceBlocks(0, 1, nil)
	s.focusedBlock = 0
	s.selAnchor = Position{Block: 0, Offset: 0}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
	return true
}

func (s *Session) spliceBlocks(start, end int, insert []model.Block) {
	if s.doc == nil {
		return
	}
	afterSplice := s.syncFoldTailsBeforeSplice(start, end, insert)
	start = clampInt(start, 0, len(s.doc.Blocks))
	end = clampInt(end, start, len(s.doc.Blocks))
	next := make([]model.Block, 0, len(s.doc.Blocks)-(end-start)+len(insert))
	next = append(next, s.doc.Blocks[:start]...)
	next = append(next, insert...)
	next = append(next, s.doc.Blocks[end:]...)
	if len(next) == 0 {
		next = []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}}
	}
	s.doc.Blocks = next
	if afterSplice != nil {
		afterSplice()
	}
}

func (s *Session) ToggleTodo(idx int) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	if s.doc.Blocks[idx].Kind != model.BlockTodo {
		return
	}
	s.pushUndo()
	s.doc.Blocks[idx].Checked = !s.doc.Blocks[idx].Checked
	s.doc.Blocks[idx].Dirty = true
	s.doc.Blocks[idx].CleanSource = ""
	s.markDirty()
}

func (s *Session) moveBlock(fromIdx, toIdx int) {
	if s.doc == nil || fromIdx < 0 || fromIdx >= len(s.doc.Blocks) || toIdx < 0 || toIdx >= len(s.doc.Blocks) || fromIdx == toIdx {
		return
	}
	if len(s.doc.Blocks) > 0 && isFrontmatterBlock(0, &s.doc.Blocks[0]) && (fromIdx == 0 || toIdx == 0) {
		return
	}
	s.pushUndo()
	blk := s.doc.Blocks[fromIdx]
	rest := make([]model.Block, 0, len(s.doc.Blocks)-1)
	rest = append(rest, s.doc.Blocks[:fromIdx]...)
	rest = append(rest, s.doc.Blocks[fromIdx+1:]...)
	out := make([]model.Block, 0, len(s.doc.Blocks))
	out = append(out, rest[:toIdx]...)
	out = append(out, blk)
	out = append(out, rest[toIdx:]...)
	s.doc.Blocks = out
	s.focusedBlock = toIdx
	s.focusPending = true
	s.markDirty()
}

func (s *Session) duplicateBlock(idx int) {
	if s.doc != nil && idx == 0 && len(s.doc.Blocks) > 0 && isFrontmatterBlock(0, &s.doc.Blocks[0]) {
		return
	}
	s.duplicateRange(idx, idx)
}

func (s *Session) deleteBlock(idx int) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	s.pushUndo()
	if len(s.doc.Blocks) == 1 {
		s.doc.Blocks[0] = model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
	} else {
		s.spliceBlocks(idx, idx+1, nil)
	}
	s.FocusBlock(idx)
	s.markDirty()
}

// --- Slash Menu & Block Handle Menu ---

func allSlashItems() []blockMenuItem {
	return []blockMenuItem{
		{Kind: cmdAI, Label: theme.Tr("AI 写作", "AI Write"), Keywords: "ai write polish xiezuo runse 写作 润色"},
		{Kind: cmdTurnH1, Label: theme.Tr("一级标题", "Heading 1"), Keywords: "h1 heading title biaoti", Hint: "#"},
		{Kind: cmdTurnH2, Label: theme.Tr("二级标题", "Heading 2"), Keywords: "h2 heading", Hint: "##"},
		{Kind: cmdTurnH3, Label: theme.Tr("三级标题", "Heading 3"), Keywords: "h3 heading", Hint: "###"},
		{Kind: cmdTable, Label: theme.Tr("表格", "Table"), Keywords: "table biaoge"},
		{Separator: true},
		{Kind: cmdTurnBullet, Label: theme.Tr("无序列表", "Bulleted list"), Keywords: "bullet list ul liebiao", Hint: "-"},
		{Kind: cmdTurnTodo, Label: theme.Tr("待办清单", "Checklist"), Keywords: "todo task check daiban", Hint: "[]"},
		{Kind: cmdCode, Label: theme.Tr("代码块", "Code Block"), Keywords: "code daima", Hint: "```"},
		{Kind: cmdDivider, Label: theme.Tr("分隔线", "Divider"), Keywords: "divider hr line fengexian", Hint: "---"},
		{Kind: cmdImage, Label: theme.Tr("图片", "Image"), Keywords: "image picture photo tupian"},
		{Kind: cmdTurnNumbered, Label: theme.Tr("有序列表", "Numbered list"), Keywords: "number ordered list ol", Hint: "1."},
		{Kind: cmdTurnQuote, Label: theme.Tr("引用", "Quote"), Keywords: "quote yinyong", Hint: ">"},
		{Kind: cmdCallout, Label: theme.Tr("标注", "Callout"), Keywords: "callout note tip warning alert biaozhu 标注 提示", Hint: "[!note]"},
		{Separator: true},
		{Kind: cmdCodeMermaid, Label: theme.Tr("Mermaid 图表", "Mermaid Diagram"), Keywords: "mermaid diagram flow", Hint: "```mermaid"},
		{Kind: cmdCodeMath, Label: theme.Tr("数学公式", "Math Formula"), Keywords: "math latex formula gongshi", Hint: "$$"},
	}
}

func allHandleMenuItems(idx, count int, hasFrontmatter bool) []blockMenuItem {
	if hasFrontmatter && idx == 0 {
		return []blockMenuItem{
			{Kind: cmdDelete, Label: theme.Tr("删除", "Delete"), Hint: "⌫", Danger: true},
		}
	}
	items := []blockMenuItem{
		{Kind: cmdTurnParagraph, Label: theme.Tr("正文", "Text"), Keywords: "text paragraph", Hint: "⌘⌥0"},
		{Kind: cmdTurnH1, Label: theme.Tr("一级标题", "Heading 1"), Hint: "⌘⌥1"},
		{Kind: cmdTurnH2, Label: theme.Tr("二级标题", "Heading 2"), Hint: "⌘⌥2"},
		{Kind: cmdTurnH3, Label: theme.Tr("三级标题", "Heading 3"), Hint: "⌘⌥3"},
		{Kind: cmdTurnBullet, Label: theme.Tr("无序列表", "Bulleted list"), Hint: "⌘⌥5"},
		{Kind: cmdTurnNumbered, Label: theme.Tr("有序列表", "Numbered list"), Hint: "⌘⌥6"},
		{Kind: cmdTurnTodo, Label: theme.Tr("待办清单", "Checklist"), Hint: "⌘⌥4"},
		{Kind: cmdTurnQuote, Label: theme.Tr("引用", "Quote"), Hint: "⌘⌥9"},
		{Kind: cmdCallout, Label: theme.Tr("标注", "Callout")},
		{Kind: cmdCode, Label: theme.Tr("代码块", "Code Block"), Hint: "⌘⌥8"},
		{Separator: true},
		{Kind: cmdDuplicate, Label: theme.Tr("复制块", "Duplicate"), Hint: "⌘D"},
	}
	minMoveIdx := 0
	if hasFrontmatter {
		minMoveIdx = 1
	}
	if idx > minMoveIdx {
		items = append(items, blockMenuItem{Kind: cmdMoveUp, Label: theme.Tr("上移", "Move Up")})
	}
	if idx+1 < count {
		items = append(items, blockMenuItem{Kind: cmdMoveDown, Label: theme.Tr("下移", "Move Down")})
	}
	items = append(items, blockMenuItem{Separator: true})
	items = append(items, blockMenuItem{Kind: cmdDelete, Label: theme.Tr("删除", "Delete"), Hint: "⌫", Danger: true})
	return items
}

func (s *Session) slashMenuEntries() []blockMenuItem {
	if s.blockMenu == nil {
		return nil
	}
	if s.blockMenu.Mode == menuHandle {
		n := 0
		hasFM := false
		if s.doc != nil {
			n = len(s.doc.Blocks)
			hasFM = n > 0 && isFrontmatterBlock(0, &s.doc.Blocks[0])
		}
		return allHandleMenuItems(s.blockMenu.BlockIdx, n, hasFM)
	}
	all := slices.DeleteFunc(allSlashItems(), func(it blockMenuItem) bool {
		return (s.minimal && it.Kind == cmdImage) || (s.aiDisabled && it.Kind == cmdAI)
	})
	q := strings.ToLower(strings.TrimSpace(s.blockMenu.Filter))
	if q == "" {
		return all
	}
	var hits []blockMenuItem
	for _, it := range all {
		if it.Separator {
			continue
		}
		lbl := strings.ToLower(it.Label)
		kw := strings.ToLower(it.Keywords)
		ht := strings.ToLower(it.Hint)
		if strings.Contains(lbl, q) || strings.Contains(kw, q) || (ht != "" && strings.Contains(ht, q)) {
			hits = append(hits, it)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		pi := strings.HasPrefix(strings.ToLower(hits[i].Label), q)
		pj := strings.HasPrefix(strings.ToLower(hits[j].Label), q)
		return pi && !pj
	})
	return hits
}

func (s *Session) openSlashMenu(idx int, triggerRect ui.Rect) {
	s.blockMenu = &blockMenuState{
		BlockIdx:    idx,
		Mode:        menuSlash,
		ActiveIndex: 0,
		TriggerRect: triggerRect,
	}
	s.clampBlockMenuIndex()
}

func (s *Session) openHandleMenu(idx int, triggerRect ui.Rect) {
	s.blockMenu = &blockMenuState{
		BlockIdx:    idx,
		Mode:        menuHandle,
		ActiveIndex: -1,
		TriggerRect: triggerRect,
	}
}

func (s *Session) syncSlashMenuText(idx int, text string) {
	if s.blockMenu == nil || s.blockMenu.Mode != menuSlash {
		return
	}
	if text == "" {
		s.blockMenu = nil
		return
	}
	var filter string
	switch {
	case strings.HasPrefix(text, "//"):
		s.blockMenu = nil
		return
	case strings.HasPrefix(text, "/"):
		filter = strings.TrimPrefix(text, "/")
	case strings.HasPrefix(text, "、"):
		filter = strings.TrimPrefix(text, "、")
	default:
		s.blockMenu = nil
		return
	}
	if s.blockMenu.Filter != filter {
		s.blockMenu.Filter = filter
		s.blockMenu.PointerActive = false
		s.blockMenu.Scroll.Y = 0
	}
	entries := s.slashMenuEntries()
	if len(entries) == 0 {
		s.blockMenu = nil
		return
	}
	s.clampBlockMenuIndex()
}

func (s *Session) clampBlockMenuIndex() {
	if s.blockMenu == nil {
		return
	}
	entries := s.slashMenuEntries()
	if len(entries) == 0 {
		s.blockMenu.ActiveIndex = 0
		return
	}
	idx := clampInt(s.blockMenu.ActiveIndex, 0, len(entries)-1)
	if entries[idx].Separator {
		for i, e := range entries {
			if !e.Separator {
				idx = i
				break
			}
		}
	}
	s.blockMenu.ActiveIndex = idx
}

func (s *Session) stepSlashMenu(down bool) bool {
	if s.blockMenu == nil {
		return false
	}
	entries := s.slashMenuEntries()
	if len(entries) == 0 {
		return false
	}
	n := len(entries)
	cur := s.blockMenu.ActiveIndex
	if cur < 0 || cur >= n {
		if down {
			cur = -1
		} else {
			cur = n
		}
	}
	step := -1
	if down {
		step = 1
	}
	for tries := 0; tries < n; tries++ {
		cur = (cur + step + n) % n
		if !entries[cur].Separator {
			s.blockMenu.ActiveIndex = cur
			s.blockMenu.PointerActive = false
			return true
		}
	}
	return false
}

func (s *Session) activateSlashMenu() bool {
	if s.blockMenu == nil {
		return false
	}
	entries := s.slashMenuEntries()
	if len(entries) == 0 || s.blockMenu.ActiveIndex < 0 || s.blockMenu.ActiveIndex >= len(entries) {
		return false
	}
	idx := s.blockMenu.ActiveIndex
	if entries[idx].Separator {
		return false
	}
	s.executeBlockCmd(s.blockMenu.BlockIdx, s.blockMenu.Mode, entries[idx].Kind)
	return true
}

func (s *Session) executeBlockCmd(idx int, mode blockMenuMode, cmd blockCmdKind) {
	s.blockMenu = nil
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	isSlashClear := mode == menuSlash && b.Kind == model.BlockParagraph &&
		(b.PlainText() == "" || strings.HasPrefix(b.PlainText(), "/") || strings.HasPrefix(b.PlainText(), "、"))

	switch cmd {
	case cmdDuplicate:
		s.duplicateBlock(idx)
		return
	case cmdMoveUp:
		s.stepBlockRange(idx, idx, false)
		return
	case cmdMoveDown:
		s.stepBlockRange(idx, idx, true)
		return
	case cmdDelete:
		s.deleteBlock(idx)
		return
	case cmdAI:
		s.OpenInlineAIOnBlock(idx)
		return
	}

	s.pushUndo()
	if isSlashClear {
		b.Spans = nil
		b.Code = ""
		if st, ok := s.richInputs[b.ID]; ok && st != nil {
			st.Sync("", 0, 0)
		}
	}
	switch cmd {
	case cmdTurnParagraph:
		convertTextBlock(b, model.BlockParagraph)
		b.Callout = ""
	case cmdTurnH1:
		convertTextBlock(b, model.BlockHeading1)
	case cmdTurnH2:
		convertTextBlock(b, model.BlockHeading2)
	case cmdTurnH3:
		convertTextBlock(b, model.BlockHeading3)
	case cmdTurnBullet:
		convertTextBlock(b, model.BlockBullet)
	case cmdTurnNumbered:
		convertTextBlock(b, model.BlockNumbered)
	case cmdTurnTodo:
		convertTextBlock(b, model.BlockTodo)
	case cmdTurnQuote:
		convertTextBlock(b, model.BlockQuote)
		b.Callout = ""
	case cmdCallout:
		convertTextBlock(b, model.BlockCallout)
		b.Callout = model.CalloutNote
	case cmdCode:
		convertTextBlock(b, model.BlockCode)
	case cmdCodeMermaid:
		b.Kind = model.BlockCode
		b.Language = "mermaid"
		b.ShowSource = true
		b.Code = "flowchart LR\n  A[Start] --> B[End]"
		b.Spans = nil
	case cmdCodeMath:
		b.Kind = model.BlockCode
		b.Language = "latex"
		b.ShowSource = true
		b.Code = `E = mc^2`
		b.Spans = nil
	case cmdTable:
		tbl := &model.TableData{
			Aligns: []model.ColAlign{model.ColAlignNone, model.ColAlignNone, model.ColAlignNone},
			Rows: [][]model.TableCell{
				{{}, {}, {}},
				{{}, {}, {}},
				{{}, {}, {}},
			},
		}
		nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockTable, Table: tbl, Dirty: true}
		if isSlashClear {
			nb.ID = b.ID
			s.doc.Blocks[idx] = nb
		} else {
			s.spliceBlocks(idx+1, idx+1, []model.Block{nb})
			idx++
		}
		s.tableActiveBlock = nb.ID
		s.tableActiveRow = 0
		s.tableActiveCol = 0
	case cmdDivider:
		if !isSlashClear {
			if b.Kind.IsHeading() && s.folded[b.ID] {
				idx = s.foldSectionEnd(idx)
			} else {
				idx++
			}
		}
		s.insertDividerWithParagraph(idx, isSlashClear)
		return
	case cmdImage:
		img := model.Block{
			ID: model.NewBlockID(), Kind: model.BlockImage, ImageAlign: model.ImageAlignCenter, ImageWidth: 100, Dirty: true,
		}
		if isSlashClear {
			img.ID = b.ID
			delete(s.richInputs, b.ID)
			s.doc.Blocks[idx] = img
		} else {
			s.spliceBlocks(idx+1, idx+1, []model.Block{img})
			idx++
		}
	}
	b = &s.doc.Blocks[clampInt(idx, 0, len(s.doc.Blocks)-1)]
	b.Dirty = true
	b.CleanSource = ""
	s.focusedBlock = clampInt(idx, 0, len(s.doc.Blocks)-1)
	rlen := len([]rune(b.PlainText()))
	s.selAnchor = Position{Block: s.focusedBlock, Offset: rlen}
	s.selHead = s.selAnchor
	if s.needsDetachedSelectionFocus() {
		s.multiSelFocusPending = true
		s.focusPending = false
		s.selSyncPending = false
	} else {
		s.multiSelFocusPending = false
		s.focusPending = true
		s.selSyncPending = true
	}
	s.markDirty()
	if cmd == cmdImage {
		s.RequestPickImages(b.ID, true)
	}
}

// --- Find ---

func (s *Session) OpenFind() {
	wasOpen := s.findOpen
	oldPad := s.canvasTopPad()
	s.findOpen = true
	if !wasOpen {
		if delta := s.canvasTopPad() - oldPad; delta > 0 {
			s.shiftScrollY(delta)
		}
	}
	s.findFocusPending = true
	s.blockMenu = nil
	s.toolbarPop = ""
	if s.hasNonEmptySelection() {
		a, b := s.orderedSelection()
		if a.Block == b.Block && a.Block >= 0 && a.Block < len(s.doc.Blocks) {
			r := []rune(s.doc.Blocks[a.Block].PlainText())
			lo, hi := clampInt(a.Offset, 0, len(r)), clampInt(b.Offset, 0, len(r))
			if hi > lo && hi-lo <= 80 {
				part := string(r[lo:hi])
				if !strings.Contains(part, "\n") {
					s.findQuery = part
				}
			}
		}
	}
	s.recomputeFind(true)
}

func (s *Session) CloseFind() {
	if !s.findOpen {
		return
	}
	oldPad := s.canvasTopPad()
	s.findOpen = false
	if delta := s.canvasTopPad() - oldPad; delta < 0 {
		s.shiftScrollY(delta)
	}
	s.selAnchor = s.selHead
	s.focusPending = true
	s.focusPreventScroll = true
}

func (s *Session) SetFindQuery(q string) {
	s.findQuery = q
	s.recomputeFind(true)
}

func (s *Session) FindNext() {
	s.recomputeFind(false)
	if len(s.findMatches) == 0 {
		return
	}
	s.findCurrent = (s.findCurrent + 1) % len(s.findMatches)
	s.gotoCurrentFindMatch()
}

func (s *Session) FindPrev() {
	s.recomputeFind(false)
	if len(s.findMatches) == 0 {
		return
	}
	s.findCurrent = (s.findCurrent - 1 + len(s.findMatches)) % len(s.findMatches)
	s.gotoCurrentFindMatch()
}

func (s *Session) recomputeFind(jump bool) {
	if s.findMatchesRev != s.docRev || s.findMatchesQuery != s.findQuery {
		s.refreshFindMatches()
	}
	if len(s.findMatches) == 0 {
		s.findCurrent = 0
		return
	}
	if jump {
		cur := s.selHead
		best := sort.Search(len(s.findMatches), func(i int) bool {
			m := s.findMatches[i]
			return m.BlockIdx > cur.Block || (m.BlockIdx == cur.Block && m.StartRune >= cur.Offset)
		})
		if best == len(s.findMatches) {
			best = 0
		}
		s.findCurrent = best
		s.gotoCurrentFindMatch()
	} else if s.findCurrent >= len(s.findMatches) {
		s.findCurrent = 0
	}
}

func (s *Session) refreshFindMatches() {
	s.findMatchesRev = s.docRev
	s.findMatchesQuery = s.findQuery
	s.findMatches = nil
	if s.findQuery == "" || s.doc == nil {
		return
	}

	needle := strings.ToLower(s.findQuery)
	needleRunes := utf8.RuneCountInString(needle)
	for idx := range s.doc.Blocks {
		hay := strings.ToLower(s.doc.Blocks[idx].PlainText())
		runeOffset := 0
		for {
			match := strings.Index(hay, needle)
			if match < 0 {
				break
			}
			// Lowercasing maps one rune to one rune, even when its byte width changes.
			start := runeOffset + utf8.RuneCountInString(hay[:match])
			runeOffset = start + needleRunes
			s.findMatches = append(s.findMatches, findMatch{BlockIdx: idx, StartRune: start, EndRune: runeOffset})
			hay = hay[match+len(needle):]
		}
	}
}

// Matches are ordered by block and end offset. The first possible overlap
// either ends after offset in blockIdx or belongs to a later block.
func (s *Session) firstFindMatchEndingAfter(blockIdx, offset int) int {
	return sort.Search(len(s.findMatches), func(i int) bool {
		m := s.findMatches[i]
		return m.BlockIdx > blockIdx || (m.BlockIdx == blockIdx && m.EndRune > offset)
	})
}

func (s *Session) findMatchesForBlock(blockIdx int) ([]findMatch, int) {
	if !s.findOpen {
		return nil, 0
	}
	first := s.firstFindMatchEndingAfter(blockIdx, 0)
	if first == len(s.findMatches) || s.findMatches[first].BlockIdx != blockIdx {
		return nil, 0
	}
	last := s.firstFindMatchEndingAfter(blockIdx+1, 0)
	return s.findMatches[first:last], first
}

func (s *Session) gotoCurrentFindMatch() {
	if s.findCurrent < 0 || s.findCurrent >= len(s.findMatches) {
		return
	}
	m := s.findMatches[s.findCurrent]
	s.jumpToFindMatch(m)
	s.focusedBlock = m.BlockIdx
	s.selAnchor = Position{Block: m.BlockIdx, Offset: m.StartRune}
	s.selHead = Position{Block: m.BlockIdx, Offset: m.EndRune}
	s.selSyncPending = true
}

// --- Outline ---

func (s *Session) Outline() []model.OutlineItem {
	if s.doc == nil {
		return nil
	}
	if s.docRev != 0 && s.outlineRev == s.docRev && s.outlineLen == len(s.doc.Blocks) {
		return s.outlineCache
	}
	var items []model.OutlineItem
	for i := range s.doc.Blocks {
		b := &s.doc.Blocks[i]
		if !b.Kind.IsHeading() {
			continue
		}
		title := strings.TrimSpace(b.PlainText())
		if title == "" {
			continue
		}
		lvl := b.Kind.HeadingLevel()
		if lvl > 4 {
			lvl = 4
		}
		items = append(items, model.OutlineItem{
			BlockIndex: i,
			BlockID:    b.ID,
			Level:      lvl,
			Title:      title,
		})
	}
	s.outlineRev = s.docRev
	s.outlineLen = len(s.doc.Blocks)
	s.outlineCache = items
	return items
}

func (s *Session) ensureBlockUnfolded(idx int) {
	if s.doc == nil || idx <= 0 || idx >= len(s.doc.Blocks) {
		return
	}
	bLevel := 99
	if s.doc.Blocks[idx].Kind.IsHeading() {
		bLevel = s.doc.Blocks[idx].Kind.HeadingLevel()
	}
	for i := idx - 1; i >= 0; i-- {
		prev := &s.doc.Blocks[i]
		if prev.Kind.IsHeading() {
			lvl := prev.Kind.HeadingLevel()
			if lvl < bLevel {
				delete(s.folded, prev.ID)
				delete(s.foldTails, prev.ID)
				bLevel = lvl
			}
		}
	}
}

// --- Annotations & Multi-block Reactions / Rewrite ---

func (s *Session) effectiveAnnotationRange() (a, b Position, quoted string, ok bool) {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return Position{}, Position{}, "", false
	}
	if s.hasNonEmptySelection() {
		a, b = s.orderedSelection()
	} else if s.focusedBlock >= 0 && s.focusedBlock < len(s.doc.Blocks) {
		rlen := len([]rune(s.doc.Blocks[s.focusedBlock].PlainText()))
		a = Position{Block: s.focusedBlock, Offset: 0}
		b = Position{Block: s.focusedBlock, Offset: rlen}
	} else {
		return Position{}, Position{}, "", false
	}

	// If selection ends at offset 0 of a later block, exclude that trailing empty slice (matches smoke_annotations.rs).
	if b.Block > a.Block && b.Offset == 0 {
		b.Block--
		b.Offset = len([]rune(s.doc.Blocks[b.Block].PlainText()))
	}

	var parts []string
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		r := []rune(s.doc.Blocks[idx].PlainText())
		lo := 0
		hi := len(r)
		if idx == a.Block {
			lo = clampInt(a.Offset, 0, len(r))
		}
		if idx == b.Block {
			hi = clampInt(b.Offset, 0, len(r))
		}
		if hi > lo {
			parts = append(parts, string(r[lo:hi]))
		}
	}
	return a, b, strings.Join(parts, "\n"), true
}

func (s *Session) AddAnnotation(note string) *model.Annotation {
	s.ensureAnnotationsLoaded()
	a, b, quoted, ok := s.effectiveAnnotationRange()
	if !ok {
		return nil
	}
	s.pushUndo()
	s.nextAnnotSeq++
	ann := &model.Annotation{
		ID:            nextAnnotationID(),
		StartBlockID:  s.doc.Blocks[a.Block].ID,
		StartOffset:   a.Offset,
		EndBlockID:    s.doc.Blocks[b.Block].ID,
		EndOffset:     b.Offset,
		QuotedText:    quoted,
		Note:          strings.TrimSpace(note),
		CreatedAtUnix: time.Now().Unix(),
	}
	s.annotations = append(s.annotations, ann)
	s.activeAnnotID = ""
	s.caretHidden = true
	s.persistAnnotations()
	return ann
}

func (s *Session) AddReaction(emoji string) {
	if emoji == "" {
		return
	}
	s.ensureAnnotationsLoaded()
	a, b, quoted, ok := s.effectiveAnnotationRange()
	if !ok {
		return
	}
	startID := s.doc.Blocks[a.Block].ID
	endID := s.doc.Blocks[b.Block].ID

	// Match existing annotation for the same span or active annotation.
	for i, ann := range s.annotations {
		if (ann.StartBlockID == startID && ann.StartOffset == a.Offset &&
			ann.EndBlockID == endID && ann.EndOffset == b.Offset) ||
			(s.activeAnnotID != "" && ann.ID == s.activeAnnotID) {
			s.toggleReactionOnIndex(i, emoji)
			return
		}
	}

	s.pushUndo()
	s.nextAnnotSeq++
	ann := &model.Annotation{
		ID:            nextAnnotationID(),
		StartBlockID:  startID,
		StartOffset:   a.Offset,
		EndBlockID:    endID,
		EndOffset:     b.Offset,
		QuotedText:    quoted,
		Reactions:     []string{emoji},
		CreatedAtUnix: time.Now().Unix(),
	}
	s.annotations = append(s.annotations, ann)
	s.caretHidden = true
	s.persistAnnotations()
}

func (s *Session) toggleReactionOnIndex(idx int, emoji string) {
	if idx < 0 || idx >= len(s.annotations) {
		return
	}
	s.pushUndo()
	ann := s.annotations[idx]
	if slices.Contains(ann.Reactions, emoji) {
		ann.Reactions = nil
		if ann.Note == "" && ann.RewriteResult == "" {
			s.annotations = append(s.annotations[:idx], s.annotations[idx+1:]...)
			if s.activeAnnotID == ann.ID {
				s.activeAnnotID = ""
			}
		}
	} else {
		ann.Reactions = []string{emoji}
	}
	s.caretHidden = true
	s.persistAnnotations()
}

func (s *Session) deleteAnnotationByID(id string) bool {
	s.ensureAnnotationsLoaded()
	for i, ann := range s.annotations {
		if ann.ID == id {
			s.pushUndo()
			s.annotations = append(s.annotations[:i], s.annotations[i+1:]...)
			if s.activeAnnotID == id {
				s.activeAnnotID = ""
			}
			if s.editingAnnotID == id {
				s.editingAnnotID = ""
			}
			if s.reactionPickerAnnotID == id {
				s.reactionPickerAnnotID = ""
			}
			s.persistAnnotations()
			return true
		}
	}
	return false
}

func (s *Session) updateAnnotationNote(id, note string) bool {
	s.ensureAnnotationsLoaded()
	for _, ann := range s.annotations {
		if ann.ID == id {
			trimmed := strings.TrimSpace(note)
			if ann.Note == trimmed {
				s.editingAnnotID = ""
				return true
			}
			s.pushUndo()
			ann.Note = trimmed
			s.editingAnnotID = ""
			s.persistAnnotations()
			return true
		}
	}
	return false
}

func (s *Session) SetAnnotationAIResponder(fn func(docMarkdown string, pending []*model.Annotation) map[string]string) {
	s.annotationAIResponder = fn
}

func (s *Session) GeneratePendingAnnotations() int {
	s.ensureAnnotationsLoaded()
	if s.aiDisabled || s.annotationAIResponder == nil || len(s.annotations) == 0 {
		return 0
	}
	var pending []*model.Annotation
	for _, a := range s.annotations {
		if a != nil && !a.Applied && strings.TrimSpace(a.Note) != "" && a.RewriteResult == "" {
			cp := *a
			if len(a.Reactions) > 0 {
				cp.Reactions = slices.Clone(a.Reactions)
			}
			pending = append(pending, &cp)
		}
	}
	if len(pending) == 0 {
		return 0
	}
	results := s.annotationAIResponder(s.Markdown(), pending)
	if len(results) == 0 {
		return 0
	}
	updated := 0
	for _, a := range s.annotations {
		if a == nil || a.Applied || strings.TrimSpace(a.Note) == "" || a.RewriteResult != "" {
			continue
		}
		if res, ok := results[a.ID]; ok && strings.TrimSpace(res) != "" {
			a.RewriteResult = res
			a.AIError = ""
			updated++
		}
	}
	if updated > 0 {
		s.persistAnnotations()
	}
	return updated
}

func (s *Session) ApplyAnnotationRewrite(id, replacement string) bool {
	if s.aiDisabled {
		return false
	}
	var ann *model.Annotation
	for _, item := range s.annotations {
		if item.ID == id {
			ann = item
			break
		}
	}
	if ann == nil || s.doc == nil {
		return false
	}
	startIdx := s.blockIndexByID(ann.StartBlockID)
	endIdx := s.blockIndexByID(ann.EndBlockID)
	if startIdx < 0 || endIdx < 0 {
		return false
	}
	if startIdx > endIdx || (startIdx == endIdx && ann.StartOffset > ann.EndOffset) {
		startIdx, endIdx = endIdx, startIdx
	}

	s.pushUndo()
	startBlk := s.doc.Blocks[startIdx]
	endBlk := s.doc.Blocks[endIdx]
	startRunes := []rune(startBlk.PlainText())
	endRunes := []rune(endBlk.PlainText())
	lo := clampInt(ann.StartOffset, 0, len(startRunes))
	hi := clampInt(ann.EndOffset, 0, len(endRunes))

	prefixSpans := sliceSpans(startBlk.Spans, 0, lo)
	suffixSpans := sliceSpans(endBlk.Spans, hi, len(endRunes))

	repLines := strings.Split(strings.ReplaceAll(replacement, "\r\n", "\n"), "\n")
	var newBlocks []model.Block
	if len(repLines) <= 1 {
		var merged []model.Span
		merged = append(merged, prefixSpans...)
		if replacement != "" {
			merged = append(merged, model.Span{Text: replacement})
		}
		merged = append(merged, suffixSpans...)
		b := startBlk.Clone()
		b.Spans = normalizeSpans(merged)
		b.Dirty = true
		b.CleanSource = ""
		newBlocks = []model.Block{b}
	} else {
		for i, line := range repLines {
			var b model.Block
			if i == 0 {
				b = startBlk.Clone()
				var sp []model.Span
				sp = append(sp, prefixSpans...)
				if line != "" {
					sp = append(sp, model.Span{Text: line})
				}
				b.Spans = normalizeSpans(sp)
			} else if i == len(repLines)-1 {
				b = endBlk.Clone()
				if i > 0 {
					b.ID = model.NewBlockID()
				}
				var sp []model.Span
				if line != "" {
					sp = append(sp, model.Span{Text: line})
				}
				sp = append(sp, suffixSpans...)
				b.Spans = normalizeSpans(sp)
			} else {
				b = model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph}
				if line != "" {
					b.Spans = []model.Span{{Text: line}}
				}
			}
			b.Dirty = true
			b.CleanSource = ""
			newBlocks = append(newBlocks, b)
		}
	}

	s.spliceBlocks(startIdx, endIdx+1, newBlocks)
	ann.StartBlockID = newBlocks[0].ID
	ann.StartOffset = lo
	ann.EndBlockID = newBlocks[len(newBlocks)-1].ID
	lastRunes := len([]rune(newBlocks[len(newBlocks)-1].PlainText()))
	suffixLen := len(endRunes) - hi
	ann.EndOffset = max(lastRunes-suffixLen, 0)
	ann.QuotedText = replacement
	ann.Applied = true
	ann.Note = ""
	ann.RewritePrompt = ""
	ann.RewriteResult = ""
	ann.AIError = ""
	if len(ann.Reactions) == 0 {
		for i, item := range s.annotations {
			if item.ID == ann.ID {
				s.annotations = append(s.annotations[:i], s.annotations[i+1:]...)
				break
			}
		}
		if s.activeAnnotID == ann.ID {
			s.activeAnnotID = ""
		}
		if s.editingAnnotID == ann.ID {
			s.editingAnnotID = ""
		}
	}
	s.focusedBlock = startIdx
	endOff := len([]rune(newBlocks[len(newBlocks)-1].PlainText()))
	s.selAnchor = Position{Block: startIdx + len(newBlocks) - 1, Offset: endOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
	s.persistAnnotations()
	return true
}

// --- BatchPlan Diff Computation & Review ---

func computeDiffLines(oldText, newText string) []model.DiffLine {
	oldLines := splitLinesNonEmptyTail(oldText)
	newLines := splitLinesNonEmptyTail(newText)
	m, n := len(oldLines), len(newLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []model.DiffLine
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			out = append(out, model.DiffLine{Kind: model.DiffEqual, OldLine: i + 1, NewLine: j + 1, Text: oldLines[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			out = append(out, model.DiffLine{Kind: model.DiffDelete, OldLine: i + 1, Text: oldLines[i]})
			i++
		} else {
			out = append(out, model.DiffLine{Kind: model.DiffInsert, NewLine: j + 1, Text: newLines[j]})
			j++
		}
	}
	for i < m {
		out = append(out, model.DiffLine{Kind: model.DiffDelete, OldLine: i + 1, Text: oldLines[i]})
		i++
	}
	for j < n {
		out = append(out, model.DiffLine{Kind: model.DiffInsert, NewLine: j + 1, Text: newLines[j]})
		j++
	}
	return out
}

func splitLinesNonEmptyTail(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (s *Session) ensurePlanOpLineSelection(op *model.PlanOp) []model.DiffLine {
	diff := computeDiffLines(op.Old, op.New)
	changedCount := 0
	for _, d := range diff {
		if d.Kind != model.DiffEqual {
			changedCount++
		}
	}
	if len(op.LineSelected) != changedCount {
		op.LineSelected = make([]bool, changedCount)
		for i := range op.LineSelected {
			op.LineSelected[i] = true
		}
	}
	return diff
}

func applySelectedDiff(op *model.PlanOp) string {
	diff := computeDiffLines(op.Old, op.New)
	var out []string
	selIdx := 0
	for _, d := range diff {
		switch d.Kind {
		case model.DiffEqual:
			out = append(out, d.Text)
		case model.DiffDelete:
			selected := true
			if selIdx < len(op.LineSelected) {
				selected = op.LineSelected[selIdx]
			}
			selIdx++
			if !selected {
				out = append(out, d.Text)
			}
		case model.DiffInsert:
			selected := true
			if selIdx < len(op.LineSelected) {
				selected = op.LineSelected[selIdx]
			}
			selIdx++
			if selected {
				out = append(out, d.Text)
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func (s *Session) acceptActivePlan() {
	if s.activePlan == nil || s.readOnly {
		return
	}
	plan := *s.activePlan
	plan.Ops = append([]model.PlanOp(nil), plan.Ops...)
	for i := range plan.Ops {
		op := &plan.Ops[i]
		s.ensurePlanOpLineSelection(op)
		if slices.Contains(op.LineSelected, false) {
			op.New = applySelectedDiff(op)
		}
	}
	if s.host != nil {
		var err error
		if reviewHost, ok := s.host.(interface{ AcceptEditorBatchPlan(*model.BatchPlan) error }); ok {
			err = reviewHost.AcceptEditorBatchPlan(&plan)
		} else {
			err = s.host.ApplyBatchPlan(&plan)
		}
		if err == nil {
			s.activePlan = nil
		}
		return
	}
	// A standalone editor can only apply a full-document change to its own note.
	if len(plan.Ops) != 1 {
		return
	}
	op := plan.Ops[0]
	if op.Kind != model.PlanOpReplace || (s.notePath != "" && op.Page != s.notePath) || s.Markdown() != op.Old {
		return
	}
	s.pushUndo()
	s.doc = normalizeDoc(s.parseMarkdownDoc(op.New))
	s.focusedBlock = 0
	s.selAnchor = Position{}
	s.selHead = Position{}
	s.markDirty()
	s.activePlan = nil
}

func (s *Session) rejectActivePlan() {
	if s.activePlan == nil {
		return
	}
	plan := s.activePlan
	plan.Status = "rejected"
	if s.host != nil {
		_ = s.host.RejectBatchPlan(plan.ID)
	}
	s.activePlan = nil
}

// --- Table Structural Edits ---

func (s *Session) tableAddRow(blockID model.BlockID, at int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	s.pushUndo()
	oldLen := len(b.Table.Rows)
	cols := len(b.Table.Rows[0])
	newRow := make([]model.TableCell, cols)
	// Inherit uniform column tint into the newly inserted row.
	for c := 0; c < cols; c++ {
		if tint, ok := uniformColTint(b.Table, c); ok && tint != model.TableTintNone {
			newRow[c].Tint = tint
		}
	}
	at = clampInt(at, 0, oldLen)
	rows := make([][]model.TableCell, 0, oldLen+1)
	rows = append(rows, b.Table.Rows[:at]...)
	rows = append(rows, newRow)
	rows = append(rows, b.Table.Rows[at:]...)
	b.Table.Rows = rows
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableDeleteRow(blockID model.BlockID, row int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) <= 1 || row < 0 || row >= len(b.Table.Rows) {
		return
	}
	s.pushUndo()
	b.Table.Rows = append(b.Table.Rows[:row], b.Table.Rows[row+1:]...)
	b.Dirty = true
	b.CleanSource = ""
	s.syncTableActiveBounds(idx)
	s.markDirty()
}

func (s *Session) tableAddCol(blockID model.BlockID, at int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 {
		return
	}
	s.pushUndo()
	cols := len(b.Table.Rows[0])
	at = clampInt(at, 0, cols)
	for r := range b.Table.Rows {
		row := b.Table.Rows[r]
		var newCell model.TableCell
		if tint, ok := uniformRowTint(b.Table, r); ok && tint != model.TableTintNone {
			newCell.Tint = tint
		}
		if align, ok := uniformRowAlignOverride(b.Table, r); ok {
			newCell.Align = align
		}
		next := make([]model.TableCell, 0, len(row)+1)
		next = append(next, row[:at]...)
		next = append(next, newCell)
		next = append(next, row[at:]...)
		b.Table.Rows[r] = next
	}
	aligns := make([]model.ColAlign, 0, cols+1)
	for c := 0; c <= cols; c++ {
		if c == at {
			aligns = append(aligns, model.ColAlignNone)
		} else {
			src := c
			if c > at {
				src = c - 1
			}
			if src < len(b.Table.Aligns) {
				aligns = append(aligns, b.Table.Aligns[src])
			} else {
				aligns = append(aligns, model.ColAlignNone)
			}
		}
	}
	b.Table.Aligns = aligns
	if len(b.Table.Widths) > 0 {
		ws := make([]float32, cols+1)
		for c := 0; c <= cols; c++ {
			if c == at {
				continue
			}
			src := c
			if c > at {
				src = c - 1
			}
			if src < len(b.Table.Widths) {
				ws[c] = b.Table.Widths[src]
			}
		}
		b.Table.Widths = ws
	}
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableDeleteCol(blockID model.BlockID, col int) {
	if s.readOnly {
		return
	}
	idx := s.blockIndexByID(blockID)
	if idx < 0 {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Table == nil || len(b.Table.Rows) == 0 || len(b.Table.Rows[0]) <= 1 || col < 0 || col >= len(b.Table.Rows[0]) {
		return
	}
	s.pushUndo()
	for r := range b.Table.Rows {
		row := b.Table.Rows[r]
		if col < len(row) {
			b.Table.Rows[r] = append(row[:col], row[col+1:]...)
		}
	}
	if col < len(b.Table.Aligns) {
		b.Table.Aligns = append(b.Table.Aligns[:col], b.Table.Aligns[col+1:]...)
	}
	if col < len(b.Table.Widths) {
		b.Table.Widths = append(b.Table.Widths[:col], b.Table.Widths[col+1:]...)
	}
	b.Dirty = true
	b.CleanSource = ""
	s.syncTableActiveBounds(idx)
	s.markDirty()
}

// --- Code Languages & Syntax Highlighting ---

type codeLangOption struct {
	Tag   string
	Label string
}

var codeLanguages = []codeLangOption{
	{Tag: "csv", Label: "CSV"},
	{Tag: "latex", Label: "LaTeX"},
	{Tag: "markdown", Label: "Markdown"},
	{Tag: "mermaid", Label: "Mermaid"},
	{Tag: "plaintext", Label: "Plain Text"},
	{Tag: "bash", Label: "Bash"},
	{Tag: "css", Label: "CSS"},
	{Tag: "go", Label: "Go"},
	{Tag: "html", Label: "HTML"},
	{Tag: "javascript", Label: "JavaScript"},
	{Tag: "json", Label: "JSON"},
	{Tag: "lean", Label: "Lean"},
	{Tag: "python", Label: "Python"},
	{Tag: "rust", Label: "Rust"},
	{Tag: "sql", Label: "SQL"},
	{Tag: "typescript", Label: "TypeScript"},
	{Tag: "xml", Label: "XML"},
	{Tag: "yaml", Label: "YAML"},
}

func languageDisplayLabel(lang string) string {
	c := codepreview.CanonicalLanguage(lang)
	if c == "plaintext" {
		return theme.Tr("纯文本", "Plain Text")
	}
	for _, opt := range codeLanguages {
		if opt.Tag == c {
			return opt.Label
		}
	}
	return strings.TrimSpace(lang)
}

type syntaxTokenKind uint8

const (
	tokPlain syntaxTokenKind = iota
	tokKeyword
	tokString
	tokComment
	tokNumber
	tokType
	tokFunc
)

type syntaxToken struct {
	Start int // rune offset
	End   int // rune offset
	Kind  syntaxTokenKind
}

func tokenizeCode(src, lang string) []syntaxToken {
	canon := codepreview.CanonicalLanguage(lang)
	if canon == "plaintext" || src == "" {
		return nil
	}
	r := []rune(src)
	n := len(r)
	var out []syntaxToken
	keywords := langKeywords(canon)

	for i := 0; i < n; {
		// Line comments: // or # or --
		if (r[i] == '/' && i+1 < n && r[i+1] == '/') ||
			((canon == "python" || canon == "bash" || canon == "yaml") && r[i] == '#') ||
			((canon == "sql" || canon == "lean") && r[i] == '-' && i+1 < n && r[i+1] == '-') {
			j := i
			for j < n && r[j] != '\n' {
				j++
			}
			out = append(out, syntaxToken{Start: i, End: j, Kind: tokComment})
			i = j
			continue
		}
		// Block comments: /* ... */
		if r[i] == '/' && i+1 < n && r[i+1] == '*' {
			j := i + 2
			for j+1 < n && !(r[j] == '*' && r[j+1] == '/') {
				j++
			}
			if j+1 < n {
				j += 2
			} else {
				j = n
			}
			out = append(out, syntaxToken{Start: i, End: j, Kind: tokComment})
			i = j
			continue
		}
		// Strings
		if r[i] == '"' || r[i] == '\'' || r[i] == '`' {
			quote := r[i]
			j := i + 1
			for j < n {
				if r[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				if r[j] == quote {
					j++
					break
				}
				if r[j] == '\n' && quote != '`' {
					break
				}
				j++
			}
			out = append(out, syntaxToken{Start: i, End: j, Kind: tokString})
			i = j
			continue
		}
		// Numbers
		if unicode.IsDigit(r[i]) && (i == 0 || !isIdentChar(r[i-1])) {
			j := i + 1
			for j < n && (unicode.IsDigit(r[j]) || r[j] == '.' || r[j] == '_' || r[j] == 'x' || (r[j] >= 'a' && r[j] <= 'f') || (r[j] >= 'A' && r[j] <= 'F')) {
				j++
			}
			out = append(out, syntaxToken{Start: i, End: j, Kind: tokNumber})
			i = j
			continue
		}
		// Identifiers / Keywords / Types / Function calls
		if isIdentStart(r[i]) {
			j := i + 1
			for j < n && isIdentChar(r[j]) {
				j++
			}
			word := string(r[i:j])
			if keywords[word] {
				out = append(out, syntaxToken{Start: i, End: j, Kind: tokKeyword})
			} else if word == "true" || word == "false" || word == "null" || word == "nil" || word == "None" || word == "True" || word == "False" {
				out = append(out, syntaxToken{Start: i, End: j, Kind: tokNumber})
			} else if j < n && r[j] == '(' {
				out = append(out, syntaxToken{Start: i, End: j, Kind: tokFunc})
			} else if unicode.IsUpper(r[i]) {
				out = append(out, syntaxToken{Start: i, End: j, Kind: tokType})
			}
			i = j
			continue
		}
		i++
	}
	return out
}

func isIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isIdentChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}

var (
	langKeywordMaps = map[string]map[string]bool{
		"go":         buildKeywordSet("package", "import", "func", "type", "struct", "interface", "return", "if", "else", "for", "range", "switch", "case", "default", "var", "const", "defer", "go", "select", "chan", "map", "make", "len", "cap", "append", "break", "continue"),
		"rust":       buildKeywordSet("fn", "let", "mut", "pub", "struct", "enum", "impl", "trait", "for", "in", "if", "else", "match", "loop", "while", "return", "use", "mod", "crate", "self", "Self", "super", "where", "async", "await", "move", "const", "static"),
		"python":     buildKeywordSet("def", "class", "import", "from", "as", "return", "if", "elif", "else", "for", "while", "in", "not", "and", "or", "with", "try", "except", "finally", "raise", "pass", "break", "continue", "lambda", "yield", "async", "await"),
		"javascript": buildKeywordSet("function", "const", "let", "var", "return", "if", "else", "for", "while", "switch", "case", "default", "import", "export", "from", "class", "extends", "new", "async", "await", "try", "catch", "throw", "type", "interface", "enum"),
		"sql":        buildKeywordSet("SELECT", "FROM", "WHERE", "INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "CREATE", "TABLE", "JOIN", "LEFT", "RIGHT", "INNER", "ON", "GROUP", "BY", "ORDER", "LIMIT", "AS", "AND", "OR", "NOT", "select", "from", "where", "insert", "update", "delete"),
		"bash":       buildKeywordSet("if", "then", "else", "elif", "fi", "for", "do", "done", "while", "case", "esac", "function", "export", "local", "return", "exit", "echo"),
	}
	defaultKeywordMap = buildKeywordSet("if", "else", "for", "while", "return", "func", "fn", "def", "class", "struct", "type", "import")
)

func buildKeywordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func langKeywords(lang string) map[string]bool {
	if lang == "typescript" {
		lang = "javascript"
	}
	if m, ok := langKeywordMaps[lang]; ok {
		return m
	}
	return defaultKeywordMap
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func formatShortcutDisplay(mac, other string) string {
	if theme.IsMacOS() {
		return mac
	}
	return other
}

func formatMenuTitle(idx int) string {
	return strconv.Itoa(idx)
}
