package markdown

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/fuzzy"
	"block/internal/model"
)

type sourceLineRange struct {
	start, end int
}

// SourceTarget identifies a rendered block, optionally narrowed to a table cell.
type SourceTarget struct {
	Block, Row, Column int
}

// SourceMap relates the current Markdown to the blocks already in the editor.
// Edited documents get their ranges from the same writer that saves them.
type SourceMap struct {
	Text   string
	doc    *model.Doc
	ranges []sourceLineRange
}

func MapSource(doc *model.Doc) SourceMap {
	m := SourceMap{doc: doc}
	m.Text = serializeMarkdown(doc, &m.ranges)
	return m
}

// BlockLineRange returns the 1-based inclusive Markdown source line range of blockIdx.
func (m SourceMap) BlockLineRange(blockIdx int) (start, end int, ok bool) {
	if m.doc == nil || blockIdx < 0 || blockIdx >= len(m.doc.Blocks) {
		return 0, 0, false
	}
	if m.ranges != nil && blockIdx < len(m.ranges) {
		r := m.ranges[blockIdx]
		if r.start > 0 {
			return r.start, r.end, true
		}
	}
	b := &m.doc.Blocks[blockIdx]
	if b.SourceLine > 0 {
		endLine := b.SourceEndLine
		if endLine < b.SourceLine {
			endLine = b.SourceLine
		}
		return b.SourceLine, endLine, true
	}
	return 0, 0, false
}

// Locate uses a 1-based source line and a 0-based byte column.
func (m SourceMap) Locate(line, column int, query string) (SourceTarget, bool) {
	target := SourceTarget{Block: -1, Row: -1, Column: -1}
	if line < 1 || m.doc == nil {
		return target, false
	}
	source := m.Text
	if m.ranges == nil {
		var originalLines []int
		source = decodeRawBlockGuardWithLines(source, &originalLines)
		if len(originalLines) > 0 {
			index, found := slices.BinarySearch(originalLines, line)
			if !found {
				return target, false
			}
			line = index + 1
		}
		if strings.HasPrefix(source, "\uFEFF") {
			source = strings.TrimPrefix(source, "\uFEFF")
			if line == 1 {
				column -= len("\uFEFF")
			}
		}
	}

	var blockRange sourceLineRange
	foldedQuery := strings.ToLower(strings.TrimSpace(query))
	queryTokens := fuzzy.Tokens(query)
	matchedTokenBlock := -1
	var matchedTokenRange sourceLineRange
	for i := range m.doc.Blocks {
		block := &m.doc.Blocks[i]
		r := sourceLineRange{block.SourceLine, block.SourceEndLine}
		if m.ranges != nil {
			r = m.ranges[i]
		}
		if r.start == 0 || line < r.start || line > r.end {
			continue
		}
		if target.Block < 0 {
			target.Block, blockRange = i, r
		}
		// HTML list items can share a source line. Favor the item that displays the match.
		if foldedQuery == "" {
			target.Block, blockRange = i, r
			break
		}
		plain := strings.ToLower(block.PlainText())
		if strings.Contains(plain, foldedQuery) {
			target.Block, blockRange = i, r
			break
		}
		if len(queryTokens) > 1 && matchedTokenBlock < 0 {
			for _, tok := range queryTokens {
				if strings.Contains(plain, tok) {
					matchedTokenBlock, matchedTokenRange = i, r
					break
				}
			}
		}
	}
	if matchedTokenBlock >= 0 && foldedQuery != "" {
		plain := strings.ToLower(m.doc.Blocks[target.Block].PlainText())
		if !strings.Contains(plain, foldedQuery) {
			target.Block, blockRange = matchedTokenBlock, matchedTokenRange
		}
	}
	if target.Block < 0 {
		return target, false
	}
	block := &m.doc.Blocks[target.Block]
	if block.Table == nil {
		return target, true
	}

	lines := splitLinesInclusive(source)
	if line > len(lines) || blockRange.end > len(lines) || column < 0 {
		return target, true
	}
	if block.Table.WasHTML {
		tableSource := strings.Join(lines[blockRange.start-1:blockRange.end], "")
		offset := column
		for _, precedingLine := range lines[blockRange.start-1 : line-1] {
			offset += len(precedingLine)
		}
		parseHTMLTableCells(tableSource, func(row, col, start, end int) {
			if start <= offset && offset < end {
				target.Row, target.Column = row, col
			}
		})
		return target, true
	}

	row := line - blockRange.start
	if row == 1 { // Pipe table alignment separator.
		return target, true
	}
	if row > 1 {
		row--
	}
	raw := lines[line-1]
	body := raw
	for depth := 0; depth < block.QuoteDepth; depth++ {
		trimmed := strings.TrimLeft(body, " \t")
		if !strings.HasPrefix(trimmed, ">") {
			break
		}
		body = trimmed[1:]
	}
	column -= len(raw) - len(body)
	walkPipeCells(body, func(col, start, end int) {
		if start <= column && column < end {
			target.Row, target.Column = row, col
		}
	})
	return target, true
}

