package modals

import (
	"slices"
	"strings"
	"sync"

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

// Manager owns the full-window Settings page and every modal overlay in Block.
type Manager struct {
	dataDir string
	host    model.WorkspaceHost

	// Injectable callbacks for host state, AI settings, search, history, diff, and export.
	GetState         func() model.AppState
	OnStateChanged   func(func(*model.AppState))
	RenderAISettings func(c *ui.Context)
	SearchFn         func(query string, allNotebooks bool) ([]model.SearchHit, error)
	ListHistoryFn    func(notePath string) ([]model.HistorySnapshot, error)
	ReadHistoryFn    func(notePath, snapID string) (string, error)
	DiffLinesFn      func(oldText, newText string) []model.DiffLine
	ExportFn         func(format model.ExportFormat, outPath string) error
	FeedbackSubmitFn func(category, message, contact string) error

	fallbackState model.AppState

	// Settings page state.
	settingsOpen        bool
	settingsTab         string
	recordingShortcut   string
	fontPickerOpen      string
	fontQuery           string
	fontCursor          int
	fontFamilies        []ui.FontFamily
	fontScroll          ui.ScrollState
	assetsDraft         string
	folderRuleDraft     string
	regexRuleDraft      string
	folderRuleError     string
	regexRuleError      string
	advancedRulesOpen   bool
	settingsScroll      ui.ScrollState
	settingsFocusHandle ui.Handle
	fontSearchHandle    ui.Handle

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

// IsRecordingShortcut reports whether the shortcut recorder is actively capturing keys.
func (m *Manager) IsRecordingShortcut() bool {
	return m.settingsOpen && m.recordingShortcut != ""
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

	// Capture keyboard for shortcut recording or Escape closing.
	root.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind != ui.InputKeyDown {
			return
		}
		if m.recordingShortcut != "" {
			if ev.Key == ui.KeyEscape && ev.Mods == 0 {
				m.recordingShortcut = ""
				return
			}
			if spec, ok := inputEventToShortcutSpec(ev); ok {
				m.applyRecordedShortcut(m.recordingShortcut, spec)
			}
			return
		}
		if ev.Key == ui.KeyEscape && ev.Mods == 0 {
			if m.fontPickerOpen != "" {
				m.fontPickerOpen = ""
				return
			}
			m.CloseSettings()
		}
	})

	root.Children(func() {
		// Left category navigation sidebar.
		ui.Column(c.Key("settings-sidebar")).Width(240).FillHeight().Shrink(0).
			Background(pal.Sidebar).BorderWidth(0, 1, 0, 0).BorderColor(pal.SidebarBorder).
			Children(func() {
				ui.Box(c).Padding(18, 16, 10, 16).Children(func() {
					ui.Text(c, theme.Tr("设置", "Settings")).FontSize(18).FontWeight(600).TextColor(pal.Text)
				})
				ui.Column(c).PaddingX(10).Gap(4).Grow(1).Children(func() {
					for _, spec := range settingsTabs {
						active := tab == spec.id
						navID := "settings-tab-" + spec.id
						title := theme.Tr(spec.zh, spec.en)
						btn := ui.ButtonBase(c.Key(navID)).FillWidth().Height(36).PaddingX(10).Gap(8).
							AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
							Label(title)
						if active {
							btn.Background(pal.Selected).Border(1, pal.Accent)
						} else if btn.Hovered() {
							btn.Background(pal.Hover)
						}
						btn.Children(func() {
							if active {
								icons.Draw(c, spec.icon, 16, pal.DuoIconColors(pal.Accent))
							} else {
								icons.Draw(c, spec.icon, 16, pal.MonoIconColors(pal.TextSecondary))
							}
							col := pal.TextSecondary
							weight := 400
							if active {
								col = pal.Text
								weight = 500
							}
							ui.Text(c, title).FontSize(14).FontWeight(weight).TextColor(col).SingleLine()
						})
						internalui.Probe(btn, navID)
						if btn.Clicked() {
							m.selectSettingsTab(spec.id)
						}
					}
				})
			})

		// Right content area.
		ui.Column(c.Key("settings-main")).Grow(1).FillHeight().Background(pal.SettingsBG).Children(func() {
			// Top breadcrumb + back button.
			ui.Row(c.Key("settings-header")).FillWidth().Height(56).PaddingX(28).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).
				BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
				Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, theme.Tr("设置", "Settings")).FontSize(14).TextColor(pal.TextSecondary)
						ui.Text(c, "/").FontSize(14).TextColor(pal.TextSecondary)
						ui.Text(c, settingsTabTitle(tab)).FontSize(18).FontWeight(600).TextColor(pal.Text)
					})
					backBtn := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
						ID:      "settings-back",
						Label:   theme.Tr("返回", "Back"),
						Tooltip: theme.Tr("关闭设置", "Close Settings"),
						Size:    32,
					})
					if backBtn.Clicked() {
						m.CloseSettings()
					}
				})

			// Scrollable body.
			ui.Scroll(c.Key("settings-scroll")).TrackScroll(&m.settingsScroll).Grow(1).FillWidth().
				Padding(24, 28, 48, 28).Gap(20).Children(func() {
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
	})

	if m.fontPickerOpen != "" {
		m.renderFontPickerOverlay(c)
	}
}

