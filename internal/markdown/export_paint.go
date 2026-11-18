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

// ---- PNG & PDF Painter (Substrates, Cards, Ornaments, Furniture & Blocks) ----

type substrateTile struct {
	bm     *ui.Bitmap
	dipW   float32
	dipH   float32
	srcW   float32
	srcH   float32
}

// buildSubstrateTiles pre-renders small repeating RGBA tiles only for fine
// pixel-level textures (dots, grain), keeping peak RAM < 2 MB even at 4x scale.
func buildSubstrateTiles(pal PaperThemeSpec, legacy bool) []substrateTile {
	if legacy {
		return nil
	}
	var out []substrateTile
	for idx, layer := range pal.Style.Substrate {
		switch layer.Kind {
		case SubstrateDots:
			gap := maxFloat32(layer.Gap, 3)
			// Choose tile side as an exact integer multiple of gap around ~120 DIP.
			reps := max(int(math.Round(float64(120.0/gap))), 1)
			dip := float32(reps) * gap
			pw := max(int(math.Round(float64(dip*exportScale))), 4)
			img := image.NewRGBA(image.Rect(0, 0, pw, pw))
			col := color.NRGBA{R: layer.Color.R, G: layer.Color.G, B: layer.Color.B, A: layer.Color.A}
			rad := maxFloat32(layer.Size, 0.6)
			for iy := 0; iy < reps; iy++ {
				cy := (float32(iy) + 0.5) * gap
				for ix := 0; ix < reps; ix++ {
					cx := (float32(ix) + 0.5) * gap
					fillDotPremul(img, cx, cy, rad, col)
				}
			}
			out = append(out, substrateTile{
				bm: ui.NewBitmap(img), dipW: dip, dipH: dip, srcW: float32(pw), srcH: float32(pw),
			})

		case SubstrateGrain:
			const dip = float32(180)
			pw := int(dip * exportScale)
			img := image.NewRGBA(image.Rect(0, 0, pw, pw))
			col := color.NRGBA{R: layer.Color.R, G: layer.Color.G, B: layer.Color.B, A: layer.Color.A}
			step := max(int(math.Round(float64(maxFloat32(layer.Gap, 2)*exportScale*0.5))), 2)
			rad := maxFloat32(layer.Size, 0.5)
			seed := uint32(0x517cc1b7) + uint32(idx)*0x9e3779b9
			for py := 0; py < pw; py += step {
				for px := 0; px < pw; px += step {
					hash := (uint32(px)*374761393 + uint32(py)*668265263) ^ seed
					hash = (hash ^ (hash >> 13)) * 1274126177
					if (hash >> 24) < 58 {
						jx := float32(px)/exportScale + float32((hash>>8)&3)*0.25
						jy := float32(py)/exportScale + float32((hash>>12)&3)*0.25
						fillDotPremul(img, jx, jy, rad, col)
					}
				}
			}
			out = append(out, substrateTile{
				bm: ui.NewBitmap(img), dipW: dip, dipH: dip, srcW: float32(pw), srcH: float32(pw),
			})
		}
	}
	return out
}

func fillDotPremul(img *image.RGBA, cx, cy, r float32, col color.NRGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	cx, cy, r = cx*exportScale, cy*exportScale, r*exportScale
	x0 := int(math.Floor(float64(cx - r - 1)))
	x1 := int(math.Ceil(float64(cx + r + 1)))
	y0 := int(math.Floor(float64(cy - r - 1)))
	y1 := int(math.Ceil(float64(cy + r + 1)))
	for py := y0; py <= y1; py++ {
		wy := ((py % h) + h) % h
		dy := float32(py) + 0.5 - cy
		for px := x0; px <= x1; px++ {
			wx := ((px % w) + w) % w
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
			if a == 0 {
				continue
			}
			idx := wy*img.Stride + wx*4
			// Premultiplied source-over into transparent/existing RGBA buffer.
			sr := (uint32(col.R)*a + 127) / 255
			sg := (uint32(col.G)*a + 127) / 255
			sb := (uint32(col.B)*a + 127) / 255
			inv := 255 - a
			img.Pix[idx] = uint8(min(int(sr+(uint32(img.Pix[idx])*inv+127)/255), 255))
			img.Pix[idx+1] = uint8(min(int(sg+(uint32(img.Pix[idx+1])*inv+127)/255), 255))
			img.Pix[idx+2] = uint8(min(int(sb+(uint32(img.Pix[idx+2])*inv+127)/255), 255))
			img.Pix[idx+3] = uint8(min(int(a+(uint32(img.Pix[idx+3])*inv+127)/255), 255))
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
	tiles := buildSubstrateTiles(sc.pal, sc.legacy)

	rgba := ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(sc.width, float32(h)).Draw(func(p *ui.Painter, r ui.Rect) {
			paintSubstrate(p, r, sc.pal, sc.legacy, tiles)
			paintCardsAndSections(p, sc)
			paintOrnaments(p, sc, r)
			if sc.meta.present {
				paintMeta(p, sc.meta, sc.pal)
			}
			if sc.title.present {
				paintExportHeader(p, sc)
			}
			for _, b := range sc.blocks {
				paintSceneBlock(p, b, sc.pal, sc.legacy)
			}
			if sc.footer.present {
				paintFooter(p, sc.footer)
			}
		})
	}, w, h, exportScale)
	return rgba, sc
}

