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

	CanBack    bool
	CanForward bool
	ZoomPct    int // 90, 100, 110

	PendingAnnotations int
	OnSendAnnotations  func()

	MenuState *TabMenuState

	OnToggleSidebar func()
	OnBack          func()
	OnForward       func()
	OnSelectTab     func(path string)
	OnCloseTab      func(path string)
	OnCloseOthers   func(keepPath string)
	OnCloseRight    func(pivotPath string)
	OnCloseAll      func()
	OnNewNote       func()
	OnToggleFeedback func()
	OnToggleAI      func()
	OnOpenHistory   func()
	OnOpenExport    func()
	OnCopyMarkdown  func()
	OnRevealActive  func()
	OnDeleteActive  func()
	OnSetZoom       func(pct int)
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

	navCount := 0
	if p.CanBack || p.CanForward {
		navCount = 2
	}
	leftW := leftLead + 8 + 28 + float32(navCount)*(28+2)
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
				if navCount > 0 {
					backBtn := blockui.IconButton(c, icons.ArrowLeft, blockui.IconButtonOptions{
						ID:       "nav-back",
						Tooltip:  fmt.Sprintf("%s %s", theme.Tr("后退", "Back"), blockui.FormatShortcutDisplay("cmd-[")),
						Disabled: !p.CanBack,
					})
					if backBtn.Clicked() && p.OnBack != nil {
						p.OnBack()
					}
					fwdBtn := blockui.IconButton(c, icons.ArrowRight, blockui.IconButtonOptions{
						ID:       "nav-forward",
						Tooltip:  fmt.Sprintf("%s %s", theme.Tr("前进", "Forward"), blockui.FormatShortcutDisplay("cmd-]")),
						Disabled: !p.CanForward,
					})
					if fwdBtn.Clicked() && p.OnForward != nil {
						p.OnForward()
					}
				}
				glyph := icons.SidebarExpand
				if p.SidebarWide {
					glyph = icons.SidebarCollapse
				}
				tip := fmt.Sprintf("%s %s", theme.Tr("侧边栏", "Sidebar"), blockui.FormatShortcutDisplay("alt-b"))
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
					Active:  p.SidebarAutoHidden && p.SidebarWide,
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
		center.Children(func() {
			if p.InSettings {
				crumb := ui.Row(c).Fill().AlignItems(ui.Center).Gap(6).DragWindow()
				crumb.Children(func() {
					ui.Text(c, theme.Tr("偏好设置", "Settings")).FontSize(14).FontWeight(500).TextColor(pal.TextSecondary).SingleLine()
					if p.SettingsTabLabel != "" {
						ui.Text(c, "/").FontSize(14).TextColor(pal.TextSecondary)
						ui.Text(c, localizeSettingsTabLabel(p.SettingsTabLabel)).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
					}
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
					ui.Text(c, title).FontSize(14).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				})
				return
			}

			multi := len(p.Tabs) > 1
			c.Key("titlebar-tab-strip")
			strip := ui.ScrollHorizontal(c).FillHeight().Shrink(1).MinWidth(0)
			blockui.Probe(strip, "titlebar-tab-strip")
			strip.Children(func() {
				ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(4).Children(func() {
					for ix, tabPath := range p.Tabs {
						tabPath := tabPath
						ix := ix
						active := !multi || tabPath == p.ActiveTab
						dirty := p.IsDirty != nil && p.IsDirty(tabPath)
						label := noteDisplayTitle(tabPath, false)
						if label == "" {
							label = theme.Tr("未命名", "Untitled")
						}
						tabID := fmt.Sprintf("tab-%d", ix)
						c.Key(tabID)
						maxW := float32(220)
						if !multi {
							maxW = 240
						}
						pill := ui.ButtonBase(c).Height(28).MinWidth(84).MaxWidth(maxW).Padding(0, 8, 0, 10).
							Gap(6).AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
							Role(ui.RoleTab).Selected(active).Label(label)
						hovered := pill.Hovered()
						switch {
						case active && multi:
							pill.Background(pal.Paper).Border(1, pal.Hairline)
						case hovered:
							pill.Background(pal.Hover)
						default:
							pill.Background(ui.Transparent)
						}
						var closeClicked bool
						pill.Children(func() {
							fg := pal.TextSecondary
							weight := 400
							if active {
								fg = pal.Text
								weight = 500
							}
							ui.Text(c, label).FontSize(14).FontWeight(weight).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							if multi {
								if hovered || active {
									closeID := fmt.Sprintf("tab-close-%d", ix)
									c.Key(closeID)
									cb := ui.ButtonBase(c).Size(18, 18).Shrink(0).Center().Radius(4).Cursor(ui.CursorPointer).
										Label(theme.Tr("关闭标签页", "Close Tab"))
									if cb.Hovered() {
										cb.Background(pal.Selected)
									}
									cb.Children(func() {
										icons.Draw(c, icons.Close, 13, pal.MonoIconColors(pal.TextSecondary))
									})
									blockui.Probe(cb, closeID)
									if cb.Clicked() {
										closeClicked = true
									}
								} else if dirty {
									ui.Box(c).Size(6, 6).Shrink(0).Radius(theme.RadiusPill).Background(pal.Accent)
								}
							} else if dirty {
								ui.Box(c).Size(6, 6).Shrink(0).Radius(theme.RadiusPill).Background(pal.Accent)
							}
						})
						blockui.Probe(pill, tabID)
						blockui.Probe(pill, "tab:"+tabPath)

						if closeClicked {
							if p.OnCloseTab != nil {
								p.OnCloseTab(tabPath)
							}
						} else if pill.RightClicked() && p.MenuState != nil {
							b := pill.Bounds()
							tx, ty := b.X+8, b.Y+b.H+4
							if px, py, ok := pill.PointerPosition(); ok {
								tx, ty = b.X+px, b.Y+py
							}
							p.MenuState.PageMenuOpen = false
							p.MenuState.TabMenuOpen = true
							p.MenuState.TabMenuIndex = ix
							p.MenuState.TabMenuPath = tabPath
							p.MenuState.TabMenuX = tx
							p.MenuState.TabMenuY = ty
						} else if pill.Clicked() && p.OnSelectTab != nil {
							p.OnSelectTab(tabPath)
						}
					}
				})
			})

			if multi {
				newTabBtn := blockui.IconButton(c, icons.Plus, blockui.IconButtonOptions{
					ID:      "tab-new",
					Size:    26,
					Tooltip: fmt.Sprintf("%s %s", theme.Tr("新建标签页", "New Tab"), blockui.FormatShortcutDisplay("cmd-t")),
				})
				if newTabBtn.Clicked() && p.OnNewNote != nil {
					p.OnNewNote()
				}
			}

			ui.Box(c).Grow(1).MinWidth(16).FillHeight().DragWindow()
		})

		// 3. Right tail: Feedback, AI Panel, Page More menu (hidden in Settings / AI Immersive).
		if !p.InSettings && !p.AIImmersive {
			tailPad := float32(10)
			if !theme.IsMacOS() && tb.Right > 0 {
				tailPad = tb.Right + 8
			}
			tail := ui.Row(c).FillHeight().Shrink(0).AlignItems(ui.Center).Gap(4).Padding(0, tailPad, 0, 0)
			tail.Children(func() {
				if p.PendingAnnotations > 0 && p.OnSendAnnotations != nil {
					lbl := fmt.Sprintf("%s (%d)", theme.Tr("统一发送", "Send all"), p.PendingAnnotations)
					sendBtn := blockui.ActionButton(c, "annotations-send-all", lbl, nil, blockui.ButtonPrimary, false)
					if sendBtn.Clicked() {
						p.OnSendAnnotations()
					}
				}

				fbBtn := blockui.IconButton(c, icons.MessageSquare, blockui.IconButtonOptions{
					ID:      "titlebar-feedback",
					Tooltip: theme.Tr("反馈与建议", "Feedback"),
					Active:  p.FeedbackOpen || p.HasFeedbackDraft,
				})
				if fbBtn.Clicked() && p.OnToggleFeedback != nil {
					p.OnToggleFeedback()
				}

				aiBtn := blockui.IconButton(c, icons.Sparkles, blockui.IconButtonOptions{
					ID:      "titlebar-ai",
					Tooltip: fmt.Sprintf("%s %s", theme.Tr("AI 助手", "AI Assistant"), blockui.FormatShortcutDisplay("cmd-j")),
					Active:  p.AIOpen,
				})
				if aiBtn.Clicked() && p.OnToggleAI != nil {
					p.OnToggleAI()
				}

				if len(p.Tabs) > 0 {
					moreOpen := p.MenuState != nil && p.MenuState.PageMenuOpen
					moreBtn := blockui.IconButton(c, icons.More, blockui.IconButtonOptions{
						ID:      "titlebar-more",
						Tooltip: theme.Tr("页面操作", "Page Actions"),
						Active:  moreOpen,
					})
					bounds := moreBtn.Bounds()
					if p.MenuState != nil && bounds.W > 0 {
						p.MenuState.PageMenuAnchor = bounds
					}
					if moreBtn.Clicked() && p.MenuState != nil {
						p.MenuState.TabMenuOpen = false
						p.MenuState.PageMenuOpen = !p.MenuState.PageMenuOpen
					}
				}
			})
		}
	})

	renderTitlebarMenus(c, p)
}

