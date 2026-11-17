package markdown

import (
	"html"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/model"
)

type refDef struct {
	dest  string
	title string
}

type inlineStyle struct {
	marks   model.Mark
	link    string
	fgColor string
	bgColor string
}

// ParseInline parses inline Markdown and safe inline HTML into normalized model.Span runs.
func ParseInline(raw string) []model.Span {
	return parseInlineWithDefs(raw, nil)
}

func parseInlineWithDefs(raw string, defs map[string]refDef) []model.Span {
	if raw == "" {
		return nil
	}
	if len(defs) == 0 && !hasASCIIPunct(raw) {
		return []model.Span{{Text: raw}}
	}
	toks := scanInlineTokens(raw, defs)
	resolveDelimiters(toks)
	spans := emitSpans(toks)
	return coalesceSpans(spans)
}

func hasASCIIPunct(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~') {
			return true
		}
	}
	return false
}

type tokKind uint8

const (
	tokText tokKind = iota
	tokCode
	tokDelim
	tokOpenStyle
	tokCloseStyle
	tokLinkOpen
	tokLinkClose
)

type inlineTok struct {
	kind       tokKind
	text       string
	ch         byte
	count      int
	orig       int
	canOp      bool
	canCl      bool
	openMarks  []model.Mark
	closeMarks []model.Mark
	style      inlineStyle
	tag        string
	isMark     model.Mark
	hasFG      bool
	hasBG      bool
	hasLnk     bool
}

func isCJKOrFullwidth(r rune) bool {
	if r >= 0x4E00 && r <= 0x9FFF {
		return true
	}
	if r >= 0x3400 && r <= 0x4DBF {
		return true
	}
	if r >= 0x3000 && r <= 0x303F { // CJK Symbols and Punctuation
		return true
	}
	if r >= 0x3040 && r <= 0x30FF { // Hiragana & Katakana
		return true
	}
	if r >= 0xAC00 && r <= 0xD7AF { // Hangul
		return true
	}
	if r >= 0xFF00 && r <= 0xFFEF { // Halfwidth and Fullwidth Forms
		return true
	}
	return unicode.Is(unicode.Han, r) || strings.ContainsRune("“”‘’—…", r)
}

