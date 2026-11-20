package tarot

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

func init() {
	RegisterArt(midnightArt{})
}

type midnightArt struct{}

type arcanaPaths struct {
	tone *ui.Path
	fill *ui.Path
	gilt *ui.Path
	ink  *ui.Path
	gold *ui.Path
}

type artAssets struct {
	// Card back (viewBox 0 0 100 172)
	latticeLines *ui.Path
	latticeDots  *ui.Path
	backShield   *ui.Path
	backDash     *ui.Path
	backThin     *ui.Path
	backLine     *ui.Path
	backGilt     *ui.Path

	// Card frame (viewBox 0 0 100 172)
	laidH     *ui.Path
	laidV     *ui.Path
	frameLine *ui.Path
	frameThin *ui.Path
	frameGilt *ui.Path

	// Slot placeholder (viewBox 0 0 100 172)
	slotSolid *ui.Path
	slotRing  *ui.Path

	// Ornaments & compass
	corner      *ui.Path // viewBox 0 0 34 34
	fleuron     *ui.Path // viewBox 0 0 84 10
	compassLine *ui.Path // viewBox 0 0 120 120
	compassGilt *ui.Path // viewBox 0 0 120 120
	eyeOutline  *ui.Path // viewBox 0 0 44 28
	eyePupil    *ui.Path // viewBox 0 0 44 28

	// Star chart (viewBox 0 0 800 800)
	chartMain    *ui.Path
	chartFine    *ui.Path
	chartDash    *ui.Path
	chartCross   *ui.Path
	chartEllipse *ui.Path
	chartEllDash *ui.Path
	chartStars   *ui.Path

	// Moon phase ring (viewBox 0 0 760 760)
	phaseRing *ui.Path
	phaseDash *ui.Path
	phaseFill *ui.Path

	// Small icons (viewBox 0 0 24 24)
	icons [8]*ui.Path

	// 22 Major Arcana glyphs (viewBox 0 0 100 140)
	glyphs map[string]arcanaPaths
}

var (
	assetsOnce sync.Once
	assets     artAssets
)

func getAssets() *artAssets {
	assetsOnce.Do(initAssets)
	return &assets
}

const artCardScale = 2.0

func parseOptPath(d string, capStyle ui.LineCap, joinStyle ui.LineJoin) *ui.Path {
	return parseOptPathScaled(d, 1, capStyle, joinStyle)
}

func parseOptPathScaled(d string, scale float64, capStyle ui.LineCap, joinStyle ui.LineJoin) *ui.Path {
	if strings.TrimSpace(d) == "" {
		return nil
	}
	if scale != 1 {
		d = scaleSVGPathD(d, scale)
	}
	p, err := ui.ParsePath(d)
	if err != nil || p == nil {
		return nil
	}
	p.Cap(capStyle).Join(joinStyle)
	return p
}

func parseFillPath(d string, rule ui.FillRule) *ui.Path {
	return parseFillPathScaled(d, 1, rule)
}

func parseFillPathScaled(d string, scale float64, rule ui.FillRule) *ui.Path {
	if strings.TrimSpace(d) == "" {
		return nil
	}
	if scale != 1 {
		d = scaleSVGPathD(d, scale)
	}
	p, err := ui.ParsePath(d)
	if err != nil || p == nil {
		return nil
	}
	p.FillRule(rule)
	return p
}

// scaleSVGPathD scales all coordinate/radius parameters in an SVG path string by k,
// preserving arc rotation and flag arguments, so small viewBox artwork rasterizes
// cleanly at high DIP resolution without upscaling blur.
func scaleSVGPathD(d string, k float64) string {
	var b strings.Builder
	b.Grow(len(d) + len(d)/4)
	n := len(d)
	i := 0
	var cmd byte
	argIdx := 0
	for i < n {
		ch := d[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ',' {
			i++
			continue
		}
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			cmd = ch
			argIdx = 0
			b.WriteByte(ch)
			i++
			continue
		}
		start := i
		if ch == '+' || ch == '-' {
			i++
		}
		for i < n && d[i] >= '0' && d[i] <= '9' {
			i++
		}
		if i < n && d[i] == '.' {
			i++
			for i < n && d[i] >= '0' && d[i] <= '9' {
				i++
			}
		}
		if i < n && (d[i] == 'e' || d[i] == 'E') {
			i++
			if i < n && (d[i] == '+' || d[i] == '-') {
				i++
			}
			for i < n && d[i] >= '0' && d[i] <= '9' {
				i++
			}
		}
		if i == start {
			i++
			continue
		}
		tok := d[start:i]
		if argIdx > 0 {
			b.WriteByte(' ')
		}
		scaleThis := true
		if cmd == 'A' || cmd == 'a' {
			mod := argIdx % 7
			if mod == 2 || mod == 3 || mod == 4 {
				scaleThis = false
			}
		}
		if scaleThis {
			if v, err := strconv.ParseFloat(tok, 64); err == nil {
				b.WriteString(tgF(v * k))
			} else {
				b.WriteString(tok)
			}
		} else {
			b.WriteString(tok)
		}
		argIdx++
	}
	return b.String()
}

// ===== TG-LIB Copperplate Pen Primitives =====

type tgPt [2]float64
type tgPoly []tgPt

func tgRound(n float64) float64 {
	v := math.Round(n*100) / 100
	if math.Abs(v) < 1e-6 {
		return 0
	}
	return v
}

func tgF(n float64) string {
	return strconv.FormatFloat(tgRound(n), 'f', -1, 64)
}

func tgP(x, y float64) string {
	return tgF(x) + " " + tgF(y)
}

func tgCirc(cx, cy, r float64) string {
	return "M" + tgP(cx-r, cy) +
		"a" + tgF(r) + " " + tgF(r) + " 0 1 0 " + tgF(2*r) + " 0" +
		"a" + tgF(r) + " " + tgF(r) + " 0 1 0 " + tgF(-2*r) + " 0"
}

func tgEll(cx, cy, rx, ry float64) string {
	return "M" + tgP(cx-rx, cy) +
		"a" + tgF(rx) + " " + tgF(ry) + " 0 1 0 " + tgF(2*rx) + " 0" +
		"a" + tgF(rx) + " " + tgF(ry) + " 0 1 0 " + tgF(-2*rx) + " 0"
}

func tgLine(pts ...tgPt) string {
	var b strings.Builder
	for i, pt := range pts {
		if i == 0 {
			b.WriteByte('M')
		} else {
			b.WriteByte('L')
		}
		b.WriteString(tgP(pt[0], pt[1]))
	}
	return b.String()
}

func tgShape(pts tgPoly) string {
	return tgLine(pts...) + "Z"
}

func tgStar(cx, cy, R, r float64, n int, rot float64) string {
	var b strings.Builder
	for i := 0; i < n*2; i++ {
		a := -math.Pi/2 + rot + float64(i)*math.Pi/float64(n)
		q := R
		if i%2 == 1 {
			q = r
		}
		if i == 0 {
			b.WriteByte('M')
		} else {
			b.WriteByte('L')
		}
		b.WriteString(tgP(cx+q*math.Cos(a), cy+q*math.Sin(a)))
	}
	b.WriteByte('Z')
	return b.String()
}

func tgRays(cx, cy, r1, r2 float64, n int, off float64) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		a := -math.Pi/2 + off + float64(i)*2*math.Pi/float64(n)
		b.WriteByte('M')
		b.WriteString(tgP(cx+r1*math.Cos(a), cy+r1*math.Sin(a)))
		b.WriteByte('L')
		b.WriteString(tgP(cx+r2*math.Cos(a), cy+r2*math.Sin(a)))
	}
	return b.String()
}

func tgCres(cx, cy, r, k float64, right bool) string {
	s1, s2 := "1", "0"
	if right {
		s1, s2 = "0", "1"
	}
	return "M" + tgP(cx, cy-r) +
		"A" + tgF(r) + " " + tgF(r) + " 0 1 " + s1 + " " + tgP(cx, cy+r) +
		"A" + tgF(r*k) + " " + tgF(r) + " 0 1 " + s2 + " " + tgP(cx, cy-r) + "Z"
}

func tgWave(x1, x2, y, a float64, n int) string {
	w := (x2 - x1) / float64(n)
	var b strings.Builder
	b.WriteByte('M')
	b.WriteString(tgP(x1, y))
	for i := 0; i < n; i++ {
		amp := -a
		if i%2 == 1 {
			amp = a
		}
		b.WriteByte('q')
		b.WriteString(tgF(w / 2))
		b.WriteByte(' ')
		b.WriteString(tgF(amp))
		b.WriteByte(' ')
		b.WriteString(tgF(w))
		b.WriteString(" 0")
	}
	return b.String()
}

func tgDrop(x, y float64) string {
	return "M" + tgP(x, y) + "q-2.2 3.4 0 4.8q2.2-1.4 0-4.8Z"
}

func tgNeedle(cx, cy, r1, r2 float64, n int, w, off float64) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		a := -math.Pi/2 + off + float64(i)*2*math.Pi/float64(n)
		b.WriteByte('M')
		b.WriteString(tgP(cx+r1*math.Cos(a-w), cy+r1*math.Sin(a-w)))
		b.WriteByte('L')
		b.WriteString(tgP(cx+r2*math.Cos(a), cy+r2*math.Sin(a)))
		b.WriteByte('L')
		b.WriteString(tgP(cx+r1*math.Cos(a+w), cy+r1*math.Sin(a+w)))
		b.WriteByte('Z')
	}
	return b.String()
}

func tgSwell(x1, y1, x2, y2, w float64) string {
	mx := (x1 + x2) / 2
	my := (y1 + y2) / 2
	l := math.Hypot(x2-x1, y2-y1)
	if l == 0 {
		l = 1
	}
	nx := (y1 - y2) / l * w
	ny := (x2 - x1) / l * w
	return "M" + tgP(x1, y1) +
		"Q" + tgP(mx+nx, my+ny) + " " + tgP(x2, y2) +
		"Q" + tgP(mx-nx, my-ny) + " " + tgP(x1, y1) + "Z"
}

func tgBox(x1, y1, x2, y2 float64) tgPoly {
	return tgPoly{{x1, y1}, {x2, y1}, {x2, y2}, {x1, y2}}
}

func tgOval(cx, cy, rx, ry float64, n int) tgPoly {
	if n <= 0 {
		n = 32
	}
	p := make(tgPoly, 0, n)
	for i := 0; i < n; i++ {
		a := float64(i)*2*math.Pi/float64(n) + 0.01
		p = append(p, tgPt{cx + rx*math.Cos(a), cy + ry*math.Sin(a)})
	}
	return p
}

func tgSector(cx, cy, r1, r2, a1, a2 float64) tgPoly {
	p := make(tgPoly, 0, 18)
	for i := 0; i <= 8; i++ {
		a := (a1 + (a2-a1)*float64(i)/8.0) * math.Pi / 180.0
		p = append(p, tgPt{cx + r2*math.Cos(a), cy + r2*math.Sin(a)})
	}
	for j := 8; j >= 0; j-- {
		b := (a1 + (a2-a1)*float64(j)/8.0) * math.Pi / 180.0
		p = append(p, tgPt{cx + r1*math.Cos(b), cy + r1*math.Sin(b)})
	}
	return p
}

func tgClip(polys []tgPoly, x1, y1, x2, y2 float64) string {
	dx := x2 - x1
	dy := y2 - y1
	var ts []float64
	for _, p := range polys {
		for i := 0; i < len(p); i++ {
			A := p[i]
			B := p[(i+1)%len(p)]
			ex := B[0] - A[0]
			ey := B[1] - A[1]
			den := dx*ey - dy*ex
			if math.Abs(den) < 1e-9 {
				continue
			}
			t := ((A[0]-x1)*ey - (A[1]-y1)*ex) / den
			u := ((A[0]-x1)*dy - (A[1]-y1)*dx) / den
			if u >= 0 && u < 1 {
				ts = append(ts, t)
			}
		}
	}
	// Insertion sort (ts is small, typically 2..8 elements)
	for i := 1; i < len(ts); i++ {
		v := ts[i]
		j := i - 1
		for j >= 0 && ts[j] > v {
			ts[j+1] = ts[j]
			j--
		}
		ts[j+1] = v
	}
	var b strings.Builder
	for k := 0; k+1 < len(ts); k += 2 {
		a := math.Max(0, ts[k])
		c := math.Min(1, ts[k+1])
		if c-a > 1e-3 {
			b.WriteByte('M')
			b.WriteString(tgP(x1+dx*a, y1+dy*a))
			b.WriteByte('L')
			b.WriteString(tgP(x1+dx*c, y1+dy*c))
		}
	}
	return b.String()
}

