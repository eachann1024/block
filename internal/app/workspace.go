package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/internal/ai"
	"block/internal/editor"
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
// notebooks & file tree, multi-tab Block Editor sessions, AI Assistant panel,
// Settings & Modal dialogs, Quick Note floating window, shortcuts, and file watching.
type Workspace struct {
	mu sync.RWMutex

	dataDir string
	win     *mygo.Window

	state          model.AppState
	sidebarWidth   float32
	sidebarPinned  bool
	zoomPct        int
	entries        []model.FileEntry
	notice         string
	virtualPreview map[string]bool // createNote preview tabs not yet written to disk

	codec    *markdown.Codec
	sessions map[string]*editor.Session
	nav      *NavHistory
	closed   *ClosedTabs
	tabOps   TabStripOps

	tabMenu   *TabMenuState
	sidebarUI *SidebarUIState

	ai     *ai.Controller
	modals *modals.Manager
	quick  *modals.QuickNoteController

	watchCancel context.CancelFunc
	lastSnapAt  map[string]time.Time
}

// NewWorkspace constructs and hydrates a Workspace backed by dataDir (or platform default when empty).
func NewWorkspace(dataDir string) *Workspace {
	if strings.TrimSpace(dataDir) != "" {
		files.SetDataDirOverride(dataDir)
	}
	actualDir := files.AppDataDir()
	st := files.LoadState()
	applyThemeFromState(st)

	codec := markdown.NewCodec()
	w := &Workspace{
		dataDir:        actualDir,
		state:          st,
		sidebarWidth:   SidebarDefaultW,
		zoomPct:        100,
		virtualPreview: make(map[string]bool),
		codec:          codec,
		sessions:       make(map[string]*editor.Session),
		nav:            NewNavHistory(),
		closed:         NewClosedTabs(),
		tabMenu:        &TabMenuState{},
		sidebarUI:      NewSidebarUIState(),
		lastSnapAt:     make(map[string]time.Time),
	}
	if st.SidebarWidth != nil && *st.SidebarWidth > 0 {
		w.sidebarWidth = ClampSidebarWidth(*st.SidebarWidth)
	}

	w.ai = ai.NewController(actualDir, w, codec)
	w.modals = modals.NewManager(actualDir, w)
	w.quick = modals.NewQuickNoteController(actualDir, w, codec)

	w.wireModalCallbacks()
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
	theme.SetTypography(st.EditorFont, st.CodeFont, st.LineHeight)
	theme.SetFolderRules(st.FolderRules)
	theme.SetIgnoredSearchFolders(st.IgnoredSearchFolders)
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
		w.RequestRedraw()
	}
	w.modals.RenderAISettings = func(c *ui.Context) {
		w.ai.RenderSettingsTab(c)
	}
	w.modals.SearchFn = func(query string, allNotebooks bool) ([]model.SearchHit, error) {
		return w.SearchWorkspace(query, allNotebooks)
	}
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