func isASCIIPunctByte(b byte) bool {
	return (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~')
}

func isUnicodePunct(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func runeBefore(s string, idx int) (rune, bool) {
	if idx <= 0 {
		return 0, false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:idx])
	return r, true
}

func runeAfter(s string, idx int) (rune, bool) {
	if idx >= len(s) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(s[idx:])
	return r, true
}

func isSpaceOrEnd(r rune, ok bool) bool {
	return !ok || unicode.IsSpace(r)
}

func classifyDelim(s string, start, end int, ch byte) (canOpen, canClose bool) {
	before, hasBefore := runeBefore(s, start)
	after, hasAfter := runeAfter(s, end)

	beforeWS := isSpaceOrEnd(before, hasBefore)
	afterWS := isSpaceOrEnd(after, hasAfter)
	beforePunct := hasBefore && isUnicodePunct(before)
	afterPunct := hasAfter && isUnicodePunct(after)

	leftFlank := !afterWS && (!afterPunct || beforeWS || beforePunct)
	rightFlank := !beforeWS && (!beforePunct || afterWS || afterPunct)

	// Relax CJK / fullwidth punctuation flanking for `*` and `~` (e.g. `**11/11：**汕头`).
	if ch == '*' || ch == '~' {
		if !afterWS && ((hasBefore && isCJKOrFullwidth(before)) || (hasAfter && isCJKOrFullwidth(after))) {
			leftFlank = true
		}
		if !beforeWS && ((hasBefore && isCJKOrFullwidth(before)) || (hasAfter && isCJKOrFullwidth(after))) {
			rightFlank = true
		}
	}

	if ch == '_' {
		canOpen = leftFlank && (!rightFlank || beforePunct)
		canClose = rightFlank && (!leftFlank || afterPunct)
		return canOpen, canClose
	}
	return leftFlank, rightFlank
}

func scanInlineTokens(s string, defs map[string]refDef) []*inlineTok {
	var toks []*inlineTok
	pairedUnknown := scanPairedUnknownTags(s)
	n := len(s)
	i := 0
	for i < n {
		b := s[i]
		switch b {
		case '\\':
			if i+1 < n {
				next := s[i+1]
				if next == '\n' {
					toks = append(toks, &inlineTok{kind: tokText, text: "\n"})
					i += 2
					continue
				}
				if next == '\r' && i+2 < n && s[i+2] == '\n' {
					toks = append(toks, &inlineTok{kind: tokText, text: "\n"})
					i += 3
					continue
				}
				if isASCIIPunctByte(next) {
					toks = append(toks, &inlineTok{kind: tokText, text: string(next)})
					i += 2
					continue
				}
			}
			toks = append(toks, &inlineTok{kind: tokText, text: "\\"})
			i++

		case '`':
			run := 0
			for i+run < n && s[i+run] == '`' {
				run++
			}
			end := findClosingBackticks(s, i+run, run)
			if end >= 0 {
				inner := s[i+run : end]
				inner = normalizeCodeSpanContent(inner)
				toks = append(toks, &inlineTok{kind: tokCode, text: inner})
				i = end + run
			} else {
				toks = append(toks, &inlineTok{kind: tokText, text: s[i : i+run]})
				i += run
			}

		case '*', '_', '~':
			run := 0
			for i+run < n && s[i+run] == b {
				run++
			}
			if b == '~' && run != 2 {
				toks = append(toks, &inlineTok{kind: tokText, text: s[i : i+run]})
				i += run
				continue
			}
			op, cl := classifyDelim(s, i, i+run, b)
			toks = append(toks, &inlineTok{
				kind:  tokDelim,
				text:  s[i : i+run],
				ch:    b,
				count: run,
				orig:  run,
				canOp: op,
				canCl: cl,
			})
			i += run

		case '!':
			if i+1 < n && s[i+1] == '[' {
				if alt, dest, consumed, ok := tryParseLinkOrImage(s[i+1:], defs, true); ok {
					if alt == "" {
						alt = fallbackImageAlt(dest)
					}
					toks = append(toks, &inlineTok{kind: tokLinkOpen, style: inlineStyle{link: dest}})
					inner := parseInlineWithDefs(alt, defs)
					for _, sp := range inner {
						toks = append(toks, &inlineTok{kind: tokText, text: sp.Text})
					}
					toks = append(toks, &inlineTok{kind: tokLinkClose})
					i += 1 + consumed
					continue
				}
			}
			toks = append(toks, &inlineTok{kind: tokText, text: "!"})
			i++

		case '[':
			if label, dest, consumed, ok := tryParseLinkOrImage(s[i:], defs, false); ok {
				toks = append(toks, &inlineTok{kind: tokLinkOpen, style: inlineStyle{link: dest}})
				innerToks := scanInlineTokens(label, defs)
				resolveDelimiters(innerToks)
				toks = append(toks, innerToks...)
				toks = append(toks, &inlineTok{kind: tokLinkClose})
				i += consumed
				continue
			}
			toks = append(toks, &inlineTok{kind: tokText, text: "["})
			i++

		case '<':
			if strings.HasPrefix(s[i:], "<!--") {
				if end := strings.Index(s[i+4:], "-->"); end >= 0 {
					i += 4 + end + 3
					continue
				}
			}
			if addr, linkURL, consumed, ok := tryParseAutolink(s[i:]); ok {
				toks = append(toks, &inlineTok{kind: tokLinkOpen, style: inlineStyle{link: linkURL}})
				toks = append(toks, &inlineTok{kind: tokText, text: addr})
				toks = append(toks, &inlineTok{kind: tokLinkClose})
				i += consumed
				continue
			}
			if rawTag, tok, consumed, ok := tryParseHTMLTag(s[i:], pairedUnknown); ok {
				if tok != nil {
					toks = append(toks, tok...)
				} else {
					toks = append(toks, &inlineTok{kind: tokText, text: rawTag})
				}
				i += consumed
				continue
			}
			toks = append(toks, &inlineTok{kind: tokText, text: "<"})
			i++

		case '&':
			if decoded, consumed, ok := tryDecodeEntity(s[i:]); ok {
				toks = append(toks, &inlineTok{kind: tokText, text: decoded})
				i += consumed
				continue
			}
			toks = append(toks, &inlineTok{kind: tokText, text: "&"})
			i++

		default:
			start := i
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
			for i < n {
				nb := s[i]
				if nb == '\\' || nb == '`' || nb == '*' || nb == '_' || nb == '~' || nb == '!' || nb == '[' || nb == '<' || nb == '&' {
					break
				}
				_, sz := utf8.DecodeRuneInString(s[i:])
				i += sz
			}
			toks = append(toks, &inlineTok{kind: tokText, text: s[start:i]})
		}
	}
	return toks
}

func findClosingBackticks(s string, from, count int) int {
	i := from
	for i < len(s) {
		if s[i] == '`' {
			run := 0
			for i+run < len(s) && s[i+run] == '`' {
				run++
			}
			if run == count {
				return i
			}
			i += run
		} else {
			i++
		}
	}
	return -1
}

func normalizeCodeSpanContent(inner string) string {
	if strings.ContainsRune(inner, '\n') {
		inner = strings.ReplaceAll(inner, "\r\n", " ")
		inner = strings.ReplaceAll(inner, "\n", " ")
	}
	if len(inner) >= 2 && inner[0] == ' ' && inner[len(inner)-1] == ' ' && strings.Trim(inner, " ") != "" {
		inner = inner[1 : len(inner)-1]
	}
	return inner
}

func fallbackImageAlt(dest string) string {
	d := strings.TrimSpace(dest)
	if idx := strings.LastIndexByte(d, '/'); idx >= 0 && idx+1 < len(d) {
		return d[idx+1:]
	}
	if d != "" {
		return d
	}
	return "image"
}

func tryParseAutolink(s string) (display, linkURL string, consumed int, ok bool) {
	if len(s) < 4 || s[0] != '<' {
		return "", "", 0, false
	}
	end := strings.IndexByte(s[1:], '>')
	if end <= 0 {
		return "", "", 0, false
	}
	inner := s[1 : 1+end]
	if strings.ContainsAny(inner, " \t\r\n<") {
		return "", "", 0, false
	}
	if colon := strings.IndexByte(inner, ':'); colon >= 2 {
		scheme := inner[:colon]
		validScheme := isASCIIAlpha(scheme[0])
		for i := 1; i < len(scheme) && validScheme; i++ {
			c := scheme[i]
			if !isASCIIAlpha(c) && !(c >= '0' && c <= '9') && c != '+' && c != '.' && c != '-' {
				validScheme = false
			}
		}
		if validScheme {
			return inner, inner, end + 2, true
		}
	}
	if at := strings.IndexByte(inner, '@'); at > 0 && at+1 < len(inner) && strings.ContainsRune(inner[at+1:], '.') && !strings.ContainsRune(inner, '/') {
		return inner, "mailto:" + inner, end + 2, true
	}
	return "", "", 0, false
}

func tryParseLinkOrImage(s string, defs map[string]refDef, isImage bool) (label, dest string, consumed int, ok bool) {
	if len(s) < 2 || s[0] != '[' {
		return "", "", 0, false
	}
	depth := 1
	i := 1
	for i < len(s) && depth > 0 {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '`':
			run := 0
			for i+run < len(s) && s[i+run] == '`' {
				run++
			}
			if end := findClosingBackticks(s, i+run, run); end >= 0 {
				i = end + run
				continue
			}
			i += run
			continue
		case '[':
			if !isImage {
				depth++
			}
		case ']':
			depth--
			if depth == 0 {
				goto foundClose
			}
		}
		i++
	}
	return "", "", 0, false

foundClose:
	label = s[1:i]
	after := i + 1
	if after < len(s) && s[after] == '(' {
		if d, endParen, ok := parseLinkDestination(s[after:]); ok {
			return label, d, after + endParen, true
		}
	}
	if len(defs) > 0 {
		if after < len(s) && s[after] == '[' {
			if closeRef := findUnescapedByte(s[after+1:], ']'); closeRef >= 0 {
				refLabel := s[after+1 : after+1+closeRef]
				key := normRefLabel(refLabel)
				if key == "" {
					key = normRefLabel(label)
				}
				if def, exists := defs[key]; exists {
					return label, def.dest, after + 1 + closeRef + 1, true
				}
			}
		}
		key := normRefLabel(label)
		if def, exists := defs[key]; exists {
			return label, def.dest, after, true
		}
	}
	return "", "", 0, false
}

