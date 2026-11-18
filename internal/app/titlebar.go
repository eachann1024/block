package app

import (
	"path/filepath"
	"strconv"
	"strings"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// TabMenuState tracks open titlebar floating menus (tab context menu or page "more" menu),
// plus horizontal tab strip scroll, middle-click close, and drag-reorder state.
type TabMenuState struct {
	MenuCursor   int
	TabMenuOpen  bool
	TabMenuIndex int
	TabMenuPath  string
	TabMenuX     float32
	TabMenuY     float32

	PageMenuOpen   bool
	PageMenuAnchor ui.Rect

	// tabCenterW is the tab cell width the pills were last sized from (-1: not laid out yet).
	tabCenterW float32

	TabScroll       ui.ScrollState
	LastActiveTab   string
	LastTabCount    int
	DragTargetPath  string
	DragAfter       bool
	MiddleClosePath string
}

type tabDragItem struct {
	Path string
}

// NewTabMenuState returns a TabMenuState with no active keyboard menu selection.
func NewTabMenuState() *TabMenuState {
	return &TabMenuState{MenuCursor: -1}
}

// Close dismisses any open titlebar menu.
func (s *TabMenuState) Close() bool {
	had := s.TabMenuOpen || s.PageMenuOpen
	s.TabMenuOpen = false
	s.PageMenuOpen = false
	s.MenuCursor = -1
	return had
}

// TitlebarProps supplies current state and callbacks to RenderTitlebar.
type TitlebarProps struct {
	SidebarWide       bool
	SidebarAutoHidden bool
	SidebarWidth      float32
	InSettings        bool
	SettingsTabLabel  string
	HideMark          bool
	AIImmersive       bool
	AIOpen            bool
	FeedbackOpen      bool
	HasFeedbackDraft  bool

	NotebookPath string
	Tabs         []string
	ActiveTab    string
	IsDirty      func(path string) bool

	ActiveSurface  TitlebarSurfaceKind
	SourceStatus   string
	PreviewZoomPct string
	PreviewZoomFit bool
	SVGShowSource  bool

	CanBack    bool
	CanForward bool
	Busy       bool

	ShortcutSidebar string
	SidebarFloating bool
	ShortcutNewTab  string
	ShortcutAI      string
	ShortcutExport  string

	// Annotation summary for the titlebar send segment (thinkly-fast annotation_summary).
	AnnotDrafts       int
	AnnotGenerating   bool
	AnnotHasAny       bool
	OnSendAnnotations func()

	MenuState *TabMenuState

	// Sidebar actions shown in the left cell while the sidebar is expanded.
	SearchOpen      bool
	ShortcutSearch  string
	ShortcutNewNote string
	OnOpenSearch    func()

	OnToggleSidebar   func()
	OnCloseSettings   func()
	OnClickNotebook   func()
	OnBack            func()
	OnForward         func()
	OnSelectTab       func(path string)
	OnCloseTab        func(path string)
	OnCloseOthers     func(keepPath string)
	OnCloseRight      func(pivotPath string)
	OnCloseAll        func()
	OnReorderTab      func(srcPath, targetPath string, after bool)
	OnNewNote         func()
	OnToggleFeedback  func()
	OnToggleAI        func()
	OnOpenHistory     func()
	OnOpenExport      func()
	OnCopyMarkdown    func()
	OnRevealPath      func(path string)
	NoteStats         func() (characters, blocks int)
	OnPreviewZoomStep func(factor float32)
	OnPreviewActual   func()
	OnPreviewFit      func()
	OnSelectSVGSource func(showSource bool)
	OnOpenInBrowser   func()
}

func noteDisplayTitle(p string, isDir bool) string {
	if p == "" {
		return ""
	}
	base := filepath.Base(p)
	if !isDir && files.IsMarkdown(base) {
		ext := filepath.Ext(base)
		return strings.TrimSuffix(base, ext)
	}
	return base
}

func localizeSettingsTabLabel(tab string) string {
	switch tab {
	case "ai":
		return theme.Tr("AI 助手", "AI Assistant")
	case "appearance":
		return theme.Tr("外观与字体", "Appearance & Fonts")
	case "folders":
		return theme.Tr("文件夹", "Folders")
	case "shortcuts":
		return theme.Tr("快捷键", "Shortcuts")
	default:
		return tab
	}
}

// RenderTitlebar renders the 40px T3-inspired floating-shell title bar
// plus any anchored tab context menu or page "More" menu.
func RenderTitlebar(c *ui.Context, p TitlebarProps) {
	pal := theme.Ocean()
	tb := c.TitleBar()
	leftLead := float32(10)
	if theme.IsMacOS() {
		leftLead = max(80, tb.Left+12)
	}

	leftW := leftLead + theme.ChromeBoxSize + 6
	if p.AIImmersive {
		leftW = leftLead
	} else if p.SidebarWide {
		leftW = max(p.SidebarWidth, SidebarMinW)
	}

	c.Key("titlebar")
	bar := ui.Row(c).FillWidth().Height(TitlebarHeight).Shrink(0).AlignItems(ui.Center)
	blockui.Probe(bar, "titlebar")

	bar.Children(func() {
		// 1. Left cell: traffic-light clearance, fixed sidebar toggle, brand mark, and Back/Forward.
		c.Key("titlebar-left")
		left := ui.Row(c).Width(leftW).FillHeight().Shrink(0).AlignItems(ui.Center).
			Padding(0, 6, 0, leftLead).DragWindow()
		blockui.Probe(left, "titlebar-left")
		left.Children(func() {
			if p.AIImmersive {
				return
			}
			sidebarOpen := p.SidebarWide || p.SidebarFloating
			glyph := icons.SidebarExpand
			if sidebarOpen {
				glyph = icons.SidebarCollapse
			}
			sbSc := nonEmptyOr(p.ShortcutSidebar, "alt-b")
			var tip string
			if p.InSettings {
				if sidebarOpen {
					tip = theme.Tr("收起导航", "Collapse Navigation")
				} else {
					tip = theme.Tr("展开导航", "Expand Navigation")
				}
			} else {
				if sidebarOpen {
					tip = theme.Tr("收起侧栏", "Collapse Sidebar")
				} else {
					tip = theme.Tr("展开侧栏", "Expand Sidebar")
				}
			}
			sbBtn := blockui.IconButton(c, glyph, blockui.IconButtonOptions{
				ID:          "sidebar-toggle",
				Tooltip:     tip,
				Shortcut:    blockui.FormatShortcutDisplay(sbSc),
				Active:      p.SidebarFloating,
				ActiveStyle: blockui.ActiveAccent,
				ChromeHover: true,
			})
			if sbBtn.Clicked() && p.OnToggleSidebar != nil {
				p.OnToggleSidebar()
			}

			if !p.SidebarWide {
				return
			}

			if !p.HideMark {
				blockui.AppMark(c).Margin(0, 0, 0, 4)
			}
			ui.Spacer(c)

			if !p.InSettings {
				ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
					backBtn := blockui.IconButton(c, icons.ArrowLeft, blockui.IconButtonOptions{
						ID:          "nav-back",
						Size:        26,
						IconSize:    16,
						Tooltip:     theme.Tr("后退", "Back"),
						Shortcut:    blockui.FormatShortcutDisplay("cmd-["),
						Disabled:    p.Busy || !p.CanBack,
						ChromeHover: true,
					})
					if backBtn.Clicked() && !p.Busy && p.CanBack && p.OnBack != nil {
						p.OnBack()
					}
					fwdBtn := blockui.IconButton(c, icons.ArrowRight, blockui.IconButtonOptions{
						ID:          "nav-forward",
						Size:        26,
						IconSize:    16,
						Tooltip:     theme.Tr("前进", "Forward"),
						Shortcut:    blockui.FormatShortcutDisplay("cmd-]"),
						Disabled:    p.Busy || !p.CanForward,
						ChromeHover: true,
					})
					if fwdBtn.Clicked() && !p.Busy && p.CanForward && p.OnForward != nil {
						p.OnForward()
					}
				})
			}
		})

		// 2. Center cell: settings breadcrumb, empty notebook title, single-tab breadcrumb, or multi-tab strip.
		c.Key("titlebar-tabs")
		center := ui.Row(c).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Gap(4).PaddingX(4).Clip()
		blockui.Probe(center, "titlebar-tabs")
		centerW := center.Bounds().W
		center.Children(func() {
			if p.InSettings {
				crumb := ui.Row(c).Fill().MinWidth(0).AlignItems(ui.Center).Gap(4).PaddingX(6).DragWindow()
				crumb.Children(func() {
					ui.Text(c, theme.Tr("设置", "Settings")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
					ui.Text(c, "/").FontSize(theme.FontTitle).TextColor(pal.Placeholder).SingleLine().Shrink(0)
					ui.Text(c, localizeSettingsTabLabel(p.SettingsTabLabel)).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
				return
			}
			if len(p.Tabs) == 0 {
				drag := ui.Row(c).Fill().MinWidth(0).AlignItems(ui.Center).Gap(7).PaddingX(6).DragWindow()
				drag.Children(func() {
					if p.NotebookPath != "" {
						nbTitle := noteDisplayTitle(p.NotebookPath, true)
						blockui.MonogramBadge(c, nbTitle)
						ui.Text(c, nbTitle).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					} else {
						ui.Text(c, "Block").FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					}
				})
				return
			}

			multi := len(p.Tabs) > 1
			renderTabStrip(c, p, centerW)

			if multi {
				newTabBtn := blockui.IconButton(c, icons.Plus, blockui.IconButtonOptions{
					ID:          "tab-new",
					Size:        theme.ChromeBoxSize,
					IconSize:    16,
					Tooltip:     theme.Tr("新建标签页", "New Tab"),
					Shortcut:    blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutNewTab, "cmd-t")),
					Disabled:    p.Busy,
					ChromeHover: true,
				})
				if newTabBtn.Clicked() && !p.Busy && p.OnNewNote != nil {
					p.OnNewNote()
				}
			}

			dragSpacer := ui.Box(c).Grow(1).MinWidth(16).FillHeight().DragWindow()
			if multi && p.OnReorderTab != nil && len(p.Tabs) > 0 {
				lastTab := p.Tabs[len(p.Tabs)-1]
				spacerDrop := false
				if dragged, over := ui.DragOver[tabDragItem](dragSpacer); over && dragged.Path != "" && dragged.Path != lastTab {
					spacerDrop = true
					if p.MenuState != nil {
						p.MenuState.DragTargetPath = lastTab
						p.MenuState.DragAfter = true
					}
				}
				if dropped, ok := ui.Drop[tabDragItem](dragSpacer); ok {
					if p.MenuState != nil {
						p.MenuState.DragTargetPath = ""
						p.MenuState.DragAfter = false
					}
					if dropped.Path != "" && dropped.Path != lastTab {
						p.OnReorderTab(dropped.Path, lastTab, true)
					}
				}
				if spacerDrop {
					dragSpacer.DrawOver(func(pt *ui.Painter, rect ui.Rect) {
						pt.Fill(ui.Rect{X: rect.X, Y: rect.Y + 6, W: 2, H: max(rect.H-12, 12)}, pal.Accent, 1)
					})
				}
			}
		})

		// 3. Right tail: Settings close button, or contextual file tools + Feedback + AI Panel + Page More pill.
		if p.InSettings {
			pad := float32(titlebarActionTrailing)
			if !theme.IsMacOS() {
				pad = 0
				if tb.Right > 0 {
					pad = tb.Right + 8
				}
			}
			ui.Row(c).FillHeight().Shrink(0).AlignItems(ui.Center).Padding(0, pad, 0, 0).Children(func() {
				closeBtn := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
					ID:          "settings-back",
					Label:       theme.Tr("关闭设置", "Close Settings"),
					Tooltip:     theme.Tr("关闭设置", "Close Settings"),
					ChromeHover: true,
				})
				if closeBtn.Clicked() && p.OnCloseSettings != nil {
					p.OnCloseSettings()
				}
			})
		} else if !p.AIImmersive {
			renderTitlebarTail(c, p, tb.Right)
		}
	})

	renderTitlebarMenus(c, p)
}

