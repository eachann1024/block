package ui

import (
	"math"
	"time"

	"block/internal/theme"
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

	frame brandSceneFrame
}

type brandSceneFrame struct {
	ready       bool
	bandRect    ui.Rect
	curSc       *compiledScene
	prevSc      *compiledScene
	alphaOut    float32
	alphaIn     float32
	u           float32
	t           float64
	wm          float32
	dCalm       float32
	dRich       float32
	orbEase     float32
	orbSettleDY float32
	artEase     float32
	artW        float32
	effGround   ui.Color
	prevRemap   bool
	curRemap    bool
	hasSky      bool
	inTrans     bool    // a scene crossfade is in flight
	animFPS     float32 // redraw request: <0 every frame, >0 that rate, 0 none
	nowClock    time.Time
}

// requestAnim asks the painter for the frame's redraw, so every part of the band
// that paints from st.frame repaints with it, not only the layer stepping it.
func (fr *brandSceneFrame) requestAnim(p *ui.Painter) {
	switch {
	case fr.animFPS < 0:
		p.AnimationFrame()
	case fr.animFPS > 0:
		p.AnimationFrameRate(fr.animFPS)
	}
}

const (
	brandSceneFrameRate         = 30
	brandSceneMinimalFrameRate  = 10
	brandSceneMaxDt             = 100 * time.Millisecond
	brandSceneSwitchDur         = 900 * time.Millisecond
	brandSceneFadeInDur         = 520 * time.Millisecond
	brandSceneFadeOutDur        = 420 * time.Millisecond
	brandSceneOrbFadeDur        = 300 * time.Millisecond
	brandSceneMotionEnvDur      = 450 * time.Millisecond
	brandSceneTempoEaseDur      = 600 * time.Millisecond
	brandSceneDensityDur        = 500 * time.Millisecond
	brandSceneArtFeatherW       = float32(24)
	brandSceneGlassRightFeather = float32(24)
	brandSceneSkyTailFade       = float32(0.75) // matches CSS mask fading to 0.25 at the right edge
)

// brandGlassVerticalStops is shared across frames: the vertical fade never changes.
var brandGlassVerticalStops = theme.BrandGlassVerticalStops()

func sceneHasSkyOrSpill(id BrandSceneID, st *brandSceneState, checkSpill bool) bool {
	has := func(sid BrandSceneID) bool {
		sc := compiledByID[sid]
		if sc == nil {
			return false
		}
		if checkSpill {
			return len(sc.spill) > 0
		}
		return len(sc.skyStops) > 0
	}
	if has(id) {
		return true
	}
	if !st.init {
		return false
	}
	return has(st.curID) || (st.transProg < 1 && has(st.prevID))
}

func stepBrandSceneFrame(st *brandSceneState, p *ui.Painter, r ui.Rect, id BrandSceneID, ground ui.Color, opts BrandScenePaintOpts) (brandSceneFrame, bool) {
	bandH := min(float32(BrandBandHeight), r.H)
	if r.W <= 0 || bandH <= 0 {
		st.frame = brandSceneFrame{}
		return brandSceneFrame{}, false
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
		st.frame = brandSceneFrame{}
		return brandSceneFrame{}, false
	}

	if IsProbeEnabled() {
		RecordBounds("brand-scene-band", bandRect)
	}

	nowClock := opts.Now
	if nowClock.IsZero() {
		nowClock = time.Now()
	}
	orbEase := smooth(st.wOrb)
	artEase := smooth(st.wArt)
	t, wm := st.elapsed, smooth(st.wm)
	if reduced {
		t, wm = 0, 0
	}
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

	inTransition := st.transProg < 1 || st.wOrb != targetOrb || st.wArt != targetArt || st.wm != targetM ||
		st.rate != targetRate || st.dCalm != targetCalm || st.dRich != targetRich
	animFPS := float32(0)
	if !reduced {
		if inTransition {
			animFPS = -1
		} else if !opts.PauseAnim && curSc != nil && st.rate > 0 && artEase > 0.002 {
			// Minimal keeps only slow landscape motion, so it redraws far less often to save power.
			animFPS = brandSceneFrameRate
			if opts.Motion.Density == BrandDensityMinimal {
				animFPS = brandSceneMinimalFrameRate
			}
		}
	}

	fr := brandSceneFrame{
		ready:       true,
		bandRect:    bandRect,
		curSc:       curSc,
		prevSc:      prevSc,
		alphaOut:    alphaOut,
		alphaIn:     alphaIn,
		u:           u,
		t:           t,
		wm:          wm,
		dCalm:       smooth(st.dCalm),
		dRich:       smooth(st.dRich),
		orbEase:     orbEase,
		orbSettleDY: 4.0 * (1 - orbEase),
		artEase:     artEase,
		artW:        min(st.artW, bandRect.W),
		effGround:   ground,
		prevRemap:   opts.SkinChrome && prevSc != nil && prevSc.dark && prevSc.ground != ground,
		curRemap:    opts.SkinChrome && curSc != nil && curSc.dark && curSc.ground != ground,
		hasSky:      (prevSc != nil && alphaOut > 0.002 && len(prevSc.skyStops) > 0) || (curSc != nil && alphaIn > 0.002 && len(curSc.skyStops) > 0),
		inTrans:     st.transProg < 1,
		animFPS:     animFPS,
		nowClock:    nowClock,
	}
	fr.requestAnim(p)
	st.frame = fr
	return fr, true
}

