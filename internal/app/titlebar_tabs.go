package app

import (
	"fmt"
	"sort"
	"time"

	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// Tab metrics (thinkly-fast app_tabs_menu.rs / app_tabs.rs).
const (
	tabAnimMs      = 100
	tabBasisW      = 220
	tabMinW        = 76
	tabMaxW        = 220
	tabCloseSlot   = 18
	singleTabMaxW  = 240
	tabPillH       = 28
	tabStripGap    = 6
	tabDragSpacerW = 16
)

// tabTransition is the short open/close/reflow motion of a multi-tab pill.
func tabTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Animation:    &blockui.SpringPanel,
		ExitDuration: tabAnimMs * time.Millisecond,
		ExitEase:     ui.CubicBezier(0.22, 1, 0.36, 1),
		Position:     true,
		Enter:        &ui.Motion{Collapse: true, Opacity: 0},
		Exit:         &ui.Motion{Collapse: true, Opacity: 0},
	}
}

// multiTabStripChrome is the tab cell width that is not pills: padding, gaps,
// the new-tab button and the drag spacer.
func multiTabStripChrome(n int) float32 {
	return float32(n-1)*tabStripGap + 20 + (theme.ChromeBoxSize + tabStripGap) + (tabDragSpacerW + tabStripGap)
}

// multiTabNaturalWidths is each pill's width with its full title (capped at tabMaxW).
func multiTabNaturalWidths(c *ui.Context, p TitlebarProps) []float32 {
	dirSuffixes := tabDirSuffixes(p.Tabs)
	out := make([]float32, len(p.Tabs))
	for i, path := range p.Tabs {
		w, _ := c.MeasureText(0, ui.Span{Text: tabTitle(path), Size: theme.FontSmall, Weight: theme.WeightRegular})
		w += theme.Space10 + theme.Space6 + tabCloseSlot + theme.Space6 + 4
		if s := dirSuffixes[path]; s != "" {
			sw, _ := c.MeasureText(0, ui.Span{Text: s, Size: theme.FontSmall, Weight: theme.WeightRegular})
			w += theme.Space6 + sw
		}
		if isExternalFile(path, p.NotebookPath) {
			w += theme.IconMicro + theme.Space6
		}
		out[i] = min(w, tabMaxW)
	}
	return out
}

