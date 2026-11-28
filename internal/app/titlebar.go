package app

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// TabMenuState tracks open titlebar floating menus (tab context menu or page "more" menu),
// plus horizontal tab strip scroll, middle-click close, and drag-reorder state.
type TabMenuState struct {
	MenuCursor       int
	MenuCursorID     string
	MenuRevealCursor bool
	// Persistent trigger handles bound to live titlebar elements so Escape restores
	// focus without registering new handles each frame.
	MenuTrigger           *ui.Handle
	BrandTrigger          ui.Handle
	PluginOverflowTrigger ui.Handle
	PageMoreTrigger       ui.Handle
	PluginCtxTrigger      ui.Handle
	TabCtxTrigger         ui.Handle

	TabMenuOpen  bool
	TabMenuIndex int
	TabMenuPath  string
	TabMenuX     float32
	TabMenuY     float32

	PageMenuOpen   bool
	PageMenuAnchor ui.Rect

	// Brand menu (cat mark) and the overflow / context menus for plugin titlebar buttons.
	BrandMenuOpen     bool
	BrandMenuAnchor   ui.Rect
	PluginMenuOpen    bool
	PluginMenuAnchor  ui.Rect
	HoverPluginSlotID string
	PluginCtxOpen     bool
	PluginCtxSlotID   string
	PluginCtxAnchor   ui.Rect

	// tabCenterW is the tab cell width the pills were last sized from (-1: not laid out yet).
	tabCenterW float32
	// plugin*: last laid-out center cell width and plugin zone, so inline plugin
	// buttons fill real free space instead of fixed window breakpoints.
	pluginCenterW    float32
	pluginZoneW      float32
	pluginInlineN    int
	pluginLimitReady bool
	// tabsNeedW is the tab cell width that shows every multi-tab title in full;
	// TitlebarNeedW adds the tail with all plugins folded. When the docked sidebar
	// leaves less than that, the sidebar yields before tabs get squeezed.
	tabsNeedW       float32
	TitlebarNeedW   float32
	lastViewportW   float32
	revealActiveTab bool

	TabScroll       ui.ScrollState
	LastActiveTab   string
	LastTabCount    int
	DragTargetPath  string
	DragAfter       bool
	TabDragGate     blockui.DragGate
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
	had := s.anyOpen()
	s.TabMenuOpen = false
	s.PageMenuOpen = false
	s.BrandMenuOpen = false
	s.PluginMenuOpen = false
	s.HoverPluginSlotID = ""
	s.PluginCtxOpen = false
	s.PluginCtxSlotID = ""
	s.MenuCursor = -1
	s.MenuCursorID = ""
	s.MenuRevealCursor = false
	s.MenuTrigger = nil
	return had
}

// CloseAndRestoreFocus dismisses any open titlebar menu (e.g. via Escape) and returns
// keyboard focus to the live titlebar trigger button that opened it.
func (s *TabMenuState) CloseAndRestoreFocus(c *ui.Context) bool {
	trigger := s.MenuTrigger
	had := s.Close()
	if had && trigger != nil {
		trigger.Focus(c)
	}
	return had
}

func (s *TabMenuState) anyOpen() bool {
	return s.TabMenuOpen || s.PageMenuOpen || s.BrandMenuOpen || s.PluginMenuOpen || s.PluginCtxOpen
}

// navigateTitlebarMenu reconciles the keyboard cursor against stable MenuItemSpec IDs
// before running NavigateMenu, so dynamic item insertions or reordering (e.g. window
// resize across breakpoints or plugin pinning) never shift Enter onto a different action.
// Tab / Shift+Tab are also consumed at the menu layer so focus cannot drift onto a
// different row button behind MenuCursor.
func navigateTitlebarMenu(c *ui.Context, card ui.Element, items []blockui.MenuItemSpec, s *TabMenuState) {
	if s == nil {
		return
	}
	_ = card.OverlayShortcut(0, ui.KeyTab)
	_ = card.OverlayShortcut(ui.Shift, ui.KeyTab)
	s.MenuRevealCursor = false
	// If pointer movement/scroll cleared MenuCursor to -1 since the last frame,
	// drop the remembered ID too so hover stays in control.
	if s.MenuCursor < 0 {
		s.MenuCursorID = ""
	}
	prevCursor := s.MenuCursor
	if s.MenuCursorID != "" {
		found := -1
		for i, it := range items {
			if it.ID == s.MenuCursorID && !it.Separator && !it.Disabled {
				found = i
				break
			}
		}
		s.MenuCursor = found
		if found < 0 {
			s.MenuCursorID = ""
		} else if found != prevCursor {
			s.MenuRevealCursor = true
		}
	}
	blockui.NavigateMenu(c, card, items, &s.MenuCursor)
	if s.MenuCursor >= 0 && s.MenuCursor < len(items) && items[s.MenuCursor].ID != "" {
		s.MenuCursorID = items[s.MenuCursor].ID
	} else {
		s.MenuCursorID = ""
	}
}

