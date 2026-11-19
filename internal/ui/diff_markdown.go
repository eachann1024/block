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

// DiffFenceMask reports, per line, whether it opens, closes or sits inside a fenced
// code block, so diff rows rendered one by one keep the fence's context. A diff mixes
// the old and new text, so each side is scanned on its own: change < 0 lines belong to
// the old text, > 0 to the new one, 0 to both; a line is code if its side says so.
func DiffFenceMask(lines []string, change []int) []bool {
	mask := make([]bool, len(lines))
	for _, side := range [2]int{-1, 1} {
		fence := ""
		for i, l := range lines {
			if change[i] == -side {
				continue
			}
			t := strings.TrimSpace(l)
			if fence != "" {
				mask[i] = true
				if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
					fence = ""
				}
				continue
			}
			for _, f := range [2]string{"```", "~~~"} {
				if strings.HasPrefix(t, f) {
					n := len(t) - len(strings.TrimLeft(t, f[:1]))
					fence = strings.Repeat(f[:1], n)
					mask[i] = true
					break
				}
			}
		}
	}
	return mask
}

var (
	diffImageRE    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	diffLinkRE     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	diffAutolinkRE = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\s]*|[^<>\s@]+@[^<>\s@]+)>`)
)

// diffPlainLinks turns links into their text: a link inside a diff row would take the
// click (opening a browser) instead of the row's accept toggle. Code spans are left
// as written.
func diffPlainLinks(s string) string {
	parts := strings.Split(s, "`")
	for i := 0; i < len(parts); i += 2 {
		p := diffImageRE.ReplaceAllString(parts[i], "$1")
		p = diffLinkRE.ReplaceAllString(p, "$1")
		parts[i] = diffAutolinkRE.ReplaceAllString(p, "$1")
	}
	return strings.Join(parts, "`")
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
		markdown.View(c, diffPlainLinks(body), diffMarkdownOptions(st))
	})
}

// DiffMarkdownCell renders one table cell's inline Markdown (code spans, emphasis).
func DiffMarkdownCell(c *ui.Context, cell string, st DiffLineStyle, header bool) ui.Element {
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
