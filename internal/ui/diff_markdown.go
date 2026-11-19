package ui

import (
	"regexp"
	"slices"
	"strconv"
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
	// Setext is DiffSetextLevels for the line: 1 or 2 shows paragraph text as that
	// heading level, -1 hides the "===" or "---" underline below it.
	Setext int
	// ListNum is DiffListNumbers for the line: the number an ordered item shows
	// ("1." "1." "1." count 1, 2, 3 as in the note); 0 keeps its own.
	ListNum int
	// Refs is DiffRefLabels of the diff: "[部署手册][deploy]" shows its text when
	// "deploy" is defined there.
	Refs map[string]bool
	// Aligns is DiffTableAligns for a table row: each column's alignment from its
	// table's separator ("---:" is right-aligned).
	Aligns []ui.Align
	// Math marks a line of a multi-line display math block (see DiffMathMask): its
	// text shows as math, a delimiter alone as a spacer.
	Math bool
	// RefDef marks a link reference definition's line (see DiffRefDefs), which the
	// note does not show.
	RefDef bool
	// Alert is DiffAlertTitles for the line: the title ("Note") its "> [!NOTE]"
	// marker shows as.
	Alert string
	// Fence marks a code fence's opening or closing line (see DiffFenceLines), which
	// the note shows as the block's edge: an opener shows only its language.
	Fence bool
	// Quote is the quote depth of a code line's block (see DiffFenceLines): in
	// "> ```sh" the code shows without its ">" markers.
	Quote int
	// Break is DiffHardBreaks for the line: its trailing "\" breaks the line.
	Break bool
}

// DiffListNumbers reports, per line, the number an ordered list item shows in its
// own text (0: not one), since each line alone would show its written number; -1
// marks an item marker that is paragraph text instead ("2." may not interrupt a
// paragraph). A changed line takes its own side, an unchanged one the new text.
func DiffListNumbers(lines []string, change []int) []int {
	masks, _ := diffScan(lines, change)
	var nums [2][]int
	for si, side := range [2]int{-1, 1} {
		num := make([]int, len(lines))
		// One open list per nesting level: its marker column, content column, kind
		// ("." ")" for ordered, the bullet otherwise) and next number.
		type list struct {
			ind, content, next int
			kind               string
		}
		var stack []list
		// para: the line above is paragraph text, which only a bullet or "1." may
		// interrupt. quote: the blockquote depth the open lists sit in.
		prevBlank, para, quote := false, false, 0
		for i, l := range lines {
			if change[i] == -side {
				continue
			}
			t := strings.TrimSpace(l)
			// A quote inside an item's content ("   > 需要管理员权限") leaves its list open;
			// left of a nested item's content it closes that sublist only.
			lead := len(strings.ReplaceAll(l[:len(l)-len(strings.TrimLeft(l, " \t"))], "\t", "    "))
			if quote == 0 && diffQuoteDepth(t) > 0 && len(stack) > 0 && lead >= stack[0].content {
				for stack[len(stack)-1].content > lead {
					stack = stack[:len(stack)-1]
				}
				prevBlank, para = false, false
				continue
			}
			// A list in a blockquote is read behind its markers; another depth is
			// another container, whose lists start over.
			if q := diffQuoteDepth(t); q != quote {
				stack, para, quote = nil, false, q
			}
			if quote > 0 {
				l = diffStripQuotes(t, quote)
				t = strings.TrimSpace(l)
			}
			if t == "" {
				prevBlank, para = true, false
				continue
			}
			ind := len(strings.ReplaceAll(l[:len(l)-len(strings.TrimLeft(l, " \t"))], "\t", "    "))
			blank := prevBlank
			prevBlank = false
			om := diffCellListRE.FindStringSubmatch(t)
			// Indented code (masked) is no item.
			item := !masks[si][i] && (om != nil || diffListItemRE.MatchString(t) && !diffRuleRE.MatchString(t))
			text := func(t string) bool {
				return t != "" && !diffHeadingRE.MatchString(t) && !diffRuleRE.MatchString(t) &&
					!strings.HasPrefix(t, ">") && !IsDiffTableRow(t) && diffFenceOpen(t) == ""
			}
			plain := !masks[si][i] && text(t)
			// The lists the line is not inside: an item left of a list's content is a
			// sibling of that list's items (or of an outer one).
			var sib *list
			saved := slices.Clone(stack)
			if item || blank || !plain {
				for len(stack) > 0 && stack[len(stack)-1].content > ind {
					top := stack[len(stack)-1]
					sib, stack = &top, stack[:len(stack)-1]
				}
			}
			kind, n := "", 0
			if item {
				kind = t[:1]
				if om != nil {
					kind = om[2]
					n, _ = strconv.Atoi(om[1])
				}
			}
			cont := item && sib != nil && sib.kind == kind
			if item && !cont && para && om != nil && n != 1 {
				// Paragraph text: "2. 这是正文" under "先说明" continues it.
				item, num[i] = false, -1
			}
			if !item {
				if plain && !blank {
					// A lazy continuation line leaves every list open.
					stack = saved
				}
				para = plain
				continue
			}
			// An item opens a paragraph only with text after its marker ("1. ## 模块" has
			// a heading, which a nested "2." may follow).
			content := ind + len(diffContainerRE.FindString(t))
			para = text(strings.TrimSpace(diffContainerRE.ReplaceAllString(t, "")))
			if cont && om != nil {
				n = sib.next
			}
			stack = append(stack, list{ind: ind, content: content, kind: kind, next: n + 1})
			num[i] = n
		}
		nums[si] = num
	}
	out := make([]int, len(lines))
	for i := range lines {
		out[i] = nums[1][i]
		if change[i] < 0 {
			out[i] = nums[0][i]
		}
	}
	return out
}

// diffRefDestRE matches a definition's destination with an optional title.
var diffRefDestRE = regexp.MustCompile(`^\s*(?:<[^<>]*>|[^\s<]\S*)` + diffLinkTitle + `\s*$`)

// diffRefTitledRE matches a destination that carries its title already.
var diffRefTitledRE = regexp.MustCompile(`^\s*(?:<[^<>]*>|[^\s<]\S*)` + diffLinkTitle[:len(diffLinkTitle)-1] + `\s*$`)

// diffRefTitleRE matches a definition title alone on its line.
var diffRefTitleRE = regexp.MustCompile(`^\s*(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\((?:\\.|[^)\\])*\))\s*$`)

// diffRefDefRE matches the start of a link reference definition, capturing its
// label and what follows the colon (the destination may sit on the next line).
var diffRefDefRE = regexp.MustCompile(`^ {0,3}\[((?:\\.|[^\[\]\\])+)\]:(.*)$`)

