package ui

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

func noHitTest(_, _ float32, _ ui.Rect) bool { return false }

type brandSceneStateKey struct{}

type brandSceneState struct {
	init bool

	curID     BrandSceneID
	prevID    BrandSceneID
	transProg float32
	transDur  time.Duration

	wm    float32 // motion envelope: 0 while still, 1 while animating
	rate  float32 // eased tempo multiplier applied to the scene clock
	dCalm float32 // density envelope of the calm tier
	dRich float32 // density envelope of the rich tier
	wOrb  float32
	wArt  float32
	artW  float32

	elapsed  float64
	lastWall time.Time
}

const (
	brandSceneFrameRate        = 30
	brandSceneMinimalFrameRate = 10
	brandSceneMaxDt            = 100 * time.Millisecond
	brandSceneSwitchDur        = 900 * time.Millisecond
	brandSceneFadeInDur        = 520 * time.Millisecond
	brandSceneFadeOutDur       = 420 * time.Millisecond
	brandSceneOrbFadeDur       = 300 * time.Millisecond
	brandSceneMotionEnvDur     = 450 * time.Millisecond
	brandSceneTempoEaseDur     = 600 * time.Millisecond
	brandSceneDensityDur       = 500 * time.Millisecond
	brandSceneArtFeatherW      = float32(24)
	brandSceneSkyTailFade      = float32(0.75) // matches CSS mask fading to 0.25 at the right edge
)

