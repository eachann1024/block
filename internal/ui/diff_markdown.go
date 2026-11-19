package ui

import (
	"regexp"
	"strings"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
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
// code block, so diff rows rendered one by one keep the fence's context.
func DiffFenceMask(lines []string) []bool {
	mask := make([]bool, len(lines))
	fence := ""
	for i, l := range lines {
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
	return mask
}

var (
	diffImageRE = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	diffLinkRE  = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
)

// diffPlainLinks turns links into their text: a link inside a diff row would take the
// click (opening a browser) instead of the row's accept toggle.
func diffPlainLinks(s string) string {
	s = diffImageRE.ReplaceAllString(s, "$1")
	return diffLinkRE.ReplaceAllString(s, "$1")
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
	o := &markdown.Options{ShareSelection: true, Theme: &markdown.Theme{
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

// ParseDiffTableCells splits a trimmed table row into its trimmed cells; an escaped
// "\\|" stays inside its cell as a literal bar.
func ParseDiffTableCells(trimmed string) []string {
	s := strings.TrimSpace(trimmed)
	if !strings.HasPrefix(s, "|") {
		return nil
	}
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, "\\|") {
		s = strings.TrimSuffix(s, "|")
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}