// DiffRefLabels reports, per line, the link reference labels its own text defines
// outside code (the old one for change < 0, the new one otherwise), normalized as
// the note matches them, so a deleted line never borrows a label added by the AI.
func DiffRefLabels(lines []string, change []int) []map[string]bool {
	labels, _ := diffRefScan(lines, change)
	out := make([]map[string]bool, len(lines))
	for i := range lines {
		out[i] = labels[1]
		if change[i] < 0 {
			out[i] = labels[0]
		}
	}
	return out
}

// DiffRefDefs marks the lines of link reference definitions in the line's own text
// (an unchanged line: the new one), destination lines included: the note shows
// nothing for them.
func DiffRefDefs(lines []string, change []int) []bool {
	_, defs := diffRefScan(lines, change)
	out := defs[1]
	for i := range lines {
		if change[i] < 0 {
			out[i] = defs[0][i]
		}
	}
	return out
}

// diffRefScan finds the link reference definitions of the old (0) and new (1) text:
// the labels each defines and the lines they take.
func diffRefScan(lines []string, change []int) (labels [2]map[string]bool, defs [2][]bool) {
	masks, heads := diffScan(lines, change)
	// A "[x]: y" inside a formula block defines nothing.
	math := diffMathSides(lines, change)
	// A definition in a blockquote counts too ("> [a]: …").
	body := func(l string) string {
		l = strings.TrimRight(l, "\r\n")
		if t := strings.TrimSpace(l); diffQuoteDepth(t) > 0 {
			return diffStripQuotes(t, diffQuoteDepth(t))
		}
		return l
	}
	for si, side := range [2]int{-1, 1} {
		refs, def := map[string]bool{}, make([]bool, len(lines))
		// A definition cannot interrupt a paragraph: "见 [文档][doc]" over "[doc]: /guide"
		// is one paragraph of text.
		para, paraDepth, skip := false, 0, -1
		for i, l := range lines {
			if change[i] == -side || i <= skip {
				continue
			}
			t := strings.TrimSpace(l)
			// A setext underline ends its heading: a definition may follow it.
			if masks[si][i] || math[si][i] || t == "" || heads[si][i] < 0 {
				para = false
				continue
			}
			depth := diffQuoteDepth(t)
			m := diffRefDefRE.FindStringSubmatch(body(l))
			// "[^1]: 来源" is a footnote, which the note shows.
			if m != nil && strings.HasPrefix(m[1], "^") {
				m = nil
			}
			if m == nil || para && paraDepth == depth {
				b := strings.TrimSpace(body(l))
				para, paraDepth = b != "" && !diffHeadingRE.MatchString(b) && !diffRuleRE.MatchString(b), depth
				continue
			}
			dest, next := m[2], -1
			if strings.TrimSpace(dest) == "" {
				// "[docs]:" alone takes its destination from the next line of its text.
				for k := i + 1; k < len(lines); k++ {
					// It stays in the definition's container: "[doc]:" over "> /guide"
					// takes nothing from the quote.
					if change[k] != -side {
						if diffQuoteDepth(strings.TrimSpace(lines[k])) == depth {
							dest, next = body(lines[k]), k
						}
						break
					}
				}
			}
			// What follows must be a destination (and maybe a title): "## 配置" is not.
			if !diffRefDestRE.MatchString(dest) {
				para, paraDepth = true, depth
				continue
			}
			refs[diffRefLabel(m[1])], def[i], para = true, true, false
			if next >= 0 {
				def[next], skip = true, next
			}
			// A title may sit on the line below: "[手册]: /guide" over `  "安装手册"`.
			if !diffRefTitledRE.MatchString(dest) {
				for k := max(i, next) + 1; k < len(lines); k++ {
					if change[k] != -side {
						if diffQuoteDepth(strings.TrimSpace(lines[k])) == depth && diffRefTitleRE.MatchString(body(lines[k])) {
							def[k], skip = true, k
						}
						break
					}
				}
			}
		}
		labels[si], defs[si] = refs, def
	}
	return labels, defs
}

// diffRefLabel normalizes a reference label: case-folded, inner whitespace collapsed.
func diffRefLabel(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// diffContainerRE matches the blockquote and list markers a fence may sit behind.
var diffContainerRE = regexp.MustCompile(`^(?:>\s*|(?:[-*+]|\d{1,9}[.)])\s+)+`)

// diffListItemRE matches a list item marker at the start of a trimmed line.
var diffListItemRE = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])(?:\s|$)`)

// diffItemInterrupts reports whether a list item marker on a trimmed line may
// interrupt a paragraph: a bullet or "1." with text after it, not "2." or a bare "-".
func diffItemInterrupts(t string) bool {
	m := diffItemStartRE.FindStringSubmatch(t)
	return m != nil && (m[1] == "" || m[1] == "1")
}

// diffItemStartRE matches a list item with text; group 1 is an ordered item's number.
var diffItemStartRE = regexp.MustCompile(`^(?:[-*+]|(\d{1,9})[.)])[ \t]+\S`)

// diffRuleRE matches a thematic break such as "* * *" or "---".
var diffRuleRE = regexp.MustCompile(`^(?:\*[ \t]*){3,}$|^(?:-[ \t]*){3,}$|^(?:_[ \t]*){3,}$`)

// diffHeadingRE matches an ATX heading such as "## Title".
var diffHeadingRE = regexp.MustCompile(`^#{1,6}(?:\s|$)`)

// diffSetextRE matches a setext heading underline ("===" or "--" of any length).
var diffSetextRE = regexp.MustCompile(`^(?:=+|-+)$`)

// diffQuoteRE matches the blockquote markers in front of a fence closer.
var diffQuoteRE = regexp.MustCompile(`^(?:>\s*)+`)

// diffQuoteDepth counts the blockquote markers a trimmed line opens with.
func diffQuoteDepth(t string) int {
	return strings.Count(diffQuoteRE.FindString(t), ">")
}

// diffStripQuotes removes n blockquote markers, each with one optional space after
// it, from the start of a trimmed line; the indent of what follows is kept.
func diffStripQuotes(t string, n int) string {
	for ; n > 0; n-- {
		u := strings.TrimLeft(t, " \t")
		if !strings.HasPrefix(u, ">") {
			break
		}
		t = strings.TrimPrefix(u[1:], " ")
	}
	return t
}

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
// the old text, > 0 to the new one, 0 to both; a line is code if its side says so,
// an unchanged one if the new text does ("# 部署" freed from its fence is a heading).
func DiffFenceMask(lines []string, change []int) []bool {
	old, cur := diffFenceSides(lines, change)
	for i := range old {
		if change[i] >= 0 {
			old[i] = cur[i]
		}
	}
	return old
}

