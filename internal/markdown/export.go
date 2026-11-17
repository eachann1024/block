package markdown

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	defaultExportWidth = 816
	exportScale        = 2.0
	maxPDFPageHeightPt = 1152
)

// ExportDocument renders doc into the requested format bytes.
func ExportDocument(doc *model.Doc, notePath string, format model.ExportFormat, prefs model.ExportPreferences) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("cannot export nil document")
	}
	if format == "" {
		format = prefs.Format
	}
	if format == "" {
		format = model.ExportMarkdown
	}
	switch format {
	case model.ExportMarkdown:
		return []byte(exportMarkdown(doc, notePath, prefs)), nil
	case model.ExportHTML:
		return []byte(exportHTML(doc, notePath, prefs)), nil
	case model.ExportPNG:
		return exportPNG(doc, notePath, prefs)
	case model.ExportPDF:
		return exportPDF(doc, notePath, prefs)
	case model.ExportDOCX:
		return exportDOCX(doc, notePath, prefs)
	case model.ExportZIP:
		return exportZIP(doc, notePath, prefs)
	default:
		return nil, fmt.Errorf("unsupported export format: %q", format)
	}
}

// ExportToFile exports doc and atomically writes the resulting bytes to outPath.
func ExportToFile(doc *model.Doc, notePath, outPath string, format model.ExportFormat, prefs model.ExportPreferences) error {
	if strings.TrimSpace(outPath) == "" {
		return errors.New("output path cannot be empty")
	}
	data, err := ExportDocument(doc, notePath, format, prefs)
	if err != nil {
		return err
	}
	dir := filepath.Dir(outPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".block-export-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func resolveExportPalette(prefs model.ExportPreferences) theme.Palette {
	switch strings.ToLower(strings.TrimSpace(prefs.Theme)) {
	case "dark":
		return theme.OceanDark
	case "light":
		return theme.OceanLight
	default:
		return theme.Ocean()
	}
}

func resolveExportWidth(prefs model.ExportPreferences) int {
	if prefs.Width >= 360 && prefs.Width <= 2400 {
		return prefs.Width
	}
	return defaultExportWidth
}

func resolveDocTitle(doc *model.Doc, notePath string) string {
	if t := strings.TrimSpace(doc.Title()); t != "" {
		return t
	}
	if base := strings.TrimSpace(filepath.Base(notePath)); base != "" && base != "." && base != string(filepath.Separator) {
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem != "" {
			return stem
		}
	}
	return theme.Tr("未命名", "Untitled")
}

func uiColorHex(c ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func uiColorCSS(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float32(c.A)/255.0)
}

func mixUIColor(a, b ui.Color, t float32) ui.Color {
	lerp := func(x, y uint8) uint8 {
		v := float32(x) + (float32(y)-float32(x))*t
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	return ui.RGB(lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B))
}

func resolveSpanColor(raw string, bg bool, pal theme.Palette) (ui.Color, bool) {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" || v == "default" || v == "none" || v == "transparent" {
		return ui.Transparent, false
	}
	if bg {
		if c, ok := theme.ResolveNamedBG(v, pal.Dark); ok {
			return c, true
		}
	} else {
		if c, ok := theme.ResolveNamedFG(v, pal.Dark); ok {
			return c, true
		}
	}
	if v == "black" {
		return ui.RGB(0, 0, 0), true
	}
	if v == "white" {
		return ui.RGB(255, 255, 255), true
	}
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 {
			r, e1 := strconv.ParseUint(h[0:1]+h[0:1], 16, 8)
			g, e2 := strconv.ParseUint(h[1:2]+h[1:2], 16, 8)
			b, e3 := strconv.ParseUint(h[2:3]+h[2:3], 16, 8)
			if e1 == nil && e2 == nil && e3 == nil {
				return ui.RGB(uint8(r), uint8(g), uint8(b)), true
			}
		} else if len(h) == 6 || len(h) == 8 {
			r, e1 := strconv.ParseUint(h[0:2], 16, 8)
			g, e2 := strconv.ParseUint(h[2:4], 16, 8)
			b, e3 := strconv.ParseUint(h[4:6], 16, 8)
			a := uint64(255)
			var e4 error
			if len(h) == 8 {
				a, e4 = strconv.ParseUint(h[6:8], 16, 8)
			}
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				return ui.Color{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}, true
			}
		}
	}
	if strings.HasPrefix(v, "rgb(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[4:len(v)-1], ",")
		if len(parts) == 3 {
			r, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			if e1 == nil && e2 == nil && e3 == nil {
				return ui.RGB(clampU8(r), clampU8(g), clampU8(b)), true
			}
		}
	}
	if strings.HasPrefix(v, "rgba(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[5:len(v)-1], ",")
		if len(parts) == 4 {
			r, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			a, e4 := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				return ui.Color{R: clampU8(r), G: clampU8(g), B: clampU8(b), A: clampU8(int(a*255 + 0.5))}, true
			}
		}
	}
	return ui.Transparent, false
}

func clampU8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func calloutPalette(kind model.CalloutKind, pal theme.Palette) (bg, border, accent ui.Color, title string) {
	switch kind {
	case model.CalloutTip:
		accent = pal.Success
		title = "TIP"
	case model.CalloutImportant:
		if pal.Dark {
			accent = ui.Hex("#a78bfa")
		} else {
			accent = ui.Hex("#7c3aed")
		}
		title = "IMPORTANT"
	case model.CalloutWarning:
		accent = pal.Warning
		title = "WARNING"
	case model.CalloutCaution:
		accent = pal.Danger
		title = "CAUTION"
	default:
		accent = pal.Accent
		title = "NOTE"
	}
	bgMix := float32(0.07)
	if pal.Dark {
		bgMix = 0.14
	}
	bg = mixUIColor(pal.Paper, accent, bgMix)
	border = mixUIColor(pal.Paper, accent, 0.35)
	return bg, border, accent, title
}

func resolveLocalImagePath(src, notePath string) (string, bool) {
	s := strings.TrimSpace(src)
	if s == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:") {
		return "", false
	}
	if strings.HasPrefix(lower, "file://") {
		if u, err := url.Parse(s); err == nil && u.Path != "" {
			s = u.Path
		} else {
			s = strings.TrimPrefix(s[7:], "localhost")
		}
	} else if strings.Contains(s, "://") {
		return "", false
	}
	if decoded, err := url.PathUnescape(s); err == nil {
		s = decoded
	}
	p := filepath.FromSlash(s)
	if !filepath.IsAbs(p) {
		if notePath == "" {
			return "", false
		}
		p = filepath.Join(filepath.Dir(notePath), p)
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", false
	}
	return p, true
}

// ---- Markdown Export ----

func exportMarkdown(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	md := SerializeMarkdown(doc)
	if prefs.IncludeMeta {
		title := resolveDocTitle(doc, notePath)
		hasTopHeading := len(doc.Blocks) > 0 && doc.Blocks[0].Kind == model.BlockHeading1
		hasFM := len(doc.Blocks) > 0 && doc.Blocks[0].Kind == model.BlockCode && doc.Blocks[0].Language == "yaml-frontmatter"
		if title != "" && !hasTopHeading && !hasFM {
			return "# " + title + "\n\n" + md
		}
	}
	return md
}

// ---- HTML Export ----

