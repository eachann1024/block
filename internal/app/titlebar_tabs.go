package app

import (
	"fmt"
	"time"

	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// Tab metrics (thinkly-fast app_tabs_menu.rs / app_tabs.rs).
const (
	tabAnimMs      = 150
	tabBasisW      = 220
	tabMinW        = 76
	tabMaxW        = 220
	tabFadeW       = 20
	singleTabMaxW  = 240
	tabPillH       = 28
	tabStripGap    = 4
	tabDragSpacerW = 16
)

// tabTransition is the 150ms ease-out-quint open/close/reflow motion of a multi-tab pill.
func tabTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: tabAnimMs * time.Millisecond,
		Ease:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Position: true,
		Enter:    &ui.Motion{Collapse: true},
		Exit:     &ui.Motion{Collapse: true},
	}
}

// multiTabWidth shares the strip among n tabs as flex-basis 220 / shrink /
// min 76 does, from the tab cell's laid-out width (padding, new-tab button
// and drag spacer taken out). Past 76 the strip scrolls.
func multiTabWidth(centerW float32, n int) float32 {
	if centerW <= 0 || n <= 0 {
		return tabBasisW
	}
	avail := centerW - 20 - (theme.ChromeBoxSize + tabStripGap) - (tabDragSpacerW + tabStripGap)
	w := (avail - float32(n-1)*tabStripGap) / float32(n)
	return max(min(w, tabMaxW), tabMinW)
}

// tabFade is the right-edge ramp from transparent col to col that clips a long title.
func tabFade(c *ui.Context, col ui.Color) ui.Element {
	return ui.Box(c).Width(tabFadeW).FillHeight().Shrink(0).Gradient(col.Alpha(0), col, 90)
}

func tabLabel(c *ui.Context, label string, fg ui.Color) {
	ui.Text(c, label).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).
		FontWeight(theme.WeightControl).TextColor(fg).SingleLine().Shrink(0)
}

func tabDirtyDot(c *ui.Context) ui.Element {
	return ui.Box(c).Size(6, 6).Shrink(0).Radius(theme.RadiusPill).Background(theme.Ocean().Accent)
}

// renderTabStrip renders the titlebar tab pills (thinkly-fast render_tab_pill).
// renderTabStrip lays out the pills; past their 76px minimum the strip scrolls.
func renderTabStrip(c *ui.Context, p TitlebarProps, centerW float32) {
	// Pills are sized from the last frame's cell width: ask for one more
	// frame whenever it changed so they settle on the current one.
	if p.MenuState != nil && (centerW <= 0 || centerW != p.MenuState.tabCenterW) {
		p.MenuState.tabCenterW = centerW
		c.After(16 * time.Millisecond)
	}
	tabW := multiTabWidth(centerW, len(p.Tabs))
	c.Key("titlebar-tab-strip")
	strip := ui.ScrollHorizontal(c).FillHeight().Shrink(1).MinWidth(0).AlignItems(ui.Center).Gap(tabStripGap).Scrollbars(false)
	blockui.Probe(strip, "titlebar-tab-strip")
	strip.Children(func() {
		if len(p.Tabs) == 1 {
			renderSingleTab(c, p, p.Tabs[0])
			return
		}
		for ix, tabPath := range p.Tabs {
			renderMultiTab(c, p, ix, tabPath, tabW)
		}
	})
}

func tabTitle(path string) string {
	if label := noteDisplayTitle(path, false); label != "" {
		return label
	}
	return theme.Tr("未命名", "Untitled")
}

