package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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
	surface      Surface
}

type surfaceKey struct {
	pluginID string
	surface  Surface
}

// PluginViewState holds the non-blocking UI snapshot for an installed plugin's Settings page.
type PluginViewState struct {
	Loading                bool
	Refreshing             bool
	BusyAction             string
	Error                  string
	Notice                 string
	Page                   *UIPage
	Values                 map[string]any
	LastActionID           string
	LastActionTone         string
	LastActionMessage      string
	LastActionButtonActive bool

	RetryKind      RetryKind
	FailedActionID string
	FailedNotePath string
	FailedNoteRoot string
	failedSaves    map[string]string
	queuedSaves    map[string]*noteSaveRequest
	saveCancel     context.CancelFunc
	PendingSaves   int

	lastNotebookRoot   string
	lastActiveNote     string
	lastNotebooksKey   string
	lastLocale         string
	editSeq            uint64
	formVersion        uint64
	errorFromBg        bool
	initialLoadStarted bool
	actionCancel       context.CancelFunc
	editedControls     map[string]bool
	editedSeq          map[string]uint64

	cachedSnapValid bool
	cachedSnapAt    time.Time
	cachedEpoch     uint64
	cachedRawActive string
	cachedRawNBs    []string
	cachedFailedLen int
	cachedSnap      *PluginViewState
}

type SupervisorConfig struct {
	AppDataDir      string
	HostVersion     string
	GetNotebookRoot func() string
	GetNotebooks    func() []string
	GetActiveNote   func() string
	GetLocale       func() string
	GetPerfSnapshot func(refreshDisk bool) (*PerfSnapshot, error)
	PickDirectory   func(title, defaultPath string, onPicked func(path string))
	PickFile        func(title, defaultPath string, extensions []string, onPicked func(path string))
	ApplyNoteWrite  func(absPath, content string, write func() (bool, error)) (bool, error)
	OnChanged       func()
	OnRedraw        func()
	// UI lets plugins drive host UI (toasts, dialogs, panels, editor).
	UI UIHost
}

// Supervisor coordinates StoreManager, lazy plugin Runtime subprocesses, async form actions,
// and non-blocking note-saved event dispatch without ever stalling the 60fps UI render thread.
type Supervisor struct {
	rpcLocks sync.Map // plugin ID -> *sync.Mutex, serializes notebook-scoped RPCs
	store    *StoreManager
	cfg      SupervisorConfig
	contrib  *contribRegistry

	mu              sync.RWMutex
	viewEpoch       atomic.Uint64
	catalog         *StoreIndex
	installed       []InstalledApp
	runtimes        map[string]*Runtime
	views           map[viewKey]*PluginViewState
	lastViewRoot    map[surfaceKey]string
	formVersions    map[surfaceKey]uint64
	liveTimers      map[surfaceKey]*time.Timer
	liveViewedAt    map[surfaceKey]time.Time
	feedbackTimers  map[viewKey]*time.Timer
	storeRefreshing bool
	installingID    string
	replacingID     string
	stopping        bool
	storeErr        string
	storeNotice     string
}

func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	sm := NewStoreManager(cfg.AppDataDir)
	_ = sm.EnsureDirs()
	installed, _ := sm.ListInstalled()
	s := &Supervisor{
		store:          sm,
		cfg:            cfg,
		contrib:        newContribRegistry(cfg.AppDataDir),
		catalog:        sm.LoadCachedOrSeedCatalog(),
		installed:      installed,
		runtimes:       make(map[string]*Runtime),
		views:          make(map[viewKey]*PluginViewState),
		lastViewRoot:   make(map[surfaceKey]string),
		formVersions:   make(map[surfaceKey]uint64),
		liveTimers:     make(map[surfaceKey]*time.Timer),
		liveViewedAt:   make(map[surfaceKey]time.Time),
		feedbackTimers: make(map[viewKey]*time.Timer),
	}
	sm.beforeReplace = s.preparePluginReplacement
	SyncActiveLanguages(installed)
	return s
}

func (s *Supervisor) preparePluginReplacement(pluginID string) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return errors.New("plugin supervisor is stopping")
	}
	s.replacingID = pluginID
	rt := s.runtimes[pluginID]
	delete(s.runtimes, pluginID)
	s.mu.Unlock()
	if rt != nil {
		return rt.Shutdown(context.Background())
	}
	return nil
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

func (s *Supervisor) Notebooks() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 4)
	add := func(raw string) {
		p := strings.TrimSpace(raw)
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		} else {
			p = filepath.Clean(p)
		}
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	if s.cfg.GetNotebooks != nil {
		for _, nb := range s.cfg.GetNotebooks() {
			add(nb)
		}
	}
	add(s.NotebookRoot())
	return out
}

