package tarot

import (
	"fmt"
	"math"
	"path/filepath"

	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// RenderWritingSidebar renders the 260px Tarot Deck sidebar on the Writing room:
// 牌堆 header with "回到牌桌" link, 今日牌 invitation card (face-down until tapped),
// and the scrollable stack of real notebook notes as Tarot cards.
func (ctrl *Controller) RenderWritingSidebar(c *ui.Context, activeNotebook string) {
	tc := theme.Tarot()
	host := ctrl.Host()
	notes := ctrl.Notes()
	daily, dailyDrawn := ctrl.DailySummary()
	dailyArc := daily.Note.Arcana()

	aside := ui.Column(c.Key("tarot-writing-sidebar")).Width(theme.TarotSidebarWidth).FillHeight().Shrink(0).
		Padding(theme.Space20, theme.Space16, theme.Space16, theme.Space16).Gap(theme.Space16).
		Background(tc.ChromeBar).BorderWidth(0, 1, 0, 0).BorderColor(tc.GoldBorderHair)
	blockui.Probe(aside, "tarot-writing-sidebar")

	aside.Children(func() {
		// 1. Header: 牌堆 + 回到牌桌 + fleuron rule.
		ui.Column(c.Key("tarot-sb-head")).FillWidth().Gap(theme.Space8).Shrink(0).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, theme.Tr("牌堆", "Deck")).Font(theme.TarotSerifFont()).
					FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(tc.TextPrimary).SingleLine()
				goTable := ui.ButtonBase(c.Key("tarot-sidebar-table")).Padding(theme.Space2, theme.Space4).
					Radius(theme.RadiusXS).Cursor(ui.CursorPointer).Label(theme.Tr("回到牌桌", "To Table"))
				fg := tc.Gold
				if goTable.Hovered() {
					fg = tc.GoldBright
				}
				goTable.Children(func() {
					ui.Text(c, theme.Tr("回到牌桌", "To Table")).FontSize(theme.FontSmall).TextColor(fg).SingleLine()
				})
				blockui.Probe(goTable, "tarot-sidebar-table")
				if goTable.Clicked() {
					ctrl.SetRoom(RoomTable)
				}
			})
			ui.Box(c).FillWidth().Height(theme.Space10).IgnoreInput(true).Draw(func(p *ui.Painter, r ui.Rect) {
				midY := r.Y + r.H*0.5
				flW := float32(24)
				p.Line(r.X, midY, r.X+r.W*0.5-flW*0.5-4, midY, 1, tc.GoldBorderMid)
				p.Line(r.X+r.W*0.5+flW*0.5+4, midY, r.X+r.W, midY, 1, tc.GoldBorderMid)
				PaintFleuron(p, ui.Rect{X: r.X + r.W*0.5 - flW*0.5, Y: r.Y, W: flW, H: r.H}, tc.Gold, 1)
			})
		})

		// 2. 今日牌 invitation card (does not auto-pop; gently breathes while face-down).
		todaySub := theme.Tr("未翻开", "Unrevealed")
		if dailyDrawn {
			orient := theme.Tr("正位", "Upright")
			if daily.Reversed {
				orient = theme.Tr("逆位", "Reversed")
			}
			todaySub = fmt.Sprintf("%s %s · %s", dailyArc.Num, dailyArc.Name(), orient)
		}
		todayBtn := ui.ButtonBase(c.Key("tarot-sidebar-daily")).FillWidth().
			Padding(theme.Space10, theme.Space12, theme.Space10, theme.Space12).Gap(theme.Space12).
			Shrink(0).AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
			Label(theme.Tr("今日牌", "Daily Card"))
		if todayBtn.Hovered() {
			todayBtn.Background(tc.TodayCardHoverBG).Border(1, tc.GoldBorderHover)
		} else {
			todayBtn.Background(tc.TodayCardBG).Border(1, tc.GoldBorderSoft)
		}
		blockui.TactileCard(todayBtn, todayBtn.Hovered(), todayBtn.Pressed(), false, false)
		todayBtn.Children(func() {
			// Mini card thumbnail (52x89).
			thumb := ui.Box(c.Key("tarot-daily-mini")).Size(52, 89).Shrink(0)
			if !dailyDrawn {
				thumb.Draw(func(p *ui.Painter, r ui.Rect) {
					alpha := float32(0.45)
					if !p.Preferences().ReduceMotion {
						sec := float64(p.Now().UnixMilli()%3200) / 3200.0
						alpha = float32(0.20 + 0.55*(0.5-0.5*math.Cos(2*math.Pi*sec)))
						p.AnimationFrameRate(30)
					}
					outer := ui.Rect{X: r.X - 4, Y: r.Y - 4, W: r.W + 8, H: r.H + 8}
					p.Stroke(outer, tc.Gold.Alpha(alpha), theme.RadiusMD, 1)
					PaintCardBack(p, r, theme.RadiusSM, 0.75)
				})
			} else {
				thumb.Draw(func(p *ui.Painter, r ui.Rect) {
					PaintCardFrame(p, r, theme.RadiusSM)
				})
				thumb.Children(func() {
					ui.Column(c).Fill().Padding(theme.Space6, theme.Space4, theme.Space6, theme.Space4).
						AlignItems(ui.Center).Gap(theme.Space2).IgnoreInput(true).Children(func() {
						ui.Text(c, dailyArc.Num).Font(theme.TarotRomanFont()).FontSize(theme.FontMicro).
							FontWeight(theme.WeightTitle).TextColor(tc.InkDark).SingleLine()
						ui.Box(c).FillWidth().Grow(1).MinHeight(0).Draw(func(p *ui.Painter, r ui.Rect) {
							PaintArcanaGlyph(p, dailyArc.Key, r, 0.65, 1)
						})
					})
				})
			}

			ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space4).Children(func() {
				ui.Text(c, theme.Tr("今日牌", "Daily Card")).Font(theme.TarotSerifFont()).
					FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(tc.GoldBright).SingleLine()
				ui.Text(c, todaySub).FontSize(theme.FontSmall).TextColor(tc.TextMuted).
					SingleLine().Ellipsis("…")
			})
		})
		blockui.Probe(todayBtn, "tarot-sidebar-daily")
		if todayBtn.Clicked() {
			ctrl.SetRoom(RoomDaily)
		}

		// 3. Scrollable Note Deck stack.
		scroll := blockui.Scroll(c.Key("tarot-sidebar-notes"), &ctrl.SidebarScroll).
			FillWidth().Grow(1).MinHeight(0)
		scroll.Children(func() {
			ui.Column(c).FillWidth().Gap(theme.Space6).Padding(theme.Space2, theme.Space10, theme.Space8, 0).Children(func() {
				if len(notes) == 0 {
					ui.Text(c, theme.Tr("暂无笔记，点击下方新建一篇", "No notes yet")).
						FontSize(theme.FontSmall).TextColor(tc.TextMuted).Padding(theme.Space8, theme.Space4)
					return
				}
				for idx, item := range notes {
					n := item
					rowID := fmt.Sprintf("tarot-note-row-%d", idx)
					row := ui.ButtonBase(c.Key(rowID)).FillWidth().Height(46).
						PaddingX(theme.Space12).Gap(theme.Space10).AlignItems(ui.Center).
						Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(n.Title)
					hovered := row.Hovered()
					if n.IsActive {
						row.LinearGradient(tc.ActiveRowGrad).Border(1, tc.GoldDark).Translate(8, 0)
						row.DrawOver(func(p *ui.Painter, r ui.Rect) {
							blockui.RecordBounds(rowID, r)
							inner := ui.Rect{X: r.X + 3, Y: r.Y + 3, W: r.W - 6, H: r.H - 6}
							p.Stroke(inner, tc.ParchmentFrame, theme.RadiusSM, 1)
						})
						blockui.Elevate(row, theme.ElevationTarotCard)
					} else {
						bd := tc.GoldBorderSoft
						tx := float32(0)
						if hovered {
							bd = tc.GoldBorderHover
							tx = 4
						}
						row.Background(tc.CardBackMid).Border(1, bd)
						if tx != 0 {
							row.Translate(tx, 0)
						}
						row.DrawOver(func(p *ui.Painter, r ui.Rect) {
							blockui.RecordBounds(rowID, r)
							// Subtle stacked-card thickness edge at the bottom.
							p.Line(r.X+4, r.Y+r.H-1.5, r.X+r.W-4, r.Y+r.H-1.5, 1, tc.GoldBorderSub)
						})
					}

					titleCol := tc.TextPrimary
					if n.IsActive {
						titleCol = tc.InkDark
					}
					row.Children(func() {
						ui.Text(c, n.Title).Font(theme.TarotSerifFont()).FontSize(theme.FontBody).
							TextColor(titleCol).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					})
					if row.Clicked() && !n.IsActive && host.OpenNote != nil {
						ctrl.TriggerNoteSwitch()
						host.OpenNote(n.Path)
					}
				}
			})
		})

		// 4. Quiet utility footer so core Block workflows remain one click away.
		nbName := theme.Tr("笔记本", "Notebook")
		if activeNotebook != "" {
			nbName = filepath.Base(activeNotebook)
		}
		ui.Row(c.Key("tarot-sb-footer")).FillWidth().Height(theme.HeightControl).Shrink(0).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space6).
			BorderWidth(1, 0, 0, 0).BorderColor(tc.GoldBorderSub).Padding(theme.Space6, 0, 0, 0).
			Children(func() {
				nbBtn := ui.ButtonBase(c.Key("tarot-notebook-switch")).Height(theme.HeightChrome).
					Grow(1).MinWidth(0).PaddingX(theme.Space6).AlignItems(ui.Center).
					Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(theme.Tr("切换笔记本", "Switch Notebook"))
				nbFG := tc.TextMuted
				if nbBtn.Hovered() || ctrl.NotebookMenuOpen {
					nbBtn.Background(tc.GoldHoverWash)
					nbFG = tc.GoldBright
				}
				nbBtn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					ctrl.NotebookMenuAnchor = r
				})
				nbBtn.Children(func() {
					ui.Text(c, nbName).FontSize(theme.FontMeta).TextColor(nbFG).
						Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
				blockui.Probe(nbBtn, "tarot-notebook-switch")
				if activeNotebook != "" {
					blockui.Tooltip(c, nbBtn, activeNotebook)
				}
				if nbBtn.Clicked() {
					ctrl.NotebookMenuOpen = !ctrl.NotebookMenuOpen
					ctrl.NotebookMenuCursor = -1
				}
				footerBtn := func(id, label string, fn func()) {
					b := ui.ButtonBase(c.Key(id)).Height(theme.HeightChrome).PaddingX(theme.Space8).
						Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(label)
					fg := tc.TextMuted
					if b.Hovered() {
						b.Background(tc.GoldHoverWash)
						fg = tc.GoldBright
					}
					b.Border(1, tc.GoldBorderHair)
					b.Children(func() {
						ui.Text(c, label).FontSize(theme.FontMeta).TextColor(fg).SingleLine()
					})
					blockui.Probe(b, id)
					if b.Clicked() && fn != nil {
						fn()
					}
				}
				footerBtn("tarot-new-note", theme.Tr("+ 新建", "+ New"), host.NewNote)
				footerBtn("tarot-search", theme.Tr("搜索", "Find"), host.OpenSearch)
			})
	})
	if host.DeferMenus != nil {
		host.DeferMenus(func() { ctrl.renderNotebookMenu(c, activeNotebook) })
	} else {
		ctrl.renderNotebookMenu(c, activeNotebook)
	}
}