// renderSingleTab: a lone tab is a plain title on the drag area, faded past 240px.
func renderSingleTab(c *ui.Context, p TitlebarProps, path string) {
	pal := theme.Ocean()
	dirty := p.IsDirty != nil && p.IsDirty(path)
	c.Key("tab:" + path)
	pill := ui.Row(c).Height(tabPillH).MinWidth(0).MaxWidth(singleTabMaxW).Shrink(1).
		Padding(0, 10, 0, 10).Radius(theme.RadiusControl).AlignItems(ui.Center).Clip().DragWindow().
		Role(ui.RoleTab).Selected(true).Label(tabTitle(path))
	pill.Children(func() {
		ui.Row(c).Shrink(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Clip().Children(func() {
			tabLabel(c, tabTitle(path), pal.Text)
			tabFade(c, pal.Shell).Absolute().Top(0).Bottom(0).Left(singleTabMaxW - 20 - tabFadeW)
		})
		if dirty {
			tabDirtyDot(c).Margin(0, 0, 0, 6)
		}
	})
	blockui.Probe(pill, "tab-0")
	blockui.Probe(pill, "tab:"+path)
}

func renderMultiTab(c *ui.Context, p TitlebarProps, ix int, path string, tabW float32) {
	pal := theme.Ocean()
	active := path == p.ActiveTab
	dirty := p.IsDirty != nil && p.IsDirty(path)
	label := tabTitle(path)
	c.Key("tab:" + path)
	pill := ui.ButtonBase(c).Width(tabW).Shrink(0).Height(tabPillH).Padding(0, 6, 0, 10).
		Radius(theme.RadiusControl).AlignItems(ui.Center).Clip().Cursor(ui.CursorPointer).
		Role(ui.RoleTab).Selected(active).Label(label).Transition(tabTransition())
	hovered := pill.Hovered()
	fg := pal.TextSecondary
	switch {
	case active:
		pill.Background(pal.Selected).Border(1, pal.Accent)
		fg = pal.Text
	case hovered:
		pill.Background(pal.Hover).Border(1, ui.Transparent)
		fg = pal.Text
	default:
		pill.Border(1, ui.Transparent)
	}

	closeHover, closeClicked := false, false
	closeBtn := func() {
		id := fmt.Sprintf("tab-close-%d", ix)
		c.Key(id)
		cb := ui.ButtonBase(c).Size(18, 18).Shrink(0).Center().Radius(theme.RadiusMicroSM).
			Cursor(ui.CursorPointer).Label(theme.Tr("关闭标签页", "Close Tab"))
		closeHover = cb.Hovered()
		ic := pal.MonoIconColors(pal.TextSecondary)
		if closeHover {
			cb.Background(pal.SelectionBG)
			ic = pal.DuoIconColors(pal.Text)
		}
		cb.Children(func() { icons.Draw(c, icons.Close, theme.IconInline, ic) })
		blockui.Probe(cb, id)
		closeClicked = cb.Clicked()
	}

	pill.Children(func() {
		body := ui.Row(c).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Clip()
		body.Children(func() {
			tabLabel(c, label, fg)
			if active {
				tabFade(c, pal.Selected).Absolute().Top(0).Bottom(0).Right(0)
				return
			}
			end := ui.Row(c).Absolute().Top(0).Bottom(0).Right(0).AlignItems(ui.Center)
			end.Children(func() {
				if hovered {
					tabFade(c, pal.Hover)
					ui.Row(c).Width(18).FillHeight().Background(pal.Hover).Center().Children(closeBtn)
					return
				}
				tabFade(c, pal.Shell)
				if dirty {
					ui.Row(c).Width(14).FillHeight().Background(pal.Shell).Center().Children(func() { tabDirtyDot(c) })
				}
			})
		})
		if active {
			if dirty {
				tabDirtyDot(c).Margin(0, 4, 0, 0)
			}
			closeBtn()
		}
	})
	blockui.Probe(pill, fmt.Sprintf("tab-%d", ix))
	blockui.Probe(pill, "tab:"+path)

	btn, pressing := pill.PressedButton()
	switch {
	case closeClicked:
		if p.OnCloseTab != nil {
			p.OnCloseTab(path)
		}
	case pressing && btn == 2:
		if p.OnCloseTab != nil {
			p.OnCloseTab(path)
		}
	case pill.RightClicked() && p.MenuState != nil:
		b := pill.Bounds()
		tx, ty := b.X+8, b.Y+b.H+4
		if px, py, ok := pill.PointerPosition(); ok {
			tx, ty = b.X+px, b.Y+py
		}
		p.MenuState.PageMenuOpen = false
		p.MenuState.TabMenuOpen = true
		p.MenuState.TabMenuIndex = ix
		p.MenuState.TabMenuPath = path
		p.MenuState.TabMenuX = tx
		p.MenuState.TabMenuY = ty
	case ((pressing && btn == 0) || pill.Clicked()) && !closeHover && !active && !p.Busy:
		// thinkly-fast activates on mouse down; Clicked covers a press and
		// release landing in one frame.
		if p.OnSelectTab != nil {
			p.OnSelectTab(path)
		}
	}
}