func parseLinkDestination(s string) (dest string, consumed int, ok bool) {
	if len(s) < 2 || s[0] != '(' {
		return "", 0, false
	}
	i := 1
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	if i >= len(s) {
		return "", 0, false
	}
	if s[i] == ')' {
		return "", i + 1, true
	}
	var rawDest string
	if s[i] == '<' {
		i++
		start := i
		for i < len(s) && s[i] != '>' && s[i] != '\n' {
			if s[i] == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			i++
		}
		if i >= len(s) || s[i] != '>' {
			return "", 0, false
		}
		rawDest = unescapeLinkText(s[start:i])
		i++
	} else {
		start := i
		parens := 0
		for i < len(s) {
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				break
			}
			if c == '(' {
				parens++
			} else if c == ')' {
				if parens == 0 {
					break
				}
				parens--
			}
			i++
		}
		if parens != 0 {
			return "", 0, false
		}
		rawDest = unescapeLinkText(s[start:i])
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	if i < len(s) && (s[i] == '"' || s[i] == '\'' || s[i] == '(') {
		closeQuote := s[i]
		if closeQuote == '(' {
			closeQuote = ')'
		}
		i++
		for i < len(s) && s[i] != closeQuote {
			if s[i] == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			i++
		}
		if i >= len(s) {
			return "", 0, false
		}
		i++
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
			i++
		}
	}
	if i >= len(s) || s[i] != ')' {
		return "", 0, false
	}
	return html.UnescapeString(rawDest), i + 1, true
}

func unescapeLinkText(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunctByte(s[i+1]) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func findUnescapedByte(s string, target byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if s[i] == target {
			return i
		}
	}
	return -1
}

func normRefLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

func tryDecodeEntity(s string) (string, int, bool) {
	semi := strings.IndexByte(s[1:], ';')
	if semi <= 0 || semi > 32 {
		return "", 0, false
	}
	body := s[1 : 1+semi]
	valid := false
	if body[0] == '#' {
		if len(body) >= 2 && (body[1] == 'x' || body[1] == 'X') {
			valid = len(body) > 2 && isAllHex(body[2:])
		} else {
			valid = len(body) > 1 && isAllDigits(body[1:])
		}
	} else if isASCIIAlpha(body[0]) {
		valid = isAllAlphaNum(body[1:])
	}
	if !valid {
		return "", 0, false
	}
	raw := s[:semi+2]
	dec := html.UnescapeString(raw)
	if dec == raw {
		return "", 0, false
	}
	return dec, semi + 2, true
}

func isASCIIAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
func isAllHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}
func isAllAlphaNum(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isASCIIAlpha(c) && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func isKnownInlineTag(name string) bool {
	switch name {
	case "a", "span", "strong", "b", "em", "i", "del", "s", "strike",
		"code", "kbd", "samp", "tt", "var", "u", "ins", "mark", "font",
		"sub", "sup", "small", "big", "q", "cite", "abbr", "dfn",
		"time", "ruby", "rt", "rp", "bdi", "bdo", "label", "br", "wbr":
		return true
	}
	return false
}

func isKnownHTMLElement(name string) bool {
	if isKnownInlineTag(name) {
		return true
	}
	switch name {
	case "p", "div", "center", "section", "article", "header", "footer", "main", "aside", "nav",
		"blockquote", "h1", "h2", "h3", "h4", "h5", "h6", "details", "summary",
		"figure", "figcaption", "ul", "ol", "li", "dl", "dt", "dd", "pre",
		"table", "thead", "tbody", "tfoot", "tr", "td", "th", "colgroup", "col",
		"hr", "img", "picture", "source", "video", "audio", "iframe", "svg", "path",
		"input", "button", "select", "option", "textarea", "form":
		return true
	}
	return false
}

func scanPairedUnknownTags(s string) map[string][2]int {
	if !strings.ContainsRune(s, '<') {
		return nil
	}
	counts := make(map[string][2]int)
	i := 0
	for i < len(s) {
		if s[i] == '<' {
			name, _, closed, empty, consumed, ok := parseTagRaw(s[i:])
			if ok && !empty && !isKnownHTMLElement(name) {
				entry := counts[name]
				if closed {
					entry[1]++
				} else {
					entry[0]++
				}
				counts[name] = entry
				i += consumed
				continue
			}
		}
		i++
	}
	return counts
}

func parseTagRaw(s string) (name string, attrs [][2]string, closed, empty bool, consumed int, ok bool) {
	if len(s) < 3 || s[0] != '<' {
		return "", nil, false, false, 0, false
	}
	i := 1
	if s[i] == '/' {
		closed = true
		i++
	}
	if i >= len(s) || !isASCIIAlpha(s[i]) {
		return "", nil, false, false, 0, false
	}
	startName := i
	for i < len(s) && (isASCIIAlpha(s[i]) || (s[i] >= '0' && s[i] <= '9') || s[i] == '-') {
		i++
	}
	name = strings.ToLower(s[startName:i])
	if i >= len(s) || (s[i] != '>' && s[i] != '/' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r') {
		return "", nil, false, false, 0, false
	}
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if i >= len(s) {
			return "", nil, false, false, 0, false
		}
		if s[i] == '/' {
			empty = true
			i++
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			if i < len(s) && s[i] == '>' {
				return name, attrs, closed, empty, i + 1, true
			}
			return "", nil, false, false, 0, false
		}
		if s[i] == '>' {
			return name, attrs, closed, empty, i + 1, true
		}
		kStart := i
		for i < len(s) && s[i] != '=' && s[i] != '>' && s[i] != '/' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
			i++
		}
		if i == kStart {
			return "", nil, false, false, 0, false
		}
		key := strings.ToLower(s[kStart:i])
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		var val string
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			if i >= len(s) {
				return "", nil, false, false, 0, false
			}
			if s[i] == '"' || s[i] == '\'' {
				q := s[i]
				i++
				vStart := i
				for i < len(s) && s[i] != q {
					i++
				}
				if i >= len(s) {
					return "", nil, false, false, 0, false
				}
				val = html.UnescapeString(s[vStart:i])
				i++
			} else {
				vStart := i
				for i < len(s) && s[i] != '>' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
					i++
				}
				val = html.UnescapeString(s[vStart:i])
			}
		}
		attrs = append(attrs, [2]string{key, val})
	}
	return "", nil, false, false, 0, false
}