func tgHatch(polys []tgPoly, deg, gap, ph float64) string {
	a := deg * math.Pi / 180.0
	c := math.Cos(a)
	s := math.Sin(a)
	lo, hi := 1e9, -1e9
	ul, uh := 1e9, -1e9
	for _, p := range polys {
		for _, q := range p {
			v := q[1]*c - q[0]*s
			u := q[0]*c + q[1]*s
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
			if u < ul {
				ul = u
			}
			if u > uh {
				uh = u
			}
		}
	}
	var b strings.Builder
	start := math.Floor(lo/gap)*gap + ph + gap/2
	for v := start; v < hi; v += gap {
		b.WriteString(tgClip(polys, (ul-1)*c-v*s, (ul-1)*s+v*c, (uh+1)*c-v*s, (uh+1)*s+v*c))
	}
	return b.String()
}

func tgBurst(polys []tgPoly, cx, cy float64, n int, off float64) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		a := off + float64(i)*2*math.Pi/float64(n)
		b.WriteString(tgClip(polys, cx, cy, cx+240*math.Cos(a), cy+240*math.Sin(a)))
	}
	return b.String()
}

func tgInside(polys []tgPoly, x, y float64) bool {
	k := 0
	for _, p := range polys {
		for i := 0; i < len(p); i++ {
			A := p[i]
			B := p[(i+1)%len(p)]
			if (A[1] > y) != (B[1] > y) && x < A[0]+(y-A[1])*(B[0]-A[0])/(B[1]-A[1]) {
				k++
			}
		}
	}
	return k%2 == 1
}

func tgStip(polys []tgPoly, n int, r float64, seed int64) string {
	s := seed
	x1, x2 := 1e9, -1e9
	y1, y2 := 1e9, -1e9
	for _, p := range polys {
		for _, q := range p {
			if q[0] < x1 {
				x1 = q[0]
			}
			if q[0] > x2 {
				x2 = q[0]
			}
			if q[1] < y1 {
				y1 = q[1]
			}
			if q[1] > y2 {
				y2 = q[1]
			}
		}
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		s = (s * 16807) % 2147483647
		rnd1 := float64(s) / 2147483647.0
		s = (s * 16807) % 2147483647
		rnd2 := float64(s) / 2147483647.0
		x := x1 + rnd1*(x2-x1)
		y := y1 + rnd2*(y2-y1)
		if tgInside(polys, x, y) {
			b.WriteString(tgCirc(x, y, r))
		}
	}
	return b.String()
}

func tgQ4(g func(X, Y func(float64) float64) string) string {
	signs := [4][2]float64{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}}
	var b strings.Builder
	for _, s := range signs {
		sx, sy := s[0], s[1]
		b.WriteString(g(
			func(x float64) float64 { return 50 + sx*(x-50) },
			func(y float64) float64 { return 86 + sy*(y-86) },
		))
	}
	return b.String()
}

// ===== Asset Precompilation =====

