package tarot

import (
	"fmt"
	"math"
	"strings"
	"time"

	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// RenderTableRoom renders the 3-card Past/Present/Future ritual table:
// 260px stacked deck sidebar on the left, midnight star-chart spread in the center,
// and 320px reading / mystic-eye panel on the right.
func (ctrl *Controller) RenderTableRoom(c *ui.Context) {
	now := time.Now()

	ctrl.mu.Lock()
	if !ctrl.Table.Initialized {
		ctrl.initSpreadLocked(now)
	}
	ts := ctrl.Table
	noteCount := len(ctrl.notes)
	ctrl.mu.Unlock()

	tc := theme.Tarot()
	reduced := c.Preferences().ReduceMotion

	busy := !ts.BusyUntil.IsZero() && now.Before(ts.BusyUntil)
	shuffling := !ts.ShuffleStart.IsZero() && !ts.ShuffleEnd.IsZero() &&
		!now.Before(ts.ShuffleStart) && now.Before(ts.ShuffleEnd)
	anyDealt := (ts.Dealt[0] && ts.Spread.HasCards[0]) ||
		(ts.Dealt[1] && ts.Spread.HasCards[1]) ||
		(ts.Dealt[2] && ts.Spread.HasCards[2])
	allOpen := ts.Spread.HasCards[0] && ts.Spread.HasCards[1] && ts.Spread.HasCards[2] &&
		ts.Flipped[0] && ts.Flipped[1] && ts.Flipped[2]

	if !reduced && tableHasActiveAnimation(ts, now) {
		c.AnimationFrame()
	}

	winW, winH := c.Size()
	lay := computeTableLayout(winW, winH)

	ui.Row(c).Fill().Background(tc.Ground).Children(func() {
		if lay.showSidebar {
			ctrl.renderTableSidebar(c, lay, ts, noteCount, busy, shuffling, anyDealt, now, reduced)
		}
		ctrl.renderTableCenter(c, lay, ts, busy, now, reduced)
		ctrl.renderTableReadingAside(c, lay, ts, busy, shuffling, anyDealt, allOpen, noteCount, now, reduced)
	})
}

type tableLayout struct {
	showSidebar bool
	sidebarW    float32
	asideW      float32
	centerW     float32
	centerH     float32
	padX        float32
	cardGap     float32
	cardW       float32
	cardH       float32
	chartSize   float32
}

// computeTableLayout keeps three cards fully visible: side panels shrink first,
// then the deck column folds into the reading aside, and card sizes snap to
// 4pt steps so cached card bitmaps survive most resize frames.
func computeTableLayout(winW, winH float32) tableLayout {
	const centerMin = 480
	lay := tableLayout{
		showSidebar: true,
		sidebarW:    theme.TarotSidebarWidth,
		asideW:      theme.TarotReadingAsideWidth,
		padX:        32,
	}
	switch {
	case winW <= 0 || winW-lay.sidebarW-lay.asideW >= centerMin:
	case winW-220-270 >= centerMin:
		lay.sidebarW, lay.asideW = 220, 270
	default:
		lay.showSidebar = false
		lay.sidebarW = 0
		lay.asideW = min(theme.TarotReadingAsideWidth, max(float32(240), float32(math.Floor(float64(winW)*0.34))))
	}
	lay.centerW = max(float32(0), winW-lay.sidebarW-lay.asideW)
	lay.centerH = max(float32(0), winH-theme.TarotTopbarHeight)
	if lay.centerW < 520 {
		lay.padX = 20
	}

	cardW := float32(196)
	lay.cardGap = 56
	if lay.centerW > 0 {
		availW := lay.centerW - lay.padX*2
		lay.cardGap = max(float32(14), min(float32(56), availW*0.07))
		cardW = min(cardW, (availW-2*lay.cardGap)/3)
	}
	if lay.centerH > 0 {
		availH := lay.centerH - 72 - 80 - 24
		if availH > 0 {
			cardW = min(cardW, availH*100.0/172.0)
		}
	}
	cardW = max(float32(96), float32(math.Floor(float64(cardW)/4))*4)
	lay.cardW = cardW
	lay.cardH = float32(math.Round(float64(cardW) * 172.0 / 100.0))

	chart := max(lay.centerW, lay.centerH) * 1.05
	lay.chartSize = min(float32(900), float32(math.Ceil(float64(chart)/60))*60)
	return lay
}

func tableHasActiveAnimation(ts TableState, now time.Time) bool {
	if !ts.BusyUntil.IsZero() && now.Before(ts.BusyUntil) {
		return true
	}
	if !ts.ShuffleStart.IsZero() && !ts.ShuffleEnd.IsZero() && now.Before(ts.ShuffleEnd) {
		return true
	}
	for i := 0; i < 3; i++ {
		if !ts.DealAt[i].IsZero() && now.Sub(ts.DealAt[i]) < 750*time.Millisecond {
			return true
		}
		if !ts.CollectAt[i].IsZero() && now.Sub(ts.CollectAt[i]) < 750*time.Millisecond {
			return true
		}
		if !ts.FlipAt[i].IsZero() && now.Sub(ts.FlipAt[i]) < 860*time.Millisecond {
			return true
		}
		if ts.Flipped[i] && !ts.GlintAt[i].IsZero() && now.Before(ts.GlintAt[i].Add(1050*time.Millisecond)) {
			return true
		}
	}
	if ts.ReadingVisible && !ts.ReadingAt.IsZero() && now.Sub(ts.ReadingAt) < 950*time.Millisecond {
		return true
	}
	return false
}

func (ctrl *Controller) renderTableSidebar(
	c *ui.Context,
	lay tableLayout,
	ts TableState,
	noteCount int,
	busy, shuffling, anyDealt bool,
	now time.Time,
	reduced bool,
) {
	tc := theme.Tarot()
	padX := float32(22)
	if lay.sidebarW < theme.TarotSidebarWidth {
		padX = 18
	}
	ui.Column(c).
		Width(lay.sidebarW).
		FillHeight().
		Shrink(0).
		Background(tc.ChromeBar).
		BorderWidth(0, 1, 0, 0).
		BorderColor(tc.GoldBorderHair).
		Padding(28, padX, 28, padX).
		Gap(28).
		Children(func() {
			// Header + fleuron divider
			ui.Column(c).FillWidth().Gap(theme.Space8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Baseline).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, theme.Tr("牌堆", "Deck")).
						Font(theme.TarotSerifFont()).
						FontSize(theme.FontHeading).
						FontWeight(theme.WeightTitle).
						LetterSpacing(4.8).
						TextColor(tc.TextPrimary).
						SingleLine()
					ui.Text(c, fmt.Sprintf(theme.Tr("%d 篇笔记", "%d notes"), noteCount)).
						FontSize(theme.FontSmall).
						TextColor(tc.TextMuted).
						SingleLine()
				})
				tarotRenderFleuronDivider(c, true)
			})

			// 6-layer stacked deck with riffle animation
			ui.Box(c).Size(150, 257).AlignSelf(ui.Center).Margin(theme.Space10, 0, 0, 0).Cache().Children(func() {
				// Soft cast shadow ellipse beneath deck
				ui.Box(c).Absolute().Left(-8).Right(-14).Bottom(-13).Height(24).IgnoreInput(true).
					Draw(func(p *ui.Painter, r ui.Rect) {
						p.FillRadialGradient(r, ui.RadialGradient{
							Center: ui.Point{X: 0.5, Y: 0.5},
							Stops: []ui.GradientStop{
								{Color: tc.CardShadowDark, At: 0},
								{Color: tc.CardShadowDark.Alpha(0), At: 0.72},
							},
						}, theme.RadiusPill)
					})
				// Slot corner outline behind deck
				ui.Box(c).Absolute().Left(-22).Top(-30).Size(200, 310).IgnoreInput(true).
					Draw(func(p *ui.Painter, r ui.Rect) {
						PaintSlotPlaceholder(p, r)
					})

				for k := 0; k < 6; k++ {
					dx, dy, rot := float32(0), float32(0), float32(0)
					if shuffling && !reduced && k >= 3 {
						dx, dy, rot = tableRiffleOffset(k, now.Sub(ts.ShuffleStart).Seconds())
					}
					paintFull := k == 5 || (shuffling && !reduced && k >= 3)
					layer := ui.Box(c).
						Absolute().
						Left(float32(k)*1.2).
						Top(float32(-k)*2).
						Size(150, 257).
						Radius(theme.RadiusLG).
						Clip().
						Border(1, tc.GoldBorderMid).
						Translate(dx, dy).
						Rotate(rot).
						Draw(func(p *ui.Painter, r ui.Rect) {
							if paintFull {
								PaintCardBack(p, r, theme.RadiusLG, 1.0)
							} else {
								p.FillRadialGradient(r, tc.CardBackGrad, theme.RadiusLG)
							}
							// Crisp parchment edge thickness along bottom/left
							p.Fill(ui.Rect{X: r.X + 2, Y: r.Y + r.H - 1.5, W: r.W - 4, H: 1.5}, tc.TextPrimary.Alpha(0.32), 0)
						})
					blockui.Elevate(layer, theme.ElevationCard)
				}
			})

			// Draw & Shuffle buttons
			ctrl.renderTableDeckActions(c, 46, busy, shuffling, anyDealt)
		})
}