func (w *Workspace) hydrateInitialWorkspace() {
	if w.state.Folder != "" {
		if info, err := os.Stat(w.state.Folder); err == nil && info.IsDir() {
			w.RescanNotebook()
		}
	}
	validTabs := make([]string, 0, len(w.state.OpenTabs))
	for _, p := range w.state.OpenTabs {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			validTabs = append(validTabs, filepath.Clean(p))
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
	w.state.OpenTabs = validTabs
	w.state.ActiveTab = active
	if active != "" {
		_ = w.ensureSessionLoaded(active)
		w.nav.Push(active)
	}
}

// AttachWindow binds the main *mygo.Window, starts folder file watching, and attaches Quick Note.
func (w *Workspace) AttachWindow(win *mygo.Window) {
	w.mu.Lock()
	w.win = win
	folder := w.state.Folder
	w.mu.Unlock()

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

// Modals returns the workspace's Modals & Settings Manager.
func (w *Workspace) Modals() *modals.Manager { return w.modals }

// QuickNote returns the workspace's Quick Note floating window controller.
func (w *Workspace) QuickNote() *modals.QuickNoteController { return w.quick }

// Codec returns the workspace's MarkdownCodec.
func (w *Workspace) Codec() *markdown.Codec { return w.codec }

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

// SessionFor returns the active *editor.Session for path, loading it from disk if needed.
func (w *Workspace) SessionFor(path string) *editor.Session {
	if path == "" {
		return nil
	}
	clean := filepath.Clean(path)
	w.mu.RLock()
	sess := w.sessions[clean]
	w.mu.RUnlock()
	if sess != nil {
		return sess
	}
	_ = w.ensureSessionLoaded(clean)
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sessions[clean]
}

// ActiveSession returns the *editor.Session of the currently active tab, or nil.
func (w *Workspace) ActiveSession() *editor.Session {
	return w.SessionFor(w.ActiveNotePath())
}

func (w *Workspace) ensureSessionLoaded(path string) error {
	clean := filepath.Clean(path)
	w.mu.RLock()
	if _, ok := w.sessions[clean]; ok {
		w.mu.RUnlock()
		return nil
	}
	w.mu.RUnlock()

	raw, _, err := files.ReadNote(clean)
	if err != nil {
		return err
	}
	doc := w.codec.Parse(raw)
	sess := editor.NewSession(clean, doc, w.codec, w)
	w.mu.Lock()
	w.sessions[clean] = sess
	w.mu.Unlock()
	return nil
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
		roots = append(roots, w.state.Notebooks...)
	} else if w.state.Folder != "" {
		roots = append(roots, w.state.Folder)
	}
	opts := w.scanOptionsLocked()
	w.mu.RUnlock()
	return files.SearchNotebooks(roots, query, opts)
}

func (w *Workspace) ReadNote(absPath string) (string, error) {
	clean := filepath.Clean(absPath)
	w.mu.RLock()
	sess := w.sessions[clean]
	w.mu.RUnlock()
	if sess != nil {
		return sess.Markdown(), nil
	}
	content, _, err := files.ReadNote(clean)
	return content, err
}

func (w *Workspace) WriteNote(absPath string, content string) error {
	clean := filepath.Clean(absPath)
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	if _, err := files.WriteNoteAtomic(clean, content); err != nil {
		return err
	}
	files.ClearRecovery(clean)
	_, _ = files.RecordHistorySnapshot(clean, content, theme.Tr("保存", "Saved"))

	doc := w.codec.Parse(content)
	w.mu.Lock()
	delete(w.virtualPreview, clean)
	if sess, ok := w.sessions[clean]; ok {
		sess.SetDoc(doc)
		sess.MarkSaved()
	} else {
		w.sessions[clean] = editor.NewSession(clean, doc, w.codec, w)
	}
	w.mu.Unlock()

	w.RescanNotebook()
	w.RequestRedraw()
	return nil
}

func (w *Workspace) OpenNote(absPath string, lineHint int) error {
	return w.openNoteInternal(absPath, false, true, lineHint)
}

func (w *Workspace) openNoteInternal(absPath string, forceNewTab bool, recordNav bool, lineHint int) error {
	if strings.TrimSpace(absPath) == "" {
		return errors.New("empty note path")
	}
	clean := filepath.Clean(absPath)
	if err := w.ensureSessionLoaded(clean); err != nil {
		return err
	}

	w.modals.CloseSettings()
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()

	w.mu.Lock()
	// Expand ancestor directories inside active notebook so the note is visible in tree.
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

	if forceNewTab || len(w.state.OpenTabs) == 0 {
		w.state.OpenTabs, w.state.ActiveTab = w.tabOps.OpenOrFocus(w.state.OpenTabs, w.state.ActiveTab, clean)
	} else {
		already := false
		for _, t := range w.state.OpenTabs {
			if t == clean {
				already = true
				break
			}
		}
		if already {
			w.state.ActiveTab = clean
		} else {
			w.state.OpenTabs, w.state.ActiveTab = w.tabOps.OpenOrFocus(w.state.OpenTabs, w.state.ActiveTab, clean)
		}
	}
	sess := w.sessions[clean]
	tabs := append([]string(nil), w.state.OpenTabs...)
	active := w.state.ActiveTab
	w.mu.Unlock()

	if recordNav {
		w.nav.Push(clean)
	}
	if sess != nil && lineHint > 0 {
		blockIdx := min(max(lineHint-1, 0), max(len(sess.Doc().Blocks)-1, 0))
		sess.FocusBlock(blockIdx)
	}
	w.persistTabs(tabs, active)
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
		// Virtual createNote preview: do NOT write to disk until accepted!
		previewDoc := w.codec.Parse(plan.Ops[0].New)
		sess := editor.NewSession(firstTarget, previewDoc, w.codec, w)
		sess.SetActivePlan(plan)

		w.mu.Lock()
		w.virtualPreview[firstTarget] = true
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

		// If the caller (e.g. ai.Controller.AcceptPlan) already wrote selective diff text to disk,
		// prefer the freshly written content when op.Kind == PlanOpCreateNote or op.Old is absent.
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
		if sess, ok := w.sessions[target]; ok {
			sess.SetActivePlan(nil)
			sess.SetDoc(doc)
			sess.MarkSaved()
		} else {
			w.sessions[target] = editor.NewSession(target, doc, w.codec, w)
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
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
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

// --- Notebook, File, and Tab Operations ---

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

	next, saveErr := files.UpdateState(func(st *model.AppState) {
		st.Folder = clean
		// Prepend or deduplicate in NormalizeState.
		st.Notebooks = append([]string{clean}, st.Notebooks...)
	})
	w.mu.Lock()
	if saveErr == nil {
		w.state = next
	} else {
		w.state.Folder = clean
	}
	w.mu.Unlock()

	w.RescanNotebook()
	if w.win != nil {
		w.startWatching(clean)
	}
	w.RequestRedraw()
	return nil
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

// RescanNotebook rescans the active notebook directory using current ScanOptions.
func (w *Workspace) RescanNotebook() {
	w.mu.RLock()
	folder := w.state.Folder
	opts := w.scanOptionsLocked()
	w.mu.RUnlock()
	if folder == "" {
		w.mu.Lock()
		w.entries = nil
		w.mu.Unlock()
		return
	}
	list, err := files.ScanNotebook(folder, opts)
	w.mu.Lock()
	if err != nil {
		w.notice = err.Error()
	} else {
		w.notice = ""
		w.entries = list
	}
	w.mu.Unlock()
}

// CreateNewNote creates an Untitled.md / 未命名.md note in the active notebook,
// opens it in a tab, and focuses block 0.
func (w *Workspace) CreateNewNote() (string, error) {
	folder := w.ActiveNotebook()
	if folder == "" {
		return "", errors.New(theme.Tr("请先打开一个笔记本文件夹", "Open a notebook folder first"))
	}
	created, err := files.CreateNewNote(folder, theme.IsEn())
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	w.RescanNotebook()
	if err := w.openNoteInternal(created, true, true, 1); err != nil {
		return created, err
	}
	if sess := w.SessionFor(created); sess != nil {
		sess.FocusBlock(0)
	}
	return created, nil
}

// CreateNewFolder creates a new subfolder in the active notebook and starts inline rename.
func (w *Workspace) CreateNewFolder() (string, error) {
	folder := w.ActiveNotebook()
	if folder == "" {
		return "", errors.New(theme.Tr("请先打开一个笔记本文件夹", "Open a notebook folder first"))
	}
	created, err := files.CreateNewFolder(folder, theme.IsEn())
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	w.RescanNotebook()
	w.sidebarUI.StartRename(created, true)
	w.RequestRedraw()
	return created, nil
}

// RenameWorkspaceItem renames oldPath to newName on disk and rebases open tabs, sessions, and history.
func (w *Workspace) RenameWorkspaceItem(oldPath, newName string) (string, error) {
	w.FlushDirtySessions()
	newPath, err := files.RenameItem(oldPath, newName)
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	w.rebaseRenamedPaths(oldPath, newPath)
	w.RescanNotebook()
	w.RequestRedraw()
	return newPath, nil
}

// MoveWorkspaceItem moves srcPath into dstDir and rebases open tabs, sessions, and history.
func (w *Workspace) MoveWorkspaceItem(srcPath, dstDir string) (string, error) {
	w.FlushDirtySessions()
	newPath, err := files.MoveItem(srcPath, dstDir)
	if err != nil {
		w.setNotice(err.Error())
		return "", err
	}
	w.sidebarUI.ExpandedDirs[filepath.Clean(dstDir)] = true
	w.rebaseRenamedPaths(srcPath, newPath)
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
	for i, t := range w.state.OpenTabs {
		if next, ok := rebase(t); ok {
			w.state.OpenTabs[i] = next
			w.nav.ReplacePath(t, next)
		}
	}
	if next, ok := rebase(w.state.ActiveTab); ok {
		w.state.ActiveTab = next
	}
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
			w.closed.Remove(k)
			w.nav.RemovePath(k)
		}
	}
	surviving := make([]string, 0, len(w.state.OpenTabs))
	for _, t := range w.state.OpenTabs {
		if !matches(t) {
			surviving = append(surviving, t)
		}
	}
	w.state.OpenTabs = surviving
	if matches(w.state.ActiveTab) {
		if len(surviving) > 0 {
			w.state.ActiveTab = surviving[len(surviving)-1]
		} else {
			w.state.ActiveTab = ""
		}
	}
	tabs := append([]string(nil), w.state.OpenTabs...)
	active := w.state.ActiveTab
	w.mu.Unlock()

	w.persistTabs(tabs, active)
}

// CloseTab flushes dirty changes on path, pushes it to ClosedTabs, and closes the tab.
func (w *Workspace) CloseTab(path string) {
	clean := filepath.Clean(path)
	w.flushSingleSession(clean)

	w.mu.Lock()
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
	if !isVirtual {
		w.closed.Push(clean)
	}
	tabs := append([]string(nil), nextTabs...)
	w.mu.Unlock()

	if nextActive != "" {
		_ = w.ensureSessionLoaded(nextActive)
	}
	w.persistTabs(tabs, nextActive)
	w.RequestRedraw()
}

// CloseActiveTab closes the active tab (or closes window when 0 tabs remain).
func (w *Workspace) CloseActiveTab() {
	active := w.ActiveNotePath()
	if active != "" {
		w.CloseTab(active)
	}
}

// ReopenClosedTab pops the most recently closed tab (`Cmd+Shift+T`) and opens it.
func (w *Workspace) ReopenClosedTab() bool {
	for w.closed.Len() > 0 {
		cand := w.closed.Pop()
		if cand == "" {
			break
		}
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			_ = w.openNoteInternal(cand, true, true, 0)
			return true
		}
	}
	return false
}

