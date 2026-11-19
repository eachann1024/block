package ui

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

func noHitTest(_, _ float32, _ ui.Rect) bool { return false }

type brandSceneClockKey struct{}

type brandSceneClock struct {
	lastWall  time.Time
	elapsedMs int64
	init      bool
}

const brandSceneStepMs = 33

// PaintBrandSceneBand paints the 88px full-window-width landscape band onto el's own backdrop
// (strictly behind all child elements such as the titlebar, sidebar controls, and right paper card),
// smoothly fading into the window ground from 52% height down to 88px.
func PaintBrandSceneBand(el ui.Element, id BrandSceneID, showOrb, pauseAnim bool) {
	ensureCompiledScenes()
	sc := compiledByID[id]
	if sc == nil || !el.Valid() {
		return
	}
	clk := ui.Local(el, brandSceneClockKey{}, func() brandSceneClock {
		return brandSceneClock{}
	})
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		bandH := min(float32(BrandBandHeight), r.H)
		if r.W <= 0 || bandH <= 0 {
			return
		}
		bandRect := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: bandH}
		if IsProbeEnabled() {
			RecordBounds("brand-scene-band", bandRect)
		}
		reduced := p.Preferences().ReduceMotion
		tSec := float64(0)
		if !reduced {
			now := p.Now()
			if !clk.init {
				clk.init = true
				clk.lastWall = now
				clk.elapsedMs = now.UnixMilli()
			} else if pauseAnim {
				clk.lastWall = now
			} else {
				dt := now.Sub(clk.lastWall).Milliseconds()
				if dt > 0 {
					if dt > 100 {
						dt = 100
					}
					clk.elapsedMs += dt
					clk.lastWall = now
				}
			}
			qMs := (clk.elapsedMs / brandSceneStepMs) * brandSceneStepMs
			tSec = float64(qMs) * 0.001
		}
		p.Clip(bandRect, 0, func() {
			paintScene(p, sc, bandRect.X, bandRect.Y, bandRect.W, BrandBandHeight, tSec, showOrb, true)

			// Equivalent to CSS mask-image: linear-gradient(to bottom, #000 52%, transparent)
			// composited directly over the solid Ground background.
			p.FillGradient(ui.Rect{X: bandRect.X, Y: bandRect.Y, W: bandRect.W, H: BrandBandHeight}, ui.LinearGradient{
				Angle: 180,
				Start: 0.52,
				End:   1.0,
				From:  sc.ground.Alpha(0),
				To:    sc.ground,
			}, 0)
		})

		if !reduced && !pauseAnim {
			p.After(brandSceneStepMs * time.Millisecond)
		}
	})
}

// BrandSceneThumbnail renders a 54px-tall cropped landscape preview tile for Settings.
func BrandSceneThumbnail(c *ui.Context, id BrandSceneID, showOrb bool) ui.Element {
	ensureCompiledScenes()
	sc := compiledByID[id]
	bg := ui.Hex("#f1f1f1") // design:allow
	if sc != nil {
		bg = sc.ground
	}
	box := ui.Box(c).FillWidth().Height(54).Clip().Background(bg).HitTest(noHitTest)
	if sc == nil {
		return box
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 {
			return
		}
		reduced := p.Preferences().ReduceMotion
		tSec := float64(0)
		if !reduced {
			tSec = float64(p.Now().UnixMilli()) * 0.001
		}
		// Match SVG viewBox="0 0 260 88" preserveAspectRatio="xMidYMid slice".
		scale := max(r.W/BrandTileWidth, r.H/BrandBandHeight)
		ox := r.X + (r.W-BrandTileWidth*scale)*0.5
		oy := r.Y + (r.H-BrandBandHeight*scale)*0.5
		p.Transform(ui.Translation(ox-r.X, oy-r.Y).Then(ui.Scaling(scale, scale)), r.X, r.Y, func() {
			paintScene(p, sc, r.X, r.Y, BrandTileWidth, BrandBandHeight, tSec, showOrb, false)
		})
		if !reduced {
			p.After(42 * time.Millisecond)
		}
	})
	return box
}

func modPos(t, period float64) float32 {
	if period <= 0 {
		return 0
	}
	m := math.Mod(t, period)
	if m < 0 {
		m += period
	}
	return float32(m / period)
}

func cosWave01(phase float32) float32 {
	return float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(phase)))
}