func (ctrl *Controller) renderTableDeckActions(c *ui.Context, btnH float32, busy, shuffling, anyDealt bool) {
	ui.Column(c).FillWidth().Gap(theme.Space10).Children(func() {
		drawLabel := theme.Tr("抽三张牌", "Draw Three Cards")
		if shuffling {
			drawLabel = theme.Tr("洗牌中…", "Shuffling…")
		} else if busy {
			drawLabel = theme.Tr("牌在路上…", "Dealing…")
		} else if anyDealt {
			drawLabel = theme.Tr("重新抽三张", "Draw Three Again")
		}

		drawBtn := tarotGoldPrimaryButton(c, drawLabel, btnH, "tarot-draw", busy, func() {
			ctrl.DrawSpread()
		})
		drawBtn.FillWidth()

		shuffleBtn := tarotGoldSecondaryButton(c, theme.Tr("洗牌", "Shuffle"), btnH, "tarot-shuffle", busy, func() {
			ctrl.ShuffleOnly()
		})
		shuffleBtn.FillWidth()
	})
}

func tableRiffleOffset(layer int, elapsedSec float64) (dx, dy, rot float32) {
	delay := float64(5-layer) * 0.08
	local := elapsedSec - delay
	if local < 0 || local >= 0.74 {
		return 0, 0, 0
	}
	phase := math.Mod(local/0.37, 1.0)
	dir := float32(1)
	peakY := float32(-12)
	settleY := float32(-5)
	if layer%2 == 1 {
		dir = -1
		peakY = -8
		settleY = -3
	}
	switch {
	case phase < 0.35:
		u := tarotEaseInOutCubic(phase / 0.35)
		return dir * 82 * float32(u), peakY * float32(u), dir * 10 * float32(u)
	case phase < 0.70:
		u := tarotEaseInOutCubic((phase - 0.35) / 0.35)
		return dir * 82 * float32(1-u), peakY + (settleY-peakY)*float32(u), dir * 10 * float32(1-u)
	default:
		u := tarotEaseInOutCubic((phase - 0.70) / 0.30)
		return 0, settleY * float32(1-u), 0
	}
}