func (s *Supervisor) notebooksFingerprint() string {
	return strings.Join(s.Notebooks(), "\x00")
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

func (s *Supervisor) CanPickFile() bool {
	return s.cfg.PickFile != nil
}

func (s *Supervisor) PickDirectory(title string, onPicked func(path string)) {
	s.pickDirectory(title, "", onPicked)
}

func (s *Supervisor) PickFileForControl(pluginID, controlID, title string, extensions []string) {
	s.PickSurfaceFileForControl(pluginID, SurfaceSettingsPage, controlID, title, extensions)
}

func (s *Supervisor) PickSurfaceFileForControl(pluginID string, surface Surface, controlID, title string, extensions []string) {
	if !s.CanPickFile() {
		return
	}
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	s.mu.RLock()
	view := s.views[key]
	if view == nil {
		s.mu.RUnlock()
		return
	}
	current, _ := view.Values[controlID].(string)
	s.mu.RUnlock()
	if !filepath.IsAbs(current) {
		current = nbRoot
	}
	s.cfg.PickFile(title, current, extensions, func(p string) {
		if p == "" || s.NotebookRoot() != nbRoot {
			return
		}
		s.updateSurfaceControlValue(pluginID, nbRoot, surface, controlID, p, true)
		s.notifyChanged()
	})
}

func (s *Supervisor) PickDirectoryForControl(pluginID, controlID, title string) {
	s.PickSurfaceDirectoryForControl(pluginID, SurfaceSettingsPage, controlID, title)
}

func (s *Supervisor) PickSurfaceDirectoryForControl(pluginID string, surface Surface, controlID, title string) {
	if !s.CanPickDirectory() {
		return
	}
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	s.mu.RLock()
	view := s.views[key]
	if view == nil {
		s.mu.RUnlock()
		return
	}
	current, _ := view.Values[controlID].(string)
	s.mu.RUnlock()
	if !filepath.IsAbs(current) {
		current = nbRoot
	}
	s.pickDirectory(title, current, func(p string) {
		if s.NotebookRoot() != nbRoot {
			return
		}
		s.updateSurfaceControlValue(pluginID, nbRoot, surface, controlID, p, true)
	})
}

func (s *Supervisor) pickDirectory(title, defaultPath string, onPicked func(path string)) {
	if !s.CanPickDirectory() || onPicked == nil {
		return
	}
	s.cfg.PickDirectory(title, defaultPath, func(p string) {
		if p == "" {
			return
		}
		onPicked(p)
		s.notifyChanged()
	})
}

func (s *Supervisor) notifyChanged() {
	s.viewEpoch.Add(1)
	if s.cfg.OnChanged != nil {
		s.cfg.OnChanged()
	}
}

func (s *Supervisor) notifyRedraw() {
	s.viewEpoch.Add(1)
	if s.cfg.OnRedraw != nil {
		s.cfg.OnRedraw()
		return
	}
	s.notifyChanged()
}

// NotifyRedraw requests a lightweight UI redraw without rescanning the notebook.
func (s *Supervisor) NotifyRedraw() {
	s.notifyRedraw()
}

// ScheduleLiveRefresh marks pluginID's settings page as actively displayed and schedules a one-shot
// background render tick after intervalMs. When the user navigates away, the timer
// expires without rescheduling.
func (s *Supervisor) ScheduleLiveRefresh(pluginID string, intervalMs int) {
	s.ScheduleSurfaceLiveRefresh(pluginID, SurfaceSettingsPage, intervalMs)
}

// ScheduleSurfaceLiveRefresh marks one plugin surface as actively displayed and schedules
// a one-shot background render tick after intervalMs.
func (s *Supervisor) ScheduleSurfaceLiveRefresh(pluginID string, surface Surface, intervalMs int) {
	if intervalMs < 500 || intervalMs > 60000 {
		return
	}
	interval := time.Duration(intervalMs) * time.Millisecond
	now := time.Now()
	sKey := surfaceKey{pluginID: pluginID, surface: surface}

	s.mu.Lock()
	s.liveViewedAt[sKey] = now
	if _, scheduled := s.liveTimers[sKey]; scheduled {
		s.mu.Unlock()
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(interval, func() {
		s.runLiveRefreshTick(pluginID, surface, interval, &timer)
	})
	s.liveTimers[sKey] = timer
	s.mu.Unlock()
}

func (s *Supervisor) stopLiveTimerLocked(sKey surfaceKey) {
	if t, ok := s.liveTimers[sKey]; ok {
		t.Stop()
		delete(s.liveTimers, sKey)
	}
	delete(s.liveViewedAt, sKey)
}

func (s *Supervisor) stopLiveTimersForPluginLocked(pluginID string) {
	for k := range s.liveTimers {
		if k.pluginID == pluginID {
			s.stopLiveTimerLocked(k)
		}
	}
	for k := range s.liveViewedAt {
		if k.pluginID == pluginID {
			delete(s.liveViewedAt, k)
		}
	}
}

// runLiveRefreshTick keeps its timer registered until it finishes, so a slow
// plugin gets at most one pending refresh per surface instead of a pile-up.
func (s *Supervisor) runLiveRefreshTick(pluginID string, surface Surface, interval time.Duration, self **time.Timer) {
	sKey := surfaceKey{pluginID: pluginID, surface: surface}
	defer func() {
		s.mu.Lock()
		if s.liveTimers[sKey] == *self {
			delete(s.liveTimers, sKey)
		}
		s.mu.Unlock()
	}()
	s.mu.RLock()
	lastViewed := s.liveViewedAt[sKey]
	s.mu.RUnlock()

	maxIdle := max(interval*2, 2500*time.Millisecond)
	if lastViewed.IsZero() || time.Since(lastViewed) > maxIdle {
		return
	}
	if !s.IsPluginEnabled(pluginID) {
		return
	}

	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := s.lockPluginRPCContext(ctx, pluginID)
	if err != nil {
		return
	}
	defer unlock()

	s.mu.RLock()
	vs := s.views[key]
	if vs == nil || vs.Loading || vs.BusyAction != "" || vs.Page == nil {
		s.mu.RUnlock()
		return
	}
	seq := vs.editSeq
	draftVals := pendingDraftValuesLocked(vs)
	s.mu.RUnlock()

	rt, err := s.ensureRuntime(ctx, pluginID, nbRoot)
	if err != nil {
		return
	}
	rt.SetNotebookRoot(nbRoot)
	rt.SetActiveNote(s.activeNoteFor(nbRoot))

	page, returnedVals, err := rt.RenderWithValuesResult(ctx, surface, draftVals)
	if err != nil || page == nil {
		return
	}
	if draftVals == nil {
		returnedVals = nil
	}

	s.mu.Lock()
	if cur := s.views[key]; cur == vs && !cur.Loading && cur.BusyAction == "" && cur.editSeq == seq {
		cur.Page = page
		cur.lastLocale = rt.locale()
		if !cur.errorFromBg {
			cur.Error = ""
			cur.RetryKind = RetryKindNone
		}
		mergePageValuesUpToSeq(cur, page, returnedVals, seq, true)
	}
	s.mu.Unlock()
	s.notifyRedraw()
}

func pendingDraftValuesLocked(vs *PluginViewState) map[string]any {
	if vs == nil || len(vs.editedControls) == 0 || len(vs.Values) == 0 {
		return nil
	}
	out := make(map[string]any, len(vs.editedControls))
	for k := range vs.editedControls {
		if v, ok := vs.Values[k]; ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

func (s *Supervisor) IsPluginEnabled(pluginID string) bool {
	inst, ok := s.GetInstalled(pluginID)
	return ok && inst.Enabled
}

// SetPluginEnabled toggles an installed plugin's enabled state, shutting down its
// background runtime immediately when disabled.
func (s *Supervisor) SetPluginEnabled(pluginID string, enabled bool) {
	s.mu.Lock()
	if err := s.store.SetEnabled(pluginID, enabled); err != nil {
		s.mu.Unlock()
		return
	}
	var rt *Runtime
	changed := false
	for i := range s.installed {
		if s.installed[i].Manifest.ID == pluginID {
			if s.installed[i].Enabled != enabled {
				s.installed[i].Enabled = enabled
				changed = true
			}
		}
	}
	if !enabled {
		rt = s.runtimes[pluginID]
		delete(s.runtimes, pluginID)
		s.stopLiveTimersForPluginLocked(pluginID)
		s.deleteViewsForPluginLocked(pluginID)
	}
	s.mu.Unlock()

	if rt != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = rt.Shutdown(ctx)
		}()
	}
	if enabled {
		s.warmupPluginAsync(pluginID)
	}
	if changed || rt != nil {
		SyncActiveLanguages(s.Installed())
		s.notifyChanged()
	}
}

// WarmupAppearancePluginsAsync initializes enabled appearance-section plugins
// that have not yet synced an editor background state, so upgrading or opening
// Block with an already-enabled background plugin applies immediately.
func (s *Supervisor) WarmupAppearancePluginsAsync() {
	if s == nil {
		return
	}
	s.mu.RLock()
	installed := append([]InstalledApp(nil), s.installed...)
	s.mu.RUnlock()
	for _, inst := range installed {
		if !inst.Enabled || !inst.Manifest.HasPermission(PermUISettings) || inst.Manifest.Contributes == nil {
			continue
		}
		hasAppearance := false
		for _, sec := range inst.Manifest.Contributes.SettingsSections {
			if sec.Tab == "appearance" {
				hasAppearance = true
				break
			}
		}
		if !hasAppearance {
			continue
		}
		if s.contrib != nil {
			s.contrib.mu.RLock()
			_, synced := s.contrib.editorBG[inst.Manifest.ID]
			s.contrib.mu.RUnlock()
			if synced {
				continue
			}
		}
		s.warmupPluginAsync(inst.Manifest.ID)
	}
}

// warmupPluginAsync initializes a newly installed or enabled UI plugin in the
// background so its plugin.initialize hook can sync host UI state (such as
// editor background or titlebar visibility) without waiting for the user to
// open the plugin's settings page.
func (s *Supervisor) warmupPluginAsync(pluginID string) {
	if s == nil || strings.TrimSpace(pluginID) == "" {
		return
	}
	inst, ok := s.GetInstalled(pluginID)
	if !ok || !inst.Enabled {
		return
	}
	if !inst.Manifest.HasPermission(PermUISettings) && !inst.Manifest.HasPermission(PermUITitlebar) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		unlock, err := s.lockPluginRPCContext(ctx, pluginID)
		if err != nil {
			return
		}
		defer unlock()
		if !s.IsPluginEnabled(pluginID) {
			return
		}
		_, _ = s.ensureRuntime(ctx, pluginID, s.NotebookRoot())
	}()
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
	if s.stopping || s.storeRefreshing {
		s.mu.Unlock()
		return
	}
	s.startCatalogRefreshLocked()
	s.mu.Unlock()
	s.notifyChanged()
}

func (s *Supervisor) startCatalogRefreshLocked() {
	s.storeRefreshing = true
	s.storeErr = ""

	go func() {
		totalTimeout := time.Duration(max(len(s.store.IndexURLs), 2))*6*time.Second + 2*time.Second
		ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
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
	s.startInstallAsync(entry.ID, func(ctx context.Context) (*InstalledApp, error) {
		return s.store.InstallEntryExplicit(ctx, entry)
	}, onInstalled)
}

func (s *Supervisor) InstallFromSourceAsync(spec string, onInstalled func(pluginID string)) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	s.startInstallAsync("__manual__", func(ctx context.Context) (*InstalledApp, error) {
		return s.store.InstallFromSourceExplicit(ctx, spec)
	}, onInstalled)
}

func (s *Supervisor) startInstallAsync(
	installingKey string,
	install func(context.Context) (*InstalledApp, error),
	onInstalled func(pluginID string),
) {
	s.mu.Lock()
	if s.stopping || s.installingID != "" {
		s.mu.Unlock()
		return
	}
	s.installingID = installingKey
	s.storeErr = ""
	s.storeNotice = ""
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		inst, err := install(ctx)
		s.finishInstallAsync(inst, err, onInstalled)
	}()
}

