package modals

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"block/internal/fuzzy"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"block/internal/updater"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	defaultSearchShortcut  = "cmd-shift-k"
	defaultQuickShortcut   = "alt-n"
	pluginShortcutDisabled = "-"
)

// PluginCommandShortcutAction returns the CustomShortcuts key for a plugin command.
func PluginCommandShortcutAction(pluginID, commandID string) string {
	return TabPluginPrefix + pluginID + "/" + commandID
}

// IsPluginCommandShortcutAction reports whether action refers to a plugin command shortcut.
func IsPluginCommandShortcutAction(action string) bool {
	return strings.HasPrefix(action, TabPluginPrefix)
}

// EffectivePluginCommandShortcut returns the active shortcut chord for a plugin
// command, applying user overrides from AppState.CustomShortcuts.
func EffectivePluginCommandShortcut(st model.AppState, pluginID, commandID, defaultShortcut string) string {
	if st.CustomShortcuts != nil {
		if raw, ok := st.CustomShortcuts[PluginCommandShortcutAction(pluginID, commandID)]; ok {
			v := strings.TrimSpace(raw)
			if v == pluginShortcutDisabled {
				return ""
			}
			if v != "" {
				return v
			}
		}
	}
	return strings.TrimSpace(defaultShortcut)
}

type shortcutActionSpec struct {
	action   string
	buttonID string
	def      string
	zh       string
	en       string
}

var shortcutActionSpecs = []shortcutActionSpec{
	{action: "global_summon", buttonID: "settings-rec-search", def: defaultSearchShortcut, zh: "全局唤出", en: "Global Summon"},
	{action: "quick_note", buttonID: "settings-rec-quick", def: defaultQuickShortcut, zh: "速记小窗", en: "Quick Note"},
	{action: "search", buttonID: "settings-rec-inapp-search", def: "cmd-k", zh: "搜索笔记", en: "Search Notes"},
	{action: "find", buttonID: "settings-keycap-find", def: "cmd-f", zh: "页内查找", en: "Find in Page"},
	{action: "new_note", buttonID: "settings-rec-new-note", def: "cmd-n", zh: "新建笔记", en: "New Note"},
	{action: "toggle_sidebar", buttonID: "settings-rec-sidebar", def: "alt-b", zh: "显示或隐藏侧栏", en: "Toggle Sidebar"},
	{action: "toggle_ai", buttonID: "settings-rec-ai", def: "cmd-j", zh: "AI Agent 面板", en: "AI Agent Panel"},
	{action: "export_image", buttonID: "settings-rec-export", def: "cmd-shift-e", zh: "导出与生成图片", en: "Export & Image"},
}

// AllShortcutDefaults returns a fresh map of every customizable shortcut action ID to its default chord.
func AllShortcutDefaults() map[string]string {
	out := make(map[string]string, len(shortcutActionSpecs))
	for _, spec := range shortcutActionSpecs {
		out[spec.action] = spec.def
	}
	return out
}

// EffectiveShortcut returns the active shortcut chord for `action`, falling back to its default.
func EffectiveShortcut(st model.AppState, action string) string {
	switch action {
	case "global_summon":
		if v := strings.TrimSpace(st.ShortcutSearch); v != "" {
			return v
		}
		if st.CustomShortcuts != nil {
			if v := strings.TrimSpace(st.CustomShortcuts[action]); v != "" {
				return v
			}
		}
		return defaultSearchShortcut
	case "quick_note":
		if v := strings.TrimSpace(st.ShortcutQuick); v != "" {
			return v
		}
		if st.CustomShortcuts != nil {
			if v := strings.TrimSpace(st.CustomShortcuts[action]); v != "" {
				return v
			}
		}
		return defaultQuickShortcut
	case "performance":
		return "cmd-shift-m"
	default:
		if st.CustomShortcuts != nil {
			if v := strings.TrimSpace(st.CustomShortcuts[action]); v != "" {
				return v
			}
		}
		for _, spec := range shortcutActionSpecs {
			if spec.action == action {
				return spec.def
			}
		}
		return ""
	}
}

// Manager owns the full-window Settings page and every modal overlay in Block.
type Manager struct {
	dataDir string
	host    model.WorkspaceHost

	// Injectable callbacks for host state, AI settings, search, history, diff, and export.
	GetState              func() model.AppState
	OnStateChanged        func(func(*model.AppState))
	RenderAISettings      func(c *ui.Context)
	AIEnabled             func() bool
	PluginHost            PluginSettingsHost
	SidebarWidth          *float32
	OnSidebarWidthChanged func(float32)
	OnForgetNotebook      func(string)
	SearchFn              func(query string, allNotebooks bool) ([]model.SearchHit, error)
	ListHistoryFn         func(notePath string) ([]model.HistorySnapshot, error)
	ReadHistoryFn         func(notePath, snapID string) (string, error)
	DiffLinesFn           func(oldText, newText string) []model.DiffLine
	ExportFn              func(format model.ExportFormat, outPath string) error
	// FeedbackSubmitFn, when set, replaces the real upload with one synchronous
	// call (tests and smoke runs); attachments and the log are not sent.
	FeedbackSubmitFn func(category, message, contact string) error
	// FeedbackNoticeFn shows a toast for a report that finished in the background.
	FeedbackNoticeFn func(message, tone string)

	fallbackState model.AppState

	// Settings page state.
	settingsOpen           bool
	settingsTab            string
	storeVisible           atomic.Bool
	shortcutScope          int
	recordingShortcut      string
	justRecordedPasses     int
	shortcutConflictNotice string
	fontPickerOpen         string
	fontTriggerRects       map[string]ui.Rect
	fontQuery              string
	fontCursor             int
	fontScrollToCursor     bool
	fontFamilies           []ui.FontFamily
	fontFamiliesEpoch      uint64
	fontCache              fontPickerCache
	fontScroll             ui.ScrollState
	assetsDraft            string
	folderRuleDraft        string
	folderRuleAdding       bool
	searchIgnorePicker     internalui.DropdownSelectState
	regexRuleDraft         string
	folderRuleError        string
	regexRuleError         string
	advancedRulesOpen      bool
	settingsScroll         ui.ScrollState
	settingsSidebarScroll  ui.ScrollState
	storeScroll            ui.ScrollState
	storeDetailScroll      ui.ScrollState
	wasStoreDetail         bool
	settingsFocusHandle    ui.Handle
	fontSearchHandle       ui.Handle

	// Performance modal state.
	perfOffset     ui.Point
	perfMu         sync.Mutex
	perfOpen       bool
	perfCancel     chan struct{}
	perfCategories []PerfCategory
	perfCPU        float64
	perfRSS        uint64
	perfSamples    int
	perfLastScan   string
	perfScanErr    string
	perfTreemap    internalui.TreemapState
	perfState      perfExtraState

	// Search modal state.
	searchPalette     searchPaletteState
	onOpenSearchHit   func(hit model.SearchHit)
	searchOpen        bool
	searchQuery       string
	searchLastQuery   string
	searchAllNB       bool
	searchHits        []model.SearchHit
	searchSelected    int
	searchErr         string
	searchInputHandle ui.Handle

	// About modal state.
	aboutOpen     bool
	aboutCopiedAt time.Time

	// Feedback modal state.
	feedbackOpen        bool
	feedbackCategory    string
	feedbackMessage     string
	feedbackContact     string
	feedbackSentContact string
	feedbackDone        bool
	feedbackErr         string
	fb                  feedbackUI

	// History modal state.
	historyOpen           bool
	historyNotePath       string
	historyCurrentContent string
	historySnapshots      []model.HistorySnapshot
	historySelectedID     string
	historySnapContent    string
	historyDiffLines      []model.DiffLine
	historyRowHeights     []float32
	historyMode           string // "diff" | "clean"
	historyConfirmRestore bool
	historyErr            string
	historyScroll         ui.ScrollState

	// Export modal state.
	exportOpen        bool
	exportNotePath    string
	exportFormat      model.ExportFormat
	exportTheme       string
	exportIncludeMeta bool
	exportOutPath     string
	exportBusy        bool
	exportStatus      string
	exportErr         string

	// Updater modal state.
	Updater           *updater.Service
	updaterOpen       bool
	updaterScroll     ui.ScrollState
	updaterSourceMenu internalui.DropdownSelectState
	updaterPreview    *updater.Snapshot
}

// NewManager creates a modal and settings manager bound to `dataDir` and `host`.
func NewManager(dataDir string, host model.WorkspaceHost) *Manager {
	m := &Manager{
		dataDir:          dataDir,
		host:             host,
		fallbackState:    model.DefaultAppState(theme.Language()),
		settingsTab:      "ai",
		feedbackCategory: "bug",
		fb:               feedbackUI{includeLog: true},
		historyMode:      "diff",
		exportFormat:     model.ExportMarkdown,
		exportTheme:      "current",
	}
	m.initDefaultPerfCategories()
	return m
}

func (m *Manager) currentState() model.AppState {
	if m.GetState != nil {
		return m.GetState()
	}
	return m.fallbackState
}

func (m *Manager) mutateState(fn func(*model.AppState)) {
	if fn == nil {
		return
	}
	if m.OnStateChanged != nil {
		m.OnStateChanged(fn)
	} else {
		fn(&m.fallbackState)
	}
	if m.host != nil {
		m.host.RequestRedraw()
	}
}

func (m *Manager) requestRedraw() {
	if m.host != nil {
		m.host.RequestRedraw()
	}
}

// IsSettingsOpen reports whether the full-window Settings page is currently open.
func (m *Manager) IsSettingsOpen() bool {
	return m.settingsOpen
}

// IsStoreVisible reports whether the App Store settings tab is currently open.
// Safe to call from any goroutine.
func (m *Manager) IsStoreVisible() bool {
	return m != nil && m.storeVisible.Load()
}

func (m *Manager) syncStoreVisible() {
	m.storeVisible.Store(m.settingsOpen && IsStoreTab(m.ActiveSettingsTab()))
}

// ActiveSettingsTab returns the currently active Settings tab ID.
func (m *Manager) ActiveSettingsTab() string {
	if m.settingsTab == "" {
		return "ai"
	}
	if m.settingsTab == TabStoreInstalled {
		return TabAppStore
	}
	return m.settingsTab
}

// ActiveSettingsTabTitle returns the localized display title for the active Settings tab.
func (m *Manager) ActiveSettingsTabTitle() string {
	return m.resolveSettingsTabTitle(m.ActiveSettingsTab())
}

// IsRecordingShortcut reports whether the shortcut recorder is actively capturing keys
// (including the remainder of the frame in which a key was just captured).
func (m *Manager) IsRecordingShortcut() bool {
	return m.settingsOpen && (m.recordingShortcut != "" || m.justRecordedPasses > 0)
}

// OpenSettings opens the full-window Settings page at `tab` ("ai", "appearance", "folders", "shortcuts", "app-store", or "plugin:<id>").
func (m *Manager) OpenSettings(tab string) {
	previous := ""
	if m.settingsOpen {
		previous = m.ActiveSettingsTab()
	}
	switch {
	case tab == "ai" || tab == "appearance" || tab == "folders" || tab == "shortcuts":
		m.settingsTab = tab
	case tab == TabStoreInstalled:
		m.settingsTab = TabAppStore
	case IsPluginSystemTab(tab):
		m.settingsTab = tab
	default:
		m.settingsTab = "ai"
	}
	st := m.currentState()
	if strings.TrimSpace(st.AssetsFolder) != "" {
		m.assetsDraft = st.AssetsFolder
	} else {
		m.assetsDraft = "assets"
	}
	m.recordingShortcut = ""
	m.justRecordedPasses = 0
	m.shortcutConflictNotice = ""
	m.fontPickerOpen = ""
	m.folderRuleError = ""
	m.folderRuleAdding = false
	m.searchIgnorePicker = internalui.DropdownSelectState{Borderless: true}
	m.regexRuleError = ""
	m.settingsOpen = true
	m.syncStoreVisible()
	m.notifySettingsTabChanged(previous, m.ActiveSettingsTab())
	m.requestRedraw()
}

// CloseSettings closes the full-window Settings page and stops any active recording.
func (m *Manager) CloseSettings() {
	if !m.settingsOpen {
		return
	}
	previous := m.ActiveSettingsTab()
	m.settingsOpen = false
	m.syncStoreVisible()
	m.notifySettingsTabChanged(previous, "")
	m.recordingShortcut = ""
	m.justRecordedPasses = 0
	m.shortcutConflictNotice = ""
	m.fontPickerOpen = ""
	m.requestRedraw()
}

type settingsTabSpec struct {
	id   string
	icon icons.Icon
	zh   string
	en   string
}

var settingsTabs = []settingsTabSpec{
	{id: "appearance", icon: icons.SettingsAppearance, zh: "外观与字体", en: "Appearance & Fonts"},
	{id: "ai", icon: icons.SettingsAI, zh: "AI Agent", en: "AI Agent"},
	{id: "folders", icon: icons.SettingsFolder, zh: "文件夹", en: "Folders"},
	{id: "shortcuts", icon: icons.SettingsShortcuts, zh: "快捷键", en: "Shortcuts"},
}

func (m *Manager) resolveSettingsTabTitle(tab string) string {
	if IsPluginSystemTab(tab) {
		if m.PluginHost != nil {
			if t := strings.TrimSpace(m.PluginHost.TabTitle(tab)); t != "" {
				return t
			}
		}
		if IsStoreTab(tab) {
			return theme.Tr("应用商店", "App Store")
		}
		return strings.TrimPrefix(tab, TabPluginPrefix)
	}
	return settingsTabTitle(tab)
}

func settingsTabTitle(tab string) string {
	for _, spec := range settingsTabs {
		if spec.id == tab {
			return theme.Tr(spec.zh, spec.en)
		}
	}
	return theme.Tr("AI Agent", "AI Agent")
}

