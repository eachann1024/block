package ui

import (
	"strings"
	"unicode"

	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ShortcutLabelStyle configures typography and tint for ShortcutLabel.
type ShortcutLabelStyle struct {
	Font       string
	FontSize   float32
	LineHeight float32
	FontWeight int
	Color      ui.Color
	IsShortcut bool
}

type shortcutSegment struct {
	text string
	key  icons.Icon
}

func keyRuneIcon(r rune) (icons.Icon, bool) {
	switch r {
	case '⌘':
		return icons.KeyCommand, true
	case '⌥':
		return icons.KeyOption, true
	case '↩', '↵', '⏎':
		return icons.KeyReturn, true
	case '⇧':
		return icons.KeyShift, true
	case '⌃':
		return icons.KeyControl, true
	case '⌫':
		return icons.KeyBackspace, true
	case '↑':
		return icons.ArrowUp, true
	case '↓':
		return icons.ArrowDown, true
	default:
		return "", false
	}
}

func parseShortcutSegments(text string, isShortcut bool) (segs []shortcutSegment, hasKey bool) {
	runes := []rune(text)
	hasKeyGlyph := false
	for _, r := range runes {
		if _, ok := keyRuneIcon(r); ok {
			hasKeyGlyph = true
			break
		}
	}
	isPlusConnector := func(i int) bool {
		if runes[i] != '+' {
			return false
		}
		if isShortcut || hasKeyGlyph {
			return true
		}
		return i > 0 && i+1 < len(runes) && unicode.IsSpace(runes[i-1]) && unicode.IsSpace(runes[i+1])
	}

	var buf strings.Builder
	flushText := func() {
		if buf.Len() > 0 {
			segs = append(segs, shortcutSegment{text: buf.String()})
			buf.Reset()
		}
	}
	for i, r := range runes {
		if glyph, ok := keyRuneIcon(r); ok {
			hasKey = true
			flushText()
			segs = append(segs, shortcutSegment{key: glyph})
			continue
		}
		if isPlusConnector(i) {
			hasKey = true
			flushText()
			segs = append(segs, shortcutSegment{key: icons.KeyPlus})
			continue
		}
		buf.WriteRune(r)
	}
	flushText()
	if hasKey {
		for i := range segs {
			if segs[i].text != "" {
				segs[i].text = strings.ReplaceAll(segs[i].text, " ", "\u00a0")
			}
		}
	}
	return segs, hasKey
}

func keyGlyphSize(glyph icons.Icon, fontSize float32) float32 {
	const targetInkEm float32 = 0.78
	switch glyph {
	case icons.KeyCommand:
		// key-command spans 16.8 / 24 units (5:5:5 Apple loop proportions).
		return fontSize * (targetInkEm / (16.8 / 24.0))
	case icons.KeyPlus:
		// key-plus spans 16.1 / 24 units; 0.56em matches compact connector proportions.
		return fontSize * (0.56 / (16.1 / 24.0))
	default:
		// key-option, key-shift, key-return, key-backspace, key-control span ~14.5 / 24 units.
		return fontSize * (targetInkEm / (14.5 / 24.0))
	}
}

func needsLatinCapNudge(s string) bool {
	hasAlnum := false
	for _, r := range s {
		if r >= 0x2E80 {
			return false
		}
		switch r {
		case 'g', 'j', 'p', 'q', 'y', 'Q', ',', ';', '(', ')', '[', ']', '{', '}', '|', '_':
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			hasAlnum = true
		}
	}
	return hasAlnum
}

// ShortcutLabel renders text with embedded keycap glyphs (`⌘`, `⌥`, `↩`, `⇧`, `⌃`, `⌫`, `↑`, `↓`, `+`)
// as centered vector icons sharing one vertical center with surrounding text (thinkly-fast ui/shortcut_label.rs).
func ShortcutLabel(c *ui.Context, text string, style ShortcutLabelStyle) ui.Element {
	lineH := style.LineHeight
	if lineH <= 0 {
		lineH = style.FontSize * 1.4
	}
	segs, hasKey := parseShortcutSegments(text, style.IsShortcut)
	if !hasKey {
		t := ui.Text(c, text).FontSize(style.FontSize).FixedLineHeight(lineH).TextColor(style.Color).SingleLine()
		if style.Font != "" {
			t.Font(style.Font)
		}
		if style.FontWeight > 0 {
			t.FontWeight(style.FontWeight)
		}
		if style.IsShortcut && needsLatinCapNudge(text) {
			t.Translate(0, -0.5)
		}
		return t
	}
	pal := theme.Ocean()

	row := ui.Row(c).Height(lineH).Shrink(0).AlignItems(ui.Center)
	row.Children(func() {
		for _, seg := range segs {
			if seg.key != "" {
				iconSize := keyGlyphSize(seg.key, style.FontSize)
				ic := icons.Draw(c, seg.key, iconSize, pal.MonoIconColors(style.Color))
				ic.Center()
				continue
			}
			t := ui.Text(c, seg.text).FontSize(style.FontSize).FixedLineHeight(lineH).TextColor(style.Color).SingleLine().Shrink(0)
			if style.Font != "" {
				t.Font(style.Font)
			}
			if style.FontWeight > 0 {
				t.FontWeight(style.FontWeight)
			}
			if style.IsShortcut && needsLatinCapNudge(seg.text) {
				t.Translate(0, -0.5)
			}
		}
	})
	return row
}

// Keycap renders a shortcut chip (thinkly-fast ui/shortcut_label.rs `kbd`):
// 18px tall, 5px side padding, 4px radius, shell fill with a btn-border ring
// and 11px secondary text. Use it for every inline shortcut hint.
func Keycap(c *ui.Context, label string) ui.Element {
	pal := theme.Ocean()
	return ui.Row(c).Height(theme.KeycapHeight).Padding(0, theme.KeycapPadX, 0, theme.KeycapPadX).
		Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusXS).Background(pal.Shell).Border(1, pal.BtnBorder).
		Children(func() {
			ShortcutLabel(c, label, ShortcutLabelStyle{
				FontSize:   theme.KeycapFontSize,
				LineHeight: theme.LineMeta,
				FontWeight: theme.WeightRegular,
				Color:      pal.TextSecondary,
				IsShortcut: true,
			})
		})
}

// ShortcutKeycap formats a shortcut spec (e.g. "cmd-k") and renders it as a Keycap.
func ShortcutKeycap(c *ui.Context, spec string) ui.Element {
	return Keycap(c, FormatShortcutDisplay(spec))
}