func (ctrl *Controller) renderTableCenter(
	c *ui.Context,
	lay tableLayout,
	ts TableState,
	busy bool,
	now time.Time,
	reduced bool,
) {
	cardW, cardH, cardGap, padX := lay.cardW, lay.cardH, lay.cardGap, lay.padX
	chartSize := lay.chartSize

	ui.Box(c).Grow(1).FillHeight().MinWidth(0).Center().Clip().
		Padding(32, padX, 40, padX).
		Draw(func(p *ui.Painter, r ui.Rect) {
			PaintTableCloth(p, r, false)
		}).
		Children(func() {
			ui.Row(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Center().IgnoreInput(true).ZIndex(-1).Children(func() {
				ui.Box(c).Size(chartSize, chartSize).Shrink(0).Cache().
					Draw(func(p *ui.Painter, r ui.Rect) {
						PaintStarChart(p, r)
					})
			})
			ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).IgnoreInput(true).ZIndex(-1).
				Draw(func(p *ui.Painter, r ui.Rect) {
					tarotPaintTableFrameAndCorners(p, r)
				})
			if lay.centerW >= 420 && lay.centerH >= 600 {
				ui.Box(c).Absolute().Left(38).Bottom(38).Size(124, 124).IgnoreInput(true).ZIndex(-1).Cache().
					Draw(func(p *ui.Painter, r ui.Rect) {
						PaintCompass(p, r)
					})
			}
			ui.Row(c).Gap(cardGap).AlignItems(ui.Start).Justify(ui.Center).Children(func() {
				for i := 0; i < 3; i++ {
					ctrl.renderTableCardSlot(c, ts, i, cardW, cardH, busy, now, reduced)
				}
			})
		})
}

