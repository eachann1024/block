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

// AppMarkOptions configures the titlebar brand button.
type AppMarkOptions struct {
	Label       string
	Tooltip     string
	TooltipSpec *TooltipSpec
	Active      bool // brand menu is open
	UpdateDot   bool // an application update is waiting
}

// AppMarkImage draws the background-free cat glyph at size×size.
func AppMarkImage(c *ui.Context, size float32) {
	if img := loadCatMark(theme.Ocean().Dark); img != nil {
		ui.Image(c, img).Size(size, size).Shrink(0)
	}
}

// AppMarkButtonWidth is the reserved width of the "cooool.top" titlebar brand button
// when computing available titlebar width in collapsed-sidebar layouts.
const AppMarkButtonWidth float32 = 74

// AppMarkButton renders the "cooool.top" titlebar brand button; clicking it opens
// the brand menu (version, updates, website, plugin store, about).
func AppMarkButton(c *ui.Context, o AppMarkOptions) ui.Element {
	pal := theme.Ocean()
	const id = "app-brand-mark"
	c.Key(id)
	a11yLabel := strings.TrimSpace(o.Label)
	if a11yLabel == "" {
		a11yLabel = "cooool.top"
	}
	btn := ui.ButtonBase(c).Height(theme.ChromeBoxSize).PaddingX(theme.Space4).Shrink(0).Center().
		Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(a11yLabel)
	if !o.Active {
		if o.TooltipSpec != nil {
			RichTooltip(c, btn, *o.TooltipSpec)
		} else {
			Tooltip(c, btn, o.Tooltip)
		}
	}
	hovered, pressed := btn.Hovered(), btn.Pressed()
	bg, border, hasBorder := ui.Transparent, ui.Transparent, o.Active
	switch {
	case o.Active:
		bg = pal.Selected
		border = pal.SelectedBorder
	case pressed || hovered:
		bg = pal.ShellChrome().Hover
	}
	AnimateSurfaceColors(btn, bg, border, hasBorder)
	TactileButton(btn, hovered, pressed, false)
	btn.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, "cooool").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()
			ui.Text(c, ".top").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				FontWeight(theme.WeightRegular).TextColor(pal.Text.Alpha(0.55)).SingleLine()
		})
	})
	if o.UpdateDot {
		dot, size := pal.UpdaterColors().BadgeDot, theme.UpdaterBadgeDotSize
		// DrawOver replaces the smoke probe's, so record its bounds here too.
		btn.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			RecordBounds(id, r)
			pt.Fill(ui.Rect{X: r.X + r.W - size - 2, Y: r.Y + 2, W: size, H: size}, dot, size/2)
		})
	} else {
		Probe(btn, id)
	}
	return btn
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
