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
	"block/internal/feedback"
	"block/internal/files"
	"block/internal/icons"
	"block/internal/markdown"
	"block/internal/modals"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/tarot"
	"block/internal/telemetry"
	"block/internal/theme"
	blockui "block/internal/ui"
	"block/internal/updater"
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

	state                model.AppState
	sidebarWidth         float32
	sidebarPinned        bool
	lastViewportW        float32
	zoomPct              int
	entries              []model.FileEntry
	lastScanErr          string
	virtualPreview       map[string]bool   // createNote preview tabs not yet written to disk
	pluginInsertInFlight map[string]int    // tabs currently saving/rolling back a plugin EditorInsert
	autoSaveDeferred     map[string]bool   // tabs skipped by autosave while pluginInsertInFlight > 0
	diskWriteSeq         map[string]uint64 // monotonic per-path disk write sequence preventing stale save callbacks
	lastDiskMarkdown     map[string]string // last markdown actually written to disk per clean path (catches mid-save undo)
	headlessMainMu       sync.Mutex        // serializes headless editor render with pluginHostUI.onMainWait when !mygo.App.IsReady()
	ghostTitle           string            // title typed on the no-note ghost page
	noteSource           noteSourceView    // read-only Markdown source view of the active note

	codec    *markdown.Codec
	sessions map[string]*editor.Session
	surfaces map[string]*TabSurface
	nav      *NavHistory
	closed   *ClosedTabs
	tabOps   TabStripOps

	tabMenu    *TabMenuState
	sidebarUI  *SidebarUIState
	edgeFloat  EdgeFloatController
	extPathBar ExternalPathBarState

	exportCtrl   *export.Controller
	historyView  *PageHistoryView
	historyToast *HistoryToast

	// chromePopups holds sidebar/titlebar menu overlays queued during a frame;
	// they are flushed after the editor so menus stack above editor toolbars.
	chromePopups []func()

	recoveryPath    string
	recoveryContent string
	recoveryVersion *int64

	ai           *ai.Controller
	modals       *modals.Manager
	updater      *updater.Service
	pluginsUI    *pluginui.Controller
	pluginUI     *pluginHostUI
	palette      CommandPalette
	quick        *modals.QuickNoteController
	searchWin    *modals.SearchWindowController
	tarot        *tarot.Controller
	tarotSyncKey string

	// OnAppMenuOrHotkeysChanged is notified whenever Settings updates shortcuts or language
	// so main.go can live-refresh the native application menu bar and OS global hotkeys.
	OnAppMenuOrHotkeysChanged func(st model.AppState)
	// OnRevealMainWindow restores, shows, and focuses the main workspace window.
	OnRevealMainWindow func()
	// OnCloseMainWindow closes the main workspace window (hiding to background on macOS).
	OnCloseMainWindow func()

	watchCancel         context.CancelFunc
	persistWindowBounds bool
	mainBoundsTimer     *time.Timer
	autoSaveTimer       *time.Timer
	sidebarWidthTimer   *time.Timer
	pendingSidebarWidth float32
	hasPendingSidebarW  bool
	lastSnapAt          map[string]time.Time
	bgSaveMu            sync.Mutex
	saveGeneration      uint64
	selfWriteUntil      time.Time

	aiSelKey         aiSelectionSyncKey
	hasAISelKey      bool
	aiSelNote        string
	lastNotifiedNote string
}

type aiSelectionSyncKey struct {
	notePath string
	docRev   uint64
	anchor   editor.Position
	head     editor.Position
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
		dataDir:              actualDir,
		state:                st,
		sidebarWidth:         SidebarDefaultW,
		zoomPct:              zoomPctFromState(st.EditorZoom),
		persistWindowBounds:  true,
		virtualPreview:       make(map[string]bool),
		pluginInsertInFlight: make(map[string]int),
		autoSaveDeferred:     make(map[string]bool),
		diskWriteSeq:         make(map[string]uint64),
		lastDiskMarkdown:     make(map[string]string),
		codec:                codec,
		sessions:             make(map[string]*editor.Session),
		surfaces:             make(map[string]*TabSurface),
		nav:                  NewNavHistory(),
		closed:               NewClosedTabs(),
		tabMenu:              NewTabMenuState(),
		sidebarUI:            NewSidebarUIState(),
		historyView:          &PageHistoryView{Mode: "diff"},
		lastSnapAt:           make(map[string]time.Time),
	}
	if st.SidebarWidth != nil && *st.SidebarWidth > 0 {
		w.sidebarWidth = ClampSidebarWidth(*st.SidebarWidth)
	}
	if recPath, recContent, version, ok := files.LatestVersionedRecoveryDraft(); ok {
		w.recoveryPath = recPath
		w.recoveryContent = recContent
		w.recoveryVersion = version
	}

	w.exportCtrl = export.NewController(actualDir, w.RequestRedraw)
	w.ai = ai.NewController(actualDir, w, codec)
	w.ai.SetOnEnabledChanged(w.onAIEnabledChanged)
	w.ai.SetOnDismissFloating(func() {
		w.edgeFloat.DismissRightWithCooldown(w)
		w.RequestRedraw()
	})
	w.ai.SetOnRevealPath(w.revealInShell)
	w.ai.SetOnPickAttachments(w.pickAIAttachments)
	w.modals = modals.NewManager(actualDir, w)
	go feedback.CleanTemp() // compressed copies a previous run left behind
	w.updater = updater.NewService(updater.Config{
		DataDir:           actualDir,
		InitialSourceMode: updater.NormalizeSourceMode(st.UpdateSource),
		LastUpdatedFrom:   st.LastUpdatedFrom,
		IsEn:              theme.IsEn,
		OnSourceModeChanged: func(mode updater.SourceMode) {
			if next, err := files.UpdateState(func(s *model.AppState) {
				s.UpdateSource = string(mode)
			}); err == nil {
				w.mu.Lock()
				w.state = next
				w.mu.Unlock()
			}
		},
		FlushBeforeUpdate: func(fromVersion string) error {
			saveErr := errors.New("editor is unavailable")
			save := func() {
				w.flushPendingSidebarWidth()
				saveErr = w.flushDirtySessions()
				if saveErr != nil {
					return
				}
				if next, err := files.UpdateState(func(s *model.AppState) {
					if s.LastUpdatedFrom == "" || updater.CompareVersion(fromVersion, s.LastUpdatedFrom) > 0 {
						s.LastUpdatedFrom = fromVersion
					}
				}); err == nil {
					w.mu.Lock()
					w.state = next
					w.mu.Unlock()
				}
				w.FlushWindowBounds()
				if w.quick != nil {
					saveErr = w.quick.Flush()
				}
			}
			if mygo.App.IsReady() {
				mygo.RunOnMain(save)
			} else {
				save()
			}
			return saveErr
		},
		QuitApp: quitForUpdate,
		OnChange: func() {
			w.RequestRedraw()
		},
	})
	w.modals.Updater = w.updater
	if updater.IsPackagedBuild() && !blockui.IsSmokeActive() {
		telemetry.Start(telemetry.Config{
			DataDir: actualDir,
			Version: updater.CurrentVersion(),
			Enabled: func() bool { return !w.State().TelemetryOff },
			Lang:    func() string { return string(theme.Language()) },
		})
	}
	w.pluginUI = newPluginHostUI(w)
	w.pluginsUI = pluginui.NewController(plugins.NewSupervisor(plugins.SupervisorConfig{
		AppDataDir:  actualDir,
		HostVersion: updater.CurrentVersion(),
		GetNotebookRoot: func() string {
			return w.State().Folder
		},
		GetNotebooks: func() []string {
			return w.Notebooks()
		},
		AddNotebook: w.RegisterNotebook,
		GetActiveNote: func() string {
			return w.State().ActiveTab
		},
		GetLocale: func() string {
			return string(theme.Language())
		},
		GetTheme: func() (string, bool) {
			mode := string(w.State().Theme)
			if mode == "" {
				mode = string(model.ThemeSystem)
			}
			return mode, theme.IsTarotMode()
		},
		GetPerfSnapshot: func(refreshDisk bool) (*plugins.PerfSnapshot, error) {
			snap := w.modals.PerfSnapshot(refreshDisk)
			cats := make([]plugins.PerfDiskCategory, len(snap.Categories))
			for i, c := range snap.Categories {
				entries := make([]plugins.PerfBreakdownEntry, len(c.Entries))
				for ei, ent := range c.Entries {
					entries[ei] = plugins.PerfBreakdownEntry{
						Name:  ent.Name,
						Path:  ent.Path,
						Bytes: ent.Bytes,
						Count: ent.Count,
					}
				}
				cats[i] = plugins.PerfDiskCategory{
					ID:        c.ID,
					Label:     c.Label,
					Detail:    c.Detail,
					Bytes:     c.Bytes,
					FileCount: c.FileCount,
					Entries:   entries,
				}
			}
			return &plugins.PerfSnapshot{
				CPUPercent: snap.CPUPercent,
				RSSBytes:   snap.RSSBytes,
				TotalBytes: snap.TotalBytes,
				LastScan:   snap.LastScan,
				ScanError:  snap.ScanError,
				Categories: cats,
			}, nil
		},
		PickDirectory: func(title, defaultPath string, onPicked func(path string)) {
			if !mygo.App.IsReady() {
				return
			}
			win := w.Window()
			go func() {
				paths, err := files.OpenDialog(win, mygo.OpenDialogOptions{
					Title:             title,
					DefaultPath:       defaultPath,
					Directory:         true,
					CreateDirectories: true,
				})
				if err == nil && len(paths) > 0 && paths[0] != "" {
					onPicked(paths[0])
				}
			}()
		},
		PickFile: func(title, defaultPath string, extensions []string, onPicked func(path string)) {
			if !mygo.App.IsReady() {
				return
			}
			win := w.Window()
			exts := append([]string(nil), extensions...)
			go func() {
				opts := mygo.OpenDialogOptions{
					Title:       title,
					DefaultPath: defaultPath,
				}
				if len(exts) > 0 {
					opts.Filters = []mygo.FileFilter{{Name: title, Extensions: exts}}
				}
				paths, err := files.OpenDialog(win, opts)
				if err == nil && len(paths) > 0 && paths[0] != "" {
					onPicked(paths[0])
				}
			}()
		},
		ApplyNoteWrite: w.applyPluginNoteWrite,
		OnChanged: func() {
			w.syncTarotWithPluginState(false)
			w.RequestRedraw()
		},
		OnRedraw: w.RequestRedraw,
		UI:       w.pluginUI,
		System:   w.pluginUI,
	}))
	w.pluginsUI.SetOnSuggestPlugin(func(_ plugins.Category) {
		w.modals.OpenFeedbackWithCategory("idea")
	})
	w.quick = modals.NewQuickNoteController(actualDir, w, codec)
	w.searchWin = modals.NewSearchWindowController(w)

	w.wireModalCallbacks()
	w.wireQuickNoteCallbacks()
	w.wireSearchWindowCallbacks()
	w.initTarot()
	w.hydrateInitialWorkspace()
	w.syncTarotWithPluginState(true)
	if sup := w.Plugins(); sup != nil {
		sup.WarmupAppearancePluginsAsync()
	}
	w.lastNotifiedNote = w.State().ActiveTab
	return w
}

// Start the installer only once MyGo has completed the cancellable quit checks.
func quitForUpdate(startInstaller func() error) error {
	if !mygo.App.IsReady() {
		return errors.New("application is unavailable")
	}
	result := make(chan error, 1)
	queued := false
	mygo.RunOnMain(func() {
		var offQuit, offCancel func()
		finish := func(err error) {
			offQuit()
			offCancel()
			result <- err
		}
		offQuit = mygo.App.OnQuit(func() { finish(startInstaller()) })
		offCancel = mygo.App.OnQuitCanceled(func() {
			finish(errors.New(theme.Tr("应用未退出，已取消安装更新", "The application did not quit. Update installation was canceled.")))
		})
		queued = true
		mygo.App.Quit()
	})
	if !queued {
		return errors.New("application is unavailable")
	}
	return <-result
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
	sess.SetAIEnabled(w.ai.Enabled())
	sess.SetInlineAIStreamResponder(w.inlineAIStreamRespond)
	sess.SetAnnotationAIResponder(w.annotationAIRespond)
	sess.OnPickImages = func(blockID model.BlockID, replace bool) {
		w.pickImagesForSession(sess, blockID, replace)
	}
	sess.ExtraContextItems = func() []editor.ContextMenuExtra {
		sup := w.Plugins()
		if sup == nil {
			return nil
		}
		items := sup.EditorMenuItems()
		if len(items) == 0 {
			return nil
		}
		out := make([]editor.ContextMenuExtra, 0, len(items))
		pal := theme.Ocean()
		iconCols := pal.DuoIconColors(pal.Text)
		for _, it := range items {
			item := it
			var menuIcon ui.MenuIcon
			if len(item.Icon.StrokePaths) > 0 {
				menuIcon = pluginui.BuildMenuIcon(item.Icon, iconCols)
			}
			out = append(out, editor.ContextMenuExtra{
				Label: item.Title,
				Icon:  menuIcon,
				OnChoose: func() {
					sup.RunCommandAsync(item.PluginID, item.Command, plugins.CommandContext{
						Note:      sup.ActiveNoteRel(),
						File:      w.ActiveNotePath(),
						Selection: sess.SelectedMarkdown(),
						Source:    "editor_menu",
					})
				},
			})
		}
		return out
	}
	return sess
}