func paintSubstrate(p *ui.Painter, r ui.Rect, pal PaperThemeSpec, legacy bool, tiles []substrateTile) {
	p.Fill(r, pal.Canvas, 0)
	if legacy {
		return
	}
	tileIdx := 0
	for _, layer := range pal.Style.Substrate {
		switch layer.Kind {
		case SubstrateGradient:
			var stops []ui.GradientStop
			if layer.Color3.A > 0 {
				stops = []ui.GradientStop{
					{Color: layer.Color, At: 0},
					{Color: layer.Color2, At: 0.55},
					{Color: layer.Color3, At: 1},
				}
			} else {
				stops = []ui.GradientStop{
					{Color: layer.Color, At: 0},
					{Color: layer.Color2, At: 1},
				}
			}
			p.FillGradient(r, ui.LinearGradient{Angle: layer.Angle, Stops: stops}, 0)

		case SubstrateGlow:
			p.FillRadialGradient(r, ui.RadialGradient{
				Center: layer.At,
				Radius: layer.Radius * r.W,
				Stops: []ui.GradientStop{
					{Color: layer.Color, At: 0},
					{Color: layer.Color.Alpha(0), At: 1},
				},
			}, 0)

		case SubstrateGrid:
			gap := maxFloat32(layer.Gap, 8)
			lw := orF(layer.Size, 1)
			for x := gap; x < r.W; x += gap {
				p.Line(x, 0, x, r.H, lw, layer.Color)
			}
			for y := gap; y < r.H; y += gap {
				p.Line(0, y, r.W, y, lw, layer.Color)
			}

		case SubstrateScanlines:
			gap := maxFloat32(layer.Gap, 3)
			lh := orF(layer.Size, 1)
			for y := float32(0); y < r.H; y += gap {
				p.Fill(ui.Rect{X: 0, Y: y, W: r.W, H: lh}, layer.Color, 0)
			}

		case SubstrateVignette:
			p.FillRadialGradient(r, ui.RadialGradient{
				Center: ui.Point{X: 0.5, Y: 0.5},
				Radius: 0,
				Stops: []ui.GradientStop{
					{Color: layer.Color.Alpha(0), At: 0.55},
					{Color: layer.Color, At: 1},
				},
			}, 0)

		case SubstrateDots, SubstrateGrain:
			if tileIdx < len(tiles) {
				tl := tiles[tileIdx]
				tileIdx++
				p.Clip(r, 0, func() {
					src := ui.Rect{W: tl.srcW, H: tl.srcH}
					for y := float32(0); y < r.H; y += tl.dipH {
						for x := float32(0); x < r.W; x += tl.dipW {
							p.ImageRegion(tl.bm, src, ui.Rect{X: x, Y: y, W: tl.dipW, H: tl.dipH}, ui.ImageOptions{})
						}
					}
				})
			}
		}
	}
}

func paintCardsAndSections(p *ui.Painter, sc exportScene) {
	pal := sc.pal
	if len(sc.sections) > 0 && pal.Style.Sections != nil {
		sec := pal.Style.Sections
		for _, sr := range sc.sections {
			if sec.Shadow.A > 0 {
				p.Shadow(sr, sec.Radius, 0, 8, 24, 0, sec.Shadow)
			}
			p.Fill(sr, sec.Fill, sec.Radius)
			if sec.Border.A > 0 {
				p.Stroke(sr, sec.Border, sec.Radius, 1)
			}
		}
		return
	}
	if pal.Card == nil || sc.cardRect.W <= 0 || sc.cardRect.H <= 0 {
		return
	}
	card := pal.Card
	cr := sc.cardRect
	if card.Kind == CardFill {
		if card.Shadow.A > 0 {
			p.Shadow(cr, card.Radius, 0, orF(card.ShadowY, 6), orF(card.Blur, 16), 0, card.Shadow)
		}
		fill := orC(card.Fill, pal.InnerCanvas)
		p.Fill(cr, fill, card.Radius)
	}
	if card.HasBorder {
		bw := orF(card.BorderW, 1)
		p.Stroke(cr, card.Border, card.Radius, bw)
	}
	if card.TopEdge.A > 0 {
		in := maxFloat32(card.Radius*0.6, 2)
		p.Line(cr.X+in, cr.Y+0.5, cr.X+cr.W-in, cr.Y+0.5, 1, card.TopEdge)
	}
	if sc.cardLabelLay != nil {
		lw, lh := sc.cardLabelLay.Size()
		lx := cr.X + 18
		ly := cr.Y - lh*0.5
		p.Fill(ui.Rect{X: lx - 6, Y: ly - 1, W: lw + 12, H: lh + 2}, pal.Canvas, 2)
		p.TextLayout(sc.cardLabelLay, lx, ly, orC(card.LabelColor, card.Border))
	}
}

