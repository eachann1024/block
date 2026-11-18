package markdown

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	defaultExportWidth = 816
	// ExportScale is the fixed raster density of PNG/PDF exports (device px per logical px).
	ExportScale        = 4.0
	exportScale        = ExportScale
	maxPDFPageHeightPt = 1152
	watermarkLabel     = "Block"
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

func parseExportThemeName(raw string) (model.ExportTheme, bool) {
	v := model.ExportTheme(strings.ToLower(strings.TrimSpace(raw)))
	switch v {
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
		return v, true
	default:
		return "", false
	}
}

func resolveExportPaper(prefs model.ExportPreferences) (PaperThemeSpec, bool) {
	if prefs.Style != "" {
		return PaperTheme(prefs.Style), false
	}
	if parsed, ok := parseExportThemeName(prefs.Theme); ok {
		return PaperTheme(parsed), false
	}
	switch strings.ToLower(strings.TrimSpace(prefs.Theme)) {
	case "light":
		return legacyOceanPaperSpec(theme.OceanLight), true
	case "dark":
		return legacyOceanPaperSpec(theme.OceanDark), true
	case "current":
		return legacyOceanPaperSpec(theme.Ocean()), true
	default:
		return PaperTheme(model.ExportThemeMono), false
	}
}

func resolveShowTitle(prefs model.ExportPreferences) bool {
	if prefs.ShowTitle != nil {
		return *prefs.ShowTitle
	}
	return true
}

func resolveWatermark(prefs model.ExportPreferences) bool {
	if prefs.Watermark != nil {
		return *prefs.Watermark
	}
	return true
}

func resolveTitleText(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	if t := strings.TrimSpace(prefs.Title); t != "" {
		return t
	}
	return resolveDocTitle(doc, notePath)
}

func exportBodyBlocks(blocks []model.Block, title string, showTitle bool) []model.Block {
	if len(blocks) > 0 && blocks[0].Kind == model.BlockCode && blocks[0].Language == "yaml-frontmatter" {
		blocks = blocks[1:]
	}
	if !showTitle || len(blocks) == 0 {
		return blocks
	}
	trimmedTitle := strings.Join(strings.Fields(title), " ")
	if trimmedTitle == "" {
		return blocks
	}
	first := &blocks[0]
	if first.Kind == model.BlockHeading1 {
		firstText := strings.Join(strings.Fields(first.PlainText()), " ")
		if firstText == trimmedTitle {
			return blocks[1:]
		}
	}
	return blocks
}

// ExportDocument renders doc into the requested format bytes.
func ExportDocument(doc *model.Doc, notePath string, format model.ExportFormat, prefs model.ExportPreferences) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("cannot export nil document")
	}
	if format == "" {
		format = prefs.Format
	}
	if format == "" {
		format = model.ExportMarkdown
	}
	switch format {
	case model.ExportMarkdown:
		return []byte(exportMarkdown(doc, notePath, prefs)), nil
	case model.ExportHTML:
		return []byte(exportHTML(doc, notePath, prefs)), nil
	case model.ExportPNG:
		return exportPNG(doc, notePath, prefs)
	case model.ExportPDF:
		return exportPDF(doc, notePath, prefs)
	case model.ExportDOCX:
		return exportDOCX(doc, notePath, prefs)
	case model.ExportZIP:
		return exportZIP(doc, notePath, prefs)
	default:
		return nil, fmt.Errorf("unsupported export format: %q", format)
	}
}

// ExportToFile exports doc and atomically writes the resulting bytes to outPath.
func ExportToFile(doc *model.Doc, notePath, outPath string, format model.ExportFormat, prefs model.ExportPreferences) error {
	if strings.TrimSpace(outPath) == "" {
		return errors.New("output path cannot be empty")
	}
	data, err := ExportDocument(doc, notePath, format, prefs)
	if err != nil {
		return err
	}
	dir := filepath.Dir(outPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".block-export-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func resolveExportWidth(prefs model.ExportPreferences) int {
	if prefs.Width >= 360 && prefs.Width <= 2400 {
		return prefs.Width
	}
	return defaultExportWidth
}

func resolveDocTitle(doc *model.Doc, notePath string) string {
	if t := strings.TrimSpace(doc.Title()); t != "" {
		return t
	}
	if base := strings.TrimSpace(filepath.Base(notePath)); base != "" && base != "." && base != string(filepath.Separator) {
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem != "" {
			return stem
		}
	}
	return theme.Tr("未命名", "Untitled")
}

func uiColorCSS(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float32(c.A)/255.0)
}

func mixUIColor(a, b ui.Color, t float32) ui.Color {
	lerp := func(x, y uint8) uint8 {
		v := float32(x) + (float32(y)-float32(x))*t
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	return ui.RGB(lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B))
}

func resolveSpanColor(raw string, bg bool, pal PaperThemeSpec) (ui.Color, bool) {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" || v == "default" || v == "none" || v == "transparent" {
		return ui.Transparent, false
	}
	if bg {
		if c, ok := theme.ResolveNamedBG(v, pal.Dark); ok {
			return c, true
		}
	} else {
		if c, ok := theme.ResolveNamedFG(v, pal.Dark); ok {
			return c, true
		}
	}
	if v == "black" {
		c := ui.RGB(0, 0, 0)
		if !bg {
			c = pal.FG(c)
		} else {
			c = pal.BG(c)
		}
		return c, true
	}
	if v == "white" {
		c := ui.RGB(255, 255, 255)
		if bg {
			c = pal.BG(c)
		}
		return c, true
	}
	var parsed ui.Color
	var ok bool
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 {
			r, e1 := strconv.ParseUint(h[0:1]+h[0:1], 16, 8)
			g, e2 := strconv.ParseUint(h[1:2]+h[1:2], 16, 8)
			b, e3 := strconv.ParseUint(h[2:3]+h[2:3], 16, 8)
			if e1 == nil && e2 == nil && e3 == nil {
				parsed, ok = ui.RGB(uint8(r), uint8(g), uint8(b)), true
			}
		} else if len(h) == 6 || len(h) == 8 {
			r, e1 := strconv.ParseUint(h[0:2], 16, 8)
			g, e2 := strconv.ParseUint(h[2:4], 16, 8)
			b, e3 := strconv.ParseUint(h[4:6], 16, 8)
			a := uint64(255)
			var e4 error
			if len(h) == 8 {
				a, e4 = strconv.ParseUint(h[6:8], 16, 8)
			}
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				parsed, ok = ui.Color{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}, true
			}
		}
	} else if strings.HasPrefix(v, "rgb(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[4:len(v)-1], ",")
		if len(parts) == 3 {
			r, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			if e1 == nil && e2 == nil && e3 == nil {
				parsed, ok = ui.RGB(clampU8(r), clampU8(g), clampU8(b)), true
			}
		}
	} else if strings.HasPrefix(v, "rgba(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[5:len(v)-1], ",")
		if len(parts) == 4 {
			r, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			a, e4 := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				parsed, ok = ui.Color{R: clampU8(r), G: clampU8(g), B: clampU8(b), A: clampU8(int(a*255 + 0.5))}, true
			}
		}
	}
	if !ok {
		return ui.Transparent, false
	}
	if bg {
		return pal.BG(parsed), true
	}
	return pal.FG(parsed), true
}

func clampU8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func resolveLocalImagePath(src, notePath string) (string, bool) {
	s := strings.TrimSpace(src)
	if s == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:") {
		return "", false
	}
	if strings.HasPrefix(lower, "file://") {
		if u, err := url.Parse(s); err == nil && u.Path != "" {
			s = u.Path
		} else {
			s = strings.TrimPrefix(s[7:], "localhost")
		}
	} else if strings.Contains(s, "://") {
		return "", false
	}
	if decoded, err := url.PathUnescape(s); err == nil {
		s = decoded
	}
	p := filepath.FromSlash(s)
	if !filepath.IsAbs(p) {
		if notePath == "" {
			return "", false
		}
		p = filepath.Join(filepath.Dir(notePath), p)
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", false
	}
	return p, true
}

// ---- Markdown Export ----

func exportMarkdown(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	md := SerializeMarkdown(doc)
	showTitle := resolveShowTitle(prefs)
	title := strings.TrimSpace(prefs.Title)
	if title == "" && (prefs.IncludeMeta || (prefs.ShowTitle != nil && *prefs.ShowTitle)) {
		title = resolveDocTitle(doc, notePath)
	}
	if showTitle && title != "" {
		hasFM := len(doc.Blocks) > 0 && doc.Blocks[0].Kind == model.BlockCode && doc.Blocks[0].Language == "yaml-frontmatter"
		duplicated := false
		for i := range doc.Blocks {
			b := &doc.Blocks[i]
			if strings.TrimSpace(b.PlainText()) != "" || b.Table != nil {
				duplicated = b.Kind == model.BlockHeading1 && strings.TrimSpace(b.PlainText()) == title
				break
			}
		}
		if !hasFM && !duplicated {
			return "# " + title + "\n\n" + md
		}
	}
	return md
}

// ---- HTML Export ----

