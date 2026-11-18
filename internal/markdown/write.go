package markdown

import (
	"fmt"
	"strconv"
	"strings"

	"block/internal/model"
)

// SerializeMarkdown serializes a model.Doc back to Markdown.
// Untouched documents return CleanRaw verbatim; untouched blocks retain CleanSource.
func SerializeMarkdown(doc *model.Doc) string {
	return serializeMarkdown(doc, nil)
}

func serializeMarkdown(doc *model.Doc, sourceRanges *[]sourceLineRange) string {
	if doc == nil || len(doc.Blocks) == 0 {
		return ""
	}
	if len(doc.Blocks) == 1 && isBlankBlock(&doc.Blocks[0]) && !doc.Blocks[0].Dirty && doc.CleanRaw == "" {
		return ""
	}

	var defs map[string]refDef
	if strings.Contains(doc.CleanRaw, "]:") {
		defs, _ = collectRefDefs(strings.TrimPrefix(decodeRawBlockGuard(doc.CleanRaw), "\uFEFF"))
	}

	if isDocUntouched(doc, defs) {
		return doc.CleanRaw
	}

	var out strings.Builder
	var num listNumbering
	var listStack []int
	blocks := doc.Blocks
	nexts := nextNonBlankBlocks(blocks)
	line, lineCursor := 1, 0
	if sourceRanges != nil {
		*sourceRanges = make([]sourceLineRange, len(blocks))
	}

	for i := 0; i < len(blocks); i++ {
		b := &blocks[i]
		n := 0
		if !isBlankBlock(b) {
			n = num.next(b)
		}
		for len(listStack) > 0 && !isBlankBlock(b) && b.Indent <= listStack[len(listStack)-1] {
			if b.Kind.IsList() && b.Indent == listStack[len(listStack)-1] {
				listStack = listStack[:len(listStack)-1]
				break
			}
			listStack = listStack[:len(listStack)-1]
		}
		inListChild := len(listStack) > 0 && b.Indent > listStack[len(listStack)-1]
		if b.Kind.IsList() {
			listStack = append(listStack, b.Indent)
		}

		if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
			out.WriteByte('\n')
		}
		if i > 0 && needsSeparator(&blocks[i-1], nexts[i-1]) {
			prevQ := effectiveQuoteDepth(&blocks[i-1])
			curQ := effectiveQuoteDepth(b)
			if prevQ > 0 && curQ > 0 {
				qSep := strings.TrimRight(strings.Repeat("> ", minInt(prevQ, curQ)), " ") + "\n"
				if !strings.HasSuffix(out.String(), "\n"+qSep) && !strings.HasSuffix(out.String(), "\n\n") {
					out.WriteString(qSep)
				}
			} else if !strings.HasSuffix(out.String(), "\n\n") {
				out.WriteByte('\n')
			}
		}

		blockStart := out.Len()
		if canUseCleanSource(b, i == 0, n, defs) {
			out.WriteString(b.CleanSource)
		} else if extraQ := extraQuotePrefixCount(b); extraQ > 0 {
			var sub strings.Builder
			serializeBlockWithCtx(b, i == 0, n, inListChild, &sub)
			prefixQuoteLines(sub.String(), extraQ, &out)
		} else {
			serializeBlockWithCtx(b, i == 0, n, inListChild, &out)
		}
		if sourceRanges != nil {
			serialized := out.String()
			line += strings.Count(serialized[lineCursor:blockStart], "\n")
			chunk := serialized[blockStart:]
			if chunk != "" {
				(*sourceRanges)[i] = sourceLineRange{line, line + countCleanLines(chunk) - 1}
			}
			line += strings.Count(chunk, "\n")
			lineCursor = len(serialized)
		}
	}

	res := out.String()
	// Respect explicit lack of trailing newline if original file had none.
	if doc.CleanRaw != "" && doc.TrailingLine == "" {
		res = strings.TrimRight(res, "\r\n")
	} else if doc.TrailingLine == "\r\n" {
		res = strings.ReplaceAll(strings.ReplaceAll(res, "\r\n", "\n"), "\n", "\r\n")
	}
	return res
}