func renderNonNoteTitlebarTools(c *ui.Context, p TitlebarProps, surface TitlebarSurfaceKind) {
	pal := theme.Ocean()
	vpW, _ := c.Size()
	showStatus := p.SourceStatus != "" && (vpW <= 0 || vpW >= 860)
	switch surface {
	case SurfaceImagePreview:
		renderZoomCluster(c, "tb-img-zoom", p)
		renderRevealButton(c, p)
	case SurfaceSVG:
		if showStatus {
			ui.Text(c, p.SourceStatus).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
		}
		svgIdx := 0
		if p.SVGShowSource {
			svgIdx = 1
		}
		if blockui.SegmentedControl(c, "svg-mode", &svgIdx, []blockui.SegmentOption{
			{ID: "svg-mode-preview", Label: theme.Tr("预览", "Preview")},
			{ID: "svg-mode-source", Label: theme.Tr("源码", "Source")},
		}) && p.OnSelectSVGSource != nil {
			p.OnSelectSVGSource(svgIdx == 1)
		}
		if !p.SVGShowSource {
			renderZoomCluster(c, "tb-svg-zoom", p)
		}
		renderRevealButton(c, p)
	case SurfaceHTML:
		if showStatus {
			ui.Text(c, p.SourceStatus).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
		}
		icOpen := icons.LinkOpen
		openBtn := blockui.ActionButton(c, "html-browser", theme.Tr("浏览器打开", "Open in Browser"), &icOpen, blockui.ButtonSecondary, false)
		if openBtn.Clicked() && p.OnOpenInBrowser != nil {
			p.OnOpenInBrowser()
		}
		renderRevealButton(c, p)
	case SurfaceCode:
		if showStatus {
			ui.Text(c, p.SourceStatus).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
		}
		renderRevealButton(c, p)
	}
}

