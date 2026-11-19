package editor

import (
	"bytes"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"block/internal/model"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/webp"
)

const (
	maxClipboardImageBytes  = 20 << 20   // 20 MiB file size cap
	maxClipboardImageSide   = 8192       // max width or height in pixels
	maxClipboardImagePixels = 32_000_000 // ~32 MP cap to prevent decompression bombs / UI stalls
)

var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

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
		reps = append(reps, transfer.Bytes(transfer.HTML, []byte(sb.String())))
	}
	svc.WriteClipboardData(transfer.New(transfer.NewItem(reps...)))
}

func (s *Session) readLocalImageAsPNG(src string) []byte {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" || IsSupportedVideoPath(trimmed) {
		return nil
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:") {
		return nil
	}
	full := s.resolveMediaPath(trimmed)
	if full == "" {
		return nil
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxClipboardImageBytes {
		return nil
	}
	f, err := os.Open(full)
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxClipboardImageBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxClipboardImageBytes {
		return nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > maxClipboardImageSide || cfg.Height > maxClipboardImageSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxClipboardImagePixels {
		return nil
	}
	if format == "png" {
		return raw
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil
	}
	return out.Bytes()
}

func (s *Session) clipboardHTMLForDoc(doc *model.Doc) string {
	if doc == nil || len(doc.Blocks) == 0 {
		return ""
	}
	var sb strings.Builder
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
		s.writeClipboardBlockHTML(&sb, b)
		i++
	}
	return sb.String()
}

func (s *Session) writeClipboardBlockHTML(sb *strings.Builder, b *model.Block) {
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
		src := strings.TrimSpace(b.ImageSrc)
		if src != "" {
			lower := strings.ToLower(src)
			if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "data:") {
				if resolved := s.resolveMediaPath(src); resolved != "" {
					if abs, err := filepath.Abs(resolved); err == nil {
						src = localFileURL(abs)
					}
				}
			}
		}
		if IsPreviewVideo(b.ImageSrc) || IsSupportedVideoPath(b.ImageSrc) {
			fmt.Fprintf(sb, `<p><video controls src="%s">%s</video></p>`, html.EscapeString(src), html.EscapeString(b.ImageAlt))
			return
		}
		fmt.Fprintf(sb, `<p><img src="%s" alt="%s"></p>`, html.EscapeString(src), html.EscapeString(b.ImageAlt))

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
		if sp.BGColor != "" {
			text = "<mark>" + text + "</mark>"
		}
		if sp.Link != "" {
			text = fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(sp.Link), text)
		}
		sb.WriteString(text)
	}
}