func isDocUntouched(doc *model.Doc, defs map[string]refDef) bool {
	if doc.CleanRaw == "" || len(doc.Blocks) == 0 {
		return false
	}
	decoded := strings.TrimPrefix(decodeRawBlockGuard(doc.CleanRaw), "\uFEFF")
	cursor := 0
	var num listNumbering
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		n := 0
		if !isBlankBlock(b) {
			n = num.next(b)
		}
		if !canUseCleanSource(b, i == 0, n, defs) {
			return false
		}
		idx := strings.Index(decoded[cursor:], b.CleanSource)
		if idx < 0 {
			return false
		}
		gap := decoded[cursor : cursor+idx]
		if !isIgnorableCleanGap(gap, defs) {
			return false
		}
		cursor += idx + len(b.CleanSource)
	}
	tail := decoded[cursor:]
	return isIgnorableCleanGap(tail, defs)
}

func isIgnorableCleanGap(gap string, defs map[string]refDef) bool {
	if strings.TrimSpace(gap) == "" {
		return true
	}
	if len(defs) == 0 {
		return false
	}
	lines := splitLinesInclusive(gap)
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		if label, _, count, ok := parseRefDefLines(lines[i:]); ok {
			if _, exists := defs[label]; exists {
				i += count
				continue
			}
		}
		return false
	}
	return true
}

func canUseCleanSource(b *model.Block, isFirst bool, listNum int, defs map[string]refDef) bool {
	if b.Dirty || b.CleanSource == "" {
		return false
	}
	if b.Kind == model.BlockCode && b.Language == "yaml-frontmatter" && (!isFirst || b.Indent > 0 || b.QuoteDepth > 0) {
		return false
	}
	if b.Kind == model.BlockNumbered && listNum > 0 {
		if m, ok := classifyListMarker(trimLineEnding(b.CleanSource)); ok && m.kind == model.BlockNumbered {
			if m.start != listNum {
				return false
			}
		}
	}
	return blockMatchesCleanSource(b, defs)
}

func effectiveQuoteDepth(b *model.Block) int {
	if b == nil {
		return 0
	}
	if b.Kind == model.BlockQuote && b.QuoteDepth < 1 {
		return 1
	}
	return b.QuoteDepth
}

func extraQuotePrefixCount(b *model.Block) int {
	if b == nil || b.QuoteDepth <= 0 {
		return 0
	}
	if b.Kind == model.BlockQuote {
		return max(b.QuoteDepth-1, 0)
	}
	return b.QuoteDepth
}

func prefixQuoteLines(raw string, depth int, out *strings.Builder) {
	if depth <= 0 || raw == "" {
		out.WriteString(raw)
		return
	}
	fullPrefix := strings.Repeat("> ", depth)
	barePrefix := strings.TrimRight(fullPrefix, " ")
	trimmed := strings.TrimSuffix(raw, "\n")
	for _, line := range strings.Split(trimmed, "\n") {
		if line == "" {
			out.WriteString(barePrefix)
		} else {
			out.WriteString(fullPrefix)
			out.WriteString(line)
		}
		out.WriteByte('\n')
	}
}