func paintBrandSceneArtLayers(p *ui.Painter, origin ui.Rect, fr *brandSceneFrame, opts BrandScenePaintOpts, artMult float32) {
	if fr.prevSc != nil && fr.alphaOut > 0.002 {
		paintSceneArt(p, fr.prevSc, sceneRenderCtx{
			p:              p,
			ox:             origin.X,
			oy:             origin.Y,
			w:              fr.artW,
			h:              BrandBandHeight,
			alpha:          fr.alphaOut * fr.artEase * artMult,
			t:              fr.t,
			wm:             fr.wm,
			dCalm:          fr.dCalm,
			dRich:          fr.dRich,
			orbAlpha:       fr.orbEase,
			orbExtraDY:     fr.orbSettleDY + 6.0*fr.u,
			layerShiftY:    3.5 * fr.u,
			showCat:        opts.ShowCat,
			vars:           evalSceneTimeVars(fr.prevSc.id, sceneProgress(fr.prevSc.id, fr.nowClock)),
			dstGround:      fr.effGround,
			hasGroundRemap: fr.prevRemap,
			skinChrome:     opts.SkinChrome,
			skinAccent:     opts.SkinAccent,
		})
	}
	if fr.curSc != nil && fr.alphaIn > 0.002 {
		orbDY := fr.orbSettleDY
		shiftY := float32(0)
		if fr.inTrans {
			orbDY += 6.0 * (1 - fr.u)
			shiftY = 3.5 * (1 - fr.u)
		}
		paintSceneArt(p, fr.curSc, sceneRenderCtx{
			p:              p,
			ox:             origin.X,
			oy:             origin.Y,
			w:              fr.artW,
			h:              BrandBandHeight,
			alpha:          fr.alphaIn * fr.artEase * artMult,
			t:              fr.t,
			wm:             fr.wm,
			dCalm:          fr.dCalm,
			dRich:          fr.dRich,
			orbAlpha:       fr.orbEase,
			orbExtraDY:     orbDY,
			layerShiftY:    shiftY,
			showCat:        opts.ShowCat,
			vars:           evalSceneTimeVars(fr.curSc.id, sceneProgress(fr.curSc.id, fr.nowClock)),
			dstGround:      fr.effGround,
			hasGroundRemap: fr.curRemap,
			skinChrome:     opts.SkinChrome,
			skinAccent:     opts.SkinAccent,
		})
	}
}

func paintBrandSceneSpill(p *ui.Painter, bandRect ui.Rect, fr *brandSceneFrame, scaleFor func(*compiledScene) float32) {
	spill := func(sc *compiledScene, a float32) {
		if sc == nil || len(sc.spill) == 0 || a <= 0.002 {
			return
		}
		scale := scaleFor(sc)
		rc := sceneRenderCtx{
			p: p, ox: bandRect.X, oy: bandRect.Y, w: fr.artW, h: BrandBandHeight,
			alpha: a * fr.artEase * scale, t: fr.t, wm: fr.wm, dCalm: fr.dCalm, dRich: fr.dRich,
			vars: evalSceneTimeVars(sc.id, sceneProgress(sc.id, fr.nowClock)),
		}
		rc.paintSpill(sc, bandRect.W)
	}
	spill(fr.prevSc, fr.alphaOut)
	spill(fr.curSc, fr.alphaIn)
}

