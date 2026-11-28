package markdown

import (
	"encoding/base64"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"unicode"

	"block/internal/media"
	"block/internal/model"
)

// ConvertClipboardHTML converts rich semantic HTML from web pages, Feishu Docs, Notion,
// Google Docs, Word, or Apple Notes into clean Block Markdown.
// It returns ("", false) when:
//   - plainFallback already contains explicit authored Markdown syntax,
//   - htmlRaw originates from a syntax-highlighted code editor / terminal dump without semantic HTML, or
//   - htmlRaw carries no semantic structure or inline formatting beyond plain text.
func ConvertClipboardHTML(htmlRaw, plainFallback string) (string, bool) {
	if len(htmlRaw) > media.MaxPasteBytes || len(plainFallback) > media.MaxPasteBytes || strings.TrimSpace(htmlRaw) == "" {
		return "", false
	}
	fragment := extractHTMLClipboardFragment(htmlRaw)
	if strings.TrimSpace(fragment) == "" {
		return "", false
	}
	if !containsMathHTML(fragment) && looksLikeAuthoredMarkdown(plainFallback) {
		return "", false
	}
	if isCodeEditorHTMLDump(fragment) || !hasSemanticHTMLFeatures(fragment) {
		return "", false
	}

	conv := &htmlPasteConverter{}
	conv.parse(fragment)
	if conv.overBudget {
		return "", false
	}
	out := strings.TrimSpace(NormalizePasteText(conv.result()))
	if out == "" {
		return "", false
	}
	return out, true
}