func initAssets() {
	const pi = math.Pi

	// 1. Card back lattice pattern (tiled across x:10.5..89.5, y:10.5..161.5)
	var latLines, latDots strings.Builder
	for col := 1; col <= 15; col++ {
		x := 2.0 + float64(col)*6.0
		for row := 0; row <= 16; row++ {
			y := 3.44 + float64(row)*10.32
			latLines.WriteString(fmt.Sprintf("M%sL%sL%sL%sZ",
				tgP(x+3, y), tgP(x+6, y+5.16), tgP(x+3, y+10.32), tgP(x, y+5.16)))
			latDots.WriteString(tgCirc(x+3, y+5.16, 0.32))
		}
	}
	assets.latticeLines = parseOptPathScaled(latLines.String(), artCardScale, ui.RoundCap, ui.RoundJoin)
	assets.latticeDots = parseFillPathScaled(latDots.String(), artCardScale, ui.NonZero)
	assets.backShield = parseFillPathScaled("M50 20L85 86L50 152L15 86Z", artCardScale, ui.NonZero)

	assets.backDash = parseOptPathScaled("M8.4 8.4H91.6V163.6H8.4Z", artCardScale, ui.RoundCap, ui.RoundJoin)
	if assets.backDash != nil {
		assets.backDash.Dash(0, 0, 2.6*artCardScale)
	}

	fan := tgQ4(func(X, Y func(float64) float64) string {
		var b strings.Builder
		for k := 1; k < 8; k++ {
			a := float64(k) * pi / 16.0
			b.WriteByte('M')
			b.WriteString(tgP(X(10.5+3.2*math.Cos(a)), Y(10.5+3.2*math.Sin(a))))
			b.WriteByte('L')
			b.WriteString(tgP(X(10.5+9.2*math.Cos(a)), Y(10.5+9.2*math.Sin(a))))
		}
		for k := 0; k <= 6; k++ {
			a := float64(k) * pi / 12.0
			if k == 0 {
				b.WriteByte('M')
			} else {
				b.WriteByte('L')
			}
			b.WriteString(tgP(X(10.5+12*math.Cos(a)), Y(10.5+12*math.Sin(a))))
		}
		return b.String()
	})
	var beads strings.Builder
	for i := 0; i < 24; i++ {
		a := float64(i) * pi / 12.0
		beads.WriteString(tgCirc(50+24.45*math.Cos(a), 86+24.45*math.Sin(a), 0.5))
	}

	backLineD := "M8.6 4.6H91.4A4 4 0 0 1 95.4 8.6V163.4A4 4 0 0 1 91.4 167.4H8.6A4 4 0 0 1 4.6 163.4V8.6A4 4 0 0 1 8.6 4.6Z" +
		"M10.5 10.5H89.5V161.5H10.5ZM50 20L85 86L50 152L15 86Z" +
		tgCirc(50, 86, 27) + tgCirc(50, 86, 20) + tgStar(50, 86, 18.2, 7.2, 8, 0) +
		"M10.5 20.5A10 10 0 0 0 20.5 10.5M79.5 10.5A10 10 0 0 0 89.5 20.5M89.5 151.5A10 10 0 0 0 79.5 161.5M20.5 161.5A10 10 0 0 0 10.5 151.5" +
		"M50 47.5V57.5M50 114.5V124.5"
	backThinD := "M6.3 6.3H93.7V165.7H6.3ZM11.8 11.8H88.2V160.2H11.8ZM50 24.6L82.55 86L50 147.4L17.45 86Z" +
		tgCirc(50, 86, 25.4) + tgCirc(50, 86, 23.5) +
		tgRays(50, 86, 20, 22, 72, 0) +
		tgRays(50, 86, 3.4, 18.2, 8, 0) +
		tgRays(50, 86, 3.4, 7.2, 8, pi/8) +
		fan
	backGiltD := tgStar(50, 86, 10.6, 4.2, 8, pi/8) +
		tgCirc(50, 86, 3.4) + tgCirc(50, 86, 1.3) +
		beads.String() +
		tgCres(50, 39.5, 5.6, 0.5, true) + tgCres(50, 132.5, 5.6, 0.5, false) +
		tgStar(50, 28.6, 1.8, 0.7, 4, 0) + tgStar(50, 143.4, 1.8, 0.7, 4, 0) +
		tgStar(19.6, 86, 1.8, 0.7, 4, 0) + tgStar(80.4, 86, 1.8, 0.7, 4, 0) +
		tgStar(50, 52.5, 1.5, 1, 2, 0) + tgStar(50, 119.5, 1.5, 1, 2, 0) +
		tgQ4(func(X, Y func(float64) float64) string {
			return tgStar(X(27), Y(40), 3, 1.1, 4, 0) + tgStar(X(20), Y(61), 2, 0.75, 4, 0) + tgCirc(X(34), Y(29.5), 0.7)
		})

	assets.backLine = parseOptPathScaled(backLineD, artCardScale, ui.RoundCap, ui.RoundJoin)
	assets.backThin = parseOptPathScaled(backThinD, artCardScale, ui.RoundCap, ui.RoundJoin)
	assets.backGilt = parseFillPathScaled(backGiltD, artCardScale, ui.EvenOdd)

	// 2. Card frame & laid paper texture
	var laidH, laidV strings.Builder
	for y := 0.75; y < 172; y += 1.5 {
		laidH.WriteString("M0 ")
		laidH.WriteString(tgF(y))
		laidH.WriteString("H100")
	}
	for x := 4.5; x < 100; x += 9.0 {
		laidV.WriteString("M")
		laidV.WriteString(tgF(x))
		laidV.WriteString(" 0V172")
	}
	assets.laidH = parseOptPathScaled(laidH.String(), artCardScale, ui.RoundCap, ui.RoundJoin)
	assets.laidV = parseOptPathScaled(laidV.String(), artCardScale, ui.RoundCap, ui.RoundJoin)

	frameLineD := "M8.5 4.5H91.5A4 4 0 0 0 95.5 8.5V163.5A4 4 0 0 0 91.5 167.5H8.5A4 4 0 0 0 4.5 163.5V8.5A4 4 0 0 0 8.5 4.5Z"
	frameThinD := "M10.09 6.4H89.91A5.9 5.9 0 0 0 93.6 10.09V161.91A5.9 5.9 0 0 0 89.91 165.6H10.09A5.9 5.9 0 0 0 6.4 161.91V10.09A5.9 5.9 0 0 0 10.09 6.4Z"
	frameGiltD := tgQ4(func(X, Y func(float64) float64) string {
		return tgCirc(X(5.7), Y(5.7), 0.7) + tgStar(X(10.9), Y(10.9), 1.8, 0.62, 4, 0)
	}) +
		tgStar(50, 5.45, 1.45, 2.1, 2, 0) + tgStar(50, 166.55, 1.45, 2.1, 2, 0) +
		tgStar(5.45, 86, 2.1, 1.45, 2, 0) + tgStar(94.55, 86, 2.1, 1.45, 2, 0) +
		tgCirc(45.6, 5.45, 0.5) + tgCirc(54.4, 5.45, 0.5) +
		tgCirc(45.6, 166.55, 0.5) + tgCirc(54.4, 166.55, 0.5)

	assets.frameLine = parseOptPathScaled(frameLineD, artCardScale, ui.RoundCap, ui.RoundJoin)
	assets.frameThin = parseOptPathScaled(frameThinD, artCardScale, ui.RoundCap, ui.RoundJoin)
	assets.frameGilt = parseFillPathScaled(frameGiltD, artCardScale, ui.NonZero)

	// 3. Slot placeholder
	assets.slotSolid = parseOptPathScaled(
		"M7 20V7H20M80 7H93V20M93 152V165H80M20 165H7V152"+
			"M50 78L51.7 84.3L58 86L51.7 87.7L50 94L48.3 87.7L42 86L48.3 84.3Z",
		artCardScale,
		ui.RoundCap, ui.RoundJoin,
	)
	assets.slotRing = parseOptPathScaled(tgCirc(50, 86, 12), artCardScale, ui.RoundCap, ui.RoundJoin)
	if assets.slotRing != nil {
		assets.slotRing.Dash(0, 0.55*artCardScale, 1.65*artCardScale)
	}

	// 4. Corner flourish, fleuron, compass, mystic eye
	assets.corner = parseOptPath(
		"M3 25V3H25M3 14A11 11 0 0 0 14 3M3 18.5A15.5 15.5 0 0 0 18.5 3"+
			"M18 14.5L18.9 17.1L21.5 18L18.9 18.9L18 21.5L17.1 18.9L14.5 18L17.1 17.1Z"+
			"M3 26.2L4.5 28.2L3 30.2L1.5 28.2ZM26.2 3L28.2 1.5L30.2 3L28.2 4.5Z"+
			tgCirc(7.2, 7.2, 1.6),
		ui.RoundCap, ui.RoundJoin,
	)
	assets.fleuron = parseOptPath(
		"M0 5H29M55 5H84M42 1L46.5 5L42 9L37.5 5ZM42 3.4L43.8 5L42 6.6L40.2 5ZM33 3.3L34.9 5L33 6.7L31.1 5ZM51 3.3L52.9 5L51 6.7L49.1 5Z",
		ui.RoundCap, ui.RoundJoin,
	)
	compassLineD := tgCirc(60, 60, 52) + tgCirc(60, 60, 49.5) + tgCirc(60, 60, 31) + tgCirc(60, 60, 6) +
		tgRays(60, 60, 49.5, 52, 72, 0) +
		tgNeedle(60, 60, 7, 46, 4, 0.36, 0) +
		tgNeedle(60, 60, 7, 34, 4, 0.3, pi/4) +
		tgRays(60, 60, 36, 44, 8, pi/8)
	var compassGiltD strings.Builder
	compassGiltD.WriteString(tgStar(60, 5.2, 2.6, 1, 4, 0))
	for i := 0; i < 4; i++ {
		a := -pi/2 + float64(i)*pi/2
		compassGiltD.WriteString(tgShape(tgPoly{
			{60 + 7*math.Cos(a-0.36), 60 + 7*math.Sin(a-0.36)},
			{60 + 46*math.Cos(a), 60 + 46*math.Sin(a)},
			{60 + 6.5*math.Cos(a), 60 + 6.5*math.Sin(a)},
		}))
	}
	assets.compassLine = parseOptPath(compassLineD, ui.RoundCap, ui.RoundJoin)
	assets.compassGilt = parseFillPath(compassGiltD.String(), ui.NonZero)

	assets.eyeOutline = parseOptPath("M2 14Q22 -4 42 14Q22 32 2 14Z"+tgCirc(22, 14, 5.5), ui.RoundCap, ui.RoundJoin)
	assets.eyePupil = parseFillPath(tgCirc(22, 14, 1.6), ui.NonZero)

	// 5. Star chart (viewBox 0 0 800 800)
	chartTicks := tgRays(400, 400, 362, 384, 72, 0) + tgRays(400, 400, 372, 384, 72, pi/72)
	chartFineD := tgRays(400, 400, 362, 367, 360, 0) + tgRays(400, 400, 196, 290, 12, pi/12)
	constellations := "M128 232L176 196L236 214L268 166L330 150" +
		"M520 640L572 606L640 622L676 580" +
		"M600 150L644 196L702 206" +
		"M110 560L150 600L132 652"
	assets.chartMain = parseOptPath(
		tgCirc(400, 400, 384)+tgCirc(400, 400, 362)+tgCirc(400, 400, 196)+chartTicks+constellations,
		ui.RoundCap, ui.RoundJoin,
	)
	assets.chartFine = parseOptPath(tgCirc(400, 400, 330)+chartFineD, ui.RoundCap, ui.RoundJoin)
	assets.chartDash = parseOptPath(tgCirc(400, 400, 290), ui.RoundCap, ui.RoundJoin)
	if assets.chartDash != nil {
		assets.chartDash.Dash(0, 2, 7)
	}
	assets.chartCross = parseOptPath("M400 16V784M16 400H784", ui.RoundCap, ui.RoundJoin)
	if assets.chartCross != nil {
		assets.chartCross.Dash(0, 1, 8)
	}
	assets.chartEllipse = parseOptPath(tgEll(400, 400, 290, 118), ui.RoundCap, ui.RoundJoin)
	if assets.chartEllipse != nil {
		assets.chartEllipse.Dash(0, 3, 6)
	}
	assets.chartStars = parseFillPath(
		tgCirc(128, 232, 3)+tgCirc(176, 196, 2.4)+tgCirc(236, 214, 3.2)+tgCirc(268, 166, 2.2)+tgCirc(330, 150, 2.8)+
			tgCirc(520, 640, 2.6)+tgCirc(572, 606, 3.2)+tgCirc(640, 622, 2.2)+tgCirc(676, 580, 2.8)+
			tgCirc(600, 150, 2.4)+tgCirc(644, 196, 3)+tgCirc(702, 206, 2.2)+
			tgCirc(110, 560, 2.6)+tgCirc(150, 600, 2.2)+tgCirc(132, 652, 3)+
			tgStar(236, 214, 8, 2.4, 4, 0)+tgStar(572, 606, 8, 2.4, 4, 0)+
			tgStar(644, 196, 7, 2.1, 4, 0)+tgStar(132, 652, 7, 2.1, 4, 0),
		ui.NonZero,
	)

	// 6. Moon phase ring (viewBox 0 0 760 760)
	var phaseRingB, phaseFillB strings.Builder
	phaseRingB.WriteString(tgCirc(380, 380, 372))
	for i := 0; i < 8; i++ {
		a := -pi/2 + float64(i)*pi/4
		x := 380 + 351*math.Cos(a)
		y := 380 + 351*math.Sin(a)
		const r = 13.0
		phaseRingB.WriteString(tgCirc(x, y, r))
		switch i {
		case 1:
			phaseFillB.WriteString(tgCres(x, y, r, 0.55, false))
		case 2:
			phaseFillB.WriteString("M" + tgP(x, y-r) + "A" + tgF(r) + " " + tgF(r) + " 0 0 1 " + tgP(x, y+r) + "Z")
		case 3:
			phaseFillB.WriteString("M" + tgP(x, y-r) + "A" + tgF(r) + " " + tgF(r) + " 0 0 1 " + tgP(x, y+r) + "A" + tgF(r*0.5) + " " + tgF(r) + " 0 0 1 " + tgP(x, y-r) + "Z")
		case 4:
			phaseFillB.WriteString(tgCirc(x, y, r))
		case 5:
			phaseFillB.WriteString("M" + tgP(x, y-r) + "A" + tgF(r) + " " + tgF(r) + " 0 0 0 " + tgP(x, y+r) + "A" + tgF(r*0.5) + " " + tgF(r) + " 0 0 0 " + tgP(x, y-r) + "Z")
		case 6:
			phaseFillB.WriteString("M" + tgP(x, y-r) + "A" + tgF(r) + " " + tgF(r) + " 0 0 0 " + tgP(x, y+r) + "Z")
		case 7:
			phaseFillB.WriteString(tgCres(x, y, r, 0.55, true))
		}
		bAng := a + pi/8
		phaseFillB.WriteString(tgStar(380+351*math.Cos(bAng), 380+351*math.Sin(bAng), 5, 1.7, 4, 0))
	}
	phaseRingB.WriteString(tgRays(380, 380, 364, 372, 96, 0))
	phaseRingB.WriteString(tgRays(380, 380, 331, 336, 120, 0))
	assets.phaseRing = parseOptPath(phaseRingB.String(), ui.RoundCap, ui.RoundJoin)
	assets.phaseDash = parseOptPath(tgCirc(380, 380, 330), ui.RoundCap, ui.RoundJoin)
	if assets.phaseDash != nil {
		assets.phaseDash.Dash(0, 1, 6)
	}
	assets.phaseFill = parseFillPath(phaseFillB.String(), ui.NonZero)

	// 7. Small vector icons (viewBox 0 0 24 24)
	assets.icons[IconMoon] = parseOptPath("M20 14.5A8.5 8.5 0 1 1 9.5 4A7 7 0 0 0 20 14.5Z", ui.RoundCap, ui.RoundJoin)
	assets.icons[IconStar4] = parseOptPath("M12 2L13.8 10.2L22 12L13.8 13.8L12 22L10.2 13.8L2 12L10.2 10.2Z", ui.RoundCap, ui.RoundJoin)
	assets.icons[IconChevronLeft] = parseOptPath("M14.5 6L8.5 12L14.5 18", ui.RoundCap, ui.RoundJoin)
	assets.icons[IconQuill] = parseOptPath("M4 20L5 15L15.5 4.5L19.5 8.5L9 19ZM13.5 6.5L17.5 10.5", ui.RoundCap, ui.RoundJoin)
	assets.icons[IconSave] = parseOptPath("M12 4V15M7 10.5L12 15.5L17 10.5M5 19.5H19", ui.RoundCap, ui.RoundJoin)
	assets.icons[IconCheck] = parseOptPath("M5 12.5L10 17L19 7.5", ui.RoundCap, ui.RoundJoin)
	assets.icons[IconClose] = parseOptPath("M6 6L18 18M18 6L6 18", ui.RoundCap, ui.RoundJoin)
	assets.icons[IconStop] = parseFillPath("M6 4.5H18A1.5 1.5 0 0 1 19.5 6V18A1.5 1.5 0 0 1 18 19.5H6A1.5 1.5 0 0 1 4.5 18V6A1.5 1.5 0 0 1 6 4.5Z", ui.NonZero)

	// 8. All 22 Major Arcana glyphs (viewBox 0 0 100 140)
	assets.glyphs = buildAllArcanaGlyphs()
}

type rawGlyph struct {
	tone, fill, gilt, ink, gold string
}

