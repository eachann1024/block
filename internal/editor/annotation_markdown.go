package editor

import (
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

type annotMDKey struct {
	text, muted, accent, codeBG, hairline ui.Color
	fontSize, lineHeight                  float32
}

func annotationAnswerTypography(mode annotationLayoutMode) (fontSize, lineHeight float32) {
	switch mode {
	case annotLayoutNarrow:
		return theme.FontSmall, theme.LineBody / theme.FontSmall
	case annotLayoutBelow:
		return theme.FontEditor, theme.LineHeading / theme.FontEditor
	default:
		return theme.FontBody, theme.LineTitle / theme.FontBody
	}
}

// annotationMarkdown renders an AI answer read-only as Markdown, styled like the AI panel.
func (s *Session) annotationMarkdown(c *ui.Context, text string, mode annotationLayoutMode) {
	pal := theme.Ocean()
	fontSize, lineHeight := annotationAnswerTypography(mode)
	key := annotMDKey{pal.Text, pal.TextSecondary, pal.Accent, pal.CodeBlockBG, pal.Hairline, fontSize, lineHeight}
	if s.annotMDOpts.Theme == nil || s.annotMDKey != key {
		s.annotMDKey = key
		s.annotMDOpts.Theme = &markdown.Theme{
			FontSize:            fontSize,
			LineHeight:          lineHeight,
			Text:                pal.Text,
			Muted:               pal.TextSecondary,
			Link:                pal.Accent,
			Strong:              pal.Text,
			StrongWeight:        600,
			Code:                pal.Text,
			CodeBackground:      pal.CodeBlockBG,
			CodeBlockBackground: pal.CodeBlockBG,
			CodeBlockBorder:     pal.Hairline,
			Rule:                pal.Hairline,
			TableBorder:         pal.Hairline,
			TableHeader:         pal.CodeBlockBG,
		}
	}
	svc := c.Services()
	s.annotMDOpts.OnLink = func(url string) {
		if ui.SafeToOpen(url) {
			svc.OpenURL(url)
		}
	}
	ui.Box(c).FillWidth().MinWidth(0).Children(func() {
		markdown.View(c, text, &s.annotMDOpts)
	})
}
