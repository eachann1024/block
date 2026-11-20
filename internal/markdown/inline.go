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
	// Ordinary punctuation is literal. Only scan runs that can contain an
	// inline construct or a bare HTTP(S) link.
	if !strings.ContainsAny(raw, "\\`*_~![<&$^=") && !strings.Contains(raw, "://") {
		return []model.Span{{Text: raw}}
	}
	toks := scanInlineTokensWithCtx(raw, defs, false)
	resolveDelimiters(toks)
	spans := emitSpans(toks)
	return coalesceSpans(spans)
}

type tokKind uint8

const (
	tokText tokKind = iota
	tokCode
	tokMath
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
	return scanInlineTokensWithCtx(s, defs, false)
}

type inlineScanState struct {
	s                     string
	unclosedBacktickMask  uint64
	unclosedBacktickLarge map[int]bool
	lastCloseBracket      int
	lastCloseParen        int
	lastGT                int
	lastDQ                int
	lastSQ                int
	noCommentCloseFrom    int
	unclosedBrackets      map[int]bool
	unclosedParens        map[int]bool
	tagFailWatermark      int
}

func newInlineScanState(s string) *inlineScanState {
	return &inlineScanState{
		s:                  s,
		lastCloseBracket:   -2,
		lastCloseParen:     -2,
		lastGT:             -2,
		lastDQ:             -2,
		lastSQ:             -2,
		noCommentCloseFrom: -1,
		tagFailWatermark:   -1,
	}
}

func (st *inlineScanState) findClosingBackticks(from, count int) int {
	if count > 0 && count < 64 {
		if (st.unclosedBacktickMask & (uint64(1) << count)) != 0 {
			return -1
		}
	} else if st.unclosedBacktickLarge != nil && st.unclosedBacktickLarge[count] {
		return -1
	}
	end := findClosingBackticks(st.s, from, count)
	if end < 0 {
		if count > 0 && count < 64 {
			st.unclosedBacktickMask |= uint64(1) << count
		} else {
			if st.unclosedBacktickLarge == nil {
				st.unclosedBacktickLarge = make(map[int]bool)
			}
			st.unclosedBacktickLarge[count] = true
		}
	}
	return end
}

func (st *inlineScanState) getLastCloseBracket() int {
	if st.lastCloseBracket == -2 {
		st.lastCloseBracket = strings.LastIndexByte(st.s, ']')
	}
	return st.lastCloseBracket
}

func (st *inlineScanState) getLastCloseParen() int {
	if st.lastCloseParen == -2 {
		st.lastCloseParen = strings.LastIndexByte(st.s, ')')
	}
	return st.lastCloseParen
}

func (st *inlineScanState) getTagBounds() (int, int, int) {
	if st.lastGT == -2 {
		st.lastGT = strings.LastIndexByte(st.s, '>')
	}
	if st.lastDQ == -2 {
		st.lastDQ = strings.LastIndexByte(st.s, '"')
	}
	if st.lastSQ == -2 {
		st.lastSQ = strings.LastIndexByte(st.s, '\'')
	}
	return st.lastGT, st.lastDQ, st.lastSQ
}

