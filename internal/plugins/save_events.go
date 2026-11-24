package plugins

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

type noteSaveRequest struct {
	event      string
	folder     string
	again      bool
	superseded bool
	cancel     context.CancelFunc
}

type failedNoteEvent struct {
	event     string
	absPath   string
	relSlash  string
	relFolder string
}

func relativeNotePath(nbRoot, absNotePath string) (string, bool) {
	if nbRoot == "" || absNotePath == "" {
		return "", false
	}
	rootClean, err := filepath.Abs(nbRoot)
	if err != nil {
		return "", false
	}
	noteClean, err := filepath.Abs(absNotePath)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootClean, noteClean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (s *Supervisor) resolveNotebookTarget(nbRoot, absPath string) (string, string, bool) {
	relSlash, ok := relativeNotePath(nbRoot, absPath)
	if !ok {
		for _, candidate := range s.Notebooks() {
			if rel, matched := relativeNotePath(candidate, absPath); matched {
				nbRoot = candidate
				relSlash = rel
				ok = true
				break
			}
		}
	}
	return nbRoot, relSlash, ok
}

// NotifyNoteSaved dispatches note.saved asynchronously to every installed plugin declaring events:note_saved.
func (s *Supervisor) NotifyNoteSaved(absNotePath string) {
	s.notifyNoteWritten("", s.NotebookRoot(), absNotePath)
}

// NotifyNoteSavedFrom is NotifyNoteSaved for a save caused by a plugin's own
// editor write; that plugin does not receive the echo.
func (s *Supervisor) NotifyNoteSavedFrom(sourcePluginID, absNotePath string) {
	s.notifyNoteWritten(sourcePluginID, s.NotebookRoot(), absNotePath)
}

// NotifyNoteDeleted dispatches note.deleted asynchronously to every enabled plugin declaring events:note_saved.
func (s *Supervisor) NotifyNoteDeleted(absPath string) {
	s.NotifyNoteDeletedFrom("", absPath)
}

// NotifyNoteDeletedFrom is NotifyNoteDeleted for a deletion caused by a plugin's
// own host.notes.trash call; that plugin does not receive the echo.
func (s *Supervisor) NotifyNoteDeletedFrom(sourcePluginID, absPath string) {
	if strings.TrimSpace(absPath) == "" {
		return
	}
	cleanPath := filepath.Clean(absPath)
	nbRoot, relSlash, ok := s.resolveNotebookTarget(s.NotebookRoot(), cleanPath)
	if !ok || relSlash == "" || relSlash == "." {
		return
	}
	for _, app := range s.Installed() {
		if !app.Enabled || app.Manifest.ID == sourcePluginID || !app.Manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		s.queueNoteDeleted(app.Manifest.ID, nbRoot, cleanPath, relSlash)
	}
}

// NotifyFolderDeleted dispatches a folder-level note.deleted event asynchronously
// to every enabled plugin declaring events:note_saved.
func (s *Supervisor) NotifyFolderDeleted(absDirPath string) {
	if strings.TrimSpace(absDirPath) == "" {
		return
	}
	cleanDir := filepath.Clean(absDirPath)
	nbRoot, relFolder, ok := s.resolveNotebookTarget(s.NotebookRoot(), cleanDir)
	if !ok || relFolder == "" || relFolder == "." {
		return
	}
	for _, app := range s.Installed() {
		if !app.Enabled || !app.Manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		s.queueFolderDeleted(app.Manifest.ID, nbRoot, cleanDir, relFolder)
	}
}

func (s *Supervisor) notifyNoteWritten(sourcePluginID, nbRoot, absNotePath string) {
	if strings.TrimSpace(absNotePath) == "" {
		return
	}
	cleanPath := filepath.Clean(absNotePath)
	resolvedRoot, relSlash, ok := s.resolveNotebookTarget(nbRoot, cleanPath)
	if !ok || relSlash == "" || relSlash == "." {
		return
	}

	installed := s.Installed()
	for _, app := range installed {
		if !app.Enabled || app.Manifest.ID == sourcePluginID || !app.Manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		pluginID := app.Manifest.ID
		s.queueNoteSaved(pluginID, resolvedRoot, cleanPath, relSlash)
	}
}

func (s *Supervisor) HasPluginError(id string) bool {
	if inst, ok := s.GetInstalled(id); !ok {
		return false
	} else if !inst.Enabled {
		s.mu.RLock()
		hasUninstErr := s.uninstallErrors[id] != ""
		s.mu.RUnlock()
		return hasUninstErr
	}
	root := s.NotebookRoot()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.uninstallErrors[id] != "" {
		return true
	}
	for key, v := range s.views {
		if key.pluginID != id {
			continue
		}
		for path := range v.failedSaves {
			if v.queuedSaves[path] == nil {
				return true
			}
		}
		if key.notebookRoot == root {
			if v.Error != "" {
				return true
			}
			if v.LastActionWasHeader && strings.EqualFold(strings.TrimSpace(v.LastActionTone), "danger") {
				return true
			}
		}
	}
	return false
}

func (s *Supervisor) queueNoteSaved(pluginID, root, path, rel string) {
	key := viewKey{pluginID, root, SurfaceSettingsPage}
	s.mu.Lock()
	v := s.views[key]
	if v == nil {
		v = &PluginViewState{Values: map[string]any{}, lastNotebookRoot: root}
		s.views[key] = v
	}
	if v.queuedSaves == nil {
		v.queuedSaves = make(map[string]*noteSaveRequest)
	}
	if active := v.queuedSaves[path]; active != nil {
		active.event = "note.saved"
		active.folder = ""
		active.superseded = false
		active.again = true
		s.mu.Unlock()
		return
	}
	request := &noteSaveRequest{
		event: "note.saved",
	}
	v.queuedSaves[path] = request
	s.mu.Unlock()
	s.notifyChanged()
	go s.dispatchNoteEvent(key, path, rel, request)
}

func (s *Supervisor) queueNoteDeleted(pluginID, root, path, rel string) {
	key := viewKey{pluginID, root, SurfaceSettingsPage}
	s.mu.Lock()
	v := s.views[key]
	if v == nil {
		v = &PluginViewState{Values: map[string]any{}, lastNotebookRoot: root}
		s.views[key] = v
	}
	delete(v.failedSaves, path)
	delete(v.failedSaveEvents, path)
	delete(v.savePausedConfirm, path)
	if v.queuedSaves == nil {
		v.queuedSaves = make(map[string]*noteSaveRequest)
	}
	if active := v.queuedSaves[path]; active != nil {
		// Never cancel an active RPC so in-progress git commits are not interrupted.
		active.event = "note.deleted"
		active.folder = ""
		active.superseded = false
		active.again = true
		s.viewEpoch.Add(1)
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	request := &noteSaveRequest{
		event: "note.deleted",
	}
	v.queuedSaves[path] = request
	s.viewEpoch.Add(1)
	s.mu.Unlock()
	s.notifyChanged()
	go s.dispatchNoteEvent(key, path, rel, request)
}

func (s *Supervisor) queueFolderDeleted(pluginID, root, cleanDir, relFolder string) {
	key := viewKey{pluginID, root, SurfaceSettingsPage}
	prefix := cleanDir + string(filepath.Separator)
	matchesPrefix := func(p string) bool {
		cp := filepath.Clean(p)
		return cp == cleanDir || strings.HasPrefix(cp, prefix)
	}
	queueKey := "dir:" + cleanDir
	s.mu.Lock()
	v := s.views[key]
	if v == nil {
		v = &PluginViewState{Values: map[string]any{}, lastNotebookRoot: root}
		s.views[key] = v
	}
	for p := range v.failedSaves {
		target := strings.TrimPrefix(p, "dir:")
		if matchesPrefix(target) {
			delete(v.failedSaves, p)
			delete(v.failedSaveEvents, p)
			delete(v.savePausedConfirm, p)
		}
	}
	for p := range v.savePausedConfirm {
		target := strings.TrimPrefix(p, "dir:")
		if matchesPrefix(target) {
			delete(v.savePausedConfirm, p)
		}
	}
	if v.queuedSaves == nil {
		v.queuedSaves = make(map[string]*noteSaveRequest)
	}
	for qk, req := range v.queuedSaves {
		if req == nil || qk == queueKey {
			continue
		}
		target := strings.TrimPrefix(qk, "dir:")
		if matchesPrefix(target) {
			req.again = false
			req.superseded = true
		}
	}
	if active := v.queuedSaves[queueKey]; active != nil {
		active.event = "note.deleted"
		active.folder = relFolder
		active.superseded = false
		active.again = true
		s.viewEpoch.Add(1)
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	request := &noteSaveRequest{
		event:  "note.deleted",
		folder: relFolder,
	}
	v.queuedSaves[queueKey] = request
	s.viewEpoch.Add(1)
	s.mu.Unlock()
	s.notifyChanged()
	go s.dispatchNoteEvent(key, queueKey, "", request)
}

func (s *Supervisor) dispatchNoteEvent(key viewKey, queueKey, relSlash string, request *noteSaveRequest) {
	defer s.lockPluginRPC(key.pluginID)()
	inst, installed := s.GetInstalled(key.pluginID)
	blockSync := installed && key.pluginID == "block-sync"
	s.mu.Lock()
	original := s.views[key]
	if original == nil || original.queuedSaves[queueKey] != request {
		s.mu.Unlock()
		return
	}
	if request.superseded {
		delete(original.queuedSaves, queueKey)
		if original.BusyAction == "__retry_note_saved__" && (original.retrySavePath == "" || original.retrySavePath == queueKey) {
			original.BusyAction = ""
			original.retrySavePath = ""
			original.ActionProgress = ActionProgress{}
		}
		s.viewEpoch.Add(1)
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	eventName := request.event
	if eventName == "" {
		eventName = "note.saved"
	}
	folderName := request.folder
	request.again = false
	timeout := 45 * time.Second
	if blockSync {
		timeout = 135 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	request.cancel = cancel
	original.activeSaves++
	s.mu.Unlock()
	defer cancel()
	startCtx := ctx
	if blockSync {
		var finishStart context.CancelFunc
		startCtx, finishStart = context.WithTimeout(ctx, 15*time.Second)
		defer finishStart()
	}
	rt, err := s.ensureRuntime(startCtx, key.pluginID, key.notebookRoot)
	timedOut := errors.Is(startCtx.Err(), context.DeadlineExceeded)
	var eventOutcome string
	if err == nil {
		eventCtx := ctx
		if blockSync {
			var finishEvent context.CancelFunc
			eventCtx, finishEvent = context.WithTimeout(ctx, 120*time.Second)
			defer finishEvent()
		}
		eventOutcome, err = rt.NotifyNoteEventResult(eventCtx, eventName, key.notebookRoot, relSlash, folderName)
		timedOut = errors.Is(eventCtx.Err(), context.DeadlineExceeded)
	}
	s.mu.Lock()
	v := s.views[key]
	if original.activeSaves > 0 {
		original.activeSaves--
	}
	request.cancel = nil
	if v != original {
		s.mu.Unlock()
		return
	}
	if v.queuedSaves[queueKey] != request {
		s.mu.Unlock()
		return
	}
	if request.superseded {
		delete(v.queuedSaves, queueKey)
		if v.BusyAction == "__retry_note_saved__" && (v.retrySavePath == "" || v.retrySavePath == queueKey) {
			v.BusyAction = ""
			v.retrySavePath = ""
			v.ActionProgress = ActionProgress{}
		}
		s.viewEpoch.Add(1)
		s.mu.Unlock()
		s.notifyChanged()
		return
	}
	requeue := request.again
	supersededByDelete := requeue && eventName == "note.saved" && request.event == "note.deleted"
	if requeue {
		go s.dispatchNoteEvent(key, queueKey, relSlash, request)
	} else {
		delete(v.queuedSaves, queueKey)
	}
	if v.BusyAction == "__retry_note_saved__" && (v.retrySavePath == "" || v.retrySavePath == queueKey) {
		v.BusyAction = ""
		v.retrySavePath = ""
		v.ActionProgress = ActionProgress{}
	}
	displayPath := relSlash
	if displayPath == "" {
		displayPath = folderName
	}
	if err != nil {
		if !supersededByDelete {
			if v.failedSaves == nil {
				v.failedSaves = make(map[string]string)
			}
			if v.failedSaveEvents == nil {
				v.failedSaveEvents = make(map[string]failedNoteEvent)
			}
			v.failedSaves[queueKey] = displayPath + " · " + err.Error()
			v.failedSaveEvents[queueKey] = failedNoteEvent{
				event:     eventName,
				absPath:   strings.TrimPrefix(queueKey, "dir:"),
				relSlash:  relSlash,
				relFolder: folderName,
			}
			delete(v.savePausedConfirm, queueKey)
		}
	} else if eventOutcome == "pending_confirm" {
		// The prior save failure has been replaced by the plugin's own pending-confirmation
		// workflow; clear the stale error so HasPluginError and the titlebar stop showing it.
		delete(v.failedSaves, queueKey)
		delete(v.failedSaveEvents, queueKey)
		delete(v.savePausedConfirm, queueKey)
	} else if eventOutcome != "skipped" {
		delete(v.failedSaves, queueKey)
		delete(v.failedSaveEvents, queueKey)
		delete(v.savePausedConfirm, queueKey)
	}
	if blockSync && eventOutcome == "synced" {
		delete(s.system.syncErrors, key.notebookRoot)
	}
	s.viewEpoch.Add(1)
	refreshPage := v.Page != nil && v.BusyAction == "" && !supersededByDelete
	editSeq := v.editSeq
	var draftVals map[string]any
	if refreshPage {
		draftVals = pendingDraftValuesLocked(v)
	}
	s.mu.Unlock()
	s.notifyChanged()
	if blockSync && err != nil && !supersededByDelete && !syncFailureAlreadyReported(err) && !errors.Is(ctx.Err(), context.Canceled) {
		s.reportSystemSyncFailure(inst, rt, key.notebookRoot, timedOut, syncStateNotSaved(err))
	}
	if err == nil && refreshPage {
		if page, returnedVals, err := rt.RenderWithValuesResult(ctx, SurfaceSettingsPage, draftVals); err == nil {
			s.mu.Lock()
			if s.views[key] == v {
				v.Page = page
				mergePageValuesUpToSeq(v, page, returnedVals, editSeq, true)
				v.formVersion = s.formVersions[surfaceKey{key.pluginID, SurfaceSettingsPage}]
				if page.Status != "" {
					v.Notice = ""
					if v.Error == "" && v.RetryKind != RetryKindAction {
						v.LastActionID = ""
						v.LastActionTone = ""
						v.LastActionMessage = ""
						v.LastActionButtonActive = false
						v.LastActionWasHeader = false
					}
				}
			}
			s.mu.Unlock()
			s.notifyChanged()
		}
	}
}
