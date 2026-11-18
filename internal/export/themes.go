package export

import (
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// PaperSpec exposes the full 1:1 paper theme palette, typography, card geometry, and swatch triad.
type PaperSpec struct {
	Theme         model.ExportTheme
	Dark          bool
	Label         string
	Canvas        ui.Color
	InnerCanvas   ui.Color
	Title         ui.Color
	Text          ui.Color
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
	Card          *markdown.CardSpec
	PadTop        float32
	PadRight      float32
	PadBottom     float32
	PadLeft       float32
	BodySize      float32
	Leading       float32
	TitleSize     float32
	HeadSizes     [3]float32
	Mark          markdown.TitleMark
	TitleFamily   string
	HeadingFamily string
	BodyFamily    string
	TitleBold     bool
	LinkFG        ui.Color
	TableHeaderBG ui.Color
	HighlightBG   ui.Color
}

var (
	lightThemes = []model.ExportTheme{
		model.ExportThemeMono,
		model.ExportThemeJournal,
		model.ExportThemeRiso,
		model.ExportThemePress,
		model.ExportThemePoster,
		model.ExportThemeMist,
	}
	darkThemes = []model.ExportTheme{
		model.ExportThemeInk,
		model.ExportThemeKraft,
		model.ExportThemeNeon,
		model.ExportThemeFilm,
		model.ExportThemeNight,
		model.ExportThemeTerminal,
	}
)

// LightThemes returns the 6 light paper styles in sidebar display order.
func LightThemes() []model.ExportTheme {
	return append([]model.ExportTheme(nil), lightThemes...)
}

// DarkThemes returns the 6 dark paper styles in sidebar display order.
func DarkThemes() []model.ExportTheme {
	return append([]model.ExportTheme(nil), darkThemes...)
}

// AllThemes returns all 12 paper styles (6 light followed by 6 dark).
func AllThemes() []model.ExportTheme {
	out := make([]model.ExportTheme, 0, len(lightThemes)+len(darkThemes))
	out = append(out, lightThemes...)
	out = append(out, darkThemes...)
	return out
}

// ThemeGroup returns either the 6 dark or 6 light paper styles.
func ThemeGroup(dark bool) []model.ExportTheme {
	if dark {
		return DarkThemes()
	}
	return LightThemes()
}

// DefaultThemeFor returns Mono for light mode and Ink for dark mode.
func DefaultThemeFor(dark bool) model.ExportTheme {
	if dark {
		return model.ExportThemeInk
	}
	return model.ExportThemeMono
}

// NormalizeTheme maps empty or unrecognized theme values to Mono.
func NormalizeTheme(t model.ExportTheme) model.ExportTheme {
	return markdown.NormalizeExportTheme(t)
}

// ThemeLabel returns the localized display name for a paper theme.
func ThemeLabel(t model.ExportTheme) string {
	switch NormalizeTheme(t) {
	case model.ExportThemeJournal:
		return theme.Tr("手账便签", "Journal")
	case model.ExportThemeRiso:
		return theme.Tr("孔版双色", "Risograph")
	case model.ExportThemePress:
		return theme.Tr("铅字报刊", "Press")
	case model.ExportThemePoster:
		return theme.Tr("大字海报", "Poster")
	case model.ExportThemeMist:
		return theme.Tr("雾面卡片", "Mist")
	case model.ExportThemeInk:
		return theme.Tr("墨黑", "Ink")
	case model.ExportThemeKraft:
		return theme.Tr("黑卡手账", "Kraft")
	case model.ExportThemeNeon:
		return theme.Tr("霓虹夜色", "Neon")
	case model.ExportThemeFilm:
		return theme.Tr("暗房胶片", "Film")
	case model.ExportThemeNight:
		return theme.Tr("夜读卡片", "Night")
	case model.ExportThemeTerminal:
		return theme.Tr("终端绿屏", "Terminal")
	default:
		return theme.Tr("黑白素纸", "Mono")
	}
}

// ThemeSpec returns the complete color palette, card geometry, and swatch triad for t.
func ThemeSpec(t model.ExportTheme) PaperSpec {
	raw := markdown.PaperTheme(t)
	return PaperSpec{
		Theme:         raw.Theme,
		Dark:          raw.Dark,
		Label:         ThemeLabel(raw.Theme),
		Canvas:        raw.Canvas,
		InnerCanvas:   raw.InnerCanvas,
		Title:         raw.Title,
		Text:          raw.Text,
		Secondary:     raw.Secondary,
		Accent:        raw.Accent,
		Accent2:       raw.Accent2,
		CodeBG:        raw.CodeBG,
		CodeBorder:    raw.CodeBorder,
		QuoteBG:       raw.QuoteBG,
		QuoteFG:       raw.QuoteFG,
		QuoteBar:      raw.QuoteBar,
		TableBorder:   raw.TableBorder,
		Divider:       raw.Divider,
		Swatch:        raw.Swatch,
		Card:          raw.Card,
		PadTop:        raw.PadTop,
		PadRight:      raw.PadRight,
		PadBottom:     raw.PadBottom,
		PadLeft:       raw.PadLeft,
		BodySize:      raw.BodySize,
		Leading:       raw.Leading,
		TitleSize:     raw.TitleSize,
		HeadSizes:     raw.HeadSizes,
		Mark:          raw.Mark,
		TitleFamily:   raw.TitleFamily,
		HeadingFamily: raw.HeadingFamily,
		BodyFamily:    raw.BodyFamily,
		TitleBold:     raw.TitleBold,
		LinkFG:        raw.LinkFG(),
		TableHeaderBG: raw.TableHeaderBG(),
		HighlightBG:   raw.HighlightBG(),
	}
}
