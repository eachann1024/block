package markdown

import (
	"html"
	"strconv"
	"strings"
	"unicode"

	"block/internal/model"
)

// Codec implements model.MarkdownCodec using the lossless block parser/serializer
// and intelligent paste normalizer.
type Codec struct{}

// NewCodec returns a stateless MarkdownCodec implementation.
func NewCodec() *Codec { return &Codec{} }

var _ model.MarkdownCodec = (*Codec)(nil)

func (c *Codec) Parse(src string) *model.Doc {
	return ParseMarkdown(src)
}

func (c *Codec) Serialize(doc *model.Doc) string {
	return SerializeMarkdown(doc)
}

func (c *Codec) SerializeInline(spans []model.Span) string {
	return SerializeInline(spans)
}

func (c *Codec) ExpandBlockRefCleanSources(blocks []model.Block, cleanRaw string) {
	expandBlockRefCleanSources(blocks, cleanRaw)
}

func (c *Codec) NormalizePaste(raw string) string {
	return NormalizePasteText(raw)
}

func (c *Codec) ConvertHTMLPaste(htmlRaw, plainFallback string) (string, bool) {
	return ConvertClipboardHTML(htmlRaw, plainFallback)
}

// ParseMarkdown parses Markdown into a lossless block-level model.Doc.
// Untouched blocks retain CleanSource and the whole document retains CleanRaw.
func ParseMarkdown(src string) *model.Doc {
	trailing := detectTrailingLine(src)
	if src == "" {
		return &model.Doc{
			Blocks:       []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph}},
			CleanRaw:     "",
			TrailingLine: "",
		}
	}

	decoded := decodeRawBlockGuard(src)
	cleaned := strings.TrimPrefix(decoded, "\uFEFF")
	lines := splitLinesInclusive(cleaned)
	blocks, _, _, _ := parseBlocksAndRefDefs(cleaned, lines, false)
	blocks = swallowSeparators(blocks)
	hadLegacyEmptyBlock := stripLegacyEmptyBlockComments(blocks)
	normalizeImportedStarts(blocks)

	if len(blocks) == 0 {
		blocks = []model.Block{{ID: model.NewBlockID(), Kind: model.BlockParagraph}}
	}
	cleanRaw := src
	if hadLegacyEmptyBlock {
		cleanRaw = ""
	}
	return &model.Doc{
		Blocks:       blocks,
		CleanRaw:     cleanRaw,
		TrailingLine: trailing,
	}
}

func detectTrailingLine(src string) string {
	if strings.HasSuffix(src, "\r\n") {
		return "\r\n"
	}
	if strings.HasSuffix(src, "\n") {
		return "\n"
	}
	return ""
}

func splitLinesInclusive(s string) []string {
	if s == "" {
		return nil
	}
	lines := make([]string, 0, strings.Count(s, "\n")+1)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimLineEnding(line string) string {
	return strings.TrimRight(line, "\r\n")
}

func leadingColumns(line string) (int, string) {
	cols := 0
	i := 0
	for i < len(line) {
		switch line[i] {
		case ' ':
			cols++
			i++
		case '\t':
			cols += 4
			i++
		default:
			return cols, line[i:]
		}
	}
	return cols, ""
}

func stripColumns(line string, cols int) string {
	rem := cols
	idx := 0
	for i := 0; i < len(line); i++ {
		if rem <= 0 {
			idx = i
			break
		}
		switch line[i] {
		case ' ':
			rem--
			idx = i + 1
		case '\t':
			if rem >= 4 {
				rem -= 4
				idx = i + 1
			} else {
				spaces := strings.Repeat(" ", 4-rem)
				return spaces + line[i+1:]
			}
		default:
			// Only whitespace is stripped: "```" at column 0 keeps all its backticks.
			return line[i:]
		}
	}
	return line[idx:]
}

func decodeRawBlockGuard(src string) string {
	return decodeRawBlockGuardWithLines(src, nil)
}

func decodeRawBlockGuardWithLines(src string, sourceLines *[]int) string {
	if !strings.Contains(src, "raw-block") {
		return src
	}
	lines := splitLinesInclusive(src)
	var out strings.Builder
	out.Grow(len(src))
	writeLine := func(i int) {
		out.WriteString(lines[i])
		if sourceLines != nil {
			*sourceLines = append(*sourceLines, i+1)
		}
	}
	i := 0
	for i < len(lines) {
		line := trimLineEnding(lines[i])
		if strings.TrimSpace(line) == "$$" {
			end := i + 1
			for end < len(lines) && strings.TrimSpace(trimLineEnding(lines[end])) != "$$" {
				end++
			}
			stop := end
			if end < len(lines) {
				stop = end + 1
			}
			for j := i; j < stop; j++ {
				writeLine(j)
			}
			i = end + 1
			continue
		}
		marker, width, info, ok := parseFenceMax3(line)
		if !ok {
			writeLine(i)
			i++
			continue
		}
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if closesFenceMax3(trimLineEnding(lines[j]), marker, width) {
				end = j
				break
			}
		}
		if end < 0 {
			for j := i; j < len(lines); j++ {
				writeLine(j)
			}
			break
		}
		firstWord := ""
		if fields := strings.Fields(info); len(fields) > 0 {
			firstWord = fields[0]
		}
		if firstWord == "thinkly-raw-block" || firstWord == "goose-raw-block" {
			start := i + 1
			if start < end && isRawBlockMarker(trimLineEnding(lines[start])) {
				start++
			}
			for j := start; j < end; j++ {
				writeLine(j)
			}
		} else {
			for j := i; j <= end; j++ {
				writeLine(j)
			}
		}
		i = end + 1
	}
	return out.String()
}

func isRawBlockMarker(line string) bool {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return false
	}
	inner := strings.TrimSpace(s[4 : len(s)-3])
	return inner == "thinkly:raw-block" || inner == "goose-note:raw-block"
}

func parseFenceMax3(line string) (marker byte, width int, info string, ok bool) {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 {
		return 0, 0, "", false
	}
	return parseFenceBody(body)
}

func parseFenceAnyIndent(line string) (indentCols, indentBytes int, marker byte, width int, info string, ok bool) {
	cols, body := leadingColumns(line)
	indentBytes = len(line) - len(body)
	m, w, inf, ok := parseFenceBody(body)
	if !ok {
		return 0, 0, 0, 0, "", false
	}
	return cols, indentBytes, m, w, inf, true
}

func parseFenceBody(body string) (marker byte, width int, info string, ok bool) {
	if strings.TrimSpace(body) == "$$" {
		return '$', 2, "math", true
	}
	if len(body) == 0 {
		return 0, 0, "", false
	}
	m := body[0]
	if m != '`' && m != '~' {
		return 0, 0, "", false
	}
	w := 0
	for w < len(body) && body[w] == m {
		w++
	}
	if w < 3 {
		return 0, 0, "", false
	}
	inf := strings.TrimSpace(body[w:])
	if m == '`' && strings.ContainsRune(inf, '`') {
		return 0, 0, "", false
	}
	return m, w, inf, true
}

func closesFenceMax3(line string, marker byte, width int) bool {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 {
		return false
	}
	return closesFenceBody(body, marker, width)
}

func closesFenceAnyIndent(line string, marker byte, width int) bool {
	_, body := leadingColumns(line)
	return closesFenceBody(body, marker, width)
}

func closesFenceBody(body string, marker byte, width int) bool {
	if marker == '$' {
		return strings.TrimSpace(body) == "$$"
	}
	count := 0
	for count < len(body) && body[count] == marker {
		count++
	}
	if count < width {
		return false
	}
	return strings.TrimSpace(body[count:]) == ""
}

func parseInlineMathBlock(line string) (int, string, bool) {
	indent, body := leadingColumns(line)
	trimmed := strings.TrimRight(body, " \t")
	if strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$") && len(trimmed) > 4 {
		inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
		if !strings.Contains(inner, "$$") {
			return indent, inner, true
		}
	}
	return 0, "", false
}

func isHTMLHR(body string) bool {
	b := strings.TrimSpace(body)
	return strings.EqualFold(b, "<hr>") || strings.EqualFold(b, "<hr/>") || strings.EqualFold(b, "<hr />")
}

func isThematicBreak(line string) bool {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 {
		return false
	}
	if isHTMLHR(body) {
		return true
	}
	var ch byte
	count := 0
	for i := 0; i < len(body); i++ {
		b := body[i]
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		if b != '-' && b != '*' && b != '_' {
			return false
		}
		if ch == 0 {
			ch = b
		} else if ch != b {
			return false
		}
		count++
	}
	return count >= 3
}

func isEmptyBlockComment(line string) bool {
	s := strings.TrimSpace(line)
	return s == "<!-- thinkly:empty-block=paragraph -->" || s == "<!-- goose-note:empty-block=paragraph -->"
}

func classifyHeading(line string) (model.BlockKind, string, bool) {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 {
		return 0, "", false
	}
	hashes := 0
	for hashes < len(body) && body[hashes] == '#' {
		hashes++
	}
	if hashes < 1 || hashes > 6 {
		return 0, "", false
	}
	after := body[hashes:]
	if after != "" && after[0] != ' ' && after[0] != '\t' {
		return 0, "", false
	}
	text := strings.TrimLeft(after, " \t")
	if strings.HasSuffix(text, "#") {
		stripped := strings.TrimRight(text, "#")
		if stripped == "" || strings.HasSuffix(stripped, " ") || strings.HasSuffix(stripped, "\t") {
			text = strings.TrimRight(stripped, " \t")
		}
	}
	return model.HeadingKind(hashes), text, true
}

func parseAlertTag(line string) (model.CalloutKind, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "[!") || !strings.HasSuffix(s, "]") {
		return "", false
	}
	tag := strings.ToUpper(strings.TrimSpace(s[2 : len(s)-1]))
	switch tag {
	case "NOTE":
		return model.CalloutNote, true
	case "TIP":
		return model.CalloutTip, true
	case "IMPORTANT":
		return model.CalloutImportant, true
	case "WARNING":
		return model.CalloutWarning, true
	case "CAUTION":
		return model.CalloutCaution, true
	}
	return "", false
}

// collectRefDefs extracts `[label]: url "title"` definitions that are referenced in source.
func collectRefDefs(src string) (map[string]refDef, map[string]bool) {
	return collectRefDefsFromLines(src, nil)
}

func collectCandidateRefDefsFromLines(src string, lines []string) map[string]refDef {
	if !strings.Contains(src, "]:") {
		return nil
	}
	if lines == nil {
		lines = splitLinesInclusive(src)
	}
	defs := make(map[string]refDef)
	inFence := false
	var fc byte
	var fl int
	for i := 0; i < len(lines); i++ {
		line := trimLineEnding(lines[i])
		if i == 0 {
			if _, count, ok := parseFrontmatter(lines); ok {
				i += count - 1
				continue
			}
		}
		if inFence {
			if closesFenceAnyIndent(line, fc, fl) {
				inFence = false
			}
			continue
		}
		if _, _, m, w, _, ok := parseFenceAnyIndent(line); ok {
			inFence = true
			fc = m
			fl = w
			continue
		}
		if count, ok := rawHTMLLineCount(lines[i:]); ok {
			i += count - 1
			continue
		}
		if _, count, ok := parseMultiLineHTMLAnchorBlock(lines[i:], nil, nil); ok && count > 0 {
			i += count - 1
			continue
		}
		if _, ok := openHTMLProseTag(line); ok {
			if count := htmlProseLineCount(lines[i:]); count > 0 {
				i += count - 1
				continue
			}
		}
		if lowerTrim := strings.ToLower(strings.TrimLeft(line, " \t")); strings.HasPrefix(lowerTrim, "<table") {
			if count := htmlTableLineCount(lines[i:]); count > 0 {
				i += count - 1
				continue
			}
		} else if strings.HasPrefix(lowerTrim, "<ul") || strings.HasPrefix(lowerTrim, "<ol") {
			if count := htmlListLineCount(lines[i:]); count > 0 {
				i += count - 1
				continue
			}
		}
		if firstTrim := strings.TrimLeft(line, " \t"); strings.HasPrefix(firstTrim, ":::") && strings.TrimSpace(firstTrim) != ":::" {
			if _, count, ok := parseCustomContainerBlock(lines[i:], nil, nil); ok && count > 0 {
				i += count - 1
				continue
			}
		}
		if isPipeCandidate(lines, i) {
			hdrCols := splitPipeCells(trimLineEnding(lines[i]))
			delimCols := splitPipeCells(trimLineEnding(lines[i+1]))
			if len(hdrCols) > 0 && len(hdrCols) <= maxParsedTableCells && len(hdrCols) == len(delimCols) {
				end := i + 2
				for end < len(lines) && isPipeTableRowLine(trimLineEnding(lines[end])) {
					end++
				}
				i = end - 1
				continue
			}
		}
		if _, ok := classifyListMarker(line); ok {
			if _, count, okL := parseListItem(lines[i:], nil, nil, 1, 0); okL && count > 0 {
				i += count - 1
				continue
			}
		}
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) <= 3 && len(trimmed) > 0 && trimmed[0] == '[' && !strings.HasPrefix(trimmed, "[^") {
			if label, def, consumed, ok := parseRefDefLines(lines[i:]); ok {
				if _, exists := defs[label]; !exists {
					defs[label] = def
				}
				i += consumed - 1
			}
		}
	}
	if len(defs) == 0 {
		return nil
	}
	return defs
}