// multiTabWidths fits pills into the tab cell: all keep their natural width when it
// fits; otherwise only the longest ones shrink to a shared cap (min tabMinW, past
// which the strip scrolls), so short tabs never strand space a long title needs.
func multiTabWidths(natural []float32, centerW float32) []float32 {
	out := append([]float32(nil), natural...)
	if centerW <= 0 || len(natural) == 0 {
		return out
	}
	avail := centerW - multiTabStripChrome(len(natural))
	sorted := append([]float32(nil), natural...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	limit := sorted[len(sorted)-1]
	for i, w := range sorted {
		rest := float32(len(sorted) - i)
		if w*rest > avail {
			limit = avail / rest
			break
		}
		avail -= w
	}
	limit = max(limit, tabMinW)
	for i, w := range out {
		out[i] = min(w, limit)
	}
	return out
}

func tabLabel(c *ui.Context, label string, fg ui.Color) {
	ui.Text(c, label).FontSize(theme.FontSmall).LineHeight(20.0 / theme.FontSmall).
		FontWeight(theme.WeightRegular).TextColor(fg).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
}

func tabDirtyDot(c *ui.Context) ui.Element {
	return ui.Box(c).Size(6, 6).Shrink(0).Radius(theme.RadiusPill).Background(theme.Ocean().Accent)
}

// renderTabStrip renders the titlebar tab pills (thinkly-fast render_tab_pill).
// renderTabStrip lays out the pills; past their 76px minimum the strip scrolls.
func renderTabStrip(c *ui.Context, p TitlebarProps, centerW, availW float32, viewportResized bool) {
	// Pills are sized from the last frame's cell width: ask for one more
	// frame whenever it changed so they settle on the current one. A cell
	// with no width yet asks once (tabCenterW -1), never every frame.
	widthShrank := false
	if m := p.MenuState; m != nil {
		widthShrank = centerW > 0 && m.tabCenterW > 0 && centerW < m.tabCenterW
		switch {
		case centerW > 0 && centerW != m.tabCenterW:
			m.tabCenterW = centerW
			c.After(16 * time.Millisecond)
		case centerW <= 0 && m.tabCenterW >= 0:
			m.tabCenterW = -1
			c.After(16 * time.Millisecond)
		}
	}
	natural := multiTabNaturalWidths(c, p)
	tabWs := multiTabWidths(natural, centerW)
	if m := p.MenuState; m != nil {
		m.tabsNeedW = 0
		if len(p.Tabs) > 1 {
			for _, w := range natural {
				m.tabsNeedW += w
			}
			m.tabsNeedW += multiTabStripChrome(len(natural))
		}
	}
	c.Key("titlebar-tab-strip")
	strip := ui.ScrollHorizontal(c).FillHeight().Shrink(1).MinWidth(0).AlignItems(ui.Center).Gap(tabStripGap).Scrollbars(false)
	var middleClosePath string
	shouldReveal := false
	if p.MenuState != nil {
		strip.TrackScroll(&p.MenuState.TabScroll)
		if p.MenuState.MiddleClosePath != "" {
			middleClosePath = p.MenuState.MiddleClosePath
			p.MenuState.MiddleClosePath = ""
		}
		postResizeReveal := p.MenuState.revealActiveTab && !viewportResized
		if postResizeReveal {
			p.MenuState.revealActiveTab = false
		}
		shouldReveal = widthShrank || postResizeReveal || p.MenuState.LastActiveTab != p.ActiveTab || p.MenuState.LastTabCount != len(p.Tabs)
	}
	blockui.Probe(strip, "titlebar-tab-strip")
	strip.Children(func() {
		if len(p.Tabs) == 1 {
			renderSingleTab(c, p, p.Tabs[0], centerW, availW)
			return
		}
		dirSuffixes := tabDirSuffixes(p.Tabs)
		for ix, tabPath := range p.Tabs {
			renderMultiTab(c, p, ix, tabPath, dirSuffixes[tabPath], tabWs[ix], shouldReveal, middleClosePath)
		}
	})
	if p.MenuState != nil {
		p.MenuState.LastActiveTab = p.ActiveTab
		p.MenuState.LastTabCount = len(p.Tabs)
	}
}

func tabTitle(path string) string {
	if label := noteDisplayTitle(path, false); label != "" {
		return label
	}
	return theme.Tr("未命名", "Untitled")
}

// renderSingleTab renders the notebook badge + breadcrumb + active page title on the drag area.
// In narrow windows or tight center cells, the notebook prefix collapses so the filename stays readable.
func renderSingleTab(c *ui.Context, p TitlebarProps, path string, centerW, availW float32) {
	pal := theme.Ocean()
	vpW, _ := c.Size()
	dirty := p.IsDirty != nil && p.IsDirty(path)
	external := isExternalFile(path, p.NotebookPath)
	compactCrumb := isNarrowTitlebar(vpW, availW) || (centerW > 0 && centerW < 240)
	nbTitle := ""
	if !external && p.NotebookPath != "" {
		nbTitle = noteDisplayTitle(p.NotebookPath, true)
	}

	if nbTitle != "" && !compactCrumb {
		c.Key("titlebar-nb-crumb")
		crumb := ui.Row(c).Height(tabPillH).PaddingX(6).Gap(7).Shrink(0).
			AlignItems(ui.Center).DragWindow()
		crumb.Children(func() {
			blockui.MonogramBadge(c, nbTitle)
			ui.Text(c, nbTitle).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).
				TextColor(pal.TextSecondary).MaxWidth(120).SingleLine().Ellipsis("…").Shrink(0)
		})
		blockui.Probe(crumb, "titlebar-nb-crumb")
		ui.Text(c, "/").FontSize(theme.FontBody).TextColor(pal.Placeholder).SingleLine().Shrink(0)
	}

	title := tabTitle(path)
	c.Key("tab:" + path)
	pill := ui.Row(c).Height(tabPillH).MinWidth(0).MaxWidth(singleTabMaxW).Shrink(1).
		Padding(0, 8, 0, 6).Gap(theme.Space6).Radius(theme.RadiusMD).AlignItems(ui.Center).Clip().DragWindow().
		Role(ui.RoleTab).AccessSelected(true).Label(title)
	if external {
		blockui.RichTooltip(c, pill, blockui.TooltipSpec{
			Rows: []blockui.TooltipRow{
				{Trigger: theme.DisplayFolderPath(path)},
				{Detail: theme.Tr("不在当前文件夹", "Outside current folder")},
			},
		})
	} else if compactCrumb && nbTitle != "" {
		blockui.RichTooltip(c, pill, blockui.TooltipSpec{
			Rows: []blockui.TooltipRow{
				{Trigger: nbTitle + " / " + title},
			},
		})
	}
	pill.Children(func() {
		if external {
			icons.DrawAnimated(c, icons.LinkOpen, theme.IconMicro, pal.MonoIconColors(pal.MenuMuted), pill.Hovered() || pill.Pressed())
		}
		ui.Text(c, title).FontSize(theme.FontSmall).LineHeight(20.0 / theme.FontSmall).
			FontWeight(theme.WeightRegular).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
		if dirty {
			tabDirtyDot(c)
		}
	})
	if blockui.IsProbeEnabled() {
		pill.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			blockui.RecordBounds("tab-0", r)
			blockui.RecordBounds("tab:"+path, r)
		})
	}
}