func exportHTML(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	pal, legacy := resolveExportPaper(prefs)
	width := resolveExportWidth(prefs)
	showTitle := resolveShowTitle(prefs)
	watermark := resolveWatermark(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	css := buildHTMLStylesheet(pal, legacy, width, title)

	var out strings.Builder
	out.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\">")
	out.WriteString(fmt.Sprintf("<meta name=\"viewport\" content=\"width=%d\">", width))
	out.WriteString("<title>")
	out.WriteString(escapeHTMLText(title))
	out.WriteString("</title><style>")
	out.WriteString(css)
	out.WriteString("</style></head><body><div class=\"page\"><article class=\"paper\">")

	if showTitle {
		out.WriteString(renderHTMLHeader(pal, legacy, title))
	}

	bodyBlocks := exportBodyBlocks(doc.Blocks, title, showTitle)
	var num listNumbering
	for i := 0; i < len(bodyBlocks); i++ {
		b := &bodyBlocks[i]
		if b.Kind.IsList() {
			start := i
			kind := b.Kind
			for i+1 < len(bodyBlocks) && bodyBlocks[i+1].Kind == kind {
				i++
			}
			renderHTMLList(&out, bodyBlocks[start:i+1], &num, pal)
			continue
		}
		num.next(b)
		renderHTMLBlock(&out, b, notePath, pal)
	}

	if watermark {
		now := time.Now().UTC().Format("2006.01.02")
		out.WriteString(fmt.Sprintf(`<footer class="doc-footer"><span>%s</span><span>%s</span></footer>`, watermarkLabel, now))
	} else {
		out.WriteString(`<div style="height:16px"></div>`)
	}
	out.WriteString("</article></div></body></html>\n")
	return out.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func htmlFontStack(fam string) string {
	switch fam {
	case fontSong:
		return `"Songti SC","Noto Serif SC",serif`
	case fontKai:
		return `"Kaiti SC","STKaiti",cursive,serif`
	case fontMono:
		return `"Menlo","JetBrains Mono",monospace`
	default:
		return `-apple-system,BlinkMacSystemFont,"PingFang SC","Helvetica Neue",sans-serif`
	}
}

func buildHTMLStylesheet(pal PaperThemeSpec, legacy bool, width int, title string) string {
	var inset, radius float32
	borderCSS := "0 0 0 transparent"
	shadowCSS := ""
	if pal.Card != nil {
		inset = pal.Card.Inset
		radius = pal.Card.Radius
		if pal.Card.HasBorder {
			borderCSS = fmt.Sprintf("inset 0 0 0 1px %s", uiColorCSS(pal.Card.Border))
		}
		if pal.Card.Shadow.A > 0 {
			shadowCSS = fmt.Sprintf(",0 8px 14px %s", uiColorCSS(pal.Card.Shadow))
		}
	}
	titleFam := pal.HeadingFamily
	if isASCII(title) {
		titleFam = pal.TitleFamily
	}
	titleWeight := 400
	if pal.TitleBold {
		titleWeight = 700
	}
	linkColor := uiColorCSS(pal.LinkFG())
	blockRadius := pal.BlockRadius()
	quoteCSS := fmt.Sprintf(
		"margin:0 0 16px;padding:12px 20px;border-left:4px solid %s;background:%s;border-radius:%.0fpx;color:%s",
		uiColorCSS(pal.QuoteBar), uiColorCSS(pal.QuoteBG), blockRadius, uiColorCSS(pal.QuoteFG),
	)
	if !legacy && pal.Theme == model.ExportThemePress {
		quoteCSS = fmt.Sprintf(
			"margin:0 0 16px;padding:12px 4px;border-top:1px solid %s;border-bottom:1px solid %s;color:%s",
			uiColorCSS(pal.QuoteBar), uiColorCSS(pal.QuoteBar), uiColorCSS(pal.QuoteFG),
		)
	}
	bulletStyle := ""
	if !legacy && pal.UsesSquareBullets() {
		bulletStyle = "ul{list-style-type:square}"
	}
	h2Rule := ""
	if !legacy && pal.Theme == model.ExportThemePress {
		h2Rule = fmt.Sprintf("h2{padding-bottom:6px;border-bottom:1px solid %s}", uiColorCSS(pal.Divider))
	} else if !legacy && pal.Theme == model.ExportThemePoster {
		h2Rule = fmt.Sprintf("h2{padding-left:12px;border-left:5px solid %s}", uiColorCSS(pal.Accent))
	} else if !legacy && pal.Theme == model.ExportThemeRiso {
		ink := ui.Color{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 80}
		h2Rule = fmt.Sprintf("h2 span.riso-h2{background:linear-gradient(transparent 55%%,%s 55%%,%s 90%%,transparent 90%%);box-decoration-break:clone}", uiColorCSS(ink), uiColorCSS(ink))
	}

	return fmt.Sprintf(`*{box-sizing:border-box}
html,body{margin:0;padding:0;background:%s;color:%s;font-family:%s}
.page{width:%dpx;max-width:100%%;margin:0 auto;padding:%.0fpx;background:%s}
.paper{background:%s;border-radius:%.0fpx;box-shadow:%s%s;padding:%.0fpx %.0fpx %.0fpx %.0fpx;font-size:%.1fpx;line-height:%.2f;color:%s;overflow-wrap:anywhere}
.doc-header{margin-bottom:28px}.doc-title{margin:0;font-family:%s;font-size:%.1fpx;line-height:1.3;font-weight:%d;color:%s}
.doc-footer{display:flex;justify-content:space-between;margin-top:28px;padding-top:16px;border-top:1px solid %s;font-size:14px;line-height:19.6px;color:%s}
h1,h2,h3,h4,h5,h6{margin:18px 0 8px;line-height:1.35;font-family:%s;color:%s;font-weight:700}
h1{font-size:%.1fpx}h2{font-size:%.1fpx}h3{font-size:%.1fpx}h4{font-size:%.1fpx}h5,h6{font-size:%.1fpx;font-weight:500}
%s
p{margin:0 0 10px}
ul,ol{margin:0 0 10px;padding-left:26px}%s ul.todo-list{list-style:none;padding-left:4px}li{margin:4px 0}
blockquote{%s}
.callout{margin:0 0 14px;padding:12px 16px;border-radius:8px;border-left:4px solid}
.callout-title{font-size:14px;font-weight:700;letter-spacing:.04em;margin-bottom:6px}
pre{margin:0 0 16px;padding:14px 18px;background:%s;border:1px solid %s;border-radius:%.0fpx;overflow-x:auto;font-family:%s;font-size:14.5px;line-height:1.6}
code{font-family:%s;font-size:.9em;background:%s;padding:1px 5px;border-radius:4px}
pre code{background:transparent;padding:0}
table{width:100%%;border-collapse:collapse;margin:0 0 14px}
th,td{border:1px solid %s;padding:8px 12px;text-align:left;vertical-align:top}
th{background:%s;color:%s;font-weight:600}
hr{border:0;border-top:1px solid %s;margin:18px 0}
a,table a,th a,td a{color:%s;text-decoration:underline}
img{max-width:100%%;height:auto;border-radius:6px}`,
		uiColorCSS(pal.Canvas), uiColorCSS(pal.Text), htmlFontStack(pal.BodyFamily),
		width, inset, uiColorCSS(pal.Canvas),
		uiColorCSS(pal.InnerCanvas), radius, borderCSS, shadowCSS,
		pal.PadTop, pal.PadRight, pal.PadBottom, pal.PadLeft, pal.BodySize, pal.Leading, uiColorCSS(pal.Text),
		htmlFontStack(titleFam), pal.TitleSize, titleWeight, uiColorCSS(pal.Title),
		uiColorCSS(pal.Divider), uiColorCSS(pal.Secondary),
		htmlFontStack(pal.HeadingFamily), uiColorCSS(pal.Title),
		pal.HeadSizes[0], pal.HeadSizes[1], pal.HeadSizes[2], pal.BodySize*1.12, pal.BodySize,
		h2Rule,
		bulletStyle,
		quoteCSS,
		uiColorCSS(pal.CodeBG), uiColorCSS(pal.CodeBorder), blockRadius, htmlFontStack(fontMono),
		htmlFontStack(fontMono), uiColorCSS(pal.CodeBG),
		uiColorCSS(pal.TableBorder),
		uiColorCSS(pal.TableHeaderBG()), uiColorCSS(pal.Title),
		uiColorCSS(pal.Divider),
		linkColor,
	)
}

func renderHTMLHeader(pal PaperThemeSpec, legacy bool, title string) string {
	escTitle := escapeHTMLText(title)
	if legacy {
		return fmt.Sprintf(`<header class="doc-header"><h1 class="doc-title">%s</h1><div style="height:2px;margin-top:14px;background:%s"></div></header>`,
			escTitle, uiColorCSS(pal.Accent))
	}
	switch pal.Mark {
	case TitleMarkBar:
		return fmt.Sprintf(`<header style="margin-bottom:34px"><h1 class="doc-title">%s</h1><div style="height:4px;width:48px;border-radius:2px;margin-top:18px;background:%s"></div></header>`,
			escTitle, uiColorCSS(pal.Accent))
	case TitleMarkWavy:
		return fmt.Sprintf(`<header style="margin-bottom:30px"><h1 class="doc-title">%s</h1><svg width="150" height="12" style="display:block;margin-top:8px"><path d="M0 6 Q6 0 12 6 T24 6 T36 6 T48 6 T60 6 T72 6 T84 6 T96 6 T108 6 T120 6 T132 6 T144 6 T156 6" fill="none" stroke="%s" stroke-width="2"/></svg></header>`,
			escTitle, uiColorCSS(pal.Accent))
	case TitleMarkDoubleRule:
		return fmt.Sprintf(`<header style="margin-bottom:34px"><h1 class="doc-title" style="text-align:center">%s</h1><div style="height:7px;margin-top:18px;border-top:3px solid %s;border-bottom:1px solid %s"></div></header>`,
			escTitle, uiColorCSS(pal.Divider), uiColorCSS(pal.Divider))
	case TitleMarkHighlight:
		ink := uiColorCSS(ui.Color{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 105})
		return fmt.Sprintf(`<header style="margin-bottom:36px"><h1 class="doc-title"><span style="background:linear-gradient(transparent 50%%,%s 50%%,%s 88%%,transparent 88%%);box-decoration-break:clone">%s</span></h1></header>`,
			ink, ink, escTitle)
	case TitleMarkCursor:
		return fmt.Sprintf(`<header style="margin-bottom:36px"><h1 class="doc-title">%s<span style="display:inline-block;vertical-align:middle;margin-left:10px;width:%.1fpx;height:%.1fpx;background:%s"></span></h1></header>`,
			escTitle, pal.TitleSize*0.55, pal.TitleSize*0.91, uiColorCSS(pal.Accent))
	case TitleMarkBand:
		top, right, _, left := pal.Margins()
		return fmt.Sprintf(`<header style="margin:-%.0fpx -%.0fpx 52px -%.0fpx;padding:%.0fpx %.0fpx 44px %.0fpx;background:%s"><h1 class="doc-title" style="color:%s">%s</h1></header>`,
			top, right, left, top, right, left, uiColorCSS(pal.Accent), uiColorCSS(pal.Accent2), escTitle)
	default: // TitleMarkRule
		return fmt.Sprintf(`<header style="margin-bottom:36px"><h1 class="doc-title">%s</h1><div style="height:2px;margin-top:20px;background:%s"></div></header>`,
			escTitle, uiColorCSS(pal.Title))
	}
}

func renderHTMLSpans(out *strings.Builder, spans []model.Span, pal PaperThemeSpec) {
	linkFG := pal.LinkFG()
	for _, sp := range spans {
		var openTags, closeTags []string
		fgColor, hasFG := resolveSpanColor(sp.FGColor, false, pal)
		if sp.Link != "" {
			linkStyle := uiColorCSS(linkFG)
			if hasFG {
				linkStyle = uiColorCSS(fgColor)
			}
			openTags = append(openTags, fmt.Sprintf(`<a href="%s" style="color:%s;text-decoration:underline">`, escapeAttrValue(sp.Link), linkStyle))
			closeTags = append(closeTags, "</a>")
		}
		var css strings.Builder
		if hasFG {
			css.WriteString("color:")
			css.WriteString(uiColorCSS(fgColor))
			css.WriteByte(';')
		} else if sp.Link != "" {
			css.WriteString("color:")
			css.WriteString(uiColorCSS(linkFG))
			css.WriteByte(';')
		}
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			css.WriteString("background-color:")
			css.WriteString(uiColorCSS(c))
			css.WriteString(";padding:1px 3px;border-radius:3px;")
		}
		if css.Len() > 0 {
			openTags = append(openTags, fmt.Sprintf(`<span style="%s">`, css.String()))
			closeTags = append(closeTags, "</span>")
		}
		if sp.Marks&model.MarkUnderline != 0 {
			openTags = append(openTags, "<u>")
			closeTags = append(closeTags, "</u>")
		}
		if sp.Marks&model.MarkStrike != 0 {
			openTags = append(openTags, "<del>")
			closeTags = append(closeTags, "</del>")
		}
		if sp.Marks&model.MarkBold != 0 {
			openTags = append(openTags, "<strong>")
			closeTags = append(closeTags, "</strong>")
		}
		if sp.Marks&model.MarkItalic != 0 {
			openTags = append(openTags, "<em>")
			closeTags = append(closeTags, "</em>")
		}
		if sp.Marks&model.MarkCode != 0 {
			openTags = append(openTags, "<code>")
			closeTags = append(closeTags, "</code>")
		}
		for _, t := range openTags {
			out.WriteString(t)
		}
		out.WriteString(strings.ReplaceAll(escapeHTMLText(sp.Text), "\n", "<br>"))
		for i := len(closeTags) - 1; i >= 0; i-- {
			out.WriteString(closeTags[i])
		}
	}
}

