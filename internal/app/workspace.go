package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"block/internal/ai"
	"block/internal/editor"
	"block/internal/export"
	"block/internal/files"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/modals"
	"block/internal/model"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Workspace coordinates the entire Block desktop application: state persistence,
// notebooks & file tree, multi-tab Block Editor & non-Markdown file surfaces,
// full-window 12-theme Export stage, full-window Page History, AI Assistant panel,
// Settings & Modal dialogs, Quick Note floating window, shortcuts, and file watching.
type Workspace struct {
	mu sync.RWMutex

	dataDir string
	win     *mygo.Window

	state          model.AppState
	sidebarWidth   float32
	sidebarPinned  bool
	lastViewportW  float32
	zoomPct        int
	entries        []model.FileEntry
	notice         string
	virtualPreview map[string]bool // createNote preview tabs not yet written to disk

	codec    *markdown.Codec
	sessions map[string]*editor.Session
	surfaces map[string]*TabSurface
	nav      *NavHistory
	closed   *ClosedTabs
	tabOps   TabStripOps

	tabMenu   *TabMenuState
	sidebarUI *SidebarUIState

	exportCtrl   *export.Controller
	historyView  *PageHistoryView
	historyToast *HistoryToast

	recoveryPath    string
	recoveryContent string

	ai        *ai.Controller
	modals    *modals.Manager
	quick     *modals.QuickNoteController
	searchWin *modals.SearchWindowController

	// OnAppMenuOrHotkeysChanged is notified whenever Settings updates shortcuts or language
	// so main.go can live-refresh the native application menu bar and OS global hotkeys.
	OnAppMenuOrHotkeysChanged func(st model.AppState)
	// OnRevealMainWindow restores, shows, and focuses the main workspace window.
	OnRevealMainWindow func()

	watchCancel         context.CancelFunc
	persistWindowBounds bool
	mainBoundsTimer     *time.Timer
	autoSaveTimer       *time.Timer
	sidebarWidthTimer   *time.Timer
	pendingSidebarWidth float32
	hasPendingSidebarW  bool
	lastSnapAt          map[string]time.Time
}

// NewWorkspace constructs and hydrates a Workspace backed by dataDir (or platform default when empty).
func NewWorkspace(dataDir string) *Workspace {
	if strings.TrimSpace(dataDir) != "" {
		files.SetDataDirOverride(dataDir)
	}
	actualDir := files.AppDataDir()
	st := files.LoadState()
	if st.MainWindowBounds == nil {
		if legacyBounds := loadLegacyMyGoMainWindowBounds(actualDir); legacyBounds != nil {
			st.MainWindowBounds = legacyBounds
		}
	}
	applyThemeFromState(st)

	codec := markdown.NewCodec()
	w := &Workspace{
		dataDir:             actualDir,
		state:               st,
		sidebarWidth:        SidebarDefaultW,
		zoomPct:             zoomPctFromState(st.EditorZoom),
		persistWindowBounds: true,
		virtualPreview:      make(map[string]bool),
		codec:               codec,
		sessions:            make(map[string]*editor.Session),
		surfaces:            make(map[string]*TabSurface),
		nav:                 NewNavHistory(),
		closed:              NewClosedTabs(),
		tabMenu:             NewTabMenuState(),
		sidebarUI:           NewSidebarUIState(),
		historyView:         &PageHistoryView{Mode: "diff"},
		lastSnapAt:          make(map[string]time.Time),
	}
	if st.SidebarWidth != nil && *st.SidebarWidth > 0 {
		w.sidebarWidth = ClampSidebarWidth(*st.SidebarWidth)
	}
	if recPath, recContent, ok := files.LatestRecoveryDraft(); ok {
		w.recoveryPath = recPath
		w.recoveryContent = recContent
	}

	w.exportCtrl = export.NewController(actualDir, w.RequestRedraw)
	w.ai = ai.NewController(actualDir, w, codec)
	w.modals = modals.NewManager(actualDir, w)
	w.quick = modals.NewQuickNoteController(actualDir, w, codec)
	w.searchWin = modals.NewSearchWindowController(w)

	w.wireModalCallbacks()
	w.wireQuickNoteCallbacks()
	w.wireSearchWindowCallbacks()
	w.hydrateInitialWorkspace()
	return w
}

func applyThemeFromState(st model.AppState) {
	theme.SetLanguage(st.Language)
	if mygo.App.IsReady() {
		theme.SetThemeMode(st.Theme, mygo.Theme.IsDark())
	} else {
		theme.SetThemeMode(st.Theme, st.Theme == model.ThemeDark)
	}
	editorFont := st.EditorFont
	if editorFont == "" && st.FontFamily == model.FontSerif {
		editorFont = "system-serif"
	}
	theme.SetTypography(editorFont, st.CodeFont, st.LineHeight)
	theme.SetZoom(float32(zoomPctFromState(st.EditorZoom)) / 100.0)
	theme.SetFolderRules(st.FolderRules)
	theme.SetIgnoredSearchFolders(st.IgnoredSearchFolders)
}

func (w *Workspace) newEditorSession(cleanPath string, doc *model.Doc) *editor.Session {
	sess := editor.NewSession(cleanPath, doc, w.codec, w)
	sess.SetDataDir(w.dataDir)
	sess.SetInlineAIResponder(w.inlineAIRespond)
	sess.SetInlineAIStreamResponder(w.inlineAIStreamRespond)
	sess.SetAnnotationAIResponder(w.annotationAIRespond)
	return sess
}

func stripOuterMarkdownFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	firstNL := strings.IndexByte(trimmed, '\n')
	if firstNL < 0 {
		return trimmed
	}
	rest := strings.TrimSpace(trimmed[firstNL+1:])
	if strings.HasSuffix(rest, "```") {
		return strings.TrimSpace(strings.TrimSuffix(rest, "```"))
	}
	return trimmed
}

func (w *Workspace) inlineAIStreamRespond(ctx context.Context, ctxText, orig, draft, instruction string, onDelta func(string)) (string, error) {
	inst := strings.TrimSpace(instruction)
	src := strings.TrimSpace(orig)
	if src == "" {
		src = strings.TrimSpace(draft)
	}
	if w.ai != nil && w.ai.IsLiveProviderReady() {
		sysPrompt := "You are Block's inline writing assistant. Output ONLY the requested Markdown text without preamble, commentary, or outer triple-backtick fences."
		var userPrompt string
		if strings.TrimSpace(orig) == "" {
			userPrompt = fmt.Sprintf("Context before cursor:\n%s\n\nDraft:\n%s\n\nInstruction:\n%s", ctxText, draft, inst)
		} else {
			userPrompt = fmt.Sprintf("Surrounding context:\n%s\n\nSelected text to transform:\n%s\n\nInstruction:\n%s", ctxText, src, inst)
		}
		out, err := w.ai.StreamPrompt(ctx, sysPrompt, userPrompt, onDelta)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(out) != "" {
			return stripOuterMarkdownFence(out), nil
		}
	}
	out := w.inlineAIOfflineFallback(ctxText, orig, draft, instruction)
	if onDelta != nil {
		onDelta(out)
	}
	return out, nil
}

func (w *Workspace) inlineAIRespond(ctxText, orig, draft, instruction string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := w.inlineAIStreamRespond(ctx, ctxText, orig, draft, instruction, nil)
	if err == nil && strings.TrimSpace(out) != "" {
		return out
	}
	return w.inlineAIOfflineFallback(ctxText, orig, draft, instruction)
}

func (w *Workspace) inlineAIOfflineFallback(ctxText, orig, draft, instruction string) string {
	inst := strings.TrimSpace(instruction)
	instLower := strings.ToLower(inst)
	src := strings.TrimSpace(orig)
	if src == "" {
		src = strings.TrimSpace(draft)
	}

	// Empty-block generation modes (续写 / 列大纲 / 待办清单 / 头脑风暴 / 自定义)
	if strings.TrimSpace(orig) == "" {
		topic := strings.TrimSpace(ctxText)
		if idx := strings.LastIndex(topic, "\n"); idx >= 0 {
			lastLine := strings.TrimSpace(topic[idx+1:])
			if lastLine != "" {
				topic = lastLine
			}
		}
		topic = strings.TrimLeft(topic, "#>-* ")
		if topic == "" {
			topic = theme.Tr("当前主题", "Current Topic")
		}
		switch {
		case strings.Contains(inst, "大纲") || strings.Contains(instLower, "outline"):
			return fmt.Sprintf("### %s 核心脉络\n\n- 背景与目标界定\n- 关键执行步骤\n- 风险与验收标准", topic)
		case strings.Contains(inst, "待办") || strings.Contains(instLower, "to-do") || strings.Contains(instLower, "todo") || strings.Contains(inst, "- [ ]"):
			return fmt.Sprintf("- [ ] 梳理 %s 的核心需求\n- [ ] 完成方案设计与实现\n- [ ] 执行端到端验证与复盘", topic)
		case strings.Contains(inst, "风暴") || strings.Contains(instLower, "brainstorm") || strings.Contains(inst, "发散"):
			return fmt.Sprintf("- 从用户高频痛点切入优化 %s\n- 简化交互层级，保持单屏直达\n- 引入自动化回归保障稳定性\n- 沉淀可复用的模块化组件", topic)
		default:
			return fmt.Sprintf("围绕「%s」，进一步明确核心边界与落地路径，让整体结构紧凑清晰、易于推进。", topic)
		}
	}

	// Selection / block edit modes (润色 / 精简 / 通俗一点 / 翻译 / 自定义)
	switch {
	case strings.Contains(inst, "翻译") || strings.Contains(instLower, "translate"):
		hasCJK := false
		for _, r := range src {
			if r >= 0x4E00 && r <= 0x9FFF {
				hasCJK = true
				break
			}
		}
		if hasCJK {
			return fmt.Sprintf("Translated: %s", src)
		}
		return fmt.Sprintf("译文：%s", src)
	case strings.Contains(inst, "精简") || strings.Contains(instLower, "simplify"):
		runes := []rune(src)
		if len(runes) > 24 {
			return string(runes[:24])
		}
		return strings.TrimSuffix(strings.TrimSuffix(src, "。"), ".")
	case strings.Contains(inst, "通俗") || strings.Contains(instLower, "plain"):
		return fmt.Sprintf("通俗来说：%s", src)
	default:
		return fmt.Sprintf("%s（已润色）", strings.TrimSuffix(strings.TrimSuffix(src, "。"), "."))
	}
}

func (w *Workspace) annotationAIRespond(docMarkdown string, pending []*model.Annotation) map[string]string {
	out := make(map[string]string, len(pending))
	if len(pending) == 0 {
		return out
	}

	if w.ai != nil && w.ai.IsLiveProviderReady() {
		type reqItem struct {
			ID         string   `json:"id"`
			AnchorText string   `json:"anchor_text"`
			Note       string   `json:"note"`
			Reactions  []string `json:"reactions,omitempty"`
		}
		items := make([]reqItem, 0, len(pending))
		for _, a := range pending {
			if a == nil {
				continue
			}
			items = append(items, reqItem{
				ID:         a.ID,
				AnchorText: a.QuotedText,
				Note:       a.Note,
				Reactions:  a.Reactions,
			})
		}
		payload, _ := json.MarshalIndent(items, "", "  ")
		sysPrompt := `You rewrite annotated spans in a Markdown document. Return ONLY valid JSON as an array of objects: [{"id":"...","replacement":"..."}]. Do not include markdown fences or extra prose.`
		userPrompt := fmt.Sprintf("Document Markdown:\n%s\n\nAnnotations to rewrite:\n%s", docMarkdown, string(payload))
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		raw, err := w.ai.CompletePrompt(ctx, sysPrompt, userPrompt)
		cancel()
		if err == nil && strings.TrimSpace(raw) != "" {
			clean := stripOuterMarkdownFence(raw)
			type respItem struct {
				ID          string `json:"id"`
				Replacement string `json:"replacement"`
			}
			var parsed []respItem
			if json.Unmarshal([]byte(clean), &parsed) == nil {
				for _, it := range parsed {
					if strings.TrimSpace(it.ID) != "" && strings.TrimSpace(it.Replacement) != "" {
						out[it.ID] = it.Replacement
					}
				}
			} else {
				var asMap map[string]string
				if json.Unmarshal([]byte(clean), &asMap) == nil {
					for k, v := range asMap {
						if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
							out[k] = v
						}
					}
				}
			}
		}
	}

	for _, a := range pending {
		if a == nil || strings.TrimSpace(out[a.ID]) != "" {
			continue
		}
		anchor := strings.TrimSpace(a.QuotedText)
		note := strings.TrimSpace(a.Note)
		noteLower := strings.ToLower(note)
		switch {
		case strings.Contains(note, "翻译") || strings.Contains(noteLower, "translate"):
			out[a.ID] = fmt.Sprintf("Translated: %s", anchor)
		case strings.Contains(note, "加粗") || strings.Contains(noteLower, "bold"):
			out[a.ID] = fmt.Sprintf("**%s**", anchor)
		case strings.Contains(note, "精简") || strings.Contains(noteLower, "concise") || strings.Contains(noteLower, "shorten"):
			runes := []rune(anchor)
			if len(runes) > 16 {
				out[a.ID] = string(runes[:16])
			} else {
				out[a.ID] = strings.TrimSuffix(strings.TrimSuffix(anchor, "。"), ".")
			}
		default:
			out[a.ID] = fmt.Sprintf("%s（%s）", anchor, note)
		}
	}
	return out
}