func blockMatchesCleanSource(b *model.Block, defs map[string]refDef) bool {
	clean := b.CleanSource
	expectedIndent := b.Indent
	if b.Indent >= 4 && b.Kind != model.BlockCode && b.Kind != model.BlockRawHTML && !strings.HasPrefix(strings.TrimSpace(clean), "<p ") {
		var sb strings.Builder
		for _, ln := range splitLinesInclusive(clean) {
			sb.WriteString(stripColumns(ln, b.Indent))
		}
		clean = sb.String()
		expectedIndent = 0
	}
	parsed := parseBlocksWithDefs(clean, defs, nil)
	if len(parsed) != 1 {
		// Multi-block clean source is only valid if it was a single list item or raw block.
		return false
	}
	p := &parsed[0]
	if p.Kind != b.Kind || p.Checked != b.Checked || p.Callout != b.Callout {
		return false
	}
	if effectiveQuoteDepth(p) != effectiveQuoteDepth(b) {
		return false
	}
	if p.Language != b.Language || p.Code != b.Code {
		return false
	}
	if p.ImageSrc != b.ImageSrc || p.ImageAlt != b.ImageAlt || p.ImageWidth != b.ImageWidth || p.ImageAlign != b.ImageAlign || p.ImageCrop != b.ImageCrop {
		return false
	}
	if p.Indent != expectedIndent {
		return false
	}
	if !spansEqual(p.Spans, b.Spans) {
		return false
	}
	if (p.Table == nil) != (b.Table == nil) {
		return false
	}
	if p.Table != nil && !tablesEqual(p.Table, b.Table) {
		return false
	}
	return true
}

func tablesEqual(a, b *model.TableData) bool {
	if a.WasHTML != b.WasHTML || len(a.Rows) != len(b.Rows) || len(a.Aligns) != len(b.Aligns) || len(a.Widths) != len(b.Widths) {
		return false
	}
	for i := range a.Aligns {
		if a.Aligns[i] != b.Aligns[i] {
			return false
		}
	}
	for i := range a.Widths {
		if a.Widths[i] != b.Widths[i] {
			return false
		}
	}
	for r := range a.Rows {
		if len(a.Rows[r]) != len(b.Rows[r]) {
			return false
		}
		for c := range a.Rows[r] {
			if !spansEqual(a.Rows[r][c].Spans, b.Rows[r][c].Spans) {
				return false
			}
		}
	}
	return true
}

func serializeBlock(b *model.Block, isFirst bool, listNum int, out *strings.Builder) {
	serializeBlockWithCtx(b, isFirst, listNum, false, out)
}

