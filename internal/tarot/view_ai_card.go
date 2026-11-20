package tarot

import (
	"math"

	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// RenderAIDevelopingCard draws the 168x288 developing Tarot card ("命运之轮 · 显影中")
// inside the AI chat stream while Tarot Mode is active and AI is generating (or right after).
func RenderAIDevelopingCard(c *ui.Context, generating bool, glintProg float32) {
	tc := theme.Tarot()
	arc := ArcanaByKey("wheel")

	status := theme.Tr("显影中", "Developing")
	if !generating {
		status = arc.Name() + " · " + theme.Tr("已显影", "Developed")
	}

	wrap := ui.Column(c.Key("tarot-ai-developing-card")).FillWidth().AlignItems(ui.Center).
		Padding(theme.Space8, 0, theme.Space12, 0).Gap(theme.Space12)
	blockui.Probe(wrap, "tarot-ai-developing-card")

	wrap.Children(func() {
		card := ui.Box(c.Key("tarot-ai-card-canvas")).Size(168, 288).Shrink(0)
		blockui.Elevate(card, theme.ElevationTarotCard)
		card.Draw(func(p *ui.Painter, r ui.Rect) {
			reduced := p.Preferences().ReduceMotion
			prog := float32(1)
			if generating {
				if reduced {
					prog = 0.68
				} else {
					const cycleMs = 6500.0
					t := float64(p.Now().UnixMilli()%int64(cycleMs)) / cycleMs
					switch {
					case t < 0.72:
						// Smooth ease-in-out reveal from 0 to 1.
						u := t / 0.72
						prog = float32(u * u * (3 - 2*u))
					case t < 0.90:
						prog = 1
					default:
						// Soft fade reset before next loop.
						prog = float32(math.Max(0, 1-(t-0.90)/0.10))
					}
					p.AnimationFrameRate(60)
				}
			} else if glintProg > 0 && glintProg < 1 && !reduced {
				p.AnimationFrameRate(60)
			}
			PaintDevelopingCard(p, r, arc.Key, generating, prog, glintProg, reduced)
		})

		ui.Text(c, status).Font(theme.TarotSerifFont()).FontSize(theme.FontBody).
			TextColor(tc.Gold).SingleLine()
	})
}