func (w *Workspace) wireQuickNoteCallbacks() {
	if w.quick == nil {
		return
	}
	w.quick.SetInlineAIResponder(w.inlineAIRespond)
	w.quick.OnSavedNote = func(path string) {
		w.RescanNotebook()
		_ = w.OpenNote(path, 0)
	}
	w.quick.OnSummonSearch = func() {
		if w.OnRevealMainWindow != nil {
			w.OnRevealMainWindow()
		} else if w.win != nil {
			w.win.Restore()
			w.win.Show()
			w.win.Focus()
		}
		if w.searchWin != nil {
			w.searchWin.Hide()
		}
		w.modals.OpenSearch(false)
		w.RequestRedraw()
	}
	w.quick.GetQuickZoom = func() float32 {
		w.mu.RLock()
		defer w.mu.RUnlock()
		if w.state.QuickEditorZoom != nil && *w.state.QuickEditorZoom > 0 {
			return *w.state.QuickEditorZoom
		}
		return 1.0
	}
	w.quick.GetQuickShortcut = func() string {
		w.mu.RLock()
		st := w.state
		w.mu.RUnlock()
		return modals.EffectiveShortcut(st, "quick_note")
	}
	w.quick.OnQuickZoomChanged = func(zoom float32) {
		z := zoom
		next, err := files.UpdateState(func(st *model.AppState) {
			st.QuickEditorZoom = &z
		})
		if err == nil {
			w.mu.Lock()
			w.state = next
			w.mu.Unlock()
		}
	}
	w.quick.GetQuickBounds = func() *model.SavedWindowBounds {
		w.mu.RLock()
		defer w.mu.RUnlock()
		if w.state.QuickWindowBounds == nil {
			return nil
		}
		cp := *w.state.QuickWindowBounds
		return &cp
	}
	w.quick.OnQuickBoundsChanged = func(b model.SavedWindowBounds) {
		cp := b
		next, err := files.UpdateState(func(st *model.AppState) {
			st.QuickWindowBounds = &cp
		})
		if err == nil {
			w.mu.Lock()
			w.state = next
			w.mu.Unlock()
		}
	}
}

func (w *Workspace) wireSearchWindowCallbacks() {
	if w.searchWin == nil {
		return
	}
	w.searchWin.GetState = func() model.AppState {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.state
	}
	w.searchWin.SearchFn = func(query string, allNotebooks bool) ([]model.SearchHit, error) {
		return w.SearchWorkspace(query, allNotebooks)
	}
	w.searchWin.OnOpenHit = func(hit model.SearchHit) {
		if w.OnRevealMainWindow != nil {
			w.OnRevealMainWindow()
		} else if w.win != nil {
			w.win.Restore()
			w.win.Show()
			w.win.Focus()
		}
		_ = w.OpenFromSearch(hit)
	}
}

func (w *Workspace) wireModalCallbacks() {
	w.modals.GetState = func() model.AppState {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.state
	}
	w.modals.OnStateChanged = func(mut func(*model.AppState)) {
		if mut == nil {
			return
		}
		next, err := files.UpdateState(mut)
		if err != nil {
			files.LogWarnf("UpdateState failed: %v", err)
			w.mu.Lock()
			mut(&w.state)
			next = w.state
			w.mu.Unlock()
		} else {
			w.mu.Lock()
			w.state = next
			w.mu.Unlock()
		}
		applyThemeFromState(next)
		if mygo.App.IsReady() {
			switch next.Theme {
			case model.ThemeDark:
				mygo.Theme.SetSource(mygo.ThemeDark)
			case model.ThemeLight:
				mygo.Theme.SetSource(mygo.ThemeLight)
			default:
				mygo.Theme.SetSource(mygo.ThemeSystem)
			}
		}
		w.RescanNotebook()
		if w.OnAppMenuOrHotkeysChanged != nil {
			w.OnAppMenuOrHotkeysChanged(next)
		}
		if w.searchWin != nil {
			w.searchWin.Invalidate()
		}
		if w.quick != nil {
			w.quick.RefreshStyle()
		}
		w.RequestRedraw()
	}
	w.modals.RenderAISettings = func(c *ui.Context) {
		w.ai.RenderSettingsTab(c)
	}
	w.modals.SidebarWidth = &w.sidebarWidth
	w.modals.OnSidebarWidthChanged = func(nw float32) {
		clamped := ClampSidebarWidth(nw)
		_, _ = files.UpdateState(func(st *model.AppState) {
			st.SidebarWidth = &clamped
		})
	}
	w.modals.OnForgetNotebook = w.ForgetNotebook
	w.modals.SearchFn = func(query string, allNotebooks bool) ([]model.SearchHit, error) {
		return w.SearchWorkspace(query, allNotebooks)
	}
	w.modals.SetOpenFromSearchFn(func(hit model.SearchHit) {
		_ = w.OpenFromSearch(hit)
	})
	w.modals.ListHistoryFn = func(notePath string) ([]model.HistorySnapshot, error) {
		return files.ListHistorySnapshots(notePath)
	}
	w.modals.ReadHistoryFn = func(notePath, snapID string) (string, error) {
		return files.ReadHistorySnapshot(notePath, snapID)
	}
	w.modals.DiffLinesFn = func(oldText, newText string) []model.DiffLine {
		return files.DiffLines(oldText, newText)
	}
	w.modals.ExportFn = func(format model.ExportFormat, outPath string) error {
		active := w.ActiveNotePath()
		if active == "" {
			return errors.New(theme.Tr("当前没有打开的笔记", "No active note to export"))
		}
		sess := w.SessionFor(active)
		if sess == nil {
			return errors.New(theme.Tr("无法读取当前笔记", "Could not read active note"))
		}
		var prefs model.ExportPreferences
		w.mu.RLock()
		if w.state.ExportPreferences != nil {
			prefs = *w.state.ExportPreferences
		}
		w.mu.RUnlock()
		prefs.Format = format
		return markdown.ExportToFile(sess.Doc(), active, outPath, format, prefs)
	}
}

func (w *Workspace) scanOptionsLocked() files.ScanOptions {
	hideNonMD := false
	if w.state.HideNonMarkdownFiles != nil {
		hideNonMD = *w.state.HideNonMarkdownFiles
	}
	hideAssets := true
	if w.state.HideAssetsFolder != nil {
		hideAssets = *w.state.HideAssetsFolder
	}
	assetsFolder := w.state.AssetsFolder
	if strings.TrimSpace(assetsFolder) == "" {
		assetsFolder = "assets"
	}
	return files.ScanOptions{
		HideNonMarkdown: hideNonMD,
		HideAssets:      hideAssets,
		AssetsFolder:    assetsFolder,
		FileOrder:       w.state.FileOrder,
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (w *Workspace) hydrateInitialWorkspace() {
	if w.state.Folder != "" {
		if info, err := os.Stat(w.state.Folder); err == nil && info.IsDir() {
			w.RescanNotebook()
		}
	}
	validTabs := make([]string, 0, len(w.state.OpenTabs))
	seen := make(map[string]bool, len(w.state.OpenTabs))
	for _, p := range w.state.OpenTabs {
		if strings.TrimSpace(p) == "" {
			continue
		}
		clean := filepath.Clean(p)
		if seen[clean] {
			continue
		}
		if info, err := os.Stat(clean); err == nil && info.Mode().IsRegular() {
			seen[clean] = true
			validTabs = append(validTabs, clean)
		}
	}
	active := filepath.Clean(w.state.ActiveTab)
	if w.state.ActiveTab == "" {
		active = ""
	}
	foundActive := false
	for _, t := range validTabs {
		if t == active {
			foundActive = true
			break
		}
	}
	if !foundActive {
		if len(validTabs) > 0 {
			active = validTabs[0]
		} else {
			active = ""
		}
	}
	changed := active != w.state.ActiveTab || !stringSlicesEqual(validTabs, w.state.OpenTabs)
	w.state.OpenTabs = validTabs
	w.state.ActiveTab = active
	if changed {
		w.persistTabs(validTabs, active)
	}
	if active != "" {
		if w.state.Folder != "" && strings.HasPrefix(active, w.state.Folder+string(filepath.Separator)) {
			dir := filepath.Dir(active)
			for dir != "" && dir != w.state.Folder && strings.HasPrefix(dir, w.state.Folder) {
				w.sidebarUI.ExpandedDirs[dir] = true
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}
		_ = w.ensureTabLoaded(active)
		w.nav.Push(active)
	}
}

// AttachWindow binds the main *mygo.Window and starts folder file watching.
func (w *Workspace) AttachWindow(win *mygo.Window) {
	w.mu.Lock()
	w.win = win
	folder := w.state.Folder
	w.mu.Unlock()

	if win != nil {
		win.OnMove(func() { w.scheduleSaveMainBounds() })
		win.OnResize(func() { w.scheduleSaveMainBounds() })
		win.OnMaximize(func() { w.scheduleSaveMainBounds() })
		win.OnUnmaximize(func() { w.scheduleSaveMainBounds() })
		win.OnEnterFullScreen(func() { w.scheduleSaveMainBounds() })
		win.OnLeaveFullScreen(func() { w.scheduleSaveMainBounds() })
		win.OnHide(func() { w.FlushWindowBounds() })
		win.OnFileDrop(func(e *mygo.FileDropEvent) {
			if e != nil && len(e.Paths) > 0 {
				if sess := w.ActiveSession(); sess != nil {
					if sess.ImportFiles(e.Paths, 0, false) > 0 {
						w.RequestRedraw()
					}
				}
			}
		})
	}

	if folder != "" {
		w.startWatching(folder)
	}
}

// Window returns the bound *mygo.Window (may be nil in unit tests).
func (w *Workspace) Window() *mygo.Window {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.win
}

// AI returns the workspace's AI Controller.
func (w *Workspace) AI() *ai.Controller { return w.ai }

// Export returns the workspace's full-window 12-theme Export Controller.
func (w *Workspace) Export() *export.Controller { return w.exportCtrl }

// Modals returns the workspace's Modals & Settings Manager.
func (w *Workspace) Modals() *modals.Manager { return w.modals }

// QuickNote returns the workspace's Quick Note floating window controller.
func (w *Workspace) QuickNote() *modals.QuickNoteController { return w.quick }

// SearchWindow returns the workspace's standalone floating search window controller.
func (w *Workspace) SearchWindow() *modals.SearchWindowController { return w.searchWin }

// Codec returns the workspace's MarkdownCodec.
func (w *Workspace) Codec() *markdown.Codec { return w.codec }

// PendingRecoveryDraft returns any unsaved crash-recovery draft found on startup.
func (w *Workspace) PendingRecoveryDraft() (notePath string, content string, ok bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.recoveryPath == "" {
		return "", "", false
	}
	return w.recoveryPath, w.recoveryContent, true
}

// ApplyPendingRecoveryDraft restores the startup crash-recovery draft into its note and clears the draft file.
func (w *Workspace) ApplyPendingRecoveryDraft() (string, error) {
	w.mu.Lock()
	p, c := w.recoveryPath, w.recoveryContent
	w.recoveryPath = ""
	w.recoveryContent = ""
	w.mu.Unlock()
	if p == "" {
		return "", nil
	}
	if err := w.WriteNote(p, c); err != nil {
		return p, err
	}
	_ = w.OpenNote(p, 0)
	return p, nil
}

// DismissPendingRecoveryDraft discards the startup crash-recovery draft.
func (w *Workspace) DismissPendingRecoveryDraft() {
	w.mu.Lock()
	p := w.recoveryPath
	w.recoveryPath = ""
	w.recoveryContent = ""
	w.mu.Unlock()
	if p != "" {
		files.ClearRecovery(p)
	}
	w.RequestRedraw()
}

// State returns a snapshot of the persisted AppState.
func (w *Workspace) State() model.AppState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state
}

// Tabs returns the open tab paths in order.
func (w *Workspace) Tabs() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]string(nil), w.state.OpenTabs...)
}

// Entries returns the current notebook FileEntry tree.
func (w *Workspace) Entries() []model.FileEntry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]model.FileEntry(nil), w.entries...)
}

// SurfaceFor returns the non-Markdown *TabSurface for path if path is a non-Markdown open file.
func (w *Workspace) SurfaceFor(path string) *TabSurface {
	if path == "" {
		return nil
	}
	clean := filepath.Clean(path)
	w.mu.RLock()
	surf := w.surfaces[clean]
	_, hasSess := w.sessions[clean]
	w.mu.RUnlock()
	if surf != nil || hasSess {
		return surf
	}
	_ = w.ensureTabLoaded(clean)
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.surfaces[clean]
}

// SessionFor returns the active *editor.Session for path (nil for non-Markdown surfaces).
func (w *Workspace) SessionFor(path string) *editor.Session {
	if path == "" {
		return nil
	}
	clean := filepath.Clean(path)
	w.mu.RLock()
	sess := w.sessions[clean]
	_, hasSurf := w.surfaces[clean]
	w.mu.RUnlock()
	if sess != nil || hasSurf {
		return sess
	}
	_ = w.ensureTabLoaded(clean)
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sessions[clean]
}

// ActiveSession returns the *editor.Session of the currently active tab, or nil.
func (w *Workspace) ActiveSession() *editor.Session {
	return w.SessionFor(w.ActiveNotePath())
}