func (ctrl *Controller) renderTableCardSlot(
	c *ui.Context,
	ts TableState,
	idx int,
	cardW, cardH float32,
	busy bool,
	now time.Time,
	reduced bool,
) {
	tc := theme.Tarot()
	card := ts.Spread.Cards[idx]
	hasCard := ts.Spread.HasCards[idx]
	dealt := ts.Dealt[idx] && hasCard
	flipped := ts.Flipped[idx]

	// Fly-in / collect animation progress: 1 = resting in slot, 0 = at left deck
	dealP := 1.0
	cardOpacity := float32(1)
	collecting := false
	if dealt {
		if !reduced && !ts.DealAt[idx].IsZero() {
			el := now.Sub(ts.DealAt[idx]).Seconds()
			if el < 0.70 {
				t := tarotAnimClamp01(el / 0.70)
				dealP = tarotCubicBezier(0.2, 1.12, 0.3, 1.0, t)
				cardOpacity = float32(tarotAnimClamp01(el / 0.22))
			}
		}
	} else if hasCard && !reduced && !ts.CollectAt[idx].IsZero() {
		el := now.Sub(ts.CollectAt[idx]).Seconds()
		if el >= 0 && el < 0.72 {
			collecting = true
			t := tarotAnimClamp01(el / 0.72)
			eased := tarotCubicBezier(0.2, 0.78, 0.24, 1.0, t)
			dealP = 1.0 - eased
			cardOpacity = float32(1.0 - tarotAnimClamp01((el-0.50)/0.22))
		}
	}

	// 3D Flip angle (0 = back, 180 = face) & flip lift
	flipAngle := float32(0)
	if flipped {
		flipAngle = 180
	}
	flipLift := float32(0)
	if !reduced && !ts.FlipAt[idx].IsZero() {
		el := now.Sub(ts.FlipAt[idx]).Seconds()
		if el >= 0 && el < 0.82 {
			t := tarotAnimClamp01(el / 0.82)
			eased := tarotCubicBezier(0.3, 1.22, 0.42, 1.0, t)
			if flipped {
				flipAngle = float32(eased * 180)
			} else {
				flipAngle = float32((1.0 - eased) * 180)
			}
			flipLift = float32(math.Sin(tarotAnimClamp01(t/0.92) * math.Pi))
		}
	}

	// Glint sweep progress (0 = inactive)
	glintProg := float32(0)
	edgeFlash := float32(0)
	if !reduced && flipped && !ts.GlintAt[idx].IsZero() {
		el := now.Sub(ts.GlintAt[idx]).Seconds()
		if el >= 0 && el < 0.98 {
			glintProg = float32(el / 0.98)
			if el < 0.20 {
				edgeFlash = float32(el / 0.20)
			} else {
				edgeFlash = float32(1.0 - tarotAnimClamp01((el-0.20)/0.78))
			}
		}
	}

	ui.Column(c).AlignItems(ui.Center).Gap(34).Children(func() {
		// Slot container (responsive 100:172 card slot)
		ui.Box(c).Size(cardW, cardH).Radius(theme.TarotRadiusCard).
			Border(1, tc.GoldBorderSoft).BorderStyle(ui.BorderDashed).
			Draw(func(p *ui.Painter, r ui.Rect) {
				PaintSlotPlaceholder(p, r)
			}).
			Children(func() {
				if dealt || collecting {
					// Soft cast shadow below card
					shadowOp := float32(0.90)
					if !dealt {
						shadowOp = cardOpacity * 0.85
					} else if flipLift > 0.1 {
						shadowOp = 0.90 - 0.45*flipLift
					}
					ui.Box(c).Absolute().Left(14).Right(14).Bottom(-16).Height(26).IgnoreInput(true).
						Opacity(shadowOp).
						Translate(0, 10*flipLift).
						Scale(1+0.12*flipLift, 1+0.12*flipLift).
						Draw(func(p *ui.Painter, r ui.Rect) {
							p.FillRadialGradient(r, ui.RadialGradient{
								Center: ui.Point{X: 0.5, Y: 0.5},
								Stops: []ui.GradientStop{
									{Color: tc.CardShadowDark, At: 0},
									{Color: tc.CardShadowDark.Alpha(0), At: 0.68},
								},
							}, theme.RadiusPill)
						})

					deckOffsetX := float32(-520 - idx*250)
					tx := deckOffsetX * float32(1.0-dealP)
					rotDeg := float32(-7.0) * float32(1.0-dealP)
					sc := float32(0.765 + 0.235*dealP + 0.04*float64(flipLift))

					cardProbeID := fmt.Sprintf("tarot-table-card-%d", idx)
					cardBtn := ui.ButtonBase(c.Key(cardProbeID)).
						Absolute().Left(-1).Top(-1).Size(cardW+2, cardH+2).
						Radius(theme.TarotRadiusCard).
						Cursor(ui.CursorPointer).
						Label(card.Position + " · " + card.Note.Title)

					hovTarget := float32(0)
					if cardBtn.Hovered() && dealt && !busy {
						hovTarget = 1
					}
					hov := cardBtn.AnimateTo("tableCardHover", hovTarget, blockui.SpringTactile)
					ty := -16*flipLift - 8*hov*(1-flipLift)

					cardBtn.
						Opacity(cardOpacity).
						Translate(tx, ty).
						Rotate(rotDeg).
						Scale(sc, sc).
						OnClick(func() {
							if !busy && dealt {
								ctrl.ToggleTableCard(idx)
							}
						}).
						DrawOver(func(p *ui.Painter, r ui.Rect) {
							blockui.RecordBounds(cardProbeID, r)
							if hov > 0.02 && flipLift < 0.2 {
								p.Stroke(r, tc.GoldBright.Alpha(0.85*hov), theme.TarotRadiusCard, 1.5)
							}
						}).
						Children(func() {
							isFlipping := flipAngle > 0.1 && flipAngle < 179.9
							showBack := isFlipping || flipAngle <= 0.1
							showFront := isFlipping || flipAngle >= 179.9

							// Back face (visible when flipAngle < 90)
							if showBack {
								back := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(theme.TarotRadiusCard).Clip().Cache().
									Draw(func(p *ui.Painter, r ui.Rect) {
										PaintCardBack(p, r, theme.TarotRadiusCard, 1.1)
									})
								if isFlipping {
									back.RotateY(flipAngle).Perspective(1300).BackfaceHidden()
								}
								blockui.Elevate(back, theme.ElevationTarotCard)
							}

							// Front face (visible when flipAngle > 90)
							if showFront {
								front := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(theme.TarotRadiusCard).Clip().Cache().
									Padding(8).
									Draw(func(p *ui.Painter, r ui.Rect) {
										PaintCardFrame(p, r, theme.TarotRadiusCard)
									}).
									DrawOver(func(p *ui.Painter, r ui.Rect) {
										if glintProg > 0 {
											PaintGlintSweep(p, r, theme.TarotRadiusCard, glintProg)
											p.AnimationFrameRate(60)
										}
										if edgeFlash > 0.01 {
											p.Stroke(r, tc.GoldBright.Alpha(edgeFlash), theme.TarotRadiusCard, 2)
										}
									}).
									Children(func() {
										padTop, padBot := float32(10), float32(24)
										if card.Reversed {
											padTop, padBot = 24, 12
										}
										face := ui.Column(c).Fill().AlignItems(ui.Center).
											Padding(padTop, 10, padBot, 10).Gap(theme.Space4)
										if card.Reversed {
											face.Rotate(180)
										}
										arc := card.Note.Arcana()
										face.Children(func() {
											ui.Text(c, arc.Num).
												Font(theme.TarotRomanFont()).
												FontSize(theme.TarotFontRomanCard).
												FontWeight(theme.WeightTitle).
												LetterSpacing(1.8).
												TextColor(tc.InkDark).
												SingleLine()

											ui.Box(c).FillWidth().Grow(1).MinHeight(0).
												Draw(func(p *ui.Painter, r ui.Rect) {
													PaintArcanaGlyph(p, arc.Key, r, 1.25, 1.0)
												})

											ui.Text(c, arc.Name()).
												Font(theme.TarotSerifFont()).
												FontSize(theme.FontMeta).
												LetterSpacing(4.8).
												TextColor(tc.InkMuted).
												SingleLine()

											ui.Box(c).Size(59, 7).Margin(3, 0, 1, 0).Shrink(0).
												Draw(func(p *ui.Painter, r ui.Rect) {
													PaintFleuron(p, r, tc.GoldDark, 1.3)
												})

											ui.Text(c, card.Note.Title).
												FillWidth().
												Font(theme.TarotSerifFont()).
												FontSize(theme.TarotFontCardTitle).
												FontWeight(theme.WeightTitle).
												LineHeight(1.4).
												TextColor(tc.InkDark).
												TextAlign(ui.Center).
												MaxLines(2)
										})
									})
								if isFlipping {
									front.RotateY(flipAngle + 180).Perspective(1300).BackfaceHidden()
								}
								blockui.Elevate(front, theme.ElevationTarotCard)
							}
						})
				}

				// Top "正在写" badge on Present (middle) card
				if idx == 1 && dealt {
					ui.Row(c).Absolute().Left(0).Right(0).Top(-13).Justify(ui.Center).IgnoreInput(true).Children(func() {
						ui.Row(c).Height(24).Padding(0, theme.Space12, 0, theme.Space10).Gap(theme.Space6).
							AlignItems(ui.Center).Radius(theme.RadiusPill).
							Background(tc.Ground).Border(1, tc.Gold).
							Children(func() {
								IconBox(c, IconQuill, theme.IconMicro, tc.GoldBright)
								ui.Text(c, theme.Tr("正在写", "Writing")).
									Font(theme.TarotSerifFont()).
									FontSize(theme.FontMeta).
									LetterSpacing(2.4).
									TextColor(tc.GoldBright).
									SingleLine()
							})
					})
				}

				// Bottom action capsule ("重读" / "续写" / "动笔")
				if dealt && flipped && !busy {
					ui.Row(c).Absolute().Left(0).Right(0).Bottom(-18).Justify(ui.Center).Children(func() {
						var actBtn ui.Element
						if idx == 1 {
							actBtn = ui.ButtonBase(c).Height(36).PaddingX(22).Radius(theme.RadiusPill).
								Cursor(ui.CursorPointer).Label(card.ActionText).
								Draw(func(p *ui.Painter, r ui.Rect) {
									p.FillGradient(r, tc.GoldBtnGrad, theme.RadiusPill)
									p.Fill(ui.Rect{X: r.X + 8, Y: r.Y + 1, W: r.W - 16, H: 1}, tc.GoldGlintCore.Alpha(0.36), 0)
								})
							blockui.Elevate(actBtn, theme.ElevationTarotCard)
							actBtn.Children(func() {
								ui.Text(c, card.ActionText).
									FontSize(theme.FontBody).
									FontWeight(theme.WeightTitle).
									LetterSpacing(2.8).
									TextColor(tc.GoldBtnText).
									SingleLine()
							})
						} else {
							actBtn = ui.ButtonBase(c).Height(36).PaddingX(22).Radius(theme.RadiusPill).
								Background(tc.Ground).Border(1, tc.GoldBorderMid).
								Cursor(ui.CursorPointer).Label(card.ActionText)
							blockui.Elevate(actBtn, theme.ElevationTarotCard)
							actBtn.Children(func() {
								ui.Text(c, card.ActionText).
									FontSize(theme.FontBody).
									LetterSpacing(2.8).
									TextColor(tc.TextPrimary).
									SingleLine()
							})
						}
						if actBtn.Hovered() {
							actBtn.BorderColor(tc.GoldBright)
						}
						if actBtn.Pressed() {
							actBtn.Translate(0, 1)
						}
						actBtn.OnClick(func() {
							ctrl.OpenNoteFromRitual(card.Note.Path, false)
						})
						blockui.Probe(actBtn, fmt.Sprintf("tarot-table-act-%d", idx))
					})
				}
			})

		// Position label + orientation meta below slot
		ui.Column(c).AlignItems(ui.Center).Gap(theme.Space6).MinHeight(46).Children(func() {
			posLabel := card.Position
			if posLabel == "" {
				labels := [3]string{
					theme.Tr("过去", "Past"),
					theme.Tr("现在", "Present"),
					theme.Tr("未来", "Future"),
				}
				posLabel = labels[idx]
			}
			ui.Text(c, posLabel).
				Font(theme.TarotSerifFont()).
				FontSize(theme.TarotFontPosLabel).
				LetterSpacing(8).
				TextColor(tc.TextPrimary).
				SingleLine()

			if dealt && flipped && card.MetaText != "" {
				metaColor := tc.TextMuted
				if card.Reversed {
					metaColor = tc.ReversedDark
				}
				ui.Text(c, card.MetaText).
					FontSize(theme.FontSmall).
					TextColor(metaColor).
					SingleLine()
			}
		})
	})
}

