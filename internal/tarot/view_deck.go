package tarot

import (
	"fmt"
	"time"

	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// RenderDeckRoom renders the 22 Major Arcana collection gallery:
// scrollable crosshatch gallery with 22-segment gold progress bar, 3-way filter,
// adaptive grid on the left, and a 380px card inspection sidebar on the right.
func (ctrl *Controller) RenderDeckRoom(c *ui.Context) {
	now := time.Now()

	ctrl.mu.RLock()
	deck := ctrl.Deck
	ctrl.mu.RUnlock()

	snap := ctrl.store.Snapshot()
	tc := theme.Tarot()
	reduced := c.Preferences().ReduceMotion

	if !reduced && !deck.TurnAt.IsZero() && now.Sub(deck.TurnAt) < 700*time.Millisecond {
		c.AnimationFrame()
	}

	collectedCount := 0
	for id := 0; id < len(MajorArcana); id++ {
		if _, ok := snap.Collected[id]; ok {
			collectedCount++
		}
	}

	filter := deck.Filter
	if filter != "got" && filter != "missing" {
		filter = "all"
	}

	ui.Row(c).Fill().Background(tc.Ground).Children(func() {
		ctrl.renderDeckGallery(c, deck, snap, collectedCount, filter)
		ctrl.renderDeckDetailAside(c, deck, snap, now, reduced)
	})
}

func (ctrl *Controller) renderDeckGallery(
	c *ui.Context,
	deck DeckState,
	snap StoreData,
	collectedCount int,
	filter string,
) {
	tc := theme.Tarot()
	winW, _ := c.Size()
	availW := winW - theme.TarotDeckAsideWidth - 112
	cols := 6
	if availW > 0 {
		cols = max(int((availW+20)/(104+20)), 2)
	}

	ui.Box(c).Grow(1).FillHeight().MinWidth(0).
		Draw(func(p *ui.Painter, r ui.Rect) {
			PaintCrosshatchCloth(p, r)
		}).
		Children(func() {
			blockui.Scroll(c, &ctrl.Deck.GridScroll).Fill().Children(func() {
				ui.Column(c).FillWidth().Padding(36, 56, 48, 56).Gap(28).Children(func() {
					// Top progress + filter bar
					ui.Row(c).FillWidth().Wrap().AlignItems(ui.End).Justify(ui.SpaceBetween).Gap(theme.Space20).Children(func() {
						ui.Column(c).Grow(1).MinWidth(300).MaxWidth(520).Gap(theme.Space12).Children(func() {
							ui.Row(c).AlignItems(ui.Baseline).Gap(theme.Space16).Children(func() {
								ui.Text(c, theme.Tr("牌册", "The Deck")).
									Font(theme.TarotSerifFont()).
									FontSize(theme.TarotFontDeckHeading).
									FontWeight(theme.TarotWeightBold).
									LetterSpacing(8).
									TextColor(tc.TextPrimary).
									SingleLine()

								ui.Row(c).AlignItems(ui.Baseline).Gap(theme.Space4).Children(func() {
									ui.Text(c, theme.Tr("已收集 ", "Collected ")).
										FontSize(theme.FontBody).
										TextColor(tc.TextMuted).
										SingleLine()
									ui.Text(c, fmt.Sprintf("%d", collectedCount)).
										Font(theme.TarotRomanFont()).
										FontSize(theme.TarotFontRomanCard).
										FontWeight(theme.WeightTitle).
										TextColor(tc.GoldBright).
										SingleLine()
									ui.Text(c, " / 22").
										FontSize(theme.FontBody).
										TextColor(tc.TextMuted).
										SingleLine()
								})
							})

							// 22-segment gold progress bar
							ui.Row(c).FillWidth().Gap(3).Children(func() {
								for id := 0; id < len(MajorArcana); id++ {
									_, got := snap.Collected[id]
									tick := ui.Box(c).Grow(1).Height(4).Radius(theme.RadiusXS)
									if got {
										tick.Draw(func(p *ui.Painter, r ui.Rect) {
											p.FillGradient(r, tc.TickGotGrad, theme.RadiusXS)
										})
									} else {
										tick.Background(tc.GoldTickMissing)
									}
								}
							})
						})

						// Segmented 3-way filter
						ui.Row(c).Gap(theme.Space2).Padding(3).Radius(theme.RadiusMD).
							Background(tc.ChromeBar).
							Border(1, tc.GoldBorderHair).
							InsetShadow(0, 1, 0, 0, tc.CardShadowDark).
							Children(func() {
								opts := [3]struct {
									id, label, probe string
								}{
									{"all", theme.Tr("全部", "All"), "tarot-deck-filter-all"},
									{"got", theme.Tr("已收集", "Collected"), "tarot-deck-filter-got"},
									{"missing", theme.Tr("未收集", "Missing"), "tarot-deck-filter-missing"},
								}
								for _, opt := range opts {
									active := filter == opt.id
									seg := ui.ButtonBase(c).Height(theme.HeightControl).PaddingX(theme.Space16).
										Radius(theme.RadiusSM).Center().Cursor(ui.CursorPointer).Label(opt.label)
									if active {
										seg.Background(tc.FilterActiveBG).Border(1, tc.GoldBorderMid)
									}
									fg := tc.TextMuted
									if active || seg.Hovered() {
										fg = tc.TextPrimary
									}
									if seg.Pressed() {
										seg.Translate(0, 1)
									}
									seg.Children(func() {
										ui.Text(c, opt.label).
											FontSize(theme.FontSmall).
											TextColor(fg).
											SingleLine()
									})
									id := opt.id
									seg.OnClick(func() {
										ctrl.SetDeckFilter(id)
									})
									blockui.Probe(seg, opt.probe)
								}
							})
					})

					// Visible cards filtered by mode
					var visible []Arcana
					for _, a := range MajorArcana {
						_, got := snap.Collected[a.ID]
						if filter == "got" && !got {
							continue
						}
						if filter == "missing" && got {
							continue
						}
						visible = append(visible, a)
					}

					if len(visible) == 0 {
						msg := theme.Tr("还没有收集到牌，去牌桌抽牌或保存一篇笔记吧。", "No cards collected yet. Draw at the Table or save a note.")
						if filter == "missing" {
							msg = theme.Tr("已集齐全部 22 张大阿卡纳。", "All 22 Major Arcana collected.")
						}
						ui.Box(c).FillWidth().Padding(48, 0, 48, 0).Center().Children(func() {
							ui.Text(c, msg).
								Font(theme.TarotSerifFont()).
								FontSize(theme.FontBody).
								TextColor(tc.TextMuted).
								TextAlign(ui.Center)
						})
						return
					}

					ui.Grid(c).FillWidth().Columns(cols).GapX(theme.Space20).GapY(26).Children(func() {
						for _, a := range visible {
							arc := a
							entry, got := snap.Collected[arc.ID]
							selected := deck.SelectedID == arc.ID
							ctrl.renderDeckGridCard(c, arc, entry, got, selected)
						}
					})
				})
			})
		})
}

func (ctrl *Controller) renderDeckGridCard(
	c *ui.Context,
	arc Arcana,
	entry CollectedEntry,
	got bool,
	selected bool,
) {
	tc := theme.Tarot()
	aria := arc.Num + " " + arc.Name()
	if got && entry.NoteTitle != "" {
		aria += "，" + entry.NoteTitle
	}
	btn := ui.ButtonBase(c).Column().FillWidth().MinWidth(0).AlignItems(ui.Center).
		Gap(theme.Space10).Cursor(ui.CursorPointer).Label(aria)
	blockui.Probe(btn, fmt.Sprintf("tarot-deck-card-%d", arc.ID))

	selTarget := float32(0)
	if selected {
		selTarget = 1
	}
	selP := btn.AnimateTo("deckSel", selTarget, blockui.SpringPanel)
	liftY := -6 * selP
	if !selected && btn.Hovered() {
		liftY -= 2
	}

	btn.OnClick(func() {
		ctrl.SelectDeckCard(arc.ID)
	}).Children(func() {
		thumb := ui.Box(c).
			FillWidth().
			AspectRatio(100.0/172.0).
			Radius(theme.RadiusMD).
			Clip().
			Translate(0, liftY)

		switch {
		case selected:
			thumb.Border(2, tc.GoldBright)
			blockui.Elevate(thumb, theme.ElevationTarotCard)
		case got:
			thumb.Border(1, tc.GoldBorderMid)
			blockui.Elevate(thumb, theme.ElevationCard)
		default:
			thumb.Border(1, tc.GoldBorderHair)
		}

		if got {
			thumb.Padding(5).
				Draw(func(p *ui.Painter, r ui.Rect) {
					PaintCardFrame(p, r, theme.RadiusMD)
				}).
				Children(func() {
					ui.Column(c).Fill().AlignItems(ui.Center).
						Padding(7, 4, 8, 4).Gap(theme.Space2).
						Children(func() {
							ui.Text(c, arc.Num).
								Font(theme.TarotRomanFont()).
								FontSize(theme.FontBody).
								FontWeight(theme.WeightTitle).
								TextColor(tc.InkDark).
								SingleLine()

							ui.Box(c).FillWidth().Grow(1).MinHeight(0).
								Draw(func(p *ui.Painter, r ui.Rect) {
									PaintArcanaGlyph(p, arc.Key, r, 1.0, 1.0)
								})

							ui.Text(c, arc.Name()).
								Font(theme.TarotSerifFont()).
								FontSize(theme.FontMicro).
								LetterSpacing(2).
								TextColor(tc.InkSecondary).
								SingleLine()
						})
				})
		} else {
			thumb.Draw(func(p *ui.Painter, r ui.Rect) {
				PaintCardBack(p, r, theme.RadiusMD, 0.85)
				p.Fill(r, tc.PanelDark.Alpha(0.45), theme.RadiusMD)
			})
		}

		caption := arc.Num + " · " + arc.Name()
		capColor := tc.TextDim
		capFont := ""
		if got && entry.NoteTitle != "" {
			caption = entry.NoteTitle
			capColor = tc.TextPrimary
			capFont = theme.TarotSerifFont()
		}
		capEl := ui.Text(c, caption).
			FillWidth().
			FontSize(theme.FontMeta).
			LineHeight(1.4).
			TextColor(capColor).
			TextAlign(ui.Center).
			SingleLine()
		if capFont != "" {
			capEl.Font(capFont)
		}
	})
}

func (ctrl *Controller) renderDeckDetailAside(
	c *ui.Context,
	deck DeckState,
	snap StoreData,
	now time.Time,
	reduced bool,
) {
	tc := theme.Tarot()
	arc := ArcanaByID(deck.SelectedID)
	entry, got := snap.Collected[arc.ID]

	turnDeg := float32(0)
	cardDY := float32(0)
	cardOp := float32(1)
	textOp := float32(1)
	textDY := float32(0)

	if !reduced && !deck.TurnAt.IsZero() {
		el := now.Sub(deck.TurnAt).Seconds()
		if el >= 0 && el < 0.62 {
			t := tarotAnimClamp01(el / 0.62)
			eased := tarotCubicBezier(0.3, 1.2, 0.45, 1.0, t)
			turnDeg = float32((1.0 - eased) * 90)
			cardDY = float32((1.0 - eased) * 8)
			cardOp = float32(0.2 + 0.8*tarotAnimClamp01(el/0.35))
		}
		if el >= 0 && el < 0.68 {
			rel := el - 0.16
			if rel < 0 {
				textOp = 0
				textDY = 8
			} else if rel < 0.50 {
				t := tarotEaseOutCubic(rel / 0.50)
				textOp = float32(t)
				textDY = float32(8 * (1 - t))
			}
		}
	}

	ui.Column(c).
		Width(theme.TarotDeckAsideWidth).
		FillHeight().
		Shrink(0).
		Background(tc.PanelDark).
		BorderWidth(0, 0, 0, 1).
		BorderColor(tc.GoldBorderHair).
		Padding(40, 36, 40, 36).
		Gap(26).
		AlignItems(ui.Center).
		Children(func() {
			// 220x378 large card preview with page-turn entrance
			cardBox := ui.Box(c).
				Size(220, 378).
				Shrink(0).
				Radius(theme.TarotRadiusDeckDetail).
				Clip().
				Translate(0, cardDY).
				RotateY(turnDeg).
				Perspective(1400).
				Opacity(cardOp)
			blockui.Elevate(cardBox, theme.ElevationTarotCard)

			if got {
				cardBox.Padding(9).
					Draw(func(p *ui.Painter, r ui.Rect) {
						PaintCardFrame(p, r, theme.TarotRadiusDeckDetail)
					}).
					Children(func() {
						ui.Column(c).Fill().AlignItems(ui.Center).
							Padding(14, 14, 16, 14).Gap(5).
							Children(func() {
								ui.Text(c, arc.Num).
									Font(theme.TarotRomanFont()).
									FontSize(theme.TarotFontRomanDetail).
									FontWeight(theme.WeightTitle).
									LetterSpacing(1.6).
									TextColor(tc.InkDark).
									SingleLine()

								ui.Box(c).FillWidth().Grow(1).MinHeight(0).
									Draw(func(p *ui.Painter, r ui.Rect) {
										PaintArcanaGlyph(p, arc.Key, r, 1.35, 1.0)
									})

								ui.Text(c, arc.Name()).
									Font(theme.TarotSerifFont()).
									FontSize(theme.FontSmall).
									LetterSpacing(6).
									TextColor(tc.InkMuted).
									SingleLine()

								ui.Box(c).Size(59, 7).Margin(3, 0, 1, 0).Shrink(0).
									Draw(func(p *ui.Painter, r ui.Rect) {
										PaintFleuron(p, r, tc.GoldDark, 1.3)
									})

								ui.Text(c, entry.NoteTitle).
									FillWidth().
									Font(theme.TarotSerifFont()).
									FontSize(theme.TarotFontRomanSmall).
									FontWeight(theme.WeightTitle).
									LineHeight(1.4).
									TextColor(tc.InkDark).
									TextAlign(ui.Center).
									MaxLines(2)
							})
					})
			} else {
				cardBox.Border(1, tc.GoldBorderMid).
					Draw(func(p *ui.Painter, r ui.Rect) {
						PaintCardBack(p, r, theme.TarotRadiusDeckDetail, 1.2)
					})
			}

			// Detail metadata & CTA
			ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space16).
				Opacity(textOp).Translate(0, textDY).
				Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
						ui.Row(c).AlignItems(ui.Baseline).Justify(ui.Center).Gap(theme.Space10).Children(func() {
							ui.Text(c, arc.Num).
								Font(theme.TarotRomanFont()).
								FontSize(theme.FontPage).
								TextColor(tc.GoldBright).
								SingleLine()
							ui.Text(c, arc.Name()).
								Font(theme.TarotSerifFont()).
								FontSize(theme.TarotFontRomanCard).
								FontWeight(theme.WeightTitle).
								LetterSpacing(4).
								TextColor(tc.TextPrimary).
								SingleLine()
						})
						tarotRenderFleuronDivider(c, false)
					})

					if got {
						ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
							ui.Text(c, entry.NoteTitle).
								FillWidth().
								Font(theme.TarotSerifFont()).
								FontSize(theme.TarotFontRomanSmall).
								TextColor(tc.TextPrimary).
								TextAlign(ui.Center).
								MaxLines(2)

							whenColor := tc.TextMuted
							if entry.Reversed {
								whenColor = tc.ReversedDark
							}
							ui.Text(c, tarotFormatDeckWhen(entry.DrawnUnix, entry.Reversed, now)).
								FontSize(theme.FontSmall).
								TextColor(whenColor).
								SingleLine()
						})

						openBtn := tarotGoldPrimaryButton(c, theme.Tr("打开这篇笔记", "Open This Note"), 46, "tarot-deck-open", entry.NotePath == "", func() {
							ctrl.OpenNoteFromRitual(entry.NotePath, false)
						})
						openBtn.FillWidth()
					} else {
						ui.Text(c, theme.Tr("还没有笔记抽到这张牌", "No note has drawn this card yet")).
							FillWidth().
							FontSize(theme.FontBody).
							TextColor(tc.TextMuted).
							TextAlign(ui.Center)

						goBtn := tarotGoldSecondaryButton(c, theme.Tr("去牌桌抽牌", "Draw at the Table"), 46, "tarot-deck-go-table", false, func() {
							ctrl.SetRoom(RoomTable)
						})
						goBtn.FillWidth()
					}
				})
		})
}