// RenderSettings draws the full-window Settings page (standalone layout with responsive sidebar yielding).
func (m *Manager) RenderSettings(c *ui.Context) {
	if !m.settingsOpen {
		return
	}
	pal := theme.Ocean()
	vpW, _ := c.Size()
	if vpW <= 0 {
		vpW = 1000
	}
	sbW := theme.SidebarMinWidth
	if m.SidebarWidth != nil && *m.SidebarWidth > 0 {
		sbW = min(max(*m.SidebarWidth, theme.SidebarMinWidth), theme.SidebarMaxWidth)
	}
	st := m.currentState()
	showDockedSidebar := !st.SidebarCollapsed && sbW+680 <= vpW+0.5

	ui.Row(c.Key("settings-standalone-root")).Fill().Background(pal.ShellChrome().Ground).Children(func() {
		if showDockedSidebar {
			m.RenderSettingsSidebar(c, false, nil)
		}
		m.renderSettingsBody(c, false, nil, true)
	})
}

// RenderSettingsSidebar draws the left category navigation column (docked or inside the narrow floating drawer),
// matching Settings.dc.html.
func (m *Manager) RenderSettingsSidebar(c *ui.Context, isDrawer bool, onDismissDrawer func()) {
	if !m.settingsOpen {
		return
	}
	pal := theme.Ocean()
	chrome := pal.ShellChrome()
	tab := m.ActiveSettingsTab()
	sbW := theme.SidebarMinWidth
	if m.SidebarWidth != nil && *m.SidebarWidth > 0 {
		sbW = min(max(*m.SidebarWidth, theme.SidebarMinWidth), theme.SidebarMaxWidth)
	}

	// The docked slot and its open/close motion belong to the workspace shell.
	wrap := ui.Row(c.Key("settings-sidebar-wrap")).Shrink(0).FillHeight()
	wrap.Children(func() {
		col := ui.Column(c.Key("settings-sidebar")).Width(sbW).FillHeight().Shrink(0)
		if isDrawer {
			col.Background(pal.Sidebar)
		}
		col.Children(func() {
			internalui.Scroll(c.Key("settings-sidebar-scroll"), &m.settingsSidebarScroll).Grow(1).MinHeight(0).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Padding(4, 8, 8, 12).Gap(2).Children(func() {
					backRow := ui.ButtonBase(c.Key("settings-sidebar-back")).FillWidth().Height(theme.HeightControl).PaddingX(theme.Space8).Gap(theme.Space10).
						Margin(0, 0, 8, 0).AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusMD).
						Cursor(ui.CursorPointer).Label(theme.Tr("返回", "Back"))
					backHov := backRow.Hovered()
					backPress := backRow.Pressed()
					backBG := ui.Transparent
					backFG := pal.TextSecondary
					if backHov || backPress {
						backBG = chrome.Hover
						backFG = pal.Text
					}
					internalui.AnimateSurfaceColors(backRow, backBG, ui.Transparent, false)
					internalui.TactileCard(backRow, backHov, backPress, false, false)
					backRow.Children(func() {
						icons.DrawAnimated(c, icons.ArrowLeft, 16, pal.DuoIconColors(backFG), backHov || backPress)
						ui.Text(c, theme.Tr("返回", "Back")).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(backFG).SingleLine()
					})
					internalui.Probe(backRow, "settings-sidebar-back")
					if backRow.Clicked() {
						m.CloseSettings()
					}

				for i, spec := range settingsTabs {
					active := tab == spec.id
					navID := "settings-tab-" + spec.id
					title := theme.Tr(spec.zh, spec.en)
					btn := ui.ButtonBase(c.Key(navID)).FillWidth().Height(theme.HeightControl).PaddingX(theme.Space8).Gap(theme.Space10).
						AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
						Label(title).Transition(internalui.CascadeRowTransition(i))
					hovered := btn.Hovered()
					pressed := btn.Pressed()
					if active {
						btn.Background(pal.Selected).Border(1, ui.Transparent)
					} else if pressed || hovered {
						btn.Background(chrome.Hover).Border(1, ui.Transparent)
					} else {
						btn.Border(1, ui.Transparent)
					}
					internalui.TactileCard(btn, hovered, pressed, active, false)
					btn.Children(func() {
						ic := pal.DuoIconColors(pal.TextSecondary)
						if active || hovered || pressed {
							ic = pal.DuoIconColors(pal.Text)
						}
						icons.DrawAnimated(c, spec.icon, theme.IconRow, ic, active || hovered || pressed)
						col := pal.TextSecondary
						weight := theme.WeightRegular
						if active {
							col = pal.Text
							weight = theme.WeightControl
						} else if hovered || pressed {
							col = pal.Text
						}
						ui.Text(c, title).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(weight).TextColor(col).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					})
					internalui.Probe(btn, navID)
					if btn.Clicked() {
						if isDrawer && onDismissDrawer != nil {
							onDismissDrawer()
						}
						m.selectSettingsTab(spec.id)
					}
				}

				if m.PluginHost != nil {
					pluginNavs := m.PluginHost.NavEntries()
					if len(pluginNavs) > 0 {
						ui.Box(c.Key("settings-plugin-divider")).FillWidth().Padding(theme.Space8, theme.Space6).Children(func() {
							ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
						})
						baseIdx := len(settingsTabs)
						installedHeaderRendered := false
						renderInstalledHeader := func() {
							if installedHeaderRendered {
								return
							}
							installedHeaderRendered = true
							ui.Box(c.Key("settings-store-divider")).FillWidth().Padding(theme.Space8, theme.Space6).Children(func() {
								ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
							})
							ui.Row(c.Key("settings-installed-header")).FillWidth().Height(theme.SettingsSectionTitleH).
								Padding(0, theme.Space4, 0, theme.Space10).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
								ui.Text(c, theme.Tr("已安装", "Installed")).FontSize(theme.FontSmall).
									FixedLineHeight(theme.LineSmall).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
								ui.Text(c, strconv.Itoa(m.PluginHost.InstalledCount())).Font("monospace").
									FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).FontWeight(theme.WeightMono).
									TextColor(pal.TextSecondary).SingleLine().Grow(1)
								addBtn := internalui.IconButton(c, icons.Plus, internalui.IconButtonOptions{
									ID:       "settings-installed-add",
									Label:    theme.Tr("安装插件", "Install plugins"),
									Tooltip:  theme.Tr("安装插件", "Install plugins"),
									Variant:  internalui.IconPlain,
									IconSize: theme.IconAction,
								})
								if addBtn.Clicked() {
									if isDrawer && onDismissDrawer != nil {
										onDismissDrawer()
									}
									m.selectSettingsTab(TabAppStore)
								}
							})
						}
						for j, entry := range pluginNavs {
							ent := entry
							if strings.HasPrefix(ent.TabID, TabPluginPrefix) {
								renderInstalledHeader()
							}
							active := tab == ent.TabID
							navID := "settings-tab-" + ent.TabID
							btn := ui.ButtonBase(c.Key(navID)).FillWidth().Height(theme.HeightControl).PaddingX(theme.Space8).Gap(theme.Space10).
								AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
								Label(ent.Title).Transition(internalui.CascadeRowTransition(baseIdx + j))
							hovered := btn.Hovered()
							pressed := btn.Pressed()
							if active {
								btn.Background(pal.Selected).Border(1, ui.Transparent)
							} else if pressed || hovered {
								btn.Background(chrome.Hover).Border(1, ui.Transparent)
							} else {
								btn.Border(1, ui.Transparent)
							}
							internalui.TactileCard(btn, hovered, pressed, active, false)
							isPlugin := strings.HasPrefix(ent.TabID, TabPluginPrefix)
							disabled := isPlugin && !ent.StatusDot
							btn.Children(func() {
								lit := ent.Lit || active || hovered || pressed
								ic := pal.DuoIconColors(pal.TextSecondary)
								if disabled && !active && !hovered && !pressed {
									ic = pal.DuoIconColors(pal.Placeholder)
									lit = false
								} else if lit {
									ic = pal.DuoIconColors(pal.Text)
								}
								if ent.DrawIcon != nil {
									if disabled {
										ui.Box(c).Shrink(0).Opacity(theme.DisabledOpacity).Children(func() {
											ent.DrawIcon(c, theme.IconRow, ic, lit)
										})
									} else {
										ent.DrawIcon(c, theme.IconRow, ic, lit)
									}
								}
								col := pal.TextSecondary
								weight := theme.WeightRegular
								if disabled && !active && !hovered && !pressed {
									col = pal.Placeholder
								} else if active {
									col = pal.Text
									weight = theme.WeightControl
								} else if hovered || pressed {
									col = pal.Text
								}
								ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
									ui.Text(c, ent.Title).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(weight).TextColor(col).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
									if ent.Beta {
										betaCol := pal.TextSecondary
										if disabled {
											betaCol = pal.Placeholder
										}
										ui.Text(c, "Beta").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
											FontWeight(theme.WeightRegular).TextColor(betaCol).SingleLine().Shrink(0)
									}
								})
								if ent.CountText != "" {
									ui.Text(c, ent.CountText).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
								} else if isPlugin && ent.Starting {
									ui.Spinner(c).FontSize(theme.IconMicro).TextColor(pal.Accent)
								} else if isPlugin && ent.HasError {
									ui.Text(c, theme.Tr("失败", "Failed")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										TextColor(pal.Danger).SingleLine().Shrink(0)
								} else if disabled {
									status := ent.DisabledLabel
									if status == "" {
										status = theme.Tr("未启用", "Disabled")
									}
									ui.Row(c).Height(theme.Space20).PaddingX(theme.Space8).Gap(theme.Space6).
										AlignItems(ui.Center).Radius(theme.RadiusPill).Background(pal.WarningSurface).Shrink(0).Children(func() {
										ui.Box(c).Width(theme.UpdaterBadgeDotSize).Height(theme.UpdaterBadgeDotSize).
											Radius(theme.RadiusPill).Background(pal.Warning)
										ui.Text(c, status).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
											FontWeight(theme.WeightControl).TextColor(pal.Warning).SingleLine().Shrink(0)
									})
								}
							})
							internalui.Probe(btn, navID)
							if btn.Clicked() {
								if isDrawer && onDismissDrawer != nil {
									onDismissDrawer()
								}
								m.selectSettingsTab(ent.TabID)
							}
						}
						if !installedHeaderRendered {
							renderInstalledHeader()
						}
					}
				}
				})
			})
			if isDrawer && onDismissDrawer != nil {
				st := m.currentState()
				sbSpec := EffectiveShortcut(st, "toggle_sidebar")
				ui.Column(c.Key("settings-drawer-footer")).FillWidth().Shrink(0).
					Padding(theme.Space8, theme.Space8, theme.Space8, theme.Space12).Gap(theme.Space8).Children(func() {
					ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
					dismissBtn := ui.ButtonBase(c.Key("settings-drawer-dismiss")).FillWidth().Height(theme.HeightControl).
						PaddingX(theme.Space10).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).
						Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
						Label(theme.Tr("收起浮窗导航", "Collapse Navigation"))
					dHov := dismissBtn.Hovered()
					dPress := dismissBtn.Pressed()
					dBG := chrome.Hover
					dFG := pal.TextSecondary
					if dHov || dPress {
						dBG = pal.Selected
						dFG = pal.Text
					}
					internalui.AnimateSurfaceColors(dismissBtn, dBG, ui.Transparent, false)
					internalui.TactileCard(dismissBtn, dHov, dPress, false, false)
					dismissBtn.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).MinWidth(0).Shrink(1).Children(func() {
							icons.DrawAnimated(c, icons.SidebarCollapse, theme.IconAction, pal.DuoIconColors(pal.Accent), dHov || dPress)
							ui.Text(c, theme.Tr("收起浮窗导航", "Collapse Navigation")).
								FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightControl).TextColor(dFG).SingleLine().Ellipsis("…")
						})
						if sbSpec != "" {
							internalui.ShortcutKeycap(c, sbSpec)
						}
					})
					internalui.Probe(dismissBtn, "settings-drawer-dismiss")
					if dismissBtn.Clicked() {
						onDismissDrawer()
					}
				})
			}
		})
		if !isDrawer && m.SidebarWidth != nil {
			if internalui.SeamlessResizeHandle(c, "settings-sidebar-resize", m.SidebarWidth, theme.SidebarMinWidth, theme.SidebarMaxWidth, theme.SidebarDefaultWidth) && m.OnSidebarWidthChanged != nil {
				m.OnSidebarWidthChanged(*m.SidebarWidth)
			}
		}
	})
}

// RenderSettingsTitlebarLeading lets the active plugin/store tab render its own
// breadcrumb inside the window titlebar. Returns true if handled.
func (m *Manager) RenderSettingsTitlebarLeading(c *ui.Context) bool {
	if !m.settingsOpen || m.PluginHost == nil {
		return false
	}
	tab := m.ActiveSettingsTab()
	if !IsPluginSystemTab(tab) {
		return false
	}
	return m.PluginHost.RenderTitlebarLeading(c, tab, m.selectSettingsTab)
}

// RenderSettingsTitlebarTrailing renders the unified right-side controls
// (status, primary action / running progress, compact switch, more menu) in the
// window titlebar for plugin and App Store detail tabs.
func (m *Manager) RenderSettingsTitlebarTrailing(c *ui.Context) {
	if !m.settingsOpen || m.PluginHost == nil {
		return
	}
	tab := m.ActiveSettingsTab()
	if !IsPluginSystemTab(tab) {
		return
	}
	m.PluginHost.RenderTitlebarTrailing(c, tab, m.selectSettingsTab)
}

// RenderSettingsTitlebarOverlays draws floating menus and error popovers owned
// by the Settings titlebar inside the top-level Overlay layer.
func (m *Manager) RenderSettingsTitlebarOverlays(c *ui.Context) {
	if !m.settingsOpen || m.PluginHost == nil {
		return
	}
	tab := m.ActiveSettingsTab()
	if !IsPluginSystemTab(tab) {
		return
	}
	m.PluginHost.RenderTitlebarOverlays(c, tab, m.selectSettingsTab)
}