// ActiveSurface returns the non-Markdown *TabSurface of the currently active tab, or nil.
func (w *Workspace) ActiveSurface() *TabSurface {
	return w.SurfaceFor(w.ActiveNotePath())
}

func (w *Workspace) ensureTabLoaded(path string) error {
	clean := filepath.Clean(path)
	w.mu.RLock()
	if _, ok := w.sessions[clean]; ok {
		w.mu.RUnlock()
		return nil
	}
	if _, ok := w.surfaces[clean]; ok {
		w.mu.RUnlock()
		return nil
	}
	w.mu.RUnlock()

	cf, err := files.ClassifyFile(clean)
	if err != nil {
		return err
	}
	if cf.Kind == model.OpenKindMarkdown {
		doc := w.codec.Parse(cf.Text)
		sess := w.newEditorSession(clean, doc)
		w.mu.Lock()
		delete(w.surfaces, clean)
		w.sessions[clean] = sess
		w.mu.Unlock()
		return nil
	}

	surf := NewTabSurface(clean, cf)
	w.mu.Lock()
	delete(w.sessions, clean)
	w.surfaces[clean] = surf
	w.mu.Unlock()
	return nil
}

// OpenExportStage opens the full-window 12-theme A-Export stage for the active Markdown note.
func (w *Workspace) OpenExportStage() bool {
	active := w.ActiveNotePath()
	if active == "" {
		return false
	}
	sess := w.SessionFor(active)
	if sess == nil {
		return false
	}
	w.FlushDirtySessions()
	if !sess.Dirty() {
		if diskText, _, err := files.ReadNote(active); err == nil && diskText != sess.Markdown() {
			sess.SetDoc(w.codec.Parse(diskText))
			sess.MarkSaved()
		}
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.modals.CloseSettings()
	w.historyView.Close()

	title := noteDisplayTitle(active, false)
	w.exportCtrl.Open(title, active, sess.Doc(), sess.SelectedDoc())
	w.RequestRedraw()
	return true
}

// IsPageHistoryOpen reports whether the full-window Page History stage is currently open.
func (w *Workspace) IsPageHistoryOpen() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.historyView != nil && w.historyView.Open
}

// OpenPageHistory opens the full-window Page History stage (`app_history_view.rs` 1:1) for the active Markdown note.
func (w *Workspace) OpenPageHistory() bool {
	active := w.ActiveNotePath()
	if active == "" {
		return false
	}
	sess := w.SessionFor(active)
	if sess == nil {
		return false
	}
	w.FlushDirtySessions()
	if !sess.Dirty() {
		if diskText, _, err := files.ReadNote(active); err == nil && diskText != sess.Markdown() {
			sess.SetDoc(w.codec.Parse(diskText))
			sess.MarkSaved()
		}
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.modals.CloseSettings()
	w.exportCtrl.Close()

	curText := sess.Markdown()
	snaps, err := files.ListHistorySnapshots(active)
	if err == nil && len(snaps) == 0 {
		// Ensure at least one baseline snapshot exists when opening history on an existing note.
		_, _ = files.RecordHistorySnapshot(active, curText, theme.Tr("当前版本", "Current version"))
		snaps, _ = files.ListHistorySnapshots(active)
	}

	w.mu.Lock()
	w.historyView.OpenForNote(
		active,
		curText,
		snaps,
		func(id string) (string, error) {
			return files.ReadHistorySnapshot(active, id)
		},
		files.DiffLines,
	)
	w.mu.Unlock()
	w.RequestRedraw()
	return true
}

// ClosePageHistory exits the full-window Page History stage.
func (w *Workspace) ClosePageHistory() {
	w.mu.Lock()
	if w.historyView != nil {
		w.historyView.Close()
	}
	w.mu.Unlock()
	w.RequestRedraw()
}

// --- model.WorkspaceHost implementation ---

func (w *Workspace) ActiveNotebook() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state.Folder
}

func (w *Workspace) Notebooks() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]string(nil), w.state.Notebooks...)
}

func (w *Workspace) ActiveNotePath() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state.ActiveTab
}

func (w *Workspace) ListNotebookFiles(root string) ([]model.FileEntry, error) {
	target := strings.TrimSpace(root)
	w.mu.RLock()
	if target == "" {
		target = w.state.Folder
	}
	opts := w.scanOptionsLocked()
	w.mu.RUnlock()
	if target == "" {
		return nil, nil
	}
	return files.ScanNotebook(target, opts)
}

func (w *Workspace) SearchWorkspace(query string, allNotebooks bool) ([]model.SearchHit, error) {
	w.mu.RLock()
	var roots []string
	if allNotebooks {
		if w.state.Folder != "" {
			roots = append(roots, w.state.Folder)
		}
		roots = append(roots, w.state.Notebooks...)
	} else if w.state.Folder != "" {
		roots = append(roots, w.state.Folder)
	}
	opts := w.scanOptionsLocked()
	w.mu.RUnlock()
	return files.SearchNotebooks(roots, query, opts)
}

func pathWithinRoot(absPath, root string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(absPath) == "" {
		return false
	}
	cleanP := filepath.Clean(absPath)
	cleanR := filepath.Clean(root)
	return cleanP == cleanR || strings.HasPrefix(cleanP, cleanR+string(filepath.Separator))
}

// OpenFromSearch opens a search hit, switching to the owning notebook if it belongs to another loaded folder.
func (w *Workspace) OpenFromSearch(hit model.SearchHit) error {
	if strings.TrimSpace(hit.Path) == "" {
		return errors.New("empty note path")
	}
	clean := filepath.Clean(hit.Path)
	w.modals.CloseSearch()
	if w.isBusy() {
		return nil
	}
	w.mu.RLock()
	curFolder := w.state.Folder
	notebooks := append([]string(nil), w.state.Notebooks...)
	w.mu.RUnlock()

	if !pathWithinRoot(clean, curFolder) {
		var bestNotebook string
		for _, nb := range notebooks {
			if pathWithinRoot(clean, nb) && len(filepath.Clean(nb)) > len(bestNotebook) {
				bestNotebook = filepath.Clean(nb)
			}
		}
		if bestNotebook != "" {
			_ = w.SetNotebook(bestNotebook)
		}
	}
	if err := w.OpenNote(clean, 0); err != nil {
		return err
	}
	if sess := w.SessionFor(clean); sess != nil {
		sess.JumpToSearchHit(hit)
		w.RequestRedraw()
	}
	return nil
}

func (w *Workspace) ReadNote(absPath string) (string, error) {
	clean := filepath.Clean(absPath)
	w.mu.RLock()
	sess := w.sessions[clean]
	surf := w.surfaces[clean]
	w.mu.RUnlock()
	if sess != nil {
		return sess.Markdown(), nil
	}
	if surf != nil && surf.IsEditableSource() {
		return surf.Source.Text, nil
	}
	content, _, err := files.ReadNote(clean)
	return content, err
}

func (w *Workspace) WriteNote(absPath string, content string) error {
	clean := filepath.Clean(absPath)
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	if !files.IsMarkdown(clean) {
		w.mu.RLock()
		existingSurf := w.surfaces[clean]
		w.mu.RUnlock()
		bom := existingSurf != nil && existingSurf.Source.BOM
		if _, err := files.WriteRawFileAtomic(clean, content, bom); err != nil {
			return err
		}
		if cf, err := files.ClassifyFile(clean); err == nil {
			w.mu.Lock()
			w.surfaces[clean] = NewTabSurface(clean, cf)
			w.mu.Unlock()
		}
		w.RescanNotebook()
		w.RequestRedraw()
		return nil
	}

	if _, err := files.WriteNoteAtomic(clean, content); err != nil {
		return err
	}
	files.ClearRecovery(clean)
	_, _ = files.RecordHistorySnapshot(clean, content, theme.Tr("保存", "Saved"))

	doc := w.codec.Parse(content)
	w.mu.Lock()
	delete(w.virtualPreview, clean)
	delete(w.surfaces, clean)
	if sess, ok := w.sessions[clean]; ok {
		sess.SetDoc(doc)
		sess.MarkSaved()
	} else {
		w.sessions[clean] = w.newEditorSession(clean, doc)
	}
	w.mu.Unlock()

	w.RescanNotebook()
	w.RequestRedraw()
	return nil
}

func (w *Workspace) OpenNote(absPath string, lineHint int) error {
	return w.openNoteInternal(absPath, true, true, lineHint)
}

