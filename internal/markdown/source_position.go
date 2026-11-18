package markdown

import (
	"slices"
	"strings"

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
	foldedQuery := strings.ToLower(query)
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
		if foldedQuery == "" || strings.Contains(strings.ToLower(block.PlainText()), foldedQuery) {
			target.Block, blockRange = i, r
			break
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