func buildAllArcanaGlyphs() map[string]arcanaPaths {
	const pi = math.Pi
	const plate = "M2 2H98V138H2Z"
	ground := tgHatch([]tgPoly{tgBox(5, 129, 95, 135)}, 0, 1.5, 0)

	// Shared TG-LIB geometry pieces
	veil := tgPoly{{28.6, 44.6}, {39, 49.3}, {50, 51.2}, {61, 49.3}, {71.4, 44.6}, {71.4, 73}, {28.6, 73}}
	var floorB strings.Builder
	fy := [3]float64{120, 125.5, 133}
	fx := func(y, u float64) float64 { return 50 + (u-0.5)*(76+y-120) }
	for r := 0; r < 2; r++ {
		for c := 0; c < 8; c++ {
			if (r+c)%2 == 0 {
				u0 := float64(c) / 8.0
				u1 := float64(c+1) / 8.0
				floorB.WriteString(tgShape(tgPoly{
					{fx(fy[r], u0), fy[r]},
					{fx(fy[r], u1), fy[r]},
					{fx(fy[r+1], u1), fy[r+1]},
					{fx(fy[r+1], u0), fy[r+1]},
				}))
			}
		}
	}
	nightSky := tgPoly{{5, 5}, {95, 5}, {95, 128}, {94, 128}, {82, 106}, {74, 114}, {54, 88}, {36, 112}, {26, 102}, {6, 128}, {5, 128}}
	var rimB strings.Builder
	for k := 0; k < 4; k++ {
		rimB.WriteString(tgHatch([]tgPoly{tgSector(50, 66, 25.6, 33.4, float64(k*90+31), float64(k*90+59))}, float64(k*90+135), 1.2, 0))
	}
	gallows := tgPoly{{5, 5}, {95, 5}, {95, 128}, {77.5, 128}, {77.5, 22}, {83.5, 22}, {83.5, 13.5}, {16.5, 13.5}, {16.5, 22}, {22.5, 22}, {22.5, 128}, {5, 128}}
	hill := tgPoly{{24.6, 104}, {31, 100.6}, {37, 99.4}, {43, 99.6}, {50, 100.6}, {57, 99.6}, {63, 99.4}, {69, 100.6}, {75.4, 104}, {75.4, 123}, {24.6, 123}}
	lane := tgPoly{{48, 102}, {52, 102}, {53.6, 110}, {55.2, 121.6}, {44.8, 121.6}, {46.4, 110}}

	var laurelB strings.Builder
	for i := 0; i < 36; i++ {
		t := float64(i) / 36.0 * 2 * pi
		x := 50 + 24*math.Cos(t)
		y := 70 + 38*math.Sin(t)
		ta := math.Atan2(38*math.Cos(t), -24*math.Sin(t))
		laurelB.WriteString(tgSwell(x, y, x+5.6*math.Cos(ta+0.55), y+5.6*math.Sin(ta+0.55), 1.5))
		laurelB.WriteString(tgSwell(x, y, x+5.6*math.Cos(ta-0.55), y+5.6*math.Sin(ta-0.55), 1.5))
	}
	globe := make(tgPoly, 0, 11)
	for i := 0; i <= 10; i++ {
		a := (-40 + 16*float64(i)) * pi / 180.0
		globe = append(globe, tgPt{50 + 11*math.Cos(a), 70 + 11*math.Sin(a)})
	}

	raw := map[string]rawGlyph{
		// ===== 9 TG-LIB Reference Arcana =====
		"magician": {
			tone: plate + tgBurst([]tgPoly{tgBox(5, 5, 95, 84), tgOval(50, 30, 20, 23, 32)}, 50, 40, 64, 0.02) +
				tgHatch([]tgPoly{tgBox(26, 113.4, 74, 119)}, 90, 2, 0) +
				tgHatch([]tgPoly{{{27, 92.6}, {33.4, 92.6}, {32.6, 98.6}, {27, 102.4}}}, 60, 1.3, 0) + ground,
			fill: tgSwell(62, 86, 62, 108.6, 1.5),
			gilt: tgNeedle(50, 40, 4.6, 12.5, 8, 0.3, pi/8) + tgStar(39, 103, 3.6, 1.4, 5, 0) + tgCirc(50, 98, 1.5),
			ink: "M13 109H87V113H13ZM20 113V128M23 113V128M77 113V128M80 113V128M26 113V119M74 113V119" +
				tgWave(26, 74, 119, 1.3, 8) +
				"M49.2 44V95M50.8 44V95M20 92H34Q34 102 27 103Q20 102 20 92ZM27 103V108M23 108H31" +
				tgCirc(39, 103, 5) + "M58 92H66" + tgCirc(62, 85, 1) +
				tgLine(tgPt{72, 90}, tgPt{78, 108}) + "M74.5 96l-3.5-2M76.5 102l3.5-2M5 128H95M8 128q4-7 8 0M84 128q4-7 8 0",
			gold: "M50 22C56 13 67 13 67 22C67 31 56 31 50 22C44 13 33 13 33 22C33 31 44 31 50 22Z" +
				tgCirc(50, 40, 3.2) + tgCirc(50, 98, 3) + tgRays(50, 40, 6, 13, 8, 0),
		},
		"priestess": {
			tone: plate + tgHatch([]tgPoly{tgBox(5, 5, 95, 29), tgBox(29, 10, 71, 26)}, 0, 1.6, 0) +
				tgHatch([]tgPoly{tgBox(16.6, 34.6, 27.4, 117.4)}, 45, 1.5, 0) +
				tgHatch([]tgPoly{tgBox(16.6, 34.6, 27.4, 117.4)}, -45, 1.5, 0) +
				tgHatch([]tgPoly{tgBox(80, 34.6, 83.4, 117.4)}, 45, 1.3, 0) +
				"M75 36V116M78 36V116" +
				tgHatch([]tgPoly{veil, tgOval(36, 52, 2.8, 2.8, 12), tgOval(50, 57, 2.8, 2.8, 12), tgOval(64, 52, 2.8, 2.8, 12), tgOval(43, 61, 2.8, 2.8, 12), tgOval(57, 61, 2.8, 2.8, 12)}, 90, 1.5, 0),
			fill: floorB.String(),
			gilt: tgCres(37, 18, 6, 0.5, false) + tgCres(63, 18, 6, 0.5, true) +
				tgCirc(36, 52, 1.6) + tgCirc(50, 57, 1.6) + tgCirc(64, 52, 1.6) +
				tgCirc(43, 61, 1.6) + tgCirc(57, 61, 1.6) + tgCres(50, 112, 5.5, 0.5, true),
			ink: "M16 34H28V118H16ZM72 34H84V118H72Z" +
				tgLine(tgPt{13, 34}, tgPt{31, 34}) + tgLine(tgPt{69, 34}, tgPt{87, 34}) +
				tgLine(tgPt{13, 30}, tgPt{31, 30}) + tgLine(tgPt{69, 30}, tgPt{87, 30}) +
				tgLine(tgPt{13, 118}, tgPt{31, 118}) + tgLine(tgPt{69, 118}, tgPt{87, 118}) +
				"M28 44Q50 58 72 44M28 52Q50 66 72 52" + tgWave(28, 72, 73, 1.4, 10) +
				"M42 76H58V100H42ZM42 76Q38 76 38 80Q38 84 42 84M58 100Q62 100 62 96Q62 92 58 92M46 84H54M46 90H54" +
				"M12 120H88M9.25 125.5H90.75M5.5 133H94.5M12 120L5.5 133M88 120L94.5 133",
			gold: tgCirc(50, 18, 6) + tgCirc(50, 18, 3.4),
		},
		"hermit": {
			tone: plate + tgHatch([]tgPoly{nightSky, tgOval(58, 62, 18, 18, 32)}, 0, 1.6, 0) +
				tgHatch([]tgPoly{tgBox(5, 5, 95, 30)}, 0, 1.6, 0.8) +
				tgHatch([]tgPoly{{{26, 102}, {36, 112}, {36, 128}, {29, 128}}}, 62, 1.4, 0) +
				tgHatch([]tgPoly{{{54, 88}, {74, 114}, {74, 128}, {58, 128}}}, 62, 1.4, 0) +
				tgHatch([]tgPoly{{{82, 106}, {94, 128}, {84, 128}}}, 62, 1.4, 0) + ground,
			fill: tgSwell(30, 18, 30, 128, 1.6),
			gilt: tgNeedle(58, 62, 13, 17.5, 12, 0.09, pi/12) + tgStar(58, 62, 5.5, 2.2, 6, 0) +
				tgStar(82, 22, 2.6, 1, 4, 0) + tgStar(14, 60, 2.2, 0.9, 4, 0) +
				tgStar(88, 46, 1.8, 0.8, 4, 0) + tgStar(44, 16, 1.6, 0.7, 4, 0) +
				tgStar(68, 12, 1.4, 0.6, 4, 0) + tgStar(12, 30, 1.5, 0.6, 4, 0),
			ink: tgCirc(30, 17, 1.6) + "M30 42Q44 34 58 42" + tgLine(tgPt{58, 42}, tgPt{58, 48}) +
				"M52 48H64L68 54V70L64 76H52L48 70V54ZM48 54H68M48 70H68" + tgLine(tgPt{58, 76}, tgPt{58, 80}) +
				"M6 128L26 102L36 112L54 88L74 114L82 106L94 128M48 96L51 93.5L54 96.5L57 93.5L60 96M5 128H95",
			gold: tgRays(58, 62, 13, 17.5, 12, 0),
		},
		"wheel": {
			tone: plate + tgHatch([]tgPoly{tgBox(5, 5, 95, 112), tgOval(50, 66, 43, 43, 48)}, 0, 1.6, 0) +
				rimB.String() + tgWave(26, 74, 126.5, 1.2, 8) + tgWave(30, 70, 130, 1, 6),
			fill: tgNeedle(50, 66, 6.2, 25, 8, 0.16, 0) + tgCirc(50, 66, 2),
			gilt: tgNeedle(50, 66, 35.5, 42, 16, 0.035, 0) +
				tgStar(50, 13, 3.4, 1.3, 4, 0) + tgStar(14, 20, 4.5, 1.6, 4, 0) +
				tgStar(86, 20, 4.5, 1.6, 4, 0) + tgStar(14, 122, 4.5, 1.6, 4, 0) + tgStar(86, 122, 4.5, 1.6, 4, 0),
			ink: tgCirc(50, 66, 34) + tgCirc(50, 66, 25) + tgCirc(50, 66, 6) +
				tgRays(50, 66, 25, 34, 8, pi/8) +
				tgCirc(50, 36.5, 2) + tgCirc(79.5, 66, 2) + tgCirc(50, 95.5, 2) + tgCirc(20.5, 66, 2) +
				tgWave(22, 78, 122, 2, 6),
			gold: tgRays(50, 66, 35, 37.5, 64, 0) +
				tgRays(14, 20, 5.6, 7.6, 4, pi/4) + tgRays(86, 20, 5.6, 7.6, 4, pi/4) +
				tgRays(14, 122, 5.6, 7.6, 4, pi/4) + tgRays(86, 122, 5.6, 7.6, 4, pi/4),
		},
		"hanged": {
			tone: plate + tgHatch([]tgPoly{gallows}, 45, 2.2, 0) + "M26.6 24V124M74.6 24V124" +
				tgHatch([]tgPoly{{{44, 62}, {56, 62}, {54, 84}, {46, 84}}}, 135, 1.3, 0) + ground,
			fill: tgSwell(24, 50, 19.5, 45.5, 1.6) + tgSwell(28, 74, 32.5, 69.5, 1.6) +
				tgSwell(76, 44, 80.5, 39.5, 1.6) + tgSwell(72, 86, 67.5, 81.5, 1.6),
			gilt: tgNeedle(50, 92, 13, 19.5, 12, 0.1, pi/12),
			ink: "M18 15H82V20H18ZM24 20V128M28 20V128M72 20V128M76 20V128" +
				tgLine(tgPt{50, 20}, tgPt{50, 62}) + tgLine(tgPt{50, 38}, tgPt{62, 48}, tgPt{53, 60}) +
				"M44 62H56L54 84H46Z" + tgLine(tgPt{46, 82}, tgPt{38, 72}, tgPt{46, 64}) +
				tgLine(tgPt{54, 82}, tgPt{62, 72}, tgPt{54, 64}) + tgCirc(50, 92, 7) + "M5 128H95",
			gold: tgCirc(50, 92, 12) + tgRays(50, 92, 13, 18, 12, 0),
		},
		"star": {
			tone: plate + tgBurst([]tgPoly{tgBox(5, 5, 95, 78), tgOval(50, 38, 23, 23, 32)}, 50, 38, 72, 0.02) +
				tgWave(12, 88, 118, 1, 14) + tgWave(20, 80, 126, 0.9, 10) + tgWave(10, 90, 130, 0.8, 14) +
				"M46 104H54M44.5 107.5H55.5M47 110.5H53" +
				tgHatch([]tgPoly{{{8, 100}, {38, 100}, {34, 105}, {8, 105}}}, 0, 1.2, 0) +
				tgHatch([]tgPoly{{{62, 100}, {92, 100}, {92, 105}, {66, 105}}}, 0, 1.2, 0) +
				tgHatch([]tgPoly{{{28, 86.6}, {35.4, 86.6}, {34.6, 93}, {32, 97.4}, {28, 98.4}}}, 60, 1.2, 0) +
				tgHatch([]tgPoly{{{72, 86.6}, {79.4, 86.6}, {78.6, 93}, {76, 97.4}, {72, 98.4}}}, 60, 1.2, 0),
			fill: "",
			gilt: tgStar(50, 38, 8, 3.5, 8, 0) + tgNeedle(50, 38, 9, 22, 8, 0.1, pi/8) +
				tgStar(18, 18, 4, 1.5, 8, 0) + tgStar(82, 18, 4, 1.5, 8, 0) +
				tgStar(14, 46, 4, 1.5, 8, 0) + tgStar(86, 46, 4, 1.5, 8, 0) +
				tgStar(24, 68, 4, 1.5, 8, 0) + tgStar(76, 68, 4, 1.5, 8, 0) + tgStar(50, 72, 3.4, 1.3, 8, 0),
			ink: "M20 86Q20 99 28 99Q36 99 36 86ZM24 86L26 80H30L32 86M31 81Q44 88 46 112" +
				"M64 86Q64 99 72 99Q80 99 80 86ZM68 86L70 80H74L76 86M69 81Q56 88 54 112" +
				tgLine(tgPt{8, 100}, tgPt{38, 100}) + tgLine(tgPt{62, 100}, tgPt{92, 100}) +
				tgWave(8, 92, 114, 2.5, 8) + tgWave(16, 84, 122, 2, 6),
			gold: tgStar(50, 38, 20, 6, 8, 0),
		},
		"moon": {
			tone: plate + tgHatch([]tgPoly{tgBox(5, 5, 95, 76), tgOval(50, 36, 29, 29, 40)}, 0, 1.6, 0) +
				tgHatch([]tgPoly{tgBox(19.5, 82.6, 23.4, 121.4)}, 45, 1.2, 0) +
				tgHatch([]tgPoly{tgBox(85.5, 82.6, 89.4, 121.4)}, 45, 1.2, 0) +
				"M11.5 88h4M12.5 104h5M11.5 112h4M77.5 88h4M78.5 104h5M77.5 112h4" +
				tgHatch([]tgPoly{hill, lane}, 0, 1.5, 0) + tgWave(12, 88, 130.5, 1, 12),
			fill: "M15.5 92h3v6h-3ZM81.5 92h3v6h-3Z" + tgStip([]tgPoly{lane}, 26, 0.32, 7),
			gilt: tgCres(46, 36, 10, 0.5, false) + tgNeedle(50, 36, 18.5, 27.5, 16, 0.045, 0) +
				tgDrop(38, 58) + tgDrop(50, 62) + tgDrop(62, 58) + tgDrop(44, 70) + tgDrop(56, 70),
			ink: tgCirc(50, 36, 15) +
				"M10 122V82H24V122M10 82V77H13V80H16V77H18V80H21V77H24V82" +
				"M76 122V82H90V122M76 82V77H79V80H82V77H84V80H87V77H90V82" +
				"M44 128Q47 108 50 92M56 128Q53 108 50 92M24 104Q37 96 50 100Q63 96 76 104" +
				tgWave(8, 92, 126, 2, 8),
			gold: tgRays(50, 36, 19, 23, 16, pi/16),
		},
		"sun": {
			tone: plate + tgBurst([]tgPoly{tgBox(5, 5, 95, 73), tgOval(50, 40, 31.5, 31.5, 40)}, 50, 40, 90, 0.017) +
				tgHatch([]tgPoly{tgBox(8, 106.7, 92, 112)}, 45, 1.8, 0) +
				tgHatch([]tgPoly{tgBox(5, 125, 95, 135)}, 0, 1.7, 0),
			fill: tgCirc(28, 81, 2.2) + tgCirc(72, 81, 2.2) + tgCirc(14, 87, 1.7) + tgCirc(86, 87, 1.7) +
				tgSwell(28, 92, 22.5, 88.5, 1.8) + tgSwell(72, 92, 77.5, 88.5, 1.8) +
				tgStip([]tgPoly{tgBox(8, 113.5, 92, 123)}, 40, 0.3, 11),
			gilt: tgNeedle(50, 40, 17, 30, 12, 0.11, 0) +
				tgNeedle(28, 81, 3.2, 7.5, 10, 0.26, 0) + tgNeedle(72, 81, 3.2, 7.5, 10, 0.26, 0) +
				tgNeedle(14, 87, 2.5, 5.8, 8, 0.3, 0) + tgNeedle(86, 87, 2.5, 5.8, 8, 0.3, 0),
			ink: "M45 38h2M53 38h2M46 44q4 3 8 0M8 96H92M8 101.3H92M8 106.7H92M8 112H92" +
				"M20 96V101.3M36 96V101.3M52 96V101.3M68 96V101.3M84 96V101.3" +
				"M12 101.3V106.7M28 101.3V106.7M44 101.3V106.7M60 101.3V106.7M76 101.3V106.7" +
				"M20 106.7V112M36 106.7V112M52 106.7V112M68 106.7V112M84 106.7V112" +
				"M28 84V96M72 84V96M14 89.5V96M86 89.5V96" +
				tgCirc(28, 81, 3) + tgCirc(72, 81, 3) + "M5 124H95",
			gold: tgCirc(50, 40, 14) + tgRays(50, 40, 17, 25, 12, pi/12),
		},
		"world": {
			tone: plate + tgHatch([]tgPoly{tgBox(5, 5, 95, 135), tgOval(50, 70, 31, 45, 48)}, 0, 1.8, 0) +
				tgHatch([]tgPoly{globe}, 130, 1.1, 0),
			fill: laurelB.String(),
			gilt: tgSwell(44, 28, 56, 36, 1.6) + tgSwell(56, 28, 44, 36, 1.6) +
				tgSwell(44, 104, 56, 112, 1.6) + tgSwell(56, 104, 44, 112, 1.6) +
				tgStar(14, 16, 5, 1.8, 4, 0) + tgStar(86, 16, 5, 1.8, 4, 0) +
				tgStar(14, 124, 5, 1.8, 4, 0) + tgStar(86, 124, 5, 1.8, 4, 0) +
				tgStip([]tgPoly{tgOval(50, 70, 18.5, 31.5, 32), tgOval(50, 70, 14.5, 14.5, 24)}, 70, 0.3, 5),
			ink: tgEll(50, 70, 24, 38) + tgCirc(50, 70, 11.4) + tgEll(50, 70, 4.6, 11.4) + tgEll(50, 70, 8.4, 11.4) +
				"M38.6 70H61.4M40.2 64.3Q50 67 59.8 64.3M40.2 75.7Q50 73 59.8 75.7",
			gold: tgCirc(50, 70, 13.2) +
				tgRays(14, 16, 6.2, 8.2, 4, pi/4) + tgRays(86, 16, 6.2, 8.2, 4, pi/4) +
				tgRays(14, 124, 6.2, 8.2, 4, pi/4) + tgRays(86, 124, 6.2, 8.2, 4, pi/4),
		},

		// ===== 13 Companion Copperplate Major Arcana =====

		// 0 · The Fool: cliff brink, pilgrim staff & bundle, leaping companion, sun at upper right, waves below
		"fool": {
			tone: plate +
				tgBurst([]tgPoly{tgBox(5, 5, 95, 78), tgOval(76, 24, 16, 16, 32)}, 76, 24, 56, 0.02) +
				tgHatch([]tgPoly{{{5, 76}, {44, 76}, {50, 128}, {5, 128}}}, 65, 1.5, 0) +
				tgHatch([]tgPoly{{{5, 76}, {44, 76}, {38, 92}, {5, 92}}}, -25, 1.6, 0) +
				ground,
			fill: tgSwell(24, 42, 68, 72, 1.3) +
				tgShape(tgPoly{{45, 90}, {52, 86}, {56, 90}, {53, 94}, {47, 94}}) +
				tgCirc(22, 40, 2.1),
			gilt: tgNeedle(76, 24, 9, 17, 10, 0.14, pi/10) +
				tgStar(28, 22, 3.8, 1.4, 6, 0) +
				tgStar(48, 16, 2.2, 0.9, 4, 0) +
				tgStar(16, 56, 2.0, 0.8, 4, 0),
			ink: "M5 76H44L50 128M16 76L22 98L14 128M30 76L36 106L34 128" +
				"M52 58Q57 52 63 58Q65 65 58 68Q51 66 52 58Z" +
				"M54 62L62 62M46 82Q54 78 60 84" +
				tgWave(52, 94, 114, 2.2, 5) + tgWave(56, 92, 121, 1.8, 4) +
				"M5 128H95",
			gold: tgCirc(76, 24, 7.2) + tgRays(76, 24, 9, 14, 10, 0) +
				tgCirc(28, 22, 5.2) + tgRays(24, 42, 4, 7, 4, pi/4),
		},

		// III · The Empress: 12-star crown arch, swaying wheat stalks, flowing stream, Venus shield
		"empress": {
			tone: plate +
				tgHatch([]tgPoly{tgBox(5, 5, 95, 42), tgOval(50, 32, 32, 22, 32)}, 0, 1.7, 0) +
				tgHatch([]tgPoly{{{5, 86}, {36, 86}, {36, 128}, {5, 128}}}, 45, 1.6, 0) +
				tgHatch([]tgPoly{{{64, 86}, {95, 86}, {95, 128}, {64, 128}}}, -45, 1.6, 0) +
				ground,
			fill: tgSwell(20, 56, 26, 63, 1.4) + tgSwell(20, 66, 26, 73, 1.4) + tgSwell(20, 76, 26, 83, 1.4) +
				tgSwell(80, 56, 74, 63, 1.4) + tgSwell(80, 66, 74, 73, 1.4) + tgSwell(80, 76, 74, 83, 1.4) +
				tgCirc(50, 94, 1.8),
			gilt: tgStar(50, 15, 2.6, 1.0, 4, 0) +
				tgStar(39, 17, 2.3, 0.9, 4, 0) + tgStar(61, 17, 2.3, 0.9, 4, 0) +
				tgStar(29, 23, 2.3, 0.9, 4, 0) + tgStar(71, 23, 2.3, 0.9, 4, 0) +
				tgStar(21, 32, 2.1, 0.8, 4, 0) + tgStar(79, 32, 2.1, 0.8, 4, 0) +
				tgCres(50, 54, 7, 0.55, true),
			ink: "M16 42Q50 18 84 42" +
				"M20 50V116M80 50V116" +
				"M34 86H66L62 114Q50 124 38 114Z" +
				tgWave(6, 36, 118, 1.8, 5) + tgWave(64, 94, 118, 1.8, 5) +
				tgWave(38, 62, 124, 1.4, 4) +
				"M5 128H95",
			gold: tgCirc(50, 94, 8.5) + "M50 102.5V115M44 109H56" +
				tgCirc(50, 54, 10) + tgRays(50, 54, 12, 17, 8, pi/8),
		},

		// IV · The Emperor: stone throne with ram horns, jagged peaks, Ankh scepter & orb
		"emperor": {
			tone: plate +
				tgHatch([]tgPoly{{{5, 5}, {95, 5}, {95, 58}, {78, 42}, {62, 58}, {50, 40}, {38, 58}, {22, 42}, {5, 58}}}, 0, 1.6, 0) +
				tgHatch([]tgPoly{tgBox(26, 44, 74, 112)}, 45, 1.7, 0) +
				tgHatch([]tgPoly{tgBox(26, 44, 74, 112)}, -45, 1.7, 0) +
				ground,
			fill: tgShape(tgPoly{{42, 22}, {46, 15}, {50, 20}, {54, 15}, {58, 22}}) +
				tgSwell(64, 68, 64, 102, 1.4),
			gilt: tgNeedle(50, 26, 8, 14, 7, 0.14, 0) +
				tgStar(18, 18, 3.2, 1.2, 4, 0) + tgStar(82, 18, 3.2, 1.2, 4, 0) +
				tgCirc(36, 80, 2.2),
			ink: "M5 58L22 42L38 58L50 40L62 58L78 42L95 58" +
				"M22 44H78V122H22ZM16 122H84V128H16Z" +
				"M22 48Q12 46 14 36Q16 30 22 34M78 48Q88 46 86 36Q84 30 78 34" +
				"M28 112H72M5 128H95",
			gold: tgCirc(64, 58, 5.5) + "M64 63.5V98M57 71H71" +
				tgCirc(36, 80, 7.5) + "M36 69V72.5M32.5 70.5H39.5" +
				"M38 26H62",
		},

		// V · The Hierophant: twin temple pillars, Papal triple crown, crossed Keys of Heaven
		"hierophant": {
			tone: plate +
				tgHatch([]tgPoly{tgBox(5, 5, 95, 34), tgOval(50, 32, 22, 20, 32)}, 0, 1.6, 0) +
				tgHatch([]tgPoly{tgBox(14, 34, 22, 118)}, 45, 1.4, 0) +
				tgHatch([]tgPoly{tgBox(78, 34, 86, 118)}, -45, 1.4, 0) +
				tgHatch([]tgPoly{tgBox(26, 118, 74, 128)}, 0, 1.5, 0) +
				ground,
			fill: tgShape(tgPoly{{34, 96}, {38, 92}, {44, 98}, {40, 102}}) +
				tgSwell(50, 44, 50, 74, 1.3),
			gilt: tgCirc(50, 88, 2.2) +
				tgNeedle(50, 24, 10, 15, 8, 0.12, pi/8) +
				tgStar(26, 20, 3.0, 1.1, 4, 0) + tgStar(74, 20, 3.0, 1.1, 4, 0) +
				"M62 76L40 98M38 94L44 100",
			ink: "M14 34H22V118H14ZM78 34H86V118H78ZM11 34H25M75 34H89M11 118H89M18 118L12 128M82 118L88 128" +
				"M50 40V76M44 46H56M41 53H59M38 60H62" +
				tgLine(tgPt{36, 76}, tgPt{62, 102}) + tgCirc(64.5, 104.5, 4) +
				"M36 76L32 80L35 83L39 79" +
				"M5 128H95",
			gold: "M42 28H58L55 22H45ZM40 34H60L58 28H42ZM50 12V22M46 16H54" +
				tgCirc(35.5, 104.5, 4) + tgLine(tgPt{64, 76}, tgPt{38, 102}) +
				"M64 76L68 80L65 83L61 79",
		},

		// VI · The Lovers: winged solar angel above, Tree of Life & Knowledge, mountain & vesica rings
		"lovers": {
			tone: plate +
				tgBurst([]tgPoly{tgBox(5, 5, 95, 76), tgOval(50, 24, 15, 15, 32)}, 50, 24, 60, 0.02) +
				tgHatch([]tgPoly{{{30, 128}, {50, 92}, {70, 128}}}, 60, 1.4, 0) +
				ground,
			fill: tgSwell(20, 82, 20, 124, 1.6) + tgSwell(80, 82, 80, 124, 1.6) +
				tgSwell(77, 114, 83, 98, 1.1) + tgSwell(83, 98, 76, 86, 1.1),
			gilt: tgNeedle(50, 24, 8, 16, 12, 0.1, pi/12) +
				tgDrop(16, 62) + tgDrop(24, 62) + tgDrop(20, 54) + tgDrop(14, 72) + tgDrop(26, 72) +
				tgStar(50, 76, 3.6, 1.4, 4, 0),
			ink: "M50 30Q32 24 12 36Q24 44 40 40Q26 48 16 54Q32 54 46 44" +
				"M50 30Q68 24 88 36Q76 44 60 40Q74 48 84 54Q68 54 54 44" +
				"M10 74Q20 50 30 74Q20 82 10 74ZM70 74Q80 50 90 74Q80 82 70 74Z" +
				"M28 128L50 90L72 128M5 128H95",
			gold: tgCirc(50, 24, 6.5) + tgRays(50, 24, 8, 14, 8, 0) +
				tgCirc(44, 76, 9) + tgCirc(56, 76, 9),
		},

		// VII · The Chariot: starry canopy, winged solar disk, chariot wheels, dark & gold sphinxes
		"chariot": {
			tone: plate +
				tgHatch([]tgPoly{tgBox(18, 16, 82, 34)}, 0, 1.5, 0) +
				tgHatch([]tgPoly{tgBox(30, 50, 70, 96)}, 90, 1.7, 0) +
				tgHatch([]tgPoly{{{5, 116}, {95, 116}, {95, 128}, {5, 128}}}, 0, 1.6, 0) +
				ground,
			fill: tgShape(tgPoly{{14, 126}, {22, 108}, {32, 108}, {36, 126}}) +
				tgCirc(25, 104, 3.5) +
				tgCirc(22, 82, 2.2) + tgCirc(78, 82, 2.2),
			gilt: tgShape(tgPoly{{64, 126}, {68, 108}, {78, 108}, {86, 126}}) +
				tgCirc(75, 104, 3.5) +
				tgStar(50, 24, 3.6, 1.4, 8, 0) +
				tgStar(30, 22, 2.2, 0.8, 4, 0) + tgStar(70, 22, 2.2, 0.8, 4, 0) +
				tgStar(24, 29, 1.8, 0.7, 4, 0) + tgStar(76, 29, 1.8, 0.7, 4, 0) +
				tgCres(34, 60, 4.2, 0.55, false) + tgCres(66, 60, 4.2, 0.55, true),
			ink: "M16 16H84V34H16ZM20 34V98M80 34V98M30 50H70V98H30Z" +
				tgCirc(22, 82, 14) + tgRays(22, 82, 3, 14, 8, 0) +
				tgCirc(78, 82, 14) + tgRays(78, 82, 3, 14, 8, 0) +
				"M10 116H90M5 128H95",
			gold: tgCirc(50, 72, 6.5) +
				"M43 70Q34 64 28 70Q35 74 43 73M57 70Q66 64 72 70Q65 74 57 73" +
				tgCirc(50, 24, 6) + "M40 34Q50 42 60 34",
		},

		// VIII · Strength: infinity lemniscate, garland wreath, stylized crowned lion
		"strength": {
			tone: plate +
				tgBurst([]tgPoly{tgBox(5, 5, 95, 52), tgOval(50, 22, 22, 12, 32)}, 50, 22, 48, 0.03) +
				tgHatch([]tgPoly{{{5, 108}, {95, 108}, {95, 128}, {5, 128}}}, 0, 1.6, 0) +
				tgHatch([]tgPoly{tgOval(50, 74, 24, 24, 32)}, 45, 1.8, 0) +
				ground,
			fill: tgSwell(24, 88, 34, 96, 1.5) + tgSwell(76, 88, 66, 96, 1.5) +
				tgSwell(20, 74, 26, 84, 1.4) + tgSwell(80, 74, 74, 84, 1.4) +
				tgShape(tgPoly{{47, 74}, {53, 74}, {50, 79}}),
			gilt: tgNeedle(50, 70, 18, 28, 12, 0.11, pi/12) +
				tgStar(22, 54, 2.8, 1.0, 4, 0) + tgStar(78, 54, 2.8, 1.0, 4, 0) +
				tgStar(50, 104, 3.2, 1.2, 4, 0),
			ink: tgCirc(50, 70, 17) +
				"M43 66H46M54 66H57M45 81Q50 84 55 81M42 54L39 48L46 52M58 54L61 48L54 52" +
				"M20 56Q14 78 28 96Q50 112 72 96Q86 78 80 56" +
				"M12 114Q34 104 50 108Q66 104 88 114M5 128H95",
			gold: "M50 22C56 13 67 13 67 22C67 31 56 31 50 22C44 13 33 13 33 22C33 31 44 31 50 22Z" +
				tgCirc(50, 70, 21) + tgRays(50, 70, 17, 25, 12, 0),
		},

		// XI · Justice: twin pillars, vertical curtain, upright sword dead-center, balanced scales
		"justice": {
			tone: plate +
				tgHatch([]tgPoly{tgBox(20, 24, 80, 114)}, 90, 1.8, 0) +
				tgHatch([]tgPoly{tgBox(12, 24, 19, 118)}, 45, 1.4, 0) +
				tgHatch([]tgPoly{tgBox(81, 24, 88, 118)}, -45, 1.4, 0) +
				ground,
			fill: floorB.String() + tgSwell(50, 28, 50, 96, 1.3),
			gilt: tgShape(tgPoly{{20, 82}, {36, 82}, {33, 88}, {23, 88}}) +
				tgShape(tgPoly{{64, 82}, {80, 82}, {77, 88}, {67, 88}}) +
				tgStar(50, 22, 3.2, 1.2, 4, 0),
			ink: "M12 24H19V118H12ZM81 24H88V118H81ZM9 24H22M78 24H91M9 118H91" +
				"M19 28Q50 38 81 28" +
				"M50 16L53 28V98H47V28ZM40 98H60M50 98V114M46 114H54" +
				"M28 52H72M28 52L20 82M28 52L36 82M72 52L64 82M72 52L80 82" +
				"M12 120H88M9.25 125.5H90.75M5.5 133H94.5M12 120L5.5 133M88 120L94.5 133",
			gold: tgCirc(50, 22, 7.5) + "M38 14L44 20L50 12L56 20L62 14" +
				tgCirc(28, 52, 2.2) + tgCirc(72, 52, 2.2) +
				"M20 82H36M64 82H80M40 98H60",
		},

		// XIII · Death: night sky, sweeping scythe blade, mystic rose, twin towers with rising sun between
		"death": {
			tone: plate +
				tgHatch([]tgPoly{tgBox(5, 5, 95, 76), tgOval(50, 42, 16, 16, 32)}, 0, 1.5, 0) +
				tgHatch([]tgPoly{tgBox(26, 82, 36, 114)}, 45, 1.3, 0) +
				tgHatch([]tgPoly{tgBox(64, 82, 74, 114)}, -45, 1.3, 0) +
				ground,
			fill: tgSwell(24, 24, 78, 122, 1.5) +
				tgShape(tgPoly{{14, 126}, {18, 119}, {22, 123}, {26, 119}, {30, 126}}),
			gilt: tgNeedle(50, 102, 10, 18, 9, 0.1, pi/9) +
				tgStar(50, 42, 7.5, 3.2, 5, 0) +
				tgStar(18, 20, 2.4, 0.9, 4, 0) + tgStar(80, 24, 2.2, 0.8, 4, 0),
			ink: "M24 24Q58 14 78 40Q52 28 30 34" +
				"M26 114V82H36V114M26 82V78H29V80H33V78H36V82" +
				"M64 114V82H74V114M64 82V78H67V80H71V78H74V82" +
				"M5 114H26M36 114H64M74 114H95" +
				tgWave(8, 92, 122, 2.0, 7) + "M5 128H95",
			gold: tgCirc(50, 42, 12) + tgCirc(50, 42, 3) +
				"M40 102A10 10 0 0 1 60 102" + tgRays(50, 102, 11, 16, 7, -pi/12),
		},

		// XIV · Temperance: alchemical triangle-in-square, winged aura, two chalices pouring sinuous stream
		"temperance": {
			tone: plate +
				tgBurst([]tgPoly{tgBox(5, 5, 95, 68), tgOval(50, 28, 16, 16, 32)}, 50, 28, 56, 0.02) +
				tgHatch([]tgPoly{{{5, 110}, {95, 110}, {95, 128}, {5, 128}}}, 0, 1.5, 0) +
				ground,
			fill: tgSwell(20, 118, 16, 100, 1.4) + tgSwell(20, 118, 25, 102, 1.2) +
				tgSwell(80, 118, 84, 100, 1.4) + tgSwell(80, 118, 75, 102, 1.2),
			gilt: tgShape(tgPoly{{50, 21}, {57, 34}, {43, 34}}) +
				tgDrop(46, 62) + tgDrop(52, 69) + tgDrop(58, 75) +
				tgStar(80, 24, 3.2, 1.2, 4, 0),
			ink: "M12 22Q28 32 36 46M10 34Q26 40 34 50" +
				"M88 22Q72 32 64 46M90 34Q74 40 66 50" +
				"M20 46H34Q34 56 27 57Q20 56 20 46ZM27 57V63M23 63H31" +
				"M66 82H80Q80 92 73 93Q66 92 66 82ZM73 93V99M69 99H77" +
				"M32 54C48 60 52 76 68 84M30 58C45 64 49 80 66 88" +
				tgWave(12, 88, 114, 1.8, 8) + tgWave(18, 82, 121, 1.4, 6) + "M5 128H95",
			gold: "M40 18H60V38H40Z" +
				"M34 52C48 58 52 78 66 86" +
				tgCirc(80, 24, 6) + tgRays(80, 24, 7, 11, 8, 0),
		},

		// XV · The Devil: inverted pentagram, bat wings, inverted torch, obsidian pedestal & loose chains
		"devil": {
			tone: plate +
				tgHatch([]tgPoly{tgBox(5, 5, 95, 128)}, 45, 2.2, 0) +
				tgHatch([]tgPoly{tgBox(5, 5, 95, 128)}, -45, 2.2, 0) +
				tgHatch([]tgPoly{tgBox(28, 82, 72, 110)}, 90, 1.4, 0) +
				ground,
			fill: tgShape(tgPoly{{50, 48}, {14, 42}, {10, 62}, {26, 68}, {42, 64}}) +
				tgShape(tgPoly{{50, 48}, {86, 42}, {90, 62}, {74, 68}, {58, 64}}) +
				tgShape(tgPoly{{28, 82}, {72, 82}, {72, 110}, {28, 110}}),
			gilt: tgStar(50, 28, 10, 4.2, 5, pi) +
				tgDrop(24, 66) + tgDrop(76, 66) +
				tgCirc(50, 96, 2.5),
			ink: "M38 22Q30 14 26 20M62 22Q70 14 74 20" +
				"M14 42L10 62L26 68L42 64M86 42L90 62L74 68L58 64" +
				"M28 82H72V110H28ZM22 110H78" +
				"M46 96L36 108L28 118M54 96L64 108L72 118" +
				tgCirc(26, 121, 3.8) + tgCirc(74, 121, 3.8) +
				"M5 128H95",
			gold: tgStar(50, 28, 12.5, 5.0, 5, pi) + tgCirc(50, 28, 13.5) +
				tgCirc(50, 96, 5) + tgRays(50, 28, 14, 18, 10, pi/10),
		},

		// XVI · The Tower: lightning bolt, blown crown, falling yods, brick tower & storm rain
		"tower": {
			tone: plate +
				tgHatch([]tgPoly{tgBox(5, 5, 95, 128)}, 68, 2.0, 0) +
				tgHatch([]tgPoly{tgBox(36, 42, 64, 126)}, 0, 2.2, 0) +
				ground,
			fill: tgShape(tgPoly{{36, 42}, {64, 42}, {66, 126}, {34, 126}}) +
				tgShape(tgPoly{{60, 24}, {66, 14}, {71, 20}, {77, 13}, {79, 25}}) +
				tgCirc(50, 62, 2.5),
			gilt: "M12 12L38 30L30 34L52 44L34 40L40 35Z" +
				tgDrop(20, 56) + tgDrop(26, 74) + tgDrop(16, 92) +
				tgDrop(76, 52) + tgDrop(82, 70) + tgDrop(74, 88) +
				tgDrop(86, 102) + tgDrop(22, 108),
			ink: "M36 42H64L66 126H34ZM34 42V36H40V40H46V36H54V40H60V36H66V42" +
				"M35 58H65M35 74H65M34 90H66M34 106H66" +
				"M46 66V58A4 4 0 0 1 54 58V66ZM46 98V90A4 4 0 0 1 54 90V98Z" +
				"M10 18Q18 12 26 18Q34 12 42 18M64 14Q74 8 84 14Q90 12 94 18" +
				"M5 128L22 114L34 126M66 126L78 114L95 128M5 128H95",
			gold: "M12 12L38 30L30 34L52 44" +
				"M59 26L66 14L71 20L77 13L80 26Z" +
				tgRays(50, 38, 6, 16, 8, pi/8),
		},

		// XX · Judgement: herald trumpet & cross banner, sound rays, rising spirit, open tomb & waves
		"judgement": {
			tone: plate +
				tgBurst([]tgPoly{tgBox(5, 5, 95, 76), tgOval(50, 32, 26, 20, 32)}, 50, 26, 60, 0.02) +
				tgHatch([]tgPoly{tgBox(28, 100, 72, 118)}, 45, 1.5, 0) +
				tgHatch([]tgPoly{{{5, 116}, {95, 116}, {95, 128}, {5, 128}}}, 0, 1.6, 0) +
				ground,
			fill: tgShape(tgPoly{{34, 98}, {66, 98}, {68, 114}, {32, 114}}) +
				tgShape(tgPoly{{5, 112}, {18, 94}, {28, 112}}) +
				tgShape(tgPoly{{72, 112}, {82, 94}, {95, 112}}),
			gilt: tgShape(tgPoly{{47, 14}, {53, 14}, {58, 38}, {42, 38}}) +
				"M48 44H52V60H48ZM41 50H59V54H41Z" +
				tgNeedle(50, 38, 14, 28, 7, 0.06, pi) +
				tgStar(50, 74, 3.6, 1.4, 4, 0),
			ink: "M47 14H53L58 38H42ZM38 42H62V62H38Z" +
				"M14 20Q28 12 42 20M58 20Q72 12 86 20" +
				"M38 82L50 92L62 82M50 78V98" +
				"M28 98H72L75 118H25ZM24 98L18 110" +
				tgWave(8, 92, 122, 1.8, 8) + "M5 128H95",
			gold: "M42 38H58M47 14H53" +
				"M34 66Q50 74 66 66M28 72Q50 82 72 72" +
				tgRays(50, 38, 10, 22, 7, pi),
		},
	}

	out := make(map[string]arcanaPaths, len(raw))
	for k, g := range raw {
		out[k] = arcanaPaths{
			tone: parseOptPathScaled(g.tone, artCardScale, ui.RoundCap, ui.RoundJoin),
			fill: parseFillPathScaled(g.fill, artCardScale, ui.NonZero),
			gilt: parseFillPathScaled(g.gilt, artCardScale, ui.NonZero),
			ink:  parseOptPathScaled(g.ink, artCardScale, ui.RoundCap, ui.RoundJoin),
			gold: parseOptPathScaled(g.gold, artCardScale, ui.RoundCap, ui.RoundJoin),
		}
	}
	return out
}

