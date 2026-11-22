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
	"block/internal/files"
	"block/internal/fuzzy"
	"block/internal/media"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
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

		if blk.Kind == model.BlockTable && blk.Table != nil {
			singleCellActive := s.tableActiveBlock == blk.ID && !s.hasWholeAtomicSelection()
			if !whole || singleCellActive {
				if row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
					spans := sliceSpansForClipboard(blk.Table.Rows[row][col].Spans, relLo, relHi)
					return &model.Doc{
						Blocks:       []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Spans: spans, Dirty: true}},
						TrailingLine: "\n",
					}
				}
			}
			if !whole {
				if sliced, ok := sliceTableSelection(blk, a.Offset, b.Offset); ok {
					return &model.Doc{
						Blocks:       []model.Block{sliced},
						TrailingLine: "\n",
					}
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
	if s.doc != nil && s.doc.CleanRaw != "" && s.codec != nil {
		if exp, ok := s.codec.(interface{ ExpandBlockRefCleanSources([]model.Block, string) }); ok {
			exp.ExpandBlockRefCleanSources(blocks, s.doc.CleanRaw)
		}
	}
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

		if blk.Kind == model.BlockTable && blk.Table != nil {
			singleCellActive := s.tableActiveBlock == blk.ID && !s.hasWholeAtomicSelection()
			if !whole || singleCellActive {
				if row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
					spans := sliceSpansForClipboard(blk.Table.Rows[row][col].Spans, relLo, relHi)
					return serializeInlineBlockMarkdown(spans, s.codec)
				}
			}
			if !whole {
				// Partial multi-cell table slice preserves cell inline formatting across tabs/newlines.
				return serializeTableMultiCellMarkdown(blk.Table, a.Offset, b.Offset, s.codec)
			}
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
		spans := sliceSpansForClipboard(blk.Spans, a.Offset, b.Offset)
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

func (s *Session) isSingleCellTableActive(blk *model.Block) bool {
	return blk != nil && blk.Kind == model.BlockTable && s.tableActiveBlock == blk.ID && !s.hasWholeAtomicSelection()
}

// SelectedMarkdownForPlugin returns the selected Markdown for host.editor.selection.
// When an entire single non-paragraph block (list item, heading, quote, callout, todo)
// is selected, it preserves the full block-level Markdown prefix so plugins wrapping
// or replacing the selection via host.editor.insert(mode="replace") never strip container syntax.
// For multi-block selections covering separate clean blockquotes ("> A\n\n> B"), it
// preserves their blank-line separation instead of merging them into a single quote.
func (s *Session) SelectedMarkdownForPlugin() string {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return ""
	}
	a, b := s.orderedSelection()
	if a.Block < 0 || b.Block >= len(s.doc.Blocks) {
		return ""
	}
	lastLen := s.blockSelectionLen(b.Block)
	singleCellWholeDoc := len(s.doc.Blocks) == 1 && s.isSingleCellTableActive(&s.doc.Blocks[0])
	if a.Block == 0 && a.Offset == 0 && b.Block == len(s.doc.Blocks)-1 && b.Offset >= lastLen &&
		s.doc.CleanRaw != "" && s.tableExtend == nil && !singleCellWholeDoc {
		allClean := true
		for i := range s.doc.Blocks {
			if s.doc.Blocks[i].Dirty {
				allClean = false
				break
			}
		}
		if allClean {
			return strings.TrimRight(s.doc.CleanRaw, "\r\n")
		}
	}
	if a.Block == b.Block {
		blk := &s.doc.Blocks[a.Block]
		rlen := s.blockSelectionLen(a.Block)
		whole := a.Offset == 0 && b.Offset >= rlen
		if whole && blk.Kind == model.BlockParagraph && !blk.Dirty {
			if strings.TrimSpace(blk.CleanSource) != "" {
				cp := blk.Clone()
				if s.doc.CleanRaw != "" && s.codec != nil {
					if exp, ok := s.codec.(interface{ ExpandBlockRefCleanSources([]model.Block, string) }); ok {
						one := []model.Block{cp}
						exp.ExpandBlockRefCleanSources(one, s.doc.CleanRaw)
						cp = one[0]
					}
				}
				if !cp.Dirty && strings.TrimSpace(cp.CleanSource) != "" {
					return strings.TrimRight(cp.CleanSource, "\r\n")
				}
			}
		}
		if whole && blk.Kind != model.BlockParagraph && blk.Kind != model.BlockCode &&
			blk.Kind != model.BlockRawHTML && blk.Kind != model.BlockTable &&
			blk.Kind != model.BlockImage && blk.Kind != model.BlockDivider {
			if !blk.Dirty && strings.TrimSpace(blk.CleanSource) != "" {
				cp := blk.Clone()
				if s.doc.CleanRaw != "" && s.codec != nil {
					if exp, ok := s.codec.(interface{ ExpandBlockRefCleanSources([]model.Block, string) }); ok {
						one := []model.Block{cp}
						exp.ExpandBlockRefCleanSources(one, s.doc.CleanRaw)
						cp = one[0]
					}
				}
				if !cp.Dirty && strings.TrimSpace(cp.CleanSource) != "" {
					return strings.TrimRight(cp.CleanSource, "\r\n")
				}
			}
			cp := blk.Clone()
			cp.ID = model.NewBlockID()
			cp.Dirty = true
			cp.CleanSource = ""
			return strings.TrimRight(s.serializeBlocks([]model.Block{cp}), "\r\n")
		}
		return s.SelectedMarkdown()
	}
	return s.SelectedMarkdown()
}

func hasAnySpanStyle(spans []model.Span) bool {
	for _, sp := range spans {
		if sp.Marks != 0 || sp.Link != "" || sp.ImageSrc != "" || sp.FGColor != "" || sp.BGColor != "" {
			return true
		}
	}
	return false
}

func splitInlineSpansForCopy(spans []model.Span) [][]model.Span {
	lines := [][]model.Span{{}}
	for _, sp := range spans {
		styled := sp.Marks != 0 || sp.Link != "" || sp.ImageSrc != "" || sp.FGColor != "" || sp.BGColor != ""
		if styled || !strings.ContainsRune(sp.Text, '\n') {
			lines[len(lines)-1] = append(lines[len(lines)-1], sp)
			continue
		}
		parts := strings.Split(sp.Text, "\n")
		for idx, part := range parts {
			if idx > 0 {
				lines = append(lines, []model.Span{})
			}
			if part != "" {
				cp := sp
				cp.Text = part
				lines[len(lines)-1] = append(lines[len(lines)-1], cp)
			}
		}
	}
	return lines
}

func serializeInlineBlockMarkdown(spans []model.Span, codec model.MarkdownCodec) string {
	if len(spans) == 0 {
		return ""
	}
	rawText := model.PlainTextOfSpans(spans)
	if rawText == "" {
		return ""
	}
	if strings.TrimSpace(rawText) == "" && !hasAnySpanStyle(spans) {
		return rawText
	}
	lineChunks := splitInlineSpansForCopy(spans)
	if len(lineChunks) == 1 {
		if ic, ok := codec.(interface{ SerializeInline([]model.Span) string }); ok {
			return ic.SerializeInline(spans)
		}
		blk := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Spans: spans, Dirty: true}
		if codec != nil {
			md := codec.Serialize(&model.Doc{Blocks: []model.Block{blk}, TrailingLine: "\n"})
			return strings.TrimSuffix(md, "\n")
		}
		return serializeSpansFallback(spans)
	}
	parts := make([]string, 0, len(lineChunks))
	for _, chunk := range lineChunks {
		if len(chunk) == 0 {
			parts = append(parts, "")
			continue
		}
		parts = append(parts, serializeInlineBlockMarkdown(chunk, codec))
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
					sub := sliceSpansForClipboard(cell.Spans, ovLo, ovHi)
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
		cell.Spans = sliceSpansForClipboard(cell.Spans, h.lo, h.hi)
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
		if cp.Kind == model.BlockNumbered {
			nextIdx := s.numberedListIndex(idx)
			if nextIdx != cp.ListIndex {
				cp.Dirty = true
				cp.CleanSource = ""
			}
			cp.ListIndex = nextIdx
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
	spans := sliceSpansForClipboard(blk.Spans, lo, hi)
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
		if b.Indent != orig {
			b.Dirty = true
			b.CleanSource = ""
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
		if sp.Marks&model.MarkMath != 0 {
			t = "$" + t + "$"
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
		if sp.Marks&model.MarkSuperscript != 0 {
			t = "^" + t + "^"
		}
		if sp.Marks&model.MarkSubscript != 0 {
			t = "~" + t + "~"
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
		"\uFEFF", "",
		"\u00A0", " ",
	).Replace(cleaned)
}

func (s *Session) isTablePasteTarget() bool {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	if s.hasMultiBlockSelection() {
		return false
	}
	if s.hasNonEmptySelection() {
		a, b := s.orderedSelection()
		if a.Block == b.Block && a.Block >= 0 && a.Block < len(s.doc.Blocks) {
			return s.doc.Blocks[a.Block].Kind == model.BlockTable && s.doc.Blocks[a.Block].Table != nil
		}
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
	if s.hasMultiCellTableSelection() || (s.hasWholeAtomicSelection() && s.isTablePasteTarget()) {
		a, b := s.orderedSelection()
		idx := a.Block
		if idx < 0 || idx >= len(s.doc.Blocks) || s.doc.Blocks[idx].Table == nil {
			return
		}
		blk := &s.doc.Blocks[idx]
		startR, startC := 0, 0
		if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, a.Offset); ok {
			startR, startC = row, col
		}
		if !s.canFitTablePaste(blk.Table, startR, startC, norm, false, true) {
			return
		}
		s.pushUndo()
		clearTableRange(blk.Table, a.Offset, b.Offset, "")
		s.focusedBlock = idx
		s.tableActiveBlock = blk.ID
		s.tableActiveRow = startR
		s.tableActiveCol = startC
		s.selAnchor = Position{Block: idx, Offset: a.Offset}
		s.selHead = s.selAnchor
		s.pasteIntoTableBlockOpts(idx, norm, false, true)
		return
	}
	idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	if s.doc.Blocks[idx].Kind == model.BlockTable && s.doc.Blocks[idx].Table != nil {
		tbl := s.doc.Blocks[idx].Table
		startR := clampInt(s.tableActiveRow, 0, max(len(tbl.Rows)-1, 0))
		startC := 0
		if len(tbl.Rows) > 0 {
			startC = clampInt(s.tableActiveCol, 0, max(len(tbl.Rows[0])-1, 0))
		}
		if !s.canFitTablePaste(tbl, startR, startC, norm, false, true) {
			return
		}
		s.pushUndo()
		s.firstLineDeleteArmed = false
		s.pasteIntoTableBlockOpts(idx, norm, false, true)
	}
}

func (s *Session) PasteText(raw string) {
	if len(raw) > media.MaxPasteBytes || s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 {
		return
	}
	s.clearAutoConvertState()
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
	// Validate table paste dimensions BEFORE pushing undo so rejected oversized pastes never pollute undo history.
	if b.Kind == model.BlockTable && b.Table != nil && strings.ContainsAny(norm, "\t\n") {
		startR := clampInt(s.tableActiveRow, 0, max(len(b.Table.Rows)-1, 0))
		startC := 0
		if len(b.Table.Rows) > 0 {
			startC = clampInt(s.tableActiveCol, 0, max(len(b.Table.Rows[0])-1, 0))
		}
		if !s.canFitTablePaste(b.Table, startR, startC, norm, false, false) {
			return
		}
	}
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

	// Single-line text without block markers inserts inline at selection (preserving leading/trailing spaces).
	if !strings.Contains(norm, "\n") && !looksLikeMarkdownBlock(norm) && b.Kind != model.BlockDivider && b.Kind != model.BlockImage {
		if !looksLikeInlineMarkdown(norm) {
			s.replaceCurrentSelectionInBlock(idx, norm)
		} else {
			s.replaceSelectionWithSpansInBlock(idx, s.parseInlineSpans(norm))
		}
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
	if len(filtered) == 1 && filtered[0].Kind == model.BlockImage &&
		filtered[0].ImageWidth == 0 && (filtered[0].ImageAlign == "" || filtered[0].ImageAlign == model.ImageAlignLeft) && filtered[0].ImageCrop.IsZero() &&
		b.Kind != model.BlockDivider && b.Kind != model.BlockImage && b.Kind != model.BlockTable && b.Kind != model.BlockCode && b.Kind != model.BlockRawHTML {
		curLen := len([]rune(b.PlainText()))
		a, endPos := s.orderedSelection()
		lo, hi := curLen, curLen
		if a.Block == idx && endPos.Block == idx {
			lo = clampInt(a.Offset, 0, curLen)
			hi = clampInt(endPos.Offset, lo, curLen)
		}
		if lo > 0 || hi < curLen {
			img := filtered[0]
			label := img.ImageAlt
			altEmpty := label == ""
			if altEmpty {
				label = fallbackInlineImageAlt(img.ImageSrc)
			}
			s.replaceSelectionWithSpansInBlock(idx, []model.Span{{
				Text:          label,
				ImageSrc:      img.ImageSrc,
				ImageTitle:    img.ImageTitle,
				ImageAltEmpty: altEmpty,
			}})
			return
		}
	}

	s.splicePastedBlocksAtSelection(idx, filtered)
}

// extractHTMLComments collects real inline HTML comments ("<!-- ... -->") in order
// from a clean Markdown source line, skipping backslash escapes and inline backtick
// code spans so literal comment text is never duplicated as a hidden comment.
func extractHTMLComments(cleanSrc string) string {
	if !strings.Contains(cleanSrc, "<!--") {
		return ""
	}
	var out strings.Builder
	n := len(cleanSrc)
	for i := 0; i < n; {
		if cleanSrc[i] == '\\' && i+1 < n {
			i += 2
			continue
		}
		if cleanSrc[i] == '`' {
			ticks := 0
			for i+ticks < n && cleanSrc[i+ticks] == '`' {
				ticks++
			}
			j := i + ticks
			closed := false
			for j < n {
				if cleanSrc[j] == '`' {
					run := 0
					for j+run < n && cleanSrc[j+run] == '`' {
						run++
					}
					if run == ticks {
						i = j + run
						closed = true
						break
					}
					j += run
					continue
				}
				j++
			}
			if !closed {
				i += ticks
			}
			continue
		}
		if strings.HasPrefix(cleanSrc[i:], "<!--") {
			j := strings.Index(cleanSrc[i+4:], "-->")
			if j < 0 {
				break
			}
			end := i + 4 + j + 3
			out.WriteString(cleanSrc[i:end])
			i = end
			continue
		}
		i++
	}
	return out.String()
}

// isUnclosedRawHTML reports whether raw ends inside an unclosed CommonMark
// HTML block (types 1-5) so splicing a new Markdown block right after it
// would be swallowed into the raw HTML block on reload.
func isUnclosedRawHTML(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	norm := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	kind := ""
	for _, line := range strings.Split(norm, "\n") {
		if kind == "" {
			trimmed := strings.TrimLeft(line, " ")
			if len(line)-len(trimmed) > 3 {
				continue
			}
			low := strings.ToLower(trimmed)
			if tag := htmlType1OpenTag(low); tag != "" {
				kind = "tag:" + tag
			} else {
				switch {
				case strings.HasPrefix(trimmed, "<!--"):
					kind = "comment"
				case strings.HasPrefix(trimmed, "<?"):
					kind = "pi"
				case strings.HasPrefix(trimmed, "<![CDATA["):
					kind = "cdata"
				case len(trimmed) >= 3 && strings.HasPrefix(trimmed, "<!") &&
					((trimmed[2] >= 'A' && trimmed[2] <= 'Z') || (trimmed[2] >= 'a' && trimmed[2] <= 'z')):
					kind = "decl"
				}
			}
		}
		if kind != "" && htmlRawLineCloses(kind, line) {
			kind = ""
		}
	}
	return kind != ""
}

func htmlType1OpenTag(low string) string {
	for _, tag := range []string{"pre", "script", "style", "textarea", "svg", "iframe"} {
		prefix := "<" + tag
		if strings.HasPrefix(low, prefix) {
			rest := low[len(prefix):]
			if rest == "" || rest[0] == '>' || rest[0] == '/' || rest[0] == ' ' || rest[0] == '\t' {
				if strings.HasPrefix(rest, "/>") || (strings.Contains(rest, "/>") && !strings.ContainsRune(rest, '>')) {
					return ""
				}
				return tag
			}
		}
	}
	return ""
}

// blockHasUnclosedRawHTML checks RawHTML blocks (and <pre>-origin BlockCode
// CleanSource) for unclosed HTML blocks that would swallow following blocks.
func blockHasUnclosedRawHTML(blk *model.Block) bool {
	if blk == nil {
		return false
	}
	if blk.Kind == model.BlockRawHTML {
		if !blk.Dirty && blk.CleanSource != "" && isUnclosedRawHTML(blk.CleanSource) {
			return true
		}
		return isUnclosedRawHTML(blk.Code)
	}
	if blk.Kind == model.BlockCode && !blk.Dirty && blk.CleanSource != "" {
		first := blk.CleanSource
		if nl := strings.IndexByte(first, '\n'); nl >= 0 {
			first = first[:nl]
		}
		trimmed := strings.ToLower(strings.TrimLeft(first, " "))
		if len(first)-len(trimmed) <= 3 && strings.HasPrefix(trimmed, "<pre") {
			return isUnclosedRawHTML(blk.CleanSource)
		}
	}
	return false
}

// blockHasUnclosedCodeFence reports whether a non-dirty block's CleanSource opens
// a CommonMark fenced code block without a matching closing fence line.
func blockHasUnclosedCodeFence(blk *model.Block) bool {
	if blk == nil || blk.Dirty || blk.CleanSource == "" {
		return false
	}
	norm := strings.ReplaceAll(strings.ReplaceAll(blk.CleanSource, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(strings.TrimRight(norm, "\n"), "\n")
	if len(lines) == 0 {
		return false
	}
	first := lines[0]
	trimmed := strings.TrimLeft(first, " ")
	if len(first)-len(trimmed) > 3 || len(trimmed) < 3 {
		return false
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return false
	}
	fenceLen := 0
	for fenceLen < len(trimmed) && trimmed[fenceLen] == ch {
		fenceLen++
	}
	if fenceLen < 3 {
		return false
	}
	if ch == '`' && strings.ContainsRune(trimmed[fenceLen:], '`') {
		return false
	}
	for _, line := range lines[1:] {
		lt := strings.TrimLeft(line, " ")
		if len(line)-len(lt) > 3 || len(lt) < fenceLen {
			continue
		}
		n := 0
		for n < len(lt) && lt[n] == ch {
			n++
		}
		if n >= fenceLen && strings.Trim(lt[n:], " \t") == "" {
			return false
		}
	}
	return true
}

const blockSwallowSentinel = "BLOCKSWALLOWSENTINEL9F3A7C2E"

func (s *Session) blockSwallowsFollowingBlocks(blk *model.Block) bool {
	if blk == nil {
		return false
	}
	if blockHasUnclosedRawHTML(blk) || blockHasUnclosedCodeFence(blk) {
		return true
	}
	if s.codec != nil {
		seg := strings.TrimRight(s.serializeBlocks([]model.Block{blk.Clone()}), "\r\n")
		if seg == "" {
			return false
		}
		probe := seg + "\n\n" + blockSwallowSentinel + "\n"
		parsed := s.parseMarkdownDoc(probe)
		if parsed == nil || len(parsed.Blocks) < 2 {
			return true
		}
		last := parsed.Blocks[len(parsed.Blocks)-1]
		if last.Kind != model.BlockParagraph || last.Indent != 0 || last.QuoteDepth != 0 || strings.TrimSpace(last.PlainText()) != blockSwallowSentinel {
			return true
		}
		return false
	}
	return false
}

func isFrontmatterFenceLine(line string) bool {
	trimmed := strings.TrimRight(strings.TrimSuffix(line, "\r"), " \t")
	return trimmed == "---"
}

func (s *Session) pairCorruptsBoundary(left, right *model.Block) bool {
	if left == nil || right == nil || s.codec == nil {
		return false
	}
	dummy := model.Block{
		ID:    model.NewBlockID(),
		Kind:  model.BlockParagraph,
		Spans: []model.Span{{Text: blockSwallowSentinel}},
		Dirty: true,
	}
	pL := s.parseMarkdownDoc(s.serializeBlocks([]model.Block{dummy, left.Clone()}))
	pR := s.parseMarkdownDoc(s.serializeBlocks([]model.Block{dummy, right.Clone()}))
	if pL == nil || pR == nil || len(pL.Blocks) != 2 || len(pR.Blocks) != 2 {
		return false
	}
	if pL.Blocks[1].Kind != left.Kind || pR.Blocks[1].Kind != right.Kind {
		return false
	}
	pBoth := s.parseMarkdownDoc(s.serializeBlocks([]model.Block{dummy, left.Clone(), right.Clone()}))
	if pBoth == nil || len(pBoth.Blocks) != 3 {
		return true
	}
	gotR := pBoth.Blocks[2]
	expR := pR.Blocks[1]
	return gotR.Kind != expR.Kind ||
		gotR.Language != expR.Language ||
		gotR.QuoteDepth != expR.QuoteDepth ||
		gotR.Code != expR.Code ||
		gotR.PlainText() != expR.PlainText()
}

// wouldSpliceCorruptBoundaries checks whether replacing s.doc.Blocks[replaceStart:replaceEnd]
// with replacement would cause any block to swallow or reclassify adjacent blocks on reload
// (such as unclosed HTML/fences, matching closing tags like <script>...</script>, or a
// document-start "---" divider turning trailing blocks into yaml-frontmatter).
func (s *Session) wouldSpliceCorruptBoundaries(replaceStart, replaceEnd int, replacement []model.Block) bool {
	if s.doc == nil {
		return false
	}
	replaceStart = clampInt(replaceStart, 0, len(s.doc.Blocks))
	replaceEnd = clampInt(replaceEnd, replaceStart, len(s.doc.Blocks))
	suffix := s.doc.Blocks[replaceEnd:]

	for i := range replacement {
		hasNext := i+1 < len(replacement) || len(suffix) > 0
		if hasNext && s.blockSwallowsFollowingBlocks(&replacement[i]) {
			return true
		}
		if i+1 < len(replacement) && s.pairCorruptsBoundary(&replacement[i], &replacement[i+1]) {
			return true
		}
	}
	if len(replacement) > 0 && len(suffix) > 0 {
		if s.pairCorruptsBoundary(&replacement[len(replacement)-1], &suffix[0]) {
			return true
		}
	}
	if replaceStart > 0 {
		prev := &s.doc.Blocks[replaceStart-1]
		if len(replacement) > 0 {
			if s.blockSwallowsFollowingBlocks(prev) || s.pairCorruptsBoundary(prev, &replacement[0]) {
				return true
			}
		} else if len(suffix) > 0 {
			if s.blockSwallowsFollowingBlocks(prev) || s.pairCorruptsBoundary(prev, &suffix[0]) {
				return true
			}
		}
	}
	if replaceStart == 0 && s.codec != nil {
		headLen := len(replacement) + len(suffix)
		if headLen >= 2 {
			head := make([]model.Block, 0, headLen)
			for i := range replacement {
				head = append(head, replacement[i].Clone())
			}
			for i := range suffix {
				head = append(head, suffix[i].Clone())
			}
			if !(head[0].Kind == model.BlockCode && head[0].Language == "yaml-frontmatter") {
				serialized := s.serializeBlocks(head)
				firstLine := serialized
				if nl := strings.IndexByte(firstLine, '\n'); nl >= 0 {
					firstLine = firstLine[:nl]
				}
				if isFrontmatterFenceLine(firstLine) {
					if parsed := s.parseMarkdownDoc(serialized); parsed != nil && len(parsed.Blocks) > 0 &&
						parsed.Blocks[0].Kind == model.BlockCode && parsed.Blocks[0].Language == "yaml-frontmatter" {
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *Session) rawHTMLPrefixCaret(prefixRaw string, repBlocks []model.Block, baseIdx, lo, cleanLen int) (int, int) {
	if len(repBlocks) == 1 && repBlocks[0].Kind == model.BlockRawHTML {
		return baseIdx, clampInt(lo+cleanLen, 0, s.blockSelectionLen(baseIdx))
	}
	prefixBlocks := s.reparsePartialRawHTML(prefixRaw)
	if len(prefixBlocks) == 0 {
		return baseIdx, 0
	}
	rel := min(len(prefixBlocks)-1, len(repBlocks)-1)
	targetIdx := baseIdx + rel
	lastPrefix := prefixBlocks[len(prefixBlocks)-1]
	off := utf8.RuneCountInString(lastPrefix.PlainText())
	if lastPrefix.Kind == model.BlockCode || lastPrefix.Kind == model.BlockRawHTML {
		off = utf8.RuneCountInString(lastPrefix.Code)
	}
	return targetIdx, clampInt(off, 0, s.blockSelectionLen(targetIdx))
}

func firstTableCellInRange(tbl *model.TableData, startRune, endRune int) (row, col, relOff int, ok bool) {
	if tbl == nil || startRune > endRune {
		return 0, 0, 0, false
	}
	pos := 0
	emptyRow, emptyCol := -1, -1
	for rIdx, r := range tbl.Rows {
		for cIdx, cell := range r {
			clen := utf8.RuneCountInString(cell.PlainText())
			cStart, cEnd := pos, pos+clen
			ovLo := max(startRune, cStart) - cStart
			ovHi := min(endRune, cEnd) - cStart
			if ovLo < ovHi {
				return rIdx, cIdx, ovLo, true
			}
			if emptyRow < 0 && startRune <= cStart && cStart <= endRune && clen == 0 {
				emptyRow, emptyCol = rIdx, cIdx
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
	if emptyRow >= 0 {
		return emptyRow, emptyCol, 0, true
	}
	return 0, 0, 0, false
}

func (s *Session) reparsePartialRawHTML(raw string) []model.Block {
	if s.codec == nil {
		if raw == "" {
			return nil
		}
		return []model.Block{{ID: model.NewBlockID(), Kind: model.BlockRawHTML, Code: raw, Dirty: true}}
	}
	parsed := s.parseMarkdownDoc(raw)
	if parsed == nil {
		return nil
	}
	out := make([]model.Block, 0, len(parsed.Blocks))
	for _, blk := range parsed.Blocks {
		if blk.Kind == model.BlockParagraph && blk.Indent == 0 && len(blk.Spans) == 0 && strings.TrimSpace(blk.CleanSource) == "" {
			continue
		}
		blk.ID = model.NewBlockID()
		if blk.CleanSource == "" {
			blk.Dirty = true
		} else {
			blk.Dirty = false
		}
		out = append(out, blk)
	}
	if len(out) > 0 {
		if exp, ok := s.codec.(interface{ ExpandBlockRefCleanSources([]model.Block, string) }); ok {
			exp.ExpandBlockRefCleanSources(out, raw)
		}
	}
	return out
}

func tablesCellsIdentical(a, b *model.TableData) bool {
	if a == nil || b == nil || len(a.Rows) != len(b.Rows) {
		return false
	}
	for r := range a.Rows {
		if len(a.Rows[r]) != len(b.Rows[r]) {
			return false
		}
		for c := range a.Rows[r] {
			if !spansIdentical(normalizeSpans(a.Rows[r][c].Spans), normalizeSpans(b.Rows[r][c].Spans)) {
				return false
			}
		}
	}
	return true
}

func spansIdentical(a, b []model.Span) bool {
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

func (s *Session) blocksStructurallyEqual(a, b *model.Block) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Kind != b.Kind || a.Indent != b.Indent || a.QuoteDepth != b.QuoteDepth ||
		a.Checked != b.Checked || a.Callout != b.Callout || a.Language != b.Language ||
		a.Code != b.Code || a.ImageSrc != b.ImageSrc || a.ImageAlt != b.ImageAlt ||
		!spansIdentical(a.Spans, b.Spans) {
		return false
	}
	if s.codec == nil {
		return a.CleanSource == b.CleanSource
	}
	sa := strings.TrimRight(s.serializeBlocks([]model.Block{a.Clone()}), "\r\n")
	sb := strings.TrimRight(s.serializeBlocks([]model.Block{b.Clone()}), "\r\n")
	return sa == sb
}

func tableGridDims(raw string) (rows, cols int) {
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	rows = len(lines)
	for _, line := range lines {
		c := strings.Count(line, "\t") + 1
		if c > cols {
			cols = c
		}
	}
	return rows, cols
}

func htmlRawLineCloses(kind, line string) bool {
	if strings.HasPrefix(kind, "tag:") {
		tag := strings.TrimPrefix(kind, "tag:")
		return strings.Contains(strings.ToLower(line), "</"+tag+">")
	}
	switch kind {
	case "comment":
		return strings.Contains(line, "-->")
	case "pi":
		return strings.Contains(line, "?>")
	case "cdata":
		return strings.Contains(line, "]]>")
	case "decl":
		return strings.Contains(line, ">")
	default:
		return false
	}
}

// preserveBlockComments reattaches hidden HTML comments from origClean onto a
// cleanly re-serialized block so SerializeMarkdown retains them verbatim.
func (s *Session) preserveBlockComments(blk *model.Block, origClean string) {
	if blk == nil || s.codec == nil {
		return
	}
	comments := extractHTMLComments(origClean)
	if comments == "" {
		return
	}
	tmp := blk.Clone()
	tmp.Dirty = true
	tmp.CleanSource = ""
	ser := strings.TrimRight(s.codec.Serialize(&model.Doc{Blocks: []model.Block{tmp}}), "\r\n")
	if ser == "" && blk.PlainText() == "" {
		return
	}
	blk.CleanSource = ser + comments
	blk.Dirty = false
}

// InsertMarkdown inserts Markdown-formatted content from a plugin or host action
// into a Markdown note, rendering native blocks immediately while preserving
// each newly parsed block's CleanSource so invisible HTML comments (e.g. plugin
// markers) survive serialization verbatim. When replaceSelection is false, any
// active selection is collapsed to its end without deleting selected text (and
// Undo restores the original pre-insert selection).
func (s *Session) InsertMarkdown(source string, replaceSelection bool) bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 || len(source) > media.MaxPasteBytes {
		return false
	}

	clean := strings.TrimPrefix(source, "\uFEFF")
	clean = strings.ReplaceAll(strings.ReplaceAll(clean, "\r\n", "\n"), "\r", "\n")

	// Compute effective selection range WITHOUT mutating s.selAnchor/s.selHead or
	// flushing overlays yet so pushUndo() records the pre-insert selection
	// (including reverse selections) and failed/no-op calls have zero side effects.
	rawA, rawB := s.orderedSelection()
	if rawA.Block < 0 || rawB.Block >= len(s.doc.Blocks) {
		return false
	}
	hadNonEmpty := s.hasNonEmptySelection()
	a, b := rawA, rawB
	if !replaceSelection && hadNonEmpty {
		a = rawB
		b = rawB
	}
	first, last := a.Block, b.Block
	if first != last && s.crossesHiddenFold(first, last) && !s.selectionCoversWholeDoc(a, b) {
		return false
	}

	isRichKind := func(k model.BlockKind) bool {
		return k != model.BlockTable && k != model.BlockImage && k != model.BlockDivider && k != model.BlockCode && k != model.BlockRawHTML
	}
	beginMutation := func() {
		s.pushUndo()
		s.firstLineDeleteArmed = false
		s.clearAutoConvertState()
		s.closeNonAnnotationOverlays()
	}

	parsed := s.parseMarkdownDoc(clean)
	blocks := make([]model.Block, 0)
	if clean != "" && parsed != nil {
		for _, blk := range parsed.Blocks {
			if blk.Kind == model.BlockParagraph && blk.Indent == 0 && len(blk.Spans) == 0 && strings.TrimSpace(blk.CleanSource) == "" {
				continue
			}
			blk.ID = model.NewBlockID()
			if blk.CleanSource == "" {
				blk.Dirty = true
			} else {
				blk.Dirty = false
			}
			blocks = append(blocks, blk)
		}
		if len(blocks) > 0 && s.codec != nil {
			if exp, ok := s.codec.(interface{ ExpandBlockRefCleanSources([]model.Block, string) }); ok {
				exp.ExpandBlockRefCleanSources(blocks, clean)
			}
		}
	}

	// Empty replacement (delete selection while preserving untouched endpoints
	// and hidden HTML comments on partially retained rich-text blocks).
	if len(blocks) == 0 {
		if !replaceSelection || !hadNonEmpty {
			return false
		}
		if first < last {
			firstLen := s.blockSelectionLen(first)
			if firstLen > 0 && a.Offset >= firstLen && !(last == first+1 && b.Offset == 0) {
				first++
				a = Position{Block: first, Offset: 0}
			}
			if first < last && b.Offset == 0 && !(last == first+1 && a.Offset >= s.blockSelectionLen(first)) {
				last--
				b = Position{Block: last, Offset: s.blockSelectionLen(last)}
			}
		}
		if first == last {
			blk := &s.doc.Blocks[first]
			rlen := s.blockSelectionLen(first)
			if a.Offset == b.Offset {
				return false
			}
			whole := a.Offset == 0 && b.Offset >= rlen
			if whole && (blk.Kind == model.BlockImage || blk.Kind == model.BlockDivider ||
				(blk.Kind == model.BlockTable && rlen > 0 && !s.isSingleCellTableActive(blk)) ||
				s.isPreviewOnlyCodeBlock(first) ||
				(rawA.Block < rawB.Block && len(s.doc.Blocks) > 1)) {
				if len(s.doc.Blocks) > 1 && s.wouldSpliceCorruptBoundaries(first, first+1, nil) {
					return false
				}
				beginMutation()
				if len(s.doc.Blocks) == 1 {
					nb := model.Block{ID: blk.ID, Kind: model.BlockParagraph, Dirty: true}
					s.doc.Blocks[first] = nb
					s.focusedBlock = first
					s.selAnchor = Position{Block: first, Offset: 0}
					s.selHead = s.selAnchor
				} else {
					s.spliceBlocks(first, first+1, nil)
					nextIdx := min(first, len(s.doc.Blocks)-1)
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
				r1, c1, relOff1, ok1 := firstTableCellInRange(blk.Table, a.Offset, b.Offset)
				beginMutation()
				clearTableRange(blk.Table, a.Offset, b.Offset, "")
				blk.Dirty = true
				blk.CleanSource = ""
				s.focusedBlock = first
				caret := a.Offset
				if ok1 && r1 < len(blk.Table.Rows) && c1 < len(blk.Table.Rows[r1]) {
					s.tableActiveBlock = blk.ID
					s.tableActiveRow = r1
					s.tableActiveCol = c1
					cellLen := utf8.RuneCountInString(blk.Table.Rows[r1][c1].PlainText())
					caret = tableCellStartOffset(blk.Table, r1, c1) + clampInt(relOff1, 0, cellLen)
				} else if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, a.Offset); ok {
					s.tableActiveBlock = blk.ID
					s.tableActiveRow = row
					s.tableActiveCol = col
				}
				s.selAnchor = Position{Block: first, Offset: caret}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.multiSelFocusPending = false
				s.markDirty()
				return true
			}
			if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML {
				runes := []rune(blk.Code)
				lo := clampInt(a.Offset, 0, len(runes))
				hi := clampInt(b.Offset, lo, len(runes))
				newCode := string(runes[:lo]) + string(runes[hi:])
				if blk.Kind == model.BlockRawHTML {
					repBlocks := s.reparsePartialRawHTML(newCode)
					if len(repBlocks) == 0 {
						if len(s.doc.Blocks) > 1 && s.wouldSpliceCorruptBoundaries(first, first+1, nil) {
							return false
						}
					} else if s.wouldSpliceCorruptBoundaries(first, first+1, repBlocks) {
						return false
					}
					beginMutation()
					if len(repBlocks) == 0 {
						if len(s.doc.Blocks) == 1 {
							s.doc.Blocks[first] = model.Block{ID: blk.ID, Kind: model.BlockParagraph, Dirty: true}
							s.focusedBlock = first
							s.selAnchor = Position{Block: first, Offset: 0}
						} else {
							s.spliceBlocks(first, first+1, nil)
							nextIdx := min(first, len(s.doc.Blocks)-1)
							s.focusedBlock = nextIdx
							s.selAnchor = Position{Block: nextIdx, Offset: 0}
						}
					} else {
						s.spliceBlocks(first, first+1, repBlocks)
						targetIdx, caret := s.rawHTMLPrefixCaret(string(runes[:lo]), repBlocks, first, lo, 0)
						s.focusedBlock = targetIdx
						s.selAnchor = Position{Block: targetIdx, Offset: caret}
					}
					s.selHead = s.selAnchor
					s.focusPending = true
					s.selSyncPending = true
					s.multiSelFocusPending = false
					s.markDirty()
					return true
				}
				cp := blk.Clone()
				cp.SetText(newCode)
				if s.wouldSpliceCorruptBoundaries(first, first+1, []model.Block{cp}) {
					return false
				}
				beginMutation()
				s.selAnchor = a
				s.selHead = b
				s.focusedBlock = first
				s.replaceCurrentSelectionInBlock(first, "")
				return true
			}
			origClean := ""
			if !blk.Dirty {
				origClean = blk.CleanSource
			}
			prefix := sliceSpans(blk.Spans, 0, clampInt(a.Offset, 0, rlen))
			suffix := sliceSpans(blk.Spans, clampInt(b.Offset, 0, rlen), rlen)
			updated := blk.Clone()
			updated.Spans = normalizeSpans(append(prefix, suffix...))
			updated.Dirty = true
			updated.CleanSource = ""
			if origClean != "" && !whole {
				s.preserveBlockComments(&updated, origClean)
			}
			if s.wouldSpliceCorruptBoundaries(first, first+1, []model.Block{updated}) {
				return false
			}
			beginMutation()
			s.doc.Blocks[first] = updated
			s.focusedBlock = first
			s.selAnchor = Position{Block: first, Offset: clampInt(a.Offset, 0, rlen)}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}

		// Multi-block empty replacement across [first..last].
		firstBlk := &s.doc.Blocks[first]
		lastBlk := &s.doc.Blocks[last]
		firstLen := s.blockSelectionLen(first)
		lastLen := s.blockSelectionLen(last)

		if a.Offset == 0 && b.Offset >= lastLen {
			if !(first == 0 && last == len(s.doc.Blocks)-1) && s.wouldSpliceCorruptBoundaries(first, last+1, nil) {
				return false
			}
			beginMutation()
			if first == 0 && last == len(s.doc.Blocks)-1 {
				nb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}
				s.doc.Blocks = []model.Block{nb}
				s.focusedBlock = 0
				s.selAnchor = Position{Block: 0, Offset: 0}
				s.selHead = s.selAnchor
			} else {
				s.spliceBlocks(first, last+1, nil)
				targetIdx := min(first, len(s.doc.Blocks)-1)
				off := 0
				if first > 0 && targetIdx == first-1 {
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

		if isRichKind(firstBlk.Kind) && isRichKind(lastBlk.Kind) {
			origFirstClean := ""
			if a.Offset > 0 && !firstBlk.Dirty {
				origFirstClean = firstBlk.CleanSource
			}
			origLastClean := ""
			if b.Offset < lastLen && !lastBlk.Dirty {
				origLastClean = lastBlk.CleanSource
			}
			prefix := sliceSpans(firstBlk.Spans, 0, clampInt(a.Offset, 0, firstLen))
			suffix := sliceSpans(lastBlk.Spans, clampInt(b.Offset, 0, lastLen), lastLen)
			updated := firstBlk.Clone()
			if a.Offset == 0 && b.Offset < lastLen {
				updated.Kind = lastBlk.Kind
				updated.Indent = lastBlk.Indent
				updated.Callout = lastBlk.Callout
				updated.Checked = lastBlk.Checked
				updated.QuoteDepth = lastBlk.QuoteDepth
				updated.QuoteNewGroup = lastBlk.QuoteNewGroup
				updated.QuoteTightBefore = lastBlk.QuoteTightBefore
			}
			updated.Spans = normalizeSpans(append(prefix, suffix...))
			updated.Dirty = true
			updated.CleanSource = ""
			if origFirstClean != "" || origLastClean != "" {
				s.preserveBlockComments(&updated, origFirstClean+origLastClean)
			}
			if s.wouldSpliceCorruptBoundaries(first, last+1, []model.Block{updated}) {
				return false
			}
			beginMutation()
			s.spliceBlocks(first, last+1, []model.Block{updated})
			caret := clampInt(a.Offset, 0, firstLen)
			s.focusedBlock = first
			s.selAnchor = Position{Block: first, Offset: caret}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}

		// Mixed-kind empty deletion preserving partial code/RawHTML/table/rich endpoints.
		var kept []model.Block
		caretBlockOffset := 0
		caretRuneOffset := 0
		placedCaret := false
		if a.Offset > 0 {
			switch firstBlk.Kind {
			case model.BlockCode:
				runes := []rune(firstBlk.Code)
				keep := clampInt(a.Offset, 0, len(runes))
				cp := firstBlk.Clone()
				cp.SetText(string(runes[:keep]))
				kept = append(kept, cp)
				caretBlockOffset = 0
				caretRuneOffset = keep
				placedCaret = true
			case model.BlockRawHTML:
				runes := []rune(firstBlk.Code)
				keep := clampInt(a.Offset, 0, len(runes))
				if keep >= len(runes) {
					kept = append(kept, firstBlk.Clone())
					caretBlockOffset = 0
					caretRuneOffset = keep
					placedCaret = true
				} else {
					rep := s.reparsePartialRawHTML(string(runes[:keep]))
					if len(rep) > 0 {
						kept = append(kept, rep...)
						caretBlockOffset = len(kept) - 1
						caretRuneOffset = len([]rune(rep[len(rep)-1].PlainText()))
						placedCaret = true
					}
				}
			case model.BlockTable:
				cp := firstBlk.Clone()
				if cp.Table != nil && a.Offset < firstLen {
					clearTableRange(cp.Table, a.Offset, firstLen, "")
					cp.Dirty = true
					cp.CleanSource = ""
				}
				kept = append(kept, cp)
				caretBlockOffset = 0
				caretRuneOffset = clampInt(a.Offset, 0, s.blockSelectionLen(first))
				placedCaret = true
			case model.BlockImage, model.BlockDivider:
				if a.Offset >= firstLen {
					kept = append(kept, firstBlk.Clone())
				}
			default:
				origClean := ""
				if !firstBlk.Dirty {
					origClean = firstBlk.CleanSource
				}
				cp := firstBlk.Clone()
				cp.Spans = normalizeSpans(sliceSpans(firstBlk.Spans, 0, clampInt(a.Offset, 0, firstLen)))
				cp.Dirty = true
				cp.CleanSource = ""
				if origClean != "" {
					s.preserveBlockComments(&cp, origClean)
				}
				kept = append(kept, cp)
				caretBlockOffset = 0
				caretRuneOffset = clampInt(a.Offset, 0, firstLen)
				placedCaret = true
			}
		}
		if b.Offset < lastLen {
			switch lastBlk.Kind {
			case model.BlockCode:
				if b.Offset == 0 {
					if !placedCaret {
						caretBlockOffset = len(kept)
						caretRuneOffset = 0
						placedCaret = true
					}
					kept = append(kept, lastBlk.Clone())
				} else {
					runes := []rune(lastBlk.Code)
					cut := clampInt(b.Offset, 0, len(runes))
					cp := lastBlk.Clone()
					cp.SetText(string(runes[cut:]))
					if !placedCaret {
						caretBlockOffset = len(kept)
						caretRuneOffset = 0
						placedCaret = true
					}
					kept = append(kept, cp)
				}
			case model.BlockRawHTML:
				if b.Offset == 0 {
					if !placedCaret {
						caretBlockOffset = len(kept)
						caretRuneOffset = 0
						placedCaret = true
					}
					kept = append(kept, lastBlk.Clone())
				} else {
					runes := []rune(lastBlk.Code)
					cut := clampInt(b.Offset, 0, len(runes))
					rep := s.reparsePartialRawHTML(string(runes[cut:]))
					if len(rep) > 0 {
						if !placedCaret {
							caretBlockOffset = len(kept)
							caretRuneOffset = 0
							placedCaret = true
						}
						kept = append(kept, rep...)
					}
				}
			case model.BlockTable:
				cp := lastBlk.Clone()
				if cp.Table != nil && b.Offset > 0 {
					clearTableRange(cp.Table, 0, b.Offset, "")
					cp.Dirty = true
					cp.CleanSource = ""
				}
				if !placedCaret {
					caretBlockOffset = len(kept)
					caretRuneOffset = 0
					placedCaret = true
				}
				kept = append(kept, cp)
			case model.BlockImage, model.BlockDivider:
				if b.Offset == 0 {
					if !placedCaret {
						caretBlockOffset = len(kept)
						caretRuneOffset = 0
						placedCaret = true
					}
					kept = append(kept, lastBlk.Clone())
				}
			default:
				origClean := ""
				if !lastBlk.Dirty {
					origClean = lastBlk.CleanSource
				}
				suffix := sliceSpans(lastBlk.Spans, clampInt(b.Offset, 0, lastLen), lastLen)
				if b.Offset == 0 && len(kept) == 0 {
					if !placedCaret {
						caretBlockOffset = 0
						caretRuneOffset = 0
						placedCaret = true
					}
					kept = append(kept, lastBlk.Clone())
				} else {
					cp := lastBlk.Clone()
					cp.Spans = normalizeSpans(suffix)
					cp.Dirty = true
					cp.CleanSource = ""
					if origClean != "" {
						s.preserveBlockComments(&cp, origClean)
					}
					if !placedCaret {
						caretBlockOffset = len(kept)
						caretRuneOffset = 0
						placedCaret = true
					}
					kept = append(kept, cp)
				}
			}
		}
		// Reject if any kept block would swallow or corrupt a following block on reload.
		if !(len(kept) == 0 && first == 0 && last == len(s.doc.Blocks)-1) && s.wouldSpliceCorruptBoundaries(first, last+1, kept) {
			return false
		}
		beginMutation()
		if len(kept) == 0 && first == 0 && last == len(s.doc.Blocks)-1 {
			kept = []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}}
		}
		s.spliceBlocks(first, last+1, kept)
		targetIdx := clampInt(first+caretBlockOffset, 0, len(s.doc.Blocks)-1)
		if !placedCaret && len(kept) == 0 {
			targetIdx = clampInt(first, 0, len(s.doc.Blocks)-1)
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

	// Single plain/rich paragraph without hidden HTML comments can merge inline.
	isInline := len(blocks) == 1 &&
		blocks[0].Kind == model.BlockParagraph &&
		blocks[0].Indent == 0 &&
		!strings.Contains(blocks[0].CleanSource, "<!--")
	isStructural := !isInline

	startBlk := &s.doc.Blocks[first]

	// Code / RawHTML single-block target.
	if first == last && (startBlk.Kind == model.BlockCode || startBlk.Kind == model.BlockRawHTML) {
		curLen := s.blockSelectionLen(first)
		// Non-destructive structural insert (or collapsed RawHTML boundary insert)
		// splices adjacent native blocks instead of burying Markdown inside code/HTML.
		if ((!replaceSelection || !hadNonEmpty) && isStructural) ||
			(startBlk.Kind == model.BlockRawHTML && a.Offset == b.Offset && (a.Offset == 0 || a.Offset >= curLen)) {
			insAt := first + 1
			if a.Offset == 0 && curLen > 0 {
				insAt = first
			}
			if s.wouldSpliceCorruptBoundaries(insAt, insAt, blocks) {
				return false
			}
			beginMutation()
			s.spliceBlocks(insAt, insAt, blocks)
			endIdx := insAt + len(blocks) - 1
			endOff := s.blockSelectionLen(endIdx)
			s.focusedBlock = endIdx
			s.selAnchor = Position{Block: endIdx, Offset: endOff}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}
		// Whole-block structural replacement of Code / RawHTML falls through to the
		// structural splice path below; partial structural replacement inside code/HTML is rejected.
		if isStructural {
			if replaceSelection && hadNonEmpty && a.Offset == 0 && b.Offset >= curLen {
				if strings.TrimRight(s.serializeBlocks([]model.Block{startBlk.Clone()}), "\r\n") ==
					strings.TrimRight(s.serializeBlocks(blocks), "\r\n") {
					return false
				}
			} else {
				return false
			}
		} else {
			runes := []rune(startBlk.Code)
			lo := clampInt(a.Offset, 0, len(runes))
			hi := clampInt(b.Offset, lo, len(runes))
			if replaceSelection && hadNonEmpty && string(runes[lo:hi]) == clean {
				return false
			}
			newCode := string(runes[:lo]) + clean + string(runes[hi:])
			if startBlk.Kind == model.BlockRawHTML {
				repBlocks := s.reparsePartialRawHTML(newCode)
				if len(repBlocks) == 0 {
					return false
				}
				if s.wouldSpliceCorruptBoundaries(first, first+1, repBlocks) {
					return false
				}
				beginMutation()
				s.spliceBlocks(first, first+1, repBlocks)
				endIdx, caret := s.rawHTMLPrefixCaret(string(runes[:lo])+clean, repBlocks, first, lo, utf8.RuneCountInString(clean))
				s.focusedBlock = endIdx
				s.selAnchor = Position{Block: endIdx, Offset: caret}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.multiSelFocusPending = false
				s.markDirty()
				return true
			}
			cp := startBlk.Clone()
			cp.SetText(newCode)
			if s.wouldSpliceCorruptBoundaries(first, first+1, []model.Block{cp}) {
				return false
			}
			beginMutation()
			s.selAnchor = a
			s.selHead = b
			s.focusedBlock = first
			s.replaceCurrentSelectionInBlock(first, clean)
			return true
		}
	}

	// Table single-block target.
	if first == last && startBlk.Kind == model.BlockTable && startBlk.Table != nil {
		rlen := s.blockSelectionLen(first)
		if (!replaceSelection || !hadNonEmpty) && isStructural {
			if s.wouldSpliceCorruptBoundaries(first+1, first+1, blocks) {
				return false
			}
			// Splice structural blocks cleanly right after the table without altering cells.
			beginMutation()
			s.spliceBlocks(first+1, first+1, blocks)
			endIdx := first + len(blocks)
			endOff := s.blockSelectionLen(endIdx)
			s.focusedBlock = endIdx
			s.selAnchor = Position{Block: endIdx, Offset: endOff}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}
		_, _, _, _, singleCell := tableCellAtRuneRange(startBlk.Table, a.Offset, b.Offset)
		replaceWholeTable := replaceSelection && hadNonEmpty && a.Offset == 0 && b.Offset >= rlen && rlen > 0 && !s.isSingleCellTableActive(startBlk) && (isStructural || !singleCell)
		if !replaceWholeTable {
			// Never bury structural Markdown inside an individual table cell.
			if isStructural {
				return false
			}
			trimmedCell := strings.TrimSuffix(clean, "\n")
			if strings.ContainsAny(trimmedCell, "\t\n") {
				if !replaceSelection || !hadNonEmpty {
					// Multi-cell/multi-line insert in insert-only mode would overwrite adjacent cells.
					return false
				}
				r1, c1, relLo1, _, ok1 := tableCellAtRuneRange(startBlk.Table, a.Offset, a.Offset)
				r2, c2, _, relHi2, ok2 := tableCellAtRuneRange(startBlk.Table, b.Offset, b.Offset)
				if !ok1 || !ok2 {
					return false
				}
				endCellLen := utf8.RuneCountInString(startBlk.Table.Rows[r2][c2].PlainText())
				pRows, pCols := tableGridDims(clean)
				// Only allow multi-cell replacement when every overwritten cell is fully
				// inside the active selection (never clobber unselected cell text or neighbors).
				if relLo1 != 0 || relHi2 < endCellLen || r1+pRows-1 > r2 || c1+pCols-1 > c2 {
					return false
				}
				if !s.canFitTablePaste(startBlk.Table, r1, c1, clean, true, false) {
					return false
				}
				rawRows := strings.Split(clean, "\n")
				if len(rawRows) > 1 && rawRows[len(rawRows)-1] == "" {
					rawRows = rawRows[:len(rawRows)-1]
				}
				simTbl := startBlk.Table.Clone()
				clearTableRange(simTbl, a.Offset, b.Offset, "")
				for dr, rLine := range rawRows {
					cols := strings.Split(rLine, "\t")
					for dc, cellText := range cols {
						if r1+dr < len(simTbl.Rows) && c1+dc < len(simTbl.Rows[r1+dr]) {
							if cellText == "" {
								simTbl.Rows[r1+dr][c1+dc].Spans = nil
							} else {
								simTbl.Rows[r1+dr][c1+dc].Spans = normalizeSpans(s.parseInlineSpans(cellText))
							}
						}
					}
				}
				if tablesCellsIdentical(startBlk.Table, simTbl) {
					return false
				}
				beginMutation()
				for r := range startBlk.Table.Rows {
					for c := range startBlk.Table.Rows[r] {
						startBlk.Table.Rows[r][c].Spans = simTbl.Rows[r][c].Spans
					}
				}
				startBlk.Dirty = true
				startBlk.CleanSource = ""
				endRow := r1 + len(rawRows) - 1
				lastCols := strings.Split(rawRows[len(rawRows)-1], "\t")
				endCol := c1 + len(lastCols) - 1
				s.focusedBlock = first
				s.tableActiveBlock = startBlk.ID
				s.tableActiveRow = endRow
				s.tableActiveCol = endCol
				endOff := tableCellStartOffset(startBlk.Table, endRow, endCol) +
					utf8.RuneCountInString(startBlk.Table.Rows[endRow][endCol].PlainText())
				s.selAnchor = Position{Block: first, Offset: endOff}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.multiSelFocusPending = false
				s.markDirty()
				return true
			}
			// Single-line text inside table cell(s): update exact target cell at (a.Offset, b.Offset).
			row, col, relLo, relHi, ok := tableCellAtRuneRange(startBlk.Table, a.Offset, b.Offset)
			if !ok {
				if !replaceSelection {
					return false
				}
				r1, c1, relOff1, ok1 := firstTableCellInRange(startBlk.Table, a.Offset, b.Offset)
				simTbl := startBlk.Table.Clone()
				clearTableRange(simTbl, a.Offset, b.Offset, "")
				insPlainLen := utf8.RuneCountInString(blocks[0].PlainText())
				if !ok1 {
					r1, c1, relOff1, _, ok1 = tableCellAtRuneRange(simTbl, a.Offset, a.Offset)
				}
				if ok1 && len(blocks[0].Spans) > 0 && r1 < len(simTbl.Rows) && c1 < len(simTbl.Rows[r1]) {
					c := &simTbl.Rows[r1][c1]
					clen := utf8.RuneCountInString(c.PlainText())
					insAt := clampInt(relOff1, 0, clen)
					var m []model.Span
					m = append(m, sliceSpans(c.Spans, 0, insAt)...)
					m = append(m, blocks[0].Spans...)
					m = append(m, sliceSpans(c.Spans, insAt, clen)...)
					c.Spans = normalizeSpans(m)
					relOff1 = insAt
				}
				if hadNonEmpty && tablesCellsIdentical(startBlk.Table, simTbl) {
					return false
				}
				beginMutation()
				for r := range startBlk.Table.Rows {
					for c := range startBlk.Table.Rows[r] {
						startBlk.Table.Rows[r][c].Spans = simTbl.Rows[r][c].Spans
					}
				}
				startBlk.Dirty = true
				startBlk.CleanSource = ""
				caret := a.Offset + insPlainLen
				s.focusedBlock = first
				if ok1 && r1 < len(startBlk.Table.Rows) && c1 < len(startBlk.Table.Rows[r1]) {
					s.tableActiveBlock = startBlk.ID
					s.tableActiveRow = r1
					s.tableActiveCol = c1
					caret = tableCellStartOffset(startBlk.Table, r1, c1) + relOff1 + insPlainLen
				} else if r2, c2, _, _, ok2 := tableCellAtRuneRange(startBlk.Table, caret, caret); ok2 {
					s.tableActiveBlock = startBlk.ID
					s.tableActiveRow = r2
					s.tableActiveCol = c2
				}
				s.selAnchor = Position{Block: first, Offset: caret}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.multiSelFocusPending = false
				s.markDirty()
				return true
			}
			cell := &startBlk.Table.Rows[row][col]
			cellLen := utf8.RuneCountInString(cell.PlainText())
			var merged []model.Span
			merged = append(merged, sliceSpans(cell.Spans, 0, relLo)...)
			merged = append(merged, blocks[0].Spans...)
			merged = append(merged, sliceSpans(cell.Spans, relHi, cellLen)...)
			normMerged := normalizeSpans(merged)
			if replaceSelection && hadNonEmpty && spansIdentical(cell.Spans, normMerged) {
				return false
			}
			beginMutation()
			cell.Spans = normMerged
			startBlk.Dirty = true
			startBlk.CleanSource = ""
			caret := a.Offset + utf8.RuneCountInString(blocks[0].PlainText())
			s.focusedBlock = first
			s.tableActiveBlock = startBlk.ID
			s.tableActiveRow = row
			s.tableActiveCol = col
			s.selAnchor = Position{Block: first, Offset: caret}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}
	}

	// Trim unselected cross-block endpoints so selecting up to offset 0 of a table/code/image/divider
	// (or from the very end of a leading block) never deletes the untouched endpoint block.
	if first < last {
		firstLen := s.blockSelectionLen(first)
		if firstLen > 0 && a.Offset >= firstLen {
			first++
			a = Position{Block: first, Offset: 0}
		}
		if first < last && b.Offset == 0 {
			last--
			b = Position{Block: last, Offset: s.blockSelectionLen(last)}
		}
	}

	// Inline merge across rich-text blocks (preserving hidden HTML comments from clean sources).
	if isInline {
		allRich := true
		for i := first; i <= last; i++ {
			if !isRichKind(s.doc.Blocks[i].Kind) {
				allRich = false
				break
			}
		}
		if allRich {
			origFirstClean := ""
			if !s.doc.Blocks[first].Dirty && (a.Offset > 0 || (first == last && b.Offset < s.blockSelectionLen(last))) {
				origFirstClean = s.doc.Blocks[first].CleanSource
			}
			origLastClean := ""
			lastRunes := s.blockSelectionLen(last)
			if last != first && !s.doc.Blocks[last].Dirty && b.Offset < lastRunes {
				origLastClean = s.doc.Blocks[last].CleanSource
			}
			prefix := sliceSpans(s.doc.Blocks[first].Spans, 0, a.Offset)
			suffix := sliceSpans(s.doc.Blocks[last].Spans, b.Offset, lastRunes)
			var merged []model.Span
			merged = append(merged, prefix...)
			merged = append(merged, blocks[0].Spans...)
			merged = append(merged, suffix...)
			normMerged := normalizeSpans(merged)
			wholeDropsComment := a.Offset == 0 && b.Offset >= lastRunes && !s.doc.Blocks[first].Dirty && strings.Contains(s.doc.Blocks[first].CleanSource, "<!--")
			if first == last && replaceSelection && hadNonEmpty && !wholeDropsComment && spansIdentical(s.doc.Blocks[first].Spans, normMerged) {
				return false
			}
			updated := s.doc.Blocks[first].Clone()
			updated.Spans = normMerged
			updated.Dirty = true
			updated.CleanSource = ""
			if origFirstClean != "" || origLastClean != "" {
				s.preserveBlockComments(&updated, origFirstClean+origLastClean)
			}
			if s.wouldSpliceCorruptBoundaries(first, last+1, []model.Block{updated}) {
				return false
			}
			beginMutation()
			s.spliceBlocks(first, last+1, []model.Block{updated})
			caret := a.Offset + utf8.RuneCountInString(blocks[0].PlainText())
			s.focusedBlock = first
			s.selAnchor = Position{Block: first, Offset: caret}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}
	}

	// Caret collapsed at end or start of a non-empty block: splice adjacent without
	// dirtying or stripping CleanSource from the untouched neighboring block.
	if first == last && a.Offset == b.Offset {
		curBlk := &s.doc.Blocks[first]
		if curBlk.Kind == model.BlockDivider || curBlk.Kind == model.BlockImage {
			if s.wouldSpliceCorruptBoundaries(first+1, first+1, blocks) {
				return false
			}
			beginMutation()
			s.spliceBlocks(first+1, first+1, blocks)
			endIdx := first + len(blocks)
			endOff := s.blockSelectionLen(endIdx)
			s.focusedBlock = endIdx
			s.selAnchor = Position{Block: endIdx, Offset: endOff}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}
		curLen := s.blockSelectionLen(first)
		if curLen > 0 && a.Offset >= curLen {
			insAt := first + 1
			var foldedHeadID model.BlockID
			if curBlk.Kind.IsHeading() && s.folded[curBlk.ID] {
				foldedHeadID = curBlk.ID
				insAt = s.foldSectionEnd(first)
			}
			if s.wouldSpliceCorruptBoundaries(insAt, insAt, blocks) {
				return false
			}
			beginMutation()
			s.spliceBlocks(insAt, insAt, blocks)
			if foldedHeadID != 0 {
				for _, blk := range blocks {
					s.noteFoldTail(foldedHeadID, blk.ID)
					s.showHiddenTail(blk.ID)
				}
			}
			endIdx := insAt + len(blocks) - 1
			if foldedHeadID != 0 {
				if idx := s.blockIndexByID(blocks[len(blocks)-1].ID); idx >= 0 {
					endIdx = idx
				}
			}
			endOff := s.blockSelectionLen(endIdx)
			s.focusedBlock = endIdx
			s.selAnchor = Position{Block: endIdx, Offset: endOff}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}
		if curLen > 0 && a.Offset == 0 {
			if s.wouldSpliceCorruptBoundaries(first, first, blocks) {
				return false
			}
			beginMutation()
			s.spliceBlocks(first, first, blocks)
			endIdx := first + len(blocks) - 1
			endOff := s.blockSelectionLen(endIdx)
			s.focusedBlock = endIdx
			s.selAnchor = Position{Block: endIdx, Offset: endOff}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.multiSelFocusPending = false
			s.markDirty()
			return true
		}
	}

	// Structural replacement / mid-block split (preserves CleanSource on inserted blocks,
	// preserves partial Code/RawHTML/Table endpoints, and preserves inline HTML comments
	// on untouched rich-text prefix/suffix fragments).
	var inserted []model.Block
	if a.Offset > 0 && first < len(s.doc.Blocks) {
		firstBlk := &s.doc.Blocks[first]
		firstLen := s.blockSelectionLen(first)
		switch firstBlk.Kind {
		case model.BlockCode:
			runes := []rune(firstBlk.Code)
			keep := clampInt(a.Offset, 0, len(runes))
			if keep > 0 {
				cp := firstBlk.Clone()
				cp.SetText(string(runes[:keep]))
				if s.blockSwallowsFollowingBlocks(&cp) {
					return false
				}
				inserted = append(inserted, cp)
			}
		case model.BlockRawHTML:
			runes := []rune(firstBlk.Code)
			keep := clampInt(a.Offset, 0, len(runes))
			if keep > 0 {
				if keep >= len(runes) {
					cp := firstBlk.Clone()
					if s.blockSwallowsFollowingBlocks(&cp) {
						return false
					}
					inserted = append(inserted, cp)
				} else {
					rep := s.reparsePartialRawHTML(string(runes[:keep]))
					for i := range rep {
						if s.blockSwallowsFollowingBlocks(&rep[i]) {
							return false
						}
					}
					inserted = append(inserted, rep...)
				}
			}
		case model.BlockTable:
			cp := firstBlk.Clone()
			if cp.Table != nil && a.Offset < firstLen {
				clearTableRange(cp.Table, a.Offset, firstLen, "")
				cp.Dirty = true
				cp.CleanSource = ""
			}
			inserted = append(inserted, cp)
		case model.BlockImage, model.BlockDivider:
			if a.Offset >= firstLen {
				inserted = append(inserted, firstBlk.Clone())
			}
		default:
			origClean := ""
			if !firstBlk.Dirty {
				origClean = firstBlk.CleanSource
			}
			prefix := sliceSpans(firstBlk.Spans, 0, clampInt(a.Offset, 0, firstLen))
			if len(prefix) > 0 {
				pb := firstBlk.Clone()
				pb.Spans = normalizeSpans(prefix)
				pb.Dirty = true
				pb.CleanSource = ""
				if origClean != "" && (first != last || b.Offset >= s.blockSelectionLen(last)) {
					s.preserveBlockComments(&pb, origClean)
				}
				inserted = append(inserted, pb)
			}
		}
	}
	lastInsertedRel := len(inserted) + len(blocks) - 1
	inserted = append(inserted, blocks...)
	if last < len(s.doc.Blocks) {
		lastBlk := &s.doc.Blocks[last]
		lastRunes := s.blockSelectionLen(last)
		if b.Offset < lastRunes {
			switch lastBlk.Kind {
			case model.BlockCode:
				if b.Offset == 0 {
					inserted = append(inserted, lastBlk.Clone())
				} else {
					runes := []rune(lastBlk.Code)
					cut := clampInt(b.Offset, 0, len(runes))
					if cut < len(runes) {
						cp := lastBlk.Clone()
						cp.SetText(string(runes[cut:]))
						inserted = append(inserted, cp)
					}
				}
			case model.BlockRawHTML:
				if b.Offset == 0 {
					inserted = append(inserted, lastBlk.Clone())
				} else {
					runes := []rune(lastBlk.Code)
					cut := clampInt(b.Offset, 0, len(runes))
					if cut < len(runes) {
						rep := s.reparsePartialRawHTML(string(runes[cut:]))
						inserted = append(inserted, rep...)
					}
				}
			case model.BlockTable:
				cp := lastBlk.Clone()
				if cp.Table != nil && b.Offset > 0 {
					clearTableRange(cp.Table, 0, b.Offset, "")
					cp.Dirty = true
					cp.CleanSource = ""
				}
				inserted = append(inserted, cp)
			case model.BlockImage, model.BlockDivider:
				if b.Offset == 0 {
					inserted = append(inserted, lastBlk.Clone())
				}
			default:
				origClean := ""
				if !lastBlk.Dirty {
					origClean = lastBlk.CleanSource
				}
				suffix := sliceSpans(lastBlk.Spans, clampInt(b.Offset, 0, lastRunes), lastRunes)
				if len(suffix) > 0 {
					var sb model.Block
					if first != last {
						sb = lastBlk.Clone()
						sb.ID = model.NewBlockID()
						sb.Spans = normalizeSpans(suffix)
						sb.Dirty = true
						sb.CleanSource = ""
					} else {
						sb = model.Block{
							ID:         model.NewBlockID(),
							Kind:       model.BlockParagraph,
							QuoteDepth: lastBlk.QuoteDepth,
							Spans:      normalizeSpans(suffix),
							Dirty:      true,
						}
					}
					if origClean != "" {
						s.preserveBlockComments(&sb, origClean)
					}
					inserted = append(inserted, sb)
				}
			}
		}
	}
	// Reject if any newly inserted or retained block would swallow or corrupt a following block on reload
	// (e.g. an unclosed code fence, unclosed HTML tag/comment, or frontmatter formation at doc start).
	if s.wouldSpliceCorruptBoundaries(first, last+1, inserted) {
		return false
	}
	if first == last && replaceSelection && hadNonEmpty && len(inserted) == 1 &&
		a.Offset == 0 && b.Offset >= s.blockSelectionLen(first) &&
		s.blocksStructurallyEqual(&s.doc.Blocks[first], &inserted[0]) {
		return false
	}
	beginMutation()
	s.spliceBlocks(first, last+1, inserted)
	endIdx := clampInt(first+lastInsertedRel, 0, len(s.doc.Blocks)-1)
	endOff := s.blockSelectionLen(endIdx)
	s.focusedBlock = endIdx
	s.selAnchor = Position{Block: endIdx, Offset: endOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.multiSelFocusPending = false
	s.markDirty()
	return true
}

// PastePlainText inserts unformatted literal text (Cmd/Ctrl+Shift+V) without parsing Markdown, HTML, or URLs.
func (s *Session) PastePlainText(raw string) {
	if len(raw) > media.MaxPasteBytes || s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 || raw == "" {
		return
	}
	s.clearAutoConvertState()
	clean := strings.TrimPrefix(raw, "\uFEFF")
	clean = strings.ReplaceAll(strings.ReplaceAll(clean, "\r\n", "\n"), "\r", "\n")
	if clean == "" {
		return
	}
	if (s.hasMultiCellTableSelection() || (s.hasWholeAtomicSelection() && s.isTablePasteTarget())) && strings.ContainsAny(clean, "\t\n") {
		a, b := s.orderedSelection()
		idx := a.Block
		if idx >= 0 && idx < len(s.doc.Blocks) && s.doc.Blocks[idx].Table != nil {
			blk := &s.doc.Blocks[idx]
			startR, startC := 0, 0
			if row, col, _, _, ok := tableCellAtRuneRange(blk.Table, a.Offset, a.Offset); ok {
				startR, startC = row, col
			}
			if !s.canFitTablePaste(blk.Table, startR, startC, clean, true, false) {
				return
			}
			s.pushUndo()
			clearTableRange(blk.Table, a.Offset, b.Offset, "")
			s.focusedBlock = idx
			s.tableActiveBlock = blk.ID
			s.tableActiveRow = startR
			s.tableActiveCol = startC
			s.selAnchor = Position{Block: idx, Offset: a.Offset}
			s.selHead = s.selAnchor
			s.pasteIntoTableBlockOpts(idx, clean, true, false)
		}
		return
	}
	if s.hasMultiCellTableSelection() || (s.hasWholeAtomicSelection() && s.isTablePasteTarget()) {
		s.ReplaceSelectionWithText(clean)
		return
	}
	if s.hasMultiBlockSelection() || s.hasWholeAtomicSelection() {
		s.ReplaceSelectionWithText(clean)
		return
	}
	idx := s.focusedBlock
	if idx < 0 || idx >= len(s.doc.Blocks) {
		idx = 0
	}
	b := &s.doc.Blocks[idx]
	// Validate table paste dimensions BEFORE pushing undo so rejected oversized pastes never pollute undo history.
	if b.Kind == model.BlockTable && b.Table != nil && strings.ContainsAny(clean, "\t\n") {
		startR := clampInt(s.tableActiveRow, 0, max(len(b.Table.Rows)-1, 0))
		startC := 0
		if len(b.Table.Rows) > 0 {
			startC = clampInt(s.tableActiveCol, 0, max(len(b.Table.Rows[0])-1, 0))
		}
		if !s.canFitTablePaste(b.Table, startR, startC, clean, true, false) {
			return
		}
	}
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
	// Never split a single inline-image span across prefix and suffix blocks.
	{
		pos := 0
		for _, sp := range b.Spans {
			n := utf8.RuneCountInString(sp.Text)
			s0, s1 := pos, pos+n
			pos = s1
			if sp.ImageSrc != "" && lo > s0 && hi < s1 {
				if lo == hi {
					lo, hi = s1, s1
				} else {
					b.Spans, _ = replaceSpanRange(b.Spans, lo, hi, nil)
					curText = []rune(b.PlainText())
					lo = s1 - (hi - lo)
					hi = lo
				}
				break
			}
		}
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
	lowerT := strings.ToLower(t)
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") ||
		strings.HasPrefix(t, "> ") || t == ">" ||
		strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") || strings.HasPrefix(t, "$$") ||
		t == "---" || t == "***" || t == "___" || strings.HasPrefix(t, "|") ||
		(strings.HasPrefix(t, "![") && strings.Contains(t, "](") && strings.HasSuffix(t, ")")) ||
		(strings.HasPrefix(lowerT, "<img ") && strings.HasSuffix(t, ">")) ||
		((strings.HasPrefix(lowerT, "<p ") && strings.HasSuffix(lowerT, "</p>")) && strings.Contains(lowerT, "<img ")) ||
		((strings.HasPrefix(lowerT, "<div ") && strings.HasSuffix(lowerT, "</div>")) && strings.Contains(lowerT, "<img ")) {
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
	return strings.ContainsAny(line, "*_`~[$^") ||
		strings.Contains(line, "==") ||
		strings.Contains(lower, "<span") || strings.Contains(lower, "<u>") || strings.Contains(lower, "<a ") ||
		strings.Contains(lower, "<sup>") || strings.Contains(lower, "<sub>") || strings.Contains(lower, "<mark>") ||
		strings.Contains(lower, "<code>") || strings.Contains(lower, "<code ") ||
		strings.Contains(lower, "<strong>") || strings.Contains(lower, "<em>") ||
		strings.Contains(lower, "<del>") || strings.Contains(lower, "<s>") ||
		strings.Contains(lower, "<b>") || strings.Contains(lower, "<i>") ||
		strings.Contains(lower, "<br>") || strings.Contains(lower, "<br/>") || strings.Contains(lower, "<br />") ||
		strings.Contains(lower, "<img") ||
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
	nextSpans, nextCaret := replaceSpanRange(b.Spans, lo, hi, insertSpans)
	b.Spans = nextSpans
	b.Dirty = true
	b.CleanSource = ""
	s.selAnchor = Position{Block: idx, Offset: nextCaret}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

const (
	maxPastedTableCells = 100_000
	maxPastedTableCols  = 50_000
)

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
	trimmedNorm := strings.TrimLeft(norm, " \t")
	if !plain && s.codec != nil && (strings.HasPrefix(trimmedNorm, "|") || strings.HasPrefix(trimmedNorm, "<!-- thinkly:table")) {
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
			if neededCols > maxPastedTableCols || neededRows > maxPastedTableCells/neededCols {
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
	if neededCols > maxPastedTableCols || neededRows > maxPastedTableCells/neededCols {
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
	nextSpans, nextCaret := replaceSpanRange(cell.Spans, cLo, cHi, insertSpans)
	cell.Spans = nextSpans
	b.Dirty = true
	b.CleanSource = ""
	newBase := tableCellStartOffset(tbl, startR, startC)
	s.selAnchor = Position{Block: idx, Offset: newBase + nextCaret}
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
		var ins []model.Span
		if insert != "" {
			ins = []model.Span{{Text: insert}}
		}
		nextSpans, caret := replaceSpanRange(b.Spans, startOff, endOff, ins)
		b.Spans = nextSpans
		b.Dirty = true
		b.CleanSource = ""
		s.selAnchor = Position{Block: idx, Offset: caret}
		s.selHead = s.selAnchor
	}
	s.selSyncPending = true
	s.markDirty()
}

// --- Span & Mark Algebra (Rune Offsets) ---

func countUnescapedPipeCols(line string) int {
	s := strings.TrimSpace(line)
	if s == "" || s == "|" {
		return 0
	}
	if strings.HasPrefix(s, "|") {
		s = s[1:]
	}
	if strings.HasSuffix(s, "|") {
		slashes := 0
		for i := len(s) - 2; i >= 0 && s[i] == '\\'; i-- {
			slashes++
		}
		if slashes%2 == 0 {
			s = s[:len(s)-1]
		}
	}
	cols := 1
	escaped := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == '|' && !escaped {
			cols++
		}
		escaped = b == '\\' && !escaped
	}
	return cols
}

func (s *Session) canFitTablePaste(tbl *model.TableData, startR, startC int, norm string, plain, keepTrailingEmptyRow bool) bool {
	if tbl == nil || len(tbl.Rows) == 0 {
		return false
	}
	if !plain && strings.HasPrefix(strings.TrimLeft(norm, " \t"), "|") {
		// Check unescaped pipe columns across EVERY line (header + body rows) before falling
		// back, so an oversized body row cannot bypass the guard via parser code-block fallback.
		lines := strings.Split(norm, "\n")
		maxPipeCols := 0
		pipeRows := 0
		for i, ln := range lines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			cols := countUnescapedPipeCols(ln)
			if cols > maxPipeCols {
				maxPipeCols = cols
			}
			if i != 1 {
				pipeRows++
			}
		}
		if maxPipeCols > 1 {
			neededCols := max(len(tbl.Rows[0]), startC+maxPipeCols)
			neededRows := max(len(tbl.Rows), startR+max(pipeRows, 1))
			if neededCols <= 0 || neededCols > maxPastedTableCols || neededRows > maxPastedTableCells/neededCols {
				return false
			}
		}
		if s.codec != nil {
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
				return neededCols > 0 && neededCols <= maxPastedTableCols && neededRows <= maxPastedTableCells/neededCols
			}
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
	return neededCols > 0 && neededCols <= maxPastedTableCols && neededRows <= maxPastedTableCells/neededCols
}

func normalizeSpans(spans []model.Span) []model.Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]model.Span, 0, len(spans))
	for i := 0; i < len(spans); {
		if spans[i].Text == "" {
			i++
			continue
		}
		base := spans[i]
		j := i + 1
		totalBytes := len(base.Text)
		count := 1
		for j < len(spans) {
			if spans[j].Text == "" {
				j++
				continue
			}
			if base.ImageSrc != "" || spans[j].ImageSrc != "" || spans[j].Marks != base.Marks || spans[j].Link != base.Link || spans[j].LinkTitle != base.LinkTitle || spans[j].FGColor != base.FGColor || spans[j].BGColor != base.BGColor {
				break
			}
			totalBytes += len(spans[j].Text)
			count++
			j++
		}
		if count > 1 {
			var sb strings.Builder
			sb.Grow(totalBytes)
			for k := i; k < j; k++ {
				if spans[k].Text != "" {
					sb.WriteString(spans[k].Text)
				}
			}
			base.Text = sb.String()
		}
		out = append(out, base)
		i = j
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
		if sp.ImageSrc != "" {
			out = append(out, fn(sp))
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
	s.clearAutoConvertState()
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
				if m&model.MarkCode != 0 {
					sp.Marks &^= model.MarkMath
				}
				if m&model.MarkMath != 0 {
					sp.Marks &^= model.MarkCode
				}
				if m&model.MarkSuperscript != 0 {
					sp.Marks &^= model.MarkSubscript
				}
				if m&model.MarkSubscript != 0 {
					sp.Marks &^= model.MarkSuperscript
				}
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

func (s *Session) detachRichInput(id model.BlockID) {
	if st, ok := s.richInputs[id]; ok && st != nil {
		st.SetDetached(true)
	}
	delete(s.richInputs, id)
}

func (s *Session) insertDetachedTextIntoFocusedBlock(delStart, delEnd int, ins string) {
	if (delEnd <= delStart && ins == "") || s.readOnly || s.doc == nil || s.focusedBlock < 0 || s.focusedBlock >= len(s.doc.Blocks) {
		return
	}
	s.suppressUndo = true
	defer func() { s.suppressUndo = false }()
	idx := s.focusedBlock
	b := &s.doc.Blocks[idx]
	switch b.Kind {
	case model.BlockDivider:
		return
	case model.BlockImage:
		if ins == "" {
			return
		}
		para := model.Block{
			ID:    model.NewBlockID(),
			Kind:  model.BlockParagraph,
			Spans: []model.Span{{Text: ins}},
			Dirty: true,
		}
		s.spliceBlocks(idx+1, idx+1, []model.Block{para})
		pos := len([]rune(ins))
		s.focusedBlock = idx + 1
		s.selAnchor = Position{Block: idx + 1, Offset: pos}
		s.selHead = s.selAnchor
		s.focusPending = true
		s.selSyncPending = true
		s.multiSelFocusPending = false
		s.markDirty()
		return
	case model.BlockTable:
		if b.Table == nil || len(b.Table.Rows) == 0 {
			return
		}
		rIdx := clampInt(s.tableActiveRow, 0, len(b.Table.Rows)-1)
		cIdx := clampInt(s.tableActiveCol, 0, max(len(b.Table.Rows[rIdx])-1, 0))
		cell := &b.Table.Rows[rIdx][cIdx]
		cur := cell.PlainText()
		rs := []rune(cur)
		base := tableCellStartOffset(b.Table, rIdx, cIdx)
		pos := clampInt(s.selHead.Offset-base, 0, len(rs))
		lo := clampInt(pos+delStart, 0, len(rs))
		hi := clampInt(pos+delEnd, lo, len(rs))
		next := string(rs[:lo]) + ins + string(rs[hi:])
		newPos := lo + len([]rune(ins))
		s.applyTableCellTextEdit(idx, rIdx, cIdx, next, newPos, newPos, appUi.RichCodeEdgeNone)
		key := tableCellKey{BlockID: b.ID, Row: rIdx, Col: cIdx}
		if st, ok := s.tableInputs[key]; ok && st != nil {
			st.Sync(cell.PlainText(), newPos, newPos)
		}
		return
	default:
		cur := b.PlainText()
		rs := []rune(cur)
		pos := clampInt(s.selHead.Offset, 0, len(rs))
		lo := clampInt(pos+delStart, 0, len(rs))
		hi := clampInt(pos+delEnd, lo, len(rs))
		next := string(rs[:lo]) + ins + string(rs[hi:])
		newPos := lo + len([]rune(ins))
		s.applyBlockTextEditWithEdge(idx, next, newPos, appUi.RichCodeEdgeNone)
		if st, ok := s.richInputs[b.ID]; ok && st != nil {
			st.Sync(b.PlainText(), s.selAnchor.Offset, s.selHead.Offset)
		}
	}
}

func (s *Session) applyBlockTextEdit(idx int, newText string, caretHint int) {
	s.applyBlockTextEditWithEdge(idx, newText, caretHint, appUi.RichCodeEdgeNone)
}

func (s *Session) applyBlockTextEditWithEdge(idx int, newText string, caretHint int, codeEdge appUi.RichCodeEdge) {
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Kind == model.BlockDivider || b.Kind == model.BlockImage || b.Kind == model.BlockTable {
		return
	}
	oldText := b.PlainText()
	if newText == oldText {
		// Still sync collapsed caret (e.g. IME committing identical text over a selection via UnmarkText).
		pos := clampInt(caretHint, 0, len([]rune(newText)))
		s.selAnchor = Position{Block: idx, Offset: pos}
		s.selHead = s.selAnchor
		return
	}
	insStart, insLen := computeEditInsertedRange(oldText, newText, caretHint)

	// Currency rollback (e.g. `$5&$10` typed char-by-char: digit immediately after `$5&$`).
	if b.Kind != model.BlockCode && b.Kind != model.BlockRawHTML {
		if rolledCaret, rolled := s.tryRollbackCurrencyMathOnBlock(idx, oldText, newText, caretHint); rolled {
			s.selAnchor = Position{Block: idx, Offset: rolledCaret}
			s.selHead = s.selAnchor
			s.markDirty()
			return
		}
	}

	clearMask, clearBG := s.consumeNextInsertGuard(b.ID, false, tableCellKey{}, insStart, insLen)
	useRightBias := s.consumeRightStyleBias(b.ID, false, tableCellKey{}, insStart, insLen)
	s.lastAutoConvert = nil
	s.postEditCodeEdge = appUi.RichCodeEdgeNone
	s.firstLineDeleteArmed = false
	rDelta := len([]rune(newText)) - len([]rune(oldText))
	if rDelta < 0 {
		rDelta = -rDelta
	}
	batchCont := s.suppressUndo
	if st, ok := s.richInputs[b.ID]; ok && st != nil && st.InBatchContinuation() {
		batchCont = true
	}
	if !batchCont {
		if rDelta <= 4 && !strings.Contains(newText, "\n") && !strings.Contains(oldText, "\n") {
			s.pushCoalescedTextUndo(fmt.Sprintf("blk:%d:%d", b.ID, b.Kind))
		} else {
			s.pushUndo()
		}
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
	if useRightBias {
		b.Spans = inheritRightSpanStyleOnInsert(b.Spans, insStart, insLen)
	}
	if insLen > 0 && (clearMask != 0 || clearBG) {
		b.Spans = s.applyNextInsertStyleClear(b.Spans, insStart, insLen, clearMask, clearBG)
	}
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

	// Snapshot right after raw typing so Backspace immediately after any auto-conversion restores raw syntax.
	rawBeforeConvert := b.Clone()
	rawCaret := clampInt(caretHint, 0, len([]rune(newText)))

	// Only trigger auto-conversions when text was actually inserted (including shorter selection replacements),
	// never on pure deletions (so Backspace/Delete never re-triggers formatting).
	if insLen > 0 {
		// Check immediate divider rule (---, －－－, ——; *** and ___ wait for space/Enter so ***bi*** works).
		if b.Kind == model.BlockParagraph && isImmediateDivider(newText) {
			s.insertDividerWithParagraph(idx, true)
			if idx+1 < len(s.doc.Blocks) {
				s.recordMultiBlockAutoConvert(idx, 2, rawBeforeConvert, rawCaret, &s.doc.Blocks[idx+1], 0)
			}
			return
		}

		// Check trailing space/fullwidth-space block-level Markdown input rules.
		if nextIdx, nextCaret, ok := s.trySpaceInputRule(idx, newText, caretHint, insLen); ok {
			s.focusedBlock = clampInt(nextIdx, 0, len(s.doc.Blocks)-1)
			s.selAnchor = Position{Block: s.focusedBlock, Offset: nextCaret}
			s.selHead = s.selAnchor
			if nextIdx == idx+1 && idx < len(s.doc.Blocks) && s.doc.Blocks[idx].Kind == model.BlockDivider && idx+1 < len(s.doc.Blocks) {
				s.recordMultiBlockAutoConvert(idx, 2, rawBeforeConvert, rawCaret, &s.doc.Blocks[idx+1], nextCaret)
			} else if nextIdx == idx && idx < len(s.doc.Blocks) {
				s.recordBlockAutoConvert(idx, rawBeforeConvert, rawCaret, &s.doc.Blocks[idx], nextCaret, false)
			}
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

		// Check inline rules: :emoji:, smart typography, and inline Markdown constructs.
		curCaret := rawCaret
		converted := false
		finalEdge := appUi.RichCodeEdgeNone
		var nextClearMask model.Mark
		var nextClearBG bool
		isMathConvert := false

		if nextSpans, nextCaret, ok := applyEmojiShortcodeOnSpans(b.Spans, curCaret); ok {
			b.Spans = nextSpans
			curCaret = nextCaret
			converted = true
		}
		if nextSpans, nextCaret, ok := applySmartTypographyOnSpans(b.Spans, curCaret, insStart, insLen); ok {
			b.Spans = nextSpans
			curCaret = nextCaret
			converted = true
		}
		if res := applyInlineInputRules(b.Spans, curCaret, insLen, b.Kind == model.BlockParagraph); res.toImage {
			s.convertParagraphToImageBlock(idx, res.imageAlt, res.imageSrc, res.imageTitle)
			s.recordBlockAutoConvert(idx, rawBeforeConvert, rawCaret, &s.doc.Blocks[idx], 0, false)
			return
		} else if res.changed {
			b.Spans = res.spans
			curCaret = res.caret
			finalEdge = res.codeEdge
			nextClearMask = res.clearMask
			nextClearBG = res.clearBG
			isMathConvert = res.isMath
			converted = true
		}

		if converted {
			b.Dirty = true
			b.CleanSource = ""
			s.selAnchor = Position{Block: idx, Offset: curCaret}
			s.selHead = s.selAnchor
			s.postEditCodeEdge = finalEdge
			s.recordBlockAutoConvert(idx, rawBeforeConvert, rawCaret, b, curCaret, isMathConvert)
			if nextClearMask != 0 || nextClearBG {
				s.nextInsertGuard = &nextInsertStyleGuard{
					blockID:   b.ID,
					docRev:    s.docRev,
					caretPos:  curCaret,
					clearMask: nextClearMask,
					clearBG:   nextClearBG,
				}
			}
			return
		}
	}

	rlen := len([]rune(b.PlainText()))
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
	if caretHint >= 0 {
		if len(newR) > len(oldR) {
			if insStart := caretHint - (len(newR) - len(oldR)); insStart >= 0 && insStart < maxPref {
				maxPref = insStart
			}
		} else if caretHint < maxPref {
			maxPref = caretHint
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
	oldEnd := len(oldR) - suf
	inserted := string(newR[pref : len(newR)-suf])
	// Editing inside a single inline-image span mutates its alt text in place instead of
	// splitting one image into duplicate images.
	{
		pos := 0
		for si, sp := range spans {
			n := utf8.RuneCountInString(sp.Text)
			s0, s1 := pos, pos+n
			pos = s1
			if sp.ImageSrc != "" && pref >= s0 && oldEnd <= s1 {
				isBoundaryInsert := inserted != "" && pref == oldEnd && (pref == s0 || pref == s1)
				isFullReplace := pref == s0 && oldEnd == s1
				if !isBoundaryInsert && !isFullReplace {
					spR := []rune(sp.Text)
					nextText := string(spR[:pref-s0]) + inserted + string(spR[oldEnd-s0:])
					out := make([]model.Span, 0, len(spans))
					out = append(out, spans[:si]...)
					if nextText != "" {
						cp := sp
						cp.Text = nextText
						cp.ImageAltEmpty = false
						out = append(out, cp)
					}
					out = append(out, spans[si+1:]...)
					return normalizeSpans(out)
				}
				break
			}
		}
	}
	left := sliceSpans(spans, 0, pref)
	right := sliceSpans(spans, oldEnd, len(oldR))
	var mid []model.Span
	if inserted != "" {
		// Inherit style of the span at insertion boundary if any.
		template := model.Span{Text: inserted}
		if codeEdge == appUi.RichCodeEdgeInsideStart && len(right) > 0 && right[0].Marks&(model.MarkCode|model.MarkMath) != 0 {
			template = right[0]
			template.Text = inserted
		} else if pref > 0 {
			if prevSlice := sliceSpans(spans, pref-1, pref); len(prevSlice) > 0 {
				template = prevSlice[0]
				template.Text = inserted
			}
		}
		if codeEdge == appUi.RichCodeEdgeAfter {
			template.Marks &^= model.MarkCode | model.MarkMath
		}
		// Typing at the end of a link keeps the surrounding style and exits the link.
		if template.Link != "" && pref+suf == len(oldR) && (len(right) == 0 || right[0].Link != template.Link) {
			template.Link = ""
		}
		// Never inherit inline image metadata onto newly typed text adjacent to an image.
		template.ImageSrc = ""
		template.ImageTitle = ""
		template.ImageAltEmpty = false
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

func isImmediateDivider(text string) bool {
	switch text {
	case "---", "－－－", "——":
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

func (s *Session) trySpaceInputRule(idx int, text string, caretRune, insLen int) (int, int, bool) {
	b := &s.doc.Blocks[idx]
	r := []rune(text)
	if len(r) == 0 {
		return 0, 0, false // emptied block (e.g. Backspace on the last char): no rule can match
	}
	if caretRune <= 0 || caretRune > len(r) {
		caretRune = len(r)
	}
	// Support both character-by-character typing (trailing space at caret) and
	// multi-rune whole-string commits (e.g. IME commit or tester Type("# Title")).
	// Single-rune non-space typing (e.g. typing "A" after undoing "# " with Backspace) never re-triggers block prefixes.
	ruleEnd := caretRune
	last := r[ruleEnd-1]
	if last != ' ' && last != '\u3000' {
		if insLen <= 1 {
			return 0, 0, false
		}
		lead := 0
		for lead < caretRune && (r[lead] == ' ' || r[lead] == '\t' || r[lead] == '\u3000') {
			lead++
		}
		firstSpace := -1
		for i := lead; i < caretRune; i++ {
			if r[i] == ' ' || r[i] == '\u3000' {
				firstSpace = i
				break
			}
		}
		if firstSpace <= lead {
			return 0, 0, false
		}
		ruleEnd = firstSpace + 1
	}
	if spanHasInlineImageAt(b.Spans, 0, ruleEnd) {
		return 0, 0, false
	}
	rawPrefix := string(r[:ruleEnd-1])
	rest := string(r[ruleEnd:])
	newCaret := max(caretRune-ruleEnd, 0)

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
			b.QuoteDepth = 0
			b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
			b.Dirty = true
			b.CleanSource = ""
			return idx, newCaret, true
		}
		if addDepth, ok := matchQuotePrefixDepth(rawPrefix); ok {
			cur := max(b.QuoteDepth, 1)
			b.QuoteDepth = min(cur+addDepth, 6)
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
		s.detachRichInput(b.ID)
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

	listIndent := leadingListIndent(rawPrefix)
	stripped := strings.TrimLeftFunc(rawPrefix, unicode.IsSpace)
	// Ordered list: 1. / 1。 / 1、 / 1． / 1) / 1） / a. / i.
	sr := []rune(stripped)
	if len(sr) >= 2 {
		suf := sr[len(sr)-1]
		if suf == '.' || suf == '。' || suf == '、' || suf == '．' || suf == ')' || suf == '）' {
			body := string(sr[:len(sr)-1])
			if n, ok := parseFullOrHalfDigits(body); ok {
				b.Kind = model.BlockNumbered
				if listIndent > 0 {
					b.Indent = listIndent
				}
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
				if listIndent > 0 {
					b.Indent = listIndent
				}
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
		if listIndent > 0 {
			b.Indent = listIndent
		}
		b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
		// Support single-step `- [ ] task` / `- [x] task` commits (e.g. IME or paste-like typing).
		restRunes := r[ruleEnd:]
		for _, tokLen := range []int{2, 3} {
			if len(restRunes) > tokLen && (restRunes[tokLen] == ' ' || restRunes[tokLen] == '\u3000') {
				if checked, ok := matchTodoPrefix(string(restRunes[:tokLen])); ok {
					b.Kind = model.BlockTodo
					b.Checked = checked
					b.Spans = sliceSpans(b.Spans, tokLen+1, len(restRunes))
					newCaret = max(newCaret-(tokLen+1), 0)
					break
				}
			}
		}
		b.Dirty = true
		b.CleanSource = ""
		return idx, newCaret, true
	}

	if checked, ok := matchTodoPrefix(stripped); ok {
		b.Kind = model.BlockTodo
		b.Checked = checked
		if listIndent > 0 {
			b.Indent = listIndent
		}
		b.Spans = sliceSpans(b.Spans, ruleEnd, len(r))
		b.Dirty = true
		b.CleanSource = ""
		return idx, newCaret, true
	}

	if qDepth, ok := matchQuotePrefixDepth(rawPrefix); ok {
		b.Kind = model.BlockQuote
		b.QuoteDepth = qDepth
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
		s.detachRichInput(divider.ID)
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
		if s.activateSlashMenu(ui.Services{}) {
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
	s.clearAutoConvertState()

	// If Enter is pressed with an active intra-block selection, delete the selected range
	// first (keeping inline image spans indivisible) before splitting the block.
	pushedUndo := false
	if s.hasNonEmptySelection() {
		a, endPos := s.orderedSelection()
		if a.Block == idx && endPos.Block == idx && a.Offset < endPos.Offset {
			s.pushUndo()
			pushedUndo = true
			nextSpans, nextCaret := replaceSpanRange(b.Spans, a.Offset, endPos.Offset, nil)
			b.Spans = nextSpans
			b.Dirty = true
			b.CleanSource = ""
			r = []rune(b.PlainText())
			caretRune = clampInt(nextCaret, 0, len(r))
			s.selAnchor = Position{Block: idx, Offset: caretRune}
			s.selHead = s.selAnchor
		}
	}
	pushUndoOnce := func() {
		if !pushedUndo {
			s.pushUndo()
			pushedUndo = true
		}
	}

	// Auto-link bare URL right before caret when pressing Enter.
	if caretRune > 0 {
		if res := tryBareURLAutolinkBefore(b.Spans, r, caretRune); res.changed {
			b.Spans = res.spans
			b.Dirty = true
			b.CleanSource = ""
		}
	}

	// Enter on fence / divider / callout / table / standalone image at end of paragraph.
	if caretRune == len(r) && !hasInlineImageSpan(b.Spans) {
		rawBeforeEnter := b.Clone()
		if (b.Kind == model.BlockParagraph || b.Kind == model.BlockQuote) && b.Callout == "" {
			if kind, ok := matchCalloutPrefix(b.PlainText()); ok {
				pushUndoOnce()
				b.Kind = model.BlockCallout
				b.Callout = kind
				b.QuoteDepth = 0
				b.Spans = nil
				b.Dirty = true
				b.CleanSource = ""
				s.selAnchor = Position{Block: idx, Offset: 0}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.markDirty()
				s.recordBlockAutoConvert(idx, rawBeforeEnter, caretRune, b, 0, false)
				return true
			}
		}
		if b.Kind == model.BlockParagraph {
			if alt, src, title, ok := matchStandaloneImageLine(b.PlainText()); ok {
				pushUndoOnce()
				s.convertParagraphToImageBlock(idx, alt, src, title)
				s.recordBlockAutoConvert(idx, rawBeforeEnter, caretRune, &s.doc.Blocks[idx], 0, false)
				return true
			}
			if headerCells, ok := s.matchTableHeaderCells(b.Spans); ok {
				pushUndoOnce()
				s.convertParagraphToTableBlock(idx, headerCells)
				s.recordBlockAutoConvert(idx, rawBeforeEnter, caretRune, &s.doc.Blocks[idx], 0, false)
				return true
			}
			if lang, ok := matchCodeFencePrefix(b.PlainText()); ok {
				pushUndoOnce()
				b.Kind = model.BlockCode
				b.Language = codepreview.CanonicalLanguage(lang)
				b.ShowSource = codepreview.IsPreviewLanguage(b.Language)
				b.Code = ""
				b.Spans = nil
				b.Dirty = true
				b.CleanSource = ""
				s.blockMenu = nil
				s.detachRichInput(b.ID)
				s.selAnchor = Position{Block: idx, Offset: 0}
				s.selHead = s.selAnchor
				s.focusPending = true
				s.selSyncPending = true
				s.markDirty()
				s.recordBlockAutoConvert(idx, rawBeforeEnter, caretRune, b, 0, false)
				return true
			}
			if isDividerLine(b.PlainText()) {
				pushUndoOnce()
				s.insertDividerWithParagraph(idx, true)
				if idx+1 < len(s.doc.Blocks) {
					s.recordMultiBlockAutoConvert(idx, 2, rawBeforeEnter, caretRune, &s.doc.Blocks[idx+1], 0)
				}
				return true
			}
		}
	}

	// Empty list or quote item: outdent or demote to paragraph instead of splitting.
	if len(r) == 0 && (b.Kind.IsList() || b.Kind == model.BlockQuote || b.Kind == model.BlockCallout) {
		pushUndoOnce()
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
		pushUndoOnce()
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
		pushUndoOnce()
		if caretRune == len(r) && r[caretRune-1] == '\n' && !spanHasInlineImageAt(b.Spans, caretRune-1, caretRune) {
			b.Spans = sliceSpans(b.Spans, 0, caretRune-1)
			b.CleanSource = ""
			b.Dirty = true
			s.spliceBlocks(idx+1, idx+1, []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph, Dirty: true}})
			s.FocusBlock(idx + 1)
		} else {
			caretRune = snapSplitOutsideInlineImage(b.Spans, caretRune)
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
	pushUndoOnce()
	caretRune = snapSplitOutsideInlineImage(b.Spans, caretRune)
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

	// Demote styled blocks / outdent lists and nested quotes first.
	if b.Kind == model.BlockQuote && b.QuoteDepth > 1 {
		s.pushUndo()
		b.QuoteDepth--
		b.Dirty = true
		b.CleanSource = ""
		s.markDirty()
		return true
	}
	if b.Kind.IsHeading() || b.Kind == model.BlockQuote || b.Kind == model.BlockCallout {
		s.pushUndo()
		b.Kind = model.BlockParagraph
		b.QuoteDepth = 0
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
		{Kind: cmdTurnH1, Label: theme.Tr("一级标题", "Heading 1"), Keywords: "h1 heading heading1 title biaoti 一级标题", Hint: "#"},
		{Kind: cmdTurnH2, Label: theme.Tr("二级标题", "Heading 2"), Keywords: "h2 heading heading2 biaoti 二级标题", Hint: "##"},
		{Kind: cmdTurnH3, Label: theme.Tr("三级标题", "Heading 3"), Keywords: "h3 heading heading3 biaoti 三级标题", Hint: "###"},
		{Kind: cmdTable, Label: theme.Tr("表格", "Table"), Keywords: "table biaoge 表格"},
		{Separator: true},
		{Kind: cmdTurnBullet, Label: theme.Tr("无序列表", "Bulleted list"), Keywords: "bullet list ul wuxu liebiao 无序列表", Hint: "-"},
		{Kind: cmdTurnTodo, Label: theme.Tr("待办清单", "Checklist"), Keywords: "todo task check checklist checkbox daiban qingdan 待办清单", Hint: "[]"},
		{Kind: cmdCode, Label: theme.Tr("代码块", "Code Block"), Keywords: "code codeblock block daima 代码块", Hint: "```"},
		{Kind: cmdDivider, Label: theme.Tr("分隔线", "Divider"), Keywords: "divider hr line fengexian 分隔线", Hint: "---"},
		{Kind: cmdImage, Label: theme.Tr("图片", "Image"), Keywords: "image picture photo tupian 图片"},
		{Kind: cmdTurnNumbered, Label: theme.Tr("有序列表", "Numbered list"), Keywords: "number numbered ordered list ol youxu liebiao 有序列表", Hint: "1."},
		{Kind: cmdTurnQuote, Label: theme.Tr("引用", "Quote"), Keywords: "quote blockquote yinyong 引用", Hint: ">"},
		{Kind: cmdCallout, Label: theme.Tr("标注", "Callout"), Keywords: "callout note tip warning alert biaozhu 标注 提示", Hint: "[!note]"},
		{Separator: true},
		{Kind: cmdCodeMermaid, Label: theme.Tr("Mermaid 图表", "Mermaid Diagram"), Keywords: "mermaid diagram flow chart tubiao liuchengtu 图表 流程图", Hint: "```mermaid"},
		{Kind: cmdCodeMath, Label: theme.Tr("数学公式", "Math Formula"), Keywords: "math latex formula shuxue gongshi 数学公式", Hint: "$$"},
	}
}

// turnCmdForKind is the handle-menu conversion that produces kind, if any.
func turnCmdForKind(kind model.BlockKind) (blockCmdKind, bool) {
	switch kind {
	case model.BlockParagraph:
		return cmdTurnParagraph, true
	case model.BlockHeading1:
		return cmdTurnH1, true
	case model.BlockHeading2:
		return cmdTurnH2, true
	case model.BlockHeading3:
		return cmdTurnH3, true
	case model.BlockBullet:
		return cmdTurnBullet, true
	case model.BlockNumbered:
		return cmdTurnNumbered, true
	case model.BlockTodo:
		return cmdTurnTodo, true
	case model.BlockQuote:
		return cmdTurnQuote, true
	case model.BlockCallout:
		return cmdCallout, true
	case model.BlockCode:
		return cmdCode, true
	}
	return 0, false
}

func appendCommonHandleItems(items []blockMenuItem, idx, count int, hasFrontmatter bool) []blockMenuItem {
	items = append(items, blockMenuItem{Kind: cmdDuplicate, Label: theme.Tr("复制块", "Duplicate"), Hint: "⌘D"})
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

// allHandleMenuItems lists the handle menu of block idx, tailored to the block kind.
func allHandleMenuItems(idx, count int, hasFrontmatter bool, b *model.Block) []blockMenuItem {
	if hasFrontmatter && idx == 0 {
		return []blockMenuItem{
			{Kind: cmdDelete, Label: theme.Tr("删除", "Delete"), Hint: "⌫", Danger: true},
		}
	}
	if b != nil {
		switch b.Kind {
		case model.BlockImage:
			var items []blockMenuItem
			if strings.TrimSpace(b.ImageSrc) != "" {
				items = append(items,
					blockMenuItem{Kind: cmdMediaFullscreen, Label: theme.Tr("全屏查看", "Fullscreen"), Hint: "F"},
					blockMenuItem{Kind: cmdOpenExternal, Label: theme.Tr("用默认程序打开", "Open externally")},
					blockMenuItem{Separator: true},
				)
			}
			curAlign := b.ImageAlign
			if curAlign == "" {
				curAlign = model.ImageAlignLeft
			}
			items = append(items,
				blockMenuItem{Kind: cmdAlignLeft, Label: theme.Tr("左对齐", "Align left"), Disabled: curAlign == model.ImageAlignLeft},
				blockMenuItem{Kind: cmdAlignCenter, Label: theme.Tr("居中对齐", "Align center"), Disabled: curAlign == model.ImageAlignCenter},
				blockMenuItem{Kind: cmdAlignRight, Label: theme.Tr("右对齐", "Align right"), Disabled: curAlign == model.ImageAlignRight},
				blockMenuItem{Separator: true},
			)
			return appendCommonHandleItems(items, idx, count, hasFrontmatter)
		case model.BlockTable:
			var items []blockMenuItem
			if b.Table != nil && len(b.Table.Rows) > 0 {
				hdrLbl := theme.Tr("隐藏表头行", "Hide header row")
				if b.Table.NoHeaderRow {
					hdrLbl = theme.Tr("显示表头行", "Show header row")
				}
				items = append(items,
					blockMenuItem{Kind: cmdTableToggleHeader, Label: hdrLbl},
					blockMenuItem{Separator: true},
				)
			}
			return appendCommonHandleItems(items, idx, count, hasFrontmatter)
		case model.BlockDivider, model.BlockRawHTML:
			return appendCommonHandleItems(nil, idx, count, hasFrontmatter)
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
	}
	if b != nil {
		if cur, ok := turnCmdForKind(b.Kind); ok {
			for i := range items {
				if items[i].Kind == cur && !items[i].Separator {
					items[i].Disabled = true
					break
				}
			}
		}
	}
	return appendCommonHandleItems(items, idx, count, hasFrontmatter)
}

func (s *Session) slashMenuEntries() []blockMenuItem {
	if s.blockMenu == nil {
		return nil
	}
	if s.blockMenu.Mode == menuHandle {
		n := 0
		hasFM := false
		var blk *model.Block
		if s.doc != nil {
			n = len(s.doc.Blocks)
			hasFM = n > 0 && isFrontmatterBlock(0, &s.doc.Blocks[0])
			if bi := s.blockMenu.BlockIdx; bi >= 0 && bi < n {
				blk = &s.doc.Blocks[bi]
			}
		}
		return allHandleMenuItems(s.blockMenu.BlockIdx, n, hasFM, blk)
	}
	all := slices.DeleteFunc(allSlashItems(), func(it blockMenuItem) bool {
		return (s.minimal && it.Kind == cmdImage) || (s.aiDisabled && it.Kind == cmdAI)
	})
	q := strings.TrimSpace(s.blockMenu.Filter)
	if q == "" {
		return all
	}
	type scoredSlashItem struct {
		item  blockMenuItem
		score int
	}
	var scored []scoredSlashItem
	for _, it := range all {
		if it.Separator {
			continue
		}
		primary := make([]string, 0, 4)
		primary = append(primary, it.Label)
		if it.Keywords != "" {
			primary = append(primary, it.Keywords)
			primary = append(primary, strings.Fields(it.Keywords)...)
		}
		var secondary []string
		if it.Hint != "" {
			secondary = []string{it.Hint}
		}
		if sc, ok := fuzzy.Score(q, primary, secondary, false); ok {
			scored = append(scored, scoredSlashItem{item: it, score: sc})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	hits := make([]blockMenuItem, len(scored))
	for i, s := range scored {
		hits[i] = s.item
	}
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
		if !entries[cur].Separator && !entries[cur].Disabled {
			s.blockMenu.ActiveIndex = cur
			s.blockMenu.PointerActive = false
			return true
		}
	}
	return false
}

func (s *Session) activateSlashMenu(svc ui.Services) bool {
	if s.blockMenu == nil {
		return false
	}
	entries := s.slashMenuEntries()
	if len(entries) == 0 || s.blockMenu.ActiveIndex < 0 || s.blockMenu.ActiveIndex >= len(entries) {
		return false
	}
	idx := s.blockMenu.ActiveIndex
	if entries[idx].Separator || entries[idx].Disabled {
		return false
	}
	s.executeBlockCmd(svc, s.blockMenu.BlockIdx, s.blockMenu.Mode, entries[idx].Kind)
	return true
}

func (s *Session) executeBlockCmd(svc ui.Services, idx int, mode blockMenuMode, cmd blockCmdKind) {
	s.blockMenu = nil
	if s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	isSlashClear := mode == menuSlash && b.Kind == model.BlockParagraph &&
		(b.PlainText() == "" || strings.HasPrefix(b.PlainText(), "/") || strings.HasPrefix(b.PlainText(), "、"))

	switch cmd {
	case cmdMediaFullscreen:
		if b.Kind == model.BlockImage && strings.TrimSpace(b.ImageSrc) != "" {
			s.OpenMediaLightbox(b.ID)
		}
		return
	case cmdOpenExternal:
		if b.Kind == model.BlockImage && strings.TrimSpace(b.ImageSrc) != "" {
			s.PauseAllVideos()
			s.openBlockLink(svc, b.ImageSrc)
		}
		return
	case cmdAlignLeft, cmdAlignCenter, cmdAlignRight:
		if b.Kind != model.BlockImage {
			return
		}
		align := model.ImageAlignLeft
		switch cmd {
		case cmdAlignCenter:
			align = model.ImageAlignCenter
		case cmdAlignRight:
			align = model.ImageAlignRight
		}
		if b.ImageAlign == align || (b.ImageAlign == "" && align == model.ImageAlignLeft) {
			return
		}
		s.pushUndo()
		b.ImageAlign = align
		b.Dirty = true
		s.markDirty()
		return
	case cmdTableToggleHeader:
		if b.Kind == model.BlockTable {
			s.tableToggleHeaderRow(b.ID)
		}
		return
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

// findToggleGuard swallows a second trigger of the same key press (menu
// accelerator + in-window shortcut) and key auto-repeat while held.
const findToggleGuard = 250 * time.Millisecond

// ToggleFind is the find shortcut: open (seeding the query from selected text),
// or close when already open and there is no new selection to search for.
func (s *Session) ToggleFind() {
	now := time.Now()
	if !s.findToggleAt.IsZero() && now.Sub(s.findToggleAt) < findToggleGuard {
		s.findToggleAt = now
		return
	}
	s.findToggleAt = now
	if s.findOpen {
		if seed := s.findSeedFromSelection(); seed == "" || strings.EqualFold(seed, s.findQuery) {
			s.CloseFind()
			return
		}
	}
	s.OpenFind()
}

// findSeedFromSelection returns the selected single-line text usable as a query.
func (s *Session) findSeedFromSelection() string {
	if s.doc == nil || !s.hasNonEmptySelection() {
		return ""
	}
	a, b := s.orderedSelection()
	if a.Block != b.Block || a.Block < 0 || a.Block >= len(s.doc.Blocks) {
		return ""
	}
	r := []rune(s.doc.Blocks[a.Block].PlainText())
	lo, hi := clampInt(a.Offset, 0, len(r)), clampInt(b.Offset, 0, len(r))
	if hi <= lo || hi-lo > 200 {
		return ""
	}
	part := string(r[lo:hi])
	if strings.ContainsAny(part, "\n\t") {
		return ""
	}
	return part
}

// OpenFind shows the floating find bar. It never changes the document layout.
func (s *Session) OpenFind() {
	s.findOpen = true
	s.findFocusPending = true
	s.blockMenu = nil
	s.toolbarPop = ""
	if seed := s.findSeedFromSelection(); seed != "" {
		s.findQuery = seed
	}
	s.recomputeFind(true)
}

func (s *Session) CloseFind() {
	if !s.findOpen {
		return
	}
	s.findOpen = false
	s.findReplaceOpen = false
	s.selAnchor = s.selHead
	s.focusPending = true
	s.focusPreventScroll = true
}

// findMatchReplaceable reports whether a match lies in plain editable text.
func (s *Session) findMatchReplaceable(m findMatch) bool {
	if s.readOnly || s.doc == nil || m.BlockIdx < 0 || m.BlockIdx >= len(s.doc.Blocks) {
		return false
	}
	b := &s.doc.Blocks[m.BlockIdx]
	switch b.Kind {
	case model.BlockCode, model.BlockRawHTML:
		return true
	case model.BlockTable, model.BlockImage, model.BlockDivider:
		return false
	}
	for _, sp := range sliceSpans(b.Spans, m.StartRune, m.EndRune) {
		if sp.ImageSrc != "" {
			return false
		}
	}
	return true
}

// replaceFindMatchText swaps one match for repl, keeping the match's inline style.
// Callers own undo and markDirty.
func (s *Session) replaceFindMatchText(m findMatch, repl string) {
	b := &s.doc.Blocks[m.BlockIdx]
	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		runes := []rune(b.Code)
		lo := clampInt(m.StartRune, 0, len(runes))
		hi := clampInt(m.EndRune, lo, len(runes))
		b.SetText(string(runes[:lo]) + repl + string(runes[hi:]))
		return
	}
	var ins []model.Span
	if repl != "" {
		sp := model.Span{}
		if seg := sliceSpans(b.Spans, m.StartRune, m.EndRune); len(seg) > 0 {
			sp = seg[0]
		}
		sp.Text = repl
		ins = []model.Span{sp}
	}
	b.Spans, _ = replaceSpanRange(b.Spans, m.StartRune, m.EndRune, ins)
	b.Dirty = true
	b.CleanSource = ""
}

// ReplaceCurrentFind replaces the active match and moves on to the next one.
func (s *Session) ReplaceCurrentFind() {
	s.recomputeFind(false)
	if len(s.findMatches) == 0 {
		return
	}
	m := s.findMatches[s.findCurrent]
	if !s.findMatchReplaceable(m) {
		s.FindNext()
		return
	}
	s.pushUndo()
	s.replaceFindMatchText(m, s.findReplace)
	s.markDirty()
	s.refreshFindMatches()
	if len(s.findMatches) == 0 {
		s.findCurrent = 0
		s.selAnchor = Position{Block: m.BlockIdx, Offset: m.StartRune + utf8.RuneCountInString(s.findReplace)}
		s.selHead = s.selAnchor
		s.selSyncPending = true
		return
	}
	after := m.StartRune + utf8.RuneCountInString(s.findReplace)
	next := sort.Search(len(s.findMatches), func(i int) bool {
		x := s.findMatches[i]
		return x.BlockIdx > m.BlockIdx || (x.BlockIdx == m.BlockIdx && x.StartRune >= after)
	})
	if next == len(s.findMatches) {
		next = 0
	}
	s.findCurrent = next
	s.gotoCurrentFindMatch()
}

// ReplaceAllFind replaces every editable match in one undo step.
func (s *Session) ReplaceAllFind() int {
	s.recomputeFind(false)
	n := 0
	for i := len(s.findMatches) - 1; i >= 0; i-- {
		m := s.findMatches[i]
		if !s.findMatchReplaceable(m) {
			continue
		}
		if n == 0 {
			s.pushUndo()
		}
		s.replaceFindMatchText(m, s.findReplace)
		n++
	}
	if n == 0 {
		return 0
	}
	s.markDirty()
	s.refreshFindMatches()
	s.findCurrent = 0
	return n
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
	s.annotFlashID = ann.ID
	s.annotFlashAt = time.Time{}
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
		if ann.Note == "" && ann.RewriteResult == "" && ann.Answer == "" {
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

type annotGenBatch struct {
	seq     map[string]uint64
	replies map[string]model.AnnotationReply
}

type annotationVersion struct {
	Index  int
	Prompt string
	Kind   string
	Text   string
}

// annotationVersions returns all completed reply versions for an annotation in chronological order.
func annotationVersions(ann *model.Annotation) []annotationVersion {
	if ann == nil {
		return nil
	}
	versions := make([]annotationVersion, 0, len(ann.Thread)+1)
	for _, t := range ann.Thread {
		if strings.TrimSpace(t.A) == "" {
			continue
		}
		kind := t.Kind
		if kind == "" {
			kind = "edit"
		}
		versions = append(versions, annotationVersion{
			Index:  len(versions),
			Prompt: t.Q,
			Kind:   kind,
			Text:   t.A,
		})
	}
	if ann.Answer != "" {
		versions = append(versions, annotationVersion{
			Index:  len(versions),
			Prompt: ann.Note,
			Kind:   model.AnnotationReplyAnswer,
			Text:   ann.Answer,
		})
	} else if ann.RewriteResult != "" {
		versions = append(versions, annotationVersion{
			Index:  len(versions),
			Prompt: ann.Note,
			Kind:   "edit",
			Text:   ann.RewriteResult,
		})
	}
	return versions
}

// activeAnnotationVersion returns the version currently inspected on the card (latest by default).
func (s *Session) activeAnnotationVersion(ann *model.Annotation) (annotationVersion, int, int, bool) {
	versions := annotationVersions(ann)
	total := len(versions)
	if total == 0 {
		return annotationVersion{}, 0, 0, false
	}
	idx := total - 1
	if s.annotVersionIdx != nil {
		if saved, ok := s.annotVersionIdx[ann.ID]; ok {
			idx = clampInt(saved, 0, total-1)
		}
	}
	return versions[idx], idx, total, true
}

func (s *Session) setAnnotationVersion(id string, idx int) {
	if s.annotVersionIdx == nil {
		s.annotVersionIdx = make(map[string]int)
	}
	s.annotVersionIdx[id] = idx
	s.annotInlineEditBlock = 0
	s.requestRedraw()
}

// activeInlineRewrite reports whether ann should render an in-place revision inside the paragraph text.
func (s *Session) activeInlineRewrite(ann *model.Annotation) (text string, busy bool, ok bool) {
	if ann == nil || s.aiDisabled || ann.Applied || s.editingAnnotID == ann.ID {
		return "", false, false
	}
	if ann.RewriteBusy {
		for i := len(ann.Thread) - 1; i >= 0; i-- {
			t := ann.Thread[i]
			if strings.TrimSpace(t.A) != "" && t.Kind != model.AnnotationReplyAnswer {
				return t.A, true, true
			}
		}
		return "", false, false
	}
	ver, _, _, hasVer := s.activeAnnotationVersion(ann)
	if !hasVer || ver.Kind == model.AnnotationReplyAnswer || strings.TrimSpace(ver.Text) == "" {
		return "", false, false
	}
	return ver.Text, false, true
}

func (s *Session) SetAnnotationAIResponder(fn func(docMarkdown string, pending []*model.Annotation) map[string]model.AnnotationReply) {
	s.annotationAIResponder = fn
}

func isPendingAnnotation(a *model.Annotation) bool {
	return a != nil && !a.Applied && !a.RewriteBusy && strings.TrimSpace(a.Note) != "" && a.RewriteResult == "" && a.Answer == ""
}

// GeneratePendingAnnotations sends every unanswered note to the AI in the
// background; replies land in pollAnnotationGeneration so the UI never blocks.
func (s *Session) GeneratePendingAnnotations() int {
	s.ensureAnnotationsLoaded()
	var batch []*model.Annotation
	for _, a := range s.annotations {
		if isPendingAnnotation(a) {
			batch = append(batch, a)
		}
	}
	return s.startAnnotationGeneration(batch)
}

// GenerateAnnotationByID triggers AI generation for one pending annotation.
func (s *Session) GenerateAnnotationByID(id string) bool {
	if s.aiDisabled || s.annotationAIResponder == nil {
		return false
	}
	s.ensureAnnotationsLoaded()
	for _, a := range s.annotations {
		if a == nil || a.ID != id || a.RewriteBusy || a.Applied {
			continue
		}
		if strings.TrimSpace(a.Note) == "" {
			continue
		}
		return s.startAnnotationGeneration([]*model.Annotation{a}) > 0
	}
	return false
}

// SendAnnotationFollowup files the card's current exchange into its thread and
// asks the follow-up as the new note, for that card alone.
func (s *Session) SendAnnotationFollowup(id, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || s.aiDisabled || s.annotationAIResponder == nil {
		return false
	}
	s.ensureAnnotationsLoaded()
	for _, a := range s.annotations {
		if a == nil || a.ID != id || a.RewriteBusy || a.Applied {
			continue
		}
		reply := a.Answer
		kind := model.AnnotationReplyAnswer
		if reply == "" {
			reply = a.RewriteResult
			kind = "edit"
		}
		s.pushUndo()
		a.Thread = append(a.Thread, model.AnnotationTurn{Q: a.Note, A: reply, Kind: kind})
		a.Note = text
		a.Answer = ""
		a.RewriteResult = ""
		a.AIError = ""
		delete(s.annotVersionIdx, id)
		s.annotInlineEditBlock = 0
		s.persistAnnotations()
		return s.startAnnotationGeneration([]*model.Annotation{a}) > 0
	}
	return false
}

// CancelAnnotationGeneration aborts an in-flight rewrite/follow-up for one card,
// restoring the previous version and returning the follow-up text to its draft.
func (s *Session) CancelAnnotationGeneration(id string) bool {
	s.ensureAnnotationsLoaded()
	for _, a := range s.annotations {
		if a == nil || a.ID != id || !a.RewriteBusy {
			continue
		}
		if s.annotGenSeq == nil {
			s.annotGenSeq = make(map[string]uint64)
		}
		s.annotGenSeq[id]++
		a.RewriteBusy = false
		if n := len(a.Thread); n > 0 {
			draftText := a.Note
			prev := a.Thread[n-1]
			a.Thread = a.Thread[:n-1]
			a.Note = prev.Q
			if prev.Kind == model.AnnotationReplyAnswer {
				a.Answer = prev.A
				a.RewriteResult = ""
			} else {
				a.RewriteResult = prev.A
				a.Answer = ""
			}
			if draftText != "" {
				if s.followupDrafts == nil {
					s.followupDrafts = make(map[string]*string)
				}
				d := draftText
				s.followupDrafts[id] = &d
			}
			delete(s.annotVersionIdx, id)
			s.persistAnnotations()
		}
		s.requestRedraw()
		return true
	}
	return false
}

func (s *Session) startAnnotationGeneration(batch []*model.Annotation) int {
	if s.aiDisabled || s.annotationAIResponder == nil || len(batch) == 0 {
		return 0
	}
	if s.annotGenSeq == nil {
		s.annotGenSeq = make(map[string]uint64)
	}
	pending := make([]*model.Annotation, 0, len(batch))
	seqs := make(map[string]uint64, len(batch))
	for _, a := range batch {
		cp := *a
		cp.Reactions = slices.Clone(a.Reactions)
		cp.Thread = slices.Clone(a.Thread)
		pending = append(pending, &cp)
		a.RewriteBusy = true
		a.AIError = ""
		s.annotGenSeq[a.ID]++
		seqs[a.ID] = s.annotGenSeq[a.ID]
	}
	if s.annotGenCh == nil {
		s.annotGenCh = make(chan annotGenBatch, 16)
	}
	ch := s.annotGenCh
	s.annotGenInFlight++
	responder := s.annotationAIResponder
	docMarkdown := s.Markdown()
	ids := make([]string, len(pending))
	for i, a := range pending {
		ids[i] = a.ID
	}
	go func() {
		results := responder(docMarkdown, pending)
		if results == nil {
			results = map[string]model.AnnotationReply{}
		}
		// Every sent id gets an entry so its card leaves the busy state even without a reply.
		for _, id := range ids {
			if _, ok := results[id]; !ok {
				results[id] = model.AnnotationReply{}
			}
		}
		ch <- annotGenBatch{seq: seqs, replies: results}
		s.requestRedraw()
	}()
	return len(pending)
}

// pollAnnotationGeneration applies finished background replies; call once per frame.
func (s *Session) pollAnnotationGeneration() {
	if s.annotGenCh == nil || s.annotGenInFlight == 0 {
		return
	}
	updated := 0
	for {
		var batch annotGenBatch
		select {
		case batch = <-s.annotGenCh:
		default:
			if updated > 0 {
				s.persistAnnotations()
			}
			return
		}
		s.annotGenInFlight--
		for _, a := range s.annotations {
			if a == nil || !a.RewriteBusy {
				continue
			}
			if expectedSeq, ok := batch.seq[a.ID]; !ok || s.annotGenSeq[a.ID] != expectedSeq {
				continue
			}
			res, ok := batch.replies[a.ID]
			if !ok {
				continue
			}
			a.RewriteBusy = false
			text := strings.TrimSpace(res.Text)
			if text == "" || a.Applied || strings.TrimSpace(a.Note) == "" {
				continue
			}
			if res.Kind == model.AnnotationReplyAnswer {
				a.Answer = text
			} else {
				a.RewriteResult = res.Text
			}
			a.AIError = ""
			delete(s.annotVersionIdx, a.ID)
			s.annotInlineEditBlock = 0
			updated++
		}
		if s.annotGenInFlight == 0 {
			if updated > 0 {
				s.persistAnnotations()
			}
			return
		}
	}
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
	ann.Answer = ""
	ann.Thread = nil
	ann.AIError = ""
	delete(s.annotVersionIdx, ann.ID)
	delete(s.annotThreadExpanded, ann.ID)
	delete(s.followupDrafts, ann.ID)
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
	selected := make([]bool, len(diff))
	selIdx := 0
	for i, d := range diff {
		if d.Kind == model.DiffEqual {
			continue
		}
		selected[i] = selIdx >= len(op.LineSelected) || op.LineSelected[selIdx]
		selIdx++
	}
	texts, changes := diffChanges(diff)
	appUi.SyncDiffTableSeps(texts, changes, selected)
	var out []string
	for i, d := range diff {
		switch {
		case d.Kind == model.DiffEqual,
			d.Kind == model.DiffDelete && !selected[i],
			d.Kind == model.DiffInsert && selected[i]:
			out = append(out, d.Text)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// diffChanges maps diff kinds to the side markers DiffFenceMask expects.
func diffChanges(diff []model.DiffLine) ([]string, []int) {
	texts := make([]string, len(diff))
	changes := make([]int, len(diff))
	for i, d := range diff {
		texts[i] = d.Text
		switch d.Kind {
		case model.DiffDelete:
			changes[i] = -1
		case model.DiffInsert:
			changes[i] = 1
		}
	}
	return texts, changes
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
	{Tag: "c", Label: "C"},
	{Tag: "csharp", Label: "C#"},
	{Tag: "cpp", Label: "C++"},
	{Tag: "css", Label: "CSS"},
	{Tag: "dart", Label: "Dart"},
	{Tag: "diff", Label: "Diff"},
	{Tag: "dockerfile", Label: "Dockerfile"},
	{Tag: "erb", Label: "ERB"},
	{Tag: "go", Label: "Go"},
	{Tag: "gomod", Label: "Go Module"},
	{Tag: "gosum", Label: "Go Checksum"},
	{Tag: "graphql", Label: "GraphQL"},
	{Tag: "hcl", Label: "HCL"},
	{Tag: "html", Label: "HTML"},
	{Tag: "ini", Label: "INI"},
	{Tag: "java", Label: "Java"},
	{Tag: "javascript", Label: "JavaScript"},
	{Tag: "jinja", Label: "Jinja"},
	{Tag: "json", Label: "JSON"},
	{Tag: "kotlin", Label: "Kotlin"},
	{Tag: "lean", Label: "Lean"},
	{Tag: "less", Label: "Less"},
	{Tag: "lua", Label: "Lua"},
	{Tag: "makefile", Label: "Makefile"},
	{Tag: "nix", Label: "Nix"},
	{Tag: "php", Label: "PHP"},
	{Tag: "plist", Label: "Property List"},
	{Tag: "protobuf", Label: "Protobuf"},
	{Tag: "python", Label: "Python"},
	{Tag: "r", Label: "R"},
	{Tag: "ruby", Label: "Ruby"},
	{Tag: "rust", Label: "Rust"},
	{Tag: "scala", Label: "Scala"},
	{Tag: "scss", Label: "SCSS"},
	{Tag: "sql", Label: "SQL"},
	{Tag: "svelte", Label: "Svelte"},
	{Tag: "swift", Label: "Swift"},
	{Tag: "toml", Label: "TOML"},
	{Tag: "typescript", Label: "TypeScript"},
	{Tag: "vue", Label: "Vue"},
	{Tag: "xml", Label: "XML"},
	{Tag: "yaml", Label: "YAML"},
	{Tag: "zig", Label: "Zig"},
}

func languageDisplayLabel(lang string) string {
	if lbl, ok := plugins.ResolveLanguageTagDisplay(lang, string(theme.Language())); ok && lbl != "" {
		return lbl
	}
	c := codepreview.CanonicalLanguage(lang)
	if c == "plaintext" {
		return theme.Tr("纯文本", "Plain Text")
	}
	for _, opt := range codeLanguages {
		if opt.Tag == c {
			return opt.Label
		}
	}
	if l := codeed.LanguageNamed(c); l != nil {
		return l.Name
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
	tokInserted
	tokDeleted
)

type syntaxToken struct {
	Start int // rune offset
	End   int // rune offset
	Kind  syntaxTokenKind
}

func mapCodeTokenKind(k codeed.TokenKind) syntaxTokenKind {
	switch k {
	case codeed.TokenKeyword, codeed.TokenOperator, codeed.TokenTag:
		return tokKeyword
	case codeed.TokenString, codeed.TokenEscape, codeed.TokenRegexp, codeed.TokenLink, codeed.TokenCode:
		return tokString
	case codeed.TokenComment:
		return tokComment
	case codeed.TokenNumber, codeed.TokenConstant:
		return tokNumber
	case codeed.TokenType, codeed.TokenProperty, codeed.TokenAttribute, codeed.TokenHeading:
		return tokType
	case codeed.TokenFunction, codeed.TokenMeta:
		return tokFunc
	case codeed.TokenInserted:
		return tokInserted
	case codeed.TokenDeleted:
		return tokDeleted
	default:
		return tokPlain
	}
}

func codeSyntaxTokens(tokens []plugins.CodeRuneToken) []syntaxToken {
	out := make([]syntaxToken, 0, len(tokens))
	for _, token := range tokens {
		if kind := mapCodeTokenKind(token.Kind); kind != tokPlain && token.End > token.Start {
			out = append(out, syntaxToken{Start: token.Start, End: token.End, Kind: kind})
		}
	}
	return out
}

func codeBlockCanUseLexer(src string) bool {
	// Some lexers scan the rest of a line for each token. Bound that work on the UI thread.
	const maxLineBytes = 4 * 1024
	for line := range strings.SplitSeq(src, "\n") {
		if len(line) > maxLineBytes {
			return false
		}
	}
	return true
}

func tokenizeCode(src, lang string) []syntaxToken {
	if src == "" || len(src) > files.HighlightLimit {
		return nil
	}
	canon := codepreview.CanonicalLanguage(lang)
	if codeBlockCanUseLexer(src) {
		if tokens, ok := plugins.TokenizeCodeByTag(lang, src); ok {
			return codeSyntaxTokens(tokens)
		}
		if l := codeed.LanguageNamed(canon); l != nil && l.Tokenizer != nil {
			return codeSyntaxTokens(plugins.TokenizeTextWithTokenizer(l.Tokenizer, src))
		}
	}
	if canon == "plaintext" {
		return nil
	}
	// Use lightweight highlighting for long lines and tags without a lexer.
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
