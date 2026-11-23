package tarot

import (
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// RenderTopbar renders the unified 44px Midnight Copperplate navigation header
// across Writing, Table, Daily, and Deck rooms.
func (ctrl *Controller) RenderTopbar(c *ui.Context, sidebarOpen bool) {
	tc := theme.Tarot()
	room := ctrl.Room()
	host := ctrl.Host()
	active, hasActive := ctrl.ActiveNote()

	tb := c.TitleBar()
	leftLead := theme.Space16
	if blockui.MacTrafficLightClear() {
		leftLead = max(float32(82), tb.Left+theme.Space12)
	}

	bar := ui.Row(c.Key("tarot-topbar")).FillWidth().Height(theme.TarotTopbarHeight).Shrink(0).
		AlignItems(ui.Center).Padding(0, theme.Space16, 0, leftLead).Gap(theme.Space12).
		Background(tc.ChromeBar)
	bar.DrawOver(func(p *ui.Painter, r ui.Rect) {
		blockui.RecordBounds("tarot-topbar", r)
		p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, tc.GoldBorderHair)
		p.Line(r.X, r.Y+r.H-3.5, r.X+r.W, r.Y+r.H-3.5, 1, tc.GoldBorderSub)
	})

	bar.Children(func() {
		// Left section: current Arcana + title on Writing, or "← 回到写作 [Esc]" on ritual rooms.
		left := ui.Row(c.Key("tarot-topbar-left")).Width(280).FillHeight().Shrink(0).
			AlignItems(ui.Center).Gap(theme.Space10).DragWindow()
		left.Children(func() {
			if room == RoomWriting {
				// Subtle sidebar toggle on Writing so user retains collapse/expand control.
				sbBtn := ui.ButtonBase(c.Key("tarot-sidebar-toggle")).Size(theme.HeightCompact, theme.HeightCompact).
					Shrink(0).Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
					Label(theme.Tr("切换牌堆侧栏", "Toggle Deck Sidebar"))
				if sbBtn.Pressed() {
					sbBtn.Background(tc.GoldPressWash).Border(1, tc.GoldBright)
				} else if sbBtn.Hovered() {
					sbBtn.Background(tc.GoldHoverWash).Border(1, tc.GoldBorderMid)
				} else if sidebarOpen {
					sbBtn.Border(1, tc.GoldBorderSoft)
				} else {
					sbBtn.Border(1, tc.GoldBorderHair)
				}
				sbBtn.Children(func() {
					IconBox(c, IconStar4, theme.IconInline, tc.Gold)
				})
				blockui.Probe(sbBtn, "tarot-sidebar-toggle")
				if sbBtn.Clicked() && host.ToggleSidebar != nil {
					host.ToggleSidebar()
				}

				if hasActive {
					arc := active.Arcana()
					ui.Text(c, arc.Num).Font(theme.TarotRomanFont()).FontSize(theme.TarotFontRomanSmall).
						FontWeight(theme.TarotWeightBold).TextColor(tc.Gold).SingleLine().Shrink(0)
					ui.Text(c, arc.Name()).Font(theme.TarotSerifFont()).FontSize(theme.FontSmall).
						TextColor(tc.TextMuted).SingleLine().Shrink(0)
					ui.Box(c).Size(1, theme.IconInline).Shrink(0).Background(tc.GoldBorderMid)
					ui.Text(c, active.Title).Font(theme.TarotSerifFont()).FontSize(theme.FontBody).
						FontWeight(theme.WeightTitle).TextColor(tc.TextPrimary).
						Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				} else {
					ui.Text(c, theme.Tr("塔罗模式", "Tarot Mode")).Font(theme.TarotSerifFont()).
						FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(tc.TextPrimary).SingleLine()
				}
			} else {
				backBtn := ui.ButtonBase(c.Key("tarot-back-writing")).Height(30).
					Padding(0, theme.Space6, 0, theme.Space8).Gap(theme.Space8).Shrink(0).
					AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
					Label(theme.Tr("回到写作", "Back to Writing"))
				fg := tc.TextWarm
				bd := tc.GoldBorderMid
				if backBtn.Pressed() {
					backBtn.Background(tc.GoldPressWash)
					fg = tc.TextPrimary
					bd = tc.GoldBright
				} else if backBtn.Hovered() {
					backBtn.Background(tc.GoldHoverWash)
					fg = tc.TextPrimary
					bd = tc.GoldBright
				}
				backBtn.Border(1, bd)
				blockui.TactileButton(backBtn, backBtn.Hovered(), backBtn.Pressed(), false)
				backBtn.Children(func() {
					IconBox(c, IconChevronLeft, theme.IconInline, tc.Gold)
					ui.Text(c, theme.Tr("回到写作", "Back to Writing")).FontSize(theme.FontSmall).
						TextColor(fg).SingleLine()
					ui.Row(c).Height(theme.KeycapHeight).PaddingX(theme.KeycapPadX).Center().
						Radius(theme.RadiusXS).Border(1, tc.GoldBorderMid).Children(func() {
						ui.Text(c, "Esc").FontSize(theme.FontMicro).TextColor(tc.TextMuted).SingleLine()
					})
				})
				blockui.Probe(backBtn, "tarot-back-writing")
				if backBtn.Clicked() {
					ctrl.BackToWriting()
				}
			}
		})

		// Center navigation: 写作 / 牌桌 / 牌册 (none underlined on Daily room).
		nav := ui.Row(c.Key("tarot-topbar-nav")).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).
			Justify(ui.Center).Gap(theme.Space4).DragWindow()
		nav.Children(func() {
			type navDef struct {
				room  Room
				id    string
				label string
			}
			tabs := []navDef{
				{RoomWriting, "tarot-nav-writing", theme.Tr("写作", "Writing")},
				{RoomTable, "tarot-nav-table", theme.Tr("牌桌", "Table")},
				{RoomDeck, "tarot-nav-deck", theme.Tr("牌册", "Deck")},
			}
			for _, t := range tabs {
				tab := t
				activeTab := room == tab.room
				btn := ui.ButtonBase(c.Key(tab.id)).FillHeight().PaddingX(theme.Space16).Center().
					Cursor(ui.CursorPointer).Label(tab.label)
				fg := tc.TextMuted
				if activeTab {
					fg = tc.TextPrimary
				} else if btn.Hovered() {
					fg = tc.TextWarm
				}
				btn.DrawOver(func(p *ui.Painter, r ui.Rect) {
					blockui.RecordBounds(tab.id, r)
					if activeTab {
						p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 2, W: r.W, H: 2}, tc.Gold, 0)
					}
				})
				btn.Children(func() {
					ui.Text(c, tab.label).Font(theme.TarotSerifFont()).FontSize(theme.FontBody).
						FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
				})
				if btn.Clicked() {
					ctrl.SetRoom(tab.room)
				}
			}
		})

		// Right section: AI (on Writing) + 退出塔罗模式.
		right := ui.Row(c.Key("tarot-topbar-right")).Width(280).FillHeight().Shrink(0).
			AlignItems(ui.Center).Justify(ui.End).Gap(theme.Space8).DragWindow()
		right.Children(func() {
			if room == RoomWriting {
				if host.IsAIEnabled != nil && host.IsAIEnabled() {
					aiOpen := host.IsAIOpen != nil && host.IsAIOpen()
					aiBtn := ui.ButtonBase(c.Key("tarot-ai")).Height(30).PaddingX(theme.Space12).
						Gap(theme.Space6).Shrink(0).AlignItems(ui.Center).Radius(theme.RadiusSM).
						Cursor(ui.CursorPointer).Label("AI")
					aiBD := tc.GoldBorderMid
					aiFG := tc.TextWarm
					if aiOpen {
						aiBtn.Background(tc.GoldPressWash)
						aiBD = tc.GoldBorderHover
						aiFG = tc.TextPrimary
					} else if aiBtn.Hovered() {
						aiBtn.Background(tc.GoldHoverWash)
						aiBD = tc.GoldBright
						aiFG = tc.TextPrimary
					}
					aiBtn.Border(1, aiBD)
					blockui.TactileButton(aiBtn, aiBtn.Hovered(), aiBtn.Pressed(), false)
					aiBtn.Children(func() {
						IconBox(c, IconStar4, theme.IconInline, tc.Gold)
						ui.Text(c, "AI").FontSize(theme.FontSmall).TextColor(aiFG).SingleLine()
					})
					blockui.Probe(aiBtn, "tarot-ai")
					if aiBtn.Clicked() && host.ToggleAI != nil {
						host.ToggleAI()
					}
				}
			}

			exitLbl := theme.Tr("退出塔罗模式", "Exit Tarot")
			exitBtn := ui.ButtonBase(c.Key("tarot-exit")).Height(30).PaddingX(theme.Space12).
				Gap(theme.Space8).Shrink(0).AlignItems(ui.Center).Radius(theme.RadiusSM).
				Cursor(ui.CursorPointer).Label(exitLbl)
			exitFG := tc.TextWarm
			exitBD := tc.GoldBorderMid
			if exitBtn.Pressed() {
				exitBtn.Background(tc.GoldPressWash)
				exitFG = tc.TextPrimary
				exitBD = tc.GoldBright
			} else if exitBtn.Hovered() {
				exitBtn.Background(tc.GoldHoverWash)
				exitFG = tc.TextPrimary
				exitBD = tc.GoldBright
			}
			exitBtn.Border(1, exitBD)
			blockui.TactileButton(exitBtn, exitBtn.Hovered(), exitBtn.Pressed(), false)
			exitBtn.Children(func() {
				IconBox(c, IconMoon, theme.IconInline, tc.Gold)
				ui.Text(c, exitLbl).FontSize(theme.FontSmall).TextColor(exitFG).SingleLine()
			})
			blockui.Probe(exitBtn, "tarot-exit")
			if exitBtn.Clicked() && host.ExitTarot != nil {
				host.ExitTarot()
			}
		})
	})
}