// PaintBrandSceneBand paints the 88px top brand band onto el's own backdrop
// (strictly behind all child elements such as the titlebar, sidebar controls, and right paper card):
// the sky gradient spans the full window and fades smoothly to the right, while illustrated
// landscape artwork is confined to opts.SceneWidth (the docked sidebar) with a soft right feather.
//
// Pass id == "" when the brand decoration is disabled so turning it off fades out naturally
// instead of vanishing abruptly.
func PaintBrandSceneBand(el ui.Element, id BrandSceneID, ground ui.Color, opts BrandScenePaintOpts) {
	if !el.Valid() {
		return
	}
	ensureCompiledScenes()
	if id != "" && compiledByID[id] == nil {
		id = ""
	}
	st := ui.Local(el, brandSceneStateKey{}, func() brandSceneState {
		return brandSceneState{}
	})
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		bandH := min(float32(BrandBandHeight), r.H)
		if r.W <= 0 || bandH <= 0 {
			return
		}
		bandRect := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: bandH}
		reduced := p.Preferences().ReduceMotion
		wall := p.Now()

		targetOrb := float32(0)
		if opts.ShowOrb {
			targetOrb = 1
		}
		targetArt := float32(0)
		if opts.SceneWidth > 0 {
			targetArt = 1
		}
		targetRate := float32(0)
		if !reduced {
			targetRate = brandSceneLeisurelyRate
		}
		targetM := float32(0)
		if targetRate > 0 {
			targetM = 1
		}
		targetCalm, targetRich := float32(1), float32(0)
		switch opts.Motion.Density {
		case BrandDensityRich:
			targetRich = 1
		case BrandDensityMinimal:
			targetCalm = 0
		}

		if !st.init {
			st.init = true
			st.curID = id
			st.prevID = ""
			st.transProg = 1
			st.wOrb = targetOrb
			st.wArt = targetArt
			st.artW = opts.SceneWidth
			st.wm = targetM
			st.rate = targetRate
			st.dCalm = targetCalm
			st.dRich = targetRich
			st.lastWall = wall
		} else {
			if opts.SceneWidth > 0 {
				st.artW = opts.SceneWidth
			}
			// Detect scene changes (time-of-day crossing, theme switch, or on/off toggle).
			if id != st.curID {
				if reduced {
					st.curID = id
					st.prevID = ""
					st.transProg = 1
				} else if st.transProg < 1 && id == st.prevID {
					// Smoothly reverse mid-flight when user toggles back (e.g. Light -> Dark -> Light).
					st.prevID, st.curID = st.curID, id
					st.transProg = 1 - st.transProg
					st.transDur = transitionDuration(st.prevID, st.curID)
				} else {
					st.prevID = st.curID
					st.curID = id
					st.transProg = 0
					st.transDur = transitionDuration(st.prevID, st.curID)
				}
			}

			dt := wall.Sub(st.lastWall)
			if dt < 0 {
				dt = 0
			} else if dt > brandSceneMaxDt {
				dt = brandSceneMaxDt
			}
			st.lastWall = wall

			if reduced {
				st.transProg = 1
				st.prevID = ""
				st.wOrb = targetOrb
				st.wArt = targetArt
				st.wm = 0
				st.rate = 0
				st.dCalm = targetCalm
				st.dRich = targetRich
			} else if dt > 0 {
				if st.transProg < 1 {
					dur := st.transDur
					if dur <= 0 {
						dur = brandSceneSwitchDur
					}
					st.transProg = min(1, st.transProg+float32(dt)/float32(dur))
					if st.transProg >= 1 {
						st.prevID = ""
					}
				}
				orbStep := float32(dt) / float32(brandSceneOrbFadeDur)
				st.wOrb = moveToward(st.wOrb, targetOrb, orbStep)
				st.wArt = moveToward(st.wArt, targetArt, orbStep)
				envStep := float32(dt) / float32(brandSceneMotionEnvDur)
				st.wm = moveToward(st.wm, targetM, envStep)
				st.rate = moveToward(st.rate, targetRate, float32(dt)/float32(brandSceneTempoEaseDur))
				densStep := float32(dt) / float32(brandSceneDensityDur)
				st.dCalm = moveToward(st.dCalm, targetCalm, densStep)
				st.dRich = moveToward(st.dRich, targetRich, densStep)
				if !opts.PauseAnim {
					st.elapsed += dt.Seconds() * float64(st.rate)
				}
			}
		}
		// Start the clock from the time of day so a fresh window never opens on the rest pose.
		if targetRate > 0 && st.elapsed == 0 {
			st.elapsed = float64(wall.UnixMilli()%(24*3600*1000))/1000.0 + 1.0
		}

		curSc := compiledByID[st.curID]
		prevSc := compiledByID[st.prevID]
		if curSc == nil && (prevSc == nil || st.transProg >= 1) {
			return
		}

		if IsProbeEnabled() {
			RecordBounds("brand-scene-band", bandRect)
		}

		nowClock := opts.Now
		if nowClock.IsZero() {
			nowClock = time.Now()
		}
		orbEase := smooth(st.wOrb)
		orbSettleDY := 4.0 * (1 - orbEase)
		artEase := smooth(st.wArt)

		t, wm := st.elapsed, smooth(st.wm)
		if reduced {
			t, wm = 0, 0
		}
		dCalm, dRich := smooth(st.dCalm), smooth(st.dRich)

		u := smootherstep(st.transProg)
		alphaOut, alphaIn := float32(0), float32(0)
		if st.transProg < 1 {
			if prevSc != nil {
				alphaOut = 1 - u
				// Keep outgoing hills fuller mid-crossfade on same-theme switches to avoid a hollow dip.
				if curSc != nil && prevSc.dark == curSc.dark {
					alphaOut = 1 - u*u
				}
			}
			if curSc != nil {
				alphaIn = u
			}
		} else if curSc != nil {
			alphaIn = 1
		}

		glassA := opts.GlassAlpha
		if glassA <= 0 || glassA > 1 {
			glassA = 1
		}
		p.Clip(bandRect, 0, func() {
			effGround := ground
			// Smoothly dissolve backdrop ground during Light <-> Dark theme switches.
			if st.transProg < 1 && prevSc != nil && prevSc.ground != ground && alphaOut > 0.002 {
				p.Fill(bandRect, prevSc.ground.Alpha((1-u)*glassA), 0)
				effGround = prevSc.ground.Alpha(1 - u).Over(ground)
			}

			// 1. Full-window sky backdrop gradient, fading horizontally toward the right edge.
			skyRect := ui.Rect{X: bandRect.X, Y: bandRect.Y, W: bandRect.W, H: BrandBandHeight}
			skyScale := float32(1)
			if opts.SkinChrome {
				skyScale = 0.72
			}
			if glassA < 1 {
				// Keep the sky a light wash so the window material still reads through.
				skyScale *= 0.6
			}
			// Only skins recolor the dark art onto their own ground; stock themes keep
			// the original art, including during light <-> dark transitions.
			prevRemap := opts.SkinChrome && prevSc != nil && prevSc.dark && prevSc.ground != effGround
			curRemap := opts.SkinChrome && curSc != nil && curSc.dark && curSc.ground != effGround
			if prevSc != nil && alphaOut > 0.002 {
				paintSceneSky(p, prevSc, skyRect, alphaOut*skyScale, effGround, prevRemap)
			}
			if curSc != nil && alphaIn > 0.002 {
				paintSceneSky(p, curSc, skyRect, alphaIn*skyScale, effGround, curRemap)
			}
			hasSky := (prevSc != nil && alphaOut > 0.002 && len(prevSc.skyStops) > 0) ||
				(curSc != nil && alphaIn > 0.002 && len(curSc.skyStops) > 0)

			skyFadeStart := opts.SceneWidth
			if skyFadeStart <= 0 {
				skyFadeStart = BrandTileWidth
			}
			if skyFadeStart > bandRect.W {
				skyFadeStart = bandRect.W
			}
			if hasSky && bandRect.W > skyFadeStart {
				p.FillGradient(ui.Rect{
					X: bandRect.X + skyFadeStart,
					Y: bandRect.Y,
					W: bandRect.W - skyFadeStart,
					H: BrandBandHeight,
				}, ui.LinearGradient{
					Angle: 90,
					From:  effGround.Alpha(0),
					To:    effGround.Alpha(brandSceneSkyTailFade * glassA),
				}, 0)
			}

			// 2. Sidebar-only illustrated scene artwork, softly feathered on its right edge.
			artW := min(st.artW, bandRect.W)
			artMult := float32(1)
			if opts.SkinChrome {
				artMult = 0.78
			}
			if artEase > 0.002 && artW > 0 {
				artRect := ui.Rect{X: bandRect.X, Y: bandRect.Y, W: artW, H: bandRect.H}
				p.Clip(artRect, 0, func() {
					if prevSc != nil && alphaOut > 0.002 {
						paintSceneArt(p, prevSc, sceneRenderCtx{
							p:              p,
							ox:             bandRect.X,
							oy:             bandRect.Y,
							w:              artW,
							h:              BrandBandHeight,
							alpha:          alphaOut * artEase * artMult,
							t:              t,
							wm:             wm,
							dCalm:          dCalm,
							dRich:          dRich,
							orbAlpha:       orbEase,
							orbExtraDY:     orbSettleDY + 6.0*u,
							layerShiftY:    3.5 * u,
							showCat:        opts.ShowCat,
							vars:           evalSceneTimeVars(prevSc.id, sceneProgress(prevSc.id, nowClock)),
							dstGround:      effGround,
							hasGroundRemap: prevRemap,
							skinChrome:     opts.SkinChrome,
							skinAccent:     opts.SkinAccent,
						})
					}
					if curSc != nil && alphaIn > 0.002 {
						orbDY := orbSettleDY
						shiftY := float32(0)
						if st.transProg < 1 {
							orbDY += 6.0 * (1 - u)
							shiftY = 3.5 * (1 - u)
						}
						paintSceneArt(p, curSc, sceneRenderCtx{
							p:              p,
							ox:             bandRect.X,
							oy:             bandRect.Y,
							w:              artW,
							h:              BrandBandHeight,
							alpha:          alphaIn * artEase * artMult,
							t:              t,
							wm:             wm,
							dCalm:          dCalm,
							dRich:          dRich,
							orbAlpha:       orbEase,
							orbExtraDY:     orbDY,
							layerShiftY:    shiftY,
							showCat:        opts.ShowCat,
							vars:           evalSceneTimeVars(curSc.id, sceneProgress(curSc.id, nowClock)),
							dstGround:      effGround,
							hasGroundRemap: curRemap,
							skinChrome:     opts.SkinChrome,
							skinAccent:     opts.SkinAccent,
						})
					}

					featherW := min(brandSceneArtFeatherW, artW)
					fx := bandRect.X + artW - featherW
					if !hasSky {
						p.FillGradient(ui.Rect{X: fx, Y: bandRect.Y, W: featherW, H: BrandBandHeight}, ui.LinearGradient{
							Angle: 90,
							From:  effGround.Alpha(0),
							To:    effGround,
						}, 0)
					} else {
						const strips = 22
						stripH := BrandBandHeight / strips
						for i := range strips {
							y0 := float32(i) * stripH
							t := (y0 + stripH*0.5) / BrandBandHeight
							col := effGround
							if prevSc != nil && alphaOut > 0.002 && len(prevSc.skyStops) > 0 {
								col = sampleSkyOver(adaptedSkyStops(prevSc.skyStops, effGround, prevRemap), t, alphaOut*skyScale, col)
							}
							if curSc != nil && alphaIn > 0.002 && len(curSc.skyStops) > 0 {
								col = sampleSkyOver(adaptedSkyStops(curSc.skyStops, effGround, curRemap), t, alphaIn*skyScale, col)
							}
							p.FillGradient(ui.Rect{X: fx, Y: bandRect.Y + y0, W: featherW, H: stripH}, ui.LinearGradient{
								Angle: 90,
								From:  col.Alpha(0),
								To:    col,
							}, 0)
						}
					}
				})
			}

			// 3. A few stars trail past the sidebar edge into the titlebar as a quiet accent.
			// Suppressed when a custom skin owns chrome so dots never clutter skin breadcrumbs.
			if !opts.SkinChrome && artEase > 0.002 && artW > 0 && bandRect.W > artW {
				spill := func(sc *compiledScene, a float32) {
					if sc == nil || len(sc.spill) == 0 || a <= 0.002 {
						return
					}
					rc := sceneRenderCtx{
						p: p, ox: bandRect.X, oy: bandRect.Y, w: artW, h: BrandBandHeight,
						alpha: a * artEase, t: t, wm: wm, dCalm: dCalm, dRich: dRich,
						vars: evalSceneTimeVars(sc.id, sceneProgress(sc.id, nowClock)),
					}
					rc.paintSpill(sc, bandRect.W)
				}
				spill(prevSc, alphaOut)
				spill(curSc, alphaIn)
			}

			// Equivalent to CSS mask-image: linear-gradient(to bottom, #000 52%, transparent)
			// composited into the active window ground so y=88 is always seamless.
			p.FillGradient(ui.Rect{X: bandRect.X, Y: bandRect.Y, W: bandRect.W, H: BrandBandHeight}, ui.LinearGradient{
				Angle: 180,
				Start: 0.52,
				End:   1.0,
				From:  ground.Alpha(0),
				To:    ground,
			}, 0)
		})

		inTransition := st.transProg < 1 || st.wOrb != targetOrb || st.wArt != targetArt || st.wm != targetM ||
			st.rate != targetRate || st.dCalm != targetCalm || st.dRich != targetRich
		if !reduced {
			if inTransition {
				p.AnimationFrame()
			} else if !opts.PauseAnim && curSc != nil && st.rate > 0 && artEase > 0.002 {
				// Minimal keeps only slow landscape motion, so it redraws far less often to save power.
				fps := float32(brandSceneFrameRate)
				if opts.Motion.Density == BrandDensityMinimal {
					fps = brandSceneMinimalFrameRate
				}
				p.AnimationFrameRate(fps)
			}
		}
	})
}