func stripQuotePrefixLine(raw string, depth int) (body string, strippedRunes int) {
	body = raw
	for d := 0; d < depth; d++ {
		trimmed := strings.TrimLeft(body, " ")
		if len(body)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, ">") {
			break
		}
		next := strings.TrimPrefix(trimmed[1:], " ")
		strippedRunes += utf8.RuneCountInString(body) - utf8.RuneCountInString(next)
		body = next
	}
	return body, strippedRunes
}

// BlockContentPosition maps a 1-based Markdown source line and optional 1-based rune column
// inside blockIdx to a 0-based line index and optional 1-based rune column inside the block's
// rendered text (block.Code for BlockCode/BlockRawHTML, block.PlainText() otherwise),
// uniformly accounting for blockquote prefixes, indentation, fences, frontmatter, and math delimiters.
func (m SourceMap) BlockContentPosition(blockIdx, line, col int) (relLine int, contentCol int, ok bool) {
	if m.doc == nil || blockIdx < 0 || blockIdx >= len(m.doc.Blocks) || line < 1 {
		return 0, 0, false
	}
	source := m.Text
	mappedLine := line
	mappedCol := col
	if m.ranges == nil {
		var originalLines []int
		source = decodeRawBlockGuardWithLines(source, &originalLines)
		if len(originalLines) > 0 {
			index, found := slices.BinarySearch(originalLines, line)
			if !found {
				return 0, 0, false
			}
			mappedLine = index + 1
		}
		if strings.HasPrefix(source, "\uFEFF") {
			source = strings.TrimPrefix(source, "\uFEFF")
			if mappedLine == 1 && mappedCol > 0 {
				mappedCol = max(mappedCol-1, 1)
			}
		}
	}

	startLine, endLine, hasRange := m.BlockLineRange(blockIdx)
	if !hasRange {
		return 0, 0, false
	}
	lines := splitLinesInclusive(source)
	if len(lines) == 0 || startLine < 1 || startLine > len(lines) {
		return 0, 0, false
	}
	clampedEnd := min(max(endLine, startLine), len(lines))
	clampedLine := min(max(mappedLine, startLine), clampedEnd)

	block := &m.doc.Blocks[blockIdx]
	qDepth := block.QuoteDepth
	if block.Kind == model.BlockQuote && qDepth == 0 {
		qDepth = 1
	}

	firstRaw := trimLineEnding(lines[startLine-1])
	firstUnquoted, _ := stripQuotePrefixLine(firstRaw, qDepth)

	firstContentLine := startLine
	stripIndentCols := 0
	inlinePrefixRunes := 0
	if block.Kind == model.BlockCode {
		switch {
		case block.Language == "yaml-frontmatter" && isFrontmatterOpen(firstUnquoted):
			firstContentLine = startLine + 1
		case block.Language == "math":
			if indent, _, isInline := parseInlineMathBlock(firstUnquoted); isInline {
				firstContentLine = startLine
				stripIndentCols = indent
				inlinePrefixRunes = 2 // "$$"
			} else if indentCols, _, _, _, _, isFence := parseFenceAnyIndent(firstUnquoted); isFence {
				firstContentLine = startLine + 1
				stripIndentCols = indentCols
			}
		default:
			if indentCols, _, _, _, _, isFence := parseFenceAnyIndent(firstUnquoted); isFence {
				firstContentLine = startLine + 1
				stripIndentCols = indentCols
			} else if block.Indent > 0 {
				stripIndentCols = block.Indent
			} else if ic, _ := leadingColumns(firstUnquoted); ic >= 4 {
				stripIndentCols = 4
			}
		}
	}

	if clampedLine < firstContentLine {
		return 0, 0, true
	}
	relLine = clampedLine - firstContentLine
	if block.Kind == model.BlockCode {
		codeLineCount := strings.Count(block.Code, "\n") + 1
		if relLine >= codeLineCount {
			return codeLineCount - 1, 0, true
		}
	}

	if mappedCol > 0 {
		targetRaw := trimLineEnding(lines[clampedLine-1])
		targetUnquoted, prefixRunes := stripQuotePrefixLine(targetRaw, qDepth)
		lineBody := targetUnquoted
		if block.Kind == model.BlockCode {
			if stripIndentCols > 0 {
				stripped := stripColumns(targetUnquoted, stripIndentCols)
				prefixRunes += utf8.RuneCountInString(targetUnquoted) - utf8.RuneCountInString(stripped)
				lineBody = stripped
			}
			prefixRunes += inlinePrefixRunes
			contentCol = max(mappedCol-prefixRunes, 1)
		} else if block.Kind == model.BlockRawHTML {
			contentCol = max(mappedCol-prefixRunes, 1)
		} else {
			if clampedLine == startLine {
				switch {
				case block.Kind.IsHeading():
					if _, hText, okH := classifyHeading(targetUnquoted); okH {
						prefixRunes += headingPrefixRunes(targetUnquoted)
						lineBody = hText
					}
				case block.Kind.IsList():
					if lm, okL := classifyListMarker(targetUnquoted); okL {
						prefixRunes += utf8.RuneCountInString(targetUnquoted) - utf8.RuneCountInString(lm.body)
						lineBody = lm.body
					}
				case block.Kind == model.BlockParagraph && block.Indent > 0:
					if _, trimmed := leadingColumns(targetUnquoted); trimmed != targetUnquoted {
						prefixRunes += utf8.RuneCountInString(targetUnquoted) - utf8.RuneCountInString(trimmed)
						lineBody = trimmed
					}
				}
			} else if block.Kind.IsList() {
				if _, trimmed := leadingColumns(targetUnquoted); trimmed != targetUnquoted {
					prefixRunes += utf8.RuneCountInString(targetUnquoted) - utf8.RuneCountInString(trimmed)
					lineBody = trimmed
				}
			}
			rawBodyCol := max(mappedCol-prefixRunes, 1)
			var defs map[string]refDef
			if strings.Contains(source, "]:") {
				defs, _ = collectRefDefsFromLines(source, lines)
			}
			contentCol = mapInlineSourceColToVisibleCol(lineBody, rawBodyCol, defs)
		}
	}
	return relLine, contentCol, true
}

