package ui

import (
	"strings"
	"sync"
	"unicode/utf8"

	"block/assets"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

var (
	catMarkOnce sync.Once
	catMarkImg  [2]ui.ImageSource // [light, dark]
)

func loadCatMark(dark bool) ui.ImageSource {
	catMarkOnce.Do(func() {
		for i, name := range []string{"app-icon/cat-mark.png", "app-icon/cat-mark-dark.png"} {
			if data, err := assets.Read(name); err == nil && len(data) > 0 {
				if img, decErr := ui.DecodeImage(data); decErr == nil {
					catMarkImg[i] = img
				}
			}
		}
	})
	if dark {
		return catMarkImg[1]
	}
	return catMarkImg[0]
}

// AppMark renders the background-free cat glyph beside a regular-weight "Block" wordmark.
// The glyph is lifted 1px so its optical center meets the wordmark's cap-height center.
func AppMark(c *ui.Context) ui.Element {
	pal := theme.Ocean()
	c.Key("app-brand-mark")
	row := ui.Row(c).Height(theme.HeightCompact).PaddingX(theme.Space4).Gap(6).AlignItems(ui.Center).Shrink(0)
	Probe(row, "app-brand-mark")
	row.Children(func() {
		if img := loadCatMark(pal.Dark); img != nil {
			ui.Image(c, img).Size(16, 16).Shrink(0).Translate(0, -1)
		}
		ui.Text(c, "Block").FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			FontWeight(theme.WeightRegular).TextColor(pal.Text).SingleLine().Shrink(0)
	})
	return row
}

// MonogramBadge renders an 18x18 rounded semantic badge showing the first rune of title
// (used for notebook identity in the titlebar breadcrumb and sidebar footer switcher).
func MonogramBadge(c *ui.Context, title string) ui.Element {
	pal := theme.Ocean()
	chrome := pal.ShellChrome()
	glyph := "B"
	trimmed := strings.TrimSpace(title)
	if trimmed != "" {
		r, _ := utf8.DecodeRuneInString(trimmed)
		if r != utf8.RuneError {
			glyph = strings.ToUpper(string(r))
		}
	}
	badge := ui.Box(c).Size(18, 18).Shrink(0).Radius(theme.RadiusBadge).Center().Background(pal.IconFill)
	badge.Children(func() {
		ui.Text(c, glyph).FontSize(theme.FontMicro).FixedLineHeight(18).FontWeight(theme.WeightTitle).
			TextColor(chrome.BadgeFG).SingleLine()
	})
	return badge
}
