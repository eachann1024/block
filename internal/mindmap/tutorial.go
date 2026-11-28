package mindmap

import (
	"math"
	"testing"
	"time"

	"block/assets"
	"block/internal/icons"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
)

const tutorialDurationSec = 10.5

type tutorialChapter struct {
	StartSec float64
	EndSec   float64
	Zh       string
	En       string
	KeyZh    string
	KeyEn    string
}

var tutorialChapters = []tutorialChapter{
	{StartSec: 0.0, EndSec: 2.2, Zh: "平移", En: "Pan", KeyZh: "空格+拖动", KeyEn: "Space+Drag"},
	{StartSec: 2.2, EndSec: 4.4, Zh: "建树", En: "Create", KeyZh: "Tab / ↵", KeyEn: "Tab / Enter"},
	{StartSec: 4.4, EndSec: 6.6, Zh: "移动", En: "Move", KeyZh: "⌥↑↓←→", KeyEn: "Alt+↑↓←→"},
	{StartSec: 6.6, EndSec: 8.4, Zh: "折叠", En: "Fold", KeyZh: "空格", KeyEn: "Space"},
	{StartSec: 8.4, EndSec: tutorialDurationSec, Zh: "视图", En: "Views", KeyZh: "⌘1/2/3", KeyEn: "Ctrl+1/2/3"},
}

type tutorialPlayerState struct {
	player  *ui.VideoPlayer
	dark    bool
	failed  bool
	playing bool
	posSec  float64
	lastAt  time.Time
}

func (tp *tutorialPlayerState) closePlayer() {
	if tp == nil || tp.player == nil {
		return
	}
	tp.player.Pause()
	tp.player.Close()
	tp.player = nil
}

func (tp *tutorialPlayerState) openForTheme(dark bool, startSec float64, autoPlay bool) {
	if tp == nil {
		return
	}
	tp.closePlayer()
	tp.dark = dark
	tp.failed = false
	tp.posSec = clampFloat64(startSec, 0, tutorialDurationSec)
	tp.playing = autoPlay
	tp.lastAt = time.Time{}

	if testing.Testing() || bui.IsProbeEnabled() {
		tp.failed = true
		return
	}

	assetPath := "media/mindmap-tutorial-light.mp4"
	if dark {
		assetPath = "media/mindmap-tutorial-dark.mp4"
	}
	raw, err := assets.Read(assetPath)
	if err != nil || len(raw) == 0 {
		tp.failed = true
		return
	}
	p, err := video.OpenData(raw)
	if err != nil || p == nil {
		tp.failed = true
		return
	}
	p.SetLoop(true)
	// The embedded tutorial MP4 has no audio track (-an); keeping muted=false avoids
	// MyGo's ReduceMotion muted-video hold while remaining completely silent.
	if startSec > 0.01 {
		p.Seek(time.Duration(startSec * float64(time.Second)))
	}
	if autoPlay {
		p.Play()
	} else {
		p.Pause()
	}
	tp.player = p
}

func (tp *tutorialPlayerState) usingVideo() bool {
	if tp == nil || tp.player == nil || tp.failed {
		return false
	}
	if tp.player.Err() != nil {
		tp.closePlayer()
		tp.failed = true
		return false
	}
	w, h := tp.player.Size()
	return tp.player.Ready() && w > 0 && h > 0
}

func (tp *tutorialPlayerState) currentPos(now time.Time) float64 {
	if tp == nil {
		return 0
	}
	if tp.usingVideo() {
		pos := tp.player.Position().Seconds()
		if pos >= 0 {
			tp.posSec = math.Mod(pos, tutorialDurationSec)
		}
		tp.playing = tp.player.Playing()
		return tp.posSec
	}
	if tp.playing && !now.IsZero() {
		if !tp.lastAt.IsZero() {
			dt := now.Sub(tp.lastAt).Seconds()
			if dt > 0 && dt < 0.25 {
				tp.posSec = math.Mod(tp.posSec+dt, tutorialDurationSec)
			}
		}
		tp.lastAt = now
	} else if !now.IsZero() {
		tp.lastAt = now
	}
	return tp.posSec
}

func (tp *tutorialPlayerState) isPlaying() bool {
	if tp == nil {
		return false
	}
	if tp.usingVideo() {
		return tp.player.Playing()
	}
	return tp.playing
}

