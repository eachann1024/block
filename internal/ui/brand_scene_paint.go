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

	wA1  float32
	wA2  float32
	wOrb float32

	elapsedA1 float64
	elapsedA2 float64
	lastWall  time.Time
}

const (
	brandSceneFrameRate    = 30
	brandSceneMaxDt        = 100 * time.Millisecond
	brandSceneSwitchDur    = 900 * time.Millisecond
	brandSceneFadeInDur    = 520 * time.Millisecond
	brandSceneFadeOutDur   = 420 * time.Millisecond
	brandSceneOrbFadeDur   = 300 * time.Millisecond
	brandSceneMotionEnvDur = 450 * time.Millisecond
)

// PaintBrandSceneBand paints the 88px full-window-width landscape band onto el's own backdrop
// (strictly behind all child elements such as the titlebar, sidebar controls, and right paper card),
// smoothly fading into ground from 52% height down to 88px.
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
		targetA1, targetA2 := float32(0), float32(0)
		if !reduced {
			switch opts.Motion {
			case BrandMotionRich:
				targetA1, targetA2 = 1, 1
			case BrandMotionCalm:
				targetA1 = 1
			}
		}

		if !st.init {
			st.init = true
			st.curID = id
			st.prevID = ""
			st.transProg = 1
			st.wOrb = targetOrb
			st.wA1 = targetA1
			st.wA2 = targetA2
			st.lastWall = wall
			if !reduced && opts.Motion != BrandMotionStill {
				seed := float64(wall.UnixMilli()%(24*3600*1000))/1000.0 + 1.0
				if targetA1 > 0 {
					st.elapsedA1 = seed
				}
				if targetA2 > 0 {
					st.elapsedA2 = seed
				}
			}
		} else {
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
				// Turning back ON from OFF in animated mode: seed clock if never started.
				if st.curID != "" && !reduced && opts.Motion != BrandMotionStill && st.elapsedA1 == 0 && st.elapsedA2 == 0 {
					seed := float64(wall.UnixMilli()%(24*3600*1000))/1000.0 + 1.0
					if targetA1 > 0 {
						st.elapsedA1 = seed
					}
					if targetA2 > 0 {
						st.elapsedA2 = seed
					}
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
				st.wA1 = 0
				st.wA2 = 0
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
				st.wOrb = moveToward(st.wOrb, targetOrb, float32(dt)/float32(brandSceneOrbFadeDur))
				envStep := float32(dt) / float32(brandSceneMotionEnvDur)
				st.wA1 = moveToward(st.wA1, targetA1, envStep)
				st.wA2 = moveToward(st.wA2, targetA2, envStep)
				if !opts.PauseAnim {
					dtSec := dt.Seconds()
					if st.wA1 > 0 {
						st.elapsedA1 += dtSec * float64(smooth(st.wA1))
					}
					if st.wA2 > 0 {
						st.elapsedA2 += dtSec * float64(smooth(st.wA2))
					}
				}
			}
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

		tA1, tA2 := st.elapsedA1, st.elapsedA2
		wA1, wA2 := smooth(st.wA1), smooth(st.wA2)
		if reduced {
			tA1, tA2 = 0, 0
			wA1, wA2 = 0, 0
		}

		u := smootherstep(st.transProg)
		p.Clip(bandRect, 0, func() {
			if st.transProg < 1 {
				// Outgoing scene gently dissolves and settles out.
				if prevSc != nil {
					alphaOut := 1 - u
					// Keep outgoing hills fuller mid-crossfade on same-theme switches to avoid a hollow dip.
					if curSc != nil && prevSc.dark == curSc.dark {
						alphaOut = 1 - u*u
					}
					if alphaOut > 0.002 {
						// Smoothly dissolve backdrop ground during Light <-> Dark theme switches.
						if prevSc.ground != ground {
							p.Fill(bandRect, prevSc.ground.Alpha(1-u), 0)
						}
						paintScene(p, prevSc, sceneRenderCtx{
							p:              p,
							ox:             bandRect.X,
							oy:             bandRect.Y,
							w:              bandRect.W,
							h:              BrandBandHeight,
							alpha:          alphaOut,
							tA1:            tA1,
							tA2:            tA2,
							wA1:            wA1,
							wA2:            wA2,
							orbAlpha:       orbEase,
							orbExtraDY:     orbSettleDY + 6.0*u,
							layerShiftY:    3.5 * u,
							showCat:        opts.ShowCat,
							avoidRightTail: true,
							vars:           evalSceneTimeVars(prevSc.id, sceneProgress(prevSc.id, nowClock)),
						})
					}
				}
				// Incoming scene gently emerges and lifts into place.
				if curSc != nil && u > 0.002 {
					paintScene(p, curSc, sceneRenderCtx{
						p:              p,
						ox:             bandRect.X,
						oy:             bandRect.Y,
						w:              bandRect.W,
						h:              BrandBandHeight,
						alpha:          u,
						tA1:            tA1,
						tA2:            tA2,
						wA1:            wA1,
						wA2:            wA2,
						orbAlpha:       orbEase,
						orbExtraDY:     orbSettleDY + 6.0*(1-u),
						layerShiftY:    3.5 * (1 - u),
						showCat:        opts.ShowCat,
						avoidRightTail: true,
						vars:           evalSceneTimeVars(curSc.id, sceneProgress(curSc.id, nowClock)),
					})
				}
			} else if curSc != nil {
				paintScene(p, curSc, sceneRenderCtx{
					p:              p,
					ox:             bandRect.X,
					oy:             bandRect.Y,
					w:              bandRect.W,
					h:              BrandBandHeight,
					alpha:          1,
					tA1:            tA1,
					tA2:            tA2,
					wA1:            wA1,
					wA2:            wA2,
					orbAlpha:       orbEase,
					orbExtraDY:     orbSettleDY,
					layerShiftY:    0,
					showCat:        opts.ShowCat,
					avoidRightTail: true,
					vars:           evalSceneTimeVars(curSc.id, sceneProgress(curSc.id, nowClock)),
				})
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

		inTransition := st.transProg < 1 || st.wOrb != targetOrb || st.wA1 != targetA1 || st.wA2 != targetA2
		if !reduced {
			if inTransition {
				p.AnimationFrame()
			} else if !opts.PauseAnim && curSc != nil && (st.wA1 > 0 || st.wA2 > 0) {
				p.AnimationFrameRate(brandSceneFrameRate)
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
				p:              p,
				ox:             r.X,
				oy:             r.Y,
				w:              BrandTileWidth,
				h:              BrandBandHeight,
				alpha:          1,
				tA1:            tSec,
				tA2:            tSec,
				wA1:            w,
				wA2:            w,
				orbAlpha:       orbA,
				showCat:        true,
				avoidRightTail: false,
				vars:           evalSceneTimeVars(sc.id, 0.5),
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
	// Sky backdrop gradient (if any), multiplied by scene alpha for smooth crossfades.
	if len(sc.skyStops) > 0 {
		stops := make([]ui.GradientStop, len(sc.skyStops))
		for i, st := range sc.skyStops {
			stops[i] = ui.GradientStop{
				Color: st.Color.Alpha((float32(st.Color.A) / 255.0) * rc.alpha),
				At:    st.At,
			}
		}
		p.FillGradient(ui.Rect{X: rc.ox, Y: rc.oy, W: rc.w, H: rc.h}, ui.LinearGradient{
			Angle: 180,
			Stops: stops,
		}, 0)
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
