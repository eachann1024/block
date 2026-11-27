package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	maxStageStateBytes = 2 * 1024 * 1024
	maxStageAssetBytes = 4 * 1024 * 1024
	maxStageWebFiles   = 128
)

var stageExternalHTTPRe = regexp.MustCompile(`(?i)https?://`)

// StageContrib declares a plugin-owned WebView stage surface under web/.
type StageContrib struct {
	ID         string        `json:"id"`
	Title      LocalizedText `json:"title"`
	Entry      string        `json:"entry"`
	FullWindow bool          `json:"full_window,omitempty"`
}

// ActiveStage describes the currently open plugin stage presented by the host.
type ActiveStage struct {
	PluginID   string          `json:"plugin_id"`
	PluginRoot string          `json:"plugin_root"`
	StageID    string          `json:"stage_id"`
	Title      string          `json:"title,omitempty"`
	Entry      string          `json:"entry"`
	EntryPath  string          `json:"entry_path"`
	FullWindow bool            `json:"full_window"`
	Route      string          `json:"route,omitempty"`
	State      json.RawMessage `json:"state,omitempty"`
}

// StageEvent carries a host.stage.emit message from a plugin runtime to the open stage UI.
type StageEvent struct {
	PluginID string          `json:"plugin_id"`
	StageID  string          `json:"stage_id"`
	Event    string          `json:"event"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

func (c *Contributes) hasStage(id string) bool {
	return c != nil && slices.ContainsFunc(c.Stages, func(x StageContrib) bool { return x.ID == id })
}

func (c *Contributes) stageByID(id string) (StageContrib, bool) {
	if c == nil {
		return StageContrib{}, false
	}
	for _, st := range c.Stages {
		if st.ID == id {
			return st, true
		}
	}
	return StageContrib{}, false
}

func hasStagePermission(m Manifest) bool {
	return m.HasPermission(PermUIAppearance) || m.HasPermission(PermUICommands) || m.HasPermission(PermUIPanels)
}

func hasDeclaredStagePermission(m Manifest) bool {
	return m.HasDeclaredPermission(PermUIAppearance) || m.HasDeclaredPermission(PermUICommands) || m.HasDeclaredPermission(PermUIPanels)
}

func validateStageEntryPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("contributes.stages.entry is required")
	}
	if strings.ContainsAny(p, "\\:\x00\r\n") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("contributes.stages.entry %q must be a relative slash path", raw)
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("contributes.stages.entry %q escapes plugin directory", raw)
	}
	if !strings.HasPrefix(clean, "web/") || clean == "web/" {
		return "", fmt.Errorf("contributes.stages.entry %q must reside inside web/", raw)
	}
	return clean, nil
}

// ValidatePluginStages verifies stage entry files exist under web/ and contain no external HTTP(S) asset links.
func ValidatePluginStages(pluginDir string, m *Manifest) error {
	if m == nil || m.Contributes == nil || len(m.Contributes.Stages) == 0 {
		return nil
	}
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return err
	}
	defer root.Close()

	webInfo, err := root.Lstat("web")
	if err != nil {
		return fmt.Errorf("stages web directory: %w", err)
	}
	if webInfo.Mode()&os.ModeSymlink != 0 || !webInfo.IsDir() {
		return errors.New("stages web directory must be a real directory")
	}

	fileCount := 0
	if err := fs.WalkDir(root.FS(), "web", func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := root.Lstat(rel)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("stage asset %s must not be a symlink", rel)
		}
		if d.IsDir() {
			return nil
		}
		fileCount++
		if fileCount > maxStageWebFiles {
			return fmt.Errorf("web/ exceeds %d files", maxStageWebFiles)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("stage asset %s must be a regular file", rel)
		}
		raw, err := readPluginFile(root, rel, maxStageAssetBytes)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		if loc := stageExternalHTTPRe.FindIndex(raw); loc != nil {
			return fmt.Errorf("stage asset %s must not reference external http:// or https:// URLs", rel)
		}
		return nil
	}); err != nil {
		return err
	}

	for _, st := range m.Contributes.Stages {
		clean, err := validateStageEntryPath(st.Entry)
		if err != nil {
			return fmt.Errorf("stages %q: %w", st.ID, err)
		}
		f, _, err := openPluginFile(root, clean)
		if err != nil {
			return fmt.Errorf("stages %q entry %s: %w", st.ID, clean, err)
		}
		_ = f.Close()
	}
	return nil
}

func cloneActiveStage(st *ActiveStage) *ActiveStage {
	if st == nil {
		return nil
	}
	cp := *st
	if len(st.State) > 0 {
		cp.State = bytes.Clone(st.State)
	}
	return &cp
}

// ActiveStage returns a copy of the currently open plugin stage, or nil if none is open.
func (s *Supervisor) ActiveStage() *ActiveStage {
	if s == nil {
		return nil
	}
	s.stageMu.RLock()
	defer s.stageMu.RUnlock()
	return cloneActiveStage(s.activeStage)
}

// CloseActiveStage closes whichever plugin stage is currently open.
func (s *Supervisor) CloseActiveStage() {
	if s == nil {
		return
	}
	s.stageMu.Lock()
	if s.activeStage == nil {
		s.stageMu.Unlock()
		return
	}
	closedPluginID := s.activeStage.PluginID
	s.activeStage = nil
	onChange := s.cfg.OnStageChange
	s.stageMu.Unlock()
	s.touchPluginRuntimeActivity(closedPluginID)
	if onChange != nil {
		onChange(nil)
	}
	s.notifyRedraw()
}

// CloseStage closes the active stage if it matches pluginID and (when non-empty) stageID.
func (s *Supervisor) CloseStage(pluginID, stageID string) {
	if s == nil || strings.TrimSpace(pluginID) == "" {
		return
	}
	targetID := strings.TrimSpace(stageID)
	s.stageMu.Lock()
	if s.activeStage == nil || s.activeStage.PluginID != pluginID || (targetID != "" && s.activeStage.StageID != targetID) {
		s.stageMu.Unlock()
		return
	}
	s.activeStage = nil
	onChange := s.cfg.OnStageChange
	s.stageMu.Unlock()
	s.touchPluginRuntimeActivity(pluginID)
	if onChange != nil {
		onChange(nil)
	}
	s.notifyRedraw()
}

func (s *Supervisor) closeStageForPlugin(pluginID string) {
	s.CloseStage(pluginID, "")
}

func (s *Supervisor) dispatchStageHostMethod(rt *Runtime, method string, params json.RawMessage) (any, *rpcError, bool) {
	m := rt.manifest
	if !hasStagePermission(m) {
		return nil, rpcErr(-32001, fmt.Sprintf("missing permission: %s, %s, or %s", PermUIAppearance, PermUICommands, PermUIPanels)), true
	}
	decode := func(v any) *rpcError {
		if len(params) == 0 {
			return nil
		}
		if err := json.Unmarshal(params, v); err != nil {
			return rpcErr(-32602, "invalid params: "+err.Error())
		}
		return nil
	}

	switch method {
	case "host.stage.open":
		var p struct {
			ID    string          `json:"id"`
			Route string          `json:"route"`
			Room  string          `json:"room"`
			State json.RawMessage `json:"state"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		if m.Contributes == nil || len(m.Contributes.Stages) == 0 {
			return nil, rpcErr(-32602, "manifest declares no stages"), true
		}
		var st StageContrib
		if id := strings.TrimSpace(p.ID); id != "" {
			var found bool
			st, found = m.Contributes.stageByID(id)
			if !found {
				return nil, rpcErr(-32602, "unknown stage: "+p.ID), true
			}
		} else if len(m.Contributes.Stages) == 1 {
			st = m.Contributes.Stages[0]
		} else {
			return nil, rpcErr(-32602, "stage id is required"), true
		}
		cleanEntry, err := validateStageEntryPath(st.Entry)
		if err != nil {
			return nil, rpcErr(-32602, err.Error()), true
		}
		if len(p.State) > maxStageStateBytes {
			return nil, rpcErr(-32602, "stage state exceeds size limit"), true
		}
		var stateCopy json.RawMessage
		if len(bytes.TrimSpace(p.State)) > 0 {
			if !json.Valid(p.State) {
				return nil, rpcErr(-32602, "stage state must be valid JSON"), true
			}
			stateCopy = bytes.Clone(p.State)
		}
		root, err := os.OpenRoot(rt.cfg.PluginDir)
		if err != nil {
			return nil, rpcErr(-32005, "open plugin directory: "+err.Error()), true
		}
		f, _, err := openPluginFile(root, cleanEntry)
		_ = root.Close()
		if err != nil {
			return nil, rpcErr(-32005, fmt.Sprintf("stage entry %s unavailable: %v", cleanEntry, err)), true
		}
		_ = f.Close()

		route := strings.TrimSpace(p.Route)
		if route == "" {
			route = strings.TrimSpace(p.Room)
		}
		if len(route) > 256 || strings.ContainsAny(route, "\r\n\x00") {
			return nil, rpcErr(-32602, "invalid stage route"), true
		}

		s.mu.RLock()
		active := s.isRuntimeActiveLocked(m.ID, rt)
		s.mu.RUnlock()
		if !active {
			return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
		}

		next := &ActiveStage{
			PluginID:   m.ID,
			PluginRoot: rt.cfg.PluginDir,
			StageID:    st.ID,
			Title:      st.Title.Resolve(s.Locale()),
			Entry:      cleanEntry,
			EntryPath:  filepath.Join(rt.cfg.PluginDir, filepath.FromSlash(cleanEntry)),
			FullWindow: st.FullWindow,
			Route:      route,
			State:      stateCopy,
		}
		s.stageMu.Lock()
		s.activeStage = cloneActiveStage(next)
		onChange := s.cfg.OnStageChange
		s.stageMu.Unlock()
		if onChange != nil {
			onChange(cloneActiveStage(next))
		}
		return map[string]any{"ok": true, "open": true, "stage": next}, nil, true

	case "host.stage.close":
		var p struct {
			ID string `json:"id"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		targetID := strings.TrimSpace(p.ID)
		s.stageMu.Lock()
		closed := false
		var onChange func(*ActiveStage)
		if s.activeStage != nil && s.activeStage.PluginID == m.ID && (targetID == "" || s.activeStage.StageID == targetID) {
			s.activeStage = nil
			closed = true
			onChange = s.cfg.OnStageChange
		}
		s.stageMu.Unlock()
		if closed {
			s.touchPluginRuntimeActivity(m.ID)
			if onChange != nil {
				onChange(nil)
			}
		}
		return map[string]any{"ok": true, "closed": closed}, nil, true

	case "host.stage.state":
		s.stageMu.RLock()
		cur := cloneActiveStage(s.activeStage)
		s.stageMu.RUnlock()
		if cur == nil || cur.PluginID != m.ID {
			return map[string]any{"open": false}, nil, true
		}
		return map[string]any{
			"open":     true,
			"stage_id": cur.StageID,
			"route":    cur.Route,
			"state":    cur.State,
		}, nil, true

	case "host.stage.emit":
		var p struct {
			ID      string          `json:"id"`
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if e := decode(&p); e != nil {
			return nil, e, true
		}
		evName := strings.TrimSpace(p.Event)
		if evName == "" || len(evName) > 128 || strings.ContainsAny(evName, "\r\n\x00") {
			return nil, rpcErr(-32602, "host.stage.emit requires a valid event name"), true
		}
		if m.Contributes == nil || len(m.Contributes.Stages) == 0 {
			return nil, rpcErr(-32602, "manifest declares no stages"), true
		}
		stageID := strings.TrimSpace(p.ID)
		if stageID == "" {
			if len(m.Contributes.Stages) == 1 {
				stageID = m.Contributes.Stages[0].ID
			} else {
				return nil, rpcErr(-32602, "stage id is required"), true
			}
		} else if !m.Contributes.hasStage(stageID) {
			return nil, rpcErr(-32602, "unknown stage: "+p.ID), true
		}
		if len(p.Payload) > maxStageStateBytes {
			return nil, rpcErr(-32602, "stage event payload exceeds size limit"), true
		}
		var payloadCopy json.RawMessage
		if len(bytes.TrimSpace(p.Payload)) > 0 {
			if !json.Valid(p.Payload) {
				return nil, rpcErr(-32602, "stage event payload must be valid JSON"), true
			}
			payloadCopy = bytes.Clone(p.Payload)
		}

		s.mu.RLock()
		active := s.isRuntimeActiveLocked(m.ID, rt)
		s.mu.RUnlock()
		if !active {
			return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
		}
		s.stageMu.RLock()
		onEmit := s.cfg.OnStageEmit
		s.stageMu.RUnlock()
		if onEmit != nil {
			onEmit(StageEvent{
				PluginID: m.ID,
				StageID:  stageID,
				Event:    evName,
				Payload:  payloadCopy,
			})
		}
		return map[string]any{"ok": true}, nil, true
	}

	return nil, rpcErr(-32601, "unknown host method: "+method), true
}

// InvokeStageAction forwards an action from a stage WebView to the plugin's
// plugin.stage_action RPC handler.
func (s *Supervisor) InvokeStageAction(ctx context.Context, pluginID, stageID, action string, payload any) (json.RawMessage, error) {
	if s == nil {
		return nil, errors.New("plugin supervisor is nil")
	}
	pid := strings.TrimSpace(pluginID)
	act := strings.TrimSpace(action)
	if pid == "" {
		return nil, errors.New("plugin_id is required")
	}
	if act == "" {
		return nil, errors.New("action is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	unlock, err := s.lockPluginRPCContext(ctx, pid)
	if err != nil {
		return nil, err
	}
	defer unlock()

	nbRoot := s.NotebookRoot()
	notebookEpoch := s.notebookEpoch.Load()
	rt, err := s.ensureRuntime(ctx, pid, nbRoot)
	if err != nil {
		return nil, err
	}
	rt.SetNotebookRoot(nbRoot)
	rt.SetActiveNote(s.activeNoteFor(nbRoot))

	sid := strings.TrimSpace(stageID)
	if sid == "" && rt.manifest.Contributes != nil && len(rt.manifest.Contributes.Stages) == 1 {
		sid = rt.manifest.Contributes.Stages[0].ID
	}
	if sid != "" && !rt.manifest.Contributes.hasStage(sid) {
		return nil, fmt.Errorf("stage %q not declared in manifest", sid)
	}

	params := map[string]any{
		"stage_id": sid,
		"action":   act,
		"payload":  payload,
	}
	if payload == nil {
		params["payload"] = map[string]any{}
	}

	s.mu.Lock()
	s.activeActionRT[pid] = rt
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.activeActionRT[pid] == rt {
			delete(s.activeActionRT, pid)
		}
		s.mu.Unlock()
	}()

	scope := systemAction{notebookRoot: nbRoot, epoch: notebookEpoch}
	actionCtx := context.WithValue(ctx, systemActionKey{}, scope)

	var out json.RawMessage
	if err := rt.call(actionCtx, "plugin.stage_action", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}