func (ctrl *Controller) renderTableReadingAside(
	c *ui.Context,
	lay tableLayout,
	ts TableState,
	busy, shuffling, anyDealt, allOpen bool,
	noteCount int,
	now time.Time,
	reduced bool,
) {
	tc := theme.Tarot()
	padX, padY := float32(30), float32(44)
	if lay.asideW < theme.TarotReadingAsideWidth {
		padX, padY = 22, 32
	}
	ui.Column(c).
		Width(lay.asideW).
		FillHeight().
		Shrink(0).
		Background(tc.PanelDark).
		BorderWidth(0, 0, 0, 1).
		BorderColor(tc.GoldBorderHair).
		Children(func() {
			blockui.Scroll(c.Key("tarot-table-aside")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Padding(padY, padX, padY, padX).Gap(24).Children(func() {
					ctrl.renderTableReadingBody(c, ts, busy, shuffling, anyDealt, allOpen, noteCount, now, reduced)
				})
			})
			if !lay.showSidebar {
				ui.Column(c).FillWidth().Shrink(0).Padding(theme.Space16, padX, padY*0.75, padX).
					BorderWidth(1, 0, 0, 0).BorderColor(tc.GoldBorderHair).
					Children(func() {
						ctrl.renderTableDeckActions(c, 40, busy, shuffling, anyDealt)
					})
			}
		})
}