func (ctrl *Controller) renderNotebookMenu(c *ui.Context, activeNotebook string) {
	if !ctrl.NotebookMenuOpen {
		return
	}
	host := ctrl.Host()
	vpW, vpH := c.Size()
	ui.Overlay(c, func() {
		closeMenu := func() {
			ctrl.NotebookMenuOpen = false
		}
		var notebooks []string
		if host.Notebooks != nil {
			notebooks = host.Notebooks()
		}
		items := make([]blockui.MenuItemSpec, 0, len(notebooks)+2)
		for _, nb := range notebooks {
			root := nb
			items = append(items, blockui.MenuItemSpec{
				ID:      "tarot-nb:" + root,
				Label:   filepath.Base(root),
				Checked: root == activeNotebook,
				OnClick: func() {
					closeMenu()
					if root != activeNotebook && host.SwitchNotebook != nil {
						host.SwitchNotebook(root)
					}
				},
			})
		}
		if len(items) > 0 {
			items = append(items, blockui.MenuItemSpec{Separator: true})
		}
		folderIcon := icons.FolderPlus
		items = append(items, blockui.MenuItemSpec{
			ID:    "tarot-nb-choose",
			Label: theme.Tr("选择文件夹…", "Choose Folder…"),
			Icon:  &folderIcon,
			OnClick: func() {
				closeMenu()
				if host.ChooseNotebook != nil {
					host.ChooseNotebook()
				}
			},
		})

		menuW := min(float32(theme.TarotSidebarWidth-theme.Space16*2), max(vpW-16, 1))
		estH := theme.MenuPadding*2 + float32(len(items))*(theme.MenuItemHeight+theme.MenuGap)
		anchor := ctrl.NotebookMenuAnchor
		if anchor.W <= 0 {
			anchor = ui.Rect{X: theme.Space16, Y: vpH - 42, W: menuW, H: 30}
		}
		spec := blockui.PlaceAbove(anchor)
		maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 304)
		ox, oy := blockui.ResolveOrigin(spec, menuW, min(estH, maxH), vpW, vpH)

		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		card := blockui.MenuSurface(ui.Column(c.Key("tarot-notebook-menu")).Absolute().
			Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
		blockui.NavigateMenu(c, card, items, &ctrl.NotebookMenuCursor)
		blockui.Probe(card, "tarot-notebook-menu")
		card.Children(func() {
			blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
				ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
					for i, it := range items {
						blockui.MenuRow(c, it, i == ctrl.NotebookMenuCursor)
					}
				})
			})
		})
		if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
			closeMenu()
		}
	})
}