func exportHTML(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	pal := resolveExportPalette(prefs)
	width := resolveExportWidth(prefs)
	title := resolveDocTitle(doc, notePath)
	css := buildHTMLStylesheet(pal, width)

	var out strings.Builder
	out.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\">")
	out.WriteString(fmt.Sprintf("<meta name=\"viewport\" content=\"width=%d\">", width))
	out.WriteString("<title>")
	out.WriteString(escapeHTMLText(title))
	out.WriteString("</title><style>")
	out.WriteString(css)
	out.WriteString("</style></head><body><div class=\"page\"><article class=\"paper\">")

	if prefs.IncludeMeta {
		out.WriteString("<header class=\"doc-header\"><h1 class=\"doc-title\">")
		out.WriteString(escapeHTMLText(title))
		out.WriteString("</h1><div class=\"doc-rule\"></div></header>")
	}

	var num listNumbering
	blocks := doc.Blocks
	for i := 0; i < len(blocks); i++ {
		b := &blocks[i]
		if b.Kind.IsList() {
			start := i
			kind := b.Kind
			for i+1 < len(blocks) && blocks[i+1].Kind == kind {
				i++
			}
			renderHTMLList(&out, blocks[start:i+1], &num, pal)
			continue
		}
		num.next(b)
		renderHTMLBlock(&out, b, notePath, pal)
	}

	if prefs.IncludeMeta {
		now := time.Now().UTC().Format("2006.01.02")
		out.WriteString(fmt.Sprintf(`<footer class="doc-footer"><span>Block</span><span>%s</span></footer>`, now))
	}
	out.WriteString("</article></div></body></html>\n")
	return out.String()
}

func buildHTMLStylesheet(pal theme.Palette, width int) string {
	return fmt.Sprintf(`*{box-sizing:border-box}
html,body{margin:0;padding:0;background:%s;color:%s;font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Helvetica Neue",sans-serif}
.page{max-width:%dpx;margin:0 auto;padding:32px 20px}
.paper{background:%s;border:1px solid %s;border-radius:14px;padding:48px 56px;font-size:16px;line-height:1.75;overflow-wrap:anywhere}
.doc-header{margin-bottom:28px}.doc-title{margin:0;font-size:32px;line-height:1.3;font-weight:700;color:%s}.doc-rule{height:2px;margin-top:14px;background:%s}
.doc-footer{display:flex;justify-content:space-between;margin-top:32px;padding-top:14px;border-top:1px solid %s;font-size:13px;color:%s}
h1,h2,h3,h4,h5,h6{margin:20px 0 10px;line-height:1.35;color:%s;font-weight:700}
h1{font-size:28px}h2{font-size:22px}h3{font-size:19px}h4{font-size:17px}h5,h6{font-size:16px}
p{margin:0 0 12px}
ul,ol{margin:0 0 12px;padding-left:26px}ul.todo-list{list-style:none;padding-left:6px}li{margin:4px 0}
blockquote{margin:0 0 14px;padding:10px 18px;border-left:4px solid %s;background:%s;border-radius:0 8px 8px 0;color:%s}
.callout{margin:0 0 14px;padding:12px 16px;border-radius:8px;border-left:4px solid;box-shadow:inset 0 0 0 1px rgba(128,128,128,.16)}
.callout-title{font-size:13px;font-weight:700;letter-spacing:.04em;margin-bottom:6px}
pre{margin:0 0 14px;padding:14px 16px;background:%s;border:1px solid %s;border-radius:8px;overflow-x:auto;font-family:"Menlo","JetBrains Mono",monospace;font-size:14px;line-height:1.55}
code{font-family:"Menlo","JetBrains Mono",monospace;font-size:.9em;background:%s;padding:1px 5px;border-radius:4px}
pre code{background:transparent;padding:0}
table{width:100%%;border-collapse:collapse;margin:0 0 14px}
th,td{border:1px solid %s;padding:8px 12px;text-align:left;vertical-align:top}
th{background:%s;font-weight:600}
hr{border:0;border-top:1px solid %s;margin:18px 0}
a{color:%s;text-decoration:underline}
img{max-width:100%%;height:auto;border-radius:6px}`,
		uiColorCSS(pal.Shell), uiColorCSS(pal.Text),
		width,
		uiColorCSS(pal.Paper), uiColorCSS(pal.Hairline),
		uiColorCSS(pal.Text), uiColorCSS(pal.Accent),
		uiColorCSS(pal.Hairline), uiColorCSS(pal.TextSecondary),
		uiColorCSS(pal.Text),
		uiColorCSS(pal.QuoteBar), uiColorCSS(pal.Hover), uiColorCSS(pal.TextSecondary),
		uiColorCSS(pal.CodeBG), uiColorCSS(pal.Hairline),
		uiColorCSS(pal.CodeBG),
		uiColorCSS(pal.Hairline),
		uiColorCSS(pal.Hover),
		uiColorCSS(pal.Hairline),
		uiColorCSS(pal.Accent),
	)
}

func renderHTMLSpans(out *strings.Builder, spans []model.Span, pal theme.Palette) {
	for _, sp := range spans {
		var openTags, closeTags []string
		if sp.Link != "" {
			openTags = append(openTags, fmt.Sprintf(`<a href="%s">`, escapeAttrValue(sp.Link)))
			closeTags = append(closeTags, "</a>")
		}
		var css strings.Builder
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			css.WriteString("color:")
			css.WriteString(uiColorCSS(c))
			css.WriteByte(';')
		}
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			css.WriteString("background-color:")
			css.WriteString(uiColorCSS(c))
			css.WriteString(";padding:1px 3px;border-radius:3px;")
		}
		if css.Len() > 0 {
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
		out.WriteString(strings.ReplaceAll(escapeHTMLText(sp.Text), "\n", "<br>"))
		for i := len(closeTags) - 1; i >= 0; i-- {
			out.WriteString(closeTags[i])
		}
	}
}

func renderHTMLList(out *strings.Builder, items []model.Block, num *listNumbering, pal theme.Palette) {
	if len(items) == 0 {
		return
	}
	first := items[0]
	openTag, closeTag := "<ul>", "</ul>"
	switch first.Kind {
	case model.BlockNumbered:
		if first.ListIndex > 1 {
			openTag = fmt.Sprintf(`<ol start="%d">`, first.ListIndex)
		} else {
			openTag = "<ol>"
		}
		closeTag = "</ol>"
	case model.BlockTodo:
		openTag = `<ul class="todo-list">`
	}
	out.WriteString(openTag)
	out.WriteByte('\n')
	for i := range items {
		it := &items[i]
		num.next(it)
		styleAttr := ""
		if it.Indent > 0 {
			styleAttr = fmt.Sprintf(` style="margin-left:%dpx"`, it.Indent*10)
		}
		if it.Kind == model.BlockTodo {
			chk := ""
			if it.Checked {
				chk = " checked"
			}
			out.WriteString(fmt.Sprintf(`<li%s><input type="checkbox" disabled%s> `, styleAttr, chk))
		} else {
			out.WriteString(fmt.Sprintf(`<li%s>`, styleAttr))
		}
		renderHTMLSpans(out, it.Spans, pal)
		out.WriteString("</li>\n")
	}
	out.WriteString(closeTag)
	out.WriteByte('\n')
}