func (w *Workspace) openNoteInternal(absPath string, forceNewTab bool, recordNav bool, lineHint int) error {
	if strings.TrimSpace(absPath) == "" {
		return errors.New("empty note path")
	}
	clean := filepath.Clean(absPath)
	if err := w.ensureTabLoaded(clean); err != nil {
		return err
	}

	w.modals.CloseSettings()
	w.exportCtrl.Close()
	w.historyView.Close()
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.sidebarUI.FocusDir = ""

	w.mu.Lock()
	w.sidebarPinned = false
	if w.historyToast != nil && filepath.Clean(w.historyToast.NotePath) != clean {
		w.historyToast = nil
	}
	if prevSess := w.sessions[w.state.ActiveTab]; prevSess != nil && w.state.ActiveTab != clean {
		prevSess.ClearTransientOverlays()
	}
	if w.state.Folder != "" && strings.HasPrefix(clean, w.state.Folder+string(filepath.Separator)) {
		dir := filepath.Dir(clean)
		for dir != "" && dir != w.state.Folder && strings.HasPrefix(dir, w.state.Folder) {
			w.sidebarUI.ExpandedDirs[dir] = true
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	prevTabs := append([]string(nil), w.state.OpenTabs...)
	prevActive := w.state.ActiveTab
	if !forceNewTab && !slices.Contains(w.state.OpenTabs, clean) {
		for i, path := range w.state.OpenTabs {
			if path == prevActive {
				w.state.OpenTabs[i] = clean
				break
			}
		}
	}
	w.state.OpenTabs, w.state.ActiveTab = w.tabOps.OpenOrFocus(w.state.OpenTabs, prevActive, clean)

	sess := w.sessions[clean]
	tabs := append([]string(nil), w.state.OpenTabs...)
	active := w.state.ActiveTab
	tabsChanged := active != prevActive || len(tabs) != len(prevTabs)
	if !tabsChanged {
		for i := range tabs {
			if tabs[i] != prevTabs[i] {
				tabsChanged = true
				break
			}
		}
	}
	w.mu.Unlock()

	if recordNav {
		w.nav.Push(clean)
	}
	if sess != nil && lineHint > 0 {
		sess.JumpToSearchHit(model.SearchHit{Line: lineHint, Column: -1})
	}
	if tabsChanged {
		w.persistTabs(tabs, active)
	}
	w.RequestRedraw()
	return nil
}

func (w *Workspace) resolvePlanTargetPath(plan *model.BatchPlan, page string) string {
	if filepath.IsAbs(page) {
		return filepath.Clean(page)
	}
	root := plan.Root
	if strings.TrimSpace(root) == "" {
		root = w.ActiveNotebook()
	}
	target := filepath.Join(root, page)
	if !files.IsMarkdown(target) {
		target += ".md"
	}
	return filepath.Clean(target)
}

func (w *Workspace) PreviewPlanInEditor(plan *model.BatchPlan) error {
	if plan == nil || len(plan.Ops) == 0 {
		return nil
	}
	firstTarget := w.resolvePlanTargetPath(plan, plan.Ops[0].Page)
	if _, err := os.Stat(firstTarget); errors.Is(err, os.ErrNotExist) {
		previewDoc := w.codec.Parse(plan.Ops[0].New)
		sess := w.newEditorSession(firstTarget, previewDoc)
		sess.SetActivePlan(plan)

		w.mu.Lock()
		w.virtualPreview[firstTarget] = true
		delete(w.surfaces, firstTarget)
		w.sessions[firstTarget] = sess
		w.state.OpenTabs, w.state.ActiveTab = w.tabOps.OpenOrFocus(w.state.OpenTabs, w.state.ActiveTab, firstTarget)
		w.mu.Unlock()
		w.RequestRedraw()
		return nil
	}
	if err := w.OpenNote(firstTarget, 0); err != nil {
		return err
	}
	if sess := w.SessionFor(firstTarget); sess != nil {
		sess.SetActivePlan(plan)
	}
	w.RequestRedraw()
	return nil
}

func (w *Workspace) ApplyBatchPlan(plan *model.BatchPlan) error {
	if plan == nil {
		return nil
	}
	if plan.Expected == nil {
		plan.Expected = make(map[string]string)
	}
	for _, op := range plan.Ops {
		target := w.resolvePlanTargetPath(plan, op.Page)
		prevContent, _, prevErr := files.ReadNote(target)
		if op.Kind == model.PlanOpCreateNote || prevErr != nil {
			if _, recorded := plan.Expected[target]; !recorded {
				plan.Expected[target] = "\x00__CREATED__"
			}
		} else {
			if _, recorded := plan.Expected[target]; !recorded {
				plan.Expected[target] = prevContent
			}
		}

		nextContent := op.New
		if prevErr == nil && op.Kind == model.PlanOpCreateNote {
			nextContent = prevContent
		} else if prevErr == nil && op.Old != "" && strings.Contains(prevContent, op.Old) {
			nextContent = strings.Replace(prevContent, op.Old, op.New, 1)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if _, err := files.WriteNoteAtomic(target, nextContent); err != nil {
			return err
		}
		_, _ = files.RecordHistorySnapshot(target, nextContent, "AI BatchPlan")

		doc := w.codec.Parse(nextContent)
		w.mu.Lock()
		delete(w.virtualPreview, target)
		delete(w.surfaces, target)
		if sess, ok := w.sessions[target]; ok {
			sess.SetActivePlan(nil)
			sess.SetDoc(doc)
			sess.MarkSaved()
		} else {
			w.sessions[target] = w.newEditorSession(target, doc)
		}
		w.state.OpenTabs, w.state.ActiveTab = w.tabOps.OpenOrFocus(w.state.OpenTabs, w.state.ActiveTab, target)
		tabs := append([]string(nil), w.state.OpenTabs...)
		active := w.state.ActiveTab
		w.mu.Unlock()
		w.persistTabs(tabs, active)
	}
	w.RescanNotebook()
	w.RequestRedraw()
	return nil
}

func (w *Workspace) RejectBatchPlan(planID string) error {
	w.mu.Lock()
	var virtualDrops []string
	for path, sess := range w.sessions {
		if ap := sess.ActivePlan(); ap != nil && (planID == "" || ap.ID == planID) {
			sess.SetActivePlan(nil)
			if w.virtualPreview[path] {
				virtualDrops = append(virtualDrops, path)
			}
		}
	}
	for _, drop := range virtualDrops {
		delete(w.virtualPreview, drop)
		delete(w.sessions, drop)
		w.state.OpenTabs, w.state.ActiveTab, _ = w.tabOps.CloseTab(w.state.OpenTabs, w.state.ActiveTab, drop)
	}
	w.mu.Unlock()
	w.RequestRedraw()
	return nil
}

func (w *Workspace) UndoLastPlan(plan *model.BatchPlan) error {
	if plan == nil {
		return nil
	}
	handled := make(map[string]bool)
	for _, op := range plan.Ops {
		target := w.resolvePlanTargetPath(plan, op.Page)
		handled[target] = true
		if op.Kind == model.PlanOpCreateNote || plan.Expected[target] == "\x00__CREATED__" {
			if _, err := os.Lstat(target); err == nil {
				_ = files.TrashItem(target)
			}
			w.removeClosedOrDeletedPath(target)
			continue
		}
		snap, hasSnap := plan.Expected[target]
		if !hasSnap {
			snap, hasSnap = plan.Expected[op.Page]
		}
		if hasSnap {
			if _, err := files.WriteNoteAtomic(target, snap); err != nil {
				return err
			}
			doc := w.codec.Parse(snap)
			w.mu.Lock()
			if sess, ok := w.sessions[target]; ok {
				sess.SetActivePlan(nil)
				sess.SetDoc(doc)
				sess.MarkSaved()
			}
			w.mu.Unlock()
		}
	}
	for key, snapshot := range plan.Expected {
		target := w.resolvePlanTargetPath(plan, key)
		if handled[target] {
			continue
		}
		if snapshot == "\x00__CREATED__" {
			if _, err := os.Lstat(target); err == nil {
				_ = files.TrashItem(target)
			}
			w.removeClosedOrDeletedPath(target)
			continue
		}
		if _, err := files.WriteNoteAtomic(target, snapshot); err != nil {
			return err
		}
		doc := w.codec.Parse(snapshot)
		w.mu.Lock()
		if sess, ok := w.sessions[target]; ok {
			sess.SetActivePlan(nil)
			sess.SetDoc(doc)
			sess.MarkSaved()
		}
		w.mu.Unlock()
	}
	w.RescanNotebook()
	w.RequestRedraw()
	return nil
}

func (w *Workspace) OpenSettings(tab string) {
	wasOpen := w.modals.IsSettingsOpen()
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.exportCtrl.Close()
	w.historyView.Close()
	if !wasOpen {
		w.mu.Lock()
		w.state.SidebarCollapsed = false
		w.sidebarPinned = false
		w.mu.Unlock()
	}
	w.modals.OpenSettings(tab)
	w.RequestRedraw()
}

func (w *Workspace) RequestRedraw() {
	w.mu.RLock()
	win := w.win
	w.mu.RUnlock()
	if win != nil {
		win.Invalidate()
	}
}

// --- Notebook, File, Reorder, and Tab Operations ---

// SetNotebook switches the active notebook folder (adding it to recent notebooks),
// scans its file tree, and persists state.json.
func (w *Workspace) SetNotebook(folder string) error {
	clean := filepath.Clean(folder)
	info, err := os.Stat(clean)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("notebook path is not a directory")
	}

	w.mu.RLock()
	prevFolder := ""
	if w.state.Folder != "" {
		prevFolder = filepath.Clean(w.state.Folder)
	}
	w.mu.RUnlock()
	switched := prevFolder != clean
	if switched {
		if err := w.flushDirtySessions(); err != nil {
			return err
		}
	}

	next, saveErr := files.UpdateState(func(st *model.AppState) {
		savedPrev := ""
		if st.Folder != "" {
			savedPrev = filepath.Clean(st.Folder)
		}
		if savedPrev != clean {
			st.OpenTabs = nil
			st.ActiveTab = ""
		}
		st.Folder = clean
		already := false
		for _, nb := range st.Notebooks {
			if filepath.Clean(nb) == clean {
				already = true
				break
			}
		}
		if !already {
			st.Notebooks = append([]string{clean}, st.Notebooks...)
		}
	})
	if saveErr != nil {
		return saveErr
	}
	w.mu.Lock()
	w.state = next
	if switched {
		w.sessions = make(map[string]*editor.Session)
		w.surfaces = make(map[string]*TabSurface)
		w.virtualPreview = make(map[string]bool)
		w.lastSnapAt = make(map[string]time.Time)
		w.closed = NewClosedTabs()
		w.nav = NewNavHistory()
		w.historyToast = nil
		if w.historyView != nil {
			w.historyView.Close()
		}
		w.tabMenu.Close()
		w.sidebarUI.ClosePopups()
		w.sidebarUI.FocusDir = ""
		w.sidebarUI.CancelRename()
		w.sidebarUI.ClearDrag()
	}
	w.mu.Unlock()

	w.RescanNotebook()
	if w.win != nil {
		w.startWatching(clean)
	}
	w.RequestRedraw()
	return nil
}

// ReorderNotebooks moves a notebook entry from fromIdx to toIdx and persists `state.Notebooks`.
func (w *Workspace) ReorderNotebooks(fromIdx, toIdx int) {
	next, err := files.UpdateState(func(st *model.AppState) {
		if fromIdx < 0 || fromIdx >= len(st.Notebooks) || toIdx < 0 || toIdx >= len(st.Notebooks) || fromIdx == toIdx {
			return
		}
		list := append([]string(nil), st.Notebooks...)
		item := list[fromIdx]
		list = append(list[:fromIdx], list[fromIdx+1:]...)
		list = append(list[:toIdx], append([]string{item}, list[toIdx:]...)...)
		st.Notebooks = list
	})
	if err == nil {
		w.mu.Lock()
		w.state = next
		w.mu.Unlock()
	}
	w.RequestRedraw()
}

// ReorderSibling reorders srcPath relative to targetPath inside parentDir and persists `state.FileOrder[parentDir]`.
func (w *Workspace) ReorderSibling(parentDir, srcPath, targetPath string, after bool) {
	cleanParent := filepath.Clean(parentDir)
	srcBase := filepath.Base(srcPath)
	dstBase := filepath.Base(targetPath)
	if srcBase == "" || dstBase == "" || srcBase == dstBase {
		return
	}

	w.mu.RLock()
	sibs := findSameKindSiblings(w.entries, srcPath)
	var allInDir []model.FileEntry
	if filepath.Clean(w.state.Folder) == cleanParent {
		allInDir = w.entries
	} else {
		var walkDir func(items []model.FileEntry) []model.FileEntry
		walkDir = func(items []model.FileEntry) []model.FileEntry {
			for _, it := range items {
				if it.IsDir {
					if filepath.Clean(it.Path) == cleanParent {
						return it.Children
					}
					if got := walkDir(it.Children); got != nil {
						return got
					}
				}
			}
			return nil
		}
		allInDir = walkDir(w.entries)
	}
	w.mu.RUnlock()

	if len(sibs) < 2 {
		return
	}

	// Build reordered same-kind basename list, preserving relative order of other-kind items.
	orderedKind := make([]string, 0, len(sibs))
	for _, s := range sibs {
		base := filepath.Base(s.Path)
		if base != srcBase {
			orderedKind = append(orderedKind, base)
		}
	}
	inserted := false
	nextKind := make([]string, 0, len(sibs))
	for _, b := range orderedKind {
		if b == dstBase {
			if after {
				nextKind = append(nextKind, b, srcBase)
			} else {
				nextKind = append(nextKind, srcBase, b)
			}
			inserted = true
		} else {
			nextKind = append(nextKind, b)
		}
	}
	if !inserted {
		nextKind = append(nextKind, srcBase)
	}

	srcIsDir := sibs[0].IsDir
	var dirs, regularFiles []string
	for _, it := range allInDir {
		b := filepath.Base(it.Path)
		if it.IsDir {
			dirs = append(dirs, b)
		} else {
			regularFiles = append(regularFiles, b)
		}
	}
	if srcIsDir {
		dirs = nextKind
	} else {
		regularFiles = nextKind
	}
	combined := append(dirs, regularFiles...)

	nextState, err := files.UpdateState(func(st *model.AppState) {
		if st.FileOrder == nil {
			st.FileOrder = make(map[string][]string)
		}
		st.FileOrder[cleanParent] = combined
	})
	if err == nil {
		w.mu.Lock()
		w.state = nextState
		w.mu.Unlock()
	}
	w.RescanNotebook()
	w.RequestRedraw()
}

// ForgetNotebook removes root from recent Notebooks without touching files on disk.
func (w *Workspace) ForgetNotebook(root string) {
	clean := filepath.Clean(root)
	next, err := files.UpdateState(func(st *model.AppState) {
		if st.Folder == clean {
			return
		}
		filtered := make([]string, 0, len(st.Notebooks))
		for _, nb := range st.Notebooks {
			if filepath.Clean(nb) != clean {
				filtered = append(filtered, nb)
			}
		}
		st.Notebooks = filtered
	})
	if err == nil {
		w.mu.Lock()
		w.state = next
		w.mu.Unlock()
	}
	w.RequestRedraw()
}

// ChooseNotebookDialog opens the native directory picker to switch notebooks.
func (w *Workspace) ChooseNotebookDialog() {
	if !mygo.App.IsReady() {
		return
	}
	win := w.Window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    win,
			Title:     theme.Tr("选择笔记本文件夹", "Choose Notebook Folder"),
			Directory: true,
		})
		if err == nil && len(paths) > 0 && paths[0] != "" {
			_ = w.SetNotebook(paths[0])
		}
	}()
}

func fileTreeEqual(a, b []model.FileEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := &a[i], &b[i]
		if x.Path != y.Path || x.Name != y.Name || x.RawName != y.RawName ||
			x.IsDir != y.IsDir || x.IsMarkdown != y.IsMarkdown ||
			!fileTreeEqual(x.Children, y.Children) {
			return false
		}
	}
	return true
}

// RescanNotebook rescans the active notebook directory using current ScanOptions.
func (w *Workspace) RescanNotebook() {
	_ = w.rescanNotebook()
}

func (w *Workspace) rescanNotebook() bool {
	w.mu.RLock()
	folder := w.state.Folder
	opts := w.scanOptionsLocked()
	w.mu.RUnlock()
	if folder == "" {
		w.mu.Lock()
		changed := len(w.entries) > 0
		w.entries = nil
		w.mu.Unlock()
		return changed
	}
	list, err := files.ScanNotebook(folder, opts)
	w.mu.Lock()
	var changed bool
	if err != nil {
		errStr := err.Error()
		changed = w.notice != errStr
		w.notice = errStr
	} else {
		changed = w.notice != "" || !fileTreeEqual(w.entries, list)
		w.notice = ""
		w.entries = list
	}
	w.mu.Unlock()
	return changed
}

func (w *Workspace) targetCreateDir(root string) string {
	cleanRoot := filepath.Clean(root)
	w.mu.RLock()
	focusDir := w.sidebarUI.FocusDir
	active := w.state.ActiveTab
	w.mu.RUnlock()

	if focusDir != "" {
		cleanFocus := filepath.Clean(focusDir)
		if cleanFocus == cleanRoot || strings.HasPrefix(cleanFocus, cleanRoot+string(filepath.Separator)) {
			if st, err := os.Stat(cleanFocus); err == nil && st.IsDir() {
				return cleanFocus
			}
		}
	}
	if active != "" {
		parent := filepath.Dir(filepath.Clean(active))
		if parent != cleanRoot && strings.HasPrefix(parent, cleanRoot+string(filepath.Separator)) {
			if st, err := os.Stat(parent); err == nil && st.IsDir() {
				return parent
			}
		}
	}
	return cleanRoot
}

