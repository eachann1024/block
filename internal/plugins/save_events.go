package plugins

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

type noteSaveRequest struct {
	again bool
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
	if inst, ok := s.GetInstalled(id); !ok || !inst.Enabled {
		return false
	}
	root := s.NotebookRoot()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, v := range s.views {
		if key.pluginID == id && (len(v.failedSaves) > 0 || (key.notebookRoot == root && v.Error != "")) {
			return true
		}
	}
	return false
}

func (s *Supervisor) queueNoteSaved(pluginID, root, path, rel string) {
	key := viewKey{pluginID, root}
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
	s.mu.Lock()
	original := s.views[key]
	if original == nil || original.queuedSaves[absNotePath] != request {
		s.mu.Unlock()
		return
	}
	request.again = false
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	original.saveCancel = cancel
	s.mu.Unlock()
	defer cancel()
	rt, err := s.ensureRuntime(ctx, key.pluginID, key.notebookRoot)
	if err == nil {
		err = rt.NotifyNoteSaved(ctx, key.notebookRoot, relSlash)
	}
	s.mu.Lock()
	v := s.views[key]
	if v != original {
		s.mu.Unlock()
		return
	}
	v.saveCancel = nil
	if v.queuedSaves[absNotePath] != request {
		s.mu.Unlock()
		return
	}
	if request.again {
		go s.dispatchNoteSaved(key, absNotePath, relSlash, request)
	} else {
		delete(v.queuedSaves, absNotePath)
	}
	if v.BusyAction == "__retry_note_saved__" {
		v.BusyAction = ""
	}
	if err != nil {
		if v.failedSaves == nil {
			v.failedSaves = make(map[string]string)
		}
		v.failedSaves[absNotePath] = relSlash + " · " + err.Error()
	} else {
		delete(v.failedSaves, absNotePath)
	}
	refreshPage := v.Page != nil && v.BusyAction == ""
	editSeq := v.editSeq
	s.mu.Unlock()
	s.notifyChanged()
	if err == nil && refreshPage {
		if page, err := rt.Render(ctx, SurfaceSettingsPage); err == nil {
			s.mu.Lock()
			if s.views[key] == v && v.editSeq == editSeq {
				v.Page = page
				mergePageValues(v, page, nil)
				v.formVersion = s.formVersions[key.pluginID]
				if page.Status != "" {
					v.Notice = ""
				}
			}
			s.mu.Unlock()
			s.notifyChanged()
		}
	}
}