func paintOrnaments(p *ui.Painter, sc exportScene, pageRect ui.Rect) {
	pal := sc.pal
	for _, os := range sc.ornaments {
		o := os.spec
		switch o.Kind {
		case OrnamentEdgeRule:
			x := o.X
			if x < 0 {
				x = pageRect.W + x
			}
			w := orF(o.W, 1)
			y0, y1 := float32(0), pageRect.H
			if w < 1 {
				y0 = pal.PadTop
				y1 = maxFloat32(pageRect.H-pal.PadBottom, y0+20)
			}
			p.Line(x, y0, x, y1, w, o.Color)

		case OrnamentSprockets:
			pitch := maxFloat32(o.Y, 32)
			sw, sh := orF(o.W, 20), orF(o.H, 16)
			startY := (pitch - sh) * 0.5
			rx := pageRect.W - o.X - sw
			for y := startY; y+sh <= pageRect.H-4; y += pitch {
				p.Fill(ui.Rect{X: o.X, Y: y, W: sw, H: sh}, o.Color, 2.5)
				p.Fill(ui.Rect{X: rx, Y: y, W: sw, H: sh}, o.Color, 2.5)
			}

		case OrnamentSideText:
			if os.textLay != nil {
				ox := pageRect.W - o.X
				oy := o.Y
				p.Transform(ui.Rotation(90), ox, oy, func() {
					p.TextLayout(os.textLay, ox, oy, o.Color)
				})
			}

		case OrnamentTape:
			tw, th := orF(o.W, 120), orF(o.H, 30)
			cx := pageRect.W*0.5 + o.X
			cy := o.Y + th*0.5
			paintWashiStrip(p, cx, cy, tw, th, o.Angle, o.Color)

		case OrnamentStamp:
			if os.textLay != nil {
				lw, lh := os.textLay.Size()
				sx := pageRect.W - o.X - lw
				sy := o.Y
				cx, cy := sx+lw*0.5, sy+lh*0.5
				p.Transform(ui.Rotation(o.Angle), cx, cy, func() {
					p.TextLayout(os.textLay, sx, sy, o.Color)
				})
			}

		case OrnamentSeal:
			d := orF(o.W, 88)
			inset := float32(0)
			if pal.Card != nil {
				inset = pal.Card.Inset
			}
			sx := pageRect.W - inset - o.X - d
			sy := inset + o.X + o.Y
			cx, cy := sx+d*0.5, sy+d*0.5
			p.Transform(ui.Rotation(o.Angle), cx, cy, func() {
				outer := (&ui.Path{}).Circle(cx, cy, d*0.5)
				inner := (&ui.Path{}).Circle(cx, cy, d*0.5-5)
				p.StrokePath(outer, 1.5, o.Color)
				p.StrokePath(inner, 0.6, o.Color)
				if os.textLay != nil {
					_, h1 := os.textLay.Size()
					h2 := float32(0)
					if os.text2Lay != nil {
						_, h2 = os.text2Lay.Size()
					}
					totalH := h1
					if h2 > 0 {
						totalH += 4 + h2
					}
					ty := cy - totalH*0.5
					p.TextLayout(os.textLay, sx, ty, o.Color)
					if os.text2Lay != nil {
						p.TextLayout(os.text2Lay, sx, ty+h1+4, o.Color)
					}
				}
			})
		}
	}
}

func paintWashiStrip(p *ui.Painter, cx, cy, w, h, angle float32, col ui.Color) {
	p.Transform(ui.Rotation(angle), cx, cy, func() {
		x0, y0 := cx-w*0.5, cy-h*0.5
		// Serrated torn-paper silhouette path.
		path := &ui.Path{}
		teeth := 4
		step := h / float32(teeth)
		notch := float32(3.5)
		path.MoveTo(x0, y0)
		path.LineTo(x0+w, y0)
		for i := 0; i < teeth; i++ {
			ty := y0 + float32(i)*step
			path.LineTo(x0+w-notch, ty+step*0.5)
			path.LineTo(x0+w, ty+step)
		}
		path.LineTo(x0, y0+h)
		for i := teeth; i > 0; i-- {
			ty := y0 + float32(i)*step
			path.LineTo(x0+notch, ty-step*0.5)
			path.LineTo(x0, ty-step)
		}
		path.Close()
		p.FillPath(path, col)
		stripe := ui.Color{R: 255, G: 255, B: 255, A: 68}
		for sx := x0 + 6; sx+5 < x0+w-4; sx += 12 {
			p.Fill(ui.Rect{X: sx, Y: y0 + 1, W: 5, H: h - 2}, stripe, 0)
		}
	})
}