func paintScene(p *ui.Painter, sc *compiledScene, ox, oy, w, h float32, tSec float64, showOrb, avoidRightTail bool) {
	at := func(x, y float32) (float32, float32) { return ox + x, oy + y }

	// 1. Sky backdrop gradient.
	if len(sc.skyStops) > 0 {
		p.FillGradient(ui.Rect{X: ox, Y: oy, W: w, H: h}, ui.LinearGradient{
			Angle: 180,
			Stops: sc.skyStops,
		}, 0)
	}

	// 2. Daybreak warm elliptical horizon glow at (190, 58) rx=160 ry=46.
	if sc.horizonGlow {
		const steps = 14
		baseCol := ui.RGBA(236, 160, 118, 1) // design:allow
		for i := steps; i >= 1; i-- {
			frac := float32(i) / steps
			var ring ui.Path
			fillEllipse(&ring, at, brandOrbX, 58, 160*frac, 46*frac)
			p.FillPath(&ring, baseCol.Alpha(0.26/steps*1.35*(1-frac*0.45)))
		}
	}

	// 3. Faint Milky Way band in Starry Night.
	if sc.galaxyBand {
		cx, cy := at(560, 22)
		p.Transform(ui.Rotation(-2), cx, cy, func() {
			var band ui.Path
			fillEllipse(&band, at, 560, 22, 520, 10)
			p.FillPath(&band, ui.Hex("#8ea4d8").Alpha(0.05)) // design:allow
		})
	}

	// 4. Stars (tiled across sceneSpanWidth, avoiding right-tail chrome buttons dynamically).
	if len(sc.stars) > 0 {
		starCol := ui.Hex("#dfe6f5") // design:allow
		for spanOff := float32(0); spanOff < w; spanOff += sceneSpanWidth {
			for i := range sc.stars {
				st := &sc.stars[i]
				sx := st.x + spanOff
				if sx >= w-4 {
					continue
				}
				if avoidRightTail && w > 320 && sx > w-140 && sx < w-4 && st.y > 4 && st.y < 36 {
					continue
				}
				alpha := st.opacity
				if st.twinkle {
					ph := modPos(tSec+float64(st.delaySec), float64(st.durSec))
					alpha = lerp(0.20, 1.0, cosWave01(ph))
				}
				var dot ui.Path
				dot.Circle(ox+sx, oy+st.y, st.r)
				p.FillPath(&dot, starCol.Alpha(alpha))
			}
		}
	}

	// 5. Celestial orb (Sun, Daybreak horizon glow disc, or Crescent Moon).
	if showOrb && sc.orbKind != orbNone {
		glowPulse := lerp(0.50, 0.90, cosWave01(modPos(tSec, 7.0)))
		switch sc.orbKind {
		case orbSun:
			var halo ui.Path
			halo.Circle(ox+brandOrbX, oy+sc.orbCY, sc.orbR+3.0)
			p.FillPath(&halo, sc.orbHalo.Alpha(sc.orbHaloOp*glowPulse))
			var core ui.Path
			core.Circle(ox+brandOrbX, oy+sc.orbCY, sc.orbR)
			p.FillPath(&core, sc.orbCore)
		case orbDaybreak:
			var halo ui.Path
			halo.Circle(ox+brandOrbX, oy+sc.orbCY, sc.orbR)
			p.FillPath(&halo, sc.orbHalo.Alpha(sc.orbHaloOp*glowPulse))
		case orbCrescent:
			var halo ui.Path
			halo.Circle(ox+brandOrbX-1, oy+sc.orbCY, sc.orbR+2.5)
			p.FillPath(&halo, sc.orbHalo.Alpha(sc.orbHaloOp*glowPulse*1.35))
			paintCrescentMoon(p, ox+brandOrbX, oy+sc.orbCY, sc.orbR, sc.orbCore)
		}
	}

	// 6. Shooting star in Crescent Moon (13s cycle, active in 84%..94%).
	if sc.shootStar && tSec > 0 {
		ph := modPos(tSec, 13.0)
		if ph >= 0.84 && ph < 0.94 {
			prog := (ph - 0.84) / 0.10
			ease := 1 - (1-prog)*(1-prog)
			var alpha float32
			if ph < 0.86 {
				alpha = (ph - 0.84) / 0.02
			} else {
				alpha = 1 - (ph-0.86)/0.08
			}
			sx := ox + 44 + 52*ease
			sy := oy + 8 + 18*ease
			var streak ui.Path
			streak.MoveTo(sx, sy).LineTo(sx+20, sy+7).Cap(ui.RoundCap)
			p.StrokePath(&streak, 1.0, ui.Hex("#eef2fb").Alpha(0.85*alpha)) // design:allow
		}
	}

	// 7. Distant birds in Dusk (120s glide across the sky).
	if sc.birds {
		ph := modPos(tSec, 120.0)
		span := max(w, 1200)
		bx := -30 + (span+60)*ph
		by := -14 * ph
		if tSec == 0 {
			bx, by = 220, -3 // visible resting spot when reduced-motion is enabled
		}
		birdCol := ui.Hex("#b49c8e") // design:allow
		paintBird(p, ox+bx, oy+30+by, 0.9, birdCol)
		paintBird(p, ox+bx+12, oy+25+by, 0.7, birdCol)
	}

	// 8. Parallax drifting hills, mist bars, wind lines, and swaying grass blades.
	for li := range sc.layers {
		lay := &sc.layers[li]
		shift := float32(0)
		if lay.driftSec > 0 && tSec > 0 {
			shift = -BrandTileWidth * modPos(tSec, float64(lay.driftSec))
		}
		if lay.hasHill {
			paintHillLayer(p, ox, oy, w, h, shift, lay.hillB, lay.hillA, lay.hillInv, lay.hillFill)
		}
		for _, m := range lay.mists {
			col := ui.Hex("#ffffff").Alpha(m.opacity) // design:allow
			for k := float32(0); k+shift < w+BrandTileWidth; k += BrandTileWidth {
				mx := m.x + k + shift
				if mx+m.w < 0 || mx > w {
					continue
				}
				p.Fill(ui.Rect{X: ox + mx, Y: oy + m.y, W: m.w, H: 5}, col, 2.5)
			}
		}
		for _, wl := range lay.winds {
			dashOff := float32(0)
			if tSec > 0 && wl.durSec > 0 {
				dashOff = -120 * modPos(tSec+float64(wl.delaySec), float64(wl.durSec))
			}
			var wp ui.Path
			any := false
			for k := float32(0); k+shift < w+BrandTileWidth; k += BrandTileWidth {
				wx := wl.x + k + shift
				if wx+80 < 0 || wx > w {
					continue
				}
				any = true
				x0, y0 := ox+wx, oy+wl.y
				// SVG: M x0 y0 c 14 -4, 28 4, 42 0 s 22 -5, 34 -1.5
				// Reflection of (x0+28, y0+4) across (x0+42, y0) is (x0+56, y0-4).
				wp.MoveTo(x0, y0).
					CubeTo(x0+14, y0-4, x0+28, y0+4, x0+42, y0).
					CubeTo(x0+56, y0-4, x0+64, y0-5, x0+76, y0-1.5)
			}
			if any {
				wp.Dash(dashOff, 22, 98).Cap(ui.RoundCap)
				p.StrokePath(&wp, 1.2, wl.color)
			}
		}
		for gi := range lay.blades {
			bg := &lay.blades[gi]
			sway := float32(0)
			if tSec > 0 {
				// skewX(-7deg .. +7deg) anchored at blade base: tan(7deg) ≈ 0.1228
				sway = lerp(-0.1228, 0.1228, cosWave01(modPos(tSec+float64(bg.delaySec), 4.6)))
			}
			var bp ui.Path
			any := false
			// Shift in [0, -260]; step across span periods to cover arbitrary widths.
			for spanOff := float32(0); spanOff+shift < w+BrandTileWidth; spanOff += sceneSpanWidth {
				for bi := range bg.blades {
					bl := &bg.blades[bi]
					bx := bl.x + spanOff + shift
					if bx < -8 || bx > w+8 {
						continue
					}
					any = true
					x0, y0 := ox+bx, oy+bl.y
					tipDX := bl.lean + sway*bl.h
					ctrlDX := bl.lean*0.4 + sway*(bl.h*0.55)
					ctrlDY := -bl.h * 0.55
					tipDY := -bl.h
					// Quadratic (0,0) -> (ctrlDX, ctrlDY) -> (tipDX, tipDY) elevated to cubic.
					c1x := x0 + (2.0/3.0)*ctrlDX
					c1y := y0 + (2.0/3.0)*ctrlDY
					c2x := x0 + tipDX + (2.0/3.0)*(ctrlDX-tipDX)
					c2y := y0 + tipDY + (2.0/3.0)*(ctrlDY-tipDY)
					bp.MoveTo(x0, y0).CubeTo(c1x, c1y, c2x, c2y, x0+tipDX, y0+tipDY)
				}
			}
			if any {
				bp.Cap(ui.RoundCap)
				p.StrokePath(&bp, 1.1, bg.color)
			}
		}
	}

	// 9. Drifting blinking fireflies in Fireflies scene.
	if len(sc.fireflies) > 0 {
		haloCol := ui.Hex("#d7f08a") // design:allow
		coreCol := ui.Hex("#e9fbb0") // design:allow
		for spanOff := float32(0); spanOff < w; spanOff += sceneSpanWidth {
			for i := range sc.fireflies {
				ff := &sc.fireflies[i]
				fx := ff.x + spanOff
				if fx > w-8 {
					continue
				}
				dx, dy := float32(0), float32(0)
				blink := float32(0.85)
				if tSec > 0 {
					mph := modPos(tSec+float64(ff.moveDelaySec), float64(ff.moveDurSec))
					dx, dy = fireflyOffset(mph)
					bph := modPos(tSec+float64(ff.blinkDelaySec), 3.4)
					blink = fireflyBlink(bph)
				}
				cx, cy := ox+fx+dx, oy+ff.y+dy
				var glow ui.Path
				glow.Circle(cx, cy, 3.2)
				p.FillPath(&glow, haloCol.Alpha(0.16*blink))
				var core ui.Path
				core.Circle(cx, cy, 1.1)
				p.FillPath(&core, coreCol.Alpha(blink))
			}
		}
	}
}