// ===== Shared Geometry Helpers =====

func fitViewBoxTransform(r ui.Rect, vbW, vbH float32) (scale, dx, dy float32) {
	sx := r.W / vbW
	sy := r.H / vbH
	scale = sx
	if sy < scale {
		scale = sy
	}
	dx = r.X + (r.W-vbW*scale)*0.5
	dy = r.Y + (r.H-vbH*scale)*0.5
	return scale, dx, dy
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func drawCrosshatchLines(p *ui.Painter, r ui.Rect, col ui.Color) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	const spacing = 22.0
	step := float32(spacing * math.Sqrt2)
	p.Clip(r, 0, func() {
		// +45 deg diagonal lines
		for x := r.X - r.H; x <= r.X+r.W; x += step {
			p.Line(x, r.Y, x+r.H, r.Y+r.H, 1, col)
		}
		// -45 deg diagonal lines
		for x := r.X; x <= r.X+r.W+r.H; x += step {
			p.Line(x, r.Y, x-r.H, r.Y+r.H, 1, col)
		}
	})
}

// ===== ArtRenderer Implementation (16 Methods) =====

func (midnightArt) PaintCardBack(p *ui.Painter, r ui.Rect, radius float32, strokeScale float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	if strokeScale <= 0 {
		strokeScale = 1.0
	}
	tc := theme.Tarot()
	a := getAssets()

	p.FillRadialGradient(r, tc.CardBackGrad, radius)
	p.Clip(r, radius, func() {
		sx := r.W / (100 * artCardScale)
		sy := r.H / (172 * artCardScale)
		s := (sx + sy) * 0.5
		if s <= 0 {
			return
		}
		p.Transform(ui.Scaling(sx, sy).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
			p.Clip(ui.Rect{X: 10.5 * artCardScale, Y: 10.5 * artCardScale, W: 79 * artCardScale, H: 151 * artCardScale}, 0, func() {
				if a.latticeLines != nil {
					p.StrokePath(a.latticeLines, 0.22*artCardScale, tc.GoldLattice)
				}
				if a.latticeDots != nil {
					p.FillPath(a.latticeDots, tc.Gold.Alpha(0.4))
				}
			})
			if a.backShield != nil {
				p.FillPath(a.backShield, tc.CardBackShield)
			}
			if a.backDash != nil {
				p.StrokePath(a.backDash, 0.5*artCardScale, tc.Gold.Alpha(0.7))
			}
			if a.backThin != nil {
				p.StrokePath(a.backThin, 0.3*artCardScale, tc.Gold.Alpha(0.62))
			}
			if a.backLine != nil {
				p.StrokePathGradient(a.backLine, strokeScale/s, tc.FoilLightGrad)
			}
			if a.backGilt != nil {
				p.FillPathGradient(a.backGilt, tc.FoilLightGrad)
			}
		})
	})
	p.Stroke(r, tc.GoldBorderMid, radius, 1)
}