func renderHTMLList(out *strings.Builder, items []model.Block, num *listNumbering, pal PaperThemeSpec) {
	if len(items) == 0 {
		return
	}
	first := items[0]
	openTag, closeTag := "<ul>", "</ul>"
	switch first.Kind {
	case model.BlockNumbered:
		if first.ListIndex > 1 {
			openTag = fmt.Sprintf(`<ol start="%d">`, first.ListIndex)
		} else {
			openTag = "<ol>"
		}
		closeTag = "</ol>"
	case model.BlockTodo:
		openTag = `<ul class="todo-list">`
	}
	out.WriteString(openTag)
	out.WriteByte('\n')
	for i := range items {
		it := &items[i]
		num.next(it)
		styleAttr := ""
		if it.Indent > 0 {
			styleAttr = fmt.Sprintf(` style="margin-left:%dpx"`, it.Indent*10)
		}
		if it.Kind == model.BlockTodo {
			chk := ""
			if it.Checked {
				chk = " checked"
			}
			out.WriteString(fmt.Sprintf(`<li%s><input type="checkbox" disabled%s> `, styleAttr, chk))
		} else {
			out.WriteString(fmt.Sprintf(`<li%s>`, styleAttr))
		}
		renderHTMLSpans(out, it.Spans, pal)
		out.WriteString("</li>\n")
	}
	out.WriteString(closeTag)
	out.WriteByte('\n')
}

func renderHTMLBlock(out *strings.Builder, b *model.Block, notePath string, pal PaperThemeSpec) {
	switch {
	case b.Kind.IsHeading():
		lvl := b.Kind.HeadingLevel()
		out.WriteString(fmt.Sprintf("<h%d>", lvl))
		if lvl == 2 && pal.Theme == model.ExportThemeRiso {
			out.WriteString(`<span class="riso-h2">`)
			renderHTMLSpans(out, b.Spans, pal)
			out.WriteString(`</span>`)
		} else {
			renderHTMLSpans(out, b.Spans, pal)
		}
		out.WriteString(fmt.Sprintf("</h%d>\n", lvl))

	case b.Kind == model.BlockDivider:
		out.WriteString("<hr>\n")

	case b.Kind == model.BlockQuote:
		out.WriteString("<blockquote><p>")
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString("</p></blockquote>\n")

	case b.Kind == model.BlockCallout:
		bg, border, accent, title := pal.CalloutColors(b.Callout)
		out.WriteString(fmt.Sprintf(
			`<div class="callout callout-%s" style="background:%s;border-left-color:%s;box-shadow:inset 0 0 0 1px %s"><div class="callout-title" style="color:%s">%s</div><div>`,
			strings.ToLower(title), uiColorCSS(bg), uiColorCSS(accent), uiColorCSS(border), uiColorCSS(accent), title,
		))
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString("</div></div>\n")

	case b.Kind == model.BlockCode:
		cls := ""
		if b.Language != "" && b.Language != "yaml-frontmatter" {
			cls = fmt.Sprintf(` class="language-%s"`, escapeAttrValue(b.Language))
		}
		out.WriteString(fmt.Sprintf("<pre><code%s>%s</code></pre>\n", cls, escapeHTMLText(b.Code)))

	case b.Kind == model.BlockTable && b.Table != nil:
		t := b.Table
		out.WriteString("<table>\n")
		if len(t.Widths) > 0 {
			var total float32
			for _, w := range t.Widths {
				total += w
			}
			if total > 0 {
				out.WriteString("<colgroup>")
				for _, w := range t.Widths {
					out.WriteString(fmt.Sprintf(`<col style="width:%.2f%%">`, w/total*100))
				}
				out.WriteString("</colgroup>\n")
			}
		}
		for r, row := range t.Rows {
			out.WriteString("<tr>")
			tag := "td"
			if r == 0 {
				tag = "th"
			}
			for c, cell := range row {
				style := ""
				if c < len(t.Aligns) && t.Aligns[c] != model.ColAlignNone {
					style = fmt.Sprintf(` style="text-align:%s"`, t.Aligns[c])
				}
				out.WriteString("<" + tag + style + ">")
				renderHTMLSpans(out, cell.Spans, pal)
				out.WriteString("</" + tag + ">")
			}
			out.WriteString("</tr>\n")
		}
		out.WriteString("</table>\n")

	case b.Kind == model.BlockImage:
		src := b.ImageSrc
		if localPath, ok := resolveLocalImagePath(src, notePath); ok {
			if raw, err := os.ReadFile(localPath); err == nil {
				mime := detectImageMime(localPath, raw)
				src = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
			}
		}
		align := string(b.ImageAlign)
		if align == "" {
			align = "left"
		}
		var imgCSS strings.Builder
		if b.ImageWidth > 0 {
			imgCSS.WriteString(fmt.Sprintf("width:%s%%;", formatFloat(b.ImageWidth)))
		}
		if !b.ImageCrop.IsZero() {
			imgCSS.WriteString(fmt.Sprintf("clip-path:inset(%.2f%% %.2f%% %.2f%% %.2f%%);",
				b.ImageCrop.Top*100, b.ImageCrop.Right*100, b.ImageCrop.Bottom*100, b.ImageCrop.Left*100))
		}
		out.WriteString(fmt.Sprintf(`<figure style="margin:12px 0;text-align:%s"><img src="%s" alt="%s"`, align, escapeAttrValue(src), escapeAttrValue(b.ImageAlt)))
		if imgCSS.Len() > 0 {
			out.WriteString(fmt.Sprintf(` style="%s"`, imgCSS.String()))
		}
		out.WriteByte('>')
		if capText := strings.TrimSpace(b.ImageAlt); capText != "" {
			out.WriteString(fmt.Sprintf(`<figcaption style="margin-top:6px;font-size:13px;color:%s">%s</figcaption>`, uiColorCSS(pal.Secondary), escapeHTMLText(capText)))
		}
		out.WriteString("</figure>\n")

	case b.Kind == model.BlockRawHTML:
		out.WriteString(b.Code)
		out.WriteByte('\n')

	default:
		if len(b.Spans) == 0 {
			out.WriteString("<p>&nbsp;</p>\n")
			return
		}
		style := ""
		if b.Indent > 0 {
			style = fmt.Sprintf(` style="margin-left:%dpx"`, b.Indent*12)
		}
		out.WriteString("<p" + style + ">")
		renderHTMLSpans(out, b.Spans, pal)
		out.WriteString("</p>\n")
	}
}

func escapeHTMLText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func detectImageMime(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	if len(data) >= 8 && bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	return "image/png"
}

// ---- PNG & PDF Visual Rendering (fixed ExportScale Paper Styles via mygo/ui) ----

type paintedBlock struct {
	kind       model.BlockKind
	x, y, w, h float32
	textX      float32
	textY      float32
	layout     *ui.TextLayout
	textColor  ui.Color
	bgColor    ui.Color
	borderCol  ui.Color
	accentCol  ui.Color
	labelLay   *ui.TextLayout
	markerLay  *ui.TextLayout
	markerCol  ui.Color
	checked    bool
	table      *paintedTable
	img        *paintedImage
}

type paintedTable struct {
	colWidths []float32
	rows      []paintedTableRow
}

type paintedTableRow struct {
	y, h  float32
	cells []paintedTableCell
}

type paintedTableCell struct {
	x, w      float32
	layout    *ui.TextLayout
	header    bool
	textColor ui.Color
	bgColor   ui.Color
}

type paintedImage struct {
	bitmap *ui.Bitmap
	src    ui.Rect
	dst    ui.Rect
}

type exportScene struct {
	width, height float32
	marginLeft    float32
	contentWidth  float32
	pal           PaperThemeSpec
	legacy        bool
	titleLayout   *ui.TextLayout
	titleX        float32
	titleY        float32
	titleH        float32
	blocks        []paintedBlock
	footerY       float32
	footerLeft    *ui.TextLayout
	footerRight   *ui.TextLayout
	showFooter    bool
}

func toUISpans(spans []model.Span, pal PaperThemeSpec, baseSize float32, isCodeBlock bool) []ui.Span {
	if len(spans) == 0 {
		return []ui.Span{{Text: " "}}
	}
	linkFG := pal.LinkFG()
	out := make([]ui.Span, 0, len(spans))
	for _, sp := range spans {
		us := ui.Span{
			Text:          sp.Text,
			Size:          baseSize,
			Italic:        sp.Marks&model.MarkItalic != 0,
			Underline:     sp.Marks&model.MarkUnderline != 0,
			Strikethrough: sp.Marks&model.MarkStrike != 0,
		}
		if sp.Marks&model.MarkBold != 0 {
			us.Weight = 700
		}
		if sp.Marks&model.MarkCode != 0 || isCodeBlock {
			us.Font = fontMono
			if !isCodeBlock {
				us.Background = pal.CodeBG
				us.Radius = 4
				us.PaddingX = 3
			}
		}
		if sp.Link != "" {
			us.Color = linkFG
			us.Underline = true
		}
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			us.Color = c
		}
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			us.Background = c
			us.Radius = 3
		}
		out = append(out, us)
	}
	return out
}

