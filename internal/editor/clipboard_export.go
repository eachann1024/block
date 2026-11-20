package editor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image/png"
	"strings"

	"block/internal/media"
	"block/internal/model"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const (
	blockTableCellsHTMLPrefix = "<!-- block-cells:"
	blockTableCellsHTMLSuffix = " -->"
)

// writeSelectionToClipboard serializes the current non-empty selection into multi-format
// clipboard data (plain text Markdown, text/markdown, semantic HTML, and PNG for single local images).
func (s *Session) writeSelectionToClipboard(svc ui.Services) bool {
	if !s.hasNonEmptySelection() || s.doc == nil {
		return false
	}
	md := s.SelectedMarkdown()
	if md == "" {
		return false
	}
	a, b := s.orderedSelection()
	if a.Block == b.Block && a.Block >= 0 && a.Block < len(s.doc.Blocks) {
		blk := &s.doc.Blocks[a.Block]
		rlen := s.blockSelectionLen(a.Block)
		whole := a.Offset == 0 && b.Offset >= rlen

		// Single-block partial/full selection inside editable code or raw HTML stays clean plain text.
		if (blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML) && !s.isPreviewOnlyCodeBlock(a.Block) {
			svc.WriteClipboard(md)
			return true
		}

		// Single-cell table or single rich-text inline selection: emit inline HTML without wrapping <p>.
		if blk.Kind == model.BlockTable && blk.Table != nil && !whole {
			if row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
				spans := sliceSpans(blk.Table.Rows[row][col].Spans, relLo, relHi)
				s.writeInlineSpansClipboard(svc, md, spans)
				return true
			}
		} else if blk.Kind != model.BlockTable && blk.Kind != model.BlockImage && blk.Kind != model.BlockDivider && !s.isPreviewOnlyCodeBlock(a.Block) {
			spans := sliceSpans(blk.Spans, a.Offset, b.Offset)
			s.writeInlineSpansClipboard(svc, md, spans)
			return true
		}
	}
	s.writeRichClipboard(svc, md, s.SelectedDoc(), true)
	return true
}

func (s *Session) writeInlineSpansClipboard(svc ui.Services, md string, spans []model.Span) {
	reps := make([]transfer.Representation, 0, 3)
	reps = append(reps,
		transfer.Bytes(transfer.Text, []byte(md)),
		transfer.Bytes(transfer.Markdown, []byte(md)),
	)
	if len(spans) > 0 {
		var sb strings.Builder
		writeClipboardSpansHTML(&sb, spans)
		if frag := sb.String(); frag != "" {
			reps = append(reps, transfer.Bytes(transfer.HTML, []byte(frag)))
		}
	}
	svc.WriteClipboardData(transfer.New(transfer.NewItem(reps...)))
}

// writeRichClipboard writes Markdown plain text, explicit text/markdown, semantic HTML,
// and (for a single local raster image block) PNG image bytes to the system clipboard.
func (s *Session) writeRichClipboard(svc ui.Services, md string, doc *model.Doc, includeHTML bool) {
	if md == "" {
		return
	}
	reps := make([]transfer.Representation, 0, 4)
	reps = append(reps,
		transfer.Bytes(transfer.Text, []byte(md)),
		transfer.Bytes(transfer.Markdown, []byte(md)),
	)
	if includeHTML && doc != nil && len(doc.Blocks) > 0 {
		if frag := s.clipboardHTMLForDoc(doc); frag != "" {
			reps = append(reps, transfer.Bytes(transfer.HTML, []byte(frag)))
		}
	}
	if doc != nil && len(doc.Blocks) == 1 && doc.Blocks[0].Kind == model.BlockImage {
		if pngRaw := s.readLocalImageAsPNG(doc.Blocks[0].ImageSrc); len(pngRaw) > 0 {
			reps = append(reps, transfer.Bytes(transfer.PNG, pngRaw))
		}
	}
	svc.WriteClipboardData(transfer.New(transfer.NewItem(reps...)))
}

