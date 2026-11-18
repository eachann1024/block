package plugins

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

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
	nbRoot := s.NotebookRoot()
	relSlash, ok := relativeNotePath(nbRoot, absNotePath)
	if !ok {
		return
	}

	installed := s.Installed()
	for _, app := range installed {
		if !app.Enabled || !app.Manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		pluginID := app.Manifest.ID
		s.queueNoteSaved(pluginID, nbRoot, absNotePath, relSlash)
	}
}

func (s *Supervisor) HasPluginError(id string) bool {
	key := viewKey{id, s.NotebookRoot()}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.views[key]
	return v != nil && (v.Error != "" || len(v.failedSaves) > 0)
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
		v.queuedSaves = make(map[string]bool)
	}
	if _, active := v.queuedSaves[path]; active {
		v.queuedSaves[path] = true
		s.mu.Unlock()
		return
	}
	v.queuedSaves[path] = false
	v.PendingSaves = len(v.queuedSaves)
	s.mu.Unlock()
	s.notifyChanged()
	go s.dispatchNoteSaved(key, path, rel, v)
}

func (s *Supervisor) dispatchNoteSaved(key viewKey, absNotePath, relSlash string, original *PluginViewState) {
	defer s.lockPluginRPC(key.pluginID)()
	s.mu.Lock()
	if s.views[key] != original {
		s.mu.Unlock()
		return
	}
	original.queuedSaves[absNotePath] = false
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt, err := s.ensureRuntime(ctx, key.pluginID, key.notebookRoot)
	if err == nil {
		err = rt.NotifyNoteSaved(ctx, relSlash)
	}
	s.mu.Lock()
	v := s.views[key]
	if v != original {
		s.mu.Unlock()
		return
	}
	if v.queuedSaves[absNotePath] {
		go s.dispatchNoteSaved(key, absNotePath, relSlash, v)
	} else {
		delete(v.queuedSaves, absNotePath)
	}
	v.PendingSaves = len(v.queuedSaves)
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
	var values map[string]any
	if refreshPage {
		values = make(map[string]any, len(v.Values))
		for id, value := range v.Values {
			values[id] = value
		}
	}
	s.mu.Unlock()
	s.notifyChanged()
	if err == nil && refreshPage {
		if page, err := rt.RenderWithValues(ctx, SurfaceSettingsPage, values); err == nil {
			s.mu.Lock()
			if s.views[key] == v && v.editSeq == editSeq {
				v.Page = page
				if page.Status != "" {
					v.Notice = ""
				}
			}
			s.mu.Unlock()
			s.notifyChanged()
		}
	}
}