func tryParseHTMLTag(s string, pairedUnknown map[string][2]int) (raw string, out []*inlineTok, consumed int, ok bool) {
	name, attrs, closed, empty, consumed, ok := parseTagRaw(s)
	if !ok {
		return "", nil, 0, false
	}
	raw = s[:consumed]
	if name == "br" {
		if !closed {
			return raw, []*inlineTok{{kind: tokText, text: "\n"}}, consumed, true
		}
		return raw, []*inlineTok{}, consumed, true
	}
	if name == "wbr" || name == "hr" {
		return raw, []*inlineTok{}, consumed, true
	}
	if name == "img" && !closed {
		alt := strings.TrimSpace(attrVal(attrs, "alt"))
		src := strings.TrimSpace(attrVal(attrs, "src"))
		label := alt
		if label == "" && src != "" {
			label = fallbackImageAlt(src)
		}
		if label == "" {
			return raw, []*inlineTok{}, consumed, true
		}
		if src != "" {
			return raw, []*inlineTok{
				{kind: tokLinkOpen, style: inlineStyle{link: src}},
				{kind: tokText, text: label},
				{kind: tokLinkClose},
			}, consumed, true
		}
		return raw, []*inlineTok{{kind: tokText, text: label}}, consumed, true
	}
	paired := pairedUnknown[name][0] > 0 && pairedUnknown[name][1] > 0
	if !isKnownHTMLElement(name) && !paired {
		return raw, nil, consumed, true
	}
	if !isKnownInlineTag(name) {
		return raw, []*inlineTok{}, consumed, true
	}
	if empty {
		return raw, []*inlineTok{}, consumed, true
	}
	if closed {
		return raw, []*inlineTok{{kind: tokCloseStyle, tag: name}}, consumed, true
	}
	tok := &inlineTok{kind: tokOpenStyle, tag: name}
	switch name {
	case "strong", "b":
		tok.isMark |= model.MarkBold
	case "em", "i", "cite", "dfn", "var":
		tok.isMark |= model.MarkItalic
	case "u", "ins":
		tok.isMark |= model.MarkUnderline
	case "del", "s", "strike":
		tok.isMark |= model.MarkStrike
	case "code", "kbd", "samp", "tt":
		tok.isMark |= model.MarkCode
	case "mark":
		tok.style.bgColor = "yellow"
		tok.hasBG = true
	case "a":
		if href := attrVal(attrs, "href"); href != "" {
			tok.style.link = href
			tok.hasLnk = true
		}
	}
	if cssVal := cssProp(attrs, "font-weight"); isCSSBold(cssVal) {
		tok.isMark |= model.MarkBold
	}
	if strings.EqualFold(cssProp(attrs, "font-style"), "italic") {
		tok.isMark |= model.MarkItalic
	}
	if td := strings.ToLower(cssProp(attrs, "text-decoration")); td != "" {
		if strings.Contains(td, "underline") {
			tok.isMark |= model.MarkUnderline
		}
		if strings.Contains(td, "line-through") {
			tok.isMark |= model.MarkStrike
		}
	}
	if fg := attrColorValue(attrs, false); fg != "" {
		tok.style.fgColor = fg
		tok.hasFG = true
	}
	if bg := attrColorValue(attrs, true); bg != "" {
		tok.style.bgColor = bg
		tok.hasBG = true
	}
	return raw, []*inlineTok{tok}, consumed, true
}

func attrVal(attrs [][2]string, key string) string {
	for _, kv := range attrs {
		if kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

func cssProp(attrs [][2]string, prop string) string {
	styleStr := attrVal(attrs, "style")
	if styleStr == "" {
		return ""
	}
	var out string
	for _, part := range strings.Split(styleStr, ";") {
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(k), prop) {
			out = strings.TrimSpace(v)
		}
	}
	return out
}

func isCSSBold(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "bold", "600", "700", "800", "900":
		return true
	}
	return false
}

func attrColorValue(attrs [][2]string, bg bool) string {
	prop := "color"
	if bg {
		prop = "background-color"
	}
	if v := cssProp(attrs, prop); v != "" {
		return cleanColorValue(v)
	}
	if bg {
		if v := cssProp(attrs, "background"); v != "" {
			return cleanColorValue(v)
		}
	}
	htmlAttr := "color"
	if bg {
		htmlAttr = "bgcolor"
	}
	if v := attrVal(attrs, htmlAttr); v != "" {
		return cleanColorValue(v)
	}
	suffix := "text-color"
	if bg {
		suffix = "background-color"
	}
	if v := attrVal(attrs, "data-thinkly-"+suffix); v != "" {
		return cleanColorValue(v)
	}
	if v := attrVal(attrs, "data-goose-"+suffix); v != "" {
		return cleanColorValue(v)
	}
	return ""
}

func cleanColorValue(v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "default", "none", "transparent", "inherit", "initial":
		return ""
	}
	return v
}

func resolveDelimiters(toks []*inlineTok) {
	openersBottom := make(map[byte]int)
	for i := 0; i < len(toks); i++ {
		cl := toks[i]
		if cl.kind != tokDelim || !cl.canCl || cl.count == 0 {
			continue
		}
		bottom := openersBottom[cl.ch]
		foundAny := false
		for j := i - 1; j >= bottom; j-- {
			op := toks[j]
			if op.kind != tokDelim || !op.canOp || op.count == 0 || op.ch != cl.ch {
				continue
			}
			// CommonMark rule of 3 for intraword/both-flanking delimiters.
			if (op.canCl || cl.canOp) && (op.orig+cl.orig)%3 == 0 && (op.orig%3 != 0 || cl.orig%3 != 0) {
				continue
			}
			use := 1
			if cl.ch == '~' {
				if op.count < 2 || cl.count < 2 {
					continue
				}
				use = 2
			} else if op.count >= 2 && cl.count >= 2 {
				use = 2
			}
			foundAny = true
			op.count -= use
			cl.count -= use
			var mark model.Mark
			switch {
			case cl.ch == '~':
				mark = model.MarkStrike
			case use == 2:
				mark = model.MarkBold
			default:
				mark = model.MarkItalic
			}
			// Outer matches wrap inner ones: prepend on closer, append on opener.
			op.openMarks = append(op.openMarks, mark)
			cl.closeMarks = append([]model.Mark{mark}, cl.closeMarks...)
			// Deactivate unmatched inner delimiters between j and i of the same or other chars?
			// Standard CommonMark deactivates inner delimiters between j and i.
			for k := j + 1; k < i; k++ {
				if toks[k].kind == tokDelim {
					toks[k].count = 0
				}
			}
			if cl.count == 0 {
				break
			}
		}
		if !foundAny && !cl.canOp {
			openersBottom[cl.ch] = i + 1
		}
	}
}

