package modals

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	defaultSearchShortcut = "cmd-shift-k"
	defaultQuickShortcut  = "alt-n"
)

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
	{action: "new_note", buttonID: "settings-rec-new-note", def: "cmd-n", zh: "新建笔记", en: "New Note"},
	{action: "toggle_sidebar", buttonID: "settings-rec-sidebar", def: "alt-b", zh: "显示或隐藏侧栏", en: "Toggle Sidebar"},
	{action: "toggle_ai", buttonID: "settings-rec-ai", def: "cmd-j", zh: "AI 助手面板", en: "AI Assistant Panel"},
	{action: "export_image", buttonID: "settings-rec-export", def: "cmd-shift-e", zh: "导出与生成图片", en: "Export & Image"},
	{action: "performance", buttonID: "settings-rec-perf", def: "cmd-shift-m", zh: "性能与占用", en: "Performance & Usage"},
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
	case "find":
		return "cmd-f"
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
	SidebarWidth          *float32
	OnSidebarWidthChanged func(float32)
	OnForgetNotebook      func(string)
	SearchFn              func(query string, allNotebooks bool) ([]model.SearchHit, error)
	ListHistoryFn         func(notePath string) ([]model.HistorySnapshot, error)
	ReadHistoryFn         func(notePath, snapID string) (string, error)
	DiffLinesFn           func(oldText, newText string) []model.DiffLine
	ExportFn              func(format model.ExportFormat, outPath string) error
	FeedbackSubmitFn      func(category, message, contact string) error

	fallbackState model.AppState

	// Settings page state.
	settingsOpen           bool
	settingsTab            string
	recordingShortcut      string
	justRecordedShortcut   bool
	shortcutConflictNotice string
	fontPickerOpen         string
	fontTriggerRects       map[string]ui.Rect
	fontQuery              string
	fontCursor             int
	fontScrollToCursor     bool
	fontFamilies           []ui.FontFamily
	fontScroll             ui.ScrollState
	assetsDraft            string
	folderRuleDraft        string
	regexRuleDraft         string
	folderRuleError        string
	regexRuleError         string
	advancedRulesOpen      bool
	settingsScroll         ui.ScrollState
	settingsFocusHandle    ui.Handle
	fontSearchHandle       ui.Handle

	// Performance modal state.
	perfMu         sync.Mutex
	perfOpen       bool
	perfCancel     chan struct{}
	perfCategories []PerfCategory
	perfSelected   string
	perfCPU        float64
	perfRSS        uint64
	perfSamples    int
	perfLastScan   string
	perfScanErr    string
	perfTreeRects  []treemapTileRect

	// Search modal state.
	searchOpen        bool
	searchQuery       string
	searchLastQuery   string
	searchAllNB       bool
	searchHits        []model.SearchHit
	searchSelected    int
	searchErr         string
	searchInputHandle ui.Handle

	// About modal state.
	aboutOpen bool

	// Feedback modal state.
	feedbackOpen        bool
	feedbackCategory    string
	feedbackMessage     string
	feedbackContact     string
	feedbackSentContact string
	feedbackDone        bool
	feedbackErr         string

	// History modal state.
	historyOpen           bool
	historyNotePath       string
	historyCurrentContent string
	historySnapshots      []model.HistorySnapshot
	historySelectedID     string
	historySnapContent    string
	historyDiffLines      []model.DiffLine
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
	updaterOpen     bool
	updaterChecking bool
	updaterUpdating bool
	updaterProgress float64
	updaterStatus   string
	updaterLatest   string
	updaterNotes    []string
}