func (ctrl *Controller) renderTableReadingBody(
	c *ui.Context,
	ts TableState,
	busy, shuffling, anyDealt, allOpen bool,
	noteCount int,
	now time.Time,
	reduced bool,
) {
	tc := theme.Tarot()
	if ts.ReadingVisible && allOpen {
		readOp := float32(1)
		readDY := float32(0)
		if !reduced && !ts.ReadingAt.IsZero() {
			el := now.Sub(ts.ReadingAt).Seconds()
			if el >= 0 && el < 0.90 {
				t := tarotEaseOutCubic(tarotAnimClamp01(el / 0.90))
				readOp = float32(t)
				readDY = float32(8 * (1 - t))
			}
		}

		ui.Column(c).FillWidth().Gap(28).Opacity(readOp).Translate(0, readDY).Children(func() {
			ui.Column(c).FillWidth().Gap(theme.Space10).Children(func() {
				ui.Text(c, theme.Tr("解读", "Reading")).
					Font(theme.TarotSerifFont()).
					FontSize(theme.FontTitle).
					FontWeight(theme.WeightTitle).
					LetterSpacing(6).
					TextColor(tc.Gold).
					SingleLine()
				tarotRenderFleuronDivider(c, true)
			})

			lineText := strings.TrimSpace(ts.AIReading)
			if lineText == "" {
				if ts.AILoading {
					lineText = theme.Tr("正在结合你的笔记推演牌意…", "Weaving an editorial reading from your notes…")
				} else if len(ts.Spread.Lines) > 0 {
					lineText = ts.Spread.Lines[ts.AltIndex%len(ts.Spread.Lines)]
				}
			}

			ui.Text(c, lineText).
				FillWidth().
				Font(theme.TarotSerifFont()).
				FontSize(theme.TarotFontReading).
				LineHeight(1.95).
				TextColor(tc.TextPrimary)

			ui.Column(c).FillWidth().Gap(theme.Space10).Children(func() {
				ctaLabel := fmt.Sprintf(theme.Tr("续写《%s》", "Continue \"%s\""), ts.Spread.CTATitle)
				ctaBtn := tarotGoldPrimaryButton(c, ctaLabel, 46, "tarot-table-cta", ts.Spread.CTAPath == "", func() {
					ctrl.OpenNoteFromRitual(ts.Spread.CTAPath, false)
				})
				ctaBtn.FillWidth()

				altLabel := theme.Tr("换一种读法", "Another Angle")
				if ts.AILoading {
					altLabel = theme.Tr("推演中…", "Reading…")
				}
				altBtn := tarotGoldSecondaryButton(c, altLabel, 46, "tarot-table-alt", ts.AILoading, func() {
					ctrl.NextReading()
				})
				altBtn.FillWidth()
			})
		})
		return
	}

	// Waiting state: breathing mystic eye + calm status hint
	ui.Column(c).FillWidth().AlignItems(ui.Start).Gap(theme.Space16).Children(func() {
		ui.Box(c).Size(44, 28).Draw(func(p *ui.Painter, r ui.Rect) {
			alpha := float32(0.85)
			if !p.Preferences().ReduceMotion {
				phase := float64(p.Now().UnixMilli()%1800) / 1800.0
				alpha = float32(0.35 + 0.65*(0.5-0.5*math.Cos(phase*2*math.Pi)))
				p.AnimationFrameRate(30)
			}
			PaintMysticEye(p, r, alpha)
		})

		waitLabel := theme.Tr("牌桌已清空", "Table cleared")
		if shuffling || busy {
			waitLabel = theme.Tr("牌正在显影", "Cards emerging")
		} else if anyDealt {
			waitLabel = theme.Tr("翻开全部三张牌", "Reveal all three cards")
		} else if noteCount == 0 {
			waitLabel = theme.Tr("先写一篇笔记即可抽牌", "Create a note to draw")
		}

		ui.Text(c, waitLabel).
			Font(theme.TarotSerifFont()).
			FontSize(theme.TarotFontRomanSmall).
			LetterSpacing(3.2).
			TextColor(tc.TextMuted)
	})
}

