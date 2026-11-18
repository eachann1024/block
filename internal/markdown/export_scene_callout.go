package markdown

import (
	"block/internal/model"

	"github.com/egoist/mygo/ui"
)

// buildPaintedCallout lays out a > [!KIND] callout in the theme's CalloutStyle.
func buildPaintedCallout(b *model.Block, bx, y, avail float32, pal PaperThemeSpec, legacy bool, ord int) (paintedBlock, float32) {
	cs := pal.Style.Callout
	custom := !legacy && cs.Custom
	tone, label := pal.resolveCallout(b.Callout, legacy)

	boxed := tone.BG.A > 0 || tone.Border.A > 0
	padX, padY := float32(18), float32(12)
	bar := float32(4)
	radius := float32(8)
	borderW := float32(1)
	pos := CalloutLabelAbove
	lfam, lsz, lwt, ltrack := pal.BodyFamily, float32(13), 700, float32(0)
	gap := float32(14)
	if custom {
		if boxed {
			padX, padY = orF(cs.PadX, 16), orF(cs.PadY, 11)
		} else {
			padX, padY = orF(cs.PadX, 0), orF(cs.PadY, 10)
		}
		bar = cs.Bar
		if bar < 0 || boxed {
			bar = 0
		}
		radius = cs.Radius
		borderW = orF(cs.BorderW, 1)
		pos = cs.LabelPos
		lfam = orS(cs.LabelFamily, pal.BodyFamily)
		lsz = orF(cs.LabelSize, 13)
		lwt = orI(cs.LabelWeight, 700)
		ltrack = cs.LabelTrack
		gap = orF(cs.Gap, 14)
	}

	invertNoGlow := tone.BG.A > 160 && colorLuma(tone.FG) < 0.35
	glow := !legacy && pal.Style.TextGlow.A > 0 && !invertNoGlow
	labelSpan := ui.Span{Text: label, Font: lfam, Size: lsz, Weight: lwt, LetterSpacing: ltrack * lsz, NoLetterSpacing: ltrack == 0}
	if glow && !(tone.LabelBG.A > 160 && colorLuma(tone.Label) < 0.35) {
		labelSpan.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
	}

	innerX := bx + padX
	innerW := maxFloat32(avail-2*padX, 60)
	if custom {
		innerX += bar
		innerW = maxFloat32(innerW-bar, 60)
	}
	bodyW := innerW
	bodyX := innerX

	var labelLay *ui.TextLayout
	lw, lh := float32(0), float32(0)
	chipPad := float32(0)
	if tone.LabelBG.A > 0 {
		chipPad = 6
	}
	if pos != CalloutLabelInline {
		lf := ui.Font{Family: lfam, Size: lsz, Weight: lwt, LineHeight: 1.3}
		width := innerW
		if pos == CalloutLabelColumn {
			width = orF(cs.LabelW, 92)
		}
		labelLay = ui.ShapeRichTextWith([]ui.Span{labelSpan}, lf, ui.TextShape{Width: width})
		lw, lh = labelLay.Size()
	}

	weight := 0
	if tone.Bold {
		weight = 600
	}
	bodyFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, Weight: weight, LineHeight: pal.Leading}
	spans := toUISpansWith(b.Spans, pal, legacy, spanStyleOpts{
		family: pal.BodyFamily, size: pal.BodySize, weight: weight, defaultFG: tone.FG, noGlow: invertNoGlow,
	})
	labelX, labelY := innerX+chipPad, y+padY
	bodyY := y + padY
	switch pos {
	case CalloutLabelColumn:
		col := orF(cs.LabelW, 92)
		bodyX = innerX + col + 12
		bodyW = maxFloat32(innerW-col-12, 60)
		// Align the label's cap height with the body's first line.
		labelY = y + padY + maxFloat32((pal.BodySize*pal.Leading-lh)*0.5, 0)
	case CalloutLabelInline:
		ls := labelSpan
		ls.Color = tone.Label
		ls.Size = pal.BodySize
		ls.Text = label + "  "
		spans = append([]ui.Span{ls}, spans...)
	default:
		bodyY = y + padY + lh + 6
	}
	lay := ui.ShapeRichTextWith(spans, bodyFont, ui.TextShape{Width: bodyW})
	_, th := lay.Size()
	bh := (bodyY - y) + th + padY
	if pos == CalloutLabelColumn {
		bh = maxFloat32(bh, lh+2*padY)
	}

	angle := float32(0)
	if custom && cs.Angle != 0 {
		angle = cs.Angle
		if ord%2 == 0 {
			angle = -angle * 0.7
		}
	}
	rule := ui.Color{}
	if custom && !boxed {
		rule = cs.Rule
	}
	_ = lw
	return paintedBlock{
		kind:       model.BlockCallout,
		x:          bx,
		y:          y,
		w:          avail,
		h:          bh,
		textX:      bodyX,
		textY:      bodyY,
		layout:     lay,
		textColor:  tone.FG,
		bgColor:    tone.BG,
		borderCol:  tone.Border,
		borderW:    borderW,
		dashed:     custom && tone.Dashed,
		accentCol:  tone.Label,
		labelLay:   labelLay,
		labelX:     labelX,
		labelY:     labelY,
		labelCol:   tone.Label,
		labelBG:    tone.LabelBG,
		radius:     radius,
		barW:       bar,
		angle:      angle,
		shadowCol:  cs.Shadow,
		ruleCol:    rule,
		topRuleCol: tone.TopRule,
		topRuleW:   tone.TopRuleW,
	}, gap
}

func paintCalloutBlock(p *ui.Painter, b paintedBlock) {
	draw := func() {
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		if b.shadowCol.A > 0 {
			p.Shadow(rect, b.radius, 0, 3, 8, 0, b.shadowCol)
		}
		if b.bgColor.A > 0 {
			p.Fill(rect, b.bgColor, b.radius)
		}
		if b.borderCol.A > 0 && b.borderW > 0 {
			if b.dashed {
				p.StrokeDashed(rect, b.borderCol, b.radius, b.borderW)
			} else {
				p.Stroke(rect, b.borderCol, b.radius, b.borderW)
			}
		}
		if b.topRuleCol.A > 0 {
			tw := orF(b.topRuleW, 1)
			if tw >= 2 {
				p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.w, H: tw}, b.topRuleCol, 0)
			} else {
				p.Line(b.x, b.y+0.5, b.x+b.w, b.y+0.5, tw, b.topRuleCol)
			}
		}
		if b.barW > 0 {
			p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.barW, H: b.h}, b.accentCol, minFloat32(b.barW*0.5, 2))
		}
		if b.ruleCol.A > 0 {
			p.Line(b.x, b.y+b.h-0.5, b.x+b.w, b.y+b.h-0.5, 0.5, b.ruleCol)
		}
		if b.labelLay != nil {
			if b.labelBG.A > 0 {
				lw, lh := b.labelLay.Size()
				p.Fill(ui.Rect{X: b.labelX - 6, Y: b.labelY - 1, W: lw + 12, H: lh + 2}, b.labelBG, 0)
			}
			p.TextLayout(b.labelLay, b.labelX, b.labelY, b.labelCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}
	}
	if b.angle != 0 {
		p.Transform(ui.Rotation(b.angle), b.x+b.w*0.5, b.y+b.h*0.5, draw)
		return
	}
	draw()
}