// CloseSettingsTitlebarOverlays closes any open plugin titlebar menu/popover.
func (m *Manager) CloseSettingsTitlebarOverlays() bool {
	if m.PluginHost == nil {
		return false
	}
	return m.PluginHost.CloseTitlebarOverlays()
}

// RenderSettingsBody draws the scrollable Settings content page inside the workspace floating card.
func (m *Manager) RenderSettingsBody(c *ui.Context, drawerOpen bool, onDismissDrawer func()) {
	m.renderSettingsBody(c, drawerOpen, onDismissDrawer, false)
}

func (m *Manager) renderSettingsBody(c *ui.Context, drawerOpen bool, onDismissDrawer func(), showCloseBtn bool) {
	if !m.settingsOpen {
		return
	}
	pal := theme.Ocean()
	cardBG := pal.ShellChrome().Card
	tab := m.ActiveSettingsTab()

	root := ui.Column(c.Key("settings-page")).Grow(1).MinWidth(0).Fill().Background(cardBG).
		TabStop(false).Bind(&m.settingsFocusHandle)
	internalui.Probe(root, "settings-page")

	// Capture keyboard for shortcut recording.
	root.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind != ui.InputKeyDown || m.recordingShortcut == "" {
			return
		}
		if ev.Key == ui.KeyEscape && ev.Mods == 0 {
			m.recordingShortcut = ""
			m.justRecordedPasses = 4
			return
		}
		if (ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete) && ev.Mods == 0 && IsPluginCommandShortcutAction(m.recordingShortcut) {
			m.clearRecordedPluginShortcut(m.recordingShortcut)
			return
		}
		if spec, ok := inputEventToShortcutSpec(ev); ok {
			m.applyRecordedShortcut(m.recordingShortcut, spec)
		}
	})

	padX := float32(32)
	if b := root.Bounds(); b.W > 0 && b.W < 560 {
		padX = 16
	} else if b.W <= 0 {
		if winW, _ := c.Size(); winW > 0 && winW < 560 {
			padX = 16
		}
	}

	listing := m.PluginHost != nil && m.PluginHost.StoreListing(tab)
	isPluginPage := strings.HasPrefix(tab, TabPluginPrefix) && m.PluginHost != nil
	inStoreDetail := IsStoreTab(tab) && !listing
	if inStoreDetail != m.wasStoreDetail {
		if inStoreDetail {
			m.storeDetailScroll.Y = 0
		}
		m.wasStoreDetail = inStoreDetail
	}
	scrollID, scroll := "settings-scroll", &m.settingsScroll
	contentWidth, padBottom := theme.SettingsContentMaxWidth, float32(56)
	if IsStoreTab(tab) {
		scrollID, scroll = "settings-store-detail-scroll", &m.storeDetailScroll
	}
	if listing {
		scrollID, scroll = "settings-store-scroll", &m.storeScroll
		contentWidth, padBottom = theme.StoreContentMaxWidth, theme.Space24
		if padX > theme.Space16 {
			padX = theme.StorePagePadding
		}
	}

	contentGap := theme.Space24
	if isPluginPage {
		contentGap = theme.Space16
	}
	hideBodyHeader := !showCloseBtn && (isPluginPage || inStoreDetail)
	padTop := theme.StorePagePadding
	if hideBodyHeader {
		padTop = theme.Space24
	}

	root.Children(func() {
		internalui.Scroll(c.Key(scrollID), scroll).Grow(1).MinHeight(0).MinWidth(0).Fill().
			Background(cardBG).Padding(padTop, padX, padBottom, padX).AlignItems(ui.Center).Children(func() {
			ui.Column(c.Key("settings-content")).FillWidth().MinWidth(0).MaxWidth(contentWidth).
				Gap(contentGap).Children(func() {
				if !hideBodyHeader {
					ui.Column(c.Key("settings-header-wrap")).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
						header := ui.Row(c.Key("settings-header")).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12)
						if listing {
							header.Wrap()
						}
						header.Children(func() {
							title := ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center)
							if listing {
								title.Basis(theme.StoreTitleWidth)
							}
							title.Children(func() {
								ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
									ui.Text(c, m.resolveSettingsTabTitle(tab)).FontSize(theme.FontSettingsTitle).FixedLineHeight(theme.LineSettingsTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
									if IsPluginSystemTab(tab) && m.PluginHost != nil && m.PluginHost.TabIsBeta(tab) {
										internalui.BetaBadge(c, false)
									}
								})
							})
							hasExtras := IsPluginSystemTab(tab) && m.PluginHost != nil
							if hasExtras || showCloseBtn {
								extras := ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Shrink(0)
								if listing {
									extras.MinWidth(0).MaxWidthPercent(100).Shrink(1).Wrap()
								}
								extras.Children(func() {
									if hasExtras {
										m.PluginHost.RenderHeaderExtras(c, tab, m.selectSettingsTab)
									}
									if showCloseBtn {
										backBtn := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
											ID:       "settings-back",
											Label:    theme.Tr("返回", "Back"),
											Tooltip:  theme.Tr("关闭设置", "Close Settings"),
											Variant:  internalui.IconChrome,
											IconSize: theme.IconInline,
										})
										if backBtn.Clicked() {
											m.CloseSettings()
										}
									}
								})
							}
						})
					})
				}

				ui.Column(c.Key("settings-tab-stage-" + tab)).FillWidth().MinWidth(0).
					Transition(internalui.SurfaceStageTransition()).Children(func() {
					switch tab {
					case "ai":
						m.renderSettingsAITab(c)
					case "appearance":
						m.renderSettingsAppearanceTab(c)
					case "folders":
						m.renderSettingsFoldersTab(c)
					case "shortcuts":
						m.renderSettingsShortcutsTab(c)
					default:
						if IsPluginSystemTab(tab) && m.PluginHost != nil {
							m.PluginHost.RenderTabBody(c, tab, m.selectSettingsTab)
							if pluginID, ok := strings.CutPrefix(tab, TabPluginPrefix); ok && pluginID != "" {
								m.renderPluginShortcutsSection(c, pluginID)
							}
						}
					}
				})
			})
		})
		if listing {
			ui.Column(c.Key("settings-store-footer")).FillWidth().MinWidth(0).Shrink(0).AlignItems(ui.Center).Children(func() {
				ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
				ui.Column(c).FillWidth().MaxWidth(contentWidth+2*padX).Padding(theme.Space12, padX, theme.Space16, padX).
					Children(func() { m.PluginHost.RenderStoreFooter(c) })
			})
		}
	})

	if m.fontPickerOpen != "" {
		m.renderFontPickerOverlay(c)
	}
	if m.recordingShortcut == "" && m.justRecordedPasses == 0 && (m.PluginHost == nil || !m.PluginHost.IsStoreLightboxOpen()) && c.Shortcut(0, ui.KeyEscape) {
		switch {
		case m.fontPickerOpen != "":
			m.fontPickerOpen = ""
		case m.searchIgnorePicker.Open:
			m.searchIgnorePicker.Open = false
		case m.CloseSettingsTitlebarOverlays():
		case drawerOpen && onDismissDrawer != nil:
			onDismissDrawer()
		default:
			m.CloseSettings()
		}
	}
	if m.justRecordedPasses > 0 {
		m.justRecordedPasses--
	}
}

func (m *Manager) selectSettingsTab(tab string) {
	if tab == TabStoreInstalled {
		tab = TabAppStore
	}
	previous := m.ActiveSettingsTab()
	m.recordingShortcut = ""
	m.justRecordedPasses = 0
	m.shortcutConflictNotice = ""
	m.fontPickerOpen = ""
	m.searchIgnorePicker.Open = false
	m.settingsTab = tab
	m.syncStoreVisible()
	if previous == tab && IsStoreTab(tab) && m.PluginHost != nil && !m.PluginHost.StoreListing(tab) {
		m.PluginHost.SettingsTabChanged(previous, tab)
		return
	}
	m.notifySettingsTabChanged(previous, tab)
}

func (m *Manager) notifySettingsTabChanged(previous, current string) {
	if previous == current {
		return
	}
	if IsStoreTab(current) {
		m.storeScroll.ScrollTo(0, 0, ui.Animation{})
	}
	if m.PluginHost != nil {
		m.PluginHost.SettingsTabChanged(previous, current)
	}
}

func (m *Manager) renderSettingsAITab(c *ui.Context) {
	pal := theme.Ocean()
	ui.Column(c.Key("settings-ai-body")).FillWidth().Gap(theme.SettingsSectionGap).Children(func() {
		if m.RenderAISettings != nil {
			m.RenderAISettings(c)
		} else {
			internalui.SettingsSection(c, func() {
				internalui.SettingsSectionTitle(c, theme.Tr("AI Agent", "AI Agent"), nil)
				ui.Text(c, theme.Tr("在工作区接入 AI 提供商与模型配置。", "Configure AI providers and model backends in the workspace.")).
					FontSize(theme.FontBody).TextColor(pal.TextSecondary)
			})
		}
		if m.PluginHost != nil {
			m.PluginHost.RenderSettingsSections(c, "ai", m.selectSettingsTab)
		}
	})
}