type styleFrame struct {
	tag     string
	marks   model.Mark
	fgColor string
	bgColor string
	link    string
	hasFG   bool
	hasBG   bool
	hasLnk  bool
}

func emitSpans(toks []*inlineTok) []model.Span {
	var out []model.Span
	var markCounts [5]int // Bold, Italic, Underline, Strike, Code
	var htmlStack []styleFrame
	var linkStack []string

	addMark := func(m model.Mark, delta int) {
		if m&model.MarkBold != 0 {
			markCounts[0] += delta
		}
		if m&model.MarkItalic != 0 {
			markCounts[1] += delta
		}
		if m&model.MarkUnderline != 0 {
			markCounts[2] += delta
		}
		if m&model.MarkStrike != 0 {
			markCounts[3] += delta
		}
		if m&model.MarkCode != 0 {
			markCounts[4] += delta
		}
	}

	currentStyle := func() inlineStyle {
		var st inlineStyle
		if markCounts[0] > 0 {
			st.marks |= model.MarkBold
		}
		if markCounts[1] > 0 {
			st.marks |= model.MarkItalic
		}
		if markCounts[2] > 0 {
			st.marks |= model.MarkUnderline
		}
		if markCounts[3] > 0 {
			st.marks |= model.MarkStrike
		}
		if markCounts[4] > 0 {
			st.marks |= model.MarkCode
		}
		if len(linkStack) > 0 {
			st.link = linkStack[len(linkStack)-1]
		}
		for _, f := range htmlStack {
			if f.hasLnk && f.link != "" {
				st.link = f.link
			}
			if f.hasFG && f.fgColor != "" {
				st.fgColor = f.fgColor
			}
			if f.hasBG && f.bgColor != "" {
				st.bgColor = f.bgColor
			}
		}
		return st
	}

	appendText := func(txt string, extraMark model.Mark) {
		if txt == "" {
			return
		}
		st := currentStyle()
		st.marks |= extraMark
		out = append(out, model.Span{
			Text:    txt,
			Marks:   st.marks,
			Link:    st.link,
			FGColor: st.fgColor,
			BGColor: st.bgColor,
		})
	}

	for _, tok := range toks {
		switch tok.kind {
		case tokText:
			appendText(tok.text, 0)
		case tokCode:
			appendText(tok.text, model.MarkCode)
		case tokDelim:
			for _, m := range tok.closeMarks {
				addMark(m, -1)
			}
			if tok.count > 0 {
				appendText(strings.Repeat(string(tok.ch), tok.count), 0)
			}
			// Open marks in reverse order so the outermost pairing applies cleanly.
			for k := len(tok.openMarks) - 1; k >= 0; k-- {
				addMark(tok.openMarks[k], 1)
			}
		case tokLinkOpen:
			linkStack = append(linkStack, tok.style.link)
		case tokLinkClose:
			if len(linkStack) > 0 {
				linkStack = linkStack[:len(linkStack)-1]
			}
		case tokOpenStyle:
			addMark(tok.isMark, 1)
			htmlStack = append(htmlStack, styleFrame{
				tag:     tok.tag,
				marks:   tok.isMark,
				fgColor: tok.style.fgColor,
				bgColor: tok.style.bgColor,
				link:    tok.style.link,
				hasFG:   tok.hasFG,
				hasBG:   tok.hasBG,
				hasLnk:  tok.hasLnk,
			})
		case tokCloseStyle:
			for idx := len(htmlStack) - 1; idx >= 0; idx-- {
				if htmlStack[idx].tag == tok.tag {
					f := htmlStack[idx]
					addMark(f.marks, -1)
					htmlStack = append(htmlStack[:idx], htmlStack[idx+1:]...)
					break
				}
			}
		}
	}
	return out
}