func (tp *tutorialPlayerState) setPlaying(play bool, now time.Time) {
	if tp == nil {
		return
	}
	tp.playing = play
	tp.lastAt = now
	if tp.player != nil && !tp.failed && tp.player.Err() == nil {
		if play {
			tp.player.Play()
		} else {
			tp.player.Pause()
		}
	}
}

func (tp *tutorialPlayerState) seek(sec float64, play bool, now time.Time) {
	if tp == nil {
		return
	}
	clamped := clampFloat64(sec, 0, tutorialDurationSec-0.02)
	tp.posSec = clamped
	tp.playing = play
	tp.lastAt = now
	if tp.player != nil && !tp.failed && tp.player.Err() == nil {
		tp.player.Seek(time.Duration(clamped * float64(time.Second)))
		if play {
			tp.player.Play()
		} else {
			tp.player.Pause()
		}
	}
}

// OpenTutorial opens the minimalist animated shortcut tutorial modal.
// It is only opened by explicit user click; never auto-opened on app startup.
func (st *BlockState) OpenTutorial(firstRun bool) {
	if st == nil {
		return
	}
	st.ShortcutsOpen = false
	st.ExportMenuOpen = false
	st.LinkNodeID = 0
	st.ImportResult = nil
	st.TutorialOpen = true
	if firstRun {
		st.TutorialFirstRun = true
	}
	if st.tutorial == nil {
		st.tutorial = &tutorialPlayerState{}
	}
	st.tutorial.openForTheme(theme.Ocean().Dark, 0, true)
}

// CloseTutorial dismisses the tutorial modal, releases decoder resources, and
// notifies the host when dismissing first-run onboarding.
func (st *BlockState) CloseTutorial(cb Callbacks) {
	if st == nil {
		return
	}
	wasFirstRun := st.TutorialFirstRun || (st.lastCfgSet && !st.lastCfg.TutorialSeen)
	st.TutorialOpen = false
	st.TutorialFirstRun = false
	if st.tutorial != nil {
		st.tutorial.closePlayer()
		st.tutorial = nil
	}
	if wasFirstRun && cb.OnTutorialSeen != nil {
		cb.OnTutorialSeen()
	}
}

// PauseTutorial pauses an active tutorial video when its tab moves to background.
func (st *BlockState) PauseTutorial() {
	if st == nil || st.tutorial == nil {
		return
	}
	st.tutorial.setPlaying(false, time.Time{})
}

// Dispose releases any native video player handles held by this MindMap block.
func (st *BlockState) Dispose() {
	if st == nil || st.tutorial == nil {
		return
	}
	st.tutorial.closePlayer()
	st.tutorial = nil
}

func tutorialChapterIndex(sec float64) int {
	for i, ch := range tutorialChapters {
		if sec < ch.EndSec || i == len(tutorialChapters)-1 {
			return i
		}
	}
	return 0
}

