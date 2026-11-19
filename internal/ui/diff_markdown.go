package ui

import (
	"regexp"
	"strings"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	mdparser "github.com/egoist/mygo/ui/markdown/parser"
)

// DiffLineStyle is the look of one rendered line in an AI diff review.
type DiffLineStyle struct {
	// Text colors the line: the diff add/delete foreground or the muted context color.
	Text ui.Color
	// CodeBG sits behind inline code spans.
	CodeBG ui.Color
	// Code marks a line inside a fenced code block (see DiffFenceMask): shown verbatim
	// in the code font, since alone it would parse as a heading or list.
	Code bool
}

// diffContainerRE matches the blockquote and list markers a fence may sit behind.
var diffContainerRE = regexp.MustCompile(`^(?:>\s*|(?:[-*+]|\d{1,9}[.)])\s+)+`)

// diffListItemRE matches a list item marker at the start of a trimmed line.
var diffListItemRE = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])(?:\s|$)`)

// diffRuleRE matches a thematic break such as "* * *" or "---".
var diffRuleRE = regexp.MustCompile(`^(?:\*[ \t]*){3,}$|^(?:-[ \t]*){3,}$|^(?:_[ \t]*){3,}$`)

// diffHeadingRE matches an ATX heading such as "## Title".
var diffHeadingRE = regexp.MustCompile(`^#{1,6}(?:\s|$)`)

// diffSetextRE matches a setext heading underline ("===" or "--" of any length).
var diffSetextRE = regexp.MustCompile(`^(?:=+|-+)$`)

// diffQuoteRE matches the blockquote markers in front of a fence closer.
var diffQuoteRE = regexp.MustCompile(`^(?:>\s*)+`)

// diffFenceOpen returns the fence run ("```", "~~~~"…) a trimmed line opens, or "".
// A backtick fence's info string may not hold a backtick, so "```x```" is inline code.
func diffFenceOpen(t string) string {
	t = diffContainerRE.ReplaceAllString(t, "")
	for _, f := range [2]string{"```", "~~~"} {
		if strings.HasPrefix(t, f) {
			n := len(t) - len(strings.TrimLeft(t, f[:1]))
			if f[0] == '`' && strings.Contains(t[n:], "`") {
				return ""
			}
			return strings.Repeat(f[:1], n)
		}
	}
	return ""
}

// DiffFenceMask reports, per line, whether it opens, closes or sits inside a fenced
// code block, so diff rows rendered one by one keep the fence's context. A diff mixes
// the old and new text, so each side is scanned on its own: change < 0 lines belong to
// the old text, > 0 to the new one, 0 to both; a line is code if its side says so.
func DiffFenceMask(lines []string, change []int) []bool {
	old, cur := diffFenceSides(lines, change)
	for i := range old {
		old[i] = old[i] || cur[i]
	}
	return old
}

// diffFenceSides is DiffFenceMask for the old and the new text separately; a line
// absent from a side is false there.
func diffFenceSides(lines []string, change []int) (old, cur []bool) {
	var masks [2][]bool
	for si, side := range [2]int{-1, 1} {
		mask := make([]bool, len(lines))
		fence, nested, fenceList := "", false, false
		// Indented code: four columns in after a blank line (or the start), unless the
		// text above is a list, whose nested content is indented the same way.
		// para: the line above is paragraph text, which a "===" underline turns into a heading.
		inIndented, prevBlank, inList, para := false, true, false, false
		for i, l := range lines {
			if change[i] == -side {
				continue
			}
			t := strings.TrimSpace(l)
			if fence == "" {
				if t == "" {
					prevBlank, para = true, false
					continue
				}
				ind := len(strings.ReplaceAll(l[:len(l)-len(strings.TrimLeft(l, " \t"))], "\t", "    "))
				wasBlank := prevBlank
				prevBlank = false
				if ind >= 4 && (inIndented || (wasBlank && !inList)) {
					mask[i], inIndented, para = true, true, false
					continue
				}
				inIndented = false
				inList = (diffListItemRE.MatchString(t) && !diffRuleRE.MatchString(t)) || (inList && ind > 0)
				// A rule, heading or setext underline ends its block on that line, so code
				// may follow at once. Indented four or more, such a line is paragraph text.
				ends := ind < 4 && (diffRuleRE.MatchString(t) || diffHeadingRE.MatchString(t) ||
					(para && diffSetextRE.MatchString(t)))
				prevBlank, para = !inList && ends, !ends
			}
			if fence != "" {
				mask[i] = true
				// Inside a plain fence "> ~~~" is code; only a fence opened behind a list or
				// quote marker may close behind a quote marker. List items close by indent,
				// so "- ~~~" inside a fence never closes it.
				if nested {
					t = diffQuoteRE.ReplaceAllString(t, "")
				}
				if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
					// Code may follow at once, unless the fence sat in a list item, whose
					// content is indented the same way.
					fence, prevBlank, inIndented = "", !fenceList, false
				}
				continue
			}
			if fence = diffFenceOpen(t); fence != "" {
				mask[i], para = true, false
				nested = diffContainerRE.MatchString(t)
				fenceList = inList || diffListItemRE.MatchString(diffQuoteRE.ReplaceAllString(t, ""))
			}
		}
		masks[si] = mask
	}
	return masks[0], masks[1]
}

