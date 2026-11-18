package markdown

import (
	"fmt"
	"strings"

	"block/internal/model"

	"github.com/egoist/mygo/ui"
)

// buildPaintedImage lays out an image (or its placeholder) inside the theme's
// figure frame, with the alt text as a caption when the theme asks for one.
func buildPaintedImage(b *model.Block, notePath string, bx, by, avail float32, pal PaperThemeSpec, legacy bool, ord int) (paintedBlock, bool) {
	is := pal.Style.Image
	if legacy || (is.Frame == ImageFrameNone && !is.Caption && is.Shadow.A == 0) {
		return buildRawImage(b, notePath, bx, by, avail, pal)
	}

	pad := float32(0)
	switch is.Frame {
	case ImageFrameMat, ImageFramePolaroid:
		pad = orF(is.Pad, 10)
	}
	topOff := float32(0)
	if is.Tape.A > 0 {
		topOff = 12
	}
	innerAvail := maxFloat32(avail-2*pad, 80)
	if is.Angle != 0 && is.Frame == ImageFramePolaroid {
		innerAvail = maxFloat32(innerAvail*0.72, 80)
	}
	pb, ok := buildRawImage(b, notePath, bx+pad, by+topOff+pad, innerAvail, pal)
	if !ok {
		return pb, false
	}

	// The picture's own rect.
	picX, picW := pb.x, pb.w
	if pb.img != nil {
		picX, picW = pb.img.dst.X, pb.img.dst.W
	}
	if is.Angle != 0 && is.Frame == ImageFramePolaroid {
		// Centre the smaller tilted print in the measure.
		shift := bx + (avail-picW-2*pad)/2 - (picX - pad)
		picX += shift
		pb.x += shift
		pb.textX += shift
		if pb.img != nil {
			pb.img.dst.X += shift
		}
	}

	// Caption.
	var capLay *ui.TextLayout
	capH := float32(0)
	alt := strings.TrimSpace(b.ImageAlt)
	if is.Caption && alt != "" && pb.img != nil {
		text := alt
		if is.CaptionPrefix != "" {
			text = strings.ReplaceAll(is.CaptionPrefix, "{n}", fmt.Sprintf("%02d", max(ord, 1))) + alt
		}
		fam := orS(is.CaptionFamily, pal.BodyFamily)
		sz := orF(is.CaptionSize, 12)
		align := ui.Start
		if is.CaptionCenter {
			align = ui.Center
		}
		sp := ui.Span{Text: text, Font: fam, Size: sz, Italic: is.CaptionItalic, LetterSpacing: is.CaptionTrack * sz}
		if pal.Style.TextGlow.A > 0 {
			sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
		}
		capLay = ui.ShapeRichTextWith([]ui.Span{sp}, ui.Font{Family: fam, Size: sz, LineHeight: 1.45}, ui.TextShape{Width: picW, Align: align})
		_, capH = capLay.Size()
	}

	picH := pb.h
	frame := ui.Rect{X: picX - pad, Y: by + topOff, W: picW + 2*pad, H: picH + 2*pad}
	capY := frame.Y + frame.H + 8
	if is.Frame == ImageFramePolaroid {
		bottom := float32(14)
		if capLay != nil {
			bottom = capH + 18
			capY = frame.Y + pad + picH + 9
		}
		frame.H = pad + picH + bottom
	}
	total := frame.Y + frame.H - by
	if is.Offset > 0 {
		total += is.Offset
	}
	if capLay != nil && is.Frame != ImageFramePolaroid {
		capY = by + total + 8
		total += 8 + capH
	}
	if is.Angle != 0 {
		total += 8
	}

	outerR := orF(is.Radius, pal.Radius)
	imgR := is.InnerRadius
	if imgR == 0 && (is.Frame == ImageFrameNone || is.Frame == ImageFrameBorder) && is.Offset == 0 {
		imgR = outerR
	}

	pb.y = by
	pb.h = total
	pb.frame = frame
	pb.frameKind = is.Frame
	pb.angle = is.Angle
	pb.shadowCol = is.Shadow
	pb.tapeCol = is.Tape
	pb.radius = outerR
	pb.imgRadius = imgR
	pb.imgOffset = is.Offset
	pb.imgCorners = is.Corners
	pb.labelLay = capLay
	pb.labelX = picX
	pb.labelY = capY
	pb.labelCol = orC(is.CaptionColor, pal.Secondary)
	if is.Frame == ImageFrameMat || is.Frame == ImageFramePolaroid {
		pb.labelBG = orC(is.BG, pal.InnerCanvas)
	}
	pb.ruleCol = is.Border
	pb.borderW = orF(is.BorderW, 1)
	return pb, true
}