func scanInlineTokensWithCtx(s string, defs map[string]refDef, inLink bool) []*inlineTok {
	var toks []*inlineTok
	pairedUnknown := scanPairedUnknownTags(s)
	st := newInlineScanState(s)
	htmlLinkDepth := 0
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
			end := st.findClosingBackticks(i+run, run)
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
			if b == '~' && run == 1 {
				if end, inner, ok := findDelimNoSpaceEnd(s, i+1, '~'); ok {
					toks = append(toks,
						&inlineTok{kind: tokOpenStyle, tag: "sub", isMark: model.MarkSubscript},
						&inlineTok{kind: tokText, text: inner},
						&inlineTok{kind: tokCloseStyle, tag: "sub"},
					)
					i = end + 1
					continue
				}
				toks = append(toks, &inlineTok{kind: tokText, text: "~"})
				i++
				continue
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

		case '^':
			run := 0
			for i+run < n && s[i+run] == '^' {
				run++
			}
			if run == 1 {
				if end, inner, ok := findDelimNoSpaceEnd(s, i+1, '^'); ok {
					toks = append(toks,
						&inlineTok{kind: tokOpenStyle, tag: "sup", isMark: model.MarkSuperscript},
						&inlineTok{kind: tokText, text: inner},
						&inlineTok{kind: tokCloseStyle, tag: "sup"},
					)
					i = end + 1
					continue
				}
			}
			toks = append(toks, &inlineTok{kind: tokText, text: s[i : i+run]})
			i += run

		case '$':
			run := 0
			for i+run < n && s[i+run] == '$' {
				run++
			}
			if run == 1 {
				if end, inner, ok := findInlineMathEnd(s, i+1); ok {
					toks = append(toks, &inlineTok{kind: tokMath, text: inner})
					i = end + 1
					continue
				}
			}
			toks = append(toks, &inlineTok{kind: tokText, text: s[i : i+run]})
			i += run

		case '=':
			run := 0
			for i+run < n && s[i+run] == '=' {
				run++
			}
			if run == 2 {
				if end, ok := findDoubleEqualEnd(s, i+2); ok {
					innerToks := scanInlineTokensWithCtx(s[i+2:end], defs, inLink)
					resolveDelimiters(innerToks)
					toks = append(toks, &inlineTok{
						kind:  tokOpenStyle,
						tag:   "mark",
						style: inlineStyle{bgColor: "yellow"},
						hasBG: true,
					})
					toks = append(toks, innerToks...)
					toks = append(toks, &inlineTok{kind: tokCloseStyle, tag: "mark"})
					i = end + 2
					continue
				}
			}
			toks = append(toks, &inlineTok{kind: tokText, text: s[i : i+run]})
			i += run

		case '!':
			if i+1 < n && s[i+1] == '[' && i+1 < st.getLastCloseBracket() {
				if alt, dest, consumed, ok := tryParseLinkOrImageAt(st, i+1, defs, true); ok {
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
			lastCB := st.getLastCloseBracket()
			if i+2 < n && s[i+1] == '^' && i+2 <= lastCB {
				if closeFn := strings.IndexByte(s[i+2:lastCB+1], ']'); closeFn > 0 {
					fnLabel := s[i+2 : i+2+closeFn]
					afterFn := i + 2 + closeFn + 1
					if !strings.ContainsAny(fnLabel, " \t\r\n[]") && (afterFn >= n || s[afterFn] != '(') {
						toks = append(toks, &inlineTok{kind: tokText, text: s[i:afterFn]})
						i = afterFn
						continue
					}
				}
			}
			if i < lastCB && (st.unclosedBrackets == nil || !st.unclosedBrackets[i]) {
				if label, dest, consumed, ok := tryParseLinkOrImageAt(st, i, defs, false); ok {
					toks = append(toks, &inlineTok{kind: tokLinkOpen, style: inlineStyle{link: dest}})
					innerToks := scanInlineTokensWithCtx(label, defs, true)
					resolveDelimiters(innerToks)
					toks = append(toks, innerToks...)
					toks = append(toks, &inlineTok{kind: tokLinkClose})
					i += consumed
					continue
				}
			}
			toks = append(toks, &inlineTok{kind: tokText, text: "["})
			i++

		case '<':
			if strings.HasPrefix(s[i:], "<!--") {
				if st.noCommentCloseFrom < 0 || i+4 < st.noCommentCloseFrom {
					if end := strings.Index(s[i+4:], "-->"); end >= 0 {
						i += 4 + end + 3
						continue
					}
					st.noCommentCloseFrom = i + 4
				}
			}
			lastGT, lastDQ, lastSQ := st.getTagBounds()
			if i < lastGT {
				if addr, linkURL, consumed, ok := tryParseAutolink(s[i : lastGT+1]); ok {
					toks = append(toks, &inlineTok{kind: tokLinkOpen, style: inlineStyle{link: linkURL}})
					toks = append(toks, &inlineTok{kind: tokText, text: addr})
					toks = append(toks, &inlineTok{kind: tokLinkClose})
					i += consumed
					continue
				}
				if i >= st.tagFailWatermark {
					if rawTag, tok, consumed, failPos, sawQuoteOrGT, ok := tryParseHTMLTagAt(s, i, lastGT, lastDQ, lastSQ, pairedUnknown); ok {
						// Literal HTML inline math: `<span class="math">...</span>` or `<code class="math">...</code>`.
						if len(tok) == 1 && tok[0].kind == tokOpenStyle && tok[0].isMark&model.MarkMath != 0 {
							if litText, closeEnd, okLit := scanLiteralHTMLMathBody(s, i+consumed, tok[0].tag); okLit {
								toks = append(toks, &inlineTok{kind: tokMath, text: litText})
								i = closeEnd
								continue
							}
						}
						if tok != nil {
							for _, t := range tok {
								if t.kind == tokOpenStyle && t.tag == "a" && t.hasLnk {
									htmlLinkDepth++
								} else if t.kind == tokCloseStyle && t.tag == "a" && htmlLinkDepth > 0 {
									htmlLinkDepth--
								}
							}
							toks = append(toks, tok...)
						} else {
							toks = append(toks, &inlineTok{kind: tokText, text: rawTag})
						}
						i += consumed
						continue
					} else if !sawQuoteOrGT && failPos > st.tagFailWatermark {
						st.tagFailWatermark = failPos
					}
				}
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
			if !inLink && htmlLinkDepth == 0 && (b == 'h' || b == 'H') {
				if disp, linkURL, consumed, ok := tryParseBareAutolink(s, i); ok {
					toks = append(toks,
						&inlineTok{kind: tokLinkOpen, style: inlineStyle{link: linkURL}},
						&inlineTok{kind: tokText, text: disp},
						&inlineTok{kind: tokLinkClose},
					)
					i += consumed
					continue
				}
			}
			start := i
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
			for i < n {
				nb := s[i]
				if nb == '\\' || nb == '`' || nb == '*' || nb == '_' || nb == '~' || nb == '!' || nb == '[' || nb == '<' || nb == '&' || nb == '^' || nb == '$' || nb == '=' {
					break
				}
				if !inLink && htmlLinkDepth == 0 && (nb == 'h' || nb == 'H') {
					if _, _, _, ok := tryParseBareAutolink(s, i); ok {
						break
					}
				}
				_, sz := utf8.DecodeRuneInString(s[i:])
				i += sz
			}
			toks = append(toks, &inlineTok{kind: tokText, text: s[start:i]})
		}
	}
	return toks
}

func tryParseBareAutolink(s string, start int) (display, linkURL string, consumed int, ok bool) {
	rem := s[start:]
	schemeLen := 0
	if len(rem) > 8 && strings.EqualFold(rem[:8], "https://") {
		schemeLen = 8
	} else if len(rem) > 7 && strings.EqualFold(rem[:7], "http://") {
		schemeLen = 7
	} else {
		return "", "", 0, false
	}

	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(s[:start])
		if isASCIIAlpha(byte(prev)) && prev < 0x80 {
			return "", "", 0, false
		}
		if (prev >= '0' && prev <= '9') || prev == '/' || prev == '.' || prev == '-' || prev == '_' || prev == '@' {
			return "", "", 0, false
		}
	}

	end := start + schemeLen
	for end < len(s) {
		r, sz := utf8.DecodeRuneInString(s[end:])
		if r <= 0x20 || r >= 0x7F || r == '<' || r == '>' || r == '"' || r == '`' {
			break
		}
		end += sz
	}

	// Strip GFM trailing punctuation & unmatched closing brackets/parens.
	for end > start+schemeLen {
		last := s[end-1]
		switch last {
		case '.', ',', ';', ':', '!', '?', '\'', '*', '_', '~':
			end--
			continue
		case ')':
			body := s[start+schemeLen : end]
			if strings.Count(body, ")") > strings.Count(body, "(") {
				end--
				continue
			}
		case ']':
			body := s[start+schemeLen : end]
			if strings.Count(body, "]") > strings.Count(body, "[") {
				end--
				continue
			}
		}
		break
	}

	if end <= start+schemeLen {
		return "", "", 0, false
	}
	rest := s[start+schemeLen : end]
	host := rest
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		host = rest[:cut]
	}
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", "", 0, false
	}
	hasAlphaNum := false
	for i := 0; i < len(host); i++ {
		c := host[i]
		if isASCIIAlpha(c) || (c >= '0' && c <= '9') {
			hasAlphaNum = true
			break
		}
	}
	if !hasAlphaNum {
		return "", "", 0, false
	}
	if !strings.ContainsRune(host, '.') && !strings.ContainsRune(host, ':') && !strings.EqualFold(host, "localhost") {
		return "", "", 0, false
	}

	raw := s[start:end]
	normURL := strings.ToLower(raw[:schemeLen]) + raw[schemeLen:]
	return raw, normURL, end - start, true
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

func isInlineSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func findDelimNoSpaceEnd(s string, from int, ch byte) (end int, inner string, ok bool) {
	n := len(s)
	if from >= n {
		return -1, "", false
	}
	var buf strings.Builder
	for i := from; i < n; i++ {
		c := s[i]
		if isInlineSpaceByte(c) {
			return -1, "", false
		}
		if c == '\\' && i+1 < n && (s[i+1] == ch || s[i+1] == '\\') {
			buf.WriteByte(s[i+1])
			i++
			continue
		}
		if c == ch {
			if i == from || (i+1 < n && s[i+1] == ch) {
				return -1, "", false
			}
			return i, buf.String(), true
		}
		buf.WriteByte(c)
	}
	return -1, "", false
}

func findInlineMathEnd(s string, from int) (end int, inner string, ok bool) {
	n := len(s)
	if from >= n || isInlineSpaceByte(s[from]) || s[from] == '$' {
		return -1, "", false
	}
	var buf strings.Builder
	for i := from; i < n; i++ {
		c := s[i]
		if c == '\n' || c == '\r' {
			return -1, "", false
		}
		if c == '\\' && i+1 < n && (s[i+1] == '$' || s[i+1] == '\\') {
			buf.WriteByte(s[i+1])
			i++
			continue
		}
		if c == '$' {
			if i == from || isInlineSpaceByte(s[i-1]) {
				return -1, "", false
			}
			if i+1 < n && (s[i+1] == '$' || (s[i+1] >= '0' && s[i+1] <= '9')) {
				return -1, "", false
			}
			return i, buf.String(), true
		}
		buf.WriteByte(c)
	}
	return -1, "", false
}

func findDoubleEqualEnd(s string, from int) (end int, ok bool) {
	n := len(s)
	if from >= n || isInlineSpaceByte(s[from]) || s[from] == '=' {
		return -1, false
	}
	for i := from; i < n; {
		c := s[i]
		if c == '\n' || c == '\r' {
			return -1, false
		}
		if c == '\\' && i+1 < n {
			i += 2
			continue
		}
		if c == '`' {
			run := 0
			for i+run < n && s[i+run] == '`' {
				run++
			}
			if closeBT := findClosingBackticks(s, i+run, run); closeBT >= 0 {
				i = closeBT + run
				continue
			}
			i += run
			continue
		}
		if c == '$' {
			run := 0
			for i+run < n && s[i+run] == '$' {
				run++
			}
			if run == 1 {
				if closeM, _, okM := findInlineMathEnd(s, i+1); okM {
					i = closeM + 1
					continue
				}
			}
			i += run
			continue
		}
		if c == '[' {
			if _, _, consumed, okL := tryParseLinkOrImage(s[i:], nil, false); okL && consumed > 0 {
				i += consumed
				continue
			}
		}
		if c == '=' && i+1 < n && s[i+1] == '=' {
			if i > from && !isInlineSpaceByte(s[i-1]) && s[i-1] != '=' && (i+2 >= n || s[i+2] != '=') {
				return i, true
			}
			for i < n && s[i] == '=' {
				i++
			}
			continue
		}
		i++
	}
	return -1, false
}