func (midnightArt) PaintCardFrame(p *ui.Painter, r ui.Rect, radius float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	tc := theme.Tarot()
	a := getAssets()

	p.FillRadialGradient(r, tc.ParchmentGrad, radius)
	p.Clip(r, radius, func() {
		sx := r.W / (100 * artCardScale)
		sy := r.H / (172 * artCardScale)
		s := (sx + sy) * 0.5
		if s <= 0 {
			return
		}
		p.Transform(ui.Scaling(sx, sy).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
			if a.laidH != nil {
				p.StrokePath(a.laidH, 0.22*artCardScale, tc.ParchmentLaid)
			}
			if a.laidV != nil {
				p.StrokePath(a.laidV, 0.3*artCardScale, tc.ParchmentLaidV)
			}
			if a.frameLine != nil {
				p.StrokePathGradient(a.frameLine, 1.2/s, tc.FoilDarkGrad)
			}
			if a.frameThin != nil {
				p.StrokePath(a.frameThin, 0.6/s, tc.GoldDark.Alpha(0.6))
			}
			if a.frameGilt != nil {
				p.FillPathGradient(a.frameGilt, tc.FoilDarkGrad)
			}
		})
	})
	p.Stroke(r, tc.ParchmentBorder, radius, 1)
}

func (midnightArt) PaintArcanaGlyph(p *ui.Painter, arcanaKey string, r ui.Rect, strokeScale float32, trim float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	scale, dx, dy := fitViewBoxTransform(r, 100*artCardScale, 140*artCardScale)
	if scale <= 0 {
		return
	}
	if strokeScale <= 0 {
		strokeScale = 1.25
	}
	tc := theme.Tarot()
	a := getAssets()
	g, ok := a.glyphs[arcanaKey]
	if !ok {
		g = a.glyphs["magician"]
	}
	useTrim := trim > 0 && trim < 1
	sw := strokeScale / scale

	p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(dx, dy)), 0, 0, func() {
		if g.tone != nil {
			p.StrokePath(g.tone, 0.32*artCardScale, tc.InkTone)
		}
		if g.fill != nil {
			p.FillPath(g.fill, tc.InkBody)
		}
		if g.gilt != nil {
			p.FillPathGradient(g.gilt, tc.FoilDarkGrad)
		}
		if g.ink != nil {
			if useTrim {
				cp := *g.ink
				cp.Trim(0, trim)
				p.StrokePath(&cp, sw, tc.InkBody)
			} else {
				p.StrokePath(g.ink, sw, tc.InkBody)
			}
		}
		if g.gold != nil {
			if useTrim {
				cp := *g.gold
				cp.Trim(0, trim)
				p.StrokePathGradient(&cp, sw, tc.FoilDarkGrad)
			} else {
				p.StrokePathGradient(g.gold, sw, tc.FoilDarkGrad)
			}
		}
	})
}