// parseBlocksAndRefDefs parses blocks and resolves link reference definitions using the
// real block and inline parsers as the single source of truth, so block boundaries (e.g.
// headings/tables/fences) and inline literals (code, math, HTML tags, bare URLs, autolinks,
// inline link destinations/titles) never diverge between usage detection and parsing.
func parseBlocksAndRefDefs(src string, lines []string, collectHidden bool) ([]model.Block, map[string]refDef, map[string]bool, []int) {
	if lines == nil {
		lines = splitLinesInclusive(src)
	}
	allDefs := collectCandidateRefDefsFromLines(src, lines)
	if len(allDefs) == 0 {
		return parseBlocksWithLinesAt(lines, nil, nil, nil, nil, 1, 0, nil), nil, nil, nil
	}
	return parseBlocksWithCandidateRefDefs(lines, allDefs, collectHidden)
}

func parseBlocksWithCandidateRefDefs(lines []string, allDefs map[string]refDef, collectHidden bool) ([]model.Block, map[string]refDef, map[string]bool, []int) {
	hideAll := make(map[string]bool, len(allDefs))
	for k := range allDefs {
		hideAll[k] = true
	}
	used := make(map[string]bool)
	consumed := make(map[string]bool)
	var hidden []int
	var hiddenPtr *[]int
	if collectHidden {
		hiddenPtr = &hidden
	}
	blocks := parseBlocksWithLinesAt(lines, allDefs, hideAll, used, consumed, 1, 0, hiddenPtr)
	for k := range used {
		if !consumed[k] {
			delete(used, k)
		}
	}
	if len(used) == 0 {
		return parseBlocksWithLinesAt(lines, nil, nil, nil, nil, 1, 0, nil), nil, nil, nil
	}
	if len(used) == len(allDefs) {
		return blocks, allDefs, used, hidden
	}
	for k := range allDefs {
		if !used[k] {
			delete(allDefs, k)
		}
	}
	hidden = nil
	blocks = parseBlocksWithLinesAt(lines, allDefs, used, nil, nil, 1, 0, hiddenPtr)
	return blocks, allDefs, used, hidden
}

func collectRefDefsFromLines(src string, lines []string) (map[string]refDef, map[string]bool) {
	if !strings.Contains(src, "]:") {
		return nil, nil
	}
	if lines == nil {
		lines = splitLinesInclusive(src)
	}
	allDefs := collectCandidateRefDefsFromLines(src, lines)
	if len(allDefs) == 0 {
		return nil, nil
	}
	_, defs, used, _ := parseBlocksWithCandidateRefDefs(lines, allDefs, false)
	return defs, used
}

// LinkReferenceLines gives the labels available for inline links and the
// 1-based source lines the note parser consumes as definitions.
func LinkReferenceLines(src string) (labels map[string]bool, hidden []int) {
	if !strings.Contains(src, "]:") {
		return nil, nil
	}
	var originalLines []int
	cleaned := strings.TrimPrefix(decodeRawBlockGuardWithLines(src, &originalLines), "\uFEFF")
	lines := splitLinesInclusive(cleaned)
	allDefs := collectCandidateRefDefsFromLines(cleaned, lines)
	if len(allDefs) == 0 {
		return nil, nil
	}
	_, defs, used, hidden := parseBlocksWithCandidateRefDefs(lines, allDefs, true)
	if len(used) == 0 {
		return nil, nil
	}
	labels = make(map[string]bool, len(defs))
	for label := range defs {
		labels[label] = true
	}
	if len(originalLines) > 0 {
		for i, line := range hidden {
			hidden[i] = originalLines[line-1]
		}
	}
	return labels, hidden
}

func parseRefDefLines(lines []string) (label string, def refDef, count int, ok bool) {
	if len(lines) == 0 {
		return "", refDef{}, 0, false
	}
	first := strings.TrimLeft(trimLineEnding(lines[0]), " ")
	if len(trimLineEnding(lines[0]))-len(first) > 3 || !strings.HasPrefix(first, "[") || strings.HasPrefix(first, "[^") {
		return "", refDef{}, 0, false
	}
	closeIdx := findUnescapedByte(first[1:], ']')
	if closeIdx <= 0 {
		return "", refDef{}, 0, false
	}
	afterLabel := first[1+closeIdx+1:]
	if !strings.HasPrefix(afterLabel, ":") {
		return "", refDef{}, 0, false
	}
	rawLabel := first[1 : 1+closeIdx]
	norm := normRefLabel(rawLabel)
	if norm == "" {
		return "", refDef{}, 0, false
	}
	rest := strings.TrimSpace(afterLabel[1:])
	consumed := 1
	if rest == "" && len(lines) > 1 {
		next := strings.TrimSpace(trimLineEnding(lines[1]))
		if next != "" {
			rest = next
			consumed = 2
		}
	}
	if rest == "" {
		return "", refDef{}, 0, false
	}
	var dest string
	var tail string
	if strings.HasPrefix(rest, "<") {
		end := strings.IndexByte(rest[1:], '>')
		if end < 0 {
			return "", refDef{}, 0, false
		}
		dest = unescapeLinkText(rest[1 : 1+end])
		tail = strings.TrimSpace(rest[1+end+1:])
	} else {
		sp := strings.IndexAny(rest, " \t")
		if sp < 0 {
			dest = unescapeLinkText(rest)
		} else {
			dest = unescapeLinkText(rest[:sp])
			tail = strings.TrimSpace(rest[sp:])
		}
	}
	if dest == "" {
		return "", refDef{}, 0, false
	}
	var title string
	if tail != "" {
		if len(tail) >= 2 && ((tail[0] == '"' && tail[len(tail)-1] == '"') || (tail[0] == '\'' && tail[len(tail)-1] == '\'') || (tail[0] == '(' && tail[len(tail)-1] == ')')) {
			title = tail[1 : len(tail)-1]
		} else {
			return "", refDef{}, 0, false
		}
	}
	return norm, refDef{dest: unescapeMarkdownEntities(dest), title: unescapeMarkdownEntities(unescapeLinkText(title))}, consumed, true
}

func consumedRefDefLines(lines []string, used map[string]bool, consumedOut map[string]bool) int {
	if len(used) == 0 {
		return 0
	}
	label, _, count, ok := parseRefDefLines(lines)
	if ok && used[label] {
		if consumedOut != nil {
			consumedOut[label] = true
		}
		return count
	}
	return 0
}

// Frontmatter parsing: leading `---` ... `---` YAML block.
func isFrontmatterOpen(line string) bool {
	l := strings.TrimPrefix(trimLineEnding(line), "\uFEFF")
	return strings.TrimRight(l, " \t") == "---"
}

func isFrontmatterClose(line string) bool {
	return strings.TrimRight(trimLineEnding(line), " \t") == "---"
}

func hasYAMLKey(trimmed string) bool {
	if len(trimmed) > 0 && (trimmed[0] == '"' || trimmed[0] == '\'') {
		q := trimmed[0]
		escaped := false
		for i := 1; i < len(trimmed); i++ {
			if q == '"' && trimmed[i] == '\\' && !escaped {
				escaped = true
				continue
			}
			if trimmed[i] == q && !escaped {
				rest := strings.TrimLeft(trimmed[i+1:], " \t")
				return strings.HasPrefix(rest, ":") && (len(rest) == 1 || rest[1] == ' ' || rest[1] == '\t')
			}
			escaped = false
		}
		return false
	}
	if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "`") || strings.HasPrefix(trimmed, "|") {
		return false
	}
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(trimmed) && (trimmed[digits] == '.' || trimmed[digits] == ')') {
		return false
	}
	for i := 1; i < len(trimmed); i++ {
		if trimmed[i] == ':' {
			rest := trimmed[i:]
			if len(rest) == 1 || rest[1] == ' ' || rest[1] == '\t' {
				return true
			}
		}
	}
	return false
}

func looksLikeFrontmatter(inner []string) bool {
	allEmpty := true
	hasBlank := false
	hasConstruct := false
	for _, raw := range inner {
		l := trimLineEnding(raw)
		if strings.TrimSpace(l) == "" {
			hasBlank = true
			continue
		}
		allEmpty = false
		if _, _, _, ok := parseFenceMax3(l); ok {
			return false
		}
		trimmed := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(trimmed, "#") || len(l) > len(trimmed) {
			continue
		}
		construct := trimmed == "-" || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-\t") ||
			strings.ContainsRune("%{}[]&*?", rune(trimmed[0])) || hasYAMLKey(trimmed)
		if !construct {
			return false
		}
		hasConstruct = true
	}
	if allEmpty {
		return true
	}
	return hasConstruct || !hasBlank
}

func parseFrontmatter(lines []string) (model.Block, int, bool) {
	if len(lines) < 2 || !isFrontmatterOpen(lines[0]) {
		return model.Block{}, 0, false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if isFrontmatterClose(lines[i]) {
			end = i
			break
		}
	}
	if end < 0 || !looksLikeFrontmatter(lines[1:end]) {
		return model.Block{}, 0, false
	}
	rawAll := strings.Join(lines[:end+1], "")
	body := strings.Join(lines[1:end], "")
	body = trimLineEnding(body)
	return model.Block{
		ID:          model.NewBlockID(),
		Kind:        model.BlockCode,
		Language:    "yaml-frontmatter",
		Code:        body,
		CleanSource: rawAll,
	}, end + 1, true
}

// Image metadata & image block parsing.
type imageMeta struct {
	width float32
	align model.ImageAlign
	crop  model.ImageCrop
}

type tableCellTint struct {
	row, col int
	tint     model.TableTint
}

type tableCellAlign struct {
	row, col int
	align    model.ColAlign
}

type tableMeta struct {
	noHeaderRow bool
	headerCol   bool
	widths      []float32
	colTints    []model.TableTint
	rowTints    []model.TableTint
	cellTints   []tableCellTint
	rowAligns   []model.ColAlign
	cellAligns  []tableCellAlign
}

func parseTableTintName(raw string) model.TableTint {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "slate":
		return model.TableTintSlate
	case "ocean":
		return model.TableTintOcean
	case "emerald":
		return model.TableTintEmerald
	case "amber":
		return model.TableTintAmber
	case "rose":
		return model.TableTintRose
	default:
		return model.TableTintNone
	}
}

func parseTableTints(raw string) []model.TableTint {
	parts := strings.Split(raw, ",")
	out := make([]model.TableTint, len(parts))
	for i, p := range parts {
		out[i] = parseTableTintName(p)
	}
	return out
}

func parseTableCellTints(raw string) []tableCellTint {
	var out []tableCellTint
	for _, item := range strings.Split(raw, ",") {
		coord, val, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok {
			continue
		}
		rStr, cStr, ok := strings.Cut(coord, ":")
		if !ok {
			continue
		}
		r, errR := strconv.Atoi(strings.TrimSpace(rStr))
		c, errC := strconv.Atoi(strings.TrimSpace(cStr))
		if errR != nil || errC != nil || r < 0 || c < 0 {
			continue
		}
		out = append(out, tableCellTint{row: r, col: c, tint: parseTableTintName(val)})
	}
	return out
}

func parseTableAligns(raw string) []model.ColAlign {
	parts := strings.Split(raw, ",")
	out := make([]model.ColAlign, len(parts))
	for i, p := range parts {
		out[i] = parseColAlign(p)
	}
	return out
}

func parseTableCellAligns(raw string) []tableCellAlign {
	var out []tableCellAlign
	for _, item := range strings.Split(raw, ",") {
		coord, val, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok {
			continue
		}
		rStr, cStr, ok := strings.Cut(coord, ":")
		if !ok {
			continue
		}
		r, errR := strconv.Atoi(strings.TrimSpace(rStr))
		c, errC := strconv.Atoi(strings.TrimSpace(cStr))
		if errR != nil || errC != nil || r < 0 || c < 0 {
			continue
		}
		if a := parseColAlign(val); a != model.ColAlignNone {
			out = append(out, tableCellAlign{row: r, col: c, align: a})
		}
	}
	return out
}