func renderHTMLBlock(out *strings.Builder, b *model.Block, notePath string, pal theme.Palette) {
	switch {
	case b.Kind.IsHeading():
		lvl := b.Kind.HeadingLevel()
		out.WriteString(fmt.Sprintf("<h%d>", lvl))
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString(fmt.Sprintf("</h%d>\n", lvl))

	case b.Kind == model.BlockDivider:
		out.WriteString("<hr>\n")

	case b.Kind == model.BlockQuote:
		out.WriteString("<blockquote><p>")
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString("</p></blockquote>\n")

	case b.Kind == model.BlockCallout:
		bg, border, accent, title := calloutPalette(b.Callout, pal)
		out.WriteString(fmt.Sprintf(
			`<div class="callout callout-%s" style="background:%s;border-left-color:%s;box-shadow:inset 0 0 0 1px %s"><div class="callout-title" style="color:%s">%s</div><div>`,
			strings.ToLower(title), uiColorCSS(bg), uiColorCSS(accent), uiColorCSS(border), uiColorCSS(accent), title,
		))
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString("</div></div>\n")

	case b.Kind == model.BlockCode:
		cls := ""
		if b.Language != "" && b.Language != "yaml-frontmatter" {
			cls = fmt.Sprintf(` class="language-%s"`, escapeAttrValue(b.Language))
		}
		out.WriteString(fmt.Sprintf("<pre><code%s>%s</code></pre>\n", cls, escapeHTMLText(b.Code)))

	case b.Kind == model.BlockTable && b.Table != nil:
		t := b.Table
		out.WriteString("<table>\n")
		if len(t.Widths) > 0 {
			var total float32
			for _, w := range t.Widths {
				total += w
			}
			if total > 0 {
				out.WriteString("<colgroup>")
				for _, w := range t.Widths {
					out.WriteString(fmt.Sprintf(`<col style="width:%.2f%%">`, w/total*100))
				}
				out.WriteString("</colgroup>\n")
			}
		}
		for r, row := range t.Rows {
			out.WriteString("<tr>")
			tag := "td"
			if r == 0 {
				tag = "th"
			}
			for c, cell := range row {
				style := ""
				if c < len(t.Aligns) && t.Aligns[c] != model.ColAlignNone {
					style = fmt.Sprintf(` style="text-align:%s"`, t.Aligns[c])
				}
				out.WriteString("<" + tag + style + ">")
				renderHTMLSpans(out, cell.Spans, pal)
				out.WriteString("</" + tag + ">")
			}
			out.WriteString("</tr>\n")
		}
		out.WriteString("</table>\n")

	case b.Kind == model.BlockImage:
		src := b.ImageSrc
		if localPath, ok := resolveLocalImagePath(src, notePath); ok {
			if raw, err := os.ReadFile(localPath); err == nil {
				mime := detectImageMime(localPath, raw)
				src = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
			}
		}
		align := string(b.ImageAlign)
		if align == "" {
			align = "left"
		}
		var imgCSS strings.Builder
		if b.ImageWidth > 0 {
			imgCSS.WriteString(fmt.Sprintf("width:%s%%;", formatFloat(b.ImageWidth)))
		}
		if !b.ImageCrop.IsZero() {
			imgCSS.WriteString(fmt.Sprintf("clip-path:inset(%.2f%% %.2f%% %.2f%% %.2f%%);",
				b.ImageCrop.Top*100, b.ImageCrop.Right*100, b.ImageCrop.Bottom*100, b.ImageCrop.Left*100))
		}
		out.WriteString(fmt.Sprintf(`<p style="text-align:%s"><img src="%s" alt="%s"`, align, escapeAttrValue(src), escapeAttrValue(b.ImageAlt)))
		if imgCSS.Len() > 0 {
			out.WriteString(fmt.Sprintf(` style="%s"`, imgCSS.String()))
		}
		out.WriteString("></p>\n")

	case b.Kind == model.BlockRawHTML:
		out.WriteString(b.Code)
		out.WriteByte('\n')

	default:
		if len(b.Spans) == 0 {
			out.WriteString("<p>&nbsp;</p>\n")
			return
		}
		style := ""
		if b.Indent > 0 {
			style = fmt.Sprintf(` style="margin-left:%dpx"`, b.Indent*12)
		}
		out.WriteString("<p" + style + ">")
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString("</p>\n")
	}
}

func escapeHTMLText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func detectImageMime(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	if len(data) >= 8 && bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	return "image/png"
}

// ---- PNG & PDF Visual Rendering (2x Ocean Paper via mygo/ui) ----

type paintedBlock struct {
	kind       model.BlockKind
	x, y, w, h float32
	textX      float32
	textY      float32
	layout     *ui.TextLayout
	textColor  ui.Color
	bgColor    ui.Color
	borderCol  ui.Color
	accentCol  ui.Color
	labelLay   *ui.TextLayout
	markerLay  *ui.TextLayout
	markerCol  ui.Color
	checked    bool
	table      *paintedTable
	img        *paintedImage
}

type paintedTable struct {
	colWidths []float32
	rows      []paintedTableRow
}

type paintedTableRow struct {
	y, h  float32
	cells []paintedTableCell
}

type paintedTableCell struct {
	x, w      float32
	layout    *ui.TextLayout
	header    bool
	textColor ui.Color
	bgColor   ui.Color
}

type paintedImage struct {
	bitmap *ui.Bitmap
	src    ui.Rect
	dst    ui.Rect
}

type exportScene struct {
	width, height float32
	pal           theme.Palette
	titleLayout   *ui.TextLayout
	titleY        float32
	titleRuleY    float32
	blocks        []paintedBlock
	footerY       float32
	footerLeft    *ui.TextLayout
	footerRight   *ui.TextLayout
	showFooter    bool
}

func toUISpans(spans []model.Span, pal theme.Palette, baseSize float32, isCodeBlock bool) []ui.Span {
	if len(spans) == 0 {
		return []ui.Span{{Text: " "}}
	}
	out := make([]ui.Span, 0, len(spans))
	for _, sp := range spans {
		us := ui.Span{
			Text:          sp.Text,
			Size:          baseSize,
			Italic:        sp.Marks&model.MarkItalic != 0,
			Underline:     sp.Marks&model.MarkUnderline != 0,
			Strikethrough: sp.Marks&model.MarkStrike != 0,
		}
		if sp.Marks&model.MarkBold != 0 {
			us.Weight = 700
		}
		if sp.Marks&model.MarkCode != 0 || isCodeBlock {
			us.Font = "Menlo, monospace"
			if !isCodeBlock {
				us.Background = pal.CodeBG
				us.Radius = 4
				us.PaddingX = 3
			}
		}
		if sp.Link != "" {
			us.Color = pal.Accent
			us.Underline = true
		}
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			us.Color = c
		}
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			us.Background = c
			us.Radius = 3
		}
		out = append(out, us)
	}
	return out
}