// writeTableCellsClipboard writes tab/newline plain text alongside an HTML table fragment
// for partial multi-cell, row, or column table copies.
func (s *Session) writeTableCellsClipboard(svc ui.Services, plain string, rows [][]model.TableCell) {
	if plain == "" && len(rows) == 0 {
		return
	}
	reps := make([]transfer.Representation, 0, 3)
	reps = append(reps,
		transfer.Bytes(transfer.Text, []byte(plain)),
		transfer.Bytes(transfer.Markdown, []byte(plain)),
	)
	if len(rows) > 0 {
		var sb strings.Builder
		sb.WriteString("<table><tbody>")
		for _, row := range rows {
			sb.WriteString("<tr>")
			for _, cell := range row {
				sb.WriteString("<td>")
				writeClipboardSpansHTML(&sb, cell.Spans)
				sb.WriteString("</td>")
			}
			sb.WriteString("</tr>")
		}
		sb.WriteString("</tbody></table>")
		if rawJSON, err := json.Marshal(rows); err == nil {
			sb.WriteString(blockTableCellsHTMLPrefix)
			sb.WriteString(base64.StdEncoding.EncodeToString(rawJSON))
			sb.WriteString(blockTableCellsHTMLSuffix)
		}
		reps = append(reps, transfer.Bytes(transfer.HTML, []byte(sb.String())))
	}
	svc.WriteClipboardData(transfer.New(transfer.NewItem(reps...)))
}

