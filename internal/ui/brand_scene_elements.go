package ui

import (
	"math"

	"github.com/egoist/mygo/ui"
)

type sceneRenderCtx struct {
	p              *ui.Painter
	ox, oy, w, h   float32
	alpha          float32 // overall scene opacity [0..1] for smooth crossfades
	t              float64 // tempo-scaled scene clock in seconds; 0 means "hold the rest pose"
	wm             float32 // motion envelope [0..1]; hides come-and-go elements while still
	dCalm, dRich   float32 // density envelopes [0..1] for the calm and rich element tiers
	orbAlpha       float32 // smooth orb visibility [0..1]
	orbExtraDY     float32 // gentle rise/set during scene or sidebar transitions
	layerShiftY    float32 // gentle vertical settle during scene transitions
	showCat        bool
	vars           sceneTimeVars
	dstGround      ui.Color
	hasGroundRemap bool
	skinChrome     bool
	skinAccent     ui.Color
}

func (rc *sceneRenderCtx) adaptGround(c ui.Color) ui.Color {
	if !rc.hasGroundRemap {
		return c
	}
	return c.Mix(rc.dstGround, 0.72)
}

func (rc *sceneRenderCtx) adaptAccent(c ui.Color, w float32) ui.Color {
	if !rc.skinChrome || rc.skinAccent.A == 0 {
		return c
	}
	return c.Mix(rc.skinAccent, w)
}

// tier returns the visibility of an element tier: 0 is always drawn, 1 needs calm density or
// richer, 2 needs rich density.
func (rc *sceneRenderCtx) tier(n uint8) float32 {
	switch n {
	case 0:
		return 1
	case 1:
		return rc.dCalm
	default:
		return rc.dRich
	}
}

// layer runs fn with the scene opacity scaled by mult, and skips it entirely once invisible.
// Colors must be derived from rc.alpha inside fn so they pick up the scaled opacity.
func (rc *sceneRenderCtx) layer(mult float32, fn func()) {
	if mult <= 0.004 {
		return
	}
	saved := rc.alpha
	rc.alpha = saved * mult
	fn()
	rc.alpha = saved
}

// fit returns how visible a discrete element ending at xRight should be: fully drawn while it
// sits left of the artwork's right feather, and faded out as a whole instead of being sliced by it.
func (rc *sceneRenderCtx) fit(xRight float32) float32 {
	return smooth(clampF((rc.w-brandSceneArtFeatherW+4-xRight)/10, 0, 1))
}

func (rc *sceneRenderCtx) at(x, y float32) (float32, float32) {
	return rc.ox + x, rc.oy + y
}

func (rc *sceneRenderCtx) col(c ui.Color, op float32) ui.Color {
	return c.Alpha(op * rc.alpha)
}

func (rc *sceneRenderCtx) altWave(t float64, delay, dur float32) float32 {
	if t <= 0 || dur <= 0 {
		return 0
	}
	// CSS animation-direction: alternate with ease-in-out has full round-trip 2*dur.
	ph := modPos(t+float64(delay), float64(dur)*2)
	if ph < 0.5 {
		return smooth(ph * 2)
	}
	return smooth((1 - ph) * 2)
}

func (rc *sceneRenderCtx) loopPhase(t float64, delay, dur float32) float32 {
	if t <= 0 || dur <= 0 {
		return 0
	}
	return modPos(t+float64(delay), float64(dur))
}

func (rc *sceneRenderCtx) radialGlow(cx, cy, r float32, c ui.Color, op float32) {
	a := op * rc.alpha
	if a <= 0.002 || r <= 0 {
		return
	}
	rc.p.FillRadialGradient(ui.Rect{X: cx - r, Y: cy - r, W: 2 * r, H: 2 * r}, ui.RadialGradient{
		Radius: r,
		Stops: []ui.GradientStop{
			{Color: c.Alpha(a), At: 0},
			{Color: c.Alpha(0), At: 1},
		},
	}, r)
}

func (rc *sceneRenderCtx) ellipseGlow(cx, cy, rx, ry float32, c ui.Color, op float32) {
	a := op * rc.alpha
	if a <= 0.002 || rx <= 0 || ry <= 0 {
		return
	}
	rc.p.Transform(ui.Scaling(rx/ry, 1), cx, cy, func() {
		rc.p.FillRadialGradient(ui.Rect{X: cx - ry, Y: cy - ry, W: 2 * ry, H: 2 * ry}, ui.RadialGradient{
			Radius: ry,
			Stops: []ui.GradientStop{
				{Color: c.Alpha(a), At: 0},
				{Color: c.Alpha(0), At: 1},
			},
		}, ry)
	})
}

func (rc *sceneRenderCtx) fillTerrain(path *ui.Path, c ui.Color) {
	if rc.alpha <= 0.002 {
		return
	}
	col := rc.adaptGround(c).Alpha(rc.alpha)
	rc.p.Transform(ui.Translation(rc.ox, rc.oy+rc.layerShiftY), 0, 0, func() {
		rc.p.FillPath(path, col)
	})
}

func (rc *sceneRenderCtx) strokeRim(path *ui.Path, width float32, c ui.Color, midStop, midOp, extraOp float32) {
	a := midOp * extraOp * rc.alpha
	if a <= 0.002 {
		return
	}
	c = rc.adaptAccent(c, 0.72)
	rc.p.Transform(ui.Translation(rc.ox, rc.oy+rc.layerShiftY), 0, 0, func() {
		rc.p.StrokePathGradient(path, width, ui.LinearGradient{
			Angle: 90,
			Stops: []ui.GradientStop{
				{Color: c.Alpha(0), At: 0},
				{Color: c.Alpha(a), At: midStop},
				{Color: c.Alpha(0), At: 1},
			},
		})
	})
}

func (rc *sceneRenderCtx) paintMist(m sceneMistBand) {
	if m.x > rc.w+40 || m.x+m.w < -40 {
		return
	}
	dx := float32(0)
	sx := float32(1)
	elemOp := float32(0.6)
	if rc.t > 0 {
		u := rc.altWave(rc.t, m.delaySec, m.durSec)
		switch m.kind {
		case mistDriftRight:
			dx = lerp(0, 24, u)
			elemOp = lerp(0.40, 0.90, u)
		case mistDriftLeft:
			dx = lerp(0, -28, u)
			sx = lerp(1.0, 1.14, u)
			elemOp = lerp(0.85, 0.35, u)
		case mistPulse:
			elemOp = lerp(0.12, 0.80, u)
		}
	}
	peakA := m.baseOp * elemOp * rc.tier(m.tier) * rc.alpha
	if peakA <= 0.002 {
		return
	}
	w := m.w * sx
	cx := m.x + m.w*0.5 + dx
	x0 := rc.ox + cx - w*0.5
	y0 := rc.oy + m.y + rc.layerShiftY
	// Two stacked bands, the inner one thinner, so the mist thins out toward its top and bottom
	// instead of ending on a hard edge.
	for _, s := range [2]float32{1, 0.5} {
		h := m.h * s
		a := peakA * 0.56
		rc.p.FillGradient(ui.Rect{X: x0, Y: y0 + (m.h-h)*0.5, W: w, H: h}, ui.LinearGradient{
			Angle: 90,
			Stops: []ui.GradientStop{
				{Color: m.color.Alpha(0), At: 0},
				{Color: m.color.Alpha(a), At: 0.28},
				{Color: m.color.Alpha(a), At: 0.72},
				{Color: m.color.Alpha(0), At: 1},
			},
		}, h*0.5)
	}
}

// paintStars draws the in-sidebar star field, keeping the right-aligned titlebar arrows clear.
func (rc *sceneRenderCtx) paintStars(stars []sceneStar, groupOp float32) {
	rc.paintStarField(stars, groupOp, 0, rc.w-4, true)
}

// paintSpill draws the few stars that trail past the sidebar edge into the titlebar.
func (rc *sceneRenderCtx) paintSpill(sc *compiledScene, maxX float32) {
	rc.paintStarField(sc.spill, rc.vars.starOp, rc.w, maxX-4, false)
}

func (rc *sceneRenderCtx) paintStarField(stars []sceneStar, groupOp, dx, maxX float32, avoidNav bool) {
	if len(stars) == 0 || groupOp*rc.alpha <= 0.002 {
		return
	}
	limitX := maxX
	if dx > 0 && rc.skinChrome {
		limitX = min(maxX, dx+76)
	}
	for i := range stars {
		st := &stars[i]
		x := st.x + dx
		if x >= limitX {
			continue
		}
		if avoidNav && st.y > 8 && st.y < 32 && x > rc.w-54 {
			continue
		}
		vis := rc.tier(st.tier)
		if vis <= 0.004 {
			continue
		}
		op := st.opacity
		if st.twinkle != twinkleNone && rc.t > 0 {
			ph := rc.loopPhase(rc.t, st.delaySec, st.durSec)
			if st.twinkle == twinkleSmooth {
				op = lerp(0.20, 1.0, cosWave01(ph))
			} else {
				op = irregularTwinkle(ph)
			}
		}
		a := op * st.dim * vis * groupOp * rc.alpha
		if dx > 0 && rc.skinChrome {
			edgeT := 1 - clampF(st.x/76, 0, 1)
			a *= edgeT * edgeT * 0.55
		}
		if a <= 0.004 {
			continue
		}
		var dot ui.Path
		dot.Circle(rc.ox+x, rc.oy+st.y, st.r)
		rc.p.FillPath(&dot, rc.adaptAccent(st.color, 0.55).Alpha(a))
	}
}

func irregularTwinkle(ph float32) float32 {
	// Keyframes: 0:0.35, 0.17:1, 0.31:0.5, 0.48:0.9, 0.63:0.25, 0.82:0.8, 1:0.35
	ks := [7][2]float32{
		{0.00, 0.35}, {0.17, 1.00}, {0.31, 0.50}, {0.48, 0.90},
		{0.63, 0.25}, {0.82, 0.80}, {1.00, 0.35},
	}
	return evalKeyframes1D(ks[:], ph)
}

func evalKeyframes1D(ks [][2]float32, ph float32) float32 {
	for i := 0; i < len(ks)-1; i++ {
		if ph <= ks[i+1][0] {
			span := ks[i+1][0] - ks[i][0]
			if span <= 0 {
				return ks[i+1][1]
			}
			u := smooth((ph - ks[i][0]) / span)
			return lerp(ks[i][1], ks[i+1][1], u)
		}
	}
	return ks[len(ks)-1][1]
}

func (rc *sceneRenderCtx) paintSparkle(cx, cy, s float32, c ui.Color, op float32) {
	a := op * rc.alpha
	if a <= 0.002 || s <= 0.05 {
		return
	}
	var sp ui.Path
	sp.MoveTo(cx, cy-s).
		QuadTo(cx, cy, cx+s, cy).
		QuadTo(cx, cy, cx, cy+s).
		QuadTo(cx, cy, cx-s, cy).
		QuadTo(cx, cy, cx, cy-s).
		Close()
	rc.p.FillPath(&sp, c.Alpha(a))
}