func (m *Manager) renderSettingsAppearanceTab(c *ui.Context) {
	pal := theme.Ocean()
	st := m.currentState()

	ui.Column(c.Key("settings-appearance-body")).FillWidth().Gap(theme.SettingsSectionGap).Children(func() {
		// Card 1: Theme & Language (app_settings_tabs.rs:64-66).
		internalui.SettingsSection(c, func() {
			internalui.SettingsSectionTitle(c, theme.Tr("主题与语言", "Theme & Language"), nil)

			curTheme := st.Theme
			if curTheme == "" {
				curTheme = theme.CurrentThemeMode()
			}
			themeIdx := 0
			switch curTheme {
			case model.ThemeLight:
				themeIdx = 1
			case model.ThemeDark:
				themeIdx = 2
			}
			internalui.SettingsRow(c, theme.Tr("外观主题", "Theme"), func() {
				if internalui.SettingsSegmented(c, "settings-theme-seg", &themeIdx, []internalui.SegmentOption{
					{ID: "settings-theme-system", Label: theme.Tr("跟随系统", "System")},
					{ID: "settings-theme-light", Label: theme.Tr("浅色", "Light")},
					{ID: "settings-theme-dark", Label: theme.Tr("深色", "Dark")},
				}) {
					modes := []model.ThemeMode{model.ThemeSystem, model.ThemeLight, model.ThemeDark}
					picked := modes[themeIdx]
					sysDark := c.Theme().Dark
					if mygo.App.IsReady() {
						sysDark = mygo.Theme.IsDark()
					}
					theme.SetThemeMode(picked, sysDark)
					if mygo.App.IsReady() {
						switch picked {
						case model.ThemeDark:
							mygo.Theme.SetSource(mygo.ThemeDark)
						case model.ThemeLight:
							mygo.Theme.SetSource(mygo.ThemeLight)
						default:
							mygo.Theme.SetSource(mygo.ThemeSystem)
						}
					}
					m.mutateState(func(s *model.AppState) {
						s.Theme = picked
					})
				}
			})

			langIdx := 0
			if theme.Language() == model.LangEn {
				langIdx = 1
			}
			internalui.SettingsRow(c, theme.Tr("界面语言", "Language"), func() {
				if internalui.SettingsSegmented(c, "settings-lang-seg", &langIdx, []internalui.SegmentOption{
					{ID: "settings-lang-zh", Label: "简体中文"},
					{ID: "settings-lang-en", Label: "English"},
				}) {
					picked := []model.LanguageMode{model.LangZh, model.LangEn}[langIdx]
					theme.SetLanguage(picked)
					m.mutateState(func(s *model.AppState) {
						s.Language = picked
					})
				}
			})

			density := st.BrandDeco.EffectiveDensity()
			decoOn := st.BrandDeco.Mode != model.BrandDecoOff
			decoVal := "rich"
			switch {
			case !decoOn:
				decoVal = "off"
			case density == model.BrandDecoDensityMinimal:
				decoVal = "minimal"
			case density == model.BrandDecoDensityCalm:
				decoVal = "calm"
			}
			decoVals := []string{"off", "minimal", "calm", "rich"}
			decoIdx := slices.Index(decoVals, decoVal)
			internalui.SettingsRow(c, theme.Tr("侧栏装饰动画", "Sidebar Decoration"), func() {
				if internalui.SettingsSegmented(c, "settings-brand-deco-seg", &decoIdx, []internalui.SegmentOption{
					{ID: "settings-brand-deco-off", Label: theme.Tr("关闭", "Off")},
					{ID: "settings-brand-deco-minimal", Label: theme.Tr("简约", "Minimal")},
					{ID: "settings-brand-deco-calm", Label: theme.Tr("舒缓", "Calm")},
					{ID: "settings-brand-deco-rich", Label: theme.Tr("丰富", "Rich")},
				}) {
					decoVal = decoVals[decoIdx]
					m.mutateState(func(s *model.AppState) {
						s.BrandDeco.Density = density
						s.BrandDeco.Motion = model.BrandDecoMotionRich
						if decoVal == "off" {
							s.BrandDeco.Mode = model.BrandDecoOff
							return
						}
						if s.BrandDeco.Mode == model.BrandDecoOff {
							s.BrandDeco.Mode = model.BrandDecoByTime
						}
						switch decoVal {
						case "minimal":
							s.BrandDeco.Density = model.BrandDecoDensityMinimal
						case "calm":
							s.BrandDeco.Density = model.BrandDecoDensityCalm
						default:
							s.BrandDeco.Density = model.BrandDecoDensityRich
						}
					})
				}
			})
		})

		// Card 2: Editor Typography & Fonts (app_settings_tabs.rs:67-70).
		edFont, cdFont, lh := theme.Typography()
		if strings.TrimSpace(st.EditorFont) != "" {
			edFont = st.EditorFont
		}
		if strings.TrimSpace(st.CodeFont) != "" {
			cdFont = st.CodeFont
		}
		if st.LineHeight != "" {
			lh = st.LineHeight
		}
		curEpoch := theme.FontEpoch()
		if len(m.fontFamilies) == 0 || m.fontFamiliesEpoch != curEpoch {
			m.fontFamilies = ui.FontFamilies()
			m.fontFamiliesEpoch = curEpoch
		}
		isDefaultBody := strings.TrimSpace(edFont) == ""
		edDisplay := strings.TrimSpace(edFont)
		if isDefaultBody {
			edDisplay = defaultBodyFontPrimary(m.fontFamilies)
		}
		cdDisplay := strings.TrimSpace(cdFont)
		if cdDisplay == "" {
			cdDisplay = resolvedCodeFontName(m.fontFamilies)
		}

		internalui.SettingsSection(c, func() {
			internalui.SettingsSectionTitle(c, theme.Tr("编辑器", "Editor"), nil)

			internalui.SettingsRow(c, theme.Tr("正文字体", "Body Font"), func() {
				m.fontPickerTrigger(c, "settings-body-font-trigger", "body", edDisplay, isDefaultBody)
			})
			internalui.SettingsRow(c, theme.Tr("代码字体", "Code Font"), func() {
				m.fontPickerTrigger(c, "settings-code-font-trigger", "code", cdDisplay, false)
			})

			lhIdx := 1
			switch lh {
			case model.LineHeightTight:
				lhIdx = 0
			case model.LineHeightLoose:
				lhIdx = 2
			}
			internalui.SettingsRow(c, theme.Tr("行高", "Line Height"), func() {
				if internalui.SettingsSegmented(c, "settings-lh-seg", &lhIdx, []internalui.SegmentOption{
					{ID: "settings-lh-tight", Label: theme.Tr("紧凑", "Compact")},
					{ID: "settings-lh-normal", Label: theme.Tr("标准", "Standard")},
					{ID: "settings-lh-loose", Label: theme.Tr("宽松", "Relaxed")},
				}) {
					modes := []model.LineHeightMode{model.LineHeightTight, model.LineHeightNormal, model.LineHeightLoose}
					picked := modes[lhIdx]
					theme.SetTypography(edFont, cdFont, picked)
					m.mutateState(func(s *model.AppState) {
						s.LineHeight = picked
					})
				}
			})
		})

		// Card 3: Application Updates & Channel Source.
		internalui.SettingsSection(c, func() {
			internalui.SettingsSectionTitle(c, theme.Tr("应用更新", "App Updates"), nil)

			curVer := updater.CurrentVersion()
			var snap updater.Snapshot
			if m.Updater != nil {
				snap = m.Updater.Snapshot()
				curVer = snap.CurrentVersion
			}

			verTitle := fmt.Sprintf("v%s", curVer)
			internalui.SettingsRow(c, theme.Tr("当前版本", "Version"), func() {
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space10).Children(func() {
					ui.Text(c, verTitle).Font("monospace").FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
					if snap.HasActionableUpdate() {
						btnLabel := fmt.Sprintf(theme.Tr("更新至 v%s", "Update to v%s"), snap.LatestVersion)
						if snap.Status == updater.StatusReady {
							btnLabel = fmt.Sprintf(theme.Tr("重启更新到 v%s", "Restart to v%s"), snap.LatestVersion)
						}
						btn := ui.ButtonBase(c.Key("settings-open-updater")).Height(theme.HeightCompact).PaddingX(theme.Space10).
							Gap(theme.Space6).AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(btnLabel)
						if btn.Hovered() || btn.Pressed() {
							btn.Background(pal.Selected)
						}
						btn.Children(func() {
							ui.Box(c).Size(theme.UpdaterBadgeDotSize, theme.UpdaterBadgeDotSize).Shrink(0).
								Radius(theme.RadiusPill).Background(pal.Accent)
							ui.Text(c, btnLabel).FontSize(theme.FontSmall).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
						})
						internalui.Probe(btn, "settings-open-updater")
						if btn.Clicked() {
							m.OpenUpdater()
						}
					} else {
						checking := snap.Status == updater.StatusChecking
						btnLabel := theme.Tr("检查更新", "Check for Updates")
						if checking {
							btnLabel = theme.Tr("正在检查…", "Checking…")
						}
						refIc := icons.Refresh
						btn := internalui.ActionButtonWithOptions(c, internalui.ActionButtonOptions{ID: "settings-check-updates", Label: btnLabel, Leading: &refIc, Style: internalui.ButtonSecondary, Disabled: checking, Loading: checking, Height: theme.HeightCompact, PaddingX: theme.Space10})
						if btn.Clicked() {
							m.OpenUpdater()
							if m.Updater != nil {
								m.Updater.CheckAsync(true)
							}
						}
					}
				})
			})

			srcMode := updater.NormalizeSourceMode(st.UpdateSource)
			if m.Updater != nil {
				srcMode = snap.SourceMode
			}
			srcModes := []updater.SourceMode{updater.SourceAuto, updater.SourceMirror, updater.SourceGitHub}
			srcIdx := max(slices.Index(srcModes, srcMode), 0)
			internalui.SettingsRow(c, theme.Tr("更新源", "Update Source"), func() {
				if internalui.SettingsSegmented(c, "settings-update-source-seg", &srcIdx, []internalui.SegmentOption{
					{ID: "settings-upd-src-auto", Label: theme.Tr("自动", "Auto")},
					{ID: "settings-upd-src-mirror", Label: theme.Tr("国内加速", "Mirror")},
					{ID: "settings-upd-src-github", Label: "GitHub"},
				}) {
					picked := srcModes[srcIdx]
					if m.Updater != nil {
						m.Updater.SetSourceMode(picked)
					} else {
						m.mutateState(func(s *model.AppState) {
							s.UpdateSource = string(picked)
						})
					}
				}
			})

			telemetryOn := !st.TelemetryOff
			telemetryLabel := theme.Tr("匿名使用统计", "Anonymous Usage Stats")
			telemetryRow := internalui.SettingsRow(c, telemetryLabel, func() {
				if internalui.TogglePill(c, "settings-telemetry-pill", telemetryLabel, &telemetryOn, false) {
					off := !telemetryOn
					m.mutateState(func(s *model.AppState) {
						s.TelemetryOff = off
					})
				}
			})
			internalui.WrappedTooltip(c, telemetryRow, theme.Tr(
				"每天最多发送一次：随机安装编号、版本、系统及大版本、CPU 架构、界面语言；服务器据网络来源记录国家/地区，不保存 IP。不含笔记内容或文件名。",
				"Sent at most once a day: a random install ID, app version, OS and major version, CPU architecture and UI language. The server records the country from the network origin without storing the IP. No note content or file names.",
			))
		})

		if m.PluginHost != nil {
			m.PluginHost.RenderSettingsSections(c, "appearance", m.selectSettingsTab)
		}
	})
	_ = pal
}

func pickInstalledFont(families []ui.FontFamily, candidates []string, fallback string) string {
	for _, cand := range candidates {
		for _, f := range families {
			if f.Name == cand {
				return cand
			}
		}
	}
	return fallback
}

func resolvedBodyFontName(families []ui.FontFamily) string {
	return pickInstalledFont(families, []string{
		"HarmonyOS Sans SC", "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", "Noto Sans SC",
		"Hiragino Sans GB", "Helvetica Neue", "Segoe UI", "DejaVu Sans",
	}, defaultBodyFontName())
}

func resolvedCodeFontName(families []ui.FontFamily) string {
	return pickInstalledFont(families, []string{
		"JetBrains Mono", "Menlo", "Monaco", "Cascadia Mono",
		"Consolas", "DejaVu Sans Mono", "Liberation Mono",
	}, defaultCodeFontName())
}

func defaultBodyFontPrimary(families []ui.FontFamily) string {
	switch runtime.GOOS {
	case "darwin", "js":
		return "SF Pro"
	case "windows":
		return "Segoe UI"
	default:
		return pickInstalledFont(families, []string{
			"Inter", "Cantarell", "Ubuntu", "DejaVu Sans", "Noto Sans CJK SC", "Noto Sans SC",
		}, "System UI")
	}
}

func defaultFontSuffix() (text string, gap float32) {
	if theme.IsEn() {
		return "(Default)", 4
	}
	return "（默认）", 0
}

func defaultBodyFontLabel(families []ui.FontFamily) string {
	primary := defaultBodyFontPrimary(families)
	suffix, gap := defaultFontSuffix()
	if gap > 0 {
		return primary + " " + suffix
	}
	return primary + suffix
}

func defaultBodyFontName() string {
	return "HarmonyOS Sans SC"
}

func defaultCodeFontName() string {
	switch runtime.GOOS {
	case "darwin", "js":
		return "Menlo"
	case "windows":
		return "Consolas"
	default:
		return "DejaVu Sans Mono"
	}
}

func (m *Manager) fontPickerTrigger(c *ui.Context, id, role, label string, isDefault bool) {
	pal := theme.Ocean()
	open := m.fontPickerOpen == role
	borderCol := ui.Transparent
	accLabel := label
	if isDefault {
		suffix, gap := defaultFontSuffix()
		if gap > 0 {
			accLabel = label + " " + suffix
		} else {
			accLabel = label + suffix
		}
	}
	btn := ui.ButtonBase(c.Key(id)).MaxWidth(248).Shrink(1).MinWidth(0).Height(theme.HeightCompact).Padding(0, theme.Space8, 0, theme.Space10).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Radius(theme.RadiusMD).
		Cursor(ui.CursorPointer).Label(accLabel)
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	bg := ui.Transparent
	if pressed || open {
		bg = pal.Selected
	} else if hovered {
		bg = pal.Hover
	}
	internalui.AnimateSurfaceColors(btn, bg, borderCol, true)
	internalui.TactileCard(btn, hovered, pressed, open, false)
	btn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		if m.fontTriggerRects == nil {
			m.fontTriggerRects = make(map[string]ui.Rect)
		}
		if open && m.fontTriggerRects[role] != r {
			m.requestRedraw()
		}
		m.fontTriggerRects[role] = r
		internalui.RecordBounds(id, r)
	})
	btn.Children(func() {
		if isDefault {
			suffix, gap := defaultFontSuffix()
			ui.Row(c).AlignItems(ui.Center).Gap(gap).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, label).FontSize(theme.FontBody).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
				ui.Text(c, suffix).FontSize(theme.FontBody).TextColor(pal.MenuMuted).SingleLine().Shrink(0)
			})
		} else {
			ui.Text(c, label).FontSize(theme.FontBody).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
		}
		glyph := icons.ChevronDown
		if open {
			glyph = icons.ChevronUp
		}
		ic := pal.DuoIconColors(pal.TextSecondary)
		if open || hovered || pressed {
			ic = pal.DuoIconColors(pal.Text)
		}
		icons.DrawAnimated(c, glyph, theme.IconMicro, ic, open || hovered || pressed)
	})
	if btn.Clicked() {
		if open {
			m.fontPickerOpen = ""
		} else {
			m.openFontPicker(role)
		}
	}
}

const (
	fontPickerRowH          float32 = theme.HeightCompact
	fontPickerVirtThreshold         = 24
	fontPickerVirtOverscan          = 4
)

type fontPickerCache struct {
	catalogRole        string
	catalogSrc         []ui.FontFamily
	catalog            []fontCandidateItem
	catalogGen         uint64
	filteredValid      bool
	filteredRole       string
	filteredQuery      string
	filteredCatalogGen uint64
	filtered           []ui.FontFamily
}

func (m *Manager) openFontPicker(role string) {
	m.fontPickerOpen = role
	m.fontQuery = ""
	m.fontCursor = -1
	m.fontScrollToCursor = false
	m.fontScroll = ui.ScrollState{}
	base := m.fontFamilies
	if len(base) == 0 {
		base = ui.FontFamilies()
	}
	if m.dataDir != "" {
		if entries, err := os.ReadDir(filepath.Join(m.dataDir, "fonts")); err == nil {
			seen := make(map[string]bool, len(base))
			for _, f := range base {
				if key := strings.ToLower(strings.TrimSpace(f.Name)); key != "" {
					seen[key] = true
				}
			}
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(ent.Name()))
				if ext == ".ttf" || ext == ".otf" || ext == ".ttc" {
					fam := strings.TrimSpace(strings.TrimSuffix(ent.Name(), filepath.Ext(ent.Name())))
					if fam == "" {
						continue
					}
					key := strings.ToLower(fam)
					if seen[key] {
						continue
					}
					seen[key] = true
					base = append(base, ui.FontFamily{Name: fam, Monospaced: isMonospaceFontName(fam)})
				}
			}
		}
	}
	m.fontFamilies = base
}

type fontCandidateItem struct {
	name      string
	searchKey string
	mono      bool
	words     []string
	subwords  []string
}

func isMonospaceFontName(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "mono") ||
		strings.Contains(lower, "consolas") ||
		strings.Contains(lower, "menlo") ||
		strings.Contains(lower, "monaco") ||
		strings.Contains(lower, "courier") {
		return true
	}
	_, subwords := tokenizeFontName(name)
	return slices.Contains(subwords, "code")
}

func isFontNameDelimiter(r rune) bool {
	return unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '.' || r == '(' || r == ')'
}