func (midnightArt) PaintTableCloth(p *ui.Painter, r ui.Rect, dailyGlow bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	tc := theme.Tarot()
	if dailyGlow {
		p.FillRadialGradient(r, tc.DailyGlowGrad, 0)
	} else {
		p.FillRadialGradient(r, tc.TableGlowGrad, 0)
	}
	drawCrosshatchLines(p, r, tc.GoldClothHatch)
}

func (midnightArt) PaintCrosshatchCloth(p *ui.Painter, r ui.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	tc := theme.Tarot()
	p.Fill(r, tc.TableCanvas, 0)
	drawCrosshatchLines(p, r, tc.GoldClothHatch)
}

func (midnightArt) PaintStarChart(p *ui.Painter, r ui.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	scale, dx, dy := fitViewBoxTransform(r, 800, 800)
	if scale <= 0 {
		return
	}
	tc := theme.Tarot()
	a := getAssets()
	col := tc.Gold.Alpha(0.16)

	p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(dx, dy)), 0, 0, func() {
		if a.chartMain != nil {
			p.StrokePath(a.chartMain, 1.0, col)
		}
		if a.chartFine != nil {
			p.StrokePath(a.chartFine, 0.7, col)
		}
		if a.chartDash != nil {
			p.StrokePath(a.chartDash, 1.0, col)
		}
		if a.chartCross != nil {
			p.StrokePath(a.chartCross, 1.0, col)
		}
		if a.chartEllipse != nil {
			p.Transform(ui.Rotation(-23.5), 400, 400, func() {
				p.StrokePath(a.chartEllipse, 0.8, col)
			})
		}
		if a.chartStars != nil {
			p.FillPath(a.chartStars, col)
		}
	})
}