func (rc *sceneRenderCtx) paintHouse(x, y float32, wall, roof, win ui.Color, hasWin, flickerWin bool, winDelay, winDur float32) {
	if rc.alpha <= 0.002 {
		return
	}
	px, py := rc.ox+x, rc.oy+y+rc.layerShiftY
	rc.p.Fill(ui.Rect{X: px - 3, Y: py - 4, W: 6, H: 4.4}, wall.Alpha(rc.alpha), 0)
	var rf ui.Path
	rf.MoveTo(px-3.9, py-4).LineTo(px, py-7.2).LineTo(px+3.9, py-4).Close()
	rc.p.FillPath(&rf, roof.Alpha(rc.alpha))
	rc.p.Fill(ui.Rect{X: px + 1.4, Y: py - 7.5, W: 1.1, H: 2.6}, roof.Alpha(rc.alpha), 0)
	if hasWin {
		wop := float32(1.0)
		if flickerWin && rc.t > 0 {
			ph := rc.loopPhase(rc.t, winDelay, winDur)
			ks := [5][2]float32{{0, 0.75}, {0.43, 1.0}, {0.47, 0.60}, {0.52, 1.0}, {1, 0.75}}
			wop = evalKeyframes1D(ks[:], ph)
		}
		rc.p.Fill(ui.Rect{X: px - 1.9, Y: py - 3.1, W: 1.6, H: 1.6}, win.Alpha(wop*rc.alpha), 0)
	}
}

func (rc *sceneRenderCtx) paintSmoke(x, y float32, c ui.Color) {
	if rc.wm <= 0.002 || rc.alpha <= 0.002 {
		return
	}
	cx, cy := rc.ox+x+1.95, rc.oy+y-8+rc.layerShiftY
	for _, del := range [3]float32{0, 2.2, 4.4} {
		ph := rc.loopPhase(rc.t, del, 6.6)
		easeOut := 1 - (1-ph)*(1-ph)
		var op float32
		if ph < 0.15 {
			op = (ph / 0.15) * 0.75
		} else {
			op = (1 - (ph-0.15)/0.85) * 0.75
		}
		a := op * rc.wm * rc.alpha
		if a <= 0.003 {
			continue
		}
		r := 1.2 * lerp(0.5, 2.0, easeOut)
		var puff ui.Path
		puff.Circle(cx+6*easeOut, cy-13*easeOut, r)
		rc.p.FillPath(&puff, c.Alpha(a))
	}
}

func (rc *sceneRenderCtx) paintLake(lk sceneLake) {
	if rc.alpha <= 0.002 {
		return
	}
	sy := rc.layerShiftY
	var body ui.Path
	fillEllipse(&body, func(x, y float32) (float32, float32) {
		return rc.ox + x, rc.oy + y + sy
	}, lk.cx, lk.cy, lk.rx, lk.ry)
	rc.p.FillPath(&body, lk.fill.Alpha(rc.alpha))

	for i, g := range lk.glints {
		vis := rc.dCalm
		if i == 1 || i == 2 {
			vis = 1
		}
		if vis <= 0.004 {
			continue
		}
		op := float32(0.6)
		sx := float32(1.0)
		if rc.t > 0 {
			u := rc.altWave(rc.t, g.delaySec, g.durSec)
			op = lerp(0.15, 0.95, u)
			sx = lerp(0.5, 1.15, u)
		}
		w := g.w * sx
		gx := rc.ox + g.x + (g.w-w)*0.5
		gy := rc.oy + g.y + sy
		rc.p.Fill(ui.Rect{X: gx, Y: gy, W: w, H: 0.7}, lk.glintColor.Alpha(op*vis*rc.alpha), 0.35)
	}
}

func (rc *sceneRenderCtx) paintCat(c sceneCatSpec) {
	if !rc.showCat || rc.alpha <= 0.002 {
		return
	}
	px, py := rc.ox+c.x, rc.oy+c.y+rc.layerShiftY
	col := c.color.Alpha(rc.alpha)

	draw := func() {
		if c.sleep {
			breatheY := float32(1.0)
			if rc.t > 0 {
				breatheY = lerp(1.0, 1.08, cosWave01(rc.loopPhase(rc.t, 0, 4.4)))
			}
			rc.p.Transform(ui.Scaling(1, breatheY), 0, 0, func() {
				var body ui.Path
				body.MoveTo(-4.6, 0).
					CubeTo(-4.8, -3.2, -1.6, -4.5, 1, -4.3).
					CubeTo(3.7, -4.1, 5, -2.1, 4.7, 0).
					Close()
				rc.p.FillPath(&body, col)
				var e1 ui.Path
				e1.MoveTo(2.2, -3.6).LineTo(2.8, -5.7).LineTo(3.9, -3.9).Close()
				rc.p.FillPath(&e1, col)
				var e2 ui.Path
				e2.MoveTo(3.9, -3.3).LineTo(5.1, -5).LineTo(5.3, -2.7).Close()
				rc.p.FillPath(&e2, col)
			})
			var tail ui.Path
			tail.MoveTo(-4.4, -0.7).QuadTo(-6.3, -2.2, -4.9, -3.4).Cap(ui.RoundCap)
			rc.p.StrokePath(&tail, 1.1, col)
			if c.hasZzz && rc.wm*rc.dCalm > 0.002 {
				for _, del := range [2]float32{0, 2.3} {
					ph := rc.loopPhase(rc.t, del, 4.6)
					easeOut := 1 - (1-ph)*(1-ph)
					var op float32
					if ph < 0.20 {
						op = (ph / 0.20) * 0.85
					} else {
						op = (1 - (ph-0.20)/0.80) * 0.85
					}
					za := op * rc.wm * rc.dCalm * rc.alpha
					if za <= 0.003 {
						continue
					}
					zx := 5.5 + 4*easeOut
					zy := -6 - 7*easeOut
					var zp ui.Path
					zp.MoveTo(zx, zy).LineTo(zx+1.9, zy).LineTo(zx, zy+2.1).LineTo(zx+1.9, zy+2.1).
						Cap(ui.RoundCap).Join(ui.RoundJoin)
					rc.p.StrokePath(&zp, 0.55, c.zzzColor.Alpha(za))
				}
			}
			return
		}

		// Seated cat silhouette
		var body ui.Path
		body.MoveTo(-3, 0).
			CubeTo(-3.5, -3, -2.3, -5.7, 0, -5.9).
			CubeTo(2.1, -5.7, 3.1, -3.2, 3, 0).
			Close()
		rc.p.FillPath(&body, col)

		// Head + ears + optional glowing blinking eyes
		drawHead := func() {
			var hd ui.Path
			hd.Circle(1.3, -7.2, 2.1)
			rc.p.FillPath(&hd, col)
			var el ui.Path
			el.MoveTo(-0.5, -8.3).LineTo(0, -10.7).LineTo(1.3, -9.2).Close()
			rc.p.FillPath(&el, col)

			earDeg := float32(0)
			if rc.t > 0 {
				ph := rc.loopPhase(rc.t, 1.0, 7.9)
				if ph > 0.90 && ph < 0.94 {
					earDeg = 20 * smooth((ph-0.90)/0.04)
				} else if ph >= 0.94 {
					earDeg = 20 * (1 - smooth((ph-0.94)/0.06))
				}
			}
			rc.p.Transform(ui.Rotation(earDeg), 1.84, -8.2, func() {
				var er ui.Path
				er.MoveTo(1.5, -9.2).LineTo(3, -10.6).LineTo(3.2, -8.2).Close()
				rc.p.FillPath(&er, col)
			})
			if c.hasEyes {
				blinkY := float32(1.0)
				if rc.t > 0 {
					ph := rc.loopPhase(rc.t, 0, 6.1)
					if ph > 0.92 && ph < 0.95 {
						blinkY = lerp(1.0, 0.1, smooth((ph-0.92)/0.03))
					} else if ph >= 0.95 {
						blinkY = lerp(0.1, 1.0, smooth((ph-0.95)/0.05))
					}
				}
				eyeCol := c.eyeColor.Alpha(rc.alpha)
				for _, ecx := range [2]float32{0.7, 2.1} {
					rc.p.Transform(ui.Scaling(1, blinkY), ecx, -7.3, func() {
						var ep ui.Path
						ep.Circle(ecx, -7.3, 0.45)
						rc.p.FillPath(&ep, eyeCol)
					})
				}
			}
		}
		if c.lookDeg != 0 {
			rc.p.Transform(ui.Rotation(c.lookDeg), 0.6, -6, drawHead)
		} else {
			drawHead()
		}

		// Swaying tail
		tailDeg := float32(0)
		if rc.t > 0 && c.tailDur > 0 {
			tailDeg = lerp(-8, 15, cosWave01(rc.loopPhase(rc.t, 0, c.tailDur)))
		}
		rc.p.Transform(ui.Rotation(tailDeg), -2.6, -0.7, func() {
			var tp ui.Path
			tp.MoveTo(-2.6, -0.7).QuadTo(-5.8, -0.2, -6.0, -3.7).Cap(ui.RoundCap)
			rc.p.StrokePath(&tp, 1.1, col)
		})
	}

	xform := ui.Translation(px, py)
	if c.flip {
		xform = ui.Scaling(-1, 1).Then(xform)
	}
	rc.p.Transform(xform, 0, 0, draw)
}

func (rc *sceneRenderCtx) paintBlades(blades []sceneBlade, c ui.Color, animMode string) {
	if len(blades) == 0 || rc.alpha <= 0.002 {
		return
	}
	var paths [3]ui.Path
	var used [3]bool
	sy := rc.layerShiftY
	for i := range blades {
		bl := &blades[i]
		if bl.x < -8 || bl.x > rc.w+8 || rc.tier(bl.tier) <= 0.004 {
			continue
		}
		sway := float32(0)
		if rc.t > 0 {
			switch animMode {
			case "gust":
				ph := rc.loopPhase(rc.t, bl.delaySec, bl.durSec)
				ks := [6][2]float32{{0, -3}, {0.18, 14}, {0.30, 3}, {0.40, 8}, {0.58, -3}, {1, -3}}
				sway = float32(math.Tan(float64(evalKeyframes1D(ks[:], ph)) * math.Pi / 180))
			case "flut":
				deg := lerp(3, 13, rc.altWave(rc.t, bl.delaySec, bl.durSec))
				sway = float32(math.Tan(float64(deg) * math.Pi / 180))
			case "reed":
				deg := lerp(-2, 3, rc.altWave(rc.t, bl.delaySec, bl.durSec))
				sway = float32(math.Tan(float64(deg) * math.Pi / 180))
			}
		}
		x0, y0 := rc.ox+bl.x, rc.oy+bl.y+sy
		ctrlX := x0 + bl.lean*0.4 + sway*(bl.h*0.55)
		ctrlY := y0 - bl.h*0.55
		tipX := x0 + bl.lean + sway*bl.h
		tipY := y0 - bl.h
		paths[bl.tier].MoveTo(x0, y0).QuadTo(ctrlX, ctrlY, tipX, tipY)
		used[bl.tier] = true
	}
	for n := range paths {
		if !used[n] {
			continue
		}
		paths[n].Cap(ui.RoundCap)
		rc.p.StrokePath(&paths[n], 1.1, c.Alpha(rc.tier(uint8(n))*rc.alpha))
	}
}