func tokenizeFontName(name string) (words []string, subwords []string) {
	rawWords := strings.FieldsFunc(name, isFontNameDelimiter)
	for _, rw := range rawWords {
		if rw == "" {
			continue
		}
		words = append(words, strings.ToLower(rw))
		runes := []rune(rw)
		start := 0
		for i := 1; i < len(runes); i++ {
			prev, curr := runes[i-1], runes[i]
			var next rune
			hasNext := i+1 < len(runes)
			if hasNext {
				next = runes[i+1]
			}
			camel := (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(curr)
			acronym := unicode.IsUpper(prev) && unicode.IsUpper(curr) && hasNext && unicode.IsLower(next)
			digitEdge := (unicode.IsLetter(prev) != unicode.IsLetter(curr)) && (unicode.IsDigit(prev) || unicode.IsDigit(curr))
			if camel || acronym || digitEdge {
				subwords = append(subwords, strings.ToLower(string(runes[start:i])))
				start = i
			}
		}
		subwords = append(subwords, strings.ToLower(string(runes[start:])))
	}
	return words, subwords
}

func scoreFontCandidate(item fontCandidateItem, q string, qTokens []string) (tier int, pos int, ok bool) {
	if item.searchKey == q {
		return 0, 0, true
	}
	if strings.HasPrefix(item.searchKey, q) {
		return 1, 0, true
	}
	for i := 1; i < len(item.words); i++ {
		if strings.HasPrefix(item.words[i], q) {
			return 2, i, true
		}
	}
	for i := 1; i < len(item.subwords); i++ {
		if strings.HasPrefix(item.subwords[i], q) {
			return 3, i, true
		}
	}
	if len(qTokens) > 1 {
		swIdx := 0
		firstPos := -1
		allMatched := true
		for _, tok := range qTokens {
			found := false
			for swIdx < len(item.subwords) {
				if strings.HasPrefix(item.subwords[swIdx], tok) {
					if firstPos < 0 {
						firstPos = swIdx
					}
					swIdx++
					found = true
					break
				}
				swIdx++
			}
			if !found {
				allMatched = false
				break
			}
		}
		if allMatched {
			if firstPos < 0 {
				firstPos = 0
			}
			return 3, firstPos, true
		}
	}
	isASCII := true
	for _, r := range q {
		if r > 127 {
			isASCII = false
			break
		}
	}
	if !isASCII || len(q) >= 3 {
		if idx := strings.Index(item.searchKey, q); idx >= 0 {
			return 4, idx, true
		}
	}
	if sc, ok := fuzzy.Score(q, []string{item.name}, nil, false); ok {
		return 5, -sc, true
	}
	return 0, 0, false
}

func buildSortedFontCatalog(families []ui.FontFamily, role string) []fontCandidateItem {
	raw := make([]ui.FontFamily, 0, len(families)+2)
	raw = append(raw, families...)
	raw = append(raw,
		ui.FontFamily{Name: resolvedBodyFontName(families)},
		ui.FontFamily{Name: resolvedCodeFontName(families), Monospaced: true},
	)

	seen := make(map[string]bool, len(raw))
	catalog := make([]fontCandidateItem, 0, len(raw))
	for _, f := range raw {
		trimmed := strings.TrimSpace(f.Name)
		if trimmed == "" || strings.HasPrefix(trimmed, ".") {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		words, subwords := tokenizeFontName(trimmed)
		catalog = append(catalog, fontCandidateItem{
			name:      trimmed,
			searchKey: key,
			mono:      isMonospaceFontName(trimmed),
			words:     words,
			subwords:  subwords,
		})
	}

	slices.SortFunc(catalog, func(a, b fontCandidateItem) int {
		if role == "code" && a.mono != b.mono {
			if a.mono {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.searchKey, b.searchKey)
	})
	return catalog
}

func (m *Manager) sortedFontCatalog(role string) []fontCandidateItem {
	if m.fontCache.catalogRole == role && slices.Equal(m.fontCache.catalogSrc, m.fontFamilies) {
		return m.fontCache.catalog
	}
	catalog := buildSortedFontCatalog(m.fontFamilies, role)
	m.fontCache.catalogRole = role
	m.fontCache.catalogSrc = slices.Clone(m.fontFamilies)
	m.fontCache.catalog = catalog
	m.fontCache.catalogGen++
	m.fontCache.filteredValid = false
	return catalog
}

func (m *Manager) filteredFonts() []ui.FontFamily {
	role := m.fontPickerOpen
	if role == "" {
		role = "body"
	}
	catalog := m.sortedFontCatalog(role)
	if m.fontCache.filteredValid &&
		m.fontCache.filteredRole == role &&
		m.fontCache.filteredQuery == m.fontQuery &&
		m.fontCache.filteredCatalogGen == m.fontCache.catalogGen {
		return m.fontCache.filtered
	}

	q := strings.ToLower(strings.TrimSpace(m.fontQuery))
	var out []ui.FontFamily
	if q == "" {
		out = make([]ui.FontFamily, len(catalog))
		for i, c := range catalog {
			out[i] = ui.FontFamily{Name: c.name, Monospaced: c.mono}
		}
	} else {
		qTokens := strings.Fields(q)
		type scored struct {
			tier int
			pos  int
			item fontCandidateItem
		}
		var ranked []scored
		for _, item := range catalog {
			if tier, pos, ok := scoreFontCandidate(item, q, qTokens); ok {
				ranked = append(ranked, scored{tier: tier, pos: pos, item: item})
			}
		}
		slices.SortFunc(ranked, func(a, b scored) int {
			if c := cmp.Compare(a.tier, b.tier); c != 0 {
				return c
			}
			if c := cmp.Compare(a.pos, b.pos); c != 0 {
				return c
			}
			return cmp.Compare(a.item.searchKey, b.item.searchKey)
		})
		out = make([]ui.FontFamily, len(ranked))
		for i, r := range ranked {
			out[i] = ui.FontFamily{Name: r.item.name, Monospaced: r.item.mono}
		}
	}

	m.fontCache.filteredValid = true
	m.fontCache.filteredRole = role
	m.fontCache.filteredQuery = m.fontQuery
	m.fontCache.filteredCatalogGen = m.fontCache.catalogGen
	m.fontCache.filtered = out
	return out
}

func (m *Manager) selectFont(role, name string) {
	edFont, cdFont, lh := theme.Typography()
	if role == "body" {
		edFont = name
		theme.SetTypography(edFont, cdFont, lh)
		m.mutateState(func(s *model.AppState) {
			s.EditorFont = name
		})
	} else {
		cdFont = name
		theme.SetTypography(edFont, cdFont, lh)
		m.mutateState(func(s *model.AppState) {
			s.CodeFont = name
		})
	}
	m.fontPickerOpen = ""
}

func (m *Manager) renderFontPickerOverlay(c *ui.Context) {
	pal := theme.Ocean()
	role := m.fontPickerOpen
	st := m.currentState()
	edFont, cdFont, _ := theme.Typography()
	if strings.TrimSpace(st.EditorFont) != "" {
		edFont = st.EditorFont
	}
	if strings.TrimSpace(st.CodeFont) != "" {
		cdFont = st.CodeFont
	}
	current := strings.TrimSpace(edFont)
	if role == "code" {
		current = strings.TrimSpace(cdFont)
		if current == "" {
			current = resolvedCodeFontName(m.fontFamilies)
		}
	}

	winW, winH := c.Size()
	if winW <= 0 {
		winW = 1100
	}
	if winH <= 0 {
		winH = 760
	}

	anchor := m.fontTriggerRects[role]
	if anchor.W <= 0 || anchor.H <= 0 {
		return
	}

	popW := min(float32(300), max(winW-16, 1))
	spec := internalui.PlaceBelow(anchor).WithAlignX(internalui.AlignX{Kind: internalui.AlignRight}).
		WithGap(6).WithMargin(8).WithReserveHeight(304)
	maxH := internalui.MaxHeightForSpec(spec, winW, winH, 304)
	listCapH := max(maxH-54, 48)

	fonts := m.filteredFonts()
	showDefaultBodyOpt := role == "body" && strings.TrimSpace(m.fontQuery) == ""
	rowCount := len(fonts)
	if showDefaultBodyOpt {
		rowCount++
	}
	listH := float32(48)
	if rowCount > 0 {
		listH = min(float32(min(rowCount, 7))*34, listCapH)
	}
	popH := min(listH+54, maxH)
	x, y := internalui.ResolveOrigin(spec, popW, popH, winW, winH)

	ui.Overlay(c, func() {
		backdrop := ui.Box(c.Key("font-picker-backdrop")).Absolute().Left(0).Top(0).Right(0).Bottom(0)

		panel := internalui.Elevate(ui.Column(c.Key("font-picker-menu-"+role)).Absolute().Left(x).Top(y).
			Width(popW).MaxHeight(maxH).Padding(theme.Space6).Gap(theme.Space4).Radius(theme.RadiusXL).
			Background(pal.Menu).Border(1, pal.MenuBorder).Clip().
			Transition(internalui.PopoverSpringTransition()), theme.ElevationPopover)
		internalui.ObserveMenuPointer(c, panel, &m.fontCursor)
		internalui.Probe(panel, "font-picker-menu")

		panel.Children(func() {
			// Search bar matching font_picker.rs:100-105.
			ui.Row(c.Key("font-picker-search-bar")).FillWidth().Padding(theme.Space2, theme.Space8, theme.Space6, theme.Space8).Gap(theme.Space8).
				AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.MenuSeparator).
				Children(func() {
					icons.Draw(c, icons.FindSearch, theme.IconRow, pal.DuoIconColors(pal.TextSecondary))
					in := ui.TextInput(c.Key("font-picker-search"), &m.fontQuery).
						Placeholder(theme.Tr("搜索字体", "Search fonts")).
						PlaceholderColor(pal.Placeholder).
						Label(theme.Tr("搜索字体", "Search fonts")).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
						Height(theme.HeightCompact).Padding(0).
						Background(ui.Transparent).Border(0, ui.Transparent).
						Bind(&m.fontSearchHandle).AutoFocus().Grow(1).MinWidth(0)
					internalui.Probe(in, "font-picker-search")
					if in.Changed() {
						m.fontCursor = 0
						m.fontScroll.Y = 0
						fonts = m.filteredFonts()
						curRows := len(fonts)
						if role == "body" && strings.TrimSpace(m.fontQuery) == "" {
							curRows++
						}
						curListH := float32(48)
						if curRows > 0 {
							curListH = min(float32(min(curRows, 7))*fontPickerRowH, listCapH)
						}
						nx, ny := internalui.ResolveOrigin(spec, popW, min(curListH+54, maxH), winW, winH)
						panel.Left(nx).Top(ny)
					}
					in.OnTextKey(func(k ui.TextKey) bool {
						hasDef := role == "body" && strings.TrimSpace(m.fontQuery) == ""
						total := len(fonts)
						if hasDef {
							total++
						}
						switch k.Key {
						case ui.KeyDown:
							if total > 0 {
								m.fontCursor = (m.fontCursor + 1 + total) % total
								m.fontScrollToCursor = true
							}
							return true
						case ui.KeyUp:
							if total > 0 {
								if m.fontCursor <= 0 {
									m.fontCursor = total - 1
								} else {
									m.fontCursor--
								}
								m.fontScrollToCursor = true
							}
							return true
						case ui.KeyEnter:
							if m.fontCursor >= 0 && m.fontCursor < total {
								if hasDef {
									if m.fontCursor == 0 {
										m.selectFont(role, "")
									} else {
										m.selectFont(role, fonts[m.fontCursor-1].Name)
									}
								} else {
									m.selectFont(role, fonts[m.fontCursor].Name)
								}
							}
							return true
						case ui.KeyEscape:
							m.fontPickerOpen = ""
							return true
						}
						return false
					})
				})

			hasDefaultRow := role == "body" && strings.TrimSpace(m.fontQuery) == ""
			curRows := len(fonts)
			if hasDefaultRow {
				curRows++
			}
			curListH := float32(48)
			if curRows > 0 {
				curListH = min(float32(min(curRows, 7))*fontPickerRowH, listCapH)
			}
			rowOffset := 0
			if hasDefaultRow {
				rowOffset = 1
			}
			if m.fontScrollToCursor && m.fontCursor >= 0 && m.fontCursor < curRows {
				itemTop := float32(m.fontCursor) * fontPickerRowH
				itemBot := itemTop + fontPickerRowH
				if itemTop < m.fontScroll.Y {
					m.fontScroll.Y = itemTop
				} else if itemBot > m.fontScroll.Y+curListH {
					m.fontScroll.Y = itemBot - curListH
				}
				m.fontScrollToCursor = false
			}
			internalui.Scroll(c.Key("font-picker-list-"+role), &m.fontScroll).
				FillWidth().Height(curListH).Gap(0).Children(func() {
				if curRows == 0 {
					ui.Row(c).FillWidth().Height(48).PaddingX(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, theme.Tr("没有匹配的字体", "No matching fonts")).FontSize(theme.FontBody).TextColor(pal.MenuMuted)
					})
					return
				}

				startRow, endRow := 0, curRows
				if curRows > fontPickerVirtThreshold {
					scrollY := max(m.fontScroll.Y, 0)
					startRow = max(int(scrollY/fontPickerRowH)-fontPickerVirtOverscan, 0)
					endRow = min(int((scrollY+curListH)/fontPickerRowH)+1+fontPickerVirtOverscan, curRows)
				}
				if startRow > 0 {
					ui.Box(c.Key("font-row-spacer-top")).FillWidth().Height(float32(startRow) * fontPickerRowH).Shrink(0)
				}

				for rowIdx := startRow; rowIdx < endRow; rowIdx++ {
					if hasDefaultRow && rowIdx == 0 {
						defPrimary := defaultBodyFontPrimary(m.fontFamilies)
						defLabel := defaultBodyFontLabel(m.fontFamilies)
						defSelected := current == ""
						defHighlighted := m.fontCursor == 0
						defRow := ui.ButtonBase(c.Key("font-row-default")).FillWidth().Height(fontPickerRowH).PaddingX(10).
							AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Radius(theme.RadiusMD).
							Cursor(ui.CursorPointer).Label(defLabel)
						internalui.RevealOnHighlight(defRow, defHighlighted)
						defHov := m.fontCursor < 0 && defRow.Hovered()
						defPressed := defRow.Pressed()
						defBG := ui.Transparent
						if defHighlighted || defHov {
							defBG = pal.MenuHover
						}
						internalui.AnimateSurfaceColors(defRow, defBG, ui.Transparent, false)
						defRow.Children(func() {
							suffix, gap := defaultFontSuffix()
							ui.Row(c).AlignItems(ui.Center).Gap(gap).Grow(1).MinWidth(0).Children(func() {
								ui.Text(c, defPrimary).FontSize(theme.FontBody).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
								ui.Text(c, suffix).FontSize(theme.FontBody).TextColor(pal.MenuMuted).SingleLine().Shrink(0)
							})
							if defSelected {
								icons.DrawAnimated(c, icons.Check, theme.IconInline, pal.MinimalIconColors(pal.Accent), defSelected || defHov || defPressed)
							}
						})
						if defRow.Clicked() {
							m.selectFont(role, "")
						}
						continue
					}

					i := rowIdx - rowOffset
					f := fonts[i]
					selected := strings.EqualFold(f.Name, current)
					highlighted := rowIdx == m.fontCursor
					row := ui.ButtonBase(c.Key("font-row-" + f.Name)).FillWidth().Height(fontPickerRowH).PaddingX(10).
						AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusMD).
						Cursor(ui.CursorPointer).Label(f.Name)
					internalui.RevealOnHighlight(row, highlighted)
					hovered := m.fontCursor < 0 && row.Hovered()
					pressed := row.Pressed()
					bg := ui.Transparent
					if highlighted || hovered {
						bg = pal.MenuHover
					}
					internalui.AnimateSurfaceColors(row, bg, ui.Transparent, false)
					row.Children(func() {
						var marks [][2]int
						if strings.TrimSpace(m.fontQuery) != "" {
							marks = fuzzy.HighlightRanges(f.Name, m.fontQuery)
						}
						internalui.HighlightedText(c, f.Name, marks, theme.FontBody, 0, pal.Text, pal.SearchHighlight).Grow(1).MinWidth(0)
						if selected {
							icons.DrawAnimated(c, icons.Check, theme.IconInline, pal.MinimalIconColors(pal.Accent), selected || hovered || pressed)
						}
					})
					if row.Clicked() {
						m.selectFont(role, f.Name)
					}
				}

				if endRow < curRows {
					ui.Box(c.Key("font-row-spacer-bot")).FillWidth().Height(float32(curRows-endRow) * fontPickerRowH).Shrink(0)
				}
			})
		})

		if panel.OverlayShortcut(0, ui.KeyEscape) || panel.PressedOutside() || backdrop.Clicked() {
			m.fontPickerOpen = ""
		}
	})
}