// CloseOtherTabs closes every open tab except keepPath.
func (w *Workspace) CloseOtherTabs(keepPath string) {
	w.FlushDirtySessions()
	clean := filepath.Clean(keepPath)
	w.mu.Lock()
	nextTabs, nextActive, closedPaths := w.tabOps.CloseOthers(w.state.OpenTabs, clean)
	for _, p := range closedPaths {
		if !w.virtualPreview[p] {
			w.closed.Push(p)
		}
		delete(w.virtualPreview, p)
		delete(w.sessions, p)
	}
	w.state.OpenTabs = nextTabs
	w.state.ActiveTab = nextActive
	tabs := append([]string(nil), nextTabs...)
	w.mu.Unlock()
	w.persistTabs(tabs, nextActive)
	w.RequestRedraw()
}

// CloseTabsToRight closes every open tab strictly to the right of pivotPath.
func (w *Workspace) CloseTabsToRight(pivotPath string) {
	w.FlushDirtySessions()
	clean := filepath.Clean(pivotPath)
	w.mu.Lock()
	nextTabs, nextActive, closedPaths := w.tabOps.CloseToRight(w.state.OpenTabs, w.state.ActiveTab, clean)
	for _, p := range closedPaths {
		if !w.virtualPreview[p] {
			w.closed.Push(p)
		}
		delete(w.virtualPreview, p)
		delete(w.sessions, p)
	}
	w.state.OpenTabs = nextTabs
	w.state.ActiveTab = nextActive
	tabs := append([]string(nil), nextTabs...)
	w.mu.Unlock()
	w.persistTabs(tabs, nextActive)
	w.RequestRedraw()
}