// RenderTutorialOverlay renders the centered minimalist Ocean Paper shortcut tutorial
// modal inside a top-level ui.Overlay so it displays cleanly above inline and fullscreen maps.
func (st *BlockState) RenderTutorialOverlay(c *ui.Context, id string, cb Callbacks) {
	if st == nil || !st.TutorialOpen {
		return
	}
	pal := theme.Ocean()
	now := c.Now()
	if now.IsZero() {
		now = time.Now()
	}

	if st.tutorial == nil {
		st.tutorial = &tutorialPlayerState{}
		st.tutorial.openForTheme(pal.Dark, 0, true)
	} else if st.tutorial.dark != pal.Dark {
		prevPos := st.tutorial.currentPos(now)
		prevPlay := st.tutorial.isPlaying()
		st.tutorial.openForTheme(pal.Dark, prevPos, prevPlay)
	}

	tp := st.tutorial
	posSec := tp.currentPos(now)
	playing := tp.isPlaying()
	activeCh := tutorialChapterIndex(posSec)
	hasPlayer := tp.player != nil && !tp.failed && tp.player.Err() == nil
	videoReady := tp.usingVideo()

	if playing {
		if videoReady {
			c.After(60 * time.Millisecond)
		} else if hasPlayer {
			c.After(30 * time.Millisecond)
		} else if !testing.Testing() && !bui.IsProbeEnabled() {
			c.AnimationFrame()
		}
	} else if hasPlayer && !videoReady {
		c.After(40 * time.Millisecond)
	}

	winW, winH := c.Size()
	margin := theme.Space20
	modalW := min(float32(660), max(winW-margin*2, float32(320)))
	stageW := max(modalW-theme.Space20*2, float32(280))
	maxStageH := max(winH-float32(190), float32(180))
	stageH := min(stageW*9.0/16.0, maxStageH)
	// Keep clean 16:9 framing when height-constrained.
	if stageH < stageW*9.0/16.0 {
		stageW = stageH * 16.0 / 9.0
		modalW = stageW + theme.Space20*2
	}

	closeAndRestore := func() {
		st.CloseTutorial(cb)
		st.restoreFocusAfterPopover(c, cb)
		c.Invalidate()
	}

	ui.Overlay(c, func() {
		layer := bui.ModalLayer(c, "mm-tut-"+id).Padding(margin).Justify(ui.Center)

		if layer.OverlayShortcut(0, ui.KeyEscape) {
			closeAndRestore()
			return
		}
		if layer.OverlayShortcut(0, ui.KeySpace) && !c.ShortcutRepeated() {
			tp.setPlaying(!tp.isPlaying(), now)
			c.Invalidate()
		}
		if layer.OverlayShortcut(0, ui.KeyLeft) {
			prev := activeCh - 1
			if prev < 0 {
				prev = len(tutorialChapters) - 1
			}
			tp.seek(tutorialChapters[prev].StartSec, true, now)
			c.Invalidate()
		}
		if layer.OverlayShortcut(0, ui.KeyRight) {
			next := (activeCh + 1) % len(tutorialChapters)
			tp.seek(tutorialChapters[next].StartSec, true, now)
			c.Invalidate()
		}
		for i := range tutorialChapters {
			if layer.OverlayShortcut(0, ui.Key1+ui.Key(i)) && !c.ShortcutRepeated() {
				tp.seek(tutorialChapters[i].StartSec, true, now)
				c.Invalidate()
			}
		}

		layer.Children(func() {
			card := ui.Column(c.Key("mm-tut-card-"+id)).
				Width(modalW).Padding(theme.Space16, theme.Space20, theme.Space16, theme.Space20).
				Gap(theme.Space12).Radius(theme.RadiusXL).
				Background(pal.Menu).Border(1, pal.MenuBorder).
				Role(ui.RoleDialog).Label(theme.Tr("思维导图快捷键教程", "Mind Map Shortcut Guide")).
				Transition(bui.ModalSpringTransition())
			bui.Elevate(card, theme.ElevationDialog)
			bui.Probe(card, "mm-tut-card-"+id)

			if card.PressedOutside() {
				closeAndRestore()
				return
			}

			card.Children(func() {
				// 1. Minimal header: title on left, close button on right (no redundant subtitle).
				hdr := ui.Row(c.Key("mm-tut-hdr-" + id)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween)
				hdr.Children(func() {
					ui.Text(c, theme.Tr("思维导图快捷键", "Mind Map Shortcuts")).
						FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
						FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()

					closeBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
						ID:       "mm-tut-close-" + id,
						Label:    theme.Tr("关闭", "Close"),
						Tooltip:  theme.Tr("关闭", "Close"),
						Shortcut: "Esc",
						Size:     theme.HeightChrome,
						IconSize: theme.IconInline,
						Radius:   theme.RadiusSM,
					})
					if closeBtn.Clicked() {
						closeAndRestore()
					}
				})

				// 2. Clean 16:9 Motion Canvas video stage.
				stage := ui.Box(c.Key("mm-tut-stage-"+id)).
					Size(stageW, stageH).Radius(theme.RadiusLG).
					Background(pal.SettingsBG).Border(1, pal.Hairline).
					Cursor(ui.CursorPointer).Clip()
				stage.Children(func() {
					// Always mount ui.Video when a player exists: MyGo's video engine stays
					// held (and never becomes Ready) until ui.Video attaches it to the frame.
					if hasPlayer {
						ui.Video(c, tp.player).Fill().Fit(ui.Contain).
							Label(theme.Tr("思维导图快捷键演示动画", "Mind map shortcut animation"))
					}
					if !videoReady {
						// Zero-flicker Ocean Paper vector frame during warm-up or headless fallback.
						fallback := ui.Box(c.Key("mm-tut-fallback-" + id)).Absolute().Top(0).Right(0).Bottom(0).Left(0).IgnoreInput(true)
						fallback.Draw(func(p *ui.Painter, r ui.Rect) {
							drawTutorialFallbackFrame(p, r, pal, posSec)
						})
					}

					// Minimal pause badge when paused.
					if !playing {
						ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).Center().IgnoreInput(true).Children(func() {
							badge := ui.Box(c).Size(theme.HeightLarge+theme.Space8, theme.HeightLarge+theme.Space8).
								Radius(theme.RadiusPill).Background(pal.Menu).Border(1, pal.MenuBorder)
							bui.Elevate(badge, theme.ElevationPopover)
							badge.Draw(func(p *ui.Painter, r ui.Rect) {
								drawPlayGlyph(p, r, pal.Text)
							})
						})
					}

					// Thin scrubbable progress hairline along bottom edge of stage.
					progTrack := ui.Box(c.Key("mm-tut-prog-" + id)).
						Absolute().Left(0).Right(0).Bottom(0).Height(theme.Space8).
						Cursor(ui.CursorPointer)
					progRatio := float32(clampFloat64(posSec/tutorialDurationSec, 0, 1))
					progTrack.Draw(func(p *ui.Painter, r ui.Rect) {
						barH := float32(2.5)
						if progTrack.Hovered() {
							barH = 4
						}
						by := r.Y + r.H - barH
						p.Fill(ui.Rect{X: r.X, Y: by, W: r.W, H: barH}, pal.Hairline.Alpha(0.75), 0)
						p.Fill(ui.Rect{X: r.X, Y: by, W: r.W * progRatio, H: barH}, pal.Accent, 0)
					})
					progTrack.HandleInput(func(ev ui.InputEvent) bool {
						if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
							b := progTrack.LayoutBounds()
							if b.W > 0 {
								ratio := clampFloat64(float64(ev.X)/float64(b.W), 0, 1)
								tp.seek(ratio*tutorialDurationSec, true, now)
								c.Invalidate()
							}
							return true
						}
						return false
					})
				})
				if stage.Clicked() {
					tp.setPlaying(!tp.isPlaying(), now)
					c.Invalidate()
				}

				// 3. Flat bottom bar: 5 chapter pills on left, playback + primary dismiss on right.
				foot := ui.Row(c.Key("mm-tut-foot-" + id)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8)
				foot.Children(func() {
					chaptersRow := ui.Row(c.Key("mm-tut-chs-" + id)).AlignItems(ui.Center).Gap(theme.Space4).Shrink(1).MinWidth(0)
					chaptersRow.Children(func() {
						for i, ch := range tutorialChapters {
							idx := i
							chapter := ch
							isActive := activeCh == idx
							lbl := theme.Tr(chapter.Zh, chapter.En)
							keyTip := mmShortcutLabel(chapter.KeyZh, chapter.KeyEn)

							btn := ui.ButtonBase(c.Key("mm-tut-ch-"+id+"-"+chapter.En)).
								Height(theme.HeightCompact).PaddingX(theme.Space10).Radius(theme.RadiusMD).
								Center().Cursor(ui.CursorPointer).Shrink(0).Label(lbl)
							hov := btn.Hovered() || btn.Pressed()
							bg := ui.Transparent
							border := ui.Transparent
							fg := pal.TextSecondary
							wt := theme.WeightRegular
							if isActive {
								bg = pal.Selected
								border = pal.SelectedBorder
								fg = pal.Text
								wt = theme.WeightControl
							} else if hov {
								bg = pal.Hover
								fg = pal.Text
							}
							btn.Background(bg).Border(1, border)
							bui.TooltipShortcut(c, btn, lbl, keyTip)
							btn.Children(func() {
								ui.Text(c, lbl).FontSize(theme.FontSmall).FontWeight(wt).TextColor(fg).SingleLine()
							})
							if btn.Clicked() {
								tp.seek(chapter.StartSec, true, now)
								c.Invalidate()
							}
						}
					})

					ctrlRow := ui.Row(c.Key("mm-tut-ctrl-" + id)).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
					ctrlRow.Children(func() {
						playTip := theme.Tr("暂停", "Pause")
						if !playing {
							playTip = theme.Tr("播放", "Play")
						}
						playBtn := ui.ButtonBase(c.Key("mm-tut-play-"+id)).
							Size(theme.HeightCompact, theme.HeightCompact).Radius(theme.RadiusMD).
							Center().Cursor(ui.CursorPointer).Label(playTip)
						playHot := playBtn.Hovered() || playBtn.Pressed()
						if playHot {
							playBtn.Background(pal.Hover)
						}
						bui.TooltipShortcut(c, playBtn, playTip, theme.Tr("空格", "Space"))
						playFG := pal.TextSecondary
						if playHot {
							playFG = pal.Text
						}
						playBtn.Draw(func(p *ui.Painter, r ui.Rect) {
							if playing {
								drawPauseGlyph(p, r, playFG)
							} else {
								drawPlayGlyph(p, r, playFG)
							}
						})
						if playBtn.Clicked() {
							tp.setPlaying(!tp.isPlaying(), now)
							c.Invalidate()
						}

						replayBtn := bui.IconButton(c, icons.Refresh, bui.IconButtonOptions{
							ID:       "mm-tut-replay-" + id,
							Label:    theme.Tr("重播", "Replay"),
							Tooltip:  theme.Tr("从头播放", "Replay"),
							Size:     theme.HeightCompact,
							IconSize: theme.IconInline,
							Radius:   theme.RadiusMD,
						})
						if replayBtn.Clicked() {
							tp.seek(0, true, now)
							c.Invalidate()
						}

						if st.TutorialFirstRun {
							doneBtn := ui.ButtonBase(c.Key("mm-tut-done-"+id)).
								Height(theme.HeightCompact).PaddingX(theme.Space12).Margin(0, 0, 0, theme.Space4).
								Radius(theme.RadiusMD).Background(pal.Accent).Center().Cursor(ui.CursorPointer).
								Label(theme.Tr("开始使用", "Get Started"))
							if doneBtn.Hovered() || doneBtn.Pressed() {
								doneBtn.Background(pal.AccentHover)
							}
							doneBtn.Children(func() {
								ui.Text(c, theme.Tr("开始使用", "Get Started")).
									FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
									TextColor(pal.Paper).SingleLine()
							})
							if doneBtn.Clicked() {
								closeAndRestore()
							}
						}
					})
				})
			})
		})
	})
}

