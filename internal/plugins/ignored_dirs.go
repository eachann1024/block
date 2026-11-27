package plugins

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

type ignoredDirsList []string

func (dirs *ignoredDirsList) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '[' {
		return fmt.Errorf("ignored_dirs must be an array of directory names")
	}
	return json.Unmarshal(raw, (*[]string)(dirs))
}

func validateIgnoredDirs(dirs []string) error {
	if len(dirs) > 32 {
		return fmt.Errorf("allows at most 32 directory names")
	}
	for i, name := range dirs {
		if name == "" || name == "." || name == ".." || len(name) > 128 || strings.ContainsAny(name, "/\\\x00") {
			return fmt.Errorf("entry %d must be a single directory name of 1–128 bytes", i+1)
		}
	}
	return nil
}

// IgnoredDirs returns the current directory-name set for workspace scans.
// Callers must treat the snapshot as immutable.
func (s *Supervisor) IgnoredDirs() map[string]struct{} {
	dirs, _ := s.ignoredDirs.Load().(map[string]struct{})
	return dirs
}

func (s *Supervisor) refreshIgnoredDirsLocked() bool {
	var dirs map[string]struct{}
	add := func(names []string) {
		for _, name := range names {
			if dirs == nil {
				dirs = make(map[string]struct{})
			}
			dirs[name] = struct{}{}
		}
	}
	for _, app := range s.installed {
		if !app.Enabled || !app.Manifest.HasDeclaredPermission(PermUISidebar) {
			continue
		}
		if app.Manifest.Contributes != nil {
			add(app.Manifest.Contributes.IgnoredDirs)
		}
		add(s.runtimeIgnoredDirs[app.Manifest.ID])
	}
	if maps.Equal(s.IgnoredDirs(), dirs) {
		return false
	}
	s.ignoredDirs.Store(dirs)
	return true
}

func (s *Supervisor) notifyIgnoredDirsChanged(changed bool) {
	if changed && s.cfg.OnIgnoredDirsChanged != nil {
		s.cfg.OnIgnoredDirsChanged()
	}
}

func (s *Supervisor) handleSidebarIgnoredDirs(rt *Runtime, set bool, decode func(any) *rpcError) (any, *rpcError, bool) {
	if !rt.manifest.HasDeclaredPermission(PermUISidebar) {
		return nil, missingPerm(PermUISidebar), true
	}
	var p struct {
		Dirs []string `json:"dirs"`
	}
	if set {
		if err := decode(&p); err != nil {
			return nil, err, true
		}
		if p.Dirs == nil {
			return nil, rpcErr(-32602, "dirs must be an array"), true
		}
		if err := validateIgnoredDirs(p.Dirs); err != nil {
			return nil, rpcErr(-32602, err.Error()), true
		}
	}

	pluginID := rt.manifest.ID
	s.mu.Lock()
	if !s.isRuntimeActiveLocked(pluginID, rt) || s.replacingID == pluginID {
		s.mu.Unlock()
		return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
	}
	if !set {
		dirs := append([]string{}, s.runtimeIgnoredDirs[pluginID]...)
		s.mu.Unlock()
		return map[string]any{"dirs": dirs}, nil, true
	}
	if len(p.Dirs) == 0 {
		delete(s.runtimeIgnoredDirs, pluginID)
	} else {
		if s.runtimeIgnoredDirs == nil {
			s.runtimeIgnoredDirs = make(map[string][]string)
		}
		s.runtimeIgnoredDirs[pluginID] = p.Dirs
	}
	ignoredDirsChanged := s.refreshIgnoredDirsLocked()
	s.mu.Unlock()
	s.notifyIgnoredDirsChanged(ignoredDirsChanged)
	return map[string]any{"ok": true}, nil, true
}
