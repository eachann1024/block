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
	appIconOnce sync.Once
	appIconImg  ui.ImageSource
)

func loadAppIconImage() ui.ImageSource {
	appIconOnce.Do(func() {
		if data, err := assets.Read("app-icon/apple-touch-icon.png"); err == nil && len(data) > 0 {
			if img, decErr := ui.DecodeImage(data); decErr == nil {
				appIconImg = img
			}
		}
	})
	return appIconImg
}

// AppMark renders the 18x18 rounded Block application icon paired with the "Block" wordmark.
func AppMark(c *ui.Context) ui.Element {
	pal := theme.Ocean()
	c.Key("app-brand-mark")
	row := ui.Row(c).Height(theme.HeightCompact).PaddingX(theme.Space4).Gap(7).AlignItems(ui.Center).Shrink(0)
	Probe(row, "app-brand-mark")
	row.Children(func() {
		iconWrap := ui.Box(c).Size(18, 18).Shrink(0).Radius(theme.RadiusBadge).Clip().
			Border(1, pal.Hairline.Alpha(0.55))
		iconWrap.Children(func() {
			if img := loadAppIconImage(); img != nil {
				ui.Image(c, img).Size(18, 18)
			} else {
				ui.Box(c).Fill().Background(pal.IconFill)
			}
		})
		ui.Text(c, "Block").FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine().Shrink(0)
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
