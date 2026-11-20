package pluginui

import (
	"strconv"
	"strings"

	"block/internal/plugins"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

func renderDeclarativeLanguageTab(c *ui.Context, ctrl *Controller, inst plugins.InstalledApp) {
	pal := theme.Ocean()
	locale := ctrl.sup.Locale()
	langs := plugins.ActiveLanguagesForPlugin(inst.Manifest.ID)
	if len(langs) == 0 {
		langs = plugins.LoadPluginLanguagesFromDir(inst.InstallDir, &inst.Manifest)
	}

	ui.Column(c.Key("plugin-lang-body-" + inst.Manifest.ID)).FillWidth().MinWidth(0).Gap(theme.SettingsSectionGap).Children(func() {
		if len(langs) == 0 {
			bui.SettingsSection(c, func() {
				bui.SettingsRowFrame(ui.Row(c)).Children(func() {
					ui.Text(c, theme.Tr("未检测到可用的语言语法定义", "No language grammars found")).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
				})
			})
			return
		}

		for _, info := range langs {
			lang := info
			c.Key("plugin-lang-sec-" + lang.Contrib.ID)
			bui.SettingsSection(c, func() {
				bui.SettingsSectionTitle(c, lang.Contrib.Name.Resolve(locale), nil)

				patterns := make([]string, 0, len(lang.Contrib.Filenames)+len(lang.Contrib.Extensions))
				patterns = append(patterns, lang.Contrib.Filenames...)
				patterns = append(patterns, lang.Contrib.Extensions...)
				if len(patterns) > 0 {
					bui.SettingsRowFrame(ui.Row(c)).Gap(theme.Space12).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
						ui.Text(c, theme.Tr("匹配文件", "Matched Files")).
							FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).Shrink(0)
						renderMonoTagChips(c, "lang-pat-"+lang.Contrib.ID, patterns)
					})
				}

				tags := make([]string, 0, 1+len(lang.Contrib.Aliases))
				tags = append(tags, lang.Contrib.ID)
				tags = append(tags, lang.Contrib.Aliases...)
				if len(tags) > 0 {
					bui.SettingsRowFrame(ui.Row(c)).Gap(theme.Space12).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
						ui.Text(c, theme.Tr("代码块标签", "Code Block Tags")).
							FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).Shrink(0)
						renderMonoTagChips(c, "lang-alias-"+lang.Contrib.ID, tags)
					})
				}

				if strings.TrimSpace(lang.Sample) != "" {
					renderLanguageHighlightPreview(c, lang)
				}
			})
		}
	})
}

func renderMonoTagChips(c *ui.Context, keyPrefix string, tags []string) {
	pal := theme.Ocean()
	ui.Row(c.Key(keyPrefix)).Gap(theme.Space4).AlignItems(ui.Center).Justify(ui.End).Wrap().MinWidth(0).Children(func() {
		for _, tag := range tags {
			t := tag
			ui.Row(c).Height(theme.HeightMicro).PaddingX(theme.Space6).Radius(theme.RadiusSM).
				Background(pal.CodeBG).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, t).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).SingleLine()
			})
		}
	})
}

func renderLanguageHighlightPreview(c *ui.Context, lang plugins.PluginLanguageInfo) {
	pal := theme.Ocean()
	lines := strings.Split(lang.Sample, "\n")
	var (
		state uint64
		buf   []codeed.Token
	)

	ui.Column(c.Key("lang-preview-"+lang.Contrib.ID)).FillWidth().MinWidth(0).
		Padding(theme.Space12, theme.Space16).Gap(theme.Space2).
		Radius(theme.RadiusLG).Background(pal.CodeBG).Border(1, pal.Hairline).Children(func() {
		for idx, line := range lines {
			var spans []ui.Span
			if lang.Tokenizer != nil && len(line) > 0 {
				buf, state = lang.Tokenizer.Tokenize(line, state, buf[:0])
				pos := 0
				for _, tk := range buf {
					end := min(max(tk.End, pos), len(line))
					if end <= pos {
						continue
					}
					spans = append(spans, ui.Span{
						Text:  line[pos:end],
						Color: languagePreviewColor(pal, tk.Kind),
					})
					pos = end
				}
				if pos < len(line) {
					spans = append(spans, ui.Span{Text: line[pos:], Color: pal.Text})
				}
			} else {
				txt := line
				if txt == "" {
					txt = " "
				}
				spans = []ui.Span{{Text: txt, Color: pal.Text}}
			}
			c.Key("lang-preview-line-" + lang.Contrib.ID + "-" + strconv.Itoa(idx))
			ui.RichText(c, spans...).Font("monospace").FontSize(theme.FontSmall).
				FixedLineHeight(theme.LineBody).FontWeight(theme.WeightMono).TextColor(pal.Text)
		}
	})
}

func languagePreviewColor(pal theme.Palette, kind codeed.TokenKind) ui.Color {
	switch kind {
	case codeed.TokenKeyword, codeed.TokenOperator:
		return pal.SyntaxKeyword
	case codeed.TokenType:
		return pal.SyntaxType
	case codeed.TokenFunction, codeed.TokenMeta:
		return pal.SyntaxFunction
	case codeed.TokenProperty, codeed.TokenAttribute, codeed.TokenHeading:
		return pal.SyntaxKey
	case codeed.TokenConstant:
		return pal.SyntaxLiteral
	case codeed.TokenNumber:
		return pal.SyntaxNumber
	case codeed.TokenString, codeed.TokenEscape, codeed.TokenRegexp, codeed.TokenLink, codeed.TokenCode:
		return pal.SyntaxString
	case codeed.TokenTag:
		return pal.SyntaxTag
	case codeed.TokenComment:
		return pal.SyntaxComment
	case codeed.TokenInserted:
		return pal.DiffAddFG
	case codeed.TokenDeleted:
		return pal.DiffDelFG
	case codeed.TokenInvalid:
		return pal.Danger
	default:
		return pal.Text
	}
}
