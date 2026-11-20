package tarot

import (
	"math"
	"time"

	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// RenderDailyRoom renders the single-card daily invitation ritual:
// rotating moon-phase ring over midnight glow, 300x514 breathing halo ritual card,
// Upright/Reversed actions ("读它" / "质疑它"), and the "站在反面" Devil's Advocate panel.
func (ctrl *Controller) RenderDailyRoom(c *ui.Context) {
	now := time.Now()
	today := now.Format("2006-01-02")

	ctrl.mu.Lock()
	if !ctrl.Daily.Initialized || ctrl.Daily.Date != today {
		ctrl.ensureDailyLocked(now)
	}
	ds := ctrl.Daily
	ctrl.mu.Unlock()

	dc := ds.Card
	reduced := c.Preferences().ReduceMotion

	if !reduced && dailyHasActiveAnimation(ds, now) {
		c.AnimationFrame()
	}

	ui.Box(c).Fill().Clip().
		Draw(func(p *ui.Painter, r ui.Rect) {
			PaintTableCloth(p, r, true)
			ringSize := float32(760)
			ringRect := ui.Rect{
				X: r.X + (r.W-ringSize)*0.5,
				Y: r.Y + (r.H-ringSize)*0.5,
				W: ringSize,
				H: ringSize,
			}
			angleDeg := float32(0)
			if !p.Preferences().ReduceMotion {
				// 240s per full revolution (1.5 deg/s)
				angleDeg = float32(math.Mod(float64(p.Now().UnixMilli())/1000.0*1.5, 360))
				p.AnimationFrameRate(30)
			}
			PaintMoonPhaseRing(p, ringRect, angleDeg)
			tarotPaintTableFrameAndCorners(p, r)
		}).
		Children(func() {
			ui.Grid(c).
				Fill().
				ColumnTracks(ui.Fr(1), ui.FitContent(), ui.Fr(1)).
				GapX(56).
				Padding(40, 56, 48, 56).
				AlignItems(ui.Center).
				Children(func() {
					// Left balance spacer
					ui.Box(c).MinWidth(0)

					// Center ritual column
					ctrl.renderDailyCenterColumn(c, ds, dc, now, reduced)

					// Right Devil's Advocate card slot
					ui.Box(c).MinWidth(0).MaxWidth(380).AlignItems(ui.Start).Children(func() {
						if ds.Flipped && ds.DoubtOpen {
							ctrl.renderDailyDoubtCard(c, ds, dc, now, reduced)
						}
					})
				})
		})
}

func dailyHasActiveAnimation(ds DailyState, now time.Time) bool {
	if !ds.FlipAt.IsZero() && now.Sub(ds.FlipAt) < 1000*time.Millisecond {
		return true
	}
	if ds.Flipped && !ds.GlintAt.IsZero() && now.Before(ds.GlintAt.Add(1300*time.Millisecond)) {
		return true
	}
	if ds.DoubtOpen && !ds.DoubtAt.IsZero() && now.Sub(ds.DoubtAt) < 950*time.Millisecond {
		return true
	}
	return false
}

func (ctrl *Controller) renderDailyCenterColumn(
	c *ui.Context,
	ds DailyState,
	dc DailyCard,
	now time.Time,
	reduced bool,
) {
	tc := theme.Tarot()
	arc := dc.Note.Arcana()

	// Flip angle (0 = back, 180 = face) & lift
	flipAngle := float32(0)
	if ds.Flipped {
		flipAngle = 180
	}
	flipLift := float32(0)
	if !reduced && !ds.FlipAt.IsZero() {
		el := now.Sub(ds.FlipAt).Seconds()
		if el >= 0 && el < 0.98 {
			t := tarotAnimClamp01(el / 0.98)
			eased := tarotCubicBezier(0.3, 1.2, 0.42, 1.0, t)
			if ds.Flipped {
				flipAngle = float32(eased * 180)
			} else {
				flipAngle = float32((1.0 - eased) * 180)
			}
			flipLift = float32(math.Sin(tarotAnimClamp01(t/0.85) * math.Pi))
		}
	}

	// Glint sweep progress
	glintProg := float32(0)
	edgeFlash := float32(0)
	if !reduced && ds.Flipped && !ds.GlintAt.IsZero() {
		el := now.Sub(ds.GlintAt).Seconds()
		if el >= 0 && el < 1.25 {
			glintProg = float32(el / 1.25)
			if el < 0.22 {
				edgeFlash = float32(el / 0.22)
			} else {
				edgeFlash = float32(1.0 - tarotAnimClamp01((el-0.22)/1.03))
			}
		}
	}

	_, winH := c.Size()
	cardH := float32(514)
	if winH > 0 {
		availH := winH - theme.TarotTopbarHeight - 236
		if availH > 0 && cardH > availH {
			cardH = max(float32(360), availH)
		}
	}
	cardW := cardH * 300.0 / 514.0

	ui.Column(c).AlignItems(ui.Center).Gap(theme.Space20).Children(func() {
		// Top date header flanked by diamond gold lines
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space16).Margin(0, 0, theme.Space12, 0).Children(func() {
			ui.Box(c).Size(46, 8).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
				paintDailyDateOrnament(p, r, true, tc.Gold)
			})
			ui.Text(c, dc.DateDisplay).
				Font(theme.TarotSerifFont()).
				FontSize(theme.TarotFontRomanSmall).
				FontWeight(theme.WeightTitle).
				LetterSpacing(8).
				TextColor(tc.Gold).
				SingleLine()
			ui.Box(c).Size(46, 8).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
				paintDailyDateOrnament(p, r, false, tc.Gold)
			})
		})

		// Responsive 300:514 ritual card
		ui.Box(c).Size(cardW, cardH).Children(func() {
			// Cast shadow beneath card
			shadowOp := float32(0.90 - 0.45*flipLift)
			ui.Box(c).Absolute().Left(26).Right(26).Bottom(-20).Height(30).IgnoreInput(true).
				Opacity(shadowOp).
				Translate(0, 12*flipLift).
				Scale(1+0.10*flipLift, 1+0.10*flipLift).
				Draw(func(p *ui.Painter, r ui.Rect) {
					p.FillRadialGradient(r, ui.RadialGradient{
						Center: ui.Point{X: 0.5, Y: 0.5},
						Stops: []ui.GradientStop{
							{Color: tc.CardShadowDark, At: 0},
							{Color: tc.CardShadowDark.Alpha(0), At: 0.68},
						},
					}, theme.RadiusPill)
				})

			// Breathing gold halo around unflipped card
			if !ds.Flipped {
				ui.Box(c).Absolute().Left(-12).Top(-12).Right(-12).Bottom(-12).IgnoreInput(true).
					Draw(func(p *ui.Painter, r ui.Rect) {
						alpha := float32(0.55)
						scale := float32(1)
						if !p.Preferences().ReduceMotion {
							phase := float64(p.Now().UnixMilli()%2600) / 2600.0
							wave := float32(0.5 - 0.5*math.Cos(phase*2*math.Pi))
							alpha = 0.25 + 0.45*wave
							scale = 1 + 0.018*wave
							p.AnimationFrameRate(30)
						}
						cx, cy := r.X+r.W*0.5, r.Y+r.H*0.5
						hw, hh := r.W*0.5*scale, r.H*0.5*scale
						outer := ui.Rect{X: cx - hw, Y: cy - hh, W: hw * 2, H: hh * 2}
						p.Stroke(outer, tc.Gold.Alpha(alpha), theme.TarotRadiusDailyCard, 1)
						outer2 := ui.Rect{X: outer.X - 4, Y: outer.Y - 4, W: outer.W + 8, H: outer.H + 8}
						p.Stroke(outer2, tc.Gold.Alpha(alpha*0.38), theme.TarotRadiusDailyCard, 1)
					})
			}

			ariaLabel := theme.Tr("翻开今日之牌", "Reveal today's card")
			if ds.Flipped {
				ariaLabel = theme.Tr("翻回牌背", "Turn card face down")
			}
			cardBtn := ui.ButtonBase(c.Key("tarot-daily-card")).Fill().Radius(theme.TarotRadiusDailyCard).
				Cursor(ui.CursorPointer).Label(ariaLabel)

			hovTarget := float32(0)
			if cardBtn.Hovered() && !ds.Flipped {
				hovTarget = 1
			}
			hov := cardBtn.AnimateTo("dailyHover", hovTarget, blockui.SpringTactile)
			up := -22*flipLift - 6*hov*(1-flipLift)
			sc := 1 + 0.035*flipLift

			cardBtn.
				Translate(0, up).
				Scale(sc, sc).
				OnClick(func() {
					ctrl.FlipDaily()
				}).
				DrawOver(func(p *ui.Painter, r ui.Rect) {
					blockui.RecordBounds("tarot-daily-card", r)
					if cardBtn.Hovered() && flipLift < 0.2 {
						p.Stroke(r, tc.GoldBright.Alpha(0.85), theme.TarotRadiusDailyCard, 1.5)
					}
				}).
				Children(func() {
					isFlipping := flipAngle > 0.1 && flipAngle < 179.9
					showBack := isFlipping || flipAngle <= 0.1
					showFront := isFlipping || flipAngle >= 179.9

					// Back face
					if showBack {
						back := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(theme.TarotRadiusDailyCard).Clip().
							Draw(func(p *ui.Painter, r ui.Rect) {
								PaintCardBack(p, r, theme.TarotRadiusDailyCard, 1.3)
							})
						if isFlipping {
							back.RotateY(flipAngle).Perspective(1600).BackfaceHidden()
						}
						blockui.Elevate(back, theme.ElevationTarotCard)
					}

					// Front face
					if showFront {
						front := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(theme.TarotRadiusDailyCard).Clip().
							Padding(10).
							Draw(func(p *ui.Painter, r ui.Rect) {
								PaintCardFrame(p, r, theme.TarotRadiusDailyCard)
							}).
							DrawOver(func(p *ui.Painter, r ui.Rect) {
								if glintProg > 0 {
									PaintGlintSweep(p, r, theme.TarotRadiusDailyCard, glintProg)
									p.AnimationFrameRate(60)
								}
								if edgeFlash > 0.01 {
									p.Stroke(r, tc.GoldBright.Alpha(edgeFlash), theme.TarotRadiusDailyCard, 2)
								}
							}).
							Children(func() {
								ui.Column(c).Fill().AlignItems(ui.Center).
									Padding(20, 24, 22, 24).Gap(theme.Space6).
									Children(func() {
										// Upper art group rotates 180deg when reversed
										artCol := ui.Column(c).FillWidth().Grow(1).MinHeight(0).
											AlignItems(ui.Center).Gap(theme.Space6)
										if dc.Reversed {
											artCol.Rotate(180)
										}
										artCol.Children(func() {
											ui.Text(c, arc.Num).
												Font(theme.TarotRomanFont()).
												FontSize(theme.TarotFontRomanDaily).
												FontWeight(theme.WeightTitle).
												LetterSpacing(2.5).
												TextColor(tc.InkDark).
												SingleLine()

											ui.Box(c).FillWidth().Grow(1).MinHeight(0).
												Draw(func(p *ui.Painter, r ui.Rect) {
													PaintArcanaGlyph(p, arc.Key, r, 1.5, 1.0)
												})
										})

										ui.Text(c, arc.Name()).
											Font(theme.TarotSerifFont()).
											FontSize(theme.FontBody).
											LetterSpacing(8).
											TextColor(tc.InkMuted).
											SingleLine()

										ui.Box(c).Size(84, 10).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
											PaintFleuron(p, r, tc.GoldDark, 1.0)
										})

										ui.Text(c, dc.Note.Title).
											FillWidth().
											Font(theme.TarotSerifFont()).
											FontSize(theme.TarotFontDailyTitle).
											FontWeight(theme.WeightTitle).
											LineHeight(1.35).
											TextColor(tc.InkDark).
											TextAlign(ui.Center).
											MaxLines(2)

										ui.Text(c, dc.Excerpt).
											FillWidth().
											Font(theme.TarotSerifFont()).
											FontSize(theme.FontSmall).
											LineHeight(1.75).
											TextColor(tc.InkSecondary).
											TextAlign(ui.Center).
											MaxLines(3)

										orientText := theme.Tr("正位", "Upright")
										badgeBorder := tc.InkMuted
										badgeFG := tc.InkSecondary
										if dc.Reversed {
											orientText = theme.Tr("逆位", "Reversed")
											badgeBorder = tc.ReversedPaper
											badgeFG = tc.ReversedPaper
										}
										ui.Row(c).Height(24).Margin(theme.Space4, 0, 0, 0).
											PaddingX(theme.Space12).Center().
											Radius(theme.RadiusPill).Border(1, badgeBorder).
											Children(func() {
												ui.Text(c, orientText).
													FontSize(theme.FontMeta).
													LetterSpacing(3.6).
													TextColor(badgeFG).
													SingleLine()
											})
									})
							})
						if isFlipping {
							front.RotateY(flipAngle + 180).Perspective(1600).BackfaceHidden()
						}
						blockui.Elevate(front, theme.ElevationTarotCard)
					}
				})
		})

		// Bottom metadata & Upright/Reversed action buttons
		ui.Column(c).AlignItems(ui.Center).Gap(theme.Space16).MinHeight(96).Children(func() {
			if ds.Flipped {
				actOp := float32(1)
				actDY := float32(0)
				if !reduced && !ds.FlipAt.IsZero() {
					el := now.Sub(ds.FlipAt).Seconds() - 0.20
					if el < 0 {
						actOp = 0
						actDY = 10
					} else if el < 0.70 {
						t := tarotEaseOutCubic(el / 0.70)
						actOp = float32(t)
						actDY = float32(10 * (1 - t))
					}
				}

				ui.Column(c).AlignItems(ui.Center).Gap(theme.Space16).Opacity(actOp).Translate(0, actDY).Children(func() {
					ui.Text(c, dc.LastOpened).
						FontSize(theme.FontSmall).
						TextColor(tc.TextMuted).
						SingleLine()

					ui.Row(c).Gap(theme.Space12).Children(func() {
						readLabel := theme.Tr("读它", "Read It")
						doubtLabel := theme.Tr("质疑它", "Challenge It")
						if dc.Reversed {
							readBtn := tarotGoldSecondaryButton(c, readLabel, 46, "tarot-daily-read", dc.Note.Path == "", func() {
								ctrl.OpenNoteFromRitual(dc.Note.Path, false)
							})
							readBtn.MinWidth(120)

							doubtBtn := tarotGoldPrimaryButton(c, doubtLabel, 46, "tarot-daily-doubt", false, func() {
								ctrl.ToggleDailyDoubt()
							})
							doubtBtn.MinWidth(120)
						} else {
							readBtn := tarotGoldPrimaryButton(c, readLabel, 46, "tarot-daily-read", dc.Note.Path == "", func() {
								ctrl.OpenNoteFromRitual(dc.Note.Path, false)
							})
							readBtn.MinWidth(120)

							doubtBtn := tarotGoldSecondaryButton(c, doubtLabel, 46, "tarot-daily-doubt", false, func() {
								ctrl.ToggleDailyDoubt()
							})
							doubtBtn.MinWidth(120)
						}
					})
				})
			}
		})
	})
}