func paintMeta(p *ui.Painter, m metaScene, pal PaperThemeSpec) {
	leftX := m.x
	if m.moon {
		// Draw crisp crescent moon icon before meta text (Night).
		cy := m.y + 9
		cx := m.x + 7
		moonPath := &ui.Path{}
		moonPath.MoveTo(cx+4.5, cy-5.5)
		moonPath.CubeTo(cx-2.5, cy-6.5, cx-6.5, cy+1.5, cx-1.5, cy+5.8)
		moonPath.CubeTo(cx+2.2, cy+8.2, cx+6.5, cy+5.5, cx+6.8, cy+2.2)
		moonPath.CubeTo(cx+2.2, cy+3.8, cx-0.8, cy-0.5, cx+4.5, cy-5.5)
		moonPath.Close().Cap(ui.RoundCap).Join(ui.RoundJoin)
		p.StrokePath(moonPath, 1.5, pal.Accent)
		leftX += 22
	}

	if m.mastLay != nil {
		p.TextLayout(m.mastLay, m.x, m.y, m.mastCol)
	}
	if m.leftLay != nil {
		lw, lh := m.leftLay.Size()
		if m.earBoxes {
			by := m.y + maxFloat32(m.h-lh-26, 0)
			box := ui.Rect{X: leftX, Y: by, W: lw + 16, H: lh + 8}
			p.Stroke(box, m.ruleCol, 0, 1)
			p.TextLayout(m.leftLay, leftX+8, by+4, m.leftCol)
		} else {
			p.TextLayout(m.leftLay, leftX, m.y, m.leftCol)
		}
	}
	if m.rightLay != nil {
		rw, rh := m.rightLay.Size()
		if m.earBoxes {
			by := m.y + maxFloat32(m.h-rh-26, 0)
			box := ui.Rect{X: m.x + m.w - rw - 16, Y: by, W: rw + 16, H: rh + 8}
			p.Stroke(box, m.ruleCol, 0, 1)
			p.TextLayout(m.rightLay, box.X+8, by+4, m.rightCol)
		} else {
			p.TextLayout(m.rightLay, m.x+m.w-rw, m.y, m.rightCol)
		}
	}

	switch m.rule {
	case MetaRuleSingle:
		p.Line(m.x, m.ruleY, m.x+m.w, m.ruleY, 1, m.ruleCol)
	case MetaRuleDouble:
		p.Fill(ui.Rect{X: m.x, Y: m.ruleY, W: m.w, H: 3}, m.ruleCol, 0)
		p.Fill(ui.Rect{X: m.x, Y: m.ruleY + 6, W: m.w, H: 1}, m.ruleCol, 0)
	}
}

func paintExportHeader(p *ui.Painter, sc exportScene) {
	pal := sc.pal
	t := sc.title
	x, w, hy, th := t.x, t.w, t.y, t.h
	titleCol := t.color
	if sc.legacy {
		p.TextLayout(t.layout, x, hy, titleCol)
		p.Fill(ui.Rect{X: x, Y: hy + th + 14, W: w, H: 2}, pal.Accent, 1)
		return
	}

	ruleCol := orC(t.ruleCol, pal.Accent)
	switch t.mark {
	case TitleMarkRule:
		rw := w * orF(t.ruleWidth, 1)
		rh := orF(t.ruleThick, 2)
		rc := orC(t.ruleCol, pal.Title)
		p.Fill(ui.Rect{X: x, Y: hy + th + 20, W: rw, H: rh}, rc, 0)

	case TitleMarkBar:
		p.Fill(ui.Rect{X: x, Y: hy + th + 18, W: 48, H: 4}, ruleCol, 2)

	case TitleMarkWavy:
		ww := w * orF(t.ruleWidth, 0.42)
		if ww < 96 {
			ww = 96
		}
		paintWaveLine(p, x+1, hy+th+14, ww, 3.8, 15, 2.2, ruleCol)

	case TitleMarkDoubleRule:
		dc := orC(t.ruleCol, pal.Divider)
		p.Fill(ui.Rect{X: x, Y: hy + th + 18, W: w, H: 3}, dc, 0)
		p.Fill(ui.Rect{X: x, Y: hy + th + 24, W: w, H: 1}, dc, 0)

	case TitleMarkRing:
		wy := hy + th + 22
		ring := (&ui.Path{}).Circle(x+5, wy, 4.5)
		p.StrokePath(ring, 1.2, ruleCol)
		p.Line(x+18, wy, x+w, wy, 1, ruleCol)

	case TitleMarkOffset:
		// Flat spot-ink block offset behind the title (Risograph overprint).
		maxLW := float32(0)
		for _, ln := range t.layout.Lines() {
			if ln.Rect.W > maxLW {
				maxLW = ln.Rect.W
			}
		}
		bw := minFloat32(maxFloat32(maxLW+18, w*0.45), w)
		p.Fill(ui.Rect{X: x + 6, Y: hy + 10, W: bw, H: maxFloat32(th-4, 24)}, ruleCol, 0)

	case TitleMarkBand:
		p.Fill(ui.Rect{X: 0, Y: 0, W: sc.width, H: hy + th + 44}, pal.Accent, 0)
		titleCol = pal.Accent2

	case TitleMarkHighlight:
		ink := ui.Color{R: ruleCol.R, G: ruleCol.G, B: ruleCol.B, A: 105}
		for _, ln := range t.layout.Lines() {
			lineH := orF(ln.Rect.H, t.fontSize*1.3)
			p.Fill(ui.Rect{X: x + ln.Rect.X - 4, Y: hy + ln.Rect.Y + lineH*0.5, W: ln.Rect.W + 8, H: lineH * 0.38}, ink, 2)
		}

	case TitleMarkCursor:
		lines := t.layout.Lines()
		if len(lines) > 0 {
			last := lines[len(lines)-1]
			lineH := orF(last.Rect.H, t.fontSize*1.3)
			cw := float32(math.Round(float64(t.fontSize * 0.52)))
			cx := minFloat32(x+last.Rect.X+last.Rect.W+8, x+w-cw)
			p.Fill(ui.Rect{X: cx, Y: hy + last.Rect.Y + lineH*0.16, W: cw, H: lineH * 0.7}, pal.Accent, 0)
		}
	}
	p.TextLayout(t.layout, x, hy, titleCol)
}

