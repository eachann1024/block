package editor

import (
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type emptyHintSpec struct {
	Text     string
	MenuKeys bool
	Armed    bool
}

func (s *Session) emptyHintFor(b *model.Block, deleteArmed bool) *emptyHintSpec {
	if b == nil || b.PlainText() != "" || b.Kind == model.BlockTable || b.Kind == model.BlockImage ||
		b.Kind == model.BlockDivider || b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML {
		return nil
	}
	switch b.Kind {
	case model.BlockParagraph:
		if deleteArmed {
			return &emptyHintSpec{
				Text:  theme.Tr("再次按删除键删除此行", "Press Backspace again to delete this line"),
				Armed: true,
			}
		}
		return &emptyHintSpec{
			Text:     theme.Tr("开始输入…", "Start typing…"),
			MenuKeys: true,
		}
	case model.BlockHeading1:
		return &emptyHintSpec{Text: theme.Tr("标题 1", "Heading 1")}
	case model.BlockHeading2:
		return &emptyHintSpec{Text: theme.Tr("标题 2", "Heading 2")}
	case model.BlockHeading3:
		return &emptyHintSpec{Text: theme.Tr("标题 3", "Heading 3")}
	case model.BlockHeading4:
		return &emptyHintSpec{Text: theme.Tr("标题 4", "Heading 4")}
	case model.BlockHeading5:
		return &emptyHintSpec{Text: theme.Tr("标题 5", "Heading 5")}
	case model.BlockHeading6:
		return &emptyHintSpec{Text: theme.Tr("标题 6", "Heading 6")}
	default:
		return nil
	}
}

func (s *Session) paintEmptyHintBand(wrap ui.Element, hint *emptyHintSpec, fontSize, lh float32) {
	if hint == nil || hint.Armed {
		return
	}
	pal := theme.Ocean()
	linePx := fontSize * lh
	wrap.Draw(func(p *ui.Painter, r ui.Rect) {
		bandH := linePx + 4
		band := ui.Rect{
			X: r.X - 8,
			Y: r.Y + (linePx-bandH)*0.5,
			W: r.W + 16,
			H: bandH,
		}
		p.Fill(band, pal.HintBand, 6)
		appUi.RecordBounds("empty-line-band", band)
	})
}

func (s *Session) renderEmptyHintRow(c *ui.Context, hint *emptyHintSpec, fontSize, lh float32) {
	if hint == nil {
		return
	}
	pal := theme.Ocean()
	hintCol := ui.Hex("#9b9a95")
	if theme.IsDark() {
		hintCol = ui.Hex("#7a7975")
	}
	if hint.Armed {
		hintCol = pal.Danger
	}
	linePx := fontSize * lh
	hintRow := ui.Row(c.Key("empty-hint-row")).Absolute().Top(0).Left(8).Right(0).Height(linePx).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).IgnoreInput(true)
	hintRow.Children(func() {
		txt := ui.Text(c, hint.Text).FontSize(fontSize).TextColor(hintCol).SingleLine()
		if edFont := resolvedEditorFont(); edFont != "" {
			txt.Font(edFont)
		}
		availBody := s.viewportWidth - s.leftContentGutter() - s.rightContentInset()
		if availBody <= 0 {
			availBody = theme.EditorStandardWidth
		}
		if hint.MenuKeys && min(availBody, theme.EditorStandardWidth) >= 220 {
			keys := ui.Row(c.Key("empty-hint-menu-keys")).AlignItems(ui.Center).Gap(6)
			keys.Children(func() {
				if s.minimal {
					appUi.Keycap(c, "/")
				} else {
					for _, capLbl := range []string{"/", "、"} {
						appUi.Keycap(c, capLbl)
					}
					ui.Text(c, theme.Tr("菜单", "Menu")).FontSize(theme.FontSmall).TextColor(hintCol).SingleLine()
				}
			})
			appUi.Probe(keys, "empty-hint-menu-keys")
		}
	})
}