func parseTableMetaComment(line string) (tableMeta, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return tableMeta{}, false
	}
	inner := strings.TrimSpace(s[4 : len(s)-3])
	if !strings.HasPrefix(inner, "thinkly:table") {
		return tableMeta{}, false
	}
	rest := strings.TrimSpace(inner[len("thinkly:table"):])
	var meta tableMeta
	for _, field := range strings.Fields(rest) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch strings.ToLower(k) {
		case "header":
			if v == "0" || strings.EqualFold(v, "false") {
				meta.noHeaderRow = true
			}
		case "headercol":
			if v == "1" || strings.EqualFold(v, "true") {
				meta.headerCol = true
			}
		case "widths":
			parts := strings.Split(v, ",")
			ws := make([]float32, len(parts))
			for idx, p := range parts {
				p = strings.TrimSuffix(strings.TrimSpace(p), "px")
				if f, err := strconv.ParseFloat(p, 32); err == nil && f > 0 {
					ws[idx] = float32(f)
				}
			}
			meta.widths = ws
		case "coltints":
			meta.colTints = parseTableTints(v)
		case "rowtints":
			meta.rowTints = parseTableTints(v)
		case "celltints":
			meta.cellTints = parseTableCellTints(v)
		case "rowaligns":
			meta.rowAligns = parseTableAligns(v)
		case "cellaligns":
			meta.cellAligns = parseTableCellAligns(v)
		}
	}
	return meta, true
}

func applyTableMeta(td *model.TableData, meta tableMeta) {
	if td == nil {
		return
	}
	if meta.noHeaderRow {
		td.NoHeaderRow = true
	}
	if meta.headerCol {
		td.HeaderCol = true
	}
	if len(meta.widths) > 0 {
		td.Widths = meta.widths
	}
	// Apply legacy/compact tints in order: column -> row -> per-cell override.
	for c, ct := range meta.colTints {
		if ct == model.TableTintNone {
			continue
		}
		for r := range td.Rows {
			if c < len(td.Rows[r]) {
				td.Rows[r][c].Tint = ct
			}
		}
	}
	for r, rt := range meta.rowTints {
		if rt == model.TableTintNone || r >= len(td.Rows) {
			continue
		}
		for c := range td.Rows[r] {
			td.Rows[r][c].Tint = rt
		}
	}
	for _, ct := range meta.cellTints {
		if ct.row < len(td.Rows) && ct.col < len(td.Rows[ct.row]) {
			td.Rows[ct.row][ct.col].Tint = ct.tint
		}
	}
	// Apply alignment overrides in order: row -> per-cell override, then strip redundant ones.
	for r, ra := range meta.rowAligns {
		if ra == model.ColAlignNone || r >= len(td.Rows) {
			continue
		}
		for c := range td.Rows[r] {
			td.Rows[r][c].Align = ra
		}
	}
	for _, ca := range meta.cellAligns {
		if ca.row < len(td.Rows) && ca.col < len(td.Rows[ca.row]) {
			td.Rows[ca.row][ca.col].Align = ca.align
		}
	}
	for r := range td.Rows {
		for c := range td.Rows[r] {
			if td.Rows[r][c].Align != model.ColAlignNone && td.Rows[r][c].Align == td.ColBaseAlign(c) {
				td.Rows[r][c].Align = model.ColAlignNone
			}
		}
	}
}

func parseImageMetaComment(line string) (imageMeta, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return imageMeta{}, false
	}
	inner := strings.TrimSpace(s[4 : len(s)-3])
	var rest string
	switch {
	case strings.HasPrefix(inner, "thinkly:image"):
		rest = strings.TrimSpace(inner[len("thinkly:image"):])
	case strings.HasPrefix(inner, "image:"):
		rest = strings.TrimSpace(inner[len("image:"):])
	default:
		return imageMeta{}, false
	}
	var meta imageMeta
	for _, field := range strings.Fields(rest) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch strings.ToLower(k) {
		case "width", "w":
			meta.width = parseImageWidth(v)
		case "align":
			meta.align = parseImageAlign(v)
		case "crop":
			meta.crop = parseImageCrop(v)
		}
	}
	return meta, true
}

func parseImageWidth(raw string) float32 {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return 0
	}
	isPct := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSuffix(s, "px")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil || f <= 0 {
		return 0
	}
	if isPct || f <= 100 {
		if f < 10 {
			return 10
		}
		if f > 100 {
			return 100
		}
		return float32(f)
	}
	// Pixel width on 672px standard editor column -> percentage 10..100.
	pct := float32(f) / 672.0 * 100.0
	if pct < 10 {
		pct = 10
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

func parseImageAlign(raw string) model.ImageAlign {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "center":
		return model.ImageAlignCenter
	case "right":
		return model.ImageAlignRight
	case "left":
		return model.ImageAlignLeft
	}
	return ""
}

func parseImageCrop(raw string) model.ImageCrop {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return model.ImageCrop{}
	}
	var vals [4]float32
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return model.ImageCrop{}
		}
		vals[i] = float32(f)
	}
	return model.ImageCrop{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: vals[3]}
}

func parseImageLineWithDefs(line string, defs map[string]refDef, used map[string]bool) (model.Block, bool) {
	if b, ok := parseImageLine(line); ok {
		return b, true
	}
	if len(defs) == 0 {
		return model.Block{}, false
	}
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "![") || strings.ContainsRune(s, '\n') {
		return model.Block{}, false
	}
	st := newInlineScanState(s)
	alt, dest, title, refKey, consumed, ok := tryParseLinkOrImageWithTitleRefAt(st, 1, defs, true)
	if !ok || refKey == "" || 1+consumed != len(s) || dest == "" {
		return model.Block{}, false
	}
	if used != nil {
		used[refKey] = true
	}
	return model.Block{
		ID:         model.NewBlockID(),
		Kind:       model.BlockImage,
		ImageSrc:   dest,
		ImageAlt:   plainImageAltFromMarkdown(alt),
		ImageTitle: title,
		ImageAlign: model.ImageAlignLeft,
	}, true
}

func parseImageLine(line string) (model.Block, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.ContainsRune(s, '\n') {
		return model.Block{}, false
	}
	if strings.HasPrefix(s, "<") {
		return parseHTMLImageLine(s)
	}
	if !strings.HasPrefix(s, "![") || !strings.HasSuffix(s, ")") {
		return model.Block{}, false
	}
	st := newInlineScanState(s)
	alt, dest, title, _, consumed, ok := tryParseLinkOrImageWithTitleRefAt(st, 1, nil, true)
	if !ok || 1+consumed != len(s) || dest == "" {
		return model.Block{}, false
	}
	return model.Block{
		ID:         model.NewBlockID(),
		Kind:       model.BlockImage,
		ImageSrc:   dest,
		ImageAlt:   plainImageAltFromMarkdown(alt),
		ImageTitle: title,
		ImageAlign: model.ImageAlignLeft,
	}, true
}

func plainImageAltFromMarkdown(alt string) string {
	if alt == "" {
		return ""
	}
	inner := parseInlineWithDefs(alt, nil)
	return model.PlainTextOfSpans(inner)
}

func parseHTMLImageLine(s string) (model.Block, bool) {
	name, attrs, closed, _, consumed, ok := parseTagRaw(s)
	if !ok || closed {
		return model.Block{}, false
	}
	align := model.ImageAlignLeft
	rest := strings.TrimSpace(s[consumed:])
	closing := ""
	if name == "p" || name == "div" {
		if a := attrVal(attrs, "align"); a != "" {
			if parsed := parseImageAlign(a); parsed != "" {
				align = parsed
			}
		} else if ta := cssProp(attrs, "text-align"); ta != "" {
			if parsed := parseImageAlign(ta); parsed != "" {
				align = parsed
			}
		}
		closing = "</" + name + ">"
		name, attrs, closed, _, consumed, ok = parseTagRaw(rest)
		if !ok || closed {
			return model.Block{}, false
		}
		rest = strings.TrimSpace(rest[consumed:])
	}
	if name != "img" && name != "video" {
		return model.Block{}, false
	}
	if name == "video" {
		if closing != "" {
			restLower := strings.ToLower(rest)
			endVideo := strings.Index(restLower, "</video>")
			if endVideo < 0 {
				return model.Block{}, false
			}
			rest = strings.TrimSpace(rest[endVideo+len("</video>"):])
			if !strings.EqualFold(rest, closing) {
				return model.Block{}, false
			}
		} else if rest != "" && !strings.EqualFold(rest, "</video>") {
			return model.Block{}, false
		}
	} else if closing != "" {
		if !strings.EqualFold(rest, closing) {
			return model.Block{}, false
		}
	} else if rest != "" {
		return model.Block{}, false
	}
	src := strings.TrimSpace(attrVal(attrs, "src"))
	if src == "" {
		return model.Block{}, false
	}
	alt := attrVal(attrs, "alt")
	if alt == "" && name == "video" {
		alt = attrVal(attrs, "title")
	}
	w := parseImageWidth(attrVal(attrs, "width"))
	if w == 0 {
		if sw := cssProp(attrs, "width"); sw != "" {
			w = parseImageWidth(sw)
		}
	}
	if align == model.ImageAlignLeft {
		if a := attrVal(attrs, "align"); a != "" {
			if parsed := parseImageAlign(a); parsed != "" {
				align = parsed
			}
		}
	}
	crop := parseImageCrop(attrVal(attrs, "data-crop"))
	return model.Block{
		ID:         model.NewBlockID(),
		Kind:       model.BlockImage,
		ImageSrc:   src,
		ImageAlt:   alt,
		ImageTitle: attrVal(attrs, "title"),
		ImageWidth: w,
		ImageAlign: align,
		ImageCrop:  crop,
	}, true
}

// List marker classification.
type listMarker struct {
	kind        model.BlockKind
	checked     bool
	indent      int
	markerWidth int
	start       int
	body        string
}

func classifyListMarker(line string) (listMarker, bool) {
	if isThematicBreak(line) {
		return listMarker{}, false
	}
	indent, body := leadingColumns(line)
	for _, tc := range []struct {
		prefix  string
		checked bool
	}{
		{"- [ ] ", false}, {"- [x] ", true}, {"- [X] ", true},
		{"+ [ ] ", false}, {"+ [x] ", true}, {"+ [X] ", true},
		{"* [ ] ", false}, {"* [x] ", true}, {"* [X] ", true},
	} {
		if strings.HasPrefix(body, tc.prefix) {
			return listMarker{
				kind:        model.BlockTodo,
				checked:     tc.checked,
				indent:      indent,
				markerWidth: 2,
				body:        body[len(tc.prefix):],
			}, true
		}
	}
	trimmedEnd := strings.TrimRight(body, " \t")
	for _, tc := range []struct {
		exact   string
		checked bool
	}{
		{"- [ ]", false}, {"- [x]", true}, {"- [X]", true},
		{"+ [ ]", false}, {"+ [x]", true}, {"+ [X]", true},
		{"* [ ]", false}, {"* [x]", true}, {"* [X]", true},
	} {
		if trimmedEnd == tc.exact {
			return listMarker{
				kind:        model.BlockTodo,
				checked:     tc.checked,
				indent:      indent,
				markerWidth: 2,
				body:        "",
			}, true
		}
	}
	for _, p := range []string{"- ", "+ ", "* ", "-\t", "+\t", "*\t"} {
		if strings.HasPrefix(body, p) {
			return listMarker{
				kind:        model.BlockBullet,
				indent:      indent,
				markerWidth: 2,
				body:        body[len(p):],
			}, true
		}
	}
	if body == "-" || body == "+" || body == "*" {
		return listMarker{
			kind:        model.BlockBullet,
			indent:      indent,
			markerWidth: 2,
			body:        "",
		}, true
	}
	digits := 0
	for digits < len(body) && body[digits] >= '0' && body[digits] <= '9' {
		digits++
	}
	if digits >= 1 && digits <= 9 {
		rest := body[digits:]
		startNum, err := strconv.Atoi(body[:digits])
		if err == nil {
			for _, sep := range []string{". ", ") ", ".\t", ")\t"} {
				if strings.HasPrefix(rest, sep) {
					return listMarker{
						kind:        model.BlockNumbered,
						indent:      indent,
						markerWidth: digits + 2,
						start:       startNum,
						body:        rest[len(sep):],
					}, true
				}
			}
			if rest == "." || rest == ")" {
				return listMarker{
					kind:        model.BlockNumbered,
					indent:      indent,
					markerWidth: digits + 2,
					start:       startNum,
					body:        "",
				}, true
			}
		}
	}
	return listMarker{}, false
}

func isPipeDelimiterLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.ContainsRune(trimmed, '-') || !strings.ContainsRune(trimmed, '|') {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case ' ', '\t', '-', ':', '|':
		default:
			return false
		}
	}
	return true
}

func isPipeCandidate(lines []string, idx int) bool {
	if idx+1 >= len(lines) {
		return false
	}
	line := trimLineEnding(lines[idx])
	if !strings.ContainsRune(line, '|') {
		return false
	}
	next := trimLineEnding(lines[idx+1])
	trimmed := strings.TrimSpace(next)
	if !strings.ContainsRune(trimmed, '-') {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case ' ', '\t', '-', ':', '|':
		default:
			return false
		}
	}
	return true
}

func trimTableCellSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r != '\u0085' && unicode.IsSpace(r)
	})
}

func splitPipeCells(line string) []string {
	var cells []string
	walkPipeCells(line, func(_, start, end int) {
		cells = append(cells, trimTableCellSpace(line[start:end]))
	})
	return cells
}

func walkPipeCells(line string, visit func(col, start, end int)) {
	left := strings.TrimLeftFunc(line, unicode.IsSpace)
	start := len(line) - len(left)
	s := strings.TrimRightFunc(left, unicode.IsSpace)
	if s == "" || s == "|" {
		return
	}
	if strings.HasPrefix(s, "|") {
		s = s[1:]
		start++
	}
	if strings.HasSuffix(s, "|") {
		slashes := 0
		for i := len(s) - 2; i >= 0 && s[i] == '\\'; i-- {
			slashes++
		}
		if slashes%2 == 0 {
			s = s[:len(s)-1]
		}
	}
	col := 0
	prev := 0
	escaped := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == '|' && !escaped {
			visit(col, start+prev, start+i)
			col++
			prev = i + 1
		}
		escaped = b == '\\' && !escaped
	}
	visit(col, start+prev, start+len(s))
}

const maxParsedTableCells = 100_000

func isPipeTableRowLine(raw string) bool {
	if strings.TrimSpace(raw) == "" || !strings.ContainsRune(raw, '|') {
		return false
	}
	if _, _, _, _, _, isFence := parseFenceAnyIndent(raw); isFence {
		return false
	}
	if isThematicBreak(raw) {
		return false
	}
	if _, _, ok := classifyHeading(raw); ok {
		return false
	}
	if strings.HasPrefix(strings.TrimLeft(raw, " "), ">") {
		return false
	}
	return true
}

func parsePipeTable(lines []string, defs map[string]refDef, usedOut map[string]bool) (model.Block, int, bool) {
	if len(lines) < 2 {
		return model.Block{}, 0, false
	}
	headerRaw := splitPipeCells(trimLineEnding(lines[0]))
	delimRaw := splitPipeCells(trimLineEnding(lines[1]))
	if len(headerRaw) == 0 || len(headerRaw) > maxParsedTableCells || len(headerRaw) != len(delimRaw) {
		return model.Block{}, 0, false
	}
	aligns := make([]model.ColAlign, len(delimRaw))
	for i, d := range delimRaw {
		d = strings.TrimSpace(d)
		if d == "" {
			return model.Block{}, 0, false
		}
		leftColon := strings.HasPrefix(d, ":")
		rightColon := strings.HasSuffix(d, ":")
		core := strings.TrimSuffix(strings.TrimPrefix(d, ":"), ":")
		if len(core) == 0 || strings.Trim(core, "-") != "" {
			return model.Block{}, 0, false
		}
		switch {
		case leftColon && rightColon:
			aligns[i] = model.ColAlignCenter
		case rightColon:
			aligns[i] = model.ColAlignRight
		case leftColon:
			aligns[i] = model.ColAlignLeft
		default:
			aligns[i] = model.ColAlignNone
		}
	}

	cols := len(headerRaw)
	var rows [][]model.TableCell
	makeRow := func(rawCells []string) []model.TableCell {
		if len(rawCells) > cols {
			cols = len(rawCells)
		}
		row := make([]model.TableCell, len(rawCells))
		for c, cellStr := range rawCells {
			unesc := strings.ReplaceAll(cellStr, `\|`, "|")
			row[c] = model.TableCell{Spans: parseInlineWithDefsAndUsed(unesc, defs, usedOut)}
		}
		return row
	}

	rows = append(rows, makeRow(headerRaw))
	end := 2
	for end < len(lines) {
		raw := trimLineEnding(lines[end])
		if !isPipeTableRowLine(raw) {
			break
		}
		cells := splitPipeCells(raw)
		if len(rows)+1 > maxParsedTableCells/max(len(cells), cols) {
			source := strings.Join(lines[:end+1], "")
			return model.Block{
				ID: model.NewBlockID(), Kind: model.BlockCode,
				Code: trimLineEnding(source), CleanSource: source,
			}, end + 1, true
		}
		rows = append(rows, makeRow(cells))
		end++
	}

	for len(aligns) < cols {
		aligns = append(aligns, model.ColAlignNone)
	}
	for r := range rows {
		for len(rows[r]) < cols {
			rows[r] = append(rows[r], model.TableCell{})
		}
	}

	return model.Block{
		ID:   model.NewBlockID(),
		Kind: model.BlockTable,
		Table: &model.TableData{
			Aligns:  aligns,
			Rows:    rows,
			WasHTML: false,
		},
		CleanSource: strings.Join(lines[:end], ""),
	}, end, true
}

func htmlTableLineCount(lines []string) int {
	depth := 0
	for i, line := range lines {
		lower := strings.ToLower(line)
		rest := lower
		for len(rest) > 0 {
			at := strings.IndexByte(rest, '<')
			if at < 0 {
				break
			}
			rest = rest[at:]
			if strings.HasPrefix(rest, "</table>") {
				if depth > 0 {
					depth--
				}
				rest = rest[8:]
				if depth == 0 {
					return i + 1
				}
			} else if strings.HasPrefix(rest, "<table") && len(rest) > 6 && (rest[6] == '>' || rest[6] == '/' || rest[6] == ' ' || rest[6] == '\t') {
				depth++
				rest = rest[6:]
			} else {
				rest = rest[1:]
			}
		}
	}
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), "</table>") {
			return i + 1
		}
	}
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			return i
		}
	}
	return len(lines)
}

func parseHTMLTable(src string) (model.TableData, bool) {
	return parseHTMLTableCells(src, nil)
}

func parseHTMLTableCells(src string, visit func(row, col, start, end int)) (model.TableData, bool) {
	lower := strings.ToLower(src)
	if !strings.Contains(lower, "<table") {
		return model.TableData{}, false
	}
	var widths []float32
	var aligns []model.ColAlign
	var rows [][]model.TableCell
	var curRow []model.TableCell
	isThinklyV1 := false
	noHeaderRow := false
	explicitHeaderAttr := false
	headerCol := false
	inRow := false
	inThead := false
	anyRow0Header := false
	row0AllHeader := true
	i := 0
	for i < len(src) {
		if src[i] != '<' {
			i++
			continue
		}
		name, attrs, closed, empty, consumed, ok := parseTagRaw(src[i:])
		if !ok {
			i++
			continue
		}
		if closed {
			if name == "thead" {
				inThead = false
			}
			if name == "tr" && inRow {
				if len(curRow) > 0 {
					rows = append(rows, curRow)
				}
				curRow = nil
				inRow = false
			}
			i += consumed
			continue
		}
		switch name {
		case "thead":
			if !empty {
				inThead = true
			}
			i += consumed
		case "table":
			if attrVal(attrs, "data-thinkly-table") != "" {
				isThinklyV1 = true
			}
			if v := attrVal(attrs, "data-header-rows"); v != "" {
				explicitHeaderAttr = true
				if v == "0" {
					noHeaderRow = true
				}
			}
			if attrVal(attrs, "data-header-cols") == "1" {
				headerCol = true
			}
			i += consumed
		case "col":
			if wStr := cssProp(attrs, "width"); wStr != "" {
				wStr = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(wStr)), "px")
				if wf, err := strconv.ParseFloat(wStr, 32); err == nil && wf > 0 {
					widths = append(widths, float32(wf))
				}
			} else if wAttr := attrVal(attrs, "width"); wAttr != "" {
				wAttr = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(wAttr)), "px")
				if wf, err := strconv.ParseFloat(wAttr, 32); err == nil && wf > 0 {
					widths = append(widths, float32(wf))
				}
			}
			i += consumed
		case "tr":
			if inRow && len(curRow) > 0 {
				rows = append(rows, curRow)
			}
			curRow = nil
			inRow = true
			i += consumed
		case "td", "th":
			if !inRow {
				inRow = true
			}
			isHdr := name == "th" || inThead
			if len(rows) == 0 {
				if isHdr {
					anyRow0Header = true
				} else {
					row0AllHeader = false
				}
			}
			if empty {
				curRow = append(curRow, model.TableCell{Header: isHdr})
				i += consumed
				continue
			}
			bodyStart := i + consumed
			bodyEnd, nextIdx := findHTMLCellClose(src, bodyStart, name)
			if visit != nil {
				visit(len(rows), len(curRow), bodyStart, bodyEnd)
			}
			inner := src[bodyStart:bodyEnd]
			spans := ParseInline(trimTableCellSpace(inner))
			cellAlign := model.ColAlignNone
			if ta := cssProp(attrs, "text-align"); ta != "" {
				cellAlign = parseColAlign(ta)
			} else if a := attrVal(attrs, "align"); a != "" {
				cellAlign = parseColAlign(a)
			}
			colIdx := len(curRow)
			if len(rows) == 0 {
				for len(aligns) <= colIdx {
					aligns = append(aligns, model.ColAlignNone)
				}
				aligns[colIdx] = cellAlign
				cellAlign = model.ColAlignNone
			} else if colIdx < len(aligns) {
				base := aligns[colIdx]
				if base == model.ColAlignNone {
					base = model.ColAlignLeft
				}
				if cellAlign == base {
					cellAlign = model.ColAlignNone
				}
			}
			curRow = append(curRow, model.TableCell{Spans: spans, Align: cellAlign, Header: isHdr})
			i = nextIdx
		default:
			i += consumed
		}
	}
	if inRow && len(curRow) > 0 {
		rows = append(rows, curRow)
	}
	if len(rows) == 0 {
		return model.TableData{}, false
	}
	if (!anyRow0Header || !row0AllHeader) && !isThinklyV1 && !explicitHeaderAttr {
		noHeaderRow = true
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 || len(rows) > maxParsedTableCells/cols {
		return model.TableData{}, false
	}
	for r := range rows {
		for len(rows[r]) < cols {
			rows[r] = append(rows[r], model.TableCell{})
		}
	}
	for len(aligns) < cols {
		aligns = append(aligns, model.ColAlignNone)
	}
	return model.TableData{
		Aligns:      aligns,
		Widths:      widths,
		NoHeaderRow: noHeaderRow,
		HeaderCol:   headerCol,
		Rows:        rows,
		WasHTML:     !isThinklyV1,
	}, true
}

func findHTMLCellClose(src string, start int, cellTag string) (bodyEnd, nextIdx int) {
	depth := 0
	i := start
	for i < len(src) {
		if src[i] != '<' {
			i++
			continue
		}
		name, _, closed, _, consumed, ok := parseTagRaw(src[i:])
		if !ok {
			i++
			continue
		}
		if name == "table" {
			if closed {
				if depth > 0 {
					depth--
				} else {
					return i, i
				}
			} else {
				depth++
			}
		} else if depth == 0 {
			if closed && (name == cellTag || name == "tr" || name == "thead" || name == "tbody" || name == "tfoot") {
				if name == cellTag {
					return i, i + consumed
				}
				return i, i
			}
			if !closed && (name == "td" || name == "th" || name == "tr") {
				return i, i
			}
		}
		i += consumed
	}
	return len(src), len(src)
}

func parseColAlign(s string) model.ColAlign {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "center":
		return model.ColAlignCenter
	case "right":
		return model.ColAlignRight
	case "left":
		return model.ColAlignLeft
	}
	return model.ColAlignNone
}