// TitlebarProps supplies current state and callbacks to RenderTitlebar.
type TitlebarProps struct {
	// DeferMenus, when set, queues the menu overlays so they are built after
	// the editor and stack above its floating toolbars.
	DeferMenus             func(fn func())
	SidebarWide            bool
	SidebarAutoHidden      bool
	SidebarWidth           float32
	InSettings             bool
	SettingsTabLabel       string
	RenderSettingsLeading  func(c *ui.Context) bool
	RenderSettingsTrailing func(c *ui.Context)
	RenderSettingsOverlays func(c *ui.Context)
	HideMark               bool
	AIImmersive            bool
	AIEnabled              bool
	AIOpen                 bool
	FeedbackOpen           bool
	HasFeedbackDraft       bool

	NotebookPath               string
	NotebookIcon               *plugins.ResolvedFileIcon
	NotebookIconPicker         bool
	NotebookIconPickerActive   bool
	OnClickNotebookIcon        func(iconRect, rowRect ui.Rect)
	OnUpdateNotebookIconBounds func(iconRect, rowRect ui.Rect)
	Tabs                       []string
	ActiveTab                  string
	IsDirty                    func(path string) bool
	FileIcons                  map[string]plugins.ResolvedFileIcon

	ActiveSurface   TitlebarSurfaceKind
	SourceStatus    string
	PreviewZoomPct  string
	PreviewZoomFit  bool
	SVGShowSource   bool
	NoteSourceView  bool
	SourceWrap      bool
	HasCopyableText bool
	CanFormatJSON   bool

	CanBack    bool
	CanForward bool

	ShortcutSidebar    string
	SidebarFloating    bool
	SidebarExpandCueAt time.Time
	ShortcutNewTab     string
	ShortcutAI         string
	ShortcutExport     string

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

	OnToggleSidebar    func()
	OnCloseSettings    func()
	OnBack             func()
	OnForward          func()
	OnSelectTab        func(path string)
	OnCloseTab         func(path string)
	OnCloseOthers      func(keepPath string)
	OnCloseRight       func(pivotPath string)
	OnCloseAll         func()
	OnReorderTab       func(srcPath, targetPath string, after bool)
	OnNewNote          func()
	OnToggleFeedback   func()
	OnToggleAI         func()
	OnOpenHistory      func()
	OnOpenExport       func()
	OnCopyMarkdown     func()
	OnToggleNoteSource func()
	OnCopyContent      func()
	OnFormatJSON       func()
	OnCopyImage        func()
	OnOpenExternal     func()
	OnRevealPath       func(path string)
	NoteStats          func() (characters, blocks int)
	PageStats          func() []PageMenuStat
	OnPreviewZoomStep  func(factor float32)
	OnPreviewActual    func()
	OnPreviewFit       func()
	OnSelectSVGSource  func(showSource bool)
	OnToggleSourceWrap func()
	OnOpenInBrowser    func()

	HiddenSlots       map[string]bool
	PluginButtons     []plugins.ResolvedTitlebarButton
	PinnedPluginSlots []string
	OnTogglePinPlugin func(slotID string) bool
	OnPluginButton    func(plugins.ResolvedTitlebarButton)
	PluginPageMenu    []plugins.ResolvedMenuItem
	OnPluginMenuItem  func(plugins.ResolvedMenuItem)
	PluginTabMenu     []plugins.ResolvedMenuItem
	OnPluginTabMenu   func(item plugins.ResolvedMenuItem, path string)

	// Brand mark: version shown in its tooltip and menu, plus the update hint it carries.
	AppVersion        string
	HasUpdate         bool
	UpdateChecking    bool
	UpdateTooltip     string
	UpdateMenuLabel   string
	UpdateHint        string
	OnOpenUpdater     func()
	OnOpenPluginStore func()
	OnOpenAbout       func()
	OnHideMark        func()
}

func slotHidden(p TitlebarProps, slotID string) bool {
	if !plugins.IsManageableTitlebarSlot(slotID) || p.HiddenSlots == nil {
		return false
	}
	return p.HiddenSlots[slotID]
}

func noteDisplayTitle(p string, isDir bool) string {
	if p == "" {
		return ""
	}
	base := filepath.Base(p)
	if !isDir {
		return files.DisplayName(base)
	}
	return base
}

