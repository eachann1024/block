package markdown

import (
	"bytes"
	"encoding/base64"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/model"

	"github.com/egoist/mygo/ui"
)

// ---- HTML Export ----

func exportHTML(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	pal, legacy := resolveExportPaper(prefs)
	width := resolveExportWidth(prefs)
	showTitle := resolveShowTitle(prefs)
	watermark := resolveWatermark(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	css := buildHTMLStylesheet(pal, legacy, width, title)

	var out strings.Builder
	out.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\">")
	out.WriteString(fmt.Sprintf("<meta name=\"viewport\" content=\"width=%d\">", width))
	out.WriteString("<title>")
	out.WriteString(escapeHTMLText(title))
	out.WriteString("</title><style>")
	out.WriteString(css)
	out.WriteString("</style></head><body><div class=\"page\"><article class=\"paper\">")

	if showTitle {
		out.WriteString(renderHTMLHeader(pal, legacy, title))
	}

	bodyBlocks := exportBodyBlocks(doc.Blocks, title, showTitle)
	var num listNumbering
	for i := 0; i < len(bodyBlocks); i++ {
		b := &bodyBlocks[i]
		if b.Kind.IsList() {
			start := i
			kind := b.Kind
			for i+1 < len(bodyBlocks) && bodyBlocks[i+1].Kind == kind {
				i++
			}
			renderHTMLList(&out, bodyBlocks[start:i+1], &num, pal)
			continue
		}
		num.next(b)
		renderHTMLBlock(&out, b, notePath, pal)
	}

	if watermark {
		now := time.Now().UTC().Format("2006.01.02")
		out.WriteString(fmt.Sprintf(`<footer class="doc-footer"><span>%s</span><span>%s</span></footer>`, watermarkLabel, now))
	} else {
		out.WriteString(`<div style="height:16px"></div>`)
	}
	out.WriteString("</article></div></body></html>\n")
	return out.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func htmlFontStack(fam string) string {
	switch fam {
	case fontSong:
		return `"Songti SC","Noto Serif SC",serif`
	case fontKai:
		return `"Kaiti SC","STKaiti",cursive,serif`
	case fontMono:
		return `"Menlo","JetBrains Mono",monospace`
	default:
		return `-apple-system,BlinkMacSystemFont,"PingFang SC","Helvetica Neue",sans-serif`
	}
}

func buildHTMLStylesheet(pal PaperThemeSpec, legacy bool, width int, title string) string {
	var inset, radius float32
	borderCSS := "0 0 0 transparent"
	shadowCSS := ""
	if pal.Card != nil {
		inset = pal.Card.Inset
		radius = pal.Card.Radius
		if pal.Card.HasBorder {
			borderCSS = fmt.Sprintf("inset 0 0 0 1px %s", uiColorCSS(pal.Card.Border))
		}
		if pal.Card.Shadow.A > 0 {
			shadowCSS = fmt.Sprintf(",0 8px 14px %s", uiColorCSS(pal.Card.Shadow))
		}
	}
	titleFam := pal.HeadingFamily
	if isASCII(title) {
		titleFam = pal.TitleFamily
	}
	titleWeight := 400
	if pal.TitleBold {
		titleWeight = 700
	}
	linkColor := uiColorCSS(pal.LinkFG())
	blockRadius := pal.BlockRadius()
	quoteCSS := fmt.Sprintf(
		"margin:0 0 16px;padding:12px 20px;border-left:4px solid %s;background:%s;border-radius:%.0fpx;color:%s",
		uiColorCSS(pal.QuoteBar), uiColorCSS(pal.QuoteBG), blockRadius, uiColorCSS(pal.QuoteFG),
	)
	if !legacy && pal.Theme == model.ExportThemePress {
		quoteCSS = fmt.Sprintf(
			"margin:0 0 16px;padding:12px 4px;border-top:1px solid %s;border-bottom:1px solid %s;color:%s",
			uiColorCSS(pal.QuoteBar), uiColorCSS(pal.QuoteBar), uiColorCSS(pal.QuoteFG),
		)
	}
	bulletStyle := ""
	if !legacy && pal.UsesSquareBullets() {
		bulletStyle = "ul{list-style-type:square}"
	}
	h2Rule := ""
	if !legacy && pal.Theme == model.ExportThemePress {
		h2Rule = fmt.Sprintf("h2{padding-bottom:6px;border-bottom:1px solid %s}", uiColorCSS(pal.Divider))
	} else if !legacy && pal.Theme == model.ExportThemePoster {
		h2Rule = fmt.Sprintf("h2{padding-left:12px;border-left:5px solid %s}", uiColorCSS(pal.Accent))
	} else if !legacy && pal.Theme == model.ExportThemeRiso {
		ink := ui.Color{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 80}
		h2Rule = fmt.Sprintf("h2 span.riso-h2{background:linear-gradient(transparent 55%%,%s 55%%,%s 90%%,transparent 90%%);box-decoration-break:clone}", uiColorCSS(ink), uiColorCSS(ink))
	}

	return fmt.Sprintf(`*{box-sizing:border-box}
html,body{margin:0;padding:0;background:%s;color:%s;font-family:%s}
.page{width:%dpx;max-width:100%%;margin:0 auto;padding:%.0fpx;background:%s}
.paper{background:%s;border-radius:%.0fpx;box-shadow:%s%s;padding:%.0fpx %.0fpx %.0fpx %.0fpx;font-size:%.1fpx;line-height:%.2f;color:%s;overflow-wrap:anywhere}
.doc-header{margin-bottom:28px}.doc-title{margin:0;font-family:%s;font-size:%.1fpx;line-height:1.3;font-weight:%d;color:%s}
.doc-footer{display:flex;justify-content:space-between;margin-top:28px;padding-top:16px;border-top:1px solid %s;font-size:14px;line-height:19.6px;color:%s}
h1,h2,h3,h4,h5,h6{margin:18px 0 8px;line-height:1.35;font-family:%s;color:%s;font-weight:700}
h1{font-size:%.1fpx}h2{font-size:%.1fpx}h3{font-size:%.1fpx}h4{font-size:%.1fpx}h5,h6{font-size:%.1fpx;font-weight:500}
%s
p{margin:0 0 10px}
ul,ol{margin:0 0 10px;padding-left:26px}%s ul.todo-list{list-style:none;padding-left:4px}li{margin:4px 0}
blockquote{%s}
.callout{margin:0 0 14px;padding:12px 16px;border-radius:8px;border-left:4px solid}
.callout-title{font-size:14px;font-weight:700;letter-spacing:.04em;margin-bottom:6px}
pre{margin:0 0 16px;padding:14px 18px;background:%s;border:1px solid %s;border-radius:%.0fpx;overflow-x:auto;font-family:%s;font-size:14.5px;line-height:1.6}
code{font-family:%s;font-size:.9em;background:%s;padding:1px 5px;border-radius:4px}
pre code{background:transparent;padding:0}
table{width:100%%;border-collapse:collapse;margin:0 0 14px}
th,td{border:1px solid %s;padding:8px 12px;text-align:left;vertical-align:top}
th{background:%s;color:%s;font-weight:600}
hr{border:0;border-top:1px solid %s;margin:18px 0}
a,table a,th a,td a{color:%s;text-decoration:underline}
img{max-width:100%%;height:auto;border-radius:6px}`,
		uiColorCSS(pal.Canvas), uiColorCSS(pal.Text), htmlFontStack(pal.BodyFamily),
		width, inset, uiColorCSS(pal.Canvas),
		uiColorCSS(pal.InnerCanvas), radius, borderCSS, shadowCSS,
		pal.PadTop, pal.PadRight, pal.PadBottom, pal.PadLeft, pal.BodySize, pal.Leading, uiColorCSS(pal.Text),
		htmlFontStack(titleFam), pal.TitleSize, titleWeight, uiColorCSS(pal.Title),
		uiColorCSS(pal.Divider), uiColorCSS(pal.Secondary),
		htmlFontStack(pal.HeadingFamily), uiColorCSS(pal.Title),
		pal.HeadSizes[0], pal.HeadSizes[1], pal.HeadSizes[2], pal.BodySize*1.12, pal.BodySize,
		h2Rule,
		bulletStyle,
		quoteCSS,
		uiColorCSS(pal.CodeBG), uiColorCSS(pal.CodeBorder), blockRadius, htmlFontStack(fontMono),
		htmlFontStack(fontMono), uiColorCSS(pal.CodeBG),
		uiColorCSS(pal.TableBorder),
		uiColorCSS(pal.TableHeaderBG()), uiColorCSS(pal.Title),
		uiColorCSS(pal.Divider),
		linkColor,
	)
}

func renderHTMLHeader(pal PaperThemeSpec, legacy bool, title string) string {
	escTitle := escapeHTMLText(title)
	if legacy {
		return fmt.Sprintf(`<header class="doc-header"><h1 class="doc-title">%s</h1><div style="height:2px;margin-top:14px;background:%s"></div></header>`,
			escTitle, uiColorCSS(pal.Accent))
	}
	switch pal.Mark {
	case TitleMarkBar:
		return fmt.Sprintf(`<header style="margin-bottom:34px"><h1 class="doc-title">%s</h1><div style="height:4px;width:48px;border-radius:2px;margin-top:18px;background:%s"></div></header>`,
			escTitle, uiColorCSS(pal.Accent))
	case TitleMarkWavy:
		return fmt.Sprintf(`<header style="margin-bottom:30px"><h1 class="doc-title">%s</h1><svg width="150" height="12" style="display:block;margin-top:8px"><path d="M0 6 Q6 0 12 6 T24 6 T36 6 T48 6 T60 6 T72 6 T84 6 T96 6 T108 6 T120 6 T132 6 T144 6 T156 6" fill="none" stroke="%s" stroke-width="2"/></svg></header>`,
			escTitle, uiColorCSS(pal.Accent))
	case TitleMarkDoubleRule:
		return fmt.Sprintf(`<header style="margin-bottom:34px"><h1 class="doc-title" style="text-align:center">%s</h1><div style="height:7px;margin-top:18px;border-top:3px solid %s;border-bottom:1px solid %s"></div></header>`,
			escTitle, uiColorCSS(pal.Divider), uiColorCSS(pal.Divider))
	case TitleMarkHighlight:
		ink := uiColorCSS(ui.Color{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 105})
		return fmt.Sprintf(`<header style="margin-bottom:36px"><h1 class="doc-title"><span style="background:linear-gradient(transparent 50%%,%s 50%%,%s 88%%,transparent 88%%);box-decoration-break:clone">%s</span></h1></header>`,
			ink, ink, escTitle)
	case TitleMarkCursor:
		return fmt.Sprintf(`<header style="margin-bottom:36px"><h1 class="doc-title">%s<span style="display:inline-block;vertical-align:middle;margin-left:10px;width:%.1fpx;height:%.1fpx;background:%s"></span></h1></header>`,
			escTitle, pal.TitleSize*0.55, pal.TitleSize*0.91, uiColorCSS(pal.Accent))
	case TitleMarkBand:
		top, right, _, left := pal.Margins()
		return fmt.Sprintf(`<header style="margin:-%.0fpx -%.0fpx 52px -%.0fpx;padding:%.0fpx %.0fpx 44px %.0fpx;background:%s"><h1 class="doc-title" style="color:%s">%s</h1></header>`,
			top, right, left, top, right, left, uiColorCSS(pal.Accent), uiColorCSS(pal.Accent2), escTitle)
	default: // TitleMarkRule
		return fmt.Sprintf(`<header style="margin-bottom:36px"><h1 class="doc-title">%s</h1><div style="height:2px;margin-top:20px;background:%s"></div></header>`,
			escTitle, uiColorCSS(pal.Title))
	}
}

func renderHTMLSpans(out *strings.Builder, spans []model.Span, pal PaperThemeSpec) {
	linkFG := pal.LinkFG()
	for _, sp := range spans {
		var openTags, closeTags []string
		fgColor, hasFG := resolveSpanColor(sp.FGColor, false, pal)
		if sp.Link != "" {
			linkStyle := uiColorCSS(linkFG)
			if hasFG {
				linkStyle = uiColorCSS(fgColor)
			}
			openTags = append(openTags, fmt.Sprintf(`<a href="%s" style="color:%s;text-decoration:underline">`, escapeAttrValue(sp.Link), linkStyle))
			closeTags = append(closeTags, "</a>")
		}
		var css strings.Builder
		if hasFG {
			css.WriteString("color:")
			css.WriteString(uiColorCSS(fgColor))
			css.WriteByte(';')
		} else if sp.Link != "" {
			css.WriteString("color:")
			css.WriteString(uiColorCSS(linkFG))
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

func renderHTMLList(out *strings.Builder, items []model.Block, num *listNumbering, pal PaperThemeSpec) {
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

func renderHTMLBlock(out *strings.Builder, b *model.Block, notePath string, pal PaperThemeSpec) {
	switch {
	case b.Kind.IsHeading():
		lvl := b.Kind.HeadingLevel()
		out.WriteString(fmt.Sprintf("<h%d>", lvl))
		if lvl == 2 && pal.Theme == model.ExportThemeRiso {
			out.WriteString(`<span class="riso-h2">`)
			renderHTMLSpans(out, b.Spans, pal)
			out.WriteString(`</span>`)
		} else {
			renderHTMLSpans(out, b.Spans, pal)
		}
		out.WriteString(fmt.Sprintf("</h%d>\n", lvl))

	case b.Kind == model.BlockDivider:
		out.WriteString("<hr>\n")

	case b.Kind == model.BlockQuote:
		out.WriteString("<blockquote><p>")
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString("</p></blockquote>\n")

	case b.Kind == model.BlockCallout:
		bg, border, accent, title := pal.CalloutColors(b.Callout)
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
		out.WriteString(fmt.Sprintf(`<figure style="margin:12px 0;text-align:%s"><img src="%s" alt="%s"`, align, escapeAttrValue(src), escapeAttrValue(b.ImageAlt)))
		if imgCSS.Len() > 0 {
			out.WriteString(fmt.Sprintf(` style="%s"`, imgCSS.String()))
		}
		out.WriteByte('>')
		if capText := strings.TrimSpace(b.ImageAlt); capText != "" {
			out.WriteString(fmt.Sprintf(`<figcaption style="margin-top:6px;font-size:13px;color:%s">%s</figcaption>`, uiColorCSS(pal.Secondary), escapeHTMLText(capText)))
		}
		out.WriteString("</figure>\n")

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