// CloseAllTabs closes all open tabs.
func (w *Workspace) CloseAllTabs() {
	w.FlushDirtySessions()
	w.mu.Lock()
	for _, p := range w.state.OpenTabs {
		if !w.virtualPreview[p] {
			w.closed.Push(p)
		}
	}
	w.virtualPreview = make(map[string]bool)
	w.sessions = make(map[string]*editor.Session)
	w.state.OpenTabs = nil
	w.state.ActiveTab = ""
	w.mu.Unlock()
	w.persistTabs(nil, "")
	w.RequestRedraw()
}

// StepTab cycles forward (+1) or backward (-1) across open tabs (`Ctrl+Tab` / `Ctrl+Shift+Tab`).
func (w *Workspace) StepTab(delta int) {
	w.mu.RLock()
	next := w.tabOps.Cycle(w.state.OpenTabs, w.state.ActiveTab, delta)
	w.mu.RUnlock()
	if next != "" {
		_ = w.openNoteInternal(next, false, true, 0)
	}
}

// SelectTabOneBased selects tab 1..9 (`Cmd+9` selects the last tab).
func (w *Workspace) SelectTabOneBased(num int) {
	w.mu.RLock()
	next := w.tabOps.SelectOneBased(w.state.OpenTabs, num)
	w.mu.RUnlock()
	if next != "" {
		_ = w.openNoteInternal(next, false, true, 0)
	}
}