func localizeSettingsTabLabel(tab string) string {
	switch tab {
	case "ai":
		return theme.Tr("AI Agent", "AI Agent")
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
	if blockui.MacTrafficLightClear() {
		leftLead = max(80, tb.Left+12)
	}

	leftW := leftLead + theme.ChromeBoxSize + 6
	if !p.HideMark {
		leftW += brandMarkGap + blockui.AppMarkButtonWidth
	}
	if p.AIImmersive {
		leftW = leftLead
	} else if p.SidebarWide {
		leftW = max(p.SidebarWidth, SidebarMinW)
	}
	vpW, _ := c.Size()
	availW := titlebarAvailableWidth(vpW, leftW, tb.Right)
	viewportResized := false
	if m := p.MenuState; m != nil && vpW > 0 {
		if m.lastViewportW > 0 && vpW != m.lastViewportW {
			viewportResized = true
			if vpW < m.lastViewportW {
				m.revealActiveTab = true
			}
			// Request one post-layout frame so updated button anchors and centerW settle.
			c.Invalidate()
		}
		m.lastViewportW = vpW
	}

	c.Key("titlebar")
	bar := ui.Row(c).FillWidth().Height(TitlebarHeight).Shrink(0).AlignItems(ui.Center)
	blockui.Probe(bar, "titlebar")

	bar.Children(func() {
		// 1. Left cell: traffic-light clearance, fixed sidebar toggle, brand wordmark, and Back/Forward.
		c.Key("titlebar-left")
		left := ui.Row(c).FillHeight().Shrink(0).AlignItems(ui.Center).
			Padding(0, 6, 0, leftLead).DragWindow()
		if p.AIImmersive {
			left.Width(leftW)
		} else if p.SidebarWide {
			left.MinWidth(leftW)
		}
		blockui.Probe(left, "titlebar-left")
		left.Children(func() {
			if p.AIImmersive {
				return
			}
			renderSidebarToggleButton(c, p)

			if !p.HideMark {
				renderBrandMark(c, p)
			} else if p.HasUpdate {
				renderHiddenMarkUpdateButton(c, p)
			}
			if !p.SidebarWide {
				return
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
						Disabled:    !p.CanBack,
						ChromeHover: true,
					})
					if backBtn.Clicked() && p.CanBack && p.OnBack != nil {
						p.OnBack()
					}
					fwdBtn := blockui.IconButton(c, icons.ArrowRight, blockui.IconButtonOptions{
						ID:          "nav-forward",
						Size:        26,
						IconSize:    16,
						Tooltip:     theme.Tr("前进", "Forward"),
						Shortcut:    blockui.FormatShortcutDisplay("cmd-]"),
						Disabled:    !p.CanForward,
						ChromeHover: true,
					})
					if fwdBtn.Clicked() && p.CanForward && p.OnForward != nil {
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
		if p.MenuState != nil {
			p.MenuState.pluginCenterW = centerW
		}
		center.Children(func() {
			if p.InSettings {
				if p.RenderSettingsLeading != nil && p.RenderSettingsLeading(c) {
					return
				}
				crumb := ui.Row(c).Fill().MinWidth(0).AlignItems(ui.Center).Gap(4).PaddingX(6).DragWindow()
				crumb.Children(func() {
					ui.Text(c, theme.Tr("设置", "Settings")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
					ui.Text(c, "/").FontSize(theme.FontBody).TextColor(pal.Placeholder).SingleLine().Shrink(0)
					ui.Text(c, localizeSettingsTabLabel(p.SettingsTabLabel)).FontSize(theme.FontBody).FontWeight(theme.WeightRegular).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
				return
			}
			if len(p.Tabs) == 0 {
				dragGap := float32(7)
				if p.NotebookPath != "" && p.NotebookIconPicker && p.OnClickNotebookIcon != nil {
					dragGap = 5
				}
				drag := ui.Row(c).Fill().MinWidth(0).AlignItems(ui.Center).Gap(dragGap).PaddingX(6).DragWindow()
				drag.Children(func() {
					if p.NotebookPath != "" {
						nbTitle := noteDisplayTitle(p.NotebookPath, true)
						renderTitlebarNotebookIcon(c, p, nbTitle, drag)
						ui.Text(c, nbTitle).FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					} else {
						ui.Text(c, "Block").FontSize(theme.FontBody).LineHeight(20.0 / theme.FontBody).TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					}
				})
				return
			}

			multi := len(p.Tabs) > 1
			renderTabStrip(c, p, centerW, availW, viewportResized)

			if multi {
				newTabBtn := blockui.IconButton(c, icons.Plus, blockui.IconButtonOptions{
					ID:          "tab-new",
					Size:        theme.ChromeBoxSize,
					IconSize:    16,
					Tooltip:     theme.Tr("新建标签页", "New Tab"),
					Shortcut:    blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutNewTab, "cmd-t")),
					ChromeHover: true,
				})
				if newTabBtn.Clicked() && p.OnNewNote != nil {
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

		// 3. Right tail: Settings plugin/store controls + close button (when docked sidebar's
		// "返回" is hidden), or contextual file tools + Feedback + AI Panel + Page More pill.
		if p.InSettings {
			pad := float32(titlebarActionTrailing)
			if !theme.IsMacOS() {
				pad = 0
				if tb.Right > 0 {
					pad = tb.Right + 8
				}
			}
			ui.Row(c).FillHeight().Shrink(0).AlignItems(ui.Center).Gap(theme.Space8).Padding(0, pad, 0, 0).Children(func() {
				if p.RenderSettingsTrailing != nil {
					p.RenderSettingsTrailing(c)
				}
				if !p.SidebarWide {
					closeBtn := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
						ID:          "settings-back",
						Label:       theme.Tr("关闭设置", "Close Settings"),
						Tooltip:     theme.Tr("关闭设置", "Close Settings"),
						ChromeHover: true,
					})
					if closeBtn.Clicked() && p.OnCloseSettings != nil {
						p.OnCloseSettings()
					}
				}
			})
		} else if !p.AIImmersive {
			renderTitlebarTail(c, p, tb.Right, availW)
		}
	})

	if p.DeferMenus != nil {
		p.DeferMenus(func() { renderTitlebarMenus(c, p, availW) })
		return
	}
	renderTitlebarMenus(c, p, availW)
}

func renderSourceStatusText(c *ui.Context, status string, isError bool) {
	pal := theme.Ocean()
	col := pal.TextSecondary
	if isError {
		col = pal.Danger
	}
	txt := ui.Text(c, status).FontSize(theme.FontMeta).TextColor(col).MaxWidth(140).SingleLine().Ellipsis("…")
	if isError {
		blockui.RichTooltip(c, txt, blockui.TooltipSpec{
			Danger:    true,
			Placement: blockui.TooltipBelow,
			Rows: []blockui.TooltipRow{
				{Trigger: theme.Tr("保存失败", "Save failed"), TriggerTone: blockui.TooltipToneDanger},
				{Detail: status},
			},
		})
	}
}

func renderNonNoteTitlebarChrome(c *ui.Context, p TitlebarProps, surface TitlebarSurfaceKind, availW float32) {
	vpW, _ := c.Size()
	isReadOnlyHint := p.SourceStatus == theme.Tr("只读预览", "Read-only")
	isSaveError := p.SourceStatus != "" && !isReadOnlyHint
	showStatus := isSaveError || (p.SourceStatus != "" && (vpW <= 0 || (vpW >= 860 && (availW <= 0 || availW >= 620))))
	switch surface {
	case SurfaceSVG:
		if showStatus {
			renderSourceStatusText(c, p.SourceStatus, isSaveError)
		}
		renderOpenInBrowserButton(c, p, availW)
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
	case SurfaceHTML:
		if showStatus {
			renderSourceStatusText(c, p.SourceStatus, isSaveError)
		}
		renderOpenInBrowserButton(c, p, availW)
	case SurfaceCode:
		if showStatus {
			renderSourceStatusText(c, p.SourceStatus, isSaveError)
		}
	}
}

func renderOpenInBrowserButton(c *ui.Context, p TitlebarProps, availW float32) {
	vpW, _ := c.Size()
	if isNarrowTitlebar(vpW, availW) {
		return
	}
	openBtn := blockui.IconButton(c, icons.LinkOpen, blockui.IconButtonOptions{
		ID:          "html-browser",
		Tooltip:     theme.Tr("在浏览器中打开", "Open in Browser"),
		ChromeHover: true,
	})
	if openBtn.Clicked() && p.OnOpenInBrowser != nil {
		p.OnOpenInBrowser()
	}
}

func renderSourceWrapButton(c *ui.Context, p TitlebarProps, availW float32) {
	vpW, _ := c.Size()
	if slotHidden(p, plugins.TitlebarSlotWrap) || isNarrowTitlebar(vpW, availW) {
		return
	}
	def, _ := plugins.BuiltinTitlebarSlotDef(plugins.TitlebarSlotWrap)
	btn := blockui.IconButton(c, def.Icon, blockui.IconButtonOptions{
		ID:          "tb-source-wrap",
		Tooltip:     def.UIText(),
		ChromeHover: true,
		Active:      p.SourceWrap,
	})
	if btn.Clicked() && p.OnToggleSourceWrap != nil {
		p.OnToggleSourceWrap()
	}
}

func renderZoomCluster(c *ui.Context, id string, p TitlebarProps, availW float32) {
	if slotHidden(p, plugins.TitlebarSlotZoom) || isTightTitlebar(availW) {
		return
	}
	pal := theme.Ocean()
	c.Key(id)
	cluster := ui.Row(c).Height(28).PaddingX(4).Gap(2).AlignItems(ui.Center).
		Radius(theme.RadiusMD).Background(pal.Card).Border(1, pal.BtnBorder)
	blockui.Probe(cluster, id)
	cluster.Children(func() {
		for _, part := range plugins.TitlebarSlotControls(plugins.TitlebarSlotZoom) {
			tip := part.UIText()
			switch part.ID {
			case "out":
				outBtn := blockui.IconButton(c, part.Icon, blockui.IconButtonOptions{
					ID:       id + "-out",
					Size:     22,
					IconSize: 13,
					Tooltip:  tip,
					Shortcut: blockui.FormatShortcutDisplay(part.Shortcut),
				})
				if outBtn.Clicked() && p.OnPreviewZoomStep != nil {
					p.OnPreviewZoomStep(1.0 / 1.2)
				}
			case "pct":
				pct := p.PreviewZoomPct
				if pct == "" {
					pct = tip
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
			case "in":
				inBtn := blockui.IconButton(c, part.Icon, blockui.IconButtonOptions{
					ID:       id + "-in",
					Size:     22,
					IconSize: 13,
					Tooltip:  tip,
					Shortcut: blockui.FormatShortcutDisplay(part.Shortcut),
				})
				if inBtn.Clicked() && p.OnPreviewZoomStep != nil {
					p.OnPreviewZoomStep(1.2)
				}
			case "fit":
				c.Key(id + "-fit")
				fitBtn := ui.ButtonBase(c).Height(22).PaddingX(6).Center().Radius(theme.RadiusXS).Border(1, ui.Transparent).Cursor(ui.CursorPointer).
					Label(tip)
				if p.PreviewZoomFit {
					fitBtn.Background(pal.Selected).Border(1, pal.SelectedBorder)
				} else if fitBtn.Hovered() {
					fitBtn.Background(pal.Hover)
				}
				fitBtn.Children(func() {
					ui.Text(c, tip).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
				})
				blockui.Probe(fitBtn, id+"-fit")
				if fitBtn.Clicked() && p.OnPreviewFit != nil {
					p.OnPreviewFit()
				}
			}
		}
	})
}

func revealFileLabel() string {
	if theme.IsMacOS() {
		return theme.Tr("在访达中显示", "Reveal in Finder")
	}
	return theme.Tr("在文件管理器中显示", "Reveal in File Manager")
}

func resolveTitlebarSurface(p TitlebarProps) TitlebarSurfaceKind {
	if len(p.Tabs) == 0 {
		return SurfaceEmpty
	}
	if p.ActiveSurface == SurfaceEmpty {
		return SurfaceNote
	}
	return p.ActiveSurface
}

func resolvePageMenuStats(p TitlebarProps, surface TitlebarSurfaceKind) []PageMenuStat {
	if surface == SurfaceNote {
		if p.NoteStats == nil {
			return nil
		}
		characters, blocks := p.NoteStats()
		return []PageMenuStat{
			{ID: "page-stat-characters", Label: theme.Tr("字符数", "Characters"), Value: strconv.Itoa(characters)},
			{ID: "page-stat-blocks", Label: theme.Tr("内容块", "Blocks"), Value: strconv.Itoa(blocks)},
		}
	}
	if p.PageStats == nil {
		return nil
	}
	return p.PageStats()
}

func renderTitlebarMenus(c *ui.Context, p TitlebarProps, availW float32) {
	vpW, vpH := c.Size()

	ui.Overlay(c, func() {
		if p.InSettings && p.RenderSettingsOverlays != nil {
			p.RenderSettingsOverlays(c)
		}
		if p.MenuState == nil || !p.MenuState.anyOpen() {
			return
		}
		if p.HideMark && p.MenuState.BrandMenuOpen {
			p.MenuState.Close()
			return
		}
		if p.InSettings && (p.MenuState.PluginMenuOpen || p.MenuState.PluginCtxOpen || p.MenuState.PageMenuOpen || p.MenuState.TabMenuOpen) {
			p.MenuState.Close()
			return
		}
		scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		if p.MenuState.BrandMenuOpen {
			renderBrandMenu(c, p, scrim)
			return
		}
		if p.MenuState.PluginMenuOpen {
			renderPluginOverflowMenu(c, p, scrim, availW)
			return
		}
		if p.MenuState.PluginCtxOpen {
			renderPluginContextMenu(c, p, scrim, availW)
			return
		}
		if p.MenuState.PageMenuOpen {
			surface := resolveTitlebarSurface(p)
			if surface == SurfaceEmpty {
				p.MenuState.Close()
				return
			}
			menuW := min(float32(232), max(vpW-16, 1))
			stats := resolvePageMenuStats(p, surface)
			items, legacyProbeIDs := buildPageMenuItems(p, surface, vpW, availW)
			estH := estimateMenuHeight(items)
			if len(stats) > 0 {
				estH += float32(len(stats))*theme.MenuItemHeight + 7 + float32(len(stats)+1)*theme.MenuGap
			}
			anchor := p.MenuState.PageMenuAnchor
			if anchor.W <= 0 {
				anchor = ui.Rect{X: vpW - 40, Y: 6, W: theme.ChromeBoxSize, H: theme.ChromeBoxSize}
			}
			spec := blockui.PlaceBelow(anchor).WithAlignX(blockui.AlignX{Kind: blockui.AlignRight})
			maxH := blockui.MaxHeightForSpec(spec, vpW, vpH, 360)
			popH := min(estH, maxH)
			ox, oy := blockui.ResolveOrigin(spec, menuW, popH, vpW, vpH)

			c.Key("page-more-menu")
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
				Focusable().FocusRing(false).AutoFocus().Role(ui.RoleMenu))
			navigateTitlebarMenu(c, card, items, p.MenuState)
			blockui.Probe(card, "page-more-menu")
			card.Children(func() {
				blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						if len(stats) > 0 {
							for _, st := range stats {
								renderPageStat(c, st)
							}
							blockui.MenuRow(c, blockui.MenuItemSpec{Separator: true}, false)
						}
						for i, it := range items {
							row := blockui.MenuRow(c, it, i == p.MenuState.MenuCursor)
							if i == p.MenuState.MenuCursor && p.MenuState.MenuRevealCursor {
								blockui.RevealActiveItem(row)
							}
							if blockui.IsProbeEnabled() && legacyProbeIDs[it.ID] != "" {
								id, legacyID := it.ID, legacyProbeIDs[it.ID]
								row.DrawOver(func(_ *ui.Painter, r ui.Rect) {
									blockui.RecordBounds(id, r)
									blockui.RecordBounds(legacyID, r)
								})
							}
						}
					})
				})
			})
			if scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.MenuState.CloseAndRestoreFocus(c)
			} else if card.PressedOutside() {
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
			card := blockui.MenuSurface(ui.Column(c).Absolute().Left(ox).Top(oy).Width(menuW).MaxHeight(maxH).
				Focusable().FocusRing(false).AutoFocus().Role(ui.RoleMenu))
			navigateTitlebarMenu(c, card, items, p.MenuState)
			blockui.Probe(card, "tab-context-menu")
			card.Children(func() {
				blockui.Scroll(c).FillWidth().MaxHeight(blockui.MenuInnerMaxHeight(maxH)).Children(func() {
					ui.Column(c).FillWidth().Gap(theme.MenuGap).Children(func() {
						for i, it := range items {
							row := blockui.MenuRow(c, it, i == p.MenuState.MenuCursor)
							if i == p.MenuState.MenuCursor && p.MenuState.MenuRevealCursor {
								blockui.RevealActiveItem(row)
							}
						}
					})
				})
			})
			if scrim.OverlayShortcut(0, ui.KeyEscape) {
				p.MenuState.CloseAndRestoreFocus(c)
			} else if card.PressedOutside() {
				p.MenuState.Close()
			}
		}
	})
	if p.MenuState != nil && p.MenuState.anyOpen() {
		blockui.ReserveMenuNavKeys(c)
		c.Shortcut(0, ui.KeyTab)
		c.Shortcut(ui.Shift, ui.KeyTab)
		if p.MenuState.PluginMenuOpen {
			c.Shortcut(0, ui.KeyP)
		}
	}
}

func renderPageStat(c *ui.Context, st PageMenuStat) {
	pal := theme.Ocean()
	row := ui.Row(c.Key(st.ID)).FillWidth().Height(theme.MenuItemHeight).PaddingX(theme.Space10).Gap(theme.Space8).AlignItems(ui.Center)
	blockui.Probe(row, st.ID)
	row.Children(func() {
		ui.Text(c, st.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.TextSecondary).Grow(1).MinWidth(36).SingleLine().Ellipsis("…")
		var badges []string
		for _, b := range st.Badges {
			if b != "" {
				badges = append(badges, b)
			}
		}
		if len(badges) > 0 {
			ui.Row(c).Gap(theme.Space4).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				for _, b := range badges {
					blockui.MetaBadge(c, b)
				}
			})
			return
		}
		ui.Text(c, st.Value).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
			TextColor(pal.Text).MinWidth(0).SingleLine().Ellipsis("…")
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

// buildPageMenuItems builds the titlebar "more" menu tailored to the active surface.
// It also returns any legacy smoke-probe aliases for collision-free plugin item IDs.
func buildPageMenuItems(p TitlebarProps, surface TitlebarSurfaceKind, vpW, availW float32) ([]blockui.MenuItemSpec, map[string]string) {
	icHist := icons.History
	icImg := icons.Image
	icCopy := icons.Copy
	icCode := icons.Code
	icFormat := icons.Sparkles
	icOpen := icons.LinkOpen
	icReveal := icons.FolderReveal
	icWrap := icons.Wrap
	icSend := icons.Send
	icZoomIn := icons.TbPlus
	icZoomOut := icons.TbMinus
	icZoomReset := icons.TbReset
	narrow := isNarrowTitlebar(vpW, availW)
	tight := isTightTitlebar(availW)
	var legacyProbeIDs map[string]string
	item := func(id, label, shortcut string, ic *icons.Icon, fn func()) blockui.MenuItemSpec {
		return blockui.MenuItemSpec{ID: id, Label: label, Shortcut: shortcut, Icon: ic, OnClick: func() {
			p.MenuState.Close()
			if fn != nil {
				fn()
			}
		}}
	}

	appendSep := func(items []blockui.MenuItemSpec) []blockui.MenuItemSpec {
		if len(items) > 0 && !items[len(items)-1].Separator {
			items = append(items, blockui.MenuItemSpec{Separator: true})
		}
		return items
	}

	// Plugin page_menu items apply to every surface (notes, code, images…).
	appendPluginPageItems := func(items []blockui.MenuItemSpec) []blockui.MenuItemSpec {
		if len(p.PluginPageMenu) == 0 {
			return items
		}
		items = appendSep(items)
		if legacyProbeIDs == nil {
			legacyProbeIDs = make(map[string]string, len(p.PluginPageMenu))
		}
		for _, pm := range p.PluginPageMenu {
			pmItem := pm
			id := "page-plugin:" + pmItem.PluginID + "/" + pmItem.ID
			legacyProbeIDs[id] = "page-plugin-" + pmItem.PluginID + "-" + pmItem.ID
			spec := item(id, pmItem.Title, "", nil, func() {
				if p.OnPluginMenuItem != nil {
					p.OnPluginMenuItem(pmItem)
				}
			})
			if len(pmItem.Icon.StrokePaths) > 0 {
				icSpec := pmItem.Icon
				spec.CustomIcon = func(c *ui.Context, size float32, colors theme.IconColors) {
					pluginui.DrawDuoSpec(c, icSpec, size, colors)
				}
			}
			items = append(items, spec)
		}
		return items
	}

	if surface == SurfaceNote {
		var items []blockui.MenuItemSpec
		if narrow && p.AnnotHasAny && !slotHidden(p, plugins.TitlebarSlotSendAll) {
			enabled := p.AnnotDrafts > 0 && !p.AnnotGenerating && p.OnSendAnnotations != nil
			sendLabel := theme.Tr("统一发送批注", "Send All Annotations")
			sendHint := ""
			if p.AnnotGenerating {
				sendLabel = theme.Tr("批注生成中…", "Generating Annotations…")
			} else if p.AnnotDrafts > 0 {
				sendHint = strconv.Itoa(p.AnnotDrafts)
			}
			items = append(items,
				blockui.MenuItemSpec{
					ID:       "page-annotations-send-all",
					Label:    sendLabel,
					Hint:     sendHint,
					Icon:     &icSend,
					Disabled: !enabled,
					OnClick: func() {
						p.MenuState.Close()
						if p.OnSendAnnotations != nil {
							p.OnSendAnnotations()
						}
					},
				},
				blockui.MenuItemSpec{Separator: true},
			)
		}
		items = append(items,
			item("page-history", theme.Tr("页面历史…", "Page History…"), "", &icHist, p.OnOpenHistory),
			item("page-export-image", theme.Tr("生成图片…", "Export as Image…"), blockui.FormatShortcutDisplay(nonEmptyOr(p.ShortcutExport, "cmd-shift-e")), &icImg, p.OnOpenExport),
			item("page-copy-md", theme.Tr("复制 Markdown", "Copy Markdown"), blockui.FormatShortcutDisplay(copyMarkdownShortcutSpec()), &icCopy, p.OnCopyMarkdown),
		)
		if p.OnToggleNoteSource != nil {
			srcLabel := theme.Tr("查看源码", "View Source")
			if p.NoteSourceView {
				srcLabel = theme.Tr("返回编辑", "Back to Editor")
			}
			items = append(items, item("page-view-source", srcLabel, "", &icCode, p.OnToggleNoteSource))
		}
		items = appendPluginPageItems(items)
		items = append(items,
			blockui.MenuItemSpec{Separator: true},
			item("page-reveal", revealFileLabel(), "", &icReveal, func() {
				if p.OnRevealPath != nil {
					p.OnRevealPath(p.ActiveTab)
				}
			}),
		)
		return items, legacyProbeIDs
	}

	var items []blockui.MenuItemSpec
	appendZoomItems := func() {
		if !tight || slotHidden(p, plugins.TitlebarSlotZoom) {
			return
		}
		items = append(items,
			item("page-zoom-in", theme.Tr("放大", "Zoom In"), blockui.FormatShortcutDisplay("cmd-="), &icZoomIn, func() {
				if p.OnPreviewZoomStep != nil {
					p.OnPreviewZoomStep(1.2)
				}
			}),
			item("page-zoom-out", theme.Tr("缩小", "Zoom Out"), blockui.FormatShortcutDisplay("cmd--"), &icZoomOut, func() {
				if p.OnPreviewZoomStep != nil {
					p.OnPreviewZoomStep(1.0 / 1.2)
				}
			}),
			blockui.MenuItemSpec{
				ID:       "page-zoom-actual",
				Label:    theme.Tr("实际大小", "Actual Size"),
				Hint:     "100%",
				Shortcut: blockui.FormatShortcutDisplay("cmd-0"),
				Icon:     &icZoomReset,
				OnClick: func() {
					p.MenuState.Close()
					if p.OnPreviewActual != nil {
						p.OnPreviewActual()
					}
				},
			},
			blockui.MenuItemSpec{
				ID:      "page-zoom-fit",
				Label:   theme.Tr("适应窗口", "Fit to Window"),
				Hint:    nonEmptyOr(p.PreviewZoomPct, "100%"),
				Checked: p.PreviewZoomFit,
				OnClick: func() {
					p.MenuState.Close()
					if p.OnPreviewFit != nil {
						p.OnPreviewFit()
					}
				},
			},
			blockui.MenuItemSpec{Separator: true},
		)
	}
	switch surface {
	case SurfaceCode, SurfaceHTML, SurfaceSVG:
		if surface == SurfaceSVG && !p.SVGShowSource {
			appendZoomItems()
		}
		if narrow && !slotHidden(p, plugins.TitlebarSlotWrap) && (surface != SurfaceSVG || p.SVGShowSource) {
			items = append(items, blockui.MenuItemSpec{
				ID:      "page-source-wrap",
				Label:   theme.Tr("自动换行", "Word Wrap"),
				Icon:    &icWrap,
				Checked: p.SourceWrap,
				OnClick: func() {
					p.MenuState.Close()
					if p.OnToggleSourceWrap != nil {
						p.OnToggleSourceWrap()
					}
				},
			})
		}
		if p.HasCopyableText {
			copyLabel := theme.Tr("复制全部内容", "Copy Content")
			if surface == SurfaceSVG {
				copyLabel = theme.Tr("复制 SVG 源码", "Copy SVG Source")
			}
			items = append(items, item("page-copy-content", copyLabel, "", &icCopy, p.OnCopyContent))
		}
		if p.CanFormatJSON {
			items = append(items, item("page-format-json", theme.Tr("格式化 JSON", "Format JSON"), "", &icFormat, p.OnFormatJSON))
		}
		if surface == SurfaceHTML || surface == SurfaceSVG {
			items = append(items, item("page-open-browser", theme.Tr("在浏览器中打开", "Open in Browser"), "", &icOpen, p.OnOpenInBrowser))
		}
	case SurfaceImagePreview:
		appendZoomItems()
		items = append(items, item("page-copy-image", theme.Tr("复制图片", "Copy Image"), "", &icCopy, p.OnCopyImage))
	}

	items = appendPluginPageItems(items)
	items = appendSep(items)
	if surface != SurfaceHTML {
		items = append(items, item("page-open-external", theme.Tr("用默认应用打开", "Open in Default App"), "", &icOpen, p.OnOpenExternal))
	}
	items = append(items,
		item("page-reveal", revealFileLabel(), "", &icReveal, func() {
			if p.OnRevealPath != nil {
				p.OnRevealPath(p.ActiveTab)
			}
		}),
	)
	return items, legacyProbeIDs
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
	var pluginItems []blockui.MenuItemSpec
	for _, pm := range p.PluginTabMenu {
		pmItem := pm
		spec := blockui.MenuItemSpec{
			ID:    "tab-plugin:" + pmItem.PluginID + "/" + pmItem.ID,
			Label: pmItem.Title,
			OnClick: func() {
				p.MenuState.Close()
				if p.OnPluginTabMenu != nil {
					p.OnPluginTabMenu(pmItem, target)
				}
			},
		}
		if len(pmItem.Icon.StrokePaths) > 0 {
			icSpec := pmItem.Icon
			spec.CustomIcon = func(c *ui.Context, size float32, colors theme.IconColors) {
				pluginui.DrawDuoSpec(c, icSpec, size, colors)
			}
		}
		pluginItems = append(pluginItems, spec)
	}
	if len(pluginItems) > 0 {
		pluginItems = append(pluginItems, blockui.MenuItemSpec{Separator: true})
	}
	return append(pluginItems, []blockui.MenuItemSpec{
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
	}...)
}

func renderSidebarToggleButton(c *ui.Context, p TitlebarProps) {
	pal := theme.Ocean()
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

	now := c.Now()
	if now.IsZero() {
		now = time.Now()
	}
	cueHold := theme.SidebarExpandCueHoldMs * time.Millisecond
	var elapsedMs float32
	cueActive := false
	if !sidebarOpen && !p.SidebarExpandCueAt.IsZero() {
		el := now.Sub(p.SidebarExpandCueAt)
		if el >= 0 && el < cueHold {
			cueActive = true
			elapsedMs = float32(el.Seconds() * 1000)
		}
	}

	ringStart := float32(theme.SidebarExpandCueRingDelayMs)
	ringDur := float32(theme.SidebarExpandCueRingMs)
	ringEnd := ringStart + ringDur
	nudgeStart := float32(theme.SidebarExpandCueNudgeDelayMs)
	nudgeDur := float32(theme.SidebarExpandCueNudgeMs)
	nudgeEnd := nudgeStart + nudgeDur
	activeMotionEnd := max(ringEnd, nudgeEnd)

	if cueActive && !testing.Testing() {
		if elapsedMs < activeMotionEnd {
			c.AnimationFrame()
		} else {
			remaining := cueHold - now.Sub(p.SidebarExpandCueAt)
			if remaining > 0 {
				c.After(remaining)
			}
		}
	}

	btn := ui.ButtonBase(c.Key("sidebar-toggle")).Height(theme.ChromeBoxSize).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).Radius(theme.RadiusMD).
		Cursor(ui.CursorPointer).Label(tip)
	blockui.RichTooltip(c, btn, blockui.TooltipSpec{
		Placement: blockui.TooltipBelow,
		Rows: []blockui.TooltipRow{{
			Trigger:  tip,
			Shortcut: blockui.FormatShortcutDisplay(sbSc),
		}},
	})

	pillTarget := float32(0)
	if cueActive {
		pillTarget = 1
	}
	pillP := pillTarget
	if !testing.Testing() {
		pillP = btn.AnimateTo("sidebarExpandPill", pillTarget, ui.Animation{
			Duration: theme.SidebarExpandCuePillMs * time.Millisecond,
			Ease:     ui.CubicBezier(0.22, 1, 0.36, 1),
		})
	}
	if pillP < 0 {
		pillP = 0
	} else if pillP > 1 {
		pillP = 1
	}

	basePad := (theme.ChromeBoxSize - theme.IconChrome) / 2
	padL := basePad + (theme.Space8-basePad)*pillP
	padR := basePad + (theme.Space10-basePad)*pillP
	btn.Padding(0, padR, 0, padL)

	hovered := btn.Hovered()
	pressed := btn.Pressed()

	bgCol := ui.Transparent
	bdCol := ui.Transparent
	hasBorder := false
	switch {
	case pillP >= 0.01:
		fillAlpha := float32(0.18)
		if hovered || pressed {
			fillAlpha = 0.26
		}
		bgCol = pal.Accent.Alpha(fillAlpha * pillP)
		bdCol = pal.Accent.Alpha(0.56 * pillP)
		hasBorder = true
	case p.SidebarFloating:
		bgCol = pal.Selected
	case hovered || pressed:
		bgCol = pal.ShellChrome().Hover
	}
	blockui.AnimateSurfaceColors(btn, bgCol, bdCol, hasBorder)
	blockui.TactileButton(btn, hovered, pressed, false)

	nudgeX := float32(0)
	if cueActive && !testing.Testing() {
		nudgeX = sidebarCueArrowNudge(elapsedMs, nudgeStart, nudgeDur)
	}

	iconColors := pal.DuoIconColors(pal.TextSecondary)
	switch {
	case pillP >= 0.01 || p.SidebarFloating:
		iconColors = pal.DuoIconColors(pal.Accent)
	case hovered || pressed:
		iconColors = pal.DuoIconColors(pal.Text)
	}
	playIconHover := hovered || pressed || (cueActive && !testing.Testing() && elapsedMs < nudgeEnd)

	labelText := theme.Tr("展开", "Expand")
	labelFullW := theme.SidebarExpandCueLabelWidthZH
	if labelText == "Expand" {
		labelFullW = theme.SidebarExpandCueLabelWidthEN
	}

	btn.Children(func() {
		iconWrap := ui.Box(c).Size(theme.IconChrome, theme.IconChrome).Shrink(0).Center()
		if nudgeX != 0 {
			iconWrap.Translate(nudgeX, 0)
		}
		iconWrap.Children(func() {
			icons.DrawAnimated(c, glyph, theme.IconChrome, iconColors, playIconHover)
		})

		if pillP > 0.005 {
			textAlpha := (pillP - 0.15) / 0.85
			if textAlpha < 0 {
				textAlpha = 0
			} else if textAlpha > 1 {
				textAlpha = 1
			}
			ui.Row(c.Key("sidebar-toggle-pill-label")).Width(labelFullW * pillP).Height(theme.ChromeBoxSize).
				Shrink(0).AlignItems(ui.Center).Clip().Opacity(textAlpha).Children(func() {
				ui.Box(c).Width(theme.Space4).Shrink(0)
				ui.Text(c, labelText).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
			})
		}

		if cueActive && !testing.Testing() && elapsedMs <= ringEnd {
			cueAt := p.SidebarExpandCueAt
			accent := pal.Accent
			ui.Box(c.Key("sidebar-toggle-pill-ring")).Absolute().Left(0).Top(0).Right(0).Bottom(0).
				Draw(func(painter *ui.Painter, r ui.Rect) {
					elMs := float32(painter.Now().Sub(cueAt).Seconds() * 1000)
					if elMs < ringStart || elMs > ringEnd {
						return
					}
					u := (elMs - ringStart) / ringDur
					inv := 1 - u
					e := 1 - inv*inv*inv
					ex := (r.W*0.14 + 3) * e
					ey := (r.H * 0.26) * e
					ringRect := ui.Rect{
						X: r.X - ex,
						Y: r.Y - ey,
						W: r.W + ex*2,
						H: r.H + ey*2,
					}
					alpha := 0.78 * (1 - e)
					if alpha > 0.005 {
						painter.Stroke(ringRect, accent.Alpha(alpha), theme.RadiusMD+ey*0.35, theme.SidebarExpandCueRingWidth)
					}
				})
		}
	})
	blockui.Probe(btn, "sidebar-toggle")
	if btn.Clicked() && p.OnToggleSidebar != nil {
		p.OnToggleSidebar()
	}
}

func sidebarCueArrowNudge(elapsedMs, startMs, durMs float32) float32 {
	if elapsedMs < startMs || elapsedMs > startMs+durMs || durMs <= 0 {
		return 0
	}
	u := (elapsedMs - startMs) / durMs
	smooth := func(t float32) float32 {
		if t <= 0 {
			return 0
		}
		if t >= 1 {
			return 1
		}
		return t * t * (3 - 2*t)
	}
	switch {
	case u < 0.28:
		return 2.4 * smooth(u/0.28)
	case u < 0.52:
		return 2.4 * (1 - smooth((u-0.28)/0.24))
	case u < 0.74:
		return 2.0 * smooth((u-0.52)/0.22)
	default:
		return 2.0 * (1 - smooth((u-0.74)/0.26))
	}
}