func coalesceSpans(spans []model.Span) []model.Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]model.Span, 0, len(spans))
	for _, s := range spans {
		if s.Text == "" {
			continue
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Marks == s.Marks && last.Link == s.Link && last.FGColor == s.FGColor && last.BGColor == s.BGColor {
				last.Text += s.Text
				continue
			}
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func spansEqual(a, b []model.Span) bool {
	ca := coalesceSpans(a)
	cb := coalesceSpans(b)
	if len(ca) != len(cb) {
		return false
	}
	for i := range ca {
		if ca[i] != cb[i] {
			return false
		}
	}
	return true
}

// SerializeInline serializes rich inline spans back to Markdown (using minimal HTML
// only for styles Markdown cannot represent such as underline or colors).
func SerializeInline(spans []model.Span) string {
	norm := coalesceSpans(spans)
	if len(norm) == 0 {
		return ""
	}
	md := serializeInlineWithMode(norm, false)
	if spansEqual(ParseInline(md), norm) {
		return md
	}
	// Fallback to HTML formatting tags for non-round-tripping delimiter edge cases.
	htmlCandidate := serializeInlineWithMode(norm, true)
	if spansEqual(ParseInline(htmlCandidate), norm) {
		return htmlCandidate
	}
	return htmlCandidate
}

type inlineWrapperKind uint8

const (
	wrapLink inlineWrapperKind = iota
	wrapSpanColor
	wrapUnderline
	wrapStrike
	wrapBold
	wrapItalic
)

type inlineWrapper struct {
	kind  inlineWrapperKind
	val1  string
	val2  string
	useHT bool
}

func wrappersForSpan(s model.Span, forceHTML bool) []inlineWrapper {
	var w []inlineWrapper
	if s.Link != "" {
		w = append(w, inlineWrapper{kind: wrapLink, val1: s.Link, useHT: forceHTML})
	}
	if s.FGColor != "" || s.BGColor != "" {
		w = append(w, inlineWrapper{kind: wrapSpanColor, val1: s.FGColor, val2: s.BGColor})
	}
	if s.Marks&model.MarkUnderline != 0 {
		w = append(w, inlineWrapper{kind: wrapUnderline})
	}
	if s.Marks&model.MarkStrike != 0 {
		w = append(w, inlineWrapper{kind: wrapStrike, useHT: forceHTML})
	}
	if s.Marks&model.MarkBold != 0 {
		w = append(w, inlineWrapper{kind: wrapBold, useHT: forceHTML})
	}
	if s.Marks&model.MarkItalic != 0 {
		w = append(w, inlineWrapper{kind: wrapItalic, useHT: forceHTML})
	}
	return w
}

func serializeInlineWithMode(spans []model.Span, forceHTML bool) string {
	var out strings.Builder
	var opened []inlineWrapper

	writeOpen := func(w inlineWrapper) {
		switch w.kind {
		case wrapLink:
			if w.useHT {
				out.WriteString(`<a href="`)
				out.WriteString(escapeAttrValue(w.val1))
				out.WriteString(`">`)
			} else {
				out.WriteByte('[')
			}
		case wrapSpanColor:
			out.WriteString(`<span style="`)
			if w.val1 != "" {
				out.WriteString("color:")
				out.WriteString(escapeAttrValue(w.val1))
			}
			if w.val2 != "" {
				if w.val1 != "" {
					out.WriteByte(';')
				}
				out.WriteString("background-color:")
				out.WriteString(escapeAttrValue(w.val2))
			}
			out.WriteString(`">`)
		case wrapUnderline:
			out.WriteString("<u>")
		case wrapStrike:
			if w.useHT {
				out.WriteString("<del>")
			} else {
				out.WriteString("~~")
			}
		case wrapBold:
			if w.useHT {
				out.WriteString("<strong>")
			} else {
				out.WriteString("**")
			}
		case wrapItalic:
			if w.useHT {
				out.WriteString("<em>")
			} else {
				out.WriteByte('*')
			}
		}
	}

	writeClose := func(w inlineWrapper) {
		switch w.kind {
		case wrapLink:
			if w.useHT {
				out.WriteString("</a>")
			} else {
				out.WriteString("](")
				out.WriteString(formatLinkDest(w.val1))
				out.WriteByte(')')
			}
		case wrapSpanColor:
			out.WriteString("</span>")
		case wrapUnderline:
			out.WriteString("</u>")
		case wrapStrike:
			if w.useHT {
				out.WriteString("</del>")
			} else {
				out.WriteString("~~")
			}
		case wrapBold:
			if w.useHT {
				out.WriteString("</strong>")
			} else {
				out.WriteString("**")
			}
		case wrapItalic:
			if w.useHT {
				out.WriteString("</em>")
			} else {
				out.WriteByte('*')
			}
		}
	}

	for _, sp := range spans {
		// Inside Markdown delimiters, spaces at boundary or empty/broken delimiters fail CommonMark.
		useHTML := forceHTML
		if !useHTML && sp.Marks&(model.MarkBold|model.MarkItalic|model.MarkStrike) != 0 {
			if strings.HasPrefix(sp.Text, " ") || strings.HasSuffix(sp.Text, " ") || strings.ContainsRune(sp.Text, '\n') {
				useHTML = true
			}
		}
		target := wrappersForSpan(sp, useHTML)
		common := 0
		for common < len(opened) && common < len(target) && opened[common] == target[common] {
			common++
		}
		for k := len(opened) - 1; k >= common; k-- {
			writeClose(opened[k])
		}
		for k := common; k < len(target); k++ {
			writeOpen(target[k])
		}
		opened = target

		if sp.Marks&model.MarkCode != 0 {
			writeInlineCode(&out, sp.Text)
		} else {
			writeInlineText(&out, sp.Text, len(opened) > 0 && opened[0].kind == wrapLink && !opened[0].useHT, useHTML)
		}
	}
	for k := len(opened) - 1; k >= 0; k-- {
		writeClose(opened[k])
	}
	return simplifyInlineEscapes(out.String(), spans)
}

func writeInlineCode(out *strings.Builder, text string) {
	longest := 0
	run := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	out.WriteString(fence)
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		out.WriteByte(' ')
		out.WriteString(text)
		out.WriteByte(' ')
	} else {
		out.WriteString(text)
	}
	out.WriteString(fence)
}

func writeInlineText(out *strings.Builder, text string, inMarkdownLink, htmlCtx bool) {
	for i := 0; i < len(text); {
		r, sz := utf8.DecodeRuneInString(text[i:])
		switch r {
		case '\n':
			out.WriteString("<br>")
		case '&':
			if htmlCtx {
				out.WriteString("&amp;")
			} else if _, _, ok := tryDecodeEntity(text[i:]); ok {
				out.WriteString(`\&`)
			} else {
				out.WriteByte('&')
			}
		case '<':
			if htmlCtx {
				out.WriteString("&lt;")
			} else {
				out.WriteString(`\<`)
			}
		case '>':
			if htmlCtx {
				out.WriteString("&gt;")
			} else {
				out.WriteByte('>')
			}
		case '\\', '`', '*', '~':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '[', ']':
			if inMarkdownLink || textContainsLinkSyntax(text) {
				out.WriteByte('\\')
			}
			out.WriteRune(r)
		default:
			out.WriteRune(r)
		}
		i += sz
	}
}

func textContainsLinkSyntax(s string) bool {
	return strings.ContainsRune(s, '[') && (strings.Contains(s, "](") || strings.Contains(s, "]["))
}

func simplifyInlineEscapes(src string, expected []model.Span) string {
	if !strings.ContainsRune(src, '\\') {
		return src
	}
	exp := coalesceSpans(expected)
	for _, ch := range []string{"~", "*", "[", "]", "`", "<", "&", "\\"} {
		esc := `\` + ch
		if !strings.Contains(src, esc) {
			continue
		}
		cand := strings.ReplaceAll(src, esc, ch)
		if spansEqual(ParseInline(cand), exp) {
			src = cand
		}
	}
	return src
}

func formatLinkDest(dest string) string {
	needAngle := false
	for i := 0; i < len(dest); i++ {
		c := dest[i]
		if c <= ' ' || c == '<' || c == '>' || c == '(' || c == ')' {
			needAngle = true
			break
		}
	}
	if needAngle {
		escaped := strings.ReplaceAll(dest, "<", "%3C")
		escaped = strings.ReplaceAll(escaped, ">", "%3E")
		return "<" + escaped + ">"
	}
	return dest
}

func escapeAttrValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizePasteText cleans clipboard / multi-line / fullwidth CJK & Chaoshan
// punctuation and entity-escaped Markdown markers into clean Markdown source.
func NormalizePasteText(raw string) string {
	if raw == "" {
		return ""
	}
	// 1. Strip UTF-8 BOM & zero-width artifacts, normalize line endings and NBSP.
	s := strings.TrimPrefix(raw, "\uFEFF")
	s = strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"\u200B", "",
		"\u200C", "",
		"\u200D", "",
		"\uFEFF", "",
		"\u00A0", " ",
	).Replace(s)

	// 2. Recover entity-escaped Markdown pasted from rich HTML views (e.g. `&#35;`, `&#45;`, `&#42;`, `&#47;`, `&#124;`).
	if looksLikeEntityEscapedMarkdown(s) {
		s = recoverEntityEscapedMarkdown(s)
	}

	// 3. Process line by line, keeping fenced code blocks verbatim except line-ending cleanup.
	lines := strings.Split(s, "\n")
	outLines := make([]string, 0, len(lines))
	inFence := false
	var fenceChar byte
	var fenceLen int

	for _, line := range lines {
		if inFence {
			if closesFenceAnyIndent(line, fenceChar, fenceLen) {
				inFence = false
			}
			outLines = append(outLines, line)
			continue
		}

		normLine := normalizePastedLine(line)
		if _, _, fc, fl, _, ok := parseFenceAnyIndent(normLine); ok {
			inFence = true
			fenceChar = fc
			fenceLen = fl
		}
		outLines = append(outLines, normLine)
	}

	// 4. Collapse 3+ consecutive blank lines outside code blocks and trim leading/trailing blank lines.
	collapsed := collapseExcessBlankLines(outLines)
	return strings.Join(collapsed, "\n")
}

func looksLikeEntityEscapedMarkdown(s string) bool {
	if !strings.ContainsRune(s, '&') {
		return false
	}
	for _, needle := range []string{"&#35;", "&#45;", "&#42;", "&#47;", "&#124;", "&#96;", "&#62;"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimLeft(line, " \t\u3000")
		if strings.HasPrefix(trimmed, "&gt; ") || strings.HasPrefix(trimmed, "&gt;\t") || trimmed == "&gt;" {
			return true
		}
	}
	return false
}

var mdEntityReplacer = strings.NewReplacer(
	"&#35;", "#",
	"&#x23;", "#",
	"&#45;", "-",
	"&#x2d;", "-",
	"&#x2D;", "-",
	"&#42;", "*",
	"&#x2a;", "*",
	"&#x2A;", "*",
	"&#47;", "/",
	"&#x2f;", "/",
	"&#x2F;", "/",
	"&#124;", "|",
	"&#x7c;", "|",
	"&#x7C;", "|",
	"&#96;", "`",
	"&#x60;", "`",
	"&#91;", "[",
	"&#93;", "]",
	"&#95;", "_",
	"&#126;", "~",
	"&#62;", ">",
	"&#x3e;", ">",
	"&#x3E;", ">",
	"&#32;", " ",
	"&#9;", "\t",
	"&gt;", ">",
)

func recoverEntityEscapedMarkdown(s string) string {
	return mdEntityReplacer.Replace(s)
}

func normalizePastedLine(line string) string {
	// Trim trailing spaces/tabs/fullwidth spaces on whitespace-only lines.
	if strings.Trim(line, " \t\u3000") == "" {
		return ""
	}
	// Preserve intentional 2-space hard breaks, otherwise trim trailing horizontal whitespace.
	hasHardBreak := strings.HasSuffix(line, "  ")
	line = strings.TrimRight(line, " \t\u3000")
	if hasHardBreak {
		line += "  "
	}

	// Normalize leading fullwidth spaces before structural markers.
	leadSpaces := 0
	pos := 0
	for pos < len(line) {
		r, sz := utf8.DecodeRuneInString(line[pos:])
		if r == ' ' {
			leadSpaces++
			pos += sz
		} else if r == '\t' {
			leadSpaces += 4
			pos += sz
		} else if r == '\u3000' {
			leadSpaces += 2
			pos += sz
		} else {
			break
		}
	}
	rest := line[pos:]
	prefix := strings.Repeat(" ", leadSpaces)

	// Normalize fullwidth fence markers (｀｀｀).
	if strings.HasPrefix(rest, "｀｀｀") {
		rest = "```" + strings.TrimLeft(rest[len("｀｀｀"):], " \u3000")
		return prefix + rest
	}

	// Normalize fullwidth headings: ＃ / ＃＃ ...
	if strings.HasPrefix(rest, "＃") || strings.HasPrefix(rest, "#") {
		hashes := 0
		rPos := 0
		for rPos < len(rest) {
			r, sz := utf8.DecodeRuneInString(rest[rPos:])
			if r == '#' || r == '＃' {
				hashes++
				rPos += sz
			} else {
				break
			}
		}
		if hashes >= 1 && hashes <= 6 {
			after := rest[rPos:]
			if after == "" {
				return strings.Repeat("#", hashes)
			}
			if strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t") || strings.HasPrefix(after, "\u3000") {
				return strings.Repeat("#", hashes) + " " + normalizeFullwidthInlineDelims(strings.TrimLeft(after, " \t\u3000"))
			}
			if strings.HasPrefix(rest, "＃") {
				return strings.Repeat("#", hashes) + " " + normalizeFullwidthInlineDelims(after)
			}
		}
	}

	// Normalize fullwidth blockquote marker: ＞ or >　
	if strings.HasPrefix(rest, "＞") {
		after := strings.TrimLeft(rest[len("＞"):], " \t\u3000")
		if after == "" {
			return prefix + ">"
		}
		return prefix + "> " + normalizeFullwidthInlineDelims(after)
	}
	if strings.HasPrefix(rest, ">\u3000") {
		return prefix + "> " + normalizeFullwidthInlineDelims(strings.TrimLeft(rest[1:], " \t\u3000"))
	}

	// Normalize fullwidth divider: ——— or －－－ or ＊＊＊
	compactDiv := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\u3000':
			return -1
		case '－', '—':
			return '-'
		case '＊':
			return '*'
		case '＿':
			return '_'
		default:
			return r
		}
	}, rest)
	if len(compactDiv) >= 3 && (strings.Trim(compactDiv, "-") == "" || strings.Trim(compactDiv, "*") == "" || strings.Trim(compactDiv, "_") == "") {
		// Only convert if original used fullwidth dashes/stars/underscores or standard divider.
		if strings.ContainsAny(rest, "－—＊＿") {
			return prefix + "---"
		}
	}

	// Normalize fullwidth bullet markers & common CJK bullet characters (`•`, `·`, `▪`, `●`, `－`, `＊`).
	for _, bullet := range []string{"－", "＊", "•", "·", "▪", "●", "-", "*", "+"} {
		if strings.HasPrefix(rest, bullet) {
			after := rest[len(bullet):]
			if after == "" {
				if bullet == "-" || bullet == "*" || bullet == "+" {
					return prefix + bullet
				}
				return prefix + "-"
			}
			if strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t") || strings.HasPrefix(after, "\u3000") {
				trimmedAfter := strings.TrimLeft(after, " \t\u3000")
				normalizedAfter := normalizeFullwidthInlineDelims(normalizeTodoPrefix(trimmedAfter))
				if bullet == "－" || bullet == "•" || bullet == "·" || bullet == "▪" || bullet == "●" {
					return prefix + "- " + normalizedAfter
				}
				if bullet == "＊" {
					return prefix + "* " + normalizedAfter
				}
				return prefix + bullet + " " + normalizedAfter
			}
			// For explicit CJK bullet symbols like `•内容`, insert space automatically.
			if bullet == "•" || bullet == "▪" || bullet == "●" {
				return prefix + "- " + normalizeFullwidthInlineDelims(normalizeTodoPrefix(after))
			}
		}
	}

	// Normalize fullwidth ordered list markers: `１．内容`, `1．内容`, `１、内容`, `１）内容`.
	if ordLine, ok := normalizeOrderedMarker(rest); ok {
		return prefix + normalizeFullwidthInlineDelims(ordLine)
	}

	// Normalize fullwidth pipe table lines: `｜ 餐别 ｜ 店名 ｜` -> `| 餐别 | 店名 |`.
	if strings.ContainsRune(rest, '｜') && (strings.HasPrefix(rest, "｜") || strings.HasPrefix(rest, "|")) {
		rest = strings.ReplaceAll(rest, "｜", "|")
	}

	// Normalize paired fullwidth bold/code markers inside line (`＊＊...＊＊` -> `**...**`, `｀code｀` -> `` `code` ``).
	rest = normalizeFullwidthInlineDelims(rest)
	return prefix + rest
}