func (s *Supervisor) finishInstallAsync(inst *InstalledApp, err error, onInstalled func(pluginID string)) {
	installedList, _ := s.store.ListInstalled()

	s.mu.Lock()
	s.installingID = ""
	s.replacingID = ""
	if err != nil {
		s.storeErr = err.Error()
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	for i := range installedList {
		installedList[i].Enabled = s.store.IsEnabled(installedList[i].Manifest.ID)
	}
	s.installed = installedList
	delete(s.runtimes, inst.Manifest.ID)
	s.deleteViewsForPluginLocked(inst.Manifest.ID)
	s.mu.Unlock()
	SyncActiveLanguages(installedList)
	if onInstalled != nil {
		onInstalled(inst.Manifest.ID)
	}
	s.warmupPluginAsync(inst.Manifest.ID)
	s.notifyChanged()
}

func (s *Supervisor) UninstallAsync(pluginID string, onDone func()) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: SurfaceSettingsPage}

	s.mu.Lock()
	if s.installingID != "" {
		s.mu.Unlock()
		return
	}
	s.installingID = "__uninstall__"
	s.replacingID = pluginID
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
		s.replacingID = ""
		if err == nil {
			for i := range installedList {
				installedList[i].Enabled = s.store.IsEnabled(installedList[i].Manifest.ID)
			}
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
		SyncActiveLanguages(installedList)
		s.contrib.forgetPlugin(pluginID)

		if onDone != nil {
			onDone()
		}
		s.notifyChanged()
	}()
}

func (s *Supervisor) stopFeedbackTimerLocked(key viewKey) {
	if t, ok := s.feedbackTimers[key]; ok {
		t.Stop()
		delete(s.feedbackTimers, key)
	}
}

func (s *Supervisor) deleteViewsForPluginLocked(pluginID string) {
	s.stopLiveTimersForPluginLocked(pluginID)
	for k := range s.lastViewRoot {
		if k.pluginID == pluginID {
			delete(s.lastViewRoot, k)
		}
	}
	for k := range s.formVersions {
		if k.pluginID == pluginID {
			delete(s.formVersions, k)
		}
	}
	for k := range s.feedbackTimers {
		if k.pluginID == pluginID {
			s.stopFeedbackTimerLocked(k)
		}
	}
	for k := range s.views {
		if k.pluginID == pluginID {
			if cancel := s.views[k].actionCancel; cancel != nil {
				cancel()
			}
			if cancel := s.views[k].saveCancel; cancel != nil {
				cancel()
			}
			delete(s.views, k)
		}
	}
}