// SyncDiffTableSeps decides each changed table separator row, which the review views
// hide, by checking the text the selection would write: in every table that results,
// exactly one separator sits right under the header row and none anywhere else, so a
// partial accept never leaves an orphan "| --- |" nor turns a table into a paragraph.
// applied[i] says whether changed line i is taken; unchanged lines are ignored. A
// deleted line whose removal would merge two tables is kept as well. When
// both the old and the new separator fit under the header, the new one is used only
// if a visible changed row of that very table is taken (an alignment-only change is
// invisible, so a partial accept keeps the old alignment).
func SyncDiffTableSeps(lines []string, change []int, applied []bool) {
	shown := DiffFenceMask(lines, change)
	oldM, newM := diffFenceSides(lines, change)
	trim := func(k int) string { return strings.TrimSpace(lines[k]) }
	cand := func(k int) bool { return change[k] != 0 && !shown[k] && IsDiffTableSep(trim(k)) }
	kept := func(k int) bool { return change[k] == 0 || (change[k] < 0) != applied[k] }
	take := func(k int, in bool) { applied[k] = (change[k] > 0) == in }
	// A table nested in a list item is indented; a row at another indent belongs to a
	// different table even with no line between them.
	indent := func(k int) int { return len(lines[k]) - len(strings.TrimLeft(lines[k], " \t")) }

	// tables[0][k], tables[1][k]: which table of the old / new text line k sits in
	// (0: none), to tell whether a deleted row belonged to the table it is judged
	// with, and whether two rows that meet in the result were ever one table.
	var tables [2][]int
	for si, side := range [2]int{-1, 1} {
		mask, ids := [2][]bool{oldM, newM}[si], make([]int, len(lines))
		for k, id, prev := 0, 0, -1; k < len(lines); k++ {
			if change[k] == -side {
				continue
			}
			if !mask[k] && IsDiffTableRow(trim(k)) {
				if prev < 0 || indent(prev) != indent(k) {
					id++
				}
				ids[k], prev = id, k
			} else {
				prev = -1
			}
		}
		tables[si] = ids
	}
	for diffSepPass(lines, change, applied, cand, kept, take, indent, tables) {
	}
}