func normalizeTodoPrefix(s string) string {
	for _, pair := range [][2]string{
		{"［ ］", "[ ]"},
		{"［　］", "[ ]"},
		{"［］", "[ ]"},
		{"[]", "[ ]"},
		{"[　]", "[ ]"},
		{"［x］", "[x]"},
		{"［X］", "[x]"},
		{"［ｘ］", "[x]"},
		{"［Ｘ］", "[x]"},
		{"[ｘ]", "[x]"},
		{"[Ｘ]", "[x]"},
	} {
		if strings.HasPrefix(s, pair[0]) {
			after := strings.TrimLeft(s[len(pair[0]):], " \t\u3000")
			if after == "" {
				return pair[1]
			}
			return pair[1] + " " + after
		}
	}
	return s
}

func normalizeOrderedMarker(rest string) (string, bool) {
	var digits strings.Builder
	pos := 0
	for pos < len(rest) && digits.Len() < 9 {
		r, sz := utf8.DecodeRuneInString(rest[pos:])
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			pos += sz
		} else if r >= '０' && r <= '９' {
			digits.WriteByte(byte('0' + (r - '０')))
			pos += sz
		} else {
			break
		}
	}
	if digits.Len() == 0 || pos >= len(rest) {
		return "", false
	}
	r, sz := utf8.DecodeRuneInString(rest[pos:])
	if r != '.' && r != '．' && r != '、' && r != ')' && r != '）' {
		return "", false
	}
	after := rest[pos+sz:]
	sep := ". "
	if r == ')' || r == '）' {
		sep = ") "
	}
	if after == "" {
		return digits.String() + strings.TrimSpace(sep), true
	}
	if strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t") || strings.HasPrefix(after, "\u3000") {
		return digits.String() + sep + strings.TrimLeft(after, " \t\u3000"), true
	}
	if r == '．' || r == '、' || r == '）' {
		return digits.String() + sep + after, true
	}
	return "", false
}

func normalizeFullwidthInlineDelims(s string) string {
	if !strings.ContainsRune(s, '＊') && !strings.ContainsRune(s, '｀') {
		return s
	}
	s = strings.ReplaceAll(s, "＊＊", "**")
	s = strings.ReplaceAll(s, "｀", "`")
	return s
}

func collapseExcessBlankLines(lines []string) []string {
	start := 0
	for start < len(lines) && lines[start] == "" {
		start++
	}
	end := len(lines)
	for end > start && lines[end-1] == "" {
		end--
	}
	if start >= end {
		return nil
	}
	out := make([]string, 0, end-start)
	inFence := false
	var fc byte
	var fl int
	blankRun := 0
	for i := start; i < end; i++ {
		line := lines[i]
		if inFence {
			if closesFenceAnyIndent(line, fc, fl) {
				inFence = false
			}
			out = append(out, line)
			continue
		}
		if _, _, c, l, _, ok := parseFenceAnyIndent(line); ok {
			inFence = true
			fc = c
			fl = l
			blankRun = 0
			out = append(out, line)
			continue
		}
		if line == "" {
			blankRun++
			if blankRun <= 1 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return out
}

