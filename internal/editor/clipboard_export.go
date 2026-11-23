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
		if blk.Kind == model.BlockTable && blk.Table != nil {
			singleCellActive := s.tableActiveBlock == blk.ID && !s.hasWholeAtomicSelection()
			if !whole || singleCellActive {
				if row, col, relLo, relHi, ok := tableCellAtRuneRange(blk.Table, a.Offset, b.Offset); ok {
					spans := sliceSpansForClipboard(blk.Table.Rows[row][col].Spans, relLo, relHi)
					s.writeInlineSpansClipboard(svc, md, spans)
					return true
				}
			}
			if !whole {
				if grid := extractTableMultiCellGrid(blk.Table, a.Offset, b.Offset); len(grid) > 0 {
					s.writeTableCellsClipboard(svc, serializeTableMultiCellPlain(blk.Table, a.Offset, b.Offset), md, grid)
					return true
				}
			}
		} else if blk.Kind != model.BlockTable && blk.Kind != model.BlockImage && blk.Kind != model.BlockDivider && !s.isPreviewOnlyCodeBlock(a.Block) {
			spans := sliceSpansForClipboard(blk.Spans, a.Offset, b.Offset)
			if whole && hasInlineImageSpan(spans) {
				cp := blk.Clone()
				cp.ID = model.NewBlockID()
				s.writeRichClipboard(svc, md, &model.Doc{Blocks: []model.Block{cp}, TrailingLine: "\n"}, true)
				return true
			}
			s.writeInlineSpansClipboard(svc, md, spans)
			return true
		}
	}
	s.writeRichClipboard(svc, md, s.SelectedDoc(), true)
	return true
}

func hasInlineImageSpan(spans []model.Span) bool {
	for _, sp := range spans {
		if sp.ImageSrc != "" {
			return true
		}
	}
	return false
}