func (rc *sceneRenderCtx) paintMeteors(meteors []sceneMeteor) {
	if rc.wm <= 0.002 || rc.alpha <= 0.002 || len(meteors) == 0 {
		return
	}
	tailCol := sceneColor("#eef2fb")
	for i := range meteors {
		m := &meteors[i]
		vis := rc.tier(m.tier) * rc.fit(max(m.x0+max(m.tailX, 0), m.x0+m.dx)+2)
		if vis <= 0.004 {
			continue
		}
		ph := rc.loopPhase(rc.t, m.delaySec, m.durSec)
		var start, peak, end float32
		if m.isNightMet {
			start, peak, end = 0.88, 0.895, 0.93
		} else {
			start = m.atFrac
			peak = m.atFrac + 0.18/m.durSec
			end = m.atFrac + 0.85/m.durSec
		}
		if ph <= start || ph >= end {
			continue
		}
		u := (ph - start) / (end - start)
		ease := 1 - (1-u)*(1-u)
		var op float32
		if ph < peak {
			op = (ph - start) / max(peak-start, 0.001)
		} else {
			op = 1 - (ph-peak)/max(end-peak, 0.001)
		}
		a := op * 0.95 * vis * rc.wm * rc.alpha
		if a <= 0.003 {
			continue
		}
		hx := rc.ox + m.x0 + m.dx*ease
		hy := rc.oy + m.y0 + m.dy*ease
		var sp ui.Path
		sp.MoveTo(hx+m.tailX, hy+m.tailY).LineTo(hx, hy).Cap(ui.RoundCap)
		ang := float32(math.Atan2(float64(m.dx), float64(-m.dy)) * 180 / math.Pi)
		rc.p.StrokePathGradient(&sp, 1.1, ui.LinearGradient{
			Angle: ang,
			Stops: []ui.GradientStop{
				{Color: tailCol.Alpha(0), At: 0},
				{Color: tailCol.Alpha(a), At: 1},
			},
		})
	}
}

// paintFlock draws a small formation of gliding birds. Each entry is the bird's offset from the
// lead bird at (fx, fy) plus its wing-beat duration in seconds.
func (rc *sceneRenderCtx) paintFlock(flock [][3]float32, fx, fy, size float32, c ui.Color) {
	if rc.alpha <= 0.003 {
		return
	}
	birdCol := c.Alpha(rc.alpha)
	for _, g := range flock {
		bx, by := rc.ox+fx+g[0], rc.oy+fy+g[1]
		if bx < rc.ox-20 || bx > rc.ox+rc.w+20 {
			continue
		}
		w := rc.altWave(rc.t, g[2]*0.5, g[2])
		for _, side := range [2]float32{-1, 1} {
			rc.p.Transform(ui.Rotation(side*lerp(-24, 28, w)), bx, by, func() {
				var wing ui.Path
				wing.MoveTo(bx, by).QuadTo(bx+side*1.6*size, by-2.2*size, bx+side*3.6*size, by-0.6*size).Cap(ui.RoundCap)
				rc.p.StrokePath(&wing, 0.9*size, birdCol)
			})
		}
	}
}

func evalWander(trk wanderTrack, ph float32) (float32, float32) {
	n := len(trk.pts) - 1
	if n <= 0 {
		return 0, 0
	}
	if ph <= trk.restFrac {
		return 0, 0
	}
	u := (ph - trk.restFrac) / (1 - trk.restFrac)
	pos := u * float32(n)
	seg := min(int(pos), n-1)
	local := smooth(pos - float32(seg))
	return lerp(trk.pts[seg].x, trk.pts[seg+1].x, local),
		lerp(trk.pts[seg].y, trk.pts[seg+1].y, local)
}

func evalDrift(item sceneDriftItem, ph float32) (x, y, rot, op float32) {
	n := len(item.pts) - 1
	if n <= 0 {
		return 0, 0, 0, 0
	}
	pos := ph * float32(n)
	seg := min(int(pos), n-1)
	local := smooth(pos - float32(seg))
	a, b := item.pts[seg], item.pts[seg+1]
	return lerp(a.x, b.x, local), lerp(a.y, b.y, local), lerp(a.rot, b.rot, local), lerp(a.op, b.op, local)
}

func paintCrescentMoon(p *ui.Painter, cx, cy, r float32, col ui.Color) {
	const k = 0.5523
	rxIn := r * 0.66
	var m ui.Path
	m.MoveTo(cx, cy-r).
		CubeTo(cx-k*r, cy-r, cx-r, cy-k*r, cx-r, cy).
		CubeTo(cx-r, cy+k*r, cx-k*r, cy+r, cx, cy+r).
		CubeTo(cx-k*rxIn, cy+r, cx-rxIn, cy+k*r, cx-rxIn, cy).
		CubeTo(cx-rxIn, cy-k*r, cx-k*rxIn, cy-r, cx, cy-r).
		Close()
	p.FillPath(&m, col)
}

// -----------------------------------------------------------------------------
// 1. 晨雾 Dawn (Light, 05–10)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintDawn(sc *compiledScene, tr *sceneTerrainCache) {
	rc.fillTerrain(&tr.peakPath, sc.peak.fill)
	rc.paintMist(sc.mists[0])

	// Sun + soft morning rays
	if oa := rc.orbAlpha * rc.alpha; oa > 0.002 {
		sy := sc.far.y(float64(brandOrbX)) + 0.5 + rc.vars.orbDy + rc.orbExtraDY
		cx, cy := rc.ox+brandOrbX, rc.oy+sy
		if rayVis := oa * rc.dCalm; rayVis > 0.004 {
			const rayR = 66
			rayAngles := [4][2]float64{{-158, -146}, {-124, -108}, {-74, -60}, {-38, -26}}
			rayDurs := [4]float32{17, 22, 14, 20}
			rayCol := sceneColor("#fff1e0")
			rayRect := ui.Rect{X: cx - rayR, Y: cy - rayR, W: 2 * rayR, H: 2 * rayR}
			for i, ang := range rayAngles {
				op := float32(0.17)
				if rc.t > 0 {
					op = lerp(0.04, 0.30, rc.altWave(rc.t, float32(i)*3.1, rayDurs[i]))
				}
				// Nested wedges feather the sides of each ray; the radial falloff dissolves its far end,
				// so the rays read as a glow behind the titlebar instead of hard blades across it.
				span := ang[1] - ang[0]
				for _, inset := range [2]float64{0, 0.24} {
					a0 := (ang[0] + span*inset) * math.Pi / 180
					a1 := (ang[1] - span*inset) * math.Pi / 180
					var ray ui.Path
					ray.MoveTo(cx, cy).
						LineTo(cx+float32(1.2*rayR*math.Cos(a0)), cy+float32(1.2*rayR*math.Sin(a0))).
						LineTo(cx+float32(1.2*rayR*math.Cos(a1)), cy+float32(1.2*rayR*math.Sin(a1))).
						Close()
					rc.p.ClipPath(&ray, func() {
						rc.p.FillRadialGradient(rayRect, ui.RadialGradient{
							Radius: rayR,
							Stops: []ui.GradientStop{
								{Color: rayCol.Alpha(0.6 * op * rayVis), At: 0},
								{Color: rayCol.Alpha(0), At: 1},
							},
						}, rayR)
					})
				}
			}
		}
		haloOp, haloScale := float32(0.75), float32(1.0)
		if rc.t > 0 {
			w := cosWave01(rc.loopPhase(rc.t, 2, 9))
			haloOp = lerp(0.55, 1.0, w)
			haloScale = lerp(1.0, 1.14, w)
		}
		hr := 13 * haloScale
		rc.p.FillRadialGradient(
			ui.Rect{X: cx - hr, Y: cy - hr, W: 2 * hr, H: 2 * hr},
			ui.RadialGradient{
				Radius: hr,
				Stops: []ui.GradientStop{
					{Color: sceneColor("#ffdcb8").Alpha(0.95 * haloOp * oa), At: 0},
					{Color: sceneColor("#ffdcb8").Alpha(0), At: 1},
				},
			}, hr,
		)
		var core ui.Path
		core.Circle(cx, cy, 4)
		rc.p.FillPath(&core, sceneColor("#ffc993").Alpha(oa))
	}

	rc.fillTerrain(&tr.farPath, sc.far.fill)
	for i := 1; i <= 5; i++ {
		rc.paintMist(sc.mists[i])
	}
	rc.fillTerrain(&tr.midPath, sc.mid.fill)

	// Cottage, chimney smoke, pines, cat
	hx := float32(108)
	hy := sc.mid.y(float64(hx)) + 1
	rc.layer(rc.dCalm, func() {
		rc.paintSmoke(hx, hy, sceneColor("#ffffff"))
	})
	rc.paintHouse(hx, hy, sceneColor("#cdd8c7"), sceneColor("#bccab5"), sceneColor("#ffd9a8"), true, false, 0, 0)
	pineCol := sceneColor("#c3d2bc").Alpha(rc.alpha)
	for _, px := range [3]float32{96, 119, 124} {
		tx, ty := rc.ox+px, rc.oy+sc.mid.y(float64(px))+1+rc.layerShiftY
		var trp ui.Path
		trp.MoveTo(tx-2, ty-2).LineTo(tx, ty-8).LineTo(tx+2, ty-2).Close()
		rc.p.FillPath(&trp, pineCol)
		rc.p.Line(tx, ty+0.5, tx, ty-3, 0.6, pineCol)
	}
	rc.paintCat(sc.cat)
	rc.paintMist(sc.mists[6])

	rc.fillTerrain(&tr.nearPath, sc.near.fill)
	rc.paintBlades(sc.blades, sc.bladeColor, "")

	// Morning dew glints on grass tips
	if dewVis := rc.dCalm * rc.alpha; dewVis > 0.004 {
		dewCol := sceneColor("#ffffff")
		for _, d := range sc.dewDrops {
			op, scf := float32(0.4), float32(1.0)
			if rc.t > 0 {
				ph := rc.loopPhase(rc.t, d.delaySec, d.durSec)
				if ph > 0.84 && ph < 0.91 {
					u := smooth((ph - 0.84) / 0.07)
					op = lerp(0.4, 1.0, u)
					scf = lerp(1.0, 1.5, u)
				} else if ph >= 0.91 {
					u := smooth((ph - 0.91) / 0.09)
					op = lerp(1.0, 0.4, u)
					scf = lerp(1.5, 1.0, u)
				}
			}
			var dp ui.Path
			dp.Circle(rc.ox+d.x, rc.oy+d.y+rc.layerShiftY, 0.7*scf)
			rc.p.FillPath(&dp, dewCol.Alpha(op*dewVis))
		}
	}
	rc.paintMist(sc.mists[7])

	// A few early birds gliding low across the morning sky
	if vis := rc.wm * rc.dRich; vis > 0.004 {
		if ph := rc.loopPhase(rc.t, 21, 70); ph < 0.30 {
			u := ph / 0.30
			rc.layer(vis*smooth(clampF(min(u, 1-u)*10, 0, 1)), func() {
				flock := [3][3]float32{{0, 0, 0.8}, {-6, 2.2, 0.9}, {-11, -1.6, 0.74}}
				rc.paintFlock(flock[:], lerp(-14, rc.w+26, u), lerp(34.5, 31, u), 0.85, sceneColor("#b3b8a8"))
			})
		}
	}
}