func buildExportScene(doc *model.Doc, notePath string, prefs model.ExportPreferences) exportScene {
	pal, legacy := resolveExportPaper(prefs)
	pageW := float32(resolveExportWidth(prefs))
	padTop, padRight, padBottom, padLeft := pal.Margins()
	contentW := pageW - padLeft - padRight

	sc := exportScene{
		width:        pageW,
		marginLeft:   padLeft,
		contentWidth: contentW,
		pal:          pal,
		legacy:       legacy,
	}
	y := padTop

	showTitle := resolveShowTitle(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	if showTitle {
		tWeight := 400
		if pal.TitleBold {
			tWeight = 700
		}
		tFam := pal.HeadingFamily
		if isASCII(title) {
			tFam = pal.TitleFamily
		}
		tFont := ui.Font{Family: tFam, Size: pal.TitleSize, Weight: tWeight, LineHeight: 1.3}
		shape := ui.TextShape{Width: contentW}
		if pal.Mark == TitleMarkDoubleRule {
			shape.Align = ui.Center
		}
		tLay := ui.ShapeRichTextWith([]ui.Span{{Text: title, Font: tFam, Weight: tWeight, Size: pal.TitleSize}}, tFont, shape)
		_, th := tLay.Size()
		sc.titleLayout = tLay
		sc.titleX = padLeft
		sc.titleY = y
		sc.titleH = th
		below := float32(36)
		switch pal.Mark {
		case TitleMarkRule:
			below = 20 + 2 + 36
		case TitleMarkBar:
			below = 18 + 4 + 34
		case TitleMarkWavy:
			below = 8 + 12 + 30
		case TitleMarkDoubleRule:
			below = 18 + 7 + 34
		case TitleMarkBand:
			below = 44 + 52
		}
		if legacy {
			below = 14 + 24
		}
		y += th + below
	}

	bodyBlocks := exportBodyBlocks(doc.Blocks, title, showTitle)
	var num listNumbering
	for i := range bodyBlocks {
		b := &bodyBlocks[i]
		ord := num.next(b)
		indent := float32(b.Indent) * 12
		if indent > contentW-120 {
			indent = contentW - 120
		}
		if indent < 0 {
			indent = 0
		}
		bx := padLeft + indent
		avail := contentW - indent

		switch {
		case b.Kind == model.BlockDivider:
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockDivider,
				x:         bx,
				y:         y,
				w:         avail,
				h:         20,
				borderCol: pal.Divider,
			})
			y += 28

		case b.Kind == model.BlockImage:
			if pb, ok := buildPaintedImage(b, notePath, bx, y, avail, pal); ok {
				y += pb.h + 14
				sc.blocks = append(sc.blocks, pb)
			}

		case b.Kind == model.BlockTable && b.Table != nil:
			pb := buildPaintedTable(b.Table, bx, y, avail, pal)
			y += pb.h + 14
			sc.blocks = append(sc.blocks, pb)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			codeText := b.Code
			if codeText == "" {
				codeText = " "
			}
			font := ui.Font{Family: fontMono, Size: 14.5, LineHeight: 1.6}
			padBoxX, padBoxY := float32(18), float32(14)
			var labelLay *ui.TextLayout
			labelH := float32(0)
			if b.Language != "" {
				langLabel := b.Language
				if strings.EqualFold(langLabel, "yaml-frontmatter") || strings.EqualFold(langLabel, "frontmatter") {
					langLabel = "yaml"
				}
				lf := ui.Font{Size: 13, Weight: 600, LineHeight: 1.3}
				labelLay = ui.ShapeRichTextWith([]ui.Span{{Text: strings.ToUpper(langLabel), Size: 13, Weight: 600}}, lf, ui.TextShape{Width: avail - 2*padBoxX})
				_, lh := labelLay.Size()
				labelH = lh + 6
			}
			lay := ui.ShapeRichTextWith([]ui.Span{{Text: codeText, Size: 14.5, Font: fontMono}}, font, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + 2*padBoxY + labelH
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY + labelH,
				layout:    lay,
				textColor: pal.Text,
				bgColor:   pal.CodeBG,
				borderCol: pal.CodeBorder,
				labelLay:  labelLay,
			})
			y += bh + 18

		case b.Kind == model.BlockCallout:
			bg, border, accent, cTitle := pal.CalloutColors(b.Callout)
			padBoxX, padBoxY := float32(18), float32(12)
			lf := ui.Font{Size: 13, Weight: 700, LineHeight: 1.3}
			labelLay := ui.ShapeRichTextWith([]ui.Span{{Text: cTitle, Size: 13, Weight: 700}}, lf, ui.TextShape{Width: avail - 2*padBoxX})
			_, lh := labelLay.Size()
			bodyFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), bodyFont, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + lh + 6 + 2*padBoxY
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockCallout,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY + lh + 6,
				layout:    lay,
				textColor: pal.Text,
				bgColor:   bg,
				borderCol: border,
				accentCol: accent,
				labelLay:  labelLay,
			})
			y += bh + 14

		case b.Kind == model.BlockQuote:
			padBoxX, padBoxY := float32(20), float32(12)
			if !legacy && pal.Theme == model.ExportThemePress {
				padBoxX = 4
			} else if pal.QuoteBG.A == 0 {
				padBoxY = 2
			}
			font := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), font, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + 2*padBoxY
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockQuote,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY,
				layout:    lay,
				textColor: pal.QuoteFG,
				bgColor:   pal.QuoteBG,
				accentCol: pal.QuoteBar,
			})
			y += bh + 18

		case b.Kind.IsList():
			var mLay *ui.TextLayout
			gutter := float32(28)
			if b.Kind == model.BlockNumbered {
				if ord <= 0 {
					ord = 1
				}
				marker := fmt.Sprintf("%d.", ord)
				mFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, Weight: 600, LineHeight: pal.Leading}
				mLay = ui.ShapeRichTextWith([]ui.Span{{Text: marker, Size: pal.BodySize, Weight: 600}}, mFont, ui.TextShape{Width: 64})
				mw, _ := mLay.Size()
				if mw+12 > gutter {
					gutter = mw + 12
				}
			}
			bodyFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), bodyFont, ui.TextShape{Width: avail - gutter})
			_, th := lay.Size()
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th,
				textX:     bx + gutter,
				textY:     y,
				layout:    lay,
				textColor: pal.Text,
				markerLay: mLay,
				markerCol: pal.Title,
				accentCol: pal.Accent,
				checked:   b.Checked,
			})
			y += th + 4

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
			sz := pal.BodySize
			switch lvl {
			case 1:
				sz = pal.HeadSizes[0]
			case 2:
				sz = pal.HeadSizes[1]
			case 3:
				sz = pal.HeadSizes[2]
			case 4:
				sz = pal.BodySize * 1.12
			}
			topGap := float32(8)
			if lvl == 1 {
				topGap = 18
			} else if lvl == 2 {
				topGap = 12
			}
			y += topGap
			weight := 700
			if lvl == 6 {
				weight = 500
			}
			font := ui.Font{Family: pal.HeadingFamily, Size: sz, Weight: weight, LineHeight: 1.35}
			spans := toUISpans(b.Spans, pal, sz, false)
			for idx := range spans {
				spans[idx].Weight = weight
			}
			lay := ui.ShapeRichTextWith(spans, font, ui.TextShape{Width: avail})
			_, th := lay.Size()
			botPad := float32(0)
			if lvl == 2 && !legacy && pal.Theme == model.ExportThemePress {
				botPad = 6
			}
			gapAfter := float32(8)
			if lvl == 1 {
				gapAfter = 10
			}
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th + botPad,
				textX:     bx,
				textY:     y,
				layout:    lay,
				textColor: pal.Title,
				accentCol: pal.Accent,
				borderCol: pal.Divider,
			})
			y += th + botPad + gapAfter

		default: // BlockParagraph
			if len(b.Spans) == 0 {
				y += 14
				continue
			}
			font := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), font, ui.TextShape{Width: avail})
			_, th := lay.Size()
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockParagraph,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th,
				textX:     bx,
				textY:     y,
				layout:    lay,
				textColor: pal.Text,
			})
			y += th + 10
		}
	}

	if resolveWatermark(prefs) {
		sc.showFooter = true
		sc.footerY = y + 28
		ff := ui.Font{Family: pal.BodyFamily, Size: 14, LineHeight: 1.4}
		sc.footerLeft = ui.ShapeRichTextWith([]ui.Span{{Text: watermarkLabel, Size: 14}}, ff, ui.TextShape{Width: contentW / 2})
		dateStr := time.Now().UTC().Format("2006.01.02")
		sc.footerRight = ui.ShapeRichTextWith([]ui.Span{{Text: dateStr, Size: 14}}, ff, ui.TextShape{Width: contentW / 2})
		y = sc.footerY + 16 + 14*1.4
	} else {
		y += 12
	}

	sc.height = y + padBottom
	if sc.height < 160 {
		sc.height = 160
	}
	return sc
}

func buildPaintedImage(b *model.Block, notePath string, bx, by, avail float32, pal PaperThemeSpec) (paintedBlock, bool) {
	if localPath, ok := resolveLocalImagePath(b.ImageSrc, notePath); ok {
		if raw, err := os.ReadFile(localPath); err == nil {
			if bm, err := ui.DecodeBitmap(raw); err == nil {
				bw, bh := bm.Size()
				if bw > 0 && bh > 0 {
					c := b.ImageCrop
					sx := c.Left * float32(bw)
					sy := c.Top * float32(bh)
					sw := (1 - c.Left - c.Right) * float32(bw)
					sh := (1 - c.Top - c.Bottom) * float32(bh)
					if sw <= 1 {
						sw = float32(bw)
						sx = 0
					}
					if sh <= 1 {
						sh = float32(bh)
						sy = 0
					}
					targetW := avail
					if b.ImageWidth > 0 && b.ImageWidth <= 100 {
						targetW = avail * b.ImageWidth / 100.0
					} else if sw < targetW {
						targetW = sw
					}
					targetH := targetW * (sh / sw)
					dx := bx
					switch b.ImageAlign {
					case model.ImageAlignCenter:
						dx = bx + (avail-targetW)/2
					case model.ImageAlignRight:
						dx = bx + avail - targetW
					}
					return paintedBlock{
						kind: model.BlockImage,
						x:    bx,
						y:    by,
						w:    avail,
						h:    targetH,
						img: &paintedImage{
							bitmap: bm,
							src:    ui.Rect{X: sx, Y: sy, W: sw, H: sh},
							dst:    ui.Rect{X: dx, Y: by, W: targetW, H: targetH},
						},
					}, true
				}
			}
		}
	}
	label := b.ImageAlt
	if label == "" {
		label = b.ImageSrc
	}
	if label == "" {
		label = "Image"
	}
	font := ui.Font{Size: 14, LineHeight: 1.5}
	lay := ui.ShapeRichTextWith([]ui.Span{{Text: "[Image] " + label, Size: 14}}, font, ui.TextShape{Width: avail - 24})
	_, th := lay.Size()
	bh := th + 20
	return paintedBlock{
		kind:      model.BlockImage,
		x:         bx,
		y:         by,
		w:         avail,
		h:         bh,
		textX:     bx + 12,
		textY:     by + 10,
		layout:    lay,
		textColor: pal.Secondary,
		bgColor:   pal.CodeBG,
		borderCol: pal.CodeBorder,
	}, true
}