func paintHillLayer(p *ui.Painter, ox, oy, w, h, shift, b, a float32, inv bool, fill ui.Color) {
	amp := a
	if inv {
		amp = -a
	}
	var path ui.Path
	startX := ox + shift
	path.MoveTo(startX, oy+b)
	lastX := startX
	for k := float32(0); shift+k < w+BrandTileWidth; k += BrandTileWidth {
		x := ox + shift + k
		path.CubeTo(x+43, oy+b-amp, x+87, oy+b-amp, x+130, oy+b).
			CubeTo(x+173, oy+b+amp, x+217, oy+b+amp, x+260, oy+b)
		lastX = x + 260
	}
	path.LineTo(lastX, oy+h).LineTo(startX, oy+h).Close()
	p.FillPath(&path, fill)
}

func paintCrescentMoon(p *ui.Painter, cx, cy, r float32, col ui.Color) {
	const k = 0.5523
	rxIn := r * 0.66
	var m ui.Path
	// Top pole -> outer left semicircle -> bottom pole -> inner left elliptical arc -> top pole.
	m.MoveTo(cx, cy-r).
		CubeTo(cx-k*r, cy-r, cx-r, cy-k*r, cx-r, cy).
		CubeTo(cx-r, cy+k*r, cx-k*r, cy+r, cx, cy+r).
		CubeTo(cx-k*rxIn, cy+r, cx-rxIn, cy+k*r, cx-rxIn, cy).
		CubeTo(cx-rxIn, cy-k*r, cx-k*rxIn, cy-r, cx, cy-r).
		Close()
	p.FillPath(&m, col)
}