func scanLiteralHTMLMathBody(s string, from int, tag string) (text string, nextPos int, ok bool) {
	closeTag := "</" + tag + ">"
	n := len(s)
	for i := from; i < n; i++ {
		if s[i] == '<' && i+len(closeTag) <= n && strings.EqualFold(s[i:i+len(closeTag)], closeTag) {
			raw := s[from:i]
			var out strings.Builder
			out.Grow(len(raw))
			for j := 0; j < len(raw); {
				if raw[j] == '<' {
					if j+4 <= len(raw) && strings.EqualFold(raw[j:j+4], "<br>") {
						out.WriteByte('\n')
						j += 4
						continue
					}
					if j+5 <= len(raw) && strings.EqualFold(raw[j:j+5], "<br/>") {
						out.WriteByte('\n')
						j += 5
						continue
					}
					if j+6 <= len(raw) && strings.EqualFold(raw[j:j+6], "<br />") {
						out.WriteByte('\n')
						j += 6
						continue
					}
				}
				if raw[j] == '&' {
					if decoded, consumed, okEnt := tryDecodeEntity(raw[j:]); okEnt {
						out.WriteString(decoded)
						j += consumed
						continue
					}
				}
				out.WriteByte(raw[j])
				j++
			}
			return out.String(), i + len(closeTag), true
		}
	}
	return "", from, false
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
	return tryParseLinkOrImageAt(newInlineScanState(s), 0, defs, isImage)
}

func tryParseLinkOrImageAt(st *inlineScanState, start int, defs map[string]refDef, isImage bool) (label, dest string, consumed int, ok bool) {
	s := st.s
	lastCB := st.getLastCloseBracket()
	if start >= lastCB || len(s)-start < 2 || s[start] != '[' {
		return "", "", 0, false
	}
	var openBuf [16]int
	openStack := openBuf[:1]
	openStack[0] = start
	i := start + 1
	limit := lastCB + 1
	for i < limit {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '`':
			run := 0
			for i+run < len(s) && s[i+run] == '`' {
				run++
			}
			if end := st.findClosingBackticks(i+run, run); end >= 0 {
				i = end + run
				continue
			}
			i += run
			continue
		case '[':
			if !isImage {
				openStack = append(openStack, i)
			}
		case ']':
			openStack = openStack[:len(openStack)-1]
			if len(openStack) == 0 {
				goto foundClose
			}
		}
		i++
	}
	if !isImage && len(openStack) > 0 {
		if st.unclosedBrackets == nil {
			st.unclosedBrackets = make(map[int]bool, len(openStack))
		}
		for _, pos := range openStack {
			st.unclosedBrackets[pos] = true
		}
	}
	return "", "", 0, false

foundClose:
	label = s[start+1 : i]
	after := i + 1
	if after < len(s) && s[after] == '(' {
		if after < st.getLastCloseParen() && (st.unclosedParens == nil || !st.unclosedParens[after]) {
			if d, endParen, ok := parseLinkDestinationAt(st, after); ok {
				return label, d, (after - start) + endParen, true
			}
		}
	}
	if len(defs) > 0 {
		if after < len(s) && s[after] == '[' && after+1 <= lastCB {
			if closeRef := findUnescapedByte(s[after+1:lastCB+1], ']'); closeRef >= 0 {
				refLabel := s[after+1 : after+1+closeRef]
				key := normRefLabel(refLabel)
				if key == "" {
					key = normRefLabel(label)
				}
				if def, exists := defs[key]; exists {
					return label, def.dest, (after - start) + 1 + closeRef + 1, true
				}
			}
		}
		key := normRefLabel(label)
		if def, exists := defs[key]; exists {
			return label, def.dest, after - start, true
		}
	}
	return "", "", 0, false
}

func parseLinkDestinationAt(st *inlineScanState, start int) (dest string, consumed int, ok bool) {
	s := st.s
	lastCP := st.getLastCloseParen()
	if start >= lastCP || len(s)-start < 2 || s[start] != '(' {
		return "", 0, false
	}
	i := start + 1
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	if i >= len(s) {
		return "", 0, false
	}
	if s[i] == ')' {
		return "", i + 1 - start, true
	}
	var rawDest string
	if s[i] == '<' {
		i++
		urlStart := i
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
		rawDest = unescapeLinkText(s[urlStart:i])
		i++
	} else {
		urlStart := i
		var parenBuf [16]int
		parenStack := parenBuf[:1]
		parenStack[0] = start
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
				parenStack = append(parenStack, i)
			} else if c == ')' {
				if len(parenStack) == 1 {
					break
				}
				parenStack = parenStack[:len(parenStack)-1]
			}
			i++
		}
		if len(parenStack) > 1 {
			if st.unclosedParens == nil {
				st.unclosedParens = make(map[int]bool, len(parenStack))
			}
			for _, p := range parenStack {
				st.unclosedParens[p] = true
			}
			return "", 0, false
		}
		rawDest = unescapeLinkText(s[urlStart:i])
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
	return html.UnescapeString(rawDest), i + 1 - start, true
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
	lastGT := strings.LastIndexByte(s, '>')
	if lastGT < 0 {
		return nil
	}
	firstLT := strings.IndexByte(s[:lastGT], '<')
	if firstLT < 0 {
		return nil
	}
	lastDQ := strings.LastIndexByte(s, '"')
	lastSQ := strings.LastIndexByte(s, '\'')
	var counts map[string][2]int
	failWatermark := -1
	i := firstLT
	for i < lastGT {
		if i < failWatermark {
			next := strings.IndexByte(s[failWatermark:lastGT], '<')
			if next < 0 {
				break
			}
			i = failWatermark + next
		}
		name, _, closed, empty, consumed, failPos, sawQuoteOrGT, ok := parseTagRawBounds(s, i, lastGT, lastDQ, lastSQ, false)
		if ok {
			if !empty && !isKnownHTMLElement(name) {
				if counts == nil {
					counts = make(map[string][2]int)
				}
				entry := counts[name]
				if closed {
					entry[1]++
				} else {
					entry[0]++
				}
				counts[name] = entry
			}
			i += consumed
			if i >= lastGT {
				break
			}
			next := strings.IndexByte(s[i:lastGT], '<')
			if next < 0 {
				break
			}
			i += next
			continue
		}
		if !sawQuoteOrGT && failPos > failWatermark {
			failWatermark = failPos
		}
		if i+1 >= lastGT {
			break
		}
		next := strings.IndexByte(s[i+1:lastGT], '<')
		if next < 0 {
			break
		}
		i = i + 1 + next
	}
	return counts
}