func buildPaintedTable(t *model.TableData, bx, by, avail float32, pal PaperThemeSpec) paintedBlock {
	cols := 0
	if len(t.Rows) > 0 {
		cols = len(t.Rows[0])
	}
	if len(t.Aligns) > cols {
		cols = len(t.Aligns)
	}
	if cols == 0 {
		return paintedBlock{kind: model.BlockTable, x: bx, y: by, w: avail, h: 20}
	}
	colWidths := make([]float32, cols)
	if len(t.Widths) == cols {
		var sum float32
		for _, w := range t.Widths {
			sum += w
		}
		if sum > 0 {
			for i, w := range t.Widths {
				colWidths[i] = w / sum * avail
			}
		}
	}
	if colWidths[0] == 0 {
		each := avail / float32(cols)
		for i := range colWidths {
			colWidths[i] = each
		}
	}

	const padCellX = float32(10)
	const padCellY = float32(7)
	pt := &paintedTable{colWidths: colWidths}
	curY := by
	headerBG := pal.TableHeaderBG()
	for r, row := range t.Rows {
		isHeader := r == 0
		rowH := float32(28)
		pRow := paintedTableRow{y: curY}
		curX := bx
		for c := 0; c < cols; c++ {
			cw := colWidths[c]
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			weight := 400
			fg := pal.Text
			bg := pal.InnerCanvas
			if isHeader {
				weight = 700
				fg = pal.Title
				bg = headerBG
			}
			font := ui.Font{Family: pal.BodyFamily, Size: 14, Weight: weight, LineHeight: 1.45}
			uSpans := toUISpans(cellSpans, pal, 14, false)
			if isHeader {
				for idx := range uSpans {
					uSpans[idx].Weight = 700
				}
			}
			align := ui.Start
			if c < len(t.Aligns) {
				switch t.Aligns[c] {
				case model.ColAlignCenter:
					align = ui.Center
				case model.ColAlignRight:
					align = ui.End
				}
			}
			lay := ui.ShapeRichTextWith(uSpans, font, ui.TextShape{Width: maxFloat32(cw-2*padCellX, 20), Align: align})
			_, th := lay.Size()
			if th+2*padCellY > rowH {
				rowH = th + 2*padCellY
			}
			pRow.cells = append(pRow.cells, paintedTableCell{
				x:         curX,
				w:         cw,
				layout:    lay,
				header:    isHeader,
				textColor: fg,
				bgColor:   bg,
			})
			curX += cw
		}
		pRow.h = rowH
		pt.rows = append(pt.rows, pRow)
		curY += rowH
	}

	return paintedBlock{
		kind:      model.BlockTable,
		x:         bx,
		y:         by,
		w:         avail,
		h:         curY - by,
		borderCol: pal.TableBorder,
		table:     pt,
	}
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func buildTextureBitmap(w, h int, pal PaperThemeSpec) *ui.Bitmap {
	switch pal.Theme {
	case model.ExportThemeRiso, model.ExportThemePress, model.ExportThemeJournal, model.ExportThemeKraft:
	default:
		return nil
	}
	pw := int(float32(w) * exportScale)
	ph := int(float32(h) * exportScale)
	img := image.NewRGBA(image.Rect(0, 0, pw, ph))
	// Fill base canvas
	c := pal.Canvas
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = 255
	}
	if pal.Theme == model.ExportThemeRiso {
		paintGrainRGBA(img, color.NRGBA{R: 40, G: 30, B: 20, A: 14}, 0x517cc1b7)
	} else if pal.Theme == model.ExportThemePress {
		paintGrainRGBA(img, color.NRGBA{R: 60, G: 45, B: 25, A: 11}, 0x9e3779b9)
	}
	if pal.Card != nil {
		inset := pal.Card.Inset
		cx, cy := inset, inset
		cw, ch := float32(w)-inset*2, float32(h)-inset*2
		if pal.Card.Shadow.A > 0 {
			s := pal.Card.Shadow
			for _, layer := range [][3]float32{{2, 0, 1.0}, {8, 4, 0.6}, {18, 10, 0.3}} {
				a := uint8(float32(s.A) * layer[2])
				fillRoundedRGBA(img, cx-layer[1], cy+layer[0]-layer[1]*0.5, cw+layer[1]*2, ch+layer[1], pal.Card.Radius+layer[1], color.NRGBA{R: s.R, G: s.G, B: s.B, A: a})
			}
		}
		fillRoundedRGBA(img, cx, cy, cw, ch, pal.Card.Radius, color.NRGBA{R: pal.InnerCanvas.R, G: pal.InnerCanvas.G, B: pal.InnerCanvas.B, A: 255})
		if pal.Card.HasBorder {
			strokeRoundedRGBA(img, cx+0.5, cy+0.5, cw-1, ch-1, pal.Card.Radius, 1.0, pal.Card.Border)
		}
		if pal.Theme == model.ExportThemeKraft {
			dot := color.NRGBA{R: 240, G: 228, B: 200, A: 22}
			gap := float32(22)
			for dy := cy + gap*0.5; dy < cy+ch-pal.Card.Radius*0.5; dy += gap {
				for dx := cx + gap*0.5; dx < cx+cw-pal.Card.Radius*0.5; dx += gap {
					fillDotRGBA(img, dx, dy, 1.3, dot)
				}
			}
		}
		if pal.Theme == model.ExportThemeJournal || pal.Theme == model.ExportThemeKraft {
			paintWashiRGBA(img, float32(w)*0.5, cy, 142, 34, -2.0, pal.Accent2)
		}
	}
	return ui.NewBitmap(img)
}

func blendPix(pix []byte, idx int, r, g, b, a uint32) {
	if a == 0 {
		return
	}
	pix[idx] = uint8((r*a + uint32(pix[idx])*(255-a) + 127) / 255)
	pix[idx+1] = uint8((g*a + uint32(pix[idx+1])*(255-a) + 127) / 255)
	pix[idx+2] = uint8((b*a + uint32(pix[idx+2])*(255-a) + 127) / 255)
}

func paintGrainRGBA(img *image.RGBA, col color.NRGBA, seed uint32) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	for py := 0; py < h; py += 3 {
		for px := 0; px < w; px += 3 {
			hash := (uint32(px)*374761393 + uint32(py)*668265263) ^ seed
			hash = (hash ^ (hash >> 13)) * 1274126177
			if (hash >> 24) < 52 {
				idx := py*img.Stride + px*4
				blendPix(img.Pix, idx, uint32(col.R), uint32(col.G), uint32(col.B), uint32(col.A))
			}
		}
	}
}

func fillDotRGBA(img *image.RGBA, cx, cy, r float32, col color.NRGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	cx, cy, r = cx*exportScale, cy*exportScale, r*exportScale
	x0 := max(int(math.Floor(float64(cx-r-1))), 0)
	x1 := min(int(math.Ceil(float64(cx+r+1))), w-1)
	y0 := max(int(math.Floor(float64(cy-r-1))), 0)
	y1 := min(int(math.Ceil(float64(cy+r+1))), h-1)
	for py := y0; py <= y1; py++ {
		dy := float32(py) + 0.5 - cy
		for px := x0; px <= x1; px++ {
			dx := float32(px) + 0.5 - cx
			dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			cov := r + 0.5 - dist
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			a := uint32(float32(col.A)*cov + 0.5)
			idx := py*img.Stride + px*4
			blendPix(img.Pix, idx, uint32(col.R), uint32(col.G), uint32(col.B), a)
		}
	}
}

func roundedRectSDF(px, py, x, y, w, h, rad float32) float32 {
	cx := x + w*0.5
	cy := y + h*0.5
	hw := w*0.5 - rad
	hh := h*0.5 - rad
	if hw < 0 {
		hw = 0
	}
	if hh < 0 {
		hh = 0
	}
	qx := float32(math.Abs(float64(px-cx))) - hw
	qy := float32(math.Abs(float64(py-cy))) - hh
	mx := maxFloat32(qx, 0)
	my := maxFloat32(qy, 0)
	out := float32(math.Sqrt(float64(mx*mx + my*my)))
	in := maxFloat32(qx, qy)
	if in > 0 {
		in = 0
	}
	return out + in - rad
}

func fillRoundedRGBA(img *image.RGBA, x, y, w, h, rad float32, col color.NRGBA) {
	pw, ph := img.Bounds().Dx(), img.Bounds().Dy()
	sx, sy, sw, sh, srad := x*exportScale, y*exportScale, w*exportScale, h*exportScale, rad*exportScale
	x0 := max(int(math.Floor(float64(sx-1))), 0)
	x1 := min(int(math.Ceil(float64(sx+sw+1))), pw-1)
	y0 := max(int(math.Floor(float64(sy-1))), 0)
	y1 := min(int(math.Ceil(float64(sy+sh+1))), ph-1)
	for py := y0; py <= y1; py++ {
		fy := float32(py) + 0.5
		for px := x0; px <= x1; px++ {
			fx := float32(px) + 0.5
			d := roundedRectSDF(fx, fy, sx, sy, sw, sh, srad)
			cov := 0.5 - d
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			a := uint32(float32(col.A)*cov + 0.5)
			idx := py*img.Stride + px*4
			blendPix(img.Pix, idx, uint32(col.R), uint32(col.G), uint32(col.B), a)
		}
	}
}

func strokeRoundedRGBA(img *image.RGBA, x, y, w, h, rad, width float32, col ui.Color) {
	pw, ph := img.Bounds().Dx(), img.Bounds().Dy()
	sx, sy, sw, sh, srad, swid := x*exportScale, y*exportScale, w*exportScale, h*exportScale, rad*exportScale, width*exportScale
	x0 := max(int(math.Floor(float64(sx-swid-1))), 0)
	x1 := min(int(math.Ceil(float64(sx+sw+swid+1))), pw-1)
	y0 := max(int(math.Floor(float64(sy-swid-1))), 0)
	y1 := min(int(math.Ceil(float64(sy+sh+swid+1))), ph-1)
	half := swid * 0.5
	for py := y0; py <= y1; py++ {
		fy := float32(py) + 0.5
		for px := x0; px <= x1; px++ {
			fx := float32(px) + 0.5
			d := float32(math.Abs(float64(roundedRectSDF(fx, fy, sx, sy, sw, sh, srad))))
			cov := half + 0.5 - d
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			a := uint32(float32(col.A)*cov + 0.5)
			idx := py*img.Stride + px*4
			blendPix(img.Pix, idx, uint32(col.R), uint32(col.G), uint32(col.B), a)
		}
	}
}

func paintWashiRGBA(img *image.RGBA, cx, cy, w, h, angleDeg float32, col ui.Color) {
	dw, dh := img.Bounds().Dx(), img.Bounds().Dy()
	cx, cy = cx*exportScale, cy*exportScale
	hw, hh := w*0.5*exportScale, h*0.5*exportScale
	notch := float32(4) * exportScale
	stripeOn, period := float32(5)*exportScale, float32(11)*exportScale
	rad := float64(angleDeg) * math.Pi / 180.0
	cosA, sinA := float32(math.Cos(rad)), float32(math.Sin(rad))
	boundX := int(math.Ceil(float64(hw*float32(math.Abs(float64(cosA)))+hh*float32(math.Abs(float64(sinA)))))) + 2
	boundY := int(math.Ceil(float64(hw*float32(math.Abs(float64(sinA)))+hh*float32(math.Abs(float64(cosA)))))) + 2
	icx, icy := int(cx), int(cy)
	for dy := -boundY; dy <= boundY; dy++ {
		py := icy + dy
		if py < 0 || py >= dh {
			continue
		}
		for dx := -boundX; dx <= boundX; dx++ {
			px := icx + dx
			if px < 0 || px >= dw {
				continue
			}
			lx := float32(dx)*cosA + float32(dy)*sinA
			ly := -float32(dx)*sinA + float32(dy)*cosA
			if float32(math.Abs(float64(ly))) > hh {
				continue
			}
			t := float32(math.Mod(float64((ly+hh)/(2*hh)*3.0), 1.0))
			if t < 0 {
				t += 1
			}
			tooth := notch * (1.0 - float32(math.Abs(float64(t*2.0-1.0))))
			if float32(math.Abs(float64(lx))) > hw-tooth {
				continue
			}
			rem := float32(math.Mod(float64(lx+hw), float64(period)))
			if rem < 0 {
				rem += period
			}
			idx := py*img.Stride + px*4
			blendPix(img.Pix, idx, uint32(col.R), uint32(col.G), uint32(col.B), uint32(col.A))
			if rem < stripeOn {
				blendPix(img.Pix, idx, 255, 255, 255, 51)
			}
		}
	}
}

func renderDocRGBA(doc *model.Doc, notePath string, prefs model.ExportPreferences) (*image.RGBA, exportScene) {
	sc := buildExportScene(doc, notePath, prefs)
	w := int(sc.width)
	h := int(sc.height + 0.5)
	if h > 16000 {
		h = 16000
	}
	texBitmap := buildTextureBitmap(w, h, sc.pal)
	rgba := ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(sc.width, float32(h)).Draw(func(p *ui.Painter, r ui.Rect) {
			if texBitmap != nil {
				bw, bh := texBitmap.Size()
				p.ImageRegion(texBitmap, ui.Rect{W: float32(bw), H: float32(bh)}, r, ui.ImageOptions{})
			} else {
				p.Fill(r, sc.pal.Canvas, 0)
				if sc.pal.Card != nil {
					inset := sc.pal.Card.Inset
					cardRect := ui.Rect{X: inset, Y: inset, W: sc.width - inset*2, H: float32(h) - inset*2}
					if sc.pal.Card.Shadow.A > 0 {
						p.Shadow(cardRect, sc.pal.Card.Radius, 0, 6, 16, 0, sc.pal.Card.Shadow)
					}
					p.Fill(cardRect, sc.pal.InnerCanvas, sc.pal.Card.Radius)
					if sc.pal.Card.HasBorder {
						p.Stroke(cardRect, sc.pal.Card.Border, sc.pal.Card.Radius, 1)
					}
				}
				if !sc.legacy && sc.pal.Theme == model.ExportThemeFilm {
					hole := ui.Color{R: 232, G: 224, B: 208, A: 40}
					for x := float32(14); x+14 <= sc.width-10; x += 30 {
						p.Fill(ui.Rect{X: x, Y: 11, W: 14, H: 18}, hole, 3)
						p.Fill(ui.Rect{X: x, Y: float32(h) - 29, W: 14, H: 18}, hole, 3)
					}
				}
			}

			if sc.titleLayout != nil {
				paintExportHeader(p, sc)
			}
			for _, b := range sc.blocks {
				paintSceneBlock(p, b, sc.pal, sc.legacy)
			}
			if sc.showFooter {
				p.Line(sc.marginLeft, sc.footerY, sc.marginLeft+sc.contentWidth, sc.footerY, 1, sc.pal.Divider)
				p.TextLayout(sc.footerLeft, sc.marginLeft, sc.footerY+16, sc.pal.Secondary)
				rw, _ := sc.footerRight.Size()
				p.TextLayout(sc.footerRight, sc.marginLeft+sc.contentWidth-rw, sc.footerY+16, sc.pal.Secondary)
			}
		})
	}, w, h, exportScale)
	return rgba, sc
}