// NewManager creates a modal and settings manager bound to `dataDir` and `host`.
func NewManager(dataDir string, host model.WorkspaceHost) *Manager {
	m := &Manager{
		dataDir:          dataDir,
		host:             host,
		fallbackState:    model.DefaultAppState(theme.Language()),
		settingsTab:      "ai",
		feedbackCategory: "bug",
		historyMode:      "diff",
		exportFormat:     model.ExportMarkdown,
		exportTheme:      "current",
		updaterLatest:    "1.0.379",
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

// ActiveSettingsTab returns the currently active Settings tab ID.
func (m *Manager) ActiveSettingsTab() string {
	if m.settingsTab == "" {
		return "ai"
	}
	return m.settingsTab
}

// IsRecordingShortcut reports whether the shortcut recorder is actively capturing keys
// (including the remainder of the frame in which a key was just captured).
func (m *Manager) IsRecordingShortcut() bool {
	return m.settingsOpen && (m.recordingShortcut != "" || m.justRecordedShortcut)
}

// OpenSettings opens the full-window Settings page at `tab` ("ai", "appearance", "folders", "shortcuts").
func (m *Manager) OpenSettings(tab string) {
	switch tab {
	case "ai", "appearance", "folders", "shortcuts":
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
	m.shortcutConflictNotice = ""
	m.fontPickerOpen = ""
	m.folderRuleError = ""
	m.regexRuleError = ""
	m.settingsOpen = true
	m.requestRedraw()
}

// CloseSettings closes the full-window Settings page and stops any active recording.
func (m *Manager) CloseSettings() {
	if !m.settingsOpen {
		return
	}
	m.settingsOpen = false
	m.recordingShortcut = ""
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
	{id: "ai", icon: icons.Sparkles, zh: "AI 助手", en: "AI Assistant"},
	{id: "appearance", icon: icons.Sliders, zh: "外观与字体", en: "Appearance & Fonts"},
	{id: "folders", icon: icons.Folder, zh: "文件夹", en: "Folders"},
	{id: "shortcuts", icon: icons.Code, zh: "快捷键", en: "Shortcuts"},
}

func settingsTabTitle(tab string) string {
	for _, spec := range settingsTabs {
		if spec.id == tab {
			return theme.Tr(spec.zh, spec.en)
		}
	}
	return theme.Tr("AI 助手", "AI Assistant")
}

// RenderSettings draws the full-window Settings page when open.
func (m *Manager) RenderSettings(c *ui.Context) {
	if !m.settingsOpen {
		return
	}
	pal := theme.Ocean()
	tab := m.ActiveSettingsTab()

	root := ui.Row(c.Key("settings-page")).Fill().Background(pal.SettingsBG).
		TabStop(false).Bind(&m.settingsFocusHandle)
	internalui.Probe(root, "settings-page")

	// Capture keyboard for shortcut recording.
	root.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind != ui.InputKeyDown || m.recordingShortcut == "" {
			return
		}
		if ev.Key == ui.KeyEscape && ev.Mods == 0 {
			m.recordingShortcut = ""
			return
		}
		if spec, ok := inputEventToShortcutSpec(ev); ok {
			m.applyRecordedShortcut(m.recordingShortcut, spec)
		}
	})

	root.Children(func() {
		// Left category navigation sidebar matching app_settings_sidebar.rs 1:1.
		sbW := float32(240)
		if m.SidebarWidth != nil && *m.SidebarWidth > 0 {
			sbW = *m.SidebarWidth
		}
		ui.Row(c.Key("settings-sidebar-wrap")).Shrink(0).FillHeight().Children(func() {
			ui.Column(c.Key("settings-sidebar")).Width(sbW).FillHeight().Shrink(0).
				Background(pal.Sidebar).BorderWidth(0, 1, 0, 0).BorderColor(pal.SidebarBorder).
				Children(func() {
					ui.Box(c).Padding(18, 16, 10, 16).Children(func() {
						ui.Text(c, theme.Tr("设置", "Settings")).FontSize(18).FontWeight(560).TextColor(pal.Text)
					})
					ui.Column(c).PaddingX(10).Gap(2).Grow(1).Children(func() {
						for _, spec := range settingsTabs {
							active := tab == spec.id
							navID := "settings-tab-" + spec.id
							title := theme.Tr(spec.zh, spec.en)
							btn := ui.ButtonBase(c.Key(navID)).FillWidth().Height(32).PaddingX(10).Gap(8).
								AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
								Label(title)
							if active {
								btn.Background(pal.Selected).Border(1, pal.Accent)
							} else {
								if btn.Hovered() {
									btn.Background(pal.Hover)
								}
								btn.Border(1, ui.Transparent)
							}
							btn.Children(func() {
								if active {
									icons.Draw(c, spec.icon, theme.IconRow, pal.DuoIconColors(pal.Accent))
								} else if btn.Hovered() {
									icons.Draw(c, spec.icon, theme.IconRow, pal.DuoIconColors(pal.Text))
								} else {
									icons.Draw(c, spec.icon, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
								}
								col := pal.TextSecondary
								weight := 400
								if active {
									col = pal.Text
									weight = 460
								} else if btn.Hovered() {
									col = pal.Text
								}
								ui.Text(c, title).FontSize(14).FontWeight(weight).TextColor(col).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							})
							internalui.Probe(btn, navID)
							if btn.Clicked() {
								m.selectSettingsTab(spec.id)
							}
						}
					})
				})
			if m.SidebarWidth != nil {
				if internalui.VerticalResizeHandle(c, "settings-sidebar-resize", m.SidebarWidth, 180, 380, false) && m.OnSidebarWidthChanged != nil {
					m.OnSidebarWidthChanged(*m.SidebarWidth)
				}
			}
		})

		// Right scrollable settings body matching app_settings_view.rs 1:1 (pt=40 px=28 pb=64 gap=28).
		internalui.Scroll(c.Key("settings-scroll"), &m.settingsScroll).Grow(1).MinWidth(0).FillHeight().
			Background(pal.SettingsBG).Padding(40, 28, 64, 28).Gap(28).Children(func() {
			ui.Row(c.Key("settings-header")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
				ui.Text(c, settingsTabTitle(tab)).FontSize(24).FontWeight(560).TextColor(pal.Text)
				backBtn := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
					ID:      "settings-back",
					Label:   theme.Tr("返回", "Back"),
					Tooltip: theme.Tr("关闭设置", "Close Settings"),
				})
				if backBtn.Clicked() {
					m.CloseSettings()
				}
			})

			switch tab {
			case "ai":
				m.renderSettingsAITab(c)
			case "appearance":
				m.renderSettingsAppearanceTab(c)
			case "folders":
				m.renderSettingsFoldersTab(c)
			case "shortcuts":
				m.renderSettingsShortcutsTab(c)
			}
		})
	})

	if m.fontPickerOpen != "" {
		m.renderFontPickerOverlay(c)
	}
	if m.recordingShortcut == "" && !m.justRecordedShortcut && c.Shortcut(0, ui.KeyEscape) {
		if m.fontPickerOpen != "" {
			m.fontPickerOpen = ""
		} else {
			m.CloseSettings()
		}
	}
	m.justRecordedShortcut = false
}

func (m *Manager) selectSettingsTab(tab string) {
	m.recordingShortcut = ""
	m.shortcutConflictNotice = ""
	m.fontPickerOpen = ""
	m.settingsTab = tab
}

func (m *Manager) renderSettingsAITab(c *ui.Context) {
	pal := theme.Ocean()
	if m.RenderAISettings != nil {
		m.RenderAISettings(c)
		return
	}
	settingsCard(c, func() {
		ui.Text(c, theme.Tr("AI 助手", "AI Assistant")).FontSize(16).FontWeight(600).TextColor(pal.Text)
		ui.Text(c, theme.Tr("在工作区接入 AI 提供商与模型配置。", "Configure AI providers and model backends in the workspace.")).
			FontSize(14).TextColor(pal.TextSecondary)
	})
}