// -----------------------------------------------------------------------------
// 2. 草原 Meadow (Light, 10–15)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintMeadow(sc *compiledScene, tr *sceneTerrainCache) {
	rc.fillTerrain(&tr.peakPath, sc.peak.fill)

	// Sun with slowly spinning rays
	if oa := rc.orbAlpha * rc.alpha; oa > 0.002 {
		sy := sc.far.y(float64(brandOrbX)) - 4 + rc.vars.orbDy + rc.orbExtraDY
		cx, cy := rc.ox+brandOrbX, rc.oy+sy
		if rayA := oa * rc.dCalm; rayA > 0.004 {
			rayDeg := float32(0)
			if rc.t > 0 {
				rayDeg = 360 * rc.loopPhase(rc.t, 0, 80)
			}
			rc.p.Transform(ui.Rotation(rayDeg), cx, cy, func() {
				var rp ui.Path
				for i := range 8 {
					a := float64(i) * math.Pi / 4
					ca, sa := float32(math.Cos(a)), float32(math.Sin(a))
					rp.MoveTo(cx+6.2*ca, cy+6.2*sa).LineTo(cx+8.4*ca, cy+8.4*sa)
				}
				rp.Cap(ui.RoundCap)
				rc.p.StrokePath(&rp, 0.9, sceneColor("#fbdf9c").Alpha(rayA))
			})
		}
		var core ui.Path
		core.Circle(cx, cy, 3.6)
		rc.p.FillPath(&core, sceneColor("#ffdf96").Alpha(oa))
	}

	// Fluffy clouds with underside shadow; a cloud that would not fit inside the sidebar is left out
	// whole rather than sliced by its edge.
	for _, cl := range sc.clouds {
		vis := rc.tier(cl.tier) * rc.fit(cl.x+34*cl.scale+cl.drift)
		if vis <= 0.004 {
			continue
		}
		shadeCol := sceneColor("#e4e9ee").Alpha(vis * rc.alpha)
		whiteCol := sceneColor("#ffffff").Alpha(vis * rc.alpha)
		dx := float32(0)
		if rc.t > 0 {
			dx = lerp(0, cl.drift, rc.altWave(rc.t, cl.delaySec, cl.durSec))
		}
		xform := ui.Scaling(cl.scale, cl.scale).Then(ui.Translation(rc.ox+cl.x+dx, rc.oy+cl.y))
		rc.p.Transform(xform, 0, 0, func() {
			rc.p.Fill(ui.Rect{X: 1, Y: 6, W: 34, H: 5}, shadeCol, 2.5)
			rc.p.Fill(ui.Rect{X: 0, Y: 4, W: 34, H: 6}, whiteCol, 3)
			var puffs ui.Path
			puffs.Circle(11, 4.5, 4.5).Circle(20, 3, 5.5).Circle(27, 6, 3.2)
			rc.p.FillPath(&puffs, whiteCol)
		})
	}

	// Distant hot-air balloon drifting above the hills
	if vis := rc.dRich * rc.fit(76); vis > 0.004 {
		bx, by := float32(28), float32(34)
		if rc.t > 0 {
			u := rc.altWave(rc.t, 30, 170)
			bx += 42 * u
			kby := [5][2]float32{{0, 0}, {0.25, -1.5}, {0.5, 0.5}, {0.75, -1}, {1, 0}}
			by += evalKeyframes1D(kby[:], u)
		}
		a := vis * rc.alpha
		rc.p.Transform(ui.Scaling(0.8, 0.8).Then(ui.Translation(rc.ox+bx, rc.oy+by)), 0, 0, func() {
			var env ui.Path
			env.MoveTo(0, -4.2).
				CubeTo(2.6, -4.2, 3.6, -2.2, 3.4, -0.6).
				CubeTo(3.2, 1, 1.4, 2.4, 0.9, 3.2).
				LineTo(-0.9, 3.2).
				CubeTo(-1.4, 2.4, -3.2, 1, -3.4, -0.6).
				CubeTo(-3.6, -2.2, -2.6, -4.2, 0, -4.2).Close()
			rc.p.FillPath(&env, sceneColor("#f1c6cf").Alpha(a))
			var stripe ui.Path
			stripe.MoveTo(0, -4.2).
				CubeTo(1, -3, 1, 1.6, 0.4, 3.2).
				LineTo(-0.4, 3.2).
				CubeTo(-1, 1.6, -1, -3, 0, -4.2).Close()
			rc.p.FillPath(&stripe, sceneColor("#ffffff").Alpha(a))
			rc.p.Fill(ui.Rect{X: -0.8, Y: 4, W: 1.6, H: 1.3}, sceneColor("#c9b79c").Alpha(a), 0)
		})
	}

	rc.fillTerrain(&tr.farPath, sc.far.fill)
	rc.fillTerrain(&tr.midPath, sc.mid.fill)

	// Grazing sheep: one always, the pair on the left from calm density up
	sheep := [3]struct {
		x, dur float32
		tier   uint8
	}{{38, 9.5, 1}, {50, 13, 1}, {116, 11, 0}}
	for i, sh := range sheep {
		vis := rc.tier(sh.tier) * rc.alpha
		if vis <= 0.004 {
			continue
		}
		legCol := sceneColor("#97a691").Alpha(vis)
		headCol := sceneColor("#93a38d").Alpha(vis)
		woolCol := sceneColor("#ffffff").Alpha(vis)
		sy := sc.mid.y(float64(sh.x)) + 1.2 + rc.layerShiftY
		rc.p.Transform(ui.Translation(rc.ox+sh.x, rc.oy+sy), 0, 0, func() {
			rc.p.Line(-1.6, 0.4, -1.6, -1.2, 0.6, legCol)
			rc.p.Line(1.3, 0.4, 1.3, -1.2, 0.6, legCol)
			var wool ui.Path
			fillEllipse(&wool, func(x, y float32) (float32, float32) { return x, y }, 0, -2.7, 2.7, 1.8)
			rc.p.FillPath(&wool, woolCol)
			gDeg := float32(0)
			if rc.t > 0 {
				ph := rc.loopPhase(rc.t, float32(i)*4.1, sh.dur)
				ks := [5][2]float32{{0, 0}, {0.52, 0}, {0.62, 58}, {0.88, 58}, {1, 0}}
				gDeg = evalKeyframes1D(ks[:], ph)
			}
			rc.p.Transform(ui.Rotation(gDeg), 2.2, -3.3, func() {
				var hd ui.Path
				fillEllipse(&hd, func(x, y float32) (float32, float32) { return x, y }, 3.2, -3.1, 1.1, 0.85)
				rc.p.FillPath(&hd, headCol)
			})
		})
	}

	rc.paintCat(sc.cat)

	// Small cloud shadow gliding across the midground
	if vis := rc.wm * rc.dRich; vis > 0.004 {
		shX := lerp(-60, rc.w+60, rc.loopPhase(rc.t, 14, 80))
		if shX > -44 && shX < rc.w+44 {
			var sh ui.Path
			fillEllipse(&sh, func(x, y float32) (float32, float32) {
				return rc.ox + x, rc.oy + y + rc.layerShiftY
			}, shX, 58, 40, 5)
			rc.p.FillPath(&sh, sceneColor("#c6d9be").Alpha(0.4*vis*rc.alpha))
		}
	}

	rc.fillTerrain(&tr.nearPath, sc.near.fill)

	// Wooden fence
	posts := [6]float32{16, 27, 38, 49, 60, 71}
	var fp ui.Path
	for _, x := range posts {
		y := rc.oy + sc.near.y(float64(x)) + 1 + rc.layerShiftY
		fp.MoveTo(rc.ox+x, y).LineTo(rc.ox+x, y-4)
	}
	for _, dy := range [2]float32{-1.4, -3.0} {
		for i, x := range posts {
			y := rc.oy + sc.near.y(float64(x)) + 1 + dy + rc.layerShiftY
			if i == 0 {
				fp.MoveTo(rc.ox+x, y)
			} else {
				fp.LineTo(rc.ox+x, y)
			}
		}
	}
	fp.Cap(ui.RoundCap)
	rc.p.StrokePath(&fp, 0.7, sceneColor("#b3c8ab").Alpha(rc.alpha))

	// Wildflowers: half from calm density up, the rest only when rich
	for i, fl := range sc.flowers {
		vis := rc.tier(uint8(1 + i%2))
		if vis <= 0.004 {
			continue
		}
		var dot ui.Path
		dot.Circle(rc.ox+fl.x, rc.oy+fl.y+rc.layerShiftY, fl.r)
		rc.p.FillPath(&dot, fl.color.Alpha(vis*rc.alpha))
	}

	// Wind-gusting grass; the taller second layer only joins when rich
	rc.paintBlades(sc.blades, sc.bladeColor, "gust")
	rc.layer(rc.dRich, func() {
		rc.paintBlades(sc.blades2, sc.blade2Color, "gust")
	})

	// Wandering butterfly
	if vis := rc.dCalm; vis > 0.004 {
		bfX, bfY := float32(166), sc.mid.y(166)-5+rc.layerShiftY
		wingSX := float32(1.0)
		if rc.t > 0 {
			dx, dy := evalWander(sc.butterflyTrk, rc.loopPhase(rc.t, 3, 27))
			bfX += dx
			bfY += dy
			wingSX = lerp(1.0, 0.18, rc.altWave(rc.t, 0, 0.55))
		}
		bfCol := sceneColor("#e79bb2").Alpha(vis * rc.alpha)
		rc.p.Transform(ui.Translation(rc.ox+bfX, rc.oy+bfY), 0, 0, func() {
			rc.p.Transform(ui.Scaling(wingSX, 1), 0, -1.5, func() {
				var wl, wr ui.Path
				fillEllipse(&wl, func(x, y float32) (float32, float32) { return x, y }, -1.3, -1.5, 1.4, 1.9)
				fillEllipse(&wr, func(x, y float32) (float32, float32) { return x, y }, 1.3, -1.5, 1.4, 1.9)
				rc.p.FillPath(&wl, bfCol)
				rc.p.FillPath(&wr, bfCol)
			})
		})
	}
}

