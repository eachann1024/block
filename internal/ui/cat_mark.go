package ui

import (
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// catMarkCycle is one loop of the cat mark: a glance, a bolt flicker and a blink.
const catMarkCycle = 3600 * time.Millisecond

// catMarkDetailSize is the smallest size that also draws the pupils and the ear bolt.
const catMarkDetailSize float32 = 48

// CatMark draws the Block cat (ears, blue eyes, bolt) on a 24-unit grid and keeps it
// looping on its own: it repaints from the painter's clock without rebuilding the view.
func CatMark(c *ui.Context, size float32, colors theme.CatMarkColors) ui.Element {
	detail := size >= catMarkDetailSize
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		phase := float32(p.Now().UnixMilli()%catMarkCycle.Milliseconds()) / float32(catMarkCycle.Milliseconds())
		s := r.W / 24
		at := func(x, y float32) (float32, float32) { return r.X + x*s, r.Y + y*s }

		var head ui.Path
		head.MoveTo(at(4, 21)).
			CubeTo(xy6(at, 4, 15, 5, 11.5, 6.5, 9.5)).
			LineTo(at(6.2, 3.5)).LineTo(at(10, 7.4)).
			CubeTo(xy6(at, 11.3, 7, 12.7, 7, 14, 7.4)).
			LineTo(at(17.8, 3.5)).LineTo(at(17.5, 9.5)).
			CubeTo(xy6(at, 19, 11.5, 20, 15, 20, 21)).
			Close()
		p.FillPath(&head, colors.Cat)

		if detail {
			var bolt ui.Path
			bolt.MoveTo(at(8, 4.6)).LineTo(at(6.9, 6.9)).LineTo(at(8, 6.9)).
				LineTo(at(7.4, 8.8)).LineTo(at(9, 6.2)).LineTo(at(7.9, 6.2)).Close()
			p.FillPath(&bolt, colors.Bolt.Alpha(catMarkZap(phase)))
		}

		open := catMarkOpen(phase)
		rx, ry := float32(1.7), float32(1.25)
		if detail {
			rx, ry = 1.6, 1.15
		}
		look := catMarkLook(phase)
		for _, cx := range [2]float32{9.3, 14.7} {
			var eye ui.Path
			fillEllipse(&eye, at, cx, 13.2, rx, ry*open)
			p.FillPath(&eye, colors.Eye)
			if detail && open > 0.3 {
				var pupil ui.Path
				fillEllipse(&pupil, at, cx+look, 13.2, 0.35, 0.95*open)
				p.FillPath(&pupil, colors.Pupil)
			}
		}

		if catMarkMoving(phase) {
			p.AnimationFrameRate(60)
		} else {
			p.After(time.Duration((catMarkNextMove(phase) - phase) * float32(catMarkCycle)))
		}
	})
}

// Keyframe windows, as fractions of the cycle.
const (
	lookOutFrom, lookOutTo = 0.40, 0.50
	zapFrom, zapTo         = 0.60, 0.76
	lookBackFrom, blinkTo  = 0.80, 0.94
	blinkFrom, blinkMid    = 0.86, 0.90
)

func catMarkMoving(t float32) bool {
	return (t >= lookOutFrom && t < lookOutTo) || (t >= zapFrom && t < zapTo) || (t >= lookBackFrom && t < blinkTo)
}

func catMarkNextMove(t float32) float32 {
	for _, start := range [3]float32{lookOutFrom, zapFrom, lookBackFrom} {
		if t < start {
			return start
		}
	}
	return 1 + lookOutFrom
}

// catMarkOpen is how open the eyes are: they shut quickly and reopen once a cycle.
func catMarkOpen(t float32) float32 {
	const shut = 0.08
	switch {
	case t >= blinkFrom && t < blinkMid:
		return lerp(1, shut, smooth((t-blinkFrom)/(blinkMid-blinkFrom)))
	case t >= blinkMid && t < blinkTo:
		return lerp(shut, 1, smooth((t-blinkMid)/(blinkTo-blinkMid)))
	}
	return 1
}

// catMarkLook shifts the pupils sideways for a short glance.
func catMarkLook(t float32) float32 {
	const reach = 0.7
	switch {
	case t >= lookOutFrom && t < lookOutTo:
		return reach * smooth((t-lookOutFrom)/(lookOutTo-lookOutFrom))
	case t >= lookOutTo && t < lookBackFrom:
		return reach
	case t >= lookBackFrom && t < 0.90:
		return reach * (1 - smooth((t-lookBackFrom)/(0.90-lookBackFrom)))
	}
	return 0
}

// catMarkZap flickers the ear bolt like a sparking filament.
func catMarkZap(t float32) float32 {
	keys := [...][2]float32{{0.60, 1}, {0.64, 0.25}, {0.68, 1}, {0.72, 0.4}, {0.76, 1}}
	for i := 1; i < len(keys); i++ {
		if t < keys[i][0] && t >= keys[i-1][0] {
			f := (t - keys[i-1][0]) / (keys[i][0] - keys[i-1][0])
			return lerp(keys[i-1][1], keys[i][1], f)
		}
	}
	return 1
}

func fillEllipse(path *ui.Path, at func(x, y float32) (float32, float32), cx, cy, rx, ry float32) {
	const k = 0.5523
	path.MoveTo(at(cx+rx, cy)).
		CubeTo(xy6(at, cx+rx, cy+k*ry, cx+k*rx, cy+ry, cx, cy+ry)).
		CubeTo(xy6(at, cx-k*rx, cy+ry, cx-rx, cy+k*ry, cx-rx, cy)).
		CubeTo(xy6(at, cx-rx, cy-k*ry, cx-k*rx, cy-ry, cx, cy-ry)).
		CubeTo(xy6(at, cx+k*rx, cy-ry, cx+rx, cy-k*ry, cx+rx, cy)).
		Close()
}

func xy6(at func(x, y float32) (float32, float32), x1, y1, x2, y2, x3, y3 float32) (float32, float32, float32, float32, float32, float32) {
	ax, ay := at(x1, y1)
	bx, by := at(x2, y2)
	cx, cy := at(x3, y3)
	return ax, ay, bx, by, cx, cy
}

func smooth(t float32) float32 { return t * t * (3 - 2*t) }

func lerp(a, b, t float32) float32 { return a + (b-a)*t }