func rawHTMLLineCount(lines []string) (int, bool) {
	if len(lines) == 0 {
		return 0, false
	}
	first := lines[0]
	body := strings.TrimLeft(first, " ")
	if len(first)-len(body) > 3 || !strings.HasPrefix(body, "<") {
		return 0, false
	}
	if strings.HasPrefix(body, "<!--") {
		for i, l := range lines {
			if strings.Contains(l, "-->") {
				return i + 1, true
			}
		}
		return len(lines), true
	}
	if strings.HasPrefix(body, "<?") {
		for i, l := range lines {
			if strings.Contains(l, "?>") {
				return i + 1, true
			}
		}
		return len(lines), true
	}
	if strings.HasPrefix(body, "<!") {
		for i, l := range lines {
			if strings.ContainsRune(l, '>') {
				return i + 1, true
			}
		}
		return len(lines), true
	}
	lower := strings.ToLower(body)
	for _, tag := range []string{"pre", "script", "style", "svg", "iframe"} {
		prefix := "<" + tag
		if strings.HasPrefix(lower, prefix) {
			rest := lower[len(prefix):]
			if rest == "" || rest[0] == '>' || rest[0] == '/' || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r' || rest[0] == '\n' {
				if strings.HasPrefix(rest, "/>") || (strings.Contains(rest, "/>") && !strings.ContainsRune(rest, '>')) {
					return 1, true
				}
				closeTag := "</" + tag + ">"
				for i, l := range lines {
					if strings.Contains(strings.ToLower(l), closeTag) {
						return i + 1, true
					}
				}
				for i := 1; i < len(lines); i++ {
					if strings.TrimSpace(lines[i]) == "" {
						return i, true
					}
				}
				return len(lines), true
			}
		}
	}
	return 0, false
}

func htmlBlockTagKind(name string) (model.BlockKind, bool) {
	switch strings.ToLower(name) {
	case "p", "div", "center", "section", "article", "header", "footer",
		"main", "aside", "nav", "figure", "figcaption", "dl", "ul", "ol":
		return model.BlockParagraph, true
	case "blockquote", "details":
		return model.BlockQuote, true
	case "h1":
		return model.BlockHeading1, true
	case "h2":
		return model.BlockHeading2, true
	case "h3":
		return model.BlockHeading3, true
	case "h4":
		return model.BlockHeading4, true
	case "h5":
		return model.BlockHeading5, true
	case "h6":
		return model.BlockHeading6, true
	}
	return 0, false
}

func parseMultiLineHTMLAnchorBlock(lines []string, defs map[string]refDef, usedOut map[string]bool) (model.Block, int, bool) {
	if len(lines) < 2 {
		return model.Block{}, 0, false
	}
	first := trimLineEnding(lines[0])
	body := strings.TrimLeft(first, " ")
	if len(first)-len(body) > 3 || !strings.HasPrefix(strings.ToLower(body), "<a") {
		return model.Block{}, 0, false
	}
	name, _, closed, empty, consumed, ok := parseTagRaw(body)
	if !ok || closed || empty || name != "a" || strings.Contains(strings.ToLower(body[consumed:]), "</a>") {
		return model.Block{}, 0, false
	}
	end := -1
	for j := 1; j < len(lines); j++ {
		ln := trimLineEnding(lines[j])
		if strings.TrimSpace(ln) == "" {
			break
		}
		if strings.Contains(strings.ToLower(ln), "</a>") {
			end = j + 1
			break
		}
	}
	if end < 0 {
		return model.Block{}, 0, false
	}
	var joined strings.Builder
	for j := 0; j < end; j++ {
		part := strings.TrimSpace(trimLineEnding(lines[j]))
		if part == "" {
			continue
		}
		if joined.Len() > 0 && !strings.HasSuffix(joined.String(), ">") && !strings.HasPrefix(part, "<") {
			joined.WriteByte(' ')
		}
		joined.WriteString(part)
	}
	orig := strings.Join(lines[:end], "")
	return model.Block{
		ID:          model.NewBlockID(),
		Kind:        model.BlockParagraph,
		Spans:       parseInlineWithDefsAndUsed(joined.String(), defs, usedOut),
		CleanSource: orig,
	}, end, true
}

func openHTMLProseTag(line string) (string, bool) {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 || !strings.HasPrefix(body, "<") {
		return "", false
	}
	rest := body[1:]
	lenName := 0
	for lenName < len(rest) && (isASCIIAlpha(rest[lenName]) || (rest[lenName] >= '0' && rest[lenName] <= '9')) {
		lenName++
	}
	if lenName == 0 || lenName >= len(rest) {
		return "", false
	}
	tag := strings.ToLower(rest[:lenName])
	after := rest[lenName]
	if after != ' ' && after != '\t' && after != '>' && after != '/' && after != '\r' && after != '\n' {
		return "", false
	}
	if _, ok := htmlBlockTagKind(tag); ok {
		return tag, true
	}
	return "", false
}

func htmlProseLineCount(lines []string) int {
	target, ok := openHTMLProseTag(lines[0])
	if !ok {
		return 1
	}
	closeNeedle := "</" + target + ">"
	openNeedle := "<" + target
	depth := 0
	for idx, line := range lines {
		if idx > 0 && strings.TrimSpace(line) == "" && target != "details" {
			return idx
		}
		lower := strings.ToLower(line)
		rest := lower
		for len(rest) > 0 {
			at := strings.IndexByte(rest, '<')
			if at < 0 {
				break
			}
			rest = rest[at:]
			if strings.HasPrefix(rest, closeNeedle) {
				depth--
				rest = rest[len(closeNeedle):]
				if depth <= 0 {
					return idx + 1
				}
			} else if strings.HasPrefix(rest, openNeedle) && len(rest) > len(openNeedle) {
				after := rest[len(openNeedle):]
				c := after[0]
				if c == '>' || c == '/' || c == ' ' || c == '\t' || c == '\r' || c == '\n' {
					beforeGt, _, _ := strings.Cut(after, ">")
					if !strings.HasSuffix(beforeGt, "/") {
						depth++
					}
					rest = rest[len(openNeedle):]
				} else {
					rest = rest[1:]
				}
			} else {
				rest = rest[1:]
			}
		}
		if depth <= 0 {
			return idx + 1
		}
	}
	return len(lines)
}

func parseHTMLProseBlock(orig string) model.Block {
	trimmed := strings.TrimSpace(orig)
	name, attrs, closed, empty, consumed, ok := parseTagRaw(trimmed)
	if ok && !closed && !empty {
		kind, known := htmlBlockTagKind(name)
		if known && name != "details" && name != "ul" && name != "ol" && name != "dl" {
			closeTag := "</" + name + ">"
			if strings.HasSuffix(strings.ToLower(trimmed), closeTag) {
				inner := trimmed[consumed : len(trimmed)-len(closeTag)]
				indent := 0
				if name == "p" {
					if di := attrVal(attrs, "data-indent"); di != "" {
						if n, err := strconv.Atoi(strings.TrimSpace(di)); err == nil && n > 0 {
							if n > 12 {
								n = 12
							}
							indent = n
						}
					}
				}
				return model.Block{
					ID:          model.NewBlockID(),
					Kind:        kind,
					Indent:      indent,
					Spans:       ParseInline(inner),
					CleanSource: orig,
				}
			}
		}
	}
	target, _ := openHTMLProseTag(orig)
	kind, _ := htmlBlockTagKind(target)
	return model.Block{
		ID:          model.NewBlockID(),
		Kind:        kind,
		Spans:       ParseInline(trimmed),
		CleanSource: orig,
	}
}

func parseCalloutBlock(lines []string, defs map[string]refDef, usedOut map[string]bool) (model.Block, int, bool) {
	if len(lines) == 0 {
		return model.Block{}, 0, false
	}
	first := trimLineEnding(lines[0])
	trimmed := strings.TrimLeft(first, " ")
	if len(first)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, ">") {
		return model.Block{}, 0, false
	}
	kind, ok := parseAlertTag(trimmed[1:])
	if !ok {
		return model.Block{}, 0, false
	}
	var bodies []string
	end := 1
	for end < len(lines) {
		line := trimLineEnding(lines[end])
		rest := strings.TrimLeft(line, " ")
		if len(line)-len(rest) > 3 || !strings.HasPrefix(rest, ">") {
			break
		}
		after := rest[1:]
		body := strings.TrimPrefix(after, " ")
		if _, isNextAlert := parseAlertTag(body); isNextAlert {
			break
		}
		bodies = append(bodies, body)
		end++
	}
	var spans []model.Span
	for i, body := range bodies {
		if i > 0 {
			spans = append(spans, model.Span{Text: "\n"})
		}
		if body != "" {
			spans = append(spans, parseInlineWithDefsAndUsed(body, defs, usedOut)...)
		}
	}
	spans = coalesceSpans(spans)
	return model.Block{
		ID:          model.NewBlockID(),
		Kind:        model.BlockCallout,
		Callout:     kind,
		Spans:       spans,
		CleanSource: strings.Join(lines[:end], ""),
	}, end, true
}

func parseCustomContainerBlock(lines []string, defs map[string]refDef, usedOut map[string]bool) (model.Block, int, bool) {
	if len(lines) == 0 {
		return model.Block{}, 0, false
	}
	first := strings.TrimLeft(trimLineEnding(lines[0]), " \t")
	if !strings.HasPrefix(first, ":::") {
		return model.Block{}, 0, false
	}
	header := strings.TrimSpace(first[3:])
	if header == "" && strings.TrimSpace(first) == ":::" {
		return model.Block{}, 0, false
	}
	closed := false
	end := len(lines)
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == ":::" {
			closed = true
			end = i + 1
			break
		}
	}
	if !closed {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "" {
				end = i
				break
			}
		}
	}
	bodyEnd := end
	if closed {
		bodyEnd = end - 1
	}
	fields := strings.Fields(header)
	tag := ""
	restTitle := ""
	if len(fields) > 0 {
		tag = strings.ToLower(fields[0])
		restTitle = strings.TrimSpace(header[len(fields[0]):])
	}
	var callout model.CalloutKind
	switch tag {
	case "note", "info":
		callout = model.CalloutNote
	case "tip", "success":
		callout = model.CalloutTip
	case "important":
		callout = model.CalloutImportant
	case "warning", "warn":
		callout = model.CalloutWarning
	case "caution", "danger", "error":
		callout = model.CalloutCaution
	}
	kind := model.BlockQuote
	if callout != "" {
		kind = model.BlockCallout
	}
	title := header
	if callout != "" {
		title = restTitle
	}
	var spans []model.Span
	if title != "" {
		tSpans := parseInlineWithDefsAndUsed(title, defs, usedOut)
		for i := range tSpans {
			tSpans[i].Marks |= model.MarkBold
		}
		spans = append(spans, tSpans...)
	}
	for i := 1; i < bodyEnd; i++ {
		body := strings.TrimSpace(trimLineEnding(lines[i]))
		if body == "" {
			continue
		}
		if len(spans) > 0 {
			spans = append(spans, model.Span{Text: "\n"})
		}
		spans = append(spans, parseInlineWithDefsAndUsed(body, defs, usedOut)...)
	}
	return model.Block{
		ID:          model.NewBlockID(),
		Kind:        kind,
		Callout:     callout,
		Spans:       coalesceSpans(spans),
		CleanSource: strings.Join(lines[:end], ""),
	}, end, true
}

func blockquoteNeedsStructuredParse(inner []string) bool {
	for i, line := range inner {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			if i > 0 && i+1 < len(inner) {
				return true
			}
			continue
		}
		if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") ||
			strings.HasPrefix(trimmed, "|") || isThematicBreak(line) {
			return true
		}
		if _, ok := classifyListMarker(line); ok {
			return true
		}
	}
	return false
}

