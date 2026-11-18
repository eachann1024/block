package markdown

import (
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"math"

	"block/internal/model"

	"github.com/egoist/mygo/ui"
)

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