// DiffHardBreaks marks the lines whose trailing "\" is a hard line break: outside
// code, with a line of text after it in the line's own text (an unchanged line: the
// new one). At the end of a paragraph ("备份目录 C:\backup\") the "\" is text.
func DiffHardBreaks(lines []string, change []int) []bool {
	masks, _ := diffScan(lines, change)
	math := diffMathSides(lines, change)
	out := make([]bool, len(lines))
	for si, side := range [2]int{-1, 1} {
		for i, l := range lines {
			if change[i] == -side || side < 0 && change[i] == 0 || masks[si][i] || math[si][i] {
				continue
			}
			l = strings.TrimRight(l, "\r\n")
			if !strings.HasSuffix(l, `\`) || diffEscaped(l, len(l)-1) {
				continue
			}
			// A heading's "\" is text: "## 备份目录 C:\backup\" keeps it.
			lt := strings.TrimSpace(l)
			own := strings.TrimSpace(diffStripQuotes(lt, diffQuoteDepth(lt)))
			if diffHeadingRE.MatchString(own) {
				continue
			}
			// Inside a list a new item ends the paragraph; outside one only an item that
			// may interrupt a paragraph does, so "先备份\" over "2. 验证服务" breaks.
			inItem := diffListItemRE.MatchString(own) || len(l) > len(strings.TrimLeft(l, " \t"))
			for k := i + 1; k < len(lines); k++ {
				if change[k] != -side {
					t := strings.TrimSpace(lines[k])
					in := strings.TrimSpace(diffStripQuotes(t, diffQuoteDepth(t)))
					// A rule, heading, fence or list item below starts a block of its own:
					// over "***" the "\" of "C:\backup\" is text.
					out[i] = in != "" && !masks[si][k] && diffQuoteDepth(t) == diffQuoteDepth(strings.TrimSpace(l)) &&
						!diffRuleRE.MatchString(in) && !diffHeadingRE.MatchString(in) && !diffSetextRE.MatchString(in) &&
						!(inItem && diffListItemRE.MatchString(in) || diffItemInterrupts(in)) && diffFenceOpen(in) == ""
					break
				}
			}
		}
	}
	return out
}

// diffFenceLineRE matches a code fence line ("```sh", "> ~~~", "- ```sh") behind
// quote and list markers; group 1 is the markers, 2 the fence run, 3 its info string.
var diffFenceLineRE = regexp.MustCompile("^((?:>\\s*)*(?:(?:[-*+]|\\d{1,9}[.)])\\s+)?)(`{3,}|~{3,})(.*)$")

// DiffFenceLines marks the opening and closing fence lines of the code blocks in
// DiffFenceMask, each side read on its own (an unchanged line: the new text); "~~~ 输出"
// inside a "~~~" block is a line of code, since a closing fence takes no text. quote
// gives each code line its block's quote depth.
func DiffFenceLines(lines []string, change []int) (fence []bool, quote []int) {
	masks, _, kinds := diffScanFences(lines, change)
	fence, quote = make([]bool, len(lines)), make([]int, len(lines))
	for si, side := range [2]int{-1, 1} {
		depth := 0
		for i, l := range lines {
			if change[i] == -side {
				continue
			}
			k := kinds[si][i]
			if !masks[si][i] {
				depth = 0
			} else if k == 1 {
				depth = diffQuoteDepth(strings.TrimSpace(l))
			}
			// The new text has the last word on a line both sides share.
			if change[i] != 0 || side > 0 {
				fence[i], quote[i] = k != 0, depth
			}
			if k == 2 {
				depth = 0
			}
		}
	}
	return fence, quote
}

// diffFenceSides is DiffFenceMask for the old and the new text separately; a line
// absent from a side is false there.
func diffFenceSides(lines []string, change []int) (old, cur []bool) {
	masks, _ := diffScan(lines, change)
	return masks[0], masks[1]
}

// DiffParaText is the DiffSetextLevels value of a line indented four columns under
// paragraph text: it continues the paragraph, so "    # 打印当前目录" keeps its "#".
const DiffParaText = 3

// DiffSetextLevels reports, per line, the setext heading it belongs to: 1 or 2 for
// its paragraph text, -1 for the underline, DiffParaText for indented paragraph text,
// 0 otherwise. Rendered one by one, "Title"
// would read as a paragraph and "---" as a rule. A changed line takes its own side,
// an unchanged one the new text.
func DiffSetextLevels(lines []string, change []int) []int {
	_, heads := diffScan(lines, change)
	out := make([]int, len(lines))
	for i := range lines {
		out[i] = heads[1][i]
		if change[i] < 0 {
			out[i] = heads[0][i]
		}
	}
	return out
}

// diffScan reads the old and the new text of a diff block by block: masks are the
// diffFenceSides code masks, heads the per-side DiffSetextLevels.
func diffScan(lines []string, change []int) (masks [2][]bool, heads [2][]int) {
	masks, heads, _ = diffScanFences(lines, change)
	return masks, heads
}

// diffScanFences is diffScan that also gives, per side, each fence line: 1 for an
// opener, 2 for a closer, as the scan reads them (indent and quote depth included).
func diffScanFences(lines []string, change []int) (masks [2][]bool, heads [2][]int, fences [2][]int8) {
	for si, side := range [2]int{-1, 1} {
		mask, head := make([]bool, len(lines)), make([]int, len(lines))
		fl := make([]int8, len(lines))
		fence, nested, fenceList, fenceQuote := "", false, false, 0
		// fenceIndent: the content column of the list item a fence sits in (0: none).
		fenceIndent := 0
		var paraLines []int
		// Indented code: four columns in after a blank line (or the start); in a list,
		// four past the item's content column (listCol), since its nested content is
		// indented too.
		// para: the line above is paragraph text, which a "===" underline turns into a heading.
		inIndented, prevBlank, inList, para, listCol := false, true, false, false, 0
		// cols: the content columns of the open list items, innermost last.
		var cols []int
		// mathClose: the delimiter that ends the open display math block ("": none).
		mathClose := ""
		for i, l := range lines {
			if change[i] == -side {
				continue
			}
			t := strings.TrimSpace(l)
			lead := len(strings.ReplaceAll(l[:len(l)-len(strings.TrimLeft(l, " \t"))], "\t", "    "))
			// A fence opened in a blockquote ends with the quote: at a blank or a line
			// with fewer ">" markers, which is read as ordinary text again. One in a list
			// item ends with the item, at a line left of its content.
			if fence != "" && fenceQuote > 0 && diffQuoteDepth(t) < fenceQuote ||
				fence != "" && fenceIndent > 0 && t != "" && lead < fenceIndent {
				fence = ""
			}
			// A display math block is a block of its own to its closer: "x^2$$" over
			// "---" is no heading.
			if fence == "" && mathClose != "" {
				if strings.HasSuffix(t, mathClose) {
					mathClose = ""
				}
				prevBlank, para, paraLines = false, false, nil
				continue
			}
			if fence == "" {
				if t == "" {
					prevBlank, para, paraLines = true, false, nil
					continue
				}
				ind := len(strings.ReplaceAll(l[:len(l)-len(strings.TrimLeft(l, " \t"))], "\t", "    "))
				if c := diffMathOpen(t); ind < 4 && !inIndented && c != "" {
					mathClose, prevBlank, para, paraLines = c, false, false, nil
					continue
				}
				wasBlank := prevBlank
				prevBlank = false
				// After a blank, or at a fence, a line left of a nested item's content
				// belongs to an outer item (a fence under "1. 检查环境" past its sublist),
				// and one left of every item's ends the list.
				if len(cols) > 0 && (wasBlank || diffFenceOpen(t) != "") && !diffListItemRE.MatchString(t) {
					for len(cols) > 0 && ind < cols[len(cols)-1] {
						cols = cols[:len(cols)-1]
					}
					if len(cols) == 0 {
						inList, listCol = false, 0
					} else {
						listCol = cols[len(cols)-1]
					}
				}
				need := 4
				if inList {
					need += listCol
				}
				if ind >= need && (inIndented || wasBlank) {
					mask[i], inIndented, para, paraLines = true, true, false, nil
					continue
				}
				if ind >= need && para {
					head[i], paraLines = DiffParaText, append(paraLines, i)
					continue
				}
				inIndented = false
				// In a blockquote the block is read behind its markers: "> 标题" over "> ---"
				// is a heading in the quote.
				inner := t
				if q := diffQuoteDepth(t); q > 0 {
					inner = strings.TrimSpace(diffStripQuotes(t, q))
				}
				// A quote line with nothing behind its ">" ends the paragraph above it:
				// "> 原因" over ">" over "> ---" is a rule, not a heading.
				if inner == "" {
					prevBlank, para, paraLines = true, false, nil
					continue
				}
				// Under paragraph text (not a list item) "-" or "==" is a setext underline,
				// not an empty list item, which may not interrupt a paragraph.
				setext := ind < 4 && para && !inList && diffSetextRE.MatchString(inner)
				item := !setext && diffListItemRE.MatchString(t) && !diffRuleRE.MatchString(t)
				if item {
					for len(cols) > 0 && cols[len(cols)-1] > ind {
						cols = cols[:len(cols)-1]
					}
					listCol = ind + len(diffContainerRE.FindString(t))
					cols = append(cols, listCol)
				}
				inList = item || !setext && inList && ind > 0
				if !inList {
					cols = nil
				}
				// A rule, heading or setext underline ends its block on that line, so code
				// may follow at once. Indented four or more, such a line is paragraph text.
				ends := setext || ind < 4 && (diffRuleRE.MatchString(inner) || diffHeadingRE.MatchString(inner) || diffMathStart(inner))
				prevBlank, para = !inList && ends, !ends
				if setext && diffSetextText(lines, paraLines, diffQuoteDepth(t)) {
					for _, p := range paraLines {
						head[p] = 2 - min(1, strings.Count(t, "="))
					}
					head[i] = -1
				}
				paraLines = append(paraLines, i)
				if !para {
					paraLines = nil
				}
			}
			if fence != "" {
				mask[i] = true
				// Inside a plain fence "> ~~~" is code; only a fence opened behind a list or
				// quote marker may close behind a quote marker. List items close by indent,
				// so "- ~~~" inside a fence never closes it.
				// A quote fence closes at its own depth only: in "> ~~~", ">> ~~~" is code.
				quoteLead := 0
				if nested && fenceQuote > 0 {
					r := diffStripQuotes(t, fenceQuote)
					t = strings.TrimLeft(r, " \t")
					quoteLead = len(strings.ReplaceAll(r[:len(r)-len(t)], "\t", "    "))
				} else if nested {
					t = diffQuoteRE.ReplaceAllString(t, "")
				}
				// A closer indented four columns past its block is code.
				if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" &&
					(fenceQuote > 0 && quoteLead < 4 || fenceQuote == 0 && lead-fenceIndent < 4) {
					// Code may follow at once, unless the fence sat in a list item, whose
					// content is indented the same way.
					fence, prevBlank, inIndented = "", !fenceList, false
					fl[i] = 2
				}
				continue
			}
			// Indented four columns (past a list item's content) "```" is paragraph text
			// or code, never a fence.
			lim := 4
			if inList {
				lim += listCol
			}
			if fence = diffFenceOpen(t); fence != "" && lead >= lim {
				fence = ""
			}
			if fence != "" {
				mask[i], para, paraLines = true, false, nil
				fl[i] = 1
				nested, fenceQuote = diffContainerRE.MatchString(t), diffQuoteDepth(t)
				fenceList = inList || diffListItemRE.MatchString(diffQuoteRE.ReplaceAllString(t, ""))
				// The item's content column, not the fence's own indent: "    ```" under
				// "- 运行" still closes at "  ```".
				fenceIndent = 0
				if fenceQuote == 0 && fenceList {
					fenceIndent = listCol
				}
			}
		}
		masks[si], heads[si], fences[si] = mask, head, fl
	}
	return masks, heads, fences
}

// diffMathOpen gives the closer of a display math block a trimmed line opens and
// leaves open ("$$" or "\\[" with no closer after it), or "".
func diffMathOpen(t string) string {
	for _, d := range [2][2]string{{"$$", "$$"}, {`\[`, `\]`}} {
		if strings.HasPrefix(t, d[0]) && !strings.Contains(t[len(d[0]):], d[1]) {
			return d[1]
		}
	}
	return ""
}

// diffMathStart reports whether a trimmed line opens display math ("$$" or "\["),
// which is a block of its own: a "---" under "$$x^2$$" is a rule, not an underline.
// A closing delimiter with text after it makes the line inline math in a paragraph.
func diffMathStart(t string) bool {
	for _, d := range [2][2]string{{"$$", "$$"}, {`\[`, `\]`}} {
		if strings.HasPrefix(t, d[0]) {
			body := t[len(d[0]):]
			j := strings.Index(body, d[1])
			return j < 0 || strings.TrimSpace(body[j+len(d[1]):]) == ""
		}
	}
	return false
}

// diffSetextText reports whether the paragraph lines above an underline form a plain
// setext heading: not a list item or table, and in the underline's blockquote depth
// q, since a quoted paragraph over a bare "---" is a rule (no lazy underline). Bar
// lines are paragraph text until a separator row makes them a table: "| A |" over
// "---" is a heading, since the underline is read first.
func diffSetextText(lines []string, para []int, q int) bool {
	for j, p := range para {
		t := strings.TrimSpace(lines[p])
		if diffQuoteDepth(t) != q {
			return false
		}
		t = strings.TrimSpace(diffStripQuotes(t, q))
		if diffListItemRE.MatchString(t) || j > 0 && IsDiffTableSep(t) {
			return false
		}
	}
	return len(para) > 0
}

// SyncDiffTableSeps decides each changed table separator row, which the review views
// hide, by checking the text the selection would write: in every table that results,
// exactly one separator sits right under the header row and none anywhere else, so a
// partial accept never leaves an orphan "| --- |" nor turns a table into a paragraph
// by accident. applied[i] says whether changed line i is taken; unchanged lines are
// ignored. A line the selection drops (a deleted blank, or a rejected inserted one)
// that would merge two tables is kept as well. When the table has visible changed
// rows, its new separator is used only if one of them is taken; a table shown
// unchanged (an alignment change, or a table turned into text) follows the AI's text.
func SyncDiffTableSeps(lines []string, change []int, applied []bool) {
	shown := DiffFenceMask(lines, change)
	oldM, newM := diffFenceSides(lines, change)
	trim := func(k int) string { return strings.TrimSpace(lines[k]) }
	_, seps := DiffTableRoles(lines, change)
	cand := func(k int) bool { return change[k] != 0 && !shown[k] && seps[k] }
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
	members := diffTableMembers(lines, change)
	for diffSepPass(lines, change, applied, cand, kept, take, indent, tables, members, seps) {
	}
}

// diffSepPass is one pass of SyncDiffTableSeps over the written text. It reports
// whether it had to keep a dropped line that would merge two tables (losing the
// blank between them leaves a second separator as a data row); the next pass then
// sees them apart.
func diffSepPass(lines []string, change []int, applied []bool, cand, kept func(int) bool,
	take func(int, bool), indent func(int) int, tables [2][]int, members [2][]bool, seps []bool) bool {
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
		var rows, cands, dels, ins, drops []int
	run:
		for ; i < len(lines); i++ {
			switch {
			case cand(i):
				cands = append(cands, i)
			case !kept(i):
				switch {
				case oldTable[i] > 0:
					dels = append(dels, i)
				case newTable[i] > 0:
					ins = append(ins, i)
				default:
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
		// A separator already kept below the first row fixes the table; a data row that
		// merely looks like one ("| --- |" under other rows) does not.
		fixed := false
		for _, r := range rows[1:] {
			fixed = fixed || seps[r]
		}
		// Two rows that meet here but were one table in neither text were split by a
		// line the selection drops (the blank between two tables): keep it. Rows with
		// no separator among them are paragraph text, which may merge.
		sep := fixed || len(cands) > 0 || IsDiffTableSep(trim(rows[0]))
		again := false
		for j := 1; j < len(rows) && sep; j++ {
			a, b := rows[j-1], rows[j]
			sameOld := change[a] <= 0 && change[b] <= 0 && oldTable[a] != 0 && oldTable[a] == oldTable[b]
			sameNew := change[a] >= 0 && change[b] >= 0 && newTable[a] != 0 && newTable[a] == newTable[b]
			if sameOld || sameNew {
				continue
			}
			// One line is enough: a blank first, else what the old text had there.
			best := -1
			for _, d := range drops {
				if a < d && d < b && (best < 0 || diffGapRank(lines[d], change[d]) < diffGapRank(lines[best], change[best])) {
					best = d
				}
			}
			if best >= 0 {
				applied[best], again = change[best] > 0, true
			}
		}
		if again {
			return true
		}
		// A block holds one table: the row right above its separator is the header (rows
		// above that are paragraph text) and every row after it is data.
		if fixed {
			continue
		}
		// taken: a visible change of this very table (a row from its header down in
		// that text, not paragraph text above it) is accepted, so its new form wins;
		// touched: it has one at all. A table the review shows unchanged follows the
		// AI's text, so a separator removed to turn it into text stays removed.
		taken, touched := false, false
		oldIDs, newIDs := map[int]bool{}, map[int]bool{}
		for _, r := range rows {
			taken = taken || change[r] > 0 && members[1][r]
			touched = touched || change[r] != 0
			if change[r] <= 0 {
				oldIDs[oldTable[r]] = true
			}
			if change[r] >= 0 {
				newIDs[newTable[r]] = true
			}
		}
		for _, d := range dels {
			taken = taken || oldIDs[oldTable[d]] && members[0][d]
			touched = touched || oldIDs[oldTable[d]]
		}
		for _, n := range ins {
			touched = touched || newIDs[newTable[n]]
		}
		newSide := taken || !touched
		// fits: k can sit under its header, the last row above it. A header of k's own
		// side must be the very row k sat under in its text, so a separator never turns
		// a row that was text in that version into a header (both headers dropped leave
		// text); and the cell counts must match, or the result reads as text.
		fits := func(k int) (bool, int) {
			h := rows[0]
			for _, r := range rows {
				if r < k {
					h = r
				}
			}
			own := change[h] == 0 || (change[h] > 0) == (change[k] > 0)
			above := k - 1
			for above >= 0 && change[above] == -change[k] {
				above--
			}
			cols := len(ParseDiffTableCells(trim(h))) == len(ParseDiffTableCells(trim(k)))
			return k > rows[0] && cols && !(own && h != above), h
		}
		pick := -1
		for _, k := range cands {
			// A changed header keeps its own version's separator (and alignment); under
			// an unchanged one the winning side's is used. None there reads as text.
			ok, h := fits(k)
			side := newSide
			if change[h] != 0 {
				side = change[h] > 0
			}
			if ok && (change[k] > 0) == side {
				pick = k
				break
			}
		}
		// Both versions have a separator, but the chosen one would land under a row of
		// the other version (kept old rows sit above the new separator): one that sits
		// under its own header keeps the table, with the other alignment.
		var sides [2]bool
		for _, k := range cands {
			sides[(change[k]+1)/2] = true
		}
		for _, k := range cands {
			if ok, h := fits(k); pick < 0 && sides[0] && sides[1] && ok &&
				(change[h] == 0 || (change[h] > 0) == (change[k] > 0)) {
				pick = k
			}
		}
		if pick >= 0 {
			take(pick, true)
		}
	}
	return false
}

// DiffTableRoles reports, per line, whether it is a table's header row or its
// separator in its own text (the old one for change < 0, the new one for > 0 and 0:
// a table wrapped in a fence shows its "| --- |" as code). A separator must follow a row of as many cells at the same indent, and
// only the first one does: a later "| --- |" is a data row and stays visible.
func DiffTableRoles(lines []string, change []int) (header, sep []bool) {
	header, sep = make([]bool, len(lines)), make([]bool, len(lines))
	diffTableRuns(lines, change, func(si int, run []int, h int) {
		if h >= 0 {
			// An unchanged header is the new text's: with the separator gone it is text.
			header[run[h]] = header[run[h]] || si == 1 || change[run[h]] < 0
			sep[run[h+1]] = sep[run[h+1]] || si == 1 || change[run[h+1]] < 0
		}
	})
	return header, sep
}

// diffTableMembers reports, per side (0 old, 1 new), whether a line is part of a
// table in that text: its header, separator or a data row; rows above a header are
// paragraph text.
func diffTableMembers(lines []string, change []int) (members [2][]bool) {
	members = [2][]bool{make([]bool, len(lines)), make([]bool, len(lines))}
	diffTableRuns(lines, change, func(si int, run []int, h int) {
		if h >= 0 {
			for _, k := range run[h:] {
				members[si][k] = true
			}
		}
	})
	return members
}

// diffTableRuns calls fn for each block of table rows (contiguous, outside code, at
// one indent) of the old (side 0) and then the new (side 1) text, with the index in
// run of its header row, or -1 when no separator makes it a table.
func diffTableRuns(lines []string, change []int, fn func(side int, run []int, h int)) {
	old, cur := diffFenceSides(lines, change)
	trim := func(k int) string { return strings.TrimSpace(lines[k]) }
	indent := func(k int) int { return len(lines[k]) - len(strings.TrimLeft(lines[k], " \t")) }
	for si, side := range [2]int{-1, 1} {
		code := [2][]bool{old, cur}[si]
		var run []int
		flush := func() {
			h := -1
			for j := 1; j < len(run) && h < 0; j++ {
				if IsDiffTableSep(trim(run[j])) &&
					len(ParseDiffTableCells(trim(run[j-1]))) == len(ParseDiffTableCells(trim(run[j]))) {
					h = j - 1
				}
			}
			if len(run) > 0 {
				fn(si, run, h)
			}
			run = nil
		}
		for k := range lines {
			if change[k] == -side {
				continue
			}
			if code[k] || !IsDiffTableRow(trim(k)) || len(run) > 0 && indent(run[0]) != indent(k) {
				flush()
			}
			if !code[k] && IsDiffTableRow(trim(k)) {
				run = append(run, k)
			}
		}
		flush()
	}
}

// diffGapRank orders the dropped lines that could keep two tables apart: a blank,
// then a deleted line (the old text had it), then a rejected inserted one.
func diffGapRank(line string, change int) int {
	switch {
	case strings.TrimSpace(line) == "":
		return 0
	case change < 0:
		return 1
	}
	return 2
}

// diffLinkDest matches a link destination: <with spaces> or bare, with balanced
// parentheses nested up to eight deep; "\(" or "\)" is an ordinary character.
var diffLinkDest = func() string {
	unit := `(?:\\\S|[^()\s\\])`
	paren := `\(` + unit + `*\)`
	for range 7 {
		paren = `\((?:` + unit + `|` + paren + `)*\)`
	}
	return `(?:<[^<>\n]*>|(?:` + unit + `|` + paren + `)*)`
}()

// diffLinkTitle matches an optional link title: "t", 't' or (t), where a backslash
// escapes the closing mark ("版本 \"Beta\"").
const diffLinkTitle = `(?:\s+(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\((?:\\.|[^)\\])*\)))?`

var (
	// Spaces may pad the destination: "[Docs]( /docs )" is a link too. In the text an
	// escaped bracket ("\]") is literal, and one level of brackets may nest
	// ("![构建状态 [CI]](…)").
	diffImageRE    = regexp.MustCompile(`!\[((?:\\.|[^\[\]\\]|\[[^\[\]]*\])*)\]\(\s*(` + diffLinkDest + `)` + diffLinkTitle + `\s*\)`)
	diffLinkRE     = regexp.MustCompile(`\[((?:\\.|[^\[\]\\]|\[[^\[\]]*\])+)\]\(\s*` + diffLinkDest + diffLinkTitle + `\s*\)`)
	diffRefLinkRE  = regexp.MustCompile(`!?\[((?:\\.|[^\[\]\\]|\[[^\[\]]*\])+)\](?:\[((?:\\.|[^\[\]\\])*)\])?`)
	diffAutolinkRE = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\s]*|[^<>\s@]+@[^<>\s@]+)>`)
)

// CodeSpans returns the [start, end) byte ranges of the code spans in s, each
// delimited by equal backtick runs.
func CodeSpans(s string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(s); {
		// A backslash-escaped backtick is literal and opens nothing.
		if s[i] != '`' || diffEscaped(s, i) {
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

// PlainLinks turns the links, images and autolinks in s into their text; refs are
// the defined reference labels (see DiffRefLabels).
func PlainLinks(refs map[string]bool, s string) string { return diffPlainLinks(s, refs) }

// diffPlainLinks turns links into their text: a link inside a diff row would take the
// click (opening a browser) instead of the row's accept toggle. Links are found with
// code spans blanked out, so code is never rewritten and brackets inside it don't
// hide a link.
func diffPlainLinks(s string, refs map[string]bool) string {
	for _, re := range []*regexp.Regexp{diffImageRE, diffLinkRE, diffAutolinkRE, diffRefLinkRE} {
		if re == diffRefLinkRE && len(refs) == 0 {
			continue
		}
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
			text := s[m[2]:m[3]]
			if re == diffRefLinkRE {
				// "[text][label]", "[label][]" or "[label]" links only when the label is
				// defined; "[label]: …" is the definition itself.
				label := text
				if m[4] >= 0 && m[5] > m[4] {
					label = s[m[4]:m[5]]
				}
				// Only a lone "[label]" at the start may be a definition or inline link.
				short := m[4] < 0
				if !refs[diffRefLabel(label)] || short && m[1] < len(s) && (s[m[1]] == '(' || s[m[1]] == ':' && m[0] == 0) {
					continue
				}
				b.WriteString(s[last:m[0]])
				b.WriteString(diffInlineStart(text))
				last = m[1]
				continue
			}
			b.WriteString(s[last:m[0]])
			if text == "" && len(m) > 4 && m[4] >= 0 {
				// An image without alt text shows its path, as code so "_" stays literal.
				if p := strings.Trim(s[m[4]:m[5]], "<>"); p != "" && !strings.Contains(p, "`") {
					text = "`" + p + "`"
				}
			}
			if re == diffAutolinkRE {
				// An autolink shows its address verbatim, so "__tests__" is not bold.
				text = diffLiteralEscaper.Replace(text)
			} else {
				// Link text is inline: "[# Release](…)" must not become a heading.
				text = diffInlineStart(text)
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
	if strings.TrimSpace(line) == "" || (st.Setext < 0 || st.RefDef) && !st.Code {
		return ui.Box(c).FillWidth().Height(theme.Space4)
	}
	if st.Fence {
		// The language is the info string's first word: "```sh title=x" shows sh.
		info := strings.Fields(strings.TrimLeft(diffFenceLineRE.ReplaceAllString(strings.TrimSpace(line), "$3"), "`~"))
		if len(info) == 0 {
			return ui.Box(c).FillWidth().Height(theme.Space4)
		}
		return ui.Text(c, info[0]).Font("monospace").FontSize(theme.FontBody).TextColor(st.Text).Grow(1).MinWidth(0)
	}
	if st.Alert != "" && !st.Code {
		line = "> **" + st.Alert + "**"
	}
	if st.Math {
		t := strings.TrimSpace(line)
		for _, d := range [2][2]string{{"$$", "$$"}, {`\[`, `\]`}} {
			if strings.HasPrefix(t, d[0]) {
				t = t[len(d[0]):]
			}
			t = strings.TrimSuffix(t, d[1])
		}
		if t = strings.TrimSpace(t); t == "" {
			return ui.Box(c).FillWidth().Height(theme.Space4)
		}
		return ui.Box(c).Grow(1).MinWidth(0).Children(func() {
			markdown.View(c, "$$"+t+"$$", diffMarkdownOptions(st))
		})
	}
	if st.Code {
		if st.Quote > 0 {
			line = diffStripQuotes(line, st.Quote)
		}
		return ui.Text(c, strings.ReplaceAll(line, "\t", "    ")).Font("monospace").FontSize(theme.FontBody).
			TextColor(st.Text).Grow(1).MinWidth(0)
	}
	// A hard line break's "\" is no text: "安装完成\" shows 安装完成.
	if st.Break {
		line = line[:len(line)-1]
	}
	// Leading spaces become padding: four of them alone would parse as a code block,
	// and a nested list item keeps its depth.
	body := strings.TrimLeft(line, " \t")
	indent := len(strings.ReplaceAll(line[:len(line)-len(body)], "\t", "    "))
	return ui.Box(c).Grow(1).MinWidth(0).Padding(0, 0, 0, float32(indent/2)*theme.Space8).Children(func() {
		// Links turn into their text first, so "[\[ A \]](…)" or "[v2 #](…)" is guarded too.
		// A line of its own starting with "\[" is display math, as in the note.
		math := strings.HasPrefix(body, `\[`)
		// An ordered item shows the number it has in its list, not the one written.
		// A marker that is paragraph text there (-1) gets its delimiter escaped.
		q := diffQuoteRE.FindString(body)
		if m := diffCellListRE.FindStringSubmatchIndex(body[len(q):]); m != nil && st.ListNum != 0 {
			rest := body[len(q):]
			if st.ListNum > 0 {
				body = q + strconv.Itoa(st.ListNum) + rest[m[3]:]
			} else {
				body = q + rest[:m[3]] + `\` + rest[m[3]:]
			}
		}
		body = diffPlainLinks(body, st.Refs)
		if !math {
			body = diffLiteralStart(body)
		}
		if st.Setext == DiffParaText {
			body = diffInlineStart(body)
		} else if st.Setext > 0 && !st.Code {
			// As an ATX heading a trailing "#" run would read as a closing sequence,
			// unless it is escaped already ("Title \#").
			if h := strings.TrimRight(body, " \t"); strings.HasSuffix(h, "#") {
				if k := len(strings.TrimRight(h, "#")); !diffEscaped(h, k) {
					body = h[:k] + `\` + h[k:]
				}
			}
			// In a blockquote the heading marker goes behind the quote markers.
			q := diffQuoteRE.FindString(body)
			body = q + strings.Repeat("#", st.Setext) + " " + body[len(q):]
		}
		markdown.View(c, body, diffMarkdownOptions(st))
	})
}

// diffAlertRE matches a top-level quote line that marks a GitHub alert ("> [!NOTE]").
var diffAlertRE = regexp.MustCompile(`(?i)^ {0,3}>[ \t]*\[!(note|tip|important|warning|caution)\][ \t]*$`)

// DiffAlertTitles gives an alert marker line its title ("Note"), in the line's own
// text (an unchanged line: the new one); other lines get "". Like the note, a
// marker makes an alert only with a line of text after it.
func DiffAlertTitles(lines []string, change []int) []string {
	masks, _ := diffScan(lines, change)
	out := make([]string, len(lines))
	for si, side := range [2]int{-1, 1} {
		for i, l := range lines {
			// The old text reads its deleted lines, the new one the rest.
			if change[i] == -side || side < 0 && change[i] == 0 || masks[si][i] {
				continue
			}
			// A plan's diff lines keep their "\n".
			m := diffAlertRE.FindStringSubmatch(strings.TrimRight(l, "\r\n"))
			if m == nil {
				continue
			}
			// Only a quote's first line marks it: in "> 引用原文\n> [!NOTE]" it is text.
			inQuote := false
			for k := i - 1; k >= 0; k-- {
				if change[k] != -side {
					inQuote = !masks[si][k] && strings.HasPrefix(strings.TrimSpace(lines[k]), ">")
					break
				}
			}
			if inQuote {
				continue
			}
			for k := i + 1; k < len(lines); k++ {
				if change[k] != -side {
					if strings.TrimSpace(lines[k]) != "" {
						out[i] = strings.ToUpper(m[1][:1]) + strings.ToLower(m[1][1:])
					}
					break
				}
			}
		}
	}
	return out
}

// diffLiteralStart keeps a line or cell start the parser would swallow: a link
// reference definition renders as nothing (its colon gets escaped), and an escaped
// "\[" would open display math (it becomes an entity, shown as a bracket).
func diffLiteralStart(s string) string {
	if diffLinkDefRE.MatchString(s) {
		s = strings.Replace(s, "]:", `]\:`, 1)
	}
	if strings.HasPrefix(s, `\[`) {
		return "&#91;" + s[2:]
	}
	return s
}

// diffLinkDefRE matches a link reference definition such as "[api]: https://…".
var diffLinkDefRE = regexp.MustCompile(`^\[[^\]]+\]:`)

// diffCellBlockRE matches a leading marker that would make a cell parse as a block
// (heading, quote, bullet, thematic break); a backslash in front keeps it literal.
var diffCellBlockRE = regexp.MustCompile("^(?:#{1,6}(?:\\s|$)|>|[-+*](?:\\s|$)|[-*_](?:\\s*[-*_]){2,}\\s*$)")

// diffCellListRE matches an ordered-list marker; its "." or ")" gets the backslash.
var diffCellListRE = regexp.MustCompile(`^(\d{1,9})([.)])(\s|$)`)

// diffInlineStart escapes a leading marker that would make inline text parse as a
// block: a heading, quote, list item, thematic break or code fence ("```x```" is
// inline code and stays).
func diffInlineStart(s string) string {
	switch {
	case diffCellListRE.MatchString(s):
		return diffCellListRE.ReplaceAllString(s, "$1\\$2$3")
	case diffCellBlockRE.MatchString(s), diffFenceOpen(s) != "" && !diffContainerRE.MatchString(s):
		return `\` + s
	}
	return s
}

// DiffMarkdownCell renders one table cell's inline Markdown (code spans, emphasis).
func DiffMarkdownCell(c *ui.Context, cell string, st DiffLineStyle, header bool) ui.Element {
	// A cell holds inline content only; links turn into their text first, so their
	// start is guarded too.
	s := strings.TrimSpace(diffLiteralStart(diffInlineStart(diffPlainLinks(cell, st.Refs))))
	if s == "" {
		return ui.Box(c)
	}
	// Bare "**" or "__" is text, not an empty emphasis.
	if strings.Trim(s, "*_") == "" {
		s = diffLiteralEscaper.Replace(s)
	}
	if header {
		s = "**" + diffUnbold(s) + "**"
	}
	return markdown.View(c, s, diffMarkdownOptions(st))
}

// diffUnbold drops the "**" pairs outside code spans: a header cell is bold all
// through, and markers nested in it would turn their text back to regular weight.
func diffUnbold(s string) string {
	spans := CodeSpans(s)
	inCode := func(i int) bool {
		for _, sp := range spans {
			if sp[0] <= i && i < sp[1] {
				return true
			}
		}
		return false
	}
	// A pair is an opener with text right after it and a closer with text right
	// before it; "m ** 2" has neither and stays literal.
	space := func(i int) bool { return i < 0 || i >= len(s) || s[i] == ' ' || s[i] == '\t' }
	var at []int
	open := -1
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '*' || s[i+1] != '*' || inCode(i) || diffEscaped(s, i) {
			continue
		}
		switch {
		case open >= 0 && !space(i-1):
			at, open = append(at, open, i), -1
		case open < 0 && !space(i+2):
			open = i
		}
		i++
	}
	// A "**" left over ("2**3") is escaped: inside the bold wrapper it would close it.
	var b strings.Builder
	last := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '*' || s[i+1] != '*' || inCode(i) || diffEscaped(s, i) {
			continue
		}
		b.WriteString(s[last:i])
		if !slices.Contains(at, i) {
			b.WriteString(`\*\*`)
		}
		last = i + 2
		i++
	}
	b.WriteString(s[last:])
	return b.String()
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
	// One bar comes off each end: in "| --- ||" the last cell is empty, not a separator.
	inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "|"), "|")
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

// DiffTableCols gives each table row the column count of its table's header, read in
// the row's own text (see DiffTableRoles), so a short row still lines up with it.
// Other lines get 0, and so do rows above the header and runs that no separator of
// as many cells makes a table, which are plain text with bars.
func DiffTableCols(lines []string, change []int) []int {
	var side [2][]int
	side[0], side[1] = make([]int, len(lines)), make([]int, len(lines))
	diffTableRuns(lines, change, func(si int, run []int, h int) {
		if h < 0 {
			return
		}
		n := len(ParseDiffTableCells(strings.TrimSpace(lines[run[h]])))
		for _, k := range run[h:] {
			side[si][k] = n
		}
	})
	// A row takes its own text's header (a deleted row is in the old text only); an
	// unchanged one the new header, so a table cut from two columns to one shows one
	// cell in a kept "| x | y |" row.
	cols := side[1]
	for k := range lines {
		// An unchanged row is read in the new text only: with the separator deleted,
		// "| 名称 |" is a paragraph with bars there.
		if cols[k] == 0 && change[k] < 0 {
			cols[k] = side[0][k]
		}
	}
	return cols
}

// DiffTableAligns gives each table row its columns' alignment, read from its
// table's separator in the row's own text (an unchanged row: the new one), so a
// "---" turned "---:" shows the column right-aligned. Other lines get nil.
func DiffTableAligns(lines []string, change []int) [][]ui.Align {
	var side [2][][]ui.Align
	side[0], side[1] = make([][]ui.Align, len(lines)), make([][]ui.Align, len(lines))
	diffTableRuns(lines, change, func(si int, run []int, h int) {
		if h < 0 {
			return
		}
		var al []ui.Align
		for _, cell := range ParseDiffTableCells(strings.TrimSpace(lines[run[h+1]])) {
			cell = strings.TrimSpace(cell)
			a := ui.Start
			switch l, r := strings.HasPrefix(cell, ":"), strings.HasSuffix(cell, ":"); {
			case l && r:
				a = ui.Center
			case r:
				a = ui.End
			}
			al = append(al, a)
		}
		for _, k := range run[h:] {
			side[si][k] = al
		}
	})
	out := side[1]
	for k := range lines {
		if change[k] < 0 || out[k] == nil {
			out[k] = side[0][k]
		}
	}
	return out
}

// DiffMathMask marks, per line, the lines of a display math block that spans lines
// ("$$" … "$$" or "\[" … "\]") in the line's own text (an unchanged line: the new
// one). Alone each would show as source; DiffMarkdownLine draws them as math.
func DiffMathMask(lines []string, change []int) []bool {
	side := diffMathSides(lines, change)
	out := side[1]
	for k := range lines {
		if change[k] < 0 {
			out[k] = side[0][k]
		}
	}
	return out
}

// diffMathSides marks the display math block lines of the old (0) and new (1) text.
// A block left open runs to the end, as in the note.
func diffMathSides(lines []string, change []int) (side [2][]bool) {
	old, cur := diffFenceSides(lines, change)
	for si, sd := range [2]int{-1, 1} {
		code := [2][]bool{old, cur}[si]
		mask := make([]bool, len(lines))
		closer, open := "", -1
		for i, l := range lines {
			// Inside a block, an indented line is formula text, not code.
			if change[i] == -sd || code[i] && open < 0 {
				continue
			}
			t := strings.TrimSpace(l)
			if open >= 0 {
				mask[i] = true
				if strings.HasSuffix(t, closer) {
					open = -1
				}
				continue
			}
			if c := diffMathOpen(t); c != "" {
				closer, open, mask[i] = c, i, true
			}
		}
		side[si] = mask
	}
	return side
}

// DiffTableRow draws one table row as cells split by hairlines, padded to cols cells.
func DiffTableRow(c *ui.Context, trimmed string, cols int, st DiffLineStyle, header bool, divider ui.Color) ui.Element {
	cells := ParseDiffTableCells(trimmed)
	for len(cells) < cols {
		cells = append(cells, "")
	}
	// Cells past the header's count are dropped, as in the note.
	if cols > 0 && len(cells) > cols {
		cells = cells[:cols]
	}
	return ui.Row(c).Grow(1).MinWidth(0).Children(func() {
		for ci, cell := range cells {
			ui.Box(c).Grow(1).Shrink(1).Basis(0).MinWidth(0).Padding(theme.Space4, theme.Space8).
				BorderWidth(0, boolBorder(ci+1 < len(cells)), 0, 0).BorderColor(divider).
				TextAlign(diffCellAlign(st.Aligns, ci)).
				Children(func() {
					DiffMarkdownCell(c, cell, st, header)
				})
		}
	})
}

func diffCellAlign(al []ui.Align, ci int) ui.Align {
	if ci < len(al) {
		return al[ci]
	}
	return ui.Start
}

func boolBorder(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
