package markdown

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

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

	cleanValid := make([]int8, len(doc.Blocks))
	if isDocUntouchedWithCache(doc, defs, cleanValid) {
		return doc.CleanRaw
	}
	if len(doc.Blocks) == 1 && isBlankBlock(&doc.Blocks[0]) {
		return ""
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
		wantSep := false
		if i > 0 {
			if isBlankBlock(b) {
				wantSep = swallowableSeparator(&blocks[i-1], nexts[i-1])
			} else {
				wantSep = needsSeparator(&blocks[i-1], nexts[i-1])
			}
		}
		if wantSep {
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
		if canUseCleanSourceCached(b, i == 0, n, defs, cleanValid, i) {
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
	if doc.CleanRaw != "" && doc.TrailingLine == "" && !isBlankBlock(&blocks[len(blocks)-1]) {
		res = strings.TrimSuffix(res, "\n")
	} else if doc.TrailingLine == "\r\n" {
		res = strings.ReplaceAll(strings.ReplaceAll(res, "\r\n", "\n"), "\n", "\r\n")
	}
	return res
}

func isDocUntouched(doc *model.Doc, defs map[string]refDef) bool {
	return isDocUntouchedWithCache(doc, defs, nil)
}

func isDocUntouchedWithCache(doc *model.Doc, defs map[string]refDef, cleanValid []int8) bool {
	if doc.CleanRaw == "" || len(doc.Blocks) == 0 {
		return false
	}
	for i := range doc.Blocks {
		if doc.Blocks[i].Dirty || doc.Blocks[i].CleanSource == "" {
			return false
		}
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
		if !canUseCleanSourceCached(b, i == 0, n, defs, cleanValid, i) {
			return false
		}
		idx := strings.Index(decoded[cursor:], b.CleanSource)
		if idx < 0 {
			return false
		}
		gap := decoded[cursor : cursor+idx]
		if !isIgnorableCleanGap(gap, defs, i > 0) {
			return false
		}
		cursor += idx + len(b.CleanSource)
	}
	tail := decoded[cursor:]
	return isIgnorableCleanGap(tail, defs, false)
}

func isIgnorableCleanGap(gap string, defs map[string]refDef, allowSeparator bool) bool {
	if gap == "" {
		return true
	}
	if strings.TrimSpace(gap) == "" {
		return allowSeparator && countCleanLines(gap) == 1
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

func canUseCleanSourceCached(b *model.Block, isFirst bool, listNum int, defs map[string]refDef, cache []int8, idx int) bool {
	if cache != nil && idx >= 0 && idx < len(cache) {
		switch cache[idx] {
		case 1:
			return true
		case -1:
			return false
		}
	}
	ok := canUseCleanSource(b, isFirst, listNum, defs)
	if cache != nil && idx >= 0 && idx < len(cache) {
		if ok {
			cache[idx] = 1
		} else {
			cache[idx] = -1
		}
	}
	return ok
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

type cleanBlockCacheEntry struct {
	single bool
	block  model.Block
}

var (
	cleanBlockCacheMu sync.RWMutex
	cleanBlockCache   = make(map[string]cleanBlockCacheEntry)
)

const (
	cleanBlockCacheMaxEntries = 4096
	cleanBlockCacheMaxLen     = 16384
)

func isSimplePlainCleanParagraph(b *model.Block, clean string) bool {
	if b.Kind != model.BlockParagraph || b.Indent != 0 || b.QuoteDepth != 0 {
		return false
	}
	nl := strings.IndexByte(clean, '\n')
	if nl >= 0 && nl != len(clean)-1 {
		return false
	}
	line := trimLineEnding(clean)
	if isBlankBlock(b) {
		return strings.TrimSpace(line) == ""
	}
	if len(b.Spans) != 1 {
		return false
	}
	sp := b.Spans[0]
	if sp.Marks != 0 || sp.Link != "" || sp.FGColor != "" || sp.BGColor != "" || sp.Text != line || line == "" {
		return false
	}
	if line[0] == ' ' || line[len(line)-1] == ' ' {
		return false
	}
	return !strings.ContainsAny(line, "#>-*_~`[]<&\\!$|:.)\t+=")
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
	} else if isSimplePlainCleanParagraph(b, clean) {
		return true
	}

	var p *model.Block
	if (len(defs) == 0 || !strings.ContainsRune(clean, '[')) && len(clean) <= cleanBlockCacheMaxLen {
		cleanBlockCacheMu.RLock()
		entry, hit := cleanBlockCache[clean]
		cleanBlockCacheMu.RUnlock()
		if !hit {
			parsed := parseBlocksWithDefs(clean, nil, nil)
			if len(parsed) == 1 {
				entry = cleanBlockCacheEntry{single: true, block: parsed[0]}
			}
			cleanBlockCacheMu.Lock()
			if len(cleanBlockCache) >= cleanBlockCacheMaxEntries {
				clear(cleanBlockCache)
			}
			cleanBlockCache[clean] = entry
			cleanBlockCacheMu.Unlock()
		}
		if !entry.single {
			return false
		}
		p = &entry.block
	} else {
		parsed := parseBlocksWithDefs(clean, defs, nil)
		if len(parsed) != 1 {
			// Multi-block clean source is only valid if it was a single list item or raw block.
			return false
		}
		p = &parsed[0]
	}

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
	if a.WasHTML != b.WasHTML || a.NoHeaderRow != b.NoHeaderRow || a.HeaderCol != b.HeaderCol ||
		len(a.Rows) != len(b.Rows) || len(a.Aligns) != len(b.Aligns) || len(a.Widths) != len(b.Widths) {
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
			if a.Rows[r][c].Align != b.Rows[r][c].Align || a.Rows[r][c].Tint != b.Rows[r][c].Tint {
				return false
			}
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
			out.WriteByte('\n')
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
		out.WriteString(escapeLiteralParagraphPrefix(inlineStr))
		out.WriteByte('\n')
	}
}

func escapeLiteralParagraphPrefix(s string) string {
	if s == "" {
		return s
	}
	// Preserve literal leading whitespace in a plain paragraph so reload does not
	// turn it into block Indent, an indented code block, or strip a space-only line.
	if s[0] == ' ' {
		return "&#32;" + s[1:]
	}
	if s[0] == '\t' {
		return "&#9;" + s[1:]
	}
	rest := s
	// Already escaped or literal HTML tag.
	if rest[0] == '\\' {
		return s
	}
	// Display math / container / fenced code blocks (only when parseFenceBody would actually treat it as a fence,
	// so leading inline code spans like ```a``b``` are not corrupted).
	if strings.HasPrefix(rest, "$$") || strings.HasPrefix(rest, ":::") {
		return `\` + rest
	}
	if _, _, _, isFence := parseFenceBody(rest); isFence {
		return `\` + rest
	}
	// ATX headings `# `..`###### ` or bare `#`..`######`.
	if rest[0] == '#' {
		n := 0
		for n < len(rest) && n < 6 && rest[n] == '#' {
			n++
		}
		if n >= 1 && n <= 6 && (n == len(rest) || rest[n] == ' ' || rest[n] == '\t') {
			return `\` + rest
		}
	}
	// Blockquote `>` / `>> `.
	if rest[0] == '>' {
		return `\` + rest
	}
	// Bullet `- ` / `+ ` / `* ` (or bare `-`/`+`/`*` on a line).
	if rest[0] == '-' || rest[0] == '+' || rest[0] == '*' {
		if len(rest) == 1 || rest[1] == ' ' || rest[1] == '\t' {
			return `\` + rest
		}
	}
	// Thematic break: >= 3 identical `-`, `*`, or `_` with optional spaces/tabs.
	if rest[0] == '-' || rest[0] == '*' || rest[0] == '_' {
		ch := rest[0]
		cnt := 0
		onlyRule := true
		for i := 0; i < len(rest); i++ {
			if rest[i] == ch {
				cnt++
			} else if rest[i] != ' ' && rest[i] != '\t' {
				onlyRule = false
				break
			}
		}
		if onlyRule && cnt >= 3 {
			return `\` + rest
		}
	}
	// Ordered list `1. ` / `1) `.
	d := 0
	for d < len(rest) && d < 9 && rest[d] >= '0' && rest[d] <= '9' {
		d++
	}
	if d > 0 && d < len(rest) && (rest[d] == '.' || rest[d] == ')') && (d+1 == len(rest) || rest[d+1] == ' ' || rest[d+1] == '\t') {
		return rest[:d] + `\` + rest[d:]
	}
	return s
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

func encodeTableTintMeta(t *model.TableData) (colTints, rowTints []model.TableTint, cellTints []string) {
	rows := len(t.Rows)
	if rows == 0 {
		return nil, nil, nil
	}
	cols := len(t.Rows[0])
	if cols == 0 {
		return nil, nil, nil
	}
	anyTint := false
	for r := 0; r < rows && !anyTint; r++ {
		for c := 0; c < len(t.Rows[r]); c++ {
			if t.Rows[r][c].Tint != model.TableTintNone {
				anyTint = true
				break
			}
		}
	}
	if !anyTint {
		return nil, nil, nil
	}

	colTints = make([]model.TableTint, cols)
	for c := 0; c < cols; c++ {
		counts := make(map[model.TableTint]int)
		bestTint := model.TableTintNone
		bestN := 0
		for r := 0; r < rows; r++ {
			if c < len(t.Rows[r]) && t.Rows[r][c].Tint != model.TableTintNone {
				v := t.Rows[r][c].Tint
				counts[v]++
				if counts[v] > bestN {
					bestN = counts[v]
					bestTint = v
				}
			}
		}
		// Only hoist to column tint when it covers strictly more than half of rows (and at least 2, or all 1 row).
		if bestTint != model.TableTintNone && bestN*2 > rows && (rows == 1 || bestN >= 2) {
			colTints[c] = bestTint
		}
	}

	rowTints = make([]model.TableTint, rows)
	for r := 0; r < rows; r++ {
		baseDiffs := 0
		counts := make(map[model.TableTint]int)
		bestTint := model.TableTintNone
		bestN := 0
		for c := 0; c < len(t.Rows[r]); c++ {
			actual := t.Rows[r][c].Tint
			if actual != colTints[c] {
				baseDiffs++
			}
			if actual != model.TableTintNone {
				counts[actual]++
				if counts[actual] > bestN {
					bestN = counts[actual]
					bestTint = actual
				}
			}
		}
		if baseDiffs == 0 || bestTint == model.TableTintNone {
			continue
		}
		rowDiffs := len(t.Rows[r]) - bestN
		if rowDiffs < baseDiffs && (len(t.Rows[r]) == 1 || bestN >= 2) {
			rowTints[r] = bestTint
		}
	}

	for r := 0; r < rows; r++ {
		for c := 0; c < len(t.Rows[r]); c++ {
			pred := colTints[c]
			if rowTints[r] != model.TableTintNone {
				pred = rowTints[r]
			}
			actual := t.Rows[r][c].Tint
			if actual != pred {
				val := string(actual)
				if val == "" {
					val = "none"
				}
				cellTints = append(cellTints, fmt.Sprintf("%d:%d=%s", r, c, val))
			}
		}
	}

	// Trim trailing empty slots; drop entirely if all empty.
	for len(colTints) > 0 && colTints[len(colTints)-1] == model.TableTintNone {
		colTints = colTints[:len(colTints)-1]
	}
	for len(rowTints) > 0 && rowTints[len(rowTints)-1] == model.TableTintNone {
		rowTints = rowTints[:len(rowTints)-1]
	}
	return colTints, rowTints, cellTints
}

func encodeTableAlignMeta(t *model.TableData) (rowAligns []model.ColAlign, cellAligns []string) {
	rows := len(t.Rows)
	if rows == 0 {
		return nil, nil
	}
	rowAligns = make([]model.ColAlign, rows)
	for r := 0; r < rows; r++ {
		baseDiffs := 0
		counts := make(map[model.ColAlign]int)
		bestAlign := model.ColAlignNone
		bestN := 0
		for c := 0; c < len(t.Rows[r]); c++ {
			eff := t.CellEffectiveAlign(r, c)
			if eff != t.ColBaseAlign(c) {
				baseDiffs++
			}
			counts[eff]++
			if counts[eff] > bestN {
				bestN = counts[eff]
				bestAlign = eff
			}
		}
		if baseDiffs == 0 {
			continue
		}
		rowDiffs := len(t.Rows[r]) - bestN
		if bestAlign != model.ColAlignNone && rowDiffs < baseDiffs && (len(t.Rows[r]) == 1 || bestN >= 2) {
			rowAligns[r] = bestAlign
		}
		for c := 0; c < len(t.Rows[r]); c++ {
			pred := t.ColBaseAlign(c)
			if rowAligns[r] != model.ColAlignNone {
				pred = rowAligns[r]
			}
			eff := t.CellEffectiveAlign(r, c)
			if eff != pred {
				cellAligns = append(cellAligns, fmt.Sprintf("%d:%d=%s", r, c, string(eff)))
			}
		}
	}
	for len(rowAligns) > 0 && rowAligns[len(rowAligns)-1] == model.ColAlignNone {
		rowAligns = rowAligns[:len(rowAligns)-1]
	}
	return rowAligns, cellAligns
}

func serializeTableMetaComment(t *model.TableData, out *strings.Builder) {
	var parts []string
	if t.NoHeaderRow {
		parts = append(parts, "header=0")
	}
	if t.HeaderCol {
		parts = append(parts, "headercol=1")
	}
	hasW := false
	for _, w := range t.Widths {
		if w > 0 {
			hasW = true
			break
		}
	}
	if hasW {
		ws := make([]string, len(t.Widths))
		for i, w := range t.Widths {
			if w > 0 {
				ws[i] = formatFloat(w)
			}
		}
		parts = append(parts, "widths="+strings.Join(ws, ","))
	}
	colTints, rowTints, cellTints := encodeTableTintMeta(t)
	if len(colTints) > 0 {
		cs := make([]string, len(colTints))
		for i, ct := range colTints {
			cs[i] = string(ct)
		}
		parts = append(parts, "coltints="+strings.Join(cs, ","))
	}
	if len(rowTints) > 0 {
		rs := make([]string, len(rowTints))
		for i, rt := range rowTints {
			rs[i] = string(rt)
		}
		parts = append(parts, "rowtints="+strings.Join(rs, ","))
	}
	if len(cellTints) > 0 {
		parts = append(parts, "celltints="+strings.Join(cellTints, ","))
	}
	rowAligns, cellAligns := encodeTableAlignMeta(t)
	if len(rowAligns) > 0 {
		as := make([]string, len(rowAligns))
		for i, ra := range rowAligns {
			as[i] = string(ra)
		}
		parts = append(parts, "rowaligns="+strings.Join(as, ","))
	}
	if len(cellAligns) > 0 {
		parts = append(parts, "cellaligns="+strings.Join(cellAligns, ","))
	}
	if len(parts) > 0 {
		out.WriteString("<!-- thinkly:table ")
		out.WriteString(strings.Join(parts, " "))
		out.WriteString(" -->\n")
	}
}

func serializeTable(t *model.TableData, out *strings.Builder) {
	if len(t.Rows) == 0 {
		return
	}
	serializeTableMetaComment(t, out)
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
			for c, cell := range row {
				tag := "td"
				if t.IsHeaderCell(r, c) {
					tag = "th"
				}
				out.WriteByte('<')
				out.WriteString(tag)
				if a := t.CellAlign(r, c); a != model.ColAlignNone {
					out.WriteString(` style="text-align:`)
					out.WriteString(string(a))
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
		// Keep inline code/math spans intact across soft newlines so SerializeInline
		// emits <code>...<br>...</code> / <span class="math">...<br>...</span> on one
		// Markdown line instead of splitting them into separate chips.
		if sp.Marks&(model.MarkCode|model.MarkMath) != 0 || !strings.ContainsRune(sp.Text, '\n') {
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
		if sp.Marks&model.MarkSuperscript != 0 {
			openTags = append(openTags, "<sup>")
			closeTags = append(closeTags, "</sup>")
		}
		if sp.Marks&model.MarkSubscript != 0 {
			openTags = append(openTags, "<sub>")
			closeTags = append(closeTags, "</sub>")
		}
		if sp.Marks&model.MarkCode != 0 {
			openTags = append(openTags, "<code>")
			closeTags = append(closeTags, "</code>")
		}
		if sp.Marks&model.MarkMath != 0 {
			openTags = append(openTags, `<span class="math">`)
			closeTags = append(closeTags, `</span>`)
		}
		for _, t := range openTags {
			out.WriteString(t)
		}
		if sp.Marks&model.MarkMath != 0 {
			writeLiteralMathHTMLText(out, sp.Text)
		} else {
			writeLiteralHTMLText(out, sp.Text)
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