// NavigateBack navigates backward in note history (`Cmd+[`).
func (w *Workspace) NavigateBack() {
	if target := w.nav.Back(); target != "" {
		_ = w.openNoteInternal(target, false, false, 0)
	}
}

// NavigateForward navigates forward in note history (`Cmd+]`).
func (w *Workspace) NavigateForward() {
	if target := w.nav.Forward(); target != "" {
		_ = w.openNoteInternal(target, false, false, 0)
	}
}

// ToggleSidebar collapses or expands (or opens/closes drawer on narrow viewports) the left sidebar (`Alt+B`).
func (w *Workspace) ToggleSidebar(viewportWidth float32) {
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()

	w.mu.Lock()
	layout := ResolveShellLayout(w.shellInputLocked(viewportWidth))
	if layout.SidebarDrawer {
		// Dismissing the narrow-viewport floating drawer keeps SidebarCollapsed=false
		// so widening the window re-docks the sidebar naturally.
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

// SetZoomPct sets the content zoom percentage (90, 100, or 110).
func (w *Workspace) SetZoomPct(pct int) {
	if pct < 50 {
		pct = 90
	} else if pct > 200 {
		pct = 110
	}
	w.mu.Lock()
	w.zoomPct = pct
	w.mu.Unlock()
	theme.SetZoom(float32(pct) / 100.0)
	w.RequestRedraw()
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

// FlushDirtySessions writes all modified non-virtual editor sessions to disk atomically.
func (w *Workspace) FlushDirtySessions() {
	w.mu.RLock()
	paths := make([]string, 0, len(w.sessions))
	for p, sess := range w.sessions {
		if sess.Dirty() && !w.virtualPreview[p] {
			paths = append(paths, p)
		}
	}
	w.mu.RUnlock()
	for _, p := range paths {
		w.flushSingleSession(p)
	}
}

func (w *Workspace) flushSingleSession(path string) {
	w.mu.RLock()
	sess := w.sessions[path]
	isVirtual := w.virtualPreview[path]
	lastSnap := w.lastSnapAt[path]
	w.mu.RUnlock()
	if sess == nil || !sess.Dirty() || isVirtual {
		return
	}
	md := sess.Markdown()
	if _, err := files.WriteNoteAtomic(path, md); err != nil {
		files.LogWarnf("auto-save failed for %s: %v", path, err)
		_ = files.SaveRecovery(path, md)
		return
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
		for {
			_, err := watcher.Next(ctx)
			if err != nil {
				return
			}
			w.RescanNotebook()
			w.RequestRedraw()
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
	// Never steal keystrokes while Settings is recording a custom shortcut!
	if w.modals.IsRecordingShortcut() {
		return
	}

	w.mu.RLock()
	customSearch := w.state.ShortcutSearch
	w.mu.RUnlock()

	matchSpec := func(custom, fallback string) bool {
		raw := fallback
		if strings.TrimSpace(custom) != "" {
			raw = custom
		}
		mods, key, ok := blockui.ParseShortcut(raw)
		if !ok {
			return false
		}
		return c.Shortcut(mods, key)
	}

	if matchSpec(customSearch, "cmd-k") {
		w.modals.OpenSearch(false)
		return
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyK) {
		w.modals.OpenSearch(true)
		return
	}
	if matchSpec("", "cmd-,") {
		if w.modals.IsSettingsOpen() {
			w.modals.CloseSettings()
		} else {
			w.OpenSettings("ai")
		}
		return
	}
	if matchSpec("", "cmd-shift-m") {
		if w.modals.IsPerformanceOpen() {
			w.modals.ClosePerformance()
		} else {
			w.modals.OpenPerformance()
		}
		return
	}
	if matchSpec("", "cmd-j") {
		w.ToggleAIPanel()
		return
	}
	if matchSpec("", "alt-b") || c.Shortcut(ui.Cmd, ui.KeyBackslash) {
		w.ToggleSidebar(vpW)
		return
	}
	if matchSpec("", "cmd-n") || matchSpec("", "cmd-t") {
		_, _ = w.CreateNewNote()
		return
	}
	if matchSpec("", "cmd-shift-n") {
		_, _ = w.CreateNewFolder()
		return
	}
	if matchSpec("", "cmd-w") {
		w.CloseActiveTab()
		return
	}
	if matchSpec("", "cmd-shift-t") {
		w.ReopenClosedTab()
		return
	}
	if matchSpec("", "cmd-shift-e") {
		if active := w.ActiveNotePath(); active != "" {
			w.modals.OpenExport(active)
		}
		return
	}
	if c.Shortcut(ui.Ctrl, ui.KeyTab) {
		w.StepTab(1)
		return
	}
	if c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab) {
		w.StepTab(-1)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyBracketLeft) {
		w.NavigateBack()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyBracketRight) {
		w.NavigateForward()
		return
	}
	if c.Shortcut(ui.Cmd, ui.Key0) {
		w.SetZoomPct(100)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyEqual) {
		w.SetZoomPct(110)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyMinus) {
		w.SetZoomPct(90)
		return
	}
	for n := 1; n <= 9; n++ {
		if c.Shortcut(ui.Cmd, ui.Key0+ui.Key(n)) {
			w.SelectTabOneBased(n)
			return
		}
	}
}

// Render renders the complete main window UI for one frame.
func (w *Workspace) Render(c *ui.Context) {
	blockui.ClearTargets()

	zoom := float32(w.ZoomPct()) / 100.0
	if zoom <= 0 {
		zoom = 1.0
	}
	if c.Zoom() != zoom {
		c.SetZoom(zoom)
	}
	theme.SetZoom(zoom)

	edFont, _, _ := theme.Typography()
	uiTheme := theme.Ocean().ToUITheme(edFont)
	c.SetTheme(&uiTheme)

	vpW, _ := c.Size()
	if vpW <= 0 {
		vpW = 1100
	}

	// Route global workspace shortcuts before child inputs when appropriate.
	w.handleAppShortcuts(c, vpW)

	// Flush dirty active editor changes seamlessly so disk state stays live.
	w.FlushDirtySessions()

	w.mu.Lock()
	in := w.shellInputLocked(vpW)
	if w.sidebarPinned && in.SidebarDesiredW+NoteFloor <= vpW+0.5 && !in.InSettings {
		w.sidebarPinned = false
		in.SidebarPinned = false
	}
	layout := ResolveShellLayout(in)
	tabs := append([]string(nil), w.state.OpenTabs...)
	activeTab := w.state.ActiveTab
	activeNB := w.state.Folder
	notebooks := append([]string(nil), w.state.Notebooks...)
	entries := append([]model.FileEntry(nil), w.entries...)
	notice := w.notice
	hasFBDraft := w.state.FeedbackDraft != nil
	w.mu.Unlock()

	pal := theme.Ocean()
	inSettings := w.modals.IsSettingsOpen()
	aiImmersive := layout.ChatFullscreen && !inSettings
	sidebarWide := !in.InSettings && (layout.SidebarDocked || layout.SidebarDrawer)
	sidebarAutoHidden := layout.SidebarYielded && !layout.SidebarDrawer

	activeSess := w.SessionFor(activeTab)
	pendingAnnots := 0
	if activeSess != nil {
		for _, a := range activeSess.Annotations() {
			if a.Note != "" && a.RewriteResult == "" && !a.RewriteBusy {
				pendingAnnots++
			}
		}
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
				SidebarWidth:      w.sidebarWidth,
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
					return false
				},
				CanBack:            w.nav.CanBack(),
				CanForward:         w.nav.CanForward(),
				ZoomPct:            w.ZoomPct(),
				PendingAnnotations: pendingAnnots,
				OnSendAnnotations: func() {
					w.ai.SetOpen(true)
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
					if activeTab != "" {
						cur, _ := w.ReadNote(activeTab)
						w.modals.OpenHistory(activeTab, cur)
					}
				},
				OnOpenExport: func() {
					if activeTab != "" {
						w.modals.OpenExport(activeTab)
					}
				},
				OnCopyMarkdown: func() {
					if activeSess != nil && mygo.App.IsReady() {
						mygo.Clipboard.WriteText(activeSess.Markdown())
					}
				},
				OnRevealActive: func() {
					if activeTab != "" && mygo.App.IsReady() {
						mygo.Shell.ShowItemInFolder(activeTab)
					}
				},
				OnDeleteActive: func() {
					if activeTab != "" {
						_ = w.TrashWorkspaceItem(activeTab)
					}
				},
				OnSetZoom: w.SetZoomPct,
			})
		}

		// 2. Main Split Body.
		body := ui.Row(c).Grow(1).MinHeight(0).FillWidth()
		body.Children(func() {
			if inSettings {
				w.modals.RenderSettings(c)
				return
			}

			// Docked Sidebar
			if !aiImmersive && layout.SidebarDocked {
				w.renderSidebarColumn(c, activeNB, notebooks, entries, activeTab, notice)
			}

			// Center Content (Note Editor or Empty Mascot State)
			if !aiImmersive {
				mainCol := ui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(pal.Paper)
				mainCol.Children(func() {
					if activeSess != nil {
						activeSess.SetOutlineForceNarrow(!layout.ShowOutline)
						activeSess.Render(c, layout.MainWidth, false)
					} else {
						w.renderEmptyWorkspaceMascot(c, activeNB, len(entries) == 0)
					}
				})
			}

			// Docked or Fullscreen AI Panel
			if layout.ChatDocked {
				if !aiImmersive {
					chatW := w.ai.PanelWidth()
					if blockui.VerticalResizeHandle(c, "ai-panel-resize", &chatW, ChatMinW, ChatMaxW, true) {
						w.ai.SetPanelWidth(chatW)
					}
				}
				w.ai.SetPanelWidth(layout.ChatWidth)
				w.ai.RenderPanel(c)
			}

			// Overlay AI Panel when window is narrow
			if layout.ChatOverlay && !aiImmersive {
				ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Width(layout.ChatWidth).
					Shadow(-8, 0, 28, 0, ui.RGBA(0, 0, 0, 0.18)).Children(func() {
					w.ai.SetPanelWidth(layout.ChatWidth)
					w.ai.RenderPanel(c)
				})
			}

			// Floating Sidebar Drawer + Scrim when narrow and explicitly toggled open
			if layout.SidebarDrawer && !aiImmersive {
				c.Key("sidebar-drawer-scrim")
				scrim := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
					Background(ui.RGBA(0, 0, 0, 0.22))
				blockui.Probe(scrim, "sidebar-drawer-scrim")
				if scrim.Clicked() {
					w.ToggleSidebar(vpW)
				}
				drawer := ui.Row(c).Absolute().Left(0).Top(0).Bottom(0).Width(w.sidebarWidth).
					Shadow(8, 0, 28, 0, ui.RGBA(0, 0, 0, 0.22))
				drawer.Children(func() {
					w.renderSidebarColumn(c, activeNB, notebooks, entries, activeTab, notice)
				})
			}
		})

		// 3. Modal Overlays (Search, About, Feedback, Performance, History, Export, Updater).
		w.modals.RenderModals(c)

		// 4. Visual Smoke HUD (click ripple + step banner when smoke mode is enabled).
		blockui.RenderSmokeHUD(c)
	})
}