func renderZoomCluster(c *ui.Context, id string, p TitlebarProps) {
	pal := theme.Ocean()
	pct := p.PreviewZoomPct
	if pct == "" {
		pct = "100%"
	}
	c.Key(id)
	cluster := ui.Row(c).Height(28).PaddingX(4).Gap(2).AlignItems(ui.Center).
		Radius(theme.RadiusMD).Background(pal.Card).Border(1, pal.BtnBorder)
	blockui.Probe(cluster, id)
	cluster.Children(func() {
		outBtn := blockui.IconButton(c, icons.TbMinus, blockui.IconButtonOptions{
			ID:       id + "-out",
			Size:     22,
			IconSize: 13,
			Tooltip:  theme.Tr("缩小", "Zoom Out"),
		})
		if outBtn.Clicked() && p.OnPreviewZoomStep != nil {
			p.OnPreviewZoomStep(1.0 / 1.2)
		}
		c.Key(id + "-pct")
		pctBtn := ui.ButtonBase(c).Height(22).PaddingX(6).Center().Radius(theme.RadiusXS).Cursor(ui.CursorPointer).Label(pct)
		if pctBtn.Hovered() {
			pctBtn.Background(pal.Hover)
		}
		pctBtn.Children(func() {
			ui.Text(c, pct).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
		})
		blockui.Probe(pctBtn, id+"-pct")
		if pctBtn.Clicked() && p.OnPreviewActual != nil {
			p.OnPreviewActual()
		}
		inBtn := blockui.IconButton(c, icons.TbPlus, blockui.IconButtonOptions{
			ID:       id + "-in",
			Size:     22,
			IconSize: 13,
			Tooltip:  theme.Tr("放大", "Zoom In"),
		})
		if inBtn.Clicked() && p.OnPreviewZoomStep != nil {
			p.OnPreviewZoomStep(1.2)
		}
		c.Key(id + "-fit")
		fitBtn := ui.ButtonBase(c).Height(22).PaddingX(6).Center().Radius(theme.RadiusXS).Border(1, ui.Transparent).Cursor(ui.CursorPointer).
			Label(theme.Tr("适应", "Fit"))
		if p.PreviewZoomFit {
			fitBtn.Background(pal.Selected).Border(1, pal.Accent)
		} else if fitBtn.Hovered() {
			fitBtn.Background(pal.Hover)
		}
		fitBtn.Children(func() {
			ui.Text(c, theme.Tr("适应", "Fit")).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
		})
		blockui.Probe(fitBtn, id+"-fit")
		if fitBtn.Clicked() && p.OnPreviewFit != nil {
			p.OnPreviewFit()
		}
	})
}