func serializeBlockWithCtx(b *model.Block, isFirst bool, listNum int, inListChild bool, out *strings.Builder) {
	switch b.Kind {
	case model.BlockDivider:
		out.WriteString(strings.Repeat(" ", b.Indent))
		out.WriteString("***\n")

	case model.BlockImage:
		serializeImageBlock(b, out)

	case model.BlockCallout:
		tag := string(b.Callout)
		if tag == "" {
			tag = string(model.CalloutNote)
		}
		out.WriteString("> [!")
		out.WriteString(tag)
		out.WriteString("]\n")
		lines := splitSpansByNewline(b.Spans)
		if len(lines) == 0 {
			return
		}
		for _, lineSpans := range lines {
			out.WriteString("> ")
			out.WriteString(SerializeInline(lineSpans))
			out.WriteByte('\n')
		}

	case model.BlockQuote:
		indentStr := strings.Repeat(" ", b.Indent)
		lines := splitSpansByNewline(b.Spans)
		if len(lines) == 0 {
			out.WriteString(indentStr)
			out.WriteString("> \n")
			return
		}
		for _, lineSpans := range lines {
			out.WriteString(indentStr)
			if len(lineSpans) == 0 {
				out.WriteString(">\n")
			} else {
				out.WriteString("> ")
				out.WriteString(SerializeInline(lineSpans))
				out.WriteByte('\n')
			}
		}

	case model.BlockCode:
		if b.Language == "yaml-frontmatter" && isFirst && b.Indent == 0 {
			inner := strings.Split(b.Code, "\n")
			hasClose := false
			for _, l := range inner {
				if isFrontmatterClose(l) {
					hasClose = true
					break
				}
			}
			if !hasClose && looksLikeFrontmatter(inner) {
				out.WriteString("---\n")
				if b.Code != "" {
					out.WriteString(b.Code)
					out.WriteByte('\n')
				}
				out.WriteString("---\n")
				return
			}
		}
		if b.Language == "math" {
			pad := strings.Repeat(" ", minInt(b.Indent, 3))
			out.WriteString(pad)
			out.WriteString("$$\n")
			if b.Code != "" {
				for _, l := range strings.Split(b.Code, "\n") {
					out.WriteString(pad)
					out.WriteString(l)
					out.WriteByte('\n')
				}
			}
			out.WriteString(pad)
			out.WriteString("$$\n")
			return
		}
		lang := b.Language
		if lang == "yaml-frontmatter" {
			lang = "yaml"
		}
		var marker byte = '`'
		if strings.ContainsRune(lang, '`') {
			marker = '~'
		}
		longest := 0
		run := 0
		for i := 0; i < len(b.Code); i++ {
			if b.Code[i] == marker {
				run++
				if run > longest {
					longest = run
				}
			} else {
				run = 0
			}
		}
		fenceLen := 3
		if longest+1 > fenceLen {
			fenceLen = longest + 1
		}
		fence := strings.Repeat(string(marker), fenceLen)
		pad := strings.Repeat(" ", minInt(b.Indent, 3))
		if b.Indent > 3 {
			pad = strings.Repeat(" ", b.Indent)
		}
		out.WriteString(pad)
		out.WriteString(fence)
		out.WriteString(lang)
		out.WriteByte('\n')
		if b.Code != "" {
			for _, l := range strings.Split(b.Code, "\n") {
				out.WriteString(pad)
				out.WriteString(l)
				out.WriteByte('\n')
			}
		}
		out.WriteString(pad)
		out.WriteString(fence)
		out.WriteByte('\n')

	case model.BlockTable:
		if b.Table != nil {
			serializeTable(b.Table, out)
		}

	case model.BlockRawHTML:
		out.WriteString(b.Code)
		if !strings.HasSuffix(b.Code, "\n") {
			out.WriteByte('\n')
		}

	case model.BlockHeading1, model.BlockHeading2, model.BlockHeading3, model.BlockHeading4, model.BlockHeading5, model.BlockHeading6:
		level := b.Kind.HeadingLevel()
		out.WriteString(strings.Repeat(" ", b.Indent))
		out.WriteString(strings.Repeat("#", level))
		out.WriteByte(' ')
		out.WriteString(SerializeInline(b.Spans))
		out.WriteByte('\n')

	case model.BlockBullet, model.BlockNumbered, model.BlockTodo:
		marker := "- "
		if b.Kind == model.BlockNumbered {
			if listNum <= 0 {
				listNum = 1
			}
			marker = fmt.Sprintf("%d. ", listNum)
		} else if b.Kind == model.BlockTodo {
			if b.Checked {
				marker = "- [x] "
			} else {
				marker = "- [ ] "
			}
		}
		lines := splitSpansByNewline(b.Spans)
		if len(lines) == 0 {
			out.WriteString(strings.Repeat(" ", b.Indent))
			out.WriteString(marker)
			out.WriteByte('\n')
			return
		}
		contPad := strings.Repeat(" ", b.Indent+len(marker))
		for idx, lineSpans := range lines {
			if idx == 0 {
				out.WriteString(strings.Repeat(" ", b.Indent))
				out.WriteString(marker)
			} else {
				out.WriteString(contPad)
			}
			out.WriteString(SerializeInline(lineSpans))
			out.WriteByte('\n')
		}

	default: // BlockParagraph
		if len(b.Spans) == 0 {
			out.WriteString("<!-- thinkly:empty-block=paragraph -->\n")
			return
		}
		if b.Indent > 0 && !inListChild {
			ind := minInt(b.Indent, 12)
			out.WriteString(fmt.Sprintf(`<p data-indent="%d">`, ind))
			serializeInlineHTML(b.Spans, out)
			out.WriteString("</p>\n")
			return
		}
		if b.Indent > 0 && inListChild {
			out.WriteString(strings.Repeat(" ", b.Indent))
		}
		inlineStr := SerializeInline(b.Spans)
		if strings.HasPrefix(inlineStr, "$$") || strings.HasPrefix(inlineStr, ":::") {
			out.WriteByte('\\')
		}
		out.WriteString(inlineStr)
		out.WriteByte('\n')
	}
}