const ensureViewReuseWindow = 32 * time.Millisecond

func (s *Supervisor) rawActiveNote() string {
	if s.cfg.GetActiveNote == nil {
		return ""
	}
	return strings.TrimSpace(s.cfg.GetActiveNote())
}

func (s *Supervisor) rawNotebooks() []string {
	if s.cfg.GetNotebooks == nil {
		return nil
	}
	return s.cfg.GetNotebooks()
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

func viewMatchesCachedSnap(vs *PluginViewState) bool {
	if !vs.cachedSnapValid || vs.cachedSnap == nil {
		return false
	}
	snap := vs.cachedSnap
	return vs.Loading == snap.Loading &&
		vs.Refreshing == snap.Refreshing &&
		vs.BusyAction == snap.BusyAction &&
		vs.Page == snap.Page &&
		vs.Notice == snap.Notice &&
		vs.LastActionID == snap.LastActionID &&
		len(vs.failedSaves) == vs.cachedFailedLen &&
		(vs.Error == snap.Error || snap.RetryKind == RetryKindNoteSaved)
}

// EnsurePluginView returns the current non-blocking UI snapshot and lazily starts/renders the plugin if needed.
func (s *Supervisor) EnsurePluginView(pluginID string) PluginViewState {
	return s.EnsureSurfaceView(pluginID, SurfaceSettingsPage)
}

func (s *Supervisor) ensureSurfaceView(pluginID string, surface Surface) PluginViewState {
	if inst, ok := s.GetInstalled(pluginID); !ok || !inst.Enabled {
		return PluginViewState{Values: map[string]any{}}
	}
	nbRoot := s.NotebookRoot()
	curLocale := normalizeLocale(s.Locale())
	rawActive := s.rawActiveNote()
	rawNBs := s.rawNotebooks()
	sKey := surfaceKey{pluginID: pluginID, surface: surface}
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	now := time.Now()
	epoch := s.viewEpoch.Load()

	s.mu.Lock()
	previousRoot, seen := s.lastViewRoot[sKey]
	rootChanged := seen && previousRoot != nbRoot
	s.lastViewRoot[sKey] = nbRoot
	if vs, ok := s.views[key]; ok &&
		!rootChanged &&
		vs.lastLocale == curLocale &&
		vs.formVersion == s.formVersions[sKey] &&
		epoch == vs.cachedEpoch &&
		now.Sub(vs.cachedSnapAt) < ensureViewReuseWindow &&
		vs.cachedRawActive == rawActive &&
		stringSlicesEqual(vs.cachedRawNBs, rawNBs) &&
		viewMatchesCachedSnap(vs) {
		snap := *vs.cachedSnap
		s.mu.Unlock()
		return snap
	}
	s.mu.Unlock()

	activeNote := s.activeNoteFor(nbRoot)
	nbKey := s.notebooksFingerprint()

	s.mu.Lock()
	vs, ok := s.views[key]
	if ok {
		if vs.Page == nil && !vs.Loading && !vs.initialLoadStarted {
			vs.Loading = true
			vs.initialLoadStarted = true
			vs.lastNotebookRoot = nbRoot
			vs.lastActiveNote = activeNote
			vs.lastNotebooksKey = nbKey
			vs.lastLocale = curLocale
			snap := s.snapshotViewLocked(key, vs)
			s.recordCachedViewLocked(vs, snap, epoch, rawActive, rawNBs)
			s.mu.Unlock()
			go s.loadAndRenderSurface(pluginID, nbRoot, surface)
			return snap
		}
		needWorkspaceSync := !vs.Loading && vs.BusyAction == "" &&
			((vs.Page != nil && (rootChanged || vs.lastLocale != curLocale || vs.formVersion != s.formVersions[sKey] ||
				vs.lastActiveNote != activeNote || vs.lastNotebooksKey != nbKey)) ||
				(vs.Page == nil && vs.lastLocale != curLocale))
		if needWorkspaceSync {
			vs.lastNotebookRoot = nbRoot
			vs.lastActiveNote = activeNote
			vs.lastNotebooksKey = nbKey
			vs.lastLocale = curLocale
			vs.Loading = true
			if vs.Page == nil {
				vs.Error = ""
				vs.RetryKind = RetryKindNone
				vs.errorFromBg = false
			}
		}
		snap := s.snapshotViewLocked(key, vs)
		s.recordCachedViewLocked(vs, snap, epoch, rawActive, rawNBs)
		s.mu.Unlock()
		if needWorkspaceSync {
			go s.loadAndRenderSurface(pluginID, nbRoot, surface)
		}
		return snap
	}
	vs = &PluginViewState{
		Loading:            true,
		Values:             map[string]any{},
		lastNotebookRoot:   nbRoot,
		lastActiveNote:     activeNote,
		lastNotebooksKey:   nbKey,
		lastLocale:         curLocale,
		initialLoadStarted: true,
	}
	s.views[key] = vs
	snap := s.snapshotViewLocked(key, vs)
	s.recordCachedViewLocked(vs, snap, epoch, rawActive, rawNBs)
	s.mu.Unlock()

	go s.loadAndRenderSurface(pluginID, nbRoot, surface)
	return snap
}

func (s *Supervisor) recordCachedViewLocked(vs *PluginViewState, snap PluginViewState, epoch uint64, rawActive string, rawNBs []string) {
	cp := snap
	vs.cachedSnap = &cp
	vs.cachedSnapAt = time.Now()
	vs.cachedEpoch = epoch
	vs.cachedRawActive = rawActive
	vs.cachedRawNBs = append([]string(nil), rawNBs...)
	vs.cachedFailedLen = len(vs.failedSaves)
	vs.cachedSnapValid = true
}

func (s *Supervisor) ReloadPluginViewAsync(pluginID string) {
	s.ReloadSurfaceViewAsync(pluginID, SurfaceSettingsPage)
}

func (s *Supervisor) reloadSurfaceViewAsync(pluginID string, surface Surface) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}

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

	go s.loadAndRenderSurface(pluginID, nbRoot, surface)
}