func buildExportScene(doc *model.Doc, notePath string, prefs model.ExportPreferences) exportScene {
	pal := resolveExportPalette(prefs)
	pageW := float32(resolveExportWidth(prefs))
	const padX = float32(64)
	const padTop = float32(56)
	const padBottom = float32(56)
	contentW := pageW - 2*padX

	sc := exportScene{
		width: pageW,
		pal:   pal,
	}
	y := padTop

	if prefs.IncludeMeta {
		title := resolveDocTitle(doc, notePath)
		tFont := ui.Font{Size: 30, Weight: 700, LineHeight: 1.3}
		tLay := ui.ShapeRichTextWith([]ui.Span{{Text: title, Weight: 700, Size: 30}}, tFont, ui.TextShape{Width: contentW})
		_, th := tLay.Size()
		sc.titleLayout = tLay
		sc.titleY = y
		sc.titleRuleY = y + th + 14
		y = sc.titleRuleY + 24
	}

	var num listNumbering
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		ord := num.next(b)
		indent := float32(b.Indent) * 12
		if indent > contentW-120 {
			indent = contentW - 120
		}
		if indent < 0 {
			indent = 0
		}
		bx := padX + indent
		avail := contentW - indent

		switch {
		case b.Kind == model.BlockDivider:
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockDivider,
				x:         bx,
				y:         y,
				w:         avail,
				h:         20,
				borderCol: pal.Hairline,
			})
			y += 28

		case b.Kind == model.BlockImage:
			if pb, ok := buildPaintedImage(b, notePath, bx, y, avail, pal); ok {
				y += pb.h + 14
				sc.blocks = append(sc.blocks, pb)
			}

		case b.Kind == model.BlockTable && b.Table != nil:
			pb := buildPaintedTable(b.Table, bx, y, avail, pal)
			y += pb.h + 16
			sc.blocks = append(sc.blocks, pb)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			codeText := b.Code
			if codeText == "" {
				codeText = " "
			}
			font := ui.Font{Family: "Menlo, monospace", Size: 13.5, LineHeight: 1.55}
			padBoxX, padBoxY := float32(16), float32(12)
			var labelLay *ui.TextLayout
			labelH := float32(0)
			if b.Language != "" {
				lf := ui.Font{Size: 12, Weight: 600, LineHeight: 1.3}
				labelLay = ui.ShapeRichTextWith([]ui.Span{{Text: strings.ToUpper(b.Language), Size: 12, Weight: 600}}, lf, ui.TextShape{Width: avail - 2*padBoxX})
				_, lh := labelLay.Size()
				labelH = lh + 6
			}
			lay := ui.ShapeRichTextWith([]ui.Span{{Text: codeText, Size: 13.5, Font: "Menlo, monospace"}}, font, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + 2*padBoxY + labelH
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY + labelH,
				layout:    lay,
				textColor: pal.Text,
				bgColor:   pal.CodeBG,
				borderCol: pal.Hairline,
				labelLay:  labelLay,
			})
			y += bh + 14

		case b.Kind == model.BlockCallout:
			bg, border, accent, title := calloutPalette(b.Callout, pal)
			padBoxX, padBoxY := float32(18), float32(12)
			lf := ui.Font{Size: 13, Weight: 700, LineHeight: 1.3}
			labelLay := ui.ShapeRichTextWith([]ui.Span{{Text: title, Size: 13, Weight: 700}}, lf, ui.TextShape{Width: avail - 2*padBoxX})
			_, lh := labelLay.Size()
			bodyFont := ui.Font{Size: 15.5, LineHeight: 1.65}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, 15.5, false), bodyFont, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + lh + 6 + 2*padBoxY
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockCallout,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY + lh + 6,
				layout:    lay,
				textColor: pal.Text,
				bgColor:   bg,
				borderCol: border,
				accentCol: accent,
				labelLay:  labelLay,
			})
			y += bh + 14

		case b.Kind == model.BlockQuote:
			padBoxX, padBoxY := float32(18), float32(10)
			font := ui.Font{Size: 16, LineHeight: 1.7}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, 16, false), font, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + 2*padBoxY
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockQuote,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY,
				layout:    lay,
				textColor: pal.TextSecondary,
				bgColor:   pal.Hover,
				accentCol: pal.QuoteBar,
			})
			y += bh + 14

		case b.Kind.IsList():
			marker := "•"
			if b.Kind == model.BlockNumbered {
				if ord <= 0 {
					ord = 1
				}
				marker = fmt.Sprintf("%d.", ord)
			} else if b.Kind == model.BlockTodo {
				if b.Checked {
					marker = "☑"
				} else {
					marker = "☐"
				}
			}
			mFont := ui.Font{Size: 16, Weight: 600, LineHeight: 1.7}
			mLay := ui.ShapeRichTextWith([]ui.Span{{Text: marker, Size: 16, Weight: 600}}, mFont, ui.TextShape{Width: 64})
			mw, _ := mLay.Size()
			gutter := float32(26)
			if mw+10 > gutter {
				gutter = mw + 10
			}
			bodyFont := ui.Font{Size: 16, LineHeight: 1.7}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, 16, false), bodyFont, ui.TextShape{Width: avail - gutter})
			_, th := lay.Size()
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th,
				textX:     bx + gutter,
				textY:     y,
				layout:    lay,
				textColor: pal.Text,
				markerLay: mLay,
				markerCol: pal.Accent,
				checked:   b.Checked,
			})
			y += th + 6

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
			sizes := [6]float32{28, 22, 19, 17.5, 16.5, 16}
			sz := sizes[lvl-1]
			topGap := float32(10)
			if lvl == 1 {
				topGap = 16
			}
			y += topGap
			font := ui.Font{Size: sz, Weight: 700, LineHeight: 1.35}
			spans := toUISpans(b.Spans, pal, sz, false)
			for idx := range spans {
				spans[idx].Weight = 700
			}
			lay := ui.ShapeRichTextWith(spans, font, ui.TextShape{Width: avail})
			_, th := lay.Size()
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th,
				textX:     bx,
				textY:     y,
				layout:    lay,
				textColor: pal.Text,
			})
			y += th + 10

		default: // BlockParagraph
			if len(b.Spans) == 0 {
				y += 14
				continue
			}
			font := ui.Font{Size: 16, LineHeight: 1.75}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, 16, false), font, ui.TextShape{Width: avail})
			_, th := lay.Size()
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockParagraph,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th,
				textX:     bx,
				textY:     y,
				layout:    lay,
				textColor: pal.Text,
			})
			y += th + 12
		}
	}

	if prefs.IncludeMeta {
		sc.showFooter = true
		sc.footerY = y + 20
		ff := ui.Font{Size: 13, LineHeight: 1.4}
		sc.footerLeft = ui.ShapeRichTextWith([]ui.Span{{Text: "Block", Size: 13}}, ff, ui.TextShape{Width: contentW / 2})
		dateStr := time.Now().UTC().Format("2006.01.02")
		sc.footerRight = ui.ShapeRichTextWith([]ui.Span{{Text: dateStr, Size: 13}}, ff, ui.TextShape{Width: contentW / 2})
		y = sc.footerY + 34
	}

	sc.height = y + padBottom
	if sc.height < 160 {
		sc.height = 160
	}
	return sc
}

