package ui

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// BuildHighlightSpans slices text into plain and highlighted spans based on UTF-8 byte ranges.
func BuildHighlightSpans(text string, marks [][2]int, hlBG ui.Color) []ui.Span {
	spans := make([]ui.Span, 0, len(marks)*2+1)
	pos := 0
	for _, r := range marks {
		start, end := r[0], r[1]
		if start < pos {
			start = pos
		}
		if end > len(text) {
			end = len(text)
		}
		if start >= end {
			continue
		}
		if (start > 0 && !utf8.RuneStart(text[start])) || (end < len(text) && !utf8.RuneStart(text[end])) {
			continue
		}
		if start > pos {
			spans = append(spans, ui.Span{Text: text[pos:start]})
		}
		spans = append(spans, ui.Span{Text: text[start:end], Background: hlBG})
		pos = end
	}
	if pos < len(text) {
		spans = append(spans, ui.Span{Text: text[pos:]})
	}
	if len(spans) == 0 {
		return []ui.Span{{Text: text}}
	}
	return spans
}

// HighlightedText renders single-line ellipsized text with search highlight ranges.
func HighlightedText(c *ui.Context, text string, marks [][2]int, size float32, weight int, fg, hlBG ui.Color) ui.Element {
	if len(marks) == 0 {
		el := ui.Text(c, text).FontSize(size).TextColor(fg).SingleLine().Ellipsis("…")
		if weight > 0 {
			el.FontWeight(weight)
		}
		return el
	}
	spans := BuildHighlightSpans(text, marks, hlBG)
	el := ui.RichText(c, spans...).FontSize(size).TextColor(fg).SingleLine().Ellipsis("…")
	if weight > 0 {
		el.FontWeight(weight)
	}
	return el
}