func parseTagRaw(s string) (name string, attrs [][2]string, closed, empty bool, consumed int, ok bool) {
	lastGT := strings.LastIndexByte(s, '>')
	if lastGT < 0 {
		return "", nil, false, false, 0, false
	}
	name, attrs, closed, empty, consumed, _, _, ok = parseTagRawBounds(s, 0, lastGT, lastGT-1, lastGT-1, true)
	return name, attrs, closed, empty, consumed, ok
}

func parseTagRawBounds(s string, start, lastGT, lastDQ, lastSQ int, needAttrs bool) (name string, attrs [][2]string, closed, empty bool, consumed, failPos int, sawQuoteOrGT, ok bool) {
	if start >= lastGT || len(s)-start < 3 || s[start] != '<' {
		return "", nil, false, false, 0, start, false, false
	}
	i := start + 1
	if s[i] == '/' {
		closed = true
		i++
	}
	if i >= len(s) || !isASCIIAlpha(s[i]) {
		return "", nil, false, false, 0, i, false, false
	}
	startName := i
	for i < len(s) && (isASCIIAlpha(s[i]) || (s[i] >= '0' && s[i] <= '9') || s[i] == '-') {
		i++
	}
	name = strings.ToLower(s[startName:i])
	if i >= len(s) || (s[i] != '>' && s[i] != '/' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r') {
		return "", nil, false, false, 0, i, i < len(s) && s[i] == '>', false
	}
	limit := lastGT + 1
	for i < limit {
		for i < limit && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if i >= limit {
			return "", nil, false, false, 0, i, sawQuoteOrGT, false
		}
		if s[i] == '/' {
			empty = true
			i++
			for i < limit && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			if i < limit && s[i] == '>' {
				return name, attrs, closed, empty, i + 1 - start, i, true, true
			}
			return "", nil, false, false, 0, i, sawQuoteOrGT, false
		}
		if s[i] == '>' {
			return name, attrs, closed, empty, i + 1 - start, i, true, true
		}
		kStart := i
		for i < limit && s[i] != '=' && s[i] != '>' && s[i] != '/' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
			if s[i] == '"' || s[i] == '\'' {
				sawQuoteOrGT = true
			}
			i++
		}
		if i == kStart {
			return "", nil, false, false, 0, i, sawQuoteOrGT, false
		}
		var key string
		if needAttrs {
			key = strings.ToLower(s[kStart:i])
		}
		for i < limit && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		var val string
		if i < limit && s[i] == '=' {
			i++
			for i < limit && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			if i >= limit {
				return "", nil, false, false, 0, i, sawQuoteOrGT, false
			}
			if s[i] == '"' || s[i] == '\'' {
				sawQuoteOrGT = true
				q := s[i]
				qLimit := lastDQ
				if q == '\'' {
					qLimit = lastSQ
				}
				if i >= qLimit {
					return "", nil, false, false, 0, i, true, false
				}
				i++
				vStart := i
				rel := strings.IndexByte(s[i:qLimit+1], q)
				if rel < 0 {
					return "", nil, false, false, 0, i, true, false
				}
				i += rel
				if needAttrs {
					val = html.UnescapeString(s[vStart:i])
				}
				i++
			} else {
				vStart := i
				for i < limit && s[i] != '>' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
					if s[i] == '"' || s[i] == '\'' {
						sawQuoteOrGT = true
					}
					i++
				}
				if needAttrs {
					val = html.UnescapeString(s[vStart:i])
				}
			}
		}
		if needAttrs {
			attrs = append(attrs, [2]string{key, val})
		}
	}
	return "", nil, false, false, 0, limit, sawQuoteOrGT, false
}

func tryParseHTMLTagAt(s string, start, lastGT, lastDQ, lastSQ int, pairedUnknown map[string][2]int) (raw string, out []*inlineTok, consumed, failPos int, sawQuoteOrGT, ok bool) {
	name, attrs, closed, empty, consumed, failPos, sawQuoteOrGT, ok := parseTagRawBounds(s, start, lastGT, lastDQ, lastSQ, true)
	if !ok {
		return "", nil, 0, failPos, sawQuoteOrGT, false
	}
	raw, out, consumed, ok = finishParsedHTMLTag(s[start:], name, attrs, closed, empty, consumed, pairedUnknown)
	return raw, out, consumed, start + consumed, true, ok
}