// CreateNewNote creates an Untitled.md / 未命名.md note in the target notebook folder,
// opens it in a tab, and focuses block 0.
func (w *Workspace) CreateNewNote() (string, error) {
	folder := w.ActiveNotebook()
	if folder == "" {
		return "", errors.New(theme.Tr("请先打开一个笔记本文件夹", "Open a notebook folder first"))
	}
	targetDir := w.targetCreateDir(folder)
	created, err := files.CreateNewNote(targetDir, theme.IsEn())
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	if targetDir != filepath.Clean(folder) {
		w.mu.Lock()
		w.sidebarUI.ExpandedDirs[targetDir] = true
		w.mu.Unlock()
	}
	w.RescanNotebook()
	if err := w.openNoteInternal(created, true, true, 0); err != nil {
		return created, err
	}
	if sess := w.SessionFor(created); sess != nil {
		sess.FocusBlock(0)
	}
	return created, nil
}

// CreateNewFolder creates a new subfolder in the target notebook folder and starts inline rename.
func (w *Workspace) CreateNewFolder() (string, error) {
	folder := w.ActiveNotebook()
	if folder == "" {
		return "", errors.New(theme.Tr("请先打开一个笔记本文件夹", "Open a notebook folder first"))
	}
	targetDir := w.targetCreateDir(folder)
	created, err := files.CreateNewFolder(targetDir, theme.IsEn())
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	if targetDir != filepath.Clean(folder) {
		w.mu.Lock()
		w.sidebarUI.ExpandedDirs[targetDir] = true
		w.mu.Unlock()
	}
	w.RescanNotebook()
	w.sidebarUI.StartRename(created, true)
	w.RequestRedraw()
	return created, nil
}

// RenameWorkspaceItem renames oldPath to newName on disk and rebases open tabs, sessions, surfaces, and history.
func (w *Workspace) RenameWorkspaceItem(oldPath, newName string) (string, error) {
	w.FlushDirtySessions()
	newPath, err := files.RenameItem(oldPath, newName)
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	w.rebaseRenamedPaths(oldPath, newPath)
	nextState := files.LoadState()
	w.mu.Lock()
	w.state.FileOrder = nextState.FileOrder
	w.mu.Unlock()
	w.RescanNotebook()
	w.RequestRedraw()
	return newPath, nil
}

// MoveWorkspaceItem moves srcPath into dstDir and rebases open tabs, sessions, surfaces, and history.
func (w *Workspace) MoveWorkspaceItem(srcPath, dstDir string) (string, error) {
	w.FlushDirtySessions()
	newPath, err := files.MoveItem(srcPath, dstDir)
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	w.sidebarUI.ExpandedDirs[filepath.Clean(dstDir)] = true
	w.rebaseRenamedPaths(srcPath, newPath)
	nextState := files.LoadState()
	w.mu.Lock()
	w.state.FileOrder = nextState.FileOrder
	w.mu.Unlock()
	w.RescanNotebook()
	w.RequestRedraw()
	return newPath, nil
}

// TrashWorkspaceItem moves path to Trash and closes any affected tabs.
func (w *Workspace) TrashWorkspaceItem(path string) error {
	if err := files.TrashItem(path); err != nil {
		w.setNotice(err.Error())
		return err
	}
	w.removeClosedOrDeletedPath(path)
	w.RescanNotebook()
	w.RequestRedraw()
	return nil
}

func (w *Workspace) rebaseRenamedPaths(from, to string) {
	cleanFrom := filepath.Clean(from)
	cleanTo := filepath.Clean(to)
	// Annotations of every note under a moved folder follow it, open or not.
	if err := files.MoveNoteAnnotations("", cleanFrom, cleanTo); err != nil {
		files.LogWarnf("move annotations %s -> %s: %v", cleanFrom, cleanTo, err)
	}
	prefix := cleanFrom + string(filepath.Separator)
	rebase := func(p string) (string, bool) {
		cp := filepath.Clean(p)
		if cp == cleanFrom {
			return cleanTo, true
		}
		if strings.HasPrefix(cp, prefix) {
			return filepath.Join(cleanTo, strings.TrimPrefix(cp, prefix)), true
		}
		return p, false
	}

	w.mu.Lock()
	rebasedTabs := make([]string, 0, len(w.state.OpenTabs))
	seenTabs := make(map[string]bool, len(w.state.OpenTabs))
	for _, t := range w.state.OpenTabs {
		next, _ := rebase(t)
		cleanNext := filepath.Clean(next)
		if !seenTabs[cleanNext] {
			seenTabs[cleanNext] = true
			rebasedTabs = append(rebasedTabs, cleanNext)
		}
	}
	w.state.OpenTabs = rebasedTabs
	if next, ok := rebase(w.state.ActiveTab); ok {
		w.state.ActiveTab = next
	}
	w.nav.Rebase(cleanFrom, cleanTo)
	w.closed.Rebase(cleanFrom, cleanTo)
	if w.sidebarUI.FocusDir != "" {
		if next, ok := rebase(w.sidebarUI.FocusDir); ok {
			w.sidebarUI.FocusDir = next
		}
	}
	if w.historyToast != nil {
		if next, ok := rebase(w.historyToast.NotePath); ok {
			w.historyToast.NotePath = next
		}
	}
	if w.tabMenu.TabMenuOpen {
		if next, ok := rebase(w.tabMenu.TabMenuPath); ok {
			w.tabMenu.TabMenuPath = next
		}
	}
	nextVirtual := make(map[string]bool, len(w.virtualPreview))
	for k, v := range w.virtualPreview {
		if next, ok := rebase(k); ok {
			nextVirtual[next] = v
		} else {
			nextVirtual[k] = v
		}
	}
	w.virtualPreview = nextVirtual

	nextSnap := make(map[string]time.Time, len(w.lastSnapAt))
	for k, v := range w.lastSnapAt {
		if next, ok := rebase(k); ok {
			nextSnap[next] = v
		} else {
			nextSnap[k] = v
		}
	}
	w.lastSnapAt = nextSnap

	nextSessions := make(map[string]*editor.Session, len(w.sessions))
	for k, sess := range w.sessions {
		if next, ok := rebase(k); ok {
			sess.SetNotePath(next)
			nextSessions[next] = sess
		} else {
			nextSessions[k] = sess
		}
	}
	w.sessions = nextSessions

	nextSurfaces := make(map[string]*TabSurface, len(w.surfaces))
	for k, surf := range w.surfaces {
		if next, ok := rebase(k); ok {
			surf.Path = next
			nextSurfaces[next] = surf
		} else {
			nextSurfaces[k] = surf
		}
	}
	w.surfaces = nextSurfaces

	tabs := append([]string(nil), w.state.OpenTabs...)
	active := w.state.ActiveTab
	w.mu.Unlock()

	w.persistTabs(tabs, active)
}

func (w *Workspace) removeClosedOrDeletedPath(target string) {
	cleanTarget := filepath.Clean(target)
	prefix := cleanTarget + string(filepath.Separator)
	matches := func(p string) bool {
		cp := filepath.Clean(p)
		return cp == cleanTarget || strings.HasPrefix(cp, prefix)
	}

	w.mu.Lock()
	for k := range w.sessions {
		if matches(k) {
			delete(w.sessions, k)
			delete(w.virtualPreview, k)
			delete(w.lastSnapAt, k)
		}
	}
	for k := range w.surfaces {
		if matches(k) {
			delete(w.surfaces, k)
		}
	}
	w.closed.RemoveUnder(cleanTarget)
	w.nav.RemoveUnder(cleanTarget)

	activeIdx := -1
	surviving := make([]string, 0, len(w.state.OpenTabs))
	for i, t := range w.state.OpenTabs {
		if t == w.state.ActiveTab {
			activeIdx = i
		}
		if !matches(t) {
			surviving = append(surviving, t)
		}
	}
	w.state.OpenTabs = surviving
	nextActive := w.state.ActiveTab
	if matches(w.state.ActiveTab) {
		if len(surviving) > 0 {
			pick := activeIdx
			if pick < 0 {
				pick = 0
			}
			if pick >= len(surviving) {
				pick = len(surviving) - 1
			}
			nextActive = surviving[pick]
		} else {
			nextActive = ""
		}
		w.state.ActiveTab = nextActive
	}
	if w.historyToast != nil && matches(w.historyToast.NotePath) {
		w.historyToast = nil
	}
	if w.historyView != nil && w.historyView.Open && matches(w.historyView.NotePath) {
		w.historyView.Close()
	}
	if w.tabMenu.TabMenuOpen && matches(w.tabMenu.TabMenuPath) {
		w.tabMenu.Close()
	}
	if w.sidebarUI.FocusDir != "" && matches(w.sidebarUI.FocusDir) {
		w.sidebarUI.FocusDir = ""
	}
	tabs := append([]string(nil), w.state.OpenTabs...)
	active := w.state.ActiveTab
	w.mu.Unlock()

	if active != "" {
		_ = w.ensureTabLoaded(active)
	}
	w.persistTabs(tabs, active)
}

// ReorderTab moves srcPath before (after==false) or after (after==true) targetPath and persists state.json.
func (w *Workspace) ReorderTab(srcPath, targetPath string, after bool) bool {
	if w.isBusy() {
		return false
	}
	cleanSrc := filepath.Clean(srcPath)
	cleanDst := filepath.Clean(targetPath)
	w.mu.Lock()
	nextTabs, moved := w.tabOps.ReorderTab(w.state.OpenTabs, cleanSrc, cleanDst, after)
	if !moved {
		w.mu.Unlock()
		return false
	}
	w.state.OpenTabs = nextTabs
	tabs := append([]string(nil), nextTabs...)
	active := w.state.ActiveTab
	w.mu.Unlock()

	w.persistTabs(tabs, active)
	w.RequestRedraw()
	return true
}

// CloseTab flushes dirty changes on path, pushes it to ClosedTabs, and closes the tab.
func (w *Workspace) CloseTab(path string) {
	if w.isBusy() {
		return
	}
	clean := filepath.Clean(path)
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.mu.Lock()
	delete(w.lastSnapAt, clean)
	w.mu.Unlock()
	if err := w.flushSingleSession(clean); err != nil {
		return
	}

	w.mu.Lock()
	closedIdx := -1
	for i, t := range w.state.OpenTabs {
		if t == clean {
			closedIdx = i
			break
		}
	}
	isVirtual := w.virtualPreview[clean]
	delete(w.virtualPreview, clean)
	nextTabs, nextActive, closed := w.tabOps.CloseTab(w.state.OpenTabs, w.state.ActiveTab, clean)
	if !closed {
		w.mu.Unlock()
		return
	}
	w.state.OpenTabs = nextTabs
	w.state.ActiveTab = nextActive
	delete(w.sessions, clean)
	delete(w.surfaces, clean)
	if w.historyToast != nil && filepath.Clean(w.historyToast.NotePath) == clean {
		w.historyToast = nil
	}
	if w.historyView != nil && w.historyView.Open && filepath.Clean(w.historyView.NotePath) == clean {
		w.historyView.Close()
	}
	if !isVirtual {
		w.closed.PushAt(clean, closedIdx)
	}
	tabs := append([]string(nil), nextTabs...)
	w.mu.Unlock()

	if nextActive != "" {
		_ = w.ensureTabLoaded(nextActive)
	}
	w.persistTabs(tabs, nextActive)
	w.RequestRedraw()
}

// CloseActiveTab closes the active tab.
func (w *Workspace) CloseActiveTab() {
	if w.modals != nil && w.modals.IsSettingsOpen() {
		return
	}
	active := w.ActiveNotePath()
	if active != "" {
		w.CloseTab(active)
	}
}