func (m *Manager) renderSettingsFoldersTab(c *ui.Context) {
	pal := theme.Ocean()
	st := m.currentState()
	hideNonMD := st.HideNonMarkdownFiles == nil || *st.HideNonMarkdownFiles
	showNonMD := !hideNonMD
	showHidden := st.ShowHiddenFiles
	hideAssets := st.HideAssetsFolder == nil || *st.HideAssetsFolder
	assetsName := st.AssetsFolder
	if strings.TrimSpace(assetsName) == "" {
		assetsName = "assets"
	}

	ui.Column(c.Key("settings-folders-body")).FillWidth().Gap(theme.SettingsSectionGap).Children(func() {
		// Sidebar visibility: flat rows separated by hairlines, no nested cards.
		ui.Column(c.Key("settings-folders-sidebar")).FillWidth().Children(func() {
			internalui.SettingsSectionTitle(c, theme.Tr("侧栏", "Sidebar"), nil)

			showRow := internalui.SettingsRowFrame(ui.Row(c.Key("folder-row-non-md"))).Justify(ui.SpaceBetween)
			showRow.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				internalui.RecordBounds("folder-hide-non-md", r)
				internalui.RecordBounds("settings-hide-non-md", r)
			})
			showRow.Children(func() {
				ui.Text(c, theme.Tr("显示非 Markdown 文件", "Show Non-Markdown Files")).
					FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
				if internalui.TogglePill(c, "settings-show-non-md-pill", theme.Tr("显示非 Markdown 文件", "Show Non-Markdown Files"), &showNonMD, false) {
					nextHide := !showNonMD
					m.mutateState(func(s *model.AppState) {
						v := nextHide
						s.HideNonMarkdownFiles = &v
					})
				}
			})

			hiddenFilesRow := internalui.SettingsRowFrame(ui.Row(c.Key("folder-row-hidden-files"))).Justify(ui.SpaceBetween)
			hiddenFilesRow.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				internalui.RecordBounds("folder-show-hidden-files", r)
				internalui.RecordBounds("settings-show-hidden-files", r)
			})
			hiddenFilesRow.Children(func() {
				ui.Text(c, theme.Tr("显示隐藏文件和文件夹", "Show Hidden Files and Folders")).
					FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
				if internalui.TogglePill(c, "settings-show-hidden-files-pill", theme.Tr("显示隐藏文件和文件夹", "Show Hidden Files and Folders"), &showHidden, false) {
					nextShow := showHidden
					m.mutateState(func(s *model.AppState) {
						s.ShowHiddenFiles = nextShow
					})
				}
			})

			rules := append([]model.FolderRule(nil), st.FolderRules...)
			internalui.SettingsRowFrame(ui.Row(c.Key("folder-row-hidden"))).PaddingY(theme.Space8).
				AlignItems(ui.Start).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Height(theme.HeightCompact).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Text(c, theme.Tr("隐藏的文件夹", "Hidden Folders")).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
				})
				ui.Row(c).Grow(1).MinWidth(0).Wrap().Justify(ui.End).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
					if hideAssets {
						if folderChip(c, "settings-hide-assets", assetsName, false) {
							m.mutateState(func(s *model.AppState) {
								v := false
								s.HideAssetsFolder = &v
							})
						}
					}
					for i, r := range rules {
						if r.Regex || strings.EqualFold(r.Pattern, assetsName) {
							continue
						}
						idx := i
						if folderChip(c, "folder-rule-del-"+r.Pattern, r.Pattern, !r.Hidden) {
							next := append(rules[:idx:idx], rules[idx+1:]...)
							m.saveFolderRules(next)
						}
					}
					m.renderFolderRuleAdd(c, assetsName)
				})
			})
			if m.folderRuleError != "" {
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					ui.Text(c, m.folderRuleError).FontSize(theme.FontMeta).TextColor(pal.Accent)
				})
			}
		})

		// Global search: every notebook is searchable by default; only exclusions are listed.
		ignoreCol := ui.Column(c.Key("settings-ignore-search")).FillWidth()
		internalui.Probe(ignoreCol, "settings-ignore-search")
		ignoreCol.Children(func() {
			internalui.SettingsSectionTitle(c, theme.Tr("全局搜索", "Global Search"), nil)
			internalui.SettingsRowFrame(ui.Row(c.Key("folder-row-search-excluded"))).PaddingY(theme.Space8).
				AlignItems(ui.Start).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Height(theme.HeightCompact).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Text(c, theme.Tr("不参与搜索的文件夹", "Excluded Folders")).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
				})
				ui.Row(c).Grow(1).MinWidth(0).Wrap().Justify(ui.End).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
					for i, folderPath := range st.IgnoredSearchFolders {
						if folderChip(c, fmt.Sprintf("folder-search-ignore-%d", i), filepath.Base(folderPath), false) {
							m.toggleSearchIgnoreFolder(folderPath)
						}
					}
					var options []internalui.SelectOption
					for i, nb := range m.knownNotebooks(st) {
						if slices.Contains(st.IgnoredSearchFolders, nb) {
							continue
						}
						options = append(options, internalui.SelectOption{
							ID:    fmt.Sprintf("settings-ignore-search-opt-%d", i),
							Value: nb,
							Label: filepath.Base(nb),
							Hint:  tailEllipsis(folderLocationLabel(nb), 32),
						})
					}
					if len(options) == 0 {
						m.searchIgnorePicker.Open = false
						return
					}
					m.searchIgnorePicker.MenuWidth = 360
					if folderAddButton(c, "settings-ignore-search-add", theme.Tr("添加不参与搜索的文件夹", "Exclude Folder"), &m.searchIgnorePicker.TriggerRect) {
						m.searchIgnorePicker.Open = !m.searchIgnorePicker.Open
						m.searchIgnorePicker.Cursor = -1
					}
					if picked := internalui.SelectMenu(c, "settings-ignore-search-picker", options, &m.searchIgnorePicker, ""); picked >= 0 {
						m.toggleSearchIgnoreFolder(options[picked].Value)
					}
				})
			})
		})

		if m.PluginHost != nil {
			m.PluginHost.RenderSettingsSections(c, "folders", m.selectSettingsTab)
		}
	})
}

// renderFolderRuleAdd shows a ghost "+ 添加" chip that expands into an inline name field.
func (m *Manager) renderFolderRuleAdd(c *ui.Context, assetsName string) {
	pal := theme.Ocean()
	if !m.folderRuleAdding {
		if folderAddButton(c, "settings-folder-rule-add", theme.Tr("添加隐藏文件夹", "Add Hidden Folder"), nil) {
			m.folderRuleAdding = true
			m.folderRuleDraft = ""
			m.folderRuleError = ""
		}
		return
	}
	in := ui.TextInput(c.Key("settings-folder-rule-input"), &m.folderRuleDraft).
		Placeholder(theme.Tr("文件夹名，如 build", "Folder, e.g. build")).
		PlaceholderColor(pal.Placeholder).
		Label(theme.Tr("文件夹名称", "Folder Name")).
		FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).CaretColor(pal.Accent).
		Height(theme.HeightCompact).Width(180).Padding(0, theme.Space10).Radius(theme.RadiusMD).
		Background(pal.Shell).Border(1, pal.Accent).AutoFocus()
	internalui.Probe(in, "settings-folder-rule-input")
	if in.Changed() {
		m.folderRuleError = ""
	}
	if in.Submitted() {
		name := strings.TrimSpace(m.folderRuleDraft)
		switch {
		case name == "":
			m.folderRuleAdding = false
		case strings.EqualFold(name, assetsName):
			m.mutateState(func(s *model.AppState) {
				v := true
				s.HideAssetsFolder = &v
			})
			m.folderRuleDraft = ""
			m.folderRuleAdding = false
		default:
			m.addFolderRule(false)
			if m.folderRuleError == "" {
				m.folderRuleAdding = false
			}
		}
	} else if !in.Focused() && strings.TrimSpace(m.folderRuleDraft) == "" {
		m.folderRuleAdding = false
		m.folderRuleError = ""
	}
}

// folderAddButton renders the outlined "+ 添加" chip; rect, when set, receives its bounds for anchoring a menu.
func folderAddButton(c *ui.Context, id, label string, rect *ui.Rect) bool {
	pal := theme.Ocean()
	btn := ui.ButtonBase(c.Key(id)).Height(theme.HeightCompact).PaddingX(theme.Space10).
		Gap(theme.Space4).AlignItems(ui.Center).Radius(theme.RadiusMD).
		Cursor(ui.CursorPointer).Label(label)
	hov := btn.Hovered() || btn.Pressed()
	if hov {
		btn.Background(pal.Hover)
	}
	// One DrawOver per element: record the probe and the menu anchor together.
	btn.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		internalui.RecordBounds(id, r)
		if rect != nil {
			*rect = r
		}
	})
	btn.Children(func() {
		icons.DrawAnimated(c, icons.Plus, theme.IconMicro, pal.DuoIconColors(pal.Accent), hov)
		ui.Text(c, theme.Tr("添加", "Add")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
	})
	return btn.Clicked()
}

// folderChip renders a compact folder tag with a trailing remove button; it returns true when removed.
func folderChip(c *ui.Context, id, name string, shown bool) bool {
	pal := theme.Ocean()
	removed := false
	ui.Row(c.Key(id+"-chip")).Height(theme.HeightCompact).Padding(0, theme.Space4, 0, theme.Space10).Gap(theme.Space4).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Border(1, pal.Hairline).Children(func() {
		ui.Text(c, name).FontSize(theme.FontSmall).TextColor(pal.Text).SingleLine()
		if shown {
			ui.Text(c, theme.Tr("· 显示", "· shown")).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
		}
		del := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
			ID:    id,
			Label: theme.Tr("移除", "Remove") + " " + name,
		})
		if del.Clicked() {
			removed = true
		}
	})
	return removed
}

// tailEllipsis keeps the last max runes of s, prefixed with "…", so the folder end stays visible.
func tailEllipsis(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-max+1:])
}

// folderLocationLabel shows a notebook's parent folder, with iCloud Drive collapsed to a readable name.
func folderLocationLabel(absPath string) string {
	parent := theme.DisplayFolderPath(filepath.Dir(absPath))
	const icloud = "~/Library/Mobile Documents/com~apple~CloudDocs"
	if strings.HasPrefix(parent, icloud) {
		label := theme.Tr("iCloud 云盘", "iCloud Drive")
		if rest := strings.Trim(strings.TrimPrefix(parent, icloud), "/"); rest != "" {
			label += " / " + strings.ReplaceAll(rest, "/", " / ")
		}
		return label
	}
	return parent
}

func (m *Manager) activeNotebookPath(st model.AppState) string {
	if m.host != nil {
		if p := strings.TrimSpace(m.host.ActiveNotebook()); p != "" {
			return p
		}
	}
	return strings.TrimSpace(st.Folder)
}

func (m *Manager) forgetNotebook(folder string) {
	if m.OnForgetNotebook != nil {
		m.OnForgetNotebook(folder)
		return
	}
	clean := filepath.Clean(folder)
	m.mutateState(func(s *model.AppState) {
		var keep []string
		for _, nb := range s.Notebooks {
			if filepath.Clean(nb) != clean {
				keep = append(keep, nb)
			}
		}
		s.Notebooks = keep
		var keepIgnored []string
		for _, nb := range s.IgnoredSearchFolders {
			if filepath.Clean(nb) != clean {
				keepIgnored = append(keepIgnored, nb)
			}
		}
		s.IgnoredSearchFolders = keepIgnored
		theme.SetIgnoredSearchFolders(keepIgnored)
		if s.Folder != "" && filepath.Clean(s.Folder) == clean {
			s.OpenTabs = nil
			s.ActiveTab = ""
			if len(keep) > 0 {
				s.Folder = keep[0]
			} else {
				s.Folder = ""
			}
		}
	})
}