func (s *Supervisor) loadAndRenderSurface(pluginID, nbRoot string, surface Surface) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sKey := surfaceKey{pluginID: pluginID, surface: surface}
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	// Results only land in the view that asked for them. A disable/enable or
	// uninstall in the meantime replaces the view, and its own load runs next.
	s.mu.RLock()
	origin := s.views[key]
	s.mu.RUnlock()
	defer s.lockPluginRPC(pluginID)()
	rt, attemptedLocale, err := s.ensureRuntimeWithLocale(ctx, pluginID, nbRoot)
	if err != nil {
		s.mu.Lock()
		if vs := s.views[key]; vs != nil && vs == origin {
			vs.Loading = false
			vs.formVersion = s.formVersions[sKey]
			if attemptedLocale != "" {
				vs.lastLocale = attemptedLocale
			}
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
	var renderSeq uint64
	var draftVals map[string]any
	if cur := s.views[key]; cur != nil {
		renderSeq = cur.editSeq
		draftVals = pendingDraftValuesLocked(cur)
	}
	s.mu.RUnlock()

	page, returnedVals, err := rt.RenderWithValuesResult(ctx, surface, draftVals)
	s.mu.Lock()
	if vs := s.views[key]; vs != nil && vs == origin {
		vs.Loading = false
		vs.formVersion = s.formVersions[sKey]
		vs.lastNotebookRoot = nbRoot
		vs.lastActiveNote = activeNote
		vs.lastLocale = rt.locale()
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
			mergePageValuesUpToSeq(vs, page, returnedVals, renderSeq, true)
		}
	}
	s.mu.Unlock()
	s.notifyChanged()
}

func mergePageValues(view *PluginViewState, page *UIPage, returned map[string]any) {
	mergePageValuesUpToSeq(view, page, returned, 0, false)
}

func mergePageValuesUpToSeq(view *PluginViewState, page *UIPage, returned map[string]any, maxSeq uint64, checkSeq bool) {
	if view.Values == nil {
		view.Values = make(map[string]any)
	}
	defaults := page.ExtractDefaultValues()
	// Keep only keys the current page declares or the plugin just returned,
	// while preserving newer user drafts created after maxSeq.
	for k := range view.Values {
		if _, declared := defaults[k]; declared {
			continue
		}
		if _, ok := returned[k]; ok {
			continue
		}
		if checkSeq && view.editedControls[k] && view.editedSeq[k] > maxSeq {
			continue
		}
		delete(view.Values, k)
		delete(view.editedControls, k)
		delete(view.editedSeq, k)
	}
	for k, v := range defaults {
		if !view.editedControls[k] {
			view.Values[k] = v
		}
	}
	for k, v := range returned {
		if checkSeq && view.editedControls[k] && view.editedSeq[k] > maxSeq {
			continue
		}
		view.Values[k] = v
		delete(view.editedControls, k)
		delete(view.editedSeq, k)
	}
}

func (s *Supervisor) lockPluginRPC(pluginID string) func() {
	unlock, _ := s.lockPluginRPCContext(context.Background(), pluginID)
	return unlock
}

func (s *Supervisor) lockPluginRPCContext(ctx context.Context, pluginID string) (func(), error) {
	value, _ := s.rpcLocks.LoadOrStore(pluginID, make(chan struct{}, 1))
	gate := value.(chan struct{})
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Supervisor) activeNoteFor(root string) string {
	if root == s.NotebookRoot() {
		return s.ActiveNoteRel()
	}
	return ""
}

func (s *Supervisor) ensureRuntime(ctx context.Context, pluginID, nbRoot string) (*Runtime, error) {
	rt, _, err := s.ensureRuntimeWithLocale(ctx, pluginID, nbRoot)
	return rt, err
}

func (s *Supervisor) ensureRuntimeWithLocale(ctx context.Context, pluginID, nbRoot string) (*Runtime, string, error) {
	s.mu.RLock()
	unavailable := s.stopping || s.replacingID == pluginID
	s.mu.RUnlock()
	if unavailable {
		return nil, "", errors.New("plugin is stopping or being replaced")
	}
	inst, ok := s.GetInstalled(pluginID)
	if !ok {
		return nil, "", osErrNotInstalled(pluginID)
	}
	if inst.Manifest.IsDeclarativeOnly() {
		return nil, "", errors.New("declarative language plugin has no runtime process")
	}
	if !inst.Enabled {
		s.mu.Lock()
		prev := s.runtimes[pluginID]
		delete(s.runtimes, pluginID)
		s.mu.Unlock()
		if prev != nil {
			_ = prev.Shutdown(ctx)
		}
		return nil, "", errPluginDisabled(pluginID)
	}
	wantLocale := normalizeLocale(s.Locale())
	s.mu.RLock()
	existing := s.runtimes[pluginID]
	s.mu.RUnlock()
	if existing != nil && !existing.closed.Load() {
		if existing.notebookRoot() == nbRoot && existing.locale() == wantLocale {
			select {
			case <-existing.exited:
			default:
				return existing, existing.locale(), nil
			}
		}
		// Plugins receive the notebook root and locale at initialization.
		_ = existing.Shutdown(ctx)
	}
	hostVer := s.cfg.HostVersion
	if hostVer == "" {
		hostVer = "1.0.379"
	}
	rawLocale := s.Locale()
	attemptedLocale := normalizeLocale(rawLocale)
	cfg := HostConfig{
		PluginDir:       inst.InstallDir,
		DataDir:         inst.DataDir,
		NotebookRoot:    nbRoot,
		ActiveNote:      s.activeNoteFor(nbRoot),
		Locale:          rawLocale,
		HostVersion:     hostVer,
		GetNotebooks:    s.Notebooks,
		GetPerfSnapshot: s.cfg.GetPerfSnapshot,
		ApplyNoteWrite:  s.cfg.ApplyNoteWrite,
		OnNoteWritten:   s.notifyNoteWritten,
		ContribHost:     s.dispatchContribHostMethod,
	}
	s.mu.Lock()
	current := false
	for _, app := range s.installed {
		if app.Manifest.ID == pluginID {
			current = app.Enabled && app.InstallDir == inst.InstallDir && app.InstalledAt.Equal(inst.InstalledAt)
			break
		}
	}
	if s.stopping || s.replacingID == pluginID || !current || ctx.Err() != nil {
		s.mu.Unlock()
		return nil, attemptedLocale, errors.New("plugin changed while starting")
	}
	rt, err := StartRuntime(context.Background(), inst.Manifest, cfg)
	if err != nil {
		s.mu.Unlock()
		return nil, attemptedLocale, err
	}
	s.runtimes[pluginID] = rt
	s.mu.Unlock()
	if err := rt.Initialize(ctx); err != nil {
		rt.abort()
		return nil, attemptedLocale, err
	}
	return rt, attemptedLocale, nil
}

func (s *Supervisor) SetControlValue(pluginID, controlID string, val any) {
	s.UpdateControlValue(pluginID, controlID, val, true)
}