func (ctrl *Controller) renderDailyDoubtCard(
	c *ui.Context,
	ds DailyState,
	dc DailyCard,
	now time.Time,
	reduced bool,
) {
	tc := theme.Tarot()
	cardOp := float32(1)
	cardDY := float32(0)
	if !reduced && !ds.DoubtAt.IsZero() {
		el := now.Sub(ds.DoubtAt).Seconds()
		if el >= 0 && el < 0.60 {
			t := tarotEaseOutCubic(el / 0.60)
			cardOp = float32(t)
			cardDY = float32(10 * (1 - t))
		}
	}

	ui.Column(c).
		FillWidth().
		MaxWidth(380).
		Padding(28, 28, 30, 28).
		Gap(22).
		Radius(theme.TarotRadiusCard).
		Background(tc.DoubtCardBG).
		Border(1, tc.GoldBorderSoft).
		Opacity(cardOp).
		Translate(0, cardDY).
		DrawOver(func(p *ui.Painter, r ui.Rect) {
			tarotPaintCornerFlourishes(p, r, 5, 26, tc.Gold.Alpha(0.70), 1.2)
		}).
		Children(func() {
			ui.Column(c).FillWidth().Gap(theme.Space10).Children(func() {
				ui.Text(c, theme.Tr("站在反面", "Devil's Advocate")).
					Font(theme.TarotSerifFont()).
					FontSize(theme.FontTitle).
					FontWeight(theme.WeightTitle).
					LetterSpacing(6).
					TextColor(tc.Gold).
					SingleLine()
				tarotRenderFleuronDivider(c, true)
			})

			numerals := [3]string{"I", "II", "III"}
			ui.Column(c).FillWidth().Gap(theme.Space16).Children(func() {
				for i := 0; i < len(dc.DoubtQuestions) && i < 3; i++ {
					qOp := float32(1)
					qDY := float32(0)
					if !reduced && !ds.DoubtAt.IsZero() {
						delay := 0.12 + float64(i)*0.18
						el := now.Sub(ds.DoubtAt).Seconds() - delay
						if el < 0 {
							qOp = 0
							qDY = 8
						} else if el < 0.55 {
							t := tarotEaseOutCubic(el / 0.55)
							qOp = float32(t)
							qDY = float32(8 * (1 - t))
						}
					}
					ui.Row(c).FillWidth().Gap(theme.Space8).AlignItems(ui.Start).Opacity(qOp).Translate(0, qDY).Children(func() {
						ui.Text(c, numerals[i]).
							Width(28).Shrink(0).
							Font(theme.TarotRomanFont()).
							FontSize(theme.TarotFontDailyTitle).
							FontWeight(theme.WeightTitle).
							LineHeight(1.5).
							TextColor(tc.Gold).
							TextAlign(ui.End).
							SingleLine()
						ui.Text(c, dc.DoubtQuestions[i]).
							Grow(1).MinWidth(0).
							Font(theme.TarotSerifFont()).
							FontSize(theme.FontTitle).
							LineHeight(1.85).
							TextColor(tc.TextPrimary)
					})
				}
			})

			ui.Row(c).FillWidth().Wrap().Gap(theme.Space10).Children(func() {
				tarotGoldPrimaryButton(c, theme.Tr("写进这篇笔记", "Write into Note"), 44, "tarot-daily-write-doubt", dc.Note.Path == "", func() {
					ctrl.OpenNoteFromRitual(dc.Note.Path, true)
				})
				tarotGoldSecondaryButton(c, theme.Tr("收起", "Close"), 44, "tarot-daily-close-doubt", false, func() {
					ctrl.ToggleDailyDoubt()
				})
			})
		})
}

func paintDailyDateOrnament(p *ui.Painter, r ui.Rect, left bool, col ui.Color) {
	midY := r.Y + r.H*0.5
	var diamondCX float32
	if left {
		p.Line(r.X, midY, r.X+34, midY, 1, col)
		diamondCX = r.X + 40
	} else {
		diamondCX = r.X + 6
		p.Line(r.X+12, midY, r.X+46, midY, 1, col)
	}
	var path ui.Path
	path.MoveTo(diamondCX, midY-3)
	path.LineTo(diamondCX+3, midY)
	path.LineTo(diamondCX, midY+3)
	path.LineTo(diamondCX-3, midY)
	path.Close()
	p.StrokePath(&path, 1, col)
}