// BrandSceneThumbnail renders a 54px-tall cropped landscape preview tile for Settings.
func BrandSceneThumbnail(c *ui.Context, id BrandSceneID, showOrb bool) ui.Element {
	ensureCompiledScenes()
	sc := compiledByID[id]
	bg := sceneColor("#f1f1f1")
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
		const previewRestSec = 11.1
		tSec := float64(0)
		w := float32(0)
		if !p.Preferences().ReduceMotion {
			tSec = previewRestSec
			w = 1
		}
		orbA := float32(0)
		if showOrb {
			orbA = 1
		}
		scale := max(r.W/BrandTileWidth, r.H/BrandBandHeight)
		ox := r.X + (r.W-BrandTileWidth*scale)*0.5
		oy := r.Y + (r.H-BrandBandHeight*scale)*0.5
		p.Transform(ui.Translation(ox-r.X, oy-r.Y).Then(ui.Scaling(scale, scale)), r.X, r.Y, func() {
			paintScene(p, sc, sceneRenderCtx{
				p:        p,
				ox:       r.X,
				oy:       r.Y,
				w:        BrandTileWidth,
				h:        BrandBandHeight,
				alpha:    1,
				t:        tSec,
				wm:       w,
				dCalm:    1,
				dRich:    1,
				orbAlpha: orbA,
				showCat:  true,
				vars:     evalSceneTimeVars(sc.id, 0.5),
			})
		})
	})
	return box
}

