package markdown

import (
	_ "image/gif"
	_ "image/jpeg"
	"math"

	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

// TitleMark describes how the document title is separated from the body.
type TitleMark uint8

const (
	TitleMarkRule TitleMark = iota
	TitleMarkBar
	TitleMarkWavy
	TitleMarkHighlight
	TitleMarkDoubleRule
	TitleMarkBand
	TitleMarkCursor
)

// CardSpec describes optional floating card geometry on a paper theme.
type CardSpec struct {
	Inset     float32
	Radius    float32
	Border    ui.Color
	HasBorder bool
	Shadow    ui.Color
}

// PaperThemeSpec defines the complete color palette and page geometry for a paper export theme.
type PaperThemeSpec struct {
	Theme         model.ExportTheme
	Dark          bool
	Canvas        ui.Color
	InnerCanvas   ui.Color
	Text          ui.Color
	Title         ui.Color
	Secondary     ui.Color
	Accent        ui.Color
	Accent2       ui.Color
	CodeBG        ui.Color
	CodeBorder    ui.Color
	QuoteBG       ui.Color
	QuoteFG       ui.Color
	QuoteBar      ui.Color
	TableBorder   ui.Color
	Divider       ui.Color
	Swatch        [3]ui.Color // [paper, accent, inner_card]
	Card          *CardSpec
	PadTop        float32
	PadRight      float32
	PadBottom     float32
	PadLeft       float32
	BodySize      float32
	Leading       float32
	TitleSize     float32
	HeadSizes     [3]float32
	Mark          TitleMark
	TitleFamily   string
	HeadingFamily string
	BodyFamily    string
	TitleBold     bool
}

const (
	fontPing = "PingFang SC"
	fontSong = "Songti SC"
	fontKai  = "Kaiti SC"
	fontMono = "Menlo, monospace"
)

func rgbHex(v uint32) ui.Color {
	return ui.RGB(uint8(v>>16), uint8(v>>8), uint8(v))
}

func rgbaBytes(r, g, b, a uint8) ui.Color {
	return ui.Color{R: r, G: g, B: b, A: a}
}

// NormalizeExportTheme resolves t to one of the 12 supported ExportTheme values (defaulting to Mono).
func NormalizeExportTheme(t model.ExportTheme) model.ExportTheme {
	switch t {
	case model.ExportThemeMono,
		model.ExportThemeJournal,
		model.ExportThemeRiso,
		model.ExportThemePress,
		model.ExportThemePoster,
		model.ExportThemeMist,
		model.ExportThemeInk,
		model.ExportThemeKraft,
		model.ExportThemeNeon,
		model.ExportThemeFilm,
		model.ExportThemeNight,
		model.ExportThemeTerminal:
		return t
	default:
		return model.ExportThemeMono
	}
}

// PaperTheme returns the 1:1 paper theme specification for any of the 12 ExportTheme styles.
func PaperTheme(t model.ExportTheme) PaperThemeSpec {
	t = NormalizeExportTheme(t)
	open := func(dark bool, mark TitleMark, titleSize float32, titleFam, bodyFam string) PaperThemeSpec {
		return PaperThemeSpec{
			Theme:         t,
			Dark:          dark,
			PadTop:        72,
			PadRight:      72,
			PadBottom:     64,
			PadLeft:       72,
			BodySize:      17,
			Leading:       1.75,
			TitleSize:     titleSize,
			HeadSizes:     [3]float32{28, 22, 19},
			Mark:          mark,
			TitleFamily:   titleFam,
			HeadingFamily: titleFam,
			BodyFamily:    bodyFam,
			TitleBold:     true,
		}
	}
	journal := func(dark bool, border ui.Color) PaperThemeSpec {
		return PaperThemeSpec{
			Theme: t,
			Dark:  dark,
			Card: &CardSpec{
				Inset:     60,
				Radius:    14,
				Border:    border,
				HasBorder: true,
				Shadow:    rgbaBytes(90, 60, 10, 16),
			},
			PadTop:        54,
			PadRight:      44,
			PadBottom:     44,
			PadLeft:       44,
			BodySize:      18,
			Leading:       1.85,
			TitleSize:     34,
			HeadSizes:     [3]float32{30, 25, 21},
			Mark:          TitleMarkWavy,
			TitleFamily:   fontKai,
			HeadingFamily: fontKai,
			BodyFamily:    fontPing,
			TitleBold:     false,
		}
	}
	floating := func(dark bool, border *ui.Color, shadow ui.Color) PaperThemeSpec {
		card := &CardSpec{
			Inset:  48,
			Radius: 20,
			Shadow: shadow,
		}
		if border != nil {
			card.Border = *border
			card.HasBorder = true
		}
		return PaperThemeSpec{
			Theme:         t,
			Dark:          dark,
			Card:          card,
			PadTop:        56,
			PadRight:      56,
			PadBottom:     52,
			PadLeft:       56,
			BodySize:      17,
			Leading:       1.75,
			TitleSize:     38,
			HeadSizes:     [3]float32{27, 21, 18.5},
			Mark:          TitleMarkBar,
			TitleFamily:   fontPing,
			HeadingFamily: fontPing,
			BodyFamily:    fontPing,
			TitleBold:     true,
		}
	}

	var s PaperThemeSpec
	switch t {
	case model.ExportThemeJournal:
		s = journal(false, ui.RGB(240, 232, 208))
		s.Canvas, s.InnerCanvas = rgbHex(0xfef9e7), rgbHex(0xfffef5)
		s.Text, s.Title, s.Secondary = rgbHex(0x1f1e1d), rgbHex(0x1f1e1d), rgbHex(0x5e5d59)
		s.Accent, s.Accent2 = rgbHex(0xd97706), rgbaBytes(252, 196, 57, 140)
		s.CodeBG, s.CodeBorder = rgbHex(0xfaf5e6), rgbHex(0xe8dcc8)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = ui.Transparent, rgbHex(0x5e5d59), rgbHex(0xe8dcc8)
		s.TableBorder, s.Divider = rgbHex(0xe8dcc8), rgbHex(0xe8dcc8)
		s.Swatch = [3]ui.Color{rgbHex(0xfef9e7), rgbHex(0xd97706), rgbHex(0xfffef5)}

	case model.ExportThemeRiso:
		s = open(false, TitleMarkHighlight, 44, fontPing, fontPing)
		s.Canvas, s.InnerCanvas = rgbHex(0xf4efe4), rgbHex(0xf4efe4)
		s.Text, s.Title, s.Secondary = rgbHex(0x1f2a44), rgbHex(0x1d3fa8), rgbHex(0x4a5470)
		s.Accent, s.Accent2 = rgbHex(0xff5aa8), rgbHex(0x1d3fa8)
		s.CodeBG, s.CodeBorder = rgbHex(0xfbf7ee), rgbHex(0x1d3fa8)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = rgbHex(0xffe4f0), rgbHex(0x1f2a44), rgbHex(0xff5aa8)
		s.TableBorder, s.Divider = rgbHex(0x9aa6cc), rgbHex(0x1d3fa8)
		s.Swatch = [3]ui.Color{rgbHex(0xf4efe4), rgbHex(0xff5aa8), rgbHex(0xf4efe4)}

	case model.ExportThemePress:
		s = open(false, TitleMarkDoubleRule, 46, fontSong, fontSong)
		s.PadTop, s.PadBottom = 64, 60
		s.BodySize, s.Leading = 17.5, 1.8
		s.Canvas, s.InnerCanvas = rgbHex(0xf1ead9), rgbHex(0xf1ead9)
		s.Text, s.Title, s.Secondary = rgbHex(0x23201b), rgbHex(0x1a1814), rgbHex(0x5a5244)
		s.Accent, s.Accent2 = rgbHex(0x9c2b1f), rgbHex(0x23201b)
		s.CodeBG, s.CodeBorder = rgbHex(0xe8dfcb), rgbHex(0xb9ab8d)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = ui.Transparent, rgbHex(0x23201b), rgbHex(0x23201b)
		s.TableBorder, s.Divider = rgbHex(0x8c826e), rgbHex(0x23201b)
		s.Swatch = [3]ui.Color{rgbHex(0xf1ead9), rgbHex(0x9c2b1f), rgbHex(0xf1ead9)}

	case model.ExportThemePoster:
		s = open(false, TitleMarkBand, 50, fontPing, fontPing)
		s.PadTop = 56
		s.Canvas, s.InnerCanvas = rgbHex(0xffffff), rgbHex(0xffffff)
		s.Text, s.Title, s.Secondary = rgbHex(0x1c1c1c), rgbHex(0x1c1c1c), rgbHex(0x5c5c5c)
		s.Accent, s.Accent2 = rgbHex(0xd8401a), rgbHex(0xffffff)
		s.CodeBG, s.CodeBorder = rgbHex(0xf5f3f1), rgbHex(0xebe6e1)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = rgbHex(0xfff1ea), rgbHex(0x1c1c1c), rgbHex(0xd8401a)
		s.TableBorder, s.Divider = rgbHex(0xe0dcd8), rgbHex(0xe8e4e0)
		s.Swatch = [3]ui.Color{rgbHex(0xffffff), rgbHex(0xe2471b), rgbHex(0xffffff)}

	case model.ExportThemeMist:
		s = floating(false, nil, rgbaBytes(30, 50, 90, 22))
		s.Canvas, s.InnerCanvas = rgbHex(0xe9edf2), rgbHex(0xffffff)
		s.Text, s.Title, s.Secondary = rgbHex(0x1e293b), rgbHex(0x0f172a), rgbHex(0x55627a)
		s.Accent, s.Accent2 = rgbHex(0x3b6cf6), rgbHex(0x3b6cf6)
		s.CodeBG, s.CodeBorder = rgbHex(0xf3f5f9), rgbHex(0xe3e8ef)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = rgbHex(0xf2f6fd), rgbHex(0x1e293b), rgbHex(0x3b6cf6)
		s.TableBorder, s.Divider = rgbHex(0xdfe5ee), rgbHex(0xe3e8ef)
		s.Swatch = [3]ui.Color{rgbHex(0xe9edf2), rgbHex(0x3b6cf6), rgbHex(0xffffff)}

	case model.ExportThemeInk:
		s = open(true, TitleMarkRule, 42, fontPing, fontPing)
		s.Canvas, s.InnerCanvas = rgbHex(0x121212), rgbHex(0x121212)
		s.Text, s.Title, s.Secondary = rgbHex(0xe6e6e6), rgbHex(0xffffff), rgbHex(0xa3a3a3)
		s.Accent, s.Accent2 = rgbHex(0xededed), rgbHex(0x3a3a3a)
		s.CodeBG, s.CodeBorder = rgbHex(0x1e1e1e), rgbHex(0x2e2e2e)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = ui.Transparent, rgbHex(0xf5f5f5), rgbHex(0xededed)
		s.TableBorder, s.Divider = rgbHex(0x3a3a3a), rgbHex(0x3a3a3a)
		s.Swatch = [3]ui.Color{rgbHex(0x121212), rgbHex(0xededed), rgbHex(0x121212)}

	case model.ExportThemeKraft:
		s = journal(true, ui.RGB(58, 53, 45))
		s.Canvas, s.InnerCanvas = rgbHex(0x1c1a17), rgbHex(0x27241f)
		s.Text, s.Title, s.Secondary = rgbHex(0xece3cf), rgbHex(0xfbf3df), rgbHex(0xb3a78c)
		s.Accent, s.Accent2 = rgbHex(0xf0c35a), rgbaBytes(240, 195, 90, 120)
		s.CodeBG, s.CodeBorder = rgbHex(0x1f1d19), rgbHex(0x4a4538)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = ui.Transparent, rgbHex(0xc9bea4), rgbHex(0x5c5446)
		s.TableBorder, s.Divider = rgbHex(0x4a4538), rgbHex(0x3d382f)
		s.Swatch = [3]ui.Color{rgbHex(0x1c1a17), rgbHex(0xf0c35a), rgbHex(0x27241f)}

	case model.ExportThemeNeon:
		s = open(true, TitleMarkBar, 44, fontPing, fontPing)
		s.Canvas, s.InnerCanvas = rgbHex(0x14152b), rgbHex(0x14152b)
		s.Text, s.Title, s.Secondary = rgbHex(0xecebff), rgbHex(0xffffff), rgbHex(0xa9a8d0)
		s.Accent, s.Accent2 = rgbHex(0x3fe0d0), rgbHex(0xff6a3d)
		s.CodeBG, s.CodeBorder = rgbHex(0x0f1024), rgbHex(0x2e3170)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = rgbHex(0x1d1f3d), rgbHex(0xecebff), rgbHex(0x3fe0d0)
		s.TableBorder, s.Divider = rgbHex(0x34376e), rgbHex(0x34376e)
		s.Swatch = [3]ui.Color{rgbHex(0x14152b), rgbHex(0x3fe0d0), rgbHex(0x14152b)}

	case model.ExportThemeFilm:
		s = open(true, TitleMarkBar, 42, fontSong, fontPing)
		s.Card = &CardSpec{Inset: 40, Radius: 10}
		s.PadTop, s.PadRight, s.PadBottom, s.PadLeft = 56, 56, 52, 56
		s.Leading = 1.8
		s.TitleBold = false
		s.HeadingFamily = fontPing
		s.Canvas, s.InnerCanvas = rgbHex(0x141210), rgbHex(0x0b0a09)
		s.Text, s.Title, s.Secondary = rgbHex(0xe8e0d0), rgbHex(0xfbf5e8), rgbHex(0xa89c86)
		s.Accent, s.Accent2 = rgbHex(0xf2a33a), rgbHex(0xf2a33a)
		s.CodeBG, s.CodeBorder = rgbHex(0x1f1c18), rgbHex(0x302b24)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = ui.Transparent, rgbHex(0xfbf5e8), rgbHex(0xf2a33a)
		s.TableBorder, s.Divider = rgbHex(0x403a30), rgbHex(0x302b24)
		s.Swatch = [3]ui.Color{rgbHex(0x141210), rgbHex(0xf2a33a), rgbHex(0x0b0a09)}

	case model.ExportThemeNight:
		border := ui.RGB(38, 44, 54)
		s = floating(true, &border, ui.Transparent)
		s.Canvas, s.InnerCanvas = rgbHex(0x0d1117), rgbHex(0x161b22)
		s.Text, s.Title, s.Secondary = rgbHex(0xd1d7e0), rgbHex(0xf0f3f6), rgbHex(0x8b949e)
		s.Accent, s.Accent2 = rgbHex(0x7aa2f7), rgbHex(0x7aa2f7)
		s.CodeBG, s.CodeBorder = rgbHex(0x0f1318), rgbHex(0x262c36)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = rgbHex(0x1b2230), rgbHex(0xd1d7e0), rgbHex(0x7aa2f7)
		s.TableBorder, s.Divider = rgbHex(0x30363d), rgbHex(0x262c36)
		s.Swatch = [3]ui.Color{rgbHex(0x0d1117), rgbHex(0x7aa2f7), rgbHex(0x161b22)}

	case model.ExportThemeTerminal:
		s = open(true, TitleMarkCursor, 36, fontPing, fontPing)
		s.TitleFamily = fontMono
		s.Canvas, s.InnerCanvas = rgbHex(0x0c0f0c), rgbHex(0x0c0f0c)
		s.Text, s.Title, s.Secondary = rgbHex(0xd6e4d6), rgbHex(0x8ee08a), rgbHex(0x7f957f)
		s.Accent, s.Accent2 = rgbHex(0x7ee787), rgbHex(0x7ee787)
		s.CodeBG, s.CodeBorder = rgbHex(0x121812), rgbHex(0x243024)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = ui.Transparent, rgbHex(0xb8ccb8), rgbHex(0x3f6b3f)
		s.TableBorder, s.Divider = rgbHex(0x2a3a2a), rgbHex(0x2a3a2a)
		s.Swatch = [3]ui.Color{rgbHex(0x0c0f0c), rgbHex(0x7ee787), rgbHex(0x0c0f0c)}

	default: // model.ExportThemeMono
		s = open(false, TitleMarkRule, 42, fontPing, fontPing)
		s.Canvas, s.InnerCanvas = rgbHex(0xffffff), rgbHex(0xffffff)
		s.Text, s.Title, s.Secondary = rgbHex(0x1f1e1d), rgbHex(0x111111), rgbHex(0x5e5d59)
		s.Accent, s.Accent2 = rgbHex(0x111111), rgbHex(0x111111)
		s.CodeBG, s.CodeBorder = rgbHex(0xf4f4f2), rgbHex(0xe4e4e0)
		s.QuoteBG, s.QuoteFG, s.QuoteBar = ui.Transparent, rgbHex(0x1f1e1d), rgbHex(0x111111)
		s.TableBorder, s.Divider = rgbHex(0xd6d6d1), rgbHex(0xdcdcd7)
		s.Swatch = [3]ui.Color{rgbHex(0xffffff), rgbHex(0x111111), rgbHex(0xffffff)}
	}
	return s
}

func (s PaperThemeSpec) Margins() (top, right, bottom, left float32) {
	var inset float32
	if s.Card != nil {
		inset = s.Card.Inset
	}
	return s.PadTop + inset, s.PadRight + inset, s.PadBottom + inset, s.PadLeft + inset
}

func (s PaperThemeSpec) BlockRadius() float32 {
	switch s.Theme {
	case model.ExportThemeJournal, model.ExportThemeKraft, model.ExportThemeMist, model.ExportThemeNight:
		return 10
	case model.ExportThemePress, model.ExportThemeRiso, model.ExportThemeTerminal:
		return 2
	default:
		return 6
	}
}

func (s PaperThemeSpec) UsesSquareBullets() bool {
	return s.Theme == model.ExportThemePoster || s.Theme == model.ExportThemeTerminal || s.Theme == model.ExportThemeRiso
}

// LinkFG returns Accent when WCAG AA contrast against InnerCanvas is >= 4.5, otherwise Title.
func (s PaperThemeSpec) LinkFG() ui.Color {
	lum := func(c ui.Color) float64 {
		lin := func(v uint8) float64 {
			x := float64(v) / 255.0
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
	}
	a, b := lum(s.Accent), lum(s.InnerCanvas)
	if b > a {
		a, b = b, a
	}
	if (a+0.05)/(b+0.05) >= 4.5 {
		return s.Accent
	}
	return s.Title
}

func (s PaperThemeSpec) TableHeaderBG() ui.Color {
	t := float32(0.08)
	if s.Dark {
		t = 0.16
	}
	return mixUIColor(s.InnerCanvas, s.Accent, t)
}

func (s PaperThemeSpec) HighlightBG() ui.Color {
	t := float32(0.22)
	if s.Dark {
		t = 0.30
	}
	return mixUIColor(s.InnerCanvas, s.Accent, t)
}

func (s PaperThemeSpec) FG(c ui.Color) ui.Color {
	if !s.Dark {
		return c
	}
	luma := (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255.0
	if luma >= 0.5 {
		return c
	}
	t := (0.5-luma)/0.5*0.7 + 0.2
	return mixUIColor(c, ui.RGB(255, 255, 255), t)
}

func (s PaperThemeSpec) BG(c ui.Color) ui.Color {
	if !s.Dark || c.A == 0 {
		return c
	}
	mixed := mixUIColor(c, s.InnerCanvas, 0.78)
	return ui.Color{R: mixed.R, G: mixed.G, B: mixed.B, A: c.A}
}

func (s PaperThemeSpec) CalloutColors(kind model.CalloutKind) (bg, border, accent ui.Color, title string) {
	switch kind {
	case model.CalloutTip:
		if s.Dark {
			accent = theme.OceanDark.Success
		} else {
			accent = theme.OceanLight.Success
		}
		title = "TIP"
	case model.CalloutImportant:
		if s.Dark {
			accent = ui.Hex("#a78bfa")
		} else {
			accent = ui.Hex("#7c3aed")
		}
		title = "IMPORTANT"
	case model.CalloutWarning:
		if s.Dark {
			accent = theme.OceanDark.Warning
		} else {
			accent = theme.OceanLight.Warning
		}
		title = "WARNING"
	case model.CalloutCaution:
		if s.Dark {
			accent = theme.OceanDark.Danger
		} else {
			accent = theme.OceanLight.Danger
		}
		title = "CAUTION"
	default:
		accent = s.LinkFG()
		title = "NOTE"
	}
	bgMix := float32(0.07)
	if s.Dark {
		bgMix = 0.12
	}
	bg = mixUIColor(s.InnerCanvas, accent, bgMix)
	border = mixUIColor(s.InnerCanvas, accent, 0.35)
	return bg, border, accent, title
}

func legacyOceanPaperSpec(pal theme.Palette) PaperThemeSpec {
	return PaperThemeSpec{
		Theme:         model.ExportThemeMono,
		Dark:          pal.Dark,
		Canvas:        pal.Shell,
		InnerCanvas:   pal.Paper,
		Text:          pal.Text,
		Title:         pal.Text,
		Secondary:     pal.TextSecondary,
		Accent:        pal.Accent,
		Accent2:       pal.AccentText,
		CodeBG:        pal.CodeBG,
		CodeBorder:    pal.Hairline,
		QuoteBG:       pal.Hover,
		QuoteFG:       pal.TextSecondary,
		QuoteBar:      pal.QuoteBar,
		TableBorder:   pal.Hairline,
		Divider:       pal.Hairline,
		Swatch:        [3]ui.Color{pal.Shell, pal.Accent, pal.Paper},
		Card:          &CardSpec{Inset: 24, Radius: 14, Border: pal.Hairline, HasBorder: true},
		PadTop:        48,
		PadRight:      56,
		PadBottom:     48,
		PadLeft:       56,
		BodySize:      16,
		Leading:       1.75,
		TitleSize:     32,
		HeadSizes:     [3]float32{28, 22, 19},
		Mark:          TitleMarkRule,
		TitleFamily:   fontPing,
		HeadingFamily: fontPing,
		BodyFamily:    fontPing,
		TitleBold:     true,
	}
}