// diffSepPass is one pass of SyncDiffTableSeps over the written text. It reports
// whether it had to keep a deleted line that would merge two tables (dropping the
// blank between them leaves a second separator as a data row); the next pass then
// sees them apart.
func diffSepPass(lines []string, change []int, applied []bool, cand, kept func(int) bool,
	take func(int, bool), indent func(int) int, tables [2][]int) bool {
	trim := func(k int) string { return strings.TrimSpace(lines[k]) }
	oldTable, newTable := tables[0], tables[1]
	// Rows are read in the written text itself, so code that becomes a table (or a
	// table that becomes code) counts the way the result parses.
	var res []int
	var resText []string
	for k := range lines {
		if kept(k) && !cand(k) {
			res, resText = append(res, k), append(resText, lines[k])
		}
	}
	resCode, _ := diffFenceSides(resText, make([]int, len(resText)))
	row := make([]bool, len(lines))
	for j, k := range res {
		row[k] = !resCode[j] && IsDiffTableRow(trim(k))
	}

	for i := 0; i < len(lines); {
		if !cand(i) && !row[i] && (kept(i) || oldTable[i] == 0) {
			i++
			continue
		}
		// A run is one block of rows in the result: kept rows plus the hidden separators
		// among them (and the deleted rows around them). Dropped lines are see-through;
		// any other kept line ends it.
		var rows, cands, dels, drops []int
	run:
		for ; i < len(lines); i++ {
			switch {
			case cand(i):
				cands = append(cands, i)
			case !kept(i):
				if oldTable[i] > 0 {
					dels = append(dels, i)
				} else if change[i] < 0 {
					drops = append(drops, i)
				}
			case row[i]:
				if len(rows) > 0 && indent(rows[len(rows)-1]) != indent(i) {
					break run
				}
				rows = append(rows, i)
			default:
				break run
			}
		}
		for _, k := range cands {
			take(k, false)
		}
		if len(rows) == 0 {
			continue
		}
		// Two rows that meet here but were one table in neither text were split by a
		// line the selection drops (the blank between two tables): keep it.
		again := false
		for j := 1; j < len(rows); j++ {
			a, b := rows[j-1], rows[j]
			sameOld := change[a] <= 0 && change[b] <= 0 && oldTable[a] != 0 && oldTable[a] == oldTable[b]
			sameNew := change[a] >= 0 && change[b] >= 0 && newTable[a] != 0 && newTable[a] == newTable[b]
			if sameOld || sameNew {
				continue
			}
			for _, d := range drops {
				if a < d && d < b {
					applied[d], again = false, true
				}
			}
		}
		if again {
			return true
		}
		// A block holds one table: the row right above its separator is the header (rows
		// above that are paragraph text) and every row after it is data. A separator
		// already kept fixes the table.
		fixed := false
		for _, r := range rows[1:] {
			fixed = fixed || IsDiffTableSep(trim(r))
		}
		if fixed {
			continue
		}
		taken := false
		tables := map[int]bool{}
		for _, r := range rows {
			taken = taken || change[r] > 0
			if change[r] <= 0 {
				tables[oldTable[r]] = true
			}
		}
		for _, d := range dels {
			taken = taken || tables[oldTable[d]]
		}
		pick := -1
		for _, k := range cands {
			if k < rows[0] {
				continue
			}
			if pick < 0 || ((change[k] > 0) == taken && (change[pick] > 0) != taken) {
				pick = k
			}
		}
		if pick >= 0 {
			take(pick, true)
		}
	}
	return false
}

// diffLinkDest matches a link destination: <with spaces> or bare (one level of parens).
const diffLinkDest = `(?:<[^<>\n]*>|(?:[^()\s]|\([^()\s]*\))*)`

// diffLinkTitle matches an optional link title: "t", 't' or (t).
const diffLinkTitle = `(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?`

var (
	diffImageRE    = regexp.MustCompile(`!\[([^\]]*)\]\((` + diffLinkDest + `)` + diffLinkTitle + `\)`)
	diffLinkRE     = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\[\]]*\])+)\]\(` + diffLinkDest + diffLinkTitle + `\)`)
	diffAutolinkRE = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\s]*|[^<>\s@]+@[^<>\s@]+)>`)
)

// CodeSpans returns the [start, end) byte ranges of the code spans in s, each
// delimited by equal backtick runs.
func CodeSpans(s string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		run := len(s[i:]) - len(strings.TrimLeft(s[i:], "`"))
		end := -1
		for j := i + run; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			r := len(s[j:]) - len(strings.TrimLeft(s[j:], "`"))
			if r == run {
				end = j + r
				break
			}
			j += r
		}
		if end < 0 {
			i += run
			continue
		}
		spans = append(spans, [2]int{i, end})
		i = end
	}
	return spans
}

// diffLiteralEscaper backslash-escapes the inline Markdown markers in plain text.
var diffLiteralEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "~", `\~`, "[", `\[`, "]", `\]`, "$", `\$`, "&", `\&`)

// diffEscaped reports whether the byte at i follows an odd run of backslashes.
func diffEscaped(s string, i int) bool {
	n := 0
	for i-n > 0 && s[i-n-1] == '\\' {
		n++
	}
	return n%2 == 1
}

