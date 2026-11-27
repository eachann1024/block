package markdown

import (
	_ "image/gif"
	_ "image/jpeg"
	"math"

	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

// CardKind is how a theme's page card is drawn.
type CardKind uint8

const (
	CardFill    CardKind = iota // a filled sheet laid on the canvas
	CardOutline                 // a hairline frame, the canvas showing through
)

// CardSpec describes the sheet or frame the content sits in.
type CardSpec struct {
	Kind CardKind
	// Inset is the distance from the page's sides and bottom; InsetTop from its top (0 = Inset).
	Inset    float32
	InsetTop float32
	Radius   float32
	Fill     ui.Color // 0 = InnerCanvas
	Border   ui.Color
	// HasBorder draws Border BorderW wide (0 = 1).
	HasBorder bool
	BorderW   float32
	Shadow    ui.Color
	ShadowY   float32 // 0 = 6
	Blur      float32 // 0 = 16
	// TopEdge is a highlight along the card's top edge (glass, lacquer).
	TopEdge ui.Color
	// Label is a template set into the top border (Terminal window title).
	Label      string
	LabelColor ui.Color
	// BodyOnly keeps the meta line above the card and the footer below it,
	// on the canvas, so the card holds only the title and body (Neon).
	BodyOnly bool
}

func (c *CardSpec) top() float32 { return orF(c.InsetTop, c.Inset) }

// PaperThemeSpec defines the palette, page geometry and design vocabulary of a paper export theme.
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
	// Radius rounds code, image and callout boxes.
	Radius float32
	Style  PaperStyle
}

const (
	fontSans    = "HarmonyOS Sans SC, PingFang SC, Microsoft YaHei, Hiragino Sans GB, Noto Sans CJK SC, Noto Sans SC, sans-serif"
	fontSerif   = "Noto Serif SC, Georgia, Songti SC, Source Han Serif SC, Noto Serif CJK SC, SimSun, serif"
	fontKai     = "LXGW WenKai, Kaiti SC, STKaiti, KaiTi, FZKai-Z03, serif"
	fontMono    = "JetBrains Mono, Menlo, Monaco, Cascadia Mono, Consolas, DejaVu Sans Mono, HarmonyOS Sans SC, PingFang SC, Microsoft YaHei, Noto Sans CJK SC, monospace"
	fontMonoCJK = "JetBrains Mono, HarmonyOS Sans SC, Menlo, PingFang SC, Consolas, Microsoft YaHei, Noto Sans CJK SC, monospace"
)

func rgbHex(v uint32) ui.Color {
	return ui.RGB(uint8(v>>16), uint8(v>>8), uint8(v))
}

func rgbaBytes(r, g, b, a uint8) ui.Color {
	return ui.Color{R: r, G: g, B: b, A: a}
}

// rgbaHex is rgbHex at opacity a (0–1).
func rgbaHex(v uint32, a float32) ui.Color {
	c := rgbHex(v)
	c.A = uint8(a*255 + 0.5)
	return c
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

// ThemeWebFontFamilies returns the downloadable CJK/Latin webfont family names
// required to render theme t natively.
func ThemeWebFontFamilies(t model.ExportTheme) []string {
	switch NormalizeExportTheme(t) {
	case model.ExportThemeJournal, model.ExportThemeKraft:
		return []string{"LXGW WenKai", "HarmonyOS Sans SC"}
	case model.ExportThemeMono,
		model.ExportThemePress,
		model.ExportThemeMist,
		model.ExportThemeInk,
		model.ExportThemeFilm,
		model.ExportThemeNight:
		return []string{"Noto Serif SC", "HarmonyOS Sans SC"}
	case model.ExportThemeRiso,
		model.ExportThemePoster,
		model.ExportThemeNeon,
		model.ExportThemeTerminal:
		return []string{"HarmonyOS Sans SC"}
	default:
		return []string{"Noto Serif SC", "HarmonyOS Sans SC"}
	}
}

// PaperTheme returns the paper theme specification for any of the 12 ExportTheme styles.
func PaperTheme(t model.ExportTheme) PaperThemeSpec {
	switch NormalizeExportTheme(t) {
	case model.ExportThemeJournal:
		return journalTheme()
	case model.ExportThemeRiso:
		return risoTheme()
	case model.ExportThemePress:
		return pressTheme()
	case model.ExportThemePoster:
		return posterTheme()
	case model.ExportThemeMist:
		return mistTheme()
	case model.ExportThemeInk:
		return inkTheme()
	case model.ExportThemeKraft:
		return kraftTheme()
	case model.ExportThemeNeon:
		return neonTheme()
	case model.ExportThemeFilm:
		return filmTheme()
	case model.ExportThemeNight:
		return nightTheme()
	case model.ExportThemeTerminal:
		return terminalTheme()
	default:
		return monoTheme()
	}
}

// Margins is the distance from each page edge to the content.
func (s PaperThemeSpec) Margins() (top, right, bottom, left float32) {
	var inset, insetTop float32
	if s.Card != nil {
		inset, insetTop = s.Card.Inset, s.Card.top()
	}
	if s.Style.Sections != nil {
		x, y := s.Style.Sections.PadX, s.Style.Sections.PadY
		return s.PadTop + y, s.PadRight + x, s.PadBottom + y, s.PadLeft + x
	}
	return s.PadTop + insetTop, s.PadRight + inset, s.PadBottom + inset, s.PadLeft + inset
}

func (s PaperThemeSpec) BlockRadius() float32 { return s.Radius }

func (s PaperThemeSpec) UsesSquareBullets() bool { return s.Style.List.Bullet == BulletSquare }

// LinkFG returns Inline.LinkColor when set, else Accent when WCAG AA contrast against InnerCanvas is >= 4.5, otherwise Title.
func (s PaperThemeSpec) LinkFG() ui.Color {
	if s.Style.Inline.LinkColor.A > 0 {
		return s.Style.Inline.LinkColor
	}
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
	if h := s.Style.Inline.Highlight; h != nil && h.BG.A > 0 {
		return h.BG
	}
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
		TitleFamily:   fontSans,
		HeadingFamily: fontSans,
		BodyFamily:    fontSans,
		TitleBold:     true,
		Radius:        6,
	}
}
