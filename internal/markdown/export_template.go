package markdown

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"block/internal/model"
)

// templateVars fills the placeholders a theme's furniture text may use:
//
//	{date} 2026.11.18  {dateCN} 2026年11月18日  {dateEN} NOV 18, 2026
//	{file} note.md     {FILE} NOTE              {title} the document title
type templateVars struct {
	r *strings.Replacer
}

func newTemplateVars(notePath, title string, now time.Time) templateVars {
	if now.IsZero() {
		now = time.Now()
	}
	file := filepath.Base(notePath)
	if notePath == "" || file == "." || file == string(filepath.Separator) {
		file = "untitled.md"
	}
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	if stem == "" {
		stem = "untitled"
	}
	return templateVars{r: strings.NewReplacer(
		"{date}", now.Format("2006.01.02"),
		"{dateCN}", now.Format("2006年1月2日"),
		"{dateEN}", strings.ToUpper(now.Format("Jan 2, 2006")),
		"{file}", file,
		"{FILE}", strings.ToUpper(stem),
		"{title}", title,
	)}
}

func (v templateVars) expand(s string) string {
	if s == "" || v.r == nil {
		return s
	}
	return v.r.Replace(s)
}

// splitTitleAccent splits title into (head, accentedTail) for themes whose
// TitleStyle.Accent highlights the final word or trailing CJK phrase.
func splitTitleAccent(title string) (string, string) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return title, ""
	}
	runes := []rune(trimmed)
	n := len(runes)
	if n <= 1 {
		return trimmed, ""
	}
	last := runes[n-1]
	if last < 0x80 && (unicode.IsLetter(last) || unicode.IsDigit(last)) {
		i := n - 1
		for i > 0 && runes[i-1] < 0x80 && (unicode.IsLetter(runes[i-1]) || unicode.IsDigit(runes[i-1]) || runes[i-1] == '-') {
			i--
		}
		if i == 0 {
			return trimmed, ""
		}
		return string(runes[:i]), string(runes[i:])
	}
	end := n
	for end > 0 && unicode.IsPunct(runes[end-1]) {
		end--
	}
	if end <= 1 {
		return trimmed, ""
	}
	take := 2
	if end <= 3 {
		take = 1
	}
	split := end - take
	// Do not split across a space or middle dot inside the accented slice.
	for j := split; j < end; j++ {
		if unicode.IsSpace(runes[j]) || unicode.IsPunct(runes[j]) {
			split = j + 1
		}
	}
	if split <= 0 || split >= end {
		return trimmed, ""
	}
	return string(runes[:split]), string(runes[split:])
}

// formatListOrdinal formats a 1-based ordered-list index according to NumberKind.
func formatListOrdinal(ord int, kind NumberKind) string {
	if ord <= 0 {
		ord = 1
	}
	switch kind {
	case NumberPadded:
		return fmt.Sprintf("%02d", ord)
	case NumberCJK:
		return chineseOrdinal(ord) + "、"
	default:
		return fmt.Sprintf("%d.", ord)
	}
}

func chineseOrdinal(n int) string {
	digits := [...]string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	switch {
	case n <= 0 || n >= 100:
		return fmt.Sprintf("%d", n)
	case n < 10:
		return digits[n]
	case n == 10:
		return "十"
	case n < 20:
		return "十" + digits[n%10]
	default:
		tens := digits[n/10] + "十"
		if n%10 == 0 {
			return tens
		}
		return tens + digits[n%10]
	}
}

// isNumericCellText reports whether s looks like a numeric/metric table value
// (e.g. "176", "99%", "176p", "232 页") suitable for tabular numeric styling.
func isNumericCellText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	runes := []rune(s)
	first := runes[0]
	if !(first >= '0' && first <= '9') && first != '+' && first != '-' && first != '$' && first != '¥' && first != '€' {
		return false
	}
	hasDigit := false
	for _, r := range runes {
		if r >= '0' && r <= '9' {
			hasDigit = true
			continue
		}
		switch r {
		case '.', ',', '%', '+', '-', '/', ':', ' ', 'p', 'P', 'x', '×', '$', '¥', '€', '页', '本', '期', '年', '月', '日':
		default:
			return false
		}
	}
	return hasDigit
}

// splitFontPair splits a composite font stack like "Georgia, Songti SC" into
// its primary Latin family and East-Asian fallback family.
func splitFontPair(fam string) (primary, eastAsia string) {
	parts := strings.Split(fam, ",")
	if len(parts) == 0 {
		return fontPing, fontPing
	}
	p0 := strings.TrimSpace(parts[0])
	if p0 == "" {
		p0 = fontPing
	}
	if len(parts) >= 2 {
		p1 := strings.TrimSpace(parts[1])
		if p1 != "" && !strings.EqualFold(p1, "monospace") && !strings.EqualFold(p1, "serif") && !strings.EqualFold(p1, "sans-serif") {
			return p0, p1
		}
	}
	if strings.EqualFold(p0, "Menlo") || strings.EqualFold(p0, "monospace") {
		return "Menlo", fontPing
	}
	return p0, p0
}

func isDefaultHighlightSpan(sp model.Span) bool {
	return strings.EqualFold(strings.TrimSpace(sp.BGColor), "yellow")
}
