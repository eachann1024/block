package plugins

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// RetryKind identifies what operation a failed PluginViewState should retry.
type RetryKind string

const (
	RetryKindNone      RetryKind = ""
	RetryKindReload    RetryKind = "reload"
	RetryKindAction    RetryKind = "action"
	RetryKindNoteSaved RetryKind = "note_saved"
	RetryKindUninstall RetryKind = "uninstall"
)

type viewKey struct {
	pluginID     string
	notebookRoot string
}

// PluginViewState holds the non-blocking UI snapshot for an installed plugin's Settings page.
type PluginViewState struct {
	Loading    bool
	BusyAction string
	Error      string
	Notice     string
	Page       *UIPage
	Values     map[string]any

	RetryKind      RetryKind
	FailedActionID string
	FailedNotePath string
	FailedNoteRoot string
	failedSaves    map[string]string
	queuedSaves    map[string]bool
	PendingSaves   int

	lastNotebookRoot   string
	lastActiveNote     string
	editSeq            uint64
	errorFromBg        bool
	initialLoadStarted bool
}

type SupervisorConfig struct {
	AppDataDir      string
	HostVersion     string
	GetNotebookRoot func() string
	GetActiveNote   func() string
	GetLocale       func() string
	PickDirectory   func(title string, onPicked func(path string))
	OnChanged       func()
}

// Supervisor coordinates StoreManager, lazy plugin Runtime subprocesses, async form actions,
// and non-blocking note-saved event dispatch without ever stalling the 60fps UI render thread.
type Supervisor struct {
	rpcLocks sync.Map // plugin ID -> *sync.Mutex, serializes notebook-scoped RPCs
	store    *StoreManager
	cfg      SupervisorConfig

	mu              sync.RWMutex
	catalog         *StoreIndex
	installed       []InstalledApp
	runtimes        map[string]*Runtime
	views           map[viewKey]*PluginViewState
	storeRefreshing bool
	installingID    string
	storeErr        string
	storeNotice     string
}

func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	sm := NewStoreManager(cfg.AppDataDir)
	_ = sm.EnsureDirs()
	installed, _ := sm.ListInstalled()
	return &Supervisor{
		store:     sm,
		cfg:       cfg,
		catalog:   sm.LoadCachedOrSeedCatalog(),
		installed: installed,
		runtimes:  make(map[string]*Runtime),
		views:     make(map[viewKey]*PluginViewState),
	}
}

func (s *Supervisor) Store() *StoreManager {
	return s.store
}

func (s *Supervisor) Locale() string {
	if s.cfg.GetLocale != nil {
		if l := strings.TrimSpace(s.cfg.GetLocale()); l != "" {
			return l
		}
	}
	return "zh"
}

func (s *Supervisor) NotebookRoot() string {
	if s.cfg.GetNotebookRoot != nil {
		return strings.TrimSpace(s.cfg.GetNotebookRoot())
	}
	return ""
}

func (s *Supervisor) ActiveNoteRel() string {
	if s.cfg.GetActiveNote == nil {
		return ""
	}
	activeAbs := strings.TrimSpace(s.cfg.GetActiveNote())
	nbRoot := s.NotebookRoot()
	if activeAbs == "" || nbRoot == "" {
		return ""
	}
	rootClean, err1 := filepath.Abs(nbRoot)
	noteClean, err2 := filepath.Abs(activeAbs)
	if err1 != nil || err2 != nil {
		return ""
	}
	rel, err := filepath.Rel(rootClean, noteClean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (s *Supervisor) CanPickDirectory() bool {
	return s.cfg.PickDirectory != nil
}

func (s *Supervisor) PickDirectory(title string, onPicked func(path string)) {
	if s.cfg.PickDirectory == nil || onPicked == nil {
		return
	}
	s.cfg.PickDirectory(title, func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		onPicked(p)
		s.notifyChanged()
	})
}

func (s *Supervisor) notifyChanged() {
	if s.cfg.OnChanged != nil {
		s.cfg.OnChanged()
	}
}

func (s *Supervisor) Catalog() *StoreIndex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalog
}

func (s *Supervisor) Installed() []InstalledApp {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]InstalledApp, len(s.installed))
	copy(out, s.installed)
	return out
}