func containsMathHTML(fragment string) bool {
	lower := strings.ToLower(fragment)
	if !strings.Contains(lower, "math") {
		return false
	}
	for i := 0; i < len(fragment); {
		lt := strings.IndexByte(fragment[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		name, attrs, closed, _, consumed, ok := parseClipboardTag(fragment, i)
		if !ok {
			i++
			continue
		}
		if !closed && (name == "code" || name == "span") && hasCSSClass(attrVal(attrs, "class"), "math") {
			return true
		}
		i += consumed
	}
	return false
}

func looksLikeAuthoredMarkdown(plain string) bool {
	if plain == "" {
		return false
	}
	s := strings.ReplaceAll(strings.ReplaceAll(plain, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	pipeRows := 0
	hasTableSep := false
	for _, rawLine := range lines {
		line := strings.TrimLeft(rawLine, " \t")
		if _, _, _, _, _, ok := parseFenceAnyIndent(rawLine); ok {
			return true
		}
		if _, _, ok := classifyHeading(rawLine); ok {
			return true
		}
		if strings.HasPrefix(line, "> ") || strings.HasPrefix(line, ">[!") {
			return true
		}
		if strings.HasPrefix(line, "- [ ] ") || strings.HasPrefix(line, "- [x] ") || strings.HasPrefix(line, "- [X] ") ||
			strings.HasPrefix(line, "* [ ] ") || strings.HasPrefix(line, "* [x] ") {
			return true
		}
		if strings.HasPrefix(line, "|") && strings.HasSuffix(strings.TrimSpace(line), "|") {
			pipeRows++
			if isPipeDelimiterLine(strings.TrimSpace(line)) {
				hasTableSep = true
			}
		}
		if hasExplicitInlineMarkdown(line) {
			return true
		}
	}
	return pipeRows >= 2 && hasTableSep
}

func hasExplicitInlineMarkdown(line string) bool {
	if strings.Contains(line, "**") || strings.Contains(line, "~~") {
		spans := ParseInline(line)
		for _, sp := range spans {
			if sp.Marks&(model.MarkBold|model.MarkStrike) != 0 {
				return true
			}
		}
	}
	if strings.ContainsRune(line, '`') {
		spans := ParseInline(line)
		for _, sp := range spans {
			if sp.Marks&model.MarkCode != 0 {
				return true
			}
		}
	}
	if strings.Contains(line, "](") {
		spans := ParseInline(line)
		for _, sp := range spans {
			if sp.Link != "" {
				return true
			}
		}
	}
	return false
}

func extractHTMLClipboardFragment(raw string) string {
	s := strings.TrimPrefix(raw, "\uFEFF")
	lower := strings.ToLower(s)
	if startIdx := strings.Index(lower, "<!--startfragment-->"); startIdx >= 0 {
		s = s[startIdx+len("<!--startfragment-->"):]
		lower = strings.ToLower(s)
		if endIdx := strings.Index(lower, "<!--endfragment-->"); endIdx >= 0 {
			s = s[:endIdx]
		}
		return stripIgnoredHTMLRegions(s)
	}
	// Strip Windows CF_HTML header lines if present without fragment markers.
	if strings.HasPrefix(s, "Version:") {
		if lt := strings.IndexByte(s, '<'); lt > 0 {
			s = s[lt:]
		}
	}
	lower = strings.ToLower(s)
	if bodyIdx := strings.Index(lower, "<body"); bodyIdx >= 0 {
		if gt := strings.IndexByte(s[bodyIdx:], '>'); gt >= 0 {
			s = s[bodyIdx+gt+1:]
			lower = strings.ToLower(s)
			if endBody := strings.LastIndex(lower, "</body>"); endBody >= 0 {
				s = s[:endBody]
			}
		}
	}
	return stripIgnoredHTMLRegions(s)
}

func stripIgnoredHTMLRegions(s string) string {
	if !strings.ContainsRune(s, '<') {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			out.WriteString(s[i:])
			break
		}
		if lt > 0 {
			out.WriteString(s[i : i+lt])
			i += lt
		}
		if strings.HasPrefix(s[i:], "<!--") {
			if end := strings.Index(s[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
			break
		}
		name, _, closed, empty, consumed, ok := parseClipboardTag(s, i)
		if ok && !closed && !empty {
			switch name {
			case "head", "style", "script", "noscript", "title", "svg", "template":
				if rel := indexCloseTagFold(s[i+consumed:], name); rel >= 0 {
					afterClose := i + consumed + rel
					if gt := strings.IndexByte(s[afterClose:], '>'); gt >= 0 {
						i = afterClose + gt + 1
						continue
					}
				}
				return out.String()
			}
		}
		if ok && (name == "meta" || name == "link") {
			i += consumed
			continue
		}
		if ok {
			out.WriteString(s[i : i+consumed])
			i += consumed
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// Limit the bytes a malformed tag can scan before it is treated as text.
func parseClipboardTag(src string, start int) (string, [][2]string, bool, bool, int, bool) {
	end := min(len(src), start+16*1024)
	last := end - 1
	name, attrs, closed, empty, consumed, _, _, ok := parseTagRawBounds(src[:end], start, last, last, last, true)
	return name, attrs, closed, empty, consumed, ok
}

func indexCloseTagFold(s, lowerName string) int {
	n := len(lowerName)
	pos := 0
	for pos < len(s) {
		idx := strings.Index(s[pos:], "</")
		if idx < 0 {
			return -1
		}
		abs := pos + idx
		if abs+2+n <= len(s) && asciiEqualFoldLower(s[abs+2:abs+2+n], lowerName) {
			return abs
		}
		pos = abs + 2
	}
	return -1
}

func asciiEqualFoldLower(a, lowerB string) bool {
	if len(a) != len(lowerB) {
		return false
	}
	for i := 0; i < len(a); i++ {
		c := a[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lowerB[i] {
			return false
		}
	}
	return true
}

func isCodeEditorHTMLDump(fragment string) bool {
	lower := strings.ToLower(fragment)
	if strings.Contains(lower, "vscode-editor-data") || strings.Contains(lower, "monaco-tokenized-source") {
		return true
	}
	// Monospace pre-formatted wrapper with zero semantic document tags (VS Code, Cursor, JetBrains, Terminal).
	hasMonospaceWrapper := (strings.Contains(lower, "monospace") || strings.Contains(lower, "menlo") || strings.Contains(lower, "consolas")) &&
		(strings.Contains(lower, "white-space: pre") || strings.Contains(lower, "white-space:pre"))
	if !hasMonospaceWrapper {
		return false
	}
	for _, sem := range []string{"<h1", "<h2", "<h3", "<h4", "<h5", "<h6", "<ul", "<ol", "<li", "<table", "<blockquote", "<a ", "<strong", "<b ", "<b>", "<em", "<mark", `class="math"`} {
		if strings.Contains(lower, sem) {
			return false
		}
	}
	return true
}

func hasSemanticHTMLFeatures(fragment string) bool {
	i := 0
	for i < len(fragment) {
		lt := strings.IndexByte(fragment[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		name, attrs, closed, _, consumed, ok := parseClipboardTag(fragment, i)
		if !ok {
			i++
			continue
		}
		if !closed {
			switch name {
			case "h1", "h2", "h3", "h4", "h5", "h6",
				"ul", "ol", "li", "table", "blockquote", "aside", "pre", "hr",
				"strong", "em", "i", "u", "ins", "del", "s", "strike", "code", "kbd", "samp", "mark", "sup", "sub":
				return true
			case "b":
				fw := strings.ToLower(strings.TrimSpace(cssProp(attrs, "font-weight")))
				if fw != "normal" && fw != "400" {
					return true
				}
			case "a":
				if media.WebLink(attrVal(attrs, "href")) != "" {
					return true
				}
			case "img":
				if isPasteableRemoteOrLocalImage(attrVal(attrs, "src")) {
					return true
				}
			case "input":
				if strings.EqualFold(strings.TrimSpace(attrVal(attrs, "type")), "checkbox") {
					return true
				}
			default:
				if hasCSSClass(attrVal(attrs, "class"), "math") {
					return true
				}
				if isCSSBold(cssProp(attrs, "font-weight")) ||
					strings.EqualFold(cssProp(attrs, "font-style"), "italic") {
					return true
				}
				if td := strings.ToLower(cssProp(attrs, "text-decoration")); strings.Contains(td, "underline") || strings.Contains(td, "line-through") {
					return true
				}
				if isHighlightBGColor(cssProp(attrs, "background-color")) {
					return true
				}
			}
		}
		i += consumed
	}
	return false
}

func isPasteableRemoteOrLocalImage(src string) bool {
	s := strings.TrimSpace(src)
	link := strings.ToLower(media.WebLink(s))
	if strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "http://") {
		return true
	}
	return isSafeRasterDataImageURI(s)
}

func isSafeRasterDataImageURI(src string) bool {
	if len(src) == 0 || len(src) > media.MaxPasteBytes {
		return false
	}
	comma := strings.IndexByte(src, ',')
	if comma <= 0 {
		return false
	}
	header := strings.ToLower(strings.TrimSpace(src[:comma]))
	switch header {
	case "data:image/png;base64", "data:image/jpeg;base64", "data:image/jpg;base64", "data:image/webp;base64", "data:image/gif;base64":
	default:
		return false
	}
	payload := strings.TrimSpace(src[comma+1:])
	if payload == "" || base64.StdEncoding.DecodedLen(len(payload)) > media.MaxImageBytes {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 || len(raw) > media.MaxImageBytes {
		return false
	}
	_, _, _, err = media.DecodeRaster(raw)
	return err == nil
}

type htmlInlineState struct {
	tag       string
	bold      *bool
	italic    *bool
	underline *bool
	strike    *bool
	code      *bool
	math      *bool
	sup       *bool
	sub       *bool
	bgColor   *string
	link      *string
	linkTitle *string
}

type htmlListCtx struct {
	ordered bool
	nextNum int
}

type htmlLIState struct {
	flushed     bool
	todo        bool
	checked     bool
	extraIndent int
}

type htmlTableCell struct {
	spans    []model.Span
	pipeText string
	isHeader bool
}

type htmlTableCtx struct {
	rows        [][]htmlTableCell
	curRow      []htmlTableCell
	rowSpans    []int
	inCell      bool
	cellSpans   []model.Span
	cellHeader  bool
	cellColspan int
	cellRowspan int
	inThead     bool
	nestedDepth int
}

type htmlPasteConverter struct {
	blocks       []string
	curSpans     []model.Span
	inlineStk    []htmlInlineState
	listStk      []htmlListCtx
	liStk        []htmlLIState
	quoteDepth   int
	headingLvl   int
	inPara       int
	paraAlignStk []model.ImageAlign
	soloImgWidth float32
	soloImgAlign model.ImageAlign
	inPre        bool
	preLang      string
	preBuf       strings.Builder
	table        *htmlTableCtx
	outputBytes  int
	overBudget   bool
}

func (c *htmlPasteConverter) activeStyle() (marks model.Mark, link, linkTitle, bg string) {
	var bold, italic, under, strike, code, mathMark, sup, sub bool
	for _, f := range c.inlineStk {
		if f.bold != nil {
			bold = *f.bold
		}
		if f.italic != nil {
			italic = *f.italic
		}
		if f.underline != nil {
			under = *f.underline
		}
		if f.strike != nil {
			strike = *f.strike
		}
		if f.code != nil {
			code = *f.code
		}
		if f.math != nil {
			mathMark = *f.math
		}
		if f.sup != nil {
			sup = *f.sup
		}
		if f.sub != nil {
			sub = *f.sub
		}
		if f.bgColor != nil {
			bg = *f.bgColor
		}
		if f.link != nil {
			link = *f.link
			if f.linkTitle != nil {
				linkTitle = *f.linkTitle
			} else {
				linkTitle = ""
			}
		}
	}
	if bold && c.headingLvl == 0 {
		marks |= model.MarkBold
	}
	if italic {
		marks |= model.MarkItalic
	}
	if under {
		marks |= model.MarkUnderline
	}
	if strike {
		marks |= model.MarkStrike
	}
	if mathMark {
		marks |= model.MarkMath
	}
	if code {
		marks |= model.MarkCode
	}
	if sup {
		marks |= model.MarkSuperscript
	}
	if sub {
		marks |= model.MarkSubscript
	}
	return marks, link, linkTitle, bg
}

func (c *htmlPasteConverter) inLiteralInline() bool {
	for _, f := range c.inlineStk {
		if (f.math != nil && *f.math) || (f.code != nil && *f.code) {
			return true
		}
	}
	return false
}

func (c *htmlPasteConverter) targetSpans() *[]model.Span {
	if c.table != nil && c.table.inCell {
		return &c.table.cellSpans
	}
	return &c.curSpans
}

func (c *htmlPasteConverter) pushText(raw string) {
	if raw == "" {
		return
	}
	decoded := html.UnescapeString(raw)
	if c.inPre {
		c.preBuf.WriteString(decoded)
		return
	}
	if c.inLiteralInline() {
		var lit strings.Builder
		lit.Grow(len(decoded))
		for _, r := range decoded {
			if r == '\u00A0' {
				r = ' '
			}
			if r == '\r' {
				continue
			}
			if r != '\n' && r != '\t' && unicode.IsControl(r) {
				continue
			}
			lit.WriteRune(r)
		}
		if lit.Len() > 0 {
			spansPtr := c.targetSpans()
			marks, link, linkTitle, bg := c.activeStyle()
			*spansPtr = append(*spansPtr, model.Span{
				Text:      lit.String(),
				Marks:     marks,
				Link:      link,
				LinkTitle: linkTitle,
				BGColor:   bg,
			})
		}
		return
	}
	marks, link, linkTitle, bg := c.activeStyle()
	if (marks != 0 || link != "" || bg != "") && !strings.ContainsAny(decoded, "\r\n\t") && strings.Trim(strings.ReplaceAll(decoded, "\u00A0", " "), " ") == "" {
		spansPtr := c.targetSpans()
		*spansPtr = append(*spansPtr, model.Span{
			Text:      strings.ReplaceAll(decoded, "\u00A0", " "),
			Marks:     marks,
			Link:      link,
			LinkTitle: linkTitle,
			BGColor:   bg,
		})
		return
	}
	// Collapse HTML source formatting newlines/tabs into single spaces while keeping non-breaking spaces clean.
	var norm strings.Builder
	norm.Grow(len(decoded))
	lastSpace := false
	spansPtr := c.targetSpans()
	if len(*spansPtr) == 0 {
		lastSpace = true
	} else {
		prevSpan := (*spansPtr)[len(*spansPtr)-1]
		if prevSpan.ImageSrc == "" && (strings.HasSuffix(prevSpan.Text, " ") || strings.HasSuffix(prevSpan.Text, "\n")) {
			lastSpace = true
		}
	}
	for _, r := range decoded {
		if r == '\u00A0' {
			r = ' '
		}
		if r == '\r' || r == '\n' || r == '\t' || r == ' ' {
			if !lastSpace {
				norm.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		norm.WriteRune(r)
		lastSpace = false
	}
	txt := norm.String()
	if txt == "" {
		return
	}
	*spansPtr = append(*spansPtr, model.Span{
		Text:      txt,
		Marks:     marks,
		Link:      link,
		LinkTitle: linkTitle,
		BGColor:   bg,
	})
}

func (c *htmlPasteConverter) pushBreak() {
	if c.inPre {
		c.preBuf.WriteByte('\n')
		return
	}
	spansPtr := c.targetSpans()
	if len(*spansPtr) == 0 {
		return
	}
	marks, link, linkTitle, bg := c.activeStyle()
	if !c.inLiteralInline() {
		last := &(*spansPtr)[len(*spansPtr)-1]
		if last.ImageSrc == "" {
			last.Text = strings.TrimRight(last.Text, " ")
		}
	}
	*spansPtr = append(*spansPtr, model.Span{
		Text:      "\n",
		Marks:     marks,
		Link:      link,
		LinkTitle: linkTitle,
		BGColor:   bg,
	})
}

func trimSpanEdges(spans []model.Span) []model.Span {
	norm := coalesceSpans(spans)
	if len(norm) == 0 {
		return nil
	}
	if norm[0].Marks == 0 && norm[0].Link == "" && norm[0].ImageSrc == "" && norm[0].BGColor == "" {
		norm[0].Text = strings.TrimLeft(norm[0].Text, " \t\n")
	}
	last := &norm[len(norm)-1]
	if last.Marks == 0 && last.Link == "" && last.ImageSrc == "" && last.BGColor == "" {
		last.Text = strings.TrimRight(last.Text, " \t\n")
	}
	return coalesceSpans(norm)
}

func (c *htmlPasteConverter) emitBlockLine(blockMD string) {
	blockMD = strings.TrimRight(blockMD, " \t\r\n")
	if strings.TrimSpace(blockMD) == "" {
		return
	}
	if c.quoteDepth > 0 {
		prefix := strings.Repeat("> ", c.quoteDepth)
		bare := strings.TrimRight(prefix, " ")
		lines := strings.Split(blockMD, "\n")
		for i, l := range lines {
			if strings.TrimSpace(l) == "" {
				lines[i] = bare
			} else {
				lines[i] = prefix + l
			}
		}
		blockMD = strings.Join(lines, "\n")
	}
	c.appendOutput(blockMD)
}

func (c *htmlPasteConverter) currentParaAlign() model.ImageAlign {
	for i := len(c.paraAlignStk) - 1; i >= 0; i-- {
		if c.paraAlignStk[i] != "" {
			return c.paraAlignStk[i]
		}
	}
	return model.ImageAlignLeft
}

func formatStyledBlockImageHTML(src, alt, title string, width float32, align model.ImageAlign) string {
	var img strings.Builder
	img.WriteString(`<img src="`)
	img.WriteString(escapeAttrValue(src))
	img.WriteString(`" alt="`)
	img.WriteString(escapeAttrValue(alt))
	img.WriteByte('"')
	if title != "" {
		img.WriteString(` title="`)
		img.WriteString(escapeAttrValue(title))
		img.WriteByte('"')
	}
	if width > 0 {
		img.WriteString(` width="`)
		img.WriteString(formatFloat(width))
		img.WriteString(`%"`)
	}
	img.WriteByte('>')
	if align != "" && align != model.ImageAlignLeft {
		return fmt.Sprintf(`<p align="%s">%s</p>`, string(align), img.String())
	}
	return img.String()
}

func (c *htmlPasteConverter) flushCurrentBlock() {
	spans := trimSpanEdges(c.curSpans)
	soloW := c.soloImgWidth
	soloAlign := c.soloImgAlign
	c.curSpans = nil
	c.soloImgWidth = 0
	c.soloImgAlign = ""
	hasActiveLI := len(c.listStk) > 0 && len(c.liStk) > 0
	var li *htmlLIState
	if hasActiveLI {
		li = &c.liStk[len(c.liStk)-1]
	}
	if len(spans) == 0 && !(hasActiveLI && li.todo && !li.flushed) {
		return
	}

	if c.headingLvl == 0 && !hasActiveLI && c.quoteDepth == 0 && len(spans) == 1 && spans[0].ImageSrc != "" &&
		spans[0].Marks == 0 && spans[0].Link == "" && spans[0].FGColor == "" && spans[0].BGColor == "" {
		if soloAlign == "" {
			soloAlign = c.currentParaAlign()
		}
		if soloW > 0 || (soloAlign != "" && soloAlign != model.ImageAlignLeft) {
			alt := spans[0].Text
			if spans[0].ImageAltEmpty {
				alt = ""
			}
			c.emitBlockLine(formatStyledBlockImageHTML(spans[0].ImageSrc, alt, spans[0].ImageTitle, soloW, soloAlign))
			return
		}
	}

	if c.headingLvl >= 1 && c.headingLvl <= 6 {
		// Collapse multi-line headings into a single clean ATX heading line (preserving image alt newlines).
		for i := range spans {
			if spans[i].ImageSrc == "" {
				spans[i].Text = strings.ReplaceAll(spans[i].Text, "\n", " ")
			}
		}
		text := strings.TrimSpace(SerializeInline(spans))
		if text != "" {
			c.emitBlockLine(strings.Repeat("#", c.headingLvl) + " " + escapeHeadingInline(text))
		}
		return
	}

	if hasActiveLI {
		indentCols := max((len(c.listStk)-1)*2+li.extraIndent*2, 0)
		indent := strings.Repeat(" ", indentCols)
		top := &c.listStk[len(c.listStk)-1]
		if !li.flushed {
			marker := "- "
			if li.todo {
				if li.checked {
					marker = "- [x] "
				} else {
					marker = "- [ ] "
				}
			} else if top.ordered {
				n := top.nextNum
				if n <= 0 {
					n = 1
				}
				marker = fmt.Sprintf("%d. ", n)
				top.nextNum = n + 1
			}
			lineGroups := splitSpansOnNewline(spans)
			if len(lineGroups) == 0 {
				c.emitBlockLine(strings.TrimRight(indent+marker, " "))
			} else {
				var lines []string
				contPad := indent + strings.Repeat(" ", len(marker))
				for idx, grp := range lineGroups {
					inlineMD := strings.TrimSpace(SerializeInline(grp))
					if idx == 0 {
						lines = append(lines, indent+marker+inlineMD)
					} else if inlineMD != "" {
						lines = append(lines, contPad+inlineMD)
					}
				}
				c.emitBlockLine(strings.Join(lines, "\n"))
			}
			li.flushed = true
			li.todo = false
			li.checked = false
			return
		}
		// Subsequent <p>/<div> inside an already-flushed <li> becomes an indented continuation line.
		contPad := indent + "  "
		for _, grp := range splitSpansOnNewline(spans) {
			if inlineMD := strings.TrimSpace(SerializeInline(grp)); inlineMD != "" {
				c.emitBlockLine(contPad + inlineMD)
			}
		}
		return
	}

	// Normal paragraph: split internal <br> lines into clean blocks or keep inline.
	lineGroups := splitSpansOnNewline(spans)
	if len(lineGroups) == 1 {
		if inlineMD := strings.TrimSpace(SerializeInline(lineGroups[0])); inlineMD != "" {
			c.emitBlockLine(inlineMD)
		}
		return
	}
	var paraLines []string
	for _, grp := range lineGroups {
		if inlineMD := strings.TrimSpace(SerializeInline(grp)); inlineMD != "" {
			paraLines = append(paraLines, inlineMD)
		}
	}
	if len(paraLines) > 0 {
		c.emitBlockLine(strings.Join(paraLines, "  \n"))
	}
}

func splitSpansOnNewline(spans []model.Span) [][]model.Span {
	if len(spans) == 0 {
		return nil
	}
	var groups [][]model.Span
	var cur []model.Span
	for _, sp := range spans {
		styled := sp.Marks != 0 || sp.Link != "" || sp.ImageSrc != "" || sp.FGColor != "" || sp.BGColor != ""
		if styled || !strings.ContainsRune(sp.Text, '\n') {
			cur = append(cur, sp)
			continue
		}
		parts := strings.Split(sp.Text, "\n")
		for i, p := range parts {
			if p != "" {
				cp := sp
				cp.Text = p
				cur = append(cur, cp)
			}
			if i+1 < len(parts) {
				groups = append(groups, coalesceSpans(cur))
				cur = nil
			}
		}
	}
	if len(cur) > 0 || len(groups) > 0 {
		groups = append(groups, coalesceSpans(cur))
	}
	return groups
}

func extractCodeLangFromAttrs(attrs [][2]string) string {
	for _, key := range []string{"data-language", "data-lang", "lang"} {
		if v := strings.TrimSpace(attrVal(attrs, key)); v != "" {
			return firstTokenClean(v)
		}
	}
	cls := attrVal(attrs, "class")
	for _, field := range strings.Fields(cls) {
		lower := strings.ToLower(field)
		for _, pfx := range []string{"language-", "lang-", "brush:"} {
			if strings.HasPrefix(lower, pfx) && len(field) > len(pfx) {
				return firstTokenClean(field[len(pfx):])
			}
		}
	}
	return ""
}

func firstTokenClean(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '_' || c == '#' {
			b.WriteByte(c)
		} else {
			break
		}
	}
	return strings.ToLower(b.String())
}

func (c *htmlPasteConverter) pushInlineTag(name string, attrs [][2]string) {
	st := htmlInlineState{tag: name}
	boolPtr := func(v bool) *bool { return &v }
	strPtr := func(v string) *string { return &v }

	switch name {
	case "strong":
		st.bold = boolPtr(true)
	case "b":
		fw := strings.ToLower(strings.TrimSpace(cssProp(attrs, "font-weight")))
		if fw == "normal" || fw == "400" {
			st.bold = boolPtr(false)
		} else {
			st.bold = boolPtr(true)
		}
	case "em", "i", "cite", "dfn", "var":
		st.italic = boolPtr(true)
	case "u", "ins":
		st.underline = boolPtr(true)
	case "del", "s", "strike":
		st.strike = boolPtr(true)
	case "code", "kbd", "samp", "tt":
		if hasCSSClass(attrVal(attrs, "class"), "math") {
			st.math = boolPtr(true)
		} else {
			st.code = boolPtr(true)
		}
	case "span":
		if hasCSSClass(attrVal(attrs, "class"), "math") {
			st.math = boolPtr(true)
		}
	case "sup":
		st.sup = boolPtr(true)
	case "sub":
		st.sub = boolPtr(true)
	case "mark":
		st.bgColor = strPtr("yellow")
	case "a":
		if href := media.WebLink(attrVal(attrs, "href")); href != "" {
			st.link = strPtr(href)
			if title := attrVal(attrs, "title"); title != "" {
				st.linkTitle = strPtr(title)
			}
		}
	}

	if fw := strings.ToLower(strings.TrimSpace(cssProp(attrs, "font-weight"))); fw != "" {
		if isCSSBold(fw) {
			st.bold = boolPtr(true)
		} else if fw == "normal" || fw == "400" || fw == "300" {
			st.bold = boolPtr(false)
		}
	}
	if fs := strings.ToLower(strings.TrimSpace(cssProp(attrs, "font-style"))); fs != "" {
		if fs == "italic" || fs == "oblique" {
			st.italic = boolPtr(true)
		} else if fs == "normal" {
			st.italic = boolPtr(false)
		}
	}
	if td := strings.ToLower(cssProp(attrs, "text-decoration")); td != "" {
		if strings.Contains(td, "underline") {
			st.underline = boolPtr(true)
		}
		if strings.Contains(td, "line-through") {
			st.strike = boolPtr(true)
		}
	}
	if bg := pasteHighlightBG(cssProp(attrs, "background-color")); bg != "" {
		st.bgColor = strPtr(bg)
	}

	c.inlineStk = append(c.inlineStk, st)
}

func isHighlightBGColor(raw string) bool {
	return pasteHighlightBG(raw) != ""
}

// pasteHighlightBG maps a pasted CSS background to the editor's only highlight (yellow).
// Theme surfaces (near-transparent, gray, white, black) and unparseable values are dropped.
func pasteHighlightBG(raw string) string {
	bg := strings.ToLower(strings.TrimSpace(cleanColorValue(raw)))
	if bg == "" {
		return ""
	}
	switch bg {
	case "yellow", "gold", "orange", "pink", "lime", "cyan", "aqua", "red", "green", "blue",
		"lightyellow", "lightgreen", "lightblue", "lightpink", "khaki", "violet", "purple", "magenta", "fuchsia":
		return "yellow"
	}
	r, g, b, a, ok := parseCSSColorRGBA(bg)
	if !ok || a < 0.25 {
		return ""
	}
	hi := max(r, g, b)
	lo := min(r, g, b)
	chroma := hi - lo
	if chroma < 12 {
		return ""
	}
	l := (hi + lo) / 510
	denom := 1 - abs64(2*l-1)
	if denom <= 0 || (chroma/255)/denom < 0.15 {
		return ""
	}
	return "yellow"
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// parseCSSColorRGBA parses #hex, rgb()/rgba() and hsl()/hsla() into 0-255 channels and 0-1 alpha.
func parseCSSColorRGBA(s string) (r, g, b, a float64, ok bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			var sb strings.Builder
			for i := 0; i < len(h); i++ {
				sb.WriteByte(h[i])
				sb.WriteByte(h[i])
			}
			h = sb.String()
		}
		if len(h) != 6 && len(h) != 8 {
			return 0, 0, 0, 0, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		a = 1
		if len(h) == 8 {
			a = float64(v&0xff) / 255
			v >>= 8
		}
		return float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff), a, true
	}
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return 0, 0, 0, 0, false
	}
	fn := s[:open]
	body := strings.NewReplacer(",", " ", "/", " ").Replace(s[open+1 : len(s)-1])
	parts := strings.Fields(body)
	if len(parts) != 3 && len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	num := func(p string, scale float64) (float64, bool) {
		pct := strings.HasSuffix(p, "%")
		p = strings.TrimSuffix(strings.TrimSuffix(p, "%"), "deg")
		f, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, false
		}
		if pct {
			return f / 100 * scale, true
		}
		return f, true
	}
	a = 1
	if len(parts) == 4 {
		v, good := num(parts[3], 1)
		if !good {
			return 0, 0, 0, 0, false
		}
		a = v
	}
	switch fn {
	case "rgb", "rgba":
		var c [3]float64
		for i := 0; i < 3; i++ {
			v, good := num(parts[i], 255)
			if !good {
				return 0, 0, 0, 0, false
			}
			c[i] = v
		}
		return c[0], c[1], c[2], a, true
	case "hsl", "hsla":
		h, ok1 := num(parts[0], 360)
		sat, ok2 := num(parts[1], 1)
		lig, ok3 := num(parts[2], 1)
		if !ok1 || !ok2 || !ok3 {
			return 0, 0, 0, 0, false
		}
		if !strings.HasSuffix(parts[1], "%") {
			sat /= 100
		}
		if !strings.HasSuffix(parts[2], "%") {
			lig /= 100
		}
		cc := (1 - abs64(2*lig-1)) * sat
		hp := math.Mod(h, 360) / 60
		x := cc * (1 - abs64(math.Mod(hp, 2)-1))
		var r1, g1, b1 float64
		switch {
		case hp < 1:
			r1, g1 = cc, x
		case hp < 2:
			r1, g1 = x, cc
		case hp < 3:
			g1, b1 = cc, x
		case hp < 4:
			g1, b1 = x, cc
		case hp < 5:
			r1, b1 = x, cc
		default:
			r1, b1 = cc, x
		}
		m := lig - cc/2
		return (r1 + m) * 255, (g1 + m) * 255, (b1 + m) * 255, a, true
	}
	return 0, 0, 0, 0, false
}

func (c *htmlPasteConverter) popInlineTag(name string) {
	for i := len(c.inlineStk) - 1; i >= 0; i-- {
		if c.inlineStk[i].tag == name {
			c.inlineStk = append(c.inlineStk[:i], c.inlineStk[i+1:]...)
			return
		}
	}
}

func (c *htmlPasteConverter) consumePendingRowSpans() {
	if c.table == nil {
		return
	}
	for {
		col := len(c.table.curRow)
		if col >= len(c.table.rowSpans) || c.table.rowSpans[col] <= 0 {
			break
		}
		c.table.rowSpans[col]--
		c.table.curRow = append(c.table.curRow, htmlTableCell{isHeader: c.table.inThead})
	}
}

func (c *htmlPasteConverter) finishTableCell() {
	if c.table == nil || !c.table.inCell {
		return
	}
	c.consumePendingRowSpans()
	spans := trimSpanEdges(c.table.cellSpans)
	isHdr := c.table.cellHeader
	colspan := c.table.cellColspan
	if colspan < 1 {
		colspan = 1
	}
	rowspan := c.table.cellRowspan
	if rowspan < 1 {
		rowspan = 1
	}
	c.table.cellSpans = nil
	c.table.inCell = false
	c.table.cellHeader = false
	c.table.cellColspan = 1
	c.table.cellRowspan = 1

	var parts []string
	for _, grp := range splitSpansOnNewline(spans) {
		if s := strings.TrimSpace(SerializeInline(grp)); s != "" {
			if strings.Contains(s, `\|`) {
				s = strings.ReplaceAll(strings.TrimSpace(serializeInlineWithModeDefs(grp, true, false, nil)), "|", "&#124;")
			} else {
				s = strings.ReplaceAll(s, "|", `\|`)
			}
			parts = append(parts, s)
		}
	}
	startCol := len(c.table.curRow)
	c.table.curRow = append(c.table.curRow, htmlTableCell{
		spans:    spans,
		pipeText: strings.Join(parts, "<br>"),
		isHeader: isHdr,
	})
	for k := 1; k < colspan; k++ {
		c.table.curRow = append(c.table.curRow, htmlTableCell{isHeader: isHdr})
	}
	if rowspan > 1 {
		need := startCol + colspan
		for len(c.table.rowSpans) < need {
			c.table.rowSpans = append(c.table.rowSpans, 0)
		}
		for col := startCol; col < startCol+colspan; col++ {
			if rowspan-1 > c.table.rowSpans[col] {
				c.table.rowSpans[col] = rowspan - 1
			}
		}
	}
}

func (c *htmlPasteConverter) finishTableRow() {
	if c.table == nil {
		return
	}
	if c.table.inCell {
		c.finishTableCell()
	}
	if len(c.table.curRow) > 0 {
		c.consumePendingRowSpans()
		c.table.rows = append(c.table.rows, c.table.curRow)
		c.table.curRow = nil
	}
}

func parseHTMLSpanAttr(attrs [][2]string, key string) int {
	raw := strings.TrimSpace(attrVal(attrs, key))
	if raw == "" {
		return 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 1
	}
	if n > 64 {
		return 64
	}
	return n
}

func (c *htmlPasteConverter) flushTable() {
	tbl := c.table
	c.table = nil
	if tbl == nil {
		return
	}
	if tbl.inCell {
		c.table = tbl
		c.finishTableCell()
		c.table = nil
	}
	if len(tbl.curRow) > 0 {
		tbl.rows = append(tbl.rows, tbl.curRow)
		tbl.curRow = nil
	}
	if len(tbl.rows) == 0 {
		return
	}
	maxCols := 0
	for _, r := range tbl.rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return
	}
	// A ragged table must fit before expanding its missing cells.
	if len(tbl.rows) > maxParsedTableCells/maxCols {
		c.overBudget = true
		return
	}
	row0AnyHeader := false
	row0AllHeader := true
	nonRow0AnyHeader := false
	for rIdx, row := range tbl.rows {
		for col := 0; col < maxCols; col++ {
			hdr := col < len(row) && row[col].isHeader
			if rIdx == 0 {
				if hdr {
					row0AnyHeader = true
				} else {
					row0AllHeader = false
				}
			} else if hdr {
				nonRow0AnyHeader = true
			}
		}
	}
	if (row0AnyHeader && !row0AllHeader) || nonRow0AnyHeader {
		var sb strings.Builder
		sb.WriteString("<!-- thinkly:table header=0 -->\n<table>\n")
		for _, row := range tbl.rows {
			sb.WriteString("<tr>")
			for col := 0; col < maxCols; col++ {
				tag := "td"
				var spans []model.Span
				if col < len(row) {
					if row[col].isHeader {
						tag = "th"
					}
					spans = row[col].spans
				}
				sb.WriteByte('<')
				sb.WriteString(tag)
				sb.WriteByte('>')
				var cellBuf strings.Builder
				serializeInlineHTML(spans, &cellBuf)
				sb.WriteString(cellBuf.String())
				sb.WriteString("</")
				sb.WriteString(tag)
				sb.WriteByte('>')
			}
			sb.WriteString("</tr>\n")
		}
		sb.WriteString("</table>")
		c.appendOutput(sb.String())
		return
	}

	var outRows []string
	if !row0AnyHeader {
		outRows = append(outRows, "<!-- thinkly:table header=0 -->")
	}
	for rIdx, row := range tbl.rows {
		cells := make([]string, maxCols)
		for col := 0; col < maxCols; col++ {
			if col < len(row) {
				cells[col] = row[col].pipeText
			}
		}
		outRows = append(outRows, "| "+strings.Join(cells, " | ")+" |")
		if rIdx == 0 {
			seps := make([]string, maxCols)
			for col := range seps {
				seps[col] = "---"
			}
			outRows = append(outRows, "| "+strings.Join(seps, " | ")+" |")
		}
	}
	c.appendOutput(strings.Join(outRows, "\n"))
}

func (c *htmlPasteConverter) appendOutput(markdown string) {
	c.outputBytes += len(markdown) + 2
	if c.outputBytes > media.MaxPasteBytes {
		c.overBudget = true
		return
	}
	c.blocks = append(c.blocks, markdown)
}

func (c *htmlPasteConverter) parse(src string) {
	const maxDepth = 64
	pos := 0
	for pos < len(src) {
		if c.overBudget || len(c.inlineStk) > maxDepth || len(c.listStk) > maxDepth ||
			len(c.liStk) > maxDepth || c.quoteDepth > maxDepth {
			c.overBudget = true
			return
		}
		lt := strings.IndexByte(src[pos:], '<')
		if lt < 0 {
			c.pushText(src[pos:])
			break
		}
		if lt > 0 {
			c.pushText(src[pos : pos+lt])
		}
		pos += lt
		name, attrs, closed, empty, consumed, ok := parseClipboardTag(src, pos)
		if !ok {
			c.pushText(src[pos : pos+1])
			pos++
			continue
		}
		pos += consumed

		if c.inPre {
			if name == "pre" && closed {
				c.inPre = false
				code := strings.ReplaceAll(c.preBuf.String(), "\r\n", "\n")
				code = strings.TrimPrefix(code, "\n")
				code = strings.TrimRight(code, "\n")
				fence := "```"
				if strings.Contains(code, "```") {
					fence = "````"
				}
				c.emitBlockLine(fence + c.preLang + "\n" + code + "\n" + fence)
				c.preBuf.Reset()
				c.preLang = ""
				continue
			}
			if name == "code" && !closed && c.preLang == "" {
				c.preLang = extractCodeLangFromAttrs(attrs)
				continue
			}
			if name == "br" && !closed {
				c.preBuf.WriteByte('\n')
				continue
			}
			if (name == "div" || name == "p") && closed && !strings.HasSuffix(c.preBuf.String(), "\n") {
				c.preBuf.WriteByte('\n')
			}
			continue
		}

		if c.table != nil {
			if c.table.nestedDepth > 0 {
				switch name {
				case "table":
					if !closed && !empty {
						c.table.nestedDepth++
					} else if closed {
						c.table.nestedDepth--
					}
				case "tr", "p", "div":
					if closed && c.table.inCell && len(c.table.cellSpans) > 0 {
						c.pushBreak()
					}
				case "br":
					if !closed && c.table.inCell {
						c.pushBreak()
					}
				case "td", "th":
					if closed && c.table.inCell && len(c.table.cellSpans) > 0 {
						c.pushText(" ")
					}
				case "img":
					if !closed && c.table.inCell {
						srcURL := strings.TrimSpace(attrVal(attrs, "src"))
						alt := attrVal(attrs, "alt")
						marks, link, linkTitle, bg := c.activeStyle()
						if isPasteableRemoteOrLocalImage(srcURL) {
							title := attrVal(attrs, "title")
							label := alt
							altEmpty := label == ""
							if altEmpty {
								label = fallbackImageAlt(srcURL)
							}
							c.table.cellSpans = append(c.table.cellSpans, model.Span{
								Text:          label,
								Marks:         marks,
								Link:          link,
								LinkTitle:     linkTitle,
								ImageSrc:      srcURL,
								ImageTitle:    title,
								ImageAltEmpty: altEmpty,
								BGColor:       bg,
							})
						} else if alt != "" {
							c.table.cellSpans = append(c.table.cellSpans, model.Span{
								Text:      alt,
								Marks:     marks,
								Link:      link,
								LinkTitle: linkTitle,
								BGColor:   bg,
							})
						}
					}
				default:
					if isKnownInlineTag(name) {
						if closed {
							c.popInlineTag(name)
						} else if !empty {
							c.pushInlineTag(name, attrs)
						}
					}
				}
				continue
			}

			switch name {
			case "table":
				if !closed && !empty {
					c.table.nestedDepth++
				} else if closed {
					c.finishTableRow()
					c.flushTable()
				}
				continue
			case "thead":
				if !closed && !empty {
					c.table.inThead = true
				} else if closed {
					c.table.inThead = false
				}
				continue
			case "tbody", "tfoot":
				if !closed && !empty {
					c.table.inThead = false
				}
				continue
			case "tr":
				if !closed && !empty {
					c.finishTableRow()
				} else if closed {
					c.finishTableRow()
				}
				continue
			case "th", "td":
				if !closed {
					if c.table.inCell {
						c.finishTableCell()
					}
					c.table.inCell = true
					c.table.cellSpans = nil
					c.table.cellHeader = name == "th" || c.table.inThead
					c.table.cellColspan = parseHTMLSpanAttr(attrs, "colspan")
					c.table.cellRowspan = parseHTMLSpanAttr(attrs, "rowspan")
					if empty {
						c.finishTableCell()
					}
				} else if closed && c.table.inCell {
					c.finishTableCell()
				}
				continue
			case "br":
				if !closed && c.table.inCell {
					c.pushBreak()
				}
				continue
			case "p", "div":
				if closed && c.table.inCell && len(c.table.cellSpans) > 0 {
					c.pushBreak()
				}
				continue
			case "img":
				if !closed && c.table.inCell {
					srcURL := strings.TrimSpace(attrVal(attrs, "src"))
					alt := attrVal(attrs, "alt")
					marks, link, linkTitle, bg := c.activeStyle()
					if isPasteableRemoteOrLocalImage(srcURL) {
						title := attrVal(attrs, "title")
						label := alt
						altEmpty := label == ""
						if altEmpty {
							label = fallbackImageAlt(srcURL)
						}
						c.table.cellSpans = append(c.table.cellSpans, model.Span{
							Text:          label,
							Marks:         marks,
							Link:          link,
							LinkTitle:     linkTitle,
							ImageSrc:      srcURL,
							ImageTitle:    title,
							ImageAltEmpty: altEmpty,
							BGColor:       bg,
						})
					} else if alt != "" {
						c.table.cellSpans = append(c.table.cellSpans, model.Span{
							Text:      alt,
							Marks:     marks,
							Link:      link,
							LinkTitle: linkTitle,
							BGColor:   bg,
						})
					}
				}
				continue
			}
			if isKnownInlineTag(name) {
				if closed {
					c.popInlineTag(name)
				} else if !empty {
					c.pushInlineTag(name, attrs)
				}
			}
			continue
		}

		switch name {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			lvl := int(name[1] - '0')
			if !closed && !empty {
				c.flushCurrentBlock()
				c.headingLvl = lvl
			} else if closed {
				c.flushCurrentBlock()
				c.headingLvl = 0
			}

		case "p":
			c.flushCurrentBlock()
			if !closed && !empty {
				c.inPara++
				al := parseImageAlign(attrVal(attrs, "align"))
				if al == "" {
					al = parseImageAlign(cssProp(attrs, "text-align"))
				}
				c.paraAlignStk = append(c.paraAlignStk, al)
			} else if closed && c.inPara > 0 {
				c.inPara--
				if len(c.paraAlignStk) > 0 {
					c.paraAlignStk = c.paraAlignStk[:len(c.paraAlignStk)-1]
				}
			}

		case "div", "section", "article", "header", "footer", "main":
			c.flushCurrentBlock()
			if name == "div" {
				if !closed && !empty {
					al := parseImageAlign(attrVal(attrs, "align"))
					if al == "" {
						al = parseImageAlign(cssProp(attrs, "text-align"))
					}
					c.paraAlignStk = append(c.paraAlignStk, al)
				} else if closed && len(c.paraAlignStk) > 0 {
					c.paraAlignStk = c.paraAlignStk[:len(c.paraAlignStk)-1]
				}
			}

		case "br":
			if !closed {
				c.pushBreak()
			}

		case "hr":
			if !closed {
				c.flushCurrentBlock()
				c.emitBlockLine("---")
			}

		case "blockquote", "aside":
			c.flushCurrentBlock()
			if !closed && !empty {
				c.quoteDepth++
			} else if closed && c.quoteDepth > 0 {
				c.quoteDepth--
			}

		case "pre":
			if !closed && !empty {
				c.flushCurrentBlock()
				c.inPre = true
				c.preLang = extractCodeLangFromAttrs(attrs)
				c.preBuf.Reset()
			}

		case "ul", "ol":
			if !closed && !empty {
				c.flushCurrentBlock()
				startNum := 1
				if name == "ol" {
					if sv := strings.TrimSpace(attrVal(attrs, "start")); sv != "" {
						if n, err := strconv.Atoi(sv); err == nil && n > 0 {
							startNum = n
						}
					}
				}
				c.listStk = append(c.listStk, htmlListCtx{ordered: name == "ol", nextNum: startNum})
			} else if closed && len(c.listStk) > 0 {
				c.flushCurrentBlock()
				c.listStk = c.listStk[:len(c.listStk)-1]
			}

		case "li":
			if !closed && !empty {
				c.flushCurrentBlock()
				st := htmlLIState{
					extraIndent: extractHTMLListExtraIndent(attrs, len(c.listStk)),
				}
				if dl := strings.ToLower(attrVal(attrs, "data-list")); strings.Contains(dl, "check") || strings.Contains(dl, "todo") {
					st.todo = true
					st.checked = strings.Contains(dl, "checked") || strings.EqualFold(attrVal(attrs, "data-checked"), "true")
				}
				c.liStk = append(c.liStk, st)
			} else if closed && len(c.liStk) > 0 {
				c.flushCurrentBlock()
				c.liStk = c.liStk[:len(c.liStk)-1]
			}

		case "input":
			if !closed && len(c.liStk) > 0 && strings.EqualFold(strings.TrimSpace(attrVal(attrs, "type")), "checkbox") {
				li := &c.liStk[len(c.liStk)-1]
				li.todo = true
				li.checked = false
				for _, kv := range attrs {
					if kv[0] == "checked" {
						li.checked = true
						break
					}
				}
			}

		case "table":
			if !closed && !empty {
				c.flushCurrentBlock()
				c.table = &htmlTableCtx{}
			}

		case "img":
			if !closed {
				srcURL := strings.TrimSpace(attrVal(attrs, "src"))
				alt := attrVal(attrs, "alt")
				marks, link, linkTitle, bg := c.activeStyle()
				if isPasteableRemoteOrLocalImage(srcURL) {
					title := attrVal(attrs, "title")
					imgW := parseImageWidth(attrVal(attrs, "width"))
					if imgW == 0 {
						if sw := cssProp(attrs, "width"); sw != "" {
							imgW = parseImageWidth(sw)
						}
					}
					imgAlign := parseImageAlign(attrVal(attrs, "align"))
					inTableCell := c.table != nil && c.table.inCell
					label := alt
					altEmpty := label == ""
					if altEmpty {
						label = fallbackImageAlt(srcURL)
					}
					if inTableCell || link != "" || marks != 0 || bg != "" || len(c.curSpans) > 0 || c.headingLvl > 0 || len(c.liStk) > 0 || c.quoteDepth > 0 || c.inPara > 0 {
						if !inTableCell && len(c.curSpans) == 0 && c.headingLvl == 0 && len(c.liStk) == 0 && c.quoteDepth == 0 {
							c.soloImgWidth = imgW
							if imgAlign != "" {
								c.soloImgAlign = imgAlign
							}
						}
						target := c.targetSpans()
						*target = append(*target, model.Span{
							Text:          label,
							Marks:         marks,
							Link:          link,
							LinkTitle:     linkTitle,
							ImageSrc:      srcURL,
							ImageTitle:    title,
							ImageAltEmpty: altEmpty,
							BGColor:       bg,
						})
					} else {
						c.flushCurrentBlock()
						al := imgAlign
						if al == "" {
							al = c.currentParaAlign()
						}
						if imgW > 0 || (al != "" && al != model.ImageAlignLeft) {
							c.emitBlockLine(formatStyledBlockImageHTML(srcURL, alt, title, imgW, al))
						} else {
							c.emitBlockLine(SerializeInline([]model.Span{{
								Text:          label,
								ImageSrc:      srcURL,
								ImageTitle:    title,
								ImageAltEmpty: altEmpty,
							}}))
						}
					}
				} else if alt != "" {
					target := c.targetSpans()
					*target = append(*target, model.Span{
						Text:      alt,
						Marks:     marks,
						Link:      link,
						LinkTitle: linkTitle,
						BGColor:   bg,
					})
				}
			}

		default:
			if isKnownInlineTag(name) {
				if closed {
					c.popInlineTag(name)
				} else if !empty {
					c.pushInlineTag(name, attrs)
				}
			}
		}
	}
	c.flushCurrentBlock()
	if c.table != nil {
		c.flushTable()
	}
}

func (c *htmlPasteConverter) result() string {
	if len(c.blocks) == 0 {
		return ""
	}
	var out strings.Builder
	for i, blk := range c.blocks {
		if i > 0 {
			prev := c.blocks[i-1]
			if isMarkdownListBlockChunk(prev) && isMarkdownListBlockChunk(blk) {
				out.WriteByte('\n')
			} else {
				out.WriteString("\n\n")
			}
		}
		out.WriteString(blk)
	}
	return out.String()
}

func isMarkdownListBlockChunk(chunk string) bool {
	firstLine := chunk
	if nl := strings.IndexByte(chunk, '\n'); nl >= 0 {
		firstLine = chunk[:nl]
	}
	_, ok := classifyListMarker(firstLine)
	return ok
}

func extractHTMLListExtraIndent(attrs [][2]string, listDepth int) int {
	if listDepth > 1 {
		return 0
	}
	if v := strings.TrimSpace(attrVal(attrs, "data-indent")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 8 {
			return n
		}
	}
	if v := strings.TrimSpace(attrVal(attrs, "aria-level")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 1 && n <= 9 {
			return n - 1
		}
	}
	cls := strings.ToLower(attrVal(attrs, "class"))
	for _, field := range strings.Fields(cls) {
		if strings.HasPrefix(field, "ql-indent-") {
			if n, err := strconv.Atoi(field[len("ql-indent-"):]); err == nil && n > 0 && n <= 8 {
				return n
			}
		}
		for _, pfx := range []string{"list-bullet", "list-number", "level-"} {
			if strings.HasPrefix(field, pfx) {
				if n, err := strconv.Atoi(field[len(pfx):]); err == nil && n > 1 && n <= 9 {
					return n - 1
				}
			}
		}
	}
	style := strings.ToLower(attrVal(attrs, "style"))
	if idx := strings.Index(style, "mso-list:"); idx >= 0 {
		rest := style[idx:]
		if lvIdx := strings.Index(rest, "level"); lvIdx >= 0 {
			numPart := rest[lvIdx+len("level"):]
			digits := 0
			for digits < len(numPart) && numPart[digits] >= '0' && numPart[digits] <= '9' {
				digits++
			}
			if digits > 0 {
				if n, err := strconv.Atoi(numPart[:digits]); err == nil && n > 1 && n <= 9 {
					return n - 1
				}
			}
		}
	}
	return 0
}