func transitionDuration(prevID, curID BrandSceneID) time.Duration {
	switch {
	case prevID == "" && curID != "":
		return brandSceneFadeInDur
	case prevID != "" && curID == "":
		return brandSceneFadeOutDur
	default:
		return brandSceneSwitchDur
	}
}

func moveToward(cur, target, maxDelta float32) float32 {
	if cur < target {
		return min(cur+maxDelta, target)
	}
	if cur > target {
		return max(cur-maxDelta, target)
	}
	return target
}

func smootherstep(t float32) float32 {
	t = clampF(t, 0, 1)
	return t * t * t * (t*(t*6-15) + 10)
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

func paintScene(p *ui.Painter, sc *compiledScene, rc sceneRenderCtx) {
	if rc.alpha <= 0.002 {
		return
	}
	paintSceneSky(p, sc, ui.Rect{X: rc.ox, Y: rc.oy, W: rc.w, H: rc.h}, rc.alpha, sc.ground, false)
	paintSceneArt(p, sc, rc)
}

func adaptedSkyStops(raw []ui.GradientStop, ground ui.Color, remap bool) []ui.GradientStop {
	if !remap || len(raw) == 0 {
		return raw
	}
	out := make([]ui.GradientStop, len(raw))
	for i, st := range raw {
		col := st.Color
		if col.A == 0 {
			col = ground.Alpha(0)
		} else {
			col = col.Mix(ground, 0.60)
		}
		out[i] = ui.GradientStop{Color: col, At: st.At}
	}
	return out
}

func paintSceneSky(p *ui.Painter, sc *compiledScene, r ui.Rect, alpha float32, ground ui.Color, remap bool) {
	if alpha <= 0.002 || len(sc.skyStops) == 0 {
		return
	}
	src := adaptedSkyStops(sc.skyStops, ground, remap)
	stops := make([]ui.GradientStop, len(src))
	for i, st := range src {
		stops[i] = ui.GradientStop{
			Color: st.Color.Alpha((float32(st.Color.A) / 255.0) * alpha),
			At:    st.At,
		}
	}
	p.FillGradient(r, ui.LinearGradient{
		Angle: 180,
		Stops: stops,
	}, 0)
}

func paintSceneArt(p *ui.Painter, sc *compiledScene, rc sceneRenderCtx) {
	if rc.alpha <= 0.002 {
		return
	}
	tr := sc.ensureTerrain(rc.w)
	switch sc.id {
	case SceneDawn:
		rc.paintDawn(sc, tr)
	case SceneMeadow:
		rc.paintMeadow(sc, tr)
	case SceneBreeze:
		rc.paintBreeze(sc, tr)
	case SceneDusk:
		rc.paintDusk(sc, tr)
	case SceneDaybreak:
		rc.paintDaybreak(sc, tr)
	case SceneFirefly:
		rc.paintFirefly(sc, tr)
	case SceneNight:
		rc.paintNight(sc, tr)
	case SceneMoon:
		rc.paintMoon(sc, tr)
	}
}

// sampleSkyOver returns the sky gradient's colour at t as it appears on screen over bg. Each stop
// is composited first and the results interpolated, which is how the painter blends a gradient
// whose stops are translucent; mixing the stops and compositing afterwards applies the fade twice.
func sampleSkyOver(stops []ui.GradientStop, t, alpha float32, bg ui.Color) ui.Color {
	if len(stops) == 0 {
		return bg
	}
	over := func(c ui.Color) ui.Color {
		return c.Alpha((float32(c.A) / 255.0) * alpha).Over(bg)
	}
	if t <= stops[0].At || len(stops) == 1 {
		return over(stops[0].Color)
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].At {
			a, b := stops[i-1], stops[i]
			span := b.At - a.At
			if span <= 0 {
				return over(b.Color)
			}
			return over(a.Color).Mix(over(b.Color), (t-a.At)/span)
		}
	}
	return over(stops[len(stops)-1].Color)
}
