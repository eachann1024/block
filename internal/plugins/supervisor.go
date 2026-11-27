package plugins

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"block/internal/files"
	"block/internal/plugins/wasmrt"
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

// BackgroundError describes a generic plugin-reported background error or warning
// surfaced in the plugin list and titlebar error pill via host.ui.set_background_error.
type BackgroundError struct {
	Key          string         `json:"key"`
	Label        string         `json:"label"`
	Detail       string         `json:"detail,omitempty"`
	RetryAction  string         `json:"retry_action,omitempty"`
	RetryLabel   string         `json:"retry_label,omitempty"`
	RetryPayload map[string]any `json:"retry_payload,omitempty"`
	Severity     string         `json:"severity,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// PluginViewState holds the non-blocking UI snapshot for an installed plugin's Settings page.
type PluginViewState struct {
	Loading                bool
	Refreshing             bool
	BusyAction             string
	ActionProgress         ActionProgress
	ChannelProgress        map[string]ActionProgress
	Error                  string
	Notice                 string
	Page                   *UIPage
	Values                 map[string]any
	LastActionID           string
	LastActionTone         string
	LastActionMessage      string
	LastActionBusyLabel    string
	LastActionSuccessLabel string
	LastActionErrorLabel   string
	LastActionRetryLabel   string
	LastActionIcon         *UIIcon
	LastActionButtonActive bool
	LastActionWasHeader    bool
	AutosaveTone           string
	AutosaveMessage        string
	BackgroundError        *BackgroundError

	RetryKind         RetryKind
	FailedActionID    string
	FailedNotePath    string
	FailedNoteRoot    string
	SavePausedConfirm bool
	failedSaves       map[string]string
	failedSaveEvents  map[string]failedNoteEvent
	savePausedConfirm map[string]bool
	queuedSaves       map[string]*noteSaveRequest
	activeSaves       int
	retrySavePath     string
	PendingSaves      int

	lastNotebookRoot   string
	lastActiveNote     string
	lastNotebooksKey   string
	lastLocale         string
	dialogToken        string
	editSeq            uint64
	formVersion        uint64
	errorFromBg        bool
	initialLoadStarted bool
	pendingReload      bool
	actionCancel       context.CancelFunc
	editedControls     map[string]bool
	editedSeq          map[string]uint64
	autosave           formAutosave

	cachedSnapValid bool
	cachedSnapAt    time.Time
	cachedEpoch     uint64
	cachedRawActive string
	cachedRawNBs    []string
	cachedFailedLen int
	cachedSnap      *PluginViewState
}

type SupervisorConfig struct {
	AppDataDir           string
	HostVersion          string
	GetNotebookRoot        func() string
	GetNotebooks           func() []string
	GetActiveNote          func() string
	GetPinnedTitlebarSlots func() []string
	GetLocale              func() string
	GetTheme             func() string
	GetPerfSnapshot      func(refreshDisk bool) (*PerfSnapshot, error)
	PickDirectory        func(title, defaultPath string, onPicked func(path string))
	PickFile             func(title, defaultPath string, extensions []string, onPicked func(path string))
	AddNotebook          func(path string) error
	ApplyNoteWrite       func(absPath, content string, write func() (bool, error)) (bool, error)
	ApplyNoteTrash       func(pluginID, absPath string, verify, trash func() error) error
	OnChanged            func()
	OnIgnoredDirsChanged func()
	OnRedraw             func()
	IsOnline             func() bool
	// UI lets plugins drive host UI (toasts, dialogs, panels, editor).
	UI            UIHost
	System        SystemHost
	AI            AIHost
	OnStageChange func(stage *ActiveStage)
	OnStageEmit   func(ev StageEvent)
}

// Supervisor coordinates StoreManager, lazy plugin Runtime subprocesses, async form actions,
// and non-blocking note-saved event dispatch without ever stalling the 60fps UI render thread.
type Supervisor struct {
	rpcLocks   sync.Map // plugin ID -> *sync.Mutex, serializes notebook-scoped RPCs
	store      *StoreManager
	cfg        SupervisorConfig
	contrib    *contribRegistry
	system     systemState
	wasmEngine *wasmrt.Engine
	wasmCtx    context.Context
	wasmCancel context.CancelFunc
	warmupDone chan struct{}

	mu                 sync.RWMutex
	viewEpoch          atomic.Uint64
	notebookEpoch      atomic.Uint64
	catalog            *StoreIndex
	installed          []InstalledApp
	ignoredDirs        atomic.Value // immutable map[string]struct{}
	runtimeIgnoredDirs map[string][]string
	installEpoch       map[string]uint64
	runtimes           map[string]*Runtime
	stageMu            sync.RWMutex
	activeStage        *ActiveStage
	views              map[viewKey]*PluginViewState
	lastViewRoot       map[surfaceKey]string
	formVersions       map[surfaceKey]uint64
	liveTimers         map[surfaceKey]*time.Timer
	liveViewedAt       map[surfaceKey]time.Time
	feedbackTimers     map[viewKey]*time.Timer
	activeActionKey    map[string]viewKey
	activeActionRT     map[string]*Runtime
	lastProgressRedraw map[string]time.Time
	uninstallErrors    map[string]string
	bgErrors           map[string]map[string]BackgroundError
	storeRefreshing    bool
	installingID       string
	batchUpdating      bool
	replacingID        string
	stopping           bool
	storeErr           string
	storeNotice        string
}

func hasWasmOrJSPlugin(apps []InstalledApp) bool {
	for _, app := range apps {
		if app.Manifest.Entry.JS != "" || app.Manifest.Entry.Wasm != "" {
			return true
		}
	}
	return false
}

func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	sm := NewStoreManager(cfg.AppDataDir)
	_ = sm.EnsureDirs()
	installed, _ := sm.ListInstalled()
	wasmCtx, wasmCancel := context.WithCancel(context.Background())
	wasmEng, err := wasmrt.NewEngine(filepath.Join(sm.CacheRoot, "wasm-cache"))
	if err != nil {
		files.LogErrorf("init shared wasm runtime failed: %v", err)
	}
	warmupDone := make(chan struct{})
	s := &Supervisor{
		store:              sm,
		cfg:                cfg,
		contrib:            newContribRegistry(cfg.AppDataDir),
		catalog:            sm.LoadCachedOrSeedCatalog(),
		installed:          installed,
		wasmEngine:         wasmEng,
		wasmCtx:            wasmCtx,
		wasmCancel:         wasmCancel,
		warmupDone:         warmupDone,
		installEpoch:       make(map[string]uint64),
		runtimes:           make(map[string]*Runtime),
		views:              make(map[viewKey]*PluginViewState),
		lastViewRoot:       make(map[surfaceKey]string),
		formVersions:       make(map[surfaceKey]uint64),
		liveTimers:         make(map[surfaceKey]*time.Timer),
		liveViewedAt:       make(map[surfaceKey]time.Time),
		feedbackTimers:     make(map[viewKey]*time.Timer),
		activeActionKey:    make(map[string]viewKey),
		activeActionRT:     make(map[string]*Runtime),
		lastProgressRedraw: make(map[string]time.Time),
		uninstallErrors:    make(map[string]string),
		bgErrors:           make(map[string]map[string]BackgroundError),
	}
	sm.beforeReplace = s.preparePluginReplacement
	s.refreshIgnoredDirsLocked()
	SyncActiveLanguagesSeq(installed, NextLanguageSyncSeq())
	s.restoreActiveThemeSkin()
	if s.wasmEngine != nil && (!testing.Testing() || hasWasmOrJSPlugin(installed)) {
		go func() {
			defer close(s.warmupDone)
			if err := s.wasmEngine.Warmup(s.wasmCtx); err != nil && s.wasmCtx.Err() == nil {
				files.LogErrorf("warmup wasm runtime failed: %v", err)
			}
		}()
	} else {
		close(s.warmupDone)
	}
	return s
}

func (s *Supervisor) preparePluginReplacement(pluginID string) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return errors.New("plugin supervisor is stopping")
	}
	if s.replacingID != pluginID && s.isPluginBusyLocked(pluginID) {
		s.mu.Unlock()
		return errors.New("plugin has an active task running; wait or cancel it before updating")
	}
	s.replacingID = pluginID
	s.installEpoch[pluginID]++
	rt := s.runtimes[pluginID]
	delete(s.runtimes, pluginID)
	delete(s.activeActionKey, pluginID)
	delete(s.activeActionRT, pluginID)
	s.clearUninstallErrorLocked(pluginID)
	s.mu.Unlock()
	if rt != nil {
		go s.clearInactiveSystemBadge(pluginID)
		s.closeStageForPlugin(pluginID)
		return rt.Shutdown(context.Background())
	}
	s.closeStageForPlugin(pluginID)
	go s.clearInactiveSystemBadge(pluginID)
	return nil
}

func (s *Supervisor) Store() *StoreManager {
	return s.store
}

// MatchesEntrySource reports whether an already-installed plugin matches the
// recorded installation provenance of catalog entry.
func (s *Supervisor) MatchesEntrySource(installed InstalledApp, entry StoreAppEntry) bool {
	if s == nil || s.store == nil {
		return false
	}
	return s.store.MatchesEntrySource(installed, entry)
}

// PluginHasPermission reports whether pluginID is currently installed, enabled,
// and holds permission p.
func (s *Supervisor) PluginHasPermission(pluginID string, p Permission) bool {
	if s == nil || strings.TrimSpace(pluginID) == "" {
		return false
	}
	m, ok := s.installedManifest(pluginID)
	return ok && m.HasPermission(p)
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
		_ = s.GrantFSPath(pluginID, p, "picker:"+string(surface)+":"+controlID)
		s.updateSurfaceControlValue(pluginID, nbRoot, surface, controlID, p, true)
	})
}

// PickChecklistFolderForControl lets the user choose a folder for a checklist
// declared with add_folder. The folder is registered as a notebook (without
// switching to it) so host.notes.* accepts it, then appended to the control value.
func (s *Supervisor) PickChecklistFolderForControl(pluginID string, surface Surface, controlID, title string) {
	if !s.CanPickDirectory() {
		return
	}
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	s.mu.RLock()
	view := s.views[key]
	s.mu.RUnlock()
	if view == nil {
		return
	}
	s.pickDirectory(title, nbRoot, func(picked string) {
		if s.NotebookRoot() != nbRoot {
			return
		}
		abs, err := filepath.Abs(picked)
		if err != nil {
			return
		}
		if s.cfg.AddNotebook != nil {
			if err := s.cfg.AddNotebook(abs); err != nil {
				return
			}
		}
		_ = s.GrantFSPath(pluginID, abs, "checklist:"+string(surface)+":"+controlID)
		s.mu.RLock()
		var current []string
		if live := s.views[key]; live != nil {
			current, _ = StringSliceValue(live.Values[controlID])
		}
		s.mu.RUnlock()
		next := make([]string, 0, len(current)+1)
		for _, existing := range current {
			if existing == abs {
				return
			}
			next = append(next, existing)
		}
		next = append(next, abs)
		s.updateSurfaceControlValue(pluginID, nbRoot, surface, controlID, next, true)
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

// GrantFSPath persists a directory or file authorization for pluginID under its apps-data dir.
func (s *Supervisor) GrantFSPath(pluginID, path, source string) error {
	if s == nil || strings.TrimSpace(pluginID) == "" {
		return errors.New("plugin ID is required")
	}
	var dataDir string
	s.mu.RLock()
	for _, a := range s.installed {
		if a.Manifest.ID == pluginID && strings.TrimSpace(a.DataDir) != "" {
			dataDir = a.DataDir
			break
		}
	}
	s.mu.RUnlock()
	if dataDir == "" && s.store != nil && strings.TrimSpace(s.store.DataRoot) != "" {
		dataDir = filepath.Join(s.store.DataRoot, pluginID)
	}
	if dataDir == "" {
		return errors.New("plugin data directory unavailable")
	}
	return GrantFSPath(dataDir, path, source)
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
		if !cur.errorFromBg && cur.RetryKind != RetryKindAction {
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

// EnabledTelemetryPluginIDs returns enabled official store plugin IDs and folds
// any enabled local or unofficial custom plugins into a single "custom" marker.
func (s *Supervisor) EnabledTelemetryPluginIDs() []string {
	s.mu.RLock()
	catalog := s.catalog
	installed := append([]InstalledApp(nil), s.installed...)
	s.mu.RUnlock()

	officialEntries := make(map[string]StoreAppEntry)
	if catalog != nil {
		for _, entry := range catalog.Apps {
			if entry.ID != "" {
				officialEntries[entry.ID] = entry
			}
		}
	}

	var out []string
	seen := make(map[string]bool)
	hasCustom := false
	for _, inst := range installed {
		if !inst.Enabled {
			continue
		}
		id := strings.TrimSpace(inst.Manifest.ID)
		entry, ok := officialEntries[id]
		if ok && s.store != nil && s.store.MatchesOfficialSource(inst, entry) {
			norm := strings.ToLower(id)
			if !seen[norm] {
				seen[norm] = true
				out = append(out, norm)
			}
		} else {
			hasCustom = true
		}
	}
	if hasCustom && !seen["custom"] {
		out = append(out, "custom")
	}
	slices.Sort(out)
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
	if !enabled && s.isPluginBusyLocked(pluginID) {
		s.mu.Unlock()
		return
	}
	for _, inst := range s.installed {
		if inst.Manifest.ID == pluginID && inst.NeedsUpdate {
			s.mu.Unlock()
			return
		}
	}
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
	if changed {
		s.installEpoch[pluginID]++
	}
	if !enabled {
		rt = s.runtimes[pluginID]
		delete(s.runtimes, pluginID)
		s.stopLiveTimersForPluginLocked(pluginID)
		s.deleteViewsForPluginLocked(pluginID)
	}
	var langSnap []InstalledApp
	var langSeq uint64
	var ignoredDirsChanged bool
	if changed || rt != nil {
		langSnap = append([]InstalledApp(nil), s.installed...)
		langSeq = NextLanguageSyncSeq()
		ignoredDirsChanged = s.refreshIgnoredDirsLocked()
	}
	s.mu.Unlock()
	s.notifyIgnoredDirsChanged(ignoredDirsChanged)

	if rt != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = rt.Shutdown(ctx)
		}()
	}
	if enabled {
		s.warmupPluginAsync(pluginID)
	} else {
		s.clearPluginThemeSkin(pluginID)
		s.closeStageForPlugin(pluginID)
		if changed {
			s.clearPluginScheduler(pluginID)
			go s.clearInactiveSystemBadge(pluginID)
		}
	}
	if changed || rt != nil {
		SyncActiveLanguagesSeq(langSnap, langSeq)
		s.notifyChanged()
	}
}

// WarmupAppearancePluginsAsync initializes enabled UI plugins (appearance sections,
// backdrops, sidebar extensions, status items, titlebar buttons) so host UI
// surfaces stay in sync on startup and notebook switches without waiting for
// the user to open settings. Plugins that only push persisted preferences
// (like editor background or hidden titlebar slots) skip re-spawning once
// warmed up for their installed version; plugins with in-memory dynamic
// surfaces (status items, titlebar buttons, sidebar sections/badges) warm up
// for the active notebook scope.
func (s *Supervisor) WarmupAppearancePluginsAsync() {
	if s == nil {
		return
	}
	s.mu.RLock()
	installed := append([]InstalledApp(nil), s.installed...)
	s.mu.RUnlock()
	for _, inst := range installed {
		if !inst.Enabled || !shouldAutoWarmupPlugin(inst.Manifest) {
			continue
		}
		if !inst.Manifest.HasDeclaredPermission(PermUISidebar) && !needsTransientUISync(inst.Manifest) &&
			s.contrib != nil && s.contrib.isWarmedUp(inst.Manifest.ID, inst.Manifest.Version) {
			continue
		}
		s.warmupPluginAsync(inst.Manifest.ID)
	}
}

// OnNotebookSwitched clears in-memory per-notebook transient UI overrides
// (titlebar badges/active highlights and status bar overrides), closes active
// plugin dialogs tied to the previous notebook, advances the notebook epoch,
// and re-warms enabled UI plugins for the newly active notebook scope.
func (s *Supervisor) OnNotebookSwitched() {
	if s == nil {
		return
	}
	s.notebookEpoch.Add(1)
	if s.cfg.UI != nil {
		s.cfg.UI.CloseDialog("", "")
	}
	if s.contrib != nil {
		s.contrib.clearTransientOverrides()
	}
	s.notifyChanged()
	s.WarmupAppearancePluginsAsync()
}

func shouldAutoWarmupPlugin(m Manifest) bool {
	if m.IsDeclarativeOnly() {
		return false
	}
	if m.EffectiveCategory() == CategoryAppearance || m.HasDeclaredPermission(PermUISidebar) {
		return true
	}
	if m.Contributes == nil {
		return false
	}
	if m.Contributes.TitlebarManager ||
		len(m.Contributes.Backdrops) > 0 ||
		len(m.Contributes.BlockDecorators) > 0 ||
		len(m.Contributes.Themes) > 0 ||
		len(m.Contributes.Stages) > 0 ||
		len(m.Contributes.SidebarSections) > 0 ||
		len(m.Contributes.FileBadges) > 0 ||
		len(m.Contributes.FileIcons) > 0 ||
		len(m.Contributes.StatusItems) > 0 ||
		len(m.Contributes.TitlebarButtons) > 0 ||
		len(m.Contributes.MenubarItems) > 0 {
		return true
	}
	for _, sec := range m.Contributes.SettingsSections {
		if sec.Tab == "appearance" {
			return true
		}
	}
	return false
}

func needsTransientUISync(m Manifest) bool {
	if m.IsDeclarativeOnly() || m.Contributes == nil {
		return false
	}
	return len(m.Contributes.StatusItems) > 0 ||
		len(m.Contributes.SidebarSections) > 0 ||
		len(m.Contributes.FileBadges) > 0 ||
		len(m.Contributes.FileIcons) > 0 ||
		len(m.Contributes.TitlebarButtons) > 0
}

// warmupPluginAsync initializes a newly installed or enabled UI plugin in the
// background so its plugin.initialize hook can sync host UI state (such as
// editor backdrops, sidebar badges, status text, or titlebar badges) without
// waiting for the user to open the plugin's settings page.
func (s *Supervisor) warmupPluginAsync(pluginID string) {
	if s == nil || strings.TrimSpace(pluginID) == "" {
		return
	}
	inst, ok := s.GetInstalled(pluginID)
	if !ok || !inst.Enabled || inst.Manifest.IsDeclarativeOnly() {
		return
	}
	if !shouldAutoWarmupPlugin(inst.Manifest) &&
		(inst.Manifest.Contributes == nil || len(inst.Manifest.Contributes.SettingsSections) == 0) {
		return
	}
	version := inst.Manifest.Version
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		unlock, err := s.lockPluginRPCContext(ctx, pluginID)
		if err != nil {
			return
		}
		defer unlock()
		if !s.IsPluginEnabled(pluginID) {
			return
		}
		nbRoot := s.NotebookRoot()
		if _, err := s.ensureRuntime(ctx, pluginID, nbRoot); err == nil && s.contrib != nil {
			s.contrib.markWarmedUp(pluginID, version)
		}
	}()
}

func (s *Supervisor) StoreStatus() (refreshing bool, installingID string, errMsg string, notice string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storeRefreshing, s.installingID, s.storeErr, s.storeNotice
}

// IsBatchUpdating reports whether an Update All operation is in progress.
func (s *Supervisor) IsBatchUpdating() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.batchUpdating
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

func formatStoreError(err error, locale string) string {
	if err == nil {
		return ""
	}
	isEn := normalizeLocale(locale) == "en"
	var netWrap *storeNetworkError
	if errors.As(err, &netWrap) {
		var netErr net.Error
		if errors.Is(netWrap, context.DeadlineExceeded) || (errors.As(netWrap, &netErr) && netErr.Timeout()) {
			if isEn {
				return "Download timed out. Check your connection and try again."
			}
			return "网络请求超时，请检查网络后重试"
		}
		if isEn {
			return "Network connection interrupted. Please try again."
		}
		return "网络连接中断，下载失败，请稍后重试"
	}
	var permErr *PermissionEscalationError
	if errors.As(err, &permErr) {
		labels := make([]string, 0, len(permErr.Added))
		for _, p := range permErr.Added {
			labels = append(labels, PermissionLabel(p, locale))
		}
		if permErr.SourceChanged {
			if len(labels) == 0 {
				if isEn {
					return "Plugin source changed and requires confirmation before replacing"
				}
				return "插件来源已变更，替换前需要先确认"
			}
			if isEn {
				return fmt.Sprintf("Plugin source changed and requires confirmation: %s", strings.Join(labels, ", "))
			}
			return fmt.Sprintf("插件来源已变更，需重新确认权限：%s", strings.Join(labels, "、"))
		}
		if isEn {
			return fmt.Sprintf("New permissions require confirmation: %s", strings.Join(labels, ", "))
		}
		return fmt.Sprintf("新增权限需要先确认：%s", strings.Join(labels, "、"))
	}
	return err.Error()
}

func (s *Supervisor) startCatalogRefreshLocked() {
	s.storeRefreshing = true
	s.storeErr = ""

	go func() {
		totalTimeout := time.Duration(max(len(s.store.IndexURLs), 2))*6*time.Second + 2*time.Second
		ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
		defer cancel()
		idx, err := s.store.RefreshCatalog(ctx)
		locale := s.Locale()
		s.mu.Lock()
		s.storeRefreshing = false
		if idx != nil {
			s.catalog = idx
		}
		if err != nil && (idx == nil || len(idx.Apps) == 0) {
			s.storeErr = formatStoreError(err, locale)
		}
		s.mu.Unlock()
		s.notifyChanged()
	}()
}

func (s *Supervisor) InstallEntryAsync(entry StoreAppEntry, onInstalled func(pluginID string)) {
	_, wasUpdate := s.GetInstalled(entry.ID)
	s.startInstallAsync(entry.ID, wasUpdate, func(ctx context.Context) (*InstalledApp, error) {
		return s.store.InstallEntryExplicit(ctx, entry)
	}, onInstalled)
}

// InstallEntryConfirmedAsync installs or updates a catalog entry after the user
// has approved approvedPermissions (or when no sensitive permissions are added).
func (s *Supervisor) InstallEntryConfirmedAsync(entry StoreAppEntry, approvedPermissions []Permission, allowSourceChange bool, onInstalled func(pluginID string)) bool {
	_, wasUpdate := s.GetInstalled(entry.ID)
	approvedCopy := append([]Permission(nil), approvedPermissions...)
	return s.startInstallAsync(entry.ID, wasUpdate, func(ctx context.Context) (*InstalledApp, error) {
		return s.store.InstallEntryConfirmed(ctx, entry, approvedCopy, allowSourceChange)
	}, onInstalled)
}

// UpdateEntriesAsync sequentially updates all provided store entries that do
// not introduce new sensitive permissions. Any entries that require new
// permissions are skipped and returned via onDone so the UI can prompt the user.
func (s *Supervisor) UpdateEntriesAsync(entries []StoreAppEntry, onInstalled func(pluginID string), onDone func(escalated []StoreAppEntry)) {
	s.mu.Lock()
	if s.stopping || s.installingID != "" {
		s.mu.Unlock()
		return
	}
	queue := make([]StoreAppEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.ID == DiagnosticsPluginID || s.isPluginBusyLocked(entry.ID) {
			continue
		}
		queue = append(queue, entry)
	}
	if len(queue) == 0 {
		s.mu.Unlock()
		if onDone != nil {
			onDone(nil)
		}
		return
	}
	s.batchUpdating = true
	s.installingID = queue[0].ID
	s.storeErr = ""
	s.storeNotice = ""
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		updated := 0
		var failedMsgs []string
		var escalated []StoreAppEntry
		for _, entry := range queue {
			s.mu.Lock()
			if s.stopping {
				s.batchUpdating = false
				s.installingID = ""
				s.replacingID = ""
				s.mu.Unlock()
				s.notifyChanged()
				return
			}
			s.installingID = entry.ID
			s.mu.Unlock()
			s.notifyChanged()

			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			inst, err := s.store.UpdateEntryNoEscalation(ctx, entry)
			cancel()
			if err != nil {
				s.mu.Lock()
				var reloadTargets []viewKey
				if s.replacingID == entry.ID {
					s.replacingID = ""
					reloadTargets = s.recoverViewsAfterReplaceLocked(entry.ID)
				}
				s.mu.Unlock()
				for _, k := range reloadTargets {
					go s.reloadSurfaceViewForNotebookAsync(k.pluginID, k.notebookRoot, k.surface)
				}
				var permErr *PermissionEscalationError
				if errors.As(err, &permErr) {
					escalated = append(escalated, entry)
					continue
				}
				failedMsgs = append(failedMsgs, fmt.Sprintf("%s: %s", entry.ID, formatStoreError(err, s.Locale())))
				continue
			}

			installedList, _ := s.store.ListInstalled()
			s.mu.Lock()
			if s.replacingID == entry.ID || (inst != nil && s.replacingID == inst.Manifest.ID) {
				s.replacingID = ""
			}
			langSnap := s.applyInstalledLocked(inst, installedList)
			langSeq := NextLanguageSyncSeq()
			ignoredDirsChanged := s.refreshIgnoredDirsLocked()
			s.mu.Unlock()
			s.notifyIgnoredDirsChanged(ignoredDirsChanged)

			SyncActiveLanguagesSeq(langSnap, langSeq)
			if onInstalled != nil {
				onInstalled(inst.Manifest.ID)
			}
			s.warmupPluginAsync(inst.Manifest.ID)
			updated++
			s.notifyChanged()
		}

		s.mu.Lock()
		s.batchUpdating = false
		s.installingID = ""
		prevReplacing := s.replacingID
		s.replacingID = ""
		var reloadTargets []viewKey
		if prevReplacing != "" {
			reloadTargets = s.recoverViewsAfterReplaceLocked(prevReplacing)
		}
		locale := s.Locale()
		isEn := normalizeLocale(locale) == "en"
		var summaryMsg string
		if updated > 0 {
			if isEn {
				if updated == 1 {
					summaryMsg = "Updated 1 plugin"
				} else {
					summaryMsg = fmt.Sprintf("Updated %d plugins", updated)
				}
			} else {
				summaryMsg = fmt.Sprintf("已更新 %d 个插件", updated)
			}
			s.storeNotice = summaryMsg
		}
		var errBody string
		if len(failedMsgs) > 0 {
			errBody = strings.Join(failedMsgs, "; ")
			s.storeErr = errBody
		}
		s.mu.Unlock()

		if updated > 0 && errBody == "" {
			s.NotifyHost(ToastRequest{
				Message: summaryMsg,
				Tone:    "success",
			})
		} else if updated > 0 && errBody != "" {
			s.NotifyHost(ToastRequest{
				Message: summaryMsg,
				Body:    errBody,
				Tone:    "warning",
			})
		} else if errBody != "" {
			failTitle := "插件更新失败"
			if isEn {
				failTitle = "Plugin update failed"
			}
			s.NotifyHost(ToastRequest{
				Message: failTitle,
				Body:    errBody,
				Tone:    "error",
			})
		}
		if onDone != nil {
			onDone(escalated)
		}
		for _, k := range reloadTargets {
			go s.reloadSurfaceViewForNotebookAsync(k.pluginID, k.notebookRoot, k.surface)
		}
		s.notifyChanged()
	}()
}

func (s *Supervisor) InstallFromSourceAsync(spec string, onInstalled func(pluginID string)) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	s.startInstallAsync("__manual__", false, func(ctx context.Context) (*InstalledApp, error) {
		return s.store.InstallFromSourceExplicit(ctx, spec)
	}, onInstalled)
}

// StageManualInstallAsync downloads/copies and validates a manual plugin source
// without installing it. If no sensitive permissions require confirmation it
// commits immediately; otherwise it hands the staged package to onNeedsConfirm.
func (s *Supervisor) StageManualInstallAsync(spec string, onNeedsConfirm func(*StagedPlugin), onInstalled func(pluginID string)) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	s.mu.Lock()
	if s.stopping || s.installingID != "" {
		s.mu.Unlock()
		return false
	}
	s.installingID = "__manual__"
	s.storeErr = ""
	s.storeNotice = ""
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		staged, err := s.store.StageManualSource(ctx, spec)
		if err != nil {
			s.finishInstallAsync(nil, err, nil, false)
			return
		}
		if !staged.NeedsConfirmation() {
			s.mu.Lock()
			stopping := s.stopping
			if stopping {
				s.installingID = ""
				s.replacingID = ""
			}
			s.mu.Unlock()
			if stopping {
				staged.Cleanup()
				return
			}
			var approved []Permission
			if staged.Previous != nil {
				approved = staged.Previous.Permissions
			}
			inst, commitErr := s.store.CommitStagedPlugin(staged, approved)
			s.finishInstallAsync(inst, commitErr, onInstalled, staged.IsUpdate)
			return
		}
		s.mu.Lock()
		stopping := s.stopping
		s.installingID = ""
		s.replacingID = ""
		s.mu.Unlock()
		if stopping || onNeedsConfirm == nil {
			staged.Cleanup()
			s.notifyChanged()
			return
		}
		onNeedsConfirm(staged)
		s.notifyChanged()
	}()
	return true
}

// CommitStagedInstallAsync installs a previously staged manual plugin after
// user permission confirmation. Returns false if another install is active.
func (s *Supervisor) CommitStagedInstallAsync(staged *StagedPlugin, approvedPermissions []Permission, onInstalled func(pluginID string)) bool {
	if staged == nil {
		return false
	}
	approvedCopy := append([]Permission(nil), approvedPermissions...)
	return s.startInstallAsync(staged.Manifest.ID, staged.IsUpdate, func(_ context.Context) (*InstalledApp, error) {
		return s.store.CommitStagedPlugin(staged, approvedCopy)
	}, onInstalled)
}

func (s *Supervisor) startInstallAsync(
	installingKey string,
	wasUpdate bool,
	install func(context.Context) (*InstalledApp, error),
	onInstalled func(pluginID string),
) bool {
	s.mu.Lock()
	if s.stopping || s.installingID != "" {
		s.mu.Unlock()
		return false
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
		s.finishInstallAsync(inst, err, onInstalled, wasUpdate)
	}()
	return true
}

func (s *Supervisor) applyInstalledLocked(inst *InstalledApp, installedList []InstalledApp) []InstalledApp {
	for i := range installedList {
		installedList[i].Enabled = !installedList[i].NeedsUpdate && s.store.IsEnabled(installedList[i].Manifest.ID)
	}
	s.installed = installedList
	langSnap := append([]InstalledApp(nil), s.installed...)
	if inst != nil {
		s.installEpoch[inst.Manifest.ID]++
		delete(s.runtimes, inst.Manifest.ID)
		s.deleteViewsForPluginLocked(inst.Manifest.ID)
	}
	return langSnap
}

func (s *Supervisor) finishInstallAsync(inst *InstalledApp, err error, onInstalled func(pluginID string), wasUpdate bool) {
	installedList, _ := s.store.ListInstalled()
	locale := s.Locale()
	isEn := normalizeLocale(locale) == "en"

	s.mu.Lock()
	prevReplacing := s.replacingID
	s.installingID = ""
	s.replacingID = ""
	if err != nil {
		msg := formatStoreError(err, locale)
		s.storeErr = msg
		var reloadTargets []viewKey
		if prevReplacing != "" {
			reloadTargets = s.recoverViewsAfterReplaceLocked(prevReplacing)
		}
		s.mu.Unlock()
		for _, k := range reloadTargets {
			go s.reloadSurfaceViewForNotebookAsync(k.pluginID, k.notebookRoot, k.surface)
		}
		title := "插件安装失败"
		if wasUpdate {
			title = "插件更新失败"
		}
		if isEn {
			title = "Plugin install failed"
			if wasUpdate {
				title = "Plugin update failed"
			}
		}
		s.NotifyHost(ToastRequest{
			Message: title,
			Body:    msg,
			Tone:    "error",
		})
		s.notifyChanged()
		return
	}
	langSnap := s.applyInstalledLocked(inst, installedList)
	var reloadTargets []viewKey
	if prevReplacing != "" && (inst == nil || prevReplacing != inst.Manifest.ID) {
		reloadTargets = s.recoverViewsAfterReplaceLocked(prevReplacing)
	}
	langSeq := NextLanguageSyncSeq()
	ignoredDirsChanged := s.refreshIgnoredDirsLocked()
	s.mu.Unlock()
	s.notifyIgnoredDirsChanged(ignoredDirsChanged)
	for _, k := range reloadTargets {
		go s.reloadSurfaceViewForNotebookAsync(k.pluginID, k.notebookRoot, k.surface)
	}
	SyncActiveLanguagesSeq(langSnap, langSeq)
	if onInstalled != nil {
		onInstalled(inst.Manifest.ID)
	}
	s.warmupPluginAsync(inst.Manifest.ID)
	name := inst.Manifest.Name.Resolve(locale)
	if name == "" {
		name = inst.Manifest.ID
	}
	title := fmt.Sprintf("已安装 %s", name)
	if wasUpdate {
		title = fmt.Sprintf("已更新 %s", name)
	}
	if isEn {
		title = fmt.Sprintf("Installed %s", name)
		if wasUpdate {
			title = fmt.Sprintf("Updated %s", name)
		}
	}
	ic := inst.Manifest.Icon
	s.NotifyHost(ToastRequest{
		Message: title,
		Tone:    "success",
		Icon:    &ic,
	})
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
	if s.isPluginBusyLocked(pluginID) {
		s.uninstallErrors[pluginID] = "plugin has an active task running; cancel it before uninstalling"
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	s.clearUninstallErrorLocked(pluginID)
	s.installingID = "__uninstall__"
	s.replacingID = pluginID
	if vs := s.views[key]; vs != nil {
		vs.BusyAction = "__uninstall__"
		vs.ActionProgress = ActionProgress{}
		vs.ChannelProgress = nil
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
		wasTitlebarMgr := false
		for _, a := range s.installed {
			if a.Manifest.ID == pluginID && isTitlebarManagerManifest(a.Manifest) {
				wasTitlebarMgr = true
				break
			}
		}
		s.installEpoch[pluginID]++
		rt := s.runtimes[pluginID]
		delete(s.runtimes, pluginID)
		delete(s.activeActionKey, pluginID)
		delete(s.activeActionRT, pluginID)
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
				installedList[i].Enabled = !installedList[i].NeedsUpdate && s.store.IsEnabled(installedList[i].Manifest.ID)
			}
			s.installed = installedList
		}
		if err != nil {
			s.storeErr = err.Error()
			s.uninstallErrors[pluginID] = err.Error()
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
			reloadTargets := s.recoverViewsAfterReplaceLocked(pluginID)
			s.mu.Unlock()
			for _, k := range reloadTargets {
				go s.reloadSurfaceViewForNotebookAsync(k.pluginID, k.notebookRoot, k.surface)
			}
			s.notifyChanged()
			return
		}
		s.storeErr = ""
		delete(s.uninstallErrors, pluginID)
		s.deleteViewsForPluginLocked(pluginID)
		hasRemainingTitlebarMgr := false
		for _, a := range s.installed {
			if isTitlebarManagerManifest(a.Manifest) {
				hasRemainingTitlebarMgr = true
				break
			}
		}
		langSnap := append([]InstalledApp(nil), s.installed...)
		langSeq := NextLanguageSyncSeq()
		ignoredDirsChanged := s.refreshIgnoredDirsLocked()
		s.mu.Unlock()
		s.notifyIgnoredDirsChanged(ignoredDirsChanged)
		SyncActiveLanguagesSeq(langSnap, langSeq)
		s.contrib.forgetPlugin(pluginID, wasTitlebarMgr && !hasRemainingTitlebarMgr)
		s.clearPluginThemeSkin(pluginID)
		s.closeStageForPlugin(pluginID)
		s.clearPluginScheduler(pluginID)
		s.clearInactiveSystemBadge(pluginID)

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

func (s *Supervisor) clearUninstallErrorLocked(pluginID string) {
	delete(s.uninstallErrors, pluginID)
	for k, v := range s.views {
		if k.pluginID == pluginID && v.RetryKind == RetryKindUninstall {
			v.Error = ""
			v.RetryKind = RetryKindNone
			v.errorFromBg = false
		}
	}
}

func (s *Supervisor) deleteViewsForPluginLocked(pluginID string) {
	delete(s.runtimeIgnoredDirs, pluginID)
	delete(s.bgErrors, pluginID)
	s.stopLiveTimersForPluginLocked(pluginID)
	delete(s.activeActionKey, pluginID)
	delete(s.activeActionRT, pluginID)
	delete(s.lastProgressRedraw, pluginID)
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
			for _, req := range s.views[k].queuedSaves {
				if req != nil && req.cancel != nil {
					req.cancel()
				}
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

// ResetDialogView clears cached view state for a dialog surface whenever a
// dialog opens (new token) or closes so stale pages/actions from a previous
// dialog instance can never be displayed or executed against the new instance.
func (s *Supervisor) ResetDialogView(pluginID, dialogID, dialogToken string) {
	if pluginID == "" || dialogID == "" {
		return
	}
	surface := DialogSurface(dialogID)
	s.mu.Lock()
	changed := false
	for k, vs := range s.views {
		if k.pluginID == pluginID && k.surface == surface {
			if vs != nil && vs.actionCancel != nil {
				vs.actionCancel()
				vs.actionCancel = nil
			}
			delete(s.views, k)
			changed = true
		}
	}
	if changed || dialogToken != "" {
		s.viewEpoch.Add(1)
	}
	s.mu.Unlock()
	if changed {
		s.notifyChanged()
	}
}

func (s *Supervisor) ensureSurfaceView(pluginID string, surface Surface) PluginViewState {
	if inst, ok := s.GetInstalled(pluginID); !ok || !inst.Enabled {
		return PluginViewState{Values: map[string]any{}}
	}
	isDialog := strings.HasPrefix(string(surface), SurfacePrefixDialog)
	curDialogToken := ""
	if isDialog && s.cfg.UI != nil {
		curDialogToken = s.cfg.UI.ActiveDialogToken(pluginID, strings.TrimPrefix(string(surface), SurfacePrefixDialog))
		if curDialogToken == "" {
			return PluginViewState{Values: map[string]any{}}
		}
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
		s.replacingID != pluginID &&
		!rootChanged &&
		(!isDialog || vs.dialogToken == curDialogToken) &&
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
	if s.replacingID == pluginID {
		snap := PluginViewState{Loading: true, Values: map[string]any{}}
		if cur, exists := s.views[key]; exists && cur != nil {
			if cur.Page != nil {
				snap = s.snapshotViewLocked(key, cur)
				snap.Loading = true
				snap.Error = ""
			} else {
				cur.pendingReload = true
			}
		}
		s.mu.Unlock()
		return snap
	}
	vs, ok := s.views[key]
	if ok && isDialog && vs.dialogToken != curDialogToken {
		if vs.actionCancel != nil {
			vs.actionCancel()
			vs.actionCancel = nil
		}
		delete(s.views, key)
		s.viewEpoch.Add(1)
		ok = false
	}
	if ok {
		if vs.Page == nil && !vs.Loading && !vs.initialLoadStarted {
			vs.Loading = true
			vs.initialLoadStarted = true
			vs.lastNotebookRoot = nbRoot
			vs.lastActiveNote = activeNote
			vs.lastNotebooksKey = nbKey
			vs.lastLocale = curLocale
			vs.dialogToken = curDialogToken
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
		dialogToken:        curDialogToken,
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
	s.reloadSurfaceViewForNotebookAsync(pluginID, s.NotebookRoot(), surface)
}

func (s *Supervisor) reloadSurfaceViewForNotebookAsync(pluginID, nbRoot string, surface Surface) {
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	curDialogToken := ""
	if strings.HasPrefix(string(surface), SurfacePrefixDialog) && s.cfg.UI != nil {
		curDialogToken = s.cfg.UI.ActiveDialogToken(pluginID, strings.TrimPrefix(string(surface), SurfacePrefixDialog))
		if curDialogToken == "" {
			return
		}
	}

	s.mu.Lock()
	vs, ok := s.views[key]
	if !ok {
		vs = &PluginViewState{Values: map[string]any{}, lastNotebookRoot: nbRoot, dialogToken: curDialogToken}
		s.views[key] = vs
	} else if curDialogToken != "" && vs.dialogToken != curDialogToken {
		vs.dialogToken = curDialogToken
	}
	if vs.Loading {
		vs.pendingReload = true
		s.mu.Unlock()
		return
	}
	vs.Loading = true
	vs.pendingReload = false
	vs.initialLoadStarted = true
	vs.Error = ""
	vs.Notice = ""
	vs.RetryKind = RetryKindNone
	vs.FailedActionID = ""
	vs.FailedNotePath = ""
	vs.FailedNoteRoot = ""
	vs.errorFromBg = false
	s.clearUninstallErrorLocked(pluginID)
	s.mu.Unlock()
	s.notifyChanged()

	go s.loadAndRenderSurface(pluginID, nbRoot, surface)
}

func (s *Supervisor) loadAndRenderSurface(pluginID, nbRoot string, surface Surface) {
	s.loadAndRenderSurfaceOnce(pluginID, nbRoot, surface, false)
}

func (s *Supervisor) loadAndRenderSurfaceOnce(pluginID, nbRoot string, surface Surface, retriedOnErr bool) {
	sKey := surfaceKey{pluginID: pluginID, surface: surface}
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	// Results only land in the view that asked for them. A disable/enable or
	// uninstall in the meantime replaces the view, and its own load runs next.
	s.mu.RLock()
	origin := s.views[key]
	originDialogToken := ""
	if origin != nil {
		originDialogToken = origin.dialogToken
	}
	startEpoch := s.installEpoch[pluginID]
	s.mu.RUnlock()
	defer s.lockPluginRPC(pluginID)()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	isDialog := strings.HasPrefix(string(surface), SurfacePrefixDialog)
	dialogID := strings.TrimPrefix(string(surface), SurfacePrefixDialog)
	if isDialog && s.cfg.UI != nil && originDialogToken != "" && s.cfg.UI.ActiveDialogToken(pluginID, dialogID) != originDialogToken {
		return
	}
	if cleanNotebookKey(s.NotebookRoot()) != cleanNotebookKey(nbRoot) {
		// Notebook switched while this reload was queued; reset initial-load flag
		// when no page ever rendered and invalidate cached snapshots so returning
		// to this notebook triggers a fresh load instead of rendering blank.
		s.mu.Lock()
		if vs := s.views[key]; vs != nil && vs == origin {
			vs.Loading = false
			vs.pendingReload = false
			vs.lastNotebookRoot = ""
			if vs.Page == nil {
				vs.initialLoadStarted = false
			}
			vs.cachedSnap = nil
			s.viewEpoch.Add(1)
		}
		s.mu.Unlock()
		return
	}
	rt, attemptedLocale, err := s.ensureRuntimeWithLocale(ctx, pluginID, nbRoot)
	if err != nil {
		sameNB := cleanNotebookKey(s.NotebookRoot()) == cleanNotebookKey(nbRoot)
		rerun := false
		logErr := false
		s.mu.Lock()
		if vs := s.views[key]; vs != nil && vs == origin {
			vs.formVersion = s.formVersions[sKey]
			if attemptedLocale != "" {
				vs.lastLocale = attemptedLocale
			}
			vs.cachedSnapValid = false
			replacingNow := s.replacingID == pluginID
			replacedDuringCall := s.installEpoch[pluginID] != startEpoch
			if isPluginReplacingError(err) || replacingNow || replacedDuringCall {
				vs.Error = ""
				vs.RetryKind = RetryKindNone
				if !replacingNow && !retriedOnErr && sameNB {
					vs.Loading = true
					vs.pendingReload = false
					rerun = true
				} else if replacingNow {
					vs.Loading = true
					vs.pendingReload = true
				} else {
					vs.Loading = false
					vs.pendingReload = false
					if vs.Page == nil {
						vs.initialLoadStarted = false
					}
				}
			} else {
				vs.Loading = false
				if !vs.errorFromBg {
					vs.Error = err.Error()
					vs.RetryKind = RetryKindReload
					logErr = !isPluginDisabledError(err)
				}
				if vs.pendingReload && !retriedOnErr && sameNB {
					vs.pendingReload = false
					vs.Loading = true
					rerun = true
				} else {
					vs.pendingReload = false
				}
			}
		}
		s.mu.Unlock()
		if logErr {
			files.LogErrorf("plugin surface load failed: id=%s surface=%s err=%v", pluginID, surface, err)
		}
		if rerun && cleanNotebookKey(s.NotebookRoot()) != cleanNotebookKey(nbRoot) {
			rerun = false
			s.mu.Lock()
			if vs := s.views[key]; vs != nil && vs == origin {
				vs.Loading = false
			}
			s.mu.Unlock()
		}
		s.notifyChanged()
		if rerun {
			go s.loadAndRenderSurfaceOnce(pluginID, nbRoot, surface, true)
		}
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
	if isDialog && s.cfg.UI != nil && originDialogToken != "" && s.cfg.UI.ActiveDialogToken(pluginID, dialogID) != originDialogToken {
		return
	}
	sameNB := cleanNotebookKey(s.NotebookRoot()) == cleanNotebookKey(nbRoot)
	rerun := false
	nextRetried := retriedOnErr
	logErr := false
	s.mu.Lock()
	if vs := s.views[key]; vs != nil && vs == origin {
		vs.formVersion = s.formVersions[sKey]
		vs.lastNotebookRoot = nbRoot
		vs.lastActiveNote = activeNote
		vs.lastLocale = rt.locale()
		vs.cachedSnapValid = false
		if err != nil {
			replacingNow := s.replacingID == pluginID
			replacedDuringCall := s.installEpoch[pluginID] != startEpoch
			if replacingNow || replacedDuringCall {
				vs.Error = ""
				vs.RetryKind = RetryKindNone
				if !replacingNow && !retriedOnErr && sameNB {
					vs.Loading = true
					vs.pendingReload = false
					rerun = true
					nextRetried = true
				} else if replacingNow {
					vs.Loading = true
					vs.pendingReload = true
				} else {
					vs.Loading = false
					vs.pendingReload = false
					if vs.Page == nil {
						vs.initialLoadStarted = false
					}
				}
			} else {
				vs.Loading = false
				if !vs.errorFromBg {
					vs.Error = err.Error()
					vs.RetryKind = RetryKindReload
					logErr = !isPluginDisabledError(err)
				}
				if vs.pendingReload && !retriedOnErr && sameNB {
					vs.pendingReload = false
					vs.Loading = true
					rerun = true
					nextRetried = true
				} else {
					vs.pendingReload = false
				}
			}
		} else {
			vs.Loading = false
			preserveActionErr := vs.errorFromBg || vs.RetryKind == RetryKindAction
			if !preserveActionErr {
				vs.Error = ""
				vs.RetryKind = RetryKindNone
				vs.FailedActionID = ""
				// Clear stale soft-error and notice state from a previous process/session after a clean reload.
				vs.Notice = ""
				vs.LastActionID = ""
				vs.LastActionTone = ""
				vs.LastActionMessage = ""
				vs.LastActionButtonActive = false
				vs.LastActionWasHeader = false
			}
			vs.Page = page
			mergePageValuesUpToSeq(vs, page, returnedVals, renderSeq, true)
			if vs.pendingReload && sameNB {
				vs.pendingReload = false
				vs.Loading = true
				rerun = true
				nextRetried = false
			} else {
				vs.pendingReload = false
			}
		}
	}
	s.mu.Unlock()
	if logErr {
		files.LogErrorf("plugin surface render failed: id=%s surface=%s err=%v", pluginID, surface, err)
	}
	if rerun && cleanNotebookKey(s.NotebookRoot()) != cleanNotebookKey(nbRoot) {
		rerun = false
		s.mu.Lock()
		if vs := s.views[key]; vs != nil && vs == origin {
			vs.Loading = false
		}
		s.mu.Unlock()
	}
	s.notifyChanged()
	if rerun {
		go s.loadAndRenderSurfaceOnce(pluginID, nbRoot, surface, nextRetried)
	}
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
		if view.protectAutosaveValue(k) || (checkSeq && view.editedControls[k] && view.editedSeq[k] > maxSeq) {
			continue
		}
		delete(view.Values, k)
		delete(view.editedControls, k)
		delete(view.editedSeq, k)
	}
	for k, v := range defaults {
		if !view.editedControls[k] && !view.protectAutosaveValue(k) {
			view.Values[k] = v
		}
	}
	for k, v := range returned {
		if view.protectAutosaveValue(k) || (checkSeq && view.editedControls[k] && view.editedSeq[k] > maxSeq) {
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

func (s *Supervisor) checkRuntimeStartAllowedLocked(ctx context.Context, pluginID string, inst InstalledApp) error {
	current := false
	for _, app := range s.installed {
		if app.Manifest.ID == pluginID {
			current = app.Enabled && app.InstallDir == inst.InstallDir && app.InstalledAt.Equal(inst.InstalledAt)
			break
		}
	}
	if s.replacingID == pluginID && !s.stopping && current && ctx.Err() == nil {
		return errPluginReplacing(pluginID)
	}
	if s.stopping || s.replacingID == pluginID || !current || ctx.Err() != nil {
		return errors.New("plugin changed while starting")
	}
	return nil
}

func (s *Supervisor) ensureRuntimeWithLocale(ctx context.Context, pluginID, nbRoot string) (*Runtime, string, error) {
	s.mu.RLock()
	stopping := s.stopping
	replacing := s.replacingID == pluginID
	s.mu.RUnlock()
	if stopping {
		return nil, "", errors.New("plugin supervisor is stopping")
	}
	if replacing {
		return nil, "", errPluginReplacing(pluginID)
	}
	inst, ok := s.GetInstalled(pluginID)
	if !ok {
		return nil, "", osErrNotInstalled(pluginID)
	}
	if inst.NeedsUpdate {
		return nil, "", errRetiredPluginEntry
	}
	if inst.Manifest.IsDeclarativeOnly() {
		return nil, "", errors.New("declarative language plugin has no runtime")
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
	curEpoch := s.notebookEpoch.Load()
	s.mu.RLock()
	existing := s.runtimes[pluginID]
	s.mu.RUnlock()
	if existing != nil && !existing.closed.Load() {
		epochFresh := !needsTransientUISync(inst.Manifest) || existing.syncedEpoch.Load() == curEpoch
		if existing.notebookRoot() == nbRoot && existing.locale() == wantLocale && epochFresh {
			select {
			case <-existing.exited:
			default:
				return existing, existing.locale(), nil
			}
		}
		// Plugins receive the notebook root, locale, and transient UI sync at initialization.
		_ = existing.Shutdown(ctx)
	}
	hostVer := s.cfg.HostVersion
	if hostVer == "" {
		hostVer = "1.0.379"
	}
	rawLocale := s.Locale()
	attemptedLocale := normalizeLocale(rawLocale)
	var rtPtr atomic.Pointer[Runtime]
	var openNoteFn func(notebookRoot, relPath string, rootInfo os.FileInfo, content []byte) error
	if s.cfg.UI != nil {
		uiHost := s.cfg.UI
		openNoteFn = func(notebookRoot, relPath string, rootInfo os.FileInfo, content []byte) error {
			expectedEpoch := s.notebookEpoch.Load()
			checkValid := func() error {
				s.mu.RLock()
				active := rtPtr.Load() != nil && s.isRuntimeActiveLocked(pluginID, rtPtr.Load())
				s.mu.RUnlock()
				if !active {
					return errors.New("plugin runtime is no longer active")
				}
				if s.notebookEpoch.Load() != expectedEpoch {
					return errors.New("notebook changed during open")
				}
				return nil
			}
			if err := checkValid(); err != nil {
				return err
			}
			return uiHost.OpenNote(notebookRoot, relPath, rootInfo, content, checkValid)
		}
	}
	curTheme := "system"
	if s.cfg.GetTheme != nil {
		curTheme = s.cfg.GetTheme()
	}
	cfg := HostConfig{
		PluginDir:         inst.InstallDir,
		DataDir:           inst.DataDir,
		CacheDir:          filepath.Join(s.store.CacheRoot, "wasm-cache"),
		DownloadsDir:      "",
		NotebookRoot:      nbRoot,
		ActiveNote:        s.activeNoteFor(nbRoot),
		Locale:            rawLocale,
		Theme:             curTheme,
		HostVersion:       hostVer,
		GetNotebooks:      s.Notebooks,
		GetPerfSnapshot:   s.cfg.GetPerfSnapshot,
		ApplyNoteWrite:    s.cfg.ApplyNoteWrite,
		ApplyNoteTrash:    s.cfg.ApplyNoteTrash,
		OnNoteWritten:     s.notifyNoteWritten,
		OnActionProgress:  s.reportActionProgress,
		OpenNote:          openNoteFn,
		ContribHost:       s.dispatchExtensionHostMethod,
		AI:                s.cfg.AI,
		SystemIntegration: s.cfg.System != nil,
		wasmEngine:        s.wasmEngine,
	}
	if cfg.SystemIntegration && inst.Manifest.HasPermission(PermSystemSecrets) {
		cfg.credentialScope = inst.credentialScope()
	}
	s.mu.RLock()
	err := s.checkRuntimeStartAllowedLocked(ctx, pluginID, inst)
	s.mu.RUnlock()
	if err != nil {
		return nil, attemptedLocale, err
	}
	rt, err := StartRuntime(context.Background(), inst.Manifest, cfg)
	if err != nil {
		return nil, attemptedLocale, err
	}
	s.mu.Lock()
	if err := s.checkRuntimeStartAllowedLocked(ctx, pluginID, inst); err != nil {
		s.mu.Unlock()
		rt.abort()
		return nil, attemptedLocale, err
	}
	prev := s.runtimes[pluginID]
	rt.syncedEpoch.Store(curEpoch)
	rtPtr.Store(rt)
	s.runtimes[pluginID] = rt
	s.mu.Unlock()
	if prev != nil && prev != existing {
		prev.abort()
	}
	if cfg.SystemIntegration && inst.Manifest.HasPermission(PermSystemBadge) {
		go func() {
			<-rt.exited
			s.clearInactiveSystemBadge(pluginID)
		}()
	}
	if err := rt.Initialize(ctx); err != nil {
		rt.abort()
		s.mu.Lock()
		if s.runtimes[pluginID] == rt {
			delete(s.runtimes, pluginID)
		}
		s.mu.Unlock()
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
	previous := vs.Values[controlID]
	vs.Values[controlID] = val
	if vs.editedControls == nil {
		vs.editedControls = make(map[string]bool)
	}
	if vs.editedSeq == nil {
		vs.editedSeq = make(map[string]uint64)
	}
	vs.editedControls[controlID] = true
	hadFeedback := vs.Notice != "" || vs.LastActionID != "" || vs.LastActionMessage != "" || vs.LastActionButtonActive || vs.RetryKind == RetryKindAction
	vs.Notice = ""
	vs.LastActionID = ""
	vs.LastActionTone = ""
	vs.LastActionMessage = ""
	vs.LastActionBusyLabel = ""
	vs.LastActionSuccessLabel = ""
	vs.LastActionErrorLabel = ""
	vs.LastActionRetryLabel = ""
	vs.LastActionIcon = nil
	vs.LastActionButtonActive = false
	vs.LastActionWasHeader = false
	if vs.Error == "" && vs.RetryKind == RetryKindAction {
		vs.RetryKind = RetryKindNone
		vs.FailedActionID = ""
		vs.errorFromBg = false
	}
	s.stopFeedbackTimerLocked(key)
	vs.editSeq++
	vs.editedSeq[controlID] = vs.editSeq
	if item, ok := vs.Page.autosaveControl(controlID); ok {
		s.recordAutosaveEditLocked(key, vs, item, previous, val)
	}
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
	startEpoch := s.installEpoch[pluginID]
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

	defer s.lockPluginRPC(pluginID)()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
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
		if cur := s.views[key]; cur != nil && cur == vs && cur.BusyAction == "" && cur.editSeq == seq && !cur.errorFromBg {
			cur.Refreshing = false
			cur.cachedSnapValid = false
			replacingNow := s.replacingID == pluginID
			replacedDuringCall := s.installEpoch[pluginID] != startEpoch
			if isPluginReplacingError(err) || replacingNow || replacedDuringCall {
				cur.pendingReload = true
			} else {
				cur.Error = err.Error()
				cur.RetryKind = RetryKindReload
			}
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
		if cur := s.views[key]; cur != nil && cur == vs && cur.BusyAction == "" && cur.editSeq == seq && !cur.errorFromBg {
			cur.Refreshing = false
			cur.cachedSnapValid = false
			replacingNow := s.replacingID == pluginID
			replacedDuringCall := s.installEpoch[pluginID] != startEpoch
			if replacingNow || replacedDuringCall {
				cur.pendingReload = true
			} else {
				cur.Error = err.Error()
				cur.RetryKind = RetryKindReload
			}
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
	if cur == nil || cur != vs || cur.BusyAction != "" || cur.editSeq != seq {
		s.mu.Unlock()
		return
	}
	cur.Refreshing = false
	cur.lastNotebookRoot = nbRoot
	cur.lastActiveNote = activeNote
	cur.lastLocale = rt.locale()
	if !cur.errorFromBg && cur.RetryKind == RetryKindReload {
		cur.Error = ""
		cur.RetryKind = RetryKindNone
	}
	updates := page.ExtractDefaultValues()
	for k := range updates {
		if _, exists := cur.Values[k]; exists {
			delete(updates, k)
		}
	}
	for k, v := range returnedVals {
		updates[k] = v
	}
	cur.mergeAutosavePage(page, updates, seq)
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

func (s *Supervisor) triggerSurfaceActionAsync(pluginID string, surface Surface, actionID string, extraPayload map[string]any) {
	notebookEpoch := s.notebookEpoch.Load()
	nbRoot := s.NotebookRoot()
	sKey := surfaceKey{pluginID: pluginID, surface: surface}
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: surface}
	isDialog := strings.HasPrefix(string(surface), SurfacePrefixDialog)
	var dialogToken string
	if isDialog && s.cfg.UI != nil {
		dialogToken = s.cfg.UI.ActiveDialogToken(pluginID, strings.TrimPrefix(string(surface), SurfacePrefixDialog))
		if dialogToken == "" {
			return
		}
	}

	s.mu.Lock()
	vs := s.views[key]
	if vs == nil || vs.Loading || vs.BusyAction != "" {
		s.mu.Unlock()
		return
	}
	if isDialog && (vs.Page == nil || (s.cfg.UI != nil && vs.dialogToken != dialogToken)) {
		s.mu.Unlock()
		return
	}
	vs.BusyAction = actionID
	vs.ActionProgress = ActionProgress{}
	vs.ChannelProgress = nil
	vs.Refreshing = false
	vs.Error = ""
	vs.Notice = ""
	vs.LastActionID = ""
	vs.LastActionTone = ""
	vs.LastActionMessage = ""
	vs.LastActionBusyLabel = ""
	vs.LastActionSuccessLabel = ""
	vs.LastActionErrorLabel = ""
	vs.LastActionRetryLabel = ""
	vs.LastActionIcon = nil
	vs.LastActionButtonActive = false
	s.stopFeedbackTimerLocked(key)
	vs.RetryKind = RetryKindNone
	vs.FailedActionID = ""
	vs.FailedNotePath = ""
	vs.FailedNoteRoot = ""
	vs.errorFromBg = false
	vs.editSeq++
	actionSeq := vs.editSeq
	queueCtx, cancel := context.WithCancel(context.Background())
	vs.actionCancel = cancel
	s.mu.Unlock()
	s.notifyRedraw()

	go func() {
		defer cancel()

		unlockRPC, err := s.lockPluginRPCContext(queueCtx, pluginID)
		ctx, finish := context.WithTimeout(queueCtx, 120*time.Second)
		defer finish()
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
				s.mu.Lock()
				values := cloneAnyMap(vs.Values)
				for pk, pv := range extraPayload {
					if values == nil {
						values = make(map[string]any, len(extraPayload))
					}
					values[pk] = pv
				}
				actionSeq = vs.editSeq
				wasHeader := vs.Page.HasHeaderAction(actionID)
				s.activeActionKey[pluginID] = key
				s.activeActionRT[pluginID] = rt
				s.mu.Unlock()
				scope := systemAction{notebookRoot: nbRoot, epoch: notebookEpoch}
				actionCtx := context.WithValue(ctx, systemActionKey{}, scope)
				res, err = rt.ActionWithDialogToken(actionCtx, surface, actionID, values, dialogToken)
				s.mu.Lock()
				if s.activeActionRT[pluginID] == rt && s.activeActionKey[pluginID] == key {
					delete(s.activeActionKey, pluginID)
					delete(s.activeActionRT, pluginID)
				}
				s.mu.Unlock()
				_ = wasHeader
			}
		}

		if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			files.LogErrorf("plugin action failed: id=%s action=%s err=%v", pluginID, actionID, err)
		}

		tone := resolveActionTone(res, err)
		var openTarget, revealTarget string
		reloadAfterCancel := false
		s.mu.Lock()
		if cur := s.views[key]; cur == vs {
			prevWasHeader := cur.Page.HasHeaderAction(actionID)
			cur.actionCancel = nil
			if cur.BusyAction != "__uninstall__" {
				cur.BusyAction = ""
			}
			cur.ActionProgress = ActionProgress{}
			cur.ChannelProgress = nil
			cur.lastNotebookRoot = nbRoot
			cur.lastActiveNote = activeNote
			if rt != nil && res != nil && res.Page != nil {
				cur.lastLocale = rt.locale()
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				cur.Notice = ""
				reloadAfterCancel = true
			} else if err != nil {
				cur.Error = err.Error()
				cur.LastActionID = actionID
				cur.LastActionTone = "danger"
				cur.LastActionMessage = err.Error()
				cur.LastActionBusyLabel = ""
				cur.LastActionSuccessLabel = ""
				cur.LastActionErrorLabel = ""
				cur.LastActionRetryLabel = ""
				cur.LastActionIcon = nil
				cur.LastActionButtonActive = false
				cur.LastActionWasHeader = prevWasHeader
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
				if tone == "success" && len(res.SyncedNotebooks) > 0 {
					s.clearNotebookSaveErrorsLocked(pluginID, res.SyncedNotebooks)
				}
				statusMsg := strings.TrimSpace(res.Status)
				if statusMsg != "" {
					cur.Notice = statusMsg
				}
				isHeader := prevWasHeader || res.Page.HasHeaderAction(actionID)
				if tone == "danger" && isHeader {
					if statusMsg == "" {
						statusMsg = "action failed"
					}
					cur.RetryKind = RetryKindAction
					cur.FailedActionID = actionID
					cur.errorFromBg = true
				}
				cur.LastActionID = actionID
				cur.LastActionTone = tone
				cur.LastActionMessage = statusMsg
				cur.LastActionBusyLabel = res.BusyLabel
				cur.LastActionSuccessLabel = res.SuccessLabel
				cur.LastActionErrorLabel = res.ErrorLabel
				cur.LastActionRetryLabel = res.RetryLabel
				if res.Icon != nil {
					icCopy := *res.Icon
					cur.LastActionIcon = &icCopy
				} else {
					cur.LastActionIcon = nil
				}
				cur.LastActionButtonActive = tone == "success"
				cur.LastActionWasHeader = isHeader
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
				keepDrafts := isStickyActionButton(actionID) && len(res.Values) == 0
				if res.Page != nil {
					cur.Page = res.Page
					cur.autosave.deferred = nil
					if cur.Page.Status == "" && statusMsg != "" {
						cur.Page.Status = statusMsg
					}
					for k, v := range res.Page.ExtractDefaultValues() {
						if cur.autosave.edits[k] != nil || (cur.editedControls[k] && (keepDrafts || cur.editedSeq[k] > actionSeq)) {
							continue
						}
						cur.Values[k] = v
						delete(cur.editedControls, k)
						delete(cur.editedSeq, k)
					}
				}
				for k, v := range res.Values {
					if cur.autosave.edits[k] != nil || (cur.editedControls[k] && cur.editedSeq[k] > actionSeq) {
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
		if reloadAfterCancel {
			s.ReloadSurfaceViewAsync(pluginID, surface)
		}

		if openTarget != "" && !rt.closed.Load() {
			openLocalPathInOS(rt.cfg.DownloadsDir, openTarget, false)
		}
		if revealTarget != "" && !rt.closed.Load() {
			openLocalPathInOS(rt.cfg.DownloadsDir, revealTarget, true)
		}
	}()
}

func (s *Supervisor) reportActionProgress(rt *Runtime, progress ActionProgress) {
	if rt == nil {
		return
	}
	pluginID := rt.manifest.ID
	const minRedrawInterval = 40 * time.Millisecond

	s.mu.Lock()
	if s.runtimes[pluginID] != rt || s.activeActionRT[pluginID] != rt {
		s.mu.Unlock()
		return
	}
	key, ok := s.activeActionKey[pluginID]
	if !ok {
		s.mu.Unlock()
		return
	}
	v := s.views[key]
	if v == nil || v.actionCancel == nil || v.BusyAction == "" || v.BusyAction == "__uninstall__" {
		s.mu.Unlock()
		return
	}
	now := time.Now()
	var isFirst, isTerminal bool
	if progress.Channel != "" {
		if v.ChannelProgress == nil {
			v.ChannelProgress = make(map[string]ActionProgress, 4)
		}
		prev, existed := v.ChannelProgress[progress.Channel]
		if existed && prev == progress {
			s.mu.Unlock()
			return
		}
		v.ChannelProgress[progress.Channel] = progress
		isFirst = !existed
		isTerminal = !progress.Running || (progress.Total > 0 && progress.Done >= progress.Total) || (progress.HasProgress && progress.Progress >= 1)
	} else {
		prev := v.ActionProgress
		if prev == progress {
			s.mu.Unlock()
			return
		}
		v.ActionProgress = progress
		isFirst = prev == (ActionProgress{})
		isTerminal = (progress.Total > 0 && progress.Done >= progress.Total) || (progress.HasProgress && progress.Progress >= 1)
	}
	s.viewEpoch.Add(1)

	shouldRedraw := isFirst || isTerminal || now.Sub(s.lastProgressRedraw[pluginID]) >= minRedrawInterval
	if shouldRedraw {
		s.lastProgressRedraw[pluginID] = now
	}
	s.mu.Unlock()
	if shouldRedraw {
		s.notifyRedraw()
	}
}

func (s *Supervisor) isPluginBusyLocked(pluginID string) bool {
	if s.system.activeEvents[pluginID] != nil {
		return true
	}
	if s.replacingID == pluginID && s.installingID != "" {
		return true
	}
	for key, v := range s.views {
		if key.pluginID != pluginID {
			continue
		}
		if v.BusyAction != "" || v.actionCancel != nil || v.activeSaves > 0 || len(v.queuedSaves) > 0 {
			return true
		}
	}
	return false
}

// IsPluginBusyAnyNotebook reports whether pluginID has an active action,
// background save sync, or install/uninstall running in any notebook.
func (s *Supervisor) IsPluginBusyAnyNotebook(pluginID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isPluginBusyLocked(pluginID)
}

// IsPluginUninstalling reports whether pluginID is currently being uninstalled.
func (s *Supervisor) IsPluginUninstalling(pluginID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.installingID == "__uninstall__" && s.replacingID == pluginID
}

// UninstallError returns the last uninstall failure message for pluginID, if any.
func (s *Supervisor) UninstallError(pluginID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.uninstallErrors[pluginID]
}

// RestartPluginAsync stops any running process for pluginID (when idle) and
// reloads its Settings page view from a fresh process.
func (s *Supervisor) RestartPluginAsync(pluginID string) {
	s.mu.Lock()
	if s.isPluginBusyLocked(pluginID) {
		s.mu.Unlock()
		return
	}
	rt := s.runtimes[pluginID]
	delete(s.runtimes, pluginID)
	delete(s.activeActionKey, pluginID)
	delete(s.activeActionRT, pluginID)
	s.clearUninstallErrorLocked(pluginID)
	s.mu.Unlock()
	if rt != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = rt.Shutdown(ctx)
			cancel()
			s.ReloadPluginViewAsync(pluginID)
		}()
		return
	}
	s.ReloadPluginViewAsync(pluginID)
}

func (s *Supervisor) CanCancelAction(pluginID string) bool {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: SurfaceSettingsPage}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for vk, v := range s.views {
		if vk.pluginID != pluginID || vk.surface != SurfaceSettingsPage || v == nil {
			continue
		}
		if vk == key && v.actionCancel != nil {
			return true
		}
		if v.BusyAction == "__retry_note_saved__" {
			if v.retrySavePath != "" {
				if req := v.queuedSaves[v.retrySavePath]; req != nil {
					return true
				}
			} else {
				for _, req := range v.queuedSaves {
					if req != nil {
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *Supervisor) CancelAction(pluginID string) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: SurfaceSettingsPage}
	s.mu.Lock()
	for vk, v := range s.views {
		if vk.pluginID != pluginID || vk.surface != SurfaceSettingsPage || v == nil {
			continue
		}
		if vk == key && v.actionCancel != nil {
			v.actionCancel()
		}
		if v.BusyAction == "__retry_note_saved__" {
			if v.retrySavePath != "" {
				if req := v.queuedSaves[v.retrySavePath]; req != nil && req.cancel != nil {
					req.cancel()
				}
				delete(v.queuedSaves, v.retrySavePath)
			} else {
				for _, req := range v.queuedSaves {
					if req != nil && req.cancel != nil {
						req.cancel()
					}
				}
			}
			v.BusyAction = ""
			v.retrySavePath = ""
			v.ActionProgress = ActionProgress{}
			v.ChannelProgress = nil
		} else if vk == key {
			v.ActionProgress = ActionProgress{}
			v.ChannelProgress = nil
		}
	}
	if activeKey, ok := s.activeActionKey[pluginID]; ok && activeKey == key {
		delete(s.activeActionKey, pluginID)
		delete(s.activeActionRT, pluginID)
	}
	s.viewEpoch.Add(1)
	s.mu.Unlock()
	s.notifyChanged()
}

// CancelPendingSaves cancels queued background note.saved sync tasks for pluginID
// in the current notebook without disturbing other notebooks or manual actions.
func (s *Supervisor) CancelPendingSaves(pluginID string) {
	nbRoot := s.NotebookRoot()
	key := viewKey{pluginID: pluginID, notebookRoot: nbRoot, surface: SurfaceSettingsPage}
	s.mu.Lock()
	if v := s.views[key]; v != nil {
		for _, req := range v.queuedSaves {
			if req != nil && req.cancel != nil {
				req.cancel()
			}
		}
		clear(v.queuedSaves)
		if v.BusyAction == "__retry_note_saved__" {
			v.BusyAction = ""
			v.retrySavePath = ""
			v.ActionProgress = ActionProgress{}
			v.ChannelProgress = nil
		}
	}
	s.viewEpoch.Add(1)
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
	actionID := snapshot.FailedActionID
	notePath := snapshot.FailedNotePath
	noteRoot := snapshot.FailedNoteRoot
	if kind == RetryKindNoteSaved {
		if noteRoot == "" {
			noteRoot = nbRoot
		}
		target := s.views[viewKey{pluginID, noteRoot, SurfaceSettingsPage}]
		if target == nil || target.BusyAction != "" {
			s.mu.Unlock()
			return
		}
		failedEvt, hasFailedEvt := target.failedSaveEvents[notePath]
		if !hasFailedEvt {
			if strings.HasPrefix(notePath, "dir:") {
				cleanDir := filepath.Clean(strings.TrimPrefix(notePath, "dir:"))
				relFolder, ok := relativeNotePath(noteRoot, cleanDir)
				if !ok || relFolder == "" || relFolder == "." {
					s.mu.Unlock()
					s.ReloadSurfaceViewAsync(pluginID, surface)
					return
				}
				failedEvt = failedNoteEvent{
					event:     "note.deleted",
					absPath:   cleanDir,
					relFolder: relFolder,
				}
			} else {
				relSlash, ok := relativeNotePath(noteRoot, notePath)
				if !ok || relSlash == "" || relSlash == "." {
					s.mu.Unlock()
					s.ReloadSurfaceViewAsync(pluginID, surface)
					return
				}
				failedEvt = failedNoteEvent{
					event:    "note.saved",
					absPath:  notePath,
					relSlash: relSlash,
				}
			}
		} else if failedEvt.relFolder == "" && failedEvt.relSlash == "" {
			relSlash, ok := relativeNotePath(noteRoot, failedEvt.absPath)
			if !ok || relSlash == "" || relSlash == "." {
				s.mu.Unlock()
				s.ReloadSurfaceViewAsync(pluginID, surface)
				return
			}
			if strings.HasPrefix(notePath, "dir:") {
				failedEvt.relFolder = relSlash
			} else {
				failedEvt.relSlash = relSlash
			}
		}
		target.BusyAction = "__retry_note_saved__"
		target.retrySavePath = notePath
		s.mu.Unlock()
		s.notifyChanged()
		if failedEvt.event == "note.deleted" {
			if failedEvt.relFolder != "" || strings.HasPrefix(notePath, "dir:") {
				cleanDir := failedEvt.absPath
				if cleanDir == "" {
					cleanDir = filepath.Clean(strings.TrimPrefix(notePath, "dir:"))
				}
				s.queueFolderDeleted(pluginID, noteRoot, cleanDir, failedEvt.relFolder)
				return
			}
			absPath := failedEvt.absPath
			if absPath == "" {
				absPath = notePath
			}
			s.queueNoteDeleted(pluginID, noteRoot, absPath, failedEvt.relSlash)
			return
		}
		absPath := failedEvt.absPath
		if absPath == "" {
			absPath = notePath
		}
		s.queueNoteSaved(pluginID, noteRoot, absPath, failedEvt.relSlash)
		return
	}
	s.mu.Unlock()

	switch kind {
	case RetryKindAction:
		if actionID != "" {
			var payload map[string]any
			if snapshot.BackgroundError != nil && snapshot.BackgroundError.RetryAction == actionID {
				payload = cloneAnyMap(snapshot.BackgroundError.RetryPayload)
			}
			s.triggerSurfaceActionAsync(pluginID, surface, actionID, payload)
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

// OpenLogFile opens Block's diagnostic log file in the OS default viewer
// (falling back to the logs folder if the file has not been created yet).
func OpenLogFile() {
	logPath := files.LogFilePath()
	if strings.TrimSpace(logPath) == "" {
		return
	}
	info, err := os.Stat(logPath)
	if err != nil {
		logPath = filepath.Dir(logPath)
		info, err = os.Stat(logPath)
		if err != nil {
			return
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if info.IsDir() {
			cmd = exec.Command("open", "-a", "Finder", logPath)
		} else {
			cmd = exec.Command("open", logPath)
		}
	case "windows":
		cmd = exec.Command("explorer.exe", logPath)
	default:
		cmd = exec.Command("xdg-open", logPath)
	}
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
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
	s.CloseActiveStage()
	s.clearAllThemeSkins()
	if s.wasmCancel != nil {
		s.wasmCancel()
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
	var cancels []context.CancelFunc
	for _, v := range s.views {
		if v == nil {
			continue
		}
		if v.actionCancel != nil {
			cancels = append(cancels, v.actionCancel)
			v.actionCancel = nil
		}
		for _, req := range v.queuedSaves {
			if req != nil && req.cancel != nil {
				cancels = append(cancels, req.cancel)
				req.cancel = nil
			}
		}
	}
	rts := make([]*Runtime, 0, len(s.runtimes))
	for id, rt := range s.runtimes {
		rts = append(rts, rt)
		delete(s.runtimes, id)
	}
	s.mu.Unlock()

	for _, fn := range cancels {
		fn()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
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
	warmupFinished := true
	if s.warmupDone != nil {
		warmupTimer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-s.warmupDone:
			warmupTimer.Stop()
		case <-warmupTimer.C:
			warmupFinished = false
		}
	}
	if warmupFinished && s.wasmEngine != nil {
		_ = s.wasmEngine.Close(context.Background())
	}
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

func cloneChannelProgress(in map[string]ActionProgress) map[string]ActionProgress {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ActionProgress, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Supervisor) selectBackgroundErrorLocked(pluginID, nbRoot string) (BackgroundError, bool) {
	perPlugin := s.bgErrors[pluginID]
	if len(perPlugin) == 0 {
		return BackgroundError{}, false
	}
	if nbRoot != "" {
		if entry, ok := perPlugin[nbRoot]; ok {
			return entry, true
		}
		cleanRoot := cleanNotebookKey(nbRoot)
		for k, entry := range perPlugin {
			if cleanNotebookKey(k) == cleanRoot {
				return entry, true
			}
		}
	}
	if entry, ok := perPlugin["default"]; ok {
		return entry, true
	}
	keys := make([]string, 0, len(perPlugin))
	for k := range perPlugin {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return perPlugin[keys[0]], true
}

func (s *Supervisor) snapshotViewLocked(current viewKey, in *PluginViewState) PluginViewState {
	var iconCopy *UIIcon
	if in.LastActionIcon != nil {
		ic := *in.LastActionIcon
		iconCopy = &ic
	}
	out := PluginViewState{
		Loading:                in.Loading,
		Refreshing:             in.Refreshing,
		BusyAction:             in.BusyAction,
		ActionProgress:         in.ActionProgress,
		ChannelProgress:        cloneChannelProgress(in.ChannelProgress),
		Error:                  in.Error,
		Notice:                 in.Notice,
		Page:                   in.Page,
		Values:                 cloneAnyMap(in.Values),
		LastActionID:           in.LastActionID,
		LastActionTone:         in.LastActionTone,
		LastActionMessage:      in.LastActionMessage,
		LastActionBusyLabel:    in.LastActionBusyLabel,
		LastActionSuccessLabel: in.LastActionSuccessLabel,
		LastActionErrorLabel:   in.LastActionErrorLabel,
		LastActionRetryLabel:   in.LastActionRetryLabel,
		LastActionIcon:         iconCopy,
		LastActionButtonActive: in.LastActionButtonActive,
		LastActionWasHeader:    in.LastActionWasHeader,
		AutosaveTone:           in.AutosaveTone,
		AutosaveMessage:        in.AutosaveMessage,
		RetryKind:              in.RetryKind,
		FailedActionID:         in.FailedActionID,
		FailedNotePath:         in.FailedNotePath,
		FailedNoteRoot:         in.FailedNoteRoot,
	}
	if current.surface == SurfaceSettingsPage {
		// Scope PendingSaves to the current notebook so badge count, menu item, and
		// CancelPendingSaves all act on the exact same scope.
		out.PendingSaves = len(in.queuedSaves)
		if out.BusyAction == "" {
			for key, v := range s.views {
				if key.pluginID == current.pluginID && key.surface == SurfaceSettingsPage && v != nil && v.BusyAction == "__retry_note_saved__" {
					out.BusyAction = v.BusyAction
					out.ActionProgress = v.ActionProgress
					break
				}
			}
		}
		var failedPath, failedRoot, failure string
		var failedPaused bool
		for key, v := range s.views {
			if key.pluginID != current.pluginID || key.surface != SurfaceSettingsPage {
				continue
			}
			for path, message := range v.failedSaves {
				if v.queuedSaves[path] != nil {
					continue
				}
				preferCurrent := key == current && failedRoot != current.notebookRoot
				samePriority := (key == current) == (failedRoot == current.notebookRoot)
				if failedPath == "" || preferCurrent || (samePriority && path < failedPath) {
					failedPath, failedRoot, failure = path, key.notebookRoot, message
					failedPaused = v.savePausedConfirm[path]
				}
			}
		}
		if out.Error == "" && out.BusyAction == "" && out.RetryKind == RetryKindNone && failedPath != "" {
			if failedRoot != current.notebookRoot {
				failure = filepath.Base(failedRoot) + " / " + failure
			}
			out.Error = failure
			out.FailedNotePath = failedPath
			out.FailedNoteRoot = failedRoot
			out.RetryKind = RetryKindNoteSaved
			out.SavePausedConfirm = failedPaused
		}
		if out.Error == "" && out.BusyAction == "" && out.RetryKind == RetryKindNone {
			if bgErr, ok := s.selectBackgroundErrorLocked(current.pluginID, current.notebookRoot); ok {
				cp := bgErr
				cp.RetryPayload = cloneAnyMap(bgErr.RetryPayload)
				out.BackgroundError = &cp
				out.Error = bgErr.Detail
				if out.Error == "" {
					out.Error = bgErr.Label
				}
				if bgErr.RetryAction != "" {
					out.RetryKind = RetryKindAction
					out.FailedActionID = bgErr.RetryAction
				} else {
					out.RetryKind = RetryKindReload
				}
			}
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

func isPluginDisabledError(err error) bool {
	var disabledErr pluginDisabledError
	return errors.As(err, &disabledErr)
}

type pluginReplacingError string

func (e pluginReplacingError) Error() string { return "plugin is stopping or being replaced" }

func errPluginReplacing(id string) error { return pluginReplacingError(id) }

func isPluginReplacingError(err error) bool {
	var replacingErr pluginReplacingError
	return errors.As(err, &replacingErr)
}

func (s *Supervisor) recoverViewsAfterReplaceLocked(pluginID string) []viewKey {
	if pluginID == "" {
		return nil
	}
	activeNB := cleanNotebookKey(s.NotebookRoot())
	var reload []viewKey
	changed := false
	for k, vs := range s.views {
		if k.pluginID != pluginID || vs == nil {
			continue
		}
		if vs.RetryKind == RetryKindUninstall || vs.errorFromBg {
			continue
		}
		if vs.pendingReload || (vs.Page == nil && (vs.Loading || vs.Error != "")) || vs.Error == errPluginReplacing(pluginID).Error() {
			vs.Loading = false
			vs.pendingReload = false
			vs.Error = ""
			vs.RetryKind = RetryKindNone
			vs.initialLoadStarted = false
			vs.cachedSnapValid = false
			changed = true
			if cleanNotebookKey(k.notebookRoot) == activeNB {
				reload = append(reload, k)
			}
		}
	}
	if changed {
		s.viewEpoch.Add(1)
	}
	return reload
}

func (s *Supervisor) clearNotebookSaveErrorsLocked(pluginID string, roots []string) {
	if len(roots) == 0 {
		return
	}
	clearedRoots := make(map[string]bool, len(roots))
	for _, root := range roots {
		if key := cleanNotebookKey(root); key != "" {
			clearedRoots[key] = true
		}
	}
	for key, view := range s.views {
		if key.pluginID == pluginID && key.surface == SurfaceSettingsPage && view != nil && clearedRoots[cleanNotebookKey(key.notebookRoot)] {
			clear(view.failedSaves)
			clear(view.failedSaveEvents)
			clear(view.savePausedConfirm)
		}
	}
}