func headingPrefixRunes(line string) int {
	body := strings.TrimLeft(line, " ")
	hashes := 0
	for hashes < len(body) && body[hashes] == '#' {
		hashes++
	}
	after := strings.TrimLeft(body[hashes:], " \t")
	return utf8.RuneCountInString(line) - utf8.RuneCountInString(after)
}

func runeCol1ToByteOffset(s string, col1 int) int {
	if col1 <= 1 {
		return 0
	}
	rIdx := 0
	for off := range s {
		if rIdx == col1-1 {
			return off
		}
		rIdx++
	}
	return len(s)
}

// mapInlineSourceColToVisibleCol maps a 1-based rune column in a single line of
// inline Markdown source to the corresponding 1-based rune column in the
// rendered visible text, accounting for hidden Markdown/HTML syntax tokens.
func mapInlineSourceColToVisibleCol(rawLine string, srcRuneCol1 int, defs map[string]refDef) int {
	if srcRuneCol1 <= 1 || rawLine == "" {
		return 1
	}
	if !strings.ContainsAny(rawLine, "\\`*_~![<&$^=") && !strings.Contains(rawLine, "://") {
		return min(srcRuneCol1, utf8.RuneCountInString(rawLine)+1)
	}
	targetByte := runeCol1ToByteOffset(rawLine, srcRuneCol1)
	toks := scanInlineTokensWithCtx(rawLine, defs, false, nil)
	resolveDelimiters(toks)

	visRunes := 0
	for _, tok := range toks {
		var txt string
		switch tok.kind {
		case tokText, tokCode, tokMath, tokImage:
			txt = tok.text
		case tokDelim:
			if tok.count > 0 {
				txt = strings.Repeat(string(tok.ch), tok.count)
			}
		default:
			continue
		}
		tokRunes := utf8.RuneCountInString(txt)
		if tokRunes == 0 {
			continue
		}
		if targetByte < tok.srcStart {
			return visRunes + 1
		}
		if targetByte >= tok.srcStart && targetByte < tok.srcEnd {
			switch tok.kind {
			case tokText:
				if len(txt) == tok.srcEnd-tok.srcStart {
					rel := utf8.RuneCountInString(rawLine[tok.srcStart:targetByte])
					return visRunes + min(rel, tokRunes) + 1
				}
				return visRunes + 1
			case tokCode, tokMath:
				inStart, inEnd := tok.innerStart, tok.innerEnd
				if inEnd <= inStart {
					return visRunes + 1
				}
				if targetByte < inStart {
					return visRunes + 1
				}
				if targetByte >= inEnd {
					return visRunes + tokRunes
				}
				rel := utf8.RuneCountInString(rawLine[inStart:targetByte])
				return visRunes + min(rel, tokRunes) + 1
			case tokDelim:
				rel := min(max(targetByte-tok.srcStart, 0), tokRunes)
				return visRunes + rel + 1
			default:
				return visRunes + 1
			}
		}
		visRunes += tokRunes
	}
	return visRunes + 1
}