// -----------------------------------------------------------------------------
// 3. 微风 Breeze (Light, 15–18)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintBreeze(sc *compiledScene, tr *sceneTerrainCache) {
	rc.fillTerrain(&tr.peakPath, sc.peak.fill)

	// Soft afternoon sun
	if oa := rc.orbAlpha * rc.alpha; oa > 0.002 {
		sy := sc.far.y(float64(brandOrbX)) - 6 + rc.vars.orbDy + rc.orbExtraDY
		cx, cy := rc.ox+brandOrbX, rc.oy+sy
		var outer, inner ui.Path
		outer.Circle(cx, cy, 6.5)
		rc.p.FillPath(&outer, sceneColor("#fff3cf").Alpha(0.8*oa))
		inner.Circle(cx, cy, 3.4)
		rc.p.FillPath(&inner, sceneColor("#ffe2a4").Alpha(oa))
	}

	rc.fillTerrain(&tr.farPath, sc.far.fill)

	// Flowing wind ribbons; only the ones that fit inside the sidebar are drawn
	if vis := rc.wm * rc.dCalm; vis > 0.002 {
		rc.p.Transform(ui.Translation(rc.ox, rc.oy+rc.layerShiftY), 0, 0, func() {
			for i := range sc.winds {
				wr := &sc.winds[i]
				fit := rc.fit(wr.endX)
				if fit <= 0.004 {
					continue
				}
				ph := rc.loopPhase(rc.t, wr.delaySec, wr.durSec)
				if ph <= 0 || ph >= 0.46 {
					continue
				}
				var op float32
				switch {
				case ph < 0.06:
					op = ph / 0.06
				case ph < 0.36:
					op = 1.0
				default:
					op = 1 - (ph-0.36)/0.10
				}
				a := op * vis * fit * rc.alpha
				if a <= 0.003 {
					continue
				}
				u := smooth(ph / 0.46)
				seg := min(float32(0.38), 62.0/max(wr.approxL, 80))
				head := lerp(0, 1+seg, u)
				from := clampF(head-seg, 0, 1)
				to := clampF(head, 0, 1)
				if to <= from+0.002 {
					continue
				}
				paintBreezeWindPath(rc.p, i, from, to, wr.color.Alpha(a))
			}
		})
	}

	// Kite on a long string, flying just above the far hills
	if vis := rc.dRich * rc.fit(72); vis > 0.004 {
		kiteDeg := float32(0)
		tailSkew := float32(0)
		if rc.t > 0 {
			kiteDeg = lerp(-4, 5, rc.altWave(rc.t, 1, 7))
			tailSkew = lerp(-16, 18, rc.altWave(rc.t, 0, 1.8))
		}
		a := vis * rc.alpha
		const ax = 20
		rc.p.Transform(ui.Translation(rc.ox+ax, rc.oy+sc.mid.y(ax)+1+rc.layerShiftY), 0, 0, func() {
			rc.p.Transform(ui.Rotation(kiteDeg), 0, 0, func() {
				var str ui.Path
				str.MoveTo(0, 0).QuadTo(19, -5, 40, -13)
				rc.p.StrokePath(&str, 0.45, sceneColor("#b9c4b4").Alpha(a))
				var body ui.Path
				body.MoveTo(40, -21).LineTo(42.8, -17).LineTo(40, -13).LineTo(37.2, -17).Close()
				rc.p.FillPath(&body, sceneColor("#ecb1a5").Alpha(a))
				var spar ui.Path
				spar.MoveTo(40, -21).LineTo(40, -13).MoveTo(37.2, -17).LineTo(42.8, -17)
				rc.p.StrokePath(&spar, 0.4, sceneColor("#d99a8d").Alpha(a))
				rc.p.Transform(ui.Skewing(tailSkew, 0), 40, -13, func() {
					var tl ui.Path
					tl.MoveTo(40, -13).QuadTo(38.4, -11, 40, -9.2).QuadTo(41.6, -7.6, 40, -5.6).Cap(ui.RoundCap)
					rc.p.StrokePath(&tl, 0.6, sceneColor("#ecb1a5").Alpha(a))
				})
			})
		})
	}

	rc.fillTerrain(&tr.midPath, sc.mid.fill)

	// Windmill at x=110 with variable-speed breeze rotation
	mx := float32(110)
	my := sc.mid.y(float64(mx)) + 1 + rc.layerShiftY
	millDeg := float32(0)
	if rc.t > 0 {
		ph := rc.loopPhase(rc.t, 2, 18)
		ks := [5][2]float32{{0, 0}, {0.22, 150}, {0.48, 250}, {0.70, 460}, {1, 720}}
		millDeg = evalKeyframes1D(ks[:], ph)
	}
	rc.p.Transform(ui.Translation(rc.ox+mx, rc.oy+my), 0, 0, func() {
		var tw ui.Path
		tw.MoveTo(-1.7, 0.5).LineTo(-0.9, -9).LineTo(0.9, -9).LineTo(1.7, 0.5).Close()
		rc.p.FillPath(&tw, sceneColor("#c6d4c0").Alpha(rc.alpha))
		armCol := sceneColor("#b5c5ae").Alpha(rc.alpha)
		sailCol := sceneColor("#dbe5d6").Alpha(rc.alpha)
		rc.p.Transform(ui.Rotation(millDeg), 0, -9, func() {
			for _, a := range [4]float32{0, 90, 180, 270} {
				rc.p.Transform(ui.Rotation(a), 0, -9, func() {
					rc.p.Line(0, -9, 0, -15.8, 0.6, armCol)
					rc.p.Fill(ui.Rect{X: 0.3, Y: -15.8, W: 1.5, H: 4.8}, sailCol, 0)
				})
			}
			var hub ui.Path
			hub.Circle(0, -9, 0.8)
			rc.p.FillPath(&hub, armCol)
		})
	})

	rc.paintCat(sc.cat)
	rc.fillTerrain(&tr.nearPath, sc.near.fill)
	rc.paintBlades(sc.blades, sc.bladeColor, "flut")

	// Dandelions: one always, a second from calm density up, a third when rich
	for _, dn := range [3][2]float32{{62, 0}, {206, 1}, {188, 2}} {
		vis := rc.tier(uint8(dn[1])) * rc.fit(dn[0]+4) * rc.alpha
		if vis <= 0.004 {
			continue
		}
		stemCol := sceneColor("#a9c69f").Alpha(vis)
		puffCol := sceneColor("#b9c6b3").Alpha(vis)
		dy := sc.near.y(float64(dn[0])) + 1 + rc.layerShiftY
		rc.p.Transform(ui.Translation(rc.ox+dn[0], rc.oy+dy), 0, 0, func() {
			var st ui.Path
			st.MoveTo(0, 0).QuadTo(1.6, -5, 1, -9.5).Cap(ui.RoundCap)
			rc.p.StrokePath(&st, 0.9, stemCol)
			var ring ui.Path
			ring.Circle(1, -11.5, 2.5).Dash(0, 0.6, 1.2).Cap(ui.RoundCap)
			rc.p.StrokePath(&ring, 0.7, puffCol)
			var ctr ui.Path
			ctr.Circle(1, -11.5, 0.7)
			rc.p.FillPath(&ctr, puffCol)
		})
	}

	// Dandelion seeds and tumbling leaves carried off by the breeze
	if rc.wm > 0.002 {
		for _, item := range sc.driftItems {
			vis := rc.tier(item.tier)
			if vis <= 0.004 {
				continue
			}
			ph := rc.loopPhase(rc.t, item.delaySec, item.durSec)
			x, y, rot, op := evalDrift(item, ph)
			// Stretch the flight so it always ends just past the sidebar edge, whatever its width.
			if x0, x1 := item.pts[0].x, item.pts[len(item.pts)-1].x; x1 > x0 {
				x = x0 + (x-x0)*(rc.w+8-x0)/(x1-x0)
			}
			a := op * vis * rc.wm * rc.alpha
			if a <= 0.003 || x < -20 || x > rc.w+20 {
				continue
			}
			c := item.color.Alpha(a)
			rc.p.Transform(ui.Rotation(rot).Then(ui.Translation(rc.ox+x, rc.oy+y)), 0, 0, func() {
				if item.isLeaf {
					var lf ui.Path
					lf.MoveTo(-1.8, 0).QuadTo(0, -1.3, 1.8, 0).QuadTo(0, 1.3, -1.8, 0).Close()
					rc.p.FillPath(&lf, c)
				} else {
					var sd ui.Path
					sd.MoveTo(0, 0).LineTo(0, 3.2).
						MoveTo(0, 0).LineTo(-1.9, -1.5).
						MoveTo(0, 0).LineTo(-0.7, -2.3).
						MoveTo(0, 0).LineTo(0.7, -2.3).
						MoveTo(0, 0).LineTo(1.9, -1.5).
						Cap(ui.RoundCap)
					rc.p.StrokePath(&sd, 0.55, c)
				}
			})
		}
	}
}

func paintBreezeWindPath(p *ui.Painter, idx int, from, to float32, c ui.Color) {
	var wp ui.Path
	switch idx {
	case 0:
		wp.MoveTo(24, 41).CubeTo(54, 33, 84, 47, 120, 39).CubeTo(156, 31, 160, 29, 184, 35)
	case 1:
		wp.MoveTo(330, 31).CubeTo(370, 25, 400, 35, 430, 29).
			CubeTo(444, 26, 446, 15, 436, 15).
			CubeTo(427, 15, 428, 26, 442, 27).
			CubeTo(462, 28, 482, 21, 512, 23)
	case 2:
		wp.MoveTo(612, 21).CubeTo(662, 28, 702, 13, 762, 19)
	case 3:
		wp.MoveTo(770, 35).CubeTo(800, 31, 814, 37, 850, 32).CubeTo(886, 27, 900, 26, 924, 32)
	case 4:
		wp.MoveTo(946, 25).CubeTo(976, 29, 1006, 19, 1030, 23).
			CubeTo(1042, 25, 1042, 12, 1033, 13).
			CubeTo(1025, 14, 1028, 23, 1041, 23).
			CubeTo(1065, 23, 1081, 15, 1113, 18)
	case 5:
		wp.MoveTo(44, 53).CubeTo(84, 47, 124, 59, 176, 51)
	case 6:
		wp.MoveTo(690, 9).CubeTo(726, 5, 754, 14, 794, 9)
	case 7:
		wp.MoveTo(1260, 22).CubeTo(1304, 16, 1338, 28, 1392, 19)
	case 8:
		wp.MoveTo(1560, 30).CubeTo(1594, 35, 1626, 23, 1658, 28).
			CubeTo(1670, 30, 1670, 17, 1661, 18).
			CubeTo(1653, 19, 1656, 28, 1669, 28).
			CubeTo(1693, 28, 1709, 20, 1741, 24)
	default:
		return
	}
	wp.Trim(from, to).Cap(ui.RoundCap)
	p.StrokePath(&wp, 1.2, c)
}

