package plugins

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PluginViewState holds the non-blocking UI snapshot for an installed plugin's Settings page.
type PluginViewState struct {
	Loading    bool
	BusyAction string
	Error      string
	Notice     string
	Page       *UIPage
	Values     map[string]any
}

type SupervisorConfig struct {
	AppDataDir      string
	HostVersion     string
	GetNotebookRoot func() string
	GetLocale       func() string
	OnChanged       func()
}

// Supervisor coordinates StoreManager, lazy plugin Runtime subprocesses, async form actions,
// and non-blocking note-saved event dispatch without ever stalling the 60fps UI render thread.
type Supervisor struct {
	store *StoreManager
	cfg   SupervisorConfig

	mu             sync.RWMutex
	catalog        *StoreIndex
	installed      []InstalledApp
	runtimes       map[string]*Runtime
	views          map[string]*PluginViewState
	storeRefreshing bool
	installingID   string
	storeErr       string
	storeNotice    string
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
		views:     make(map[string]*PluginViewState),
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		delete(s.views, inst.Manifest.ID)
		s.mu.Unlock()

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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		delete(s.views, inst.Manifest.ID)
		s.mu.Unlock()

		if onInstalled != nil {
			onInstalled(inst.Manifest.ID)
		}
		s.notifyChanged()
	}()
}

func (s *Supervisor) UninstallAsync(pluginID string, onDone func()) {
	go func() {
		s.mu.Lock()
		rt := s.runtimes[pluginID]
		delete(s.runtimes, pluginID)
		delete(s.views, pluginID)
		s.mu.Unlock()

		if rt != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = rt.Shutdown(ctx)
			cancel()
		}
		_ = s.store.Uninstall(pluginID, false)
		installedList, _ := s.store.ListInstalled()

		s.mu.Lock()
		s.installed = installedList
		s.mu.Unlock()

		if onDone != nil {
			onDone()
		}
		s.notifyChanged()
	}()
}

// EnsurePluginView returns the current non-blocking UI snapshot and lazily starts/renders the plugin if needed.
func (s *Supervisor) EnsurePluginView(pluginID string) PluginViewState {
	s.mu.Lock()
	vs, ok := s.views[pluginID]
	if ok {
		snap := cloneViewState(vs)
		s.mu.Unlock()
		return snap
	}
	vs = &PluginViewState{Loading: true, Values: map[string]any{}}
	s.views[pluginID] = vs
	snap := cloneViewState(vs)
	s.mu.Unlock()

	go s.loadAndRenderPlugin(pluginID)
	return snap
}

func (s *Supervisor) ReloadPluginViewAsync(pluginID string) {
	s.mu.Lock()
	vs, ok := s.views[pluginID]
	if !ok {
		vs = &PluginViewState{Values: map[string]any{}}
		s.views[pluginID] = vs
	}
	if vs.Loading {
		s.mu.Unlock()
		return
	}
	vs.Loading = true
	vs.Error = ""
	s.mu.Unlock()
	s.notifyChanged()

	go s.loadAndRenderPlugin(pluginID)
}

func (s *Supervisor) loadAndRenderPlugin(pluginID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rt, err := s.ensureRuntime(ctx, pluginID)
	if err != nil {
		s.mu.Lock()
		if vs := s.views[pluginID]; vs != nil {
			vs.Loading = false
			vs.Error = err.Error()
		}
		s.mu.Unlock()
		s.notifyChanged()
		return
	}

	rt.SetNotebookRoot(s.NotebookRoot())
	page, err := rt.Render(ctx, SurfaceSettingsPage)
	s.mu.Lock()
	if vs := s.views[pluginID]; vs != nil {
		vs.Loading = false
		if err != nil {
			vs.Error = err.Error()
		} else {
			vs.Error = ""
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

func (s *Supervisor) ensureRuntime(ctx context.Context, pluginID string) (*Runtime, error) {
	s.mu.RLock()
	existing := s.runtimes[pluginID]
	s.mu.RUnlock()
	if existing != nil && !existing.closed.Load() {
		existing.SetNotebookRoot(s.NotebookRoot())
		return existing, nil
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
		NotebookRoot: s.NotebookRoot(),
		Locale:       s.Locale(),
		HostVersion:  hostVer,
	})
	if err != nil {
		return nil, err
	}
	if err := rt.Initialize(ctx); err != nil {
		_ = rt.Shutdown(context.Background())
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
	s.mu.Lock()
	if vs := s.views[pluginID]; vs != nil {
		if vs.Values == nil {
			vs.Values = map[string]any{}
		}
		vs.Values[controlID] = val
	}
	s.mu.Unlock()
}

func (s *Supervisor) TriggerActionAsync(pluginID, actionID string) {
	s.mu.Lock()
	vs := s.views[pluginID]
	if vs == nil || vs.BusyAction != "" {
		s.mu.Unlock()
		return
	}
	vs.BusyAction = actionID
	vs.Error = ""
	valsCopy := make(map[string]any, len(vs.Values))
	for k, v := range vs.Values {
		valsCopy[k] = v
	}
	s.mu.Unlock()
	s.notifyChanged()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		rt, err := s.ensureRuntime(ctx, pluginID)
		var res *ActionResult
		if err == nil {
			rt.SetNotebookRoot(s.NotebookRoot())
			res, err = rt.Action(ctx, SurfaceSettingsPage, actionID, valsCopy)
		}

		s.mu.Lock()
		if cur := s.views[pluginID]; cur != nil {
			cur.BusyAction = ""
			if err != nil {
				cur.Error = err.Error()
			} else if res != nil {
				cur.Error = ""
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
			}
		}
		s.mu.Unlock()
		s.notifyChanged()
	}()
}

// NotifyNoteSaved dispatches note.saved asynchronously to every installed plugin declaring events:note_saved.
func (s *Supervisor) NotifyNoteSaved(absNotePath string) {
	nbRoot := s.NotebookRoot()
	if nbRoot == "" || absNotePath == "" {
		return
	}
	rootClean, err := filepath.Abs(nbRoot)
	if err != nil {
		return
	}
	noteClean, err := filepath.Abs(absNotePath)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(rootClean, noteClean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return
	}

	installed := s.Installed()
	for _, app := range installed {
		if !app.Enabled || !app.Manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		pluginID := app.Manifest.ID
		relSlash := filepath.ToSlash(rel)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			rt, err := s.ensureRuntime(ctx, pluginID)
			if err != nil {
				return
			}
			rt.SetNotebookRoot(nbRoot)
			if err := rt.NotifyNoteSaved(ctx, relSlash); err != nil {
				return
			}
			// Refresh cached view if already open so recent sync logs update live.
			s.mu.RLock()
			hasView := s.views[pluginID] != nil
			s.mu.RUnlock()
			if hasView {
				if page, err := rt.Render(ctx, SurfaceSettingsPage); err == nil {
					s.mu.Lock()
					if cur := s.views[pluginID]; cur != nil {
						cur.Page = page
					}
					s.mu.Unlock()
					s.notifyChanged()
				}
			}
		}()
	}
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
		Loading:    in.Loading,
		BusyAction: in.BusyAction,
		Error:      in.Error,
		Notice:     in.Notice,
		Page:       in.Page,
		Values:     make(map[string]any, len(in.Values)),
	}
	for k, v := range in.Values {
		out.Values[k] = v
	}
	return out
}

type notInstalledError string

func (e notInstalledError) Error() string { return "plugin not installed: " + string(e) }

func osErrNotInstalled(id string) error { return notInstalledError(id) }