func renderRevealButton(c *ui.Context, p TitlebarProps) {
	revBtn := blockui.IconButton(c, icons.FolderReveal, blockui.IconButtonOptions{
		ID:          "tb-reveal",
		Tooltip:     revealFileLabel(),
		ChromeHover: true,
	})
	if revBtn.Clicked() && p.OnRevealPath != nil {
		p.OnRevealPath(p.ActiveTab)
	}
}

func revealFileLabel() string {
	if theme.IsMacOS() {
		return theme.Tr("在访达中显示", "Reveal in Finder")
	}
	return theme.Tr("在文件管理器中显示", "Reveal in File Manager")
}

func renderTitlebarMenus(c *ui.Context, p TitlebarProps) {
	vpW, vpH := c.Size()

	ui.Overlay(c, func() {
		if p.MenuState == nil || (!p.MenuState.TabMenuOpen && !p.MenuState.PageMenuOpen) {
			return
		}
		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		if p.MenuState.PageMenuOpen {
			menuW := min(float32(216), max(vpW-16, 1))
			items := buildPageMenuItems(p)
			estH := estimateMenuHeight(items)
			if p.NoteStats != nil {
				estH += 2*theme.MenuItemHeight + 7 + 3*theme.MenuGap
			}
			anchor := p.MenuState.PageMenuAnchor
			if anchor.W <= 0 {
				anchor = ui.Rect{X: vpW - 40, Y: 6, W: theme.ChromeBoxSize, H: theme.ChromeBoxSize}
			}
			spec := blockui.PlaceBelow(anchor).WithAlignX(blockui.AlignX{Kind: blockui.AlignRight})
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 304)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("page-more-menu")
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			blockui.NavigateMenu(c, card, items, &p.MenuState.MenuCursor)
			blockui.Probe(card, "page-more-menu")
			card.Children(func() {
				blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						if p.NoteStats != nil {
							characters, blocks := p.NoteStats()
							renderPageStat(c, "page-stat-characters", theme.Tr("字符数", "Characters"), characters)
							renderPageStat(c, "page-stat-blocks", theme.Tr("内容块", "Blocks"), blocks)
							blockui.MenuRow(c, blockui.MenuItemSpec{Separator: true}, false)
						}
						for i, it := range items {
							blockui.MenuRow(c, it, i == p.MenuState.MenuCursor)
						}
					})
				})
			})
			if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.MenuState.Close()
			}
			return
		}

		if p.MenuState.TabMenuOpen {
			items := buildTabContextItems(p)
			if len(items) == 0 {
				p.MenuState.Close()
				return
			}
			menuW := min(float32(204), max(vpW-16, 1))
			estH := estimateMenuHeight(items)
			spec := blockui.PlaceAtPoint(p.MenuState.TabMenuX, p.MenuState.TabMenuY)
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 304)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("tab-context-menu")
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			blockui.NavigateMenu(c, card, items, &p.MenuState.MenuCursor)
			blockui.Probe(card, "tab-context-menu")
			card.Children(func() {
				blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						for i, it := range items {
							blockui.MenuRow(c, it, i == p.MenuState.MenuCursor)
						}
					})
				})
			})
			if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.MenuState.Close()
			}
		}
	})
}

