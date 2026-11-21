package editor

import (
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

type annotMDKey struct {
	text, muted, accent, codeBG, hairline ui.Color
}

// annotationMarkdown renders an AI answer read-only as Markdown, styled like the AI panel.
func (s *Session) annotationMarkdown(c *ui.Context, text string) {
	pal := theme.Ocean()
	key := annotMDKey{pal.Text, pal.TextSecondary, pal.Accent, pal.CodeBlockBG, pal.Hairline}
	if s.annotMDOpts.Theme == nil || s.annotMDKey != key {
		s.annotMDKey = key
		s.annotMDOpts.Theme = &markdown.Theme{
			FontSize:            theme.FontBody,
			LineHeight:          theme.LineBody / theme.FontBody,
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