func paintWaveLine(p *ui.Painter, x, midY, width, amp, period, strokeW float32, col ui.Color) {
	steps := max(int(math.Round(float64(width/period))), 2)
	seg := width / float32(steps)
	path := &ui.Path{}
	path.MoveTo(x, midY)
	for i := 0; i < steps; i++ {
		x0 := x + float32(i)*seg
		crest := midY - amp
		if i%2 == 1 {
			crest = midY + amp
		}
		path.QuadTo(x0+seg*0.5, crest, x0+seg, midY)
	}
	path.Cap(ui.RoundCap)
	p.StrokePath(path, strokeW, col)
}

func paintFooter(p *ui.Painter, f footerScene) {
	switch f.rule {
	case FooterLine:
		p.Line(f.x, f.y, f.x+f.w, f.y, 1, f.ruleCol)
	case FooterDashed:
		drawStyledLine(p, f.x, f.y, f.x+f.w, f.y, 1.3, LineDashed, f.ruleCol)
	case FooterFlank:
		if f.centerLay != nil {
			cw, ch := f.centerLay.Size()
			cx := f.x + (f.w-cw)*0.5
			midY := f.textY + ch*0.5
			if cx-f.x > 20 {
				drawStyledLine(p, f.x, midY, cx-14, midY, 1.2, LineDotted, f.ruleCol)
			}
			rx := cx + cw + 14
			if f.x+f.w-rx > 20 {
				drawStyledLine(p, rx, midY, f.x+f.w, midY, 1.2, LineDotted, f.ruleCol)
			}
			p.TextLayout(f.centerLay, cx, f.textY, f.color)
		}
	}

	leftEnd := f.x
	if f.leftLay != nil {
		lw, lh := f.leftLay.Size()
		p.TextLayout(f.leftLay, f.x, f.textY, f.color)
		leftEnd = f.x + lw + 10
		if len(f.swatches) > 0 {
			sy := f.textY + maxFloat32((lh-12)*0.5, 0)
			for i, sw := range f.swatches {
				p.Fill(ui.Rect{X: leftEnd + float32(i)*16, Y: sy, W: 12, H: 12}, sw, 0)
			}
		}
	}
	if f.centerLay != nil && f.rule != FooterFlank {
		p.TextLayout(f.centerLay, f.x, f.textY, f.color)
	}
	if f.rightLay != nil {
		rw, _ := f.rightLay.Size()
		p.TextLayout(f.rightLay, f.x+f.w-rw, f.textY, f.rightColor)
	}
}

func drawStyledLine(p *ui.Painter, x0, y0, x1, y1, width float32, style LineStyle, col ui.Color) {
	if width <= 0 || col.A == 0 {
		return
	}
	switch style {
	case LineDashed:
		path := &ui.Path{}
		path.MoveTo(x0, y0).LineTo(x1, y1).Dash(0, maxFloat32(width*4, 5), maxFloat32(width*3, 4))
		p.StrokePath(path, width, col)
	case LineDotted:
		path := &ui.Path{}
		path.MoveTo(x0, y0).LineTo(x1, y1).Cap(ui.RoundCap).Dash(0, 0.2, maxFloat32(width*3.2, 4))
		p.StrokePath(path, width, col)
	default:
		p.Line(x0, y0, x1, y1, width, col)
	}
}