func (s *Supervisor) UpdateControlValue(pluginID, controlID string, val any, immediate bool) {
	s.UpdateSurfaceControlValue(pluginID, SurfaceSettingsPage, controlID, val, immediate)
}

func (s *Supervisor) updateSurfaceControlValue(pluginID, nbRoot string, surface Surface, controlID string, val any, immediate bool) {
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}

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
	if vs.editedControls == nil {
		vs.editedControls = make(map[string]bool)
	}
	if vs.editedSeq == nil {
		vs.editedSeq = make(map[string]uint64)
	}
	vs.editedControls[controlID] = true
	hadFeedback := vs.Notice != "" || vs.LastActionID != "" || vs.LastActionMessage != "" || vs.LastActionButtonActive
	vs.Notice = ""
	vs.LastActionID = ""
	vs.LastActionTone = ""
	vs.LastActionMessage = ""
	vs.LastActionButtonActive = false
	s.stopFeedbackTimerLocked(key)
	vs.editSeq++
	vs.editedSeq[controlID] = vs.editSeq
	vs.cachedSnapValid = false
	s.viewEpoch.Add(1)
	seq := vs.editSeq
	s.mu.Unlock()

	if hadFeedback || immediate {
		s.notifyRedraw()
	}

	delay := 220 * time.Millisecond
	if immediate {
		delay = 25 * time.Millisecond
	}
	go s.refreshPreviewForSeq(pluginID, nbRoot, surface, seq, delay)
}

