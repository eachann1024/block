package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// TabMenuState tracks open titlebar floating menus (tab context menu or page "more" menu).
type TabMenuState struct {
	TabMenuOpen  bool
	TabMenuIndex int
	TabMenuPath  string
	TabMenuX     float32
	TabMenuY     float32

	PageMenuOpen   bool
	PageMenuAnchor ui.Rect

	// tabCenterW is the tab cell width the pills were last sized from.
	tabCenterW float32
}

// Close dismisses any open titlebar menu.
func (s *TabMenuState) Close() bool {
	had := s.TabMenuOpen || s.PageMenuOpen
	s.TabMenuOpen = false
	s.PageMenuOpen = false
	return had
}

// TitlebarProps supplies current state and callbacks to RenderTitlebar.
type TitlebarProps struct {
	SidebarWide       bool
	SidebarAutoHidden bool
	SidebarWidth      float32
	InSettings        bool
	SettingsTabLabel  string
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
	ShortcutNewTab  string
	ShortcutAI      string
	ShortcutExport  string

	// Annotation summary for the titlebar send segment (thinkly-fast annotation_summary).
	AnnotDrafts       int
	AnnotGenerating   bool
	AnnotHasAny       bool
	OnSendAnnotations func()

	MenuState *TabMenuState

	OnToggleSidebar   func()
	OnBack            func()
	OnForward         func()
	OnSelectTab       func(path string)
	OnCloseTab        func(path string)
	OnCloseOthers     func(keepPath string)
	OnCloseRight      func(pivotPath string)
	OnCloseAll        func()
	OnNewNote         func()
	OnToggleFeedback  func()
	OnToggleAI        func()
	OnOpenHistory     func()
	OnOpenExport      func()
	OnCopyMarkdown    func()
	OnRevealActive    func()
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

// RenderTitlebar renders the 40px Ocean Paper custom title bar (`Tabs-1-Titlebar`)
// plus any anchored tab context menu or page "More" menu.
func RenderTitlebar(c *ui.Context, p TitlebarProps) {
	pal := theme.Ocean()
	tb := c.TitleBar()
	leftLead := float32(10)
	if theme.IsMacOS() {
		leftLead = max(82, tb.Left+12)
	}

	showBack := !p.InSettings && p.CanBack
	showFwd := !p.InSettings && p.CanForward
	navCount := 0
	if showBack {
		navCount++
	}
	if showFwd {
		navCount++
	}
	leftW := leftLead + 8 + theme.ChromeBoxSize + float32(navCount)*(theme.ChromeBoxSize+2)
	if p.AIImmersive {
		leftW = leftLead
	} else if p.SidebarWide {
		leftW = max(p.SidebarWidth, leftW)
	}

	c.Key("titlebar")
	bar := ui.Row(c).FillWidth().Height(TitlebarHeight).Shrink(0).AlignItems(ui.Center).
		Background(pal.Shell).BorderWidth(0, 0, 1, 0).BorderColor(pal.SidebarBorder)
	blockui.Probe(bar, "titlebar")

	bar.Children(func() {
		// 1. Left cell: traffic-light clearance, optional Back/Forward, sidebar toggle.
		c.Key("titlebar-left")
		left := ui.Row(c).Width(leftW).FillHeight().Shrink(0).AlignItems(ui.Center).
			Padding(0, 8, 0, leftLead).DragWindow()
		if !p.AIImmersive {
			left.BorderWidth(0, 1, 0, 0).BorderColor(pal.SidebarBorder)
		}
		blockui.Probe(left, "titlebar-left")
		left.Children(func() {
			if p.AIImmersive {
				return
			}
			ui.Spacer(c)
			ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
				if showBack {
					backBtn := blockui.IconButton(c, icons.ChevronLeft, blockui.IconButtonOptions{
						ID:      "nav-back",
						Tooltip: theme.Tr("后退", "Back"),
					})
					if backBtn.Clicked() && p.OnBack != nil {
						p.OnBack()
					}
				}
				if showFwd {
					fwdBtn := blockui.IconButton(c, icons.ChevronRight, blockui.IconButtonOptions{
						ID:      "nav-forward",
						Tooltip: theme.Tr("前进", "Forward"),
					})
					if fwdBtn.Clicked() && p.OnForward != nil {
						p.OnForward()
					}
				}
				glyph := icons.SidebarExpand
				if p.SidebarWide {
					glyph = icons.SidebarCollapse
				}
				sbSc := p.ShortcutSidebar
				if sbSc == "" {
					sbSc = "alt-b"
				}
				tip := fmt.Sprintf("%s %s", theme.Tr("侧边栏", "Sidebar"), blockui.FormatShortcutDisplay(sbSc))
				if p.SidebarAutoHidden {
					if p.InSettings {
						tip = theme.Tr("窗口较窄，设置导航已让出。点击以浮层打开", "Settings navigation stepped aside. Click to open it over the page")
					} else {
						tip = theme.Tr("窗口较窄，侧栏已让出。点击打开，笔记保持可读宽度", "The sidebar stepped aside so the note stays readable. Click to open it")
					}
				}
				sbBtn := blockui.IconButton(c, glyph, blockui.IconButtonOptions{
					ID:      "sidebar-toggle",
					Tooltip: tip,
				})
				if sbBtn.Clicked() && p.OnToggleSidebar != nil {
					p.OnToggleSidebar()
				}
			})
		})

		// 2. Center cell: settings breadcrumb, empty title, or multi-tab strip.
		c.Key("titlebar-tabs")
		center := ui.Row(c).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Gap(4).PaddingX(10).Clip()
		blockui.Probe(center, "titlebar-tabs")
		centerW := center.Bounds().W
		center.Children(func() {
			if p.InSettings {
				crumb := ui.Row(c).Fill().AlignItems(ui.Center).Gap(6).PaddingX(6).DragWindow()
				crumb.Children(func() {
					ui.Text(c, theme.Tr("设置", "Settings")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					ui.Text(c, "/").FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					ui.Text(c, localizeSettingsTabLabel(p.SettingsTabLabel)).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
				})
				return
			}
			if len(p.Tabs) == 0 {
				title := "Block"
				if p.NotebookPath != "" {
					title = noteDisplayTitle(p.NotebookPath, true)
				}
				drag := ui.Row(c).Fill().AlignItems(ui.Center).DragWindow()
				drag.Children(func() {
					ui.Text(c, title).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
				})
				return
			}

			multi := len(p.Tabs) > 1
			renderTabStrip(c, p, centerW)

			if multi {
				newTabBtn := blockui.IconButton(c, icons.Plus, blockui.IconButtonOptions{
					ID:       "tab-new",
					Size:     theme.ChromeBoxSize,
					IconSize: 16,
					Tooltip:  fmt.Sprintf("%s %s", theme.Tr("新建标签页", "New Tab"), blockui.FormatShortcutDisplay("cmd-t")),
				})
				if newTabBtn.Clicked() && p.OnNewNote != nil {
					p.OnNewNote()
				}
			}

			ui.Box(c).Grow(1).MinWidth(16).FillHeight().DragWindow()
		})

		// 3. Right tail: contextual file tools + Feedback + AI Panel + Page More pill.
		if !p.InSettings && !p.AIImmersive {
			renderTitlebarTail(c, p, tb.Right)
		}
	})

	renderTitlebarMenus(c, p)
}