func (s *Supervisor) GetInstalled(pluginID string) (InstalledApp, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.installed {
		if a.Manifest.ID == pluginID {
			return a, true
		}
	}
	return InstalledApp{}, false
}

func (s *Supervisor) IsInstalled(pluginID string) bool {
	_, ok := s.GetInstalled(pluginID)
	return ok
}

func (s *Supervisor) StoreStatus() (refreshing bool, installingID string, errMsg string, notice string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storeRefreshing, s.installingID, s.storeErr, s.storeNotice
}

func (s *Supervisor) ClearStoreMessage() {
	s.mu.Lock()
	if s.storeErr == "" && s.storeNotice == "" {
		s.mu.Unlock()
		return
	}
	s.storeErr = ""
	s.storeNotice = ""
	s.mu.Unlock()
	s.notifyChanged()
}

func (s *Supervisor) RefreshCatalogAsync() {
	s.mu.Lock()
	if s.storeRefreshing {
		s.mu.Unlock()
		return
	}
	s.storeRefreshing = true
	s.storeErr = ""
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		idx, err := s.store.RefreshCatalog(ctx)
		s.mu.Lock()
		s.storeRefreshing = false
		if idx != nil {
			s.catalog = idx
		}
		if err != nil && (idx == nil || len(idx.Apps) == 0) {
			s.storeErr = err.Error()
		}
		s.mu.Unlock()
		s.notifyChanged()
	}()
}

func (s *Supervisor) InstallEntryAsync(entry StoreAppEntry, onInstalled func(pluginID string)) {
	s.mu.Lock()
	if s.installingID != "" {
		s.mu.Unlock()
		return
	}
	s.installingID = entry.ID
	s.storeErr = ""
	s.storeNotice = ""
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		defer s.lockPluginRPC(entry.ID)()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		inst, err := s.store.InstallEntry(ctx, entry)
		installedList, _ := s.store.ListInstalled()

		s.mu.Lock()
		s.installingID = ""
		if err != nil {
			s.storeErr = err.Error()
			s.mu.Unlock()
			s.notifyChanged()
			return
		}
		s.installed = installedList
		prevRt := s.runtimes[inst.Manifest.ID]
		delete(s.runtimes, inst.Manifest.ID)
		s.deleteViewsForPluginLocked(inst.Manifest.ID)
		s.mu.Unlock()
		if prevRt != nil {
			go prevRt.Shutdown(context.Background())
		}

		if onInstalled != nil {
			onInstalled(inst.Manifest.ID)
		}
		s.notifyChanged()
	}()
}

func (s *Supervisor) InstallFromSourceAsync(spec string, onInstalled func(pluginID string)) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	s.mu.Lock()
	if s.installingID != "" {
		s.mu.Unlock()
		return
	}
	s.installingID = "__manual__"
	s.storeErr = ""
	s.storeNotice = ""
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		inst, err := s.store.InstallFromSource(ctx, spec)
		installedList, _ := s.store.ListInstalled()

		s.mu.Lock()
		s.installingID = ""
		if err != nil {
			s.storeErr = err.Error()
			s.mu.Unlock()
			s.notifyChanged()
			return
		}
		s.installed = installedList
		prevRt := s.runtimes[inst.Manifest.ID]
		delete(s.runtimes, inst.Manifest.ID)
		s.deleteViewsForPluginLocked(inst.Manifest.ID)
		s.mu.Unlock()
		if prevRt != nil {
			go prevRt.Shutdown(context.Background())
		}

		if onInstalled != nil {
			onInstalled(inst.Manifest.ID)
		}
		s.notifyChanged()
	}()
}