func buildPaintedImage(b *model.Block, notePath string, bx, by, avail float32, pal theme.Palette) (paintedBlock, bool) {
	if localPath, ok := resolveLocalImagePath(b.ImageSrc, notePath); ok {
		if raw, err := os.ReadFile(localPath); err == nil {
			if bm, err := ui.DecodeBitmap(raw); err == nil {
				bw, bh := bm.Size()
				if bw > 0 && bh > 0 {
					c := b.ImageCrop
					sx := c.Left * float32(bw)
					sy := c.Top * float32(bh)
					sw := (1 - c.Left - c.Right) * float32(bw)
					sh := (1 - c.Top - c.Bottom) * float32(bh)
					if sw <= 1 {
						sw = float32(bw)
						sx = 0
					}
					if sh <= 1 {
						sh = float32(bh)
						sy = 0
					}
					targetW := avail
					if b.ImageWidth > 0 && b.ImageWidth <= 100 {
						targetW = avail * b.ImageWidth / 100.0
					} else if sw < targetW {
						targetW = sw
					}
					targetH := targetW * (sh / sw)
					dx := bx
					switch b.ImageAlign {
					case model.ImageAlignCenter:
						dx = bx + (avail-targetW)/2
					case model.ImageAlignRight:
						dx = bx + avail - targetW
					}
					return paintedBlock{
						kind: model.BlockImage,
						x:    bx,
						y:    by,
						w:    avail,
						h:    targetH,
						img: &paintedImage{
							bitmap: bm,
							src:    ui.Rect{X: sx, Y: sy, W: sw, H: sh},
							dst:    ui.Rect{X: dx, Y: by, W: targetW, H: targetH},
						},
					}, true
				}
			}
		}
	}
	label := b.ImageAlt
	if label == "" {
		label = b.ImageSrc
	}
	if label == "" {
		label = "Image"
	}
	font := ui.Font{Size: 14, LineHeight: 1.5}
	lay := ui.ShapeRichTextWith([]ui.Span{{Text: "[Image] " + label, Size: 14}}, font, ui.TextShape{Width: avail - 24})
	_, th := lay.Size()
	bh := th + 20
	return paintedBlock{
		kind:      model.BlockImage,
		x:         bx,
		y:         by,
		w:         avail,
		h:         bh,
		textX:     bx + 12,
		textY:     by + 10,
		layout:    lay,
		textColor: pal.TextSecondary,
		bgColor:   pal.CodeBG,
		borderCol: pal.Hairline,
	}, true
}

func buildPaintedTable(t *model.TableData, bx, by, avail float32, pal theme.Palette) paintedBlock {
	cols := 0
	if len(t.Rows) > 0 {
		cols = len(t.Rows[0])
	}
	if len(t.Aligns) > cols {
		cols = len(t.Aligns)
	}
	if cols == 0 {
		return paintedBlock{kind: model.BlockTable, x: bx, y: by, w: avail, h: 20}
	}
	colWidths := make([]float32, cols)
	if len(t.Widths) == cols {
		var sum float32
		for _, w := range t.Widths {
			sum += w
		}
		if sum > 0 {
			for i, w := range t.Widths {
				colWidths[i] = w / sum * avail
			}
		}
	}
	if colWidths[0] == 0 {
		each := avail / float32(cols)
		for i := range colWidths {
			colWidths[i] = each
		}
	}

	const padCellX = float32(10)
	const padCellY = float32(7)
	pt := &paintedTable{colWidths: colWidths}
	curY := by
	for r, row := range t.Rows {
		isHeader := r == 0
		rowH := float32(28)
		pRow := paintedTableRow{y: curY}
		curX := bx
		for c := 0; c < cols; c++ {
			cw := colWidths[c]
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			weight := 400
			fg := pal.Text
			bg := pal.Paper
			if isHeader {
				weight = 700
				bg = pal.Hover
			}
			font := ui.Font{Size: 14, Weight: weight, LineHeight: 1.45}
			uSpans := toUISpans(cellSpans, pal, 14, false)
			if isHeader {
				for idx := range uSpans {
					uSpans[idx].Weight = 700
				}
			}
			align := ui.Start
			if c < len(t.Aligns) {
				switch t.Aligns[c] {
				case model.ColAlignCenter:
					align = ui.Center
				case model.ColAlignRight:
					align = ui.End
				}
			}
			lay := ui.ShapeRichTextWith(uSpans, font, ui.TextShape{Width: maxFloat32(cw-2*padCellX, 20), Align: align})
			_, th := lay.Size()
			if th+2*padCellY > rowH {
				rowH = th + 2*padCellY
			}
			pRow.cells = append(pRow.cells, paintedTableCell{
				x:         curX,
				w:         cw,
				layout:    lay,
				header:    isHeader,
				textColor: fg,
				bgColor:   bg,
			})
			curX += cw
		}
		pRow.h = rowH
		pt.rows = append(pt.rows, pRow)
		curY += rowH
	}

	return paintedBlock{
		kind:      model.BlockTable,
		x:         bx,
		y:         by,
		w:         avail,
		h:         curY - by,
		borderCol: pal.Hairline,
		table:     pt,
	}
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func renderDocRGBA(doc *model.Doc, notePath string, prefs model.ExportPreferences) (*image.RGBA, exportScene) {
	sc := buildExportScene(doc, notePath, prefs)
	w := int(sc.width)
	h := int(sc.height + 0.5)
	if h > 16000 {
		h = 16000
	}
	rgba := ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(sc.width, float32(h)).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, sc.pal.Paper, 0)
			if sc.titleLayout != nil {
				p.TextLayout(sc.titleLayout, 64, sc.titleY, sc.pal.Text)
				p.Fill(ui.Rect{X: 64, Y: sc.titleRuleY, W: sc.width - 128, H: 2}, sc.pal.Accent, 1)
			}
			for _, b := range sc.blocks {
				paintSceneBlock(p, b, sc.pal)
			}
			if sc.showFooter {
				p.Line(64, sc.footerY, sc.width-64, sc.footerY, 1, sc.pal.Hairline)
				p.TextLayout(sc.footerLeft, 64, sc.footerY+10, sc.pal.TextSecondary)
				rw, _ := sc.footerRight.Size()
				p.TextLayout(sc.footerRight, sc.width-64-rw, sc.footerY+10, sc.pal.TextSecondary)
			}
		})
	}, w, h, exportScale)
	return rgba, sc
}