func (s *Supervisor) refreshPreviewForSeq(pluginID, nbRoot string, surface Surface, seq uint64, delay time.Duration) {
	if delay > 0 {
		time.Sleep(delay)
	}
	sKey := surfaceKey{pluginID: pluginID, surface: surface}
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	s.mu.RLock()
	vs := s.views[key]
	if vs == nil || vs.BusyAction != "" || vs.editSeq != seq {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	var refreshTimer *time.Timer
	if !testing.Testing() {
		refreshTimer = time.AfterFunc(140*time.Millisecond, func() {
			s.mu.Lock()
			if cur := s.views[key]; cur != nil && cur.BusyAction == "" && cur.editSeq == seq {
				cur.Refreshing = true
				s.mu.Unlock()
				s.notifyRedraw()
				return
			}
			s.mu.Unlock()
		})
	}
	defer func() {
		if refreshTimer != nil {
			refreshTimer.Stop()
		}
		s.mu.Lock()
		wasRefreshing := false
		if cur := s.views[key]; cur != nil && cur.Refreshing && cur.editSeq == seq {
			cur.Refreshing = false
			wasRefreshing = true
		}
		s.mu.Unlock()
		if wasRefreshing {
			s.notifyRedraw()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	defer s.lockPluginRPC(pluginID)()
	s.mu.RLock()
	if s.views[key] != vs || vs.BusyAction != "" || vs.editSeq != seq {
		s.mu.RUnlock()
		return
	}
	valsCopy := make(map[string]any, len(vs.editedControls))
	for id := range vs.editedControls {
		valsCopy[id] = vs.Values[id]
	}
	s.mu.RUnlock()
	rt, err := s.ensureRuntime(ctx, pluginID, nbRoot)
	if err != nil {
		s.mu.Lock()
		if cur := s.views[key]; cur != nil && cur.BusyAction == "" && cur.editSeq == seq && !cur.errorFromBg {
			cur.Refreshing = false
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

	page, returnedVals, err := rt.RenderWithValuesResult(ctx, surface, valsCopy)
	if err != nil {
		s.mu.Lock()
		if cur := s.views[key]; cur != nil && cur.BusyAction == "" && cur.editSeq == seq && !cur.errorFromBg {
			cur.Refreshing = false
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
	cur.Refreshing = false
	cur.Page = page
	cur.lastNotebookRoot = nbRoot
	cur.lastActiveNote = activeNote
	cur.lastLocale = rt.locale()
	if !cur.errorFromBg && cur.RetryKind == RetryKindReload {
		cur.Error = ""
		cur.RetryKind = RetryKindNone
	}
	for k, v := range page.ExtractDefaultValues() {
		if _, exists := cur.Values[k]; !exists {
			cur.Values[k] = v
		}
	}
	for k, v := range returnedVals {
		cur.Values[k] = v
		delete(cur.editedControls, k)
		delete(cur.editedSeq, k)
	}
	if len(returnedVals) > 0 {
		s.formVersions[sKey]++
		cur.formVersion = s.formVersions[sKey]
	}
	s.mu.Unlock()
	s.notifyChanged()
}

func isStickyActionButton(actionID string) bool {
	id := strings.ToLower(strings.TrimSpace(actionID))
	return id == "test_conn" || strings.HasPrefix(id, "test_") || strings.HasSuffix(id, "_test")
}

func resolveActionTone(res *ActionResult, err error) string {
	if err != nil {
		return "danger"
	}
	if res == nil {
		return "info"
	}
	switch strings.ToLower(strings.TrimSpace(res.Tone)) {
	case "success":
		return "success"
	case "danger", "error":
		return "danger"
	case "warning", "warn":
		return "warning"
	case "info":
		return "info"
	}
	if res.OK != nil {
		if *res.OK {
			return "success"
		}
		return "danger"
	}
	lower := strings.ToLower(strings.TrimSpace(res.Status))
	for _, kw := range []string{
		"失败", "错误", "无法", "未连通", "不可", "无效", "不存在", "请先", "请填写", "请选择", "请完整", "不能为空", "拒绝",
		"failed", "error", "invalid", "missing", "unauthorized", "forbidden",
	} {
		if strings.Contains(lower, kw) {
			return "danger"
		}
	}
	for _, kw := range []string{
		"待确认", "暂停", "跳过", "已取消", "变化", "未发现", "未找到",
		"warning", "skipped", "cancelled", "canceled", "pending",
	} {
		if strings.Contains(lower, kw) {
			return "warning"
		}
	}
	return "success"
}

func (s *Supervisor) TriggerActionAsync(pluginID, actionID string) {
	s.TriggerSurfaceActionAsync(pluginID, SurfaceSettingsPage, actionID)
}

func (s *Supervisor) triggerSurfaceActionAsync(pluginID string, surface Surface, actionID string) {
	nbRoot := s.NotebookRoot()
	sKey := surfaceKey{pluginID: pluginID, surface: surface}
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}

	s.mu.Lock()
	vs := s.views[key]
	if vs == nil || vs.Loading || vs.BusyAction != "" {
		s.mu.Unlock()
		return
	}
	vs.BusyAction = actionID
	vs.Refreshing = false
	vs.Error = ""
	vs.Notice = ""
	vs.LastActionID = ""
	vs.LastActionTone = ""
	vs.LastActionMessage = ""
	vs.LastActionButtonActive = false
	s.stopFeedbackTimerLocked(key)
	vs.RetryKind = RetryKindNone
	vs.FailedActionID = ""
	vs.FailedNotePath = ""
	vs.FailedNoteRoot = ""
	vs.errorFromBg = false
	vs.editSeq++
	actionSeq := vs.editSeq
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	vs.actionCancel = cancel
	s.mu.Unlock()
	s.notifyRedraw()

	go func() {
		startedAt := time.Now()
		defer cancel()

		unlockRPC, err := s.lockPluginRPCContext(ctx, pluginID)
		var rt *Runtime
		if err == nil {
			rt, err = s.ensureRuntime(ctx, pluginID, nbRoot)
		}
		var res *ActionResult
		activeNote := s.activeNoteFor(nbRoot)
		if err == nil {
			rt.SetNotebookRoot(nbRoot)
			rt.SetActiveNote(activeNote)
			s.mu.RLock()
			stale := vs.formVersion != s.formVersions[sKey]
			staleSeq := vs.editSeq
			staleDrafts := pendingDraftValuesLocked(vs)
			s.mu.RUnlock()
			if stale {
				var fresh *UIPage
				var returned map[string]any
				fresh, returned, err = rt.RenderWithValuesResult(ctx, surface, staleDrafts)
				if err == nil {
					s.mu.Lock()
					if fresh != nil {
						vs.Page = fresh
						vs.lastLocale = rt.locale()
					}
					mergePageValuesUpToSeq(vs, fresh, returned, staleSeq, true)
					vs.formVersion = s.formVersions[sKey]
					s.mu.Unlock()
				}
			}
			if err == nil {
				s.mu.RLock()
				values := cloneAnyMap(vs.Values)
				actionSeq = vs.editSeq
				s.mu.RUnlock()
				res, err = rt.Action(ctx, surface, actionID, values)
			}
		}
		if !testing.Testing() {
			const minBusyBeat = 220 * time.Millisecond
			if elapsed := time.Since(startedAt); elapsed < minBusyBeat {
				time.Sleep(minBusyBeat - elapsed)
			}
		}

		tone := resolveActionTone(res, err)
		var openTarget, revealTarget string
		s.mu.Lock()
		if cur := s.views[key]; cur == vs {
			cur.actionCancel = nil
			cur.BusyAction = ""
			cur.lastNotebookRoot = nbRoot
			cur.lastActiveNote = activeNote
			if rt != nil && res != nil && res.Page != nil {
				cur.lastLocale = rt.locale()
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				cur.Notice = ""
			} else if err != nil {
				cur.Error = err.Error()
				cur.LastActionID = actionID
				cur.LastActionTone = "danger"
				cur.LastActionMessage = err.Error()
				cur.LastActionButtonActive = false
				cur.RetryKind = RetryKindAction
				cur.FailedActionID = actionID
				cur.errorFromBg = true
			} else if res != nil {
				s.formVersions[sKey]++
				cur.formVersion = s.formVersions[sKey]
				cur.Error = ""
				cur.RetryKind = RetryKindNone
				cur.FailedActionID = ""
				cur.errorFromBg = false
				statusMsg := strings.TrimSpace(res.Status)
				if statusMsg != "" {
					cur.Notice = statusMsg
				}
				cur.LastActionID = actionID
				cur.LastActionTone = tone
				cur.LastActionMessage = statusMsg
				cur.LastActionButtonActive = tone == "success"
				if cur.LastActionButtonActive && !testing.Testing() && !isStickyActionButton(actionID) {
					s.stopFeedbackTimerLocked(key)
					s.feedbackTimers[key] = time.AfterFunc(2400*time.Millisecond, func() {
						s.mu.Lock()
						changed := false
						if v := s.views[key]; v != nil && v.LastActionID == actionID && v.LastActionButtonActive {
							v.LastActionButtonActive = false
							changed = true
						}
						delete(s.feedbackTimers, key)
						s.mu.Unlock()
						if changed {
							s.notifyRedraw()
						}
					})
				}
				if res.Page != nil {
					cur.Page = res.Page
					if cur.Page.Status == "" && statusMsg != "" {
						cur.Page.Status = statusMsg
					}
					for k, v := range res.Page.ExtractDefaultValues() {
						if cur.editedControls[k] && cur.editedSeq[k] > actionSeq {
							continue
						}
						cur.Values[k] = v
						delete(cur.editedControls, k)
						delete(cur.editedSeq, k)
					}
				}
				for k, v := range res.Values {
					if cur.editedControls[k] && cur.editedSeq[k] > actionSeq {
						continue
					}
					cur.Values[k] = v
					delete(cur.editedControls, k)
					delete(cur.editedSeq, k)
				}
				openTarget = res.OpenPath
				revealTarget = res.RevealPath
			}
		}
		s.mu.Unlock()
		if unlockRPC != nil {
			unlockRPC()
		}
		s.notifyChanged()

		if openTarget != "" && !rt.closed.Load() {
			openLocalPathInOS(rt.cfg.DownloadsDir, openTarget, false)
		}
		if revealTarget != "" && !rt.closed.Load() {
			openLocalPathInOS(rt.cfg.DownloadsDir, revealTarget, true)
		}
	}()
}

func (s *Supervisor) CanCancelAction(pluginID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, v := range s.views {
		if key.pluginID == pluginID && (v.actionCancel != nil || len(v.queuedSaves) > 0) {
			return true
		}
	}
	return false
}

func (s *Supervisor) CancelAction(pluginID string) {
	s.mu.Lock()
	for key, v := range s.views {
		if key.pluginID != pluginID {
			continue
		}
		if v.actionCancel != nil {
			v.actionCancel()
		}
		if v.saveCancel != nil {
			v.saveCancel()
		}
		clear(v.queuedSaves)
		if v.BusyAction == "__retry_note_saved__" {
			v.BusyAction = ""
		}
	}
	s.mu.Unlock()
	s.notifyChanged()
}

// RetryPluginAsync retries the last failed operation for pluginID in the current notebook,
// distinguishing action/event/uninstall retries from full page reloads.
func (s *Supervisor) RetryPluginAsync(pluginID string) {
	s.RetrySurfaceAsync(pluginID, SurfaceSettingsPage)
}

// RetrySurfaceAsync retries the last failed operation for one plugin surface in the current notebook.
func (s *Supervisor) RetrySurfaceAsync(pluginID string, surface Surface) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}

	s.mu.Lock()
	vs := s.views[key]
	if vs == nil {
		s.mu.Unlock()
		s.ReloadSurfaceViewAsync(pluginID, surface)
		return
	}
	if vs.Loading || vs.BusyAction != "" {
		s.mu.Unlock()
		return
	}
	snapshot := s.snapshotViewLocked(key, vs)
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
			s.ReloadSurfaceViewAsync(pluginID, surface)
			return
		}
		target := s.views[viewKey{pluginID, noteRoot, SurfaceSettingsPage}]
		if target == nil || target.BusyAction != "" {
			s.mu.Unlock()
			return
		}
		target.BusyAction = "__retry_note_saved__"
		s.mu.Unlock()
		s.notifyChanged()
		s.queueNoteSaved(pluginID, noteRoot, notePath, relSlash)
		return
	}
	s.mu.Unlock()

	switch kind {
	case RetryKindAction:
		if actionID != "" {
			s.TriggerSurfaceActionAsync(pluginID, surface, actionID)
			return
		}
		s.ReloadSurfaceViewAsync(pluginID, surface)
	case RetryKindUninstall:
		s.UninstallAsync(pluginID, nil)
	default:
		s.ReloadSurfaceViewAsync(pluginID, surface)
	}
}

func pluginOutputPath(downloadsDir, rawPath string, reveal bool) (string, os.FileInfo) {
	cleaned := filepath.Clean(strings.TrimSpace(rawPath))
	if !filepath.IsAbs(cleaned) || !filepath.IsAbs(downloadsDir) {
		return "", nil
	}
	rel, err := filepath.Rel(downloadsDir, cleaned)
	if err != nil || !filepath.IsLocal(rel) {
		return "", nil
	}
	root, err := os.OpenRoot(downloadsDir)
	if err != nil {
		return "", nil
	}
	defer root.Close()
	if err := checkPluginParents(root, rel); err != nil {
		return "", nil
	}
	info, err := root.Lstat(rel)
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return "", nil
	}
	if !reveal && info.IsDir() && strings.EqualFold(filepath.Ext(cleaned), ".app") {
		return "", nil
	}
	if !reveal && !info.IsDir() {
		switch strings.ToLower(filepath.Ext(cleaned)) {
		case ".html", ".htm", ".pdf", ".txt", ".md", ".markdown", ".csv", ".tsv",
			".png", ".jpg", ".jpeg", ".webp", ".gif", ".docx", ".xlsx", ".pptx":
		default:
			return "", nil
		}
	}
	return cleaned, info
}

func openLocalPathInOS(downloadsDir, rawPath string, reveal bool) {
	cleaned, info := pluginOutputPath(downloadsDir, rawPath, reveal)
	if info == nil {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			cmd = exec.Command("open", "-R", cleaned)
		} else if info.IsDir() {
			cmd = exec.Command("open", "-a", "Finder", cleaned)
		} else {
			cmd = exec.Command("open", cleaned)
		}
	case "windows":
		if reveal && !info.IsDir() {
			cmd = exec.Command("explorer.exe", "/select,"+cleaned)
		} else {
			cmd = exec.Command("explorer.exe", cleaned)
		}
	default:
		target := cleaned
		if reveal && !info.IsDir() {
			target = filepath.Dir(cleaned)
		}
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
}

func (s *Supervisor) ShutdownAll() {
	// Release host calls parked on the UI thread first: ShutdownAll usually
	// runs on that thread, and a plugin waiting on it would stall shutdown.
	if c, ok := s.cfg.UI.(interface{ HostClosing() }); ok {
		c.HostClosing()
	}
	s.mu.Lock()
	s.stopping = true
	for k := range s.liveTimers {
		s.stopLiveTimerLocked(k)
	}
	clear(s.liveViewedAt)
	for k := range s.feedbackTimers {
		s.stopFeedbackTimerLocked(k)
	}
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

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if slice, ok := v.([]string); ok {
			cp := make([]string, len(slice))
			copy(cp, slice)
			out[k] = cp
		} else {
			out[k] = v
		}
	}
	return out
}