func (s *Supervisor) UninstallAsync(pluginID string, onDone func()) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}

	s.mu.Lock()
	if s.installingID != "" {
		s.mu.Unlock()
		return
	}
	s.installingID = "__uninstall__"
	if vs := s.views[key]; vs != nil {
		vs.BusyAction = "__uninstall__"
		vs.Error = ""
		vs.RetryKind = RetryKindNone
		vs.FailedActionID = ""
		vs.FailedNotePath = ""
		vs.FailedNoteRoot = ""
		vs.errorFromBg = false
	}
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		defer s.lockPluginRPC(pluginID)()
		s.mu.Lock()
		rt := s.runtimes[pluginID]
		delete(s.runtimes, pluginID)
		s.mu.Unlock()
		if rt != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = rt.Shutdown(ctx)
			cancel()
		}
		err := s.store.Uninstall(pluginID, false)
		installedList, _ := s.store.ListInstalled()

		s.mu.Lock()
		s.installingID = ""
		if err == nil {
			s.installed = installedList
		}
		if err != nil {
			s.storeErr = err.Error()
			vs := s.views[key]
			if vs == nil {
				vs = &PluginViewState{
					Values:           map[string]any{},
					lastNotebookRoot: nbRoot,
				}
				s.views[key] = vs
			}
			vs.BusyAction = ""
			vs.Error = err.Error()
			vs.RetryKind = RetryKindUninstall
			vs.FailedActionID = ""
			vs.FailedNotePath = ""
			vs.FailedNoteRoot = ""
			vs.errorFromBg = true
			s.mu.Unlock()
			s.notifyChanged()
			return
		}
		s.storeErr = ""
		s.deleteViewsForPluginLocked(pluginID)
		s.mu.Unlock()

		if onDone != nil {
			onDone()
		}
		s.notifyChanged()
	}()
}

func (s *Supervisor) deleteViewsForPluginLocked(pluginID string) {
	for k := range s.views {
		if k.pluginID == pluginID {
			delete(s.views, k)
		}
	}
}

// EnsurePluginView returns the current non-blocking UI snapshot and lazily starts/renders the plugin if needed.
func (s *Supervisor) EnsurePluginView(pluginID string) PluginViewState {
	nbRoot := s.NotebookRoot()
	activeNote := s.activeNoteFor(nbRoot)
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}

	s.mu.Lock()
	vs, ok := s.views[key]
	if ok {
		if vs.Page == nil && !vs.Loading && !vs.initialLoadStarted {
			vs.Loading = true
			vs.initialLoadStarted = true
			vs.lastNotebookRoot = nbRoot
			vs.lastActiveNote = activeNote
			snap := cloneViewState(vs)
			s.mu.Unlock()
			go s.loadAndRenderPlugin(pluginID, nbRoot)
			return snap
		}
		needWorkspaceSync := !vs.Loading && vs.BusyAction == "" && vs.Page != nil &&
			vs.lastActiveNote != activeNote
		var syncSeq uint64
		if needWorkspaceSync {
			vs.lastNotebookRoot = nbRoot
			vs.lastActiveNote = activeNote
			vs.editSeq++
			syncSeq = vs.editSeq
		}
		snap := cloneViewState(vs)
		s.mu.Unlock()
		if needWorkspaceSync {
			go s.refreshPreviewForSeq(pluginID, nbRoot, syncSeq, 0)
		}
		return snap
	}
	vs = &PluginViewState{
		Loading:            true,
		Values:             map[string]any{},
		lastNotebookRoot:   nbRoot,
		lastActiveNote:     activeNote,
		initialLoadStarted: true,
	}
	s.views[key] = vs
	snap := cloneViewState(vs)
	s.mu.Unlock()

	go s.loadAndRenderPlugin(pluginID, nbRoot)
	return snap
}

func (s *Supervisor) ReloadPluginViewAsync(pluginID string) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}

	s.mu.Lock()
	vs, ok := s.views[key]
	if !ok {
		vs = &PluginViewState{Values: map[string]any{}, lastNotebookRoot: nbRoot}
		s.views[key] = vs
	}
	if vs.Loading {
		s.mu.Unlock()
		return
	}
	vs.Loading = true
	vs.initialLoadStarted = true
	vs.Error = ""
	vs.RetryKind = RetryKindNone
	vs.FailedActionID = ""
	vs.FailedNotePath = ""
	vs.FailedNoteRoot = ""
	vs.errorFromBg = false
	s.mu.Unlock()
	s.notifyChanged()

	go s.loadAndRenderPlugin(pluginID, nbRoot)
}