// -----------------------------------------------------------------------------
// 4. 晚霞 Dusk (Light, 18–05)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintDusk(sc *compiledScene, tr *sceneTerrainCache) {
	// Emerging stars as evening deepens
	rc.paintStars(sc.stars, rc.vars.starOp)

	// Evening sparkle in the strip above the titlebar controls
	if vis := rc.dRich; vis > 0.004 {
		spOp := float32(0.7)
		if rc.t > 0 {
			spOp = lerp(0.20, 1.0, cosWave01(rc.loopPhase(rc.t, 1, 5.5)))
		}
		rc.paintSparkle(rc.ox+rc.w-34, rc.oy+5.5, 2.0, sceneColor("#ffffff"), spOp*vis)
	}

	rc.fillTerrain(&tr.peakPath, sc.peak.fill)

	// Setting sun
	if oa := rc.orbAlpha * rc.alpha; oa > 0.002 {
		sy := sc.far.y(float64(brandOrbX)) - 1 + rc.vars.orbDy + rc.orbExtraDY
		cx, cy := rc.ox+brandOrbX, rc.oy+sy
		haloOp, haloScale := float32(0.75), float32(1.0)
		if rc.t > 0 {
			w := cosWave01(rc.loopPhase(rc.t, 4, 11))
			haloOp = lerp(0.55, 1.0, w)
			haloScale = lerp(1.0, 1.14, w)
		}
		r := 14 * haloScale
		rc.p.FillRadialGradient(ui.Rect{X: cx - r, Y: cy - r, W: 2 * r, H: 2 * r}, ui.RadialGradient{
			Radius: r,
			Stops: []ui.GradientStop{
				{Color: sceneColor("#f7c9ab").Alpha(0.95 * haloOp * oa), At: 0},
				{Color: sceneColor("#f7c9ab").Alpha(0), At: 1},
			},
		}, r)
		var core ui.Path
		core.Circle(cx, cy, 4.6)
		rc.p.FillPath(&core, sceneColor("#f2a679").Alpha(oa))
	}

	rc.fillTerrain(&tr.farPath, sc.far.fill)
	rc.fillTerrain(&tr.midPath, sc.mid.fill)

	// Round-canopy trees: one always, a smaller companion from calm density up
	for _, ts := range [2][3]float32{{56, 1.0, 0}, {43, 0.7, 1}} {
		vis := rc.tier(uint8(ts[2])) * rc.alpha
		if vis <= 0.004 {
			continue
		}
		treeCol := sceneColor("#cbc3b5").Alpha(vis)
		tx, ty := rc.ox+ts[0], rc.oy+sc.mid.y(float64(ts[0]))+1+rc.layerShiftY
		rc.p.Transform(ui.Scaling(ts[1], ts[1]).Then(ui.Translation(tx, ty)), 0, 0, func() {
			rc.p.Fill(ui.Rect{X: -0.6, Y: -6, W: 1.2, H: 7}, treeCol, 0)
			var canopy ui.Path
			canopy.Circle(0, -8.4, 3.4).Circle(-2.6, -6.6, 2.5).Circle(2.6, -6.8, 2.3)
			rc.p.FillPath(&canopy, treeCol)
		})
	}

	// Lit cottage, cat, shimmering sunset lake
	rc.paintHouse(82, sc.mid.y(82)+1, sceneColor("#cfc7b9"), sceneColor("#c0b7a8"), sceneColor("#ffc97e"), true, true, 0, 7.7)
	rc.paintCat(sc.cat)
	rc.paintLake(sc.lake)
	rc.fillTerrain(&tr.nearPath, sc.near.fill)
	rc.paintBlades(sc.blades, sc.bladeColor, "")

	// A small V of wild geese heading home, right to left
	if vis := rc.wm * rc.dRich; vis > 0.004 {
		if ph := rc.loopPhase(rc.t, 10, 62); ph < 0.45 {
			u := ph / 0.45
			rc.layer(vis*smooth(clampF(min(u, 1-u)*10, 0, 1)), func() {
				flock := [5][3]float32{{0, 0, 0.67}, {6, 1.8, 0.75}, {12, 3.6, 0.62}, {6, -1.8, 0.72}, {12, -3.6, 0.8}}
				rc.paintFlock(flock[:], lerp(rc.w+16, -40, u), lerp(34.5, 32, u), 0.8, sceneColor("#b49c8e"))
			})
		}
	}
}

// -----------------------------------------------------------------------------
// 5. 破晓 Daybreak (Dark, 04–07)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintDaybreak(sc *compiledScene, tr *sceneTerrainCache) {
	// Swelling dawn horizon glow (a1, scaled by glowOp)
	if gop := rc.vars.glowOp; gop > 0.002 {
		bandOp := float32(0.75)
		swellOp, swellScale := float32(0.85), float32(1.0)
		if rc.t > 0 {
			bandOp = lerp(0.40, 1.0, rc.altWave(rc.t, 3, 17))
			sw := rc.altWave(rc.t, 1, 13)
			swellOp = lerp(0.65, 1.0, sw)
			swellScale = lerp(0.90, 1.12, sw)
		}
		rc.ellipseGlow(rc.ox+brandOrbX, rc.oy+42, 760, 9, sceneColor("#f0aa80"), 0.20*bandOp*gop)
		rc.ellipseGlow(rc.ox+brandOrbX, rc.oy+58, 172*swellScale, 48*swellScale, sceneColor("#eca076"), 0.30*swellOp*gop)
	}

	// Fading pre-dawn stars
	rc.paintStars(sc.stars, rc.vars.starOp)

	// Morning star (Venus), low in the west under the window controls
	if vis := rc.dRich; vis > 0.004 {
		vScale, vOp := float32(0.95), float32(0.8)
		if rc.t > 0 {
			w := cosWave01(rc.loopPhase(rc.t, 0, 6.5))
			vScale = lerp(0.70, 1.15, w)
			vOp = lerp(0.55, 1.0, w)
		}
		vx, vy := rc.ox+38, rc.oy+36
		rc.paintSparkle(vx, vy, 2.6*vScale, sceneColor("#fff1d8"), vOp*vis)
		var vc ui.Path
		vc.Circle(vx, vy, 0.8)
		rc.p.FillPath(&vc, sceneColor("#fffaf0").Alpha(vis*rc.alpha))
	}
	// Peaks + warm sunrise rim light
	rc.fillTerrain(&tr.peakPath, sc.peak.fill)
	rc.strokeRim(&tr.peakRim, 0.7, sc.rimColor, sc.rimMidStop, sc.rimMidOp, sc.rimPeakOp)

	// Horizon glow orb
	if oa := rc.orbAlpha * rc.alpha; oa > 0.002 {
		haloOp, haloScale := float32(0.75), float32(1.0)
		if rc.t > 0 {
			w := cosWave01(rc.loopPhase(rc.t, 2, 9))
			haloOp = lerp(0.55, 1.0, w)
			haloScale = lerp(1.0, 1.14, w)
		}
		cx, cy := rc.ox+brandOrbX, rc.oy+57+rc.orbExtraDY
		r := 9 * haloScale
		rc.p.FillRadialGradient(ui.Rect{X: cx - r, Y: cy - r, W: 2 * r, H: 2 * r}, ui.RadialGradient{
			Radius: r,
			Stops: []ui.GradientStop{
				{Color: sceneColor("#f2b48a").Alpha(0.5 * haloOp * oa), At: 0},
				{Color: sceneColor("#f2b48a").Alpha(0), At: 1},
			},
		}, r)
	}

	// Far ridge + warm sunrise rim light
	rc.fillTerrain(&tr.farPath, sc.far.fill)
	rc.strokeRim(&tr.farRim, 0.9, sc.rimColor, sc.rimMidStop, sc.rimMidOp, 1.0)

	// Cottage, smoke, dark pines, sleeping cat, morning mist
	hx := float32(104)
	hy := sc.far.y(float64(hx)) + 1
	rc.layer(rc.dCalm, func() {
		rc.paintSmoke(hx, hy, sceneColor("#8c93a6"))
	})
	rc.paintHouse(hx, hy, rc.adaptGround(sceneColor("#1f2027")), rc.adaptGround(sceneColor("#1a1b21")), sceneColor("#ffc97e"), true, true, 2, 8.3)
	for _, pn := range [5][2]float32{{92, 0}, {116, 0}, {121, 1}, {198, 1}, {204, 1}} {
		vis := rc.tier(uint8(pn[1])) * rc.fit(pn[0]+3) * rc.alpha
		if vis <= 0.004 {
			continue
		}
		tx, ty := rc.ox+pn[0], rc.oy+sc.far.y(float64(pn[0]))+0.4+rc.layerShiftY
		var pp ui.Path
		pp.MoveTo(tx-2.2, ty+0.6).LineTo(tx, ty-8).LineTo(tx+2.2, ty+0.6).Close()
		rc.p.FillPath(&pp, rc.adaptGround(sceneColor("#2a2a33")).Alpha(vis))
	}
	rc.paintCat(sc.cat)
	rc.paintMist(sc.mists[0])
	rc.fillTerrain(&tr.nearPath, sc.near.fill)
	rc.paintBlades(sc.blades, sc.bladeColor, "")
	rc.paintMist(sc.mists[1])
}

