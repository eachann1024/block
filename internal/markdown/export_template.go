package markdown

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
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
	case NumberRoman:
		return romanOrdinal(ord) + "."
	case NumberBare:
		return fmt.Sprintf("%d", ord)
	default:
		return fmt.Sprintf("%d.", ord)
	}
}

func romanOrdinal(n int) string {
	if n <= 0 || n >= 4000 {
		return fmt.Sprintf("%d", n)
	}
	vals := [...]int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := [...]string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// calloutIndex maps a callout kind to its slot in CalloutStyle.Labels / Tones.
func calloutIndex(kind model.CalloutKind) int {
	switch kind {
	case model.CalloutTip:
		return 1
	case model.CalloutImportant:
		return 2
	case model.CalloutWarning:
		return 3
	case model.CalloutCaution:
		return 4
	default:
		return 0
	}
}

// resolveCallout returns the colours and label for a callout under the theme's CalloutStyle.
func (s PaperThemeSpec) resolveCallout(kind model.CalloutKind, legacy bool) (tone CalloutTone, label string) {
	bg, border, accent, title := s.CalloutColors(kind)
	tone = CalloutTone{BG: bg, Border: border, Label: accent, FG: s.Text}
	label = title
	cs := s.Style.Callout
	if legacy || !cs.Custom {
		return tone, label
	}
	i := calloutIndex(kind)
	t := cs.Tones[i]
	tone = CalloutTone{
		BG:       t.BG,
		Border:   t.Border,
		Label:    orC(t.Label, accent),
		LabelBG:  t.LabelBG,
		FG:       orC(t.FG, s.Text),
		TopRule:  t.TopRule,
		TopRuleW: t.TopRuleW,
		Dashed:   cs.Dashed || t.Dashed,
		Bold:     t.Bold,
	}
	label = orS(cs.Labels[i], title)
	return tone, label
}

func colorLuma(c ui.Color) float32 {
	return (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255.0
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

func isGenericFontKeyword(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sans-serif", "serif", "monospace", "cursive", "fantasy", "system-ui":
		return true
	default:
		return false
	}
}

// splitFontPair splits a composite font stack into its primary Latin family and
// East-Asian fallback family for DOCX <w:rFonts>.
func splitFontPair(fam string) (primary, eastAsia string) {
	switch fam {
	case fontKai:
		return "LXGW WenKai", "LXGW WenKai"
	case fontSerif:
		return "Noto Serif SC", "Noto Serif SC"
	case fontSans, "":
		return "HarmonyOS Sans SC", "HarmonyOS Sans SC"
	case fontMono, fontMonoCJK:
		return "JetBrains Mono", "HarmonyOS Sans SC"
	}
	var clean []string
	for _, part := range strings.Split(fam, ",") {
		p := strings.TrimSpace(strings.Trim(part, `"'`))
		if p == "" || isGenericFontKeyword(p) {
			continue
		}
		clean = append(clean, p)
	}
	if len(clean) == 0 {
		return "HarmonyOS Sans SC", "HarmonyOS Sans SC"
	}
	p0 := clean[0]
	if len(clean) >= 2 && (strings.EqualFold(p0, "JetBrains Mono") || strings.EqualFold(p0, "Menlo") || strings.EqualFold(p0, "Georgia") || strings.EqualFold(p0, "Didot") || strings.EqualFold(p0, "Avenir Next")) {
		return p0, clean[1]
	}
	if strings.EqualFold(p0, "JetBrains Mono") || strings.EqualFold(p0, "Menlo") {
		return p0, "HarmonyOS Sans SC"
	}
	return p0, p0
}

func isDefaultHighlightSpan(sp model.Span) bool {
	return strings.EqualFold(strings.TrimSpace(sp.BGColor), "yellow")
}

// exportQuoteLevels leaves the first level to the paper's quote style.
func exportQuoteLevels(b *model.Block) int {
	depth := b.QuoteDepth
	if b.Kind == model.BlockQuote {
		depth--
	}
	return max(depth, 0)
}

func exportTableTint(t *model.TableData, row, col int, pal PaperThemeSpec) (ui.Color, bool) {
	colors := theme.OceanLight.Table()
	if pal.Dark {
		colors = theme.OceanDark.Table()
	}
	if tint := t.CellTint(row, col); tint != model.TableTintNone {
		return colors.CellTint(string(tint))
	}
	return ui.Color{}, false
}

// imageCaptionSide keeps a narrow figure readable on small paper widths.
func imageCaptionSide(style ImageStyle, alt string, width float32) float32 {
	if !style.Caption || strings.TrimSpace(alt) == "" || width < 260 {
		return 0
	}
	return style.CaptionSide
}

// exportSubHeadingSize continues the scale below the third-level heading.
func exportSubHeadingSize(size float32, level int) float32 {
	return max(size-float32(level-3)*1.4, 11)
}