func serializeImageBlock(b *model.Block, out *strings.Builder) {
	out.WriteString(strings.Repeat(" ", b.Indent))
	align := b.ImageAlign
	if align == "" {
		align = model.ImageAlignLeft
	}
	hasWidth := b.ImageWidth > 0
	hasAlign := align != model.ImageAlignLeft
	hasCrop := !b.ImageCrop.IsZero()

	if !hasWidth && !hasAlign && !hasCrop {
		alt := strings.ReplaceAll(b.ImageAlt, `\`, `\\`)
		alt = strings.ReplaceAll(alt, `]`, `\]`)
		dest := b.ImageSrc
		if strings.ContainsAny(dest, " \t()<>") {
			dest = "<" + strings.ReplaceAll(strings.ReplaceAll(dest, "<", "%3C"), ">", "%3E") + ">"
		}
		out.WriteString(fmt.Sprintf("![%s](%s)\n", alt, dest))
		return
	}

	if hasCrop && !hasWidth && !hasAlign {
		out.WriteString(fmt.Sprintf(
			"<!-- thinkly:image crop=%s,%s,%s,%s -->\n",
			formatFloat(b.ImageCrop.Top),
			formatFloat(b.ImageCrop.Right),
			formatFloat(b.ImageCrop.Bottom),
			formatFloat(b.ImageCrop.Left),
		))
		out.WriteString(strings.Repeat(" ", b.Indent))
		alt := strings.ReplaceAll(b.ImageAlt, `\`, `\\`)
		alt = strings.ReplaceAll(alt, `]`, `\]`)
		out.WriteString(fmt.Sprintf("![%s](%s)\n", alt, formatLinkDest(b.ImageSrc)))
		return
	}

	var img strings.Builder
	img.WriteString(`<img src="`)
	img.WriteString(escapeAttrValue(b.ImageSrc))
	img.WriteString(`" alt="`)
	img.WriteString(escapeAttrValue(b.ImageAlt))
	img.WriteByte('"')
	if hasWidth {
		img.WriteString(` width="`)
		img.WriteString(formatFloat(b.ImageWidth))
		img.WriteString(`%"`)
	}
	if hasCrop {
		img.WriteString(fmt.Sprintf(
			` data-crop="%s,%s,%s,%s"`,
			formatFloat(b.ImageCrop.Top),
			formatFloat(b.ImageCrop.Right),
			formatFloat(b.ImageCrop.Bottom),
			formatFloat(b.ImageCrop.Left),
		))
	}
	img.WriteByte('>')
	switch align {
	case model.ImageAlignCenter:
		out.WriteString(`<p align="center">`)
		out.WriteString(img.String())
		out.WriteString("</p>\n")
	case model.ImageAlignRight:
		out.WriteString(`<p align="right">`)
		out.WriteString(img.String())
		out.WriteString("</p>\n")
	default:
		out.WriteString(img.String())
		out.WriteByte('\n')
	}
}

func formatFloat(v float32) string {
	return strconv.FormatFloat(float64(v), 'f', -1, 32)
}