func parseBlockquoteBlocks(lines []string, defs map[string]refDef, usedOut map[string]bool, startLine, depth int) ([]model.Block, int, bool) {
	if len(lines) == 0 {
		return nil, 0, false
	}
	first := trimLineEnding(lines[0])
	trimmed := strings.TrimLeft(first, " ")
	if len(first)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, ">") {
		return nil, 0, false
	}
	var inner []string
	end := 0
	for end < len(lines) {
		line := trimLineEnding(lines[end])
		rest := strings.TrimLeft(line, " ")
		if len(line)-len(rest) > 3 || !strings.HasPrefix(rest, ">") {
			break
		}
		body := strings.TrimPrefix(rest[1:], " ")
		if end > 0 {
			if _, isAlert := parseAlertTag(body); isAlert {
				break
			}
		}
		inner = append(inner, body)
		end++
	}
	if end == 0 {
		return nil, 0, false
	}
	origSrc := strings.Join(lines[:end], "")
	if blockquoteNeedsStructuredParse(inner) {
		var innerBuf strings.Builder
		for _, l := range inner {
			innerBuf.WriteString(l)
			innerBuf.WriteByte('\n')
		}
		children := swallowSeparators(parseBlocksWithDefsAndUsedAt(innerBuf.String(), defs, nil, usedOut, startLine, depth+1))
		var out []model.Block
		prevEndLine := startLine - 1
		for _, ch := range children {
			if isBlankBlock(&ch) && len(children) > 1 {
				continue
			}
			ch.QuoteDepth++
			if ch.Kind == model.BlockParagraph && ch.Indent == 0 {
				ch.Kind = model.BlockQuote
			}
			if ch.CleanSource != "" &&
				ch.SourceLine >= startLine &&
				ch.SourceEndLine >= ch.SourceLine &&
				ch.SourceEndLine < startLine+end &&
				ch.SourceLine > prevEndLine {
				ch.CleanSource = strings.Join(lines[ch.SourceLine-startLine:ch.SourceEndLine-startLine+1], "")
				prevEndLine = ch.SourceEndLine
			} else {
				ch.CleanSource = ""
			}
			out = append(out, ch)
		}
		if len(out) == 1 {
			out[0].CleanSource = origSrc
		}
		if len(out) > 0 {
			out[0].QuoteNewGroup = true
			for i := 1; i < len(out); i++ {
				out[i].QuoteNewGroup = false
				if out[i].QuoteDepth > out[i-1].QuoteDepth &&
					out[i].SourceLine > 0 && out[i-1].SourceEndLine > 0 &&
					out[i].SourceLine == out[i-1].SourceEndLine+1 {
					out[i].QuoteTightBefore = true
				}
			}
			return out, end, true
		}
	}
	var spans []model.Span
	for i, body := range inner {
		if i > 0 {
			spans = append(spans, model.Span{Text: "\n"})
		}
		if body != "" {
			spans = append(spans, parseInlineWithDefsAndUsed(body, defs, usedOut)...)
		}
	}
	blk := model.Block{
		ID:            model.NewBlockID(),
		Kind:          model.BlockQuote,
		QuoteDepth:    1,
		QuoteNewGroup: true,
		Spans:         coalesceSpans(spans),
		CleanSource:   origSrc,
		SourceLine:    startLine,
		SourceEndLine: startLine + end - 1,
	}
	absorbDecoratorQuote(&blk)
	return []model.Block{blk}, end, true
}

func isEscapedAt(s string, idx int) bool {
	bs := 0
	for i := idx - 1; i >= 0 && s[i] == '\\'; i-- {
		bs++
	}
	return bs%2 == 1
}

func isValidDecoratorSegment(seg string, allowLeadingDigit bool) bool {
	if seg == "" || len(seg) > 48 {
		return false
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if i == 0 && !allowLeadingDigit && (c < 'a' || c > 'z') {
			return false
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func decoratorFromSource(src string) (prefix, kind string, ok bool) {
	pos := 0
	for pos < len(src) {
		rel := strings.Index(src[pos:], "<!--")
		if rel < 0 {
			return "", "", false
		}
		start := pos + rel
		if isEscapedAt(src, start) {
			pos = start + 4
			continue
		}
		endRel := strings.Index(src[start+4:], "-->")
		if endRel < 0 {
			return "", "", false
		}
		inner := strings.TrimSpace(src[start+4 : start+4+endRel])
		pos = start + 4 + endRel + 3
		fields := strings.Fields(inner)
		if len(fields) == 0 {
			continue
		}
		parts := strings.Split(fields[0], ":")
		if len(parts) < 2 || len(parts) > 3 {
			continue
		}
		if !isValidDecoratorSegment(parts[0], false) || !isValidDecoratorSegment(parts[1], true) {
			continue
		}
		if len(parts) == 3 && !isValidDecoratorSegment(parts[2], true) {
			continue
		}
		if len(fields) > 1 {
			metaOK := true
			for _, f := range fields[1:] {
				if !strings.Contains(f, "=") {
					metaOK = false
					break
				}
			}
			if !metaOK {
				continue
			}
		}
		prefix = parts[0] + ":" + parts[1]
		if len(parts) == 3 {
			kind = parts[2]
		}
		return prefix, kind, true
	}
	return "", "", false
}

func absorbDecoratorQuote(b *model.Block) {
	if b == nil || b.Kind != model.BlockQuote {
		return
	}
	prefix, kind, ok := decoratorFromSource(b.CleanSource)
	if !ok {
		return
	}
	b.DecoratorPrefix = prefix
	b.DecoratorKind = kind
	if spans, tagKind := stripDecoratorTagMarkers(b.Spans); tagKind != "" {
		b.Spans = spans
		if b.DecoratorKind == "" {
			b.DecoratorKind = tagKind
		}
	}
}

func decoratorTagCut(line string) (kind string, n int, ok bool) {
	if !strings.HasPrefix(line, "[") {
		return "", 0, false
	}
	end := strings.IndexByte(line, ']')
	if end <= 3 {
		return "", 0, false
	}
	inside := line[1:end]
	colon := strings.IndexByte(inside, ':')
	if colon <= 0 || colon >= len(inside)-1 {
		return "", 0, false
	}
	tag := inside[:colon]
	kind = inside[colon+1:]
	if !isValidDecoratorSegment(tag, false) || !isValidDecoratorSegment(kind, true) {
		return "", 0, false
	}
	n = end + 1
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return kind, n, true
}

func stripDecoratorTagMarkers(spans []model.Span) ([]model.Span, string) {
	text := model.PlainTextOfSpans(spans)
	if !strings.Contains(text, "[") || !strings.Contains(text, ":") {
		return spans, ""
	}
	rs := []rune(text)
	type cut struct{ lo, hi int }
	var cuts []cut
	kind := ""
	lineStart := 0
	for i := 0; i <= len(rs); i++ {
		if i != len(rs) && rs[i] != '\n' {
			continue
		}
		line := string(rs[lineStart:i])
		if marker, n, ok := decoratorTagCut(line); ok {
			if kind == "" {
				kind = marker
			}
			cuts = append(cuts, cut{lineStart, lineStart + len([]rune(line[:n]))})
		}
		lineStart = i + 1
	}
	if kind == "" {
		return spans, ""
	}
	for i := len(cuts) - 1; i >= 0; i-- {
		spans = deleteSpanRunes(spans, cuts[i].lo, cuts[i].hi)
	}
	return coalesceSpans(spans), kind
}

func deleteSpanRunes(spans []model.Span, lo, hi int) []model.Span {
	if hi <= lo {
		return spans
	}
	out := make([]model.Span, 0, len(spans))
	pos := 0
	for _, sp := range spans {
		r := []rune(sp.Text)
		next := pos + len(r)
		if next <= lo || pos >= hi {
			if sp.Text != "" {
				out = append(out, sp)
			}
			pos = next
			continue
		}
		var b strings.Builder
		for i, ch := range r {
			at := pos + i
			if at < lo || at >= hi {
				b.WriteRune(ch)
			}
		}
		sp.Text = b.String()
		if sp.Text != "" {
			out = append(out, sp)
		}
		pos = next
	}
	return out
}

func startsBlockInList(line string, nextRaw string, tail []string) bool {
	if isThematicBreak(line) {
		return true
	}
	if _, ok := classifyListMarker(line); ok {
		return true
	}
	if _, _, _, _, _, ok := parseFenceAnyIndent(line); ok {
		return true
	}
	if _, _, ok := parseInlineMathBlock(line); ok {
		return true
	}
	_, trimmed := leadingColumns(line)
	tEnd := strings.TrimRight(trimmed, " \t")
	if tEnd != "" && (strings.Trim(tEnd, "=") == "" || strings.Trim(tEnd, "-") == "") {
		return true
	}
	if strings.HasPrefix(trimmed, ">") || (strings.HasPrefix(trimmed, ":::") && strings.TrimSpace(trimmed) != ":::") {
		return true
	}
	if _, _, ok := classifyHeading(trimmed); ok {
		return true
	}
	if _, ok := parseImageLine(trimmed); ok {
		return true
	}
	if _, ok := openHTMLProseTag(trimmed); ok {
		return true
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "<table") {
		return true
	}
	if _, ok := rawHTMLLineCount(tail); ok {
		return true
	}
	return strings.ContainsRune(trimmed, '|') && nextRaw != "" && isPipeDelimiterLine(trimLineEnding(nextRaw))
}

func parseListItem(lines []string, defs map[string]refDef, usedOut map[string]bool, startLine, depth int) ([]model.Block, int, bool) {
	if len(lines) == 0 {
		return nil, 0, false
	}
	line0 := trimLineEnding(lines[0])
	marker, ok := classifyListMarker(line0)
	if !ok {
		return nil, 0, false
	}
	contentCol := marker.indent + marker.markerWidth

	// 1. Same-line fenced code block (`- ```lang`).
	if _, _, fc, fw, finfo, isFence := parseFenceAnyIndent(marker.body); isFence {
		var body strings.Builder
		end := 1
		for end < len(lines) {
			nextRaw := lines[end]
			end++
			if closesFenceAnyIndent(trimLineEnding(nextRaw), fc, fw) {
				break
			}
			body.WriteString(stripColumns(nextRaw, contentCol))
		}
		codeText := trimLineEnding(body.String())
		parent := model.Block{
			ID:            model.NewBlockID(),
			Kind:          marker.kind,
			Indent:        marker.indent,
			ListIndex:     marker.start,
			Checked:       marker.checked,
			CleanSource:   lines[0],
			SourceLine:    startLine,
			SourceEndLine: startLine,
		}
		child := model.Block{
			ID:            model.NewBlockID(),
			Kind:          model.BlockCode,
			Indent:        contentCol,
			Language:      finfo,
			Code:          codeText,
			CleanSource:   strings.Join(lines[1:end], ""),
			SourceLine:    startLine + 1,
			SourceEndLine: max(startLine+end-1, startLine+1),
		}
		return []model.Block{parent, child}, end, true
	}

	// 2. Collect continuation lines of the item's first paragraph.
	var paraLines []string
	paraEnd := 1
	if strings.TrimSpace(marker.body) != "" {
		paraLines = append(paraLines, marker.body)
		for paraEnd < len(lines) {
			nextLine := trimLineEnding(lines[paraEnd])
			if strings.TrimSpace(nextLine) == "" {
				break
			}
			nextIndent, trimmed := leadingColumns(nextLine)
			if marker.indent > 0 && nextIndent < marker.indent {
				break
			}
			nextRaw := ""
			if paraEnd+1 < len(lines) {
				nextRaw = lines[paraEnd+1]
			}
			if startsBlockInList(nextLine, nextRaw, lines[paraEnd:]) {
				break
			}
			paraLines = append(paraLines, trimmed)
			paraEnd++
		}
	}

	// 3. Check for indented child blocks.
	firstChild := -1
	for i := paraEnd; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			ci, _ := leadingColumns(trimLineEnding(lines[i]))
			if (i == paraEnd && ci > marker.indent) || (i > paraEnd && ci >= contentCol) {
				firstChild = i
			}
			break
		}
	}

	joinedBody := strings.Join(paraLines, "\n")
	parent := model.Block{
		ID:            model.NewBlockID(),
		Kind:          marker.kind,
		Indent:        marker.indent,
		ListIndex:     marker.start,
		Checked:       marker.checked,
		Spans:         parseInlineWithDefsAndUsed(joinedBody, defs, usedOut),
		SourceLine:    startLine,
		SourceEndLine: startLine + paraEnd - 1,
	}
	if firstChild < 0 {
		parent.CleanSource = strings.Join(lines[:paraEnd], "")
		return []model.Block{parent}, paraEnd, true
	}

	firstChildIndent, _ := leadingColumns(trimLineEnding(lines[firstChild]))
	minAfterBlank := contentCol
	if firstChildIndent < minAfterBlank {
		minAfterBlank = firstChildIndent
	}
	regionEnd := firstChild + 1
	_, _, fc, fw, _, inFence := parseFenceAnyIndent(trimLineEnding(lines[firstChild]))
	for regionEnd < len(lines) {
		line := trimLineEnding(lines[regionEnd])
		if inFence {
			if closesFenceAnyIndent(line, fc, fw) {
				inFence = false
			}
			regionEnd++
			continue
		}
		if strings.TrimSpace(line) == "" {
			regionEnd++
			continue
		}
		col, _ := leadingColumns(line)
		afterBlank := strings.TrimSpace(lines[regionEnd-1]) == ""
		belongs := (afterBlank && col >= minAfterBlank) || (!afterBlank && col > marker.indent)
		if !belongs {
			break
		}
		_, _, fc, fw, _, inFence = parseFenceAnyIndent(line)
		regionEnd++
	}

	contentEnd := firstChild + 1
	for i := regionEnd - 1; i >= firstChild; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			contentEnd = i + 1
			break
		}
	}

	minChildCol := contentCol
	for i := firstChild; i < contentEnd; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		c, _ := leadingColumns(trimLineEnding(lines[i]))
		if c < minChildCol {
			minChildCol = c
		}
	}
	stripCols := minChildCol
	if stripCols < marker.indent+1 {
		stripCols = marker.indent + 1
	}

	var childSource strings.Builder
	for i := firstChild; i < contentEnd; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			childSource.WriteByte('\n')
		} else {
			childSource.WriteString(stripColumns(lines[i], stripCols))
		}
	}
	children := parseBlocksWithDefsAndUsedAt(childSource.String(), defs, nil, usedOut, startLine+firstChild, depth+1)

	parent.CleanSource = strings.Join(lines[:paraEnd], "")
	result := []model.Block{parent}
	for i := paraEnd; i < firstChild; i++ {
		result = append(result, model.Block{
			ID:            model.NewBlockID(),
			Kind:          model.BlockParagraph,
			CleanSource:   lines[i],
			SourceLine:    startLine + i,
			SourceEndLine: startLine + i,
		})
	}
	origIdx := firstChild
	for _, child := range children {
		if !isBlankBlock(&child) {
			child.Indent += stripCols
		}
		nLines := countCleanLines(child.CleanSource)
		if nLines > 0 && origIdx+nLines <= contentEnd {
			child.CleanSource = strings.Join(lines[origIdx:origIdx+nLines], "")
			origIdx += nLines
		} else {
			child.CleanSource = ""
		}
		result = append(result, child)
	}
	return result, contentEnd, true
}

func countCleanLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func parseBlocksWithDefs(src string, defs map[string]refDef, usedLabels map[string]bool) []model.Block {
	return parseBlocksWithDefsAndUsedAt(src, defs, usedLabels, nil, 1, 0)
}

func parseBlocksWithDefsAt(src string, defs map[string]refDef, usedLabels map[string]bool, startLine, depth int) []model.Block {
	return parseBlocksWithDefsAndUsedAt(src, defs, usedLabels, nil, startLine, depth)
}

func parseBlocksWithDefsAndUsedAt(src string, defs map[string]refDef, usedLabels map[string]bool, usedOut map[string]bool, startLine, depth int) []model.Block {
	return parseBlocksWithLinesAt(splitLinesInclusive(src), defs, usedLabels, usedOut, nil, startLine, depth, nil)
}

func parseBlocksWithLinesAt(lines []string, defs map[string]refDef, usedLabels map[string]bool, usedOut map[string]bool, consumedOut map[string]bool, startLine, depth int, refLines *[]int) []model.Block {
	const maxBlockDepth = 48
	if depth >= maxBlockDepth && len(lines) > 0 {
		raw := strings.Join(lines, "")
		return []model.Block{{
			ID: model.NewBlockID(), Kind: model.BlockCode, Code: trimLineEnding(raw),
			CleanSource: raw, SourceLine: startLine, SourceEndLine: startLine + len(lines) - 1,
		}}
	}
	var result []model.Block
	i := 0
	unusedRefDefEnd := -1
	appendBlock := func(b model.Block, count int) {
		b.SourceLine = startLine + i
		b.SourceEndLine = b.SourceLine + count - 1
		result = append(result, b)
	}
	for i < len(lines) {
		rawLine := lines[i]
		line := trimLineEnding(rawLine)

		if i == 0 {
			if b, count, ok := parseFrontmatter(lines); ok {
				appendBlock(b, count)
				i += count
				continue
			}
		}
		if isEmptyBlockComment(line) {
			appendBlock(model.Block{
				ID:          model.NewBlockID(),
				Kind:        model.BlockParagraph,
				CleanSource: rawLine,
			}, 1)
			i++
			continue
		}
		if strings.TrimSpace(line) == "" {
			appendBlock(model.Block{
				ID:          model.NewBlockID(),
				Kind:        model.BlockParagraph,
				CleanSource: rawLine,
			}, 1)
			i++
			continue
		}
		if isThematicBreak(line) {
			appendBlock(model.Block{
				ID:          model.NewBlockID(),
				Kind:        model.BlockDivider,
				CleanSource: rawLine,
			}, 1)
			i++
			continue
		}
		// Image metadata comment immediately followed by an image line.
		if meta, ok := parseImageMetaComment(line); ok && i+1 < len(lines) {
			if img, imgOK := parseImageLineWithDefs(trimLineEnding(lines[i+1]), defs, usedOut); imgOK {
				if meta.width > 0 {
					img.ImageWidth = meta.width
				}
				if meta.align != "" {
					img.ImageAlign = meta.align
				}
				if !meta.crop.IsZero() {
					img.ImageCrop = meta.crop
				}
				img.CleanSource = lines[i] + lines[i+1]
				appendBlock(img, 2)
				i += 2
				continue
			}
		}
		if img, ok := parseImageLineWithDefs(line, defs, usedOut); ok {
			img.CleanSource = rawLine
			appendBlock(img, 1)
			i++
			continue
		}
		if count := consumedRefDefLines(lines[i:], usedLabels, consumedOut); count > 0 {
			if refLines != nil {
				for j := range count {
					*refLines = append(*refLines, startLine+i+j)
				}
			}
			i += count
			continue
		}
		if b, count, ok := parseCalloutBlock(lines[i:], defs, usedOut); ok {
			appendBlock(b, count)
			i += count
			continue
		}
		if qBlocks, count, ok := parseBlockquoteBlocks(lines[i:], defs, usedOut, startLine+i, depth); ok {
			result = append(result, qBlocks...)
			i += count
			continue
		}
		if items, count, ok := parseListItem(lines[i:], defs, usedOut, startLine+i, depth); ok {
			result = append(result, items...)
			i += count
			continue
		}
		if b, count, ok := parseCustomContainerBlock(lines[i:], defs, usedOut); ok {
			appendBlock(b, count)
			i += count
			continue
		}
		if lowerTrim := strings.ToLower(strings.TrimLeft(line, " \t")); strings.HasPrefix(lowerTrim, "<ul") || strings.HasPrefix(lowerTrim, "<ol") {
			if listBlocks, count, ok := parseHTMLListLines(lines[i:], defs, usedOut, startLine+i); ok {
				result = append(result, listBlocks...)
				i += count
				continue
			}
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimLeft(line, " \t")), "<table") {
			count := htmlTableLineCount(lines[i:])
			orig := strings.Join(lines[i:i+count], "")
			if td, ok := parseHTMLTable(orig); ok {
				appendBlock(model.Block{
					ID:          model.NewBlockID(),
					Kind:        model.BlockTable,
					Table:       &td,
					CleanSource: orig,
				}, count)
			} else {
				appendBlock(model.Block{
					ID:          model.NewBlockID(),
					Kind:        model.BlockRawHTML,
					Code:        trimLineEnding(orig),
					CleanSource: orig,
				}, count)
			}
			i += count
			continue
		}
		if meta, ok := parseTableMetaComment(line); ok && i+1 < len(lines) {
			nextTrim := strings.ToLower(strings.TrimLeft(trimLineEnding(lines[i+1]), " \t"))
			if strings.HasPrefix(nextTrim, "<table") {
				count := htmlTableLineCount(lines[i+1:])
				orig := strings.Join(lines[i+1:i+1+count], "")
				if td, tOK := parseHTMLTable(orig); tOK {
					applyTableMeta(&td, meta)
					appendBlock(model.Block{
						ID:          model.NewBlockID(),
						Kind:        model.BlockTable,
						Table:       &td,
						CleanSource: lines[i] + orig,
					}, 1+count)
					i += 1 + count
					continue
				}
			} else if isPipeCandidate(lines, i+1) {
				if b, count, pOK := parsePipeTable(lines[i+1:], defs, usedOut); pOK {
					applyTableMeta(b.Table, meta)
					b.CleanSource = lines[i] + b.CleanSource
					appendBlock(b, 1+count)
					i += 1 + count
					continue
				}
			}
		}
		if count, ok := rawHTMLLineCount(lines[i:]); ok {
			orig := strings.Join(lines[i:i+count], "")
			trimmed := strings.TrimSpace(orig)
			if strings.HasPrefix(strings.ToLower(trimmed), "<pre") {
				appendBlock(model.Block{
					ID:          model.NewBlockID(),
					Kind:        model.BlockCode,
					Code:        stripPreTags(trimmed),
					CleanSource: orig,
				}, count)
			} else {
				appendBlock(model.Block{
					ID:          model.NewBlockID(),
					Kind:        model.BlockRawHTML,
					Code:        trimLineEnding(orig),
					CleanSource: orig,
				}, count)
			}
			i += count
			continue
		}
		if b, count, ok := parseMultiLineHTMLAnchorBlock(lines[i:], defs, usedOut); ok {
			appendBlock(b, count)
			i += count
			continue
		}
		if _, ok := openHTMLProseTag(line); ok {
			count := htmlProseLineCount(lines[i:])
			orig := strings.Join(lines[i:i+count], "")
			appendBlock(parseHTMLProseBlock(orig), count)
			i += count
			continue
		}
		if isPipeCandidate(lines, i) {
			if b, count, ok := parsePipeTable(lines[i:], defs, usedOut); ok {
				appendBlock(b, count)
				i += count
				continue
			}
		}
		if indent, math, ok := parseInlineMathBlock(line); ok {
			if indent > 3 {
				indent = 3
			}
			appendBlock(model.Block{
				ID:          model.NewBlockID(),
				Kind:        model.BlockCode,
				Indent:      indent,
				Language:    "math",
				Code:        math,
				CleanSource: rawLine,
			}, 1)
			i++
			continue
		}
		if indentCols, _, marker, width, info, ok := parseFenceAnyIndent(line); ok {
			var body strings.Builder
			end := i + 1
			for end < len(lines) {
				next := lines[end]
				end++
				if closesFenceAnyIndent(trimLineEnding(next), marker, width) {
					break
				}
				body.WriteString(stripColumns(next, indentCols))
			}
			orig := strings.Join(lines[i:end], "")
			codeText := trimLineEnding(body.String())
			if indentCols > 3 {
				indentCols = 3
			}
			appendBlock(model.Block{
				ID:          model.NewBlockID(),
				Kind:        model.BlockCode,
				Indent:      indentCols,
				Language:    info,
				Code:        codeText,
				CleanSource: orig,
			}, end-i)
			i = end
			continue
		}
		indentCols, bodyTrimmed := leadingColumns(line)
		prevIsProse := false
		if len(result) > 0 {
			prev := &result[len(result)-1]
			if !isBlankBlock(prev) && (prev.Kind == model.BlockParagraph || prev.Kind.IsList() || prev.Kind == model.BlockQuote) {
				prevIsProse = true
			}
		}
		if indentCols >= 4 && !prevIsProse {
			end := i + 1
			for end < len(lines) {
				l := trimLineEnding(lines[end])
				if strings.TrimSpace(l) != "" {
					c, _ := leadingColumns(l)
					if c < 4 {
						break
					}
				}
				end++
			}
			contentEnd := i + 1
			for j := end - 1; j >= i; j-- {
				if strings.TrimSpace(lines[j]) != "" {
					contentEnd = j + 1
					break
				}
			}
			var body strings.Builder
			for j := i; j < contentEnd; j++ {
				if strings.TrimSpace(lines[j]) == "" {
					body.WriteByte('\n')
				} else {
					body.WriteString(stripColumns(lines[j], 4))
				}
			}
			orig := strings.Join(lines[i:contentEnd], "")
			appendBlock(model.Block{
				ID:          model.NewBlockID(),
				Kind:        model.BlockCode,
				Code:        trimLineEnding(body.String()),
				CleanSource: orig,
			}, contentEnd-i)
			i = contentEnd
			continue
		}
		if hKind, hText, ok := classifyHeading(line); ok {
			appendBlock(model.Block{
				ID:          model.NewBlockID(),
				Kind:        hKind,
				Spans:       parseInlineWithDefsAndUsed(hText, defs, usedOut),
				CleanSource: rawLine,
			}, 1)
			i++
			continue
		}
		isUnusedRefDef := i < unusedRefDefEnd
		if !isUnusedRefDef && indentCols <= 3 && len(bodyTrimmed) > 0 && bodyTrimmed[0] == '[' && !strings.HasPrefix(bodyTrimmed, "[^") {
			if _, _, refConsumed, okRef := parseRefDefLines(lines[i:]); okRef {
				isUnusedRefDef = true
				if i+refConsumed > unusedRefDefEnd {
					unusedRefDefEnd = i + refConsumed
				}
			}
		}
		if indentCols <= 3 && i+1 < len(lines) {
			nextLine := trimLineEnding(lines[i+1])
			nextBody := strings.TrimLeft(nextLine, " ")
			if len(nextLine)-len(nextBody) <= 3 {
				nextTrimmed := strings.TrimRight(nextBody, " \t")
				if nextTrimmed != "" {
					var setextKind model.BlockKind
					if strings.Trim(nextTrimmed, "=") == "" {
						setextKind = model.BlockHeading1
					} else if strings.Trim(nextTrimmed, "-") == "" {
						setextKind = model.BlockHeading2
					}
					if setextKind != 0 {
						if !isUnusedRefDef {
							orig := lines[i] + lines[i+1]
							appendBlock(model.Block{
								ID:          model.NewBlockID(),
								Kind:        setextKind,
								Spans:       parseInlineWithDefsAndUsed(bodyTrimmed, defs, usedOut),
								CleanSource: orig,
							}, 2)
							i += 2
							continue
						}
					}
				}
			}
		}
		if indentCols > 12 {
			indentCols = 12
		}
		var paraSpans []model.Span
		if isUnusedRefDef {
			// Unused link reference definitions remain literal paragraphs and must not resolve
			// other reference definitions inside their URL or title.
			paraSpans = parseInlineWithDefs(bodyTrimmed, nil)
		} else {
			paraSpans = parseInlineWithDefsAndUsed(bodyTrimmed, defs, usedOut)
		}
		appendBlock(model.Block{
			ID:          model.NewBlockID(),
			Kind:        model.BlockParagraph,
			Indent:      indentCols,
			Spans:       paraSpans,
			CleanSource: rawLine,
		}, 1)
		i++
	}
	return result
}