func drawPlayGlyph(p *ui.Painter, r ui.Rect, col ui.Color) {
	cx := r.X + r.W*0.5 + 0.8
	cy := r.Y + r.H*0.5
	var path ui.Path
	path.Join(ui.RoundJoin)
	path.MoveTo(cx-3.8, cy-4.8)
	path.LineTo(cx+4.6, cy)
	path.LineTo(cx-3.8, cy+4.8)
	path.Close()
	p.FillPath(&path, col)
}

func drawPauseGlyph(p *ui.Painter, r ui.Rect, col ui.Color) {
	cx := r.X + r.W*0.5
	cy := r.Y + r.H*0.5
	p.Fill(ui.Rect{X: cx - 3.8, Y: cy - 4.5, W: 2.6, H: 9}, col, 1)
	p.Fill(ui.Rect{X: cx + 1.2, Y: cy - 4.5, W: 2.6, H: 9}, col, 1)
}

// drawTutorialFallbackFrame paints a clean, matching Ocean Paper vector Motion Canvas frame
// while the GPU video player prepares its first frame or on headless/unsupported backends.
func drawTutorialFallbackFrame(p *ui.Painter, r ui.Rect, pal theme.Palette, t float64) {
	p.Fill(r, pal.SettingsBG, 0)
	if r.W < 80 || r.H < 60 {
		return
	}
	scale := r.W / 1280.0
	cx := r.X + r.W*0.5
	cy := r.Y + r.H*0.5 - 12*scale

	// Subtle Ocean Paper dot grid
	step := float32(36) * scale
	if step >= 8 {
		dotCol := pal.Hairline.Alpha(0.8)
		for gx := r.X + step*0.5; gx < r.X+r.W; gx += step {
			for gy := r.Y + step*0.5; gy < r.Y+r.H; gy += step {
				p.Fill(ui.Rect{X: gx - 0.8, Y: gy - 0.8, W: 1.6, H: 1.6}, dotCol, 1)
			}
		}
	}

	// Pan displacement in scene 0
	panX := float32(0)
	if t < 3.8 {
		panX = float32(math.Sin(t*1.6)) * 34 * scale
	}

	rootW, rootH := 148*scale, 52*scale
	brW, brH := 104*scale, 42*scale
	rx := cx - 135*scale + panX
	ry := cy
	bx := cx + 82*scale + panX
	ys := [3]float32{cy - 104*scale, cy, cy + 104*scale}
	labels := [3]string{
		theme.Tr("规划", "Plan"),
		theme.Tr("设计", "Design"),
		theme.Tr("发布", "Ship"),
	}

	// Bezier connectors
	for i, by := range ys {
		var path ui.Path
		x1 := rx + rootW*0.5
		x2 := bx - brW*0.5
		mid := (x1 + x2) * 0.5
		path.MoveTo(x1, ry)
		path.CubeTo(mid, ry, mid, by, x2, by)
		col := pal.Hairline
		if i == 1 && t >= 3.8 {
			col = pal.Accent
		}
		p.StrokePath(&path, max(1.6*scale, 1), col)
	}

	drawCard := func(x, y, w, h float32, text string, isRoot, selected bool) {
		rect := ui.Rect{X: x - w*0.5, Y: y - h*0.5, W: w, H: h}
		rad := theme.RadiusMD * scale
		if isRoot {
			rad = theme.RadiusLG * scale
		}
		bg := pal.Paper
		bd := pal.Hairline
		fg := pal.Text
		if selected {
			bg = pal.SelectionBG
			bd = pal.Accent
			fg = pal.SelectionFG
		} else if isRoot {
			bd = pal.BtnBorder
		}
		p.Fill(rect, bg, rad)
		p.Stroke(rect, bd, rad, max(1.4*scale, 1))
		fs := theme.FontBody
		wt := theme.WeightControl
		if isRoot {
			fs = theme.FontTitle
			wt = theme.WeightTitle
		}
		sp := ui.Span{Text: text, Size: fs, Weight: wt, Color: fg}
		tw, th := p.MeasureText(0, sp)
		p.RichText(x-tw*0.5, y-th*0.5, 0, sp)
	}

	drawCard(rx, ry, rootW, rootH, theme.Tr("项目计划", "Project"), true, false)
	for i, by := range ys {
		drawCard(bx, by, brW, brH, labels[i], false, i == 1 && t >= 3.8)
	}

	// Bottom floating keycap HUD matching current chapter
	ch := tutorialChapters[tutorialChapterIndex(t)]
	keyText := mmShortcutLabel(ch.KeyZh, ch.KeyEn)
	lblText := theme.Tr(ch.Zh, ch.En)
	kSp := ui.Span{Text: keyText, Size: theme.FontMeta, Weight: theme.WeightMono, Color: pal.SelectionFG}
	lSp := ui.Span{Text: lblText, Size: theme.FontSmall, Weight: theme.WeightControl, Color: pal.TextSecondary}
	kw, kh := p.MeasureText(0, kSp)
	lw, lh := p.MeasureText(0, lSp)
	capW := kw + theme.Space16
	capH := theme.HeightCompact
	pillW := capW + theme.Space10 + lw + theme.Space20
	pillH := capH + theme.Space12
	px := cx - pillW*0.5
	py := r.Y + r.H - pillH - theme.Space20
	p.Fill(ui.Rect{X: px, Y: py, W: pillW, H: pillH}, pal.Paper, theme.RadiusLG)
	p.Stroke(ui.Rect{X: px, Y: py, W: pillW, H: pillH}, pal.Hairline, theme.RadiusLG, 1)

	kx := px + theme.Space10
	ky := py + theme.Space6
	p.Fill(ui.Rect{X: kx, Y: ky, W: capW, H: capH}, pal.SelectionBG, theme.RadiusSM)
	p.Stroke(ui.Rect{X: kx, Y: ky, W: capW, H: capH}, pal.Accent, theme.RadiusSM, 1.2)
	p.RichText(kx+(capW-kw)*0.5, ky+(capH-kh)*0.5, 0, kSp)
	p.RichText(kx+capW+theme.Space10, py+(pillH-lh)*0.5, 0, lSp)
}

func clampFloat64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