func (s *Supervisor) loadAndRenderPlugin(pluginID, nbRoot string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}
	defer s.lockPluginRPC(pluginID)()
	rt, err := s.ensureRuntime(ctx, pluginID, nbRoot)
	if err != nil {
		s.mu.Lock()
		if vs := s.views[key]; vs != nil {
			vs.Loading = false
			if !vs.errorFromBg {
				vs.Error = err.Error()
				vs.RetryKind = RetryKindReload
			}
		}
		s.mu.Unlock()
		s.notifyChanged()
		return
	}

	activeNote := s.activeNoteFor(nbRoot)
	rt.SetNotebookRoot(nbRoot)
	rt.SetActiveNote(activeNote)

	s.mu.RLock()
	var currentVals map[string]any
	if vs := s.views[key]; vs != nil && len(vs.Values) > 0 {
		currentVals = make(map[string]any, len(vs.Values))
		for k, v := range vs.Values {
			currentVals[k] = v
		}
	}
	s.mu.RUnlock()

	page, err := rt.RenderWithValues(ctx, SurfaceSettingsPage, currentVals)
	s.mu.Lock()
	if vs := s.views[key]; vs != nil {
		vs.Loading = false
		vs.lastNotebookRoot = nbRoot
		vs.lastActiveNote = activeNote
		if err != nil {
			if !vs.errorFromBg {
				vs.Error = err.Error()
				vs.RetryKind = RetryKindReload
			}
		} else {
			if !vs.errorFromBg {
				vs.Error = ""
				vs.RetryKind = RetryKindNone
			}
			vs.Page = page
			defaults := page.ExtractDefaultValues()
			if len(vs.Values) == 0 {
				vs.Values = defaults
			} else {
				for k, v := range defaults {
					if _, exists := vs.Values[k]; !exists {
						vs.Values[k] = v
					}
				}
			}
		}
	}
	s.mu.Unlock()
	s.notifyChanged()
}

func (s *Supervisor) lockPluginRPC(pluginID string) func() {
	value, _ := s.rpcLocks.LoadOrStore(pluginID, new(sync.Mutex))
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func (s *Supervisor) activeNoteFor(root string) string {
	if root == s.NotebookRoot() {
		return s.ActiveNoteRel()
	}
	return ""
}

func (s *Supervisor) ensureRuntime(ctx context.Context, pluginID, nbRoot string) (*Runtime, error) {
	s.mu.RLock()
	existing := s.runtimes[pluginID]
	s.mu.RUnlock()
	if existing != nil && !existing.closed.Load() {
		if existing.notebookRoot() == nbRoot {
			select {
			case <-existing.exited:
			default:
				return existing, nil
			}
		}
		// Plugins receive the notebook root at initialization.
		_ = existing.Shutdown(ctx)
	}
	inst, ok := s.GetInstalled(pluginID)
	if !ok {
		return nil, osErrNotInstalled(pluginID)
	}
	hostVer := s.cfg.HostVersion
	if hostVer == "" {
		hostVer = "1.0.379"
	}
	rt, err := StartRuntime(context.Background(), inst.Manifest, HostConfig{
		PluginDir:    inst.InstallDir,
		DataDir:      inst.DataDir,
		NotebookRoot: nbRoot,
		ActiveNote:   s.activeNoteFor(nbRoot),
		Locale:       s.Locale(),
		HostVersion:  hostVer,
	})
	if err != nil {
		return nil, err
	}
	if err := rt.Initialize(ctx); err != nil {
		_ = rt.Shutdown(ctx)
		return nil, err
	}

	s.mu.Lock()
	if prev := s.runtimes[pluginID]; prev != nil && prev != rt {
		go prev.Shutdown(context.Background())
	}
	s.runtimes[pluginID] = rt
	s.mu.Unlock()
	return rt, nil
}

func (s *Supervisor) SetControlValue(pluginID, controlID string, val any) {
	s.UpdateControlValue(pluginID, controlID, val, true)
}

func (s *Supervisor) UpdateControlValue(pluginID, controlID string, val any, immediate bool) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}

	s.mu.Lock()
	vs := s.views[key]
	if vs == nil {
		s.mu.Unlock()
		return
	}
	if vs.Values == nil {
		vs.Values = map[string]any{}
	}
	vs.Values[controlID] = val
	vs.editSeq++
	seq := vs.editSeq
	s.mu.Unlock()

	delay := 220 * time.Millisecond
	if immediate {
		delay = 25 * time.Millisecond
	}
	go s.refreshPreviewForSeq(pluginID, nbRoot, seq, delay)
}