func (w *Workspace) pickImagesForSession(sess *editor.Session, blockID model.BlockID, replace bool) {
	if !mygo.App.IsReady() || sess == nil {
		return
	}
	win := w.Window()
	go func() {
		paths, err := files.OpenDialog(win, mygo.OpenDialogOptions{
			Title:    theme.Tr("上传图片", "Upload Image"),
			Multiple: true,
			Filters: []mygo.FileFilter{
				{
					Name:       theme.Tr("图片", "Images"),
					Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif"},
				},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		mygo.RunOnMain(func() {
			if sess.ImportImageFiles(paths, blockID, replace) > 0 {
				w.flushOrScheduleDirtySessions()
				if win != nil {
					win.Focus()
				}
				w.RequestRedraw()
			}
		})
	}()
}

func (w *Workspace) pickAIAttachments() {
	if !mygo.App.IsReady() || w.ai == nil {
		return
	}
	win := w.Window()
	go func() {
		paths, err := files.OpenDialog(win, mygo.OpenDialogOptions{
			Title:    theme.Tr("添加附件", "Add Attachments"),
			Multiple: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		mygo.RunOnMain(func() {
			w.ai.AddAttachmentFiles(paths)
			if win != nil {
				win.Focus()
			}
			w.RequestRedraw()
		})
	}()
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
	if !w.ai.Enabled() {
		return "", errors.New(theme.Tr("AI 助手已关闭", "AI Assistant is off"))
	}
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

func (w *Workspace) annotationAIRespond(docMarkdown string, pending []*model.Annotation) map[string]model.AnnotationReply {
	out := make(map[string]model.AnnotationReply, len(pending))
	if len(pending) == 0 || !w.ai.Enabled() {
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
		sysPrompt := `You handle margin notes on a Markdown document. For each note decide its intent:
- "edit": the note asks to change the anchored text (rewrite, shorten, translate, fix, reformat). "text" is the full replacement for anchor_text only.
- "answer": the note asks a question or wants an opinion, explanation or check, without asking for a change. "text" is a short, direct answer in the note's language; never a rewritten version of the text.
When unsure, prefer "answer". Return ONLY valid JSON: [{"id":"...","kind":"edit"|"answer","text":"..."}]. No markdown fences or extra prose.`
		userPrompt := fmt.Sprintf("Document Markdown:\n%s\n\nNotes:\n%s", docMarkdown, string(payload))
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		raw, err := w.ai.CompletePrompt(ctx, sysPrompt, userPrompt)
		cancel()
		if err == nil && strings.TrimSpace(raw) != "" {
			clean := stripOuterMarkdownFence(raw)
			type respItem struct {
				ID          string `json:"id"`
				Kind        string `json:"kind"`
				Text        string `json:"text"`
				Replacement string `json:"replacement"`
			}
			var parsed []respItem
			if json.Unmarshal([]byte(clean), &parsed) == nil {
				for _, it := range parsed {
					text := it.Text
					if strings.TrimSpace(text) == "" {
						text = it.Replacement
					}
					if strings.TrimSpace(it.ID) == "" || strings.TrimSpace(text) == "" {
						continue
					}
					kind := "edit"
					if strings.EqualFold(strings.TrimSpace(it.Kind), model.AnnotationReplyAnswer) {
						kind = model.AnnotationReplyAnswer
					}
					out[it.ID] = model.AnnotationReply{Kind: kind, Text: text}
				}
			}
		}
	}

	for _, a := range pending {
		if a == nil || strings.TrimSpace(out[a.ID].Text) != "" {
			continue
		}
		anchor := strings.TrimSpace(a.QuotedText)
		note := strings.TrimSpace(a.Note)
		noteLower := strings.ToLower(note)
		edit := func(text string) { out[a.ID] = model.AnnotationReply{Kind: "edit", Text: text} }
		switch {
		case looksLikeQuestion(note):
			out[a.ID] = model.AnnotationReply{Kind: model.AnnotationReplyAnswer, Text: theme.Tr("AI 暂时连不上，稍后再发一次试试。", "AI is unavailable right now. Try sending again later.")}
		case strings.Contains(note, "翻译") || strings.Contains(noteLower, "translate"):
			edit(fmt.Sprintf("Translated: %s", anchor))
		case strings.Contains(note, "加粗") || strings.Contains(noteLower, "bold"):
			edit(fmt.Sprintf("**%s**", anchor))
		case strings.Contains(note, "精简") || strings.Contains(noteLower, "concise") || strings.Contains(noteLower, "shorten"):
			runes := []rune(anchor)
			if len(runes) > 16 {
				edit(string(runes[:16]))
			} else {
				edit(strings.TrimSuffix(strings.TrimSuffix(anchor, "。"), "."))
			}
		default:
			edit(fmt.Sprintf("%s（%s）", anchor, note))
		}
	}
	return out
}

// looksLikeQuestion is the offline guess for notes that ask rather than request an edit.
func looksLikeQuestion(note string) bool {
	n := strings.TrimSpace(note)
	if n == "" {
		return false
	}
	if strings.HasSuffix(n, "?") || strings.HasSuffix(n, "？") || strings.HasSuffix(n, "吗") || strings.HasSuffix(n, "呢") {
		return true
	}
	for _, p := range []string{"为什么", "什么", "怎么", "是否", "是不是", "能不能", "你觉得", "如何", "why", "what", "how", "is ", "does ", "should "} {
		if strings.HasPrefix(strings.ToLower(n), p) {
			return true
		}
	}
	return false
}

func (w *Workspace) wireQuickNoteCallbacks() {
	if w.quick == nil {
		return
	}
	w.quick.SetAIEnabled(w.ai.Enabled())
	w.quick.SetInlineAIStreamResponder(func(ctx context.Context, ctxText, orig, draft, instruction string, _ func(string)) (string, error) {
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := w.inlineAIStreamRespond(requestCtx, ctxText, orig, draft, instruction, nil)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !w.ai.Enabled() {
			return "", errors.New(theme.Tr("AI 助手已关闭", "AI Assistant is off"))
		}
		if err == nil && strings.TrimSpace(out) != "" {
			return out, nil
		}
		return w.inlineAIOfflineFallback(ctxText, orig, draft, instruction), nil
	})
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
		w.mu.RLock()
		prevState := w.state
		prevScan := w.scanOptionsLocked()
		w.mu.RUnlock()

		next, err := files.UpdateState(mut)
		var nextScan files.ScanOptions
		if err != nil {
			files.LogWarnf("UpdateState failed: %v", err)
			w.mu.Lock()
			mut(&w.state)
			next = w.state
			nextScan = w.scanOptionsLocked()
			w.mu.Unlock()
		} else {
			w.mu.Lock()
			w.state = next
			nextScan = w.scanOptionsLocked()
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
		if prevState.Folder != next.Folder || !scanOptionsEqual(prevScan, nextScan) {
			w.RescanNotebook()
		}
		if sup := w.Plugins(); sup != nil {
			if prevState.Theme != next.Theme {
				mode := string(next.Theme)
				if mode == "" {
					mode = string(model.ThemeSystem)
				}
				sup.EmitWorkspaceEvent("theme.changed", map[string]any{"theme": mode})
			}
			if prevState.Language != next.Language {
				sup.EmitWorkspaceEvent("locale.changed", map[string]any{"locale": string(theme.Language())})
			}
		}
		if prevState.Language != next.Language ||
			prevState.ShortcutSearch != next.ShortcutSearch ||
			prevState.ShortcutQuick != next.ShortcutQuick ||
			!shortcutsMapEqual(prevState.CustomShortcuts, next.CustomShortcuts) {
			if w.OnAppMenuOrHotkeysChanged != nil {
				w.OnAppMenuOrHotkeysChanged(next)
			}
		}
		if w.searchWin != nil {
			w.searchWin.Invalidate()
		}
		if w.quick != nil {
			w.quick.RefreshStyle()
		}
		w.RequestRedraw()
	}
	w.modals.AIEnabled = w.ai.Enabled
	w.modals.RenderAISettings = func(c *ui.Context) {
		w.ai.RenderSettingsTab(c)
	}
	w.modals.PluginHost = w.pluginsUI
	w.modals.SidebarWidth = &w.sidebarWidth
	w.modals.OnSidebarWidthChanged = func(nw float32) {
		clamped := ClampSidebarWidth(nw)
		_, _ = files.UpdateState(func(st *model.AppState) {
			st.SidebarWidth = &clamped
		})
	}
	w.modals.OnForgetNotebook = w.ForgetNotebook
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
	w.modals.FeedbackNoticeFn = func(message, tone string) {
		w.notify(message, tone)
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
	hideNonMD := true
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
		SortMode:        w.state.TreeSort,
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

func shortcutsMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func scanOptionsEqual(a, b files.ScanOptions) bool {
	if a.HideNonMarkdown != b.HideNonMarkdown ||
		a.HideAssets != b.HideAssets ||
		a.AssetsFolder != b.AssetsFolder ||
		a.SortMode != b.SortMode ||
		len(a.FileOrder) != len(b.FileOrder) {
		return false
	}
	for k, listA := range a.FileOrder {
		listB, ok := b.FileOrder[k]
		if !ok || !stringSlicesEqual(listA, listB) {
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
	if w.state.Folder != "" {
		notes, dirs := countWorkspaceEntries(w.Entries())
		files.LogInfof("workspace opened: folder=%s notes=%d dirs=%d tabs=%d", w.state.Folder, notes, dirs, len(validTabs))
	}
}

func countWorkspaceEntries(entries []model.FileEntry) (notes, dirs int) {
	for _, entry := range entries {
		if entry.IsDir {
			dirs++
			n, d := countWorkspaceEntries(entry.Children)
			notes += n
			dirs += d
			continue
		}
		notes++
	}
	return notes, dirs
}

func openKindName(kind model.OpenKind) string {
	switch kind {
	case model.OpenKindMarkdown:
		return "markdown"
	case model.OpenKindSource:
		return "source"
	case model.OpenKindPreview:
		return "preview"
	case model.OpenKindBlocked:
		return "blocked"
	default:
		return "unknown"
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
		win.OnFileDrop(w.handleWindowFileDrop)
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
	path, err := w.applyRecoveryDraft()
	if path != "" && err == nil {
		_ = w.OpenNote(path, 0)
	}
	return path, err
}

func (w *Workspace) applyRecoveryDraft() (string, error) {
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	w.mu.RLock()
	p, content, version := w.recoveryPath, w.recoveryContent, w.recoveryVersion
	w.mu.RUnlock()
	if p == "" {
		return "", nil
	}
	if version == nil {
		return p, errors.New(theme.Tr("此恢复草稿缺少原始版本，请保留草稿并手动合并", "The recovery draft needs to be merged manually"))
	}
	if err := files.CheckTextVersion(p, *version); err != nil {
		return p, err
	}
	if err := w.writeNoteLocked(p, content); err != nil {
		return p, err
	}
	w.mu.Lock()
	w.recoveryPath, w.recoveryContent, w.recoveryVersion = "", "", nil
	w.mu.Unlock()
	return p, nil
}

// DismissPendingRecoveryDraft discards the startup crash-recovery draft.
func (w *Workspace) DismissPendingRecoveryDraft() {
	w.mu.Lock()
	p := w.recoveryPath
	w.recoveryPath = ""
	w.recoveryContent = ""
	w.recoveryVersion = nil
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
		files.LogErrorf("note read failed: path=%s err=%v", clean, err)
		return err
	}
	w.installClassifiedTab(clean, cf)
	return nil
}

func (w *Workspace) installClassifiedTab(clean string, cf model.ClassifiedFile) {
	files.LogInfof("note opened: path=%s bytes=%d kind=%s", clean, cf.RawSize, openKindName(cf.Kind))
	if cf.Kind == model.OpenKindMarkdown {
		doc := w.codec.Parse(cf.Text)
		sess := w.newEditorSession(clean, doc)
		sess.SetDiskFingerprint(files.TextFingerprint(cf.Text, false))
		w.mu.Lock()
		delete(w.surfaces, clean)
		w.sessions[clean] = sess
		w.mu.Unlock()
		return
	}

	surf := NewTabSurface(clean, cf)
	surf.OnRedraw = w.RequestRedraw
	w.mu.Lock()
	if oldSess := w.sessions[clean]; oldSess != nil {
		delete(w.sessions, clean)
		delete(w.lastDiskMarkdown, clean)
		defer oldSess.Close()
	}
	w.surfaces[clean] = surf
	w.mu.Unlock()
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
	wholeDoc, selectionDoc := sess.Doc(), sess.SelectedDoc()
	w.FlushDirtySessions()
	if selectionDoc == nil && !sess.Dirty() {
		if diskText, _, err := files.ReadNote(active); err == nil && diskText != sess.Markdown() {
			wholeDoc = w.codec.Parse(diskText)
		}
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.edgeFloat.DismissAll(w)
	w.modals.CloseSettings()
	w.historyView.Close()

	title := noteDisplayTitle(active, false)
	w.exportCtrl.OpenWithSelection(title, active, wholeDoc, selectionDoc)
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
	if err := w.FlushDirtySessions(); err != nil {
		return false
	}
	curText, fingerprint, err := files.ReadNote(active)
	if err != nil {
		w.setNotice(err.Error())
		return false
	}
	if fingerprint != sess.DiskFingerprint() {
		sess.SetDiskFingerprint(fingerprint)
		sess.SetDoc(w.codec.Parse(curText))
		sess.MarkSaved()
		w.mu.Lock()
		w.bumpDiskWriteSeqLocked(active)
		delete(w.lastDiskMarkdown, filepath.Clean(active))
		w.mu.Unlock()
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.edgeFloat.DismissAll(w)
	w.modals.CloseSettings()
	w.exportCtrl.Close()

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
	if err != nil {
		files.LogErrorf("note read failed: path=%s err=%v", clean, err)
	}
	return content, err
}

func (w *Workspace) WriteNote(absPath string, content string) error {
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	return w.writeNoteLocked(absPath, content)
}

func (w *Workspace) writeNoteLocked(absPath, content string) error {
	clean := filepath.Clean(absPath)
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		files.LogErrorf("note save failed: path=%s bytes=%d err=%v", clean, len(content), err)
		return err
	}
	if !files.IsMarkdown(clean) {
		w.mu.RLock()
		existingSurf := w.surfaces[clean]
		w.mu.RUnlock()
		binaryPlist := (existingSurf != nil && existingSurf.Source.BinaryPlist) || (existingSurf == nil && files.IsBinaryPlistFile(clean))
		if binaryPlist {
			if _, _, err := files.WriteBinaryPlistAtomic(clean, content); err != nil {
				files.LogErrorf("note save failed: path=%s bytes=%d err=%v", clean, len(content), err)
				return err
			}
		} else {
			bom := existingSurf != nil && existingSurf.Source.BOM
			if _, err := files.WriteRawFileAtomic(clean, content, bom); err != nil {
				files.LogErrorf("note save failed: path=%s bytes=%d err=%v", clean, len(content), err)
				return err
			}
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

	fingerprint, err := files.WriteNoteAtomic(clean, content)
	if err != nil {
		files.LogErrorf("note save failed: path=%s bytes=%d err=%v", clean, len(content), err)
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
	w.sessions[clean].SetDiskFingerprint(fingerprint)
	w.bumpDiskWriteSeqLocked(clean)
	delete(w.lastDiskMarkdown, clean)
	w.mu.Unlock()

	w.RescanNotebook()
	w.notifyPluginNoteSaved(clean)
	w.RequestRedraw()
	return nil
}

// Plugins returns the decoupled plugin Supervisor, or nil if uninitialized.
func (w *Workspace) Plugins() *plugins.Supervisor {
	if w.pluginsUI == nil {
		return nil
	}
	return w.pluginsUI.Supervisor()
}

func (w *Workspace) notifyPluginNoteSaved(absPath string) {
	if w.tarot != nil && theme.IsTarotMode() {
		w.tarot.OnNoteSaved(absPath)
	}
	if sup := w.Plugins(); sup != nil {
		source := ""
		if w.pluginUI != nil {
			source = w.pluginUI.takeInsertSource(absPath)
		}
		sup.NotifyNoteSavedFrom(source, absPath)
	}
}

// openSessionPathLocked maps a plugin-supplied path to the key of an open
// session for the same file. On case-insensitive volumes "Notes/A.md" and
// "notes/a.md" are one file, and the dirty check must see the open tab.
// Caller holds w.mu.
func (w *Workspace) openSessionPathLocked(absPath string) string {
	if _, ok := w.sessions[absPath]; ok {
		return absPath
	}
	var target os.FileInfo
	for key := range w.sessions {
		if !strings.EqualFold(key, absPath) {
			continue
		}
		if target == nil {
			fi, err := os.Stat(absPath)
			if err != nil {
				return absPath
			}
			target = fi
		}
		if fi, err := os.Stat(key); err == nil && os.SameFile(fi, target) {
			return key
		}
	}
	return absPath
}

func (w *Workspace) applyPluginNoteWrite(absPath, content string, write func() (bool, error)) (bool, error) {
	apply := func() (bool, error) {
		w.bgSaveMu.Lock()
		defer w.bgSaveMu.Unlock()
		w.mu.RLock()
		absPath = w.openSessionPathLocked(absPath)
		sess := w.sessions[absPath]
		isVirtual := w.virtualPreview[absPath]
		lastDiskMD, hasLastDisk := w.lastDiskMarkdown[filepath.Clean(absPath)]
		w.mu.RUnlock()
		if sess != nil && (isVirtual || sess.Dirty() || (hasLastDisk && sess.Markdown() != lastDiskMD)) {
			return false, plugins.ErrNoteConflict
		}
		changed, err := write()
		if err != nil || !changed {
			return changed, err
		}
		if sess != nil {
			doc := w.codec.Parse(content)
			w.mu.Lock()
			delete(w.surfaces, absPath)
			sess.SetDiskFingerprint(files.TextFingerprint(content, false))
			sess.SetDoc(doc)
			sess.MarkSaved()
			w.bumpDiskWriteSeqLocked(absPath)
			delete(w.lastDiskMarkdown, filepath.Clean(absPath))
			w.mu.Unlock()
		}
		return true, nil
	}
	var changed bool
	err := errors.New("editor is unavailable for plugin writes")
	if mygo.App.IsReady() {
		mygo.RunOnMain(func() { changed, err = apply() })
	} else {
		changed, err = apply()
	}
	if changed {
		_, _ = files.RecordHistorySnapshot(absPath, content, theme.Tr("插件保存", "Plugin save"))
		w.RescanNotebook()
		w.RequestRedraw()
	}
	return changed, err
}

func (w *Workspace) OpenNote(absPath string, lineHint int) error {
	return w.openNoteInternal(absPath, true, true, lineHint)
}

// openRootValidatedNote opens a note already verified and read through an
// os.Root handle by a plugin runtime, re-checking runtime/epoch liveness,
// notebook registration, directory identity, and full Markdown content
// classification on the main thread without re-opening the path through
// symlink-following filesystem calls.
func (w *Workspace) openRootValidatedNote(notebookRoot, relPath string, rootInfo os.FileInfo, content []byte, checkValid func() error) error {
	if checkValid != nil {
		if err := checkValid(); err != nil {
			return err
		}
	}
	rootAbs, err := filepath.Abs(strings.TrimSpace(notebookRoot))
	if err != nil {
		return err
	}
	registered := false
	for _, nb := range w.Notebooks() {
		if nbAbs, err := filepath.Abs(nb); err == nil && nbAbs == rootAbs {
			registered = true
			break
		}
	}
	if !registered {
		return errors.New("notebook is no longer registered")
	}
	if rootInfo != nil {
		curInfo, statErr := os.Stat(rootAbs)
		if statErr != nil || !os.SameFile(curInfo, rootInfo) {
			return errors.New("notebook directory changed while opening")
		}
	}
	if relPath == "" || strings.HasPrefix(relPath, "/") || strings.HasSuffix(relPath, "/") ||
		strings.ContainsAny(relPath, "\\:\x00\r\n") {
		return errors.New("invalid relative note path")
	}
	for _, seg := range strings.Split(relPath, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errors.New("invalid relative note path")
		}
	}
	// Validate target file bytes BEFORE switching notebooks or reusing cached tabs
	// so invalid UTF-8 / oversized / non-Markdown files never mutate workspace state.
	clean := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(relPath)))
	cf := files.ClassifyBytes(clean, content)
	if cf.Kind != model.OpenKindMarkdown {
		if cf.Reason != "" {
			return errors.New(cf.Reason)
		}
		return errors.New("target file cannot be opened as Markdown")
	}
	curRoot := strings.TrimSpace(w.State().Folder)
	if curRoot != "" {
		if curAbs, err := filepath.Abs(curRoot); err == nil {
			curRoot = curAbs
		}
	}
	if curRoot != rootAbs {
		if err := w.SetNotebook(rootAbs); err != nil {
			return err
		}
		if rootInfo != nil {
			curInfo, statErr := os.Stat(rootAbs)
			if statErr != nil || !os.SameFile(curInfo, rootInfo) {
				return errors.New("notebook directory changed while switching")
			}
		}
	}
	w.mu.RLock()
	_, hasSess := w.sessions[clean]
	surf := w.surfaces[clean]
	hasDirtySurf := surf != nil && surf.IsDirty()
	w.mu.RUnlock()
	if !hasSess && !hasDirtySurf {
		doc := w.codec.Parse(cf.Text)
		sess := w.newEditorSession(clean, doc)
		sess.SetDiskFingerprint(files.TextFingerprint(cf.Text, false))
		w.mu.Lock()
		if _, exists := w.sessions[clean]; exists {
			w.mu.Unlock()
			sess.Close()
		} else if existingSurf := w.surfaces[clean]; existingSurf != nil && existingSurf.IsDirty() {
			w.mu.Unlock()
			sess.Close()
		} else {
			delete(w.surfaces, clean)
			w.sessions[clean] = sess
			w.mu.Unlock()
		}
	}
	return w.openNoteInternal(clean, false, true, 0)
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
	w.edgeFloat.DismissAll(w)
	w.sidebarUI.FocusDir = ""

	if !forceNewTab {
		w.mu.RLock()
		replaceCandidate := ""
		if w.state.ActiveTab != "" && w.state.ActiveTab != clean && !slices.Contains(w.state.OpenTabs, clean) {
			replaceCandidate = w.state.ActiveTab
		}
		w.mu.RUnlock()
		if replaceCandidate != "" {
			_ = w.flushSingleSession(replaceCandidate)
		}
	}

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

	var evictedSess *editor.Session
	if prevActive != "" && prevActive != clean && !slices.Contains(w.state.OpenTabs, prevActive) {
		prevClean := filepath.Clean(prevActive)
		inFlight := w.pluginInsertInFlight[prevActive] > 0 || w.pluginInsertInFlight[prevClean] > 0
		lastDiskMD, hasLastDisk := w.lastDiskMarkdown[prevClean]
		if s := w.sessions[prevActive]; s != nil && !inFlight && !s.Dirty() && (!hasLastDisk || s.Markdown() == lastDiskMD) {
			evictedSess = s
			delete(w.sessions, prevActive)
			delete(w.virtualPreview, prevActive)
			delete(w.lastSnapAt, prevActive)
			delete(w.lastDiskMarkdown, prevClean)
		}
		if sf := w.surfaces[prevActive]; sf != nil && !sf.IsDirty() {
			delete(w.surfaces, prevActive)
		}
	}

	var trimList []*editor.Session
	if w.state.ActiveTab != prevActive && len(w.sessions) > 3 {
		for k, s := range w.sessions {
			if k != w.state.ActiveTab && s != nil {
				trimList = append(trimList, s)
			}
		}
	}

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

	if evictedSess != nil {
		evictedSess.Close()
	}
	for _, ts := range trimList {
		ts.TrimInactiveCaches()
	}

	if recordNav {
		w.nav.Push(clean)
	}
	if sess != nil && active != prevActive {
		sess.ClearFocus()
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

func (w *Workspace) aiPlanEditorPaths(target string) []string {
	w.mu.RLock()
	openPaths := make([]string, 0, len(w.sessions))
	for path := range w.sessions {
		openPaths = append(openPaths, path)
	}
	w.mu.RUnlock()
	// A tab may use a different alias for the same saved note, or for its parent before creation.
	parent, _ := filepath.EvalSymlinks(filepath.Dir(target))
	canonical, _ := filepath.EvalSymlinks(target)
	var paths []string
	for _, path := range openPaths {
		dir, err := filepath.EvalSymlinks(filepath.Dir(path))
		resolved, _ := filepath.EvalSymlinks(path)
		if path == target || (err == nil && dir == parent && filepath.Base(path) == filepath.Base(target)) ||
			(canonical != "" && canonical == resolved) {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return []string{target}
	}
	slices.Sort(paths)
	return paths
}

func (w *Workspace) PreviewPlanInEditor(plan *model.BatchPlan) error {
	return w.PreviewBatchPlanContext(context.Background(), plan)
}

func (w *Workspace) PreviewBatchPlanContext(ctx context.Context, plan *model.BatchPlan) error {
	target, original, exists, err := ai.ReadPlanPreview(ctx, w, plan)
	if err != nil {
		return err
	}
	preview := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !w.ai.Enabled() {
			return errors.New(theme.Tr("AI 助手已关闭", "AI Assistant is off"))
		}
		paths := w.aiPlanEditorPaths(target)
		for _, path := range paths {
			if sess := w.SessionFor(path); sess != nil && sess.Dirty() {
				return errors.New(theme.Tr("目标笔记有未保存的编辑。", "The target has unsaved edits."))
			}
		}
		target := paths[0]
		sess := w.SessionFor(target)
		if !exists {
			sess = w.newEditorSession(target, w.codec.Parse(plan.Ops[0].New))
			sess.SetActivePlan(plan)
			w.mu.Lock()
			w.virtualPreview[target] = true
			delete(w.surfaces, target)
			w.sessions[target] = sess
			w.state.OpenTabs, w.state.ActiveTab = w.tabOps.OpenOrFocus(w.state.OpenTabs, w.state.ActiveTab, target)
			w.mu.Unlock()
		} else {
			if sess == nil {
				sess = w.newEditorSession(target, w.codec.Parse(original))
				sess.SetDiskFingerprint(files.TextFingerprint(original, false))
				w.mu.Lock()
				delete(w.surfaces, target)
				w.sessions[target] = sess
				w.mu.Unlock()
			}
			if err := w.OpenNote(target, 0); err != nil {
				return err
			}
			sess.SetActivePlan(plan)
		}
		w.RequestRedraw()
		return nil
	}
	if !mygo.App.IsReady() {
		return preview()
	}
	err = errors.New("editor is unavailable for AI preview")
	mygo.RunOnMain(func() { err = preview() })
	return err
}

func (w *Workspace) AcceptEditorBatchPlan(plan *model.BatchPlan) error {
	return w.ai.AcceptEditorPlan(plan)
}

func (w *Workspace) ApplyBatchPlan(plan *model.BatchPlan) error {
	return w.executeAIPlan(plan, false)
}

func (w *Workspace) executeAIPlan(plan *model.BatchPlan, undo bool) error {
	if plan == nil || !w.ai.Enabled() {
		return errors.New(theme.Tr("AI 修改计划已失效", "The AI plan is unavailable"))
	}
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()

	var targets [][]string
	for _, op := range plan.Ops {
		target, _, err := ai.LocatePlanNote(w, plan.Root, op.Page)
		if err != nil {
			return err
		}
		paths := w.aiPlanEditorPaths(target)
		for _, path := range paths {
			if sess := w.SessionFor(path); sess != nil && sess.Dirty() {
				err := errors.New(theme.Tr("目标笔记有未保存的编辑，请先处理后重试。", "The target has unsaved edits. Save them before retrying."))
				w.setNotice(err.Error())
				return err
			}
		}
		targets = append(targets, paths)
	}
	if err := ai.ApplyPlanFiles(w, plan, undo); err != nil {
		w.setNotice(err.Error())
		return err
	}
	w.markSelfWriteWindow(2 * time.Second)
	w.mu.Lock()
	w.saveGeneration++
	w.mu.Unlock()

	var firstCreated string
	for i, op := range plan.Ops {
		paths := targets[i]
		if undo && op.Kind == model.PlanOpCreateNote {
			for _, target := range paths {
				w.removeClosedOrDeletedPath(target)
			}
			continue
		}
		content := op.New
		if undo {
			content = op.Old
		}
		_, _ = files.RecordHistorySnapshot(paths[0], content, "AI BatchPlan")
		for _, target := range paths {
			doc := w.codec.Parse(content)
			w.mu.Lock()
			delete(w.virtualPreview, target)
			delete(w.surfaces, target)
			if sess := w.sessions[target]; sess != nil {
				sess.SetActivePlan(nil)
				sess.SetDiskFingerprint(files.TextFingerprint(content, false))
				sess.SetDoc(doc)
				sess.MarkSaved()
				w.bumpDiskWriteSeqLocked(target)
				delete(w.lastDiskMarkdown, filepath.Clean(target))
			}
			w.mu.Unlock()
		}
		if !undo && firstCreated == "" && op.Kind == model.PlanOpCreateNote {
			firstCreated = paths[0]
		}
	}
	if firstCreated != "" {
		_ = w.OpenNote(firstCreated, 0)
	}
	w.RescanNotebook()
	w.RequestRedraw()
	return nil
}

func (w *Workspace) RejectBatchPlan(planID string) error {
	w.ai.DismissEditorPlan(planID)
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
		delete(w.lastDiskMarkdown, filepath.Clean(drop))
		w.state.OpenTabs, w.state.ActiveTab, _ = w.tabOps.CloseTab(w.state.OpenTabs, w.state.ActiveTab, drop)
	}
	active := w.state.ActiveTab
	w.mu.Unlock()
	w.emitNoteOpenedIfChanged(active)
	w.RequestRedraw()
	return nil
}

func (w *Workspace) UndoLastPlan(plan *model.BatchPlan) error {
	return w.executeAIPlan(plan, true)
}

func (w *Workspace) OpenPluginStoreSource(source string) {
	w.OpenSettings(modals.TabAppStore)
	if w.pluginsUI != nil {
		w.pluginsUI.OpenStoreSource(source)
	}
}

func (w *Workspace) OpenSettings(tab string) {
	wasOpen := w.modals.IsSettingsOpen()
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	if w.ai != nil {
		w.ai.CloseTransientPopups()
	}
	w.edgeFloat.DismissAll(w)
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

// InvalidateAllWindows marks the main workspace, Quick Note, and standalone search
// windows dirty (e.g. after background CDN font registration completes).
func (w *Workspace) InvalidateAllWindows() {
	w.RequestRedraw()
	if w.quick != nil {
		w.quick.Invalidate()
	}
	if w.searchWin != nil {
		w.searchWin.Invalidate()
	}
}

// --- Notebook, File, Reorder, and Tab Operations ---

// SetNotebook switches the active notebook folder (adding it to recent notebooks),
// scans its file tree, and persists state.json.
func (w *Workspace) SetNotebook(folder string) error {
	clean := filepath.Clean(folder)
	info, err := os.Stat(clean)
	if err != nil {
		files.LogErrorf("workspace switch failed: folder=%s err=%v", clean, err)
		return err
	}
	if !info.IsDir() {
		err := errors.New("notebook path is not a directory")
		files.LogErrorf("workspace switch failed: folder=%s err=%v", clean, err)
		return err
	}

	w.mu.RLock()
	prevFolder := ""
	if w.state.Folder != "" {
		prevFolder = filepath.Clean(w.state.Folder)
	}
	w.mu.RUnlock()
	switched := prevFolder != clean
	if switched {
		w.ghostTitle = ""
	}
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
		files.LogErrorf("workspace switch state save failed: folder=%s err=%v", clean, saveErr)
		return saveErr
	}
	w.mu.Lock()
	w.state = next
	if switched {
		w.sessions = make(map[string]*editor.Session)
		w.surfaces = make(map[string]*TabSurface)
		w.virtualPreview = make(map[string]bool)
		w.lastSnapAt = make(map[string]time.Time)
		w.lastDiskMarkdown = make(map[string]string)
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
		w.lastNotifiedNote = next.ActiveTab
	}
	w.mu.Unlock()

	w.RescanNotebook()
	if switched {
		notes, dirs := countWorkspaceEntries(w.Entries())
		files.LogInfof("workspace switched: folder=%s notes=%d dirs=%d", clean, notes, dirs)
		if sup := w.Plugins(); sup != nil {
			sup.OnNotebookSwitched()
		}
	}
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

func entriesInDir(root string, entries []model.FileEntry, targetDir string) ([]model.FileEntry, bool) {
	cleanTarget := filepath.Clean(targetDir)
	if root != "" && filepath.Clean(root) == cleanTarget {
		return entries, true
	}
	var walk func(items []model.FileEntry) ([]model.FileEntry, bool)
	walk = func(items []model.FileEntry) ([]model.FileEntry, bool) {
		for _, it := range items {
			if !it.IsDir {
				continue
			}
			if filepath.Clean(it.Path) == cleanTarget {
				return it.Children, true
			}
			if got, ok := walk(it.Children); ok {
				return got, true
			}
		}
		return nil, false
	}
	return walk(entries)
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
	allInDir, _ := entriesInDir(w.state.Folder, w.entries, cleanParent)
	allInDir = append([]model.FileEntry(nil), allInDir...)
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

// ForgetNotebook removes root from recent Notebooks (switching to the next remaining
// notebook or clearing the active workspace if root was active) without touching files on disk.
func (w *Workspace) ForgetNotebook(root string) {
	if strings.TrimSpace(root) == "" {
		return
	}
	clean := filepath.Clean(root)

	w.mu.RLock()
	prevFolder := ""
	if w.state.Folder != "" {
		prevFolder = filepath.Clean(w.state.Folder)
	}
	w.mu.RUnlock()

	if prevFolder == clean {
		if err := w.flushDirtySessions(); errors.Is(err, errPluginInsertInFlight) {
			w.setNotice(err.Error())
			return
		}
	}

	var switched bool
	next, err := files.UpdateState(func(st *model.AppState) {
		filtered := make([]string, 0, len(st.Notebooks))
		for _, nb := range st.Notebooks {
			if filepath.Clean(nb) != clean {
				filtered = append(filtered, nb)
			}
		}
		st.Notebooks = filtered

		filteredIgnored := make([]string, 0, len(st.IgnoredSearchFolders))
		for _, nb := range st.IgnoredSearchFolders {
			if filepath.Clean(nb) != clean {
				filteredIgnored = append(filteredIgnored, nb)
			}
		}
		st.IgnoredSearchFolders = filteredIgnored

		if st.Folder != "" && filepath.Clean(st.Folder) == clean {
			switched = true
			st.OpenTabs = nil
			st.ActiveTab = ""
			nextFolder := ""
			for _, nb := range filtered {
				nbClean := filepath.Clean(nb)
				if info, statErr := os.Stat(nbClean); statErr == nil && info.IsDir() {
					nextFolder = nbClean
					break
				}
			}
			st.Folder = nextFolder
		}
	})
	if err == nil {
		w.mu.Lock()
		w.state = next
		if switched {
			w.lastScanErr = ""
			w.sessions = make(map[string]*editor.Session)
			w.surfaces = make(map[string]*TabSurface)
			w.virtualPreview = make(map[string]bool)
			w.lastSnapAt = make(map[string]time.Time)
			w.lastDiskMarkdown = make(map[string]string)
			w.closed = NewClosedTabs()
			w.nav = NewNavHistory()
			w.historyToast = nil
			if w.historyView != nil {
				w.historyView.Close()
			}
			w.tabMenu.Close()
			w.sidebarUI.SortFilterOpen = false
			w.sidebarUI.ContextOpen = false
			w.sidebarUI.MoveSubmenuOpen = false
			w.sidebarUI.MoveHoverHold = false
			w.sidebarUI.ConfirmForgetNotebook = ""
			w.sidebarUI.FocusDir = ""
			w.sidebarUI.ExpandedDirs = make(map[string]bool)
			w.sidebarUI.CancelRename()
			w.sidebarUI.ClearDrag()
			w.lastNotifiedNote = next.ActiveTab
		}
		w.mu.Unlock()
		theme.SetIgnoredSearchFolders(next.IgnoredSearchFolders)
	}
	if switched {
		w.RescanNotebook()
		if w.win != nil {
			w.startWatching(next.Folder)
		}
		if sup := w.Plugins(); sup != nil {
			sup.OnNotebookSwitched()
		}
	}
	if w.searchWin != nil {
		w.searchWin.Invalidate()
	}
	w.RequestRedraw()
}

// RegisterNotebook adds folder to the notebook list without switching to it.
func (w *Workspace) RegisterNotebook(folder string) error {
	clean := filepath.Clean(folder)
	info, err := os.Stat(clean)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("notebook path is not a directory")
	}
	w.mu.RLock()
	for _, nb := range w.state.Notebooks {
		if filepath.Clean(nb) == clean {
			w.mu.RUnlock()
			return nil
		}
	}
	w.mu.RUnlock()
	next, err := files.UpdateState(func(st *model.AppState) {
		for _, nb := range st.Notebooks {
			if filepath.Clean(nb) == clean {
				return
			}
		}
		st.Notebooks = append(st.Notebooks, clean)
	})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.state.Notebooks = next.Notebooks
	w.mu.Unlock()
	w.RequestRedraw()
	return nil
}

// ChooseNotebookDialog opens the native directory picker to switch notebooks.
func (w *Workspace) ChooseNotebookDialog() {
	if !mygo.App.IsReady() {
		return
	}
	win := w.Window()
	go func() {
		paths, err := files.OpenDialog(win, mygo.OpenDialogOptions{
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
		w.lastScanErr = ""
		w.mu.Unlock()
		return changed
	}
	list, err := files.ScanNotebook(folder, opts)
	w.mu.Lock()
	var changed bool
	var scanErr string
	if err != nil {
		errStr := err.Error()
		changed = w.lastScanErr != errStr
		w.lastScanErr = errStr
		if changed {
			scanErr = errStr
		}
	} else {
		changed = !fileTreeEqual(w.entries, list)
		w.lastScanErr = ""
		w.entries = list
	}
	w.mu.Unlock()
	if err != nil && changed {
		files.LogWarnf("workspace scan failed: folder=%s err=%v", folder, err)
		w.notify(scanErr, "error")
	}
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

func (w *Workspace) beginCreatedItemRename(root, path string, isDir bool) {
	root = filepath.Clean(root)
	for dir := filepath.Dir(path); dir != root && isSameOrDescendant(dir, root); dir = filepath.Dir(dir) {
		w.sidebarUI.ExpandedDirs[dir] = true
	}
	w.modals.CloseSettings()
	w.ai.SetFullscreen(false)
	w.sidebarUI.StartRename(path, isDir)
	w.sidebarUI.RenamingNewItem = true
	w.RequestRedraw()
}

// CreateNewNote creates an Untitled.md / 未命名.md note, switches to it immediately, and starts inline rename.
func (w *Workspace) CreateNewNote() (string, error) {
	folder := w.ActiveNotebook()
	if folder == "" {
		return "", errors.New(theme.Tr("请先打开一个笔记本文件夹", "Open a notebook folder first"))
	}
	return w.CreateNewNoteIn(w.targetCreateDir(folder))
}

// CreateNewNoteIn creates a new note inside dir, opens it, and starts inline rename.
func (w *Workspace) CreateNewNoteIn(dir string) (string, error) {
	folder := w.ActiveNotebook()
	if folder == "" || dir == "" {
		return "", errors.New(theme.Tr("请先打开一个笔记本文件夹", "Open a notebook folder first"))
	}
	created, err := files.CreateNewNote(dir, theme.IsEn())
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	w.RescanNotebook()
	if err := w.openNoteInternal(created, true, true, 0); err != nil {
		w.setNotice(err.Error())
	}
	w.beginCreatedItemRename(folder, created, false)
	return created, nil
}

// CreateNewFolder creates a new subfolder in the target notebook folder,
// places it at the bottom of sibling folders, and starts inline rename.
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
	w.RescanNotebook()
	if w.placeCreatedFolderAtBottom(targetDir, created) {
		w.RescanNotebook()
	}
	w.beginCreatedItemRename(folder, created, true)
	return created, nil
}

func (w *Workspace) placeCreatedFolderAtBottom(targetDir, created string) bool {
	cleanTarget := filepath.Clean(targetDir)
	cleanCreated := filepath.Clean(created)
	createdBase := filepath.Base(cleanCreated)
	if createdBase == "" || createdBase == "." {
		return false
	}

	w.mu.RLock()
	sortMode := w.state.TreeSort
	root := w.state.Folder
	inDir, found := entriesInDir(root, w.entries, cleanTarget)
	inDirSnap := append([]model.FileEntry(nil), inDir...)
	w.mu.RUnlock()

	if sortMode != model.TreeSortManual {
		nextState, err := files.UpdateState(func(st *model.AppState) {
			if len(st.FileOrder) == 0 {
				return
			}
			for key, children := range st.FileOrder {
				if filepath.Clean(key) != cleanTarget {
					continue
				}
				filtered := make([]string, 0, len(children)+1)
				for _, child := range children {
					full := child
					if !filepath.IsAbs(full) {
						full = filepath.Join(cleanTarget, child)
					}
					if filepath.Clean(full) != cleanCreated {
						filtered = append(filtered, child)
					}
				}
				st.FileOrder[key] = append(filtered, createdBase)
			}
		})
		if err == nil {
			w.mu.Lock()
			w.state = nextState
			w.mu.Unlock()
		}
		return false
	}

	if !found {
		return false
	}

	var dirs, regularFiles []string
	lastDirIdx := -1
	createdIdx := -1
	for i, it := range inDirSnap {
		b := filepath.Base(it.Path)
		if it.IsDir {
			lastDirIdx = i
			if filepath.Clean(it.Path) == cleanCreated {
				createdIdx = i
				continue
			}
			dirs = append(dirs, b)
		} else {
			regularFiles = append(regularFiles, b)
		}
	}
	dirs = append(dirs, createdBase)
	combined := append(dirs, regularFiles...)

	nextState, err := files.UpdateState(func(st *model.AppState) {
		if st.FileOrder == nil {
			st.FileOrder = make(map[string][]string)
		}
		for key := range st.FileOrder {
			if key != cleanTarget && filepath.Clean(key) == cleanTarget {
				delete(st.FileOrder, key)
			}
		}
		st.FileOrder[cleanTarget] = combined
	})
	if err == nil {
		w.mu.Lock()
		w.state = nextState
		w.mu.Unlock()
	}
	return createdIdx != lastDirIdx
}

func (w *Workspace) finishSidebarRename(oldPath, newName string) error {
	newNote := w.sidebarUI.RenamingNewItem && !w.sidebarUI.RenamingDir
	path, err := w.RenameWorkspaceItem(oldPath, newName)
	if err != nil {
		return err
	}
	if newNote {
		if err := w.openNoteInternal(path, true, true, 0); err != nil {
			w.setNotice(err.Error())
		} else if sess := w.SessionFor(path); sess != nil {
			sess.FocusBlock(0)
		}
	}
	return nil
}

// RenameWorkspaceItem renames oldPath to newName on disk and rebases open tabs, sessions, surfaces, and history.
func (w *Workspace) RenameWorkspaceItem(oldPath, newName string) (string, error) {
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	if err := w.flushDirtySessionsLocked(); err != nil {
		files.LogErrorf("rename failed: path=%s err=%v", oldPath, err)
		w.setNotice(err.Error())
		return "", err
	}
	newPath, err := files.RenameItem(oldPath, newName)
	if err != nil {
		files.LogErrorf("rename failed: path=%s err=%v", oldPath, err)
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
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	if err := w.flushDirtySessionsLocked(); err != nil {
		files.LogErrorf("move failed: src=%s dst=%s err=%v", srcPath, dstDir, err)
		w.setNotice(err.Error())
		return "", err
	}
	newPath, err := files.MoveItem(srcPath, dstDir)
	if err != nil {
		files.LogErrorf("move failed: src=%s dst=%s err=%v", srcPath, dstDir, err)
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
	return w.trashWorkspaceItem(path, func() error { return files.TrashItem(path) })
}

func (w *Workspace) trashWorkspaceItem(path string, moveToTrash func() error) error {
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	if err := w.flushDirtySessionsLocked(); err != nil {
		files.LogErrorf("trash failed: path=%s err=%v", path, err)
		w.setNotice(err.Error())
		return err
	}
	if err := moveToTrash(); err != nil {
		files.LogErrorf("trash failed: path=%s err=%v", path, err)
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

	nextDiskMD := make(map[string]string, len(w.lastDiskMarkdown))
	for k, v := range w.lastDiskMarkdown {
		if next, ok := rebase(k); ok {
			nextDiskMD[filepath.Clean(next)] = v
		} else {
			nextDiskMD[k] = v
		}
	}
	w.lastDiskMarkdown = nextDiskMD

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

	var toClose []*editor.Session
	w.mu.Lock()
	for k, s := range w.sessions {
		if matches(k) {
			if s != nil {
				toClose = append(toClose, s)
			}
			delete(w.sessions, k)
			delete(w.virtualPreview, k)
			delete(w.lastSnapAt, k)
			delete(w.lastDiskMarkdown, filepath.Clean(k))
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

	for _, s := range toClose {
		s.Close()
	}

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
	w.edgeFloat.DismissAll(w)
	if err := w.flushSingleSession(clean); err != nil {
		w.setNotice(err.Error())
		return
	}

	w.mu.Lock()
	delete(w.lastSnapAt, clean)
	closedIdx := -1
	for i, t := range w.state.OpenTabs {
		if t == clean {
			closedIdx = i
			break
		}
	}
	isVirtual := w.virtualPreview[clean]
	delete(w.virtualPreview, clean)
	prevActive := w.state.ActiveTab
	nextTabs, nextActive, closed := w.tabOps.CloseTab(w.state.OpenTabs, prevActive, clean)
	if !closed {
		w.mu.Unlock()
		return
	}
	w.state.OpenTabs = nextTabs
	w.state.ActiveTab = nextActive
	closedSess := w.sessions[clean]
	delete(w.sessions, clean)
	delete(w.surfaces, clean)
	delete(w.lastDiskMarkdown, clean)
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

	if closedSess != nil {
		closedSess.Close()
	}

	if nextActive != "" {
		_ = w.ensureTabLoaded(nextActive)
		if nextActive != prevActive {
			if sess := w.SessionFor(nextActive); sess != nil {
				sess.ClearFocus()
			}
		}
	}
	w.persistTabs(tabs, nextActive)
	w.RequestRedraw()
}

// CloseActiveTab closes the active tab, or closes the main window when no tab is open.
func (w *Workspace) CloseActiveTab() {
	if w.modals != nil && (w.modals.IsSettingsOpen() || w.modals.HasActiveModal()) {
		return
	}
	if w.exportCtrl.IsOpen() || w.IsPageHistoryOpen() {
		return
	}
	active := w.ActiveNotePath()
	if active != "" {
		w.CloseTab(active)
		return
	}
	if w.OnCloseMainWindow != nil {
		w.OnCloseMainWindow()
	} else if win := w.Window(); win != nil {
		win.Close()
	}
}

// ReopenClosedTab pops the most recently closed tab (`Cmd+Shift+T`) and restores it at its prior index.
func (w *Workspace) ReopenClosedTab() bool {
	if w.isBusy() {
		return false
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.edgeFloat.DismissAll(w)
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
	w.edgeFloat.DismissAll(w)
	if err := w.flushDirtySessions(); err != nil {
		w.setNotice(err.Error())
		return
	}
	clean := filepath.Clean(keepPath)
	w.mu.Lock()
	prevActive := w.state.ActiveTab
	origTabs := append([]string(nil), w.state.OpenTabs...)
	nextTabs, nextActive, closedPaths := w.tabOps.CloseOthers(w.state.OpenTabs, clean)
	if len(closedPaths) == 0 {
		w.mu.Unlock()
		return
	}
	w.closed.RecordOthers(origTabs, clean, func(p string) bool { return w.virtualPreview[p] })
	var closedSessions []*editor.Session
	for _, p := range closedPaths {
		if s := w.sessions[p]; s != nil {
			closedSessions = append(closedSessions, s)
		}
		delete(w.virtualPreview, p)
		delete(w.sessions, p)
		delete(w.surfaces, p)
		delete(w.lastSnapAt, p)
		delete(w.lastDiskMarkdown, filepath.Clean(p))
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

	for _, s := range closedSessions {
		s.Close()
	}

	if nextActive != "" {
		_ = w.ensureTabLoaded(nextActive)
		if nextActive != prevActive {
			if sess := w.SessionFor(nextActive); sess != nil {
				sess.ClearFocus()
			}
		}
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
	w.edgeFloat.DismissAll(w)
	if err := w.flushDirtySessions(); err != nil {
		w.setNotice(err.Error())
		return
	}
	clean := filepath.Clean(pivotPath)
	w.mu.Lock()
	prevActive := w.state.ActiveTab
	pivotIdx := -1
	for i, t := range w.state.OpenTabs {
		if t == clean {
			pivotIdx = i
			break
		}
	}
	nextTabs, nextActive, closedPaths := w.tabOps.CloseToRight(w.state.OpenTabs, prevActive, clean)
	if len(closedPaths) == 0 {
		w.mu.Unlock()
		return
	}
	if pivotIdx >= 0 {
		w.closed.RecordFrom(pivotIdx+1, closedPaths, func(p string) bool { return w.virtualPreview[p] })
	}
	closedSet := make(map[string]bool, len(closedPaths))
	var closedSessions []*editor.Session
	for _, p := range closedPaths {
		closedSet[p] = true
		if s := w.sessions[p]; s != nil {
			closedSessions = append(closedSessions, s)
		}
		delete(w.virtualPreview, p)
		delete(w.sessions, p)
		delete(w.surfaces, p)
		delete(w.lastSnapAt, p)
		delete(w.lastDiskMarkdown, filepath.Clean(p))
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

	for _, s := range closedSessions {
		s.Close()
	}

	if nextActive != "" {
		_ = w.ensureTabLoaded(nextActive)
		if nextActive != prevActive {
			if sess := w.SessionFor(nextActive); sess != nil {
				sess.ClearFocus()
			}
		}
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
	w.edgeFloat.DismissAll(w)
	if err := w.flushDirtySessions(); err != nil {
		w.setNotice(err.Error())
		return
	}
	w.mu.Lock()
	var closedSessions []*editor.Session
	for _, s := range w.sessions {
		if s != nil {
			closedSessions = append(closedSessions, s)
		}
	}
	w.closed.RecordFrom(0, w.state.OpenTabs, func(p string) bool { return w.virtualPreview[p] })
	w.virtualPreview = make(map[string]bool)
	w.sessions = make(map[string]*editor.Session)
	w.surfaces = make(map[string]*TabSurface)
	w.lastSnapAt = make(map[string]time.Time)
	w.lastDiskMarkdown = make(map[string]string)
	w.historyToast = nil
	if w.historyView != nil && w.historyView.Open {
		w.historyView.Close()
	}
	w.state.OpenTabs = nil
	w.state.ActiveTab = ""
	w.mu.Unlock()
	for _, s := range closedSessions {
		s.Close()
	}
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
	wasEdgeLeft := w.edgeFloat.LeftOpen()
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.edgeFloat.DismissAll(w)
	if wasEdgeLeft {
		w.sidebarUI.CancelRename()
		w.RequestRedraw()
		return
	}

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
	w.sidebarUI.CancelRename()
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

func (w *Workspace) onAIEnabledChanged(enabled bool) {
	w.mu.RLock()
	for _, sess := range w.sessions {
		sess.SetAIEnabled(enabled)
	}
	w.mu.RUnlock()
	w.quick.SetAIEnabled(enabled)
	if !enabled {
		w.edgeFloat.DismissRightWithCooldown(w)
		_ = w.RejectBatchPlan("") // Close editor previews while preserving the plan in chat.
	}
	if w.OnAppMenuOrHotkeysChanged != nil {
		w.OnAppMenuOrHotkeysChanged(w.State())
	}
	w.RequestRedraw()
}

// ToggleAIPanel opens or closes the AI Assistant panel (`Cmd+J`), unless blocked by Settings.
func (w *Workspace) ToggleAIPanel() bool {
	if !w.ai.Enabled() || w.modals.IsSettingsOpen() {
		return false
	}
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.edgeFloat.DismissAll(w)
	w.ai.ToggleOpen()
	if w.ai.IsOpen() && w.pluginUI != nil {
		w.pluginUI.closePanelLocal("", "")
	}
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
	w.emitNoteOpenedIfChanged(active)
}

func (w *Workspace) emitNoteOpenedIfChanged(active string) {
	w.mu.Lock()
	if active == w.lastNotifiedNote {
		w.mu.Unlock()
		return
	}
	w.lastNotifiedNote = active
	w.mu.Unlock()
	if active == "" {
		return
	}
	if sup := w.Plugins(); sup != nil {
		if rel := sup.ActiveNoteRel(); rel != "" {
			sup.EmitWorkspaceEvent("note.opened", map[string]any{"path": rel})
		}
	}
}

func (w *Workspace) notify(msg, tone string) {
	if w.pluginUI != nil {
		w.pluginUI.Toast("", msg, tone, nil)
	}
}

func (w *Workspace) notifyPathCopied() {
	w.notify(theme.Tr("已复制路径", "Copied path"), "success")
}

func (w *Workspace) setNotice(msg string) {
	w.notify(msg, "error")
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

type pendingSessionSave struct {
	path string
	sess *editor.Session
	rev  uint64
	md   string
}

func (w *Workspace) markSelfWriteWindow(d time.Duration) {
	w.mu.Lock()
	until := time.Now().Add(d)
	if until.After(w.selfWriteUntil) {
		w.selfWriteUntil = until
	}
	w.mu.Unlock()
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
			w.saveGeneration++
			generation := w.saveGeneration
			var batch []pendingSessionSave
			for p, sess := range w.sessions {
				if sess != nil && sess.Dirty() && !w.virtualPreview[p] {
					if w.pluginInsertInFlight[p] > 0 || w.pluginInsertInFlight[filepath.Clean(p)] > 0 {
						if w.autoSaveDeferred == nil {
							w.autoSaveDeferred = make(map[string]bool)
						}
						w.autoSaveDeferred[p] = true
						continue
					}
					batch = append(batch, pendingSessionSave{
						path: p,
						sess: sess,
						rev:  sess.DocRev(),
						md:   sess.Markdown(),
					})
				}
			}
			w.mu.Unlock()
			w.flushPendingSidebarWidth()
			if len(batch) == 0 {
				return
			}
			go w.runAsyncSessionSaves(generation, batch)
		})
	})
	w.mu.Unlock()
}

func (w *Workspace) bumpDiskWriteSeqLocked(path string) uint64 {
	clean := filepath.Clean(path)
	if w.diskWriteSeq == nil {
		w.diskWriteSeq = make(map[string]uint64)
	}
	w.diskWriteSeq[clean]++
	return w.diskWriteSeq[clean]
}

func (w *Workspace) bumpDiskWriteSeq(path string) uint64 {
	w.mu.Lock()
	seq := w.bumpDiskWriteSeqLocked(path)
	w.mu.Unlock()
	return seq
}

func (w *Workspace) currentDiskWriteSeq(path string) uint64 {
	clean := filepath.Clean(path)
	w.mu.RLock()
	seq := w.diskWriteSeq[clean]
	w.mu.RUnlock()
	return seq
}

func (w *Workspace) runAsyncSessionSaves(generation uint64, batch []pendingSessionSave) {
	_, _ = w.runSessionSaves(generation, batch, false)
}

func (w *Workspace) runSessionSaves(generation uint64, batch []pendingSessionSave, force bool) (map[string]error, map[string]bool) {
	w.bgSaveMu.Lock()
	w.mu.RLock()
	current := w.saveGeneration
	w.mu.RUnlock()
	if !force && generation != current {
		w.bgSaveMu.Unlock()
		return nil, nil
	}

	type completedSave struct {
		item pendingSessionSave
		seq  uint64
	}
	var done []completedSave
	var saveError error
	perPathErr := make(map[string]error)
	completed := make(map[string]bool)
	for _, item := range batch {
		w.mu.RLock()
		live := w.sessions[item.path] == item.sess && !w.virtualPreview[item.path]
		w.mu.RUnlock()
		if !live {
			if force {
				perPathErr[item.path] = errors.New("note session closed before save completed")
			}
			continue
		}
		if item.sess.DocRev() != item.rev {
			if force {
				perPathErr[item.path] = errors.New("save superseded before disk flush completed")
			}
			continue
		}
		writeRecovery := !force
		if err := w.saveSessionMarkdownOpts(item.path, item.sess, item.md, item.rev, writeRecovery); err != nil {
			if errors.Is(err, errSaveSuperseded) {
				if force {
					perPathErr[item.path] = err
				}
				continue
			}
			perPathErr[item.path] = err
			if saveError == nil {
				saveError = err
			}
			continue
		}
		seq := w.currentDiskWriteSeq(item.path)
		completed[item.path] = true
		if writeRecovery {
			files.ClearRecovery(item.path)
		}
		now := time.Now()
		w.mu.RLock()
		lastSnap := w.lastSnapAt[item.path]
		w.mu.RUnlock()
		if now.Sub(lastSnap) >= 5*time.Second {
			_, _ = files.RecordHistorySnapshot(item.path, item.md, theme.Tr("自动保存", "Auto-save"))
			w.mu.Lock()
			w.lastSnapAt[item.path] = now
			w.mu.Unlock()
		}
		done = append(done, completedSave{item: item, seq: seq})
	}
	if force && len(perPathErr) > 0 {
		// Invalidate any pending async autosave captured before the caller rolls back
		// so a stale snapshot can never sneak in and write rolled-back text to disk.
		w.mu.Lock()
		if w.autoSaveTimer != nil {
			w.autoSaveTimer.Stop()
			w.autoSaveTimer = nil
		}
		w.saveGeneration++
		w.mu.Unlock()
	}
	w.bgSaveMu.Unlock()
	if len(done) == 0 && saveError == nil {
		return perPathErr, completed
	}
	applyOnMain := func() {
		if saveError != nil {
			w.setNotice(saveError.Error())
		}
		anyMarked := false
		needReschedule := false
		for _, d := range done {
			cleanPath := filepath.Clean(d.item.path)
			w.mu.RLock()
			live := w.sessions[d.item.path] == d.item.sess
			latestSeq := w.diskWriteSeq[cleanPath]
			w.mu.RUnlock()
			if !live || latestSeq != d.seq {
				continue
			}
			if d.item.sess.MarkSavedIfUnchanged(d.item.rev, d.item.md) {
				anyMarked = true
			} else {
				// Session moved to a newer docRev while d.item.md was being written to disk
				// (e.g. user pressed Undo back to BASE mid-write). Sync savedMarkdown with
				// what is actually on disk so Dirty() never falsely reports clean, and
				// trigger autosave to converge disk back to the current session text.
				d.item.sess.SyncDiskSavedMarkdown(d.item.md)
				if d.item.sess.Dirty() {
					needReschedule = true
				}
				anyMarked = true
			}
			w.notifyPluginNoteSaved(d.item.path)
		}
		if anyMarked {
			w.RequestRedraw()
		}
		if needReschedule {
			w.flushOrScheduleDirtySessions()
		}
	}
	if mygo.App.IsReady() {
		mygo.RunOnMain(applyOnMain)
	} else {
		applyOnMain()
	}
	return perPathErr, completed
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
func (w *Workspace) FlushDirtySessions() error {
	w.flushPendingSidebarWidth()
	err := w.flushDirtySessions()
	if err != nil {
		w.setNotice(err.Error())
	}
	return err
}

func (w *Workspace) flushDirtySessions() error {
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	return w.flushDirtySessionsLocked()
}

func (w *Workspace) flushDirtySessionsLocked() error {
	w.mu.Lock()
	w.saveGeneration++
	if w.autoSaveTimer != nil {
		w.autoSaveTimer.Stop()
		w.autoSaveTimer = nil
	}
	type sessionCandidate struct {
		path        string
		sess        *editor.Session
		lastDiskMD  string
		hasLastDisk bool
	}
	candidates := make([]sessionCandidate, 0, len(w.sessions))
	paths := make([]string, 0, len(w.sessions))
	hasInFlight := false
	for p, sess := range w.sessions {
		if sess == nil || w.virtualPreview[p] {
			continue
		}
		clean := filepath.Clean(p)
		if w.pluginInsertInFlight[p] > 0 || w.pluginInsertInFlight[clean] > 0 {
			hasInFlight = true
			continue
		}
		lastDiskMD, hasLastDisk := w.lastDiskMarkdown[clean]
		candidates = append(candidates, sessionCandidate{
			path:        p,
			sess:        sess,
			lastDiskMD:  lastDiskMD,
			hasLastDisk: hasLastDisk,
		})
	}
	for path, surface := range w.surfaces {
		if surface.IsDirty() {
			paths = append(paths, path)
		}
	}
	w.mu.Unlock()
	for _, c := range candidates {
		if c.sess.Dirty() || (c.hasLastDisk && c.sess.Markdown() != c.lastDiskMD) {
			paths = append(paths, c.path)
		}
	}
	var firstErr error
	for _, p := range paths {
		if err := w.flushSingleSessionLocked(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil && hasInFlight {
		firstErr = errPluginInsertInFlight
	}
	return firstErr
}

func (w *Workspace) flushSingleSession(path string) error {
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	return w.flushSingleSessionLocked(path)
}

func (w *Workspace) flushSingleSessionLocked(path string) error {
	clean := filepath.Clean(path)
	w.mu.RLock()
	source := w.surfaces[path]
	sess := w.sessions[path]
	isVirtual := w.virtualPreview[path]
	inFlight := w.pluginInsertInFlight[path] > 0 || w.pluginInsertInFlight[clean] > 0
	lastSnap := w.lastSnapAt[path]
	lastDiskMD, hasLastDisk := w.lastDiskMarkdown[clean]
	w.mu.RUnlock()
	if source != nil && source.IsDirty() {
		return source.saveSource()
	}
	if inFlight {
		return errPluginInsertInFlight
	}
	if sess == nil || isVirtual {
		return nil
	}
	md := sess.Markdown()
	if !sess.Dirty() && (!hasLastDisk || md == lastDiskMD) {
		return nil
	}
	if err := w.saveSessionMarkdown(path, sess, md); err != nil {
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
	w.notifyPluginNoteSaved(path)
	return nil
}

var (
	errSaveSuperseded       = errors.New("save superseded before disk flush completed")
	errPluginInsertInFlight = errors.New("editor insert save in progress")
)

func (w *Workspace) flushSingleSessionNoRecovery(path string, expectedRev uint64) error {
	w.bgSaveMu.Lock()
	defer w.bgSaveMu.Unlock()
	w.mu.RLock()
	sess := w.sessions[path]
	isVirtual := w.virtualPreview[path]
	lastSnap := w.lastSnapAt[path]
	w.mu.RUnlock()
	if sess == nil || !sess.Dirty() || isVirtual {
		return nil
	}
	md := sess.Markdown()
	if err := w.saveSessionMarkdownOpts(path, sess, md, expectedRev, false); err != nil {
		return err
	}
	sess.MarkSaved()
	now := time.Now()
	if now.Sub(lastSnap) >= 5*time.Second {
		_, _ = files.RecordHistorySnapshot(path, md, theme.Tr("自动保存", "Auto-save"))
		w.mu.Lock()
		w.lastSnapAt[path] = now
		w.mu.Unlock()
	}
	w.notifyPluginNoteSaved(path)
	return nil
}

func (w *Workspace) saveSessionMarkdown(path string, sess *editor.Session, text string) error {
	return w.saveSessionMarkdownOpts(path, sess, text, 0, true)
}

func (w *Workspace) saveSessionMarkdownOpts(path string, sess *editor.Session, text string, expectedRev uint64, writeRecovery bool) error {
	if expectedRev != 0 && sess.DocRev() != expectedRev {
		return errSaveSuperseded
	}
	w.markSelfWriteWindow(800 * time.Millisecond)
	fingerprint, err := files.WriteNoteConditionalFingerprint(path, text, sess.DiskFingerprint())
	if err == nil {
		sess.SetDiskFingerprint(fingerprint)
		clean := filepath.Clean(path)
		w.mu.Lock()
		w.bumpDiskWriteSeqLocked(clean)
		if w.lastDiskMarkdown == nil {
			w.lastDiskMarkdown = make(map[string]string)
		}
		w.lastDiskMarkdown[clean] = text
		w.mu.Unlock()
		w.markSelfWriteWindow(800 * time.Millisecond)
		return nil
	}
	files.LogWarnf("auto-save failed for %s: %v", path, err)
	if !writeRecovery {
		return err
	}
	if recoveryErr := files.SaveRecoveryForVersion(path, text, sess.DiskFingerprint()); recoveryErr != nil {
		files.LogErrorf("recovery draft save failed: path=%s err=%v", path, recoveryErr)
		return errors.Join(err, recoveryErr)
	}
	return err
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
		files.LogWarnf("file watcher start failed: folder=%s err=%v", folder, err)
		return
	}
	go func() {
		defer watcher.Close()
		events := make(chan struct{}, 1)
		go func() {
			for {
				if _, err := watcher.Next(ctx); err != nil {
					if ctx.Err() == nil {
						files.LogWarnf("file watcher stopped: folder=%s err=%v", folder, err)
					}
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
				w.mu.RLock()
				inSelfWrite := time.Now().Before(w.selfWriteUntil)
				w.mu.RUnlock()
				if inSelfWrite {
					continue
				}
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
	renameVisible := w.sidebarUI.RenamingNewItem && !inSettings
	aiOpen := w.ai.IsOpen()
	pluginPanelOpen := !aiOpen && w.pluginUI != nil && w.pluginUI.hasOpenPanel()
	chatDesiredW := w.ai.PanelWidth()
	if pluginPanelOpen {
		chatDesiredW = w.pluginUI.panelWidth()
	}
	return ShellInput{
		WindowWidth:     winW,
		InSettings:      inSettings,
		SidebarVisible:  !w.state.SidebarCollapsed || renameVisible,
		SidebarPinned:   w.sidebarPinned || renameVisible,
		SidebarDesiredW: w.sidebarWidth,
		ChatVisible:     aiOpen || pluginPanelOpen,
		ChatFullscreen:  aiOpen && w.ai.IsFullscreen(),
		ChatDesiredW:    chatDesiredW,
		TitlebarNeedW:   w.tabMenu.TitlebarNeedW,
	}
}

func (w *Workspace) isAIPanelVisible(vpW float32) bool {
	if w.ai == nil || !w.ai.Enabled() || !w.ai.IsOpen() || w.modals.IsSettingsOpen() {
		return false
	}
	if w.ai.IsFullscreen() {
		return true
	}
	if vpW <= 0 {
		if win := w.Window(); win != nil {
			if ww, _ := win.Size(); ww > 0 {
				vpW = float32(ww)
			}
		}
	}
	if vpW <= 0 {
		vpW = 1280
	}
	w.mu.RLock()
	in := w.shellInputLocked(vpW)
	w.mu.RUnlock()
	layout := ResolveShellLayout(in)
	return layout.ChatDocked || w.edgeFloat.RightOpen()
}

func (w *Workspace) handleAppShortcuts(c *ui.Context, vpW float32) {
	if w.pluginsUI != nil && w.pluginsUI.IsStoreLightboxOpen() {
		return
	}
	if w.ai != nil && w.ai.IsLightboxOpen() {
		if w.isAIPanelVisible(vpW) {
			return
		}
		w.ai.CloseTransientPopups()
	}
	if w.modals.IsRecordingShortcut() || w.exportCtrl.IsOpen() || w.IsPageHistoryOpen() {
		return
	}

	st := w.State()
	editor.SetFindShortcut(modals.EffectiveShortcut(st, "find"))

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
	if w.modals.IsPerformanceOpen() && matchAction("performance") {
		w.modals.ClosePerformance()
		return
	}
	if w.palette.IsOpen() {
		if matchRaw(blockui.DefaultCommandPaletteShortcut) {
			w.palette.Close()
		}
		return
	}
	if w.modals.HasActiveModal() || w.hasActivePluginModal() {
		return
	}
	if theme.IsTarotMode() && w.tarot != nil && w.tarot.Room() != tarot.RoomWriting && c.Shortcut(0, ui.KeyEscape) {
		w.tarot.BackToWriting()
		return
	}
	if matchRaw(blockui.DefaultCommandPaletteShortcut) {
		w.tabMenu.Close()
		w.sidebarUI.ClosePopups()
		w.edgeFloat.DismissAll(w)
		w.palette.Open()
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
	if w.ai.Enabled() && matchAction("toggle_ai") {
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
	if matchRaw(copyMarkdownShortcutSpec()) {
		w.copyActiveMarkdown(c.Services())
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		w.FlushDirtySessions()
		if theme.IsTarotMode() && w.tarot != nil {
			w.tarot.TriggerSaveFlash()
		}
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
	w.handlePluginCommandShortcuts(c)
}

func (w *Workspace) handlePluginCommandShortcuts(c *ui.Context) {
	sup := w.Plugins()
	if sup == nil {
		return
	}
	if w.sidebarUI.RenamingPath != "" || w.sidebarUI.RenamingNewItem || w.sidebarUI.HasActiveOverlay() {
		return
	}
	if sess := w.ActiveSession(); sess != nil && (sess.IsFindOpen() || sess.IsInlineAIOpen() || sess.IsSlashMenuOpen()) {
		return
	}
	textFocused := c.FocusTakesText()
	for _, cmd := range sup.Commands() {
		if cmd.Shortcut == "" {
			continue
		}
		mods, key, ok := blockui.ParseShortcut(cmd.Shortcut)
		if !ok || isHostReservedShortcut(mods, key) {
			continue
		}
		if textFocused {
			if mods&(ui.Cmd|ui.Ctrl|ui.Alt) == 0 || isTextInputReservedShortcut(mods, key) {
				continue
			}
		}
		if c.Shortcut(mods, key) {
			sup.RunCommandAsync(cmd.PluginID, cmd.ID, plugins.CommandContext{
				Note:      sup.ActiveNoteRel(),
				File:      w.ActiveNotePath(),
				Selection: w.activeSelectionText(),
				Source:    "shortcut",
			})
			return
		}
	}
}

func isTextInputReservedShortcut(mods ui.Modifiers, key ui.Key) bool {
	if mods == ui.Cmd {
		switch key {
		case ui.KeyA, ui.KeyC, ui.KeyV, ui.KeyX, ui.KeyZ,
			ui.KeyBackspace, ui.KeyDelete, ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
			return true
		}
	}
	if mods == (ui.Cmd|ui.Shift) && key == ui.KeyZ {
		return true
	}
	if mods&(ui.Alt|ui.Ctrl) != 0 {
		switch key {
		case ui.KeyBackspace, ui.KeyDelete, ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
			return true
		}
	}
	return false
}

func copyMarkdownShortcutSpec() string {
	// Alt+Cmd+C on macOS, Ctrl+Alt+C on Windows/Linux.
	return "alt-cmd-c"
}

func (w *Workspace) copyActiveMarkdown(svc ui.Services) {
	sess := w.ActiveSession()
	if sess == nil {
		return
	}
	txt := sess.SelectedMarkdown()
	if txt == "" {
		txt = sess.Markdown()
	}
	if txt == "" {
		return
	}
	svc.WriteClipboard(txt)
}

func (w *Workspace) handleEditorZoomShortcuts(c *ui.Context) bool {
	if w.modals.IsRecordingShortcut() || w.modals.IsSettingsOpen() ||
		w.modals.HasActiveModal() || w.hasActivePluginModal() || w.exportCtrl.IsOpen() || w.IsPageHistoryOpen() {
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

	pal := theme.Ocean()
	uiTheme := pal.ToUITheme("")
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
			if w.pluginUI != nil {
				w.pluginUI.renderToast(c)
			}
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
						w.bgSaveMu.Lock()
						defer w.bgSaveMu.Unlock()
						w.mu.RLock()
						currentSession := w.sessions[notePath]
						w.mu.RUnlock()
						if currentSession != nil && currentSession.Dirty() {
							w.historyView.ErrorMsg = theme.Tr("笔记已有新编辑，无法恢复", "The note has newer edits")
							w.RequestRedraw()
							return
						}
						preRestore, _, err := files.ReadNote(notePath)
						if err == nil && preRestore != w.historyView.CurrentText {
							err = errors.New(theme.Tr("文件已变化，请重新对比", "The file changed. Compare it again before restoring"))
						}
						if err == nil {
							_, err = files.RecordHistorySnapshot(notePath, preRestore, theme.Tr("恢复前快照", "Before restore"))
						}
						if err == nil {
							err = w.writeNoteLocked(notePath, restoredText)
						}
						if err != nil {
							w.historyView.ErrorMsg = err.Error()
							w.RequestRedraw()
							return
						}
						w.historyView.Close()
						w.mu.Lock()
						w.historyToast = &HistoryToast{
							Active:              true,
							NotePath:            notePath,
							Message:             theme.Tr("已恢复到所选历史版本", "Restored selected snapshot"),
							UndoContent:         preRestore,
							RestoredFingerprint: files.TextFingerprint(restoredText, false),
							CanUndo:             preRestore != restoredText,
						}
						w.mu.Unlock()
						w.RequestRedraw()
					},
				)
			})
			w.modals.RenderModals(c)
			w.renderPluginOverlays(c)
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
	var dirtyTabs map[string]bool
	for _, tp := range tabs {
		isDirty := false
		if s := w.sessions[tp]; s != nil && s.Dirty() {
			isDirty = true
		} else if sf := w.surfaces[tp]; sf != nil && sf.IsDirty() {
			isDirty = true
		}
		if isDirty {
			if dirtyTabs == nil {
				dirtyTabs = make(map[string]bool, len(tabs))
			}
			dirtyTabs[tp] = true
		}
	}
	activeTab := w.state.ActiveTab
	activeNB := w.state.Folder
	notebooks := append([]string(nil), w.state.Notebooks...)
	entries := append([]model.FileEntry(nil), w.entries...)
	hasFBDraft := w.state.FeedbackDraft != nil
	toast := w.historyToast
	stSnap := w.state
	w.mu.Unlock()
	sourceSoftWrap = stSnap.SourceSoftWrap

	inSettings := w.modals.IsSettingsOpen()
	aiImmersive := layout.ChatFullscreen && !inSettings
	sidebarWide := layout.SidebarDocked
	sidebarAutoHidden := layout.SidebarYielded && !layout.SidebarDrawer
	dockedSidebarWidth := float32(0)
	if layout.SidebarDocked {
		dockedSidebarWidth = layout.SidebarWidth
	}

	activeSurf := w.SurfaceFor(activeTab)
	var activeSess *editor.Session
	var noteStats func() (characters, blocks int)
	if activeSurf == nil {
		activeSess = w.SessionFor(activeTab)
		if activeSess != nil {
			noteStats = activeSess.NoteStats
		}
	}

	var annDrafts int
	var annGenerating, annHasAny bool
	aiEnabled := w.ai.Enabled()
	if aiEnabled && activeSess != nil {
		for _, a := range activeSess.Annotations() {
			body := strings.TrimSpace(a.Note) != ""
			if body && a.RewriteResult == "" && a.Answer == "" && !a.RewriteBusy && !a.Applied {
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

	if theme.IsTarotMode() && w.tarot != nil && !inSettings && !aiImmersive {
		cardLeft := float32(8)
		if layout.SidebarDocked {
			cardLeft = 0
		}
		cardHorizMargin := cardLeft + 8
		effMainW := max(layout.MainWidth-cardHorizMargin, 0)
		w.renderTarotWorkspace(
			c, vpW, layout, entries, activeNB, activeTab, dirtyTabs[activeTab],
			activeSurf, activeSess, toast, aiEnabled, sidebarWide, effMainW, cardHorizMargin, 0, 0,
		)
		return
	}

	chrome := pal.ShellChrome()
	clear(w.chromePopups)
	w.chromePopups = w.chromePopups[:0]
	c.Key("workspace-root")
	root := ui.Column(c).Fill().Background(chrome.Ground).TextColor(pal.Text).Clip()
	blockui.Probe(root, "workspace-root")

	canEdgeLeft, canEdgeRight, edgeSafeL, edgeSafeR := w.edgeFloat.PrepareFrame(
		c, w, vpW, layout, inSettings, aiImmersive, activeSess,
	)
	// Handle narrow sidebar drawer Escape before activeSess.Render registers its window Escape shortcut.
	if layout.SidebarDrawer && !aiImmersive && !inSettings && !w.modals.HasActiveModal() && !w.hasActivePluginModal() && !w.sidebarUI.HasActiveOverlay() && (activeSess == nil || !activeSess.HasAnnotationInputFocused(c)) && c.Shortcut(0, ui.KeyEscape) {
		w.ToggleSidebar(vpW)
		layout.SidebarDrawer = false
	}

	// Window-wide top brand sky band + sidebar-width illustrated landscape painted onto
	// root's own backdrop so the titlebar, sidebar controls, and right floating paper card
	// always sit cleanly above it. Always invoked while !aiImmersive (even when scID == "")
	// so scene switches and on/off toggles crossfade smoothly instead of snapping.
	if !aiImmersive {
		now := time.Now()
		scID := blockui.ResolveBrandScene(stSnap.BrandDeco, pal.Dark, now)
		pauseBrandAnim := !c.WindowActive() || (activeSess != nil && activeSess.IsScrollingForHover(c.Now()))
		sceneW := float32(0)
		if sidebarWide {
			sceneW = max(dockedSidebarWidth, SidebarMinW)
		}
		blockui.PaintBrandSceneBand(root, scID, chrome.Ground, blockui.BrandScenePaintOpts{
			ShowOrb:    sidebarWide && !stSnap.BrandDeco.HideOrb,
			ShowCat:    !stSnap.BrandDeco.HideCat,
			SceneWidth: sceneW,
			Motion:     blockui.ResolveBrandMotion(stSnap.BrandDeco),
			PauseAnim:  pauseBrandAnim,
			Now:        now,
		})
	}

	root.Children(func() {
		// 1. Top 40px Titlebar (hidden in AI Immersive full-window mode).
		if aiImmersive {
			w.tabMenu.Close()
		} else {
			brandHasUpdate, brandUpdateChecking, brandUpdateTip, brandUpdateMenuLabel, brandUpdateHint := w.brandUpdateHint()
			RenderTitlebar(c, TitlebarProps{
				DeferMenus:             w.deferChromePopup,
				SidebarWide:            sidebarWide,
				SidebarAutoHidden:      sidebarAutoHidden,
				SidebarFloating:        w.edgeFloat.LeftOpen() || layout.SidebarDrawer,
				SidebarWidth:           dockedSidebarWidth,
				InSettings:             inSettings,
				SettingsTabLabel:       w.modals.ActiveSettingsTabTitle(),
				RenderSettingsLeading:  w.modals.RenderSettingsTitlebarLeading,
				RenderSettingsTrailing: w.modals.RenderSettingsTitlebarTrailing,
				RenderSettingsOverlays: w.modals.RenderSettingsTitlebarOverlays,
				AIImmersive:            aiImmersive,
				AIEnabled:              aiEnabled,
				AIOpen:                 w.ai.IsOpen() || w.edgeFloat.RightOpen(),
				FeedbackOpen:           w.modals.IsFeedbackOpen(),
				HasFeedbackDraft:       hasFBDraft,
				NotebookPath:           activeNB,
				Tabs:                   tabs,
				ActiveTab:              activeTab,
				IsDirty: func(path string) bool {
					return dirtyTabs[path]
				},
				ActiveSurface:   tbSurface,
				SourceStatus:    srcStatus,
				PreviewZoomPct:  zoomPctStr,
				PreviewZoomFit:  zoomFit,
				SVGShowSource:   svgShowSource,
				NoteSourceView:  w.noteSourceActive(activeTab),
				SourceWrap:      SourceSoftWrap(),
				HasCopyableText: activeSurf != nil && activeSurf.HasCopyableText(),
				CanFormatJSON:   activeSurf != nil && activeSurf.CanFormatJSON(),
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
				OnCloseSettings: func() {
					w.modals.CloseSettings()
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
					w.copyActiveMarkdown(c.Services())
				},
				OnToggleNoteSource: func() {
					w.toggleNoteSource(activeTab, activeSess)
				},
				OnCopyContent: func() {
					if activeSurf == nil {
						return
					}
					if txt := activeSurf.CurrentText(); txt != "" {
						c.Services().WriteClipboard(txt)
						w.notify(theme.Tr("已复制全部内容", "Copied content"), "success")
					}
				},
				OnCopyAsMarkdown: func() {
					if activeSurf == nil {
						return
					}
					if fenced := activeSurf.MarkdownCodeFence(); fenced != "" {
						c.Services().WriteClipboard(fenced)
						w.notify(theme.Tr("已复制 Markdown 代码块", "Copied as Markdown code block"), "success")
					}
				},
				OnFormatJSON: func() {
					if activeSurf == nil {
						return
					}
					changed, err := activeSurf.FormatJSON()
					if err != nil {
						w.notify(fmt.Sprintf(theme.Tr("JSON 格式有误：%v", "Invalid JSON: %v"), err), "error")
						return
					}
					if !changed {
						w.notify(theme.Tr("JSON 已是规范格式", "JSON is already formatted"), "info")
						return
					}
					w.notify(theme.Tr("已格式化 JSON", "Formatted JSON"), "success")
					w.RequestRedraw()
				},
				OnCopyImage: func() {
					if activeTab == "" {
						return
					}
					if err := copyImageFileToClipboard(activeTab, c.Services()); err != nil {
						w.notify(fmt.Sprintf(theme.Tr("复制图片失败：%v", "Failed to copy image: %v"), err), "error")
						return
					}
					w.notify(theme.Tr("已复制图片", "Copied image"), "success")
				},
				OnCopyImageMarkdown: func() {
					if activeTab == "" {
						return
					}
					c.Services().WriteClipboard(markdownImageRef(activeTab, activeNB))
					w.notify(theme.Tr("已复制 Markdown 图片链接", "Copied Markdown image link"), "success")
				},
				OnOpenExternal: func() {
					if activeTab != "" {
						if mygo.App.IsReady() {
							_ = mygo.Shell.OpenPath(activeTab)
						} else {
							c.OpenURL(LocalFileURL(activeTab))
						}
					}
				},
				OnRevealPath: w.revealInShell,
				NoteStats:    noteStats,
				PageStats: func() []PageMenuStat {
					if activeSurf == nil {
						return nil
					}
					return activeSurf.PageMenuStats()
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
				OnToggleSourceWrap: func() {
					ToggleSourceSoftWrap()
					next := SourceSoftWrap()
					if w.modals.OnStateChanged != nil {
						w.modals.OnStateChanged(func(s *model.AppState) { s.SourceSoftWrap = next })
					}
					w.RequestRedraw()
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
							c.OpenURL(LocalFileURL(activeTab))
						}
					}
				},
				HiddenSlots:     w.pluginHiddenTitlebarSlots(),
				AppVersion:      updater.CurrentVersion(),
				HasUpdate:       brandHasUpdate,
				UpdateChecking:  brandUpdateChecking,
				UpdateTooltip:   brandUpdateTip,
				UpdateMenuLabel: brandUpdateMenuLabel,
				UpdateHint:      brandUpdateHint,
				OnOpenUpdater: func() {
					// OpenUpdater already initiates a check when StatusIdle; only force a
					// manual refresh if the updater was already settled at UpToDate/Error.
					forceRefresh := false
					if w.updater != nil {
						snap := w.updater.Snapshot()
						forceRefresh = snap.Status == updater.StatusUpToDate || snap.Status == updater.StatusError
					}
					w.modals.OpenUpdater()
					if forceRefresh && w.updater != nil {
						w.updater.CheckAsync(true)
					}
				},
				OnOpenPluginStore: func() {
					w.OpenSettings("app-store")
				},
				OnOpenAbout:       w.modals.OpenAbout,
				PluginButtons:     w.pluginTitlebarButtons(),
				PinnedPluginSlots: stSnap.PinnedPluginButtons,
				OnTogglePinPlugin: w.togglePinnedPluginButton,
				OnPluginButton:    w.onPluginTitlebarButton,
				PluginPageMenu:    w.pluginPageMenuItems(),
				OnPluginMenuItem:  w.onPluginPageMenuItem,
				PluginTabMenu:     w.pluginTabMenuItems(),
				OnPluginTabMenu:   w.onPluginTabMenuItem,
			})
		}

		// 2. Main Split Body.
		body := ui.Row(c.Key("workspace-body")).Grow(1).MinHeight(0).FillWidth()
		body.Children(func() {
			// Docked Sidebar (File Tree in workspace mode, Category Nav in Settings mode).
			// One persistent slot keeps its width across workspace ↔ settings and only
			// cross-fades its content, so the main column never collapses and regrows.
			// Skipped while the narrow drawer shows the same sidebar, so the two never
			// render it in one frame.
			if !layout.SidebarDrawer {
				blockui.DockPanel(c, "docked-sidebar-slot", !aiImmersive && layout.SidebarDocked, layout.SidebarWidth, false, func() {
					if inSettings {
						ui.Row(c.Key("docked-sidebar-settings")).Shrink(0).FillHeight().
							Transition(blockui.PanelContentSwapTransition()).Children(func() {
							w.modals.RenderSettingsSidebar(c, false, nil)
						})
					} else {
						ui.Row(c.Key("docked-sidebar-workspace")).Shrink(0).FillHeight().
							Transition(blockui.PanelContentSwapTransition()).Children(func() {
							w.renderSidebarColumn(c, stSnap, activeNB, notebooks, entries, activeTab, false)
						})
					}
				})
			}

			// Right Floating Paper Card (Settings Canvas, or Note Editor + Docked AI Panel).
			cardLeft := float32(8)
			if layout.SidebarDocked {
				cardLeft = 0
			}
			aiEligible := aiEnabled && !inSettings && !aiImmersive && w.ai.IsOpen() && activeSurf == nil && activeSess != nil && activeSess.IsMarkdownNote()
			if !aiEligible {
				w.syncAIEditorSelectionHidden(activeTab)
			}
			if inSettings {
				minMainW := max(min(layout.ContentFloor, vpW)-cardLeft-8, 0)
				ui.Box(c.Key("stage-settings-slot")).Grow(1).MinWidth(minMainW).FillHeight().
					Padding(0, 8, 8, cardLeft).Children(func() {
					settingsCard := blockui.Elevate(
						ui.Column(c.Key("stage-settings")).Fill().Radius(theme.RadiusLG).Clip().
							Background(chrome.Card).Border(1, chrome.CardBorder).
							Transition(blockui.SurfaceStageTransition()),
						theme.ElevationCard,
					)
					settingsCard.Children(func() {
						w.modals.RenderSettingsBody(c, layout.SidebarDrawer, func() {
							w.ToggleSidebar(vpW)
						})
					})
				})
			} else if aiImmersive {
				if layout.ChatDocked {
					ui.Column(c.Key("stage-ai-immersive")).Grow(1).MinWidth(0).FillHeight().
						Transition(blockui.SurfaceStageTransition()).Children(func() {
						w.ai.SetRenderWidth(layout.ChatWidth)
						w.ai.RenderPanel(c)
					})
				}
			} else {
				cardHorizMargin := cardLeft + 8
				effMainW := max(layout.MainWidth-cardHorizMargin, 0)
				minMainW := float32(0)
				if !layout.ChatDocked {
					minMainW = max(min(layout.ContentFloor, vpW)-cardHorizMargin, 0)
				}
				ui.Row(c.Key("workspace-panel-slot")).Grow(1).MinWidth(0).FillHeight().
					Padding(0, 8, 8, cardLeft).Children(func() {
					panelCard := blockui.Elevate(
						ui.Row(c.Key("workspace-panel-card")).Fill().Radius(theme.RadiusLG).Clip().
							Background(chrome.Card).Border(1, chrome.CardBorder),
						theme.ElevationCard,
					)
					blockui.Probe(panelCard, "workspace-panel-card")
					panelCard.Children(func() {
						mainCol := ui.Column(c.Key("stage-workspace")).Grow(1).MinWidth(minMainW).FillHeight().
							Background(pal.Paper).Transition(blockui.SurfaceStageTransition())
						mainCol.Children(func() {
							if activeTab != "" && isExternalFile(activeTab, activeNB) {
								renderExternalPathBar(c, activeTab, &w.extPathBar, w.openDirInShell, w.revealInShell, w.notifyPathCopied)
							}
							if toast != nil && toast.Active && toast.NotePath == activeTab {
								toast.RenderBanner(
									c,
									func() {
										w.bgSaveMu.Lock()
										defer w.bgSaveMu.Unlock()
										if sess := w.SessionFor(toast.NotePath); sess != nil && sess.Dirty() {
											w.setNotice(theme.Tr("笔记已有新编辑，无法撤回恢复", "The note has newer edits"))
											return
										}
										if err := files.CheckTextVersion(toast.NotePath, toast.RestoredFingerprint); err != nil {
											w.setNotice(err.Error())
											return
										}
										if err := w.writeNoteLocked(toast.NotePath, toast.UndoContent); err != nil {
											w.setNotice(err.Error())
											return
										}
										w.mu.Lock()
										w.historyToast = nil
										w.mu.Unlock()
									},
									func() {
										w.mu.Lock()
										w.historyToast = nil
										w.mu.Unlock()
										w.RequestRedraw()
									},
								)
							}

							noteArea := ui.Column(c.Key("workspace-note-area")).Grow(1).MinHeight(0).FillWidth()
							noteArea.Children(func() {
								switch {
								case activeSurf != nil:
									if w.pluginUI != nil {
										w.pluginUI.setEditorFocus(activeTab, false)
									}
									activeSurf.Render(
										c,
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
								case activeSess != nil && w.noteSourceActive(activeTab):
									if w.pluginUI != nil {
										w.pluginUI.setEditorFocus(activeTab, false)
									}
									w.renderNoteSource(c)
								case activeSess != nil:
									headlessLock := !mygo.App.IsReady()
									if headlessLock {
										w.headlessMainMu.Lock()
									}
									activeSess.SetEdgeFloatInsets(edgeSafeL, edgeSafeR)
									activeSess.SetOutlineForceNarrow(!layout.ShowOutline || w.edgeFloat.LeftOpen())
									activeSess.SetBackground(w.pluginEditorBackground())
									activeSess.Render(c, effMainW, false)
									if w.pluginUI != nil {
										w.pluginUI.setEditorFocus(activeTab, activeSess.HasFocus(c))
									}
									if aiEligible {
										w.syncAIEditorSelection(c, activeSess)
									}
									if headlessLock {
										w.headlessMainMu.Unlock()
									}
								default:
									if w.pluginUI != nil {
										w.pluginUI.setEditorFocus("", false)
									}
									allowGhostFocus := !inSettings && !w.modals.HasActiveModal() && !w.hasActivePluginModal() &&
										w.sidebarUI.RenamingPath == "" && !layout.SidebarDrawer &&
										!w.edgeFloat.LeftOpen() && !w.edgeFloat.RightOpen()
									w.renderEmptyWorkspaceMascot(c, activeNB, len(entries) == 0, allowGhostFocus)
								}
							})
							renderPluginStatusStrip(c, w.Plugins(), w.activeSelectionText, w.ActiveNotePath)
						})

						aiOpen := w.ai.IsOpen()
						if aiOpen && w.pluginUI != nil {
							w.pluginUI.closePanelLocal("", "")
						}
						pluginPanelOpen := !aiOpen && w.pluginUI != nil && w.pluginUI.hasOpenPanel()
						blockui.DockPanel(c, "docked-ai-slot", layout.ChatDocked && aiOpen, layout.ChatWidth, true, func() {
							chatW := w.ai.PanelWidth()
							maxChat := ChatLimit(vpW-cardHorizMargin, w.sidebarWidth, w.state.SidebarCollapsed, ChatMinW, ChatMaxW)
							if blockui.VerticalResizeHandle(c, "ai-panel-resize", &chatW, ChatMinW, maxChat, true) {
								w.ai.SyncPanelWidth(chatW)
								c.Invalidate()
							}
							w.ai.SetRenderWidth(layout.ChatWidth)
							w.ai.RenderPanel(c)
						})
						if w.pluginUI != nil {
							w.pluginUI.renderDockedPanel(c, layout.ChatDocked && pluginPanelOpen, layout.ChatWidth, vpW, cardHorizMargin)
						}
					})
				})
			}

			// Scheme A Edge-Summoned Floating Sidebar / AI Cards & Colored Edge Hot-Zones.
			w.edgeFloat.RenderBodyOverlays(
				c, body, w, vpW, inSettings, canEdgeLeft, canEdgeRight,
				stSnap, activeNB, notebooks, entries, activeTab, activeSess,
			)

			// Floating Sidebar Card when narrow and explicitly toggled open via button / Alt+B (unified with Scheme A card).
			if layout.SidebarDrawer && !aiImmersive {
				c.Key("sidebar-drawer-scrim")
				scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
					Background(ui.Transparent)
				blockui.Probe(scrim, "sidebar-drawer-scrim")
				if scrim.Clicked() {
					w.ToggleSidebar(vpW)
				}
				efPal := pal.EdgeFloat()
				drawerW := min(ClampSidebarWidth(w.sidebarWidth), max(vpW-40, SidebarMinW))
				c.Key("sidebar-drawer-panel")
				drawer := blockui.Elevate(
					ui.Row(c).Absolute().
						Left(theme.EdgeFloatInset).Top(0).Bottom(theme.EdgeFloatInset).
						Width(drawerW).Radius(theme.RadiusLG).Clip().
						Background(pal.Sidebar).Border(1, efPal.CardBorder).
						Transition(blockui.EdgeFloatCardTransition(false)),
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
						w.renderSidebarColumn(c, stSnap, activeNB, notebooks, entries, activeTab, true)
					}
				})
				renderEdgeHotZone(
					c,
					"edge-hotzone-left",
					false,
					theme.Tr("收起侧栏", "Collapse Sidebar"),
					func() {
						w.ToggleSidebar(vpW)
					},
				)
			}
		})

		// 2.9. Chrome menus (titlebar, sidebar) above editor floating toolbars.
		w.flushChromePopups()

		// 3. Modal Overlays (Search, About, Feedback, Performance, Updater).
		w.modals.RenderModals(c)
		w.renderPluginOverlays(c)

		// 4. Visual Smoke HUD (click ripple + step banner when smoke mode is enabled).
		blockui.RenderSmokeHUD(c)
	})
}

func (w *Workspace) deferChromePopup(fn func()) {
	w.chromePopups = append(w.chromePopups, fn)
}

func (w *Workspace) flushChromePopups() {
	pending := w.chromePopups
	w.chromePopups = w.chromePopups[:0]
	for _, fn := range pending {
		fn()
	}
	clear(pending)
}

func (w *Workspace) hasActivePluginModal() bool {
	if w.palette.IsOpen() {
		return true
	}
	return w.pluginUI != nil && w.pluginUI.hasDialog()
}

func (w *Workspace) activeSelectionText() string {
	if w.ActiveSurface() != nil {
		return ""
	}
	if sess := w.ActiveSession(); sess != nil && sess.IsMarkdownNote() {
		return sess.SelectedMarkdown()
	}
	return ""
}

func (w *Workspace) pluginEditorBackground() editor.BackgroundStyle {
	sup := w.Plugins()
	if sup == nil {
		return editor.BackgroundStyle{}
	}
	bg := sup.EditorBackground()
	style := editor.BackgroundStyle{
		Mode:     bg.Mode,
		Contrast: bg.Contrast,
	}
	if bd, ok := sup.ActiveBackdrop(); ok {
		style.Backdrop = &editor.BackdropSpec{
			FillPaths:   bd.Icon.FillPaths,
			StrokePaths: bd.Icon.StrokePaths,
			AccentPaths: bd.Icon.AccentPaths,
			Pattern:     bd.Pattern,
			Density:     bd.Density,
			Opacity:     bd.Opacity,
			Tint:        bd.Tint,
			DarkGlow:    bd.DarkGlow,
		}
	}
	return style
}

func (w *Workspace) pluginSidebarFileBadges(activeNB string) map[string][]plugins.ResolvedFileBadge {
	sup := w.Plugins()
	if sup == nil || activeNB == "" {
		return nil
	}
	return sup.FileBadgesForNotebook(activeNB)
}

func (w *Workspace) pluginSidebarMenuItems() []plugins.ResolvedMenuItem {
	sup := w.Plugins()
	if sup == nil {
		return nil
	}
	return sup.SidebarMenuItems()
}

func (w *Workspace) onPluginSidebarMenuItem(it plugins.ResolvedMenuItem, targetPath string, isDir bool) {
	sup := w.Plugins()
	if sup == nil || it.Command == "" || targetPath == "" {
		return
	}
	rel := notebookRel(sup.NotebookRoot(), targetPath)
	if rel == "" {
		return
	}
	cc := plugins.CommandContext{File: targetPath, Source: "sidebar_menu"}
	if isDir {
		cc.Folder = rel
	} else {
		cc.Note = rel
	}
	sup.RunCommandAsync(it.PluginID, it.Command, cc)
}

func (w *Workspace) renderPluginSidebarSections(c *ui.Context) {
	if w.pluginsUI != nil {
		w.pluginsUI.RenderSidebarSections(c)
	}
}

func (w *Workspace) pluginHiddenTitlebarSlots() map[string]bool {
	sup := w.Plugins()
	if sup == nil {
		return nil
	}
	var hidden map[string]bool
	for _, id := range []string{
		plugins.TitlebarSlotFeedback,
		plugins.TitlebarSlotAI,
		plugins.TitlebarSlotSendAll,
		plugins.TitlebarSlotWrap,
		plugins.TitlebarSlotZoom,
		plugins.TitlebarSlotReveal,
	} {
		if sup.TitlebarHidden(id) {
			if hidden == nil {
				hidden = make(map[string]bool)
			}
			hidden[id] = true
		}
	}
	return hidden
}

func (w *Workspace) pluginTitlebarButtons() []plugins.ResolvedTitlebarButton {
	sup := w.Plugins()
	if sup == nil {
		return nil
	}
	buttons := sup.TitlebarButtons()
	if w.pluginUI != nil {
		for i := range buttons {
			if buttons[i].Panel != "" && w.pluginUI.isPanelActive(buttons[i].PluginID, buttons[i].Panel) {
				buttons[i].Active = true
			}
		}
	}
	return buttons
}

func (w *Workspace) onPluginTitlebarButton(b plugins.ResolvedTitlebarButton) {
	if b.Panel != "" {
		if w.pluginUI != nil {
			w.pluginUI.togglePanelLocal(b.PluginID, b.Panel)
		}
		return
	}
	if b.Command != "" {
		if sup := w.Plugins(); sup != nil {
			sup.RunCommandAsync(b.PluginID, b.Command, plugins.CommandContext{
				Note:      sup.ActiveNoteRel(),
				File:      w.ActiveNotePath(),
				Selection: w.activeSelectionText(),
				Source:    "titlebar",
			})
		}
	}
}

func (w *Workspace) pluginPageMenuItems() []plugins.ResolvedMenuItem {
	sup := w.Plugins()
	if sup == nil {
		return nil
	}
	return sup.PageMenuItems()
}

func (w *Workspace) onPluginPageMenuItem(it plugins.ResolvedMenuItem) {
	sup := w.Plugins()
	if sup == nil || it.Command == "" {
		return
	}
	sup.RunCommandAsync(it.PluginID, it.Command, plugins.CommandContext{
		Note:      sup.ActiveNoteRel(),
		File:      w.ActiveNotePath(),
		Selection: w.activeSelectionText(),
		Source:    "page_menu",
	})
}

func (w *Workspace) pluginTabMenuItems() []plugins.ResolvedMenuItem {
	sup := w.Plugins()
	if sup == nil {
		return nil
	}
	return sup.TabMenuItems()
}

// onPluginTabMenuItem runs a tab_menu command against the right-clicked tab,
// which may be a file outside the current notebook.
func (w *Workspace) onPluginTabMenuItem(it plugins.ResolvedMenuItem, path string) {
	sup := w.Plugins()
	if sup == nil || it.Command == "" || path == "" {
		return
	}
	sup.RunCommandAsync(it.PluginID, it.Command, plugins.CommandContext{
		Note:   notebookRel(sup.NotebookRoot(), path),
		File:   path,
		Source: "tab_menu",
	})
}

// notebookRel returns path relative to root in slash form, or "" when path is
// not inside root.
func notebookRel(root, path string) string {
	if root == "" || path == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return ""
	}
	return rel
}

func (w *Workspace) renderPluginOverlays(c *ui.Context) {
	if w.pluginUI != nil {
		w.pluginUI.renderToast(c)
		w.pluginUI.renderDialog(c)
	}
	if w.palette.IsOpen() {
		var items []PaletteItem
		if sup := w.Plugins(); sup != nil {
			for _, cmd := range sup.Commands() {
				var ic *plugins.DuoIconSpec
				if len(cmd.Icon.StrokePaths) > 0 {
					cp := cmd.Icon
					ic = &cp
				}
				items = append(items, PaletteItem{
					ID:       cmd.PluginID + "/" + cmd.ID,
					Title:    cmd.Title,
					Subtitle: cmd.PluginName,
					Shortcut: cmd.Shortcut,
					Icon:     ic,
				})
			}
		}
		w.palette.Render(c, items, func(chosen PaletteItem) {
			sup := w.Plugins()
			if sup == nil {
				return
			}
			pluginID, cmdID, ok := strings.Cut(chosen.ID, "/")
			if !ok {
				return
			}
			sup.RunCommandAsync(pluginID, cmdID, plugins.CommandContext{
				Note:      sup.ActiveNoteRel(),
				File:      w.ActiveNotePath(),
				Selection: w.activeSelectionText(),
				Source:    "palette",
			})
		})
	}
}

func (w *Workspace) renderSidebarColumn(
	c *ui.Context,
	stSnap model.AppState,
	activeNB string,
	notebooks []string,
	entries []model.FileEntry,
	activeNote string,
	isDrawer bool,
) {
	hideNonMD := true
	if stSnap.HideNonMarkdownFiles != nil {
		hideNonMD = *stSnap.HideNonMarkdownFiles
	}
	hideAssets := true
	if stSnap.HideAssetsFolder != nil {
		hideAssets = *stSnap.HideAssetsFolder
	}
	RenderSidebar(c, SidebarProps{
		DeferPopups:  w.deferChromePopup,
		State:        w.sidebarUI,
		Width:        &w.sidebarWidth,
		FloatingCard: isDrawer,
		OnWidthChanged: func(nw float32) {
			w.scheduleSaveSidebarWidth(ClampSidebarWidth(nw))
		},
		ActiveNotebook:    activeNB,
		Notebooks:         notebooks,
		Entries:           entries,
		ActiveNote:        activeNote,
		SettingsOpen:      w.modals.IsSettingsOpen(),
		SearchOpen:        w.modals.IsSearchOpen(),
		Busy:              w.isBusy(),
		SortMode:          stSnap.TreeSort,
		HideNonMarkdown:   hideNonMD,
		HideAssets:        hideAssets,
		ShortcutSearch:    modals.EffectiveShortcut(stSnap, "search"),
		ShortcutNewFolder: "cmd-shift-n",
		ShortcutNewNote:   modals.EffectiveShortcut(stSnap, "new_note"),
		OnOpenSearch: func() {
			if isDrawer {
				w.mu.Lock()
				w.sidebarPinned = false
				w.mu.Unlock()
			}
			w.modals.OpenSearch(false)
		},
		OnSetSortMode: func(mode model.TreeSortMode) {
			if w.modals.OnStateChanged != nil {
				w.modals.OnStateChanged(func(s *model.AppState) {
					s.TreeSort = mode
				})
			}
		},
		OnToggleShowNonMD: func() {
			if w.modals.OnStateChanged != nil {
				w.modals.OnStateChanged(func(s *model.AppState) {
					cur := true
					if s.HideNonMarkdownFiles != nil {
						cur = *s.HideNonMarkdownFiles
					}
					next := !cur
					s.HideNonMarkdownFiles = &next
				})
			}
		},
		OnToggleShowAssets: func() {
			if w.modals.OnStateChanged != nil {
				w.modals.OnStateChanged(func(s *model.AppState) {
					cur := true
					if s.HideAssetsFolder != nil {
						cur = *s.HideAssetsFolder
					}
					next := !cur
					s.HideAssetsFolder = &next
				})
			}
		},
		OnNewFolder: func() {
			_, _ = w.CreateNewFolder()
		},
		OnNewNote: func() {
			_, _ = w.CreateNewNote()
		},
		OnNewNoteIn: func(dir string) {
			_, _ = w.CreateNewNoteIn(dir)
		},
		OnOpenNote: func(path string, inNewTab bool) {
			if isDrawer {
				w.mu.Lock()
				w.sidebarPinned = false
				w.mu.Unlock()
			}
			_ = w.openNoteInternal(path, inNewTab, true, 0)
		},
		OnRenameItem: w.finishSidebarRename,
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
		RequestRedraw:    w.RequestRedraw,
		OnOpenSettings: func() {
			w.OpenSettings("ai")
		},
		FileBadges:           w.pluginSidebarFileBadges(activeNB),
		PluginContextMenu:    w.pluginSidebarMenuItems(),
		OnPluginContextMenu:  w.onPluginSidebarMenuItem,
		RenderPluginSections: w.renderPluginSidebarSections,
	})
}

// Updater returns the application update service.
func (w *Workspace) Updater() *updater.Service {
	return w.updater
}

func (w *Workspace) togglePinnedPluginButton(slotID string) bool {
	slotID = strings.TrimSpace(slotID)
	if slotID == "" {
		return false
	}
	var activeSlots map[string]bool
	if buttons := w.pluginTitlebarButtons(); len(buttons) > 0 {
		activeSlots = make(map[string]bool, len(buttons))
		for _, b := range buttons {
			activeSlots[pluginButtonSlotID(b)] = true
		}
	}
	next, err := files.UpdateState(func(s *model.AppState) {
		s.PinnedPluginButtons = TogglePinnedPluginSlot(s.PinnedPluginButtons, slotID, activeSlots)
	})
	if err != nil {
		files.LogWarnf("pinned plugin buttons save failed: %v", err)
		msg := theme.Tr("保存固定设置失败", "Failed to save pinned buttons")
		w.notify(msg, "error")
		w.RequestRedraw()
		return false
	}
	w.mu.Lock()
	w.state.PinnedPluginButtons = next.PinnedPluginButtons
	w.mu.Unlock()
	w.RequestRedraw()
	return true
}

// brandUpdateHint reports whether the titlebar brand mark should carry the update dot,
// whether an update check is currently in flight, and concise labels for tooltip & menu.
func (w *Workspace) brandUpdateHint() (hasUpdate, checking bool, tip, menuLabel, menuHint string) {
	if w.updater == nil {
		return false, false, "", "", ""
	}
	snap := w.updater.Snapshot()
	if snap.Status == updater.StatusChecking {
		return false, true, "", theme.Tr("正在检查更新…", "Checking for Updates…"), ""
	}
	if !snap.HasActionableUpdate() {
		return false, false, "", "", ""
	}
	ver := ""
	if snap.LatestVersion != "" {
		ver = "v" + snap.LatestVersion
	}
	switch snap.Status {
	case updater.StatusDownloading:
		return true, false,
			fmt.Sprintf(theme.Tr("正在下载 %s · %d%%", "Downloading %s · %d%%"), nonEmptyOr(ver, theme.Tr("更新", "update")), snap.Percent),
			theme.Tr("正在下载更新", "Downloading Update"),
			fmt.Sprintf("%d%%", snap.Percent)
	case updater.StatusInstalling:
		return true, false,
			theme.Tr("正在保存笔记并重启…", "Saving notes and restarting…"),
			theme.Tr("正在重启安装…", "Restarting to Install…"),
			""
	case updater.StatusReady:
		return true, false,
			fmt.Sprintf(theme.Tr("新版本 %s 已就绪", "Update %s is ready"), nonEmptyOr(ver, theme.Tr("更新", "update"))),
			theme.Tr("新版本已就绪", "Update Ready"),
			ver
	default:
		return true, false,
			fmt.Sprintf(theme.Tr("发现新版本 %s", "Update %s available"), nonEmptyOr(ver, theme.Tr("更新", "update"))),
			theme.Tr("发现新版本", "Update Available"),
			ver
	}
}

func (w *Workspace) shortcutForSidebar(stSnap model.AppState) string {
	return modals.EffectiveShortcut(stSnap, "new_note")
}

func (w *Workspace) revealInShell(path string) {
	if path != "" && mygo.App.IsReady() {
		mygo.Shell.ShowItemInFolder(path)
	}
}

func (w *Workspace) openDirInShell(path string) {
	if path != "" && mygo.App.IsReady() {
		_ = mygo.Shell.OpenPath(path)
	}
}

func (w *Workspace) renderEmptyWorkspaceMascot(c *ui.Context, activeNB string, emptyFolder, allowGhostFocus bool) {
	pal := theme.Ocean()
	st := w.state
	c.Key("workspace-empty")
	if activeNB != "" && !emptyFolder {
		w.renderGhostPage(c, st, allowGhostFocus)
		return
	}
	box := blockui.EmptyStage(c, pal.Paper, theme.EmptyStateWidth, func() {
		if activeNB == "" {
			icons.Mascot(c, icons.MascotNoFolder, theme.EmptyMascotSize)
			blockui.EmptyHeading(c, theme.Tr("打开一个文件夹开始记录", "Open a folder to start writing"))
			blockui.EmptyLead(c, theme.Tr("笔记就是这个文件夹里的 Markdown 文件。", "Notes are the Markdown files in that folder."))
			blockui.EmptyActions(c, func() {
				if blockui.EmptyActionGlyphRow(c, "empty-choose-folder", theme.Tr("选择文件夹…", "Choose Folder…"), icons.FolderOpen, "").Clicked() {
					w.ChooseNotebookDialog()
				}
				recent := make([]string, 0, 3)
				for _, nb := range st.Notebooks {
					if nb != "" && len(recent) < 3 {
						recent = append(recent, nb)
					}
				}
				if len(recent) > 0 {
					blockui.EmptyActionSeparator(c)
				}
				for i, nb := range recent {
					title := noteDisplayTitle(nb, true)
					if blockui.EmptyActionRow(c, fmt.Sprintf("empty-recent-notebook-%d", i), title, func() {
						blockui.MonogramBadge(c, title)
					}, "").Clicked() {
						_ = w.SetNotebook(nb)
					}
				}
			})
			return
		}
		icons.Mascot(c, icons.MascotEmptyFolder, theme.EmptyMascotSize)
		blockui.EmptyHeading(c, theme.Tr("这个笔记本还没有笔记", "This notebook has no notes yet"))
		blockui.EmptyActions(c, func() {
			if blockui.EmptyActionGlyphRow(c, "empty-new-note", theme.Tr("新建笔记", "New Note"), icons.NotePlus, modals.EffectiveShortcut(st, "new_note")).Clicked() {
				_, _ = w.CreateNewNote()
			}
			if blockui.EmptyActionGlyphRow(c, "empty-new-folder", theme.Tr("新建文件夹", "New Folder"), icons.FolderPlus, "").Clicked() {
				_, _ = w.CreateNewFolder()
			}
			if blockui.EmptyActionGlyphRow(c, "empty-switch-notebook", theme.Tr("切换到其他笔记本", "Switch Notebook"), icons.FolderOpen, "").Clicked() {
				w.ChooseNotebookDialog()
			}
		})
	})
	blockui.Probe(box, "workspace-empty")
}

// renderGhostPage is the no-note state: a blank page whose title field creates
// a note on Enter (named after what was typed, or the default name).
func (w *Workspace) renderGhostPage(c *ui.Context, st model.AppState, allowAutoFocus bool) {
	pal := theme.Ocean()
	box := blockui.EmptyPageStage(c, pal.Paper, theme.EditorStandardWidth, func() {
		in := ui.TextInputBase(c.Key("empty-ghost-title"), &w.ghostTitle).
			FillWidth().Padding(0).Label(theme.Tr("新笔记标题", "New note title")).
			Placeholder(theme.Tr("未命名", "Untitled")).PlaceholderColor(pal.ListBullet).
			FontSize(theme.FontDisplay).FixedLineHeight(theme.LineDisplay).FontWeight(theme.WeightTitle).
			TextColor(pal.Text).CaretColor(pal.Accent)
		if allowAutoFocus {
			in.AutoFocus()
		}
		blockui.Probe(in, "empty-ghost-title")
		if in.Submitted() && w.createTitledNote(w.ghostTitle) {
			w.ghostTitle = ""
		}
		if in.Shortcut(0, ui.KeyEscape) {
			w.ghostTitle = ""
		}
		ui.Text(c, theme.Tr("直接输入就会新建一篇笔记，也可以从左侧选一篇。", "Type a title and press Return to create a note, or pick one on the left.")).
			FillWidth().MinWidth(0).FontSize(theme.FontEditor).FixedLineHeight(theme.LineEditor).TextColor(pal.TextSecondary).Margin(theme.Space12, 0, 0, 0)
		ui.Row(c).Gap(theme.Space20).AlignItems(ui.Center).Wrap().Margin(theme.Space24, 0, 0, 0).Children(func() {
			blockui.EmptyHint(c, func() { blockui.Keycap(c, "/") }, theme.Tr("插入块", "Insert block"))
			if sc := modals.EffectiveShortcut(st, "search"); sc != "" {
				blockui.EmptyHint(c, func() { blockui.ShortcutKeycap(c, sc) }, theme.Tr("搜索", "Search"))
			}
			if sc := modals.EffectiveShortcut(st, "toggle_ai"); sc != "" {
				blockui.EmptyHint(c, func() { blockui.ShortcutKeycap(c, sc) }, theme.Tr("AI 助手", "AI Assistant"))
			}
		})
	})
	blockui.Probe(box, "workspace-empty")
}

// createTitledNote creates a note in the target folder, names it title when
// given, opens it with the caret in the body, and reports success. A title
// that can't be used as a file name leaves nothing behind.
func (w *Workspace) createTitledNote(title string) bool {
	title = strings.TrimSpace(title)
	if ext := filepath.Ext(title); strings.EqualFold(ext, ".md") {
		title = strings.TrimSpace(strings.TrimSuffix(title, ext))
	}
	if title == "" {
		_, err := w.CreateNewNote()
		return err == nil
	}
	folder := w.ActiveNotebook()
	if folder == "" {
		return false
	}
	created, err := files.CreateNewNote(w.targetCreateDir(folder), theme.IsEn())
	if err != nil {
		w.setNotice(err.Error())
		return false
	}
	path, err := w.RenameWorkspaceItem(created, title)
	if err != nil {
		_ = os.Remove(created)
		w.RescanNotebook()
		return false
	}
	if err := w.openNoteInternal(path, true, true, 0); err != nil {
		w.setNotice(err.Error())
	} else if sess := w.SessionFor(path); sess != nil {
		sess.FocusBlock(0)
	}
	return true
}

func (w *Workspace) syncAIEditorSelectionHidden(activeTab string) {
	if activeTab != w.aiSelNote {
		w.ai.ClearEditorSelection()
		w.aiSelNote = activeTab
	} else {
		w.ai.HideEditorSelection()
	}
	w.hasAISelKey = false
}

func (w *Workspace) syncAIEditorSelection(c *ui.Context, sess *editor.Session) {
	notePath, docRev, anchor, head, nonEmpty := sess.SelectionKey()
	if !nonEmpty {
		w.hasAISelKey = false
		if notePath != w.aiSelNote || sess.HasFocus(c) {
			w.ai.ClearEditorSelection()
			w.aiSelNote = notePath
		}
		return
	}
	key := aiSelectionSyncKey{
		notePath: notePath,
		docRev:   docRev,
		anchor:   anchor,
		head:     head,
	}
	if w.hasAISelKey && w.aiSelKey == key {
		return
	}
	noteChanged := notePath != w.aiSelNote
	w.aiSelKey = key
	w.hasAISelKey = true
	w.aiSelNote = notePath

	md, preview, startLine, endLine, ok := sess.ExportSelectionSnapshot()
	if !ok {
		if noteChanged || sess.HasFocus(c) {
			w.ai.ClearEditorSelection()
		} else {
			w.ai.HideEditorSelection()
		}
		return
	}
	w.ai.SetEditorSelection(&ai.EditorSelection{
		NotePath:     notePath,
		Markdown:     md,
		PlainPreview: preview,
		StartLine:    startLine,
		EndLine:      endLine,
		AnchorBlock:  anchor.Block,
		AnchorOffset: anchor.Offset,
		HeadBlock:    head.Block,
		HeadOffset:   head.Offset,
	})
}