// -----------------------------------------------------------------------------
// 6. 萤火 Firefly (Dark, 07–20)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintFirefly(sc *compiledScene, tr *sceneTerrainCache) {
	rc.paintStars(sc.stars, 1.0)
	rc.fillTerrain(&tr.peakPath, sc.peak.fill)
	rc.fillTerrain(&tr.farPath, sc.far.fill)
	rc.paintCat(sc.cat)

	glowCol := sceneColor("#d7f08a")
	coreCol := sceneColor("#e9fbb0")

	drawFly := func(ff *sceneFirefly) {
		// A fly that would hover in the sidebar's edge feather is left out whole.
		vis := rc.tier(ff.tier) * rc.fit(ff.x+5)
		if vis <= 0.004 {
			return
		}
		dx, dy := float32(0), float32(0)
		blink := float32(0.85)
		if rc.t > 0 {
			dx, dy = evalWander(sc.ffWanders[ff.wanderIdx], rc.loopPhase(rc.t, ff.moveDelaySec, ff.moveDurSec))
			if ff.flipX {
				dx = -dx
			}
			blink = evalFireflyBlinkV2(ff.blinkKind, rc.loopPhase(rc.t, ff.blinkDelaySec, ff.blinkDurSec))
		}
		cx, cy := rc.ox+ff.x+dx, rc.oy+ff.y+dy+rc.layerShiftY
		rc.radialGlow(cx, cy, 4.6*ff.size, glowCol, 0.5*0.85*blink*vis)
		var core ui.Path
		core.Circle(cx, cy, 1.1*ff.size)
		rc.p.FillPath(&core, coreCol.Alpha(0.85*blink*vis*rc.alpha))
	}

	// Distant small fireflies behind pond & near ridge
	for i := range sc.farFlies {
		drawFly(&sc.farFlies[i])
	}

	// Pond + rim + expanding ripples + firefly water reflections
	sy := rc.layerShiftY
	var pond ui.Path
	fillEllipse(&pond, func(x, y float32) (float32, float32) {
		return rc.ox + x, rc.oy + y + sy
	}, 176, 60, 44, 3.2)
	rc.p.FillPath(&pond, rc.adaptGround(sceneColor("#1b232d")).Alpha(rc.alpha))
	var rim ui.Path
	rim.MoveTo(rc.ox+134, rc.oy+59.4+sy).QuadTo(rc.ox+176, rc.oy+56.4+sy, rc.ox+218, rc.oy+59.4+sy)
	rc.p.StrokePath(&rim, 0.5, rc.adaptAccent(sceneColor("#37475a"), 0.55).Alpha(0.7*rc.alpha))

	if rc.wm > 0.002 {
		ripCol := sceneColor("#63788c")
		for _, rp := range [2][3]float32{{160, 0, 7.3}, {192, 3.9, 9.7}} {
			ph := rc.loopPhase(rc.t, rp[1], rp[2])
			if ph >= 0.60 {
				continue
			}
			u := ph / 0.60
			easeOut := 1 - (1-u)*(1-u)
			var op float32
			if ph < 0.10 {
				op = (ph / 0.10) * 0.70
			} else {
				op = (1 - (ph-0.10)/0.50) * 0.70
			}
			a := op * rc.wm * rc.dRich * rc.alpha
			if a <= 0.003 {
				continue
			}
			s := lerp(0.2, 1.3, easeOut)
			var ep ui.Path
			fillEllipse(&ep, func(x, y float32) (float32, float32) {
				return rc.ox + x, rc.oy + y + sy
			}, rp[0], 60.4, 9*s, 1.5*s)
			rc.p.StrokePath(&ep, 0.5, ripCol.Alpha(a))
		}
	}
	reflXs := [4]float32{150, 171, 187, 203}
	for i, rx := range reflXs {
		op := float32(0.6)
		if rc.t > 0 {
			op = lerp(0.20, 1.0, cosWave01(rc.loopPhase(rc.t, sc.pondReflDel[i], sc.pondReflDur[i])))
		}
		var dot ui.Path
		dot.Circle(rc.ox+rx, rc.oy+sc.pondReflY[i]+sy, 0.55)
		rc.p.FillPath(&dot, glowCol.Alpha(op*rc.dCalm*rc.alpha))
	}

	rc.fillTerrain(&tr.nearPath, sc.near.fill)
	rc.paintBlades(sc.blades, sc.bladeColor, "reed")

	// Cattails: a pair always, more as the density rises
	for i, ct := range [5][2]float32{{38, 0}, {47, 0}, {112, 1}, {200, 2}, {207, 2}} {
		vis := rc.tier(uint8(ct[1])) * rc.fit(ct[0]+3) * rc.alpha
		if vis <= 0.004 {
			continue
		}
		stalkCol := sceneColor("#34413c").Alpha(vis)
		headCol := sceneColor("#3e4c45").Alpha(vis)
		cx := ct[0]
		cy := sc.near.y(float64(cx)) + 1.2 + sy
		skew := float32(0)
		if rc.t > 0 {
			skew = lerp(-2, 3, rc.altWave(rc.t, sc.cattailDel[i], sc.cattailDur[i]))
		}
		rc.p.Transform(ui.Skewing(skew, 0).Then(ui.Translation(rc.ox+cx, rc.oy+cy)), 0, 0, func() {
			var st ui.Path
			st.MoveTo(0, 0).QuadTo(1, -7, 0.4, -13).Cap(ui.RoundCap)
			rc.p.StrokePath(&st, 0.9, stalkCol)
			var hd ui.Path
			fillEllipse(&hd, func(x, y float32) (float32, float32) { return x, y }, 0.5, -13.5, 1.1, 2.7)
			rc.p.FillPath(&hd, headCol)
		})
	}
	// Glowing mushrooms: one always, the others from calm density up
	mushSpecs := [3][3]float32{{20, 1.0, 0}, {25, 0.7, 1}, {96, 0.85, 1}}
	for i, ms := range mushSpecs {
		vis := rc.tier(uint8(ms[2]))
		if vis <= 0.004 {
			continue
		}
		mStem := sceneColor("#56655e").Alpha(vis * rc.alpha)
		mCap := sceneColor("#7f9c8c").Alpha(vis * rc.alpha)
		mx, my := rc.ox+ms[0], rc.oy+sc.near.y(float64(ms[0]))+1+sy
		gOp := float32(0.65)
		if rc.t > 0 {
			gOp = lerp(0.20, 1.0, cosWave01(rc.loopPhase(rc.t, sc.mushDel[i], sc.mushDur[i])))
		}
		rc.radialGlow(mx, my-2.4*ms[1], 4*ms[1], glowCol, 0.5*gOp*vis)
		rc.p.Transform(ui.Scaling(ms[1], ms[1]).Then(ui.Translation(mx, my)), 0, 0, func() {
			rc.p.Fill(ui.Rect{X: -0.4, Y: -2, W: 0.8, H: 2.6}, mStem, 0)
			var cap ui.Path
			cap.MoveTo(-2.1, -2).CubeTo(-2.1, -4.53, 2.1, -4.53, 2.1, -2).Close()
			rc.p.FillPath(&cap, mCap)
		})
	}
	// Foreground & upper-sky fireflies
	for i := range sc.fireflies {
		drawFly(&sc.fireflies[i])
	}

	// Traveling firefly wandering across the sidebar sky, below the titlebar controls
	if vis := rc.wm * rc.dRich; vis > 0.002 {
		ph := rc.loopPhase(rc.t, 44, 47)
		if ph < 0.56 {
			kpts := [7][4]float32{
				{0.00, 0, 0, 0},
				{0.04, 40, -6, 1},
				{0.14, 150, -12, 1},
				{0.24, 260, -3, 1},
				{0.36, 420, -14, 1},
				{0.48, 560, -6, 1},
				{0.56, 680, -16, 0},
			}
			var tx, ty, top float32
			for i := 0; i < len(kpts)-1; i++ {
				if ph <= kpts[i+1][0] {
					u := smooth((ph - kpts[i][0]) / (kpts[i+1][0] - kpts[i][0]))
					tx = lerp(kpts[i][1], kpts[i+1][1], u)
					ty = lerp(kpts[i][2], kpts[i+1][2], u)
					top = lerp(kpts[i][3], kpts[i+1][3], u)
					break
				}
			}
			cx := rc.ox + 20 + tx*(rc.w-50)/680
			cy := rc.oy + 40 + ty*0.5
			blink := evalFireflyBlinkV2(ffBlinkSingle, rc.loopPhase(rc.t, 0, 3.4))
			if fa := top * blink * vis; fa > 0.003 {
				rc.radialGlow(cx, cy, 4.6, glowCol, 0.5*fa)
				var core ui.Path
				core.Circle(cx, cy, 1.1)
				rc.p.FillPath(&core, coreCol.Alpha(fa*rc.alpha))
			}
		}
	}
}

func evalFireflyBlinkV2(kind fireflyBlinkKind, ph float32) float32 {
	switch kind {
	case ffBlinkDouble:
		ks := [6][2]float32{{0, 0.08}, {0.10, 1.0}, {0.18, 0.20}, {0.28, 1.0}, {0.45, 0.08}, {1, 0.08}}
		return evalKeyframes1D(ks[:], ph)
	case ffBlinkSlow:
		ks := [4][2]float32{{0, 0.08}, {0.30, 0.90}, {0.70, 0.90}, {1, 0.08}}
		return evalKeyframes1D(ks[:], ph)
	default:
		ks := [4][2]float32{{0, 0.10}, {0.40, 1.0}, {0.60, 1.0}, {1, 0.10}}
		return evalKeyframes1D(ks[:], ph)
	}
}

// -----------------------------------------------------------------------------
// 7. 星空 Night (Dark, 20–01)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintNight(sc *compiledScene, tr *sceneTerrainCache) {

	rc.paintStars(sc.stars, 1.0)

	// Constellations, drawn small in the sky under the titlebar controls: the Big Dipper on the
	// left, Cassiopeia further right where a wide sidebar leaves room for it.
	if vis := rc.dRich; vis > 0.004 {
		dip := [7][2]float32{{20, 32.25}, {26.5, 31}, {32.5, 32.25}, {38.5, 34.25}, {44, 38.75}, {52, 39.75}, {54, 34.5}}
		cas := [5][2]float32{{296, 39.5}, {301, 34}, {307, 37.5}, {313, 32}, {319.5, 36}}
		constellation := func(pts [][2]float32, closeTo int, a float32) {
			if a <= 0.004 {
				return
			}
			var lp ui.Path
			for i, pt := range pts {
				if i == 0 {
					lp.MoveTo(rc.ox+pt[0], rc.oy+pt[1])
				} else {
					lp.LineTo(rc.ox+pt[0], rc.oy+pt[1])
				}
			}
			if closeTo >= 0 {
				lp.LineTo(rc.ox+pts[closeTo][0], rc.oy+pts[closeTo][1])
			}
			lp.Join(ui.RoundJoin)
			rc.p.StrokePath(&lp, 0.45, rc.adaptAccent(sceneColor("#8ea4d8"), 0.70).Alpha(0.30*a))
			for _, pt := range pts {
				var dot ui.Path
				dot.Circle(rc.ox+pt[0], rc.oy+pt[1], 0.7)
				rc.p.FillPath(&dot, rc.adaptAccent(sceneColor("#eef2fb"), 0.65).Alpha(a))
			}
		}
		constellation(dip[:], 3, vis*rc.alpha)
		constellation(cas[:], -1, vis*rc.fit(322)*rc.alpha)
	}
	// Flare stars
	rc.layer(rc.dCalm, func() {
		flareCol := rc.adaptAccent(sceneColor("#f4f7ff"), 0.65)
		for _, fl := range sc.flares {
			fit := rc.fit(fl.x + fl.size)
			if fit <= 0.004 {
				continue
			}
			fx, fy := rc.ox+fl.x, rc.oy+fl.y
			var core ui.Path
			core.Circle(fx, fy, 0.8)
			rc.p.FillPath(&core, flareCol.Alpha(fit*rc.alpha))
			scf, op := float32(0.3), float32(0.3)
			if rc.t > 0 {
				ph := rc.loopPhase(rc.t, fl.delaySec, fl.durSec)
				if ph > 0.80 && ph < 0.90 {
					u := smooth((ph - 0.80) / 0.10)
					scf, op = lerp(0.3, 1.0, u), lerp(0.3, 1.0, u)
				} else if ph >= 0.90 {
					u := smooth((ph - 0.90) / 0.10)
					scf, op = lerp(1.0, 0.3, u), lerp(1.0, 0.3, u)
				}
			}
			rc.paintSparkle(fx, fy, fl.size*scf, flareCol, op*fit)
		}
	})
	// Passing satellite, gliding through the strip above the titlebar controls
	if vis := rc.wm * rc.dRich; vis > 0.002 {
		ph := rc.loopPhase(rc.t, 58, 61)
		if ph < 0.37 {
			u := ph / 0.37
			var op float32
			switch {
			case ph < 0.04:
				op = (ph / 0.04) * 0.95
			case ph < 0.33:
				op = 0.95
			default:
				op = (1 - (ph-0.33)/0.04) * 0.95
			}
			var sat ui.Path
			sat.Circle(rc.ox+8+(rc.w-30)*u, rc.oy+4+3*u, 0.85)
			rc.p.FillPath(&sat, sceneColor("#e9eefc").Alpha(op*vis*rc.alpha))
		}
	}
	rc.paintMeteors(sc.meteors)
	rc.fillTerrain(&tr.peakPath, sc.peak.fill)
	rc.fillTerrain(&tr.farPath, sc.far.fill)

	// Pines, a shade darker than the ridge so their silhouettes read against it
	pines := [8][3]float32{
		{18, 1.0, 0}, {27, 0.75, 1}, {33, 1.15, 0}, {62, 0.9, 1},
		{207, 0.9, 0}, {214, 0.7, 1}, {246, 1.1, 1}, {253, 0.8, 1},
	}
	for _, pn := range pines {
		vis := rc.tier(uint8(pn[2])) * rc.fit(pn[0]+3) * rc.alpha
		if vis <= 0.004 {
			continue
		}
		pineCol := rc.adaptGround(sceneColor("#1c2029")).Alpha(vis)
		px, py := rc.ox+pn[0], rc.oy+sc.far.y(float64(pn[0]))+0.4+rc.layerShiftY
		rc.p.Transform(ui.Scaling(pn[1], pn[1]).Then(ui.Translation(px, py)), 0, 0, func() {
			var pp ui.Path
			pp.MoveTo(-2.3, 0.6).LineTo(0, -7.5).LineTo(2.3, 0.6).Close()
			rc.p.FillPath(&pp, pineCol)
		})
	}
	// Camp: bonfire glow, tent, campfire, rising sparks, cat looking up
	tx := float32(106)
	ty := sc.far.y(float64(tx)) + 1 + rc.layerShiftY
	fx := tx + 13
	fy := sc.far.y(float64(fx)) + 0.8 + rc.layerShiftY

	glowOp, glowScale := float32(0.80), float32(1.0)
	if rc.t > 0 {
		u := rc.altWave(rc.t, 0, 2.2)
		glowOp = lerp(0.55, 1.0, u)
		glowScale = lerp(0.92, 1.08, u)
	}
	rc.radialGlow(rc.ox+fx, rc.oy+fy-1.8, 11*glowScale, sceneColor("#f2a25c"), 0.34*glowOp)

	// Tent
	rc.p.Transform(ui.Translation(rc.ox+tx, rc.oy+ty), 0, 0, func() {
		var body, lit, door ui.Path
		body.MoveTo(-6.5, 0).LineTo(0, -7).LineTo(6.5, 0).Close()
		lit.MoveTo(0, -7).LineTo(6.5, 0).LineTo(2.2, 0).Close()
		door.MoveTo(-1.5, 0).LineTo(0, -3.8).LineTo(1.5, 0).Close()
		rc.p.FillPath(&body, rc.adaptGround(sceneColor("#30374a")).Alpha(rc.alpha))
		rc.p.FillPath(&lit, rc.adaptGround(sceneColor("#3a4258")).Alpha(rc.alpha))
		rc.p.FillPath(&door, rc.adaptGround(sceneColor("#191c24")).Alpha(rc.alpha))
	})

	// Campfire logs, dancing flames, sparks
	rc.p.Transform(ui.Translation(rc.ox+fx, rc.oy+fy), 0, 0, func() {
		logCol := sceneColor("#3b3230").Alpha(rc.alpha)
		var logs ui.Path
		logs.MoveTo(-2.4, 0.2).LineTo(2.4, -0.6).MoveTo(-2.4, -0.6).LineTo(2.4, 0.2).Cap(ui.RoundCap)
		rc.p.StrokePath(&logs, 0.8, logCol)

		paintFlame := func(delay, dur float32, c ui.Color, outer bool) {
			sx, sy, sk := float32(1), float32(1), float32(0)
			if rc.t > 0 {
				u := rc.altWave(rc.t, delay, dur)
				ksX := [4][2]float32{{0, 1}, {0.3, 0.88}, {0.6, 1.08}, {1, 0.95}}
				ksY := [4][2]float32{{0, 1}, {0.3, 1.20}, {0.6, 0.88}, {1, 1.12}}
				ksS := [4][2]float32{{0, 0}, {0.3, -6}, {0.6, 5}, {1, -2}}
				sx = evalKeyframes1D(ksX[:], u)
				sy = evalKeyframes1D(ksY[:], u)
				sk = evalKeyframes1D(ksS[:], u)
			}
			rc.p.Transform(ui.Scaling(sx, sy).Then(ui.Skewing(sk, 0)), 0, 0, func() {
				var fl ui.Path
				if outer {
					fl.MoveTo(-1.5, 0).CubeTo(-1.7, -1.7, -0.3, -2.1, 0, -4.1).CubeTo(0.5, -2.3, 1.8, -1.7, 1.5, 0).Close()
				} else {
					fl.MoveTo(-0.8, 0).CubeTo(-0.9, -1, -0.1, -1.2, 0, -2.3).CubeTo(0.3, -1.3, 1, -1, 0.8, 0).Close()
				}
				rc.p.FillPath(&fl, c.Alpha(rc.alpha))
			})
		}
		paintFlame(0, 1.2, sceneColor("#f2a25c"), true)
		paintFlame(0.3, 0.9, sceneColor("#ffd899"), false)

		if rc.wm > 0.002 {
			spCol := sceneColor("#ffcf8a")
			for _, sp := range [2][4]float32{{0, 0.4, 0, 3.1}, {-0.8, 0.35, 1.7, 4.3}} {
				ph := rc.loopPhase(rc.t, sp[2], sp[3])
				easeOut := 1 - (1-ph)*(1-ph)
				var op float32
				if ph < 0.10 {
					op = ph / 0.10
				} else {
					op = 1 - (ph-0.10)/0.90
				}
				a := op * rc.wm * rc.dCalm * rc.alpha
				if a <= 0.003 {
					continue
				}
				var dot ui.Path
				dot.Circle(sp[0]+3*easeOut, lerp(-3, -15, easeOut), sp[1])
				rc.p.FillPath(&dot, spCol.Alpha(a))
			}
		}
	})

	rc.paintCat(sc.cat)
	rc.fillTerrain(&tr.nearPath, sc.near.fill)
	rc.paintBlades(sc.blades, sc.bladeColor, "")
}