func decodeBlockTableCellsHTML(htmlRaw string) ([][]model.TableCell, bool) {
	start := strings.Index(htmlRaw, blockTableCellsHTMLPrefix)
	if start < 0 {
		return nil, false
	}
	rest := htmlRaw[start+len(blockTableCellsHTMLPrefix):]
	end := strings.Index(rest, blockTableCellsHTMLSuffix)
	if end <= 0 {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(rest[:end])
	if err != nil {
		return nil, false
	}
	var rows [][]model.TableCell
	if err := json.Unmarshal(decoded, &rows); err != nil || len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

func (s *Session) pasteTableCellsGrid(srcRows [][]model.TableCell) bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 || len(srcRows) == 0 {
		return false
	}
	s.clearAutoConvertState()
	idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	if s.hasMultiCellTableSelection() && !s.hasWholeAtomicSelection() {
		a, b := s.orderedSelection()
		if a.Block < 0 || a.Block >= len(s.doc.Blocks) || s.doc.Blocks[a.Block].Table == nil {
			return false
		}
		idx = a.Block
		blk := &s.doc.Blocks[idx]
		s.pushUndo()
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
	} else {
		if s.doc.Blocks[idx].Kind != model.BlockTable || s.doc.Blocks[idx].Table == nil {
			return false
		}
		s.pushUndo()
	}
	b := &s.doc.Blocks[idx]
	tbl := b.Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return false
	}
	startR := clampInt(s.tableActiveRow, 0, len(tbl.Rows)-1)
	startC := clampInt(s.tableActiveCol, 0, max(len(tbl.Rows[0])-1, 0))
	neededCols := max(len(tbl.Rows[0]), 1)
	for _, r := range srcRows {
		if startC+len(r) > neededCols {
			neededCols = startC + len(r)
		}
	}
	neededRows := max(len(tbl.Rows), startR+len(srcRows))
	if neededRows > maxPastedTableCells/neededCols {
		return false
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
	for dr, srcRow := range srcRows {
		for dc, srcCell := range srcRow {
			dst := &tbl.Rows[startR+dr][startC+dc]
			dst.Spans = append([]model.Span(nil), srcCell.Spans...)
		}
	}
	b.Dirty = true
	b.CleanSource = ""
	s.firstLineDeleteArmed = false
	s.focusedBlock = idx
	s.tableActiveBlock = b.ID
	s.tableActiveRow = startR + len(srcRows) - 1
	s.tableActiveCol = startC + max(len(srcRows[len(srcRows)-1])-1, 0)
	endOff := tableCellStartOffset(tbl, s.tableActiveRow, s.tableActiveCol) +
		len([]rune(tbl.Rows[s.tableActiveRow][s.tableActiveCol].PlainText()))
	s.selAnchor = Position{Block: idx, Offset: endOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.multiSelFocusPending = false
	s.markDirty()
	return true
}

func (s *Session) readClipboardImage(src string, limit int64) ([]byte, error) {
	f, _, err := media.OpenLocal(s.mediaDir(), src, limit)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return media.ReadFile(f, limit)
}

func (s *Session) readLocalImageAsPNG(src string) []byte {
	raw, err := s.readClipboardImage(src, media.MaxImageBytes)
	if err != nil {
		return nil
	}
	img, _, format, err := media.DecodeRaster(raw)
	if err != nil {
		return nil
	}
	if format == "png" {
		return raw
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil || out.Len() > media.MaxImageBytes {
		return nil
	}
	return out.Bytes()
}

func (s *Session) clipboardHTMLForDoc(doc *model.Doc) string {
	if doc == nil || len(doc.Blocks) == 0 {
		return ""
	}
	var sb strings.Builder
	imageBudget := media.MaxImageBytes
	// Group consecutive list items into <ul> / <ol> containers for clean pasting into Word/Feishu/Notion.
	for i := 0; i < len(doc.Blocks); {
		b := &doc.Blocks[i]
		if b.Kind.IsList() {
			tag := "ul"
			if b.Kind == model.BlockNumbered {
				tag = "ol"
			}
			if tag == "ol" && b.ListIndex > 1 {
				fmt.Fprintf(&sb, `<ol start="%d">`, b.ListIndex)
			} else {
				sb.WriteString("<" + tag + ">")
			}
			for i < len(doc.Blocks) && doc.Blocks[i].Kind == b.Kind {
				sb.WriteString("<li>")
				if doc.Blocks[i].Kind == model.BlockTodo {
					if doc.Blocks[i].Checked {
						sb.WriteString(`<input type="checkbox" checked disabled> `)
					} else {
						sb.WriteString(`<input type="checkbox" disabled> `)
					}
				}
				writeClipboardSpansHTML(&sb, doc.Blocks[i].Spans)
				sb.WriteString("</li>")
				i++
			}
			sb.WriteString("</" + tag + ">")
			continue
		}
		s.writeClipboardBlockHTML(&sb, b, &imageBudget)
		i++
	}
	return sb.String()
}

func (s *Session) writeClipboardBlockHTML(sb *strings.Builder, b *model.Block, imageBudget *int) {
	if b == nil {
		return
	}
	switch {
	case b.Kind.IsHeading():
		lvl := b.Kind.HeadingLevel()
		fmt.Fprintf(sb, "<h%d>", lvl)
		writeClipboardSpansHTML(sb, b.Spans)
		fmt.Fprintf(sb, "</h%d>", lvl)

	case b.Kind == model.BlockQuote || b.Kind == model.BlockCallout:
		sb.WriteString("<blockquote><p>")
		writeClipboardSpansHTML(sb, b.Spans)
		sb.WriteString("</p></blockquote>")

	case b.Kind == model.BlockCode:
		if lang := strings.TrimSpace(b.Language); lang != "" {
			fmt.Fprintf(sb, `<pre><code class="language-%s">%s</code></pre>`, html.EscapeString(lang), html.EscapeString(b.Code))
		} else {
			fmt.Fprintf(sb, "<pre><code>%s</code></pre>", html.EscapeString(b.Code))
		}

	case b.Kind == model.BlockRawHTML:
		fmt.Fprintf(sb, "<pre><code>%s</code></pre>", html.EscapeString(b.Code))

	case b.Kind == model.BlockDivider:
		sb.WriteString("<hr>")

	case b.Kind == model.BlockImage:
		alt := html.EscapeString(b.ImageAlt)
		raw, err := s.readClipboardImage(b.ImageSrc, int64(*imageBudget))
		if err == nil {
			if _, _, format, err := media.DecodeRaster(raw); err == nil {
				*imageBudget -= len(raw)
				fmt.Fprintf(sb, `<p><img src="data:%s;base64,%s" alt="%s"></p>`, media.ImageMIME(format), base64.StdEncoding.EncodeToString(raw), alt)
				return
			}
		}
		fmt.Fprintf(sb, "<p>%s</p>", alt)

	case b.Kind == model.BlockTable && b.Table != nil && len(b.Table.Rows) > 0:
		tbl := b.Table
		sb.WriteString("<table>")
		startBody := 0
		if !tbl.NoHeaderRow {
			sb.WriteString("<thead><tr>")
			for cIdx, cell := range tbl.Rows[0] {
				writeClipboardTableCellHTML(sb, "th", cell, tbl, cIdx)
			}
			sb.WriteString("</tr></thead>")
			startBody = 1
		}
		if startBody < len(tbl.Rows) {
			sb.WriteString("<tbody>")
			for rIdx := startBody; rIdx < len(tbl.Rows); rIdx++ {
				sb.WriteString("<tr>")
				for cIdx, cell := range tbl.Rows[rIdx] {
					tag := "td"
					if tbl.IsHeaderCell(rIdx, cIdx) {
						tag = "th"
					}
					writeClipboardTableCellHTML(sb, tag, cell, tbl, cIdx)
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</tbody>")
		}
		sb.WriteString("</table>")

	default:
		sb.WriteString("<p>")
		writeClipboardSpansHTML(sb, b.Spans)
		sb.WriteString("</p>")
	}
}

func writeClipboardTableCellHTML(sb *strings.Builder, tag string, cell model.TableCell, tbl *model.TableData, cIdx int) {
	align := cell.Align
	if align == model.ColAlignNone && cIdx < len(tbl.Aligns) {
		align = tbl.Aligns[cIdx]
	}
	switch align {
	case model.ColAlignCenter:
		fmt.Fprintf(sb, `<%s style="text-align:center">`, tag)
	case model.ColAlignRight:
		fmt.Fprintf(sb, `<%s style="text-align:right">`, tag)
	default:
		fmt.Fprintf(sb, "<%s>", tag)
	}
	writeClipboardSpansHTML(sb, cell.Spans)
	fmt.Fprintf(sb, "</%s>", tag)
}

func writeClipboardSpansHTML(sb *strings.Builder, spans []model.Span) {
	for _, sp := range spans {
		text := strings.ReplaceAll(html.EscapeString(sp.Text), "\n", "<br>")
		if sp.Marks&model.MarkCode != 0 {
			text = "<code>" + text + "</code>"
		}
		if sp.Marks&model.MarkMath != 0 {
			text = `<code class="math">` + text + `</code>`
		}
		if sp.Marks&model.MarkBold != 0 {
			text = "<strong>" + text + "</strong>"
		}
		if sp.Marks&model.MarkItalic != 0 {
			text = "<em>" + text + "</em>"
		}
		if sp.Marks&model.MarkUnderline != 0 {
			text = "<u>" + text + "</u>"
		}
		if sp.Marks&model.MarkStrike != 0 {
			text = "<s>" + text + "</s>"
		}
		if sp.Marks&model.MarkSuperscript != 0 {
			text = "<sup>" + text + "</sup>"
		}
		if sp.Marks&model.MarkSubscript != 0 {
			text = "<sub>" + text + "</sub>"
		}
		if sp.BGColor != "" {
			text = "<mark>" + text + "</mark>"
		}
		if href := media.WebLink(sp.Link); href != "" {
			text = fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(href), text)
		}
		sb.WriteString(text)
	}
}
