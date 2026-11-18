package ui

import (
	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ShortcutLabelStyle configures typography and tint for ShortcutLabel.
type ShortcutLabelStyle struct {
	FontSize   float32
	LineHeight float32
	FontWeight int
	Color      ui.Color
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

func parseShortcutSegments(text string) (segs []shortcutSegment, hasKey bool) {
	textStart := -1
	for idx, r := range text {
		if glyph, ok := keyRuneIcon(r); ok {
			hasKey = true
			if textStart >= 0 {
				segs = append(segs, shortcutSegment{text: text[textStart:idx]})
				textStart = -1
			}
			segs = append(segs, shortcutSegment{key: glyph})
		} else if textStart < 0 {
			textStart = idx
		}
	}
	if textStart >= 0 {
		segs = append(segs, shortcutSegment{text: text[textStart:]})
	}
	return segs, hasKey
}

// ShortcutLabel renders text with embedded keycap glyphs (`⌘`, `⌥`, `↩`, `⇧`, `⌃`, `⌫`, `↑`, `↓`)
// as centered vector icons sharing one vertical center with surrounding text (thinkly-fast ui/shortcut_label.rs).
func ShortcutLabel(c *ui.Context, text string, style ShortcutLabelStyle) ui.Element {
	lineH := style.LineHeight
	if lineH <= 0 {
		lineH = style.FontSize * 1.4
	}
	segs, hasKey := parseShortcutSegments(text)
	if !hasKey {
		t := ui.Text(c, text).FontSize(style.FontSize).FixedLineHeight(lineH).TextColor(style.Color).SingleLine()
		if style.FontWeight > 0 {
			t.FontWeight(style.FontWeight)
		}
		return t
	}
	const keyInkRatio float32 = 15.6 / 24.0
	const keyInkEm float32 = 0.9
	const keyGap float32 = 2.0
	iconSize := style.FontSize * (keyInkEm / keyInkRatio)
	pal := theme.Ocean()

	row := ui.Row(c).Height(lineH).Shrink(0).AlignItems(ui.Center)
	row.Children(func() {
		prevKey := false
		for _, seg := range segs {
			if seg.key != "" {
				if prevKey {
					ui.Box(c).Width(keyGap).Shrink(0)
				}
				prevKey = true
				ic := icons.Draw(c, seg.key, iconSize, pal.MonoIconColors(style.Color))
				ic.Center()
				continue
			}
			prevKey = false
			t := ui.Text(c, seg.text).FontSize(style.FontSize).FixedLineHeight(lineH).TextColor(style.Color).SingleLine().Shrink(0)
			if style.FontWeight > 0 {
				t.FontWeight(style.FontWeight)
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
		Radius(theme.RadiusMicroSM).Background(pal.Shell).Border(1, pal.BtnBorder).
		Children(func() {
			ShortcutLabel(c, label, ShortcutLabelStyle{
				FontSize:   theme.KeycapFontSize,
				LineHeight: 16,
				FontWeight: theme.WeightRegular,
				Color:      pal.TextSecondary,
			})
		})
}

// ShortcutKeycap formats a shortcut spec (e.g. "cmd-k") and renders it as a Keycap.
func ShortcutKeycap(c *ui.Context, spec string) ui.Element {
	return Keycap(c, FormatShortcutDisplay(spec))
}