// -----------------------------------------------------------------------------
// 8. 新月 Moon (Dark, 01–04)
// -----------------------------------------------------------------------------

func (rc *sceneRenderCtx) paintMoon(sc *compiledScene, tr *sceneTerrainCache) {
	rc.paintStars(sc.stars, 1.0)

	// Crescent moon + earthshine dark disc + soft moonlight halo
	if oa := rc.orbAlpha * rc.alpha; oa > 0.002 {
		if rc.skinChrome {
			oa *= 0.86
		}
		cy := rc.oy + 37.5 + rc.vars.orbDy + rc.orbExtraDY
		haloOp, haloScale := float32(0.75), float32(1.0)
		if rc.t > 0 {
			w := cosWave01(rc.loopPhase(rc.t, 3, 10))
			haloOp = lerp(0.55, 1.0, w)
			haloScale = lerp(1.0, 1.14, w)
		}
		r := 15 * haloScale
		hcx := rc.ox + 189
		haloCol := rc.adaptAccent(sceneColor("#9fb4e6"), 0.78)
		moonCol := rc.adaptAccent(sceneColor("#e7ecf8"), 0.65)
		rc.p.FillRadialGradient(ui.Rect{X: hcx - r, Y: cy - r, W: 2 * r, H: 2 * r}, ui.RadialGradient{
			Radius: r,
			Stops: []ui.GradientStop{
				{Color: haloCol.Alpha(0.26 * haloOp * oa), At: 0},
				{Color: haloCol.Alpha(0), At: 1},
			},
		}, r)
		var earthshine ui.Path
		earthshine.Circle(rc.ox+brandOrbX, cy, 4.5)
		rc.p.FillPath(&earthshine, moonCol.Alpha(0.09*oa))
		paintCrescentMoon(rc.p, rc.ox+brandOrbX, cy, 4.5, moonCol.Alpha(oa))
	}

	rc.paintMeteors(sc.meteors)

	// Thin moonlit cloud wisps drifting through the sidebar sky, clear of the titlebar controls
	for _, mw := range sc.moonWisps {
		vis := rc.tier(mw.tier) * rc.alpha
		if mw.minW > 0 {
			// The third wisp only joins once the sidebar is wide enough to give it its own room.
			vis *= smooth(clampF((rc.w-mw.minW)/60, 0, 1))
		}
		if vis <= 0.004 {
			continue
		}
		x := rc.w + 10 - (rc.w+110)*modPos(rc.t+float64(mw.delaySec), float64(mw.durSec))
		wispBody := rc.adaptGround(sceneColor("#343c50")).Alpha(0.5 * vis)
		wispRim := rc.adaptAccent(sceneColor("#8fa0c8"), 0.65).Alpha(0.3 * vis)
		rc.p.Transform(ui.Scaling(mw.scale, mw.scale).Then(ui.Translation(rc.ox+x, rc.oy+mw.y)), 0, 0, func() {
			rc.p.Fill(ui.Rect{X: 0, Y: 3, W: 150, H: 5}, wispBody, 2.5)
			rc.p.Fill(ui.Rect{X: 28, Y: 0, W: 92, H: 5}, wispBody, 2.5)
			rc.p.Fill(ui.Rect{X: 54, Y: 6.5, W: 124, H: 4}, wispBody, 2)
			rc.p.Fill(ui.Rect{X: 34, Y: 0, W: 80, H: 0.7}, wispRim, 0.35)
		})
	}
	// Peaks & far ridge with silver moonlight rim-lights
	rc.fillTerrain(&tr.peakPath, sc.peak.fill)
	rc.strokeRim(&tr.peakRim, 0.7, sc.rimColor, sc.rimMidStop, sc.rimMidOp, sc.rimPeakOp)
	rc.fillTerrain(&tr.farPath, sc.far.fill)
	rc.strokeRim(&tr.farRim, 0.9, sc.rimColor, sc.rimMidStop, sc.rimMidOp, 1.0)

	// Bare tree with blinking owl at x=58
	tx := float32(58)
	ty := sc.far.y(float64(tx)) + 0.6 + rc.layerShiftY
	treeCol := rc.adaptGround(sceneColor("#1d2029")).Alpha(rc.alpha)
	rc.p.Transform(ui.Translation(rc.ox+tx, rc.oy+ty), 0, 0, func() {
		var trunk ui.Path
		trunk.MoveTo(0, 0.6).CubeTo(0.5, -5, -0.6, -9, 0, -14.5).Cap(ui.RoundCap)
		rc.p.StrokePath(&trunk, 1.3, treeCol)
		var b1, b2, b3, b4 ui.Path
		b1.MoveTo(0, -8).QuadTo(4, -10, 7.4, -9.4).Cap(ui.RoundCap)
		b2.MoveTo(0, -11).QuadTo(-3.5, -12.6, -6, -15.8).Cap(ui.RoundCap)
		b3.MoveTo(4.5, -9.3).QuadTo(5.5, -11.5, 7.7, -12.1).Cap(ui.RoundCap)
		b4.MoveTo(-3, -13).QuadTo(-3.4, -14.8, -2.4, -16).Cap(ui.RoundCap)
		rc.p.StrokePath(&b1, 0.9, treeCol)
		rc.p.StrokePath(&b2, 0.8, treeCol)
		rc.p.StrokePath(&b3, 0.6, treeCol)
		rc.p.StrokePath(&b4, 0.6, treeCol)

		// Owl perched on branch at (4.4, -9.5)
		rc.p.Transform(ui.Translation(4.4, -9.5), 0, 0, func() {
			var body ui.Path
			fillEllipse(&body, func(x, y float32) (float32, float32) { return x, y }, 0, -2, 1.6, 2.1)
			rc.p.FillPath(&body, treeCol)
			var el, er ui.Path
			el.MoveTo(-1.5, -3.4).LineTo(-1.2, -4.8).LineTo(-0.3, -3.8).Close()
			er.MoveTo(1.5, -3.4).LineTo(1.2, -4.8).LineTo(0.3, -3.8).Close()
			rc.p.FillPath(&el, treeCol)
			rc.p.FillPath(&er, treeCol)

			blinkY := float32(1.0)
			if rc.t > 0 {
				ph := rc.loopPhase(rc.t, 0, 7.3)
				if ph > 0.92 && ph < 0.95 {
					blinkY = lerp(1.0, 0.1, smooth((ph-0.92)/0.03))
				} else if ph >= 0.95 {
					blinkY = lerp(0.1, 1.0, smooth((ph-0.95)/0.05))
				}
			}
			eyeCol := sceneColor("#e7ecf8").Alpha(rc.alpha)
			for _, ex := range [2]float32{-0.6, 0.6} {
				rc.p.Transform(ui.Scaling(1, blinkY), ex, -2.7, func() {
					var ep ui.Path
					ep.Circle(ex, -2.7, 0.45)
					rc.p.FillPath(&ep, eyeCol)
				})
			}
		})
	})

	rc.paintCat(sc.cat)
	rc.paintLake(sc.lake)
	rc.fillTerrain(&tr.nearPath, sc.near.fill)
	rc.paintBlades(sc.blades, sc.bladeColor, "")
}