func (m *Manager) knownNotebooks(st model.AppState) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	// Exact all_notebook_folders order: active notebook FIRST, then remaining notebooks in order.
	add(m.activeNotebookPath(st))
	if m.host != nil {
		for _, nb := range m.host.Notebooks() {
			add(nb)
		}
	}
	add(st.Folder)
	for _, nb := range st.Notebooks {
		add(nb)
	}
	return out
}

func (m *Manager) toggleSearchIgnoreFolder(folder string) {
	st := m.currentState()
	next := append([]string(nil), st.IgnoredSearchFolders...)
	if idx := slices.Index(next, folder); idx >= 0 {
		next = append(next[:idx], next[idx+1:]...)
	} else {
		next = append(next, folder)
	}
	theme.SetIgnoredSearchFolders(next)
	m.mutateState(func(s *model.AppState) {
		s.IgnoredSearchFolders = next
	})
}

func (m *Manager) addFolderRule(isRegex bool) {
	st := m.currentState()
	raw := m.folderRuleDraft
	if isRegex {
		raw = theme.NormalizeFolderPattern(m.regexRuleDraft)
	}
	valid, err := theme.ValidateFolderRule(raw, isRegex)
	if err != nil {
		if isRegex {
			m.regexRuleError = err.Error()
		} else {
			m.folderRuleError = err.Error()
		}
		return
	}
	for _, existing := range st.FolderRules {
		if existing.Regex == isRegex && strings.EqualFold(existing.Pattern, valid) {
			msg := theme.Tr("文件夹规则已存在", "Folder rule already exists")
			if isRegex {
				m.regexRuleError = theme.Tr("正则表达式规则已存在", "Regex rule already exists")
			} else {
				m.folderRuleError = msg
			}
			return
		}
	}
	rules := append([]model.FolderRule(nil), st.FolderRules...)
	rules = append(rules, model.FolderRule{Pattern: valid, Regex: isRegex, Hidden: true})
	if isRegex {
		m.regexRuleDraft = ""
		m.regexRuleError = ""
	} else {
		m.folderRuleDraft = ""
		m.folderRuleError = ""
	}
	m.saveFolderRules(rules)
}

func (m *Manager) saveFolderRules(rules []model.FolderRule) {
	theme.SetFolderRules(rules)
	m.mutateState(func(s *model.AppState) {
		s.FolderRules = rules
	})
}

// renderSettingsShortcutsTab renders the Shortcuts tab; every listed row is a recorder.
func (m *Manager) renderSettingsShortcutsTab(c *ui.Context) {
	st := m.currentState()
	searchSpec := EffectiveShortcut(st, "global_summon")
	quickSpec := EffectiveShortcut(st, "quick_note")
	customized := normalizeShortcutSpec(searchSpec) != normalizeShortcutSpec(defaultSearchShortcut) ||
		normalizeShortcutSpec(quickSpec) != normalizeShortcutSpec(defaultQuickShortcut) ||
		strings.TrimSpace(st.ShortcutSearch) != "" ||
		strings.TrimSpace(st.ShortcutQuick) != "" ||
		len(st.CustomShortcuts) > 0

	var pluginCmds []PluginCommandShortcut
	if m.PluginHost != nil {
		pluginCmds = m.PluginHost.CommandShortcuts("")
	}
	hasPlugins := len(pluginCmds) > 0
	if !hasPlugins {
		m.shortcutScope = 0
	}

	resetAll := func() {
		if !customized {
			return
		}
		if internalui.SettingsTextAction(c, "settings-shortcuts-reset", theme.Tr("恢复默认", "Reset"), nil).Clicked() {
			m.recordingShortcut = ""
			m.shortcutConflictNotice = ""
			m.mutateState(func(s *model.AppState) {
				s.ShortcutSearch = ""
				s.ShortcutQuick = ""
				s.CustomShortcuts = nil
			})
		}
	}

	ui.Column(c.Key("settings-shortcuts-body")).FillWidth().Gap(theme.SettingsSectionGap).Children(func() {
		if hasPlugins {
			ui.Row(c.Key("settings-shortcuts-toolbar")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
				internalui.SettingsSegmented(c, "settings-shortcuts-scope", &m.shortcutScope, []internalui.SegmentOption{
					{Label: theme.Tr("全部", "All")},
					{Label: theme.Tr("内置", "Built-in")},
					{Label: theme.Tr("插件", "Plugins")},
				})
				resetAll()
			})
		}

		m.renderShortcutConflictNotice(c)

		if m.shortcutScope == 0 || m.shortcutScope == 1 {
			internalui.SettingsSection(c, func() {
				var trailing func()
				if !hasPlugins {
					trailing = resetAll
				}
				internalui.SettingsSectionTitle(c, theme.Tr("全局", "Global"), trailing)

				internalui.SettingsRow(c, theme.Tr("全局唤出", "Global Summon"), func() {
					m.shortcutRecorderTray(c, "settings-rec-search", "global_summon", searchSpec)
				})
				internalui.SettingsRow(c, theme.Tr("速记小窗", "Quick Note"), func() {
					m.shortcutRecorderTray(c, "settings-rec-quick", "quick_note", quickSpec)
				})
			})

			internalui.SettingsSection(c, func() {
				internalui.SettingsSectionTitle(c, theme.Tr("应用内", "In App"), nil)
				internalui.SettingsRow(c, theme.Tr("搜索笔记", "Search Notes"), func() {
					m.shortcutRecorderTray(c, "settings-keycap-search", "search", EffectiveShortcut(st, "search"))
				})
				internalui.SettingsRow(c, theme.Tr("页内查找", "Find in Page"), func() {
					m.shortcutRecorderTray(c, "settings-keycap-find", "find", EffectiveShortcut(st, "find"))
				})
				internalui.SettingsRow(c, theme.Tr("新建笔记", "New Note"), func() {
					m.shortcutRecorderTray(c, "settings-keycap-new", "new_note", EffectiveShortcut(st, "new_note"))
				})
				internalui.SettingsRow(c, theme.Tr("显示或隐藏侧栏", "Toggle Sidebar"), func() {
					m.shortcutRecorderTray(c, "settings-keycap-sidebar", "toggle_sidebar", EffectiveShortcut(st, "toggle_sidebar"))
				})
				if m.AIEnabled == nil || m.AIEnabled() {
					internalui.SettingsRow(c, theme.Tr("AI Agent 面板", "AI Agent Panel"), func() {
						m.shortcutRecorderTray(c, "settings-keycap-ai", "toggle_ai", EffectiveShortcut(st, "toggle_ai"))
					})
				}
				internalui.SettingsRow(c, theme.Tr("导出与生成图片", "Export & Image"), func() {
					m.shortcutRecorderTray(c, "settings-keycap-export", "export_image", EffectiveShortcut(st, "export_image"))
				})
				internalui.SettingsRow(c, theme.Tr("偏好设置", "Settings"), func() {
					internalui.ShortcutKeycap(c, internalui.SettingsShortcut)
				})
			})
		}

		if (m.shortcutScope == 0 || m.shortcutScope == 2) && hasPlugins {
			pal := theme.Ocean()
			type pluginGroup struct {
				id   string
				name string
				cmds []PluginCommandShortcut
			}
			var groups []pluginGroup
			groupIdx := make(map[string]int, len(pluginCmds))
			for _, cmd := range pluginCmds {
				idx, ok := groupIdx[cmd.PluginID]
				if !ok {
					idx = len(groups)
					groupIdx[cmd.PluginID] = idx
					name := strings.TrimSpace(cmd.PluginName)
					if name == "" {
						name = cmd.PluginID
					}
					groups = append(groups, pluginGroup{id: cmd.PluginID, name: name})
				}
				groups[idx].cmds = append(groups[idx].cmds, cmd)
			}
			for _, grp := range groups {
				internalui.SettingsSection(c, func() {
					internalui.SettingsSectionTitle(c, grp.name, func() {
						ui.Text(c, theme.Tr("插件", "Plugin")).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).SingleLine()
					})
					for _, cmd := range grp.cmds {
						action := PluginCommandShortcutAction(cmd.PluginID, cmd.CommandID)
						eff := EffectivePluginCommandShortcut(st, cmd.PluginID, cmd.CommandID, cmd.DefaultShortcut)
						probeID := "settings-keycap-plugin-" + cmd.PluginID + "-" + cmd.CommandID
						internalui.SettingsRow(c, cmd.Title, func() {
							m.shortcutRecorderTray(c, probeID, action, eff)
						})
					}
				})
			}
		}

		if m.PluginHost != nil && (m.shortcutScope == 0 || m.shortcutScope == 2) {
			m.PluginHost.RenderSettingsSections(c, "shortcuts", m.selectSettingsTab)
		}
	})
}

func (m *Manager) renderPluginShortcutsSection(c *ui.Context, pluginID string) {
	if m.PluginHost == nil {
		return
	}
	cmds := m.PluginHost.CommandShortcuts(pluginID)
	if len(cmds) == 0 {
		return
	}
	st := m.currentState()
	customized := false
	for _, cmd := range cmds {
		eff := EffectivePluginCommandShortcut(st, cmd.PluginID, cmd.CommandID, cmd.DefaultShortcut)
		if normalizeShortcutSpec(eff) != normalizeShortcutSpec(cmd.DefaultShortcut) {
			customized = true
			break
		}
	}
	ui.Box(c.Key("plugin-shortcuts-wrap-"+pluginID)).FillWidth().Margin(theme.SettingsSectionGap, 0, 0, 0).Children(func() {
		internalui.SettingsSection(c, func() {
			internalui.SettingsSectionTitle(c, theme.Tr("快捷键", "Shortcuts"), func() {
				if customized {
					if internalui.SettingsTextAction(c, "plugin-shortcuts-reset-"+pluginID, theme.Tr("恢复默认", "Reset"), nil).Clicked() {
						m.recordingShortcut = ""
						m.shortcutConflictNotice = ""
						m.mutateState(func(s *model.AppState) {
							if s.CustomShortcuts == nil {
								return
							}
							for _, cmd := range cmds {
								delete(s.CustomShortcuts, PluginCommandShortcutAction(cmd.PluginID, cmd.CommandID))
							}
							if len(s.CustomShortcuts) == 0 {
								s.CustomShortcuts = nil
							}
						})
					}
				}
				if internalui.SettingsTextAction(c, "plugin-shortcuts-all-"+pluginID, theme.Tr("全部快捷键", "All Shortcuts"), nil).Clicked() {
					m.selectSettingsTab("shortcuts")
				}
			})
			m.renderShortcutConflictNotice(c)
			for _, cmd := range cmds {
				action := PluginCommandShortcutAction(cmd.PluginID, cmd.CommandID)
				eff := EffectivePluginCommandShortcut(st, cmd.PluginID, cmd.CommandID, cmd.DefaultShortcut)
				probeID := "plugin-keycap-" + cmd.PluginID + "-" + cmd.CommandID
				internalui.SettingsRow(c, cmd.Title, func() {
					m.shortcutRecorderTray(c, probeID, action, eff)
				})
			}
		})
	})
}

func (m *Manager) renderShortcutConflictNotice(c *ui.Context) {
	if m.shortcutConflictNotice == "" {
		return
	}
	pal := theme.Ocean()
	noticeRow := ui.Row(c.Key("settings-shortcut-conflict-notice")).FillWidth().
		Padding(theme.Space8, theme.Space12, theme.Space8, theme.Space12).Margin(0, 0, theme.Space8, 0).Radius(theme.RadiusMD).
		Background(pal.WarningSurface)
	internalui.Probe(noticeRow, "settings-shortcut-conflict-notice")
	noticeRow.Children(func() {
		ui.Text(c, m.shortcutConflictNotice).FontSize(theme.FontBody).TextColor(pal.Warning)
	})
}

func (m *Manager) shortcutRecorderTray(c *ui.Context, id, kind, spec string) {
	pal := theme.Ocean()
	recording := m.recordingShortcut == kind
	emptySpec := strings.TrimSpace(spec) == ""
	accLabel := formatShortcutLabel(spec)
	if recording {
		accLabel = theme.Tr("按下快捷键…", "Press shortcut…")
	} else if emptySpec {
		accLabel = theme.Tr("点击设置", "Click to set")
	}
	trayBG := ui.Transparent
	borderCol := ui.Transparent
	if recording {
		trayBG = pal.Selected
		borderCol = pal.Accent
	}
	btn := ui.ButtonBase(c.Key(id)).Height(theme.HeightCompact).PaddingX(theme.Space6).Gap(theme.Space4).
		AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(accLabel)
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	if !recording && (hovered || pressed) {
		trayBG = pal.Hover
	}
	internalui.AnimateSurfaceColors(btn, trayBG, borderCol, true)
	internalui.TactileCard(btn, hovered, pressed, recording, false)
	btn.Children(func() {
		if recording {
			ui.Box(c).PaddingX(theme.Space6).Children(func() {
				ui.Text(c, accLabel).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
			})
			return
		}
		if emptySpec {
			ui.Box(c).PaddingX(theme.Space6).Children(func() {
				ui.Text(c, accLabel).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
			})
			return
		}
		for _, part := range hotkeyParts(spec) {
			internalui.ShortcutLabel(c, part, internalui.ShortcutLabelStyle{
				FontSize:   theme.KeycapFontSize,
				LineHeight: theme.LineMeta,
				FontWeight: theme.WeightMono,
				Color:      pal.TextSecondary,
				IsShortcut: true,
			})
		}
	})
	internalui.Probe(btn, id)
	if btn.Clicked() {
		if recording {
			m.recordingShortcut = ""
		} else {
			m.recordingShortcut = kind
		}
	}
}