func (m *Manager) selectSettingsTab(tab string) {
	m.recordingShortcut = ""
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

	// Card 1: Theme & Language.
	settingsCard(c, func() {
		ui.Text(c, theme.Tr("主题与语言", "Theme & Language")).FontSize(16).FontWeight(600).TextColor(pal.Text)

		// Theme segmented control.
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

		// Language segmented control.
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

	// Card 2: Editor Typography & Fonts.
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
	edDisplay := edFont
	if edDisplay == "" {
		edDisplay = theme.Tr("系统默认无衬线", "System Sans")
	}
	cdDisplay := cdFont
	if cdDisplay == "" {
		cdDisplay = "Menlo"
	}

	settingsCard(c, func() {
		ui.Text(c, theme.Tr("编辑器", "Editor")).FontSize(16).FontWeight(600).TextColor(pal.Text)

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
}

func (m *Manager) fontPickerTrigger(c *ui.Context, id, role, label string) {
	pal := theme.Ocean()
	open := m.fontPickerOpen == role
	borderCol := pal.BtnBorder
	if open {
		borderCol = pal.Accent
	}
	btn := ui.ButtonBase(c.Key(id)).Width(240).Height(34).PaddingX(12).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusControl).
		Background(pal.Shell).Border(1, borderCol).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(14).TextColor(pal.Text).SingleLine().Grow(1)
		glyph := icons.ChevronDown
		if open {
			glyph = icons.ChevronUp
		}
		icons.Draw(c, glyph, 14, pal.MonoIconColors(pal.TextSecondary))
	})
	internalui.Probe(btn, id)
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
	if len(m.fontFamilies) == 0 {
		m.fontFamilies = ui.FontFamilies()
	}
}

func (m *Manager) filteredFonts() []ui.FontFamily {
	q := strings.ToLower(strings.TrimSpace(m.fontQuery))
	role := m.fontPickerOpen
	var out []ui.FontFamily
	for _, f := range m.fontFamilies {
		if role == "code" && !f.Monospaced && q == "" {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(f.Name), q) {
			continue
		}
		out = append(out, f)
	}
	if role == "code" && len(out) == 0 && q == "" {
		return m.fontFamilies
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
	open := true
	title := theme.Tr("选择正文字体", "Choose Body Font")
	if m.fontPickerOpen == "code" {
		title = theme.Tr("选择代码字体", "Choose Code Font")
	}
	edFont, cdFont, _ := theme.Typography()
	current := edFont
	if m.fontPickerOpen == "code" {
		current = cdFont
	}

	internalui.ModalCard(c, "font-picker-modal", title, &open, 360, 420, func() {
		fonts := m.filteredFonts()
		ui.Column(c).FillWidth().Padding(14).Gap(10).Children(func() {
			ui.Text(c, title).FontSize(16).FontWeight(600).TextColor(pal.Text)

			in := ui.TextInput(c.Key("font-picker-search"), &m.fontQuery).
				Placeholder(theme.Tr("搜索字体", "Search fonts")).
				Label(theme.Tr("搜索字体", "Search fonts")).
				Bind(&m.fontSearchHandle).AutoFocus().FillWidth()
			internalui.Probe(in, "font-picker-search")
			if in.Changed() {
				m.fontCursor = 0
				fonts = m.filteredFonts()
			}
			in.OnTextKey(func(k ui.TextKey) bool {
				switch k.Key {
				case ui.KeyDown:
					if len(fonts) > 0 && m.fontCursor < len(fonts)-1 {
						m.fontCursor++
					}
					return true
				case ui.KeyUp:
					if m.fontCursor > 0 {
						m.fontCursor--
					}
					return true
				case ui.KeyEnter:
					if len(fonts) > 0 && m.fontCursor < len(fonts) {
						m.selectFont(m.fontPickerOpen, fonts[m.fontCursor].Name)
					}
					return true
				case ui.KeyEscape:
					m.fontPickerOpen = ""
					return true
				}
				return false
			})

			ui.Scroll(c.Key("font-picker-list")).TrackScroll(&m.fontScroll).
				FillWidth().Height(260).Gap(2).Children(func() {
				if len(fonts) == 0 {
					ui.Box(c).Padding(12).Children(func() {
						ui.Text(c, theme.Tr("没有匹配的字体", "No matching fonts")).FontSize(14).TextColor(pal.TextSecondary)
					})
					return
				}
				limit := min(len(fonts), 80)
				for i := 0; i < limit; i++ {
					f := fonts[i]
					selected := strings.EqualFold(f.Name, current)
					highlighted := i == m.fontCursor
					row := ui.ButtonBase(c.Key("font-row-"+f.Name)).FillWidth().Height(34).PaddingX(10).
						AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusControl).
						Cursor(ui.CursorPointer).Label(f.Name)
					if highlighted || row.Hovered() {
						row.Background(pal.Hover)
					} else if selected {
						row.Background(pal.Selected)
					}
					row.Children(func() {
						ui.Text(c, f.Name).FontSize(14).TextColor(pal.Text).SingleLine().Grow(1)
						if selected {
							icons.Draw(c, icons.Check, 15, pal.MonoIconColors(pal.Accent))
						}
					})
					if row.Clicked() {
						m.selectFont(m.fontPickerOpen, f.Name)
					}
				}
			})
		})
	})
	if !open {
		m.fontPickerOpen = ""
	}
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

	// Card 1: Hide Non-Markdown Files toggle.
	settingsCard(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
			lblRow := ui.Row(c).Gap(10).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			lblRow.Children(func() {
				icons.Draw(c, icons.Note, 16, pal.MonoIconColors(pal.Text))
				ui.Text(c, theme.Tr("不显示 MD 格式以外的文件", "Hide Non-Markdown Files")).
					FontSize(14).TextColor(pal.Text).AccessHidden(true)
			})
			val := hideNonMD
			toggled := internalui.TogglePill(c, "settings-hide-non-md", theme.Tr("隐藏非 Markdown 文件", "Hide Non-Markdown Files"), &val, false)
			if lblRow.Clicked() {
				val = !val
				toggled = true
			}
			if toggled {
				m.mutateState(func(s *model.AppState) {
					v := val
					s.HideNonMarkdownFiles = &v
				})
			}
		})
	})

	// Card 2: Assets folder + custom exact/regex rules.
	settingsCard(c, func() {
		ui.Text(c, theme.Tr("工作区文件夹清单", "Workspace Folder Rules")).FontSize(16).FontWeight(600).TextColor(pal.Text)

		// Assets folder row.
		ui.Row(c.Key("settings-assets-row")).FillWidth().Padding(8, 10, 8, 10).Radius(theme.RadiusControl).
			Background(pal.Shell).Border(1, pal.Hairline).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(func() {
					icons.Draw(c, icons.Folder, 16, pal.DuoIconColors(pal.Text))
					assetsIn := ui.TextInput(c.Key("settings-assets-input"), &m.assetsDraft).
						Placeholder("assets").Label(theme.Tr("资源目录名", "Assets Folder")).Width(180)
					internalui.Probe(assetsIn, "settings-assets-input")
					if assetsIn.Changed() || assetsIn.Submitted() {
						sanitized := strings.TrimSpace(m.assetsDraft)
						if sanitized != "" && !strings.ContainsAny(sanitized, `/\`) {
							m.mutateState(func(s *model.AppState) {
								s.AssetsFolder = sanitized
							})
						}
					}
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, theme.Tr("隐藏资源目录", "Hide Assets")).FontSize(14).TextColor(pal.TextSecondary)
					val := hideAssets
					if internalui.TogglePill(c, "settings-hide-assets", theme.Tr("隐藏资源目录", "Hide Assets Folder"), &val, false) {
						m.mutateState(func(s *model.AppState) {
							v := val
							s.HideAssetsFolder = &v
						})
					}
				})
			})

		// Exact rules list.
		rules := append([]model.FolderRule(nil), st.FolderRules...)
		for i, r := range rules {
			if r.Regex || strings.EqualFold(r.Pattern, assetsName) {
				continue
			}
			idx := i
			rule := r
			ui.Row(c.Key("folder-exact-row-"+rule.Pattern)).FillWidth().Padding(8, 10, 8, 10).
				Radius(theme.RadiusControl).Background(pal.Shell).Border(1, pal.Hairline).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(func() {
					icons.Draw(c, icons.Folder, 16, pal.MonoIconColors(pal.TextSecondary))
					ui.Text(c, rule.Pattern).FontSize(14).TextColor(pal.Text).SingleLine()
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					visIdx := 0
					if !rule.Hidden {
						visIdx = 1
					}
					if internalui.SegmentedControl(c, "folder-rule-vis-"+rule.Pattern, &visIdx, []internalui.SegmentOption{
						{Label: theme.Tr("隐藏", "Hide")},
						{Label: theme.Tr("显示", "Show")},
					}) {
						rules[idx].Hidden = visIdx == 0
						m.saveFolderRules(rules)
					}
					del := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
						ID:    "folder-rule-del-" + rule.Pattern,
						Label: theme.Tr("删除规则", "Remove Rule"),
					})
					if del.Clicked() {
						next := append(rules[:idx], rules[idx+1:]...)
						m.saveFolderRules(next)
					}
				})
			})
		}

		// Add exact rule input + button.
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("settings-folder-rule-input"), &m.folderRuleDraft).
				Placeholder(theme.Tr("输入文件夹名称，如 build 或 drafts", "Enter folder name, e.g. build")).
				Label(theme.Tr("文件夹名称", "Folder Name")).Grow(1)
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
			ui.Text(c, m.folderRuleError).FontSize(14).TextColor(pal.Danger)
		}

		// Collapsible regex rules section.
		ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
		advBtn := ui.ButtonBase(c.Key("settings-regex-toggle")).Padding(4, 6, 4, 6).Gap(6).
			AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
			Label(theme.Tr("高级规则（正则表达式）", "Advanced Rules (Regex)"))
		advBtn.Children(func() {
			chevron := icons.ChevronRight
			if m.advancedRulesOpen {
				chevron = icons.ChevronDown
			}
			icons.Draw(c, chevron, 14, pal.MonoIconColors(pal.TextSecondary))
			ui.Text(c, theme.Tr("高级规则（正则表达式）", "Advanced Rules (Regex)")).FontSize(14).TextColor(pal.TextSecondary)
		})
		internalui.Probe(advBtn, "settings-regex-toggle")
		if advBtn.Clicked() {
			m.advancedRulesOpen = !m.advancedRulesOpen
		}

		if m.advancedRulesOpen {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c.Key("settings-regex-rule-input"), &m.regexRuleDraft).
					Placeholder(theme.Tr("temp|cache", "temp|cache")).
					Label(theme.Tr("正则表达式", "Regex Pattern")).Grow(1)
				internalui.Probe(in, "settings-regex-rule-input")
				submitted := in.Submitted()
				if in.Changed() {
					m.regexRuleError = ""
				}
				addBtn := internalui.ActionButton(c, "settings-regex-rule-add", theme.Tr("添加正则", "Add Regex"), nil, internalui.ButtonSecondary, false)
				if addBtn.Clicked() || submitted {
					m.addFolderRule(true)
				}
			})
			if m.regexRuleError != "" {
				ui.Text(c, m.regexRuleError).FontSize(14).TextColor(pal.Danger)
			}
			for i, r := range rules {
				if !r.Regex {
					continue
				}
				idx := i
				rule := r
				ui.Row(c.Key("folder-regex-row-"+rule.Pattern)).FillWidth().Padding(8, 10, 8, 10).
					Radius(theme.RadiusControl).Background(pal.Shell).Border(1, pal.Hairline).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(func() {
						icons.Draw(c, icons.Code, 16, pal.MonoIconColors(pal.TextSecondary))
						ui.Text(c, "/"+rule.Pattern+"/").FontSize(14).TextColor(pal.Text).SingleLine()
					})
					del := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
						ID:    "folder-regex-del-" + rule.Pattern,
						Label: theme.Tr("删除正则规则", "Remove Regex Rule"),
					})
					if del.Clicked() {
						next := append(rules[:idx], rules[idx+1:]...)
						m.saveFolderRules(next)
					}
				})
			}
		}
	})

	// Card 3: Global Search Ignore List.
	ignoreCard := ui.Column(c.Key("settings-ignore-search")).FillWidth().Padding(16).Gap(12).
		Radius(theme.RadiusCard).Background(pal.Card).Border(1, pal.Hairline)
	internalui.Probe(ignoreCard, "settings-ignore-search")
	ignoreCard.Children(func() {
		ui.Text(c, theme.Tr("全局搜索忽略列表", "Global Search Ignore List")).FontSize(16).FontWeight(600).TextColor(pal.Text)
		notebooks := m.knownNotebooks(st)
		if len(notebooks) == 0 {
			ui.Text(c, theme.Tr("暂无已打开的笔记本目录", "No notebook folders yet")).FontSize(14).TextColor(pal.TextSecondary)
		}
		for i, nb := range notebooks {
			ignored := slices.Contains(st.IgnoredSearchFolders, nb)
			id := "settings-ignore-search-" + string(rune('0'+i))
			if i >= 10 {
				id = "settings-ignore-search-item-" + nb
			}
			ui.Row(c.Key(id)).FillWidth().Padding(8, 10, 8, 10).Radius(theme.RadiusControl).
				Background(pal.Shell).Border(1, pal.Hairline).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).
				Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Grow(1).Children(func() {
						icons.Draw(c, icons.Folder, 16, pal.MonoIconColors(pal.TextSecondary))
						ui.Text(c, theme.DisplayFolderPath(nb)).FontSize(14).TextColor(pal.Text).SingleLine().Grow(1)
					})
					pillLabel := theme.Tr("参与搜索", "Included")
					style := internalui.ButtonSecondary
					if ignored {
						pillLabel = theme.Tr("已忽略", "Ignored")
						style = internalui.ButtonDanger
					}
					btn := internalui.ActionButton(c, id+"-btn", pillLabel, nil, style, false)
					if btn.Clicked() {
						m.toggleSearchIgnoreFolder(nb)
					}
				})
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
	if m.host != nil {
		add(m.host.ActiveNotebook())
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

func (m *Manager) renderSettingsShortcutsTab(c *ui.Context) {
	pal := theme.Ocean()
	st := m.currentState()
	searchSpec := st.ShortcutSearch
	if searchSpec == "" {
		searchSpec = defaultSearchShortcut
	}
	quickSpec := st.ShortcutQuick
	if quickSpec == "" {
		quickSpec = defaultQuickShortcut
	}
	customized := searchSpec != defaultSearchShortcut || quickSpec != defaultQuickShortcut

	settingsCard(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, theme.Tr("快捷键", "Shortcuts")).FontSize(16).FontWeight(600).TextColor(pal.Text)
			if customized {
				rst := internalui.ActionButton(c, "settings-shortcuts-reset", theme.Tr("恢复默认", "Reset"), nil, internalui.ButtonGhost, false)
				if rst.Clicked() {
					m.recordingShortcut = ""
					m.mutateState(func(s *model.AppState) {
						s.ShortcutSearch = ""
						s.ShortcutQuick = ""
					})
				}
			}
		})

		settingRow(c, theme.Tr("全局唤出", "Global Summon"), func() {
			m.shortcutRecorderButton(c, "settings-rec-search", "search", searchSpec)
		})
		settingRow(c, theme.Tr("速记小窗", "Quick Note"), func() {
			m.shortcutRecorderButton(c, "settings-rec-quick", "quick", quickSpec)
		})
		settingRow(c, theme.Tr("搜索笔记", "Search Notes"), func() {
			keycapText(c, internalui.FormatShortcutDisplay("cmd-k"))
		})
		settingRow(c, theme.Tr("当前笔记查找", "Find in Note"), func() {
			keycapText(c, internalui.FormatShortcutDisplay("cmd-f"))
		})
		settingRow(c, theme.Tr("新建笔记", "New Note"), func() {
			keycapText(c, internalui.FormatShortcutDisplay("cmd-n"))
		})
		settingRow(c, theme.Tr("AI 助手面板", "AI Assistant Panel"), func() {
			keycapText(c, internalui.FormatShortcutDisplay("cmd-j"))
		})
		settingRow(c, theme.Tr("性能与占用", "Performance & Usage"), func() {
			keycapText(c, internalui.FormatShortcutDisplay("cmd-shift-m"))
		})
	})
}

func (m *Manager) shortcutRecorderButton(c *ui.Context, id, kind, spec string) {
	pal := theme.Ocean()
	recording := m.recordingShortcut == kind
	label := internalui.FormatShortcutDisplay(spec)
	if recording {
		label = theme.Tr("按下快捷键…", "Press shortcut…")
	}
	borderCol := pal.BtnBorder
	bg := pal.Shell
	if recording {
		borderCol = pal.Accent
		bg = pal.Selected
	}
	btn := ui.ButtonBase(c.Key(id)).Height(32).MinWidth(130).PaddingX(12).Center().
		Radius(theme.RadiusControl).Background(bg).Border(1, borderCol).
		Cursor(ui.CursorPointer).Label(label)
	btn.Children(func() {
		ui.Text(c, label).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
	})
	internalui.Probe(btn, id)
	if btn.Clicked() {
		if recording {
			m.recordingShortcut = ""
		} else {
			m.recordingShortcut = kind
			m.settingsFocusHandle.Focus(c)
		}
	}
}

func (m *Manager) applyRecordedShortcut(kind, spec string) {
	m.recordingShortcut = ""
	m.mutateState(func(s *model.AppState) {
		if kind == "search" {
			s.ShortcutSearch = spec
		} else if kind == "quick" {
			s.ShortcutQuick = spec
		}
	})
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
	case ui.KeyComma:
		return "comma"
	case ui.KeyPeriod:
		return "period"
	case ui.KeySlash:
		return "slash"
	case ui.KeyMinus:
		return "minus"
	case ui.KeyEqual:
		return "equal"
	case ui.KeyBracketLeft:
		return "bracketleft"
	case ui.KeyBracketRight:
		return "bracketright"
	default:
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
	pal := theme.Ocean()
	ui.Column(c).FillWidth().Padding(16).Gap(14).Radius(theme.RadiusCard).
		Background(pal.Card).Border(1, pal.Hairline).Children(children)
}

func settingRow(c *ui.Context, label string, control func()) {
	pal := theme.Ocean()
	ui.Row(c).FillWidth().MinHeight(34).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
		ui.Text(c, label).FontSize(14).TextColor(pal.Text)
		control()
	})
}

func keycapText(c *ui.Context, label string) {
	pal := theme.Ocean()
	ui.Box(c).Height(28).PaddingX(10).Center().Radius(theme.RadiusControl).
		Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
		ui.Text(c, label).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
	})
}
