package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"block/internal/editor"
	"block/internal/files"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo"
	codeed "github.com/egoist/mygo/ui/editor"
)

const externalFocusCooldown = 350 * time.Millisecond

// externalSyncState keeps scheduling and watcher bookkeeping separate from the
// workspace lock. The worker prepares disk changes before applying them on the UI thread.
type externalSyncState struct {
	mu        sync.Mutex
	stopped   bool
	stop      chan struct{}
	wake      chan struct{}
	running   bool
	pending   map[string]time.Time
	lastFocus time.Time

	watchFolder   string
	watchCancel   context.CancelFunc
	watchFailures int
	watchRetryAt  time.Time

	lifecycleOnce sync.Once
	blurTimer     *time.Timer
	focusEpoch    uint64
	appFocused    bool

	trackerMu   sync.Mutex
	trackedPath string // canonical path at bind time, accounting for symlink aliases

	maintStop chan struct{}
}

type diskMarkdownSnapshot struct {
	diskText   string
	editorText string
}

func (w *Workspace) ScheduleExternalReconcile(reason string, delay time.Duration) {
	s := &w.externalSync
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	when := time.Now().Add(delay)
	if reason == "focus" {
		when = timeAfter(when, s.lastFocus.Add(externalFocusCooldown))
	}
	if s.pending == nil {
		s.pending = make(map[string]time.Time)
	}
	if prior, ok := s.pending[reason]; !ok || when.Before(prior) {
		s.pending[reason] = when
	}
	w.startExternalWorkerLocked()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func timeAfter(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (w *Workspace) ReconcileExternalChanges(reason string) {
	w.ScheduleExternalReconcile(reason, 0)
}

func (w *Workspace) startExternalWorkerLocked() {
	s := &w.externalSync
	if s.running || s.stopped || len(s.pending) == 0 {
		return
	}
	if s.stop == nil {
		s.stop = make(chan struct{})
		s.wake = make(chan struct{}, 1)
	}
	s.running = true
	go w.runExternalReconciler(s.stop, s.wake)
}

func (w *Workspace) runExternalReconciler(stop, wake <-chan struct{}) {
	s := &w.externalSync
	defer func() {
		s.mu.Lock()
		s.running = false
		w.startExternalWorkerLocked()
		s.mu.Unlock()
	}()
	for {
		s.mu.Lock()
		if s.stop != stop || s.stopped || len(s.pending) == 0 {
			s.mu.Unlock()
			return
		}
		now := time.Now()
		var next time.Time
		var reasons []string
		for reason, when := range s.pending {
			if !when.After(now) {
				delete(s.pending, reason)
				reasons = append(reasons, reason)
				if reason == "focus" {
					s.lastFocus = now
				}
			} else if next.IsZero() || when.Before(next) {
				next = when
			}
		}
		s.mu.Unlock()
		if len(reasons) != 0 {
			// A save may have extended the suppression window after the watcher
			// queued its initial deadline. Keep the observation pending.
			if i := slices.Index(reasons, "watcher"); i >= 0 {
				w.mu.RLock()
				until := w.selfWriteUntil
				w.mu.RUnlock()
				if now.Before(until) {
					w.ScheduleExternalReconcile("watcher", time.Until(until)+60*time.Millisecond)
					reasons = slices.Delete(reasons, i, i+1)
				}
			}
			if len(reasons) != 0 && !externalSyncStopped(stop) {
				w.reconcileExternalOnce(stop, len(reasons) == 1 && reasons[0] == "tab-switch")
			}
			continue
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-stop:
			timer.Stop()
			return
		case <-wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func externalSyncStopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

func (w *Workspace) StopExternalSync() {
	s := &w.externalSync
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.stop != nil {
		close(s.stop)
	}
	clear(s.pending)
	s.cancelWatcherLocked()
	s.watchFolder = ""
	s.watchFailures = 0
	if s.blurTimer != nil {
		s.blurTimer.Stop()
		s.blurTimer = nil
	}
	if s.maintStop != nil {
		close(s.maintStop)
		s.maintStop = nil
	}
	s.focusEpoch++
	s.mu.Unlock()

	s.trackerMu.Lock()
	tracker := w.rootTracker
	w.rootTracker, s.trackedPath = nil, ""
	s.trackerMu.Unlock()
	if tracker != nil {
		_ = tracker.Close()
	}
}

// ResumeExternalSync restores the resources stopped by a canceled quit.
func (w *Workspace) ResumeExternalSync() {
	s := &w.externalSync
	s.mu.Lock()
	wasStopped := s.stopped
	if wasStopped {
		s.stopped = false
		s.stop, s.wake = nil, nil
	}
	s.mu.Unlock()
	if wasStopped {
		w.StartNotebookMaintenance()
		w.ReconcileExternalChanges("startup")
	}
}

const notebookMaintenanceInterval = time.Hour

// StartNotebookMaintenance begins background cleanup of expired replaced stash
// entries (7 days / 64 max) and disappeared notebooks (>30 days missing).
func (w *Workspace) StartNotebookMaintenance() {
	s := &w.externalSync
	s.mu.Lock()
	if s.stopped || s.maintStop != nil {
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.maintStop = stop
	s.mu.Unlock()
	go w.runNotebookMaintenance(stop)
}

func (w *Workspace) runNotebookMaintenance(stop <-chan struct{}) {
	w.runNotebookMaintenancePass(true)
	ticker := time.NewTicker(notebookMaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.runNotebookMaintenancePass(false)
		}
	}
}

func (w *Workspace) runNotebookMaintenancePass(migrateAll bool) {
	if migrateAll {
		seen := make(map[string]bool)
		for _, nb := range w.Notebooks() {
			if nb == "" || seen[nb] {
				continue
			}
			seen[nb] = true
			files.MigrateLegacyReplacedDirIn(w.dataDir, nb)
		}
		if active := w.ActiveNotebook(); active != "" && !seen[active] {
			files.MigrateLegacyReplacedDirIn(w.dataDir, active)
		}
	}
	files.CleanupReplacedDirsIn(w.dataDir)
	if expired, err := files.SweepNotebookRegistryIn(w.dataDir); err == nil && len(expired) > 0 {
		if sup := w.Plugins(); sup != nil {
			for _, rem := range expired {
				sup.EmitNotebookRemovedEvent(rem.ID, rem.Path)
			}
		}
	}
}

func (s *externalSyncState) cancelWatcherLocked() {
	if s.watchCancel != nil {
		s.watchCancel()
		s.watchCancel = nil
	}
	s.watchRetryAt = time.Time{}
}

func (w *Workspace) watcherFailed(ctx context.Context, folder string, err error, uptime time.Duration) {
	s := &w.externalSync
	s.mu.Lock()
	if ctx.Err() != nil || s.stopped {
		s.mu.Unlock()
		return
	}
	s.cancelWatcherLocked()
	if uptime >= 5*time.Second {
		s.watchFailures = 0
	}
	s.watchFailures++
	// Keep the first recovery prompt, then back off if opening or staying
	// attached continues to fail.
	backoff := 80 * time.Millisecond << min(s.watchFailures-1, 6)
	s.watchRetryAt = time.Now().Add(backoff)
	s.mu.Unlock()
	files.LogWarnf("file watcher stopped: folder=%s err=%v", folder, err)
	w.ScheduleExternalReconcile("watcher-eof", 80*time.Millisecond)
}

func (w *Workspace) ensureNotebookWatcher(folder string) {
	if !mygo.App.IsReady() || folder == "" {
		return
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if folder != w.state.Folder {
		return
	}
	s := &w.externalSync
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if s.watchFolder != folder {
		s.cancelWatcherLocked()
		s.watchFolder = folder
		s.watchFailures = 0
	}
	if s.watchCancel != nil {
		s.mu.Unlock()
		return
	}
	if delay := time.Until(s.watchRetryAt); delay > 0 {
		s.mu.Unlock()
		w.ScheduleExternalReconcile("watcher-retry", delay)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.watchCancel = cancel
	s.mu.Unlock()
	go w.watchNotebook(ctx, folder)
}

func (w *Workspace) attachAppLifecycleHooks() {
	s := &w.externalSync
	s.lifecycleOnce.Do(func() {
		onFocus := func() {
			w.ReconcileExternalChanges("focus")
			if sup := w.Plugins(); sup != nil && !s.isStopped() {
				sup.EmitSystemEvent("app.focus", false)
			}
		}
		mygo.Power.OnResume(func() {
			w.ScheduleExternalReconcile("wake", time.Second)
			if sup := w.Plugins(); sup != nil && !s.isStopped() {
				sup.EmitSystemEvent("system.wake", mygo.Network.Status().Online)
			}
		})
		if runtime.GOOS == "darwin" {
			mygo.App.OnDidBecomeActive(onFocus)
			return
		}
		updateFocus := func() {
			focused := false
			for _, win := range mygo.Windows() {
				if win.IsFocused() {
					focused = true
					break
				}
			}
			s.mu.Lock()
			regained := focused && !s.appFocused && !s.stopped
			s.appFocused = focused
			s.mu.Unlock()
			if regained {
				onFocus()
			}
		}
		watchWindow := func(win *mygo.Window) {
			win.OnFocus(updateFocus)
			lostFocus := func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.stopped {
					return
				}
				if s.blurTimer != nil {
					s.blurTimer.Stop()
				}
				s.focusEpoch++
				epoch := s.focusEpoch
				s.blurTimer = time.AfterFunc(75*time.Millisecond, func() {
					mygo.RunOnMain(func() {
						s.mu.Lock()
						current := !s.stopped && epoch == s.focusEpoch
						if current {
							s.blurTimer = nil
						}
						s.mu.Unlock()
						if current {
							updateFocus()
						}
					})
				})
			}
			win.OnBlur(lostFocus)
			win.OnClosed(lostFocus)
		}
		mygo.App.OnWindowCreated(watchWindow)
		for _, win := range mygo.Windows() {
			watchWindow(win)
		}
		updateFocus()
	})
}

func (s *externalSyncState) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// onExternalMain also makes scheduled work harmless if a quit happens while
// the worker is waiting for the main thread. It never holds a save lock while dispatching.
func (w *Workspace) onExternalMain(stop <-chan struct{}, apply func()) {
	if externalSyncStopped(stop) {
		return
	}
	run := func() {
		if !externalSyncStopped(stop) {
			apply()
		}
	}
	if mygo.App.IsReady() {
		mygo.RunOnMain(run)
	} else {
		w.headlessMainMu.Lock()
		defer w.headlessMainMu.Unlock()
		run()
	}
}

// bindNotebookTracker is called at startup and under bgSaveMu after a notebook
// switch or rebase. The existing handle stays valid while a replacement is opened.
func (w *Workspace) bindNotebookTracker(folder string) {
	s := &w.externalSync
	s.trackerMu.Lock()
	previous := w.rootTracker
	unchanged := previous != nil && previous.InitialPath() == folder
	s.trackerMu.Unlock()
	if unchanged || s.isStopped() {
		return
	}
	var tracker *files.DirTracker
	var canonical string
	if folder != "" {
		tracker, _ = files.OpenDirTracker(folder)
		if tracker != nil {
			canonical, _ = tracker.CurrentPath()
		}
	}
	s.trackerMu.Lock()
	if s.isStopped() {
		s.trackerMu.Unlock()
		if tracker != nil {
			_ = tracker.Close()
		}
		return
	}
	w.rootTracker, s.trackedPath = tracker, canonical
	s.trackerMu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	if tracker == nil {
		return
	}
	locator := tracker.Locator()
	w.mu.RLock()
	known := w.state.NotebookIdentities[folder]
	w.mu.RUnlock()
	if locator == "" || locator == known {
		return
	}
	next, err := w.updateStateSync(func(st *model.AppState) {
		if filepath.Clean(st.Folder) == folder {
			if st.NotebookIdentities == nil {
				st.NotebookIdentities = make(map[string]string)
			}
			st.NotebookIdentities[folder] = locator
		}
	})
	if err != nil {
		files.LogWarnf("notebook identity save failed: folder=%s err=%v", folder, err)
		return
	}
	w.mu.Lock()
	if w.state.Folder == folder {
		w.state.NotebookIdentities = next.NotebookIdentities
	}
	w.mu.Unlock()
}

func (w *Workspace) reconcileNotebookRoot(folder string) (newRoot string, available bool) {
	if folder == "" {
		return "", false
	}
	clean := filepath.Clean(folder)
	s := &w.externalSync
	s.trackerMu.Lock()
	tracker, trackedPath := w.rootTracker, s.trackedPath
	s.trackerMu.Unlock()
	w.mu.RLock()
	locator := w.state.NotebookIdentities[clean]
	w.mu.RUnlock()
	trackerLost := false
	if tracker != nil && tracker.InitialPath() == clean {
		if locator == "" {
			locator = tracker.Locator()
		}
		live, ok := tracker.CurrentPath()
		trackerLost = !ok
		if ok {
			live = filepath.Clean(live)
			if live != clean && live != trackedPath && !files.IsTrashPath(live) {
				if info, err := os.Stat(live); err == nil && info.IsDir() {
					return live, true
				}
			}
		}
	}
	info, err := os.Stat(clean)
	if err == nil && info.IsDir() {
		if trackerLost {
			// The original handle can outlive a directory replaced at the same path.
			s.trackerMu.Lock()
			if w.rootTracker == tracker {
				w.rootTracker, s.trackedPath = nil, ""
			}
			s.trackerMu.Unlock()
			_ = tracker.Close()
		}
		return "", true
	}
	if locator != "" && (err == nil || errors.Is(err, os.ErrNotExist)) {
		if live, ok := files.ResolveDirLocator(clean, locator); ok {
			live = filepath.Clean(live)
			if live != clean && !files.IsTrashPath(live) {
				if info, err := os.Stat(live); err == nil && info.IsDir() {
					return live, true
				}
			}
		}
	}
	return "", false
}

func (w *Workspace) rebaseNotebookRoot(oldRoot, newRoot string) bool {
	oldRoot = filepath.Clean(oldRoot)
	if info, err := os.Stat(newRoot); err != nil || !info.IsDir() {
		return false
	}
	locator := files.CaptureDirLocator(newRoot)
	var rebased bool
	next, err := w.updateStateSync(func(st *model.AppState) {
		if st.Folder == "" || filepath.Clean(st.Folder) != oldRoot {
			return
		}
		rebased = true
		st.Folder = newRoot
		for i, root := range st.Notebooks {
			if filepath.Clean(root) == oldRoot {
				st.Notebooks[i] = newRoot
			}
		}
		// UpdateState normalizes the notebook list and deduplicates the new root.
		if st.NotebookIdentities == nil {
			st.NotebookIdentities = make(map[string]string)
		}
		if locator == "" {
			locator = st.NotebookIdentities[oldRoot]
		}
		delete(st.NotebookIdentities, oldRoot)
		if locator != "" {
			st.NotebookIdentities[newRoot] = locator
		}
		st.OpenTabs = rebasePathList(st.OpenTabs, oldRoot, newRoot)
		st.ActiveTab, _ = rebasePath(st.ActiveTab, oldRoot, newRoot)
		st.TabBindings = rebasePathMap(st.TabBindings, oldRoot, newRoot)
		for path, binding := range st.TabBindings {
			if binding.ConfineRoot == "" {
				continue
			}
			binding.ConfineRoot, _ = rebasePath(binding.ConfineRoot, oldRoot, newRoot)
			if rel, err := filepath.Rel(binding.ConfineRoot, path); err == nil && filepath.IsLocal(rel) {
				binding.ConfineRel = rel
			} else {
				binding.ConfineRoot, binding.ConfineRel, binding.RootIdentity = "", "", ""
			}
			st.TabBindings[path] = binding
		}
	})
	if err != nil {
		files.LogWarnf("notebook move save failed: %s -> %s: %v", oldRoot, newRoot, err)
		return false
	}
	if !rebased {
		return false
	}
	renamedRec, _, _ := files.RenameNotebookIn(w.dataDir, oldRoot, newRoot)
	files.RebaseFileOrder(oldRoot, newRoot)
	order := files.LoadState().FileOrder
	w.mu.Lock()
	w.state.Folder = next.Folder
	w.state.Notebooks = next.Notebooks
	w.state.NotebookIdentities = next.NotebookIdentities
	w.state.FileOrder = order
	w.state.TabBindings = next.TabBindings
	w.entries = rebaseEntryTree(w.entries, oldRoot, newRoot)
	w.entriesRevision++
	w.mu.Unlock()
	w.rebaseWorkspacePaths(oldRoot, newRoot, false)
	w.bindNotebookTracker(newRoot)
	if sup := w.Plugins(); sup != nil {
		sup.RenameNotebookRoot(oldRoot, newRoot)
		if renamedRec.ID != "" {
			sup.EmitNotebookRenamedEvent(renamedRec.ID, oldRoot, newRoot)
		}
	}
	w.sidebarUI.notebookAvail().Forget(oldRoot)
	w.checkNotebookAvailability()
	return true
}

func (w *Workspace) checkNotebookAvailability() {
	roots := w.Notebooks()
	if folder := w.ActiveNotebook(); folder != "" && !slices.Contains(roots, folder) {
		roots = append(roots, folder)
	}
	w.sidebarUI.notebookAvail().Check(roots, w.RequestRedraw)
}

func (w *Workspace) notifyNotebookMoved(newRoot string) {
	w.notify(fmt.Sprintf(theme.Tr("文件夹已移动，已自动同步至 %s", "Folder moved, synced to %s"), filepath.Base(newRoot)), "info")
}

func rebasePath(path, from, to string) (string, bool) {
	if path == "" {
		return path, false
	}
	rel, err := filepath.Rel(from, path)
	if err != nil || !filepath.IsLocal(rel) {
		return path, false
	}
	return filepath.Join(to, rel), true
}

func rebasePathList(paths []string, from, to string) []string {
	if len(paths) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		path, _ = rebasePath(path, from, to)
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func rebasePathMap[V any](paths map[string]V, from, to string) map[string]V {
	if paths == nil {
		return nil
	}
	out := make(map[string]V, len(paths))
	for path, value := range paths {
		if _, moved := rebasePath(path, from, to); !moved {
			out[path] = value
		}
	}
	for path, value := range paths {
		if next, moved := rebasePath(path, from, to); moved {
			out[next] = value
		}
	}
	return out
}

func rebaseEntryTree(entries []model.FileEntry, from, to string) []model.FileEntry {
	out := slices.Clone(entries)
	for i := range out {
		out[i].Path, _ = rebasePath(out[i].Path, from, to)
		out[i].Children = rebaseEntryTree(out[i].Children, from, to)
	}
	return out
}

func (w *Workspace) reconcileExternalOnce(stop <-chan struct{}, activeOnly bool) {
	folder := w.ActiveNotebook()
	newRoot, available := w.reconcileNotebookRoot(folder)
	changed := false
	if newRoot != "" {
		w.onExternalMain(stop, func() {
			if !w.bgSaveMu.TryLock() {
				w.ScheduleExternalReconcile("save-in-flight", 120*time.Millisecond)
				return
			}
			defer w.bgSaveMu.Unlock()
			if w.ActiveNotebook() != folder || w.externalMoveBlocked(pathMove{from: folder, to: newRoot}) {
				return
			}
			changed = w.rebaseNotebookRoot(folder, newRoot)
		})
		if !changed {
			return
		}
		folder, activeOnly = newRoot, false
		w.notifyNotebookMoved(newRoot)
	} else if folder != "" && !available {
		if a := w.sidebarUI.notebookAvail(); !a.Checking() && !a.Missing(folder) {
			w.checkNotebookAvailability()
		}
	} else if folder != "" {
		if w.sidebarUI.notebookAvail().Missing(folder) {
			w.checkNotebookAvailability()
		}
		s := &w.externalSync
		s.trackerMu.Lock()
		bound := w.rootTracker != nil && w.rootTracker.InitialPath() == filepath.Clean(folder)
		s.trackerMu.Unlock()
		if !bound && w.bgSaveMu.TryLock() {
			if w.ActiveNotebook() == folder && !externalSyncStopped(stop) {
				w.bindNotebookTracker(folder)
			}
			w.bgSaveMu.Unlock()
		}
	}
	if available && !externalSyncStopped(stop) {
		w.ensureNotebookWatcher(folder)
		if !activeOnly && w.reconcileExternalTree(stop, folder) {
			changed = true
		}
	}
	if changed {
		w.RequestRedraw()
	}
	w.reconcileOpenSessions(stop, folder, activeOnly)
}

type pathMove struct {
	from string
	to   string
}

// externalMoveBlocked preserves the two editor sessions when a destination is
// already occupied, and lets plugin insert/save finish using its original key.
func (w *Workspace) externalMoveBlocked(move pathMove) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for path, count := range w.pluginInsertInFlight {
		if count > 0 && (isSameOrDescendant(path, move.from) || isSameOrDescendant(path, move.to)) {
			w.ScheduleExternalReconcile("plugin-in-flight", 250*time.Millisecond)
			return true
		}
	}
	for path := range w.sessions {
		if to, moved := rebasePath(path, move.from, move.to); moved && (w.sessions[to] != nil || w.surfaces[to] != nil) {
			return true
		}
	}
	for path := range w.surfaces {
		if to, moved := rebasePath(path, move.from, move.to); moved && (w.sessions[to] != nil || w.surfaces[to] != nil) {
			return true
		}
	}
	return false
}

func (w *Workspace) reconcileExternalTree(stop <-chan struct{}, folder string) bool {
	w.mu.RLock()
	if w.state.Folder != folder {
		w.mu.RUnlock()
		return false
	}
	oldEntries, revision := w.entries, w.entriesRevision
	opts := w.scanOptionsLocked()
	openFileIDs := make(map[string]string, len(w.tabBindings))
	for _, path := range w.state.OpenTabs {
		if id := w.tabBindings[filepath.Clean(path)].FileIdentity; id != "" {
			openFileIDs[filepath.Clean(path)] = id
		}
	}
	w.mu.RUnlock()

	list, err := files.ScanNotebook(folder, opts)
	if err != nil {
		w.mu.Lock()
		changed := w.state.Folder == folder && w.entriesRevision == revision && w.lastScanErr != err.Error()
		if changed {
			w.lastScanErr = err.Error()
		}
		w.mu.Unlock()
		if changed {
			files.LogWarnf("external notebook scan failed: folder=%s err=%v", folder, err)
		}
		return changed
	}
	captureWindowsDirIdentities(list, oldEntries)
	moves := reconcileInternalMoves(folder, oldEntries, list, openFileIDs)
	if len(moves) == 0 {
		return w.publishExternalTree(stop, folder, revision, opts, list)
	}

	var sortAgain bool
	var changed bool
	w.onExternalMain(stop, func() {
		if !w.bgSaveMu.TryLock() {
			w.ScheduleExternalReconcile("save-in-flight", 120*time.Millisecond)
			return
		}
		defer w.bgSaveMu.Unlock()
		w.mu.RLock()
		valid := w.state.Folder == folder && w.entriesRevision == revision && scanOptionsEqual(opts, w.scanOptionsLocked())
		w.mu.RUnlock()
		if !valid {
			w.ScheduleExternalReconcile("tree-changed", 40*time.Millisecond)
			return
		}
		for _, move := range moves {
			if w.externalMoveBlocked(move) {
				return // retain the old identities for the next pass
			}
		}
		for _, move := range moves {
			files.RebaseFileOrder(move.from, move.to)
			w.rebaseRenamedPaths(move.from, move.to)
		}
		order := files.LoadState().FileOrder
		w.mu.Lock()
		w.state.FileOrder = order
		nextOpts := w.scanOptionsLocked()
		sortAgain = !scanOptionsEqual(nextOpts, opts)
		opts = nextOpts
		changed = !fileTreeEqual(w.entries, list)
		w.entries, w.lastScanErr = list, ""
		w.entriesRevision++
		revision = w.entriesRevision
		w.mu.Unlock()
	})
	if sortAgain && !externalSyncStopped(stop) {
		if sorted, err := files.ScanNotebook(folder, opts); err == nil {
			captureWindowsDirIdentities(sorted, list)
			changed = w.publishExternalTree(stop, folder, revision, opts, sorted) || changed
		}
	}
	return changed
}

func (w *Workspace) publishExternalTree(stop <-chan struct{}, folder string, revision uint64, opts files.ScanOptions, list []model.FileEntry) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if externalSyncStopped(stop) || w.state.Folder != folder {
		return false
	}
	if w.entriesRevision != revision || !scanOptionsEqual(opts, w.scanOptionsLocked()) {
		w.ScheduleExternalReconcile("tree-changed", 40*time.Millisecond)
		return false
	}
	changed := !fileTreeEqual(w.entries, list)
	w.entries, w.lastScanErr = list, ""
	w.entriesRevision++
	return changed
}

// Windows needs an open handle for directory identity. Capture identities on
// discovery so directory moves can be recognized after the old name is gone.
func captureWindowsDirIdentities(entries, previous []model.FileEntry) {
	if runtime.GOOS != "windows" {
		return
	}
	known := make(map[string]string)
	var remember func([]model.FileEntry)
	remember = func(items []model.FileEntry) {
		for _, item := range items {
			if item.IsDir {
				known[item.Path] = item.Identity
				remember(item.Children)
			}
		}
	}
	remember(previous)
	var fill func([]model.FileEntry)
	fill = func(items []model.FileEntry) {
		for i := range items {
			item := &items[i]
			if !item.IsDir {
				continue
			}
			if item.Identity == "" {
				item.Identity = known[item.Path]
				if item.Identity == "" {
					item.Identity = files.PathIdentity(item.Path)
				}
			}
			fill(item.Children)
		}
	}
	fill(entries)
}

func reconcileInternalMoves(folder string, oldEntries, newEntries []model.FileEntry, openFileIDs map[string]string) []pathMove {
	oldPaths := make(map[string]bool)
	oldFiles := make(map[string]string)
	var oldDirs []model.FileEntry
	var collectOld func([]model.FileEntry)
	collectOld = func(items []model.FileEntry) {
		for _, item := range items {
			oldPaths[item.Path] = true
			if item.IsDir {
				oldDirs = append(oldDirs, item)
				collectOld(item.Children)
			} else {
				oldFiles[item.Path] = item.Identity
			}
		}
	}
	collectOld(oldEntries)
	for path, id := range openFileIDs {
		if _, inside := rebasePath(path, folder, folder); inside && oldFiles[path] == "" {
			oldFiles[path] = id
		}
	}
	newPaths := make(map[string]bool)
	newDirByID, newFileByID := make(map[string]string), make(map[string]string)
	var newFileCandidates []string
	addID := func(byID map[string]string, id, path string) {
		if id == "" {
			return
		}
		if _, duplicate := byID[id]; duplicate {
			byID[id] = "" // multiple hard links cannot identify a unique move
		} else {
			byID[id] = path
		}
	}
	var collectNew func([]model.FileEntry)
	collectNew = func(items []model.FileEntry) {
		for _, item := range items {
			newPaths[item.Path] = true
			if item.IsDir {
				addID(newDirByID, item.Identity, item.Path)
				collectNew(item.Children)
			} else {
				addID(newFileByID, item.Identity, item.Path)
				if runtime.GOOS == "windows" && item.Identity == "" && !oldPaths[item.Path] {
					newFileCandidates = append(newFileCandidates, item.Path)
				}
			}
		}
	}
	collectNew(newEntries)

	var moves []pathMove
	dirMoved := make(map[string]bool)
	covered := func(path string) bool {
		for dir := filepath.Dir(path); pathWithinRoot(dir, folder); dir = filepath.Dir(dir) {
			if dirMoved[dir] {
				return true
			}
			if dir == folder || filepath.Dir(dir) == dir {
				break
			}
		}
		return false
	}
	match := func(from, to string) bool {
		if to == "" || newPaths[from] || oldPaths[to] || files.IsTrashPath(to) || covered(from) {
			return false
		}
		// A filtered entry can disappear from the tree while remaining in place.
		// A case-only rename is observable in directory entries on every platform.
		if !strings.EqualFold(from, to) {
			if _, err := os.Lstat(from); !errors.Is(err, os.ErrNotExist) {
				return false
			}
		}
		moves = append(moves, pathMove{from: from, to: to})
		return true
	}
	// Scan order visits every directory before its descendants.
	for _, dir := range oldDirs {
		if match(dir.Path, newDirByID[dir.Identity]) {
			dirMoved[dir.Path] = true
		}
	}
	sourceCount := make(map[string]int)
	for path, id := range oldFiles {
		if id != "" && !newPaths[path] && !covered(path) {
			sourceCount[id]++
		}
	}
	if len(sourceCount) > 0 {
		for _, path := range newFileCandidates {
			addID(newFileByID, files.PathIdentity(path), path)
		}
	}
	for path, id := range oldFiles {
		if sourceCount[id] == 1 {
			match(path, newFileByID[id])
		}
	}
	return moves
}

type externalNoteRead struct {
	path     string
	sess     *editor.Session
	seq      uint64
	original int64
	diskFP   int64
	diskText string
	doc      *model.Doc
	fileInfo os.FileInfo
	fileID   string
}

func (w *Workspace) reconcileOpenSessions(stop <-chan struct{}, folder string, activeOnly bool) {
	w.mu.RLock()
	if w.state.Folder != folder {
		w.mu.RUnlock()
		return
	}
	var notes []externalNoteRead
	active := w.state.ActiveTab
	add := func(path string) {
		if sess := w.sessions[path]; sess != nil && !w.virtualPreview[path] && w.pluginInsertInFlight[path] == 0 {
			notes = append(notes, externalNoteRead{
				path: path, sess: sess, seq: w.diskWriteSeq[path], original: sess.DiskFingerprint(),
			})
		}
	}
	add(active)
	if !activeOnly {
		for _, path := range w.state.OpenTabs {
			if path != active {
				add(path)
			}
		}
	}
	w.mu.RUnlock()

	changed := notes[:0]
	for _, note := range notes {
		if externalSyncStopped(stop) {
			return
		}
		text, fp, err := w.readTabDiskNote(note.path)
		if err != nil {
			if activeOnly && errors.Is(err, os.ErrNotExist) {
				w.ReconcileExternalChanges("tab-missing")
			}
			continue
		}
		if fp == note.original {
			continue
		}
		note.diskText, note.diskFP = text, fp
		note.doc = w.codec.Parse(text)
		if binding, ok := w.tabBinding(note.path); ok && !binding.EnforceIdentity && binding.ConfineRoot == "" {
			if info, err := os.Lstat(note.path); err == nil && info.Mode().IsRegular() &&
				(binding.FileInfo == nil || !os.SameFile(info, binding.FileInfo)) {
				note.fileInfo = info
				note.fileID = files.PathIdentity(note.path)
			}
		}
		changed = append(changed, note)
	}
	if len(changed) == 0 {
		return
	}
	w.onExternalMain(stop, func() {
		if !w.bgSaveMu.TryLock() {
			w.ScheduleExternalReconcile("save-in-flight", 120*time.Millisecond)
			return
		}
		defer w.bgSaveMu.Unlock()
		reloaded := false
		for _, note := range changed {
			if w.applyExternalNote(folder, note) {
				reloaded = true
			}
		}
		if reloaded {
			w.RequestRedraw()
		}
	})
}

// applyExternalNote runs on the UI thread under bgSaveMu, with no save able
// to publish a new disk fingerprint between validation and the document update.
func (w *Workspace) applyExternalNote(folder string, note externalNoteRead) bool {
	path, sess := note.path, note.sess
	w.mu.Lock()
	if w.state.Folder != folder || w.sessions[path] != sess || w.virtualPreview[path] ||
		w.pluginInsertInFlight[path] > 0 || w.diskWriteSeq[path] != note.seq || sess.DiskFingerprint() != note.original {
		w.mu.Unlock()
		return false
	}
	source := w.noteSourceActiveLocked(path)
	// Source changes can exist in the buffer before the next render records them.
	unsyncedSource := source && w.noteSource.st.Buffer != nil && w.noteSource.st.Buffer.Version() != w.noteSource.codeVersion
	lastDisk, hasLastDisk := w.lastDiskMarkdown[path]
	dirty := sess.Dirty() || w.noteSourceDirtyLocked(path) || unsyncedSource ||
		(!source && hasLastDisk && sess.Markdown() != lastDisk.editorText)
	if dirty {
		prev, seen := w.lastExternalConflictFP[path]
		if w.lastExternalConflictFP == nil {
			w.lastExternalConflictFP = make(map[string]int64)
		}
		w.lastExternalConflictFP[path] = note.diskFP
		w.mu.Unlock()
		if !seen || prev != note.diskFP {
			w.notify(theme.Tr("当前笔记已被外部修改，已保留你的未保存内容", "Note changed externally; kept your unsaved edits"), "warning")
		}
		return false
	}
	w.mu.Unlock()

	// SetDoc reads annotation storage. Keep that work outside Workspace.mu.
	sess.SetDiskFingerprint(note.diskFP)
	sess.SetDoc(note.doc)
	sess.MarkSaved()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bumpDiskWriteSeqLocked(path)
	if w.lastDiskMarkdown == nil {
		w.lastDiskMarkdown = make(map[string]diskMarkdownSnapshot)
	}
	w.lastDiskMarkdown[path] = diskMarkdownSnapshot{diskText: note.diskText, editorText: sess.Markdown()}
	delete(w.lastExternalConflictFP, path)
	if binding, ok := w.tabBindings[path]; ok && note.fileInfo != nil && !binding.EnforceIdentity && binding.ConfineRoot == "" {
		binding.FileInfo, binding.FileIdentity = note.fileInfo, note.fileID
		w.tabBindings[path] = binding
	}
	if w.noteSourceActiveLocked(path) && w.noteSource.st.Buffer != nil {
		buf := w.noteSource.st.Buffer
		buf.Reset(note.diskText)
		buf.SetLanguage(codeed.LanguageForText(filepath.Base(path), note.diskText))
		w.noteSource.codeVersion = buf.Version()
		w.noteSource.text = note.diskText
		w.noteSource.savedText = note.diskText
		w.noteSource.dirty = false
		w.noteSource.recordedUndo = false
		w.noteSource.docRev = sess.DocRev()
	}
	return true
}