// diffPlainLinks turns links into their text: a link inside a diff row would take the
// click (opening a browser) instead of the row's accept toggle. Links are found with
// code spans blanked out, so code is never rewritten and brackets inside it don't
// hide a link.
func diffPlainLinks(s string) string {
	for _, re := range []*regexp.Regexp{diffImageRE, diffLinkRE, diffAutolinkRE} {
		masked := []byte(s)
		for _, sp := range CodeSpans(s) {
			for k := sp[0]; k < sp[1]; k++ {
				masked[k] = 'x'
			}
		}
		var b strings.Builder
		last := 0
		for _, m := range re.FindAllSubmatchIndex(masked, -1) {
			if diffEscaped(s, m[0]) {
				// "\<…>" or "\[…](…)" is literal text, not a link.
				continue
			}
			b.WriteString(s[last:m[0]])
			text := s[m[2]:m[3]]
			if text == "" && len(m) > 4 && m[4] >= 0 {
				// An image without alt text shows its path, as code so "_" stays literal.
				if p := strings.Trim(s[m[4]:m[5]], "<>"); p != "" && !strings.Contains(p, "`") {
					text = "`" + p + "`"
				}
			}
			if re == diffAutolinkRE {
				// An autolink shows its address verbatim, so "__tests__" is not bold.
				text = diffLiteralEscaper.Replace(text)
			}
			b.WriteString(text)
			last = m[1]
		}
		b.WriteString(s[last:])
		s = b.String()
	}
	return s
}

type diffMDKey struct{ text, code ui.Color }

var diffMDOpts = map[diffMDKey]*markdown.Options{}

func diffMarkdownOptions(st DiffLineStyle) *markdown.Options {
	k := diffMDKey{st.Text, st.CodeBG}
	if o, ok := diffMDOpts[k]; ok {
		return o
	}
	hs := func(size float32) markdown.HeadingStyle {
		return markdown.HeadingStyle{Size: size, Weight: theme.WeightTitle, Color: st.Text}
	}
	// ShareSelection: no per-line selectable, so clicks still reach the row toggles.
	// No bare autolinks (they would take the click) and no tables (rows are split
	// into cells by DiffTableRow).
	ext := mdparser.ExtTaskLists | mdparser.ExtStrikethrough | mdparser.ExtMath | mdparser.ExtAlerts
	o := &markdown.Options{ShareSelection: true, Extensions: ext, Theme: &markdown.Theme{
		FontSize:       theme.FontBody,
		LineHeight:     1.5,
		Text:           st.Text,
		Muted:          st.Text,
		Link:           st.Text,
		Strong:         st.Text,
		StrongWeight:   theme.WeightTitle,
		Code:           st.Text,
		CodeBackground: st.CodeBG,
		BlockSpacing:   0.01,
		ItemSpacing:    0.01,
		Headings:       [6]markdown.HeadingStyle{hs(1.3), hs(1.18), hs(1.08), hs(1), hs(1), hs(1)},
		QuoteText:      st.Text,
	}}
	diffMDOpts[k] = o
	return o
}

// DiffMarkdownLine renders one Markdown source line of a diff the way the note shows
// it (headings, emphasis, inline code, list markers) instead of as raw source.
// Blank lines collapse to a thin spacer so paragraphs keep their rhythm.
func DiffMarkdownLine(c *ui.Context, line string, st DiffLineStyle) ui.Element {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return ui.Box(c).FillWidth().Height(theme.Space4)
	}
	if st.Code {
		return ui.Text(c, strings.ReplaceAll(line, "\t", "    ")).Font("monospace").FontSize(theme.FontBody).
			TextColor(st.Text).Grow(1).MinWidth(0)
	}
	// Leading spaces become padding: four of them alone would parse as a code block,
	// and a nested list item keeps its depth.
	body := strings.TrimLeft(line, " \t")
	indent := len(strings.ReplaceAll(line[:len(line)-len(body)], "\t", "    "))
	return ui.Box(c).Grow(1).MinWidth(0).Padding(0, 0, 0, float32(indent/2)*theme.Space8).Children(func() {
		// A link reference definition renders as nothing; escape its colon to show it
		// (a leading "\\[" would open display math).
		if diffLinkDefRE.MatchString(body) {
			body = strings.Replace(body, "]:", "]\\:", 1)
		}
		// An escaped "\[" opening the line would start display math; the note shows a
		// bracket, so write it as an entity.
		if strings.HasPrefix(body, `\[`) && !strings.Contains(body, `\]`) {
			body = "&#91;" + body[2:]
		}
		markdown.View(c, diffPlainLinks(body), diffMarkdownOptions(st))
	})
}

// diffLinkDefRE matches a link reference definition such as "[api]: https://…".
var diffLinkDefRE = regexp.MustCompile(`^\[[^\]]+\]:`)

// diffCellBlockRE matches a leading marker that would make a cell parse as a block
// (heading, quote, fence, bullet, thematic break); a backslash in front keeps it literal.
var diffCellBlockRE = regexp.MustCompile("^(?:#{1,6}(?:\\s|$)|>|~{3,}|`{3,}|[-+*](?:\\s|$)|[-*_](?:\\s*[-*_]){2,}\\s*$)")

