package tarot

import (
	"fmt"
	"math"
	"time"

	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// QuickNoteProps configures the Tarot Mode skin for the standalone Quick Note floating window.
// Strictly skin-layer only: 3 card spines for the 3 slots, parchment editor sheet, and gold save flash.
type QuickNoteProps struct {
	ActiveSlot    int
	SlotNames     [3]string
	SavedFlash    bool
	Notice        string
	DealAt        time.Time
	SaveFlashAt   time.Time
	OnHide        func()
	OnSelectSlot  func(slot int)
	OnSave        func()
	OnWheelScroll func(delta float32)
}

// RenderQuickNoteSkin renders the Quick Note window with the Midnight Copperplate skin.
func RenderQuickNoteSkin(c *ui.Context, p QuickNoteProps, renderEditor func()) {
	tc := theme.Tarot()
	now := time.Now()

	root := ui.Column(c.Key("quick-note-root")).Fill().Background(tc.QuickShell).
		TextColor(tc.TextPrimary).Border(1, tc.GoldBorderSoft).Clip()
	blockui.Probe(root, "quick-note-root")

	root.Children(func() {
		// 1. 44px Midnight Copperplate drag header: golden dismiss dot, 3 card spines, brass save button.
		header := ui.Row(c.Key("quick-header")).FillWidth().Height(theme.TarotTopbarHeight).Shrink(0).
			Padding(0, theme.Space12, 0, theme.Space16).AlignItems(ui.Center).Justify(ui.SpaceBetween).
			Background(tc.ChromeBar).KeepFocus()
		header.DragWindow()
		header.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			blockui.RecordBounds("quick-header", r)
			pt.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, tc.GoldBorderHair)
			pt.Line(r.X, r.Y+r.H-3.5, r.X+r.W, r.Y+r.H-3.5, 1, tc.GoldBorderSub)
		})
		header.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputScroll || ev.Momentum || p.OnWheelScroll == nil {
				return false
			}
			if ev.Mods&(ui.Ctrl|ui.Alt|ui.Super) != 0 {
				return false
			}
			axis := ev.DY
			if math.Abs(float64(ev.DX)) > math.Abs(float64(ev.DY)) {
				axis = ev.DX
			}
			if math.Abs(float64(axis)) < 0.5 {
				return false
			}
			p.OnWheelScroll(axis)
			return true
		})

		header.Children(func() {
			// Left: golden dismiss dot.
			closeBtn := ui.ButtonBase(c.Key("quick-close")).Size(theme.HeightCompact, theme.HeightCompact).
				Shrink(0).Center().Radius(theme.RadiusPill).Cursor(ui.CursorPointer).
				Label(theme.Tr("隐藏速记", "Hide Quick Note")).KeepFocus()
			if closeBtn.Hovered() {
				closeBtn.Background(tc.GoldHoverWash)
			}
			closeBtn.Children(func() {
				ui.Box(c).Size(theme.Space12, theme.Space12).Radius(theme.RadiusPill).
					Background(tc.Gold).Border(1, tc.GoldDeep)
			})
			blockui.Probe(closeBtn, "quick-close")
			if closeBtn.Clicked() && p.OnHide != nil {
				p.OnHide()
			}

			// Center: 3 card spines (active expands into mini parchment card; idle are thin midnight card backs).
			ui.Row(c.Key("quick-pager-layer")).Absolute().Left(0).Top(0).Right(0).Bottom(0).
				Center().PassThrough().Children(func() {
				pager := ui.Row(c.Key("quick-pager")).AlignItems(ui.Center).Gap(theme.Space4)
				blockui.Probe(pager, "quick-pager")
				pager.Children(func() {
					for i := 0; i < 3; i++ {
						slot := i
						active := slot == p.ActiveSlot
						name := p.SlotNames[slot]
						if name == "" {
							name = fmt.Sprintf(theme.Tr("速记 %d", "Note %d"), slot+1)
						}
						btnID := fmt.Sprintf("quick-slot-%d", slot)
						btn := ui.ButtonBase(c.Key(btnID)).Height(theme.HeightLarge).PaddingX(theme.Space4).
							Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
							Label(fmt.Sprintf(theme.Tr("速记页 %d · %s", "Slot %d · %s"), slot+1, name)).KeepFocus()
						if btn.Hovered() {
							btn.Background(tc.GoldHoverWash)
						}
						btn.Children(func() {
							targetW := float32(14)
							if active {
								targetW = 68
							}
							w := blockui.SpringPanelTo(btn, "tarotSpineW", targetW)
							spine := ui.Box(c).Width(w).Height(30).Radius(theme.RadiusXS).Clip().Center()
							if active {
								spine.Translate(0, -1)
								spine.Draw(func(pt *ui.Painter, r ui.Rect) {
									PaintCardFrame(pt, r, theme.RadiusXS)
								})
								spine.Children(func() {
									ui.Text(c, name).Font(theme.TarotSerifFont()).FontSize(theme.FontMeta).
										FontWeight(theme.WeightTitle).TextColor(tc.InkDark).SingleLine().Ellipsis("…")
								})
							} else {
								spine.Draw(func(pt *ui.Painter, r ui.Rect) {
									PaintCardBack(pt, r, theme.RadiusXS, 0.55)
								})
							}
						})
						blockui.Probe(btn, btnID)
						if btn.Clicked() && p.OnSelectSlot != nil {
							p.OnSelectSlot(slot)
						}
					}
				})
			})

			// Right: brass-outlined save button -> turns solid gold foil on savedFlash.
			saveBtn := ui.ButtonBase(c.Key("quick-save")).Size(theme.HeightControl, theme.HeightControl).
				Shrink(0).Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
				Label(theme.Tr("存入正式笔记", "Save to Note")).KeepFocus()
			iconKind := IconSave
			iconCol := tc.Gold
			if p.SavedFlash {
				saveBtn.LinearGradient(tc.GoldBtnGrad).Border(1, tc.GoldBright)
				iconKind = IconCheck
				iconCol = tc.GoldBtnText
			} else if saveBtn.Hovered() {
				saveBtn.Background(tc.GoldHoverWash).Border(1, tc.GoldBright)
			} else {
				saveBtn.Border(1, tc.GoldBorderMid)
			}
			saveBtn.Children(func() {
				IconBox(c, iconKind, theme.IconAction, iconCol)
			})
			saveBtn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				blockui.RecordBounds("quick-save", r)
				blockui.RecordBounds("save-to-note", r)
			})
			if saveBtn.Clicked() && p.OnSave != nil {
				p.OnSave()
			}
		})

		// 2. Parchment editor sheet (12px midnight gutter around warm parchment card with 4 gold corners).
		sheetWrap := ui.Box(c.Key("quick-tarot-sheet-wrap")).Grow(1).MinHeight(0).FillWidth().
			Padding(theme.Space12)
		sheetWrap.Children(func() {
			flashProg := float32(0)
			if !p.SaveFlashAt.IsZero() {
				el := now.Sub(p.SaveFlashAt).Seconds()
				if el >= 0 && el < 1.1 {
					flashProg = float32(el / 1.1)
				}
			}

			sheet := ui.Box(c.Key(fmt.Sprintf("quick-editor-slot-%d", p.ActiveSlot))).
				Fill().Padding(theme.Space20, theme.Space16, theme.Space20, theme.Space16).
				Radius(theme.RadiusMD).Background(tc.ParchmentPaper).Clip()
			// Subtle deal-in rise when switching slots.
			if !p.DealAt.IsZero() {
				el := now.Sub(p.DealAt).Seconds()
				if el >= 0 && el < 0.30 {
					u := float32(el / 0.30)
					ease := 1 - (1-u)*(1-u)*(1-u)
					sheet.Translate(0, 8*(1-ease)).Opacity(0.45 + 0.55*ease)
					c.Invalidate()
				}
			}
			sheet.DrawOver(func(pt *ui.Painter, r ui.Rect) {
				blockui.RecordBounds("quick-note-editor", r)
				PaintPaperSheetOverlay(pt, r, theme.RadiusMD, flashProg, false)
				if flashProg > 0 && flashProg < 1 && !pt.Preferences().ReduceMotion {
					pt.AnimationFrameRate(60)
				}
			})
			sheet.Children(func() {
				if renderEditor != nil {
					theme.WithPalette(theme.TarotPaperPalette(), renderEditor)
				}
			})
		})
	})

	// 3. Bottom-right dark-gold floating toast pill on save.
	if p.Notice != "" {
		toast := ui.Row(c.Key("quick-notice")).Absolute().Right(theme.Space16).Bottom(theme.Space16).
			MaxWidth(360).Height(30).Padding(0, theme.Space12, 0, theme.Space12).Gap(theme.Space8).
			AlignItems(ui.Center).Radius(theme.RadiusPill).Background(tc.Ground).
			Border(1, tc.GoldBorderHover).Transition(blockui.PopoverSpringTransition())
		blockui.Elevate(toast, theme.ElevationTarotCard)
		toast.Children(func() {
			IconBox(c, IconCheck, theme.IconInline, tc.GoldBright)
			ui.Text(c, p.Notice).FontSize(theme.FontSmall).TextColor(tc.GoldBright).SingleLine()
		})
	}
}