// --- Shared Tarot room helpers (used by view_table, view_daily, view_deck) ---

func tarotAnimClamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func tarotEaseOutCubic(t float64) float64 {
	inv := 1 - tarotAnimClamp01(t)
	return 1 - inv*inv*inv
}

func tarotEaseInOutCubic(t float64) float64 {
	t = tarotAnimClamp01(t)
	if t < 0.5 {
		return 4 * t * t * t
	}
	inv := -2*t + 2
	return 1 - (inv*inv*inv)/2
}

func tarotCubicBezier(x1, y1, x2, y2, t float64) float64 {
	t = tarotAnimClamp01(t)
	sample := func(a, b, u float64) float64 {
		inv := 1 - u
		return 3*inv*inv*u*a + 3*inv*u*u*b + u*u*u
	}
	lo, hi := 0.0, 1.0
	for i := 0; i < 14; i++ {
		mid := 0.5 * (lo + hi)
		if sample(x1, x2, mid) < t {
			lo = mid
		} else {
			hi = mid
		}
	}
	return sample(y1, y2, 0.5*(lo+hi))
}

func tarotRenderFleuronDivider(c *ui.Context, leftBright bool) {
	tc := theme.Tarot()
	leftFrom, leftTo := tc.GoldBorderSub, tc.GoldBorderMid
	rightFrom, rightTo := tc.GoldBorderMid, tc.GoldBorderSub
	if leftBright {
		leftFrom, leftTo = tc.GoldBorderMid, tc.GoldBorderSub
		rightFrom, rightTo = tc.GoldBorderSub, tc.GoldBorderMid
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
		ui.Box(c).Grow(1).Height(1).Draw(func(p *ui.Painter, r ui.Rect) {
			p.FillGradient(r, ui.LinearGradient{Angle: 90, From: leftFrom, To: leftTo}, 0)
		})
		ui.Box(c).Size(24, 8).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			PaintFleuron(p, r, tc.Gold, 1)
		})
		ui.Box(c).Grow(1).Height(1).Draw(func(p *ui.Painter, r ui.Rect) {
			p.FillGradient(r, ui.LinearGradient{Angle: 90, From: rightFrom, To: rightTo}, 0)
		})
	})
}

func tarotPaintTableFrameAndCorners(p *ui.Painter, r ui.Rect) {
	tc := theme.Tarot()
	if r.W <= 40 || r.H <= 40 {
		return
	}
	p.Stroke(ui.Rect{X: r.X + 14, Y: r.Y + 14, W: r.W - 28, H: r.H - 28}, tc.GoldBorderSoft, 0, 1)
	p.Stroke(ui.Rect{X: r.X + 18, Y: r.Y + 18, W: r.W - 36, H: r.H - 36}, tc.GoldBorderSub, 0, 1)
	tarotPaintCornerFlourishes(p, r, 11, 34, tc.Gold, 1.1)
}