func (s *Supervisor) refreshPreviewForSeq(pluginID, nbRoot string, seq uint64, delay time.Duration) {
	if delay > 0 {
		time.Sleep(delay)
	}
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}
	s.mu.RLock()
	vs := s.views[key]
	if vs == nil || vs.BusyAction != "" || vs.editSeq != seq {
		s.mu.RUnlock()
		return
	}
	valsCopy := make(map[string]any, len(vs.Values))
	for k, v := range vs.Values {
		valsCopy[k] = v
	}
	s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	defer s.lockPluginRPC(pluginID)()
	rt, err := s.ensureRuntime(ctx, pluginID, nbRoot)
	if err != nil {
		s.mu.Lock()
		if cur := s.views[key]; cur != nil && cur.BusyAction == "" && cur.editSeq == seq && !cur.errorFromBg {
			cur.Error = err.Error()
			cur.RetryKind = RetryKindReload
		}
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	activeNote := s.activeNoteFor(nbRoot)
	rt.SetNotebookRoot(nbRoot)
	rt.SetActiveNote(activeNote)

	page, err := rt.RenderWithValues(ctx, SurfaceSettingsPage, valsCopy)
	if err != nil {
		s.mu.Lock()
		if cur := s.views[key]; cur != nil && cur.BusyAction == "" && cur.editSeq == seq && !cur.errorFromBg {
			cur.Error = err.Error()
			cur.RetryKind = RetryKindReload
		}
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	if page == nil {
		return
	}

	s.mu.Lock()
	cur := s.views[key]
	if cur == nil || cur.BusyAction != "" || cur.editSeq != seq {
		s.mu.Unlock()
		return
	}
	cur.Page = page
	cur.lastNotebookRoot = nbRoot
	cur.lastActiveNote = activeNote
	if !cur.errorFromBg && cur.RetryKind == RetryKindReload {
		cur.Error = ""
		cur.RetryKind = RetryKindNone
	}
	for k, v := range page.ExtractDefaultValues() {
		if _, exists := cur.Values[k]; !exists {
			cur.Values[k] = v
		}
	}
	s.mu.Unlock()
	s.notifyChanged()
}

func (s *Supervisor) TriggerActionAsync(pluginID, actionID string) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}

	s.mu.Lock()
	vs := s.views[key]
	if vs == nil || vs.BusyAction != "" {
		s.mu.Unlock()
		return
	}
	vs.BusyAction = actionID
	vs.Error = ""
	vs.RetryKind = RetryKindNone
	vs.FailedActionID = ""
	vs.FailedNotePath = ""
	vs.FailedNoteRoot = ""
	vs.errorFromBg = false
	vs.editSeq++
	valsCopy := make(map[string]any, len(vs.Values))
	for k, v := range vs.Values {
		valsCopy[k] = v
	}
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		defer s.lockPluginRPC(pluginID)()
		rt, err := s.ensureRuntime(ctx, pluginID, nbRoot)
		var res *ActionResult
		activeNote := s.activeNoteFor(nbRoot)
		if err == nil {
			rt.SetNotebookRoot(nbRoot)
			rt.SetActiveNote(activeNote)
			res, err = rt.Action(ctx, SurfaceSettingsPage, actionID, valsCopy)
		}

		var openTarget, revealTarget string
		s.mu.Lock()
		if cur := s.views[key]; cur != nil {
			cur.BusyAction = ""
			cur.lastNotebookRoot = nbRoot
			cur.lastActiveNote = activeNote
			if err != nil {
				cur.Error = err.Error()
				cur.RetryKind = RetryKindAction
				cur.FailedActionID = actionID
				cur.errorFromBg = true
			} else if res != nil {
				cur.Error = ""
				cur.RetryKind = RetryKindNone
				cur.FailedActionID = ""
				cur.errorFromBg = false
				if res.Status != "" {
					cur.Notice = res.Status
				}
				if res.Page != nil {
					cur.Page = res.Page
					if cur.Page.Status == "" && res.Status != "" {
						cur.Page.Status = res.Status
					}
					for k, v := range res.Page.ExtractDefaultValues() {
						cur.Values[k] = v
					}
				}
				for k, v := range res.Values {
					cur.Values[k] = v
				}
				openTarget = res.OpenPath
				revealTarget = res.RevealPath
			}
		}
		s.mu.Unlock()
		s.notifyChanged()

		if openTarget != "" {
			openLocalPathInOS(openTarget, false)
		}
		if revealTarget != "" {
			openLocalPathInOS(revealTarget, true)
		}
	}()
}

