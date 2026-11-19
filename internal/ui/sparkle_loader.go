package ui

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// sparkleCycle is one loop of the sparkle loader: a quarter turn with a breath and a twinkle.
const sparkleCycle = 1800 * time.Millisecond

// SparkleLoader draws a looping four-point sparkle for "AI is working" rows. The main
// star eases through a quarter turn while breathing, and a small companion star
// twinkles out of phase. It repaints from the painter's clock without rebuilding the view.
func SparkleLoader(c *ui.Context, size float32, main, accent ui.Color) ui.Element {
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		t := float32(p.Now().UnixMilli()%sparkleCycle.Milliseconds()) / float32(sparkleCycle.Milliseconds())
		s := r.W / 20

		// Main star: eased quarter turn (seamless thanks to 4-fold symmetry) and a breath.
		turn := smooth(t) * (math.Pi / 2)
		breath := 0.5 - 0.5*float32(math.Cos(float64(t)*2*math.Pi))
		big := (7.2 - 1.1*breath) * s
		var star ui.Path
		sparklePath(&star, r.X+9*s, r.Y+11*s, big, turn)
		p.FillPath(&star, main.Alpha(0.75+0.25*(1-breath)))

		// Companion star: twinkles in when the main star is at its smallest.
		tw := float32(math.Sin(float64(t) * math.Pi))
		small := (1.2 + 2.0*tw) * s
		var spark ui.Path
		sparklePath(&spark, r.X+16*s, r.Y+4*s, small, -turn)
		p.FillPath(&spark, accent.Alpha(0.35+0.65*tw))

		p.AnimationFrameRate(60)
	})
}

// sparklePath appends a four-point star with concave sides centered on (cx, cy).
func sparklePath(path *ui.Path, cx, cy, radius float32, angle float32) {
	pt := func(a float64, d float32) (float32, float32) {
		return cx + d*float32(math.Cos(a)), cy + d*float32(math.Sin(a))
	}
	pinch := radius * 0.14
	for i := 0; i < 4; i++ {
		a := float64(angle) + float64(i)*math.Pi/2
		tx, ty := pt(a, radius)
		if i == 0 {
			path.MoveTo(tx, ty)
		}
		mid := a + math.Pi/4
		c1x, c1y := pt(mid, pinch)
		nx, ny := pt(a+math.Pi/2, radius)
		path.CubeTo(c1x, c1y, c1x, c1y, nx, ny)
	}
	path.Close()
}