// writeInlineSpansClipboard keeps text/plain free of Markdown markers (like Notion / Feishu text
// selections) while text/markdown and HTML carry formatting for rich targets and Block itself.
func (s *Session) writeInlineSpansClipboard(svc ui.Services, md string, spans []model.Span) {
	plain := model.PlainTextOfSpans(spans)
	if plain == "" {
		plain = md
	}
	reps := make([]transfer.Representation, 0, 3)
	reps = append(reps,
		transfer.Bytes(transfer.Text, []byte(plain)),
		transfer.Bytes(transfer.Markdown, []byte(md)),
	)
	if len(spans) > 0 {
		var sb strings.Builder
		imageBudget := media.MaxImageBytes
		s.writeClipboardSpansHTML(&sb, spans, &imageBudget)
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

// writeTableCellsClipboard writes unformatted tab/newline text/plain (spreadsheet friendly),
// tab/newline Markdown, and an HTML table fragment for partial multi-cell, row, or column table copies.
func (s *Session) writeTableCellsClipboard(svc ui.Services, plain, md string, rows [][]model.TableCell) {
	if md == "" && len(rows) == 0 {
		return
	}
	if strings.Trim(plain, "\t\n") == "" {
		plain = md
	}
	reps := make([]transfer.Representation, 0, 3)
	reps = append(reps,
		transfer.Bytes(transfer.Text, []byte(plain)),
		transfer.Bytes(transfer.Markdown, []byte(md)),
	)
	if len(rows) > 0 {
		var sb strings.Builder
		imageBudget := media.MaxImageBytes
		sb.WriteString("<table><tbody>")
		for _, row := range rows {
			sb.WriteString("<tr>")
			for _, cell := range row {
				sb.WriteString("<td>")
				s.writeClipboardSpansHTML(&sb, cell.Spans, &imageBudget)
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

func extractTableMultiCellGrid(tbl *model.TableData, startRune, endRune int) [][]model.TableCell {
	if tbl == nil || startRune >= endRune {
		return nil
	}
	type cellEntry struct {
		row   int
		col   int
		spans []model.Span
		align model.ColAlign
		tint  model.TableTint
	}
	var entries []cellEntry
	minRow, maxRow := -1, -1
	minCol, maxCol := -1, -1
	pos := 0
	for rIdx, r := range tbl.Rows {
		for cIdx, cell := range r {
			clen := len([]rune(cell.PlainText()))
			cStart, cEnd := pos, pos+clen
			touched := false
			var sub []model.Span
			if clen > 0 {
				ovLo := max(startRune, cStart) - cStart
				ovHi := min(endRune, cEnd) - cStart
				if ovLo < ovHi {
					touched = true
					sub = sliceSpansForClipboard(cell.Spans, ovLo, ovHi)
				}
			} else if startRune <= cStart && cStart <= endRune {
				touched = true
			}
			if touched {
				if minRow < 0 || rIdx < minRow {
					minRow = rIdx
				}
				if rIdx > maxRow {
					maxRow = rIdx
				}
				if minCol < 0 || cIdx < minCol {
					minCol = cIdx
				}
				if cIdx > maxCol {
					maxCol = cIdx
				}
				entries = append(entries, cellEntry{
					row:   rIdx,
					col:   cIdx,
					spans: sub,
					align: cell.Align,
					tint:  cell.Tint,
				})
			}
			pos = cEnd
			if cIdx+1 < len(r) {
				pos++ // '\t'
			}
		}
		if rIdx+1 < len(tbl.Rows) {
			pos++ // '\n'
		}
	}
	if len(entries) == 0 {
		return nil
	}
	numRows := maxRow - minRow + 1
	numCols := maxCol - minCol + 1
	grid := make([][]model.TableCell, numRows)
	for r := range grid {
		grid[r] = make([]model.TableCell, numCols)
	}
	for _, e := range entries {
		grid[e.row-minRow][e.col-minCol] = model.TableCell{
			Spans: e.spans,
			Align: e.align,
			Tint:  e.tint,
		}
	}
	return grid
}

func (s *Session) pasteTableCellsGrid(srcRows [][]model.TableCell) bool {
	if s.readOnly || s.doc == nil || len(s.doc.Blocks) == 0 || len(srcRows) == 0 {
		return false
	}
	multiCell := s.hasMultiCellTableSelection() || (s.hasWholeAtomicSelection() && s.isTablePasteTarget())
	idx := clampInt(s.focusedBlock, 0, len(s.doc.Blocks)-1)
	var a, b Position
	if multiCell {
		a, b = s.orderedSelection()
		if a.Block < 0 || a.Block >= len(s.doc.Blocks) || s.doc.Blocks[a.Block].Table == nil {
			return false
		}
		idx = a.Block
	} else if s.doc.Blocks[idx].Kind != model.BlockTable || s.doc.Blocks[idx].Table == nil {
		return false
	}
	blk := &s.doc.Blocks[idx]
	tbl := blk.Table
	if tbl == nil || len(tbl.Rows) == 0 {
		return false
	}
	// Validate target bounds and maxPastedTableCells BEFORE pushing undo or mutating any cell.
	startR := clampInt(s.tableActiveRow, 0, len(tbl.Rows)-1)
	startC := clampInt(s.tableActiveCol, 0, max(len(tbl.Rows[0])-1, 0))
	if multiCell {
		startR, startC = 0, 0
		if row, col, _, _, ok := tableCellAtRuneRange(tbl, a.Offset, a.Offset); ok {
			startR, startC = row, col
		}
	}
	neededCols := max(len(tbl.Rows[0]), 1)
	for _, r := range srcRows {
		if startC+len(r) > neededCols {
			neededCols = startC + len(r)
		}
	}
	neededRows := max(len(tbl.Rows), startR+len(srcRows))
	if neededCols <= 0 || neededCols > maxPastedTableCols || neededRows > maxPastedTableCells/neededCols {
		return false
	}

	s.clearAutoConvertState()
	s.pushUndo()
	if multiCell {
		clearTableRange(tbl, a.Offset, b.Offset, "")
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
	blk.Dirty = true
	blk.CleanSource = ""
	s.firstLineDeleteArmed = false
	s.focusedBlock = idx
	s.tableActiveBlock = blk.ID
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
				s.writeClipboardSpansHTML(&sb, doc.Blocks[i].Spans, &imageBudget)
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
		s.writeClipboardSpansHTML(sb, b.Spans, imageBudget)
		fmt.Fprintf(sb, "</h%d>", lvl)

	case b.Kind == model.BlockQuote || b.Kind == model.BlockCallout:
		sb.WriteString("<blockquote><p>")
		s.writeClipboardSpansHTML(sb, b.Spans, imageBudget)
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
		tag := s.clipboardImageTagHTMLWithWidth(b.ImageSrc, b.ImageAlt, b.ImageTitle, b.ImageWidth, imageBudget)
		if b.ImageAlign != "" && b.ImageAlign != model.ImageAlignLeft {
			fmt.Fprintf(sb, `<p align="%s">%s</p>`, html.EscapeString(string(b.ImageAlign)), tag)
		} else {
			fmt.Fprintf(sb, "<p>%s</p>", tag)
		}

	case b.Kind == model.BlockTable && b.Table != nil && len(b.Table.Rows) > 0:
		tbl := b.Table
		sb.WriteString("<table>")
		startBody := 0
		if !tbl.NoHeaderRow {
			sb.WriteString("<thead><tr>")
			for cIdx, cell := range tbl.Rows[0] {
				s.writeClipboardTableCellHTML(sb, "th", cell, tbl, cIdx, imageBudget)
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
					s.writeClipboardTableCellHTML(sb, tag, cell, tbl, cIdx, imageBudget)
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</tbody>")
		}
		sb.WriteString("</table>")

	default:
		sb.WriteString("<p>")
		s.writeClipboardSpansHTML(sb, b.Spans, imageBudget)
		sb.WriteString("</p>")
	}
}

func (s *Session) clipboardImageTagHTML(src, alt, title string, imageBudget *int) string {
	return s.clipboardImageTagHTMLWithWidth(src, alt, title, 0, imageBudget)
}

func (s *Session) clipboardImageTagHTMLWithWidth(src, alt, title string, width float32, imageBudget *int) string {
	escAlt := strings.NewReplacer("\r", "&#13;", "\n", "&#10;").Replace(html.EscapeString(alt))
	titleAttr := ""
	if title != "" {
		escTitle := strings.NewReplacer("\r", "&#13;", "\n", "&#10;").Replace(html.EscapeString(title))
		titleAttr = fmt.Sprintf(` title="%s"`, escTitle)
	}
	widthAttr := ""
	if width > 0 {
		widthAttr = fmt.Sprintf(` width="%g%%"`, width)
	}
	if raw, err := s.readClipboardImage(src, int64(*imageBudget)); err == nil {
		if _, _, format, err := media.DecodeRaster(raw); err == nil {
			*imageBudget -= len(raw)
			return fmt.Sprintf(`<img src="data:%s;base64,%s" alt="%s"%s%s>`, media.ImageMIME(format), base64.StdEncoding.EncodeToString(raw), escAlt, titleAttr, widthAttr)
		}
	}
	if isSafeClipboardRasterDataURI(src, imageBudget) {
		return fmt.Sprintf(`<img src="%s" alt="%s"%s%s>`, html.EscapeString(strings.TrimSpace(src)), escAlt, titleAttr, widthAttr)
	}
	if isSafeClipboardRef(src) {
		return fmt.Sprintf(`<img src="%s" alt="%s"%s%s>`, html.EscapeString(src), escAlt, titleAttr, widthAttr)
	}
	return escAlt
}

func isSafeClipboardRasterDataURI(src string, imageBudget *int) bool {
	s := strings.TrimSpace(src)
	comma := strings.IndexByte(s, ',')
	if comma <= 0 {
		return false
	}
	header := strings.ToLower(strings.TrimSpace(s[:comma]))
	switch header {
	case "data:image/png;base64", "data:image/jpeg;base64", "data:image/jpg;base64", "data:image/webp;base64", "data:image/gif;base64":
	default:
		return false
	}
	payload := strings.TrimSpace(s[comma+1:])
	if payload == "" || base64.StdEncoding.DecodedLen(len(payload)) > *imageBudget {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 || len(raw) > *imageBudget {
		return false
	}
	if _, _, _, err := media.DecodeRaster(raw); err != nil {
		return false
	}
	*imageBudget -= len(raw)
	return true
}

func isSafeClipboardRef(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if media.WebLink(s) != "" {
		return true
	}
	// Allow safe relative URLs/paths without a dangerous scheme prefix.
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			return false
		}
		if c == '/' || c == '?' || c == '#' {
			return true
		}
	}
	return true
}

func (s *Session) writeClipboardTableCellHTML(sb *strings.Builder, tag string, cell model.TableCell, tbl *model.TableData, cIdx int, imageBudget *int) {
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
	s.writeClipboardSpansHTML(sb, cell.Spans, imageBudget)
	fmt.Fprintf(sb, "</%s>", tag)
}

func (s *Session) writeClipboardSpansHTML(sb *strings.Builder, spans []model.Span, imageBudget *int) {
	for i := 0; i < len(spans); {
		href := ""
		if isSafeClipboardRef(spans[i].Link) {
			href = spans[i].Link
		}
		if href != "" {
			linkTitle := spans[i].LinkTitle
			j := i + 1
			for j < len(spans) && spans[j].Link == href && spans[j].LinkTitle == linkTitle {
				j++
			}
			if linkTitle != "" {
				escTitle := strings.NewReplacer("\r", "&#13;", "\n", "&#10;").Replace(html.EscapeString(linkTitle))
				fmt.Fprintf(sb, `<a href="%s" title="%s">`, html.EscapeString(href), escTitle)
			} else {
				fmt.Fprintf(sb, `<a href="%s">`, html.EscapeString(href))
			}
			for k := i; k < j; k++ {
				s.writeClipboardSpanInnerHTML(sb, spans[k], imageBudget)
			}
			sb.WriteString("</a>")
			i = j
			continue
		}
		s.writeClipboardSpanInnerHTML(sb, spans[i], imageBudget)
		i++
	}
}

func (s *Session) writeClipboardSpanInnerHTML(sb *strings.Builder, sp model.Span, imageBudget *int) {
	var body string
	if sp.ImageSrc != "" {
		alt := sp.Text
		if sp.ImageAltEmpty {
			alt = ""
		}
		body = s.clipboardImageTagHTML(sp.ImageSrc, alt, sp.ImageTitle, imageBudget)
	} else {
		body = strings.ReplaceAll(html.EscapeString(sp.Text), "\n", "<br>")
	}
	if sp.Marks&model.MarkCode != 0 {
		body = "<code>" + body + "</code>"
	}
	if sp.Marks&model.MarkMath != 0 {
		body = `<code class="math">` + body + `</code>`
	}
	if sp.Marks&model.MarkBold != 0 {
		body = "<strong>" + body + "</strong>"
	}
	if sp.Marks&model.MarkItalic != 0 {
		body = "<em>" + body + "</em>"
	}
	if sp.Marks&model.MarkUnderline != 0 {
		body = "<u>" + body + "</u>"
	}
	if sp.Marks&model.MarkStrike != 0 {
		body = "<s>" + body + "</s>"
	}
	if sp.Marks&model.MarkSuperscript != 0 {
		body = "<sup>" + body + "</sup>"
	}
	if sp.Marks&model.MarkSubscript != 0 {
		body = "<sub>" + body + "</sub>"
	}
	if sp.BGColor != "" {
		body = "<mark>" + body + "</mark>"
	}
	sb.WriteString(body)
}