func (w *Workspace) renderSidebarColumn(
	c *ui.Context,
	activeNB string,
	notebooks []string,
	entries []model.FileEntry,
	activeNote string,
	notice string,
) {
	RenderSidebar(c, SidebarProps{
		State: w.sidebarUI,
		Width: &w.sidebarWidth,
		OnWidthChanged: func(nw float32) {
			clamped := ClampSidebarWidth(nw)
			_, _ = files.UpdateState(func(st *model.AppState) {
				st.SidebarWidth = &clamped
			})
		},
		ActiveNotebook: activeNB,
		Notebooks:      notebooks,
		Entries:        entries,
		ActiveNote:     activeNote,
		Notice:         notice,
		SearchOpen:     w.modals.IsSearchOpen(),
		SettingsOpen:   w.modals.IsSettingsOpen(),
		OnOpenSearch: func() {
			w.modals.OpenSearch(false)
		},
		OnNewFolder: func() {
			_, _ = w.CreateNewFolder()
		},
		OnNewNote: func() {
			_, _ = w.CreateNewNote()
		},
		OnOpenNote: func(path string, inNewTab bool) {
			_ = w.openNoteInternal(path, inNewTab, true, 0)
		},
		OnRenameItem: func(oldPath, newName string) {
			_, _ = w.RenameWorkspaceItem(oldPath, newName)
		},
		OnMoveItem: func(srcPath, dstDir string) {
			_, _ = w.MoveWorkspaceItem(srcPath, dstDir)
		},
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
	box := ui.Column(c).Fill().Center().Gap(14).Padding(32).Background(pal.Paper)
	blockui.Probe(box, "workspace-empty")
	box.Children(func() {
		switch {
		case activeNB == "":
			icons.Mascot(c, icons.MascotNoFolder, 220)
			ui.Text(c, theme.Tr("打开一个文件夹开始记录", "Open a folder to start writing")).
				FontSize(18).FontWeight(600).TextColor(pal.Text)
			btn := blockui.ActionButton(c, "empty-choose-folder", theme.Tr("选择文件夹…", "Choose Folder…"), nil, blockui.ButtonPrimary, false)
			if btn.Clicked() {
				w.ChooseNotebookDialog()
			}
		case emptyFolder:
			icons.Mascot(c, icons.MascotEmptyFolder, 220)
			ui.Text(c, theme.Tr("这个笔记本还没有笔记", "This notebook has no notes yet")).
				FontSize(18).FontWeight(600).TextColor(pal.Text)
			ic := icons.NotePlus
			btn := blockui.ActionButton(c, "empty-new-note", theme.Tr("新建笔记", "New Note"), &ic, blockui.ButtonPrimary, false)
			if btn.Clicked() {
				_, _ = w.CreateNewNote()
			}
		default:
			icons.Mascot(c, icons.MascotNoNote, 220)
			ui.Text(c, theme.Tr("从左侧选择一篇笔记，或新建一篇", "Select a note on the left or create a new one")).
				FontSize(16).FontWeight(500).TextColor(pal.TextSecondary)
			ic := icons.NotePlus
			btn := blockui.ActionButton(c, "empty-new-note", theme.Tr("新建笔记", "New Note"), &ic, blockui.ButtonPrimary, false)
			if btn.Clicked() {
				_, _ = w.CreateNewNote()
			}
		}
	})
}