func tarotPaintCornerFlourishes(p *ui.Painter, r ui.Rect, inset, size float32, col ui.Color, strokeW float32) {
	if r.W < size*2+inset*2 || r.H < size*2+inset*2 {
		return
	}
	PaintCornerFlourish(p, ui.Rect{X: r.X + inset, Y: r.Y + inset, W: size, H: size}, 0, col, strokeW)
	PaintCornerFlourish(p, ui.Rect{X: r.X + r.W - inset - size, Y: r.Y + inset, W: size, H: size}, 90, col, strokeW)
	PaintCornerFlourish(p, ui.Rect{X: r.X + r.W - inset - size, Y: r.Y + r.H - inset - size, W: size, H: size}, 180, col, strokeW)
	PaintCornerFlourish(p, ui.Rect{X: r.X + inset, Y: r.Y + r.H - inset - size, W: size, H: size}, 270, col, strokeW)
}

func tarotGoldPrimaryButton(
	c *ui.Context,
	label string,
	h float32,
	probeID string,
	disabled bool,
	onClick func(),
) ui.Element {
	tc := theme.Tarot()
	cursor := ui.CursorPointer
	if disabled {
		cursor = ui.CursorDefault
	}
	btn := ui.ButtonBase(c).Height(h).PaddingX(theme.Space20).Radius(theme.RadiusMD).
		Center().Cursor(cursor).Disabled(disabled).DimDisabled(false).Label(label)
	if disabled {
		btn.Opacity(0.55)
	}
	btn.Draw(func(p *ui.Painter, r ui.Rect) {
		p.FillGradient(r, tc.GoldBtnGrad, theme.RadiusMD)
		p.Fill(ui.Rect{X: r.X + 4, Y: r.Y + 1, W: max(float32(0), r.W-8), H: 1}, tc.GoldGlintCore.Alpha(0.38), 0)
		p.Fill(ui.Rect{X: r.X + 4, Y: r.Y + r.H - 1.5, W: max(float32(0), r.W-8), H: 1}, tc.GoldDeep.Alpha(0.45), 0)
		if !disabled && btn.Hovered() {
			p.Fill(r, tc.GoldLight.Alpha(0.14), theme.RadiusMD)
		}
		if !disabled && btn.Pressed() {
			p.Fill(r, tc.GoldDeep.Alpha(0.16), theme.RadiusMD)
		}
	})
	if !disabled && btn.Pressed() {
		btn.Translate(0, 1)
	}
	btn.Children(func() {
		ui.Text(c, label).
			FontSize(theme.FontBody).
			FontWeight(theme.WeightTitle).
			TextColor(tc.GoldBtnText).
			SingleLine()
	})
	if onClick != nil {
		btn.OnClick(func() {
			if !disabled {
				onClick()
			}
		})
	}
	if probeID != "" {
		blockui.Probe(btn, probeID)
	}
	return btn
}

func tarotGoldSecondaryButton(
	c *ui.Context,
	label string,
	h float32,
	probeID string,
	disabled bool,
	onClick func(),
) ui.Element {
	tc := theme.Tarot()
	cursor := ui.CursorPointer
	if disabled {
		cursor = ui.CursorDefault
	}
	btn := ui.ButtonBase(c).Height(h).PaddingX(theme.Space20).Radius(theme.RadiusMD).
		Center().Cursor(cursor).Disabled(disabled).DimDisabled(false).Label(label).
		Border(1, tc.GoldBorderMid).
		InsetShadow(0, 0, 0, 2, tc.Ground.Alpha(0.40)).
		InsetShadow(0, 0, 0, 3, tc.GoldBorderSub)
	if disabled {
		btn.Opacity(0.55)
	} else if btn.Pressed() {
		btn.Background(tc.GoldPressWash).BorderColor(tc.GoldBright).Translate(0, 1)
	} else if btn.Hovered() {
		btn.Background(tc.GoldHoverWash).BorderColor(tc.GoldBright)
	}
	btn.Children(func() {
		ui.Text(c, label).
			FontSize(theme.FontBody).
			TextColor(tc.TextPrimary).
			SingleLine()
	})
	if onClick != nil {
		btn.OnClick(func() {
			if !disabled {
				onClick()
			}
		})
	}
	if probeID != "" {
		blockui.Probe(btn, probeID)
	}
	return btn
}

func tarotFormatDeckWhen(drawnUnix int64, reversed bool, now time.Time) string {
	orient := theme.Tr("正位", "Upright")
	if reversed {
		orient = theme.Tr("逆位", "Reversed")
	}
	if drawnUnix <= 0 {
		return orient
	}
	t := time.Unix(drawnUnix, 0)
	if t.Format("2006-01-02") == now.Format("2006-01-02") {
		return theme.Tr("今天抽到 · ", "Drawn today · ") + orient
	}
	if theme.IsEn() {
		return fmt.Sprintf("Drawn %s · %s", t.Format("Jan 2"), orient)
	}
	return fmt.Sprintf("%d 月 %d 日抽到 · %s", int(t.Month()), t.Day(), orient)
}