func paintExportHeader(p *ui.Painter, sc exportScene) {
	pal := sc.pal
	x, w, hy, th := sc.titleX, sc.contentWidth, sc.titleY, sc.titleH
	titleCol := pal.Title
	if sc.legacy {
		p.TextLayout(sc.titleLayout, x, hy, titleCol)
		p.Fill(ui.Rect{X: x, Y: hy + th + 14, W: w, H: 2}, pal.Accent, 1)
		return
	}
	switch pal.Mark {
	case TitleMarkRule:
		p.Fill(ui.Rect{X: x, Y: hy + th + 20, W: w, H: 2}, pal.Title, 0)
	case TitleMarkBar:
		p.Fill(ui.Rect{X: x, Y: hy + th + 18, W: 48, H: 4}, pal.Accent, 2)
	case TitleMarkWavy:
		path := &ui.Path{}
		wy := hy + th + 8
		mid := wy + 12*0.6
		step := float32(150-4) / 6.0
		path.MoveTo(x+2, mid)
		for i := 0; i < 6; i++ {
			x0 := x + 2 + step*float32(i)
			crest := wy + 12*0.1
			if i%2 != 0 {
				crest = wy + 12*1.1
			}
			path.QuadTo(x0+step*0.5, crest, x0+step, mid)
		}
		path.Cap(ui.RoundCap)
		p.StrokePath(path, 2.4, pal.Accent)
	case TitleMarkDoubleRule:
		p.Fill(ui.Rect{X: x, Y: hy + th + 18, W: w, H: 3}, pal.Divider, 0)
		p.Fill(ui.Rect{X: x, Y: hy + th + 24, W: w, H: 1}, pal.Divider, 0)
	case TitleMarkBand:
		p.Fill(ui.Rect{X: 0, Y: 0, W: sc.width, H: hy + th + 44}, pal.Accent, 0)
		titleCol = pal.Accent2
	case TitleMarkHighlight:
		ink := ui.Color{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 105}
		lines := sc.titleLayout.Lines()
		for _, ln := range lines {
			lineH := ln.Rect.H
			if lineH <= 0 {
				lineH = pal.TitleSize * 1.3
			}
			p.Fill(ui.Rect{X: x + ln.Rect.X - 4, Y: hy + ln.Rect.Y + lineH*0.5, W: ln.Rect.W + 8, H: lineH * 0.38}, ink, 2)
		}
	case TitleMarkCursor:
		lines := sc.titleLayout.Lines()
		if len(lines) > 0 {
			last := lines[len(lines)-1]
			lineH := last.Rect.H
			if lineH <= 0 {
				lineH = pal.TitleSize * 1.3
			}
			cw := float32(math.Round(float64(pal.TitleSize * 0.55)))
			p.Fill(ui.Rect{X: x + last.Rect.X + last.Rect.W + 10, Y: hy + last.Rect.Y + lineH*0.16, W: cw, H: lineH * 0.7}, pal.Accent, 0)
		}
	}
	p.TextLayout(sc.titleLayout, x, hy, titleCol)
}