// TableCellVisibleColumn maps a 1-based Markdown source line and 1-based rune column
// inside a table block to the 1-based visible rune column inside the resolved table cell.
func (m SourceMap) TableCellVisibleColumn(blockIdx, line, col int) (visCol int, ok bool) {
	if m.doc == nil || blockIdx < 0 || blockIdx >= len(m.doc.Blocks) || line < 1 || col < 1 {
		return 0, false
	}
	block := &m.doc.Blocks[blockIdx]
	if block.Table == nil {
		return 0, false
	}
	source := m.Text
	mappedLine := line
	mappedCol := col
	if m.ranges == nil {
		var originalLines []int
		source = decodeRawBlockGuardWithLines(source, &originalLines)
		if len(originalLines) > 0 {
			index, found := slices.BinarySearch(originalLines, line)
			if !found {
				return 0, false
			}
			mappedLine = index + 1
		}
		if strings.HasPrefix(source, "\uFEFF") {
			source = strings.TrimPrefix(source, "\uFEFF")
			if mappedLine == 1 {
				mappedCol = max(mappedCol-1, 1)
			}
		}
	}
	startLine, endLine, hasRange := m.BlockLineRange(blockIdx)
	if !hasRange {
		return 0, false
	}
	lines := splitLinesInclusive(source)
	if len(lines) == 0 || mappedLine < startLine || mappedLine > endLine || mappedLine > len(lines) || endLine > len(lines) {
		return 0, false
	}
	var defs map[string]refDef
	if strings.Contains(source, "]:") {
		defs, _ = collectRefDefsFromLines(source, lines)
	}
	mapCellRaw := func(rawCell string, relByte int) (int, bool) {
		trimmedLeft := strings.TrimLeftFunc(rawCell, func(r rune) bool {
			return r != '\u0085' && unicode.IsSpace(r)
		})
		leadBytes := len(rawCell) - len(trimmedLeft)
		trimmedCell := trimTableCellSpace(rawCell)
		if trimmedCell == "" {
			return 0, false
		}
		adjByte := min(max(relByte-leadBytes, 0), len(trimmedCell))
		srcCellCol := utf8.RuneCountInString(trimmedCell[:adjByte]) + 1
		return mapInlineSourceColToVisibleCol(trimmedCell, srcCellCol, defs), true
	}

	if block.Table.WasHTML {
		tableSource := strings.Join(lines[startLine-1:endLine], "")
		lineStr := lines[mappedLine-1]
		byteOff := runeCol1ToByteOffset(lineStr, mappedCol)
		for _, preceding := range lines[startLine-1 : mappedLine-1] {
			byteOff += len(preceding)
		}
		foundStart, foundEnd := -1, -1
		parseHTMLTableCells(tableSource, func(_, _, start, end int) {
			if start <= byteOff && byteOff < end {
				foundStart, foundEnd = start, end
			}
		})
		if foundStart < 0 || foundEnd <= foundStart {
			return 0, false
		}
		return mapCellRaw(tableSource[foundStart:foundEnd], byteOff-foundStart)
	}

	row := mappedLine - startLine
	if row == 1 { // Pipe table separator row.
		return 0, false
	}
	raw := trimLineEnding(lines[mappedLine-1])
	body := raw
	for depth := 0; depth < block.QuoteDepth; depth++ {
		trimmed := strings.TrimLeft(body, " \t")
		if !strings.HasPrefix(trimmed, ">") {
			break
		}
		body = trimmed[1:]
	}
	rawByte := runeCol1ToByteOffset(raw, mappedCol)
	byteCol := rawByte - (len(raw) - len(body))
	if byteCol < 0 {
		byteCol = 0
	}
	foundStart, foundEnd := -1, -1
	walkPipeCells(body, func(_, start, end int) {
		if start <= byteCol && byteCol < end {
			foundStart, foundEnd = start, end
		}
	})
	if foundStart < 0 || foundEnd <= foundStart {
		return 0, false
	}
	return mapCellRaw(body[foundStart:foundEnd], byteCol-foundStart)
}