func paintSceneBlock(p *ui.Painter, b paintedBlock, pal theme.Palette) {
	switch {
	case b.kind == model.BlockDivider:
		midY := b.y + b.h/2
		p.Line(b.x, midY, b.x+b.w, midY, 1, b.borderCol)

	case b.kind == model.BlockImage:
		if b.img != nil {
			p.ImageRegion(b.img.bitmap, b.img.src, b.img.dst, ui.ImageOptions{})
		} else if b.layout != nil {
			p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, b.bgColor, 8)
			p.Stroke(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, b.borderCol, 8, 1)
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockTable && b.table != nil:
		for _, row := range b.table.rows {
			for _, cell := range row.cells {
				rect := ui.Rect{X: cell.x, Y: row.y, W: cell.w, H: row.h}
				p.Fill(rect, cell.bgColor, 0)
				p.Stroke(rect, b.borderCol, 0, 1)
				if cell.layout != nil {
					p.TextLayout(cell.layout, cell.x+10, row.y+7, cell.textColor)
				}
			}
		}

	case b.kind == model.BlockCode || b.kind == model.BlockRawHTML:
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		p.Fill(rect, b.bgColor, 8)
		p.Stroke(rect, b.borderCol, 8, 1)
		if b.labelLay != nil {
			p.TextLayout(b.labelLay, b.textX, b.y+12, pal.TextSecondary)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockCallout:
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		p.Fill(rect, b.bgColor, 8)
		p.Stroke(rect, b.borderCol, 8, 1)
		p.Fill(ui.Rect{X: b.x, Y: b.y, W: 4, H: b.h}, b.accentCol, 2)
		if b.labelLay != nil {
			p.TextLayout(b.labelLay, b.textX, b.y+12, b.accentCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockQuote:
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		p.Fill(rect, b.bgColor, 6)
		p.Fill(ui.Rect{X: b.x, Y: b.y, W: 4, H: b.h}, b.accentCol, 2)
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind.IsList():
		if b.markerLay != nil {
			p.TextLayout(b.markerLay, b.x, b.y, b.markerCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	default:
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}
	}
}

func exportPNG(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	rgba, _ := renderDocRGBA(doc, notePath, prefs)
	if rgba == nil || rgba.Bounds().Empty() {
		return nil, errors.New("png render returned empty image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func exportPDF(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	rgba, _ := renderDocRGBA(doc, notePath, prefs)
	if rgba == nil || rgba.Bounds().Empty() {
		return nil, errors.New("pdf render returned empty image")
	}
	bounds := rgba.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	maxSliceH := int(maxPDFPageHeightPt * exportScale)

	type pdfPage struct {
		w, h    int
		ptW     int
		ptH     int
		zStream []byte
	}
	var pages []pdfPage
	for y := 0; y < imgH; y += maxSliceH {
		sliceH := imgH - y
		if sliceH > maxSliceH {
			sliceH = maxSliceH
		}
		rgb := make([]byte, imgW*sliceH*3)
		pos := 0
		for row := 0; row < sliceH; row++ {
			rowOff := (y + row) * rgba.Stride
			for col := 0; col < imgW; col++ {
				px := rgba.Pix[rowOff+col*4 : rowOff+col*4+4]
				// Composite premultiplied RGBA over white.
				gap := uint8(255 - px[3])
				rgb[pos] = addSaturate(px[0], gap)
				rgb[pos+1] = addSaturate(px[1], gap)
				rgb[pos+2] = addSaturate(px[2], gap)
				pos += 3
			}
		}
		var zBuf bytes.Buffer
		zw := zlib.NewWriter(&zBuf)
		if _, err := zw.Write(rgb); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		ptW := int(float32(imgW)/exportScale + 0.5)
		ptH := int(float32(sliceH)/exportScale + 0.5)
		if ptW < 1 {
			ptW = 1
		}
		if ptH < 1 {
			ptH = 1
		}
		pages = append(pages, pdfPage{
			w: imgW, h: sliceH, ptW: ptW, ptH: ptH, zStream: zBuf.Bytes(),
		})
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	var offsets []int
	writeObj := func(num int, body []byte) {
		offsets = append(offsets, out.Len())
		out.WriteString(fmt.Sprintf("%d 0 obj\n", num))
		out.Write(body)
		out.WriteString("\nendobj\n")
	}

	// Object 1: Catalog, Object 2: Pages
	// Each page i uses 3 objects: Page (3 + 3*i), Image XObject (4 + 3*i), Contents (5 + 3*i)
	var kids strings.Builder
	for i := range pages {
		if i > 0 {
			kids.WriteByte(' ')
		}
		kids.WriteString(fmt.Sprintf("%d 0 R", 3+i*3))
	}
	writeObj(1, []byte("<< /Type /Catalog /Pages 2 0 R >>"))
	writeObj(2, []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(pages))))

	for i, pg := range pages {
		pageObj := 3 + i*3
		imgObj := 4 + i*3
		contentObj := 5 + i*3

		pageDict := fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
			pg.ptW, pg.ptH, imgObj, contentObj,
		)
		writeObj(pageObj, []byte(pageDict))

		var imgBuf bytes.Buffer
		imgBuf.WriteString(fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n",
			pg.w, pg.h, len(pg.zStream),
		))
		imgBuf.Write(pg.zStream)
		imgBuf.WriteString("\nendstream")
		writeObj(imgObj, imgBuf.Bytes())

		cmd := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im0 Do Q", pg.ptW, pg.ptH)
		contentDict := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmd), cmd)
		writeObj(contentObj, []byte(contentDict))
	}

	xrefOffset := out.Len()
	totalObjs := 2 + len(pages)*3 + 1
	out.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", totalObjs))
	for _, off := range offsets {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	out.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", totalObjs, xrefOffset))
	return out.Bytes(), nil
}

func addSaturate(a, b uint8) uint8 {
	s := int(a) + int(b)
	if s > 255 {
		return 255
	}
	return uint8(s)
}

// ---- DOCX Export (OOXML zip) ----

const (
	docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
)

type docxAssets struct {
	rels      []string
	media     []docxMedia
	nextRelID int
}

type docxMedia struct {
	path string
	data []byte
}

func (a *docxAssets) addLink(href string) string {
	id := fmt.Sprintf("rId%d", a.nextRelID)
	a.nextRelID++
	a.rels = append(a.rels, fmt.Sprintf(
		`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`,
		id, escapeAttrValue(href),
	))
	return id
}

func (a *docxAssets) addImage(data []byte, ext string, widthPx, heightPx float32, alt string) string {
	num := len(a.media) + 1
	relTarget := fmt.Sprintf("media/image%d.%s", num, ext)
	id := fmt.Sprintf("rId%d", a.nextRelID)
	a.nextRelID++
	a.rels = append(a.rels, fmt.Sprintf(
		`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="%s"/>`,
		id, relTarget,
	))
	a.media = append(a.media, docxMedia{path: "word/" + relTarget, data: data})
	cx := int64(widthPx * 9525)
	cy := int64(heightPx * 9525)
	return fmt.Sprintf(
		`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Image %d" descr="%s"/><wp:cNvGraphicFramePr/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="image%d.%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, num, num, escapeAttrValue(alt), num, ext, id, cx, cy,
	)
}

func docxHex(c ui.Color) string {
	return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
}

func docxTwips(px float32) int {
	v := int(px*15 + 0.5)
	if v < 0 {
		return 0
	}
	return v
}

func exportDOCX(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	pal := resolveExportPalette(prefs)
	pageW := float32(resolveExportWidth(prefs))
	const marginPx = float32(72)
	contentW := pageW - 2*marginPx

	assets := &docxAssets{nextRelID: 3}
	var body strings.Builder

	if prefs.IncludeMeta {
		title := resolveDocTitle(doc, notePath)
		body.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:pStyle w:val="Heading1"/><w:pBdr><w:bottom w:val="single" w:sz="12" w:space="6" w:color="%s"/></w:pBdr><w:spacing w:before="0" w:after="240"/></w:pPr><w:r><w:rPr><w:b/><w:bCs/><w:color w:val="%s"/><w:sz w:val="48"/><w:szCs w:val="48"/></w:rPr>%s</w:r></w:p>`,
			docxHex(pal.Accent), docxHex(pal.Text), docxTextXML(title),
		))
	}

	var num listNumbering
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		ord := num.next(b)
		indentPx := float32(b.Indent) * 12
		avail := contentW - indentPx
		if avail < 120 {
			avail = 120
		}

		switch {
		case b.Kind == model.BlockDivider:
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="%s"/></w:pBdr><w:spacing w:before="120" w:after="160"/></w:pPr></w:p>`,
				docxHex(pal.Hairline),
			))

		case b.Kind == model.BlockImage:
			if lp, ok := resolveLocalImagePath(b.ImageSrc, notePath); ok {
				if raw, err := os.ReadFile(lp); err == nil {
					if cfg, formatName, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
						ext := "png"
						switch formatName {
						case "jpeg":
							ext = "jpg"
						case "gif":
							ext = "gif"
						}
						targetW := avail
						if b.ImageWidth > 0 && b.ImageWidth <= 100 {
							targetW = avail * b.ImageWidth / 100
						} else if float32(cfg.Width) < targetW {
							targetW = float32(cfg.Width)
						}
						targetH := targetW * float32(cfg.Height) / float32(cfg.Width)
						run := assets.addImage(raw, ext, targetW, targetH, b.ImageAlt)
						jc := "left"
						if b.ImageAlign == model.ImageAlignCenter {
							jc = "center"
						} else if b.ImageAlign == model.ImageAlignRight {
							jc = "right"
						}
						body.WriteString(fmt.Sprintf(
							`<w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="0" w:after="160"/></w:pPr>%s</w:p>`,
							jc, run,
						))
						continue
					}
				}
			}
			alt := b.ImageAlt
			if alt == "" {
				alt = b.ImageSrc
			}
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="120"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
				docxHex(pal.TextSecondary), docxTextXML("[Image] "+alt),
			))

		case b.Kind == model.BlockTable && b.Table != nil:
			body.WriteString(renderDOCXTable(b.Table, avail, indentPx, pal, assets))
			body.WriteString(`<w:p><w:pPr><w:spacing w:before="0" w:after="100" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			var inner strings.Builder
			if b.Language != "" {
				inner.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:spacing w:before="0" w:after="60"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="20"/></w:rPr>%s</w:r></w:p>`,
					docxHex(pal.TextSecondary), docxTextXML(strings.ToUpper(b.Language)),
				))
			}
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Menlo" w:hAnsi="Menlo" w:eastAsia="PingFang SC" w:cs="Menlo"/><w:sz w:val="21"/><w:szCs w:val="21"/><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
				docxHex(pal.Text), docxTextXML(b.Code),
			))
			borders := fmt.Sprintf(
				`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
				docxHex(pal.Hairline), docxHex(pal.Hairline), docxHex(pal.Hairline), docxHex(pal.Hairline),
			)
			body.WriteString(renderDOCXBox(inner.String(), avail, indentPx, pal.CodeBG, borders))

		case b.Kind == model.BlockCallout:
			bg, border, accent, title := calloutPalette(b.Callout, pal)
			var inner strings.Builder
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="80"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr>%s</w:r></w:p>`,
				docxHex(accent), docxTextXML(title),
			))
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="360" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				renderDOCXRuns(b.Spans, pal, false, 24, pal.Text, assets),
			))
			borders := fmt.Sprintf(
				`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
				docxHex(border), docxHex(accent), docxHex(border), docxHex(border),
			)
			body.WriteString(renderDOCXBox(inner.String(), avail, indentPx, bg, borders))

		case b.Kind == model.BlockQuote:
			inner := fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="360" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				renderDOCXRuns(b.Spans, pal, false, 24, pal.TextSecondary, assets),
			)
			borders := fmt.Sprintf(`<w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/>`, docxHex(pal.QuoteBar))
			body.WriteString(renderDOCXBox(inner, avail, indentPx, pal.Hover, borders))

		case b.Kind.IsList():
			marker := "•"
			if b.Kind == model.BlockNumbered {
				if ord <= 0 {
					ord = 1
				}
				marker = fmt.Sprintf("%d.", ord)
			} else if b.Kind == model.BlockTodo {
				if b.Checked {
					marker = "☑"
				} else {
					marker = "☐"
				}
			}
			gutter := float32(24)
			runs := renderDOCXRuns(b.Spans, pal, false, 24, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="%d"/></w:tabs><w:ind w:left="%d" w:hanging="%d"/><w:spacing w:before="0" w:after="80" w:line="360" w:lineRule="auto"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/></w:rPr>%s</w:r><w:r><w:tab/></w:r>%s</w:p>`,
				docxTwips(indentPx+gutter), docxTwips(indentPx+gutter), docxTwips(gutter),
				docxHex(pal.Accent), docxTextXML(marker), runs,
			))

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
			halfPts := [6]int{42, 33, 28, 26, 24, 24}[lvl-1]
			runs := renderDOCXRuns(b.Spans, pal, true, halfPts, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:pStyle w:val="Heading%d"/><w:spacing w:before="200" w:after="120"/></w:pPr>%s</w:p>`,
				lvl, runs,
			))

		default:
			runs := renderDOCXRuns(b.Spans, pal, false, 24, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:before="0" w:after="140" w:line="360" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				docxTwips(indentPx), runs,
			))
		}
	}

	if prefs.IncludeMeta {
		dateStr := time.Now().UTC().Format("2006.01.02")
		body.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:top w:val="single" w:sz="6" w:space="6" w:color="%s"/></w:pBdr><w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs><w:spacing w:before="280" w:after="0"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="20"/></w:rPr>%s<w:tab/>%s</w:r></w:p>`,
			docxHex(pal.Hairline), docxTwips(contentW), docxHex(pal.TextSecondary), docxTextXML("Block"), docxTextXML(dateStr),
		))
	}

	body.WriteString(fmt.Sprintf(
		`<w:sectPr><w:pgSz w:w="%d" w:h="16838"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr>`,
		docxTwips(pageW), docxTwips(marginPx), docxTwips(marginPx), docxTwips(marginPx), docxTwips(marginPx),
	))

	docXML := fmt.Sprintf(`%s<w:document %s><w:background w:color="%s"/><w:body>%s</w:body></w:document>`,
		xmlDecl, docxNS, docxHex(pal.Paper), body.String(),
	)

	stylesXML := buildDOCXStyles(pal)
	settingsXML := fmt.Sprintf(
		`%s<w:settings %s><w:displayBackgroundShape/><w:autoHyphenation w:val="0"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`,
		xmlDecl, docxNS,
	)
	contentTypes := xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="png" ContentType="image/png"/>` +
		`<Default Extension="jpg" ContentType="image/jpeg"/>` +
		`<Default Extension="gif" ContentType="image/gif"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
		`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
		`</Types>`
	rootRels := xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	docRels := fmt.Sprintf(
		`%s<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>%s</Relationships>`,
		xmlDecl, strings.Join(assets.rels, ""),
	)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	writeZipEntry := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	entries := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", []byte(contentTypes)},
		{"_rels/.rels", []byte(rootRels)},
		{"word/_rels/document.xml.rels", []byte(docRels)},
		{"word/styles.xml", []byte(stylesXML)},
		{"word/settings.xml", []byte(settingsXML)},
		{"word/document.xml", []byte(docXML)},
	}
	for _, e := range entries {
		if err := writeZipEntry(e.name, e.data); err != nil {
			return nil, err
		}
	}
	for _, m := range assets.media {
		if err := writeZipEntry(m.path, m.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return zipBuf.Bytes(), nil
}

func buildDOCXStyles(pal theme.Palette) string {
	var out strings.Builder
	out.WriteString(fmt.Sprintf(
		`%s<w:styles %s><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="PingFang SC" w:hAnsi="PingFang SC" w:eastAsia="PingFang SC" w:cs="PingFang SC"/><w:color w:val="%s"/><w:sz w:val="24"/><w:szCs w:val="24"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:widowControl w:val="0"/><w:spacing w:before="0" w:after="0" w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`,
		xmlDecl, docxNS, docxHex(pal.Text),
	))
	halfPts := [6]int{42, 33, 28, 26, 24, 24}
	for lvl := 1; lvl <= 6; lvl++ {
		out.WriteString(fmt.Sprintf(
			`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr><w:b/><w:bCs/><w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
			lvl, lvl, lvl-1, docxHex(pal.Text), halfPts[lvl-1], halfPts[lvl-1],
		))
	}
	out.WriteString("</w:styles>")
	return out.String()
}

func docxTextXML(s string) string {
	var out strings.Builder
	lines := strings.Split(s, "\n")
	for li, line := range lines {
		if li > 0 {
			out.WriteString("<w:br/>")
		}
		parts := strings.Split(line, "\t")
		for pi, piece := range parts {
			if pi > 0 {
				out.WriteString("<w:tab/>")
			}
			if piece != "" {
				out.WriteString(`<w:t xml:space="preserve">`)
				out.WriteString(escapeXMLText(piece))
				out.WriteString("</w:t>")
			}
		}
	}
	return out.String()
}

func escapeXMLText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func renderDOCXRuns(spans []model.Span, pal theme.Palette, baseBold bool, halfPt int, baseFG ui.Color, assets *docxAssets) string {
	if len(spans) == 0 {
		return ""
	}
	var out strings.Builder
	for _, sp := range spans {
		var rpr strings.Builder
		if sp.Marks&model.MarkCode != 0 {
			rpr.WriteString(`<w:rFonts w:ascii="Menlo" w:hAnsi="Menlo" w:eastAsia="PingFang SC" w:cs="Menlo"/>`)
		}
		if baseBold || sp.Marks&model.MarkBold != 0 {
			rpr.WriteString("<w:b/><w:bCs/>")
		}
		if sp.Marks&model.MarkItalic != 0 {
			rpr.WriteString("<w:i/><w:iCs/>")
		}
		if sp.Marks&model.MarkStrike != 0 {
			rpr.WriteString("<w:strike/>")
		}
		if sp.Marks&model.MarkUnderline != 0 || sp.Link != "" {
			rpr.WriteString(`<w:u w:val="single"/>`)
		}
		fg := baseFG
		if sp.Link != "" {
			fg = pal.Accent
		}
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			fg = c
		}
		rpr.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, docxHex(fg)))
		rpr.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, halfPt, halfPt))
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(c)))
		} else if sp.Marks&model.MarkCode != 0 {
			rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(pal.CodeBG)))
		}
		run := fmt.Sprintf(`<w:r><w:rPr>%s</w:rPr>%s</w:r>`, rpr.String(), docxTextXML(sp.Text))
		if sp.Link != "" {
			rID := assets.addLink(sp.Link)
			out.WriteString(fmt.Sprintf(`<w:hyperlink r:id="%s">%s</w:hyperlink>`, rID, run))
		} else {
			out.WriteString(run)
		}
	}
	return out.String()
}

func renderDOCXBox(content string, widthPx, indentPx float32, bg ui.Color, borders string) string {
	wTwips := docxTwips(widthPx)
	iTwips := docxTwips(indentPx)
	return fmt.Sprintf(
		`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/><w:tblBorders>%s</w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="150" w:type="dxa"/><w:bottom w:w="150" w:type="dxa"/><w:left w:w="210" w:type="dxa"/><w:right w:w="210" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr>%s</w:tc></w:tr></w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="120" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
		wTwips, iTwips, borders, wTwips, wTwips, docxHex(bg), content,
	)
}

func renderDOCXTable(t *model.TableData, widthPx, indentPx float32, pal theme.Palette, assets *docxAssets) string {
	cols := 0
	if len(t.Rows) > 0 {
		cols = len(t.Rows[0])
	}
	if cols == 0 {
		return ""
	}
	colWidths := make([]int, cols)
	totalTwips := docxTwips(widthPx)
	if len(t.Widths) == cols {
		var sum float32
		for _, w := range t.Widths {
			sum += w
		}
		if sum > 0 {
			for i, w := range t.Widths {
				colWidths[i] = int(float32(totalTwips)*(w/sum) + 0.5)
			}
		}
	}
	if colWidths[0] == 0 {
		each := totalTwips / cols
		for i := range colWidths {
			colWidths[i] = each
		}
	}

	var grid strings.Builder
	for _, cw := range colWidths {
		grid.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, cw))
	}
	bc := docxHex(pal.Hairline)
	borders := fmt.Sprintf(
		`<w:top w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="%s"/>`,
		bc, bc, bc, bc, bc, bc,
	)
	var out strings.Builder
	out.WriteString(fmt.Sprintf(
		`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/><w:tblBorders>%s</w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="90" w:type="dxa"/><w:bottom w:w="90" w:type="dxa"/><w:left w:w="120" w:type="dxa"/><w:right w:w="120" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>%s</w:tblGrid>`,
		totalTwips, docxTwips(indentPx), borders, grid.String(),
	))
	for r, row := range t.Rows {
		isHeader := r == 0
		out.WriteString("<w:tr>")
		for c := 0; c < cols; c++ {
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			shd := ""
			if isHeader {
				shd = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(pal.Hover))
			}
			jc := "left"
			if c < len(t.Aligns) && t.Aligns[c] != model.ColAlignNone {
				jc = string(t.Aligns[c])
			}
			runs := renderDOCXRuns(cellSpans, pal, isHeader, 22, pal.Text, assets)
			out.WriteString(fmt.Sprintf(
				`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s<w:vAlign w:val="top"/></w:tcPr><w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr>%s</w:p></w:tc>`,
				colWidths[c], shd, jc, runs,
			))
		}
		out.WriteString("</w:tr>")
	}
	out.WriteString("</w:tbl>")
	return out.String()
}