func paintSceneBlock(p *ui.Painter, b paintedBlock, pal PaperThemeSpec, legacy bool) {
	radius := pal.BlockRadius()
	switch {
	case b.kind == model.BlockDivider:
		midY := b.y + b.h/2
		p.Line(b.x, midY, b.x+b.w, midY, 1, b.borderCol)

	case b.kind == model.BlockImage:
		if b.img != nil {
			p.ImageRegion(b.img.bitmap, b.img.src, b.img.dst, ui.ImageOptions{})
		} else if b.layout != nil {
			p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, b.bgColor, 8)
			p.Stroke(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, b.borderCol, 8, 1)
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockTable && b.table != nil:
		for _, row := range b.table.rows {
			for _, cell := range row.cells {
				rect := ui.Rect{X: cell.x, Y: row.y, W: cell.w, H: row.h}
				p.Fill(rect, cell.bgColor, 0)
				p.Stroke(rect, b.borderCol, 0, 1)
				if cell.layout != nil {
					p.TextLayout(cell.layout, cell.x+10, row.y+7, cell.textColor)
				}
			}
		}

	case b.kind == model.BlockCode || b.kind == model.BlockRawHTML:
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		p.Fill(rect, b.bgColor, radius)
		sw := float32(1)
		if !legacy && pal.Theme == model.ExportThemeRiso {
			sw = 1.5
		}
		p.Stroke(rect, b.borderCol, radius, sw)
		if b.labelLay != nil {
			p.TextLayout(b.labelLay, b.textX, b.y+14, pal.Secondary)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockCallout:
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		p.Fill(rect, b.bgColor, 8)
		p.Stroke(rect, b.borderCol, 8, 1)
		p.Fill(ui.Rect{X: b.x, Y: b.y, W: 4, H: b.h}, b.accentCol, 2)
		if b.labelLay != nil {
			p.TextLayout(b.labelLay, b.textX, b.y+12, b.accentCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockQuote:
		if !legacy && pal.Theme == model.ExportThemePress {
			p.Line(b.x, b.y, b.x+b.w, b.y, 1, b.accentCol)
			p.Line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1, b.accentCol)
		} else {
			if b.bgColor.A > 0 {
				p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, b.bgColor, radius)
			}
			barW := float32(4)
			if !legacy && (pal.Theme == model.ExportThemeJournal || pal.Theme == model.ExportThemeFilm) {
				barW = 3
			}
			p.Fill(ui.Rect{X: b.x, Y: b.y, W: barW, H: b.h}, b.accentCol, barW*0.5)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind.IsList():
		lineH := pal.BodySize * pal.Leading
		cy := b.y + lineH*0.5
		switch b.kind {
		case model.BlockTodo:
			if b.checked {
				p.Fill(ui.Rect{X: b.x, Y: cy - 7.5, W: 15, H: 15}, b.accentCol, 3.5)
				chk := &ui.Path{}
				chk.MoveTo(b.x+3.5, cy)
				chk.LineTo(b.x+6.5, cy+3.2)
				chk.LineTo(b.x+11.5, cy-3.2)
				chk.Cap(ui.RoundCap).Join(ui.RoundJoin)
				p.StrokePath(chk, 1.8, pal.InnerCanvas)
			} else {
				p.Stroke(ui.Rect{X: b.x + 0.75, Y: cy - 6.75, W: 13.5, H: 13.5}, b.accentCol, 3, 1.5)
			}
		case model.BlockNumbered:
			if b.markerLay != nil {
				p.TextLayout(b.markerLay, b.x, b.y, b.markerCol)
			}
		default: // Bullet
			if !legacy && pal.UsesSquareBullets() {
				p.Fill(ui.Rect{X: b.x + 2, Y: cy - 3.5, W: 7, H: 7}, b.accentCol, 0)
			} else {
				p.Fill(ui.Rect{X: b.x + 2, Y: cy - 3.2, W: 6.4, H: 6.4}, b.accentCol, 3.2)
			}
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockHeading2:
		if !legacy {
			switch pal.Theme {
			case model.ExportThemeRiso:
				ink := ui.Color{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 80}
				lw, _ := b.layout.Size()
				if lw > b.w {
					lw = b.w
				}
				lineH := pal.HeadSizes[1] * 1.35
				p.Fill(ui.Rect{X: b.x - 3, Y: b.textY + lineH*0.55, W: lw + 6, H: lineH * 0.35}, ink, 2)
			case model.ExportThemePress:
				p.Line(b.x, b.y+b.h-1, b.x+b.w, b.y+b.h-1, 1, b.borderCol)
			case model.ExportThemePoster:
				lineH := pal.HeadSizes[1] * 1.35
				bh := lineH * 0.62
				p.Fill(ui.Rect{X: b.x - 16, Y: b.textY + (lineH-bh)*0.5, W: 5, H: bh}, b.accentCol, 0)
			}
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	default:
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}
	}
}

func exportPNG(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	rgba, _ := renderDocRGBA(doc, notePath, prefs)
	if rgba == nil || rgba.Bounds().Empty() {
		return nil, errors.New("png render returned empty image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func exportPDF(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	rgba, _ := renderDocRGBA(doc, notePath, prefs)
	if rgba == nil || rgba.Bounds().Empty() {
		return nil, errors.New("pdf render returned empty image")
	}
	bounds := rgba.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	maxSliceH := int(maxPDFPageHeightPt * exportScale)

	type pdfPage struct {
		w, h    int
		ptW     int
		ptH     int
		zStream []byte
	}
	var pages []pdfPage
	for y := 0; y < imgH; y += maxSliceH {
		sliceH := imgH - y
		if sliceH > maxSliceH {
			sliceH = maxSliceH
		}
		rgb := make([]byte, imgW*sliceH*3)
		pos := 0
		for row := 0; row < sliceH; row++ {
			rowOff := (y + row) * rgba.Stride
			for col := 0; col < imgW; col++ {
				px := rgba.Pix[rowOff+col*4 : rowOff+col*4+4]
				// Composite premultiplied RGBA over white.
				gap := uint8(255 - px[3])
				rgb[pos] = addSaturate(px[0], gap)
				rgb[pos+1] = addSaturate(px[1], gap)
				rgb[pos+2] = addSaturate(px[2], gap)
				pos += 3
			}
		}
		var zBuf bytes.Buffer
		zw := zlib.NewWriter(&zBuf)
		if _, err := zw.Write(rgb); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		ptW := int(float32(imgW)/exportScale + 0.5)
		ptH := int(float32(sliceH)/exportScale + 0.5)
		if ptW < 1 {
			ptW = 1
		}
		if ptH < 1 {
			ptH = 1
		}
		pages = append(pages, pdfPage{
			w: imgW, h: sliceH, ptW: ptW, ptH: ptH, zStream: zBuf.Bytes(),
		})
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	var offsets []int
	writeObj := func(num int, body []byte) {
		offsets = append(offsets, out.Len())
		out.WriteString(fmt.Sprintf("%d 0 obj\n", num))
		out.Write(body)
		out.WriteString("\nendobj\n")
	}

	var kids strings.Builder
	for i := range pages {
		if i > 0 {
			kids.WriteByte(' ')
		}
		kids.WriteString(fmt.Sprintf("%d 0 R", 3+i*3))
	}
	writeObj(1, []byte("<< /Type /Catalog /Pages 2 0 R >>"))
	writeObj(2, []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(pages))))

	for i, pg := range pages {
		pageObj := 3 + i*3
		imgObj := 4 + i*3
		contentObj := 5 + i*3

		pageDict := fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
			pg.ptW, pg.ptH, imgObj, contentObj,
		)
		writeObj(pageObj, []byte(pageDict))

		var imgBuf bytes.Buffer
		imgBuf.WriteString(fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n",
			pg.w, pg.h, len(pg.zStream),
		))
		imgBuf.Write(pg.zStream)
		imgBuf.WriteString("\nendstream")
		writeObj(imgObj, imgBuf.Bytes())

		cmd := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im0 Do Q", pg.ptW, pg.ptH)
		contentDict := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmd), cmd)
		writeObj(contentObj, []byte(contentDict))
	}

	xrefOffset := out.Len()
	totalObjs := 2 + len(pages)*3 + 1
	out.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", totalObjs))
	for _, off := range offsets {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	out.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", totalObjs, xrefOffset))
	return out.Bytes(), nil
}

func addSaturate(a, b uint8) uint8 {
	s := int(a) + int(b)
	if s > 255 {
		return 255
	}
	return uint8(s)
}

// ---- DOCX Export (OOXML zip, aligned with 12 paper styles) ----

const (
	docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
)

type docxAssets struct {
	rels      []string
	media     []docxMedia
	nextRelID int
}

type docxMedia struct {
	path string
	data []byte
}

func (a *docxAssets) addLink(href string) string {
	id := fmt.Sprintf("rId%d", a.nextRelID)
	a.nextRelID++
	a.rels = append(a.rels, fmt.Sprintf(
		`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`,
		id, escapeAttrValue(href),
	))
	return id
}

func (a *docxAssets) addImage(data []byte, ext string, widthPx, heightPx float32, alt string) string {
	num := len(a.media) + 1
	relTarget := fmt.Sprintf("media/image%d.%s", num, ext)
	id := fmt.Sprintf("rId%d", a.nextRelID)
	a.nextRelID++
	a.rels = append(a.rels, fmt.Sprintf(
		`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="%s"/>`,
		id, relTarget,
	))
	a.media = append(a.media, docxMedia{path: "word/" + relTarget, data: data})
	cx := int64(widthPx * 9525)
	cy := int64(heightPx * 9525)
	return fmt.Sprintf(
		`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Image %d" descr="%s"/><wp:cNvGraphicFramePr/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="image%d.%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, num, num, escapeAttrValue(alt), num, ext, id, cx, cy,
	)
}

func docxHex(c ui.Color) string {
	return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
}

func docxTwips(px float32) int {
	v := int(px*15 + 0.5)
	if v < 0 {
		return 0
	}
	return v
}

func docxHalfPt(px float32) int {
	v := int(px*1.5 + 0.5)
	if v < 16 {
		return 16
	}
	return v
}

func docxFontXML(fam string) string {
	if fam == fontMono {
		return `<w:rFonts w:ascii="Menlo" w:hAnsi="Menlo" w:eastAsia="PingFang SC" w:cs="Menlo"/>`
	}
	return fmt.Sprintf(`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, fam, fam, fam, fam)
}

func exportDOCX(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	pal, legacy := resolveExportPaper(prefs)
	pageW := float32(resolveExportWidth(prefs))
	topM, rightM, bottomM, leftM := pal.Margins()
	contentW := pageW - leftM - rightM
	if contentW < 240 {
		contentW = 240
	}
	bodyHalfPt := docxHalfPt(pal.BodySize)
	line240 := int(pal.Leading*240 + 0.5)

	assets := &docxAssets{nextRelID: 3}
	var body strings.Builder

	showTitle := resolveShowTitle(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	if showTitle {
		body.WriteString(renderDOCXTitle(pal, legacy, title, pageW, topM, rightM, leftM))
	}

	bodyBlocks := exportBodyBlocks(doc.Blocks, title, showTitle)
	var num listNumbering
	for i := range bodyBlocks {
		b := &bodyBlocks[i]
		ord := num.next(b)
		indentPx := float32(b.Indent) * 12
		avail := contentW - indentPx
		if avail < 120 {
			avail = 120
		}

		switch {
		case b.Kind == model.BlockDivider:
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="%s"/></w:pBdr><w:spacing w:before="120" w:after="160"/></w:pPr></w:p>`,
				docxHex(pal.Divider),
			))

		case b.Kind == model.BlockImage:
			if lp, ok := resolveLocalImagePath(b.ImageSrc, notePath); ok {
				if raw, err := os.ReadFile(lp); err == nil {
					if cfg, formatName, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
						ext := "png"
						switch formatName {
						case "jpeg":
							ext = "jpg"
						case "gif":
							ext = "gif"
						}
						targetW := avail
						if b.ImageWidth > 0 && b.ImageWidth <= 100 {
							targetW = avail * b.ImageWidth / 100
						} else if float32(cfg.Width) < targetW {
							targetW = float32(cfg.Width)
						}
						targetH := targetW * float32(cfg.Height) / float32(cfg.Width)
						run := assets.addImage(raw, ext, targetW, targetH, b.ImageAlt)
						jc := "left"
						if b.ImageAlign == model.ImageAlignCenter {
							jc = "center"
						} else if b.ImageAlign == model.ImageAlignRight {
							jc = "right"
						}
						body.WriteString(fmt.Sprintf(
							`<w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="0" w:after="160"/></w:pPr>%s</w:p>`,
							jc, run,
						))
						continue
					}
				}
			}
			alt := strings.TrimSpace(b.ImageAlt)
			if alt == "" {
				alt = strings.TrimSpace(b.ImageSrc)
			}
			label := "[Image]"
			if alt != "" {
				label = "[Image] " + alt
			}
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="120"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
				docxHex(pal.Secondary), docxTextXML(label),
			))

		case b.Kind == model.BlockTable && b.Table != nil:
			body.WriteString(renderDOCXTable(b.Table, avail, indentPx, pal, assets))
			body.WriteString(`<w:p><w:pPr><w:spacing w:before="0" w:after="100" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			var inner strings.Builder
			if b.Language != "" {
				inner.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:spacing w:before="0" w:after="60"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="20"/></w:rPr>%s</w:r></w:p>`,
					docxHex(pal.Secondary), docxTextXML(strings.ToUpper(b.Language)),
				))
			}
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr><w:r><w:rPr>%s<w:sz w:val="22"/><w:szCs w:val="22"/><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
				docxFontXML(fontMono), docxHex(pal.Text), docxTextXML(b.Code),
			))
			borders := fmt.Sprintf(
				`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
				docxHex(pal.CodeBorder), docxHex(pal.CodeBorder), docxHex(pal.CodeBorder), docxHex(pal.CodeBorder),
			)
			body.WriteString(renderDOCXBox(inner.String(), avail, indentPx, pal.CodeBG, borders))

		case b.Kind == model.BlockCallout:
			bg, border, accent, cTitle := pal.CalloutColors(b.Callout)
			var inner strings.Builder
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="80"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr>%s</w:r></w:p>`,
				docxHex(accent), docxTextXML(cTitle),
			))
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				line240, renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.Text, assets),
			))
			borders := fmt.Sprintf(
				`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
				docxHex(border), docxHex(accent), docxHex(border), docxHex(border),
			)
			body.WriteString(renderDOCXBox(inner.String(), avail, indentPx, bg, borders))

		case b.Kind == model.BlockQuote:
			inner := fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				line240, renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.QuoteFG, assets),
			)
			borders := fmt.Sprintf(`<w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/>`, docxHex(pal.QuoteBar))
			if !legacy && pal.Theme == model.ExportThemePress {
				borders = fmt.Sprintf(
					`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
					docxHex(pal.QuoteBar), docxHex(pal.QuoteBar),
				)
			}
			bg := pal.QuoteBG
			if bg.A == 0 {
				bg = pal.InnerCanvas
			}
			body.WriteString(renderDOCXBox(inner, avail, indentPx, bg, borders))

		case b.Kind.IsList():
			marker := "•"
			if !legacy && pal.UsesSquareBullets() {
				marker = "▪"
			}
			if b.Kind == model.BlockNumbered {
				if ord <= 0 {
					ord = 1
				}
				marker = fmt.Sprintf("%d.", ord)
			} else if b.Kind == model.BlockTodo {
				if b.Checked {
					marker = "☑"
				} else {
					marker = "☐"
				}
			}
			gutter := float32(24)
			runs := renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="%d"/></w:tabs><w:ind w:left="%d" w:hanging="%d"/><w:spacing w:before="0" w:after="80" w:line="%d" w:lineRule="auto"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/></w:rPr>%s</w:r><w:r><w:tab/></w:r>%s</w:p>`,
				docxTwips(indentPx+gutter), docxTwips(indentPx+gutter), docxTwips(gutter), line240,
				docxHex(pal.Accent), docxTextXML(marker), runs,
			))

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
			px := pal.BodySize
			switch lvl {
			case 1:
				px = pal.HeadSizes[0]
			case 2:
				px = pal.HeadSizes[1]
			case 3:
				px = pal.HeadSizes[2]
			case 4:
				px = pal.BodySize * 1.12
			}
			halfPts := docxHalfPt(px)
			bold := lvl != 6
			runs := renderDOCXRuns(b.Spans, pal, bold, halfPts, pal.Title, assets)
			bdr := ""
			if lvl == 2 && !legacy && pal.Theme == model.ExportThemePress {
				bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="4" w:color="%s"/></w:pBdr>`, docxHex(pal.Divider))
			}
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:pStyle w:val="Heading%d"/>%s<w:spacing w:before="200" w:after="120"/></w:pPr>%s</w:p>`,
				lvl, bdr, runs,
			))

		default:
			runs := renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:before="0" w:after="140" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				docxTwips(indentPx), line240, runs,
			))
		}
	}

	if resolveWatermark(prefs) {
		dateStr := time.Now().UTC().Format("2006.01.02")
		body.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:top w:val="single" w:sz="6" w:space="6" w:color="%s"/></w:pBdr><w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs><w:spacing w:before="280" w:after="0"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="21"/></w:rPr>%s<w:tab/>%s</w:r></w:p>`,
			docxHex(pal.Divider), docxTwips(contentW), docxHex(pal.Secondary), docxTextXML(watermarkLabel), docxTextXML(dateStr),
		))
	}

	body.WriteString(fmt.Sprintf(
		`<w:sectPr><w:pgSz w:w="%d" w:h="16838"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr>`,
		docxTwips(pageW), docxTwips(topM), docxTwips(rightM), docxTwips(bottomM), docxTwips(leftM),
	))

	docXML := fmt.Sprintf(`%s<w:document %s><w:background w:color="%s"/><w:body>%s</w:body></w:document>`,
		xmlDecl, docxNS, docxHex(pal.InnerCanvas), body.String(),
	)

	stylesXML := buildDOCXStyles(pal)
	settingsXML := fmt.Sprintf(
		`%s<w:settings %s><w:displayBackgroundShape/><w:autoHyphenation w:val="0"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`,
		xmlDecl, docxNS,
	)
	contentTypes := xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-officedocument.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="png" ContentType="image/png"/>` +
		`<Default Extension="jpg" ContentType="image/jpeg"/>` +
		`<Default Extension="gif" ContentType="image/gif"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
		`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
		`</Types>`
	rootRels := xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	docRels := fmt.Sprintf(
		`%s<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>%s</Relationships>`,
		xmlDecl, strings.Join(assets.rels, ""),
	)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	writeZipEntry := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	entries := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", []byte(contentTypes)},
		{"_rels/.rels", []byte(rootRels)},
		{"word/_rels/document.xml.rels", []byte(docRels)},
		{"word/styles.xml", []byte(stylesXML)},
		{"word/settings.xml", []byte(settingsXML)},
		{"word/document.xml", []byte(docXML)},
	}
	for _, e := range entries {
		if err := writeZipEntry(e.name, e.data); err != nil {
			return nil, err
		}
	}
	for _, m := range assets.media {
		if err := writeZipEntry(m.path, m.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return zipBuf.Bytes(), nil
}

func renderDOCXTitle(pal PaperThemeSpec, legacy bool, title string, pageW, topM, rightM, leftM float32) string {
	fam := pal.HeadingFamily
	if isASCII(title) {
		fam = pal.TitleFamily
	}
	halfPt := docxHalfPt(pal.TitleSize)
	boldXML := ""
	if pal.TitleBold {
		boldXML = "<w:b/><w:bCs/>"
	}
	fg := pal.Title
	if !legacy && pal.Mark == TitleMarkBand {
		fg = pal.Accent2
	}
	hl := ""
	if !legacy && pal.Mark == TitleMarkHighlight {
		hl = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(pal.HighlightBG()))
	}
	run := fmt.Sprintf(`<w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/>%s</w:rPr>%s</w:r>`,
		docxFontXML(fam), boldXML, docxHex(fg), halfPt, halfPt, hl, docxTextXML(title))
	if !legacy && pal.Mark == TitleMarkCursor {
		run += fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t> ▌</w:t></w:r>`, docxHex(pal.Accent), halfPt)
	}
	if !legacy && pal.Mark == TitleMarkBand {
		full := docxTwips(pageW)
		return fmt.Sprintf(
			`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="-%d" w:type="dxa"/><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:bottom w:w="660" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr><w:p><w:pPr><w:keepNext/><w:spacing w:before="0" w:after="0"/></w:pPr>%s</w:p></w:tc></w:tr></w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="240" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
			full, docxTwips(leftM), docxTwips(topM), docxTwips(leftM), docxTwips(rightM), full, full, docxHex(pal.Accent), run,
		)
	}
	jc := ""
	bdr := fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="12" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Accent))
	if !legacy {
		switch pal.Mark {
		case TitleMarkDoubleRule:
			jc = `<w:jc w:val="center"/>`
			bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="double" w:sz="8" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Divider))
		case TitleMarkRule:
			bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="12" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Title))
		case TitleMarkWavy:
			bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="wave" w:sz="8" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Accent))
		case TitleMarkHighlight, TitleMarkCursor:
			bdr = ""
		}
	}
	return fmt.Sprintf(
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/><w:keepNext/>%s%s<w:spacing w:before="0" w:after="260"/></w:pPr>%s</w:p>`,
		jc, bdr, run,
	)
}

func buildDOCXStyles(pal PaperThemeSpec) string {
	bodyHalfPt := docxHalfPt(pal.BodySize)
	line240 := int(pal.Leading*240 + 0.5)
	var out strings.Builder
	out.WriteString(fmt.Sprintf(
		`%s<w:styles %s><w:docDefaults><w:rPrDefault><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:widowControl w:val="0"/><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`,
		xmlDecl, docxNS, docxFontXML(pal.BodyFamily), docxHex(pal.Text), bodyHalfPt, bodyHalfPt, line240,
	))
	for lvl := 1; lvl <= 6; lvl++ {
		px := pal.BodySize
		switch lvl {
		case 1:
			px = pal.HeadSizes[0]
		case 2:
			px = pal.HeadSizes[1]
		case 3:
			px = pal.HeadSizes[2]
		case 4:
			px = pal.BodySize * 1.12
		}
		hp := docxHalfPt(px)
		bTag := "<w:b/><w:bCs/>"
		if lvl == 6 {
			bTag = `<w:b w:val="0"/><w:bCs w:val="0"/>`
		}
		out.WriteString(fmt.Sprintf(
			`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
			lvl, lvl, lvl-1, docxFontXML(pal.HeadingFamily), bTag, docxHex(pal.Title), hp, hp,
		))
	}
	out.WriteString("</w:styles>")
	return out.String()
}