func htmlListLineCount(lines []string) int {
	depth := 0
	for idx, l := range lines {
		pos := 0
		for pos < len(l) {
			lt := strings.IndexByte(l[pos:], '<')
			if lt < 0 {
				break
			}
			pos += lt
			name, _, closed, empty, consumed, ok := parseTagRaw(l[pos:])
			if !ok {
				pos++
				continue
			}
			if (name == "ul" || name == "ol") && !empty {
				if closed {
					depth--
					if depth == 0 {
						return idx + 1
					}
				} else {
					depth++
				}
			}
			pos += consumed
		}
	}
	return 0
}

func parseHTMLListLines(lines []string, defs map[string]refDef, usedOut map[string]bool, startLine int) ([]model.Block, int, bool) {
	if len(lines) == 0 {
		return nil, 0, false
	}
	depth := 0
	lineCount := 0
	for idx, l := range lines {
		pos := 0
		for pos < len(l) {
			lt := strings.IndexByte(l[pos:], '<')
			if lt < 0 {
				break
			}
			pos += lt
			name, _, closed, empty, consumed, ok := parseTagRaw(l[pos:])
			if !ok {
				pos++
				continue
			}
			if (name == "ul" || name == "ol") && !empty {
				if closed {
					depth--
					if depth == 0 {
						lineCount = idx + 1
						break
					}
				} else {
					depth++
				}
			}
			pos += consumed
		}
		if lineCount > 0 {
			break
		}
	}
	if lineCount == 0 {
		return nil, 0, false
	}
	src := strings.Join(lines[:lineCount], "")
	type listFrame struct {
		ordered bool
		nextNum int
	}
	var stack []listFrame
	var out []model.Block
	var curBuf strings.Builder
	inLI := false
	liTodo := false
	liChecked := false
	line, liLine, lineCursor := startLine, startLine, 0
	pos := 0

	flushLI := func() {
		if !inLI || len(stack) == 0 {
			curBuf.Reset()
			return
		}
		raw := strings.TrimSpace(curBuf.String())
		curBuf.Reset()
		if raw == "" && !liTodo {
			return
		}
		top := &stack[len(stack)-1]
		indent := (len(stack) - 1) * 2
		kind := model.BlockBullet
		listIdx := 0
		if liTodo {
			kind = model.BlockTodo
		} else if top.ordered {
			kind = model.BlockNumbered
			listIdx = top.nextNum
			top.nextNum++
		}
		out = append(out, model.Block{
			ID:            model.NewBlockID(),
			Kind:          kind,
			Indent:        indent,
			ListIndex:     listIdx,
			SourceLine:    liLine,
			SourceEndLine: line,
			Checked:       liChecked,
			Spans:         parseInlineWithDefsAndUsed(raw, defs, usedOut),
		})
		liTodo = false
		liChecked = false
	}

	for pos < len(src) {
		lt := strings.IndexByte(src[pos:], '<')
		if lt < 0 {
			if inLI {
				curBuf.WriteString(src[pos:])
			}
			break
		}
		if inLI && lt > 0 {
			curBuf.WriteString(src[pos : pos+lt])
		}
		pos += lt
		line += strings.Count(src[lineCursor:pos], "\n")
		lineCursor = pos
		name, attrs, closed, empty, consumed, ok := parseTagRaw(src[pos:])
		if !ok {
			if inLI {
				curBuf.WriteByte(src[pos])
			}
			pos++
			continue
		}
		switch name {
		case "ul", "ol":
			if !closed && !empty {
				flushLI()
				inLI = false
				startNum := 1
				if name == "ol" {
					if sv := strings.TrimSpace(attrVal(attrs, "start")); sv != "" {
						if n, err := strconv.Atoi(sv); err == nil && n > 0 {
							startNum = n
						}
					}
				}
				stack = append(stack, listFrame{ordered: name == "ol", nextNum: startNum})
			} else if closed && len(stack) > 0 {
				flushLI()
				inLI = false
				stack = stack[:len(stack)-1]
			}
		case "li":
			if !closed && !empty {
				flushLI()
				inLI = true
				liLine = line
				liTodo = false
				liChecked = false
			} else if closed {
				flushLI()
				inLI = false
			}
		case "input":
			if inLI && !closed && strings.EqualFold(strings.TrimSpace(attrVal(attrs, "type")), "checkbox") {
				liTodo = true
				liChecked = false
				for _, kv := range attrs {
					if kv[0] == "checked" {
						liChecked = true
						break
					}
				}
			}
		case "p", "div":
			if inLI && closed && curBuf.Len() > 0 {
				curBuf.WriteByte('\n')
			}
		default:
			if inLI {
				curBuf.WriteString(src[pos : pos+consumed])
			}
		}
		pos += consumed
	}
	line = startLine + lineCount - 1
	flushLI()
	if len(out) == 0 {
		return nil, 0, false
	}
	if len(out) == 1 {
		out[0].CleanSource = src
	}
	return out, lineCount, true
}

func stripPreTags(s string) string {
	_, _, _, _, consumed, ok := parseTagRaw(s)
	if ok {
		s = s[consumed:]
	}
	lower := strings.ToLower(s)
	if idx := strings.LastIndex(lower, "</pre>"); idx >= 0 {
		s = s[:idx]
	}
	// Strip inner <code>...</code> wrapper if present.
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(trimmed), "<code") {
		if _, _, _, _, c2, ok2 := parseTagRaw(trimmed); ok2 {
			inner := trimmed[c2:]
			if idx := strings.LastIndex(strings.ToLower(inner), "</code>"); idx >= 0 {
				s = inner[:idx]
			}
		}
	}
	return html.UnescapeString(trimLineEnding(s))
}

func isBlankBlock(b *model.Block) bool {
	return b.Kind == model.BlockParagraph && b.Indent == 0 && b.QuoteDepth == 0 && len(b.Spans) == 0 &&
		!isEmptyBlockComment(b.CleanSource)
}

func stripLegacyEmptyBlockComments(blocks []model.Block) bool {
	stripped := false
	for i := range blocks {
		if isEmptyBlockComment(blocks[i].CleanSource) || strings.Contains(blocks[i].CleanSource, "empty-block=paragraph") {
			blocks[i].CleanSource = ""
			stripped = true
		}
	}
	return stripped
}

func isPlainTextBlock(b *model.Block) bool {
	if b.Kind != model.BlockParagraph || b.Indent != 0 {
		return false
	}
	for _, span := range b.Spans {
		if span.Text != "" {
			return true
		}
	}
	return false
}

func nextNonBlankBlocks(blocks []model.Block) []*model.Block {
	out := make([]*model.Block, len(blocks))
	var seen *model.Block
	for i := len(blocks) - 1; i >= 0; i-- {
		out[i] = seen
		if !isBlankBlock(&blocks[i]) {
			seen = &blocks[i]
		}
	}
	return out
}

func requiredSeparator(this *model.Block, next *model.Block) bool {
	if next == nil {
		return false
	}
	if this.Kind == model.BlockCallout && (next.Kind == model.BlockQuote || isPlainTextBlock(next)) {
		return true
	}
	isPipe := this.Kind == model.BlockTable && this.Table != nil && !this.Table.WasHTML
	nextPipe := next.Kind == model.BlockTable && next.Table != nil && !next.Table.WasHTML
	isImg := this.Kind == model.BlockImage
	nextImg := next.Kind == model.BlockImage
	if !isPipe && !isImg && !isPlainTextBlock(this) {
		return false
	}
	return isPipe || isPlainTextBlock(next) || nextPipe || (isImg && !nextImg)
}

func optionalSeparator(this *model.Block, next *model.Block) bool {
	if isBlankBlock(this) || next == nil {
		return false
	}
	if this.Kind == model.BlockTable {
		return false
	}
	if this.Kind.IsList() && next.Kind.IsList() {
		return false
	}
	return true
}

func needsSeparator(this *model.Block, next *model.Block) bool {
	return requiredSeparator(this, next) || optionalSeparator(this, next)
}

func swallowableSeparator(this *model.Block, next *model.Block) bool {
	if isBlankBlock(this) || next == nil {
		return false
	}
	if needsSeparator(this, next) {
		return true
	}
	// Loose Markdown lists (`- a\n\n- b`, including nested child lists) and HTML tables
	// followed by a blank line should not synthesize stray empty paragraph blocks.
	if this.Kind.IsList() && next.Kind.IsList() {
		return true
	}
	if this.Kind == model.BlockTable {
		return true
	}
	return false
}

func swallowSeparators(blocks []model.Block) []model.Block {
	kept := blocks[:0]
	start := 0
	for i := 1; i+1 < len(blocks); i++ {
		if !isBlankBlock(&blocks[i]) || isBlankBlock(&blocks[i-1]) {
			continue
		}
		// Only the first blank in a run can be a separator.
		next := i + 1
		for next < len(blocks) && isBlankBlock(&blocks[next]) {
			next++
		}
		if next < len(blocks) && swallowableSeparator(&blocks[i-1], &blocks[next]) {
			kept = append(kept, blocks[start:i]...)
			start = i + 1
		}
		i = next - 1
	}
	kept = append(kept, blocks[start:]...)
	clear(blocks[len(kept):])
	return kept
}

type listNumbering struct {
	levels []struct {
		indent int
		num    int
	}
}

func (n *listNumbering) advance(b *model.Block) (num int, continued bool, isNumbered bool) {
	if !b.Kind.IsList() {
		for len(n.levels) > 0 && n.levels[len(n.levels)-1].indent >= b.Indent {
			n.levels = n.levels[:len(n.levels)-1]
		}
		return 0, false, false
	}
	for len(n.levels) > 0 && n.levels[len(n.levels)-1].indent > b.Indent {
		n.levels = n.levels[:len(n.levels)-1]
	}
	if b.Kind != model.BlockNumbered {
		if len(n.levels) > 0 && n.levels[len(n.levels)-1].indent == b.Indent {
			n.levels = n.levels[:len(n.levels)-1]
		}
		return 0, false, false
	}
	if len(n.levels) > 0 && n.levels[len(n.levels)-1].indent == b.Indent {
		n.levels[len(n.levels)-1].num++
		return n.levels[len(n.levels)-1].num, true, true
	}
	start := b.ListIndex
	if start <= 0 {
		start = 1
	}
	n.levels = append(n.levels, struct {
		indent int
		num    int
	}{indent: b.Indent, num: start})
	return start, false, true
}

func (n *listNumbering) next(b *model.Block) int {
	num, _, ok := n.advance(b)
	if !ok {
		return 0
	}
	return num
}

func normalizeImportedStarts(blocks []model.Block) {
	var num listNumbering
	afterBlank := false
	for i := range blocks {
		b := &blocks[i]
		if isBlankBlock(b) {
			afterBlank = true
			continue
		}
		val, continued, isOrd := num.advance(b)
		if isOrd {
			if afterBlank {
				if b.ListIndex == 1 && !continued {
					b.ListIndex = 0
				} else if continued && b.ListIndex > 0 && b.ListIndex != val {
					num.levels[len(num.levels)-1].num = b.ListIndex
				}
			} else if continued || b.ListIndex == 1 {
				b.ListIndex = 0
			}
		}
		afterBlank = false
	}
}