func (s *Supervisor) snapshotViewLocked(current viewKey, in *PluginViewState) PluginViewState {
	out := PluginViewState{
		Loading:                in.Loading,
		Refreshing:             in.Refreshing,
		BusyAction:             in.BusyAction,
		Error:                  in.Error,
		Notice:                 in.Notice,
		Page:                   in.Page,
		Values:                 cloneAnyMap(in.Values),
		LastActionID:           in.LastActionID,
		LastActionTone:         in.LastActionTone,
		LastActionMessage:      in.LastActionMessage,
		LastActionButtonActive: in.LastActionButtonActive,
		RetryKind:              in.RetryKind,
		FailedActionID:         in.FailedActionID,
		FailedNotePath:         in.FailedNotePath,
		FailedNoteRoot:         in.FailedNoteRoot,
	}
	if current.surface == SurfaceSettingsPage {
		var failedPath, failedRoot, failure string
		for key, v := range s.views {
			if key.pluginID != current.pluginID || key.surface != SurfaceSettingsPage {
				continue
			}
			out.PendingSaves += len(v.queuedSaves)
			for path, message := range v.failedSaves {
				if v.queuedSaves[path] != nil {
					continue
				}
				preferCurrent := key == current && failedRoot != current.notebookRoot
				samePriority := (key == current) == (failedRoot == current.notebookRoot)
				if failedPath == "" || preferCurrent || (samePriority && path < failedPath) {
					failedPath, failedRoot, failure = path, key.notebookRoot, message
				}
			}
		}
		if out.Error == "" && out.BusyAction == "" && failedPath != "" {
			if failedRoot != current.notebookRoot {
				failure = filepath.Base(failedRoot) + " / " + failure
			}
			out.Error = failure
			out.FailedNotePath = failedPath
			out.FailedNoteRoot = failedRoot
			out.RetryKind = RetryKindNoteSaved
		}
	}
	return out
}

type notInstalledError string

func (e notInstalledError) Error() string { return "plugin not installed: " + string(e) }

func osErrNotInstalled(id string) error { return notInstalledError(id) }

type pluginDisabledError string

func (e pluginDisabledError) Error() string { return "plugin disabled: " + string(e) }

func errPluginDisabled(id string) error { return pluginDisabledError(id) }