func renderPageStat(c *ui.Context, id, label string, value int) {
	pal := theme.Ocean()
	row := ui.Row(c.Key(id)).FillWidth().Height(theme.MenuItemHeight).PaddingX(10).Gap(8).AlignItems(ui.Center)
	blockui.Probe(row, id)
	row.Children(func() {
		ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		ui.Text(c, strconv.Itoa(value)).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.Text).Shrink(0).SingleLine()
	})
}

func estimateMenuHeight(items []blockui.MenuItemSpec) float32 {
	h := theme.MenuPadding * 2
	for i, it := range items {
		if i > 0 {
			h += theme.MenuGap
		}
		if it.Separator {
			h += 7
		} else {
			h += theme.MenuItemHeight
		}
	}
	return h
}

// buildPageMenuItems mirrors thinkly-fast page-more-menu.
func buildPageMenuItems(p TitlebarProps) []blockui.MenuItemSpec {
	icHist := icons.History
	icImg := icons.Image
	icCopy := icons.Copy
	icReveal := icons.FolderReveal
	item := func(id, label, shortcut string, ic *icons.Icon, fn func()) blockui.MenuItemSpec {
		return blockui.MenuItemSpec{ID: id, Label: label, Shortcut: shortcut, Icon: ic, OnClick: func() {
			p.MenuState.Close()
			if fn != nil {
				fn()
			}
		}}
	}
	return []blockui.MenuItemSpec{
		item("page-history", theme.Tr("页面历史…", "Page History…"), "", &icHist, p.OnOpenHistory),
		item("page-export-image", theme.Tr("生成图片…", "Export as Image…"), blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutExport, "cmd-shift-e")), &icImg, p.OnOpenExport),
		item("page-copy-md", theme.Tr("复制 Markdown", "Copy Markdown"), blockui.FormatShortcutDisplay("alt-cmd-c"), &icCopy, p.OnCopyMarkdown),
		{Separator: true},
		item("page-reveal", revealFileLabel(), "", &icReveal, func() {
			if p.OnRevealPath != nil {
				p.OnRevealPath(p.ActiveTab)
			}
		}),
	}
}

