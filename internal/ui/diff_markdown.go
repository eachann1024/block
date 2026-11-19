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
		fence, nested := "", false
		// Indented code: four columns in after a blank line (or the start), unless the
		// text above is a list, whose nested content is indented the same way.
		inIndented, prevBlank, inList := false, true, false
		for i, l := range lines {
			if change[i] == -side {
				continue
			}
			t := strings.TrimSpace(l)
			if fence == "" {
				if t == "" {
					prevBlank = true
					continue
				}
				ind := len(strings.ReplaceAll(l[:len(l)-len(strings.TrimLeft(l, " \t"))], "\t", "    "))
				wasBlank := prevBlank
				prevBlank = false
				if ind >= 4 && (inIndented || (wasBlank && !inList)) {
					mask[i], inIndented = true, true
					continue
				}
				inIndented = false
				inList = diffListItemRE.MatchString(t) || (inList && ind > 0)
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
					fence = ""
				}
				continue
			}
			if fence = diffFenceOpen(t); fence != "" {
				mask[i] = true
				nested = diffContainerRE.MatchString(t)
			}
		}
		masks[si] = mask
	}
	return masks[0], masks[1]
}

// SyncDiffTableSeps decides each changed table separator row, which the review views
// hide, from the rows the user can see, so a partial accept never leaves an orphan
// "| --- |" nor writes a change nobody saw. applied[i] says whether changed line i is
// taken; unchanged lines are ignored. Each side (old text, new text) is read with its
// own fences. A separator under a changed header follows that header. Under an
// unchanged header it follows the separator it replaces when that one is visible
// (it was code); otherwise the other changed rows of its table, and stays as it was
// when the table has none (an alignment-only change is invisible, so a partial
// accept keeps the old one).
func SyncDiffTableSeps(lines []string, change []int, applied []bool) {
	shown := DiffFenceMask(lines, change)
	hidden := func(k int) bool { return !shown[k] && IsDiffTableSep(strings.TrimSpace(lines[k])) }
	oldM, newM := diffFenceSides(lines, change)
	for si, side := range [2]int{-1, 1} {
		mask := [2][]bool{oldM, newM}[si]
		on := func(i int) bool { return change[i] != -side }
		isRow := func(i int) bool { return !mask[i] && IsDiffTableRow(strings.TrimSpace(lines[i])) }
		for i := 0; i < len(lines); {
			if !on(i) || !isRow(i) {
				i++
				continue
			}
			start, end := i, i
			for ; i < len(lines) && (!on(i) || isRow(i)); i++ {
				if on(i) {
					end = i + 1
				}
			}
			i = end
			any := false
			for k := start; k < end; k++ {
				if change[k] != 0 && !hidden(k) && IsDiffTableRow(strings.TrimSpace(lines[k])) && applied[k] {
					any = true
				}
			}
			for j := start; j < end; j++ {
				if change[j] != side || !IsDiffTableSep(strings.TrimSpace(lines[j])) {
					continue
				}
				h := j - 1
				for h >= start && (!on(h) || IsDiffTableSep(strings.TrimSpace(lines[h]))) {
					h--
				}
				if h >= start && change[h] == side {
					applied[j] = applied[h]
					continue
				}
				applied[j] = any
				// The separator it replaces sits in the same run of changes. Two hidden
				// separators decide together (above); a visible one (the old line was code)
				// is the user's choice to follow.
				lo, hi := j, j
				for lo > 0 && change[lo-1] != 0 {
					lo--
				}
				for hi+1 < len(lines) && change[hi+1] != 0 {
					hi++
				}
				for k := lo; k <= hi; k++ {
					if change[k] == -side && IsDiffTableSep(strings.TrimSpace(lines[k])) {
						if !hidden(k) {
							applied[j] = applied[k]
						} else if v, ok := diffSepPairForced(change, applied, j, k); ok {
							applied[j] = v
						}
						break
					}
				}
			}
		}
	}
}

// diffLinkTitle matches an optional link title: "t", 't' or (t).
const diffLinkTitle = `(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?`

var (
	diffImageRE    = regexp.MustCompile(`!\[([^\]]*)\]\(((?:[^()\s]|\([^()\s]*\))*)` + diffLinkTitle + `\)`)
	diffLinkRE     = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\[\]]*\])+)\]\((?:[^()\s]|\([^()\s]*\))*` + diffLinkTitle + `\)`)
	diffAutolinkRE = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\s]*|[^<>\s@]+@[^<>\s@]+)>`)
)

// diffSepPairForced handles two hidden separators j and k that replace each other.
// When a line between them stays in the result, only the earlier one can be the
// table's separator (the later would sit under that line, not under the header), so
// the pair is forced: keep the earlier, drop the later. ok is false when nothing in
// between stays, leaving the choice to the table's rows.
func diffSepPairForced(change []int, applied []bool, j, k int) (v, ok bool) {
	lo, hi := min(j, k), max(j, k)
	for m := lo + 1; m < hi; m++ {
		if (change[m] < 0 && !applied[m]) || (change[m] > 0 && applied[m]) {
			// Keeping the earlier one means: an inserted earlier separator is applied,
			// a deleted earlier separator is not (it stays), and the later follows suit.
			return change[lo] > 0, true
		}
	}
	return false, false
}

// diffCodeSpans returns the [start, end) byte ranges of the code spans in s, each
// delimited by equal backtick runs.
func diffCodeSpans(s string) [][2]int {
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

// diffPlainLinks turns links into their text: a link inside a diff row would take the
// click (opening a browser) instead of the row's accept toggle. Links are found with
// code spans blanked out, so code is never rewritten and brackets inside it don't
// hide a link.
func diffPlainLinks(s string) string {
	for _, re := range []*regexp.Regexp{diffImageRE, diffLinkRE, diffAutolinkRE} {
		masked := []byte(s)
		for _, sp := range diffCodeSpans(s) {
			for k := sp[0]; k < sp[1]; k++ {
				masked[k] = 'x'
			}
		}
		var b strings.Builder
		last := 0
		for _, m := range re.FindAllSubmatchIndex(masked, -1) {
			b.WriteString(s[last:m[0]])
			text := s[m[2]:m[3]]
			if text == "" && len(m) > 4 && m[4] >= 0 {
				text = s[m[4]:m[5]] // an image without alt text shows its path
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