func renderMultiTab(c *ui.Context, p TitlebarProps, ix int, path, dirSuffix string, tabW float32, shouldReveal bool, middleClosePath string) {
	pal := theme.Ocean()
	active := path == p.ActiveTab
	dirty := p.IsDirty != nil && p.IsDirty(path)
	external := isExternalFile(path, p.NotebookPath)
	label := tabTitle(path)
	a11yLabel := label
	if dirSuffix != "" {
		a11yLabel = label + " · " + dirSuffix
	}
	c.Key("tab:" + path)
	pill := ui.Row(c).MaxWidth(tabW).Shrink(0).Height(tabPillH).Padding(0, theme.Space6, 0, 0).Gap(theme.Space6).
		Radius(theme.RadiusMD).AlignItems(ui.Center).Justify(ui.Start).Clip().Transition(tabTransition())
	if (external || dirSuffix != "") && (p.MenuState == nil || !p.MenuState.TabMenuOpen) {
		rows := []blockui.TooltipRow{{Trigger: theme.DisplayFolderPath(path)}}
		if external {
			rows = append(rows, blockui.TooltipRow{Detail: theme.Tr("不在当前文件夹", "Outside current folder")})
		}
		blockui.RichTooltip(c, pill, blockui.TooltipSpec{Rows: rows})
	}
	pill.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind == ui.InputPointerDown && ev.Button == 2 && !p.Busy && p.MenuState != nil {
			p.MenuState.MiddleClosePath = path
		}
	})

	isDropTarget := false
	dropAfter := false
	if !p.Busy && p.OnReorderTab != nil {
		if dragged, over := ui.DragOver[tabDragItem](pill); over && dragged.Path != "" && dragged.Path != path {
			b := pill.Bounds()
			px, _, hasPos := pill.PointerPosition()
			if hasPos && b.W > 0 {
				dropAfter = px >= b.W*0.5
			} else if p.MenuState != nil && p.MenuState.DragTargetPath == path {
				dropAfter = p.MenuState.DragAfter
			}
			isDropTarget = true
			if p.MenuState != nil {
				p.MenuState.DragTargetPath = path
				p.MenuState.DragAfter = dropAfter
			}
		}
		if dropped, ok := ui.Drop[tabDragItem](pill); ok {
			after := dropAfter
			if !isDropTarget && p.MenuState != nil && p.MenuState.DragTargetPath == path {
				after = p.MenuState.DragAfter
			} else {
				b := pill.Bounds()
				if px, _, hasPos := pill.PointerPosition(); hasPos && b.W > 0 {
					after = px >= b.W*0.5
				}
			}
			if p.MenuState != nil {
				p.MenuState.DragTargetPath = ""
				p.MenuState.DragAfter = false
			}
			if dropped.Path != "" && dropped.Path != path {
				p.OnReorderTab(dropped.Path, path, after)
			}
		}
	}

	hovered := pill.Hovered()
	tabCtxOpen := p.MenuState != nil && p.MenuState.TabMenuOpen && p.MenuState.TabMenuPath == path
	chrome := pal.ShellChrome()
	fg := pal.TextSecondary
	switch {
	case active:
		blockui.Elevate(pill.Background(pal.Paper).Border(1, chrome.TabBorder), theme.ElevationCard)
		fg = pal.Text
	case hovered || tabCtxOpen || isDropTarget:
		pill.Background(chrome.Hover).Border(1, ui.Transparent)
		fg = pal.Text
	default:
		pill.Border(1, ui.Transparent)
	}

	closeHover, closeClicked := false, false
	closeBtn := func() {
		c.Key("tab-close:" + path)
		cursor := ui.CursorPointer
		if p.Busy {
			cursor = ui.CursorDefault
		}
		cb := ui.ButtonBase(c).Size(18, 18).Shrink(0).Center().Radius(theme.RadiusXS).
			Cursor(cursor).Disabled(p.Busy).Label(fmt.Sprintf(theme.Tr("关闭标签页「%s」", "Close Tab %s"), label))
		closeHover = !p.Busy && cb.Hovered()
		closePress := !p.Busy && cb.Pressed()
		ic := pal.MonoIconColors(pal.TextSecondary)
		cbBG := ui.Transparent
		if active {
			cbBG = chrome.Hover
		}
		if closeHover || closePress {
			cbBG = chrome.Hover
			if active {
				cbBG = pal.Selected
			}
			ic = pal.MonoIconColors(pal.Text)
		}
		blockui.AnimateSurfaceColors(cb, cbBG, ui.Transparent, false)
		blockui.TactileButton(cb, closeHover, closePress, false)
		cb.Children(func() { icons.DrawAnimated(c, icons.Close, theme.IconMicro, ic, closeHover || closePress) })
		if blockui.IsProbeEnabled() {
			id := fmt.Sprintf("tab-close-%d", ix)
			blockui.Probe(cb, id)
		}
		closeClicked = cb.Clicked()
	}

	var sel ui.Element
	pill.Children(func() {
		tabCursor := ui.CursorPointer
		if p.Busy {
			tabCursor = ui.CursorDefault
		}
		sel = ui.ButtonBase(c.Key("tab-select:"+path)).Grow(1).MinWidth(0).FillHeight().Padding(0, 0, 0, theme.Space10).Gap(theme.Space6).
			AlignItems(ui.Center).Justify(ui.Start).Cursor(tabCursor).Disabled(p.Busy).DimDisabled(false).
			FocusRing(false).Role(ui.RoleTab).AccessSelected(active).Label(a11yLabel)
		if !p.Busy && p.OnReorderTab != nil {
			var gate *blockui.DragGate
			if p.MenuState != nil {
				gate = &p.MenuState.TabDragGate
			}
			gate.Attach(sel, path, tabDragItem{Path: path}, blockui.DefaultDragThreshold)
		}
		if tabCtxOpen {
			sel.Bind(&p.MenuState.TabCtxTrigger)
		}
		sel.Children(func() {
			if external {
				icons.DrawAnimated(c, icons.LinkOpen, theme.IconMicro, pal.MonoIconColors(pal.MenuMuted), hovered || sel.Pressed())
			}
			tabLabel(c, label, fg)
			if dirSuffix != "" {
				ui.Text(c, dirSuffix).FontSize(theme.FontSmall).LineHeight(20.0 / theme.FontSmall).
					FontWeight(theme.WeightRegular).TextColor(pal.MenuMuted).
					Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
			}
		})
		// The close slot is always reserved so a pill never changes width on hover.
		// FocusWithin keeps the close button reachable via keyboard Tab on any tab.
		switch {
		case hovered || pill.FocusWithin() || (active && !dirty):
			closeBtn()
		case dirty:
			ui.Box(c).Size(tabCloseSlot, tabCloseSlot).Shrink(0).Center().Children(func() { tabDirtyDot(c) })
		default:
			ui.Box(c).Size(tabCloseSlot, tabCloseSlot).Shrink(0)
		}
	})
	if shouldReveal && active {
		blockui.RevealActiveItem(pill)
	}
	if isDropTarget || blockui.IsProbeEnabled() {
		pill.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			if blockui.IsProbeEnabled() {
				blockui.RecordBounds(fmt.Sprintf("tab-%d", ix), r)
				blockui.RecordBounds("tab:"+path, r)
			}
			if isDropTarget {
				x := r.X
				if dropAfter {
					x = r.X + r.W - 2
				}
				pt.Fill(ui.Rect{X: x, Y: r.Y + 2, W: 2, H: max(r.H-4, 8)}, pal.Accent, 1)
			}
		})
	}

	btn, pressing := sel.PressedButton()
	midTriggered := middleClosePath == path || (p.MenuState == nil && pressing && btn == 2)
	rightClicked := sel.RightClicked() || pill.RightClicked()
	switch {
	case (closeClicked || midTriggered) && !p.Busy:
		if p.MenuState != nil {
			p.MenuState.Close()
		}
		if p.OnCloseTab != nil {
			p.OnCloseTab(path)
		}
	case rightClicked && !p.Busy && p.MenuState != nil:
		b := pill.Bounds()
		tx, ty := b.X+8, b.Y+b.H+4
		if px, py, ok := pill.PointerPosition(); ok {
			tx, ty = b.X+px, b.Y+py
		}
		p.MenuState.Close()
		p.MenuState.TabMenuOpen = true
		p.MenuState.TabMenuIndex = ix
		p.MenuState.TabMenuPath = path
		p.MenuState.TabMenuX = tx
		p.MenuState.TabMenuY = ty
		p.MenuState.MenuTrigger = &p.MenuState.TabCtxTrigger
	case ((pressing && btn == 0) || sel.Clicked()) && !closeHover && !active && !p.Busy && !sel.Dragging():
		if p.MenuState != nil {
			p.MenuState.Close()
		}
		// thinkly-fast activates on mouse down; Clicked covers a press and
		// release landing in one frame.
		if p.OnSelectTab != nil {
			p.OnSelectTab(path)
		}
	}
}