func tryParseHTMLTag(s string, pairedUnknown map[string][2]int) (raw string, out []*inlineTok, consumed int, ok bool) {
	name, attrs, closed, empty, consumed, ok := parseTagRaw(s)
	if !ok {
		return "", nil, 0, false
	}
	return finishParsedHTMLTag(s, name, attrs, closed, empty, consumed, pairedUnknown)
}

func finishParsedHTMLTag(s, name string, attrs [][2]string, closed, empty bool, consumed int, pairedUnknown map[string][2]int) (raw string, out []*inlineTok, outConsumed int, ok bool) {
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
		if hasCSSClass(attrVal(attrs, "class"), "math") {
			tok.isMark |= model.MarkMath
		} else {
			tok.isMark |= model.MarkCode
		}
	case "span":
		if hasCSSClass(attrVal(attrs, "class"), "math") {
			tok.isMark |= model.MarkMath
		}
	case "sup":
		tok.isMark |= model.MarkSuperscript
	case "sub":
		tok.isMark |= model.MarkSubscript
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

func delimCharSlot(ch byte) int {
	switch ch {
	case '*':
		return 0
	case '_':
		return 1
	case '~':
		return 2
	default:
		return 0
	}
}

func resolveDelimiters(toks []*inlineTok) {
	var openersBottom [3][3][2]int
	for i := 0; i < len(toks); i++ {
		cl := toks[i]
		if cl.kind != tokDelim || !cl.canCl || cl.count == 0 {
			continue
		}
		ci := delimCharSlot(cl.ch)
		modIdx := cl.orig % 3
		canOpIdx := 0
		if cl.canOp {
			canOpIdx = 1
		}
		bottom := openersBottom[ci][modIdx][canOpIdx]
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
		if cl.count > 0 {
			nextBottom := i
			if !cl.canOp {
				nextBottom = i + 1
			}
			openersBottom[ci][modIdx][canOpIdx] = nextBottom
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
	var markCounts [8]int // Bold, Italic, Underline, Strike, Code, Superscript, Subscript, Math
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
		if m&model.MarkSuperscript != 0 {
			markCounts[5] += delta
		}
		if m&model.MarkSubscript != 0 {
			markCounts[6] += delta
		}
		if m&model.MarkMath != 0 {
			markCounts[7] += delta
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
		if markCounts[5] > 0 {
			st.marks |= model.MarkSuperscript
		}
		if markCounts[6] > 0 {
			st.marks |= model.MarkSubscript
		}
		if markCounts[7] > 0 {
			st.marks |= model.MarkMath
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
		case tokMath:
			appendText(tok.text, model.MarkMath)
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
	var joined strings.Builder
	for _, s := range spans {
		if s.Text == "" {
			continue
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Marks == s.Marks && last.Link == s.Link && last.FGColor == s.FGColor && last.BGColor == s.BGColor {
				if joined.Len() == 0 {
					joined.Grow(len(last.Text) + len(s.Text))
					joined.WriteString(last.Text)
				}
				joined.WriteString(s.Text)
				continue
			}
			if joined.Len() > 0 {
				last.Text = joined.String()
				joined.Reset()
			}
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	if joined.Len() > 0 {
		out[len(out)-1].Text = joined.String()
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
	mdBare := serializeInlineWithMode(norm, false, true)
	if spansEqual(ParseInline(mdBare), norm) {
		return mdBare
	}
	mdExplicit := serializeInlineWithMode(norm, false, false)
	if spansEqual(ParseInline(mdExplicit), norm) {
		return mdExplicit
	}
	// Fallback to HTML formatting tags for non-round-tripping delimiter edge cases.
	htmlCandidate := serializeInlineWithMode(norm, true, false)
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
	wrapSup
	wrapSub
)

type inlineWrapper struct {
	kind  inlineWrapperKind
	val1  string
	val2  string
	useHT bool
}

func wrappersForSpan(s model.Span, forceHTML, allowBareURL bool) []inlineWrapper {
	var w []inlineWrapper
	isBareSelfLink := allowBareURL && !forceHTML && s.Link != "" &&
		s.Marks == 0 && s.FGColor == "" && s.BGColor == "" && isExactBareAutolink(s.Text, s.Link)
	if s.Link != "" && !isBareSelfLink {
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
	if s.Marks&model.MarkSuperscript != 0 {
		w = append(w, inlineWrapper{kind: wrapSup, useHT: forceHTML})
	}
	if s.Marks&model.MarkSubscript != 0 {
		w = append(w, inlineWrapper{kind: wrapSub, useHT: forceHTML})
	}
	return w
}

func isExactBareAutolink(text, link string) bool {
	if text == "" || link == "" {
		return false
	}
	disp, url, consumed, ok := tryParseBareAutolink(text, 0)
	return ok && consumed == len(text) && disp == text && url == link
}

func serializeInlineWithMode(spans []model.Span, forceHTML, allowBareURL bool) string {
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
		case wrapSup:
			if w.useHT {
				out.WriteString("<sup>")
			} else {
				out.WriteByte('^')
			}
		case wrapSub:
			if w.useHT {
				out.WriteString("<sub>")
			} else {
				out.WriteByte('~')
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
		case wrapSup:
			if w.useHT {
				out.WriteString("</sup>")
			} else {
				out.WriteByte('^')
			}
		case wrapSub:
			if w.useHT {
				out.WriteString("</sub>")
			} else {
				out.WriteByte('~')
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
		if !useHTML && sp.Marks&(model.MarkSuperscript|model.MarkSubscript) != 0 {
			if strings.ContainsAny(sp.Text, " \t\n\r") {
				useHTML = true
			}
		}
		target := wrappersForSpan(sp, useHTML, allowBareURL)
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
		} else if sp.Marks&model.MarkMath != 0 {
			if useHTML || !canSerializeDollarMath(sp.Text) {
				out.WriteString(`<span class="math">`)
				writeLiteralHTMLText(&out, sp.Text)
				out.WriteString(`</span>`)
			} else {
				writeInlineMath(&out, sp.Text)
			}
		} else if allowBareURL && !useHTML && sp.Link != "" && len(opened) == 0 && isExactBareAutolink(sp.Text, sp.Link) {
			out.WriteString(sp.Text)
		} else {
			writeInlineText(&out, sp.Text, len(opened) > 0 && opened[0].kind == wrapLink && !opened[0].useHT, useHTML, sp.Link == "")
		}
	}
	for k := len(opened) - 1; k >= 0; k-- {
		writeClose(opened[k])
	}
	return simplifyInlineEscapes(out.String(), spans)
}

func writeLiteralHTMLText(out *strings.Builder, text string) {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '\n':
			out.WriteString("<br>")
		default:
			out.WriteByte(text[i])
		}
	}
}

func hasCSSClass(classAttr, want string) bool {
	for _, f := range strings.Fields(classAttr) {
		if f == want {
			return true
		}
	}
	return false
}

func canSerializeDollarMath(text string) bool {
	if text == "" || isInlineSpaceByte(text[0]) || isInlineSpaceByte(text[len(text)-1]) {
		return false
	}
	if strings.ContainsAny(text, "\n\r") {
		return false
	}
	return true
}

func writeInlineMath(out *strings.Builder, text string) {
	out.WriteByte('$')
	for i := 0; i < len(text); i++ {
		if text[i] == '$' || text[i] == '\\' {
			out.WriteByte('\\')
		}
		out.WriteByte(text[i])
	}
	out.WriteByte('$')
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
	needSpacePad := strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") ||
		(len(text) >= 2 && text[0] == ' ' && text[len(text)-1] == ' ' && strings.Trim(text, " ") != "")
	if needSpacePad {
		out.WriteByte(' ')
		out.WriteString(text)
		out.WriteByte(' ')
	} else {
		out.WriteString(text)
	}
	out.WriteString(fence)
}

func writeInlineText(out *strings.Builder, text string, inMarkdownLink, htmlCtx, escapeBareURL bool) {
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
		case ':':
			if escapeBareURL && !inMarkdownLink && !htmlCtx && strings.HasPrefix(text[i:], "://") &&
				((i >= 4 && strings.EqualFold(text[i-4:i], "http")) || (i >= 5 && strings.EqualFold(text[i-5:i], "https"))) {
				out.WriteString(`\:`)
			} else {
				out.WriteByte(':')
			}
		case '\\', '`', '*', '~', '_', '^', '$':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '=':
			if strings.Contains(text, "==") {
				out.WriteByte('\\')
			}
			out.WriteByte('=')
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
	return strings.ContainsRune(s, '[') && (strings.Contains(s, "](") || strings.Contains(s, "][") || strings.Contains(s, "[^"))
}

var escapeSimplifyTargets = [13]byte{'~', '*', '_', '[', ']', '`', '<', '&', ':', '\\', '^', '$', '='}

func escapeSimplifySlot(ch byte) int {
	switch ch {
	case '~':
		return 0
	case '*':
		return 1
	case '_':
		return 2
	case '[':
		return 3
	case ']':
		return 4
	case '`':
		return 5
	case '<':
		return 6
	case '&':
		return 7
	case ':':
		return 8
	case '\\':
		return 9
	case '^':
		return 10
	case '$':
		return 11
	case '=':
		return 12
	default:
		return -1
	}
}

func stripAllActiveEscapeFor(src string, ch byte) string {
	var b strings.Builder
	b.Grow(len(src))
	pos := 0
	for pos+1 < len(src) {
		idx := strings.IndexByte(src[pos:], '\\')
		if idx < 0 {
			break
		}
		bs := pos + idx
		if bs+1 >= len(src) {
			break
		}
		backRun := 0
		for k := bs - 1; k >= 0 && src[k] == '\\'; k-- {
			backRun++
		}
		if backRun%2 == 1 || src[bs+1] != ch {
			b.WriteString(src[pos : bs+1])
			pos = bs + 1
			continue
		}
		b.WriteString(src[pos:bs])
		b.WriteByte(ch)
		pos = bs + 2
	}
	b.WriteString(src[pos:])
	return b.String()
}

func simplifyInlineEscapes(src string, expected []model.Span) string {
	if !strings.ContainsRune(src, '\\') {
		return src
	}
	var counts [13]int
	anyFound := false
	for i := 0; i+1 < len(src); {
		idx := strings.IndexByte(src[i:], '\\')
		if idx < 0 {
			break
		}
		bs := i + idx
		if bs+1 >= len(src) {
			break
		}
		end := bs + 1
		for end < len(src) && src[end] == '\\' {
			end++
		}
		run := end - bs
		if run >= 2 {
			counts[9] += run / 2
			anyFound = true
			if run%2 == 1 && end < len(src) {
				if slot := escapeSimplifySlot(src[end]); slot >= 0 && slot != 9 {
					counts[slot]++
				}
				i = end + 1
			} else {
				i = end
			}
		} else {
			if slot := escapeSimplifySlot(src[bs+1]); slot >= 0 {
				counts[slot]++
				anyFound = true
			}
			i = bs + 2
		}
	}
	if !anyFound {
		return src
	}
	exp := coalesceSpans(expected)
	for slot, ch := range escapeSimplifyTargets {
		if counts[slot] == 0 {
			continue
		}
		if ch != '\\' && counts[slot] >= 2 {
			candAll := stripAllActiveEscapeFor(src, ch)
			if spansEqual(ParseInline(candAll), exp) {
				src = candAll
				continue
			}
		}
		pos := 0
		for pos+1 < len(src) {
			idx := strings.IndexByte(src[pos:], '\\')
			if idx < 0 {
				break
			}
			bs := pos + idx
			if bs+1 >= len(src) {
				break
			}
			// Count preceding backslashes so we only test an active escape backslash.
			backRun := 0
			for k := bs - 1; k >= 0 && src[k] == '\\'; k-- {
				backRun++
			}
			if backRun%2 == 1 || src[bs+1] != ch {
				pos = bs + 1
				continue
			}
			cand := src[:bs] + src[bs+1:]
			if spansEqual(ParseInline(cand), exp) {
				src = cand
				pos = bs + 1
			} else {
				pos = bs + 2
			}
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
	// Trim trailing spaces/tabs/fullwidth spaces on whitespace-only lines (unless TSV row with >=1 tab and non-tab cells? No, pure whitespace has no cell text).
	if strings.Trim(line, " \t\u3000") == "" {
		return ""
	}
	// Preserve tab-separated (TSV) columns verbatim so leading/trailing empty cells survive paste.
	if strings.ContainsRune(line, '\t') {
		return strings.TrimRight(line, " \u3000")
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

	// Normalize standalone checkbox markers (`☐ Task`, `☑ Task`, `[ ] Task`, `【】 Task`) without a leading `- `.
	if todoBody, checked, ok := matchStandaloneTodoLine(rest); ok {
		box := "- [ ]"
		if checked {
			box = "- [x]"
		}
		if todoBody == "" {
			return prefix + box
		}
		return prefix + box + " " + normalizeFullwidthInlineDelims(todoBody)
	}

	// Normalize fullwidth bullet markers & common CJK/Unicode bullet characters.
	for _, bullet := range []string{"－", "＊", "•", "◦", "‣", "▸", "▹", "►", "◆", "◇", "❖", "·", "▪", "●", "–", "-", "*", "+"} {
		if strings.HasPrefix(rest, bullet) {
			after := rest[len(bullet):]
			if after == "" {
				if bullet == "–" {
					break
				}
				if bullet == "-" || bullet == "*" || bullet == "+" {
					return prefix + bullet
				}
				return prefix + "-"
			}
			if strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t") || strings.HasPrefix(after, "\u3000") {
				trimmedAfter := strings.TrimLeft(after, " \t\u3000")
				normalizedAfter := normalizeFullwidthInlineDelims(normalizeTodoPrefix(trimmedAfter))
				if bullet == "＊" {
					return prefix + "* " + normalizedAfter
				}
				if bullet == "-" || bullet == "*" || bullet == "+" {
					return prefix + bullet + " " + normalizedAfter
				}
				return prefix + "- " + normalizedAfter
			}
			// For explicit Unicode bullet glyphs like `•内容` or `◦内容`, insert space automatically.
			switch bullet {
			case "•", "◦", "‣", "▸", "▹", "►", "◆", "◇", "❖", "▪", "●":
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

var todoPrefixMarkers = []struct {
	raw          string
	checked      bool
	requireSpace bool
}{
	{"☐", false, false},
	{"☑", true, false},
	{"☒", true, false},
	{"［ ］", false, false},
	{"［　］", false, false},
	{"［］", false, false},
	{"【 】", false, false},
	{"【　】", false, false},
	{"【】", false, false},
	{"[ ]", false, true},
	{"[]", false, true},
	{"[　]", false, false},
	{"［x］", true, false},
	{"［X］", true, false},
	{"［ｘ］", true, false},
	{"［Ｘ］", true, false},
	{"【x】", true, false},
	{"【X】", true, false},
	{"[x]", true, true},
	{"[X]", true, true},
	{"[ｘ]", true, false},
	{"[Ｘ]", true, false},
}

func matchStandaloneTodoLine(s string) (body string, checked, ok bool) {
	for _, m := range todoPrefixMarkers {
		if !strings.HasPrefix(s, m.raw) {
			continue
		}
		after := s[len(m.raw):]
		if after == "" {
			return "", m.checked, true
		}
		hasWS := strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t") || strings.HasPrefix(after, "\u3000")
		if m.requireSpace && !hasWS {
			continue
		}
		return strings.TrimLeft(after, " \t\u3000"), m.checked, true
	}
	return "", false, false
}

func normalizeTodoPrefix(s string) string {
	for _, m := range todoPrefixMarkers {
		if !strings.HasPrefix(s, m.raw) {
			continue
		}
		after := s[len(m.raw):]
		hasWS := after == "" || strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t") || strings.HasPrefix(after, "\u3000")
		if m.requireSpace && !hasWS {
			continue
		}
		box := "[ ]"
		if m.checked {
			box = "[x]"
		}
		trimmed := strings.TrimLeft(after, " \t\u3000")
		if trimmed == "" {
			return box
		}
		return box + " " + trimmed
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