func renderTitlebarMenus(c *ui.Context, p TitlebarProps) {
	if p.MenuState == nil || (!p.MenuState.TabMenuOpen && !p.MenuState.PageMenuOpen) {
		return
	}
	pal := theme.Ocean()
	vpW, vpH := c.Size()

	ui.Overlay(c, func() {
		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		if p.MenuState.PageMenuOpen {
			menuW := float32(228)
			items := buildPageMenuItems(p)
			estH := estimateMenuHeight(items)
			anchor := p.MenuState.PageMenuAnchor
			if anchor.W <= 0 {
				anchor = ui.Rect{X: vpW - 40, Y: 6, W: 28, H: 28}
			}
			spec := blockui.PlaceBelow(anchor).WithAlignX(blockui.AlignX{Kind: blockui.AlignRight})
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 380)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("page-more-menu")
			card := ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
				Padding(6).Radius(theme.RadiusSurface).Background(pal.Paper).Border(1, pal.Hairline).
				Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.16)).Role(ui.RoleMenu)
			blockui.Probe(card, "page-more-menu")
			card.Children(func() {
				ui.Scroll(c).MaxHeight(maxH - 12).Children(func() {
					for _, it := range items {
						blockui.MenuRow(c, it, false)
					}
				})
			})
			if card.PressedOutside() || scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.MenuState.Close()
			}
			return
		}

		if p.MenuState.TabMenuOpen {
			menuW := float32(208)
			items := buildTabContextItems(p)
			estH := estimateMenuHeight(items)
			spec := blockui.PlaceAtPoint(p.MenuState.TabMenuX, p.MenuState.TabMenuY)
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 320)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("tab-context-menu")
			card := ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
				Padding(6).Radius(theme.RadiusSurface).Background(pal.Paper).Border(1, pal.Hairline).
				Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.16)).Role(ui.RoleMenu)
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
	h := float32(12)
	for _, it := range items {
		if it.Separator {
			h += 9
		} else {
			h += 32
		}
	}
	return h
}

