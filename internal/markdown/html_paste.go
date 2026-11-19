package markdown

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode"

	"block/internal/model"
)

// ConvertClipboardHTML converts rich semantic HTML from web pages, Feishu Docs, Notion,
// Google Docs, Word, or Apple Notes into clean Block Markdown.
// It returns ("", false) when:
//   - plainFallback already contains explicit authored Markdown syntax,
//   - htmlRaw originates from a syntax-highlighted code editor / terminal dump without semantic HTML, or
//   - htmlRaw carries no semantic structure or inline formatting beyond plain text.
func ConvertClipboardHTML(htmlRaw, plainFallback string) (string, bool) {
	if strings.TrimSpace(htmlRaw) == "" {
		return "", false
	}
	if looksLikeAuthoredMarkdown(plainFallback) {
		return "", false
	}
	fragment := extractHTMLClipboardFragment(htmlRaw)
	if strings.TrimSpace(fragment) == "" {
		return "", false
	}
	if isCodeEditorHTMLDump(fragment) || !hasSemanticHTMLFeatures(fragment) {
		return "", false
	}

	conv := &htmlPasteConverter{}
	conv.parse(fragment)
	out := strings.TrimSpace(NormalizePasteText(conv.result()))
	if out == "" {
		return "", false
	}
	return out, true
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
		}
		name, _, closed, empty, consumed, ok := parseTagRaw(s[i:])
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
			}
		}
		if ok && (name == "meta" || name == "link") {
			i += consumed
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
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
	for _, sem := range []string{"<h1", "<h2", "<h3", "<h4", "<h5", "<h6", "<ul", "<ol", "<li", "<table", "<blockquote", "<a ", "<strong", "<b ", "<b>", "<em", "<mark"} {
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
		name, attrs, closed, _, consumed, ok := parseTagRaw(fragment[i:])
		if !ok {
			i++
			continue
		}
		if !closed {
			switch name {
			case "h1", "h2", "h3", "h4", "h5", "h6",
				"ul", "ol", "li", "table", "blockquote", "aside", "pre", "hr",
				"strong", "em", "i", "u", "ins", "del", "s", "strike", "code", "kbd", "samp", "mark":
				return true
			case "b":
				fw := strings.ToLower(strings.TrimSpace(cssProp(attrs, "font-weight")))
				if fw != "normal" && fw != "400" {
					return true
				}
			case "a":
				if href := strings.TrimSpace(attrVal(attrs, "href")); href != "" && !strings.HasPrefix(strings.ToLower(href), "javascript:") {
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
				if isCSSBold(cssProp(attrs, "font-weight")) ||
					strings.EqualFold(cssProp(attrs, "font-style"), "italic") {
					return true
				}
				if td := strings.ToLower(cssProp(attrs, "text-decoration")); strings.Contains(td, "underline") || strings.Contains(td, "line-through") {
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
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

type htmlInlineState struct {
	tag       string
	bold      *bool
	italic    *bool
	underline *bool
	strike    *bool
	code      *bool
	highlight *bool
	link      *string
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

type htmlTableCtx struct {
	rows        [][]string
	curRow      []string
	inCell      bool
	cellSpans   []model.Span
	hasHeaderTR bool
}

type htmlPasteConverter struct {
	blocks     []string
	curSpans   []model.Span
	inlineStk  []htmlInlineState
	listStk    []htmlListCtx
	liStk      []htmlLIState
	quoteDepth int
	headingLvl int
	inPre      bool
	preLang    string
	preBuf     strings.Builder
	table      *htmlTableCtx
}

func (c *htmlPasteConverter) activeStyle() (marks model.Mark, link string, bg string) {
	var bold, italic, under, strike, code, hi bool
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
		if f.highlight != nil {
			hi = *f.highlight
		}
		if f.link != nil {
			link = *f.link
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
	if code {
		marks |= model.MarkCode
	}
	if hi {
		bg = "yellow"
	}
	return marks, link, bg
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
	// Collapse HTML source formatting newlines/tabs into single spaces while keeping non-breaking spaces clean.
	var norm strings.Builder
	norm.Grow(len(decoded))
	lastSpace := false
	spansPtr := c.targetSpans()
	if len(*spansPtr) == 0 {
		lastSpace = true
	} else {
		prev := (*spansPtr)[len(*spansPtr)-1].Text
		if strings.HasSuffix(prev, " ") || strings.HasSuffix(prev, "\n") {
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
	marks, link, bg := c.activeStyle()
	*spansPtr = append(*spansPtr, model.Span{
		Text:    txt,
		Marks:   marks,
		Link:    link,
		BGColor: bg,
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
	last := &(*spansPtr)[len(*spansPtr)-1]
	last.Text = strings.TrimRight(last.Text, " ")
	*spansPtr = append(*spansPtr, model.Span{Text: "\n"})
}

func trimSpanEdges(spans []model.Span) []model.Span {
	norm := coalesceSpans(spans)
	if len(norm) == 0 {
		return nil
	}
	norm[0].Text = strings.TrimLeft(norm[0].Text, " \t\n")
	norm[len(norm)-1].Text = strings.TrimRight(norm[len(norm)-1].Text, " \t\n")
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
	c.blocks = append(c.blocks, blockMD)
}

func (c *htmlPasteConverter) flushCurrentBlock() {
	spans := trimSpanEdges(c.curSpans)
	c.curSpans = nil
	hasActiveLI := len(c.listStk) > 0 && len(c.liStk) > 0
	var li *htmlLIState
	if hasActiveLI {
		li = &c.liStk[len(c.liStk)-1]
	}
	if len(spans) == 0 && !(hasActiveLI && li.todo && !li.flushed) {
		return
	}

	if c.headingLvl >= 1 && c.headingLvl <= 6 {
		// Collapse multi-line headings into a single clean ATX heading line.
		for i := range spans {
			spans[i].Text = strings.ReplaceAll(spans[i].Text, "\n", " ")
		}
		text := strings.TrimSpace(SerializeInline(spans))
		if text != "" {
			c.emitBlockLine(strings.Repeat("#", c.headingLvl) + " " + text)
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
		st.code = boolPtr(true)
	case "mark":
		st.highlight = boolPtr(true)
	case "a":
		href := strings.TrimSpace(attrVal(attrs, "href"))
		if href != "" && !strings.HasPrefix(strings.ToLower(href), "javascript:") {
			st.link = strPtr(href)
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

	c.inlineStk = append(c.inlineStk, st)
}

func (c *htmlPasteConverter) popInlineTag(name string) {
	for i := len(c.inlineStk) - 1; i >= 0; i-- {
		if c.inlineStk[i].tag == name {
			c.inlineStk = append(c.inlineStk[:i], c.inlineStk[i+1:]...)
			return
		}
	}
}

func (c *htmlPasteConverter) flushTable() {
	tbl := c.table
	c.table = nil
	if tbl == nil || len(tbl.rows) == 0 {
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
	var outRows []string
	for rIdx, row := range tbl.rows {
		cells := make([]string, maxCols)
		for col := 0; col < maxCols; col++ {
			if col < len(row) {
				cells[col] = row[col]
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
	c.blocks = append(c.blocks, strings.Join(outRows, "\n"))
}

func (c *htmlPasteConverter) parse(src string) {
	pos := 0
	for pos < len(src) {
		lt := strings.IndexByte(src[pos:], '<')
		if lt < 0 {
			c.pushText(src[pos:])
			break
		}
		if lt > 0 {
			c.pushText(src[pos : pos+lt])
		}
		pos += lt
		name, attrs, closed, empty, consumed, ok := parseTagRaw(src[pos:])
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
			switch name {
			case "table":
				if closed {
					c.flushTable()
				}
				continue
			case "tr":
				if !closed && !empty {
					c.table.curRow = nil
				} else if closed && len(c.table.curRow) > 0 {
					c.table.rows = append(c.table.rows, c.table.curRow)
					c.table.curRow = nil
				}
				continue
			case "th", "td":
				if !closed && !empty {
					c.table.inCell = true
					c.table.cellSpans = nil
					if name == "th" {
						c.table.hasHeaderTR = true
					}
				} else if closed && c.table.inCell {
					spans := trimSpanEdges(c.table.cellSpans)
					c.table.cellSpans = nil
					c.table.inCell = false
					var parts []string
					for _, grp := range splitSpansOnNewline(spans) {
						if s := strings.TrimSpace(SerializeInline(grp)); s != "" {
							s = strings.ReplaceAll(s, "|", `\|`)
							parts = append(parts, s)
						}
					}
					c.table.curRow = append(c.table.curRow, strings.Join(parts, "<br>"))
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

		case "p", "div", "section", "article", "header", "footer", "main":
			c.flushCurrentBlock()

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
				if isPasteableRemoteOrLocalImage(srcURL) {
					alt := strings.TrimSpace(attrVal(attrs, "alt"))
					c.flushCurrentBlock()
					c.emitBlockLine(fmt.Sprintf("![%s](%s)", strings.ReplaceAll(alt, "]", `\]`), formatLinkDest(srcURL)))
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
