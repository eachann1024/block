package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// shimmerCycle is one sweep of the highlight band across the label and a short rest.
const shimmerCycle = 1600 * time.Millisecond

type shimmerTextKey struct{}

type shimmerTextCache struct {
	text   string
	font   ui.Font
	glyphs []ui.Glyph
	width  float32
}

// ShimmerText draws a single-line label whose glyphs a soft highlight band sweeps
// across, for the live step of an AI run. It repaints from the painter's clock
// without rebuilding the view, and stays still when the system asks for less motion.
func ShimmerText(c *ui.Context, text string, size float32, weight int, base, highlight ui.Color) ui.Element {
	font := ui.Font{Family: c.Theme().Font, Size: size, Weight: weight}
	box := ui.Box(c).Shrink(0)
	cache := ui.Local(box, shimmerTextKey{}, func() shimmerTextCache { return shimmerTextCache{} })
	if cache.text != text || cache.font != font || cache.glyphs == nil {
		cache.text, cache.font = text, font
		cache.glyphs = ui.Shape(text, font)
		cache.width = 0
		for _, g := range cache.glyphs {
			cache.width = max(cache.width, g.X+g.Advance)
		}
	}
	glyphs, width := cache.glyphs, cache.width
	m := font.Metrics()
	lineH := max(size*20/14, m.Ascent+m.Descent)
	return box.Size(width, lineH).Label(text).Draw(func(p *ui.Painter, r ui.Rect) {
		baseline := r.Y + (r.H-(m.Ascent+m.Descent))/2 + m.Ascent
		if p.Preferences().ReduceMotion {
			p.Glyphs(glyphs, r.X, baseline, base)
			return
		}
		band := max(r.W*0.45, size*2.5)
		t := float32(p.Now().UnixMilli()%shimmerCycle.Milliseconds()) / float32(shimmerCycle.Milliseconds())
		center := -band + t*(r.W+band*2)
		for i := range glyphs {
			g := glyphs[i]
			d := (g.X + g.Advance/2 - center) / band
			if d < 0 {
				d = -d
			}
			col := base
			if d < 1 {
				col = base.Mix(highlight, smooth(1-d))
			}
			p.Glyphs(glyphs[i:i+1], r.X, baseline, col)
		}
		p.AnimationFrameRate(60)
	})
}