// ReopenClosedTab pops the most recently closed tab (`Cmd+Shift+T`) and restores it at its prior index.
func (w *Workspace) ReopenClosedTab() bool {
	if w.isBusy() {
		return false
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	for {
		entry, ok := w.closed.PopEntry()
		if !ok {
			break
		}
		if entry.Path == "" {
			continue
		}
		w.mu.RLock()
		alreadyOpen := false
		for _, t := range w.state.OpenTabs {
			if t == entry.Path {
				alreadyOpen = true
				break
			}
		}
		w.mu.RUnlock()
		if alreadyOpen {
			continue
		}
		if info, err := os.Stat(entry.Path); err == nil && info.Mode().IsRegular() {
			if err := w.openNoteInternal(entry.Path, true, true, 0); err != nil {
				continue
			}
			if entry.Index >= 0 {
				w.mu.Lock()
				if nextTabs, moved := w.tabOps.MoveToIndex(w.state.OpenTabs, entry.Path, entry.Index); moved {
					w.state.OpenTabs = nextTabs
					tabs := append([]string(nil), nextTabs...)
					active := w.state.ActiveTab
					w.mu.Unlock()
					w.persistTabs(tabs, active)
					w.RequestRedraw()
				} else {
					w.mu.Unlock()
				}
			}
			return true
		}
	}
	return false
}

// CloseOtherTabs closes every open tab except keepPath.
func (w *Workspace) CloseOtherTabs(keepPath string) {
	if w.isBusy() {
		return
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	if err := w.flushDirtySessions(); err != nil {
		return
	}
	clean := filepath.Clean(keepPath)
	w.mu.Lock()
	origTabs := append([]string(nil), w.state.OpenTabs...)
	nextTabs, nextActive, closedPaths := w.tabOps.CloseOthers(w.state.OpenTabs, clean)
	if len(closedPaths) == 0 {
		w.mu.Unlock()
		return
	}
	w.closed.RecordOthers(origTabs, clean, func(p string) bool { return w.virtualPreview[p] })
	for _, p := range closedPaths {
		delete(w.virtualPreview, p)
		delete(w.sessions, p)
		delete(w.surfaces, p)
		delete(w.lastSnapAt, p)
	}
	if w.historyToast != nil && filepath.Clean(w.historyToast.NotePath) != clean {
		w.historyToast = nil
	}
	if w.historyView != nil && w.historyView.Open && filepath.Clean(w.historyView.NotePath) != clean {
		w.historyView.Close()
	}
	w.state.OpenTabs = nextTabs
	w.state.ActiveTab = nextActive
	tabs := append([]string(nil), nextTabs...)
	w.mu.Unlock()

	if nextActive != "" {
		_ = w.ensureTabLoaded(nextActive)
	}
	w.persistTabs(tabs, nextActive)
	w.RequestRedraw()
}

// CloseTabsToRight closes every open tab strictly to the right of pivotPath.
func (w *Workspace) CloseTabsToRight(pivotPath string) {
	if w.isBusy() {
		return
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	if err := w.flushDirtySessions(); err != nil {
		return
	}
	clean := filepath.Clean(pivotPath)
	w.mu.Lock()
	pivotIdx := -1
	for i, t := range w.state.OpenTabs {
		if t == clean {
			pivotIdx = i
			break
		}
	}
	nextTabs, nextActive, closedPaths := w.tabOps.CloseToRight(w.state.OpenTabs, w.state.ActiveTab, clean)
	if len(closedPaths) == 0 {
		w.mu.Unlock()
		return
	}
	if pivotIdx >= 0 {
		w.closed.RecordFrom(pivotIdx+1, closedPaths, func(p string) bool { return w.virtualPreview[p] })
	}
	closedSet := make(map[string]bool, len(closedPaths))
	for _, p := range closedPaths {
		closedSet[p] = true
		delete(w.virtualPreview, p)
		delete(w.sessions, p)
		delete(w.surfaces, p)
		delete(w.lastSnapAt, p)
	}
	if w.historyToast != nil && closedSet[filepath.Clean(w.historyToast.NotePath)] {
		w.historyToast = nil
	}
	if w.historyView != nil && w.historyView.Open && closedSet[filepath.Clean(w.historyView.NotePath)] {
		w.historyView.Close()
	}
	w.state.OpenTabs = nextTabs
	w.state.ActiveTab = nextActive
	tabs := append([]string(nil), nextTabs...)
	w.mu.Unlock()

	if nextActive != "" {
		_ = w.ensureTabLoaded(nextActive)
	}
	w.persistTabs(tabs, nextActive)
	w.RequestRedraw()
}

// CloseAllTabs closes all open tabs.
func (w *Workspace) CloseAllTabs() {
	if w.isBusy() {
		return
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	if err := w.flushDirtySessions(); err != nil {
		return
	}
	w.mu.Lock()
	w.closed.RecordFrom(0, w.state.OpenTabs, func(p string) bool { return w.virtualPreview[p] })
	w.virtualPreview = make(map[string]bool)
	w.sessions = make(map[string]*editor.Session)
	w.surfaces = make(map[string]*TabSurface)
	w.lastSnapAt = make(map[string]time.Time)
	w.historyToast = nil
	if w.historyView != nil && w.historyView.Open {
		w.historyView.Close()
	}
	w.state.OpenTabs = nil
	w.state.ActiveTab = ""
	w.mu.Unlock()
	w.persistTabs(nil, "")
	w.RequestRedraw()
}

// StepTab cycles forward (+1) or backward (-1) across open tabs (`Ctrl+Tab` / `Ctrl+Shift+Tab`).
func (w *Workspace) StepTab(delta int) {
	if w.modals != nil && w.modals.IsSettingsOpen() {
		return
	}
	if w.isBusy() {
		return
	}
	w.mu.RLock()
	next := w.tabOps.Cycle(w.state.OpenTabs, w.state.ActiveTab, delta)
	w.mu.RUnlock()
	if next != "" {
		_ = w.openNoteInternal(next, false, true, 0)
	}
}

// SelectTabOneBased selects tab 1..9 (`Cmd+9` selects the last tab).
func (w *Workspace) SelectTabOneBased(num int) {
	if w.isBusy() {
		return
	}
	w.mu.RLock()
	next := w.tabOps.SelectOneBased(w.state.OpenTabs, num)
	w.mu.RUnlock()
	if next != "" {
		_ = w.openNoteInternal(next, false, true, 0)
	}
}

func (w *Workspace) isBusy() bool {
	if w.ai != nil && w.ai.IsStreaming() {
		return true
	}
	w.mu.RLock()
	active := w.state.ActiveTab
	sess := w.sessions[active]
	w.mu.RUnlock()
	if sess != nil && sess.IsBusy() {
		return true
	}
	return false
}

// NavigateBack navigates backward in note history (`Cmd+[`).
func (w *Workspace) NavigateBack() {
	if w.modals != nil && w.modals.IsSettingsOpen() {
		return
	}
	if w.isBusy() {
		return
	}
	for {
		target := w.nav.Back()
		if target == "" {
			w.RequestRedraw()
			return
		}
		if err := w.openNoteInternal(target, false, false, 0); err == nil {
			return
		}
	}
}

// NavigateForward navigates forward in note history (`Cmd+]`).
func (w *Workspace) NavigateForward() {
	if w.modals != nil && w.modals.IsSettingsOpen() {
		return
	}
	if w.isBusy() {
		return
	}
	for {
		target := w.nav.Forward()
		if target == "" {
			w.RequestRedraw()
			return
		}
		if err := w.openNoteInternal(target, false, false, 0); err == nil {
			return
		}
	}
}

// ToggleSidebar collapses or expands (or opens/closes drawer on narrow viewports) the left sidebar (`Alt+B`).
func (w *Workspace) ToggleSidebar(viewportWidth float32) {
	if w.IsPageHistoryOpen() || w.exportCtrl.IsOpen() {
		return
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()

	w.mu.Lock()
	if viewportWidth <= 0 {
		viewportWidth = w.lastViewportW
	}
	if viewportWidth <= 0 && w.win != nil {
		cw, _ := w.win.ContentSize()
		if cw > 0 {
			z := float32(w.zoomPct) / 100.0
			if z <= 0 {
				z = 1.0
			}
			viewportWidth = float32(cw) / z
		}
	}
	if viewportWidth <= 0 {
		viewportWidth = 1100
	}
	layout := ResolveShellLayout(w.shellInputLocked(viewportWidth))
	if layout.SidebarDrawer {
		w.sidebarPinned = false
	} else if layout.SidebarDocked {
		w.state.SidebarCollapsed = true
		w.sidebarPinned = false
	} else {
		w.state.SidebarCollapsed = false
		w.sidebarPinned = true
	}
	collapsed := w.state.SidebarCollapsed
	w.mu.Unlock()

	_, _ = files.UpdateState(func(st *model.AppState) {
		st.SidebarCollapsed = collapsed
	})
	w.RequestRedraw()
}

// ToggleAIPanel opens or closes the AI Assistant panel (`Cmd+J`), unless blocked by Settings.
func (w *Workspace) ToggleAIPanel() bool {
	if w.modals.IsSettingsOpen() {
		return false
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.ai.ToggleOpen()
	w.RequestRedraw()
	return true
}

// SetZoomPct sets and persists the main window zoom percentage within [70%, 130%] (10% steps).
func (w *Workspace) SetZoomPct(pct int) {
	pct = snapMainZoomPct(pct)
	z := float32(pct) / 100.0

	w.mu.Lock()
	w.zoomPct = pct
	w.state.EditorZoom = &z
	w.mu.Unlock()

	theme.SetZoom(z)
	if next, err := files.UpdateState(func(st *model.AppState) {
		zz := z
		st.EditorZoom = &zz
	}); err == nil {
		w.mu.Lock()
		w.state = next
		w.mu.Unlock()
	}
	w.RequestRedraw()
}

// StepZoomPct adjusts the main window zoom by deltaPct (e.g. +10 or -10) within [70%, 130%].
func (w *Workspace) StepZoomPct(deltaPct int) int {
	cur := snapMainZoomPct(w.ZoomPct())
	next := max(MainZoomMinPct, min(cur+deltaPct, MainZoomMaxPct))
	w.SetZoomPct(next)
	return next
}

// ResetZoomPct resets the main window zoom back to 100% and persists it.
func (w *Workspace) ResetZoomPct() int {
	w.SetZoomPct(MainZoomDefaultPct)
	return MainZoomDefaultPct
}

// ZoomPct returns the current zoom percentage (default 100).
func (w *Workspace) ZoomPct() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.zoomPct <= 0 {
		return 100
	}
	return w.zoomPct
}

func (w *Workspace) persistTabs(tabs []string, active string) {
	_, _ = files.UpdateState(func(st *model.AppState) {
		st.OpenTabs = append([]string(nil), tabs...)
		st.ActiveTab = active
	})
}

func (w *Workspace) setNotice(msg string) {
	w.mu.Lock()
	w.notice = msg
	w.mu.Unlock()
	w.RequestRedraw()
}

const (
	workspaceAutoSaveDebounce = 300 * time.Millisecond
	sidebarWidthSaveDebounce  = 200 * time.Millisecond
	fileWatchCoalesceWindow   = 120 * time.Millisecond
)

func (w *Workspace) hasDirtyNonVirtualSession() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for p, sess := range w.sessions {
		if sess.Dirty() && !w.virtualPreview[p] {
			return true
		}
	}
	return false
}

func (w *Workspace) flushOrScheduleDirtySessions() {
	if !w.hasDirtyNonVirtualSession() {
		return
	}
	w.mu.Lock()
	if w.win == nil || blockui.IsProbeEnabled() {
		w.mu.Unlock()
		w.FlushDirtySessions()
		return
	}
	if w.autoSaveTimer != nil {
		w.autoSaveTimer.Reset(workspaceAutoSaveDebounce)
		w.mu.Unlock()
		return
	}
	w.autoSaveTimer = time.AfterFunc(workspaceAutoSaveDebounce, func() {
		mygo.RunOnMain(func() {
			w.mu.Lock()
			w.autoSaveTimer = nil
			w.mu.Unlock()
			w.FlushDirtySessions()
		})
	})
	w.mu.Unlock()
}

func (w *Workspace) scheduleSaveSidebarWidth(clamped float32) {
	w.mu.Lock()
	if w.win == nil || blockui.IsProbeEnabled() {
		w.hasPendingSidebarW = false
		w.mu.Unlock()
		_, _ = files.UpdateState(func(st *model.AppState) {
			st.SidebarWidth = &clamped
		})
		return
	}
	w.pendingSidebarWidth = clamped
	w.hasPendingSidebarW = true
	if w.sidebarWidthTimer != nil {
		w.sidebarWidthTimer.Reset(sidebarWidthSaveDebounce)
		w.mu.Unlock()
		return
	}
	w.sidebarWidthTimer = time.AfterFunc(sidebarWidthSaveDebounce, func() {
		w.flushPendingSidebarWidth()
	})
	w.mu.Unlock()
}

func (w *Workspace) flushPendingSidebarWidth() {
	w.mu.Lock()
	if w.sidebarWidthTimer != nil {
		w.sidebarWidthTimer.Stop()
		w.sidebarWidthTimer = nil
	}
	if !w.hasPendingSidebarW {
		w.mu.Unlock()
		return
	}
	clamped := w.pendingSidebarWidth
	w.hasPendingSidebarW = false
	w.mu.Unlock()
	_, _ = files.UpdateState(func(st *model.AppState) {
		st.SidebarWidth = &clamped
	})
}

// FlushDirtySessions writes all modified non-virtual editor sessions to disk atomically.
func (w *Workspace) FlushDirtySessions() {
	w.flushPendingSidebarWidth()
	_ = w.flushDirtySessions()
}

func (w *Workspace) flushDirtySessions() error {
	w.mu.Lock()
	if w.autoSaveTimer != nil {
		w.autoSaveTimer.Stop()
		w.autoSaveTimer = nil
	}
	paths := make([]string, 0, len(w.sessions))
	for p, sess := range w.sessions {
		if sess.Dirty() && !w.virtualPreview[p] {
			paths = append(paths, p)
		}
	}
	w.mu.Unlock()
	var firstErr error
	for _, p := range paths {
		if err := w.flushSingleSession(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (w *Workspace) flushSingleSession(path string) error {
	w.mu.RLock()
	sess := w.sessions[path]
	isVirtual := w.virtualPreview[path]
	lastSnap := w.lastSnapAt[path]
	w.mu.RUnlock()
	if sess == nil || !sess.Dirty() || isVirtual {
		return nil
	}
	md := sess.Markdown()
	if _, err := files.WriteNoteAtomic(path, md); err != nil {
		files.LogWarnf("auto-save failed for %s: %v", path, err)
		_ = files.SaveRecovery(path, md)
		return err
	}
	sess.MarkSaved()
	files.ClearRecovery(path)
	now := time.Now()
	if now.Sub(lastSnap) >= 5*time.Second {
		_, _ = files.RecordHistorySnapshot(path, md, theme.Tr("自动保存", "Auto-save"))
		w.mu.Lock()
		w.lastSnapAt[path] = now
		w.mu.Unlock()
	}
	return nil
}

func (w *Workspace) startWatching(folder string) {
	w.mu.Lock()
	if w.watchCancel != nil {
		w.watchCancel()
		w.watchCancel = nil
	}
	if !mygo.App.IsReady() || folder == "" {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.watchCancel = cancel
	w.mu.Unlock()

	watcher, err := mygo.WatchFiles(ctx, folder, mygo.FileWatchOptions{
		Recursive: true,
		HintsOnly: true,
		Ignore: func(p string, isDir bool) bool {
			base := filepath.Base(p)
			return isDir && (strings.HasPrefix(base, ".") || base == "node_modules")
		},
	})
	if err != nil {
		return
	}
	go func() {
		defer watcher.Close()
		events := make(chan struct{}, 1)
		go func() {
			for {
				if _, err := watcher.Next(ctx); err != nil {
					close(events)
					return
				}
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}()

		var debounce *time.Timer
		var debounceC <-chan time.Time
		defer func() {
			if debounce != nil {
				debounce.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-events:
				if !ok {
					return
				}
				if debounce == nil {
					debounce = time.NewTimer(fileWatchCoalesceWindow)
					debounceC = debounce.C
				} else {
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(fileWatchCoalesceWindow)
				}
			case <-debounceC:
				debounceC = nil
				if w.rescanNotebook() {
					w.RequestRedraw()
				}
			}
		}
	}()
}

// --- Rendering & App Shortcut Routing ---

func (w *Workspace) shellInputLocked(winW float32) ShellInput {
	inSettings := w.modals.IsSettingsOpen()
	return ShellInput{
		WindowWidth:     winW,
		InSettings:      inSettings,
		SidebarVisible:  !w.state.SidebarCollapsed,
		SidebarPinned:   w.sidebarPinned,
		SidebarDesiredW: w.sidebarWidth,
		ChatVisible:     w.ai.IsOpen(),
		ChatFullscreen:  w.ai.IsFullscreen(),
		ChatDesiredW:    w.ai.PanelWidth(),
	}
}

func (w *Workspace) handleAppShortcuts(c *ui.Context, vpW float32) {
	if w.modals.IsRecordingShortcut() || w.exportCtrl.IsOpen() || w.IsPageHistoryOpen() {
		return
	}

	st := w.State()

	matchRaw := func(raw string) bool {
		mods, key, ok := blockui.ParseShortcut(raw)
		if !ok {
			return false
		}
		return c.Shortcut(mods, key)
	}
	matchAction := func(action string) bool {
		return matchRaw(modals.EffectiveShortcut(st, action))
	}

	if w.modals.IsSearchOpen() {
		if matchRaw("cmd-w") {
			w.modals.CloseSearch()
			return
		}
		if c.Shortcut(ui.Cmd, ui.Key0) {
			w.modals.ResetSearchScopeAll()
			return
		}
		if matchAction("global_summon") {
			if modals.MarkSearchTriggered() {
				w.modals.OpenSearch(true)
			}
			return
		}
		if matchAction("search") {
			w.modals.OpenSearch(false)
			return
		}
		if matchAction("quick_note") {
			w.quick.TriggerHotkey()
			return
		}
		return
	}

	if matchAction("global_summon") {
		if modals.MarkSearchTriggered() {
			w.modals.OpenSearch(true)
		}
		return
	}
	if matchAction("quick_note") {
		w.quick.TriggerHotkey()
		return
	}
	if matchAction("search") {
		w.modals.OpenSearch(false)
		return
	}
	if matchRaw("cmd-,") {
		if w.modals.IsSettingsOpen() {
			w.modals.CloseSettings()
		} else {
			w.OpenSettings("ai")
		}
		return
	}
	if w.modals.IsSettingsOpen() {
		if matchAction("toggle_sidebar") || c.Shortcut(ui.Cmd, ui.KeyBackslash) {
			w.ToggleSidebar(vpW)
			return
		}
		return
	}
	if matchAction("performance") {
		if w.modals.IsPerformanceOpen() {
			w.modals.ClosePerformance()
		} else {
			w.modals.OpenPerformance()
		}
		return
	}
	if matchAction("toggle_ai") {
		w.ToggleAIPanel()
		return
	}
	if matchAction("toggle_sidebar") || c.Shortcut(ui.Cmd, ui.KeyBackslash) {
		w.ToggleSidebar(vpW)
		return
	}
	if matchAction("new_note") || matchRaw("cmd-t") {
		_, _ = w.CreateNewNote()
		return
	}
	if matchRaw("cmd-shift-n") {
		_, _ = w.CreateNewFolder()
		return
	}
	if matchRaw("cmd-w") {
		w.CloseActiveTab()
		return
	}
	if matchRaw("cmd-shift-t") {
		w.ReopenClosedTab()
		return
	}
	if matchAction("export_image") {
		w.OpenExportStage()
		return
	}
	if c.Shortcut(ui.Ctrl, ui.KeyTab) || c.Shortcut(ui.Ctrl, ui.KeyPageDown) ||
		c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketRight) || c.Shortcut(ui.Cmd|ui.Alt, ui.KeyRight) {
		w.StepTab(1)
		return
	}
	if c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab) || c.Shortcut(ui.Ctrl, ui.KeyPageUp) ||
		c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketLeft) || c.Shortcut(ui.Cmd|ui.Alt, ui.KeyLeft) {
		w.StepTab(-1)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyBracketLeft) || (!theme.IsMacOS() && c.Shortcut(ui.Alt, ui.KeyLeft)) {
		w.NavigateBack()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyBracketRight) || (!theme.IsMacOS() && c.Shortcut(ui.Alt, ui.KeyRight)) {
		w.NavigateForward()
		return
	}
	for n := 1; n <= 9; n++ {
		if c.Shortcut(ui.Cmd, ui.Key0+ui.Key(n)) {
			w.SelectTabOneBased(n)
			return
		}
	}
}

func (w *Workspace) handleEditorZoomShortcuts(c *ui.Context) bool {
	if w.modals.IsRecordingShortcut() || w.modals.IsSettingsOpen() ||
		w.modals.HasActiveModal() || w.exportCtrl.IsOpen() || w.IsPageHistoryOpen() {
		return false
	}
	if c.Shortcut(ui.Cmd, ui.KeyEqual) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEqual) {
		w.StepZoomPct(MainZoomStepPct)
		return true
	}
	if c.Shortcut(ui.Cmd, ui.KeyMinus) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyMinus) {
		w.StepZoomPct(-MainZoomStepPct)
		return true
	}
	if c.Shortcut(ui.Cmd, ui.Key0) {
		w.ResetZoomPct()
		return true
	}
	return false
}

// Render renders the complete main window UI for one frame.
func (w *Workspace) Render(c *ui.Context) {
	blockui.ClearTargets()

	w.handleEditorZoomShortcuts(c)

	zoom := float32(w.ZoomPct()) / 100.0
	if zoom <= 0 {
		zoom = 1.0
	}
	if c.Zoom() != 1.0 {
		c.SetZoom(1.0)
	}
	theme.SetZoom(zoom)

	edFont, _, _ := theme.Typography()
	pal := theme.Ocean()
	uiTheme := pal.ToUITheme(edFont)
	uiTheme.ScrollbarStyle = ui.ScrollbarStyle{
		TrackWidth: theme.ScrollGutter,
		ThumbWidth: theme.ScrollThumbWidth,
		MinLength:  32,
		Thumb:      pal.ScrollThumb,
		Hover:      pal.ScrollThumbHover,
		Active:     pal.ScrollThumbHover,
	}
	c.SetTheme(&uiTheme)

	// Full-window 12-theme A-Export stage covers the entire window when active.
	if w.exportCtrl.IsOpen() {
		c.Key("workspace-root")
		root := ui.Column(c).Fill().Background(pal.Shell).TextColor(pal.Text).Clip()
		blockui.Probe(root, "workspace-root")
		root.Children(func() {
			ui.Column(c.Key("stage-export")).Fill().Transition(blockui.SurfaceStageTransition()).Children(func() {
				w.exportCtrl.Render(c)
			})
			blockui.RenderSmokeHUD(c)
		})
		return
	}

	// Full-window Page History stage covers the entire window when active.
	if w.IsPageHistoryOpen() {
		c.Key("workspace-root")
		root := ui.Column(c).Fill().Background(pal.Shell).TextColor(pal.Text).Clip()
		blockui.Probe(root, "workspace-root")
		root.Children(func() {
			notePath := w.historyView.NotePath
			ui.Column(c.Key("stage-history")).Fill().Transition(blockui.SurfaceStageTransition()).Children(func() {
				w.historyView.Render(
					c,
					func() { w.ClosePageHistory() },
					func(id string) {
						w.historyView.SelectVersion(id, func(snapID string) (string, error) {
							return files.ReadHistorySnapshot(notePath, snapID)
						}, files.DiffLines)
						w.RequestRedraw()
					},
					func(delta int) {
						w.historyView.Navigate(delta, func(snapID string) (string, error) {
							return files.ReadHistorySnapshot(notePath, snapID)
						}, files.DiffLines)
						w.RequestRedraw()
					},
					func() { w.OpenPageHistory() },
					func(_ string, restoredText string) {
						preRestore, _ := w.ReadNote(notePath)
						_, _ = files.RecordHistorySnapshot(notePath, preRestore, theme.Tr("恢复前快照", "Before restore"))
						_ = w.WriteNote(notePath, restoredText)
						w.historyView.Close()
						w.mu.Lock()
						w.historyToast = &HistoryToast{
							Active:      true,
							NotePath:    notePath,
							Message:     theme.Tr("已恢复到所选历史版本", "Restored selected snapshot"),
							UndoContent: preRestore,
							CanUndo:     preRestore != restoredText,
						}
						w.mu.Unlock()
						w.RequestRedraw()
					},
				)
			})
			w.modals.RenderModals(c)
			blockui.RenderSmokeHUD(c)
		})
		return
	}

	vpW, _ := c.Size()
	if vpW <= 0 {
		vpW = 1100
	}

	w.handleAppShortcuts(c, vpW)
	w.flushOrScheduleDirtySessions()

	w.mu.Lock()
	w.lastViewportW = vpW
	in := w.shellInputLocked(vpW)
	layout := ResolveShellLayout(in)
	if layout.SidebarDocked && !layout.ChatFullscreen {
		w.sidebarPinned = false
	}
	tabs := append([]string(nil), w.state.OpenTabs...)
	activeTab := w.state.ActiveTab
	activeNB := w.state.Folder
	notebooks := append([]string(nil), w.state.Notebooks...)
	entries := append([]model.FileEntry(nil), w.entries...)
	notice := w.notice
	hasFBDraft := w.state.FeedbackDraft != nil
	toast := w.historyToast
	stSnap := w.state
	w.mu.Unlock()

	inSettings := w.modals.IsSettingsOpen()
	aiImmersive := layout.ChatFullscreen && !inSettings
	sidebarWide := layout.SidebarDocked || layout.SidebarDrawer
	sidebarAutoHidden := layout.SidebarYielded && !layout.SidebarDrawer
	dockedSidebarWidth := float32(0)
	if layout.SidebarDocked {
		dockedSidebarWidth = layout.SidebarWidth
	}

	activeSurf := w.SurfaceFor(activeTab)
	var activeSess *editor.Session
	if activeSurf == nil {
		activeSess = w.SessionFor(activeTab)
	}

	var annDrafts int
	var annGenerating, annHasAny bool
	if activeSess != nil {
		for _, a := range activeSess.Annotations() {
			body := strings.TrimSpace(a.Note) != ""
			if body && a.RewriteResult == "" && !a.RewriteBusy {
				annDrafts++
			}
			annGenerating = annGenerating || a.RewriteBusy
			annHasAny = annHasAny || (body && !a.Applied)
		}
	}

	tbSurface := SurfaceEmpty
	var srcStatus, zoomPctStr string
	var zoomFit, svgShowSource bool
	if activeSurf != nil {
		tbSurface = activeSurf.TitlebarSurface()
		srcStatus = activeSurf.SourceStatusLabel()
		zoomPctStr = activeSurf.ZoomPctString()
		zoomFit = activeSurf.ZoomFitMode
		svgShowSource = activeSurf.SVGShowSource
	} else if activeSess != nil {
		tbSurface = SurfaceNote
	}

	c.Key("workspace-root")
	root := ui.Column(c).Fill().Background(pal.Shell).TextColor(pal.Text).Clip()
	blockui.Probe(root, "workspace-root")

	root.Children(func() {
		// 1. Top 40px Titlebar (hidden in AI Immersive full-window mode).
		if !aiImmersive {
			RenderTitlebar(c, TitlebarProps{
				SidebarWide:       sidebarWide,
				SidebarAutoHidden: sidebarAutoHidden,
				SidebarWidth:      dockedSidebarWidth,
				InSettings:        inSettings,
				SettingsTabLabel:  w.modals.ActiveSettingsTab(),
				AIImmersive:       aiImmersive,
				AIOpen:            w.ai.IsOpen(),
				FeedbackOpen:      w.modals.IsFeedbackOpen(),
				HasFeedbackDraft:  hasFBDraft,
				NotebookPath:      activeNB,
				Tabs:              tabs,
				ActiveTab:         activeTab,
				IsDirty: func(path string) bool {
					if s := w.sessions[path]; s != nil {
						return s.Dirty()
					}
					if sf := w.surfaces[path]; sf != nil {
						return sf.IsDirty()
					}
					return false
				},
				ActiveSurface:   tbSurface,
				SourceStatus:    srcStatus,
				PreviewZoomPct:  zoomPctStr,
				PreviewZoomFit:  zoomFit,
				SVGShowSource:   svgShowSource,
				CanBack:         w.nav.CanBack(),
				CanForward:      w.nav.CanForward(),
				Busy:            w.isBusy(),
				AnnotDrafts:     annDrafts,
				AnnotGenerating: annGenerating,
				AnnotHasAny:     annHasAny,
				ShortcutSidebar: modals.EffectiveShortcut(stSnap, "toggle_sidebar"),
				ShortcutSearch:  modals.EffectiveShortcut(stSnap, "search"),
				ShortcutNewNote: modals.EffectiveShortcut(stSnap, "new_note"),
				SearchOpen:      w.modals.IsSearchOpen(),
				OnOpenSearch: func() {
					w.modals.OpenSearch(false)
				},
				ShortcutNewTab: "cmd-t",
				ShortcutAI:     modals.EffectiveShortcut(stSnap, "toggle_ai"),
				ShortcutExport: modals.EffectiveShortcut(stSnap, "export_image"),
				OnSendAnnotations: func() {
					if activeSess != nil {
						activeSess.GeneratePendingAnnotations()
						w.RequestRedraw()
					}
				},
				MenuState: w.tabMenu,
				OnToggleSidebar: func() {
					w.ToggleSidebar(vpW)
				},
				OnBack:    w.NavigateBack,
				OnForward: w.NavigateForward,
				OnSelectTab: func(path string) {
					_ = w.openNoteInternal(path, false, true, 0)
				},
				OnCloseTab:    w.CloseTab,
				OnCloseOthers: w.CloseOtherTabs,
				OnCloseRight:  w.CloseTabsToRight,
				OnCloseAll:    w.CloseAllTabs,
				OnReorderTab: func(srcPath, targetPath string, after bool) {
					w.ReorderTab(srcPath, targetPath, after)
				},
				OnNewNote: func() {
					_, _ = w.CreateNewNote()
				},
				OnToggleFeedback: func() {
					if w.modals.IsFeedbackOpen() {
						w.modals.CloseFeedback()
					} else {
						w.modals.OpenFeedback()
					}
				},
				OnToggleAI: func() {
					w.ToggleAIPanel()
				},
				OnOpenHistory: func() {
					w.OpenPageHistory()
				},
				OnOpenExport: func() {
					w.OpenExportStage()
				},
				OnCopyMarkdown: func() {
					if activeSess != nil && mygo.App.IsReady() {
						txt := activeSess.SelectedMarkdown()
						if txt == "" {
							txt = activeSess.Markdown()
						}
						mygo.Clipboard.WriteText(txt)
					}
				},
				OnRevealActive: func() {
					if activeTab != "" && mygo.App.IsReady() {
						mygo.Shell.ShowItemInFolder(activeTab)
					}
				},
				OnPreviewZoomStep: func(factor float32) {
					if activeSurf != nil {
						activeSurf.ZoomStep(factor)
						w.RequestRedraw()
					}
				},
				OnPreviewActual: func() {
					if activeSurf != nil {
						activeSurf.ZoomActual()
						w.RequestRedraw()
					}
				},
				OnPreviewFit: func() {
					if activeSurf != nil {
						activeSurf.ZoomFit()
						w.RequestRedraw()
					}
				},
				OnSelectSVGSource: func(showSource bool) {
					if activeSurf != nil {
						activeSurf.SVGShowSource = showSource
						w.RequestRedraw()
					}
				},
				OnOpenInBrowser: func() {
					if activeTab != "" {
						if mygo.App.IsReady() {
							_ = mygo.Shell.OpenPath(activeTab)
						} else {
							c.OpenURL("file://" + activeTab)
						}
					}
				},
			})
		}

		// 2. Main Split Body.
		body := ui.Row(c).Grow(1).MinHeight(0).FillWidth()
		body.Children(func() {
			// Docked Sidebar (File Tree in workspace mode, Category Nav in Settings mode).
			if !aiImmersive && layout.SidebarDocked {
				if inSettings {
					w.modals.RenderSettingsSidebar(c, false, nil)
				} else {
					ui.Row(c.Key("docked-sidebar-slot")).Shrink(0).FillHeight().
						Transition(blockui.DockedPanelTransition(false)).Children(func() {
						w.renderSidebarColumn(c, stSnap, activeNB, notebooks, entries, activeTab, notice, false)
					})
				}
			}

			// Center Content (Settings Canvas, Note Editor, Non-Markdown Surface, or Empty Mascot State).
			if inSettings {
				minMainW := min(layout.ContentFloor, vpW)
				ui.Column(c.Key("stage-settings")).Grow(1).MinWidth(minMainW).FillHeight().
					Background(pal.SettingsBG).Transition(blockui.SurfaceStageTransition()).Children(func() {
					w.modals.RenderSettingsBody(c, layout.SidebarDrawer, func() {
						w.ToggleSidebar(vpW)
					})
				})
			} else if !aiImmersive {
				minMainW := float32(0)
				if !layout.ChatDocked {
					minMainW = min(layout.ContentFloor, vpW)
				}
				mainCol := ui.Column(c.Key("stage-workspace")).Grow(1).MinWidth(minMainW).FillHeight().
					Background(pal.Paper).Transition(blockui.SurfaceStageTransition())
				mainCol.Children(func() {
					if toast != nil && toast.Active && toast.NotePath == activeTab {
						toast.RenderBanner(
							c,
							func() {
								undoTxt := toast.UndoContent
								target := toast.NotePath
								w.mu.Lock()
								w.historyToast = nil
								w.mu.Unlock()
								_ = w.WriteNote(target, undoTxt)
							},
							func() {
								w.mu.Lock()
								w.historyToast = nil
								w.mu.Unlock()
								w.RequestRedraw()
							},
						)
					}

					switch {
					case activeSurf != nil:
						activeSurf.Render(
							c,
							func(p, text string, bom bool) error {
								_, err := files.WriteRawFileAtomic(p, text, bom)
								return err
							},
							func(p string) {
								if mygo.App.IsReady() {
									mygo.Shell.ShowItemInFolder(p)
								}
							},
							func(p string) {
								if mygo.App.IsReady() {
									_ = mygo.Shell.OpenPath(p)
								}
							},
						)
					case activeSess != nil:
						activeSess.SetOutlineForceNarrow(!layout.ShowOutline)
						activeSess.Render(c, layout.MainWidth, false)
					default:
						w.renderEmptyWorkspaceMascot(c, activeNB, len(entries) == 0)
					}
				})
			}

			// Docked or Fullscreen AI Panel (stays docked even on narrow viewports per shell_layout.rs)
			if layout.ChatDocked {
				if aiImmersive {
					ui.Column(c.Key("stage-ai-immersive")).Grow(1).MinWidth(0).FillHeight().
						Transition(blockui.SurfaceStageTransition()).Children(func() {
						w.ai.SetRenderWidth(layout.ChatWidth)
						w.ai.RenderPanel(c)
					})
				} else {
					ui.Row(c.Key("docked-ai-slot")).Shrink(0).FillHeight().
						Transition(blockui.DockedPanelTransition(true)).Children(func() {
						chatW := w.ai.PanelWidth()
						maxChat := ChatLimit(vpW, w.sidebarWidth, w.state.SidebarCollapsed, ChatMinW, ChatMaxW)
						if blockui.VerticalResizeHandle(c, "ai-panel-resize", &chatW, ChatMinW, maxChat, true) {
							w.ai.SyncPanelWidth(chatW)
							c.Invalidate()
						}
						w.ai.SetRenderWidth(layout.ChatWidth)
						w.ai.RenderPanel(c)
					})
				}
			}

			// Floating Sidebar Drawer + Scrim when narrow and explicitly toggled open (Note or Settings).
			if layout.SidebarDrawer && !aiImmersive {
				c.Key("sidebar-drawer-scrim")
				scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
					Background(pal.Scrim()).Transition(blockui.ScrimFadeTransition())
				blockui.Probe(scrim, "sidebar-drawer-scrim")
				if scrim.Clicked() {
					w.ToggleSidebar(vpW)
				}
				c.Key("sidebar-drawer-panel")
				drawer := blockui.Elevate(
					ui.Row(c).Absolute().Left(0).Top(0).Bottom(0).Width(w.sidebarWidth).
						Transition(blockui.FloatingDrawerTransition()),
					theme.ElevationDrawer,
				)
				drawer.Children(func() {
					if inSettings {
						w.modals.RenderSettingsSidebar(c, true, func() {
							w.mu.Lock()
							w.sidebarPinned = false
							w.mu.Unlock()
							w.RequestRedraw()
						})
					} else {
						w.renderSidebarColumn(c, stSnap, activeNB, notebooks, entries, activeTab, notice, true)
					}
				})
			}
		})

		// 3. Modal Overlays (Search, About, Feedback, Performance, Updater).
		w.modals.RenderModals(c)

		// 4. Visual Smoke HUD (click ripple + step banner when smoke mode is enabled).
		blockui.RenderSmokeHUD(c)
	})
}

func (w *Workspace) renderSidebarColumn(
	c *ui.Context,
	stSnap model.AppState,
	activeNB string,
	notebooks []string,
	entries []model.FileEntry,
	activeNote string,
	notice string,
	isDrawer bool,
) {
	RenderSidebar(c, SidebarProps{
		State: w.sidebarUI,
		Width: &w.sidebarWidth,
		OnWidthChanged: func(nw float32) {
			w.scheduleSaveSidebarWidth(ClampSidebarWidth(nw))
		},
		ActiveNotebook:  activeNB,
		Notebooks:       notebooks,
		Entries:         entries,
		ActiveNote:      activeNote,
		Notice:          notice,
		SettingsOpen:    w.modals.IsSettingsOpen(),
		ShortcutNewNote: modals.EffectiveShortcut(stSnap, "new_note"),
		OnNewFolder: func() {
			_, _ = w.CreateNewFolder()
		},
		OnNewNote: func() {
			_, _ = w.CreateNewNote()
		},
		OnOpenNote: func(path string, inNewTab bool) {
			if isDrawer {
				w.mu.Lock()
				w.sidebarPinned = false
				w.mu.Unlock()
			}
			_ = w.openNoteInternal(path, inNewTab, true, 0)
		},
		OnRenameItem: func(oldPath, newName string) {
			_, _ = w.RenameWorkspaceItem(oldPath, newName)
		},
		OnMoveItem: func(srcPath, dstDir string) {
			_, _ = w.MoveWorkspaceItem(srcPath, dstDir)
		},
		OnReorderSibling:   w.ReorderSibling,
		OnReorderNotebooks: w.ReorderNotebooks,
		OnTrashItem: func(path string) {
			_ = w.TrashWorkspaceItem(path)
		},
		OnRevealItem: func(path string) {
			if mygo.App.IsReady() {
				mygo.Shell.ShowItemInFolder(path)
			}
		},
		OnSwitchNotebook: func(root string) {
			_ = w.SetNotebook(root)
		},
		OnForgetNotebook: w.ForgetNotebook,
		OnChooseNotebook: w.ChooseNotebookDialog,
		OnOpenSettings: func() {
			w.OpenSettings("ai")
		},
		OnOpenUpdater: func() {
			w.modals.OpenUpdater()
		},
	})
}

func (w *Workspace) renderEmptyWorkspaceMascot(c *ui.Context, activeNB string, emptyFolder bool) {
	pal := theme.Ocean()
	c.Key("workspace-empty")
	box := ui.Column(c).Fill().Center().Gap(theme.Space16).Padding(theme.Space24).Background(pal.Paper)
	blockui.Probe(box, "workspace-empty")
	box.Children(func() {
		switch {
		case activeNB == "":
			icons.Mascot(c, icons.MascotNoFolder, 220)
			ui.Text(c, theme.Tr("打开一个文件夹开始记录", "Open a folder to start writing")).
				FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).FontWeight(theme.WeightTitle).TextColor(pal.Text)
			btn := blockui.ActionButton(c, "empty-choose-folder", theme.Tr("选择文件夹…", "Choose Folder…"), nil, blockui.ButtonPrimary, false)
			if btn.Clicked() {
				w.ChooseNotebookDialog()
			}
		case emptyFolder:
			icons.Mascot(c, icons.MascotEmptyFolder, 220)
			ui.Text(c, theme.Tr("这个笔记本还没有笔记", "This notebook has no notes yet")).
				FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).FontWeight(theme.WeightTitle).TextColor(pal.Text)
			ic := icons.NotePlus
			btn := blockui.ActionButton(c, "empty-new-note", theme.Tr("新建笔记", "New Note"), &ic, blockui.ButtonPrimary, false)
			if btn.Clicked() {
				_, _ = w.CreateNewNote()
			}
		default:
			icons.Mascot(c, icons.MascotNoNote, 220)
			ui.Text(c, theme.Tr("从左侧选择一篇笔记，或新建一篇", "Select a note on the left or create a new one")).
				FontSize(theme.FontEditor).FixedLineHeight(theme.LineEditor).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
			ic := icons.NotePlus
			btn := blockui.ActionButton(c, "empty-new-note", theme.Tr("新建笔记", "New Note"), &ic, blockui.ButtonPrimary, false)
			if btn.Clicked() {
				_, _ = w.CreateNewNote()
			}
		}
	})
}