func paintSceneBlock(p *ui.Painter, b paintedBlock, pal PaperThemeSpec, legacy bool) {
	radius := pal.BlockRadius()
	switch {
	case b.kind == model.BlockDivider:
		if !legacy && b.divKind == DividerText && b.layout != nil {
			p.TextLayout(b.layout, b.x, b.y+4, b.borderCol)
		} else if !legacy && b.divKind == DividerRepeat && b.layout != nil {
			p.Clip(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, 0, func() {
				p.TextLayout(b.layout, b.x, b.y+2, b.borderCol)
			})
		} else {
			midY := b.y + b.h*0.5
			lw := orF(b.divWidth, 1)
			if lw >= 2 {
				p.Fill(ui.Rect{X: b.x, Y: midY - lw*0.5, W: b.w, H: lw}, b.borderCol, 0)
			} else {
				p.Line(b.x, midY, b.x+b.w, midY, lw, b.borderCol)
			}
		}

	case b.kind == model.BlockImage:
		if b.img != nil {
			p.ImageRegion(b.img.bitmap, b.img.src, b.img.dst, ui.ImageOptions{})
		} else if b.layout != nil {
			p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, b.bgColor, 8)
			p.Stroke(ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}, b.borderCol, 8, 1)
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case b.kind == model.BlockTable && b.table != nil:
		paintTableBlock(p, b)

	case b.kind == model.BlockCode || b.kind == model.BlockRawHTML:
		paintCodeBlock(p, b, radius)

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
		paintQuoteBlock(p, b, radius)

	case b.kind.IsList():
		paintListBlock(p, b, pal, legacy)

	case b.kind.IsHeading():
		paintHeadingBlock(p, b, pal, legacy)

	default:
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}
	}
}

func paintHeadingBlock(p *ui.Painter, b paintedBlock, pal PaperThemeSpec, legacy bool) {
	if !legacy && b.kind == model.BlockHeading2 {
		lineH := pal.HeadSizes[1] * 1.35
		switch b.headPrefix {
		case HeadPrefixDot:
			p.Fill(ui.Rect{X: b.x, Y: b.textY + lineH*0.5 - 6, W: 12, H: 12}, b.accentCol, 6)
		case HeadPrefixTag:
			if b.labelLay != nil {
				lw, lh := b.labelLay.Size()
				tagRect := ui.Rect{X: b.labelX, Y: b.labelY, W: lw + 12, H: lh + 4}
				p.Fill(tagRect, b.labelBG, 0)
				p.TextLayout(b.labelLay, b.labelX+6, b.labelY+2, b.labelCol)
			}
			if b.layout != nil {
				tw, _ := b.layout.Size()
				rx := b.textX + tw + 14
				if b.x+b.w-rx > 16 {
					p.Fill(ui.Rect{X: rx, Y: b.textY + lineH*0.5 - 2, W: b.x + b.w - rx, H: 4}, b.headRuleCol, 0)
				}
			}
		case HeadPrefixLabel:
			if b.labelLay != nil {
				lw, lh := b.labelLay.Size()
				p.TextLayout(b.labelLay, b.labelX, b.labelY, b.labelCol)
				rx := b.labelX + lw + 12
				if b.x+b.w-rx > 16 {
					p.FillGradient(ui.Rect{X: rx, Y: b.labelY + lh*0.5, W: b.x + b.w - rx, H: 1}, ui.LinearGradient{
						Angle: 90,
						From:  b.headRuleCol.Alpha(0.6),
						To:    b.headRuleCol.Alpha(0),
					}, 0)
				}
			}
		case HeadPrefixNumber:
			if b.labelLay != nil {
				p.TextLayout(b.labelLay, b.labelX, b.labelY, b.labelCol)
			}
		}
		if b.headRule {
			p.Line(b.x, b.y+b.h-1, b.x+b.w, b.y+b.h-1, 1, b.headRuleCol)
		}
	}
	if b.layout != nil {
		p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
	}
}