func renderNonNoteTitlebarTools(c *ui.Context, p TitlebarProps, surface TitlebarSurfaceKind) {
	pal := theme.Ocean()
	switch surface {
	case SurfaceImagePreview:
		renderZoomCluster(c, "tb-img-zoom", p)
		renderRevealButton(c, p)
	case SurfaceSVG:
		if p.SourceStatus != "" {
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
		if p.SourceStatus != "" {
			ui.Text(c, p.SourceStatus).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
		}
		icOpen := icons.LinkOpen
		openBtn := blockui.ActionButton(c, "html-browser", theme.Tr("浏览器打开", "Open in Browser"), &icOpen, blockui.ButtonSecondary, false)
		if openBtn.Clicked() && p.OnOpenInBrowser != nil {
			p.OnOpenInBrowser()
		}
		renderRevealButton(c, p)
	case SurfaceCode:
		if p.SourceStatus != "" {
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
		Radius(theme.RadiusControl).Background(pal.Card).Border(1, pal.BtnBorder)
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
		pctBtn := ui.ButtonBase(c).Height(22).PaddingX(6).Center().Radius(4).Cursor(ui.CursorPointer).Label(pct)
		if pctBtn.Hovered() {
			pctBtn.Background(pal.Hover)
		}
		pctBtn.Children(func() {
			ui.Text(c, pct).FontSize(14).TextColor(pal.Text).SingleLine()
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
		fitBtn := ui.ButtonBase(c).Height(22).PaddingX(6).Center().Radius(4).Border(1, ui.Transparent).Cursor(ui.CursorPointer).
			Label(theme.Tr("适应", "Fit"))
		if p.PreviewZoomFit {
			fitBtn.Background(pal.Selected).Border(1, pal.Accent)
		} else if fitBtn.Hovered() {
			fitBtn.Background(pal.Hover)
		}
		fitBtn.Children(func() {
			ui.Text(c, theme.Tr("适应", "Fit")).FontSize(14).TextColor(pal.Text).SingleLine()
		})
		blockui.Probe(fitBtn, id+"-fit")
		if fitBtn.Clicked() && p.OnPreviewFit != nil {
			p.OnPreviewFit()
		}
	})
}

func renderRevealButton(c *ui.Context, p TitlebarProps) {
	revBtn := blockui.IconButton(c, icons.FolderReveal, blockui.IconButtonOptions{
		ID:      "tb-reveal",
		Tooltip: theme.Tr("在访达中显示", "Reveal in File Manager"),
	})
	if revBtn.Clicked() && p.OnRevealActive != nil {
		p.OnRevealActive()
	}
}

func renderTitlebarMenus(c *ui.Context, p TitlebarProps) {
	vpW, vpH := c.Size()

	ui.Overlay(c, func() {
		if p.MenuState == nil || (!p.MenuState.TabMenuOpen && !p.MenuState.PageMenuOpen) {
			return
		}
		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		if p.MenuState.PageMenuOpen {
			menuW := float32(216)
			items := buildPageMenuItems(p)
			estH := estimateMenuHeight(items)
			anchor := p.MenuState.PageMenuAnchor
			if anchor.W <= 0 {
				anchor = ui.Rect{X: vpW - 40, Y: 6, W: theme.ChromeBoxSize, H: theme.ChromeBoxSize}
			}
			spec := blockui.PlaceBelow(anchor).WithAlignX(blockui.AlignX{Kind: blockui.AlignRight})
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 380)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("page-more-menu")
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			blockui.Probe(card, "page-more-menu")
			card.Children(func() {
				ui.Scroll(c).MaxHeight(maxH - theme.MenuPadding*2).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						for _, it := range items {
							blockui.MenuRow(c, it, false)
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
			menuW := float32(204)
			items := buildTabContextItems(p)
			estH := estimateMenuHeight(items)
			spec := blockui.PlaceAtPoint(p.MenuState.TabMenuX, p.MenuState.TabMenuY)
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 320)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("tab-context-menu")
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).Role(ui.RoleMenu))
			blockui.Probe(card, "tab-context-menu")
			card.Children(func() {
				for _, it := range items {
					blockui.MenuRow(c, it, false)
				}
			})
			if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.MenuState.Close()
			}
		}
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
		item("page-reveal", theme.Tr("在访达中显示", "Reveal in Finder"), "", &icReveal, p.OnRevealActive),
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
	ix := p.MenuState.TabMenuIndex
	for i, t := range p.Tabs {
		if t == target {
			ix = i
			break
		}
	}
	hasOthers := len(p.Tabs) > 1
	hasRight := ix >= 0 && ix+1 < len(p.Tabs)
	icClose := icons.TabClose
	icOthers := icons.TabCloseOthers
	icRight := icons.TabCloseRight
	icAll := icons.TabCloseAll
	return []blockui.MenuItemSpec{
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