func (midnightArt) PaintMoonPhaseRing(p *ui.Painter, r ui.Rect, angleDeg float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	scale, dx, dy := fitViewBoxTransform(r, 760, 760)
	if scale <= 0 {
		return
	}
	tc := theme.Tarot()
	a := getAssets()
	strokeCol := tc.Gold.Alpha(0.20)
	fillCol := tc.Gold.Alpha(0.11)

	t := ui.Translation(-380, -380).
		Then(ui.Scaling(scale, scale)).
		Then(ui.Rotation(angleDeg)).
		Then(ui.Translation(dx+380*scale, dy+380*scale))

	p.Transform(t, 0, 0, func() {
		if a.phaseRing != nil {
			p.StrokePath(a.phaseRing, 1.0, strokeCol)
		}
		if a.phaseDash != nil {
			p.StrokePath(a.phaseDash, 1.0, strokeCol)
		}
		if a.phaseFill != nil {
			p.FillPath(a.phaseFill, fillCol)
		}
	})
}

func (midnightArt) PaintCornerFlourish(p *ui.Painter, r ui.Rect, rotDeg float32, col ui.Color, strokeW float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	a := getAssets()
	if a.corner == nil {
		return
	}
	sx := r.W / 34
	sy := r.H / 34
	s := (sx + sy) * 0.5
	if s <= 0 {
		return
	}
	if strokeW <= 0 {
		strokeW = 1.1
	}
	t := ui.Translation(-17, -17).
		Then(ui.Scaling(sx, sy)).
		Then(ui.Rotation(rotDeg)).
		Then(ui.Translation(r.X+r.W*0.5, r.Y+r.H*0.5))

	p.Transform(t, 0, 0, func() {
		p.StrokePath(a.corner, strokeW/s, col)
	})
}

func (midnightArt) PaintFleuron(p *ui.Painter, r ui.Rect, col ui.Color, strokeW float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	a := getAssets()
	if a.fleuron == nil {
		return
	}
	scale, dx, dy := fitViewBoxTransform(r, 84, 10)
	if scale <= 0 {
		return
	}
	if strokeW <= 0 {
		strokeW = 1.2
	}
	p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(dx, dy)), 0, 0, func() {
		p.StrokePath(a.fleuron, strokeW/scale, col)
	})
}

func (midnightArt) PaintCompass(p *ui.Painter, r ui.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	scale, dx, dy := fitViewBoxTransform(r, 120, 120)
	if scale <= 0 {
		return
	}
	tc := theme.Tarot()
	a := getAssets()
	col := tc.Gold.Alpha(0.42)

	p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(dx, dy)), 0, 0, func() {
		if a.compassLine != nil {
			p.StrokePath(a.compassLine, 0.8, col)
		}
		if a.compassGilt != nil {
			p.FillPath(a.compassGilt, col)
		}
	})
}

func (midnightArt) PaintSlotPlaceholder(p *ui.Painter, r ui.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	sx := r.W / (100 * artCardScale)
	sy := r.H / (172 * artCardScale)
	s := (sx + sy) * 0.5
	if s <= 0 {
		return
	}
	tc := theme.Tarot()
	a := getAssets()
	col := tc.Gold.Alpha(0.50)

	p.Transform(ui.Scaling(sx, sy).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
		if a.slotSolid != nil {
			p.StrokePath(a.slotSolid, 1.0/s, col)
		}
		if a.slotRing != nil {
			p.StrokePath(a.slotRing, 1.0/s, col)
		}
	})
}

func (midnightArt) PaintMysticEye(p *ui.Painter, r ui.Rect, alpha float32) {
	alpha = clamp01(alpha)
	if r.W <= 0 || r.H <= 0 || alpha <= 0 {
		return
	}
	scale, dx, dy := fitViewBoxTransform(r, 44, 28)
	if scale <= 0 {
		return
	}
	tc := theme.Tarot()
	a := getAssets()
	col := tc.Gold.Alpha(alpha)

	p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(dx, dy)), 0, 0, func() {
		if a.eyeOutline != nil {
			p.StrokePath(a.eyeOutline, 1.3/scale, col)
		}
		if a.eyePupil != nil {
			p.FillPath(a.eyePupil, col)
		}
	})
}

func (midnightArt) PaintGlintSweep(p *ui.Painter, r ui.Rect, radius float32, prog float32) {
	if r.W <= 0 || r.H <= 0 || prog <= 0 || prog >= 1 {
		return
	}
	tc := theme.Tarot()

	var bandFade float32
	if prog < 0.14 {
		bandFade = prog / 0.14
	} else {
		bandFade = (1 - prog) / 0.86
	}
	bandFade = clamp01(bandFade)

	stops := make([]ui.GradientStop, len(tc.GlintGrad.Stops))
	for i, st := range tc.GlintGrad.Stops {
		stops[i] = ui.GradientStop{
			Color: st.Color.Alpha(bandFade),
			At:    st.At,
		}
	}
	grad := ui.LinearGradient{
		Angle: tc.GlintGrad.Angle,
		Stops: stops,
	}

	bandW := r.W * 1.1
	cx := (r.X + r.W + bandW*0.25) - prog*(r.W+bandW*0.5)
	bandRect := ui.Rect{
		X: cx - bandW*0.5,
		Y: r.Y - r.H*0.15,
		W: bandW,
		H: r.H * 1.3,
	}

	p.Clip(r, radius, func() {
		p.FillGradient(bandRect, grad, 0)
	})

	var edgeAlpha float32
	if prog < 0.18 {
		edgeAlpha = prog / 0.18
	} else {
		edgeAlpha = (1 - prog) / 0.82
	}
	edgeAlpha = clamp01(edgeAlpha)
	if edgeAlpha > 0 {
		p.Stroke(r, tc.GoldBright.Alpha(edgeAlpha), radius, 2)
	}
}

func (midnightArt) PaintPaperSheetOverlay(p *ui.Painter, r ui.Rect, radius float32, flashProg float32, showFleurons bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	tc := theme.Tarot()
	renderer := midnightArt{}

	// Double inset hairline borders
	if r.W > 24 && r.H > 24 {
		outer := ui.Rect{X: r.X + 7, Y: r.Y + 7, W: r.W - 14, H: r.H - 14}
		inner := ui.Rect{X: r.X + 11, Y: r.Y + 11, W: r.W - 22, H: r.H - 22}
		p.Stroke(outer, tc.ParchmentFrame, theme.RadiusXS, 1)
		p.Stroke(inner, tc.ParchmentFrame2, theme.Space2, 1)
	}

	// Top and bottom centered fleurons with parchment mask behind them
	if showFleurons && r.W > 100 {
		const maskW, maskH = 67, 9
		const flW, flH = 59, 7
		topMask := ui.Rect{X: r.X + (r.W-maskW)*0.5, Y: r.Y + 3, W: maskW, H: maskH}
		botMask := ui.Rect{X: r.X + (r.W-maskW)*0.5, Y: r.Y + r.H - 12, W: maskW, H: maskH}
		p.Fill(topMask, tc.ParchmentPaper, 0)
		p.Fill(botMask, tc.ParchmentPaper, 0)
		topFl := ui.Rect{X: r.X + (r.W-flW)*0.5, Y: r.Y + 4, W: flW, H: flH}
		botFl := ui.Rect{X: r.X + (r.W-flW)*0.5, Y: r.Y + r.H - 11, W: flW, H: flH}
		renderer.PaintFleuron(p, topFl, tc.GoldDark, 1.3)
		renderer.PaintFleuron(p, botFl, tc.GoldDark, 1.3)
	}

	// Save flash state
	cornerCol := tc.GoldDark
	cornerScale := float32(1)
	if flashProg > 0 && flashProg < 1 {
		var pulse float32
		if flashProg < 0.25 {
			pulse = flashProg / 0.25
		} else {
			pulse = (1 - flashProg) / 0.75
		}
		pulse = clamp01(pulse)
		cornerCol = tc.GoldDark.Mix(tc.GoldFlash, pulse)
		cornerScale = 1 + 0.18*pulse

		var ringAlpha float32
		if flashProg < 0.20 {
			ringAlpha = flashProg / 0.20
		} else {
			ringAlpha = (1 - flashProg) / 0.80
		}
		ringAlpha = clamp01(ringAlpha)
		if ringAlpha > 0 {
			p.Stroke(r, tc.GoldRing.Alpha(ringAlpha), radius, 2)
		}
	}

	// Four corner flourishes
	cornerSz := float32(34)
	inset := float32(14)
	if r.W < 440 {
		cornerSz = 24
		inset = 6
	}
	corners := [4]struct {
		x, y, rot float32
	}{
		{r.X + inset, r.Y + inset, 0},
		{r.X + r.W - inset - cornerSz, r.Y + inset, 90},
		{r.X + r.W - inset - cornerSz, r.Y + r.H - inset - cornerSz, 180},
		{r.X + inset, r.Y + r.H - inset - cornerSz, 270},
	}
	for _, c := range corners {
		cr := ui.Rect{X: c.x, Y: c.y, W: cornerSz, H: cornerSz}
		if cornerScale != 1 {
			cx, cy := cr.X+cr.W*0.5, cr.Y+cr.H*0.5
			p.Transform(ui.Scaling(cornerScale, cornerScale), cx, cy, func() {
				renderer.PaintCornerFlourish(p, cr, c.rot, cornerCol, 1.1)
			})
		} else {
			renderer.PaintCornerFlourish(p, cr, c.rot, cornerCol, 1.1)
		}
	}
}

func (midnightArt) PaintDevelopingCard(p *ui.Painter, r ui.Rect, arcanaKey string, generating bool, prog float32, glintProg float32, reduced bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	tc := theme.Tarot()
	renderer := midnightArt{}
	radius := theme.RadiusLG

	renderer.PaintCardBack(p, r, radius, 1.0)
	glyphRect := ui.Rect{
		X: r.X + r.W*0.12,
		Y: r.Y + r.H*0.16,
		W: r.W * 0.76,
		H: r.H * 0.68,
	}

	if !generating || reduced {
		renderer.PaintCardFrame(p, r, radius)
		renderer.PaintArcanaGlyph(p, arcanaKey, glyphRect, 1.1, 1)
		if glintProg > 0 && glintProg < 1 {
			renderer.PaintGlintSweep(p, r, radius, glintProg)
		}
		return
	}

	prog = clamp01(prog)
	if prog > 0 {
		revealH := r.H * prog
		clipR := ui.Rect{
			X: r.X,
			Y: r.Y + r.H - revealH,
			W: r.W,
			H: revealH,
		}
		p.Clip(clipR, 0, func() {
			renderer.PaintCardFrame(p, r, radius)
			if prog > 0.002 {
				renderer.PaintArcanaGlyph(p, arcanaKey, glyphRect, 1.1, prog)
			}
		})
	}

	if prog > 0 && prog < 1 {
		scanY := r.Y + r.H*(1-prog)
		scanR := ui.Rect{
			X: r.X - 10,
			Y: scanY - 0.75,
			W: r.W + 20,
			H: 1.5,
		}
		p.FillGradient(scanR, tc.ScanLineGrad, 0)
	}

	if glintProg > 0 && glintProg < 1 {
		renderer.PaintGlintSweep(p, r, radius, glintProg)
	}
}

func (midnightArt) PaintIcon(p *ui.Painter, r ui.Rect, kind IconKind, col ui.Color) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	a := getAssets()
	if int(kind) < 0 || int(kind) >= len(a.icons) {
		return
	}
	path := a.icons[kind]
	if path == nil {
		return
	}
	scale, dx, dy := fitViewBoxTransform(r, 24, 24)
	if scale <= 0 {
		return
	}
	p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(dx, dy)), 0, 0, func() {
		if kind == IconStop {
			p.FillPath(path, col)
			return
		}
		p.StrokePath(path, 1.65/scale, col)
	})
}