// diffCellListRE matches an ordered-list marker; its "." or ")" gets the backslash.
var diffCellListRE = regexp.MustCompile(`^(\d{1,9})([.)])(\s|$)`)

// DiffMarkdownCell renders one table cell's inline Markdown (code spans, emphasis).
func DiffMarkdownCell(c *ui.Context, cell string, st DiffLineStyle, header bool) ui.Element {
	// A cell holds inline content only: escape a leading heading, quote or list marker.
	if diffCellListRE.MatchString(cell) {
		cell = diffCellListRE.ReplaceAllString(cell, "$1\\$2$3")
	} else if diffCellBlockRE.MatchString(cell) {
		cell = "\\" + cell
	}
	if header {
		cell = "**" + strings.TrimSpace(cell) + "**"
	}
	if strings.TrimSpace(strings.Trim(cell, "*")) == "" {
		return ui.Box(c)
	}
	return markdown.View(c, diffPlainLinks(cell), diffMarkdownOptions(st))
}

// IsDiffTableRow reports whether a trimmed Markdown line is a table row.
func IsDiffTableRow(trimmed string) bool {
	return strings.HasPrefix(trimmed, "|") && strings.Count(trimmed, "|") >= 2
}

// IsDiffTableSep reports whether a trimmed line is a table separator such as "| --- | :-: |".
func IsDiffTableSep(trimmed string) bool {
	if !IsDiffTableRow(trimmed) {
		return false
	}
	inner := strings.Trim(trimmed, "|")
	if strings.TrimSpace(inner) == "" {
		return false
	}
	for _, cell := range strings.Split(inner, "|") {
		t := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(cell), ":"), ":")
		if t == "" || strings.Trim(t, "-") != "" {
			return false
		}
	}
	return true
}

// ParseDiffTableCells splits a trimmed table row into its trimmed cells. A backslash
// escapes the next character, so "\\|" is a literal bar inside its cell while "\\\\|"
// is a backslash followed by a cell boundary.
func ParseDiffTableCells(trimmed string) []string {
	s := strings.TrimSpace(trimmed)
	if !strings.HasPrefix(s, "|") {
		return nil
	}
	s = s[1:]
	var cells []string
	var cur strings.Builder
	endedOnBar := false
	for i := 0; i < len(s); i++ {
		endedOnBar = false
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '\\' && i+1 < len(s):
			cur.WriteString(s[i : i+2])
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			endedOnBar = true
		default:
			cur.WriteByte(s[i])
		}
	}
	if !endedOnBar || strings.TrimSpace(cur.String()) != "" {
		cells = append(cells, strings.TrimSpace(cur.String()))
	}
	return cells
}

// DiffTableCols gives each table row the column count of its whole table (the widest
// row of the contiguous run of table lines outside code), so a short row still lines
// up with its header. Non-table lines get 0.
func DiffTableCols(lines []string, code []bool) []int {
	cols := make([]int, len(lines))
	for i := 0; i < len(lines); {
		if code[i] || !IsDiffTableRow(strings.TrimSpace(lines[i])) {
			i++
			continue
		}
		start, widest := i, 0
		for ; i < len(lines) && !code[i] && IsDiffTableRow(strings.TrimSpace(lines[i])); i++ {
			widest = max(widest, len(ParseDiffTableCells(strings.TrimSpace(lines[i]))))
		}
		for j := start; j < i; j++ {
			cols[j] = widest
		}
	}
	return cols
}

// DiffTableRow draws one table row as cells split by hairlines, padded to cols cells.
func DiffTableRow(c *ui.Context, trimmed string, cols int, st DiffLineStyle, header bool, divider ui.Color) ui.Element {
	cells := ParseDiffTableCells(trimmed)
	for len(cells) < cols {
		cells = append(cells, "")
	}
	return ui.Row(c).Grow(1).MinWidth(0).Children(func() {
		for ci, cell := range cells {
			ui.Box(c).Grow(1).Shrink(1).Basis(0).MinWidth(0).Padding(theme.Space4, theme.Space8).
				BorderWidth(0, boolBorder(ci+1 < len(cells)), 0, 0).BorderColor(divider).
				Children(func() {
					DiffMarkdownCell(c, cell, st, header)
				})
		}
	})
}

func boolBorder(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
