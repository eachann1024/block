package plugins

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

type noteSaveRequest struct {
	again  bool
	cancel context.CancelFunc
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

// NotifyNoteSaved dispatches note.saved asynchronously to every installed plugin declaring events:note_saved.
func (s *Supervisor) NotifyNoteSaved(absNotePath string) {
	s.notifyNoteWritten("", s.NotebookRoot(), absNotePath)
}

// NotifyNoteSavedFrom is NotifyNoteSaved for a save caused by a plugin's own
// editor write; that plugin does not receive the echo.
func (s *Supervisor) NotifyNoteSavedFrom(sourcePluginID, absNotePath string) {
	s.notifyNoteWritten(sourcePluginID, s.NotebookRoot(), absNotePath)
}

func (s *Supervisor) notifyNoteWritten(sourcePluginID, nbRoot, absNotePath string) {
	relSlash, ok := relativeNotePath(nbRoot, absNotePath)
	if !ok {
		for _, candidate := range s.Notebooks() {
			if rel, matched := relativeNotePath(candidate, absNotePath); matched {
				nbRoot = candidate
				relSlash = rel
				ok = true
				break
			}
		}
	}
	if !ok {
		return
	}

	installed := s.Installed()
	for _, app := range installed {
		if !app.Enabled || app.Manifest.ID == sourcePluginID || !app.Manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		pluginID := app.Manifest.ID
		s.queueNoteSaved(pluginID, nbRoot, absNotePath, relSlash)
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
		active.again = true
		s.mu.Unlock()
		return
	}
	request := &noteSaveRequest{}
	v.queuedSaves[path] = request
	s.mu.Unlock()
	s.notifyChanged()
	go s.dispatchNoteSaved(key, path, rel, request)
}

func (s *Supervisor) dispatchNoteSaved(key viewKey, absNotePath, relSlash string, request *noteSaveRequest) {
	defer s.lockPluginRPC(key.pluginID)()
	inst, installed := s.GetInstalled(key.pluginID)
	blockSync := installed && key.pluginID == "block-sync"
	s.mu.Lock()
	original := s.views[key]
	if original == nil || original.queuedSaves[absNotePath] != request {
		s.mu.Unlock()
		return
	}
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
		eventOutcome, err = rt.NotifyNoteSavedResult(eventCtx, key.notebookRoot, relSlash)
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
	if v.queuedSaves[absNotePath] != request {
		s.mu.Unlock()
		return
	}
	if request.again {
		go s.dispatchNoteSaved(key, absNotePath, relSlash, request)
	} else {
		delete(v.queuedSaves, absNotePath)
	}
	if v.BusyAction == "__retry_note_saved__" && (v.retrySavePath == "" || v.retrySavePath == absNotePath) {
		v.BusyAction = ""
		v.retrySavePath = ""
		v.ActionProgress = ActionProgress{}
	}
	if err != nil {
		if v.failedSaves == nil {
			v.failedSaves = make(map[string]string)
		}
		v.failedSaves[absNotePath] = relSlash + " · " + err.Error()
		delete(v.savePausedConfirm, absNotePath)
	} else if eventOutcome == "pending_confirm" {
		// The prior save failure has been replaced by the plugin's own pending-confirmation
		// workflow; clear the stale error so HasPluginError and the titlebar stop showing it.
		delete(v.failedSaves, absNotePath)
		delete(v.savePausedConfirm, absNotePath)
	} else if eventOutcome != "skipped" {
		delete(v.failedSaves, absNotePath)
		delete(v.savePausedConfirm, absNotePath)
	}
	if blockSync && eventOutcome == "synced" {
		delete(s.system.syncErrors, key.notebookRoot)
	}
	s.viewEpoch.Add(1)
	refreshPage := v.Page != nil && v.BusyAction == ""
	editSeq := v.editSeq
	var draftVals map[string]any
	if refreshPage {
		draftVals = pendingDraftValuesLocked(v)
	}
	s.mu.Unlock()
	s.notifyChanged()
	if blockSync && err != nil && !syncFailureAlreadyReported(err) && !errors.Is(ctx.Err(), context.Canceled) {
		s.reportSystemSyncFailure(inst, rt, key.notebookRoot, timedOut, false)
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