// RetryPluginAsync retries the last failed operation for pluginID in the current notebook,
// distinguishing action/event/uninstall retries from full page reloads.
func (s *Supervisor) RetryPluginAsync(pluginID string) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot}

	s.mu.Lock()
	vs := s.views[key]
	if vs == nil {
		s.mu.Unlock()
		s.ReloadPluginViewAsync(pluginID)
		return
	}
	if vs.Loading || vs.BusyAction != "" {
		s.mu.Unlock()
		return
	}
	snapshot := cloneViewState(vs)
	kind := snapshot.RetryKind
	actionID := vs.FailedActionID
	notePath := snapshot.FailedNotePath
	noteRoot := snapshot.FailedNoteRoot
	if kind == RetryKindNoteSaved {
		if noteRoot == "" {
			noteRoot = nbRoot
		}
		relSlash, ok := relativeNotePath(noteRoot, notePath)
		if !ok {
			s.mu.Unlock()
			s.ReloadPluginViewAsync(pluginID)
			return
		}
		vs.BusyAction = "__retry_note_saved__"
		vs.Error = ""
		vs.RetryKind = RetryKindNone
		vs.FailedActionID = ""
		vs.FailedNotePath = ""
		vs.FailedNoteRoot = ""
		vs.errorFromBg = false
		s.mu.Unlock()
		s.notifyChanged()
		s.queueNoteSaved(pluginID, noteRoot, notePath, relSlash)
		return
	}
	s.mu.Unlock()

	switch kind {
	case RetryKindAction:
		if actionID != "" {
			s.TriggerActionAsync(pluginID, actionID)
			return
		}
		s.ReloadPluginViewAsync(pluginID)
	case RetryKindUninstall:
		s.UninstallAsync(pluginID, nil)
	default:
		s.ReloadPluginViewAsync(pluginID)
	}
}

func openLocalPathInOS(rawPath string, reveal bool) {
	cleaned := filepath.Clean(strings.TrimSpace(rawPath))
	if cleaned == "" || !filepath.IsAbs(cleaned) {
		return
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if reveal && !info.IsDir() {
			cmd = exec.Command("open", "-R", cleaned)
		} else {
			cmd = exec.Command("open", cleaned)
		}
	case "windows":
		if reveal && !info.IsDir() {
			cmd = exec.Command("explorer", "/select,"+cleaned)
		} else {
			cmd = exec.Command("cmd", "/c", "start", "", cleaned)
		}
	default:
		target := cleaned
		if reveal && !info.IsDir() {
			target = filepath.Dir(cleaned)
		}
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}

func (s *Supervisor) ShutdownAll() {
	s.mu.Lock()
	rts := make([]*Runtime, 0, len(s.runtimes))
	for id, rt := range s.runtimes {
		rts = append(rts, rt)
		delete(s.runtimes, id)
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, rt := range rts {
		if rt == nil {
			continue
		}
		wg.Add(1)
		go func(r *Runtime) {
			defer wg.Done()
			_ = r.Shutdown(ctx)
		}(rt)
	}
	wg.Wait()
}

func cloneViewState(in *PluginViewState) PluginViewState {
	out := PluginViewState{
		Loading:        in.Loading,
		BusyAction:     in.BusyAction,
		Error:          in.Error,
		Notice:         in.Notice,
		Page:           in.Page,
		Values:         make(map[string]any, len(in.Values)),
		RetryKind:      in.RetryKind,
		FailedActionID: in.FailedActionID,
		FailedNotePath: in.FailedNotePath,
		FailedNoteRoot: in.FailedNoteRoot,
		PendingSaves:   in.PendingSaves,
	}
	for k, v := range in.Values {
		out.Values[k] = v
	}
	if out.Error == "" && out.BusyAction == "" {
		var first string
		for path := range in.failedSaves {
			if first == "" || path < first {
				first = path
			}
		}
		if first != "" {
			out.Error = in.failedSaves[first]
			out.FailedNotePath = first
			out.FailedNoteRoot = in.lastNotebookRoot
			out.RetryKind = RetryKindNoteSaved
		}
	}
	return out
}

type notInstalledError string

func (e notInstalledError) Error() string { return "plugin not installed: " + string(e) }

func osErrNotInstalled(id string) error { return notInstalledError(id) }