func paintCodeBlock(p *ui.Painter, b paintedBlock, radius float32) {
	switch b.codeKind {
	case CodePlain:
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case CodeFenced:
		p.Fill(ui.Rect{X: b.x, Y: b.y, W: 2, H: b.h}, b.borderCol, 0)
		if b.labelLay != nil {
			p.TextLayout(b.labelLay, b.labelX, b.labelY, b.labelCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
			if b.label2Lay != nil {
				_, th := b.layout.Size()
				p.TextLayout(b.label2Lay, b.labelX, b.textY+th+2, b.labelCol)
			}
		}

	default: // CodeBox
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		if b.bgColor.A > 0 {
			p.Fill(rect, b.bgColor, radius)
		}
		if b.borderW > 0 && b.borderCol.A > 0 {
			if b.dashed {
				p.StrokeDashed(rect, b.borderCol, radius, b.borderW)
			} else {
				p.Stroke(rect, b.borderCol, radius, b.borderW)
			}
		}
		if b.labelLay != nil {
			p.TextLayout(b.labelLay, b.labelX, b.labelY, b.labelCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}
	}
}

func paintQuoteBlock(p *ui.Painter, b paintedBlock, radius float32) {
	switch b.quoteKind {
	case QuoteIndent, QuotePrefix:
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case QuoteSticky:
		noteRect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h - 8}
		cx, cy := noteRect.X+noteRect.W*0.5, noteRect.Y+noteRect.H*0.5
		p.Transform(ui.Rotation(b.quoteAngle), cx, cy, func() {
			p.Shadow(noteRect, 2, 0, 6, 14, 0, rgbaHex(0x6e5014, 0.16))
			p.Fill(noteRect, b.bgColor, 2)
			if b.layout != nil {
				p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
			}
		})

	case QuoteOffset:
		box := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h - 6}
		off := ui.Rect{X: b.x + 6, Y: b.y + 6, W: b.w, H: b.h - 6}
		p.Fill(box, b.bgColor, 0)
		p.Stroke(off, b.accentCol, 0, 2)
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case QuotePull:
		p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.w, H: 3}, b.accentCol, 0)
		p.Fill(ui.Rect{X: b.x, Y: b.y + b.h - 1, W: b.w, H: 1}, b.accentCol, 0)
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case QuoteBigMark:
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		if b.quoteCard {
			if b.bgColor.A > 0 {
				p.Fill(rect, b.bgColor, 14)
			}
			if b.borderCol.A > 0 {
				p.Stroke(rect, b.borderCol, 14, 1)
			}
			if b.label2Lay != nil {
				p.TextLayout(b.label2Lay, b.x+20, b.y+10, b.accentCol)
			}
		} else if b.label2Lay != nil {
			p.TextLayout(b.label2Lay, b.x-8, b.y-18, b.accentCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	case QuoteFramed:
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		p.Stroke(rect, b.accentCol, 0, 1)
		if b.labelLay != nil {
			lw, lh := b.labelLay.Size()
			lx := b.x + 12
			ly := b.y - lh*0.5
			p.Fill(ui.Rect{X: lx - 6, Y: ly - 1, W: lw + 12, H: lh + 2}, b.labelBG, 0)
			p.TextLayout(b.labelLay, lx, ly, b.labelCol)
		}
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}

	default: // QuoteBar
		rect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
		if b.bgColor.A > 0 {
			p.Fill(rect, b.bgColor, radius)
		}
		barW := orF(b.quoteBarW, 4)
		barRect := ui.Rect{X: b.x, Y: b.y, W: barW, H: b.h}
		if b.glowCol.A > 0 {
			p.Shadow(barRect, barW*0.5, 0, 0, 12, 1, b.glowCol)
		}
		p.Fill(barRect, b.accentCol, barW*0.5)
		if b.layout != nil {
			p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
		}
	}
}

func paintListBlock(p *ui.Painter, b paintedBlock, pal PaperThemeSpec, legacy bool) {
	lineH := pal.BodySize * pal.Leading
	cy := b.y + lineH*0.5

	switch b.kind {
	case model.BlockTodo:
		if !legacy && b.todoKind == TodoText {
			if b.markerLay != nil {
				p.TextLayout(b.markerLay, b.x, b.y, b.markerCol)
			}
		} else if !legacy && b.todoKind == TodoRing {
			r := float32(5.5)
			cx := b.x + r + 1
			ring := (&ui.Path{}).Circle(cx, cy, r)
			p.StrokePath(ring, 1.3, b.todoBorder)
			if b.checked {
				p.Fill(ui.Rect{X: cx - 3.2, Y: cy - 3.2, W: 6.4, H: 6.4}, b.todoCheck, 3.2)
			}
		} else if legacy {
			if b.checked {
				p.Fill(ui.Rect{X: b.x, Y: cy - 7.5, W: 15, H: 15}, b.accentCol, 3.5)
				paintCheckmark(p, b.x+3.5, cy, pal.InnerCanvas, 1.8)
			} else {
				p.Stroke(ui.Rect{X: b.x + 0.75, Y: cy - 6.75, W: 13.5, H: 13.5}, b.accentCol, 3, 1.5)
			}
		} else {
			p.Stroke(ui.Rect{X: b.x + 0.75, Y: cy - 6.75, W: 13.5, H: 13.5}, b.todoBorder, 3, 1.5)
			if b.checked {
				paintCheckmark(p, b.x+3.2, cy, b.todoCheck, 2.0)
			}
		}

	case model.BlockNumbered:
		if b.markerLay != nil {
			p.TextLayout(b.markerLay, b.x, b.y, b.markerCol)
		}

	default: // BlockBullet
		if b.markerLay != nil {
			p.TextLayout(b.markerLay, b.x, b.y, b.markerCol)
		} else if legacy {
			p.Fill(ui.Rect{X: b.x + 2, Y: cy - 3.2, W: 6.4, H: 6.4}, b.accentCol, 3.2)
		} else {
			switch b.bulletKind {
			case BulletSquare:
				sz := orF(b.bulletSize, 7)
				p.Fill(ui.Rect{X: b.x + 2, Y: cy - sz*0.5, W: sz, H: sz}, b.accentCol, 0)
			case BulletRing:
				sz := orF(b.bulletSize, 8.5)
				ring := (&ui.Path{}).Circle(b.x+2+sz*0.5, cy, sz*0.5)
				p.StrokePath(ring, 1.3, b.accentCol)
			case BulletDash:
				p.Fill(ui.Rect{X: b.x + 1, Y: cy - 0.8, W: 8, H: 1.6}, b.accentCol, 0.8)
			default: // BulletDot
				sz := orF(b.bulletSize, 6.4)
				p.Fill(ui.Rect{X: b.x + 2, Y: cy - sz*0.5, W: sz, H: sz}, b.accentCol, sz*0.5)
			}
		}
	}
	if b.layout != nil {
		p.TextLayout(b.layout, b.textX, b.textY, b.textColor)
	}
}