func hotkeyParts(spec string) []string {
	norm := normalizeShortcutSpec(spec)
	if norm == "" {
		if strings.TrimSpace(spec) == "" {
			return nil
		}
		return []string{spec}
	}
	tokens := strings.Split(norm, "-")
	keyTok := tokens[len(tokens)-1]
	modToks := tokens[:len(tokens)-1]
	var ctrl, alt, shift, cmd, win bool
	for _, m := range modToks {
		switch m {
		case "ctrl":
			ctrl = true
		case "alt":
			alt = true
		case "shift":
			shift = true
		case "cmd":
			cmd = true
		case "win":
			win = true
		}
	}

	keyLabel := strings.ToUpper(keyTok)
	switch keyTok {
	case "space":
		keyLabel = theme.Tr("空格", "Space")
	case "comma":
		keyLabel = ","
	case "period":
		keyLabel = "."
	case "slash":
		keyLabel = "/"
	case "minus":
		keyLabel = "-"
	case "equal":
		keyLabel = "="
	case "bracketleft":
		keyLabel = "["
	case "bracketright":
		keyLabel = "]"
	case "backspace":
		if theme.IsMacOS() {
			keyLabel = "⌫"
		} else {
			keyLabel = "Backspace"
		}
	case "delete":
		keyLabel = "Del"
	case "enter":
		if theme.IsMacOS() {
			keyLabel = "↩"
		} else {
			keyLabel = "Enter"
		}
	case "escape":
		keyLabel = "Esc"
	case "tab":
		keyLabel = "Tab"
	case "left":
		keyLabel = "←"
	case "right":
		keyLabel = "→"
	case "up":
		keyLabel = "↑"
	case "down":
		keyLabel = "↓"
	case "\\", ";", "'", "`":
		keyLabel = keyTok
	}

	var out []string
	if theme.IsMacOS() {
		if ctrl {
			out = append(out, "⌃")
		}
		if alt {
			out = append(out, "⌥")
		}
		if shift {
			out = append(out, "⇧")
		}
		if cmd {
			out = append(out, "⌘")
		}
	} else {
		if ctrl || cmd {
			out = append(out, "Ctrl")
		}
		if alt {
			out = append(out, "Alt")
		}
		if shift {
			out = append(out, "Shift")
		}
		if win {
			if runtime.GOOS == "windows" {
				out = append(out, "Win")
			} else {
				out = append(out, "Super")
			}
		}
	}
	out = append(out, keyLabel)
	return out
}

func setActionShortcut(s *model.AppState, action, spec string) {
	switch action {
	case "global_summon":
		if normalizeShortcutSpec(spec) == normalizeShortcutSpec(defaultSearchShortcut) {
			s.ShortcutSearch = ""
		} else {
			s.ShortcutSearch = spec
		}
	case "quick_note":
		if normalizeShortcutSpec(spec) == normalizeShortcutSpec(defaultQuickShortcut) {
			s.ShortcutQuick = ""
		} else {
			s.ShortcutQuick = spec
		}
	default:
		if spec == "" {
			if s.CustomShortcuts != nil {
				delete(s.CustomShortcuts, action)
				if len(s.CustomShortcuts) == 0 {
					s.CustomShortcuts = nil
				}
			}
			return
		}
		if s.CustomShortcuts == nil {
			s.CustomShortcuts = make(map[string]string)
		}
		s.CustomShortcuts[action] = spec
	}
}

func (m *Manager) pluginCommandShortcutByAction(action string) (PluginCommandShortcut, bool) {
	if m.PluginHost == nil || !IsPluginCommandShortcutAction(action) {
		return PluginCommandShortcut{}, false
	}
	for _, cmd := range m.PluginHost.CommandShortcuts("") {
		if PluginCommandShortcutAction(cmd.PluginID, cmd.CommandID) == action {
			return cmd, true
		}
	}
	return PluginCommandShortcut{}, false
}

func (m *Manager) effectiveActionShortcut(st model.AppState, action string) string {
	if IsPluginCommandShortcutAction(action) {
		if cmd, ok := m.pluginCommandShortcutByAction(action); ok {
			return EffectivePluginCommandShortcut(st, cmd.PluginID, cmd.CommandID, cmd.DefaultShortcut)
		}
		if st.CustomShortcuts != nil {
			v := strings.TrimSpace(st.CustomShortcuts[action])
			if v == pluginShortcutDisabled {
				return ""
			}
			return v
		}
		return ""
	}
	return EffectiveShortcut(st, action)
}

func setPluginCommandShortcut(s *model.AppState, action, defaultSpec, spec string) {
	normSpec := normalizeShortcutSpec(spec)
	normDef := normalizeShortcutSpec(defaultSpec)
	if normSpec == normDef {
		setActionShortcut(s, action, "")
		return
	}
	if normSpec == "" {
		setActionShortcut(s, action, pluginShortcutDisabled)
		return
	}
	setActionShortcut(s, action, spec)
}

func (m *Manager) clearRecordedPluginShortcut(action string) {
	m.recordingShortcut = ""
	m.justRecordedPasses = 4
	m.shortcutConflictNotice = ""
	defaultSpec := ""
	if cmd, ok := m.pluginCommandShortcutByAction(action); ok {
		defaultSpec = cmd.DefaultShortcut
	}
	m.mutateState(func(s *model.AppState) {
		setPluginCommandShortcut(s, action, defaultSpec, "")
	})
}

func (m *Manager) applyRecordedShortcut(action, spec string) {
	m.recordingShortcut = ""
	m.justRecordedPasses = 4
	normNew := normalizeShortcutSpec(spec)
	if mods, key, ok := internalui.ParseShortcut(spec); ok &&
		(internalui.IsFixedAppShortcut(mods, key) || (IsPluginCommandShortcutAction(action) && internalui.IsHostReservedShortcut(mods, key))) {
		m.shortcutConflictNotice = theme.Tr("该快捷键为系统保留，请选择其他组合", "This shortcut is reserved by Block")
		return
	}
	var conflictMsg string
	var pluginCmds []PluginCommandShortcut
	if m.PluginHost != nil {
		pluginCmds = m.PluginHost.CommandShortcuts("")
	}
	actionDefault := ""
	if IsPluginCommandShortcutAction(action) {
		for _, cmd := range pluginCmds {
			if PluginCommandShortcutAction(cmd.PluginID, cmd.CommandID) == action {
				actionDefault = cmd.DefaultShortcut
				break
			}
		}
	}

	m.mutateState(func(s *model.AppState) {
		prevSpec := m.effectiveActionShortcut(*s, action)
		if normalizeShortcutSpec(prevSpec) == normNew {
			if IsPluginCommandShortcutAction(action) {
				setPluginCommandShortcut(s, action, actionDefault, spec)
			} else {
				setActionShortcut(s, action, spec)
			}
			return
		}
		for _, other := range shortcutActionSpecs {
			if other.action == action {
				continue
			}
			if normalizeShortcutSpec(EffectiveShortcut(*s, other.action)) == normNew {
				otherLabel := theme.Tr(other.zh, other.en)
				if prevSpec == "" {
					conflictMsg = theme.Tr(
						fmt.Sprintf("该快捷键已被「%s」占用", otherLabel),
						fmt.Sprintf("Shortcut is already used by %s", otherLabel),
					)
					return
				}
				reassigned := prevSpec
				if normalizeShortcutSpec(prevSpec) == normalizeShortcutSpec(other.def) {
					reassigned = ""
				}
				setActionShortcut(s, other.action, reassigned)
				conflictMsg = theme.Tr(
					fmt.Sprintf("已将冲突快捷键从「%s」调整为 %s", otherLabel, formatShortcutLabel(prevSpec)),
					fmt.Sprintf("Reassigned conflicting shortcut from %s to %s", otherLabel, formatShortcutLabel(prevSpec)),
				)
				break
			}
		}
		if conflictMsg == "" {
			for _, other := range pluginCmds {
				otherAction := PluginCommandShortcutAction(other.PluginID, other.CommandID)
				if otherAction == action {
					continue
				}
				otherEff := EffectivePluginCommandShortcut(*s, other.PluginID, other.CommandID, other.DefaultShortcut)
				if otherEff != "" && normalizeShortcutSpec(otherEff) == normNew {
					swapSpec := prevSpec
					if swapSpec != "" {
						if mods, key, ok := internalui.ParseShortcut(swapSpec); ok && internalui.IsHostReservedShortcut(mods, key) {
							swapSpec = ""
						}
					}
					setPluginCommandShortcut(s, otherAction, other.DefaultShortcut, swapSpec)
					if swapSpec == "" {
						conflictMsg = theme.Tr(
							fmt.Sprintf("已清除冲突快捷键「%s」的绑定", other.Title),
							fmt.Sprintf("Cleared conflicting shortcut from %s", other.Title),
						)
					} else {
						conflictMsg = theme.Tr(
							fmt.Sprintf("已将冲突快捷键从「%s」调整为 %s", other.Title, formatShortcutLabel(swapSpec)),
							fmt.Sprintf("Reassigned conflicting shortcut from %s to %s", other.Title, formatShortcutLabel(swapSpec)),
						)
					}
					break
				}
			}
		}
		if IsPluginCommandShortcutAction(action) {
			setPluginCommandShortcut(s, action, actionDefault, spec)
		} else {
			setActionShortcut(s, action, spec)
		}
	})
	m.shortcutConflictNotice = conflictMsg
}

func normalizeShortcutSpec(spec string) string {
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" {
		return ""
	}
	trailingMinus := strings.HasSuffix(s, "--") || strings.HasSuffix(s, "+-")
	s = strings.ReplaceAll(s, "+", "-")
	var ctrl, alt, shift, cmd, win bool
	var keyPart string
	for _, p := range strings.Split(s, "-") {
		switch p {
		case "":
			continue
		case "cmd", "cmdorctrl", "meta", "super", "command", "secondary":
			cmd = true
		case "win":
			win = true
		case "ctrl", "control":
			ctrl = true
		case "alt", "opt", "option":
			alt = true
		case "shift":
			shift = true
		case "comma", ",":
			keyPart = "comma"
		case "period", ".":
			keyPart = "period"
		case "slash", "/":
			keyPart = "slash"
		case "minus", "-":
			keyPart = "minus"
		case "equal", "=", "plus":
			keyPart = "equal"
		case "bracketleft", "[":
			keyPart = "bracketleft"
		case "bracketright", "]":
			keyPart = "bracketright"
		case "backslash", "\\":
			keyPart = "\\"
		case "semicolon", ";":
			keyPart = ";"
		case "quote", "apostrophe", "'":
			keyPart = "'"
		case "backquote", "grave", "`":
			keyPart = "`"
		case "return", "enter":
			keyPart = "enter"
		case "esc", "escape":
			keyPart = "escape"
		case "del", "delete":
			keyPart = "delete"
		case "arrowleft", "left":
			keyPart = "left"
		case "arrowright", "right":
			keyPart = "right"
		case "arrowup", "up":
			keyPart = "up"
		case "arrowdown", "down":
			keyPart = "down"
		default:
			keyPart = p
		}
	}
	if trailingMinus && keyPart == "" {
		keyPart = "minus"
	}
	if keyPart == "" {
		return ""
	}
	if theme.IsMacOS() && win {
		cmd, win = true, false
	}
	if !theme.IsMacOS() && ctrl {
		cmd, ctrl = true, false
	}
	var parts []string
	if ctrl {
		parts = append(parts, "ctrl")
	}
	if alt {
		parts = append(parts, "alt")
	}
	if shift {
		parts = append(parts, "shift")
	}
	if cmd {
		parts = append(parts, "cmd")
	}
	if win {
		parts = append(parts, "win")
	}
	parts = append(parts, keyPart)
	return strings.Join(parts, "-")
}

func formatShortcutLabel(spec string) string {
	parts := hotkeyParts(spec)
	if len(parts) == 0 {
		return spec
	}
	if theme.IsMacOS() {
		return strings.Join(parts, "")
	}
	return strings.Join(parts, "+")
}

func inputEventToShortcutSpec(ev ui.InputEvent) (string, bool) {
	if ev.Mods == 0 || ev.Key == ui.KeyUnknown {
		return "", false
	}
	keyPart := shortcutKeyName(ev.Key)
	if keyPart == "" {
		return "", false
	}
	var parts []string
	if theme.IsMacOS() && ev.Mods&ui.Ctrl != 0 {
		parts = append(parts, "ctrl")
	}
	if ev.Mods&ui.Alt != 0 {
		parts = append(parts, "alt")
	}
	if ev.Mods&ui.Shift != 0 {
		parts = append(parts, "shift")
	}
	if ev.Mods&ui.Cmd != 0 {
		parts = append(parts, "cmd")
	}
	if !theme.IsMacOS() && ev.Mods&ui.Super != 0 {
		parts = append(parts, "win")
	}
	parts = append(parts, keyPart)
	return strings.Join(parts, "-"), true
}

func shortcutKeyName(k ui.Key) string {
	switch k {
	case ui.KeySpace:
		return "space"
	case ui.KeyTab:
		return "tab"
	case ui.KeyEnter:
		return "enter"
	case ui.KeyBackspace:
		return "backspace"
	case ui.KeyDelete:
		return "delete"
	case ui.KeyLeft:
		return "left"
	case ui.KeyRight:
		return "right"
	case ui.KeyUp:
		return "up"
	case ui.KeyDown:
		return "down"
	case ui.KeyComma:
		return "comma"
	case ui.KeyPeriod:
		return "period"
	case ui.KeySlash:
		return "slash"
	case ui.KeyBackslash:
		return "\\"
	case ui.KeySemicolon:
		return ";"
	case ui.KeyQuote:
		return "'"
	case ui.KeyBackquote:
		return "`"
	case ui.KeyMinus:
		return "minus"
	case ui.KeyEqual:
		return "equal"
	case ui.KeyBracketLeft:
		return "bracketleft"
	case ui.KeyBracketRight:
		return "bracketright"
	default:
		if k >= ui.KeyF1 && k <= ui.KeyF12 {
			return fmt.Sprintf("f%d", 1+int(k-ui.KeyF1))
		}
		if k >= ui.KeyA && k <= ui.KeyZ {
			return string(rune('a' + (k - ui.KeyA)))
		}
		if k >= ui.Key0 && k <= ui.Key9 {
			return string(rune('0' + (k - ui.Key0)))
		}
		return ""
	}
}