func nonEmptyOr(val, fallback string) string {
	if strings.TrimSpace(val) != "" {
		return val
	}
	return fallback
}

func buildTabContextItems(p TitlebarProps) []blockui.MenuItemSpec {
	target := p.MenuState.TabMenuPath
	ix := -1
	for i, t := range p.Tabs {
		if t == target {
			ix = i
			break
		}
	}
	if ix < 0 {
		return nil
	}
	hasOthers := len(p.Tabs) > 1
	hasRight := ix+1 < len(p.Tabs)
	icClose := icons.TabClose
	icOthers := icons.TabCloseOthers
	icRight := icons.TabCloseRight
	icAll := icons.TabCloseAll
	icReveal := icons.FolderReveal
	return []blockui.MenuItemSpec{
		{
			ID:    "tab-menu-reveal",
			Label: revealFileLabel(),
			Icon:  &icReveal,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnRevealPath != nil {
					p.OnRevealPath(target)
				}
			},
		},
		{Separator: true},
		{
			ID:    "tab-menu-close",
			Label: theme.Tr("关闭", "Close"),
			Icon:  &icClose,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnCloseTab != nil {
					p.OnCloseTab(target)
				}
			},
		},
		{
			ID:       "tab-menu-close-others",
			Label:    theme.Tr("关闭其他", "Close Others"),
			Icon:     &icOthers,
			Disabled: !hasOthers,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnCloseOthers != nil {
					p.OnCloseOthers(target)
				}
			},
		},
		{
			ID:       "tab-menu-close-right",
			Label:    theme.Tr("关闭右侧", "Close to the Right"),
			Icon:     &icRight,
			Disabled: !hasRight,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnCloseRight != nil {
					p.OnCloseRight(target)
				}
			},
		},
		{Separator: true},
		{
			ID:     "tab-menu-close-all",
			Label:  theme.Tr("关闭全部", "Close All"),
			Icon:   &icAll,
			Danger: true,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnCloseAll != nil {
					p.OnCloseAll()
				}
			},
		},
	}
}