func docxTextXML(s string) string {
	var out strings.Builder
	lines := strings.Split(s, "\n")
	for li, line := range lines {
		if li > 0 {
			out.WriteString("<w:br/>")
		}
		parts := strings.Split(line, "\t")
		for pi, piece := range parts {
			if pi > 0 {
				out.WriteString("<w:tab/>")
			}
			if piece != "" {
				out.WriteString(`<w:t xml:space="preserve">`)
				out.WriteString(escapeXMLText(piece))
				out.WriteString("</w:t>")
			}
		}
	}
	return out.String()
}

func escapeXMLText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func renderDOCXRuns(spans []model.Span, pal PaperThemeSpec, baseBold bool, halfPt int, baseFG ui.Color, assets *docxAssets) string {
	if len(spans) == 0 {
		return ""
	}
	linkFG := pal.LinkFG()
	var out strings.Builder
	for _, sp := range spans {
		var rpr strings.Builder
		if sp.Marks&model.MarkCode != 0 {
			rpr.WriteString(docxFontXML(fontMono))
		}
		if baseBold || sp.Marks&model.MarkBold != 0 {
			rpr.WriteString("<w:b/><w:bCs/>")
		}
		if sp.Marks&model.MarkItalic != 0 {
			rpr.WriteString("<w:i/><w:iCs/>")
		}
		if sp.Marks&model.MarkStrike != 0 {
			rpr.WriteString("<w:strike/>")
		}
		if sp.Marks&model.MarkUnderline != 0 || sp.Link != "" {
			rpr.WriteString(`<w:u w:val="single"/>`)
		}
		fg := baseFG
		if sp.Link != "" {
			fg = linkFG
		}
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			fg = c
		}
		rpr.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, docxHex(fg)))
		rpr.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, halfPt, halfPt))
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(c)))
		} else if sp.Marks&model.MarkCode != 0 {
			rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(pal.CodeBG)))
		}
		run := fmt.Sprintf(`<w:r><w:rPr>%s</w:rPr>%s</w:r>`, rpr.String(), docxTextXML(sp.Text))
		if sp.Link != "" {
			rID := assets.addLink(sp.Link)
			out.WriteString(fmt.Sprintf(`<w:hyperlink r:id="%s">%s</w:hyperlink>`, rID, run))
		} else {
			out.WriteString(run)
		}
	}
	return out.String()
}

func renderDOCXBox(content string, widthPx, indentPx float32, bg ui.Color, borders string) string {
	wTwips := docxTwips(widthPx)
	iTwips := docxTwips(indentPx)
	return fmt.Sprintf(
		`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/><w:tblBorders>%s</w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="150" w:type="dxa"/><w:bottom w:w="150" w:type="dxa"/><w:left w:w="210" w:type="dxa"/><w:right w:w="210" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr>%s</w:tc></w:tr></w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="120" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
		wTwips, iTwips, borders, wTwips, wTwips, docxHex(bg), content,
	)
}

func renderDOCXTable(t *model.TableData, widthPx, indentPx float32, pal PaperThemeSpec, assets *docxAssets) string {
	cols := 0
	if len(t.Rows) > 0 {
		cols = len(t.Rows[0])
	}
	if cols == 0 {
		return ""
	}
	colWidths := make([]int, cols)
	totalTwips := docxTwips(widthPx)
	if len(t.Widths) == cols {
		var sum float32
		for _, w := range t.Widths {
			sum += w
		}
		if sum > 0 {
			for i, w := range t.Widths {
				colWidths[i] = int(float32(totalTwips)*(w/sum) + 0.5)
			}
		}
	}
	if colWidths[0] == 0 {
		each := totalTwips / cols
		for i := range colWidths {
			colWidths[i] = each
		}
	}

	var grid strings.Builder
	for _, cw := range colWidths {
		grid.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, cw))
	}
	bc := docxHex(pal.TableBorder)
	borders := fmt.Sprintf(
		`<w:top w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="%s"/>`,
		bc, bc, bc, bc, bc, bc,
	)
	var out strings.Builder
	out.WriteString(fmt.Sprintf(
		`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/><w:tblBorders>%s</w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="90" w:type="dxa"/><w:bottom w:w="90" w:type="dxa"/><w:left w:w="120" w:type="dxa"/><w:right w:w="120" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>%s</w:tblGrid>`,
		totalTwips, docxTwips(indentPx), borders, grid.String(),
	))
	headerBG := pal.TableHeaderBG()
	for r, row := range t.Rows {
		isHeader := r == 0
		out.WriteString("<w:tr>")
		for c := 0; c < cols; c++ {
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			shd := ""
			baseFG := pal.Text
			if isHeader {
				shd = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(headerBG))
				baseFG = pal.Title
			}
			jc := "left"
			if c < len(t.Aligns) && t.Aligns[c] != model.ColAlignNone {
				jc = string(t.Aligns[c])
			}
			runs := renderDOCXRuns(cellSpans, pal, isHeader, 22, baseFG, assets)
			out.WriteString(fmt.Sprintf(
				`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s<w:vAlign w:val="top"/></w:tcPr><w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr>%s</w:p></w:tc>`,
				colWidths[c], shd, jc, runs,
			))
		}
		out.WriteString("</w:tr>")
	}
	out.WriteString("</w:tbl>")
	return out.String()
}

// ---- ZIP Export (Markdown + Local Referenced Images) ----

func exportZIP(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	cloned := doc.Clone()
	usedNames := make(map[string]int)
	pathToZip := make(map[string]string)

	type zipAsset struct {
		zipPath string
		data    []byte
	}
	var assets []zipAsset

	for i := range cloned.Blocks {
		b := &cloned.Blocks[i]
		if b.Kind != model.BlockImage {
			continue
		}
		localPath, ok := resolveLocalImagePath(b.ImageSrc, notePath)
		if !ok {
			continue
		}
		zipPath, seen := pathToZip[localPath]
		if !seen {
			raw, err := os.ReadFile(localPath)
			if err != nil {
				continue
			}
			base := filepath.Base(localPath)
			if base == "" || base == "." || base == string(filepath.Separator) {
				base = "image.png"
			}
			count := usedNames[base]
			usedNames[base] = count + 1
			name := base
			if count > 0 {
				ext := filepath.Ext(base)
				stem := strings.TrimSuffix(base, ext)
				name = fmt.Sprintf("%s-%d%s", stem, count, ext)
			}
			zipPath = "assets/" + name
			pathToZip[localPath] = zipPath
			assets = append(assets, zipAsset{zipPath: zipPath, data: raw})
		}
		if b.ImageSrc != zipPath {
			b.ImageSrc = zipPath
			b.Dirty = true
			b.CleanSource = ""
		}
	}

	mdContent := exportMarkdown(cloned, notePath, prefs)
	mdName := "note.md"
	if base := strings.TrimSpace(filepath.Base(notePath)); base != "" && base != "." && base != string(filepath.Separator) {
		if !strings.HasSuffix(strings.ToLower(base), ".md") {
			base += ".md"
		}
		mdName = base
	} else if t := strings.TrimSpace(doc.Title()); t != "" {
		safe := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`/\:*?"<>|`, r) || r < 0x20 {
				return '-'
			}
			return r
		}, t)
		if safe != "" {
			mdName = safe + ".md"
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mw, err := zw.Create(mdName)
	if err != nil {
		return nil, err
	}
	if _, err := mw.Write([]byte(mdContent)); err != nil {
		return nil, err
	}
	for _, a := range assets {
		aw, err := zw.Create(a.zipPath)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(a.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