func (m *Manager) renderSettingsAppearanceTab(c *ui.Context) {
	pal := theme.Ocean()
	st := m.currentState()

	ui.Column(c.Key("settings-appearance-body")).FillWidth().Gap(24).Children(func() {
		// Card 1: Theme & Language (app_settings_tabs.rs:64-66).
		settingsCardGap(c, 16, func() {
			cardTitle(c, theme.Tr("主题与语言", "Theme & Language"))

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
			settingRow(c, theme.Tr("外观主题", "Theme"), func() {
				if internalui.SegmentedControl(c, "settings-theme-seg", &themeIdx, []internalui.SegmentOption{
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
			settingRow(c, theme.Tr("界面语言", "Language"), func() {
				if internalui.SegmentedControl(c, "settings-lang-seg", &langIdx, []internalui.SegmentOption{
					{ID: "settings-lang-zh", Label: "简体中文"},
					{ID: "settings-lang-en", Label: "English"},
				}) {
					picked := model.LangZh
					if langIdx == 1 {
						picked = model.LangEn
					}
					theme.SetLanguage(picked)
					m.mutateState(func(s *model.AppState) {
						s.Language = picked
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
		if len(m.fontFamilies) == 0 {
			m.fontFamilies = ui.FontFamilies()
		}
		edDisplay := strings.TrimSpace(edFont)
		if edDisplay == "" {
			edDisplay = resolvedBodyFontName(m.fontFamilies)
		}
		cdDisplay := strings.TrimSpace(cdFont)
		if cdDisplay == "" {
			cdDisplay = resolvedCodeFontName(m.fontFamilies)
		}

		settingsCardGap(c, 16, func() {
			cardTitle(c, theme.Tr("编辑器", "Editor"))

			settingRow(c, theme.Tr("正文字体", "Body Font"), func() {
				m.fontPickerTrigger(c, "settings-body-font-trigger", "body", edDisplay)
			})
			settingRow(c, theme.Tr("代码字体", "Code Font"), func() {
				m.fontPickerTrigger(c, "settings-code-font-trigger", "code", cdDisplay)
			})

			lhIdx := 1
			switch lh {
			case model.LineHeightTight:
				lhIdx = 0
			case model.LineHeightLoose:
				lhIdx = 2
			}
			settingRow(c, theme.Tr("行高", "Line Height"), func() {
				if internalui.SegmentedControl(c, "settings-lh-seg", &lhIdx, []internalui.SegmentOption{
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
		"PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", "Noto Sans SC",
		"Hiragino Sans GB", "Helvetica Neue", "Segoe UI", "DejaVu Sans",
	}, defaultBodyFontName())
}

func resolvedCodeFontName(families []ui.FontFamily) string {
	return pickInstalledFont(families, []string{
		"JetBrains Mono", "Menlo", "Monaco", "Cascadia Mono",
		"Consolas", "DejaVu Sans Mono", "Liberation Mono",
	}, defaultCodeFontName())
}

func defaultBodyFontName() string {
	switch runtime.GOOS {
	case "darwin", "js":
		return "PingFang SC"
	case "windows":
		return "Microsoft YaHei"
	default:
		return "Noto Sans SC"
	}
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

func (m *Manager) fontPickerTrigger(c *ui.Context, id, role, label string) {
	pal := theme.Ocean()
	open := m.fontPickerOpen == role
	borderCol := pal.BtnBorder
	if open {
		borderCol = pal.Accent
	}
	btn := ui.ButtonBase(c.Key(id)).Width(248).Height(36).Padding(0, 10, 0, 12).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusControl).
		Background(pal.Shell).Border(1, borderCol).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
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
		ui.Text(c, label).FontSize(15).TextColor(pal.Text).SingleLine().Grow(1)
		glyph := icons.ChevronDown
		if open {
			glyph = icons.ChevronUp
		}
		if open || btn.Hovered() {
			icons.Draw(c, glyph, 14, pal.DuoIconColors(pal.Text))
		} else {
			icons.Draw(c, glyph, 14, pal.MonoIconColors(pal.TextSecondary))
		}
	})
	if btn.Clicked() {
		if open {
			m.fontPickerOpen = ""
		} else {
			m.openFontPicker(role)
		}
	}
}

func (m *Manager) openFontPicker(role string) {
	m.fontPickerOpen = role
	m.fontQuery = ""
	m.fontCursor = 0
	base := m.fontFamilies
	if len(base) == 0 {
		base = ui.FontFamilies()
	}
	if m.dataDir != "" {
		if entries, err := os.ReadDir(filepath.Join(m.dataDir, "fonts")); err == nil {
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(ent.Name()))
				if ext == ".ttf" || ext == ".otf" || ext == ".ttc" {
					fam := strings.TrimSuffix(ent.Name(), filepath.Ext(ent.Name()))
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

func (m *Manager) filteredFonts() []ui.FontFamily {
	role := m.fontPickerOpen
	if role == "" {
		role = "body"
	}
	catalog := buildSortedFontCatalog(m.fontFamilies, role)
	q := strings.ToLower(strings.TrimSpace(m.fontQuery))
	if q == "" {
		out := make([]ui.FontFamily, len(catalog))
		for i, c := range catalog {
			out[i] = ui.FontFamily{Name: c.name, Monospaced: c.mono}
		}
		return out
	}

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
	out := make([]ui.FontFamily, len(ranked))
	for i, r := range ranked {
		out[i] = ui.FontFamily{Name: r.item.name, Monospaced: r.item.mono}
	}
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
	if current == "" {
		current = resolvedBodyFontName(m.fontFamilies)
	}
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
	listH := float32(48)
	if len(fonts) > 0 {
		listH = min(float32(min(len(fonts), 7))*34, listCapH)
	}
	popH := min(listH+54, maxH)
	x, y := internalui.ResolveOrigin(spec, popW, popH, winW, winH)

	ui.Overlay(c, func() {
		backdrop := ui.Box(c.Key("font-picker-backdrop")).Absolute().Left(0).Top(0).Right(0).Bottom(0)

		panel := ui.Column(c.Key("font-picker-menu-"+role)).Absolute().Left(x).Top(y).
			Width(popW).MaxHeight(maxH).Padding(6).Gap(4).Radius(theme.RadiusSurface).
			Background(pal.Menu).Border(1, pal.MenuBorder).
			Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.22)).Clip().
			Transition(internalui.PopupTransition())
		internalui.Probe(panel, "font-picker-menu")

		panel.Children(func() {
			// Search bar matching font_picker.rs:100-105.
			ui.Row(c.Key("font-picker-search-bar")).FillWidth().Padding(2, 8, 6, 8).Gap(8).
				AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.MenuSeparator).
				Children(func() {
					icons.Draw(c, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
					in := ui.TextInput(c.Key("font-picker-search"), &m.fontQuery).
						Placeholder(theme.Tr("搜索字体", "Search fonts")).
						Label(theme.Tr("搜索字体", "Search fonts")).
						FontSize(14).Height(28).PaddingX(0).
						Background(ui.Transparent).Border(0, ui.Transparent).
						Bind(&m.fontSearchHandle).AutoFocus().Grow(1).MinWidth(0)
					internalui.Probe(in, "font-picker-search")
					if in.Changed() {
						m.fontCursor = 0
						m.fontScrollToCursor = true
						fonts = m.filteredFonts()
						curListH := float32(48)
						if len(fonts) > 0 {
							curListH = min(float32(min(len(fonts), 7))*34, listCapH)
						}
						nx, ny := internalui.ResolveOrigin(spec, popW, min(curListH+54, maxH), winW, winH)
						panel.Left(nx).Top(ny)
					}
					in.OnTextKey(func(k ui.TextKey) bool {
						switch k.Key {
						case ui.KeyDown:
							if len(fonts) > 0 && m.fontCursor < len(fonts)-1 {
								m.fontCursor++
								m.fontScrollToCursor = true
							}
							return true
						case ui.KeyUp:
							if m.fontCursor > 0 {
								m.fontCursor--
								m.fontScrollToCursor = true
							}
							return true
						case ui.KeyEnter:
							if len(fonts) > 0 && m.fontCursor < len(fonts) {
								m.selectFont(role, fonts[m.fontCursor].Name)
							}
							return true
						case ui.KeyEscape:
							m.fontPickerOpen = ""
							return true
						}
						return false
					})
				})

			curListH := float32(48)
			if len(fonts) > 0 {
				curListH = min(float32(min(len(fonts), 7))*34, listCapH)
			}
			internalui.Scroll(c.Key("font-picker-list-"+role), &m.fontScroll).
				FillWidth().Height(curListH).Gap(0).Children(func() {
				if len(fonts) == 0 {
					ui.Row(c).FillWidth().Height(48).PaddingX(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, theme.Tr("没有匹配的字体", "No matching fonts")).FontSize(14).TextColor(pal.MenuMuted)
					})
					return
				}
				for i, f := range fonts {
					selected := strings.EqualFold(f.Name, current)
					highlighted := i == m.fontCursor
					row := ui.ButtonBase(c.Key("font-row-" + f.Name)).FillWidth().Height(34).PaddingX(10).
						AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusControl).
						Cursor(ui.CursorPointer).Label(f.Name)
					if selected {
						row.Background(pal.MenuChecked)
					} else if highlighted || row.Hovered() {
						row.Background(pal.MenuHover)
					}
					if highlighted && m.fontScrollToCursor {
						internalui.RevealActiveItem(row)
						m.fontScrollToCursor = false
					}
					row.Children(func() {
						ui.Text(c, f.Name).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine().Grow(1)
						if selected {
							icons.Draw(c, icons.Check, 16, pal.MonoIconColors(pal.Text))
						}
					})
					if row.Clicked() {
						m.selectFont(role, f.Name)
					}
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
	hideNonMD := st.HideNonMarkdownFiles != nil && *st.HideNonMarkdownFiles
	hideAssets := st.HideAssetsFolder == nil || *st.HideAssetsFolder
	assetsName := st.AssetsFolder
	if strings.TrimSpace(assetsName) == "" {
		assetsName = "assets"
	}

	ui.Column(c.Key("settings-folders-body")).FillWidth().Gap(24).Children(func() {
		// Card 1: Full-row Hide Non-Markdown Files checkbox (app_settings_folders_view.rs:111-122).
		settingsCardGap(c, 16, func() {
			label := theme.Tr("不显示 MD 格式以外的文件", "Hide Non-Markdown Files")
			rowBG := pal.Shell
			borderCol := pal.Hairline
			if hideNonMD {
				rowBG = pal.Selected
				borderCol = pal.Accent
			}
			chkRow := ui.ButtonBase(c.Key("folder-row-non-md")).FillWidth().MinHeight(46).
				Padding(8, 14, 8, 14).Radius(theme.RadiusSurface).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).
				Cursor(ui.CursorPointer).Label(label)
			if chkRow.Hovered() {
				borderCol = pal.Accent
			}
			chkRow.Background(rowBG).Border(1, borderCol)
			chkRow.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				internalui.RecordBounds("folder-hide-non-md", r)
				internalui.RecordBounds("settings-hide-non-md", r)
			})
			chkRow.Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
					icCol := pal.DuoIconColors(pal.Text)
					if hideNonMD {
						icCol = pal.DuoIconColors(pal.Accent)
					}
					icons.Draw(c, icons.Note, theme.IconRow, icCol)
					ui.Text(c, label).FontSize(14).FontWeight(theme.WeightControl).TextColor(pal.Text).AccessHidden(true)
				})
				folderCheckBox(c, hideNonMD)
			})
			if chkRow.Clicked() {
				nextVal := !hideNonMD
				m.mutateState(func(s *model.AppState) {
					v := nextVal
					s.HideNonMarkdownFiles = &v
				})
			}
		})

		// Card 2: Workspace Folder Rules (app_settings_folders_view.rs:123-188).
		settingsCardGap(c, 16, func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				cardTitle(c, theme.Tr("工作区文件夹清单", "Workspace Folder Rules"))
				ui.Text(c, theme.Tr("隐藏影响侧栏与搜索，不删除文件", "Hiding affects sidebar & search; files are not deleted")).
					FontSize(14).TextColor(pal.TextSecondary)
			})

			rules := append([]model.FolderRule(nil), st.FolderRules...)

			// Assets row + exact folder rules stack (gap=8).
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				folderTableRow(c, "settings-assets-row", hideAssets,
					func() {
						icons.Draw(c, icons.Folder, theme.IconRow, pal.DuoIconColors(pal.Text))
						assetsIn := ui.TextInput(c.Key("settings-assets-input"), &m.assetsDraft).
							Placeholder("assets").Label(theme.Tr("资源目录名", "Assets Folder")).
							Font("monospace").FontSize(14).Height(34).PaddingX(10).
							Radius(theme.RadiusControl).Background(pal.Shell).Border(1, pal.BtnBorder).
							Grow(1).MinWidth(0)
						internalui.Probe(assetsIn, "settings-assets-input")
						if assetsIn.Changed() || assetsIn.Submitted() {
							sanitized := strings.TrimSpace(m.assetsDraft)
							if sanitized != "" && !strings.ContainsAny(sanitized, `/\`) {
								m.mutateState(func(s *model.AppState) {
									s.AssetsFolder = sanitized
								})
							}
						}
					},
					func() {
						visIdx := 0
						if !hideAssets {
							visIdx = 1
						}
						if internalui.SegmentedControl(c, "settings-hide-assets", &visIdx, []internalui.SegmentOption{
							{ID: "settings-assets-hidden", Label: theme.Tr("隐藏", "Hidden")},
							{ID: "settings-assets-visible", Label: theme.Tr("显示", "Show")},
						}) {
							nextHide := visIdx == 0
							m.mutateState(func(s *model.AppState) {
								v := nextHide
								s.HideAssetsFolder = &v
							})
						}
						ui.Row(c).MinWidth(28).Justify(ui.End).Children(func() {
							ui.Text(c, theme.Tr("系统", "System")).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
						})
					},
				)

				for i, r := range rules {
					if r.Regex || strings.EqualFold(r.Pattern, assetsName) {
						continue
					}
					idx := i
					rule := r
					folderTableRow(c, "folder-exact-row-"+rule.Pattern, rule.Hidden,
						func() {
							icCol := pal.DuoIconColors(pal.Text)
							if rule.Hidden {
								icCol = pal.MonoIconColors(pal.TextSecondary)
							}
							icons.Draw(c, icons.Folder, theme.IconRow, icCol)
							ui.Text(c, rule.Pattern).Font("monospace").FontSize(14).TextColor(pal.Text).SingleLine()
						},
						func() {
							visIdx := 0
							if !rule.Hidden {
								visIdx = 1
							}
							if internalui.SegmentedControl(c, "folder-rule-vis-"+rule.Pattern, &visIdx, []internalui.SegmentOption{
								{Label: theme.Tr("隐藏", "Hidden")},
								{Label: theme.Tr("显示", "Show")},
							}) {
								rules[idx].Hidden = visIdx == 0
								m.saveFolderRules(rules)
							}
							ui.Row(c).MinWidth(28).Justify(ui.End).Children(func() {
								del := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
									ID:    "folder-rule-del-" + rule.Pattern,
									Label: theme.Tr("删除规则", "Remove Rule"),
								})
								if del.Clicked() {
									next := append(rules[:idx], rules[idx+1:]...)
									m.saveFolderRules(next)
								}
							})
						},
					)
				}
			})

			// Add exact folder rule input + button.
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					in := ui.TextInput(c.Key("settings-folder-rule-input"), &m.folderRuleDraft).
						Placeholder(theme.Tr("输入文件夹名称，如 build 或 drafts", "Enter folder name, e.g. build")).
						Label(theme.Tr("文件夹名称", "Folder Name")).
						FontSize(14).Height(34).PaddingX(10).Radius(theme.RadiusControl).
						Background(pal.Shell).Border(1, pal.BtnBorder).Grow(1).MinWidth(0)
					internalui.Probe(in, "settings-folder-rule-input")
					submitted := in.Submitted()
					if in.Changed() {
						m.folderRuleError = ""
					}
					addBtn := internalui.ActionButton(c, "settings-folder-rule-add", theme.Tr("添加文件夹", "Add Folder"), nil, internalui.ButtonSecondary, false)
					if addBtn.Clicked() || submitted {
						m.addFolderRule(false)
					}
				})
				if m.folderRuleError != "" {
					ui.Text(c, m.folderRuleError).FontSize(14).TextColor(pal.Accent)
				}
			})

			// Advanced Regex collapsible section (pt=12, border-top hairline, gap=10).
			var regexCount int
			for _, r := range rules {
				if r.Regex {
					regexCount++
				}
			}
			ui.Column(c).FillWidth().Padding(12, 0, 0, 0).Gap(10).
				BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
				ui.Row(c).FillWidth().Justify(ui.Start).Children(func() {
					advBtn := ui.ButtonBase(c.Key("settings-regex-toggle")).Gap(6).
						AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer).
						Label(theme.Tr("高级规则（正则表达式）", "Advanced Rules (Regex)"))
					advBtn.Children(func() {
						chevron := icons.ChevronRight
						if m.advancedRulesOpen {
							chevron = icons.ChevronDown
						}
						if m.advancedRulesOpen || advBtn.Hovered() {
							icons.Draw(c, chevron, 14, pal.DuoIconColors(pal.Text))
						} else {
							icons.Draw(c, chevron, 14, pal.MonoIconColors(pal.TextSecondary))
						}
						ui.Text(c, theme.Tr("高级规则（正则表达式）", "Advanced Rules (Regex)")).FontSize(14).TextColor(pal.TextSecondary)
						if regexCount > 0 {
							ui.Box(c).Padding(1, 6, 1, 6).Radius(theme.RadiusControl).
								Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
								ui.Text(c, fmt.Sprintf("%d", regexCount)).FontSize(14).TextColor(pal.TextSecondary)
							})
						}
					})
					internalui.Probe(advBtn, "settings-regex-toggle")
					if advBtn.Clicked() {
						m.advancedRulesOpen = !m.advancedRulesOpen
					}
				})

				if m.advancedRulesOpen {
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							var submitted bool
							ui.Row(c.Key("regex-slash-affix")).Grow(1).MinWidth(0).Height(34).PaddingX(10).
								AlignItems(ui.Center).Radius(theme.RadiusControl).
								Background(pal.Shell).Border(1, pal.BtnBorder).Children(func() {
								ui.Text(c, "/").Font("monospace").FontSize(14).FontWeight(theme.WeightMonoMedium).TextColor(pal.TextSecondary)
								in := ui.TextInput(c.Key("settings-regex-rule-input"), &m.regexRuleDraft).
									Placeholder("temp|cache").
									Label(theme.Tr("正则表达式", "Regex Pattern")).
									Font("monospace").FontSize(14).Height(32).PaddingX(4).
									Background(ui.Transparent).Border(0, ui.Transparent).Grow(1).MinWidth(0)
								internalui.Probe(in, "settings-regex-rule-input")
								submitted = in.Submitted()
								if in.Changed() {
									m.regexRuleError = ""
								}
								ui.Text(c, "/").Font("monospace").FontSize(14).FontWeight(theme.WeightMonoMedium).TextColor(pal.TextSecondary)
							})
							addBtn := internalui.ActionButton(c, "settings-regex-rule-add", theme.Tr("添加正则", "Add Regex"), nil, internalui.ButtonSecondary, false)
							if addBtn.Clicked() || submitted {
								m.addFolderRule(true)
							}
						})
						if m.regexRuleError != "" {
							ui.Text(c, m.regexRuleError).FontSize(14).TextColor(pal.Accent)
						}
						if regexCount > 0 {
							ui.Column(c).FillWidth().Padding(4, 0, 0, 0).Gap(8).Children(func() {
								for i, r := range rules {
									if !r.Regex {
										continue
									}
									idx := i
									rule := r
									folderTableRow(c, "folder-regex-row-"+rule.Pattern, rule.Hidden,
										func() {
											icCol := pal.DuoIconColors(pal.Text)
											if rule.Hidden {
												icCol = pal.MonoIconColors(pal.TextSecondary)
											}
											icons.Draw(c, icons.Code, theme.IconRow, icCol)
											ui.Text(c, "/"+rule.Pattern+"/").Font("monospace").FontSize(14).TextColor(pal.Text).SingleLine()
										},
										func() {
											visIdx := 0
											if !rule.Hidden {
												visIdx = 1
											}
											if internalui.SegmentedControl(c, "folder-regex-vis-"+rule.Pattern, &visIdx, []internalui.SegmentOption{
												{Label: theme.Tr("隐藏", "Hidden")},
												{Label: theme.Tr("显示", "Show")},
											}) {
												rules[idx].Hidden = visIdx == 0
												m.saveFolderRules(rules)
											}
											ui.Row(c).MinWidth(28).Justify(ui.End).Children(func() {
												del := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
													ID:    "folder-regex-del-" + rule.Pattern,
													Label: theme.Tr("删除正则规则", "Remove Regex Rule"),
												})
												if del.Clicked() {
													next := append(rules[:idx], rules[idx+1:]...)
													m.saveFolderRules(next)
												}
											})
										},
									)
								}
							})
						}
					})
				}
			})
		})

		// Card 3: Global Search Ignore List (app_settings_folders_view.rs:29-57, 189-191).
		ignoreCard := ui.Column(c.Key("settings-ignore-search")).FillWidth().Padding(18, 20, 18, 20).Gap(16).
			Radius(theme.RadiusSurface).Background(pal.Card).Border(1, pal.Hairline)
		internalui.Probe(ignoreCard, "settings-ignore-search")
		ignoreCard.Children(func() {
			cardTitle(c, theme.Tr("全局搜索忽略列表", "Global Search Ignore List"))
			activeNB := m.activeNotebookPath(st)
			notebooks := m.knownNotebooks(st)
			if len(notebooks) == 0 {
				ui.Text(c, theme.Tr("暂无已打开的笔记本目录", "No notebook folders yet")).FontSize(14).TextColor(pal.TextSecondary)
				return
			}
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for i, nb := range notebooks {
					idx := i
					folderPath := nb
					ignored := slices.Contains(st.IgnoredSearchFolders, folderPath)
					isCurrent := folderPath == activeNB
					id := fmt.Sprintf("settings-ignore-search-%d", idx)
					folderName := filepath.Base(folderPath)
					shortPath := theme.DisplayFolderPath(folderPath)

					folderTableRow(c, id, ignored,
						func() {
							icCol := pal.DuoIconColors(pal.Text)
							if ignored {
								icCol = pal.MonoIconColors(pal.TextSecondary)
							}
							icons.Draw(c, icons.Folder, theme.IconRow, icCol)
							ui.Text(c, folderName).Font("monospace").FontSize(14).TextColor(pal.Text).SingleLine().Shrink(0)
							ui.Text(c, shortPath).Font("monospace").FontSize(14).TextColor(pal.TextSecondary).SingleLine().Grow(1)
						},
						func() {
							pillLabel := theme.Tr("不参与全局搜索", "Exclude from Global Search")
							pillBG := pal.Shell
							pillBD := pal.Hairline
							pillFG := pal.TextSecondary
							if ignored {
								pillBG = pal.Selected
								pillBD = pal.Accent
								pillFG = pal.Text
							}
							pill := ui.ButtonBase(c.Key(id + "-btn")).Height(32).PaddingX(10).Gap(8).
								AlignItems(ui.Center).Radius(theme.RadiusControl).
								Cursor(ui.CursorPointer).Label(pillLabel)
							if pill.Hovered() {
								pillBD = pal.Accent
								pillFG = pal.Text
							}
							pill.Background(pillBG).Border(1, pillBD)
							pill.DrawOver(func(_ *ui.Painter, r ui.Rect) {
								internalui.RecordBounds(id+"-btn", r)
								if idx == 0 {
									internalui.RecordBounds("folder-search-ignore-0", r)
								}
							})
							pill.Children(func() {
								folderCheckBox(c, ignored)
								ui.Text(c, pillLabel).FontSize(14).FontWeight(theme.WeightControl).TextColor(pillFG).SingleLine()
							})
							if pill.Clicked() {
								m.toggleSearchIgnoreFolder(folderPath)
							}

							ui.Row(c).MinWidth(28).Justify(ui.End).Children(func() {
								if isCurrent {
									ui.Text(c, theme.Tr("当前", "Current")).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
								} else {
									rmBtn := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
										ID:    fmt.Sprintf("folder-remove-notebook-%d", idx),
										Label: theme.Tr("移除笔记本", "Remove Notebook"),
									})
									if rmBtn.Clicked() {
										m.forgetNotebook(folderPath)
									}
								}
							})
						},
					)
				}
			})
		})
	})
}

func folderCheckBox(c *ui.Context, checked bool) {
	pal := theme.Ocean()
	bd := pal.BtnBorder
	if checked {
		bd = pal.Accent
	}
	ui.Box(c).Size(15, 15).Radius(theme.RadiusMicroSM).Center().Shrink(0).
		Background(pal.Card).Border(1, bd).Children(func() {
		if checked {
			icons.Draw(c, icons.Check, theme.IconMicro, pal.DuoIconColors(pal.Accent))
		}
	})
}

func folderTableRow(c *ui.Context, key string, isHidden bool, left func(), right func()) {
	pal := theme.Ocean()
	bg := pal.Card
	if isHidden {
		bg = pal.Shell
	}
	ui.Row(c.Key(key)).FillWidth().MinWidth(0).MinHeight(46).Padding(4, 12, 4, 12).
		Radius(theme.RadiusSurface).Background(bg).Border(1, pal.Hairline).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(left)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Shrink(0).Children(right)
	})
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
	m.mutateState(func(s *model.AppState) {
		var keep []string
		for _, nb := range s.Notebooks {
			if nb != folder {
				keep = append(keep, nb)
			}
		}
		s.Notebooks = keep
		var keepIgnored []string
		for _, nb := range s.IgnoredSearchFolders {
			if nb != folder {
				keepIgnored = append(keepIgnored, nb)
			}
		}
		s.IgnoredSearchFolders = keepIgnored
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

// renderSettingsShortcutsTab renders the exact 7-row Shortcuts list from app_settings_tabs.rs:74-103:
// 1. 全局唤出 (recorder)
// 2. 速记小窗 (recorder)
// 3. 搜索笔记 (static keycap)
// 4. 当前笔记查找 (static keycap)
// 5. 新建笔记 (static keycap)
// 6. AI 助手面板 (static keycap)
// 7. 性能与占用 (static keycap)
func (m *Manager) renderSettingsShortcutsTab(c *ui.Context) {
	pal := theme.Ocean()
	st := m.currentState()
	searchSpec := EffectiveShortcut(st, "global_summon")
	quickSpec := EffectiveShortcut(st, "quick_note")
	customized := normalizeShortcutSpec(searchSpec) != normalizeShortcutSpec(defaultSearchShortcut) ||
		normalizeShortcutSpec(quickSpec) != normalizeShortcutSpec(defaultQuickShortcut) ||
		strings.TrimSpace(st.ShortcutSearch) != "" ||
		strings.TrimSpace(st.ShortcutQuick) != "" ||
		len(st.CustomShortcuts) > 0

	settingsCardGap(c, 12, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			cardTitle(c, theme.Tr("快捷键", "Shortcuts"))
			if customized {
				rstLabel := theme.Tr("恢复默认", "Reset")
				rst := ui.ButtonBase(c.Key("settings-shortcuts-reset")).Padding(4, 8, 4, 8).
					Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(rstLabel)
				if rst.Hovered() {
					rst.Background(pal.Hover)
				}
				rst.Children(func() {
					ui.Text(c, rstLabel).FontSize(14).TextColor(pal.Accent).SingleLine()
				})
				internalui.Probe(rst, "settings-shortcuts-reset")
				if rst.Clicked() {
					m.recordingShortcut = ""
					m.shortcutConflictNotice = ""
					m.mutateState(func(s *model.AppState) {
						s.ShortcutSearch = ""
						s.ShortcutQuick = ""
						s.CustomShortcuts = nil
					})
				}
			}
		})

		if m.shortcutConflictNotice != "" {
			noticeRow := ui.Row(c.Key("settings-shortcut-conflict-notice")).FillWidth().
				Padding(8, 12, 8, 12).Radius(theme.RadiusControl).
				Background(pal.WarningSurface).Border(1, pal.Warning.Alpha(0.35))
			internalui.Probe(noticeRow, "settings-shortcut-conflict-notice")
			noticeRow.Children(func() {
				ui.Text(c, m.shortcutConflictNotice).FontSize(14).TextColor(pal.Warning)
			})
		}

		settingRow(c, theme.Tr("全局唤出", "Global Summon"), func() {
			m.shortcutRecorderTray(c, "settings-rec-search", "global_summon", searchSpec)
		})
		settingRow(c, theme.Tr("速记小窗", "Quick Note"), func() {
			m.shortcutRecorderTray(c, "settings-rec-quick", "quick_note", quickSpec)
		})
		settingRow(c, theme.Tr("搜索笔记", "Search Notes"), func() {
			staticKeycapTray(c, "settings-keycap-search", EffectiveShortcut(st, "search"))
		})
		settingRow(c, theme.Tr("当前笔记查找", "Find in Note"), func() {
			staticKeycapTray(c, "settings-keycap-find", "cmd-f")
		})
		settingRow(c, theme.Tr("新建笔记", "New Note"), func() {
			staticKeycapTray(c, "settings-keycap-new", EffectiveShortcut(st, "new_note"))
		})
		settingRow(c, theme.Tr("AI 助手面板", "AI Assistant Panel"), func() {
			staticKeycapTray(c, "settings-keycap-ai", EffectiveShortcut(st, "toggle_ai"))
		})
		settingRow(c, theme.Tr("性能与占用", "Performance & Usage"), func() {
			staticKeycapTray(c, "settings-keycap-perf", EffectiveShortcut(st, "performance"))
		})
	})
}

func (m *Manager) shortcutRecorderTray(c *ui.Context, id, kind, spec string) {
	pal := theme.Ocean()
	recording := m.recordingShortcut == kind
	accLabel := formatShortcutLabel(spec)
	if recording {
		accLabel = theme.Tr("按下快捷键…", "Press shortcut…")
	}
	trayBG := pal.Shell
	if pal.Dark {
		trayBG = pal.CodeBG
	}
	borderCol := pal.BtnBorder
	if recording {
		trayBG = pal.Selected
		borderCol = pal.Accent
	}
	btn := ui.ButtonBase(c.Key(id)).Height(32).PaddingX(5).Gap(4).
		AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(accLabel)
	if !recording && btn.Hovered() {
		borderCol = pal.Accent
	}
	btn.Background(trayBG).Border(1, borderCol)
	btn.Children(func() {
		if recording {
			ui.Box(c).PaddingX(6).Children(func() {
				ui.Text(c, accLabel).FontSize(14).FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
			})
			return
		}
		for _, part := range hotkeyParts(spec) {
			keycapChip(c, part, false)
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

func staticKeycapTray(c *ui.Context, id, spec string) {
	pal := theme.Ocean()
	trayBG := pal.Shell
	if pal.Dark {
		trayBG = pal.CodeBG
	}
	tray := ui.Row(c.Key(id)).Height(32).PaddingX(5).Gap(4).
		AlignItems(ui.Center).Justify(ui.Center).
		Radius(theme.RadiusControl).Background(trayBG).Border(1, pal.Hairline)
	internalui.Probe(tray, id)
	tray.Children(func() {
		for _, part := range hotkeyParts(spec) {
			keycapChip(c, part, false)
		}
	})
}

func keycapChip(c *ui.Context, part string, recording bool) {
	pal := theme.Ocean()
	bg := ui.Hex("#f4f4f2")
	if pal.Dark {
		bg = ui.Hex("#252a31")
	}
	bd := pal.BtnBorder
	fg := pal.Text
	if recording {
		bd = pal.Accent
		fg = pal.Accent
	}
	ui.Row(c).MinWidth(22).Height(22).PaddingX(6).Radius(theme.RadiusMicroMD).
		Background(bg).Border(1, bd).AlignItems(ui.Center).Justify(ui.Center).
		Children(func() {
			if glyph, ok := keycapVectorIcon(part); ok {
				icons.Draw(c, glyph, 15, pal.MonoIconColors(fg))
				return
			}
			ui.Text(c, part).FontSize(12).FontWeight(theme.WeightMonoMedium).TextColor(fg).SingleLine().AccessHidden(true)
		})
}

func keycapVectorIcon(part string) (icons.Icon, bool) {
	switch part {
	case "⌘":
		return icons.KeyCommand, true
	case "⌥":
		return icons.KeyOption, true
	case "⇧":
		return icons.KeyShift, true
	case "⌃":
		return icons.KeyControl, true
	case "⌫":
		return icons.KeyBackspace, true
	case "↩", "↵", "⏎":
		return icons.KeyReturn, true
	case "↑":
		return icons.ArrowUp, true
	case "↓":
		return icons.ArrowDown, true
	default:
		return "", false
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
	var ctrl, alt, shift, cmd bool
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
		keyLabel = "⌫"
	case "enter":
		keyLabel = "↩"
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
		if ctrl {
			out = append(out, "Ctrl")
		}
		if shift {
			out = append(out, "Shift")
		}
		if alt {
			out = append(out, "Alt")
		}
		if cmd {
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

func (m *Manager) applyRecordedShortcut(action, spec string) {
	m.recordingShortcut = ""
	m.justRecordedShortcut = true
	normNew := normalizeShortcutSpec(spec)
	var conflictMsg string

	m.mutateState(func(s *model.AppState) {
		prevSpec := EffectiveShortcut(*s, action)
		if normalizeShortcutSpec(prevSpec) == normNew {
			setActionShortcut(s, action, spec)
			return
		}
		for _, other := range shortcutActionSpecs {
			if other.action == action {
				continue
			}
			if normalizeShortcutSpec(EffectiveShortcut(*s, other.action)) == normNew {
				reassigned := prevSpec
				if normalizeShortcutSpec(prevSpec) == normalizeShortcutSpec(other.def) {
					reassigned = ""
				}
				setActionShortcut(s, other.action, reassigned)
				otherLabel := theme.Tr(other.zh, other.en)
				conflictMsg = theme.Tr(
					fmt.Sprintf("已将冲突快捷键从「%s」调整为 %s", otherLabel, formatShortcutLabel(prevSpec)),
					fmt.Sprintf("Reassigned conflicting shortcut from %s to %s", otherLabel, formatShortcutLabel(prevSpec)),
				)
				break
			}
		}
		setActionShortcut(s, action, spec)
	})
	m.shortcutConflictNotice = conflictMsg
}

func normalizeShortcutSpec(spec string) string {
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "+", "-")
	var ctrl, alt, shift, cmd bool
	var keyPart string
	for _, p := range strings.Split(s, "-") {
		switch p {
		case "":
			continue
		case "cmd", "cmdorctrl", "meta", "super", "command", "secondary":
			cmd = true
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
		default:
			keyPart = p
		}
	}
	if keyPart == "" {
		return ""
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
	if ev.Mods&ui.Ctrl != 0 {
		parts = append(parts, "ctrl")
	}
	if ev.Mods&ui.Alt != 0 {
		parts = append(parts, "alt")
	}
	if ev.Mods&ui.Shift != 0 {
		parts = append(parts, "shift")
	}
	if ev.Mods&ui.Super != 0 {
		parts = append(parts, "cmd")
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

func settingsCard(c *ui.Context, children func()) {
	settingsCardGap(c, 16, children)
}

func settingsCardGap(c *ui.Context, gap float32, children func()) {
	pal := theme.Ocean()
	ui.Column(c).FillWidth().MinWidth(0).Padding(18, 20, 18, 20).Gap(gap).Radius(theme.RadiusSurface).
		Background(pal.Card).Border(1, pal.Hairline).Children(children)
}

func cardTitle(c *ui.Context, title string) {
	pal := theme.Ocean()
	ui.Text(c, title).FontSize(theme.FontCardTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text)
}

func settingRow(c *ui.Context, label string, control func()) {
	pal := theme.Ocean()
	ui.Row(c).FillWidth().MinWidth(0).MinHeight(36).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(16).Children(func() {
		ui.Text(c, label).FontSize(14).TextColor(pal.Text).SingleLine().Shrink(0)
		ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(control)
	})
}
