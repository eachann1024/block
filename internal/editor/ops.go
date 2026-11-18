package editor

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// --- Selection Snapshots & Markdown Copy ---

type toolbarCaps struct {
	BlockType bool
	RichText  bool
	Bold      bool
	Link      bool
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
		if k != model.BlockImage && k != model.BlockDivider {
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
	caps := toolbarCaps{BlockType: true, RichText: true, Bold: true}
	hasTable := false
	tableMultiCell := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		if a.Block != b.Block && idx == b.Block && b.Offset == 0 {
			break
		}
		blk := &s.doc.Blocks[idx]
		rich := blk.Kind != model.BlockCode && blk.Kind != model.BlockRawHTML && blk.Kind != model.BlockImage && blk.Kind != model.BlockDivider
		if blk.Kind == model.BlockTable {
			hasTable = true
			lo, hi := s.selectionSpanRange(idx, a, b)
			if _, _, _, _, ok := tableCellAtRuneRange(blk.Table, lo, hi); !ok {
				tableMultiCell = true
			}
		}
		caps.BlockType = caps.BlockType && rich && blk.Kind != model.BlockTable
		caps.RichText = caps.RichText && rich
		caps.Bold = caps.Bold && rich && !blk.Kind.IsHeading()
	}
	multiBlockTable := hasTable && a.Block != b.Block && !(b.Block == a.Block+1 && b.Offset == 0)
	caps.Link = caps.RichText && !multiBlockTable && !tableMultiCell
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

		if whole && (blk.Kind == model.BlockTable || blk.Kind == model.BlockImage || blk.Kind == model.BlockDivider) {
			cp := blk.Clone()
			cp.Dirty = true
			cp.CleanSource = ""
			return &model.Doc{Blocks: []model.Block{cp}, TrailingLine: "\n"}
		}

		part := s.sliceBlockForSelection(a.Block, a.Offset, b.Offset, false)
		return &model.Doc{Blocks: []model.Block{part}, TrailingLine: "\n"}
	}

	// Single trailing newline boundary between adjacent blocks.
	first := &s.doc.Blocks[a.Block]
	firstLen := len([]rune(first.PlainText()))
	if a.Block+1 == b.Block && a.Offset == firstLen && firstLen > 0 && b.Offset == 0 {
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

		if whole && (blk.Kind == model.BlockTable || blk.Kind == model.BlockImage || blk.Kind == model.BlockDivider) {
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

	first := &s.doc.Blocks[a.Block]
	firstLen := len([]rune(first.PlainText()))
	if a.Block+1 == b.Block && a.Offset == firstLen && firstLen > 0 && b.Offset == 0 {
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
			ovLo := max(startRune, cStart) - cStart
			ovHi := min(endRune, cEnd) - cStart
			if ovLo < ovHi {
				rowTouched = true
				sub := sliceSpans(cell.Spans, ovLo, ovHi)
				cellParts = append(cellParts, serializeInlineBlockMarkdown(sub, codec))
			} else if startRune <= cEnd && endRune >= cStart && (startRune < cStart || endRune > cEnd) {
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
			ovLo := max(startRune, cStart) - cStart
			ovHi := min(endRune, cEnd) - cStart
			if ovLo < ovHi {
				rMin = min(rMin, rIdx)
				rMax = max(rMax, rIdx)
				cMin = min(cMin, cIdx)
				cMax = max(cMax, cIdx)
				hits = append(hits, overlap{r: rIdx, c: cIdx, lo: ovLo, hi: ovHi})
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
		rows[h.r-rMin][h.c-cMin] = model.TableCell{Spans: sliceSpans(cell.Spans, h.lo, h.hi)}
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
		Table: &model.TableData{Aligns: aligns, Rows: rows},
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

func (s *Session) PasteText(raw string) {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	norm := raw
	if s.codec != nil {
		norm = s.codec.NormalizePaste(raw)
	} else {
		norm = strings.ReplaceAll(raw, "\r\n", "\n")
		norm = strings.ReplaceAll(norm, "\r", "\n")
	}
	if norm == "" {
		return
	}
	if s.pasteOverMultiSelection(norm) {
		return
	}
	idx := s.focusedBlock
	if idx < 0 || idx >= len(s.doc.Blocks) {
		idx = 0
	}
	b := &s.doc.Blocks[idx]
	s.pushUndo()
	s.firstLineDeleteArmed = false

	// Code block pastes raw text literally at caret.
	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		s.replaceCurrentSelectionInBlock(idx, norm)
		return
	}

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
	for i := range parsed.Blocks {
		if parsed.Blocks[i].ID == 0 {
			parsed.Blocks[i].ID = model.NewBlockID()
		}
		parsed.Blocks[i].Dirty = true
	}

	if len(parsed.Blocks) == 1 && parsed.Blocks[0].Kind == model.BlockParagraph && b.Kind != model.BlockDivider && b.Kind != model.BlockImage {
		s.replaceSelectionWithSpansInBlock(idx, parsed.Blocks[0].Spans)
		return
	}

	// Splice parsed blocks into document.
	a, endPos := s.orderedSelection()
	curText := []rune(b.PlainText())
	if a.Block == idx && endPos.Block == idx && (len(curText) > 0 && (a.Offset > 0 || endPos.Offset < len(curText))) {
		prefix := sliceSpans(b.Spans, 0, a.Offset)
		suffix := sliceSpans(b.Spans, endPos.Offset, len(curText))
		if len(prefix) == 0 && len(suffix) == 0 {
			s.spliceBlocks(idx, idx+1, parsed.Blocks)
			s.focusedBlock = idx + len(parsed.Blocks) - 1
		} else {
			var inserted []model.Block
			if len(prefix) > 0 {
				pb := b.Clone()
				pb.Spans = prefix
				pb.Dirty = true
				pb.CleanSource = ""
				inserted = append(inserted, pb)
			}
			inserted = append(inserted, parsed.Blocks...)
			if len(suffix) > 0 {
				sb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Spans: suffix, Dirty: true}
				inserted = append(inserted, sb)
			}
			s.spliceBlocks(idx, idx+1, inserted)
			s.focusedBlock = idx + len(inserted) - 1
		}
	} else if b.PlainText() == "" && b.Kind == model.BlockParagraph {
		s.spliceBlocks(idx, idx+1, parsed.Blocks)
		s.focusedBlock = idx + len(parsed.Blocks) - 1
	} else {
		s.spliceBlocks(idx+1, idx+1, parsed.Blocks)
		s.focusedBlock = idx + len(parsed.Blocks)
	}
	endLen := len([]rune(s.doc.Blocks[s.focusedBlock].PlainText()))
	s.selAnchor = Position{Block: s.focusedBlock, Offset: endLen}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

func looksLikeMarkdownBlock(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "# ") || strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "### ") ||
		strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "> ") ||
		strings.HasPrefix(t, "```") || t == "---" || strings.HasPrefix(t, "|")
}

func looksLikeInlineMarkdown(line string) bool {
	return strings.ContainsAny(line, "*_`~[") || strings.Contains(line, "<span") || strings.Contains(line, "<u>")
}

func (s *Session) parseInlineSpans(text string) []model.Span {
	if text == "" {
		return nil
	}
	if s.codec != nil {
		if doc := s.codec.Parse(text); doc != nil && len(doc.Blocks) == 1 && doc.Blocks[0].Kind == model.BlockParagraph {
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
	if len(grid) == 0 || maxCols < 2 {
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

func (s *Session) pasteIntoTableBlock(idx int, norm string) {
	b := &s.doc.Blocks[idx]
	tbl := b.Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return
	}
	startR := clampInt(s.tableActiveRow, 0, len(tbl.Rows)-1)
	startC := clampInt(s.tableActiveCol, 0, max(len(tbl.Rows[0])-1, 0))
	if !strings.Contains(norm, "\t") && !strings.Contains(norm, "\n") {
		cell := &tbl.Rows[startR][startC]
		cell.Spans = append(cell.Spans, s.parseInlineSpans(norm)...)
		cell.Spans = normalizeSpans(cell.Spans)
		b.Dirty = true
		b.CleanSource = ""
		s.focusPending = true
		s.markDirty()
		return
	}
	rawRows := strings.Split(norm, "\n")
	if len(rawRows) > 1 && rawRows[len(rawRows)-1] == "" {
		rawRows = rawRows[:len(rawRows)-1]
	}
	neededCols := len(tbl.Rows[0])
	for _, rLine := range rawRows {
		cols := strings.Split(rLine, "\t")
		if startC+len(cols) > neededCols {
			neededCols = startC + len(cols)
		}
	}
	for len(tbl.Aligns) < neededCols {
		tbl.Aligns = append(tbl.Aligns, model.ColAlignNone)
	}
	neededRows := max(len(tbl.Rows), startR+len(rawRows))
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
			tbl.Rows[startR+dr][startC+dc] = model.TableCell{Spans: s.parseInlineSpans(cellText)}
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.tableActiveRow = startR + len(rawRows) - 1
	lastCols := strings.Split(rawRows[len(rawRows)-1], "\t")
	s.tableActiveCol = startC + len(lastCols) - 1
	s.focusPending = true
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

func (s *Session) selectionHasMark(m model.Mark) bool {
	if !s.hasNonEmptySelection() {
		return false
	}
	a, b := s.orderedSelection()
	saw := false
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		if blk.Kind == model.BlockCode || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage || blk.Kind == model.BlockTable {
			continue
		}
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		saw = true
		if !spansAllMatch(blk.Spans, lo, hi, func(sp model.Span) bool { return sp.Marks&m != 0 }) {
			return false
		}
	}
	return saw
}

func (s *Session) ToggleMarkOnSelection(m model.Mark) {
	if !s.hasNonEmptySelection() {
		return
	}
	a, b := s.orderedSelection()
	clearMark := s.selectionHasMark(m)
	s.pushUndo()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		if blk.Kind == model.BlockCode || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage || blk.Kind == model.BlockTable {
			continue
		}
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		blk.Spans = applySpanTransform(blk.Spans, lo, hi, func(sp model.Span) model.Span {
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
	s.markDirty()
}

func (s *Session) selectionLinkURL() string {
	if !s.hasNonEmptySelection() {
		return ""
	}
	a, b := s.orderedSelection()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		pos := 0
		for _, sp := range blk.Spans {
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
	if !s.hasNonEmptySelection() {
		return
	}
	a, b := s.orderedSelection()
	s.pushUndo()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		if blk.Kind == model.BlockCode || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
			continue
		}
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		blk.Spans = applySpanTransform(blk.Spans, lo, hi, func(sp model.Span) model.Span {
			sp.Link = strings.TrimSpace(url)
			return sp
		})
		blk.Dirty = true
		blk.CleanSource = ""
	}
	s.markDirty()
}

func (s *Session) ApplyColorOnSelection(fgColor, bgColor *string) {
	if !s.hasNonEmptySelection() {
		return
	}
	a, b := s.orderedSelection()
	s.pushUndo()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		if blk.Kind == model.BlockCode || blk.Kind == model.BlockDivider || blk.Kind == model.BlockImage {
			continue
		}
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo >= hi {
			continue
		}
		blk.Spans = applySpanTransform(blk.Spans, lo, hi, func(sp model.Span) model.Span {
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
	s.markDirty()
}

func (s *Session) selectionColors() (fgName, bgName string) {
	fgName, bgName = "default", "default"
	if !s.hasNonEmptySelection() {
		return
	}
	a, b := s.orderedSelection()
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		blk := &s.doc.Blocks[idx]
		lo, hi := s.selectionSpanRange(idx, a, b)
		pos := 0
		for _, sp := range blk.Spans {
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
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	oldText := b.PlainText()
	if newText == oldText {
		return
	}
	s.firstLineDeleteArmed = false
	s.pushUndo()

	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		b.SetText(newText)
		s.markDirty()
		s.selAnchor = Position{Block: idx, Offset: clampInt(caretHint, 0, len([]rune(newText)))}
		s.selHead = s.selAnchor
		return
	}

	// Preserve inline spans when editing text via rune diff.
	b.Spans = spliceSpansByTextEdit(b.Spans, oldText, newText)
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()

	// Check slash menu trigger / filter on plain paragraphs.
	if b.Kind == model.BlockParagraph {
		if (strings.HasPrefix(newText, "/") || strings.HasPrefix(newText, "、")) && !strings.ContainsAny(newText, " \t\n") && oldText == "" {
			s.openSlashMenu(idx, s.blockRects[b.ID])
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
		s.convertBlockToDivider(idx)
		return
	}

	// Check trailing space/fullwidth-space Markdown input rules.
	if nextIdx, nextCaret, ok := s.trySpaceInputRule(idx, newText, caretHint); ok {
		s.focusedBlock = nextIdx
		s.selAnchor = Position{Block: nextIdx, Offset: nextCaret}
		s.selHead = s.selAnchor
		s.focusPending = true
		s.selSyncPending = true
		return
	}

	rlen := len([]rune(newText))
	pos := clampInt(caretHint, 0, rlen)
	s.selAnchor = Position{Block: idx, Offset: pos}
	s.selHead = s.selAnchor
}

func spliceSpansByTextEdit(spans []model.Span, oldText, newText string) []model.Span {
	if newText == "" {
		return nil
	}
	if len(spans) == 0 {
		return []model.Span{{Text: newText}}
	}
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
	left := sliceSpans(spans, 0, pref)
	right := sliceSpans(spans, len(oldR)-suf, len(oldR))
	inserted := string(newR[pref : len(newR)-suf])
	var mid []model.Span
	if inserted != "" {
		// Inherit style of the span at insertion boundary if any.
		template := model.Span{Text: inserted}
		if pref > 0 {
			if prevSlice := sliceSpans(spans, pref-1, pref); len(prevSlice) > 0 {
				template = prevSlice[0]
				template.Text = inserted
			}
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
		s.convertBlockToDivider(idx)
		return idx + 1, 0, true
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
		b.Language = canonicalLangTag(lang)
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

func (s *Session) convertBlockToDivider(idx int) {
	if idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	s.blockMenu = nil
	s.doc.Blocks[idx] = model.Block{ID: s.doc.Blocks[idx].ID, Kind: model.BlockDivider, Dirty: true}
	next := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
	s.spliceBlocks(idx+1, idx+1, []model.Block{next})
	s.focusedBlock = idx + 1
	s.selAnchor = Position{Block: idx + 1, Offset: 0}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
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
				b.Language = canonicalLangTag(lang)
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
				s.convertBlockToDivider(idx)
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
			s.focusedBlock = idx - 1
			endOff := len([]rune(s.doc.Blocks[idx-1].PlainText()))
			if s.doc.Blocks[idx-1].Kind == model.BlockDivider || s.doc.Blocks[idx-1].Kind == model.BlockImage {
				endOff = 0
			}
			s.selAnchor = Position{Block: idx - 1, Offset: endOff}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
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
		s.focusedBlock = 0
	} else {
		s.spliceBlocks(idx, idx+1, nil)
		s.focusedBlock = clampInt(idx, 0, len(s.doc.Blocks)-1)
	}
	s.selAnchor = Position{Block: s.focusedBlock, Offset: 0}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

// --- Slash Menu & Block Handle Menu ---

func allSlashItems() []blockMenuItem {
	return []blockMenuItem{
		{Kind: cmdAI, Label: theme.Tr("AI 写作", "AI Write"), Keywords: "ai write polish xiezuo runse 写作 润色"},
		{Kind: cmdTurnH1, Label: theme.Tr("一级标题", "Heading 1"), Keywords: "h1 heading title biaoti", Hint: "⌘⌥1"},
		{Kind: cmdTurnH2, Label: theme.Tr("二级标题", "Heading 2"), Keywords: "h2 heading", Hint: "⌘⌥2"},
		{Kind: cmdTurnH3, Label: theme.Tr("三级标题", "Heading 3"), Keywords: "h3 heading", Hint: "⌘⌥3"},
		{Kind: cmdTable, Label: theme.Tr("表格", "Table"), Keywords: "table biaoge"},
		{Separator: true},
		{Kind: cmdTurnBullet, Label: theme.Tr("无序列表", "Bulleted list"), Keywords: "bullet list ul liebiao", Hint: "⌘⌥5"},
		{Kind: cmdTurnTodo, Label: theme.Tr("待办清单", "Checklist"), Keywords: "todo task check daiban", Hint: "⌘⌥4"},
		{Kind: cmdCode, Label: theme.Tr("代码块", "Code Block"), Keywords: "code daima", Hint: "⌘⌥8"},
		{Kind: cmdDivider, Label: theme.Tr("分隔线", "Divider"), Keywords: "divider hr line fengexian"},
		{Kind: cmdImage, Label: theme.Tr("图片", "Image"), Keywords: "image picture photo tupian"},
		{Kind: cmdTurnNumbered, Label: theme.Tr("有序列表", "Numbered list"), Keywords: "number ordered list ol", Hint: "⌘⌥6"},
		{Kind: cmdTurnQuote, Label: theme.Tr("引用", "Quote"), Keywords: "quote yinyong", Hint: "⌘⌥9"},
		{Kind: cmdCallout, Label: theme.Tr("标注", "Callout"), Keywords: "callout note tip warning alert biaozhu 标注 提示"},
		{Separator: true},
		{Kind: cmdCodeMermaid, Label: theme.Tr("Mermaid 图表", "Mermaid Diagram"), Keywords: "mermaid diagram flow"},
		{Kind: cmdCodeMath, Label: theme.Tr("数学公式", "Math Formula"), Keywords: "math latex formula gongshi"},
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
	all := allSlashItems()
	if s.minimal {
		filtered := make([]blockMenuItem, 0, len(all))
		for _, it := range all {
			if it.Kind == cmdImage {
				continue
			}
			filtered = append(filtered, it)
		}
		all = filtered
	}
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
		if strings.Contains(lbl, q) || strings.Contains(kw, q) {
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
		ActiveIndex: 0,
		TriggerRect: triggerRect,
	}
	s.clampBlockMenuIndex()
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
	cur := clampInt(s.blockMenu.ActiveIndex, 0, len(entries)-1)
	step := -1
	if down {
		step = 1
	}
	for tries := 0; tries < len(entries); tries++ {
		cur = (cur + step + len(entries)) % len(entries)
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
	if len(entries) == 0 {
		return false
	}
	idx := clampInt(s.blockMenu.ActiveIndex, 0, len(entries)-1)
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

	if cmd == cmdImage && s.OnPickImages != nil {
		targetID := b.ID
		if isSlashClear {
			b.Spans = nil
			b.Code = ""
			b.Dirty = true
			b.CleanSource = ""
			s.selAnchor = Position{Block: idx, Offset: 0}
			s.selHead = s.selAnchor
			s.selSyncPending = true
			s.markDirty()
		}
		s.OnPickImages(targetID, isSlashClear)
		return
	}

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
		div := model.Block{ID: model.NewBlockID(), Kind: model.BlockDivider, Dirty: true}
		trail := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
		if isSlashClear {
			s.spliceBlocks(idx, idx+1, []model.Block{div, trail})
			idx = idx + 1
		} else {
			s.spliceBlocks(idx+1, idx+1, []model.Block{div, trail})
			idx = idx + 2
		}
	case cmdImage:
		img := model.Block{
			ID: model.NewBlockID(), Kind: model.BlockImage, ImageAlign: model.ImageAlignCenter, ImageWidth: 100, Dirty: true,
		}
		if isSlashClear {
			img.ID = b.ID
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
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

// --- Find ---

func (s *Session) OpenFind() {
	s.findOpen = true
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
	s.findOpen = false
	s.selAnchor = s.selHead
	s.focusPending = true
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
	s.findMatches = nil
	q := s.findQuery
	if q == "" || s.doc == nil {
		s.findCurrent = 0
		return
	}
	needle := []rune(strings.ToLower(q))
	if len(needle) == 0 {
		s.findCurrent = 0
		return
	}
	for bIdx := range s.doc.Blocks {
		hay := []rune(strings.ToLower(s.doc.Blocks[bIdx].PlainText()))
		for i := 0; i+len(needle) <= len(hay); {
			if slices.Equal(hay[i:i+len(needle)], needle) {
				s.findMatches = append(s.findMatches, findMatch{
					BlockIdx:  bIdx,
					StartRune: i,
					EndRune:   i + len(needle),
				})
				i += len(needle)
			} else {
				i++
			}
		}
	}
	if len(s.findMatches) == 0 {
		s.findCurrent = 0
		return
	}
	if jump {
		cur := s.selHead
		best := 0
		for i, m := range s.findMatches {
			if m.BlockIdx > cur.Block || (m.BlockIdx == cur.Block && m.StartRune >= cur.Offset) {
				best = i
				break
			}
		}
		s.findCurrent = best
		s.gotoCurrentFindMatch()
	} else if s.findCurrent >= len(s.findMatches) {
		s.findCurrent = 0
	}
}

func (s *Session) gotoCurrentFindMatch() {
	if s.findCurrent < 0 || s.findCurrent >= len(s.findMatches) {
		return
	}
	m := s.findMatches[s.findCurrent]
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

func (s *Session) JumpToBlock(idx int) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	id := s.doc.Blocks[idx].ID
	// Unfold any parent heading so target block is visible.
	s.ensureBlockUnfolded(idx)
	s.highlightedBlockID = id
	s.highlightUntil = time.Now().Add(1600 * time.Millisecond)
	s.outlineHovered = false
	s.outlinePinnedClosed = true
	if rect, ok := s.blockRects[id]; ok && s.editorRect.H > 0 {
		targetY := s.bodyScroll.Y + (rect.Y - s.editorRect.Y) - 36
		if targetY < 0 {
			targetY = 0
		}
		s.bodyScroll.Y = targetY
	}
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
	s.activeAnnotID = ann.ID
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
			if s.footPickerAnnotID == id {
				s.footPickerAnnotID = ""
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
	if s.annotationAIResponder == nil || len(s.annotations) == 0 {
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
	if s.activePlan == nil {
		return
	}
	plan := s.activePlan
	for i := range plan.Ops {
		op := &plan.Ops[i]
		s.ensurePlanOpLineSelection(op)
		op.New = applySelectedDiff(op)
	}
	if s.host != nil {
		_ = s.host.ApplyBatchPlan(plan)
	}
	// If any op matches the current note (or there is a single replace op in standalone test), update doc.
	for i := range plan.Ops {
		op := &plan.Ops[i]
		if op.Kind != model.PlanOpCreateNote && (s.notePath == "" || op.Page == s.notePath || strings.HasSuffix(s.notePath, op.Page)) {
			s.pushUndo()
			var nextDoc *model.Doc
			if s.codec != nil {
				nextDoc = s.codec.Parse(op.New)
			} else {
				nextDoc = fallbackParsePaste(op.New)
			}
			s.doc = normalizeDoc(nextDoc)
			s.focusedBlock = 0
			s.selAnchor = Position{}
			s.selHead = Position{}
			s.markDirty()
			break
		}
	}
	plan.Status = "applied"
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
	newRow := make([]model.TableCell, cols)
	at = clampInt(at, 0, len(b.Table.Rows))
	rows := make([][]model.TableCell, 0, len(b.Table.Rows)+1)
	rows = append(rows, b.Table.Rows[:at]...)
	rows = append(rows, newRow)
	rows = append(rows, b.Table.Rows[at:]...)
	b.Table.Rows = rows
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableDeleteRow(blockID model.BlockID, row int) {
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
	s.markDirty()
}

func (s *Session) tableAddCol(blockID model.BlockID, at int) {
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
		next := make([]model.TableCell, 0, len(row)+1)
		next = append(next, row[:at]...)
		next = append(next, model.TableCell{})
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
	b.Dirty = true
	b.CleanSource = ""
	s.markDirty()
}

func (s *Session) tableDeleteCol(blockID model.BlockID, col int) {
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
	b.Dirty = true
	b.CleanSource = ""
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

func canonicalLangTag(tag string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(tag)))
	if len(fields) == 0 {
		return "plaintext"
	}
	switch fields[0] {
	case "", "auto", "plaintext", "text", "plain", "txt":
		return "plaintext"
	case "csv":
		return "csv"
	case "latex", "math", "tex", "katex":
		return "latex"
	case "markdown", "md":
		return "markdown"
	case "mermaid", "mmd":
		return "mermaid"
	case "bash", "sh", "shell", "zsh":
		return "bash"
	case "css":
		return "css"
	case "go", "golang":
		return "go"
	case "html", "htm":
		return "html"
	case "javascript", "js", "jsx":
		return "javascript"
	case "json", "jsonc":
		return "json"
	case "lean", "lean4":
		return "lean"
	case "python", "py":
		return "python"
	case "rust", "rs":
		return "rust"
	case "sql":
		return "sql"
	case "typescript", "ts", "tsx":
		return "typescript"
	case "xml", "svg":
		return "xml"
	case "yaml", "yml", "yaml-frontmatter", "frontmatter":
		return "yaml"
	default:
		return fields[0]
	}
}

func isPreviewLang(lang string) bool {
	c := canonicalLangTag(lang)
	return c == "mermaid" || c == "latex" || c == "csv"
}

func languageDisplayLabel(lang string) string {
	c := canonicalLangTag(lang)
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
	canon := canonicalLangTag(lang)
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

	mathPreviewReplacer = strings.NewReplacer(
		`\alpha`, "α", `\beta`, "β", `\gamma`, "γ", `\delta`, "δ", `\theta`, "θ", `\lambda`, "λ", `\pi`, "π", `\sigma`, "σ", `\omega`, "ω",
		`\sum`, "∑", `\prod`, "∏", `\int`, "∫", `\infty`, "∞", `\sqrt`, "√", `\pm`, "±", `\times`, "×", `\div`, "÷",
		`\leq`, "≤", `\geq`, "≥", `\neq`, "≠", `\approx`, "≈", `\rightarrow`, "→", `\Rightarrow`, "⇒", `\in`, "∈",
		`\partial`, "∂", `\nabla`, "∇", `^2`, "²", `^3`, "³", `^n`, "ⁿ",
		`$$`, "", `$`, "",
	)
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

// --- Preview Parsers (CSV, Mermaid, LaTeX) ---

func parseCSVRows(src string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var cells []string
		var cur strings.Builder
		inQuotes := false
		for i := 0; i < len(line); i++ {
			ch := line[i]
			switch {
			case ch == '"' && inQuotes && i+1 < len(line) && line[i+1] == '"':
				cur.WriteByte('"')
				i++
			case ch == '"':
				inQuotes = !inQuotes
			case ch == ',' && !inQuotes:
				cells = append(cells, strings.TrimSpace(cur.String()))
				cur.Reset()
			default:
				cur.WriteByte(ch)
			}
		}
		cells = append(cells, strings.TrimSpace(cur.String()))
		rows = append(rows, cells)
	}
	return rows
}

type mermaidEdge struct {
	From  string
	To    string
	Label string
}

func parseMermaidGraph(src string) (nodes []string, labels map[string]string, edges []mermaidEdge, errMsg string) {
	labels = make(map[string]string)
	seen := make(map[string]bool)
	addNode := func(raw string) string {
		raw = strings.TrimSpace(raw)
		id := raw
		lbl := raw
		if i := strings.IndexByte(raw, '['); i > 0 && strings.HasSuffix(raw, "]") {
			id = strings.TrimSpace(raw[:i])
			lbl = strings.TrimSpace(raw[i+1 : len(raw)-1])
		} else if i := strings.IndexByte(raw, '('); i > 0 && strings.HasSuffix(raw, ")") {
			id = strings.TrimSpace(raw[:i])
			lbl = strings.TrimSpace(raw[i+1 : len(raw)-1])
		}
		if id == "" {
			return ""
		}
		if !seen[id] {
			seen[id] = true
			nodes = append(nodes, id)
		}
		if _, ok := labels[id]; !ok || lbl != id {
			labels[id] = lbl
		}
		return id
	}

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") ||
			strings.HasPrefix(line, "graph ") || strings.HasPrefix(line, "flowchart ") ||
			strings.HasPrefix(line, "sequenceDiagram") {
			continue
		}
		if idx := strings.Index(line, "-->"); idx >= 0 {
			left := strings.TrimSpace(line[:idx])
			right := strings.TrimSpace(line[idx+3:])
			edgeLbl := ""
			if strings.HasPrefix(right, "|") {
				if end := strings.IndexByte(right[1:], '|'); end >= 0 {
					edgeLbl = strings.TrimSpace(right[1 : end+1])
					right = strings.TrimSpace(right[end+2:])
				}
			}
			a := addNode(left)
			b := addNode(right)
			if a != "" && b != "" {
				edges = append(edges, mermaidEdge{From: a, To: b, Label: edgeLbl})
			}
		} else if idx := strings.Index(line, "->"); idx >= 0 {
			a := addNode(line[:idx])
			b := addNode(line[idx+2:])
			if a != "" && b != "" {
				edges = append(edges, mermaidEdge{From: a, To: b})
			}
		} else {
			addNode(line)
		}
	}
	if len(nodes) == 0 {
		return nil, nil, nil, theme.Tr("Mermaid 图表内容为空或无法解析", "Empty or unrecognized Mermaid diagram")
	}
	return nodes, labels, edges, ""
}

func formatMathPreview(src string) (string, string) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return "", theme.Tr("数学公式为空", "Math formula is empty")
	}
	out := strings.TrimSpace(mathPreviewReplacer.Replace(trimmed))
	// Balance braces check for basic error bar feedback.
	depth := 0
	for _, ch := range trimmed {
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth < 0 {
				return out, theme.Tr("LaTeX 花括号不匹配", "Unbalanced LaTeX braces")
			}
		}
	}
	if depth != 0 {
		return out, theme.Tr("LaTeX 花括号未闭合", "Unclosed LaTeX brace")
	}
	return out, ""
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