func paintCheckmark(p *ui.Painter, x0, cy float32, col ui.Color, w float32) {
	chk := &ui.Path{}
	chk.MoveTo(x0, cy)
	chk.LineTo(x0+3.0, cy+3.2)
	chk.LineTo(x0+8.5, cy-3.5)
	chk.Cap(ui.RoundCap).Join(ui.RoundJoin)
	p.StrokePath(chk, w, col)
}

func paintTableBlock(p *ui.Painter, b paintedBlock) {
	pt := b.table
	ts := pt.style
	if !ts.Rules {
		for _, row := range pt.rows {
			for _, cell := range row.cells {
				rect := ui.Rect{X: cell.x, Y: row.y, W: cell.w, H: row.h}
				if cell.bgColor.A > 0 {
					p.Fill(rect, cell.bgColor, 0)
				}
				p.Stroke(rect, pt.ruleCol, 0, 1)
				if cell.layout != nil {
					p.TextLayout(cell.layout, cell.x+pt.padX, row.y+pt.padY, cell.textColor)
				}
			}
		}
		return
	}

	outerRect := ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h}
	paintInner := func() {
		for _, row := range pt.rows {
			for _, cell := range row.cells {
				if cell.bgColor.A > 0 {
					p.Fill(ui.Rect{X: cell.x, Y: row.y, W: cell.w, H: row.h}, cell.bgColor, 0)
				}
				if cell.layout != nil {
					p.TextLayout(cell.layout, cell.x+pt.padX, row.y+pt.padY, cell.textColor)
				}
			}
		}

		// Column separators.
		if ts.ColSep > 0 && len(pt.colWidths) > 1 {
			cx := b.x
			for i := 0; i < len(pt.colWidths)-1; i++ {
				cx += pt.colWidths[i]
				drawStyledLine(p, cx, b.y, cx, b.y+b.h, ts.ColSep, ts.ColSepLine, pt.ruleCol)
			}
		}

		// Top rule (only when Outer == 0 so we do not double-stroke top border).
		if ts.Top > 0 && ts.Outer == 0 {
			if ts.Top >= 2 {
				p.Fill(ui.Rect{X: b.x, Y: b.y, W: b.w, H: ts.Top}, pt.strongCol, 0)
			} else {
				p.Line(b.x, b.y, b.x+b.w, b.y, ts.Top, pt.strongCol)
			}
		}

		// Row separators.
		for rIdx := 0; rIdx < len(pt.rows)-1; rIdx++ {
			ry := pt.rows[rIdx].y + pt.rows[rIdx].h
			if rIdx == 0 && !pt.noHeader {
				if ts.HeadSep > 0 {
					drawStyledLine(p, b.x, ry, b.x+b.w, ry, ts.HeadSep, ts.HeadSepLine, pt.strongCol)
				}
			} else if ts.RowSep > 0 {
				drawStyledLine(p, b.x, ry, b.x+b.w, ry, ts.RowSep, ts.RowSepLine, pt.ruleCol)
			}
		}

		// When NoHeaderRow is true and Top == 0 and Outer == 0 (e.g. Ink / Night border-top on every row),
		// draw a subtle top line if RowSep > 0 so the first row matches the artboard.
		if pt.noHeader && ts.Top == 0 && ts.Outer == 0 && ts.RowSep > 0 {
			drawStyledLine(p, b.x, b.y, b.x+b.w, b.y, ts.RowSep, ts.RowSepLine, pt.ruleCol)
		}

		// Bottom rule.
		if ts.Bottom > 0 && ts.Outer == 0 {
			by := b.y + b.h
			if ts.Bottom >= 2 {
				p.Fill(ui.Rect{X: b.x, Y: by - ts.Bottom, W: b.w, H: ts.Bottom}, pt.strongCol, 0)
			} else {
				p.Line(b.x, by, b.x+b.w, by, ts.Bottom, pt.strongCol)
			}
		}
	}

	if ts.Outer > 0 {
		p.Clip(outerRect, ts.OuterRadius, paintInner)
		p.Stroke(outerRect, pt.strongCol, ts.OuterRadius, ts.Outer)
	} else {
		paintInner()
	}
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