// paintFigure draws an image block built by buildPaintedImage.
func paintFigure(p *ui.Painter, b paintedBlock) {
	draw := func() {
		f := b.frame
		if b.imgOffset > 0 && b.ruleCol.A > 0 {
			off := ui.Rect{X: f.X + b.imgOffset, Y: f.Y + b.imgOffset, W: f.W, H: f.H}
			p.Stroke(off, b.ruleCol, 0, b.borderW)
		} else if b.frameKind != ImageFrameNone || b.shadowCol.A > 0 {
			r := b.radius
			if b.frameKind == ImageFramePolaroid {
				r = 2
			}
			if b.shadowCol.A > 0 {
				p.Shadow(f, r, 0, 6, 16, 0, b.shadowCol)
			}
			if b.labelBG.A > 0 {
				p.Fill(f, b.labelBG, r)
			}
			if b.ruleCol.A > 0 && b.frameKind != ImageFrameNone && b.frameKind != ImageFrameBorder {
				p.Stroke(f, b.ruleCol, r, b.borderW)
			}
		}
		if b.img != nil {
			if b.imgRadius > 0.5 {
				p.Clip(b.img.dst, b.imgRadius, func() {
					p.ImageRegion(b.img.bitmap, b.img.src, b.img.dst, ui.ImageOptions{})
				})
			} else {
				p.ImageRegion(b.img.bitmap, b.img.src, b.img.dst, ui.ImageOptions{})
			}
		} else if b.layout != nil {
			_, th := b.layout.Size()
			r := ui.Rect{X: b.textX - 12, Y: b.textY - 10, W: b.w, H: th + 20}
			p.Fill(r, b.bgColor, 8)
			p.Stroke(r, b.borderCol, 8, 1)
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}
		if b.imgOffset == 0 && b.frameKind == ImageFrameBorder && b.ruleCol.A > 0 {
			p.Stroke(f, b.ruleCol, b.radius, b.borderW)
		}
		if b.imgCorners && b.ruleCol.A > 0 {
			arm := float32(22)
			tl := (&ui.Path{}).MoveTo(f.X-6, f.Y-6+arm).LineTo(f.X-6, f.Y-6).LineTo(f.X-6+arm, f.Y-6)
			br := (&ui.Path{}).MoveTo(f.X+f.W+6-arm, f.Y+f.H+6).LineTo(f.X+f.W+6, f.Y+f.H+6).LineTo(f.X+f.W+6, f.Y+f.H+6-arm)
			p.StrokePath(tl, 2, b.ruleCol)
			p.StrokePath(br, 2, b.ruleCol)
		}
		if b.tapeCol.A > 0 {
			paintWashiStrip(p, f.X+f.W*0.5, f.Y, 88, 24, -4, b.tapeCol)
		}
		if b.labelLay != nil {
			p.TextLayout(b.labelLay, b.labelX, b.labelY, b.labelCol)
		}
	}
	if b.angle != 0 {
		f := b.frame
		p.Transform(ui.Rotation(b.angle), f.X+f.W*0.5, f.Y+f.H*0.5, draw)
		return
	}
	draw()
}