func buildPageMenuItems(p TitlebarProps) []blockui.MenuItemSpec {
	icHist := icons.History
	icImg := icons.Image
	icCopy := icons.Copy
	icReveal := icons.FolderReveal
	icTrash := icons.Trash
	zoom := p.ZoomPct
	if zoom == 0 {
		zoom = 100
	}
	return []blockui.MenuItemSpec{
		{
			ID:    "page-history",
			Label: theme.Tr("页面历史…", "Page History…"),
			Icon:  &icHist,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnOpenHistory != nil {
					p.OnOpenHistory()
				}
			},
		},
		{
			ID:       "page-export-image",
			Label:    theme.Tr("导出与生成图片…", "Export & Image…"),
			Shortcut: blockui.FormatShortcutDisplay("cmd-shift-e"),
			Icon:     &icImg,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnOpenExport != nil {
					p.OnOpenExport()
				}
			},
		},
		{
			ID:       "page-copy-md",
			Label:    theme.Tr("复制 Markdown", "Copy Markdown"),
			Shortcut: blockui.FormatShortcutDisplay("alt-cmd-c"),
			Icon:     &icCopy,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnCopyMarkdown != nil {
					p.OnCopyMarkdown()
				}
			},
		},
		{Separator: true},
		{
			ID:      "page-zoom-90",
			Label:   theme.Tr("缩放 90%", "Zoom 90%"),
			Checked: zoom == 90,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnSetZoom != nil {
					p.OnSetZoom(90)
				}
			},
		},
		{
			ID:      "page-zoom-100",
			Label:   theme.Tr("缩放 100%", "Zoom 100%"),
			Checked: zoom == 100,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnSetZoom != nil {
					p.OnSetZoom(100)
				}
			},
		},
		{
			ID:      "page-zoom-110",
			Label:   theme.Tr("缩放 110%", "Zoom 110%"),
			Checked: zoom == 110,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnSetZoom != nil {
					p.OnSetZoom(110)
				}
			},
		},
		{Separator: true},
		{
			ID:    "page-reveal",
			Label: theme.Tr("在访达中显示", "Reveal in File Manager"),
			Icon:  &icReveal,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnRevealActive != nil {
					p.OnRevealActive()
				}
			},
		},
		{
			ID:     "page-delete",
			Label:  theme.Tr("移到废纸篓", "Move to Trash"),
			Icon:   &icTrash,
			Danger: true,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnDeleteActive != nil {
					p.OnDeleteActive()
				}
			},
		},
	}
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