func paintBird(p *ui.Painter, x, y, sc float32, col ui.Color) {
	// Two quadratic wing arches: M0 0 q 2.2 -2.2 4.4 0 q 2.2 -2.2 4.4 0
	arch := func(path *ui.Path, x0, y0 float32) {
		qx, qy := 2.2*sc, -2.2*sc
		ex := 4.4 * sc
		path.CubeTo(x0+(2.0/3.0)*qx, y0+(2.0/3.0)*qy, x0+ex+(2.0/3.0)*(qx-ex), y0+(2.0/3.0)*qy, x0+ex, y0)
	}
	var bp ui.Path
	bp.MoveTo(x, y)
	arch(&bp, x, y)
	arch(&bp, x+4.4*sc, y)
	bp.Cap(ui.RoundCap)
	p.StrokePath(&bp, 0.9, col)
}

func fireflyOffset(ph float32) (float32, float32) {
	// Keyframes: 0:(0,0) -> 0.25:(5,-4) -> 0.50:(-3,-7) -> 0.75:(-6,-2) -> 1:(0,0)
	pts := [...][2]float32{{0, 0}, {5, -4}, {-3, -7}, {-6, -2}, {0, 0}}
	seg := min(int(ph*4), 3)
	local := smooth(ph*4 - float32(seg))
	return lerp(pts[seg][0], pts[seg+1][0], local), lerp(pts[seg][1], pts[seg+1][1], local)
}

func fireflyBlink(ph float32) float32 {
	switch {
	case ph < 0.40:
		return lerp(0.15, 1.0, smooth(ph/0.40))
	case ph < 0.60:
		return 1.0
	default:
		return lerp(1.0, 0.15, smooth((ph-0.60)/0.40))
	}
}