func paintBrandSceneOpaque(p *ui.Painter, st *brandSceneState, ground ui.Color, opts BrandScenePaintOpts, fr *brandSceneFrame) {
	bandRect := fr.bandRect
	p.Clip(bandRect, 0, func() {
		effGround := ground
		// Smoothly dissolve backdrop ground during Light <-> Dark theme switches.
		if st.transProg < 1 && fr.prevSc != nil && fr.prevSc.ground != ground && fr.alphaOut > 0.002 {
			p.Fill(bandRect, fr.prevSc.ground.Alpha(1-fr.u), 0)
			effGround = fr.prevSc.ground.Alpha(1 - fr.u).Over(ground)
		}
		fr.effGround = effGround
		fr.prevRemap = opts.SkinChrome && fr.prevSc != nil && fr.prevSc.dark && fr.prevSc.ground != effGround
		fr.curRemap = opts.SkinChrome && fr.curSc != nil && fr.curSc.dark && fr.curSc.ground != effGround

		// 1. Full-window sky backdrop gradient, fading horizontally toward the right edge.
		skyRect := ui.Rect{X: bandRect.X, Y: bandRect.Y, W: bandRect.W, H: BrandBandHeight}
		skyScale := float32(1)
		if opts.SkinChrome {
			skyScale = 0.72
		}
		if fr.prevSc != nil && fr.alphaOut > 0.002 {
			paintSceneSky(p, fr.prevSc, skyRect, fr.alphaOut*skyScale, effGround, fr.prevRemap)
		}
		if fr.curSc != nil && fr.alphaIn > 0.002 {
			paintSceneSky(p, fr.curSc, skyRect, fr.alphaIn*skyScale, effGround, fr.curRemap)
		}

		skyFadeStart := opts.SceneWidth
		if skyFadeStart <= 0 {
			skyFadeStart = BrandTileWidth
		}
		if skyFadeStart > bandRect.W {
			skyFadeStart = bandRect.W
		}
		if fr.hasSky && bandRect.W > skyFadeStart {
			p.FillGradient(ui.Rect{
				X: bandRect.X + skyFadeStart,
				Y: bandRect.Y,
				W: bandRect.W - skyFadeStart,
				H: BrandBandHeight,
			}, ui.LinearGradient{
				Angle: 90,
				From:  effGround.Alpha(0),
				To:    effGround.Alpha(brandSceneSkyTailFade),
			}, 0)
		}

		// 2. Sidebar-only illustrated scene artwork, softly feathered on its right edge.
		artW := fr.artW
		artMult := float32(1)
		if opts.SkinChrome {
			artMult = 0.78
		}
		if fr.artEase > 0.002 && artW > 0 {
			artRect := ui.Rect{X: bandRect.X, Y: bandRect.Y, W: artW, H: bandRect.H}
			p.Clip(artRect, 0, func() {
				paintBrandSceneArtLayers(p, bandRect, fr, opts, artMult)

				featherW := min(brandSceneArtFeatherW, artW)
				fx := bandRect.X + artW - featherW
				if !fr.hasSky {
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
						if fr.prevSc != nil && fr.alphaOut > 0.002 && len(fr.prevSc.skyStops) > 0 {
							col = sampleSkyOver(adaptedSkyStops(fr.prevSc.skyStops, effGround, fr.prevRemap), t, fr.alphaOut*skyScale, col)
						}
						if fr.curSc != nil && fr.alphaIn > 0.002 && len(fr.curSc.skyStops) > 0 {
							col = sampleSkyOver(adaptedSkyStops(fr.curSc.skyStops, effGround, fr.curRemap), t, fr.alphaIn*skyScale, col)
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
		if !opts.SkinChrome && fr.artEase > 0.002 && artW > 0 && bandRect.W > artW {
			paintBrandSceneSpill(p, bandRect, fr, func(*compiledScene) float32 { return 1 })
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
}

// PaintBrandSceneBand paints the 88px top brand band onto el's own backdrop
// (strictly behind all child elements such as the titlebar, sidebar controls, and right paper card):
// the sky gradient spans the full window and fades smoothly to the right, while illustrated
// landscape artwork is confined to opts.SceneWidth (the docked sidebar) with a soft right feather.
//
// Pass id == "" when the brand decoration is disabled so turning it off fades out naturally
// instead of vanishing abruptly.
func PaintBrandSceneBand(c *ui.Context, el ui.Element, id BrandSceneID, ground ui.Color, opts BrandScenePaintOpts) {
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

	if !opts.Glass {
		el.Draw(func(p *ui.Painter, r ui.Rect) {
			fr, ok := stepBrandSceneFrame(st, p, r, id, ground, opts)
			if !ok {
				return
			}
			paintBrandSceneOpaque(p, st, ground, opts, &fr)
		})
		return
	}

	el.Draw(func(p *ui.Painter, r ui.Rect) {
		_, _ = stepBrandSceneFrame(st, p, r, id, ground, opts)
	})

	hasAnyScene := id != "" || (st.init && (st.curID != "" || (st.prevID != "" && st.transProg < 1)))
	if !hasAnyScene {
		return
	}

	// Glass mode uses real alpha masks instead of painting opaque ground over the
	// backdrop, so the sky and landscape merge softly into the window material
	// both vertically toward the sidebar and horizontally into the titlebar.
	el.MaskLinearGradient(ui.LinearGradient{
		Angle: 180,
		Stops: brandGlassVerticalStops,
	})

	artW := opts.SceneWidth
	if artW <= 0 && st.init && st.wArt > 0.002 {
		artW = st.artW
	}
	vpW, _ := c.Size()
	if vpW <= 0 {
		vpW = brandDesignWidth
	}
	// Sky and spill take each scene's own light/dark scale so a theme crossfade
	// does not jump; the artwork group shares one opacity across both themes.
	skyScale := func(sc *compiledScene) float32 {
		s, _, _ := theme.BrandGlassLayerOpacity(sc.dark, opts.SkinChrome)
		return s
	}
	spillScale := func(sc *compiledScene) float32 {
		_, _, s := theme.BrandGlassLayerOpacity(sc.dark, opts.SkinChrome)
		return s
	}
	_, artOpacity, _ := theme.BrandGlassLayerOpacity(theme.IsDark(), opts.SkinChrome)
	hasSkyBuild := sceneHasSkyOrSpill(id, st, false)
	hasSpillBuild := !opts.SkinChrome && artW > 0 && sceneHasSkyOrSpill(id, st, true)

	el.Children(func() {
		if hasSkyBuild {
			skyBox := ui.Box(c.Key("brand-band-sky")).
				Absolute().Left(0).Top(0).Right(0).Bottom(0).IgnoreInput(true)
			skyBox.MaskLinearGradient(ui.LinearGradient{
				Angle: 90,
				Stops: theme.BrandGlassHorizontalSkyStops(artW, vpW),
			})
			skyBox.Draw(func(p *ui.Painter, r ui.Rect) {
				fr := &st.frame
				fr.requestAnim(p)
				if !fr.ready || !fr.hasSky {
					return
				}
				if fr.prevSc != nil && fr.alphaOut > 0.002 {
					paintSceneSky(p, fr.prevSc, r, fr.alphaOut*skyScale(fr.prevSc), fr.effGround, fr.prevRemap)
				}
				if fr.curSc != nil && fr.alphaIn > 0.002 {
					paintSceneSky(p, fr.curSc, r, fr.alphaIn*skyScale(fr.curSc), fr.effGround, fr.curRemap)
				}
			})
		}

		if artW > 0 {
			artBoxW := artW + brandSceneGlassRightFeather
			artBox := ui.Box(c.Key("brand-band-art")).
				Absolute().Left(0).Top(0).Bottom(0).Width(artBoxW).
				IgnoreInput(true).Opacity(artOpacity)
			artBox.MaskLinearGradient(ui.LinearGradient{
				Angle: 90,
				Stops: theme.BrandGlassHorizontalArtStops(artW, artBoxW),
			})
			artBox.Children(func() {
				ui.Box(c.Key("brand-band-art-draw")).Fill().IgnoreInput(true).Draw(func(p *ui.Painter, r ui.Rect) {
					fr := &st.frame
					fr.requestAnim(p)
					if !fr.ready || fr.artEase <= 0.002 || fr.artW <= 0 {
						return
					}
					paintBrandSceneArtLayers(p, r, fr, opts, 1.0)
				})
			})
		}

		if hasSpillBuild {
			ui.Box(c.Key("brand-band-spill")).
				Absolute().Left(0).Top(0).Right(0).Bottom(0).IgnoreInput(true).
				Draw(func(p *ui.Painter, r ui.Rect) {
					fr := &st.frame
					fr.requestAnim(p)
					if !fr.ready || fr.artEase <= 0.002 || fr.artW <= 0 || r.W <= fr.artW {
						return
					}
					paintBrandSceneSpill(p, r, fr, spillScale)
				})
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