func serializeTable(t *model.TableData, out *strings.Builder) {
	if len(t.Rows) == 0 {
		return
	}
	if t.WasHTML {
		out.WriteString("<table>\n")
		if len(t.Widths) > 0 {
			out.WriteString("<colgroup>")
			for _, w := range t.Widths {
				out.WriteString(fmt.Sprintf(`<col style="width:%spx">`, formatFloat(w)))
			}
			out.WriteString("</colgroup>\n")
		}
		for r, row := range t.Rows {
			out.WriteString("<tr>")
			tag := "td"
			if r == 0 {
				tag = "th"
			}
			for c, cell := range row {
				out.WriteByte('<')
				out.WriteString(tag)
				if c < len(t.Aligns) && t.Aligns[c] != model.ColAlignNone {
					out.WriteString(` style="text-align:`)
					out.WriteString(string(t.Aligns[c]))
					out.WriteByte('"')
				}
				out.WriteByte('>')
				serializeInlineHTML(cell.Spans, out)
				out.WriteString("</")
				out.WriteString(tag)
				out.WriteByte('>')
			}
			out.WriteString("</tr>\n")
		}
		out.WriteString("</table>\n")
		return
	}

	cols := len(t.Rows[0])
	if len(t.Aligns) > cols {
		cols = len(t.Aligns)
	}
	for r, row := range t.Rows {
		out.WriteByte('|')
		for c := 0; c < cols; c++ {
			out.WriteByte(' ')
			if c < len(row) {
				cellStr := SerializeInline(row[c].Spans)
				cellStr = strings.ReplaceAll(cellStr, "|", `\|`)
				out.WriteString(cellStr)
			}
			out.WriteString(" |")
		}
		out.WriteByte('\n')
		if r == 0 {
			out.WriteByte('|')
			for c := 0; c < cols; c++ {
				align := model.ColAlignNone
				if c < len(t.Aligns) {
					align = t.Aligns[c]
				}
				switch align {
				case model.ColAlignCenter:
					out.WriteString(" :---: |")
				case model.ColAlignRight:
					out.WriteString(" ---: |")
				case model.ColAlignLeft:
					out.WriteString(" :--- |")
				default:
					out.WriteString(" --- |")
				}
			}
			out.WriteByte('\n')
		}
	}
}

func splitSpansByNewline(spans []model.Span) [][]model.Span {
	if len(spans) == 0 {
		return nil
	}
	lines := [][]model.Span{{}}
	for _, sp := range spans {
		if !strings.ContainsRune(sp.Text, '\n') {
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

func serializeInlineHTML(spans []model.Span, out *strings.Builder) {
	for _, sp := range coalesceSpans(spans) {
		var openTags, closeTags []string
		if sp.Link != "" {
			openTags = append(openTags, fmt.Sprintf(`<a href="%s">`, escapeAttrValue(sp.Link)))
			closeTags = append(closeTags, "</a>")
		}
		if sp.FGColor != "" || sp.BGColor != "" {
			var css strings.Builder
			if sp.FGColor != "" {
				css.WriteString("color:")
				css.WriteString(escapeAttrValue(sp.FGColor))
			}
			if sp.BGColor != "" {
				if sp.FGColor != "" {
					css.WriteByte(';')
				}
				css.WriteString("background-color:")
				css.WriteString(escapeAttrValue(sp.BGColor))
			}
			openTags = append(openTags, fmt.Sprintf(`<span style="%s">`, css.String()))
			closeTags = append(closeTags, "</span>")
		}
		if sp.Marks&model.MarkUnderline != 0 {
			openTags = append(openTags, "<u>")
			closeTags = append(closeTags, "</u>")
		}
		if sp.Marks&model.MarkStrike != 0 {
			openTags = append(openTags, "<del>")
			closeTags = append(closeTags, "</del>")
		}
		if sp.Marks&model.MarkBold != 0 {
			openTags = append(openTags, "<strong>")
			closeTags = append(closeTags, "</strong>")
		}
		if sp.Marks&model.MarkItalic != 0 {
			openTags = append(openTags, "<em>")
			closeTags = append(closeTags, "</em>")
		}
		if sp.Marks&model.MarkCode != 0 {
			openTags = append(openTags, "<code>")
			closeTags = append(closeTags, "</code>")
		}
		for _, t := range openTags {
			out.WriteString(t)
		}
		for i := 0; i < len(sp.Text); i++ {
			switch sp.Text[i] {
			case '&':
				out.WriteString("&amp;")
			case '<':
				out.WriteString("&lt;")
			case '>':
				out.WriteString("&gt;")
			case '\n':
				out.WriteString("<br>")
			default:
				out.WriteByte(sp.Text[i])
			}
		}
		for i := len(closeTags) - 1; i >= 0; i-- {
			out.WriteString(closeTags[i])
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