// ---- ZIP Export (Markdown + Local Referenced Images) ----

func exportZIP(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	cloned := doc.Clone()
	usedNames := make(map[string]int)
	pathToZip := make(map[string]string)

	type zipAsset struct {
		zipPath string
		data    []byte
	}
	var assets []zipAsset

	for i := range cloned.Blocks {
		b := &cloned.Blocks[i]
		if b.Kind != model.BlockImage {
			continue
		}
		localPath, ok := resolveLocalImagePath(b.ImageSrc, notePath)
		if !ok {
			continue
		}
		zipPath, seen := pathToZip[localPath]
		if !seen {
			raw, err := os.ReadFile(localPath)
			if err != nil {
				continue
			}
			base := filepath.Base(localPath)
			if base == "" || base == "." || base == string(filepath.Separator) {
				base = "image.png"
			}
			count := usedNames[base]
			usedNames[base] = count + 1
			name := base
			if count > 0 {
				ext := filepath.Ext(base)
				stem := strings.TrimSuffix(base, ext)
				name = fmt.Sprintf("%s-%d%s", stem, count, ext)
			}
			zipPath = "assets/" + name
			pathToZip[localPath] = zipPath
			assets = append(assets, zipAsset{zipPath: zipPath, data: raw})
		}
		if b.ImageSrc != zipPath {
			b.ImageSrc = zipPath
			b.Dirty = true
			b.CleanSource = ""
		}
	}

	mdContent := exportMarkdown(cloned, notePath, prefs)
	mdName := "note.md"
	if base := strings.TrimSpace(filepath.Base(notePath)); base != "" && base != "." && base != string(filepath.Separator) {
		if !strings.HasSuffix(strings.ToLower(base), ".md") {
			base += ".md"
		}
		mdName = base
	} else if t := strings.TrimSpace(doc.Title()); t != "" {
		safe := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`/\:*?"<>|`, r) || r < 0x20 {
				return '-'
			}
			return r
		}, t)
		if safe != "" {
			mdName = safe + ".md"
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mw, err := zw.Create(mdName)
	if err != nil {
		return nil, err
	}
	if _, err := mw.Write([]byte(mdContent)); err != nil {
		return nil, err
	}
	for _, a := range assets {
		aw, err := zw.Create(a.zipPath)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(a.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
