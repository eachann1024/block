package markdown

import (
	"math"
	"sync"

	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

type themeThumbCacheKey struct {
	theme model.ExportTheme
	w     int
	h     int
	epoch uint64
}

type themeThumbCacheEntry struct {
	bm *ui.Bitmap
}

var (
	themeThumbCacheMu sync.RWMutex
	themeThumbCache   = make(map[themeThumbCacheKey]themeThumbCacheEntry)
)

// PaintThemeThumb renders the signature miniature preview of a paper export
// theme inside r (matching designs/export-themes/project/Main.dc.html).
func PaintThemeThumb(p *ui.Painter, r ui.Rect, t model.ExportTheme, radius float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	theme.EnsureBuiltinCodeFonts()
	const thumbScale = float32(2)
	key := themeThumbCacheKey{
		theme: t,
		w:     int(math.Round(float64(r.W * thumbScale))),
		h:     int(math.Round(float64(r.H * thumbScale))),
		epoch: theme.FontEpoch(),
	}
	themeThumbCacheMu.RLock()
	entry, ok := themeThumbCache[key]
	themeThumbCacheMu.RUnlock()
	if !ok {
		wPx := max(int(math.Ceil(float64(r.W))), 1)
		hPx := max(int(math.Ceil(float64(r.H))), 1)
		rgba := ui.Render(func(c *ui.Context) {
			ui.Box(c).Size(float32(wPx), float32(hPx)).Draw(func(tp *ui.Painter, _ ui.Rect) {
				// Render full-bleed into the offscreen cache and clip at blit time so
				// rounded corners never bake the offscreen surface's white background.
				paintThemeThumbUncached(tp, ui.Rect{W: r.W, H: r.H}, t, 0)
			})
		}, wPx, hPx, thumbScale)
		if rgba == nil || rgba.Bounds().Empty() {
			paintThemeThumbUncached(p, r, t, radius)
			return
		}
		entry = themeThumbCacheEntry{bm: ui.NewBitmap(rgba)}
		themeThumbCacheMu.Lock()
		if len(themeThumbCache) >= 64 {
			clear(themeThumbCache)
		}
		themeThumbCache[key] = entry
		themeThumbCacheMu.Unlock()
	}
	p.Clip(r, radius, func() {
		p.ImageRegion(entry.bm, ui.Rect{W: r.W * thumbScale, H: r.H * thumbScale}, r, ui.ImageOptions{})
	})
}

func paintThemeThumbUncached(p *ui.Painter, r ui.Rect, t model.ExportTheme, radius float32) {
	spec := PaperTheme(t)
	p.Clip(r, radius, func() {
		p.Fill(r, spec.Canvas, 0)
		switch spec.Theme {
		case model.ExportThemeJournal:
			grid := rgbHex(0xd3e2f1)
			for x := r.X + 8; x < r.X+r.W; x += 8 {
				p.Line(x, r.Y, x, r.Y+r.H, 1, grid)
			}
			for y := r.Y + 8; y < r.Y+r.H; y += 8 {
				p.Line(r.X, y, r.X+r.W, y, 1, grid)
			}
			card := ui.Rect{X: r.X + 12, Y: r.Y + 11, W: r.W - 22, H: r.H - 16}
			cx, cy := card.X+card.W*0.5, card.Y+card.H*0.5
			p.Transform(ui.Rotation(-2), cx, cy, func() {
				p.Shadow(card, 2, 0, 2, 4, 0, rgbaHex(0x5a3c0a, 0.18))
				p.Fill(card, spec.InnerCanvas, 2)
				paintThumbText(p, "手写便签", fontKai, 13, 400, card.X+8, card.Y+8, spec.Title)
				paintWaveLine(p, card.X+8, card.Y+28, minFloat32(54, card.W-16), 2, 9, 1.4, spec.Accent)
			})
			paintWashiStrip(p, cx+2, card.Y, 32, 9, 3, rgbaHex(0xf0b45a, 0.65))

		case model.ExportThemeRiso:
			p.Fill(ui.Rect{X: r.X + 12, Y: r.Y + 15, W: minFloat32(76, r.W-24), H: 24}, spec.Accent2, 0)
			paintThumbText(p, "孔版", fontSans, 17, 900, r.X+15, r.Y+12, spec.Accent)
			paintThumbText(p, "01", fontMono, 9, 600, r.X+r.W-22, r.Y+r.H-15, spec.Accent)

		case model.ExportThemePress:
			paintThumbCentered(p, "块 报", fontSerif, 14, 900, r.X, r.Y+6, r.W, spec.Title)
			ry := r.Y + 25
			p.Fill(ui.Rect{X: r.X + 10, Y: ry, W: r.W - 20, H: 2}, spec.Title, 0)
			p.Line(r.X+10, ry+4, r.X+r.W-10, ry+4, 0.7, spec.Title)
			colW := (r.W - 28) / 3
			rule := rgbHex(0x8c826e)
			for c := 0; c < 3; c++ {
				cx := r.X + 10 + float32(c)*(colW+4)
				for ly := ry + 9; ly < r.Y+r.H-6; ly += 4 {
					p.Line(cx, ly, cx+colW-2, ly, 1, rule)
				}
				if c < 2 {
					p.Line(cx+colW+1.5, ry+8, cx+colW+1.5, r.Y+r.H-6, 0.6, rule)
				}
			}

		case model.ExportThemePoster:
			f := ui.Font{Family: fontSans, Size: 24, Weight: 900, LineHeight: 1.05}
			lay := ui.ShapeRichTextWith([]ui.Span{
				{Text: "大", Font: fontSans, Size: 24, Weight: 900, Color: spec.Title},
				{Text: "字", Font: fontSans, Size: 24, Weight: 900, Color: spec.Canvas, Background: spec.Accent, PaddingX: 3, PaddingY: 1},
			}, f, ui.TextShape{Width: r.W - 16})
			p.TextLayout(lay, r.X+10, r.Y+10, spec.Title)
			ox, oy := r.X+r.W-8, r.Y+10
			p.Transform(ui.Rotation(90), ox, oy, func() {
				paintThumbText(p, "2026", fontMono, 8, 500, ox, oy, spec.Secondary)
			})

		case model.ExportThemeMist:
			p.FillGradient(r, ui.LinearGradient{
				Angle: 160,
				From:  rgbHex(0xeef1f5),
				To:    rgbHex(0xdde2ea),
			}, 0)
			c1 := ui.Rect{X: r.X + 9, Y: r.Y + 8, W: r.W - 18, H: 24}
			c2 := ui.Rect{X: r.X + 9, Y: r.Y + 36, W: r.W - 18, H: maxFloat32(r.H-44, 12)}
			for _, cr := range []ui.Rect{c1, c2} {
				p.Shadow(cr, 6, 0, 2, 6, 0, rgbaHex(0x1e325a, 0.08))
				p.Fill(cr, spec.InnerCanvas, 6)
				p.Stroke(cr, rgbHex(0xe4e1dc), 6, 1)
			}
			paintThumbText(p, "Mist", fontSerif, 11, 500, c1.X+8, c1.Y+5, spec.Title)

		case model.ExportThemeInk:
			paintThumbText(p, "“", fontSerif, 30, 400, r.X+10, r.Y+2, rgbHex(0x3a3833))
			f := ui.Font{Family: fontSerif, Size: 13, Weight: 400, LineHeight: 1.2}
			lay := ui.ShapeRichTextWith([]ui.Span{{
				Text: "墨 黑", Font: fontSerif, Size: 13, LetterSpacing: 2,
			}}, f, ui.TextShape{Width: r.W - 24})
			p.TextLayout(lay, r.X+14, r.Y+20, spec.Title)
			p.Line(r.X+14, r.Y+40, r.X+14+(r.W-28)*0.36, r.Y+40, 1, spec.Accent)

		case model.ExportThemeKraft:
			grid := rgbaHex(0xd6b05c, 0.12)
			for x := r.X + 10; x < r.X+r.W; x += 10 {
				p.Line(x, r.Y, x, r.Y+r.H, 1, grid)
			}
			for y := r.Y + 10; y < r.Y+r.H; y += 10 {
				p.Line(r.X, y, r.X+r.W, y, 1, grid)
			}
			frame := ui.Rect{X: r.X + 5, Y: r.Y + 5, W: r.W - 10, H: r.H - 10}
			p.Stroke(frame, spec.Accent2, 4, 1)
			paintThumbText(p, "黑卡", fontKai, 14, 400, r.X+12, r.Y+16, spec.Title)
			scx, scy := r.X+r.W-22, r.Y+22
			ring := (&ui.Path{}).Circle(scx, scy, 11)
			p.StrokePath(ring, 1.1, spec.Accent2)
			paintThumbCentered(p, "11·18", fontMono, 6.5, 500, scx-11, scy-4, 22, spec.Accent2)

		case model.ExportThemeNeon:
			p.FillRadialGradient(r, ui.RadialGradient{
				Center: ui.Point{X: 0.8, Y: 0.2},
				Radius: r.W * 0.65,
				Stops: []ui.GradientStop{
					{Color: rgbHex(0x3a1450), At: 0},
					{Color: spec.Canvas.Alpha(0), At: 1},
				},
			}, 0)
			f := ui.Font{Family: fontSans, Size: 15, Weight: 800, LineHeight: 1.2}
			lay := ui.ShapeRichTextWith([]ui.Span{
				{Text: "夜 ", Font: fontSans, Size: 15, Weight: 800, Color: spec.Title},
				{Text: "NEON", Font: fontSans, Size: 15, Weight: 800, Color: spec.Accent, Shadow: ui.NewTextShadow(0, 0, 8, spec.Accent.Alpha(0.75))},
			}, f, ui.TextShape{Width: r.W - 20})
			p.TextLayout(lay, r.X+10, r.Y+11, spec.Title)
			paintThumbText(p, "— SIGNAL —", fontMono, 7.5, 500, r.X+10, r.Y+r.H-16, spec.Secondary)

		case model.ExportThemeFilm:
			hole := rgbHex(0x3a2f26)
			for x := r.X + 5; x+6 <= r.X+r.W-4; x += 12 {
				p.Fill(ui.Rect{X: x, Y: r.Y + 4, W: 6, H: 5}, hole, 1)
				p.Fill(ui.Rect{X: x, Y: r.Y + r.H - 9, W: 6, H: 5}, hole, 1)
			}
			paintThumbText(p, "FRAME", fontMono, 7.5, 500, r.X+10, r.Y+14, spec.Accent)
			paintThumbText(p, "暗房", fontSerif, 14, 600, r.X+10, r.Y+25, spec.Title)

		case model.ExportThemeNight:
			card := ui.Rect{X: r.X + 8, Y: r.Y + 7, W: r.W - 16, H: r.H - 14}
			p.Shadow(card, 8, 0, 3, 8, 0, rgbaHex(0x000000, 0.4))
			p.Fill(card, spec.InnerCanvas, 8)
			paintThumbText(p, "夜读", fontSerif, 13, 600, card.X+9, card.Y+6, spec.Title)
			f := ui.Font{Family: fontSerif, Size: 8.5, Italic: true, LineHeight: 1.2}
			lay := ui.ShapeRichTextWith([]ui.Span{{
				Text: "quiet page", Font: fontSerif, Size: 8.5, Italic: true,
				Color: rgbHex(0xf6e3bf), Background: rgbaHex(0xe8a642, 0.28), Radius: 2, PaddingX: 3,
			}}, f, ui.TextShape{Width: card.W - 14})
			p.TextLayout(lay, card.X+9, card.Y+24, spec.Text)

		case model.ExportThemeTerminal:
			scan := rgbaHex(0x7ee787, 0.07)
			for y := r.Y; y < r.Y+r.H; y += 3 {
				p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 1}, scan, 0)
			}
			frame := ui.Rect{X: r.X + 6, Y: r.Y + 6, W: r.W - 12, H: r.H - 12}
			p.Stroke(frame, spec.Secondary, 2, 1)
			f := ui.Font{Family: fontMono, Size: 11, Weight: 600, LineHeight: 1.2}
			lay := ui.ShapeRichTextWith([]ui.Span{{
				Text: "$ note", Font: fontMono, Size: 11, Weight: 600,
				Shadow: ui.NewTextShadow(0, 0, 6, rgbaHex(0x7ee787, 0.55)),
			}}, f, ui.TextShape{Width: frame.W - 16})
			lw, lh := lay.Size()
			tx, ty := frame.X+8, frame.Y+(frame.H-lh)*0.5
			p.TextLayout(lay, tx, ty, spec.Accent)
			p.Fill(ui.Rect{X: tx + lw + 3, Y: ty + 1, W: 6, H: lh - 2}, spec.Accent, 0)

		default: // Mono
			p.Line(r.X+10, r.Y+8, r.X+10, r.Y+r.H-8, 0.8, spec.Title)
			paintThumbText(p, "书页 Aa", fontSerif, 14, 600, r.X+16, r.Y+9, spec.Title)
			p.Line(r.X+16, r.Y+30, r.X+r.W-12, r.Y+30, 0.6, spec.Title)
			paintThumbCentered(p, "— 12 —", fontSerif, 8.5, 400, r.X+12, r.Y+r.H-16, r.W-20, spec.Secondary)
		}
	})
}

func paintThumbText(p *ui.Painter, s, fam string, size float32, weight int, x, y float32, col ui.Color) {
	f := ui.Font{Family: fam, Size: size, Weight: weight, LineHeight: 1.2}
	lay := ui.ShapeRichTextWith([]ui.Span{{Text: s, Font: fam, Size: size, Weight: weight}}, f, ui.TextShape{Width: 200})
	p.TextLayout(lay, x, y, col)
}

func paintThumbCentered(p *ui.Painter, s, fam string, size float32, weight int, x, y, w float32, col ui.Color) {
	f := ui.Font{Family: fam, Size: size, Weight: weight, LineHeight: 1.2}
	lay := ui.ShapeRichTextWith([]ui.Span{{Text: s, Font: fam, Size: size, Weight: weight}}, f, ui.TextShape{Width: w, Align: ui.Center})
	p.TextLayout(lay, x, y, col)
}
